> Part of the plan in `README.md` of this folder. Read that file first: its Global Constraints apply to every task here.

### Task 32: `run`: from the units' facts to conditions and to the record

**Files:**
- Create: `internal/run/conditions.go`, `internal/run/record.go`
- Test: `internal/run/conditions_test.go`, `internal/run/record_test.go`

**Interfaces:**
- Consumes: `type Result struct { Name string; Ending Ending; ExitCode int; Began, Ended time.Time; Stdout string }` and the Ending constants, `type Release` of deploy (Tasks 23 and 24); `type Transition`, `type Count`, `type Install`, `type PodNote` (Tasks 25 and 26); `type Decision`, `type WindowResult`, `type Reason`, `type Condition` (Tasks 12 and 13); `type Facts`, `type Target` (Tasks 28 and 29); `type Config`, `type Setting`, `type ImageRef` (Tasks 17 and 19); `type Run` of report and its nested types (Task 30).
- Produces: `module internal/run`
  The sequence of one run, from the results folder to the exit code. It
  hides the order of the phases, where each condition is placed, and what
  is cleaned up. It sees the probe and the deploy driver as interfaces and
  holds no rule of the verdict.
- Produces: `func installCondition(r deploy.Result) (verdict.Condition, bool)`
  The condition an install's command gives, if any: install-failed when it
  exited non-zero, could not be started, or was stopped at the limit.
- Produces: `func commandCondition(r deploy.Result) (verdict.Condition, bool)`
  The condition a transition's command gives, if any: command-failed when
  it exited non-zero or could not be started; transition-stalled, with a
  text that says the tool stopped the command, when it ran to the limit.
- Produces: `type facts struct { cfg *config.Config; decision verdict.Decision; transitions []transitionFacts; install watch.Install; releases []deploy.Release; probe probe.Facts; probeStopped bool; target probe.Target; images map[string]config.ImageRef; removed, warnings []string; cluster *report.Cluster; version, out string }`
  Everything the run has learned, in the units' own types, at the moment
  it writes a record. probeStopped says that the probe was asked to stop
  and that probe holds what its Stop returned.
- Produces: `type transitionFacts struct { tr watch.Transition; cmd deploy.Result; stopping, starting []watch.PodNote }`
  One transition as the units reported it: what the watcher saw, how the
  command ended, and the pods that were stopping or starting at the
  window's first failure.
- Produces: `func record(f facts) report.Run`
  The report's record of the run so far, final or not. It copies and
  renames; it decides nothing.

Read first: section 7 ("When a command fails", "When a command runs too long", "When a signal stops the run") and section 10 (the list of reasons, what `result.json` holds, the console's bullets on probe failures and on images), and Task 30's schema.

- [ ] **Step 1: Write the failing tests for the two conditions**

`internal/run/conditions_test.go`:

```go
package run

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"rollproof/internal/deploy"
	"rollproof/internal/verdict"
)

func TestInstallCondition(t *testing.T) {
	rows := []struct {
		name string
		in   deploy.Result
		want verdict.Condition
		ok   bool
	}{
		{"succeeded", deploy.Result{Ending: deploy.Succeeded, ExitCode: 0}, verdict.Condition{}, false},
		{"exited non-zero", deploy.Result{Ending: deploy.ExitedNonZero, ExitCode: 1},
			verdict.Condition{Code: verdict.InstallFailed, Text: "the install command exited with code 1"}, true},
		{"not started", deploy.Result{Ending: deploy.NotStarted, ExitCode: -1},
			verdict.Condition{Code: verdict.InstallFailed, Text: "the install command could not be started"}, true},
		{"stopped at the limit", deploy.Result{Ending: deploy.StoppedAtLimit, ExitCode: -1},
			verdict.Condition{Code: verdict.InstallFailed, Text: "the install command was still running at limits.timeout and the tool stopped it"}, true},
		{"stopped by a signal", deploy.Result{Ending: deploy.StoppedBySignal, ExitCode: -1}, verdict.Condition{}, false},
		{"the reading of the revision failed", deploy.Result{Name: "status-install", Ending: deploy.ExitedNonZero, ExitCode: 1},
			verdict.Condition{Code: verdict.InstallFailed, Text: "the command status-install, which follows the install, exited with code 1"}, true},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			got, ok := installCondition(row.in)
			if ok != row.ok {
				t.Fatalf("ok = %v, want %v", ok, row.ok)
			}
			if diff := cmp.Diff(row.want, got); diff != "" {
				t.Errorf("condition (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCommandCondition(t *testing.T) {
	rows := []struct {
		name string
		in   deploy.Result
		want verdict.Condition
		ok   bool
	}{
		{"succeeded", deploy.Result{Ending: deploy.Succeeded, ExitCode: 0}, verdict.Condition{}, false},
		{"exited non-zero", deploy.Result{Ending: deploy.ExitedNonZero, ExitCode: 3},
			verdict.Condition{Code: verdict.CommandFailed, Text: "the command exited with code 3"}, true},
		{"not started", deploy.Result{Ending: deploy.NotStarted, ExitCode: -1},
			verdict.Condition{Code: verdict.CommandFailed, Text: "the command could not be started"}, true},
		{"stopped at the limit", deploy.Result{Ending: deploy.StoppedAtLimit, ExitCode: -1},
			verdict.Condition{Code: verdict.TransitionStalled, Text: "the command was still running at limits.timeout and the tool stopped it"}, true},
		{"stopped by a signal", deploy.Result{Ending: deploy.StoppedBySignal, ExitCode: -1}, verdict.Condition{}, false},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			got, ok := commandCondition(row.in)
			if ok != row.ok {
				t.Fatalf("ok = %v, want %v", ok, row.ok)
			}
			if diff := cmp.Diff(row.want, got); diff != "" {
				t.Errorf("condition (-want +got):\n%s", diff)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/run/ -run 'TestInstallCondition|TestCommandCondition' -v`
Expected: FAIL: `undefined: installCondition`

- [ ] **Step 3: Implement `installCondition` and `commandCondition` in `internal/run/conditions.go`**

`func installCondition(r deploy.Result) (verdict.Condition, bool)` and `func commandCondition(r deploy.Result) (verdict.Condition, bool)`, each with its comment from the Interfaces block. The file starts with the package comment of `module internal/run` from the Interfaces block. The texts are the tests' texts; the exit code in "exited with code N" is `r.ExitCode`. installCondition names the command by `r.Name`: when it is empty or `install`, the texts are "the install command …"; otherwise "the command NAME, which follows the install, …" with the same three endings ("exited with code N", "could not be started", "was still running at limits.timeout and the tool stopped it"), so that a failed reading of the revision (`status-install`) is not blamed on the install. commandCondition does not read `r.Name`. The condition's At stays zero: the caller places it.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/run/ -run 'TestInstallCondition|TestCommandCondition' -v`
Expected: PASS

- [ ] **Step 5: Write the failing tests for the record**

`internal/run/record_test.go`. `t0`, `ms`, `orders` and `testConfig` are used again by Task 33's tests.

```go
package run

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/types"

	"rollproof/internal/config"
	"rollproof/internal/deploy"
	"rollproof/internal/probe"
	"rollproof/internal/report"
	"rollproof/internal/verdict"
	"rollproof/internal/watch"
)

var t0 = time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)

// ms is t0 plus n milliseconds.
func ms(n int) time.Time { return t0.Add(time.Duration(n) * time.Millisecond) }

func ptr(t time.Time) *time.Time { return &t }

func i64(n int64) *int64 { return &n }

var orders = watch.Workload{Kind: "deployment", Name: "orders"}

// testConfig is a configuration that this build can run, with the defaults
// of section 8.
func testConfig(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	chart := filepath.Join(dir, "chart")
	return &config.Config{
		Path: filepath.Join(dir, "rollproof.yaml"), Dir: dir,
		Cluster: config.Cluster{Workers: 3},
		Images:  map[string]config.Image{"orders": {Local: "orders:candidate"}},
		Deploy: config.Deploy{Namespace: "orders", Helm: &config.Helm{
			Release:   "orders",
			Previous:  &config.Release{Chart: chart, IsPath: true},
			Candidate: &config.Release{Chart: chart, IsPath: true},
		}},
		Probe: config.Probe{HTTP: &config.HTTP{URL: "http://orders.orders.svc/work?ms=20",
			Rate: 200, Timeout: 2 * time.Second, ExpectMin: 200, ExpectMax: 299}},
		Limits: config.Limits{Steady: 10 * time.Second, Settle: 15 * time.Second,
			Timeout: 5 * time.Minute, Silence: 5 * time.Second},
	}
}

// sampleFacts is a run that passed: three windows, a deploy and a rollback of
// deployment/orders, and three images, one of which no pod ran.
func sampleFacts(t *testing.T) facts {
	t.Helper()
	cfg := testConfig(t)
	cfg.Images = map[string]config.Image{
		"previous":  {Local: "rollproof-fixture:previous"},
		"candidate": {Local: "rollproof-fixture:candidate"},
		"aux":       {Local: "aux:1"},
	}
	cfg.Probe.HTTP.Headers = map[string]string{"X-Token": "S3CRET", "Host": "orders.example"}
	count := func(digest string, stop time.Duration) watch.Count {
		return watch.Count{Workload: orders, AtStart: 2, Replaced: 2, Created: 2,
			QuickestStop: stop, Probed: "yes", Digests: []string{digest}}
	}
	return facts{
		cfg: cfg,
		decision: verdict.Decision{
			Verdict: verdict.Pass, GoOn: true,
			Windows: []verdict.WindowResult{
				{Name: verdict.Steady, Start: ms(0), End: ms(10000), Result: verdict.ResultPass, Checks: 2000, SlowestMs: 41, LagMs: 6},
				{Name: verdict.Deploy, Start: ms(10000), End: ms(43400), Result: verdict.ResultPass, Checks: 6680, SlowestMs: 212, LagMs: 38},
				{Name: verdict.Rollback, Start: ms(43400), End: ms(77300), Result: verdict.ResultPass, Checks: 6780, SlowestMs: 187, LagMs: 35},
			},
		},
		transitions: []transitionFacts{
			{tr: watch.Transition{Name: verdict.Deploy, Began: ms(10000), Finished: ms(28400), Ended: ms(43400),
				Workloads: []watch.Count{count("sha256:bbb", 5300*time.Millisecond)}, Replaced: true},
				cmd: deploy.Result{Ending: deploy.Succeeded}},
			{tr: watch.Transition{Name: verdict.Rollback, Began: ms(43400), Finished: ms(62300), Ended: ms(77300),
				Workloads: []watch.Count{count("sha256:aaa", 5200*time.Millisecond)}, Replaced: true},
				cmd: deploy.Result{Ending: deploy.Succeeded}},
		},
		install: watch.Install{Workloads: []watch.Count{
			{Workload: orders, Created: 2, Probed: "yes", Digests: []string{"sha256:aaa"}}}},
		releases: []deploy.Release{
			{Role: "previous", Chart: "orders", Version: "0.1.0", Source: "./chart", InlineKeys: []string{"image"}},
			{Role: "candidate", Chart: "orders", Version: "0.1.0", Source: "./chart",
				ValueFiles: []string{"values-kind.yaml"}, InlineKeys: []string{"image", "readiness"}},
		},
		probe:        probe.Facts{Lines: 78, JSONTextLines: 2, StoppedWhenAsked: true, ClockDifference: 300 * time.Millisecond},
		probeStopped: true,
		target:       probe.Target{Service: &types.NamespacedName{Namespace: "orders", Name: "orders"}},
		images: map[string]config.ImageRef{
			"previous":  {Repository: "rollproof.test/previous", Digest: "sha256:aaa"},
			"candidate": {Repository: "rollproof.test/candidate", Digest: "sha256:bbb"},
			"aux":       {Repository: "rollproof.test/aux", Digest: "sha256:ccc"},
		},
		removed:  []string{"HELM_NAMESPACE"},
		warnings: []string{"the evidence of pod orders-6d5f9c-x2k4q is incomplete"},
		cluster: &report.Cluster{Provider: "kind", ProviderVersion: "v0.33.0", Kubernetes: "v1.37.0",
			NodeImage: "kindest/node:v1.37.0@sha256:ddd", Workers: 3},
		version: "v0.1.0-test",
		out:     "/tmp/rollproof-results-7f3a",
	}
}

func settingKeys(s []report.Setting) []string {
	var keys []string
	for _, x := range s {
		keys = append(keys, x.Key)
	}
	return keys
}

func TestRecordCopiesTheDecision(t *testing.T) {
	f := sampleFacts(t)
	f.transitions = f.transitions[:1]
	f.transitions[0].stopping = []watch.PodNote{{Name: "orders-6d5f9c-x2k4q", Workload: orders, Old: true, For: 300 * time.Millisecond}}
	f.decision = verdict.Decision{
		Verdict: verdict.Fail,
		Reasons: []verdict.Reason{{Code: verdict.ProbeFailures, Verdict: verdict.Fail, Window: verdict.Deploy,
			Text: "243 checks failed (refused 198, timeout 45), first at 10:00:17.31", Decided: true}},
		Windows: []verdict.WindowResult{
			{Name: verdict.Steady, Start: ms(0), End: ms(10000), Result: verdict.ResultPass, Checks: 2000, SlowestMs: 39, LagMs: 5},
			{Name: verdict.Deploy, Start: ms(10000), End: ms(39100), Result: verdict.ResultFail, Checks: 5820, Failed: 243,
				AfterFinish: 31, Crossed: 1, ByClass: map[string]int{"refused": 198, "timeout": 45},
				FirstFailure: ms(17310), SlowestMs: 198, LagMs: 12},
			{Name: verdict.Rollback, Result: verdict.NotRun},
		},
	}
	got := record(f)
	if got.Verdict != "fail" {
		t.Errorf("verdict %q, want fail", got.Verdict)
	}
	wantReasons := []report.Reason{{Code: "probe-failures", Verdict: "fail", Window: "deploy",
		Text: "243 checks failed (refused 198, timeout 45), first at 10:00:17.31", Decided: true,
		Detail: []string{
			"stopping then   orders-6d5f9c-x2k4q (previous release), told to stop 0.3 s before",
			"starting then   none",
		}}}
	if diff := cmp.Diff(wantReasons, got.Reasons, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("reasons (-want +got):\n%s", diff)
	}
	wantWindows := []report.Window{
		{Name: "steady", Start: ptr(ms(0)), End: ptr(ms(10000)), Result: "pass", Checks: 2000, SlowestMs: 39, LagMs: 5},
		{Name: "deploy", Start: ptr(ms(10000)), End: ptr(ms(39100)), Result: "fail", Checks: 5820, Failed: 243,
			FailedAfterFinish: 31, CrossedStart: 1, FailedByClass: map[string]int{"refused": 198, "timeout": 45},
			FirstFailure: ptr(ms(17310)), SlowestMs: 198, LagMs: 12},
		{Name: "rollback", Result: "not-run"},
	}
	if diff := cmp.Diff(wantWindows, got.Windows, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("windows (-want +got):\n%s", diff)
	}
}

func TestRecordFailureDetail(t *testing.T) {
	old := watch.PodNote{Name: "orders-6d5f9c-x2k4q", Workload: orders, Old: true, For: 300 * time.Millisecond}
	young := watch.PodNote{Name: "orders-7f8e9d-k3m2p", Workload: orders, For: 1200 * time.Millisecond}
	second := watch.PodNote{Name: "orders-6d5f9c-p7r2t", Workload: orders, Old: true, For: 2 * time.Second}
	rows := []struct {
		name               string
		window             verdict.WindowName
		stopping, starting []watch.PodNote
		want               []string
	}{
		{"deploy, an old pod stopping", verdict.Deploy, []watch.PodNote{old}, nil, []string{
			"stopping then   orders-6d5f9c-x2k4q (previous release), told to stop 0.3 s before",
			"starting then   none"}},
		{"deploy, a new pod starting", verdict.Deploy, nil, []watch.PodNote{young}, []string{
			"stopping then   none",
			"starting then   orders-7f8e9d-k3m2p (candidate), created 1.2 s before"}},
		{"deploy, two old pods stopping", verdict.Deploy, []watch.PodNote{old, second}, nil, []string{
			"stopping then   orders-6d5f9c-x2k4q (previous release), told to stop 0.3 s before",
			"stopping then   orders-6d5f9c-p7r2t (previous release), told to stop 2.0 s before",
			"starting then   none"}},
		{"rollback, the releases change sides", verdict.Rollback, []watch.PodNote{old}, []watch.PodNote{young}, []string{
			"stopping then   orders-6d5f9c-x2k4q (candidate), told to stop 0.3 s before",
			"starting then   orders-7f8e9d-k3m2p (previous release), created 1.2 s before"}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			f := sampleFacts(t)
			for i := range f.transitions {
				if f.transitions[i].tr.Name == row.window {
					f.transitions[i].stopping, f.transitions[i].starting = row.stopping, row.starting
				}
			}
			f.decision.Verdict = verdict.Fail
			f.decision.Reasons = []verdict.Reason{{Code: verdict.ProbeFailures, Verdict: verdict.Fail,
				Window: row.window, Text: "1 check failed", Decided: true}}
			got := record(f)
			if len(got.Reasons) != 1 {
				t.Fatalf("%d reasons, want 1", len(got.Reasons))
			}
			if diff := cmp.Diff(row.want, got.Reasons[0].Detail); diff != "" {
				t.Errorf("detail (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRecordStallDetail(t *testing.T) {
	f := sampleFacts(t)
	incomplete := []string{
		"deployment/orders: Waiting for deployment \"orders\" rollout to finish: 1 of 3 updated replicas are available...",
		"orders-7f8e9d-k3m2p: Pending; not scheduled: Unschedulable, 0/4 nodes are available",
	}
	f.transitions[0].tr.Stalled = true
	f.transitions[0].tr.Incomplete = incomplete
	f.decision.Verdict = verdict.Fail
	f.decision.Reasons = []verdict.Reason{
		{Code: verdict.TransitionStalled, Verdict: verdict.Fail, Window: verdict.Deploy,
			Text: "did not finish within limits.timeout", Decided: true},
		{Code: verdict.PodUnschedulable, Verdict: verdict.Fail, Window: verdict.Deploy,
			Text: "orders-7f8e9d-k3m2p could not be scheduled"},
	}
	got := record(f)
	if diff := cmp.Diff(incomplete, got.Reasons[0].Detail); diff != "" {
		t.Errorf("detail of transition-stalled (-want +got):\n%s", diff)
	}
	if d := got.Reasons[1].Detail; len(d) != 0 {
		t.Errorf("detail of pod-unschedulable is %q, want none", d)
	}

	f.decision = verdict.Decision{Verdict: verdict.NotVerified, Reasons: []verdict.Reason{{
		Code: verdict.NothingWatched, Verdict: verdict.NotVerified, Decided: true,
		Text: "15 s after the install command returned, namespace orders held no Deployment, StatefulSet or DaemonSet that matches the selector app=order"}}}
	if d := record(f).Reasons[0].Detail; len(d) != 0 {
		t.Errorf("detail of nothing-watched is %q, want none", d)
	}
}

func TestRecordReasonTextLines(t *testing.T) {
	rows := []struct {
		name       string
		code       verdict.Code
		text       string
		wantText   string
		wantDetail []string
	}{
		{"nothing watched, two lines", verdict.NothingWatched,
			"nothing to watch\n15 s after the install command returned, namespace orders held no Deployment, StatefulSet or DaemonSet that matches the selector app=order",
			"nothing to watch",
			[]string{"15 s after the install command returned, namespace orders held no Deployment, StatefulSet or DaemonSet that matches the selector app=order"}},
		{"install failed, three lines", verdict.InstallFailed,
			"the install was not ready within limits.timeout\ndeployment/orders: 0 of 2 updated replicas are available\norders-6d5f9c-x2k4q: Running; container orders waiting: CrashLoopBackOff, last ended with exit code 1",
			"the install was not ready within limits.timeout",
			[]string{"deployment/orders: 0 of 2 updated replicas are available",
				"orders-6d5f9c-x2k4q: Running; container orders waiting: CrashLoopBackOff, last ended with exit code 1"}},
		{"one line", verdict.ClusterFailed, "create the cluster: a node did not start",
			"create the cluster: a node did not start", nil},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			f := sampleFacts(t)
			f.decision = verdict.Decision{Verdict: verdict.NotVerified, Reasons: []verdict.Reason{{
				Code: row.code, Verdict: verdict.NotVerified, Text: row.text, Decided: true}}}
			got := record(f)
			if len(got.Reasons) != 1 {
				t.Fatalf("%d reasons, want 1", len(got.Reasons))
			}
			if got.Reasons[0].Text != row.wantText {
				t.Errorf("text %q, want %q", got.Reasons[0].Text, row.wantText)
			}
			if diff := cmp.Diff(row.wantDetail, got.Reasons[0].Detail, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("detail (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRecordTransitions(t *testing.T) {
	f := sampleFacts(t)
	f.transitions[0].tr.SettleRestarts = []string{"pod orders-7f8e9d-z9y8x of deployment/orders created"}
	f.transitions[0].tr.Workloads = append(f.transitions[0].tr.Workloads, watch.Count{
		Workload: watch.Workload{Kind: "deployment", Name: "orders-worker"}, AtStart: 1, Removed: true, Probed: "no"})
	f.transitions[1].cmd = deploy.Result{Ending: deploy.ExitedNonZero, ExitCode: 1}
	want := []report.Transition{
		{Name: "deploy", Command: "ok", SettleRestartedBy: []string{"pod orders-7f8e9d-z9y8x of deployment/orders created"},
			Workloads: []report.Workload{
				{Workload: "deployment/orders", PodsAtStart: 2, PodsReplaced: 2, PodsCreated: 2, Probed: "yes", QuickestStopMs: i64(5300)},
				{Workload: "deployment/orders-worker", PodsAtStart: 1, Removed: true, Probed: "no"},
			}},
		{Name: "rollback", Command: "exit-nonzero",
			Workloads: []report.Workload{
				{Workload: "deployment/orders", PodsAtStart: 2, PodsReplaced: 2, PodsCreated: 2, Probed: "yes", QuickestStopMs: i64(5200)},
			}},
	}
	if diff := cmp.Diff(want, record(f).Transitions, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("transitions (-want +got):\n%s", diff)
	}
}

func TestRecordProbe(t *testing.T) {
	f := sampleFacts(t)
	f.target.Notes = []string{"the Service sends one client to one pod; every connection of the probe reaches the same pod until that pod is removed"}
	got := record(f)
	if len(got.Probes) != 1 {
		t.Fatalf("%d probes, want 1", len(got.Probes))
	}
	want := report.Probe{
		Kind: "http", Target: "GET http://orders.orders.svc/work?ms=20",
		Headers: []string{"Host", "X-Token"}, Lines: 78, JSONLinesWithoutOK: 2,
		CertificateChecked: true, StoppedWhenAsked: true, Notes: f.target.Notes,
	}
	if diff := cmp.Diff(want, got.Probes[0], cmpopts.EquateEmpty(), cmpopts.IgnoreFields(report.Probe{}, "Settings")); diff != "" {
		t.Errorf("probe (-want +got):\n%s", diff)
	}
	wantKeys := []string{"probe.http.rate", "probe.http.timeout", "probe.http.expect-status"}
	if diff := cmp.Diff(wantKeys, settingKeys(got.Probes[0].Settings)); diff != "" {
		t.Errorf("probe settings (-want +got):\n%s", diff)
	}

	f.cfg.Probe.HTTP.URL = "https://orders.orders.svc/work"
	if record(f).Probes[0].CertificateChecked {
		t.Error("certificate_checked is true for an https URL")
	}

	f.decision.Windows = []verdict.WindowResult{
		{Name: verdict.Steady, Result: verdict.NotRun},
		{Name: verdict.Deploy, Result: verdict.NotRun},
		{Name: verdict.Rollback, Result: verdict.NotRun},
	}
	if got := record(f).Probes; len(got) != 0 {
		t.Errorf("probes %+v, want none when the steady window has no start", got)
	}
}

func TestRecordProbeNotAskedToStop(t *testing.T) {
	f := sampleFacts(t)
	f.probe, f.probeStopped = probe.Facts{}, false
	if got := record(f).Probes; len(got) != 1 || !got[0].StoppedWhenAsked {
		t.Errorf("probes %+v: a probe that was never asked to stop is reported as one that did not stop", got)
	}
	f.probe, f.probeStopped = probe.Facts{StoppedWhenAsked: false}, true
	if got := record(f).Probes; len(got) != 1 || got[0].StoppedWhenAsked {
		t.Errorf("probes %+v: a probe that did not stop when asked is reported as one that did", got)
	}
}

func TestRecordRunFacts(t *testing.T) {
	f := sampleFacts(t)
	f.decision.Ignored = []verdict.Condition{{Code: verdict.ProbeLineInvalid, Text: "t is later than the line", At: ms(80000)}}
	got := record(f)
	if got.Schema != 1 || got.Final || got.Driver != "helm" || got.Name != "orders" ||
		got.Tool.Version != "v0.1.0-test" || got.Out != "/tmp/rollproof-results-7f3a" {
		t.Errorf("schema %d, final %v, driver %q, name %q, tool %q, out %q",
			got.Schema, got.Final, got.Driver, got.Name, got.Tool.Version, got.Out)
	}
	if diff := cmp.Diff(f.cluster, got.Cluster); diff != "" {
		t.Errorf("cluster (-want +got):\n%s", diff)
	}
	wantReleases := []report.Release{
		{Role: "previous", Chart: "orders", Version: "0.1.0", Source: "./chart", InlineKeys: []string{"image"}},
		{Role: "candidate", Chart: "orders", Version: "0.1.0", Source: "./chart",
			ValuesFiles: []string{"values-kind.yaml"}, InlineKeys: []string{"image", "readiness"}},
	}
	if diff := cmp.Diff(wantReleases, got.Releases, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("releases (-want +got):\n%s", diff)
	}
	wantKeys := []string{"cluster.node-image", "cluster.registry-image", "cluster.workers",
		"limits.steady", "limits.settle", "limits.timeout", "limits.silence"}
	if diff := cmp.Diff(wantKeys, settingKeys(got.Settings)); diff != "" {
		t.Errorf("settings (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"HELM_NAMESPACE"}, got.RemovedVariables); diff != "" {
		t.Errorf("removed variables (-want +got):\n%s", diff)
	}
	wantWarnings := []string{
		"the evidence of pod orders-6d5f9c-x2k4q is incomplete",
		"after the last window: probe-line-invalid: t is later than the line",
	}
	if diff := cmp.Diff(wantWarnings, got.Warnings); diff != "" {
		t.Errorf("warnings (-want +got):\n%s", diff)
	}
}

func TestRecordImages(t *testing.T) {
	want := []report.Image{
		{Name: "aux", Digest: "sha256:ccc"},
		{Name: "candidate", Digest: "sha256:bbb", RunBy: []report.RunBy{
			{Workload: "deployment/orders", Transition: "deploy"}}},
		{Name: "previous", Digest: "sha256:aaa", RunBy: []report.RunBy{
			{Workload: "deployment/orders", Transition: "install"},
			{Workload: "deployment/orders", Transition: "rollback"}}},
	}
	if diff := cmp.Diff(want, record(sampleFacts(t)).Images, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("images (-want +got):\n%s", diff)
	}
}

func TestRecordHidesHeaderValues(t *testing.T) {
	data, err := json.Marshal(record(sampleFacts(t)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "S3CRET") {
		t.Errorf("the record holds a header's value:\n%s", data)
	}
	if !strings.Contains(string(data), "X-Token") {
		t.Error("the record lost the header's name")
	}
}

func TestRecordWithoutConfig(t *testing.T) {
	f := facts{
		decision: verdict.Decision{Verdict: verdict.NotVerified, Reasons: []verdict.Reason{{
			Code: verdict.ConfigRejected, Verdict: verdict.NotVerified, Decided: true,
			Text: "the file rollproof.yaml cannot be read"}}},
		version: "v0.1.0-test",
		out:     "/tmp/rollproof-results-7f3a",
	}
	got := record(f)
	if got.Verdict != "not-verified" || len(got.Reasons) != 1 || got.Reasons[0].Code != "config-rejected" {
		t.Errorf("verdict %q, reasons %+v", got.Verdict, got.Reasons)
	}
	if n := len(got.Windows) + len(got.Transitions) + len(got.Probes) + len(got.Releases) +
		len(got.Settings) + len(got.Images); n != 0 {
		t.Errorf("%d entries in the lists, want none", n)
	}
	if got.Driver != "" || got.Name != "" || got.Cluster != nil {
		t.Errorf("driver %q, name %q, cluster %v; want all empty", got.Driver, got.Name, got.Cluster)
	}
}
```

- [ ] **Step 6: Run the tests to verify they fail**

Run: `go test ./internal/run/ -v`
Expected: FAIL: `undefined: facts`

- [ ] **Step 7: Implement `type facts`, `type transitionFacts` and `func record(f facts) report.Run` in `internal/run/record.go`**

Each with its comment from the Interfaces block. `record` copies and renames, by these rules:

1. Always: Schema 1, Final false (the writer marks it), Verdict from the decision, Tool.Version from `f.version`, Out from `f.out`, Cluster from `f.cluster`.
2. Reasons: one `report.Reason` for each `decision.Reasons`, in order; Window is the window's name, empty for a reason with no window. A condition's Text may have more than one line (Task 12): Text is the first line of the reason's text, and Detail is every further line of it, in order, followed by these lines: for `probe-failures`, the pod lines of rule 3 from the `transitionFacts` whose `tr.Name` is the reason's window; for `transition-stalled`, that transition's `tr.Incomplete`; for every other code, none.
3. Pod lines: one line for each stopping pod, `stopping then   NAME (RELEASE), told to stop S s before`, or the one line `stopping then   none`; then one line for each starting pod, `starting then   NAME (RELEASE), created S s before`, or `starting then   none`. S is `For` in seconds with one decimal. RELEASE in the deploy: `previous release` for a pod with Old true, `candidate` otherwise; in the rollback the other way round. There are three spaces after "then".
4. Windows: one `report.Window` for each `decision.Windows`, field by field (AfterFinish to FailedAfterFinish, Crossed to CrossedStart, ByClass to FailedByClass); a zero time becomes nil.
5. Warnings: `f.warnings`, then one for each `decision.Ignored`: `"after the last window: " + c.Error()`.
6. With `f.cfg` nil, nothing more: every other list stays empty and Driver and Name stay empty.
7. Driver `helm` and Name `cfg.Deploy.Helm.Release` when `cfg.Deploy.Helm` is set.
8. Transitions: one for each `f.transitions`, in order: Name, Stalled, Command `string(cmd.Ending)`, SettleRestartedBy from `tr.SettleRestarts`, and one `report.Workload` for each `tr.Workloads` (Workload is `Count.Workload.String()`; QuickestStopMs is `QuickestStop.Milliseconds()`, nil when QuickestStop is zero).
9. Probes: when `cfg.Probe.HTTP` is set and the decision's steady window has a Start (the probe started), one entry; before that, none. The entry: Kind `http`, Target `"GET " + URL`, Settings the entries of `cfg.Settings()` whose key starts with `probe.http`, Headers the header names sorted, Lines and JSONLinesWithoutOK from `f.probe`, CertificateChecked false for an `https` URL and true otherwise (a comment says that on `http` there is no certificate to check), StoppedWhenAsked `f.probe.StoppedWhenAsked || !f.probeStopped` (a probe that was never asked to stop is not reported as one that did not stop), Notes from `f.target.Notes`. A header's value is never read.
10. Releases from `f.releases` (ValueFiles to ValuesFiles); Settings the entries of `cfg.Settings()` whose key does not start with `probe.`; RemovedVariables from `f.removed`.
11. Images: one for each key of `cfg.Images`, sorted by name: Digest from `f.images[name]`; RunBy first `{workload, "install"}` for each Count of `f.install` whose Digests holds the digest, then `{workload, TRANSITION}` for each Count of each transition, in order, whose Digests holds it.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/run/ -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/run/conditions.go internal/run/record.go internal/run/conditions_test.go internal/run/record_test.go
git commit -m "Turn the units' facts into conditions and into the report's record"
```

### Task 33: `run`: the sequence of a run

**Files:**
- Create: `internal/run/run.go`
- Test: `internal/run/run_test.go`, `internal/run/fakes_test.go`

**Interfaces:**
- Consumes: `func installCondition`, `func commandCondition`, `type facts`, `func record(f facts) report.Run` (Task 32); `func Decide(t Timeline) Decision`, `type Timeline`, `type Window`, `func AsCondition(err error) Condition` (Tasks 12 to 14); `type Deployer interface` (Task 24); `type Probe interface`, `type Started`, `type Event`, `type Target` (Tasks 28 and 29); `type Transition`, `type Install`, `type PodNote` (Tasks 25 and 26); `func Open(out string, getenv func(string) string) (*Writer, error)`, `func (w *Writer) Final(r Run) error`, `func (w *Writer) Evidence(name string) (io.WriteCloser, error)`, `func Console(r Run) string`, `type Cluster` of report (Tasks 30 and 31); `type Config`, `func (c *Config) NotBuilt() []string`, `func (c *Config) Resolve(images map[string]ImageRef) error` (Tasks 17 to 19).
- Produces: `type Options struct { ConfigPath, Out, Name string; KeepCluster bool; Stdout, Stderr io.Writer; Version string; Getenv func(string) string }`
  What the run command was given. Version is the tool's own, for the
  record.
- Produces: `type Clock interface { Now() time.Time; After(d time.Duration) <-chan time.Time }`
  The run's source of time, so that a test can drive it.
- Produces: `type Watcher interface { InstallReady(ctx context.Context, began time.Time, exited <-chan struct{}) (watch.Install, error); Transition(ctx context.Context, name verdict.WindowName, began time.Time, exited <-chan struct{}) watch.Transition; PodsAt(at time.Time) (stopping, starting []watch.PodNote); Stop() }`
  What the run needs of the watch unit.
- Produces: `type Cluster interface { Store(ctx context.Context) (probeImage string, images map[string]config.ImageRef, err error); Deployer(cfg *config.Config) (deploy.Deployer, []string, error); Watcher(ctx context.Context, cfg *config.Config, service *types.NamespacedName) (Watcher, error); Target(ctx context.Context, cfg *config.Config) (probe.Target, error); Probe(cfg *config.Config, probeImage string, raw io.Writer) probe.Probe; Facts() report.Cluster; Kept() string; Delete(ctx context.Context) error }`
  A created cluster as the run uses it: each method is one unit's step.
  Deployer also returns the names of the variables taken out of the
  commands' environment. Kept is the sentence for a kept cluster: its
  kubeconfig file and context.
- Produces: `type Units struct { Load func(path string) (*config.Config, error); Check func(ctx context.Context, cfg *config.Config) error; Create func(ctx context.Context, cfg *config.Config, name string, evidence func(string) (io.WriteCloser, error)) (Cluster, error); Clock Clock }`
  The steps that other packages perform. main supplies the real ones and a
  test supplies fakes.
- Produces: `func Run(ctx context.Context, o Options, u Units) int`
  Runs one test from the results folder to the exit code: 0 for a pass, 1
  for a fail, 2 for not verified. When ctx ends, the run is interrupted:
  it writes its final result first and leaves the cluster.

Read first: section 4 (what one run does; windows), section 5 (the flow of one run; the three properties), section 10 (rules 1, 2, 3, 8, 9 and 10; "When it is written"; "On a signal"), section 9 ("Name and lifetime": what is deleted and when; `--keep-cluster`), section 6 ("Lifecycle": Stop), and section 8 (exit codes; a crash of the tool).

- [ ] **Step 1: Write the fakes in `internal/run/fakes_test.go`**

The types, `newWorld` and two event helpers:

```go
package run

import (
	"bytes"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"rollproof/internal/config"
	"rollproof/internal/deploy"
	"rollproof/internal/probe"
	"rollproof/internal/verdict"
	"rollproof/internal/watch"
)

// callLog is the order of the calls that the fakes received, safe for use
// from several goroutines.
type callLog struct {
	mu    sync.Mutex
	names []string
}

// fakeClock is a Clock that moves only by Advance, which also fires every
// timer that is due.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []fakeTimer
}

type fakeTimer struct {
	at time.Time
	ch chan time.Time
}

// world is one scripted run: the clock, the call log, the hooks, what each
// fake returns, and what the run printed.
type world struct {
	t                            *testing.T
	clock                        *fakeClock
	calls                        *callLog
	hooks                        map[string]func()
	out                          string
	keep                         bool
	cfg                          *config.Config
	loadErr, checkErr, createErr error
	cluster                      *fakeCluster
	deployer                     *fakeDeployer
	watcher                      *fakeWatcher
	probe                        *fakeProbe
	stdout, stderr               bytes.Buffer
}

// fakeCluster is the Cluster that Units.Create returns.
type fakeCluster struct {
	w                                                       *world
	storeErr, deployerErr, watcherErr, targetErr, deleteErr error
	images                                                  map[string]config.ImageRef
	target                                                  probe.Target
	removed                                                 []string
	kept                                                    string
	service                                                 *types.NamespacedName
}

// fakeDeployer is the Deployer that fakeCluster.Deployer returns.
type fakeDeployer struct {
	w        *world
	fetchErr error
	results  map[string]deploy.Result
	releases []deploy.Release
}

// fakeWatcher is the Watcher that fakeCluster.Watcher returns.
type fakeWatcher struct {
	w                  *world
	install            watch.Install
	installErr         error
	transitions        map[verdict.WindowName]watch.Transition
	stopping, starting []watch.PodNote
}

// fakeProbe is the probe.Probe that fakeCluster.Probe returns.
type fakeProbe struct {
	w        *world
	startErr error
	events   []probe.Event
	ch       chan probe.Event
	once     sync.Once
	facts    probe.Facts
}

// newWorld scripts a run that passes: the probe reports 200 passed checks
// each second from t0 to t0+70 s; the deploy and the rollback each replace
// both pods of deployment/orders, finish 15 s after they begin, and end 30 s
// after.
func newWorld(t *testing.T) *world {
	w := &world{t: t, clock: &fakeClock{now: t0}, calls: &callLog{}, hooks: map[string]func(){},
		out: t.TempDir(), cfg: testConfig(t)}
	replaced := watch.Transition{Replaced: true, Workloads: []watch.Count{{Workload: orders,
		AtStart: 2, Replaced: 2, Created: 2, QuickestStop: 5200 * time.Millisecond, Probed: "yes",
		Digests: []string{"sha256:aaa"}}}}
	w.cluster = &fakeCluster{w: w,
		images: map[string]config.ImageRef{"orders": {Repository: "rollproof.test/orders", Digest: "sha256:aaa"}},
		target: probe.Target{Service: &types.NamespacedName{Namespace: "orders", Name: "orders"}},
		kept:   "the cluster is kept: kubeconfig /tmp/rollproof-test/kubeconfig, context kind-rollproof-test"}
	w.deployer = &fakeDeployer{w: w, results: map[string]deploy.Result{}, releases: []deploy.Release{
		{Role: "previous", Chart: "orders", Version: "0.1.0", Source: "./chart"},
		{Role: "candidate", Chart: "orders", Version: "0.1.0", Source: "./chart", InlineKeys: []string{"image"}}}}
	w.watcher = &fakeWatcher{w: w,
		install: watch.Install{Workloads: []watch.Count{{Workload: orders, Created: 2, Probed: "yes",
			Digests: []string{"sha256:aaa"}}}},
		transitions: map[verdict.WindowName]watch.Transition{verdict.Deploy: replaced, verdict.Rollback: replaced}}
	w.probe = &fakeProbe{w: w, events: passes(0, 70), facts: probe.Facts{Lines: 71, StoppedWhenAsked: true}}
	return w
}

// passes is one summary line of 200 passed checks for each second from
// t0+from to t0+to, both included.
func passes(from, to int) []probe.Event {
	var events []probe.Event
	for s := from; s <= to; s++ {
		at := t0.Add(time.Duration(s) * time.Second)
		events = append(events, probe.Event{Obs: &verdict.Observation{Written: at, T: at, OK: true, N: 200, Ms: 30, Lag: 2}})
	}
	return events
}

// failureAt is one refused check that began at t0+at and was written at
// t0+written.
func failureAt(at, written time.Duration) probe.Event {
	return probe.Event{Obs: &verdict.Observation{Written: t0.Add(written), T: t0.Add(at), OK: false, N: 1,
		Class: "refused", Ms: 1}}
}
```

The methods, one sentence for each type:

- `callLog`: `add(name string)` appends a name, `list() []string` returns a copy of the names in order, and `count(name string) int` says how often a name was added, all under the mutex.
- `fakeClock`: `Now() time.Time` gives now; `Advance(d time.Duration)` moves now by d and sends the new time on the channel (capacity 1) of every timer that is due; `After(d time.Duration) <-chan time.Time` registers a timer at now plus d and then calls `Advance(d)` itself, so that a run never waits in a test.
- `world`: `units() Units` gives a Load, a Check and a Create that record "load", "check" and "create", call `w.hook` with that name, and return `cfg` or `loadErr`, `checkErr`, and `cluster` or a nil Cluster with `createErr`, with `clock` as the Clock; `run(ctx context.Context) int` calls `Run(ctx, Options{ConfigPath: filepath.Join(w.cfg.Dir, "rollproof.yaml"), Out: w.out, KeepCluster: w.keep, Stdout: &w.stdout, Stderr: &w.stderr, Version: "test", Getenv: func(string) string { return "" }}, w.units())`; `hook(name string)` runs `hooks[name]` when it is set; `result() resultFile` reads `w.out/result.json` and decodes it, and fails the test when it cannot.
- `fakeCluster` implements `Cluster`: Store, Deployer, Watcher, Target, Probe and Delete record "store", "deployer", "watcher", "target", "probe" and "delete", call the hook of that name and return their scripted values (Store gives `"rollproof.test/rollproof@sha256:fff"`, `images` and `storeErr`; Deployer gives `w.deployer`, `removed` and `deployerErr`, with a nil Deployer when the error is set; Watcher keeps the service it was given in `service` and gives `w.watcher` and `watcherErr`, with a nil Watcher when the error is set; Target gives `target` and `targetErr`; Probe gives `w.probe`; Delete gives `deleteErr`), while Facts gives `report.Cluster{Provider: "kind", ProviderVersion: "v0.33.0", Kubernetes: "v1.37.0", NodeImage: "kindest/node:v1.37.0@sha256:ddd", Workers: 3}` and Kept gives `kept`, both recording nothing.
- `fakeDeployer` implements `deploy.Deployer`: Fetch records "fetch" and returns `fetchErr`; Install, Deploy and Rollback record "install", "deploy" and "rollback", call the hook of that name and return `results[name]`, or `deploy.Result{Ending: deploy.Succeeded}` when it has none, with Began and Ended at the clock's time; Transitions gives `[verdict.Deploy, verdict.Rollback]`; Releases gives `releases`.
- `fakeWatcher` implements `Watcher`: InstallReady and Transition first wait until `exited` is closed or ctx ends, then record "install-ready" or "transition-" and the window's name and call the hook of that name; InstallReady returns `install` with `installErr` when it is set, else with ctx's error when ctx has ended; Transition returns `transitions[name]` with Name and Began filled and, unless ctx has ended, Ended at began plus 30 s (began plus `cfg.Limits.Timeout` when the scripted value is Stalled) and Finished at began plus 15 s (zero when Stalled), after moving the clock to Ended, or, when ctx has ended, Ended at the clock's time and Finished zero; PodsAt returns `stopping` and `starting`; Stop records "watch-stop".
- `fakeProbe` implements `probe.Probe`: Start records "start", calls the hook, returns `startErr` when it is set, and otherwise makes `ch` with capacity `len(events)+64`, puts `events` into it and returns `probe.Started{First: the clock's time, Events: ch}`; `send(ev probe.Event)` puts one more event into `ch`; Stop records "probe-stop", calls the hook, closes `ch` once when it exists, and returns `facts` and nil.

The methods add the imports they need to the block above.

- [ ] **Step 2: Write the failing tests for the sequence**

`internal/run/run_test.go`:

```go
package run

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"rollproof/internal/config"
	"rollproof/internal/deploy"
	"rollproof/internal/probe"
	"rollproof/internal/verdict"
	"rollproof/internal/watch"
)

// resultFile is the part of result.json that these tests read.
type resultFile struct {
	Final   bool   `json:"final"`
	Verdict string `json:"verdict"`
	Reasons []struct {
		Code    string `json:"code"`
		Window  string `json:"window"`
		Text    string `json:"text"`
		Decided bool   `json:"decided"`
	} `json:"reasons"`
	Windows []struct {
		Name   string     `json:"name"`
		Result string     `json:"result"`
		End    *time.Time `json:"end"`
	} `json:"windows"`
	Warnings []string `json:"warnings"`
}

// codes are the reasons' codes in order.
func (r resultFile) codes() []string {
	var codes []string
	for _, x := range r.Reasons {
		codes = append(codes, x.Code)
	}
	return codes
}

// decided is the code of the reason marked as the one that decided.
func (r resultFile) decided() string {
	for _, x := range r.Reasons {
		if x.Decided {
			return x.Code
		}
	}
	return ""
}

// results are the windows' results in order.
func (r resultFile) results() []string {
	var results []string
	for _, x := range r.Windows {
		results = append(results, x.Result)
	}
	return results
}

// windowOf is the window of the first reason with the code, and its text.
func (r resultFile) windowOf(code string) (window, text string) {
	for _, x := range r.Reasons {
		if x.Code == code {
			return x.Window, x.Text
		}
	}
	return "", ""
}

func TestPass(t *testing.T) {
	w := newWorld(t)
	w.hooks["probe-stop"] = func() {
		if w.result().Final {
			t.Error("result.json was final before the probe was stopped")
		}
	}
	if code := w.run(context.Background()); code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, w.stdout.String())
	}
	want := []string{"load", "check", "create", "store", "deployer", "fetch", "target", "watcher",
		"install", "install-ready", "probe", "start", "deploy", "transition-deploy",
		"rollback", "transition-rollback", "probe-stop", "watch-stop", "delete"}
	if diff := cmp.Diff(want, w.calls.list()); diff != "" {
		t.Errorf("calls (-want +got):\n%s", diff)
	}
	r := w.result()
	if !r.Final || r.Verdict != "pass" {
		t.Errorf("final %v, verdict %q; want a final pass", r.Final, r.Verdict)
	}
	if diff := cmp.Diff([]string{"pass", "pass", "pass"}, r.results()); diff != "" {
		t.Errorf("window results (-want +got):\n%s", diff)
	}
	if !strings.HasPrefix(w.stdout.String(), "rollproof: PASS") {
		t.Errorf("stdout does not start with the console text:\n%s", w.stdout.String())
	}
}

func TestDeployFailsRollbackNotRun(t *testing.T) {
	w := newWorld(t)
	w.probe.events = append(w.probe.events, failureAt(20*time.Second, 20*time.Second))
	if code := w.run(context.Background()); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if n := w.calls.count("rollback"); n != 0 {
		t.Errorf("rollback called %d times, want 0", n)
	}
	if n := w.calls.count("probe-stop"); n != 1 {
		t.Errorf("probe stopped %d times, want 1", n)
	}
	r := w.result()
	if r.Verdict != "fail" || r.decided() != "probe-failures" {
		t.Errorf("verdict %q decided by %q; want fail by probe-failures", r.Verdict, r.decided())
	}
	if diff := cmp.Diff([]string{"pass", "fail", "not-run"}, r.results()); diff != "" {
		t.Errorf("window results (-want +got):\n%s", diff)
	}
}

func TestEachBeforeFailure(t *testing.T) {
	setUp := []string{"load", "check", "create", "store", "deployer", "fetch"}
	then := func(tail ...string) []string { return append(append([]string{}, setUp...), tail...) }
	rows := []struct {
		name  string
		fault func(w *world)
		code  string
		calls []string
	}{
		{"load", func(w *world) {
			w.loadErr = verdict.Condition{Code: verdict.ConfigRejected, Text: "unknown key nope at line 3"}
		}, "config-rejected", []string{"load"}},
		{"check", func(w *world) {
			w.checkErr = verdict.Condition{Code: verdict.RunnerUnsupported, Text: "no container engine that kind supports"}
		}, "runner-unsupported", []string{"load", "check"}},
		{"create", func(w *world) {
			w.createErr = verdict.Condition{Code: verdict.ClusterFailed, Text: "create the cluster: a node did not start"}
		}, "cluster-failed", []string{"load", "check", "create"}},
		{"store", func(w *world) {
			w.cluster.storeErr = verdict.Condition{Code: verdict.ClusterFailed, Text: "node rollproof-x-worker could not pull rollproof.test/orders"}
		}, "cluster-failed", []string{"load", "check", "create", "store", "delete"}},
		{"fetch", func(w *world) {
			w.deployer.fetchErr = verdict.Condition{Code: verdict.ChartFetchFailed, Text: "the previous release's chart could not be fetched"}
		}, "chart-fetch-failed", then("delete")},
		{"target", func(w *world) {
			w.cluster.targetErr = verdict.Condition{Code: verdict.TargetUnsupported, Text: "the Service orders/orders is headless"}
		}, "target-unsupported", then("target", "delete")},
		{"install exits non-zero", func(w *world) {
			w.deployer.results["install"] = deploy.Result{Ending: deploy.ExitedNonZero, ExitCode: 1}
			w.watcher.installErr = verdict.Condition{Code: verdict.NothingWatched, Text: "must not be used"}
		}, "install-failed", then("target", "watcher", "install", "install-ready", "watch-stop", "delete")},
		{"nothing watched", func(w *world) {
			w.watcher.installErr = verdict.Condition{Code: verdict.NothingWatched,
				Text: "15 s after the install command returned, namespace orders held no Deployment, StatefulSet or DaemonSet"}
		}, "nothing-watched", then("target", "watcher", "install", "install-ready", "watch-stop", "delete")},
		{"probe not started", func(w *world) {
			w.probe.startErr = verdict.Condition{Code: verdict.ProbeNotStarted, Text: "no observation within limits.timeout"}
		}, "probe-not-started", then("target", "watcher", "install", "install-ready", "probe", "start", "watch-stop", "delete")},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			w := newWorld(t)
			row.fault(w)
			if code := w.run(context.Background()); code != 2 {
				t.Fatalf("exit %d, want 2", code)
			}
			if diff := cmp.Diff(row.calls, w.calls.list()); diff != "" {
				t.Errorf("calls (-want +got):\n%s", diff)
			}
			r := w.result()
			if !r.Final || r.Verdict != "not-verified" {
				t.Errorf("final %v, verdict %q; want a final not-verified", r.Final, r.Verdict)
			}
			if diff := cmp.Diff([]string{row.code}, r.codes()); diff != "" {
				t.Errorf("reasons (-want +got):\n%s", diff)
			}
			if r.decided() != row.code {
				t.Errorf("decided by %q, want %q", r.decided(), row.code)
			}
		})
	}
}

func TestNotBuilt(t *testing.T) {
	w := newWorld(t)
	w.cfg.Probe = config.Probe{Custom: &config.Custom{
		Image: "registry.example/tools/busybox@sha256:" + strings.Repeat("0", 64)}}
	if code := w.run(context.Background()); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if diff := cmp.Diff([]string{"load"}, w.calls.list()); diff != "" {
		t.Errorf("calls (-want +got):\n%s", diff)
	}
	if window, text := w.result().windowOf("config-rejected"); window != "" || text != "not built yet: probe.custom" {
		t.Errorf("config-rejected in window %q with text %q", window, text)
	}
}

func TestResultsFolderCannotBeWritten(t *testing.T) {
	w := newWorld(t)
	w.out = filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(w.out, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := w.run(context.Background()); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.HasPrefix(w.stderr.String(), "rollproof: cannot write results: ") {
		t.Errorf("stderr is %q", w.stderr.String())
	}
	if calls := w.calls.list(); len(calls) != 0 {
		t.Errorf("calls %q, want none", calls)
	}
}

func TestFirstFileBeforeAnything(t *testing.T) {
	w := newWorld(t)
	w.hooks["load"] = func() {
		r := w.result()
		if r.Final {
			t.Error("result.json is final when Load is called")
		}
		if diff := cmp.Diff([]string{"internal-error"}, r.codes()); diff != "" {
			t.Errorf("first file's reasons (-want +got):\n%s", diff)
		}
	}
	w.run(context.Background())
	if w.calls.count("load") != 1 {
		t.Error("Load was not called")
	}
}

func TestStall(t *testing.T) {
	w := newWorld(t)
	w.probe.events = passes(0, 320)
	w.watcher.transitions[verdict.Deploy] = watch.Transition{
		Stalled:       true,
		Incomplete:    []string{"deployment/orders: 1 of 3 updated replicas are available"},
		Unschedulable: []string{"orders-7f8e9d-k3m2p: 0/4 nodes are available: 3 node(s) didn't match pod anti-affinity rules"},
		Workloads:     []watch.Count{{Workload: orders, AtStart: 3, Created: 1, Probed: "yes"}},
	}
	w.deployer.results["deploy"] = deploy.Result{Ending: deploy.StoppedAtLimit, ExitCode: -1}
	if code := w.run(context.Background()); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	r := w.result()
	if diff := cmp.Diff([]string{"transition-stalled", "pod-unschedulable"}, r.codes()); diff != "" {
		t.Errorf("reasons (-want +got):\n%s", diff)
	}
	if r.decided() != "transition-stalled" {
		t.Errorf("decided by %q, want transition-stalled", r.decided())
	}
	if _, text := r.windowOf("transition-stalled"); !strings.Contains(text,
		"the command was still running at limits.timeout and the tool stopped it") {
		t.Errorf("the stall's text does not say that the tool stopped the command: %q", text)
	}
	if diff := cmp.Diff([]string{"pass", "fail", "not-run"}, r.results()); diff != "" {
		t.Errorf("window results (-want +got):\n%s", diff)
	}
}

func TestCommandFailed(t *testing.T) {
	w := newWorld(t)
	w.deployer.results["deploy"] = deploy.Result{Ending: deploy.ExitedNonZero, ExitCode: 1}
	tr := w.watcher.transitions[verdict.Deploy]
	tr.Replaced = false
	w.watcher.transitions[verdict.Deploy] = tr
	if code := w.run(context.Background()); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	r := w.result()
	if diff := cmp.Diff([]string{"command-failed"}, r.codes()); diff != "" {
		t.Errorf("reasons (-want +got):\n%s", diff)
	}
	if window, _ := r.windowOf("command-failed"); window != "deploy" {
		t.Errorf("command-failed in window %q, want deploy", window)
	}
	if end := r.Windows[1].End; end == nil || !end.Equal(t0.Add(40*time.Second)) {
		t.Errorf("the deploy window ended at %v, want when the Watcher returned, %v", end, t0.Add(40*time.Second))
	}
	if n := w.calls.count("rollback"); n != 0 {
		t.Errorf("rollback called %d times, want 0", n)
	}
}

func TestNoPodReplaced(t *testing.T) {
	w := newWorld(t)
	tr := w.watcher.transitions[verdict.Deploy]
	tr.Replaced = false
	tr.Workloads = []watch.Count{{Workload: orders, AtStart: 2, Probed: "yes"}}
	w.watcher.transitions[verdict.Deploy] = tr
	if code := w.run(context.Background()); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	r := w.result()
	if diff := cmp.Diff([]string{"no-pod-replaced"}, r.codes()); diff != "" {
		t.Errorf("reasons (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"pass", "not-verified", "not-run"}, r.results()); diff != "" {
		t.Errorf("window results (-want +got):\n%s", diff)
	}
}

func TestLateFailure(t *testing.T) {
	w := newWorld(t)
	w.hooks["transition-rollback"] = func() {
		w.probe.send(failureAt(35*time.Second, 45*time.Second))
	}
	if code := w.run(context.Background()); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	r := w.result()
	if diff := cmp.Diff([]string{"pass", "fail", "not-judged"}, r.results()); diff != "" {
		t.Errorf("window results (-want +got):\n%s", diff)
	}
	if window, _ := r.windowOf("probe-failures"); window != "deploy" || r.decided() != "probe-failures" {
		t.Errorf("probe-failures in window %q, decided by %q", window, r.decided())
	}
}

func TestProbeFaultIsPlacedByItsTime(t *testing.T) {
	w := newWorld(t)
	w.probe.events = append(w.probe.events, probe.Event{Fault: &verdict.Condition{
		Code: verdict.ProbeExited, Text: "the probe exited with code 1", At: t0.Add(50 * time.Second)}})
	if code := w.run(context.Background()); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	r := w.result()
	if diff := cmp.Diff([]string{"pass", "pass", "not-verified"}, r.results()); diff != "" {
		t.Errorf("window results (-want +got):\n%s", diff)
	}
	if window, _ := r.windowOf("probe-exited"); window != "rollback" {
		t.Errorf("probe-exited in window %q, want rollback", window)
	}
}

func TestKeepCluster(t *testing.T) {
	w := newWorld(t)
	w.keep = true
	if code := w.run(context.Background()); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if n := w.calls.count("delete"); n != 0 {
		t.Errorf("Delete called %d times, want 0", n)
	}
	if !strings.Contains(w.stdout.String(), w.cluster.kept) {
		t.Errorf("stdout does not name the kept cluster:\n%s", w.stdout.String())
	}
}

func TestDeleteFails(t *testing.T) {
	w := newWorld(t)
	w.cluster.deleteErr = errors.New("the engine did not answer")
	if code := w.run(context.Background()); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	r := w.result()
	if r.Verdict != "pass" || len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "the engine did not answer") {
		t.Errorf("verdict %q, warnings %q", r.Verdict, r.Warnings)
	}
	if !strings.Contains(w.stdout.String(), "warning: ") || !strings.Contains(w.stdout.String(), "the engine did not answer") {
		t.Errorf("stdout has no warning:\n%s", w.stdout.String())
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/run/ -v`
Expected: FAIL: the package does not compile, `undefined: Units`

- [ ] **Step 4: Implement `func Run(ctx context.Context, o Options, u Units) int` in `internal/run/run.go`**

With `type Options`, `type Clock`, `type Watcher`, `type Cluster` and `type Units`, each with its interface comment from the Interfaces block.

The sequence. Each step's error goes through verdict.AsCondition; a step named "Before" places it in Timeline.Before.
 0. w := report.Open(o.Out, o.Getenv). On an error: write "rollproof: cannot write results: ERROR" to o.Stderr and return 2. Nothing else has been done (Review Focus 3).
 1. Before: cfg := u.Load(o.ConfigPath). Then, if cfg.NotBuilt() is not empty: a ConfigRejected condition, text "not built yet: KEY, KEY".
 2. Before: u.Check(ctx, cfg).
 3. Before: c := u.Create(ctx, cfg, o.Name, w.Evidence).
 4. Before: probeImage, images := c.Store(ctx); then cfg.Resolve(images) (an error there is an InternalError).
 5. Before: d, removed := c.Deployer(cfg); d.Fetch(ctx).
 6. Before: target := c.Target(ctx, cfg); wt := c.Watcher(ctx, cfg, target.Service).
 7. Before: the install. began := now; start d.Install(ctx) in a goroutine that closes `exited` when it returns; inst, err := wt.InstallReady(ctx, began, exited). The conditions: installCondition of the command's Result when it gives one, else err.
 8. Before: p := c.Probe(cfg, probeImage, raw) with raw from w.Evidence("probe.log"); st, err := p.Start(ctx).
 9. The windows are fixed now: steady, then d.Transitions(). The steady window's Start is st.First. A goroutine reads st.Events into the Timeline (observations to Observations, faults to Timed) under a mutex, until the channel is closed.
 10. Wait until st.First + limits.steady (Clock.After). Set the steady window's End. decision := Decide (with Now from the Clock). If !decision.GoOn: go to 12.
 11. For each transition in order: Start := the previous window's End; section 4 allows no gap between windows. Run d.Deploy or d.Rollback in a goroutine that closes `exited`; tr := wt.Transition(ctx, name, Start, exited); wait for the command's Result (it has exited, or ctx ended). The window's Finished is tr.Finished and its End tr.Ended. Its conditions: tr.Conditions(result.Ending == Succeeded), and commandCondition(result) when it gives one. When the window has a first failure, keep wt.PodsAt(that moment) for the record. decision := Decide. If !decision.GoOn: stop the loop.
 12. p.Stop(ctx); wait until the Events channel is closed. final := Decide. The verdict is decided here (rule 10).
 13. w.Final(record(...)); write the window marks to w.Evidence("windows.json"); print report.Console to o.Stdout.
 14. wt.Stop(). Unless o.KeepCluster: c.Delete(ctx2), with a context that a signal does not end. With o.KeepCluster: print c.Kept(). An error in step 13's evidence or in this step is a warning: add it and call w.Final again.
 15. return final.Verdict.ExitCode().

When a "Before" step fails: no later step runs; if a probe was started it is stopped; go to 13 with the decision of a Timeline whose Before has the condition. A cluster that was created is deleted in 14.

The record is filled from what the run has so far; a run that ended early has fewer parts.

What the list leaves open:

1. State. One unexported struct holds the writer, cfg, the cluster, the deployer, the watcher, the probe, the Timeline with its mutex, and what each unit returned (the facts of Task 32), so that steps 13 to 15 can run from any point of the sequence.
2. Time. "now" is `u.Clock.Now()`. The steady window's End is `st.First` plus `limits.steady`; step 10 waits on `u.Clock.After` of that moment minus now. Every Decide gets Silence `cfg.Limits.Silence` and Now `u.Clock.Now()`.
3. Events. The goroutine of step 9 is the only reader of `st.Events`. Before each Decide, run sends it a request (a reply channel) and waits until it closes the reply; on a request the goroutine first takes every event already waiting in the channel. Decide then sees everything the probe has delivered. Step 12 waits until the goroutine has seen the channel closed.
4. Commands. The goroutine of step 7 or 11 sends the command's Result on a channel of capacity 1 and then closes `exited`. In step 7 InstallReady gets a context derived from ctx that this goroutine cancels when installCondition gives a condition, so that a failed install does not wait out `limits.timeout`; after InstallReady returns, run waits for the Result.
5. A stall reported twice. When `tr.Conditions` gives TransitionStalled and commandCondition gives TransitionStalled too, only the watcher's is placed, its Text followed by "; " and the command's Text. Conditions are placed with a zero At into the window's Conditions.
6. First failures. After each Decide of steps 11 and 12, for each transition window whose WindowResult has a FirstFailure and whose pods are not kept yet, keep `wt.PodsAt(FirstFailure)` as that transition's stopping and starting pods.
7. Releases are `d.Releases()` after a Fetch that succeeded, and none otherwise.
8. A failed `p.Start`: Stop is not called. The probe's pod goes with the cluster, or stays in a kept one for debugging.
9. Step 13: record gets the struct's facts, with cluster a pointer to `c.Facts()` when a cluster exists, out `w.Dir()`, and probeStopped true once `p.Stop` has returned (probe then holds the Facts it returned). `windows.json` is a JSON array with one object for each window of the Timeline, `{"name", "start", "finished", "end"}`, times in RFC 3339 with nanoseconds and null when zero. The Kept sentence is printed after the console text, on a line of its own.
10. Step 14: `wt.Stop()` when a watcher exists. The warnings are "the window marks were not written: ERROR" and "the cluster was not deleted: ERROR". Each is added to the record's warnings and printed to `o.Stdout` as `warning: TEXT`, and then `w.Final` is called again.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/run/ -v`
Expected: PASS

- [ ] **Step 6: Write the failing tests for a signal and for a panic**

Add to `internal/run/run_test.go`:

```go
func TestSignalDuringDeploy(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.hooks["transition-deploy"] = func() {
		w.clock.Advance(5 * time.Second)
		cancel()
	}
	if code := w.run(ctx); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	r := w.result()
	if !r.Final {
		t.Error("result.json is not final")
	}
	if diff := cmp.Diff([]string{"interrupted"}, r.codes()); diff != "" {
		t.Errorf("reasons (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"pass", "not-verified", "not-run"}, r.results()); diff != "" {
		t.Errorf("window results (-want +got):\n%s", diff)
	}
	if n := w.calls.count("delete"); n != 0 {
		t.Errorf("Delete called %d times, want 0", n)
	}
	if n := w.calls.count("probe-stop"); n != 0 {
		t.Errorf("the probe's Stop called %d times, want 0", n)
	}
}

func TestSignalAfterDeployFailed(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.probe.events = append(w.probe.events, failureAt(12*time.Second, 12*time.Second))
	w.hooks["transition-deploy"] = func() {
		w.clock.Advance(20 * time.Second)
		cancel()
	}
	if code := w.run(ctx); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	r := w.result()
	if diff := cmp.Diff([]string{"probe-failures", "interrupted"}, r.codes()); diff != "" {
		t.Errorf("reasons (-want +got):\n%s", diff)
	}
	if r.Verdict != "fail" || r.decided() != "probe-failures" {
		t.Errorf("verdict %q decided by %q; want fail by probe-failures", r.Verdict, r.decided())
	}
}

func TestSignalWhileStoppingTheProbe(t *testing.T) {
	rows := []struct {
		name   string
		failed bool
		exit   int
	}{
		{"every window clean", false, 2},
		{"the rollback had failed", true, 1},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			w := newWorld(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if row.failed {
				w.probe.events = append(w.probe.events, failureAt(50*time.Second, 50*time.Second))
			}
			w.hooks["probe-stop"] = cancel
			if code := w.run(ctx); code != row.exit {
				t.Fatalf("exit %d, want %d", code, row.exit)
			}
			r := w.result()
			if window, _ := r.windowOf("interrupted"); window != "rollback" {
				t.Errorf("interrupted in window %q, want rollback", window)
			}
			if n := w.calls.count("delete"); n != 0 {
				t.Errorf("Delete called %d times, want 0", n)
			}
		})
	}
}

func TestSignalAfterTheVerdict(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.hooks["delete"] = cancel
	if code := w.run(ctx); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	r := w.result()
	if !r.Final || r.Verdict != "pass" {
		t.Errorf("final %v, verdict %q; want a final pass", r.Final, r.Verdict)
	}
	if diff := cmp.Diff([]string{"interrupted after the verdict was decided"}, r.Warnings); diff != "" {
		t.Errorf("warnings (-want +got):\n%s", diff)
	}
}

func TestPanicInAUnit(t *testing.T) {
	rows := []struct {
		name   string
		failed bool
		exit   int
		codes  []string
	}{
		{"the deploy window clean", false, 2, []string{"internal-error"}},
		{"the deploy window had failed", true, 1, []string{"probe-failures", "internal-error"}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			w := newWorld(t)
			if row.failed {
				w.probe.events = append(w.probe.events, failureAt(12*time.Second, 12*time.Second))
			}
			w.hooks["transition-deploy"] = func() {
				w.clock.Advance(20 * time.Second)
				panic("the watcher broke")
			}
			if code := w.run(context.Background()); code != row.exit {
				t.Fatalf("exit %d, want %d", code, row.exit)
			}
			r := w.result()
			if diff := cmp.Diff(row.codes, r.codes()); diff != "" {
				t.Errorf("reasons (-want +got):\n%s", diff)
			}
			if window, text := r.windowOf("internal-error"); window != "deploy" || text != "the watcher broke" {
				t.Errorf("internal-error in window %q with text %q", window, text)
			}
			if n := w.calls.count("delete"); n != 1 {
				t.Errorf("Delete called %d times, want 1", n)
			}
		})
	}
}
```

- [ ] **Step 7: Run the tests to verify they fail**

Run: `go test ./internal/run/ -run 'TestSignal|TestPanic' -v`
Expected: FAIL: the signal tests report a call to Delete or a reason other than `interrupted`, and the run ends with `panic: the watcher broke`.

- [ ] **Step 8: Handle a signal and a panic in `internal/run/run.go`**

A signal (ctx ends) at any step before 12 is done: place a Condition with Interrupted in the window in progress (an open window's End becomes now), or in Before when no window has begun; do not wait for a running command (the Runner stops it; its Ending is StoppedBySignal and gives no condition); do NOT call p.Stop with a grace wait and do NOT delete the cluster; go to 13 at once, then return. During step 12 (the probe is being stopped): the Interrupted condition goes into the last window that began. After step 12: the signal is a warning, "interrupted after the verdict was decided", and the cluster is left.

A panic in any step is recovered at the top of Run: a Condition with InternalError and the panic's text, placed like a signal (window in progress, else Before, else the last window); then 13; the cluster is deleted as in 14.

What these two paragraphs leave open:

1. After every step, and when a wait of step 10, 11 or 12 ends, ctx is checked first. When it has ended, the step's own error is dropped and `verdict.Condition{Code: verdict.Interrupted, Text: "a signal stopped the run"}` is placed instead. The transition that was cut has none of its own conditions placed.
2. Both Interrupted and InternalError go into the open window (Start set, End zero; its End becomes now), else into Before when no window has begun, else into the last window that began.
3. On a signal, run asks the event reader once for what waits (rule 3 of Step 4) and does not wait for the channel to close.
4. After step 12, a panic is a warning too: "the tool failed after the verdict was decided: TEXT". A signal that has come by the start of step 14 leaves the cluster: Delete is not called. One that comes during Delete (which runs under a context that no signal ends) is found when Delete returns.
5. The recover at the top of Run is a deferred recover around steps 1 to 12; after it, Run goes on with step 13. Every goroutine that run starts recovers a panic too, hands the text to the main flow and closes its channels; the main flow then panics again with that text.
6. `wt.Stop()` is called on every path, a signal's included: it waits for nothing.

- [ ] **Step 9: Run the tests to verify they pass**

Run: `go test ./internal/run/ -v`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
git add internal/run/run.go internal/run/run_test.go internal/run/fakes_test.go
git commit -m "Sequence a run: set-up, install, the windows, the verdict, the report and the clean-up"
```

### Task 34: `cmd/rollproof`: the commands, and the real units

**Files:**
- Create: `cmd/rollproof/main.go`, `cmd/rollproof/wire.go`
- Test: `cmd/rollproof/main_test.go`

**Interfaces:**
- Consumes: `func Run(ctx context.Context, o Options, u Units) int`, `type Units`, `type Cluster interface`, `type Watcher interface`, `type Clock` (Task 33); `func Load` (Tasks 17 to 19); `func CheckRunner`, `func Name`, `func RunDir`, `func Create`, `type Spec`, `func DefaultNodeImage`, `const DefaultRegistryImage`, `func KindVersion` (Tasks 20 and 21); `func Store` (Task 22); `func NewRunner`, `func (r *OSRunner) Removed() []string`, `func CheckHelm`, `func NewHelm` (Tasks 23 and 24); `func Start` of watch (Task 27); `func CheckTarget`, `func NewPod`, `func HTTPArgs`, `func ParseHTTPArgs`, `func RunHTTP` (Tasks 16, 28 and 29).
- Produces: `rollproof run [-f FILE] [--out DIR] [--name NAME] [--keep-cluster]`
  Runs one test. FILE defaults to rollproof.yaml in the working folder.
  Exits 0 for a pass, 1 for a fail and 2 for not verified, a rejected
  configuration and a wrong command line included.
- Produces: `rollproof probe http --url URL --rate N --timeout D --expect-status RANGE [--header NAME=VALUE]…`
  The built-in probe, as its pod runs it. It writes contract lines to
  standard output until it gets SIGTERM, then drains and exits 0.
- Produces: `rollproof version`
  Prints the tool's version, the kind release it links, and the references
  of the default node image and the default registry image, one a line.

Read first: section 8 ("The command line", "What the runner must hold"), section 9 ("Name and lifetime"; "The kubeconfig": what `--keep-cluster` prints) and section 6 (the drain of the built-in probe: the request timeout plus 5 s).

- [ ] **Step 1: Write the failing tests**

`cmd/rollproof/main_test.go`. None of them needs a cluster: the two runs end in Load or in NotBuilt.

```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"rollproof/internal/probe"
)

func noEnv(string) string { return "" }

// lockedBuffer is a bytes.Buffer that several goroutines may write to.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// readResult reads the reasons of OUT/result.json.
func readResult(t *testing.T, out string) (final bool, codes, texts []string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Final   bool `json:"final"`
		Reasons []struct {
			Code string `json:"code"`
			Text string `json:"text"`
		} `json:"reasons"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	for _, x := range r.Reasons {
		codes = append(codes, x.Code)
		texts = append(texts, x.Text)
	}
	return r.Final, codes, texts
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"nope"},
		{"run", "--nope"},
		{"run", "-f", "rollproof.yaml", "extra"},
		{"probe"},
		{"probe", "tcp"},
	} {
		var stdout, stderr bytes.Buffer
		if code := realMain(args, &stdout, &stderr, noEnv); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
		if !strings.Contains(stderr.String(), "usage: rollproof run") {
			t.Errorf("%q: no usage text on stderr:\n%s", args, stderr.String())
		}
		if n := strings.Count(stderr.String(), "\n"); n > 10 {
			t.Errorf("%q: %d lines on stderr, want at most 10", args, n)
		}
	}
}

func TestVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := realMain([]string{"version"}, &stdout, &stderr, noEnv); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	prefixes := []string{"rollproof ", "kind ", "node image ", "registry image "}
	if len(lines) != len(prefixes) {
		t.Fatalf("%d lines, want %d:\n%s", len(lines), len(prefixes), stdout.String())
	}
	for i, p := range prefixes {
		if !strings.HasPrefix(lines[i], p) {
			t.Errorf("line %d is %q, want the prefix %q", i+1, lines[i], p)
		}
	}
	if !strings.Contains(lines[2], "@sha256:") {
		t.Errorf("the node image has no digest: %q", lines[2])
	}
}

func TestRunWithoutAFile(t *testing.T) {
	for _, flag := range []string{"-out", "--out"} {
		out := t.TempDir()
		var stdout, stderr bytes.Buffer
		if code := realMain([]string{"run", "-f", "does-not-exist.yaml", flag, out}, &stdout, &stderr, noEnv); code != 2 {
			t.Fatalf("%s: exit %d, want 2", flag, code)
		}
		final, codes, _ := readResult(t, out)
		if !final || len(codes) != 1 || codes[0] != "config-rejected" {
			t.Errorf("%s: final %v, reasons %q; want a final file with config-rejected", flag, final, codes)
		}
	}
}

func TestRunNotBuilt(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "chart"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "rollproof.yaml")
	text := `version: 1
deploy:
  namespace: orders
  helm:
    release: orders
    previous: { chart: ./chart }
    candidate: { chart: ./chart }
probe:
  custom:
    image: registry.example/tools/busybox@sha256:` + strings.Repeat("0", 64) + `
    command: [sh, -c, "sleep 3600"]
`
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"-keep-cluster", "--keep-cluster"} {
		out := t.TempDir()
		var stdout, stderr bytes.Buffer
		if code := realMain([]string{"run", "-f", file, "--out", out, flag}, &stdout, &stderr, noEnv); code != 2 {
			t.Fatalf("%s: exit %d, want 2\n%s", flag, code, stderr.String())
		}
		final, codes, texts := readResult(t, out)
		if !final || len(codes) != 1 || codes[0] != "config-rejected" || texts[0] != "not built yet: probe.custom" {
			t.Errorf("%s: final %v, reasons %q %q", flag, final, codes, texts)
		}
	}
}

func TestProbeHTTP(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := realMain([]string{"probe", "http", "--url", "http://127.0.0.1:1/", "--rate", "0"}, &stdout, &stderr, noEnv); code != 2 {
		t.Errorf("rate 0: exit %d, want 2", code)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	old := probeContext
	probeContext = func() (context.Context, context.CancelFunc) { return ctx, cancel }
	t.Cleanup(func() { probeContext = old })

	out := &lockedBuffer{}
	var errOut bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- realMain([]string{"probe", "http", "--url", srv.URL + "/", "--rate", "20", "--timeout", "2s",
			"--expect-status", "200-299"}, out, &errOut, noEnv)
	}()
	time.Sleep(time.Second)
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d, want 0\n%s", code, errOut.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("probe http did not return within 5 s of the cancel")
	}
	text := strings.TrimSuffix(out.String(), "\n")
	if text == "" {
		t.Fatal("probe http wrote no line")
	}
	written := time.Now()
	for _, line := range strings.Split(text, "\n") {
		if got := probe.ParseLine(line, written); got.Kind != probe.Observed {
			t.Errorf("line %q is not an observation", line)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/rollproof/ -v`
Expected: FAIL: `undefined: realMain`

- [ ] **Step 3: Write `cmd/rollproof/main.go`**

- `var version = "dev"`, set at build time with `-ldflags "-X main.version=…"`.
- `func main()` is `os.Exit(realMain(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))`.
- `func realMain(args []string, stdout, stderr io.Writer, getenv func(string) string) int` picks the subcommand by `args[0]`.
- `var probeContext = func() (context.Context, context.CancelFunc) { return signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt) }`, the hook that the probe test replaces.
- The usage text, exactly:

```text
usage: rollproof run [-f FILE] [--out DIR] [--name NAME] [--keep-cluster]
       rollproof version
       rollproof probe http --url URL --rate N --timeout D --expect-status RANGE [--header NAME=VALUE]...
```

The subcommands:

1. No argument, or a first argument that is none of `run`, `probe` and `version`: the usage text on stderr, return 2.
2. `run`: a `flag.FlagSet` named run with `flag.ContinueOnError`, its output on stderr and its Usage printing the usage text; flags `f` (default `rollproof.yaml`), `out`, `name` (strings) and `keep-cluster` (bool), each a pointer that `fs.String` or `fs.Bool` returns. A parse error or an argument left after the flags: return 2. Otherwise `ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)` with `defer stop()`, and return `run.Run(ctx, run.Options{ConfigPath: *f, Out: *out, Name: *name, KeepCluster: *keepCluster, Stdout: stdout, Stderr: stderr, Version: version, Getenv: getenv}, realUnits(*keepCluster))`.
3. `probe`: `probe.ParseHTTPArgs(args)` with the whole list, which starts with `probe` and `http` as `HTTPArgs` gives it; ParseHTTPArgs itself rejects a list that does not start with those two words. Its error goes to stderr with the usage text, return 2. Then `ctx, stop := probeContext()` with `defer stop()`, and `probe.RunHTTP(ctx, o, stdout)`; an error (the host cannot be resolved) is printed to stderr as `rollproof: ERROR`, return 1; else 0.
4. `version`: with any further argument, the usage text and 2. Otherwise four lines on stdout: `rollproof VERSION`, `kind ` and `cluster.KindVersion()`, `node image ` and `cluster.DefaultNodeImage()`, `registry image ` and `cluster.DefaultRegistryImage`; return 0.

- [ ] **Step 4: Write `cmd/rollproof/wire.go`**

```go
// realUnits joins the real units into the steps of a run. keep is
// --keep-cluster: a cluster that failed to come up is then kept too.
func realUnits(keep bool) run.Units

// wiring is what one run's real units share: the engine that Check found.
type wiring struct{ engine cluster.Engine }

// session is a created cluster as run.Cluster: each method calls one unit.
type session struct {
	c        *cluster.Cluster
	client   kubernetes.Interface
	cfg      *config.Config
	evidence func(string) (io.WriteCloser, error)
}

var _ run.Cluster = (*session)(nil)

// realClock is the run's clock: time.Now and time.After.
type realClock struct{}

// httpOptions are the built-in probe's settings as the probe takes them.
func httpOptions(h *config.HTTP) probe.HTTPOptions
```

What each part calls:

- Load: `config.Load(path, os.LookupEnv)`.
- Check: `cluster.CheckRunner(ctx)`, keeping the Engine in the wiring for Create; then `deploy.CheckHelm()` when `cfg.Deploy.Helm` is set.
- Create: `n, err := cluster.Name(cfg.Path, name)` (its error is a ConfigRejected condition already); then `cluster.Create(ctx, cluster.Spec{Name: n, RunDir: cluster.RunDir(n), NodeImage: cfg.Cluster.NodeImage, RegistryImage: cfg.Cluster.RegistryImage, Workers: cfg.Cluster.Workers, Namespace: cfg.Deploy.Namespace, Timeout: cfg.Limits.Timeout, Keep: keep}, engine)`; then `c.Client()`. When the client cannot be made, Create calls `c.Delete` when keep is false, because `run` deletes no cluster whose Create failed, and returns a ClusterFailed condition, "the cluster's API server cannot be reached: ERROR". A kept cluster stays for debugging (section 9, "Name and lifetime"). It returns a session that keeps c, the client, cfg and the evidence function, or a nil `run.Cluster` with the error.
- Store: `images.Store(ctx, s.c, exe, s.cfg.Images, s.cfg.Limits.Timeout)` with exe from `os.Executable()`; it returns `stored.Probe.Ref()` and `stored.Images`.
- Deployer: `runner := deploy.NewRunner(deploy.RunnerOptions{Dir: cfg.Dir, Kubeconfig: s.c.Kubeconfig, Environ: os.Environ(), Evidence: func(n string) (io.WriteCloser, error) { return s.evidence("commands/" + n) }})`, then `deploy.NewHelm(deploy.HelmOptions{Helm: *cfg.Deploy.Helm, Namespace: cfg.Deploy.Namespace, Kubeconfig: s.c.Kubeconfig, Context: s.c.Context, RunDir: s.c.RunDir, Dir: cfg.Dir, Timeout: cfg.Limits.Timeout, Runner: runner})`; it returns the driver and `runner.Removed()`.
- Target: `probe.CheckTarget(ctx, s.client, cfg.Probe.HTTP.URL)`.
- Watcher: `watch.Start(ctx, s.client, watch.Options{Namespace: cfg.Deploy.Namespace, Selector: cfg.Deploy.Selector, Settle: cfg.Limits.Settle, Timeout: cfg.Limits.Timeout, Workers: cfg.Cluster.Workers, Service: service, Now: time.Now})`; a nil `run.Watcher` with the error when it fails.
- Probe: `probe.NewPod(probe.PodOptions{Client: s.client, Image: probeImage, Args: probe.HTTPArgs(httpOptions(cfg.Probe.HTTP)), Drain: cfg.Probe.HTTP.Timeout + 5*time.Second, StartTimeout: cfg.Limits.Timeout, Raw: raw, Now: time.Now})`.
- Facts: `report.Cluster{Provider: "kind", ProviderVersion: cluster.KindVersion(), Kubernetes: s.c.KubernetesVersion, NodeImage: s.c.NodeImage, Workers: s.c.Workers}`.
- Kept: `fmt.Sprintf("the cluster is kept: kubeconfig %s, context %s", s.c.Kubeconfig, s.c.Context)`.
- Delete: `s.c.Delete(ctx)`.
- Clock: `realClock{}`.

`httpOptions` copies URL, Headers, Rate, Timeout, ExpectMin and ExpectMax.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./cmd/rollproof/ -v`
Expected: PASS

Run: `go vet ./...`
Expected: no output.

- [ ] **Step 6: Build the binary and run it**

Run: `env CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=dev" -o bin/rollproof ./cmd/rollproof`
Expected: no output.

Run: `bin/rollproof version`
Expected: four lines, `rollproof dev`, `kind v0.33.0`, `node image kindest/node:v1.37.0@sha256:…` and `registry image docker.io/library/registry:3@sha256:…`.

- [ ] **Step 7: Commit**

```bash
git add cmd/rollproof/main.go cmd/rollproof/wire.go cmd/rollproof/main_test.go
git commit -m "Add the rollproof command: run, probe http and version, wired to the real units"
```
