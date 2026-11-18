//go:build e2e

package e2e

import (
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
