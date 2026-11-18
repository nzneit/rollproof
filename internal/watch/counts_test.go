package watch

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	"rollproof/internal/verdict"
)

var (
	orders = Workload{Kind: "deployment", Name: "orders"}
	inc    = deployment("orders", 2, 1, 2, 2, 2, 2) // a new spec, not observed yet
)

// rollingUpdate is the deploy after base: ReplicaSet orders-b brings two new
// pods, and an old pod is deleted each time a new one is ready. It finishes
// at +12.1 s and its window ends at +27.1 s.
func rollingUpdate() []ev {
	a1 := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, true)
	a2 := pod("orders-a-2", "a2", "ReplicaSet", "orders-a", true, true)
	return []ev{
		set(0.2, inc),
		exited(0.5),
		set(2, replicaSet("orders-b", "orders")),
		set(2, pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false)),
		set(4, pod("orders-b-1", "b1", "ReplicaSet", "orders-b", true, false)),
		set(4, a1),
		set(5, pod("orders-b-2", "b2", "ReplicaSet", "orders-b", false, false)),
		set(7, pod("orders-b-2", "b2", "ReplicaSet", "orders-b", true, false)),
		set(7, a2),
		set(8, deployment("orders", 2, 2, 2, 2, 2, 2)),
		gone(9.3, a1),
		gone(12.1, a2),
	}
}

func TestCounts(t *testing.T) {
	stsBefore, stsEvents := statefulSetBack()
	for _, r := range []struct {
		name     string
		before   []runtime.Object
		events   []ev
		end      float64 // the window has ended by then
		replaced bool
		counts   []Count
	}{
		{"a rolling update of two pods", base(), rollingUpdate(), 27.1, true,
			[]Count{{Workload: orders, AtStart: 2, Replaced: 2, Created: 2, QuickestStop: 5100 * time.Millisecond}}},
		{"nothing changes", base(), []ev{exited(2)}, 17, false,
			[]Count{{Workload: orders, AtStart: 2}}},
		{"two workloads, only the second changes",
			append(base(), deployment("cache", 1, 1, 1, 1, 1, 1), replicaSet("cache-a", "cache"),
				pod("cache-a-1", "k1", "ReplicaSet", "cache-a", true, false)),
			rollingUpdate(), 27.1, true,
			[]Count{
				{Workload: Workload{Kind: "deployment", Name: "cache"}, AtStart: 1},
				{Workload: orders, AtStart: 2, Replaced: 2, Created: 2, QuickestStop: 5100 * time.Millisecond},
			}},
		{"a renamed Deployment", base(), renamed(), 29, true,
			[]Count{
				{Workload: orders, AtStart: 2, Replaced: 2, Removed: true, QuickestStop: 8 * time.Second},
				{Workload: Workload{Kind: "deployment", Name: "orders-v2"}, Created: 2, Added: true},
			}},
		{"a scale-up replaces nothing", base(), []ev{
			set(1, deployment("orders", 2, 1, 3, 2, 2, 2)),
			exited(1.5),
			set(2, pod("orders-a-3", "a3", "ReplicaSet", "orders-a", false, false)),
			set(4, pod("orders-a-3", "a3", "ReplicaSet", "orders-a", true, false)),
			set(4.5, deployment("orders", 2, 2, 3, 3, 3, 3)),
		}, 19.5, false,
			[]Count{{Workload: orders, AtStart: 2, Created: 1}}},
		{"a StatefulSet's pod back under its name", stsBefore, stsEvents, 25, true,
			[]Count{{Workload: Workload{Kind: "statefulset", Name: "web"},
				AtStart: 1, Replaced: 1, Created: 1, QuickestStop: 5 * time.Second}}},
	} {
		t.Run(r.name, func(t *testing.T) {
			s, ended := feed(options(), r.before, r.events).State(at(r.end))
			if !ended || s.Stalled {
				t.Fatalf("State(+%v s): ended %v, stalled %v; want ended and not stalled", r.end, ended, s.Stalled)
			}
			if s.Replaced != r.replaced {
				t.Errorf("Replaced = %v, want %v", s.Replaced, r.replaced)
			}
			if diff := cmp.Diff(r.counts, s.Workloads, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("counts (-want +got):\n%s", diff)
			}
		})
	}
}

// A pod first seen after the moment the transition began is created by it,
// even when Begin is told later.
func TestBeginToldLate(t *testing.T) {
	tr := NewTracker(options())
	for _, obj := range base() {
		tr.Set(obj, t0)
	}
	tr.Set(replicaSet("orders-b", "orders"), at(0.5))
	tr.Set(pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false), at(0.5))
	tr.Begin(verdict.Deploy, t0)
	s, _ := tr.State(at(1))
	want := []Count{{Workload: orders, AtStart: 2, Created: 1}}
	if diff := cmp.Diff(want, s.Workloads, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("counts (-want +got):\n%s", diff)
	}
}

func TestQuickestStop(t *testing.T) {
	a1 := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, true)
	a2 := pod("orders-a-2", "a2", "ReplicaSet", "orders-a", true, true)
	neverReady := pod("orders-b-3", "b3", "ReplicaSet", "orders-b", false, true)
	events := []ev{
		set(0.2, inc),
		exited(0.5),
		set(2, replicaSet("orders-b", "orders")),
		set(2, pod("orders-b-1", "b1", "ReplicaSet", "orders-b", true, false)),
		set(3, pod("orders-b-2", "b2", "ReplicaSet", "orders-b", true, false)),
		set(5, pod("orders-b-3", "b3", "ReplicaSet", "orders-b", false, false)),
		set(6, neverReady),
		gone(6.1, neverReady),
		set(10, a1),
		set(12, a2),
		set(13, deployment("orders", 2, 2, 2, 2, 2, 2)),
		gone(15.3, a1),
		gone(17.2, a2),
	}
	s, _ := feed(options(), base(), events).State(at(32.2))
	if got := s.Workloads[0].QuickestStop; got != 5200*time.Millisecond {
		t.Errorf("QuickestStop = %v, want 5.2s: the pod that was never ready does not count", got)
	}
}

func TestDigests(t *testing.T) {
	withImages := func(p *corev1.Pod, imageIDs ...string) *corev1.Pod {
		for i, id := range imageIDs {
			p.Status.ContainerStatuses = append(p.Status.ContainerStatuses,
				corev1.ContainerStatus{Name: []string{"app", "sidecar"}[i], ImageID: id})
		}
		return p
	}
	before := base()
	withImages(before[2].(*corev1.Pod), "rollproof.test/orders@sha256:old")
	events := []ev{
		set(0.2, inc),
		exited(0.5),
		set(1, replicaSet("orders-b", "orders")),
		set(2, withImages(pod("orders-b-1", "b1", "ReplicaSet", "orders-b", true, false),
			"rollproof.test/orders@sha256:aaa", "docker.io/library/x@sha256:bbb")),
		set(3, withImages(pod("orders-b-2", "b2", "ReplicaSet", "orders-b", true, false),
			"rollproof.test/orders@sha256:aaa")),
	}
	s, _ := feed(options(), before, events).State(at(4))
	if diff := cmp.Diff([]string{"sha256:aaa", "sha256:bbb"}, s.Workloads[0].Digests); diff != "" {
		t.Errorf("digests of the created pods (-want +got):\n%s", diff)
	}
}

func TestNoPodReplaced(t *testing.T) {
	noPod := []verdict.Condition{{Code: verdict.NoPodReplaced,
		Text: "the transition finished and replaced no pod: it changed nothing the test can judge"}}
	s, _ := feed(options(), base(), []ev{exited(2)}).State(at(17))
	if diff := cmp.Diff(noPod, s.Conditions(true)); diff != "" {
		t.Errorf("Conditions(true) of a transition that changed nothing (-want +got):\n%s", diff)
	}
	if got := s.Conditions(false); len(got) != 0 {
		t.Errorf("Conditions(false) = %v; a failed command is judged as what it is", got)
	}
	s, _ = feed(options(), base(), rollingUpdate()).State(at(27.1))
	if got := s.Conditions(true); len(got) != 0 {
		t.Errorf("Conditions(true) of a rolling update = %v, want none", got)
	}
}

func TestStallDetails(t *testing.T) {
	const scheduler = "0/4 nodes are available: 1 node(s) had untolerated taint {node-role.kubernetes.io/control-plane: }, 3 node(s) didn't match pod anti-affinity rules."
	const notUpdated = `deployment/orders: Waiting for deployment "orders" rollout to finish: 1 out of 2 new replicas have been updated...`
	// The Transition holds no worker count, so the sentence carries it (Workers is 3).
	const noRoom = "pod orders-b-1 could not be scheduled on the test cluster's 3 worker nodes (cluster.workers): " + scheduler
	pending := pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false)
	pending.Status.Phase = corev1.PodPending
	pending.Status.Conditions = append(pending.Status.Conditions, corev1.PodCondition{Type: corev1.PodScheduled,
		Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable, Message: scheduler})
	crashing := pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false)
	crashing.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "app", RestartCount: 4,
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
		LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
			Reason: "Error", ExitCode: 1}}}}
	for _, r := range []struct {
		name          string
		events        []ev
		end           float64
		incomplete    []string
		unschedulable []string
		conditions    []verdict.Condition
	}{
		{name: "a pod that cannot be scheduled",
			events: []ev{set(0.2, inc), exited(0.5), set(2, replicaSet("orders-b", "orders")),
				set(2, pending), set(2.5, deployment("orders", 2, 2, 2, 1, 2, 3))},
			end: 300,
			incomplete: []string{notUpdated,
				"pod orders-b-1: phase Pending, not scheduled (Unschedulable: " + scheduler + ")"},
			unschedulable: []string{noRoom},
			conditions: []verdict.Condition{
				{Code: verdict.TransitionStalled, Text: "did not finish within limits.timeout"},
				{Code: verdict.PodUnschedulable, Text: noRoom + "; a rolling update needs room for one more pod"},
			}},
		{name: "a pod in CrashLoopBackOff",
			events: []ev{set(0.2, inc), exited(0.5), set(2, replicaSet("orders-b", "orders")),
				set(2, crashing), set(3, deployment("orders", 2, 2, 2, 1, 1, 3))},
			end: 300,
			incomplete: []string{notUpdated,
				"pod orders-b-1: phase Running, container app waiting (CrashLoopBackOff) and last ended (Error, exit code 1)"},
			conditions: []verdict.Condition{
				{Code: verdict.TransitionStalled, Text: "did not finish within limits.timeout"},
			}},
		{name: "the settle time started again after the timeout",
			events: []ev{set(1, inc), exited(2), set(290, deployment("orders", 2, 2, 2, 2, 2, 2)),
				set(303, pod("orders-a-3", "a3", "ReplicaSet", "orders-a", false, false))},
			end: 303,
			conditions: []verdict.Condition{
				{Code: verdict.TransitionStalled, Text: "did not finish within limits.timeout; the settle time started again: " +
					"pod orders-a-3 of deployment/orders was created 303.0 s after the transition began"},
			}},
	} {
		t.Run(r.name, func(t *testing.T) {
			s, ended := feed(options(), base(), r.events).State(at(r.end))
			if !ended || !s.Stalled {
				t.Fatalf("State(+%v s): ended %v, stalled %v; want a stall", r.end, ended, s.Stalled)
			}
			if diff := cmp.Diff(r.incomplete, s.Incomplete, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Incomplete (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(r.unschedulable, s.Unschedulable, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Unschedulable (-want +got):\n%s", diff)
			}
			// A stall never gives no-pod-replaced, whether its command succeeded or not.
			for _, ok := range []bool{true, false} {
				if diff := cmp.Diff(r.conditions, s.Conditions(ok)); diff != "" {
					t.Errorf("Conditions(%v) (-want +got):\n%s", ok, diff)
				}
			}
		})
	}
}

func TestUnsupported(t *testing.T) {
	web := statefulSet("web", 1, 0, 1, 0, 0, appsv1.OnDeleteStatefulSetStrategyType)
	events := append([]ev{set(0.1, web)}, rollingUpdate()...)
	s, ended := feed(options(), base(), events).State(at(27.1))
	if !ended || s.Stalled || !s.Finished.Equal(at(12.1)) {
		t.Fatalf("ended %v, stalled %v, finished %v; the OnDelete workload must not hold the transition open",
			ended, s.Stalled, s.Finished)
	}
	if diff := cmp.Diff([]Workload{{Kind: "statefulset", Name: "web"}}, s.Unsupported); diff != "" {
		t.Errorf("Unsupported (-want +got):\n%s", diff)
	}
	want := []verdict.Condition{{Code: verdict.WorkloadUnsupported,
		Text: "statefulset/web updates with OnDelete; the tool judges only a workload that updates by rolling"}}
	if diff := cmp.Diff(want, s.Conditions(true)); diff != "" {
		t.Errorf("Conditions (-want +got):\n%s", diff)
	}
}

func TestInstallState(t *testing.T) {
	const running = -1.0 // the install command has not exited
	installed := base()
	for _, obj := range installed[2:] {
		p := obj.(*corev1.Pod)
		p.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "app", ImageID: "rollproof.test/orders@sha256:old"}}
	}
	for _, r := range []struct {
		name        string
		selector    labels.Selector // nil: every workload
		objs        []runtime.Object
		exited, now float64
		ready       bool
		install     Install
		code        verdict.Code // empty: no error
		text        string
	}{
		{name: "ready", objs: installed, exited: 3, now: 3, ready: true,
			install: Install{Ready: at(3), Workloads: []Count{{Workload: orders, Digests: []string{"sha256:old"}}}}},
		{name: "the command still runs", objs: installed, exited: running, now: 3},
		{name: "nothing watched, one second before the settle time has passed",
			selector: labels.SelectorFromSet(labels.Set{"app": "order"}), objs: installed, exited: 2, now: 16},
		{name: "nothing watched once the settle time has passed",
			selector: labels.SelectorFromSet(labels.Set{"app": "order"}), objs: installed, exited: 2, now: 17,
			code: verdict.NothingWatched,
			text: "nothing to watch\n" +
				"15 s after the install command returned, namespace orders held no Deployment, StatefulSet or DaemonSet that matches the selector app=order"},
		{name: "nothing watched, with no selector", exited: 2, now: 17,
			code: verdict.NothingWatched,
			text: "nothing to watch\n" +
				"15 s after the install command returned, namespace orders held no Deployment, StatefulSet or DaemonSet"},
		{name: "a DaemonSet that updates with OnDelete",
			objs:   append(base(), daemonSet("agent", 1, 1, 3, 3, 3, appsv1.OnDeleteDaemonSetStrategyType)),
			exited: 2, now: 2,
			code: verdict.WorkloadUnsupported,
			text: "daemonset/agent updates with OnDelete; the tool judges only a workload that updates by rolling"},
		{name: "not ready one second before the timeout",
			objs: []runtime.Object{deployment("orders", 1, 0, 2, 0, 0, 0)}, exited: 2, now: 299},
		{name: "not ready at the timeout",
			objs: []runtime.Object{deployment("orders", 1, 0, 2, 0, 0, 0)}, exited: 2, now: 300,
			code: verdict.InstallFailed,
			text: "the install was not ready within limits.timeout\n" +
				"deployment/orders: Waiting for deployment spec update to be observed..."},
	} {
		t.Run(r.name, func(t *testing.T) {
			o := options()
			if r.selector != nil {
				o.Selector = r.selector
			}
			tr := NewTracker(o)
			for _, obj := range r.objs {
				tr.Set(obj, at(1))
			}
			var exitedAt time.Time
			if r.exited != running {
				exitedAt = at(r.exited)
			}
			install, ready, err := tr.InstallState(t0, exitedAt, at(r.now))
			if ready != r.ready {
				t.Errorf("ready = %v, want %v", ready, r.ready)
			}
			if diff := cmp.Diff(r.install, install, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("install (-want +got):\n%s", diff)
			}
			var c verdict.Condition
			switch {
			case r.code == "" && err != nil:
				t.Errorf("err = %v, want none", err)
			case r.code != "" && (!errors.As(err, &c) || c.Code != r.code || c.Text != r.text):
				t.Errorf("err = %v, want %s: %s", err, r.code, r.text)
			}
		})
	}
}

func TestPodsAt(t *testing.T) {
	tr := feed(options(), base(), rollingUpdate())
	stopping, starting := tr.PodsAt(at(4.3))
	want := []PodNote{{Name: "orders-a-1", Workload: orders, Old: true, For: 300 * time.Millisecond}}
	if diff := cmp.Diff(want, stopping); diff != "" {
		t.Errorf("stopping at +4.3 s (-want +got):\n%s", diff)
	}
	if len(starting) != 0 {
		t.Errorf("starting at +4.3 s = %v, want none", starting)
	}
	stopping, starting = tr.PodsAt(at(5.5))
	want = []PodNote{{Name: "orders-a-1", Workload: orders, Old: true, For: 1500 * time.Millisecond}}
	if diff := cmp.Diff(want, stopping); diff != "" {
		t.Errorf("stopping at +5.5 s (-want +got):\n%s", diff)
	}
	want = []PodNote{{Name: "orders-b-2", Workload: orders, For: 500 * time.Millisecond}}
	if diff := cmp.Diff(want, starting); diff != "" {
		t.Errorf("starting at +5.5 s (-want +got):\n%s", diff)
	}
}

// A moment of the deploy, asked during the rollback, is judged against the
// deploy: a pod that the deploy created is not old then, although it is an
// old pod of the rollback.
func TestPodsAtAnEarlierTransition(t *testing.T) {
	tr := feed(options(), base(), rollingUpdate()) // the deploy's window ends at +27.1 s
	tr.Begin(verdict.Rollback, at(30))
	tr.Set(pod("orders-b-1", "b1", "ReplicaSet", "orders-b", true, true), at(32))
	stopping, starting := tr.PodsAt(at(5.5))
	want := []PodNote{{Name: "orders-a-1", Workload: orders, Old: true, For: 1500 * time.Millisecond}}
	if diff := cmp.Diff(want, stopping); diff != "" {
		t.Errorf("stopping at +5.5 s (-want +got):\n%s", diff)
	}
	want = []PodNote{{Name: "orders-b-2", Workload: orders, For: 500 * time.Millisecond}}
	if diff := cmp.Diff(want, starting); diff != "" {
		t.Errorf("starting at +5.5 s, a moment of the deploy (-want +got):\n%s", diff)
	}
	stopping, starting = tr.PodsAt(at(32.4))
	want = []PodNote{{Name: "orders-b-1", Workload: orders, Old: true, For: 400 * time.Millisecond}}
	if diff := cmp.Diff(want, stopping); diff != "" {
		t.Errorf("stopping at +32.4 s, a moment of the rollback (-want +got):\n%s", diff)
	}
	if len(starting) != 0 {
		t.Errorf("starting at +32.4 s = %v, want none", starting)
	}
}

// play tells tr the events in order, as feed does after Begin.
func play(tr *Tracker, events []ev) {
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
}

// relabel gives d the label app: app in place of its own.
func relabel(d *appsv1.Deployment, app string) *appsv1.Deployment {
	d.Labels = map[string]string{"app": app}
	return d
}

// In the rollback, Deployment orders did not exist when it began, although its
// record holds a live object by the time the counts are asked.
func TestCountsAfterARenameAndARollback(t *testing.T) {
	tr := feed(options(), base(), renamed()) // the deploy's window ends at +29 s
	back := deployment("orders", 1, 0, 2, 0, 0, 0)
	back.UID = "uid-orders-2"
	backDone := deployment("orders", 1, 1, 2, 2, 2, 2)
	backDone.UID = "uid-orders-2"
	c1 := pod("orders-v2-a-1", "c1", "ReplicaSet", "orders-v2-a", true, true)
	c2 := pod("orders-v2-a-2", "c2", "ReplicaSet", "orders-v2-a", true, true)
	tr.Begin(verdict.Rollback, at(30))
	play(tr, []ev{
		set(31, back),
		set(31, replicaSet("orders-c", "orders")),
		exited(32),
		gone(33, deployment("orders-v2", 1, 1, 2, 2, 2, 2)),
		set(34, c1),
		set(34, c2),
		set(35, pod("orders-c-1", "d1", "ReplicaSet", "orders-c", true, false)),
		set(35, pod("orders-c-2", "d2", "ReplicaSet", "orders-c", true, false)),
		set(39, backDone),
		gone(41, replicaSet("orders-v2-a", "orders-v2")),
		gone(42, c1),
		gone(44, c2),
	})
	s, ended := tr.State(at(59))
	if !ended || s.Stalled || s.Name != verdict.Rollback {
		t.Fatalf("State(+59 s): %s, ended %v, stalled %v; want the rollback, ended and not stalled", s.Name, ended, s.Stalled)
	}
	want := []Count{
		{Workload: orders, Created: 2, Added: true},
		{Workload: Workload{Kind: "deployment", Name: "orders-v2"}, AtStart: 2, Replaced: 2, Removed: true,
			QuickestStop: 8 * time.Second},
	}
	if diff := cmp.Diff(want, s.Workloads, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("counts of the rollback (-want +got):\n%s", diff)
	}
	if !s.Replaced {
		t.Error("Replaced = false, want true")
	}
}

// A workload first seen after the moment the transition began did not exist
// then, even when Begin is told later; so does one made again under its old
// name after that moment.
func TestBeginToldLateWorkload(t *testing.T) {
	cache := Workload{Kind: "deployment", Name: "cache"}
	tr := NewTracker(options())
	for _, obj := range base() {
		tr.Set(obj, t0)
	}
	tr.Set(deployment("cache", 1, 1, 1, 1, 1, 1), at(0.5))
	tr.Begin(verdict.Deploy, t0)
	s, _ := tr.State(at(1))
	want := []Count{{Workload: cache, Added: true}, {Workload: orders, AtStart: 2}}
	if diff := cmp.Diff(want, s.Workloads, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("counts with a workload told late (-want +got):\n%s", diff)
	}

	again := deployment("orders", 1, 1, 2, 2, 2, 2)
	again.UID = "uid-orders-2"
	tr = NewTracker(options())
	for _, obj := range base() {
		tr.Set(obj, at(-2))
	}
	tr.Gone(base()[0], at(-1))
	tr.Set(again, at(0.5))
	tr.Begin(verdict.Deploy, t0)
	s, _ = tr.State(at(1))
	want = []Count{{Workload: orders, AtStart: 2, Added: true}}
	if diff := cmp.Diff(want, s.Workloads, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("counts with a workload made again and told late (-want +got):\n%s", diff)
	}
}

func TestCountsFollowTheWatchedSet(t *testing.T) {
	onlyOrders := labels.SelectorFromSet(labels.Set{"app": "orders"})
	oldA1 := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, true)
	oldA2 := pod("orders-a-2", "a2", "ReplicaSet", "orders-a", true, true)
	for _, r := range []struct {
		name     string
		selector labels.Selector // nil: every workload
		before   []runtime.Object
		events   []ev
		end      float64
		counts   []Count
	}{
		{name: "a workload that joins in the settle time is counted",
			selector: onlyOrders,
			before:   append(base(), deployment("jobs", 2, 1, 1, 1, 1, 1)),
			events: []ev{exited(2), set(5, relabel(deployment("jobs", 2, 1, 1, 1, 1, 1), "orders")),
				set(9, relabel(deployment("jobs", 2, 2, 1, 1, 1, 1), "orders"))},
			end:    24,
			counts: []Count{{Workload: Workload{Kind: "deployment", Name: "jobs"}}, {Workload: orders, AtStart: 2}}},
		{name: "a workload that stops matching while it rolls out stays counted",
			selector: onlyOrders,
			before:   base(),
			events: []ev{set(1, inc), set(2, relabel(deployment("orders", 2, 1, 2, 2, 2, 2), "other")), exited(3),
				set(20, relabel(deployment("orders", 2, 2, 2, 2, 2, 2), "other"))},
			end:    35,
			counts: []Count{{Workload: orders, AtStart: 2}}},
		{name: "a workload deleted in the settle time is removed, and its pods replaced",
			before: base(),
			events: []ev{set(1, deployment("orders-v2", 1, 1, 2, 2, 2, 2)), exited(2),
				gone(5, deployment("orders", 1, 1, 2, 2, 2, 2)), set(20, oldA1), set(20, oldA2),
				gone(22, oldA1), gone(23, oldA2)},
			end: 38,
			counts: []Count{
				{Workload: orders, AtStart: 2, Replaced: 2, Removed: true, QuickestStop: 2 * time.Second},
				{Workload: Workload{Kind: "deployment", Name: "orders-v2"}, Added: true},
			}},
	} {
		t.Run(r.name, func(t *testing.T) {
			o := options()
			if r.selector != nil {
				o.Selector = r.selector
			}
			s, ended := feed(o, r.before, r.events).State(at(r.end))
			if !ended || s.Stalled {
				t.Fatalf("State(+%v s): ended %v, stalled %v; want ended and not stalled", r.end, ended, s.Stalled)
			}
			if diff := cmp.Diff(r.counts, s.Workloads, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("counts (-want +got):\n%s", diff)
			}
		})
	}
}

// The counts of an ended window stay as they were at its end, whatever the
// records hold later: a workload made again, a ReplicaSet that loses its
// Deployment.
func TestCountsKeptWhenTheWindowEnds(t *testing.T) {
	tr := feed(options(), base(), renamed()) // the window ends at +29 s
	orphaned := replicaSet("orders-v2-a", "orders-v2")
	orphaned.OwnerReferences = nil
	tr.Set(deployment("orders", 1, 0, 2, 0, 0, 0), at(35))
	tr.Set(orphaned, at(35))
	s, ended := tr.State(at(36))
	if !ended {
		t.Fatal("State(+36 s) has not ended")
	}
	want := []Count{
		{Workload: orders, AtStart: 2, Replaced: 2, Removed: true, QuickestStop: 8 * time.Second},
		{Workload: Workload{Kind: "deployment", Name: "orders-v2"}, Created: 2, Added: true},
	}
	if diff := cmp.Diff(want, s.Workloads, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("counts after the window ended (-want +got):\n%s", diff)
	}
	if !s.Replaced {
		t.Error("Replaced = false, want true")
	}
}

// A pod known only from its Gone was shown by no Set: nothing says that the
// transition created it, so it counts for no Created, and a scale-down stays a
// transition that replaced no pod.
func TestCountsLeaveOutAPodKnownOnlyFromItsGone(t *testing.T) {
	liveA1 := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, false)
	unseenA3 := pod("orders-a-3", "a3", "ReplicaSet", "orders-a", true, false)
	s, ended := feed(options(), base(), []ev{exited(2), gone(5, liveA1), gone(6, unseenA3)}).State(at(21))
	if !ended || s.Stalled {
		t.Fatalf("State(+21 s): ended %v, stalled %v; want ended and not stalled", ended, s.Stalled)
	}
	want := []Count{{Workload: orders, AtStart: 2, Replaced: 1}}
	if diff := cmp.Diff(want, s.Workloads, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("counts (-want +got):\n%s", diff)
	}
	noPod := []verdict.Condition{{Code: verdict.NoPodReplaced,
		Text: "the transition finished and replaced no pod: it changed nothing the test can judge"}}
	if diff := cmp.Diff(noPod, s.Conditions(true)); diff != "" {
		t.Errorf("Conditions(true) (-want +got):\n%s", diff)
	}
}

// A pod whose deletion was first seen before the transition began did not
// stop in it.
func TestQuickestStopCountsFromTheStart(t *testing.T) {
	early := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, true)
	a2 := pod("orders-a-2", "a2", "ReplicaSet", "orders-a", true, true)
	tr := NewTracker(options())
	for _, obj := range base() {
		tr.Set(obj, at(-2))
	}
	tr.Set(early, at(-1))
	tr.Begin(verdict.Deploy, t0)
	play(tr, []ev{exited(1), gone(4, early), set(5, a2), gone(8, a2)})
	s, _ := tr.State(at(9))
	if got := s.Workloads[0].QuickestStop; got != 3*time.Second {
		t.Errorf("QuickestStop = %v, want 3s: the pod whose deletion was seen before the start does not count", got)
	}
}

// A pod gone at the moment its deletion was first seen stopped in no time,
// and no slower pod replaces that figure.
func TestQuickestStopOfZero(t *testing.T) {
	a1 := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, true)
	a2 := pod("orders-a-2", "a2", "ReplicaSet", "orders-a", true, true)
	events := []ev{
		set(0.2, inc),
		exited(0.5),
		set(2, replicaSet("orders-b", "orders")),
		set(2, pod("orders-b-1", "b1", "ReplicaSet", "orders-b", true, false)),
		set(5, pod("orders-b-2", "b2", "ReplicaSet", "orders-b", true, false)),
		set(7, a2),
		set(8, deployment("orders", 2, 2, 2, 2, 2, 2)),
		set(9.3, a1),
		gone(9.3, a1),
		gone(12.1, a2),
	}
	s, _ := feed(options(), base(), events).State(at(27.1))
	if got := s.Workloads[0].QuickestStop; got != 0 {
		t.Errorf("QuickestStop = %v, want 0s", got)
	}
}

// Digests are those of every Set of a created pod up to the bound, and an
// image ID without a digest is skipped.
func TestDigestsOfEverySet(t *testing.T) {
	withImages := func(imageIDs ...string) *corev1.Pod {
		p := pod("orders-b-1", "b1", "ReplicaSet", "orders-b", true, false)
		for _, id := range imageIDs {
			p.Status.ContainerStatuses = append(p.Status.ContainerStatuses, corev1.ContainerStatus{Name: "app", ImageID: id})
		}
		return p
	}
	tr := feed(options(), base(), []ev{
		set(0.2, inc),
		exited(0.5),
		set(1, replicaSet("orders-b", "orders")),
		set(2, withImages("rollproof.test/orders@sha256:aaa")),
		set(3, withImages("docker-pullable://rollproof.test/orders@sha256:ccc", "docker.io/library/x:1.0")),
	})
	s, _ := tr.State(at(4))
	if diff := cmp.Diff([]string{"sha256:aaa", "sha256:ccc"}, s.Workloads[0].Digests); diff != "" {
		t.Errorf("digests at +4 s (-want +got):\n%s", diff)
	}
	s, _ = tr.State(at(2.5))
	if diff := cmp.Diff([]string{"sha256:aaa"}, s.Workloads[0].Digests); diff != "" {
		t.Errorf("digests at +2.5 s (-want +got):\n%s", diff)
	}
}

// A caller that changes a list of what State returned changes nothing in the
// Tracker, whether the window has ended or is open.
func TestStateReturnsCopies(t *testing.T) {
	pending := pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false)
	pending.Status.Phase = corev1.PodPending
	pending.Status.Conditions = append(pending.Status.Conditions, corev1.PodCondition{Type: corev1.PodScheduled,
		Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable, Message: "no room"})
	pending.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "app", ImageID: "rollproof.test/orders@sha256:aaa"}}
	oldA1 := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, true)
	nonEmpty := map[string]bool{}
	for _, r := range []struct {
		name   string
		before []runtime.Object
		events []ev
		now    float64
	}{
		{"a stall with a pod that cannot be scheduled",
			append(base(), statefulSet("web", 1, 1, 1, 1, 1, appsv1.OnDeleteStatefulSetStrategyType)),
			[]ev{set(0.2, inc), exited(0.5), set(2, replicaSet("orders-b", "orders")), set(2, pending),
				set(2.5, deployment("orders", 2, 2, 2, 1, 2, 3))}, 300},
		{"a stall after the settle time started again", base(),
			[]ev{set(1, inc), exited(2), set(290, deployment("orders", 2, 2, 2, 2, 2, 2)),
				set(303, pod("orders-a-3", "a3", "ReplicaSet", "orders-a", false, false))}, 303},
		{"an open window after the settle time started again", base(),
			[]ev{exited(2), set(5, oldA1)}, 6},
	} {
		t.Run(r.name, func(t *testing.T) {
			want, _ := feed(options(), r.before, r.events).State(at(r.now))
			tr := feed(options(), r.before, r.events)
			s, _ := tr.State(at(r.now))
			for i := range s.SettleRestarts {
				s.SettleRestarts[i] = "changed"
				nonEmpty["SettleRestarts"] = true
			}
			for i := range s.Incomplete {
				s.Incomplete[i] = "changed"
				nonEmpty["Incomplete"] = true
			}
			for i := range s.Unschedulable {
				s.Unschedulable[i] = "changed"
				nonEmpty["Unschedulable"] = true
			}
			for i := range s.Unsupported {
				s.Unsupported[i] = Workload{Kind: "changed"}
				nonEmpty["Unsupported"] = true
			}
			for i := range s.Workloads {
				s.Workloads[i].AtStart = 99
				nonEmpty["Workloads"] = true
				for j := range s.Workloads[i].Digests {
					s.Workloads[i].Digests[j] = "changed"
					nonEmpty["Digests"] = true
				}
			}
			got, _ := tr.State(at(r.now))
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("State after its lists were changed (-want +got):\n%s", diff)
			}
		})
	}
	for _, list := range []string{"SettleRestarts", "Incomplete", "Unschedulable", "Unsupported", "Workloads", "Digests"} {
		if !nonEmpty[list] {
			t.Errorf("no script fills %s, so its copy is not tested", list)
		}
	}
}

func TestMoreStallDetails(t *testing.T) {
	const notUpdated = `deployment/orders: Waiting for deployment "orders" rollout to finish: 1 out of 2 new replicas have been updated...`
	pending := pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false)
	pending.Status.Phase = corev1.PodPending
	pending.Status.Conditions = append(pending.Status.Conditions, corev1.PodCondition{Type: corev1.PodScheduled,
		Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable, Message: "no room"})
	crashing := pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false)
	crashing.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "app",
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}}}
	gated := pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false)
	gated.Status.Phase = corev1.PodPending
	gated.Status.Conditions = append(gated.Status.Conditions, corev1.PodCondition{Type: corev1.PodScheduled,
		Status: corev1.ConditionFalse, Reason: corev1.PodReasonSchedulingGated,
		Message: "Scheduling is blocked due to non-empty scheduling gates"})
	sidecar := pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false)
	sidecar.Status.Conditions = append(sidecar.Status.Conditions, corev1.PodCondition{Type: corev1.PodScheduled,
		Status: corev1.ConditionTrue})
	sidecar.Status.ContainerStatuses = []corev1.ContainerStatus{
		{Name: "app", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
		{Name: "sidecar",
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 137}},
			LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				Reason: "OOMKilled", ExitCode: 137}}},
	}
	noState := pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false)
	noState.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "app"}}
	oldA1 := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, true)
	oldA2 := pod("orders-a-2", "a2", "ReplicaSet", "orders-a", true, true)
	webPending := pod("web-1", "w1", "StatefulSet", "web", false, false)
	webPending.Status.Phase = corev1.PodPending
	webPending.Status.Conditions = pending.Status.Conditions
	rollingB := []ev{set(0.2, inc), exited(0.5), set(2, replicaSet("orders-b", "orders"))}
	notUpdatedStatus := deployment("orders", 2, 2, 2, 1, 1, 3)
	for _, r := range []struct {
		name          string
		before        []runtime.Object
		events        []ev
		end           float64
		incomplete    []string
		unschedulable []string
		conditions    []verdict.Condition
	}{
		{name: "a pod is described as its last Set shows it",
			events: append(rollingB, set(2, pending), set(3, crashing), set(3, notUpdatedStatus)),
			end:    300,
			incomplete: []string{notUpdated,
				"pod orders-b-1: phase Running, container app waiting (CrashLoopBackOff)"},
			conditions: []verdict.Condition{{Code: verdict.TransitionStalled, Text: "did not finish within limits.timeout"}}},
		{name: "a pod held by a scheduling gate is not one that could not be scheduled",
			events: append(rollingB, set(2, gated), set(3, notUpdatedStatus)),
			end:    300,
			incomplete: []string{notUpdated,
				"pod orders-b-1: phase Pending, not scheduled (SchedulingGated: Scheduling is blocked due to non-empty scheduling gates)"},
			conditions: []verdict.Condition{{Code: verdict.TransitionStalled, Text: "did not finish within limits.timeout"}}},
		{name: "a container running and one that ended",
			events: append(rollingB, set(2, sidecar), set(3, notUpdatedStatus)),
			end:    300,
			incomplete: []string{notUpdated,
				"pod orders-b-1: phase Running, container app running, container sidecar terminated (Error, exit code 137) and last ended (OOMKilled, exit code 137)"},
			conditions: []verdict.Condition{{Code: verdict.TransitionStalled, Text: "did not finish within limits.timeout"}}},
		{name: "a container with no state is waiting, as the API defines",
			events: append(rollingB, set(2, noState), set(3, notUpdatedStatus)),
			end:    300,
			incomplete: []string{notUpdated,
				"pod orders-b-1: phase Running, container app waiting ()"},
			conditions: []verdict.Condition{{Code: verdict.TransitionStalled, Text: "did not finish within limits.timeout"}}},
		{name: "a deleted workload with pods left, and a pod stopping, asked long after the stall",
			events: []ev{set(1, deployment("orders-v2", 1, 1, 2, 2, 2, 2)), exited(2),
				gone(3, deployment("orders", 1, 1, 2, 2, 2, 2)), set(10, oldA1)},
			end: 400,
			incomplete: []string{"deployment/orders was deleted and 2 of its pods are not gone",
				"pod orders-a-1 has been stopping for 290.0 s and is not gone"},
			conditions: []verdict.Condition{{Code: verdict.TransitionStalled,
				Text: "did not finish within limits.timeout; the settle time started again: " +
					"deployment/orders was deleted 3.0 s after the transition began"}}},
		{name: "a deleted workload counts only its pods that are not gone",
			events: []ev{set(1, deployment("orders-v2", 1, 1, 2, 2, 2, 2)), exited(2),
				gone(3, deployment("orders", 1, 1, 2, 2, 2, 2)), set(10, oldA1), gone(12, oldA1)},
			end:        300,
			incomplete: []string{"deployment/orders was deleted and 1 of its pods are not gone"},
			conditions: []verdict.Condition{{Code: verdict.TransitionStalled,
				Text: "did not finish within limits.timeout; the settle time started again: " +
					"deployment/orders was deleted 3.0 s after the transition began"}}},
		{name: "a deleted workload with no pod left gives no sentence",
			events: []ev{set(1, deployment("orders-v2", 1, 0, 2, 0, 0, 0)), exited(2),
				gone(3, deployment("orders", 1, 1, 2, 2, 2, 2)), set(4, oldA1), set(4, oldA2), gone(6, oldA1), gone(7, oldA2)},
			end:        300,
			incomplete: []string{"deployment/orders-v2: Waiting for deployment spec update to be observed..."},
			conditions: []verdict.Condition{{Code: verdict.TransitionStalled, Text: "did not finish within limits.timeout"}}},
		{name: "a pod that is gone is neither described nor named as not scheduled",
			events:     append(rollingB, set(2, pending), gone(2.2, pending), set(3, notUpdatedStatus)),
			end:        300,
			incomplete: []string{notUpdated},
			conditions: []verdict.Condition{{Code: verdict.TransitionStalled, Text: "did not finish within limits.timeout"}}},
		{name: "the details are taken after the change that caused the stall",
			events:     []ev{set(1, inc), exited(2), set(290, deployment("orders", 2, 2, 2, 2, 2, 2)), set(301, oldA1)},
			end:        301,
			incomplete: []string{"pod orders-a-1 has been stopping for 0.0 s and is not gone"},
			conditions: []verdict.Condition{{Code: verdict.TransitionStalled,
				Text: "did not finish within limits.timeout; the settle time started again: " +
					"pod orders-a-1 of deployment/orders was deleted 301.0 s after the transition began"}}},
		{name: "a workload that does not take part gives no details",
			before: append(base(), statefulSet("web", 2, 1, 2, 1, 0, appsv1.OnDeleteStatefulSetStrategyType),
				pod("web-0", "w0", "StatefulSet", "web", true, true), webPending),
			events:     []ev{set(1, inc), exited(2)},
			end:        300,
			incomplete: []string{"deployment/orders: Waiting for deployment spec update to be observed..."},
			conditions: []verdict.Condition{
				{Code: verdict.TransitionStalled, Text: "did not finish within limits.timeout"},
				{Code: verdict.WorkloadUnsupported,
					Text: "statefulset/web updates with OnDelete; the tool judges only a workload that updates by rolling"},
			}},
	} {
		t.Run(r.name, func(t *testing.T) {
			before := r.before
			if before == nil {
				before = base()
			}
			s, ended := feed(options(), before, r.events).State(at(r.end))
			if !ended || !s.Stalled {
				t.Fatalf("State(+%v s): ended %v, stalled %v; want a stall", r.end, ended, s.Stalled)
			}
			if diff := cmp.Diff(r.incomplete, s.Incomplete, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Incomplete (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(r.unschedulable, s.Unschedulable, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Unschedulable (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(r.conditions, s.Conditions(true)); diff != "" {
				t.Errorf("Conditions(true) (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUnschedulableNamesTheWorkers(t *testing.T) {
	pending := pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false)
	pending.Status.Phase = corev1.PodPending
	pending.Status.Conditions = append(pending.Status.Conditions, corev1.PodCondition{Type: corev1.PodScheduled,
		Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable, Message: "no room"})
	o := options()
	o.Workers = 5
	s, _ := feed(o, base(), []ev{set(0.2, inc), exited(0.5), set(2, replicaSet("orders-b", "orders")),
		set(2, pending), set(2.5, deployment("orders", 2, 2, 2, 1, 2, 3))}).State(at(300))
	want := []string{"pod orders-b-1 could not be scheduled on the test cluster's 5 worker nodes (cluster.workers): no room"}
	if diff := cmp.Diff(want, s.Unschedulable); diff != "" {
		t.Errorf("Unschedulable (-want +got):\n%s", diff)
	}
}

func TestUnsupportedAtSomeMoment(t *testing.T) {
	const judged = "; the tool judges only a workload that updates by rolling"
	agent := daemonSet("agent", 1, 1, 3, 3, 3, appsv1.OnDeleteDaemonSetStrategyType)
	web := statefulSet("web", 1, 1, 1, 1, 1, appsv1.OnDeleteStatefulSetStrategyType)
	agentWorkload := Workload{Kind: "daemonset", Name: "agent"}
	webWorkload := Workload{Kind: "statefulset", Name: "web"}
	for _, r := range []struct {
		name        string
		selector    labels.Selector // nil: every workload
		before      []runtime.Object
		events      []ev
		end         float64
		unsupported []Workload
		text        string // the text of WorkloadUnsupported; empty: none
	}{
		{name: "a workload watched from the start",
			before: append(base(), agent), events: []ev{exited(2)}, end: 17,
			unsupported: []Workload{agentWorkload}, text: "daemonset/agent updates with OnDelete" + judged},
		{name: "a workload that updates by rolling later in the transition stays listed",
			before: append(base(), agent),
			events: []ev{exited(2), set(5, daemonSet("agent", 2, 2, 3, 3, 3, appsv1.RollingUpdateDaemonSetStrategyType))},
			end:    20, unsupported: []Workload{agentWorkload}, text: "daemonset/agent updates with OnDelete" + judged},
		{name: "two workloads, sorted",
			before: append(base(), web, agent), events: []ev{exited(2)}, end: 17,
			unsupported: []Workload{agentWorkload, webWorkload},
			text:        "daemonset/agent updates with OnDelete; statefulset/web updates with OnDelete" + judged},
		{name: "a workload that joins the watched set while it does not roll",
			before: base(), events: []ev{exited(2), set(5, web)}, end: 17,
			unsupported: []Workload{webWorkload}, text: "statefulset/web updates with OnDelete" + judged},
		{name: "a workload the selector leaves out is not listed",
			selector: labels.SelectorFromSet(labels.Set{"app": "orders"}),
			before:   append(base(), agent), events: []ev{exited(2)}, end: 17},
		{name: "a workload the selector leaves out and that a Set shows in the window is not listed",
			selector: labels.SelectorFromSet(labels.Set{"app": "orders"}),
			before:   base(), events: []ev{exited(2), set(5, agent)}, end: 17},
		{name: "a workload told when the window ends is not listed",
			before: base(), events: []ev{exited(2), set(17, web)}, end: 17},
		{name: "a workload first told gone while it does not roll",
			before: base(), events: []ev{exited(2), gone(5, web)}, end: 17,
			unsupported: []Workload{webWorkload}, text: "statefulset/web updates with OnDelete" + judged},
	} {
		t.Run(r.name, func(t *testing.T) {
			o := options()
			if r.selector != nil {
				o.Selector = r.selector
			}
			s, ended := feed(o, r.before, r.events).State(at(r.end))
			if !ended || s.Stalled {
				t.Fatalf("State(+%v s): ended %v, stalled %v; want ended and not stalled", r.end, ended, s.Stalled)
			}
			if diff := cmp.Diff(r.unsupported, s.Unsupported, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Unsupported (-want +got):\n%s", diff)
			}
			var got string
			for _, c := range s.Conditions(false) {
				if c.Code == verdict.WorkloadUnsupported {
					got = c.Text
				}
			}
			if got != r.text {
				t.Errorf("WorkloadUnsupported text = %q, want %q", got, r.text)
			}
		})
	}
}

// A transition that finished keeps no stall details, although a pod of a
// complete workload could not be scheduled.
func TestNoStallDetailsForAFinish(t *testing.T) {
	pending := pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false)
	pending.Status.Phase = corev1.PodPending
	pending.Status.Conditions = append(pending.Status.Conditions, corev1.PodCondition{Type: corev1.PodScheduled,
		Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable, Message: "no room"})
	before := append(base(), replicaSet("orders-b", "orders"), pending)
	s, ended := feed(options(), before, []ev{exited(2)}).State(at(17))
	if !ended || s.Stalled {
		t.Fatalf("State(+17 s): ended %v, stalled %v; want ended and not stalled", ended, s.Stalled)
	}
	if len(s.Incomplete) != 0 || len(s.Unschedulable) != 0 {
		t.Errorf("Incomplete = %v, Unschedulable = %v; want none for a transition that finished", s.Incomplete, s.Unschedulable)
	}
}

// A Transition that did not stall and whose window has not ended, or ended
// without finishing, is not verified (ruling T26-2 as amended): read while
// open, before it finishes or in its settle time, or cut short by its caller
// before the timeout. It gives one InternalError, no stall and no
// no-pod-replaced. Its details are those of the moment State was asked.
func TestConditionsOfAWindowThatDidNotEnd(t *testing.T) {
	const notObserved = "deployment/orders: Waiting for deployment spec update to be observed..."
	early := verdict.Condition{Code: verdict.InternalError, Text: "the transition was judged before its window ended"}
	pending := pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false)
	pending.Status.Phase = corev1.PodPending
	pending.Status.Conditions = append(pending.Status.Conditions, corev1.PodCondition{Type: corev1.PodScheduled,
		Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable, Message: "no room"})
	oldA1 := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, true)
	for _, r := range []struct {
		name          string
		events        []ev
		now           float64
		cutShort      bool // the caller sets Ended to now, as a context that ended does
		finished      bool // whether Finished is set at now
		incomplete    []string
		unschedulable []string
		conditions    []verdict.Condition
	}{
		{name: "read while open, before it finishes",
			events: []ev{set(1, inc), exited(2)}, now: 3,
			incomplete: []string{notObserved},
			conditions: []verdict.Condition{early}},
		{name: "read while open, in its settle time, which gives no no-pod-replaced",
			events: []ev{exited(2)}, now: 10, finished: true,
			conditions: []verdict.Condition{early}},
		{name: "cut short before the timeout",
			events: []ev{set(1, inc), exited(2), set(10, oldA1), set(11, replicaSet("orders-b", "orders")),
				set(12, pending)},
			now: 50, cutShort: true,
			incomplete: []string{notObserved,
				"pod orders-b-1: phase Pending, not scheduled (Unschedulable: no room)",
				"pod orders-a-1 has been stopping for 40.0 s and is not gone"},
			unschedulable: []string{"pod orders-b-1 could not be scheduled on the test cluster's 3 worker nodes (cluster.workers): no room"},
			conditions:    []verdict.Condition{early}},
	} {
		t.Run(r.name, func(t *testing.T) {
			s, ended := feed(options(), base(), r.events).State(at(r.now))
			if ended || s.Stalled || s.Finished.IsZero() == r.finished {
				t.Fatalf("State(+%v s): ended %v, stalled %v, finished %v; want open, not stalled, finished %v",
					r.now, ended, s.Stalled, s.Finished, r.finished)
			}
			if r.cutShort {
				s.Ended = at(r.now)
			}
			if diff := cmp.Diff(r.incomplete, s.Incomplete, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Incomplete (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(r.unschedulable, s.Unschedulable, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Unschedulable (-want +got):\n%s", diff)
			}
			for _, ok := range []bool{true, false} {
				if diff := cmp.Diff(r.conditions, s.Conditions(ok)); diff != "" {
					t.Errorf("Conditions(%v) (-want +got):\n%s", ok, diff)
				}
			}
		})
	}
}

// A deploy cut short by SIGINT, before it finishes or in its settle time: the
// caller sets Ended to the moment its context ended and clears Finished
// (T26-3), and the run places interrupted in the deploy's window. The verdict
// is not verified, decided by interrupted, and no condition is a fail code
// (spec section 12; review row).
func TestConditionsOfASignalDuringTheDeploy(t *testing.T) {
	for _, r := range []struct {
		name   string
		events []ev
		now    float64
	}{
		{"before it finishes", []ev{set(1, inc), exited(2)}, 50},
		{"in its settle time", rollingUpdate(), 20},
	} {
		t.Run(r.name, func(t *testing.T) {
			s, ended := feed(options(), base(), r.events).State(at(r.now))
			if ended {
				t.Fatalf("State(+%v s) has ended", r.now)
			}
			s.Ended, s.Finished = at(r.now), time.Time{}
			cs := append(s.Conditions(true), verdict.Condition{Code: verdict.Interrupted, Text: "a signal stopped the run"})
			for _, c := range cs {
				if c.Code.Verdict() == verdict.Fail {
					t.Errorf("condition %v gives a fail verdict", c)
				}
			}
			var obs []verdict.Observation
			for x := -10.0; x < r.now; x += 0.5 {
				obs = append(obs, verdict.Observation{Written: at(x), T: at(x), OK: true, N: 1})
			}
			d := verdict.Decide(verdict.Timeline{
				Windows: []verdict.Window{
					{Name: verdict.Steady, Start: at(-10), End: t0},
					{Name: verdict.Deploy, Start: t0, End: at(r.now), Conditions: cs},
					{Name: verdict.Rollback},
				},
				Observations: obs, Silence: 5 * time.Second, Now: at(r.now),
			})
			var decided verdict.Code
			for _, rs := range d.Reasons {
				if rs.Decided {
					decided = rs.Code
				}
			}
			if d.Verdict != verdict.NotVerified || decided != verdict.Interrupted {
				t.Errorf("verdict %s decided by %s from conditions %v; want not-verified decided by interrupted",
					d.Verdict, decided, cs)
			}
		})
	}
}

// Pods of a workload the selector leaves out are not counted, so a deploy that
// replaces only that workload's pods replaced no pod (review row for R1 and
// R1b).
func TestCountsLeaveOutUnwatchedPods(t *testing.T) {
	o := options()
	o.Selector = labels.SelectorFromSet(labels.Set{"app": "orders"})
	redis := deployment("redis", 1, 1, 1, 1, 1, 1)
	redis.Labels = map[string]string{"app": "redis"}
	r1 := pod("redis-a-1", "r1", "ReplicaSet", "redis-a", true, false)
	r1Stopping := pod("redis-a-1", "r1", "ReplicaSet", "redis-a", true, true)
	before := append(base(), redis, replicaSet("redis-a", "redis"), r1)
	s, ended := feed(o, before, []ev{
		exited(1),
		set(2, pod("redis-a-2", "r2", "ReplicaSet", "redis-a", true, false)),
		set(3, r1Stopping),
		gone(4, r1Stopping),
	}).State(at(30))
	if !ended || s.Stalled {
		t.Fatalf("State(+30 s): ended %v, stalled %v; want ended and not stalled", ended, s.Stalled)
	}
	want := []Count{{Workload: orders, AtStart: 2}}
	if diff := cmp.Diff(want, s.Workloads, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("counts (-want +got):\n%s", diff)
	}
	noPod := []verdict.Condition{{Code: verdict.NoPodReplaced,
		Text: "the transition finished and replaced no pod: it changed nothing the test can judge"}}
	if diff := cmp.Diff(noPod, s.Conditions(true)); diff != "" {
		t.Errorf("Conditions(true) (-want +got):\n%s", diff)
	}
}

// A workload watched at the start that stops matching and then updates with
// OnDelete is still listed: it stays watched (T25-2) (review row for R2).
func TestUnsupportedAfterItStopsMatching(t *testing.T) {
	o := options()
	o.Selector = labels.SelectorFromSet(labels.Set{"app": "orders"})
	web := statefulSet("web", 1, 1, 1, 1, 1, appsv1.RollingUpdateStatefulSetStrategyType)
	web.Labels = map[string]string{"app": "orders"}
	webOnDelete := statefulSet("web", 2, 2, 1, 1, 1, appsv1.OnDeleteStatefulSetStrategyType)
	webOnDelete.Labels = map[string]string{"app": "other"}
	s, ended := feed(o, append(base(), web), append([]ev{set(0.1, webOnDelete)}, rollingUpdate()...)).State(at(27.1))
	if !ended || s.Stalled {
		t.Fatalf("State(+27.1 s): ended %v, stalled %v; want ended and not stalled", ended, s.Stalled)
	}
	want := []Workload{{Kind: "statefulset", Name: "web"}}
	if diff := cmp.Diff(want, s.Unsupported); diff != "" {
		t.Errorf("Unsupported (-want +got):\n%s", diff)
	}
}

func TestInstallStateFromItsRecords(t *testing.T) {
	withImage := func(p *corev1.Pod, id string) *corev1.Pod {
		p.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "app", ImageID: id}}
		return p
	}
	liveA1 := withImage(pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, false), "rollproof.test/orders@sha256:one")
	liveA2 := withImage(pod("orders-a-2", "a2", "ReplicaSet", "orders-a", true, false), "rollproof.test/orders@sha256:two")
	stoppingA1 := withImage(pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, true), "rollproof.test/orders@sha256:one")
	installed := []ev{set(1, deployment("orders", 1, 1, 2, 2, 2, 2)), set(1, replicaSet("orders-a", "orders")),
		set(1, liveA1), set(1, liveA2)}
	for _, r := range []struct {
		name        string
		events      []ev
		exited, now float64
		ready       bool
		install     Install
		code        verdict.Code // empty: no error
		text        string
	}{
		{name: "a pod that is stopping holds the install",
			events: append(installed, set(2, stoppingA1)), exited: 3, now: 3},
		{name: "ready once that pod is gone, with the digests of the pods left",
			events: append(installed, set(2, stoppingA1), gone(4, stoppingA1)), exited: 3, now: 4, ready: true,
			install: Install{Ready: at(4), Workloads: []Count{{Workload: orders, Digests: []string{"sha256:two"}}}}},
		{name: "the timeout names a pod that is stopping",
			events: append(installed, set(2, stoppingA1)), exited: 3, now: 300,
			code: verdict.InstallFailed,
			text: "the install was not ready within limits.timeout\n" +
				"pod orders-a-1 has been stopping for 298.0 s and is not gone"},
		{name: "a deleted workload is not watched",
			events: []ev{set(1, deployment("orders", 1, 1, 2, 2, 2, 2)), gone(2, deployment("orders", 1, 1, 2, 2, 2, 2))},
			exited: 2, now: 17,
			code: verdict.NothingWatched,
			text: "nothing to watch\n" +
				"15 s after the install command returned, namespace orders held no Deployment, StatefulSet or DaemonSet"},
		{name: "a workload that does not roll, while the command runs, is left to the timeout",
			events: append(installed, set(1, daemonSet("agent", 1, 1, 3, 3, 3, appsv1.OnDeleteDaemonSetStrategyType))),
			exited: -1, now: 300,
			code: verdict.InstallFailed,
			text: "the install was not ready within limits.timeout"},
		{name: "nothing watched while the command runs is not yet an error",
			exited: -1, now: 17},
	} {
		t.Run(r.name, func(t *testing.T) {
			tr := NewTracker(options())
			play(tr, r.events)
			var exitedAt time.Time
			if r.exited >= 0 {
				exitedAt = at(r.exited)
			}
			install, ready, err := tr.InstallState(t0, exitedAt, at(r.now))
			if ready != r.ready {
				t.Errorf("ready = %v, want %v", ready, r.ready)
			}
			if diff := cmp.Diff(r.install, install, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("install (-want +got):\n%s", diff)
			}
			var c verdict.Condition
			switch {
			case r.code == "" && err != nil:
				t.Errorf("err = %v, want none", err)
			case r.code != "" && (!errors.As(err, &c) || c.Code != r.code || c.Text != r.text):
				t.Errorf("err = %v, want %s: %s", err, r.code, r.text)
			}
		})
	}
}

// The text of nothing-watched takes the namespace and limits.settle from the
// options, and a nil selector selects every workload.
func TestNothingWatchedNamesTheOptions(t *testing.T) {
	tr := NewTracker(TrackerOptions{Namespace: "shop", Settle: 7500 * time.Millisecond, Timeout: 300 * time.Second, Workers: 3})
	if _, _, err := tr.InstallState(t0, at(1), at(8.4)); err != nil {
		t.Errorf("InstallState(+8.4 s) err = %v, want none before the settle time has passed", err)
	}
	_, _, err := tr.InstallState(t0, at(1), at(8.5))
	want := verdict.Condition{Code: verdict.NothingWatched, Text: "nothing to watch\n" +
		"7.5 s after the install command returned, namespace shop held no Deployment, StatefulSet or DaemonSet"}
	var c verdict.Condition
	if !errors.As(err, &c) || c != want {
		t.Errorf("InstallState(+8.5 s) err = %v, want %v", err, want)
	}
}

// PodsAt answers only for a moment inside a window: not before the first,
// not at a window's end, not between two windows, and not once the last
// window's time has come although nothing has ended it yet.
func TestPodsAtOutsideAnyWindow(t *testing.T) {
	agentX := Workload{Kind: "daemonset", Name: "agent"}
	tr := NewTracker(options())
	for _, obj := range append(base(), daemonSet("agent", 2, 2, 3, 0, 3, appsv1.OnDeleteDaemonSetStrategyType)) {
		tr.Set(obj, at(-3))
	}
	tr.Set(pod("agent-x", "x1", "DaemonSet", "agent", true, true), at(-2)) // stopping, never gone
	tr.Begin(verdict.Deploy, t0)
	tr.CommandExited(at(2)) // finished at +2 s; the window's time comes at +17 s
	for _, r := range []struct {
		at   float64
		want []PodNote // nil: no window holds the moment
	}{
		{-1, nil},
		{16, []PodNote{{Name: "agent-x", Workload: agentX, Old: true, For: 18 * time.Second}}},
		{17, nil},
	} {
		stopping, starting := tr.PodsAt(at(r.at))
		if diff := cmp.Diff(r.want, stopping); diff != "" || starting != nil {
			t.Errorf("PodsAt(+%v s) = %v, %v; want stopping %v and no starting", r.at, stopping, starting, r.want)
		}
	}
	tr.State(at(18))
	tr.Begin(verdict.Rollback, at(20))
	for _, r := range []struct {
		at   float64
		want []PodNote
	}{
		{18, nil},
		{21, []PodNote{{Name: "agent-x", Workload: agentX, Old: true, For: 23 * time.Second}}},
	} {
		stopping, starting := tr.PodsAt(at(r.at))
		if diff := cmp.Diff(r.want, stopping); diff != "" || starting != nil {
			t.Errorf("PodsAt(+%v s) = %v, %v; want stopping %v and no starting", r.at, stopping, starting, r.want)
		}
	}
}

// Only pods of the workloads watched in the transition count; an old pod that
// is not ready is not starting; and a pod that is deleted before it was ready
// is stopping, not starting.
func TestPodsAtWatchedPods(t *testing.T) {
	o := options()
	o.Selector = labels.SelectorFromSet(labels.Set{"app": "orders"})
	before := append(base(), deployment("redis", 1, 1, 1, 1, 1, 1), replicaSet("redis-a", "redis"),
		pod("redis-a-1", "r1", "ReplicaSet", "redis-a", true, true),
		pod("stray", "s1", "ReplicaSet", "unknown", false, false),
		pod("orders-a-3", "a3", "ReplicaSet", "orders-a", false, false))
	tr := feed(o, before, []ev{set(0.2, inc), exited(0.5), set(1, replicaSet("orders-b", "orders")),
		set(2, pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false)),
		set(3, pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, true))})
	stopping, starting := tr.PodsAt(at(2.5))
	want := []PodNote{{Name: "orders-b-1", Workload: orders, For: 500 * time.Millisecond}}
	if len(stopping) != 0 {
		t.Errorf("stopping at +2.5 s = %v, want none: redis is not watched", stopping)
	}
	if diff := cmp.Diff(want, starting); diff != "" {
		t.Errorf("starting at +2.5 s (-want +got):\n%s", diff)
	}
	stopping, starting = tr.PodsAt(at(3.5))
	if diff := cmp.Diff(want, stopping); diff != "" {
		t.Errorf("stopping at +3.5 s (-want +got):\n%s", diff)
	}
	if len(starting) != 0 {
		t.Errorf("starting at +3.5 s = %v, want none: the pod is stopping", starting)
	}
}

// While the window is open, the counts are those at the moment asked: a pod
// created, gone or a workload deleted later is not counted yet.
func TestCountsAtTheMomentAsked(t *testing.T) {
	tr := feed(options(), base(), rollingUpdate())
	for _, r := range []struct {
		at       float64
		replaced bool
		counts   []Count
	}{
		{4.5, false, []Count{{Workload: orders, AtStart: 2, Created: 1}}},
		{9, false, []Count{{Workload: orders, AtStart: 2, Created: 2}}},
		{10, true, []Count{{Workload: orders, AtStart: 2, Replaced: 1, Created: 2, QuickestStop: 5300 * time.Millisecond}}},
	} {
		s, ended := tr.State(at(r.at))
		if ended {
			t.Fatalf("State(+%v s) has ended", r.at)
		}
		if s.Replaced != r.replaced {
			t.Errorf("State(+%v s): Replaced = %v, want %v", r.at, s.Replaced, r.replaced)
		}
		if diff := cmp.Diff(r.counts, s.Workloads, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("counts at +%v s (-want +got):\n%s", r.at, diff)
		}
	}
	s, _ := feed(options(), base(), renamed()).State(at(2.5))
	want := []Count{{Workload: orders, AtStart: 2}, {Workload: Workload{Kind: "deployment", Name: "orders-v2"}, Added: true}}
	if diff := cmp.Diff(want, s.Workloads, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("counts of the rename at +2.5 s (-want +got):\n%s", diff)
	}
}

// decided is the verdict that verdict.Decide gives a run whose deploy window
// is s, with a clean steady window and a probe that passed every half second,
// and the code that decided it.
func decided(s Transition) (verdict.Verdict, verdict.Code) {
	var obs []verdict.Observation
	for x := -10.0; at(x).Before(s.Ended); x += 0.5 {
		obs = append(obs, verdict.Observation{Written: at(x), T: at(x), OK: true, N: 1})
	}
	d := verdict.Decide(verdict.Timeline{
		Windows: []verdict.Window{
			{Name: verdict.Steady, Start: at(-10), End: t0},
			{Name: verdict.Deploy, Start: t0, Finished: s.Finished, End: s.Ended, Conditions: s.Conditions(true)},
			{Name: verdict.Rollback},
		},
		Observations: obs, Silence: 5 * time.Second, Now: s.Ended,
	})
	for _, r := range d.Reasons {
		if r.Decided {
			return d.Verdict, r.Code
		}
	}
	return d.Verdict, ""
}

// The watch of the cluster missed changes (T27-1, widened). The first moment
// at or after the transition's start is its Missed, also when it is told
// before Begin; a moment before the start, after the window ended, or at its
// end, is not. Conditions gives an InternalError of its own for it, in place
// of a stall's fail codes (T27-1 amended), so neither a stall nor a finished
// window with a break in it fails or passes: it is not verified.
func TestMissed(t *testing.T) {
	missed := func(s float64) verdict.Condition {
		return verdict.Condition{Code: verdict.InternalError,
			Text: fmt.Sprintf("the watch of the cluster missed changes %.1f s after the transition began", s)}
	}
	for _, r := range []struct {
		name    string
		tr      func() *Tracker
		missed  time.Time
		cs      []verdict.Condition
		verdict verdict.Verdict
		code    verdict.Code
	}{
		{"the first of two moments in the window", func() *Tracker {
			tr := feed(options(), base(), rollingUpdate())
			tr.Missed(at(13))
			tr.Missed(at(20))
			return tr
		}, at(13), []verdict.Condition{missed(13)}, verdict.NotVerified, verdict.InternalError},
		{"before the transition began", func() *Tracker {
			tr := NewTracker(options())
			for _, obj := range base() {
				tr.Set(obj, at(-2))
			}
			tr.Missed(at(-1))
			tr.Begin(verdict.Deploy, t0)
			play(tr, rollingUpdate())
			return tr
		}, time.Time{}, nil, verdict.Pass, ""},
		{"at the transition's start, told before its Begin", func() *Tracker {
			tr := NewTracker(options())
			for _, obj := range base() {
				tr.Set(obj, t0)
			}
			tr.Missed(t0)
			tr.Begin(verdict.Deploy, t0)
			play(tr, rollingUpdate())
			return tr
		}, t0, []verdict.Condition{missed(0)}, verdict.NotVerified, verdict.InternalError},
		{"the first of the moments after the start, told before its Begin", func() *Tracker {
			tr := NewTracker(options())
			for _, obj := range base() {
				tr.Set(obj, at(-2))
			}
			tr.Missed(at(-1))
			tr.Missed(at(0.1))
			tr.Missed(at(0.15))
			tr.Begin(verdict.Deploy, t0)
			play(tr, rollingUpdate())
			return tr
		}, at(0.1), []verdict.Condition{missed(0.1)}, verdict.NotVerified, verdict.InternalError},
		{"after the window ended", func() *Tracker {
			tr := feed(options(), base(), rollingUpdate())
			tr.State(at(30))
			tr.Missed(at(31))
			return tr
		}, time.Time{}, nil, verdict.Pass, ""},
		{"at the end of the window, which belongs to no window", func() *Tracker {
			tr := feed(options(), base(), rollingUpdate())
			tr.Missed(at(27.1))
			return tr
		}, time.Time{}, nil, verdict.Pass, ""},
		// Ruling T27-1 amended: with a break in the window, the view at the
		// limit may be stale, so a stall gives no fail code. The value before
		// the amendment was [transition-stalled, the missed InternalError], fail.
		{"in a stall", func() *Tracker {
			tr := feed(options(), base(), []ev{set(1, inc), exited(2)})
			tr.Missed(at(3))
			return tr
		}, at(3), []verdict.Condition{missed(3)}, verdict.NotVerified, verdict.InternalError},
		// The review's case: the settle time runs past the limit, and the list
		// after a break tells a pod deleted, and its replacement, after it.
		{"a stall decided by a change that a list again told past the limit", func() *Tracker {
			var events []ev
			for _, e := range rollingUpdate() {
				e.s += 278
				events = append(events, e)
			}
			tr := feed(options(), base(), events)
			tr.Missed(at(301))
			tr.Gone(pod("orders-b-1", "b1", "ReplicaSet", "orders-b", true, false), at(301))
			tr.Set(pod("orders-b-3", "b3", "ReplicaSet", "orders-b", true, false), at(301))
			return tr
		}, at(301), []verdict.Condition{missed(301)}, verdict.NotVerified, verdict.InternalError},
	} {
		t.Run(r.name, func(t *testing.T) {
			s, ended := r.tr().State(at(400))
			if !ended {
				t.Fatal("the window has not ended at +400 s")
			}
			if !s.Missed.Equal(r.missed) {
				t.Errorf("Missed = %v, want %v", s.Missed, r.missed)
			}
			if diff := cmp.Diff(r.cs, s.Conditions(true), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("conditions (-want +got):\n%s", diff)
			}
			if v, c := decided(s); v != r.verdict || c != r.code {
				t.Errorf("verdict %s decided by %q; want %s decided by %q", v, c, r.verdict, r.code)
			}
		})
	}
}

// A moment told after a window ended changes that window in no case, and is
// the next transition's Missed only when it is at or after that transition's
// start; the first such moment is (T27-1, widened).
func TestMissedAfterAWindowEnded(t *testing.T) {
	for _, r := range []struct {
		name    string
		moments []float64
		want    time.Time
	}{
		{"before the next start", []float64{31}, time.Time{}},
		{"before and after the next start", []float64{31, 33, 34}, at(33)},
	} {
		t.Run(r.name, func(t *testing.T) {
			tr := feed(options(), base(), rollingUpdate())
			if _, ended := tr.State(at(30)); !ended {
				t.Fatal("the deploy's window has not ended at +30 s")
			}
			for _, m := range r.moments {
				tr.Missed(at(m))
			}
			if deploy, _ := tr.State(at(35)); deploy.Name != verdict.Deploy || !deploy.Missed.IsZero() {
				t.Errorf("the deploy after the moments: %s, Missed %v; want the deploy, unchanged", deploy.Name, deploy.Missed)
			}
			tr.Begin(verdict.Rollback, at(32))
			tr.CommandExited(at(35))
			rollback, ended := tr.State(at(60))
			if !ended || !rollback.Missed.Equal(r.want) {
				t.Errorf("rollback: ended %v, Missed %v; want ended, Missed %v", ended, rollback.Missed, r.want)
			}
		})
	}
}

// What the Tracker makes of the Watcher's watches going down and coming up
// again (T27-4): a window in which a watch was down at any moment, from its
// start to its end, missed changes from the first such moment, or from its
// start. A stretch down that ended before the start counts for nothing.
// Before T27-4, "down and up again inside the window" was "down and up again
// before the window's end" and gave no Missed; the rows that told watchBroke
// now tell the same moments without it, with the same values.
func TestWatchDown(t *testing.T) {
	before := func() *Tracker {
		tr := NewTracker(options())
		for _, obj := range base() {
			tr.Set(obj, at(-3))
		}
		return tr
	}
	for _, r := range []struct {
		name   string
		tr     func() *Tracker
		missed time.Time
	}{
		{"down in the settle time, at the window's end", func() *Tracker {
			tr := feed(options(), base(), rollingUpdate())
			tr.watchDown("Pod", at(20))
			return tr
		}, at(20)},
		{"down and up again inside the window", func() *Tracker {
			tr := feed(options(), base(), rollingUpdate())
			tr.watchDown("Pod", at(20))
			tr.watchUp("Pod", at(21))
			return tr
		}, at(20)},
		{"down at the window's end, which belongs to no window", func() *Tracker {
			tr := feed(options(), base(), rollingUpdate())
			tr.watchDown("Pod", at(27.1))
			return tr
		}, time.Time{}},
		{"down from before the start to the end", func() *Tracker {
			tr := before()
			tr.watchDown("Pod", at(-1))
			tr.Begin(verdict.Deploy, t0)
			play(tr, rollingUpdate())
			return tr
		}, t0},
		{"down from before the start, up in the window", func() *Tracker {
			tr := before()
			tr.watchDown("Pod", at(-1))
			tr.Begin(verdict.Deploy, t0)
			tr.watchUp("Pod", at(0.15))
			play(tr, rollingUpdate())
			return tr
		}, t0},
		{"down from before the start, up after it, told before Begin", func() *Tracker {
			tr := before()
			tr.watchDown("Pod", at(-1))
			tr.watchUp("Pod", at(0.15))
			tr.Begin(verdict.Deploy, t0)
			play(tr, rollingUpdate())
			return tr
		}, t0},
		{"a stretch down that ended before the start", func() *Tracker {
			tr := before()
			tr.watchDown("Pod", at(-2))
			tr.watchUp("Pod", at(-1))
			tr.Begin(verdict.Deploy, t0)
			play(tr, rollingUpdate())
			return tr
		}, time.Time{}},
		{"down twice in the window, timed when it first went down", func() *Tracker {
			tr := feed(options(), base(), rollingUpdate())
			tr.watchDown("Pod", at(13))
			tr.watchDown("Pod", at(14))
			tr.watchUp("Pod", at(15))
			return tr
		}, at(13)},
		{"down in the window, timed when it went down", func() *Tracker {
			tr := feed(options(), base(), rollingUpdate())
			tr.watchDown("Pod", at(14))
			tr.watchUp("Pod", at(15))
			return tr
		}, at(14)},
	} {
		t.Run(r.name, func(t *testing.T) {
			s, ended := r.tr().State(at(400))
			if !ended || !s.Missed.Equal(r.missed) {
				t.Errorf("ended %v, Missed %v; want ended, Missed %v", ended, s.Missed, r.missed)
			}
		})
	}
}

// The review's gap across the start (T27-4): the pods' watch is down across
// the transition's start, and the changes it held back, a1 deleted and a3
// made before the start, are told when it is up again, after the start. The
// deploy changed no pod template. The window is not verified and never
// passes, whether Begin is told before the watch is up or after.
func TestAGapAcrossTheStart(t *testing.T) {
	replay := func(tr *Tracker, s float64) {
		tr.Gone(pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, true), at(s))
		tr.Set(pod("orders-a-3", "a3", "ReplicaSet", "orders-a", true, false), at(s))
	}
	for _, r := range []struct {
		name string
		tell func(tr *Tracker)
	}{
		{"Begin told after the watch is up", func(tr *Tracker) {
			tr.watchDown("Pod", at(-1))
			tr.watchUp("Pod", at(0.5))
			replay(tr, 0.5)
			tr.Begin(verdict.Deploy, t0)
		}},
		{"Begin told while the watch is down", func(tr *Tracker) {
			tr.watchDown("Pod", at(-1))
			tr.Begin(verdict.Deploy, t0)
			tr.watchUp("Pod", at(0.5))
			replay(tr, 0.5)
		}},
	} {
		t.Run(r.name, func(t *testing.T) {
			tr := NewTracker(options())
			for _, obj := range base() {
				tr.Set(obj, at(-3))
			}
			r.tell(tr)
			tr.CommandExited(at(1))
			s, ended := tr.State(at(60))
			if v, code := decided(s); !ended || !s.Missed.Equal(t0) || v != verdict.NotVerified || code != verdict.InternalError {
				t.Errorf("ended %v, Missed %v, verdict %s decided by %q; want Missed at the start, not verified by internal-error",
					ended, s.Missed, v, code)
			}
		})
	}
}

// The review's renewal across the limit (T27-4): the settle time runs past
// the limit, and the pods' watch is down from +297 s to +301 s. The changes it
// held back, told at +301 s, make a stall there. The window is not verified,
// and never fails.
func TestARenewalAcrossTheLimit(t *testing.T) {
	var events []ev
	for _, e := range rollingUpdate() {
		e.s += 278
		events = append(events, e)
	}
	tr := feed(options(), base(), events)
	tr.watchDown("Pod", at(297))
	tr.watchUp("Pod", at(301))
	tr.Gone(pod("orders-b-1", "b1", "ReplicaSet", "orders-b", true, false), at(301))
	tr.Set(pod("orders-b-3", "b3", "ReplicaSet", "orders-b", true, false), at(301))
	s, _ := tr.State(at(400))
	if v, code := decided(s); !s.Missed.Equal(at(297)) || v != verdict.NotVerified || code != verdict.InternalError {
		t.Errorf("stalled %v, Missed %v, verdict %s decided by %q; want Missed at +297 s, not verified by internal-error",
			s.Stalled, s.Missed, v, code)
	}
}
