package watch

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8swatch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	discoveryv1client "k8s.io/client-go/kubernetes/typed/discovery/v1"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"rollproof/internal/verdict"
)

// fakeClock is the Watcher's clock in these tests; only the test moves it.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// startWatcher starts a Watcher on namespace orders of a fake client that
// holds objs, with settle 15 s and timeout 300 s, and stops it when the test
// ends. It returns once the fake client serves all five watches, so that no
// change the test makes falls between an informer's list and its watch.
func startWatcher(t *testing.T, service *types.NamespacedName, objs ...runtime.Object) (*Watcher, *fake.Clientset, *fakeClock) {
	t.Helper()
	client := fake.NewClientset(objs...)
	var watches atomic.Int32
	client.PrependWatchReactor("*", func(k8stesting.Action) (bool, k8swatch.Interface, error) {
		watches.Add(1)
		return false, nil, nil
	})
	clock := &fakeClock{now: t0}
	w, err := Start(t.Context(), client, Options{Namespace: "orders", Settle: 15 * time.Second,
		Timeout: 300 * time.Second, Workers: 3, Service: service, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Stop)
	for deadline := time.Now().Add(5 * time.Second); watches.Load() < 5; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the informers did not start their watches")
		}
	}
	return w, client, clock
}

// drive moves the fake clock on by one second every 10 ms of real time until
// res delivers, and fails the test after 10 s of real time.
func drive[T any](t *testing.T, c *fakeClock, res <-chan T) T {
	t.Helper()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case v := <-res:
			return v
		case <-tick.C:
			c.Advance(time.Second)
		case <-deadline:
			t.Fatal("no result within 10 s of real time")
		}
	}
}

// until waits, for at most 5 s of real time, until cond holds for the
// transition so far, as the Watcher's Tracker gives it at the fake clock's time.
func until(t *testing.T, w *Watcher, c *fakeClock, cond func(Transition) bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		w.mu.Lock()
		s, _ := w.tr.State(c.Now())
		w.mu.Unlock()
		if cond(s) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("not reached within 5 s; the transition so far: %+v", s)
		}
	}
}

func closed() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}

// update stores a Deployment or a Pod in the fake client, whole, status included.
func update(t *testing.T, client *fake.Clientset, obj runtime.Object) {
	t.Helper()
	var err error
	switch o := obj.(type) {
	case *appsv1.Deployment:
		_, err = client.AppsV1().Deployments("orders").Update(t.Context(), o, metav1.UpdateOptions{})
	case *corev1.Pod:
		_, err = client.CoreV1().Pods("orders").Update(t.Context(), o, metav1.UpdateOptions{})
	default:
		t.Fatalf("update: %T", obj)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestStartSeesWhatExists(t *testing.T) {
	w, _, clock := startWatcher(t, nil, base()...)
	w.mu.Lock()
	install, ready, err := w.tr.InstallState(t0, t0, clock.Now())
	w.mu.Unlock()
	if err != nil || !ready || len(install.Workloads) != 1 || install.Workloads[0].Workload != orders {
		t.Fatalf("right after Start: %+v, ready %v, err %v; want deployment/orders, ready", install, ready, err)
	}
}

func TestStartRejectsASelector(t *testing.T) {
	_, err := Start(t.Context(), fake.NewClientset(), Options{Namespace: "orders", Selector: "app in (",
		Settle: 15 * time.Second, Timeout: 300 * time.Second, Workers: 3, Now: time.Now})
	var c verdict.Condition
	if err == nil || errors.As(err, &c) {
		t.Fatalf("err = %v; want a plain error", err)
	}
}

func TestInstallReady(t *testing.T) {
	t.Run("ready once the Deployment is complete", func(t *testing.T) {
		objs := base()
		objs[0] = deployment("orders", 1, 0, 2, 0, 0, 0)
		w, client, _ := startWatcher(t, nil, objs...)
		res := make(chan error, 1)
		go func() {
			_, err := w.InstallReady(t.Context(), t0, closed())
			res <- err
		}()
		select {
		case err := <-res:
			t.Fatalf("InstallReady returned (%v) before the Deployment was complete", err)
		case <-time.After(300 * time.Millisecond):
		}
		update(t, client, deployment("orders", 1, 1, 2, 2, 2, 2))
		select {
		case err := <-res:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("InstallReady did not return once the Deployment was complete")
		}
	})
	t.Run("nothing to watch", func(t *testing.T) {
		w, _, clock := startWatcher(t, nil)
		res := make(chan error, 1)
		go func() {
			_, err := w.InstallReady(t.Context(), t0, closed())
			res <- err
		}()
		err := drive(t, clock, res)
		var c verdict.Condition
		if !errors.As(err, &c) || c.Code != verdict.NothingWatched {
			t.Fatalf("err = %v, want nothing-watched", err)
		}
		if got := clock.Now().Sub(t0); got < 15*time.Second {
			t.Errorf("nothing-watched after %v of the fake clock, before the settle time", got)
		}
	})
}

func TestTransition(t *testing.T) {
	w, client, clock := startWatcher(t, nil, base()...)
	ctx := t.Context()
	exited := make(chan struct{})
	res := make(chan Transition, 1)
	go func() { res <- w.Transition(ctx, verdict.Deploy, clock.Now(), exited) }()
	until(t, w, clock, func(s Transition) bool { return s.Name == verdict.Deploy })
	clock.Advance(time.Second)

	update(t, client, deployment("orders", 2, 1, 2, 2, 2, 2))
	if _, err := client.AppsV1().ReplicaSets("orders").Create(ctx, replicaSet("orders-b", "orders"), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []*corev1.Pod{
		pod("orders-b-1", "b1", "ReplicaSet", "orders-b", true, false),
		pod("orders-b-2", "b2", "ReplicaSet", "orders-b", true, false),
	} {
		if _, err := client.CoreV1().Pods("orders").Create(ctx, p, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, old := range []*corev1.Pod{
		pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, true),
		pod("orders-a-2", "a2", "ReplicaSet", "orders-a", true, true),
	} {
		update(t, client, old)
		if err := client.CoreV1().Pods("orders").Delete(ctx, old.Name, metav1.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	update(t, client, deployment("orders", 2, 2, 2, 2, 2, 2))
	until(t, w, clock, func(s Transition) bool {
		return len(s.Workloads) == 1 && s.Workloads[0].Replaced == 2 && s.Workloads[0].Created == 2
	})
	close(exited)

	tr := drive(t, clock, res)
	if tr.Stalled || !tr.Replaced {
		t.Fatalf("stalled %v, replaced %v; want a finished transition that replaced pods", tr.Stalled, tr.Replaced)
	}
	if got := tr.Ended.Sub(tr.Finished); got != 15*time.Second {
		t.Errorf("the window ended %v after the transition finished, want the settle time", got)
	}
	want := []Count{{Workload: orders, AtStart: 2, Replaced: 2, Created: 2, Probed: "not known"}}
	if diff := cmp.Diff(want, tr.Workloads, cmpopts.IgnoreFields(Count{}, "QuickestStop"), cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("counts (-want +got):\n%s", diff)
	}
}

func TestTransitionCancelled(t *testing.T) {
	w, _, clock := startWatcher(t, nil, base()...)
	ctx, cancel := context.WithCancel(t.Context())
	res := make(chan Transition, 1)
	go func() { res <- w.Transition(ctx, verdict.Deploy, clock.Now(), make(chan struct{})) }()
	until(t, w, clock, func(s Transition) bool { return s.Name == verdict.Deploy })
	clock.Advance(3 * time.Second)
	cancel()
	select {
	case tr := <-res:
		if !tr.Ended.Equal(at(3)) || tr.Stalled || !tr.Finished.IsZero() {
			t.Errorf("ended %v, stalled %v, finished %v; want ended at +3 s, not stalled, not finished",
				tr.Ended, tr.Stalled, tr.Finished)
		}
	case <-time.After(time.Second):
		t.Fatal("Transition did not return within 1 s of the cancel")
	}
}

// probedObjects is the namespace of TestProbed: two workloads, of which one is behind the
// Service orders.
func probedObjects() []runtime.Object {
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-x7k2p", Namespace: "orders",
			Labels: map[string]string{discoveryv1.LabelServiceName: "orders"}},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{
			{Addresses: []string{"10.244.1.5"},
				TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "orders", Name: "orders-a-1", UID: "a1"}},
			{Addresses: []string{"10.244.2.7"},
				TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "orders", Name: "orders-a-2", UID: "a2"}},
		},
	}
	return append(base(), slice,
		deployment("batch", 1, 1, 1, 1, 1, 1),
		replicaSet("batch-a", "batch"),
		pod("batch-a-1", "k1", "ReplicaSet", "batch-a", true, false))
}

// probedValues runs one deploy transition on w and gives each workload's Probed.
func probedValues(t *testing.T, w *Watcher, clock *fakeClock) map[string]string {
	t.Helper()
	res := make(chan Transition, 1)
	go func() { res <- w.Transition(t.Context(), verdict.Deploy, clock.Now(), closed()) }()
	got := map[string]string{}
	for _, c := range drive(t, clock, res).Workloads {
		got[c.Workload.Name] = c.Probed
	}
	return got
}

func TestProbed(t *testing.T) {
	objs := probedObjects()
	for _, r := range []struct {
		name          string
		service       *types.NamespacedName
		batch, orders string
	}{
		{"the Service's endpoints are pods of orders", &types.NamespacedName{Namespace: "orders", Name: "orders"}, "no", "yes"},
		{"no Service", nil, "not known", "not known"},
		{"a Service in another namespace", &types.NamespacedName{Namespace: "shop", Name: "orders"}, "no", "no"},
	} {
		t.Run(r.name, func(t *testing.T) {
			w, _, clock := startWatcher(t, r.service, objs...)
			got := probedValues(t, w, clock)
			if diff := cmp.Diff(map[string]string{"batch": r.batch, "orders": r.orders}, got); diff != "" {
				t.Errorf("Probed (-want +got):\n%s", diff)
			}
		})
	}
}

// TestUseService pins the Service that UseService names: a Watcher started with none, and told
// the Service before its first Transition, reads the endpoints that a Watcher started with that
// Service reads.
func TestUseService(t *testing.T) {
	service := &types.NamespacedName{Namespace: "orders", Name: "orders"}
	started, _, clock := startWatcher(t, service, probedObjects()...)
	want := probedValues(t, started, clock)

	told, _, clock := startWatcher(t, nil, probedObjects()...)
	told.UseService(service)
	got := probedValues(t, told, clock)

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Probed (-want +got):\n%s", diff)
	}
}

func TestStopTwice(t *testing.T) {
	w, _, _ := startWatcher(t, nil, base()...)
	w.Stop()
	w.Stop()
	select {
	case <-w.done:
	case <-time.After(5 * time.Second):
		t.Fatal("a goroutine of the Watcher still runs after Stop")
	}
}

// Start returns only when the Tracker has been told what exists, even when a
// list is slow; the brief's test waits for the watches first.
func TestStartWaitsForWhatExists(t *testing.T) {
	client := fake.NewClientset(base()...)
	client.PrependReactor("list", "deployments", func(k8stesting.Action) (bool, runtime.Object, error) {
		time.Sleep(200 * time.Millisecond)
		return false, nil, nil
	})
	w, err := Start(t.Context(), client, Options{Namespace: "orders", Settle: 15 * time.Second,
		Timeout: 300 * time.Second, Workers: 3, Now: func() time.Time { return t0 }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Stop)
	w.mu.Lock()
	install, ready, err := w.tr.InstallState(t0, t0, t0)
	w.mu.Unlock()
	if err != nil || !ready || len(install.Workloads) != 1 {
		t.Fatalf("when Start returned: %+v, ready %v, err %v; want deployment/orders, ready", install, ready, err)
	}
}

// After Stop no informer runs: a pod created then is never told.
func TestNothingIsToldAfterStop(t *testing.T) {
	w, client, _ := startWatcher(t, nil, base()...)
	w.Stop()
	if _, err := client.CoreV1().Pods("orders").Create(t.Context(),
		pod("orders-a-3", "a3", "ReplicaSet", "orders-a", true, false), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	w.mu.Lock()
	_, told := w.tr.podWorkloadByUID("a3")
	w.mu.Unlock()
	if told {
		t.Error("a pod created after Stop was told to the Tracker")
	}
}

// Stop returns only when no handler of the Watcher runs: here a handler waits
// for the lock that the test holds.
func TestStopWaitsForAHandler(t *testing.T) {
	w, client, _ := startWatcher(t, nil, base()...)
	w.mu.Lock()
	if _, err := client.CoreV1().Pods("orders").Create(t.Context(),
		pod("orders-a-3", "a3", "ReplicaSet", "orders-a", true, false), metav1.CreateOptions{}); err != nil {
		w.mu.Unlock()
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // the pod's handler now waits for the lock
	stopped := make(chan struct{})
	go func() {
		w.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
		w.mu.Unlock()
		t.Fatal("Stop returned while a handler still ran")
	case <-time.After(300 * time.Millisecond):
	}
	w.mu.Unlock()
	receive(t, stopped)
}

// receive returns what res delivers, and fails the test after 2 s of real time.
func receive[T any](t *testing.T, res <-chan T) T {
	t.Helper()
	select {
	case v := <-res:
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("no result within 2 s of real time")
	}
	panic("unreachable")
}

// A ctx that ends in the settle time cuts the window short (T26-3): Finished
// is zero and Ended is the cancel, so Conditions gives the InternalError and
// the window is not verified, never passed.
func TestTransitionCancelledInTheSettleTime(t *testing.T) {
	w, _, clock := startWatcher(t, nil, base()...)
	ctx, cancel := context.WithCancel(t.Context())
	res := make(chan Transition, 1)
	go func() { res <- w.Transition(ctx, verdict.Deploy, clock.Now(), closed()) }()
	until(t, w, clock, func(s Transition) bool { return !s.Finished.IsZero() })
	clock.Advance(5 * time.Second)
	cancel()
	tr := receive(t, res)
	if !tr.Finished.IsZero() || !tr.Ended.Equal(at(5)) || tr.Stalled {
		t.Errorf("finished %v, ended %v, stalled %v; want not finished, ended at +5 s, not stalled",
			tr.Finished, tr.Ended, tr.Stalled)
	}
	want := []verdict.Condition{{Code: verdict.InternalError, Text: "the transition was judged before its window ended"}}
	if diff := cmp.Diff(want, tr.Conditions(true)); diff != "" {
		t.Errorf("conditions (-want +got):\n%s", diff)
	}
}

// A ctx that ends after the window's end changes nothing (T26-3): the window
// ended at the end of its settle time, before the cancel.
func TestTransitionCancelledAfterItsWindowEnded(t *testing.T) {
	w, _, clock := startWatcher(t, nil, base()...)
	ctx, cancel := context.WithCancel(t.Context())
	res := make(chan Transition, 1)
	go func() { res <- w.Transition(ctx, verdict.Deploy, clock.Now(), closed()) }()
	until(t, w, clock, func(s Transition) bool { return !s.Finished.IsZero() })
	clock.Advance(20 * time.Second)
	cancel()
	tr := receive(t, res)
	if !tr.Finished.Equal(t0) || !tr.Ended.Equal(at(15)) {
		t.Errorf("finished %v, ended %v; want finished at +0 s and ended at +15 s", tr.Finished, tr.Ended)
	}
	if cs := tr.Conditions(true); len(cs) != 1 || cs[0].Code != verdict.NoPodReplaced {
		t.Errorf("conditions %v; want no-pod-replaced alone", cs)
	}
}

// Begin takes the began it is given, not the Watcher's clock, so that a
// window begins where the one before it ended and no moment falls between.
func TestTransitionsBackToBack(t *testing.T) {
	w, _, clock := startWatcher(t, nil, base()...)
	ctx := t.Context()
	res := make(chan Transition, 1)
	go func() { res <- w.Transition(ctx, verdict.Deploy, clock.Now(), closed()) }()
	deploy := drive(t, clock, res)
	clock.Advance(3 * time.Second)
	go func() { res <- w.Transition(ctx, verdict.Rollback, deploy.Ended, closed()) }()
	rollback := drive(t, clock, res)
	if !rollback.Began.Equal(deploy.Ended) {
		t.Errorf("the rollback began at %v, want where the deploy ended, %v", rollback.Began, deploy.Ended)
	}
	if len(rollback.Workloads) != 1 || rollback.Workloads[0].AtStart != 2 {
		t.Errorf("rollback counts %+v; want deployment/orders with its 2 pods at the start", rollback.Workloads)
	}
}

// A rollback whose pod template needs no new pod changes the Deployment's
// generation and nothing else (p05, run 4). No pod event comes, and the
// window still ends: a finished transition that replaced no pod, not a stall.
func TestTransitionThatNeedsNoNewPod(t *testing.T) {
	w, client, clock := startWatcher(t, nil, base()...)
	exited := make(chan struct{})
	res := make(chan Transition, 1)
	go func() { res <- w.Transition(t.Context(), verdict.Rollback, clock.Now(), exited) }()
	until(t, w, clock, func(s Transition) bool { return s.Name == verdict.Rollback })
	update(t, client, deployment("orders", 2, 1, 2, 2, 2, 2))
	update(t, client, deployment("orders", 2, 2, 2, 2, 2, 2))
	close(exited)
	tr := drive(t, clock, res)
	if tr.Stalled || tr.Finished.IsZero() || tr.Ended.Sub(tr.Finished) != 15*time.Second || tr.Replaced {
		t.Fatalf("stalled %v, finished %v, ended %v, replaced %v; want a finished window that replaced no pod",
			tr.Stalled, tr.Finished, tr.Ended, tr.Replaced)
	}
	if cs := tr.Conditions(true); len(cs) != 1 || cs[0].Code != verdict.NoPodReplaced {
		t.Errorf("conditions %v; want no-pod-replaced alone", cs)
	}
}

// What an informer tells after it lost its watch and listed again: a
// deletion whose final state is not known (a DeletedFinalStateUnknown), and
// an object made again under its old name, seen as an update whose UID
// changed. Each is told in the settle time of a transition, and starts it
// again with what the Tracker makes of it.
func TestWhatAnInformerFindsAtAListAgain(t *testing.T) {
	a1 := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, false)
	newOrders := deployment("orders", 1, 1, 2, 2, 2, 2)
	newOrders.UID = "uid-orders-2"
	for _, r := range []struct {
		name string
		tell func(w *Watcher)
		want string
	}{
		{"a pod's deletion with its last state", func(w *Watcher) {
			handler{w: w, kind: "Pod"}.OnDelete(cache.DeletedFinalStateUnknown{Key: "orders/orders-a-1", Obj: a1})
		}, "pod orders-a-1 of deployment/orders was deleted 0.0 s after the transition began"},
		{"a pod's deletion with no state: the object last told under its key", func(w *Watcher) {
			handler{w: w, kind: "Pod"}.OnDelete(cache.DeletedFinalStateUnknown{Key: "orders/orders-a-1"})
		}, "pod orders-a-1 of deployment/orders was deleted 0.0 s after the transition began"},
		{"the deletion of a pod never told, with its last state", func(w *Watcher) {
			handler{w: w, kind: "Pod"}.OnDelete(cache.DeletedFinalStateUnknown{Key: "orders/orders-a-3",
				Obj: pod("orders-a-3", "a3", "ReplicaSet", "orders-a", true, false)})
		}, "pod orders-a-3 of deployment/orders was deleted 0.0 s after the transition began"},
		{"a Deployment's deletion with no state", func(w *Watcher) {
			handler{w: w, kind: "Deployment"}.OnDelete(cache.DeletedFinalStateUnknown{Key: "orders/orders"})
		}, "deployment/orders was deleted 0.0 s after the transition began"},
		{"a pod made again under its name", func(w *Watcher) {
			again := pod("orders-a-1", "a1x", "ReplicaSet", "orders-a", true, false)
			handler{w: w, kind: "Pod"}.OnUpdate(a1, again)
		}, "pod orders-a-1 of deployment/orders was deleted 0.0 s after the transition began"},
		{"a Deployment made again under its name", func(w *Watcher) {
			handler{w: w, kind: "Deployment"}.OnUpdate(base()[0], newOrders)
		}, "deployment/orders was deleted 0.0 s after the transition began"},
	} {
		t.Run(r.name, func(t *testing.T) {
			w, _, clock := startWatcher(t, nil, base()...)
			res := make(chan Transition, 1)
			go func() { res <- w.Transition(t.Context(), verdict.Deploy, clock.Now(), closed()) }()
			until(t, w, clock, func(s Transition) bool { return !s.Finished.IsZero() })
			r.tell(w)
			until(t, w, clock, func(s Transition) bool { return slices.Contains(s.SettleRestarts, r.want) })
		})
	}
}

// Probed counts either reading: one that fails counts as no reading, and a
// workload behind the Service at the second reading alone is probed.
func TestProbedFromEitherReading(t *testing.T) {
	service := &types.NamespacedName{Namespace: "orders", Name: "orders"}
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-x7k2p", Namespace: "orders",
			Labels: map[string]string{discoveryv1.LabelServiceName: "orders"}},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.244.1.5"},
			TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "orders", Name: "orders-a-1", UID: "a1"}}},
	}
	for _, r := range []struct {
		name string
		// fail lists the readings, from 1, whose List fails. When late holds,
		// the first reading finds no EndpointSlice and slice is created after
		// it. cancel ends ctx after the first reading.
		fail   []int
		late   bool
		cancel bool
		want   string
	}{
		{"behind the Service at the second reading alone", nil, true, false, "yes"},
		{"both readings fail", []int{1, 2}, false, false, "not known"},
		{"the first reading fails", []int{1}, false, false, "yes"},
		{"no second reading after ctx ended", nil, true, true, "no"},
	} {
		t.Run(r.name, func(t *testing.T) {
			objs := base()
			if !r.late {
				objs = append(objs, slice)
			}
			w, client, clock := startWatcher(t, service, objs...)
			var lists atomic.Int32
			client.PrependReactor("list", "endpointslices", func(k8stesting.Action) (bool, runtime.Object, error) {
				n := int(lists.Add(1))
				switch {
				case slices.Contains(r.fail, n):
					return true, nil, errors.New("the API server is not reachable")
				case r.late && n == 1:
					return true, &discoveryv1.EndpointSliceList{}, nil
				}
				return false, nil, nil
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			exited := make(chan struct{})
			res := make(chan Transition, 1)
			go func() { res <- w.Transition(ctx, verdict.Deploy, clock.Now(), exited) }()
			for deadline := time.Now().Add(5 * time.Second); lists.Load() < 1; time.Sleep(10 * time.Millisecond) {
				if time.Now().After(deadline) {
					t.Fatal("the first reading was not made")
				}
			}
			if r.late {
				if _, err := client.DiscoveryV1().EndpointSlices("orders").Create(t.Context(), slice, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			var tr Transition
			if r.cancel {
				cancel()
				tr = receive(t, res)
			} else {
				close(exited)
				tr = drive(t, clock, res)
			}
			if len(tr.Workloads) != 1 || tr.Workloads[0].Probed != r.want {
				t.Errorf("counts %+v; want orders probed %q", tr.Workloads, r.want)
			}
			if r.cancel && lists.Load() != 1 {
				t.Errorf("%d readings after ctx ended before the window did; want the first alone", lists.Load())
			}
		})
	}
}

func TestInstallReadyGivesCtxsError(t *testing.T) {
	objs := base()
	objs[0] = deployment("orders", 1, 0, 2, 0, 0, 0)
	w, _, _ := startWatcher(t, nil, objs...)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := w.InstallReady(ctx, t0, closed()); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v; want context.Canceled", err)
	}
}

// Start stops waiting when ctx ends before the informers have listed what
// exists, stops what it started, and returns ctx's error.
func TestStartStopsWhenCtxEnds(t *testing.T) {
	client := fake.NewClientset()
	listing, release := make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	client.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		select {
		case listing <- struct{}{}:
		default:
		}
		<-release
		return false, nil, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	res := make(chan error, 1)
	go func() {
		_, err := Start(ctx, client, Options{Namespace: "orders", Settle: 15 * time.Second,
			Timeout: 300 * time.Second, Workers: 3, Now: time.Now})
		res <- err
	}()
	select {
	case <-listing:
	case <-time.After(5 * time.Second):
		t.Fatal("the informer of pods did not list")
	}
	cancel()
	// The informer of pods ends once its list returns, and Start waits for it.
	time.AfterFunc(500*time.Millisecond, free)
	select {
	case err := <-res:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v; want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after ctx ended")
	}
}

// withVersion returns a copy of the pod p with the resource version v.
func withVersion(p *corev1.Pod, v string) *corev1.Pod {
	p = p.DeepCopy()
	p.ResourceVersion = v
	return p
}

// What the handlers tell the Tracker as a sign that the watch missed changes
// (T27-1): an update whose resource version did not change, or a tombstone,
// while a window is open, or after the transition's start and before its
// Begin is told. A real update tells nothing, and neither does a sign before
// the transition's start.
func TestMissedChanges(t *testing.T) {
	a1 := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, false)
	// When the sign comes. Begin is told after the start, as run tells it.
	const (
		inWindow    = iota // 2 s after the window began
		beforeStart        // 1 s before the transition's start
		beforeBegin        // 1 s after the transition's start, before Begin is told
	)
	after := map[int]time.Duration{inWindow: 2 * time.Second, beforeBegin: time.Second}
	for _, r := range []struct {
		name   string
		when   int
		tell   func(h handler)
		missed bool
	}{
		{"an update with the same resource version", inWindow, func(h handler) {
			h.OnUpdate(withVersion(a1, "7"), withVersion(a1, "7"))
		}, true},
		{"a tombstone", inWindow, func(h handler) {
			h.OnDelete(cache.DeletedFinalStateUnknown{Key: "orders/orders-a-1", Obj: a1})
		}, true},
		{"an update with the same resource version and no window open", beforeStart, func(h handler) {
			h.OnUpdate(withVersion(a1, "7"), withVersion(a1, "7"))
		}, false},
		{"an update with the same resource version after the start and before Begin", beforeBegin, func(h handler) {
			h.OnUpdate(withVersion(a1, "7"), withVersion(a1, "7"))
		}, true},
		{"an update whose resource version changed", inWindow, func(h handler) {
			h.OnUpdate(withVersion(a1, "7"), withVersion(a1, "8"))
		}, false},
		// The fake client gives no object a resource version; the API server
		// gives every object one.
		{"an update with no resource version", inWindow, func(h handler) {
			h.OnUpdate(a1, a1)
		}, false},
	} {
		t.Run(r.name, func(t *testing.T) {
			w, _, clock := startWatcher(t, nil, base()...)
			h := handler{w: w, kind: "Pod"}
			if r.when == beforeStart {
				r.tell(h)
				clock.Advance(time.Second)
			}
			began := clock.Now()
			if r.when == beforeBegin {
				clock.Advance(time.Second)
				r.tell(h)
			}
			exited := make(chan struct{})
			res := make(chan Transition, 1)
			go func() { res <- w.Transition(t.Context(), verdict.Deploy, began, exited) }()
			until(t, w, clock, func(s Transition) bool { return s.Name == verdict.Deploy })
			if r.when == inWindow {
				clock.Advance(2 * time.Second)
				r.tell(h)
			}
			close(exited)
			tr := drive(t, clock, res)
			var want time.Time
			if r.missed {
				want = began.Add(after[r.when])
			}
			if !tr.Missed.Equal(want) {
				t.Errorf("Missed = %v, want %v", tr.Missed, want)
			}
			text := fmt.Sprintf("the watch of the cluster missed changes %.1f s after the transition began",
				after[r.when].Seconds())
			has := slices.ContainsFunc(tr.Conditions(true), func(c verdict.Condition) bool {
				return c.Code == verdict.InternalError && c.Text == text
			})
			if has != r.missed {
				t.Errorf("conditions %v; the condition for missed changes is there: %v, want %v",
					tr.Conditions(true), has, r.missed)
			}
		})
	}
}

// A list again reaches the handlers through client-go's informer (T27-1): the
// API server ends the pods' watch as expired, the informer lists again, and
// each pod it already holds comes as an update with the same resource version.
func TestAListAgainIsMissed(t *testing.T) {
	objs := base()
	for i, o := range objs {
		o.(metav1.Object).SetResourceVersion(fmt.Sprint(10 + i))
	}
	client := fake.NewClientset(objs...)
	expiring := k8swatch.NewRaceFreeFake()
	var podWatches atomic.Int32
	client.PrependWatchReactor("pods", func(k8stesting.Action) (bool, k8swatch.Interface, error) {
		if podWatches.Add(1) == 1 {
			return true, expiring, nil
		}
		return false, nil, nil
	})
	clock := &fakeClock{now: t0}
	w, err := Start(t.Context(), client, Options{Namespace: "orders", Settle: 15 * time.Second,
		Timeout: 300 * time.Second, Workers: 3, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Stop)
	for deadline := time.Now().Add(5 * time.Second); podWatches.Load() < 1; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the informer of pods did not watch")
		}
	}
	go w.Transition(t.Context(), verdict.Deploy, clock.Now(), make(chan struct{}))
	until(t, w, clock, func(s Transition) bool { return s.Name == verdict.Deploy })
	clock.Advance(2 * time.Second)
	expiring.Error(&metav1.Status{Status: metav1.StatusFailure, Code: 410, Reason: metav1.StatusReasonExpired,
		Message: "too old resource version"})
	until(t, w, clock, func(s Transition) bool { return s.Missed.Equal(at(2)) })
}

// A first Probed reading that hangs does not hold the command's exit back
// (T27-3): the command exits at +1 s while the reading hangs, and the
// transition finishes at +1 s, not as a stall at the limit. The reading
// returns only after the window ended, so it counts as not made: orders, behind
// the Service then, is not probed by the second reading, which finds no
// endpoint.
func TestAHeldReadingHoldsNothing(t *testing.T) {
	service := &types.NamespacedName{Namespace: "orders", Name: "orders"}
	w, client, clock := startWatcher(t, service, base()...)
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	behind := discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-x7k2p", Namespace: "orders",
			Labels: map[string]string{discoveryv1.LabelServiceName: "orders"}},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.244.1.5"},
			TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "orders", Name: "orders-a-1", UID: "a1"}}},
	}
	var lists atomic.Int32
	client.PrependReactor("list", "endpointslices", func(k8stesting.Action) (bool, runtime.Object, error) {
		if lists.Add(1) == 1 {
			<-release
			return true, &discoveryv1.EndpointSliceList{Items: []discoveryv1.EndpointSlice{behind}}, nil
		}
		return true, &discoveryv1.EndpointSliceList{}, nil
	})
	exited := make(chan struct{})
	res := make(chan Transition, 1)
	go func() { res <- w.Transition(t.Context(), verdict.Deploy, clock.Now(), exited) }()
	for deadline := time.Now().Add(5 * time.Second); lists.Load() < 1; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("no reading")
		}
	}
	clock.Advance(time.Second)
	close(exited) // the command exits at +1 s
	until(t, w, clock, func(s Transition) bool { return s.Finished.Equal(at(1)) })
	clock.Advance(300 * time.Second)
	free()
	tr := receive(t, res)
	if tr.Stalled || !tr.Finished.Equal(at(1)) || !tr.Ended.Equal(at(16)) {
		t.Errorf("stalled %v, finished %v, ended %v; want finished at +1 s and ended at +16 s",
			tr.Stalled, tr.Finished, tr.Ended)
	}
	if len(tr.Workloads) != 1 || tr.Workloads[0].Probed != "no" {
		t.Errorf("counts %+v; want orders probed no, from the second reading alone", tr.Workloads)
	}
}

// hangingClient is the fake client whose EndpointSlice lists return only when
// their ctx ends, as a real client's do when the API server does not answer.
type hangingClient struct{ *fake.Clientset }

func (c hangingClient) DiscoveryV1() discoveryv1client.DiscoveryV1Interface {
	return hangingDiscovery{c.Clientset.DiscoveryV1()}
}

type hangingDiscovery struct {
	discoveryv1client.DiscoveryV1Interface
}

func (d hangingDiscovery) EndpointSlices(namespace string) discoveryv1client.EndpointSliceInterface {
	return hangingSlices{d.DiscoveryV1Interface.EndpointSlices(namespace)}
}

type hangingSlices struct {
	discoveryv1client.EndpointSliceInterface
}

func (hangingSlices) List(ctx context.Context, _ metav1.ListOptions) (*discoveryv1.EndpointSliceList, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// Readings that hang end with the window (T27-3): the first when the window
// ends, the last one settle time after it. Transition returns, and neither
// counts.
func TestReadingsEndWithTheWindow(t *testing.T) {
	client := fake.NewClientset(base()...)
	var watches atomic.Int32
	client.PrependWatchReactor("*", func(k8stesting.Action) (bool, k8swatch.Interface, error) {
		watches.Add(1)
		return false, nil, nil
	})
	clock := &fakeClock{now: t0}
	w, err := Start(t.Context(), hangingClient{client}, Options{Namespace: "orders", Settle: 300 * time.Millisecond,
		Timeout: 300 * time.Second, Workers: 3, Service: &types.NamespacedName{Namespace: "orders", Name: "orders"},
		Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Stop)
	waitFor(t, &watches, 5, "watches")
	res := make(chan Transition, 1)
	go func() { res <- w.Transition(t.Context(), verdict.Deploy, clock.Now(), closed()) }()
	tr := drive(t, clock, res)
	if len(tr.Workloads) != 1 || tr.Workloads[0].Probed != "not known" {
		t.Errorf("counts %+v; want orders probed not known", tr.Workloads)
	}
}

// CommandExited is told when exited closes, at the clock's time then: a
// rollout complete at +1 s whose command exits at +40 s finishes at +40 s,
// and its window ends at +55 s, one settle time later.
func TestTheExitIsToldWhenItComes(t *testing.T) {
	w, client, clock := startWatcher(t, nil, base()...)
	exited := make(chan struct{})
	res := make(chan Transition, 1)
	go func() { res <- w.Transition(t.Context(), verdict.Deploy, clock.Now(), exited) }()
	until(t, w, clock, func(s Transition) bool { return s.Name == verdict.Deploy })
	clock.Advance(time.Second)
	update(t, client, deployment("orders", 2, 2, 2, 2, 2, 2)) // complete at +1 s
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		w.mu.Lock()
		generation := w.tr.workloads[orders].generation
		w.mu.Unlock()
		if generation == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the Deployment's new generation was not told")
		}
	}
	clock.Advance(39 * time.Second)
	close(exited) // the command exits at +40 s
	until(t, w, clock, func(s Transition) bool { return !s.Finished.IsZero() })
	tr := drive(t, clock, res)
	if !tr.Finished.Equal(at(40)) || !tr.Ended.Equal(at(55)) {
		t.Errorf("finished %v, ended %v; want finished at +40 s and ended at +55 s", tr.Finished, tr.Ended)
	}
}

// Only the watched namespace is watched: a complete Deployment with its pods
// in kube-system is not.
func TestOnlyTheNamespaceIsWatched(t *testing.T) {
	others := []runtime.Object{deployment("coredns", 1, 1, 1, 1, 1, 1), replicaSet("coredns-a", "coredns"),
		pod("coredns-a-1", "c1", "ReplicaSet", "coredns-a", true, false)}
	for _, o := range others {
		o.(metav1.Object).SetNamespace("kube-system")
	}
	t.Run("a transition lists deployment/orders alone", func(t *testing.T) {
		w, _, clock := startWatcher(t, nil, append(base(), others...)...)
		res := make(chan Transition, 1)
		go func() { res <- w.Transition(t.Context(), verdict.Deploy, clock.Now(), closed()) }()
		tr := drive(t, clock, res)
		if len(tr.Workloads) != 1 || tr.Workloads[0].Workload != orders {
			t.Errorf("workloads %+v; want deployment/orders alone", tr.Workloads)
		}
	})
	t.Run("nothing in the namespace is nothing to watch", func(t *testing.T) {
		w, _, clock := startWatcher(t, nil, others...)
		res := make(chan error, 1)
		go func() {
			_, err := w.InstallReady(t.Context(), t0, closed())
			res <- err
		}()
		var c verdict.Condition
		if err := drive(t, clock, res); !errors.As(err, &c) || c.Code != verdict.NothingWatched {
			t.Errorf("err = %v, want nothing-watched", err)
		}
	})
}

// A last reading that returns after its bound, settle time after the window,
// counts for nothing, even from a client that returns then with no error.
func TestALateLastReadingCountsForNothing(t *testing.T) {
	behind := discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-x7k2p", Namespace: "orders",
			Labels: map[string]string{discoveryv1.LabelServiceName: "orders"}},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.244.1.5"},
			TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "orders", Name: "orders-a-1", UID: "a1"}}},
	}
	client := fake.NewClientset(base()...)
	var watches, lists atomic.Int32
	client.PrependWatchReactor("*", func(k8stesting.Action) (bool, k8swatch.Interface, error) {
		watches.Add(1)
		return false, nil, nil
	})
	client.PrependReactor("list", "endpointslices", func(k8stesting.Action) (bool, runtime.Object, error) {
		if lists.Add(1) == 1 {
			return true, &discoveryv1.EndpointSliceList{}, nil
		}
		time.Sleep(600 * time.Millisecond) // past the bound of 300 ms
		return true, &discoveryv1.EndpointSliceList{Items: []discoveryv1.EndpointSlice{behind}}, nil
	})
	clock := &fakeClock{now: t0}
	w, err := Start(t.Context(), client, Options{Namespace: "orders", Settle: 300 * time.Millisecond,
		Timeout: 300 * time.Second, Workers: 3, Service: &types.NamespacedName{Namespace: "orders", Name: "orders"},
		Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Stop)
	waitFor(t, &watches, 5, "watches")
	res := make(chan Transition, 1)
	go func() { res <- w.Transition(t.Context(), verdict.Deploy, clock.Now(), closed()) }()
	tr := drive(t, clock, res)
	if len(tr.Workloads) != 1 || tr.Workloads[0].Probed != "no" {
		t.Errorf("counts %+v; want orders probed no, from the first reading alone", tr.Workloads)
	}
}
