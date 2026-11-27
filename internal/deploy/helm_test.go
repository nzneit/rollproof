//go:build linux

package deploy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.yaml.in/yaml/v3"

	"github.com/nzneit/rollproof/internal/config"
	"github.com/nzneit/rollproof/internal/verdict"
)

const (
	kubeconfig  = "/run/rollproof-test/kubeconfig"
	kubeContext = "kind-rollproof-test"
	timeout     = 5 * time.Minute
)

var _ Deployer = (*HelmDriver)(nil)

// helmConfig names a remote previous release with one inline map and a
// candidate chart in the configuration's folder /work/app with a values
// file and an inline map.
func helmConfig() config.Helm {
	return config.Helm{
		Release: "orders",
		Previous: &config.Release{
			Chart: "oci://registry.example/charts/orders", Version: "1.4.2",
			Values: []config.Value{{Inline: map[string]any{"replicas": 2}}},
		},
		Candidate: &config.Release{
			Chart: "/work/app/chart", IsPath: true,
			Values: []config.Value{
				{File: "/work/app/test/values-kind.yaml"},
				{Inline: map[string]any{
					"image": map[string]any{"digest": "sha256:abc"}, "replicas": 3,
					"enabled": true, "args": []any{"--fast", 2}, "port": "8080",
				}},
			},
		},
	}
}

// candidateOnly is helmConfig with no previous release: its candidate is the chart path /work/app/chart.
func candidateOnly() config.Helm {
	h := helmConfig()
	h.Previous = nil
	return h
}

// scripted is a fake that answers a whole run: it pulls the previous chart,
// shows both charts, and gives revision 1 as the install's status.
func scripted() *fakeRunner {
	f := newFake()
	f.effects["pull-previous"] = writeArchive("orders", "1.4.2")
	f.results["show-previous"] = Result{Ending: Succeeded, Stdout: "apiVersion: v2\nname: orders\nversion: 1.4.2\n"}
	f.results["show-candidate"] = Result{Ending: Succeeded, Stdout: "apiVersion: v2\nname: orders\nversion: 1.5.0\ndescription: the candidate\n"}
	f.results["status-install"] = Result{Ending: Succeeded, Stdout: `{"name":"orders","namespace":"orders","version":1,"info":{"status":"deployed"}}`}
	return f
}

func newDriver(t *testing.T, h config.Helm, f *fakeRunner) (*HelmDriver, string) {
	t.Helper()
	runDir := filepath.Join(t.TempDir(), "rollproof-test")
	if err := os.Mkdir(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return NewHelm(HelmOptions{
		Helm: h, Namespace: "orders", Kubeconfig: kubeconfig, Context: kubeContext,
		RunDir: runDir, Dir: "/work/app", Timeout: timeout, Runner: f,
	}), runDir
}

// mustPanic fails the test unless f panics with a message that holds want.
func mustPanic(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		if got := fmt.Sprint(recover()); !strings.Contains(got, want) {
			t.Errorf("panic %q, want one that holds %q", got, want)
		}
	}()
	f()
}

func TestHelmCommandLines(t *testing.T) {
	f := scripted()
	drv, runDir := newDriver(t, helmConfig(), f)
	ctx := context.Background()
	if err := drv.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		name string
		run  func(context.Context) Result
	}{{"install", drv.Install}, {"deploy", drv.Deploy}, {"rollback", drv.Rollback}} {
		if r := step.run(ctx); r.Name != step.name || r.Ending != Succeeded {
			t.Fatalf("got %+v, want %s to succeed", r, step.name)
		}
	}
	archive := filepath.Join(runDir, "charts", "previous", "orders-1.4.2.tgz")
	cl := []string{"--namespace", "orders", "--kubeconfig", kubeconfig, "--kube-context", kubeContext}
	want := []Command{
		{Name: "pull-previous", Args: []string{"helm", "pull", "oci://registry.example/charts/orders", "--version", "1.4.2",
			"--destination", filepath.Join(runDir, "charts", "previous")}, Timeout: timeout},
		{Name: "show-previous", Args: []string{"helm", "show", "chart", archive}, Timeout: timeout},
		{Name: "show-candidate", Args: []string{"helm", "show", "chart", "/work/app/chart"}, Timeout: timeout},
		{Name: "install", Args: slices.Concat([]string{"helm", "upgrade", "--install", "orders", archive}, cl,
			[]string{"--timeout", "5m0s", "--values", filepath.Join(runDir, "values", "previous-1.yaml")}), Timeout: timeout},
		{Name: "status-install", Args: slices.Concat([]string{"helm", "status", "orders"}, cl, []string{"-o", "json"}), Timeout: timeout, MaxStdout: 64 << 20},
		{Name: "deploy", Args: slices.Concat([]string{"helm", "upgrade", "orders", "/work/app/chart", "--reset-values"}, cl,
			[]string{"--timeout", "5m0s", "--values", "/work/app/test/values-kind.yaml", "--values", filepath.Join(runDir, "values", "candidate-2.yaml")}), Timeout: timeout},
		{Name: "rollback", Args: slices.Concat([]string{"helm", "rollback", "orders", "1"}, cl, []string{"--timeout", "5m0s"}), Timeout: timeout},
	}
	if d := cmp.Diff(want, f.calls); d != "" {
		t.Errorf("commands (-want +got):\n%s", d)
	}
	for _, c := range f.calls {
		for _, flag := range []string{"--wait", "--atomic", "--rollback-on-failure", "--create-namespace"} {
			if slices.Contains(c.Args, flag) {
				t.Errorf("%s passes %s", c.Name, flag)
			}
		}
	}
}

func TestHelmBothChartsRemote(t *testing.T) {
	h := helmConfig()
	h.Candidate = &config.Release{Chart: "oci://registry.example/charts/orders", Version: "1.5.0"}
	f := scripted()
	f.effects["pull-candidate"] = writeArchive("orders", "1.5.0")
	drv, runDir := newDriver(t, h, f)
	if err := drv.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	drv.Install(context.Background())
	drv.Deploy(context.Background())
	if d := cmp.Diff([]string{"pull-previous", "show-previous", "pull-candidate", "show-candidate", "install", "status-install", "deploy"}, f.names()); d != "" {
		t.Errorf("names (-want +got):\n%s", d)
	}
	if got, want := f.calls[6].Args[3], filepath.Join(runDir, "charts", "candidate", "orders-1.5.0.tgz"); got != want {
		t.Errorf("deploy names %q, want %q", got, want)
	}
}

func TestHelmInlineValuesKeepTheirTypes(t *testing.T) {
	drv, runDir := newDriver(t, helmConfig(), scripted())
	if err := drv.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(runDir, "values", "candidate-2.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(text, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"image": map[string]any{"digest": "sha256:abc"}, "replicas": 3, "enabled": true, "args": []any{"--fast", 2}, "port": "8080"}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("candidate-2.yaml (-want +got):\n%s", d)
	}
}

func TestHelmFetchFailures(t *testing.T) {
	const output = "PULL-OUTPUT"
	for _, tc := range []struct {
		name  string
		setup func(f *fakeRunner)
		role  string
		chart string
	}{
		{"the pull exits non-zero", func(f *fakeRunner) {
			delete(f.effects, "pull-previous")
			f.results["pull-previous"] = Result{Ending: ExitedNonZero, ExitCode: 1, Stdout: output}
		}, "previous", "oci://registry.example/charts/orders"},
		{"the pull is stopped at the limit", func(f *fakeRunner) {
			delete(f.effects, "pull-previous")
			f.results["pull-previous"] = Result{Ending: StoppedAtLimit, ExitCode: -1, Stdout: output}
		}, "previous", "oci://registry.example/charts/orders"},
		{"the pull leaves no archive", func(f *fakeRunner) { delete(f.effects, "pull-previous") }, "previous", "oci://registry.example/charts/orders"},
		{"show cannot start", func(f *fakeRunner) {
			f.results["show-candidate"] = Result{Ending: NotStarted, ExitCode: -1, Stdout: output}
		}, "candidate", "/work/app/chart"},
		{"show gives no name", func(f *fakeRunner) {
			f.results["show-candidate"] = Result{Ending: Succeeded, Stdout: "version: 1.5.0\n" + output + "\n"}
		}, "candidate", "/work/app/chart"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := scripted()
			tc.setup(f)
			drv, _ := newDriver(t, helmConfig(), f)
			var c verdict.Condition
			if err := drv.Fetch(context.Background()); !errors.As(err, &c) || c.Code != verdict.ChartFetchFailed {
				t.Fatalf("got %v, want a chart-fetch-failed condition", err)
			}
			if !strings.Contains(c.Text, tc.role) || !strings.Contains(c.Text, tc.chart) || strings.Contains(c.Text, output) {
				t.Errorf("text %q, want the role %q and the chart %q and not the output", c.Text, tc.role, tc.chart)
			}
		})
	}
}

func TestHelmFetchStoppedBySignal(t *testing.T) {
	f := scripted()
	delete(f.effects, "pull-previous")
	f.results["pull-previous"] = Result{Ending: StoppedBySignal, ExitCode: -1}
	drv, _ := newDriver(t, helmConfig(), f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := drv.Fetch(ctx)
	var c verdict.Condition
	if !errors.Is(err, context.Canceled) || errors.As(err, &c) {
		t.Errorf("got %v, want the context's error and no condition", err)
	}
}

func TestHelmInstallReadsTheRevision(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status Result
		of     string
		ending Ending
		arg    string
	}{
		{"revision 4", Result{Ending: Succeeded, Stdout: `{"version":4}`}, "install", Succeeded, "4"},
		{"status fails", Result{Ending: ExitedNonZero, ExitCode: 1}, "status-install", ExitedNonZero, ""},
		{"no version", Result{Ending: Succeeded, Stdout: `{"name":"orders"}`}, "status-install", ExitedNonZero, ""},
		{"not JSON", Result{Ending: Succeeded, Stdout: "Error: something"}, "status-install", ExitedNonZero, ""},
		{"status stopped by a signal", Result{Ending: StoppedBySignal, ExitCode: -1}, "status-install", StoppedBySignal, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := scripted()
			f.results["status-install"] = tc.status
			drv, _ := newDriver(t, helmConfig(), f)
			if err := drv.Fetch(context.Background()); err != nil {
				t.Fatal(err)
			}
			if r := drv.Install(context.Background()); r.Name != tc.of || r.Ending != tc.ending {
				t.Fatalf("Install gave the result of %q, ended %s; want %q, %s", r.Name, r.Ending, tc.of, tc.ending)
			}
			if tc.arg == "" {
				return
			}
			drv.Rollback(context.Background())
			if got := f.calls[len(f.calls)-1].Args[3]; got != tc.arg {
				t.Errorf("rollback to %q, want %q", got, tc.arg)
			}
		})
	}
}

func TestHelmEndingsPassThrough(t *testing.T) {
	f := scripted()
	stalled := Result{Name: "deploy", Ending: StoppedAtLimit, ExitCode: -1, Began: time.Unix(100, 0), Ended: time.Unix(400, 0)}
	f.results["deploy"] = stalled
	drv, _ := newDriver(t, helmConfig(), f)
	if err := drv.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	drv.Install(context.Background())
	if d := cmp.Diff(stalled, drv.Deploy(context.Background())); d != "" {
		t.Errorf("Deploy (-want +got):\n%s", d)
	}

	f = scripted()
	failed := Result{Name: "install", Ending: ExitedNonZero, ExitCode: 1}
	f.results["install"] = failed
	drv, _ = newDriver(t, helmConfig(), f)
	if err := drv.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(failed, drv.Install(context.Background())); d != "" {
		t.Errorf("Install (-want +got):\n%s", d)
	}
	if slices.Contains(f.names(), "status-install") {
		t.Error("the status was read after a failed install")
	}
}

func TestHelmTransitionsAndReleases(t *testing.T) {
	h := helmConfig()
	h.Candidate.Values = append(h.Candidate.Values, config.Value{Inline: map[string]any{"image": map[string]any{"tag": "x"}, "zone": "a"}})
	drv, _ := newDriver(t, h, scripted())
	if d := cmp.Diff([]verdict.WindowName{verdict.Deploy, verdict.Rollback}, drv.Transitions()); d != "" {
		t.Errorf("Transitions (-want +got):\n%s", d)
	}
	if err := drv.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []Release{
		{Role: "previous", Chart: "orders", Version: "1.4.2", Source: "oci://registry.example/charts/orders", InlineKeys: []string{"replicas"}},
		{Role: "candidate", Chart: "orders", Version: "1.5.0", Source: "./chart", ValueFiles: []string{"values-kind.yaml"},
			InlineKeys: []string{"args", "enabled", "image", "port", "replicas", "zone"}},
	}
	if d := cmp.Diff(want, drv.Releases()); d != "" {
		t.Errorf("Releases (-want +got):\n%s", d)
	}

	outside := helmConfig()
	outside.Candidate.Chart = "/elsewhere/chart"
	drv, _ = newDriver(t, outside, scripted())
	if err := drv.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := drv.Releases()[1].Source; got != "/elsewhere/chart" {
		t.Errorf("Source of a chart outside the folder %q, want the absolute path", got)
	}
}

func TestHelmWithNoPreviousCommandLines(t *testing.T) {
	f := scripted()
	drv, runDir := newDriver(t, candidateOnly(), f)
	if err := drv.Fetch(t.Context()); err != nil {
		t.Fatal(err)
	}
	if r := drv.Install(t.Context()); r.Name != "install" || r.Ending != Succeeded {
		t.Fatalf("Install gave %+v, want install to succeed", r)
	}
	cl := []string{"--namespace", "orders", "--kubeconfig", kubeconfig, "--kube-context", kubeContext}
	want := []Command{
		{Name: "show-candidate", Args: []string{"helm", "show", "chart", "/work/app/chart"}, Timeout: timeout},
		{Name: "install", Args: slices.Concat([]string{"helm", "upgrade", "--install", "orders", "/work/app/chart"}, cl,
			[]string{"--timeout", "5m0s", "--values", "/work/app/test/values-kind.yaml",
				"--values", filepath.Join(runDir, "values", "candidate-2.yaml")}), Timeout: timeout},
		{Name: "status-install", Args: slices.Concat([]string{"helm", "status", "orders"}, cl, []string{"-o", "json"}),
			Timeout: timeout, MaxStdout: 64 << 20},
	}
	if d := cmp.Diff(want, f.calls); d != "" {
		t.Errorf("commands (-want +got):\n%s", d)
	}
}

func TestHelmWithNoPreviousRemoteCandidate(t *testing.T) {
	h := candidateOnly()
	h.Candidate = &config.Release{Chart: "oci://registry.example/charts/orders", Version: "1.5.0"}
	f := scripted()
	f.effects["pull-candidate"] = writeArchive("orders", "1.5.0")
	drv, runDir := newDriver(t, h, f)
	if err := drv.Fetch(t.Context()); err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff([]string{"pull-candidate", "show-candidate"}, f.names()); d != "" {
		t.Errorf("Fetch's commands (-want +got):\n%s", d)
	}
	drv.Install(t.Context())
	archive := filepath.Join(runDir, "charts", "candidate", "orders-1.5.0.tgz")
	if got := f.calls[2]; got.Name != "install" || got.Args[4] != archive {
		t.Errorf("the install is %s %q, want install of %s", got.Name, got.Args, archive)
	}
}

func TestHelmWithNoPreviousTransitionsAndReleases(t *testing.T) {
	drv, _ := newDriver(t, candidateOnly(), scripted())
	if d := cmp.Diff([]verdict.WindowName{verdict.Restart}, drv.Transitions()); d != "" {
		t.Errorf("Transitions (-want +got):\n%s", d)
	}
	if err := drv.Fetch(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []Release{{Role: "candidate", Chart: "orders", Version: "1.5.0", Source: "./chart",
		ValueFiles: []string{"values-kind.yaml"}, InlineKeys: []string{"args", "enabled", "image", "port", "replicas"}}}
	if d := cmp.Diff(want, drv.Releases()); d != "" {
		t.Errorf("Releases (-want +got):\n%s", d)
	}
}

func TestHelmWithNoPreviousRefusesATransition(t *testing.T) {
	drv, _ := newDriver(t, candidateOnly(), scripted())
	if err := drv.Fetch(t.Context()); err != nil {
		t.Fatal(err)
	}
	drv.Install(t.Context())
	mustPanic(t, "no previous release", func() { drv.Deploy(t.Context()) })
	mustPanic(t, "no previous release", func() { drv.Rollback(t.Context()) })
}

func TestHelmProgrammingErrors(t *testing.T) {
	drv, _ := newDriver(t, helmConfig(), scripted())
	mustPanic(t, "Install", func() { drv.Install(context.Background()) })
	if err := drv.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	mustPanic(t, "Deploy", func() { drv.Deploy(context.Background()) })
	mustPanic(t, "Rollback", func() { drv.Rollback(context.Background()) })
}

func TestCheckHelm(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var c verdict.Condition
	if err := CheckHelm(); !errors.As(err, &c) || c.Code != verdict.RunnerUnsupported || c.Text != "helm is not on the PATH" {
		t.Errorf("got %v, want runner-unsupported: helm is not on the PATH", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "helm"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if err := CheckHelm(); err != nil {
		t.Errorf("got %v with helm on the PATH", err)
	}
}

// TestHelmShowUnreadable: an answer of show that the tool cannot read fails
// closed with the chart-fetch-failed code, whatever way it is unreadable.
func TestHelmShowUnreadable(t *testing.T) {
	for name, out := range map[string]string{
		"no name":              "apiVersion: v2\nversion: 1.5.0\n",
		"no version":           "apiVersion: v2\nname: orders\n",
		"empty":                "",
		"not a mapping":        "Error: something",
		"a list":               "- name: orders\n  version: 1.5.0\n",
		"a name that is a map": "name:\n  a: b\nversion: 1.5.0\n",
	} {
		t.Run(name, func(t *testing.T) {
			f := scripted()
			f.results["show-candidate"] = Result{Ending: Succeeded, Stdout: out}
			drv, _ := newDriver(t, helmConfig(), f)
			var c verdict.Condition
			if err := drv.Fetch(context.Background()); !errors.As(err, &c) || c.Code != verdict.ChartFetchFailed {
				t.Fatalf("got %v, want a chart-fetch-failed condition", err)
			}
		})
	}
}

// TestHelmNoValueIsReported: no value of an inline map or a values file
// reaches a Release, and a fetch error never holds one.
func TestHelmNoValueIsReported(t *testing.T) {
	h := helmConfig()
	h.Candidate.Values = []config.Value{{Inline: map[string]any{"password": "hunter2-secret", "n": 424242}}}
	f := scripted()
	f.results["show-candidate"] = Result{Ending: Succeeded, Stdout: "name: orders\nversion: 1.5.0\n"}
	drv, _ := newDriver(t, h, f)
	if err := drv.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%+v", drv.Releases()); strings.Contains(got, "hunter2") || strings.Contains(got, "424242") {
		t.Errorf("a value reached the releases: %s", got)
	}
	f = scripted()
	f.results["show-candidate"] = Result{Ending: ExitedNonZero, ExitCode: 1}
	drv, _ = newDriver(t, h, f)
	if err := drv.Fetch(context.Background()); err == nil || strings.Contains(err.Error(), "hunter2") || strings.Contains(err.Error(), "424242") {
		t.Errorf("error %v holds a value or is nil", err)
	}
}

// TestHelmWrittenFilesArePrivate: inline values may hold secrets, so the
// file is readable by its owner alone, and so is its folder.
func TestHelmWrittenFilesArePrivate(t *testing.T) {
	drv, runDir := newDriver(t, helmConfig(), scripted())
	if err := drv.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{
		filepath.Join(runDir, "values", "candidate-2.yaml"): 0o600,
		filepath.Join(runDir, "values"):                     0o700,
		filepath.Join(runDir, "charts", "previous"):         0o700,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s has mode %o, want %o", path, got, want)
		}
	}
}

// writeArchives is what a pull does that leaves more than one archive.
func writeArchives(names ...string) func(c Command) error {
	return func(c Command) error {
		for _, n := range names {
			if err := os.WriteFile(filepath.Join(argAfter(c.Args, "--destination"), n), []byte("archive"), 0o644); err != nil {
				return err
			}
		}
		return nil
	}
}

func wantChartFetchFailed(t *testing.T, err error, role string) {
	t.Helper()
	var c verdict.Condition
	if !errors.As(err, &c) || c.Code != verdict.ChartFetchFailed {
		t.Fatalf("got %v, want a chart-fetch-failed condition", err)
	}
	if !strings.Contains(c.Text, role) {
		t.Errorf("text %q does not name the role %q", c.Text, role)
	}
}

// TestHelmFetchFailsClosed: each way a good-looking fetch is wrong gives
// chart-fetch-failed, one check at a time.
func TestHelmFetchFailsClosed(t *testing.T) {
	t.Run("two archives", func(t *testing.T) {
		f := scripted()
		f.effects["pull-previous"] = writeArchives("orders-1.4.2.tgz", "other-1.0.0.tgz")
		drv, _ := newDriver(t, helmConfig(), f)
		wantChartFetchFailed(t, drv.Fetch(context.Background()), "previous")
	})
	t.Run("a pull that failed but left an archive", func(t *testing.T) {
		f := scripted()
		f.results["pull-previous"] = Result{Ending: ExitedNonZero, ExitCode: 1}
		drv, _ := newDriver(t, helmConfig(), f)
		wantChartFetchFailed(t, drv.Fetch(context.Background()), "previous")
	})
	t.Run("a show that failed but gave a good answer", func(t *testing.T) {
		f := scripted()
		f.results["show-candidate"] = Result{Ending: ExitedNonZero, ExitCode: 1, Stdout: "name: orders\nversion: 1.5.0\n"}
		drv, _ := newDriver(t, helmConfig(), f)
		wantChartFetchFailed(t, drv.Fetch(context.Background()), "candidate")
	})
	t.Run("a context that ends during show", func(t *testing.T) {
		f := scripted()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f.effects["show-candidate"] = func(Command) error { cancel(); return nil }
		f.results["show-candidate"] = Result{Ending: Succeeded, Stdout: "name: orders\nversion: 1.5.0\n"}
		drv, _ := newDriver(t, helmConfig(), f)
		var c verdict.Condition
		if err := drv.Fetch(ctx); !errors.Is(err, context.Canceled) || errors.As(err, &c) {
			t.Errorf("got %v, want the context's error and no condition", err)
		}
	})
}

func TestHelmStatusVersionZero(t *testing.T) {
	f := scripted()
	f.results["status-install"] = Result{Ending: Succeeded, Stdout: `{"version":0}`}
	drv, _ := newDriver(t, helmConfig(), f)
	if err := drv.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r := drv.Install(context.Background()); r.Name != "status-install" || r.Ending != ExitedNonZero || r.ExitCode != 0 {
		t.Errorf("got %+v, want the status as exit-nonzero with code 0", r)
	}
}

func TestHelmSourceOfAPathChart(t *testing.T) {
	for chart, want := range map[string]string{
		"/work/app":         ".",
		"/work/app/chart":   "./chart",
		"/work":             "/work",
		"/work/x":           "/work/x",
		"/work/application": "/work/application",
	} {
		h := helmConfig()
		h.Candidate.Chart = chart
		drv, _ := newDriver(t, h, scripted())
		if err := drv.Fetch(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := drv.Releases()[1].Source; got != want {
			t.Errorf("Source of %q is %q, want %q", chart, got, want)
		}
	}
}

func TestHelmRunFolderWithAPatternCharacter(t *testing.T) {
	f := scripted()
	runDir := filepath.Join(t.TempDir(), "rollproof-run[1]")
	drv := NewHelm(HelmOptions{Helm: helmConfig(), Namespace: "orders", Kubeconfig: kubeconfig, Context: kubeContext,
		RunDir: runDir, Dir: "/work/app", Timeout: timeout, Runner: f})
	if err := drv.Fetch(context.Background()); err != nil {
		t.Fatalf("a good pull into a folder with a [ in its path: %v", err)
	}
}

// TestHelmFetchNeedsItsOwnRunFolder: Fetch removes <RunDir>/charts/<role>, so it refuses a folder
// whose name is not the run's own, before it removes or writes anything.
func TestHelmFetchNeedsItsOwnRunFolder(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "charts", "previous")
	if err := os.MkdirAll(keep, 0o700); err != nil {
		t.Fatal(err)
	}
	f := scripted()
	drv := NewHelm(HelmOptions{Helm: helmConfig(), Namespace: "orders", Kubeconfig: kubeconfig, Context: kubeContext,
		RunDir: dir, Dir: "/work/app", Timeout: timeout, Runner: f})
	var c verdict.Condition
	if err := drv.Fetch(context.Background()); !errors.As(err, &c) || c.Code != verdict.InternalError ||
		c.Text != "the run folder's name must begin with rollproof-" {
		t.Fatalf("got %v, want an internal-error condition with the run folder's name", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("%d commands ran, want none", len(f.calls))
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("the folder %s was removed: %v", keep, err)
	}
}

func TestHelmSecondFetch(t *testing.T) {
	f := scripted()
	drv, runDir := newDriver(t, helmConfig(), f)
	if err := drv.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.effects["pull-previous"] = writeArchive("orders", "1.4.3")
	if err := drv.Fetch(context.Background()); err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	drv.Install(context.Background())
	want := filepath.Join(runDir, "charts", "previous", "orders-1.4.3.tgz")
	for _, c := range f.calls {
		if c.Name == "install" && c.Args[4] != want {
			t.Errorf("install names %q, want the second pull's archive %q", c.Args[4], want)
		}
	}
}

func TestHelmFetchNeedsAnAbsoluteRunFolder(t *testing.T) {
	for _, runDir := range []string{"", "run"} {
		t.Run("run folder "+strconv.Quote(runDir), func(t *testing.T) {
			t.Chdir(t.TempDir())
			keep := filepath.Join(runDir, "charts", "previous")
			if err := os.MkdirAll(keep, 0o700); err != nil {
				t.Fatal(err)
			}
			f := scripted()
			drv := NewHelm(HelmOptions{Helm: helmConfig(), Namespace: "orders", Kubeconfig: kubeconfig, Context: kubeContext,
				RunDir: runDir, Dir: "/work/app", Timeout: timeout, Runner: f})
			var c verdict.Condition
			if err := drv.Fetch(context.Background()); !errors.As(err, &c) || c.Code != verdict.InternalError ||
				c.Text != "the run folder must be an absolute path" {
				t.Fatalf("got %v, want an internal-error condition with the run folder's text", err)
			}
			if len(f.calls) != 0 {
				t.Errorf("%d commands ran, want none", len(f.calls))
			}
			if _, err := os.Stat(keep); err != nil {
				t.Errorf("the folder %s was removed: %v", keep, err)
			}
		})
	}
}
