package main

import (
	"crypto/rand"
	"fmt"
	"io"
	"syscall"

	"rollproof/internal/report"
)

// newStopToken gives the token of one run's lines for GitHub's workflow commands:
// 26 random characters of crypto/rand.Text, which no command of the runner is
// named, new for each run so that no text from outside the tool can name it, as it
// could name a fixed token or one from the environment. A test replaces it, and
// puts it back with t.Cleanup.
var newStopToken = func() string { return rand.Text() }

// withWorkflowCommandsStopped runs command, and returns its exit code, between the
// lines that stop and resume the workflow commands of GitHub's runner (design,
// section 8, "Workflow commands"). On GitHub Actions, as report.GitHubActions
// detects it, it first prints "::stop-commands::TOKEN" on stdout and then on
// stderr, TOKEN new for each call: the runner reads each stream in its own order,
// so each needs the line first, and the one that the runner meets second is a
// plain line. The runner then reads no line of the command as a command, but for
// the two things that the design's section 8 leaves open. When command has
// returned and signalled says that no signal came, it prints "::TOKEN::" on
// stdout, so that a step that runs the tool from a script of its own can use
// workflow commands again. After a signal it prints nothing more: the process
// ends by the signal, and the step with it. A panic in command passes through
// with no resume line: the commands stay stopped for the rest of the step, and the
// panic's trace is read as plain lines (decision 26). Elsewhere it only runs
// command. A nil getenv is not GitHub Actions.
func withWorkflowCommandsStopped(stdout, stderr io.Writer, getenv func(string) string, signalled func() bool, command func() int) int {
	if getenv == nil || !report.GitHubActions(getenv) {
		return command()
	}
	token := newStopToken()
	// Each stream is read in its own order, so each gets the stop line before anything else. The
	// lines are written as they are, not through the run's masker: they hold no value of the
	// configuration.
	fmt.Fprintln(stdout, "::stop-commands::"+token)
	fmt.Fprintln(stderr, "::stop-commands::"+token)
	code := command()
	if !signalled() {
		fmt.Fprintln(stdout, "::"+token+"::")
	}
	return code
}

// signalCame says whether runContext kept a signal, by which endProcess will end
// the process. runCommand's release has returned by the time
// withWorkflowCommandsStopped asks, so the answer is final; a signal that comes
// later takes its default action and ends the process at once.
func signalCame() bool {
	_, ok := keptSignal.Load().(syscall.Signal)
	return ok
}
