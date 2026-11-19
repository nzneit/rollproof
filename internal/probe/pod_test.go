package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	goruntime "runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
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

const customImage = "registry.example/tools/busybox@sha256:6d9ac9237a84afe1516540f40a0fafdc86859b2141954b4d643af7066d598b74"

// customOptions is a custom probe's pod as section 6's example writes it.
func customOptions() PodOptions {
	return PodOptions{
		Image:   customImage,
		Command: []string{"sh", "-c", `trap 'exit 0' TERM; while true; do if nc -z -w1 "$TARGET" 5432; then echo '{"ok":true}'; else echo '{"ok":false,"class":"refused"}'; fi; sleep 0.1; done`},
		Env: []corev1.EnvVar{
			{Name: "TARGET", Value: "orders-db.orders"},
			{Name: "TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "probe-token"}, Key: "token"}}},
		},
		ServiceAccountToken: true,
		Drain:               10 * time.Second,
	}
}

func TestPodSpecCustom(t *testing.T) {
	got, err := yaml.Marshal(podSpec(customOptions()))
	if err != nil {
		t.Fatal(err)
	}
	const path = "testdata/pod-custom.golden.yaml"
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
}

// A custom probe's pod differs from the built-in probe's only in its image, command, args, env,
// drain and token (section 6).
func TestCustomPodKeepsTheBuiltInSettings(t *testing.T) {
	custom := podSpec(customOptions())
	builtIn := podSpec(PodOptions{Image: probeImage, Args: []string{"probe", "http"}, Drain: 7 * time.Second})
	if d := cmp.Diff(builtIn.ObjectMeta, custom.ObjectMeta); d != "" {
		t.Errorf("metadata differs (-built-in +custom):\n%s", d)
	}
	bc, cc := builtIn.Spec.Containers[0], custom.Spec.Containers[0]
	if cc.Name != "probe" || cc.ImagePullPolicy != corev1.PullIfNotPresent {
		t.Errorf("container %q, pull policy %q; want probe, IfNotPresent", cc.Name, cc.ImagePullPolicy)
	}
	if d := cmp.Diff(bc.SecurityContext, cc.SecurityContext); d != "" {
		t.Errorf("security context differs (-built-in +custom):\n%s", d)
	}
	if d := cmp.Diff(builtIn.Spec.NodeSelector, custom.Spec.NodeSelector); d != "" {
		t.Errorf("node selector differs:\n%s", d)
	}
	if d := cmp.Diff(builtIn.Spec.Tolerations, custom.Spec.Tolerations); d != "" {
		t.Errorf("tolerations differ:\n%s", d)
	}
	if custom.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("restart policy %q, want Never", custom.Spec.RestartPolicy)
	}
	if g := *custom.Spec.TerminationGracePeriodSeconds; g != 10 {
		t.Errorf("grace %d s, want the custom probe's drain, 10 s", g)
	}
	if !slices.Equal(cc.Command, customOptions().Command) || cc.Args != nil {
		t.Errorf("command %q, args %q; want the configured command and no args", cc.Command, cc.Args)
	}
	if d := cmp.Diff(customOptions().Env, cc.Env); d != "" {
		t.Errorf("env differs:\n%s", d)
	}
	if a := custom.Spec.AutomountServiceAccountToken; a == nil || !*a || custom.Spec.ServiceAccountName != "" {
		t.Errorf("token %v, service account %q; want the default service account's token mounted", a, custom.Spec.ServiceAccountName)
	}
	if a := builtIn.Spec.AutomountServiceAccountToken; a == nil || *a || bc.Command != nil || bc.Env != nil {
		t.Errorf("the built-in probe's pod has token %v, command %q, env %v; want none of them", a, bc.Command, bc.Env)
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

// evidenceFiles is an Evidence that keeps each file in memory.
type evidenceFiles struct {
	mu    sync.Mutex
	files map[string]*bytes.Buffer
}

type nopCloser struct{ *bytes.Buffer }

func (nopCloser) Close() error { return nil }

func (e *evidenceFiles) open(name string) (io.WriteCloser, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.files == nil {
		e.files = map[string]*bytes.Buffer{}
	}
	b := &bytes.Buffer{}
	e.files[name] = b
	return nopCloser{b}, nil
}

func (e *evidenceFiles) get(name string) (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	b, ok := e.files[name]
	if !ok {
		return "", false
	}
	return b.String(), true
}

// refusedBy makes the API server refuse every create of a pod with err.
func refusedBy(s probeSetup, err error) {
	s.client.PrependReactor("create", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, err
	})
}

// endsWhenDeleted gives the probe of s one log stream: text, and then nothing until the probe's
// pod is deleted, by Stop or by anyone else, when the stream ends with io.EOF, as a real pod's
// log ends when its container does (section 14, point 26). Call it before Start.
func (s probeSetup) endsWhenDeleted(t *testing.T, text string) {
	t.Helper()
	deleted, testEnded := make(chan struct{}), make(chan struct{})
	var once sync.Once
	pods := corev1.SchemeGroupVersion.WithResource("pods")
	s.client.PrependReactor("delete", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		if _, err := s.client.Tracker().Get(pods, cluster.ProbeNamespace, "probe"); err == nil {
			once.Do(func() { close(deleted) })
		}
		return false, nil, nil
	})
	pr, pw := io.Pipe()
	t.Cleanup(func() {
		close(testEnded)
		pw.Close()
	})
	go func() {
		io.WriteString(pw, text)
		select {
		case <-deleted:
		case <-testEnded:
		}
		pw.Close()
	}()
	s.pod.open = (&fakeLog{streams: []io.Reader{pr}}).open
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
	s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 5*time.Second)
	s.endsWhenDeleted(t, logLine(logAt(100), "starting")+logLine(logAt(400), `{"ok":true,"t":"`+early+`"}`))
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
	// The stream stayed open past the drain, so the log was not read to its end: the gap, which
	// the follower sends after the stop's cancel, reaches Events while it has room (section 6).
	checkGap(t, allEvents(s.pod.Events()), 1, []time.Time{logAt(400), logAt(0)}, 0)
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
			s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 5*time.Second)
			s.endsWhenDeleted(t, logLine(logAt(400), `{"ok":true}`))
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
	s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 5*time.Second)
	s.endsWhenDeleted(t, logLine(logAt(400), `{"ok":true}`))
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
	s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 5*time.Second)
	s.endsWhenDeleted(t, log.String())
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

func TestStartRefusedPod(t *testing.T) {
	gr := schema.GroupResource{Resource: "pods"}
	for _, c := range []struct {
		name string
		err  error
		want string
	}{
		{"Pod Security", apierrors.NewForbidden(gr, "probe",
			errors.New(`violates PodSecurity "restricted:latest": runAsNonRoot != true`)),
			`violates PodSecurity "restricted:latest": runAsNonRoot != true`},
		{"validation", apierrors.NewInvalid(schema.GroupKind{Kind: "Pod"}, "probe", field.ErrorList{
			field.Invalid(field.NewPath("spec", "containers").Index(0).Child("env").Index(0).Child("valueFrom"), "",
				"may not have more than one field specified at a time")}),
			"may not have more than one field specified at a time"},
		{"a bad request", apierrors.NewBadRequest("the pod is not one the server reads"), "the pod is not one the server reads"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 10*time.Second)
			refusedBy(s, c.err)
			began := time.Now()
			_, err := s.pod.Start(context.Background())
			got := condition(err)
			if got.Code != verdict.ProbeNotStarted || !strings.HasPrefix(got.Text, "the API server refused the probe's pod: ") ||
				!strings.Contains(got.Text, c.want) {
				t.Errorf("Start = %v, want probe-not-started with the API server's text", err)
			}
			if time.Since(began) > 2*time.Second {
				t.Error("Start waited for StartTimeout")
			}
			if _, open := <-s.pod.Events(); open {
				t.Error("Events is still open")
			}
			if facts, _ := s.pod.Stop(context.Background()); !facts.StoppedWhenAsked {
				t.Error("a probe that never ran did not stop when asked")
			}
		})
	}
}

// A create that fails for another reason than a refusal stays the API's error, which run reports
// as a failure of the tool.
func TestStartCreateFailsOtherwise(t *testing.T) {
	s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 10*time.Second)
	refusedBy(s, apierrors.NewServiceUnavailable("etcd is not answering"))
	_, err := s.pod.Start(context.Background())
	if err == nil || condition(err).Code != "" || !strings.Contains(err.Error(), "etcd is not answering") {
		t.Errorf("Start = %v, want the API's error and no Condition", err)
	}
}

// Review Focus 2: a custom probe's env reads a Secret that does not exist.
func TestStartNamesAMissingSecret(t *testing.T) {
	status := corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: []corev1.ContainerStatus{{
		Name: "probe", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
			Reason: "CreateContainerConfigError", Message: `secret "probe-token" not found`}},
	}}}
	s := newProbe(t, &fakeLog{}, status, 100*time.Millisecond, 300*time.Millisecond)
	_, err := s.pod.Start(context.Background())
	got := condition(err)
	if got.Code != verdict.ProbeNotStarted ||
		got.Text != `no observation within limits.timeout; the pod is Pending; container probe is waiting: CreateContainerConfigError: secret "probe-token" not found` {
		t.Errorf("Start = %v, want probe-not-started that names the Secret's error", err)
	}
}

// withSidecars makes every created probe pod carry a mesh's init container and proxy, as an
// injector would add them.
func withSidecars(s probeSetup) {
	s.client.PrependReactor("create", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		p := a.(k8stesting.CreateAction).GetObject().(*corev1.Pod)
		p.Spec.InitContainers = append(p.Spec.InitContainers, corev1.Container{Name: "istio-init"})
		p.Spec.Containers = append(p.Spec.Containers, corev1.Container{Name: "istio-proxy"})
		return false, nil, nil
	})
}

func TestStopWritesThePodAndItsOtherContainers(t *testing.T) {
	s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 5*time.Second)
	withSidecars(s)
	var ev evidenceFiles
	s.pod.o.Evidence = ev.open
	s.endsWhenDeleted(t, logLine(logAt(400), `{"ok":true}`))
	s.clock.Set(logAt(700))
	if _, err := s.pod.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := ev.get("probe-pod.json"); ok {
		t.Error("probe-pod.json was written before the stop")
	}
	if _, err := s.pod.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The fake API removes the pod at its delete, so a pod in the file was read before the delete.
	pod, ok := ev.get("probe-pod.json")
	var read corev1.Pod
	if !ok || json.Unmarshal([]byte(pod), &read) != nil || read.Name != "probe" || read.Status.Phase != corev1.PodRunning {
		t.Errorf("probe-pod.json = %q, want the pod as the API gave it before its delete", pod)
	}
	for _, name := range []string{"probe-pod/istio-init.log", "probe-pod/istio-proxy.log"} {
		if got, ok := ev.get(name); got != "fake logs" {
			t.Errorf("%s = %q, %v; want the container's log", name, got, ok)
		}
	}
	if _, ok := ev.get("probe-pod/probe.log"); ok {
		t.Error("the probe's own container was written to probe-pod/; it belongs in probe.log")
	}
}

func TestStartTimeoutWritesThePod(t *testing.T) {
	status := corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: []corev1.ContainerStatus{{
		Name: "probe", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}},
	}}}
	s := newProbe(t, &fakeLog{}, status, 100*time.Millisecond, 300*time.Millisecond)
	var ev evidenceFiles
	s.pod.o.Evidence = ev.open
	if _, err := s.pod.Start(context.Background()); condition(err).Code != verdict.ProbeNotStarted {
		t.Fatalf("Start = %v, want probe-not-started", err)
	}
	if got, _ := ev.get("probe-pod.json"); !strings.Contains(got, "ImagePullBackOff") {
		t.Errorf("probe-pod.json = %q, want the pod's status with the waiting reason", got)
	}
}

func TestPodEvidenceThatCannotBeRead(t *testing.T) {
	s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 5*time.Second)
	withSidecars(s)
	var ev evidenceFiles
	s.pod.o.Evidence = ev.open
	s.client.PrependReactor("get", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() == "log" {
			return true, nil, errors.New("the pod's log is gone")
		}
		return false, nil, nil
	})
	s.endsWhenDeleted(t, logLine(logAt(400), `{"ok":true}`))
	s.clock.Set(logAt(700))
	if _, err := s.pod.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pod.Stop(context.Background()); err != nil {
		t.Errorf("Stop = %v; evidence that cannot be read is no error of the stop", err)
	}
	if got, _ := ev.get("probe-pod/istio-proxy.log"); !strings.HasPrefix(got, "rollproof: the log could not be read: ") ||
		!strings.Contains(got, "the pod's log is gone") {
		t.Errorf("probe-pod/istio-proxy.log = %q, want the reason", got)
	}
}

func TestRefusedPodWritesNoEvidence(t *testing.T) {
	s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 5*time.Second)
	refusedBy(s, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "probe", errors.New("denied")))
	var ev evidenceFiles
	s.pod.o.Evidence = ev.open
	s.pod.Start(context.Background())
	s.pod.Stop(context.Background())
	if len(ev.files) != 0 {
		t.Errorf("evidence %v, want none for a pod that never existed", ev.files)
	}
}

// closedWithin reads events until it is closed, and fails the test when that takes longer than d.
func closedWithin(t *testing.T, events <-chan Event, d time.Duration) {
	t.Helper()
	timeout := time.After(d)
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatalf("Events was not closed within %v", d)
		}
	}
}

// A Stop before Start closes Events, so that its reader ends, and ends the Pod: a later Start
// gives an error and creates no pod.
func TestStopBeforeStartClosesEvents(t *testing.T) {
	s := newProbe(t, newFakeLog(logLine(logAt(400), `{"ok":true}`)), runningStatus(), 100*time.Millisecond, 5*time.Second)
	if f, err := s.pod.Stop(context.Background()); err != nil || f != (Facts{}) {
		t.Errorf("Stop before Start = %+v, %v; want no facts and no error", f, err)
	}
	closedWithin(t, s.pod.Events(), time.Second)
	if f, err := s.pod.Stop(context.Background()); err != nil || f != (Facts{}) {
		t.Errorf("second Stop = %+v, %v; want no facts and no error", f, err)
	}
	if _, err := s.pod.Start(context.Background()); err == nil {
		t.Error("Start after Stop succeeded, want an error")
	}
	for _, a := range s.client.Actions() {
		if a.GetVerb() == "create" {
			t.Errorf("Start after Stop created %s", a.GetResource().Resource)
		}
	}
}

func TestEventsClosedWhenNoPodWasCreated(t *testing.T) {
	s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 300*time.Millisecond)
	s.client.PrependReactor("create", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("the API server refused the pod")
	})
	if _, err := s.pod.Start(context.Background()); err == nil {
		t.Fatal("Start succeeded although the API server refused the pod")
	}
	closedWithin(t, s.pod.Events(), time.Second)
}

// A Start that panics once the follower and the watcher run leaves them to Stop, which stops
// them as it stops a started probe, and closes Events.
func TestStopAfterAStartThatPanicked(t *testing.T) {
	const drain = 100 * time.Millisecond
	s := newProbe(t, newFakeLog(logLine(logAt(400), `{"ok":true}`)), runningStatus(), drain, 5*time.Second)
	old := afterRunStarted
	afterRunStarted = func() { panic("a fault of Start") }
	t.Cleanup(func() { afterRunStarted = old })
	before := goruntime.NumGoroutine()
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("Start did not panic")
			}
		}()
		s.pod.Start(context.Background())
	}()
	began := time.Now()
	s.pod.Stop(context.Background())
	if took, limit := time.Since(began), drain+stopMargin+time.Second; took > limit {
		t.Errorf("Stop took %v, want at most %v", took, limit)
	}
	closedWithin(t, s.pod.Events(), time.Second)
	for deadline := time.Now().Add(2 * time.Second); goruntime.NumGoroutine() > before; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%d goroutines 2 s after Stop, %d before Start", goruntime.NumGoroutine(), before)
		}
	}
}

// A Start that panics before its run exists, in the create of its pod, followed nothing: Stop
// gives an error, so that the stop is not read as one that the probe obeyed, and gives it again
// at a second call. It closes Events.
func TestStopAfterAStartThatPanickedBeforeItsRun(t *testing.T) {
	s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 5*time.Second)
	s.client.PrependReactor("create", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		panic("a fault of the API client")
	})
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("Start did not panic")
			}
		}()
		s.pod.Start(context.Background())
	}()
	const want = "the probe's start failed before its log was followed"
	for _, call := range []string{"Stop", "second Stop"} {
		f, err := s.pod.Stop(context.Background())
		if err == nil || err.Error() != want || f != (Facts{}) {
			t.Errorf("%s = %+v, %v; want no facts and the error %q", call, f, err, want)
		}
	}
	closedWithin(t, s.pod.Events(), time.Second)
}

// slowClient is a client whose pod gets and deletes, once slow is set, wait until their context
// ends. client-go's fake gives its reactors no context, and holds its lock while one runs.
type slowClient struct {
	kubernetes.Interface
	slow *atomic.Bool
}

func (c slowClient) CoreV1() typedcorev1.CoreV1Interface {
	return slowCore{c.Interface.CoreV1(), c.slow}
}

type slowCore struct {
	typedcorev1.CoreV1Interface
	slow *atomic.Bool
}

func (c slowCore) Pods(namespace string) typedcorev1.PodInterface {
	return slowPods{c.CoreV1Interface.Pods(namespace), c.slow}
}

type slowPods struct {
	typedcorev1.PodInterface
	slow *atomic.Bool
}

func (p slowPods) Get(ctx context.Context, name string, o metav1.GetOptions) (*corev1.Pod, error) {
	if p.slow.Load() {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return p.PodInterface.Get(ctx, name, o)
}

func (p slowPods) Delete(ctx context.Context, name string, o metav1.DeleteOptions) error {
	if p.slow.Load() {
		<-ctx.Done()
		return ctx.Err()
	}
	return p.PodInterface.Delete(ctx, name, o)
}

// An API server that hangs does not hold the stop: the last reading of the pod and the delete
// each wait at most stopMargin, and the log is read for at most the drain and stopMargin.
func TestStopBoundsItsAPICalls(t *testing.T) {
	const drain = 200 * time.Millisecond
	l := &fakeLog{streams: []io.Reader{openStream(t, logLine(logAt(400), `{"ok":true}`))}}
	s := newProbe(t, l, runningStatus(), drain, 5*time.Second)
	old := stopMargin
	stopMargin = 100 * time.Millisecond
	t.Cleanup(func() { stopMargin = old })
	var slow atomic.Bool
	s.pod.o.Client = slowClient{Interface: s.client, slow: &slow}
	s.clock.Set(logAt(700))
	if _, err := s.pod.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	slow.Store(true) // after Start, so that neither Start nor the watcher's first reading waits
	limit := drain + 3*stopMargin + 500*time.Millisecond
	done := make(chan error, 1)
	go func() {
		_, err := s.pod.Stop(context.Background())
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Stop = %v, want the error of the delete that ran out of time", err)
		}
	case <-time.After(limit):
		t.Fatalf("Stop did not return within %v", limit)
	}
	closedWithin(t, s.pod.Events(), time.Second)
}

// Counts gives the lines read so far: zeros before Start, the lines read while the probe runs,
// and after Stop the counts of its Facts.
func TestCountsWhileTheProbeRuns(t *testing.T) {
	l := &fakeLog{streams: []io.Reader{openStream(t, logLine(logAt(100), `{"level":"info"}`)+logLine(logAt(400), `{"ok":true}`))}}
	s := newProbe(t, l, runningStatus(), 100*time.Millisecond, 5*time.Second)
	s.clock.Set(logAt(700))
	if lines, jsonText := s.pod.Counts(); lines != 0 || jsonText != 0 {
		t.Errorf("Counts before Start = %d, %d; want zeros", lines, jsonText)
	}
	if _, err := s.pod.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if lines, jsonText := s.pod.Counts(); lines != 2 || jsonText != 1 {
		t.Errorf("Counts while the probe runs = %d, %d; want 2 lines, 1 JSON text", lines, jsonText)
	}
	facts, err := s.pod.Stop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if lines, jsonText := s.pod.Counts(); lines != facts.Lines || jsonText != facts.JSONTextLines || lines != 2 {
		t.Errorf("Counts after Stop = %d, %d; want the Facts' %d, %d, and 2 lines", lines, jsonText, facts.Lines, facts.JSONTextLines)
	}
}

// Counts survives a Stop that panicked: the run's entry still gives the lines read. The panic
// leaves nothing of the run behind: the follower has ended and Events is closed.
func TestCountsAfterAStopThatPanicked(t *testing.T) {
	l := &fakeLog{streams: []io.Reader{openStream(t, logLine(logAt(100), "starting")+logLine(logAt(400), `{"ok":true}`))}}
	s := newProbe(t, l, runningStatus(), 100*time.Millisecond, 5*time.Second)
	var armed atomic.Bool
	s.client.PrependReactor("delete", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		if armed.Load() {
			panic("a fault of the API client")
		}
		return false, nil, nil
	})
	s.clock.Set(logAt(700))
	if _, err := s.pod.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	armed.Store(true)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("Stop did not panic")
			}
		}()
		s.pod.Stop(context.Background())
	}()
	if lines, _ := s.pod.Counts(); lines != 2 {
		t.Errorf("Counts after a Stop that panicked = %d lines, want 2", lines)
	}
	select {
	case <-s.pod.run.followerDone:
	default:
		t.Error("the follower still runs after the Stop that panicked")
	}
	closedWithin(t, s.pod.Events(), time.Second)
}

// The log ends inside the drain, but the pod reads the container as running until after the
// stop's wait ran out (section 14, point 26): the probe stopped when asked. The log was not read
// to the end that the pod's status confirms, so the gap still ends Events.
func TestStopWhileThePodsStatusLags(t *testing.T) {
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	s := newProbe(t, &fakeLog{streams: []io.Reader{pr}}, runningStatus(), 200*time.Millisecond, 5*time.Second)
	var lagging atomic.Bool
	deleted := make(chan struct{})
	var once sync.Once
	s.client.PrependReactor("delete", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		if !lagging.Load() {
			return false, nil, nil
		}
		once.Do(func() { close(deleted) })
		return true, nil, nil // the pod stays, and reads as running
	})
	s.client.PrependReactor("get", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		if lagging.Load() {
			s.clock.Set(s.clock.Now().Add(time.Second)) // each reading of the pod takes the clock on
		}
		return false, nil, nil
	})
	s.clock.Set(logAt(700))
	go io.WriteString(pw, logLine(logAt(400), `{"ok":true}`))
	if _, err := s.pod.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	lagging.Store(true)
	done := make(chan Facts, 1)
	go func() {
		f, _ := s.pod.Stop(context.Background())
		done <- f
	}()
	select {
	case <-deleted: // the stop was asked
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not delete the pod")
	}
	io.WriteString(pw, logLine(logAt(900), `{"ok":true}`))
	pw.Close() // the stream ends inside the drain, by the clock
	var facts Facts
	select {
	case facts = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return")
	}
	if !facts.StoppedWhenAsked {
		t.Errorf("facts %+v; the log ended inside the drain, so the probe stopped when asked", facts)
	}
	events := allEvents(s.pod.Events())
	if len(events) != 4 || events[0].Obs == nil || events[1].Obs == nil || !events[1].Obs.Written.Equal(logAt(900)) {
		t.Fatalf("events = %+v, want both observations and the gap's two faults", events)
	}
	for i, e := range events[2:] {
		if f := e.Fault; f == nil || f.Code != verdict.InternalError || f.Text != gapText || (i == 0 && !f.At.Equal(logAt(900))) {
			t.Errorf("event %d = %+v, want the gap, first at the last line read", i+2, e)
		}
	}
}

// holdStopDeletes makes the stop's delete of the probe's pod, the one with a grace period, keep
// the pod, which reads as it did, and calls held when held is not nil. The replace that Start
// makes, with a grace period of 0, deletes as the fake client does.
func (s probeSetup) holdStopDeletes(held func()) {
	s.client.PrependReactor("delete", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		g := a.(k8stesting.DeleteAction).GetDeleteOptions().GracePeriodSeconds
		if g == nil || *g == 0 {
			return false, nil, nil
		}
		if held != nil {
			held()
		}
		return true, nil, nil
	})
}

// No stream's end was the end of the log when every opening after the last stream failed, or
// when none opened: the probe did not stop when asked, whatever the moment the last stream ended
// (section 6).
func TestStopWhenNoStreamEndedTheLog(t *testing.T) {
	t.Run("a stream ends before the ask, and every opening fails until the pod is gone after the drain", func(t *testing.T) {
		const drain = 200 * time.Millisecond
		s := newProbe(t, &fakeLog{}, runningStatus(), drain, 5*time.Second)
		failing := make(chan struct{})
		var opens atomic.Int32
		var once sync.Once
		s.pod.open = func(context.Context, *time.Time) (io.ReadCloser, error) {
			if opens.Add(1) == 1 {
				return io.NopCloser(strings.NewReader(logLine(logAt(400), `{"ok":true}`))), nil
			}
			once.Do(func() { close(failing) })
			return nil, errors.New("the API server refused the log")
		}
		var deleted atomic.Bool
		s.holdStopDeletes(func() { deleted.Store(true) })
		s.client.PrependReactor("get", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
			if !deleted.Load() {
				return false, nil, nil
			}
			// The first reading after the stop's delete finds the pod gone, a second after the
			// ask by the clock, which is after the drain.
			s.clock.Set(logAt(1700))
			return true, nil, apierrors.NewNotFound(corev1.Resource("pods"), "probe")
		})
		s.clock.Set(logAt(700))
		if _, err := s.pod.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		select {
		case <-failing: // the stream ended before the ask, and the reopen it owes fails
		case <-time.After(2 * time.Second):
			t.Fatal("the log was not opened again")
		}
		go allEvents(s.pod.Events())
		f, err := s.pod.Stop(context.Background())
		if err != nil || f.StoppedWhenAsked {
			t.Errorf("Stop = %+v, %v; want StoppedWhenAsked false and no error", f, err)
		}
	})
	t.Run("no opening succeeds, and the wait runs out", func(t *testing.T) {
		s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 100*time.Millisecond)
		s.pod.open = func(context.Context, *time.Time) (io.ReadCloser, error) {
			return nil, errors.New("the API server refused the log")
		}
		s.holdStopDeletes(nil)
		go allEvents(s.pod.Events())
		if _, err := s.pod.Start(context.Background()); condition(err).Code != verdict.ProbeNotStarted {
			t.Fatalf("Start = %v, want probe-not-started", err)
		}
		f, err := s.pod.Stop(context.Background())
		if err != nil || f.StoppedWhenAsked {
			t.Errorf("Stop after the failed Start = %+v, %v; want StoppedWhenAsked false and no error", f, err)
		}
	})
}

// An event goes into Events while it has room, also after the run's context ended: the gap that
// the follower emits when the stop cuts the log is not lost to a random choice. With no room,
// the send gives up once the context has ended.
func TestSendAfterTheContextEnded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &probeRun{events: make(chan Event, 1)}
	for i := range 100 { // one send alone would pass half the time with a plain select
		if !r.send(ctx, Event{}) {
			t.Fatalf("send %d gave up with room in the channel", i)
		}
		<-r.events
	}
	r.events <- Event{}
	if r.send(ctx, Event{}) {
		t.Error("send went into a full channel")
	}
}
