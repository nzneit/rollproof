// Package verdict is the verdict of a run as a pure function of what happened in it, and the
// words every other unit uses for what happened. It holds the judging of every rule of section 10
// and does no I/O; rule 2's window lengths and rule 8's "run the transition to its end" are run's.
// It knows nothing of Kubernetes, Helm or files.
package verdict

import (
	"errors"
	"time"
)

// Verdict is how a run ended. These are the strings of result.json and of the action's output.
type Verdict string

// Verdicts of a run: Pass is "pass", Fail is "fail" and NotVerified is "not-verified".
const (
	Pass        Verdict = "pass"
	Fail        Verdict = "fail"
	NotVerified Verdict = "not-verified"
)

// ExitCode is the exit code of the run command: 0 for Pass, 1 for Fail, and 2 for NotVerified and
// for any other value.
func (v Verdict) ExitCode() int {
	switch v {
	case Pass:
		return 0
	case Fail:
		return 1
	default:
		return 2
	}
}

// Code is a reason code of section 10. There are 22, each a constant named after its string in
// Go's form: ProbeFailures is "probe-failures", InternalError is "internal-error".
type Code string

// Reason codes of section 10, in the order of its list. The first four give Fail; the rest give
// NotVerified.
const (
	ProbeFailures       Code = "probe-failures"
	TransitionStalled   Code = "transition-stalled"
	PodUnschedulable    Code = "pod-unschedulable"
	CommandFailed       Code = "command-failed"
	ConfigRejected      Code = "config-rejected"
	RunnerUnsupported   Code = "runner-unsupported"
	ClusterFailed       Code = "cluster-failed"
	ChartFetchFailed    Code = "chart-fetch-failed"
	SetupFailed         Code = "setup-failed"
	InstallFailed       Code = "install-failed"
	NothingWatched      Code = "nothing-watched"
	WorkloadUnsupported Code = "workload-unsupported"
	TargetUnsupported   Code = "target-unsupported"
	ProbeNotStarted     Code = "probe-not-started"
	ProbeExited         Code = "probe-exited"
	ProbeLineInvalid    Code = "probe-line-invalid"
	SteadyNotClean      Code = "steady-not-clean"
	ProbeSilent         Code = "probe-silent"
	NoPodReplaced       Code = "no-pod-replaced"
	RestartFailed       Code = "restart-failed"
	Interrupted         Code = "interrupted"
	InternalError       Code = "internal-error"
)

// Codes returns every code, in the order of section 10's list of reasons. That order picks the
// deciding reason among several in one window. It returns a new slice on every call, so that no
// caller can change the order.
func Codes() []Code {
	return []Code{
		ProbeFailures,
		TransitionStalled,
		PodUnschedulable,
		CommandFailed,
		ConfigRejected,
		RunnerUnsupported,
		ClusterFailed,
		ChartFetchFailed,
		SetupFailed,
		InstallFailed,
		NothingWatched,
		WorkloadUnsupported,
		TargetUnsupported,
		ProbeNotStarted,
		ProbeExited,
		ProbeLineInvalid,
		SteadyNotClean,
		ProbeSilent,
		NoPodReplaced,
		RestartFailed,
		Interrupted,
		InternalError,
	}
}

// Verdict is the verdict a code stands for: Fail for the first four of Codes, and NotVerified for
// every other code, one this build does not know included.
func (c Code) Verdict() Verdict {
	switch c {
	case ProbeFailures, TransitionStalled, PodUnschedulable, CommandFailed:
		return Fail
	default:
		return NotVerified
	}
}

// WindowName is the name of a window: Steady "steady", Deploy "deploy", Rollback "rollback" or
// Restart "restart".
type WindowName string

// Names of the windows of a run.
const (
	Steady   WindowName = "steady"
	Deploy   WindowName = "deploy"
	Rollback WindowName = "rollback"
	Restart  WindowName = "restart"
)

// Result is how one window ended: ResultPass "pass", ResultFail "fail", ResultNotVerified
// "not-verified", NotRun "not-run" or NotJudged "not-judged".
type Result string

// Results of one window.
const (
	ResultPass        Result = "pass"
	ResultFail        Result = "fail"
	ResultNotVerified Result = "not-verified"
	NotRun            Result = "not-run"
	NotJudged         Result = "not-judged"
)

// Condition is one thing that happened and that a rule weighs. Text says it for people: its first
// line is the short form, and any further lines are detail. At is the moment it happened; it is
// zero when the caller places the condition in a window itself.
type Condition struct {
	Code Code
	Text string
	At   time.Time
}

// Error returns the code, a colon and the text, so that a unit can return a Condition as its error.
func (c Condition) Error() string {
	return string(c.Code) + ": " + c.Text
}

// AsCondition returns the Condition that err is or wraps. An error that holds none becomes a
// Condition with InternalError and the error's text. err must not be nil.
func AsCondition(err error) Condition {
	var c Condition
	if errors.As(err, &c) {
		return c
	}
	return Condition{Code: InternalError, Text: err.Error()}
}

// Observation is one line of a probe that carries ok. Written is when the node's runtime stamped
// the line, T when the check began (equal to Written when the line gave none). N is how many
// passed checks the line stands for, 1 or more, and always 1 on a failure. Class, Ms and Lag are
// as section 6 defines.
type Observation struct {
	Written, T time.Time
	OK         bool
	N          int
	Class      string
	Ms, Lag    float64
}
