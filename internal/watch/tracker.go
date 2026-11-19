// Package watch tells what happens to the watched workloads of one namespace: when an install is
// ready, when a transition has finished or stalled, and which pods were replaced. It hides the
// rollout rules of each kind, pod ownership and the settle time. The only change it makes in the
// cluster is the restart of the watched workloads. A workload that matches the selector at any
// moment of a transition is watched in it for the rest of that transition.
package watch

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"rollproof/internal/verdict"
)

// Workload is one watched workload. Kind is deployment, statefulset or daemonset.
type Workload struct {
	Kind, Name string
}

// String returns the kind, a slash and the name: deployment/orders.
func (w Workload) String() string {
	return w.Kind + "/" + w.Name
}

// TrackerOptions is what a Tracker needs: which workloads are watched, limits.settle,
// limits.timeout, and, for the text of its sentences, the namespace's name and the number of
// worker nodes.
type TrackerOptions struct {
	Namespace       string
	Selector        labels.Selector
	Settle, Timeout time.Duration
	Workers         int
}

// Transition is what happened in one transition. Finished is when it last came to be finished,
// zero for a stall. Ended is when its window ended. Missed is when the watch of the cluster first
// missed changes in the window, zero when it missed none. The string lists are sentences for the
// report: what started the settle time again, what is not complete, which pods could not be
// scheduled. LastEvent is the last event of the namespace about what a stall names.
type Transition struct {
	Name                                      verdict.WindowName
	Began, Finished, Ended, Missed            time.Time
	Stalled                                   bool
	SettleRestarts, Incomplete, Unschedulable []string
	Unsupported                               []Workload
	Workloads                                 []Count
	Replaced                                  bool
	// LastEvent is the last event of the namespace about what a stall names, when the window
	// stalled: a watched workload that is not complete, a ReplicaSet of such a Deployment, or a
	// pod of one that is not ready or is stopping, written KIND/NAME: REASON: MESSAGE with kind
	// in lower case and line breaks as spaces. Empty when it did not stall, no such event
	// exists, or the events could not be listed in the settle time.
	LastEvent string
}

// Count is one workload in one transition: its pods at the start, how many of those are gone, how
// many pods were created, and the image digests those ran. QuickestStop is the shortest stop of a
// ready pod that was deleted. Zero means that none was timed, or that one was gone at the moment
// its deletion was first seen. Probed is yes, no or not known.
type Count struct {
	Workload                   Workload
	AtStart, Replaced, Created int
	Added, Removed             bool
	QuickestStop               time.Duration
	Probed                     string
	Digests                    []string
}

// Tracker follows the watched workloads of one namespace through the transitions of a run. It
// takes no lock: its caller holds one.
type Tracker struct {
	o         TrackerOptions
	workloads map[Workload]*workloadRecord
	// owners maps a ReplicaSet's name to the Deployment that is its controller. An entry stays
	// after the ReplicaSet is gone, so that its pods keep their workload.
	owners map[string]string
	// pods holds every pod by UID, never by name: a StatefulSet's pod that comes back under its
	// name is a new pod.
	pods map[types.UID]*podRecord
	// transitions holds every transition so far, in the order they began, so that PodsAt can
	// answer for a moment of any of them. The last is the open or last transition.
	transitions []*transition
	// missedOutside holds, in the order told, each stretch in which the watch may have missed
	// changes that ended while no window was open, since the last Begin: a watch down and up
	// again, or a moment that Missed was told. Begin carries into its transition the first moment
	// of them at or after its start.
	missedOutside []blindStretch
	// down holds, for each of the Watcher's watches of the cluster that is down, the moment it
	// went down.
	down map[string]time.Time
	// install holds what the Tracker keeps of the install from the first moment it found it
	// ready; nil before that.
	install *installTrack
}

// blindStretch is a stretch of time, from and until included, in which the watch of the cluster
// may have missed changes.
type blindStretch struct {
	from, until time.Time
}

// workloadRecord is what the Tracker keeps of one Deployment, StatefulSet or DaemonSet.
type workloadRecord struct {
	obj        runtime.Object
	uid        types.UID
	generation int64
	matched    bool // its labels matched the selector at its last Set; Begin seeds the watched set from it
	firstSeen  time.Time
	gone       time.Time // zero while it exists
}

// podRecord is what the Tracker keeps of one pod. The value of its deletion timestamp is never
// read: it is the deadline that the API server writes, not the moment of the deletion.
type podRecord struct {
	name                 string
	ownerKind, ownerName string // its controller owner; empty when it has none
	obj                  *corev1.Pod
	firstSeen            time.Time
	firstReady           time.Time
	deleting             time.Time // when a deletion timestamp was first seen
	readyWhenDeleting    bool      // whether the Set that first showed the deletion showed it ready
	gone                 time.Time
	// shown is false for a pod known only from its Gone: no Set showed it, so nothing says that
	// a transition created it or what it ran.
	shown bool
	// digests maps each image digest that a Set showed in its container statuses to the first
	// moment one did.
	digests map[string]time.Time
}

// transition is one transition of the run.
type transition struct {
	t      Transition
	exited time.Time // when its command exited; zero until then
	// finishes counts the times it came to be finished, also when a settle restart and a new
	// finish fall at one moment and leave Finished as it was.
	finishes int
	// atStart holds the pods that the transition may replace: those recorded at Begin whose
	// first sight is at or before its start and that were not gone by then.
	atStart map[types.UID]bool
	// existed holds the workloads that existed at its start: those recorded at Begin whose first
	// sight is at or before its start and that were not gone by then.
	existed map[Workload]bool
	// watched holds the workloads watched in the transition. Begin seeds it with those that
	// exist and match the selector then. While the window is open, a Set that shows a workload
	// matching adds it, and so does the Gone that takes a matching workload from existing to
	// gone. A workload stays in it for the rest of the transition, whatever its later labels.
	watched map[Workload]bool
	// unsupported holds the workloads watched at some moment of the transition whose object at
	// that moment did not update by rolling.
	unsupported map[Workload]bool
}

// NewTracker returns a Tracker with nothing in it. It does no I/O and reads no clock: it is told
// every change of an object and the time of each call.
func NewTracker(o TrackerOptions) *Tracker {
	return &Tracker{
		o:         o,
		workloads: map[Workload]*workloadRecord{},
		owners:    map[string]string{},
		pods:      map[types.UID]*podRecord{},
		down:      map[string]time.Time{},
	}
}

// Set tells the Tracker that a Deployment, StatefulSet, DaemonSet, ReplicaSet or Pod of the
// namespace was created or changed, and when it learned it.
func (t *Tracker) Set(obj runtime.Object, at time.Time) {
	t.endIfDue(at)
	var c change
	if w, meta, ok := workloadOf(obj); ok {
		c = t.setWorkload(w, obj, meta, at)
		t.noteUnsupported(w)
	} else {
		switch o := obj.(type) {
		case *appsv1.ReplicaSet:
			t.setReplicaSet(o)
		case *corev1.Pod:
			c = t.setPod(o, at)
		}
	}
	t.afterChange(c, at)
	t.noteInstall(at)
}

// Gone tells the Tracker that such an object no longer exists.
func (t *Tracker) Gone(obj runtime.Object, at time.Time) {
	t.endIfDue(at)
	var c change
	if w, meta, ok := workloadOf(obj); ok {
		c = t.goneWorkload(w, obj, meta, at)
		t.noteUnsupported(w)
	} else if p, ok := obj.(*corev1.Pod); ok {
		c = t.gonePod(p, at)
	}
	// A ReplicaSet's entry stays when it is gone, and any other type is ignored.
	t.afterChange(c, at)
	t.noteInstall(at)
}

// Begin starts a transition at the given moment. The pods that exist then are the ones it may
// replace. When the watch may have missed changes at or after the start, as told before Begin, or
// a watch is down when Begin is told, the first such moment, or the start, is the transition's
// Missed. One transition at a time.
func (t *Tracker) Begin(name verdict.WindowName, at time.Time) {
	t.endIfDue(at)
	if t.windowOpen() {
		panic(fmt.Sprintf("watch: Begin(%s) while the window of %s is open", name, t.current().t.Name))
	}
	atStart := map[types.UID]bool{}
	for uid, p := range t.pods {
		if !p.firstSeen.After(at) && (p.gone.IsZero() || p.gone.After(at)) {
			atStart[uid] = true
		}
	}
	watched := map[Workload]bool{}
	existed := map[Workload]bool{}
	for w, rec := range t.workloads {
		if !rec.gone.IsZero() && !rec.gone.After(at) {
			continue
		}
		if rec.matched {
			watched[w] = true
		}
		if !rec.firstSeen.After(at) {
			existed[w] = true
		}
	}
	var missed time.Time
	first := func(m time.Time) {
		if m = later(m, at); missed.IsZero() || m.Before(missed) {
			missed = m
		}
	}
	for _, b := range t.missedOutside {
		if !b.until.Before(at) { // the view was not whole again before the start
			first(b.from)
		}
	}
	for _, d := range t.down {
		first(d)
	}
	t.missedOutside = nil
	t.transitions = append(t.transitions, &transition{t: Transition{Name: name, Began: at, Missed: missed},
		atStart: atStart, existed: existed, watched: watched, unsupported: map[Workload]bool{}})
	for w := range watched {
		t.noteUnsupported(w)
	}
}

// CommandExited tells the Tracker that the transition's command has exited, however it ended.
func (t *Tracker) CommandExited(at time.Time) {
	t.endIfDue(at)
	if t.windowOpen() && t.current().exited.IsZero() {
		t.current().exited = at
	}
	t.afterChange(change{}, at)
}

// Missed tells the Tracker that the watch of the cluster may have missed changes at the given
// moment: it listed again, so a change between its last event and that list is seen only by its
// sum, or not at all. It records the moment whenever it is told. The first such moment in a
// transition's window is its Missed. Begin may be told after the transition's start, so a moment
// told while no window is open counts for the next transition when it is at or after that
// transition's start. An earlier moment counts for no transition, and a window that has ended
// never changes.
func (t *Tracker) Missed(at time.Time) {
	t.endIfDue(at)
	if t.windowOpen() {
		t.noteMissed(later(at, t.current().t.Began))
		return
	}
	t.keepOutside(blindStretch{at, at})
}

// watchDown tells the Tracker that the watch named source went down at the given moment: its
// stream ended, or brought an error. It is down until watchUp. A window in which a watch was down
// at any moment, from its start to its end, may have missed changes: its Missed is the first such
// moment, or its start.
func (t *Tracker) watchDown(source string, at time.Time) {
	t.endIfDue(at)
	if _, down := t.down[source]; down {
		return
	}
	t.down[source] = at
	if t.windowOpen() {
		t.noteMissed(later(at, t.current().t.Began))
	}
}

// watchUp tells the Tracker that the watch named source is up again: a watch call succeeded, or a
// list's events have been delivered. A stretch down that ends while no window is open counts for
// the next transition when it reaches that transition's start.
func (t *Tracker) watchUp(source string, at time.Time) {
	t.endIfDue(at)
	from, down := t.down[source]
	if !down {
		return
	}
	delete(t.down, source)
	if !t.windowOpen() {
		t.keepOutside(blindStretch{from, at})
	}
}

// keepOutside keeps a stretch that ended while no window was open, for the next Begin.
func (t *Tracker) keepOutside(b blindStretch) {
	if n := len(t.missedOutside); n == 0 || t.missedOutside[n-1] != b {
		// A list again tells one moment for each object it lists; equal stretches add nothing.
		t.missedOutside = append(t.missedOutside, b)
	}
}

// noteMissed makes m the open window's Missed when it is the first moment so far.
func (t *Tracker) noteMissed(m time.Time) {
	if tr := t.current(); tr.t.Missed.IsZero() || m.Before(tr.t.Missed) {
		tr.t.Missed = m
	}
}

func later(a, b time.Time) time.Time {
	if a.Before(b) {
		return b
	}
	return a
}

// current is the open or last transition, nil before the first Begin.
func (t *Tracker) current() *transition {
	if len(t.transitions) == 0 {
		return nil
	}
	return t.transitions[len(t.transitions)-1]
}

// windowOpen reports whether a transition has begun and its window has not ended.
func (t *Tracker) windowOpen() bool {
	tr := t.current()
	return tr != nil && tr.t.Ended.IsZero()
}

// openFinish gives the open transition's name and Finished, and how many times it has come to be
// finished. At is zero and the count is 0 when no window is open; At is zero when the open
// transition is not finished now.
func (t *Tracker) openFinish() (Finish, int) {
	if !t.windowOpen() {
		return Finish{}, 0
	}
	tr := t.current()
	return Finish{Name: tr.t.Name, At: tr.t.Finished}, tr.finishes
}

// State returns the transition so far, and whether its window has ended: it finished and the
// settle time then passed with no change, or it stalled. The Transition also has its counts for
// every watched workload, whether it replaced pods, what is not complete at a stall, which pods
// could not be scheduled, and which workloads update in a way the tool cannot judge. While the
// window is open, what is not complete and which pods could not be scheduled are taken at now,
// so that a caller that cuts the window short has them. Every list in it is a copy.
func (t *Tracker) State(now time.Time) (Transition, bool) {
	t.endIfDue(now)
	tr := t.current()
	if tr == nil {
		return Transition{}, false
	}
	s := tr.t
	if s.Ended.IsZero() {
		s = t.summary(tr, now)
		t.details(&s, now)
	}
	return s.copied(), !s.Ended.IsZero()
}

// stallObjects returns the objects that the last transition's stall names, each as kind/name with
// the kind in lower case: the workloads that take part and are not complete, or are deleted with
// pods left; the ReplicaSets that are the controllers of such a Deployment; and the pods of those
// workloads that are not gone and are not ready or are stopping. It is empty when the last
// transition did not stall.
func (t *Tracker) stallObjects() map[string]bool {
	tr := t.current()
	if tr == nil || !tr.t.Stalled {
		return nil
	}
	named := map[string]bool{}
	var stalled []Workload
	for _, w := range t.takingPart() {
		rec := t.workloads[w]
		if !rec.gone.IsZero() {
			if t.podsLeft(w) {
				named[w.String()] = true
				stalled = append(stalled, w)
			}
			continue
		}
		if done, _ := Complete(rec.obj); !done {
			named[w.String()] = true
			stalled = append(stalled, w)
		}
	}
	for rs, d := range t.owners {
		for _, w := range stalled {
			if w.Kind == "deployment" && w.Name == d {
				named["replicaset/"+rs] = true
				break
			}
		}
	}
	for _, p := range t.pods {
		if !p.gone.IsZero() {
			continue
		}
		w, ok := t.podWorkload(p)
		if !ok || !named[w.String()] {
			continue
		}
		if !podReady(p.obj) || !p.deleting.IsZero() {
			named["pod/"+p.name] = true
		}
	}
	return named
}

// endIfDue ends the open transition's window when its time has come by at: Settle after it
// finished, or, when it has not finished, Timeout after it began, as a stall. It runs before the
// change told at at is applied, so a change at the window's end or later belongs to no
// transition.
func (t *Tracker) endIfDue(at time.Time) {
	if !t.windowOpen() {
		return
	}
	tr := t.current()
	if !tr.t.Finished.IsZero() {
		if end := tr.t.Finished.Add(t.o.Settle); !at.Before(end) {
			t.endWindow(end, false)
		}
		return
	}
	if end := tr.t.Began.Add(t.o.Timeout); !at.Before(end) {
		t.endWindow(end, true)
	}
}

// endWindow ends the open transition's window at end, as a stall when stalled holds. It keeps the
// transition's counts and, for a stall, the details of what is not complete, from the records as
// they are now; later changes of the records cannot alter them. When endIfDue ends the window,
// the records hold every change told before end and none told at end, because the window is half
// open. A pod created or gone exactly at end is then left out, although "at or before the bound"
// would count it. That fails closed: it can only lower the counts, so no-pod-replaced is given
// more often, not less. When a change in the settle time stalls the window, the records include
// that change.
func (t *Tracker) endWindow(end time.Time, stalled bool) {
	tr := t.current()
	tr.t.Ended = end
	tr.t.Stalled = stalled
	tr.t = t.summary(tr, end)
	if stalled {
		t.details(&tr.t, end)
	}
}

// changeKind is a change that starts the settle time again when it concerns a workload that takes
// part in the finish: a pod created or deleted, a change of spec, a workload that joins the
// watched set, or a workload deleted. Nothing else starts it again: not readiness, not a
// restarted container, not a change of status.
type changeKind int

const (
	noChange changeKind = iota
	podCreated
	podDeleted
	specChanged
	workloadJoined
	workloadDeleted
)

// change is what one Set or Gone did that may start the settle time again.
type change struct {
	kind changeKind
	pod  string // the pod's name, for podCreated and podDeleted
	// workload is the zero Workload for a pod whose ReplicaSet is not known; no workload of that
	// name is ever recorded, so it never takes part.
	workload Workload
}

// sentence says what the change was. since is the time from the transition's start.
func (c change) sentence(since time.Duration) string {
	s := since.Seconds()
	switch c.kind {
	case podCreated:
		return fmt.Sprintf("pod %s of %s was created %.1f s after the transition began", c.pod, c.workload, s)
	case podDeleted:
		return fmt.Sprintf("pod %s of %s was deleted %.1f s after the transition began", c.pod, c.workload, s)
	case workloadJoined:
		return fmt.Sprintf("%s joined the watched workloads %.1f s after the transition began", c.workload, s)
	case workloadDeleted:
		return fmt.Sprintf("%s was deleted %.1f s after the transition began", c.workload, s)
	default:
		return fmt.Sprintf("the spec of %s changed %.1f s after the transition began", c.workload, s)
	}
}

// afterChange applies a change to the open transition: it starts the settle time again, or, when
// the transition is not finished, finishes it when all three conditions hold.
func (t *Tracker) afterChange(c change, at time.Time) {
	if !t.windowOpen() {
		return
	}
	tr := t.current()
	if !tr.t.Finished.IsZero() {
		if c.kind == noChange || !t.takesPart(c.workload) {
			return
		}
		tr.t.Finished = time.Time{}
		tr.t.SettleRestarts = append(tr.t.SettleRestarts, c.sentence(at.Sub(tr.t.Began)))
		if !at.Before(tr.t.Began.Add(t.o.Timeout)) {
			t.endWindow(at, true)
			return
		}
	}
	if t.finished() {
		tr.t.Finished = at
		tr.finishes++
	}
}

// finished reports whether the open transition meets all three conditions: its command has
// exited; each workload that takes part exists and is complete, or is gone and none of its pods is
// left; and no pod that takes part has been deleted and is not gone.
func (t *Tracker) finished() bool {
	tr := t.current()
	if tr.exited.IsZero() {
		return false
	}
	for w := range tr.watched {
		if !t.takesPart(w) {
			continue
		}
		if rec := t.workloads[w]; rec.gone.IsZero() {
			if done, _ := Complete(rec.obj); !done {
				return false
			}
		} else if t.podsLeft(w) {
			return false
		}
	}
	for _, p := range t.pods {
		if p.deleting.IsZero() || !p.gone.IsZero() {
			continue
		}
		if w, ok := t.podWorkload(p); ok && t.takesPart(w) {
			return false
		}
	}
	return true
}

// takesPart reports whether w takes part in the finish of the open transition: it is in the
// transition's watched set, and its last object updates by rolling.
func (t *Tracker) takesPart(w Workload) bool {
	return t.current().watched[w] && Rolling(t.workloads[w].obj)
}

// noteUnsupported notes w as a workload the tool cannot judge when, while a window is open, w is
// watched in the transition and its object does not update by rolling.
func (t *Tracker) noteUnsupported(w Workload) {
	if t.windowOpen() && t.current().watched[w] && !Rolling(t.workloads[w].obj) {
		t.current().unsupported[w] = true
	}
}

// podsLeft reports whether a pod of w is not gone.
func (t *Tracker) podsLeft(w Workload) bool {
	for _, p := range t.pods {
		if pw, ok := t.podWorkload(p); ok && pw == w && p.gone.IsZero() {
			return true
		}
	}
	return false
}

// podWorkload is the workload of a pod: through its ReplicaSet to a Deployment, or its StatefulSet
// or DaemonSet directly. A pod whose ReplicaSet is not known has none.
func (t *Tracker) podWorkload(p *podRecord) (Workload, bool) {
	switch p.ownerKind {
	case "ReplicaSet":
		if d, ok := t.owners[p.ownerName]; ok {
			return Workload{Kind: "deployment", Name: d}, true
		}
	case "StatefulSet":
		return Workload{Kind: "statefulset", Name: p.ownerName}, true
	case "DaemonSet":
		return Workload{Kind: "daemonset", Name: p.ownerName}, true
	}
	return Workload{}, false
}

// workloadOf returns the workload that obj is, and its metadata, when obj is a Deployment,
// StatefulSet or DaemonSet.
func workloadOf(obj runtime.Object) (Workload, *metav1.ObjectMeta, bool) {
	switch o := obj.(type) {
	case *appsv1.Deployment:
		return Workload{Kind: "deployment", Name: o.Name}, &o.ObjectMeta, true
	case *appsv1.StatefulSet:
		return Workload{Kind: "statefulset", Name: o.Name}, &o.ObjectMeta, true
	case *appsv1.DaemonSet:
		return Workload{Kind: "daemonset", Name: o.Name}, &o.ObjectMeta, true
	}
	return Workload{}, nil, false
}

// selects reports whether the selector selects a workload with these labels. A nil selector
// selects every workload.
func (t *Tracker) selects(meta *metav1.ObjectMeta) bool {
	return t.o.Selector == nil || t.o.Selector.Matches(labels.Set(meta.Labels))
}

// setWorkload records a Set of a workload. The Set is of a new object when the workload is seen
// for the first time, when its record is gone, or when its UID differs from the recorded one; its
// first sight is then this moment. Its spec changed when the object is new, whatever the
// generations are, or when its generation is higher than the recorded one: a workload created, or
// made again under its old name, in the settle time starts it again. While a window is open, a
// Set that shows the workload matching adds it to the watched set; one that adds it, and is not a
// change of spec, says that it joined.
func (t *Tracker) setWorkload(w Workload, obj runtime.Object, meta *metav1.ObjectMeta, at time.Time) change {
	rec, known := t.workloads[w]
	if !known {
		rec = &workloadRecord{}
		t.workloads[w] = rec
	}
	newObject := !known || !rec.gone.IsZero() || meta.UID != rec.uid
	if newObject {
		rec.firstSeen = at
	}
	specChange := newObject || meta.Generation > rec.generation
	rec.obj = obj
	rec.uid = meta.UID
	rec.generation = meta.Generation
	rec.matched = t.selects(meta)
	rec.gone = time.Time{}
	joined := t.watch(w, rec.matched)
	switch {
	case specChange:
		return change{kind: specChanged, workload: w}
	case joined:
		return change{kind: workloadJoined, workload: w}
	}
	return change{}
}

// goneWorkload records that a workload no longer exists. A workload not told before is recorded
// as seen and gone at once. A workload that goes from existing to gone was deleted at this
// moment; while a window is open and this Gone shows it matching, it joins the watched set, so
// that its pods are waited for. A Gone of a workload that is already gone changes nothing and
// adds nothing to the watched set.
func (t *Tracker) goneWorkload(w Workload, obj runtime.Object, meta *metav1.ObjectMeta, at time.Time) change {
	rec, known := t.workloads[w]
	if !known {
		rec = &workloadRecord{obj: obj, uid: meta.UID, generation: meta.Generation, matched: t.selects(meta), firstSeen: at}
		t.workloads[w] = rec
	}
	if !rec.gone.IsZero() {
		return change{}
	}
	rec.gone = at
	t.watch(w, t.selects(meta))
	return change{kind: workloadDeleted, workload: w}
}

// watch adds w to the open transition's watched set when matches holds, and reports whether it
// was not in the set before.
func (t *Tracker) watch(w Workload, matches bool) bool {
	if !matches || !t.windowOpen() || t.current().watched[w] {
		return false
	}
	t.current().watched[w] = true
	return true
}

// setReplicaSet records the Deployment that is a ReplicaSet's controller, or that it has none.
func (t *Tracker) setReplicaSet(rs *appsv1.ReplicaSet) {
	if ref := metav1.GetControllerOf(rs); ref != nil && ref.Kind == "Deployment" {
		t.owners[rs.Name] = ref.Name
		return
	}
	delete(t.owners, rs.Name)
}

// setPod records a Set of a pod. A pod that is gone stays gone.
func (t *Tracker) setPod(p *corev1.Pod, at time.Time) change {
	rec, known := t.pods[p.UID]
	if known && !rec.gone.IsZero() {
		return change{}
	}
	if !known {
		rec = &podRecord{firstSeen: at, shown: true}
		t.pods[p.UID] = rec
	}
	rec.name = p.Name
	rec.ownerKind, rec.ownerName = controllerOf(p)
	rec.obj = p
	rec.noteDigests(p, at)
	ready := podReady(p)
	if ready && rec.firstReady.IsZero() {
		rec.firstReady = at
	}
	deleted := false
	if p.DeletionTimestamp != nil && rec.deleting.IsZero() {
		rec.deleting = at
		rec.readyWhenDeleting = ready
		deleted = true
	}
	switch {
	case !known:
		return t.podChange(podCreated, rec)
	case deleted:
		return t.podChange(podDeleted, rec)
	}
	return change{}
}

// gonePod records that a pod no longer exists. A pod not seen before is recorded as seen and gone
// at once. The pod was deleted at this moment when no deletion timestamp was seen before it.
func (t *Tracker) gonePod(p *corev1.Pod, at time.Time) change {
	rec, known := t.pods[p.UID]
	if !known {
		rec = &podRecord{name: p.Name, obj: p, firstSeen: at}
		rec.ownerKind, rec.ownerName = controllerOf(p)
		t.pods[p.UID] = rec
	}
	if !rec.gone.IsZero() {
		return change{}
	}
	rec.gone = at
	if rec.deleting.IsZero() {
		return t.podChange(podDeleted, rec)
	}
	return change{}
}

// podChange is a change of kind k to the pod rec, with the pod's workload.
func (t *Tracker) podChange(k changeKind, rec *podRecord) change {
	w, _ := t.podWorkload(rec)
	return change{kind: k, pod: rec.name, workload: w}
}

// controllerOf returns the kind and name of a pod's controller owner, or two empty strings.
func controllerOf(p *corev1.Pod) (string, string) {
	if ref := metav1.GetControllerOf(p); ref != nil {
		return ref.Kind, ref.Name
	}
	return "", ""
}

// podReady reports whether a pod's condition Ready is True.
func podReady(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// podEvents gives a PodEvent for each moment of each pod record: created at its first Set, ready
// at its first ready Set, deleted at the first Set that showed a deletion timestamp, or at its
// Gone when none did, and gone. A pod known only from its Gone has no Set, so it has a gone alone.
// The order is that of PodEvents.
func (t *Tracker) podEvents() []PodEvent {
	var events []PodEvent
	for _, p := range t.pods {
		var workload string
		if w, ok := t.podWorkload(p); ok {
			workload = w.String()
		}
		add := func(at time.Time, kind string) {
			if !at.IsZero() {
				events = append(events, PodEvent{At: at, Pod: p.name, Workload: workload, Kind: kind})
			}
		}
		if p.shown {
			add(p.firstSeen, "created")
			add(p.firstReady, "ready")
			if p.deleting.IsZero() {
				add(p.gone, "deleted")
			} else {
				add(p.deleting, "deleted")
			}
		}
		add(p.gone, "gone")
	}
	order := []string{"created", "ready", "deleted", "gone"}
	slices.SortFunc(events, func(a, b PodEvent) int {
		return cmp.Or(a.At.Compare(b.At), cmp.Compare(a.Pod, b.Pod),
			cmp.Compare(slices.Index(order, a.Kind), slices.Index(order, b.Kind)))
	})
	return events
}
