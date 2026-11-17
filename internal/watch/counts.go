package watch

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"rollproof/internal/verdict"
)

// Conditions returns what the transition gives the verdict: a stall, with a second condition when
// a pod could not be scheduled; a workload the tool cannot judge; and, only when it finished and
// its command succeeded, that it replaced no pod. A Transition whose window has not ended, or
// that ended without finishing (read while open, or cut short by its caller), is judged as a
// stall: it gives TransitionStalled, PodUnschedulable when a pod could not be scheduled, and never
// NoPodReplaced, so that such a Transition never passes.
func (tr Transition) Conditions(commandSucceeded bool) []verdict.Condition {
	stalled := tr.Stalled || tr.Finished.IsZero() || tr.Ended.IsZero()
	var cs []verdict.Condition
	if stalled {
		text := "did not finish within limits.timeout"
		if len(tr.SettleRestarts) > 0 {
			text += "; the settle time started again: " + strings.Join(tr.SettleRestarts, "; ")
		}
		cs = append(cs, verdict.Condition{Code: verdict.TransitionStalled, Text: text})
		if len(tr.Unschedulable) > 0 {
			cs = append(cs, verdict.Condition{Code: verdict.PodUnschedulable,
				Text: strings.Join(tr.Unschedulable, "; ") + "; a rolling update needs room for one more pod"})
		}
	}
	if len(tr.Unsupported) > 0 {
		// A Transition holds no object. The API server admits only RollingUpdate and OnDelete as a
		// StatefulSet's or a DaemonSet's spec.updateStrategy.type, and writes RollingUpdate for an
		// empty one, so a workload that does not update by rolling updates with OnDelete.
		cs = append(cs, unsupported(tr.Unsupported, func(Workload) string {
			return string(appsv1.OnDeleteStatefulSetStrategyType)
		}))
	}
	if !stalled && commandSucceeded && !tr.Replaced {
		cs = append(cs, verdict.Condition{Code: verdict.NoPodReplaced,
			Text: "the transition finished and replaced no pod: it changed nothing the test can judge"})
	}
	return cs
}

// unsupported is the condition for workloads the tool cannot judge, each with the update strategy
// type that updateType gives for it.
func unsupported(ws []Workload, updateType func(Workload) string) verdict.Condition {
	parts := make([]string, len(ws))
	for i, w := range ws {
		parts[i] = w.String() + " updates with " + updateType(w)
	}
	return verdict.Condition{Code: verdict.WorkloadUnsupported,
		Text: strings.Join(parts, "; ") + "; the tool judges only a workload that updates by rolling"}
}

// copied returns tr with every list copied, so that a caller that changes one changes nothing in
// the Tracker.
func (tr Transition) copied() Transition {
	tr.SettleRestarts = slices.Clone(tr.SettleRestarts)
	tr.Incomplete = slices.Clone(tr.Incomplete)
	tr.Unschedulable = slices.Clone(tr.Unschedulable)
	tr.Unsupported = slices.Clone(tr.Unsupported)
	tr.Workloads = slices.Clone(tr.Workloads)
	for i := range tr.Workloads {
		tr.Workloads[i].Digests = slices.Clone(tr.Workloads[i].Digests)
	}
	return tr
}

// summary returns the Transition of tr with its counts at the bound b, whether it replaced pods,
// and the workloads it watched that do not update by rolling. b is the window's end when it has
// ended, and the moment asked about while it is open.
func (t *Tracker) summary(tr *transition, b time.Time) Transition {
	s := tr.t
	s.Workloads = t.counts(tr, b)
	replaced, created := false, false
	for _, c := range s.Workloads {
		replaced = replaced || c.Replaced > 0
		created = created || c.Created > 0
	}
	s.Replaced = replaced && created
	s.Unsupported = sortedWorkloads(tr.unsupported)
	return s
}

// counts returns one Count for each workload watched in tr, sorted by kind and then name, at the
// bound b. AtStart counts the pods it may replace, and Replaced those of them gone by b. Created
// counts its other pods that a Set showed, first seen from the transition's start to b, and
// Digests are what those ran. QuickestStop is the shortest time from the first sight of a
// deletion to gone, over its pods whose deletion was first seen from the start on by a Set that
// showed them ready, and that were gone by b.
func (t *Tracker) counts(tr *transition, b time.Time) []Count {
	ws := sortedWorkloads(tr.watched)
	index := make(map[Workload]int, len(ws))
	cs := make([]Count, len(ws))
	for i, w := range ws {
		index[w] = i
		rec := t.workloads[w]
		cs[i] = Count{Workload: w, Added: !tr.existed[w],
			Removed: tr.existed[w] && !rec.gone.IsZero() && !rec.gone.After(b)}
	}
	began := tr.t.Began
	digests := make([]map[string]bool, len(ws))
	stopped := make([]bool, len(ws)) // whether cs[i].QuickestStop holds a pod's time
	for uid, p := range t.pods {
		w, ok := t.podWorkload(p)
		i, watched := index[w]
		if !ok || !watched {
			continue
		}
		c := &cs[i]
		goneByB := !p.gone.IsZero() && !p.gone.After(b)
		switch {
		case tr.atStart[uid]:
			c.AtStart++
			if goneByB {
				c.Replaced++
			}
		case p.shown && !p.firstSeen.Before(began) && !p.firstSeen.After(b):
			c.Created++
			for d, seen := range p.digests {
				if !seen.After(b) {
					if digests[i] == nil {
						digests[i] = map[string]bool{}
					}
					digests[i][d] = true
				}
			}
		}
		if p.readyWhenDeleting && !p.deleting.Before(began) && goneByB {
			if d := p.gone.Sub(p.deleting); !stopped[i] || d < c.QuickestStop {
				c.QuickestStop, stopped[i] = d, true
			}
		}
	}
	for i := range cs {
		cs[i].Digests = sortedKeys(digests[i])
	}
	return cs
}

// takingPart returns the workloads that take part in the finish of the open transition, sorted by
// kind and then name.
func (t *Tracker) takingPart() []Workload {
	var ws []Workload
	for _, w := range sortedWorkloads(t.current().watched) {
		if t.takesPart(w) {
			ws = append(ws, w)
		}
	}
	return ws
}

// details fills s's Incomplete and Unschedulable from the records as they are now, for the
// workloads that take part in the open transition, with the stopping times counted to at.
func (t *Tracker) details(s *Transition, at time.Time) {
	part := t.takingPart()
	s.Incomplete = t.incomplete(part, at)
	s.Unschedulable = t.unschedulable(part)
}

// incomplete returns, at the moment at, a sentence for each thing that keeps the workloads ws from
// being complete, ws being those that take part, in their order. A workload that exists and is
// not complete gives kubectl's message, then a sentence for each of its pods that is not gone and
// not ready; one that is gone gives the number of its pods that are not gone. Then each of their
// pods that is stopping and not gone gives how long it has been stopping.
func (t *Tracker) incomplete(ws []Workload, at time.Time) []string {
	part := make(map[Workload]bool, len(ws))
	for _, w := range ws {
		part[w] = true
	}
	pods := t.sortedPods()
	var out []string
	for _, w := range ws {
		var mine []*podRecord
		for _, p := range pods {
			if pw, ok := t.podWorkload(p); ok && pw == w && p.gone.IsZero() {
				mine = append(mine, p)
			}
		}
		rec := t.workloads[w]
		if !rec.gone.IsZero() {
			if len(mine) > 0 {
				out = append(out, fmt.Sprintf("%s was deleted and %d of its pods are not gone", w, len(mine)))
			}
			continue
		}
		done, msg := Complete(rec.obj)
		if done {
			continue
		}
		out = append(out, w.String()+": "+msg)
		for _, p := range mine {
			if !podReady(p.obj) {
				out = append(out, podSentence(p.obj))
			}
		}
	}
	for _, p := range pods {
		if w, ok := t.podWorkload(p); ok && part[w] && !p.deleting.IsZero() && p.gone.IsZero() {
			out = append(out, fmt.Sprintf("pod %s has been stopping for %.1f s and is not gone",
				p.name, at.Sub(p.deleting).Seconds()))
		}
	}
	return out
}

// podSentence says a pod's phase, why it is not scheduled, and the state of each of its
// containers and how each last ended.
func podSentence(p *corev1.Pod) string {
	var b strings.Builder
	fmt.Fprintf(&b, "pod %s: phase %s", p.Name, p.Status.Phase)
	if c, ok := scheduled(p); ok && c.Status == corev1.ConditionFalse {
		fmt.Fprintf(&b, ", not scheduled (%s: %s)", c.Reason, c.Message)
	}
	for _, s := range p.Status.ContainerStatuses {
		fmt.Fprintf(&b, ", container %s %s", s.Name, containerState(s.State))
		if last := s.LastTerminationState.Terminated; last != nil {
			fmt.Fprintf(&b, " and last ended (%s, exit code %d)", last.Reason, last.ExitCode)
		}
	}
	return b.String()
}

// containerState says a container's state. A state with none of its three parts set is waiting,
// as the API defines.
func containerState(s corev1.ContainerState) string {
	switch {
	case s.Running != nil:
		return "running"
	case s.Terminated != nil:
		return fmt.Sprintf("terminated (%s, exit code %d)", s.Terminated.Reason, s.Terminated.ExitCode)
	case s.Waiting != nil:
		return fmt.Sprintf("waiting (%s)", s.Waiting.Reason)
	}
	return "waiting ()"
}

// scheduled returns a pod's condition PodScheduled, and whether it has one.
func scheduled(p *corev1.Pod) (corev1.PodCondition, bool) {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodScheduled {
			return c, true
		}
	}
	return corev1.PodCondition{}, false
}

// unschedulable returns a sentence for each pod of the workloads ws that is not gone and that the
// scheduler found no node for, by name. The sentence names the number of worker nodes, which the
// Transition does not hold.
func (t *Tracker) unschedulable(ws []Workload) []string {
	var out []string
	for _, p := range t.sortedPods() {
		w, ok := t.podWorkload(p)
		if !ok || !slices.Contains(ws, w) || !p.gone.IsZero() {
			continue
		}
		if c, ok := scheduled(p.obj); ok && c.Status == corev1.ConditionFalse && c.Reason == corev1.PodReasonUnschedulable {
			out = append(out, fmt.Sprintf("pod %s could not be scheduled on the test cluster's %d worker nodes (cluster.workers): %s",
				p.name, t.o.Workers, c.Message))
		}
	}
	return out
}

// Install is what the install left: every watched workload with its pods and the image digests
// they run.
type Install struct {
	Workloads []Count
}

// InstallState reports whether the install is ready at now. exited is when its command exited,
// zero while it runs. The error is a Condition: nothing to watch one settle time after the
// command exited; a workload the tool cannot judge; or not ready at began plus the timeout.
func (t *Tracker) InstallState(began, exited, now time.Time) (Install, bool, error) {
	// The install watches the workloads that exist and whose labels matched at their last Set;
	// those that update by rolling take part.
	set := map[Workload]bool{}
	for w, rec := range t.workloads {
		if rec.gone.IsZero() && rec.matched {
			set[w] = true
		}
	}
	watched := sortedWorkloads(set)
	var part, notRolling []Workload
	for _, w := range watched {
		if Rolling(t.workloads[w].obj) {
			part = append(part, w)
		} else {
			notRolling = append(notRolling, w)
		}
	}
	switch {
	case !exited.IsZero() && len(notRolling) > 0:
		return Install{}, false, unsupported(notRolling, t.updateType)
	case !exited.IsZero() && len(watched) > 0 && t.installReady(part):
		inst := Install{Workloads: make([]Count, len(watched))}
		for i, w := range watched {
			inst.Workloads[i] = Count{Workload: w, Digests: t.runningDigests(w)}
		}
		return inst, true, nil
	case !exited.IsZero() && len(watched) == 0 && !now.Before(exited.Add(t.o.Settle)):
		return Install{}, false, t.nothingWatched()
	case !now.Before(began.Add(t.o.Timeout)):
		text := "the install was not ready within limits.timeout"
		for _, s := range t.incomplete(part, now) {
			text += "\n" + s
		}
		return Install{}, false, verdict.Condition{Code: verdict.InstallFailed, Text: text}
	}
	return Install{}, false, nil
}

// installReady reports whether each of the workloads ws is complete and none of their pods has
// been deleted and is not gone.
func (t *Tracker) installReady(ws []Workload) bool {
	for _, w := range ws {
		if done, _ := Complete(t.workloads[w].obj); !done {
			return false
		}
	}
	for _, p := range t.pods {
		if w, ok := t.podWorkload(p); ok && slices.Contains(ws, w) && !p.deleting.IsZero() && p.gone.IsZero() {
			return false
		}
	}
	return true
}

// runningDigests returns the image digests that any Set showed for the pods of w that are not
// gone, sorted.
func (t *Tracker) runningDigests(w Workload) []string {
	set := map[string]bool{}
	for _, p := range t.pods {
		if pw, ok := t.podWorkload(p); ok && pw == w && p.gone.IsZero() {
			for d := range p.digests {
				set[d] = true
			}
		}
	}
	return sortedKeys(set)
}

// updateType returns the spec.updateStrategy.type of w's last object; empty for a Deployment.
func (t *Tracker) updateType(w Workload) string {
	switch o := t.workloads[w].obj.(type) {
	case *appsv1.StatefulSet:
		return string(o.Spec.UpdateStrategy.Type)
	case *appsv1.DaemonSet:
		return string(o.Spec.UpdateStrategy.Type)
	}
	return ""
}

// nothingWatched is the condition for an install that left nothing to watch. It names the
// namespace, the selector unless it selects everything, and the three kinds.
func (t *Tracker) nothingWatched() verdict.Condition {
	text := "nothing to watch\n" + strconv.FormatFloat(t.o.Settle.Seconds(), 'f', -1, 64) +
		" s after the install command returned, namespace " + t.o.Namespace +
		" held no Deployment, StatefulSet or DaemonSet"
	if t.o.Selector != nil && !t.o.Selector.Empty() {
		text += " that matches the selector " + t.o.Selector.String()
	}
	return verdict.Condition{Code: verdict.NothingWatched, Text: text}
}

// PodNote is a pod that was stopping or starting at some moment. Old says it existed when the
// transition that holds that moment began. For is how long it had been stopping, or starting, at
// that moment.
type PodNote struct {
	Name     string
	Workload Workload
	Old      bool
	For      time.Duration
}

// PodsAt returns the watched pods that, at the given moment, were deleted and not yet gone, and
// those that were created and not yet ready. The moment may lie in any transition so far.
func (t *Tracker) PodsAt(at time.Time) (stopping, starting []PodNote) {
	tr := t.holding(at)
	if tr == nil {
		return nil, nil
	}
	began := tr.t.Began
	for _, p := range t.sortedPods() {
		w, ok := t.podWorkload(p)
		if !ok || !tr.watched[w] || p.firstSeen.After(at) || goneBy(p, at) {
			continue
		}
		old := !p.firstSeen.After(began) && !goneBy(p, began)
		switch {
		case !p.deleting.IsZero() && !p.deleting.After(at):
			stopping = append(stopping, PodNote{Name: p.name, Workload: w, Old: old, For: at.Sub(p.deleting)})
		case !old && (p.firstReady.IsZero() || p.firstReady.After(at)):
			starting = append(starting, PodNote{Name: p.name, Workload: w, For: at.Sub(p.firstSeen)})
		}
	}
	return stopping, starting
}

// goneBy reports whether the pod p was gone at or before the moment m.
func goneBy(p *podRecord, m time.Time) bool {
	return !p.gone.IsZero() && !p.gone.After(m)
}

// holding returns the transition whose window holds the moment at: it began at or before at, and
// at is before the end of its window. The last window, while it has not been ended, ends when its
// time comes: Settle after it finished, or Timeout after it began. holding returns nil when no
// window holds at.
func (t *Tracker) holding(at time.Time) *transition {
	for _, tr := range t.transitions {
		end := tr.t.Ended
		if end.IsZero() {
			end = tr.t.Began.Add(t.o.Timeout)
			if !tr.t.Finished.IsZero() {
				end = tr.t.Finished.Add(t.o.Settle)
			}
		}
		if !tr.t.Began.After(at) && at.Before(end) {
			return tr
		}
	}
	return nil
}

// noteDigests notes each image digest that p's container statuses show, from "sha256:" on, with
// the first moment a Set showed it. An image ID without "sha256:" is skipped.
func (p *podRecord) noteDigests(pod *corev1.Pod, at time.Time) {
	for _, s := range pod.Status.ContainerStatuses {
		i := strings.Index(s.ImageID, "sha256:")
		if i < 0 {
			continue
		}
		if p.digests == nil {
			p.digests = map[string]time.Time{}
		}
		if _, seen := p.digests[s.ImageID[i:]]; !seen {
			p.digests[s.ImageID[i:]] = at
		}
	}
}

// sortedPods returns every pod, by name, then by first sight, then by UID: a StatefulSet's pod
// back under its name comes after the pod it replaced.
func (t *Tracker) sortedPods() []*podRecord {
	pods := make([]*podRecord, 0, len(t.pods))
	for _, p := range t.pods {
		pods = append(pods, p)
	}
	slices.SortFunc(pods, func(a, b *podRecord) int {
		return cmp.Or(cmp.Compare(a.name, b.name), a.firstSeen.Compare(b.firstSeen),
			cmp.Compare(a.uid(), b.uid()))
	})
	return pods
}

// uid is the pod's UID, from its last object.
func (p *podRecord) uid() types.UID {
	return p.obj.UID
}

// sortedWorkloads returns the workloads of a set, sorted by kind and then name.
func sortedWorkloads(set map[Workload]bool) []Workload {
	ws := make([]Workload, 0, len(set))
	for w := range set {
		ws = append(ws, w)
	}
	slices.SortFunc(ws, func(a, b Workload) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Name, b.Name))
	})
	return ws
}

// sortedKeys returns the keys of a set, sorted; nil for an empty set.
func sortedKeys(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
