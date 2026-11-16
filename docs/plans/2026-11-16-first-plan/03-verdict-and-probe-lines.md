> Part of the plan in `README.md` of this folder. Read that file first: its Global Constraints apply to every task here.

## Part 3: The walking skeleton

Part 3 starts when Task 11's list allows it. Tasks 12 to 20, 23 to 26, 28 and 30 to 35 need no cluster. Tasks 21, 22, 27, 29 and 36 need a container engine, one cluster at a time.

### Task 12: `verdict`: the vocabulary

**Files:**
- Create: `internal/verdict/vocab.go`
- Test: `internal/verdict/vocab_test.go`

**Interfaces:**
- Consumes: the module `rollproof` (Task 2).
- Produces: `module internal/verdict`
  The verdict of a run as a pure function of what happened in it, and the
  words every other unit uses for what happened. It holds every rule of
  section 10 and does no I/O. It knows nothing of Kubernetes, Helm or files.
- Produces: `type Verdict string`
  How a run ended: Pass "pass", Fail "fail" or NotVerified "not-verified".
  These are the strings of result.json and of the action's output.
- Produces: `func (v Verdict) ExitCode() int`
  The exit code of the run command: 0 for Pass, 1 for Fail, and 2 for
  NotVerified and for any other value.
- Produces: `type Code string`
  A reason code of section 10. There are 22, each a constant named after
  its string in Go's form: ProbeFailures is "probe-failures", InternalError
  is "internal-error".
- Produces: `func Codes() []Code`
  Every code, in the order of section 10's list of reasons. That order
  picks the deciding reason among several in one window.
- Produces: `func (c Code) Verdict() Verdict`
  The verdict a code stands for: Fail for the first four of Codes, and
  NotVerified for every other code, one this build does not know included.
- Produces: `type WindowName string`
  The name of a window: Steady "steady", Deploy "deploy", Rollback
  "rollback" or Restart "restart".
- Produces: `type Result string`
  How one window ended: ResultPass "pass", ResultFail "fail",
  ResultNotVerified "not-verified", NotRun "not-run" or NotJudged
  "not-judged".
- Produces: `type Condition struct { Code Code; Text string; At time.Time }`
  One thing that happened and that a rule weighs. Text says it for people:
  its first line is the short form, and any further lines are detail. At
  is the moment it happened; it is zero when the caller places the
  condition in a window itself.
- Produces: `func (c Condition) Error() string`
  The code, a colon and the text, so that a unit can return a Condition as
  its error.
- Produces: `func AsCondition(err error) Condition`
  The Condition that err is or wraps. An error that holds none becomes a
  Condition with InternalError and the error's text. err must not be nil.
- Produces: `type Observation struct { Written, T time.Time; OK bool; N int; Class string; Ms, Lag float64 }`
  One line of a probe that carries ok. Written is when the node's runtime
  stamped the line, T when the check began (equal to Written when the line
  gave none). N is how many passed checks the line stands for, 1 or more,
  and always 1 on a failure. Class, Ms and Lag are as section 6 defines.

- [ ] **Step 1: Write the failing tests**

Create `internal/verdict/vocab_test.go`:

```go
package verdict

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestCodesOrderAndVerdict(t *testing.T) {
	want := []struct {
		code Code
		text string
	}{
		{ProbeFailures, "probe-failures"},
		{TransitionStalled, "transition-stalled"},
		{PodUnschedulable, "pod-unschedulable"},
		{CommandFailed, "command-failed"},
		{ConfigRejected, "config-rejected"},
		{RunnerUnsupported, "runner-unsupported"},
		{ClusterFailed, "cluster-failed"},
		{ChartFetchFailed, "chart-fetch-failed"},
		{SetupFailed, "setup-failed"},
		{InstallFailed, "install-failed"},
		{NothingWatched, "nothing-watched"},
		{WorkloadUnsupported, "workload-unsupported"},
		{TargetUnsupported, "target-unsupported"},
		{ProbeNotStarted, "probe-not-started"},
		{ProbeExited, "probe-exited"},
		{ProbeLineInvalid, "probe-line-invalid"},
		{SteadyNotClean, "steady-not-clean"},
		{ProbeSilent, "probe-silent"},
		{NoPodReplaced, "no-pod-replaced"},
		{RestartFailed, "restart-failed"},
		{Interrupted, "interrupted"},
		{InternalError, "internal-error"},
	}
	got := Codes()
	if len(got) != len(want) {
		t.Fatalf("Codes() has %d codes, want %d", len(got), len(want))
	}
	for i, w := range want {
		if string(w.code) != w.text {
			t.Errorf("the constant for %q is %q", w.text, w.code)
		}
		if got[i] != w.code {
			t.Errorf("Codes()[%d] = %q, want %q", i, got[i], w.code)
		}
		wantVerdict := NotVerified
		if i < 4 {
			wantVerdict = Fail
		}
		if v := w.code.Verdict(); v != wantVerdict {
			t.Errorf("%s.Verdict() = %q, want %q", w.code, v, wantVerdict)
		}
	}
	if v := Code("something-new").Verdict(); v != NotVerified {
		t.Errorf("an unknown code gives %q, want %q", v, NotVerified)
	}
	got[0] = "changed"
	if Codes()[0] != ProbeFailures {
		t.Error("changing the slice that Codes returned changed the order")
	}
}

func TestExitCode(t *testing.T) {
	for _, tc := range []struct {
		v    Verdict
		want int
	}{
		{Pass, 0},
		{Fail, 1},
		{NotVerified, 2},
		{Verdict(""), 2},
		{Verdict("PASS"), 2},
	} {
		if got := tc.v.ExitCode(); got != tc.want {
			t.Errorf("Verdict(%q).ExitCode() = %d, want %d", tc.v, got, tc.want)
		}
	}
}

func TestConditionIsAnError(t *testing.T) {
	c := Condition{Code: SetupFailed, Text: "x"}
	if got := c.Error(); got != "setup-failed: x" {
		t.Errorf("Error() = %q, want %q", got, "setup-failed: x")
	}

	exited := Condition{Code: ProbeExited, Text: "exit code 1", At: time.Date(2026, 11, 13, 10, 0, 33, 0, time.UTC)}
	wrapped := fmt.Errorf("follow: %w", fmt.Errorf("pod: %w", exited))
	var found Condition
	if !errors.As(wrapped, &found) || found != exited {
		t.Errorf("errors.As found %+v, want %+v", found, exited)
	}
	if got := AsCondition(wrapped); got != exited {
		t.Errorf("AsCondition(wrapped) = %+v, want %+v", got, exited)
	}

	want := Condition{Code: InternalError, Text: "boom"}
	if got := AsCondition(errors.New("boom")); got != want {
		t.Errorf("AsCondition(boom) = %+v, want %+v", got, want)
	}
}

func TestStrings(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{string(Pass), "pass"},
		{string(Fail), "fail"},
		{string(NotVerified), "not-verified"},
		{string(Steady), "steady"},
		{string(Deploy), "deploy"},
		{string(Rollback), "rollback"},
		{string(Restart), "restart"},
		{string(ResultPass), "pass"},
		{string(ResultFail), "fail"},
		{string(ResultNotVerified), "not-verified"},
		{string(NotRun), "not-run"},
		{string(NotJudged), "not-judged"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/verdict/ -v`
Expected: FAIL: the package does not build, with `undefined: Codes` among the errors.

- [ ] **Step 3: Write `internal/verdict/vocab.go`**

Its package comment is the text of `module internal/verdict` in the Interfaces block. Every exported name below gets its interface comment from the Interfaces block. The file imports `errors` and `time` and nothing else.

- `type Verdict string` with the constants `Pass`, `Fail` and `NotVerified`, and `func (v Verdict) ExitCode() int`.
- `type Code string` with the 22 constants, named and spelled as the test lists them, in that order. `func Codes() []Code` returns a new slice on every call, so that no caller can change the order. `func (c Code) Verdict() Verdict`.
- `type WindowName string` with `Steady`, `Deploy`, `Rollback` and `Restart`; `type Result string` with `ResultPass`, `ResultFail`, `ResultNotVerified`, `NotRun` and `NotJudged`.
- `type Condition struct { Code Code; Text string; At time.Time }` and `func (c Condition) Error() string` on a value receiver, so that `errors.As` finds a `Condition` value. `func AsCondition(err error) Condition` uses `errors.As`; when err holds no Condition it returns `Condition{Code: InternalError, Text: err.Error()}`.
- `type Observation struct { Written, T time.Time; OK bool; N int; Class string; Ms, Lag float64 }`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/verdict/ -v`
Expected: PASS

Run: `go vet ./internal/verdict/`
Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add internal/verdict/vocab.go internal/verdict/vocab_test.go
git commit -m "Add the verdict vocabulary: verdicts, reason codes, conditions and observations"
```

### Task 13: `verdict.Decide`: the rules on placed conditions

**Files:**
- Create: `internal/verdict/decide.go`
- Test: `internal/verdict/decide_test.go`

**Interfaces:**
- Consumes: `type Condition struct { Code Code; Text string; At time.Time }`, `type Observation struct { … }`, `func Codes() []Code`, `func (c Code) Verdict() Verdict` and the constants (Task 12).
- Produces: `type Window struct { Name WindowName; Start, Finished, End time.Time; Conditions []Condition }`
  One window of a run. Start is zero for a window that never began, and End
  is zero while it is open. Finished is when its transition last came to be
  finished: zero for the steady window and for a stall. Conditions are what
  the caller placed in this window.
- Produces: `type Timeline struct { Before []Condition; Windows []Window; Observations []Observation; Timed []Condition; Silence time.Duration; Now time.Time }`
  Everything known of a run so far. Before is what happened before the
  steady window. Windows are all the run's windows in order, those not yet
  begun included. Timed are conditions that Decide places by their At.
  Silence is limits.silence. Now is the moment of judging.
- Produces: `type Reason struct { Code Code; Verdict Verdict; Window WindowName; Text string; Decided bool }`
  One reason of the verdict, as result.json lists it. Window is empty for a
  reason with no window. Decided marks the one reason that decided.
- Produces: `type WindowResult struct { Name WindowName; Start, End time.Time; Result Result; Checks, Failed, AfterFinish, Crossed int; ByClass map[string]int; FirstFailure time.Time; SlowestMs, LagMs float64 }`
  How one window ended, with its counts. AfterFinish counts failures that
  began at or after the window's Finished. Crossed counts failures that
  began before the window's Start and were written in or after it.
  FirstFailure is zero when nothing failed.
- Produces: `type Decision struct { Verdict Verdict; GoOn bool; Reasons []Reason; Windows []WindowResult; Uncounted int; Ignored []Condition }`
  The verdict so far. GoOn says whether the run starts its next transition.
  Uncounted is the number of observations outside every window. Ignored are
  Timed conditions later than the last window's End; the report shows them
  as warnings.
- Produces: `func Decide(t Timeline) Decision`
  Judges everything known so far by the rules of section 10. Call it at the
  end of each window and once more after the probe has stopped; the last
  call's Decision is the run's. GoOn is true when the verdict so far is
  Pass. A Timeline in which no window has begun, with nothing in Before,
  gives NotVerified with InternalError.

- [ ] **Step 1: Write the failing tests**

Create `internal/verdict/decide_test.go`. Every row is a Timeline literal with times from one base, `t0`. Each row's Timeline goes through `worked`, which adds the passing lines of a probe that worked. Decide ignores them until Task 14; after that they make sure that counting adds no condition of its own to a row. The helpers of this file are used by Task 14's tests too; the package's own code declares none of their names.

```go
package verdict

import (
	"reflect"
	"testing"
	"time"
)

// t0 is the base of every time in the tables of this package.
var t0 = time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)

const sec, msec = time.Second, time.Millisecond

// at is the moment d after t0.
func at(d time.Duration) time.Time { return t0.Add(d) }

// The windows of the tables, from t0: steady [0 s, 10 s), deploy
// [10 s, 40 s) finished at 25 s, rollback [40 s, 70 s) finished at 55 s.
func steady(cs ...Condition) Window {
	return Window{Name: Steady, Start: at(0), End: at(10 * sec), Conditions: cs}
}

func deploy(cs ...Condition) Window {
	return Window{Name: Deploy, Start: at(10 * sec), Finished: at(25 * sec), End: at(40 * sec), Conditions: cs}
}

func rollback(cs ...Condition) Window {
	return Window{Name: Rollback, Start: at(40 * sec), Finished: at(55 * sec), End: at(70 * sec), Conditions: cs}
}

// stalled is w as a transition that never finished.
func stalled(w Window) Window { w.Finished = time.Time{}; return w }

// inProgress is w before it has ended.
func inProgress(w Window) Window { w.Finished, w.End = time.Time{}, time.Time{}; return w }

func notBegun(name WindowName) Window { return Window{Name: name} }

func cond(c Code) Condition { return Condition{Code: c, Text: "text of " + string(c)} }

func timed(c Code, d time.Duration) Condition {
	return Condition{Code: c, Text: "text of " + string(c), At: at(d)}
}

// worked gives tl the observations of a probe that worked: one passing
// summary line a second from the steady window's Start to the end of the
// last window that began (Now while that window is open), and a Silence of
// 5 s, so that counting observations adds no condition to a row.
func worked(tl Timeline) Timeline {
	tl.Silence = 5 * sec
	if len(tl.Windows) == 0 || tl.Windows[0].Start.IsZero() {
		return tl
	}
	end := tl.Now
	for _, w := range tl.Windows {
		if w.Start.IsZero() {
			break
		}
		end = w.End
		if end.IsZero() {
			end = tl.Now
		}
	}
	for m := tl.Windows[0].Start; m.Before(end); m = m.Add(sec) {
		tl.Observations = append(tl.Observations, Observation{Written: m, T: m, OK: true, N: 200, Ms: 30, Lag: 5})
	}
	return tl
}

// summary is a Decision as the tables state it.
type summary struct {
	Verdict Verdict
	Results []Result
	Reasons []string // "WINDOW:code"; WINDOW is empty for a reason with no window
	Decided Code
	GoOn    bool
}

// summarize gives d as a summary. It fails the test when a reason carries
// a verdict other than its code's, or when reasons exist and not exactly
// one of them decided.
func summarize(t *testing.T, d Decision) summary {
	t.Helper()
	s := summary{Verdict: d.Verdict, GoOn: d.GoOn}
	for _, w := range d.Windows {
		s.Results = append(s.Results, w.Result)
	}
	decided := 0
	for _, r := range d.Reasons {
		s.Reasons = append(s.Reasons, string(r.Window)+":"+string(r.Code))
		if r.Verdict != r.Code.Verdict() {
			t.Errorf("reason %s carries %q, want %q", r.Code, r.Verdict, r.Code.Verdict())
		}
		if r.Decided {
			s.Decided = r.Code
			decided++
		}
	}
	if len(d.Reasons) > 0 && decided != 1 {
		t.Errorf("%d reasons decided, want 1", decided)
	}
	return s
}

type placedRow struct {
	name    string
	tl      Timeline // worked() adds the observations
	want    summary
	ignored []Code
}

func placedRows() []placedRow {
	pass3 := []Result{ResultPass, ResultPass, ResultPass}
	notRun3 := []Result{NotRun, NotRun, NotRun}
	return []placedRow{{
		name: "all three windows ended with no condition",
		tl:   Timeline{Windows: []Window{steady(), deploy(), rollback()}, Now: at(70 * sec)},
		want: summary{Verdict: Pass, Results: pass3, GoOn: true},
	}, {
		name: "steady ended clean, deploy in progress, rollback not begun",
		tl:   Timeline{Windows: []Window{steady(), inProgress(deploy()), notBegun(Rollback)}, Now: at(30 * sec)},
		want: summary{Verdict: Pass, Results: []Result{ResultPass, ResultPass, NotRun}, GoOn: true},
	}, {
		name: "Before has config-rejected",
		tl:   Timeline{Before: []Condition{cond(ConfigRejected)}, Windows: []Window{notBegun(Steady), notBegun(Deploy), notBegun(Rollback)}, Now: at(0)},
		want: summary{Verdict: NotVerified, Results: notRun3, Reasons: []string{":config-rejected"}, Decided: ConfigRejected},
	}, {
		name: "Before has two conditions; the earlier in Codes decides",
		tl:   Timeline{Before: []Condition{cond(Interrupted), cond(ClusterFailed)}, Now: at(0)},
		want: summary{Verdict: NotVerified, Reasons: []string{":cluster-failed", ":interrupted"}, Decided: ClusterFailed},
	}, {
		name: "steady has interrupted",
		tl:   Timeline{Windows: []Window{steady(cond(Interrupted)), notBegun(Deploy), notBegun(Rollback)}, Now: at(10 * sec)},
		want: summary{Verdict: NotVerified, Results: []Result{ResultNotVerified, NotRun, NotRun}, Reasons: []string{"steady:interrupted"}, Decided: Interrupted},
	}, {
		name: "steady has probe-silent and steady-not-clean",
		tl:   Timeline{Windows: []Window{steady(cond(ProbeSilent), cond(SteadyNotClean)), notBegun(Deploy), notBegun(Rollback)}, Now: at(10 * sec)},
		want: summary{Verdict: NotVerified, Results: []Result{ResultNotVerified, NotRun, NotRun}, Reasons: []string{"steady:steady-not-clean", "steady:probe-silent"}, Decided: SteadyNotClean},
	}, {
		name: "steady holds only a fail code; it is not verified all the same, and that reason decides",
		tl:   Timeline{Windows: []Window{steady(cond(CommandFailed)), notBegun(Deploy), notBegun(Rollback)}, Now: at(10 * sec)},
		want: summary{Verdict: NotVerified, Results: []Result{ResultNotVerified, NotRun, NotRun}, Reasons: []string{"steady:command-failed"}, Decided: CommandFailed},
	}, {
		name: "deploy stalled with a pod that could not be scheduled",
		tl:   Timeline{Windows: []Window{steady(), stalled(deploy(cond(PodUnschedulable), cond(TransitionStalled))), notBegun(Rollback)}, Now: at(40 * sec)},
		want: summary{Verdict: Fail, Results: []Result{ResultPass, ResultFail, NotRun}, Reasons: []string{"deploy:transition-stalled", "deploy:pod-unschedulable"}, Decided: TransitionStalled},
	}, {
		name: "deploy command failed",
		tl:   Timeline{Windows: []Window{steady(), deploy(cond(CommandFailed)), notBegun(Rollback)}, Now: at(40 * sec)},
		want: summary{Verdict: Fail, Results: []Result{ResultPass, ResultFail, NotRun}, Reasons: []string{"deploy:command-failed"}, Decided: CommandFailed},
	}, {
		name: "deploy replaced no pod",
		tl:   Timeline{Windows: []Window{steady(), deploy(cond(NoPodReplaced)), notBegun(Rollback)}, Now: at(40 * sec)},
		want: summary{Verdict: NotVerified, Results: []Result{ResultPass, ResultNotVerified, NotRun}, Reasons: []string{"deploy:no-pod-replaced"}, Decided: NoPodReplaced},
	}, {
		name: "the probe exited in a deploy that stalled; fail outranks",
		tl:   Timeline{Windows: []Window{steady(), stalled(deploy(cond(ProbeExited), cond(TransitionStalled))), notBegun(Rollback)}, Now: at(40 * sec)},
		want: summary{Verdict: Fail, Results: []Result{ResultPass, ResultFail, NotRun}, Reasons: []string{"deploy:transition-stalled", "deploy:probe-exited"}, Decided: TransitionStalled},
	}, {
		name: "the tool failed after the deploy command failed",
		tl:   Timeline{Windows: []Window{steady(), deploy(cond(InternalError), cond(CommandFailed)), notBegun(Rollback)}, Now: at(40 * sec)},
		want: summary{Verdict: Fail, Results: []Result{ResultPass, ResultFail, NotRun}, Reasons: []string{"deploy:command-failed", "deploy:internal-error"}, Decided: CommandFailed},
	}, {
		name: "a signal during the deploy",
		tl:   Timeline{Windows: []Window{steady(), deploy(cond(Interrupted)), notBegun(Rollback)}, Now: at(40 * sec)},
		want: summary{Verdict: NotVerified, Results: []Result{ResultPass, ResultNotVerified, NotRun}, Reasons: []string{"deploy:interrupted"}, Decided: Interrupted},
	}, {
		name: "a late condition in deploy after the rollback ended clean",
		tl:   Timeline{Windows: []Window{steady(), deploy(cond(ProbeFailures)), rollback()}, Now: at(70 * sec)},
		want: summary{Verdict: Fail, Results: []Result{ResultPass, ResultFail, NotJudged}, Reasons: []string{"deploy:probe-failures"}, Decided: ProbeFailures},
	}, {
		name: "rollback has workload-unsupported",
		tl:   Timeline{Windows: []Window{steady(), deploy(), rollback(cond(WorkloadUnsupported))}, Now: at(70 * sec)},
		want: summary{Verdict: NotVerified, Results: []Result{ResultPass, ResultPass, ResultNotVerified}, Reasons: []string{"rollback:workload-unsupported"}, Decided: WorkloadUnsupported},
	}, {
		name: "a signal while the probe stops, placed in the last window",
		tl:   Timeline{Windows: []Window{steady(), deploy(), rollback(cond(Interrupted))}, Now: at(72 * sec)},
		want: summary{Verdict: NotVerified, Results: []Result{ResultPass, ResultPass, ResultNotVerified}, Reasons: []string{"rollback:interrupted"}, Decided: Interrupted},
	}, {
		name: "a signal while the probe stops, after the last window failed",
		tl:   Timeline{Windows: []Window{steady(), deploy(), rollback(cond(Interrupted), cond(ProbeFailures))}, Now: at(72 * sec)},
		want: summary{Verdict: Fail, Results: []Result{ResultPass, ResultPass, ResultFail}, Reasons: []string{"rollback:probe-failures", "rollback:interrupted"}, Decided: ProbeFailures},
	}, {
		name: "a restart-only run whose restart was refused",
		tl: Timeline{Windows: []Window{steady(), {Name: Restart, Start: at(10 * sec), End: at(40 * sec), Conditions: []Condition{cond(RestartFailed)}}},
			Now: at(40 * sec)},
		want: summary{Verdict: NotVerified, Results: []Result{ResultPass, ResultNotVerified}, Reasons: []string{"restart:restart-failed"}, Decided: RestartFailed},
	}, {
		name: "a timed probe-exited inside the deploy window",
		tl:   Timeline{Windows: []Window{steady(), deploy(), rollback()}, Timed: []Condition{timed(ProbeExited, 20*sec)}, Now: at(70 * sec)},
		want: summary{Verdict: NotVerified, Results: []Result{ResultPass, ResultNotVerified, NotJudged}, Reasons: []string{"deploy:probe-exited"}, Decided: ProbeExited},
	}, {
		name: "a timed condition at a window's Start belongs to that window",
		tl:   Timeline{Windows: []Window{steady(), deploy(), rollback()}, Timed: []Condition{timed(ProbeLineInvalid, 10*sec)}, Now: at(70 * sec)},
		want: summary{Verdict: NotVerified, Results: []Result{ResultPass, ResultNotVerified, NotJudged}, Reasons: []string{"deploy:probe-line-invalid"}, Decided: ProbeLineInvalid},
	}, {
		name:    "timed conditions at and after the last window's End are ignored",
		tl:      Timeline{Windows: []Window{steady(), deploy(), rollback()}, Timed: []Condition{timed(ProbeLineInvalid, 70*sec), timed(ProbeExited, 71*sec)}, Now: at(72 * sec)},
		want:    summary{Verdict: Pass, Results: pass3, GoOn: true},
		ignored: []Code{ProbeLineInvalid, ProbeExited},
	}, {
		name: "a timed condition before the steady window's Start goes to Before",
		tl:   Timeline{Windows: []Window{steady(), deploy(), rollback()}, Timed: []Condition{timed(ProbeLineInvalid, -1*sec)}, Now: at(70 * sec)},
		want: summary{Verdict: NotVerified, Results: notRun3, Reasons: []string{":probe-line-invalid"}, Decided: ProbeLineInvalid},
	}, {
		name: "a timed condition between one window's End and the next Start belongs to the earlier",
		tl: Timeline{Windows: []Window{steady(), deploy(), {Name: Rollback, Start: at(40500 * msec), Finished: at(55 * sec), End: at(70 * sec)}},
			Timed: []Condition{timed(ProbeLineInvalid, 40200*msec)}, Now: at(70 * sec)},
		want: summary{Verdict: NotVerified, Results: []Result{ResultPass, ResultNotVerified, NotJudged}, Reasons: []string{"deploy:probe-line-invalid"}, Decided: ProbeLineInvalid},
	}}
}

func TestDecidePlaced(t *testing.T) {
	for _, tc := range placedRows() {
		t.Run(tc.name, func(t *testing.T) {
			d := Decide(worked(tc.tl))
			if got := summarize(t, d); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
			var ignored []Code
			for _, c := range d.Ignored {
				ignored = append(ignored, c.Code)
			}
			if !reflect.DeepEqual(ignored, tc.ignored) {
				t.Errorf("Ignored = %v, want %v", ignored, tc.ignored)
			}
			for _, r := range d.Reasons {
				if r.Text != "text of "+string(r.Code) {
					t.Errorf("reason %s has the text %q", r.Code, r.Text)
				}
			}
			if len(d.Windows) != len(tc.tl.Windows) {
				t.Fatalf("%d window results, want %d", len(d.Windows), len(tc.tl.Windows))
			}
			for i, w := range tc.tl.Windows {
				r := d.Windows[i]
				if r.Name != w.Name || !r.Start.Equal(w.Start) || !r.End.Equal(w.End) {
					t.Errorf("window %d is %s [%v, %v), want %s [%v, %v)", i, r.Name, r.Start, r.End, w.Name, w.Start, w.End)
				}
			}
		})
	}
}

func TestDecideNothingBegan(t *testing.T) {
	for name, tl := range map[string]Timeline{
		"an empty Timeline":        {},
		"windows that never began": {Windows: []Window{notBegun(Steady), notBegun(Deploy), notBegun(Rollback)}, Now: at(0)},
	} {
		t.Run(name, func(t *testing.T) {
			d := Decide(tl)
			want := []Reason{{Code: InternalError, Verdict: NotVerified, Text: "the run did not finish", Decided: true}}
			if d.Verdict != NotVerified || d.GoOn || !reflect.DeepEqual(d.Reasons, want) {
				t.Errorf("got %+v, want not-verified with %+v and GoOn false", d, want)
			}
			for _, w := range d.Windows {
				if w.Result != NotRun {
					t.Errorf("window %s is %s, want not-run", w.Name, w.Result)
				}
			}
		})
	}
}

func TestDecideExitCodes(t *testing.T) {
	want := map[string]int{
		"all three windows ended with no condition":       0,
		"the tool failed after the deploy command failed": 1,
		"rollback has workload-unsupported":               2,
	}
	for _, tc := range placedRows() {
		code, ok := want[tc.name]
		if !ok {
			continue
		}
		if got := Decide(worked(tc.tl)).Verdict.ExitCode(); got != code {
			t.Errorf("%s: exit code %d, want %d", tc.name, got, code)
		}
		delete(want, tc.name)
	}
	if len(want) != 0 {
		t.Errorf("rows not found: %v", want)
	}
}

func TestFailOutranksNotVerified(t *testing.T) {
	for _, f := range Codes() {
		if f.Verdict() != Fail {
			continue
		}
		for _, nv := range Codes() {
			if nv.Verdict() != NotVerified {
				continue
			}
			t.Run(string(f)+"+"+string(nv), func(t *testing.T) {
				tl := Timeline{Windows: []Window{steady(), deploy(cond(nv), cond(f)), notBegun(Rollback)}, Now: at(40 * sec)}
				want := summary{
					Verdict: Fail,
					Results: []Result{ResultPass, ResultFail, NotRun},
					Reasons: []string{"deploy:" + string(f), "deploy:" + string(nv)},
					Decided: f,
				}
				if got := summarize(t, Decide(worked(tl))); !reflect.DeepEqual(got, want) {
					t.Errorf("got  %+v\nwant %+v", got, want)
				}
			})
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/verdict/ -run 'TestDecide|TestFailOutranks' -v`
Expected: FAIL: the package does not build, with `undefined: Window` and `undefined: Decide` among the errors.

- [ ] **Step 3: Implement `func Decide(t Timeline) Decision` in `internal/verdict/decide.go`**

Declare `Window`, `Timeline`, `Reason`, `WindowResult` and `Decision` as the Interfaces block gives them, each with its interface comment, and `Decide` with its interface comment.

Decide applies these rules, in this order:

1. If Before is not empty: verdict NotVerified; every window's result is not-run; the reasons are the Before conditions with no window; GoOn false.
2. If there is no window (and nothing in Before): one reason, InternalError, text "the run did not finish"; NotVerified; GoOn false.
3. Place each Timed condition: in the window whose [Start, End) holds its At, an open window holding everything from its Start to Now; one whose At is before the first window's Start goes to Before (then rule 1 applies); one whose At is at or after the last begun window's End goes to Ignored.
4. Judge each begun window (Start not zero) in order. Its conditions' codes give its result: for the steady window, any condition at all gives not-verified (rule 4); for a transition's window, any condition whose code's verdict is Fail gives fail, else any condition gives not-verified, else pass (rules 5 to 7). An open window (End zero) with no condition is "pass so far" for the result and is not counted as ended.
5. The first begun window whose result is not pass decides (rules 1 and 8): the verdict is that window's result as a Verdict; every begun window after it gets not-judged; every window that never began gets not-run. If no begun window fails to pass, the verdict is Pass and windows that never began are not-run.
6. Reasons: every condition of every begun window up to and including the deciding one, in window order and, inside a window, in the order of Codes(); each with its code's verdict and its window's name. Conditions in windows after the deciding one are not listed. The one marked Decided is, in the deciding window, the first in the order of Codes() among those whose code's verdict equals the run's verdict.
7. GoOn is true when the verdict so far is Pass.

Where the list leaves a choice, these hold:

- Windows have no gap between them (section 4). For placing, a begun window that the next begun window follows ends at that window's Start; the last begun window ends at its End, or at Now while it is open. "The last begun window's End" in rule 3 is that end.
- Rule 3 runs before rule 1 is checked. When no window has begun, every Timed condition goes to Before.
- Rule 2 also holds when windows are given and none of them has begun: a run that judged nothing never passes.
- Under rule 1, Before is the deciding window for rule 6: its conditions are listed in the order of Codes(), and Decided is chosen as rule 6 says.
- Sort stably. A code that Codes() does not hold sorts after every code it holds.
- In the deciding window, when no reason's code carries the run's verdict (the steady window is not verified whatever its conditions are), the first reason in the order of Codes() is Decided.
- `Decision.Windows` has one WindowResult for each window of t, in order, with its Name, Start and End as given.
- The steady window is the one named `Steady`.

Build Decide from two unexported steps, so that Task 14 adds one call between them:

```go
// placement is a Timeline with every condition in its place (rule 3).
type placement struct {
	before    []Condition    // t.Before, then Timed conditions earlier than the first window
	windows   [][]Condition  // the conditions of each window of t.Windows, in order
	ignored   []Condition    // Timed conditions at or after the last begun window's end
	ends      []time.Time    // where each begun window ends for placing; zero if it never began
	results   []WindowResult // Name, Start and End of each window; Task 14 adds the counts
	uncounted int            // set by Task 14
}

// place applies rule 3 and fills results with each window's Name, Start and End.
func place(t Timeline) placement

// judge applies rules 1, 2 and 4 to 7 to what place gave.
func judge(t Timeline, p placement) Decision

func Decide(t Timeline) Decision { return judge(t, place(t)) }
```

judge copies `p.results`, `p.ignored` and `p.uncounted` into the Decision and sets each result.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/verdict/ -v`
Expected: PASS, Task 12's tests included; `TestFailOutranksNotVerified` runs 72 subtests.

- [ ] **Step 5: Commit**

```bash
git add internal/verdict/decide.go internal/verdict/decide_test.go
git commit -m "Decide a run's verdict from placed conditions"
```

### Task 14: `verdict.Decide`: observations, counts and silence

**Files:**
- Create: `internal/verdict/observe.go`
- Modify: `internal/verdict/decide.go`
- Test: `internal/verdict/observe_test.go`

**Interfaces:**
- Consumes: `func Decide(t Timeline) Decision` and the types of Task 13; `type Observation struct { … }` (Task 12).
- Produces: `func Decide(t Timeline) Decision`
  As in Task 13, and now it also reads the observations: it counts them
  into windows by T, turns a failure into ProbeFailures in a transition's
  window and into SteadyNotClean in the steady window, and turns a stretch
  with no observation longer than Silence into ProbeSilent for every window
  the stretch overlaps.

- [ ] **Step 1: Write the failing tests**

Create `internal/verdict/observe_test.go`. It uses the helpers of `decide_test.go` (Task 13). The rows use Task 13's windows, steady [t0, t0+10s), deploy [t0+10s, t0+40s) finished at t0+25s and rollback [t0+40s, t0+70s), and a Silence of 5 s. `TestSilenceBelongsToEveryWindowItOverlaps` calls the unexported `place` and `observe`, because a condition in a window after the deciding one is never listed among the reasons.

```go
package verdict

import (
	"reflect"
	"slices"
	"testing"
	"time"
)

// passes gives one passing summary line a second, written at its T, from
// `from` up to but not including `to`: N 200, Ms 30, Lag 5.
func passes(from, to time.Duration) []Observation {
	var obs []Observation
	for d := from; d < to; d += sec {
		obs = append(obs, Observation{Written: at(d), T: at(d), OK: true, N: 200, Ms: 30, Lag: 5})
	}
	return obs
}

// failure is one failed check that began at began and whose line was
// written at written: Ms 12, Lag 2.
func failure(began, written time.Duration, class string) Observation {
	return Observation{Written: at(written), T: at(began), OK: false, N: 1, Class: class, Ms: 12, Lag: 2}
}

// deployFailures gives the 243 failures of section 10's failing example:
// 212 from 12.31 s, one every 50 ms, then 31 from 25 s (the deploy's
// Finished), one every 100 ms. The first 198 are refused, the other 45
// timed out.
func deployFailures() []Observation {
	var obs []Observation
	for i := range 243 {
		began := 12310*msec + time.Duration(i)*50*msec
		if i >= 212 {
			began = 25*sec + time.Duration(i-212)*100*msec
		}
		class := "refused"
		if i >= 198 {
			class = "timeout"
		}
		obs = append(obs, failure(began, began, class))
	}
	return obs
}

// timeline is a Timeline judged at now, with a Silence of 5 s.
func timeline(now time.Duration, windows []Window, obs ...[]Observation) Timeline {
	return Timeline{Windows: windows, Observations: slices.Concat(obs...), Silence: 5 * sec, Now: at(now)}
}

func withTimed(tl Timeline, cs ...Condition) Timeline { tl.Timed = cs; return tl }

func steadyOnly() []Window { return []Window{steady(), notBegun(Deploy), notBegun(Rollback)} }
func upToDeploy() []Window { return []Window{steady(), deploy(), notBegun(Rollback)} }
func allThree() []Window   { return []Window{steady(), deploy(), rollback()} }

// tally is the part of a WindowResult that counting fills.
type tally struct {
	Checks, Failed, AfterFinish, Crossed int
	ByClass                              map[string]int
	FirstFailure                         time.Time
	SlowestMs, LagMs                     float64
}

func tallyOf(w WindowResult) tally {
	got := tally{Checks: w.Checks, Failed: w.Failed, AfterFinish: w.AfterFinish, Crossed: w.Crossed,
		FirstFailure: w.FirstFailure, SlowestMs: w.SlowestMs, LagMs: w.LagMs}
	if len(w.ByClass) > 0 {
		got.ByClass = w.ByClass
	}
	return got
}

func TestObserve(t *testing.T) {
	clean := tally{Checks: 2000, SlowestMs: 30, LagMs: 5}
	pass3 := []Result{ResultPass, ResultPass, ResultPass}
	tests := []struct {
		name      string
		tl        Timeline
		want      summary
		tallies   map[WindowName]tally
		texts     map[string]string // "WINDOW:code" -> the reason's Text
		uncounted int
	}{{
		name:    "ten summary lines of N 200 in steady",
		tl:      timeline(10*sec, steadyOnly(), passes(0, 10*sec)),
		want:    summary{Verdict: Pass, Results: []Result{ResultPass, NotRun, NotRun}, GoOn: true},
		tallies: map[WindowName]tally{Steady: clean},
	}, {
		name: "a failure in steady",
		tl:   timeline(10*sec, steadyOnly(), passes(0, 10*sec), []Observation{failure(4500*msec, 4500*msec, "refused")}),
		want: summary{Verdict: NotVerified, Results: []Result{ResultNotVerified, NotRun, NotRun}, Reasons: []string{"steady:steady-not-clean"}, Decided: SteadyNotClean},
		tallies: map[WindowName]tally{Steady: {Checks: 2001, Failed: 1, ByClass: map[string]int{"refused": 1},
			FirstFailure: at(4500 * msec), SlowestMs: 30, LagMs: 5}},
		texts: map[string]string{"steady:steady-not-clean": "1 check failed (refused 1), first at 10:00:04.50"},
	}, {
		name: "243 failures in deploy, 31 of them after it finished",
		tl:   timeline(40*sec, upToDeploy(), passes(0, 40*sec), deployFailures()),
		want: summary{Verdict: Fail, Results: []Result{ResultPass, ResultFail, NotRun}, Reasons: []string{"deploy:probe-failures"}, Decided: ProbeFailures},
		tallies: map[WindowName]tally{Deploy: {Checks: 6243, Failed: 243, AfterFinish: 31, ByClass: map[string]int{"refused": 198, "timeout": 45},
			FirstFailure: at(12310 * msec), SlowestMs: 30, LagMs: 5}},
		texts: map[string]string{"deploy:probe-failures": "243 checks failed (refused 198, timeout 45), first at 10:00:12.31"},
	}, {
		name: "a failure that began before the deploy and was written in it",
		tl:   timeline(40*sec, upToDeploy(), passes(0, 40*sec), []Observation{failure(9800*msec, 10300*msec, "refused")}),
		want: summary{Verdict: NotVerified, Results: []Result{ResultNotVerified, NotJudged, NotRun}, Reasons: []string{"steady:steady-not-clean"}, Decided: SteadyNotClean},
		tallies: map[WindowName]tally{
			Steady: {Checks: 2001, Failed: 1, ByClass: map[string]int{"refused": 1}, FirstFailure: at(9800 * msec), SlowestMs: 30, LagMs: 5},
			Deploy: {Checks: 6000, Crossed: 1, SlowestMs: 30, LagMs: 5},
		},
		texts: map[string]string{"steady:steady-not-clean": "1 check failed (refused 1), first at 10:00:09.80"},
	}, {
		name: "a failure that began before the rollback and was written in it",
		tl:   timeline(70*sec, allThree(), passes(0, 70*sec), []Observation{failure(39900*msec, 40200*msec, "reset")}),
		want: summary{Verdict: Fail, Results: []Result{ResultPass, ResultFail, NotJudged}, Reasons: []string{"deploy:probe-failures"}, Decided: ProbeFailures},
		tallies: map[WindowName]tally{
			Deploy:   {Checks: 6001, Failed: 1, AfterFinish: 1, ByClass: map[string]int{"reset": 1}, FirstFailure: at(39900 * msec), SlowestMs: 30, LagMs: 5},
			Rollback: {Checks: 6000, Crossed: 1, SlowestMs: 30, LagMs: 5},
		},
		texts: map[string]string{"deploy:probe-failures": "1 check failed (reset 1), first at 10:00:39.90"},
	}, {
		name: "an observation that began before the steady window",
		tl: timeline(10*sec, steadyOnly(), passes(0, 10*sec),
			[]Observation{{Written: at(500 * msec), T: at(-500 * msec), OK: true, N: 50, Ms: 30, Lag: 5}}),
		want:      summary{Verdict: Pass, Results: []Result{ResultPass, NotRun, NotRun}, GoOn: true},
		tallies:   map[WindowName]tally{Steady: clean},
		uncounted: 1,
	}, {
		name:      "a failure that began at the last window's End is not counted",
		tl:        timeline(71*sec, allThree(), passes(0, 70*sec), []Observation{failure(70*sec, 70500*msec, "timeout")}),
		want:      summary{Verdict: Pass, Results: pass3, GoOn: true},
		tallies:   map[WindowName]tally{Rollback: {Checks: 6000, SlowestMs: 30, LagMs: 5}},
		uncounted: 1,
	}, {
		name: "a failure written during the drain that began in the last window",
		tl:   timeline(72*sec, allThree(), passes(0, 70*sec), []Observation{failure(69500*msec, 71500*msec, "timeout")}),
		want: summary{Verdict: Fail, Results: []Result{ResultPass, ResultPass, ResultFail}, Reasons: []string{"rollback:probe-failures"}, Decided: ProbeFailures},
		tallies: map[WindowName]tally{Rollback: {Checks: 6001, Failed: 1, AfterFinish: 1, ByClass: map[string]int{"timeout": 1},
			FirstFailure: at(69500 * msec), SlowestMs: 30, LagMs: 5}},
	}, {
		name:  "a 6 s gap in the middle of deploy",
		tl:    timeline(70*sec, allThree(), passes(0, 21*sec), passes(26*sec, 70*sec)),
		want:  summary{Verdict: NotVerified, Results: []Result{ResultPass, ResultNotVerified, NotJudged}, Reasons: []string{"deploy:probe-silent"}, Decided: ProbeSilent},
		texts: map[string]string{"deploy:probe-silent": "no observation for 6.0 s, from 10:00:20.00"},
	}, {
		name:  "a gap that starts in steady and ends in deploy; steady decides",
		tl:    timeline(70*sec, allThree(), passes(0, 8*sec), passes(13*sec, 70*sec)),
		want:  summary{Verdict: NotVerified, Results: []Result{ResultNotVerified, NotJudged, NotJudged}, Reasons: []string{"steady:probe-silent"}, Decided: ProbeSilent},
		texts: map[string]string{"steady:probe-silent": "no observation for 6.0 s, from 10:00:07.00"},
	}, {
		name:  "no observation after 33 s; the stretch to the window's End counts",
		tl:    timeline(40*sec, upToDeploy(), passes(0, 34*sec)),
		want:  summary{Verdict: NotVerified, Results: []Result{ResultPass, ResultNotVerified, NotRun}, Reasons: []string{"deploy:probe-silent"}, Decided: ProbeSilent},
		texts: map[string]string{"deploy:probe-silent": "no observation for 7.0 s, from 10:00:33.00"},
	}, {
		name:  "the second observation 6 s after the first, at the steady Start",
		tl:    timeline(10*sec, steadyOnly(), passes(0, 1*sec), passes(6*sec, 10*sec)),
		want:  summary{Verdict: NotVerified, Results: []Result{ResultNotVerified, NotRun, NotRun}, Reasons: []string{"steady:probe-silent"}, Decided: ProbeSilent},
		texts: map[string]string{"steady:probe-silent": "no observation for 6.0 s, from 10:00:00.00"},
	}, {
		name: "the probe exited at 33 s; no silence after the exit",
		tl:   withTimed(timeline(70*sec, allThree(), passes(0, 33*sec)), timed(ProbeExited, 33*sec)),
		want: summary{Verdict: NotVerified, Results: []Result{ResultPass, ResultNotVerified, NotJudged}, Reasons: []string{"deploy:probe-exited"}, Decided: ProbeExited},
	}, {
		name:  "silence before the exit still counts",
		tl:    withTimed(timeline(70*sec, allThree(), passes(0, 27*sec)), timed(ProbeExited, 33*sec)),
		want:  summary{Verdict: NotVerified, Results: []Result{ResultPass, ResultNotVerified, NotJudged}, Reasons: []string{"deploy:probe-exited", "deploy:probe-silent"}, Decided: ProbeExited},
		texts: map[string]string{"deploy:probe-silent": "no observation for 7.0 s, from 10:00:26.00"},
	}, {
		name: "a placed probe-exited ends silence from its window's Start",
		tl:   timeline(70*sec, []Window{steady(), deploy(cond(ProbeExited)), rollback()}, passes(0, 10*sec)),
		want: summary{Verdict: NotVerified, Results: []Result{ResultPass, ResultNotVerified, NotJudged}, Reasons: []string{"deploy:probe-exited"}, Decided: ProbeExited},
	}, {
		name: "a 4.9 s gap is not silence",
		tl: timeline(70*sec, allThree(), passes(0, 21*sec),
			[]Observation{{Written: at(24900 * msec), T: at(24900 * msec), OK: true, N: 1}}, passes(25*sec, 70*sec)),
		want: summary{Verdict: Pass, Results: pass3, GoOn: true},
	}, {
		name: "a gap of exactly limits.silence is not silence",
		tl:   timeline(70*sec, allThree(), passes(0, 21*sec), passes(25*sec, 70*sec)),
		want: summary{Verdict: Pass, Results: pass3, GoOn: true},
	}, {
		name: "slowest is the slowest pass; lag is the furthest of any line",
		tl: timeline(70*sec, allThree(), passes(0, 70*sec), []Observation{
			{Written: at(12500 * msec), T: at(12500 * msec), OK: true, N: 1, Ms: 41},
			{Written: at(13500 * msec), T: at(13500 * msec), OK: true, N: 1, Ms: 212},
			{Written: at(16500 * msec), T: at(14500 * msec), OK: false, N: 1, Class: "timeout", Ms: 1999, Lag: 38},
		}),
		want: summary{Verdict: Fail, Results: []Result{ResultPass, ResultFail, NotJudged}, Reasons: []string{"deploy:probe-failures"}, Decided: ProbeFailures},
		tallies: map[WindowName]tally{
			Steady: clean,
			Deploy: {Checks: 6003, Failed: 1, ByClass: map[string]int{"timeout": 1}, FirstFailure: at(14500 * msec), SlowestMs: 212, LagMs: 38},
		},
	}, {
		name: "classes with equal counts, and a failure with no class, in a deploy that stalled",
		tl: timeline(40*sec, []Window{steady(), stalled(deploy()), notBegun(Rollback)}, passes(0, 40*sec),
			[]Observation{failure(26*sec, 26*sec, "503"), failure(27*sec, 27*sec, "")}),
		want: summary{Verdict: Fail, Results: []Result{ResultPass, ResultFail, NotRun}, Reasons: []string{"deploy:probe-failures"}, Decided: ProbeFailures},
		tallies: map[WindowName]tally{Deploy: {Checks: 6002, Failed: 2, ByClass: map[string]int{"503": 1, "unclassified": 1},
			FirstFailure: at(26 * sec), SlowestMs: 30, LagMs: 5}},
		texts: map[string]string{"deploy:probe-failures": "2 checks failed (503 1, unclassified 1), first at 10:00:26.00"},
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := Decide(tc.tl)
			if got := summarize(t, d); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
			if d.Uncounted != tc.uncounted {
				t.Errorf("Uncounted = %d, want %d", d.Uncounted, tc.uncounted)
			}
			for _, w := range d.Windows {
				if want, ok := tc.tallies[w.Name]; ok {
					if got := tallyOf(w); !reflect.DeepEqual(got, want) {
						t.Errorf("%s:\ngot  %+v\nwant %+v", w.Name, got, want)
					}
				}
			}
			for _, r := range d.Reasons {
				if want, ok := tc.texts[string(r.Window)+":"+string(r.Code)]; ok && r.Text != want {
					t.Errorf("%s %s: text %q, want %q", r.Window, r.Code, r.Text, want)
				}
			}
		})
	}
}

func TestLateFailureJudgesItsWindowAgain(t *testing.T) {
	tl := timeline(55*sec, []Window{steady(), deploy(), inProgress(rollback())}, passes(0, 55*sec))
	before := summary{Verdict: Pass, Results: []Result{ResultPass, ResultPass, ResultPass}, GoOn: true}
	if got := summarize(t, Decide(tl)); !reflect.DeepEqual(got, before) {
		t.Fatalf("before the late failure:\ngot  %+v\nwant %+v", got, before)
	}

	tl.Observations = append(tl.Observations, failure(30*sec, 54*sec, "timeout"))
	after := summary{Verdict: Fail, Results: []Result{ResultPass, ResultFail, NotJudged}, Reasons: []string{"deploy:probe-failures"}, Decided: ProbeFailures}
	if got := summarize(t, Decide(tl)); !reflect.DeepEqual(got, after) {
		t.Errorf("after the late failure:\ngot  %+v\nwant %+v", got, after)
	}
}

func TestSilenceBelongsToEveryWindowItOverlaps(t *testing.T) {
	tl := timeline(70*sec, allThree(), passes(0, 8*sec), passes(13*sec, 70*sec))
	p := place(tl)
	observe(tl, &p)
	for i, want := range []bool{true, true, false} {
		got := slices.ContainsFunc(p.windows[i], func(c Condition) bool { return c.Code == ProbeSilent })
		if got != want {
			t.Errorf("%s: probe-silent %v, want %v", tl.Windows[i].Name, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/verdict/ -run 'TestObserve|TestLateFailure|TestSilence' -v`
Expected: FAIL: the package does not build: `undefined: observe`.

- [ ] **Step 3: Implement `func observe(t Timeline, p *placement)` in `internal/verdict/observe.go`, and call it from Decide**

In `decide.go`, Decide becomes:

```go
func Decide(t Timeline) Decision {
	p := place(t)
	observe(t, &p)
	return judge(t, p)
}
```

and its interface comment gains the second paragraph of Task 14's Interfaces block.

observe adds counts to `p.results`, conditions to `p.windows` and lines to `p.uncounted`. It does nothing when the steady window has not begun. Its rules:

1. **Assignment.** An observation belongs to the begun window i for which T is at or after `Windows[i].Start` and before `p.ends[i]` (Task 13's ends: an open window holds to Now, and a gap between windows belongs to the earlier one). One that no window holds is not counted: `uncounted` goes up by one line, not by N.
2. **Counts.** Checks adds N for a pass and 1 for a failure. A failure adds 1 to Failed and to `ByClass[Class]`, with the key "unclassified" when Class is empty. FirstFailure is the earliest T of a failure in the window.
3. **After finish.** AfterFinish counts the window's failures whose T is at or after the window's Finished, when Finished is not zero.
4. **Crossed.** A failure counted in window i adds 1 to Crossed of every later begun window j whose Start is after its T and at or before its Written. A failure that no window counts crosses nothing.
5. **Slowest and lag.** SlowestMs is the largest Ms among the window's passes; a failure's Ms does not count. LagMs is the largest Lag among all its observations.
6. **Failures.** A window with one or more failures gets a Condition with ProbeFailures (a transition's window) or SteadyNotClean (the steady window), with a zero At. Its text is "N checks failed (CLASS COUNT, CLASS COUNT), first at HH:MM:SS.cc", with "check" for one failure; classes are sorted by count, largest first, then by name; the time is FirstFailure in UTC with the layout `15:04:05.00`.
7. **Silence.** The measured stretch runs from the steady window's Start to the end of the last begun window as `p.ends` gives it, and ends earlier at the moment of the earliest ProbeExited in `p.windows`: its At, or its window's Start when At is zero. Take the Written times of all observations that lie in that stretch, sorted. Each piece between the stretch's start and the first of them, between two neighbours, and between the last of them and the stretch's end, that is longer than Silence (not equal), gives a Condition with ProbeSilent and a zero At to every begun window i it overlaps: the piece starts before `p.ends[i]` and ends after `Windows[i].Start`. Its text is "no observation for S s, from HH:MM:SS.cc", with S the piece's length in seconds and one decimal (`strconv.FormatFloat(d.Seconds(), 'f', 1, 64)`) and the time of the piece's start as in rule 6.
8. **Late results** need nothing more: Decide judges everything again at each call.

The order of `t.Observations` does not matter.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/verdict/ -v`
Expected: PASS, the tests of Tasks 12 and 13 included.

- [ ] **Step 5: Commit**

```bash
git add internal/verdict/observe.go internal/verdict/decide.go internal/verdict/observe_test.go
git commit -m "Count observations into windows, and judge failures and silence"
```

### Task 15: `probe.ParseLine`: the line contract

**Files:**
- Create: `internal/probe/line.go`
- Test: `internal/probe/line_test.go`

**Interfaces:**
- Consumes: `type Observation struct { Written, T time.Time; OK bool; N int; Class string; Ms, Lag float64 }` (Task 12).
- Produces: `module internal/probe`
  The probe contract of section 6 as the tool keeps it: reading a probe's
  lines, running a probe as a pod, and the built-in HTTP probe. It hides the
  pod, its log stream and the line format. It judges nothing: it hands
  observations and faults to its caller.
- Produces: `type Kind int`
  What one line of a probe's output is: Observed (it carries ok), Text (log
  text), JSONText (a JSON object with no ok field) or Invalid (it breaks the
  contract).
- Produces: `type Line struct { Kind Kind; Obs verdict.Observation; Fault string }`
  One line as the contract reads it. Obs is set for Observed. Fault says,
  for Invalid, which rule the line broke; it names the field and never
  quotes the line.
- Produces: `func ParseLine(raw string, written time.Time) Line`
  Reads one whole line of a probe's output. raw has no line end. written is
  the runtime's timestamp of the line; it becomes the observation's Written
  and, when the line has no t, its T. A t later than written is Invalid.

- [ ] **Step 1: Write the failing test**

Create `internal/probe/line_test.go`. Every Invalid row also checks that the Fault does not quote the line.

```go
package probe

import (
	"strings"
	"testing"
	"time"

	"rollproof/internal/verdict"
)

func TestParseLine(t *testing.T) {
	written := time.Date(2026, 11, 13, 10, 0, 7, 500_000_000, time.UTC)
	given := time.Date(2026, 11, 13, 10, 0, 7, 120_000_000, time.UTC)
	pass := verdict.Observation{Written: written, T: written, OK: true, N: 1}
	tests := []struct {
		raw   string
		kind  Kind
		obs   verdict.Observation // for Observed
		fault string              // for Invalid: a part of Fault
	}{
		{raw: `{"ok":true}`, kind: Observed, obs: pass},
		{raw: `{"ok":false,"class":"refused","ms":12.5,"lag":3}`, kind: Observed,
			obs: verdict.Observation{Written: written, T: written, N: 1, Class: "refused", Ms: 12.5, Lag: 3}},
		{raw: `   {"ok":true}`, kind: Observed, obs: pass},
		{raw: "\t{\"ok\":true}   ", kind: Observed, obs: pass},
		{raw: "{\"ok\":true}\r", kind: Observed, obs: pass},
		{raw: `{"ok":true,"n":200,"t":"2026-11-13T10:00:07.120Z","ms":41,"lag":6}`, kind: Observed,
			obs: verdict.Observation{Written: written, T: given, OK: true, N: 200, Ms: 41, Lag: 6}},
		{raw: `{"ok":true,"t":"2026-11-13T11:00:07.12+01:00"}`, kind: Observed,
			obs: verdict.Observation{Written: written, T: given, OK: true, N: 1}},
		{raw: `{"ok":true,"t":"2026-11-13T10:00:07.5Z"}`, kind: Observed, obs: pass},
		{raw: `{"ok":true,"ms":0,"lag":0}`, kind: Observed, obs: pass},

		// Log text.
		{raw: `starting`, kind: Text},
		{raw: `2026/11/13 10:00:00 listening`, kind: Text},
		{raw: `  ok: ready`, kind: Text},
		{raw: `the word "ok" and no colon`, kind: Text},
		{raw: ``, kind: Text},
		{raw: `   `, kind: Text},

		// An observation behind a prefix.
		{raw: `2026-11-13 10:00:07 {"ok":false}`, kind: Invalid, fault: "behind a prefix"},
		{raw: `INFO "ok" : true`, kind: Invalid, fault: "behind a prefix"},
		{raw: "level=info \"ok\"\t:false", kind: Invalid, fault: "behind a prefix"},

		// Not one JSON object.
		{raw: `{"ok":tru`, kind: Invalid, fault: "not one JSON object"},
		{raw: `{"ok":true} trailing`, kind: Invalid, fault: "not one JSON object"},
		{raw: `{"ok":true}{"ok":true}`, kind: Invalid, fault: "not one JSON object"},
		{raw: `{`, kind: Invalid, fault: "not one JSON object"},

		// JSON objects with no top-level ok are log text.
		{raw: `{"level":"info","msg":"x"}`, kind: JSONText},
		{raw: `{"detail":{"ok":true}}`, kind: JSONText},
		{raw: `{"msg":"the \"ok\": field"}`, kind: JSONText},
		{raw: `{}`, kind: JSONText},

		// ok.
		{raw: `{"ok":"yes"}`, kind: Invalid, fault: "ok is not"},
		{raw: `{"ok":1}`, kind: Invalid, fault: "ok is not"},
		{raw: `{"ok":null}`, kind: Invalid, fault: "ok is not"},
		{raw: `{"ok":"S3CRET-VALUE"}`, kind: Invalid, fault: "ok is not"},

		// n.
		{raw: `{"ok":true,"n":0}`, kind: Invalid, fault: "n is not an integer of 1 or more"},
		{raw: `{"ok":true,"n":-1}`, kind: Invalid, fault: "n is not an integer of 1 or more"},
		{raw: `{"ok":true,"n":1.5}`, kind: Invalid, fault: "n is not an integer of 1 or more"},
		{raw: `{"ok":true,"n":"3"}`, kind: Invalid, fault: "n is not an integer of 1 or more"},
		{raw: `{"ok":true,"n":null}`, kind: Invalid, fault: "n is not an integer of 1 or more"},
		{raw: `{"ok":false,"n":1}`, kind: Invalid, fault: "n is not allowed beside ok false"},
		{raw: `{"ok":false,"n":5}`, kind: Invalid, fault: "n is not allowed beside ok false"},

		// t.
		{raw: `{"ok":true,"t":12}`, kind: Invalid, fault: "t is not a string"},
		{raw: `{"ok":true,"t":"yesterday"}`, kind: Invalid, fault: "t is not RFC 3339"},
		{raw: `{"ok":true,"t":"2026-11-13T10:00:07"}`, kind: Invalid, fault: "t is not RFC 3339"},
		{raw: `{"ok":true,"t":"2026-11-13T10:00:07.500000001Z"}`, kind: Invalid, fault: "t is later than the line"},
		{raw: `{"ok":false,"t":"2026-11-14T10:00:00Z"}`, kind: Invalid, fault: "t is later than the line"},

		// class, ms, lag.
		{raw: `{"ok":false,"class":5}`, kind: Invalid, fault: "class is not a string"},
		{raw: `{"ok":true,"ms":-1}`, kind: Invalid, fault: "ms is not a number of 0 or more"},
		{raw: `{"ok":true,"ms":"41"}`, kind: Invalid, fault: "ms is not a number of 0 or more"},
		{raw: `{"ok":true,"ms":null}`, kind: Invalid, fault: "ms is not a number of 0 or more"},
		{raw: `{"ok":true,"lag":-0.5}`, kind: Invalid, fault: "lag is not a number of 0 or more"},
		{raw: `{"ok":true,"lag":true}`, kind: Invalid, fault: "lag is not a number of 0 or more"},

		// detail is never read.
		{raw: `{"ok":true,"detail":"text"}`, kind: Observed, obs: pass},
		{raw: `{"ok":true,"detail":[1,{"ok":"x"}]}`, kind: Observed, obs: pass},
		{raw: `{"ok":true,"detail":null}`, kind: Observed, obs: pass},

		// v.
		{raw: `{"ok":true,"v":1}`, kind: Observed, obs: pass},
		{raw: `{"ok":true,"v":2}`, kind: Invalid, fault: "v is not 1"},
		{raw: `{"ok":true,"v":"1"}`, kind: Invalid, fault: "v is not 1"},
		{raw: `{"ok":true,"v":1.5}`, kind: Invalid, fault: "v is not 1"},

		// A top-level name the tool does not know is ignored.
		{raw: `{"ok":true,"extra":1}`, kind: Observed, obs: pass},

		// A top-level name that is there twice makes the line invalid, so
		// that a failure cannot be hidden behind a later "ok": true. Inside
		// detail the tool reads nothing, so a repeated name there is not seen.
		{raw: `{"ok":false,"ok":true}`, kind: Invalid, fault: "a top-level name is there twice"},
		{raw: `{"ok":true,"class":"a","class":"b"}`, kind: Invalid, fault: "a top-level name is there twice"},
		{raw: `{"ok":true,"detail":{"a":1,"a":2}}`, kind: Observed, obs: pass},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			got := ParseLine(tc.raw, written)
			if got.Kind != tc.kind {
				t.Fatalf("Kind = %v, want %v (fault %q)", got.Kind, tc.kind, got.Fault)
			}
			switch tc.kind {
			case Observed:
				o := got.Obs
				if !o.Written.Equal(tc.obs.Written) || !o.T.Equal(tc.obs.T) || o.OK != tc.obs.OK ||
					o.N != tc.obs.N || o.Class != tc.obs.Class || o.Ms != tc.obs.Ms || o.Lag != tc.obs.Lag {
					t.Errorf("Obs = %+v, want %+v", o, tc.obs)
				}
			case Invalid:
				if !strings.Contains(got.Fault, tc.fault) {
					t.Errorf("Fault = %q, want it to hold %q", got.Fault, tc.fault)
				}
				if strings.Contains(got.Fault, strings.TrimSpace(tc.raw)) || strings.Contains(got.Fault, "S3CRET") {
					t.Errorf("Fault %q quotes the line", got.Fault)
				}
			default:
				if got.Fault != "" || got.Obs != (verdict.Observation{}) {
					t.Errorf("a %v line carries %+v", got.Kind, got)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/probe/ -run TestParseLine -v`
Expected: FAIL: the package does not build: `undefined: ParseLine` among the errors.

- [ ] **Step 3: Implement `func ParseLine(raw string, written time.Time) Line` in `internal/probe/line.go`**

Its package comment is the text of `module internal/probe` in the Interfaces block. `Kind`, its four constants, `Line` and `ParseLine` get their interface comments from the Interfaces block. The constants start at `iota + 1`, so that the zero Kind is no kind.

ParseLine reads a line in this order and stops at the first fault:

1. Skip leading white space (`strings.TrimLeftFunc(raw, unicode.IsSpace)`).
2. A line that does not start with `{` is Text, unless it matches the regular expression `"ok"\s*:`; then it is Invalid with the Fault `an observation behind a prefix: "ok" and a colon in log text`.
3. Decode the line with a `json.Decoder` into `map[string]json.RawMessage`. A decode error, or a `dec.Token()` after it that does not return `io.EOF`, is Invalid: `the line is not one JSON object`. Then walk the same text's top-level names with a second decoder's `Token` calls (the opening brace, then a name and one skipped value at a time, the value read with `Decode` into a `json.RawMessage`): a name that occurs twice is Invalid: `a top-level name is there twice`. `encoding/json` would keep the last of them without a word.
4. With no top-level `ok`, the line is JSONText.
5. `json.Unmarshal` of `null` into a bool, an int, a float64 or a string succeeds and changes nothing, so a known field whose raw value is `null` is a fault of that field. `detail` and every name the contract does not know are never read.
6. Check the known fields in the order of section 6's table. Each fault is exactly this text:

| Field | Rule | Fault |
| --- | --- | --- |
| `ok` | `true` or `false` | `ok is not true or false` |
| `t` | a string | `t is not a string` |
| `t` | `time.Parse(time.RFC3339, …)` succeeds, so a zone is required | `t is not RFC 3339 with a zone` |
| `t` | not after written | `t is later than the line` |
| `n` | an integer, 1 or more | `n is not an integer of 1 or more` |
| `n` | absent when ok is false | `n is not allowed beside ok false` |
| `class` | a string | `class is not a string` |
| `ms` | a number, 0 or more | `ms is not a number of 0 or more` |
| `lag` | a number, 0 or more | `lag is not a number of 0 or more` |
| `v` | the integer 1 | `v is not 1, the one version of the contract this tool reads` |

7. Otherwise the line is Observed, with `Written: written`, T from `t` or else written, OK, N from `n` or else 1, and Class, Ms and Lag as given.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/probe/ -run TestParseLine -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/probe/line.go internal/probe/line_test.go
git commit -m "Read a probe's lines as the contract says"
```

### Task 16: The built-in HTTP probe

**Files:**
- Create: `internal/probe/http.go`, `internal/probe/httpargs.go`
- Test: `internal/probe/http_test.go`, `internal/probe/httpargs_test.go`

**Interfaces:**
- Consumes: `func ParseLine(raw string, written time.Time) Line` (Task 15), in tests, to read back what the probe wrote.
- Produces: `type HTTPOptions struct { URL string; Headers map[string]string; Rate int; Timeout time.Duration; ExpectMin, ExpectMax int }`
  The settings of the built-in probe. Rate is requests a second, 1 or more.
  ExpectMin and ExpectMax are the status range that passes, both ends
  included. A Host entry of Headers sets the host the request names.
- Produces: `func RunHTTP(ctx context.Context, o HTTPOptions, out io.Writer) error`
  Sends GET requests to o.URL on an open schedule, each on a new
  connection, and writes contract lines to out until ctx ends. Then it
  starts no new request, waits for those in flight, writes their lines and
  returns nil. It returns an error only when the URL's host cannot be
  resolved at the start.
- Produces: `func HTTPArgs(o HTTPOptions) []string`
  The arguments that make the binary's probe http command run o: what the
  probe's pod is started with.
- Produces: `func ParseHTTPArgs(args []string) (HTTPOptions, error)`
  The reverse of HTTPArgs, for the command itself. For every o,
  ParseHTTPArgs(HTTPArgs(o)) gives o back.

- [ ] **Step 1: Write the failing tests for the arguments**

Create `internal/probe/httpargs_test.go`. HTTPArgs gives the pod's whole argument list, which starts with `probe http` because the image's entrypoint is the binary itself (Task 22); ParseHTTPArgs reads that same list, so the round trip is `ParseHTTPArgs(HTTPArgs(o))` as the Interfaces block says.

```go
package probe

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHTTPArgs(t *testing.T) {
	o := HTTPOptions{
		URL:       "http://orders.orders.svc/api/orders/latest",
		Headers:   map[string]string{"X-Query": "a=b", "Host": "orders.example"},
		Rate:      50,
		Timeout:   500 * time.Millisecond,
		ExpectMin: 200,
		ExpectMax: 200,
	}
	want := []string{"probe", "http",
		"--url", "http://orders.orders.svc/api/orders/latest",
		"--rate", "50",
		"--timeout", "500ms",
		"--expect-status", "200-200",
		"--header", "Host=orders.example",
		"--header", "X-Query=a=b",
	}
	if got := HTTPArgs(o); !reflect.DeepEqual(got, want) {
		t.Errorf("HTTPArgs:\ngot  %q\nwant %q", got, want)
	}
}

func TestHTTPArgsRoundTrip(t *testing.T) {
	for _, o := range []HTTPOptions{
		{URL: "http://orders.orders.svc/api/orders/latest", Rate: 200, Timeout: 2 * time.Second, ExpectMin: 200, ExpectMax: 299},
		{URL: "http://orders.orders.svc/", Headers: map[string]string{"Host": "orders.example", "X-Query": "a=b"},
			Rate: 50, Timeout: 500 * time.Millisecond, ExpectMin: 200, ExpectMax: 200},
		{URL: "https://orders.orders.svc:8443/health?full=1", Rate: 1, Timeout: 90 * time.Second, ExpectMin: 200, ExpectMax: 399},
	} {
		got, err := ParseHTTPArgs(HTTPArgs(o))
		if err != nil {
			t.Errorf("ParseHTTPArgs(HTTPArgs(%+v)): %v", o, err)
			continue
		}
		if !reflect.DeepEqual(got, o) {
			t.Errorf("round trip:\ngot  %+v\nwant %+v", got, o)
		}
	}
}

func TestParseHTTPArgsRejects(t *testing.T) {
	base := []string{"probe", "http", "--url", "http://orders.orders.svc/", "--rate", "200", "--timeout", "2s", "--expect-status", "200-299"}
	// with gives base with the value after flag replaced, or without the flag when value is "".
	with := func(flag, value string) []string {
		var args []string
		for i := 0; i < len(base); i++ {
			if base[i] == flag {
				if value != "" {
					args = append(args, flag, value)
				}
				i++
				continue
			}
			args = append(args, base[i])
		}
		return args
	}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"no --url", with("--url", "")},
		{"rate 0", with("--rate", "0")},
		{"a negative rate", with("--rate", "-5")},
		{"a timeout that does not parse", with("--timeout", "2 s")},
		{"a timeout of zero", with("--timeout", "0s")},
		{"expect-status with min above max", with("--expect-status", "299-200")},
		{"expect-status that is not a range", with("--expect-status", "2xx")},
		{"expect-status with one code", with("--expect-status", "200")},
		{"a header with no =", append(with("--url", "http://orders.orders.svc/"), "--header", "Host")},
		{"a flag it does not know", append(with("--url", "http://orders.orders.svc/"), "--keep-alive")},
		{"not the probe http command", base[2:]},
	} {
		if _, err := ParseHTTPArgs(tc.args); err == nil {
			t.Errorf("%s: %q was accepted", tc.name, strings.Join(tc.args, " "))
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/probe/ -run 'TestHTTPArgs|TestParseHTTPArgs' -v`
Expected: FAIL: the package does not build: `undefined: HTTPOptions` among the errors.

- [ ] **Step 3: Implement `internal/probe/httpargs.go`**

`type HTTPOptions`, `func HTTPArgs(o HTTPOptions) []string` and `func ParseHTTPArgs(args []string) (HTTPOptions, error)`, each with its interface comment from the Interfaces block.

- HTTPArgs: `probe`, `http`, then `--url`, `--rate` (`strconv.Itoa`), `--timeout` (`Duration.String()`), `--expect-status MIN-MAX`, then one `--header NAME=VALUE` for each header, names sorted.
- ParseHTTPArgs: args must start with `probe`, `http`; the rest goes to a `flag.FlagSet` named "probe http" with `flag.ContinueOnError` and its output set to `io.Discard`. `--header` is a `flag.Value` that may repeat and splits at the first `=`. It returns an error for: no `--url`; a rate below 1; a timeout that `time.ParseDuration` rejects or that is not above zero; an expect-status that is not two integers joined by one hyphen, or whose first is above its second; a header with no `=` or an empty name; a flag it does not know; an argument left over. Headers is nil when no `--header` is given. An error names the flag.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/probe/ -run 'TestHTTPArgs|TestParseHTTPArgs' -v`
Expected: PASS

- [ ] **Step 5: Write the failing tests for the probe**

Create `internal/probe/http_test.go`. Every test runs against a server in the same process on the loopback interface. No test calls `t.Parallel`: two of them replace the package variable `lookupHost`.

```go
package probe

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"rollproof/internal/verdict"
)

// capture is the io.Writer the tests give RunHTTP. It keeps each Write with
// the moment it was made.
type capture struct {
	mu  sync.Mutex
	got []capturedWrite
}

type capturedWrite struct {
	at   time.Time
	text string
}

func (c *capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, capturedWrite{time.Now(), string(p)})
	return len(p), nil
}

// observations fails the test unless every Write was one whole line that
// ParseLine reads as an observation, with the Write's moment as written.
func (c *capture) observations(t *testing.T) []verdict.Observation {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	var obs []verdict.Observation
	for _, s := range c.got {
		text, ok := strings.CutSuffix(s.text, "\n")
		if !ok || strings.Contains(text, "\n") {
			t.Fatalf("a Write was not one line: %q", s.text)
		}
		line := ParseLine(text, s.at)
		if line.Kind != Observed {
			t.Fatalf("not an observation (%s): %q", line.Fault, text)
		}
		obs = append(obs, line.Obs)
	}
	return obs
}

// testOptions are the settings of these tests: rate 50, timeout 2 s, 200-299.
func testOptions(url string) HTTPOptions {
	return HTTPOptions{URL: url, Rate: 50, Timeout: 2 * time.Second, ExpectMin: 200, ExpectMax: 299}
}

// runFor runs RunHTTP for d and then cancels its context. It fails the test
// unless RunHTTP returns nil within o.Timeout plus 1 s of the cancel.
func runFor(t *testing.T, o HTTPOptions, d time.Duration) *capture {
	t.Helper()
	out := &capture{}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- RunHTTP(ctx, o, out) }()
	time.Sleep(d)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunHTTP: %v", err)
		}
	case <-time.After(o.Timeout + time.Second):
		t.Fatalf("RunHTTP did not return within %v of the cancel", o.Timeout+time.Second)
	}
	return out
}

// checks is the number of checks that obs stands for.
func checks(obs []verdict.Observation) int {
	n := 0
	for _, o := range obs {
		n += o.N
	}
	return n
}

func TestRunHTTPOpenModel(t *testing.T) {
	var started atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		started.Add(1)
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()
	obs := runFor(t, testOptions(srv.URL), time.Second).observations(t)
	if n := started.Load(); n < 45 || n > 55 {
		t.Errorf("%d requests started in 1 s at rate 50, want 45 to 55", n)
	}
	if n := checks(obs); int64(n) != started.Load() {
		t.Errorf("the lines stand for %d checks, the server saw %d", n, started.Load())
	}
}

func TestRunHTTPLag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	obs := runFor(t, testOptions(srv.URL), 1500*time.Millisecond).observations(t)
	if len(obs) == 0 {
		t.Fatal("no line")
	}
	for _, o := range obs {
		if !o.OK || o.Lag >= 50 {
			t.Errorf("got %+v, want a pass with lag under 50 ms", o)
		}
	}
}

func TestRunHTTPNewConnectionForEachRequest(t *testing.T) {
	var mu sync.Mutex
	requests, addrs := 0, map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests++
		addrs[r.RemoteAddr] = true
	}))
	defer srv.Close()
	runFor(t, testOptions(srv.URL), 500*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if requests < 20 || len(addrs) != requests {
		t.Errorf("%d requests came from %d remote addresses, want one address for each", requests, len(addrs))
	}
}

func TestRunHTTPFailureClasses(t *testing.T) {
	hijacked := func(w http.ResponseWriter) net.Conn {
		conn, _, _ := w.(http.Hijacker).Hijack()
		return conn
	}
	var redirected atomic.Int64
	tests := []struct {
		name    string
		class   string
		handler http.HandlerFunc // nil: a port on which nothing listens
	}{
		{"a status outside the range", "503", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}},
		{"a redirect is judged and not followed", "302", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/elsewhere" {
				redirected.Add(1)
				return
			}
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		}},
		{"connection refused", "refused", nil},
		{"connection reset", "reset", func(w http.ResponseWriter, _ *http.Request) {
			conn := hijacked(w)
			conn.(*net.TCPConn).SetLinger(0)
			conn.Close()
		}},
		{"a body cut short after the headers", "eof", func(w http.ResponseWriter, _ *http.Request) {
			conn := hijacked(w)
			conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nshort"))
			conn.Close()
		}},
		{"no answer within the timeout", "timeout", func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}},
		{"headers, then a body that stalls past the timeout", "timeout", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}},
		{"an answer that is not HTTP", "other", func(w http.ResponseWriter, _ *http.Request) {
			conn := hijacked(w)
			conn.Write([]byte("not http\r\n\r\n"))
			conn.Close()
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var url string
			if tc.handler == nil {
				ln, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				url = "http://" + ln.Addr().String() + "/"
				ln.Close()
			} else {
				srv := httptest.NewServer(tc.handler)
				defer srv.Close()
				url = srv.URL + "/"
			}
			o := testOptions(url)
			o.Rate, o.Timeout = 20, 300*time.Millisecond
			out := runFor(t, o, 300*time.Millisecond)
			for _, s := range out.got {
				var fields map[string]any
				if err := json.Unmarshal([]byte(s.text), &fields); err != nil {
					t.Fatalf("%q: %v", s.text, err)
				}
				keys := slices.Sorted(maps.Keys(fields))
				if !slices.Equal(keys, []string{"class", "lag", "ms", "ok", "t"}) {
					t.Errorf("a failure line has the fields %v, want class, lag, ms, ok and t", keys)
				}
			}
			obs := out.observations(t)
			if len(obs) == 0 {
				t.Fatal("no line")
			}
			for _, ob := range obs {
				if ob.OK || ob.Class != tc.class {
					t.Errorf("got %+v, want a failure of class %q", ob, tc.class)
				}
			}
		})
	}
	if n := redirected.Load(); n != 0 {
		t.Errorf("the redirect was followed %d times", n)
	}
}

func TestRunHTTPSummaryLines(t *testing.T) {
	var answered atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		if answered.Add(1) == 10 {
			time.Sleep(120 * time.Millisecond)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}))
	defer srv.Close()
	obs := runFor(t, testOptions(srv.URL), 2500*time.Millisecond).observations(t)
	if len(obs) < 2 || len(obs) > 4 {
		t.Errorf("%d summary lines in 2.5 s, want 2 to 4", len(obs))
	}
	slowest := 0.0
	for i, o := range obs {
		if !o.OK || o.N < 1 {
			t.Errorf("got %+v, want a summary line", o)
		}
		if i > 0 && !o.T.After(obs[i-1].T) {
			t.Errorf("summary line %d begins at %v, not after the line before it", i, o.T)
		}
		slowest = max(slowest, o.Ms)
	}
	if slowest < 120 {
		t.Errorf("the longest ms is %v, want at least 120", slowest)
	}
	if n := checks(obs); int64(n) != answered.Load() {
		t.Errorf("the lines stand for %d checks, the server answered %d", n, answered.Load())
	}
}

func TestRunHTTPHeadersAndHost(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, r.Host+" "+r.Header.Get("X-Forwarded-Proto")+" "+r.Header.Get("X-Query"))
	}))
	defer srv.Close()
	o := testOptions(srv.URL)
	o.Headers = map[string]string{"hOsT": "orders.example", "X-Forwarded-Proto": "https", "X-Query": "a=b"}
	// A second Host header would make the server answer 400, which fails the line.
	for _, ob := range runFor(t, o, 500*time.Millisecond).observations(t) {
		if !ob.OK {
			t.Errorf("got %+v, want a pass", ob)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 {
		t.Fatal("no request arrived")
	}
	for _, s := range seen {
		if s != "orders.example https a=b" {
			t.Errorf("the server received %q, want %q", s, "orders.example https a=b")
		}
	}
}

func TestRunHTTPSWithoutCertificateCheck(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	obs := runFor(t, testOptions(srv.URL), 500*time.Millisecond).observations(t)
	if len(obs) == 0 {
		t.Fatal("no line")
	}
	for _, o := range obs {
		if !o.OK {
			t.Errorf("got %+v, want a pass", o)
		}
	}
}

func TestRunHTTPResolvesOnce(t *testing.T) {
	var mu sync.Mutex
	var hosts, serverNames []string
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		hosts = append(hosts, r.Host)
	}))
	srv.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		mu.Lock()
		defer mu.Unlock()
		serverNames = append(serverNames, hello.ServerName)
		return nil, nil
	}}
	srv.StartTLS()
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	var calls atomic.Int64
	defer func(saved func(context.Context, string) ([]string, error)) { lookupHost = saved }(lookupHost)
	lookupHost = func(_ context.Context, host string) ([]string, error) {
		calls.Add(1)
		if host != "probe-target.test" {
			return nil, errors.New("unexpected host " + host)
		}
		return []string{"127.0.0.1"}, nil
	}
	obs := runFor(t, testOptions("https://probe-target.test:"+port+"/"), time.Second).observations(t)
	if n := calls.Load(); n != 1 {
		t.Errorf("the host was looked up %d times, want 1", n)
	}
	if checks(obs) < 40 {
		t.Errorf("%d checks, want about 50", checks(obs))
	}
	mu.Lock()
	defer mu.Unlock()
	for _, h := range hosts {
		if h != "probe-target.test:"+port {
			t.Errorf("Host %q, want %q", h, "probe-target.test:"+port)
		}
	}
	for _, n := range serverNames {
		if n != "probe-target.test" {
			t.Errorf("TLS server name %q, want probe-target.test", n)
		}
	}
}

func TestRunHTTPUnresolvableHost(t *testing.T) {
	defer func(saved func(context.Context, string) ([]string, error)) { lookupHost = saved }(lookupHost)
	lookupHost = func(context.Context, string) ([]string, error) { return nil, errors.New("no such host") }
	out := &capture{}
	if err := RunHTTP(t.Context(), testOptions("http://nowhere.test/"), out); err == nil {
		t.Error("RunHTTP returned nil for a host that does not resolve")
	}
	if len(out.got) != 0 {
		t.Errorf("RunHTTP wrote %d lines", len(out.got))
	}
}

func TestRunHTTPDrainsOnCancel(t *testing.T) {
	var started atomic.Int64
	var lastStart atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		started.Add(1)
		lastStart.Store(time.Now().UnixNano())
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()
	o := testOptions(srv.URL)
	o.Timeout = time.Second
	cancelled := time.Now().Add(time.Second)
	obs := runFor(t, o, time.Second).observations(t)
	if late := time.Unix(0, lastStart.Load()).Sub(cancelled); late > 100*time.Millisecond {
		t.Errorf("a request reached the server %v after the cancel", late)
	}
	if n := checks(obs); int64(n) != started.Load() {
		t.Errorf("the lines stand for %d checks, %d requests started", n, started.Load())
	}
}

func TestRunHTTPNeverSilentWhileItWorks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(600 * time.Millisecond)
	}))
	defer srv.Close()
	o := testOptions(srv.URL)
	o.Timeout = time.Second
	began := time.Now()
	out := runFor(t, o, 3*time.Second)
	out.observations(t)
	bound := o.Timeout + time.Second
	previous := began
	for _, s := range out.got {
		if gap := s.at.Sub(previous); gap > bound {
			t.Errorf("no line for %v, want at most %v", gap, bound)
		}
		previous = s.at
	}
	if len(out.got) == 0 {
		t.Error("no line")
	}
}

func TestRunHTTPSendsQueryAndPort(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, r.URL.Path+"?"+r.URL.RawQuery)
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	runFor(t, testOptions("http://127.0.0.1:"+port+"/work?ms=20"), 300*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 {
		t.Fatal("no request arrived")
	}
	for _, s := range seen {
		if s != "/work?ms=20" {
			t.Errorf("the server saw %q, want /work?ms=20", s)
		}
	}
}
```

- [ ] **Step 6: Run the tests to verify they fail**

Run: `go test ./internal/probe/ -run TestRunHTTP -v`
Expected: FAIL: the package does not build: `undefined: RunHTTP` among the errors.

- [ ] **Step 7: Implement `func RunHTTP(ctx context.Context, o HTTPOptions, out io.Writer) error` in `internal/probe/http.go`**

With its interface comment from the Interfaces block. Start from the experiment's load generator, `RESEARCH/2026-11-12/L5/src/loadgen/main.go` from the repository root: its open-model loop, its `classify` function, unchanged, and its transport settings. Put a comment above `classify` that says it comes from that file of the host repository, the load generator of the experiment L5.

1. **Start.** Parse o.URL. The port is the URL's, or 80 for http and 443 for https. Call `lookupHost(ctx, u.Hostname())` once, where `var lookupHost = func(ctx context.Context, host string) ([]string, error)` is an unexported package variable that calls `net.DefaultResolver.LookupHost`. An error, or no address, is returned before any line is written.
2. **Transport.** `DialContext` ignores the address it is given and dials the first address found and the port, with a `net.Dialer` whose Timeout is o.Timeout. `DisableKeepAlives: true`, `DisableCompression: true`, no proxy, and `TLSClientConfig: &tls.Config{InsecureSkipVerify: true}` with ServerName left empty, so that the TLS server name and the Host header carry the URL's host. The client's `CheckRedirect` returns `http.ErrUseLastResponse`; the client has no Timeout of its own.
3. **Schedule.** Request i is due at `start.Add(time.Duration(i) * time.Second / time.Duration(o.Rate))`. Wait for it in one `select` with ctx.Done(); once ctx has ended, start no request. Each request runs in its own goroutine, counted by a `sync.WaitGroup`.
4. **One request.** began is `time.Now()` when it starts; its lag is began minus its due time. Its context is `context.WithTimeout(context.WithoutCancel(ctx), o.Timeout)`, so that the end of ctx stops the schedule and not the requests in flight. A GET of o.URL carries every entry of o.Headers, except that a name equal to "Host" in any letter case sets `req.Host` instead. The body is read to its end with `io.Copy(io.Discard, resp.Body)` under the same timeout. Class: an error of the request or of the body gives `classify(err)`; else a status outside [ExpectMin, ExpectMax] gives `strconv.Itoa(status)`; else the request passed. Its ms is the time from began to that moment.
5. **Lines.** One JSON object with the fields in this order: `ok`, `n` (omitted when zero, so never on a failure), `t`, `class` (omitted when empty), `ms`, `lag`. `t` is `began.UTC().Format(time.RFC3339Nano)`; ms and lag are milliseconds, `float64(d.Microseconds()) / 1000`. A failure's line is written as soon as it ends. Passes are gathered by the second of the run in which they ended: second k is [start + k s, start + (k+1) s). At each second boundary (a `time.Ticker` started with the schedule), one summary line is written for each gathered second that has ended, oldest first: n is the number of passes, t the earliest began, ms the longest, lag the furthest.
6. **Writing.** Each line is the JSON and "\n" in one `out.Write` call, under a mutex. Nothing is buffered.
7. **End.** When ctx has ended: wait for the requests in flight, write the summary lines of every second still gathered, and return nil.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/probe/ -v -count=1`
Expected: PASS, Task 15's test included. The run takes about 20 s.

- [ ] **Step 9: Commit**

```bash
git add internal/probe/http.go internal/probe/httpargs.go internal/probe/http_test.go internal/probe/httpargs_test.go
git commit -m "Add the built-in HTTP probe: an open-model load that writes contract lines"
```
