# rollproof: the action, the job summary and the remaining workflows (build-order step 6) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build step 6 of the design's build order. The tool writes the step's outputs (`out`, and `verdict` at two moments) and a job summary on GitHub Actions; after the final `result.json` it writes the outputs, the summary and the console's text before it completes the evidence; it stops waiting for kind at a signal in every call into kind before the verdict; under Podman it asks the engine for a node image by the name that kind's Podman provider uses. The composite action (`action.yml` with its script `action.sh`) and the tool's own image come with a test of the action in three runs. The suite runs on GitHub in three jobs, its harness choosing the Helm major by a variable and keeping GitHub's files out of the runs it starts. The README says what works and how to call the action. Step 5's small leftovers are carried or answered. On GitHub Actions the tool and the action's script keep the runner from reading a workflow command in what they print (Task 14, added during the build). Nothing runs on GitHub until the maintainer says so (Tasks 1 and 13).

**Architecture:** `report` gains two things and `run` calls both: `GitHub`, the step's two files as the environment names them, which appends outputs in the delimiter form and summary text; and `Summary`, a pure function that renders the console's blocks as Markdown, with every text from outside the tool inside a fenced code block or a code span. `run.finish` writes, in this order, the final `result.json`, the outputs, the summary and the console's text, and only then completes the evidence; a warning after the console is a line of the console and of the summary. `cluster` sends every call into kind that comes before the verdict through one function, `untilDone`, that stops waiting when the context ends and leaves kind's goroutine to finish; the next run's removal of leftovers deletes what kind left. The action is one composite step that `exec`s `action.sh`, which `exec`s the tool, so that the tool is the step's first process and gets the runner's signals. The workflows are files of their own, started by hand.

**Tech Stack:** As built: Go 1.27, `k8s.io/client-go` v0.37, `sigs.k8s.io/kind` v0.33.0 as a library, `github.com/google/go-containerregistry` v0.22.1, `go.yaml.in/yaml/v3`, `github.com/google/go-cmp` in tests; bash for `action.sh`; Helm 3.22.1 and 4.3.2 and kubectl v1.37.0 for the fixtures, fetched by `hack/fetchhelm`; on GitHub, the hosted images `ubuntu-24.04` (Docker 28.0.4, Helm 3.22.1) and `ubuntu-26.04` (Docker 29.4.2, Helm 4.3.2). No new Go dependency.

**Spec:** `projects/rollproof/docs/specs/2026-11-13-rollproof-design.md`, at `1a705ba` of the branch `rollproof-step6-design`. Section numbers in this plan are that document's. Section 14, point 32, holds the rulings of 2026-11-22 for this step and overrides anything older; its last bullet holds the maintainer's choice of how the two new workflows are started (decision 1). Section 8's paragraph "Workflow commands", section 14, point 33, and the bullets that name them in sections 10 to 13 and 16 were added during the build, at `7286764`; point 33 holds the ruling that Task 14 builds. An executor reads the sections a task names before it starts the task. Where the design is silent, the decisions of `docs/plans/2026-11-16-first-plan/README.md`, the rulings of `docs/plans/2026-11-17-install-window/README.md`, the decisions of `docs/plans/2026-11-19-step4/README.md` and `docs/plans/2026-11-20-step5/README.md`, section 14's points 24 to 31, and this plan's decisions below stand. The records `docs/proofs/p03-p04-p20-github-runner.md` and `docs/proofs/p24-hosted-runner.md` hold what the runner does and what the first runs on Docker took.

## Global Constraints

Every task's requirements include these.

- **Linux only.** Module `rollproof`. Apache-2.0. No file that this plan writes or changes under `projects/rollproof/` cites a row of the host repository's decision log or follows its service rules. The workflow files under `.github/workflows/` sit outside `projects/`, and they hold nothing that looks like one of the host's row IDs (a capital D, G or O, a hyphen and digits). The host repository's citation check reads tracked Markdown only, not these files, so that rule is checked by a search, run from the repository root with the workflow files in place of FILE: `grep -nE '[DGO]-[0-9]{3}' FILE FILE`, which must print nothing (grep then exits 1, which is the pass). After a change to the repository, `bun tools/citations/cli.ts repo`, run from the repository root, must exit 0 all the same, as the host asks for any change. For both, an exit code of 2 means the check did not run: report it as unchecked, never as a pass.
- **Paths and commands.** Commands run from `projects/rollproof/` unless a step says "from the repository root". Every command a person types behaves the same in bash and in fish: no unquoted glob, no `VAR=value command` (write `env VAR=value command`), no `$(…)`. Scripts that only GitHub's runner or a test runs (`action.sh`, the workflows' `run:` blocks) are bash and may use bash's forms.
- **Git.** Every git command runs on its own: never inside a compound command, a pipe or a redirection. Stage by path.
- **The binary is static.** `env CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=dev" -o bin/rollproof ./cmd/rollproof`. `GOOS` is never set on this machine, for any command. `GOARCH=arm64` appears only in the arm64 build of Task 10, which runs nothing.
- **No new configuration key, no new reason code, no new schema version.** `result.json` stays at `schema` 1 and gains no field. The outputs and the job summary are new things the tool writes, not fields.
- **Reason codes,** in the order of section 10's list: `probe-failures`, `transition-stalled`, `pod-unschedulable`, `command-failed` (fail); `config-rejected`, `runner-unsupported`, `cluster-failed`, `chart-fetch-failed`, `setup-failed`, `install-failed`, `nothing-watched`, `workload-unsupported`, `target-unsupported`, `probe-not-started`, `probe-exited`, `probe-line-invalid`, `steady-not-clean`, `probe-silent`, `no-pod-replaced`, `restart-failed`, `interrupted`, `internal-error` (not verified).
- **Names that are contract** (sections 6, 8 and 9, and this plan's public decisions): the namespace `rollproof-probe`, the prefix `rollproof-`, the registry host `rollproof.test`, the file `rollproof.yaml`; the outputs `verdict` and `out`; the action's inputs `config`, `image` and `out`; the files `action.yml`, `action.sh` and `Dockerfile` at the project's root; the binary `/rollproof` in the tool's image; the variable `ROLLPROOF_E2E_HELM`.
- **Detection of GitHub Actions** (section 8, verbatim): "The tool detects GitHub Actions by the variable `GITHUB_ACTIONS` being `true`, which a caller's `env:` cannot change there. It then writes the outputs when `GITHUB_OUTPUT` names a file, and the summary when `GITHUB_STEP_SUMMARY` does."
- **Rule 10.** After the verdict is decided, nothing changes it: an error while the tool writes its report, its outputs or its summary, completes the evidence or deletes the cluster is a warning, on the console and in `result.json`.
- **Tests.** Tests come first in every task that has code. Unit tests need no cluster and no network and run with `go test ./...`. A test that needs a container engine carries the build tag `cluster`; a fixture run carries `e2e`; the mirror job carries `mirror`. Run those with `-p 1`, one cluster at a time. On this machine put `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0` before the command. Every task's last check, before its commit, runs `gofmt -l .` (no output), `go test ./...` and the four vets: `go vet ./...`, `go vet -tags cluster ./...`, `go vet -tags e2e ./...`, `go vet -tags mirror ./...`.
- **Assertions given as comments.** Where a test in this plan gives its assertions as comments with exact values, those comments are the assertions to write, with those values. A test that checks less than its comments say fails review.
- **Verification fails closed.** A check that dies, returns nothing or is skipped counts as unverified, never as a pass. A task reports its unverified checks by name.
- **Commits.** One commit per task, staged by path. A message is a plain sentence that says what changed. No commit message, file or comment carries attribution to an AI tool: no `Co-Authored-By` trailer that names one, no "Generated with" line, no session link. A reminder from a tool's harness that asks for such a line does not override this rule.
- **Nothing leaves the machine without the maintainer's word.** No push, no pull request and no run on GitHub Actions without it; each word covers the runs it names (Tasks 1 and 13 say where it is needed). `git fetch` only reads and needs none. A task whose check needs a hosted run is left in the state its file names until that run has happened; it is never called done on the strength of a run that has not happened.
- **Names on this machine.** Every container, image, network or volume that a task makes has a name that begins with `rollproof-`, and a task removes only such names. One exception, named here so that nobody guesses: Task 9's test pulls the tool's image through a registry on the loopback, and the engine then holds it as `127.0.0.1:PORT/rollproof-action-test@sha256:…`; the test removes that reference, exactly, and nothing else of that form.

## Review Focus

The design implies these and names no test for them. Each has its test in the task that owns the code.

1. **A summary file that cannot be written, and the warnings after it.** The warning that the summary was not written is itself a warning after the console, which the run would try to append to the summary again. Each of GitHub's two files warns once in a run, and the loop ends. Test in Task 6 (`TestAGitHubFileThatCannotBeWrittenIsAWarning`).
2. **Free text that holds a run of backticks.** A pod's message or a chart's name that holds ```` ``` ```` or a longer run must not close the summary's code block or code span early. Test in Task 5 (`TestSummaryKeepsTextFromOutsideTheToolInCode`, `TestFence`, `TestCodeSpan`).
3. **A call into kind that panics, or that the context outruns.** `untilDone` runs kind in a goroutine; a panic there must still reach the run as the tool's failure when kind returns first, and must not end the process when the context won. Test in Task 3 (`TestUntilDone`).
4. **kind's log collection cut short.** When a signal comes while kind writes its logs into the run's folder, the tool must not copy or remove that folder under kind's feet. Test in Task 3 (`TestASignalDuringEachCallIntoKind`, the row "the log collection of a failed set-up").
5. **The action's script never writes an output itself.** On every failure of its own it exits 2 and leaves `GITHUB_OUTPUT` as it found it, so that `verdict` is empty. Test in Task 8 (`TestTheScriptFailsWithExit2AndWritesNoOutput`).
6. **The runner's own GitHub variables in the suite.** On GitHub the job's `GITHUB_OUTPUT` and `GITHUB_STEP_SUMMARY` reach the harness; the runs it starts must not see them. Test in Task 7 (`TestRunEnvKeepsTheJobsGitHubFilesOut`).
7. **A line that GitHub's runner reads as a workflow command.** No text from outside the tool may reach the runner as a command while the tool or the action's script runs. A reviewer tries to put a line before the stop line, or after the resume line: a bad flag and an argument that is no flag, whose texts the flag package prints before the run; a rejected configuration; a results folder that cannot be opened; a run that ends by a signal, which must print no resume line; a panic that escapes the run; a failure of the script before its stop line, of which only `uname`'s own output may come first; a token that a caller could set through `env:` or guess. The two places left open, a line on standard error just before the tool's end and a stop line of standard error that the runner reads after the resume line of a run that ended at once, are named in section 8 and are no test's: both lie in the order in which the runner reads the two streams. Tests in Task 14 (`TestTheRunCommandsLinesComeFirstAndLast`, `TestASignalLeavesWorkflowCommandsStopped`, `TestAPanicInTheRunLeavesNoResumeLine`, `TestTheScriptStopsWorkflowCommandsFirst`, `TestTheScriptFailsWithExit2AndWritesNoOutput`, `TestWorkflowCommandFaults`).

## What this plan builds, and what it leaves

| Unit or file | In this plan | Left for a later plan |
| --- | --- | --- |
| `report` | `GitHub` (outputs and summary files), `Summary`, `SummaryLine`, the console's blocks as data for both | Nothing |
| `run` | The outputs at both moments, the summary, the console before the evidence, late warnings as lines of both | Nothing |
| `cluster` | `untilDone` around every call into kind before the verdict; kind's Podman name for node images; the comment on `pickEngine` | Nothing |
| `cmd/rollproof` | Tests of the signal's path through `failedClientError` and `storeFailed`, and of the exit after a signal with a refused write; the lines for GitHub's workflow commands around `run` (Task 14) | Nothing |
| `action.yml`, `action.sh`, `Dockerfile` | Every line, the script's stop line of Task 14 among them; tests with fake engines and with this machine's engine | The digest, version and repository written at a release (step 7) |
| The tool's image | Built from `Dockerfile` by the engine, for one platform, in the action's test | The release's index for amd64 and arm64 (step 7) |
| `e2e` harness | GitHub's variables kept out; files of their own for the first fixture and the mirror job; the Helm major by `ROLLPROOF_E2E_HELM`; the check of the lines for workflow commands in every run (Task 14) | Nothing |
| Workflows | `rollproof-e2e.yml`, `rollproof-action.yml`, the unit workflow's new checks; the two names on `main` (Task 1) | Moving them to a repository of their own, kind's automated updates, releases (step 7) |
| `README.md`, `e2e/doc.go` | What works, the action, the outputs, the summary, the workflows, the variable | The action's use from another repository (step 7) |
| `docs/proofs/` | The record of the runs on GitHub and its row (Task 13) | Nothing |
| Step 5's leftovers | Eight carried, one of them in part (Tasks 3, 7, 12) | Twenty-nine kept or already answered, each with its reason (Task 12) |

## Decisions made in this plan

The design left these open or silent. Each is the smaller choice. The maintainer reviews them with the plan; the ones marked **public** change something a user sees or depends on, and the ones marked **internal** do not.

1. **The maintainer's choice of 2026-11-22, public: how `rollproof-e2e.yml` and `rollproof-action.yml` are started before step 6 is merged (section 14, point 32, its last bullet).** The maintainer chose the first of three ways, which the plan writes. Task 1 puts both names on `main` through a small pull request, from a branch of its own, as files whose one job never runs (`if: false`); when that pull request is merged, the step's branch takes `main` in, and an add/add conflict on the two files keeps the step's version. Only Task 13's runs on GitHub depend on it; no build task waits for it. Its push, its pull request and its merge need the maintainer's word. The other two ways, which lost, and what would have changed in the plan under each:
   - **The new jobs as further parts of `rollproof-proofs.yml`.** Task 1 falls away, and Task 10 also adds two values of `part` to `rollproof-proofs.yml` whose jobs are copies of those of the two new files, which Task 13 then starts in their place. The two new files themselves stay unrun until after the merge, so graduation's "green on GitHub" waits for one more run.
   - **A trigger other than by hand for the time of the step.** Task 1 falls away, and Task 10 gives both files a `push` trigger limited to the branch `rollproof-step6-design` under which every job is skipped (`if: github.event_name == 'workflow_dispatch'`); Task 13 pushes, then starts them by hand with `gh workflow run FILE --ref rollproof-step6-design`, and a last commit removes the `push` trigger before the merge. This rests on the case nobody tried; one try of it alone takes a scratch branch holding a workflow of one job that prints a line, under both triggers, one push (whose run starts no job; that a run whose jobs are all skipped bills no minute is from memory, not read), one `gh workflow run` against that branch, and the branch's deletion afterwards: at most one billed minute, and a push and a start by hand that both need the maintainer's word.

   What lost, and why: the second way, because the two files are what graduation needs green; the third, because it rests on the case nobody tried and spends a push trigger in a host repository whose minutes are billed.
2. **public: the outputs' form.** Each output is appended in the delimiter form: `NAME<<DELIMITER`, the value, the delimiter, each on a line of its own, the delimiter `ROLLPROOF_EOF_` followed by 26 random characters (`crypto/rand.Text`), drawn again while the value holds it. The first moment writes `out` and then `verdict` in one append, right after the first `result.json`; the second writes `verdict` in one append, right after the final `result.json`, also when that write was refused (then `fail` for a decided fail and `not-verified` otherwise). The outputs are not masked: they are written before the configuration loads, and their values are the results folder's absolute path and a word of the vocabulary. The file is opened for appending and never made: a `GITHUB_OUTPUT` that names no existing file is a write that fails. What lost: the plain form `NAME=VALUE`, because a value with a line end would be a line that is no output, which fails the step and loses every output after it (point 4); a fixed delimiter, because a value could hold it.
3. **public: a GitHub file that cannot be written.** A failed write of the outputs is the warning "the step's outputs were not written: ERROR", and of the summary "the job summary was not written: ERROR". Each file warns once in a run; later writes to it are still tried, and fail without a warning. When the first output write fails, `verdict` stays empty although the tool started. What lost: a warning for every failed write, because a summary that cannot be written would then warn once more for every warning appended to it, without end.
4. **public: what the job summary looks like.** The summary holds the console's blocks, in the console's order (section 10): the first line as a level-three heading, `### rollproof: WORD` followed, when the console's first line goes on, by the rest of it in a code span; the set-up block (releases, images, probe, install, changed settings, removed variables) as a fenced code block, line for line as the console prints it, so that its columns stay aligned; the table of windows as a Markdown table, its numbers right-aligned, and the lines under it on failures that crossed a start as a fenced code block; the table of transitions as a Markdown table, the workload's name in a code span; on a pass, the lists as the paragraphs `exercised` and `not exercised`, each followed by a Markdown list whose items read `NAME: TEXT` for a transition and the line itself for what was not exercised; and the reasons, the warnings and the results line as a fenced code block. A warning that arises after the summary was written is appended as one more fenced code block of one line, after a blank line. The line that names a kept cluster is not in the summary: it names a path on the runner. Blocks are separated by one blank line, and the summary ends with a line end. The whole summary of section 10's pass:

   ````markdown
   ### rollproof: PASS `orders (Helm) on Kubernetes v1.37.0 in kind`

   ```
   releases    previous   orders 1.4.2, oci://registry.example/charts/orders; chart defaults
               candidate  orders 1.5.0, ./chart; values: inline (image)
   images      orders     run by deployment/orders, in pods the deploy created
   probe       built-in HTTP: GET http://orders.orders.svc/api/orders/latest
               200 a second (one check every 5 ms), timeout 2 s, status 200-299
   install     settled after 15.0 s under the probe; failures not judged
               3,000 checks, 6 failed (refused 5, timeout 1), the last 14.9 s before the steady window
               slowest 44 ms, behind 9 ms
   ```

   | window | length | checks | failed | after finish | slowest | behind | result |
   | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
   | steady | 10.0 s | 2,000 | 0 | - | 41 ms | 6 ms | pass |
   | deploy | 33.4 s | 6,680 | 0 | 0 | 212 ms | 38 ms | pass |
   | rollback | 33.9 s | 6,780 | 0 | 0 | 187 ms | 35 ms | pass |

   | transition | workload | probed | pods replaced | quickest stop |
   | --- | --- | --- | --- | --- |
   | deploy | `deployment/orders` | yes | 2 of 2 | 5.3 s |
   | rollback | `deployment/orders` | yes | 2 of 2 | 5.2 s |

   exercised

   - deploy: the candidate's start-up and the previous release's shutdown
   - rollback: the previous release's start-up and the candidate's shutdown

   not exercised

   - the delay before a larger cluster stops routing to a stopping pod
   - connections held open, and requests in flight on a pod after new connections left it
   - the path through an ingress, a load balancer or a mesh
   - routing that depends on where the client is
   - capacity while pods are down, and autoscaling
   - storage that attaches to one node at a time
   - latency below the timeout
   - data migrations
   - a node drain

   ```
   results: /tmp/rollproof-results-7f3a
   ```
   ````

   What lost: every line as a Markdown paragraph, because the columns of the set-up block are lost and every character that Markdown or GitHub reads (emphasis, links, bare addresses that GitHub turns into links, HTML) would need an escape of its own; the whole console in one code block, because section 10 asks for tables and lists.
5. **public: how the summary keeps text from outside the tool from changing the page** (section 10; the place that the security review of section 14, point 13, reads). Text from outside the tool is every string of the record that `report.Mask` masks: the fields without the tag `mask:"keep"`, such as names, chart sources, values files, the probe's target, settings' values, reasons' texts and details, warnings and the results folder. In the summary such text stands only inside a fenced code block or a code span, where GitHub's Markdown reads neither emphasis, links, autolinks nor HTML. A fenced code block opens and closes with a run of backticks one longer than the longest run of backticks in any of its lines, three at the least; no line inside can close it. A code span is delimited by a run of backticks one longer than the longest run inside it, with one space inside each delimiter when the text begins or ends with a backtick or a space, which the renderer takes off again. In a table cell, every `|` is written `\|`, inside a code span too, as GitHub's tables ask. Before any of this, the record is masked by the run's masker and cleaned as the console is: a line end, every other control character, the line and paragraph separators and the bidirectional controls become a space, so that no text adds a line to a table or a heading. The tool's own words (the vocabulary, numbers, fixed sentences) stand as they are; none holds a character that Markdown reads, and the words of a table cell are written with `|` escaped all the same. What lost: escaping every character that Markdown reads with a backslash, because GitHub turns bare addresses into links after the escapes are read, and every missed rule changes the page.
6. **public: the console before the evidence, and warnings after it as lines** (section 10; point 32). `finish` writes the final `result.json`, the second `verdict`, the summary and the console's text, from one record, and then completes the evidence. A warning that arises after the console was printed (from the evidence, the watcher's stop, the cluster's deletion, a failed GitHub write) is printed as a line `warning: TEXT` after it, appended to the summary as a line, and `result.json` is written once more after each batch. The run keeps how many warnings the console and the summary have told (`warningsTold`) apart from how many the last `result.json` holds (`warningsWritten`). After a final `result.json` that could not be written, a later warning is printed and the file is not tried again. What lost: printing the console last, which section 10 now rules out, and telling each warning by a write of `result.json` of its own, which writes the file more often for nothing.
7. **internal: every call into kind before the verdict stops waiting at a signal.** The calls, read in the code: kind's delete in `remove`, at the start of `Create`, in the clean-up of a failed set-up step and through `Cluster.Delete` in `failedClientError`; kind's create and its delete of a failed attempt in `createKind`; kind's log collection in `collectLogs`, after a failed set-up step and through `Cluster.CollectLogs` in `failedClientError` and `storeFailed`; and three short calls of `Create`'s node settings, kind's `ListNodes`, a node's `Role` and `nodeutils.WriteFile`, which also run engine commands without a context. Each runs through `untilDone(ctx, call)`, which returns `ctx`'s error as soon as `ctx` ends and otherwise the call's error; a panic of the call is handed back and raised in the caller when the call returned first, and dropped when `ctx` won. The goroutine that `ctx` outran runs on until kind returns or the process ends; kind's engine commands that it started finish on their own; what they made carries the cluster's name, and the next run's removal of leftovers at the start of `Create` deletes it (section 9, "Name and lifetime"); on a hosted runner the job's end removes the machine. After a log collection that a signal cut short, nothing is copied and the half-written folder stays in the run's folder, which the next run removes. The delete at a run's end keeps its context that no signal ends, so `untilDone` waits there as before. What lost: one goroutine around the whole of `Create`, because the steps that take a context are already cut short, and a failure after `Create` returned would still wait; a kind with a context, which kind's library does not have.
8. **public: kind's node image under Podman** (section 9; point 32). Under Podman the tool asks the engine for every node image, the default one and `cluster.node-image` alike, by the name that kind's Podman provider uses: the copy `podmanName` of kind v0.33.0's `sanitizeImage`, which puts `docker.io/` in front of a name with no registry host, `docker.io/library/` in front of a name of one segment, and drops the tag when the name has a digest. It does so in the three places that name a node image to the engine: the check and the pull in `pullIfMissing`, through the name that `Create` hands it, and the lookup of the digest in `nodeImage`. `nodeImage` returns the name as the configuration or kind's default writes it, with the digest after it; the report and `rollproof version` keep that name. The registry image is not renamed: kind never asks for it, and its default already names its host. Every other engine is asked by the name as written. What lost: an alias in Podman's own settings, which would change the runner's configuration; the rule for every engine, because kind's Docker and nerdctl providers ask by the name as written.
9. **public: the tool's image.** One file, `/rollproof`, the static binary, with the entrypoint `/rollproof` and the user `65532:65532`, the layout of the probe's image (section 9). It is built by the engine's `build` from `projects/rollproof/Dockerfile`, `FROM scratch`, with the folder that holds the binary as its context. In the action's test it is built for the runner's one platform, amd64, and pushed by tag to a registry in the job, then named by its digest. At a release (step 7) it is an index of amd64 and arm64, and its digest is written into `action.yml`. What lost: a program under `hack/` that builds the image with `internal/images`' own layer code, because the engine's build from a Dockerfile is what the harness already uses for every fixture image and what a release pipeline uses.
10. **public: the action's step and its script.** `action.yml` has one composite step, `id: run`, `shell: bash`, whose `env:` carries the three inputs as `ROLLPROOF_ACTION_CONFIG`, `ROLLPROOF_ACTION_IMAGE` and `ROLLPROOF_ACTION_OUT` (never as expressions inside the script) and the three values a release writes, `ROLLPROOF_ACTION_VERSION`, `ROLLPROOF_ACTION_REPOSITORY` and `ROLLPROOF_ACTION_DIGEST`, empty until then; its whole script is `exec bash "$GITHUB_ACTION_PATH/action.sh"`. `action.sh` does the work and ends with `exec` of the tool, so that the tool is the step's first process (point 20). The outputs `verdict` and `out` are mapped from `steps.run.outputs`. What lost: the script inline in `action.yml`, because a test could then run it only by copying it out of the YAML.
11. **public: how the action checks for Linux.** `uname -s` prints `Linux`, or the action stops. What lost: `RUNNER_OS`, which only GitHub's runner sets.
12. **public: the engine the action uses.** The tool's own rule (`pickEngine`), line for line in bash: `KIND_EXPERIMENTAL_PROVIDER` when it is `docker`, `podman`, `nerdctl`, `finch` or `nerdctl.lima`, else the first of `docker`, `nerdctl` and `podman` whose `-v` exits 0 with a first line that begins `Docker version`, `nerdctl version` or `podman version`. The action then starts the tool with `KIND_EXPERIMENTAL_PROVIDER` set to that name (section 8), and with its own six variables unset, so that the tool's environment is the caller's. What lost: kind's own test (exactly one line of output, and `finch` after `nerdctl`), because the action would then pick another engine than the tool where the two differ; the rule alone, with nothing passed on, because two writings of one rule can still differ (point 32).
13. **public: the image the action pulls, and before a release.** An `image` that holds `@sha256:` is used as it is. Any other gets `@` and the digest written at the release, also one that carries a tag, which engines then pull by the digest. An empty `image` means the repository written at the release. Before the first release both are empty, so the action stops with a message unless `image` carries a digest. The action pulls every time it runs; a pull of an image the engine holds asks the registry and moves no layer. What lost: refusing a tag beside the written digest, which a caller who mirrors by tag would meet for nothing; checking the engine's store first, because a reference by digest that a build left untagged is not found by every engine.
14. **public: where the action puts the binary.** In a new folder, `mktemp -d "$RUNNER_TEMP/rollproof-action-XXXXXX"` (`${TMPDIR:-/tmp}` off GitHub), every time; the runner empties its temporary folder when the job ends. What lost: one folder for each digest, used again by a second call in the job, because a binary left there by a call that failed half-way would be used as it is.
15. **public: the route out of the image.** `ENGINE create REF /rollproof`, then `ENGINE export ID | tar -x -C DIR rollproof`, then `ENGINE rm ID`. `create` passes `/rollproof` as the command, so that an engine that wants a command for an image without an entrypoint gets one; the container never starts. `export` and `tar` take the file out, because rootless nerdctl refuses `cp` from a container that never ran. On rootless nerdctl `export` was tried once on 2026-11-22 and gave the file's name without a leading `./`; on Docker and Podman the route was read and never run, and Docker's member name `rollproof` is from memory, not read. Task 9 runs the route on this machine's nerdctl, the cluster job of Task 10 runs Task 9's test on Docker, and the action's own test (Task 13) shows it on Docker. What lost: `cp`, refused by rootless nerdctl; `tar -x` of every member, which fails on the device files that Docker's export holds when the tar does not run as root (from memory, not read).
16. **public: from which folder a relative `config` or `out` is read.** The action's script changes to `GITHUB_WORKSPACE` before it starts the tool (to the folder it started in when that variable is not set), so a relative `config` and a relative `out` are read from the workspace, where `actions/checkout` puts the repository. The tool then reads every relative path in the configuration from the file's own folder, as always (section 8). What lost: the step's starting folder as it is, which for a composite action's step is the workspace by default (from memory, not read) and which a later runner may change.
17. **public: how the action fails itself.** It prints `rollproof: …` to standard error, says what is missing, writes no output and no summary, and exits 2, so that `verdict` is empty (section 8). An `exec` that cannot start the binary is caught and fails the same way: the script sets `shopt -s execfail`, under which a failed `exec` returns, and `set +e` on the line before the `exec`, so that the line after it fails as the other failures do. Both are needed, as tried with bash 5.3.20 on the developer's machine on 2026-11-22. With `set -e` on and the `exec` alone on its line, bash ends the script itself with 126 or 127, its own codes, and no line after the `exec` runs. With the `exec` before `|| fail`, a file of the wrong format reaches `fail`, but for a file that is missing (127) or may not be executed (126) bash still ends the script itself. With `set +e` first, all three reach the line after the `exec`, and a program that starts runs as before. The script's own check that the binary is there and executable stops the last two before the `exec`, so the test's row uses a file of the wrong format, and no test shows that `set +e` is needed. The action prints no workflow command of its own but the line of decision 26 that stops them, so that no later line of the step, an input's text or the tool's output, can become one; it said here before that printing none was enough, which was wrong, since the runner reads commands in every line a step prints, an engine's message and an input in a failure's text included (section 14, point 33). What lost: exit 1, which section 8 keeps for a fail; the script writing `verdict` as `not-verified` itself, which would make a run that never started look like one that did not finish.
18. **internal: the action's own test** (section 12; point 32). One job on `ubuntu-24.04`, limit 30 minutes: a registry container in the job on `127.0.0.1:5000`; the tool's image built and pushed there and named by its digest; the fixture images built as the harness's `TestMain` builds them (`go build` with the version in `-ldflags`, the fixture's Dockerfile, the engine's build, the tags `rollproof-fixture:previous` and `rollproof-fixture:candidate`); then three runs of `./projects/rollproof`, each from a fixed file under `hack/action/testdata/`, each checked by the program `hack/actioncheck`: a pass, from a file whose release name only `env:` on the step sets; a fail, the candidate with no readiness probe, under `continue-on-error`; and a run cut short by `timeout-minutes: 2` on the step, from a file with `limits.steady: 10m`, under `continue-on-error`. The checker reads each run's summary in the runner's masked copy, `$RUNNER_TEMP/_runner_file_commands/step_summary_*-scrubbed`, found by its results line, and fails with a message that names the folder when the folder or the copy is not there. A job that fails uploads the three results folders for three days. Why 2 minutes and 10 minutes: the first fixture took 166 s and 138 s on a hosted runner (p24), of which the steady window, the deploy's and the rollback's windows took 10.0 + 28.1 + 27.3 = 65.4 s and 10.0 + 27.8 + 26.1 = 63.9 s, so its steady window began 100.6 s and 74.1 s after the tool started, the first of them with the pulls of the node and registry images that the third run, last in the job, does not make. A limit of 120 s, less the action's own few seconds before the `exec`, so falls in the steady window on both runners; on a runner whose set-up takes longer, it falls in set-up, kind's creation included, and the assertion holds there too (Task 3). The verdict cannot come before the steady window has run its 600 s, at least 674 s after the start. Section 8 holds `limits.silence` (5 s) shorter than `limits.steady` and `limits.settle`, which 10 minutes keeps, and sets no upper bound on `limits.steady`. What lost: a limit of one minute, which would fall in set-up every time and never with the probe running; three jobs, one for each run, which build the images three times for about three billed minutes more.
19. **internal: the suite's workflow** (section 12; point 32). Three jobs. `cluster` on `ubuntu-24.04`: `go test -tags cluster -p 1 -count=1 -v -timeout 35m` over `./internal/cluster/ ./internal/images/ ./internal/probe/ ./internal/watch/ ./hack/action/`, job limit 45 minutes; it starts no run of the tool that makes a cluster, and uploads nothing. `helm-3` on `ubuntu-24.04` and `helm-4` on `ubuntu-26.04`, each `needs: cluster`, each with `ROLLPROOF_E2E_HELM` set to its major and `go test -tags e2e -p 1 -count=1 ./e2e/ -v -timeout 120m` under a job limit of 130 minutes; a job that fails uploads `${TMPDIR:-/tmp}/rollproof-e2e-results`, where the harness keeps one results folder for each run (`outDir`), for three days. Every job sets `CGO_ENABLED=0` and `KIND_EXPERIMENTAL_PROVIDER=docker`. Why 120 minutes: one major took 4,297 s and 4,411 s on this machine in step 5's last run of the suite, its log not in the repository, and the first fixture on a hosted runner 1.01 to 1.21 times as long (p24); at 1.2 times, with 143 s of set-up and the control's 119 s, a job is about 93 minutes. What lost: `./...` in the `cluster` job, which would also run every unit test and change the time p24 measured, at the cost that a package that gains a test under the tag must be added to the list by hand.
20. **internal: the Helm major and the fourteenth row's control.** The harness reads `ROLLPROOF_E2E_HELM`: `3` runs the Helm 3 subtests only, `4` the Helm 4 ones, unset or empty both, as a developer's run does; any other value makes `TestMain` exit 1 with a message. The control of the fourteenth row runs in every run of its test, under Helm 3, whatever the variable says, so it runs in both Helm jobs: each job runs on its own runner image, with its own kernel and Docker, and the control shows that kind's network plugin enforces the policy there. It costs the Helm 4 job about 2 to 3 billed minutes; the maintainer kept it in both jobs on 2026-11-22 (section 14, point 32). What lost: the control in the Helm 3 job alone, as the record p24 sized it, which leaves the Helm 4 job's row on another image with no control of its own.
21. **internal: GitHub's variables in the harness** (section 12). The harness takes `GITHUB_OUTPUT` and `GITHUB_STEP_SUMMARY` out of the environment of every run it starts; `GITHUB_ACTIONS` stays as it is, so a run on GitHub detects GitHub and writes nothing, since no file is named. The first fixture's test sets, for its own run, `GITHUB_ACTIONS=true` and both variables, naming the files `github-output` and `github-summary` that the harness makes empty in the run's results folder, and reads them. The mirror job's two runs get the same two files, so that the search for the password, which reads every file of the results folder, covers both (section 12, "The mirror job"; see finding 1). For the three workflows that start the harness on GitHub this means one thing: no run writes into the job's own outputs or summary, in `rollproof-e2e.yml`, in `rollproof-mirror.yml`, or in the `first-fixture` job of `rollproof-proofs.yml`, whose step summary therefore stays empty; the first fixture's files stay in its results folder, which a failed Helm job of the suite uploads (decision 19), and the mirror job's stay in its results folder on the runner, since `rollproof-mirror.yml` uploads nothing. What lost: taking `GITHUB_ACTIONS` out as well, which the design does not ask and which changes nothing while no file is named; a fixture of its own for the GitHub files, about 2.5 minutes more for each Helm major.
22. **internal: `rollproof-proofs.yml` stays as it is.** Section 13 names it as the place of the proofs that need a hosted runner, and the records cite it at `cd7bc1f` and `a5051f0`. Nothing in it breaks with this plan: its `first-fixture` job now also checks the first fixture's GitHub files, which the harness makes in the results folder, and its Podman job's alias for kind's node image becomes unneeded and stays harmless. What lost: taking out its jobs that the new workflows repeat, which changes a record's method for no gain.
23. **internal: the unit workflow** (section 8: the first release is built for amd64 and arm64). It gains the vets under each build tag, so that a pull request cannot break the code that only runs by hand, and a build for arm64 that checks the binary is static and for that architecture; it runs nothing built for arm64. What lost: the arm64 build only at the first release (step 7), which would leave a build fault for arm64 unseen until then.
24. **public: the README.** What works on nerdctl and on Docker; the action: its inputs, its outputs with the empty `verdict`, a caller's upload step with `if: always()`, the warning that the evidence holds values unmasked, the warning against `if: always()` on the action's own step, the 7.5 s after a cancel, `exec rollproof run` in a plain step, and that the action cannot yet be used from another repository; the suite's command, its time, the variable; the three workflows, each started by hand. What lost: a page of its own for the action under `docs/`, which section 13's layout does not have.
25. **internal: step 5's leftovers.** The small faults that step 5's reviews deferred and its final fix wave did not carry: Task 12 carries five, Task 3 carries two (one of them in part), Task 7 one; Task 12's file lists all thirty-seven with the answer for each. What lost: carrying them all, because twenty-nine sit in units this step does not change and none is a wrong behaviour that a user meets.
26. **public: the lines for GitHub's workflow commands** (section 8, "Workflow commands"; section 14, point 33; the maintainer's approval of 2026-11-22, made during the build). On GitHub Actions, as `report.GitHubActions` detects it (`GITHUB_ACTIONS` exactly `true`, the rule of `GitHubFrom`, now written once), `rollproof run` prints `::stop-commands::TOKEN` as its first line on standard output and then as its first line on standard error, TOKEN 26 characters of `crypto/rand.Text`, new for each run. When the command has returned and no signal was kept for `endProcess`, it prints `::TOKEN::` as its last line on standard output, whatever the exit code. The code is a wrapper in `cmd/rollproof`, `withWorkflowCommandsStopped`, around the `run` case of `realMain`, so that a bad flag, an argument that is no flag, a rejected configuration and a results folder that cannot be opened all come between the two lines. `version`, `probe` and the top-level usage print neither, since none prints text from outside the tool. A panic that escapes the run prints no resume line: the commands stay stopped for the rest of the step, and the panic's trace is read as plain lines. The lines are written as they are, not through the run's masker; they hold no value of the configuration. `action.sh` prints `::stop-commands::` and a token on both streams right after its check for Linux, before any line that can hold an input or an engine's output, and never prints a resume line; the token is the first line of `/proc/sys/kernel/random/uuid`, read with bash's own `read`, so that no program is added. When it gets no token it fails as its other failures do: `rollproof: no token for the line that stops the runner's workflow commands: PATH gave none`, exit 2, no output. After its `exec`, the tool's own stop lines and its resume line, which name another token, are plain lines to the runner. The harness checks both lines in every run, by the run's own `GITHUB_ACTIONS`. Left open, and named in section 10: a line on standard error just before the tool's end may be read after the resume line; after the verdict the tool's own such line holds only the results folder's path and the system's error, and the client library's log can hold the API server's text on one quoted line. Left open too, and named in section 8: when a run ends within a moment of its start, as a usage error does, the runner may read the stop line of standard error after the resume line, and it then reads no command for the rest of the step. What lost: escaping `::` and `##[` in every line the tool prints, because every missed place is a hole and the console's golden text changes; never resuming in the tool, because a later `::add-mask::` in the caller's own script would be ignored without a word and leave a secret in the log; the lines in `run.Run`, because the flag package's errors come before it and whether the process ends by a signal is known only in `cmd/rollproof`; a token from an environment variable, which a caller's `env:` could fix and a text from outside the tool then name; a resume line in the action's script, which would open the step again before the tool's own stop line; and, weighed by the controller of the build after the approval, for the two things left open (section 14, point 33, has both): a resume line on standard error too, which could resume before the runner has read the console's last lines, and the tool's standard error joined to its standard output on GitHub Actions, which leaves nothing open but takes the tool's errors out of a file to which a caller's script sent standard error.

## What writing this plan found in the spec

None blocks the plan; each has the smaller choice above, and the plan can go on without an answer.

1. **Section 12, "The suite's workflow":** the design read "the first fixture's test alone sets the three variables of section 8 for its run", beside "The job ends with the search for the password, in both forms, in everything the run wrote" for the mirror job, whose runs would then write no GitHub file to search. The plan reads "alone" within the suite, which the mirror job is not part of (its own tag and workflow), and gives the mirror job's runs files of their own (decision 21). Can go on. The design now reads "of the suite's runs, the first fixture's alone gets the three variables of section 8 …", and gives the mirror job's runs files of their own. Corrected in the design since (`9dcc832`).
2. **Section 10, "On a signal":** the list after "while kind creates a cluster" names the calls that wait for kind's own work. Three short calls of `Create`'s node settings (kind's `ListNodes`, a node's `Role`, `nodeutils.WriteFile`) also run engine commands without a context. The plan sends them through `untilDone` too, by the sentence before the list, "Where such a call comes before the verdict, it stops waiting for kind instead" (decision 7). Can go on. Corrected in the design since (`9dcc832`).
3. **Section 8, the outputs:** the design read "empty when the tool never started or could not open its results folder", which left out a first write of the outputs that fails although the tool started (a `GITHUB_OUTPUT` that names no file). The plan keeps it a warning, and `verdict` then stays empty (decision 3). Can go on. The design's table now reads "empty when the tool never started, could not open its results folder or could not write the outputs". Corrected in the design since (`9dcc832`).
4. **Section 8, "Secrets":** "The tool masks every such password in everything it writes" would include the outputs, which are written before the configuration loads. Their values are the results folder's absolute path and a word of the vocabulary; the plan masks neither (decision 2). Can go on. Corrected in the design since (`9dcc832`; `1a705ba` for the path, and for sections 10 and 11, which said the same of everything the tool writes).
5. **Section 8, the action:** the design does not say from which folder a relative `config` is read, nor where the binary goes, nor what the tool's image holds; decisions 9, 14 and 16. Can go on.
6. **Section 9, Podman:** "the tool asks the engine for a node image by the name that kind's Podman provider uses" covers the node image only; `pullIfMissing` also pulls the registry image. The plan renames node images only (decision 8). Can go on.
7. **Section 10, the summary:** the design read "text that came from outside the tool is escaped", which the plan meets with code blocks and code spans rather than escaped characters (decision 5); section 14, point 13, should say so to the security review. Can go on. Section 10 now reads "inside code that it cannot close, or escaped": corrected in the design since (`9dcc832`). Point 13 now speaks of the way the summary keeps such text from changing a run's page, and sends the reader to section 10 and to this plan's decision 5 (`0ae445a`).
8. **Section 10, the summary:** the line that names a kept cluster, which `cleanUp` prints after the console, is neither a warning nor a block of the console; the design is silent. The plan leaves it out of the summary (decision 4). Can go on.
9. **Section 12, "The suite's workflow":** "about 170 to 200 billed minutes" assumes the control in the Helm 3 job only. With the control in both Helm jobs (decision 20) a run is about 172 to 205 billed minutes. Can go on. The maintainer kept the control in both jobs, and the design now gives that figure (`1a705ba`).
10. **Section 12, "The suite's workflow":** the first job "runs the tests under the build tag `cluster`". The plan adds `./hack/action/`'s test under that tag to it, about a minute, so that the action's route out of the image runs on Docker before the action's own test (decision 19). Can go on.
11. **Section 16, the last list of what is not shown:** "the action's way of taking the binary out of its image, on Docker and on Podman" holds a belief this plan rests on: that Docker's `export` names the file `rollproof` with no `./` in front (decision 15). The first runs that show it are Task 10's cluster job and the action's test (Task 13). Can go on.

A last read of the corrections of the design and of this plan, on 2026-11-22, found six more places in the design, corrected there (`1a705ba`), and three in this plan, corrected in place: the `exec` at the end of `action.sh` (decision 17), a comment of `checkGitHubFiles` (Task 7) and the name of a test (Task 6).

During the build, on 2026-11-22, a reading of the runner's source found that the runner reads workflow commands in every line that a step prints, standard output and standard error alike, which the design had not considered, and that decision 17 said wrongly that the action printing none was enough. The maintainer approved a defence the same day. The design carries it (section 8, "Workflow commands"; section 14, point 33; `7286764`), decision 17 is corrected, decision 26 states it, and Task 14 builds it, after Task 12 and before Task 13.

## File structure

All paths are under `projects/rollproof/` unless they begin with `.github/`.

| Path | Change | Task |
| --- | --- | --- |
| `.github/workflows/rollproof-e2e.yml`, `.github/workflows/rollproof-action.yml` on a branch from `main` | Files that do nothing, so that `main` holds both names | 1 |
| `internal/cluster/podmanname.go` (new), `podmanname_test.go` (new), `engine.go`, `cluster.go`, `runner.go`, `engine_test.go`, `create_test.go` | `podmanName`, `Engine.nodeImageName`, the three places, the comment on `pickEngine` | 2 |
| `internal/cluster/kind.go` (new), `kind_test.go` (new), `cluster.go`, `logs.go`, `create_test.go`, `logs_test.go`, `cluster_test.go`; `cmd/rollproof/main_test.go`; `internal/run/run_test.go` | `untilDone`, `writeNodeSettings`, the seams, the tests of each call | 3 |
| `internal/report/github.go` (new), `github_test.go` (new) | `GitHub`, `GitHubFrom`, `Output`, `WriteOutputs`, `AppendSummary` | 4 |
| `internal/report/summary.go` (new), `summary_test.go` (new), `console.go`, `testdata/summary-*.golden.md` (new) | `Summary`, `SummaryLine`; the console's blocks as data | 5 |
| `internal/run/run.go`, `run_test.go`, `fakes_test.go`; `cmd/rollproof/main_test.go` | The outputs, the summary, the console before the evidence, late warnings | 6 |
| `e2e/runenv_test.go` (new, no build tag), `e2e/harness_test.go`, `config_test.go`, `fixtures_test.go`, `custom_test.go`, `commands_test.go`, `mirror_helpers_test.go`, `mirror_test.go` | GitHub's variables, the two files, the Helm major | 7 |
| `Dockerfile` (new), `action.yml` (new), `action.sh` (new), `hack/action/doc.go` (new), `hack/action/action_test.go` (new) | The tool's image, the action, its tests with fake engines | 8 |
| `hack/action/testdata/pass.yaml`, `fail.yaml`, `cut.yaml` (new), `hack/action/inputs_test.go` (new), `hack/action/action_cluster_test.go` (new, tag `cluster`), `hack/actioncheck/main.go` (new), `hack/actioncheck/main_test.go` (new) | The action's test inputs, its checker, the script on this machine's engine | 9 |
| `.github/workflows/rollproof-e2e.yml` (new), `.github/workflows/rollproof-action.yml` (new), `.github/workflows/rollproof-unit.yml` | The workflows | 10 |
| `README.md`, `e2e/doc.go` | The documentation | 11 |
| `internal/deploy/command.go`, `command_test.go`, `internal/watch/counts_test.go`, `internal/report/mask_test.go`, `internal/mask/mask.go`, `mask_test.go` | Step 5's leftovers | 12 |
| `cmd/rollproof/workflowcommands.go` (new), `cmd/rollproof/workflowcommands_test.go` (new), `cmd/rollproof/main.go`, `cmd/rollproof/main_test.go`; `internal/report/github.go`, `github_test.go`; `action.sh`, `hack/action/action_test.go`, `hack/action/action_cluster_test.go`; `e2e/runenv_test.go`, `e2e/harness_test.go`; `README.md` | The lines for GitHub's workflow commands, in the tool, the action's script and the harness | 14 |
| `docs/proofs/p32-step6-workflows.md` (new), `docs/proofs/README.md`, `README.md` | The record of the runs on GitHub | 13 |

## The tasks

One plan in ten files. Read this file first, then the file of the task at hand. Tasks are numbered through all files and are done in order, except Task 1, which goes as far as its stop and then waits for the maintainer while the others go on, and Task 14, added during the build in a file of its own, which is done after Task 12 and before Task 13.

| File | Tasks | What |
| --- | --- | --- |
| `01-stubs.md` | 1 | The two names on `main` |
| `02-cluster.md` | 2 to 3 | kind's Podman name; every call into kind before the verdict |
| `03-report.md` | 4 to 5 | GitHub's files; the job summary |
| `04-run.md` | 6 | The order of what a run writes |
| `05-harness.md` | 7 | The suite's harness |
| `06-action.md` | 8 to 9 | The tool's image and the action; its test's inputs, its checker, a run on this machine |
| `07-workflows.md` | 10 | The workflows |
| `08-docs-and-leftovers.md` | 11 to 12 | The README and `e2e/doc.go`; step 5's leftovers |
| `09-hosted-runs.md` | 13 | The runs on GitHub and their record |
| `10-workflow-commands.md` | 14 | The lines for GitHub's workflow commands (done before Task 13) |

| Task | Title | Files | Rests on | How it is verified | Hosted run |
| --- | --- | --- | --- | --- | --- |
| 1 | The two workflows' names on `main` | two stub workflows on a branch from `main` | nothing | the search for row IDs; citation check; the stubs' YAML parses | Needs the word for its push, its pull request and its merge (citations check, about 2 billed minutes) |
| 2 | kind's node image under Podman | `internal/cluster` | nothing | unit tests: kind's 13 cases, the three places, a fake `podman` | No; under Podman nothing is shown (see the task) |
| 3 | Every call into kind before the verdict | `internal/cluster`, `cmd/rollproof`, `internal/run` | nothing | unit tests of each call; one cluster test on this machine | No |
| 4 | GitHub's files | `internal/report` | nothing | unit tests | No |
| 5 | The job summary | `internal/report` | nothing | golden files, escaping tests, the console's golden files unchanged | No |
| 6 | The order of what a run writes | `internal/run`, `cmd/rollproof` | 4, 5 | unit tests; first fixture once on this machine | No |
| 7 | The suite's harness | `e2e` | 6 | unit tests; first fixture under both majors and under one; the fourteenth row under Helm 4; the mirror job, on this machine | No |
| 8 | The tool's image and the action | `Dockerfile`, `action.yml`, `action.sh`, `hack/action` | 6 | unit tests with fake engines | No |
| 9 | The action's test inputs, its checker, a run on this machine | `hack/action`, `hack/actioncheck` | 8 | unit tests; the cluster test on this machine's nerdctl | No |
| 10 | The workflows | `.github/workflows/` | 7, 8, 9 | YAML parses; the search for row IDs; citation check | No (their runs are Task 13's) |
| 11 | The README and `e2e/doc.go` | `README.md`, `e2e/doc.go` | 7 to 10 | read against the decisions; `go vet` | No |
| 12 | Step 5's leftovers | `internal/deploy`, `internal/watch`, `internal/report`, `internal/mask` | nothing | unit tests | No |
| 14 | The lines for GitHub's workflow commands | `cmd/rollproof`, `internal/report`, `action.sh`, `hack/action`, `e2e`, `README.md` | 6 to 9, 11 | unit tests; the cluster test of `hack/action` on this machine | No; Task 13's hosted run of the action's test shows the lines in its log |
| 13 | The runs on GitHub, and their record | `docs/proofs/`, `README.md` | 1 to 12, 14 | the whole suite, the mirror job and the cluster tests on this machine; then the hosted runs | Yes: one word for all of them (below) |

### The runs on GitHub, and what they cost

Times are billed minutes, each job's time rounded up to a whole minute as GitHub bills it, at $0.006 a minute for a private repository's runner of 2 CPUs (p24). The maintainer is asked twice in this plan, and once more at the step's end.

1. **Task 1's word, early.** The push of the branch `rollproof-step6-stubs`, its pull request to `main`, and the maintainer's merge. The host's `citations` workflow runs on the pull request and on the push to `main` it makes: about 1 billed minute each. While it waits, Tasks 2 to 12 and 14 go on; nothing but Task 13 needs it.
2. **Task 13's word, once Tasks 1 to 12 and 14 are done and the local checks of Task 13's first steps have passed.** It covers, in this order: the push of the branch `rollproof-step6-design`; one run each of `rollproof-action.yml` and `rollproof-mirror.yml`, started together; and, when both have passed, one run of `rollproof-e2e.yml`. If either of the first two fails, the plan stops there, reports, and does not start the suite; a fix is a new task, and its run a new word.
   - `rollproof-action.yml`: about 11 billed minutes (a build of about 2.5 minutes, three runs of about 2.8, 2.5 and 2.2 minutes, the checks), at most 30, its limit.
   - `rollproof-mirror.yml`: about 12 billed minutes (step 5's two runs of the job on this machine took 403 s and 470 s, their logs not in the repository, plus about 3 minutes for Go, `fetchhelm` and the builds), at most 75, its limit. It has never run on GitHub; this is its first run, and the first of the mirror job on Docker.
   - `rollproof-e2e.yml`: 16 billed minutes for `cluster` (p24's job, plus about a minute for `./hack/action/`), and about 77 to 93 for each Helm job (decision 19), about 172 to 205 in all; at most 305, its three limits.
   - In all: about 195 to 230 billed minutes, about $1.20 to $1.40; at most about 410 if every job ran to its limit.
   While it waits, Task 13 stays at its stop: everything built and checked on this machine, the hosted checks unverified.
3. **At the step's end, outside this plan's tasks.** The step's own pull request, when the maintainer asks for it, runs `rollproof-unit.yml` (about 3 to 5 billed minutes, with Task 10's new checks) and `citations` (about 1) on each push to it.

Not in this plan, each for its own word if the maintainer wants it: the try of the untried case of decision 1's third way (at most 1 billed minute); a run of `rollproof-proofs.yml`'s Podman job without its alias, which would show the tool's pull of kind's node image by kind's name (Task 2) under Podman (about 5 billed minutes, after a change to that job).

## Interfaces between tasks

The names that one task produces and a later one uses. Each task's Interfaces block repeats the ones it touches with their interface comments.

- `func podmanName(image string) string`, `func (e Engine) nodeImageName(image string) string` (Task 2, `internal/cluster`).
- `func untilDone(ctx context.Context, call func() error) error`; the seams `kindListNodes`, `nodeRole`, `writeNodeFile`; `func writeNodeSettings(ctx context.Context, p *kindcluster.Provider, name string, regs []config.Registry, asHosts []string, runRegistry string) ([]nodes.Node, error)` (Task 3, `internal/cluster`).
- `type GitHub struct{ /* unexported */ }`, `func GitHubFrom(getenv func(string) string) GitHub`, `type Output struct{ Name, Value string }`, `func (g GitHub) WriteOutputs(outs ...Output) error`, `func (g GitHub) AppendSummary(text string) error` (Task 4, `internal/report`).
- `func Summary(r Run) string`, `func SummaryLine(line string) string` (Task 5, `internal/report`).
- In `internal/run` (Task 6): `type gitHubFiles interface { WriteOutputs(outs ...report.Output) error; AppendSummary(text string) error }`; `var gitHubOf func(getenv func(string) string) gitHubFiles`; the warning prefixes `"the step's outputs were not written: "` and `"the job summary was not written: "`.
- **The outputs file** (Tasks 4, 6, 7, 9): appends of `NAME<<DELIMITER\nVALUE\nDELIMITER\n`, in this order in a run: `out` (the results folder's absolute path) and `verdict` (`not-verified`) in one append; then `verdict` once more (`pass`, `fail` or `not-verified`). A run whose results folder cannot be opened writes nothing.
- **The summary file** (Tasks 5, 6, 7, 9): one append of `Summary(Mask(final record))`; then, for each warning after the console, one append of `SummaryLine("warning: " + masked text)`. Its first line is `### rollproof: PASS`, `### rollproof: FAIL` or `### rollproof: NOT VERIFIED`, optionally followed by a space and a code span; the first append ends with the line `results: FOLDER` and the fence that closes its block, and the later appends follow it.
- In `e2e` (Task 7): `fixture.github bool`; `const helmVar = "ROLLPROOF_E2E_HELM"`; `func helmMajors(value string) ([]string, error)`; `var majors []string`; `func underEachHelm(t *testing.T, f fixture, check func(t *testing.T, r result))`; `type githubFiles struct{ output, summary string }`; `func runEnv(base []string, helmDir string, gh *githubFiles) []string`; `func readGitHubOutputs(path string) (map[string][]string, error)`; the files `github-output` and `github-summary` in a run's results folder.
- `action.yml` and `action.sh` (Task 8): the step's variables `ROLLPROOF_ACTION_CONFIG`, `ROLLPROOF_ACTION_IMAGE`, `ROLLPROOF_ACTION_OUT`, `ROLLPROOF_ACTION_VERSION`, `ROLLPROOF_ACTION_REPOSITORY`, `ROLLPROOF_ACTION_DIGEST`; the script's functions `pick_engine`, `image_ref`, `take_binary`, `main`; its texts of failure (Task 8's table).
- `hack/action/testdata/pass.yaml`, `fail.yaml`, `cut.yaml` (Task 9): the variable `ROLLPROOF_ACTION_TEST_RELEASE`, which only `pass.yaml` reads and which must be `orders`.
- `hack/actioncheck` (Task 9): `go run ./hack/actioncheck -verdict V -out PATH -want-verdict WORD -want-out PATH [-want-decided CODE] -step-files DIR`; exit 0 when every check holds, 1 with one line on standard error for each fault, 2 for a flag it cannot read. `func run(args []string, stdout, stderr io.Writer) int` is the command, and `main` is `os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))`.
- `func GitHubActions(getenv func(string) string) bool` (Task 14, `internal/report`), which `GitHubFrom` asks.
- In `cmd/rollproof` (Task 14): `var newStopToken = func() string { return rand.Text() }`; `func withWorkflowCommandsStopped(stdout, stderr io.Writer, getenv func(string) string, signalled func() bool, command func() int) int`; `func signalCame() bool`; in its tests, `func startChild(t *testing.T, fake string, env ...string) *helperChild`, which no longer passes this process's `GITHUB_ACTIONS` to the child.
- `action.sh` (Task 14): the variable `token_file` and the function `stop_commands`; the text of failure `rollproof: no token for the line that stops the runner's workflow commands: PATH gave none`.
- In `e2e` (Task 14): `var stopLine *regexp.Regexp`; `func envValue(env []string, name string) string`; `func workflowCommandFaults(stdout, stderr string, onGitHub, bySignal bool) []string`.
- **The lines for workflow commands** (Tasks 14 and 13): the tool, on GitHub Actions, `::stop-commands::TOKEN` first on standard output and first on standard error, TOKEN 26 characters of `A` to `Z` and `2` to `7`, and `::TOKEN::` last on standard output when it ends by itself and no signal came; the action's script, `::stop-commands::UUID` first on both streams right after its check for Linux, UUID in the kernel's form, and never a resume line.

## Alternatives considered

The design's own block for `action.yml`, from section 8, comes first, copied unchanged as the design asks; the alternatives after it are this plan's.

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

### How a run writes to GitHub's files

Common case: `run` has a results folder and a final record, and on GitHub it must append outputs and a summary to the two files the runner named, at fixed moments.

**A. A small value in `report` with two operations; `run` decides the moments**

- `report.GitHubFrom(getenv) GitHub`, `(GitHub).WriteOutputs(outs ...Output) error`, `(GitHub).AppendSummary(text string) error`

```go
s.gh = gitHubOf(o.Getenv)
s.writeOutputs(report.Output{Name: "out", Value: w.Dir()}, report.Output{Name: "verdict", Value: "not-verified"})
s.appendSummary(report.Summary(report.Mask(rec, s.mask)))
```

**B. The results writer does it all** (expected to lose)

- `report.Open(out, getenv)` writes the first outputs itself; `(*Writer).Final(r)` writes the second `verdict` and the summary.

```go
w, err := report.Open(o.Out, o.Getenv) // writes out and verdict
err = s.w.Final(rec)                    // writes result.json, verdict and the summary
```

| | A. Two operations | B. Inside the writer |
| --- | --- | --- |
| Ease of use for the common caller | Two calls at the moments section 8 names | No call at all |
| Interface size | A constructor, a type, two methods | None new |
| Generality | Late warnings appended by `run`; a refused final write still writes `verdict` | `Final` runs again for every warning, and a refused `Final` must still write `verdict` |
| Information hidden | The delimiter form and the files stay in `report` | Same, but `Final` learns the summary and the outputs |
| Implementation simplicity | Plain | `Final` grows three jobs, and every rewrite for a warning would append the summary again |
| Efficiency | Equal | Equal |

Third design: not needed.

Pick: A.

Losers: B, because `Final` is called again for each late warning, and the summary must be appended once.

### The job summary's form

Common case: the console's blocks become a page on GitHub that nothing from a chart, a command or the API server can change.

**A. Code blocks and code spans for free text, tables and lists for the tool's own structure**

**B. Every line as a Markdown paragraph, each special character escaped** (expected to lose)

**C. The whole console in one code block**

| | A. Code where text is free | B. Escaped paragraphs | C. One code block |
| --- | --- | --- | --- |
| Ease of use for the common caller | Tables and lists render; set-up stays aligned | Alignment lost | Reads as the console |
| Interface size | `Summary`, `SummaryLine` | Same | Same |
| Generality | Every block of section 10 | Every block | No tables, no lists: against section 10 |
| Information hidden | Two rules: fence length, span delimiter | A rule for each character, and GitHub's autolinks after escapes | One rule |
| Implementation simplicity | Small | Many rules, each a way to fail | Smallest |
| Efficiency | Equal | Equal | Equal |

Third design: C is the third.

Pick: A.

Losers: B, because one missed rule changes the page, and GitHub's autolinks act after escapes; C, because section 10 asks for tables and lists.

### Stopping the wait for kind

Common case: a signal comes while kind creates a cluster, deletes one or takes its logs, and the run must write its final results within 7.5 s.

**A. One function, `untilDone`, at each call into kind**

```go
err := untilDone(ctx, func() error { return kindCreate(p, name, options...) })
```

**B. One goroutine around the whole of `Create`** (expected to lose)

```go
c, err := createUntilDone(ctx, s, e)
```

| | A. At each call | B. Around `Create` |
| --- | --- | --- |
| Ease of use for the common caller | One line at each of nine places | One line |
| Interface size | One function, three seams | One function |
| Generality | Also the calls of `failedClientError` and `storeFailed`, after `Create` returned | `Create` only |
| Information hidden | Each step keeps its own error text | A cut `Create` leaves its `fail` running in the background, logs and removal included |
| Implementation simplicity | Small, tested call by call | Small, but the background `fail` may delete what a quick next run makes |
| Efficiency | Equal | Equal |

Third design: not needed.

Pick: A.

Losers: B, because `Create`'s own clean-up would go on behind the run, and the calls after `Create` returned would still wait.

### Where the action's script lives

Common case: GitHub runs the action's step, and a test on this machine runs the same script with fake engines.

**A. `action.sh` beside `action.yml`, run by `exec bash`**

**B. The script inline in `action.yml`** (expected to lose)

| | A. A file | B. Inline |
| --- | --- | --- |
| Ease of use for the common caller | Same | Same |
| Interface size | Two files | One file |
| Generality | Tests source it and call each function | A test copies the YAML's text out |
| Information hidden | `action.yml` holds the inputs, outputs and release values; the script the logic | Mixed |
| Implementation simplicity | Plain bash file | Bash inside YAML, with its quoting |
| Efficiency | One `exec` more | Equal |

Third design: not needed.

Pick: A.

Losers: B, because the script is the action's whole logic and must be tested where it is.

### Where the tool prints the lines for workflow commands

Common case: a step on GitHub runs `rollproof run`, which may print a usage error, a rejected configuration's console, or a whole run's console and warnings, and then end by itself or by a signal.

**A. A wrapper around the `run` case of `realMain`**

```go
return withWorkflowCommandsStopped(stdout, stderr, getenv, signalCame, func() int {
	return runCommand(args[1:], stdout, stderr, getenv)
})
```

**B. Inside `run.Run`** (expected to lose)

```go
s.stopCommands() // first, before report.Open
defer s.resumeCommands()
```

| | A. Around `run` in `cmd/rollproof` | B. In `run.Run` |
| --- | --- | --- |
| Ease of use for the common caller | Same | Same |
| Interface size | One function, one seam for the token, one for the signal | Two methods, a token seam in the run's fakes |
| Generality | Usage errors and every ending of a run inside the lines | The flag package's errors come before the stop line |
| Information hidden | `run` knows nothing of GitHub's runner beyond its two files | `run` learns of the runner's log |
| Implementation simplicity | Small; the signal's end is read where it is kept | Must guess whether the process will end by a signal before `release` has returned |
| Efficiency | Equal | Equal |

Third design: not needed.

Pick: A.

Losers: B, because a usage error would stand before the stop line, and the signal's end is known for certain only after `run.Run` has returned.

Design delta: better, because the run's results now reach GitHub at the moments section 8 names and survive a cancel in the order section 10 asks, the summary has one escaping rule that a reviewer can read in one place, every wait for kind before the verdict ends at a signal through one function, and the action's logic is a script that tests run as it is.
