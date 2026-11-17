package report

import (
	"fmt"
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

// stallRun is a deploy that stalled because the surge pod found no node. Its
// sentences are the ones internal/watch writes: kubectl's message under the
// workload's name, the pod's phase and why it is not scheduled, and the
// unschedulable sentence of the tracker.
func stallRun() Run {
	const scheduler = "0/4 nodes are available: 1 node(s) had untolerated taint {node-role.kubernetes.io/control-plane: }, 3 node(s) didn't match pod anti-affinity rules. preemption: 0/4 nodes are available: 1 Preemption is not helpful for scheduling, 3 No preemption victims found for incoming pod."
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
				`deployment/orders: Waiting for deployment "orders" rollout to finish: 1 out of 3 new replicas have been updated...`,
				"pod orders-7c9d8b-q8w2n: phase Pending, not scheduled (Unschedulable: " + scheduler + ")",
			},
			Decided: true,
		},
		{
			Code: "pod-unschedulable", Verdict: "fail", Window: "deploy",
			Text: "pod orders-7c9d8b-q8w2n could not be scheduled on the test cluster's 3 worker nodes (cluster.workers): " + scheduler + "; a rolling update needs room for one more pod",
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
		Reasons: []Reason{{Code: "config-rejected", Verdict: "not-verified", Text: "deploy.namespace: reserved for the cluster or the probe", Decided: true}},
		Out:     "/tmp/rollproof-results-7f3a",
	}
	want := "rollproof: NOT VERIFIED\n\ncould not test: deploy.namespace: reserved for the cluster or the probe   [config-rejected]\nresults: /tmp/rollproof-results-7f3a\n"
	if got := Console(r); got != want {
		t.Errorf("Console =\n%s\nwant\n%s", got, want)
	}
}

// A failure that crosses two starts is counted in both windows, once in each:
// the console prints each window's own count and never their sum.
func TestConsoleCrossedIsCountedPerWindow(t *testing.T) {
	r := failRun()
	r.Windows[0].CrossedStart = 1
	r.Windows[1].CrossedStart = 1
	got := Console(r)
	for _, want := range []string{
		"1 failed check began before the steady started and ended in it\n",
		"1 failed check began before the deploy started and ended in it\n",
	} {
		if strings.Count(got, want) != 1 {
			t.Errorf("Console has %d of %q, want 1, in\n%s", strings.Count(got, want), want, got)
		}
	}
	if strings.Contains(got, "2 failed checks") {
		t.Errorf("Console sums the crossed counts of two windows:\n%s", got)
	}
}

// One window can carry several probe-silent reasons and only the first is
// decided; the console prints them all, in the order the record holds them.
func TestConsoleSeveralReasonsOfOneWindow(t *testing.T) {
	r := failRun()
	r.Verdict = "not-verified"
	r.Reasons = []Reason{
		{Code: "probe-silent", Verdict: "not-verified", Window: "deploy", Text: "the probe wrote nothing for 5.2 s", Decided: true},
		{Code: "probe-silent", Verdict: "not-verified", Window: "deploy", Text: "the probe wrote nothing for 6.1 s"},
		{Code: "probe-silent", Verdict: "not-verified", Window: "deploy", Text: "the probe wrote nothing for 7.0 s"},
	}
	got := Console(r)
	want := "deploy: the probe wrote nothing for 5.2 s   [probe-silent]\n" +
		"deploy: the probe wrote nothing for 6.1 s   [probe-silent]\n" +
		"deploy: the probe wrote nothing for 7.0 s   [probe-silent]\n"
	if !strings.Contains(got, want) {
		t.Errorf("Console has no\n%s\nin\n%s", want, got)
	}
}

// An error of kind's Create or Delete can carry its own line ends. The
// console writes each reason, each detail and each warning on one line.
func TestConsoleReasonsOnOneLine(t *testing.T) {
	r := notVerifiedRun()
	r.Reasons = []Reason{{
		Code: "cluster-failed", Verdict: "not-verified", Text: "creating the cluster: failed to create cluster\nsecond line\r\nthird line\rfourth line",
		Detail:  []string{"node rollproof-x-worker:\r\n  exit status 1\nbecause of a key quota"},
		Decided: true,
	}}
	r.Warnings = []string{"deleting the cluster:\nit was already gone"}
	got := Console(r)
	for _, want := range []string{
		"could not test: creating the cluster: failed to create cluster second line third line fourth line   [cluster-failed]\n",
		"  node rollproof-x-worker:   exit status 1 because of a key quota\n",
		"warning: deleting the cluster: it was already gone\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Console has no\n%q\nin\n%q", want, got)
		}
	}
	if strings.ContainsRune(got, '\r') {
		t.Errorf("Console holds a carriage return: %q", got)
	}
}

var allCodes = []string{
	"probe-failures", "transition-stalled", "pod-unschedulable", "command-failed",
	"config-rejected", "runner-unsupported", "cluster-failed", "chart-fetch-failed", "setup-failed",
	"install-failed", "nothing-watched", "workload-unsupported", "target-unsupported", "probe-not-started",
	"probe-exited", "probe-line-invalid", "steady-not-clean", "probe-silent", "no-pod-replaced",
	"restart-failed", "interrupted", "internal-error",
}

// A reason of the record that the console leaves out hides why a run failed.
func TestConsolePrintsEveryReason(t *testing.T) {
	for _, base := range []struct {
		name string
		run  func() Run
	}{{"fail", failRun}, {"not verified", notVerifiedRun}, {"before the configuration", func() Run { return Run{Verdict: "not-verified", Out: "/tmp/x"} }}} {
		t.Run(base.name, func(t *testing.T) {
			r := base.run()
			r.Reasons = nil
			for i, code := range allCodes {
				for n := 0; n < 3; n++ {
					window := ""
					if i%2 == 0 {
						window = []string{"steady", "deploy", "rollback"}[n]
					}
					r.Reasons = append(r.Reasons, Reason{
						Code: code, Verdict: "fail", Window: window, Decided: i == 0 && n == 0,
						Text:   fmt.Sprintf("text %s %d", code, n),
						Detail: []string{fmt.Sprintf("detail %s %d", code, n)},
					})
				}
			}
			got := Console(r)
			for _, rs := range r.Reasons {
				head := "could not test: "
				if rs.Window != "" {
					head = rs.Window + ": "
				}
				want := head + rs.Text + "   [" + rs.Code + "]\n  " + rs.Detail[0] + "\n"
				if !strings.Contains(got, want) {
					t.Errorf("Console has no\n%s\nin\n%s", want, got)
				}
			}
		})
	}
}

// The verdict line is the Run's verdict and nothing else: a verdict that is
// not pass or fail is never printed as one of them.
func TestConsoleVerdictLine(t *testing.T) {
	for verdict, want := range map[string]string{
		"pass": "rollproof: PASS   orders", "fail": "rollproof: FAIL   orders",
		"not-verified": "rollproof: NOT VERIFIED   orders", "": "rollproof: NOT VERIFIED   orders", "passed": "rollproof: NOT VERIFIED   orders",
	} {
		r := passRun()
		r.Verdict = verdict
		if got := Console(r); !strings.HasPrefix(got, want) {
			t.Errorf("verdict %q: Console begins %q, want %q", verdict, strings.SplitN(got, "\n", 2)[0], want)
		}
	}
}

// Every window and every workload of the record has its row.
func TestConsoleDropsNoRow(t *testing.T) {
	r := passRun()
	r.Verdict = "fail"
	r.Windows = append(r.Windows, Window{Name: "restart", Start: at(80_000), End: at(90_000), Result: "pass", Checks: 10, SlowestMs: 1, LagMs: 1})
	r.Transitions = append(r.Transitions, Transition{Name: "restart", Command: "ok", Workloads: []Workload{
		{Workload: "deployment/a", PodsAtStart: 1, PodsReplaced: 1, Probed: "yes"},
		{Workload: "statefulset/b", PodsAtStart: 1, PodsReplaced: 1, Probed: "no"},
		{Workload: "daemonset/c", PodsAtStart: 1, Probed: "not known"},
	}})
	got := Console(r)
	for _, want := range []string{"\nsteady ", "\ndeploy ", "\nrollback ", "\nrestart ", "deployment/a ", "statefulset/b ", "daemonset/c "} {
		if !strings.Contains(got, want) {
			t.Errorf("Console has no %q in\n%s", want, got)
		}
	}
}

// A Detail line longer than 92 characters is broken at the last space that
// keeps it within 92, with the same indent; a word longer than that stays
// whole; a shorter line keeps its spaces.
func TestConsoleWrapsDetail(t *testing.T) {
	long := strings.Repeat("w", 100)
	r := notVerifiedRun()
	r.Reasons = []Reason{{Code: "internal-error", Verdict: "not-verified", Text: "x", Decided: true, Detail: []string{
		strings.Repeat("a", 89) + " bb cc",
		"short   line  with   spaces",
		"see " + long + " end",
	}}}
	got := Console(r)
	want := "could not test: x   [internal-error]\n" +
		"  " + strings.Repeat("a", 89) + "\n  bb cc\n" +
		"  short   line  with   spaces\n" +
		"  see\n  " + long + "\n  end\n"
	if !strings.Contains(got, want) {
		t.Errorf("Console has no\n%s\nin\n%s", want, got)
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "  ") && len(line) > 92 && !strings.Contains(line, long) {
			t.Errorf("line of %d characters: %s", len(line), line)
		}
	}
}
