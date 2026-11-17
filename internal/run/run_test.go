package run

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

func TestNoGoroutineOutlivesTheRun(t *testing.T) {
	rows := []struct {
		name   string
		signal bool
	}{
		{"a run that passes", false},
		{"a run a signal cuts short", true},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			w := newWorld(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if row.signal {
				w.hooks["transition-deploy"] = cancel
			}
			before := runtime.NumGoroutine()
			w.run(ctx)
			deadline := time.Now().Add(5 * time.Second)
			for runtime.NumGoroutine() > before {
				if time.Now().After(deadline) {
					t.Fatalf("%d goroutines before the run, %d after it", before, runtime.NumGoroutine())
				}
				runtime.Gosched()
			}
		})
	}
}

func TestToolFaultAfterTheLastWindow(t *testing.T) {
	w := newWorld(t)
	w.probe.events = append(w.probe.events, probe.Event{Fault: &verdict.Condition{
		Code: verdict.InternalError, Text: "the probe's log could not be followed without a gap",
		At: t0.Add(80 * time.Second)}})
	if code := w.run(context.Background()); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	r := w.result()
	if diff := cmp.Diff([]string{"pass", "pass", "not-verified"}, r.results()); diff != "" {
		t.Errorf("window results (-want +got):\n%s", diff)
	}
	if window, _ := r.windowOf("internal-error"); window != "rollback" {
		t.Errorf("internal-error in window %q, want rollback", window)
	}
	if len(r.Warnings) != 0 {
		t.Errorf("warnings %q, want none", r.Warnings)
	}
}

func TestEvidenceThatCannotBeOpenedIsAWarning(t *testing.T) {
	w := newWorld(t)
	w.hooks["store"] = func() {
		dir := filepath.Join(w.out, "evidence")
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dir, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if code := w.run(context.Background()); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	r := w.result()
	if r.Verdict != "pass" || len(r.Warnings) != 2 {
		t.Fatalf("verdict %q, warnings %q; want a pass with two warnings", r.Verdict, r.Warnings)
	}
	if !strings.Contains(r.Warnings[0], "probe.log") || !strings.Contains(r.Warnings[1], "windows.json") {
		t.Errorf("warnings %q do not name the two files", r.Warnings)
	}
}

func TestWindowMarks(t *testing.T) {
	w := newWorld(t)
	if code := w.run(context.Background()); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	b, err := os.ReadFile(filepath.Join(w.out, "evidence", "windows.json"))
	if err != nil {
		t.Fatal(err)
	}
	var marks []struct {
		Name     string     `json:"name"`
		Start    *time.Time `json:"start"`
		Finished *time.Time `json:"finished"`
		End      *time.Time `json:"end"`
	}
	if err := json.Unmarshal(b, &marks); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range marks {
		names = append(names, m.Name)
	}
	if diff := cmp.Diff([]string{"steady", "deploy", "rollback"}, names); diff != "" {
		t.Errorf("names (-want +got):\n%s", diff)
	}
	if marks[0].Finished != nil {
		t.Errorf("the steady window's finished is %v, want null", marks[0].Finished)
	}
	if marks[1].Finished == nil || !marks[1].Finished.Equal(t0.Add(25*time.Second)) {
		t.Errorf("the deploy window's finished is %v, want %v", marks[1].Finished, t0.Add(25*time.Second))
	}
	if marks[2].End == nil || !marks[2].End.Equal(t0.Add(70*time.Second)) {
		t.Errorf("the rollback window's end is %v, want %v", marks[2].End, t0.Add(70*time.Second))
	}
}
