package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"rollproof/internal/cluster"
	"rollproof/internal/config"
	"rollproof/internal/deploy"
	"rollproof/internal/probe"
	"rollproof/internal/verdict"
)

func noEnv(string) string { return "" }

// lockedBuffer is a bytes.Buffer that several goroutines may write to. first
// is closed by the first write.
type lockedBuffer struct {
	mu    sync.Mutex
	b     bytes.Buffer
	first chan struct{}
	once  sync.Once
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.first != nil {
		l.once.Do(func() { close(l.first) })
	}
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// readResult reads the reasons of OUT/result.json.
func readResult(t *testing.T, out string) (final bool, codes, texts []string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Final   bool `json:"final"`
		Reasons []struct {
			Code string `json:"code"`
			Text string `json:"text"`
		} `json:"reasons"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	for _, x := range r.Reasons {
		codes = append(codes, x.Code)
		texts = append(texts, x.Text)
	}
	return r.Final, codes, texts
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"nope"},
		{"run", "--nope"},
		{"run", "-f", "rollproof.yaml", "extra"},
		{"probe"},
		{"probe", "tcp"},
		{"version", "extra"},
	} {
		var stdout, stderr bytes.Buffer
		if code := realMain(args, &stdout, &stderr, noEnv); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
		if !strings.Contains(stderr.String(), "usage: rollproof run") {
			t.Errorf("%q: no usage text on stderr:\n%s", args, stderr.String())
		}
		if n := strings.Count(stderr.String(), "\n"); n > 10 {
			t.Errorf("%q: %d lines on stderr, want at most 10", args, n)
		}
	}
}

// TestUsageTextIsExact pins the whole usage text, line by line and indent,
// against a literal written here rather than the command's own constant.
func TestUsageTextIsExact(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := realMain(nil, &stdout, &stderr, noEnv); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	want := `usage: rollproof run [-f FILE] [--out DIR] [--name NAME] [--keep-cluster]
       rollproof version
       rollproof probe http --url URL --rate N --timeout D --expect-status RANGE [--header NAME=VALUE]...
`
	if got := stderr.String(); got != want {
		t.Errorf("stderr is\n%s\nwant\n%s", got, want)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout holds %q, want nothing", stdout.String())
	}
}

func TestVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := realMain([]string{"version"}, &stdout, &stderr, noEnv); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	prefixes := []string{"rollproof ", "kind ", "node image ", "registry image "}
	if len(lines) != len(prefixes) {
		t.Fatalf("%d lines, want %d:\n%s", len(lines), len(prefixes), stdout.String())
	}
	for i, p := range prefixes {
		if !strings.HasPrefix(lines[i], p) {
			t.Errorf("line %d is %q, want the prefix %q", i+1, lines[i], p)
		}
	}
	if !strings.Contains(lines[2], "@sha256:") {
		t.Errorf("the node image has no digest: %q", lines[2])
	}
}

func TestRunWithoutAFile(t *testing.T) {
	for _, flag := range []string{"-out", "--out"} {
		out := t.TempDir()
		var stdout, stderr bytes.Buffer
		if code := realMain([]string{"run", "-f", "does-not-exist.yaml", flag, out}, &stdout, &stderr, noEnv); code != 2 {
			t.Fatalf("%s: exit %d, want 2", flag, code)
		}
		final, codes, _ := readResult(t, out)
		if !final || len(codes) != 1 || codes[0] != "config-rejected" {
			t.Errorf("%s: final %v, reasons %q; want a final file with config-rejected", flag, final, codes)
		}
	}
}

// TestRunWithoutAFileFlag leaves -f out, in an empty folder, and pins that the
// reason names rollproof.yaml: the default, and nothing else.
func TestRunWithoutAFileFlag(t *testing.T) {
	t.Chdir(t.TempDir())
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := realMain([]string{"run", "--out", out}, &stdout, &stderr, noEnv); code != 2 {
		t.Fatalf("exit %d, want 2\n%s", code, stderr.String())
	}
	final, codes, texts := readResult(t, out)
	if !final || len(codes) != 1 || codes[0] != "config-rejected" {
		t.Fatalf("final %v, reasons %q, want a final file with one config-rejected", final, codes)
	}
	if !strings.Contains(texts[0], "rollproof.yaml") {
		t.Errorf("the reason is %q, want it to name rollproof.yaml", texts[0])
	}
}

func TestRunNotBuilt(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "chart"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "rollproof.yaml")
	text := `version: 1
deploy:
  namespace: orders
  helm:
    release: orders
    previous: { chart: ./chart }
    candidate: { chart: ./chart }
probe:
  custom:
    image: registry.example/tools/busybox@sha256:` + strings.Repeat("0", 64) + `
    command: [sh, -c, "sleep 3600"]
`
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"-keep-cluster", "--keep-cluster"} {
		out := t.TempDir()
		var stdout, stderr bytes.Buffer
		if code := realMain([]string{"run", "-f", file, "--out", out, flag}, &stdout, &stderr, noEnv); code != 2 {
			t.Fatalf("%s: exit %d, want 2\n%s", flag, code, stderr.String())
		}
		final, codes, texts := readResult(t, out)
		if !final || len(codes) != 1 || codes[0] != "config-rejected" || texts[0] != "not built yet: probe.custom" {
			t.Errorf("%s: final %v, reasons %q %q", flag, final, codes, texts)
		}
	}
}

func TestFailedClientError(t *testing.T) {
	clientErr := errors.New("no route to the API server")
	reached := "the cluster's API server cannot be reached: no route to the API server"
	left := reached + ". Parts of the run may be left behind; the next run with this name removes them."
	cleanUp := errors.New("the engine is gone")
	for _, tc := range []struct {
		name       string
		keep       bool
		cancelled  bool
		deleteErr  error
		wantCalls  int
		wantText   string
		wantJoined string
	}{
		{"kept", true, false, cleanUp, 0, reached, ""},
		{"the run's context has ended", false, true, cleanUp, 0, reached, ""},
		{"the delete succeeds", false, false, nil, 1, reached, ""},
		{"the delete fails", false, false, cleanUp, 1, left,
			"the clean-up after it failed: the engine is gone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.cancelled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			calls := 0
			del := func(context.Context) error {
				calls++
				return tc.deleteErr
			}
			err := failedClientError(ctx, tc.keep, del, clientErr)
			if calls != tc.wantCalls {
				t.Errorf("the delete was called %d times, want %d", calls, tc.wantCalls)
			}
			if err == nil {
				t.Fatal("no error")
			}
			got := verdict.AsCondition(err)
			if got.Code != verdict.ClusterFailed || got.Text != tc.wantText {
				t.Errorf("condition %s %q, want cluster-failed %q", got.Code, got.Text, tc.wantText)
			}
			if tc.wantJoined != "" && !strings.Contains(err.Error(), tc.wantJoined) {
				t.Errorf("the error is %q, want it to hold %q", err.Error(), tc.wantJoined)
			}
		})
	}
}

func TestEndCommands(t *testing.T) {
	t.Run("without Deployer", func(t *testing.T) {
		var s session
		s.EndCommands() // must not panic
	})
	t.Run("after Deployer", func(t *testing.T) {
		dir := t.TempDir()
		cfg := &config.Config{
			Dir:    dir,
			Deploy: config.Deploy{Helm: &config.Helm{Previous: &config.Release{}}},
		}
		ev := &recordingEvidence{}
		s := &session{
			c:        &cluster.Cluster{Kubeconfig: filepath.Join(dir, "kubeconfig")},
			cfg:      cfg,
			evidence: func(string) (io.WriteCloser, error) { return ev, nil },
		}
		if _, _, err := s.Deployer(cfg); err != nil {
			t.Fatal(err)
		}

		pidFile := filepath.Join(dir, "pid")
		// A failing EndCommands leaves the TERM-ignoring command running: kill its
		// group so that the failure does not outlive the test. ESRCH when it is gone.
		t.Cleanup(func() {
			data, err := os.ReadFile(pidFile)
			if err != nil {
				return // the command never started
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				return
			}
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ran := make(chan deploy.Result, 1)
		go func() {
			ran <- s.runner.Run(ctx, deploy.Command{Name: "c", Args: []string{"sh", "-c",
				`echo $$ > "$0"; trap "" TERM; while :; do sleep 0.05; done`, pidFile}})
		}()
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(pidFile); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("the command did not start")
			}
			time.Sleep(10 * time.Millisecond)
		}
		cancel()

		var res deploy.Result
		select {
		case res = <-ran:
		case <-time.After(5 * time.Second):
			t.Fatal("the runner did not return after its context ended")
		}
		if res.Ending != deploy.StoppedBySignal {
			t.Fatalf("ending %s, want stopped-by-signal", res.Ending)
		}

		s.EndCommands()
		if !ev.isClosed() {
			t.Error("EndCommands did not close the runner's evidence")
		}
	})
}

// recordingEvidence is an evidence writer that records that it was closed.
type recordingEvidence struct {
	mu     sync.Mutex
	closed bool
}

func (e *recordingEvidence) Write(p []byte) (int, error) { return len(p), nil }

func (e *recordingEvidence) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	return nil
}

func (e *recordingEvidence) isClosed() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closed
}

func TestProbeHTTP(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := realMain([]string{"probe", "http", "--url", "http://127.0.0.1:1/", "--rate", "0"}, &stdout, &stderr, noEnv); code != 2 {
		t.Errorf("rate 0: exit %d, want 2", code)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	old := probeContext
	probeContext = func() (context.Context, context.CancelFunc) { return ctx, cancel }
	t.Cleanup(func() { probeContext = old })

	out := &lockedBuffer{first: make(chan struct{})}
	var errOut bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- realMain([]string{"probe", "http", "--url", srv.URL + "/", "--rate", "20", "--timeout", "2s",
			"--expect-status", "200-299"}, out, &errOut, noEnv)
	}()
	select {
	case <-out.first:
	case <-time.After(5 * time.Second):
		t.Fatal("probe http wrote no line before the deadline")
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d, want 0\n%s", code, errOut.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("probe http did not return within 5 s of the cancel")
	}
	text := strings.TrimSuffix(out.String(), "\n")
	if text == "" {
		t.Fatal("probe http wrote no line")
	}
	written := time.Now()
	for _, line := range strings.Split(text, "\n") {
		if got := probe.ParseLine(line, written); got.Kind != probe.Observed {
			t.Errorf("line %q is not an observation", line)
		}
	}
}

// TestProbeHTTPErrorExitsOne pins the probe's exit code and stderr line when
// RunHTTP fails. %zz is not a URL, so RunHTTP rejects it before it resolves
// anything: the test reaches no network.
func TestProbeHTTPErrorExitsOne(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := realMain([]string{"probe", "http", "--url", "%zz", "--rate", "1", "--timeout", "1s",
		"--expect-status", "200-299"}, &stdout, &stderr, noEnv)
	if code != 1 {
		t.Fatalf("exit %d, want 1\n%s", code, stderr.String())
	}
	if !strings.HasPrefix(stderr.String(), "rollproof: ") {
		t.Errorf("stderr is %q, want it to start with %q", stderr.String(), "rollproof: ")
	}
	if !strings.Contains(stderr.String(), "the probe's URL does not parse") {
		t.Errorf("stderr is %q, want the run's error text", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout holds %q, want nothing", stdout.String())
	}
}
