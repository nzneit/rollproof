> Part of the plan in `README.md` of this folder. Read that file first: its Global Constraints apply to every task here.

### Task 17: `config`: the schema, strict reading, defaults and types

**Files:**
- Create: `internal/config/config.go`, `internal/config/load.go`
- Test: `internal/config/load_test.go`, `internal/config/testdata/full.yaml`, `internal/config/testdata/minimal.yaml`

**Interfaces:**
- Consumes: `type Condition struct { Code Code; Text string; At time.Time }` and the constant ConfigRejected (Task 12).
- Produces: `module internal/config`
  The run description. It reads rollproof.yaml, rejects what is wrong with
  it, and gives every other unit typed values with defaults filled in and
  paths made absolute. It hides YAML, the ${…} forms and the defaults.
- Produces: `type Config struct { Path, Dir string; Cluster Cluster; Registries []Registry; Images map[string]Image; Setup [][]string; Deploy Deploy; Probe Probe; Limits Limits }`
  One run description, valid. Path is the file's real absolute path and Dir
  the folder that holds it. Every other field is one top-level key of the
  file, as section 8 gives it.
- Produces: `type Cluster struct { NodeImage, RegistryImage string; Workers int }`
  The cluster key. An empty image means the tool's default. Workers is 1 or
  more.
- Produces: `type Registry struct { Host, Username, Password, CA, Mirror string }`
  One entry of registries. CA is an absolute path. Password is the value of
  the one environment variable it named.
- Produces: `type Image struct { Local, Archive, As string }`
  One entry of images. Exactly one of Local and Archive is set; Archive is
  an absolute path.
- Produces: `type Deploy struct { Namespace, Selector string; Helm *Helm; Commands *Commands }`
  The deploy key. Exactly one of Helm and Commands is set.
- Produces: `type Helm struct { Release string; Previous, Candidate *Release }`
  The Helm driver's settings. Previous is nil for a run with no previous
  release.
- Produces: `type Release struct { Chart, Version string; IsPath bool; Values []Value }`
  One release description. IsPath says that Chart is a path that exists;
  Chart is then absolute and Version is empty.
- Produces: `type Value struct { File string; Inline map[string]any }`
  One item of a release's values: a file by absolute path, or an inline
  map. Exactly one is set.
- Produces: `type Commands struct { Install, Deploy, Rollback []string }`
  The commands driver's settings. Deploy and Rollback are nil when absent.
- Produces: `type Probe struct { HTTP *HTTP; Custom *Custom }`
  The probe key. Exactly one of HTTP and Custom is set.
- Produces: `type HTTP struct { URL string; Headers map[string]string; Rate int; Timeout time.Duration; ExpectMin, ExpectMax int }`
  The built-in probe's settings, defaults filled in.
- Produces: `type Custom struct { Image string; Command, Args []string; Env []EnvVar }`
  A custom probe's settings. Env entries are as in a pod's container.
- Produces: `type EnvVar struct { Name, Value string; ValueFrom map[string]any }`
  One entry of a custom probe's env: a name with a value, or with a
  valueFrom that is passed to the pod as it is.
- Produces: `type Limits struct { Steady, Settle, Timeout, Silence time.Duration }`
  The limits key, defaults filled in.
- Produces: `func Load(path string, env func(name string) (string, bool)) (*Config, error)`
  Reads and validates the file at path. env looks up an environment
  variable. An error is a verdict.Condition with ConfigRejected whose text
  names the key and never quotes a value.

- [ ] **Step 1: Add the YAML library and go-cmp**

Run: `go get go.yaml.in/yaml/v3@v3.0.4`
Run: `go get github.com/google/go-cmp/cmp`
Expected: `go.mod` requires both (go-cmp may already be there from an earlier task).

- [ ] **Step 2: Write the two testdata files**

`internal/config/testdata/full.yaml` is section 8's schema block with every key given a value that is not its default. It holds no `${…}` form, no password and no relative path outside `setup`, so that the values this task asserts still hold after Tasks 18 and 19 make paths absolute and fill forms:

```yaml
version: 1

cluster:
  node-image: docker.io/kindest/node:v1.37.0@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  registry-image: docker.io/library/registry:3@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
  workers: 4

registries:
  - host: registry.example
    ca: /etc/rollproof/corp-ca.pem
    mirror: mirror.corp.example/registry-example

images:
  orders:
    local: orders:candidate
    as: registry.example/team/orders

setup:
  - [kubectl, apply, -f, test/prerequisites.yaml]

deploy:
  namespace: orders
  selector: app=orders
  helm:
    release: orders
    previous:
      chart: oci://registry.example/charts/orders
      version: "1.4.2"
    candidate:
      chart: oci://registry.example/charts/orders
      version: "1.5.0"
      values:
        - /etc/rollproof/values-kind.yaml
        - image: { digest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" }
          replicas: 3

probe:
  http:
    url: http://orders.orders.svc/api/orders/latest
    headers: { Host: orders.example }
    rate: 100
    timeout: 1s
    expect-status: 200-204

limits:
  steady: 20s
  settle: 30s
  timeout: 10m
  silence: 6s
```

`internal/config/testdata/minimal.yaml`:

```yaml
version: 1
deploy:
  namespace: orders
  helm:
    release: orders
    candidate:
      chart: ./chart
probe:
  http:
    url: http://orders.orders.svc/api/orders/latest
```

- [ ] **Step 3: Write the failing tests for files that load**

Create `internal/config/load_test.go`. The helpers serve Tasks 18 and 19 too. `fromTestdata` copies a file beside an empty `chart` folder, because from Task 18 on `./chart` names a chart only when that folder exists.

```go
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
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./internal/config/`
Expected: FAIL, the package does not build: `undefined: Load`.

- [ ] **Step 5: Write the types in `internal/config/config.go`**

The package comment is the text under `module internal/config` in the Interfaces block. Every type of the Interfaces block, with its interface comment from the Interfaces block. No methods.

- [ ] **Step 6: Write `func Load(path string, env func(name string) (string, bool)) (*Config, error)` in `internal/config/load.go`**

With its interface comment from the Interfaces block. `env` is read from Task 19 on.

The defaults, as constants; Task 19 reads them too:

```go
const (
	defaultWorkers     = 3
	defaultRate        = 200
	defaultHTTPTimeout = 2 * time.Second
	defaultExpectMin   = 200
	defaultExpectMax   = 299
	defaultSteady      = 10 * time.Second
	defaultSettle      = 15 * time.Second
	defaultTimeout     = 5 * time.Minute
	defaultSilence     = 5 * time.Second
)
```

Unexported file structs carry the keys in yaml tags spelled exactly as section 8's block spells them (`node-image`, `registry-image`, `expect-status`, `valueFrom` …): `fileConfig` with the eight top-level keys (its `Registries` is `[]fileRegistry` and its `Images` `map[string]fileImage`, which Tasks 18 and 19 read), and `fileCluster`, `fileRegistry`, `fileImage`, `fileDeploy`, `fileHelm`, `fileRelease`, `fileCommands`, `fileProbe`, `fileHTTP`, `fileCustom`, `fileEnvVar`, `fileLimits`. Field types:

- A key with a default is a pointer: `*int` for workers and rate, `*duration` for `probe.http.timeout` and the four limits, `*status` for expect-status.
- `helm`, `commands`, `http`, `custom`, `previous` and `candidate` are pointers to their structs, nil when absent.
- An env entry's `value` is `*string`, so that `value: ""` is a value; its `valueFrom` is `map[string]any`.
- A release's `version` is `chartVersion`, its `values` `[]valueItem`.
- The top-level `version` is a `yaml.Node`, not a pointer to one: yaml.v3 fills a field of type `yaml.Node` and rejects `*yaml.Node`.
- Every other key is a `string`, a command a `[]string`, `setup` a `[][]string`, `headers` a `map[string]string`.

Four value types read one value each. Each has `UnmarshalYAML(n *yaml.Node) error`, and on a bad value returns `&leafError{line: n.Line, column: n.Column, problem: PROBLEM}` with the PROBLEM given here. The decoder hands such an error back unchanged; Step 10 adds the key path.

```go
type leafError struct {
	line, column int
	problem      string
}
```

- `type duration time.Duration`: a scalar that matches `^([0-9]+)(ms|s|m)$`, whose number is more than zero and fits a `time.Duration` in that unit. PROBLEM: "must be a whole number followed by ms, s or m, more than zero".
- `type status struct{ min, max int }`: a scalar that matches `^([0-9]{3})(?:-([0-9]{3}))?$`, both ends from 100 to 599 and min not above max; one code gives min equal to max. PROBLEM: "must be one status code from 100 to 599, or a range MIN-MAX of them".
- `type chartVersion string`: a scalar whose `n.ShortTag()` is `!!str` and whose value is not empty. PROBLEM for `!!int` and `!!float`: "YAML reads this as a number; write the version in quotes"; for an empty string: "must not be empty"; for anything else: "must be a string in quotes". An absent or null version never reaches `UnmarshalYAML` and stays empty.
- `type valueItem struct{ file string; inline map[string]any }`: a scalar whose ShortTag is `!!str` and whose value is not empty is a file; a mapping is decoded into `inline` with `n.Decode`. PROBLEM for anything else: "must be a file path or a map".

`Load`, in order:

1. Path is `filepath.Abs(path)` with `filepath.EvalSymlinks` applied; Dir is `filepath.Dir(Path)`.
2. Read the file and decode it with `yaml.NewDecoder` into a `yaml.Node` named `root`. Step 10 takes key paths from it; Task 19 fills forms in it.
3. Decode the same bytes again, into a `fileConfig`, with a `yaml.NewDecoder` on which `KnownFields(true)` is set. A key that the file structs do not have is an error.
4. Convert to `*Config`: a nil pointer takes its default; an absent list or map stays nil; an env entry with no `value` has Value "".

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/config/ -run 'TestLoad(Full|Minimal|ThroughALink|Accepts)' -v`
Expected: PASS

- [ ] **Step 8: Write the failing tests for what is rejected**

Append to `internal/config/load_test.go`. Every row plants `S3CRET-VALUE`, in the base or in the row, and `rejected` asserts that no text quotes it. `TestRejectionTexts` pins the form of a text: the key path, the line, a colon and the problem.

```go
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
```

- [ ] **Step 9: Run the tests to verify they fail**

Run: `go test ./internal/config/`
Expected: FAIL in TestLoadRejects, TestRejectionTexts and TestLoadFileErrors: the errors are the decoder's own, not ConfigRejected conditions, and no range is checked yet.

- [ ] **Step 10: Turn every failure of reading into a ConfigRejected condition, in `internal/config/load.go`**

`func walk(n *yaml.Node, path string, visit func(path string, key, value *yaml.Node))` visits every value node under n with its key path, depth first. A mapping's value has the parent's path, a dot and the key (no dot at the top); a sequence item has the parent's path and `[i]`, and key nil. Paths read `limits.steady`, `registries[0].host`, `deploy.helm.candidate.values[1].image.digest`. Task 19 walks with it too.

`func newIndex(root *yaml.Node) *index` records, in one walk, each value node's path, line, column and `ShortTag()`, and its key node's line and text.

`func (x *index) reject(path, problem string) error` returns `verdict.Condition{Code: verdict.ConfigRejected, Text: …}` whose text is `PATH (line L): PROBLEM`, L the line of the key (of the item, for a sequence item), or `PATH: PROBLEM` when the path is not in the file.

`func (x *index) decodeError(err error) error` turns an error of the strict decode into such a condition. It never uses the decoder's text, which quotes values:

1. A `*leafError`: the path of the value node at its line and column; `PATH (line L): PROBLEM`, or `line L: PROBLEM` when no node is there.
2. A `*yaml.TypeError`, by its first entry:
   1. `line L: field K not found in type …`: the path of the key node K on line L; PROBLEM "unknown key".
   2. `line L: cannot unmarshal TAG …`: of the value nodes on line L whose ShortTag is TAG, the one with the smallest column. PROBLEM from the Go type after the entry's last ` into `: `int` gives "must be a whole number"; one that starts with `[]` "must be a list"; one that starts with `map[` or `config.` "must be a map"; `string` "must be a single value"; any other "has the wrong type".
   3. An entry that holds "already defined": `line L: a key appears twice in one map`.
   4. Any other entry: `line L: rejected`.

Before the strict decode, Load's texts name the path as the caller gave it:

| Case | Text |
| --- | --- |
| No such file | `the configuration file PATH does not exist` |
| Any other error of reading | `the configuration file PATH cannot be read` |
| The decoder returns `io.EOF` (empty, or comments only) | `the configuration file PATH is empty` |
| A second `Decode` on the same decoder does not return `io.EOF` | `the configuration file PATH holds more than one YAML document` |
| A syntax error | `the configuration file PATH is not valid YAML (line L)`, L read from the parser's message; without the brackets when it has no line |
| The document is not a mapping | `the configuration file PATH does not hold a map of keys` |

The conversion of Step 6 rejects, each with `x.reject`:

| Path | When | PROBLEM |
| --- | --- | --- |
| `version` | absent (the node's Kind is 0) | `required; this tool reads version 1` |
| `version` | not a `!!int` scalar 1 | `this tool reads version 1 only` |
| `cluster.workers`, `probe.http.rate` | below 1 | `must be 1 or more` |
| `registries[I]` | one of username and password is empty | `username and password come together or not at all` |
| `probe.custom.env[I]` | no name | `needs a name` |
| `probe.custom.env[I]` | both or neither of value and valueFrom | `needs exactly one of value and valueFrom` |

- [ ] **Step 11: Run the tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 12: Commit**

```bash
git add go.mod go.sum internal/config
git commit -m "Read the run description strictly, with its defaults and value types"
```

### Task 18: `config`: what is rejected, paths, and what this build cannot run yet

**Files:**
- Create: `internal/config/validate.go`, `internal/config/notbuilt.go`
- Modify: `internal/config/load.go`
- Test: `internal/config/validate_test.go`, `internal/config/notbuilt_test.go`

**Interfaces:**
- Consumes: `func Load(path string, env func(name string) (string, bool)) (*Config, error)` and the types of Task 17.
- Produces: `func Load(path string, env func(name string) (string, bool)) (*Config, error)`
  As in Task 17, and now it also rejects every combination that sections 7
  and 8 reject, and makes every relative path absolute from the file's
  folder.
- Produces: `func (c *Config) NotBuilt() []string`
  The keys of this configuration that this build cannot run yet, such as
  registries or probe.custom, sorted. Empty when the build can run all of
  it. The configuration is valid either way.

- [ ] **Step 1: Add the library that parses a label selector**

Run: `go get k8s.io/apimachinery@v0.37`
Expected: `go.mod` requires `k8s.io/apimachinery` v0.37.x, the minor that Task 21's client-go needs.

- [ ] **Step 2: Write the failing tests for rejections and paths**

Create `internal/config/validate_test.go`. It uses the helpers of `load_test.go` (Task 17): `base`, `helmBlock`, `httpProbe`, `variant`, `loadText`, `loadWith`, `rejected`, `realPath`, `customProbe`, `noEnv`. `labels.Parse` accepts `app== in` (it reads `in` there as a value), so the selector row uses `app in S3CRET-VALUE`, which it rejects.

```go
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const previousBlock = "    previous:\n      chart: oci://registry.example/charts/orders\n      version: \"1.4.2\"\n"

const candidateBlock = "    candidate:\n      chart: oci://registry.example/charts/orders\n      version: \"1.5.0\"\n"

// commandsBlock is a deploy.commands block with the given command lines.
func commandsBlock(lines string) string { return "  commands:\n" + lines }

func TestValidateRejects(t *testing.T) {
	const url, ns = "    url: http://orders.orders.svc/api/S3CRET-VALUE\n", "  namespace: orders\n"
	registries := func(entries string) string { return "registries:\n" + entries + "deploy:\n" }
	image := func(name string) string { return "images:\n  " + name + ":\n    local: orders:S3CRET-VALUE\ndeploy:\n" }
	for _, tc := range []struct{ name, old, new, want string }{
		{"helm and commands", helmBlock, helmBlock + commandsBlock("    install: [kubectl, apply, -k, overlays/S3CRET-VALUE]\n"), "deploy.commands"},
		{"neither helm nor commands", helmBlock, "", "deploy.helm"},
		{"an http and a custom probe", "  http:\n", "  custom:\n    image: registry.example/tools/S3CRET-VALUE\n  http:\n", "probe.custom"},
		{"no probe", httpProbe, "", "probe.http"},
		{"an image with local and archive", "deploy:\n", "images:\n  orders:\n    local: orders:candidate\n    archive: S3CRET-VALUE.tar\ndeploy:\n", "images.orders"},
		{"an image with neither", "deploy:\n", "images:\n  orders:\n    as: registry.example/S3CRET-VALUE\ndeploy:\n", "images.orders"},
		{"an image named Orders", "deploy:\n", image("Orders"), "images.Orders"},
		{"an image named my image", "deploy:\n", image("my image"), "images.my image"},
		{"an image named -x", "deploy:\n", image(`"-x"`), "images.-x"},
		{"an image named x-", "deploy:\n", image("x-"), "images.x-"},
		{"an image named a..b", "deploy:\n", image("a..b"), "images.a..b"},
		{"helm with no candidate", candidateBlock, "", "deploy.helm.candidate"},
		{"helm with no release", "    release: orders\n", "", "deploy.helm.release"},
		{"a release with no chart", candidateBlock, "    candidate:\n      version: \"1.5.0\"\n", "deploy.helm.candidate.chart"},
		{"commands with no install", helmBlock, commandsBlock("    deploy: [kubectl, apply, -k, overlays/S3CRET-VALUE]\n"), "deploy.commands.install"},
		{"a rollback with no deploy", helmBlock, commandsBlock("    install: [kubectl, apply, -k, overlays/previous]\n    rollback: [kubectl, apply, -k, overlays/S3CRET-VALUE]\n"), "deploy.commands.deploy"},
		{"silence equal to steady", "  steady: 10s\n", "  steady: 5s\n", "limits.silence"},
		{"silence longer than steady", "  steady: 10s\n", "  steady: 4s\n", "limits.silence"},
		{"silence equal to settle", "  settle: 15s\n", "  settle: 5s\n", "limits.silence"},
		{"silence longer than settle", "  settle: 15s\n", "  settle: 3s\n", "limits.silence"},
		{"probe timeout plus one second equal to silence", "    timeout: 2s\n", "    timeout: 4s\n",
			"probe.http.timeout (line 20): plus one second must be shorter than limits.silence"},
		{"probe timeout longer than silence", "    timeout: 2s\n", "    timeout: 6s\n", "probe.http.timeout"},
		{"one host twice", "deploy:\n", registries("  - host: registry.example\n  - host: registry.example\n    mirror: S3CRET-VALUE\n"), "registries[1].host"},
		{"a registry with no host", "deploy:\n", registries("  - mirror: S3CRET-VALUE\n"), "registries[0].host"},
		{"namespace kube-system", ns, "  namespace: kube-system\n", "deploy.namespace"},
		{"namespace kube-public", ns, "  namespace: kube-public\n", "deploy.namespace"},
		{"namespace kube-node-lease", ns, "  namespace: kube-node-lease\n", "deploy.namespace"},
		{"namespace local-path-storage", ns, "  namespace: local-path-storage\n", "deploy.namespace"},
		{"the probe's namespace", ns, "  namespace: rollproof-probe\n", "deploy.namespace"},
		{"an empty namespace", ns, "  namespace: \"\"\n", "deploy.namespace"},
		{"no namespace", ns, "", "deploy.namespace"},
		{"a URL with a user and a password", url, "    url: http://user:S3CRET-VALUE@orders.orders.svc/\n", "probe.http.url"},
		{"a URL with a user", url, "    url: http://S3CRET-VALUE@orders.orders.svc/\n", "probe.http.url"},
		{"a URL with another scheme", url, "    url: ftp://orders.orders.svc/S3CRET-VALUE\n", "probe.http.url"},
		{"a URL with no scheme", url, "    url: orders.orders.svc/S3CRET-VALUE\n", "probe.http.url"},
		{"a URL with no host", url, "    url: http:///S3CRET-VALUE\n", "probe.http.url"},
		{"no URL", url, "", "probe.http.url"},
		{"a remote chart with no version", candidateBlock, "    candidate:\n      chart: oci://registry.example/charts/S3CRET-VALUE\n", "deploy.helm.candidate.version"},
		{"an alias chart with no version", candidateBlock, "    candidate:\n      chart: stable/S3CRET-VALUE\n", "deploy.helm.candidate.version"},
		{"a comma in a values file's path", candidateBlock, candidateBlock + "      values:\n        - test/values,S3CRET-VALUE.yaml\n", "deploy.helm.candidate.values[0]"},
		{"a selector that does not parse", ns, ns + "  selector: app in S3CRET-VALUE\n", "deploy.selector"},
		{"a custom probe with no image", httpProbe, "probe:\n  custom:\n    command: [sh, -c, \"echo S3CRET-VALUE\"]\n", "probe.custom.image"},
		{"a password written in the file", "deploy:\n", registries("  - host: registry.example\n    username: ci\n    password: S3CRET-VALUE\n"), "registries[0].password"},
		{"a password with text beside the form", "deploy:\n", registries("  - host: registry.example\n    username: ci\n    password: S3CRET-VALUE${env.P}\n"), "registries[0].password"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadText(t, variant(t, base, tc.old, tc.new))
			rejected(t, err, tc.want)
		})
	}
}

func TestValidateAccepts(t *testing.T) {
	for _, tc := range []struct{ name, old, new string }{
		{"an empty selector", "  namespace: orders\n", "  namespace: orders\n  selector: \"\"\n"},
		{"a selector with a set", "  namespace: orders\n", "  namespace: orders\n  selector: \"app=orders,tier in (a,b)\"\n"},
		{"an https URL", "    url: http://orders.orders.svc/api/S3CRET-VALUE\n", "    url: https://orders.orders.svc:8443/api\n"},
		{"probe timeout 2s with silence 5s", "    timeout: 2s\n", "    timeout: 2s\n"},
		{"probe timeout 3900ms with silence 5s", "    timeout: 2s\n", "    timeout: 3900ms\n"},
		{"an image named orders", "deploy:\n", "images:\n  orders:\n    local: orders:candidate\ndeploy:\n"},
		{"an image named orders-v2", "deploy:\n", "images:\n  orders-v2:\n    local: orders:candidate\ndeploy:\n"},
		{"an image named a.b_c", "deploy:\n", "images:\n  a.b_c:\n    local: orders:candidate\ndeploy:\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := loadText(t, variant(t, base, tc.old, tc.new)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// project makes a folder named "my project" under a new temporary folder,
// with the given subfolders, and returns the folder and its real path.
func project(t *testing.T, subfolders ...string) (dir, real string) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), "my project")
	for _, sub := range append([]string{"."}, subfolders...) {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir, realPath(t, dir)
}

func TestVersionBesideAChartPath(t *testing.T) {
	dir, _ := project(t, "chart")
	_, err := loadWith(t, dir, variant(t, base, candidateBlock, "    candidate:\n      chart: ./chart\n      version: \"1.5.0\"\n"), noEnv)
	rejected(t, err, "deploy.helm.candidate.version")
}

func TestChartPaths(t *testing.T) {
	dir, real := project(t, "chart")
	t.Chdir(t.TempDir())
	for _, chart := range []string{"./chart", "chart", "chart/", filepath.Join(dir, "chart")} {
		c, err := loadWith(t, dir, variant(t, base, candidateBlock, "    candidate:\n      chart: "+chart+"\n"), noEnv)
		if err != nil {
			t.Fatalf("chart %q: %v", chart, err)
		}
		want := &Release{Chart: filepath.Join(real, "chart"), IsPath: true}
		if d := cmp.Diff(want, c.Deploy.Helm.Candidate); d != "" {
			t.Errorf("chart %q (-want +got):\n%s", chart, d)
		}
	}
	for _, chart := range []string{"oci://registry.example/charts/orders", "stable/orders"} {
		c, err := loadWith(t, dir, variant(t, base, candidateBlock, "    candidate:\n      chart: "+chart+"\n      version: \"1.5.0\"\n"), noEnv)
		if err != nil {
			t.Fatalf("chart %q: %v", chart, err)
		}
		want := &Release{Chart: chart, Version: "1.5.0"}
		if d := cmp.Diff(want, c.Deploy.Helm.Candidate); d != "" {
			t.Errorf("chart %q (-want +got):\n%s", chart, d)
		}
	}
}

func TestPathsAreRelativeToTheFile(t *testing.T) {
	dir, real := project(t, "chart")
	t.Chdir(t.TempDir())
	text := variant(t, base, candidateBlock, "    candidate:\n      chart: ./chart\n      values:\n        - test/values-kind.yaml\n        - { replicas: 2 }\n")
	text = variant(t, text, "deploy:\n", "registries:\n  - host: registry.example\n    ca: certs/corp-ca.pem\n"+
		"images:\n  orders:\n    archive: images/orders.tar\nsetup:\n  - [kubectl, apply, -f, test/prerequisites.yaml]\ndeploy:\n")
	c, err := loadWith(t, dir, text, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	wantValues := []Value{{File: filepath.Join(real, "test", "values-kind.yaml")}, {Inline: map[string]any{"replicas": 2}}}
	if d := cmp.Diff(wantValues, c.Deploy.Helm.Candidate.Values); d != "" {
		t.Errorf("values (-want +got):\n%s", d)
	}
	if got, want := c.Registries[0].CA, filepath.Join(real, "certs", "corp-ca.pem"); got != want {
		t.Errorf("ca %q, want %q", got, want)
	}
	// Task 22's later plan reads the archive; this build only makes its path absolute.
	if got, want := c.Images["orders"].Archive, filepath.Join(real, "images", "orders.tar"); got != want {
		t.Errorf("archive %q, want %q", got, want)
	}
	if d := cmp.Diff([][]string{{"kubectl", "apply", "-f", "test/prerequisites.yaml"}}, c.Setup); d != "" {
		t.Errorf("setup changed (-want +got):\n%s", d)
	}

	commands := variant(t, base, helmBlock, commandsBlock("    install: [kubectl, apply, -k, overlays/previous]\n"))
	c, err = loadWith(t, dir, commands, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff([]string{"kubectl", "apply", "-k", "overlays/previous"}, c.Deploy.Commands.Install); d != "" {
		t.Errorf("install changed (-want +got):\n%s", d)
	}
}

func TestPathsThroughALink(t *testing.T) {
	dir, real := project(t, "chart")
	target := filepath.Join(dir, "rollproof.yaml")
	if err := os.WriteFile(target, []byte(variant(t, base, candidateBlock, "    candidate:\n      chart: ./chart\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "rollproof.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	c, err := Load(link, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := c.Deploy.Helm.Candidate.Chart, filepath.Join(real, "chart"); got != want {
		t.Errorf("chart %q, want %q", got, want)
	}
}
```

- [ ] **Step 3: Write the failing tests for `NotBuilt`**

Create `internal/config/notbuilt_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestNotBuilt(t *testing.T) {
	for _, tc := range []struct {
		name, old, new string
		want           []string
	}{
		{"everything built", "deploy:\n", "deploy:\n", nil},
		{"registries", "deploy:\n", "registries:\n  - host: registry.example\ndeploy:\n", []string{"registries"}},
		{"setup", "deploy:\n", "setup:\n  - [kubectl, apply, -f, test/prerequisites.yaml]\ndeploy:\n", []string{"setup"}},
		{"the commands driver", helmBlock, commandsBlock("    install: [kubectl, apply, -k, overlays/previous]\n" +
			"    deploy: [kubectl, apply, -k, overlays/candidate]\n    rollback: [kubectl, apply, -k, overlays/previous]\n"), []string{"deploy.commands"}},
		{"a custom probe", httpProbe, customProbe(""), []string{"probe.custom"}},
		{"an image from an archive", "deploy:\n", "images:\n  orders:\n    archive: orders.tar\ndeploy:\n", []string{"images.orders.archive"}},
		{"an image with as", "deploy:\n", "images:\n  orders:\n    local: orders:candidate\n    as: registry.example/team/orders\ndeploy:\n", []string{"images.orders.as"}},
		{"a local image", "deploy:\n", "images:\n  orders:\n    local: orders:candidate\ndeploy:\n", nil},
		{"no previous release", previousBlock, "", []string{"deploy.helm.previous"}},
		{"several at once", "deploy:\n", "registries:\n  - host: registry.example\nsetup:\n  - [kubectl, apply, -f, test/prerequisites.yaml]\n" +
			"images:\n  orders:\n    local: orders:candidate\n    as: registry.example/team/orders\ndeploy:\n", []string{"images.orders.as", "registries", "setup"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := loadText(t, variant(t, base, tc.old, tc.new))
			if err != nil {
				t.Fatal(err)
			}
			if d := cmp.Diff(tc.want, c.NotBuilt(), cmpopts.EquateEmpty()); d != "" {
				t.Errorf("NotBuilt (-want +got):\n%s", d)
			}
		})
	}
}

func TestNotBuiltMinimalWithAPrevious(t *testing.T) {
	path := fromTestdata(t, "minimal.yaml")
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	withPrevious := variant(t, string(text), "    candidate:\n", previousBlock+"    candidate:\n")
	c, err := loadWith(t, filepath.Dir(path), withPrevious, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.NotBuilt(); len(got) != 0 {
		t.Errorf("NotBuilt = %q, want none", got)
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./internal/config/`
Expected: FAIL, the package does not build: `c.NotBuilt undefined (type *Config has no field or method NotBuilt)`.

- [ ] **Step 5: Make paths absolute: `func absolutePaths(c *Config)` in `internal/config/validate.go`**

A path is made absolute by `filepath.Join(c.Dir, p)` unless it is empty or already absolute. Dir is the file's real folder (Task 17), so a file reached through a link resolves against the folder it lies in.

- `registries[].ca`, `images.NAME.archive` and every values file: made absolute.
- A chart that does not start with `oci://`: made absolute and cleaned; when `os.Stat` finds it, Chart becomes that path and IsPath true. Otherwise Chart stays as written and IsPath false.
- `setup` and the commands driver's arguments: unchanged. Commands start in Dir (section 7), so their relative paths already resolve there.

- [ ] **Step 6: Reject what sections 7 and 8 reject: `func validate(c *Config, x *index) error` in `internal/config/validate.go`**

It runs on the converted, absolute configuration and returns the first rejection, checking in the order of this table, each with `x.reject(PATH, PROBLEM)` (Task 17). Put this comment above `validate`: "An image archive that holds more than one image is rejected too (section 8). That check needs the file, so the plan that builds `archive` reads it."

| Path | When | PROBLEM |
| --- | --- | --- |
| `deploy` | both or neither of helm and commands | `exactly one of deploy.helm and deploy.commands` |
| `probe` | both or neither of http and custom | `exactly one of probe.http and probe.custom` |
| `images.NAME` | NAME does not match `^[a-z0-9]+(?:(?:[._]\|__\|-+)[a-z0-9]+)*$`, the grammar of a path component in a distribution reference | `the name must be lower-case letters and digits, joined by a dot, one or two underscores, or dashes` |
| `images.NAME` | both or neither of local and archive | `exactly one of local and archive` |
| `deploy.helm.release` | empty | `required` |
| `deploy.helm.candidate` | absent | `required` |
| `deploy.helm.ROLE.chart` | empty (ROLE is previous, then candidate) | `required` |
| `deploy.helm.ROLE.version` | IsPath and a version | `not allowed beside a chart that is a path` |
| `deploy.helm.ROLE.version` | not IsPath and no version | `required for a chart that is not a path` |
| `deploy.helm.ROLE.values[I]` | the absolute path holds a comma | `Helm splits a values file's path at a comma` |
| `deploy.commands.install` | empty | `required` |
| `deploy.commands.deploy` | rollback set, deploy empty | `required beside deploy.commands.rollback` |
| `deploy.namespace` | empty | `required` |
| `deploy.namespace` | kube-system, kube-public, kube-node-lease, local-path-storage or rollproof-probe | `reserved for the cluster or the probe` |
| `deploy.selector` | `labels.Parse` (k8s.io/apimachinery/pkg/labels) returns an error; an empty selector parses | `not a valid label selector` |
| `limits.silence` | not shorter than limits.steady | `must be shorter than limits.steady` |
| `limits.silence` | not shorter than limits.settle | `must be shorter than limits.settle` |
| `probe.http.url` | empty | `required` |
| `probe.http.url` | `url.Parse` fails | `not a URL` |
| `probe.http.url` | the URL has user information | `must not carry a user name or a password` |
| `probe.http.url` | the scheme is not http or https | `the scheme must be http or https` |
| `probe.http.url` | `Hostname()` is empty | `names no host` |
| `probe.http.timeout` | the timeout plus 1 s is not shorter than limits.silence: the built-in probe reports a pass in the summary line of the second in which it ended, so while it works it may write nothing for one timeout and one second | `plus one second must be shorter than limits.silence` |
| `probe.custom.image` | empty | `required` |
| `registries[I].host` | empty | `required` |
| `registries[I].host` | the host of an earlier entry J | `the same host as registries[J]` |

No text quotes a value: the errors of `labels.Parse` and `url.Parse` quote the input, so they are never used.

- [ ] **Step 7: Check the password's raw form: `func checkPasswordForms(f *fileConfig, x *index) error` in `internal/config/load.go`**

A non-empty `registries[I].password` that does not match `^\$\{env\.[A-Za-z_][A-Za-z0-9_]*\}$` is rejected at `registries[I].password` with `must be one ${env.NAME} form`. It reads the file struct, before substitution exists. It lives in `load.go` because Task 19 moves the check into substitution and changes `load.go` only.

- [ ] **Step 8: Call them from `Load`**

After the strict decode: `checkPasswordForms`. After the conversion has set Path and Dir: `absolutePaths`, then `validate`.

- [ ] **Step 9: Write `func (c *Config) NotBuilt() []string` in `internal/config/notbuilt.go`**

With its interface comment from the Interfaces block. One key for each that is present, sorted with `sort.Strings`: `registries` (an entry), `setup` (a command), `deploy.commands`, `deploy.helm.previous` (Helm with no previous), `probe.custom`, `images.NAME.archive` and `images.NAME.as` for each entry that sets one.

- [ ] **Step 10: Run the tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS, Task 17's tests included.

- [ ] **Step 11: Commit**

```bash
git add go.mod go.sum internal/config
git commit -m "Reject the configurations the design rejects, and make paths absolute"
```

### Task 19: `config`: substitution, image references, and changed settings

**Files:**
- Create: `internal/config/substitute.go`
- Modify: `internal/config/load.go`
- Test: `internal/config/substitute_test.go`

**Interfaces:**
- Consumes: `func Load(path string, env func(name string) (string, bool)) (*Config, error)` and the types of Tasks 17 and 18.
- Produces: `func Load(path string, env func(name string) (string, bool)) (*Config, error)`
  As in Task 18, and now it fills every ${env.NAME} form from env and
  checks every ${images.…} form, which it leaves in place for Resolve.
- Produces: `type ImageRef struct { Repository, Digest string }`
  Where one image of the run can be pulled from: its repository, host
  included, and its digest with the sha256: prefix.
- Produces: `func (r ImageRef) Ref() string`
  The repository and the digest joined by @.
- Produces: `func (c *Config) Resolve(images map[string]ImageRef) error`
  Fills every ${images.NAME.digest}, .repository and .ref form under setup,
  deploy and probe. images must have every name the forms use; a missing
  one is an error. After Resolve no form is left.
- Produces: `type Setting struct { Key, Value, Default string }`
  One setting as text for the report: its key as the file spells it, its
  value, and its default.
- Produces: `func (c *Config) Settings() []Setting`
  Every setting of cluster, probe.http and limits with its value and its
  default, in a fixed order. It never holds a header's value, a password
  or a command.

- [ ] **Step 1: Write the failing tests**

Create `internal/config/substitute_test.go`. It uses the helpers of `load_test.go` (Task 17) and `candidateBlock` of `validate_test.go` (Task 18).

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// envOf looks names up in m.
func envOf(m map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := m[name]
		return v, ok
	}
}

// withInline adds one inline values map, written in flow style, to the candidate.
func withInline(t *testing.T, text, inline string) string {
	t.Helper()
	return variant(t, text, candidateBlock, candidateBlock+"      values:\n        - "+inline+"\n")
}

// withImage adds the images entry orders, a local image, to text.
func withImage(t *testing.T, text string) string {
	t.Helper()
	return variant(t, text, "deploy:\n", "images:\n  orders:\n    local: orders:candidate\ndeploy:\n")
}

func TestSubstituteFills(t *testing.T) {
	inline := func(c *Config) any { return c.Deploy.Helm.Candidate.Values[0].Inline }
	for _, tc := range []struct {
		name, text string
		env        map[string]string
		get        func(c *Config) any
		want       any
	}{
		{"a whole value", variant(t, base, "  namespace: orders\n", "  namespace: ${env.NS}\n"),
			map[string]string{"NS": "orders"}, func(c *Config) any { return c.Deploy.Namespace }, "orders"},
		{"text around a form", withInline(t, base, `{ note: "a-${env.A}-b" }`),
			map[string]string{"A": "x"}, inline, map[string]any{"note": "a-x-b"}},
		{"$${ is a literal ${", withInline(t, base, `{ note: "$${env.X}" }`),
			nil, inline, map[string]any{"note": "${env.X}"}},
		{"one pass", withInline(t, base, `{ note: "${env.A}" }`),
			map[string]string{"A": "${env.OTHER}"}, inline, map[string]any{"note": "${env.OTHER}"}},
		{"any depth, and no key", withInline(t, base, `{ "${env.K}": { list: ["${env.V}", 3] } }`),
			map[string]string{"K": "key", "V": "value"}, inline, map[string]any{"${env.K}": map[string]any{"list": []any{"value", 3}}}},
		{"a chart version", variant(t, base, "      version: \"1.4.2\"\n", "      version: ${env.PREVIOUS_VERSION}\n"),
			map[string]string{"PREVIOUS_VERSION": "1.10"}, func(c *Config) any { return c.Deploy.Helm.Previous.Version }, "1.10"},
		{"a password", variant(t, base, "deploy:\n", "registries:\n  - host: registry.example\n    username: ci\n    password: ${env.REG_PW}\ndeploy:\n"),
			map[string]string{"REG_PW": planted}, func(c *Config) any { return c.Registries[0].Password }, planted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := loadWith(t, t.TempDir(), tc.text, envOf(tc.env))
			if err != nil {
				t.Fatal(err)
			}
			if d := cmp.Diff(tc.want, tc.get(c)); d != "" {
				t.Errorf("(-want +got):\n%s", d)
			}
		})
	}
}

func TestSubstituteRejects(t *testing.T) {
	note := func(form string) string { return withInline(t, base, `{ note: "`+form+`" }`) }
	image := withImage(t, base)
	registry := func(password string) string {
		return variant(t, base, "deploy:\n", "registries:\n  - host: registry.example\n    username: ci\n    password: "+password+"\ndeploy:\n")
	}
	for _, tc := range []struct {
		name, text string
		env        map[string]string
		want       string
	}{
		{"a form in a number field", variant(t, base, "  workers: 3\n", "  workers: ${env.N}\n"), map[string]string{"N": planted}, "cluster.workers"},
		{"a variable that is not set", variant(t, base, "  namespace: orders\n", "  namespace: ${env.NS}\n"), nil, "NS"},
		{"a variable set to nothing", variant(t, base, "  namespace: orders\n", "  namespace: ${env.NS}\n"), map[string]string{"NS": ""}, "NS"},
		{"${foo}", note("${foo}"), nil, "values[0].note"},
		{"${env.}", note("${env.}"), nil, "values[0].note"},
		{"${ENV.X}", note("${ENV.X}"), map[string]string{"X": planted}, "values[0].note"},
		{"${images.orders}", withImage(t, note("${images.orders}")), nil, "values[0].note"},
		{"${images.orders.tag}", withImage(t, note("${images.orders.tag}")), nil, "values[0].note"},
		{"an unclosed form", note("S3CRET-VALUE ${env.X"), map[string]string{"X": planted}, "values[0].note"},
		{"an image form under cluster", variant(t, image, "  workers: 3\n", "  node-image: ${images.orders.ref}\n  workers: 3\n"), nil, "cluster.node-image"},
		{"an image form under registries", variant(t, image, "images:\n", "registries:\n  - host: registry.example\n    mirror: ${images.orders.repository}\nimages:\n"), nil, "registries[0].mirror"},
		{"an image form under images", variant(t, image, "    local: orders:candidate\n", "    local: orders:candidate\n    as: ${images.orders.repository}\n"), nil, "images.orders.as"},
		{"an image form under limits", variant(t, image, "  steady: 10s\n", "  steady: ${images.orders.digest}\n"), nil, "limits.steady"},
		{"an image that images does not list", withImage(t, note("${images.nope.digest}")), nil, "nope"},
		{"a password written in the file", registry(planted), nil, "registries[0].password"},
		{"a password with text beside the form", registry("x${env.P}"), map[string]string{"P": planted}, "registries[0].password"},
		{"a password from a variable that is not set", registry("${env.P}"), nil, "P"},
		{"a later rejection after a password was taken", variant(t, registry("${env.P}"), "deploy:\n", "  - host: registry.example\ndeploy:\n"),
			map[string]string{"P": planted}, "registries[1].host"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadWith(t, t.TempDir(), tc.text, envOf(tc.env))
			rejected(t, err, tc.want)
		})
	}
}

func TestValuesFilesAreNotRead(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "values.yaml"), []byte("image: ${env.NOPE}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	text := variant(t, base, candidateBlock, candidateBlock+"      values:\n        - values.yaml\n")
	if _, err := loadWith(t, dir, text, noEnv); err != nil {
		t.Fatal(err)
	}
}

func TestResolve(t *testing.T) {
	text := withImage(t, withInline(t, base, `{ image: { digest: "${images.orders.digest}", repository: "${images.orders.repository}",`+
		` ref: "${images.orders.ref}", tagged: "candidate@${images.orders.digest}" } }`))
	text = variant(t, text, "deploy:\n", "setup:\n  - [kubectl, set, image, deploy/x, \"app=${images.orders.ref}\"]\ndeploy:\n")
	text = variant(t, text, "      X-Planted: S3CRET-VALUE\n", "      X-Planted: S3CRET-VALUE\n      X-Image: ${images.orders.digest}\n")
	dir := t.TempDir()
	load := func() *Config {
		c, err := loadWith(t, dir, text, noEnv)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	stored := map[string]ImageRef{"orders": {Repository: "rollproof.test/orders", Digest: "sha256:abc"}}

	c := load()
	image := func(c *Config) any { return c.Deploy.Helm.Candidate.Values[0].Inline["image"] }
	before := map[string]any{"digest": "${images.orders.digest}", "repository": "${images.orders.repository}",
		"ref": "${images.orders.ref}", "tagged": "candidate@${images.orders.digest}"}
	if d := cmp.Diff(before, image(c)); d != "" {
		t.Errorf("Load filled an image form (-want +got):\n%s", d)
	}
	if err := c.Resolve(stored); err != nil {
		t.Fatal(err)
	}
	after := map[string]any{"digest": "sha256:abc", "repository": "rollproof.test/orders",
		"ref": "rollproof.test/orders@sha256:abc", "tagged": "candidate@sha256:abc"}
	if d := cmp.Diff(after, image(c)); d != "" {
		t.Errorf("after Resolve (-want +got):\n%s", d)
	}
	if got := c.Setup[0][4]; got != "app=rollproof.test/orders@sha256:abc" {
		t.Errorf("setup argument %q", got)
	}
	if got := c.Probe.HTTP.Headers["X-Image"]; got != "sha256:abc" {
		t.Errorf("header %q", got)
	}

	twice := load()
	for range 2 {
		if err := twice.Resolve(stored); err != nil {
			t.Fatal(err)
		}
	}
	if d := cmp.Diff(c, twice); d != "" {
		t.Errorf("a second Resolve changed the configuration (-once +twice):\n%s", d)
	}

	if err := load().Resolve(map[string]ImageRef{}); err == nil || !strings.Contains(err.Error(), "orders") {
		t.Errorf("Resolve with no stored image: %v, want an error that names orders", err)
	}
	if got := (ImageRef{Repository: "rollproof.test/orders", Digest: "sha256:abc"}).Ref(); got != "rollproof.test/orders@sha256:abc" {
		t.Errorf("Ref() = %q", got)
	}
}

func TestResolveKeepsALiteralBesideAForm(t *testing.T) {
	c, err := loadWith(t, t.TempDir(), withImage(t, withInline(t, base, `{ both: "$${images.orders.digest} is ${images.orders.digest}" }`)), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	both := func() any { return c.Deploy.Helm.Candidate.Values[0].Inline["both"] }
	if got := both(); got != "$${images.orders.digest} is ${images.orders.digest}" {
		t.Errorf("after Load %q, want $${ kept beside the form", got)
	}
	if err := c.Resolve(map[string]ImageRef{"orders": {Repository: "rollproof.test/orders", Digest: "sha256:abc"}}); err != nil {
		t.Fatal(err)
	}
	if got := both(); got != "${images.orders.digest} is sha256:abc" {
		t.Errorf("after Resolve %q", got)
	}
}

func TestSettings(t *testing.T) {
	c, err := Load(fromTestdata(t, "minimal.yaml"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	want := []Setting{
		{"cluster.node-image", "the linked kind release's", "the linked kind release's"},
		{"cluster.registry-image", "the tool's pinned registry image", "the tool's pinned registry image"},
		{"cluster.workers", "3", "3"},
		{"probe.http.rate", "200", "200"},
		{"probe.http.timeout", "2s", "2s"},
		{"probe.http.expect-status", "200-299", "200-299"},
		{"limits.steady", "10s", "10s"},
		{"limits.settle", "15s", "15s"},
		{"limits.timeout", "5m", "5m"},
		{"limits.silence", "5s", "5s"},
	}
	if d := cmp.Diff(want, c.Settings()); d != "" {
		t.Errorf("minimal.yaml (-want +got):\n%s", d)
	}

	text := variant(t, base, "  workers: 3\n", "  node-image: kindest/node:v1.37.0@"+digestA+"\n  workers: 4\n")
	text = variant(t, text, "    rate: 200\n", "    rate: 100\n")
	text = variant(t, text, "  steady: 10s\n", "  steady: 5m\n")
	c, err = loadText(t, text)
	if err != nil {
		t.Fatal(err)
	}
	var changed []Setting
	for _, s := range c.Settings() {
		if strings.Contains(s.Value, planted) {
			t.Errorf("setting %s holds a header's value", s.Key)
		}
		if s.Value != s.Default {
			changed = append(changed, s)
		}
	}
	wantChanged := []Setting{
		{"cluster.node-image", "kindest/node:v1.37.0@" + digestA, "the linked kind release's"},
		{"cluster.workers", "4", "3"},
		{"probe.http.rate", "100", "200"},
		{"limits.steady", "5m", "10s"},
	}
	if d := cmp.Diff(wantChanged, changed); d != "" {
		t.Errorf("changed settings (-want +got):\n%s", d)
	}

	c, err = loadText(t, variant(t, base, "    expect-status: 200-299\n", "    expect-status: 200\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Settings()[5]; got.Key != "probe.http.expect-status" || got.Value != "200" {
		t.Errorf("one status code prints as %+v", got)
	}

	c, err = loadText(t, variant(t, base, httpProbe, customProbe("")))
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, s := range c.Settings() {
		keys = append(keys, s.Key)
	}
	wantKeys := []string{"cluster.node-image", "cluster.registry-image", "cluster.workers", "limits.steady", "limits.settle", "limits.timeout", "limits.silence"}
	if d := cmp.Diff(wantKeys, keys); d != "" {
		t.Errorf("custom probe (-want +got):\n%s", d)
	}
}

func TestFormatDuration(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Minute, "5m"},
		{2 * time.Minute, "2m"},
		{10 * time.Second, "10s"},
		{90 * time.Second, "90s"},
		{500 * time.Millisecond, "500ms"},
		{1500 * time.Millisecond, "1500ms"},
	} {
		if got := formatDuration(tc.d); got != tc.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/config/`
Expected: FAIL, the package does not build: `undefined: ImageRef`.

- [ ] **Step 3: Fill the forms in the node tree: `func substitute(root *yaml.Node, images map[string]fileImage, env func(string) (string, bool), x *index) error` in `internal/config/substitute.go`**

It walks `root` with `walk` (Task 17) and changes only value nodes that are scalars whose `ShortTag()` is `!!str`. Keys are never visited as values, and a scalar typed by YAML as a number or a boolean is left alone. It stops at the first rejection, each made with `x.reject(PATH, PROBLEM)`.

1. At a path `registries[I].password`, a value that does not match the pattern of Task 18's `checkPasswordForms` is rejected first: `must be one ${env.NAME} form`.
2. The value is read once, left to right, into a new string; nothing written is read again:
   - `$${` writes `${`, except in a value that holds an images form: there it writes `$${`, and Resolve turns it into `${` when it fills the forms. When the pass meets an images form, the value is read again from the start with `$${` kept.
   - `${` up to the next `}` is a form; with no `}`: `a ${ with no closing }`.
   - `env.NAME`, NAME matching `[A-Za-z_][A-Za-z0-9_]*`: env(NAME) is written. Not set: `the variable NAME is not set`; set to "": `the variable NAME is empty`.
   - `images.NAME.FIELD`, FIELD one of digest, repository and ref, NAME the text between `images.` and the last dot: only when the path is `setup`, starts with `setup[`, `deploy.` or `probe.`, else `${images.…} forms are allowed under setup, deploy and probe only`; NAME must be a key of `images`, else `images has no entry NAME`. The form is written back unchanged for `Resolve`.
   - Any other form: `a ${…} form that this tool does not know; write $${ for a literal ${`.
   - Any other character is written as it is.

A values file is a path to Helm and is never opened.

- [ ] **Step 4: Change `Load` in `internal/config/load.go`**

`yaml.Node.Decode` cannot reject unknown keys, so the strict decode of Task 17 stays on the file's own bytes: it sees every form as a string, so it rejects the same keys and the same types, with the file's line numbers. Then:

1. `substitute(&root, f.Images, env, x)` on the node tree of Task 17's step 2.
2. Decode `root` again with `root.Decode` into a new `fileConfig`; an error goes through `x.decodeError`, so a filled duration that does not parse is rejected with its key.
3. Convert, make paths absolute and validate as before, on the filled values.

Remove `checkPasswordForms` and its call: step 1 of Step 3 does its work.

- [ ] **Step 5: Write `ImageRef`, `Ref` and `Resolve` in `internal/config/substitute.go`**

`type ImageRef struct { Repository, Digest string }`, `func (r ImageRef) Ref() string` and `func (c *Config) Resolve(images map[string]ImageRef) error`, each with its interface comment from the Interfaces block.

Resolve reads each of these strings once, left to right: `$${` writes `${`, and a match of `^\$\{images\.([^}]+)\.(digest|repository|ref)\}` at that point writes the image's Digest, Repository or `Ref()`. A string in which it meets no images form is left as it is: Load has already turned its `$${` into `${`. The strings: every argument of `Setup`; `Deploy.Namespace`, `Deploy.Selector`; for Helm the release and, for each release, Chart, Version, every values File and every string at any depth of every Inline map (inside maps and lists; keys untouched); the three lists of `Deploy.Commands`; `Probe.HTTP.URL` and each header's value; `Probe.Custom.Image`, `Command`, `Args`, and each env entry's Value and every string at any depth of its ValueFrom. A NAME that `images` lacks is an error whose text names it. Text around a form stays.

- [ ] **Step 6: Write `Setting` and `Settings` in `internal/config/substitute.go`**

`type Setting struct { Key, Value, Default string }` and `func (c *Config) Settings() []Setting`, each with its interface comment from the Interfaces block.

The entries, in this order: `cluster.node-image`, `cluster.registry-image`, `cluster.workers`; then, when `Probe.HTTP` is set, `probe.http.rate`, `probe.http.timeout`, `probe.http.expect-status`; then `limits.steady`, `limits.settle`, `limits.timeout`, `limits.silence`. Defaults come from Task 17's constants. The node image's Default is `the linked kind release's` and the registry image's `the tool's pinned registry image`; an empty image prints as its Default. Numbers print with `strconv.Itoa`. expect-status prints as `MIN-MAX`, or as the one code when MIN equals MAX.

`func formatDuration(d time.Duration) string`: whole minutes as `Nm`, else whole seconds as `Ns`, else `Nms`.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS, the tests of Tasks 17 and 18 included.

- [ ] **Step 8: Commit**

```bash
git add internal/config
git commit -m "Fill the configuration's variable and image forms, and list changed settings"
```
