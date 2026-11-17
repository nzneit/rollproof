package run

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
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
		Code    string   `json:"code"`
		Window  string   `json:"window"`
		Text    string   `json:"text"`
		Detail  []string `json:"detail"`
		Decided bool     `json:"decided"`
	} `json:"reasons"`
	Windows []struct {
		Name   string     `json:"name"`
		Result string     `json:"result"`
		End    *time.Time `json:"end"`
	} `json:"windows"`
	Warnings []string `json:"warnings"`
	Probes   []struct {
		Target string `json:"target"`
	} `json:"probes"`
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

// detailOf is the detail lines of the first reason with the code.
func (r resultFile) detailOf(code string) []string {
	for _, x := range r.Reasons {
		if x.Code == code {
			return x.Detail
		}
	}
	return nil
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
	want := []string{"load", "check", "create", "store", "deployer", "fetch", "watcher",
		"install", "install-ready", "target", "probe", "start", "deploy", "transition-deploy",
		"rollback", "transition-rollback", "probe-stop", "end-commands", "watch-stop", "delete"}
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

// TestUseServiceAfterTheTarget pins where the watcher gets its Service: the one
// the target named, given after the target check and before the probe starts.
func TestUseServiceAfterTheTarget(t *testing.T) {
	w := newWorld(t)
	if code := w.run(context.Background()); code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, w.stdout.String())
	}
	if diff := cmp.Diff(w.cluster.target.Service, w.watcher.service); diff != "" {
		t.Errorf("the Service given to UseService (-want +got):\n%s", diff)
	}
	calls := w.watcher.useCalls
	if len(calls) == 0 || calls[len(calls)-1] != "target" {
		t.Fatalf("UseService came after %q, want the target check last", calls)
	}
	if slices.Contains(calls, "start") {
		t.Errorf("UseService came after the probe started; the calls were %q", calls)
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
		}, "cluster-failed", []string{"load", "check", "create", "store", "end-commands", "delete"}},
		{"fetch", func(w *world) {
			w.deployer.fetchErr = verdict.Condition{Code: verdict.ChartFetchFailed, Text: "the previous release's chart could not be fetched"}
		}, "chart-fetch-failed", then("end-commands", "delete")},
		{"target", func(w *world) {
			w.cluster.targetErr = verdict.Condition{Code: verdict.TargetUnsupported, Text: "the Service orders/orders is headless"}
		}, "target-unsupported", then("watcher", "install", "install-ready", "target", "end-commands", "watch-stop", "delete")},
		{"install exits non-zero", func(w *world) {
			w.deployer.results["install"] = deploy.Result{Ending: deploy.ExitedNonZero, ExitCode: 1}
			w.watcher.installErr = verdict.Condition{Code: verdict.NothingWatched, Text: "must not be used"}
		}, "install-failed", then("watcher", "install", "install-ready", "end-commands", "watch-stop", "delete")},
		{"nothing watched", func(w *world) {
			w.watcher.installErr = verdict.Condition{Code: verdict.NothingWatched,
				Text: "15 s after the install command returned, namespace orders held no Deployment, StatefulSet or DaemonSet"}
		}, "nothing-watched", then("watcher", "install", "install-ready", "end-commands", "watch-stop", "delete")},
		{"probe not started", func(w *world) {
			w.probe.startErr = verdict.Condition{Code: verdict.ProbeNotStarted, Text: "no observation within limits.timeout"}
		}, "probe-not-started", then("watcher", "install", "install-ready", "target", "probe", "start", "end-commands", "watch-stop", "delete")},
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

func TestEndCommandsRunsAfterTheCommandsAreJoined(t *testing.T) {
	w := newWorld(t)
	hold := make(chan struct{})
	w.deployer.block = map[string]chan struct{}{"deploy": hold}
	started := make(chan struct{})
	w.hooks["deploy"] = func() { close(started) } // the command's goroutine is inside the command
	w.watcher.before = func(name verdict.WindowName) {
		if name == verdict.Deploy {
			<-started // the command still runs when the watcher's panic begins
			panic("the watcher broke")
		}
	}
	// The panic leaves the deploy command's goroutine outstanding. The join must end it before
	// EndCommands runs, so EndCommands is reached only once the test lets the command go; the
	// fake fails the test when it is reached while a command still runs.
	reached := make(chan struct{})
	w.hooks["end-commands"] = func() { close(reached) }
	ended := make(chan int, 1)
	go func() { ended <- w.run(context.Background()) }()
	select {
	case <-reached:
		// The calls are in the wrong order: EndCommands ran before the join.
	case <-time.After(100 * time.Millisecond):
		// The right order: the run is still waiting for the command.
	}
	close(hold)
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("the run did not finish after the command was let go")
	}
}

func TestAPanicInEndCommandsIsAWarning(t *testing.T) {
	w := newWorld(t)
	w.hooks["end-commands"] = func() { panic("the runner broke") }
	if code := w.run(context.Background()); code != 0 {
		t.Fatalf("exit %d, want 0: a panic after the verdict changes nothing", code)
	}
	r := w.result()
	if !r.Final || r.Verdict != "pass" {
		t.Errorf("final %v, verdict %q; want a final pass", r.Final, r.Verdict)
	}
	want := []string{"the tool failed after the verdict was decided: the runner broke"}
	if diff := cmp.Diff(want, r.Warnings); diff != "" {
		t.Errorf("warnings (-want +got):\n%s", diff)
	}
	if n := w.calls.count("delete"); n != 1 {
		t.Errorf("Delete called %d times, want 1", n)
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

func TestResolveIsCalledOnce(t *testing.T) {
	w := newWorld(t)
	// Load keeps a $${…} literal escaped and each Resolve reads it back one level: a literal in
	// the probe's URL stays a literal after the one Resolve of the run, and a second call would
	// fill it with the image's reference.
	w.cfg.Probe.HTTP.URL = "http://orders.orders.svc/$${images.orders.digest}"
	resolved := "http://orders.orders.svc/${images.orders.digest}"
	w.hooks["deployer"] = func() {
		// The deploy driver takes its settings from the configuration it is handed, so Resolve
		// must have run before it is built.
		if w.cfg.Probe.HTTP.URL != resolved {
			t.Errorf("the URL is %q when the deployer is built, want %q", w.cfg.Probe.HTTP.URL, resolved)
		}
	}
	w.hooks["fetch"] = func() {
		// The deploy driver reads the configuration after Resolve, and never before it.
		if w.cfg.Probe.HTTP.URL != resolved {
			t.Errorf("the URL is %q when the deploy driver fetches, want %q", w.cfg.Probe.HTTP.URL, resolved)
		}
	}
	if code := w.run(context.Background()); code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, w.stdout.String())
	}
	r := w.result()
	if len(r.Probes) != 1 {
		t.Fatalf("probes %d, want 1", len(r.Probes))
	}
	if want := "GET " + resolved; r.Probes[0].Target != want {
		t.Errorf("target %q, want %q: Resolve was not called exactly once", r.Probes[0].Target, want)
	}
}

func TestSignalAfterTheVerdictOnEveryCleanUpPath(t *testing.T) {
	rows := []struct {
		name        string
		keep        bool
		deleteErr   error
		wantDelete  int
		wantWarning []string
	}{
		{"the deletion failed", false, errors.New("the engine did not answer"), 1,
			[]string{"the cluster was not deleted: the engine did not answer",
				"interrupted after the verdict was decided"}},
		{"the cluster is kept", true, nil, 0,
			[]string{"interrupted after the verdict was decided"}},
		{"the cluster is left because of the signal", false, nil, 0,
			[]string{"interrupted after the verdict was decided"}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			w := newWorld(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			w.keep = row.keep
			w.cluster.deleteErr = row.deleteErr
			if row.wantDelete > 0 {
				w.hooks["delete"] = cancel // the signal comes while the cluster is deleted
			} else {
				w.hooks["watch-stop"] = cancel // the signal comes once the verdict is decided
			}
			if code := w.run(ctx); code != 0 {
				t.Fatalf("exit %d, want 0", code)
			}
			r := w.result()
			if !r.Final || r.Verdict != "pass" {
				t.Errorf("final %v, verdict %q; want a final pass", r.Final, r.Verdict)
			}
			if n := w.calls.count("delete"); n != row.wantDelete {
				t.Errorf("Delete called %d times, want %d", n, row.wantDelete)
			}
			if diff := cmp.Diff(row.wantWarning, r.Warnings); diff != "" {
				t.Errorf("warnings (-want +got):\n%s", diff)
			}
		})
	}
}

// holdClock is the fake clock except that the channel After gives never fires until the test
// sends on release, so that a test can hold the run inside a wait and see what ends it.
type holdClock struct {
	*fakeClock
	at      chan struct{}
	release chan time.Time
	once    sync.Once
}

// After records that the run has reached the wait and gives it a channel the test controls.
func (c *holdClock) After(d time.Duration) <-chan time.Time {
	c.once.Do(func() { close(c.at) })
	return c.release
}

// cancelOnWrite cancels the run's context the first time anything is written to it: the console
// text is written between the verdict and the clean-up.
type cancelOnWrite struct {
	buf    bytes.Buffer
	cancel context.CancelFunc
	once   sync.Once
}

// Write cancels the run and keeps what is written.
func (w *cancelOnWrite) Write(p []byte) (int, error) {
	w.once.Do(w.cancel)
	return w.buf.Write(p)
}

func TestANoClusterRunWarnsOfASignalAfterTheVerdict(t *testing.T) {
	w := newWorld(t)
	w.loadErr = verdict.Condition{Code: verdict.ConfigRejected, Text: "unknown key nope at line 3"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &cancelOnWrite{cancel: cancel}
	u := w.units()
	code := Run(ctx, Options{ConfigPath: filepath.Join(w.cfg.Dir, "rollproof.yaml"), Out: w.out,
		Stdout: out, Stderr: &w.stderr, Version: "test", Getenv: func(string) string { return "" }}, u)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	r := w.result()
	if diff := cmp.Diff([]string{"interrupted after the verdict was decided"}, r.Warnings); diff != "" {
		t.Errorf("warnings (-want +got):\n%s", diff)
	}
}

func TestSignalEndsTheSteadyWait(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clk := &holdClock{fakeClock: w.clock, at: make(chan struct{}), release: make(chan time.Time, 1)}
	u := w.units()
	u.Clock = clk
	done := make(chan int, 1)
	go func() {
		done <- Run(ctx, Options{ConfigPath: filepath.Join(w.cfg.Dir, "rollproof.yaml"), Out: w.out,
			Stdout: &w.stdout, Stderr: &w.stderr, Version: "test", Getenv: func(string) string { return "" }}, u)
	}()
	select {
	case <-clk.at: // the run is in the steady window's wait
	case <-time.After(2 * time.Second):
		clk.release <- t0
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		t.Fatal("the run never reached the steady window's wait")
	}
	cancel()
	select {
	case code := <-done:
		if code != 2 {
			t.Errorf("exit %d, want 2", code)
		}
	case <-time.After(2 * time.Second):
		clk.release <- t0 // let the run finish, so that no goroutine outlives the test
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		t.Fatal("a signal did not end the steady window's wait: Run was still waiting for limits.steady")
	}
	r := w.result()
	if window, _ := r.windowOf("interrupted"); window != "steady" {
		t.Errorf("interrupted in window %q, want steady", window)
	}
	if diff := cmp.Diff([]string{"not-verified", "not-run", "not-run"}, r.results()); diff != "" {
		t.Errorf("window results (-want +got):\n%s", diff)
	}
}

func TestACutWindowHasAnEnd(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.hooks["transition-deploy"] = func() {
		w.clock.Advance(5 * time.Second)
		cancel()
	}
	w.run(ctx)
	r := w.result()
	if len(r.Windows) < 2 {
		t.Fatalf("windows %d, want at least 2", len(r.Windows))
	}
	if end := r.Windows[1].End; end == nil || !end.Equal(t0.Add(15*time.Second)) {
		t.Errorf("the deploy window a signal cut ends at %v, want %v", end, t0.Add(15*time.Second))
	}
}

func TestAPanicInACommandGoroutine(t *testing.T) {
	w := newWorld(t)
	w.hooks["deploy"] = func() { panic("the deploy driver broke") }
	if code := w.run(context.Background()); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	r := w.result()
	if diff := cmp.Diff([]string{"internal-error"}, r.codes()); diff != "" {
		t.Errorf("reasons (-want +got):\n%s", diff)
	}
	if window, text := r.windowOf("internal-error"); window != "deploy" || text != "the deploy driver broke" {
		t.Errorf("internal-error in window %q with text %q", window, text)
	}
	if n := w.calls.count("delete"); n != 1 {
		t.Errorf("Delete called %d times, want 1", n)
	}
	if n := w.calls.count("rollback"); n != 0 {
		t.Errorf("rollback called %d times, want 0", n)
	}
}

func TestTheEventReaderHandsAPanicToTheMainFlow(t *testing.T) {
	s := &runState{}
	events := make(chan probe.Event, 1)
	r := &reader{events: events, ask: make(chan chan struct{}),
		stop: make(chan struct{}), done: make(chan struct{}),
		add: func(probe.Event) { panic("the timeline broke") }}
	s.reader = r
	go s.read(r)
	events <- probe.Event{}
	select {
	case <-r.done: // the reader handed its panic over and closed its channels
	case <-time.After(2 * time.Second):
		t.Fatal("the reader never handed over its panic: it neither panicked nor returned")
	}
	caught := make(chan any, 1)
	go func() {
		defer func() { caught <- recover() }()
		s.ask()
	}()
	select {
	case p := <-caught:
		if p != "the timeline broke" {
			t.Errorf("the main flow caught %v, want the reader's panic", p)
		}
	case <-time.After(time.Second):
		t.Fatal("the main flow did not learn of the reader's panic")
	}
	s.ask() // the panic is handed over once, not on every request

	t.Run("the panic lands while a request waits", func(t *testing.T) {
		s := &runState{}
		events := make(chan probe.Event, 1)
		r := &reader{events: events, ask: make(chan chan struct{}),
			stop: make(chan struct{}), done: make(chan struct{}),
			add: func(probe.Event) { panic("the timeline broke") },
			// The reader takes the request first, and only then meets the event that panics:
			// it never closes the reply.
			onAsk: func() { events <- probe.Event{} }}
		s.reader = r
		go s.read(r)
		caught := make(chan any, 1)
		go func() {
			defer func() { caught <- recover() }()
			s.ask()
		}()
		select {
		case p := <-caught:
			if p != "the timeline broke" {
				t.Errorf("the main flow caught %v, want the reader's panic", p)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("the request waited for a reply that the reader's panic never sent")
		}
	})
}

func TestNoGapBetweenWindows(t *testing.T) {
	w := newWorld(t)
	// The clock moves on past each wait, so a transition that began at Clock.Now() instead of the
	// previous window's End would show a gap.
	w.clock.jump = 3 * time.Second
	if code := w.run(context.Background()); code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, w.stdout.String())
	}
	marks := w.windowMarks()
	if len(marks) != 3 {
		t.Fatalf("marks %d, want 3", len(marks))
	}
	if marks[0].End == nil || marks[1].Start == nil || !marks[1].Start.Equal(*marks[0].End) {
		t.Errorf("the deploy window starts at %v, want the steady window's end %v", marks[1].Start, marks[0].End)
	}
	if marks[1].End == nil || marks[2].Start == nil || !marks[2].Start.Equal(*marks[1].End) {
		t.Errorf("the rollback window starts at %v, want the deploy window's end %v", marks[2].Start, marks[1].End)
	}
}

// windowMarks reads the windows' boundaries from the evidence folder.
func (w *world) windowMarks() []mark {
	w.t.Helper()
	b, err := os.ReadFile(filepath.Join(w.out, "evidence", "windows.json"))
	if err != nil {
		w.t.Fatalf("read windows.json: %v", err)
	}
	var marks []mark
	if err := json.Unmarshal(b, &marks); err != nil {
		w.t.Fatalf("decode windows.json: %v", err)
	}
	return marks
}

func TestTheRecordKeepsThePodsAtTheFirstFailure(t *testing.T) {
	w := newWorld(t)
	w.watcher.stopping = []watch.PodNote{{Name: "orders-6d5f9c-x2k4q", Workload: orders, Old: true,
		For: 300 * time.Millisecond}}
	w.watcher.starting = []watch.PodNote{{Name: "orders-7f8e9d-k3m2p", Workload: orders,
		For: 800 * time.Millisecond}}
	w.probe.events = append(w.probe.events, failureAt(20*time.Second, 20*time.Second))
	if code := w.run(context.Background()); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	detail := strings.Join(w.result().detailOf("probe-failures"), "\n")
	if !strings.Contains(detail, "orders-6d5f9c-x2k4q") || !strings.Contains(detail, "told to stop 0.3 s before") {
		t.Errorf("the stopping pod is missing from the detail:\n%s", detail)
	}
	if !strings.Contains(detail, "orders-7f8e9d-k3m2p") || !strings.Contains(detail, "created 0.8 s before") {
		t.Errorf("the starting pod is missing from the detail:\n%s", detail)
	}
	if diff := cmp.Diff([]time.Time{t0.Add(20 * time.Second)}, w.watcher.podsAt); diff != "" {
		t.Errorf("PodsAt was asked for (-want +got):\n%s", diff)
	}
}

func TestAFailedSteadyWindowRunsNoTransition(t *testing.T) {
	w := newWorld(t)
	w.probe.events = append(w.probe.events, failureAt(5*time.Second, 5*time.Second))
	if code := w.run(context.Background()); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	r := w.result()
	if diff := cmp.Diff([]string{"steady-not-clean"}, r.codes()); diff != "" {
		t.Errorf("reasons (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"not-verified", "not-run", "not-run"}, r.results()); diff != "" {
		t.Errorf("window results (-want +got):\n%s", diff)
	}
	for _, name := range []string{"deploy", "rollback"} {
		if n := w.calls.count(name); n != 0 {
			t.Errorf("%s called %d times, want 0", name, n)
		}
	}
}

// stubFile is an evidence file whose writes or close fail as the test scripts.
type stubFile struct {
	writeErr, closeErr error
}

// Write fails with writeErr, or keeps what is written.
func (f stubFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

// Close fails with closeErr.
func (f stubFile) Close() error { return f.closeErr }

func TestTheEvidenceWrapperWarns(t *testing.T) {
	rows := []struct {
		name  string
		file  stubFile
		want  string
		write int
	}{
		{"a write fails", stubFile{writeErr: errors.New("no room")},
			"the evidence deploy could not be written: no room", 2},
		{"a close fails", stubFile{closeErr: errors.New("no room")},
			"the evidence deploy could not be written: no room", 0},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			var got []string
			e := &evidence{
				files: func(string) (io.WriteCloser, error) { return row.file, nil },
				warn:  func(s string) { got = append(got, s) },
			}
			f, err := e.funcOf("deploy")
			if err != nil || f == nil {
				t.Fatalf("funcOf gave %v, %v", f, err)
			}
			for range row.write {
				_, _ = f.Write([]byte("x"))
			}
			_ = f.Close()
			_ = f.Close()
			if diff := cmp.Diff([]string{row.want}, got); diff != "" {
				t.Errorf("warnings (-want +got):\n%s", diff)
			}
		})
	}
	t.Run("the file cannot be opened", func(t *testing.T) {
		var got []string
		e := &evidence{
			files: func(string) (io.WriteCloser, error) { return nil, errors.New("the folder is gone") },
			warn:  func(s string) { got = append(got, s) },
		}
		f, err := e.funcOf("deploy")
		if err != nil || f == nil {
			t.Fatalf("funcOf gave %v, %v; want a writer that drops what is written", f, err)
		}
		if _, err := f.Write([]byte("x")); err != nil {
			t.Errorf("writing to the dropped evidence gave %v", err)
		}
		if diff := cmp.Diff([]string{"the evidence deploy was not opened: the folder is gone"}, got); diff != "" {
			t.Errorf("warnings (-want +got):\n%s", diff)
		}
	})
}

func TestARefusedFinalKeepsTheVerdictsClass(t *testing.T) {
	rows := []struct {
		name   string
		failed bool
		want   int
	}{
		{"a window had failed", true, 1},
		{"every window passed", false, 2},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			w := newWorld(t)
			if row.failed {
				w.probe.events = append(w.probe.events, failureAt(20*time.Second, 20*time.Second))
			}
			w.hooks["probe-stop"] = func() {
				// Removing the folder after the first result.json refuses the final write for
				// root as well, which a read-only folder does not.
				if err := os.RemoveAll(w.out); err != nil {
					t.Fatal(err)
				}
			}
			if code := w.run(context.Background()); code != row.want {
				t.Errorf("exit %d, want %d", code, row.want)
			}
			if !strings.HasPrefix(w.stderr.String(), "rollproof: cannot write results: ") {
				t.Errorf("stderr is %q", w.stderr.String())
			}
			if w.stdout.Len() != 0 {
				t.Errorf("stdout is %q, want no console: the final file was not written", w.stdout.String())
			}
			if n := w.calls.count("delete"); n != 1 {
				t.Errorf("Delete called %d times, want 1", n)
			}
			if n := w.calls.count("end-commands"); n != 1 {
				t.Errorf("EndCommands called %d times, want 1", n)
			}
		})
	}
}
