package verdict

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestCodesOrderAndVerdict(t *testing.T) {
	want := []struct {
		code Code
		text string
	}{
		{ProbeFailures, "probe-failures"},
		{TransitionStalled, "transition-stalled"},
		{PodUnschedulable, "pod-unschedulable"},
		{CommandFailed, "command-failed"},
		{ConfigRejected, "config-rejected"},
		{RunnerUnsupported, "runner-unsupported"},
		{ClusterFailed, "cluster-failed"},
		{ChartFetchFailed, "chart-fetch-failed"},
		{SetupFailed, "setup-failed"},
		{InstallFailed, "install-failed"},
		{NothingWatched, "nothing-watched"},
		{WorkloadUnsupported, "workload-unsupported"},
		{TargetUnsupported, "target-unsupported"},
		{ProbeNotStarted, "probe-not-started"},
		{ProbeExited, "probe-exited"},
		{ProbeLineInvalid, "probe-line-invalid"},
		{SteadyNotClean, "steady-not-clean"},
		{ProbeSilent, "probe-silent"},
		{NoPodReplaced, "no-pod-replaced"},
		{RestartFailed, "restart-failed"},
		{Interrupted, "interrupted"},
		{InternalError, "internal-error"},
	}
	got := Codes()
	if len(got) != len(want) {
		t.Fatalf("Codes() has %d codes, want %d", len(got), len(want))
	}
	for i, w := range want {
		if string(w.code) != w.text {
			t.Errorf("the constant for %q is %q", w.text, w.code)
		}
		if got[i] != w.code {
			t.Errorf("Codes()[%d] = %q, want %q", i, got[i], w.code)
		}
		wantVerdict := NotVerified
		if i < 4 {
			wantVerdict = Fail
		}
		if v := w.code.Verdict(); v != wantVerdict {
			t.Errorf("%s.Verdict() = %q, want %q", w.code, v, wantVerdict)
		}
	}
	if v := Code("something-new").Verdict(); v != NotVerified {
		t.Errorf("an unknown code gives %q, want %q", v, NotVerified)
	}
	got[0] = "changed"
	if Codes()[0] != ProbeFailures {
		t.Error("changing the slice that Codes returned changed the order")
	}
}

func TestExitCode(t *testing.T) {
	for _, tc := range []struct {
		v    Verdict
		want int
	}{
		{Pass, 0},
		{Fail, 1},
		{NotVerified, 2},
		{Verdict(""), 2},
		{Verdict("PASS"), 2},
	} {
		if got := tc.v.ExitCode(); got != tc.want {
			t.Errorf("Verdict(%q).ExitCode() = %d, want %d", tc.v, got, tc.want)
		}
	}
}

func TestConditionIsAnError(t *testing.T) {
	c := Condition{Code: SetupFailed, Text: "x"}
	if got := c.Error(); got != "setup-failed: x" {
		t.Errorf("Error() = %q, want %q", got, "setup-failed: x")
	}

	exited := Condition{Code: ProbeExited, Text: "exit code 1", At: time.Date(2026, 11, 13, 10, 0, 33, 0, time.UTC)}
	wrapped := fmt.Errorf("follow: %w", fmt.Errorf("pod: %w", exited))
	var found Condition
	if !errors.As(wrapped, &found) || found != exited {
		t.Errorf("errors.As found %+v, want %+v", found, exited)
	}
	if got := AsCondition(wrapped); got != exited {
		t.Errorf("AsCondition(wrapped) = %+v, want %+v", got, exited)
	}

	want := Condition{Code: InternalError, Text: "boom"}
	if got := AsCondition(errors.New("boom")); got != want {
		t.Errorf("AsCondition(boom) = %+v, want %+v", got, want)
	}
}

func TestStrings(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{string(Pass), "pass"},
		{string(Fail), "fail"},
		{string(NotVerified), "not-verified"},
		{string(Steady), "steady"},
		{string(Deploy), "deploy"},
		{string(Rollback), "rollback"},
		{string(Restart), "restart"},
		{string(ResultPass), "pass"},
		{string(ResultFail), "fail"},
		{string(ResultNotVerified), "not-verified"},
		{string(NotRun), "not-run"},
		{string(NotJudged), "not-judged"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}
