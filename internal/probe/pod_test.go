package probe

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"sigs.k8s.io/yaml"

	"rollproof/internal/cluster"
	"rollproof/internal/verdict"
)

var update = flag.Bool("update", false, "rewrite the golden files")

const probeImage = "rollproof.test/rollproof@sha256:1333aec6e2215a19bb2a7361377cf1cb27f1261bcf1ddae03e4ea086c8bab89c"

func TestPodSpec(t *testing.T) {
	o := PodOptions{
		Image: probeImage,
		Args:  []string{"probe", "http", "--url", "http://orders.orders.svc/api/orders/latest", "--rate", "200", "--timeout", "2s", "--expect-status", "200-299"},
		Drain: 7 * time.Second,
	}
	got, err := yaml.Marshal(podSpec(o))
	if err != nil {
		t.Fatal(err)
	}
	const path = "testdata/pod-http.golden.yaml"
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs\n--- got\n%s--- want\n%s", path, got, want)
	}
	for _, c := range []struct {
		drain time.Duration
		grace int64
	}{{7 * time.Second, 7}, {7500 * time.Millisecond, 8}, {500 * time.Millisecond, 1}} {
		if g := *podSpec(PodOptions{Drain: c.drain}).Spec.TerminationGracePeriodSeconds; g != c.grace {
			t.Errorf("drain %v: grace %d s, want %d s", c.drain, g, c.grace)
		}
	}
}

// fakeClock is a clock that the test sets.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time  { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Set(t time.Time) { c.mu.Lock(); defer c.mu.Unlock(); c.t = t }

// runningStatus is the status of a probe pod whose container runs.
func runningStatus() corev1.PodStatus {
	return corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{
		{Name: "probe", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
	}}
}

// terminatedStatus is the status of a probe pod whose container has ended.
func terminatedStatus(code int32, reason string, finished time.Time) corev1.PodStatus {
	return corev1.PodStatus{Phase: corev1.PodFailed, ContainerStatuses: []corev1.ContainerStatus{
		{Name: "probe", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
			ExitCode: code, Reason: reason, FinishedAt: metav1.NewTime(finished),
		}}},
	}}
}

// probeSetup is one Pod on a fake client, with its clock and its raw output.
type probeSetup struct {
	pod    *Pod
	client *fake.Clientset
	clock  *fakeClock
	raw    *bytes.Buffer
}

// newProbe makes a Pod on a fake client that holds objects. The probe's pod
// gets status when it is created, and its log is l.
func newProbe(t *testing.T, l *fakeLog, status corev1.PodStatus, drain, startTimeout time.Duration, objects ...runtime.Object) probeSetup {
	t.Helper()
	quickPolling(t)
	oldMargin := stopMargin
	stopMargin = 50 * time.Millisecond
	t.Cleanup(func() { stopMargin = oldMargin })
	client := fake.NewClientset(objects...)
	client.PrependReactor("create", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		a.(k8stesting.CreateAction).GetObject().(*corev1.Pod).Status = status
		return false, nil, nil
	})
	s := probeSetup{client: client, clock: &fakeClock{t: logAt(0)}, raw: &bytes.Buffer{}}
	s.pod = NewPod(PodOptions{
		Client: client, Image: probeImage, Args: []string{"probe", "http"},
		Drain: drain, StartTimeout: startTimeout, Raw: s.raw, Now: s.clock.Now,
	})
	s.pod.open = l.open
	t.Cleanup(func() { s.pod.Stop(context.Background()) }) // a test that fails leaves no probe running
	return s
}

// setStatus replaces the status of the probe's pod.
func (s probeSetup) setStatus(t *testing.T, status corev1.PodStatus) {
	t.Helper()
	pods := s.client.CoreV1().Pods(cluster.ProbeNamespace)
	p, err := pods.Get(context.Background(), "probe", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p.Status = status
	if _, err := pods.UpdateStatus(context.Background(), p, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
}

// waitGone waits until the probe's pod no longer exists.
func (s probeSetup) waitGone(t *testing.T) {
	t.Helper()
	for range 200 {
		_, err := s.client.CoreV1().Pods(cluster.ProbeNamespace).Get(context.Background(), "probe", metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the probe's pod was not deleted")
}

// lastDeleteGrace is the grace period of the last delete of a pod.
func (s probeSetup) lastDeleteGrace() *int64 {
	var grace *int64
	for _, a := range s.client.Actions() {
		if d, ok := a.(k8stesting.DeleteAction); ok && a.GetResource().Resource == "pods" {
			grace = d.GetDeleteOptions().GracePeriodSeconds
		}
	}
	return grace
}

// allEvents reads Events until Stop has closed it.
func allEvents(events <-chan Event) []Event {
	var all []Event
	for e := range events {
		all = append(all, e)
	}
	return all
}

// condition is the Condition that err is, or a zero one.
func condition(err error) verdict.Condition {
	var c verdict.Condition
	errors.As(err, &c)
	return c
}

// openStream is a stream that gives the line and then stays open until the
// follower closes it, or until the test ends (ruling C3).
func openStream(t *testing.T, line string) io.Reader {
	t.Helper()
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	go io.WriteString(pw, line)
	return pr
}

func TestStartReplacesALeftoverPod(t *testing.T) {
	leftover := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "probe", Namespace: cluster.ProbeNamespace, Labels: map[string]string{"left": "over"}}}
	s := newProbe(t, newFakeLog(logLine(logAt(400), `{"ok":true}`)), runningStatus(), 100*time.Millisecond, 5*time.Second, leftover)
	if _, err := s.pod.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.pod.Stop(context.Background())
	var verbs []string
	for _, a := range s.client.Actions() {
		if a.GetResource().Resource == "pods" && (a.GetVerb() == "delete" || a.GetVerb() == "create") {
			verbs = append(verbs, a.GetVerb())
		}
	}
	if !slices.Equal(verbs, []string{"delete", "create"}) {
		t.Errorf("pod actions %v, want delete then create", verbs)
	}
	p, err := s.client.CoreV1().Pods(cluster.ProbeNamespace).Get(context.Background(), "probe", metav1.GetOptions{})
	if err != nil || p.Labels["left"] != "" || p.Labels["app.kubernetes.io/name"] != "rollproof-probe" {
		t.Errorf("the pod is %v, %v; want the new one", p.Labels, err)
	}
}

func TestStartReturnsAtTheFirstObservation(t *testing.T) {
	early := logAt(-1000).Format(time.RFC3339Nano)
	l := newFakeLog(logLine(logAt(100), "starting") + logLine(logAt(400), `{"ok":true,"t":"`+early+`"}`))
	s := newProbe(t, l, runningStatus(), 100*time.Millisecond, 5*time.Second)
	s.clock.Set(logAt(700))
	st, err := s.pod.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.First.Equal(logAt(400)) {
		t.Errorf("First = %v, want the runtime's stamp of the observation, %v", st.First, logAt(400))
	}
	facts, err := s.pod.Stop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	events := allEvents(s.pod.Events())
	if len(events) != 1 || events[0].Obs == nil || !events[0].Obs.T.Equal(logAt(-1000)) {
		t.Errorf("events = %+v, want the one observation", events)
	}
	if facts.Lines != 2 || facts.ClockDifference != 300*time.Millisecond {
		t.Errorf("facts = %+v, want 2 lines and a clock difference of 300ms", facts)
	}
	if !strings.Contains(s.raw.String(), "starting") {
		t.Errorf("raw = %q, want every line", s.raw.String())
	}
}

func TestClockCheck(t *testing.T) {
	for _, c := range []struct {
		name  string
		d     time.Duration
		fails bool
	}{
		{"read 0.3 s after the stamp", 300 * time.Millisecond, false},
		{"read 2.5 s after the stamp", 2500 * time.Millisecond, true},
		{"read 2.5 s before the stamp", -2500 * time.Millisecond, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newProbe(t, newFakeLog(logLine(logAt(400), `{"ok":true}`)), runningStatus(), 100*time.Millisecond, 5*time.Second)
			s.clock.Set(logAt(400).Add(c.d))
			_, err := s.pod.Start(context.Background())
			got := condition(err)
			switch {
			case c.fails && (got.Code != verdict.RunnerUnsupported || !strings.Contains(got.Text, "the nodes' clock differs from the runner's by 2.5s")):
				t.Errorf("Start = %v, want runner-unsupported for the clock", err)
			case !c.fails && err != nil:
				t.Errorf("Start = %v, want no error", err)
			}
			facts, _ := s.pod.Stop(context.Background())
			if facts.ClockDifference != c.d {
				t.Errorf("ClockDifference = %v, want %v", facts.ClockDifference, c.d)
			}
		})
	}
}

func TestStartWhenTheProbeExitsFirst(t *testing.T) {
	s := newProbe(t, &fakeLog{}, terminatedStatus(2, "Error", logAt(300)), 100*time.Millisecond, 10*time.Second)
	began := time.Now()
	_, err := s.pod.Start(context.Background())
	got := condition(err)
	if got.Code != verdict.ProbeExited || !strings.Contains(got.Text, "exit code 2") || !strings.Contains(got.Text, "Error") {
		t.Errorf("Start = %v, want probe-exited with the exit code and the reason", err)
	}
	if time.Since(began) > 2*time.Second {
		t.Error("Start waited for StartTimeout")
	}
}

func TestStartTimesOut(t *testing.T) {
	for _, c := range []struct {
		name   string
		log    *fakeLog
		status corev1.PodStatus
		want   []string
	}{
		{"not scheduled", &fakeLog{}, corev1.PodStatus{Phase: corev1.PodPending, Conditions: []corev1.PodCondition{{
			Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable",
			Message: "0/4 nodes are available: 1 node(s) had untolerated taint",
		}}}, []string{"Pending", "0/4 nodes are available: 1 node(s) had untolerated taint"}},
		{"image not pulled", &fakeLog{}, corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: []corev1.ContainerStatus{{
			Name: "probe", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}},
		}}}, []string{"Pending", "ImagePullBackOff"}},
		{"log text only", newFakeLog(logLine(logAt(100), "starting")), runningStatus(), []string{"Running"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newProbe(t, c.log, c.status, 100*time.Millisecond, 300*time.Millisecond)
			began := time.Now()
			_, err := s.pod.Start(context.Background())
			got := condition(err)
			if got.Code != verdict.ProbeNotStarted {
				t.Fatalf("Start = %v, want probe-not-started", err)
			}
			for _, w := range c.want {
				if !strings.Contains(got.Text, w) {
					t.Errorf("text %q has no %q", got.Text, w)
				}
			}
			if time.Since(began) < 300*time.Millisecond {
				t.Error("Start gave up before StartTimeout")
			}
		})
	}
}

func TestStartEndsWithItsContext(t *testing.T) {
	s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 10*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := s.pod.Start(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Start = %v, want the context's error", err)
	}
}

func TestProbeExitsAfterStart(t *testing.T) {
	for _, c := range []struct {
		name     string
		finished time.Time
		want     time.Time
	}{
		{"at the container's finishedAt", logAt(5000), logAt(5000)},
		{"at now when the status gives no time", time.Time{}, logAt(700)},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newProbe(t, newFakeLog(logLine(logAt(400), `{"ok":true}`)), runningStatus(), 100*time.Millisecond, 5*time.Second)
			s.clock.Set(logAt(700))
			_, err := s.pod.Start(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer s.pod.Stop(context.Background())
			s.setStatus(t, terminatedStatus(1, "Error", c.finished))
			timeout := time.After(2 * time.Second)
			for {
				select {
				case e := <-s.pod.Events():
					if e.Fault == nil {
						continue
					}
					if e.Fault.Code != verdict.ProbeExited || !e.Fault.At.Equal(c.want) {
						t.Errorf("fault %+v, want probe-exited at %v", *e.Fault, c.want)
					}
					return
				case <-timeout:
					t.Fatal("no fault after the container ended")
				}
			}
		})
	}
}

func TestStopDrains(t *testing.T) {
	for _, c := range []struct {
		name     string
		clockRan time.Duration
		stopped  bool
	}{
		{"the stream ends inside the drain", 0, true},
		{"the stream ends after the drain by the clock", 3 * time.Second, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			pr, pw := io.Pipe()
			s := newProbe(t, &fakeLog{streams: []io.Reader{pr}}, runningStatus(), 2500*time.Millisecond, 5*time.Second)
			s.clock.Set(logAt(700))
			go io.WriteString(pw, logLine(logAt(400), `{"ok":true}`))
			_, err := s.pod.Start(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan Facts)
			go func() {
				f, _ := s.pod.Stop(context.Background())
				done <- f
			}()
			s.waitGone(t)
			s.clock.Set(logAt(700).Add(c.clockRan))
			io.WriteString(pw, logLine(logAt(9000), `{"ok":true}`))
			pw.Close()
			facts := <-done
			if facts.StoppedWhenAsked != c.stopped {
				t.Errorf("StoppedWhenAsked = %v, want %v", facts.StoppedWhenAsked, c.stopped)
			}
			events := allEvents(s.pod.Events())
			if len(events) != 2 || events[1].Obs == nil || !events[1].Obs.Written.Equal(logAt(9000)) {
				t.Errorf("events = %+v, want the observation of the drain too", events)
			}
			if g := s.lastDeleteGrace(); g == nil || *g != 3 {
				t.Errorf("grace period %v, want 3 s", g)
			}
		})
	}
}

func TestStopKillsAProbeThatDoesNotStop(t *testing.T) {
	// Ruling C3: the stream stays open; the brief served newFakeLog(the line), which ends.
	l := &fakeLog{streams: []io.Reader{openStream(t, logLine(logAt(400), `{"ok":true}`))}}
	s := newProbe(t, l, runningStatus(), 200*time.Millisecond, 5*time.Second)
	_, err := s.pod.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	facts, err := s.pod.Stop(context.Background())
	if err != nil || facts.StoppedWhenAsked {
		t.Errorf("Stop = %+v, %v; want StoppedWhenAsked false and no error", facts, err)
	}
	if took := time.Since(began); took > time.Second {
		t.Errorf("Stop took %v, want the drain and the margin", took)
	}
	allEvents(s.pod.Events()) // returns only when Events is closed
}

func TestStopEndsWithItsContext(t *testing.T) {
	// Ruling C3: the stream stays open; the brief served newFakeLog(the line), which ends.
	l := &fakeLog{streams: []io.Reader{openStream(t, logLine(logAt(400), `{"ok":true}`))}}
	s := newProbe(t, l, runningStatus(), 10*time.Second, 5*time.Second)
	_, err := s.pod.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := s.pod.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Stop = %v, want the context's error", err)
	}
	allEvents(s.pod.Events())
}

func TestStopBeforeStartAndTwice(t *testing.T) {
	s := newProbe(t, newFakeLog(logLine(logAt(400), `{"ok":true}`)), runningStatus(), 100*time.Millisecond, 5*time.Second)
	if f, err := s.pod.Stop(context.Background()); err != nil || f != (Facts{}) {
		t.Errorf("Stop before Start = %+v, %v; want no facts and no error", f, err)
	}
	s = newProbe(t, newFakeLog(logLine(logAt(400), `{"ok":true}`)), runningStatus(), 100*time.Millisecond, 5*time.Second)
	if _, err := s.pod.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	first, err := s.pod.Stop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second, err := s.pod.Stop(context.Background()); err != nil || second != first {
		t.Errorf("second Stop = %+v, %v; want %+v and no error", second, err, first)
	}
}

// untilFault reads events up to the first fault, for at most 2 s.
func untilFault(t *testing.T, events <-chan Event) []Event {
	t.Helper()
	var read []Event
	timeout := time.After(2 * time.Second)
	for {
		select {
		case e := <-events:
			read = append(read, e)
			if e.Fault != nil {
				return read
			}
		case <-timeout:
			t.Fatalf("no fault in 2 s; read %+v", read)
		}
	}
}

// exits counts the probe-exited faults among events.
func exits(events []Event) int {
	n := 0
	for _, e := range events {
		if e.Fault != nil && e.Fault.Code == verdict.ProbeExited {
			n++
		}
	}
	return n
}

// Stop reads the pod once more before it deletes it, so that an exit the
// watcher has not seen is still a fault, and one it has seen is not repeated.
func TestStopReportsAnExitOnce(t *testing.T) {
	for _, c := range []struct {
		name       string
		watcherSaw bool
	}{
		{"the watcher did not see it", false},
		{"the watcher saw it", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newProbe(t, newFakeLog(logLine(logAt(400), `{"ok":true}`)), runningStatus(), 100*time.Millisecond, 5*time.Second)
			if !c.watcherSaw {
				pollEvery = time.Hour // the watcher reads the pod once, at its start
			}
			s.clock.Set(logAt(700))
			_, err := s.pod.Start(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for range 200 { // until the watcher has read the running pod
				s.pod.run.mu.Lock()
				read := s.pod.run.lastPod != nil
				s.pod.run.mu.Unlock()
				if read {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			s.setStatus(t, terminatedStatus(1, "Error", logAt(5000)))
			var events []Event
			if c.watcherSaw {
				events = untilFault(t, s.pod.Events())
			}
			if _, err := s.pod.Stop(context.Background()); err != nil {
				t.Fatal(err)
			}
			events = append(events, allEvents(s.pod.Events())...)
			if n := exits(events); n != 1 {
				t.Fatalf("%d probe-exited faults, want 1: %+v", n, events)
			}
			if f := events[len(events)-1].Fault; f == nil || !f.At.Equal(logAt(5000)) || f.Text != "the probe exited with exit code 1 (Error)" {
				t.Errorf("last event = %+v, want the exit at the container's finishedAt", events[len(events)-1])
			}
		})
	}
}

func TestStartWhenThePodIsGone(t *testing.T) {
	s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 10*time.Second)
	s.client.PrependReactor("get", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(corev1.Resource("pods"), "probe")
	})
	began := time.Now()
	_, err := s.pod.Start(context.Background())
	if got := condition(err); got.Code != verdict.ProbeExited || got.Text != "the probe's pod is gone" {
		t.Errorf("Start = %v, want probe-exited for the pod that is gone", err)
	}
	if time.Since(began) > 2*time.Second {
		t.Error("Start waited for StartTimeout")
	}
}

// An exit seen at about the moment of the first observation is reported once:
// by Start, or as a fault in Events.
func TestExitAroundTheFirstObservation(t *testing.T) {
	byStart, inEvents := 0, 0
	for i := range 20 {
		s := newProbe(t, newFakeLog(logLine(logAt(400), `{"ok":true}`)), terminatedStatus(1, "Error", logAt(500)), 100*time.Millisecond, 5*time.Second)
		s.clock.Set(logAt(700))
		_, err := s.pod.Start(context.Background())
		if err != nil {
			if c := condition(err); c.Code != verdict.ProbeExited {
				t.Errorf("run %d: Start = %v, want probe-exited", i, err)
			}
			byStart++
			continue
		}
		if _, err := s.pod.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		if n := exits(allEvents(s.pod.Events())); n != 1 {
			t.Errorf("run %d: %d probe-exited faults in Events, want 1", i, n)
		}
		inEvents++
	}
	t.Logf("the exit came from Start %d times and in Events %d times", byStart, inEvents)
}

// Stop returns, and closes Events, when nobody reads Events and it is full:
// the follower and the watcher both wait on a send then.
func TestStopWhenNobodyReadsEvents(t *testing.T) {
	var log strings.Builder
	for i := range 1100 {
		log.WriteString(logLine(logAt(400+i), `{"ok":true}`))
	}
	l := &fakeLog{streams: []io.Reader{openStream(t, log.String())}}
	s := newProbe(t, l, runningStatus(), 100*time.Millisecond, 5*time.Second)
	s.clock.Set(logAt(700))
	_, err := s.pod.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for range 200 { // until the follower waits on a full channel
		if len(s.pod.Events()) == cap(s.pod.Events()) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.setStatus(t, terminatedStatus(1, "Error", logAt(5000)))
	time.Sleep(100 * time.Millisecond) // the watcher reads the pod and waits on its send
	done := make(chan Facts)
	go func() {
		f, _ := s.pod.Stop(context.Background())
		done <- f
	}()
	select {
	case f := <-done:
		if f.StoppedWhenAsked {
			t.Error("StoppedWhenAsked = true, want false: the follower was cut off")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return")
	}
	if n := len(allEvents(s.pod.Events())); n != cap(s.pod.Events()) {
		t.Errorf("%d events, want a full channel of %d", n, cap(s.pod.Events()))
	}
}

// An error of the API when the pod is read is no exit: the follower opens the
// log again, the watcher reads again, and a pod never read is in the phase
// Unknown.
func TestAnAPIErrorIsNoExit(t *testing.T) {
	for _, c := range []struct {
		name string
		log  *fakeLog
		want string // the text of Start's error; empty for none
	}{
		{"the follower opens the log again", newFakeLog(logLine(logAt(100), "starting"), logLine(logAt(400), `{"ok":true}`)), ""},
		{"the pod's state is not known", &fakeLog{}, "no observation within limits.timeout; the pod is Unknown"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newProbe(t, c.log, runningStatus(), 100*time.Millisecond, 300*time.Millisecond)
			var created atomic.Bool
			s.client.PrependReactor("create", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
				created.Store(true)
				return false, nil, nil
			})
			s.client.PrependReactor("get", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
				if !created.Load() {
					return false, nil, nil
				}
				return true, nil, apierrors.NewInternalError(errors.New("etcd is down"))
			})
			s.clock.Set(logAt(700))
			_, err := s.pod.Start(context.Background())
			switch {
			case c.want == "" && err != nil:
				t.Errorf("Start = %v, want no error", err)
			case c.want != "" && condition(err) != verdict.Condition{Code: verdict.ProbeNotStarted, Text: c.want}:
				t.Errorf("Start = %v, want probe-not-started with %q", err, c.want)
			}
			s.pod.Stop(context.Background())
		})
	}
}

// A pod deleted by someone else is an exit, once; Stop then finds no pod to
// delete, and that is no error.
func TestStopWhenThePodIsGone(t *testing.T) {
	s := newProbe(t, newFakeLog(logLine(logAt(400), `{"ok":true}`)), runningStatus(), 100*time.Millisecond, 5*time.Second)
	s.clock.Set(logAt(700))
	_, err := s.pod.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.client.CoreV1().Pods(cluster.ProbeNamespace).Delete(context.Background(), "probe", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	events := untilFault(t, s.pod.Events())
	if _, err := s.pod.Stop(context.Background()); err != nil {
		t.Errorf("Stop = %v, want no error", err)
	}
	events = append(events, allEvents(s.pod.Events())...)
	if n := exits(events); n != 1 {
		t.Fatalf("%d probe-exited faults, want 1: %+v", n, events)
	}
	if f := events[len(events)-1].Fault; f == nil || f.Text != "the probe's pod is gone" || !f.At.Equal(logAt(700)) {
		t.Errorf("last event = %+v, want the pod gone, at now", events[len(events)-1])
	}
}

// The follower's sends have a reader from the probe's first line: 3,000 lines that break the
// contract come before the first observation, more than the channel holds, and Start returns.
func TestEventsAreReadFromTheStart(t *testing.T) {
	var log strings.Builder
	for i := range 3000 {
		log.WriteString(logLine(logAt(100+i), `{"ok":"yes"}`))
	}
	log.WriteString(logLine(logAt(4000), `{"ok":true}`))
	l := &fakeLog{streams: []io.Reader{openStream(t, log.String())}}
	s := newProbe(t, l, runningStatus(), 100*time.Millisecond, 5*time.Second)
	s.clock.Set(logAt(4000))
	read := make(chan []Event, 1)
	go func() { read <- allEvents(s.pod.Events()) }()
	st, err := s.pod.Start(context.Background())
	if err != nil {
		t.Fatalf("Start = %v; with a reader of Events from before Start, the follower never waits", err)
	}
	if !st.First.Equal(logAt(4000)) {
		t.Errorf("First = %v, want %v", st.First, logAt(4000))
	}
	if _, err := s.pod.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	faults, observations := 0, 0
	for _, e := range <-read {
		if e.Fault != nil {
			faults++
		} else {
			observations++
		}
	}
	if faults != 3000 || observations != 1 {
		t.Errorf("%d faults and %d observations were read, want 3000 and 1", faults, observations)
	}
}

// A Start that failed leaves no probe running, unless its context ended: the pod is deleted with
// the drain as its grace period, Events is closed, and Stop gives what that stop left.
func TestAFailedStartStopsTheProbe(t *testing.T) {
	background := func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }
	soon := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), 100*time.Millisecond)
	}
	refuse := errors.New("the API server refused the delete")
	for _, c := range []struct {
		name       string
		log        *fakeLog
		status     corev1.PodStatus
		clock      time.Time
		ctx        func() (context.Context, context.CancelFunc)
		failDelete bool
		code       verdict.Code // empty: the context's error
		grace      int64        // of the last delete: 1 for the drain of 100 ms, 0 for the delete Start begins with
	}{
		{"no observation in time", newFakeLog(logLine(logAt(100), "starting")), runningStatus(), logAt(700), background, false, verdict.ProbeNotStarted, 1},
		{"the clock check fails", newFakeLog(logLine(logAt(400), `{"ok":true}`)), runningStatus(), logAt(2900), background, false, verdict.RunnerUnsupported, 1},
		{"the probe exits first", &fakeLog{}, terminatedStatus(2, "Error", logAt(300)), logAt(700), background, false, verdict.ProbeExited, 1},
		{"the delete fails", newFakeLog(logLine(logAt(100), "starting")), runningStatus(), logAt(700), background, true, verdict.ProbeNotStarted, 1},
		{"the context ends", &fakeLog{}, runningStatus(), logAt(700), soon, false, "", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newProbe(t, c.log, c.status, 100*time.Millisecond, 300*time.Millisecond)
			s.clock.Set(c.clock)
			if c.failDelete {
				deletes := 0
				s.client.PrependReactor("delete", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
					if deletes++; deletes == 1 {
						return false, nil, nil // the delete of a leftover pod, which Start begins with
					}
					return true, nil, refuse
				})
			}
			read := make(chan []Event, 1)
			go func() { read <- allEvents(s.pod.Events()) }()
			ctx, cancel := c.ctx()
			defer cancel()
			_, err := s.pod.Start(ctx)
			switch {
			case c.code == "" && !errors.Is(err, context.DeadlineExceeded):
				t.Errorf("Start = %v, want the context's error", err)
			case c.code != "" && condition(err).Code != c.code:
				t.Errorf("Start = %v, want %s", err, c.code)
			}
			select {
			case <-read:
			case <-time.After(2 * time.Second):
				t.Fatal("Events was not closed when Start had failed")
			}
			if g := s.lastDeleteGrace(); g == nil || *g != c.grace {
				t.Errorf("the last delete of the pod had the grace period %v, want %d", g, c.grace)
			}
			before := len(s.client.Actions())
			_, stopErr := s.pod.Stop(context.Background())
			if n := len(s.client.Actions()); n != before {
				t.Errorf("Stop after a failed Start made %d more calls of the API, want none", n-before)
			}
			if c.failDelete != errors.Is(stopErr, refuse) {
				t.Errorf("Stop = %v; it gives the error of the delete that the failed Start made, and no other", stopErr)
			}
		})
	}
}

// A Start that failed before its pod was created ran nothing, so the probe did not fail to stop.
func TestAStartThatCreatedNoPodStoppedWhenAsked(t *testing.T) {
	s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 300*time.Millisecond)
	s.client.PrependReactor("create", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("the API server refused the pod")
	})
	go allEvents(s.pod.Events())
	if _, err := s.pod.Start(context.Background()); err == nil {
		t.Fatal("Start succeeded although the API server refused the pod")
	}
	if f, _ := s.pod.Stop(context.Background()); !f.StoppedWhenAsked {
		t.Errorf("facts %+v; a probe whose pod was never created stopped when asked", f)
	}
}
