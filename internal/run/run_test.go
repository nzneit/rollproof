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
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"rollproof/internal/config"
	"rollproof/internal/deploy"
	"rollproof/internal/probe"
	"rollproof/internal/report"
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
		Start  *time.Time `json:"start"`
		End    *time.Time `json:"end"`
		Checks int        `json:"checks"`
	} `json:"windows"`
	Install *struct {
		Ready            time.Time  `json:"ready"`
		FirstObservation *time.Time `json:"first_observation"`
		Settled          *time.Time `json:"settled"`
		Checks           int        `json:"checks"`
		Failed           int        `json:"failed"`
		LastFailure      *time.Time `json:"last_failure"`
	} `json:"install"`
	Warnings []string `json:"warnings"`
	Probes   []struct {
		Target             string   `json:"target"`
		Lines              int      `json:"lines"`
		JSONLinesWithoutOK int      `json:"json_lines_without_ok"`
		StoppedWhenAsked   *bool    `json:"stopped_when_asked"` // nil for null
		Notes              []string `json:"notes"`
	} `json:"probes"`
	Transitions []struct {
		Name    string  `json:"name"`
		Command *string `json:"command"` // nil for null
		Restart *string `json:"restart"` // nil for null
	} `json:"transitions"`
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
		// The probe is stopped on a goroutine of its own: the hook reports with t.Error.
		r, ok := w.resultFromHook()
		if ok && r.Final {
			t.Error("result.json was final before the probe was stopped")
		}
	}
	if code := w.run(context.Background()); code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, w.stdout.String())
	}
	want := []string{"load", "check", "create", "store", "deployer", "fetch", "setup", "watcher",
		"install", "install-ready", "target", "probe", "start", "install-settled", "deploy", "transition-deploy",
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
	setUp := []string{"load", "check", "create", "store", "deployer", "fetch", "setup"}
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
		}, "chart-fetch-failed", []string{"load", "check", "create", "store", "deployer", "fetch", "end-commands", "delete"}},
		{"setup fails", func(w *world) {
			w.cluster.setup = deploy.Result{Name: "setup", Ending: deploy.ExitedNonZero, ExitCode: 1}
		}, "setup-failed", then("end-commands", "delete")},
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
		{"probe options", func(w *world) {
			w.cluster.probeErr = verdict.Condition{Code: verdict.ConfigRejected, Text: "probe.custom.env[0]: not a form a pod's env takes"}
		}, "config-rejected", then("watcher", "install", "install-ready", "target", "probe", "end-commands", "watch-stop", "delete")},
		{"probe not started", func(w *world) {
			w.probe.startErr = verdict.Condition{Code: verdict.ProbeNotStarted, Text: "no observation within limits.timeout"}
		}, "probe-not-started", then("watcher", "install", "install-ready", "target", "probe", "start", "probe-stop", "end-commands", "watch-stop", "delete")},
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

// A setup command stopped by a signal is the signal's, not a failed command: the
// run reports interrupted and nothing after the set-up starts.
func TestSetupStoppedBySignal(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.cluster.setup = deploy.Result{Name: "setup", Ending: deploy.StoppedBySignal, ExitCode: -1}
	w.hooks["setup"] = cancel
	if code := w.run(ctx); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	r := w.result()
	if diff := cmp.Diff([]string{"interrupted"}, r.codes()); diff != "" {
		t.Errorf("reasons (-want +got):\n%s", diff)
	}
	calls := w.calls.list()
	if slices.Contains(calls, "watcher") || slices.Contains(calls, "install") {
		t.Errorf("the run went on after the signal: calls %q", calls)
	}
}

func TestNotBuilt(t *testing.T) {
	w := newWorld(t)
	w.cfg.Registries = []config.Registry{{Host: "registry.example"}}
	if code := w.run(context.Background()); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if diff := cmp.Diff([]string{"load"}, w.calls.list()); diff != "" {
		t.Errorf("calls (-want +got):\n%s", diff)
	}
	if window, text := w.result().windowOf("config-rejected"); window != "" || text != "not built yet: registries" {
		t.Errorf("config-rejected in window %q with text %q", window, text)
	}
}

// A custom probe has no target check, and watch is told of no Service: whether a workload is
// probed is not known (decision 8). result.json names the probe by its image and gives its env's
// names and the counts of its command and args, never their values (section 8, "Secrets").
func TestCustomProbeHasNoTargetCheck(t *testing.T) {
	w := newWorld(t)
	image := "registry.example/tools/busybox@sha256:" + strings.Repeat("0", 64)
	w.cfg.Probe = config.Probe{Custom: &config.Custom{Image: image, Command: []string{"sh", "-c", "echo S3CRET-VALUE"},
		Env: []config.EnvVar{{Name: "TOKEN", ValueFrom: map[string]any{"secretKeyRef": map[string]any{"name": "probe-token", "key": "token"}}}}}}
	for _, name := range []verdict.WindowName{verdict.Deploy, verdict.Rollback} {
		tr := w.watcher.transitions[name]
		tr.Workloads = slices.Clone(tr.Workloads)
		tr.Workloads[0].Probed = "not known"
		w.watcher.transitions[name] = tr
	}
	if code := w.run(context.Background()); code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, w.stdout.String())
	}
	if slices.Contains(w.calls.list(), "target") {
		t.Errorf("the target was checked for a custom probe: calls %q", w.calls.list())
	}
	if w.watcher.service != nil {
		t.Errorf("UseService was given %v, want nil", w.watcher.service)
	}
	if calls := w.watcher.useCalls; len(calls) == 0 || calls[len(calls)-1] != "install-ready" {
		t.Errorf("UseService came after %q, want install-ready last", calls)
	}
	b, err := os.ReadFile(filepath.Join(w.out, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"S3CRET-VALUE", "probe-token"} {
		if strings.Contains(string(b), value) {
			t.Errorf("result.json holds %q", value)
		}
	}
	var file map[string]any
	if err := json.Unmarshal(b, &file); err != nil {
		t.Fatal(err)
	}
	p := file["probes"].([]any)[0].(map[string]any)
	want := map[string]any{"kind": "custom", "target": image, "env_names": []any{"TOKEN"},
		"command_count": 3.0, "args_count": 0.0, "certificate_checked": nil}
	for k, v := range want {
		if diff := cmp.Diff(v, p[k]); diff != "" {
			t.Errorf("probes[0].%s (-want +got):\n%s", k, diff)
		}
	}
	if _, ok := p["certificate_checked"]; !ok {
		t.Error("probes[0] has no certificate_checked")
	}
	tr := file["transitions"].([]any)[0].(map[string]any)
	probed := tr["workloads"].([]any)[0].(map[string]any)["probed"]
	if probed != "not known" {
		t.Errorf("transitions[0].workloads[0].probed is %v, want not known", probed)
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
	w.probe.events = passes(-15, 320)
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
	w.deployer.results["deploy"] = deploy.Result{Ending: deploy.StoppedBySignal, ExitCode: -1}
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
	// The command ended before the interrupted check, so the record keeps its ending.
	if len(r.Transitions) != 1 || r.Transitions[0].Name != "deploy" || r.Transitions[0].Command == nil ||
		*r.Transitions[0].Command != "stopped-by-signal" {
		t.Errorf("transitions %+v, want deploy with command stopped-by-signal", r.Transitions)
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
				t.Errorf("exit %d, want %d", code, row.exit)
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
			w.probeWasStopped()
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
	if len(marks) != 4 {
		t.Fatalf("%d window marks, want 4", len(marks))
	}
	var names []string
	for _, m := range marks {
		names = append(names, m.Name)
	}
	if diff := cmp.Diff([]string{"install", "steady", "deploy", "rollback"}, names); diff != "" {
		t.Errorf("names (-want +got):\n%s", diff)
	}
	if marks[1].Finished != nil {
		t.Errorf("the steady window's finished is %v, want null", marks[1].Finished)
	}
	if marks[2].Finished == nil || !marks[2].Finished.Equal(t0.Add(25*time.Second)) {
		t.Errorf("the deploy window's finished is %v, want %v", marks[2].Finished, t0.Add(25*time.Second))
	}
	if marks[3].End == nil || !marks[3].End.Equal(t0.Add(70*time.Second)) {
		t.Errorf("the rollback window's end is %v, want %v", marks[3].End, t0.Add(70*time.Second))
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
	w.probeWasStopped()
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
	if len(marks) != 4 || marks[0].Name != "install" {
		t.Fatalf("marks %+v, want four, install first", marks)
	}
	if marks[1].End == nil || marks[2].Start == nil || !marks[2].Start.Equal(*marks[1].End) {
		t.Errorf("the deploy window starts at %v, want the steady window's end %v", marks[2].Start, marks[1].End)
	}
	if marks[2].End == nil || marks[3].Start == nil || !marks[3].Start.Equal(*marks[2].End) {
		t.Errorf("the rollback window starts at %v, want the deploy window's end %v", marks[3].Start, marks[2].End)
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
				// root as well, which a read-only folder does not. The probe is stopped on a
				// goroutine of its own: the hook reports with t.Error.
				if err := os.RemoveAll(w.out); err != nil {
					t.Error(err)
					return
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

// A pass that ends with a window that never ran or never ended is not verified: Decide cannot tell
// the end of a window from the end of the run (carry-in W1), so the run checks it itself.
func TestAPassWithAWindowOpenIsNotVerified(t *testing.T) {
	s := &runState{}
	for _, tc := range []struct {
		name string
		tl   verdict.Timeline
		text string
	}{
		{"a window never ran", verdict.Timeline{
			Windows: []verdict.Window{
				{Name: verdict.Steady, Start: t0, End: t0.Add(10 * time.Second)},
				{Name: verdict.Deploy},
				{Name: verdict.Rollback},
			},
			Silence: time.Hour,
			Now:     t0.Add(10 * time.Second),
		}, "the run ended although the deploy window never ran"},
		{"the last window never ended", verdict.Timeline{
			Windows: []verdict.Window{
				{Name: verdict.Steady, Start: t0, End: t0.Add(10 * time.Second)},
				{Name: verdict.Deploy, Start: t0.Add(10 * time.Second)},
			},
			Silence: time.Hour,
			Now:     t0.Add(20 * time.Second),
		}, "the run ended while the deploy window was still open"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := s.passedWithAWindowOpen(tc.tl, verdict.Decision{Verdict: verdict.Pass, GoOn: true})
			if d.Verdict != verdict.NotVerified || d.GoOn {
				t.Errorf("got %s, GoOn %v; want not-verified and GoOn false", d.Verdict, d.GoOn)
			}
			var texts []string
			for _, r := range d.Reasons {
				if r.Code != verdict.InternalError {
					t.Errorf("reason %s, want internal-error only", r.Code)
				}
				texts = append(texts, r.Text)
			}
			if !slices.Contains(texts, tc.text) {
				t.Errorf("reasons %q do not hold %q", texts, tc.text)
			}
		})
	}
}

// panicWriter is a results writer whose final write panics.
type panicWriter struct{}

func (*panicWriter) Final(report.Run) error { panic("the results writer broke") }

func (*panicWriter) Evidence(string) (io.WriteCloser, error) { return nopWriteCloser{io.Discard}, nil }

func (*panicWriter) Dir() string { return "out" }

// nopWriteCloser is an io.Writer that can be closed.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// A panic before the final result.json was written is the refused write: the first file says the
// run did not finish, so exit 1 only for a decided fail and 2 for any other verdict.
func TestAPanicInTheFinalWriteIsARefusedWrite(t *testing.T) {
	for _, tc := range []struct {
		name    string
		verdict verdict.Verdict
		want    int
	}{
		{"a decided fail", verdict.Fail, 1},
		{"a pass", verdict.Pass, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			s := &runState{
				o:        Options{Stdout: &stdout, Stderr: &stderr},
				parent:   context.Background(),
				w:        &panicWriter{},
				warnings: &warnings{},
				decision: verdict.Decision{Verdict: tc.verdict},
			}
			if code := s.finish(); code != tc.want {
				t.Errorf("exit %d, want %d", code, tc.want)
			}
			if !strings.HasPrefix(stderr.String(), "rollproof: cannot write results: ") {
				t.Errorf("stderr is %q", stderr.String())
			}
			if !strings.Contains(stderr.String(), "the results writer broke") {
				t.Errorf("stderr does not hold the panic's text: %q", stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout is %q, want no console: the final file was not written", stdout.String())
			}
		})
	}
}

// Stop's error means that the probe's pod may still be running: the run keeps it as a warning.
func TestTheProbesStopErrorIsAWarning(t *testing.T) {
	w := newWorld(t)
	w.probe.stopErr = errors.New("the pod delete was refused")
	if code := w.run(context.Background()); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	r := w.result()
	if r.Verdict != "pass" {
		t.Errorf("verdict %q, want pass", r.Verdict)
	}
	if len(r.Warnings) != 1 {
		t.Fatalf("warnings %q, want one", r.Warnings)
	}
	for _, part := range []string{"the probe could not be stopped", "the pod delete was refused", "its pod may still be there"} {
		if !strings.Contains(r.Warnings[0], part) {
			t.Errorf("the warning %q does not hold %q", r.Warnings[0], part)
		}
	}
	if !strings.Contains(w.stdout.String(), "the pod delete was refused") {
		t.Errorf("the console does not print the warning:\n%s", w.stdout.String())
	}
	// The stop failed without a signal: the tool did not finish asking, and does not judge the probe.
	w.probeStopNotJudged(noteStopFailed)
}

// The notes of a probe entry whose stop the tool did not judge, exact (section 10).
const (
	noteNotAsked    = "the run was interrupted, so the tool did not ask the probe to stop"
	noteCutBySignal = "the run was interrupted while the tool waited for the probe to stop"
	noteStopFailed  = "the tool failed while it stopped the probe"
)

// probeStopNotJudged fails the test unless result.json's one probe entry has stopped_when_asked
// null, the counts that the probe's Counts gives, and note as its last note, and unless the
// console printed note and no "did not stop when asked".
func (w *world) probeStopNotJudged(note string) {
	w.t.Helper()
	r := w.result()
	if len(r.Probes) != 1 {
		w.t.Fatalf("probes %+v, want one", r.Probes)
	}
	p := r.Probes[0]
	if p.StoppedWhenAsked != nil {
		w.t.Errorf("stopped_when_asked is %v, want null", *p.StoppedWhenAsked)
	}
	if p.Lines != w.probe.linesRead || p.JSONLinesWithoutOK != w.probe.jsonTextRead {
		w.t.Errorf("lines %d and json_lines_without_ok %d, want the %d and %d that Counts gives",
			p.Lines, p.JSONLinesWithoutOK, w.probe.linesRead, w.probe.jsonTextRead)
	}
	if len(p.Notes) == 0 || p.Notes[len(p.Notes)-1] != note {
		w.t.Errorf("notes %q, want %q last", p.Notes, note)
	}
	if out := w.stdout.String(); !strings.Contains(out, note) || strings.Contains(out, "did not stop when asked") {
		w.t.Errorf("the console does not print the note %q, or says that the probe did not stop when asked:\n%s", note, out)
	}
}

func TestASignalLeavesTheProbeUnasked(t *testing.T) {
	w := newWorld(t)
	w.probe.linesRead, w.probe.jsonTextRead = 7, 1
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	u := w.units()
	u.Clock = &lateClock{fakeClock: w.clock, before: cancel} // the signal comes in the steady window
	code := Run(ctx, Options{ConfigPath: filepath.Join(w.cfg.Dir, "rollproof.yaml"), Out: w.out,
		Stdout: &w.stdout, Stderr: &w.stderr, Version: "test", Getenv: func(string) string { return "" }}, u)
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if window, _ := w.result().windowOf("interrupted"); window != "steady" {
		t.Errorf("interrupted in window %q, want steady", window)
	}
	if n := w.calls.count("probe-stop"); n != 0 {
		t.Errorf("the probe's Stop was called %d times, want 0", n)
	}
	if n := w.calls.count("delete"); n != 0 {
		t.Errorf("Delete called %d times, want 0: the cluster stays after a signal", n)
	}
	w.probeStopNotJudged(noteNotAsked)
}

func TestASignalDuringTheStop(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.probe.facts = probe.Facts{Lines: 3, StoppedWhenAsked: false}
	w.probe.linesRead, w.probe.jsonTextRead = 7, 1
	w.hooks["probe-stop"] = func() {
		cancel()
		w.probe.stopErr = context.Canceled // Stop gives up when its context ends
	}
	if code := w.run(ctx); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	w.probeStopNotJudged(noteCutBySignal)
}

func TestAStopThatPanicsIsTheToolsFault(t *testing.T) {
	w := newWorld(t)
	w.probe.linesRead, w.probe.jsonTextRead = 7, 1
	w.hooks["probe-stop"] = func() { panic("the probe unit broke") }
	if code := w.run(context.Background()); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if codes := w.result().codes(); !slices.Contains(codes, "internal-error") {
		t.Errorf("reasons %q, want internal-error among them", codes)
	}
	w.probeStopNotJudged(noteStopFailed)
}

func TestASignalDuringTheProbesStart(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.probe.startWaits = true
	w.probe.linesRead, w.probe.jsonTextRead = 7, 1
	w.hooks["start"] = cancel
	if code := w.run(ctx); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if diff := cmp.Diff([]string{"interrupted"}, w.result().codes()); diff != "" {
		t.Errorf("reasons (-want +got):\n%s", diff)
	}
	if n := w.calls.count("probe-stop"); n != 0 {
		t.Errorf("the probe's Stop was called %d times, want 0", n)
	}
	w.probeStopNotJudged(noteNotAsked)
}

// A Start that failed other than by its context had begun its own stop, also when a signal came
// meanwhile: Stop is asked for what that stop left. When the signal cut that stop, the entry says
// so; when the stop had completed, its Facts are kept.
func TestASignalDuringAFailedStartsStop(t *testing.T) {
	for _, c := range []struct {
		name    string
		stopErr error
	}{
		{"the signal cut the stop", context.Canceled},
		{"the stop had completed", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			w.hooks["start"] = cancel
			w.probe.startErr = verdict.Condition{Code: verdict.ProbeNotStarted, Text: "no observation within limits.timeout"}
			w.probe.stopErr = c.stopErr
			w.probe.facts = probe.Facts{Lines: 3, JSONTextLines: 1, StoppedWhenAsked: true}
			w.probe.linesRead, w.probe.jsonTextRead = 7, 2
			if code := w.run(ctx); code != 2 {
				t.Errorf("exit %d, want 2", code)
			}
			if diff := cmp.Diff([]string{"interrupted"}, w.result().codes()); diff != "" {
				t.Errorf("reasons (-want +got):\n%s", diff)
			}
			if n := w.calls.count("probe-stop"); n != 1 {
				t.Errorf("the probe's Stop was called %d times, want 1", n)
			}
			if c.stopErr != nil {
				w.probeStopNotJudged(noteCutBySignal)
				return
			}
			r := w.result()
			if len(r.Probes) != 1 || r.Probes[0].StoppedWhenAsked == nil || !*r.Probes[0].StoppedWhenAsked ||
				r.Probes[0].Lines != 3 || r.Probes[0].JSONLinesWithoutOK != 1 {
				t.Errorf("probes %+v, want one with the Facts of the stop: stopped when asked, 3 lines, 1 JSON text", r.Probes)
			}
		})
	}
}

// Section 7, "When the tool fails": a command that still runs is stopped before the probe.
func TestCommandsEndBeforeTheProbeStops(t *testing.T) {
	w := newWorld(t)
	hold := make(chan struct{})
	defer close(hold)
	w.deployer.block = map[string]chan struct{}{"deploy": hold}
	w.deployer.orCtx = map[string]bool{"deploy": true}
	started := make(chan struct{})
	w.hooks["deploy"] = func() { close(started) } // the command's goroutine is inside the command
	w.watcher.before = func(name verdict.WindowName) {
		if name == verdict.Deploy {
			<-started // the deploy command waits on its block or its context
			panic("the watcher broke")
		}
	}
	w.hooks["probe-stop"] = func() {
		// The probe is stopped on a goroutine of its own: the hook reports with t.Error.
		if !w.deployer.cancelled("deploy") {
			t.Error("the probe was asked to stop while the deploy command's context had not ended")
		}
	}
	if code := w.run(context.Background()); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	r := w.result()
	if len(r.Probes) != 1 || r.Probes[0].StoppedWhenAsked == nil || !*r.Probes[0].StoppedWhenAsked {
		t.Errorf("probes %+v, want one that stopped when asked", r.Probes)
	}
}

// The probe's channel is taken before the stop begins: a panic there leaves no stop running behind
// it, and the stop is the tool's failure.
func TestEventsThatPanicsInTheStop(t *testing.T) {
	w := newWorld(t)
	w.probe.eventsPanic = true
	w.probe.linesRead, w.probe.jsonTextRead = 7, 1
	if code := w.run(context.Background()); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if codes := w.result().codes(); !slices.Contains(codes, "internal-error") {
		t.Errorf("reasons %q, want internal-error among them", codes)
	}
	w.probeStopNotJudged(noteStopFailed)
}

// A decision that panics in the stop after a failure of the tool, and again in its handler, is a
// warning: the verdict was decided when the failure was placed.
func TestADecisionThatPanicsTwice(t *testing.T) {
	w := newWorld(t)
	armed, calls := false, 0
	w.hooks["transition-deploy"] = func() {
		armed = true
		panic("the watcher broke")
	}
	defer func(h func()) { decideHook = h }(decideHook)
	decideHook = func() {
		if !armed {
			return
		}
		calls++
		if calls == 2 || calls == 3 { // the first is toolFailed's, which returns
			panic("the verdict broke")
		}
	}
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("Run panicked: %v", p)
		}
	}()
	code := w.run(context.Background())
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	r := w.result()
	found := false
	for _, x := range r.Warnings {
		found = found || strings.HasPrefix(x, "the tool failed after the verdict was decided: ")
	}
	if !found {
		t.Errorf("warnings %q, want one that begins %q", r.Warnings, "the tool failed after the verdict was decided: ")
	}
}

// Rule 3: a failure of the tool and a signal in one window are both reasons of that window, and the
// signal is no warning after the verdict.
func TestAFailureAndASignalInOneWindow(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.hooks["transition-deploy"] = func() {
		cancel()
		panic("the watcher broke")
	}
	if code := w.run(ctx); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	r := w.result()
	var inDeploy []string
	for _, x := range r.Reasons {
		if x.Window == "deploy" {
			inDeploy = append(inDeploy, x.Code)
		}
	}
	if !slices.Contains(inDeploy, "internal-error") || !slices.Contains(inDeploy, "interrupted") {
		t.Errorf("the deploy window's reasons are %q, want internal-error and interrupted", inDeploy)
	}
	if slices.Contains(r.Warnings, "interrupted after the verdict was decided") {
		t.Errorf("warnings %q: the signal was reported after the verdict too", r.Warnings)
	}
}

// count is how many of the reasons have the code.
func (r resultFile) count(code string) int {
	n := 0
	for _, x := range r.Reasons {
		if x.Code == code {
			n++
		}
	}
	return n
}

// A signal that comes once the stop after a failure of the tool has begun, and before a panic in
// that stop, is placed before the handler's decision, as the stop's own path places one.
func TestASignalBeforeAPanicInTheStopAfterAFailure(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.hooks["install-settled"] = func() { panic("the watch unit broke") }
	w.probe.eventsPanic = true
	w.probe.linesRead, w.probe.jsonTextRead = 7, 1
	w.hooks["events"] = cancel // stopAfterFailure's guard has passed; Events then panics
	if code := w.run(ctx); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	r := w.result()
	if n := r.count("interrupted"); n != 1 {
		t.Errorf("%d interrupted reasons, want 1; reasons %q", n, r.codes())
	}
	if window, _ := r.windowOf("interrupted"); window != "install" {
		t.Errorf("interrupted in window %q, want install, the last that began", window)
	}
	var faults []string
	for _, x := range r.Reasons {
		if x.Code == "internal-error" {
			faults = append(faults, x.Text)
		}
	}
	if diff := cmp.Diff([]string{"the watch unit broke", "the probe's events broke"}, faults); diff != "" {
		t.Errorf("internal-error texts (-want +got):\n%s", diff)
	}
	if slices.Contains(r.Warnings, "interrupted after the verdict was decided") {
		t.Errorf("warnings %q: the signal was reported after the verdict too", r.Warnings)
	}
	w.probeStopNotJudged(noteStopFailed)
}

// The decision that places a signal beside a failure of the tool panics: the panic is a warning,
// the run goes on to its end and writes its final result, and the verdict kept is the one decided
// at the failure. That verdict does not hold the signal, which is then reported as one after it.
func TestADecisionAfterASignalThatPanics(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	armed, calls := false, 0
	w.hooks["transition-deploy"] = func() {
		armed = true
		cancel()
		panic("the watcher broke")
	}
	defer func(h func()) { decideHook = h }(decideHook)
	decideHook = func() {
		if !armed {
			return
		}
		calls++
		if calls == 2 { // the first is toolFailed's, which returns; the second places the signal
			panic("the verdict broke")
		}
	}
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("Run panicked: %v", p)
		}
	}()
	if code := w.run(ctx); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	r := w.result()
	if !r.Final || r.Verdict != "not-verified" {
		t.Errorf("final %v, verdict %q; want a final not-verified", r.Final, r.Verdict)
	}
	if window, text := r.windowOf("internal-error"); window != "deploy" || text != "the watcher broke" {
		t.Errorf("internal-error in window %q with text %q", window, text)
	}
	want := []string{"the tool failed after the verdict was decided: the verdict broke",
		"interrupted after the verdict was decided"}
	if diff := cmp.Diff(want, r.Warnings); diff != "" {
		t.Errorf("warnings (-want +got):\n%s", diff)
	}
	if n := w.calls.count("end-commands"); n != 1 {
		t.Errorf("EndCommands called %d times, want 1: the run did not go on to its end", n)
	}
}

// The decision that places a signal beside a failure of the tool panics after Decide, while it
// keeps the pods of a first failure that the probe's last lines brought: that decision keeps
// nothing, so the verdict kept is the one decided at the failure, and the signal is reported once,
// as one after the verdict.
func TestADecisionThatPanicsWhileItKeepsThePods(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var armed atomic.Bool
	w.hooks["transition-deploy"] = func() {
		w.clock.Advance(2 * time.Second)
		at := w.clock.Now().Add(-time.Second).Sub(t0)
		w.probe.last = []probe.Event{failureAt(at, at)} // a failed check in the deploy window
		panic("the watcher broke")
	}
	w.hooks["probe-stop"] = func() {
		armed.Store(true)
		cancel()
	}
	w.hooks["pods-at"] = func() {
		if armed.Load() {
			panic("the watch unit broke")
		}
	}
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("Run panicked: %v", p)
		}
	}()
	if code := w.run(ctx); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	r := w.result()
	if n := r.count("interrupted"); n != 0 {
		t.Errorf("%d interrupted reasons, want none: the decision that placed the signal kept nothing; reasons %q", n, r.codes())
	}
	if n := w.calls.count("probe-stop"); n != 1 {
		t.Errorf("the probe's Stop was called %d times, want 1", n)
	}
	want := []string{"the tool failed after the verdict was decided: the watch unit broke",
		"interrupted after the verdict was decided"}
	if diff := cmp.Diff(want, r.Warnings); diff != "" {
		t.Errorf("warnings (-want +got):\n%s", diff)
	}
}

// Counts is read once, by the first record: a rewrite of result.json for a later warning gives
// the same counts, also when the probe's counts have moved on since.
func TestTheProbesCountsAreReadOnce(t *testing.T) {
	w := newWorld(t)
	w.probe.stopErr = errors.New("the pod delete was refused") // the stop gives no Facts to use
	w.probe.linesRead, w.probe.jsonTextRead = 7, 1
	w.cluster.deleteErr = errors.New("the engine did not answer") // a warning after the first write
	w.hooks["delete"] = func() {
		if p := w.result().Probes; len(p) != 1 || p[0].Lines != 7 || p[0].JSONLinesWithoutOK != 1 {
			t.Errorf("the first final result.json has probes %+v, want 7 lines and 1", p)
		}
		w.probe.linesRead, w.probe.jsonTextRead = 99, 9
	}
	if code := w.run(context.Background()); code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
	r := w.result()
	if !slices.Contains(r.Warnings, "the cluster was not deleted: the engine did not answer") {
		t.Fatalf("warnings %q: result.json was not written again", r.Warnings)
	}
	if p := r.Probes; len(p) != 1 || p[0].Lines != 7 || p[0].JSONLinesWithoutOK != 1 {
		t.Errorf("the rewritten result.json has probes %+v, want the first counts, 7 lines and 1", p)
	}
}

// A signal already placed as a reason is not placed again when a failure of the tool follows it.
func TestASignalPlacedBeforeAToolFailureIsPlacedOnce(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func(h func(probe.Event)) { eventHook = h }(eventHook)
	eventHook = func(probe.Event) {
		if ctx.Err() != nil {
			panic("the timeline broke") // the reader fails on the first line after the signal
		}
	}
	u := w.units()
	u.Clock = &lateClock{fakeClock: w.clock, before: func() {
		cancel() // the signal comes in the steady window
		w.probe.send(passes(1, 1)[0])
	}}
	code := Run(ctx, Options{ConfigPath: filepath.Join(w.cfg.Dir, "rollproof.yaml"), Out: w.out,
		Stdout: &w.stdout, Stderr: &w.stderr, Version: "test", Getenv: func(string) string { return "" }}, u)
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	r := w.result()
	if n := r.count("interrupted"); n != 1 {
		t.Errorf("%d interrupted reasons, want 1; reasons %q", n, r.codes())
	}
	if window, text := r.windowOf("internal-error"); window != "steady" || text != "the timeline broke" {
		t.Errorf("internal-error in window %q with text %q", window, text)
	}
}

// The run reads the probe's events from before the probe starts (section 5). Start sends 5,000
// observations and the scripted ones on a channel with no buffer, so it returns only while a
// reader takes them.
func TestTheReaderStartsWithTheProbe(t *testing.T) {
	w := newWorld(t)
	w.probe.unbuffered = true
	first := w.clock.Now()
	var early []probe.Event
	for i := range 5000 { // one check every 2 ms for the probe's first 10 s
		at := first.Add(time.Duration(i) * 2 * time.Millisecond)
		early = append(early, probe.Event{Obs: &verdict.Observation{Written: at, T: at, OK: true, N: 1, Ms: 30, HasMs: true, Lag: 2, HasLag: true}})
	}
	w.probe.events = append(early, w.probe.events...)
	done := make(chan int, 1)
	go func() { done <- w.run(context.Background()) }()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d, want 0\n%s", code, w.stdout.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Start is waiting for a reader of the probe's events: the run must read them from the moment the probe starts")
	}
	checks := 0
	for _, x := range w.result().Windows {
		checks += x.Checks
	}
	if checks != 14000 { // 200 a second for the 70 s of the three windows; the 5,000 early ones fall in the install window
		t.Errorf("the windows hold %d checks, want 14000: none may be lost", checks)
	}
	if in := w.result().Install; in == nil || in.Checks != 8000 {
		t.Errorf("install %+v, want 8000 checks: the 5000 early ones and the 3000 of the install window", in)
	}
}

// result.json's install at each ending of a run (section 12).
func TestTheInstallRecordAtEachEnding(t *testing.T) {
	first, ready := t0.Add(-15*time.Second), t0.Add(-21*time.Second)
	t.Run("before the install was ready", func(t *testing.T) {
		w := newWorld(t)
		w.watcher.installErr = verdict.Condition{Code: verdict.InstallFailed, Text: "the install was not ready within limits.timeout"}
		w.run(context.Background())
		if r := w.result(); r.Install != nil || len(r.Probes) != 0 {
			t.Errorf("install %+v and %d probes; want neither", r.Install, len(r.Probes))
		}
	})
	t.Run("before the probe's first observation", func(t *testing.T) {
		w := newWorld(t)
		w.probe.startErr = verdict.Condition{Code: verdict.ProbeNotStarted, Text: "no observation within limits.timeout"}
		w.probe.facts = probe.Facts{Lines: 12, JSONTextLines: 12, StoppedWhenAsked: true}
		w.run(context.Background())
		r := w.result()
		if r.Install == nil || !r.Install.Ready.Equal(ready) || r.Install.FirstObservation != nil || r.Install.Settled != nil || r.Install.Checks != 0 {
			t.Errorf("install %+v; want ready at %v, no first observation, no settling and no counts", r.Install, ready)
		}
		if len(r.Probes) != 1 || r.Probes[0].Lines != 12 {
			t.Errorf("probes %+v; want the probe that was started, with its 12 lines", r.Probes)
		}
	})
	t.Run("before the install had settled", func(t *testing.T) {
		w := newWorld(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		w.hooks["install-settled"] = func() {
			w.clock.Advance(4 * time.Second)
			cancel()
		}
		w.run(ctx)
		in := w.result().Install
		if in == nil || in.FirstObservation == nil || !in.FirstObservation.Equal(first) || in.Settled != nil || in.Checks != 800 {
			t.Errorf("install %+v; want the first observation at %v, no settling, and the 800 checks of 4 s", in, first)
		}
	})
	t.Run("after the install had settled", func(t *testing.T) {
		w := newWorld(t)
		w.probe.events = append(w.probe.events, failureAt(-14900*time.Millisecond, -14900*time.Millisecond))
		if code := w.run(context.Background()); code != 0 {
			t.Fatalf("exit %d, want 0: a failed check in the install window decides nothing", code)
		}
		r := w.result()
		in := r.Install
		if in == nil || in.Settled == nil || !in.Settled.Equal(t0) || in.Checks != 3001 || in.Failed != 1 ||
			in.LastFailure == nil || !in.LastFailure.Equal(t0.Add(-14900*time.Millisecond)) {
			t.Errorf("install %+v; want settled at %v, 3001 checks, 1 failed, the last 14.9 s before", in, t0)
		}
		if r.Windows[0].Start == nil || !r.Windows[0].Start.Equal(*in.Settled) {
			t.Errorf("the steady window starts at %v, want the moment the install had settled", r.Windows[0].Start)
		}
	})
}

// Rule 9: a fault of the probe before its first observation makes the run not verified, also when
// the probe then never starts. Both reasons are listed, and neither has a window.
func TestFaultsBeforeAFailedStartAreReasons(t *testing.T) {
	w := newWorld(t)
	w.probe.events = []probe.Event{{Fault: &verdict.Condition{Code: verdict.ProbeLineInvalid,
		Text: "the line is not one JSON object", At: w.clock.Now()}}}
	w.probe.startErr = verdict.Condition{Code: verdict.ProbeNotStarted, Text: "no observation within limits.timeout"}
	if code := w.run(context.Background()); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	r := w.result()
	if diff := cmp.Diff([]string{"probe-not-started", "probe-line-invalid"}, r.codes()); diff != "" {
		t.Errorf("reasons (-want +got):\n%s", diff)
	}
	for _, x := range r.Reasons {
		if x.Window != "" {
			t.Errorf("%s has the window %q, want none", x.Code, x.Window)
		}
	}
	if r.decided() != "probe-not-started" {
		t.Errorf("decided by %q, want probe-not-started", r.decided())
	}
	if n := w.calls.count("probe-stop"); n != 1 {
		t.Errorf("the probe's Stop was called %d times after a failed start, want 1", n)
	}
}

// A start that failed leaves the probe to be stopped; when that stop also fails, the run keeps it
// as a warning, as it does for any stop error (section 6).
func TestAFailedStartWhoseStopFailedWarns(t *testing.T) {
	w := newWorld(t)
	w.probe.startErr = verdict.Condition{Code: verdict.ProbeNotStarted, Text: "no observation within limits.timeout"}
	w.probe.stopErr = errors.New("the pod delete was refused")
	if code := w.run(context.Background()); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	r := w.result()
	if len(r.Warnings) != 1 {
		t.Fatalf("warnings %q, want the stop's one", r.Warnings)
	}
	for _, part := range []string{"the probe could not be stopped", "the pod delete was refused", "its pod may still be there"} {
		if !strings.Contains(r.Warnings[0], part) {
			t.Errorf("the warning %q does not hold %q", r.Warnings[0], part)
		}
	}
	if n := w.calls.count("probe-stop"); n != 1 {
		t.Errorf("the probe's Stop was called %d times after a failed start, want 1", n)
	}
	w.probeStopNotJudged(noteStopFailed)
}

// A Start that panicked before its log was followed gives an error at Stop: the tool did not
// finish asking, so the probe's stop is not judged and its pod may still be there.
func TestAStartThatPanicsWhoseStopGivesAnError(t *testing.T) {
	w := newWorld(t)
	w.hooks["start"] = func() { panic("the probe unit broke") }
	w.probe.stopErr = errors.New("the probe's start failed before its log was followed")
	if code := w.run(context.Background()); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	r := w.result()
	if codes := r.codes(); !slices.Contains(codes, "internal-error") {
		t.Errorf("reasons %q, want internal-error among them", codes)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "its pod may still be there") {
		t.Errorf("warnings %q, want the stop's one", r.Warnings)
	}
	w.probeStopNotJudged(noteStopFailed)
}

// lateClock is the fake clock except that it runs before once, ahead of the run's first wait,
// which is the steady window's.
type lateClock struct {
	*fakeClock
	before func()
	once   sync.Once
}

func (c *lateClock) After(d time.Duration) <-chan time.Time {
	c.once.Do(c.before)
	return c.fakeClock.After(d)
}

// when reports whether a mark's time is there and is want.
func when(got *time.Time, want time.Time) bool { return got != nil && got.Equal(want) }

// The install window runs from the first observation until the install has settled, the steady
// window begins there, and result.json's windows do not list it (sections 4 and 10).
func TestTheSteadyWindowBeginsWhenTheInstallHasSettled(t *testing.T) {
	w := newWorld(t)
	w.watcher.late = 3 * time.Second // the tool looks 3 s after the install settled
	if code := w.run(context.Background()); code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, w.stdout.String())
	}
	marks := w.windowMarks()
	if len(marks) != 4 || marks[0].Name != "install" || marks[1].Name != "steady" {
		t.Fatalf("marks %+v, want install, steady, deploy, rollback", marks)
	}
	if !when(marks[0].Start, t0.Add(-15*time.Second)) || !when(marks[0].End, t0) {
		t.Errorf("the install window is [%v, %v), want [%v, %v)", marks[0].Start, marks[0].End, t0.Add(-15*time.Second), t0)
	}
	if !when(marks[1].Start, t0) || !when(marks[1].End, t0.Add(10*time.Second)) {
		t.Errorf("the steady window is [%v, %v), want [%v, %v)", marks[1].Start, marks[1].End, t0, t0.Add(10*time.Second))
	}
	if !when(marks[2].Start, t0.Add(10*time.Second)) {
		t.Errorf("the deploy's window starts at %v, want the steady window's end, %v", marks[2].Start, t0.Add(10*time.Second))
	}
	r := w.result()
	if diff := cmp.Diff([]string{"pass", "pass", "pass"}, r.results()); diff != "" {
		t.Errorf("window results (-want +got):\n%s", diff)
	}
	if r.Windows[0].Name != "steady" || r.Windows[0].Start == nil || !r.Windows[0].Start.Equal(t0) {
		t.Errorf("result.json's first window is %+v, want steady from %v", r.Windows[0], t0)
	}
}

func TestAFailedCheckInTheInstallWindowDecidesNothing(t *testing.T) {
	w := newWorld(t)
	w.probe.events = append(w.probe.events, failureAt(-10*time.Second, -10*time.Second))
	if code := w.run(context.Background()); code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, w.stdout.String())
	}
	if r := w.result(); len(r.Reasons) != 0 {
		t.Errorf("reasons %v, want none", r.codes())
	}
}

// Rule 4, and Review Focus 4: a run that ends in the install window stops the probe, deletes the
// cluster and writes a final result.
func TestTheInstallWindowNotVerified(t *testing.T) {
	first := t0.Add(-15 * time.Second)
	failed := verdict.Condition{Code: verdict.InstallFailed, Text: "the install was not ready when limits.timeout passed"}
	agent := verdict.Condition{Code: verdict.WorkloadUnsupported, At: t0.Add(-6 * time.Second),
		Text: "daemonset/agent updates with OnDelete; the tool judges only a workload that updates by rolling"}
	settling := func(s watch.Settling) func(*world) {
		return func(w *world) { w.watcher.settle = func(time.Time) watch.Settling { return s } }
	}
	for _, row := range []struct {
		name         string
		script       func(w *world)
		code, window string
		end          time.Time // the install window's end in windows.json
	}{
		{"the probe exits", func(w *world) {
			w.probe.events = append(w.probe.events, probe.Event{Fault: &verdict.Condition{Code: verdict.ProbeExited,
				Text: "the probe exited with exit code 1 (Error)", At: t0.Add(-5 * time.Second)}})
		}, "probe-exited", "install", t0},
		{"the probe is silent and then writes again", func(w *world) {
			w.probe.events = append(passes(-15, -12), passes(-5, 70)...)
		}, "probe-silent", "install", t0},
		{"the install fails while it settles",
			settling(watch.Settling{First: first, Ended: t0.Add(25 * time.Second), InWindow: []verdict.Condition{failed}}),
			"install-failed", "install", t0.Add(25 * time.Second)},
		{"a workload the tool cannot judge appears",
			settling(watch.Settling{First: first, Settled: t0, Ended: t0, InWindow: []verdict.Condition{agent}}),
			"workload-unsupported", "install", t0},
		{"the install had failed before the first observation",
			settling(watch.Settling{First: first, Ended: first, Before: []verdict.Condition{failed}}),
			"install-failed", "", first},
	} {
		t.Run(row.name, func(t *testing.T) {
			w := newWorld(t)
			row.script(w)
			if code := w.run(context.Background()); code != 2 {
				t.Fatalf("exit %d, want 2\n%s", code, w.stdout.String())
			}
			r := w.result()
			if !r.Final || r.Verdict != "not-verified" {
				t.Errorf("final %v, verdict %q; want a final not-verified", r.Final, r.Verdict)
			}
			if diff := cmp.Diff([]string{row.code}, r.codes()); diff != "" {
				t.Errorf("reasons (-want +got):\n%s", diff)
			}
			if window, _ := r.windowOf(row.code); window != row.window {
				t.Errorf("%s in window %q, want %q", row.code, window, row.window)
			}
			if marks := w.windowMarks(); len(marks) < 2 || !when(marks[0].End, row.end) || marks[1].Start != nil {
				t.Errorf("marks %+v; want the install window to end at %v, and no steady window", marks, row.end)
			}
			if diff := cmp.Diff([]string{"not-run", "not-run", "not-run"}, r.results()); diff != "" {
				t.Errorf("window results (-want +got):\n%s", diff)
			}
			for name, want := range map[string]int{"deploy": 0, "probe-stop": 1, "delete": 1} {
				if n := w.calls.count(name); n != want {
					t.Errorf("%s was called %d times, want %d", name, n, want)
				}
			}
		})
	}
}

func TestSignalInTheInstallWindow(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.hooks["install-settled"] = func() {
		w.clock.Advance(4 * time.Second)
		cancel()
	}
	if code := w.run(ctx); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	r := w.result()
	if window, _ := r.windowOf("interrupted"); window != "install" || !r.Final {
		t.Errorf("interrupted in window %q, final %v; want install and final", window, r.Final)
	}
	if marks := w.windowMarks(); marks[0].End == nil || !marks[0].End.Equal(t0.Add(-11*time.Second)) || marks[1].Start != nil {
		t.Errorf("marks %+v; want the install window cut at the signal and no steady window", marks)
	}
}

func TestAToolFailureInTheInstallWindow(t *testing.T) {
	w := newWorld(t)
	w.hooks["install-settled"] = func() { panic("the watch unit broke") }
	if code := w.run(context.Background()); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if window, text := w.result().windowOf("internal-error"); window != "install" || text != "the watch unit broke" {
		t.Errorf("internal-error in window %q with text %q", window, text)
	}
	w.probeWasStopped()
}

func TestASettlingWithNoResultIsTheToolsFailure(t *testing.T) {
	w := newWorld(t)
	w.watcher.settle = func(first time.Time) watch.Settling {
		return watch.Settling{First: first, Ended: first.Add(time.Second)}
	}
	if code := w.run(context.Background()); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if window, text := w.result().windowOf("internal-error"); window != "install" || text != "the install neither settled nor failed" {
		t.Errorf("internal-error in window %q with text %q", window, text)
	}
	w.probeWasStopped()
}

// probeWasStopped fails the test unless the run stopped the probe once, kept the Facts its Stop
// gave in the probe's entry, and deleted the cluster (section 6, "Stop").
func (w *world) probeWasStopped() {
	w.t.Helper()
	for name, want := range map[string]int{"probe-stop": 1, "delete": 1} {
		if n := w.calls.count(name); n != want {
			w.t.Errorf("%s was called %d times, want %d", name, n, want)
		}
	}
	r := w.result()
	if len(r.Probes) != 1 || r.Probes[0].Lines != w.probe.facts.Lines {
		w.t.Errorf("probes %+v, want one whose lines are the %d that Stop gave", r.Probes, w.probe.facts.Lines)
	}
}

// A failure of the tool stops the probe, as the end of the windows does (section 6, "Stop"). When
// the failure was a panic of the event reader, nothing reads the probe's events any more, and the
// drain's lines come on a channel with no buffer: the stop must not wait for a reader.
func TestTheProbeStopsAfterTheReaderPanics(t *testing.T) {
	w := newWorld(t)
	w.probe.unbuffered = true
	w.probe.events = passes(-15, -15)
	w.probe.last = passes(-14, -10)
	defer func(h func(probe.Event)) { eventHook = h }(eventHook)
	eventHook = func(probe.Event) { panic("the timeline broke") }
	done := make(chan int, 1)
	go func() { done <- w.run(context.Background()) }()
	select {
	case code := <-done:
		if code != 2 {
			t.Fatalf("exit %d, want 2", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run did not end: the probe's stop waits for a reader that has failed")
	}
	r := w.result()
	if window, text := r.windowOf("internal-error"); window != "install" || text != "the timeline broke" {
		t.Errorf("internal-error in window %q with text %q", window, text)
	}
	w.probeWasStopped()
}

// A panic or a signal while the probe stops after a failure of the tool: the panic is one more
// internal-error and the first stays, and a signal is placed as stopProbe places one.
func TestWhileTheProbeStopsAfterAToolFailure(t *testing.T) {
	t.Run("a panic", func(t *testing.T) {
		w := newWorld(t)
		w.hooks["install-settled"] = func() { panic("the watch unit broke") }
		w.hooks["probe-stop"] = func() { panic("the probe unit broke") }
		if code := w.run(context.Background()); code != 2 {
			t.Errorf("exit %d, want 2", code)
		}
		r := w.result()
		var texts []string
		for _, x := range r.Reasons {
			texts = append(texts, x.Code+" in "+x.Window+": "+x.Text)
		}
		want := []string{"internal-error in install: the watch unit broke", "internal-error in install: the probe unit broke"}
		if diff := cmp.Diff(want, texts); diff != "" {
			t.Errorf("reasons (-want +got):\n%s", diff)
		}
		for name, want := range map[string]int{"probe-stop": 1, "delete": 1} {
			if n := w.calls.count(name); n != want {
				t.Errorf("%s was called %d times, want %d", name, n, want)
			}
		}
	})
	t.Run("a signal", func(t *testing.T) {
		w := newWorld(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		w.hooks["install-settled"] = func() { panic("the watch unit broke") }
		w.hooks["probe-stop"] = cancel
		if code := w.run(ctx); code != 2 {
			t.Errorf("exit %d, want 2", code)
		}
		r := w.result()
		if diff := cmp.Diff([]string{"interrupted", "internal-error"}, r.codes()); diff != "" {
			t.Errorf("reasons (-want +got):\n%s", diff)
		}
		if window, _ := r.windowOf("interrupted"); window != "install" {
			t.Errorf("interrupted in window %q, want install", window)
		}
		for name, want := range map[string]int{"probe-stop": 1, "delete": 0} {
			if n := w.calls.count(name); n != want {
				t.Errorf("%s was called %d times, want %d", name, n, want)
			}
		}
		if len(r.Probes) != 1 || r.Probes[0].Lines != w.probe.facts.Lines {
			t.Errorf("probes %+v, want one whose lines are the %d that Stop gave", r.Probes, w.probe.facts.Lines)
		}
	})
}

// Rule 8: a condition of the install window that the tool learns of only after the steady window
// began is placed late. The steady window runs to its end, and the run stops there.
func TestAnInstallWindowJudgedLate(t *testing.T) {
	w := newWorld(t)
	u := w.units()
	u.Clock = &lateClock{fakeClock: w.clock, before: func() {
		w.probe.send(probe.Event{Fault: &verdict.Condition{Code: verdict.ProbeLineInvalid,
			Text: "the line is not one JSON object", At: t0.Add(-5 * time.Second)}})
	}}
	code := Run(context.Background(), Options{ConfigPath: filepath.Join(w.cfg.Dir, "rollproof.yaml"), Out: w.out,
		Stdout: &w.stdout, Stderr: &w.stderr, Version: "test", Getenv: func(string) string { return "" }}, u)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	r := w.result()
	if window, _ := r.windowOf("probe-line-invalid"); window != "install" {
		t.Errorf("probe-line-invalid in window %q, want install", window)
	}
	if diff := cmp.Diff([]string{"not-judged", "not-run", "not-run"}, r.results()); diff != "" {
		t.Errorf("window results (-want +got):\n%s", diff)
	}
	if end := r.Windows[0].End; end == nil || !end.Equal(t0.Add(10*time.Second)) {
		t.Errorf("the steady window ended at %v, want its whole length, %v", end, t0.Add(10*time.Second))
	}
	if n := w.calls.count("deploy"); n != 0 {
		t.Errorf("deploy was called %d times, want 0", n)
	}
}

// restartWorld is a world whose deployer gives a restart alone, as a Helm driver with no previous
// release does, and whose restart replaces both pods of deployment/orders.
func restartWorld(t *testing.T) *world {
	w := newWorld(t)
	w.deployer.transitions = []verdict.WindowName{verdict.Restart}
	w.watcher.restart = watch.Restart{Accepted: true}
	w.watcher.transitions[verdict.Restart] = w.watcher.transitions[verdict.Deploy]
	return w
}

// after gives the calls that follow the first call named name.
func after(calls []string, name string) []string {
	i := slices.Index(calls, name)
	if i < 0 {
		return nil
	}
	return calls[i+1:]
}

// A restart's window runs the watcher's restart in place of a command and then waits for the
// transition as for any other; its record says that the API server took the change, and a pass
// says that only a restart was tested.
func TestRestartTransition(t *testing.T) {
	w := restartWorld(t)
	if code := w.run(context.Background()); code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, w.stdout.String())
	}
	got := after(w.calls.list(), "install-settled")
	if len(got) < 3 || !slices.Equal(got[:3], []string{"restart", "transition-restart", "probe-stop"}) {
		t.Errorf("the calls after install-settled are %q, want restart, transition-restart, probe-stop first", got)
	}
	r := w.result()
	if diff := cmp.Diff([]string{"pass", "pass"}, r.results()); diff != "" {
		t.Errorf("window results (-want +got):\n%s", diff)
	}
	if len(r.Windows) != 2 || r.Windows[0].Name != "steady" || r.Windows[1].Name != "restart" {
		t.Errorf("windows %+v, want steady and restart", r.Windows)
	}
	if len(r.Transitions) != 1 || r.Transitions[0].Command != nil || r.Transitions[0].Restart == nil ||
		*r.Transitions[0].Restart != "accepted" {
		t.Errorf("transitions %+v, want one with command null and restart accepted", r.Transitions)
	}
	const line = "\n  a deploy to another release and a rollback, because only a restart was tested\n"
	if !strings.Contains(w.stdout.String(), line) {
		t.Errorf("stdout has no %q:\n%s", line, w.stdout.String())
	}
}

// A restart that the API server refused makes its window not verified as restart-failed, with the
// workload and the server's text, and never as a transition that replaced no pod.
func TestRestartStopsAtARefusal(t *testing.T) {
	w := restartWorld(t)
	w.watcher.restart = watch.Restart{Accepted: false, Workload: "statefulset/db",
		Text: `admission webhook "deny" denied the request`}
	w.watcher.transitions[verdict.Restart] = watch.Transition{}
	if code := w.run(context.Background()); code != 2 {
		t.Fatalf("exit %d, want 2\n%s", code, w.stdout.String())
	}
	r := w.result()
	if diff := cmp.Diff([]string{"restart-failed"}, r.codes()); diff != "" {
		t.Errorf("reasons (-want +got):\n%s", diff)
	}
	const want = `the API server refused the restart of statefulset/db: admission webhook "deny" denied the request`
	if window, text := r.windowOf("restart-failed"); window != "restart" || text != want {
		t.Errorf("restart-failed in window %q with text %q, want restart with %q", window, text, want)
	}
	if len(r.Transitions) != 1 || r.Transitions[0].Restart == nil || *r.Transitions[0].Restart != "refused" {
		t.Errorf("transitions %+v, want one with restart refused", r.Transitions)
	}
}

// In a restart-only run the install installs the candidate, and the install's texts name it
// (section 4, "A run with no previous release"); a run with a previous release keeps the texts as
// they are.
func TestRestartOnlyInstallNamesTheCandidate(t *testing.T) {
	restartOnly := []verdict.WindowName{verdict.Restart}
	both := []verdict.WindowName{verdict.Deploy, verdict.Rollback}
	settleWith := func(text string) func(w *world) {
		return func(w *world) {
			w.watcher.settle = func(first time.Time) watch.Settling {
				return watch.Settling{First: first, Ended: first.Add(5 * time.Second),
					InWindow: []verdict.Condition{{Code: verdict.InstallFailed, At: first.Add(5 * time.Second), Text: text}}}
			}
		}
	}
	settleBefore := func(text string) func(w *world) {
		return func(w *world) {
			w.watcher.settle = func(first time.Time) watch.Settling {
				return watch.Settling{First: first, Ended: first.Add(5 * time.Second),
					Before: []verdict.Condition{{Code: verdict.InstallFailed, Text: text}}}
			}
		}
	}
	const (
		within  = "the install was not ready within limits.timeout\ndeployment/orders: 0 of 2 ready"
		passed  = "the install was not ready when limits.timeout passed\npod orders-1 was deleted"
		stopped = "the install stopped being ready after limits.timeout, while it still settled\npod orders-1 was deleted"
	)
	for _, row := range []struct {
		name        string
		transitions []verdict.WindowName
		fault       func(w *world)
		window      string
		text        string
		detail      []string
	}{
		{"the install exits non-zero, restart only", restartOnly, func(w *world) {
			w.deployer.results["install"] = deploy.Result{Ending: deploy.ExitedNonZero, ExitCode: 1}
		}, "", "the install of the candidate exited with code 1", nil},
		{"the install exits non-zero, with a previous release", both, func(w *world) {
			w.deployer.results["install"] = deploy.Result{Ending: deploy.ExitedNonZero, ExitCode: 1}
		}, "", "the install command exited with code 1", nil},
		{"not ready within the limit, restart only", restartOnly, func(w *world) {
			w.watcher.installErr = verdict.Condition{Code: verdict.InstallFailed, Text: within}
		}, "", "the install of the candidate was not ready within limits.timeout", []string{"deployment/orders: 0 of 2 ready"}},
		{"not ready within the limit, with a previous release", both, func(w *world) {
			w.watcher.installErr = verdict.Condition{Code: verdict.InstallFailed, Text: within}
		}, "", "the install was not ready within limits.timeout", []string{"deployment/orders: 0 of 2 ready"}},
		{"not ready when the limit passed, restart only", restartOnly, settleWith(passed),
			"install", "the install of the candidate was not ready when limits.timeout passed", []string{"pod orders-1 was deleted"}},
		{"not ready when the limit passed, with a previous release", both, settleWith(passed),
			"install", "the install was not ready when limits.timeout passed", []string{"pod orders-1 was deleted"}},
		{"stopped being ready, restart only", restartOnly, settleWith(stopped),
			"install", "the install of the candidate stopped being ready after limits.timeout, while it still settled", []string{"pod orders-1 was deleted"}},
		{"stopped being ready, with a previous release", both, settleWith(stopped),
			"install", "the install stopped being ready after limits.timeout, while it still settled", []string{"pod orders-1 was deleted"}},
		{"stopped being ready before the install window, restart only", restartOnly, settleBefore(stopped),
			"", "the install of the candidate stopped being ready after limits.timeout, while it still settled", []string{"pod orders-1 was deleted"}},
		{"stopped being ready before the install window, with a previous release", both, settleBefore(stopped),
			"", "the install stopped being ready after limits.timeout, while it still settled", []string{"pod orders-1 was deleted"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			w := newWorld(t)
			w.deployer.transitions = row.transitions
			row.fault(w)
			if code := w.run(context.Background()); code != 2 {
				t.Fatalf("exit %d, want 2\n%s", code, w.stdout.String())
			}
			r := w.result()
			if diff := cmp.Diff([]string{"install-failed"}, r.codes()); diff != "" {
				t.Errorf("reasons (-want +got):\n%s", diff)
			}
			if window, text := r.windowOf("install-failed"); window != row.window || text != row.text {
				t.Errorf("install-failed in window %q with text %q, want %q with %q", window, text, row.window, row.text)
			}
			if diff := cmp.Diff(row.detail, r.detailOf("install-failed"), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("detail (-want +got):\n%s", diff)
			}
		})
	}
}

// A panic in the restart is the tool's failure, caught as a command's is and placed in the
// restart's window.
func TestRestartPanics(t *testing.T) {
	w := restartWorld(t)
	w.watcher.restartPanics = true
	if code := w.run(context.Background()); code != 2 {
		t.Fatalf("exit %d, want 2\n%s", code, w.stdout.String())
	}
	r := w.result()
	if !slices.Contains(r.codes(), "internal-error") {
		t.Fatalf("reasons %q, want internal-error", r.codes())
	}
	if window, text := r.windowOf("internal-error"); window != "restart" || !strings.Contains(text, "the restart broke") {
		t.Errorf("internal-error in window %q with text %q, want restart with the panic's text", window, text)
	}
}

// A transition whose name the run cannot start is the tool's failure, and is never run as a
// deploy.
func TestUnknownTransition(t *testing.T) {
	w := newWorld(t)
	w.deployer.transitions = []verdict.WindowName{"upgrade"}
	if code := w.run(context.Background()); code != 2 {
		t.Fatalf("exit %d, want 2\n%s", code, w.stdout.String())
	}
	r := w.result()
	if diff := cmp.Diff([]string{"internal-error"}, r.codes()); diff != "" {
		t.Errorf("reasons (-want +got):\n%s", diff)
	}
	if _, text := r.windowOf("internal-error"); text != "the tool cannot start a upgrade transition" {
		t.Errorf("internal-error with text %q", text)
	}
	for _, name := range []string{"deploy", "rollback", "restart"} {
		if n := w.calls.count(name); n != 0 {
			t.Errorf("%s called %d times, want 0", name, n)
		}
	}
}

// A run with a deploy and no rollback runs the deploy alone, and its pass says what that leaves
// out.
func TestDeployWithoutRollback(t *testing.T) {
	w := newWorld(t)
	w.deployer.transitions = []verdict.WindowName{verdict.Deploy}
	if code := w.run(context.Background()); code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, w.stdout.String())
	}
	if n := w.calls.count("rollback"); n != 0 {
		t.Errorf("rollback called %d times, want 0", n)
	}
	r := w.result()
	if diff := cmp.Diff([]string{"pass", "pass"}, r.results()); diff != "" {
		t.Errorf("window results (-want +got):\n%s", diff)
	}
	const line = "the candidate's shutdown and the installed release's start-up, because the run has no rollback"
	if !strings.Contains(w.stdout.String(), line) {
		t.Errorf("stdout has no %q:\n%s", line, w.stdout.String())
	}
}

// A restart that limits.timeout cut, and no signal, is a stall told once with the watcher's own,
// never a refusal: its record's restart is null.
func TestRestartStoppedAtTheLimit(t *testing.T) {
	w := restartWorld(t)
	w.cfg.Limits.Timeout = 50 * time.Millisecond
	w.watcher.restartWaits = true
	w.watcher.transitions[verdict.Restart] = watch.Transition{Stalled: true}
	done := make(chan int, 1)
	go func() { done <- w.run(context.Background()) }()
	var code int
	select {
	case code = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the run still waits for a restart that limits.timeout should have stopped")
	}
	if code != 1 {
		t.Fatalf("exit %d, want 1 (a stall is a fail)\n%s", code, w.stdout.String())
	}
	r := w.result()
	if diff := cmp.Diff([]string{"transition-stalled"}, r.codes()); diff != "" {
		t.Errorf("reasons (-want +got):\n%s", diff)
	}
	if window, text := r.windowOf("transition-stalled"); window != "restart" ||
		!strings.Contains(text, "the restart was still running at limits.timeout") {
		t.Errorf("transition-stalled in window %q with text %q", window, text)
	}
	if len(r.Transitions) != 1 || r.Transitions[0].Restart != nil {
		t.Errorf("transitions %+v, want one with restart null", r.Transitions)
	}
}

// A restart that a signal cut short is interrupted and never reaches the restart's own judgement:
// no refusal is told and its record's restart is null.
func TestRestartInterrupted(t *testing.T) {
	w := restartWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.watcher.restart = watch.Restart{Accepted: false, Text: "context canceled"}
	w.hooks["restart"] = cancel
	if code := w.run(ctx); code != 2 {
		t.Fatalf("exit %d, want 2\n%s", code, w.stdout.String())
	}
	r := w.result()
	if diff := cmp.Diff([]string{"interrupted"}, r.codes()); diff != "" {
		t.Errorf("reasons (-want +got):\n%s", diff)
	}
	if len(r.Transitions) != 1 || r.Transitions[0].Restart != nil {
		t.Errorf("transitions %+v, want one with restart null", r.Transitions)
	}
	raw, err := os.ReadFile(filepath.Join(w.out, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	for where, text := range map[string]string{"stdout": w.stdout.String(), "result.json": string(raw)} {
		if strings.Contains(text, "refused the restart of") {
			t.Errorf("%s tells of a refusal:\n%s", where, text)
		}
	}
}
