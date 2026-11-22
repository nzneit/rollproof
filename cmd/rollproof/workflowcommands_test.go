package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode"
)

// stopToken replaces newStopToken, for the test, by one that gives token, and puts it back with
// t.Cleanup.
func stopToken(t *testing.T, token string) {
	t.Helper()
	real := newStopToken
	newStopToken = func() string { return token }
	t.Cleanup(func() { newStopToken = real })
}

// noStopToken replaces newStopToken, for the test, by one that fails the test when called, and
// puts it back with t.Cleanup.
func noStopToken(t *testing.T) {
	t.Helper()
	real := newStopToken
	newStopToken = func() string {
		t.Error("newStopToken was called")
		return "NOTOKEN"
	}
	t.Cleanup(func() { newStopToken = real })
}

// githubEnv is a getenv that gives GITHUB_ACTIONS "true" and nothing else.
func githubEnv(name string) string {
	if name == "GITHUB_ACTIONS" {
		return "true"
	}
	return ""
}

// fakeCommand writes "a line of the run\n" to stdout and "an error of the run\n" to stderr and
// returns 1.
func fakeCommand(stdout, stderr io.Writer) func() int {
	return func() int {
		fmt.Fprint(stdout, "a line of the run\n")
		fmt.Fprint(stderr, "an error of the run\n")
		return 1
	}
}

// tokenForm is the form of a token of the real newStopToken: 26 characters of crypto/rand.Text.
var tokenForm = regexp.MustCompile(`^[A-Z2-7]{26}$`)

// splitLines gives s's lines: its text without its last line end, split at each line end; none
// for an empty text.
func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// stoppedRun runs withWorkflowCommandsStopped with stopToken "TESTTOKEN", getenv githubEnv, a
// signalled that gives signal, and a command that records what stdout and stderr hold at its
// start and then does what fakeCommand does. It checks the streams at the command's start, and
// that signalled was asked once, after the command returned; it gives the exit code and the two
// streams.
func stoppedRun(t *testing.T, signal bool) (code int, stdout, stderr string) {
	t.Helper()
	stopToken(t, "TESTTOKEN")
	var out, errOut bytes.Buffer
	var atStartOut, atStartErr string
	returned := false
	asked, askedAfter := 0, 0
	signalled := func() bool {
		asked++
		if returned {
			askedAfter++
		}
		return signal
	}
	command := func() int {
		atStartOut, atStartErr = out.String(), errOut.String()
		code := fakeCommand(&out, &errOut)()
		returned = true
		return code
	}
	code = withWorkflowCommandsStopped(&out, &errOut, githubEnv, signalled, command)
	if want := "::stop-commands::TESTTOKEN\n"; atStartOut != want || atStartErr != want {
		t.Errorf("at the command's start stdout held %q and stderr %q, want each %q", atStartOut, atStartErr, want)
	}
	if asked != 1 || askedAfter != 1 {
		t.Errorf("signalled was asked %d times, %d of them after the command returned; want once, after", asked, askedAfter)
	}
	return code, out.String(), errOut.String()
}

func TestWorkflowCommandsAreStoppedAroundTheRun(t *testing.T) {
	code, stdout, stderr := stoppedRun(t, false)
	if code != 1 {
		t.Errorf("withWorkflowCommandsStopped returned %d, want 1", code)
	}
	if want := "::stop-commands::TESTTOKEN\na line of the run\n::TESTTOKEN::\n"; stdout != want {
		t.Errorf("stdout is %q, want %q", stdout, want)
	}
	if want := "::stop-commands::TESTTOKEN\nan error of the run\n"; stderr != want {
		t.Errorf("stderr is %q, want %q", stderr, want)
	}
}

func TestNoResumeLineAfterASignal(t *testing.T) {
	code, stdout, stderr := stoppedRun(t, true)
	if code != 1 {
		t.Errorf("withWorkflowCommandsStopped returned %d, want 1", code)
	}
	if want := "::stop-commands::TESTTOKEN\na line of the run\n"; stdout != want {
		t.Errorf("stdout is %q, want %q", stdout, want)
	}
	if want := "::stop-commands::TESTTOKEN\nan error of the run\n"; stderr != want {
		t.Errorf("stderr is %q, want %q", stderr, want)
	}
}

func TestNoWorkflowCommandLinesOffGitHub(t *testing.T) {
	// valueOf gives a getenv whose GITHUB_ACTIONS is value.
	valueOf := func(value string) func(string) string {
		return func(name string) string {
			if name == "GITHUB_ACTIONS" {
				return value
			}
			return ""
		}
	}
	for _, tc := range []struct {
		name   string
		getenv func(string) string
	}{
		{"nil", nil},
		{"GITHUB_ACTIONS unset", noEnv},
		{`""`, valueOf("")},
		{`"false"`, valueOf("false")},
		{`"TRUE"`, valueOf("TRUE")},
		{`"1"`, valueOf("1")},
		{`" true"`, valueOf(" true")},
		{`"true "`, valueOf("true ")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			noStopToken(t)
			signalled := func() bool {
				t.Error("signalled was called")
				return false
			}
			var stdout, stderr bytes.Buffer
			if code := withWorkflowCommandsStopped(&stdout, &stderr, tc.getenv, signalled, fakeCommand(&stdout, &stderr)); code != 1 {
				t.Errorf("withWorkflowCommandsStopped returned %d, want 1", code)
			}
			if want := "a line of the run\n"; stdout.String() != want {
				t.Errorf("stdout is %q, want %q", stdout.String(), want)
			}
			if want := "an error of the run\n"; stderr.String() != want {
				t.Errorf("stderr is %q, want %q", stderr.String(), want)
			}
		})
	}
}

// A panic that escapes the run is no end by itself that the step can trust: no resume line is
// printed, so the commands stay stopped. That the runner then reads the panic's trace on stderr
// as plain lines is read in its source, not shown here (decision 26).
func TestAPanicInTheRunLeavesNoResumeLine(t *testing.T) {
	stopToken(t, "TESTTOKEN")
	signalled := func() bool {
		t.Error("signalled was called")
		return false
	}
	var stdout, stderr bytes.Buffer
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		withWorkflowCommandsStopped(&stdout, &stderr, githubEnv, signalled, func() int { panic("the run broke") })
	}()
	if recovered != "the run broke" {
		t.Errorf("the panic that reached the test is %v, want %q", recovered, "the run broke")
	}
	if want := "::stop-commands::TESTTOKEN\n"; stdout.String() != want || stderr.String() != want {
		t.Errorf("stdout is %q and stderr %q, want each %q", stdout.String(), stderr.String(), want)
	}
}

func TestEachRunHasItsOwnToken(t *testing.T) {
	var tokens []string
	for i := range 2 {
		var stdout, stderr bytes.Buffer
		withWorkflowCommandsStopped(&stdout, &stderr, githubEnv, func() bool { return false }, func() int { return 0 })
		lines := splitLines(stdout.String())
		if len(lines) != 2 {
			t.Fatalf("call %d: stdout is %q, want two lines", i+1, stdout.String())
		}
		token, ok := strings.CutPrefix(lines[0], "::stop-commands::")
		if !ok || !tokenForm.MatchString(token) {
			t.Fatalf("call %d: stdout's first line is %q, want %q and a token that %s matches", i+1, lines[0], "::stop-commands::", tokenForm)
		}
		if want := "::" + token + "::"; lines[1] != want {
			t.Errorf("call %d: stdout's second line is %q, want %q", i+1, lines[1], want)
		}
		if want := lines[0] + "\n"; stderr.String() != want {
			t.Errorf("call %d: stderr is %q, want %q", i+1, stderr.String(), want)
		}
		tokens = append(tokens, token)
	}
	if tokens[0] == tokens[1] {
		t.Errorf("both calls had the token %q, want two", tokens[0])
	}
}

// The lines come first and last on every path where the run command ends by itself, its usage
// errors included, through realMain and the real units.
func TestTheRunCommandsLinesComeFirstAndLast(t *testing.T) {
	const stop, resume = "::stop-commands::TESTTOKEN", "::TESTTOKEN::"
	stopToken(t, "TESTTOKEN")
	out := t.TempDir()
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		args  []string
		check func(t *testing.T, stdout, stderr string)
	}{
		{"a flag it does not know", []string{"run", "--nope"}, func(t *testing.T, stdout, stderr string) {
			if want := stop + "\n" + resume + "\n"; stdout != want {
				t.Errorf("stdout is %q, want %q", stdout, want)
			}
			lines := splitLines(stderr)
			if len(lines) < 2 || lines[0] != stop || lines[1] != "flag provided but not defined: -nope" {
				t.Errorf("stderr's first two lines are not %q and %q:\n%s", stop, "flag provided but not defined: -nope", stderr)
			}
			if !strings.Contains(stderr, "usage: rollproof run") {
				t.Errorf("stderr does not hold %q:\n%s", "usage: rollproof run", stderr)
			}
		}},
		{"an argument that is no flag", []string{"run", "-f", "rollproof.yaml", "extra"}, func(t *testing.T, stdout, stderr string) {
			if want := stop + "\n" + resume + "\n"; stdout != want {
				t.Errorf("stdout is %q, want %q", stdout, want)
			}
			if lines := splitLines(stderr); len(lines) == 0 || lines[0] != stop {
				t.Errorf("stderr's first line is not %q:\n%s", stop, stderr)
			}
			if !strings.Contains(stderr, "usage: rollproof run") {
				t.Errorf("stderr does not hold %q:\n%s", "usage: rollproof run", stderr)
			}
		}},
		{"a rejected configuration", []string{"run", "-f", "does-not-exist.yaml", "--out", out}, func(t *testing.T, stdout, stderr string) {
			lines := splitLines(stdout)
			n := len(lines)
			if n < 4 || lines[0] != stop || lines[1] != "rollproof: NOT VERIFIED" || lines[n-1] != resume || lines[n-2] != "results: "+out {
				t.Errorf("stdout's first two lines are not %q and %q, or its last two not %q and %q:\n%s",
					stop, "rollproof: NOT VERIFIED", "results: "+out, resume, stdout)
			}
			if want := stop + "\n"; stderr != want {
				t.Errorf("stderr is %q, want %q", stderr, want)
			}
		}},
		{"a results folder that cannot be opened", []string{"run", "-f", "does-not-exist.yaml", "--out", file}, func(t *testing.T, stdout, stderr string) {
			if want := stop + "\n" + resume + "\n"; stdout != want {
				t.Errorf("stdout is %q, want %q", stdout, want)
			}
			lines := splitLines(stderr)
			if len(lines) != 2 || lines[0] != stop || !strings.HasPrefix(lines[1], "rollproof: cannot write results: ") {
				t.Errorf("stderr is not two lines, %q and one that begins %q:\n%s", stop, "rollproof: cannot write results: ", stderr)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := realMain(tc.args, &stdout, &stderr, githubEnv); code != 2 {
				t.Errorf("exit %d, want 2\nstdout:\n%sstderr:\n%s", code, stdout.String(), stderr.String())
			}
			tc.check(t, stdout.String(), stderr.String())
		})
	}
}

// newStopToken replaced by a function that fails the test when called; realMain with getenv
// githubEnv. One row each: ["version"] exits 0; no args exits 2; ["probe", "http", "--url",
// "http://127.0.0.1:1/", "--rate", "0"] exits 2. In each row no line of stdout or stderr begins "::"
// after its leading white space, and none holds "##[".
func TestOnlyTheRunCommandStopsWorkflowCommands(t *testing.T) {
	noStopToken(t)
	for _, tc := range []struct {
		name string
		args []string
		code int
	}{
		{"version", []string{"version"}, 0},
		{"no args", nil, 2},
		{"probe", []string{"probe", "http", "--url", "http://127.0.0.1:1/", "--rate", "0"}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := realMain(tc.args, &stdout, &stderr, githubEnv); code != tc.code {
				t.Errorf("exit %d, want %d", code, tc.code)
			}
			for name, s := range map[string]string{"stdout": stdout.String(), "stderr": stderr.String()} {
				for _, l := range splitLines(s) {
					if strings.HasPrefix(strings.TrimLeftFunc(l, unicode.IsSpace), "::") {
						t.Errorf("%s holds the line %q, which begins %q after its leading white space", name, l, "::")
					}
					if strings.Contains(l, "##[") {
						t.Errorf("%s holds the line %q, which holds %q", name, l, "##[")
					}
				}
			}
		})
	}
}

// childToken gives the token of a child's stop line, the first line of its stdout, which must be
// "::stop-commands::" and a token of the real newStopToken's form; it fails the test otherwise.
func childToken(t *testing.T, c *helperChild) string {
	t.Helper()
	lines := splitLines(c.stdout.String())
	if len(lines) == 0 {
		t.Fatalf("the child's stdout is empty%s", c.output())
	}
	token, ok := strings.CutPrefix(lines[0], "::stop-commands::")
	if !ok || !tokenForm.MatchString(token) {
		t.Fatalf("the child's first line of stdout is %q, want %q and a token that %s matches%s",
			lines[0], "::stop-commands::", tokenForm, c.output())
	}
	return token
}

// After a signal the process ends by it, and the step with it: no resume line (section 8,
// "Workflow commands").
func TestASignalLeavesWorkflowCommandsStopped(t *testing.T) {
	c := startChild(t, fakeWaits, "GITHUB_ACTIONS=true")
	c.waitFor(t, "started")
	c.signal(t, syscall.SIGTERM)
	ws := c.wait(t, 10*time.Second)
	if !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
		t.Errorf("the child ended %s, want by %s%s", ending(ws), syscall.SIGTERM, c.output())
	}
	token := childToken(t, c)
	stop, resume := "::stop-commands::"+token, "::"+token+"::"
	if got, want := splitLines(c.stdout.String()), []string{stop, "started", "returned"}; !slices.Equal(got, want) {
		t.Errorf("the child's stdout is %q, want %q%s", got, want, c.output())
	}
	errLines := splitLines(c.stderr.String())
	if len(errLines) == 0 || errLines[0] != stop {
		t.Errorf("the child's first line of stderr is not %q%s", stop, c.output())
	}
	for name, s := range map[string]string{"stdout": c.stdout.String(), "stderr": c.stderr.String()} {
		for _, l := range splitLines(s) {
			if l == resume {
				t.Errorf("the child's %s holds the resume line %q%s", name, resume, c.output())
			}
		}
	}
}

func TestARunThatEndsByItselfResumesWorkflowCommands(t *testing.T) {
	c := startChild(t, fakeFails, "GITHUB_ACTIONS=true")
	ws := c.wait(t, 20*time.Second)
	if ws.Signaled() || ws.ExitStatus() != 1 {
		t.Errorf("the child ended %s, want with exit 1%s", ending(ws), c.output())
	}
	token := childToken(t, c)
	stop := "::stop-commands::" + token
	if got, want := splitLines(c.stdout.String()), []string{stop, fakeFailsLine, "::" + token + "::"}; !slices.Equal(got, want) {
		t.Errorf("the child's stdout is %q, want %q%s", got, want, c.output())
	}
	if errLines := splitLines(c.stderr.String()); len(errLines) == 0 || errLines[0] != stop {
		t.Errorf("the child's first line of stderr is not %q%s", stop, c.output())
	}
}
