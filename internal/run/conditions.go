// Package run is the sequence of one run, from the results folder to the exit
// code. It hides the order of the phases, where each condition is placed, and
// what is cleaned up. It sees the probe and the deploy driver as interfaces
// and holds no rule of the verdict.
package run

import (
	"fmt"

	"rollproof/internal/deploy"
	"rollproof/internal/verdict"
)

// kind classifies how a command ended.
type kind int

const (
	noFailure  kind = iota // success, or a stop that a signal caused
	failed                 // exited non-zero, was not started, or ran to the limit
	unreadable             // exited 0 with an answer the tool could not read
	unknown                // an Ending this build does not know
)

// classify says in words how a command that ended badly ended, for the end of
// a sentence about it, and which kind of ending it was.
func classify(r deploy.Result) (string, kind) {
	switch r.Ending {
	case deploy.Succeeded, deploy.StoppedBySignal:
		return "", noFailure
	case deploy.ExitedNonZero:
		if r.ExitCode == 0 {
			return "gave an answer the tool could not read", unreadable
		}
		return fmt.Sprintf("exited with code %d", r.ExitCode), failed
	case deploy.NotStarted:
		return "could not be started", failed
	case deploy.StoppedAtLimit:
		return "was still running at limits.timeout and the tool stopped it", failed
	}
	// An ending that this build does not know is never a success.
	return "ended in a way the tool does not know", unknown
}

// installCondition returns the condition an install's command gives, if any:
// install-failed when it exited non-zero, gave an answer the tool could not
// read, could not be started, or was stopped at the limit; internal-error when
// its Ending is one this build does not know. A command that succeeded or was
// stopped by a signal gives none (the run reports a signal as interrupted). A
// command that follows the install, such as the reading of the revision, is
// named in the text, so that its failure is not blamed on the install.
func installCondition(r deploy.Result) (verdict.Condition, bool) {
	end, k := classify(r)
	if k == noFailure {
		return verdict.Condition{}, false
	}
	subject := "the install command"
	if r.Name != "" && r.Name != "install" {
		subject = "the command " + r.Name + ", which follows the install,"
	}
	code := verdict.InstallFailed
	if k == unknown {
		code = verdict.InternalError
	}
	return verdict.Condition{Code: code, Text: subject + " " + end}, true
}

// commandCondition returns the condition a transition's command gives, if any:
// command-failed when it exited non-zero or could not be started;
// transition-stalled, with a text that says the tool stopped the command, when
// it ran to the limit; internal-error when it gave an answer the tool could
// not read or its Ending is one this build does not know, because exit 1 means
// a fail verdict only. A command that succeeded or was stopped by a signal
// gives none.
func commandCondition(r deploy.Result) (verdict.Condition, bool) {
	end, k := classify(r)
	if k == noFailure {
		return verdict.Condition{}, false
	}
	code := verdict.CommandFailed
	switch {
	case k == unreadable || k == unknown:
		code = verdict.InternalError
	case r.Ending == deploy.StoppedAtLimit:
		code = verdict.TransitionStalled
	}
	return verdict.Condition{Code: code, Text: "the command " + end}, true
}
