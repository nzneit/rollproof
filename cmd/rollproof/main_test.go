package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/client-go/kubernetes/fake"

	"rollproof/internal/cluster"
	"rollproof/internal/config"
	"rollproof/internal/deploy"
	"rollproof/internal/probe"
	"rollproof/internal/run"
	"rollproof/internal/verdict"
	"rollproof/internal/watch"
)

func noEnv(string) string { return "" }

// lockedBuffer is a bytes.Buffer that several goroutines may write to. first
// is closed by the first write.
type lockedBuffer struct {
	mu    sync.Mutex
	b     bytes.Buffer
	first chan struct{}
	once  sync.Once
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.first != nil {
		l.once.Do(func() { close(l.first) })
	}
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// readResult reads the reasons of OUT/result.json.
func readResult(t *testing.T, out string) (final bool, codes, texts []string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Final   bool `json:"final"`
		Reasons []struct {
			Code string `json:"code"`
			Text string `json:"text"`
		} `json:"reasons"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	for _, x := range r.Reasons {
		codes = append(codes, x.Code)
		texts = append(texts, x.Text)
	}
	return r.Final, codes, texts
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"nope"},
		{"run", "--nope"},
		{"run", "-f", "rollproof.yaml", "extra"},
		{"probe"},
		{"probe", "tcp"},
		{"version", "extra"},
	} {
		var stdout, stderr bytes.Buffer
		if code := realMain(args, &stdout, &stderr, noEnv); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
		if !strings.Contains(stderr.String(), "usage: rollproof run") {
			t.Errorf("%q: no usage text on stderr:\n%s", args, stderr.String())
		}
		if n := strings.Count(stderr.String(), "\n"); n > 10 {
			t.Errorf("%q: %d lines on stderr, want at most 10", args, n)
		}
	}
}

// TestUsageTextIsExact pins the whole usage text, line by line and indent,
// against a literal written here rather than the command's own constant.
func TestUsageTextIsExact(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := realMain(nil, &stdout, &stderr, noEnv); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	want := `usage: rollproof run [-f FILE] [--out DIR] [--name NAME] [--keep-cluster]
       rollproof version
       rollproof probe http --url URL --rate N --timeout D --expect-status RANGE [--header NAME=VALUE]...
`
	if got := stderr.String(); got != want {
		t.Errorf("stderr is\n%s\nwant\n%s", got, want)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout holds %q, want nothing", stdout.String())
	}
}

func TestVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := realMain([]string{"version"}, &stdout, &stderr, noEnv); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	prefixes := []string{"rollproof ", "kind ", "node image ", "registry image "}
	if len(lines) != len(prefixes) {
		t.Fatalf("%d lines, want %d:\n%s", len(lines), len(prefixes), stdout.String())
	}
	for i, p := range prefixes {
		if !strings.HasPrefix(lines[i], p) {
			t.Errorf("line %d is %q, want the prefix %q", i+1, lines[i], p)
		}
	}
	if !strings.Contains(lines[2], "@sha256:") {
		t.Errorf("the node image has no digest: %q", lines[2])
	}
}

func TestRunWithoutAFile(t *testing.T) {
	for _, flag := range []string{"-out", "--out"} {
		out := t.TempDir()
		var stdout, stderr bytes.Buffer
		if code := realMain([]string{"run", "-f", "does-not-exist.yaml", flag, out}, &stdout, &stderr, noEnv); code != 2 {
			t.Fatalf("%s: exit %d, want 2", flag, code)
		}
		final, codes, _ := readResult(t, out)
		if !final || len(codes) != 1 || codes[0] != "config-rejected" {
			t.Errorf("%s: final %v, reasons %q; want a final file with config-rejected", flag, final, codes)
		}
	}
}

// TestRunWithKeepCluster passes -keep-cluster and --keep-cluster to a run whose configuration
// stops before Check: either spelling is taken, and the run writes its final result.
func TestRunWithKeepCluster(t *testing.T) {
	for _, flag := range []string{"-keep-cluster", "--keep-cluster"} {
		out := t.TempDir()
		var stdout, stderr bytes.Buffer
		if code := realMain([]string{"run", "-f", "does-not-exist.yaml", flag, "--out", out}, &stdout, &stderr, noEnv); code != 2 {
			t.Fatalf("%s: exit %d, want 2\n%s", flag, code, stderr.String())
		}
		if strings.Contains(stderr.String(), "usage:") {
			t.Errorf("%s: the flag was refused:\n%s", flag, stderr.String())
		}
		final, codes, _ := readResult(t, out)
		if !final || len(codes) != 1 || codes[0] != "config-rejected" {
			t.Errorf("%s: final %v, reasons %q; want a final file with config-rejected", flag, final, codes)
		}
	}
}

// TestRunWithoutAFileFlag leaves -f out, in an empty folder, and pins that the
// reason names rollproof.yaml: the default, and nothing else.
func TestRunWithoutAFileFlag(t *testing.T) {
	t.Chdir(t.TempDir())
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := realMain([]string{"run", "--out", out}, &stdout, &stderr, noEnv); code != 2 {
		t.Fatalf("exit %d, want 2\n%s", code, stderr.String())
	}
	final, codes, texts := readResult(t, out)
	if !final || len(codes) != 1 || codes[0] != "config-rejected" {
		t.Fatalf("final %v, reasons %q, want a final file with one config-rejected", final, codes)
	}
	if !strings.Contains(texts[0], "rollproof.yaml") {
		t.Errorf("the reason is %q, want it to name rollproof.yaml", texts[0])
	}
}

func TestFailedClientError(t *testing.T) {
	clientErr := errors.New("no route to the API server")
	reached := "the cluster's API server cannot be reached: no route to the API server"
	kept := "the cluster is kept: kubeconfig /tmp/rollproof-x/kubeconfig, context kind-rollproof-x"
	left := reached + ". Parts of the run may be left behind; the next run with this name removes them."
	cleanUp := errors.New("the engine is gone")
	noLogs := "; the cluster's logs were not all collected: no nodes"
	for _, tc := range []struct {
		name         string
		keep         bool
		cancelled    bool
		cancelDuring string // "collect" or "delete": that call cancels the run's context and returns its error
		collectErr   error
		deleteErr    error
		wantCollect  int
		wantCalls    int
		wantOrder    []string
		wantText     string
		wantJoined   string
	}{
		{"kept", true, false, "", nil, cleanUp, 1, 0, []string{"collect"},
			reached + ". The cluster is kept: kubeconfig /tmp/rollproof-x/kubeconfig, context kind-rollproof-x", ""},
		{"the run's context has ended", false, true, "", nil, cleanUp, 0, 0, nil, reached, ""},
		{"the run's context has ended, and the cluster is kept", true, true, "", nil, cleanUp, 0, 0, nil,
			reached + ". The cluster is kept: kubeconfig /tmp/rollproof-x/kubeconfig, context kind-rollproof-x", ""},
		{"the delete succeeds", false, false, "", nil, nil, 1, 1, []string{"collect", "delete"}, reached, ""},
		{"the delete fails", false, false, "", nil, cleanUp, 1, 1, []string{"collect", "delete"}, left,
			"the clean-up after it failed: the engine is gone"},
		{"the logs cannot be collected", false, false, "", errors.New("no nodes"), nil, 1, 1,
			[]string{"collect", "delete"}, reached + noLogs, ""},
		{"the logs cannot be collected, and the cluster is kept", true, false, "", errors.New("no nodes"), nil, 1, 0,
			[]string{"collect"},
			reached + noLogs + ". The cluster is kept: kubeconfig /tmp/rollproof-x/kubeconfig, context kind-rollproof-x", ""},
		{"a signal during the collection", false, false, "collect", nil, nil, 1, 0, []string{"collect"},
			reached + "; the cluster's logs were not all collected: context canceled", ""},
		{"a signal during the delete", false, false, "delete", nil, nil, 1, 1, []string{"collect", "delete"}, left,
			"the clean-up after it failed: context canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelled {
				cancel()
			}
			var order []string
			collectCalls := 0
			collect := func(context.Context) error {
				order = append(order, "collect")
				collectCalls++
				if tc.cancelDuring == "collect" {
					cancel()
					return ctx.Err()
				}
				return tc.collectErr
			}
			calls := 0
			del := func(context.Context) error {
				order = append(order, "delete")
				calls++
				if tc.cancelDuring == "delete" {
					cancel()
					return ctx.Err()
				}
				return tc.deleteErr
			}
			err := failedClientError(ctx, tc.keep, kept, collect, del, clientErr)
			if collectCalls != tc.wantCollect {
				t.Errorf("the collection was called %d times, want %d", collectCalls, tc.wantCollect)
			}
			if calls != tc.wantCalls {
				t.Errorf("the delete was called %d times, want %d", calls, tc.wantCalls)
			}
			if diff := cmp.Diff(tc.wantOrder, order); diff != "" {
				t.Errorf("the calls (-want +got):\n%s", diff)
			}
			if err == nil {
				t.Fatal("no error")
			}
			got := verdict.AsCondition(err)
			if got.Code != verdict.ClusterFailed || got.Text != tc.wantText {
				t.Errorf("condition %s %q, want cluster-failed %q", got.Code, got.Text, tc.wantText)
			}
			if tc.wantJoined != "" && !strings.Contains(err.Error(), tc.wantJoined) {
				t.Errorf("the error is %q, want it to hold %q", err.Error(), tc.wantJoined)
			}
		})
	}
}

// TestStoreFailedTakesTheLogs pins that images.Store's error takes the cluster's logs for a
// cluster-failed error while the run's context has not ended, and only then; a collection's error
// adds its sentence to the condition's text.
func TestStoreFailedTakesTheLogs(t *testing.T) {
	failed := verdict.Condition{Code: verdict.ClusterFailed, Text: "the probe's image: boom"}
	internal := verdict.Condition{Code: verdict.InternalError, Text: "boom"}
	noLogs := "; the cluster's logs were not all collected: no nodes"
	for _, tc := range []struct {
		name         string
		err          error
		cancelled    bool
		cancelDuring bool // the collection cancels the run's context and returns its error
		collectErr   error
		wantCalls    int
		wantErr      error  // the error that must come back unchanged
		wantText     string // the condition's text, when the error is replaced
	}{
		{"a cluster-failed error", failed, false, false, nil, 1, failed, ""},
		{"the logs cannot be collected", failed, false, false, errors.New("no nodes"), 1, nil, failed.Text + noLogs},
		{"an internal-error error", internal, false, false, nil, 0, internal, ""},
		{"the run's context has ended", failed, true, false, nil, 0, failed, ""},
		{name: "a signal during the collection", err: failed, cancelDuring: true, wantCalls: 1,
			wantText: failed.Text + "; the cluster's logs were not all collected: context canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelled {
				cancel()
			}
			calls := 0
			collect := func(context.Context) error {
				calls++
				if tc.cancelDuring {
					cancel()
					return ctx.Err()
				}
				return tc.collectErr
			}
			got := storeFailed(ctx, tc.err, collect)
			if calls != tc.wantCalls {
				t.Errorf("the collection was called %d times, want %d", calls, tc.wantCalls)
			}
			if tc.wantErr != nil {
				if got != tc.wantErr {
					t.Errorf("storeFailed = %v, want the error unchanged", got)
				}
				return
			}
			c := verdict.AsCondition(got)
			if c.Code != verdict.ClusterFailed || c.Text != tc.wantText {
				t.Errorf("condition %s %q, want cluster-failed %q", c.Code, c.Text, tc.wantText)
			}
		})
	}
}

func TestEndCommands(t *testing.T) {
	t.Run("without Deployer", func(t *testing.T) {
		var s session
		s.EndCommands() // must not panic
	})
	t.Run("after Deployer", func(t *testing.T) {
		dir := t.TempDir()
		cfg := &config.Config{
			Dir:    dir,
			Deploy: config.Deploy{Helm: &config.Helm{Previous: &config.Release{}}},
		}
		ev := &recordingEvidence{}
		s := &session{
			c:        &cluster.Cluster{Kubeconfig: filepath.Join(dir, "kubeconfig")},
			cfg:      cfg,
			evidence: func(string) (io.WriteCloser, error) { return ev, nil },
		}
		if _, _, err := s.Deployer(cfg); err != nil {
			t.Fatal(err)
		}

		pidFile := filepath.Join(dir, "pid")
		// A failing EndCommands leaves the TERM-ignoring command running: kill its
		// group so that the failure does not outlive the test. ESRCH when it is gone.
		t.Cleanup(func() {
			data, err := os.ReadFile(pidFile)
			if err != nil {
				return // the command never started
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				return
			}
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ran := make(chan deploy.Result, 1)
		go func() {
			ran <- s.runner.Run(ctx, deploy.Command{Name: "c", Args: []string{"sh", "-c",
				`echo $$ > "$0"; trap "" TERM; while :; do sleep 0.05; done`, pidFile}})
		}()
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(pidFile); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("the command did not start")
			}
			time.Sleep(10 * time.Millisecond)
		}
		cancel()

		var res deploy.Result
		select {
		case res = <-ran:
		case <-time.After(5 * time.Second):
			t.Fatal("the runner did not return after its context ended")
		}
		if res.Ending != deploy.StoppedBySignal {
			t.Fatalf("ending %s, want stopped-by-signal", res.Ending)
		}

		s.EndCommands()
		if !ev.isClosed() {
			t.Error("EndCommands did not close the runner's evidence")
		}
	})
}

// TestDeployerChoosesTheDriver pins that Deployer gives the driver that the configuration names:
// the commands driver for deploy.commands, and the Helm driver for deploy.helm, also with no
// previous release.
func TestDeployerChoosesTheDriver(t *testing.T) {
	restart := []verdict.WindowName{verdict.Restart}
	for _, row := range []struct {
		name   string
		deploy config.Deploy
		want   string
	}{
		{"commands", config.Deploy{Namespace: "orders",
			Commands: &config.Commands{Install: []string{"kubectl", "apply", "-k", "x"}}}, "*deploy.CommandsDriver"},
		{"helm with no previous", config.Deploy{Namespace: "orders",
			Helm: &config.Helm{Release: "orders", Candidate: &config.Release{Chart: "./chart", IsPath: true}}}, "*deploy.HelmDriver"},
	} {
		t.Run(row.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := &config.Config{Dir: dir, Deploy: row.deploy, Limits: config.Limits{Timeout: time.Minute}}
			s := &session{
				c:        &cluster.Cluster{Kubeconfig: filepath.Join(dir, "kubeconfig")},
				cfg:      cfg,
				evidence: func(string) (io.WriteCloser, error) { return &recordingEvidence{}, nil },
			}
			d, _, err := s.Deployer(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer s.EndCommands()
			if got := fmt.Sprintf("%T", d); got != row.want {
				t.Errorf("Deployer gave %s, want %s", got, row.want)
			}
			if diff := cmp.Diff(restart, d.Transitions()); diff != "" {
				t.Errorf("transitions (-want +got):\n%s", diff)
			}
		})
	}
}

// TestSessionSetup pins the evidence names the setup commands run under and that
// the first command that fails ends the set-up, with its index.
func TestSessionSetup(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Dir:    dir,
		Setup:  [][]string{{"sh", "-c", "exit 0"}, {"sh", "-c", "exit 3"}, {"sh", "-c", "exit 0"}},
		Deploy: config.Deploy{Helm: &config.Helm{Previous: &config.Release{}}},
		Limits: config.Limits{Timeout: time.Minute},
	}
	var names []string
	s := &session{
		c:   &cluster.Cluster{Kubeconfig: filepath.Join(dir, "kubeconfig")},
		cfg: cfg,
		evidence: func(name string) (io.WriteCloser, error) {
			names = append(names, name)
			return &recordingEvidence{}, nil
		},
	}
	if _, _, err := s.Deployer(cfg); err != nil {
		t.Fatal(err)
	}
	res, i := s.Setup(context.Background())
	if res.Ending != deploy.ExitedNonZero || res.ExitCode != 3 || i != 1 {
		t.Errorf("setup ended %s with code %d at index %d, want exit-nonzero with code 3 at index 1", res.Ending, res.ExitCode, i)
	}
	if diff := cmp.Diff([]string{"commands/setup", "commands/setup-2"}, names); diff != "" {
		t.Errorf("evidence names (-want +got):\n%s", diff)
	}
}

// customConfig is a configuration whose probe is a custom one, with a command, an argument and an
// env entry that takes its value from a Secret.
func customConfig() *config.Config {
	return &config.Config{Limits: config.Limits{Timeout: time.Minute}, Probe: config.Probe{Custom: &config.Custom{
		Image:   "registry.example/tools/busybox@sha256:" + strings.Repeat("0", 64),
		Command: []string{"sh", "-c", "loop"}, Args: []string{"a"},
		Env: []config.EnvVar{{Name: "TOKEN", ValueFrom: map[string]any{"secretKeyRef": map[string]any{"name": "probe-token", "key": "token"}}}}}}}
}

// httpConfig is a configuration whose probe is the built-in one, with a request timeout of 2 s.
func httpConfig() *config.Config {
	return &config.Config{Limits: config.Limits{Timeout: time.Minute}, Probe: config.Probe{HTTP: &config.HTTP{
		URL: "http://orders.orders.svc/", Rate: 200, Timeout: 2 * time.Second, ExpectMin: 200, ExpectMax: 299}}}
}

// badEnvConfig is a custom probe's configuration whose env entry a pod cannot hold.
func badEnvConfig() *config.Config {
	cfg := customConfig()
	cfg.Probe.Custom.Env = []config.EnvVar{{Name: "TOKEN", ValueFrom: map[string]any{"secretKeyRef": "S3CRET-VALUE"}}}
	return cfg
}

// envRejected fails the test unless err is a config-rejected condition about probe.custom.env
// that does not quote the planted value.
func envRejected(t *testing.T, err error) {
	t.Helper()
	var c verdict.Condition
	if !errors.As(err, &c) {
		t.Fatalf("error %v, want a verdict.Condition", err)
	}
	if c.Code != verdict.ConfigRejected || !strings.HasPrefix(c.Text, "probe.custom.env") || strings.Contains(c.Text, "S3CRET-VALUE") {
		t.Errorf("condition %s %q, want config-rejected about probe.custom.env with no value", c.Code, c.Text)
	}
}

func TestPodOptions(t *testing.T) {
	const probeImage = "rollproof.test/rollproof@sha256:fff"
	custom := customConfig()
	o, err := podOptions(custom, probeImage)
	if err != nil {
		t.Fatal(err)
	}
	if o.Image != custom.Probe.Custom.Image {
		t.Errorf("image %q, want the custom image", o.Image)
	}
	if diff := cmp.Diff([]string{"sh", "-c", "loop"}, o.Command); diff != "" {
		t.Errorf("command (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"a"}, o.Args); diff != "" {
		t.Errorf("args (-want +got):\n%s", diff)
	}
	if len(o.Env) != 1 || o.Env[0].ValueFrom == nil || o.Env[0].ValueFrom.SecretKeyRef == nil ||
		o.Env[0].ValueFrom.SecretKeyRef.Name != "probe-token" {
		t.Errorf("env %+v, want TOKEN from the Secret probe-token", o.Env)
	}
	if !o.ServiceAccountToken || o.Drain != 10*time.Second {
		t.Errorf("token %v, drain %s; want the default token and 10s", o.ServiceAccountToken, o.Drain)
	}

	builtIn := httpConfig()
	o, err = podOptions(builtIn, probeImage)
	if err != nil {
		t.Fatal(err)
	}
	if o.Image != probeImage || o.Command != nil || o.Env != nil {
		t.Errorf("image %q, command %q, env %v; want the probe image and no command or env", o.Image, o.Command, o.Env)
	}
	if diff := cmp.Diff(probe.HTTPArgs(httpOptions(builtIn.Probe.HTTP)), o.Args); diff != "" {
		t.Errorf("args (-want +got):\n%s", diff)
	}
	if o.ServiceAccountToken || o.Drain != 7*time.Second {
		t.Errorf("token %v, drain %s; want no token and 7s", o.ServiceAccountToken, o.Drain)
	}

	_, err = podOptions(badEnvConfig(), probeImage)
	envRejected(t, err)

	// Both probes name the mirror of an image's host as the configuration does (decision 12).
	image := "registry.example/x@sha256:" + strings.Repeat("0", 64)
	for name, cfg := range map[string]*config.Config{"custom": customConfig(), "built-in": httpConfig()} {
		cfg.Registries = []config.Registry{{Host: "registry.example", Mirror: "mirror.corp.example/r"}}
		o, err := podOptions(cfg, probeImage)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := cfg.MirrorNote(image)
		if want == "" {
			t.Fatalf("%s: the configuration names no mirror for %s", name, image)
		}
		if o.MirrorNote == nil {
			t.Errorf("%s: MirrorNote is nil, want the configuration's", name)
			continue
		}
		if got := o.MirrorNote(image); got != want {
			t.Errorf("%s: MirrorNote gives %q, want %q", name, got, want)
		}
	}
}

// clusterSpec gives Create the fields of the configuration that it needs, the cluster's full name
// and its folder, keep, the evidence opener, the configuration's registries, and the hosts that an
// entry of images names in as.
func TestClusterSpec(t *testing.T) {
	ev := func(string) (io.WriteCloser, error) { return nil, nil }
	cfg := &config.Config{
		Cluster: config.Cluster{NodeImage: "kindest/node:v1.37.0@sha256:" + strings.Repeat("d", 64),
			RegistryImage: "registry:3@sha256:" + strings.Repeat("e", 64), Workers: 2},
		Deploy: config.Deploy{Namespace: "orders"},
		Limits: config.Limits{Timeout: 3 * time.Minute},
		Registries: []config.Registry{
			{Host: "registry.example", Username: "ci", Password: "s3cret-pass", Mirror: "mirror.corp.example/r"},
			{Host: "docker.io", CA: "/etc/rollproof/corp-ca.pem"},
		},
		Images: map[string]config.Image{
			"orders": {Local: "orders:candidate", As: "registry.example/team/orders"},
		},
	}
	s := clusterSpec(cfg, "rollproof-x", true, ev)
	want := cluster.Spec{
		Name:          "rollproof-x",
		RunDir:        cluster.RunDir("rollproof-x"),
		NodeImage:     cfg.Cluster.NodeImage,
		RegistryImage: cfg.Cluster.RegistryImage,
		Workers:       2,
		Namespace:     "orders",
		Timeout:       3 * time.Minute,
		Keep:          true,
		Registries:    cfg.Registries,
		AsHosts:       cfg.AsHosts(),
	}
	if diff := cmp.Diff(want, s, cmpopts.IgnoreFields(cluster.Spec{}, "Evidence")); diff != "" {
		t.Errorf("Spec (-want +got):\n%s", diff)
	}
	if reflect.ValueOf(s.Evidence).Pointer() != reflect.ValueOf(ev).Pointer() {
		t.Error("Evidence is not the opener that create was given")
	}
}

// The watcher's options carry the configuration's fields, the session's evidence and the probe's
// namespace, and name the mirror of an image's host as the configuration does (decision 12).
func TestWatchOptions(t *testing.T) {
	ev := func(string) (io.WriteCloser, error) { return nil, nil }
	cfg := &config.Config{
		Cluster:    config.Cluster{Workers: 2},
		Deploy:     config.Deploy{Namespace: "orders", Selector: "app=orders"},
		Limits:     config.Limits{Settle: 15 * time.Second, Timeout: 3 * time.Minute},
		Registries: []config.Registry{{Host: "registry.example", Mirror: "mirror.corp.example/r"}},
	}
	s := &session{client: fake.NewClientset(), cfg: cfg, evidence: ev}
	o := s.watchOptions(cfg)
	want := watch.Options{Namespace: "orders", Selector: "app=orders", Settle: 15 * time.Second,
		Timeout: 3 * time.Minute, Workers: 2, ProbeNamespace: "rollproof-probe"}
	if diff := cmp.Diff(want, o, cmpopts.IgnoreFields(watch.Options{}, "Now", "Evidence", "MirrorNote")); diff != "" {
		t.Errorf("Options (-want +got):\n%s", diff)
	}
	if o.Now == nil {
		t.Error("Now is nil, want the clock")
	}
	if reflect.ValueOf(o.Evidence).Pointer() != reflect.ValueOf(ev).Pointer() {
		t.Error("Evidence is not the session's opener")
	}
	image := "registry.example/x@sha256:" + strings.Repeat("0", 64)
	note := cfg.MirrorNote(image)
	if note == "" {
		t.Fatalf("the configuration names no mirror for %s", image)
	}
	if o.MirrorNote == nil {
		t.Fatal("MirrorNote is nil, want the configuration's")
	}
	if got := o.MirrorNote(image); got != note {
		t.Errorf("MirrorNote gives %q, want %q", got, note)
	}
}

// A custom env that a pod cannot hold is rejected by check, before a cluster
// exists (README "Interfaces between tasks").
func TestCheckRejectsACustomEnv(t *testing.T) {
	envRejected(t, (&wiring{}).check(context.Background(), badEnvConfig()))
}

// Both probes write their pod's evidence into the run's evidence folder under
// the names that probe gives, and read the API with the session's client
// (Task 10's review).
func TestProbeOptionsCarryTheSession(t *testing.T) {
	for _, row := range []struct {
		name string
		cfg  *config.Config
	}{{"built-in", httpConfig()}, {"custom", customConfig()}} {
		t.Run(row.name, func(t *testing.T) {
			var names []string
			s := &session{client: fake.NewClientset(), cfg: row.cfg, evidence: func(name string) (io.WriteCloser, error) {
				names = append(names, name)
				return &recordingEvidence{}, nil
			}}
			raw := &bytes.Buffer{}
			o, err := s.probeOptions(row.cfg, "rollproof.test/rollproof@sha256:fff", raw)
			if err != nil {
				t.Fatal(err)
			}
			if o.Client != s.client || o.Raw != raw || o.StartTimeout != time.Minute || o.Now == nil {
				t.Errorf("client %v, raw %v, start timeout %s, now set %v; want the session's, the run's and a minute",
					o.Client, o.Raw, o.StartTimeout, o.Now != nil)
			}
			if o.Evidence == nil {
				t.Fatal("no Evidence")
			}
			if _, err := o.Evidence("probe-pod.json"); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff([]string{"probe-pod.json"}, names); diff != "" {
				t.Errorf("evidence names (-want +got):\n%s", diff)
			}
		})
	}
}

// recordingEvidence is an evidence writer that records that it was closed.
type recordingEvidence struct {
	mu     sync.Mutex
	closed bool
}

func (e *recordingEvidence) Write(p []byte) (int, error) { return len(p), nil }

func (e *recordingEvidence) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	return nil
}

func (e *recordingEvidence) isClosed() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closed
}

func TestProbeHTTP(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := realMain([]string{"probe", "http", "--url", "http://127.0.0.1:1/", "--rate", "0"}, &stdout, &stderr, noEnv); code != 2 {
		t.Errorf("rate 0: exit %d, want 2", code)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	old := probeContext
	probeContext = func() (context.Context, context.CancelFunc) { return ctx, cancel }
	t.Cleanup(func() { probeContext = old })

	out := &lockedBuffer{first: make(chan struct{})}
	var errOut bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- realMain([]string{"probe", "http", "--url", srv.URL + "/", "--rate", "20", "--timeout", "2s",
			"--expect-status", "200-299"}, out, &errOut, noEnv)
	}()
	select {
	case <-out.first:
	case <-time.After(5 * time.Second):
		t.Fatal("probe http wrote no line before the deadline")
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d, want 0\n%s", code, errOut.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("probe http did not return within 5 s of the cancel")
	}
	text := strings.TrimSuffix(out.String(), "\n")
	if text == "" {
		t.Fatal("probe http wrote no line")
	}
	written := time.Now()
	for _, line := range strings.Split(text, "\n") {
		if got := probe.ParseLine(line, written, time.Time{}); got.Kind != probe.Observed {
			t.Errorf("line %q is not an observation", line)
		}
	}
}

// TestProbeHTTPErrorExitsOne pins the probe's exit code and stderr line when
// RunHTTP fails. %zz is not a URL, so RunHTTP rejects it before it resolves
// anything: the test reaches no network.
func TestProbeHTTPErrorExitsOne(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := realMain([]string{"probe", "http", "--url", "%zz", "--rate", "1", "--timeout", "1s",
		"--expect-status", "200-299"}, &stdout, &stderr, noEnv)
	if code != 1 {
		t.Fatalf("exit %d, want 1\n%s", code, stderr.String())
	}
	if !strings.HasPrefix(stderr.String(), "rollproof: ") {
		t.Errorf("stderr is %q, want it to start with %q", stderr.String(), "rollproof: ")
	}
	if !strings.Contains(stderr.String(), "the probe's URL does not parse") {
		t.Errorf("stderr is %q, want the run's error text", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout holds %q, want nothing", stdout.String())
	}
}

// The fake runs of the helper child, which ROLLPROOF_HELPER names.
const (
	fakeWaits   = "waits"   // prints started, waits until its context ends, prints returned and returns 2
	fakeLingers = "lingers" // prints started, prints cancelled when its context ends, waits 30 s and prints returned
	fakeFails   = "fails"   // prints fakeFailsLine and returns 1 at once
)

// fakeFailsLine is the line that the fails fake prints before it returns 1, so
// that its exit can be told from a child whose test framework failed.
const fakeFailsLine = "the fake run returns 1"

// sigintIgnored is whether this test process started with SIGINT ignored, as a
// background job of a script does. A child inherits that. It is read as the
// package starts: once a test has called signal.Notify for SIGINT,
// signal.Ignored reports false, though signal.Stop has made SIGINT ignored again.
var sigintIgnored = signal.Ignored(syscall.SIGINT)

// skipIfSIGINTIgnored skips a test that sends SIGINT to a child that would
// inherit SIGINT ignored. The tool then ends by raise's fallback, exit 130
// after 2 s, and not by the signal.
func skipIfSIGINTIgnored(t *testing.T) {
	t.Helper()
	if sigintIgnored {
		t.Skip("SIGINT was ignored as this test process started: the child inherits it ignored, " +
			"so the tool ends by the fallback exit 130, not by SIGINT")
	}
}

// TestHelperProcess is the child that the signal tests start. Without
// ROLLPROOF_HELPER it does nothing. With it, it runs `rollproof run` with the
// fake run that the variable names, and ends the process as main does.
func TestHelperProcess(t *testing.T) {
	fake := os.Getenv("ROLLPROOF_HELPER")
	if fake == "" {
		return
	}
	runOf = func(ctx context.Context, o run.Options, _ run.Units) int {
		// A child that its test left behind ends on its own.
		orphaned := time.After(time.Minute)
		switch fake {
		case fakeWaits:
			fmt.Fprintln(o.Stdout, "started")
			select {
			case <-ctx.Done():
			case <-orphaned:
				return 3
			}
			fmt.Fprintln(o.Stdout, "returned")
			return 2
		case fakeLingers:
			fmt.Fprintln(o.Stdout, "started")
			select {
			case <-ctx.Done():
			case <-orphaned:
				return 3
			}
			fmt.Fprintln(o.Stdout, "cancelled")
			time.Sleep(30 * time.Second)
			fmt.Fprintln(o.Stdout, "returned")
			return 2
		case fakeFails:
			fmt.Fprintln(o.Stdout, fakeFailsLine)
			return 1
		}
		fmt.Fprintf(o.Stderr, "no fake run is named %q\n", fake)
		return 3
	}
	endProcess(realMain([]string{"run", "-f", "rollproof.yaml"}, os.Stdout, os.Stderr, os.Getenv))
}

// helperChild is this test binary, started to run TestHelperProcess with a
// fake run.
type helperChild struct {
	cmd    *exec.Cmd
	stdout lockedBuffer
	stderr lockedBuffer
	exited chan struct{} // closed when Wait has returned and the output is complete
}

// startChild starts the child with the fake run that fake names. The child is
// killed at the end of the test if it still runs.
func startChild(t *testing.T, fake string) *helperChild {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := &helperChild{exited: make(chan struct{})}
	c.cmd = exec.Command(exe, "-test.run=^TestHelperProcess$")
	c.cmd.Env = append(os.Environ(), "ROLLPROOF_HELPER="+fake)
	c.cmd.Stdout = &c.stdout
	c.cmd.Stderr = &c.stderr
	if err := c.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = c.cmd.Wait()
		close(c.exited)
	}()
	t.Cleanup(func() {
		select {
		case <-c.exited:
			return
		default:
		}
		_ = c.cmd.Process.Kill()
		select {
		case <-c.exited:
		case <-time.After(5 * time.Second):
			t.Error("the child did not end within 5 s of its kill")
		}
	})
	return c
}

// output gives the child's stdout and stderr, for a failure's message.
func (c *helperChild) output() string {
	return "\nstdout:\n" + c.stdout.String() + "stderr:\n" + c.stderr.String()
}

// waitFor waits up to 20 s until the child has written line on stdout.
func (c *helperChild) waitFor(t *testing.T, line string) {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for !strings.Contains(c.stdout.String(), line+"\n") {
		select {
		case <-c.exited:
			if strings.Contains(c.stdout.String(), line+"\n") {
				return
			}
			t.Fatalf("the child ended before it wrote %q%s", line, c.output())
		case <-deadline:
			t.Fatalf("the child did not write %q within 20 s%s", line, c.output())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// signal sends sig to the child's process.
func (c *helperChild) signal(t *testing.T, sig syscall.Signal) {
	t.Helper()
	if err := c.cmd.Process.Signal(sig); err != nil {
		t.Fatalf("sending %s: %v", sig, err)
	}
}

// wait waits up to within for the child to end, and gives how it ended. A race
// that the race detector found in the child fails the test too, since a child
// that a signal ends exits with no code to say so.
func (c *helperChild) wait(t *testing.T, within time.Duration) syscall.WaitStatus {
	t.Helper()
	select {
	case <-c.exited:
	case <-time.After(within):
		t.Fatalf("the child did not end within %s%s", within, c.output())
	}
	if strings.Contains(c.stderr.String(), "DATA RACE") {
		t.Errorf("the race detector found a race in the child%s", c.output())
	}
	return c.cmd.ProcessState.Sys().(syscall.WaitStatus)
}

// ending says how a child ended, for a failure's message.
func ending(ws syscall.WaitStatus) string {
	if ws.Signaled() {
		return "by " + ws.Signal().String()
	}
	return "with exit " + strconv.Itoa(ws.ExitStatus())
}

// TestASignalEndsTheToolByIt sends SIGINT or SIGTERM while the run waits for
// its context: the run returns, and the process then ends by that signal, as a
// shell expects of a program that a user interrupts (section 10, "On a signal").
func TestASignalEndsTheToolByIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		sig  syscall.Signal
	}{
		{"SIGINT", syscall.SIGINT},
		{"SIGTERM", syscall.SIGTERM},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.sig == syscall.SIGINT {
				skipIfSIGINTIgnored(t)
			}
			c := startChild(t, fakeWaits)
			c.waitFor(t, "started")
			c.signal(t, tc.sig)
			ws := c.wait(t, 10*time.Second)
			if !ws.Signaled() || ws.Signal() != tc.sig {
				t.Errorf("the child ended %s, want by %s%s", ending(ws), tc.sig, c.output())
			}
			if !strings.Contains(c.stdout.String(), "returned\n") {
				t.Errorf("the process ended before the run returned%s", c.output())
			}
		})
	}
}

// TestASecondSignalEndsTheToolAtOnce sends a second signal while the run is
// still ending after the first: the process ends by a signal at once, and the
// run's own end never comes. The back-to-back case sends SIGINT and then
// SIGTERM, since Linux may merge a second SIGINT into a first one that is still
// pending. It accepts an end by either: which of two pending signals the tool
// sees first is not the test's to choose.
func TestASecondSignalEndsTheToolAtOnce(t *testing.T) {
	skipIfSIGINTIgnored(t)
	t.Run("after the run's context ended", func(t *testing.T) {
		c := startChild(t, fakeLingers)
		c.waitFor(t, "started")
		c.signal(t, syscall.SIGINT)
		c.waitFor(t, "cancelled")
		c.signal(t, syscall.SIGINT)
		ws := c.wait(t, 5*time.Second)
		if !ws.Signaled() || ws.Signal() != syscall.SIGINT {
			t.Errorf("the child ended %s, want by %s%s", ending(ws), syscall.SIGINT, c.output())
		}
		if strings.Contains(c.stdout.String(), "returned") {
			t.Errorf("the run returned before the process ended%s", c.output())
		}
	})
	t.Run("back to back", func(t *testing.T) {
		c := startChild(t, fakeLingers)
		c.waitFor(t, "started")
		c.signal(t, syscall.SIGINT)
		c.signal(t, syscall.SIGTERM)
		ws := c.wait(t, 5*time.Second)
		if !ws.Signaled() || (ws.Signal() != syscall.SIGINT && ws.Signal() != syscall.SIGTERM) {
			t.Errorf("the child ended %s, want by %s or %s%s", ending(ws), syscall.SIGINT, syscall.SIGTERM, c.output())
		}
		if strings.Contains(c.stdout.String(), "returned") {
			t.Errorf("the run returned before the process ended%s", c.output())
		}
	})
}

// TestNoSignalExitsWithTheCode pins that a run that no signal stopped ends the
// process with the run's exit code. The fake's line and the absence of the test
// framework's failure texts tell that exit from a child whose test failed.
func TestNoSignalExitsWithTheCode(t *testing.T) {
	c := startChild(t, fakeFails)
	ws := c.wait(t, 20*time.Second)
	if ws.Signaled() || ws.ExitStatus() != 1 {
		t.Errorf("the child ended %s, want with exit 1%s", ending(ws), c.output())
	}
	if !strings.Contains(c.stdout.String(), fakeFailsLine+"\n") {
		t.Errorf("the child did not write %q%s", fakeFailsLine, c.output())
	}
	if out := c.stdout.String() + c.stderr.String(); strings.Contains(out, "FAIL") || strings.Contains(out, "panic:") {
		t.Errorf("the child's test framework reported a failure%s", c.output())
	}
}
