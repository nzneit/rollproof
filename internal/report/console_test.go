package report

import (
	"fmt"
	"maps"
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

// notVerifiedRun is the record of section 10's run that could not test, which
// ended before the tool started the probe. Its golden file, the third example
// of section 10, has no probe lines.
func notVerifiedRun() Run {
	r := passRun()
	r.Verdict = "not-verified"
	r.Windows = []Window{{Name: "steady", Result: "not-run"}, {Name: "deploy", Result: "not-run"}, {Name: "rollback", Result: "not-run"}}
	r.Transitions = nil
	r.Probes, r.Install = nil, nil
	r.Images[0].RunBy = nil
	r.Reasons = []Reason{{
		Code: "nothing-watched", Verdict: "not-verified", Text: "nothing to watch",
		Detail:  []string{"15 s after the install command returned, namespace orders held no Deployment, StatefulSet or DaemonSet that matches the selector app=order"},
		Decided: true,
	}}
	return r
}

// beforeFirstRun is a run that ended before the probe's first observation: the probe did not
// start in time.
func beforeFirstRun() Run {
	r := passRun()
	r.Verdict = "not-verified"
	r.Windows = []Window{{Name: "steady", Result: "not-run"}, {Name: "deploy", Result: "not-run"}, {Name: "rollback", Result: "not-run"}}
	r.Transitions = nil
	r.Images[0].RunBy = nil
	r.Install = &Install{Ready: *at(-21_000)}
	r.Reasons = []Reason{{Code: "probe-not-started", Verdict: "not-verified", Decided: true,
		Text: "no observation within limits.timeout; the pod is Pending; container probe is waiting: ImagePullBackOff"}}
	return r
}

// inInstallRun is a run that a signal ended in the install window.
func inInstallRun() Run {
	r := beforeFirstRun()
	r.Install = &Install{Ready: *at(-21_000), FirstObservation: at(-15_000), Checks: 1200, Failed: 6,
		FailedByClass: map[string]int{"refused": 5, "timeout": 1}, LastFailure: at(-14_900), SlowestMs: 44, LagMs: 9}
	r.Reasons = []Reason{{Code: "interrupted", Verdict: "not-verified", Window: "install", Text: "a signal stopped the run", Decided: true}}
	return r
}

// installDecidedRun is a run whose install window decided the verdict: the probe was silent in it.
func installDecidedRun() Run {
	r := beforeFirstRun()
	r.Install = &Install{Ready: *at(-21_000), FirstObservation: at(-15_000), Settled: at(0), Checks: 1800, SlowestMs: 44, LagMs: 9}
	r.Reasons = []Reason{{Code: "probe-silent", Verdict: "not-verified", Window: "install", Decided: true,
		Text: "no observation for 6.0 s, from 09:59:42.00"}}
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
		{"testdata/console-before-first-observation.golden.txt", beforeFirstRun()},
		{"testdata/console-in-install-window.golden.txt", inInstallRun()},
		{"testdata/console-install-decided.golden.txt", installDecidedRun()},
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
		{"a probe that did not stop", passRun, func(r *Run) { r.Probes[0].StoppedWhenAsked = new(false) },
			"status 200-299\n            the probe did not stop when asked; checks still in flight were not reported\n"},
		{"variables taken out", passRun, func(r *Run) { r.RemovedVariables = []string{"HELM_DRIVER", "HELM_NAMESPACE"} },
			"slowest 44 ms, behind 9 ms\ntaken out of the commands' environment: HELM_DRIVER, HELM_NAMESPACE\n\nwindow"},
		{"the settle time started again", passRun, func(r *Run) {
			r.Install.SettleRestartedBy = []string{"no workload was watched 8.0 s after the install was ready"}
		}, "            slowest 44 ms, behind 9 ms\n            the settle time started again: no workload was watched"},
		{"an install window judged late: its line", failRun, func(r *Run) {
			r.Verdict, r.Transitions = "not-verified", nil
			r.Windows[0].Result, r.Windows[1] = "not-judged", Window{Name: "deploy", Result: "not-run"}
			r.Reasons = []Reason{{Code: "probe-line-invalid", Verdict: "not-verified", Window: "install", Text: "the line is not one JSON object", Decided: true}}
		}, "install     settled after 15.0 s under the probe; NOT VERIFIED\n"},
		{"an install window judged late: the first table is there", failRun, func(r *Run) {
			r.Verdict, r.Transitions = "not-verified", nil
			r.Windows[0].Result, r.Windows[1] = "not-judged", Window{Name: "deploy", Result: "not-run"}
			r.Reasons = []Reason{{Code: "probe-line-invalid", Verdict: "not-verified", Window: "install", Text: "the line is not one JSON object", Decided: true}}
		}, "ms   not judged\ndeploy "},
		{"a reason with no window decided, and the install window did not", installDecidedRun, func(r *Run) {
			r.Reasons = []Reason{{Code: "workload-unsupported", Verdict: "not-verified", Text: "daemonset/agent updates with OnDelete", Decided: true}}
		}, "install     settled after 15.0 s under the probe; failures not judged\n"},
		{"no check in the install window", inInstallRun, func(r *Run) {
			r.Install.Checks, r.Install.Failed, r.Install.FailedByClass, r.Install.LastFailure = 0, 0, nil, nil
		}, "            0 checks, 0 failed\n            slowest -, behind -\n"},
		{"one check in the install window", inInstallRun, func(r *Run) {
			r.Install.Checks, r.Install.Failed, r.Install.FailedByClass, r.Install.LastFailure = 1, 0, nil, nil
		}, "            1 check, 0 failed\n            slowest 44 ms, behind 9 ms\n"},
		{"every check failed in the install window", passRun, func(r *Run) {
			r.Install.Failed, r.Install.FailedByClass, r.Install.LastFailure = 3000, map[string]int{"503": 3000}, at(-100)
		}, "            3,000 checks, 3,000 failed (503 3,000), the last 0.1 s before the steady window\n            slowest -, behind 9 ms\n"},
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

// A probe whose stop the tool did not judge is not blamed: its note is printed, and no line says
// that it did not stop when asked. A probe that did not stop is.
func TestConsoleProbeNotAsked(t *testing.T) {
	const note = "the run was interrupted, so the tool did not ask the probe to stop"
	const line = "did not stop when asked"
	r := passRun()
	r.Probes[0].StoppedWhenAsked = nil
	r.Probes[0].Notes = []string{note}
	got := Console(r)
	if strings.Contains(got, line) {
		t.Errorf("Console says %q of a probe that was not asked:\n%s", line, got)
	}
	if !strings.Contains(got, "status 200-299\n            "+note+"\n") {
		t.Errorf("Console does not print the note under the probe:\n%s", got)
	}
	r.Probes[0].StoppedWhenAsked, r.Probes[0].Notes = new(false), nil
	if got := Console(r); !strings.Contains(got, line) {
		t.Errorf("Console does not say %q of a probe that did not stop:\n%s", line, got)
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

// A pass with a deploy and no rollback says that the candidate's shutdown and the installed
// release's start-up were not exercised (plan ruling 13), first under "not exercised"; a pass with
// a rollback says nothing new.
func TestConsoleDeployWithoutRollback(t *testing.T) {
	const line = "the candidate's shutdown and the installed release's start-up, because the run has no rollback"
	r := passRun()
	r.Windows = r.Windows[:2]
	r.Transitions = r.Transitions[:1]
	got := Console(r)
	want := "not exercised\n  " + line + "\n  " + strings.Join(notExercised, "\n  ") + "\n"
	if !strings.Contains(got, want) {
		t.Errorf("Console has no\n%s\nin\n%s", want, got)
	}
	if strings.Contains(got, "  rollback ") {
		t.Errorf("Console has a rollback line in\n%s", got)
	}
	if pass := Console(passRun()); strings.Contains(pass, line) {
		t.Errorf("a pass with a rollback holds %q:\n%s", line, pass)
	}
	for _, n := range []string{"fail", "not-verified"} {
		f := r
		f.Verdict = n
		if strings.Contains(Console(f), line) {
			t.Errorf("a %s run holds %q", n, line)
		}
	}
}

// A rollback window that did not run is not a rollback the run exercised: a
// deploy-only pass says so, and "exercised" has no rollback line.
func TestConsoleDeployWithoutRollbackThatRan(t *testing.T) {
	r := passRun()
	r.Windows[2] = Window{Name: "rollback", Result: "not-run"}
	r.Transitions = r.Transitions[:1]
	got := Console(r)
	want := "not exercised\n  the candidate's shutdown and the installed release's start-up, because the run has no rollback\n  " + notExercised[0] + "\n"
	if !strings.Contains(got, want) {
		t.Errorf("Console has no\n%s\nin\n%s", want, got)
	}
	if strings.Contains(got, "  rollback ") {
		t.Errorf("Console has a rollback line in\n%s", got)
	}
	// A transition whose window did not run prints no line either.
	r = passRun()
	r.Windows[2] = Window{Name: "rollback", Result: "not-run"}
	if got := Console(r); strings.Contains(got, "  rollback ") {
		t.Errorf("Console has a rollback line for a window that did not run:\n%s", got)
	}
}

// The word of a window with a not-verified result, in the window table.
func TestConsoleNotVerifiedWindow(t *testing.T) {
	r := failRun()
	r.Windows[1].Result = "not-verified"
	if got := Console(r); !strings.Contains(got, "   NOT VERIFIED\n") {
		t.Errorf("Console has no NOT VERIFIED result in\n%s", got)
	}
}

func TestClean(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"plain text and spaces", "a  b c", "a  b c"},
		{"unicode text", "café 世", "café 世"},
		{"LF", "a\nb", "a b"},
		{"CR LF is one space", "a\r\nb", "a b"},
		{"CR", "a\rb", "a b"},
		{"tab", "a\tb", "a b"},
		{"VT", "a\vb", "a b"},
		{"FF", "a\fb", "a b"},
		{"ESC", "a\x1b[2Ab", "a [2Ab"},
		{"BEL", "a\ab", "a b"},
		{"NUL", "a\x00b", "a b"},
		{"DEL", "a\x7fb", "a b"},
		{"C1 low", "a\u0080b", "a b"},
		{"NEL", "a\u0085b", "a b"},
		{"C1 high", "a\u009fb", "a b"},
		{"line separator", "a b", "a b"},
		{"paragraph separator", "a b", "a b"},
		{"bidi embedding", "a‪b‮b", "a b b"},
		{"bidi isolate", "a⁦b⁩b", "a b b"},
		{"empty", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := clean(c.in); got != c.want {
				t.Errorf("clean(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// Two classes whose names clean to one name are one class on the console: their counts are added,
// so that they still sum to the failed checks.
func TestCleanClassesAddsCountsThatCleanToOneName(t *testing.T) {
	got := cleanClasses(map[string]int{"time\nout": 2, "time\tout": 3, "time out": 4, "refused": 1})
	want := map[string]int{"time out": 9, "refused": 1}
	if !maps.Equal(got, want) {
		t.Errorf("cleanClasses = %v, want %v", got, want)
	}
}

// Text from the cluster, the commands and the configuration cannot redraw or
// split the verdict line or any other line.
func TestConsoleCleansEveryField(t *testing.T) {
	const bad = "\x1b[2J\a\v\f\u0085 ‮\t"
	r := passRun()
	r.Name = "or" + bad + "ders"
	r.Releases[1].Source = "./ch" + bad + "art"
	r.Probes[0].Target = "GET http://o" + bad + "rders"
	r.Probes[0].Notes = []string{"no" + bad + "te"}
	r.Install.FailedByClass = map[string]int{"re" + bad + "fused": 6}
	r.Install.SettleRestartedBy = []string{"res" + bad + "tart"}
	r.Transitions[0].Workloads[0].Workload = "deployment/or" + bad + "ders"
	r.Warnings = []string{"war" + bad + "ning"}
	r.Verdict = "fail"
	r.Reasons = []Reason{{Code: "probe-failures", Verdict: "fail", Window: "deploy", Text: "te" + bad + "xt",
		Detail: []string{"de" + bad + "tail"}, Decided: true}}
	got := Console(r)
	for _, c := range strings.TrimPrefix(bad, "\x1b[2J") {
		if strings.ContainsRune(got, c) {
			t.Errorf("Console holds %U", c)
		}
	}
	for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if strings.ContainsAny(line, "\r\v\f\x1b") {
			t.Errorf("line %q holds a control character", line)
		}
	}
	if strings.Contains(got, "\x1b") || !strings.HasPrefix(got, "rollproof: FAIL   or ") || strings.Count(got, "rollproof: ") != 1 {
		t.Errorf("line 1 is not the verdict line:\n%s", got)
	}
}
