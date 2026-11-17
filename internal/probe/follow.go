package probe

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"rollproof/internal/verdict"
)

// Event is one thing a running probe reported: an observation, or a fault of the probe with the
// moment it happened. Exactly one is set.
type Event struct {
	Obs   *verdict.Observation
	Fault *verdict.Condition
}

// pollEvery is how often the probe's pod is read, and how long a failed
// opening of its log waits before the next.
var pollEvery = 200 * time.Millisecond

// logSource opens the log of the container named probe, following, with
// timestamps, from since, or from its start when since is nil.
type logSource func(ctx context.Context, since *time.Time) (io.ReadCloser, error)

// The texts of the faults that the log itself gives.
const (
	gapText         = "the probe's log could not be followed without a gap"
	noTimestampText = "the probe's log has a line with no timestamp"
)

// follower reads one probe's log through every opening of it, so that no line is lost and none
// is read twice, and turns each line into an Event. The reopening rule is the one that
// docs/proofs/p11-log-rotation.md records.
type follower struct {
	open    logSource
	running func(ctx context.Context) bool
	raw     io.Writer
	now     func() time.Time
	emit    func(Event)

	asked           atomic.Bool // stopAsked was called
	lines, jsonText atomic.Int64

	// Read and written by run alone.
	read   bool      // a line has been read
	last   time.Time // the runtime's timestamp of the last line read
	atLast int       // how many of the lines read carry last
}

// stream is what run knows of one opening of the log.
type stream struct {
	skipping bool // the log was opened again, and no line that is not a repeat has come yet
	skipped  int  // how many lines at the last timestamp read were skipped in this stream
}

// newFollower returns a follower of one probe's log. running says whether
// the container named probe still runs; raw receives every line as it came;
// now gives the moment of a fault that has no time of its own; emit receives
// every observation and every fault, in the order of the log.
func newFollower(open logSource, running func(ctx context.Context) bool, raw io.Writer, now func() time.Time, emit func(Event)) *follower {
	return &follower{open: open, running: running, raw: raw, now: now, emit: emit}
}

// stopAsked tells the follower that Stop was asked: from then on a last
// piece with no line end is kept in raw and not read as a line.
func (f *follower) stopAsked() { f.asked.Store(true) }

// counts gives how many lines were read, and how many of them were JSON
// objects with no ok field. It is safe to call while run runs.
func (f *follower) counts() (lines, jsonText int) {
	return int(f.lines.Load()), int(f.jsonText.Load())
}

// run follows the log until it has ended for good or ctx ends.
//
// It opens the log from its start until a line has been read, and after that from the second of
// the last line read; a failed opening is tried again after pollEvery while the container runs.
// A stream that ends while the container runs is opened again at once. A last piece with no line
// end that came with that end is dropped, because the next stream gives the whole line; when the
// log has ended for good, the piece is a line before stopAsked and is only kept in raw after it.
// Where run cannot tell that it read the log to its end, it emits the gap at now: when a stream
// ended with an error other than io.EOF and the container no longer runs, so that the stream is
// not opened again or cannot be; and when a stream opened again ended, with the container ended,
// before as many lines of the last timestamp as were read had come back.
func (f *follower) run(ctx context.Context) {
	broken := false // a stream ended with an error other than io.EOF, and none has opened since
	for {
		var since *time.Time
		if f.read {
			s := f.last.Truncate(time.Second)
			since = &s
		}
		rc, err := f.open(ctx, since)
		if err != nil {
			switch {
			case ctx.Err() != nil:
				return
			case !f.running(ctx):
				if broken {
					f.gap()
				}
				return
			case !pause(ctx, pollEvery):
				return
			}
			continue
		}
		broken = false
		s := &stream{skipping: since != nil}
		piece, err := f.follow(ctx, rc, s)
		switch {
		case ctx.Err() != nil:
			f.keep(s, piece)
			return
		case f.running(ctx):
			broken = !errors.Is(err, io.EOF)
		case !errors.Is(err, io.EOF):
			f.keep(s, piece)
			f.gap()
			return
		default:
			if piece != "" && !f.asked.Load() {
				f.take(s, piece, piece+"\n")
			} else {
				f.keep(s, piece)
			}
			if s.skipping && s.skipped < f.atLast {
				f.gap()
			}
			return
		}
	}
}

// follow reads one stream to its end and closes it, also when ctx ends while a read waits. It
// returns the piece with no line end that came with the end, and the error that ended it.
func (f *follower) follow(ctx context.Context, rc io.ReadCloser, s *stream) (piece string, err error) {
	closed := make(chan struct{})
	stopClosing := context.AfterFunc(ctx, func() {
		rc.Close()
		close(closed)
	})
	defer func() {
		if stopClosing() {
			rc.Close()
		} else {
			<-closed
		}
	}()
	br := bufio.NewReader(rc)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return line, err
		}
		f.take(s, strings.TrimSuffix(line, "\n"), line)
	}
}

// take reads one line: line has no line end, and asCame is the line as it came, with its line
// end. A line whose timestamp does not parse is written to raw and is a fault at now. In a
// stream opened again a repeat is skipped, and the first line that is not one ends the
// skipping, after the gap when fewer lines of the last timestamp came back than were read.
// Every other line is written to raw, counted, and read by ParseLine with its timestamp.
func (f *follower) take(s *stream, line, asCame string) {
	stamp, text, _ := strings.Cut(line, " ")
	ts, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		f.write(asCame)
		f.emit(Event{Fault: &verdict.Condition{Code: verdict.InternalError, At: f.now(), Text: noTimestampText}})
		return
	}
	if s.skipping {
		if f.repeat(s, ts) {
			return
		}
		s.skipping = false
		if s.skipped < f.atLast {
			f.gap()
		}
	}
	f.write(asCame)
	if f.read && ts.Equal(f.last) {
		f.atLast++
	} else {
		f.last, f.atLast = ts, 1
	}
	f.read = true
	f.lines.Add(1)
	l := ParseLine(text, ts)
	switch l.Kind {
	case Observed:
		obs := l.Obs
		f.emit(Event{Obs: &obs})
	case Invalid:
		f.emit(Event{Fault: &verdict.Condition{Code: verdict.ProbeLineInvalid, At: ts, Text: l.Fault}})
	case JSONText:
		f.jsonText.Add(1)
	}
}

// repeat reports whether a line stamped ts, in the stream s opened again, is one that was read
// before: its timestamp is before the last one read, or equal to it while fewer lines with it
// were skipped in s than were read. It counts each repeat at the last timestamp.
func (f *follower) repeat(s *stream, ts time.Time) bool {
	switch {
	case ts.Before(f.last):
		return true
	case ts.Equal(f.last) && s.skipped < f.atLast:
		s.skipped++
		return true
	}
	return false
}

// keep writes a piece with no line end to raw with a line end, and does nothing else with it. A
// piece that repeats a line read before is not written.
func (f *follower) keep(s *stream, piece string) {
	if piece == "" {
		return
	}
	stamp, _, _ := strings.Cut(piece, " ")
	if ts, err := time.Parse(time.RFC3339Nano, stamp); err == nil && s.skipping && f.repeat(s, ts) {
		return
	}
	f.write(piece + "\n")
}

// gap emits the fault of a stretch of the log that may be lost, at now.
func (f *follower) gap() {
	f.emit(Event{Fault: &verdict.Condition{Code: verdict.InternalError, At: f.now(), Text: gapText}})
}

// write gives s to raw. A failed write is not reported: raw is evidence, the verdict does not
// read it, and the follower has nothing to act on.
func (f *follower) write(s string) {
	io.WriteString(f.raw, s)
}

// pause waits d, and reports false when ctx ended first.
func pause(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
