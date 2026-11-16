> Part of the plan in `README.md` of this folder. Read that file first: its Global Constraints apply to every task here.

## Part 2: Proofs

A proof is a throwaway program and a record. The program lives under `proofs/`, the record under `docs/proofs/`. A proof's task has no red-green cycle: its steps are write the program, run it, write the record, and state the decision. Each record ends with the line this plan assumed and whether it holds. Task 11 is the gate: Part 3 starts only when every assumption that Part 3 rests on holds.

Every proof cluster is named `rollproof-proof-…`, runs one at a time, and is deleted by the program that made it, also when the program fails.

### Task 2: The module, its licence, and pinned Helm binaries

**Files:**
- Create: `go.mod`, `LICENSE`, `README.md`, `.gitignore`
- Create: `hack/fetchhelm/main.go`
- Test: `hack/fetchhelm/main_test.go`

**Interfaces:**
- Consumes: nothing from another task.
- Produces: `go run ./hack/fetchhelm`
  Puts Helm 3.22.1 at .tools/helm-3/helm and Helm 4.3.2 at .tools/helm-4/helm,
  for the machine's own CPU architecture. Each download is checked against a
  SHA-256 written in the program; a mismatch leaves no file and exits 1. A
  binary that is already there with the right checksum is not fetched again.

Commands in this task run from `projects/rollproof/`.

- [ ] **Step 1: Write the main module's files**

`go.mod`, written by hand (not with `go mod init`, which writes a patch version):

```
module rollproof

go 1.27
```

`.gitignore`:

```
bin/
.tools/
```

`README.md`, exactly:

```markdown
# rollproof

`rollproof` is a working name.

It tests, in a CI job or on a laptop, whether deploying a new version of a Kubernetes workload, and rolling it back, causes downtime.

Nothing is released; the design is docs/specs/2026-11-13-rollproof-design.md.

Run the unit tests with `go test ./...`.
```

`LICENSE`:

Run: `curl -fsSLo LICENSE https://www.apache.org/licenses/LICENSE-2.0.txt`
Run: `grep -m1 -v '^[[:space:]]*$' LICENSE`
Expected: one line that reads `Apache License` after its leading spaces.
Run: `wc -l LICENSE`
Expected: `202 LICENSE`

- [ ] **Step 2: Write the failing tests for `hack/fetchhelm`**

`hack/fetchhelm/main_test.go`:

```go
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

// archive returns a gzipped tar that holds linux-ARCH/helm with content, and its SHA-256.
func archive(t *testing.T, content string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "linux-" + runtime.GOARCH + "/helm", Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

// server serves one archive for each Helm version, as get.helm.sh names them, and counts requests.
func server(t *testing.T) (string, map[key]string, *atomic.Int32) {
	t.Helper()
	three, sum3 := archive(t, "helm three\n")
	four, sum4 := archive(t, "helm four\n")
	files := map[string][]byte{
		"/helm-v3.22.1-linux-" + runtime.GOARCH + ".tar.gz": three,
		"/helm-v4.3.2-linux-" + runtime.GOARCH + ".tar.gz":  four,
	}
	hits := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		b, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	sums := map[key]string{
		{"3.22.1", runtime.GOARCH}: sum3,
		{"4.3.2", runtime.GOARCH}:  sum4,
	}
	return srv.URL, sums, hits
}

func TestFetchUnpacksBothVersions(t *testing.T) {
	base, sums, _ := server(t)
	dir := t.TempDir()
	if err := fetch(base, sums, dir); err != nil {
		t.Fatal(err)
	}
	for sub, want := range map[string]string{"helm-3": "helm three\n", "helm-4": "helm four\n"} {
		p := filepath.Join(dir, sub, "helm")
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o755 {
			t.Errorf("%s: mode %v, want 0755", p, fi.Mode().Perm())
		}
		if got, _ := os.ReadFile(p); string(got) != want {
			t.Errorf("%s holds %q, want %q", p, got, want)
		}
	}
}

func TestFetchRejectsAWrongChecksum(t *testing.T) {
	base, sums, _ := server(t)
	sums[key{"3.22.1", runtime.GOARCH}] = strings.Repeat("0", 64)
	dir := t.TempDir()
	err := fetch(base, sums, dir)
	if err == nil || !strings.Contains(err.Error(), "3.22.1") {
		t.Fatalf("fetch = %v, want an error that names 3.22.1", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("%s holds %v after a mismatch, want nothing", dir, entries)
	}
}

func TestFetchSkipsAPresentBinary(t *testing.T) {
	base, sums, hits := server(t)
	dir := t.TempDir()
	if err := fetch(base, sums, dir); err != nil {
		t.Fatal(err)
	}
	before := hits.Load()
	if err := fetch(base, sums, dir); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load() - before; n != 0 {
		t.Errorf("the second fetch made %d requests, want none", n)
	}
}

func TestFetchReplacesAChangedBinary(t *testing.T) {
	base, sums, hits := server(t)
	dir := t.TempDir()
	if err := fetch(base, sums, dir); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "helm-3", "helm")
	if err := os.WriteFile(p, []byte("changed\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := hits.Load()
	if err := fetch(base, sums, dir); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load() - before; n != 1 {
		t.Errorf("fetch made %d requests, want 1 (helm 3.22.1 only)", n)
	}
	if got, _ := os.ReadFile(p); string(got) != "helm three\n" {
		t.Errorf("%s holds %q after the second fetch, want the archive's content", p, got)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./hack/fetchhelm/ -v`
Expected: FAIL: `undefined: key` and `undefined: fetch`

- [ ] **Step 4: Implement `hack/fetchhelm/main.go`**

A comment at the top says what the program does, in the words of the Interfaces block. Standard library only.

- `type key struct{ version, arch string }`
- `var checksums = map[key]string{{"3.22.1", "amd64"}: "", {"3.22.1", "arm64"}: "", {"4.3.2", "amd64"}: "", {"4.3.2", "arm64"}: ""}`. Step 6 fills the four values.
- `func fetch(base string, sums map[key]string, dir string) error`, by these rules:
  1. The versions are `3.22.1`, then `4.3.2`; each goes to `DIR/helm-3/helm` or `DIR/helm-4/helm`. The first error stops the program.
  2. The checksum is `sums[key{version, runtime.GOARCH}]`. An absent or empty value is an error that names the version and the architecture.
  3. The file `helm.sha256` beside the binary holds one line, `ARCHIVE BINARY`: the archive's SHA-256 from the table and the binary's own SHA-256, written when the binary was unpacked. When it names the table's checksum and the binary on disk has the SHA-256 it names, print `helm V: already present` and make no request.
  4. Otherwise GET `BASE/helm-vV-linux-ARCH.tar.gz`. A status other than 200 is an error that names the version and the status.
  5. Copy the body into a temporary file in DIR (create DIR with `os.MkdirAll` first), hashing it on the way. On a mismatch, remove the temporary file and return `helm V: checksum mismatch: got G, want W`. Nothing else exists in DIR at this point.
  6. Unpack the entry `linux-ARCH/helm` into `DIR/helm-M/helm.tmp` with mode 0755, rename it to `DIR/helm-M/helm`, write `helm.sha256`, remove the temporary archive, and print `helm V: fetched to DIR/helm-M/helm`. An archive with no such entry is an error and leaves no file.
- `func printChecksums(base string) error`: for each version and each of `amd64` and `arm64`, GET the archive and print `V ARCH SHA256`, then GET the same URL with `.sha256sum` added and print `  published: TEXT`.
- `main`: flags `-dir` (default `.tools`) and `-print-checksums`; the base is `https://get.helm.sh`. An error is printed to standard error and exits 1.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./hack/fetchhelm/ -v`
Expected: PASS

- [ ] **Step 6: Fill the checksum table**

Run: `go run ./hack/fetchhelm -print-checksums`
Expected: four lines `V ARCH SHA256`, each followed by a `published:` line whose first field is the same SHA-256. Compare each pair by eye; when all four agree, paste the four values into `checksums`. If one differs, stop and report it.

- [ ] **Step 7: Fetch the binaries and check them**

Run: `go run ./hack/fetchhelm`
Expected: `helm 3.22.1: fetched to .tools/helm-3/helm` and `helm 4.3.2: fetched to .tools/helm-4/helm`
Run: `.tools/helm-3/helm version --short`
Expected: a line that starts `v3.22.1`
Run: `.tools/helm-4/helm version --short`
Expected: a line that starts `v4.3.2`
Run: `go run ./hack/fetchhelm`
Expected: `helm 3.22.1: already present` and `helm 4.3.2: already present`

- [ ] **Step 8: Commit**

```bash
git add go.mod LICENSE README.md .gitignore hack/fetchhelm
git commit -m "Add the rollproof module, its licence, and pinned Helm binaries"
```

### Task 3: The proofs module, its lab, and the form of a record

**Files:**
- Create: `proofs/go.mod`, `proofs/internal/lab/lab.go`, `proofs/internal/lab/image.go`
- Test: `proofs/internal/lab/lab_test.go` (build tag `cluster`)
- Create: `docs/proofs/README.md`

**Interfaces:**
- Consumes: nothing from another task.
- Produces: `module proofs/internal/lab`
  A throwaway cluster for one proof: made through kind's library with the
  engine that kind would pick, with a kubeconfig file of its own, reachable
  by a Kubernetes client and by commands run in its nodes. For proofs only.
- Produces: `func Up(ctx context.Context, name string, cfg *v1alpha4.Cluster) (*Lab, error)`
  Creates the cluster rollproof-proof-NAME from cfg and returns when every
  node is ready. It first deletes a cluster of that name. The kubeconfig is
  a new file in a new temporary folder; the caller's own is not touched.
- Produces: `func (l *Lab) Kubeconfig() string`
  The path of this cluster's kubeconfig file.
- Produces: `func (l *Lab) Client() *kubernetes.Clientset`
  A client for this cluster's API server.
- Produces: `func (l *Lab) Engine() string`
  The engine's command name: docker, nerdctl or podman.
- Produces: `func (l *Lab) Nodes() []string`
  The node names, the control-plane node first.
- Produces: `func (l *Lab) Exec(node string, stdin io.Reader, args ...string) (string, error)`
  Runs a command in a node and returns its output, standard error included.
  A non-zero exit is an error that carries the output.
- Produces: `func (l *Lab) Container(ctx context.Context, name string, args ...string) (Container, error)`
  Starts a container on the cluster's network. args are the engine's run
  arguments after the name and the network, the image included. Down
  removes it. The result has its address on that network and a Logs method.
- Produces: `func (l *Lab) Registry(ctx context.Context, name string, env ...string) (Registry, error)`
  Starts a registry container on the cluster's network, with env set in it.
  Push is its address on the runner's loopback, Node its address for the
  nodes, and Logs returns what it has logged so far.
- Produces: `func PushBinary(ctx context.Context, push, repository, binary string) (string, error)`
  Builds an image that holds the one static binary as /app, pushes it to
  the registry at push by digest, and returns "sha256:…". No tag is written.
- Produces: `func (l *Lab) Down() error`
  Deletes the cluster, every container that Container or Registry started,
  and the temporary folder. Safe to call twice.
- Produces: `record docs/proofs/README.md`
  The form of a proof's record, and the list that Task 11 fills: for each
  open point its result, its decision, what this plan assumed, whether that
  holds, and what still waits for a run elsewhere.

Commands for the proofs module run from `projects/rollproof/proofs/`, and each step says so. The commit runs from `projects/rollproof/`.

- [ ] **Step 1: Make the proofs module**

`proofs/go.mod`, written by hand:

```
module rollproof-proofs

go 1.27
```

From `proofs/`:

Run: `go get sigs.k8s.io/kind@v0.33.0 k8s.io/client-go@v0.37 k8s.io/api@v0.37 k8s.io/apimachinery@v0.37 github.com/google/go-containerregistry@latest`
Run: `go list -m sigs.k8s.io/kind k8s.io/client-go github.com/google/go-containerregistry`
Expected: `sigs.k8s.io/kind v0.33.0`, `k8s.io/client-go v0.37.N` and `github.com/google/go-containerregistry vX.Y.Z`. Keep the last two versions for Step 6.

- [ ] **Step 2: Write the lab, `proofs/internal/lab/lab.go`**

The package comment is the text of `module proofs/internal/lab`. Each exported name gets its interface comment from the Interfaces block.

Types beside `Lab` (whose fields are unexported: the provider, the cluster's full name, the temporary folder, the kubeconfig path, the client, the engine's name, the nodes, the names of the containers started, and whether Down has run):

```go
type Container struct {
	Name    string // the engine's name for it: the cluster's name, a hyphen, and the name given
	Address string // its IPv4 address on the network kind
	engine  string
}

type Registry struct {
	Container
	Push string // 127.0.0.1:PORT, where the runner reaches it
	Node string // ADDRESS:5000, where the nodes reach it
}
```

- `func (c Container) Logs() (string, error)`: `ENGINE logs NAME`, standard output and standard error together.

`Up(ctx, name, cfg)`:
1. The engine, chosen by kind's own test, so that the name and kind's provider cannot disagree. When `KIND_EXPERIMENTAL_PROVIDER` is `docker`, `podman`, `nerdctl`, `finch` or `nerdctl.lima`, that is the name. Otherwise the name is the first of `docker`, `nerdctl` and `podman` for which `NAME -v` exits 0 and prints a first line that starts with `Docker version`, `nerdctl version` or `podman version`; this is the test of `IsAvailable` in kind's three providers (`pkg/cluster/internal/providers/` at v0.33.0), in the order of `DetectNodeProvider`, and a comment says so. No name is an error. The provider is made with the option that matches the name: `cluster.ProviderWithDocker()`, `cluster.ProviderWithPodman()`, or `cluster.ProviderWithNerdctl(NAME)` for `nerdctl`, `finch` and `nerdctl.lima`. Then `NAME info` must exit 0; if it does not, Up returns an error that carries its output.
2. The full name is `rollproof-proof-` + name. The folder is `os.MkdirTemp("", FULL+"-")`; the kubeconfig is `kubeconfig` in it.
3. Leftovers: `provider.Delete(FULL, KUBECONFIG)` (its error is ignored when `provider.List()` then lacks FULL), then `ENGINE rm -f` for every name in `ENGINE ps -a --format "{{.Names}}"` that starts with `FULL-`.
4. The description is `cfg.DeepCopy()`; kind's defaulting writes into what it is given. When `ENGINE info --format "{{json .}}"` has an entry of `SecurityOptions` that contains `rootless`, and no entry of `ContainerdConfigPatches` contains `NoNewKeyring`, append this patch:

```toml
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc.options]
  NoNewKeyring = true
```

5. `provider.Create(FULL, cluster.CreateWithV1Alpha4Config(c), cluster.CreateWithKubeconfigPath(KUBECONFIG), cluster.CreateWithWaitForReady(0), cluster.CreateWithRetain(false), cluster.CreateWithDisplayUsage(false), cluster.CreateWithDisplaySalutation(false))`.
6. A client from the kubeconfig (`clientcmd.BuildConfigFromFlags("", KUBECONFIG)`). Poll the nodes every second, for at most 5 minutes and while ctx lasts, until there are as many as the description has and each has the condition Ready with status True. Print to standard error, at the first poll, `lab: Create returned after D; N of M nodes Ready`, and at the end `lab: every node Ready after D`, D counted from the call to Create. Task 4 reads these two lines.
7. The nodes: `provider.ListNodes(FULL)`, the one whose `Role()` is `control-plane` first, then the rest by name.
8. Any error from step 3 on: Down, then return the error.

`Exec(node, stdin, args...)`: the node by name (an unknown name is an error); `n.Command(args[0], args[1:]...)`, `SetStdin` when stdin is not nil, `SetStdout` and `SetStderr` to one buffer, `Run`. An error carries the command, the node and the output.

`Container(ctx, name, args...)`: `ENGINE run -d --name FULL-NAME --network kind ARGS…`. The name is kept for Down before the result is checked. The address comes from `ENGINE inspect FULL-NAME`, parsed as JSON: `NetworkSettings.Networks`, the entry `kind`, or, when there is none, its only entry (nerdctl may name the entry after the interface). No address is an error.

`Registry(ctx, name, env...)`: PORT from `net.Listen("tcp", "127.0.0.1:0")`, closed at once; `Container(ctx, name, "-p", "127.0.0.1:PORT:5000", "-e", ENV1, …, "docker.io/library/registry:3")`; then wait, at most 30 seconds, until `GET http://127.0.0.1:PORT/v2/` answers 200. Push is `127.0.0.1:PORT`; Node is the address and `:5000`.

`Down()`: when it has run before, return nil and do nothing. Otherwise `provider.Delete(FULL, KUBECONFIG)`, then `ENGINE rm -f` for each container name once (an rm that fails while `ENGINE inspect NAME` fails too counts as done), then `os.RemoveAll` of the folder, in that order (kind's delete recreates the folder of the kubeconfig it is given). Return the first error.

- [ ] **Step 3: Write `PushBinary` in `proofs/internal/lab/image.go`**

With its interface comment from the Interfaces block. A tar layer with one entry, `app`, mode 0755, the binary's bytes (`tarball.LayerFromOpener`); `mutate.AppendLayers(empty.Image, layer)`; then read its config file, set `OS` to `linux`, `Architecture` to `runtime.GOARCH`, `Config.Entrypoint` to `["/app"]` and `Config.User` to `65532`, and write it back with `mutate.ConfigFile` (the read keeps the layer's diff ID). `remote.Write` to `name.NewDigest(push+"/"+repository+"@"+DIGEST, name.Insecure)` with `remote.WithContext(ctx)`. Return the digest as `sha256:…`.

- [ ] **Step 4: Write the lab's test**

`proofs/internal/lab/lab_test.go`:

```go
//go:build cluster

package lab

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/kind/pkg/cluster"
)

// provider returns kind's provider for the engine that Lab.Engine names.
func provider(engine string) *cluster.Provider {
	switch engine {
	case "docker":
		return cluster.NewProvider(cluster.ProviderWithDocker())
	case "podman":
		return cluster.NewProvider(cluster.ProviderWithPodman())
	default:
		return cluster.NewProvider(cluster.ProviderWithNerdctl(""))
	}
}

func TestUpDown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	l, err := Up(ctx, "lab", &v1alpha4.Cluster{Nodes: []v1alpha4.Node{{Role: v1alpha4.ControlPlaneRole}}})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Down()

	nodes := l.Nodes()
	if len(nodes) != 1 {
		t.Fatalf("Nodes() = %v, want one name", nodes)
	}
	if out, err := l.Exec(nodes[0], nil, "crictl", "version"); err != nil {
		t.Fatalf("crictl version in %s: %v\n%s", nodes[0], err, out)
	}
	dir := filepath.Dir(l.Kubeconfig())

	if err := l.Down(); err != nil {
		t.Fatal(err)
	}
	names, err := provider(l.Engine()).List()
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(names, "rollproof-proof-lab") {
		t.Errorf("kind still lists rollproof-proof-lab after Down: %v", names)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the kubeconfig's folder %s is still there after Down (%v)", dir, err)
	}
	if err := l.Down(); err != nil {
		t.Errorf("a second Down: %v", err)
	}
}
```

- [ ] **Step 5: Run the lab's test**

From `proofs/`:

Run: `go mod tidy`
Run: `go vet ./...`
Run: `go vet -tags cluster ./...`
Expected: no output from either.
Run: `go test -tags cluster -p 1 ./internal/lab/ -run TestUpDown -v -timeout 15m`
Expected: PASS, with the two `lab:` lines in the output.

- [ ] **Step 6: Write `docs/proofs/README.md`**

In the versions line, write the two versions that Step 1 printed.

```markdown
# Proofs

A proof settles an open point of section 14 of the design before code rests on it. Its program is under `proofs/`, in the module `rollproof-proofs`, and runs from that folder; its record is in this folder. Each program makes one cluster named `rollproof-proof-…`, one at a time, and deletes it, also when the program fails.

Versions in the proofs module: `sigs.k8s.io/kind` v0.33.0; `k8s.io/client-go`, `k8s.io/api` and `k8s.io/apimachinery` VERSION; `github.com/google/go-containerregistry` VERSION.

## The form of a record

Every record has these headings, in this order:

- `## Question`: the open point, in one or two sentences.
- `## Machine and versions`: the system, the engine and whether it runs without root, Go, kind, the node image, and every other tool the method used, each with its version.
- `## Method`: what the program does, and the exact command that ran it.
- `## Result`: what happened, with the lines of output that show it.
- `## Decision`: what the plan does because of the result.

It ends with a pair of lines for each thing the plan assumed, each line starting at the beginning of its line:

    Plan assumed: "…"
    Holds: yes

`Holds:` is followed by `yes`, `no` or `not measured here`.

## The list

Task 11 fills this list from the records.

| Point | Question | Result | Decision | Plan assumed | Holds | Tasks that rest on it |
| --- | --- | --- | --- | --- | --- | --- |
| 1, first half | | | | | | |
| 1, second half | | | | | | |
| 5, command lines | | | | | | |
| 8, nerdctl | | | | | | |
| 8, Docker | | | | | | |
| 9 | | | | | | |
| 10 | | | | | | |
| 11 | | | | | | |
| 12 | | | | | | |
```

- [ ] **Step 7: Commit**

From `projects/rollproof/`:

```bash
git add proofs/go.mod proofs/go.sum proofs/internal/lab docs/proofs/README.md
git commit -m "Add the proofs module with its lab, and the form of a proof's record"
```

### Task 4: Proofs of points 9 and 8: the kind release, and kind as a library

**Files:**
- Create: `proofs/p08-kind-library/main.go`
- Create: `docs/proofs/p08-kind-library.md`, `docs/proofs/p09-kind-release.md`
- Create (repository root): `.github/workflows/rollproof-proofs.yml`

**Interfaces:**
- Consumes: `func Up(ctx context.Context, name string, cfg *v1alpha4.Cluster) (*Lab, error)` and the other operations of `proofs/internal/lab` (Task 3).
- Produces: `record docs/proofs/p09-kind-release.md`
  Which kind release the tool links, why, and the node images that release
  lists, each with its digest; the oldest of them is the second image of
  the suite's matrix.
- Produces: `record docs/proofs/p08-kind-library.md`
  Whether the linked release creates, through the engine on this machine, a
  cluster with the node settings of section 9, and the exact settings that
  worked. The Docker half is marked as not run.
- Produces: `workflow .github/workflows/rollproof-proofs.yml`
  Runs the proof programs on a hosted runner, where the engine is Docker.
  It starts on manual trigger only, and nobody triggers it without the
  maintainer's word.

Program commands in this task run from `projects/rollproof/proofs/`; the workflow's check runs from the repository root, and the commit from `projects/rollproof/`.

- [ ] **Step 1: Write the program `proofs/p08-kind-library/main.go`**

Every proof program of Tasks 4 to 10 has this shape, and the later tasks refer to it as "the shape of Task 4":
- `main` is `os.Exit(run())`. `run` makes its context with `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)` and defers `l.Down()` as soon as `lab.Up` returns a lab, so the cluster goes also when a check fails or the run is interrupted. Nothing inside `run` calls `os.Exit`, `log.Fatal` or `panic`.
- Each check prints one line to standard output, `ok: …` or `FAILED: …`, and a failed check does not stop the program unless a later action needs what it failed to make. A fact that is read and not judged prints as `info: …`.
- The last line is `pNN: all checks passed` with exit 0, or `pNN: N checks failed` with exit 1.

What p08 does:
1. Before anything: for `$HOME/.kube/config` and for each path in `$KUBECONFIG` (split at `:`), note whether it exists and its SHA-256. Print `info:` lines with `KIND_EXPERIMENTAL_PROVIDER` and whether `docker -v` exits 0.
2. Write `{"auths":{}}` to `config.json` in a temporary folder of its own (removed at the end).
3. Build the description:
   - `Nodes`: one `{Role: v1alpha4.ControlPlaneRole}`, then three `{Role: v1alpha4.WorkerRole, Labels: map[string]string{"topology.kubernetes.io/zone": "rollproof-zone-N"}}` for N = 1, 2, 3.
   - On every node, `ExtraMounts: []v1alpha4.Mount{{HostPath: CONFIG_JSON, ContainerPath: "/var/lib/kubelet/config.json", Readonly: true}}`.
   - `ContainerdConfigPatches: []string{"[plugins.\"io.containerd.grpc.v1.cri\".registry]\n  config_path = \"/etc/containerd/certs.d\""}`.
4. `lab.Up(ctx, "p08", cfg)`. Its two `lab:` lines on standard error give when `Create` returned, how many nodes were Ready then, and when the last one was.
5. Checks after Up:
   - the engine: when `KIND_EXPERIMENTAL_PROVIDER` is unset and `docker -v` failed, `l.Engine()` is `nerdctl`; in every case `ENGINE inspect rollproof-proof-p08-control-plane` exits 0;
   - `l.Nodes()` has four names, the first `rollproof-proof-p08-control-plane`;
   - every file of action 1 is as it was, and `l.Kubeconfig()` exists and its current context is `kind-rollproof-proof-p08` (`clientcmd.LoadFromFile`);
   - with the client: each worker has `topology.kubernetes.io/zone`, and the three values are `rollproof-zone-1`, `-2` and `-3`, once each; the control-plane node has no such label and has the taint `node-role.kubernetes.io/control-plane` with effect `NoSchedule`;
   - in each node with `l.Exec`: `cat /var/lib/kubelet/config.json` gives `{"auths":{}}` (trimmed); `containerd config dump` has a line `config_path = "/etc/containerd/certs.d"` or `config_path = '/etc/containerd/certs.d'`, the value ending at the quote (print the matching lines); `crictl version` exits 0 (print its `RuntimeVersion` line).
6. Facts and the plan's versions:
   - `info:` the rootless finding: the `SecurityOptions` of `ENGINE info --format "{{json .}}"`, and whether one contains `rootless`;
   - check: `version.Version()` of `sigs.k8s.io/kind/pkg/cmd/kind/version` is `0.33.0` (it has no `v`);
   - check: `defaults.Image` of `sigs.k8s.io/kind/pkg/apis/config/defaults` starts with `kindest/node:v1.37.0@sha256:` (the description names no image, so kind uses it); `info:` the image of `ENGINE inspect` of the control-plane container, as printed;
   - check: the server's `GitVersion` (`Discovery().ServerVersion()`) starts with `v1.37.`.
7. `l.Down()`, then checks: the folder of `l.Kubeconfig()` is gone; `ENGINE ps -a --format "{{.Names}}"` lists no name that starts with `rollproof-proof-p08`; every file of action 1 is still as it was.

- [ ] **Step 2: Run it**

Run: `go run ./p08-kind-library`
Expected: no line starts with `FAILED:`, and the last line is `p08: all checks passed`.

A failed check is a result, not a fault to fix in the program: keep the output for the record.

- [ ] **Step 3: Read the kind release for point 9**

Run: `git ls-remote --tags --refs https://github.com/kubernetes-sigs/kind`
Expected: the newest tag that does not end in `-alpha` is `v0.33.0` (a `v0.34.0-alpha` may be listed).
Run, in a clone of `kubernetes-sigs/kind`: `git show v0.33.0:pkg/apis/config/defaults/image.go`
Expected: `const Image = "kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5"`
Run: `gh release view v0.33.0 --repo kubernetes-sigs/kind`
Expected: release notes with a list of node images, each `kindest/node:vX.Y.Z@sha256:…`.

If a release newer than v0.33.0 that is not an alpha exists: record it, change nothing in the plan, and write `Holds: no`; Task 11 reports it.

- [ ] **Step 4: Write the two records**

Both have the headings of `docs/proofs/README.md`.

`docs/proofs/p09-kind-release.md`: under Method the three commands of Step 3; under Result the newest tags, the line of `image.go`, and the release's list of node images with their digests, copied as given; under Decision the release linked and why, and the oldest image of the list, named as the second image of the suite's matrix (section 12). It ends:

```
Plan assumed: "The tool links sigs.k8s.io/kind v0.33.0, the newest release that is not an alpha on 2026-11-16; its default node image is kindest/node:v1.37.0."
Holds: yes
```

`docs/proofs/p08-kind-library.md`: under Machine and versions the engine and its version, the rootless finding, Go, kind's version, the node image with its digest, the Kubernetes version and the runtime version (the spec read containerd v2.3.4); under Method the description of action 3 in full and the command; under Result the program's output, the two `lab:` lines, the matching `config_path` lines, and what kind leaves on the machine (the engine's network `kind` and the node image, from `ENGINE network ls` and `ENGINE images`); under Decision the settings that worked, for Tasks 20 and 21. Then a section `## Docker` with the one line `not run; waits for the maintainer's word`. It ends:

```
Plan assumed: "Through nerdctl, kind v0.33.0 as a library creates one control-plane node and three workers with: a zone label on each worker from the cluster description; the containerd patch that sets config_path to /etc/containerd/certs.d; a file of the runner mounted into every node; a kubeconfig written only to the path the tool gives. cluster.DetectNodeProvider picks nerdctl where Docker is absent. With a wait time of zero, Create waits for no node, so the tool waits for every node itself. A rootless engine is shown by a SecurityOptions entry of `ENGINE info` that contains rootless."
Holds: yes
```

Write `no` in either `Holds:` line where the result differs, and say under Result which part.

- [ ] **Step 5: Write the workflow `.github/workflows/rollproof-proofs.yml` (repository root)**

Find the pin for `actions/setup-go`:

Run: `git ls-remote --tags https://github.com/actions/setup-go`
Expected: tags `vX.Y.Z`. Take the newest release tag; its commit is the line ending `vX.Y.Z^{}` when there is one, else the line ending `vX.Y.Z`.

The file, with that commit and tag in place of COMMIT and vX.Y.Z:

```yaml
name: rollproof proofs

on:
  workflow_dispatch:

permissions:
  contents: read

jobs:
  proofs:
    runs-on: ubuntu-24.04
    timeout-minutes: 30
    defaults:
      run:
        shell: bash
        working-directory: projects/rollproof/proofs
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: actions/setup-go@COMMIT # vX.Y.Z
        with:
          go-version: "1.27"
          cache-dependency-path: projects/rollproof/proofs/go.sum
      - name: kind as a library, on Docker
        run: go run ./p08-kind-library
      - name: The registry order in the nodes, on Docker
        run: go run ./p01-registry-order
```

`p01-registry-order` is written in Task 6. Do not trigger the workflow.

From the repository root:
Run: `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/rollproof-proofs.yml`
Expected: no output, exit 0.

- [ ] **Step 6: Commit**

From `projects/rollproof/`:

```bash
git add proofs/p08-kind-library docs/proofs/p08-kind-library.md docs/proofs/p09-kind-release.md ../../.github/workflows/rollproof-proofs.yml
git commit -m "Prove kind as a library on nerdctl, and choose the kind release to link"
```

### Task 5: Proof of point 12: kernel keys on a rootless engine

**Files:**
- Create: `proofs/p12-keys/main.go`
- Create: `docs/proofs/p12-keys.md`

**Interfaces:**
- Consumes: the operations of `proofs/internal/lab` (Task 3).
- Produces: `record docs/proofs/p12-keys.md`
  How many kernel keys of the user one cluster holds through a run, with and
  without the runtime option that stops a container from taking a session
  key, and whether the tool sets that option on a rootless engine.

Commands in this task run from `projects/rollproof/proofs/`, the commit from `projects/rollproof/`.

- [ ] **Step 1: Write the program `proofs/p12-keys/main.go`**

The shape of Task 4, with one difference: each round calls `lab.Up` and `Down` itself, and defers `Down` only for its own lab.

A reading is the line of `/proc/key-users` whose first field is `UID:` for `os.Getuid()`. Its form is `UID: USAGE NKEYS/NIKEYS QNKEYS/MAXKEYS QNBYTES/MAXBYTES`; the reading is QNKEYS and MAXKEYS. Each reading prints as `info: round R, STAGE: QNKEYS/MAXKEYS`.

Two rounds. Round 1 is "without the option": its description carries this patch, so the lab adds none (the lab leaves its patch out when a patch of the caller mentions `NoNewKeyring`):

```toml
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc.options]
  NoNewKeyring = false
```

Round 2 is "with the option": its description has no patch, and the lab adds `NoNewKeyring = true` on a rootless engine.

Each round:
1. Read: baseline.
2. `lab.Up(ctx, "p12", cfg)`, cfg with one control-plane node and three workers. Read: after Up. In round 1 only, check the engine: when no entry of `SecurityOptions` in `ENGINE info --format "{{json .}}"` contains `rootless`, Down, print `not a rootless engine: nothing to measure` and exit 0.
3. Start a sampler: read every 500 ms until the round ends, and keep the peak. When QNKEYS passes 180, cancel the round's context and Down at once; the round is "stopped early" and its peak is what was seen.
4. The pause image: `crictl images` in a worker; the tag of the line for `registry.k8s.io/pause`. Print `info: pause image registry.k8s.io/pause:TAG`. Use `3.10` if that is the tag.
5. Create the namespace `p12` and a Deployment `load` there: 6 replicas, labels `app: load`, one container `pause` of `registry.k8s.io/pause:TAG` with `imagePullPolicy: IfNotPresent`. Wait, at most 5 minutes, until `readyReplicas` is 6. Read: 6 ready.
6. Twice: patch `spec.template.metadata.annotations["kubectl.kubernetes.io/restartedAt"]` to the time in RFC 3339 (a strategic merge patch), then wait, at most 5 minutes, until `observedGeneration` is at least `generation` and `status.replicas`, `updatedReplicas` and `availableReplicas` are all 6. Read: after restart 1, after restart 2.
7. Down. Stop the sampler. Read: after Down.

Print, for each round, `info: round R peak QNKEYS, above baseline P` and whether it stopped early. The last line is:

```
p12: peak without the option N, with the option M, quota Q
```

N and M are the peaks above baseline; Q is MAXKEYS.

- [ ] **Step 2: Run it**

Run: `go run ./p12-keys`
Expected: the last line `p12: peak without the option N, with the option M, quota Q`, or `not a rootless engine: nothing to measure`.

- [ ] **Step 3: Write the record `docs/proofs/p12-keys.md`**

The headings of `docs/proofs/README.md`. Under Result, a table:

| Round | Baseline | After Up | 6 ready | Restart 1 | Restart 2 | After Down | Peak | Above baseline |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| without the option | | | | | | | | |
| with the option | | | | | | | | |

and the pause tag the nodes held. Under Decision, the rule: when the peak above baseline without the option is at most 100 (half the default quota of 200), the option leaves the tool, Task 20 drops the effect of its `rootless` argument, and the line below is `Holds: no`; otherwise the option stays and it is `Holds: yes`. On an engine that is not rootless, say so and write `Holds: not measured here`. It ends:

```
Plan assumed: "On a rootless engine the tool sets NoNewKeyring = true for the runc runtime in the nodes' containerd; one cluster of four nodes with the first fixture's shape, through an install and two rollouts, would otherwise use more than half of the user's key quota."
Holds: yes
```

- [ ] **Step 4: Commit**

From `projects/rollproof/`:

```bash
git add proofs/p12-keys docs/proofs/p12-keys.md
git commit -m "Measure the kernel keys one cluster uses on a rootless engine"
```

### Task 6: Proof of point 1, first half: registry order, digest and tag, and how a node pulls

**Files:**
- Create: `proofs/p01-registry-order/main.go`
- Create: `docs/proofs/p01-registry-order.md`

**Interfaces:**
- Consumes: the operations of `proofs/internal/lab` (Task 3), `PushBinary` among them.
- Produces: `record docs/proofs/p01-registry-order.md`
  What a node does with a reference for a host whose settings list the
  run's registry first: by digest, by tag, by both, and when the run's
  registry lacks the image. Also the command by which the tool has a node
  pull, and whether a pod's status names the digest it ran.

Commands in this task run from `projects/rollproof/proofs/`, the commit from `projects/rollproof/`. Read first: section 9 ("The run's registry", "How the caller's image gets in", "The order for one host"), section 14 point 1, `docs/hosts.md` of containerd at its tag v2.3.4 (the version in the node image), and `site/static/examples/kind-with-registry.sh` of kind at its tag v0.33.0.

- [ ] **Step 1: Write the program `proofs/p01-registry-order/main.go`**

The shape of Task 4. What it does:
1. Five images. Write a temporary folder with `go.mod` (`module p01app`, a blank line, `go 1.27`) and this `main.go`:

```go
package main

import (
	"fmt"
	"time"
)

var v = "unset"

func main() {
	fmt.Println(v)
	time.Sleep(time.Hour)
}
```

   Build it five times in that folder, with `CGO_ENABLED=0` and `GOOS=linux` added to the command's environment: `go build -trimpath -ldflags "-s -w -X main.v=V" -o DIR/V .` for V = `x`, `y`, `y2`, `z`, `w`. Five binaries give five digests: X, Y, Y2, Z and W.
2. `lab.Up(ctx, "p01", cfg)`: one control-plane node and one worker, with the `config_path` patch in the form `docs/proofs/p08-kind-library.md` records as working.
3. `run := l.Registry(ctx, "run")` and `real := l.Registry(ctx, "real")`. `real` stands for the real registry of the made-up host `registry.example`.
4. Push with `lab.PushBinary`: X to `run.Push` as `team/app`; Y to `run.Push` as `team/app`; Y2 to `real.Push` as `team/app`; Z to `real.Push` as `team/app`; W to `run.Push` as `probe/rollproof`. Then tag Y as `team/app:v1` in `run` and Y2 as `team/app:v1` in `real`: `remote.Image` of the digest reference, `remote.Write` to `name.NewTag(PUSH+"/team/app:v1", name.Insecure)`. The proof uses a tag here; the tool never does.
5. In every node: `l.Exec(node, nil, "mkdir", "-p", "/etc/containerd/certs.d/registry.example")`, then `l.Exec(node, strings.NewReader(TEXT), "cp", "/dev/stdin", "/etc/containerd/certs.d/registry.example/hosts.toml")` with TEXT:

```toml
server = "http://REAL_NODE"
[host."http://RUN_NODE"]
  capabilities = ["pull"]
```

   REAL_NODE is `real.Node`, RUN_NODE is `run.Node`.
6. The logs. `type request struct{ Method, Path string; Status int }` and `func requests(log string) []request`: one request for each access-log line of the form `"METHOD PATH HTTP/1.1" STATUS`; when the log has none, one for each line with the fields `http.request.method`, `http.request.uri` and `http.response.status`. Before each case note the length of both registries' `Logs()`; after it, print `info: CASE run: METHOD PATH STATUS` and `info: CASE real: …` for each new request whose path starts with `/v2/`.
7. Before each of the cases a to f, remove the image that the case pulls from the worker: `crictl rmi REFERENCE`, an error ignored. Every pull is `crictl pull REFERENCE` in the worker, through `l.Exec`.
8. The cases, each one `ok:` or `FAILED:` line:
   - a. Pull `registry.example/team/app@X`. It succeeds; `run` has `/v2/team/app/manifests/X` with status 200; `real` has no new request.
   - b. Pull `registry.example/team/app:v1`. `crictl inspecti -o json registry.example/team/app:v1` lists `registry.example/team/app@Y2` in `status.repoDigests`; `run` has no request for `/v2/team/app/manifests/v1`.
   - c. Pull `registry.example/team/app@Z`. It succeeds; `run` has a 404 for a path that ends in Z, and `real` then has `/v2/team/app/manifests/Z` with 200.
   - d. Pull `registry.example/team/app:whatever@X`. It succeeds; neither registry has a request whose path ends in `/manifests/whatever`.
   - e. In every node, write `/etc/containerd/certs.d/rollproof.test/hosts.toml` (as in action 5) with only:

```toml
[host."http://RUN_NODE"]
  capabilities = ["pull"]
```

     Pull `rollproof.test/probe/rollproof@W`: it succeeds. Then pull `rollproof.test/probe/rollproof@sha256:` followed by 64 zeros: print as `info:` the error text and how long the failed pull took.
   - f. `crictl rmi registry.example/team/app@X`, then `ctr -n k8s.io images pull registry.example/team/app@X` in the worker. Print as `info:` the exit code, the output, and the new requests of both registries; no request in `run` means `ctr` does not use the node's settings.
   - g. Create the namespace `p01` and a pod `probe-w` there: `nodeName` the worker, one container `app` of `rollproof.test/probe/rollproof@W`, `imagePullPolicy: IfNotPresent`, `restartPolicy: Never`. Wait, at most 2 minutes, until it runs. `run` has no new request for `/manifests/W`; `status.containerStatuses[0].imageID` contains W; print the imageID as `info:`, as it is. Then `crictl rmi registry.example/team/app@X` (an error ignored), and a second pod `kubelet-x`, the same but with `registry.example/team/app@X`: it runs, and `run` has `/v2/team/app/manifests/X` with 200, which shows that the kubelet's own pull goes through the same settings.
9. Down.

The program passes when a, c, d, the first pull of e and g hold, and b ended with Y2. The last line is `p01: all checks passed`.

- [ ] **Step 2: Run it**

Run: `go run ./p01-registry-order`
Expected: no line starts with `FAILED:`, and the last line is `p01: all checks passed`.

- [ ] **Step 3: Write the record `docs/proofs/p01-registry-order.md`**

The headings of `docs/proofs/README.md`. Under Method the five images, both settings files as written, and the command. Under Result, one paragraph for each case a to g, each quoting the log lines that show it, and for e the error text and the time of the failed pull, for f what `ctr` did, for g the imageID as printed. Under Decision: the settings form for Task 20; the pull command for Tasks 21 and 22, and the command the tool must not use; whether the report's lines on images can rest on `imageID`. It ends with four pairs, each `Holds:` set from the result:

```
Plan assumed: "For a host, the node's settings file lists the run's registry as `[host.\"http://ADDRESS:5000\"]` with `capabilities = [\"pull\"]`, before the real host. A reference by digest is then served by the run's registry; a reference by tag is not answered by it and goes to the real host; a reference with a tag and a digest is pulled by the digest alone; an image the run's registry lacks is fetched from the real host."
Holds: yes
Plan assumed: "The tool has a node pull with `crictl pull REFERENCE`, run in the node. That pull uses the node's registry settings, the ones the kubelet's pulls use."
Holds: yes
Plan assumed: "A settings file written into a running node is used by the next pull, with no restart."
Holds: yes
Plan assumed: "A pod's status gives, in imageID, the digest of the image each container runs."
Holds: yes
```

- [ ] **Step 4: Commit**

From `projects/rollproof/`:

```bash
git add proofs/p01-registry-order docs/proofs/p01-registry-order.md
git commit -m "Prove the registry order in the nodes, and how a node pulls"
```

### Task 7: Proof of point 1, second half: a mirror under a path, with a private authority, and the one list for a host

**Files:**
- Create: `proofs/p01b-mirror/main.go`
- Create: `docs/proofs/p01b-mirror.md`

**Interfaces:**
- Consumes: the operations of `proofs/internal/lab` (Task 3); the settings form that `docs/proofs/p01-registry-order.md` records (Task 6).
- Produces: `record docs/proofs/p01b-mirror.md`
  The one settings file for a host that has the run's registry, a mirror
  under a path and a private authority together; what each of the two kinds
  of mirror path needs; and the order in which a node tries the places.
  Nothing in this plan's skeleton rests on it: it is input for step 5.

Commands in this task run from `projects/rollproof/proofs/`, the commit from `projects/rollproof/`. Read first: section 9 ("Registries: credentials, mirrors and certificate authorities", "The order for one host"), section 14 point 1, and `docs/hosts.md` as in Task 6. Nothing in Part 3 depends on this proof: if it does not hold, Task 11 records that for the plan of step 5, and Part 3 goes on.

- [ ] **Step 1: Write the program `proofs/p01b-mirror/main.go`**

The shape of Task 4. What it does:
1. Three images, A, B and F, built as in Task 6, action 1, with V = `a`, `b`, `f`.
2. `lab.Up(ctx, "p01b", cfg)`, cfg as in Task 6.
3. Certificates with `crypto/x509`, in a temporary folder CERTS: a CA (`ca.pem`), and one server certificate signed by it (`server.pem`, key `server-key.pem`) for the names `mirror.corp.example` and `cache.corp.example` and the address `127.0.0.1`, which the runner's pushes use. Both valid for one day.
4. `run := l.Registry(ctx, "run")`, plain HTTP.
5. Two TLS registries, each through `l.Container` with the image `docker.io/library/registry:3` and these arguments, each with a PORT of its own found by listening on `127.0.0.1:0` and closing:
   `-p 127.0.0.1:PORT:443 -v CERTS:/certs:ro -e REGISTRY_HTTP_ADDR=0.0.0.0:443 -e REGISTRY_HTTP_TLS_CERTIFICATE=/certs/server.pem -e REGISTRY_HTTP_TLS_KEY=/certs/server-key.pem`.
   The URLs in the settings below carry no port, so the registries listen on 443.
   - `project`: these arguments only. An image under the repository `proxy-project/team/app` stands for a cache that wants its project name inside the API path.
   - `prefixed`: these arguments and `-v rollproof-proof-p01b-prefixed-data:/var/lib/registry`, first without a prefix. Run `ENGINE volume rm -f rollproof-proof-p01b-prefixed-data` before, and again after Down.
   Wait until `GET https://127.0.0.1:PORT/v2/` answers 200, with a client whose `RootCAs` hold the CA.
6. Push. A: `lab.PushBinary` to `run.Push` as `team/app`. B and F: `lab.PushBinary` to `run.Push` as `stage/b` and `stage/f` (PushBinary takes no transport), then copy each: `remote.Image` of its digest reference in `run`, `remote.Write` with `remote.WithTransport` (a transport that trusts the CA) to `127.0.0.1:PORT/team/app:v1` in `prefixed` for B, and to `127.0.0.1:PORT/proxy-project/team/app:v1` in `project` for F. No case asks `run` for `stage/…`.
7. Start `prefixed` again with its prefix: `ENGINE rm -f rollproof-proof-p01b-prefixed`, then `l.Container` with the same arguments and `-e REGISTRY_HTTP_PREFIX=/registry-example/`, on the same volume. Wait until `GET https://127.0.0.1:PORT/registry-example/v2/` answers 200. Print `info: prefixed was filled without its prefix and started again with it, on the same volume`.
8. In every node: `sh -c "echo PREFIXED_ADDRESS mirror.corp.example >> /etc/hosts"` and `sh -c "echo PROJECT_ADDRESS cache.corp.example >> /etc/hosts"`; `mkdir -p /etc/containerd/certs.d/registry.example`; the CA's PEM to `/etc/containerd/certs.d/registry.example/ca.pem` (`cp /dev/stdin`).
9. For each case: write that case's settings file to `/etc/containerd/certs.d/registry.example/hosts.toml` in every node; remove the case's image from the worker with `crictl rmi` (an error ignored); note the length of the three registries' logs and the time in UTC as TIME, written `2006-01-02 15:04:05 UTC`; pull with `crictl pull` in the worker; print the new requests of each registry as in Task 6, action 6, and the worker's `journalctl -u containerd --no-pager --since TIME` lines that contain `trying next host` or `fetch failed`, which name each host tried, in order.
10. Form A, the mirror under a path; RUN_NODE is `run.Node`:

```toml
server = "https://registry.example"
[host."http://RUN_NODE"]
  capabilities = ["pull"]
[host."https://mirror.corp.example/registry-example"]
  capabilities = ["pull", "resolve"]
  ca = "/etc/containerd/certs.d/registry.example/ca.pem"
```

   - a. Pull `registry.example/team/app@A`: it succeeds, and `run` served `/v2/team/app/manifests/A` with 200.
   - b. Pull `registry.example/team/app:v1`: it succeeds; `run` has no request for `/manifests/v1`; `prefixed` has a request whose path starts with `/registry-example/v2/team/app/`, with 200.
   - c. Form A without its `ca` line, and the pull of b: it fails, and the error contains `certificate`.
   - d. Pull `registry.example/team/app@sha256:` followed by 64 zeros: it fails, and the error names `registry.example`. The containerd lines show the order of attempts; the last host tried should be `registry.example`.
   - e. Form A with its two `[host…]` blocks swapped, and the pull of a: print as `info:` which registry was asked first, from the logs and the containerd lines.
11. Form B, the project inside the API path: form A with the mirror's block replaced by

```toml
[host."https://cache.corp.example/v2/proxy-project"]
  capabilities = ["pull", "resolve"]
  ca = "/etc/containerd/certs.d/registry.example/ca.pem"
  override_path = true
```

   - f. Pull `registry.example/team/app:v1`: it succeeds, and `project` has a request whose path starts with `/v2/proxy-project/team/app/`, with 200.
   - g. Form A's way of writing pointed at `project`: the block `[host."https://cache.corp.example/proxy-project"]` with the same `capabilities` and `ca` and no `override_path`. Pull `registry.example/team/app:v1`: print as `info:` the paths `project` was asked for, and whether the pull failed.
12. Down; remove the volume.

The program passes when a, b, c, d and f behaved as expected. The last line is `p01b: all checks passed`.

- [ ] **Step 2: Run it**

Run: `go run ./p01b-mirror`
Expected: no line starts with `FAILED:`, and the last line is `p01b: all checks passed`.

- [ ] **Step 3: Write the record `docs/proofs/p01b-mirror.md`**

The headings of `docs/proofs/README.md`. Under Method, both forms as written, how `prefixed` was filled (started without its prefix and again with it), and the command. Under Result, for each case a to g, the log lines of all three registries and the containerd lines that show it; for e, whether the file's order is the order of attempts; for g, the path the node asked for. Under Decision, for the plan of step 5: the one settings file for a host that has the run's registry, a mirror and a `ca`; and, as a proposal, what the configuration's `mirror` key must be able to say so that the tool can write form A or form B, with the two example values `https://mirror.corp.example/registry-example` and `https://cache.corp.example/v2/proxy-project`. It ends:

```
Plan assumed: "For one host the node's settings file lists, in this order, the run's registry (pull only), the mirror over HTTPS with its `ca` (pull and resolve), and the real host as `server`. A node tries them in that order. A mirror whose registry API sits under a path prefix is written as the host with that prefix; a cache that wants its project name inside the API path needs `override_path = true`."
Holds: yes
```

- [ ] **Step 4: Commit**

From `projects/rollproof/`:

```bash
git add proofs/p01b-mirror docs/proofs/p01b-mirror.md
git commit -m "Prove a mirror under a path with a private authority, for the enterprise step"
```

### Task 8: Proof of point 10: the rollout rules

**Files:**
- Create: `proofs/p10-rollout-rules/rules_test.go`
- Create: `proofs/p10-rollout-rules/size/clientonly/main.go`, `proofs/p10-rollout-rules/size/withkubectl/main.go`
- Create: `docs/proofs/p10-rollout-rules.md`

**Interfaces:**
- Consumes: nothing from another task but the `proofs` module (Task 3).
- Produces: `record docs/proofs/p10-rollout-rules.md`
  Whether kubectl's package with the rules of rollout status can be
  imported, what it costs in binary size, what each rule returns for the
  cases the tool meets, and whether the tool imports the rules or copies
  them.

Commands in this task run from `projects/rollproof/proofs/`, the commit from `projects/rollproof/`. Read first: section 7 ("When a transition has finished", "What is watched", Strategies) and section 14 point 10. The source is `k8s.io/kubectl/pkg/polymorphichelpers/rollout_status.go`, at `staging/src/k8s.io/kubectl/pkg/polymorphichelpers/` in Kubernetes at its tag v1.37.0, and in the module cache after Step 2. Its API: `StatusViewerFor(kind schema.GroupKind) (StatusViewer, error)` and `StatusViewer.Status(obj runtime.Unstructured, revision int64) (string, bool, error)`.

- [ ] **Step 1: Count the modules before**

Run: `go list -m all | wc -l`
Expected: a number; keep it for the record.

- [ ] **Step 2: Add kubectl's package**

Run: `go get k8s.io/kubectl@v0.37`
Run: `go list -m k8s.io/kubectl k8s.io/client-go k8s.io/api k8s.io/apimachinery`
Expected: four versions; record whether `client-go`, `api` and `apimachinery` are the ones Task 3 chose.

- [ ] **Step 3: Write `proofs/p10-rollout-rules/rules_test.go`**

`package rules`, one test `TestRolloutRules` with a subtest for each case, named by its letter. A helper converts a typed object and asks kubectl's rule:

```go
// status runs kubectl's rule for the object's kind, as kubectl rollout status does with revision 0.
func status(t *testing.T, obj runtime.Object, gk schema.GroupKind) (string, bool, error)
```

It converts with `runtime.DefaultUnstructuredConverter.ToUnstructured`, wraps the map in `&unstructured.Unstructured{Object: m}`, gets the viewer with `polymorphichelpers.StatusViewerFor(gk)` and calls `Status(u, 0)`. Every case logs message, done and error with `t.Logf`; it asserts only the column "Asserted".

The three base objects are typed `appsv1` literals:
- Deployment `orders`: generation 2; `spec.replicas` 3; status `observedGeneration` 2, `replicas` 3, `updatedReplicas` 3, `readyReplicas` 3, `availableReplicas` 3.
- StatefulSet `db`: generation 2; `spec.replicas` 3; `updateStrategy` RollingUpdate with `rollingUpdate.partition` 0, as the API server fills it in; status `observedGeneration` 2, `replicas` 3, `readyReplicas` 3, `currentReplicas` 3, `updatedReplicas` 3, `currentRevision` and `updateRevision` both `db-2`.
- DaemonSet `agent`: generation 2; `updateStrategy` RollingUpdate; status `observedGeneration` 2, `desiredNumberScheduled` 3, `updatedNumberScheduled` 3, `numberReady` 3, `numberAvailable` 3.

| Case | Object | Change from the base | Asserted |
| --- | --- | --- | --- |
| a | Deployment | none | done true, no error |
| b | Deployment | `updatedReplicas` 2 | done false |
| c | Deployment | status `replicas` 4 (an old pod still counted) | done false |
| d | Deployment | `availableReplicas` 2 | done false |
| e | Deployment | generation 3 | done false |
| f | Deployment | condition `Progressing`, status False, reason `ProgressDeadlineExceeded` | an error; log its text |
| g | Deployment | generation 1 and `observedGeneration` 1: nothing changed | done true |
| h | StatefulSet | none | done true |
| i | StatefulSet | `updatedReplicas` 2, `currentRevision` `db-1` | done false |
| j | StatefulSet | `partition` 2, `updatedReplicas` 1, `currentRevision` `db-1` | log only |
| k | StatefulSet | `updateStrategy` OnDelete, no `rollingUpdate` | log message, done and error exactly |
| l | DaemonSet | none | done true |
| m | DaemonSet | `numberAvailable` 2 | done false |
| n | DaemonSet | `updateStrategy` OnDelete | log message, done and error exactly |
| o | Deployment | `terminatingReplicas` 2 | done true: the rule does not wait for terminating pods |

- [ ] **Step 4: Write the two size programs**

`proofs/p10-rollout-rules/size/clientonly/main.go`:

```go
package main

import (
	"fmt"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func main() {
	c, err := kubernetes.NewForConfig(&rest.Config{Host: "http://127.0.0.1:1"})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(c != nil, appsv1.SchemeGroupVersion)
}
```

`proofs/p10-rollout-rules/size/withkubectl/main.go`: the same, with the import `"k8s.io/kubectl/pkg/polymorphichelpers"` and, before the last line:

```go
	v, err := polymorphichelpers.StatusViewerFor(appsv1.SchemeGroupVersion.WithKind("Deployment").GroupKind())
	fmt.Println(v != nil, err)
```

- [ ] **Step 5: Run the rules**

Run: `go mod tidy`
Run: `go list -m all | wc -l`
Expected: a larger number than Step 1; keep it.
Run: `go test ./p10-rollout-rules/ -v`
Expected: PASS, with a log line for each of the fifteen cases.

- [ ] **Step 6: Measure the cost in binary size**

Run: `env CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/clientonly ./p10-rollout-rules/size/clientonly`
Run: `env CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/withkubectl ./p10-rollout-rules/size/withkubectl`
Run: `stat -c "%s %n" bin/clientonly bin/withkubectl`
Expected: two sizes in bytes. `bin/` is ignored by git.

- [ ] **Step 7: Write the record `docs/proofs/p10-rollout-rules.md`**

The headings of `docs/proofs/README.md`. Under Result: whether the package resolved with the client-go already required, and the versions chosen; the module counts before and after; the two sizes and their difference; the logged message, done and error of every case. Say that no rule waits for terminating pods (case o), so the tool adds that condition itself.

Under Decision: import when the difference is under 3 MB, otherwise copy with a comparing test; write which. The line below is `Holds: no` when the decision is to import. Then, for Task 25:
- the exact conditions each rule tests, as a short list of field comparisons per kind, in the order the rule tests them, with what it returns for each;
- the licence header of `rollout_status.go`, as it stands.

It ends:

```
Plan assumed: "The tool copies the three Status rules of k8s.io/kubectl/pkg/polymorphichelpers (rollout_status.go at the tag that matches client-go) into internal/watch/rollout.go, with their licence header and source, and a test in the tool's module compares the copy with kubectl's package on the same objects. Importing the package into the binary costs more than 3 MB."
Holds: yes
```

- [ ] **Step 8: Commit**

From `projects/rollproof/`:

```bash
git add proofs/go.mod proofs/go.sum proofs/p10-rollout-rules docs/proofs/p10-rollout-rules.md
git commit -m "Read and measure kubectl's rollout rules, and decide to copy them"
```

### Task 9: Proof of point 11: a pod's log across rotation, and opening a stream again

**Files:**
- Create: `proofs/p11-log-rotation/main.go`, `proofs/p11-log-rotation/liner/main.go`
- Create: `docs/proofs/p11-log-rotation.md`

**Interfaces:**
- Consumes: the operations of `proofs/internal/lab` (Task 3); the settings form and the pull command that `docs/proofs/p01-registry-order.md` records (Task 6).
- Produces: `record docs/proofs/p11-log-rotation.md`
  Whether following a pod's log through the API loses lines when the
  kubelet rotates the log, how a stream that ended is opened again with no
  line lost and none read twice, and what the last piece of an exited
  container's output looks like.

Commands in this task run from `projects/rollproof/proofs/`, the commit from `projects/rollproof/`. Read first: section 6 ("Time", "The log stream") and section 14 point 11.

- [ ] **Step 1: Write the probe stand-in `proofs/p11-log-rotation/liner/main.go`**

A static program, standard library only. Flags:
- `-n` the number of lines (default 10);
- `-rate` lines a second; 0 writes as fast as it can;
- `-pad` the number of `x` bytes in `pad` (default 0);
- `-tail`: after the lines, write a last piece with no line end, then exit;
- `-same`: write the identical line `{"ok":false,"class":"refused"}` instead.

Each line is `{"ok":true,"detail":{"i":N,"pad":"xxx…"}}`, with N from 1, written with one `os.Stdout.Write` of the whole line and its `\n`; no buffering. Line N is written no earlier than (N-1)/rate seconds after the start. The tail piece is `{"ok":true,"detail":{"i":N}}` with N one past the last line, and no `\n`. SIGTERM exits 0 at once; so does the end of the lines.

- [ ] **Step 2: Write the program `proofs/p11-log-rotation/main.go`**

The shape of Task 4. What it does:
1. Build `liner` with `go build -trimpath -ldflags "-s -w" -o DIR/liner ./p11-log-rotation/liner`, with `CGO_ENABLED=0` and `GOOS=linux` added to the command's environment.
2. `lab.Up(ctx, "p11", cfg)`: one control-plane node and one worker, cfg as in Task 6.
3. `reg := l.Registry(ctx, "run")`; in every node the settings file for `rollproof.test` that `docs/proofs/p01-registry-order.md` records (case e); `lab.PushBinary` of `liner` to `reg.Push` as `probe/liner`; `crictl pull rollproof.test/probe/liner@DIGEST` in the worker.
4. The namespace `p11`. For each experiment: delete a pod `liner` that is left and wait until it is gone; create the pod `liner`: `restartPolicy: Never`, `nodeName` the worker, one container `probe` of `rollproof.test/probe/liner@DIGEST` with `imagePullPolicy: IfNotPresent` and the experiment's arguments; wait, at most 2 minutes, until the container runs or has ended.
5. The reader: `CoreV1().Pods("p11").GetLogs("liner", &corev1.PodLogOptions{Container: "probe", Follow: true, Timestamps: true, SinceTime: SINCE}).Stream(ctx)`, SINCE nil when the stream first opens; read with `bufio.Reader.ReadString('\n')`, never a `Scanner` (lines can be long); split each line at its first space into the timestamp, parsed with `time.RFC3339Nano`, and the text; take N from `detail.i`.
6. The reopening rule. Keep LAST, the timestamp of the last line read, and AT, how many lines read carry exactly LAST. Open again with SINCE `metav1.NewTime(LAST.Truncate(time.Second))`. Skip a line whose timestamp is before LAST; at LAST, skip the first AT lines; take every line after. Note the first line returned after the reopen and how many lines at LAST came back: when that first line is later than LAST, or fewer than AT lines at LAST came back, the rule has seen a gap.
7. The experiments:
   - A, rotation: `-n 150000 -rate 15000 -pad 180` (about 30 MiB in 10 s; the kubelet rotates at 10 MiB and looks every 10 s). Follow from the start to the end of the stream. Check: N runs from 1 to n with no gap and no repeat, and no timestamp is before the one before it. Then `ls -la /var/log/pods/p11_liner_UID/probe/` in the worker. When it lists no rotated file (a name that starts with `0.log.`), double `-n` and run A again, at most twice. Print as `info:` the `-n` that rotated, the first and last timestamp as received, the number of lines, the gaps, the repeats and the listing.
   - B, reopen: `-n 60000 -rate 2000` (30 s). At 5, 12 and 19 seconds after the stream first opened, cancel the stream's context, wait 2 s, and open again by the rule. Check: every N from 1 to 60,000 read exactly once.
   - C, reopen after a rotation: the arguments with which A rotated. Poll the `ls` of A every 500 ms; when a rotated file appears, cut the stream, wait 12 s, and open again by the rule; follow to the end. Print as `info:` the lines lost (the missing N), the first line returned after the reopen, the lines at LAST that came back against AT, and whether the rule saw a gap. C is recorded, not judged.
   - D, same text and same stamp: `-same -n 50000 -rate 0`. Cut after 10,000 and after 30,000 lines read, and open again by the rule each time. Check: exactly 50,000 lines read. Print as `info:` the largest number of lines that shared one timestamp.
   - E, the tail: `-n 10 -tail`. Follow to the end of the stream. Print as `info:` the last piece as received (`%q`), whether it ends with `\n`, its timestamp, the error that ended the stream (`io.EOF` expected), and the container's terminated state from the pod's status (exit code and reason).
8. Down.

The last line is `p11: A ok, B ok, C TEXT, D ok, E recorded`, with `FAILED` in place of `ok` for a failed check, and TEXT `no line lost`, `N lines lost, gap seen` or `N lines lost, gap not seen`. Exit 0 when A, B and D hold.

- [ ] **Step 3: Run it**

Run: `go run ./p11-log-rotation`
Expected: the last line `p11: A ok, B ok, C …, D ok, E recorded`.

- [ ] **Step 4: Write the record `docs/proofs/p11-log-rotation.md`**

The headings of `docs/proofs/README.md`. Under Result: each experiment's lines; the exact form of the timestamp before a line, quoted from the output; that a last piece without a line end comes back without one (or what came back). Under Decision, for Task 29, the reopening rule as it was run, in five lines or fewer, and what the tool can detect when lines are lost. It ends with three pairs, each `Holds:` set from the result:

```
Plan assumed: "A log followed from its start through the API (follow and timestamps on) gives every line once, in order, across the kubelet's rotations."
Holds: yes
Plan assumed: "A stream that ended while the container runs is opened again with sinceTime set to the second of the last line read. The lines of that second that were already read are skipped by counting them, so two lines with the same timestamp and the same text are neither lost nor read twice."
Holds: yes
Plan assumed: "When the lines after the break are no longer returned (they sit in a rotated file), the follower can see it: the first line returned is later than the last line read plus what it skipped, and the count does not match. The tool then reports a gap."
Holds: yes
```

- [ ] **Step 5: Commit**

From `projects/rollproof/`:

```bash
git add proofs/p11-log-rotation docs/proofs/p11-log-rotation.md
git commit -m "Prove following a pod's log across rotation, and the rule for opening it again"
```

### Task 10: Proof of point 5: Helm's command lines in both majors

**Files:**
- Create: `proofs/p05-helm-cli/main.go`, `proofs/p05-helm-cli/chart/Chart.yaml`, `proofs/p05-helm-cli/chart/values.yaml`, `proofs/p05-helm-cli/chart/values.schema.json`, `proofs/p05-helm-cli/chart/templates/deployment.yaml`
- Create: `docs/proofs/p05-helm-cli.md`

**Interfaces:**
- Consumes: the operations of `proofs/internal/lab` (Task 3); the binaries that `go run ./hack/fetchhelm` puts under `.tools/` (Task 2).
- Produces: `record docs/proofs/p05-helm-cli.md`
  For Helm 3.22.1 and 4.3.2: that each command line of section 7 exists
  and means the same, the revisions an install, an upgrade and a rollback
  give, how the revision is read, the exit code of a failure, and which
  environment variables override the command line.

Commands in this task run from `projects/rollproof/proofs/`, the commit from `projects/rollproof/`. Read first: section 7 ("The Helm driver", "The candidate's values", "The cluster Helm acts on") and section 14 points 5 and 17.

- [ ] **Step 1: Write the chart `proofs/p05-helm-cli/chart/`**

`Chart.yaml`:

```yaml
apiVersion: v2
name: p05
version: 0.1.0
```

`values.yaml`:

```yaml
replicas: 2
marker: chart
```

`values.schema.json`:

```json
{
  "type": "object",
  "properties": {
    "replicas": { "type": "integer" },
    "marker": { "type": "string" }
  },
  "required": ["replicas"]
}
```

`templates/deployment.yaml`, with the pause tag that `docs/proofs/p12-keys.md` recorded in place of `3.10` if it differs. The Deployment is named after the release, so release `p05` gives the Deployment `p05` and the contrast release `p05b` of Step 2 does not collide with it:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Release.Name }}
spec:
  replicas: {{ .Values.replicas }}
  selector:
    matchLabels:
      app: {{ .Release.Name }}
  template:
    metadata:
      labels:
        app: {{ .Release.Name }}
      annotations:
        marker: {{ .Values.marker | quote }}
    spec:
      containers:
        - name: pause
          image: registry.k8s.io/pause:3.10
          imagePullPolicy: IfNotPresent
```

- [ ] **Step 2: Write the program `proofs/p05-helm-cli/main.go`**

The shape of Task 4, with the last line `p05: all checks passed for VERSION` (VERSION from check 1). It takes `-helm PATH`, made absolute with `filepath.Abs`. Every helm command runs with `exec.Command`, its folder set to `p05-helm-cli` (so `./chart` names the chart), and an environment that is the program's own with the eleven `HELM_` variables of the Global Constraints taken out. For each, print the command line, its exit code and its output. CL below stands for `--namespace p05 --kubeconfig F --kube-context CTX`: F is `l.Kubeconfig()` and CTX that file's current context.

1. `lab.Up(ctx, "p05", cfg)`: one control-plane node and one worker. Create the namespace `p05` with the client; the tool never passes `--create-namespace`. `charts := l.Registry(ctx, "charts")`.
2. The checks, each `ok:` or `FAILED:` unless marked `info:`:
   1. `helm version --short`: exit 0, a line that starts with `v`.
   2. Install: write VALUES1 (`marker: one`) to a temporary file. `helm upgrade --install p05 ./chart CL --timeout 2m0s --values VALUES1`: exit 0. At once, read the Deployment `p05` and print as `info:` whether `readyReplicas` was 2 when the command returned. `helm status p05 CL -o json`: `version` is 1, `info.status` is `deployed`.
   3. Upgrade with reset: `helm upgrade p05 ./chart --reset-values CL --timeout 2m0s`: exit 0; `helm status` gives `version` 2; `helm get values p05 CL -o json` prints `null` or `{}`; the Deployment's pod template annotation `marker` is `chart`. Then, for contrast, as `info:`: `helm upgrade --install p05b ./chart CL --timeout 2m0s --values VALUES1`, then `helm upgrade p05b ./chart CL --timeout 2m0s` with no values and no `--reset-values`; the annotation of Deployment `p05b` (expected to stay `one`).
   4. Rollback: `helm rollback p05 1 CL --timeout 2m0s`: exit 0; `helm status` gives `version` 3; `helm get manifest p05 CL --revision 1` and `helm get manifest p05 CL --revision 3` print the same text.
   5. Pull: `helm package ./chart -d TMP`; `helm push TMP/p05-0.1.0.tgz oci://PUSH/charts --plain-http` (PUSH is `charts.Push`); `helm pull oci://PUSH/charts/p05 --version 0.1.0 --destination DIR --plain-http`: DIR holds `p05-0.1.0.tgz`; `helm show chart DIR/p05-0.1.0.tgz` prints `name: p05` and `version: 0.1.0`. The tool does not pass `--plain-http`; the proof needs it for a registry without TLS.
   6. A failure: `helm upgrade p05 ./chart --reset-values --set replicas=two CL --timeout 2m0s`: exit not 0; print the exit code as `info:` (assumed 1); `helm status` still gives `version` 3.
   7. Overrides, `info:` both: with `HELM_NAMESPACE=elsewhere` added to the environment, `helm status p05 CL -o json`: whether it finds the release (which of the two wins). With `HELM_KUBEAPISERVER=https://127.0.0.1:1` added, the same command: its exit code and error (it is expected to go to that server and fail to connect).
   8. Flags, `info:`: in the text of `helm upgrade --help` and of `helm rollback --help`, whether each of `--reset-values`, `--timeout`, `--install`, `--wait`, `--atomic` and `--rollback-on-failure` appears. Help leaves out a flag that Helm marks deprecated, so for each flag the help of `helm upgrade` does not show, also run `helm upgrade --FLAG --help` and print whether Helm answers `unknown flag`.
3. Down.

- [ ] **Step 3: Run it under both majors**

Run: `go run ./p05-helm-cli -helm ../.tools/helm-3/helm`
Expected: no line starts with `FAILED:`; the last line is `p05: all checks passed for v3.22.1`.
Run: `go run ./p05-helm-cli -helm ../.tools/helm-4/helm`
Expected: no line starts with `FAILED:`; the last line is `p05: all checks passed for v4.3.2`.

- [ ] **Step 4: Write the record `docs/proofs/p05-helm-cli.md`**

The headings of `docs/proofs/README.md`. Under Result, a table with one row for each check and one column for each major, holding the exit codes, the revisions, how the revision was read, whether the Deployment was ready when the install returned, the contrast of check 3, the winners of check 7, and the flags of check 8. Under Decision, the command lines Task 24 uses, and any difference between the majors that changes them. It ends:

```
Plan assumed: "Under Helm 3.22.1 and under Helm 4.3.2 the same command lines work: `helm pull CHART --version V --destination DIR`; `helm upgrade --install RELEASE CHART --namespace NS --kubeconfig FILE --kube-context CTX --timeout D --values FILE`; `helm upgrade RELEASE CHART --reset-values …`; `helm rollback RELEASE REVISION …`; `helm status RELEASE -o json`, whose `version` is the revision; `helm show chart ARCHIVE`, which gives the chart's name and version. They give revisions 1, 2 and 3. Each returns before the rollout is complete. Any failure exits non-zero. A duration is accepted as Go prints it (2m0s)."
Holds: yes
```

- [ ] **Step 5: Commit**

From `projects/rollproof/`:

```bash
git add proofs/p05-helm-cli docs/proofs/p05-helm-cli.md
git commit -m "Prove Helm's command lines under both majors"
```

### Task 11: The gate: compare every result with what this plan assumed

**Files:**
- Modify: `docs/proofs/README.md`

**Interfaces:**
- Consumes: the records of Tasks 4 to 10.
- Produces: `record docs/proofs/README.md`
  The filled list: for each open point its result, its decision, the line
  this plan assumed, whether it holds, and what still waits for a run on a
  hosted runner. Part 3 starts only when the list allows it.

Commands in this task run from `projects/rollproof/` unless a step says "from `proofs/`".

- [ ] **Step 1: Fill the list in `docs/proofs/README.md`**

One row for each point the list already names, from the records of Tasks 4 to 10. Result and Decision are one sentence each; Plan assumed is copied from the record; Holds is `yes`, `no` or `not measured here`, as the record says. A row whose record has several assumptions holds only when all of them do; its Plan assumed cell names the record and the number of assumptions. The last column:

| Point | Tasks that rest on it |
| --- | --- |
| 1, first half | Tasks 20, 21, 22, 29 |
| 1, second half | none in this plan (step 5) |
| 5, command lines | Task 24, Task 36 |
| 8, nerdctl | Tasks 20, 21 |
| 8, Docker | Tasks 21, 22, 36 on a hosted runner |
| 9 | Global Constraints, Task 21 |
| 10 | Task 25 |
| 11 | Task 29 |
| 12 | Task 20 |

The row for point 8 with Docker has Result `not run; waits for the maintainer's word` and Holds `not measured here`.

- [ ] **Step 2: Apply the rule**

When every row that names a task of this plan says `yes`, Part 3 starts. Two rows may say `not measured here` instead: point 12 on a machine that is not rootless, and point 8 with Docker, which waits for a hosted runner (Step 4 lists it).

When any such row says `no`: STOP. Do not start Part 3. Write a section `## What changes` under the list that names each task whose text no longer fits and what the result says instead, and hand the README to the maintainer. The plan is revised before Part 3 starts.

- [ ] **Step 3: Check that nothing is left running**

Run: `nerdctl ps -a --format "{{.Names}}"` (with the engine the proofs used in place of `nerdctl`)
Expected: no name that starts with `rollproof-proof-`.

Add a section `## What the proofs leave on the machine` under the list: kind's network `kind` and the node image, which the proofs did not remove.

- [ ] **Step 4: Write what is still unproved**

A section `## Still unproved` under the list:
- point 8 with Docker: `p08-kind-library` and `p01-registry-order` in `.github/workflows/rollproof-proofs.yml`, waiting for the maintainer's word;
- the engine's `save` format under Docker: Task 22's test with the `cluster` tag, on a hosted runner;
- the open points that section 13 gives to later steps: points 2 and 19 with step 5; points 3, 4 and 20 with step 6; the fixture runs of point 5 with steps 3 and 4; each fact of point 21 with the step that builds on it; points 7 and 16 with the fixture series of step 7.

- [ ] **Step 5: Verify**

From `proofs/`:
Run: `go vet ./...`
Expected: no output.

From `projects/rollproof/`:
Run: `grep -c "^Holds:" docs/proofs/p01-registry-order.md docs/proofs/p01b-mirror.md docs/proofs/p05-helm-cli.md docs/proofs/p08-kind-library.md docs/proofs/p09-kind-release.md docs/proofs/p10-rollout-rules.md docs/proofs/p11-log-rotation.md docs/proofs/p12-keys.md`
Expected:

```
docs/proofs/p01-registry-order.md:4
docs/proofs/p01b-mirror.md:1
docs/proofs/p05-helm-cli.md:1
docs/proofs/p08-kind-library.md:1
docs/proofs/p09-kind-release.md:1
docs/proofs/p10-rollout-rules.md:1
docs/proofs/p11-log-rotation.md:3
docs/proofs/p12-keys.md:1
```

and each file's last line starts with `Holds:`.

- [ ] **Step 6: Commit**

```bash
git add docs/proofs/README.md
git commit -m "Record the proofs' results against what the plan assumed"
```
