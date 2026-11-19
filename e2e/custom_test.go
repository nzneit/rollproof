//go:build e2e

package e2e

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixtureProbe returns a custom probe of the fixture's own.
//
// The fixture probe, rollproof-fixture-probe:dev, with env; TARGET is set
// to http://orders.orders.svc beside the given entries.
func fixtureProbe(env map[string]string) *customProbe {
	all := map[string]string{"TARGET": "http://orders.orders.svc"}
	maps.Copy(all, env)
	return &customProbe{image: "probe", env: all}
}

// tcpProbe is the custom probe of rows 15, 18 and 30.
//
// The TCP loop of section 6 against the fixture's Service and port, in
// busybox 1.37.0 from its archive.
var tcpProbe = &customProbe{image: "busybox", command: []string{"sh", "-c", tcpLoop}}

// restrictProbeNamespace is the setup of row 30.
//
// The setup command that makes the probe's namespace enforce the restricted
// Pod Security level at its latest version.
var restrictProbeNamespace = [][]string{{"kubectl", "label", "namespace", "rollproof-probe", "pod-security.kubernetes.io/enforce=restricted"}}

// checkReasonWindow checks where a reason was raised.
//
// Fails the test unless rec has a reason with code, and every reason with
// code has that window.
func checkReasonWindow(t *testing.T, rec record, code, window string) {
	t.Helper()
	found := false
	for _, reason := range rec.Reasons {
		if reason.Code != code {
			continue
		}
		found = true
		if reason.Window != window {
			t.Errorf("the reason %s is in the window %q, want %s: %+v", code, reason.Window, window, reason)
		}
	}
	if !found {
		t.Errorf("result.json has no reason %s: %+v", code, rec.Reasons)
	}
}

// checkSetupOutput checks what the first setup command wrote.
//
// Fails the test unless the evidence of the first setup command,
// evidence/commands/setup in the test's results folder, holds want.
func checkSetupOutput(t *testing.T, want string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(outDir(t), "evidence", "commands", "setup"))
	if err != nil {
		t.Fatalf("the setup command's evidence: %v", err)
	}
	if !strings.Contains(string(b), want) {
		t.Errorf("evidence/commands/setup does not hold %q:\n%s", want, b)
	}
}

// logInstallFailures logs the figure that section 14, point 7, asks every run to record: the
// install window's failures and how long before the steady window the last of them came.
func logInstallFailures(t *testing.T, rec record) {
	t.Helper()
	in := rec.Install
	switch {
	case in == nil:
		t.Logf("install window: none recorded")
	case in.LastFailure == nil || in.Settled == nil:
		t.Logf("install window: %d of %d checks failed, the last at %v, settled at %v", in.Failed, in.Checks, in.LastFailure, in.Settled)
	default:
		t.Logf("install window: %d of %d checks failed, the last %v before the steady window",
			in.Failed, in.Checks, in.Settled.Sub(*in.LastFailure))
	}
}

// Row 6. A custom probe stops writing when it first reads the candidate's version from the Service,
// and never writes again: not verified, probe-silent in the deploy's window.
func TestCustomProbeGoesSilentOnTheCandidate(t *testing.T) {
	f := fixture{name: "custom-probe-goes-silent-on-the-candidate",
		custom: fixtureProbe(map[string]string{"VERSION": "candidate", "ON_VERSION": "silent"})}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 2, verdict: "not-verified", codes: []string{"probe-silent"}, decided: "probe-silent",
			windows: map[string]string{"steady": "pass", "deploy": "not-verified", "rollback": "not-run"}})
		checkReasonWindow(t, readRecord(t, outDir(t)), "probe-silent", "deploy")
	})
}

// Row 7. A custom probe exits when it first reads the candidate's version from the Service: not
// verified, probe-exited in the deploy's window.
func TestCustomProbeExitsOnTheCandidate(t *testing.T) {
	f := fixture{name: "custom-probe-exits-on-the-candidate",
		custom: fixtureProbe(map[string]string{"VERSION": "candidate", "ON_VERSION": "exit"})}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 2, verdict: "not-verified", codes: []string{"probe-exited"}, decided: "probe-exited",
			windows: map[string]string{"steady": "pass", "deploy": "not-verified", "rollback": "not-run"}})
		checkReasonWindow(t, readRecord(t, outDir(t)), "probe-exited", "deploy")
	})
}

// Row 15. A custom probe, the TCP loop of section 6 pointed at the fixture's own Service and port,
// judges the first fixture: pass. With a custom probe the tool checks no target, so it cannot say
// which workloads the probe reaches.
func TestTCPLoopJudgesTheFirstFixture(t *testing.T) {
	f := fixture{name: "tcp-loop-judges-the-first-fixture", custom: tcpProbe}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass", windows: allPass})
		rec := readRecord(t, outDir(t))
		if len(rec.Probes) != 1 || rec.Probes[0].Kind != "custom" {
			t.Errorf("probes %+v; want one, of kind custom", rec.Probes)
		}
		if w := rec.workload(t, "deploy", "deployment/orders"); w.Probed != "not known" || w.PodsReplaced != 2 {
			t.Errorf("deploy: deployment/orders probed %q, replaced %d; want not known and 2", w.Probed, w.PodsReplaced)
		}
	})
}

// Row 16. The same check, as a small program that first reports a failed check every 2 ms for 5 s
// by its own clock, and only then checks the Service: pass; the install record holds at least
// 2,000 failures, the last of them at least 9 s before the steady window, and neither the steady
// window nor a transition's holds one.
func TestProbeFailsItsFirstFiveSeconds(t *testing.T) {
	f := fixture{name: "probe-fails-its-first-five-seconds", custom: fixtureProbe(map[string]string{"FAIL_FIRST": "5s"})}
	underBothHelms(t, f, func(t *testing.T, r result) {
		rec := readRecord(t, outDir(t))
		in := rec.Install
		if in == nil || len(in.SettleRestartedBy) != 0 {
			t.Fatalf("install %+v; want a record whose settle time did not start again", in)
		}
		expect(t, r, result{exit: 0, verdict: "pass", windows: allPass})
		if in.Failed < 2000 || in.Settled == nil || in.LastFailure == nil || in.Settled.Sub(*in.LastFailure) < 9*time.Second {
			t.Errorf("install: %d failed, the last at %v, settled at %v; want 2000 or more, the last 9 s or more before", in.Failed, in.LastFailure, in.Settled)
		}
		for _, name := range []string{"steady", "deploy", "rollback"} {
			if w := rec.window(t, name); w.Failed != 0 {
				t.Errorf("%s: %d failed, want none", name, w.Failed)
			}
		}
		logInstallFailures(t, rec)
	})
}

// Row 17. The same, with failed checks for its first 20 s: not verified, steady-not-clean; the
// failures from 15 s after its first observation on fall in the steady window.
func TestProbeFailsItsFirstTwentySeconds(t *testing.T) {
	f := fixture{name: "probe-fails-its-first-twenty-seconds", custom: fixtureProbe(map[string]string{"FAIL_FIRST": "20s"})}
	underBothHelms(t, f, func(t *testing.T, r result) {
		rec := readRecord(t, outDir(t))
		in := rec.Install
		if in == nil || len(in.SettleRestartedBy) != 0 {
			t.Fatalf("install %+v; want a record whose settle time did not start again", in)
		}
		expect(t, r, result{exit: 2, verdict: "not-verified", codes: []string{"steady-not-clean"}, decided: "steady-not-clean",
			windows: map[string]string{"steady": "not-verified", "deploy": "not-run", "rollback": "not-run"}})
		s := rec.window(t, "steady")
		if in.FirstObservation == nil || s.Start == nil {
			t.Fatalf("install %+v, steady %+v; want a first observation and a steady start", in, s)
		}
		if d := s.Start.Sub(*in.FirstObservation); d < 15*time.Second || d >= 20*time.Second {
			t.Errorf("the steady window began %v after the first observation, want from 15 s to under 20 s", d)
		}
		if in.Failed == 0 || s.Failed == 0 || s.FirstFailure == nil || s.FirstFailure.Before(*s.Start) {
			t.Errorf("install %d failed; steady %d failed, the first at %v; want failures in both", in.Failed, s.Failed, s.FirstFailure)
		}
		logInstallFailures(t, rec)
	})
}

// Row 18. The TCP loop, as a custom probe, judges a candidate with no readiness probe: fail, in the
// deploy window.
func TestTCPLoopJudgesACandidateWithoutReadiness(t *testing.T) {
	f := fixture{name: "tcp-loop-judges-a-candidate-without-readiness", custom: tcpProbe,
		candidate: map[string]any{"readiness": false, "startDelay": "5s"}}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 1, verdict: "fail", codes: []string{"probe-failures"}, decided: "probe-failures", windows: failedDeploy})
		rec := readRecord(t, outDir(t))
		if len(rec.Probes) != 1 || rec.Probes[0].Kind != "custom" {
			t.Errorf("probes %+v; want one, of kind custom", rec.Probes)
		}
		checkReasonWindow(t, rec, "probe-failures", "deploy")
		if d := rec.window(t, "deploy"); d.Failed == 0 {
			t.Errorf("deploy: %d failed, want the TCP loop's failures", d.Failed)
		}
	})
}

// Row 27. A custom probe ignores SIGTERM: pass; the report says that the probe did not stop.
func TestCustomProbeIgnoresSIGTERM(t *testing.T) {
	f := fixture{name: "custom-probe-ignores-sigterm", custom: fixtureProbe(map[string]string{"IGNORE_TERM": "true"})}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass", windows: allPass})
		p := readRecord(t, outDir(t)).Probes
		if len(p) != 1 || p[0].StoppedWhenAsked == nil || *p[0].StoppedWhenAsked {
			t.Errorf("probes %+v; want one whose stopped_when_asked is false", p)
		}
	})
}

// Row 30. A setup command makes the probe's namespace enforce the restricted level, and the TCP
// loop of section 6, as a custom probe, judges the run: pass.
func TestTCPLoopInARestrictedProbeNamespace(t *testing.T) {
	f := fixture{name: "tcp-loop-in-a-restricted-probe-namespace", custom: tcpProbe, setup: restrictProbeNamespace}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass", windows: allPass})
		checkSetupOutput(t, "namespace/rollproof-probe labeled")
		if p := readRecord(t, outDir(t)).Probes; len(p) != 1 || p[0].Kind != "custom" {
			t.Errorf("probes %+v; want one, of kind custom", p)
		}
	})
}
