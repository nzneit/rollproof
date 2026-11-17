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

func notBegun(name WindowName, cs ...Condition) Window { return Window{Name: name, Conditions: cs} }

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
		want: summary{Verdict: NotVerified, Results: []Result{NotJudged, NotJudged, NotJudged}, Reasons: []string{":probe-line-invalid"}, Decided: ProbeLineInvalid},
	}, {
		name: "a condition on a window that never began",
		tl:   Timeline{Windows: []Window{steady(), deploy(), notBegun(Rollback, cond(ProbeFailures))}, Now: at(40 * sec)},
		want: summary{Verdict: NotVerified, Results: []Result{NotJudged, NotJudged, NotRun}, Reasons: []string{":internal-error", "rollback:probe-failures"}, Decided: InternalError},
	}, {
		name: "the deploy window began, but the steady window before it never did",
		tl: Timeline{Windows: []Window{notBegun(Steady), deploy(), notBegun(Rollback)},
			Observations: []Observation{{Written: at(11 * sec), T: at(11 * sec), OK: false, N: 1}}, Now: at(40 * sec)},
		want: summary{Verdict: NotVerified, Results: []Result{NotRun, NotJudged, NotRun}, Reasons: []string{":internal-error"}, Decided: InternalError},
	}, {
		name: "the rollback window began, but the deploy window before it never did",
		tl:   Timeline{Windows: []Window{steady(), notBegun(Deploy), rollback()}, Now: at(70 * sec)},
		want: summary{Verdict: NotVerified, Results: []Result{NotJudged, NotRun, NotJudged}, Reasons: []string{":internal-error"}, Decided: InternalError},
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
				if r.Text != "text of "+string(r.Code) && !(r.Code == InternalError && r.Window == "") {
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
