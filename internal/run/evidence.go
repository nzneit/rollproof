package run

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"rollproof/internal/verdict"
	"rollproof/internal/watch"
)

// completionLimit bounds the listing of the events after the verdict, also
// after a signal: an API server that stops answering cannot hold the run.
// A test shortens it.
var completionLimit = 10 * time.Second

// mark is one window's boundaries in windows.json, and every finish of a
// transition in the order they came.
type mark struct {
	Name     string      `json:"name"`
	Start    *time.Time  `json:"start"`
	Finished *time.Time  `json:"finished"`
	End      *time.Time  `json:"end"`
	Finishes []time.Time `json:"finishes"`
}

// marks is the window marks of one run. The run's own flow calls set, follow
// and end; the follower's goroutine, while it runs, is the only one that takes
// finishes from the channel, so that they are kept in the order they came.
type marks struct {
	replace func(name string, b []byte) error
	warn    func(string)

	mu       sync.Mutex
	windows  []verdict.Window
	finishes map[verdict.WindowName][]time.Time
	in       <-chan watch.Finish // the finishes not yet taken; nil before follow and once closed
	warned   bool

	// Touched by the run's own flow only.
	ask   chan chan struct{} // a request that the follower take what waits; nil until follow
	stop  chan struct{}      // closed by end
	done  chan struct{}      // closed when the follower has returned
	ended bool
}

// newMarks returns the window marks of one run, written whole to windows.json
// by replace at each mark. The first failure to write is one warning through
// warn; later failures are not repeated.
func newMarks(replace func(name string, b []byte) error, warn func(string)) *marks {
	return &marks{replace: replace, warn: warn, finishes: map[verdict.WindowName][]time.Time{}}
}

// set records each window's start, finish and end as ws gives them, takes
// every finish that waits, and writes the file. Called by the run's own flow
// when a window begins or ends.
func (m *marks) set(ws []verdict.Window) {
	following := m.sync()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.windows = slices.Clone(ws)
	if !following {
		m.takeWaiting()
	}
	m.write()
}

// follow writes the file again for each finish that arrives, while the run
// waits, until end. Each transition's entry keeps every finish in the order
// they came, also one that its settle time undid.
func (m *marks) follow(finishes <-chan watch.Finish) {
	m.mu.Lock()
	m.in = finishes
	m.mu.Unlock()
	m.ask, m.stop, m.done = make(chan chan struct{}), make(chan struct{}), make(chan struct{})
	go m.run(finishes, m.ask, m.stop, m.done)
}

// end stops following, takes every finish that waits and writes the file a
// last time. Safe to call more than once.
func (m *marks) end() {
	if m.ended {
		return
	}
	m.ended = true
	if m.stop != nil {
		close(m.stop)
		<-m.done
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.takeWaiting()
	m.write()
}

// run is the follower: it takes each finish as it arrives and writes the
// file, and takes what waits when set asks, until stop is closed or the channel
// is. A panic stops it and is the warning of a failed write, given once as
// write gives one; the run goes on, and set and end take the finishes
// themselves from then on.
func (m *marks) run(in <-chan watch.Finish, ask chan chan struct{}, stop, done chan struct{}) {
	defer close(done)
	defer func() {
		if p := recover(); p != nil {
			m.mu.Lock()
			defer m.mu.Unlock()
			if !m.warned {
				m.warned = true
				m.warn("the evidence windows.json was not written: " + panicText(p))
			}
		}
	}()
	for {
		select {
		case f, ok := <-in:
			if !m.took(f, ok) {
				return
			}
		case reply := <-ask:
			open := m.takeWaitingLocked()
			close(reply)
			if !open {
				return
			}
		case <-stop:
			return
		}
	}
}

// took keeps the finish f that the follower received, takes every other that
// waits, and writes the file; ok false is the channel's close. It reports
// whether the channel is still open.
func (m *marks) took(f watch.Finish, ok bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !ok {
		m.in = nil
		return false
	}
	m.finishes[f.Name] = append(m.finishes[f.Name], f.At)
	open := m.takeWaiting()
	m.write()
	return open
}

// takeWaitingLocked is takeWaiting under mu.
func (m *marks) takeWaitingLocked() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.takeWaiting()
}

// sync has the follower take every finish that waits, and reports whether it
// did: false when follow never ran or the follower has returned.
func (m *marks) sync() bool {
	if m.ask == nil {
		return false
	}
	reply := make(chan struct{})
	select {
	case m.ask <- reply:
		// A follower that panics while it answers never closes the reply; its done ends the wait.
		select {
		case <-reply:
			return true
		case <-m.done:
			return false
		}
	case <-m.done:
		return false
	}
}

// takeWaiting takes every finish that already waits on the channel, and
// reports whether the channel is still open. A closed channel is forgotten,
// so that it is never read again. The caller holds mu.
func (m *marks) takeWaiting() bool {
	for m.in != nil {
		select {
		case f, ok := <-m.in:
			if !ok {
				m.in = nil
				return false
			}
			m.finishes[f.Name] = append(m.finishes[f.Name], f.At)
		default:
			return true
		}
	}
	return false
}

// write writes windows.json whole, and warns at the first failure. The caller
// holds mu.
func (m *marks) write() {
	out := make([]mark, 0, len(m.windows))
	for _, w := range m.windows {
		out = append(out, mark{Name: string(w.Name), Start: timePtr(w.Start), Finished: timePtr(w.Finished),
			End: timePtr(w.End), Finishes: m.finishes[w.Name]})
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err == nil {
		err = m.replace("windows.json", b)
	}
	if err != nil && !m.warned {
		m.warned = true
		m.warn("the evidence windows.json was not written: " + err.Error())
	}
}

// writeTimeline writes one tab-separated row for each whole second from
// from's second up to to: the checks that began in it (an observation counts
// n), the failed ones, and the pod events of that second. A header comes
// first. What lies before from's second or at or after to is in no row.
func writeTimeline(w io.Writer, obs []verdict.Observation, pods []watch.PodEvent, from, to time.Time) error {
	start := from.Truncate(time.Second)
	type row struct {
		checks, failed int
		pods           []string
	}
	var rows []row
	if to.After(start) {
		rows = make([]row, (to.Sub(start)+time.Second-1)/time.Second)
	}
	// index is the row of the moment at, or -1 when no row holds it.
	index := func(at time.Time) int {
		if at.Before(start) || !at.Before(to) {
			return -1
		}
		return int(at.Sub(start) / time.Second)
	}
	for _, o := range obs {
		if i := index(o.T); i >= 0 {
			rows[i].checks += o.N
			if !o.OK {
				rows[i].failed += o.N
			}
		}
	}
	for _, p := range pods {
		if i := index(p.At); i >= 0 {
			rows[i].pods = append(rows[i].pods, p.Kind+" "+p.Pod+" ("+p.Workload+")")
		}
	}
	b := bufio.NewWriter(w)
	fmt.Fprint(b, "second\tchecks\tfailed\tpod events\n")
	for i, r := range rows {
		sec := start.Add(time.Duration(i) * time.Second).UTC().Format(time.RFC3339)
		fmt.Fprintf(b, "%s\t%d\t%d\t%s\n", sec, r.checks, r.failed, strings.Join(r.pods, ", "))
	}
	return b.Flush()
}

// timelineFrom is the moment from which timeline.tsv's rows begin: first, the runtime's stamp of
// the probe's first observation, or the earliest t of an observation that the install window
// counts when that is earlier. The install window, which opens no earlier than first, counts every
// observation whose t is before its start (section 6), such as the checks that the probe's start
// met and whose lines were written late, so that every check and failure the install record counts
// falls in a row (section 14, point 25). A run with no install window counts none of them, and its
// rows begin at first.
func timelineFrom(t verdict.Timeline, first time.Time) time.Time {
	if len(t.Windows) == 0 || t.Windows[0].Name != verdict.Install || t.Windows[0].Start.IsZero() {
		return first
	}
	from := first
	for _, o := range t.Observations {
		if o.T.Before(from) {
			from = o.T
		}
	}
	return from
}

// completeEvidence writes the marks a last time, timeline.tsv and the events
// of both namespaces, after the final result.json and before the cluster is
// deleted. Each failure is one warning and changes no verdict. The events'
// listing is bounded by completionLimit, also after a signal. A panic ends the
// completion there and is the warning that a failure after the verdict was
// decided gives.
func (s *runState) completeEvidence() {
	defer func() {
		if p := recover(); p != nil {
			s.warnings.add("the tool failed after the verdict was decided: " + panicText(p))
		}
	}()
	s.marks.end()
	if first := s.probeStart.First; !first.IsZero() {
		t, _ := s.timeline()
		to := s.u.Clock.Now()
		for _, w := range slices.Backward(t.Windows) {
			if !w.Start.IsZero() {
				if !w.End.IsZero() {
					to = w.End
				}
				break
			}
		}
		var pods []watch.PodEvent
		if s.watcher != nil {
			pods = s.watcher.PodEvents()
		}
		from := timelineFrom(t, first)
		if err := s.writeEvidence("timeline.tsv", func(w io.Writer) error {
			return writeTimeline(w, t.Observations, pods, from, to)
		}); err != nil {
			s.warnings.add("the evidence timeline.tsv was not written: " + err.Error())
		}
	}
	if s.watcher != nil {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(s.parent), completionLimit)
		defer cancel()
		if err := s.writeEvidence("events.txt", func(w io.Writer) error {
			return s.watcher.WriteEvents(ctx, w)
		}); err != nil {
			s.warnings.add("the events were not written: " + err.Error())
		}
	}
}

// writeEvidence opens the evidence file name, has fill write it, and closes
// it, also when fill panics, and returns the first error of the three, so that
// each file's failure is told once.
func (s *runState) writeEvidence(name string, fill func(io.Writer) error) (err error) {
	f, err := s.w.Evidence(name)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	return fill(f)
}
