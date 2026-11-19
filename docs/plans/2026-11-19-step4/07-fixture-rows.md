# Step 4 plan, part 7: the rows of section 12, and the whole suite

The rows below run code that Tasks 1 to 21 built, so none of them has a failing stage of its own: Step 2 of each task checks that the rows build, and Step 3 runs them. Each row is a test that runs once under Helm 3 and once under Helm 4 (`underBothHelms`, decision 21), and takes about four minutes per run. Rows are numbered as section 12's table numbers them: the first row is row 1, and row 31 is the previous release whose handler fails every request. A test's name is a stable contract.

**When a row does not give its expected result.** The expectation comes from section 12; the row is evidence about the code or the design, not about itself. Never change an expected verdict, code, window or record to make a row pass, and never retry a run until it passes. Change the harness or a fixture only where its evidence shows that it does not make what the row describes (a chart that renders something else, an action that came late), and say so in the report. Otherwise keep the results folder (the path the failure prints), and stop for the maintainer with the row, the console text and the evidence that shows the cause. Row 12 says what its failure means: the limit that section 6 states has moved (section 12, "The twelfth row").

### Task 22: The rows with the built-in probe, and the first fixture on the oldest node image

Design: section 12 (the table, rows 5, 8, 9, 11 to 14, 19, 20, 23 to 26 and 28, and the paragraphs under it; "The suite's matrix"); section 10 ("Rules of the verdict", "Reasons"); section 7 ("Pods replaced", "When a transition has finished", "The restart"); section 6 ("What it cannot see"). README decisions 5, 13, 21, 23.

Row 5, the previous release that exits on SIGTERM, is in no earlier step: the first plan built rows 1 to 4 and 31. It is written here.

**Files:**
- Modify: `e2e/fixtures_test.go` (the rows below)

**Interfaces:**
- Consumes: `fixture`'s `nodeImage`, `noPrevious`, `candidateRunsPrevious`, `act` and `keepsCluster`; `restartAfterTheDeploysFirstFinish`, `interruptDuringTheDeploy`, `readRecord`, `readMarks`, `readConsole`, `result.signal` (Task 20); the chart's values `workload`, `denyIngress`, `allowProbe`, `second`, `secondRevision`, `hooks`, `failAfter`, `failFor` (Task 21).
- Produces: `var allPass = map[string]string{"steady": "pass", "deploy": "pass", "rollback": "pass"}`
  The window results of a run that passed with a deploy and a rollback.
- Produces: `var failedDeploy = map[string]string{"steady": "pass", "deploy": "fail", "rollback": "not-run"}`
  The window results of a run whose deploy failed, so that its rollback was
  not run (rule 1 of section 10).
- Produces: `const oldestNodeImage = "kindest/node:v1.34.11@sha256:44e222ee2132dab25ff87301682f89eb82c7880ea3a1bf543bfe9708fd08d67d"`
  The oldest node image that kind v0.33.0 lists, from
  docs/proofs/p09-kind-release.md, on which the first fixture also runs.

- [ ] **Step 1: Write the tests** in `e2e/fixtures_test.go`, each with a comment that gives its row as section 12 words it, as the present tests do, after the two variables of the Interfaces block.

```go
// The first fixture on the oldest node image that the linked kind release lists: pass.
func TestFirstFixtureOnTheOldestNodeImage(t *testing.T) {
	f := fixture{name: "first-on-the-oldest-node-image", nodeImage: oldestNodeImage}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass", windows: allPass})
		c := readRecord(t, outDir(t)).Cluster
		if c == nil || c.Kubernetes != "v1.34.11" || c.NodeImage != oldestNodeImage {
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
		for _, m := range marks {
			if m.Name == "deploy" && (len(m.Finishes) < 2 || !m.Finishes[len(m.Finishes)-1].After(restarted)) {
				t.Errorf("deploy: finishes %v, restart at %v; want two or more, the last after the restart", m.Finishes, restarted)
			}
		}
		if d := rec.window(t, "deploy"); d.End == nil || !d.End.After(restarted) {
			t.Errorf("the deploy window ended at %v, before the restart at %v", d.End, restarted)
		}
	})
}

// Row 14. The chart denies traffic by default, and the test's values let the probe in: pass.
func TestChartDeniesTrafficByDefault(t *testing.T) {
	values := map[string]any{"denyIngress": true, "allowProbe": true}
	f := fixture{name: "chart-denies-traffic-by-default", previous: values, candidate: values}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass", windows: allPass})
	})
}
```

  Rows 19 and 20 follow one shape. `TestStatefulSet` (row 19, "The workload is a StatefulSet behind a Service with a cluster address: pass"): both releases `{"workload": "StatefulSet"}`; pass with `allPass`; in the deploy and the rollback, `rec.workload(t, name, "statefulset/orders").PodsReplaced == 2`. `TestDaemonSet` (row 20, the same for a DaemonSet): both releases `{"workload": "DaemonSet"}`; pass with `allPass`; `daemonset/orders` replaced 3 in each, one per worker.

```go
// Row 23. The chart has a hook after the install and one before the upgrade, each a Job that runs
// longer than the settle time: pass; no window closes while Helm runs.
func TestHooksLongerThanTheSettleTime(t *testing.T) {
	values := map[string]any{"hooks": true, "hookSleep": "20s"}
	f := fixture{name: "hooks-longer-than-the-settle-time", previous: values, candidate: values}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass", windows: allPass})
		marks, err := readMarks(filepath.Join(outDir(t), "evidence", "windows.json"))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range marks {
			// Helm runs the pre-upgrade hook, 20 s, before it changes anything.
			if m.Name == "deploy" && (m.Start == nil || len(m.Finishes) == 0 || m.Finishes[0].Sub(*m.Start) < 20*time.Second) {
				t.Errorf("deploy: start %v, finishes %v; want the first finish 20 s or more after the start", m.Start, m.Finishes)
			}
		}
	})
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
```

- [ ] **Step 2: Check that they build**
  Run: `go vet -tags e2e ./e2e/` / Expected: no output.
- [ ] **Step 3: Run the rows under both Helm majors** (about two hours)
  Run: `env CGO_ENABLED=0 KIND_EXPERIMENTAL_PROVIDER=nerdctl go test -tags e2e -p 1 -count=1 -timeout 360m -v ./e2e/ -run '^(TestFirstFixtureOnTheOldestNodeImage|TestPreviousExitsOnSIGTERM|TestDeployReplacesNoPod|TestPreviousNeverReady|TestNoPreviousRelease|TestBothReleasesEndWithoutDraining|TestSecondRolloutAfterTheFirstFinished|TestChartDeniesTrafficByDefault|TestStatefulSet|TestDaemonSet|TestHooksLongerThanTheSettleTime|TestCandidateFailsAfterTheRolloutFinished|TestCandidateValueBreaksTheSchema|TestSIGINTDuringTheDeploy|TestCandidateChangesOnlyAnUnprobedWorkload)$'` / Expected: PASS, 15 tests, each under `helm-3` and `helm-4`. On a failure, the rule at the top of this file applies. After it, `nerdctl ps -a` lists no container whose name begins `rollproof-e2e-`.
- [ ] **Step 4: Commit**
  `git add e2e/fixtures_test.go` and `git commit -m "Add the fixture rows of the built-in probe, row 5 among them, and the first fixture on the oldest node image"`

### Task 23: The rows with a custom probe

Design: section 12 (rows 6, 7, 15 to 18, 27 and 30, and the paragraphs under the table: the two rows with a probe that fails its first seconds, the sixth and seventh rows' codes and windows, the rows that use small custom probes); section 6 ("The contract": "Lifecycle", "A probe that does not stop", "Where probe pods run"); section 7 ("When the install has settled"). README decisions 8, 9, 19, 20, 23.

**Files:**
- Create: `e2e/custom_test.go` (build tag `e2e`)

**Interfaces:**
- Consumes: `fixture.custom`, `fixture.setup`, `customProbe`, `tcpLoop`, `readRecord` (Task 20); the fixture probe's `TARGET`, `VERSION`, `ON_VERSION`, `FAIL_FIRST`, `IGNORE_TERM` (Task 20); `result.json`'s probe kind `"custom"`, `probed` `"not known"` for a custom probe (Task 15, decisions 8 and 9).
- Produces: `func fixtureProbe(env map[string]string) *customProbe`
  The fixture probe, rollproof-fixture-probe:dev, with env; TARGET is set
  to http://orders.orders.svc beside the given entries.
- Produces: `var tcpProbe = &customProbe{image: "busybox", command: []string{"sh", "-c", tcpLoop}}`
  The TCP loop of section 6 against the fixture's Service and port, in
  busybox 1.37.0 from its archive.
- Produces: `var restrictProbeNamespace = [][]string{{"kubectl", "label", "namespace", "rollproof-probe", "pod-security.kubernetes.io/enforce=restricted"}}`
  The setup command that makes the probe's namespace enforce the restricted
  Pod Security level at its latest version.
- Produces: `func checkReasonWindow(t *testing.T, rec record, code, window string)`
  Fails the test unless rec has a reason with code, and every reason with
  code has that window.
- Produces: `func checkSetupOutput(t *testing.T, want string)`
  Fails the test unless the evidence of the first setup command,
  evidence/commands/setup in the test's results folder, holds want.

- [ ] **Step 1: Write the tests** in `e2e/custom_test.go`. `allPass` and `failedDeploy` are Task 22's.

```go
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
```

  `TestCustomProbeExitsOnTheCandidate` (row 7, "A custom probe exits when it first reads the candidate's version from the Service: not verified, probe-exited in the deploy's window"): the same with `"ON_VERSION": "exit"`, the code `probe-exited`, and `checkReasonWindow(t, rec, "probe-exited", "deploy")`.

```go
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
	})
}
```

  `TestTCPLoopJudgesACandidateWithoutReadiness` (row 18, "The TCP loop, as a custom probe, judges a candidate with no readiness probe: fail, in the deploy window"): `custom: tcpProbe`, `candidate: {"readiness": false, "startDelay": "5s"}`, as `TestCandidateWithoutReadiness`; expect exit 1, `fail`, `probe-failures` decided, `failedDeploy`.

```go
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
	})
}
```

- [ ] **Step 2: Check that they build**
  Run: `go vet -tags e2e ./e2e/` / Expected: no output.
- [ ] **Step 3: Run the rows under both Helm majors** (about 70 minutes)
  Run: `env CGO_ENABLED=0 KIND_EXPERIMENTAL_PROVIDER=nerdctl go test -tags e2e -p 1 -count=1 -timeout 360m -v ./e2e/ -run '^(TestCustomProbeGoesSilentOnTheCandidate|TestCustomProbeExitsOnTheCandidate|TestTCPLoopJudgesTheFirstFixture|TestProbeFailsItsFirstFiveSeconds|TestProbeFailsItsFirstTwentySeconds|TestTCPLoopJudgesACandidateWithoutReadiness|TestCustomProbeIgnoresSIGTERM|TestTCPLoopInARestrictedProbeNamespace)$'` / Expected: PASS, 8 tests, each under `helm-3` and `helm-4`. On a failure, the rule at the top of this file applies. Give in the report, for rows 16 and 17, the install window's failures and how long before the steady window the last came (section 14, point 7).
- [ ] **Step 4: Commit**
  `git add e2e/custom_test.go` and `git commit -m "Add the fixture rows of the custom probe: silent, exited, the TCP loop, failures in its first seconds, SIGTERM ignored, and a restricted namespace"`

### Task 24: The rows with `setup` and the commands driver

Design: section 12 (rows 10, 21, 22 and 29); section 7 ("Where commands run", "The namespace", "Setup commands", "The commands driver", "When a command fails", "Transitions"); section 10 (the console's "not exercised"). README decisions 6, 7, 18.

**Files:**
- Create: `e2e/commands_test.go` (build tag `e2e`)

**Interfaces:**
- Consumes: `fixture.commands`, `fixture.setup`, `archiveDigests`, `readRecord`, `readConsole` (Task 20); `restrictProbeNamespace`, `checkSetupOutput` (Task 23); `result.json`'s `driver` `"commands"`, `name`, and each release's `role` and `program` (Task 14, decision 6); `evidence/commands/setup` (Task 13, decision 7).
- Produces: `func checkCommandsRecord(t *testing.T, rec record, roles ...string)`
  Fails the test unless rec's driver is commands, its name is orders, its
  releases are the given roles in order, each with the program kubectl, and
  the images previous and candidate carry the digests of their archives.

  The commands driver's rows apply, with `kubectl apply -f` and no `--namespace`, the manifests that `runFixture` rendered from the fixture chart with `helm template`. The probe's URL names `orders.orders.svc`, so a pass shows that such a command acted in `deploy.namespace` (section 7, "The namespace"; decision 18): the target check refuses a host under `.svc` whose Service does not exist, and an install that put nothing in `orders` would find nothing to watch.

- [ ] **Step 1: Write the tests** in `e2e/commands_test.go`. `allPass` is Task 22's.

```go
// Row 10. A setup command fails: not verified. The run ends before the install window, so
// result.json lists no window.
func TestSetupCommandFails(t *testing.T) {
	f := fixture{name: "setup-command-fails", setup: [][]string{{"kubectl", "get", "configmap", "rollproof-fixture-absent"}}}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 2, verdict: "not-verified", codes: []string{"setup-failed"}, decided: "setup-failed"})
		checkSetupOutput(t, `configmaps "rollproof-fixture-absent" not found`)
	})
}

// Row 21. The first fixture through the commands driver, with all three commands: pass.
func TestCommandsDriverWithAllThreeCommands(t *testing.T) {
	f := fixture{name: "commands-driver-with-all-three-commands", commands: []string{"install", "deploy", "rollback"}}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass", windows: allPass})
		rec := readRecord(t, outDir(t))
		checkCommandsRecord(t, rec, "install", "deploy", "rollback")
		for _, name := range []string{"deploy", "rollback"} {
			if w := rec.workload(t, name, "deployment/orders"); w.PodsReplaced != 2 || w.Probed != "yes" {
				t.Errorf("%s: deployment/orders replaced %d, probed %q; want 2 and yes", name, w.PodsReplaced, w.Probed)
			}
		}
	})
}

// Row 22. The same with install and deploy only: pass, reported as a deploy with no rollback.
func TestCommandsDriverWithInstallAndDeployOnly(t *testing.T) {
	f := fixture{name: "commands-driver-with-install-and-deploy-only", commands: []string{"install", "deploy"}}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass", windows: map[string]string{"steady": "pass", "deploy": "pass"}})
		rec := readRecord(t, outDir(t))
		checkCommandsRecord(t, rec, "install", "deploy")
		if len(rec.Transitions) != 1 || rec.Transitions[0].Name != "deploy" {
			t.Errorf("transitions %+v; want the deploy alone", rec.Transitions)
		}
		want := "  the candidate's shutdown and the installed release's start-up, because the run has no rollback\n"
		if c := readConsole(t); !strings.Contains(c, want) {
			t.Errorf("the console does not hold %q:\n%s", want, c)
		}
	})
}

// Row 29. A setup command makes the probe's namespace enforce the restricted level: pass.
func TestSetupMakesTheProbeNamespaceRestricted(t *testing.T) {
	f := fixture{name: "setup-makes-the-probe-namespace-restricted", setup: restrictProbeNamespace}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass", windows: allPass})
		checkSetupOutput(t, "namespace/rollproof-probe labeled")
	})
}
```

- [ ] **Step 2: Check that they build**
  Run: `go vet -tags e2e ./e2e/` / Expected: no output.
- [ ] **Step 3: Run the rows under both Helm majors** (about 35 minutes)
  Run: `env CGO_ENABLED=0 KIND_EXPERIMENTAL_PROVIDER=nerdctl go test -tags e2e -p 1 -count=1 -timeout 360m -v ./e2e/ -run '^(TestSetupCommandFails|TestCommandsDriverWithAllThreeCommands|TestCommandsDriverWithInstallAndDeployOnly|TestSetupMakesTheProbeNamespaceRestricted)$'` / Expected: PASS, 4 tests, each under `helm-3` and `helm-4`. On a failure, the rule at the top of this file applies; a digest that `checkCommandsRecord` finds different from the archive's is a fault of the harness's belief that the tool stores an archive under the digest that `tarball.ImageFromPath` gives, and is reported as such.
- [ ] **Step 4: Commit**
  `git add e2e/commands_test.go` and `git commit -m "Add the fixture rows of the setup commands and the commands driver"`

### Task 25: The whole suite under both Helm majors, and the README

Design: section 12 ("The suite's matrix"); section 13 ("Build order", step 4).

**Files:**
- Modify: `README.md` (what works)

**Interfaces:**
- Consumes: every row of Tasks 22 to 24 and the five of the first plan.
- Produces: `README.md`
  What works now, the suite and how to run it, and what is not built yet,
  as the run of this task found it.

- [ ] **Step 1: Run the unit suite and the checks**
  Run: `gofmt -l .` (prints nothing), `go vet ./...`, `go vet -tags cluster ./...`, `go vet -tags e2e ./...`, `go test -count=1 ./...` / Expected: PASS.
- [ ] **Step 2: Run the whole suite once** (about four hours)
  Run: `go run ./hack/fetchhelm`, then `env CGO_ENABLED=0 KIND_EXPERIMENTAL_PROVIDER=nerdctl go test -tags e2e -p 1 -count=1 -timeout 360m -v ./e2e/` / Expected: PASS, 32 tests (the five of the first plan, 15 of Task 22, 8 of Task 23, 4 of Task 24), each under `helm-3` and `helm-4`: 64 runs. Keep the output (for example by adding ` 2>&1 | tee /tmp/rollproof-e2e-step4.log`). On a failure, the rule at the top of this file applies: the README is not changed, and the task stops at this step. After the run, `nerdctl ps -a` lists no container whose name begins `rollproof-e2e-`.
- [ ] **Step 3: Record the results.** The task's report gives: the date; the engine (rootless nerdctl) and its version; Helm 3.22.1 and 4.3.2; the node images (kind v0.33.0's default, and `oldestNodeImage` for its row); each test with its two results and its time; and, from `TestFirstFixture`'s log line and rows 16 and 17, the install window's failures and how long before the steady window the last came (section 14, point 7).
- [ ] **Step 4: Update `README.md`.** Keep its first three paragraphs. Rewrite "What works now" to say: on Linux with nerdctl, `rollproof run` creates its own kind cluster; deploys through Helm, with or without a previous release (a run with none tests a restart), or through the commands driver; runs the `setup` commands; judges with the built-in HTTP probe or a custom probe that `docs/probe-contract.md` describes; takes images from the engine or from an archive; and writes `result.json`, the console report and the evidence folder. Then: every row of the design's section 12 gives its expected verdict under Helm 3.22.1 and Helm 4.3.2, and the first fixture also on `kindest/node:v1.34.11`, on the date of Step 2's run, which the sentence gives. Then how to run it: `go run ./hack/fetchhelm` fetches the Helm binaries and kubectl 1.37.0 under `.tools/`; the suite's first run pulls busybox 1.37.0 by its digest into `.tools/`; `go test -tags e2e -p 1 ./e2e/ -timeout 360m` runs the suite in about four hours. "Not built yet" becomes: `registries` and `as` in `images`, which end as `config-rejected` with the text "not built yet" and the key's name. Keep the last line on the unit tests.
- [ ] **Step 5: Commit**
  `git add README.md` and `git commit -m "Say in the README what step 4 built and that every fixture of section 12 passes under both Helm majors"`
