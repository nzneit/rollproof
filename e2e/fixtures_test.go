//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// underBothHelms runs f once with each Helm major, as two subtests, and hands
// each result to check.
func underBothHelms(t *testing.T, f fixture, check func(t *testing.T, r result)) {
	for _, helm := range []string{"helm-3", "helm-4"} {
		t.Run(helm, func(t *testing.T) {
			g := f
			g.helm = helm
			check(t, runFixture(t, g))
		})
	}
}

// expect compares what a run gave with what its row of section 12 expects.
func expect(t *testing.T, got, want result) {
	t.Helper()
	if diff := cmp.Diff(want, got, cmp.AllowUnexported(result{}), cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("result (-want +got):\n%s", diff)
	}
}

// Both releases report ready only when they can serve, and after SIGTERM
// serve on and then drain: pass.
func TestFirstFixture(t *testing.T) {
	underBothHelms(t, fixture{name: "first"}, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass",
			windows: map[string]string{"steady": "pass", "deploy": "pass", "rollback": "pass"}})
		seen := 0
		for _, tr := range readTransitions(t, outDir(t)) {
			for _, w := range tr.Workloads {
				if w.Workload != "deployment/orders" {
					continue
				}
				seen++
				if w.PodsReplaced != 2 || w.Probed != "yes" || w.QuickestStopMs == nil || *w.QuickestStopMs < 5000 {
					t.Errorf("%s: deployment/orders replaced %d, probed %q, quickest stop %v ms; want 2, yes, 5000 or more",
						tr.Name, w.PodsReplaced, w.Probed, w.QuickestStopMs)
				}
			}
		}
		if seen != 2 {
			t.Errorf("deployment/orders is in %d transitions, want 2", seen)
		}
		got := readInstall(t, outDir(t))
		in, steady := got.Install, got.Windows[0]
		if in.FirstObservation == nil || in.Settled == nil || in.Checks == 0 {
			t.Fatalf("install %+v; want a first observation, a settling and checks", in)
		}
		if d := in.Settled.Sub(*in.FirstObservation); d < 15*time.Second {
			t.Errorf("the install settled %v after the first observation, want limits.settle, 15s, or more", d)
		}
		if steady.Start == nil || !steady.Start.Equal(*in.Settled) {
			t.Errorf("the steady window starts at %v, want the moment the install had settled, %v", steady.Start, in.Settled)
		}
		// The figure that section 14, points 7 and 23 ask every run to record.
		if in.LastFailure == nil {
			t.Logf("install window: %d checks, none failed", in.Checks)
		} else {
			t.Logf("install window: %d of %d checks failed %v, the last %v before the steady window",
				in.Failed, in.Checks, in.FailedByClass, in.Settled.Sub(*in.LastFailure))
		}
	})
}

// The candidate has no readiness probe: fail in the deploy window; the
// rollback is not run.
func TestCandidateWithoutReadiness(t *testing.T) {
	f := fixture{name: "candidate-without-readiness",
		candidate: map[string]any{"readiness": false, "startDelay": "5s"}}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 1, verdict: "fail", codes: []string{"probe-failures"}, decided: "probe-failures",
			windows: map[string]string{"steady": "pass", "deploy": "fail", "rollback": "not-run"}})
	})
}

// The candidate exits on SIGTERM: fail in the rollback window.
func TestCandidateExitsOnSIGTERM(t *testing.T) {
	f := fixture{name: "candidate-exits-on-sigterm", candidate: map[string]any{"shutdown": "exit"}}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 1, verdict: "fail", codes: []string{"probe-failures"}, decided: "probe-failures",
			windows: map[string]string{"steady": "pass", "deploy": "pass", "rollback": "fail"}})
	})
}

// No node is free for the extra pod: fail, the deploy stalls, and
// pod-unschedulable is listed beside the stall.
func TestNoRoomForTheExtraPod(t *testing.T) {
	f := fixture{name: "no-room-for-the-extra-pod",
		previous:  map[string]any{"replicas": 3},
		candidate: map[string]any{"replicas": 3},
		workers:   3,
		limits:    map[string]string{"timeout": "90s"}}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 1, verdict: "fail",
			codes: []string{"pod-unschedulable", "transition-stalled"}, decided: "transition-stalled",
			windows: map[string]string{"steady": "pass", "deploy": "fail", "rollback": "not-run"}})
	})
}

// The previous release's pods report ready, and its handler fails every request: not verified,
// steady-not-clean, within a minute of the install; the text says that no check has passed since
// the probe started.
func TestPreviousFailsEveryRequest(t *testing.T) {
	f := fixture{name: "previous-fails-every-request", previous: map[string]any{"failWork": true}}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 2, verdict: "not-verified", codes: []string{"steady-not-clean"}, decided: "steady-not-clean",
			windows: map[string]string{"steady": "not-verified", "deploy": "not-run", "rollback": "not-run"}})
		got := readInstall(t, outDir(t))
		in, steady := got.Install, got.Windows[0]
		// The handler answers 500. The first checks may instead have been refused or timed out while
		// the cluster began to route to the new Service (section 7).
		if in.Checks == 0 || in.Failed != in.Checks || in.FailedByClass["500"] == 0 {
			t.Errorf("install %+v; want every check of the install window failed, and 500 among the classes", in)
		}
		if steady.End == nil || steady.End.Sub(in.Ready) > time.Minute {
			t.Errorf("the steady window ended at %v, want within a minute of the install being ready at %v", steady.End, in.Ready)
		}
		said := false
		for _, reason := range got.Reasons {
			for _, line := range reason.Detail {
				said = said || (reason.Code == "steady-not-clean" && strings.HasPrefix(line, "no check has passed since the probe started"))
			}
		}
		if !said {
			t.Errorf("the reasons do not say that no check has passed since the probe started: %+v", got.Reasons)
		}
	})
}

// allPass is the window results of a run that passed with a deploy and a rollback.
var allPass = map[string]string{"steady": "pass", "deploy": "pass", "rollback": "pass"}

// failedDeploy is the window results of a run whose deploy failed, so that its rollback was
// not run (rule 1 of section 10).
var failedDeploy = map[string]string{"steady": "pass", "deploy": "fail", "rollback": "not-run"}

// oldestNodeImage is the oldest node image that kind v0.33.0 lists, from
// docs/proofs/p09-kind-release.md, on which the first fixture also runs.
const oldestNodeImage = "kindest/node:v1.34.11@sha256:44e222ee2132dab25ff87301682f89eb82c7880ea3a1bf543bfe9708fd08d67d"

// The first fixture on the oldest node image that the linked kind release lists: pass.
func TestFirstFixtureOnTheOldestNodeImage(t *testing.T) {
	f := fixture{name: "first-on-the-oldest-node-image", nodeImage: oldestNodeImage}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass", windows: allPass})
		c := readRecord(t, outDir(t)).Cluster
		if c.Kubernetes != "v1.34.11" || c.NodeImage != oldestNodeImage {
			t.Errorf("cluster %+v; want Kubernetes v1.34.11 on %s", c, oldestNodeImage)
		}
	})
}

// Row 5. The previous release exits on SIGTERM: fail, in the deploy window; the rollback is not run.
func TestPreviousExitsOnSIGTERM(t *testing.T) {
	f := fixture{name: "previous-exits-on-sigterm", previous: map[string]any{"shutdown": "exit"}}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 1, verdict: "fail", codes: []string{"probe-failures"}, decided: "probe-failures", windows: failedDeploy})
	})
}

// Row 8. The deploy replaces no pod: not verified.
func TestDeployReplacesNoPod(t *testing.T) {
	f := fixture{name: "deploy-replaces-no-pod", candidateRunsPrevious: true}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 2, verdict: "not-verified", codes: []string{"no-pod-replaced"}, decided: "no-pod-replaced",
			windows: map[string]string{"steady": "pass", "deploy": "not-verified", "rollback": "not-run"}})
	})
}

// Row 9. The previous release never becomes ready: not verified. The run ends before the install
// window, so result.json lists no window.
func TestPreviousNeverReady(t *testing.T) {
	f := fixture{name: "previous-never-ready", previous: map[string]any{"startDelay": "1h"},
		limits: map[string]string{"timeout": "90s"}}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 2, verdict: "not-verified", codes: []string{"install-failed"}, decided: "install-failed"})
	})
}

// Row 11. No previous release is named: pass, reported as restart only.
func TestNoPreviousRelease(t *testing.T) {
	f := fixture{name: "no-previous-release", noPrevious: true}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass", windows: map[string]string{"steady": "pass", "restart": "pass"}})
		rec := readRecord(t, outDir(t))
		tr := rec.transition(t, "restart")
		if string(tr.Command) != "null" || tr.Restart == nil || *tr.Restart != "accepted" {
			t.Errorf("restart: command %s, restart %v; want null and accepted", tr.Command, tr.Restart)
		}
		if w := rec.workload(t, "restart", "deployment/orders"); w.PodsReplaced != 2 {
			t.Errorf("the restart replaced %d pods of deployment/orders, want 2", w.PodsReplaced)
		}
		if len(rec.Transitions) != 1 || len(rec.Releases) != 1 || rec.Releases[0].Role != "candidate" {
			t.Errorf("transitions %+v, releases %+v; want the restart alone, and the candidate alone", rec.Transitions, rec.Releases)
		}
		if c := readConsole(t); !strings.Contains(c, "  a deploy to another release and a rollback, because only a restart was tested\n") {
			t.Errorf("the console does not say that only a restart was tested:\n%s", c)
		}
	})
}

// Row 12. Both releases serve for 5 s after SIGTERM and then end without draining: pass, a known
// miss of the built-in probe (section 6). A failure here means that the limit has moved.
func TestBothReleasesEndWithoutDraining(t *testing.T) {
	values := map[string]any{"serveAfterTerm": "5s", "drainDeadline": "0s"}
	f := fixture{name: "both-releases-end-without-draining", previous: values, candidate: values}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass", windows: allPass})
		seen := 0
		for _, tr := range readTransitions(t, outDir(t)) {
			for _, w := range tr.Workloads {
				if w.Workload != "deployment/orders" {
					continue
				}
				seen++
				if w.QuickestStopMs == nil || *w.QuickestStopMs < 5000 {
					t.Errorf("%s: deployment/orders quickest stop %v ms; want 5000 or more", tr.Name, w.QuickestStopMs)
				}
			}
		}
		if seen != 2 {
			t.Errorf("deployment/orders is in %d transitions, want 2", seen)
		}
	})
}

// Row 13. A second rollout starts 5 s after the tool saw the deploy's first one finish: pass; the
// deploy's window covers both rollouts, and result.json says what started the settle time again.
func TestSecondRolloutAfterTheFirstFinished(t *testing.T) {
	var restarted time.Time
	f := fixture{name: "second-rollout-after-the-first-finished",
		act: restartAfterTheDeploysFirstFinish(5*time.Second, &restarted)}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass", windows: allPass})
		rec := readRecord(t, outDir(t))
		tr := rec.transition(t, "deploy")
		if len(tr.SettleRestartedBy) == 0 || !strings.HasPrefix(tr.SettleRestartedBy[0], "the spec of deployment/orders changed") {
			t.Errorf("deploy: settle_restarted_by %q; want first the spec of deployment/orders", tr.SettleRestartedBy)
		}
		if w := rec.workload(t, "deploy", "deployment/orders"); w.PodsCreated != 4 {
			t.Errorf("the deploy created %d pods of deployment/orders, want 4: two by the deploy, two by the restart", w.PodsCreated)
		}
		marks, err := readMarks(filepath.Join(outDir(t), "evidence", "windows.json"))
		if err != nil {
			t.Fatal(err)
		}
		m := findMark(marks, "deploy")
		if m == nil {
			t.Fatalf("evidence/windows.json has no deploy mark: %+v", marks)
		}
		if len(m.Finishes) < 2 || !m.Finishes[len(m.Finishes)-1].After(restarted) {
			t.Errorf("deploy: finishes %v, restart at %v; want two or more, the last after the restart", m.Finishes, restarted)
		}
		if d := rec.window(t, "deploy"); d.End == nil || !d.End.After(restarted) {
			t.Errorf("the deploy window ended at %v, before the restart at %v", d.End, restarted)
		}
	})
}

// Row 14. The chart denies traffic by default, and the test's values let the probe in: pass.
// The control comes first (decision 24): the same chart with the policy that denies and without
// the rule that lets the probe in must not pass, or the row would pass whether or not kind's
// network plugin enforces the policy. The control runs once, under Helm 3: it tests the cluster's
// network plugin, not Helm. A control that does not give its expected result stops the test
// before the row runs.
func TestChartDeniesTrafficByDefault(t *testing.T) {
	controlled := t.Run("control", func(t *testing.T) {
		denied := map[string]any{"denyIngress": true, "allowProbe": false}
		r := runFixture(t, fixture{name: "chart-denies-traffic-control", previous: denied, candidate: denied, helm: "helm-3"})
		if r.verdict == "pass" {
			t.Fatal("the control passed: kind's network plugin does not enforce the policy, so row 14 shows nothing; stop for the maintainer (decision 24)")
		}
		expect(t, r, result{exit: 2, verdict: "not-verified", codes: []string{"steady-not-clean"}, decided: "steady-not-clean",
			windows: map[string]string{"steady": "not-verified", "deploy": "not-run", "rollback": "not-run"}})
	})
	if !controlled {
		t.Fatal("the control did not give steady-not-clean, so the row is not run; stop for the maintainer (decision 24)")
	}
	values := map[string]any{"denyIngress": true, "allowProbe": true}
	f := fixture{name: "chart-denies-traffic-by-default", previous: values, candidate: values}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass", windows: allPass})
	})
}

// Row 19. The workload is a StatefulSet behind a Service with a cluster address: pass.
func TestStatefulSet(t *testing.T) {
	values := map[string]any{"workload": "StatefulSet"}
	f := fixture{name: "statefulset", previous: values, candidate: values}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass", windows: allPass})
		rec := readRecord(t, outDir(t))
		for _, name := range []string{"deploy", "rollback"} {
			if w := rec.workload(t, name, "statefulset/orders"); w.PodsReplaced != 2 {
				t.Errorf("%s: statefulset/orders replaced %d pods, want 2", name, w.PodsReplaced)
			}
		}
	})
}

// Row 20. The workload is a DaemonSet behind a Service with a cluster address: pass.
func TestDaemonSet(t *testing.T) {
	values := map[string]any{"workload": "DaemonSet"}
	f := fixture{name: "daemonset", previous: values, candidate: values}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass", windows: allPass})
		rec := readRecord(t, outDir(t))
		for _, name := range []string{"deploy", "rollback"} {
			if w := rec.workload(t, name, "daemonset/orders"); w.PodsReplaced != 3 {
				t.Errorf("%s: daemonset/orders replaced %d pods, want 3, one per worker", name, w.PodsReplaced)
			}
		}
	})
}

// Row 23. The chart has a hook after the install and one before the upgrade, each a Job that runs
// longer than the settle time: pass; no window closes while Helm runs. Helm exits only after the
// post-install Job has completed, and the install is not ready until Helm has exited (section 7),
// so the install's ready time in result.json comes at or after that Job's completion event in
// evidence/events.txt; and the deploy's first finish comes 20 s or more after its start, the
// pre-upgrade hook's run.
func TestHooksLongerThanTheSettleTime(t *testing.T) {
	values := map[string]any{"hooks": true, "hookSleep": "20s"}
	f := fixture{name: "hooks-longer-than-the-settle-time", previous: values, candidate: values}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass", windows: allPass})
		rec := readRecord(t, outDir(t))
		if rec.Install == nil {
			t.Fatal("result.json has no install")
		}
		// The event's time is a whole second, truncated by the API server, and the ready time is
		// truncated to the millisecond; neither is rounded up. In a correct run the true ready time
		// is at or after the true completion, and truncation keeps that order, because a second is
		// a whole number of milliseconds, so a correct run never fails the check. The coarser
		// second makes it easier to meet: it proves that the install was not ready more than a
		// second before the hook completed, not that it was not ready in the hook's last second.
		completed := jobCompleted(t, outDir(t), "orders", "orders-post-install")
		if rec.Install.Ready.Before(completed) {
			t.Errorf("install ready %s, before the post-install Job completed at %s",
				rec.Install.Ready.Format(time.RFC3339Nano), completed.Format(time.RFC3339Nano))
		}
		marks, err := readMarks(filepath.Join(outDir(t), "evidence", "windows.json"))
		if err != nil {
			t.Fatal(err)
		}
		m := findMark(marks, "deploy")
		if m == nil {
			t.Fatalf("evidence/windows.json has no deploy mark: %+v", marks)
		}
		// Helm runs the pre-upgrade hook, 20 s, before it changes anything.
		if m.Start == nil || len(m.Finishes) == 0 || m.Finishes[0].Sub(*m.Start) < 20*time.Second {
			t.Errorf("deploy: start %v, finishes %v; want the first finish 20 s or more after the start", m.Start, m.Finishes)
		}
	})
}

// jobCompleted returns the time of the latest event with reason Completed about job/name in
// namespace, read from out/evidence/events.txt, whose lines are tab-separated: the time in RFC
// 3339, the namespace, the type, the reason, the object as kind/name, the count and the message
// (watch.WriteEvents). It fails the test when the file cannot be read, a matching line's time
// cannot be read, or no line matches.
func jobCompleted(t *testing.T, out, namespace, name string) time.Time {
	t.Helper()
	path := filepath.Join(out, "evidence", "events.txt")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the events: %v", err)
	}
	var latest time.Time
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, "\t")
		if len(f) < 7 || f[1] != namespace || f[3] != "Completed" || f[4] != "job/"+name {
			continue
		}
		at, err := time.Parse(time.RFC3339, f[0])
		if err != nil {
			t.Fatalf("%s: the time of %q: %v", path, line, err)
		}
		if at.After(latest) {
			latest = at
		}
	}
	if latest.IsZero() {
		t.Fatalf("%s has no Completed event of job/%s in namespace %s", path, name, namespace)
	}
	return latest
}

// Row 24. The candidate's pods serve at first and then fail requests for some seconds, beginning
// after the rollout has finished: fail, in the deploy window, counted under "after finish".
// limits.settle is 30 s so that failures 20 s after a pod listens fall after the finish and inside
// the window.
func TestCandidateFailsAfterTheRolloutFinished(t *testing.T) {
	f := fixture{name: "candidate-fails-after-the-rollout-finished",
		candidate: map[string]any{"failAfter": "20s", "failFor": "5s"},
		limits:    map[string]string{"settle": "30s"}}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 1, verdict: "fail", codes: []string{"probe-failures"}, decided: "probe-failures", windows: failedDeploy})
		if d := readRecord(t, outDir(t)).window(t, "deploy"); d.Failed == 0 || d.FailedAfterFinish != d.Failed {
			t.Errorf("deploy: %d failed, %d after finish; want every failure after finish", d.Failed, d.FailedAfterFinish)
		}
	})
}

// Row 25. A value breaks the candidate chart's schema: fail, command-failed.
func TestCandidateValueBreaksTheSchema(t *testing.T) {
	f := fixture{name: "candidate-value-breaks-the-schema", candidate: map[string]any{"replicas": 0}}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 1, verdict: "fail", codes: []string{"command-failed"}, decided: "command-failed", windows: failedDeploy})
		if tr := readRecord(t, outDir(t)).transition(t, "deploy"); string(tr.Command) != `"exit-nonzero"` {
			t.Errorf("deploy: command %s, want \"exit-nonzero\"", tr.Command)
		}
	})
}

// Row 26. The run gets SIGINT during the deploy: not verified, interrupted; result.json is final,
// the cluster stays for the next run, and the tool ends by SIGINT. runFixture checks that the
// file is final and, with keepsCluster, that the cluster stayed; it removes it afterwards.
func TestSIGINTDuringTheDeploy(t *testing.T) {
	f := fixture{name: "sigint-during-the-deploy", act: interruptDuringTheDeploy, keepsCluster: true}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: -1, signal: "interrupt", verdict: "not-verified", codes: []string{"interrupted"}, decided: "interrupted",
			windows: map[string]string{"steady": "pass", "deploy": "not-verified", "rollback": "not-run"}})
	})
}

// Row 28. The candidate changes a second workload only, which the probe does not reach: pass; the
// probed workload reads "not replaced", and the deploy's line under "exercised" says so.
func TestCandidateChangesOnlyAnUnprobedWorkload(t *testing.T) {
	f := fixture{name: "candidate-changes-only-an-unprobed-workload", candidateRunsPrevious: true,
		previous:  map[string]any{"second": true, "secondRevision": "1"},
		candidate: map[string]any{"second": true, "secondRevision": "2"}}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass", windows: allPass})
		rec := readRecord(t, outDir(t))
		if w := rec.workload(t, "deploy", "deployment/orders"); w.PodsReplaced != 0 || w.Probed != "yes" {
			t.Errorf("deploy: deployment/orders replaced %d, probed %q; want 0 and yes", w.PodsReplaced, w.Probed)
		}
		if w := rec.workload(t, "deploy", "deployment/orders-worker"); w.PodsReplaced != 1 || w.Probed != "no" {
			t.Errorf("deploy: deployment/orders-worker replaced %d, probed %q; want 1 and no", w.PodsReplaced, w.Probed)
		}
		want := "  deploy      no probed workload had a pod replaced: the probe watched a rollout that it did not reach\n"
		if c := readConsole(t); !strings.Contains(c, want) {
			t.Errorf("the console does not hold %q:\n%s", want, c)
		}
	})
}
