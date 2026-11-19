# Step 4 plan, part 6: the harness, the fixture probe and the chart's variants

### Task 20: kubectl in `.tools/`, the harness's new fields and actions, `GET /version`, the fixture probe, busybox's archive

Design: section 12 ("With a cluster", the table and every paragraph under it, "The suite's matrix"); section 6 ("The contract": "Lines", "Lifecycle", "Where probe pods run", and the TCP loop under `internal/probe`); section 7 ("Where commands run", "The namespace", "The commands driver"); section 8 ("The config file", "Substitution"); section 16, the run of 2026-11-19 (busybox 1.37.0 as user 65532). README decisions 13, 18, 19, 20, 21.

**Files:**
- Modify: `hack/fetchhelm/main.go` (kubectl), `hack/fetchhelm/main_test.go`
- Modify: `fixtures/service/main.go` (`GET /version`), `fixtures/service/main_test.go`
- Create: `fixtures/probe/main.go`, `fixtures/probe/main_test.go`, `fixtures/probe/Dockerfile`
- Create: `e2e/config_test.go` (no build tag: the run description, the window marks and the two actions, tested without a cluster)
- Modify: `e2e/harness_test.go` (TestMain's new images and archives, `runFixture`'s new fields and the action loop, `readRecord`, `readConsole`, `checkClusterKept`, `removeKeptCluster`), `e2e/doc.go` (`-timeout 360m`)

**Interfaces:**
- Consumes: `config.Load(path string, env func(name string) (string, bool)) (*config.Config, error)` and `(*config.Config).NotBuilt() []string`, with `setup`, `deploy.commands`, `deploy.helm.previous`, `probe.custom` and `images.*.archive` off that list (Tasks 1, 13, 14, 15); `evidence/windows.json` rewritten whole at each mark, each entry with `name`, `start` and the list `finishes` (Task 17, decision 13); `result.json`'s `restart`, `program`, `driver` `"commands"`, probe kind `"custom"` (Tasks 14, 15); the binary's end by SIGINT (as built).
- Produces: `func fetchKubectl(base, arch, sum, dir string) error`
  Puts kubectl 1.37.0 for linux/arch at dir/kubectl with mode 0755, from
  base/release/v1.37.0/bin/linux/ARCH/kubectl, checked against sum, the
  SHA-256 of the binary. A binary whose record dir/kubectl.sha256 names sum
  and matches the file is not fetched again. An arch other than amd64, or a
  mismatch, is an error that names "kubectl 1.37.0" and leaves no file.
- Produces: `GET /version`
  The fixture service answers 200 with its built-in version and a line end,
  whatever FAIL_WORK says.
- Produces: `environment TARGET, VERSION, ON_VERSION, FAIL_FIRST, IGNORE_TERM`
  The fixture probe's modes. TARGET is the Service's base URL, default
  http://orders.orders.svc. ON_VERSION is silent or exit: what the probe does
  when a check first reads VERSION. FAIL_FIRST is a duration of failed-check
  lines, one every 2 ms, before the first check. IGNORE_TERM true ignores
  SIGTERM.
- Produces: `func run(env func(string) string, out io.Writer, term <-chan struct{}) error`
  The fixture probe: one check of GET TARGET/version after another, a line
  of the probe contract on out for each, each in one write. It returns nil
  when term is closed (a nil term never is) or when ON_VERSION=exit has read
  VERSION; an unknown ON_VERSION or a bad duration is an error at once.
- Produces: `func ignoresTerm(env func(string) string) bool`
  Reports whether IGNORE_TERM is true, so that main ignores SIGTERM and
  passes run a nil term.
- Produces: `type customProbe struct { image string; command []string; env map[string]string }`
  A fixture's probe.custom. image is the images entry it runs: "probe", the
  fixture probe built as rollproof-fixture-probe:dev, or "busybox", busybox
  1.37.0 from its archive. command and env are the container's; env's
  entries go into the file sorted by name.
- Produces: `type fixture struct { …; nodeImage string; noPrevious, candidateRunsPrevious bool; setup [][]string; commands []string; custom *customProbe; act action; keepsCluster bool }`
  The fields beside today's: cluster.node-image ("" is kind's default); a
  Helm run with no previous release; a candidate whose image values name the
  previous image; the setup commands; the commands driver's roles in order
  (nil is the Helm driver); a custom probe (nil is the built-in one); an
  action on the running run; a run that is to leave its cluster behind.
- Produces: `type action func(r running, marks []mark, now time.Time) (done bool, err error)`
  Something the harness does to a running run. It is called every 100 ms
  with the latest whole reading of evidence/windows.json and the harness's
  clock, until it reports done, returns an error, or the run ends. A run
  that ends before its action reported done fails the test.
- Produces: `type running struct { signal func(os.Signal) error; kubectl func(args ...string) error }`
  A run in progress as an action reaches it: signal sends a signal to the
  tool's process; kubectl runs .tools/kubectl with args against the test
  cluster's kubeconfig and returns its error with its output.
- Produces: `type mark struct { Name string; Start *time.Time; Finishes []time.Time }`
  One window of evidence/windows.json, as far as the harness reads it: its
  name, when it began (nil before it did), and each moment its transition
  came to be finished, in order.
- Produces: `func readMarks(path string) ([]mark, error)`
  Reads the window marks at path. A file that is missing, empty or cut
  short (being rewritten) is an error, which the action loop skips.
- Produces: `func restartAfterTheDeploysFirstFinish(delay time.Duration, at *time.Time) action`
  The action of row 13: once the deploy's first finish plus delay has come,
  it runs `kubectl rollout restart deployment/orders --namespace orders`,
  sets *at to that moment and reports done.
- Produces: `func interruptDuringTheDeploy(r running, marks []mark, now time.Time) (bool, error)`
  The action of row 26: once the deploy window has begun, it sends the tool
  SIGINT and reports done.
- Produces: `func renderConfig(f fixture, in inputs) (string, error)`
  The text of f's rollproof.yaml, every block as JSON: the cluster, images,
  setup, the Helm releases or the commands, the probe and the limits. in
  gives the chart's absolute path and the archives TestMain prepared. The
  commands driver applies previous.yaml and candidate.yaml beside the file.
- Produces: `func readRecord(t *testing.T, out string) record`
  out/result.json as the step 4 rows read it: driver, name, reasons with
  their windows, windows with their counts, the install record, transitions
  with command, restart, settle restarts and workloads, probes, releases,
  images and cluster. Fails the test when the file cannot be read.
- Produces: `var archiveDigests map[string]string`
  The digest of each fixture image's archive that TestMain saved, by images
  entry: previous and candidate, with the sha256: prefix, as the tool stores
  an archive under it.
- Produces: `func readConsole(t *testing.T) string`
  The console text of the test's last run, which runFixture keeps in the
  results folder as console.txt.
- Produces: `result.signal string`
  The name of the signal that ended the tool, such as "interrupt"; empty
  when it exited by itself. exit is then -1.

- [ ] **Step 1: Write the failing tests**
  - `hack/fetchhelm/main_test.go`. A server like `server`'s serves `/release/v1.37.0/bin/linux/amd64/kubectl` with the bytes `kubectl\n` and counts requests; `sum` is their SHA-256.
    - `TestFetchKubectl`: `fetchKubectl(base, "amd64", sum, dir)` returns nil; `dir/kubectl` holds `kubectl\n` with mode 0755.
    - `TestFetchKubectlRejectsAWrongChecksum`: with 64 zeros as sum, the error holds `kubectl 1.37.0` and `checksum mismatch`, and `os.ReadDir(dir)` is empty.
    - `TestFetchKubectlSkipsAPresentBinary`: a second call makes no request.
    - `TestFetchKubectlRefusesAnotherArchitecture`: arch `arm64` gives exactly `kubectl 1.37.0: no checksum for architecture arm64`, and no request.
  - `fixtures/service/main_test.go`, `TestVersion`: `version = "v-test"`, `FAIL_WORK=true`; `get(addr, "/version")` gives 200 and the body exactly `"v-test\n"`.
  - `fixtures/probe/main_test.go`. `target(t, bodies ...string)` starts an `httptest.Server` that answers `/version` with the bodies in turn, the last one for good, and records each request's time; `lines(buf)` splits the output and decodes each line as JSON, failing the test on one that is not an object.
    - `TestWritesALinePerCheck`: target `previous\n`; run with `TARGET` its URL; close term after 300 ms. Assert: run returns nil within 1 s of the close; at least 3 lines; each has `ok` true and `ms` 0 or more, and no other field than `ok` and `ms`.
    - `TestFailedCheckLines`: the server answers 503. Assert every line is `{"ok":false,"class":"503","ms":…}`.
    - `TestSilentOnceItReadsTheVersion`: bodies `previous\n` three times, then `candidate\n`; `ON_VERSION=silent`, `VERSION=candidate`. Assert: after 500 ms exactly 3 lines, after 500 ms more still 3; run returns nil within 1 s of term's close.
    - `TestExitsOnceItReadsTheVersion`: the same bodies, `ON_VERSION=exit`. Assert: run returns nil within 2 s with term never closed, and exactly 3 lines.
    - `TestFailsFirstEveryTwoMilliseconds`: `FAIL_FIRST=200ms`, target `previous\n`; close term after 500 ms. Assert: the first 80 to 101 lines are exactly `{"ok":false,"class":"fixture"}`, every later line has `ok` true, and the server's first request came 200 ms or more after run began.
    - `TestRejectsAnUnknownOnVersion`: `ON_VERSION=bogus` returns an error that holds `ON_VERSION` at once and writes nothing.
    - `TestIgnoresTermOnlyWhenAsked`: `ignoresTerm` is true for `IGNORE_TERM=true`, false for an empty value and for `false`.
  - `e2e/config_test.go` (no build tag, package `e2e`). Two archives are random images (`random.Image(1024, 1)`) written with `tarball.WriteToFile` into `t.TempDir()`; `in := inputs{chart: <abs fixtures/chart>, archives: {"previous": a, "candidate": b, "busybox": c}}`.
    - `TestConfigOfEachShapeLoads`, a table; each case renders with `renderConfig`, writes `rollproof.yaml` into `t.TempDir()` and loads it with `config.Load(path, os.LookupEnv)`. Every case: no error, and `cfg.NotBuilt()` empty. Then:

      | Case | Fixture | Assert |
      | --- | --- | --- |
      | both releases | `fixture{}` | `Helm.Previous != nil`, `Probe.HTTP.URL == "http://orders.orders.svc/work?ms=20"`, `Images["previous"].Local == "rollproof-fixture:previous"`, `Cluster.Workers == 3` |
      | no previous | `noPrevious: true` | `Helm.Previous == nil`, `Helm.Candidate != nil` |
      | candidate runs previous | `candidateRunsPrevious: true` | the text holds `"${images.previous.digest}"` twice and not `"${images.candidate.digest}"` |
      | node image | `nodeImage: "kindest/node:v1.34.11@sha256:44e222ee2132dab25ff87301682f89eb82c7880ea3a1bf543bfe9708fd08d67d"` | `Cluster.NodeImage` is that reference |
      | setup | `setup: [][]string{{"kubectl", "get", "namespace", "orders"}}` | `cfg.Setup` equals it |
      | three commands | `commands: []string{"install", "deploy", "rollback"}` | `Deploy.Helm == nil`; `Install` and `Rollback` are `[kubectl apply -f previous.yaml]`, `Deploy` is `[kubectl apply -f candidate.yaml]`; `Images["previous"].Archive == a` |
      | two commands | `commands: []string{"install", "deploy"}` | `Rollback == nil` |
      | fixture probe | `custom: &customProbe{image: "probe", env: {"VERSION": "candidate", "ON_VERSION": "silent"}}` | `Probe.HTTP == nil`, `Custom.Image == "${images.probe.ref}"`, env names `[ON_VERSION VERSION]` in that order, `Images["probe"].Local == "rollproof-fixture-probe:dev"` |
      | TCP loop | `custom: &customProbe{image: "busybox", command: []string{"sh", "-c", tcpLoop}}` | `Custom.Image == "${images.busybox.ref}"`, `Custom.Command` equals it, `Images["busybox"].Archive == c` |
      | limits | `limits: {"settle": "30s", "timeout": "90s"}` | `Limits.Settle == 30*time.Second`, `Limits.Timeout == 90*time.Second` |

    - `TestReadMarks`: a file `[{"name":"steady","start":T0,"finishes":[]},{"name":"deploy","start":T1,"finishes":[T2,T3]},{"name":"rollback","start":null}]` reads back as three marks, deploy's `Finishes == []time.Time{T2, T3}`, rollback's `Start == nil`; the same text cut at its middle gives an error; a missing file gives an error with `errors.Is(err, fs.ErrNotExist)`.
    - `TestInterruptWaitsForTheDeploysStart`: a `running` whose `signal` records what it got. With marks whose deploy has `Start` nil: `(false, nil)` and nothing sent. With deploy's `Start` set: `(true, nil)` and exactly `os.Interrupt` sent once.
    - `TestRestartComesAfterTheDelay`: a `running` whose `kubectl` records its arguments; `var at time.Time`; `act := restartAfterTheDeploysFirstFinish(5*time.Second, &at)`; deploy's `Finishes` is `[F]`. At `now = F+4.9s`: `(false, nil)`, kubectl not called. At `now = F+5.1s`: `(true, nil)`, kubectl called once with `[rollout restart deployment/orders --namespace orders]`, `at == now`. With no finish yet: `(false, nil)`.
- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./hack/fetchhelm/ ./fixtures/service/ ./fixtures/probe/ ./e2e/ -run 'TestFetchKubectl|TestVersion|TestWrites|TestFailed|TestSilent|TestExits|TestFailsFirst|TestRejects|TestIgnores|TestConfigOfEachShape|TestReadMarks|TestInterrupt|TestRestartComes' -v` / Expected: FAIL: the packages do not build (`fetchKubectl`, `run`, `renderConfig`, `readMarks` and the rest are not defined), and `TestVersion` gets 404.
- [ ] **Step 3: Get kubectl's checksum, measure busybox's digest, and implement**, each name with its interface comment from the Interfaces block.
  - **kubectl's checksum.** Run `curl -fsSL https://dl.k8s.io/release/v1.37.0/bin/linux/amd64/kubectl.sha256`: dl.k8s.io publishes beside each binary a file that holds its SHA-256 alone. Write that value into `hack/fetchhelm/main.go` as `kubectlSum`, beside the constant `kubectlVersion = "1.37.0"`. `main` calls `fetchKubectl("https://dl.k8s.io", runtime.GOARCH, kubectlSum, *dir)` after the Helm binaries; the record file is `kubectl.sha256` holding the sum twice (archive and binary are one file here, so `present` serves as it is). `printChecksums` also prints kubectl's computed SHA-256 and the published `.sha256`. Rewrite the package comment to name kubectl 1.37.0 at `.tools/kubectl`, for linux/amd64 only.
  - **busybox's digest.** Run `nerdctl pull --platform linux/amd64 docker.io/library/busybox:1.37.0`, then `nerdctl image inspect --format '{{index .RepoDigests 0}}' docker.io/library/busybox:1.37.0`. It prints `docker.io/library/busybox@sha256:…`, the digest of the tag's index. Write it into `e2e/harness_test.go` as `const busyboxRef = "docker.io/library/busybox@sha256:…"`, with a comment that gives the date it was measured and the two commands. Give the digest in the task's report.
  - **`GET /version`** in `fixtures/service/main.go`, and the line for it in the package comment.
  - **`fixtures/probe/main.go`**: `main` calls `ignoresTerm`; when it is true it calls `signal.Ignore(syscall.SIGTERM)` and passes run a nil term, else a channel closed on SIGTERM. `run` makes each check a GET of `TARGET + "/version"` with a client that opens a new connection for every request and has a 1 s timeout, then pauses 20 ms. A check passes on status 200 to 299; a failed check's class is the status code as a string, `timeout` for a timed-out request, `other` for any other error. Lines are exactly `{"ok":true,"ms":N}` and `{"ok":false,"class":"C","ms":N}`, with N the check's milliseconds; a `FAIL_FIRST` line is exactly `{"ok":false,"class":"fixture"}`, from a 2 ms ticker. The check that reads VERSION (the body with white space trimmed) writes no line: with `silent` the probe then writes nothing more and waits for term; with `exit` run returns nil. An error from run is printed to standard error, and the process exits 1. `fixtures/probe/Dockerfile`: `FROM scratch`, `COPY probe /probe`, `USER 65532:65532`, `ENTRYPOINT ["/probe"]`.
  - **`e2e/config_test.go`**: the types `fixture` (moved here from `harness_test.go`, with the new fields), `customProbe`, `action`, `running`, `mark`, `inputs{chart string; archives map[string]string}`; `renderConfig`, `readMarks`, the two actions, and `tcpLoop`, the loop of section 6 unchanged but for its target:

    ```go
    const tcpLoop = `trap 'exit 0' TERM
    while true; do
      if nc -z -w1 orders.orders 80; then echo '{"ok":true}'
      else echo '{"ok":false,"class":"refused"}'; fi
      sleep 0.1
    done`
    ```

    `configTemplate` becomes this; each field is a JSON text from `json.Marshal`, or empty when the block is absent:

    ```go
    const configTemplate = `version: 1
    cluster: {{.Cluster}}
    images: {{.Images}}
    {{- if .Setup}}
    setup: {{.Setup}}
    {{- end}}
    deploy:
      namespace: orders
    {{- if .Commands}}
      commands: {{.Commands}}
    {{- else}}
      helm:
        release: orders
    {{- if .Previous}}
        previous: {{.Previous}}
    {{- end}}
        candidate: {{.Candidate}}
    {{- end}}
    probe: {{.Probe}}
    {{- if .Limits}}
    limits: {{.Limits}}
    {{- end}}
    `
    ```

    Cluster is `{"workers": N}` (N is `f.workers`, or 3), with `"node-image"` when `f.nodeImage` is set. Images: `previous` and `candidate` as `{"local": "rollproof-fixture:previous"}` and `:candidate` for the Helm driver, and as `{"archive": in.archives[…]}` for the commands driver; `probe` as `{"local": "rollproof-fixture-probe:dev"}` or `busybox` as `{"archive": in.archives["busybox"]}` for a custom probe. A Helm release is `{"chart": in.chart, "values": [{"image": {"repository": "${images.E.repository}", "digest": "${images.E.digest}"}}, f.previous or f.candidate when not empty]}`, with E `previous` for the previous release and for a candidate with `candidateRunsPrevious`, else `candidate`; `noPrevious` leaves the previous release out. Commands: `install` and `rollback` are `["kubectl", "apply", "-f", "previous.yaml"]`, `deploy` is `["kubectl", "apply", "-f", "candidate.yaml"]`, for the roles in `f.commands`. Probe is `{"http": {"url": "http://orders.orders.svc/work?ms=20"}}`, or `{"custom": {"image": "${images.I.ref}", "command": …, "env": [{"name": …, "value": …}, …]}}`, with `command` and `env` left out when empty.
  - **`e2e/harness_test.go`**:
    - `TestMain` also: checks `.tools/kubectl` as it checks the Helm binaries (the message names `go run ./hack/fetchhelm`); builds `./fixtures/probe` with `-trimpath -ldflags "-s -w"` and its Dockerfile into the image `rollproof-fixture-probe:dev`, the way `buildFixture` builds the service; saves `rollproof-fixture:previous` and `:candidate` with `engine save -o` into a folder made with `os.MkdirTemp("", "rollproof-fixture-archives-")`, removed after `m.Run()`, and keeps each archive's digest, `tarball.ImageFromPath(path, nil)` then `Digest()`, in `archiveDigests`; and, when `.tools/busybox-1.37.0.tar` is missing, runs `engine pull --platform linux/amd64 busyboxRef`, `engine tag busyboxRef rollproof-fixture-busybox:1.37.0` and `engine save -o` to a temporary name beside the archive, renamed into place when the save succeeded.
    - `runFixture`: links `.tools/kubectl` into the Helm link's folder beside `helm`, so that `setup` and the commands find it on the PATH. For a fixture with `commands`, it renders, before the run and into the folder of `rollproof.yaml`, `previous.yaml` and `candidate.yaml` with `.tools/<f.helm>/helm template orders <chart> -f <values.json>`, where the values file is the release's map with `image` set to `{"repository": "rollproof.test/E", "digest": archiveDigests[E]}` and E as for `renderConfig`; there is no `--namespace` anywhere. It starts the tool with `cmd.Start` between `signal.Notify(c, os.Interrupt)` and `signal.Stop(c)`, so that the tool starts with SIGINT at its default action even when `go test` was started with SIGINT ignored. While the tool runs and `f.act` is set, it reads `out/evidence/windows.json` every 100 ms with `readMarks`, skips a reading that gives an error, and calls `f.act` with the marks and `time.Now()`; its `running` sends signals with `cmd.Process.Signal` and runs `.tools/kubectl` with `KUBECONFIG` set to `filepath.Join(os.TempDir(), "rollproof-e2e", "kubeconfig")` (the run folder that `cluster.RunDir` names). An action's error, and a run that ended before its action reported done, fail the test with the fixture's name. After `cmd.Wait`, it writes the console text to `out/console.txt`; an `*exec.ExitError` whose `syscall.WaitStatus` says `Signaled()` sets `signal` to the signal's `String()`. With `f.keepsCluster` it calls `checkClusterKept` in place of `checkClusterGone`.
    - `checkClusterKept(t, f)` fails the test unless the engine lists `rollproof-e2e-control-plane` and `rollproof-e2e-registry`; it then registers `removeKeptCluster` with `t.Cleanup`. `removeKeptCluster` runs `engine rm -f -v` on every container whose name begins `rollproof-e2e-`, then removes `filepath.Join(os.TempDir(), "rollproof-e2e")`; it removes nothing else.
    - `record`, read by `readRecord`, holds with their `result.json` keys: `driver`, `name`; `reasons[]` `code`, `window`, `text`, `detail`; `windows[]` `name`, `start`, `end`, `result`, `failed`, `failed_after_finish`, `first_failure`; `install` `first_observation`, `settled`, `last_failure`, `settle_restarted_by`, `checks`, `failed`; `transitions[]` `name`, `command` (a `json.RawMessage`), `restart` (`*string`), `settle_restarted_by`, `workloads[]` `workload`, `pods_replaced`, `pods_created`, `probed`; `probes[]` `kind`, `stopped_when_asked` (`*bool`); `releases[]` `role`, `program`; `images[]` `name`, `digest`; `cluster` `kubernetes`, `node_image`. Go field names are the keys in Go's camel case (`FailedAfterFinish`, `FirstObservation`, `SettleRestartedBy`, `PodsCreated`, `StoppedWhenAsked`, `NodeImage`); times are `*time.Time`. Methods `window(t, name)`, `transition(t, name)` and `workload(t, transition, workload)` return that entry and fail the test when it is missing.
  - **`e2e/doc.go`**: the command becomes `go test -tags e2e -p 1 ./e2e/ -v -timeout 360m`; the text says that the suite runs every row of section 12 under both Helm majors, takes about four hours, and needs `.tools/kubectl` and busybox's archive besides the Helm binaries (`go run ./hack/fetchhelm` fetches kubectl; the first run of the suite pulls busybox).
- [ ] **Step 4: Run the tests to see them pass**
  Run: `go test ./hack/fetchhelm/ ./fixtures/service/ ./fixtures/probe/ ./e2e/ -v` / Expected: PASS.
  Run: `go vet -tags e2e ./e2e/` / Expected: no output.
  Run: `go run ./hack/fetchhelm`, then `go run ./hack/fetchhelm -print-checksums` / Expected: `.tools/kubectl` exists, `.tools/kubectl version --client` prints `v1.37.0`, and the computed and published kubectl sums agree.
  Run: `env CGO_ENABLED=0 KIND_EXPERIMENTAL_PROVIDER=nerdctl go test -tags e2e -p 1 -count=1 -timeout 360m -v ./e2e/ -run 'TestFirstFixture$'` / Expected: PASS under both Helm majors (the harness's changes keep the first fixture as it was), and `.tools/busybox-1.37.0.tar` exists afterwards.
- [ ] **Step 5: Commit**
  `git add hack/fetchhelm/main.go hack/fetchhelm/main_test.go fixtures/service/main.go fixtures/service/main_test.go fixtures/probe/main.go fixtures/probe/main_test.go fixtures/probe/Dockerfile e2e/config_test.go e2e/harness_test.go e2e/doc.go` and `git commit -m "Give the fixtures kubectl, a probe program, busybox's archive, the service's version and a harness that renders every block and acts on a running run"`

### Task 21: The chart's variants and the service's switches for them

Design: section 12 (the rows of a StatefulSet, a DaemonSet, a chart that denies traffic, Helm hooks, pods that fail after the rollout finished, and a second workload); section 6 ("Where probe pods run": the rule that lets the probe in); section 7 ("What is watched", "Strategies", and the four facts about Helm hooks).

**Files:**
- Modify: `fixtures/service/main.go` (`FAIL_AFTER`, `FAIL_FOR`, `JOB_SLEEP`), `fixtures/service/main_test.go`
- Create: `fixtures/chart/templates/_pod.tpl`, `statefulset.yaml`, `daemonset.yaml`, `second.yaml`, `networkpolicy.yaml`, `hooks.yaml`
- Modify: `fixtures/chart/templates/deployment.yaml`, `fixtures/chart/values.yaml`, `fixtures/chart/values.schema.json`

**Interfaces:**
- Consumes: the chart and the service as Task 20 left them.
- Produces: `environment FAIL_AFTER, FAIL_FOR, JOB_SLEEP`
  FAIL_AFTER and FAIL_FOR are durations: from FAIL_AFTER after the server
  listens, GET /work answers 500 "failing" for FAIL_FOR, while /readyz and
  /version answer as before; empty FAIL_AFTER never fails. JOB_SLEEP set
  makes the process sleep that long and exit 0 without listening, as a Job.
- Produces: `values workload, denyIngress, allowProbe, second, secondRevision, hooks, hookSleep, failAfter, failFor`
  workload is Deployment (default), StatefulSet or DaemonSet, each named
  orders with the same pod. denyIngress adds a policy that denies all
  ingress in the namespace; allowProbe the rule that lets probe pods in.
  second adds orders-worker, which the Service does not select; changing
  secondRevision rolls it alone. hooks adds two Jobs that sleep hookSleep.
- Produces: `values.schema.json` with every new value
  additionalProperties stays false: a value the schema does not name, or a
  value outside its type or range, makes Helm refuse the release.

- [ ] **Step 1: Write the failing tests**
  `fixtures/service/main_test.go`:
  - `TestFailsForAWhileAfterItListens`: `SHUTDOWN=drain-now`, `FAIL_AFTER=300ms`, `FAIL_FOR=300ms`. Counted from the moment `listening` returns: at 100 ms `/work?ms=0` gives 200; at 400 ms it gives 500 with the body `failing`, and `/readyz` and `/version` give 200; at 800 ms `/work?ms=0` gives 200 again.
  - `TestJobSleepEndsWithoutListening`: `JOB_SLEEP=200ms`. `serve` returns nil after 200 ms or more, and the ready channel received nothing.
  - `TestBadFailAfter`: `FAIL_AFTER=soon` makes `serve` return an error that holds `FAIL_AFTER`.
- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./fixtures/service/ -run 'TestFailsForAWhile|TestJobSleep|TestBadFailAfter' -v` / Expected: FAIL: `/work` answers 200 at 400 ms, `serve` listens with `JOB_SLEEP` set, and `FAIL_AFTER=soon` is not rejected.
- [ ] **Step 3: Implement the switches and the variants.**
  - `fixtures/service/main.go`: the three variables, read with `durEnv`, written in the package comment and in the start-up log line. `JOB_SLEEP` is read first, before anything listens.
  - `templates/_pod.tpl` defines `orders.podSpec`: the pod spec of today's Deployment (grace period, no token, the security settings, the container with its env and readiness probe), with `FAIL_AFTER` and `FAIL_FOR` from `failAfter` and `failFor`, and the anti-affinity only when `spread` is true and `workload` is not DaemonSet (a DaemonSet's extra pod shares its node with the pod it replaces).
  - `deployment.yaml` renders only when `workload` is `Deployment`, and includes `orders.podSpec`; otherwise unchanged.
  - `statefulset.yaml`, when `workload` is `StatefulSet`: `StatefulSet` `orders`, `serviceName: orders`, `replicas`, the same selector and labels, `updateStrategy: {type: RollingUpdate}`, the pod spec from `orders.podSpec`.
  - `daemonset.yaml`, when `workload` is `DaemonSet`: `DaemonSet` `orders`, `updateStrategy: {type: RollingUpdate, rollingUpdate: {maxSurge: 1, maxUnavailable: 0}}`, no toleration (it runs on the workers only, not beside the probe), the pod spec from `orders.podSpec`.
  - `second.yaml`, when `second` is true: `Deployment` `orders-worker`, labels and selector `app.kubernetes.io/name: orders-worker`, 1 replica, the same strategy as `orders`, the pod template annotation `rollproof.test/revision: {{ .Values.secondRevision | quote }}`, one container of the same image with `SHUTDOWN` `helper` and the readiness probe, the same security settings and no token.
  - `networkpolicy.yaml`: when `denyIngress` is true, `NetworkPolicy` `orders-deny-ingress` with `podSelector: {}` and `policyTypes: [Ingress]`; when `allowProbe` is true, `NetworkPolicy` `orders-allow-probe` that selects `app.kubernetes.io/name: orders` and allows ingress on port 8080 from one peer that holds both `namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: rollproof-probe}}` and `podSelector: {matchLabels: {app.kubernetes.io/name: rollproof-probe}}` (section 6).
  - `hooks.yaml`, when `hooks` is true: two `Job`s, `orders-post-install` with `"helm.sh/hook": post-install` and `orders-pre-upgrade` with `"helm.sh/hook": pre-upgrade`, each with `"helm.sh/hook-delete-policy": before-hook-creation,hook-succeeded`, `backoffLimit: 0`, `restartPolicy: Never`, no token, the pod's security settings, and one container of the chart's image with `JOB_SLEEP` set to `hookSleep`.
  - `values.yaml` adds, each with a comment: `workload: Deployment`, `denyIngress: false`, `allowProbe: false`, `second: false`, `secondRevision: "1"`, `hooks: false`, `hookSleep: "20s"` (longer than the default settle time, 15 s), `failAfter: ""`, `failFor: ""`. `values.schema.json` adds `workload` as `{"enum": ["Deployment", "StatefulSet", "DaemonSet"]}`, the booleans as `{"type": "boolean"}`, and the four strings as `{"type": "string"}`.
- [ ] **Step 4: Run the tests to see them pass, and check the chart with both Helm majors**
  Run: `go test ./fixtures/service/ -v` / Expected: PASS.
  No cluster is needed for the chart. Run each command once with `.tools/helm-3/helm` and once with `.tools/helm-4/helm` in its place.
  Run: `.tools/helm-3/helm lint fixtures/chart --set image.repository=rollproof.test/x --set image.digest=sha256:0000000000000000000000000000000000000000000000000000000000000000 --set workload=StatefulSet --set denyIngress=true --set allowProbe=true --set second=true --set hooks=true` / Expected: the last line is `1 chart(s) linted, 0 chart(s) failed`.
  Run: `.tools/helm-3/helm template orders fixtures/chart --set image.repository=rollproof.test/x --set image.digest=sha256:0000000000000000000000000000000000000000000000000000000000000000 | grep -c '^kind: '` / Expected: `2` (the Deployment and the Service: the defaults render what they rendered before).
  Run: `.tools/helm-3/helm template orders fixtures/chart --set image.repository=rollproof.test/x --set image.digest=sha256:0000000000000000000000000000000000000000000000000000000000000000 --set workload=StatefulSet | grep -c -e '^kind: StatefulSet' -e '^kind: Deployment'` / Expected: `1`.
  Run: `.tools/helm-3/helm template orders fixtures/chart --set image.repository=rollproof.test/x --set image.digest=sha256:0000000000000000000000000000000000000000000000000000000000000000 --set workload=DaemonSet | grep -c -e '^kind: DaemonSet' -e podAntiAffinity` / Expected: `1`.
  Run: `.tools/helm-3/helm template orders fixtures/chart --set image.repository=rollproof.test/x --set image.digest=sha256:0000000000000000000000000000000000000000000000000000000000000000 --set denyIngress=true --set allowProbe=true | grep -c -e '^kind: NetworkPolicy' -e 'kubernetes.io/metadata.name: rollproof-probe'` / Expected: `3`.
  Run: `.tools/helm-3/helm template orders fixtures/chart --set image.repository=rollproof.test/x --set image.digest=sha256:0000000000000000000000000000000000000000000000000000000000000000 --set second=true --set-string secondRevision=2 | grep -c -e '^  name: orders-worker$' -e 'rollproof.test/revision: "2"'` / Expected: `2`.
  Run: `.tools/helm-3/helm template orders fixtures/chart --set image.repository=rollproof.test/x --set image.digest=sha256:0000000000000000000000000000000000000000000000000000000000000000 --set hooks=true | grep -c -e '"helm.sh/hook": post-install' -e '"helm.sh/hook": pre-upgrade'` / Expected: `2`.
  Run: `.tools/helm-3/helm template orders fixtures/chart --set image.repository=rollproof.test/x --set image.digest=sha256:0000000000000000000000000000000000000000000000000000000000000000 --set workload=Job` / Expected: an error that holds `values don't meet the specifications of the schema(s)`, and a non-zero exit. The same for `--set replicas=0` in place of `--set workload=Job` (row 25 depends on it).
- [ ] **Step 5: Commit**
  `git add fixtures/service/main.go fixtures/service/main_test.go fixtures/chart` and `git commit -m "Give the fixture chart a StatefulSet, a DaemonSet, a second workload, a policy that denies traffic and Helm hooks, and the service a time of failing after it listens"`
