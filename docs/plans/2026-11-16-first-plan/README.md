# rollproof: host repository, proofs and walking skeleton — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record the incubation in the host repository, settle by experiment the open points that the first code rests on, and build a `rollproof` binary that creates its own kind cluster, runs a Helm deploy and rollback under the built-in HTTP probe, and gives the expected verdict for the first four fixtures.

**Architecture:** One Go binary of nine internal packages, each with one job (section 5 of the spec). `verdict` is a pure function over a value that `run` fills; `run` sees the probe and the deploy driver as interfaces and every other unit as a step it calls; Kubernetes is reached through client-go, kind through its Go library, and Helm through the caller's own `helm` binary. The proofs are throwaway programs in a module of their own, and their recorded results gate the skeleton.

**Tech Stack:** Go 1.27; `sigs.k8s.io/kind` v0.33.0 as a library; `k8s.io/client-go`, `k8s.io/api` and `k8s.io/apimachinery` v0.37; `go.yaml.in/yaml/v3` v3.0.4; `github.com/google/go-containerregistry`; the standard library's `flag`, `net/http` and `testing`; `github.com/google/go-cmp` in tests; Helm 3.22.1 and 4.3.2 as binaries for the fixtures; nerdctl on the developer's machine and Docker on a hosted runner.

**Spec:** `projects/rollproof/docs/specs/2026-11-13-rollproof-design.md`. Section numbers in this plan are that document's. An executor reads the sections a task names before it starts the task.

## Global Constraints

Every task's requirements include these.

- **Linux only.** Every file that reaches a container engine, a signal or `/proc` builds on Linux alone. No other system is supported (section 8).
- **Module.** The Go module is named `rollproof`, with no host. Every package is under `internal/` except `cmd/rollproof` (section 13).
- **Licence.** Apache-2.0. Code copied from Kubernetes or kind keeps its licence header and names its source file and tag in a comment.
- **Stands apart from the host.** No file under `projects/rollproof/` cites a row of the host repository's decision log or follows its service rules. Task 1 is the one task that edits the host's own files.
- **Paths and commands.** Commands in this plan run from `projects/rollproof/` unless a step says "from the repository root". Every command behaves the same in bash and fish: no unquoted glob, no `VAR=value` prefix (write `env VAR=value command`), no `$(…)`.
- **The binary is static.** Build with `env CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=dev" -o bin/rollproof ./cmd/rollproof`. The binary is also the probe's whole image (section 9).
- **kind is linked.** `sigs.k8s.io/kind` v0.33.0. The tool never calls a `kind` binary and never writes the caller's kubeconfig (sections 2 and 9).
- **Helm is the caller's binary.** The tool never passes `--wait`, `--atomic`, `--rollback-on-failure` or `--create-namespace` (section 7).
- **Images by digest only.** Nothing is stored in the run's registry under a tag, and every reference the tool writes is `REPOSITORY@sha256:…` (section 9).
- **Names that are contract** (sections 6, 8 and 9): the probe's namespace `rollproof-probe`; the label `app.kubernetes.io/name: rollproof-probe`; the probe's container `probe`; the cluster-name prefix `rollproof-`; the registry host `rollproof.test`; the default file `rollproof.yaml`.
- **Verdict strings:** `pass`, `fail`, `not-verified`. **Window results:** `pass`, `fail`, `not-verified`, `not-run`, `not-judged`. **Window names:** `steady`, `deploy`, `rollback`, `restart`. **Exit codes of `run`:** 0, 1, 2 (sections 8 and 10).
- **Reason codes,** in the order of section 10's list, which is also the order that picks the deciding reason: `probe-failures`, `transition-stalled`, `pod-unschedulable`, `command-failed` (fail); `config-rejected`, `runner-unsupported`, `cluster-failed`, `chart-fetch-failed`, `setup-failed`, `install-failed`, `nothing-watched`, `workload-unsupported`, `target-unsupported`, `probe-not-started`, `probe-exited`, `probe-line-invalid`, `steady-not-clean`, `probe-silent`, `no-pod-replaced`, `restart-failed`, `interrupted`, `internal-error` (not verified).
- **Defaults** (section 8): `cluster.workers` 3; `probe.http.rate` 200; `probe.http.timeout` 2s; `probe.http.expect-status` 200-299; `limits.steady` 10s; `limits.settle` 15s; `limits.timeout` 5m; `limits.silence` 5s. The drain is 10 s for a custom probe and the request timeout plus 5 s for the built-in probe. A command past `limits.timeout` gets SIGTERM and is killed 10 s later.
- **Variables taken out of every command's environment** (section 7): `HELM_KUBECONTEXT`, `HELM_KUBEAPISERVER`, `HELM_KUBETOKEN`, `HELM_KUBECAFILE`, `HELM_KUBEASUSER`, `HELM_KUBEASGROUPS`, `HELM_KUBEINSECURE_SKIP_TLS_VERIFY`, `HELM_KUBETLS_SERVER_NAME`, `HELM_NAMESPACE`, `HELM_DRIVER`, `HELM_DRIVER_SQL_CONNECTION_STRING`.
- **Text for people never quotes a configuration value** when it rejects a configuration (section 8).
- **Tests.** Unit tests need no cluster and no network and run with `go test ./...`. A test that needs a container engine carries the build tag `cluster`; a fixture run carries the tag `e2e`. Run those with `-p 1`: one cluster at a time, because a rootless engine has a key quota (section 9).
- **Commits.** One commit per task unless a task says otherwise. A message is a plain sentence that says what changed. It carries no attribution to an AI tool: no `Co-Authored-By` trailer that names one, no "Generated with" line, no session link.
- **Nothing leaves the machine.** No push, no pull request and no run on GitHub Actions without the maintainer's word. A proof that needs a hosted runner is written, and waits (section 13).

## Review Focus

The spec implies these and names no test for them. Each has its test in the task that owns the code.

1. **A killed run left a cluster, a registry container or a run folder of the same name, perhaps only one of the three.** The next run removes each one that exists and then creates its own; it never deletes a name without the prefix. Test in Task 21.
2. **A command writes megabytes of output, or writes after it was told to stop.** The tool neither blocks nor holds the output in memory: the output goes to the evidence as it is written. Test in Task 23.
3. **The results folder cannot be created or written, or `--out` names a file.** `run` says so on standard error and exits 2 before it creates a cluster. Test in Task 33.
4. **The configuration file is reached through a symbolic link, a path with `..`, or a folder with a space in its name.** The cluster's name is the same for the same file, commands run in the file's real folder, and Helm gets absolute paths. Tests in Tasks 18 and 20.
5. **The probe's log stream is opened again and the lines around the break repeat: two failures with the same text and the same timestamp.** No line is lost and none is counted twice. Test in Task 29.

## What this plan builds, and what it leaves

Build-order steps 1 to 3 of section 13. The skeleton is cut like this:

| Unit | In this plan | Left for a later plan |
| --- | --- | --- |
| `config` | The whole schema of version 1, every rejection of section 8, substitution, paths, defaults. A list of what this build cannot run yet | Nothing |
| `cluster` | Runner checks, the kind cluster, the run's registry, node settings for `rollproof.test`, the ready wait, both namespaces, the kubeconfig, names, the run's folder, clean-up | Mirror, `ca` and credential settings (step 5); kind's own logs in the evidence (step 4) |
| `images` | The probe's own image; `local` images, stored under `rollproof.test` | `archive` (step 4); `as` (step 5) |
| `deploy` | The command runner; the Helm driver with `previous` and `candidate` | The commands driver, `setup`, a run with no previous release (step 4) |
| `watch` | The rollout rules for the three kinds, finish, quiet settle, stall, pod counts, quickest stop, which workloads are behind a Service, which digests pods ran | The restart (step 4); pod logs and events in the evidence, and the last event message in a stall's text (step 4) |
| `probe` | The line contract, the built-in HTTP probe, the target check, the probe's pod, the clock check | The custom probe (step 4) |
| `run` | The whole sequence for a deploy and a rollback, signals, a failure of the tool | The restart-only sequence, `setup` (step 4) |
| `verdict` | Every rule of section 10 | Nothing |
| `report` | `result.json`, the console text, and three kinds of evidence: the probe's raw lines, the window marks, the commands' output | The rest of the evidence folder (step 4); the job summary and the action's outputs (step 6) |
| `cmd/rollproof` | `run`, `probe http`, `version` | Nothing |
| Tests with a cluster | The first four fixtures of section 12, under Helm 3 and Helm 4, on the developer's machine | The other 23 fixtures, and the first fixture on the oldest node image of the linked kind release (step 4); the mirror job (step 5); the action's test (step 6) |

A configuration that names a part this build cannot run yet (`registries`, `setup`, `deploy.commands`, `probe.custom`, an `archive` or an `as` in `images`, or a Helm release with no `previous`) is valid, and `run` ends it as `config-rejected` with the text "not built yet" and the key's name. Each later plan removes its keys from that list.

## Decisions made in this plan

The spec left these open or silent. Each is the smaller choice. The maintainer reviews them with the plan; the six marked **public** (1, 2, 3, 16, 17 and 18) change something a user depends on.

1. **public: the names in `result.json`.** Keys are lower case with underscores. Task 30 gives the whole schema. Section 10 fixes what the file holds and not what its keys are called.
2. **public: the grammar of a duration** is digits followed by `ms`, `s` or `m`. `1h`, `1.5s` and `1m30s` are rejected. Section 8 says "a number with a unit" and shows those three units.
3. **public: which evidence the first build writes.** Section 13's build order names no step for the evidence folder. This plan writes the probe's raw lines, the window marks and each command's output, and leaves the rest to the plan for step 4.
4. **The whole configuration schema is built now,** with a list of what the build cannot run yet. The other way, a schema that grows with each step, would have every later plan reshape the types that every unit reads.
5. **The clock check's bound is 2 seconds** between the runtime's timestamp on the first observation and the moment the tool read it. Task 29 measures the difference on the developer's machine and records it (section 14, point 21).
6. **How each runner check is made** (section 8 says what is checked, not how). Linux: the build. The engine: found as kind's library finds it, and `info` must succeed. Another machine: `DOCKER_HOST` or `CONTAINER_HOST` that names `tcp://` or `ssh://`. Inside a container: `/.dockerenv` or `/run/.containerenv` exists. Static binary: the executable has no program interpreter. "May run privileged containers" has no cheap check; a cluster that cannot start is `cluster-failed`.
7. **"After finish"** counts failures that began at or after the moment the transition last came to be finished. When the settle time started again, the earlier finish did not stand (section 7).
8. **A fault of the probe that happens after the last window has ended** is not judged and becomes a warning. Section 6 says this of a cut last line only.
9. **The cluster's name** without `--name` is `rollproof-` and the first 10 hexadecimal digits of the SHA-256 of the configuration file's real, absolute path.
10. **Each worker's zone label** is `rollproof-zone-1`, `rollproof-zone-2` and so on.
11. **An image reaches the run's registry** through the engine's `save` and a push by digest from inside the tool. An engine's own `push` needs a tag, which section 9 forbids in the run's registry.
12. **The command line uses the standard library's `flag`,** so `-out` and `--out` both work.
13. **Two folders that section 13's layout does not list:** `proofs/`, a Go module of throwaway programs with their records under `docs/proofs/`, and `hack/`, for the program that fetches pinned Helm binaries. Neither is part of a release.
14. **The Docker half of point 8** needs a hosted runner or a machine with Docker. Its proof program is written in Task 4 and runs when the maintainer says so. The skeleton is built and tested on nerdctl until then, and Task 11 lists it as unproved.
15. **A `deploy.selector` that is not a valid label selector is rejected with the configuration.** Section 8's list of rejections does not name it; without it the mistake would show only after the cluster exists.
16. **public: the name of an `images` entry must be a valid path component of a repository** (lower-case letters and digits, with single dots, underscores or hyphens between them). The name becomes `rollproof.test/NAME`; a name such as `My Image` would otherwise fail only after the cluster exists.
17. **public: a probe's line that has a top-level name twice is invalid.** Go's JSON reader keeps the last of them without a word, so `{"ok":false,"ok":true}` would count as a pass. Section 6 says that a failure must not vanish because of the way it was printed.
18. **public: `probe.http.timeout` plus one second must be shorter than `limits.silence`.** Section 8 asks only that the timeout be shorter. The built-in probe reports a pass in the summary line of the second in which it ended, so a probe that works can write nothing for one timeout and one second. The defaults (2 s and 5 s) are not touched.
19. **A Service of type ExternalName is a host that names no Service of the cluster.** It is probed as it is, and the report cannot say which workloads the probe reaches. Section 6's four cases do not name it.
20. **`KIND_EXPERIMENTAL_PROVIDER` is read as kind's own command line reads it:** `docker`, `podman`, `nerdctl`, `finch` and `nerdctl.lima`. Without it the engine is chosen by the test that kind's providers use, and then must answer `info`.
21. **The rollout rules are copied from kubectl, and a test compares the copy with kubectl's own package.** This is what the plan assumes; Task 8 measures what an import would cost and decides.
22. **The name of a variable in `${env.NAME}`** is a letter or an underscore, then letters, digits and underscores. Section 8 gives no grammar.
23. **A configuration file that holds more than one YAML document is rejected.** A reader that took the first and ignored the rest could hide a weakened test in the part it ignored.
24. **Every required key of section 8's schema is checked:** `deploy.helm.release`, a release's `chart`, `registries[].host`, `probe.http.url` and `probe.custom.image`. Section 8 marks them as required and its list of rejections does not repeat them.
25. **A command whose child process keeps its output open after the command has exited** counts as exited one second later. Without this, such a command would be reported as stopped at the limit.

## What writing this plan found in the spec

None of these changes a decision of the design. Each is a sentence of the spec that a reading of code or a trial run showed to be inexact. The spec is not edited by this plan; the maintainer decides which to correct there.

- **Section 6, "Output", and section 8, "What else is rejected":** "Within one request timeout every request has passed or failed, and either writes a line." A pass writes its line when its second is over, up to one second later. Decision 18 follows from it.
- **Section 6, "Output":** a failure's line is said to carry `t`, `class` and `ms`. The plan also writes `lag` on it, which the contract allows on any line and the report's "behind" column needs.
- **Section 9, "Ready":** "kind's own wait covers the control-plane node only." With a wait time of zero, which is what the tool passes, kind's library waits for no node. The tool waits for every node itself either way.
- **Section 7, "When a transition has finished", and section 14, point 10:** kubectl's rule for a StatefulSet compares revisions only when the object has no rolling-update settings, which the API server always fills in; for `OnDelete` it answers "done" together with an error. At Kubernetes 1.37 a Deployment's status also counts terminating pods, which kubectl's rule does not read. The tool's own third condition, that every deleted pod is gone, stands.
- **Section 10, the console's second table:** "the old pod of that workload that was gone soonest". Section 7 counts every pod that the transition deleted and that was ready, a pod it created included. The plan follows section 7.
- **Section 4, "Windows":** the windows have no gap between them. The plan therefore starts a transition's window at the end of the window before it, not at a later reading of the clock.

## File structure

All paths are under `projects/rollproof/` except the last block.

| Path | Responsibility |
| --- | --- |
| `go.mod`, `go.sum`, `LICENSE`, `README.md`, `.gitignore` | The module, the licence, a first README that says what works, and the ignore list (`bin/`, `.tools/`) |
| `cmd/rollproof/main.go` | Parses the command line and calls `run.Run`, `probe.RunHTTP` or the version text |
| `cmd/rollproof/wire.go` | Joins the real units into `run.Units` |
| `internal/verdict/vocab.go` | Verdicts, codes, window names, `Condition`, `Observation` |
| `internal/verdict/decide.go` | `Decide`: the rules of section 10 on placed conditions |
| `internal/verdict/observe.go` | Counting, window assignment, crossing, silence |
| `internal/probe/line.go` | `ParseLine`: the line contract |
| `internal/probe/http.go`, `httpargs.go` | The built-in HTTP probe and its arguments |
| `internal/probe/target.go` | The target check |
| `internal/probe/pod.go`, `follow.go` | The probe's pod, its log stream, start, clock check, stop |
| `internal/config/config.go` | The types of a run description |
| `internal/config/load.go` | Strict reading, defaults, types |
| `internal/config/validate.go`, `notbuilt.go` | What is rejected; what this build cannot run yet |
| `internal/config/substitute.go` | The `${…}` forms, `Resolve`, `Settings` |
| `internal/cluster/names.go` | The cluster's name and the run's folder |
| `internal/cluster/kindconfig.go`, `hosts.go` | The kind description and a node's registry settings |
| `internal/cluster/runner.go` | The runner checks |
| `internal/cluster/cluster.go`, `registry.go`, `engine.go` | Create, ready, registry, namespaces, kubeconfig, delete |
| `internal/images/images.go`, `own.go`, `local.go` | Store by digest, the probe's own image, node pulls |
| `internal/deploy/command.go` | The command runner |
| `internal/deploy/deploy.go`, `helm.go` | The `Deployer` interface and the Helm driver |
| `internal/watch/rollout.go` | When a rollout of each kind is complete |
| `internal/watch/tracker.go`, `counts.go` | The transition tracker, with no I/O |
| `internal/watch/watcher.go`, `service.go` | The tracker fed from the API; workloads behind a Service |
| `internal/run/run.go` | The sequence of a run |
| `internal/run/conditions.go`, `record.go` | How a command's ending becomes a condition; how the units' facts become the report's record |
| `internal/report/record.go`, `result.go` | The run record and `result.json` |
| `internal/report/console.go`, `evidence.go` | The console text; the evidence files |
| `fixtures/service/`, `fixtures/chart/` | The fixture service and its chart |
| `e2e/` | The harness and the first four fixtures |
| `hack/fetchhelm/main.go` | Fetches Helm 3.22.1 and 4.3.2 into `.tools/`, by checksum |
| `proofs/` (module `rollproof-proofs`) | `internal/lab/` and one program for each proof |
| `docs/proofs/` | One record for each proof, and `README.md` with the gate's list |

In the host repository, from its root:

| Path | Change |
| --- | --- |
| `docs/decisions-and-open-items.md` | One new row |
| `AGENTS.md`, `CLAUDE.md`, `README.md` | One line each |
| `tools/citations/model.ts`, its two tests, `README.md`, `DESIGN.md` | `projects/` joins the folders the check does not read |
| `.github/workflows/rollproof-unit.yml` | The unit tests, on a pull request that touches the project |

## The tasks

This is one plan in eleven files, because its test code makes it long. Read this file first, then the file of the task at hand. Tasks are numbered through all files and are done in order.

| File | Tasks | What |
| --- | --- | --- |
| `01-host-repository.md` | 1 | The host repository |
| `02-proofs.md` | 2 to 11 | The proofs |
| `03-verdict-and-probe-lines.md` | 12 to 16 | `verdict`, the probe's lines, the built-in HTTP probe |
| `04-config.md` | 17 to 19 | `config` |
| `05-cluster-and-images.md` | 20 to 22 | `cluster` and `images` |
| `06-deploy.md` | 23 to 24 | `deploy` |
| `07-watch-and-target.md` | 25 to 28 | `watch`, and the probe's target check |
| `08-probe-pod-and-report.md` | 29 to 31 | The probe's pod, and `report` |
| `09-run-and-command.md` | 32 to 34 | `run` and `cmd/rollproof` |
| `10-fixtures-and-workflow.md` | 35 to 37 | The fixtures, the harness and the unit-test workflow |

## Alternatives considered

The blocks for `internal/probe`, `internal/deploy`, and `internal/config` with `action.yml` are the spec's own, copied unchanged from sections 6, 7 and 8. They choose the public contracts. The Go signatures that keep those contracts are in the tasks' Interfaces blocks.

### `internal/probe`

Common case: a user with a workload the built-in probe does not cover writes a small program that keeps checking it, and wants the tool to judge that program's results across the deploy and the rollback.

**A. Observation stream**

- The tool starts the probe once and stops it once with SIGTERM.
- The probe prints one JSON line per check: `{"ok": true}` or `{"ok": false}`, with optional fields.
- The tool assigns each line to a window by its time, and counts.

```sh
# a TCP check as a shell loop
trap 'exit 0' TERM
while true; do
  if nc -z -w1 orders-db.orders 5432; then echo '{"ok":true}'
  else echo '{"ok":false,"class":"refused"}'; fi
  sleep 0.1
done

# a consumer probe that reports a late result with its own time
echo '{"ok":false,"t":"2026-11-13T10:00:07.120Z","class":"lost","detail":"message 4411 not consumed within 5s"}'
```

**B. Phase callbacks**

- `probe start`
- `probe window-begin <name>`
- `probe window-end <name>`, which prints `{"failed":N,"total":M}`
- `probe stop`

```sh
# the same TCP check, keeping state between calls
case "$1" in
  start)        ./checker --daemon --pidfile /tmp/p.pid --counts /tmp/counts ;;
  window-begin) cp /tmp/counts "/tmp/begin-$2" ;;
  window-end)   ./diff-counts "/tmp/begin-$2" /tmp/counts ;;
  stop)         kill "$(cat /tmp/p.pid)" ;;
esac
```

**C. Exit code per window** (expected to lose)

- For each window the tool runs the probe command and stops it at the window's end.
- Exit 0 means no downtime. Exit 1 means downtime.

```sh
# an existing k6 script; k6 exits 99 when a threshold fails
k6 run rollout.js
```

Third design: not needed; option A leaves the probe author a loop that prints a line.

| | A. Observation stream | B. Phase callbacks | C. Exit code per window |
| --- | --- | --- | --- |
| Ease of use for the common caller | A loop that prints a line | State files and four entry points | One command, if a script already exists |
| Interface size | One line format with one required field | Four operations and a result format | One command and an exit code |
| Generality | New windows need no probe change; a late result carries its own time | Every new window is a new case for every probe | Cannot place a failure inside a window |
| Information hidden | The probe never sees windows, thresholds or the timeline | The timeline and the counting leak into every probe | Hides the evidence the report needs |
| Implementation simplicity | Read lines, sort by time, detect silence | A control channel into a pod, and errors per call | Simplest |
| Efficiency | A line per check; `n` batches identical ones | Summaries only | Summaries only |

Pick: A.

Losers: B, because every probe must keep state between calls and re-implement counting, and the tool's timeline leaks into each one.

Losers: C, because a crashed probe and real downtime both show as a non-zero exit, the moments between windows go unobserved, and the report gets no evidence.

### `internal/deploy`

Common case: a user with a Helm chart names the previous release and the candidate, and wants the tool to install the first, upgrade to the second and roll back.

**A. Two release descriptions; the tool derives the transitions**

- `namespace`, shared by both drivers: where the release goes, and what the tool watches.
- Helm driver: `release`, `previous`, `candidate`.
- Commands driver: three commands named `install`, `deploy` and `rollback`.
- The tool owns the order and the meaning of each transition.

```yaml
# a Helm chart
deploy:
  namespace: orders
  helm:
    release: orders
    previous:  { chart: oci://registry.example/charts/orders, version: 1.4.2 }
    candidate: { chart: ./chart, values: [{ image: { digest: "sha256:…" } }] }

# Kustomize, through commands
deploy:
  namespace: orders
  commands:
    install:  [kubectl, apply, -k, overlays/previous]
    deploy:   [kubectl, apply, -k, overlays/candidate]
    rollback: [kubectl, apply, -k, overlays/previous]
```

**B. A scripted list of steps**

- The caller writes an ordered list. Each step names a transition and the action that performs it.

```yaml
# the same Helm chart as steps
steps:
  - install:  { helm: { chart: oci://registry.example/charts/orders, version: 1.4.2 } }
  - deploy:   { helm: { upgrade: { chart: ./chart } } }
  - rollback: { helm: { rollback: { revision: 1 } } }
```

**C. Rendered manifests in; the tool applies them** (expected to lose)

- The caller renders two manifest files. The tool applies the previous one, then the candidate, then the previous one again.

```sh
# render first, then run
helm template orders oci://registry.example/charts/orders --version 1.4.2 > previous.yaml
helm template orders ./chart > candidate.yaml
rollproof run --previous previous.yaml --candidate candidate.yaml
```

Third design: not needed; option A leaves a Helm user nothing to write beyond naming the two releases.

| | A. Two releases | B. Steps | C. Manifests |
| --- | --- | --- | --- |
| Ease of use for the common caller | Name two releases | Write the sequence every time | Render twice before every run |
| Interface size | A namespace, an optional selector, and three Helm fields or three commands | A step grammar times each driver's actions | Two paths |
| Generality | Deploy, rollback and restart for Helm and for anything scriptable; fixed order | Any sequence | Only what an apply can express; no Helm hooks or release history |
| Information hidden | The order, the Helm calls, what counts as a rollback | All three are the caller's to get right | Hides the apply, pushes rendering out |
| Implementation simplicity | Two drivers | A step interpreter and two drivers | One applier |
| Efficiency | One `helm` process per transition | The same | Direct API calls |

Pick: A.

Losers: B, because every user would write the same sequence and could write a rollback that is really a second upgrade.

Losers: C, because it bypasses Helm's own upgrade and rollback, so it tests a path that Helm users do not deploy by; users who do deploy rendered manifests are served by A's commands driver.

Canary kept: 3: install, deploy and rollback are the three moments at which `run` acts, and it marks a window between them; no design with fewer operations was found.

### `internal/config` and `action.yml`

Common case: a repo's workflow builds its image, then runs the test with one step; a developer reproduces that run on a Linux laptop with one command.

**A. One config file describes the run; the action takes its path**

- `rollproof.yaml` holds everything.
- The command is `rollproof run`.
- The action has three inputs.

```yaml
# rollproof.yaml
version: 1
images:
  orders: { local: orders:candidate, as: registry.example/team/orders }
deploy:
  namespace: orders
  helm:
    release: orders
    previous:  { chart: oci://registry.example/charts/orders, version: "${env.PREVIOUS_VERSION}" }
    candidate: { chart: ./chart, values: [{ image: { digest: "${images.orders.digest}" } }] }
probe:
  http: { url: http://orders.orders.svc/api/orders/latest }

# the workflow
- run: docker build -t orders:candidate .
- uses: OWNER/rollproof@COMMIT
  env: { PREVIOUS_VERSION: 1.4.2 }

# a Linux laptop
env PREVIOUS_VERSION=1.4.2 rollproof run
```

**B. Action inputs are the interface, mirrored as flags**

- Every setting is a flat `with:` input and a matching command-line flag.

```yaml
# the workflow
- uses: OWNER/rollproof@COMMIT
  with:
    release: orders
    namespace: orders
    chart: ./chart
    previous-chart: oci://registry.example/charts/orders
    previous-version: 1.4.2
    set: |
      image.digest=sha256:…
    image: orders:candidate
    image-as: registry.example/team/orders
    probe-url: http://orders.orders.svc/api/orders/latest

# a Linux laptop
rollproof run --release orders --namespace orders --chart ./chart --previous-chart oci://registry.example/charts/orders --previous-version 1.4.2 --set image.digest=sha256:… --image orders:candidate --image-as registry.example/team/orders --probe-url http://orders.orders.svc/api/orders/latest
```

**C. A Go library called from the user's own test** (expected to lose)

- Users write a Go test that calls `rollproof.Run`.

```go
func TestRollout(t *testing.T) {
    rollproof.Run(t, rollproof.Helm{Release: "orders" /* … */}, rollproof.HTTP{URL: "…"})
}
```

Third design: not needed; option A leaves the caller one file and one step.

| | A. Config file | B. Inputs and flags | C. Go library |
| --- | --- | --- | --- |
| Ease of use for the common caller | One file, one step, one local command | Nothing extra to learn, but long, and retyped locally | Must write Go |
| Interface size | One schema, three action inputs, four flags | About thirty inputs, each duplicated as a flag | A Go API |
| Generality | The same file on a Linux laptop, on GitHub and on another CI system that meets the runner's requirements | Shaped by GitHub; another CI needs its own mapping | Anything Go can express |
| Information hidden | No mapping between action and tool exists | String encodings for lists and maps show through | Library internals become the caller's concern |
| Implementation simplicity | One loader and validator | Two lists kept in step | A stable public Go API to maintain |
| Efficiency | No difference | No difference | No difference |

Pick: A.

Losers: B, because action inputs are strings, so lists and maps need encodings, and the local command drifts from the workflow.

Losers: C, because it demands Go in every consumer repo and makes a Go API a public contract.

### `internal/verdict`

Common case: at the end of each window, and once after the probe has stopped, `run` asks what the verdict is so far and whether the run goes on.

**A. One pure function over a value**

- `Decide(t Timeline) Decision`

```go
d := verdict.Decide(timeline)
if !d.GoOn { break }

final := verdict.Decide(timeline) // after the probe stopped
os.Exit(final.Verdict.ExitCode())
```

**B. An accumulator**

- `New(silence time.Duration) *Judge`
- `(*Judge).Observe(o Observation)`, `(*Judge).Place(w WindowName, c Condition)`
- `(*Judge).EndWindow(w WindowName, at time.Time) (goOn bool)`
- `(*Judge).Final() Decision`

```go
j := verdict.New(limits.Silence)
for o := range events { j.Observe(o) }
if !j.EndWindow(verdict.Deploy, now) { break }

final := j.Final()
```

**C. Predicates that the caller combines** (expected to lose)

- `Failed(w Window) bool`, `Silent(obs []Observation, w Window, limit time.Duration) bool`, `Outranks(a, b Code) bool`

```go
if verdict.Failed(w) || (verdict.Silent(obs, w, limit) && !verdict.Failed(w)) { stop = true }
```

Third design: not needed; option A leaves the caller one call.

| | A. Pure function | B. Accumulator | C. Predicates |
| --- | --- | --- | --- |
| Ease of use for the common caller | One call with what it has | Calls in the right order, all run long | Writes the rules' order itself |
| Interface size | One function, two value types | Five operations | One function for each rule |
| Generality | A late result is one more element; judged again from the start | A late result needs a way back into a closed window | Every new rule changes the caller |
| Information hidden | Every rule and their order | The rules, but the order of calls becomes contract | Nothing: the order is the caller's |
| Implementation simplicity | A fold over a value; a table test is a literal | State between calls; a test is a script of calls | Simplest to write, hardest to keep right |
| Efficiency | Reads every observation at each call: thousands, a few times | Reads each once | The same as A |

Pick: A.

Losers: B, because a result that the probe reports late has to change a window that was already ended, so the accumulator would keep every observation anyway and its call order would be one more thing to get right.

Losers: C, because `run` would hold a copy of the rules, which section 5 forbids.

### `internal/watch`

Common case: `run` starts a transition and wants to know when its window has ended and what happened to the pods in it.

**A. A tracker fed by the API, asked for one transition at a time**

- `Start(ctx, client, Options) (*Watcher, error)`
- `(*Watcher).InstallReady(ctx, began, exited) (Install, error)`
- `(*Watcher).Transition(ctx, name, began, exited) Transition`

```go
exited := make(chan struct{})
go func() { result = deployer.Deploy(ctx); close(exited) }()
tr := watcher.Transition(ctx, verdict.Deploy, began, exited)
window.End, window.Finished = tr.Ended, tr.Finished
```

**B. Snapshots that the caller compares**

- `Snapshot(ctx, client, namespace, selector) (State, error)`
- `Finished(before, now State) bool`, `Replaced(before, now State) []Count`

```go
before, _ := watch.Snapshot(ctx, client, ns, sel)
for { now, _ := watch.Snapshot(ctx, client, ns, sel); if watch.Finished(before, now) { break }; time.Sleep(500 * time.Millisecond) }
```

**C. `kubectl rollout status` for each workload** (expected to lose)

- `Wait(ctx, kubeconfig, namespace string) error`

```go
err := watch.Wait(ctx, kubeconfig, ns) // runs kubectl for each workload
```

Third design: not needed; option A leaves the caller one call for a transition.

| | A. Tracker | B. Snapshots | C. kubectl |
| --- | --- | --- | --- |
| Ease of use for the common caller | One call that returns when the window has ended | A loop, a settle timer and a stall timer in the caller | One call, and no pod counts |
| Interface size | Three operations and two result types | Three functions and a state type | One function |
| Generality | A workload added, removed or renamed in a transition is one more event | The same, if no pod lives and dies between two snapshots | Only workloads that existed at the start |
| Information hidden | The rollout rules, the settle time, pod identity | The loop and both timers leak to the caller | Hides the rules; cannot see terminating pods |
| Implementation simplicity | A state machine with no I/O, and a thin shell around it | Simple reads, the logic spread over caller and unit | Simplest |
| Efficiency | Events only | A list of every object twice a second | A process for each workload |

Pick: A.

Losers: B, because the moment a pod was told to stop comes only from an event, and a pod that is created and gone between two snapshots is never counted.

Losers: C, because `kubectl rollout status` returns before old pods are gone (section 3), and the runner need not hold `kubectl`.

Canary kept: 3: inside the package, the tracker is told of each change (`Set`, `Gone`, `Begin`, `CommandExited`) and asked for the state; telling and asking are separate by nature, and the caller outside the package uses one operation for a transition.

### `internal/cluster`

Common case: `run` wants a ready test cluster with its registry and both namespaces, and wants all of it gone at the end.

**A. One call up, one call down**

- `CheckRunner(ctx) (Engine, error)`
- `Create(ctx, Spec, Engine) (*Cluster, error)`
- `(*Cluster).Delete(ctx) error`

```go
c, err := cluster.Create(ctx, spec, engine)
if err != nil { return err }
defer c.Delete(ctx)
```

**B. Steps that the caller orders**

- `NewKind(ctx, Spec) (*Kind, error)`, `StartRegistry(ctx, *Kind) (Registry, error)`
- `WriteNodeSettings(ctx, *Kind, Registry) error`, `WaitReady(ctx, *Kind) error`, `CreateNamespaces(ctx, *Kind, ...string) error`

```go
k, _ := cluster.NewKind(ctx, spec)
reg, _ := cluster.StartRegistry(ctx, k)
_ = cluster.WriteNodeSettings(ctx, k, reg)
_ = cluster.WaitReady(ctx, k)
_ = cluster.CreateNamespaces(ctx, k, "rollproof-probe", ns)
```

**C. The `kind` binary and a script** (expected to lose)

- `Up(ctx, scriptPath string) error`

```go
err := cluster.Up(ctx, "hack/kind-with-registry.sh")
```

Third design: not needed; option A leaves the caller two calls.

| | A. One call each way | B. Steps | C. Binary and script |
| --- | --- | --- | --- |
| Ease of use for the common caller | Create, and delete | Five calls in an order it must know | One call, and a tool to install |
| Interface size | Three operations | Five or more | One |
| Generality | A new node setting changes no caller | The same, with a new step for each | Whatever the script does |
| Information hidden | The order, the engine, the nodes' settings, the clean-up of a half-made cluster | The order is the caller's, and so is the clean-up | Hides all of it in a shell script |
| Implementation simplicity | One function that holds the order | The same code, split | Least code |
| Efficiency | No difference | No difference | One more process |

Pick: A.

Losers: B, because the order (the registry after the cluster, the settings before the first pull, ready before the namespaces) is a fact of kind and of the container runtime, and the caller would also own what to remove when a step fails.

Losers: C, because section 2 rules out the `kind` binary on the runner.

### `internal/images`

Common case: `run` wants the caller's local images and the probe's own image in the nodes, and the digest of each.

**A. One call; the tool pushes by digest itself**

- `Store(ctx, c *cluster.Cluster, binary string, images map[string]config.Image, timeout time.Duration) (Stored, error)`

```go
stored, err := images.Store(ctx, c, ownBinary, cfg.Images, cfg.Limits.Timeout)
err = cfg.Resolve(stored.Images)
```

**B. The engine pushes**

- `Push(ctx, engine cluster.Engine, local, target string) (digest string, err error)`
- `Pull(ctx, c *cluster.Cluster, ref string) error`

```go
digest, _ := images.Push(ctx, c.Engine, "orders:candidate", c.PushRegistry+"/orders:run")
_ = images.Pull(ctx, c, "rollproof.test/orders@"+digest)
```

**C. kind's image load** (expected to lose)

- `Load(ctx, c *cluster.Cluster, local string) error`

```go
err := images.Load(ctx, c, "orders:candidate")
```

Third design: not needed; option A leaves the caller one call.

| | A. One call | B. Engine push | C. kind load |
| --- | --- | --- | --- |
| Ease of use for the common caller | One call, digests back | Two calls for each image, and a tag to invent | One call, and no digest |
| Interface size | One function, one result type | Two functions | One function |
| Generality | A new source of images is a new key of the configuration | Each engine pushes to a plain-HTTP registry in its own way | Images by tag only |
| Information hidden | Archives, the registry protocol, the node pull | The registry's address and a tag reach the caller | Everything |
| Implementation simplicity | A library for archives and pushes | Engine flags for each engine | Simplest |
| Efficiency | One copy through the tool | One copy through the engine | One copy into each node |

Pick: A.

Losers: B, because an engine's push needs a tag, which section 9 forbids in the run's registry, and each engine needs its own setting before it pushes to a registry without TLS.

Losers: C, because a pod whose image is referenced by digest did not start after either of kind's load commands (section 3).

### `internal/run`

Common case: `main` hands over the options and gets an exit code.

**A. `run` builds every unit itself**

- `Run(ctx context.Context, o Options) int`

```go
os.Exit(run.Run(ctx, opts))
```

**B. `run` is given the units as steps**

- `Run(ctx context.Context, o Options, u Units) int`
- `Units`: three functions and a clock; `Cluster`: what a created cluster gives the run

```go
os.Exit(run.Run(ctx, opts, realUnits())) // main

code := run.Run(ctx, opts, run.Units{Load: load, Check: ok, Create: fakeCluster, Clock: clock}) // a test
```

**C. A list of steps that an engine runs in order** (expected to lose)

- `Run(ctx context.Context, steps []Step) int`

```go
os.Exit(run.Run(ctx, []run.Step{loadStep, clusterStep, imagesStep, installStep, probeStep, deployStep, rollbackStep}))
```

Third design: not needed; option B leaves `main` one call and one value to build.

| | A. Builds its own | B. Given the units | C. Step list |
| --- | --- | --- | --- |
| Ease of use for the common caller | One call | One call and one value | The caller writes the sequence |
| Interface size | One function | One function, one struct, three small interfaces | One function and a step type |
| Generality | A new driver changes `run` | A new driver changes `main` only | Any sequence, also a wrong one |
| Information hidden | The sequence; but `run` learns kind, Helm and HTTP | The sequence, and nothing of any unit's insides | Nothing: the sequence is the caller's |
| Implementation simplicity | Least code | A little wiring in `main` | A loop that cannot express "go on only if the window passed" without flags |
| Efficiency | No difference | No difference | No difference |

Pick: B.

Losers: A, because the order of a run, what ends it and what is cleaned up after a signal could then be tested only with a cluster, and `run` would import Helm and kind, which section 5 forbids.

Losers: C, because the windows are not a flat list: each transition depends on the verdict so far.

Canary kept: 3: the `Cluster` interface that `run` is given has one method for each unit's step; the order of those steps is what `run` exists to hold, and no design with fewer was found that keeps `run` free of the units' insides.

### `internal/report`

Common case: `run` keeps one record of the run and wants it written as `result.json` at fixed moments, and printed for people at the end.

**A. One record; a writer for the folder; text as a pure function**

- `Open(out string, getenv func(string) string) (*Writer, error)`
- `(*Writer).Final(r Run) error`
- `Console(r Run) string`

```go
w, err := report.Open(o.Out, os.Getenv) // writes the first result.json
// … the run fills rec …
_ = w.Final(rec)
fmt.Fprint(o.Stdout, report.Console(rec))
```

**B. A sink of events**

- `(*Reporter).WindowEnded(w)`, `(*Reporter).TransitionDone(t)`, `(*Reporter).Decided(d)`, `(*Reporter).Close() error`

```go
rep.WindowEnded(w)
rep.TransitionDone(tr)
rep.Decided(final)
_ = rep.Close()
```

**C. Templates over the units' own types** (expected to lose)

- `Write(out string, d verdict.Decision, trs []watch.Transition, rel []deploy.Release, …) error`

```go
_ = report.Write(out, final, transitions, releases, facts, images, warnings)
```

Third design: not needed; option A leaves the caller a value to fill and two calls.

| | A. Record and writer | B. Event sink | C. Templates |
| --- | --- | --- | --- |
| Ease of use for the common caller | Fill a value, call twice | A call for each thing that happens | One call with many arguments |
| Interface size | Three operations and the record | A method for each event | One function |
| Generality | A new fact is a new field | A new fact is a new method | A new fact is a new argument |
| Information hidden | The file formats and the moments of writing | The same, but the order of calls becomes contract | The file format follows every unit's types |
| Implementation simplicity | Marshal a value; render a value | State between calls | Least code |
| Efficiency | No difference | No difference | No difference |

Pick: A.

Losers: B, because the report would hold the run's state a second time, and a golden test would be a script of calls and not a value.

Losers: C, because `result.json` is a contract that must not change when a unit's type does.

Canary kept: 3: `Open`, `Final` and `Console` are three calls in the common case; the file and the text are two forms of one record, and a `Final` that also printed would tie the file's moment to the console's.

`cmd/rollproof` adds no interface: its command line is chosen in the block for `internal/config` and `action.yml`. `fixtures/`, `e2e/`, `hack/` and `proofs/` hold test and proof programs that no other code calls.

Design delta: neutral, because the one change to existing code adds an entry to a list that the citation check already has.
