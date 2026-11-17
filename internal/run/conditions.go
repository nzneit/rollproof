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

const unknownEnding = "ended in a way the tool does not know"

// ending says in words how a command that failed ended, for the end of a
// sentence about it. ok is false for an ending that is no failure: success,
// and a stop that a signal caused, which the run reports as interrupted.
func ending(r deploy.Result) (text string, ok bool) {
	switch r.Ending {
	case deploy.Succeeded, deploy.StoppedBySignal:
		return "", false
	case deploy.ExitedNonZero:
		if r.ExitCode == 0 {
			// A reading that exits 0 with an answer the tool cannot read ends here.
			return "gave an answer the tool could not read", true
		}
		return fmt.Sprintf("exited with code %d", r.ExitCode), true
	case deploy.NotStarted:
		return "could not be started", true
	case deploy.StoppedAtLimit:
		return "was still running at limits.timeout and the tool stopped it", true
	}
	// An ending that this build does not know is never a success; the callers
	// give it internal-error, because it is the tool's own fault, not a fail.
	return unknownEnding, true
}

// installCondition returns the condition an install's command gives, if any:
// install-failed when it exited non-zero, could not be started, or was stopped
// at the limit. A command that follows the install, such as the reading of the
// revision, is named in the text, so that its failure is not blamed on the
// install.
func installCondition(r deploy.Result) (verdict.Condition, bool) {
	end, ok := ending(r)
	if !ok {
		return verdict.Condition{}, false
	}
	subject := "the install command"
	if r.Name != "" && r.Name != "install" {
		subject = "the command " + r.Name + ", which follows the install,"
	}
	code := verdict.InstallFailed
	if end == unknownEnding {
		code = verdict.InternalError
	}
	return verdict.Condition{Code: code, Text: subject + " " + end}, true
}

// commandCondition returns the condition a transition's command gives, if any:
// command-failed when it exited non-zero or could not be started;
// transition-stalled, with a text that says the tool stopped the command, when
// it ran to the limit.
func commandCondition(r deploy.Result) (verdict.Condition, bool) {
	end, ok := ending(r)
	if !ok {
		return verdict.Condition{}, false
	}
	code := verdict.CommandFailed
	switch {
	case r.Ending == deploy.StoppedAtLimit:
		code = verdict.TransitionStalled
	case end == unknownEnding:
		code = verdict.InternalError
	}
	return verdict.Condition{Code: code, Text: "the command " + end}, true
}
