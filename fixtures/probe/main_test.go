package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// server is a target that answers /version with one status and its bodies in turn, the last one
// for good, and records the moment of each request.
type server struct {
	url   string
	mu    sync.Mutex
	times []time.Time
}

// target starts a server for the test.
func target(t *testing.T, status int, bodies ...string) *server {
	t.Helper()
	s := &server{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			http.NotFound(w, r)
			return
		}
		s.mu.Lock()
		i := len(s.times)
		s.times = append(s.times, time.Now())
		s.mu.Unlock()
		w.WriteHeader(status)
		w.Write([]byte(bodies[min(i, len(bodies)-1)]))
	}))
	t.Cleanup(srv.Close)
	s.url = srv.URL
	return s
}

// first returns the moment of the server's first request, and false when none came.
func (s *server) first() (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.times) == 0 {
		return time.Time{}, false
	}
	return s.times[0], true
}

// output is the probe's standard output, safe to read while run writes it. It records each write
// as one piece, so that a test can see a line written in more than one write.
type output struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	pieces []string
}

func (o *output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.pieces = append(o.pieces, string(p))
	return o.buf.Write(p)
}

func (o *output) text() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

// lines splits the output into lines and decodes each as a JSON object, failing the test on one
// that is not an object or that came in more than one write.
func lines(t *testing.T, o *output) []map[string]any {
	t.Helper()
	o.mu.Lock()
	for _, p := range o.pieces {
		if !strings.HasSuffix(p, "\n") || strings.Count(p, "\n") != 1 {
			t.Errorf("a write that is not one whole line: %q", p)
		}
	}
	o.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSuffix(o.text(), "\n"), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil || m == nil {
			t.Fatalf("the line %q is not a JSON object: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

// start runs the probe with env in a goroutine. It returns the output, term, and run's result.
func start(env map[string]string) (*output, chan struct{}, <-chan error) {
	o := &output{}
	term := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- run(func(k string) string { return env[k] }, o, term) }()
	return o, term, done
}

// returns waits up to d for run's result and fails the test unless it is nil.
func returns(t *testing.T, done <-chan error, d time.Duration) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run = %v, want nil", err)
		}
	case <-time.After(d):
		t.Fatalf("run did not return within %v", d)
	}
}

func TestWritesALinePerCheck(t *testing.T) {
	s := target(t, 200, "previous\n")
	o, term, done := start(map[string]string{"TARGET": s.url})
	time.Sleep(300 * time.Millisecond)
	close(term)
	returns(t, done, time.Second)
	got := lines(t, o)
	if len(got) < 3 {
		t.Fatalf("%d lines, want 3 or more", len(got))
	}
	for _, line := range got {
		ms, isNumber := line["ms"].(float64)
		if line["ok"] != true || !isNumber || ms < 0 || len(line) != 2 {
			t.Errorf("the line %v; want ok true, ms 0 or more, and no other field", line)
		}
	}
}

func TestFailedCheckLines(t *testing.T) {
	s := target(t, 503, "unavailable\n")
	o, term, done := start(map[string]string{"TARGET": s.url})
	time.Sleep(200 * time.Millisecond)
	close(term)
	returns(t, done, time.Second)
	got := lines(t, o)
	if len(got) == 0 {
		t.Fatal("no line")
	}
	for _, line := range got {
		ms, isNumber := line["ms"].(float64)
		if line["ok"] != false || line["class"] != "503" || !isNumber || ms < 0 || len(line) != 3 {
			t.Errorf("the line %v; want {\"ok\":false,\"class\":\"503\",\"ms\":…}", line)
		}
	}
}

func TestSilentOnceItReadsTheVersion(t *testing.T) {
	s := target(t, 200, "previous\n", "previous\n", "previous\n", "candidate\n")
	o, term, done := start(map[string]string{"TARGET": s.url, "ON_VERSION": "silent", "VERSION": "candidate"})
	time.Sleep(500 * time.Millisecond)
	if n := len(lines(t, o)); n != 3 {
		t.Fatalf("%d lines after 500 ms, want 3", n)
	}
	time.Sleep(500 * time.Millisecond)
	if n := len(lines(t, o)); n != 3 {
		t.Fatalf("%d lines after 1 s, want still 3", n)
	}
	close(term)
	returns(t, done, time.Second)
}

func TestExitsOnceItReadsTheVersion(t *testing.T) {
	s := target(t, 200, "previous\n", "previous\n", "previous\n", "candidate\n")
	o, _, done := start(map[string]string{"TARGET": s.url, "ON_VERSION": "exit", "VERSION": "candidate"})
	returns(t, done, 2*time.Second)
	if n := len(lines(t, o)); n != 3 {
		t.Errorf("%d lines, want 3", n)
	}
}

func TestFailsFirstEveryTwoMilliseconds(t *testing.T) {
	s := target(t, 200, "previous\n")
	began := time.Now()
	o, term, done := start(map[string]string{"TARGET": s.url, "FAIL_FIRST": "200ms"})
	time.Sleep(500 * time.Millisecond)
	close(term)
	returns(t, done, time.Second)
	text := strings.Split(strings.TrimSuffix(o.text(), "\n"), "\n")
	failed := 0
	for failed < len(text) && text[failed] == `{"ok":false,"class":"fixture"}` {
		failed++
	}
	if failed < 80 || failed > 101 {
		t.Errorf("%d failed-check lines first, want 80 to 101", failed)
	}
	for _, line := range lines(t, o)[failed:] {
		if line["ok"] != true {
			t.Errorf("the line %v after the failed ones; want ok true", line)
		}
	}
	first, ok := s.first()
	if !ok {
		t.Fatal("the server got no request")
	}
	if d := first.Sub(began); d < 200*time.Millisecond {
		t.Errorf("the first request came %v after run began, want 200 ms or more", d)
	}
}

func TestRejectsAnUnknownOnVersion(t *testing.T) {
	o, _, done := start(map[string]string{"TARGET": "http://127.0.0.1:1", "ON_VERSION": "bogus"})
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "ON_VERSION") {
			t.Errorf("run = %v, want an error that names ON_VERSION", err)
		}
	case <-time.After(time.Second):
		t.Fatal("run did not return at once")
	}
	if text := o.text(); text != "" {
		t.Errorf("run wrote %q, want nothing", text)
	}
}

func TestRejectsABadFailFirst(t *testing.T) {
	for _, value := range []string{"soon", "-5s"} {
		t.Run(value, func(t *testing.T) {
			o, _, done := start(map[string]string{"TARGET": "http://127.0.0.1:1", "FAIL_FIRST": value})
			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), "FAIL_FIRST") {
					t.Errorf("run = %v, want an error that names FAIL_FIRST", err)
				}
			case <-time.After(time.Second):
				t.Fatal("run did not return at once")
			}
			if text := o.text(); text != "" {
				t.Errorf("run wrote %q, want nothing", text)
			}
		})
	}
}

func TestIgnoresTermOnlyWhenAsked(t *testing.T) {
	for value, want := range map[string]bool{"true": true, "": false, "false": false} {
		if got := ignoresTerm(func(string) string { return value }); got != want {
			t.Errorf("ignoresTerm with IGNORE_TERM=%q = %v, want %v", value, got, want)
		}
	}
}
