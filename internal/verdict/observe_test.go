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
		name: "silence is measured on Written, not on T",
		tl: timeline(70*sec, allThree(), passes(0, 21*sec),
			func() []Observation {
				var obs []Observation
				for d := 21 * sec; d < 31*sec; d += sec {
					obs = append(obs, Observation{Written: at(31 * sec), T: at(d), OK: true, N: 200, Ms: 30, Lag: 5})
				}
				return obs
			}(), passes(31*sec, 70*sec)),
		want:  summary{Verdict: NotVerified, Results: []Result{ResultPass, ResultNotVerified, NotJudged}, Reasons: []string{"deploy:probe-silent"}, Decided: ProbeSilent},
		texts: map[string]string{"deploy:probe-silent": "no observation for 11.0 s, from 10:00:20.00"},
	}, {
		name: "after-finish is judged on T, not on Written",
		tl:   timeline(70*sec, allThree(), passes(0, 70*sec), []Observation{failure(24*sec, 26*sec, "refused")}),
		want: summary{Verdict: Fail, Results: []Result{ResultPass, ResultFail, NotJudged}, Reasons: []string{"deploy:probe-failures"}, Decided: ProbeFailures},
		tallies: map[WindowName]tally{Deploy: {Checks: 6001, Failed: 1, AfterFinish: 0, ByClass: map[string]int{"refused": 1},
			FirstFailure: at(24 * sec), SlowestMs: 30, LagMs: 5}},
	}, {
		name: "a failure written in the deploy crosses no later window",
		tl:   timeline(70*sec, allThree(), passes(0, 70*sec), []Observation{failure(30*sec, 31*sec, "refused")}),
		want: summary{Verdict: Fail, Results: []Result{ResultPass, ResultFail, NotJudged}, Reasons: []string{"deploy:probe-failures"}, Decided: ProbeFailures},
		tallies: map[WindowName]tally{
			Deploy:   {Checks: 6001, Failed: 1, AfterFinish: 1, ByClass: map[string]int{"refused": 1}, FirstFailure: at(30 * sec), SlowestMs: 30, LagMs: 5},
			Rollback: {Checks: 6000, Crossed: 0, SlowestMs: 30, LagMs: 5},
		},
	}, {
		name: "first failure is the earliest, whatever the order of the lines",
		tl: timeline(70*sec, allThree(), passes(0, 70*sec),
			[]Observation{failure(30*sec, 30*sec, "refused"), failure(20*sec, 20*sec, "refused")}),
		want: summary{Verdict: Fail, Results: []Result{ResultPass, ResultFail, NotJudged}, Reasons: []string{"deploy:probe-failures"}, Decided: ProbeFailures},
		tallies: map[WindowName]tally{Deploy: {Checks: 6002, Failed: 2, AfterFinish: 1, ByClass: map[string]int{"refused": 2},
			FirstFailure: at(20 * sec), SlowestMs: 30, LagMs: 5}},
	}, {
		name: "an open deploy judged at a zero Now",
		tl: Timeline{Windows: []Window{steady(), inProgress(deploy()), notBegun(Rollback)}, Silence: 5 * sec,
			Observations: []Observation{failure(12*sec, 12*sec, "refused")}},
		want:      summary{Verdict: NotVerified, Results: []Result{NotJudged, NotJudged, NotRun}, Reasons: []string{":internal-error"}, Decided: InternalError},
		texts:     map[string]string{":internal-error": "the run was judged at a moment before the deploy window began"},
		uncounted: 1,
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
