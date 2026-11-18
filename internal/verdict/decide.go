package verdict

import (
	"slices"
	"time"
)

// Window is one window of a run. Start is zero for a window that never began, and End is zero
// while it is open. Finished is when its transition last came to be finished: zero for the install
// and steady windows and for a stall. Conditions are what the caller placed in this window.
type Window struct {
	Name       WindowName
	Start      time.Time
	Finished   time.Time
	End        time.Time
	Conditions []Condition
}

// Timeline is everything known of a run so far. Before is what happened before the first window.
// Windows are all the run's windows in order, those not yet begun included. Timed are conditions
// that Decide places by their At. Silence is limits.silence. Now is the moment of judging.
type Timeline struct {
	Before       []Condition
	Windows      []Window
	Observations []Observation
	Timed        []Condition
	Silence      time.Duration
	Now          time.Time
}

// Reason is one reason of the verdict, as result.json lists it. Window is empty for a reason with
// no window. Decided marks the one reason that decided.
type Reason struct {
	Code    Code
	Verdict Verdict
	Window  WindowName
	Text    string
	Decided bool
}

// WindowResult is how one window ended, with its counts. AfterFinish counts failures that began at
// or after the window's Finished. Crossed counts failures that began before the window's Start and
// were written in or after it; only a transition's window counts them, because the transition may
// be what cut them (plan ruling 10). FirstFailure is zero when nothing failed. LastFailure is the
// latest time at which a check that failed in this window began, zero when nothing failed.
type WindowResult struct {
	Name         WindowName
	Start, End   time.Time
	Result       Result
	Checks       int
	Failed       int
	AfterFinish  int
	Crossed      int
	ByClass      map[string]int
	FirstFailure time.Time
	LastFailure  time.Time
	SlowestMs    float64
	LagMs        float64
}

// Decision is the verdict so far. GoOn says whether the run starts its next transition. Uncounted
// is the number of observations that no window counts: those after the last window ended, and
// those before the first window when the run has no install window. Ignored are Timed conditions
// at or after the last window's End; the report shows them as warnings.
type Decision struct {
	Verdict   Verdict
	GoOn      bool
	Reasons   []Reason
	Windows   []WindowResult
	Uncounted int
	Ignored   []Condition
}

// Decide judges everything known so far by the rules of section 10. Call it at the end of each
// window and once more after the probe has stopped; the last call's Decision is the run's. GoOn is
// true when the verdict so far is Pass. A Timeline in which no window has begun, with nothing in
// Before, gives NotVerified with InternalError. The install window is judged as the first of
// t.Windows when the caller puts it there: a failed check in it is counted and gives no condition,
// and any condition makes it not verified.
//
// Decide also reads the observations: it counts them into windows by T, turns a failure into
// ProbeFailures in a transition's window and into SteadyNotClean in the steady window, and turns a
// stretch with no observation longer than Silence into ProbeSilent for every window the stretch
// overlaps. Crossed is counted for a transition's window only.
func Decide(t Timeline) Decision {
	p := place(t)
	observe(t, &p)
	return judge(t, p)
}

// placement is a Timeline with every condition in its place (rule 3).
type placement struct {
	before    []Condition    // t.Before, then Timed conditions earlier than the first window
	windows   [][]Condition  // the conditions of each window of t.Windows, in order
	ignored   []Condition    // Timed conditions at or after the last begun window's end
	ends      []time.Time    // where each begun window ends for placing; zero if it never began
	results   []WindowResult // Name, Start and End of each window; observe adds the counts
	uncounted int            // set by observe
}

// place applies rule 3 and fills results with each window's Name, Start and End.
func place(t Timeline) placement {
	n := len(t.Windows)
	p := placement{
		before:  slices.Clone(t.Before),
		windows: make([][]Condition, n),
		ends:    make([]time.Time, n),
		results: make([]WindowResult, n),
	}
	var begun []int
	for i, w := range t.Windows {
		p.windows[i] = slices.Clone(w.Conditions)
		p.results[i] = WindowResult{Name: w.Name, Start: w.Start, End: w.End}
		if !w.Start.IsZero() {
			begun = append(begun, i)
		}
	}
	for k, i := range begun {
		switch {
		case k+1 < len(begun):
			p.ends[i] = t.Windows[begun[k+1]].Start
		case !t.Windows[i].End.IsZero():
			p.ends[i] = t.Windows[i].End
		default:
			p.ends[i] = t.Now
		}
	}
	for _, c := range t.Timed {
		if len(begun) == 0 || c.At.Before(t.Windows[begun[0]].Start) {
			p.before = append(p.before, c)
			continue
		}
		placed := false
		for _, i := range begun {
			if !c.At.Before(t.Windows[i].Start) && c.At.Before(p.ends[i]) {
				p.windows[i] = append(p.windows[i], c)
				placed = true
				break
			}
		}
		if !placed {
			p.ignored = append(p.ignored, c)
		}
	}
	return p
}

// judge applies rules 1 and 4 to 9 to what place gave. Rule 2, the windows'
// lengths, and rule 8's running of a transition to its end are run's.
func judge(t Timeline, p placement) Decision {
	d := Decision{
		Windows:   slices.Clone(p.results),
		Uncounted: p.uncounted,
		Ignored:   slices.Clone(p.ignored),
	}
	begun := false
	for _, w := range t.Windows {
		begun = begun || !w.Start.IsZero()
	}

	// A condition placed on a window that never began is the caller's mistake; it is never
	// dropped. The run is not verified, and the conditions are listed after the reason.
	var stray []Reason
	var strayWindow WindowName
	for i, w := range t.Windows {
		if w.Start.IsZero() && len(p.windows[i]) > 0 {
			if stray == nil {
				strayWindow = w.Name
			}
			stray = append(stray, reasonsOf(w.Name, p.windows[i], "")...)
		}
	}
	if stray != nil {
		d.Verdict = NotVerified
		d.Reasons = append([]Reason{{
			Code: InternalError, Verdict: NotVerified, Decided: true,
			Text: "a condition was placed in the " + string(strayWindow) + " window, which never began",
		}}, stray...)
		d.notJudgeBegun(t)
		return d
	}
	// Judged at a moment before the open window began, the window has no span to hold the probe's
	// lines, so nothing could be counted. That is the caller's mistake, and the run is not verified.
	for i := len(t.Windows) - 1; i >= 0; i-- {
		w := t.Windows[i]
		if w.Start.IsZero() {
			continue
		}
		if w.End.IsZero() && t.Now.Before(w.Start) {
			d.Verdict = NotVerified
			d.Reasons = []Reason{{
				Code: InternalError, Verdict: NotVerified, Decided: true,
				Text: "the run was judged at a moment before the " + string(w.Name) + " window began",
			}}
			d.notJudgeBegun(t)
			return d
		}
		break
	}
	// A window cannot begin after one before it never did: the observations that fall between
	// them have no window to be counted in, so the run would pass with them dropped. That is the
	// caller's mistake, and the run is not verified.
	var unbegun *Window
	for i, w := range t.Windows {
		if w.Start.IsZero() {
			if unbegun == nil {
				unbegun = &t.Windows[i]
			}
			continue
		}
		if unbegun != nil {
			d.Verdict = NotVerified
			d.Reasons = []Reason{{
				Code: InternalError, Verdict: NotVerified, Decided: true,
				Text: "the " + string(w.Name) + " window began, but the " + string(unbegun.Name) + " window before it never did",
			}}
			d.notJudgeBegun(t)
			return d
		}
	}
	if len(p.before) > 0 {
		d.Verdict = NotVerified
		d.Reasons = reasonsOf("", p.before, NotVerified)
		d.notJudgeBegun(t)
		return d
	}
	if !begun {
		for i := range d.Windows {
			d.Windows[i].Result = NotRun
		}
		d.Verdict = NotVerified
		d.Reasons = []Reason{{Code: InternalError, Verdict: NotVerified, Text: "the run did not finish", Decided: true}}
		return d
	}

	d.Verdict = Pass
	deciding := -1
	for i, w := range t.Windows {
		switch {
		case deciding >= 0 && !w.Start.IsZero():
			d.Windows[i].Result = NotJudged
		case w.Start.IsZero():
			d.Windows[i].Result = NotRun
		default:
			r := resultOf(w.Name, p.windows[i])
			d.Windows[i].Result = r
			if r != ResultPass {
				deciding = i
				d.Verdict = Verdict(r)
			}
		}
	}
	for i := 0; deciding >= 0 && i <= deciding; i++ {
		if t.Windows[i].Start.IsZero() {
			continue
		}
		if i == deciding {
			d.Reasons = append(d.Reasons, reasonsOf(t.Windows[i].Name, p.windows[i], d.Verdict)...)
		} else {
			d.Reasons = append(d.Reasons, reasonsOf(t.Windows[i].Name, p.windows[i], "")...)
		}
	}
	d.GoOn = d.Verdict == Pass
	return d
}

// resultOf judges one begun window by its conditions: the install window and the steady window are
// not verified by any condition, a transition's window fails on a Fail code and is otherwise not
// verified by any condition.
func resultOf(name WindowName, cs []Condition) Result {
	if len(cs) == 0 {
		return ResultPass
	}
	if name == Install || name == Steady {
		return ResultNotVerified
	}
	for _, c := range cs {
		if c.Code.Verdict() == Fail {
			return ResultFail
		}
	}
	return ResultNotVerified
}

// reasonsOf lists cs as reasons of window w, in the order of Codes. When decides is not empty, the
// first reason whose code's verdict equals it is marked Decided; when none does, the first reason is.
func reasonsOf(w WindowName, cs []Condition, decides Verdict) []Reason {
	rank := map[Code]int{}
	for i, c := range Codes() {
		rank[c] = i
	}
	pos := func(c Code) int {
		if i, ok := rank[c]; ok {
			return i
		}
		return len(rank)
	}
	sorted := slices.Clone(cs)
	slices.SortStableFunc(sorted, func(a, b Condition) int { return pos(a.Code) - pos(b.Code) })
	var rs []Reason
	for _, c := range sorted {
		rs = append(rs, Reason{Code: c.Code, Verdict: c.Code.Verdict(), Window: w, Text: c.Text})
	}
	if len(rs) == 0 || decides == "" {
		return rs
	}
	pick := 0
	for i, r := range rs {
		if r.Verdict == decides {
			pick = i
			break
		}
	}
	rs[pick].Decided = true
	return rs
}

// notJudgeBegun sets the result of every window of t: not-judged for one that began, not-run for
// one that never did.
func (d *Decision) notJudgeBegun(t Timeline) {
	for i, w := range t.Windows {
		d.Windows[i].Result = NotRun
		if !w.Start.IsZero() {
			d.Windows[i].Result = NotJudged
		}
	}
}
