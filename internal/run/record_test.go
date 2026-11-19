package run

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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
		probe:     probe.Facts{Lines: 78, JSONTextLines: 2, StoppedWhenAsked: true, ClockDifference: 300 * time.Millisecond},
		probeStop: asked,
		// What Counts gave: a record whose stop was asked does not use it.
		probeLines: 80, probeJSONText: 3,
		probeBegun: true,
		target:     probe.Target{Service: &types.NamespacedName{Namespace: "orders", Name: "orders"}},
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
		"deployment/orders: Waiting for deployment \"orders\" rollout to finish: 1 out of 3 new replicas have been updated...",
		"pod orders-7f8e9d-k3m2p: phase Pending, not scheduled (Unschedulable: 0/4 nodes are available...)",
	}
	f.transitions[0].tr.Stalled = true
	f.transitions[0].tr.Incomplete = incomplete
	f.decision.Verdict = verdict.Fail
	f.decision.Reasons = []verdict.Reason{
		{Code: verdict.TransitionStalled, Verdict: verdict.Fail, Window: verdict.Deploy,
			Text: "did not finish within limits.timeout", Decided: true},
		{Code: verdict.PodUnschedulable, Verdict: verdict.Fail, Window: verdict.Deploy,
			Text: "pod orders-7f8e9d-k3m2p could not be scheduled on the test cluster's 3 worker nodes (cluster.workers): 0/4 nodes are available...; a rolling update needs room for one more pod"},
	}
	got := record(f)
	if diff := cmp.Diff(incomplete, got.Reasons[0].Detail); diff != "" {
		t.Errorf("detail of transition-stalled (-want +got):\n%s", diff)
	}
	if d := got.Reasons[1].Detail; len(d) != 0 {
		t.Errorf("detail of pod-unschedulable is %q, want none", d)
	}

	const last = "pod/orders-7f8e9d-k3m2p: FailedScheduling: 0/4 nodes are available..."
	f.transitions[0].tr.LastEvent = last
	got = record(f)
	if diff := cmp.Diff(append(slices.Clone(incomplete), "the last event: "+last), got.Reasons[0].Detail); diff != "" {
		t.Errorf("detail of transition-stalled with a last event (-want +got):\n%s", diff)
	}
	if got.Reasons[0].Text != "did not finish within limits.timeout" {
		t.Errorf("the stall's text %q changed", got.Reasons[0].Text)
	}
	if d := got.Reasons[1].Detail; len(d) != 0 {
		t.Errorf("detail of pod-unschedulable is %q, want none", d)
	}
	f.transitions[0].tr.LastEvent = ""

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
			"the install was not ready within limits.timeout\ndeployment/orders: Waiting for deployment \"orders\" rollout to finish: 0 out of 2 new replicas have been updated...\npod orders-6d5f9c-x2k4q: phase Running, container orders waiting (CrashLoopBackOff) and last ended (Error, exit code 1)",
			"the install was not ready within limits.timeout",
			[]string{"deployment/orders: Waiting for deployment \"orders\" rollout to finish: 0 out of 2 new replicas have been updated...",
				"pod orders-6d5f9c-x2k4q: phase Running, container orders waiting (CrashLoopBackOff) and last ended (Error, exit code 1)"}},
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
	f.transitions[0].tr.SettleRestarts = []string{"pod orders-7f8e9d-z9y8x of deployment/orders was created 12.3 s after the transition began"}
	f.transitions[0].tr.Workloads = append(f.transitions[0].tr.Workloads, watch.Count{
		Workload: watch.Workload{Kind: "deployment", Name: "orders-worker"}, AtStart: 1, Removed: true, Probed: "no"})
	f.transitions[1].cmd = deploy.Result{Ending: deploy.ExitedNonZero, ExitCode: 1}
	f.transitions[1].tr.Stalled = true
	f.transitions[1].tr.Workloads[0].Added = true
	want := []report.Transition{
		{Name: "deploy", Command: "ok", SettleRestartedBy: []string{"pod orders-7f8e9d-z9y8x of deployment/orders was created 12.3 s after the transition began"},
			Workloads: []report.Workload{
				{Workload: "deployment/orders", PodsAtStart: 2, PodsReplaced: 2, PodsCreated: 2, Probed: "yes", QuickestStopMs: i64(5300)},
				{Workload: "deployment/orders-worker", PodsAtStart: 1, Removed: true, Probed: "no"},
			}},
		{Name: "rollback", Stalled: true, Command: "exit-nonzero",
			Workloads: []report.Workload{
				{Workload: "deployment/orders", PodsAtStart: 2, PodsReplaced: 2, PodsCreated: 2, Added: true, Probed: "yes", QuickestStopMs: i64(5200)},
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
		CertificateChecked: true, StoppedWhenAsked: new(true), Notes: f.target.Notes,
	}
	if diff := cmp.Diff(want, got.Probes[0], cmpopts.EquateEmpty(), cmpopts.IgnoreFields(report.Probe{}, "Settings")); diff != "" {
		t.Errorf("probe (-want +got):\n%s", diff)
	}
	wantKeys := []string{"probe.http.rate", "probe.http.timeout", "probe.http.expect-status"}
	if diff := cmp.Diff(wantKeys, settingKeys(got.Probes[0].Settings)); diff != "" {
		t.Errorf("probe settings (-want +got):\n%s", diff)
	}

	// Stop was asked and gave its Facts: a probe that did not stop within the drain is false.
	f.probe.StoppedWhenAsked = false
	if p := record(f).Probes[0]; p.StoppedWhenAsked == nil || *p.StoppedWhenAsked || p.Lines != 78 {
		t.Errorf("probe %+v, want stopped_when_asked false and the 78 lines that Stop gave", p)
	}
	f.probe.StoppedWhenAsked = true

	f.cfg.Probe.HTTP.URL = "https://orders.orders.svc/work"
	if record(f).Probes[0].CertificateChecked {
		t.Error("certificate_checked is true for an https URL")
	}
	for _, u := range []string{"HTTPS://orders.orders.svc/work", "Https://orders.orders.svc/work", "https://orders.orders.svc/work", "ht\ttp://%zz"} {
		f.cfg.Probe.HTTP.URL = u
		if record(f).Probes[0].CertificateChecked {
			t.Errorf("certificate_checked is true for the URL %q", u)
		}
	}
	f.cfg.Probe.HTTP.URL = "HTTP://orders.orders.svc/work"
	if !record(f).Probes[0].CertificateChecked {
		t.Error("certificate_checked is false for an http URL")
	}
}

// settled gives f an install that was ready 21 s before t0, a first observation 15 s before t0,
// and an install window that ended at t0 with the counts of section 10's example.
func settled(f facts) facts {
	f.install.Ready = ms(-21000)
	f.settling = watch.Settling{First: ms(-15000), Settled: ms(0), Ended: ms(0)}
	f.decision.Windows = append([]verdict.WindowResult{{Name: verdict.Install, Start: ms(-15000), End: ms(0),
		Result: verdict.ResultPass, Checks: 3000, Failed: 6, ByClass: map[string]int{"refused": 5, "timeout": 1},
		FirstFailure: ms(-14990), LastFailure: ms(-14900), SlowestMs: 44, LagMs: 9}}, f.decision.Windows...)
	return f
}

func TestRecordInstall(t *testing.T) {
	whole := report.Install{Ready: ms(-21000), FirstObservation: ptr(ms(-15000)), Settled: ptr(ms(0)),
		Checks: 3000, Failed: 6, FailedByClass: map[string]int{"refused": 5, "timeout": 1},
		LastFailure: ptr(ms(-14900)), SlowestMs: 44, LagMs: 9}
	t.Run("a run that reached its steady window", func(t *testing.T) {
		r := record(settled(sampleFacts(t)))
		if diff := cmp.Diff(&whole, r.Install, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("install (-want +got):\n%s", diff)
		}
		if len(r.Windows) != 3 || r.Windows[0].Name != "steady" {
			t.Errorf("windows %+v; the install window is never among them", r.Windows)
		}
	})
	t.Run("a run that ended before the install had settled", func(t *testing.T) {
		f := settled(sampleFacts(t))
		f.settling = watch.Settling{First: ms(-15000), Ended: ms(-9000),
			Restarts: []string{"no workload was watched 8.0 s after the install was ready"}}
		want := whole
		want.Settled, want.SettleRestartedBy = nil, f.settling.Restarts
		if diff := cmp.Diff(&want, record(f).Install, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("install (-want +got):\n%s", diff)
		}
	})
	t.Run("a run that ended before the probe's first observation", func(t *testing.T) {
		f := sampleFacts(t)
		f.install.Ready = ms(-21000)
		f.decision = verdict.Decision{Verdict: verdict.NotVerified}
		if diff := cmp.Diff(&report.Install{Ready: ms(-21000)}, record(f).Install, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("install (-want +got):\n%s", diff)
		}
	})
	t.Run("a run that ended before the install was ready", func(t *testing.T) {
		f := sampleFacts(t)
		f.decision = verdict.Decision{Verdict: verdict.NotVerified}
		if got := record(f).Install; got != nil {
			t.Errorf("install = %+v, want none", got)
		}
	})
}

// Ruling 3: the probe is in the record from the moment the tool starts it, whether or not an
// observation came.
func TestRecordProbeOnceTheToolStartedIt(t *testing.T) {
	f := sampleFacts(t)
	f.decision = verdict.Decision{Verdict: verdict.NotVerified}
	if n := len(record(f).Probes); n != 1 {
		t.Errorf("%d probes in the record of a run whose probe was started, want 1", n)
	}
	f.probeBegun = false
	if n := len(record(f).Probes); n != 0 {
		t.Errorf("%d probes in the record of a run that never started its probe, want 0", n)
	}
}

// A stop that the tool did not ask, did not wait for or did not finish is not judged: null, the
// counts that Counts gave, and a note after the target's that says which (section 10).
func TestRecordProbeNotAskedToStop(t *testing.T) {
	const affinity = "the Service sends one client to one pod; every connection of the probe reaches the same pod until that pod is removed"
	for _, row := range []struct {
		name string
		stop probeStop
		note string
	}{
		{"a signal ended the run", notAsked, noteNotAsked},
		{"a signal came while the tool waited", cutBySignal, noteCutBySignal},
		{"the tool failed while it stopped the probe", stopFailed, noteStopFailed},
	} {
		t.Run(row.name, func(t *testing.T) {
			f := sampleFacts(t)
			f.target.Notes = []string{affinity}
			f.probeStop = row.stop
			f.probe = probe.Facts{Lines: 3, StoppedWhenAsked: false}
			f.probeLines, f.probeJSONText = 7, 1
			got := record(f).Probes
			if len(got) != 1 {
				t.Fatalf("%d probes, want 1", len(got))
			}
			if got[0].StoppedWhenAsked != nil {
				t.Errorf("stopped_when_asked is %v, want null", *got[0].StoppedWhenAsked)
			}
			if got[0].Lines != 7 || got[0].JSONLinesWithoutOK != 1 {
				t.Errorf("lines %d and json_lines_without_ok %d, want the 7 and 1 of Counts",
					got[0].Lines, got[0].JSONLinesWithoutOK)
			}
			if diff := cmp.Diff([]string{affinity, row.note}, got[0].Notes); diff != "" {
				t.Errorf("notes (-want +got):\n%s", diff)
			}
		})
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

func TestRecordNeverCopiesCommandOutput(t *testing.T) {
	f := sampleFacts(t)
	for i := range f.transitions {
		f.transitions[i].cmd.Stdout = "S3CRET-VALUE"
	}
	f.cfg.Probe.HTTP.Headers["X-Other"] = "S3CRET-VALUE"
	data, err := json.Marshal(record(f))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "S3CRET-VALUE") {
		t.Errorf("the record holds a value that only a command's output or a header holds:\n%s", data)
	}
}

func TestRecordKeepsEmptyListsAsLists(t *testing.T) {
	f := sampleFacts(t)
	f.decision = verdict.Decision{Verdict: verdict.Pass, GoOn: true}
	f.warnings = nil
	got := record(f)
	w, err := report.Open(t.TempDir(), func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Final(got); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(w.Dir(), "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"reasons", "warnings"} {
		if s := strings.TrimSpace(string(out[key])); s != "[]" {
			t.Errorf("%s is %s, want []", key, s)
		}
	}
}

func TestRecordCopiesProbed(t *testing.T) {
	for _, probed := range []string{"yes", "no", "not known"} {
		f := sampleFacts(t)
		f.transitions[0].tr.Workloads[0].Probed = probed
		if got := record(f).Transitions[0].Workloads[0].Probed; got != probed {
			t.Errorf("probed %q, want %q", got, probed)
		}
	}
}

func TestRecordCrossedIsCopiedPerWindow(t *testing.T) {
	f := sampleFacts(t)
	f.decision.Windows[1].Crossed, f.decision.Windows[2].Crossed = 1, 1
	got := record(f).Windows
	if got[1].CrossedStart != 1 || got[2].CrossedStart != 1 {
		t.Errorf("crossed %d and %d, want 1 and 1", got[1].CrossedStart, got[2].CrossedStart)
	}
}

func TestRecordKeepsEveryReasonOfAWindow(t *testing.T) {
	f := sampleFacts(t)
	f.decision.Verdict = verdict.NotVerified
	f.decision.Reasons = []verdict.Reason{
		{Code: verdict.ProbeSilent, Verdict: verdict.NotVerified, Window: verdict.Deploy, Text: "first silence", Decided: true},
		{Code: verdict.ProbeSilent, Verdict: verdict.NotVerified, Window: verdict.Deploy, Text: "second silence"},
		{Code: verdict.ProbeSilent, Verdict: verdict.NotVerified, Window: verdict.Deploy, Text: "third silence"},
	}
	got := record(f).Reasons
	if len(got) != 3 || got[0].Text != "first silence" || !got[0].Decided ||
		got[1].Text != "second silence" || got[1].Decided || got[2].Text != "third silence" {
		t.Errorf("reasons %+v", got)
	}
}

func TestRecordTextWithCarriageReturns(t *testing.T) {
	f := sampleFacts(t)
	f.decision = verdict.Decision{Verdict: verdict.NotVerified, Reasons: []verdict.Reason{{
		Code: verdict.ClusterFailed, Verdict: verdict.NotVerified, Decided: true, Text: "create the cluster\r\na node did not start"}}}
	got := record(f).Reasons[0]
	if got.Text != "create the cluster" || !cmp.Equal(got.Detail, []string{"a node did not start"}) {
		t.Errorf("text %q, detail %q", got.Text, got.Detail)
	}
}

func TestRecordPodLinesOutsideDeployAndRollback(t *testing.T) {
	f := sampleFacts(t)
	f.transitions = append(f.transitions, transitionFacts{
		tr:       watch.Transition{Name: verdict.Restart},
		stopping: []watch.PodNote{{Name: "orders-6d5f9c-x2k4q", Workload: orders, Old: true, For: time.Second}}})
	f.decision.Verdict = verdict.Fail
	f.decision.Reasons = []verdict.Reason{{Code: verdict.ProbeFailures, Verdict: verdict.Fail,
		Window: verdict.Restart, Text: "1 check failed", Decided: true}}
	want := []string{"stopping then   orders-6d5f9c-x2k4q, told to stop 1.0 s before", "starting then   none"}
	if diff := cmp.Diff(want, record(f).Reasons[0].Detail); diff != "" {
		t.Errorf("detail (-want +got):\n%s", diff)
	}
}

func TestRecordImageWithoutDigest(t *testing.T) {
	f := sampleFacts(t)
	f.images = nil
	f.transitions[0].tr.Workloads[0].Digests = []string{""}
	for _, im := range record(f).Images {
		if im.Digest != "" || len(im.RunBy) != 0 {
			t.Errorf("image %+v: a digest that was never read must match no workload", im)
		}
	}
}
