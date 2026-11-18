package watch

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"rollproof/internal/verdict"
)

// Settling is the install from ready until it settled under the probe. First is the probe's first
// observation, as given. Settled is zero when the install did not settle. Ended is when the wait
// ended, never before First. Restarts say what started the settle time again. Before holds what
// happened before First, InWindow what happened from it on, each with its moment in At.
type Settling struct {
	First, Settled, Ended time.Time
	Restarts              []string
	Before, InWindow      []verdict.Condition
}

// installTrack is what the Tracker keeps of the install from the moment it was ready.
type installTrack struct {
	began, ready time.Time
	breaks       []installBreak         // in the order they began; only the last can be open
	unsupported  map[Workload]time.Time // each watched workload that does not roll, with when it was first seen so
	over         *Settling              // the answer, once the settling has ended
}

// installBreak is a stretch in which the install's conditions did not hold. until is zero while
// it lasts. sentence says what did not hold when it began.
type installBreak struct {
	from, until time.Time
	sentence    string
}

// byKindAndName orders workloads by kind and then name.
func byKindAndName(a, b Workload) int {
	return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Name, b.Name))
}

// installWatched returns the workloads the install watches, those among them that update by
// rolling, and the rest, each sorted by kind and then name. It watches the workloads that exist
// and whose labels matched at their last Set.
func (t *Tracker) installWatched() (watched, part, notRolling []Workload) {
	set := map[Workload]bool{}
	for w, rec := range t.workloads {
		if rec.gone.IsZero() && rec.matched {
			set[w] = true
		}
	}
	watched = sortedWorkloads(set)
	for _, w := range watched {
		if Rolling(t.workloads[w].obj) {
			part = append(part, w)
		} else {
			notRolling = append(notRolling, w)
		}
	}
	return watched, part, notRolling
}

// noteInstall notes, at the moment at, whether the install's conditions hold, after a change. It
// does nothing before the install was ready and after the settling has ended. It notes each
// watched workload that does not update by rolling, with the first moment it was seen so, and
// opens a break when the conditions stopped holding, or closes the open one when they hold again.
func (t *Tracker) noteInstall(at time.Time) {
	in := t.install
	if in == nil || in.over != nil {
		return
	}
	watched, part, notRolling := t.installWatched()
	for _, w := range notRolling {
		if _, seen := in.unsupported[w]; !seen {
			in.unsupported[w] = at
		}
	}
	held := len(watched) > 0 && t.installReady(part)
	open := len(in.breaks) > 0 && in.breaks[len(in.breaks)-1].until.IsZero()
	switch {
	case held && open:
		in.breaks[len(in.breaks)-1].until = at
	case !held && !open:
		in.breaks = append(in.breaks, installBreak{from: at, sentence: t.installSentence(at, watched, part)})
	}
}

// installSentence says what did not hold at the moment at, when a break began: the first of no
// workload watched, a workload that is not complete, and a pod that was deleted and is not gone.
// The seconds are counted from the moment the install was ready.
func (t *Tracker) installSentence(at time.Time, watched, part []Workload) string {
	s := at.Sub(t.install.ready).Seconds()
	if len(watched) == 0 {
		return fmt.Sprintf("no workload was watched %.1f s after the install was ready", s)
	}
	for _, w := range part {
		if done, msg := Complete(t.workloads[w].obj); !done {
			return fmt.Sprintf("%s was not complete %.1f s after the install was ready: %s", w, s, msg)
		}
	}
	for _, p := range t.sortedPods() {
		if w, ok := t.podWorkload(p); ok && slices.Contains(part, w) && !p.deleting.IsZero() && p.gone.IsZero() {
			return fmt.Sprintf("pod %s of %s was deleted and not gone %.1f s after the install was ready", p.name, w, s)
		}
	}
	return "" // the conditions did not hold, so one of the three above says so
}

// InstallSettling returns the settling so far, and whether it has ended: the install settled or
// failed, or a workload the tool cannot judge was seen before first. first is when the probe's
// first observation was written, the same at every call. Once it has ended, every later call
// gives the same answer. It panics when InstallState has not reported ready.
func (t *Tracker) InstallSettling(first, now time.Time) (Settling, bool) {
	in := t.install
	if in == nil {
		panic("watch: InstallSettling before InstallState reported the install ready")
	}
	if in.over != nil {
		return *in.over, true
	}
	s := Settling{First: first}
	settled, failed, cause, counted := in.outcome(first, now, t.o.Settle, t.o.Timeout)
	early, earlyAt := in.unsupportedIn(func(seen time.Time) bool { return seen.Before(first) })
	if len(early) > 0 {
		// Found before the probe's first observation, it ends the run there (section 7, ruling 5),
		// so the settling ended at first and the install window has no length. A failure before
		// first came before the run ended, and is kept; one at or after first did not happen in the
		// settling. Restarts say only what started the settle time before the settling ended.
		settled = time.Time{}
		if !failed.IsZero() && !failed.Before(first) {
			failed, cause = time.Time{}, nil
		}
		counted = slices.DeleteFunc(counted, func(b installBreak) bool { return b.from.After(first) })
	}
	for _, b := range counted {
		s.Restarts = append(s.Restarts, b.sentence)
	}
	switch {
	case len(early) > 0:
		s.Ended = first
	case !settled.IsZero():
		s.Settled, s.Ended = settled, settled
	case !failed.IsZero():
		s.Ended = later(failed, first)
	default:
		return s, false
	}

	var cs []verdict.Condition
	limit := in.began.Add(t.o.Timeout)
	if !failed.IsZero() && cause != nil {
		text := "the install was not ready when limits.timeout passed"
		if cause.from.After(limit) {
			text = "the install stopped being ready after limits.timeout, while it still settled"
		}
		cs = append(cs, verdict.Condition{Code: verdict.InstallFailed, At: failed,
			Text: text + "\n" + cause.sentence})
	}
	late, lateAt := in.unsupportedIn(func(seen time.Time) bool {
		return !seen.Before(first) && !seen.After(s.Ended)
	})
	for _, group := range []struct {
		ws []Workload
		at time.Time
	}{{early, earlyAt}, {late, lateAt}} {
		if len(group.ws) == 0 {
			continue
		}
		c := unsupported(group.ws, t.updateType)
		c.At = group.at
		cs = append(cs, c)
	}
	if m, missed := t.missedBetween(first, s.Ended); missed {
		cs = append(cs, verdict.Condition{Code: verdict.InternalError, At: m,
			Text: fmt.Sprintf("the watch of the cluster missed changes %.1f s after the probe's first observation",
				m.Sub(first).Seconds())})
	}
	for _, c := range cs {
		if c.At.Before(first) {
			s.Before = append(s.Before, c)
		} else {
			s.InWindow = append(s.InWindow, c)
		}
	}
	in.over = &s
	return s, true
}

// unsupportedIn returns the workloads the tool cannot judge whose first sight satisfies keep,
// sorted by kind and then name, and the earliest of those moments.
func (in *installTrack) unsupportedIn(keep func(time.Time) bool) ([]Workload, time.Time) {
	var ws []Workload
	var at time.Time
	for w, seen := range in.unsupported {
		if !keep(seen) {
			continue
		}
		ws = append(ws, w)
		if at.IsZero() || seen.Before(at) {
			at = seen
		}
	}
	slices.SortFunc(ws, byKindAndName)
	return ws, at
}

// missedBetween returns the first moment m, with first at or before m and m at or before until, at
// which the watch of the cluster may have missed changes, and whether there is one. It reads the
// two records of what the watch missed and changes neither.
func (t *Tracker) missedBetween(first, until time.Time) (time.Time, bool) {
	var m time.Time
	note := func(x time.Time) {
		if m.IsZero() || x.Before(m) {
			m = x
		}
	}
	for _, b := range t.missedOutside {
		if !b.until.Before(first) && !b.from.After(until) {
			note(later(b.from, first))
		}
	}
	for _, d := range t.down {
		if !d.After(until) {
			note(later(d, first))
		}
	}
	return m, !m.IsZero()
}

// outcome gives the moment the install settled, or the moment it failed and the break that
// failed it. Both moments are zero while neither has come by now. counted are the breaks that
// started the settle time again.
func (in *installTrack) outcome(first, now time.Time, settle, timeout time.Duration) (settled, failed time.Time, cause *installBreak, counted []installBreak) {
	limit := in.began.Add(timeout)
	from := first // the settle time is counted from here
	for i := range in.breaks {
		b := &in.breaks[i]
		if !b.from.Before(from.Add(settle)) {
			break // the install had settled before this break began
		}
		open := b.until.IsZero()
		if open || b.until.After(first) {
			counted = append(counted, *b)
		}
		switch {
		case b.from.After(limit):
			return time.Time{}, b.from, b, counted // it broke after the limit, before it had settled
		case open && !now.Before(limit), !open && b.until.After(limit):
			return time.Time{}, limit, b, counted // it did not hold when the limit passed
		case open:
			return time.Time{}, time.Time{}, nil, counted // broken now, inside the limit
		}
		from = later(from, b.until)
	}
	if end := from.Add(settle); !now.Before(end) {
		return end, time.Time{}, nil, counted
	}
	return time.Time{}, time.Time{}, nil, counted
}
