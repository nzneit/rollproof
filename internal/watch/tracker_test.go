package watch

import (
	"math"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/nzneit/rollproof/internal/verdict"
)

// at is t0 plus s seconds.
func at(s float64) time.Time {
	return t0.Add(time.Duration(math.Round(s * float64(time.Second))))
}

// options are the Tracker's settings unless a test changes them.
func options() TrackerOptions {
	return TrackerOptions{Namespace: "orders", Selector: labels.Everything(),
		Settle: 15 * time.Second, Timeout: 300 * time.Second, Workers: 3}
}

// ev is one thing told to the Tracker: Set, Gone, or, with no object, CommandExited.
type ev struct {
	s    float64
	obj  runtime.Object
	gone bool
}

func set(s float64, obj runtime.Object) ev  { return ev{s: s, obj: obj} }
func gone(s float64, obj runtime.Object) ev { return ev{s: s, obj: obj, gone: true} }
func exited(s float64) ev                   { return ev{s: s} }

// base is the release before a deploy: Deployment orders, complete, with two
// ready pods of its ReplicaSet orders-a.
func base() []runtime.Object {
	return []runtime.Object{
		deployment("orders", 1, 1, 2, 2, 2, 2),
		replicaSet("orders-a", "orders"),
		pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, false),
		pod("orders-a-2", "a2", "ReplicaSet", "orders-a", true, false),
	}
}

// feed tells a new Tracker every object of before at t0, begins a deploy at
// t0, and then tells it the events in order.
func feed(o TrackerOptions, before []runtime.Object, events []ev) *Tracker {
	tr := NewTracker(o)
	for _, obj := range before {
		tr.Set(obj, t0)
	}
	tr.Begin(verdict.Deploy, t0)
	for _, e := range events {
		switch {
		case e.obj == nil:
			tr.CommandExited(at(e.s))
		case e.gone:
			tr.Gone(e.obj, at(e.s))
		default:
			tr.Set(e.obj, at(e.s))
		}
	}
	return tr
}

// renamed replaces Deployment orders with orders-v2 after base. The old
// ReplicaSet is gone before the old pods are.
func renamed() []ev {
	a1 := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, true)
	a2 := pod("orders-a-2", "a2", "ReplicaSet", "orders-a", true, true)
	return []ev{
		set(1, deployment("orders-v2", 1, 0, 2, 0, 0, 0)),
		set(1, replicaSet("orders-v2-a", "orders-v2")),
		exited(2),
		gone(3, deployment("orders", 1, 1, 2, 2, 2, 2)),
		set(4, a1),
		set(4, a2),
		set(5, pod("orders-v2-a-1", "c1", "ReplicaSet", "orders-v2-a", true, false)),
		set(5, pod("orders-v2-a-2", "c2", "ReplicaSet", "orders-v2-a", true, false)),
		set(9, deployment("orders-v2", 1, 1, 2, 2, 2, 2)),
		gone(11, replicaSet("orders-a", "orders")),
		gone(12, a1),
		gone(14, a2),
	}
}

// statefulSetBack replaces pod web-0 of StatefulSet web. The new web-0 has a
// new UID and is told before the old one is gone.
func statefulSetBack() ([]runtime.Object, []ev) {
	const rolling = appsv1.RollingUpdateStatefulSetStrategyType
	old := pod("web-0", "w0a", "StatefulSet", "web", true, true)
	before := []runtime.Object{
		statefulSet("web", 1, 1, 1, 1, 1, rolling),
		pod("web-0", "w0a", "StatefulSet", "web", true, false),
	}
	return before, []ev{
		set(1, statefulSet("web", 2, 1, 1, 1, 1, rolling)),
		exited(2),
		set(5, old),
		set(8, pod("web-0", "w0b", "StatefulSet", "web", true, false)),
		set(9, statefulSet("web", 2, 2, 1, 1, 1, rolling)),
		gone(10, old),
	}
}

func TestTransitionFinish(t *testing.T) {
	// none: Finished is zero, or there is no moment at which the window is still open.
	const none = -1.0
	inc := deployment("orders", 2, 1, 2, 2, 2, 2) // a new spec, not observed yet
	done := deployment("orders", 2, 2, 2, 2, 2, 2)
	oldA1 := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, true)
	restarted := func(ready bool) *corev1.Pod {
		p := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", ready, false)
		p.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "app", RestartCount: 1}}
		return p
	}
	stsBefore, stsEvents := statefulSetBack()
	oldA2 := pod("orders-a-2", "a2", "ReplicaSet", "orders-a", true, true)
	newW0 := pod("web-0", "w0b", "StatefulSet", "web", true, true) // the new web-0, deleted
	stsAgainBefore, stsAgainEvents := statefulSetBack()
	liveA1 := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, false)
	unseenA3 := pod("orders-a-3", "a3", "ReplicaSet", "orders-a", true, false)
	orphaned := replicaSet("orders-a", "orders")
	orphaned.OwnerReferences = nil
	// agent is a complete DaemonSet at generation 3; agentNew and agentNewDone are a new object
	// under its name, with a new UID and generation 1.
	const dsRolling = appsv1.RollingUpdateDaemonSetStrategyType
	agent := daemonSet("agent", 3, 3, 3, 3, 3, dsRolling)
	agentNew := daemonSet("agent", 1, 0, 0, 0, 0, dsRolling)
	agentNew.UID = "uid-agent-2"
	agentNewDone := daemonSet("agent", 1, 1, 3, 3, 3, dsRolling)
	agentNewDone.UID = "uid-agent-2"
	// relabelled gives d the label app: app in place of its own.
	relabelled := func(d *appsv1.Deployment, app string) *appsv1.Deployment {
		d.Labels = map[string]string{"app": app}
		return d
	}
	onlyOrders := labels.SelectorFromSet(labels.Set{"app": "orders"})
	incOther := relabelled(deployment("orders", 2, 1, 2, 2, 2, 2), "other")
	doneOther := relabelled(deployment("orders", 2, 2, 2, 2, 2, 2), "other")
	jobs := deployment("jobs", 2, 1, 1, 1, 1, 1) // app=jobs, a new spec not observed yet
	jobsIn := relabelled(deployment("jobs", 2, 1, 1, 1, 1, 1), "orders")
	jobsInDone := relabelled(deployment("jobs", 2, 2, 1, 1, 1, 1), "orders")
	a3 := pod("orders-a-3", "a3", "ReplicaSet", "orders-a", false, false)
	for _, r := range []struct {
		name     string
		selector labels.Selector // nil: every workload
		before   []runtime.Object
		events   []ev
		finished float64
		stalled  bool
		open     float64 // State at this moment has not ended
		end      float64 // State at this moment has ended, and Ended is this moment
		restarts []string
	}{
		{name: "nothing changes and the command exits at +2 s",
			before: base(), events: []ev{exited(2)},
			finished: 2, open: 16, end: 17},
		{name: "the rollout completes at +20 s and the command exits at +30 s",
			before: base(), events: []ev{set(1, inc), set(20, done), exited(30)},
			finished: 30, open: 44, end: 45},
		{name: "an old pod is terminating until +26 s",
			before: base(), events: []ev{set(1, inc), exited(2), set(10, oldA1), set(20, done), gone(26, oldA1)},
			finished: 26, open: 40, end: 41},
		{name: "a pod created and deleted in the transition must be gone too",
			before: base(), events: []ev{set(1, inc), exited(2),
				set(4, replicaSet("orders-b", "orders")),
				set(5, pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false)),
				set(15, pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, true)),
				set(20, done),
				gone(23, pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, true))},
			finished: 23, open: 37, end: 38},
		{name: "a pod created in the settle time starts it again",
			before: base(), events: []ev{set(1, inc), exited(2), set(20, done),
				set(24, deployment("orders", 2, 2, 2, 2, 1, 2)),
				set(25, pod("orders-a-3", "a3", "ReplicaSet", "orders-a", false, false)),
				set(31, done)},
			finished: 31, open: 45, end: 46,
			restarts: []string{"pod orders-a-3 of deployment/orders was created 25.0 s after the transition began"}},
		{name: "a pod deleted in the settle time starts it again",
			before: base(), events: []ev{exited(2), set(5, oldA1), gone(7, oldA1)},
			finished: 7, open: 21, end: 22,
			restarts: []string{"pod orders-a-1 of deployment/orders was deleted 5.0 s after the transition began"}},
		{name: "a new spec in the settle time starts it again",
			before: base(), events: []ev{exited(2), set(5, inc), set(9, done)},
			finished: 9, open: 23, end: 24,
			restarts: []string{"the spec of deployment/orders changed 5.0 s after the transition began"}},
		{name: "readiness and a restarted container do not start it again",
			before: base(), events: []ev{exited(2), set(5, restarted(false)), set(8, restarted(true))},
			finished: 2, open: 16, end: 17},
		{name: "a workload that the selector leaves out counts for nothing",
			selector: labels.SelectorFromSet(labels.Set{"app": "orders"}),
			before: append(base(),
				deployment("redis", 2, 1, 1, 1, 1, 1),
				replicaSet("redis-a", "redis"),
				pod("redis-a-1", "r1", "ReplicaSet", "redis-a", true, true)),
			events:   []ev{exited(2), set(5, pod("redis-a-2", "r2", "ReplicaSet", "redis-a", false, false))},
			finished: 2, open: 16, end: 17},
		{name: "not finished at the timeout: a stall",
			before: base(), events: []ev{set(1, inc), exited(2)},
			finished: none, stalled: true, open: 299, end: 300},
		{name: "a change after the timeout, in the settle time, is a stall at that moment",
			before: base(), events: []ev{set(1, inc), exited(2), set(290, done),
				set(303, pod("orders-a-3", "a3", "ReplicaSet", "orders-a", false, false))},
			finished: none, stalled: true, open: none, end: 303,
			restarts: []string{"pod orders-a-3 of deployment/orders was created 303.0 s after the transition began"}},
		{name: "the settle time may run past the timeout",
			before: base(), events: []ev{set(1, inc), exited(2), set(290, done)},
			finished: 290, open: 304, end: 305},
		{name: "a StatefulSet's pod back under its name is a new pod",
			before: stsBefore, events: stsEvents,
			finished: 10, open: 24, end: 25},
		{name: "a workload deleted in the transition is complete when its pods are gone",
			before: base(), events: renamed(),
			finished: 14, open: 28, end: 29},
		{name: "a workload that updates with OnDelete does not hold the transition open",
			before: append(base(),
				daemonSet("agent", 2, 2, 3, 0, 3, appsv1.OnDeleteDaemonSetStrategyType),
				pod("agent-x", "x1", "DaemonSet", "agent", true, true)),
			events:   []ev{exited(2)},
			finished: 2, open: 16, end: 17},
		{name: "a StatefulSet's new pod under the old name is deleted in the settle time",
			before: stsAgainBefore, events: append(stsAgainEvents, set(12, newW0), gone(13, newW0)),
			finished: 13, open: 27, end: 28,
			restarts: []string{"pod web-0 of statefulset/web was deleted 12.0 s after the transition began"}},
		{name: "a workload deleted in the transition waits for pods not yet marked for deletion",
			before: base(), events: []ev{set(1, deployment("orders-v2", 1, 1, 2, 2, 2, 2)),
				gone(3, deployment("orders", 1, 1, 2, 2, 2, 2)), exited(4),
				set(5, oldA1), set(5, oldA2), gone(7, oldA1), gone(8, oldA2)},
			finished: 8, open: 22, end: 23},
		{name: "a pod gone with no deletion seen is deleted then, once",
			before: base(), events: []ev{exited(2), gone(5, liveA1), gone(9, liveA1), set(11, oldA1),
				gone(13, unseenA3)},
			finished: 13, open: 27, end: 28,
			restarts: []string{"pod orders-a-1 of deployment/orders was deleted 5.0 s after the transition began",
				"pod orders-a-3 of deployment/orders was deleted 13.0 s after the transition began"}},
		{name: "a ReplicaSet that loses its Deployment takes its pods out of it",
			before: base(), events: []ev{set(1, deployment("orders-v2", 1, 1, 2, 2, 2, 2)),
				gone(3, deployment("orders", 1, 1, 2, 2, 2, 2)), set(4, orphaned), exited(5)},
			finished: 5, open: 19, end: 20},
		{name: "a workload created in the settle time starts it again",
			before: base(), events: []ev{exited(2), set(5, deployment("orders-v2", 1, 0, 2, 0, 0, 0)),
				set(9, deployment("orders-v2", 1, 1, 2, 2, 2, 2))},
			finished: 9, open: 23, end: 24,
			restarts: []string{"the spec of deployment/orders-v2 changed 5.0 s after the transition began"}},
		{name: "a workload made again under its old name in the settle time starts it again",
			before:   append(base(), agent),
			events:   []ev{exited(2), gone(3, agent), set(5, agentNew), set(9, agentNewDone)},
			finished: 9, open: 23, end: 24,
			restarts: []string{"daemonset/agent was deleted 3.0 s after the transition began",
				"the spec of daemonset/agent changed 5.0 s after the transition began"}},
		{name: "a workload whose UID changes with no Gone told starts it again",
			before:   append(base(), agent),
			events:   []ev{exited(2), set(5, agentNew), set(9, agentNewDone)},
			finished: 9, open: 23, end: 24,
			restarts: []string{"the spec of daemonset/agent changed 5.0 s after the transition began"}},
		{name: "a workload told again after its Gone starts it again, with its old UID and generation",
			before:   append(base(), agent),
			events:   []ev{exited(2), gone(3, agent), set(5, agent)},
			finished: 5, open: 19, end: 20,
			restarts: []string{"daemonset/agent was deleted 3.0 s after the transition began",
				"the spec of daemonset/agent changed 5.0 s after the transition began"}},
		{name: "a workload first told gone waits for its pods",
			before: []runtime.Object{replicaSet("orders-a", "orders"), liveA1,
				pod("orders-a-2", "a2", "ReplicaSet", "orders-a", true, false)},
			events: []ev{gone(3, deployment("orders", 1, 1, 2, 2, 2, 2)), exited(4),
				set(5, oldA1), set(5, oldA2), gone(7, oldA1), gone(8, oldA2)},
			finished: 8, open: 22, end: 23},
		{name: "an object of another kind changes nothing",
			before: base(), events: []ev{exited(2), set(5, &corev1.ConfigMap{}), gone(6, &corev1.ConfigMap{})},
			finished: 2, open: 16, end: 17},
		{name: "a workload that stops matching while it rolls out stays watched in the transition",
			selector: onlyOrders,
			before:   base(), events: []ev{set(1, inc), set(2, incOther), exited(3), set(20, doneOther)},
			finished: 20, open: none, end: 35},
		{name: "a workload that joins in the settle time must complete its rollout",
			selector: onlyOrders,
			before:   append(base(), jobs),
			events:   []ev{exited(2), set(5, jobsIn), set(9, jobsInDone)},
			finished: 9, open: 23, end: 24,
			restarts: []string{"deployment/jobs joined the watched workloads 5.0 s after the transition began"}},
		{name: "a workload that a Set before the transition shows matching is watched from its start",
			selector: onlyOrders,
			before:   append(base(), jobs, jobsIn),
			events:   []ev{exited(2), set(9, jobsInDone)},
			finished: 9, open: 23, end: 24},
		{name: "a workload outside the selector that changes in the transition counts for nothing",
			selector: onlyOrders,
			before:   append(base(), jobs),
			events:   []ev{exited(2), set(5, deployment("jobs", 3, 1, 1, 1, 1, 1))},
			finished: 2, open: 16, end: 17},
		{name: "a workload told gone twice is deleted once",
			before:   append(base(), agent),
			events:   []ev{exited(2), gone(3, agent), gone(6, agent)},
			finished: 3, open: 17, end: 18,
			restarts: []string{"daemonset/agent was deleted 3.0 s after the transition began"}},
		{name: "the rollout completes after the timeout: a stall at the timeout",
			before: base(), events: []ev{set(1, inc), exited(2), set(305, done)},
			finished: none, stalled: true, open: none, end: 300},
		{name: "the last old pod is gone after the timeout: a stall at the timeout",
			before: base(), events: []ev{set(1, inc), exited(2), set(10, oldA1), set(20, done), gone(305, oldA1)},
			finished: none, stalled: true, open: none, end: 300},
		{name: "the command exits after the timeout: a stall at the timeout",
			before: base(), events: []ev{set(1, inc), set(20, done), exited(305)},
			finished: none, stalled: true, open: none, end: 300},
		{name: "a pod created when the settle time ends belongs to no transition",
			before: base(), events: []ev{exited(2), set(17, a3)},
			finished: 2, open: none, end: 17},
		{name: "a change exactly at the timeout, in the settle time, is a stall",
			before: base(), events: []ev{set(1, inc), exited(2), set(290, done), set(300, a3)},
			finished: none, stalled: true, open: none, end: 300,
			restarts: []string{"pod orders-a-3 of deployment/orders was created 300.0 s after the transition began"}},
		{name: "a workload deleted in the settle time holds the window until its pods are gone",
			before: base(), events: []ev{set(1, deployment("orders-v2", 1, 1, 2, 2, 2, 2)), exited(2),
				gone(5, deployment("orders", 1, 1, 2, 2, 2, 2)), set(20, oldA1), set(20, oldA2), gone(22, oldA1), gone(23, oldA2)},
			finished: 23, open: 37, end: 38,
			restarts: []string{"deployment/orders was deleted 5.0 s after the transition began"}},
	} {
		t.Run(r.name, func(t *testing.T) {
			o := options()
			if r.selector != nil {
				o.Selector = r.selector
			}
			tr := feed(o, r.before, r.events)
			if r.open != none {
				if s, ended := tr.State(at(r.open)); ended {
					t.Errorf("State(+%v s) has ended, at %v", r.open, s.Ended)
				}
			}
			s, ended := tr.State(at(r.end))
			if !ended {
				t.Fatalf("State(+%v s) has not ended", r.end)
			}
			want := Transition{Name: verdict.Deploy, Began: t0, Ended: at(r.end),
				Stalled: r.stalled, SettleRestarts: r.restarts}
			if r.finished != none {
				want.Finished = at(r.finished)
			}
			got := Transition{Name: s.Name, Began: s.Began, Finished: s.Finished, Ended: s.Ended,
				Stalled: s.Stalled, SettleRestarts: s.SettleRestarts}
			if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("transition (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBeginTwice(t *testing.T) {
	tr := feed(options(), base(), []ev{exited(2)}) // finished at +2 s, ends at +17 s
	func() {
		defer func() {
			if recover() == nil {
				t.Error("Begin at +10 s, while the deploy's window is open, did not panic")
			}
		}()
		tr.Begin(verdict.Rollback, at(10))
	}()
	tr = feed(options(), base(), []ev{exited(2)})
	tr.Begin(verdict.Rollback, at(20))
	if s, _ := tr.State(at(20)); s.Name != verdict.Rollback || !s.Began.Equal(at(20)) {
		t.Errorf("after the deploy's window ended, Begin gave %+v", s)
	}
}

func TestStateBeforeBegin(t *testing.T) {
	tr := NewTracker(options())
	for _, obj := range base() {
		tr.Set(obj, t0)
	}
	if s, ended := tr.State(at(1)); ended || !cmp.Equal(s, Transition{}) {
		t.Errorf("State before Begin = %+v, %v; want a zero Transition and false", s, ended)
	}
}

func TestWorkloadString(t *testing.T) {
	if got := (Workload{Kind: "deployment", Name: "orders"}).String(); got != "deployment/orders" {
		t.Errorf("String() = %q, want deployment/orders", got)
	}
}
