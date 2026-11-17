package probe

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"rollproof/internal/verdict"
)

// capture is the io.Writer the tests give RunHTTP. It keeps each Write with
// the moment it was made.
type capture struct {
	mu  sync.Mutex
	got []capturedWrite
}

type capturedWrite struct {
	at   time.Time
	text string
}

func (c *capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, capturedWrite{time.Now(), string(p)})
	return len(p), nil
}

// observations fails the test unless every Write was one whole line that
// ParseLine reads as an observation, with the Write's moment as written.
func (c *capture) observations(t *testing.T) []verdict.Observation {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	var obs []verdict.Observation
	for _, s := range c.got {
		text, ok := strings.CutSuffix(s.text, "\n")
		if !ok || strings.Contains(text, "\n") {
			t.Fatalf("a Write was not one line: %q", s.text)
		}
		line := ParseLine(text, s.at)
		if line.Kind != Observed {
			t.Fatalf("not an observation (%s): %q", line.Fault, text)
		}
		obs = append(obs, line.Obs)
	}
	return obs
}

// testOptions are the settings of these tests: rate 50, timeout 2 s, 200-299.
func testOptions(url string) HTTPOptions {
	return HTTPOptions{URL: url, Rate: 50, Timeout: 2 * time.Second, ExpectMin: 200, ExpectMax: 299}
}

// runFor runs RunHTTP for d and then cancels its context. It fails the test
// unless RunHTTP returns nil within o.Timeout plus 1 s of the cancel.
func runFor(t *testing.T, o HTTPOptions, d time.Duration) *capture {
	t.Helper()
	out := &capture{}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- RunHTTP(ctx, o, out) }()
	time.Sleep(d)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunHTTP: %v", err)
		}
	case <-time.After(o.Timeout + time.Second):
		t.Fatalf("RunHTTP did not return within %v of the cancel", o.Timeout+time.Second)
	}
	return out
}

// checks is the number of checks that obs stands for.
func checks(obs []verdict.Observation) int {
	n := 0
	for _, o := range obs {
		n += o.N
	}
	return n
}

func TestRunHTTPOpenModel(t *testing.T) {
	var started atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		started.Add(1)
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()
	obs := runFor(t, testOptions(srv.URL), time.Second).observations(t)
	if n := started.Load(); n < 45 || n > 55 {
		t.Errorf("%d requests started in 1 s at rate 50, want 45 to 55", n)
	}
	if n := checks(obs); int64(n) != started.Load() {
		t.Errorf("the lines stand for %d checks, the server saw %d", n, started.Load())
	}
}

func TestRunHTTPLag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	obs := runFor(t, testOptions(srv.URL), 1500*time.Millisecond).observations(t)
	if len(obs) == 0 {
		t.Fatal("no line")
	}
	for _, o := range obs {
		if !o.OK || o.Lag >= 50 {
			t.Errorf("got %+v, want a pass with lag under 50 ms", o)
		}
	}
}

func TestRunHTTPNewConnectionForEachRequest(t *testing.T) {
	var mu sync.Mutex
	requests, addrs := 0, map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests++
		addrs[r.RemoteAddr] = true
	}))
	defer srv.Close()
	runFor(t, testOptions(srv.URL), 500*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if requests < 20 || len(addrs) != requests {
		t.Errorf("%d requests came from %d remote addresses, want one address for each", requests, len(addrs))
	}
}

func TestRunHTTPFailureClasses(t *testing.T) {
	hijacked := func(w http.ResponseWriter) net.Conn {
		conn, _, _ := w.(http.Hijacker).Hijack()
		return conn
	}
	var redirected atomic.Int64
	tests := []struct {
		name    string
		class   string
		handler http.HandlerFunc // nil: a port on which nothing listens
	}{
		{"a status outside the range", "503", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}},
		{"a redirect is judged and not followed", "302", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/elsewhere" {
				redirected.Add(1)
				return
			}
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		}},
		{"connection refused", "refused", nil},
		{"connection reset", "reset", func(w http.ResponseWriter, _ *http.Request) {
			conn := hijacked(w)
			conn.(*net.TCPConn).SetLinger(0)
			conn.Close()
		}},
		{"a body cut short after the headers", "eof", func(w http.ResponseWriter, _ *http.Request) {
			conn := hijacked(w)
			conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nshort"))
			conn.Close()
		}},
		{"no answer within the timeout", "timeout", func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}},
		{"headers, then a body that stalls past the timeout", "timeout", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}},
		{"an answer that is not HTTP", "other", func(w http.ResponseWriter, _ *http.Request) {
			conn := hijacked(w)
			conn.Write([]byte("not http\r\n\r\n"))
			conn.Close()
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var url string
			if tc.handler == nil {
				ln, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				url = "http://" + ln.Addr().String() + "/"
				ln.Close()
			} else {
				srv := httptest.NewServer(tc.handler)
				defer srv.Close()
				url = srv.URL + "/"
			}
			o := testOptions(url)
			o.Rate, o.Timeout = 20, 300*time.Millisecond
			out := runFor(t, o, 300*time.Millisecond)
			for _, s := range out.got {
				var fields map[string]any
				if err := json.Unmarshal([]byte(s.text), &fields); err != nil {
					t.Fatalf("%q: %v", s.text, err)
				}
				keys := slices.Sorted(maps.Keys(fields))
				if !slices.Equal(keys, []string{"class", "lag", "ms", "ok", "t"}) {
					t.Errorf("a failure line has the fields %v, want class, lag, ms, ok and t", keys)
				}
			}
			obs := out.observations(t)
			if len(obs) == 0 {
				t.Fatal("no line")
			}
			for _, ob := range obs {
				if ob.OK || ob.Class != tc.class {
					t.Errorf("got %+v, want a failure of class %q", ob, tc.class)
				}
			}
		})
	}
	if n := redirected.Load(); n != 0 {
		t.Errorf("the redirect was followed %d times", n)
	}
}

func TestRunHTTPSummaryLines(t *testing.T) {
	var answered atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		if answered.Add(1) == 10 {
			time.Sleep(120 * time.Millisecond)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}))
	defer srv.Close()
	obs := runFor(t, testOptions(srv.URL), 2500*time.Millisecond).observations(t)
	if len(obs) < 2 || len(obs) > 4 {
		t.Errorf("%d summary lines in 2.5 s, want 2 to 4", len(obs))
	}
	slowest := 0.0
	for i, o := range obs {
		if !o.OK || o.N < 1 {
			t.Errorf("got %+v, want a summary line", o)
		}
		if i > 0 && !o.T.After(obs[i-1].T) {
			t.Errorf("summary line %d begins at %v, not after the line before it", i, o.T)
		}
		slowest = max(slowest, o.Ms)
	}
	if slowest < 120 {
		t.Errorf("the longest ms is %v, want at least 120", slowest)
	}
	if n := checks(obs); int64(n) != answered.Load() {
		t.Errorf("the lines stand for %d checks, the server answered %d", n, answered.Load())
	}
}

func TestRunHTTPHeadersAndHost(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, r.Host+" "+r.Header.Get("X-Forwarded-Proto")+" "+r.Header.Get("X-Query"))
	}))
	defer srv.Close()
	o := testOptions(srv.URL)
	o.Headers = map[string]string{"hOsT": "orders.example", "X-Forwarded-Proto": "https", "X-Query": "a=b"}
	// A second Host header would make the server answer 400, which fails the line.
	for _, ob := range runFor(t, o, 500*time.Millisecond).observations(t) {
		if !ob.OK {
			t.Errorf("got %+v, want a pass", ob)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 {
		t.Fatal("no request arrived")
	}
	for _, s := range seen {
		if s != "orders.example https a=b" {
			t.Errorf("the server received %q, want %q", s, "orders.example https a=b")
		}
	}
}

func TestRunHTTPSWithoutCertificateCheck(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	obs := runFor(t, testOptions(srv.URL), 500*time.Millisecond).observations(t)
	if len(obs) == 0 {
		t.Fatal("no line")
	}
	for _, o := range obs {
		if !o.OK {
			t.Errorf("got %+v, want a pass", o)
		}
	}
}

func TestRunHTTPResolvesOnce(t *testing.T) {
	var mu sync.Mutex
	var hosts, serverNames []string
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		hosts = append(hosts, r.Host)
	}))
	srv.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		mu.Lock()
		defer mu.Unlock()
		serverNames = append(serverNames, hello.ServerName)
		return nil, nil
	}}
	srv.StartTLS()
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	var calls atomic.Int64
	defer func(saved func(context.Context, string) ([]string, error)) { lookupHost = saved }(lookupHost)
	lookupHost = func(_ context.Context, host string) ([]string, error) {
		calls.Add(1)
		if host != "probe-target.test" {
			return nil, errors.New("unexpected host " + host)
		}
		return []string{"127.0.0.1"}, nil
	}
	obs := runFor(t, testOptions("https://probe-target.test:"+port+"/"), time.Second).observations(t)
	if n := calls.Load(); n != 1 {
		t.Errorf("the host was looked up %d times, want 1", n)
	}
	if checks(obs) < 40 {
		t.Errorf("%d checks, want about 50", checks(obs))
	}
	mu.Lock()
	defer mu.Unlock()
	for _, h := range hosts {
		if h != "probe-target.test:"+port {
			t.Errorf("Host %q, want %q", h, "probe-target.test:"+port)
		}
	}
	for _, n := range serverNames {
		if n != "probe-target.test" {
			t.Errorf("TLS server name %q, want probe-target.test", n)
		}
	}
}

func TestRunHTTPUnresolvableHost(t *testing.T) {
	defer func(saved func(context.Context, string) ([]string, error)) { lookupHost = saved }(lookupHost)
	lookupHost = func(context.Context, string) ([]string, error) { return nil, errors.New("no such host") }
	out := &capture{}
	if err := RunHTTP(t.Context(), testOptions("http://nowhere.test/"), out); err == nil {
		t.Error("RunHTTP returned nil for a host that does not resolve")
	}
	if len(out.got) != 0 {
		t.Errorf("RunHTTP wrote %d lines", len(out.got))
	}
}

func TestRunHTTPDrainsOnCancel(t *testing.T) {
	var started atomic.Int64
	var lastStart atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		started.Add(1)
		lastStart.Store(time.Now().UnixNano())
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()
	o := testOptions(srv.URL)
	o.Timeout = time.Second
	cancelled := time.Now().Add(time.Second)
	obs := runFor(t, o, time.Second).observations(t)
	if late := time.Unix(0, lastStart.Load()).Sub(cancelled); late > 100*time.Millisecond {
		t.Errorf("a request reached the server %v after the cancel", late)
	}
	if n := checks(obs); int64(n) != started.Load() {
		t.Errorf("the lines stand for %d checks, %d requests started", n, started.Load())
	}
}

func TestRunHTTPNeverSilentWhileItWorks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(600 * time.Millisecond)
	}))
	defer srv.Close()
	o := testOptions(srv.URL)
	o.Timeout = time.Second
	began := time.Now()
	out := runFor(t, o, 3*time.Second)
	out.observations(t)
	bound := o.Timeout + time.Second
	previous := began
	for _, s := range out.got {
		if gap := s.at.Sub(previous); gap > bound {
			t.Errorf("no line for %v, want at most %v", gap, bound)
		}
		previous = s.at
	}
	if len(out.got) == 0 {
		t.Error("no line")
	}
}

func TestRunHTTPSendsQueryAndPort(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, r.URL.Path+"?"+r.URL.RawQuery)
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	runFor(t, testOptions("http://127.0.0.1:"+port+"/work?ms=20"), 300*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 {
		t.Fatal("no request arrived")
	}
	for _, s := range seen {
		if s != "/work?ms=20" {
			t.Errorf("the server saw %q, want /work?ms=20", s)
		}
	}
}
