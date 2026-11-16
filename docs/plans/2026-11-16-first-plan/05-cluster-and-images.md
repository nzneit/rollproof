> Part of the plan in `README.md` of this folder. Read that file first: its Global Constraints apply to every task here.

### Task 20: `cluster`: names, the run's folder, the kind description and a node's registry settings

**Files:**
- Create: `internal/cluster/names.go`, `internal/cluster/kindconfig.go`, `internal/cluster/hosts.go`
- Test: `internal/cluster/names_test.go`, `internal/cluster/kindconfig_test.go`, `internal/cluster/hosts_test.go`, `internal/cluster/testdata/kind-rootless.golden.yaml`, `internal/cluster/testdata/kind-rootful.golden.yaml`, `internal/cluster/testdata/hosts-test.golden.toml`

**Interfaces:**
- Consumes: `type Condition struct { Code Code; Text string; At time.Time }` (Task 12); the settings that `docs/proofs/p08-kind-library.md`, `docs/proofs/p01-registry-order.md` and `docs/proofs/p12-keys.md` record (Tasks 4 to 6).
- Produces: `module internal/cluster`
  A run's kind cluster and its registry. It checks the runner, creates
  both, makes the nodes pull through the run's registry, waits until the
  cluster is ready, creates the two namespaces, and deletes all of it. It
  hides kind, the container engine and the nodes' registry settings.
- Produces: `const ProbeNamespace = "rollproof-probe"`
  The namespace of every probe pod.
- Produces: `const TestHost = "rollproof.test"`
  The registry host, known only inside a run, under which the tool stores
  images that name no host of their own.
- Produces: `func Name(configPath, name string) (string, error)`
  The cluster's name. With name it is rollproof-NAME; name may hold lower
  case letters, digits, dots and hyphens. Without, it is rollproof- and ten
  hexadecimal digits that depend only on the file's real absolute path.
- Produces: `func RunDir(clusterName string) string`
  The run's folder: one under the system's temporary directory that
  carries the cluster's name. It is not created here.
- Produces: `type Spec struct { Name, RunDir, NodeImage, RegistryImage string; Workers int; Namespace string; Timeout time.Duration; Keep bool }`
  What Create needs. An empty NodeImage or RegistryImage means the default.
  Namespace is the workload's namespace. Timeout bounds the wait until the
  cluster is ready. Keep leaves a cluster that failed to come up in place.
- Produces: `func KindConfig(s Spec, rootless bool) *v1alpha4.Cluster`
  The kind description of the run's cluster: one control-plane node and
  s.Workers workers, a zone label on each worker, and the runtime settings
  the nodes need. rootless adds the setting for an engine without root.
- Produces: `func HostsTOML(runRegistry string) string`
  The registry settings of a node for TestHost: the run's registry, at its
  address on the cluster's network, may serve a pull and may not resolve a
  tag.

- [ ] **Step 1: Write the failing tests for the names**

`internal/cluster/names_test.go`:

```go
package cluster

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rollproof/internal/verdict"
)

func TestContractNames(t *testing.T) {
	if ProbeNamespace != "rollproof-probe" {
		t.Errorf("ProbeNamespace = %q", ProbeNamespace)
	}
	if TestHost != "rollproof.test" {
		t.Errorf("TestHost = %q", TestHost)
	}
}

// writeConfig creates dir/rollproof.yaml and returns its path.
func writeConfig(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "rollproof.yaml")
	if err := os.WriteFile(p, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNameFromPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := writeConfig(t, dir)
	real, err := filepath.EvalSymlinks(file)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(real))
	want := "rollproof-" + hex.EncodeToString(sum[:])[:10]

	link := filepath.Join(t.TempDir(), "link.yaml")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	for _, p := range []string{file, link, "rollproof.yaml", filepath.Join(dir, "sub", "..", "rollproof.yaml")} {
		got, err := Name(p, "")
		if err != nil {
			t.Fatalf("Name(%q): %v", p, err)
		}
		if got != want {
			t.Errorf("Name(%q) = %q, want %q", p, got, want)
		}
	}

	other := writeConfig(t, t.TempDir())
	got, err := Name(other, "")
	if err != nil {
		t.Fatal(err)
	}
	if got == want {
		t.Errorf("two files gave the same name %q", got)
	}
}

func TestNameGiven(t *testing.T) {
	file := writeConfig(t, t.TempDir())
	for _, n := range []string{"pr-12.a", "a", strings.Repeat("a", 40)} {
		got, err := Name(file, n)
		if err != nil {
			t.Errorf("Name(file, %q): %v", n, err)
			continue
		}
		if got != "rollproof-"+n {
			t.Errorf("Name(file, %q) = %q", n, got)
		}
	}
	for _, n := range []string{"Upper", "has space", "under_score", strings.Repeat("a", 41), "-lead", "trail-", ".lead", "trail."} {
		_, err := Name(file, n)
		if err == nil {
			t.Errorf("Name(file, %q) gave no error", n)
			continue
		}
		if c := verdict.AsCondition(err); c.Code != verdict.ConfigRejected || !strings.Contains(c.Text, "--name") {
			t.Errorf("Name(file, %q): %v, want config-rejected naming --name", n, err)
		}
	}
}

func TestNameMissingFile(t *testing.T) {
	_, err := Name(filepath.Join(t.TempDir(), "absent.yaml"), "")
	if err == nil {
		t.Fatal("no error for a file that does not exist")
	}
	if c := verdict.AsCondition(err); c.Code != verdict.ConfigRejected {
		t.Errorf("code = %s, want config-rejected", c.Code)
	}
}

func TestRunDir(t *testing.T) {
	if got, want := RunDir("rollproof-pr-12.a"), filepath.Join(os.TempDir(), "rollproof-pr-12.a"); got != want {
		t.Errorf("RunDir = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cluster/ -run 'TestContractNames|TestName|TestRunDir' -v`
Expected: FAIL, with `undefined: ProbeNamespace` and `undefined: Name` among the compile errors.

- [ ] **Step 3: Implement the names in `internal/cluster/names.go`**

The file opens with the package comment, which is the sentence of `module internal/cluster` in the Interfaces block. It holds `const ProbeNamespace = "rollproof-probe"`, `const TestHost = "rollproof.test"`, `func Name(configPath, name string) (string, error)` and `func RunDir(clusterName string) string`, each with its interface comment from the Interfaces block. `RunDir` is `filepath.Join(os.TempDir(), clusterName)`. `Name` follows these rules:

1. A name that is not empty must match `^[a-z0-9]([a-z0-9.-]{0,38}[a-z0-9])?$`: 1 to 40 characters, so that `rollproof-NAME-control-plane` stays within the 64 characters of a Linux host name. Otherwise the error is `verdict.Condition{Code: verdict.ConfigRejected, Text: "--name may hold 1 to 40 lower-case letters, digits, dots and hyphens, and must start and end with a letter or a digit"}`. The result is `"rollproof-" + name`, and the path is not read.
2. With an empty name: `filepath.Abs`, then `filepath.EvalSymlinks`. An error of either is a ConfigRejected condition whose text is "the configuration file's path cannot be resolved: " and the error.
3. The result is `"rollproof-"` and the first 10 hexadecimal digits of the SHA-256 of the real path's bytes.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cluster/ -run 'TestContractNames|TestName|TestRunDir' -v`
Expected: PASS

- [ ] **Step 5: Add kind as a dependency**

Run: `go get sigs.k8s.io/kind@v0.33.0`
Expected: `go.mod` requires `sigs.k8s.io/kind v0.33.0`.

- [ ] **Step 6: Write the failing golden tests, and the golden files**

The tests encode the description with `go.yaml.in/yaml/v3`: kind's own loader reads its configuration with that library (`pkg/internal/apis/config/encoding/load.go` at v0.33.0). The library honours the `yaml` tags of kind's types, so empty fields are left out. The tests also read the text back strictly, as kind does, to show that kind accepts it.

`internal/cluster/kindconfig_test.go`:

```go
package cluster

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	yaml "go.yaml.in/yaml/v3"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// golden compares got with testdata/file byte for byte, and rewrites the file first with -update.
func golden(t *testing.T, file string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", file)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs (-want +got):\n%s", file, cmp.Diff(string(want), string(got)))
	}
}

// encodeKind writes the description as kind reads it: go.yaml.in/yaml/v3, the library kind's
// own config loader uses, indented by two spaces.
func encodeKind(t *testing.T, c *v1alpha4.Cluster) []byte {
	t.Helper()
	var b bytes.Buffer
	e := yaml.NewEncoder(&b)
	e.SetIndent(2)
	if err := e.Encode(c); err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestKindConfigGolden(t *testing.T) {
	for _, tc := range []struct {
		file     string
		rootless bool
	}{
		{"kind-rootless.golden.yaml", true},
		{"kind-rootful.golden.yaml", false},
	} {
		t.Run(tc.file, func(t *testing.T) {
			c := KindConfig(Spec{Name: "rollproof-x", Workers: 3}, tc.rootless)
			out := encodeKind(t, c)
			golden(t, tc.file, out)

			// kind's own loader decodes the file strictly; it must read back the same description.
			var back v1alpha4.Cluster
			d := yaml.NewDecoder(bytes.NewReader(out))
			d.KnownFields(true)
			if err := d.Decode(&back); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c, &back); diff != "" {
				t.Errorf("read back (-want +got):\n%s", diff)
			}
		})
	}
}

func TestKindConfigShape(t *testing.T) {
	c := KindConfig(Spec{Name: "rollproof-x", Workers: 1}, false)
	if c.Name != "" {
		t.Errorf("Name = %q, want it unset", c.Name)
	}
	want := []v1alpha4.Node{
		{Role: v1alpha4.ControlPlaneRole},
		{Role: v1alpha4.WorkerRole, Labels: map[string]string{"topology.kubernetes.io/zone": "rollproof-zone-1"}},
	}
	if diff := cmp.Diff(want, c.Nodes); diff != "" {
		t.Errorf("nodes (-want +got):\n%s", diff)
	}

	const image = "registry.example/kindest/node:v1.36.1@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	c = KindConfig(Spec{Name: "rollproof-x", Workers: 3, NodeImage: image}, false)
	if len(c.Nodes) != 4 {
		t.Fatalf("%d nodes, want 4", len(c.Nodes))
	}
	for i, n := range c.Nodes {
		if n.Image != image {
			t.Errorf("node %d image = %q, want %q", i, n.Image, image)
		}
	}
}
```

`internal/cluster/hosts_test.go`:

```go
package cluster

import "testing"

func TestHostsTOMLGolden(t *testing.T) {
	golden(t, "hosts-test.golden.toml", []byte(HostsTOML("10.89.0.5:5000")))
}
```

`internal/cluster/testdata/kind-rootless.golden.yaml`, ending with one newline:

```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
  - role: worker
    labels:
      topology.kubernetes.io/zone: rollproof-zone-1
  - role: worker
    labels:
      topology.kubernetes.io/zone: rollproof-zone-2
  - role: worker
    labels:
      topology.kubernetes.io/zone: rollproof-zone-3
containerdConfigPatches:
  - |-
    [plugins."io.containerd.grpc.v1.cri".registry]
      config_path = "/etc/containerd/certs.d"
  - |-
    [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc.options]
      NoNewKeyring = true
```

`internal/cluster/testdata/kind-rootful.golden.yaml`, ending with one newline:

```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
  - role: worker
    labels:
      topology.kubernetes.io/zone: rollproof-zone-1
  - role: worker
    labels:
      topology.kubernetes.io/zone: rollproof-zone-2
  - role: worker
    labels:
      topology.kubernetes.io/zone: rollproof-zone-3
containerdConfigPatches:
  - |-
    [plugins."io.containerd.grpc.v1.cri".registry]
      config_path = "/etc/containerd/certs.d"
```

`internal/cluster/testdata/hosts-test.golden.toml`, two lines, each ending with a newline, and nothing else (no `server` line: `rollproof.test` has no real host):

```toml
[host."http://10.89.0.5:5000"]
  capabilities = ["pull"]
```

- [ ] **Step 7: Run the tests to verify they fail**

Run: `go test ./internal/cluster/ -run 'TestKindConfig|TestHostsTOML' -v`
Expected: FAIL, with `undefined: KindConfig` and `undefined: HostsTOML` among the compile errors.

- [ ] **Step 8: Implement `Spec` and `KindConfig` in `internal/cluster/kindconfig.go`, and `HostsTOML` in `internal/cluster/hosts.go`**

`type Spec struct { Name, RunDir, NodeImage, RegistryImage string; Workers int; Namespace string; Timeout time.Duration; Keep bool }`, `func KindConfig(s Spec, rootless bool) *v1alpha4.Cluster` and `func HostsTOML(runRegistry string) string`, each with its interface comment from the Interfaces block. The type is `sigs.k8s.io/kind/pkg/apis/config/v1alpha4.Cluster`. `KindConfig` reads neither `Keep` nor the fields that only `Create` uses.

`KindConfig` sets `TypeMeta{Kind: "Cluster", APIVersion: "kind.x-k8s.io/v1alpha4"}` and leaves `Name` unset. Its nodes are one `v1alpha4.ControlPlaneRole` node with no labels, then `s.Workers` nodes of `v1alpha4.WorkerRole`, worker i (from 1) with the one label `topology.kubernetes.io/zone: rollproof-zone-i`. Every node's `Image` is `s.NodeImage`; empty leaves kind's default. `ContainerdConfigPatches` holds the first of these two texts, and the second too when `rootless` is true. Write them as two unexported constants:

```go
const (
	registryPatch = "[plugins.\"io.containerd.grpc.v1.cri\".registry]\n  config_path = \"/etc/containerd/certs.d\""
	keyringPatch  = "[plugins.\"io.containerd.grpc.v1.cri\".containerd.runtimes.runc.options]\n  NoNewKeyring = true"
)
```

The plugin name `io.containerd.grpc.v1.cri` is the one in the node image's containerd configuration, which is of version 2. `HostsTOML` returns `[host."http://` + runRegistry + `"]`, a newline, `  capabilities = ["pull"]` and a newline.

These values are what the plan assumed. Check them against the records before the next step:
- `docs/proofs/p08-kind-library.md`: the `config_path` patch and the zone labels that worked.
- `docs/proofs/p01-registry-order.md`: the form of the hosts file (assumption 1).
- `docs/proofs/p12-keys.md`: if its last line is `Holds: no`, `KindConfig` ignores `rootless`, and `kind-rootless.golden.yaml` becomes the same text as the rootful file.

Where a record differs, the record wins: change the constant, rewrite the golden files with `go test ./internal/cluster/ -run 'TestKindConfigGolden|TestHostsTOMLGolden' -update`, read the new files, and add one sentence to the commit message that names the record and what changed.

- [ ] **Step 9: Run the tests to verify they pass**

Run: `go test ./internal/cluster/ -v`
Expected: PASS

Run: `go mod tidy`
Run: `go vet ./internal/cluster/`
Expected: no output.

- [ ] **Step 10: Commit**

```bash
git add go.mod go.sum internal/cluster
git commit -m "Name a run's cluster, and describe it and its node settings for kind"
```

### Task 21: `cluster`: the runner checks, create, ready, registry, namespaces, delete

**Files:**
- Create: `internal/cluster/runner.go`, `internal/cluster/engine.go`, `internal/cluster/cluster.go`, `internal/cluster/registry.go`
- Test: `internal/cluster/runner_test.go`, `internal/cluster/cluster_test.go` (build tag `cluster`)

**Interfaces:**
- Consumes: `func Name(configPath, name string) (string, error)`, `func RunDir(clusterName string) string`, `type Spec struct { … }`, `func KindConfig(s Spec, rootless bool) *v1alpha4.Cluster`, `func HostsTOML(runRegistry string) string`, `const ProbeNamespace` (Task 20); `type Condition` with RunnerUnsupported and ClusterFailed (Task 12).
- Produces: `type Engine struct { Binary string; Rootless bool }`
  The container engine the run uses: its command name, as kind's library
  would pick it, and whether it runs without root.
- Produces: `func CheckRunner(ctx context.Context) (Engine, error)`
  Checks the runner before anything is created: the engine, that it is on
  this machine, that the tool is not inside a container, and that the
  tool's own binary is static. An error is a Condition with
  RunnerUnsupported that names what is missing.
- Produces: `func DefaultNodeImage() string`
  The node image of the linked kind release, with its digest.
- Produces: `const DefaultRegistryImage = "docker.io/library/registry:3@sha256:…"`
  The registry image the tool starts when the configuration names none,
  pinned by digest.
- Produces: `func KindVersion() string`
  The version of the kind release the tool links.
- Produces: `type Cluster struct { Name, RunDir, Kubeconfig, Context string; Engine Engine; PushRegistry, NodeRegistry string; KubernetesVersion, NodeImage string; Workers int }`
  A run's cluster, ready. Kubeconfig is its own file and Context that
  file's one context. PushRegistry is the run's registry as the runner
  reaches it; NodeRegistry is the same registry as the nodes reach it.
- Produces: `func Create(ctx context.Context, s Spec, e Engine) (*Cluster, error)`
  Removes whatever an earlier run of this name left, then creates the
  cluster and its registry and returns when every node is ready, DNS runs
  and both namespaces exist. On an error nothing of the run is left
  behind, unless s.Keep is set or ctx has ended. An error is a Condition
  with ClusterFailed.
- Produces: `func (c *Cluster) Client() (kubernetes.Interface, error)`
  A client for the cluster's API server.
- Produces: `func (c *Cluster) Nodes() []string`
  The node names, the control-plane node first.
- Produces: `func (c *Cluster) Exec(ctx context.Context, node string, args ...string) ([]byte, error)`
  Runs a command in a node and returns its output, standard error
  included. A non-zero exit is an error that carries the output.
- Produces: `func (c *Cluster) Delete(ctx context.Context) error`
  Deletes the cluster, the registry with its volumes, and the run's
  folder. Deleting what is already gone is not an error.

The tests with the build tag `cluster` run with `env CGO_ENABLED=0`: `Create` is reached after `CheckRunner`, which checks that the running binary is static, and a test binary that links the `net` package with cgo is not.

- [ ] **Step 1: Add the dependencies**

Run: `go get k8s.io/client-go@v0.37 k8s.io/api@v0.37 k8s.io/apimachinery@v0.37`
Run: `go get github.com/google/go-containerregistry@latest`
Expected: `go.mod` requires the three `k8s.io` modules at a v0.37 release, and `github.com/google/go-containerregistry`. A test of this task pushes an image with it; Task 22 uses it in code.

- [ ] **Step 2: Write the failing unit tests for the runner checks**

`internal/cluster/runner_test.go`. The nerdctl text below was read on this machine on 2026-11-16 with `nerdctl info --format '{{json .SecurityOptions}}'`; read it again and replace it if it differs.

```go
//go:build linux

package cluster

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"testing"

	"sigs.k8s.io/kind/pkg/apis/config/defaults"

	"rollproof/internal/verdict"
)

// Recorded on 2026-11-16: `nerdctl info --format "{{json .}}"` on the developer's machine (rootless
// nerdctl 2.4.2), and the form Docker gives on a host where it runs as root. Fields that the tool
// does not read are left out.
const (
	nerdctlInfo = `{"SecurityOptions":["name=seccomp,profile=builtin","name=cgroupns","name=rootless"]}`
	dockerInfo  = `{"SecurityOptions":["name=apparmor","name=seccomp,profile=builtin","name=cgroupns"]}`
)

// The first line that `NAME -v` prints for each engine.
const (
	dockerV  = "Docker version 28.5.1, build e180ab8"
	nerdctlV = "nerdctl version 2.4.2"
	podmanV  = "podman version 5.6.2"
)

// machine is what the fake probes of CheckRunner report.
type machine struct {
	env      map[string]string
	files    []string          // paths that exist
	versions map[string]string // engine binary -> what `BINARY -v` prints; absent means it fails
	infos    map[string]string // engine binary -> output of its info command; absent means it fails
	static   bool
}

// fake installs m in the package's probe variables until the test ends.
func fake(t *testing.T, m machine) {
	t.Helper()
	oldEnv, oldExists, oldVersion, oldInfo, oldExe, oldStatic := lookupEnv, fileExists, engineVersion, engineInfo, executablePath, binaryIsStatic
	t.Cleanup(func() {
		lookupEnv, fileExists, engineVersion, engineInfo, executablePath, binaryIsStatic = oldEnv, oldExists, oldVersion, oldInfo, oldExe, oldStatic
	})
	lookupEnv = func(name string) string { return m.env[name] }
	fileExists = func(path string) bool {
		for _, f := range m.files {
			if f == path {
				return true
			}
		}
		return false
	}
	engineVersion = func(_ context.Context, binary string) ([]byte, error) {
		out, ok := m.versions[binary]
		if !ok {
			return nil, errors.New("executable file not found in $PATH")
		}
		return []byte(out + "\n"), nil
	}
	engineInfo = func(_ context.Context, binary string) ([]byte, error) {
		out, ok := m.infos[binary]
		if !ok {
			return nil, errors.New("exit status 1")
		}
		return []byte(out), nil
	}
	executablePath = func() (string, error) { return "/usr/local/bin/rollproof", nil }
	binaryIsStatic = func(string) (bool, error) { return m.static, nil }
}

func TestCheckRunner(t *testing.T) {
	provider := func(name string) map[string]string { return map[string]string{"KIND_EXPERIMENTAL_PROVIDER": name} }
	for _, tc := range []struct {
		name string
		m    machine
		want Engine
		text string // the RunnerUnsupported text; empty when CheckRunner succeeds
	}{
		{"nerdctl, rootless", machine{versions: map[string]string{"nerdctl": nerdctlV},
			infos: map[string]string{"nerdctl": nerdctlInfo}, static: true},
			Engine{Binary: "nerdctl", Rootless: true}, ""},
		{"docker before nerdctl", machine{versions: map[string]string{"docker": dockerV, "nerdctl": nerdctlV},
			infos: map[string]string{"docker": dockerInfo, "nerdctl": nerdctlInfo}, static: true},
			Engine{Binary: "docker", Rootless: false}, ""},
		{"podman last", machine{versions: map[string]string{"podman": podmanV},
			infos: map[string]string{"podman": `{}`}, static: true},
			Engine{Binary: "podman"}, ""},
		{"a docker command that is podman does not count as docker", machine{
			versions: map[string]string{"docker": podmanV, "podman": podmanV},
			infos:    map[string]string{"docker": `{}`, "podman": `{}`}, static: true},
			Engine{Binary: "podman"}, ""},
		{"the first engine found has no working info", machine{versions: map[string]string{"docker": dockerV, "nerdctl": nerdctlV},
			infos: map[string]string{"nerdctl": nerdctlInfo}, static: true},
			Engine{}, "no container engine that kind supports"},
		{"the variable picks podman", machine{env: provider("podman"), versions: map[string]string{"docker": dockerV},
			infos: map[string]string{"docker": dockerInfo, "podman": `{}`}, static: true},
			Engine{Binary: "podman"}, ""},
		{"the variable picks finch", machine{env: provider("finch"),
			infos: map[string]string{"finch": nerdctlInfo}, static: true},
			Engine{Binary: "finch", Rootless: true}, ""},
		{"the variable picks nerdctl.lima", machine{env: provider("nerdctl.lima"),
			infos: map[string]string{"nerdctl.lima": nerdctlInfo}, static: true},
			Engine{Binary: "nerdctl.lima", Rootless: true}, ""},
		{"the variable picks nerdctl and its info fails", machine{env: provider("nerdctl"),
			versions: map[string]string{"docker": dockerV}, infos: map[string]string{"docker": dockerInfo}, static: true},
			Engine{}, "no container engine that kind supports"},
		{"an unknown value is ignored", machine{env: provider("something"), versions: map[string]string{"nerdctl": nerdctlV},
			infos: map[string]string{"nerdctl": nerdctlInfo}, static: true},
			Engine{Binary: "nerdctl", Rootless: true}, ""},
		{"no engine", machine{static: true},
			Engine{}, "no container engine that kind supports"},
		{"info that is not JSON counts as failed", machine{versions: map[string]string{"docker": dockerV},
			infos: map[string]string{"docker": "Client: Docker Engine"}, static: true},
			Engine{}, "no container engine that kind supports"},
		{"DOCKER_HOST over tcp", machine{env: map[string]string{"DOCKER_HOST": "tcp://10.0.0.1:2375"},
			versions: map[string]string{"docker": dockerV}, infos: map[string]string{"docker": dockerInfo}, static: true},
			Engine{}, "the container engine is on another machine"},
		{"CONTAINER_HOST over ssh", machine{env: map[string]string{"CONTAINER_HOST": "ssh://user@build.example/run/podman/podman.sock"},
			versions: map[string]string{"podman": podmanV}, infos: map[string]string{"podman": `{}`}, static: true},
			Engine{}, "the container engine is on another machine"},
		{"DOCKER_HOST on a local socket", machine{env: map[string]string{"DOCKER_HOST": "unix:///run/user/1000/docker.sock"},
			versions: map[string]string{"docker": dockerV}, infos: map[string]string{"docker": dockerInfo}, static: true},
			Engine{Binary: "docker"}, ""},
		{"inside Docker", machine{files: []string{"/.dockerenv"},
			versions: map[string]string{"docker": dockerV}, infos: map[string]string{"docker": dockerInfo}, static: true},
			Engine{}, "the tool runs inside a container"},
		{"inside Podman", machine{files: []string{"/run/.containerenv"},
			versions: map[string]string{"podman": podmanV}, infos: map[string]string{"podman": `{}`}, static: true},
			Engine{}, "the tool runs inside a container"},
		{"a binary that is not static", machine{versions: map[string]string{"docker": dockerV},
			infos: map[string]string{"docker": dockerInfo}, static: false},
			Engine{}, "the tool's binary is not statically linked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake(t, tc.m)
			got, err := CheckRunner(context.Background())
			if tc.text == "" {
				if err != nil {
					t.Fatalf("CheckRunner: %v", err)
				}
				if got != tc.want {
					t.Errorf("engine = %+v, want %+v", got, tc.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("CheckRunner gave %+v and no error, want %q", got, tc.text)
			}
			if c := verdict.AsCondition(err); c.Code != verdict.RunnerUnsupported || c.Text != tc.text {
				t.Errorf("error = %v, want runner-unsupported: %s", err, tc.text)
			}
		})
	}
}

func TestNetwork(t *testing.T) {
	both := map[string]string{"KIND_EXPERIMENTAL_DOCKER_NETWORK": "docker-net", "KIND_EXPERIMENTAL_PODMAN_NETWORK": "podman-net"}
	for _, tc := range []struct {
		binary string
		env    map[string]string
		want   string
	}{
		{"docker", both, "docker-net"},
		{"podman", both, "podman-net"},
		{"nerdctl", both, "kind"},
	} {
		t.Run(tc.binary, func(t *testing.T) {
			fake(t, machine{env: tc.env})
			e := Engine{Binary: tc.binary}
			if got := e.network(); got != tc.want {
				t.Errorf("network() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRootlessFromInfo(t *testing.T) {
	for _, tc := range []struct {
		name, info string
		want       bool
	}{
		{"nerdctl rootless", nerdctlInfo, true},
		{"docker as root", dockerInfo, false},
		{"docker rootless", `{"SecurityOptions":["name=seccomp,profile=builtin","name=rootless","name=cgroupns"]}`, true},
		{"podman rootless", `{"host":{"security":{"rootless":true,"seccompEnabled":true}}}`, true},
		{"no security options", `{}`, false},
	} {
		got, err := rootlessFromInfo([]byte(tc.info))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: rootless = %v, want %v", tc.name, got, tc.want)
		}
	}
	if _, err := rootlessFromInfo([]byte("Client: Docker Engine")); err == nil {
		t.Error("no error for output that is not JSON")
	}
}

// build compiles a one-file main package with CGO_ENABLED set to cgo and returns the binary's path.
func build(t *testing.T, src, cgo string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module probe\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "probe")
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED="+cgo)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, b)
	}
	return out
}

func TestIsStatic(t *testing.T) {
	static := build(t, "package main\n\nfunc main() {}\n", "0")
	if ok, err := isStatic(static); err != nil || !ok {
		t.Errorf("isStatic(static) = %v, %v; want true", ok, err)
	}

	cc, err := exec.Command("go", "env", "CC").Output()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath(strings.TrimSpace(string(cc))); err != nil {
		t.Skip("no C compiler: the dynamically linked case is not tested")
	}
	dynamic := build(t, "package main\n\nimport \"net\"\n\nfunc main() { _, _ = net.LookupHost(\"localhost\") }\n", "1")
	if ok, err := isStatic(dynamic); err != nil || ok {
		t.Errorf("isStatic(dynamic) = %v, %v; want false", ok, err)
	}

	text := filepath.Join(t.TempDir(), "not-elf")
	if err := os.WriteFile(text, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := isStatic(text); err == nil {
		t.Error("isStatic of a file that is not ELF gave no error")
	}
}

func TestDefaults(t *testing.T) {
	if got := DefaultNodeImage(); got != defaults.Image || !strings.Contains(got, "@sha256:") {
		t.Errorf("DefaultNodeImage = %q, want kind's %q", got, defaults.Image)
	}
	if !regexp.MustCompile(`^docker\.io/library/registry:3@sha256:[0-9a-f]{64}$`).MatchString(DefaultRegistryImage) {
		t.Errorf("DefaultRegistryImage = %q, not pinned by digest", DefaultRegistryImage)
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Skip("no build information in this binary")
	}
	for _, d := range info.Deps {
		if d.Path == "sigs.k8s.io/kind" {
			if got := KindVersion(); got != d.Version || got == "" {
				t.Errorf("KindVersion = %q, want %q", got, d.Version)
			}
			return
		}
	}
	t.Skip("sigs.k8s.io/kind is not listed in this binary's build information")
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/cluster/ -run 'TestCheckRunner|TestNetwork|TestRootlessFromInfo|TestIsStatic|TestDefaults' -v`
Expected: FAIL, with `undefined: lookupEnv`, `undefined: engineVersion` and `undefined: CheckRunner` among the compile errors.

- [ ] **Step 4: Implement the runner checks in `internal/cluster/runner.go`**

The file starts with `//go:build linux`, as do `engine.go`, `cluster.go` and `registry.go`: the build tag is the check that the runner is Linux. It holds `type Engine struct { Binary string; Rootless bool }` and `func CheckRunner(ctx context.Context) (Engine, error)`, each with its interface comment from the Interfaces block, and these unexported names:

- Function variables that the test replaces, each set to the real probe: `lookupEnv func(name string) string` (`os.Getenv`); `fileExists func(path string) bool` (`os.Stat` succeeds); `engineVersion func(ctx context.Context, binary string) ([]byte, error)`, which runs `BINARY -v` and returns its standard output, an error when it does not exit 0; `engineInfo func(ctx context.Context, binary string) ([]byte, error)`, which runs `BINARY info --format "{{json .}}"` (the last argument is the literal text `{{json .}}`) and returns its standard output; `executablePath func() (string, error)` (`os.Executable`); `binaryIsStatic func(path string) (bool, error)` (`isStatic`).
- `func rootlessFromInfo(info []byte) (bool, error)`: decodes the JSON text; true when an entry of `SecurityOptions` contains `rootless` (the form of Docker and nerdctl), or when `host.security.rootless` is true (the form of Podman); an error when the text is not JSON.
- `func isStatic(path string) (bool, error)`: opens the file with `debug/elf`; false when a program header has the type `elf.PT_INTERP`; an error when the file is not ELF.

`CheckRunner` makes these checks in this order, and the first that fails gives `verdict.Condition{Code: verdict.RunnerUnsupported, Text: …}` with the test's text:

1. The engine, chosen as kind chooses it, so that the name and kind's provider cannot disagree. When `KIND_EXPERIMENTAL_PROVIDER` is `docker`, `podman`, `nerdctl`, `finch` or `nerdctl.lima`, that is the name, as kind's own command line takes it (`pkg/internal/runtime/runtime.go` at v0.33.0). Otherwise the name is the first of `docker`, `nerdctl` and `podman` whose `engineVersion` succeeds and whose first line starts with `Docker version`, `nerdctl version` or `podman version` respectively: the test of `IsAvailable` in kind's three providers. Then `engineInfo` of that name must succeed and `rootlessFromInfo` must read its output; `Rootless` is what it says. No name, or a failed `info`, gives "no container engine that kind supports".
2. `DOCKER_HOST` or `CONTAINER_HOST` that starts with `tcp://` or `ssh://`.
3. `/.dockerenv` or `/run/.containerenv` exists.
4. `binaryIsStatic(executablePath())`. An error of either is RunnerUnsupported with the text "the tool's binary cannot be read: " and the error.

In `internal/cluster/engine.go`, which `TestNetwork` needs now: `func (e Engine) network() string`, the name of the engine's network for kind, read through `lookupEnv` as kind's providers read it: for `docker`, `KIND_EXPERIMENTAL_DOCKER_NETWORK`, or `"kind"` when it is not set; for `podman`, `KIND_EXPERIMENTAL_PODMAN_NETWORK`, or `"kind"`; for every other engine (nerdctl's provider), `"kind"`.

- [ ] **Step 5: Implement the defaults**

In `internal/cluster/cluster.go`: `func DefaultNodeImage() string`, which returns `defaults.Image` of `sigs.k8s.io/kind/pkg/apis/config/defaults`, and `func KindVersion() string`, which returns the `Version` of the entry `sigs.k8s.io/kind` among the `Deps` of `debug.ReadBuildInfo()`, and `"unknown"` when there is none. Each has its interface comment from the Interfaces block.

In `internal/cluster/registry.go`: `const DefaultRegistryImage` with its interface comment. Find the digest once:

Run: `go run github.com/google/go-containerregistry/cmd/crane@latest digest docker.io/library/registry:3`
Expected: one line, `sha256:` and 64 hexadecimal digits.

The constant is `"docker.io/library/registry:3@"` followed by that line. Its value is the command's output on the day this step is done; a comment above the constant gives that date and the command.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/cluster/ -v`
Expected: PASS. On a machine with no C compiler, `TestIsStatic` ends as SKIP after the static case has passed.

- [ ] **Step 7: Write the failing tests with a cluster**

`internal/cluster/cluster_test.go`:

```go
//go:build linux && cluster

package cluster

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"

	"rollproof/internal/verdict"
)

// testSpec is a cluster with the given workers, named rollproof-test-SUFFIX.
func testSpec(suffix string, workers int) Spec {
	n := "rollproof-test-" + suffix
	return Spec{Name: n, RunDir: RunDir(n), Workers: workers, Namespace: "app", Timeout: 5 * time.Minute}
}

// testEngine is this machine's engine, found as run finds it.
func testEngine(t *testing.T) Engine {
	t.Helper()
	e, err := CheckRunner(context.Background())
	if err != nil {
		t.Fatalf("CheckRunner: %v", err)
	}
	return e
}

// engine runs the engine's command line on the runner and returns its combined output, trimmed.
func engine(t *testing.T, e Engine, args ...string) (string, error) {
	t.Helper()
	out, err := exec.Command(e.Binary, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// fileHash is the SHA-256 of the file at path, or "absent".
func fileHash(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "absent"
	}
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

// pushTestImage pushes a one-layer image by digest to the registry at push, under repository,
// and returns the digest.
func pushTestImage(t *testing.T, push, repository string) string {
	t.Helper()
	layer, err := random.Layer(512, types.DockerLayer)
	if err != nil {
		t.Fatal(err)
	}
	img, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	cfg = cfg.DeepCopy()
	cfg.OS, cfg.Architecture = "linux", runtime.GOARCH
	if img, err = mutate.ConfigFile(img, cfg); err != nil {
		t.Fatal(err)
	}
	d, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}
	ref, err := name.NewDigest(push+"/"+repository+"@"+d.String(), name.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(ref, img); err != nil {
		t.Fatal(err)
	}
	return d.String()
}

func TestCreateAndDelete(t *testing.T) {
	ctx := context.Background()
	e := testEngine(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	callers := filepath.Join(home, ".kube", "config")
	before := fileHash(t, callers)

	s := testSpec("create", 3)
	c, err := Create(ctx, s, e)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer c.Delete(ctx)

	nodes := c.Nodes()
	if len(nodes) != 4 || nodes[0] != s.Name+"-control-plane" {
		t.Fatalf("Nodes() = %v, want 4 names with %s-control-plane first", nodes, s.Name)
	}
	client, err := c.Client()
	if err != nil {
		t.Fatal(err)
	}
	var zones []string
	for i, n := range nodes {
		node, err := client.CoreV1().Nodes().Get(ctx, n, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		ready := false
		for _, cond := range node.Status.Conditions {
			ready = ready || cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue
		}
		if !ready {
			t.Errorf("node %s is not ready", n)
		}
		zone, has := node.Labels["topology.kubernetes.io/zone"]
		if i == 0 && has {
			t.Errorf("the control-plane node has the zone %q", zone)
		}
		if i > 0 {
			zones = append(zones, zone)
		}
	}
	slices.Sort(zones)
	if want := []string{"rollproof-zone-1", "rollproof-zone-2", "rollproof-zone-3"}; !slices.Equal(zones, want) {
		t.Errorf("worker zones %v, want %v", zones, want)
	}
	dns, err := client.AppsV1().Deployments("kube-system").Get(ctx, "coredns", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if dns.Status.AvailableReplicas < *dns.Spec.Replicas {
		t.Errorf("coredns has %d of %d replicas available", dns.Status.AvailableReplicas, *dns.Spec.Replicas)
	}
	for _, ns := range []string{ProbeNamespace, "app"} {
		if _, err := client.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{}); err != nil {
			t.Errorf("namespace %s: %v", ns, err)
		}
	}

	kc, err := clientcmd.LoadFromFile(c.Kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	if len(kc.Contexts) != 1 || kc.CurrentContext != c.Context || kc.Contexts[c.Context].Namespace != "app" {
		t.Errorf("kubeconfig: current %q, %d contexts; want the one context %q with namespace app",
			kc.CurrentContext, len(kc.Contexts), c.Context)
	}
	if fi, err := os.Stat(c.Kubeconfig); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("the kubeconfig's mode: %v; want 0600", err)
	}
	if fi, err := os.Stat(c.RunDir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("the run folder's mode: %v; want 0700", err)
	}
	if c.NodeImage != DefaultNodeImage() || !strings.HasPrefix(c.KubernetesVersion, "v1.") {
		t.Errorf("NodeImage %q, KubernetesVersion %q", c.NodeImage, c.KubernetesVersion)
	}

	digest := pushTestImage(t, c.PushRegistry, "probe/x")
	if out, err := c.Exec(ctx, nodes[1], "crictl", "pull", TestHost+"/probe/x@"+digest); err != nil {
		t.Errorf("crictl pull in %s: %v\n%s", nodes[1], err, out)
	}
	out, err := c.Exec(ctx, nodes[1], "sh", "-c", "echo to-out; echo to-err >&2")
	if err != nil || !strings.Contains(string(out), "to-out") || !strings.Contains(string(out), "to-err") {
		t.Errorf("Exec = %q, %v; want both streams", out, err)
	}
	if _, err := c.Exec(ctx, nodes[1], "false"); err == nil {
		t.Error("Exec of a command that exits 1 gave no error")
	}

	if got := fileHash(t, callers); got != before {
		t.Errorf("the caller's kubeconfig changed: %s before, %s after", before, got)
	}

	if err := c.Delete(ctx); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	names, err := engine(t, e, "ps", "-a", "--format", "{{.Names}}")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range strings.Fields(names) {
		if strings.HasPrefix(n, s.Name+"-") {
			t.Errorf("container %s is left after Delete", n)
		}
	}
	if _, err := os.Stat(s.RunDir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the run's folder is left after Delete: %v", err)
	}
}

// Review Focus 1: a killed run left a folder and a registry container, and no cluster.
func TestCreateReplacesLeftovers(t *testing.T) {
	ctx := context.Background()
	e := testEngine(t)
	s := testSpec("leftovers", 1)
	if err := os.MkdirAll(s.RunDir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(s.RunDir, "left-by-a-killed-run")
	if err := os.WriteFile(old, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := engine(t, e, "run", "-d", "--name", s.Name+"-registry", DefaultRegistryImage); err != nil {
		t.Fatalf("start a leftover registry: %v\n%s", err, out)
	}

	c, err := Create(ctx, s, e)
	if err != nil {
		t.Fatalf("Create over leftovers: %v", err)
	}
	t.Cleanup(func() { c.Delete(ctx) })
	if _, err := os.Stat(old); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the old file is still there: %v", err)
	}

	created := func() map[string]time.Time {
		m := map[string]time.Time{}
		for _, n := range c.Nodes() {
			out, err := engine(t, e, "inspect", "--format", "{{.Created}}", n)
			if err != nil {
				t.Fatalf("inspect %s: %v\n%s", n, err, out)
			}
			at, err := time.Parse(time.RFC3339Nano, out)
			if err != nil {
				t.Fatalf("the creation time of %s: %v", n, err)
			}
			m[n] = at
		}
		return m
	}
	first := created()
	if c, err = Create(ctx, s, e); err != nil {
		t.Fatalf("Create over a running cluster: %v", err)
	}
	for n, at := range created() {
		if !at.After(first[n]) {
			t.Errorf("node %s was created at %v, not after %v", n, at, first[n])
		}
	}
}

func TestRefusesANameWithoutThePrefix(t *testing.T) {
	ctx := context.Background()
	e := testEngine(t)
	if out, err := engine(t, e, "run", "-d", "--name", "other-registry", DefaultRegistryImage); err != nil {
		t.Fatalf("start other-registry: %v\n%s", err, out)
	}
	t.Cleanup(func() { engine(t, e, "rm", "-f", "-v", "other-registry") })

	_, err := Create(ctx, Spec{Name: "other", RunDir: RunDir("other"), Workers: 1, Namespace: "app", Timeout: time.Minute}, e)
	if c := verdict.AsCondition(err); err == nil || c.Code != verdict.InternalError {
		t.Fatalf("Create(other) = %v, want internal-error", err)
	}
	if out, err := engine(t, e, "inspect", "--format", "{{.State.Running}}", "other-registry"); err != nil || out != "true" {
		t.Errorf("other-registry: running %q, %v; want true", out, err)
	}
}

func TestDeleteTwice(t *testing.T) {
	ctx := context.Background()
	c, err := Create(ctx, testSpec("delete", 1), testEngine(t))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := c.Delete(ctx); err != nil {
		t.Fatalf("first Delete: %v", err)
	}
	if err := c.Delete(ctx); err != nil {
		t.Errorf("second Delete: %v", err)
	}
}

// Keep: a step that fails after kind has made the cluster (a registry image that exists nowhere)
// leaves nothing without Keep, and leaves the nodes and the run's folder with it.
func TestKeepAfterFailure(t *testing.T) {
	ctx := context.Background()
	e := testEngine(t)
	const missing = "rollproof.test/no-such-registry@sha256:0000000000000000000000000000000000000000000000000000000000000000"
	containers := func(name string) []string {
		out, err := engine(t, e, "ps", "-a", "--format", "{{.Names}}")
		if err != nil {
			t.Fatal(err)
		}
		var left []string
		for _, n := range strings.Fields(out) {
			if strings.HasPrefix(n, name+"-") {
				left = append(left, n)
			}
		}
		slices.Sort(left)
		return left
	}

	s := testSpec("nokeep", 1)
	s.RegistryImage = missing
	_, err := Create(ctx, s, e)
	if c := verdict.AsCondition(err); err == nil || c.Code != verdict.ClusterFailed || !strings.HasPrefix(c.Text, "start the registry") {
		t.Fatalf("Create = %v, want cluster-failed in start the registry", err)
	}
	if left := containers(s.Name); len(left) != 0 {
		t.Errorf("containers left without Keep: %v", left)
	}
	if _, err := os.Stat(s.RunDir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the run's folder is left without Keep: %v", err)
	}

	kept := testSpec("keep", 1)
	kept.RegistryImage = missing
	kept.Keep = true
	t.Cleanup(func() { remove(ctx, e, kept.Name, kept.RunDir) })
	_, err = Create(ctx, kept, e)
	if c := verdict.AsCondition(err); err == nil || c.Code != verdict.ClusterFailed || !strings.HasPrefix(c.Text, "start the registry") {
		t.Fatalf("Create with Keep = %v, want cluster-failed in start the registry", err)
	}
	if got, want := containers(kept.Name), []string{kept.Name + "-control-plane", kept.Name + "-worker"}; !slices.Equal(got, want) {
		t.Errorf("containers with Keep: %v, want %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(kept.RunDir, "kubeconfig")); err != nil {
		t.Errorf("the run's folder and its kubeconfig are not kept: %v", err)
	}
}
```

- [ ] **Step 8: Run the tests to verify they fail**

Run: `env CGO_ENABLED=0 go test -tags cluster -p 1 ./internal/cluster/ -v -timeout 20m`
Expected: FAIL, with `undefined: Create` among the compile errors.

- [ ] **Step 9: Implement the engine's command line in `internal/cluster/engine.go`**

More unexported methods of `Engine`, beside `network` of Step 4:

- `func (e Engine) run(ctx context.Context, args ...string) (string, error)`: runs `e.Binary ARGS` on the runner and returns the combined output, trimmed. A failure is an error that holds the command line and the last line of the output.
- `func (e Engine) provider() *kindcluster.Provider`, with `kindcluster` the package `sigs.k8s.io/kind/pkg/cluster`: `kindcluster.NewProvider(kindcluster.ProviderWithLogger(log.NoopLogger{}), OPTION)`, where `log` is `sigs.k8s.io/kind/pkg/log` and OPTION is `kindcluster.ProviderWithDocker()`, `kindcluster.ProviderWithPodman()` for `docker` and `podman`, and `kindcluster.ProviderWithNerdctl(e.Binary)` for `nerdctl`, `finch` and `nerdctl.lima`, as kind's own command line does.
- `func (e Engine) address(ctx context.Context, container, network string) (string, error)`: `ENGINE inspect CONTAINER`, decoded as a JSON list; the first element's `NetworkSettings.Networks[network].IPAddress`. When no key equals `network` and there is exactly one entry, that entry's `IPAddress`: nerdctl may key it otherwise. The `Container` operation of `proofs/internal/lab` (Task 3) records the form that worked on this machine.
- `func (e Engine) removeContainer(ctx context.Context, name string) error`: `ENGINE rm -f -v NAME`. When that fails and `ENGINE inspect NAME` fails too, the container is gone and the result is nil.

- [ ] **Step 10: Implement the run's registry in `internal/cluster/registry.go`**

`func startRegistry(ctx context.Context, e Engine, name, image string, deadline time.Time) (push, node string, err error)` does step 4 of the list in the next step, for the container `NAME-registry`. It waits by a GET of `http://127.0.0.1:PORT/v2/` every 200 ms until one answers 200; past `deadline` it returns an error that says the registry did not answer.

- [ ] **Step 11: Implement `Cluster`, `Create`, `Client`, `Nodes`, `Exec` and `Delete` in `internal/cluster/cluster.go`**

`type Cluster struct { Name, RunDir, Kubeconfig, Context string; Engine Engine; PushRegistry, NodeRegistry string; KubernetesVersion, NodeImage string; Workers int }` and the five functions of the Interfaces block, each with its interface comment from the Interfaces block. `Cluster` also holds, unexported, the kind provider and the nodes (`[]nodes.Node`, from `sigs.k8s.io/kind/pkg/cluster/nodes`) in the order of `Nodes`. `proofs/internal/lab` (Task 3) is a working use of kind's library; read it and do not import it.

`Create`, in this order (each step's failure removes what was made, unless the step says otherwise, and returns ClusterFailed with the step's name in the text):

1. Remove leftovers of the same name: the cluster (provider.Delete(name, kubeconfig)), the registry container NAME-registry (`ENGINE rm -f -v`), the run's folder. The name must start with "rollproof-": refuse anything else with an InternalError condition, before any removal.
2. Make the run's folder with mode 0700.
3. provider.Create(name, CreateWithV1Alpha4Config(KindConfig(s, e.Rootless)), CreateWithKubeconfigPath(RunDir/kubeconfig), CreateWithWaitForReady(0), CreateWithRetain(s.Keep), CreateWithDisplayUsage(false), CreateWithDisplaySalutation(false)). The provider is made with the option that matches e.Binary and a logger that discards.
4. Start the registry: `ENGINE run -d --name NAME-registry --network NETWORK -p 127.0.0.1:PORT:5000 IMAGE`, PORT found by listening on 127.0.0.1:0 and closing. NETWORK is the engine's network for kind, `e.network()`. Read the container's address on that network with `ENGINE inspect`. PushRegistry is "127.0.0.1:PORT", NodeRegistry is "ADDRESS:5000". Wait until GET http://127.0.0.1:PORT/v2/ answers 200.
5. In every node write /etc/containerd/certs.d/rollproof.test/hosts.toml with HostsTOML(NodeRegistry) (mkdir -p, then `cp /dev/stdin FILE` with the text on standard input).
6. Wait, at most s.Timeout from the start of Create, until every node is Ready and the Deployment coredns in kube-system has all its replicas available.
7. Create the namespaces ProbeNamespace and s.Namespace.
8. Set the namespace of the kubeconfig's one context to s.Namespace (clientcmd: load the file, set Contexts[current].Namespace, write it back) and keep the file's mode at 0600.
9. Read the server's version for KubernetesVersion, and the node image reference with its digest for NodeImage (`ENGINE inspect` of the control-plane container, or the image the description named).

How each step is made:

- Step 1, the clean-up after a failure, and `Delete` use one unexported `func remove(ctx context.Context, e Engine, name, runDir string) error`. It first refuses, with `verdict.Condition{Code: verdict.InternalError, …}`, a name without the prefix and a `runDir` whose last element is not the name. Then, in this order, and going on after an error: it removes `RUNDIR/kubeconfig.lock`, which a killed run may have left and which kind's delete takes with an exclusive create; `provider.Delete(name, filepath.Join(runDir, "kubeconfig"))`; `removeContainer(name + "-registry")`; `os.RemoveAll(runDir)`. The folder goes last because kind's delete makes the kubeconfig's folder again when it is missing. The errors are joined with `errors.Join`.
- The failure text is the step's name, a colon and the error. The names are: "remove leftovers", "make the run's folder", "create the kind cluster", "start the registry", "write the node settings", "wait until ready", "create the namespaces", "set the kubeconfig's namespace", "read the versions". The texts of steps 3 and 6 end with: "; a cluster of several nodes also fails when the kernel's inotify limits are low (fs.inotify.max_user_watches, fs.inotify.max_user_instances)".
- When `s.Keep` is set, step 3's `CreateWithRetain(true)` has kind keep the nodes of a cluster that failed to come up, and a failure of any step after step 1 removes nothing; `Create` still returns ClusterFailed. When `ctx` has ended, `Create` also returns without the clean-up: section 10 leaves what a signal stopped to the next run, and a delete takes longer than the time a cancelled job has.
- Steps 4 and 6 wait under one deadline: the moment `Create` began plus `s.Timeout`. Step 4 is `startRegistry(ctx, e, s.Name, IMAGE, deadline)` on the network `e.network()`, IMAGE being `s.RegistryImage`, or `DefaultRegistryImage` when that is empty.
- Step 5 is `nodeutils.WriteFile(node, "/etc/containerd/certs.d/rollproof.test/hosts.toml", HostsTOML(NodeRegistry))` (package `sigs.k8s.io/kind/pkg/cluster/nodeutils`) for each node of `provider.ListNodes(name)`; that function does the `mkdir -p` and the `cp /dev/stdin`. The path and the form are those of `docs/proofs/p01-registry-order.md`; its assumption 3 (a file written into a running node is used by the next pull) is what lets the file come after the cluster.
- Step 6 polls once a second: the node list has `1 + s.Workers` nodes, each with the condition `Ready` `True`, and `kube-system/coredns` has `Status.AvailableReplicas` at least `*Spec.Replicas`. The text at the deadline gives both counts.
- Step 8 is `clientcmd.LoadFromFile`, then `clientcmd.WriteToFile(*cfg, path)`, then `os.Chmod(path, 0o600)`. `Context` is the file's `CurrentContext`.
- Step 9: `KubernetesVersion` is `Discovery().ServerVersion().GitVersion`. `NodeImage` is `s.NodeImage`, or `DefaultNodeImage()` when that is empty. A reference without `@sha256:` gets its digest from `ENGINE image inspect --format "{{json .RepoDigests}}" REFERENCE`: the part after `@` of the first entry. `docs/proofs/p08-kind-library.md` names the command that printed the node image with its digest; use that one if it differs.
- `Nodes` returns the names of the nodes that `provider.ListNodes` gave after step 3: the one whose `Role()` is `control-plane` first, then the others sorted by name.
- `Exec` finds the node by name (an unknown name is an error), runs `node.CommandContext(ctx, args[0], args[1:]...)` with standard output and standard error set to one buffer, and returns the buffer. On a failure the error's text ends with the output.
- `Client` is `clientcmd.BuildConfigFromFlags("", c.Kubeconfig)` and `kubernetes.NewForConfig`.
- `Delete` is `remove(ctx, c.Engine, c.Name, c.RunDir)`.

- [ ] **Step 12: Run the tests with a cluster to verify they pass**

Run: `env CGO_ENABLED=0 go test -tags cluster -p 1 ./internal/cluster/ -v -timeout 20m`
Expected: PASS for `TestCreateAndDelete`, `TestCreateReplacesLeftovers`, `TestRefusesANameWithoutThePrefix`, `TestDeleteTwice` and `TestKeepAfterFailure`.

Run: `nerdctl ps -a --format '{{.Names}}'` (`docker` in place of `nerdctl` on Docker)
Expected: no name that starts with `rollproof-test-`, and no `other-registry`.

- [ ] **Step 13: Run every unit test and vet**

Run: `go mod tidy`
Run: `go test ./...`
Expected: PASS

Run: `go vet -tags cluster ./internal/cluster/`
Expected: no output.

- [ ] **Step 14: Commit**

```bash
git add go.mod go.sum internal/cluster
git commit -m "Create and delete a run's kind cluster with its registry and namespaces"
```

### Task 22: `images`: the probe's own image, local images, and the nodes' pulls

**Files:**
- Create: `internal/images/images.go`, `internal/images/own.go`, `internal/images/local.go`
- Test: `internal/images/own_test.go`, `internal/images/images_test.go` (build tag `cluster`)

**Interfaces:**
- Consumes: `type Cluster struct { … }` with PushRegistry, Engine, `func (c *Cluster) Nodes() []string` and `func (c *Cluster) Exec(ctx context.Context, node string, args ...string) ([]byte, error)` (Task 21); `const TestHost` (Task 20); `type Image struct { Local, Archive, As string }` and `type ImageRef struct { Repository, Digest string }` (Tasks 17 and 19); ClusterFailed (Task 12).
- Produces: `module internal/images`
  Gets images into the run's registry by digest and into the nodes, and
  says where each can be pulled from. It hides image archives, the
  registry's protocol and the way a node is asked to pull.
- Produces: `type Stored struct { Probe config.ImageRef; Images map[string]config.ImageRef }`
  What Store put in place: the probe's own image, and each entry of the
  configuration's images under its name.
- Produces: `func Store(ctx context.Context, c *cluster.Cluster, binary string, images map[string]config.Image, timeout time.Duration) (Stored, error)`
  Packages the tool's binary at the given path as the probe's image and
  has the control-plane node pull it; stores each local image and has
  every node pull it. Nothing is stored under a tag. timeout bounds each
  node's pull. An error is a Condition with ClusterFailed.

The tests with the build tag `cluster` run with `env CGO_ENABLED=0`, for the reason Task 21 gives: they reach `cluster.CheckRunner`, which checks that the running binary is static.

- [ ] **Step 1: Bring the image library up to date**

Run: `go get github.com/google/go-containerregistry@latest`
Expected: `go.mod` requires `github.com/google/go-containerregistry` (Task 21 added it; this takes the newest release). The packages in use are `name`, `v1`, `v1/empty`, `v1/mutate`, `v1/tarball` and `v1/remote`.

- [ ] **Step 2: Write the failing unit tests**

`internal/images/own_test.go`:

```go
//go:build linux

package images

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"rollproof/internal/cluster"
	"rollproof/internal/config"
	"rollproof/internal/verdict"
)

func TestOwnImage(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "probe-binary")
	content := []byte("\x7fELF stands in for the tool's binary")
	if err := os.WriteFile(bin, content, 0o644); err != nil {
		t.Fatal(err)
	}

	img, err := ownImage(bin)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OS != "linux" || cfg.Architecture != runtime.GOARCH {
		t.Errorf("platform %s/%s, want linux/%s", cfg.OS, cfg.Architecture, runtime.GOARCH)
	}
	if diff := cmp.Diff([]string{"/rollproof"}, cfg.Config.Entrypoint); diff != "" {
		t.Errorf("entrypoint (-want +got):\n%s", diff)
	}
	if cfg.Config.User != "65532:65532" {
		t.Errorf("user %q, want 65532:65532", cfg.Config.User)
	}

	layers, err := img.Layers()
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) != 1 {
		t.Fatalf("%d layers, want 1", len(layers))
	}
	rc, err := layers[0].Uncompressed()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	tr := tar.NewReader(rc)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Name != "rollproof" || hdr.Typeflag != tar.TypeReg || hdr.Mode != 0o755 {
		t.Errorf("entry %q, type %c, mode %o; want rollproof, a regular file, 0755", hdr.Name, hdr.Typeflag, hdr.Mode)
	}
	got, err := io.ReadAll(tr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Error("the layer's file differs from the binary")
	}
	if _, err := tr.Next(); err != io.EOF {
		t.Errorf("the layer holds a second entry: %v", err)
	}

	again, err := ownImage(bin)
	if err != nil {
		t.Fatal(err)
	}
	d1, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}
	d2, err := again.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Errorf("two builds from one file gave %s and %s", d1, d2)
	}
}

// Store refuses what config.NotBuilt stops, before it touches the cluster: the empty Cluster and
// the missing binary would otherwise give cluster-failed.
func TestStoreRefusesWhatIsNotBuilt(t *testing.T) {
	for _, img := range []config.Image{
		{Archive: "/tmp/orders.tar"},
		{Local: "orders:candidate", As: "registry.example/team/orders"},
	} {
		_, err := Store(context.Background(), &cluster.Cluster{}, "/does/not/exist",
			map[string]config.Image{"orders": img}, time.Minute)
		if c := verdict.AsCondition(err); err == nil || c.Code != verdict.InternalError || !strings.Contains(c.Text, "images.orders") {
			t.Errorf("Store(%+v) = %v, want internal-error naming images.orders", img, err)
		}
	}
}

// Section 12: a node that cannot pull a stored image, against fakes of the node's command runner.
func TestNodeThatCannotPull(t *testing.T) {
	const (
		node = "rollproof-x-worker"
		ref  = "rollproof.test/app@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	)
	ctx := context.Background()

	full := func(context.Context, string, ...string) ([]byte, error) {
		return []byte("pulling\nfatal: no space left on device\n\n"), errors.New("exit status 1")
	}
	err := pull(ctx, full, node, ref, time.Minute)
	if c := verdict.AsCondition(err); err == nil || c.Code != verdict.ClusterFailed || !strings.Contains(c.Text, node) ||
		!strings.Contains(c.Text, ref) || !strings.Contains(c.Text, "no space left on device") {
		t.Errorf("a full disk: %v, want cluster-failed naming the node, the reference and the last line", err)
	}

	stuck := func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	start := time.Now()
	err = pull(ctx, stuck, node, ref, 50*time.Millisecond)
	if c := verdict.AsCondition(err); err == nil || c.Code != verdict.ClusterFailed {
		t.Errorf("a pull that never ends: %v, want cluster-failed", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("a pull with a timeout of 50ms returned after %v", d)
	}

	var got []string
	fine := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		got = args
		return []byte("Image is up to date for sha256:0123\n"), nil
	}
	if err := pull(ctx, fine, node, ref, time.Minute); err != nil {
		t.Errorf("a pull that succeeds: %v", err)
	}
	if diff := cmp.Diff([]string{"crictl", "pull", ref}, got); diff != "" {
		t.Errorf("the command run in the node (-want +got):\n%s", diff)
	}
}
```

- [ ] **Step 3: Write the failing tests with a cluster**

`internal/images/images_test.go`:

```go
//go:build linux && cluster

// These tests need a container engine and make one kind cluster.
//
// Under Docker the engine's archive has another layout than under nerdctl. This test is the proof
// that Store reads it; it runs on a hosted runner when the maintainer says so.
package images

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"rollproof/internal/cluster"
	"rollproof/internal/config"
	"rollproof/internal/verdict"
)

var (
	shared     *cluster.Cluster
	sharedErr  error
	sharedOnce sync.Once
)

// testCluster creates, once for the package, a cluster with one worker; TestMain deletes it.
func testCluster(t *testing.T) *cluster.Cluster {
	t.Helper()
	sharedOnce.Do(func() {
		e, err := cluster.CheckRunner(context.Background())
		if err != nil {
			sharedErr = err
			return
		}
		n := "rollproof-test-images"
		shared, sharedErr = cluster.Create(context.Background(), cluster.Spec{
			Name: n, RunDir: cluster.RunDir(n), Workers: 1, Namespace: "app", Timeout: 5 * time.Minute}, e)
	})
	if sharedErr != nil {
		t.Fatalf("test cluster: %v", sharedErr)
	}
	return shared
}

func TestMain(m *testing.M) {
	code := m.Run()
	if shared != nil {
		if err := shared.Delete(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, "delete the test cluster:", err)
			code = 1
		}
	}
	os.Exit(code)
}

// tinyProgram builds a static program that prints ok and sleeps, and returns its folder and path.
func tinyProgram(t *testing.T) (dir, bin string) {
	t.Helper()
	dir = t.TempDir()
	src := "package main\n\nimport (\n\t\"fmt\"\n\t\"time\"\n)\n\nfunc main() {\n\tfmt.Println(\"ok\")\n\ttime.Sleep(time.Hour)\n}\n"
	for f, body := range map[string]string{"go.mod": "module app\n\ngo 1.27\n", "main.go": src} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin = filepath.Join(dir, "app")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return dir, bin
}

// tags returns the tags the run's registry lists for repository.
func tags(t *testing.T, push, repository string) []string {
	t.Helper()
	resp, err := http.Get("http://" + push + "/v2/" + repository + "/tags/list")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct{ Tags []string }
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
	}
	return body.Tags
}

func TestStoreOwnAndLocal(t *testing.T) {
	ctx := context.Background()
	c := testCluster(t)
	dir, bin := tinyProgram(t)
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\nCOPY app /app\nENTRYPOINT [\"/app\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(c.Engine.Binary, "build", "-t", "rollproof-test-img:one", dir).CombinedOutput(); err != nil {
		t.Fatalf("build the local image: %v\n%s", err, out)
	}
	t.Cleanup(func() { exec.Command(c.Engine.Binary, "rmi", "rollproof-test-img:one").Run() })

	s, err := Store(ctx, c, bin, map[string]config.Image{"app": {Local: "rollproof-test-img:one"}}, 2*time.Minute)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	app := s.Images["app"]
	if app.Repository != "rollproof.test/app" || !strings.HasPrefix(app.Digest, "sha256:") {
		t.Errorf("app stored as %+v", app)
	}
	for _, n := range c.Nodes() {
		if out, err := c.Exec(ctx, n, "crictl", "inspecti", app.Ref()); err != nil {
			t.Errorf("%s does not hold %s: %v\n%s", n, app.Ref(), err, out)
		}
	}
	if s.Probe.Repository != "rollproof.test/rollproof" || !strings.HasPrefix(s.Probe.Digest, "sha256:") {
		t.Errorf("probe stored as %+v", s.Probe)
	}
	if out, err := c.Exec(ctx, c.Nodes()[0], "crictl", "inspecti", s.Probe.Ref()); err != nil {
		t.Errorf("the control-plane node does not hold %s: %v\n%s", s.Probe.Ref(), err, out)
	}
	for _, repo := range []string{"app", "rollproof"} {
		if got := tags(t, c.PushRegistry, repo); len(got) != 0 {
			t.Errorf("the run's registry has tags %v for %s", got, repo)
		}
	}

	// Section 14, point 21: a pod's status names the digest it runs. If this fails, stop and report.
	client, err := c.Client()
	if err != nil {
		t.Fatal(err)
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "stored-app", Namespace: "app"},
		Spec: corev1.PodSpec{
			NodeName:   c.Nodes()[1],
			Containers: []corev1.Container{{Name: "app", Image: app.Ref(), ImagePullPolicy: corev1.PullIfNotPresent}},
		},
	}
	if _, err := client.CoreV1().Pods("app").Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		p, err := client.CoreV1().Pods("app").Get(ctx, "stored-app", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if p.Status.Phase == corev1.PodRunning && len(p.Status.ContainerStatuses) == 1 {
			if id := p.Status.ContainerStatuses[0].ImageID; !strings.Contains(id, app.Digest) {
				t.Errorf("imageID %q does not hold the digest %s", id, app.Digest)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the pod is %s after 2 minutes: %+v", p.Status.Phase, p.Status.ContainerStatuses)
		}
		time.Sleep(time.Second)
	}
}

func TestStoreUnknownLocalImage(t *testing.T) {
	c := testCluster(t)
	_, bin := tinyProgram(t)
	_, err := Store(context.Background(), c, bin, map[string]config.Image{"app": {Local: "rollproof-test-img:absent"}}, time.Minute)
	if err == nil {
		t.Fatal("Store of an image the engine does not hold gave no error")
	}
	if cond := verdict.AsCondition(err); cond.Code != verdict.ClusterFailed || !strings.Contains(cond.Text, "rollproof-test-img:absent") {
		t.Errorf("error = %v, want cluster-failed naming the image", err)
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./internal/images/ -v`
Expected: FAIL, with `undefined: ownImage`, `undefined: Store` and `undefined: pull` among the compile errors.

- [ ] **Step 5: Implement the probe's own image in `internal/images/own.go`**

`func ownImage(binary string) (v1.Image, error)`, with `v1` the package `github.com/google/go-containerregistry/pkg/v1`. `Store` calls it with its `binary`, which `main` sets to `os.Executable()`.

- The one layer is `tarball.LayerFromOpener` over a tar stream of one entry: `Name: "rollproof"`, `Typeflag: tar.TypeReg`, `Mode: 0o755`, `Size` the file's size, `ModTime: time.Unix(0, 0)`, owner 0, and the file's bytes. The fixed time is what makes two builds from one file give one digest.
- `mutate.AppendLayers(empty.Image, layer)`; take a `DeepCopy` of that image's config, which holds the layer's diff ID, and set `OS` to `"linux"`, `Architecture` to `runtime.GOARCH`, `Config.Entrypoint` to `[]string{"/rollproof"}` and `Config.User` to `"65532:65532"`; apply it with `mutate.ConfigFile`.

`proofs/internal/lab/image.go` (Task 3) is a working single-binary image; read it, and write this one afresh.

- [ ] **Step 6: Implement a local image in `internal/images/local.go`**

The file starts with `//go:build linux`, as does `images.go`. `func storeLocal(ctx context.Context, c *cluster.Cluster, entry, local string) (config.ImageRef, error)` puts the engine's image `local` (NAME:TAG) into the run's registry and returns `{Repository: cluster.TestHost + "/" + entry, Digest: …}`. `config.Load` rejects an `images` key that is not a valid repository path component, so `entry` needs no check here:

1. Make `c.RunDir/images` with mode 0700. Run `ENGINE save LOCAL -o FILE`, `ENGINE` being `c.Engine.Binary` and FILE `c.RunDir/images/ENTRY.tar`. A failure gives the text "images.ENTRY: the engine could not save LOCAL: " and the last line of its output.
2. `tarball.LoadManifest` of the file: more than one entry gives "images.ENTRY: the engine's archive of LOCAL holds more than one image".
3. `tarball.ImageFromPath(FILE, nil)`; an error gives "images.ENTRY: the engine's archive of LOCAL cannot be read: " and the error.
4. Push it by digest to `c.PushRegistry` under the repository ENTRY (Step 7's `pushImage`); a failure gives "images.ENTRY: push to the run's registry failed: " and the error. Remove FILE after the push, also after a failure.

Every error is a `verdict.Condition` with ClusterFailed.

- [ ] **Step 7: Implement `Stored` and `Store` in `internal/images/images.go`**

The file opens with the package comment, which is the sentence of `module internal/images` in the Interfaces block. It holds `type Stored struct { Probe config.ImageRef; Images map[string]config.ImageRef }` and `func Store(ctx context.Context, c *cluster.Cluster, binary string, images map[string]config.Image, timeout time.Duration) (Stored, error)`, each with its interface comment from the Interfaces block, and two unexported helpers:

- `func pushImage(ctx context.Context, registry, repository string, img v1.Image) (string, error)`: `name.NewDigest(registry + "/" + repository + "@" + DIGEST, name.Insecure)` and `remote.Write(ref, img, remote.WithContext(ctx))`; returns the digest, `sha256:` included. No tag is written.
- `func pull(ctx context.Context, exec func(ctx context.Context, node string, args ...string) ([]byte, error), node, ref string, timeout time.Duration) error`: runs `exec(ctx, node, "crictl", "pull", REF)` under `context.WithTimeout(ctx, timeout)`. `Store` passes `c.Exec`; the parameter is the seam that `TestNodeThatCannotPull` fakes. A failure is a ClusterFailed condition whose text is "NODE could not pull REF: " and the last line of the output that is not empty, or the error when the output has none. `crictl pull` in the node is the command `docs/proofs/p01-registry-order.md` assumption 2 settles; if the record names another, use that.

`Store` works in this order:

1. Before anything else: an entry with `Archive` or `As` set gives `verdict.Condition{Code: verdict.InternalError, Text: "images.NAME: archive and as are not built yet"}`. `config.NotBuilt` stops these before `run` gets here.
2. `ownImage(binary)`, pushed to `c.PushRegistry` under the repository `rollproof`; `Probe` is `{Repository: cluster.TestHost + "/rollproof", Digest: …}`. Only the control-plane node, `c.Nodes()[0]`, pulls it. A failure to build or push gives "the probe's image: " and the error.
3. For each entry, in the order of their sorted names: `storeLocal` gives `Images[NAME]`, then every node of `c.Nodes()`, in that order, pulls its `Ref()`.

Every error but that of point 1 is a `verdict.Condition` with ClusterFailed.

- [ ] **Step 8: Run the unit tests to verify they pass**

Run: `go test ./internal/images/ -v`
Expected: PASS for `TestOwnImage`, `TestStoreRefusesWhatIsNotBuilt` and `TestNodeThatCannotPull`

- [ ] **Step 9: Run the tests with a cluster to verify they pass**

Run: `env CGO_ENABLED=0 go test -tags cluster -p 1 ./internal/images/ -v -timeout 20m`
Expected: PASS for `TestStoreOwnAndLocal` and `TestStoreUnknownLocalImage`.

If the assertion on `imageID` fails, the fact of section 14, point 21 does not hold: stop, and report it to the maintainer with the `imageID` the pod gave. The report's lines for `images` rest on it.

Run: `nerdctl ps -a --format '{{.Names}}'` (`docker` in place of `nerdctl` on Docker)
Expected: no name that starts with `rollproof-test-`.

- [ ] **Step 10: Vet**

Run: `go mod tidy`
Run: `go vet -tags cluster ./internal/images/`
Expected: no output.

- [ ] **Step 11: Commit**

```bash
git add go.mod go.sum internal/images
git commit -m "Store the probe's own image and local images by digest, and pull them into the nodes"
```
