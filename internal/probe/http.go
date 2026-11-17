package probe

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// lookupHost resolves the URL's host once, at the start of a run. Tests replace it.
var lookupHost = func(ctx context.Context, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

// httpLine is one line of the probe's output. The fields are written in this order. N is left
// out when zero, so a failure never carries it; Class is left out when empty.
type httpLine struct {
	OK    bool    `json:"ok"`
	N     int     `json:"n,omitempty"`
	T     string  `json:"t"`
	Class string  `json:"class,omitempty"`
	Ms    float64 `json:"ms"`
	Lag   float64 `json:"lag"`
}

// secondOfPasses gathers the requests that passed and ended in one second of the run.
type secondOfPasses struct {
	n       int
	first   time.Time
	ms, lag float64
}

// httpRun is the state of one RunHTTP call that its requests share.
type httpRun struct {
	o      HTTPOptions
	client *http.Client
	start  time.Time

	writeMu sync.Mutex
	out     io.Writer

	gatherMu sync.Mutex
	gathered map[int]*secondOfPasses
}

// RunHTTP sends GET requests to o.URL on an open schedule, each on a new connection, and writes
// contract lines to out until ctx ends. Then it starts no new request, waits for those in flight,
// writes their lines and returns nil. It returns an error only before the first request: the URL
// does not parse or names no host, or the host cannot be resolved.
//
// When o.Warmup is above zero, RunHTTP first sends one check at a time, leaving no more than the
// rate's interval between two of them, and writes nothing for them. The observations begin once
// its checks have kept passing for o.Warmup, timed from when the first of them returned; a failed
// check starts that time again. A target that never answers leaves the probe with no observation
// at all, so the tool ends the run not verified when its wait for the first observation
// (limits.timeout) runs out. A ctx that ends during the warm-up returns nil with nothing written.
//
// A write to out that fails is not reported: RunHTTP's caller has nothing to act on. A reader of
// the log sees the silence; on a pipe, a failed write ends the process with SIGPIPE.
func RunHTTP(ctx context.Context, o HTTPOptions, out io.Writer) error {
	u, err := url.Parse(o.URL)
	if err != nil {
		return errors.New("the probe's URL does not parse")
	}
	if u.Hostname() == "" {
		return errors.New("the probe's URL names no host")
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	addrs, err := lookupHost(ctx, u.Hostname())
	if err != nil {
		return fmt.Errorf("the probe's host cannot be resolved: %w", err)
	}
	if len(addrs) == 0 {
		return errors.New("the probe's host resolved to no address")
	}
	target := net.JoinHostPort(addrs[0], port)
	dialer := &net.Dialer{Timeout: o.Timeout}
	tr := &http.Transport{
		// The address the transport asks for is the URL's host; the probe dials the one it
		// resolved at the start.
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, target)
		},
		Proxy:              nil,
		DisableKeepAlives:  true,
		DisableCompression: true,
		// The kubelet's own HTTP check does not verify the certificate either. ServerName stays
		// empty, so the TLS server name is the URL's host.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	r := &httpRun{
		o: o,
		client: &http.Client{
			Transport:     tr,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		out:      out,
		gathered: map[int]*secondOfPasses{},
	}

	if o.Warmup > 0 && !r.warmUp(ctx) {
		return nil
	}

	var wg sync.WaitGroup
	r.start = time.Now()
	// The summary lines have a goroutine of their own, so that a Write that blocks never stops
	// the schedule.
	stopTicks := make(chan struct{})
	ticksDone := make(chan struct{})
	go func() {
		defer close(ticksDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopTicks:
				return
			case <-ticker.C:
				r.flush(-1)
			}
		}
	}()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for i := 0; ; {
		select {
		case <-ctx.Done():
			wg.Wait()
			close(stopTicks)
			<-ticksDone
			r.flush(math.MaxInt)
			return nil
		case <-timer.C:
			if ctx.Err() != nil {
				continue // select picks among ready cases at random: start nothing once ctx ended
			}
			due := r.start.Add(time.Duration(i) * time.Second / time.Duration(o.Rate))
			wg.Add(1)
			go func() {
				defer wg.Done()
				r.request(ctx, due)
			}()
			i++
			timer.Reset(time.Until(r.start.Add(time.Duration(i) * time.Second / time.Duration(o.Rate))))
		}
	}
}

// warmUp sends one request at a time until the checks have passed without a failure for
// o.Warmup, and reports whether that happened. The time is measured from when the first check of a
// run of passes returned, so that one slow success does not stand for the whole warm-up. It writes
// nothing: a failed check only starts the time again. No more than the rate's interval is left
// between two checks.
func (r *httpRun) warmUp(ctx context.Context) bool {
	interval := time.Second / time.Duration(r.o.Rate)
	var since time.Time // when the first successful check of the run returned; zero before that
	for {
		if ctx.Err() != nil {
			return false
		}
		began := time.Now()
		rctx, cancel := context.WithTimeout(ctx, r.o.Timeout)
		class, err := r.get(rctx)
		returned := time.Now()
		cancel()
		if err != nil || class != "" {
			since = time.Time{}
		} else {
			if since.IsZero() {
				since = returned
			}
			if returned.Sub(since) >= r.o.Warmup {
				return true
			}
		}
		wait := interval - returned.Sub(began)
		if wait <= 0 {
			continue
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

// request sends one request that was due at due, and writes or gathers its line.
func (r *httpRun) request(ctx context.Context, due time.Time) {
	began := time.Now()
	lag := began.Sub(due)
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.o.Timeout)
	defer cancel()
	class, err := r.get(rctx)
	if err != nil {
		class = classify(err)
	}
	ms := time.Since(began)
	if class == "" {
		r.gather(began, ms, lag)
		return
	}
	r.write(httpLine{T: stamp(began), Class: class, Ms: millis(ms), Lag: millis(lag)})
}

// get sends the request and reads the answer's body to its end. It gives the status as a class
// when the status is outside the range that passes, and "" when the request passed.
func (r *httpRun) get(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.o.URL, nil)
	if err != nil {
		return "", err
	}
	for name, value := range r.o.Headers {
		if strings.EqualFold(name, "Host") {
			req.Host = value
		} else {
			req.Header.Set(name, value)
		}
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return "", err
	}
	if resp.StatusCode < r.o.ExpectMin || resp.StatusCode > r.o.ExpectMax {
		return strconv.Itoa(resp.StatusCode), nil
	}
	return "", nil
}

// gather adds a request that passed to the second of the run in which it ended.
func (r *httpRun) gather(began time.Time, ms, lag time.Duration) {
	r.gatherMu.Lock()
	defer r.gatherMu.Unlock()
	k := int(time.Since(r.start) / time.Second)
	s := r.gathered[k]
	if s == nil {
		s = &secondOfPasses{first: began}
		r.gathered[k] = s
	}
	s.n++
	if began.Before(s.first) {
		s.first = began
	}
	s.ms = max(s.ms, millis(ms))
	s.lag = max(s.lag, millis(lag))
}

// flush writes one summary line for each gathered second that has ended, oldest first. The
// count of ended seconds is read under the lock that gather holds, so a pass is never put into
// a second that has been flushed. A negative upTo means the seconds ended so far; any other
// value is the first second that is not flushed. The lines are written after the lock is
// released, so a slow Write holds up no request. Only one flush runs at a time.
func (r *httpRun) flush(upTo int) {
	var lines []httpLine
	r.gatherMu.Lock()
	if upTo < 0 {
		upTo = int(time.Since(r.start) / time.Second)
	}
	for _, k := range slices.Sorted(maps.Keys(r.gathered)) {
		if k >= upTo {
			break
		}
		s := r.gathered[k]
		delete(r.gathered, k)
		lines = append(lines, httpLine{OK: true, N: s.n, T: stamp(s.first), Ms: s.ms, Lag: s.lag})
	}
	r.gatherMu.Unlock()
	for _, l := range lines {
		r.write(l)
	}
}

// write sends one line, the JSON and a line end in one Write, under a mutex.
func (r *httpRun) write(l httpLine) {
	l.OK = l.Class == ""
	b, _ := json.Marshal(l) // a struct of these fields always marshals
	b = append(b, '\n')
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	r.out.Write(b)
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func millis(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// classify names how a request failed. It comes from the file RESEARCH/2026-11-12/L5/src/
// loadgen/main.go of the host repository, the load generator of the experiment L5.
func classify(err error) string {
	var ne net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "refused"
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE):
		return "reset"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "eof"
	}
	return "other"
}
