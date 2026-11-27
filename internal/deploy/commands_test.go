//go:build linux

package deploy

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/nzneit/rollproof/internal/config"
	"github.com/nzneit/rollproof/internal/verdict"
)

var _ Deployer = (*CommandsDriver)(nil)

// allThree names the three commands of section 7's example, with a rollback by a script.
func allThree() config.Commands {
	return config.Commands{
		Install:  []string{"kubectl", "apply", "-f", "rendered/previous.yaml"},
		Deploy:   []string{"kubectl", "apply", "-f", "rendered/candidate.yaml"},
		Rollback: []string{"./rollback.sh", "orders"},
	}
}

func TestCommandsCommandLines(t *testing.T) {
	f := newFake()
	drv := NewCommands(CommandsOptions{Commands: allThree(), Timeout: 5 * time.Minute, Runner: f})
	if err := drv.Fetch(t.Context()); err != nil || len(f.calls) != 0 {
		t.Fatalf("Fetch: %v and %d commands, want nil and none", err, len(f.calls))
	}
	for _, step := range []struct {
		name string
		run  func(context.Context) Result
	}{{"install", drv.Install}, {"deploy", drv.Deploy}, {"rollback", drv.Rollback}} {
		if r := step.run(t.Context()); r.Name != step.name || r.Ending != Succeeded {
			t.Fatalf("got %+v, want %s to succeed", r, step.name)
		}
	}
	want := []Command{
		{Name: "install", Args: allThree().Install, Timeout: 5 * time.Minute},
		{Name: "deploy", Args: allThree().Deploy, Timeout: 5 * time.Minute},
		{Name: "rollback", Args: allThree().Rollback, Timeout: 5 * time.Minute},
	}
	if d := cmp.Diff(want, f.calls); d != "" {
		t.Errorf("commands (-want +got):\n%s", d)
	}
}

func TestCommandsTransitions(t *testing.T) {
	installOnly := config.Commands{Install: allThree().Install}
	installAndDeploy := config.Commands{Install: allThree().Install, Deploy: allThree().Deploy}
	for _, tc := range []struct {
		name     string
		commands config.Commands
		want     []verdict.WindowName
	}{
		{"all three", allThree(), []verdict.WindowName{verdict.Deploy, verdict.Rollback}},
		{"install and deploy", installAndDeploy, []verdict.WindowName{verdict.Deploy}},
		{"install only", installOnly, []verdict.WindowName{verdict.Restart}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewCommands(CommandsOptions{Commands: tc.commands, Timeout: time.Minute, Runner: newFake()})
			if diff := cmp.Diff(tc.want, d.Transitions()); diff != "" {
				t.Errorf("Transitions (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCommandsReleases(t *testing.T) {
	for _, tc := range []struct {
		name     string
		commands config.Commands
		want     []Release
	}{
		{"all three", allThree(), []Release{{Role: "install", Program: "kubectl"},
			{Role: "deploy", Program: "kubectl"}, {Role: "rollback", Program: "./rollback.sh"}}},
		{"install only", config.Commands{Install: allThree().Install}, []Release{{Role: "install", Program: "kubectl"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewCommands(CommandsOptions{Commands: tc.commands, Timeout: time.Minute, Runner: newFake()})
			if diff := cmp.Diff(tc.want, d.Releases()); diff != "" {
				t.Errorf("Releases before Fetch (-want +got):\n%s", diff)
			}
			if err := d.Fetch(t.Context()); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, d.Releases()); diff != "" {
				t.Errorf("Releases after Fetch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCommandsEndingsPassThrough(t *testing.T) {
	began, ended := time.Unix(100, 0), time.Unix(400, 0)
	for _, tc := range []struct {
		step   string
		result Result
	}{
		{"install", Result{Name: "install", Ending: ExitedNonZero, ExitCode: 2, Began: began, Ended: ended}},
		{"deploy", Result{Name: "deploy", Ending: StoppedAtLimit, ExitCode: -1, Began: began, Ended: ended}},
		{"deploy", Result{Name: "deploy", Ending: StoppedBySignal, ExitCode: -1, Began: began, Ended: ended}},
		{"rollback", Result{Name: "rollback", Ending: NotStarted, ExitCode: -1, Began: began, Ended: ended}},
	} {
		t.Run(tc.step+" "+string(tc.result.Ending), func(t *testing.T) {
			f := newFake()
			f.results[tc.step] = tc.result
			d := NewCommands(CommandsOptions{Commands: allThree(), Timeout: time.Minute, Runner: f})
			run := map[string]func(context.Context) Result{"install": d.Install, "deploy": d.Deploy, "rollback": d.Rollback}[tc.step]
			if diff := cmp.Diff(tc.result, run(t.Context())); diff != "" {
				t.Errorf("%s (-want +got):\n%s", tc.step, diff)
			}
		})
	}
}

func TestCommandsProgrammingErrors(t *testing.T) {
	d := NewCommands(CommandsOptions{Commands: config.Commands{Install: []string{"true"}}, Timeout: time.Minute, Runner: newFake()})
	mustPanic(t, "deploy command", func() { d.Deploy(t.Context()) })
	mustPanic(t, "rollback command", func() { d.Rollback(t.Context()) })
}

// Section 12: the variables of section 7 are gone, and the command starts in the configuration's folder.
func TestCommandsRunInTheFolderWithoutHelmVariables(t *testing.T) {
	// Every HELM_* variable of the shell that runs the test is dropped first, so that the exact
	// Removed() list below holds whatever that shell exports.
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "HELM_") })
	env = append(env, "HELM_KUBEAPISERVER=https://elsewhere.example:6443", "HELM_DRIVER=sql")
	h := newHarness(t, env)
	d := NewCommands(CommandsOptions{Commands: config.Commands{Install: []string{"sh", "-c", "pwd -P; env"}},
		Timeout: time.Minute, Runner: h.runner})
	if r := d.Install(t.Context()); r.Ending != Succeeded {
		t.Fatalf("install ended %s", r.Ending)
	}
	dir, err := filepath.EvalSymlinks(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(h.read(t, "install"), "\n")
	for _, line := range []string{dir, "KUBECONFIG=" + testKubeconfig} {
		if !slices.Contains(lines, line) {
			t.Errorf("the evidence has no line %q", line)
		}
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "HELM_KUBEAPISERVER=") || strings.HasPrefix(line, "HELM_DRIVER=") {
			t.Errorf("the command saw %q", line)
		}
	}
	if d := cmp.Diff([]string{"HELM_DRIVER", "HELM_KUBEAPISERVER"}, h.runner.Removed()); d != "" {
		t.Errorf("Removed (-want +got):\n%s", d)
	}
}
