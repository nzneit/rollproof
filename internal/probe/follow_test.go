package probe

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"rollproof/internal/verdict"
)

var logStart = time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)

// logAt is logStart plus ms milliseconds.
func logAt(ms int) time.Time { return logStart.Add(time.Duration(ms) * time.Millisecond) }

// logLine is one line of a log stream as the API gives it with timestamps on.
func logLine(ts time.Time, text string) string {
	return ts.Format(time.RFC3339Nano) + " " + text + "\n"
}

// fakeLog serves its streams in order, one for each opening, and records the
// since of each opening. A nil stream makes its opening fail. An opening
// after the last stream gives a stream that blocks until its ctx ends.
type fakeLog struct {
	mu      sync.Mutex
	streams []io.Reader
	opened  []*time.Time
}

func newFakeLog(texts ...string) *fakeLog {
	l := &fakeLog{}
	for _, s := range texts {
		l.streams = append(l.streams, strings.NewReader(s))
	}
	return l
}

func (l *fakeLog) open(ctx context.Context, since *time.Time) (io.ReadCloser, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.opened = append(l.opened, since)
	i := len(l.opened) - 1
	switch {
	case i >= len(l.streams):
		return io.NopCloser(blocked{ctx}), nil
	case l.streams[i] == nil:
		return nil, errors.New("container is waiting to start")
	}
	// A stream that can be closed is served as it is, so that the follower can end a read
	// that waits on it, as a stream of the API ends (ruling C3).
	if rc, ok := l.streams[i].(io.ReadCloser); ok {
		return rc, nil
	}
	return io.NopCloser(l.streams[i]), nil
}

// running is true while a stream is left to serve.
func (l *fakeLog) running(context.Context) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.opened) < len(l.streams)
}

// blocked gives nothing until its ctx ends.
type blocked struct{ ctx context.Context }

func (b blocked) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

// quickPolling makes the package's waits short for one test.
func quickPolling(t *testing.T) {
	t.Helper()
	old := pollEvery
	pollEvery = 10 * time.Millisecond
	t.Cleanup(func() { pollEvery = old })
}

// followAll runs a follower over l until the log has ended for good.
func followAll(t *testing.T, l *fakeLog, stopAsked bool) (f *follower, events []Event, raw string) {
	t.Helper()
	quickPolling(t)
	var buf bytes.Buffer
	now := func() time.Time { return logAt(60_000) }
	f = newFollower(l.open, l.running, &buf, now, func(e Event) { events = append(events, e) })
	if stopAsked {
		f.stopAsked()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f.run(ctx)
	if ctx.Err() != nil {
		t.Fatal("the follower did not end by itself")
	}
	return f, events, buf.String()
}

func TestFollowLineKinds(t *testing.T) {
	stream := logLine(logAt(100), "starting") +
		logLine(logAt(200), `{"level":"info","msg":"listening"}`) +
		logLine(logAt(300), `{"ok":true}`) +
		logLine(logAt(400), `{"ok":"yes"}`) +
		logLine(logAt(500), `{"ok":false,"class":"refused"}`)
	f, events, raw := followAll(t, newFakeLog(stream), false)
	if raw != stream {
		t.Errorf("raw = %q, want the stream as it came", raw)
	}
	if len(events) != 3 {
		t.Fatalf("%d events, want 3: %+v", len(events), events)
	}
	if o := events[0].Obs; o == nil || !o.OK || !o.Written.Equal(logAt(300)) {
		t.Errorf("event 0 = %+v, want the pass written at +300 ms", events[0])
	}
	if c := events[1].Fault; c == nil || c.Code != verdict.ProbeLineInvalid || !c.At.Equal(logAt(400)) || c.Text == "" {
		t.Errorf("event 1 = %+v, want probe-line-invalid at +400 ms with the rule it broke", events[1])
	}
	if o := events[2].Obs; o == nil || o.OK || o.Class != "refused" {
		t.Errorf("event 2 = %+v, want the failure", events[2])
	}
	if lines, jsonText := f.counts(); lines != 5 || jsonText != 1 {
		t.Errorf("counts = %d, %d; want 5 lines, 1 JSON text", lines, jsonText)
	}
}

func TestFollowLongLine(t *testing.T) {
	long := `{"ok":true,"detail":"` + strings.Repeat("x", 100_000) + `"}`
	_, events, _ := followAll(t, newFakeLog(logLine(logAt(100), long)), false)
	if len(events) != 1 || events[0].Obs == nil {
		t.Errorf("events = %d, want one observation", len(events))
	}
}

// Review Focus 5: two identical failures with one timestamp, a break, and a
// stream that repeats both and adds a third and a fourth.
func TestFollowReopensWithoutLossOrRepeat(t *testing.T) {
	fail := `{"ok":false,"class":"refused"}`
	same := logAt(1500)
	l := newFakeLog(
		logLine(same, fail)+logLine(same, fail),
		logLine(same, fail)+logLine(same, fail)+logLine(same, fail)+logLine(logAt(2700), fail),
	)
	f, events, raw := followAll(t, l, false)
	failures := 0
	for _, e := range events {
		if e.Fault != nil {
			t.Errorf("fault %+v", *e.Fault)
		}
		if e.Obs != nil && !e.Obs.OK {
			failures++
		}
	}
	if failures != 4 {
		t.Errorf("%d failures, want 4", failures)
	}
	if len(l.opened) != 2 || l.opened[0] != nil || l.opened[1] == nil || !l.opened[1].Equal(logAt(1000)) {
		t.Errorf("openings since %v, want nil and then %v", l.opened, logAt(1000))
	}
	if n := strings.Count(raw, "\n"); n != 4 {
		t.Errorf("raw has %d lines, want 4: a repeated line is not written again", n)
	}
	if lines, _ := f.counts(); lines != 4 {
		t.Errorf("lines = %d, want 4", lines)
	}
}

func TestFollowRetriesAnOpeningThatFails(t *testing.T) {
	l := &fakeLog{streams: []io.Reader{nil, nil, strings.NewReader(logLine(logAt(100), `{"ok":true}`))}}
	_, events, _ := followAll(t, l, false)
	if len(events) != 1 || len(l.opened) != 3 {
		t.Errorf("%d events after %d openings, want 1 after 3", len(events), len(l.opened))
	}
	for i, since := range l.opened {
		if since != nil {
			t.Errorf("opening %d since %v, want from the start", i, *since)
		}
	}
}

func TestFollowGap(t *testing.T) {
	ok := `{"ok":true}`
	for _, c := range []struct {
		name          string
		first, second string
	}{
		{"the first line after the break is later than the last line read",
			logLine(logAt(1200), ok) + logLine(logAt(1700), ok),
			logLine(logAt(3700), ok)},
		{"fewer lines of the last timestamp came back than were read",
			logLine(logAt(1700), ok) + logLine(logAt(1700), ok),
			logLine(logAt(1700), ok) + logLine(logAt(2900), ok)},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, events, _ := followAll(t, newFakeLog(c.first, c.second), false)
			if len(events) != 4 {
				t.Fatalf("%d events, want 4: %+v", len(events), events)
			}
			if f := events[2].Fault; f == nil || f.Code != verdict.InternalError || !f.At.Equal(logAt(60_000)) ||
				f.Text != "the probe's log could not be followed without a gap" {
				t.Errorf("event 2 = %+v, want internal-error for the gap, at now", events[2])
			}
			if events[3].Obs == nil {
				t.Errorf("event 3 = %+v, want the line after the gap", events[3])
			}
		})
	}
}

func TestFollowLastPiece(t *testing.T) {
	piece := logAt(200).Format(time.RFC3339Nano) + ` {"ok":false,"class":"eof"}`
	stream := logLine(logAt(100), `{"ok":true}`) + piece
	for _, c := range []struct {
		name      string
		stopAsked bool
		events    int
	}{
		{"before Stop it is a line", false, 2},
		{"after Stop it is kept and not judged", true, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, events, raw := followAll(t, newFakeLog(stream), c.stopAsked)
			if len(events) != c.events {
				t.Errorf("%d events, want %d", len(events), c.events)
			}
			if lines, _ := f.counts(); lines != c.events {
				t.Errorf("lines = %d, want %d", lines, c.events)
			}
			if !strings.HasSuffix(raw, piece+"\n") {
				t.Errorf("raw = %q, want it to end with the piece", raw)
			}
		})
	}
}

func TestFollowLineWithoutTimestamp(t *testing.T) {
	_, events, raw := followAll(t, newFakeLog("no timestamp here\n"), false)
	if len(events) != 1 || events[0].Fault == nil || events[0].Fault.Code != verdict.InternalError ||
		events[0].Fault.Text != "the probe's log has a line with no timestamp" || !events[0].Fault.At.Equal(logAt(60_000)) {
		t.Errorf("events = %+v, want one internal-error for the line, at now", events)
	}
	if raw != "no timestamp here\n" {
		t.Errorf("raw = %q", raw)
	}
}

// breaks gives text and then the error of a connection that broke.
func breaks(text string) io.Reader {
	return io.MultiReader(strings.NewReader(text), iotest.ErrReader(io.ErrUnexpectedEOF))
}

// A piece with no line end that comes with the end of a stream while the
// container runs is dropped: the stream opened again gives the whole line, and
// taking the piece would make that line a repeat.
func TestFollowDropsAPieceWhenTheLogIsOpenedAgain(t *testing.T) {
	ok, fail := `{"ok":true}`, `{"ok":false,"class":"refused"}`
	cut := logAt(1500).Format(time.RFC3339Nano) + " " // cut after the timestamp: log text if taken
	for _, c := range []struct {
		name  string
		first io.Reader
	}{
		{"the stream ends with io.EOF", strings.NewReader(logLine(logAt(1000), ok) + cut)},
		{"the stream breaks", breaks(logLine(logAt(1000), ok) + cut)},
	} {
		t.Run(c.name, func(t *testing.T) {
			second := logLine(logAt(1000), ok) + logLine(logAt(1500), fail)
			l := &fakeLog{streams: []io.Reader{c.first, strings.NewReader(second)}}
			f, events, raw := followAll(t, l, false)
			if len(events) != 2 || events[1].Obs == nil || events[1].Obs.OK || !events[1].Obs.Written.Equal(logAt(1500)) {
				t.Errorf("events = %+v, want the pass and the failure at +1500 ms", events)
			}
			if want := logLine(logAt(1000), ok) + logLine(logAt(1500), fail); raw != want {
				t.Errorf("raw = %q, want %q", raw, want)
			}
			if lines, _ := f.counts(); lines != 2 {
				t.Errorf("lines = %d, want 2", lines)
			}
		})
	}
}

// Where the follower cannot tell that it read the log to its end, it emits the
// gap at now; where it can, it emits none.
func TestFollowGapAtTheEnd(t *testing.T) {
	ok := `{"ok":true}`
	piece := logAt(200).Format(time.RFC3339Nano) + ` {"ok":fa`
	for _, c := range []struct {
		name    string
		streams []io.Reader
		gap     bool
		raw     string
	}{
		{"a stream breaks and the container has ended",
			[]io.Reader{breaks(logLine(logAt(100), ok) + piece)}, true, logLine(logAt(100), ok) + piece + "\n"},
		{"a stream breaks and the log cannot be opened again",
			[]io.Reader{breaks(logLine(logAt(100), ok)), nil}, true, logLine(logAt(100), ok)},
		{"a stream opened again ends before the lines of the last timestamp came back",
			[]io.Reader{strings.NewReader(logLine(logAt(1700), ok)), strings.NewReader("")}, true, logLine(logAt(1700), ok)},
		{"a stream opened again breaks inside a repeat and the container has ended",
			[]io.Reader{strings.NewReader(logLine(logAt(1700), ok)), breaks(logAt(1700).Format(time.RFC3339Nano) + ` {"ok":tr`)},
			true, logLine(logAt(1700), ok)},
		{"a stream ends with io.EOF and the log cannot be opened again",
			[]io.Reader{strings.NewReader(logLine(logAt(100), ok)), nil}, false, logLine(logAt(100), ok)},
		{"a stream opened again gives back every line of the last timestamp",
			[]io.Reader{strings.NewReader(logLine(logAt(1700), ok)), strings.NewReader(logLine(logAt(1700), ok))}, false, logLine(logAt(1700), ok)},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, events, raw := followAll(t, &fakeLog{streams: c.streams}, false)
			want := 1
			if c.gap {
				want = 2
			}
			if len(events) != want || events[0].Obs == nil {
				t.Fatalf("events = %+v, want the observation and %d more", events, want-1)
			}
			if g := events[len(events)-1].Fault; c.gap && (g == nil || g.Code != verdict.InternalError ||
				g.Text != "the probe's log could not be followed without a gap" || !g.At.Equal(logAt(60_000))) {
				t.Errorf("last event = %+v, want internal-error for the gap, at now", events[len(events)-1])
			}
			if raw != c.raw {
				t.Errorf("raw = %q, want %q", raw, c.raw)
			}
			if lines, _ := f.counts(); lines != 1 {
				t.Errorf("lines = %d, want 1", lines)
			}
		})
	}
}

// failingWriter refuses every write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("no space left on device") }

// A write to raw that fails changes nothing that the follower reports.
func TestFollowGoesOnWhenRawFails(t *testing.T) {
	quickPolling(t)
	l := newFakeLog(logLine(logAt(100), `{"ok":true}`) + logLine(logAt(200), `{"ok":false}`))
	var events []Event
	f := newFollower(l.open, l.running, failingWriter{}, func() time.Time { return logAt(60_000) },
		func(e Event) { events = append(events, e) })
	f.run(context.Background())
	if len(events) != 2 || events[0].Obs == nil || events[1].Obs == nil {
		t.Errorf("events = %+v, want both observations", events)
	}
}
