package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"rollproof/internal/config"
	"rollproof/internal/deploy"
	"rollproof/internal/verdict"
)

func TestInstallCondition(t *testing.T) {
	rows := []struct {
		name        string
		in          deploy.Result
		restartOnly bool
		want        verdict.Condition
		ok          bool
	}{
		{"succeeded", deploy.Result{Ending: deploy.Succeeded, ExitCode: 0}, false, verdict.Condition{}, false},
		{"exited non-zero", deploy.Result{Ending: deploy.ExitedNonZero, ExitCode: 1}, false,
			verdict.Condition{Code: verdict.InstallFailed, Text: "the install command exited with code 1"}, true},
		{"not started", deploy.Result{Ending: deploy.NotStarted, ExitCode: -1}, false,
			verdict.Condition{Code: verdict.InstallFailed, Text: "the install command could not be started"}, true},
		{"stopped at the limit", deploy.Result{Ending: deploy.StoppedAtLimit, ExitCode: -1}, false,
			verdict.Condition{Code: verdict.InstallFailed, Text: "the install command was still running at limits.timeout and the tool stopped it"}, true},
		{"stopped by a signal", deploy.Result{Ending: deploy.StoppedBySignal, ExitCode: -1}, false, verdict.Condition{}, false},
		{"the reading of the revision failed", deploy.Result{Name: "status-install", Ending: deploy.ExitedNonZero, ExitCode: 1}, false,
			verdict.Condition{Code: verdict.InstallFailed, Text: "the command status-install, which follows the install, exited with code 1"}, true},
		{"an answer the tool could not read", deploy.Result{Name: "status-install", Ending: deploy.ExitedNonZero, ExitCode: 0}, false,
			verdict.Condition{Code: verdict.InstallFailed, Text: "the command status-install, which follows the install, gave an answer the tool could not read"}, true},
		{"an answer the tool could not read, install", deploy.Result{Name: "install", Ending: deploy.ExitedNonZero, ExitCode: 0}, false,
			verdict.Condition{Code: verdict.InstallFailed, Text: "the install command gave an answer the tool could not read"}, true},
		{"a named command not started", deploy.Result{Name: "status-install", Ending: deploy.NotStarted, ExitCode: -1}, false,
			verdict.Condition{Code: verdict.InstallFailed, Text: "the command status-install, which follows the install, could not be started"}, true},
		{"a named command at the limit", deploy.Result{Name: "status-install", Ending: deploy.StoppedAtLimit, ExitCode: -1}, false,
			verdict.Condition{Code: verdict.InstallFailed, Text: "the command status-install, which follows the install, was still running at limits.timeout and the tool stopped it"}, true},
		{"an ending the tool does not know", deploy.Result{Ending: deploy.Ending("vanished"), ExitCode: -1}, false,
			verdict.Condition{Code: verdict.InternalError, Text: "the install command ended in a way the tool does not know"}, true},
		{"exited non-zero, restart only", deploy.Result{Ending: deploy.ExitedNonZero, ExitCode: 1}, true,
			verdict.Condition{Code: verdict.InstallFailed, Text: "the install of the candidate exited with code 1"}, true},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			got, ok := installCondition(row.in, row.restartOnly)
			if ok != row.ok {
				t.Fatalf("ok = %v, want %v", ok, row.ok)
			}
			if diff := cmp.Diff(row.want, got); diff != "" {
				t.Errorf("condition (-want +got):\n%s", diff)
			}
		})
	}
}

// substitutedWords loads a configuration whose second setup command's first word is the form
// ${env.ROLLPROOF_TOOL}, with that variable set to a planted value, and returns its SetupWords
// and the value.
func substitutedWords(t *testing.T) ([]string, string) {
	t.Helper()
	const value = "S3CRET-TOOL"
	text := `version: 1
setup:
  - [kubectl, apply, -f, prerequisites.yaml]
  - ["${env.ROLLPROOF_TOOL}", apply]
deploy:
  namespace: orders
  helm:
    release: orders
    candidate:
      chart: oci://registry.example/charts/orders
      version: "1.5.0"
probe:
  http:
    url: http://orders.orders.svc/
`
	path := filepath.Join(t.TempDir(), "rollproof.yaml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path, func(name string) (string, bool) {
		if name == "ROLLPROOF_TOOL" {
			return value, true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Setup[1][0] != value {
		t.Fatalf("setup[1][0] = %q, want the substituted value", cfg.Setup[1][0])
	}
	return cfg.SetupWords, value
}

func TestSetupCondition(t *testing.T) {
	kubectl := []string{"kubectl"}
	substituted, value := substitutedWords(t)
	rows := []struct {
		name  string
		in    deploy.Result
		i     int
		words []string
		ok    bool
		code  verdict.Code
		text  string
	}{
		{"succeeded", deploy.Result{Name: "setup", Ending: deploy.Succeeded}, -1, kubectl, false, "", ""},
		{"stopped by a signal", deploy.Result{Name: "setup", Ending: deploy.StoppedBySignal, ExitCode: -1}, 0, kubectl, false, "", ""},
		{"exited non-zero", deploy.Result{Name: "setup", Ending: deploy.ExitedNonZero, ExitCode: 3}, 0, kubectl,
			true, verdict.SetupFailed, "setup[0] (kubectl) exited with code 3"},
		{"killed by a signal", deploy.Result{Name: "setup", Ending: deploy.ExitedNonZero, ExitCode: -1}, 0, kubectl,
			true, verdict.SetupFailed, "setup[0] (kubectl) was killed by a signal"},
		{"not started", deploy.Result{Name: "setup", Ending: deploy.NotStarted, ExitCode: -1}, 0, kubectl,
			true, verdict.SetupFailed, "setup[0] (kubectl) could not be started"},
		{"stopped at the limit", deploy.Result{Name: "setup", Ending: deploy.StoppedAtLimit, ExitCode: -1}, 0, kubectl,
			true, verdict.SetupFailed, "setup[0] (kubectl) was still running at limits.timeout and the tool stopped it"},
		{"an answer the tool could not read", deploy.Result{Name: "setup", Ending: deploy.ExitedNonZero, ExitCode: 0}, 0, kubectl,
			true, verdict.InternalError, "setup[0] (kubectl) gave an answer the tool could not read"},
		{"an ending the tool does not know", deploy.Result{Name: "setup", Ending: deploy.Ending("gone")}, 0, kubectl,
			true, verdict.InternalError, "setup[0] (kubectl) ended in a way the tool does not know"},
		{"a command after the first", deploy.Result{Name: "setup", Ending: deploy.ExitedNonZero, ExitCode: 1}, 2,
			[]string{"sh", "helm", "kubectl"}, true, verdict.SetupFailed, "setup[2] (kubectl) exited with code 1"},
		{"a first word that a form fills", deploy.Result{Name: "setup", Ending: deploy.ExitedNonZero, ExitCode: 1}, 1,
			substituted, true, verdict.SetupFailed, "setup[1] (${env.ROLLPROOF_TOOL}) exited with code 1"},
		{"no word for the index", deploy.Result{Name: "setup", Ending: deploy.ExitedNonZero, ExitCode: 1}, 1,
			kubectl, true, verdict.SetupFailed, "setup[1] exited with code 1"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			got, ok := setupCondition(row.in, row.i, row.words)
			if ok != row.ok {
				t.Fatalf("ok = %v, want %v", ok, row.ok)
			}
			if !row.ok {
				if diff := cmp.Diff(verdict.Condition{}, got); diff != "" {
					t.Errorf("condition (-want +got):\n%s", diff)
				}
				return
			}
			if got.Code != row.code || got.Text != row.text {
				t.Errorf("condition = {%q, %q}, want {%q, %q}", got.Code, got.Text, row.code, row.text)
			}
			if strings.Contains(got.Text, value) {
				t.Errorf("condition text %q quotes the value a form gave", got.Text)
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
