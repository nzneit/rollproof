package run

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"rollproof/internal/deploy"
	"rollproof/internal/verdict"
)

func TestInstallCondition(t *testing.T) {
	rows := []struct {
		name string
		in   deploy.Result
		want verdict.Condition
		ok   bool
	}{
		{"succeeded", deploy.Result{Ending: deploy.Succeeded, ExitCode: 0}, verdict.Condition{}, false},
		{"exited non-zero", deploy.Result{Ending: deploy.ExitedNonZero, ExitCode: 1},
			verdict.Condition{Code: verdict.InstallFailed, Text: "the install command exited with code 1"}, true},
		{"not started", deploy.Result{Ending: deploy.NotStarted, ExitCode: -1},
			verdict.Condition{Code: verdict.InstallFailed, Text: "the install command could not be started"}, true},
		{"stopped at the limit", deploy.Result{Ending: deploy.StoppedAtLimit, ExitCode: -1},
			verdict.Condition{Code: verdict.InstallFailed, Text: "the install command was still running at limits.timeout and the tool stopped it"}, true},
		{"stopped by a signal", deploy.Result{Ending: deploy.StoppedBySignal, ExitCode: -1}, verdict.Condition{}, false},
		{"the reading of the revision failed", deploy.Result{Name: "status-install", Ending: deploy.ExitedNonZero, ExitCode: 1},
			verdict.Condition{Code: verdict.InstallFailed, Text: "the command status-install, which follows the install, exited with code 1"}, true},
		{"an answer the tool could not read", deploy.Result{Name: "status-install", Ending: deploy.ExitedNonZero, ExitCode: 0},
			verdict.Condition{Code: verdict.InstallFailed, Text: "the command status-install, which follows the install, gave an answer the tool could not read"}, true},
		{"an answer the tool could not read, install", deploy.Result{Name: "install", Ending: deploy.ExitedNonZero, ExitCode: 0},
			verdict.Condition{Code: verdict.InstallFailed, Text: "the install command gave an answer the tool could not read"}, true},
		{"a named command not started", deploy.Result{Name: "status-install", Ending: deploy.NotStarted, ExitCode: -1},
			verdict.Condition{Code: verdict.InstallFailed, Text: "the command status-install, which follows the install, could not be started"}, true},
		{"a named command at the limit", deploy.Result{Name: "status-install", Ending: deploy.StoppedAtLimit, ExitCode: -1},
			verdict.Condition{Code: verdict.InstallFailed, Text: "the command status-install, which follows the install, was still running at limits.timeout and the tool stopped it"}, true},
		{"an ending the tool does not know", deploy.Result{Ending: deploy.Ending("vanished"), ExitCode: -1},
			verdict.Condition{Code: verdict.InternalError, Text: "the install command ended in a way the tool does not know"}, true},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			got, ok := installCondition(row.in)
			if ok != row.ok {
				t.Fatalf("ok = %v, want %v", ok, row.ok)
			}
			if diff := cmp.Diff(row.want, got); diff != "" {
				t.Errorf("condition (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCommandCondition(t *testing.T) {
	rows := []struct {
		name string
		in   deploy.Result
		want verdict.Condition
		ok   bool
	}{
		{"succeeded", deploy.Result{Ending: deploy.Succeeded, ExitCode: 0}, verdict.Condition{}, false},
		{"exited non-zero", deploy.Result{Ending: deploy.ExitedNonZero, ExitCode: 3},
			verdict.Condition{Code: verdict.CommandFailed, Text: "the command exited with code 3"}, true},
		{"killed by a signal", deploy.Result{Ending: deploy.ExitedNonZero, ExitCode: -1},
			verdict.Condition{Code: verdict.CommandFailed, Text: "the command was killed by a signal"}, true},
		{"an answer the tool could not read", deploy.Result{Ending: deploy.ExitedNonZero, ExitCode: 0},
			verdict.Condition{Code: verdict.InternalError, Text: "the command gave an answer the tool could not read"}, true},
		{"an ending the tool does not know", deploy.Result{Ending: deploy.Ending("vanished"), ExitCode: -1},
			verdict.Condition{Code: verdict.InternalError, Text: "the command ended in a way the tool does not know"}, true},
		{"not started", deploy.Result{Ending: deploy.NotStarted, ExitCode: -1},
			verdict.Condition{Code: verdict.CommandFailed, Text: "the command could not be started"}, true},
		{"stopped at the limit", deploy.Result{Ending: deploy.StoppedAtLimit, ExitCode: -1},
			verdict.Condition{Code: verdict.TransitionStalled, Text: "the command was still running at limits.timeout and the tool stopped it"}, true},
		{"stopped by a signal", deploy.Result{Ending: deploy.StoppedBySignal, ExitCode: -1}, verdict.Condition{}, false},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			got, ok := commandCondition(row.in)
			if ok != row.ok {
				t.Fatalf("ok = %v, want %v", ok, row.ok)
			}
			if diff := cmp.Diff(row.want, got); diff != "" {
				t.Errorf("condition (-want +got):\n%s", diff)
			}
		})
	}
}
