# rollproof: the rest of the two contracts (build-order step 4) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build step 4 of the design's build order: the custom probe and `docs/probe-contract.md`, the commands driver, the `setup` commands, restart-only runs, `archive` in `images`, the rest of the evidence folder with the last event in a stall's text, and the remaining fixtures of section 12, with the first fixture on the oldest node image.

**Architecture:** Each new part lands in the unit that section 5 gives it, behind the interfaces `run` already uses: the commands driver is a second `deploy.Deployer`, `setup` runs on the same command runner, the restart is a `watch` operation that `run` calls in place of a command, and the custom probe is a second shape of the probe's pod. Evidence that must be written while the run goes (pod logs, pod lists, the window marks) is written by the unit that sees it; evidence that needs the whole run (the timeline, the events) is written by `run` after the verdict and before the cluster is deleted. The fixtures grow a second image (a small probe program), chart variants and a harness that can act on the run while it goes.

**Tech Stack:** As built: Go 1.27, `k8s.io/client-go`, `k8s.io/api` and `k8s.io/apimachinery` v0.37, `sigs.k8s.io/kind` v0.33.0 as a library, `github.com/google/go-containerregistry`, `go.yaml.in/yaml/v3`, `github.com/google/go-cmp` in tests; Helm 3.22.1 and 4.3.2 and, new, kubectl v1.37.0 as binaries for the fixtures, fetched by `hack/fetchhelm`; busybox 1.37.0 as the stock image of the TCP loop. No new Go dependency.

**Spec:** `projects/rollproof/docs/specs/2026-11-13-rollproof-design.md`, at `522b99d` of the branch `rollproof-step4-design`. Section numbers in this plan are that document's. An executor reads the sections a task names before it starts the task. Where the design is silent, the decisions of `docs/plans/2026-11-16-first-plan/README.md` and the rulings of `docs/plans/2026-11-17-install-window/README.md` stand (section 13), and so do this plan's decisions below.

## Global Constraints

Every task's requirements include these.

- **Linux only.** Module `rollproof`, every package under `internal/` except `cmd/rollproof`. Apache-2.0. No file under `projects/rollproof/` cites a row of the host repository's decision log or follows its service rules.
- **Paths and commands.** Commands run from `projects/rollproof/` unless a step says "from the repository root". Every command behaves the same in bash and fish: no unquoted glob, no `VAR=value` prefix (write `env VAR=value command`), no `$(…)`.
- **The binary is static.** `env CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=dev" -o bin/rollproof ./cmd/rollproof`.
- **No new configuration key, no new reason code, no new schema version.** Step 4 builds keys that version 1 already has. `result.json` stays at `schema` 1; this plan adds fields to it and changes the meaning of none (section 10).
- **Reason codes,** in the order of section 10's list, which also picks the deciding reason: `probe-failures`, `transition-stalled`, `pod-unschedulable`, `command-failed` (fail); `config-rejected`, `runner-unsupported`, `cluster-failed`, `chart-fetch-failed`, `setup-failed`, `install-failed`, `nothing-watched`, `workload-unsupported`, `target-unsupported`, `probe-not-started`, `probe-exited`, `probe-line-invalid`, `steady-not-clean`, `probe-silent`, `no-pod-replaced`, `restart-failed`, `interrupted`, `internal-error` (not verified).
- **Window names:** `install`, `steady`, `deploy`, `rollback`, `restart`. `install` is never in `result.json`'s `windows` and never in the console's first table.
- **Transitions** (section 7, "Transitions"): Helm `previous` and `candidate` give deploy, then rollback; Helm `candidate` only gives restart; commands `install`, `deploy` and `rollback` give deploy, then rollback; `install` and `deploy` give deploy; `install` only gives restart.
- **Names that are contract** (sections 6 and 9): the namespace `rollproof-probe`, the label `app.kubernetes.io/name: rollproof-probe`, the container `probe`, the prefix `rollproof-`, the registry host `rollproof.test`, the file `rollproof.yaml`.
- **Every probe pod** (section 6): user 65532, not root, no privilege escalation, every capability dropped, the runtime's default seccomp profile, restart policy `Never`, on the control-plane node. The built-in probe's pod mounts no service-account token; a custom probe's pod mounts the namespace's default one. The drain is 10 s for a custom probe, and the request timeout plus 5 s for the built-in probe.
- **Defaults** (section 8): `cluster.workers` 3; `probe.http.rate` 200; `probe.http.timeout` 2s; `limits.steady` 10s; `limits.settle` 15s; `limits.timeout` 5m; `limits.silence` 5s. A command past `limits.timeout` gets SIGTERM on its process group and SIGKILL 10 s later. The clock check's bound is 2 s (first plan, decision 5).
- **Variables taken out of every command's environment** (section 7): `HELM_KUBECONTEXT`, `HELM_KUBEAPISERVER`, `HELM_KUBETOKEN`, `HELM_KUBECAFILE`, `HELM_KUBEASUSER`, `HELM_KUBEASGROUPS`, `HELM_KUBEINSECURE_SKIP_TLS_VERIFY`, `HELM_KUBETLS_SERVER_NAME`, `HELM_NAMESPACE`, `HELM_DRIVER`, `HELM_DRIVER_SQL_CONNECTION_STRING`. The `setup` commands and the commands driver lose them too.
- **Names and no values** (section 8): the console and `result.json` give the names of a custom probe's `env` entries and the counts of its `command` and `args`, never their values. The evidence folder may hold the values. Text that rejects a configuration never quotes a value.
- **Rule 10.** After the verdict is decided, nothing changes it: an error while the tool writes the report, completes the evidence or deletes the cluster is a warning, on the console and in `result.json`.
- **Tests.** Unit tests need no cluster and no network and run with `go test ./...`. A test that needs a container engine carries the build tag `cluster`; a fixture run carries the tag `e2e`. Run those with `-p 1`. On a machine whose engine is nerdctl, put `env KIND_EXPERIMENTAL_PROVIDER=nerdctl` before the command, and `CGO_ENABLED=0` with it for cluster and e2e tests. Never set `GOOS` for `go run` or `go test`.
- **Commits.** One commit per task. A message is a plain sentence that says what changed. It carries no attribution to an AI tool: no `Co-Authored-By` trailer that names one, no "Generated with" line, no session link.
- **Nothing leaves the machine.** No push, no pull request and no run on GitHub Actions without the maintainer's word. A container, image, network or volume whose name does not begin with `rollproof-` is never removed.

## Review Focus

The design implies these and names no test for them. Each has its test in the task that owns the code.

1. **A `setup` command or a command of the commands driver reads its standard input or asks a question.** It reads end of file at once and goes on or fails; it never waits until `limits.timeout`. Test in Task 4 (`TestSetupCommandsReadNoInput`).
2. **A custom probe's `env` takes a value from a Secret that does not exist.** The pod never starts; the run ends as `probe-not-started` after `limits.timeout`, and the text names the Secret's error from the pod's state, not "no observation" alone. Test in Task 10 (`TestStartNamesAMissingSecret`).
3. **An `archive` path that is a folder, an empty file, not a tar, or a tar that holds no image the tool can read.** The configuration is rejected before a cluster exists, with the key's name and no value. Test in Task 1 (`TestArchiveThatIsNotAnImage`).
4. **The evidence cannot be completed: the API server stops answering after the verdict, the events cannot be listed, the evidence folder is gone, a pod's log is gone before it was read.** The verdict and `result.json` stand. A failure that `run` meets is one warning; a unit that writes evidence as it goes (`watch`'s pod logs, `probe`'s pod status) writes the failure into that file instead, and a file it cannot open is skipped. Tests in Task 17 (`TestEvidenceFailuresAreWarnings`) and Task 7 (a pod gone before its log was read).
5. **A restart in which one watched workload is refused and another was already changed.** The changed one rolls; the window is not verified as `restart-failed`, with the API server's text and the workload's name; the tool does not change the workloads after the refused one. Test in Task 5 (`TestRestartStopsAtTheFirstRefusal`).

## What this plan builds, and what it leaves

| Unit | In this plan | Left for a later plan |
| --- | --- | --- |
| `config` | `archive` checked when the file is loaded; a custom probe's image needs a digest; `setup`, `deploy.commands`, a Helm release with no `previous`, `probe.custom` and `archive` leave the list of what the build cannot run | `registries` and `as` stay on that list (step 5) |
| `images` | `archive`, stored by digest and pulled into every node | `as` (step 5) |
| `deploy` | The commands driver; a Helm driver with no `previous`; `setup` on the same runner | Nothing |
| `watch` | The restart; the moment each transition finished, as it happens; pod lists before and after each transition; the logs of watched pods; the events of both namespaces; the last event in a stall's text; the pods' history for the timeline | Nothing |
| `probe` | The custom probe's pod; a pod that the API server refuses; the moment the tool asked for the pod and the floor of `t`; whether a bad line was a failed check's; the probe pod's status and its other containers' logs in the evidence | Nothing |
| `verdict` | A window with no `ms` or no `lag` has none | Nothing |
| `run` | `setup` in the sequence; restart transitions; a custom probe with no target check; a failed check's bad line written during the drain judged in the last window; the window marks written as they happen; the per-second timeline; the evidence completed before the cluster is deleted | Nothing |
| `report` | The custom probe's lines, the commands driver's releases, a restart's record, "-" and null for a missing slowest check or lag | The job summary and the action's outputs (step 6) |
| `cluster` | kind's logs and the registry's log after a set-up that failed | The mirror, `ca` and credential settings (step 5) |
| `docs/` | `probe-contract.md` | Nothing |
| Tests with a cluster | Rows 5 to 30 of section 12, and the first fixture on the oldest node image of kind v0.33.0, under Helm 3 and Helm 4 | The mirror job (step 5); the action's test and the workflows (step 6); the fixture series (step 7) |

Masking a registry password in a command's output and a pod's log comes with `registries` in step 5: before then, no run holds a password.

## Decisions made in this plan

The design left these open or silent. Each is the smaller choice. The maintainer reviews them with the plan; the ones marked **public** change something a user depends on.

1. **Each key leaves the list of what the build cannot run in the task that wires it from end to end** (Tasks 13 to 15), not in the task that builds its unit. A key the binary accepted before its part is joined would panic in `cmd/rollproof/wire.go`, which reads `cfg.Deploy.Helm` and `cfg.Probe.HTTP` today. `archive` is the exception: Task 1 builds it from end to end, and `wire.go` reads nothing of it, so Task 1 takes it off the list.
2. **public: an `archive` is read as `docker save`, `podman save` and `nerdctl save` write one:** a tar that holds a `manifest.json`. A tar with no `manifest.json`, an OCI layout with none included, is rejected with the configuration as "is not an image archive the tool can read". The check that it holds exactly one image runs when the file is loaded, so both rejections come before a cluster exists (section 8).
3. **public: the restart writes the annotation `kubectl.kubernetes.io/restartedAt`** on each watched workload's pod template, with the tool's clock in RFC 3339, by a strategic merge patch, as `kubectl rollout restart` does. The workloads are changed one at a time in the order of their kind and name; the first refusal ends the attempt, and the workloads after it are not changed (Review Focus 5).
4. **The restart's command moment.** The restart's "command has exited" (section 7, "When a transition has finished") is the moment the last patch returned or the first refusal came. `run` hands it to `watch` as the end of a command, so the rules for a transition do not change.
5. **public: a restart's record in `result.json`.** A transition's `command` is null for a restart, and a new key `restart` holds `"accepted"` or `"refused"`; it is null for a deploy and a rollback. Section 10 asks for "whether the API server took its change" and fixes no name.
6. **public: the commands driver's releases in `result.json`.** Each entry of `releases` has `role` (`install`, `deploy` or `rollback`) and a new key `program`, the first word of its command; the Helm fields are empty. `driver` is `"commands"`, and `name` is `deploy.namespace` (section 10: the console's name is the namespace).
7. **`setup` evidence** goes to `evidence/commands/setup`, `setup-2` and so on, through the runner's own naming. The commands run in order and the first that does not succeed ends the set-up: `setup-failed` for a non-zero exit, a program that cannot start and a command stopped at the limit; a command that a signal stopped is the signal's (`interrupted`), and one stopped by a failure of the tool is `internal-error` (section 7).
8. **The target check is skipped for a custom probe,** and `watch` is told of no Service. The second table's "probed" reads "not known" (section 10).
9. **The custom probe's name in the report** is its image reference as resolved, which carries a digest by section 8's rule. Its kind in `result.json` is `"custom"`; `certificate_checked` is null.
10. **The floor of `t` applies to every probe**, built-in included, with the clock check's bound of 2 s. "The moment the tool asked for the probe's pod" is the tool's clock just before the API call that creates the pod (sections 6 and 14, point 29).
11. **"A failed check's line"** (section 6, the drain rule): a whole line that is a JSON object whose top-level `ok` is `false`, also when it holds `ok` twice and one of them is `false`. The final piece of a stopped probe, with no line end, stays not judged. Such a line's fault, written after the last window ended, is placed in the last window; every other fault after the last window stays a warning. This narrows the first plan's decision 8 as point 29 says.
12. **The events of both namespaces are listed once,** after the verdict and before the cluster is deleted, not followed by an informer: the API server keeps them for an hour (section 10). The stall's last event is listed at the moment of the stall, for the pods and workloads the stall's text names.
13. **The window marks are rewritten whole** in `evidence/windows.json` at each mark: when a window begins, each time a transition comes to be finished, and when a window ends. Each transition's entry keeps every finish in a list `finishes`, so a finish that the settle time undid stays recorded. Section 10 says the folder's layout is not a contract; row 13's harness reads this file (section 12).
14. **The per-second timeline** is `evidence/timeline.tsv`: one row for each second from the probe's first observation to the end of the last window, with the checks, the failures, and the pod events of that second (created, ready, deleted, gone, with the pod's name and workload).
15. **Pod lists** are `evidence/pods/<transition>-before.json` and `-after.json`, from the watch's own view at the transition's start and at its window's end. Pod logs are `evidence/pods/<pod>/<container>.log`, followed from the pod's first sight until it is gone, and `<container>.previous.log` when the kubelet still holds a restarted container's previous run.
16. **The probe pod's status** goes to `evidence/probe-pod.json`, read when the probe's start ends without a first observation and just before the tool deletes the pod; the logs of its other containers, such as a mesh's proxy, go to `evidence/probe-pod/<container>.log`, read once at that moment. The probe's own container stays in `evidence/probe.log`.
17. **kind's logs after a failed creation:** the tool creates the cluster with kind's option to keep the nodes of a failed creation, takes kind's logs of the last failed attempt into `evidence/kind/` and the registry container's log into `evidence/registry.log`, and then removes them as it does today, unless `--keep` is set. Earlier failed attempts are removed before the next as today. A set-up that fails after the cluster exists (an image that cannot be stored or pulled) takes the same logs before the run ends.
18. **The fixtures' kubectl.** `hack/fetchhelm` also fetches kubectl v1.37.0 for linux/amd64 into `.tools/kubectl`, by checksum. The `setup` rows and the commands-driver rows use it, as a user would. The commands-driver rows apply, with `kubectl apply -f`, manifests that the harness renders from the fixture chart with `helm template` before the run; with no `--namespace`, they settle the belief that such a command acts in `deploy.namespace` (section 7; the coherence review's C23).
19. **One fixture probe program,** `fixtures/probe/`, built into the image `rollproof-fixture-probe:dev` from scratch like the fixture service, with a mode chosen by its environment: silent once it reads the candidate's version, exit once it reads it, ignore SIGTERM, and fail every 2 ms for a given time before it checks the Service. The fixture service gains `GET /version`. Rows 6, 7, 16, 17 and 27 use it.
20. **The TCP loop runs in busybox 1.37.0, entered through `images` with `archive`.** The harness pulls busybox by its pinned digest once and saves it to an archive; the configuration names `${images.busybox.ref}`. Rows 15, 18 and 30 so also exercise `archive` from end to end.
21. **The matrix is the design's:** every row runs under Helm 3 and under Helm 4, the oldest-image row included (section 12). Each task runs its own rows while it is built; Task 25 runs the whole suite, which takes about four hours on the developer's machine. `e2e/doc.go` gives `-timeout 360m`.
22. **Point 23's sketch, the probe's start judged in `verdict`, is not built.** The start stays in `probe`; nothing in step 4 needs it moved.
23. **The fixtures keep `limits.settle` at 15 s or more,** above the 7 s that section 14, point 24, names.

## What writing this plan found in the spec

None changes a decision of the design.

- **Section 9, "A custom probe's image":** when `images` does not list it, the kubelet pulls it with the probe's pod, by the pull policy `IfNotPresent`. The tool's `images.Store` does nothing for it, and a pull that fails already shows in the pod's state.
- **Section 10, "Evidence folder":** "the window boundaries and the moment each transition finished, each written when it happens" is built as a file rewritten whole at each mark (decision 13).

## File structure

All paths are under `projects/rollproof/`.

| Path | Change | Task |
| --- | --- | --- |
| `internal/config/validate.go`, `load.go`, `notbuilt.go` | The archive's check and `archive` off `NotBuilt` (1); the custom image's digest (15); keys leave `NotBuilt` (13, 14, 15) | 1, 13, 14, 15 |
| `internal/images/images.go`, `archive.go` (new) | `archive` | 1 |
| `internal/deploy/commands.go` (new), `deploy.go` | The commands driver; `Release.Program` | 2 |
| `internal/deploy/helm.go` | A Helm driver with no `previous` | 3 |
| `internal/deploy/setup.go` (new) | `Setup` | 4 |
| `internal/watch/restart.go` (new) | The restart | 5 |
| `internal/watch/marks.go` (new), `tracker.go`, `watcher.go`, `cmd/rollproof/wire.go` | Finishes as they happen; pod lists; `watch.Options.Evidence` joined | 6 |
| `internal/watch/podlogs.go` (new), `events.go` (new), `watcher.go`, `tracker.go`, `cmd/rollproof/wire.go` | Pod logs, events, the pods' history; `watch.Options.ProbeNamespace` joined | 7 |
| `internal/watch/tracker.go`, `watcher.go`, `events.go`, `internal/run/record.go` | The last event, after the stall's detail lines | 8 |
| `internal/probe/pod.go`, `custom.go` (new), `testdata/pod-custom.golden.yaml` | The custom probe's pod | 9 |
| `internal/probe/pod.go` | A refused pod; the pod's status and other logs in the evidence | 10 |
| `internal/probe/line.go`, `follow.go`, `pod.go` | The floor of `t`; a failed check's bad line | 11 |
| `internal/verdict/vocab.go`, `decide.go`, `observe.go`, `internal/probe/line.go`, `internal/report/record.go`, `result.go`, `console.go`, `testdata/`, `internal/run/record.go` | No `ms` or no `lag`: flags in `verdict`, pointers in `report` | 12 |
| `internal/run/run.go`, `conditions.go`, `cmd/rollproof/wire.go` | `setup` | 13 |
| `internal/run/run.go`, `record.go`, `conditions.go`, `internal/report/record.go`, `console.go`, `cmd/rollproof/wire.go` | Restart transitions; the commands driver joined; their records | 14 |
| `internal/run/run.go`, `record.go`, `internal/report/console.go`, `cmd/rollproof/wire.go` | The custom probe joined; its record and lines | 15 |
| `internal/run/run.go` | A failed check's bad line in the drain | 16 |
| `internal/run/evidence.go` (new), `run.go`, `internal/report/evidence.go` | Marks as they happen (`ReplaceEvidence`); the timeline; the events in `events.txt`; completion as warnings | 17 |
| `internal/cluster/cluster.go`, `logs.go` (new), `cmd/rollproof/wire.go` | kind's and the registry's logs | 18 |
| `docs/probe-contract.md` (new) | The contract for probe authors | 19 |
| `hack/fetchhelm/main.go`, `e2e/harness_test.go`, `e2e/config_test.go` (new, no build tag: the fixture types, the template and the mark reader, tested without a cluster), `e2e/doc.go`, `fixtures/service/main.go`, `fixtures/probe/` (new) | kubectl; the harness's new fields and actions; `/version`; the fixture probe | 20 |
| `fixtures/chart/templates/`, `values.yaml`, `values.schema.json`, `fixtures/service/main.go` | StatefulSet, DaemonSet, a second workload, a policy that denies, hooks; the switches `FAIL_AFTER`, `FAIL_FOR`, `JOB_SLEEP` | 21 |
| `e2e/fixtures_test.go`, `e2e/custom_test.go` (new), `e2e/commands_test.go` (new) | The rows | 22, 23, 24 |
| `README.md` | What works | 25 |

## The tasks

One plan in seven files. Read this file first, then the file of the task at hand. Tasks are numbered through all files and are done in order.

| File | Tasks | What |
| --- | --- | --- |
| `01-config-and-images.md` | 1 | `archive` |
| `02-deploy.md` | 2 to 4 | The commands driver, a Helm driver with no `previous`, `setup` |
| `03-watch.md` | 5 to 8 | The restart, marks and pod lists, pod logs and events, the last event after a stall's details |
| `04-probe-and-verdict.md` | 9 to 12 | The custom probe's pod, a refused pod, the floor of `t` and a failed check's bad line, no `ms` or `lag` |
| `05-run-report-cluster.md` | 13 to 19 | `setup`, restarts and the commands driver, the custom probe, the drain, the evidence, kind's logs, the contract document |
| `06-fixtures-harness.md` | 20 to 21 | kubectl, the harness, the fixture probe, the chart's variants |
| `07-fixture-rows.md` | 22 to 25 | The rows of section 12, and the whole suite |

| Task | What it delivers |
| --- | --- |
| 1 | `archive` from the configuration's check to the nodes' pull |
| 2 | `deploy.NewCommands`, a `Deployer` for the commands driver |
| 3 | `deploy.NewHelm` with no `previous`: install the candidate, transitions `[restart]` |
| 4 | `deploy.Setup` |
| 5 | `(*watch.Watcher).Restart` |
| 6 | `(*watch.Watcher).Finishes`; pod lists before and after a transition |
| 7 | Pod logs and events in the evidence; `(*watch.Watcher).PodEvents` |
| 8 | The last event message, after a stall's detail lines |
| 9 | The custom probe's pod: `PodOptions.Command`, `Env`, `ServiceAccountToken`; `probe.CustomEnv` |
| 10 | A refused pod is `probe-not-started`; `PodOptions.Evidence` |
| 11 | `ParseLine`'s floor of `t`; `Line.FailedCheck` and `Event.FailedCheck` |
| 12 | No `ms` or no `lag` gives null and "-": `Observation.HasMs`, `HasLag`; `WindowResult.HasSlowest`, `HasLag` |
| 13 | `setup` in the run; `Cluster.Setup`; `setup` leaves `NotBuilt` |
| 14 | Restart transitions and the commands driver in the run; `deploy.commands` and `deploy.helm.previous` leave `NotBuilt` |
| 15 | The custom probe in the run; `probe.custom` leaves `NotBuilt` |
| 16 | A failed check's bad line written during the drain judged in the last window |
| 17 | `windows.json` as it happens, `timeline.tsv`, events, completion as warnings |
| 18 | kind's logs and the registry's log after a failed set-up |
| 19 | `docs/probe-contract.md` |
| 20 | kubectl in `.tools/`; the harness's fields; `/version`; the fixture probe image; busybox's archive |
| 21 | The chart's variants |
| 22 | Rows with the built-in probe: 5, 8, 9, 11, 12, 13, 14, 19, 20, 23, 24, 25, 26, 28, and the oldest node image |
| 23 | Rows with a custom probe: 6, 7, 15, 16, 17, 18, 27, 30 |
| 24 | Rows with `setup` and the commands driver: 10, 21, 22, 29 |
| 25 | The whole suite under both Helm majors; the README |

## Interfaces between tasks

The names that one task produces and a later one uses. Each task's Interfaces block repeats the ones it touches with their interface comments; these are the names, so that the files agree.

- `deploy.CommandsOptions{Commands config.Commands; Timeout time.Duration; Runner Runner}` and `deploy.NewCommands(o CommandsOptions) *CommandsDriver` (Task 2). `*CommandsDriver` implements `Deployer`: `Fetch` does nothing; `Transitions` follows section 7's table; `Releases` gives one `Release` for each command, with `Role` and `Program`.
- `deploy.Release.Program string`: the first word of the commands driver's command for that role; empty for Helm (Task 2).
- `deploy.NewHelm(o HelmOptions) *HelmDriver` with `o.Helm.Previous == nil`: `Fetch` fetches the candidate only, `Install` installs it, `Transitions` gives `[verdict.Restart]`, `Releases` gives the candidate only. `Deploy` and `Rollback` are not called (Task 3).
- `deploy.Setup(ctx context.Context, r Runner, commands [][]string, timeout time.Duration) Result` (Task 4): runs the commands in order with the name `setup`; returns the `Result` of the first that did not succeed, or a `Result` named `setup` with `Ending` `Succeeded` when all did or there were none.
- `watch.Restart{Began, Ended time.Time; Accepted bool; Workload string; Text string}` and `(*watch.Watcher).Restart(ctx context.Context) watch.Restart` (Task 5): changes every watched workload as decision 3 says. `Workload` and `Text` name the refused workload and the API server's answer.
- `watch.Finish{Name verdict.WindowName; At time.Time}` and `(*watch.Watcher).Finishes() <-chan watch.Finish` (Task 6): one value each time a transition comes to be finished; never blocks the watch.
- `watch.Options.Evidence func(name string) (io.WriteCloser, error)` (Task 6): where `watch` writes the pod lists (Task 6) and the pod logs (Task 7); nil writes nothing. `watch.Options.ProbeNamespace string` (Task 7): the probe's namespace, whose events `WriteEvents` lists too; `watch` does not import `cluster`.
- `watch.PodEvent{At time.Time; Pod, Workload string; Kind string}` with `Kind` one of `created`, `ready`, `deleted`, `gone`, and `(*watch.Watcher).PodEvents() []watch.PodEvent` (Task 7), in order of time.
- `(*watch.Watcher).WriteEvents(ctx context.Context, w io.Writer) error` (Task 7): lists the events of `deploy.namespace` and `rollproof-probe` and writes them, oldest first.
- `watch.Transition.LastEvent string` (Task 8): the last event message for what the stall's text names, empty when none; `run`'s record prints it as the reason's last detail line, after the not-complete workloads and pods.
- `probe.PodOptions.Command []string`, `Env []corev1.EnvVar`, `ServiceAccountToken bool` (Task 9), and `Evidence func(name string) (io.WriteCloser, error)` (Task 10).
- `probe.CustomEnv(env []config.EnvVar) ([]corev1.EnvVar, error)` (Task 9): turns the configuration's entries into a container's, `valueFrom` included, refusing a form the API would refuse.
- `probe.ParseLine(text string, written, asked time.Time) Line` (Task 11): `asked` is the moment the tool asked for the pod; zero skips the floor. `probe.Line.FailedCheck bool` and `probe.Event.FailedCheck bool`: the line was a failed check's line in decision 11's sense (an `Event`'s `Fault` is a `*verdict.Condition`, which carries no such flag).
- `verdict.Observation` gains `HasMs bool` and `HasLag bool` (the line gave the field), and `verdict.WindowResult` gains `HasSlowest bool` and `HasLag bool` (Task 12): false when no passed line of the window gave `ms`, or no line gave `lag`. The install record's counts follow the same rule.
- `report.Transition.Restart *string` (`"accepted"` or `"refused"`; nil for a deploy and a rollback) and `report.Transition.Command *string` (nil for a restart), by decision 5; `report.Release.Program string`; `report.Probe.CertificateChecked *bool` (nil for a custom probe, decision 9) (Tasks 14 and 15).
- `run.Cluster.Setup(ctx context.Context) deploy.Result` (Task 13); `run.Cluster.Probe` returns `(probe.Probe, error)`, and `wiring.check` calls `probe.CustomEnv` so that a bad `env` is `config-rejected` before a cluster exists (Task 15); `run.Watcher` gains `Restart`, `Finishes`, `PodEvents` and `WriteEvents` (Tasks 14 and 17).
- `(*report.Writer).ReplaceEvidence(name string, b []byte) error` (Task 17): writes the file whole under that name, replacing an earlier one, for `windows.json`.
- Evidence names are given in full by the unit that writes them (`pods/…`, `probe-pod.json`, `probe-pod/…`, `kind/…`, `registry.log`); `wire` passes the session's evidence function to each unit with no prefix (Tasks 15 and 17).
- `cluster.Spec.Evidence func(name string) (io.WriteCloser, error)` and `(*cluster.Cluster).CollectLogs(ctx context.Context) error` (Task 18).

## Alternatives considered

The blocks for `internal/probe` and `internal/deploy` are the spec's own, copied unchanged from sections 6 and 7 in the first plan; step 4 builds the parts of them that the skeleton left and changes neither contract. The Go seams below are this plan's.

### Where `setup` runs

Common case: `run` runs the caller's `setup` commands after the cluster is ready and before the install, and maps the first that did not succeed to a reason.

**A. A method of `run.Cluster`**

- `Cluster.Setup(ctx context.Context) deploy.Result`

```go
res := s.cluster.Setup(ctx)
if c, failed := setupCondition(res); failed { s.tl.Before = append(s.tl.Before, c) }
```

**B. A method of `deploy.Deployer`** (expected to lose)

- `Deployer.Setup(ctx context.Context) Result`

```go
res := s.deployer.Setup(ctx)
```

| | A. On `Cluster` | B. On `Deployer` |
| --- | --- | --- |
| Ease of use for the common caller | One call | One call |
| Interface size | One method on the bag of real units | One method on the public deploy contract, in both drivers |
| Generality | The same for both drivers by construction | Each driver must do it the same way |
| Information hidden | `run` sees no runner | The same |
| Implementation simplicity | `wire` calls `deploy.Setup` with the session's runner | Both drivers call `deploy.Setup` |
| Efficiency | Equal | Equal |

Third design: not needed; neither makes `run` do extra work.

Pick: A.

Losers: B, because `setup` is not a transition, section 7's contract does not list it, and every driver would repeat the same call.

### How the restart reaches `watch`

Common case: in a restart's window, `run` has every watched workload restarted and then waits for the transition as for any other.

**A. A restart operation, then the usual transition**

- `(*watch.Watcher).Restart(ctx context.Context) watch.Restart`
- `Watcher.Transition(ctx, verdict.Restart, began, exited)` as today

```go
exited := make(chan struct{})
go func() { res = s.watcher.Restart(ctx); close(exited) }()
tr := s.watcher.Transition(ctx, verdict.Restart, began, exited)
```

**B. One call that restarts and waits** (expected to lose)

- `(*watch.Watcher).RestartTransition(ctx context.Context, began time.Time) (watch.Transition, watch.Restart)`

```go
tr, res := s.watcher.RestartTransition(ctx, began)
```

| | A. Restart, then Transition | B. One call |
| --- | --- | --- |
| Ease of use for the common caller | The same shape as a command | One call |
| Interface size | One new method | One new method |
| Generality | The transition's rules stay in one path for every kind of transition | A second path that must keep the same rules |
| Information hidden | `run` knows the restart is a step that ends | Hides it |
| Implementation simplicity | `Transition` unchanged | `follow` called from two places |
| Efficiency | Equal | Equal |

Third design: not needed.

Pick: A.

Losers: B, because a second path into the tracker is where the rules of a finished transition would drift apart.

### Where evidence is written

Common case: pod logs must be written while the pods live, and the timeline needs the probe's observations and the pods' history after the run.

**A. The unit that sees it writes it while it happens; `run` writes what needs the whole run**

- `watch.Options.Evidence`, `probe.PodOptions.Evidence`, `cluster.Spec.Evidence`; `run` writes `timeline.tsv`, the events and `windows.json`.

**B. Every unit hands its data to `run`, which writes everything** (expected to lose)

- `Watcher.PodLogs() map[string][]byte`, and the same for the probe's pod and kind.

| | A. Written where seen | B. All through `run` |
| --- | --- | --- |
| Ease of use for the common caller | `run` passes one function once | `run` copies every kind of data |
| Interface size | One field per unit | One accessor per kind of data |
| Generality | Streams as large as the logs are | Holds every log in memory |
| Information hidden | Each unit's files are its own | `run` learns every unit's data |
| Implementation simplicity | Each unit writes what it holds | Buffers and accessors |
| Efficiency | No copy in memory | Every log held until the end |

Third design: not needed.

Pick: A.

Losers: B, because a pod's log cannot be held in memory for a long run, and section 10 asks that it be followed while the pod lives.

Design delta: better, because `run` stays the one place that knows the run's shape, and each unit's new evidence stays behind the unit that sees it.
