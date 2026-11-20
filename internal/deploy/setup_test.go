//go:build linux

package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"rollproof/internal/config"
)

func TestSetupRunsInOrder(t *testing.T) {
	f := newFake()
	cmds := [][]string{{"kubectl", "apply", "-f", "test/prerequisites.yaml"}, {"kubectl", "create", "secret", "generic", "probe-token"}}
	r, i := Setup(t.Context(), f, cmds, 5*time.Minute)
	if r.Name != "setup" || r.Ending != Succeeded || i != -1 {
		t.Errorf("Setup gave %+v and index %d, want setup to succeed and index -1", r, i)
	}
	want := []Command{{Name: "setup", Args: cmds[0], Timeout: 5 * time.Minute}, {Name: "setup", Args: cmds[1], Timeout: 5 * time.Minute}}
	if d := cmp.Diff(want, f.calls); d != "" {
		t.Errorf("commands (-want +got):\n%s", d)
	}
}

func TestSetupWithNoCommands(t *testing.T) {
	f := newFake()
	r, i := Setup(t.Context(), f, nil, time.Minute)
	if r.Name != "setup" || r.Ending != Succeeded || r.ExitCode != 0 || i != -1 || len(f.calls) != 0 {
		t.Errorf("Setup with no commands gave %+v and index %d after %d commands, want setup to succeed, exit code 0, index -1, none run", r, i, len(f.calls))
	}
}

func TestSetupStopsAtTheFirstFailure(t *testing.T) {
	h := newHarness(t, os.Environ())
	marker := filepath.Join(t.TempDir(), "third-ran")
	r, i := Setup(t.Context(), h.runner, [][]string{{"true"}, {"sh", "-c", "exit 4"}, {"touch", marker}}, time.Minute)
	if r.Name != "setup" || r.Ending != ExitedNonZero || r.ExitCode != 4 || i != 1 {
		t.Errorf("Setup gave %+v and index %d, want setup, exit-nonzero, exit code 4, index 1", r, i)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("the third command ran (stat: %v)", err)
	}
	if got := h.read(t, "setup"); !strings.HasSuffix(got, "rollproof: ok, exit code 0\n") {
		t.Errorf("evidence setup %q", got)
	}
	if got := h.read(t, "setup-2"); !strings.HasSuffix(got, "rollproof: exit-nonzero, exit code 4\n") {
		t.Errorf("evidence setup-2 %q", got)
	}
	if _, err := os.Stat(filepath.Join(h.evidence, "setup-3")); !os.IsNotExist(err) {
		t.Errorf("evidence setup-3 exists (stat: %v)", err)
	}
}

func TestSetupEndings(t *testing.T) {
	setKillAfter(t, 200*time.Millisecond)
	h := newHarness(t, os.Environ())
	t.Cleanup(h.runner.Close)

	if r, _ := Setup(t.Context(), h.runner, [][]string{{"rollproof-test-no-such-program"}}, time.Minute); r.Ending != NotStarted || r.ExitCode != -1 {
		t.Errorf("a program that cannot start: %+v, want not-started, exit code -1", r)
	}

	start := time.Now()
	if r, _ := Setup(t.Context(), h.runner, [][]string{{"sh", "-c", "sleep 30"}}, 300*time.Millisecond); r.Ending != StoppedAtLimit {
		t.Errorf("a command past its limit: %+v, want stopped-at-limit", r)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("a command past its limit of 300ms returned after %v", d)
	}

	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(100*time.Millisecond, cancel)
	start = time.Now()
	if r, _ := Setup(ctx, h.runner, [][]string{{"sh", "-c", "sleep 30"}}, time.Minute); r.Ending != StoppedBySignal {
		t.Errorf("a command whose ctx ended: %+v, want stopped-by-signal", r)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("a command whose ctx ended returned after %v", d)
	}
}

// Review Focus 1: a command that reads its standard input, or asks a question, goes on or fails
// at once and never waits until the limit.
func TestSetupCommandsReadNoInput(t *testing.T) {
	h := newHarness(t, os.Environ())
	start := time.Now()
	r, _ := Setup(t.Context(), h.runner, [][]string{
		{"sh", "-c", `read line; echo "read gave $?"`},
		{"sh", "-c", `printf "continue? [y/N] "; read answer || exit 3`},
	}, 30*time.Second)
	if r.Ending != ExitedNonZero || r.ExitCode != 3 {
		t.Errorf("Setup gave %+v, want exit-nonzero, exit code 3", r)
	}
	if got := h.read(t, "setup"); !strings.Contains(got, "read gave 1") {
		t.Errorf("evidence setup %q, want the read to have met end of file", got)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("the setup commands took %v; a read waited for input", d)
	}

	drv := NewCommands(CommandsOptions{Commands: config.Commands{Install: []string{"sh", "-c", "read answer || exit 5"}},
		Timeout: 30 * time.Second, Runner: h.runner})
	if r := drv.Install(t.Context()); r.Ending != ExitedNonZero || r.ExitCode != 5 || r.Ended.Sub(r.Began) > 5*time.Second {
		t.Errorf("the install gave %+v, want exit-nonzero, exit code 5, within 5 s", r)
	}
}
