package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"rollproof/internal/verdict"
)

// planted is a value that no rejection text may quote.
const planted = "S3CRET-VALUE"

const helmBlock = `  helm:
    release: orders
    previous:
      chart: oci://registry.example/charts/orders
      version: "1.4.2"
    candidate:
      chart: oci://registry.example/charts/orders
      version: "1.5.0"
`

const httpProbe = `probe:
  http:
    url: http://orders.orders.svc/api/S3CRET-VALUE
    headers:
      X-Planted: S3CRET-VALUE
    rate: 200
    timeout: 2s
    expect-status: 200-299
`

// base is a valid run description. Each table row changes it in one place.
const base = "version: 1\ncluster:\n  workers: 3\ndeploy:\n  namespace: orders\n" + helmBlock + httpProbe +
	"limits:\n  steady: 10s\n  settle: 15s\n  timeout: 5m\n  silence: 5s\n"

var (
	digestA = "sha256:" + strings.Repeat("a", 64)
	digestB = "sha256:" + strings.Repeat("b", 64)
	digestC = "sha256:" + strings.Repeat("c", 64)
	digestD = "sha256:" + strings.Repeat("d", 64)
)

func noEnv(string) (string, bool) { return "", false }

// variant returns text with old replaced by new. old must occur in text exactly once.
func variant(t *testing.T, text, old, new string) string {
	t.Helper()
	if n := strings.Count(text, old); n != 1 {
		t.Fatalf("%q occurs %d times, want once", old, n)
	}
	return strings.Replace(text, old, new, 1)
}

// loadWith writes text to dir/rollproof.yaml and loads that file.
func loadWith(t *testing.T, dir, text string, env func(string) (string, bool)) (*Config, error) {
	t.Helper()
	path := filepath.Join(dir, "rollproof.yaml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load(path, env)
}

func loadText(t *testing.T, text string) (*Config, error) {
	t.Helper()
	return loadWith(t, t.TempDir(), text, noEnv)
}

// rejected fails the test unless err is a ConfigRejected condition whose
// text holds want and does not hold the planted value.
func rejected(t *testing.T, err error, want string) {
	t.Helper()
	var c verdict.Condition
	if !errors.As(err, &c) {
		t.Fatalf("got error %v, want a verdict.Condition", err)
	}
	if c.Code != verdict.ConfigRejected {
		t.Errorf("code %q, want %q", c.Code, verdict.ConfigRejected)
	}
	if !strings.Contains(c.Text, want) {
		t.Errorf("text %q does not hold %q", c.Text, want)
	}
	if strings.Contains(c.Text, planted) {
		t.Errorf("text %q quotes a value", c.Text)
	}
}

// realPath is path made absolute, with every symbolic link resolved.
func realPath(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// fromTestdata copies testdata/name into a new folder that also holds an
// empty folder named chart, as a run description sits beside its chart, and
// returns the copy's path.
func fromTestdata(t *testing.T, name string) string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "chart"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollproof.yaml")
	if err := os.WriteFile(path, text, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// customProbe is a probe block with a custom probe, followed by env lines.
func customProbe(env string) string {
	return "probe:\n  custom:\n    image: registry.example/tools/busybox@" + digestD +
		"\n    command: [sh, -c, \"echo S3CRET-VALUE\"]\n" + env
}

func TestLoadFull(t *testing.T) {
	path := filepath.Join("testdata", "full.yaml")
	got, err := Load(path, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	real := realPath(t, path)
	want := &Config{
		Path: real,
		Dir:  filepath.Dir(real),
		Cluster: Cluster{
			NodeImage:     "docker.io/kindest/node:v1.37.0@" + digestA,
			RegistryImage: "docker.io/library/registry:3@" + digestB,
			Workers:       4,
		},
		Registries: []Registry{{
			Host:   "registry.example",
			CA:     "/etc/rollproof/corp-ca.pem",
			Mirror: "mirror.corp.example/registry-example",
		}},
		Images: map[string]Image{"orders": {Local: "orders:candidate", As: "registry.example/team/orders"}},
		Setup:  [][]string{{"kubectl", "apply", "-f", "test/prerequisites.yaml"}},
		Deploy: Deploy{
			Namespace: "orders",
			Selector:  "app=orders",
			Helm: &Helm{
				Release:  "orders",
				Previous: &Release{Chart: "oci://registry.example/charts/orders", Version: "1.4.2"},
				Candidate: &Release{
					Chart:   "oci://registry.example/charts/orders",
					Version: "1.5.0",
					Values: []Value{
						{File: "/etc/rollproof/values-kind.yaml"},
						{Inline: map[string]any{"image": map[string]any{"digest": digestC}, "replicas": 3}},
					},
				},
			},
		},
		Probe: Probe{HTTP: &HTTP{
			URL:       "http://orders.orders.svc/api/orders/latest",
			Headers:   map[string]string{"Host": "orders.example"},
			Rate:      100,
			Timeout:   time.Second,
			ExpectMin: 200,
			ExpectMax: 204,
		}},
		Limits: Limits{Steady: 20 * time.Second, Settle: 30 * time.Second, Timeout: 10 * time.Minute, Silence: 6 * time.Second},
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("Load(full.yaml) mismatch (-want +got):\n%s", d)
	}
}

func TestLoadMinimal(t *testing.T) {
	c, err := Load(fromTestdata(t, "minimal.yaml"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(Cluster{Workers: 3}, c.Cluster); d != "" {
		t.Errorf("cluster (-want +got):\n%s", d)
	}
	if c.Probe.HTTP == nil {
		t.Fatal("probe.http is not set")
	}
	wantHTTP := HTTP{URL: "http://orders.orders.svc/api/orders/latest", Rate: 200, Timeout: 2 * time.Second, ExpectMin: 200, ExpectMax: 299}
	if d := cmp.Diff(wantHTTP, *c.Probe.HTTP); d != "" {
		t.Errorf("probe.http (-want +got):\n%s", d)
	}
	wantLimits := Limits{Steady: 10 * time.Second, Settle: 15 * time.Second, Timeout: 5 * time.Minute, Silence: 5 * time.Second}
	if d := cmp.Diff(wantLimits, c.Limits); d != "" {
		t.Errorf("limits (-want +got):\n%s", d)
	}
	if c.Registries != nil || c.Images != nil || c.Setup != nil || c.Probe.Custom != nil || c.Deploy.Commands != nil {
		t.Errorf("keys the file does not have are set: %+v", c)
	}
	if h := c.Deploy.Helm; h == nil || h.Release != "orders" || h.Previous != nil || h.Candidate == nil {
		t.Errorf("deploy.helm is %+v", h)
	}
}

func TestLoadThroughALink(t *testing.T) {
	realDir := t.TempDir()
	target := filepath.Join(realDir, "rollproof.yaml")
	if err := os.WriteFile(target, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	c, err := Load(link, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if c.Path != realPath(t, target) || c.Dir != realPath(t, realDir) {
		t.Errorf("Path %q, Dir %q; want the real file and its folder", c.Path, c.Dir)
	}

	t.Chdir(realDir)
	c, err = Load("rollproof.yaml", noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if c.Path != realPath(t, target) {
		t.Errorf("Path %q from a relative path, want %q", c.Path, realPath(t, target))
	}
}

func TestLoadAccepts(t *testing.T) {
	limitsTimeout := func(c *Config) any { return c.Limits.Timeout }
	expect := func(c *Config) any { return [2]int{c.Probe.HTTP.ExpectMin, c.Probe.HTTP.ExpectMax} }
	envOK := "    env:\n      - name: MODE\n        value: fast\n" +
		"      - name: TOKEN\n        valueFrom: { secretKeyRef: { name: probe-token, key: token } }\n"
	for _, tc := range []struct {
		name, old, new string
		get            func(c *Config) any
		want           any
	}{
		{"a duration in ms", "  timeout: 5m\n", "  timeout: 500ms\n", limitsTimeout, 500 * time.Millisecond},
		{"a duration in s", "  timeout: 5m\n", "  timeout: 10s\n", limitsTimeout, 10 * time.Second},
		{"a duration in m", "  timeout: 5m\n", "  timeout: 7m\n", limitsTimeout, 7 * time.Minute},
		{"one status code", "    expect-status: 200-299\n", "    expect-status: 200\n", expect, [2]int{200, 200}},
		{"a status range", "    expect-status: 200-299\n", "    expect-status: 200-204\n", expect, [2]int{200, 204}},
		{"a chart version in quotes", "      version: \"1.4.2\"\n", "      version: \"1.10\"\n",
			func(c *Config) any { return c.Deploy.Helm.Previous.Version }, "1.10"},
		{"one worker", "  workers: 3\n", "  workers: 1\n", func(c *Config) any { return c.Cluster.Workers }, 1},
		{"rate 1", "    rate: 200\n", "    rate: 1\n", func(c *Config) any { return c.Probe.HTTP.Rate }, 1},
		{"custom env entries", httpProbe, customProbe(envOK), func(c *Config) any { return c.Probe.Custom.Env }, []EnvVar{
			{Name: "MODE", Value: "fast"},
			{Name: "TOKEN", ValueFrom: map[string]any{"secretKeyRef": map[string]any{"name": "probe-token", "key": "token"}}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := loadText(t, variant(t, base, tc.old, tc.new))
			if err != nil {
				t.Fatal(err)
			}
			if d := cmp.Diff(tc.want, tc.get(c)); d != "" {
				t.Errorf("(-want +got):\n%s", d)
			}
		})
	}
}

func TestLoadRejects(t *testing.T) {
	const limitsTimeout, expectStatus, workers = "  timeout: 5m\n", "    expect-status: 200-299\n", "  workers: 3\n"
	const previousVersion, headers = "      version: \"1.4.2\"\n", "    headers:\n      X-Planted: S3CRET-VALUE\n"
	for _, tc := range []struct{ name, old, new, want string }{
		{"an unknown key at the top", "version: 1\n", "version: 1\nbogus: S3CRET-VALUE\n", "bogus"},
		{"an unknown key in cluster", workers, workers + "  nodes: S3CRET-VALUE\n", "cluster.nodes"},
		{"an unknown key in a release", "      version: \"1.5.0\"\n", "      version: \"1.5.0\"\n      values-file: S3CRET-VALUE\n", "deploy.helm.candidate.values-file"},
		{"an unknown key in probe.http", "    rate: 200\n", "    rate: 200\n    method: S3CRET-VALUE\n", "probe.http.method"},
		{"an unknown key in limits", "  silence: 5s\n", "  silence: 5s\n  drain: S3CRET-VALUE\n", "limits.drain"},
		{"no version", "version: 1\n", "", "version"},
		{"version 2", "version: 1\n", "version: 2\n", "version"},
		{"version as a string", "version: 1\n", "version: \"1\"\n", "version"},
		{"a duration in hours", limitsTimeout, "  timeout: 1h\n", "limits.timeout"},
		{"a duration with a fraction", limitsTimeout, "  timeout: 1.5s\n", "limits.timeout"},
		{"a duration of two units", limitsTimeout, "  timeout: 1m30s\n", "limits.timeout"},
		{"a duration with no unit", limitsTimeout, "  timeout: 10\n", "limits.timeout"},
		{"a duration with a space", limitsTimeout, "  timeout: \"10 s\"\n", "limits.timeout"},
		{"a negative duration", limitsTimeout, "  timeout: -5s\n", "limits.timeout"},
		{"a zero duration", limitsTimeout, "  timeout: 0s\n", "limits.timeout"},
		{"a duration that is a word", limitsTimeout, "  timeout: S3CRET-VALUE\n", "limits.timeout"},
		{"a reversed status range", expectStatus, "    expect-status: 299-200\n", "probe.http.expect-status"},
		{"status 99", expectStatus, "    expect-status: 99\n", "probe.http.expect-status"},
		{"status 600", expectStatus, "    expect-status: 600\n", "probe.http.expect-status"},
		{"status 2xx", expectStatus, "    expect-status: 2xx\n", "probe.http.expect-status"},
		{"a status with three parts", expectStatus, "    expect-status: 200-299-300\n", "probe.http.expect-status"},
		{"a status that is a word", expectStatus, "    expect-status: S3CRET-VALUE\n", "probe.http.expect-status"},
		{"chart version 1.10 as a number", previousVersion, "      version: 1.10\n", "write the version in quotes"},
		{"chart version 2 as a number", previousVersion, "      version: 2\n", "deploy.helm.previous.version"},
		{"an empty chart version", previousVersion, "      version: \"\"\n", "deploy.helm.previous.version"},
		{"a flow map, a long bad value after a string", "cluster:\n  workers: 3\n", "cluster: {node-image: img, workers: S3CRET-VALUE}\n", "cluster.workers"},
		{"a flow map, a short bad value after a string", "cluster:\n  workers: 3\n", "cluster: {node-image: img, workers: nope}\n", "cluster.workers"},
		{"a nested flow map, a bad value after a string", httpProbe, "probe: {http: {url: u, rate: S3CRET-VALUE}}\n", "probe.http.rate"},
		{"no worker", workers, "  workers: 0\n", "cluster.workers"},
		{"workers -1", workers, "  workers: -1\n", "cluster.workers"},
		{"workers that is a word", workers, "  workers: S3CRET-VALUE\n", "cluster.workers"},
		{"rate 0", "    rate: 200\n", "    rate: 0\n", "probe.http.rate"},
		{"headers as a list", headers, "    headers:\n      - S3CRET-VALUE\n", "probe.http.headers"},
		{"a header with a map", headers, "    headers:\n      X-Planted:\n        inner: S3CRET-VALUE\n", "probe.http.headers"},
		{"custom env as a map", httpProbe, customProbe("    env:\n      TOKEN: S3CRET-VALUE\n"), "probe.custom.env"},
		{"a custom env entry with no name", httpProbe, customProbe("    env:\n      - value: S3CRET-VALUE\n"), "probe.custom.env[0]"},
		{"a custom env entry with value and valueFrom", httpProbe, customProbe("    env:\n      - name: TOKEN\n        value: S3CRET-VALUE\n" +
			"        valueFrom: { secretKeyRef: { name: probe-token, key: token } }\n"), "probe.custom.env[0]"},
		{"a custom env entry with neither", httpProbe, customProbe("    env:\n      - name: TOKEN\n"), "probe.custom.env[0]"},
		{"a username with no password", "deploy:\n", "registries:\n  - host: registry.example\n    username: S3CRET-VALUE\ndeploy:\n", "registries[0]"},
		{"a password with no username", "deploy:\n", "registries:\n  - host: registry.example\n    password: S3CRET-VALUE\ndeploy:\n", "registries[0]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadText(t, variant(t, base, tc.old, tc.new))
			rejected(t, err, tc.want)
		})
	}
}

func TestRejectionTexts(t *testing.T) {
	for _, tc := range []struct{ old, new, want string }{
		{"  workers: 3\n", "  workers: 3\n  nodes: S3CRET-VALUE\n", "cluster.nodes (line 4): unknown key"},
		{"  workers: 3\n", "  workers: S3CRET-VALUE\n", "cluster.workers (line 3): must be a whole number"},
		{"  workers: 3\n", "  workers: 0\n", "cluster.workers (line 3): must be 1 or more"},
		{"      version: \"1.4.2\"\n", "      version: 1.10\n", "deploy.helm.previous.version (line 10): YAML reads this as a number; write the version in quotes"},
		{"  timeout: 5m\n", "  timeout: 1h\n", "limits.timeout (line 25): must be a whole number followed by ms, s or m, more than zero"},
		{"version: 1\n", "", "version: required; this tool reads version 1"},
	} {
		_, err := loadText(t, variant(t, base, tc.old, tc.new))
		var c verdict.Condition
		if !errors.As(err, &c) || c.Text != tc.want {
			t.Errorf("got %v, want the text %q", err, tc.want)
		}
	}
}

func TestLoadFileErrors(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "absent.yaml")
	_, err := Load(absent, noEnv)
	rejected(t, err, absent)

	for _, tc := range []struct{ name, text string }{
		{"not YAML", "version: 1\nprobe: [S3CRET-VALUE\n"},
		{"empty", ""},
		{"only a comment", "# S3CRET-VALUE\n"},
		{"not a map", "- S3CRET-VALUE\n"},
		{"two documents", "version: 1\n---\nbogus: S3CRET-VALUE\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rollproof.yaml")
			if err := os.WriteFile(path, []byte(tc.text), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path, noEnv)
			rejected(t, err, path)
		})
	}
}
