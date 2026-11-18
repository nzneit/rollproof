> Part of the plan in `README.md` of this folder. Read that file first: its Global Constraints and its rulings apply to every task here.

### Task 6: The install record in `result.json`

Read section 10, "What a run writes", the item "`result.json`".

**Files:**
- Modify: `internal/report/record.go`, `internal/report/result.go`, `internal/run/record.go`, `internal/run/run.go` (the facts that `record` is given)
- Test: `internal/report/result_test.go`, `internal/report/testdata/result-pass.golden.json`, `internal/report/testdata/result-first.golden.json`, `internal/run/record_test.go`, `internal/run/fakes_test.go`, `internal/run/run_test.go`

**Interfaces:**
- Consumes: `Install.Ready` and `type Settling` of `internal/watch` (Task 1); the install window's `WindowResult`, with `LastFailure` (Task 2); the settling that `runState` keeps (Task 4).
- Produces: `type Install struct { Ready time.Time; FirstObservation, Settled *time.Time; SettleRestartedBy []string; Checks, Failed int; FailedByClass map[string]int; LastFailure *time.Time; SlowestMs, LagMs float64 }`
  The install as the report states it, apart from the windows. A nil time
  is one that never came. The counts are the install window's, all zero
  when the probe gave no observation. Its keys are ready,
  first_observation, settled, settle_restarted_by, checks, failed,
  failed_by_class, last_failure, slowest_ms and lag_ms.
- Produces: `func (i Install) MarshalJSON() ([]byte, error)`
  Writes the keys in the order of the fields, each time in UTC as RFC
  3339 with milliseconds, a nil time as null, a nil FailedByClass as {}
  and a nil SettleRestartedBy as [].
- Produces: `Run.Install *Install`
  The install's record, under the key install after windows. It is nil,
  and written as null, for a run that ended before the install was ready.

- [ ] **Step 1: Write the failing tests for the record's form**

In `internal/report/result_test.go`, `passRun()` gains the install of section 10's example, between `Windows` and `Transitions`:

```go
		Install: &Install{Ready: *at(-21_000), FirstObservation: at(-15_000), Settled: at(0),
			Checks: 3000, Failed: 6, FailedByClass: map[string]int{"refused": 5, "timeout": 1},
			LastFailure: at(-14_900), SlowestMs: 44, LagMs: 9},
```

In `TestNoNulls`, the keys that may be null become `"cluster", "end", "first_failure", "first_observation", "install", "last_failure", "quickest_stop_ms", "settled", "start"`, and the second record of that test gains `Install: &Install{}`. Add:

```go
// The install record at each ending of a run (section 12): no record before the install was
// ready; no time of the first observation and no counts before it; no time of settling before it.
func TestInstallRecordAtEachEnding(t *testing.T) {
	counts := Install{Ready: *at(-21_000), FirstObservation: at(-15_000), Checks: 1200, Failed: 6,
		FailedByClass: map[string]int{"refused": 5, "timeout": 1}, LastFailure: at(-14_900), SlowestMs: 44, LagMs: 9}
	for _, c := range []struct {
		name    string
		install *Install
		want    string // the install's value in result.json, with no white space
	}{
		{"before the install was ready", nil, `null`},
		{"before the probe's first observation", &Install{Ready: *at(-21_000)},
			`{"ready":"2026-11-13T09:59:31.000Z","first_observation":null,"settled":null,"settle_restarted_by":[],` +
				`"checks":0,"failed":0,"failed_by_class":{},"last_failure":null,"slowest_ms":0,"lag_ms":0}`},
		{"before the install had settled", &counts,
			`{"ready":"2026-11-13T09:59:31.000Z","first_observation":"2026-11-13T09:59:37.000Z","settled":null,"settle_restarted_by":[],` +
				`"checks":1200,"failed":6,"failed_by_class":{"refused":5,"timeout":1},"last_failure":"2026-11-13T09:59:37.100Z","slowest_ms":44,"lag_ms":9}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			b, err := encode(Run{Verdict: "not-verified", Install: c.install})
			if err != nil {
				t.Fatal(err)
			}
			var file struct {
				Install json.RawMessage `json:"install"`
			}
			if err := json.Unmarshal(b, &file); err != nil {
				t.Fatal(err)
			}
			var got bytes.Buffer
			if err := json.Compact(&got, file.Install); err != nil {
				t.Fatal(err)
			}
			if got.String() != c.want {
				t.Errorf("install is\n%s\nwant\n%s", got.String(), c.want)
			}
		})
	}
}
```

The ending "after the install had settled" is the golden file of `TestGoldenPass`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/report/ -count=1`
Expected: FAIL at the build, with "undefined: Install" and "unknown field Install".

- [ ] **Step 3: Implement in `internal/report/record.go` and `internal/report/result.go`, with the interface comments from the Interfaces block**

`Install` is written as `Window` is: a `MarshalJSON` of its own gives the key order and the form of the times. `encode` works on a copy of the caller's `Install`, as it does for every list.

- [ ] **Step 4: Make the two golden files again, and read them**

Run: `go test ./internal/report/ -run 'TestFirst|TestGoldenPass' -update -count=1`
Expected: PASS. From the repository root, `git diff --stat projects/rollproof/internal/report/testdata` names only `result-first.golden.json` and `result-pass.golden.json`.

`result-first.golden.json` has one new line, after `"windows": [],`:

```json
  "install": null,
```

`result-pass.golden.json` has this after the list of windows, and nothing else changes in it:

```json
  "install": {
    "ready": "2026-11-13T09:59:31.000Z",
    "first_observation": "2026-11-13T09:59:37.000Z",
    "settled": "2026-11-13T09:59:52.000Z",
    "settle_restarted_by": [],
    "checks": 3000,
    "failed": 6,
    "failed_by_class": {
      "refused": 5,
      "timeout": 1
    },
    "last_failure": "2026-11-13T09:59:37.100Z",
    "slowest_ms": 44,
    "lag_ms": 9
  },
```

Run: `go test ./internal/report/ -count=1`
Expected: PASS. The console's golden files do not change in this task: `Console` does not read the install yet.

- [ ] **Step 5: Write the failing tests for what `run` records**

In `internal/run/record_test.go`, `sampleFacts` gains `probeBegun: true`. Delete the last block of `TestRecordProbe`, the one that expects no probe when the steady window has no start: ruling 3 ends that rule, and `TestRecordProbeOnceTheToolStartedIt` below holds both of its cases. Nothing else in the file changes. Add:

```go
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
```

In `internal/run/fakes_test.go`, the install that `newWorld` scripts for the fake watcher gains `Ready: t0.Add(-21 * time.Second)`. In `internal/run/run_test.go`, `resultFile` gains the install, and its `Probes` gains `Lines int` with the tag `json:"lines"`:

```go
	Install *struct {
		Ready            time.Time  `json:"ready"`
		FirstObservation *time.Time `json:"first_observation"`
		Settled          *time.Time `json:"settled"`
		Checks           int        `json:"checks"`
		Failed           int        `json:"failed"`
		LastFailure      *time.Time `json:"last_failure"`
	} `json:"install"`
```

`TestTheReaderStartsWithTheProbe` gains its last assertion: `r.Install.Checks` is 8,000, the 5,000 early checks and the 3,000 of the fifteen scripted lines of the install window. Add:

```go
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
```

- [ ] **Step 6: Run the tests to verify they fail**

Run: `go test ./internal/run/ -count=1`
Expected: FAIL at the build, with "unknown field probeBegun" and "unknown field settling".

- [ ] **Step 7: Implement in `internal/run/record.go` and `internal/run/run.go`**

- `facts` gains `settling watch.Settling` and `probeBegun bool`. `runState.record` fills them: the settling that `windows` kept, and whether `startProbe` has made the probe.
- `record` fills `Install`:
  - nil when the install's `Ready` is zero;
  - `Ready` from the install;
  - `FirstObservation`, the counts, `FailedByClass`, `LastFailure`, `SlowestMs` and `LagMs` from the decision's install window, when it has one that began;
  - `Settled` from the settling, nil when it is zero;
  - `SettleRestartedBy` from the settling's `Restarts`.
- The probe's entry is in `Probes` when `probeBegun` holds. `started` goes.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/run/ ./internal/report/ ./cmd/rollproof/ -count=1`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/report/record.go internal/report/result.go internal/report/result_test.go internal/report/testdata/result-first.golden.json internal/report/testdata/result-pass.golden.json internal/run/record.go internal/run/record_test.go internal/run/run.go internal/run/fakes_test.go internal/run/run_test.go
git commit -m "Record the install apart from the windows in result.json"
```

### Task 7: The install's lines in the console text

Read section 10, "What a run writes": the two examples and the item "Console".

**Files:**
- Modify: `internal/report/console.go`
- Test: `internal/report/console_test.go`, and in `internal/report/testdata/`: `console-pass.golden.txt` and `console-fail.golden.txt` (copied again from the design), `console-stall.golden.txt` and `console-changed.golden.txt` (made again), `console-before-first-observation.golden.txt`, `console-in-install-window.golden.txt` and `console-install-decided.golden.txt` (new)

**Interfaces:**
- Consumes: `Run.Install *Install` (Task 6); the second line of a `steady-not-clean` reason's text, which is already a detail line of the record (Task 2).
- Produces: `func Console(r Run) string`
  As built, and it prints what the run reached: the images' and the
  probe's lines once the record has a probe, the install's lines once the
  install has a first observation, and the windows' table once a window
  of the list began. It stays a pure function of r.

- [ ] **Step 1: Copy the two examples again**

Replace `internal/report/testdata/console-pass.golden.txt` with the text between the code fences under "A run that passed:" in section 10, and `console-fail.golden.txt` with the text under "A run that failed:", each with one line end after its last line. Both now hold three lines that begin with `install`. `TestConsoleExamples` compares these two files and never rewrites them.

- [ ] **Step 2: Write the failing tests**

In `internal/report/console_test.go`:

`notVerifiedRun` is a run that ended before the tool started the probe. It sets `r.Probes = nil` and `r.Install = nil`, and its comment says so: its golden file, the third example of section 10, has no probe lines.

Three records for the endings that section 12 names:

```go
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
```

In `TestConsoleExamples`, the list of files that `golden` makes gains:

```go
		{"testdata/console-before-first-observation.golden.txt", beforeFirstRun()},
		{"testdata/console-in-install-window.golden.txt", inInstallRun()},
		{"testdata/console-install-decided.golden.txt", installDecidedRun()},
```

In `TestConsoleLines`, the row "variables taken out" now expects the line after the install's lines: `"slowest 44 ms, behind 9 ms\ntaken out of the commands' environment: HELM_DRIVER, HELM_NAMESPACE\n\nwindow"`. Add these rows:

```go
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
```

In `TestConsoleCleansEveryField`, the record also gets `r.Install.FailedByClass = map[string]int{"re" + bad + "fused": 6}` and `r.Install.SettleRestartedBy = []string{"res" + bad + "tart"}`; the test's own checks then cover both (Review Focus 5).

In `TestConsoleDeployWithoutRollback` and `TestConsoleDeployWithoutRollbackThatRan`, the line is `the candidate's shutdown and the installed release's start-up, because the run has no rollback` (ruling 13).

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/report/ -run 'TestConsole' -count=1`
Expected: FAIL. `TestConsoleExamples` reports that `console-pass.golden.txt` and `console-fail.golden.txt` differ, by the three install lines, and that the three new files do not exist.

- [ ] **Step 4: Implement in `internal/report/console.go`, with the interface comment from the Interfaces block**

- The images' lines and the probe's lines are printed when the record has a probe, and the same test decides whether the images' names widen the first column (today `started` decides both). The windows' table is printed when a window of `r.Windows` has a start.
- The install's lines are printed when `r.Install` has a first observation. They stand directly under the probe's lines, before "settings that differ from their defaults", with `install` in the first column as `probe` is and every further line indented 12 spaces:
  1. `settled after %.1f s under the probe`, with the seconds from the first observation to the settling, or `had not settled under the probe when the run ended`; then `; ` and `NOT VERIFIED` when a reason that decided has the window `install`, and otherwise `failures not judged`.
  2. `%s checks, %s failed`, the numbers written as the table writes them, and `check` for exactly one. With a failure, it goes on with a space and, in brackets, each class and its count, the most frequent first and equal counts by name, joined by a comma and a space; then `, the last %.1f s before the steady window` for an install that settled, or `, the last at ` and the time of day in UTC as `15:04:05.00` for one that did not.
  3. `slowest %s, behind %s`, each as the table writes it, with `-` for the slowest when no check passed and `-` for both when there was no check.
  4. For each restart, `the settle time started again: ` and its sentence, wrapped as a reason's detail lines are, with this indent.
- `cleanRun` cleans the install's restart sentences and the names of its classes, so that the console prints only from the cleaned copy.
- `exercisedLists` writes the line of ruling 13.

- [ ] **Step 5: Make the golden files, and read them**

Run: `go test ./internal/report/ -run TestConsoleExamples -update -count=1`
Expected: PASS. From the repository root, `git status --short projects/rollproof/internal/report/testdata` shows `console-stall.golden.txt` and `console-changed.golden.txt` as changed and the three new files, beside the two that Step 1 replaced. In the two that changed, the only difference is the three install lines of the pass example, under the probe's lines.

`console-in-install-window.golden.txt` reads:

```
rollproof: NOT VERIFIED   orders (Helm) on Kubernetes v1.37.0 in kind

releases    previous   orders 1.4.2, oci://registry.example/charts/orders; chart defaults
            candidate  orders 1.5.0, ./chart; values: inline (image)
images      orders     run by no watched pod
probe       built-in HTTP: GET http://orders.orders.svc/api/orders/latest
            200 a second (one check every 5 ms), timeout 2 s, status 200-299
install     had not settled under the probe when the run ended; NOT VERIFIED
            1,200 checks, 6 failed (refused 5, timeout 1), the last at 09:59:37.10
            slowest 44 ms, behind 9 ms

install: a signal stopped the run   [interrupted]
results: /tmp/rollproof-results-7f3a
```

`console-install-decided.golden.txt` has the same first seven lines, and then:

```
install     settled after 15.0 s under the probe; NOT VERIFIED
            1,800 checks, 0 failed
            slowest 44 ms, behind 9 ms

install: no observation for 6.0 s, from 09:59:42.00   [probe-silent]
results: /tmp/rollproof-results-7f3a
```

`console-before-first-observation.golden.txt` has the same first seven lines, no line that begins with `install`, and then:

```

could not test: no observation within limits.timeout; the pod is Pending; container probe is waiting: ImagePullBackOff   [probe-not-started]
results: /tmp/rollproof-results-7f3a
```

If a file reads otherwise, the code is wrong, not the file: correct `console.go` and make the file again.

- [ ] **Step 6: Run the package's tests**

Run: `go test ./internal/report/ ./internal/run/ -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/report/console.go internal/report/console_test.go internal/report/testdata/console-pass.golden.txt internal/report/testdata/console-fail.golden.txt internal/report/testdata/console-stall.golden.txt internal/report/testdata/console-changed.golden.txt internal/report/testdata/console-before-first-observation.golden.txt internal/report/testdata/console-in-install-window.golden.txt internal/report/testdata/console-install-decided.golden.txt
git commit -m "Print the install's lines in the console text"
```

### Task 8: The fixtures: the install record, and a previous release that fails every request

Read section 12, "With a cluster", and the last three items of section 14, point 23. This task needs a container engine, and the Helm binaries under `.tools/` (`go run ./hack/fetchhelm` fetches them; that is the one step of this plan that uses the network, and only when they are missing). It starts clusters on the developer's machine and nothing else.

**Files:**
- Modify: `fixtures/service/main.go`, `fixtures/chart/values.yaml`, `fixtures/chart/values.schema.json`, `fixtures/chart/templates/deployment.yaml`, `e2e/harness_test.go`, `e2e/fixtures_test.go`, `README.md`
- Test: `fixtures/service/main_test.go`, `e2e/fixtures_test.go`

**Interfaces:**
- Consumes: the keys of `result.json`'s `install` (Task 6); the second line of a `steady-not-clean` reason (Task 2).
- Produces: nothing that other code calls. The fixture service gains the environment variable FAIL_WORK, and the chart the value failWork.

- [ ] **Step 1: Write the failing test for the service's switch**

Add to `fixtures/service/main_test.go`:

```go
// With FAIL_WORK the handler fails every request while the pod still reports ready.
func TestWorkFailsWhenAsked(t *testing.T) {
	ready, _, done := launch(t, map[string]string{"ADDR": "127.0.0.1:0", "SHUTDOWN": "drain-now", "FAIL_WORK": "true"})
	addr := listening(t, ready, done)
	if code, _, err := get(addr, "/work?ms=20"); err != nil || code != 500 {
		t.Errorf("/work?ms=20: %d, %v; want 500", code, err)
	}
	if code, _, err := get(addr, "/readyz"); err != nil || code != 200 {
		t.Errorf("/readyz: %d, %v; want 200", code, err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./fixtures/service/ -run TestWorkFailsWhenAsked -count=1`
Expected: FAIL, with "/work?ms=20: 200".

- [ ] **Step 3: Add the switch to the service and the chart**

- `fixtures/service/main.go`: when `FAIL_WORK` is `true`, `GET /work` answers 500 with the body `failing` and does nothing else; `/readyz` does not change. The header comment of the file lists the variable, and the start-up line logs it.
- `fixtures/chart/values.yaml`: `failWork: false`, under the comment `# failWork makes /work answer 500 while /readyz still answers 200.`
- `fixtures/chart/values.schema.json`: `"failWork": { "type": "boolean" }`.
- `fixtures/chart/templates/deployment.yaml`: the environment variable `FAIL_WORK` with the value `{{ .Values.failWork | quote }}`.

Run: `go test ./fixtures/service/ -count=1`
Expected: PASS.

- [ ] **Step 4: Write the fixture tests**

In `e2e/harness_test.go`, add a reader of what the two tests need:

```go
// installResult is result.json's install, the steady window's times, and each reason's detail,
// as far as the fixtures read them.
type installResult struct {
	Install *struct {
		Ready            time.Time      `json:"ready"`
		FirstObservation *time.Time     `json:"first_observation"`
		Settled          *time.Time     `json:"settled"`
		Checks           int            `json:"checks"`
		Failed           int            `json:"failed"`
		FailedByClass    map[string]int `json:"failed_by_class"`
		LastFailure      *time.Time     `json:"last_failure"`
	} `json:"install"`
	Windows []struct {
		Name  string     `json:"name"`
		Start *time.Time `json:"start"`
		End   *time.Time `json:"end"`
	} `json:"windows"`
	Reasons []struct {
		Code   string   `json:"code"`
		Detail []string `json:"detail"`
	} `json:"reasons"`
}

// readInstall reads out/result.json as an installResult, and fails the test when the file has no
// install, or no steady window first in its list.
func readInstall(t *testing.T, out string) installResult
```

`e2e/fixtures_test.go` imports `strings` and `time` for the two tests below. In it, `TestFirstFixture` also asserts the install record, inside the function it hands to `underBothHelms`:

```go
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
```

And the new row:

```go
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
```

Run: `go vet -tags e2e ./e2e/`
Expected: no output.

- [ ] **Step 5: Run every test that needs no cluster, and build the binary**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: PASS for every package.

Run: `env CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=dev" -o bin/rollproof ./cmd/rollproof`
Expected: no output.

- [ ] **Step 6: Run the probe's test with a cluster**

Run, on a machine whose engine is nerdctl: `env CGO_ENABLED=0 KIND_EXPERIMENTAL_PROVIDER=nerdctl go test -tags cluster -p 1 -count=1 -run TestPodOnACluster ./internal/probe/`
Expected: PASS: 80 to 120 checks passed in the probe's first 5 s and none failed. Its target is the node's registry, which no install has just created.

- [ ] **Step 7: Run the fixtures**

Run, on a machine whose engine is nerdctl: `env KIND_EXPERIMENTAL_PROVIDER=nerdctl go test -tags e2e -p 1 -count=1 -timeout 120m -v ./e2e/`
Expected: PASS for the five tests, each under `helm-3` and `helm-4`. `TestFirstFixture` logs its line about the install window for each.

If `TestFirstFixture` ends with `steady-not-clean`, stop. Do not change a limit, the fixture or the probe. That result is the answer to an open point of section 14, point 23: the probe's own pod, or the cluster, meets failures that last longer than `limits.settle`. Report the two logged lines, the probe's log that the harness prints, and the run's `result.json`.

- [ ] **Step 8: Say in the README what works now**

In `README.md`, the sentence "The first four fixtures of the design's section 12 give their expected verdicts under Helm 3.22.1 and Helm 4.3.2." becomes "The first four fixtures of the design's section 12, and the one whose previous release fails every request, give their expected verdicts under Helm 3.22.1 and Helm 4.3.2."

- [ ] **Step 9: Commit**

```bash
git add fixtures/service/main.go fixtures/service/main_test.go fixtures/chart/values.yaml fixtures/chart/values.schema.json fixtures/chart/templates/deployment.yaml e2e/harness_test.go e2e/fixtures_test.go README.md
git commit -m "Add the fixture whose previous release fails every request, and assert the install record"
```

In the report of this task, give the lines that `TestFirstFixture` logged about the install window under each Helm major, and how long the suite took.
