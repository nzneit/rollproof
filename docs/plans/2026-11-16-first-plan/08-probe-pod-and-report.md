> Part of the plan in `README.md` of this folder. Read that file first: its Global Constraints apply to every task here.

### Task 29: `probe`: the probe's pod

**Files:**
- Create: `internal/probe/pod.go`, `internal/probe/follow.go`
- Test: `internal/probe/follow_test.go`, `internal/probe/pod_test.go`, `internal/probe/testdata/pod-http.golden.yaml`, `internal/probe/pod_cluster_test.go` (build tag `cluster`)
- Modify: `docs/proofs/README.md` (the clock check's measurement)

**Interfaces:**
- Consumes: `func ParseLine(raw string, written time.Time) Line` (Task 15); `func HTTPArgs(o HTTPOptions) []string` (Task 16); `const ProbeNamespace` (Task 20); `type Observation`, `type Condition` with ProbeNotStarted, ProbeExited, ProbeLineInvalid, RunnerUnsupported, InternalError (Task 12); the reopening rule that `docs/proofs/p11-log-rotation.md` records (Task 9); `func Create` and `func Store` in the cluster test (Tasks 21 and 22).
- Produces: `type Event struct { Obs *verdict.Observation; Fault *verdict.Condition }`
  One thing a running probe reported: an observation, or a fault of the
  probe with the moment it happened. Exactly one is set.
- Produces: `type Started struct { First time.Time; Events <-chan Event }`
  A probe that has started. First is when its first observation was
  written, which is where the steady window begins. Events carries every
  observation from the first on, and every fault, until Stop has returned.
- Produces: `type Facts struct { Lines, JSONTextLines int; StoppedWhenAsked bool; ClockDifference time.Duration }`
  What the report says of a probe besides its results: how many lines it
  wrote, how many of them were JSON objects with no ok field, whether it
  exited inside the drain, and what the clock check measured.
- Produces: `type Probe interface { Start(ctx context.Context) (Started, error); Stop(ctx context.Context) (Facts, error) }`
  One probe of a run. Start returns when the first observation has
  arrived; its error is a Condition: the probe did not start in time, it
  exited first, or the nodes' clock differs from the runner's. Stop asks
  the probe to stop, reads what it still writes, and closes Events.
- Produces: `const ClockBound = 2 * time.Second`
  The largest difference, either way, between the runtime's timestamp of
  the first observation and the moment the tool read it.
- Produces: `type PodOptions struct { Client kubernetes.Interface; Image string; Args []string; Drain, StartTimeout time.Duration; Raw io.Writer; Now func() time.Time }`
  What the probe's pod needs: its image by digest, its arguments, the
  drain, limits.timeout for the start, and where every raw line goes with
  its timestamp.
- Produces: `func NewPod(o PodOptions) *Pod`
  A probe that runs as a pod on the control-plane node. It implements
  Probe.

This task's reopening rule is the one this plan assumed for `docs/proofs/p11-log-rotation.md` (Task 9). Read that record first. If it states another rule, the record wins: change rule 4 of Step 8 and the two tests that pin it (`TestFollowReopensWithoutLossOrRepeat`, `TestFollowGap`), and say so in the commit message.

- [ ] **Step 1: Write the golden file of the probe's pod**

`internal/probe/testdata/pod-http.golden.yaml`, as `sigs.k8s.io/yaml` writes the pod (keys sorted, one line end at the end):

```yaml
apiVersion: v1
kind: Pod
metadata:
  labels:
    app.kubernetes.io/name: rollproof-probe
  name: probe
  namespace: rollproof-probe
spec:
  automountServiceAccountToken: false
  containers:
  - args:
    - probe
    - http
    - --url
    - http://orders.orders.svc/api/orders/latest
    - --rate
    - "200"
    - --timeout
    - 2s
    - --expect-status
    - 200-299
    image: rollproof.test/rollproof@sha256:1333aec6e2215a19bb2a7361377cf1cb27f1261bcf1ddae03e4ea086c8bab89c
    imagePullPolicy: IfNotPresent
    name: probe
    resources: {}
    securityContext:
      allowPrivilegeEscalation: false
      capabilities:
        drop:
        - ALL
      runAsNonRoot: true
      runAsUser: 65532
      seccompProfile:
        type: RuntimeDefault
  nodeSelector:
    node-role.kubernetes.io/control-plane: ""
  restartPolicy: Never
  terminationGracePeriodSeconds: 7
  tolerations:
  - effect: NoSchedule
    key: node-role.kubernetes.io/control-plane
    operator: Exists
status: {}
```

- [ ] **Step 2: Write the failing test of the pod**

`internal/probe/pod_test.go`:

```go
package probe

import (
	"bytes"
	"flag"
	"os"
	"testing"
	"time"

	"sigs.k8s.io/yaml"
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
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/probe/ -run TestPodSpec -v`
Expected: FAIL: `undefined: PodOptions`

- [ ] **Step 4: Implement `type PodOptions` and `func podSpec(o PodOptions) *corev1.Pod` in `internal/probe/pod.go`**

`PodOptions` with its interface comment from the Interfaces block. `podSpec` is unexported, with the comment "podSpec is the probe's pod as Start creates it." It sets exactly the fields of the golden file and nothing else: `TypeMeta` v1 Pod; name `probe`, namespace `cluster.ProbeNamespace`, the label `app.kubernetes.io/name: rollproof-probe`; `AutomountServiceAccountToken` false; one container named `probe` with `o.Image`, `o.Args`, `IfNotPresent` and a container security context of `RunAsNonRoot` true, `RunAsUser` 65532, `AllowPrivilegeEscalation` false, capabilities drop `ALL`, seccomp `RuntimeDefault`; the node selector `node-role.kubernetes.io/control-plane: ""`; restart policy `Never`; `TerminationGracePeriodSeconds` the drain in whole seconds rounded up, `int64((o.Drain + time.Second - 1) / time.Second)`; one toleration with that key, operator `Exists` and effect `NoSchedule`.

If `go.mod` does not list `sigs.k8s.io/yaml` yet, run `go mod tidy`.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/probe/ -run TestPodSpec -v`
Expected: PASS. If the only difference is how a scalar is quoted, read the diff, and when the pod is the same, rewrite the file with `-update` and look at it again.

- [ ] **Step 6: Write the failing tests of the follower**

`internal/probe/follow_test.go`. The fake log serves prepared streams and records the `since` of each opening; `pod_test.go` uses it too.

```go
package probe

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"rollproof/internal/verdict"
)

var logStart = time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)

// logAt is logStart plus ms milliseconds.
func logAt(ms int) time.Time { return logStart.Add(time.Duration(ms) * time.Millisecond) }

// logLine is one line of a log stream as the API gives it with timestamps on.
func logLine(ts time.Time, text string) string {
	return ts.Format(time.RFC3339Nano) + " " + text + "\n"
}

// fakeLog serves its streams in order, one for each opening, and records the
// since of each opening. A nil stream makes its opening fail. An opening
// after the last stream gives a stream that blocks until its ctx ends.
type fakeLog struct {
	mu      sync.Mutex
	streams []io.Reader
	opened  []*time.Time
}

func newFakeLog(texts ...string) *fakeLog {
	l := &fakeLog{}
	for _, s := range texts {
		l.streams = append(l.streams, strings.NewReader(s))
	}
	return l
}

func (l *fakeLog) open(ctx context.Context, since *time.Time) (io.ReadCloser, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.opened = append(l.opened, since)
	i := len(l.opened) - 1
	switch {
	case i >= len(l.streams):
		return io.NopCloser(blocked{ctx}), nil
	case l.streams[i] == nil:
		return nil, errors.New("container is waiting to start")
	}
	return io.NopCloser(l.streams[i]), nil
}

// running is true while a stream is left to serve.
func (l *fakeLog) running(context.Context) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.opened) < len(l.streams)
}

// blocked gives nothing until its ctx ends.
type blocked struct{ ctx context.Context }

func (b blocked) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

// quickPolling makes the package's waits short for one test.
func quickPolling(t *testing.T) {
	t.Helper()
	old := pollEvery
	pollEvery = 10 * time.Millisecond
	t.Cleanup(func() { pollEvery = old })
}

// followAll runs a follower over l until the log has ended for good.
func followAll(t *testing.T, l *fakeLog, stopAsked bool) (f *follower, events []Event, raw string) {
	t.Helper()
	quickPolling(t)
	var buf bytes.Buffer
	now := func() time.Time { return logAt(60_000) }
	f = newFollower(l.open, l.running, &buf, now, func(e Event) { events = append(events, e) })
	if stopAsked {
		f.stopAsked()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f.run(ctx)
	if ctx.Err() != nil {
		t.Fatal("the follower did not end by itself")
	}
	return f, events, buf.String()
}

func TestFollowLineKinds(t *testing.T) {
	stream := logLine(logAt(100), "starting") +
		logLine(logAt(200), `{"level":"info","msg":"listening"}`) +
		logLine(logAt(300), `{"ok":true}`) +
		logLine(logAt(400), `{"ok":"yes"}`) +
		logLine(logAt(500), `{"ok":false,"class":"refused"}`)
	f, events, raw := followAll(t, newFakeLog(stream), false)
	if raw != stream {
		t.Errorf("raw = %q, want the stream as it came", raw)
	}
	if len(events) != 3 {
		t.Fatalf("%d events, want 3: %+v", len(events), events)
	}
	if o := events[0].Obs; o == nil || !o.OK || !o.Written.Equal(logAt(300)) {
		t.Errorf("event 0 = %+v, want the pass written at +300 ms", events[0])
	}
	if c := events[1].Fault; c == nil || c.Code != verdict.ProbeLineInvalid || !c.At.Equal(logAt(400)) || c.Text == "" {
		t.Errorf("event 1 = %+v, want probe-line-invalid at +400 ms with the rule it broke", events[1])
	}
	if o := events[2].Obs; o == nil || o.OK || o.Class != "refused" {
		t.Errorf("event 2 = %+v, want the failure", events[2])
	}
	if lines, jsonText := f.counts(); lines != 5 || jsonText != 1 {
		t.Errorf("counts = %d, %d; want 5 lines, 1 JSON text", lines, jsonText)
	}
}

func TestFollowLongLine(t *testing.T) {
	long := `{"ok":true,"detail":"` + strings.Repeat("x", 100_000) + `"}`
	_, events, _ := followAll(t, newFakeLog(logLine(logAt(100), long)), false)
	if len(events) != 1 || events[0].Obs == nil {
		t.Errorf("events = %d, want one observation", len(events))
	}
}

// Review Focus 5: two identical failures with one timestamp, a break, and a
// stream that repeats both and adds a third and a fourth.
func TestFollowReopensWithoutLossOrRepeat(t *testing.T) {
	fail := `{"ok":false,"class":"refused"}`
	same := logAt(1500)
	l := newFakeLog(
		logLine(same, fail)+logLine(same, fail),
		logLine(same, fail)+logLine(same, fail)+logLine(same, fail)+logLine(logAt(2700), fail),
	)
	f, events, raw := followAll(t, l, false)
	failures := 0
	for _, e := range events {
		if e.Fault != nil {
			t.Errorf("fault %+v", *e.Fault)
		}
		if e.Obs != nil && !e.Obs.OK {
			failures++
		}
	}
	if failures != 4 {
		t.Errorf("%d failures, want 4", failures)
	}
	if len(l.opened) != 2 || l.opened[0] != nil || l.opened[1] == nil || !l.opened[1].Equal(logAt(1000)) {
		t.Errorf("openings since %v, want nil and then %v", l.opened, logAt(1000))
	}
	if n := strings.Count(raw, "\n"); n != 4 {
		t.Errorf("raw has %d lines, want 4: a repeated line is not written again", n)
	}
	if lines, _ := f.counts(); lines != 4 {
		t.Errorf("lines = %d, want 4", lines)
	}
}

func TestFollowRetriesAnOpeningThatFails(t *testing.T) {
	l := &fakeLog{streams: []io.Reader{nil, nil, strings.NewReader(logLine(logAt(100), `{"ok":true}`))}}
	_, events, _ := followAll(t, l, false)
	if len(events) != 1 || len(l.opened) != 3 {
		t.Errorf("%d events after %d openings, want 1 after 3", len(events), len(l.opened))
	}
	for i, since := range l.opened {
		if since != nil {
			t.Errorf("opening %d since %v, want from the start", i, *since)
		}
	}
}

func TestFollowGap(t *testing.T) {
	ok := `{"ok":true}`
	for _, c := range []struct {
		name          string
		first, second string
	}{
		{"the first line after the break is later than the last line read",
			logLine(logAt(1200), ok) + logLine(logAt(1700), ok),
			logLine(logAt(3700), ok)},
		{"fewer lines of the last timestamp came back than were read",
			logLine(logAt(1700), ok) + logLine(logAt(1700), ok),
			logLine(logAt(1700), ok) + logLine(logAt(2900), ok)},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, events, _ := followAll(t, newFakeLog(c.first, c.second), false)
			if len(events) != 4 {
				t.Fatalf("%d events, want 4: %+v", len(events), events)
			}
			if f := events[2].Fault; f == nil || f.Code != verdict.InternalError || !f.At.Equal(logAt(60_000)) ||
				f.Text != "the probe's log could not be followed without a gap" {
				t.Errorf("event 2 = %+v, want internal-error for the gap, at now", events[2])
			}
			if events[3].Obs == nil {
				t.Errorf("event 3 = %+v, want the line after the gap", events[3])
			}
		})
	}
}

func TestFollowLastPiece(t *testing.T) {
	piece := logAt(200).Format(time.RFC3339Nano) + ` {"ok":false,"class":"eof"}`
	stream := logLine(logAt(100), `{"ok":true}`) + piece
	for _, c := range []struct {
		name      string
		stopAsked bool
		events    int
	}{
		{"before Stop it is a line", false, 2},
		{"after Stop it is kept and not judged", true, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, events, raw := followAll(t, newFakeLog(stream), c.stopAsked)
			if len(events) != c.events {
				t.Errorf("%d events, want %d", len(events), c.events)
			}
			if lines, _ := f.counts(); lines != c.events {
				t.Errorf("lines = %d, want %d", lines, c.events)
			}
			if !strings.HasSuffix(raw, piece+"\n") {
				t.Errorf("raw = %q, want it to end with the piece", raw)
			}
		})
	}
}

func TestFollowLineWithoutTimestamp(t *testing.T) {
	_, events, raw := followAll(t, newFakeLog("no timestamp here\n"), false)
	if len(events) != 1 || events[0].Fault == nil || events[0].Fault.Code != verdict.InternalError ||
		events[0].Fault.Text != "the probe's log has a line with no timestamp" || !events[0].Fault.At.Equal(logAt(60_000)) {
		t.Errorf("events = %+v, want one internal-error for the line, at now", events)
	}
	if raw != "no timestamp here\n" {
		t.Errorf("raw = %q", raw)
	}
}
```

- [ ] **Step 7: Run the tests to verify they fail**

Run: `go test ./internal/probe/ -run TestFollow -v`
Expected: FAIL: the package does not build, with `undefined:` errors for `pollEvery`, `follower` and `Event`

- [ ] **Step 8: Implement `type Event` and the follower in `internal/probe/follow.go`**

`Event` with its interface comment from the Interfaces block. The rest is unexported, with these declarations and comments:

```go
// pollEvery is how often the probe's pod is read, and how long a failed
// opening of its log waits before the next.
var pollEvery = 200 * time.Millisecond

// logSource opens the log of the container named probe, following, with
// timestamps, from since, or from its start when since is nil.
type logSource func(ctx context.Context, since *time.Time) (io.ReadCloser, error)

// newFollower returns a follower of one probe's log. running says whether
// the container named probe still runs; raw receives every line as it came;
// now gives the moment of a fault that has no time of its own; emit receives
// every observation and every fault, in the order of the log.
func newFollower(open logSource, running func(ctx context.Context) bool, raw io.Writer, now func() time.Time, emit func(Event)) *follower

// run follows the log until it has ended for good or ctx ends.
func (f *follower) run(ctx context.Context)

// stopAsked tells the follower that Stop was asked: from then on a last
// piece with no line end is kept in raw and not read as a line.
func (f *follower) stopAsked()

// counts gives how many lines were read, and how many of them were JSON
// objects with no ok field. It is safe to call while run runs.
func (f *follower) counts() (lines, jsonText int)
```

The rules of `run`:

1. It opens the log with `since` nil until a line has been read, and after that with `since` the timestamp of the last line read cut to the second (`Truncate(time.Second)`).
2. When an opening fails: if `running` is false, `run` returns; otherwise it waits `pollEvery` and opens again.
3. It reads with `bufio.Reader.ReadString('\n')`, so a line has any length. A line is split at its first space into the timestamp, parsed with `time.RFC3339Nano`, and the text.
4. In a stream that was opened again, a line is a repeat when its timestamp is before the last one read, or equal to it while fewer lines with that timestamp have been skipped in this stream than were read before. A repeat is skipped: not written to raw, not counted, not parsed. The first line that is not a repeat ends the skipping. When at that moment fewer lines with the last timestamp were skipped than had been read, it first emits `Event{Fault: &verdict.Condition{Code: verdict.InternalError, At: now(), Text: "the probe's log could not be followed without a gap"}}`, and then goes on with that line.
5. Every other line is written to raw as it came, with its line end. Its timestamp becomes the last one read, with a count of the lines read at it. It counts as a line and goes to `ParseLine(text, timestamp)`: `Observed` emits `Event{Obs}`; `Invalid` emits `Event{Fault: &verdict.Condition{Code: verdict.ProbeLineInvalid, At: timestamp, Text: line.Fault}}`; `JSONText` counts as JSON text; `Text` does nothing more.
6. A last piece with no line end (it comes with `io.EOF`) is a line like any other before `stopAsked`. After `stopAsked` it is written to raw with a line end, and nothing else.
7. A line whose timestamp does not parse is written to raw and emits `Event{Fault: &verdict.Condition{Code: verdict.InternalError, At: now(), Text: "the probe's log has a line with no timestamp"}}`.
8. When a stream ends, by `io.EOF` or by an error: if ctx has ended or `running` is false, `run` returns; otherwise it opens the log again.

- [ ] **Step 9: Run the tests to verify they pass**

Run: `go test ./internal/probe/ -run 'TestFollow|TestPodSpec' -v`
Expected: PASS

- [ ] **Step 10: Write the failing tests of Start and Stop**

In `internal/probe/pod_test.go`, replace the import block with this one:

```go
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
```

and add below `TestPodSpec`:

```go
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
	events := allEvents(st.Events)
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
			st, err := s.pod.Start(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer s.pod.Stop(context.Background())
			s.setStatus(t, terminatedStatus(1, "Error", c.finished))
			timeout := time.After(2 * time.Second)
			for {
				select {
				case e := <-st.Events:
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
			st, err := s.pod.Start(context.Background())
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
			events := allEvents(st.Events)
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
	s := newProbe(t, newFakeLog(logLine(logAt(400), `{"ok":true}`)), runningStatus(), 200*time.Millisecond, 5*time.Second)
	st, err := s.pod.Start(context.Background())
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
	allEvents(st.Events) // returns only when Events is closed
}

func TestStopEndsWithItsContext(t *testing.T) {
	s := newProbe(t, newFakeLog(logLine(logAt(400), `{"ok":true}`)), runningStatus(), 10*time.Second, 5*time.Second)
	st, err := s.pod.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := s.pod.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Stop = %v, want the context's error", err)
	}
	allEvents(st.Events)
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
```

- [ ] **Step 11: Run the tests to verify they fail**

Run: `go test ./internal/probe/ -run 'TestStart|TestClockCheck|TestProbeExits|TestStop' -v`
Expected: FAIL: the package does not build, with `undefined:` errors for `Pod`, `stopMargin` and `NewPod`

- [ ] **Step 12: Implement `Started`, `Facts`, `Probe`, `ClockBound`, `Pod`, `NewPod`, `Start` and `Stop` in `internal/probe/pod.go`**

With their interface comments from the Interfaces block; `Pod`'s comment is that of `NewPod`. Also:

```go
// stopMargin is how long Stop waits past the drain for the probe's log to end.
var stopMargin = 5 * time.Second
```

`Pod` holds an unexported `open logSource` that `NewPod` sets to the API's log: `o.Client.CoreV1().Pods(cluster.ProbeNamespace).GetLogs("probe", &corev1.PodLogOptions{Container: "probe", Follow: true, Timestamps: true, SinceTime: since}).Stream(ctx)`, with `since` as a `*metav1.Time`. A test replaces `open`.

Its unexported `running(ctx) bool`, which the follower gets: reads the pod; false when it is not found or when the container named `probe` is terminated; true otherwise, an error of the API included.

The pod's state, for the text of `ProbeNotStarted`: "the pod is PHASE", then "; not scheduled: MESSAGE" when its `PodScheduled` condition is `False`, then "; container probe is waiting: REASON" when that container waits.

`Start`:

1. Delete the pod `probe` of `cluster.ProbeNamespace` with a grace period of 0; not found is no error. Read it every `pollEvery` until it is not found, for at most `StartTimeout`; past that, return the plain error "the probe pod of an earlier run is still there".
2. Create `podSpec(o)`. An error of the API is returned as it is.
3. Start two goroutines under a context of their own, `context.WithCancel(context.WithoutCancel(ctx))`, which only `Stop` and a failed `Start` cancel, so that they outlive `Start`: the follower (`p.open`, `p.running`, `o.Raw`, `o.Now`, and the emit of rule 4), and a watcher that reads the pod every `pollEvery`.
4. The emit: the first `Observed` event notes its `Written` and `o.Now()` at that moment, and wakes `Start`. Every event then goes into the `Events` channel, which holds 1024 events; a send waits while the channel is full and gives up when the goroutines' context is cancelled, so no goroutine is left behind.
5. `Start` waits for the first of: the first observation; the watcher finding the container named `probe` terminated, which gives `ProbeExited` with the text "the probe exited before its first observation, with exit code N (REASON)"; `StartTimeout` since `Start` began, which gives `ProbeNotStarted` with the text "no observation within limits.timeout; " and the pod's state; ctx ending, which gives `ctx.Err()`.
6. At the first observation, `ClockDifference` is the noted `o.Now()` minus `Written`. When it is more than `ClockBound` either way, `Start` returns `RunnerUnsupported` with the text "the nodes' clock differs from the runner's by D", where D is the difference's size as `time.Duration` prints it (`2.5s`). Otherwise it returns `Started{First: Written, Events}`.
7. When `Start` returns an error, it cancels both goroutines, waits for them, closes `Events`, and keeps the Facts so far. The pod stays; the deletion of the cluster removes it.

The watcher after `Start` returned and before `Stop` was asked: when the container named `probe` is terminated, it emits `Event{Fault: &verdict.Condition{Code: verdict.ProbeExited, At: finishedAt, Text: "the probe exited with exit code N (REASON)"}}` once, with `o.Now()` for `At` when `finishedAt` is zero, and stops reading. When the pod is not found, it emits `ProbeExited` at `o.Now()` with the text "the probe's pod is gone".

`Stop`:

1. Before `Start`, after a `Start` that failed, or a second time: return the Facts kept, and nil.
2. Tell the follower `stopAsked`, note `asked := o.Now()`, then delete the pod with `GracePeriodSeconds` the drain rounded up to whole seconds, as in `podSpec`. Not found is no error.
3. Wait for the follower to end, for at most `Drain + stopMargin` on a real timer, or until ctx ends.
4. `StoppedWhenAsked` is true when the follower ended by itself, and either it had ended before `Stop` was called or `o.Now()` when it ended, minus `asked`, is less than `Drain`. When the wait ran out or ctx ended, cancel the follower and wait for it; `StoppedWhenAsked` is false.
5. Stop the watcher, close `Events`, and keep and return `Facts{Lines, JSONTextLines, StoppedWhenAsked, ClockDifference}`, with the counts of the follower; return `ctx.Err()` beside them when ctx ended in rule 3.

- [ ] **Step 13: Run the package's tests to verify they pass**

Run: `go test -count=3 ./internal/probe/`
Expected: `ok  	rollproof/internal/probe`

Run: `go vet ./...`
Expected: no output

- [ ] **Step 14: Write the test with a cluster**

`internal/probe/pod_cluster_test.go`. `cmd/rollproof` does not exist before Task 34, so the probe's image is this test binary: its `TestMain` runs the built-in probe when it is started as `probe http …`, as the tool's binary will. The binary must be static, so the test runs with `env CGO_ENABLED=0`.

```go
//go:build cluster

package probe

import (
	"bytes"
	"context"
	"debug/elf"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"rollproof/internal/cluster"
	"rollproof/internal/images"
)

// TestMain lets this test binary be the probe's image: started as
// "probe http …" it runs the built-in probe, as the tool's binary does.
func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "probe" && os.Args[2] == "http" {
		os.Exit(probeMain(os.Args[3:]))
	}
	os.Exit(m.Run())
}

func probeMain(args []string) int {
	o, err := ParseHTTPArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	if err := RunHTTP(ctx, o, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func TestPodOnACluster(t *testing.T) {
	ctx := context.Background()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f, err := elf.Open(bin)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			t.Fatal("the test binary is not static: run this test with env CGO_ENABLED=0")
		}
	}
	f.Close()

	engine, err := cluster.CheckRunner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const name = "rollproof-test-probe"
	c, err := cluster.Create(ctx, cluster.Spec{Name: name, RunDir: cluster.RunDir(name), Workers: 1, Namespace: "probe-test", Timeout: 5 * time.Minute}, engine)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Delete(context.Background())
	stored, err := images.Store(ctx, c, bin, nil, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	client, err := c.Client()
	if err != nil {
		t.Fatal(err)
	}

	o := HTTPOptions{URL: "http://" + c.NodeRegistry + "/v2/", Rate: 20, Timeout: 2 * time.Second, ExpectMin: 200, ExpectMax: 299}
	var raw bytes.Buffer
	p := NewPod(PodOptions{
		Client: client, Image: stored.Probe.Ref(), Args: HTTPArgs(o),
		Drain: o.Timeout + 5*time.Second, StartTimeout: 60 * time.Second, Raw: &raw, Now: time.Now,
	})
	st, err := p.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}

	passed, failed := 0, 0
	until := time.After(6 * time.Second)
read:
	for {
		select {
		case e := <-st.Events:
			switch {
			case e.Fault != nil:
				t.Errorf("fault: %v", *e.Fault)
			case !e.Obs.Written.Before(st.First.Add(5 * time.Second)):
				// written after the first 5 s: not counted
			case e.Obs.OK:
				passed += e.Obs.N
			default:
				failed++
			}
		case <-until:
			break read
		}
	}

	began := time.Now()
	facts, err := p.Stop(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for range st.Events {
	}
	if took := time.Since(began); took > o.Timeout+5*time.Second {
		t.Errorf("Stop took %v, longer than the drain", took)
	}
	if !facts.StoppedWhenAsked {
		t.Error("the probe did not stop when asked")
	}
	if passed < 80 || passed > 120 || failed != 0 {
		t.Errorf("%d checks passed and %d failed in the first 5 s, want 80 to 120 and none", passed, failed)
	}
	if facts.ClockDifference.Abs() > ClockBound {
		t.Errorf("clock difference %v, more than %v", facts.ClockDifference, ClockBound)
	}
	t.Logf("clock check: the first observation was read %v after the runtime stamped it", facts.ClockDifference)
}
```

- [ ] **Step 15: Run the test with a cluster**

Run: `env CGO_ENABLED=0 go test -tags cluster -p 1 ./internal/probe/ -run TestPodOnACluster -v -timeout 20m`
Expected: PASS, with a line `clock check: the first observation was read D after the runtime stamped it`.

- [ ] **Step 16: Record the clock check**

Add to `docs/proofs/README.md`, at its end, with D from Step 15, the engine Task 11 recorded, and today's date:

```markdown
## Clock check

Measured on DATE by `TestPodOnACluster` (`internal/probe/pod_cluster_test.go`) on ENGINE: the first observation was read D after the node's runtime stamped it. The bound is 2 s (`probe.ClockBound`). An engine in a virtual machine, which can give the nodes a clock of their own, was not measured (section 14, point 21).
```

- [ ] **Step 17: Commit**

```bash
git add internal/probe go.mod go.sum docs/proofs/README.md
git commit -m "Run the probe as a pod: start, follow its log, check the clock, and stop with a drain"
```

### Task 30: `report`: the run record and `result.json`

**Files:**
- Create: `internal/report/record.go`, `internal/report/result.go`, `internal/report/evidence.go`
- Test: `internal/report/result_test.go`, `internal/report/testdata/result-pass.golden.json`, `internal/report/testdata/result-first.golden.json`

**Interfaces:**
- Consumes: nothing from another task but the module. This package imports no other unit.
- Produces: `module internal/report`
  What a run writes: result.json, the console text and the evidence files.
  It hides the file formats and the moments of writing. It is given one
  record of plain values and reads nothing from the cluster.
- Produces: `type Run struct { Schema int; Final bool; Verdict, Driver, Name string; Reasons []Reason; Windows []Window; Transitions []Transition; Probes []Probe; Releases []Release; Settings []Setting; Images []Image; RemovedVariables, Warnings []string; Tool Tool; Cluster *Cluster; Out string }`
  The record of one run: everything result.json holds, and Out, the
  results folder, for the console's last line. Every nested type is
  defined in the same file, with only strings, numbers, booleans and times.
- Produces: `func First() Run`
  The record a run starts with: not final, not-verified, with the one
  reason internal-error and the text "the run did not finish".
- Produces: `func Open(out string, getenv func(string) string) (*Writer, error)`
  Makes the results folder ready and writes the first result.json into it.
  An empty out means a new folder under the runner's temporary directory
  on GitHub Actions, and under the system's otherwise. It replaces
  result.json and the evidence folder there and touches nothing else.
- Produces: `func (w *Writer) Dir() string`
  The results folder's absolute path.
- Produces: `func (w *Writer) Final(r Run) error`
  Replaces result.json with r, marked final, in one step. It may be called
  again when a warning arises after the verdict.
- Produces: `func (w *Writer) Evidence(name string) (io.WriteCloser, error)`
  A new file of the evidence folder. name may hold folders. A name that is
  already taken gets a number after it.

The names of `result.json` are a public contract (decision 1 of this plan). The schema is the record of Step 4 with its json tags. Keys are written in the order of the fields.

- [ ] **Step 1: Write the two golden files**

`internal/report/testdata/result-first.golden.json`:

```json
{
  "schema": 1,
  "final": false,
  "verdict": "not-verified",
  "driver": "",
  "name": "",
  "reasons": [
    {
      "code": "internal-error",
      "verdict": "not-verified",
      "text": "the run did not finish",
      "detail": [],
      "decided": true
    }
  ],
  "windows": [],
  "transitions": [],
  "probes": [],
  "releases": [],
  "settings": [],
  "images": [],
  "removed_variables": [],
  "warnings": [],
  "tool": {
    "version": ""
  },
  "cluster": null
}
```

`internal/report/testdata/result-pass.golden.json`, the run that passed in section 10:

```json
{
  "schema": 1,
  "final": true,
  "verdict": "pass",
  "driver": "helm",
  "name": "orders",
  "reasons": [],
  "windows": [
    {
      "name": "steady",
      "start": "2026-11-13T09:59:52.000Z",
      "end": "2026-11-13T10:00:02.000Z",
      "result": "pass",
      "checks": 2000,
      "failed": 0,
      "failed_after_finish": 0,
      "crossed_start": 0,
      "failed_by_class": {},
      "first_failure": null,
      "slowest_ms": 41,
      "lag_ms": 6
    },
    {
      "name": "deploy",
      "start": "2026-11-13T10:00:02.000Z",
      "end": "2026-11-13T10:00:35.400Z",
      "result": "pass",
      "checks": 6680,
      "failed": 0,
      "failed_after_finish": 0,
      "crossed_start": 0,
      "failed_by_class": {},
      "first_failure": null,
      "slowest_ms": 212,
      "lag_ms": 38
    },
    {
      "name": "rollback",
      "start": "2026-11-13T10:00:35.400Z",
      "end": "2026-11-13T10:01:09.300Z",
      "result": "pass",
      "checks": 6780,
      "failed": 0,
      "failed_after_finish": 0,
      "crossed_start": 0,
      "failed_by_class": {},
      "first_failure": null,
      "slowest_ms": 187,
      "lag_ms": 35
    }
  ],
  "transitions": [
    {
      "name": "deploy",
      "stalled": false,
      "command": "ok",
      "settle_restarted_by": [],
      "workloads": [
        {
          "workload": "deployment/orders",
          "pods_at_start": 2,
          "pods_replaced": 2,
          "pods_created": 2,
          "added": false,
          "removed": false,
          "probed": "yes",
          "quickest_stop_ms": 5300
        }
      ]
    },
    {
      "name": "rollback",
      "stalled": false,
      "command": "ok",
      "settle_restarted_by": [],
      "workloads": [
        {
          "workload": "deployment/orders",
          "pods_at_start": 2,
          "pods_replaced": 2,
          "pods_created": 2,
          "added": false,
          "removed": false,
          "probed": "yes",
          "quickest_stop_ms": 5200
        }
      ]
    }
  ],
  "probes": [
    {
      "kind": "http",
      "target": "GET http://orders.orders.svc/api/orders/latest",
      "settings": [
        {
          "key": "probe.http.rate",
          "value": "200",
          "default": "200"
        },
        {
          "key": "probe.http.timeout",
          "value": "2s",
          "default": "2s"
        },
        {
          "key": "probe.http.expect-status",
          "value": "200-299",
          "default": "200-299"
        }
      ],
      "headers": [],
      "env_names": [],
      "command_count": 0,
      "args_count": 0,
      "lines": 78,
      "json_lines_without_ok": 0,
      "certificate_checked": true,
      "stopped_when_asked": true,
      "notes": []
    }
  ],
  "releases": [
    {
      "role": "previous",
      "chart": "orders",
      "version": "1.4.2",
      "source": "oci://registry.example/charts/orders",
      "values_files": [],
      "inline_keys": []
    },
    {
      "role": "candidate",
      "chart": "orders",
      "version": "1.5.0",
      "source": "./chart",
      "values_files": [],
      "inline_keys": [
        "image"
      ]
    }
  ],
  "settings": [
    {
      "key": "cluster.node-image",
      "value": "the linked kind release's",
      "default": "the linked kind release's"
    },
    {
      "key": "cluster.registry-image",
      "value": "the tool's pinned registry image",
      "default": "the tool's pinned registry image"
    },
    {
      "key": "cluster.workers",
      "value": "3",
      "default": "3"
    },
    {
      "key": "limits.steady",
      "value": "10s",
      "default": "10s"
    },
    {
      "key": "limits.settle",
      "value": "15s",
      "default": "15s"
    },
    {
      "key": "limits.timeout",
      "value": "5m",
      "default": "5m"
    },
    {
      "key": "limits.silence",
      "value": "5s",
      "default": "5s"
    }
  ],
  "images": [
    {
      "name": "orders",
      "digest": "sha256:4f0ddb145f76c84c30f552c4e642ff87f49aaa01f83b09f89e34144d2af22d52",
      "run_by": [
        {
          "workload": "deployment/orders",
          "transition": "deploy"
        }
      ]
    }
  ],
  "removed_variables": [],
  "warnings": [],
  "tool": {
    "version": "dev"
  },
  "cluster": {
    "provider": "kind",
    "provider_version": "v0.33.0",
    "kubernetes": "v1.37.0",
    "node_image": "kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5",
    "workers": 3
  }
}
```

Each file ends with one line end.

- [ ] **Step 2: Write the failing tests**

`internal/report/result_test.go`. Its helpers and `passRun` serve Task 31's tests too.

```go
package report

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the golden files that the code produces")

var t0 = time.Date(2026, 11, 13, 9, 59, 52, 0, time.UTC)

const (
	ordersDigest  = "sha256:4f0ddb145f76c84c30f552c4e642ff87f49aaa01f83b09f89e34144d2af22d52"
	kindNodeImage = "kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5"
)

// at is t0 plus ms milliseconds, as the record holds a time.
func at(ms int) *time.Time { t := t0.Add(time.Duration(ms) * time.Millisecond); return &t }

// stopMs is a quickest stop as the record holds it.
func stopMs(ms int64) *int64 { return &ms }

func noEnv(string) string { return "" }

// golden compares got with the file at path; -update rewrites the file first.
func golden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sameAsFile(t, path, got)
}

// sameAsFile compares got with the file at path and never rewrites it.
func sameAsFile(t *testing.T, path string, got []byte) {
	t.Helper()
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs\n--- got\n%s--- want\n%s", path, got, want)
	}
}

// write makes the file at path, and its folders, with text in it.
func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// names lists the entries of a folder.
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func ordersReplaced(quickest int64) Workload {
	return Workload{Workload: "deployment/orders", PodsAtStart: 2, PodsReplaced: 2, PodsCreated: 2, Probed: "yes", QuickestStopMs: stopMs(quickest)}
}

// defaultSettings is what the configuration's Settings gives for the
// examples' file, without the probe's three.
func defaultSettings() []Setting {
	return []Setting{
		{Key: "cluster.node-image", Value: "the linked kind release's", Default: "the linked kind release's"},
		{Key: "cluster.registry-image", Value: "the tool's pinned registry image", Default: "the tool's pinned registry image"},
		{Key: "cluster.workers", Value: "3", Default: "3"},
		{Key: "limits.steady", Value: "10s", Default: "10s"},
		{Key: "limits.settle", Value: "15s", Default: "15s"},
		{Key: "limits.timeout", Value: "5m", Default: "5m"},
		{Key: "limits.silence", Value: "5s", Default: "5s"},
	}
}

// passRun is the record of section 10's run that passed.
func passRun() Run {
	return Run{
		Schema: 1, Final: true, Verdict: "pass", Driver: "helm", Name: "orders",
		Windows: []Window{
			{Name: "steady", Start: at(0), End: at(10_000), Result: "pass", Checks: 2000, SlowestMs: 41, LagMs: 6},
			{Name: "deploy", Start: at(10_000), End: at(43_400), Result: "pass", Checks: 6680, SlowestMs: 212, LagMs: 38},
			{Name: "rollback", Start: at(43_400), End: at(77_300), Result: "pass", Checks: 6780, SlowestMs: 187, LagMs: 35},
		},
		Transitions: []Transition{
			{Name: "deploy", Command: "ok", Workloads: []Workload{ordersReplaced(5300)}},
			{Name: "rollback", Command: "ok", Workloads: []Workload{ordersReplaced(5200)}},
		},
		Probes: []Probe{{
			Kind:   "http",
			Target: "GET http://orders.orders.svc/api/orders/latest",
			Settings: []Setting{
				{Key: "probe.http.rate", Value: "200", Default: "200"},
				{Key: "probe.http.timeout", Value: "2s", Default: "2s"},
				{Key: "probe.http.expect-status", Value: "200-299", Default: "200-299"},
			},
			Lines: 78, CertificateChecked: true, StoppedWhenAsked: true,
		}},
		Releases: []Release{
			{Role: "previous", Chart: "orders", Version: "1.4.2", Source: "oci://registry.example/charts/orders"},
			{Role: "candidate", Chart: "orders", Version: "1.5.0", Source: "./chart", InlineKeys: []string{"image"}},
		},
		Settings: defaultSettings(),
		Images:   []Image{{Name: "orders", Digest: ordersDigest, RunBy: []RunBy{{Workload: "deployment/orders", Transition: "deploy"}}}},
		Tool:     Tool{Version: "dev"},
		Cluster:  &Cluster{Provider: "kind", ProviderVersion: "v0.33.0", Kubernetes: "v1.37.0", NodeImage: kindNodeImage, Workers: 3},
		Out:      "/tmp/rollproof-results-7f3a",
	}
}

func TestFirst(t *testing.T) {
	got, err := encode(First())
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "testdata/result-first.golden.json", got)
}

func TestGoldenPass(t *testing.T) {
	got, err := encode(passRun())
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "testdata/result-pass.golden.json", got)
}

func TestOpenWritesTheFirstFile(t *testing.T) {
	out := t.TempDir()
	w, err := Open(out, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if w.Dir() != out {
		t.Errorf("Dir() = %q, want %q", w.Dir(), out)
	}
	got, err := os.ReadFile(filepath.Join(out, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	sameAsFile(t, "testdata/result-first.golden.json", got)
	if n := names(t, filepath.Join(out, "evidence")); len(n) != 0 {
		t.Errorf("evidence holds %v, want nothing", n)
	}
}

func TestOpenReplacesAnEarlierRun(t *testing.T) {
	out := t.TempDir()
	write(t, filepath.Join(out, "result.json"), `{"schema":1,"final":true,"verdict":"pass"}`)
	write(t, filepath.Join(out, "evidence", "probe.log"), "old\n")
	write(t, filepath.Join(out, "notes.txt"), "mine\n")
	if _, err := Open(out, noEnv); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	sameAsFile(t, "testdata/result-first.golden.json", got)
	if n := names(t, filepath.Join(out, "evidence")); len(n) != 0 {
		t.Errorf("evidence holds %v, want nothing", n)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "notes.txt")); string(b) != "mine\n" {
		t.Errorf("notes.txt = %q, want it untouched", b)
	}
}

func TestOpenDefaultFolder(t *testing.T) {
	runner := t.TempDir()
	actions := func(k string) string {
		switch k {
		case "GITHUB_ACTIONS":
			return "true"
		case "RUNNER_TEMP":
			return runner
		}
		return ""
	}
	a, err := Open("", actions)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open("", actions)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []*Writer{a, b} {
		if filepath.Dir(w.Dir()) != runner || !strings.HasPrefix(filepath.Base(w.Dir()), "rollproof-results-") {
			t.Errorf("Dir() = %q, want a rollproof-results- folder under %q", w.Dir(), runner)
		}
	}
	if a.Dir() == b.Dir() {
		t.Errorf("two runs share %q", a.Dir())
	}

	system := t.TempDir()
	t.Setenv("TMPDIR", system)
	c, err := Open("", noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(c.Dir()) != system {
		t.Errorf("without GitHub Actions Dir() = %q, want a folder under %q", c.Dir(), system)
	}
}

func TestOpenFails(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a-file")
	write(t, file, "x")
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o700) })
	for _, c := range []struct {
		name, out string
		notAsRoot bool
	}{
		{"out names a file", file, false},
		{"out under a folder the tool cannot write", filepath.Join(locked, "results"), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.notAsRoot && os.Geteuid() == 0 {
				t.Skip("root writes into any folder")
			}
			_, err := Open(c.out, noEnv)
			if err == nil || !strings.Contains(err.Error(), c.out) {
				t.Errorf("Open(%q) = %v, want an error that names the path", c.out, err)
			}
		})
	}
}

func TestFinalIsOneStep(t *testing.T) {
	out := t.TempDir()
	w, err := Open(out, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	r := passRun()
	r.Final = false
	read := func() Run {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(out, "result.json"))
		if err != nil {
			t.Fatal(err)
		}
		var got Run
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("result.json does not parse: %v", err)
		}
		return got
	}
	if err := w.Final(r); err != nil {
		t.Fatal(err)
	}
	if got := read(); !got.Final || got.Verdict != "pass" {
		t.Errorf("final %v, verdict %q; want true, pass", got.Final, got.Verdict)
	}
	r.Warnings = []string{"deleting the cluster: it was already gone"}
	if err := w.Final(r); err != nil {
		t.Fatal(err)
	}
	if got := read(); !slices.Equal(got.Warnings, r.Warnings) {
		t.Errorf("warnings %v, want %v", got.Warnings, r.Warnings)
	}
	if n := names(t, out); !slices.Equal(n, []string{"evidence", "result.json"}) {
		t.Errorf("the folder holds %v, want only evidence and result.json", n)
	}
}

// nulls collects the keys whose value is null, at any depth.
func nulls(v any, key string, found *[]string) {
	switch x := v.(type) {
	case nil:
		*found = append(*found, key)
	case map[string]any:
		for k, e := range x {
			nulls(e, k, found)
		}
	case []any:
		for _, e := range x {
			nulls(e, key, found)
		}
	}
}

func TestNoNulls(t *testing.T) {
	timesAndCluster := []string{"cluster", "end", "first_failure", "quickest_stop_ms", "start"}
	for _, r := range []Run{
		{},
		{
			Reasons:     []Reason{{Code: "internal-error"}},
			Windows:     []Window{{Name: "steady"}},
			Transitions: []Transition{{Name: "deploy", Workloads: []Workload{{Workload: "deployment/orders"}}}},
			Probes:      []Probe{{Kind: "http"}},
			Releases:    []Release{{Role: "candidate"}},
			Images:      []Image{{Name: "orders"}},
		},
	} {
		b, err := encode(r)
		if err != nil {
			t.Fatal(err)
		}
		var v any
		if err := json.Unmarshal(b, &v); err != nil {
			t.Fatal(err)
		}
		var found []string
		nulls(v, "", &found)
		for _, k := range found {
			if !slices.Contains(timesAndCluster, k) {
				t.Errorf("%s is null in\n%s", k, b)
			}
		}
		if !strings.Contains(string(b), `"schema": 1,`) {
			t.Errorf("schema is not 1 in\n%s", b)
		}
		if len(r.Windows) > 0 && !strings.Contains(string(b), `"failed_by_class": {}`) {
			t.Errorf("failed_by_class is not {} in\n%s", b)
		}
	}
}

func TestEvidenceNames(t *testing.T) {
	w, err := Open(t.TempDir(), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"commands/install", "commands/install-2", "commands/install-3"} {
		f, err := w.Evidence("commands/install")
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		if _, err := os.Stat(filepath.Join(w.Dir(), "evidence", want)); err != nil {
			t.Errorf("no file %s: %v", want, err)
		}
	}
	for _, bad := range []string{"", "../escape", "commands/../../escape", "/etc/escape"} {
		if f, err := w.Evidence(bad); err == nil {
			f.Close()
			t.Errorf("Evidence(%q) was allowed", bad)
		}
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/report/ -v`
Expected: FAIL: the package does not build, with `undefined:` errors for `Run`, `Window` and the other types.

- [ ] **Step 4: Define the record in `internal/report/record.go`**

The package comment is the module's from the Interfaces block. `Run` has its interface comment from the Interfaces block.

```go
// Package report is what a run writes: result.json, the console text and the
// evidence files. It hides the file formats and the moments of writing. It is
// given one record of plain values and reads nothing from the cluster.
package report

import "time"

// Run is the record of one run: everything result.json holds, and Out, the
// results folder, for the console's last line. Every nested type is defined
// in the same file, with only strings, numbers, booleans and times.
type Run struct {
	Schema           int          `json:"schema"`
	Final            bool         `json:"final"`
	Verdict          string       `json:"verdict"`
	Driver           string       `json:"driver"`
	Name             string       `json:"name"`
	Reasons          []Reason     `json:"reasons"`
	Windows          []Window     `json:"windows"`
	Transitions      []Transition `json:"transitions"`
	Probes           []Probe      `json:"probes"`
	Releases         []Release    `json:"releases"`
	Settings         []Setting    `json:"settings"`
	Images           []Image      `json:"images"`
	RemovedVariables []string     `json:"removed_variables"`
	Warnings         []string     `json:"warnings"`
	Tool             Tool         `json:"tool"`
	Cluster          *Cluster     `json:"cluster"`
	Out              string       `json:"-"`
}

// Reason is one reason of the verdict. Window is empty for a reason with no
// window. Detail holds lines for people that follow the text. Decided marks
// the one reason that decided.
type Reason struct {
	Code    string   `json:"code"`
	Verdict string   `json:"verdict"`
	Window  string   `json:"window,omitempty"`
	Text    string   `json:"text"`
	Detail  []string `json:"detail"`
	Decided bool     `json:"decided"`
}

// Window is one window with its result and its counts. A nil time is one
// that never came: the start of a window that did not run, or the first
// failure of a window with none.
type Window struct {
	Name              string         `json:"name"`
	Start             *time.Time     `json:"start"`
	End               *time.Time     `json:"end"`
	Result            string         `json:"result"`
	Checks            int            `json:"checks"`
	Failed            int            `json:"failed"`
	FailedAfterFinish int            `json:"failed_after_finish"`
	CrossedStart      int            `json:"crossed_start"`
	FailedByClass     map[string]int `json:"failed_by_class"`
	FirstFailure      *time.Time     `json:"first_failure"`
	SlowestMs         float64        `json:"slowest_ms"`
	LagMs             float64        `json:"lag_ms"`
}

// Transition is one transition that ran. Command is how its command ended:
// ok, exit-nonzero, not-started, stopped-at-limit or stopped-by-signal.
type Transition struct {
	Name              string     `json:"name"`
	Stalled           bool       `json:"stalled"`
	Command           string     `json:"command"`
	SettleRestartedBy []string   `json:"settle_restarted_by"`
	Workloads         []Workload `json:"workloads"`
}

// Workload is one watched workload in one transition. Workload is the kind,
// a slash and the name. Probed is yes, no or not known. QuickestStopMs is
// nil when no ready pod was deleted.
type Workload struct {
	Workload       string `json:"workload"`
	PodsAtStart    int    `json:"pods_at_start"`
	PodsReplaced   int    `json:"pods_replaced"`
	PodsCreated    int    `json:"pods_created"`
	Added          bool   `json:"added"`
	Removed        bool   `json:"removed"`
	Probed         string `json:"probed"`
	QuickestStopMs *int64 `json:"quickest_stop_ms"`
}

// Probe is one probe that judged the run. Headers and EnvNames are names,
// never values.
type Probe struct {
	Kind               string    `json:"kind"`
	Target             string    `json:"target"`
	Settings           []Setting `json:"settings"`
	Headers            []string  `json:"headers"`
	EnvNames           []string  `json:"env_names"`
	CommandCount       int       `json:"command_count"`
	ArgsCount          int       `json:"args_count"`
	Lines              int       `json:"lines"`
	JSONLinesWithoutOK int       `json:"json_lines_without_ok"`
	CertificateChecked bool      `json:"certificate_checked"`
	StoppedWhenAsked   bool      `json:"stopped_when_asked"`
	Notes              []string  `json:"notes"`
}

// Release is one release of the Helm driver: the chart's name and the
// version Helm resolved, where it came from, and the names of the values
// files and the top-level keys of the inline maps. Never a value.
type Release struct {
	Role        string   `json:"role"`
	Chart       string   `json:"chart"`
	Version     string   `json:"version"`
	Source      string   `json:"source"`
	ValuesFiles []string `json:"values_files"`
	InlineKeys  []string `json:"inline_keys"`
}

// Setting is one setting as text, with its default.
type Setting struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Default string `json:"default"`
}

// Image is one entry of the configuration's images, with its digest and the
// watched workloads whose pods ran it.
type Image struct {
	Name   string  `json:"name"`
	Digest string  `json:"digest"`
	RunBy  []RunBy `json:"run_by"`
}

// RunBy is one watched workload whose pods ran an image, and the transition
// that created those pods: install, deploy or rollback.
type RunBy struct {
	Workload   string `json:"workload"`
	Transition string `json:"transition"`
}

// Tool is the tool that wrote the record.
type Tool struct {
	Version string `json:"version"`
}

// Cluster is the test cluster. Provider is kind.
type Cluster struct {
	Provider        string `json:"provider"`
	ProviderVersion string `json:"provider_version"`
	Kubernetes      string `json:"kubernetes"`
	NodeImage       string `json:"node_image"`
	Workers         int    `json:"workers"`
}
```

- [ ] **Step 5: Implement `result.json` in `internal/report/result.go`**

- `const timeLayout = "2006-01-02T15:04:05.000Z07:00"`, used on `t.UTC()`.
- `func (w Window) MarshalJSON() ([]byte, error)`, with the comment "MarshalJSON writes the window's keys in the order of its fields, each time in UTC as RFC 3339 with milliseconds, a nil time as null, and a nil FailedByClass as {}." It marshals an unexported struct with the same json names in the same order and `*string` times. Reading back with `encoding/json` needs nothing more: `time.Time` parses that form.
- `func encode(r Run) ([]byte, error)`, unexported: the bytes of `result.json`. It sets `Schema` to 1 and replaces every nil list with an empty one at every depth (`Reasons`, `Windows`, `Transitions`, `Probes`, `Releases`, `Settings`, `Images`, `RemovedVariables`, `Warnings`, `Reason.Detail`, `Transition.SettleRestartedBy`, `Transition.Workloads`, `Probe.Settings`, `Probe.Headers`, `Probe.EnvNames`, `Probe.Notes`, `Release.ValuesFiles`, `Release.InlineKeys`, `Image.RunBy`), on copies, so the caller's record does not change. It writes with a `json.Encoder` with `SetIndent("", "  ")` and `SetEscapeHTML(false)`, which ends the text with one line end.
- `func First() Run`, with its interface comment: `Schema` 1, `Final` false, `Verdict` "not-verified", and one reason `{Code: "internal-error", Verdict: "not-verified", Text: "the run did not finish", Decided: true}`. Nothing else is set.
- `type Writer struct` with one unexported field, the folder's absolute path. `Open`, `Dir` and `Final` with their interface comments.
- `Open`: (1) with `out` empty, the base is `getenv("RUNNER_TEMP")` when `getenv("GITHUB_ACTIONS")` is "true" and `RUNNER_TEMP` is not empty, else `os.TempDir()`, and the folder is `os.MkdirTemp(base, "rollproof-results-")`; with `out` set, the folder is `filepath.Abs(out)`, made with `os.MkdirAll(dir, 0o755)`. (2) Remove `evidence` in it with everything inside, and make it again, empty, mode 0755. (3) Write `First()` as `result.json`. Every error's text starts "results folder PATH: " with the absolute path (or the base, for a new folder).
- `Final(r)` sets `r.Final` to true and writes `r`.
- Writing, for `Open` and `Final`: `encode`; `os.CreateTemp(dir, ".result-*.json")`; write; close; `os.Rename` to `result.json`. On an error the temporary file is removed.

- [ ] **Step 6: Implement `Evidence` in `internal/report/evidence.go`**

With its interface comment from the Interfaces block. A name that is empty, absolute, or has `..` as one of its elements (split at `/`) is refused with an error that names it. The file is `evidence/NAME` in the results folder; its folders are made with mode 0755; it is opened with `O_WRONLY|O_CREATE|O_EXCL` and mode 0644. When the name is taken, the next free of `NAME-2`, `NAME-3` and so on is used.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/report/ -v`
Expected: PASS. `TestOpenFails` reports its second row as skipped when the tests run as root.

Run: `go vet ./...`
Expected: no output

- [ ] **Step 8: Commit**

```bash
git add internal/report
git commit -m "Write result.json: the first file, the final file, and the evidence folder"
```

### Task 31: `report`: the console text

**Files:**
- Create: `internal/report/console.go`
- Test: `internal/report/console_test.go`, `internal/report/testdata/console-pass.golden.txt`, `internal/report/testdata/console-fail.golden.txt`, `internal/report/testdata/console-not-verified.golden.txt`, `internal/report/testdata/console-stall.golden.txt`, `internal/report/testdata/console-changed.golden.txt`

**Interfaces:**
- Consumes: `type Run struct { … }` and its nested types (Task 30).
- Produces: `func Console(r Run) string`
  The text a run prints at its end, as section 10 shows it: the verdict
  line, the releases, images and probe, one row for each window and for
  each workload of each transition, every reason with its code, the lists
  of what a pass exercised and did not, every warning, and the results
  folder.

- [ ] **Step 1: Copy section 10's three examples into golden files**

The three examples are the files, byte for byte, each with one line end after its last line. As this plan was written they are lines 1603 to 1634, 1640 to 1659 and 1665 to 1673 of the spec; copy them with:

```bash
sed -n '1603,1634p' docs/specs/2026-11-13-rollproof-design.md > internal/report/testdata/console-pass.golden.txt
sed -n '1640,1659p' docs/specs/2026-11-13-rollproof-design.md > internal/report/testdata/console-fail.golden.txt
sed -n '1665,1673p' docs/specs/2026-11-13-rollproof-design.md > internal/report/testdata/console-not-verified.golden.txt
```

and check that each file is exactly the text below. The test never rewrites these three.

`internal/report/testdata/console-pass.golden.txt`:

```text
rollproof: PASS   orders (Helm) on Kubernetes v1.37.0 in kind

releases    previous   orders 1.4.2, oci://registry.example/charts/orders; chart defaults
            candidate  orders 1.5.0, ./chart; values: inline (image)
images      orders     run by deployment/orders, in pods the deploy created
probe       built-in HTTP: GET http://orders.orders.svc/api/orders/latest
            200 a second (one check every 5 ms), timeout 2 s, status 200-299

window     length   checks   failed   after finish   slowest   behind   result
steady     10.0 s    2,000        0              -     41 ms     6 ms   pass
deploy     33.4 s    6,680        0              0    212 ms    38 ms   pass
rollback   33.9 s    6,780        0              0    187 ms    35 ms   pass

transition  workload            probed   pods replaced   quickest stop
deploy      deployment/orders   yes      2 of 2          5.3 s
rollback    deployment/orders   yes      2 of 2          5.2 s

exercised
  deploy      the candidate's start-up and the previous release's shutdown
  rollback    the previous release's start-up and the candidate's shutdown
not exercised
  the delay before a larger cluster stops routing to a stopping pod
  connections held open, and requests in flight on a pod after new connections left it
  the path through an ingress, a load balancer or a mesh
  routing that depends on where the client is
  capacity while pods are down, and autoscaling
  storage that attaches to one node at a time
  latency below the timeout
  data migrations
  a node drain

results: /tmp/rollproof-results-7f3a
```

`internal/report/testdata/console-fail.golden.txt`:

```text
rollproof: FAIL   orders (Helm) on Kubernetes v1.37.0 in kind

releases    previous   orders 1.4.2, oci://registry.example/charts/orders; chart defaults
            candidate  orders 1.5.0, ./chart; values: inline (image)
images      orders     run by deployment/orders, in pods the deploy created
probe       built-in HTTP: GET http://orders.orders.svc/api/orders/latest
            200 a second (one check every 5 ms), timeout 2 s, status 200-299

window     length   checks   failed   after finish   slowest   behind   result
steady     10.0 s    2,000        0              -     39 ms     5 ms   pass
deploy     29.1 s    5,820      243             31    198 ms    12 ms   FAIL
rollback        -        -        -              -         -        -   not run

transition  workload            probed   pods replaced   quickest stop
deploy      deployment/orders   yes      2 of 2          0.8 s

deploy: 243 checks failed (refused 198, timeout 45), first at 10:00:07.31   [probe-failures]
  stopping then   orders-6d5f9c-x2k4q (previous release), told to stop 0.3 s before
  starting then   none
results: /tmp/rollproof-results-7f3a
```

`internal/report/testdata/console-not-verified.golden.txt`:

```text
rollproof: NOT VERIFIED   orders (Helm) on Kubernetes v1.37.0 in kind

releases    previous   orders 1.4.2, oci://registry.example/charts/orders; chart defaults
            candidate  orders 1.5.0, ./chart; values: inline (image)

could not test: nothing to watch   [nothing-watched]
  15 s after the install command returned, namespace orders held no Deployment, StatefulSet
  or DaemonSet that matches the selector app=order
results: /tmp/rollproof-results-7f3a
```

- [ ] **Step 2: Write the two golden files that the examples do not show**

`internal/report/testdata/console-stall.golden.txt`, a deploy that stalled because the extra pod found no node:

```text
rollproof: FAIL   orders (Helm) on Kubernetes v1.37.0 in kind

releases    previous   orders 1.4.2, oci://registry.example/charts/orders; chart defaults
            candidate  orders 1.5.0, ./chart; values: inline (image)
images      orders     run by no watched pod
probe       built-in HTTP: GET http://orders.orders.svc/api/orders/latest
            200 a second (one check every 5 ms), timeout 2 s, status 200-299

window      length   checks   failed   after finish   slowest   behind   result
steady      10.0 s    2,000        0              -     41 ms     6 ms   pass
deploy     300.0 s   60,000        0              0     35 ms     4 ms   FAIL
rollback         -        -        -              -         -        -   not run

transition  workload            probed   pods replaced   quickest stop
deploy      deployment/orders   yes      not replaced    -

deploy: did not finish within limits.timeout   [transition-stalled]
  deployment/orders is not complete: Waiting for deployment "orders" rollout to finish: 1
  out of 3 new replicas have been updated...
  pod orders-7c9d8b-q8w2n is Pending, not scheduled (Unschedulable): 0/4 nodes are
  available: 1 node(s) had untolerated taint {node-role.kubernetes.io/control-plane: }, 3
  node(s) didn't match pod anti-affinity rules. preemption: 0/4 nodes are available: 1
  Preemption is not helpful for scheduling, 3 No preemption victims found for incoming pod.
deploy: orders-7c9d8b-q8w2n could not be scheduled; the test cluster has 3 worker nodes (cluster.workers); a rolling update needs room for one more pod   [pod-unschedulable]
results: /tmp/rollproof-results-7f3a
```

`internal/report/testdata/console-changed.golden.txt`, a pass with four workers, a steady window of 5m, a rate of 100, and a second watched workload that the probe does not reach:

```text
rollproof: PASS   orders (Helm) on Kubernetes v1.37.0 in kind

releases    previous   orders 1.4.2, oci://registry.example/charts/orders; chart defaults
            candidate  orders 1.5.0, ./chart; values: inline (image)
images      orders     run by deployment/orders, in pods the deploy created
probe       built-in HTTP: GET http://orders.orders.svc/api/orders/latest
            100 a second (one check every 10 ms), timeout 2 s, status 200-299
settings that differ from their defaults
  cluster.workers  4  (default 3)
  limits.steady  5m  (default 10s)
  probe.http.rate  100  (default 200)

window      length   checks   failed   after finish   slowest   behind   result
steady     300.0 s   30,000        0              -     41 ms     6 ms   pass
deploy      33.4 s    3,340        0              0    212 ms    38 ms   pass
rollback    33.9 s    3,390        0              0    187 ms    35 ms   pass

transition  workload                   probed   pods replaced   quickest stop
deploy      deployment/orders          yes      2 of 2          5.3 s
            deployment/orders-worker   no       2 of 2          1.2 s
rollback    deployment/orders          yes      2 of 2          5.2 s
            deployment/orders-worker   no       2 of 2          1.2 s

exercised
  deploy      the candidate's start-up and the previous release's shutdown
  rollback    the previous release's start-up and the candidate's shutdown
not exercised
  the delay before a larger cluster stops routing to a stopping pod
  connections held open, and requests in flight on a pod after new connections left it
  the path through an ingress, a load balancer or a mesh
  routing that depends on where the client is
  capacity while pods are down, and autoscaling
  storage that attaches to one node at a time
  latency below the timeout
  data migrations
  a node drain

results: /tmp/rollproof-results-7f3a
```

- [ ] **Step 3: Write the failing tests**

`internal/report/console_test.go`. It uses the helpers and `passRun` of `result_test.go` (Task 30).

```go
package report

import (
	"strings"
	"testing"
)

// withValue sets the value of the setting with the given key.
func withValue(s []Setting, key, value string) {
	for i := range s {
		if s[i].Key == key {
			s[i].Value = value
		}
	}
}

// failRun is the record of section 10's run that failed.
func failRun() Run {
	r := passRun()
	r.Verdict = "fail"
	r.Windows = []Window{
		{Name: "steady", Start: at(0), End: at(10_000), Result: "pass", Checks: 2000, SlowestMs: 39, LagMs: 5},
		{Name: "deploy", Start: at(10_000), End: at(39_100), Result: "fail", Checks: 5820, Failed: 243, FailedAfterFinish: 31,
			FailedByClass: map[string]int{"refused": 198, "timeout": 45}, FirstFailure: at(15_310), SlowestMs: 198, LagMs: 12},
		{Name: "rollback", Result: "not-run"},
	}
	r.Transitions = []Transition{{Name: "deploy", Command: "ok", Workloads: []Workload{ordersReplaced(800)}}}
	r.Reasons = []Reason{{
		Code: "probe-failures", Verdict: "fail", Window: "deploy",
		Text: "243 checks failed (refused 198, timeout 45), first at 10:00:07.31",
		Detail: []string{
			"stopping then   orders-6d5f9c-x2k4q (previous release), told to stop 0.3 s before",
			"starting then   none",
		},
		Decided: true,
	}}
	return r
}

// notVerifiedRun is the record of section 10's run that could not test. The
// probe's entry is filled, as the run fills it once the configuration is read;
// the console leaves it out because no window began.
func notVerifiedRun() Run {
	r := passRun()
	r.Verdict = "not-verified"
	r.Windows = []Window{{Name: "steady", Result: "not-run"}, {Name: "deploy", Result: "not-run"}, {Name: "rollback", Result: "not-run"}}
	r.Transitions = nil
	r.Images[0].RunBy = nil
	r.Reasons = []Reason{{
		Code: "nothing-watched", Verdict: "not-verified", Text: "nothing to watch",
		Detail:  []string{"15 s after the install command returned, namespace orders held no Deployment, StatefulSet or DaemonSet that matches the selector app=order"},
		Decided: true,
	}}
	return r
}

// stallRun is a deploy that stalled because the surge pod found no node.
func stallRun() Run {
	r := passRun()
	r.Verdict = "fail"
	r.Windows = []Window{
		r.Windows[0],
		{Name: "deploy", Start: at(10_000), End: at(310_000), Result: "fail", Checks: 60000, SlowestMs: 35, LagMs: 4},
		{Name: "rollback", Result: "not-run"},
	}
	r.Transitions = []Transition{{Name: "deploy", Stalled: true, Command: "ok",
		Workloads: []Workload{{Workload: "deployment/orders", PodsAtStart: 3, PodsCreated: 1, Probed: "yes"}}}}
	r.Images[0].RunBy = nil
	r.Reasons = []Reason{
		{
			Code: "transition-stalled", Verdict: "fail", Window: "deploy", Text: "did not finish within limits.timeout",
			Detail: []string{
				`deployment/orders is not complete: Waiting for deployment "orders" rollout to finish: 1 out of 3 new replicas have been updated...`,
				"pod orders-7c9d8b-q8w2n is Pending, not scheduled (Unschedulable): 0/4 nodes are available: 1 node(s) had untolerated taint {node-role.kubernetes.io/control-plane: }, 3 node(s) didn't match pod anti-affinity rules. preemption: 0/4 nodes are available: 1 Preemption is not helpful for scheduling, 3 No preemption victims found for incoming pod.",
			},
			Decided: true,
		},
		{
			Code: "pod-unschedulable", Verdict: "fail", Window: "deploy",
			Text: "orders-7c9d8b-q8w2n could not be scheduled; the test cluster has 3 worker nodes (cluster.workers); a rolling update needs room for one more pod",
		},
	}
	return r
}

// changedRun is a pass with four workers, a steady window of five minutes,
// a rate of 100, and a second watched workload that the probe does not reach.
func changedRun() Run {
	r := passRun()
	r.Windows[0].End, r.Windows[0].Checks = at(300_000), 30000
	r.Windows[1].Start, r.Windows[1].End, r.Windows[1].Checks = at(300_000), at(333_400), 3340
	r.Windows[2].Start, r.Windows[2].End, r.Windows[2].Checks = at(333_400), at(367_300), 3390
	withValue(r.Settings, "cluster.workers", "4")
	withValue(r.Settings, "limits.steady", "5m")
	withValue(r.Probes[0].Settings, "probe.http.rate", "100")
	r.Cluster.Workers = 4
	for i := range r.Transitions {
		r.Transitions[i].Workloads = append(r.Transitions[i].Workloads, Workload{
			Workload: "deployment/orders-worker", PodsAtStart: 2, PodsReplaced: 2, PodsCreated: 2, Probed: "no", QuickestStopMs: stopMs(1200),
		})
	}
	return r
}

func TestConsoleExamples(t *testing.T) {
	// The three examples of section 10 are copied from the design and are never rewritten.
	for _, c := range []struct {
		path string
		run  Run
	}{
		{"testdata/console-pass.golden.txt", passRun()},
		{"testdata/console-fail.golden.txt", failRun()},
		{"testdata/console-not-verified.golden.txt", notVerifiedRun()},
	} {
		t.Run(c.path, func(t *testing.T) { sameAsFile(t, c.path, []byte(Console(c.run))) })
	}
	for _, c := range []struct {
		path string
		run  Run
	}{
		{"testdata/console-stall.golden.txt", stallRun()},
		{"testdata/console-changed.golden.txt", changedRun()},
	} {
		t.Run(c.path, func(t *testing.T) { golden(t, c.path, []byte(Console(c.run))) })
	}
}

func TestConsoleLines(t *testing.T) {
	const affinity = "the Service sends one client to one pod; every connection of the probe reaches the same pod until that pod is removed"
	const warning = "deleting the cluster: it was already gone"
	for _, c := range []struct {
		name   string
		base   func() Run
		change func(r *Run)
		want   string
	}{
		{"failures crossed a start", failRun, func(r *Run) { r.Windows[1].CrossedStart = 3 },
			"   not run\n3 failed checks began before the deploy started and ended in it\n\ntransition"},
		{"one failure crossed a start", failRun, func(r *Run) { r.Windows[1].CrossedStart = 1 },
			"\n1 failed check began before the deploy started and ended in it\n"},
		{"JSON lines with no ok", passRun, func(r *Run) { r.Probes[0].JSONLinesWithoutOK = 12 },
			"status 200-299\n            12 lines were JSON objects with no ok field\n"},
		{"one JSON line with no ok", passRun, func(r *Run) { r.Probes[0].JSONLinesWithoutOK = 1 },
			"status 200-299\n            1 line was a JSON object with no ok field\n"},
		{"https", passRun, func(r *Run) { r.Probes[0].CertificateChecked = false },
			"status 200-299\n            the server's certificate was not verified\n"},
		{"a note of the target", passRun, func(r *Run) { r.Probes[0].Notes = []string{affinity} },
			"status 200-299\n            " + affinity + "\n"},
		{"a probe that did not stop", passRun, func(r *Run) { r.Probes[0].StoppedWhenAsked = false },
			"status 200-299\n            the probe did not stop when asked; checks still in flight were not reported\n"},
		{"variables taken out", passRun, func(r *Run) { r.RemovedVariables = []string{"HELM_DRIVER", "HELM_NAMESPACE"} },
			"status 200-299\ntaken out of the commands' environment: HELM_DRIVER, HELM_NAMESPACE\n\nwindow"},
		{"a warning after a pass", passRun, func(r *Run) { r.Warnings = []string{warning} },
			"  a node drain\n\nwarning: " + warning + "\nresults: /tmp/rollproof-results-7f3a\n"},
		{"a warning after a fail", failRun, func(r *Run) { r.Warnings = []string{warning} },
			"  starting then   none\nwarning: " + warning + "\nresults: /tmp/rollproof-results-7f3a\n"},
		{"no probed workload replaced", passRun, func(r *Run) { r.Transitions[1].Workloads[0].Probed = "no" },
			"  deploy      the candidate's start-up and the previous release's shutdown\n" +
				"  rollback    no probed workload had a pod replaced: the probe watched a rollout that it did not reach\n"},
		{"whether the probe reached a workload is not known", passRun, func(r *Run) { r.Transitions[1].Workloads[0].Probed = "not known" },
			"  rollback    the previous release's start-up and the candidate's shutdown\n"},
		{"values files and inline keys", passRun, func(r *Run) {
			r.Releases[1].ValuesFiles = []string{"values-test.yaml"}
			r.Releases[1].InlineKeys = []string{"image", "replicas"}
		}, "            candidate  orders 1.5.0, ./chart; values: values-test.yaml, inline (image, replicas)\n"},
		{"an image run in two transitions", passRun, func(r *Run) {
			r.Images[0].RunBy = []RunBy{{Workload: "deployment/orders", Transition: "install"}, {Workload: "deployment/orders", Transition: "deploy"}}
		}, "images      orders     run by deployment/orders, in pods the install created\n" +
			"                       run by deployment/orders, in pods the deploy created\n"},
		{"a window that ran and was not judged", failRun, func(r *Run) {
			r.Windows[2] = Window{Name: "rollback", Start: at(39_100), End: at(72_000), Result: "not-judged", Checks: 6580, SlowestMs: 180, LagMs: 30}
		}, "\nrollback   32.9 s    6,580        0              0    180 ms    30 ms   not judged\n"},
		{"a workload removed and one added", passRun, func(r *Run) {
			r.Transitions[0].Workloads = []Workload{
				{Workload: "deployment/orders", PodsAtStart: 2, PodsReplaced: 2, Removed: true, Probed: "yes", QuickestStopMs: stopMs(5300)},
				{Workload: "deployment/orders-v2", PodsCreated: 2, Added: true, Probed: "yes"},
			}
		}, "deploy      deployment/orders      yes      removed         5.3 s\n" +
			"            deployment/orders-v2   yes      added           -\n"},
		{"no cluster yet", notVerifiedRun, func(r *Run) { r.Cluster = nil },
			"rollproof: NOT VERIFIED   orders (Helm)\n\nreleases"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := c.base()
			c.change(&r)
			if got := Console(r); !strings.Contains(got, c.want) {
				t.Errorf("Console has no\n%s\nin\n%s", c.want, got)
			}
		})
	}
}

func TestConsoleBeforeTheConfiguration(t *testing.T) {
	r := Run{
		Verdict: "not-verified",
		Reasons: []Reason{{Code: "config-rejected", Verdict: "not-verified", Text: "deploy.namespace: kube-system is reserved", Decided: true}},
		Out:     "/tmp/rollproof-results-7f3a",
	}
	want := "rollproof: NOT VERIFIED\n\ncould not test: deploy.namespace: kube-system is reserved   [config-rejected]\nresults: /tmp/rollproof-results-7f3a\n"
	if got := Console(r); got != want {
		t.Errorf("Console =\n%s\nwant\n%s", got, want)
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./internal/report/ -run Console -v`
Expected: FAIL: `undefined: Console`

- [ ] **Step 5: Implement `func Console(r Run) string` in `internal/report/console.go`**

With its interface comment from the Interfaces block. It is a pure function: no clock, no environment, no terminal width. The nine lines of what a pass did not exercise are the spec's, in its order, as the example prints them:

```go
var notExercised = []string{
	"the delay before a larger cluster stops routing to a stopping pod",
	"connections held open, and requests in flight on a pod after new connections left it",
	"the path through an ingress, a load balancer or a mesh",
	"routing that depends on where the client is",
	"capacity while pods are down, and autoscaling",
	"storage that attaches to one node at a time",
	"latency below the timeout",
	"data migrations",
	"a node drain",
}
```

The layout. The text is blocks joined by one empty line; every line ends with a line end; a block with no line is left out. "The probe started" means that a window has a `Start`.

1. **The verdict line.** "rollproof: " and PASS, FAIL or NOT VERIFIED (for `pass`, `fail`, and any other verdict). When `Name` is set: three spaces, the name, and " (Helm)" for the driver `helm` (another driver in parentheses as it is). When `Cluster` is set: " on Kubernetes " + `Cluster.Kubernetes` + " in " + `Cluster.Provider`.
2. **The set-up block.** Its first column is 12 characters wide: the word on the first line of each group, spaces on the others.
   1. One line per release, under "releases": the role in a column as wide as the longest of the roles and, when the images lines print, of the image names, plus two spaces; then `CHART VERSION, SOURCE; ` and either "chart defaults", when the release has no values file and no inline key, or "values: " and the values files and "inline (KEY, KEY)", joined by ", ".
   2. Only when the probe started, under "images", for each image: in the same role column its name on its first line, then one line per `RunBy` "run by WORKLOAD, in pods the TRANSITION created", or one line "run by no watched pod".
   3. Only when the probe started, for each probe of kind `http`: under "probe", "built-in HTTP: " and `Target`; then, indented 12 spaces, "RATE a second (one check every N ms), timeout T, status S" from the probe's settings `probe.http.rate`, `probe.http.timeout` and `probe.http.expect-status`, with N = 1000 / RATE with one decimal when it is not whole (5, 3.3) and T the duration with a space before its unit (2 s, 500 ms). Then, each on its own line, indented 12 spaces, in this order: "N lines were JSON objects with no ok field" when `JSONLinesWithoutOK` is not 0 ("1 line was a JSON object with no ok field"); "the server's certificate was not verified" when `CertificateChecked` is false; each of `Notes`; "the probe did not stop when asked; checks still in flight were not reported" when `StoppedWhenAsked` is false.
   4. When a setting's `Value` differs from its `Default`, of `Settings` and then of each probe's `Settings`, whether or not the probe started: the line "settings that differ from their defaults", then one line each: two spaces, the key, two spaces, the value, two spaces, and "(default DEFAULT)".
   5. When `RemovedVariables` is not empty: "taken out of the commands' environment: " and the names joined by ", ".
3. **The windows table**, only when the probe started. Columns window, length, checks, failed, after finish, slowest, behind, result; each as wide as its widest cell or header; three spaces between columns; window and result left-aligned, the others right-aligned; the last column not padded. A window with no `Start` has "-" in every column but result. Otherwise: length is `End - Start` in seconds with one decimal and " s" ("-" with no `End`); checks, failed and after finish (`FailedAfterFinish`) with a comma for thousands, and after finish "-" for the steady window; slowest is `SlowestMs` rounded with `math.Round` and " ms", "-" when no check passed (`Checks == Failed`); behind is `LagMs` the same way, "-" when `Checks` is 0. Results print as pass, FAIL, NOT VERIFIED, not run and not judged. Under the rows, for each window whose `CrossedStart` is not 0: "N failed checks began before the WINDOW started and ended in it" ("1 failed check began …").
4. **The transitions table**, when a transition has a workload. Headers transition, workload, probed, pods replaced, quickest stop. The first column is 12 characters wide; each other column is as wide as its widest cell or header and is followed by three spaces; all left-aligned; the last column not padded. One row for each workload of each transition, the transition's name on its first row only; probed is `Probed` as it is. Pods replaced: "added" when `Added`, else "removed" when `Removed`, else "not replaced" when `PodsReplaced` is 0, else "R of S" (`PodsReplaced` of `PodsAtStart`). Quickest stop: `QuickestStopMs` in seconds with one decimal and " s", "-" when nil.
5. **The lists**, for a pass only. "exercised", then one line per transition: two spaces, its name padded to 12, and "the candidate's start-up and the previous release's shutdown" for the deploy or "the previous release's start-up and the candidate's shutdown" for the rollback; when every workload of the transition has `Probed` "yes" or "no" and none with "yes" has `PodsReplaced` above 0, "no probed workload had a pod replaced: the probe watched a rollout that it did not reach" instead. When a workload's `Probed` is "not known" (a custom probe, or a URL whose host names no Service of the cluster), the transition's ordinary line is printed. Then "not exercised" and `notExercised`, each indented two spaces.
6. **The last block.** Each reason: "WINDOW: TEXT   [CODE]", or "could not test: TEXT   [CODE]" when its `Window` is empty; then each `Detail` line indented two spaces. A `Detail` line longer than 92 characters with its indent (92 is the widest line of section 10's examples) is broken at the last space that keeps it within 92 and goes on in a new line with the same indent; a word longer than that stays whole; a shorter line is printed as it is, its spaces kept. Then "warning: TEXT" for each warning. Then "results: " and `Out`.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/report/ -v`
Expected: PASS

Run: `go vet ./...`
Expected: no output

- [ ] **Step 7: Commit**

```bash
git add internal/report
git commit -m "Print a run's result for people, as the design's examples show it"
```
