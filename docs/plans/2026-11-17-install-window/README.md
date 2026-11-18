# rollproof: the install window — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring the walking skeleton to the design's revision of 2026-11-17: the install settles under the running probe in an install window whose failed checks are counted and not judged, the steady window begins when the install has settled, and the built-in probe has no warm-up.

**Architecture:** `watch` learns when the install has settled, from the times of the changes it is told and the moment of the probe's first observation that `run` hands it. `verdict` takes the install window as one more window in its list, so every rule it has applies to it, and `report` keeps it apart from the windows. `run` reads the probe's events from before the probe starts, so that none waits for a reader.

**Tech Stack:** As built: Go 1.27, `k8s.io/client-go` v0.37, `sigs.k8s.io/kind` v0.33.0 as a library, `github.com/google/go-cmp` in tests, Helm 3.22.1 and 4.3.2 as binaries for the fixtures. No new dependency.

**Spec:** `projects/rollproof/docs/specs/2026-11-13-rollproof-design.md`. Section numbers in this plan are that document's. The revision this plan builds is the difference between two commits of it: from the repository root, `git diff dd44bab 660cecc -- projects/rollproof/docs/specs/2026-11-13-rollproof-design.md`. An executor reads that difference once, and the sections a task names before it starts the task. The plan that built the skeleton is `docs/plans/2026-11-16-first-plan/`.

## Global Constraints

Every task's requirements include these.

- **Linux only.** Module `rollproof`, every package under `internal/` except `cmd/rollproof`. Apache-2.0. No file under `projects/rollproof/` cites a row of the host repository's decision log or follows its service rules.
- **Paths and commands.** Commands run from `projects/rollproof/` unless a step says "from the repository root". Every command behaves the same in bash and fish: no unquoted glob, no `VAR=value` prefix (write `env VAR=value command`), no `$(…)`.
- **The binary is static.** `env CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=dev" -o bin/rollproof ./cmd/rollproof`.
- **The revision adds no configuration key, no reason code and no schema version** (section 14, point 23). `result.json` stays at `schema` 1.
- **Reason codes,** in the order of section 10's list, unchanged: `probe-failures`, `transition-stalled`, `pod-unschedulable`, `command-failed` (fail); `config-rejected`, `runner-unsupported`, `cluster-failed`, `chart-fetch-failed`, `setup-failed`, `install-failed`, `nothing-watched`, `workload-unsupported`, `target-unsupported`, `probe-not-started`, `probe-exited`, `probe-line-invalid`, `steady-not-clean`, `probe-silent`, `no-pod-replaced`, `restart-failed`, `interrupted`, `internal-error` (not verified).
- **The windows** (section 4), with no gap between them: the install window, from the moment the probe's first observation was written (the runtime's timestamp, never `t`) until the install has settled; the steady window, from there for `limits.steady`; then one window for each transition.
- **Settled** (section 7): the install's conditions have held without a break for `limits.settle`, counted from the later of the first observation and the moment the conditions last came to hold. A break is a moment at which no workload is watched, a watched workload's rollout is not complete, or a deleted pod is not gone.
- **The install window's rule** (section 10, rule 4): a failed check in it is counted and decides nothing. Silence, a fault of the probe, an install that failed, a watched workload of a form the tool cannot judge, a signal or a failure of the tool makes it not verified.
- **Where it is reported** (section 10): never in `result.json`'s `windows`, whose names stay `steady`, `deploy`, `rollback` and `restart`, and never in the console's first table. A reason's `window` is `install` for it.
- **No probe warms up.** Whether a check passed or failed never moves the start of the steady window (section 6).
- **Defaults** (section 8): `limits.steady` 10s, `limits.settle` 15s, `limits.timeout` 5m, `limits.silence` 5s, `probe.http.rate` 200, `probe.http.timeout` 2s. The drain of the built-in probe is its request timeout plus 5 s.
- **Tests.** Unit tests need no cluster and no network and run with `go test ./...`. A test that needs a container engine carries the build tag `cluster`; a fixture run carries the tag `e2e`. Run those with `-p 1`. On a machine whose engine is nerdctl, put `env KIND_EXPERIMENTAL_PROVIDER=nerdctl` before the command.
- **Commits.** One commit per task. A message is a plain sentence that says what changed. It carries no attribution to an AI tool: no `Co-Authored-By` trailer that names one, no "Generated with" line, no session link.
- **Nothing leaves the machine.** No push, no pull request and no run on GitHub Actions without the maintainer's word.

## Review Focus

The revision implies these and names no test for them. Each has its test in the task that owns the code.

1. **A probe writes thousands of lines before the steady window begins.** Every line is read as it comes and counted where it belongs; none waits for a reader, and the probe's log is never held back. Tests in Task 3 (`TestEventsAreReadFromTheStart`, `TestTheReaderStartsWithTheProbe`) and Task 6 (the count in the install record).
2. **The tool looks at the cluster a moment after the install settled, and a change arrived in between.** The steady window begins at the moment the settle time had passed, not at the moment the tool looked, and that change does not start the time again. Tests in Task 1 (the row "a change after the install had settled") and Task 4 (`TestTheSteadyWindowBeginsWhenTheInstallHasSettled`).
3. **The cluster's DNS hangs instead of failing.** The built-in probe keeps its rate, and every request ends within its timeout with a line, so the probe is not silent. Test in Task 5 (`TestRunHTTPLookupsThatHang`).
4. **The run ends in the install window.** The probe is stopped, the cluster is deleted, `result.json` is final, and the exit code is 2. Test in Task 4 (`TestTheInstallWindowNotVerified`).
5. **A failure class or a sentence from the cluster, printed in the install's lines, holds a control character.** It cannot redraw or split a line of the console. Test in Task 7 (`TestConsoleCleansEveryField`).

## What this plan builds, and what it leaves

| Unit | In this plan | Left |
| --- | --- | --- |
| `watch` | When the install has settled: breaks, the limit, a workload that does not roll, a watch that missed changes | Nothing of the revision |
| `verdict` | The install window in rules 1 to 4, 8 and 9; its counts; the time of the last failure; the line for a steady window in which no check ever passed | Nothing of the revision |
| `probe` | Events from the probe's first line; a failed start that stops the probe; the built-in probe with no warm-up and a lookup inside the request | The custom probe (step 4) |
| `run` | The install window, the steady window's start, the reader from the probe's start | The restart-only sequence, `setup` (step 4) |
| `report` | The install record in `result.json`, the install's lines and the endings of the console text | The job summary (step 6) |
| `cluster` | Each node keeps an attempt to connect that had no answer for 5 s (section 9, Task 9) | A node of Docker or Podman, not tried (section 14, point 24) |
| Tests with a cluster | The first four fixtures again, with the install record asserted for the first, and one new row | The other rows (step 4) |

The fixture rows of section 12 that the revision added or changed:

| Row | Where it is built | Why |
| --- | --- | --- |
| The same custom probe reports a failed check about 500 times a second for its first 5 s | Build-order step 4 | The skeleton has no custom probe. `probe.custom` is on the list of what this build cannot run |
| The same, with failed checks for its first 20 s | Build-order step 4 | The same |
| The previous release's pods report ready, and its handler fails every request | This plan, Task 8 | It needs the built-in probe, the Helm driver and one switch in the fixture service, all of which exist. It is the one run in a cluster that shows what the revision gives: `steady-not-clean` within a minute, where the warm-up gave `probe-not-started` after `limits.timeout` (ruling 1) |

The two rows with a custom probe are there, among other things, to show a reader of the probe's lines that starts late (section 12). Until step 4 builds them, Task 3's unit tests show it, and so does the row built here: every check of its install window fails and is a line of its own, about 3,000 of them, which is more than the probe's channel holds.

## Rulings

Each fills a silence of the design, settles two of its sentences that disagree (9, 10 and 11), or picks the words for one of its sentences (13). Each is the smaller choice, and where a ruling reads a sentence of the design, it says which. The maintainer reviews them with the plan.

1. **This plan builds one of the three new fixture rows.** Section 13 gives the skeleton the first four fixtures and step 4 "the remaining fixtures". The row with a previous release that fails every request is built here all the same. Why: it uses only what is built, and without it nothing in a cluster exercises the revision beyond "the four fixtures still pass". Cost if wrong: one switch in the fixture service and one test are built a plan early.
2. **The install record's names in `result.json`.** The key `install` follows `windows`. It holds `ready`, `first_observation`, `settled`, `settle_restarted_by`, `checks`, `failed`, `failed_by_class`, `last_failure`, `slowest_ms` and `lag_ms`. It is `null` for a run that ended before the install was ready. A time that never came is `null`, and "no counts" is written as zeros beside a `null` `first_observation`. Why: the file's own habit, in which every key is always there and only a time or the cluster is `null`. Cost if wrong: keys are renamed before the first release; no release carries the schema yet.
3. **The probe is in the record from the moment the tool starts it.** `result.json`'s `probes` has its entry, and the console prints the images' lines and the probe's, once the tool has started the probe, also when no observation ever came. Why: section 10 asks for the probe's lines in a run that ended before the first observation, and lists in `probes` the probes the tool started; the console is a pure function of the record. Cost if wrong: a probe that judged nothing is listed; its `lines` and the reason say so.
4. **Between "the install is ready" and the first observation, nobody asks the watch.** The tool checks the target and starts the probe in that time. The Tracker still notes every break by its time, so the settle time is counted as section 7 says. An install that failed in that time (its conditions did not hold when the limit passed, or broke after it), and a workload that does not roll and was first seen in that time, are learned only when the wait for the settling begins. They have no window (rule 9). After either, the install window has no length. Cost if wrong: such a run ends when the probe has written its first observation, not at once.
5. **A workload the tool cannot judge, first seen before the first observation, ends the run when the wait for the settling begins.** Section 7 says that one "found before the probe's first observation" ends the run. By ruling 4 the tool learns of it only when it asks, at the first observation, so the settling ends there, the install window has no length, and the reason has no window (rule 9). One first seen in the install window makes that window not verified, and the window runs to its end (rule 2). Cost if wrong: none for the verdict; such a run ends a moment after the first observation instead of at once.
6. **What counts as "the settle time started again", and the sentence for it.** A break counts when it was not over at the first observation, or began after it. A break that was over by then moved nothing and is not reported. The sentences count from the moment the install was ready: "no workload was watched 8.0 s after the install was ready", "deployment/orders was not complete 8.0 s after the install was ready: " and kubectl's message, "pod orders-a-1 of deployment/orders was deleted and not gone 8.0 s after the install was ready". Cost if wrong: wording.
7. **The two texts of `install-failed` after the install was ready:** "the install was not ready when limits.timeout passed" and "the install stopped being ready after limits.timeout, while it still settled", each followed by the sentence of the break that failed it. They follow the reason's line in section 10's table. Cost if wrong: wording.
8. **A watch of the cluster that missed changes in the install window makes that window not verified,** as `internal-error`, the way it does in a transition's window. Why: "held without a break" is a claim about every moment, and the tool cannot make it for a stretch it did not see. The design never speaks of a watch that breaks; the first plan's reviews added the rule for transitions. Cost if wrong: a run in which the API server made the tool list again while the install settled is not verified although nothing was wrong.
9. **The built-in probe's lookup.** A lookup that fails before the request's timeout is a failed check of class `other`, and so is one that gives no address. A request whose timeout passes while its lookup still runs is class `timeout`, like any request with no answer in time. Requests that start before the first lookup has succeeded each look the name up; the first address that any of them gets is kept. Why: section 6 says both "a request whose lookup fails is a failure of class `other`" and "the lookup is part of the request and of its timeout". Cost if wrong: one class name in the counts of a run whose DNS hangs.
10. **`crossed_start` is counted for a transition's window only.** A failed check that began in the install window and was written in the steady window is counted in the install window and adds nothing to the steady window's `crossed_start`. Why: section 6 reports crossings of "the start of a transition, because the transition may be what cut them". Cost if wrong: one number in `result.json` that no text explains.
11. **"No check has passed since the probe started"** is true when no line with `ok: true` has a time before the end of the steady window, the lines before the install window included. The line under the count reads "no check has passed since the probe started: every check failed for 25.0 s", with the time from the first observation to the end of the steady window. `verdict` writes it, as the second line of the reason's text. Cost if wrong: wording, or a line that is missing when a single early check passed.
12. **Console text that section 10's examples do not show.** The install's lines stand directly under the probe's, before "settings that differ from their defaults". An install that had not settled reads "had not settled under the probe when the run ended", and its second line ends "the last at 09:59:37.10". A window with no failure reads "0 failed" and stops there. A slowest check or a lag that does not exist reads "-". Each restart is a line "the settle time started again: " and its sentence. Cost if wrong: wording, and four golden files are made again.
13. **The line for a deploy with no rollback** becomes "the candidate's shutdown and the installed release's start-up, because the run has no rollback" (section 10, "What every report states"). No run of the skeleton reaches it; the code and its tests exist. Cost if wrong: wording.
14. **`windows.json` in the evidence gets a mark for the install window,** first in the list, named `install`. Section 10 says that the folder's layout is not a contract. Cost if wrong: step 4's harness reads the marks by name and is not affected.
15. **A start that failed stops the probe before `Start` returns,** unless a signal ended it: section 6 has the tool stop the probe "when the run ends before the probe's first observation", and section 10 has a signal leave the probe's pod for the next run. The pod of a probe that had exited is deleted too, as it is after a window. The run then asks `Stop` for what that stop left, so the record has the probe's line counts. Cost if wrong: with `--keep-cluster`, the pod of a probe that never started is gone when the developer looks; its state is in the reason's text.

16. **Task 9 was added on 2026-11-18, after Task 8's Step 7 stopped.** That step met timeouts long after the probe's first seconds, and section 14, point 24, records their cause and the maintainer's choice: each node forgets an attempt to connect that had no answer within 5 s. Task 8's commit stays as it is. Task 8 is complete when its Steps 6 and 7 pass again, which Task 9's last step runs. Why: the cause lies in the cluster, which no task of this plan touched, and the change to the fixtures and the README that Task 8 made does not depend on it. Cost if wrong: the README's sentence about the fixtures is ahead of the code for the commits between Task 8 and Task 9, on a branch that nobody else has.

## Open points this plan names and does not settle

Section 14, point 22. Both rules stand as built: a signal that comes while the tool is stopping the probe makes the run not verified unless a window had already failed, also when the last window was the install window; and a probe that exits before its first observation is `probe-exited` at once.

Section 14, point 23, in its order:

- **Nothing has been run with the revision.** Task 8 runs five fixtures under both Helm majors on the developer's machine. That is the first run.
- **Every run is at least `limits.settle` longer.** Built as written. Task 8 logs, for the first fixture, how long before the steady window the last failed check came. That figure is for point 7; this plan changes no default.
- **Whether the probe's own pod meets a failure of its own that outlasts the settle time.** The first fixture in Task 8 shows it. If its steady window is not clean, the executor stops and reports; the plan tunes nothing. Its first runs stopped on failures that came from the nodes, not from the probe's pod (section 14, point 24); Task 9 runs it again.
- **The install's settle time starts again when a pod stops being ready, and a transition's does not.** Built as section 7 says. The maintainer has not ruled on the difference.
- **The install settles for `limits.settle` and has no key of its own; a failed check in the install window never changes a verdict.** Built as written.
- **An installed release that fails only in its first seconds is not seen at the install.** Built as written; ruling 13 is the report's one line about it.
- **A run that passes may print failed checks.** Built as written.
- **The steady window's start in `result.json` is no longer the first observation.** The schema version stays 1.
- **The report does not say how many ready endpoints the target had.** Not built.

## File structure

All paths are under `projects/rollproof/`.

| Path | Change | Task |
| --- | --- | --- |
| `internal/watch/settle.go` (new) | The install's breaks, when it has settled or failed, `Settling` | 1 |
| `internal/watch/counts.go`, `tracker.go`, `watcher.go` | `Install.Ready`; the Tracker is told of breaks; `InstallSettled` | 1 |
| `internal/verdict/vocab.go`, `decide.go`, `observe.go` | The window name `install`; its judging and counts; the last failure; the line of ruling 11 | 2 |
| `internal/probe/pod.go` | `Events` before `Start`; a failed start stops the probe | 3 |
| `internal/run/run.go` | The reader starts with the probe (3); the install window and the steady window's start (4); the facts for the record (6) | 3, 4, 6 |
| `internal/probe/http.go`, `httpargs.go` | No warm-up; the schedule starts with the probe; the lookup inside the request | 5 |
| `internal/report/record.go`, `result.go` | `Install`, `Run.Install`, their JSON | 6 |
| `internal/run/record.go` | The install window out of `windows` (4); the install record and the probe's entry (6) | 4, 6 |
| `internal/report/console.go` | The install's lines, the endings, the line of ruling 13 | 7 |
| `internal/report/testdata/` | Two examples copied again from the design, three new golden files, four made again | 6, 7 |
| `fixtures/service/main.go`, `fixtures/chart/` | The switch `FAIL_WORK` and the value `failWork` | 8 |
| `e2e/harness_test.go`, `e2e/fixtures_test.go`, `README.md` | The install record read and asserted; the new row; one sentence of the README | 8 |
| `internal/cluster/cluster.go` | Each node keeps an attempt to connect that had no answer for 5 s | 9 |

## The tasks

Read this file first, then the file of the task at hand. Tasks are done in order. The order is section 13's: the rule for when the install has settled, then the install window together with the built-in probe's changes, then the counts and the report. Task 5 comes after Task 4, so that no commit holds a probe without a warm-up and a run without an install window.

| File | Tasks | What |
| --- | --- | --- |
| `01-settle-window-and-probe.md` | 1 to 5 | `watch`, `verdict`, the probe's pod, `run`, the built-in probe |
| `02-record-report-and-fixtures.md` | 6 to 8 | The install record, the console text, the fixtures |
| `03-node-settings.md` | 9 | The nodes' setting for an attempt to connect that had no answer, and Task 8's cluster runs again (ruling 16) |

---

## Alternatives considered

### `internal/watch`

Common case: `run` has the moment of the probe's first observation and wants to know when the install has settled under the probe, or that it failed.

**A. One more wait, beside `InstallReady` and `Transition`**

- `(*Watcher).InstallSettled(ctx context.Context, first time.Time) Settling`
- `type Settling struct { First, Settled, Ended time.Time; Restarts []string; Before, InWindow []verdict.Condition }`

```go
set := watcher.InstallSettled(ctx, started.First)
install.End, steady.Start = set.Ended, set.Settled
```

**B. One call for the whole install, told the first observation through a channel**

- `(*Watcher).Install(ctx context.Context, began time.Time, exited <-chan struct{}, ready chan<- Install, first <-chan time.Time) (Settling, error)`

```go
ready, first := make(chan watch.Install, 1), make(chan time.Time, 1)
go func() { set, err = watcher.Install(ctx, began, exited, ready, first); close(done) }()
inst := <-ready
// the target check and the probe's start come here
first <- started.First
<-done
```

**C. The install window as a transition** (expected to lose)

- `(*Watcher).Transition(ctx, verdict.Install, first, exited)`, with a channel that is already closed

```go
tr := watcher.Transition(ctx, verdict.Install, started.First, closed)
install.End, steady.Start = tr.Ended, tr.Ended
```

Third design: not needed; option A leaves the caller one call.

| | A. One more wait | B. One call, two channels | C. A transition |
| --- | --- | --- | --- |
| Ease of use for the common caller | One call when it has the first observation | A goroutine, two channels and their order | One call it already knows, and results it must not read |
| Interface size | One operation, one result type | One operation with five parameters | None new |
| Generality | A custom probe's first observation is the same argument | The same | Only while the install's rule equals a transition's, which it does not |
| Information hidden | The breaks, the limit, where a condition is placed | The same, but the caller holds the wait's life | The differences leak as flags: readiness, the limit's start, no pod count |
| Implementation simplicity | A walk over noted breaks, and a wait like `Transition`'s | The same walk, and a state machine across two waits | Least new code, most special cases |
| Efficiency | No difference | No difference | No difference |

Pick: A.

Losers: B, because `run` does two steps of its own between "ready" and the first observation, and a call that spans them makes the caller run it beside itself and feed it in the right order.

Losers: C, because the install's rule is not a transition's: a change of readiness breaks it, its limit is counted from the install's start and not from the window's, and it replaces no pod, so `Transition` would carry a flag for each difference.

Canary kept: 3: the install's readiness, its settling and a transition are three waits in the common case; `run` acts between each two of them, and B, the design that joins the first two, lost for that reason.

### `internal/verdict`

Common case: at the end of each window, the install window's included, `run` asks what the verdict is so far and whether the run goes on.

**A. The install window is one more `Window` in `Timeline.Windows`**

- `const Install WindowName = "install"`
- `Decide(t Timeline) Decision`, unchanged

```go
tl.Windows = []verdict.Window{{Name: verdict.Install, Start: first}, {Name: verdict.Steady}, {Name: verdict.Deploy}}
d := verdict.Decide(tl)
if !d.GoOn { return }
```

**B. The install window in fields of its own**

- `Timeline.Install Window`, `Decision.Install InstallResult`

```go
tl.Install = verdict.Window{Start: first, End: settled}
d := verdict.Decide(tl)
if d.Install.NotVerified || !d.GoOn { return }
```

**C. `run` takes the install window out before it asks** (expected to lose)

- `Decide(t Timeline) Decision`, unchanged; `run` drops the failed checks from before the steady window and puts every other condition of that time into `Before`

```go
tl.Observations = fromSteadyOn(observations)
tl.Before = append(tl.Before, installConditions...)
d := verdict.Decide(tl)
```

Third design: not needed; option A leaves the caller the call it has.

| | A. One more window | B. Fields of its own | C. `run` takes it out |
| --- | --- | --- | --- |
| Ease of use for the common caller | One more element in a list it builds anyway | A second place to fill and a second result to read | A filter and a rule to write |
| Interface size | One constant | Two fields and a type | None |
| Generality | Every rule that walks the windows applies: placing by time, silence, a late condition, not judged | Each such rule gets a second branch | Rules 3, 4 and 8 for that window are the caller's |
| Information hidden | That a failed check there decides nothing | The same | Nothing: `run` holds the rule |
| Implementation simplicity | Two conditions on the window's name | A second pass through every step | Least code in `verdict`, a copy of its rules in `run` |
| Efficiency | No difference | No difference | No difference |

Pick: A.

Losers: B, because the install window differs from the steady window in one thing, that a failed check gives no condition, and a second set of fields would repeat everything else.

Losers: C, because `run` would hold a copy of the rules, which section 5 forbids, the counts would be lost, and a reason could not carry `window: install`.

### `internal/probe`

Common case: `run` starts the probe once, reads everything it reports from its first line on, and stops it once.

**A. The events are there before the start**

- `Probe.Events() <-chan Event`
- `Probe.Start(ctx context.Context) (Started, error)`, which still returns at the first observation
- `Probe.Stop(ctx context.Context) (Facts, error)`

```go
p := cluster.Probe(cfg, image, raw)
go read(p.Events())
started, err := p.Start(ctx)
```

**B. The caller hands in a sink**

- `Probe.Start(ctx context.Context, sink func(Event)) (Started, error)`
- `Probe.Stop(ctx context.Context) (Facts, error)`

```go
started, err := p.Start(ctx, timeline.add) // called from the probe's own goroutines
```

**C. A larger buffer** (expected to lose)

- `Started.Events`, as built, with room for a million events

```go
started, err := p.Start(ctx)
// … later, when the windows are fixed
go read(started.Events)
```

Third design: not needed; option A costs the caller one line before `Start`.

| | A. Events before the start | B. A sink | C. A larger buffer |
| --- | --- | --- | --- |
| Ease of use for the common caller | One line before `Start` | One argument | Nothing to do |
| Interface size | Three operations | Two operations and a function type | Two operations and a field |
| Generality | A fault before the first observation reaches the caller too | The same | Bounded by a number |
| Information hidden | The pod, the log stream, the line format | The same, but the probe's goroutines run the caller's code | The same, and a limit nobody sees |
| Implementation simplicity | The channel is made one call earlier | The reader in `run`, which answers a request and hands over a panic, is built again inside `probe` or lost | One number |
| Efficiency | One channel send for each event | One call for each event | The same as A, and memory for the buffer |

Pick: A.

Losers: B, because a sink that blocks stops the probe's log with nothing to bound it, and the run's reader, with the tests that pin how it ends and how its panic is handed over, would be rebuilt for no gain.

Losers: C, because a send still waits when the buffer is full, and no test can show that a reader exists.

Canary kept: 3: `Events`, `Start` and `Stop` are three operations in the common case; the reader has to exist before the first line and `Start` has to report the first observation, and B, the design with two operations, lost.

### `internal/report`

Common case: `run` fills one record, and a wrapper reads `result.json` and adds up the windows' failures.

**A. The install has a record of its own**

- `Run.Install *Install`, beside `Run.Windows`

```go
for _, w := range result.Windows { failed += w.Failed } // a wrapper: the install's failures are not in it
```

**B. A window named `install` in the list**

- `Run.Windows[0]`, with a result of its own

```go
for _, w := range result.Windows { if w.Name != "install" { failed += w.Failed } }
```

**C. Console text only** (expected to lose)

- No field; `Console` prints the install's lines from counts that `result.json` does not hold

```go
fmt.Print(report.Console(rec)) // a wrapper has no number to read
```

Third design: not needed; option A leaves the wrapper's loop as it is.

| | A. A record of its own | B. A window in the list | C. Console only |
| --- | --- | --- | --- |
| Ease of use for the common caller | The loop over the windows stays right | Every wrapper must know one name to skip | Nothing to read |
| Interface size | One field, one type | One more value of a name and of a result | None |
| Generality | The install's own times (ready, first observation, settled) have a home | They would need fields that no other window has | None |
| Information hidden | Nothing a wrapper needs | A window whose failures do not count, which the list cannot say | The counts |
| Implementation simplicity | One type and its encoding | A row with special cases in every table | Least |
| Efficiency | No difference | No difference | No difference |

Pick: A.

Losers: B, because it is the trap that the maintainer's first ruling of section 14, point 23 names: a window whose failures do not count, in a list that wrappers add up.

Losers: C, because section 10 makes `result.json` the contract for wrappers and gives the install record its fields.

`internal/run` has no choice of its own to make: its `Watcher` interface gains the operation picked for `internal/watch`, the `Probe` it holds is the one picked for `internal/probe`, and `Run` keeps its signature. `cmd/rollproof`, `fixtures/` and `e2e/` hold no interface that other code calls.

Design delta: better, because the built-in probe loses the one thing it knew about when judging starts, the install's wait gets a settle rule in the unit that already holds the transitions' rule, and `verdict` takes the new window through the entry point it has.
