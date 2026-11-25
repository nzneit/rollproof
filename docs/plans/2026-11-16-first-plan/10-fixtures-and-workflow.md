> Part of the plan in `README.md` of this folder. Read that file first: its Global Constraints apply to every task here.

### Task 35: The fixture service and its chart

**Files:**
- Create: `fixtures/service/main.go`, `fixtures/service/Dockerfile`
- Test: `fixtures/service/main_test.go`
- Create: `fixtures/chart/Chart.yaml`, `fixtures/chart/values.yaml`, `fixtures/chart/values.schema.json`, `fixtures/chart/templates/deployment.yaml`, `fixtures/chart/templates/service.yaml`

**Interfaces:**
- Consumes: nothing from another task but the module.
- Produces: `module fixtures/service`
  The service under test in the project's own fixtures: a small HTTP
  server whose start delay and behaviour on SIGTERM are set by environment
  variables, and whose version string is built in, so that two builds are
  two images.
- Produces: `GET /work?ms=N`
  Works for N milliseconds and answers 200 with the pod's name and the
  built-in version.
- Produces: `GET /readyz`
  Answers 200. The server listens only when it can serve.
- Produces: `environment START_DELAY, SHUTDOWN, SERVE_AFTER_TERM, DRAIN_DEADLINE, ADDR`
  START_DELAY is the time before the server listens. SHUTDOWN is exit (end
  at once on SIGTERM), drain-now (stop accepting and drain), or helper
  (serve on for SERVE_AFTER_TERM, then drain under DRAIN_DEADLINE).
- Produces: `module fixtures/chart`
  A Helm chart of the fixture service: one Deployment named orders and one
  Service named orders on port 80. Its values make each variant that the
  fixtures of section 12 need.
- Produces: `values image.repository, image.digest, replicas, readiness, startDelay, shutdown, serveAfterTerm, drainDeadline, spread, gracePeriod`
  image.digest is required. replicas is 2. readiness (true) adds the
  readiness probe on /readyz. spread (true) forbids two pods on one node.
  The strategy is always one more pod and none unavailable.

Read first: section 12 ("With a cluster": the fixture, why the two images differ, the first four rows), and the experiment's service and its two manifests, `A-compliant.yaml` and `base.yaml`, which are files of the incubating repository and are not published, for the service and the Deployment's shape.

- [ ] **Step 1: Write the failing tests**

`fixtures/service/main_test.go`. `ADDR` is `127.0.0.1:0` and the ready channel gives the address; the stop that ctx's end stands for is SIGTERM.

```go
package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"
)

// client opens a new connection for every request, as the built-in probe does.
var client = &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 5 * time.Second}

func get(addr, path string) (int, string, error) {
	resp, err := client.Get("http://" + addr + path)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), err
}

// launch starts serve with env in a goroutine. stop stands for SIGTERM.
func launch(t *testing.T, env map[string]string) (ready <-chan string, stop context.CancelFunc, done <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r := make(chan string, 1)
	errc := make(chan error, 1)
	go func() { errc <- serve(ctx, func(k string) string { return env[k] }, r) }()
	return r, cancel, errc
}

// listening waits until serve listens and returns its address.
func listening(t *testing.T, ready <-chan string, done <-chan error) string {
	t.Helper()
	select {
	case addr := <-ready:
		return addr
	case err := <-done:
		t.Fatalf("serve returned before it listened: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not listen within 5 s")
	}
	return ""
}

func refused(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err == nil {
		conn.Close()
	}
	return errors.Is(err, syscall.ECONNREFUSED)
}

func TestWorkAndReadyz(t *testing.T) {
	version = "v-test"
	ready, _, done := launch(t, map[string]string{"ADDR": "127.0.0.1:0", "SHUTDOWN": "drain-now"})
	addr := listening(t, ready, done)
	began := time.Now()
	code, body, err := get(addr, "/work?ms=20")
	if err != nil || code != 200 {
		t.Fatalf("/work?ms=20: %d, %v; want 200", code, err)
	}
	if d := time.Since(began); d < 20*time.Millisecond {
		t.Errorf("/work?ms=20 answered in %v, want 20 ms or more", d)
	}
	if !strings.Contains(body, "v-test") {
		t.Errorf("the body %q does not hold the version", body)
	}
	if code, _, err := get(addr, "/readyz"); err != nil || code != 200 {
		t.Errorf("/readyz: %d, %v; want 200", code, err)
	}
}

func TestStartDelay(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	began := time.Now()
	ready, _, done := launch(t, map[string]string{"ADDR": addr, "START_DELAY": "300ms", "SHUTDOWN": "drain-now"})
	time.Sleep(100 * time.Millisecond)
	if !refused(addr) {
		t.Error("a dial 100 ms after the start was not refused")
	}
	listening(t, ready, done)
	if d := time.Since(began); d < 300*time.Millisecond {
		t.Errorf("it listened after %v, want 300 ms or more", d)
	}
	if code, _, err := get(addr, "/readyz"); err != nil || code != 200 {
		t.Errorf("/readyz: %d, %v; want 200", code, err)
	}
}

func TestHelperServesOnThenDrains(t *testing.T) {
	ready, stop, done := launch(t, map[string]string{"ADDR": "127.0.0.1:0", "SHUTDOWN": "helper",
		"SERVE_AFTER_TERM": "300ms", "DRAIN_DEADLINE": "2s"})
	addr := listening(t, ready, done)
	stop()
	time.Sleep(100 * time.Millisecond)
	if code, _, err := get(addr, "/work?ms=0"); err != nil || code != 200 {
		t.Fatalf("a new request 100 ms after the stop: %d, %v; want 200", code, err)
	}
	inFlight := make(chan int, 1)
	go func() {
		code, _, _ := get(addr, "/work?ms=200") // still running when the 300 ms are over
		inFlight <- code
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not return within 3 s of the stop")
	}
	if code := <-inFlight; code != 200 {
		t.Errorf("the request in flight ended with %d, want 200", code)
	}
	if !refused(addr) {
		t.Error("a dial after serve returned was not refused")
	}
}

func TestDrainNow(t *testing.T) {
	ready, stop, done := launch(t, map[string]string{"ADDR": "127.0.0.1:0", "SHUTDOWN": "drain-now",
		"DRAIN_DEADLINE": "2s"})
	addr := listening(t, ready, done)
	inFlight := make(chan int, 1)
	go func() {
		code, _, _ := get(addr, "/work?ms=200")
		inFlight <- code
	}()
	time.Sleep(50 * time.Millisecond)
	stop()
	time.Sleep(50 * time.Millisecond)
	if !refused(addr) {
		t.Error("a new connection right after the stop was not refused")
	}
	if code := <-inFlight; code != 200 {
		t.Errorf("the request in flight ended with %d, want 200", code)
	}
	if err := <-done; err != nil {
		t.Errorf("serve: %v", err)
	}
}

func TestExitModeRegistersNoHandler(t *testing.T) {
	if handlesTerm("exit") {
		t.Error("handlesTerm(exit) is true")
	}
	if !handlesTerm("drain-now") || !handlesTerm("helper") {
		t.Error("handlesTerm is false for drain-now or helper")
	}
}

func TestBadSettings(t *testing.T) {
	for _, env := range []map[string]string{
		{"ADDR": "127.0.0.1:0", "SHUTDOWN": "linger"},
		{"ADDR": "127.0.0.1:0", "SHUTDOWN": "helper", "START_DELAY": "soon"},
		{"ADDR": "127.0.0.1:0", "SHUTDOWN": "helper", "DRAIN_DEADLINE": "10"},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := serve(ctx, func(k string) string { return env[k] }, nil)
		cancel()
		if err == nil {
			t.Errorf("%v: serve returned nil, want an error", env)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./fixtures/service/ -v`
Expected: FAIL: `undefined: serve`

- [ ] **Step 3: Copy the experiments' service into `fixtures/service/main.go` and change it**

Copy the experiment's service source, a file of the incubating repository that is not published, unchanged, then make exactly these changes:

1. The package comment: its first line says the file is the service of the experiments that the project grew from (section 3), changed so that a test can run it; the `VERSION` line becomes `version is built in: -ldflags "-X main.version=…" (default dev), so that two builds are two images`.
2. Add `var version = "dev"` at package level, and delete the reading of `VERSION`.
3. `durEnv` becomes `func durEnv(env func(string) string, name string, def time.Duration) (time.Duration, error)`: it reads with env, and a value that does not parse returns `fmt.Errorf("bad %s=%q: %v", name, v, err)` in place of `log.Fatalf`.
4. Add `func handlesTerm(mode string) bool`: true for `drain-now` and `helper`, false otherwise.
5. Everything in main from `pod, _ := os.Hostname()` to the end moves into `func serve(ctx context.Context, env func(string) string, ready chan<- string) error`, which:
   - reads every setting with env in place of `os.Getenv`, and returns an error, before anything listens, for a `SHUTDOWN` other than `exit`, `drain-now` and `helper` (`bad SHUTDOWN=%q`) and for a duration that does not parse;
   - has no `signal.Notify` and no term channel: ctx ending is the stop;
   - after the start delay, listens with `ln, err := net.Listen("tcp", addr)` and returns its error, sends `ln.Addr().String()` on ready when ready is not nil, and serves with `srv.Serve(ln)` in the goroutine (the `http.Server` gets no Addr);
   - waits in its select for an error of Serve, which it returns, or for `ctx.Done()`;
   - then, as before, sleeps `SERVE_AFTER_TERM` in helper mode and calls `Shutdown` under `DRAIN_DEADLINE`; an error of Shutdown other than `http.ErrServerClosed` is returned in place of `os.Exit(1)`; at the end it returns nil.
   The log lines stay as they are, except that `got %v` with the signal becomes `stopping`.
6. main becomes:

```go
func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.LUTC)
	ctx := context.Background()
	mode := os.Getenv("SHUTDOWN")
	if mode == "" {
		mode = "exit"
	}
	// Only modes that handle SIGTERM register for it. In exit mode no handler
	// is registered, so the Go runtime ends the process on SIGTERM.
	if handlesTerm(mode) {
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
		defer stop()
	}
	if err := serve(ctx, os.Getenv, nil); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./fixtures/service/ -v`
Expected: PASS

- [ ] **Step 5: Write `fixtures/service/Dockerfile`**

```dockerfile
FROM scratch
COPY svc /svc
USER 65532:65532
ENTRYPOINT ["/svc"]
```

The binary is built outside it. Check that it builds static:

Run: `env CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=previous" -o bin/fixture-previous/svc ./fixtures/service`
Expected: no output.

Run: `file bin/fixture-previous/svc`
Expected: a line that holds `statically linked`.

- [ ] **Step 6: Write the chart**

`fixtures/chart/Chart.yaml`:

```yaml
apiVersion: v2
name: orders
description: The fixture service of rollproof's own tests.
type: application
version: 0.1.0
```

`fixtures/chart/values.yaml`:

```yaml
# image.repository and image.digest are required; the schema rejects them empty.
image:
  repository: ""
  digest: ""
replicas: 2
# readiness adds the readiness probe on /readyz.
readiness: true
startDelay: "0s"
# shutdown is exit, drain-now or helper.
shutdown: helper
serveAfterTerm: "5s"
drainDeadline: "10s"
# spread forbids two pods on one node.
spread: true
gracePeriod: 30
```

`fixtures/chart/values.schema.json` (with no `$schema` key, so that Helm's validator never fetches a meta-schema):

```json
{
  "type": "object",
  "required": ["image"],
  "properties": {
    "image": {
      "type": "object",
      "required": ["repository", "digest"],
      "properties": {
        "repository": { "type": "string", "minLength": 1 },
        "digest": { "type": "string", "pattern": "^sha256:[a-f0-9]{64}$" }
      }
    },
    "replicas": { "type": "integer", "minimum": 1 },
    "readiness": { "type": "boolean" },
    "startDelay": { "type": "string" },
    "shutdown": { "enum": ["exit", "drain-now", "helper"] },
    "serveAfterTerm": { "type": "string" },
    "drainDeadline": { "type": "string" },
    "spread": { "type": "boolean" },
    "gracePeriod": { "type": "integer", "minimum": 0 }
  }
}
```

`fixtures/chart/templates/deployment.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: orders
  labels:
    app.kubernetes.io/name: orders
spec:
  replicas: {{ .Values.replicas }}
  selector:
    matchLabels:
      app.kubernetes.io/name: orders
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0
  template:
    metadata:
      labels:
        app.kubernetes.io/name: orders
    spec:
      terminationGracePeriodSeconds: {{ .Values.gracePeriod }}
      automountServiceAccountToken: false
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532
        runAsGroup: 65532
        seccompProfile:
          type: RuntimeDefault
      {{- if .Values.spread }}
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchLabels:
                  app.kubernetes.io/name: orders
              topologyKey: kubernetes.io/hostname
      {{- end }}
      containers:
        - name: orders
          image: "{{ .Values.image.repository }}@{{ .Values.image.digest }}"
          imagePullPolicy: IfNotPresent
          ports:
            - name: http
              containerPort: 8080
          env:
            - name: START_DELAY
              value: {{ .Values.startDelay | quote }}
            - name: SHUTDOWN
              value: {{ .Values.shutdown | quote }}
            - name: SERVE_AFTER_TERM
              value: {{ .Values.serveAfterTerm | quote }}
            - name: DRAIN_DEADLINE
              value: {{ .Values.drainDeadline | quote }}
          securityContext:
            allowPrivilegeEscalation: false
            capabilities:
              drop: ["ALL"]
          {{- if .Values.readiness }}
          readinessProbe:
            httpGet:
              path: /readyz
              port: 8080
            periodSeconds: 1
            failureThreshold: 1
          {{- end }}
```

`fixtures/chart/templates/service.yaml`:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: orders
  labels:
    app.kubernetes.io/name: orders
spec:
  type: ClusterIP
  selector:
    app.kubernetes.io/name: orders
  ports:
    - name: http
      port: 80
      targetPort: 8080
```

- [ ] **Step 7: Check the chart with both Helm majors**

No cluster is needed. Run each command below once with `.tools/helm-3/helm` and once with `.tools/helm-4/helm` in its place.

Run: `.tools/helm-3/helm lint fixtures/chart --set image.repository=rollproof.test/x --set image.digest=sha256:0000000000000000000000000000000000000000000000000000000000000000`
Expected: the last line is `1 chart(s) linted, 0 chart(s) failed`.

Run: `.tools/helm-3/helm template orders fixtures/chart --set image.repository=rollproof.test/x --set image.digest=sha256:0000000000000000000000000000000000000000000000000000000000000000 | grep -c readinessProbe`
Expected: `1`

Run: `.tools/helm-3/helm template orders fixtures/chart --set image.repository=rollproof.test/x --set image.digest=sha256:0000000000000000000000000000000000000000000000000000000000000000 --set readiness=false | grep -c readinessProbe`
Expected: `0`

Run: `.tools/helm-3/helm template orders fixtures/chart --set image.repository=rollproof.test/x --set image.digest=sha256:0000000000000000000000000000000000000000000000000000000000000000 --set spread=false | grep -c podAntiAffinity`
Expected: `0`

Run: `.tools/helm-3/helm template orders fixtures/chart --set image.repository=rollproof.test/x --set image.digest=sha256:abc`
Expected: an error that holds `values don't meet the specifications of the schema(s)`, and a non-zero exit.

- [ ] **Step 8: Commit**

```bash
git add fixtures/service/main.go fixtures/service/Dockerfile fixtures/service/main_test.go fixtures/chart
git commit -m "Add the fixture service and its chart"
```

### Task 36: The harness, and the first four fixtures under both Helm majors

**Files:**
- Create: `e2e/harness_test.go`, `e2e/fixtures_test.go` (both with the build tag `e2e`), `e2e/doc.go`
- Modify: `README.md`
- Modify: `docs/proofs/README.md`

**Interfaces:**
- Consumes: the binary of Task 34, built with the command of the Global Constraints; `module fixtures/service` and `module fixtures/chart` (Task 35); the Helm binaries under `.tools/` (Task 2); the keys of `result.json` (Task 30).
- Produces: `func runFixture(t *testing.T, f fixture) result`
  Runs the real binary once on one fixture, with one Helm binary first on
  the PATH, and returns what result.json says and the exit code. It builds
  the binary and the two fixture images once for the whole test run.
- Produces: `type fixture struct { name string; previous, candidate map[string]any; workers int; limits map[string]string; helm string }`
  One row of section 12: the chart values of each release, the cluster's
  size, the limits that differ from the defaults, and which Helm binary.
- Produces: `type result struct { exit int; verdict string; codes []string; decided string; windows map[string]string }`
  What a fixture run gave: the exit code, the verdict, the set of reason
  codes, the deciding code, and each window's result by name.

Read first: section 12 ("With a cluster", the first four rows of the list, the paragraph on which code a row expects, "The suite's matrix"), section 14 point 5 (the Helm 4 conflict to look for) and section 13 (build-order step 3).

This task needs a container engine, the Helm binaries of Task 2 under `.tools/`, and one cluster at a time.

- [ ] **Step 1: Write `e2e/doc.go`**

```go
// Package e2e runs the real rollproof binary against the fixture service,
// one fixture of the design's section 12 at a time, under Helm 3 and Helm 4.
//
// Its tests carry the build tag e2e. They need Linux, a container engine
// that kind supports (docker, nerdctl or podman), and the Helm binaries that
// `go run ./hack/fetchhelm` puts under .tools/. A run takes about 4 minutes.
// Each run creates the cluster rollproof-e2e and deletes it at its end, so
// the suite runs one test at a time:
//
//	go test -tags e2e -p 1 ./e2e/ -v -timeout 120m
package e2e
```

- [ ] **Step 2: Write the harness in `e2e/harness_test.go`**

It starts with `//go:build e2e`. Package variables `root` (the project's folder: `filepath.Abs("..")` from `e2e/`) and `engine` (the engine's command name).

`func TestMain(m *testing.M)`, in this order; a failure prints the command and its output to standard error and exits 1:

1. The engine: `KIND_EXPERIMENTAL_PROVIDER` when it is docker, podman or nerdctl; else the first of docker, nerdctl and podman whose `ENGINE info` exits 0.
2. `.tools/helm-3/helm` and `.tools/helm-4/helm` exist under root; when one does not, the message is `the Helm binaries are missing under .tools/; run: go run ./hack/fetchhelm`.
3. The binary: `go build -trimpath -ldflags "-s -w -X main.version=dev" -o bin/rollproof ./cmd/rollproof` in root, with `CGO_ENABLED=0` added to the command's environment.
4. For VERSION `previous` and then `candidate`: a new temporary folder DIR; `go build -trimpath -ldflags "-s -w -X main.version=VERSION" -o DIR/svc ./fixtures/service` in root with `CGO_ENABLED=0`; a copy of `fixtures/service/Dockerfile` in DIR; `ENGINE build -t rollproof-fixture:VERSION DIR`.
5. `os.Exit(m.Run())`.

The run description and the part of `result.json` that the fixtures read beyond `result`:

```go
// configTemplate is the run description of one fixture, for text/template.
// Workers is f.workers, or 3 when it is 0. Chart is the absolute path of
// fixtures/chart as a JSON string. Previous and Candidate are "" when the
// fixture gives that release no values, else ", " and the values as a JSON
// object. Limits is "" when the fixture changes no limit, else "limits: " and
// f.limits as a JSON object.
const configTemplate = `version: 1
cluster: { workers: {{.Workers}} }
images:
  previous:  { local: "rollproof-fixture:previous" }
  candidate: { local: "rollproof-fixture:candidate" }
deploy:
  namespace: orders
  helm:
    release: orders
    previous:  { chart: {{.Chart}}, values: [ { image: { repository: "${images.previous.repository}", digest: "${images.previous.digest}" } }{{.Previous}} ] }
    candidate: { chart: {{.Chart}}, values: [ { image: { repository: "${images.candidate.repository}", digest: "${images.candidate.digest}" } }{{.Candidate}} ] }
probe:
  http: { url: "http://orders.orders.svc/work?ms=20" }
{{.Limits}}
`

// transitionResult is one entry of result.json's transitions, as far as the
// fixtures read it.
type transitionResult struct {
	Name      string `json:"name"`
	Workloads []struct {
		Workload       string `json:"workload"`
		PodsReplaced   int    `json:"pods_replaced"`
		Probed         string `json:"probed"`
		QuickestStopMs *int64 `json:"quickest_stop_ms"`
	} `json:"workloads"`
}
```

`func runFixture(t *testing.T, f fixture) result`, with its interface comment from the Interfaces block:

1. `out := outDir(t)`; remove it, then create it.
2. `t.Cleanup`: when `t.Failed()`, `t.Logf` the path of out and the last 40 lines of `out/evidence/probe.log`.
3. Write `rollproof.yaml` into `t.TempDir()` from `configTemplate` with `text/template`, with the data its comment gives; the chart is `filepath.Join(root, "fixtures", "chart")`.
4. A new temporary folder that holds a symbolic link named `helm` to `root/.tools/HELM/helm`, where HELM is `f.helm` (`helm-3` or `helm-4`). The command's environment is `os.Environ()` with PATH set to that folder, a colon and the PATH before.
5. Run `root/bin/rollproof run -f FILE --out OUT --name e2e` under a context of 15 minutes, with standard output and standard error into one buffer that goes to `t.Logf` with the fixture's name and its Helm after the run.
6. The exit code is 0, or `ExitCode()` of an `*exec.ExitError`; any other error fails the test.
7. Read `OUT/result.json`; when it is missing, or its `final` is false, `t.Fatalf`.
8. Fill the result: exit, verdict, codes (the reasons' codes, sorted, each once), decided (the code of the reason whose `decided` is true, or ""), windows (each window's name to its result).

`func outDir(t *testing.T) string` is `filepath.Join(os.TempDir(), "rollproof-e2e", strings.ReplaceAll(t.Name(), "/", "-"))`. It is not a `t.TempDir()`, because the folder stays after the test, so that the path printed on a failure still leads to the evidence.

`func readTransitions(t *testing.T, out string) []transitionResult` reads the key `transitions` of `out/result.json`.

- [ ] **Step 3: Write the fixtures in `e2e/fixtures_test.go`**

```go
//go:build e2e

package e2e

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// underBothHelms runs f once with each Helm major, as two subtests, and hands
// each result to check.
func underBothHelms(t *testing.T, f fixture, check func(t *testing.T, r result)) {
	for _, helm := range []string{"helm-3", "helm-4"} {
		t.Run(helm, func(t *testing.T) {
			g := f
			g.helm = helm
			check(t, runFixture(t, g))
		})
	}
}

// expect compares what a run gave with what its row of section 12 expects.
func expect(t *testing.T, got, want result) {
	t.Helper()
	if diff := cmp.Diff(want, got, cmp.AllowUnexported(result{}), cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("result (-want +got):\n%s", diff)
	}
}

// Both releases report ready only when they can serve, and after SIGTERM
// serve on and then drain: pass.
func TestFirstFixture(t *testing.T) {
	underBothHelms(t, fixture{name: "first"}, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass",
			windows: map[string]string{"steady": "pass", "deploy": "pass", "rollback": "pass"}})
		seen := 0
		for _, tr := range readTransitions(t, outDir(t)) {
			for _, w := range tr.Workloads {
				if w.Workload != "deployment/orders" {
					continue
				}
				seen++
				if w.PodsReplaced != 2 || w.Probed != "yes" || w.QuickestStopMs == nil || *w.QuickestStopMs < 5000 {
					t.Errorf("%s: deployment/orders replaced %d, probed %q, quickest stop %v ms; want 2, yes, 5000 or more",
						tr.Name, w.PodsReplaced, w.Probed, w.QuickestStopMs)
				}
			}
		}
		if seen != 2 {
			t.Errorf("deployment/orders is in %d transitions, want 2", seen)
		}
	})
}

// The candidate has no readiness probe: fail in the deploy window; the
// rollback is not run.
func TestCandidateWithoutReadiness(t *testing.T) {
	f := fixture{name: "candidate-without-readiness",
		candidate: map[string]any{"readiness": false, "startDelay": "5s"}}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 1, verdict: "fail", codes: []string{"probe-failures"}, decided: "probe-failures",
			windows: map[string]string{"steady": "pass", "deploy": "fail", "rollback": "not-run"}})
	})
}

// The candidate exits on SIGTERM: fail in the rollback window.
func TestCandidateExitsOnSIGTERM(t *testing.T) {
	f := fixture{name: "candidate-exits-on-sigterm", candidate: map[string]any{"shutdown": "exit"}}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 1, verdict: "fail", codes: []string{"probe-failures"}, decided: "probe-failures",
			windows: map[string]string{"steady": "pass", "deploy": "pass", "rollback": "fail"}})
	})
}

// No node is free for the extra pod: fail, the deploy stalls, and
// pod-unschedulable is listed beside the stall.
func TestNoRoomForTheExtraPod(t *testing.T) {
	f := fixture{name: "no-room-for-the-extra-pod",
		previous:  map[string]any{"replicas": 3},
		candidate: map[string]any{"replicas": 3},
		workers:   3,
		limits:    map[string]string{"timeout": "90s"}}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 1, verdict: "fail",
			codes: []string{"pod-unschedulable", "transition-stalled"}, decided: "transition-stalled",
			windows: map[string]string{"steady": "pass", "deploy": "fail", "rollback": "not-run"}})
	})
}
```

- [ ] **Step 4: Check that the suite builds**

Run: `go vet -tags e2e ./e2e/`
Expected: no output.

Run: `go test ./...`
Expected: PASS, with `rollproof/e2e [no test files]` among the lines: without the tag only `doc.go` is built.

- [ ] **Step 5: Run the first fixture alone**

Run: `go test -tags e2e -p 1 ./e2e/ -run TestFirstFixture -v -timeout 120m`
Expected: PASS for `TestFirstFixture/helm-3` and `TestFirstFixture/helm-4`.

- [ ] **Step 6: Run the whole suite**

Run: `go test -tags e2e -p 1 ./e2e/ -v -timeout 120m`
Expected: PASS for all eight subtests.

If a fixture gives another verdict, do not change the expectation. Read the evidence in the folder that the failure prints, find whether the tool or the fixture is wrong, and fix that. If the design itself is contradicted, for example when the first fixture fails under Helm 4 with a conflict over a field (section 14, point 5), stop and report it to the maintainer.

- [ ] **Step 7: Say in `README.md` what works now**

Replace the line that begins "Nothing is released" with these three paragraphs:

```markdown
Nothing is released; the design is docs/specs/2026-11-13-rollproof-design.md.

What works now, on Linux with nerdctl: `rollproof run` creates its own kind cluster, installs a previous release with Helm, deploys the candidate and rolls it back, all under the built-in HTTP probe. The first four fixtures of the design's section 12 give their expected verdicts under Helm 3.22.1 and Helm 4.3.2 (`go test -tags e2e -p 1 ./e2e/`).

Not built yet: `registries`, `setup`, `deploy.commands`, `probe.custom`, `archive` and `as` in `images`, and a Helm run with no `previous`. A configuration that names one of them ends as `config-rejected`, with the text "not built yet" and the key's name.
```

- [ ] **Step 8: Record the fixture runs in `docs/proofs/README.md`**

Under the heading "Still unproved", add this item, with the engine, its version and the date of the runs in place of the capitals:

```markdown
- Point 5, the fixture runs: the first four fixtures ran under Helm 3.22.1 and under Helm 4.3.2 on ENGINE VERSION on DATE. Helm 4 applied every object, the rollback included, with no conflict over a field.
```

When a run found a conflict, the item names the object, the field and the fixture in place of its last sentence.

- [ ] **Step 9: Commit**

```bash
git add e2e/doc.go e2e/harness_test.go e2e/fixtures_test.go README.md docs/proofs/README.md
git commit -m "Run the first four fixtures against the real binary, under Helm 3 and Helm 4"
```

### Task 37: The workflow for the unit tests

**Files:**
- Create (repository root): `.github/workflows/rollproof-unit.yml`

**Interfaces:**
- Consumes: the module's unit tests (Tasks 12 to 35); the pinned `uses:` lines of `.github/workflows/rollproof-proofs.yml` (Task 4).
- Produces: `workflow .github/workflows/rollproof-unit.yml`
  Runs the project's unit tests on a pull request that changes a file of
  the project or this workflow. It runs no test that needs a container
  engine.

Read first: section 13 ("During incubation": where workflows sit, what runs on a pull request).

Paths and commands in this task are from the repository root, except in Step 3, which runs from `projects/rollproof/`.

- [ ] **Step 1: Write `.github/workflows/rollproof-unit.yml`**

`CHECKOUT` and `SETUP_GO` stand for the two `uses:` values of `.github/workflows/rollproof-proofs.yml` (Task 4): copy each with its commit and its comment, exactly as that file has it.

```yaml
name: rollproof unit tests

on:
  pull_request:
    paths:
      - "projects/rollproof/**"
      - ".github/workflows/rollproof-unit.yml"

permissions:
  contents: read

defaults:
  run:
    shell: bash
    working-directory: projects/rollproof

jobs:
  unit:
    runs-on: ubuntu-24.04
    timeout-minutes: 15
    steps:
      - uses: CHECKOUT
        with:
          persist-credentials: false
      - uses: SETUP_GO
        with:
          go-version: "1.27"
          cache-dependency-path: projects/rollproof/go.sum
      - name: Vet
        run: go vet ./...
      - name: Unit tests
        run: go test ./...
      - name: Build
        run: env CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=dev" -o bin/rollproof ./cmd/rollproof
      - name: The binary is statically linked
        run: |
          file bin/rollproof
          file bin/rollproof | grep -q "statically linked"
```

No step needs a container engine: `go test ./...` runs no file with the tag `cluster` or `e2e`.

- [ ] **Step 2: Check the workflow without running it on GitHub**

Run: `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/rollproof-unit.yml .github/workflows/rollproof-proofs.yml`
Expected: no output, exit code 0.

Run: `go run github.com/rhysd/actionlint/cmd/actionlint@latest -version`
Expected: a first line with the version that `@latest` resolved; note it for the commit message.

- [ ] **Step 3: Run the workflow's commands on this machine**

From `projects/rollproof/`:

Run: `go vet ./...`
Expected: no output.

Run: `go test ./...`
Expected: `ok` for every package with tests.

Run: `env CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=dev" -o bin/rollproof ./cmd/rollproof`
Expected: no output.

Run: `file bin/rollproof`
Expected: a line that holds `statically linked`.

- [ ] **Step 4: Run the citation check of the host repository**

Run: `bun tools/citations/cli.ts repo`
Expected: `citation check: 0 errors, 0 warnings`

- [ ] **Step 5: Commit**

VERSION is the version that Step 2 noted.

```bash
git add .github/workflows/rollproof-unit.yml
git commit -m "Run rollproof's unit tests on pull requests that touch the project" -m "Checked with actionlint VERSION."
```

The workflow first runs when the branch is pushed. Pushing waits for the maintainer's word: do not push.
