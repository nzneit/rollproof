//go:build linux

package deploy

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

const testKubeconfig = "/run/rollproof-test/kubeconfig"

// helmVariables are the eleven variables of section 7, sorted.
var helmVariables = []string{
	"HELM_DRIVER", "HELM_DRIVER_SQL_CONNECTION_STRING", "HELM_KUBEAPISERVER", "HELM_KUBEASGROUPS",
	"HELM_KUBEASUSER", "HELM_KUBECAFILE", "HELM_KUBECONTEXT", "HELM_KUBEINSECURE_SKIP_TLS_VERIFY",
	"HELM_KUBETLS_SERVER_NAME", "HELM_KUBETOKEN", "HELM_NAMESPACE",
}

type harness struct {
	dir, evidence string
	runner        *OSRunner
}

// newHarness gives a runner whose commands start in a new folder and whose
// evidence files go to a second new folder.
func newHarness(t *testing.T, environ []string) harness {
	t.Helper()
	h := harness{dir: t.TempDir(), evidence: t.TempDir()}
	evidence := h.evidence
	h.runner = NewRunner(RunnerOptions{
		Dir:        h.dir,
		Kubeconfig: testKubeconfig,
		Environ:    environ,
		Evidence: func(name string) (io.WriteCloser, error) {
			return os.Create(filepath.Join(evidence, name))
		},
	})
	return h
}

func (h harness) read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.evidence, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// setKillAfter shortens the wait between SIGTERM and SIGKILL for one test.
func setKillAfter(t *testing.T, d time.Duration) {
	old := killAfter
	killAfter = d
	t.Cleanup(func() { killAfter = old })
}

// setWaitDelay shortens how long Run waits for the output to close after the child exited.
func setWaitDelay(t *testing.T, d time.Duration) {
	old := waitDelay
	waitDelay = d
	t.Cleanup(func() { waitDelay = old })
}

// waitFor fails the test unless cond becomes true within d.
func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("%s did not happen within %v", what, d)
}

// groupOf reads the process id that a script wrote to path; with Setpgid it is also the group's id.
func groupOf(t *testing.T, path string) int {
	t.Helper()
	var pid int
	waitFor(t, 2*time.Second, "the script writing its pid", func() bool {
		b, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(b)))
		return err == nil
	})
	return pid
}

func groupGone(pgid int) bool { return syscall.Kill(-pgid, 0) == syscall.ESRCH }

func TestRunStartsInDir(t *testing.T) {
	h := newHarness(t, os.Environ())
	r := h.runner.Run(context.Background(), Command{Name: "pwd", Args: []string{"pwd", "-P"}})
	want, err := filepath.EvalSymlinks(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Ending != Succeeded || strings.TrimSpace(r.Stdout) != want {
		t.Errorf("got %+v, want %q on standard output", r, want)
	}
}

func TestRunUsesNoShell(t *testing.T) {
	h := newHarness(t, os.Environ())
	r := h.runner.Run(context.Background(), Command{Name: "echo", Args: []string{"echo", "$HOME"}})
	if r.Stdout != "$HOME\n" {
		t.Errorf("standard output %q, want the text $HOME", r.Stdout)
	}
}

func TestRunEnvironment(t *testing.T) {
	environ := append(os.Environ(), "KUBECONFIG=/home/someone/.kube/config", "ROLLPROOF_KEEP=yes", "HELM_CACHE_HOME=/tmp/helm-cache")
	for _, name := range helmVariables {
		environ = append(environ, name+"=set-by-the-caller")
	}
	h := newHarness(t, environ)
	r := h.runner.Run(context.Background(), Command{Name: "env", Args: []string{"env"}})
	got := map[string]string{}
	for _, line := range strings.Split(r.Stdout, "\n") {
		if name, value, ok := strings.Cut(line, "="); ok {
			got[name] = value
		}
	}
	if got["KUBECONFIG"] != testKubeconfig {
		t.Errorf("KUBECONFIG is %q, want %q", got["KUBECONFIG"], testKubeconfig)
	}
	for _, name := range helmVariables {
		if _, ok := got[name]; ok {
			t.Errorf("%s reached the command", name)
		}
	}
	if got["ROLLPROOF_KEEP"] != "yes" || got["HELM_CACHE_HOME"] != "/tmp/helm-cache" {
		t.Errorf("a variable not on the list was changed: %q, %q", got["ROLLPROOF_KEEP"], got["HELM_CACHE_HOME"])
	}
	if d := cmp.Diff(helmVariables, h.runner.Removed()); d != "" {
		t.Errorf("Removed (-want +got):\n%s", d)
	}

	two := newHarness(t, []string{"PATH=" + os.Getenv("PATH"), "HELM_NAMESPACE=elsewhere", "HELM_DRIVER=memory"})
	if d := cmp.Diff([]string{"HELM_DRIVER", "HELM_NAMESPACE"}, two.runner.Removed()); d != "" {
		t.Errorf("Removed (-want +got):\n%s", d)
	}
}

func TestRunEndings(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		timeout time.Duration
		ending  Ending
		code    int
	}{
		{"exit 0", []string{"true"}, 0, Succeeded, 0},
		{"exit 3", []string{"sh", "-c", "exit 3"}, 0, ExitedNonZero, 3},
		{"a signal the tool did not send", []string{"sh", "-c", "kill -KILL $$"}, 0, ExitedNonZero, -1},
		{"no such program", []string{"rollproof-test-no-such-program"}, 0, NotStarted, -1},
		{"no program at all", nil, 0, NotStarted, -1},
		{"timeout 0 is no limit", []string{"sleep", "0.5"}, 0, Succeeded, 0},
		{"stopped at the limit", []string{"sleep", "30"}, 300 * time.Millisecond, StoppedAtLimit, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, os.Environ())
			r := h.runner.Run(context.Background(), Command{Name: "c", Args: tc.args, Timeout: tc.timeout})
			if r.Name != "c" || r.Ending != tc.ending || r.ExitCode != tc.code {
				t.Errorf("got %q %s with code %d, want \"c\" %s with code %d", r.Name, r.Ending, r.ExitCode, tc.ending, tc.code)
			}
			if r.Began.IsZero() || r.Ended.Before(r.Began) {
				t.Errorf("Began %v, Ended %v", r.Began, r.Ended)
			}
		})
	}
}

func TestRunSendsSIGTERMAtTheLimit(t *testing.T) {
	h := newHarness(t, os.Environ())
	termFile := filepath.Join(t.TempDir(), "term")
	script := `trap 'echo term > "$0"; exit 0' TERM; sleep 30 & wait`
	r := h.runner.Run(context.Background(), Command{Name: "c", Args: []string{"sh", "-c", script, termFile}, Timeout: 300 * time.Millisecond})
	if r.Ending != StoppedAtLimit || r.ExitCode != -1 {
		t.Errorf("got %s with code %d, want stopped-at-limit with code -1", r.Ending, r.ExitCode)
	}
	if b, err := os.ReadFile(termFile); err != nil || string(b) != "term\n" {
		t.Errorf("the command did not get SIGTERM: %q, %v", b, err)
	}
}

func TestRunKillsAfterKillAfter(t *testing.T) {
	setKillAfter(t, 300*time.Millisecond)
	h := newHarness(t, os.Environ())
	start := time.Now()
	r := h.runner.Run(context.Background(), Command{Name: "c", Args: []string{"sh", "-c", `trap "" TERM; sleep 30`}, Timeout: 300 * time.Millisecond})
	if took := time.Since(start); took < 550*time.Millisecond || took > 1500*time.Millisecond {
		t.Errorf("Run took %v, want about 600ms", took)
	}
	if r.Ending != StoppedAtLimit {
		t.Errorf("ending %s, want stopped-at-limit", r.Ending)
	}
}

func TestRunStopsTheWholeGroup(t *testing.T) {
	h := newHarness(t, os.Environ())
	pidFile := filepath.Join(t.TempDir(), "pid")
	r := h.runner.Run(context.Background(), Command{Name: "c", Args: []string{"sh", "-c", `echo $$ > "$0"; sleep 30 & wait`, pidFile}, Timeout: 300 * time.Millisecond})
	if r.Ending != StoppedAtLimit {
		t.Fatalf("ending %s, want stopped-at-limit", r.Ending)
	}
	pgid := groupOf(t, pidFile)
	waitFor(t, 2*time.Second, "the end of every process of the group", func() bool { return groupGone(pgid) })
}

func TestRunReturnsAtOnceOnSignal(t *testing.T) {
	setKillAfter(t, 500*time.Millisecond)
	h := newHarness(t, os.Environ())
	pidFile, termFile := filepath.Join(t.TempDir(), "pid"), filepath.Join(t.TempDir(), "term")
	script := `echo $$ > "$0"; trap 'echo term > "$1"' TERM; while :; do sleep 0.05; done`
	ctx, cancel := context.WithCancel(context.Background())
	var cancelled time.Time
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancelled = time.Now()
		cancel()
	}()
	r := h.runner.Run(ctx, Command{Name: "c", Args: []string{"sh", "-c", script, pidFile, termFile}})
	returned := time.Now()
	if r.Name != "c" || r.Ending != StoppedBySignal || r.ExitCode != -1 {
		t.Errorf("got %q %s with code %d, want \"c\" stopped-by-signal with code -1", r.Name, r.Ending, r.ExitCode)
	}
	if took := returned.Sub(cancelled); took > 200*time.Millisecond {
		t.Errorf("Run returned %v after the cancel, want at once", took)
	}
	pgid := groupOf(t, pidFile)
	if groupGone(pgid) {
		t.Error("the command ended before killAfter; Run cannot have returned without waiting")
	}
	waitFor(t, 2*time.Second, "SIGTERM reaching the command", func() bool { _, err := os.Stat(termFile); return err == nil })
	waitFor(t, 3*time.Second, "the kill after killAfter", func() bool { return groupGone(pgid) })
	waitFor(t, 3*time.Second, "the evidence's last line", func() bool {
		return strings.HasSuffix(h.read(t, "c"), "rollproof: stopped-by-signal, exit code -1\n")
	})
}

func TestRunDoesNotWaitForABackgroundProcess(t *testing.T) {
	setWaitDelay(t, 200*time.Millisecond)
	h := newHarness(t, os.Environ())
	pidFile := filepath.Join(t.TempDir(), "pid")
	start := time.Now()
	// The background sleep keeps standard output open after the shell has exited.
	r := h.runner.Run(context.Background(), Command{Name: "c", Args: []string{"sh", "-c", `echo $$ > "$0"; sleep 30 & exit 0`, pidFile}, Timeout: 5 * time.Second})
	took := time.Since(start)
	pgid := groupOf(t, pidFile)
	t.Cleanup(func() { syscall.Kill(-pgid, syscall.SIGKILL) })
	if r.Ending != Succeeded || r.ExitCode != 0 || took > 2*time.Second {
		t.Errorf("got %s with code %d after %v, want ok with code 0 soon after the wait delay", r.Ending, r.ExitCode, took)
	}
}

func TestRunEvidence(t *testing.T) {
	h := newHarness(t, os.Environ())
	script := "echo one; sleep 0.2; echo two >&2; sleep 0.2; printf three"
	h.runner.Run(context.Background(), Command{Name: "mixed", Args: []string{"sh", "-c", script}})
	want := `sh -c "echo one; sleep 0.2; echo two >&2; sleep 0.2; printf three"` + "\none\ntwo\nthree\nrollproof: ok, exit code 0\n"
	if d := cmp.Diff(want, h.read(t, "mixed")); d != "" {
		t.Errorf("evidence (-want +got):\n%s", d)
	}

	h.runner.Run(context.Background(), Command{Name: "same", Args: []string{"sh", "-c", "exit 3"}})
	h.runner.Run(context.Background(), Command{Name: "same", Args: []string{"true"}})
	if got := h.read(t, "same"); !strings.HasSuffix(got, "rollproof: exit-nonzero, exit code 3\n") {
		t.Errorf("first evidence %q", got)
	}
	if got := h.read(t, "same-2"); got != "true\nrollproof: ok, exit code 0\n" {
		t.Errorf("second evidence %q", got)
	}

	h.runner.Run(context.Background(), Command{Name: "absent", Args: []string{"rollproof-test-no-such-program"}})
	if got := h.read(t, "absent"); !strings.HasSuffix(got, "rollproof: not-started, exit code -1\n") {
		t.Errorf("evidence of a command that did not start %q", got)
	}
}

func TestRunLargeOutput(t *testing.T) {
	h := newHarness(t, os.Environ())
	script := "head -c 20971520 /dev/zero; head -c 20971520 /dev/zero >&2"
	r := h.runner.Run(context.Background(), Command{Name: "large", Args: []string{"sh", "-c", script}})
	if r.Ending != Succeeded {
		t.Fatalf("ending %s", r.Ending)
	}
	if len(r.Stdout) != 1<<20 {
		t.Errorf("Stdout holds %d bytes, want the first 1 MiB", len(r.Stdout))
	}
	info, err := os.Stat(filepath.Join(h.evidence, "large"))
	if err != nil {
		t.Fatal(err)
	}
	if size := info.Size(); size < 40<<20 || size > 40<<20+1024 {
		t.Errorf("evidence holds %d bytes, want all 40 MiB of the output", size)
	}
}

func TestRunWritingAfterSIGTERM(t *testing.T) {
	setKillAfter(t, 300*time.Millisecond)
	h := newHarness(t, os.Environ())
	start := time.Now()
	r := h.runner.Run(context.Background(), Command{Name: "c", Args: []string{"sh", "-c", `trap "" TERM; while :; do echo x; done`}, Timeout: 300 * time.Millisecond})
	if took := time.Since(start); r.Ending != StoppedAtLimit || took > 1500*time.Millisecond {
		t.Errorf("got %s after %v, want stopped-at-limit after about 600ms", r.Ending, took)
	}
}

// closeRecorder is an evidence writer that records that it was closed.
type closeRecorder struct {
	mu     sync.Mutex
	closed bool
}

func (c *closeRecorder) Write(p []byte) (int, error) { return len(p), nil }
func (c *closeRecorder) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}
func (c *closeRecorder) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func TestRunKillsAGrandchildThatIgnoresSIGTERM(t *testing.T) {
	setKillAfter(t, 300*time.Millisecond)
	setWaitDelay(t, 200*time.Millisecond)
	h := newHarness(t, os.Environ())
	dir := t.TempDir()
	pidFile, childFile := filepath.Join(dir, "pid"), filepath.Join(dir, "child")
	script := `echo $$ > "$0"; (trap "" TERM; exec sh -c 'echo $$ > "$0"; exec sleep 30' "$1") & wait`
	r := h.runner.Run(context.Background(), Command{Name: "c", Args: []string{"sh", "-c", script, pidFile, childFile}, Timeout: 300 * time.Millisecond})
	if r.Ending != StoppedAtLimit {
		t.Fatalf("ending %s, want stopped-at-limit", r.Ending)
	}
	pgid, child := groupOf(t, pidFile), groupOf(t, childFile)
	if !groupGone(pgid) {
		t.Error("Run returned while a process of the group was still running")
	}
	if err := syscall.Kill(child, 0); err != syscall.ESRCH {
		t.Errorf("the grandchild that ignores SIGTERM is still running: %v", err)
	}
}

func TestRunReturnsAtOnceWhenCtxEndsDuringTheGrace(t *testing.T) {
	setKillAfter(t, 3*time.Second)
	h := newHarness(t, os.Environ())
	t.Cleanup(h.runner.Close)
	ctx, cancel := context.WithCancel(context.Background())
	var cancelled time.Time
	go func() {
		time.Sleep(600 * time.Millisecond)
		cancelled = time.Now()
		cancel()
	}()
	r := h.runner.Run(ctx, Command{Name: "c", Args: []string{"sh", "-c", `trap "" TERM; while :; do sleep 0.05; done`}, Timeout: 300 * time.Millisecond})
	returned := time.Now()
	if r.Ending != StoppedAtLimit || r.ExitCode != -1 {
		t.Errorf("got %s with code %d, want stopped-at-limit with code -1", r.Ending, r.ExitCode)
	}
	if took := returned.Sub(cancelled); took > 200*time.Millisecond {
		t.Errorf("Run returned %v after the cancel, want at once", took)
	}
}

func TestCloseKillsPendingStops(t *testing.T) {
	setKillAfter(t, 5*time.Second)
	rec := &closeRecorder{}
	r := NewRunner(RunnerOptions{Dir: t.TempDir(), Kubeconfig: testKubeconfig, Environ: os.Environ(),
		Evidence: func(string) (io.WriteCloser, error) { return rec, nil }})
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		groupOf(t, pidFile)
		cancel()
	}()
	res := r.Run(ctx, Command{Name: "c", Args: []string{"sh", "-c", `echo $$ > "$0"; trap "" TERM; while :; do sleep 0.05; done`, pidFile}})
	if res.Ending != StoppedBySignal {
		t.Fatalf("ending %s, want stopped-by-signal", res.Ending)
	}
	pgid := groupOf(t, pidFile)
	if groupGone(pgid) {
		t.Fatal("the command ended before Close; the test proves nothing")
	}
	start := time.Now()
	r.Close()
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("Close took %v, want well before killAfter", took)
	}
	if !groupGone(pgid) {
		t.Error("the group is still running after Close")
	}
	if !rec.isClosed() {
		t.Error("the evidence was not closed when Close returned")
	}
}

func TestRunRefusesAndAdapts(t *testing.T) {
	t.Run("PWD is Dir", func(t *testing.T) {
		h := newHarness(t, append(os.Environ(), "PWD=/somewhere/else"))
		r := h.runner.Run(context.Background(), Command{Name: "c", Args: []string{"env"}})
		// env, not sh: sh corrects a PWD that is not its folder, and a Makefile does not.
		var got []string
		for _, line := range strings.Split(r.Stdout, "\n") {
			if strings.HasPrefix(line, "PWD=") {
				got = append(got, line)
			}
		}
		if d := cmp.Diff([]string{"PWD=" + h.dir}, got); d != "" {
			t.Errorf("PWD entries (-want +got):\n%s", d)
		}
	})
	for _, tc := range []struct{ name, kubeconfig, reason string }{
		{"no kubeconfig", "", "no kubeconfig was given"},
		{"relative kubeconfig", "kube/config", "the kubeconfig path is not absolute"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evidence := t.TempDir()
			r := NewRunner(RunnerOptions{Dir: t.TempDir(), Kubeconfig: tc.kubeconfig, Environ: os.Environ(),
				Evidence: func(name string) (io.WriteCloser, error) { return os.Create(filepath.Join(evidence, name)) }})
			res := r.Run(context.Background(), Command{Name: "c", Args: []string{"true"}})
			if res.Ending != NotStarted || res.ExitCode != -1 {
				t.Errorf("got %s with code %d, want not-started with code -1", res.Ending, res.ExitCode)
			}
			b, _ := os.ReadFile(filepath.Join(evidence, "c"))
			if want := "true\nrollproof: could not start: " + tc.reason + "\nrollproof: not-started, exit code -1\n"; string(b) != want {
				t.Errorf("evidence %q, want %q", b, want)
			}
		})
	}
	t.Run("ctx already ended", func(t *testing.T) {
		h := newHarness(t, os.Environ())
		marker := filepath.Join(t.TempDir(), "ran")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r := h.runner.Run(ctx, Command{Name: "c", Args: []string{"sh", "-c", `echo x > "$0"`, marker}})
		time.Sleep(200 * time.Millisecond)
		if r.Ending != StoppedBySignal || r.ExitCode != -1 {
			t.Errorf("got %s with code %d, want stopped-by-signal with code -1", r.Ending, r.ExitCode)
		}
		if _, err := os.Stat(marker); err == nil {
			t.Error("a process was started although ctx had ended")
		}
	})
	t.Run("last line at the limit", func(t *testing.T) {
		h := newHarness(t, os.Environ())
		h.runner.Run(context.Background(), Command{Name: "c", Args: []string{"sleep", "30"}, Timeout: 300 * time.Millisecond})
		if got := h.read(t, "c"); !strings.HasSuffix(got, "rollproof: stopped-at-limit, exit code -1\n") {
			t.Errorf("evidence %q", got)
		}
	})
	t.Run("could-not-start line", func(t *testing.T) {
		h := newHarness(t, os.Environ())
		const program = "rollproof-test-no-such-program"
		_, lookErr := exec.LookPath(program)
		h.runner.Run(context.Background(), Command{Name: "c", Args: []string{program}})
		want := program + "\nrollproof: could not start: " + lookErr.Error() + "\nrollproof: not-started, exit code -1\n"
		if d := cmp.Diff(want, h.read(t, "c")); d != "" {
			t.Errorf("evidence (-want +got):\n%s", d)
		}
	})
	t.Run("Stdout when ctx ends", func(t *testing.T) {
		setKillAfter(t, 300*time.Millisecond)
		h := newHarness(t, os.Environ())
		t.Cleanup(h.runner.Close)
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			waitFor(t, 2*time.Second, "the output", func() bool {
				b, _ := os.ReadFile(filepath.Join(h.evidence, "c"))
				return strings.Contains(string(b), "\nhello\n")
			})
			cancel()
		}()
		r := h.runner.Run(ctx, Command{Name: "c", Args: []string{"sh", "-c", "echo hello; sleep 30"}})
		if r.Ending != StoppedBySignal || r.Stdout != "hello\n" {
			t.Errorf("got %s with Stdout %q, want stopped-by-signal with the output so far", r.Ending, r.Stdout)
		}
	})
	for name, evidence := range map[string]func(string) (io.WriteCloser, error){
		"nil Evidence":     nil,
		"failing Evidence": func(string) (io.WriteCloser, error) { return nil, errors.New("no folder") },
	} {
		t.Run(name, func(t *testing.T) {
			r := NewRunner(RunnerOptions{Dir: t.TempDir(), Kubeconfig: testKubeconfig, Environ: os.Environ(), Evidence: evidence})
			res := r.Run(context.Background(), Command{Name: "c", Args: []string{"echo", "hi"}})
			if res.Ending != Succeeded || res.Stdout != "hi\n" {
				t.Errorf("got %s with Stdout %q, want the command to run", res.Ending, res.Stdout)
			}
		})
	}
}
