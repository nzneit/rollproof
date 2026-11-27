package watch

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8swatch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/nzneit/rollproof/internal/verdict"
)

// steeredStream is the first watch stream of pods: it passes the fake
// client's events on until the test ends it cleanly, or with an error event:
// the one an API server sends when a watch expires (expire), or when it
// throttles a client (throttle).
type steeredStream struct {
	real      k8swatch.Interface
	out       chan k8swatch.Event
	expire    chan struct{}
	throttle  chan struct{}
	end       chan struct{}
	done      chan struct{}
	once      sync.Once
	forwarded atomic.Int32
}

func (s *steeredStream) ResultChan() <-chan k8swatch.Event { return s.out }

func (s *steeredStream) Stop() {
	s.once.Do(func() {
		close(s.done)
		s.real.Stop()
	})
}

func (s *steeredStream) run() {
	defer close(s.out)
	for {
		select {
		case e, ok := <-s.real.ResultChan():
			if !ok {
				return
			}
			select {
			case s.out <- e:
				s.forwarded.Add(1)
			case <-s.done:
				return
			}
		case <-s.expire:
			s.fail(&metav1.Status{Status: metav1.StatusFailure, Code: 410, Reason: metav1.StatusReasonExpired,
				Message: "too old resource version"})
			return
		case <-s.throttle:
			s.fail(&metav1.Status{Status: metav1.StatusFailure, Code: 429, Reason: metav1.StatusReasonTooManyRequests,
				Message: "too many requests"})
			return
		case <-s.end:
			return
		case <-s.done:
			return
		}
	}
}

// fail sends an error event and waits until the stream is stopped.
func (s *steeredStream) fail(status *metav1.Status) {
	select {
	case s.out <- k8swatch.Event{Type: k8swatch.Error, Object: status}:
		<-s.done
	case <-s.done:
	}
}

// startSteered starts a Watcher as startWatcher does, on base, whose first
// watch of pods is a steeredStream. later answers each later call to watch
// pods, numbered from 2; when it returns false the fake client answers.
func startSteered(t *testing.T, later func(n int32) (bool, k8swatch.Interface, error)) (*Watcher, *fake.Clientset, *fakeClock, *steeredStream, *atomic.Int32) {
	t.Helper()
	client := fake.NewClientset(base()...)
	first := &steeredStream{out: make(chan k8swatch.Event, 100), expire: make(chan struct{}),
		throttle: make(chan struct{}), end: make(chan struct{}), done: make(chan struct{})}
	var watches, podWatches atomic.Int32
	client.PrependWatchReactor("*", func(k8stesting.Action) (bool, k8swatch.Interface, error) {
		watches.Add(1)
		return false, nil, nil
	})
	client.PrependWatchReactor("pods", func(k8stesting.Action) (bool, k8swatch.Interface, error) {
		n := podWatches.Add(1)
		if n == 1 {
			watches.Add(1)
			real, err := client.Tracker().Watch(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "orders")
			if err != nil {
				return true, nil, err
			}
			first.real = real
			go first.run()
			return true, first, nil
		}
		if later != nil {
			return later(n)
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
	for deadline := time.Now().Add(5 * time.Second); watches.Load() < 5; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the informers did not start their watches")
		}
	}
	return w, client, clock, first, &podWatches
}

// passEvent changes pod orders-a-1 and waits until the first stream has passed
// the change on, so that the reflector does not take the stream for one that
// ended at once.
func passEvent(t *testing.T, client *fake.Clientset, first *steeredStream) {
	t.Helper()
	p := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, false)
	p.Labels = map[string]string{"touched": "yes"}
	update(t, client, p)
	for deadline := time.Now().Add(5 * time.Second); first.forwarded.Load() < 1; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the first stream passed no event on")
		}
	}
}

// waitFor waits, for at most 5 s of real time, until n holds at least want.
func waitFor(t *testing.T, n *atomic.Int32, want int32, what string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); n.Load() < want; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%s: %d, want %d", what, n.Load(), want)
		}
	}
}

// follow runs a deploy from began, with a command that has not exited, until
// the test ends, and waits for it to return.
func follow(t *testing.T, w *Watcher, began time.Time) {
	ctx, cancel := context.WithCancel(t.Context())
	res := make(chan Transition, 1)
	go func() { res <- w.Transition(ctx, verdict.Deploy, began, make(chan struct{})) }()
	t.Cleanup(func() {
		cancel()
		<-res
	})
}

// missedText is the condition's text for changes missed s seconds into the
// transition.
func missedText(s float64) string {
	return fmt.Sprintf("the watch of the cluster missed changes %.1f s after the transition began", s)
}

func hasCondition(cs []verdict.Condition, code verdict.Code, text string) bool {
	return slices.ContainsFunc(cs, func(c verdict.Condition) bool { return c.Code == code && c.Text == text })
}

// The review's case (T27-2): a rollout replaced both pods and finished at
// +1 s; 14 s into the settle time the pods' watch expires and pod orders-b-1
// is deleted, and the window's end at +16 s comes before the reflector lists
// again. The window is not verified, with Missed at the break.
func TestAWatchBreakInTheSettleTime(t *testing.T) {
	w, client, clock, first, _ := startSteered(t, nil)
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
	until(t, w, clock, func(s Transition) bool { return s.Finished.Equal(at(1)) })
	clock.Advance(14 * time.Second)
	close(first.expire) // the pods' watch expires at +15 s
	until(t, w, clock, func(s Transition) bool { return !s.Missed.IsZero() })
	if err := client.CoreV1().Pods("orders").Delete(ctx, "orders-b-1", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	clock.Advance(2 * time.Second) // +17 s: past the window's end at +16 s
	tr := receive(t, res)
	if !tr.Ended.Equal(at(16)) || !tr.Missed.Equal(at(15)) {
		t.Errorf("ended %v, Missed %v; want ended at +16 s with Missed at +15 s", tr.Ended, tr.Missed)
	}
	if v, code := decided(tr); v != verdict.NotVerified || code != verdict.InternalError ||
		!hasCondition(tr.Conditions(true), verdict.InternalError, missedText(15)) {
		t.Errorf("verdict %s decided by %q from %v; want not verified, decided by the missed changes", v, code, tr.Conditions(true))
	}
}

// An error event is a break (T27-2), also when the reflector then watches
// again from its last version with no list, as it does for a throttled
// watch.
func TestAnErrorEventIsABreak(t *testing.T) {
	w, _, clock, first, _ := startSteered(t, nil)
	follow(t, w, clock.Now())
	until(t, w, clock, func(s Transition) bool { return s.Name == verdict.Deploy })
	clock.Advance(2 * time.Second)
	close(first.throttle) // an error event at +2 s
	until(t, w, clock, func(s Transition) bool { return s.Missed.Equal(at(2)) })
}

// A watch call that fails after a stream ended keeps the watch down: the
// window's Missed is when the stream ended (T27-2, T27-4). The call is held
// while the clock moves on.
func TestAFailedWatchCallKeepsTheWatchDown(t *testing.T) {
	calling, fail := make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(fail) }) }
	w, client, clock, first, _ := startSteered(t, func(n int32) (bool, k8swatch.Interface, error) {
		if n != 2 {
			return false, nil, nil
		}
		calling <- struct{}{}
		<-fail
		return true, nil, fmt.Errorf("dial tcp: %w", syscall.ECONNREFUSED)
	})
	t.Cleanup(release) // before the Watcher's Stop, which waits for the informer
	follow(t, w, clock.Now())
	until(t, w, clock, func(s Transition) bool { return s.Name == verdict.Deploy })
	passEvent(t, client, first)
	clock.Advance(2 * time.Second)
	close(first.end) // the stream ends cleanly at +2 s
	select {
	case <-calling:
	case <-time.After(5 * time.Second):
		t.Fatal("the informer did not watch again")
	}
	clock.Advance(3 * time.Second)
	release() // the call fails at +5 s
	until(t, w, clock, func(s Transition) bool { return s.Missed.Equal(at(2)) })
}

// A renewal inside a window, a stream that ends cleanly and a watch from the
// last version that succeeds, makes the window not verified, with Missed when
// the stream ended (T27-4): with a watch timeout of a day, a renewal inside a
// run is abnormal. Before T27-4 this row was TestACleanRenewalIsNoBreak and
// gave no Missed.
func TestARenewalInsideAWindowMakesItNotVerified(t *testing.T) {
	w, client, clock, first, podWatches := startSteered(t, nil)
	exited := make(chan struct{})
	res := make(chan Transition, 1)
	go func() { res <- w.Transition(t.Context(), verdict.Deploy, clock.Now(), exited) }()
	until(t, w, clock, func(s Transition) bool { return s.Name == verdict.Deploy })
	passEvent(t, client, first)
	clock.Advance(2 * time.Second)
	close(first.end)
	waitFor(t, podWatches, 2, "watches of pods")
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		w.mu.Lock()
		_, down := w.tr.down["Pod"]
		w.mu.Unlock()
		if !down {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the watch of pods is not up again")
		}
	}
	close(exited)
	tr := drive(t, clock, res)
	if !tr.Missed.Equal(at(2)) || !hasCondition(tr.Conditions(true), verdict.InternalError, missedText(2)) {
		t.Errorf("Missed %v, conditions %v; want Missed at +2 s and its condition", tr.Missed, tr.Conditions(true))
	}
}

// A window whose end comes due while a watch is down is not verified, with
// Missed when the watch went down (T27-2). The stream ends cleanly in the
// settle time and the next watch call is held past the window's end.
func TestAWindowThatEndsWhileAWatchIsDown(t *testing.T) {
	calling, free := make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(free) }) }
	w, client, clock, first, _ := startSteered(t, func(n int32) (bool, k8swatch.Interface, error) {
		if n == 2 {
			calling <- struct{}{}
			<-free
		}
		return false, nil, nil
	})
	t.Cleanup(release)
	res := make(chan Transition, 1)
	go func() { res <- w.Transition(t.Context(), verdict.Deploy, clock.Now(), closed()) }()
	until(t, w, clock, func(s Transition) bool { return s.Finished.Equal(t0) })
	passEvent(t, client, first)
	clock.Advance(5 * time.Second)
	close(first.end) // the watch goes down at +5 s, in the settle time
	select {
	case <-calling:
	case <-time.After(5 * time.Second):
		t.Fatal("the informer did not watch again")
	}
	clock.Advance(20 * time.Second) // +25 s: the window's end at +15 s has come
	tr := receive(t, res)
	if !tr.Ended.Equal(at(15)) || !tr.Missed.Equal(at(5)) ||
		!hasCondition(tr.Conditions(true), verdict.InternalError, missedText(5)) {
		t.Errorf("ended %v, Missed %v, conditions %v; want ended at +15 s, Missed at +5 s, and its condition",
			tr.Ended, tr.Missed, tr.Conditions(true))
	}
}

// A stream that ends inside a window makes it not verified from that moment
// (T27-4), and a list again after it, plain or a watch that sends the initial
// events, keeps that moment. The connection here is apart from the informers:
// the fake client holds its lock while a reactor holds a call, so a held watch
// would hold the list too. Before T27-4 this row was
// TestAListAfterTheFirstIsABreak, and its check before the list gave no
// Missed.
func TestAListAgainKeepsTheMomentTheWatchWentDown(t *testing.T) {
	for _, r := range []struct {
		name string
		list func(c *connection) error
	}{
		{"a list", func(c *connection) error {
			_, err := c.list(t.Context(), metav1.ListOptions{})
			return err
		}},
		{"a watch that sends the initial events", func(c *connection) error {
			yes := true
			s, err := c.Watch(t.Context(), metav1.ListOptions{SendInitialEvents: &yes})
			if err == nil {
				s.Stop()
			}
			return err
		}},
	} {
		t.Run(r.name, func(t *testing.T) {
			w, _, clock := startWatcher(t, nil, base()...)
			follow(t, w, clock.Now())
			until(t, w, clock, func(s Transition) bool { return s.Name == verdict.Deploy })
			var streams []*k8swatch.RaceFreeFakeWatcher
			c := &connection{w: w, kind: "apart", example: &corev1.Pod{}, firstUp: make(chan struct{}),
				list: func(context.Context, metav1.ListOptions) (runtime.Object, error) {
					return &corev1.PodList{}, nil
				},
				watch: func(context.Context, metav1.ListOptions) (k8swatch.Interface, error) {
					s := k8swatch.NewRaceFreeFake()
					streams = append(streams, s)
					return s, nil
				}}
			if _, err := c.list(t.Context(), metav1.ListOptions{}); err != nil {
				t.Fatal(err)
			}
			first, err := c.Watch(t.Context(), metav1.ListOptions{})
			if err != nil {
				t.Fatal(err)
			}
			clock.Advance(2 * time.Second)
			streams[0].Stop() // the stream ends at +2 s
			first.Stop()
			clock.Advance(2 * time.Second)
			missed := func() time.Time {
				w.mu.Lock()
				defer w.mu.Unlock()
				s, _ := w.tr.State(clock.Now())
				return s.Missed
			}
			if m := missed(); !m.Equal(at(2)) {
				t.Fatalf("Missed %v after a stream that ended at +2 s; want +2 s", m)
			}
			if err := r.list(c); err != nil {
				t.Fatal(err)
			}
			if m := missed(); !m.Equal(at(2)) {
				t.Errorf("Missed %v after the list, want +2 s, when the stream before it ended", m)
			}
		})
	}
}

// After Stop, no watch stream of the Watcher passes events on.
func TestNoStreamAfterStop(t *testing.T) {
	w, _, _ := startWatcher(t, nil, base()...)
	waitFor(t, &w.streams, 5, "streams before Stop")
	w.Stop()
	if n := w.streams.Load(); n != 0 {
		t.Errorf("%d streams after Stop, want 0", n)
	}
}

// A stream's Stop returns when its end has been told, so that the end comes
// before any call that the informer makes next, a watch that is up included.
func TestAStreamsStopTellsItsEndFirst(t *testing.T) {
	w, _, clock := startWatcher(t, nil, base()...)
	c := &connection{w: w, kind: "apart", example: &corev1.Pod{}, firstUp: make(chan struct{}),
		watch: func(context.Context, metav1.ListOptions) (k8swatch.Interface, error) {
			return k8swatch.NewRaceFreeFake(), nil
		}}
	s, err := c.Watch(t.Context(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Second)
	s.Stop()
	w.mu.Lock()
	down, ok := w.tr.down["apart"]
	w.mu.Unlock()
	if !ok || !down.Equal(at(1)) {
		t.Errorf("down %v (%v) when Stop returned; want down at +1 s", down, ok)
	}
}

// A watch down across a transition's start makes its window not verified,
// with Missed at the start (T27-4): a renewal whose watch call returns after
// the start, and a break, a watch that expires and lists again, whose list
// returns after the start. Begin is told while the watch is down.
func TestAGapAcrossTheStartThroughTheWatcher(t *testing.T) {
	for _, r := range []struct {
		name   string
		expire bool // the stream ends with an expired error, not cleanly
	}{
		{"a renewal", false},
		{"a break", true},
	} {
		t.Run(r.name, func(t *testing.T) {
			calling, free := make(chan struct{}, 1), make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(free) }) }
			hold := func() {
				calling <- struct{}{}
				<-free
			}
			var lists atomic.Int32
			w, client, clock, first, podWatches := startSteered(t, func(n int32) (bool, k8swatch.Interface, error) {
				if n == 2 && !r.expire {
					hold() // the renewal's watch call
				}
				return false, nil, nil
			})
			client.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
				if lists.Add(1) == 1 && r.expire {
					hold() // the list after the expired watch
				}
				return false, nil, nil
			})
			t.Cleanup(release)
			if r.expire {
				close(first.expire)
			} else {
				passEvent(t, client, first)
				close(first.end)
			}
			select { // the watch went down at +0 s
			case <-calling:
			case <-time.After(5 * time.Second):
				t.Fatal("the informer did not watch or list again")
			}
			clock.Advance(time.Second)
			exited := make(chan struct{})
			res := make(chan Transition, 1)
			go func() { res <- w.Transition(t.Context(), verdict.Deploy, at(1), exited) }()
			until(t, w, clock, func(s Transition) bool { return s.Name == verdict.Deploy })
			clock.Advance(time.Second)
			release() // the watch is up again at +2 s
			waitFor(t, podWatches, 2, "watches of pods")
			close(exited)
			tr := drive(t, clock, res)
			if !tr.Missed.Equal(at(1)) || !hasCondition(tr.Conditions(true), verdict.InternalError, missedText(0)) {
				t.Errorf("Missed %v, conditions %v; want Missed at the start, +1 s, and its condition",
					tr.Missed, tr.Conditions(true))
			}
		})
	}
}

// Every watch call asks the API server for a timeout of a day, so that no
// routine renewal falls inside a run (T27-4): a plain watch and one that sends
// the initial events alike, whatever the informer asked for.
func TestTheWatchAsksForADay(t *testing.T) {
	w, _, _ := startWatcher(t, nil, base()...)
	var asked []int64
	c := &connection{w: w, kind: "apart", example: &corev1.Pod{}, firstUp: make(chan struct{}),
		watch: func(_ context.Context, o metav1.ListOptions) (k8swatch.Interface, error) {
			asked = append(asked, *o.TimeoutSeconds)
			return k8swatch.NewRaceFreeFake(), nil
		}}
	five, yes := int64(300), true
	for _, o := range []metav1.ListOptions{{TimeoutSeconds: &five}, {TimeoutSeconds: &five, SendInitialEvents: &yes}} {
		s, err := c.Watch(t.Context(), o)
		if err != nil {
			t.Fatal(err)
		}
		s.Stop()
	}
	if want := []int64{86400, 86400}; !slices.Equal(asked, want) {
		t.Errorf("the watch calls asked for %v seconds, want %v", asked, want)
	}
}

// Start returns only when every watch is up, so that a window begins with each
// up: here the watch of pods comes 300 ms after its list.
func TestStartWaitsForEveryWatch(t *testing.T) {
	client := fake.NewClientset(base()...)
	client.PrependWatchReactor("pods", func(k8stesting.Action) (bool, k8swatch.Interface, error) {
		time.Sleep(300 * time.Millisecond)
		return false, nil, nil
	})
	w, err := Start(t.Context(), client, Options{Namespace: "orders", Settle: 15 * time.Second,
		Timeout: 300 * time.Second, Workers: 3, Now: func() time.Time { return t0 }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Stop)
	for kind, c := range w.conns {
		select {
		case <-c.firstUp:
		default:
			t.Errorf("the watch of %s is not up when Start returns", kind)
		}
	}
}
