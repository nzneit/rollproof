// Package watch tells what happens to the watched workloads of one namespace: when an install is
// ready, when a transition has finished or stalled, and which pods were replaced. It hides the
// rollout rules of each kind, pod ownership and the settle time. It changes nothing in the
// cluster.
package watch

import (
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
// zero for a stall. Ended is when its window ended. The string lists are sentences for the report:
// what started the settle time again, what is not complete, which pods could not be scheduled.
type Transition struct {
	Name                                      verdict.WindowName
	Began, Finished, Ended                    time.Time
	Stalled                                   bool
	SettleRestarts, Incomplete, Unschedulable []string
	Unsupported                               []Workload
	Workloads                                 []Count
	Replaced                                  bool
}

// Count is one workload in one transition: its pods at the start, how many of those are gone, how
// many pods were created, and the image digests those ran. QuickestStop is zero when no ready pod
// was deleted. Probed is yes, no or not known.
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
	cur  *transition // nil before the first Begin
}

// workloadRecord is what the Tracker keeps of one Deployment, StatefulSet or DaemonSet.
type workloadRecord struct {
	obj        runtime.Object
	uid        types.UID
	generation int64
	matched    bool // its labels matched the selector at its last Set
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
}

// transition is the open or last transition.
type transition struct {
	t      Transition
	exited time.Time // when its command exited; zero until then
	// atStart holds the pods that the transition may replace: those recorded at Begin whose
	// first sight is at or before its start and that were not gone by then.
	atStart map[types.UID]bool
}

// NewTracker returns a Tracker with nothing in it. It does no I/O and reads no clock: it is told
// every change of an object and the time of each call.
func NewTracker(o TrackerOptions) *Tracker {
	return &Tracker{
		o:         o,
		workloads: map[Workload]*workloadRecord{},
		owners:    map[string]string{},
		pods:      map[types.UID]*podRecord{},
	}
}

// Set tells the Tracker that a Deployment, StatefulSet, DaemonSet, ReplicaSet or Pod of the
// namespace was created or changed, and when it learned it.
func (t *Tracker) Set(obj runtime.Object, at time.Time) {
	t.endIfDue(at)
	var c change
	if w, meta, ok := workloadOf(obj); ok {
		c = t.setWorkload(w, obj, meta, at)
	} else {
		switch o := obj.(type) {
		case *appsv1.ReplicaSet:
			t.setReplicaSet(o)
		case *corev1.Pod:
			c = t.setPod(o, at)
		}
	}
	t.afterChange(c, at)
}

// Gone tells the Tracker that such an object no longer exists.
func (t *Tracker) Gone(obj runtime.Object, at time.Time) {
	t.endIfDue(at)
	var c change
	if w, meta, ok := workloadOf(obj); ok {
		t.goneWorkload(w, obj, meta, at)
	} else if p, ok := obj.(*corev1.Pod); ok {
		c = t.gonePod(p, at)
	}
	// A ReplicaSet's entry stays when it is gone, and any other type is ignored.
	t.afterChange(c, at)
}

// Begin starts a transition at the given moment. The pods that exist then are the ones it may
// replace. One transition at a time.
func (t *Tracker) Begin(name verdict.WindowName, at time.Time) {
	t.endIfDue(at)
	if t.cur != nil && t.cur.t.Ended.IsZero() {
		panic(fmt.Sprintf("watch: Begin(%s) while the window of %s is open", name, t.cur.t.Name))
	}
	atStart := map[types.UID]bool{}
	for uid, p := range t.pods {
		if !p.firstSeen.After(at) && (p.gone.IsZero() || p.gone.After(at)) {
			atStart[uid] = true
		}
	}
	t.cur = &transition{t: Transition{Name: name, Began: at}, atStart: atStart}
}

// CommandExited tells the Tracker that the transition's command has exited, however it ended.
func (t *Tracker) CommandExited(at time.Time) {
	t.endIfDue(at)
	if t.cur != nil && t.cur.t.Ended.IsZero() && t.cur.exited.IsZero() {
		t.cur.exited = at
	}
	t.afterChange(change{}, at)
}

// State returns the transition so far, and whether its window has ended: it finished and the
// settle time then passed with no change, or it stalled.
func (t *Tracker) State(now time.Time) (Transition, bool) {
	t.endIfDue(now)
	if t.cur == nil {
		return Transition{}, false
	}
	s := t.cur.t
	s.SettleRestarts = slices.Clone(s.SettleRestarts)
	return s, !s.Ended.IsZero()
}

// endIfDue ends the open transition's window when its time has come by at: Settle after it
// finished, or, when it has not finished, Timeout after it began, as a stall.
func (t *Tracker) endIfDue(at time.Time) {
	tr := t.cur
	if tr == nil || !tr.t.Ended.IsZero() {
		return
	}
	if !tr.t.Finished.IsZero() {
		if end := tr.t.Finished.Add(t.o.Settle); !at.Before(end) {
			tr.t.Ended = end
		}
		return
	}
	if end := tr.t.Began.Add(t.o.Timeout); !at.Before(end) {
		tr.t.Ended = end
		tr.t.Stalled = true
	}
}

// changeKind is a change that starts the settle time again when it concerns a workload that takes
// part in the finish.
type changeKind int

const (
	noChange changeKind = iota
	podCreated
	podDeleted
	specChanged
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
	default:
		return fmt.Sprintf("the spec of %s changed %.1f s after the transition began", c.workload, s)
	}
}

// afterChange applies a change to the open transition: it starts the settle time again, or, when
// the transition is not finished, finishes it when all three conditions hold.
func (t *Tracker) afterChange(c change, at time.Time) {
	tr := t.cur
	if tr == nil || !tr.t.Ended.IsZero() {
		return
	}
	if !tr.t.Finished.IsZero() {
		if c.kind == noChange || !t.takesPart(c.workload) {
			return
		}
		tr.t.Finished = time.Time{}
		tr.t.SettleRestarts = append(tr.t.SettleRestarts, c.sentence(at.Sub(tr.t.Began)))
		if !at.Before(tr.t.Began.Add(t.o.Timeout)) {
			tr.t.Stalled = true
			tr.t.Ended = at
			return
		}
	}
	if t.finished() {
		tr.t.Finished = at
	}
}

// finished reports whether the open transition meets all three conditions: its command has
// exited; each workload that takes part exists and is complete, or is gone and none of its pods is
// left; and no pod that takes part has been deleted and is not gone.
func (t *Tracker) finished() bool {
	if t.cur.exited.IsZero() {
		return false
	}
	for w, rec := range t.workloads {
		if !t.takesPart(w) {
			continue
		}
		if rec.gone.IsZero() {
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

// takesPart reports whether w takes part in the finish of the open transition: it is watched in
// it, and its last object updates by rolling. A workload is watched when it existed at the
// transition's start or appeared later, and its labels matched at its last Set.
func (t *Tracker) takesPart(w Workload) bool {
	rec, ok := t.workloads[w]
	if !ok || !rec.matched {
		return false
	}
	if !rec.gone.IsZero() && !rec.gone.After(t.cur.t.Began) {
		return false
	}
	return Rolling(rec.obj)
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

// setWorkload records a Set of a workload. Its spec changed when it is seen for the first time,
// when its record is gone, when its UID differs from the recorded one, or when its generation is
// higher than the recorded one. The first three hold whatever the generations are: a workload
// created, or made again under its old name, in the settle time starts it again.
func (t *Tracker) setWorkload(w Workload, obj runtime.Object, meta *metav1.ObjectMeta, at time.Time) change {
	rec, known := t.workloads[w]
	if !known {
		rec = &workloadRecord{firstSeen: at}
		t.workloads[w] = rec
	}
	specChange := !known || !rec.gone.IsZero() || meta.UID != rec.uid || meta.Generation > rec.generation
	rec.obj = obj
	rec.uid = meta.UID
	rec.generation = meta.Generation
	rec.matched = t.selects(meta)
	rec.gone = time.Time{}
	if specChange {
		return change{kind: specChanged, workload: w}
	}
	return change{}
}

// goneWorkload records that a workload no longer exists.
func (t *Tracker) goneWorkload(w Workload, obj runtime.Object, meta *metav1.ObjectMeta, at time.Time) {
	rec, known := t.workloads[w]
	if !known {
		rec = &workloadRecord{obj: obj, uid: meta.UID, generation: meta.Generation, matched: t.selects(meta), firstSeen: at}
		t.workloads[w] = rec
	}
	if rec.gone.IsZero() {
		rec.gone = at
	}
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
		rec = &podRecord{firstSeen: at}
		t.pods[p.UID] = rec
	}
	rec.name = p.Name
	rec.ownerKind, rec.ownerName = controllerOf(p)
	rec.obj = p
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
