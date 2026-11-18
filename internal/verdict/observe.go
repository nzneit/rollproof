package verdict

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// clockLayout is how a condition's text writes a moment: UTC, to the hundredth of a second.
const clockLayout = "15:04:05.00"

// observe adds what the probe saw to p: counts in p.results, ProbeFailures, SteadyNotClean and
// ProbeSilent conditions in p.windows, and the lines that no window holds in p.uncounted. It adds
// to windows that began only. It does nothing when the first window has not begun.
func observe(t Timeline, p *placement) {
	if len(t.Windows) == 0 || t.Windows[0].Start.IsZero() {
		return
	}
	stretchEnd := silenceEnd(t, p)
	countObservations(t, p)
	addFailures(t, p)
	addSilence(t, p, stretchEnd)
}

// holder is the begun window that holds the moment m, or -1 when none does.
func holder(t Timeline, p *placement, m time.Time) int {
	for i, w := range t.Windows {
		if !w.Start.IsZero() && !m.Before(w.Start) && m.Before(p.ends[i]) {
			return i
		}
	}
	return -1
}

// countObservations fills the counts of p.results (steps 1 to 5 of the observation rules).
func countObservations(t Timeline, p *placement) {
	for _, o := range t.Observations {
		i := holder(t, p, o.T)
		if i < 0 {
			p.uncounted++
			continue
		}
		r := &p.results[i]
		r.LagMs = max(r.LagMs, o.Lag)
		if o.OK {
			r.Checks += o.N
			r.SlowestMs = max(r.SlowestMs, o.Ms)
			continue
		}
		r.Checks++
		r.Failed++
		if r.ByClass == nil {
			r.ByClass = map[string]int{}
		}
		class := o.Class
		if class == "" {
			class = "unclassified"
		}
		r.ByClass[class]++
		if r.FirstFailure.IsZero() || o.T.Before(r.FirstFailure) {
			r.FirstFailure = o.T
		}
		if r.LastFailure.IsZero() || o.T.After(r.LastFailure) {
			r.LastFailure = o.T
		}
		if f := t.Windows[i].Finished; !f.IsZero() && !o.T.Before(f) {
			r.AfterFinish++
		}
		for j := i + 1; j < len(t.Windows); j++ {
			if t.Windows[j].Name == Install || t.Windows[j].Name == Steady {
				continue
			}
			if s := t.Windows[j].Start; !s.IsZero() && s.After(o.T) && !s.After(o.Written) {
				p.results[j].Crossed++
			}
		}
	}
}

// addFailures places a condition in each begun window that counted a failure (step 6 of the
// observation rules), except the install window: a failure there is counted and decides nothing.
// For the steady window it adds a line when no check has passed since the probe started.
func addFailures(t Timeline, p *placement) {
	for i, w := range t.Windows {
		r := p.results[i]
		if w.Start.IsZero() || w.Name == Install || r.Failed == 0 {
			continue
		}
		type classCount struct {
			class string
			n     int
		}
		var cs []classCount
		for class, n := range r.ByClass {
			cs = append(cs, classCount{class, n})
		}
		slices.SortFunc(cs, func(a, b classCount) int {
			if a.n != b.n {
				return b.n - a.n
			}
			return strings.Compare(a.class, b.class)
		})
		parts := make([]string, len(cs))
		for k, c := range cs {
			parts[k] = c.class + " " + strconv.Itoa(c.n)
		}
		noun := "checks"
		if r.Failed == 1 {
			noun = "check"
		}
		code := ProbeFailures
		if w.Name == Steady {
			code = SteadyNotClean
		}
		text := fmt.Sprintf("%d %s failed (%s), first at %s", r.Failed, noun, strings.Join(parts, ", "), r.FirstFailure.UTC().Format(clockLayout))
		if w.Name == Steady && !passedBefore(t, p.ends[i]) {
			text += "\nno check has passed since the probe started: every check failed for " +
				strconv.FormatFloat(p.ends[i].Sub(t.Windows[0].Start).Seconds(), 'f', 1, 64) + " s"
		}
		p.windows[i] = append(p.windows[i], Condition{
			Code: code,
			Text: text,
		})
	}
}

// passedBefore says whether any observation of the timeline passed at a time before end.
func passedBefore(t Timeline, end time.Time) bool {
	for _, o := range t.Observations {
		if o.OK && o.T.Before(end) {
			return true
		}
	}
	return false
}

// silenceEnd is where the measured stretch ends: the end of the last begun window, or the moment
// of the earliest ProbeExited that p holds, when that is earlier.
func silenceEnd(t Timeline, p *placement) time.Time {
	var end time.Time
	for i, w := range t.Windows {
		if !w.Start.IsZero() {
			end = p.ends[i]
		}
	}
	for i, w := range t.Windows {
		if w.Start.IsZero() {
			continue
		}
		for _, c := range p.windows[i] {
			if c.Code != ProbeExited {
				continue
			}
			at := c.At
			if at.IsZero() {
				at = w.Start
			}
			if at.Before(end) {
				end = at
			}
		}
	}
	return end
}

// addSilence places a ProbeSilent condition in every begun window that a stretch without an
// observation, longer than Silence, overlaps (step 7 of the observation rules).
func addSilence(t Timeline, p *placement, end time.Time) {
	start := t.Windows[0].Start
	var written []time.Time
	for _, o := range t.Observations {
		if !o.Written.Before(start) && !o.Written.After(end) {
			written = append(written, o.Written)
		}
	}
	slices.SortFunc(written, time.Time.Compare)
	edges := slices.Concat([]time.Time{start}, written, []time.Time{end})
	for k := 0; k+1 < len(edges); k++ {
		from, to := edges[k], edges[k+1]
		if to.Sub(from) <= t.Silence {
			continue
		}
		text := "no observation for " + strconv.FormatFloat(to.Sub(from).Seconds(), 'f', 1, 64) +
			" s, from " + from.UTC().Format(clockLayout)
		for i, w := range t.Windows {
			if !w.Start.IsZero() && from.Before(p.ends[i]) && to.After(w.Start) {
				p.windows[i] = append(p.windows[i], Condition{Code: ProbeSilent, Text: text})
			}
		}
	}
}
