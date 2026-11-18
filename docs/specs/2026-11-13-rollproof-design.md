# rollproof: design

Date: 2026-11-13. Revised on 2026-11-16. Amended and revised on 2026-11-17. Amended on 2026-11-18.

Status: the design below was agreed section by section in conversation on 2026-11-13. Three
reviews followed. The first two:

- A review on 2026-11-14 proposed changes that make the public surface smaller and close gaps in
  the contracts. The maintainer accepted them on 2026-11-16.
- A review on 2026-11-16 asked how general the tool is and how far a pass can be trusted. Two
  further agents then looked in documentation and source code for evidence for and against each
  answer to it, and three answers changed. The maintainer accepted the result on 2026-11-16.

Two readings of source code on 2026-11-16 then checked what the text said of Helm and of eight
further points. The maintainer accepted the four changes to the Helm driver that followed, and
confirmed that a probe on the runner stays out of the first release (section 14, points 17
and 18).

A third review on 2026-11-16 went through the text part by part, to find what is wrong with it
before anything is built from it. It found two rules that gave a wrong verdict, and many places
where an implementer would have had to guess. The drafting session checked the most serious
findings against the text and the sources and proposed an answer to each finding. The maintainer
accepted all of them that day, the one step of point 17 that had been the drafting session's own
among them.

A check of the revised text against itself followed, also on 2026-11-16: a script for what can be
checked mechanically, and three readers who each followed one thread through the whole document.
They found 48 places. This text carries the corrections. Most follow from what the rest of the
text already said. Where the text gave no lead, the drafting session filled the gap with the
smaller choice and listed those choices for the maintainer, who raised no objection. The
maintainer answered the three questions that change a verdict or a reason code, two of them
tentatively (section 14, point 22).

This text includes all of it. This written form waited for the maintainer's review, and the
maintainer approved it on 2026-11-16. No code existed then.

One amendment followed on 2026-11-17, while the first plan was built: the built-in probe warmed
up before its first observation. The plan's first end-to-end runs showed why, and the maintainer
accepted it for now. A revision on the same day, after the first plan was built, took the warm-up
out again. The install now settles under the running probe, and the steady window begins when it
has settled (sections 4, 6, 7 and 10). The maintainer chose that design from six sketches.
Two reviews of the revision followed the same day, and the maintainer ruled on what they found.
Section 14, point 23 records the steps, the rulings and what the revision leaves open. The
maintainer approved the revision. A second plan brought the code to it on 2026-11-18, with one
more change to the cluster that its runs called for (section 13; section 14, point 24).
The maintainer ruled the same day on what its reviews and runs left (section 14, points 25 to
27). The second plan carried point 25, and a third plan brings the code to the others.

An AI session on a Fable model drafted this document with the maintainer. A separate AI agent on a
Fable model wrote each of the first two reviews. AI agents on an Opus model gathered the evidence,
did the two readings, wrote the third review, eight of them with one part of the text each, and
were the three readers of the last check. Two AI agents on a Fable model each sketched three
designs for the revision of 2026-11-17. An AI session on an Opus model compared them with the
maintainer, and the same session, then on a Fable model, wrote the revision. Two AI agents on a
DeepSeek model reviewed it, and the session, on an Opus model again, checked their findings and
carried the maintainer's rulings into the text.
The amendments of 2026-11-18 were written by an AI session on an Opus model, at the maintainer's
rulings.
Where a statement rests on a measurement, a page or a source file that was read, section 16 names
the source. Everything else is design, not fact. Nothing here had been run when the design was
agreed. The revision of 2026-11-17 rests on one result of the first plan's runs, and section 16
names that too.

`rollproof` is a working name. It has not been checked for collisions.

## 1. Purpose

`rollproof` tests, in a CI job or on a laptop, whether deploying a new version of a Kubernetes
workload, and rolling it back, causes downtime. It creates a throwaway kind cluster, installs the
previous release, keeps a probe running against the workload, deploys the candidate, rolls back,
and fails if the probe saw a failure or a transition stalled.

It is for any team that deploys to Kubernetes. It is public, open-source work under the Apache-2.0
licence. An organization may fork it and run the fork inside its own network.

It carries no rule of any one organization. The organization that started it configures it like
any other user.

### What a pass shows, and what it cannot

A pass shows that, in a real Kubernetes control plane with real kubelets, the manifests, probes
and shutdown behaviour of the workloads that the probe reaches kept the probe free of failures
while their pods were replaced, in each transition that the report names. The report says which
watched workloads the built-in probe reaches (section 10).

A pass is necessary, not sufficient. Every pass lists what the run exercised and what it did not
(section 10). The main limits:

- **The test cluster is small.** It has a handful of Services, and its nodes stop sending new
  connections to a stopping pod within about a second. A production cluster can take longer, and
  an ingress controller or a cloud load balancer learns of the change on a path of its own. A pod
  that stops sooner than its real cluster stops routing to it refuses connections there and
  passes here.
- **The built-in probe opens a new connection for every request.** It sees a pod that takes
  traffic before it can serve, and a pod that stops while it is still routed to. It does not see
  a connection that a client kept open, or a request still in flight on a pod after new
  connections have moved away from it (section 6).
- **Nodes are containers on one machine.** There is no real network between them, no real node
  capacity and no cloud load balancer. The probe's load is too small to need the capacity that a
  rollout takes away.
- **The probe has one place.** It runs on one node of the cluster, which has no zone. Routing
  that depends on where a client is does not show.
- **A custom probe is the caller's.** The tool vouches that it ran and reported, and for nothing
  that it checks.

### Who can use it

The tool runs on Linux, beside a container engine that may run privileged containers (section 8).
It tests Deployments, and StatefulSets and DaemonSets that update by rolling (section 7). The
built-in probe sends HTTP GET requests; anything else is a custom probe (section 6).

It cannot be used, in the first release:

- on a runner that is an unprivileged pod or container, or with a container engine on another
  machine;
- from a job that itself runs inside a container;
- for pods that no Deployment, StatefulSet or DaemonSet manages;
- with a deployer that begins its change later than the settle time after the transition looked
  finished, such as a controller that polls a Git repository (section 7).

A real chart may need values of its own for the test before it starts in a small cluster with no
cloud around it. The pass then covers the manifests with those values (section 9).

## 2. Decisions that shaped the design

The maintainer made these choices on 2026-11-13. Each names what it rules out.

| Question | Decision | Ruled out |
| --- | --- | --- |
| Who is it for | Public and open source; an organization may fork it | A tool that encodes one organization's rules |
| Which workloads | Any workload from the first release, through a bring-your-own probe, with an HTTP probe built in | HTTP only; HTTP and gRPC only |
| How a deploy is driven | Helm built in, plus bring-your-own commands | Helm only; commands only |
| How far "enterprise" reaches | Authenticated pulls, plus registry mirrors and private certificate authorities. Fully offline stays possible and untested | Authenticated pulls only; a tested offline mode |
| Overall shape | One command-line tool with a thin GitHub Action around it | A reusable workflow assembled from existing actions; a declarative test run by an existing test engine |
| How kind is reached | Linked in as a Go library | The `kind` binary found on the runner |

On 2026-11-16 the maintainer narrowed how two of these are delivered in the first release:

- A bring-your-own probe runs as a pod in the cluster. A probe that runs on the runner waits
  (sections 6 and 15).
- The tool sets up registry access for the cluster's nodes only. Helm uses the configuration the
  caller gave it on the runner (sections 7 and 9).

Why one tool and a thin action, against a reusable workflow:

- GitHub Enterprise Server cannot call a reusable workflow that lives on github.com; a copy has to
  be stored on the server, together with every action it uses.
- A workflow dependency lockfile pins actions, including the ones an action uses, but it does not
  pin calls to reusable workflows.
- A reusable workflow is a whole job, so the caller cannot build its image in the same job. An
  action is a step.
- One self-contained tool is one thing for an organization to allow, mirror and fork, and it gives
  every user on Linux one local command that reproduces a CI failure.

Why kind is linked in: the kind that a GitHub-hosted runner carries changes with the runner image.
Pinning kind inside the binary removes that moving part, and removes one tool the runner must hold.
Section 9 names what the link costs.

## 3. Background

### What two experiments measured

Two experiments on 2026-11-12 replaced the pods of a small Go HTTP service under load in a kind
cluster, once on a workstation and once in 160 jobs on GitHub-hosted runners. The design takes
these results from them:

- **The gate needs two signals.** A run passed only when no request failed and
  `kubectl rollout status` exited 0. A variant with no node free for the extra pod failed no
  request; its rollout stalled, and only the exit code showed it.
- **The client decides what is caught.** A client that opened a new connection per request saw
  failed requests in every run of every variant that fails requests. A client that kept
  connections alive passed a service with no readiness probe in 15 of 15 runs.
- **Failures continue after the rollout reports done.** For the variants that failed, about half or
  more of the failed requests began after `kubectl rollout status` had returned. That command does
  not wait for old pods to finish terminating. For the variants whose process exits as soon as it
  is told to stop, requests on new connections kept timing out until the load ended, up to about
  40 s after the rollout had finished.
- **It fits a small runner.** On a hosted runner with 2 CPUs, 200 requests a second ran with no
  false failure in 150 jobs. That bounds the false-failure rate at 2.0 % per job (95 %), for that
  service, load, runner size and day. A job took about 4.5 minutes. Two CPUs is the size GitHub
  gives a private repository; a public one gets four.
- **A digest reference needs a registry.** A pod whose image was referenced by digest did not start
  after either of kind's image-load commands. With a registry container in the job it started; the
  registry cost about 8 seconds to set up.

What the experiments did not cover, and this design adds: a change of version, a rollback, a
workload that is not their own test service, a chart, any registry that needs credentials, and
anything other than HTTP GET.

What the experiments could not show, and this design therefore does not claim:

- **A pod that ends with work still on it.** No variant served after SIGTERM and then ended
  without draining, and the kept-alive client's library sends a request again when a reused
  connection fails. So the experiments say nothing about connections that a client holds open.
- **How long a pod must keep serving.** The variants served for 0 s or 5 s after SIGTERM. The
  shortest time that passes was not measured.
- **That a stall carries over.** The variant with no free node stalled because the cluster had
  exactly as many nodes as the workload needed. A real cluster usually has spare nodes. The tool
  still fails such a run, and gives the stall a second reason that says a pod could not be
  scheduled (section 10).
- **Capacity and latency.** No variant broke the surge numbers, the replica count or the grace
  period alone, and the load was light: peak load and tail latency were not tested.

### Prior art

A web search on 2026-11-13 found no reusable tool, action or workflow that does this. It found four
small demonstration repositories that run the same loop once for their own sample application, and
Helm's chart-testing, whose upgrade test runs `helm test` after an upgrade and sends no traffic
during it. The search has gaps that its report lists; the verdict is the absence of a finding.

## 4. What one run does

One run is one cluster, one workload under test and one Kubernetes version. Testing several
versions is the caller's job matrix. The workload under test may be several objects. A watched
workload, in the sections that follow, is one of them: one Deployment, StatefulSet or DaemonSet.

```
set up     cluster, namespaces, registry, images, charts, setup commands
install    previous release, wait until ready
probe on   : install window | steady | deploy and settle | roll back and settle |
             failures counted | failures judged from here on
verdict    report, evidence, exit code
```

1. **Set up.** Create a kind cluster with several nodes, wait until every node is ready, and
   create the probe's namespace and the workload's. Start a registry container, put the caller's
   images into it and pull them into the nodes. Fetch the charts that the configuration names,
   and run the caller's `setup` commands.
2. **Install** the previous release and wait until it is ready (section 7).
3. **Start the probe**, let the install settle under it, and hold a steady window before anything
   changes. The time in which the install settles under the probe is the install window. It lasts
   `limits.settle`, counted from the probe's first observation at the earliest and started again by
   a break (section 7). The probe's failed checks in it are counted and reported, and they are not
   judged. Silence, a fault of the probe, a watched workload the tool cannot judge, a signal or a
   failure of the tool or of the install in it makes the run not verified (rule 4 of section 10).
   The steady window begins when the install has settled.
4. **Deploy the candidate.** Wait for the transition to finish, under a timeout, and keep the
   probe running for the settle time after it.
5. **Roll back** to the previous release, with the same wait and the same settle time. This step
   runs only when the deploy passed.
6. **Decide and report.**

A transition is a deploy, a rollback or a restart. The install is not one: the probe starts only
when the install is ready, and a failed check in the install window is not judged.

### Windows

The time from the probe's first observation to the end of the last window is divided into windows
with no gap between them: the install window, the steady window, then one window for each
transition.

The install window runs from the probe's first observation until the install has settled under
the probe (section 7), which takes `limits.settle` at the least. The steady window begins there
and lasts `limits.steady`. A transition's window runs from the moment the tool starts the
transition until `limits.settle` after the transition has finished (section 7), or until
`limits.timeout` when the transition stalls. The settle time is part of the window because
failures can begin after a rollout reports done (section 3). It has to be quiet: a pod change
inside it puts the transition back in progress.

A failed check in the install window is counted and reported, and it does not judge the release. The
install is not a transition, and a cluster may need a moment before it routes to a Service that the
install has just created (section 7). Silence, a fault of the probe, an install that failed, a
watched workload the tool cannot judge, a signal or a failure of the tool in the install window
makes the run not verified, as in any window (rule 4 of section 10). The report keeps the install
window apart from the other windows, so that nobody adds its failures to theirs (section 10).

The time before the probe's first observation, and the time in which the tool stops the probe,
belong to no window. A check that began before the first observation was written is counted in
the install window all the same, so that the install's counts hold every check of the probe's
start. A result that the probe writes while it is being stopped still counts, by its `t`
(section 6).

### When a run passes

A run passes only when all four hold:

1. Every transition finished inside `limits.timeout`, and its command succeeded.
2. The probe reported no failure in the steady window or in a transition's window.
3. The probe kept to its contract and ran until the tool stopped it, and it produced observations
   in every window, with no silence longer than `limits.silence`.
4. Every transition replaced at least one pod.

This list is a summary. Section 10 gives the rules in full.

The report lists, for every transition, how many pods of each watched workload were replaced. A
workload of which none was replaced is marked "not replaced" and does not change the verdict
(section 7).

### Three verdicts

- **Pass.**
- **Fail.** Downtime was seen in a transition's window, a transition stalled, or the command that
  performs a deploy or a rollback failed.
- **Not verified.** The run could not judge. Among the causes: the configuration was rejected,
  set-up broke, a chart could not be fetched, a `setup` command failed, the previous release
  never became ready, the install left nothing to watch, a workload or the probe's target has a
  form the tool cannot judge, the probe never started, the steady window was not clean, the probe
  gave no verdict for a window, a transition replaced no pod, or the run was interrupted. The
  table of reasons in section 10 is the full list.

Not verified fails the job, as fail does. It is reported apart from fail, as "could not test", so
that nobody reads a broken test as downtime or a skipped test as a pass. Section 10 gives the
rules.

### A run with no previous release

A repository that adopts the tool before its second release has nothing to upgrade from. A
configuration that names no previous release gets a run that installs the candidate and restarts
it under the probe, which is what the experiments did. Nothing has to be switched on for this: the
transitions follow from what the configuration names (section 7). Every report of such a run says
that only a restart was tested.

In such a run the install installs the candidate. A candidate that never becomes ready is then
not verified, as every install that never becomes ready is: the test could not begin. The text
names the candidate, not a previous release.

The restart-only run is also the way in for a service that is broken today. While the previous
release shuts down badly, every deploy fails in its window, whatever the candidate does, until a
fixed release exists to deploy from. A restart of the candidate alone tests the candidate.

## 5. Components

The tool is one Go binary. Each unit has one job.

| Unit | What it does | What it knows about |
| --- | --- | --- |
| `config` | Reads and validates the run description. It fills in the `${images.…}` forms later, when `run` hands it the digests | Nothing else |
| `cluster` | Checks the runner before anything is created: the system, the engine, the tool's own binary. Creates and deletes the kind cluster and the run's registry, writes mirror, certificate and credential settings into the nodes, waits until the cluster is ready, and creates the probe's namespace and the workload's | kind, the container engine, the Kubernetes API |
| `images` | Puts the caller's images and the probe's own image into the run's registry, has the nodes pull them, and returns their digests | The container engine, image archives |
| `deploy` | The deploy contract: install, deploy, roll back. A Helm driver and a commands driver. It checks that `helm` is there, fetches the charts, and runs the caller's `setup` commands with the commands driver's command runner | Helm, or the caller's commands |
| `watch` | Observes the watched workloads: when the install is ready and when it has settled, counted from the time of the probe's first observation that `run` hands it, when a transition finished, whether it stalled, which pods were replaced and how long they took to stop. It follows the pods' status and logs while they live, and gathers the events and pod states for the evidence. It performs the restart, because it holds the set of watched workloads. Given a Service's name by `probe`, it says which watched workloads are behind it | The Kubernetes API |
| `probe` | The probe contract: start, stop, collect observations. The built-in HTTP probe and the custom probe. Before the probe starts it checks the built-in probe's target, and names that Service to `run`. At the first observation it checks the clock | The probe's pod, and for the built-in probe its URL |
| `run` | Sequences the phases and marks the windows on the timeline. It reads the probe's observations from the moment the probe starts, so that none waits for a reader | The two contracts as interfaces, and every other unit only as a step it calls |
| `verdict` | Turns observations and transition results into pass, fail or not verified, with reasons, and says at the end of each window, the install window's included, whether the run goes on | Nothing. It is a pure function |
| `report` | Writes the console text, `result.json`, the GitHub job summary and the evidence folder | File formats |

Flow of one run: `config`, then `cluster`, then `images`. `config` then fills in the image forms,
`deploy` fetches the charts and runs the `setup` commands, and `deploy` installs. `probe` starts,
and its first observation opens the install window. `watch` says when the install has settled
under the probe, and the steady window begins there. For each transition `run` marks the window,
`deploy` acts or `watch` restarts, and `watch` waits. At the end of each window, the install
window's included, `run` hands `verdict` what it has so far, and it begins the next window only
when `verdict` says that the run goes on. Then `probe` stops, `verdict` decides once more with
what arrived late, and `report` writes.

Three properties the plan must keep:

- `run` never learns Helm or HTTP. It sees only the two contracts, so a new probe or a new deploy
  driver changes one unit.
- `verdict` does no I/O. Every route to each verdict is a table test that needs no cluster, the
  decision to go on after a window included. `run` holds no copy of the rules.
- Kubernetes is reached through the Go client, and kind through its Go library. Helm is the
  caller's own `helm` binary, because the test should use the Helm that deploys for real.

## 6. The probe contract

This is a public interface. Authors of custom probes depend on it. The block under the next heading
is copied unchanged into the implementation plan.

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

### The contract

**Form.** A custom probe is a container image. The tool runs it as a pod in the cluster, with the
`command`, `args` and `env` that the configuration gives; they mean what they mean in a pod's
container. `env` is therefore a list, and an entry may take its value from a Secret (section 8).
A stock image and a shell loop are a complete probe, with no image to build.

A probe that runs on the runner is not in the first release. One form keeps the contract small,
and a pod reaches a Service's cluster address in the same way with every container engine.
Section 15 says what a probe on the runner would need.

**Lines.** The probe writes one JSON object per line. The tool reads the pod's log, which carries
the container's standard output and standard error as one stream, so it does not matter which of
the two a line was written to.

| Field | Type | Required | Meaning |
| --- | --- | --- | --- |
| `ok` | boolean | yes | `true` when the check passed, `false` when it failed |
| `t` | string: RFC 3339 with a zone | no | When the check began. Default: the time the line was written |
| `n` | integer, 1 or more | no | How many identical checks that passed this line stands for. Default 1. Not allowed beside `ok: false`: every failure is a line of its own |
| `class` | string | no | A short label for the kind of failure, such as `refused` or `timeout` |
| `ms` | number, 0 or more | no | How long the check took, in milliseconds. On a line with `n`, the longest of them |
| `lag` | number, 0 or more | no | How far behind its own schedule the check began, in milliseconds. On a line with `n`, the furthest of them |
| `detail` | any JSON value | no | Data for the evidence. The tool does not read it |
| `v` | integer | no | The version of this contract that the line is written for. Default 1, the only version so far |

Fields are only ever added to this table. The tool ignores a field it does not know and keeps it
in the evidence. A later field that changes how a line counts comes with a new `v`, and a line
whose `v` the tool does not know makes its window not verified, so an older tool never counts a
newer line wrongly. Every top-level name is reserved for the contract; a probe puts data of its
own into `detail`.

How a line is read:

- White space before the first character is skipped.
- A line that does not start with `{` is log text. It is kept in the evidence and not counted.
  There is one exception. Log text that holds `"ok"` followed by a colon looks like an observation
  behind a prefix, such as the date that a logging library puts first, and it makes its window
  not verified. A failure must not vanish because of the way it was printed.
- A line that starts with `{` must be one valid JSON object, or its window is not verified.
- A valid object with no top-level `ok` field is log text, so a library that logs JSON does no
  harm. The report says how many such lines there were.
- An object whose `ok` is not `true` or `false`, or whose known field has a type or a value other
  than the table's, makes its window not verified.
- The last line of a probe that was stopped may be cut short. A final piece with no line end,
  written after the tool asked the probe to stop, is kept in the evidence and not judged.

A fault of a line is placed in the window that holds the moment the line was written. Section 10
says how it is weighed against a failure in the same window. A fault of a line that was written
before the probe's first observation has no window, and makes the run not verified (rule 9 of
section 10).

**Time.** The default time of a line is the timestamp the node's container runtime puts on it in
the pod's log. The runtime takes it from its own clock when it has read the whole line from the
container's output, which is not the moment the probe wrote it. A probe therefore writes each
line as soon as its check ends and does not buffer its output: in Python, `flush=True` or `-u`.
`t` is for a result that is known late, as a consumer probe's is. It does not make up for
buffered output, because silence is measured on when lines were written. A line longer than
16 KiB is stored in pieces and comes back with the time of its first piece.

Putting the probe's lines and the tool's own window marks on one clock is the tool's job, never
the probe author's. With kind, the runner and the nodes share a clock: the nodes are containers
on the runner's kernel, and Linux gives a container no wall clock of its own. The tool checks
this when the probe starts. It compares the time that the runtime put on the first observation
with the moment it read that line, and a difference above a bound that the plan sets ends the run
as not verified. The check is part of the probe's start: the install window opens only when it
has passed, and a check that failed has no window (rule 9 of section 10). An engine
that runs in a virtual machine can have a clock of its own.

An observation is assigned to the window that holds its time, the install window included. One
in the install window is counted there and is not judged (section 7). One whose time is before
the install window began is counted in the install window too: the first observation's own check
began before its line was written, and so may others that the probe's start met. One whose time
is after the last window ended is not counted; the evidence keeps it.
One whose `t` is later than the moment its line was written is a fault of the line, because a
check cannot begin after its result. Like every such fault, it makes the window in which the line
was written not verified.

A failed check that began in one window and whose line was written in a later one is counted
where it began. The report says how many failures crossed the start of a transition in this way,
because the transition may be what cut them. A failed check that began in the install window and
whose line was written in the steady window is therefore counted in the install window, and is
not judged.

**Lifecycle.**

- **Start.** The tool starts the probe when the install is ready. The probe counts as started
  when its first observation arrives. If none arrives within `limits.timeout`, the run is not
  verified, and the report shows the state of the probe's pod (section 10). A probe that exits
  before its first observation ends that wait at once: the run is not verified because the probe
  exited, and nobody waits out the timeout for a probe that is gone.
- **The install window.** The first observation opens the install window, at the moment its
  line was written, by the runtime's timestamp and not by a `t` on the line. The install settles
  under the probe for `limits.settle`, counted from that moment at the earliest and started again
  by a break of the install's conditions (section 7). A failed check in the install window is
  counted and is not judged. The steady window begins when the install has settled, and from
  there on a failed check is judged: in the steady window it makes the run not verified. A probe
  therefore needs no warm-up of its own. A target that the install has just created may need a
  moment before the cluster routes to it, and what a probe's first checks meet then falls in the
  install window. Silence and a fault of the probe are judged in the install window as in any
  other (section 10). Whether a check passed or failed never moves the start of the steady
  window. Only the moment of the first observation, and the install's own conditions, do.
- **Exit.** The probe's pod is never restarted: its restart policy is `Never`. The tool reads an
  exit from the state of the container named `probe`. A probe that exits before the tool stops it
  makes the window in which it exited not verified, the install window included. One that exits
  before its first observation makes the run not verified (rule 9 of section 10). The silence
  that follows an exit is not reported beside it: the exit is the reason.
- **Silence.** Silence is time with no observation. Log text does not end it. It is measured on
  the tool's clock from the probe's first observation to the end of the last window, by when
  observation lines were written and never by `t`, so a probe that reports late results is not
  silent for that reason. A stretch longer than `limits.silence` with no observation makes every
  window it overlaps not verified. The stretch from a window's start to its first observation
  counts, and so does the stretch from the last observation to the window's end. A probe that
  hangs and never writes again therefore cannot pass, and nor can one that hangs in the install
  window and then writes again. The configuration keeps `limits.silence` shorter than
  `limits.steady` and `limits.settle` (section 8), so a window with no observation at all is
  always silent. The built-in probe writes a line within one request timeout and one second while
  it works, so its first moments are not silent for that reason either.
- **Stop.** The tool stops the probe when the run's last window has ended, whether or not every
  transition was run, or when the run ends before the probe's first observation, unless a signal
  ended the run (section 10). It deletes the probe's pod with a grace period as long as the
  drain, so the probe gets SIGTERM, and it goes on reading the probe's lines until the container
  has exited.
  The drain is 10 seconds for a custom probe. For the built-in probe it is the request timeout
  plus 5 seconds.
- **What a probe does on SIGTERM.** It starts no new check, writes the line of every check that
  is still in flight, and exits. A late result written during the drain counts when its `t` falls
  inside a window.
- **A probe that does not stop.** A probe that is still running when the drain ends is killed
  with its pod. That changes no verdict. The report says that the probe did not stop when asked,
  and that checks still in flight were not reported. A shell that runs as the first process of
  its container ignores SIGTERM unless it sets a trap, which is why the example above sets one.
- **Late results.** A probe whose results can arrive up to D seconds after a check began needs
  `limits.settle` of at least D. Otherwise a failure that a transition caused is reported after
  that transition's window has closed (rule 8 of section 10), or, for the last window, later than
  the drain, and then it is lost. The contract has no value for "not known yet".
- **The log stream.** The tool follows the probe's log for the whole run. When the stream ends
  while the container still runs, the tool opens it again, and it must neither lose a line nor
  count one twice. How it does that, and whether following a log survives the kubelet's rotation
  of it, is open (section 14, point 11). After the tool has asked the probe to stop, a stream
  that ends while the pod still reads the container as running is not opened again: the tool
  waits until the pod reads the container as ended, and the end of that stream is then the end
  of the log. A stream ends only after the container has ended, and the pod's status says so a
  moment later (section 14, point 26). Where the tool cannot read the log to its end, the window
  is not verified, as a failure of the tool.
- The probe knows nothing of windows, transitions or thresholds.

**What the author owns.** The rate and the shape of the load are the probe author's. A loop that
runs one check after another slows down when the workload does and reports at most one failure
per timeout, where the built-in probe keeps its rate. What a probe cannot see, the tool cannot
judge. A probe that opens a new connection for every check sees nothing of the connections that
real clients hold, and for gRPC those are what a rollout breaks.

**Where probe pods run.** These names are part of the contract:

| What | Name |
| --- | --- |
| The namespace of every probe pod | `rollproof-probe` |
| A label on every probe pod | `app.kubernetes.io/name: rollproof-probe` |
| The probe's container | `probe` |

They follow the tool's name, which is a working name (section 14).

A probe pod is not in the workload's namespace. A probe therefore names its target with the
namespace: `orders-db.orders`, and not `orders-db`, which resolves only inside the probe's own
namespace.

The tool creates the namespace before the `setup` commands run (section 7), so that `setup` can
add to it what a probe needs: a Secret that the probe's `env` reads, rights for its default
service account when a custom probe reads the Kubernetes API, or the label by which a mesh
injects its proxy.

A workload whose chart denies traffic by default needs a rule that lets the probe in. That rule
belongs in the workload's namespace, beside the rule that denies. It selects the probe's namespace
by the label `kubernetes.io/metadata.name`, which Kubernetes puts on every namespace, and the
probe's pod by the label above. The test's values or a `setup` command add it. kind's own network
plugin enforces such policies.

The tool reads the log of the container named `probe` and judges an early exit by that container
alone, so a proxy that a mesh injects into the pod does not break the contract. It does change
what is tested: the probe's requests then pass through the mesh. A mesh's proxy may send a failed
request again before the probe sees the failure (Istio does so twice by default), which hides
failures. A probe that starts before its proxy is ready reports failures at first. They fall in
the install window, and are not judged, when the proxy is ready within the time the install
settles.

Probe pods run on the control-plane node, so that the worker nodes hold only the workload and the
tool never watches a probe pod. One consequence: a Service whose traffic policy is local to the
node has no endpoint for a client on that node, and cannot be probed from there.

The built-in probe's pod meets the `restricted` Pod Security level, so a label that `setup` puts
on the namespace cannot keep it out. For a custom probe the image decides.

### The built-in HTTP probe

It speaks the same contract. It is the tool's own binary, started as `rollproof probe http`.

- **Open model.** Request *i* starts at start + *i*/rate, whatever earlier requests did. Start is
  the moment the probe starts. The probe reports from its first request on and has no warm-up
  (section 14, point 23).
- **Requests.** HTTP GET to `probe.http.url`, with the headers of `probe.http.headers` when the
  configuration gives any. A `Host` header sets the host that the request names. A URL may be
  `https`. The probe then does not verify the server's certificate, as the kubelet's own HTTP
  check does not, and the report says so.
- **Answers.** The probe does not follow a redirect: an answer with a 3xx status is judged by
  `expect-status` like any other. It reads the answer's body to its end, inside the request
  timeout, so an answer that is cut after its headers is a failure.
- **Connections.** It opens a new connection for every request. In the experiments only such a
  client caught a service with no readiness probe; a client that kept connections alive passed it
  in 15 of 15 runs.
- **What it cannot see.** Once the cluster has stopped sending new connections to a stopping pod,
  the probe has no connection to that pod. A connection that a real client kept open to it, and a
  request still in flight on it, can then be cut with no trace in the probe. A pod that serves for
  some seconds after SIGTERM and then ends without draining therefore passes. Section 12 keeps a
  fixture for this known miss, and section 15 says why a stream of held connections waits.
- **A failure** is a response outside the expected status range (default 200 to 299), a connection
  error, a body that ends early, or no complete answer within the request timeout. The class is a
  string: the status code, such as `"503"`, or one of `refused`, `reset`, `eof`, `timeout`,
  `other`.
- **Output.** One line per failed request, with `t`, `class` and `ms`. One summary line per second
  for the requests that passed and ended in that second: `n`, the `t` of the first of them, the
  longest of them in `ms`, and in `lag` the furthest that any of them began behind its schedule. A
  second in which no request passed gets no summary line. Within one request timeout every
  request has passed or failed, and either writes a line, so the probe is never silent while it
  works: the configuration keeps `probe.http.timeout` shorter than `limits.silence` (section 8).
- **On SIGTERM** it starts no new request, waits for those in flight to end or to time out,
  writes their lines and exits.
- **Names.** It resolves the URL's host for its first request, and for each request after it until
  a lookup succeeds, and then dials that address, so cluster DNS is not on the path of every
  request. The lookup is part of the request and of its timeout. A request whose lookup fails
  before the request timeout is a failure of class `other`, and one whose timeout passes while
  its lookup still runs is a failure of class `timeout`, so a name that the cluster does not
  resolve yet falls in the install window like any other failed check, and a name that never
  resolves makes the steady window not verified (`steady-not-clean`). A Service's cluster address
  does not change during a rollout.

**The target check.** Before it starts the probe, the tool reads the URL's host as the name of a
Service: `NAME.NAMESPACE`, with or without `.svc` and the cluster's domain after it. A single
label names a Service in the probe's own namespace. The tool then looks the Service up, and the
run is not verified in four cases:

- the host ends in `.svc` or in the cluster's domain, and no such Service exists;
- the Service is headless. It has no cluster address: its name resolves to the addresses of pods,
  and those change;
- the host names one pod of a headless Service, as `orders-0.orders.orders.svc` does. The probe
  would go on dialling the address of a pod that has been replaced;
- the Service's internal traffic policy is local to the node, so it has no endpoint for the
  probe's node.

The text names the ways out: a Service with a cluster address for the test, or a custom probe. A
host that names no Service of the cluster, an outside name or an address, is probed as it is. The
report then cannot say which workloads the probe reaches.

A Service with affinity by client address sends all of the probe's connections to one pod until
that pod is removed. The run goes on, and the report says so.

**What to probe.** An endpoint that does what a caller's request does: it runs through the request
path and reaches what the service depends on. A health endpoint is cheap and is often not on that
path, so a probe of it can pass while callers fail. The experiments probed an endpoint that worked
for 20 ms on every request. A probed request that runs longer than the pod keeps serving after
SIGTERM is still in flight when the pod ends, and the probe sees it fail. That is the one view the
built-in probe has of requests in flight.

Inside `run`, a probe is this (a sketch; the plan may adjust it):

```go
// Probe is one running probe. Observations arrive on the channel until Stop returns.
type Probe interface {
    Start(ctx context.Context) (<-chan Observation, error)
    Stop(ctx context.Context) error
}
```

## 7. The deploy contract

This is a public interface. The block under the next heading is copied unchanged into the
implementation plan.

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

### The contract

**No discovery.** The tool never works out what the previous release is. The caller names both
releases. The documentation gives two recipes: the last published chart version, and the chart
from the base branch checked out into a second folder.

**Transitions.** The tool derives them from what the configuration names. There is no list to
write, and the order is fixed.

| The configuration names | The install installs | Transitions under the probe |
| --- | --- | --- |
| Helm: `previous` and `candidate` | `previous` | deploy, then rollback |
| Helm: `candidate` only | `candidate` | restart |
| Commands: `install`, `deploy` and `rollback` | what `install` installs | deploy, then rollback |
| Commands: `install` and `deploy` | what `install` installs | deploy |
| Commands: `install` only | what `install` installs | restart |

Any other combination is rejected: a Helm configuration with no `candidate`, or a `rollback`
command with no `deploy`. A Helm deploy with no rollback, and a restart after a rollback, are not
in the first release.

**Where commands run.** Every command the tool starts (Helm, the `setup` commands and the commands
of the commands driver) runs on the runner, directly and with no shell, in the folder that holds
the configuration file. A relative path in an argument is therefore relative to that folder, as
every path in the file is (section 8). Each command gets `KUBECONFIG` set to the test cluster's
file.

**The namespace.** The tool creates `deploy.namespace` before the `setup` commands run, for both
drivers, so that `setup` can put there what the workload needs. A chart must not bring that
Namespace object itself: Helm refuses to take over an object that it did not create. The test
cluster's kubeconfig file has one context, and that context names `deploy.namespace` as its
namespace. A command that names no namespace, such as `kubectl apply -k`, therefore acts in
`deploy.namespace`.

`deploy.namespace` may not be `kube-system`, `kube-public`, `kube-node-lease`,
`local-path-storage` or the probe's namespace. A restart there would replace the cluster's own
pods or the probe.

**Setup commands.** `setup` is a list of commands. Each is an argument list. They run in order,
after the cluster is ready, the images are in the run's registry and in the nodes, and both
namespaces exist, and before the install. They are for what a probe needs in its namespace
(section 6), and for what the workload needs and its chart does not bring: a Secret, a network
policy, a custom resource definition, a database, a broker. Whatever they create in
`deploy.namespace` is watched like the workload, unless `deploy.selector` leaves it out, so a
dependency belongs in a namespace of its own. Each `setup` command is bounded by `limits.timeout`.

**The Helm driver.**

| Step | What the tool runs |
| --- | --- |
| set-up | `helm pull` of every chart that is not a path, at its `version`, into the run's folder |
| install | `helm upgrade --install` of the release that the table under "Transitions" names, into `deploy.namespace` |
| deploy | `helm upgrade` to the candidate, with `--reset-values` |
| rollback | `helm rollback` to the revision the tool read after the install |

The install, the deploy and the rollback each get `--timeout` with the value of `limits.timeout`,
and the flags that name the test cluster (see "The cluster Helm acts on" below). The pull talks
to no cluster. It gets the same environment as the other three.

**Charts are fetched first.** The pull happens during set-up, before the probe starts, and the
install and the deploy then name the fetched archive. A chart that cannot be fetched makes the
run not verified: the registry is down, a login is missing, or the version does not exist. None
of those is downtime, and without the pull each would fail the deploy. The pull also tells the
tool which chart version Helm resolved, and the report gives it. For a chart that is a path, the
tool reads the version from the chart's own file. Each pull is bounded by `limits.timeout`.

On a new cluster the install is revision 1, the deploy revision 2 and the rollback revision 3. A
rollback applies the manifest that Helm stored for the revision it goes back to, without rendering
it again, as a new revision. The pod templates are then those of the install, so Kubernetes brings
the previous release's pods back. The tool names the revision. With none, Helm goes back to the
highest revision less one, whatever state that one is in.

A release description has `chart`, `version` and `values`. `chart` is required, `version` is
required for a chart that is not a path, and `values` is optional.

- `chart` is a path, an `oci://` reference, or a name that the runner's Helm configuration
  resolves, such as a repository alias with a chart name. An alias needs that repository's index
  on the runner, from an earlier `helm repo add` or `helm repo update`. When `chart` is a path
  that exists, Helm uses it and ignores `version`, so the configuration rejects a `version`
  beside such a `chart`.
- `version` is written as a string. YAML reads `1.10` as a number and drops the zero, and Helm
  takes an empty or absent version to mean the newest chart, so the configuration rejects a
  number, an empty string, and a chart that is not a path and has no version.
- `values` is a list whose items are files or inline maps, applied in the order written: a later
  item wins over an earlier one, key by key. The tool writes each inline map to a values file of
  its own, so types are kept and no key needs a path grammar. Helm splits the argument that
  names a values file at a comma, so the configuration rejects a values file whose path holds
  one.

**The candidate's values.** The deploy passes `--reset-values`. Without it, Helm gives an upgrade
that passes no values the values of the release's last revision, and a candidate with no `values`
would be rendered with the previous release's. With it, the candidate is rendered from its own
chart and its own `values` alone.

Helm runs with the runner's own configuration: its repositories and its registry logins are the
ones the caller set up before the step. The first release names the Helm major versions it is
tested with (section 14).

**The cluster Helm acts on.** The tool names the test cluster on every Helm command line: the
cluster's kubeconfig file with `--kubeconfig`, its context with `--kube-context`, and
`deploy.namespace` with `--namespace`.

That is not enough by itself. Helm takes cluster settings from its environment as well, and each
one that is set overrides what the kubeconfig file says, whichever file the command line names. A
server address among them would send Helm, and the candidate with it, to a cluster other than the
test cluster. `HELM_DRIVER` moves the place where Helm keeps its release records: with `sql` to a
database outside the cluster, and with `memory` into the memory of one Helm process. The tool
therefore takes these variables out of the environment of every command it starts:

`HELM_KUBECONTEXT`, `HELM_KUBEAPISERVER`, `HELM_KUBETOKEN`, `HELM_KUBECAFILE`, `HELM_KUBEASUSER`,
`HELM_KUBEASGROUPS`, `HELM_KUBEINSECURE_SKIP_TLS_VERIFY`, `HELM_KUBETLS_SERVER_NAME`,
`HELM_NAMESPACE`, `HELM_DRIVER` and `HELM_DRIVER_SQL_CONNECTION_STRING`.

The `setup` commands and the commands driver lose them too, because a caller's script may run
Helm. Such a Helm, with no namespace given, takes the namespace of the kubeconfig's context,
which is `deploy.namespace`. The console names each variable that was set and taken out. Every
other variable passes through unchanged.

Helm is called without `--wait` and without the flag that rolls a failed upgrade back (`--atomic`
in Helm 3, `--rollback-on-failure` in Helm 4; in both majors that flag also turns waiting on). In
both majors Helm then returns before the rollout is complete.

Helm hooks run as Helm runs them. Four facts about them matter here:

- Helm waits for a hook that is a Job or a Pod even when it is not told to wait. It runs the
  hooks that come before an upgrade before it changes anything, and the hooks that come after an
  install or an upgrade before it exits. While it waits, the release is marked as pending, and a
  second upgrade of it is refused. A transition is therefore not finished, and an install not
  ready, until Helm has exited (see "When a transition has finished").
- Helm bounds each such hook by its `--timeout`, which is 5 minutes when none is given. The tool
  passes `limits.timeout` there, so that a limit raised for a slow chart reaches Helm too. Helm
  applies it to each hook on its own. The tool's limit covers the whole transition from its
  start, hooks included, and it alone decides a stall.
- A rollback runs the rollback hooks that were stored with the revision it goes back to.
- A hook that fails makes the command exit non-zero, as every other failure of Helm does.

**The commands driver.** Each command is an argument list, started as "Where commands run" says
and without the variables listed under "The cluster Helm acts on". `install` is required. `deploy`
and `rollback` are optional, as the table under "Transitions" shows.

A command must itself bring the change to the watched workloads. It may return as soon as it has
submitted the change; the tool watches from there. A command that hands the change to a controller
is covered only when the controller begins within `limits.settle` of the moment the transition
looked finished (see "When a transition has finished"). A controller that polls a Git repository
every few minutes is outside that. A team that deploys through one applies, with this driver, the
manifests that the controller would apply: the readiness, shutdown and strategy under test are in
the manifests, not in the controller.

**When a command fails.** For both drivers, a non-zero exit of a `setup` command or of the install
makes the run not verified, and a non-zero exit of a deploy or a rollback is a fail. A command
whose program cannot be started counts as a non-zero exit. Helm exits with the same code for
every failure. A deploy that failed because a value broke the chart's schema is therefore a fail
like any other, and it should be: the candidate cannot be deployed with those values. The
command's output in the evidence says what it was.

**When a command runs too long.** A command that is still running at `limits.timeout` is stopped:
the tool sends it SIGTERM and kills it 10 seconds later. For a deploy or a rollback that is a
stall. For the install and for a `setup` command the run is not verified.

**When a signal stops the run.** The tool stops a command that is running in the same way, but
does not wait for it. However such a command then ends, its ending is the tool's doing and is
not judged as a failed command.

**When the tool fails.** The tool stops a command that is running in the same way, and does so
before it stops the probe (section 6), so that no command goes on changing the cluster while the
probe's last lines are read. Its ending is not judged either (section 14, point 27).

**The restart.** The tool performs it itself, the same way for both drivers: it changes the pod
template annotation that `kubectl rollout restart` changes, on every watched workload. A
dependency that the chart bundles into the same namespace is restarted with the rest, unless
`deploy.selector` leaves it out. When the API server refuses that change, the restart's window
is not verified: the test could not be made. The refusal ends the tool's attempt, so the window
does not wait for a rollout that was never started.

**What is watched.** The Deployments, StatefulSets and DaemonSets in `deploy.namespace`,
optionally narrowed by `deploy.selector`, a label selector. The rule is the same for both drivers.
A chart that places workloads in a second namespace has only the named one watched.

- **The set is read live.** A workload that matches at any moment of a transition is watched in
  it. One that the transition creates must complete its rollout. One that the transition deletes
  is complete when its pods are gone. A release may therefore add, drop or rename a workload.
- **Nothing to watch.** When no such workload exists `limits.settle` after the install command
  returned, the run is not verified, and the text names the namespace, the selector and the three
  kinds. A workload that an operator creates a few seconds after the command returned is still
  found, and a mistyped selector costs seconds and not the whole timeout. The check stops when
  the install is ready. After that, a set with no workload is a break of the install (section 7,
  "When the install has settled"). Pods that some other controller manages are not watched
  (section 15).
- **Strategies.** A watched StatefulSet or DaemonSet must update by rolling. With `OnDelete`, a
  changed template replaces no pod until someone deletes one, and `kubectl rollout status` gives
  no status for any strategy but the rolling one. Such a workload is not verified: found before
  the probe's first observation, it ends the run, found in the install window, it makes that
  window not verified, and brought in by a transition, it makes that transition's window not
  verified. It is left out of the conditions for a ready install and a finished transition,
  which it could never meet, so it does not turn into a stall. A Deployment is watched with
  either of its strategies. One that deletes all its pods before it
  creates new ones fails the probe, and that is the right verdict.
- **Identity.** A pod is identified by its UID, never by its name. A StatefulSet's pod comes back
  under the same name and is a new pod.
- **Tests.** The project's own fixtures are Deployments, with one StatefulSet and one DaemonSet
  (section 12).

**When a transition has finished.** All three must hold:

- the transition's command has exited, or, for a restart, the tool's attempt to make its change
  has ended;
- every watched workload reports its rollout complete, by the conditions `kubectl rollout status`
  uses for its kind;
- every pod of a watched workload that has been deleted is gone, not merely terminating. That
  holds for the pods that existed when the transition began and for those created during it.

For a Deployment, `kubectl rollout status` gives up once the Deployment's own progress deadline
has passed. The tool does not: the controller goes on after that deadline, and the condition
clears if the rollout completes. `limits.timeout` alone decides a stall, and the stall's text
shows the condition.

The transition's window then stays open for `limits.settle`, and that time has to be quiet. If,
during it, a pod of a watched workload is created or deleted, or the spec of a watched workload
changes, the transition has not finished after all: all three conditions have to hold again, and
the settle time starts again from there. A change of readiness or a restarted container does not
start it again, because neither creates a pod or changes a spec.

The quiet settle time is what covers a second rollout that follows the first, for example when a
controller restarts a workload after a Secret changed, and a command whose change begins a little
after it returned. It does not cover a change that begins later than `limits.settle` after the
transition looked finished.

A transition that has not finished within `limits.timeout`, counted from its start, has stalled.
The limit is always counted from the transition's start. A transition that finished in time may
have its settle time run past the limit. If a change then puts it back in progress, it has
stalled at that moment, and its window ends there. When the settle time started again, the
stall's text says what started it. The same limit applies to the install and to the probe's
start.

**When the install is ready.** When its command has exited, at least one workload is watched, and
the two other conditions hold. The tool then starts the probe (section 6).

**When the install has settled.** The install settles under the running probe. It has settled
when its conditions have held without a break for `limits.settle`. That time is counted from the
later of two moments: the moment the probe's first observation was written, and the moment the
conditions last came to hold. A break is a moment at which one of those conditions does not
hold: no workload is watched, a watched workload does not report its rollout complete, or a
deleted pod is not gone. It starts the time again. The time from the probe's first observation
until the install has settled is the install window (section 4), and the steady window begins
at its end.

A failed check in the install window is counted for the report, and it is not judged. Two things
are new in that time, and the settle time is for both:

- **The target.** A cluster may route to a Service that the install has just created only a
  moment after its endpoints are ready. In the first plan's end-to-end runs kube-proxy on the
  probe's node took about a second, and the built-in probe's first checks were refused
  (section 16). No release caused those failures. A few attempts in that time leave the node
  without being sent to a pod, and the tool sets the nodes so that the kernel forgets them
  within 5 s (section 9).
- **The probe's own pod.** What its first moments meet, such as a proxy that a mesh injects
  (section 6), falls in the same time.

A failed check in the install window decides nothing, because the install is not a transition
(section 4). The steady window follows it and judges the installed release as before: a release
that keeps failing requests fails them in the steady window too. Whether a check passed or failed
never moves the start of the steady window, so a probe cannot shape its own judgement by what it
reports. The moment of its first observation is a floor: a probe that is slow to write it delays
the steady window, within the `limits.timeout` that its start has.

The install's rule is stricter than the quiet settle time of a transition. Each change that starts
a transition's settle time again, a pod created or deleted or a spec changed, also leaves a
watched workload's rollout not complete, at least until its controller has seen the change, so it
is a break of the install too (section 16). A change of readiness is a break of the install, and
does not start a transition's settle time again. In a transition's window, the failures that a
pod which stops being ready causes are judged. In the install window they are not, so the tool
itself asks for a release that stays ready (section 14, point 23).

The limit is as for a transition. The install has `limits.timeout`, counted from its start, to
become ready, and its settle time may run past that limit. Conditions that do not hold when the
limit passes, or that break after it and before the install has settled, end the run as not
verified: the install failed. When the settle time started again, the report says what started
it.

The install window runs to its end as any window does (rule 2 of section 10), so that the
record of that time is whole: the tool waits until the install has settled whatever the probe
reports or does meanwhile, an exit after its first observation included. A probe that exits
before its first observation ends the run at once (section 6). `verdict` then decides whether the
run goes on. Silence, a fault of the probe, a watched workload of a form the tool cannot judge,
a signal or a failure of the tool in the install window makes the run not verified (rule 4 of
section 10), and the steady window does not begin. A condition of the install window that the
tool learns of only after the steady window began is placed late: the steady window runs to its
end, and the run stops there (rule 8 of section 10).

**Pods replaced.** For each watched workload the tool counts how many of the pods that existed at
the start of a transition are gone at its end, and how many pods the transition created. A
transition replaced pods when at least one pod of a watched workload that existed at its start is
gone at its end, and at least one pod of a watched workload was created in it. The two may belong
to different workloads, so a release that renames a Deployment has replaced pods.

The count is judged only for a transition that finished and whose command succeeded. A stall and
a failed command are judged as what they are (section 10). A transition that finished and
replaced no pod is not verified: it changed nothing the test can judge.

The report lists the count for every workload. A workload with none is marked "not replaced", and
one that the transition created or deleted "added" or "removed" (section 10). A run still passes
when the workload behind the probe is one that was not replaced. A deploy that leaves a workload
alone caused it no downtime, and the report says that its rollout was not tested.

**How long old pods took to stop.** For each pod that a transition deleted and that was ready
when it was deleted, the tool notes when its own watch first saw the deletion and when the pod
was gone. The report gives the shortest of these for each watched workload in each transition. It
is an upper bound: that pod served for at most this long after it was told to stop, and the time
includes the kubelet's clean-up. A small figure is a warning for a larger cluster (section 1). A
large one proves nothing. A pod that was never ready is left out, because it stops quickly and
says nothing of shutdown. The pod's own deletion timestamp is not used for this: it is the
deadline, the time of the request plus the grace period, in whole seconds.

Inside `run`, a deploy driver is this (a sketch; the plan may adjust it):

```go
// Deployer performs the install and the transitions that a driver owns.
type Deployer interface {
    Install(ctx context.Context) error
    Deploy(ctx context.Context) error
    Rollback(ctx context.Context) error
}
```

The plan's interface tells three endings of a command apart, because the report does: it exited
non-zero, it could not be started, or the tool stopped it at the limit. The restart
is not in this interface; `watch` performs it (section 5).

## 8. Configuration and the action

This is a public interface. The block under the next heading is copied unchanged into the
implementation plan.

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

### The config file

The file is `rollproof.yaml` unless `-f` names another. Unknown keys are rejected, so a typo cannot
silently weaken a test.

```yaml
version: 1                  # required

cluster:
  node-image: …             # a full reference; default: the linked kind release's own
  registry-image: …         # a full reference; default: pinned by digest in each release
  workers: 3                # 1 or more

registries:                 # section 9: what the nodes need in order to pull
  - host: registry.example  # required; one entry for a host
    username: ${env.REGISTRY_USER}
    password: ${env.REGISTRY_PASSWORD}
    ca: certs/corp-ca.pem   # a PEM file with one or more certificates
    mirror: mirror.corp.example/registry-example

images:                     # section 9: images that are not in a registry yet
  orders:
    local: orders:candidate # or: archive: PATH
    as: registry.example/team/orders

setup:                      # section 7: commands run before the install
  - [kubectl, apply, -f, test/prerequisites.yaml]

deploy:                     # section 7
  namespace: orders         # required
  selector: app=orders      # optional
  helm:                     # or: commands
    release: orders         # required: the name of the Helm release
    previous:               # optional; without it the run is restart-only
      chart: oci://registry.example/charts/orders
      version: "1.4.2"      # a string
    candidate:              # required
      chart: ./chart
      values:               # files and inline maps, in order
        - test/values-kind.yaml
        - image: { digest: "${images.orders.digest}" }
  # commands:
  #   install:  [kubectl, apply, -k, overlays/previous]    # required
  #   deploy:   [kubectl, apply, -k, overlays/candidate]
  #   rollback: [kubectl, apply, -k, overlays/previous]

probe:                      # section 6
  http:
    url: http://orders.orders.svc/api/orders/latest    # required
    headers: { Host: orders.example }   # optional
    rate: 200
    timeout: 2s
    expect-status: 200-299
  # or:
  # custom:
  #   image: registry.example/tools/busybox@sha256:…   # required
  #   command: [sh, -c, "…"]
  #   args: []
  #   env:                  # a list, as in a pod's container
  #     - name: TOKEN
  #       valueFrom: { secretKeyRef: { name: probe-token, key: token } }

limits:
  steady: 10s
  settle: 15s
  timeout: 5m               # the defaults table says what it bounds
  silence: 5s
```

Exactly one of each pair must be present: `deploy.helm` or `deploy.commands`, `probe.http` or
`probe.custom`, and `local` or `archive` in each entry of `images`.

**Version.** `version` is required, and it is `1`. A tool rejects a version it does not know.
Within a version, keys are only added, and a tool rejects a key it does not know, so a file
written for a newer tool fails loudly on an older one. A default may change between releases; the
report prints every value that differs from its default (section 10).

**Paths.** Every relative path in the file is relative to the folder that holds the file: a
chart, a values file, a `ca` file, an image archive, and a path among a command's arguments. The
tool starts every command in that folder (section 7) and gives Helm absolute paths. A run is then
decided by the file, not by the folder it was started from.

**Types.**

- A duration is a number with a unit: `500ms`, `10s`, `5m`.
- `expect-status` is one status code, or one range written with a hyphen, both ends included.
- A chart's `version` is a string (section 7).
- `cluster.workers` is 1 or more. With no worker the workload would share the probe's node.
- `headers` is a map of header names to values. The built-in probe sends them with every request.
  They are for what the workload needs in order to answer as it answers a caller: a host name, or
  the header that says the caller's connection was encrypted. They are not for a credential (see
  "Secrets").
- A custom probe's `env` is a list of entries, as in a pod's container. An entry has a `name`,
  and either a `value` or a `valueFrom`.
- In an entry of `registries`, `username` and `password` come together or not at all.

**What else is rejected.**

- A `limits.silence` that is not shorter than both `limits.steady` and `limits.settle`. A window
  must be long enough to show a silent probe.
- A `probe.http.timeout` that is not shorter than `limits.silence`. While every request is slow
  and none has timed out yet, the built-in probe writes nothing, and a sound run would end as
  silent.
- The same host in two entries of `registries`.
- A `deploy.namespace` that section 7 reserves.
- A probe URL that carries a user name or a password.
- A combination of releases or commands that section 7 rejects, a `version` beside a chart path
  that exists, a chart that is not a path and has no `version`, and a comma in a values file's
  path.
- An image archive that holds more than one image.

**Substitution.** It applies inside string values, never to keys, and only to fields whose type is
a string. Three forms exist:

- `${env.NAME}`: an environment variable;
- `${images.NAME.digest}`, `${images.NAME.repository}` and `${images.NAME.ref}`: the digest, the
  repository, and the two joined, of an image that the `images` section put into the run's
  registry. They may appear under `setup`, `deploy` and `probe`, and `NAME` must be an entry of
  `images`;
- `$${`: a literal `${`.

Any other `${…}` is a configuration error, so a form added later cannot change the meaning of a
file that exists today. Substitution is one pass: text that was put in is not read again, so a
value may itself hold `${`. Values files are passed to Helm as they are; substitution does not
reach into them.

A variable that is not set, or is set to the empty string, is a configuration error that names
the variable. It is never an empty value. On GitHub a secret that does not exist arrives as an
empty string, and a workflow started from a fork gets no secrets at all.

**Secrets.** The one secret the tool handles is a registry password. `registries[].password` must
be a single `${env.NAME}`; a password written in the file is rejected, and the text that rejects
any configuration never quotes a value. The tool masks every such password in everything it
writes, in its plain form and in the encoded form in which the nodes' credentials file holds it.
That includes the output of commands and the pod logs that it copies into the evidence.

The tool masks nothing else. A chart value or a probe header's value is written as it was given,
so a caller who passes a real secret there keeps the evidence to themselves. GitHub's own masking
does not cover the evidence: its documentation promises it for job logs, job summaries and job
outputs, and says nothing of an uploaded artifact or of a file that a step writes.

The console, the job summary and `result.json` give names and no values: the names of the probe's
headers, of a custom probe's `env` entries and of the values files. A custom probe's `command`
and `args` appear there as counts. The evidence folder may hold the values.

A custom probe that needs a credential takes it through `env` with `valueFrom`, from a Secret
that a `setup` command created in the probe's namespace. The value then never passes through the
tool.

### Defaults, and the reason for each

| Setting | Default | Reason |
| --- | --- | --- |
| `cluster.node-image` | The default node image of the linked kind release | kind pins it by digest and maintains it. A table of the tool's own would go stale with every kind release |
| `cluster.registry-image` | A registry image pinned by digest, written into the tool at each release | It is the one image the tool itself starts. A digest names its content, so an organization can mirror it and name the copy here |
| `cluster.workers` | 3 | A rolling update that adds a pod before it removes one needs a node for the extra pod. Three workers give that room to two replicas that must not share a node, which was the experiments' shape. A workload with three such replicas needs four workers (section 9) |
| `probe.http.rate` | 200 a second | Measured to fit a hosted runner with 2 CPUs, with no false failure in 150 jobs, against a handler that sleeps. A handler that costs CPU may not hold this rate on a small runner, and a clean steady window does not show that it will: a transition adds a starting pod to the same CPUs. The report gives, for every window, how far the probe ran behind its schedule, which shows a starved runner |
| `probe.http.timeout` | 2 s | It is the caller's own latency rule: a slower request is a failure. 2 s is the experiments' value, where the slowest request that passed on a hosted runner took 247 ms. A service with a latency budget writes its budget here, and keeps `limits.silence` above it |
| `probe.http.expect-status` | 200 to 299 | A redirect or an error page is not an answer from the workload |
| `limits.steady` | 10 s | Long enough to show that the probe and the workload work before anything changes: 2,000 checks at the default rate. It is not a control for a failure that is rare without any rollout; the paragraph below the table is for that |
| `limits.settle` | 15 s | It covers what begins only after a transition has finished: a second rollout, a controller that acts a little late, a new pod that fails soon after it became ready, and a probe's late results. The experiments do not size it. Their failures after a rollout reported done ended within about 3 s, but they are counted from `kubectl rollout status`, which returns before old pods are gone, and the tool waits for those. 15 s is a first value, and a fixture is to confirm it or change it (section 14, point 7). The install settles for the same time under the probe, in the install window (section 7). There it covers the moment a cluster takes to route to a Service that the install has just created, about a second in the first plan's runs, and the first moments of the probe's own pod. Nothing sizes it for that either. Every run that reaches its steady window reports how long before it the last failed check came, which shows how much of the time was needed |
| `limits.timeout` | 5 minutes | The length of Helm's default timeout for one operation. It is not the same bound: Helm applies its timeout to each hook separately, and this limit covers a whole transition from its start, hooks included. The tool passes the value to Helm as its `--timeout` (section 7). One limit covers, each on its own: the wait for the cluster to be ready, a node's pull of an image, a chart's fetch, a `setup` command, the install, the probe's start and a transition |
| `limits.silence` | 5 s | While it works, the built-in probe writes a line within one request timeout, and at the defaults at least one a second. A custom probe that checks less often raises it, and raises `limits.steady` and `limits.settle` with it |

There is no failure budget. A pass means that the probe reported no failure in the steady window
or in a transition's window, which is the claim the tool is named for. A failed check in the
install window is counted and does not change the verdict (section 7). Thresholds of other kinds
are not in the first release (section 15).

**A failure that no pod event explains.** With no budget, one failed request in a judged window
keeps a run from passing, and a workload may fail one request in some minutes with no rollout at
all, or a small runner may starve the probe. The check for that is one run with `limits.steady: 5m`.
A failure in that steady window is the workload's own baseline or the runner, not the rollout. The
built-in probe also reports how far it ran behind its schedule (section 6), which shows a starved
runner.

### The command line

- `rollproof run`, with `-f FILE`, `--out DIR`, `--name NAME` and `--keep-cluster`.
- `rollproof probe http`, which the probe pod runs. Users do not call it.
- `rollproof version`. It prints the tool's version, the kind release it links, and the
  references of the node image and the registry image that it uses by default, so that an
  organization knows what to mirror.

Exit codes of `run`: 0 for pass, 1 for fail, 2 for not verified. A configuration that is rejected
exits 2. Exit 1 means a fail verdict and nothing else. The tool turns a crash of its own into the
reason `internal-error`, and the exit code follows the verdict that section 10 then gives: 2,
unless the window in progress had already failed. When SIGINT or SIGTERM came during the run,
the tool ends by that signal instead, after it has written its results, whatever the verdict
(section 10, "On a signal").

`--out` names the folder for the results. It holds `result.json` and a folder named `evidence`.
The tool replaces those two there and touches nothing else in the folder. The default is a new
folder under the system's temporary directory, or under the runner's own temporary directory
when the tool detects GitHub Actions. The console prints its path.

`--name NAME` names the cluster `rollproof-NAME` (section 9).

### The action

`action.yml` is a composite action. It uses no other action, so an organization allows and syncs
one action, and a lockfile pins one.

| Input | Meaning | Default |
| --- | --- | --- |
| `config` | Path of the config file | `rollproof.yaml` |
| `image` | The repository to pull the tool's own image from. The action adds the digest that was written into `action.yml` at the release. A reference that already carries a digest is used as it is | The project's repository |
| `out` | Folder for the results | a folder under the runner's temporary directory |

| Output | Meaning |
| --- | --- |
| `verdict` | `pass`, `fail` or `not-verified` |
| `out` | The folder that holds `result.json` and the evidence |

The output is not named `outcome`, because GitHub gives every step an `outcome` of its own, with
other values, and a condition written against the wrong one is silently false.

How the tool reaches the runner: the action checks that the runner is Linux, finds the container
engine as kind does, pulls the tool's image by digest and copies the binary out of it. Everything
the tool needs from the network is then an image, so an organization mirrors one kind of thing,
and a digest names content, not a location. An organization that mirrors the image changes the
repository and keeps the digest. Each release also attaches plain Linux binaries, for laptops and
for other CI systems.

The action starts the tool in place of its own shell, with `exec`. GitHub sends a cancel signal
to the first process of a step, and the tool has to be that process to learn of it (section 14,
point 20).

The version and the image digest are written into `action.yml` at release time. A composite action
cannot rely on reading its own ref at run time.

The tool writes the outputs and the job summary itself when it detects GitHub Actions, so the
action holds no logic beyond fetching the binary and calling it. A caller whose runner already
holds the binary needs no action: a plain step that runs `rollproof run` gives the same outputs
and the same summary.

Two things in this section rest on open points. That `env:` on the step that calls the action
reaches the action's own steps is not shown (section 14, point 3). Neither is that the `verdict`
output is kept when the step exits non-zero (point 4).

The action uploads nothing. The documentation shows the caller's own upload step, which uses the
`out` output.

What the runner must hold: Linux; a container engine that kind supports, on the same host and in
the same network namespace as the tool, that may run privileged containers; and `helm` on the PATH
when the Helm driver is used. The tool checks these before it creates anything, and a runner that
lacks one makes `run` stop with a message and exit 2. Section 9 lists the runners that this rules
out. kind documents one more need: with the kernel's inotify limits at a low default, pods in a
cluster of several nodes fail with "too many open files". The text for a failed set-up names
those limits.

Each release says which CPU architectures it is built for and on which of them its cluster suite
ran. The first release is built for amd64 and arm64, and its suite runs on amd64. The released
binaries are statically linked, because the binary is also the probe's whole image (section 9).

## 9. Cluster, images and enterprise settings

### The cluster

- **Shape.** One control-plane node and `cluster.workers` worker nodes, at least one. Each worker
  carries a zone label of its own (`topology.kubernetes.io/zone`), and the control-plane node
  carries none. A workload that must spread by zone can then be scheduled. The probe's traffic
  starts on the control-plane node, and a node with no zone ignores zone hints, so that traffic
  reaches every endpoint as it would with no zones at all. Kubernetes' own multi-zone test job on
  kind has this shape. One thing does change: a Service that asks for zone-local routing gets it
  between the workload's own pods.
- **Ready.** The cluster is ready when every node is ready and the cluster's DNS runs. kind's own
  wait covers the control-plane node only, so the tool waits for the rest before it loads images,
  runs `setup` or installs. Otherwise the install's pods would land on whichever worker came up
  first, and the rollout under test would differ from run to run. The wait is bounded by
  `limits.timeout`.
- **What the shape decides.** A rolling update that adds a pod before it removes one needs room
  for the extra pod. A workload with as many replicas as there are workers, and a rule that
  forbids two of its pods on one node, has no room here and stalls. That stall shows that the
  manifests cannot roll in a cluster of this shape. A real cluster usually has spare nodes, so
  the run fails with two reasons: the stall, and a second one that says a pod could not be
  scheduled (section 10). The text names `cluster.workers`.
- **Kubernetes version.** The node image decides it. The default is the default node image of the
  kind release the tool links, which kind pins by digest. `cluster.node-image` names another by
  full reference; that is how a mirror is used, and how a job matrix covers several versions. The
  tool promises the node images that its own suite ran on (section 12). The others that kind
  lists for the linked release are expected to work, and any other image is untested. Every
  report states the Kubernetes version that was read from the cluster.
- **Name and lifetime.** A run's cluster and registry carry a name that begins with `rollproof-`.
  The rest is derived from the configuration file's path, so the name is the same from run to
  run. `--name NAME` gives `rollproof-NAME` instead, for two runs at once on one file; kind allows
  lower-case letters, digits, dots and hyphens there. `run` first deletes any cluster and
  registry of its name, then creates its own, and deletes them at the end, whatever the verdict,
  except after a signal (section 10). The registry goes with its volumes. Because every name
  carries the prefix, the tool never deletes a cluster that it did not name. `--keep-cluster`
  leaves them for debugging, a cluster that failed to come up included, and the next run replaces
  them. The stable name is what cleans up after a cancelled job: on a runner
  that persists, a cancelled step has about 10 seconds before its processes are killed, and
  deleting a cluster took 12 to 22 s in the experiments.
- **The kubeconfig.** The tool gives kind a kubeconfig file of its own, in the run's folder, when
  it creates the cluster and when it deletes one. Left alone, kind's library writes a new cluster
  into the caller's own kubeconfig, makes it the current context, and clears that context when
  the cluster is deleted. The tool never writes the caller's file. With `--keep-cluster` the
  console prints the file's path and the context's name.
- **The run's folder.** The kubeconfig, the credentials file for the nodes, the inline values
  files and the fetched charts live in one folder under the system's temporary directory. The
  folder carries the cluster's name, so that the next run finds it, and only the user who runs
  the tool can read it. It is not the results folder, which a caller may upload. It is removed
  with the cluster. A kept cluster therefore keeps its credentials file, which its nodes mount,
  and the next run's clean-up removes what a killed run left.
- **Storage.** kind has one storage class, `standard`. A volume of that class is a folder on one
  node, and it is tied to that node by the node's host name. A StatefulSet's pod that is deleted
  and created again keeps its claim, so it returns to the same node, whatever the zone labels
  say. A Deployment's new pod that mounts a claim starts on the same node and shares the folder.
  A cloud disk that attaches to one node at a time would make that pod wait, and this cluster
  does not show it (section 10).
- **Rootless engines.** When the container engine runs without root, the tool sets the option in
  the nodes' container runtime that stops each container from taking a kernel session key. Without
  it, a few clusters exhaust the user's key quota. Whether one cluster needs it is open
  (section 14).
- **Attempts to connect that had no answer.** The tool sets, in each node, how long the kernel's
  connection tracking keeps an attempt to connect that has had no answer: 5 s, where Linux keeps
  it 120 s (`net.netfilter.nf_conntrack_tcp_timeout_syn_sent`). While kube-proxy brings in the
  rules of a Service that the install has just created, a few of the built-in probe's attempts
  leave the node without being sent to a pod, and the kernel keeps each one with that route.
  The probe opens a connection for each request and uses each source port again, often within
  half a minute. A later attempt from one of those ports takes the kept route and times out, up to
  120 s after the first: in the steady window, or in a transition's window, where it fails a
  release that did nothing wrong. With 5 s, the kept attempts are gone inside the install
  window (section 7). A node on which the value cannot be set fails the cluster
  (`cluster-failed`). Section 14, point 24, has the runs and what the setting hides.
- **What linking kind costs.** A new Kubernetes minor reaches users only after a kind release and
  then a release of this tool. The project accepts that rhythm and automates it: a dependency
  update that moves kind runs the cluster suite, and a green run is released.

### Values for the test

A chart written for production may not start in this cluster as it is. The usual reasons:

- resource requests sized for production nodes, on a runner with two or four CPUs;
- a storage class by name, where kind has one class, `standard`;
- node selectors or tolerations for node pools that these nodes are not in;
- a readiness gate that a cloud controller sets: a condition that nobody sets counts as false,
  and the pod is never ready;
- cloud identity or cloud services that the pods need in order to start;
- a Service of type LoadBalancer, which nothing here fulfils, and an autoscaler, which has no
  metrics here.

Each is answered by a values file for the test, or by a `setup` command. The pass then covers the
manifests with those values, and the report names the values files that were used (section 10). A
values file for the test may change sizes, classes and placement. It should not change what is
under test: the probes, the update strategy, the grace period and the shutdown hooks.

### The run's registry

The tool starts a registry container on the cluster's network, after the cluster exists, and
writes its address into every node's registry settings. This is what makes an image reference by
digest resolve. The registry's own image is `cluster.registry-image`, which has a default pinned
by digest (section 8).

The node settings name the registry by its address on the cluster's network, not by a name.
Behind a proxy, kind passes the proxy variables into its nodes and exempts the network's
subnets, the node names and the cluster's service names. A registry that the settings named
otherwise would be asked for through the proxy, which cannot reach it. On the runner the registry
listens on a port of the loopback address that each run picks for itself, so two runs do not
collide.

### How the caller's image gets in

`images` lists the images that are not in a registry yet. Each entry is named, and one key says
where the image is now:

- `local: NAME:TAG`: in the runner's container engine;
- `archive: PATH`: an image archive file that holds exactly one image. One that holds more is
  rejected with the configuration.

The tool puts the image into the run's registry and records its digest.

The image is stored by digest only, never under a tag. A tag can come to name another image with
no sign of it. When the run's registry does not hold a tag, or cannot be reached, a node goes on
to the real registry and runs whatever that tag names there; and a node that already holds an
image under a tag does not pull that tag again. A digest names one content, wherever it is pulled
from. A chart that takes only a tag writes the tag with the digest after it,
`candidate@${images.orders.digest}`. The node's container runtime drops the tag from such a
reference and asks the registry for the digest alone. Kubernetes still takes the default pull
policy from the tag: `latest` before the digest gives `Always`, and any other tag gives
`IfNotPresent`. The pull is by digest in both cases. The fall-back to the real registry and the
dropped tag are read from the runtime's source and have not been run (section 14, point 1).

`as` names the repository that the real manifests use, for example
`registry.example/team/orders`. The tool stores the image in the run's registry under that
repository's path, without the host, because a node asks a mirror for the path alone. It tells
the nodes, for that registry host, to try the run's registry first. The manifest under test is
then the manifest that deploys: the same repository and the same digest form, with nothing
rewritten.

When `as` is left out, the tool uses a repository under `rollproof.test`, a host that exists only
inside the run. `${images.NAME.repository}` is the repository, `${images.NAME.digest}` the digest,
and `${images.NAME.ref}` the two joined, so a chart that takes them apart or together can be
given them. The address of the run's registry never appears in a caller's file.

**Into the nodes.** After it has stored an image, the tool has every node pull it, by its
repository and digest, through the node's own registry settings. A node that cannot pull an image
that the tool has just stored shows a fault of the set-up: the registry, the node settings or a
full disk. The run is then not verified before anything is deployed, and no stalled deploy is
blamed on the candidate. The first pull of the candidate also no longer falls inside the deploy's
window. Each node's pull is bounded by `limits.timeout`. How the tool asks a node to pull, so
that the node uses the settings it uses for the kubelet, is open (section 14, point 1).

**Which pods ran it.** For every entry of `images`, the report says which watched workloads had
pods that ran its digest, and which transition created those pods (section 10). A chart that does
not read the value it was given, or a typing error in its key, shows there as an image that no
watched pod ran. That does not change the verdict: a probe's image and a dependency's are rightly
run by no watched pod.

An image that is already in a registry is not listed. The nodes pull it from there, through the
`registries` settings, as they pull the previous release's images. A caller who needs its digest
passes it in through the environment.

**The probe's own image.** The tool packages its own running binary as an image, pushes it into
the run's registry and has the control-plane node pull it. Nothing is pulled from outside, and
the probe cannot be a different version from the tool. The image holds the binary and nothing
else, so the binary must be statically linked. The released ones are. The tool checks its own
binary before it creates anything, and one that needs a shared library makes `run` stop with a
message. This is one reason the tool runs on Linux only.

### Registries: credentials, mirrors and certificate authorities

A `registries` entry says what the cluster's nodes need in order to pull from one host. There is
one entry for a host. Each node gets the mirror and certificate settings for the host, and a
credentials file, mounted when the cluster is created, that holds only the hosts listed. The
runner's own Docker login file is never copied into a node.

The entry configures nothing else. Helm runs with the runner's own configuration (section 7), and
the tool itself pulls from no registry.

`ca` is a PEM file with one or more certificates. The nodes trust them for the entry's host and
for its mirror, in addition to the authorities they trust already.

`mirror` is a host, with or without a path, reached over HTTPS. The node's container runtime puts
the registry API's own path after it. A cache that expects its project name inside that API path,
and not before it, needs a different node setting, and how `mirror` says which of the two it
names is open (section 14).

**The order for one host.** A node has one list of places for each registry host, in this order:

1. the run's registry, when an entry of `images` names that host in `as`. It may serve a pull and
   may not resolve a tag;
2. the entry's mirror, which may do both;
3. the real host, which may do both.

The real host stays last. A mirror that lacks an image or is down is therefore passed over with
no sign, and in a network that reaches only the mirror the pull then fails at the real host.
Whether a caller should be able to forbid that last step is open (section 14, point 2).

What the source of the kubelet and of the container runtime shows about credentials (read, not
run; section 16): the kubelet looks credentials up by the registry host in the image's own name,
and passes them on with no host attached. The container runtime then offers them to every host it
contacts for that pull. Three things follow.

- An entry's username and password reach the entry's mirror too. The caller must trust the mirror
  with them.
- Credentials listed under a mirror's own host are never used for an image that is named after the
  original host. A mirror that needs credentials of its own needs another mechanism in the node,
  and that is open (section 14).
- Credentials that a caller lists under the real host for the mirror's sake are offered to the
  real host as well, whenever a pull goes on to it.

**Who can read the credentials.** The file sits in every node, at the place where the kubelet
reads it, and kind sets no Pod Security level. Any pod that mounts that path of its node can
therefore read it: the workload under test, a Helm hook, a pod that a `setup` command started, a
custom probe. A caller gives the tool credentials that can only read, and that expire soon.

Pods may still use `imagePullSecrets` from their own chart. The tool does nothing for them and
does not get in their way.

### What stays outside the tool

- **The engine's own pulls.** The tool cannot configure the runner's container engine. That
  engine pulls the node image and the registry image, and for the action the tool's own image. The
  caller gives them as full mirror references, logs the engine in beforehand when that is needed,
  and makes the engine trust a mirror's private certificate authority.
- **Helm's own pulls.** A chart registry that needs a login or a private certificate authority is
  set up for Helm by the caller, before the step. The tool passes Helm no login and no
  certificate flag. Helm reads its repositories and its registry logins from its own files on the
  runner, and falls back to the logins that Docker stored. Given no certificate flag, Helm trusts
  the certificate authorities of the runner's system store. A caller adds a private authority to
  that store, or names a bundle in `SSL_CERT_FILE` or a folder in `SSL_CERT_DIR`. A bundle named
  that way replaces the system's, so it must also hold the public authorities that are still
  needed. A repository that was added under an alias with a certificate file keeps that file.
  The tool passes both variables on to Helm.
- **Proxies.** kind passes `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY` into its nodes. The tool
  adds nothing to them; it names its registry by address so that kind's own exemptions cover it
  (see "The run's registry"). No run behind a proxy has been made (section 14, point 19).
- **Runners inside Kubernetes pods.** kind needs privileged containers. A runner pod that may run
  them, with a container engine inside it, is a runner like any other. One that may not cannot
  run the tool; GitHub's smallest hosted runner is such a container. This is documented as a
  requirement and not solved.
- **Jobs inside a container, and engines elsewhere.** The tool reaches the cluster's API server
  and the run's registry on the engine host's own loopback address. A job that runs inside a
  container is in another network namespace, and an engine on another machine is another host.
  Neither is supported in the first release (section 15).
- **Engines other than Docker.** kind marks its Podman and nerdctl providers experimental. Docker
  is the supported engine. The project is developed on nerdctl, so nerdctl is expected to work and
  is not promised. An engine that runs without root counts as one that may run privileged
  containers.

## 10. Verdict and report

### Rules of the verdict

1. The windows are judged in order: the install window, the steady window, then each
   transition's. The first window that fails or is not verified decides the verdict and ends the
   run. The windows after it are not run.
2. Every window runs to its end, whatever happened in it, so that the evidence is whole. The
   install window lasts until the install has settled, or until the install fails (section 7).
   The steady window lasts `limits.steady`. A transition's window lasts until the transition has
   finished and the settle time has passed, or until `limits.timeout` when it stalls. Only a
   signal, or a failure of the tool itself, cuts a window short.
3. Each condition from the probe's first observation on is placed in a window. Rule 9 says what
   happens to one before it.
   - A failure belongs to the window that holds its time.
   - Silence belongs to every window it overlaps.
   - A stall, a failed command, a refused restart, a watched workload of a form the tool cannot
     judge, and a transition that replaced no pod belong to their transition's window. An install
     that fails after the probe's first observation, and a watched workload of a form the tool
     cannot judge that appears in that time, belong to the install window.
   - A fault of the probe belongs to the window that holds the moment it happened: an early exit,
     a line that breaks the contract, a `t` later than its line.
   - A signal and a failure of the tool itself belong to the window in progress. One that comes
     while the tool is stopping the probe, before the verdict is decided, belongs to the last
     window.
4. The install window is not verified when silence, a fault of the probe, an install that
   failed, a watched workload of a form the tool cannot judge, a signal or a failure of the tool
   is placed in it. A failure placed in it is counted
   and reported, and it decides nothing: the install is not a transition, and the steady window
   that follows judges the installed release (section 7).
   The steady window is not verified when anything is placed in it: a failure, silence, a fault
   of the probe, a signal, a failure of the tool. A failure before anything changed points at the
   previous release, at the probe or at the runner, not at a transition.
5. A transition's window fails when the probe reported a failure in it, when the transition
   stalled, or when its command failed.
6. A transition's window is not verified when the probe was silent in it, when a fault of the
   probe, a signal or a failure of the tool is placed in it, when the API server refused the
   tool's restart, when a watched workload took a form the tool cannot judge, or when the
   transition finished, its command succeeded and it replaced no pod.
7. In one window fail outranks not verified, and the report lists every reason. A probe that
   reports failures and then crashes has shown downtime. So has a run in which the tool itself
   failed after the probe had reported failures.
8. A condition may be placed late, in a window the run has already left: a failure that the probe
   reports late, a fault in a line that the tool reads late, or silence that began in one window
   and passed the limit in the next. That window is then judged by it all the same, the install
   window included. The window in progress at that moment runs to its end, and the run stops
   there. Every window that ran after the judged one is reported as not
   judged, and a transition that never started as not run.
9. What happens before the probe's first observation has no window. Each of these makes the run
   not verified: a rejected configuration, a runner the tool cannot use, a cluster, an image or a
   chart that could not be set up, a `setup` command or an install that failed, nothing to watch,
   a workload or a target the tool cannot judge, a probe that never started, a clock in the nodes
   that differs from the runner's, a fault of the probe (an early exit, a line that breaks the
   contract, a `t` later than its line), a signal, a failure of the tool.
10. Nothing that happens after the verdict was decided changes it. The verdict is decided once
    the probe has stopped and its late lines are in (section 5). An error while the tool then
    writes its report, completes the evidence or deletes the cluster is a warning, on the
    console and in `result.json`.

Why a failed transition ends the run: in the experiments, a service that exits as soon as it is
told to stop kept failing new connections for up to about 40 s after its rollout had finished.
After a deploy that failed this way, those failures would fall into the rollback's window and be
counted against the rollback. The cost is that after a failed deploy the rollback is tested only
in the next run.

### What a run writes

A run that passed:

```
rollproof: PASS   orders (Helm) on Kubernetes v1.37.0 in kind

releases    previous   orders 1.4.2, oci://registry.example/charts/orders; chart defaults
            candidate  orders 1.5.0, ./chart; values: inline (image)
images      orders     run by deployment/orders, in pods the deploy created
probe       built-in HTTP: GET http://orders.orders.svc/api/orders/latest
            200 a second (one check every 5 ms), timeout 2 s, status 200-299
install     settled after 15.0 s under the probe; failures not judged
            3,000 checks, 6 failed (refused 5, timeout 1), the last 14.9 s before the steady window
            slowest 44 ms, behind 9 ms

window     length   checks   failed   after finish   slowest   behind   result
steady     10.0 s    2,000        0              -     41 ms     6 ms   pass
deploy     33.4 s    6,680        0              0    212 ms    38 ms   pass
rollback   33.9 s    6,780        0              0    187 ms    35 ms   pass

transition  workload            probed   pods replaced   quickest stop
deploy      deployment/orders   yes      2 of 2          5.3 s
rollback    deployment/orders   yes      2 of 2          5.2 s

exercised
  deploy      the candidate's start-up and the previous release's shutdown
  rollback    the previous release's start-up and the candidate's shutdown
not exercised
  the delay before a larger cluster stops routing to a stopping pod
  connections held open, and requests in flight on a pod after new connections left it
  the path through an ingress, a load balancer or a mesh
  routing that depends on where the client is
  capacity while pods are down, and autoscaling
  storage that attaches to one node at a time
  latency below the timeout
  data migrations
  a node drain

results: /tmp/rollproof-results-7f3a
```

A run that failed:

```
rollproof: FAIL   orders (Helm) on Kubernetes v1.37.0 in kind

releases    previous   orders 1.4.2, oci://registry.example/charts/orders; chart defaults
            candidate  orders 1.5.0, ./chart; values: inline (image)
images      orders     run by deployment/orders, in pods the deploy created
probe       built-in HTTP: GET http://orders.orders.svc/api/orders/latest
            200 a second (one check every 5 ms), timeout 2 s, status 200-299
install     settled after 15.0 s under the probe; failures not judged
            3,000 checks, 6 failed (refused 5, timeout 1), the last 14.9 s before the steady window
            slowest 44 ms, behind 9 ms

window     length   checks   failed   after finish   slowest   behind   result
steady     10.0 s    2,000        0              -     39 ms     5 ms   pass
deploy     29.1 s    5,820      243             31    198 ms    12 ms   FAIL
rollback        -        -        -              -         -        -   not run

transition  workload            probed   pods replaced   quickest stop
deploy      deployment/orders   yes      2 of 2          0.8 s

deploy: 243 checks failed (refused 198, timeout 45), first at 10:00:07.31   [probe-failures]
  stopping then   orders-6d5f9c-x2k4q (previous release), told to stop 0.3 s before
  starting then   none
results: /tmp/rollproof-results-7f3a
```

A run that could not test:

```
rollproof: NOT VERIFIED   orders (Helm) on Kubernetes v1.37.0 in kind

releases    previous   orders 1.4.2, oci://registry.example/charts/orders; chart defaults
            candidate  orders 1.5.0, ./chart; values: inline (image)

could not test: nothing to watch   [nothing-watched]
  15 s after the install command returned, namespace orders held no Deployment, StatefulSet
  or DaemonSet that matches the selector app=order
results: /tmp/rollproof-results-7f3a
```

- **Console.** The summaries above.
  - The first line gives the verdict, a name, the driver and the Kubernetes version. The name is
    the Helm release's, or, for the commands driver, the workload's namespace.
  - A run prints the lines for what it reached. One that ended before the tool started the probe
    has no probe lines and no tables. One that ended before the probe's first observation has the
    probe's lines, and no install lines and no tables. One that ended in the install window, or
    whose install window decided the verdict before the steady window began, has the install's
    lines and no tables. One whose install window was judged late has the first table too, with
    the windows that ran reported as not judged. When the install had not settled when the run
    ended, the first line says so in place of the time it took, and the others give the counts up
    to that moment and the time of the last failure.
  - The lines for the releases give, for the Helm driver, each chart's name, the version that
    Helm resolved and where it came from, and the names of the values files and the top-level
    keys of the inline maps. The commands driver gives the commands' first words.
  - The lines for the images say, for each entry of `images`, which watched workloads had pods
    that ran its digest and which transition created those pods, or "run by no watched pod".
  - The probe's lines give its target and its settings. Every configuration value that differs
    from its default is printed with the default beside it, so that a weakened test shows: a
    setting of the probe, a limit, the number of workers, an image of the cluster. A custom
    probe is named by its image digest and the names of its `env` entries, with the line "a
    custom probe judged this run; the tool does not know what it checks".
  - The install's lines follow the probe's. They say how long the install took to settle under
    the probe, counted from the probe's first observation, and what the probe reported in the
    install window: the checks, the failures by class, how long before the steady window the last
    failure came, the slowest check that passed and how far the probe ran behind its schedule.
    The first line ends with NOT VERIFIED when the install window decided the verdict, on time or
    late, and otherwise with "failures not judged". When the settle time
    started again, they say what started it.
  - The first table has a row for every window but the install window, whose lines come before
    it. Its result is one of pass, FAIL, NOT VERIFIED, not run and not judged. "After finish"
    counts the failures that fell in the settle time.
    "Slowest" is the slowest check that passed, and "behind" the furthest that the probe ran
    behind its schedule, both as the probe reported them. When failures crossed the start of a
    transition (section 6), a line under the table says how many.
  - The second table lists every watched workload for every transition that ran. "Probed" says
    whether the workload's pods are behind the Service that the built-in probe's URL names: the
    tool reads that Service's endpoints and their owners at the start and at the end of each
    transition. With a custom probe, and with a URL whose host names no Service of the cluster,
    the column reads "not known". A workload of which no pod was
    replaced reads "not replaced", and one that the transition created or deleted reads "added"
    or "removed". "Quickest stop" is the figure of section 7: the old pod of that workload that
    was gone soonest served for at most that long after it was told to stop.
  - Every reason is printed with its code in brackets.
  - On probe failures, the lines under the count name the watched pods that were stopping
    (deleted, not gone) or starting (created, not ready) at the moment of the first failure, and
    the release each belongs to. They say what was happening and name no cause.
  - On `steady-not-clean`, when no check has passed since the probe started, a line under the
    count says so, and for how long. The target has then passed no check: every check failed,
    whether refused, answered outside the range or not answered. That points at the installed
    release or at what the probe was aimed at, and not at a failure that came and went.
  - On a stall, the console lists each watched workload that is not complete, with its counts,
    and each pod that is not ready: its phase, why it is not scheduled, the state of its
    containers and how each last ended. Those come from the pod's status. The last event message
    follows them; Kubernetes keeps events as best effort, and for one hour by default. When a pod
    could not be scheduled, the text names the cluster's shape and `cluster.workers`. When the
    settle time started again, the text says what started it. When the command was still running
    and the tool stopped it, the text says so. A probe that never started is shown the same way,
    by the state of its pod.
  - Every pass ends with the lists "exercised" and "not exercised" (see "What every report
    states").
  - The probe's lines also say how many of its lines were JSON objects with no `ok` field, that
    the server's certificate was not verified when the URL is `https`, that a Service sends one
    client to one pod when it does, and that the probe did not stop when asked when it did not
    (section 6), or why the tool did not ask it. The console names each variable that the tool took out of its commands'
    environment (section 7), and prints every warning.
- **`result.json`.** The same facts for machines. It is the stable contract for wrappers. It
  holds:
  - a schema version, and whether the file is final;
  - `verdict`: `pass`, `fail` or `not-verified`, the strings of the action's output;
  - the reasons. Each has a `code` from the table below, the `verdict` that the code stands for,
    the `window` it was placed in, if any, which is `install` for the install window, and a
    text. One reason is marked as the one that decided: of the reasons that carry the verdict in
    the window that decided, the install window included, or of all the reasons when the run
    ended before the probe's first observation, the one that comes first in the table below;
  - every window but the install window: its name (`steady`, `deploy`, `rollback` or
    `restart`), its start and its end,
    and its result, which is one of `pass`, `fail`, `not-verified`, `not-run` and `not-judged`.
    With them its counts: checks, failures, failures after the transition finished, failures by
    class, failures that crossed its start, and the time of the first failure. And the slowest
    check that passed and the furthest lag that the probe reported;
  - the install: when it was ready, when the probe's first observation was written, when it had
    settled, and what started its settle time again, if anything did. With them the counts of the
    install window: checks, failures, failures by class, the time of the last failure, the
    slowest check that passed and the furthest lag that the probe reported. They count every
    observation whose time is before the install window or falls in it, a failure whose line was
    written after it included. The install window is reported here and not in the list of windows, so a wrapper
    that adds up the windows' failures does not count these. The steady window's start is the
    moment the install had settled; the time of the probe's first observation is here. A run
    that ended before the install was ready has no install record. One that ended before the
    probe's first observation has no time of it and no counts, and one that ended before the
    install had settled has no time of settling;
  - every transition: whether it stalled, how its command ended, and what started the settle time
    again, if anything did. For each watched workload: the pods at the start, the pods replaced,
    whether it was added or removed, whether it is probed, and the quickest stop;
  - the probes the tool started, as a list that has one entry in the first release. Each has
    its settings and their defaults, the names of its headers or of its `env` entries in place of
    their values, the counts of its `command` and `args`, how many lines it wrote and how many of
    them were JSON objects with no `ok` field, whether it ran without a certificate check, and
    whether it stopped when asked. When the tool did not ask it to stop, because a signal stopped
    the run, or did not finish asking, because the tool failed while it stopped the probe,
    `stopped_when_asked` is null, the counts are those of the lines read until then, and a note
    says which (section 14, point 27);
  - for the Helm driver, for each release: the chart's name, the version that Helm resolved,
    where the chart came from, and the names of the values files and the top-level keys of the
    inline maps that the tool passed, and never a value. Helm keeps the merged values and not
    where they came from, so this is the tool's own record. The commands driver gives the first
    word of each command;
  - the limits and the cluster's settings, each with its default;
  - every entry of `images`: its digest, and which watched workloads had pods that ran it, by
    transition;
  - the names of the variables that the tool took out of its commands' environment;
  - the warnings;
  - the tool's version;
  - the cluster: its provider, which is `kind`, the provider's version, the Kubernetes version,
    the node image digest and the number of workers.

  Within a schema version, from the first release that carries it, nothing is removed and
  nothing changes its meaning; fields and reason codes are added. A wrapper that meets a code it
  does not know goes by that reason's `verdict`. The list of what a pass did not exercise is text
  for people: `result.json` carries the facts behind it and no codes for it.

  **When it is written.** The tool writes a first `result.json` as soon as it has the results
  folder: `not-verified`, with the reason `internal-error` and the text "the run did not
  finish", and marked as not final. With that first file it replaces the `evidence` folder, and
  it writes the evidence as the run goes. It replaces `result.json` in one step when the verdict
  exists, before it completes the evidence and before it deletes the cluster. When a warning
  arises after that, it replaces the file once more, with the same verdict. A run that is killed
  therefore leaves a file that says it did not finish, and never an earlier run's pass. A rejected
  configuration gets a final file too. A wrapper treats a missing file, or one that is not final,
  as not verified.

  **On a signal.** SIGINT or SIGTERM stops the run at once. The tool stops a command that is
  running (section 7), writes the final `result.json` and the outputs first, and then completes
  what evidence it can. It leaves the cluster, with the probe's pod in it, for the next run to
  delete (section 9). A cancelled step has about 10 seconds, which is
  not enough to delete a cluster.

  A signal that comes while the tool is stopping the probe, after the last window has ended, is
  `interrupted` too. Results that the probe had still to write may be lost, so the run is not
  verified, unless that window had already failed. A signal that comes after the verdict was
  decided changes nothing and is a warning (rule 10).

  When it has written its results and completed its evidence, the tool ends by the signal that
  came, so that the shell learns of it as of any program that a user interrupts: the status is
  130 for SIGINT and 143 for SIGTERM, and a script that ran the tool stops too. The verdict is in
  `result.json`. A second signal ends the tool at once. Evidence that it had still to complete is
  then missing, and a `result.json` that it had not yet replaced says that the run did not
  finish (section 14, point 27).
- **GitHub job summary.** The same tables and lists as Markdown, written only when the tool
  detects GitHub Actions.
- **Evidence folder.**
  - The probe's raw lines; the window boundaries; a per-second timeline that puts failure counts
    beside pod events (created, ready, deleted, gone).
  - The events of the workload's namespace and of the probe's, which stay after their pod is
    gone, for the one hour that the API server keeps an event by default.
  - The pod lists before and after each transition, and the logs of every pod of a watched
    workload, old and new. The tool follows each pod's log while the pod lives, because a pod's
    log cannot be read once the pod is gone, and it adds the previous run of a restarted
    container where the kubelet still holds it.
  - The status of the probe's pod, and the logs of every container in it.
  - The commands the tool ran, with their output.
  - When set-up failed: kind's own logs, and the log of the run's registry. kind deletes the
    nodes of a cluster that failed to come up, so the tool has it keep them, takes their logs,
    and then deletes them itself.

  The folder's layout is not a contract.

### Reasons

| Code | Verdict | When |
| --- | --- | --- |
| `probe-failures` | fail | The probe reported a failure in a transition's window |
| `transition-stalled` | fail | A transition did not finish inside `limits.timeout` |
| `pod-unschedulable` | fail | Listed beside `transition-stalled` when a pod of a watched workload could not be scheduled. The stall may be the test cluster's size and not the manifests (section 9) |
| `command-failed` | fail | The command for a deploy or a rollback exited non-zero, or could not be started |
| `config-rejected` | not verified | The configuration was rejected |
| `runner-unsupported` | not verified | The runner cannot run the tool: it is not Linux; it has no container engine that kind supports, on the tool's own host and in its network namespace, that may run privileged containers; it lacks `helm` for the Helm driver; the tool's binary is not statically linked; or the nodes' clock differs from the runner's |
| `cluster-failed` | not verified | The cluster or the run's registry could not be set up, or an image could not be stored or pulled into the nodes |
| `chart-fetch-failed` | not verified | A chart could not be fetched during set-up |
| `setup-failed` | not verified | A `setup` command exited non-zero, could not be started, or ran past `limits.timeout` |
| `install-failed` | not verified | The install exited non-zero or could not be started, ran past `limits.timeout`, left a release that was not ready when that limit passed, or left one that stopped being ready after the limit, while the install still settled (section 7) |
| `nothing-watched` | not verified | `limits.settle` after the install command returned, no Deployment, StatefulSet or DaemonSet matched `deploy.namespace` and `deploy.selector` |
| `workload-unsupported` | not verified | A watched StatefulSet or DaemonSet has an update strategy other than the rolling one |
| `target-unsupported` | not verified | The built-in probe's URL names a Service that does not exist, is headless, or routes only within a node, or it names one pod of a headless Service |
| `probe-not-started` | not verified | No observation arrived inside `limits.timeout`, and the probe was still running |
| `probe-exited` | not verified | The probe exited before the tool stopped it, whether before its first observation or in a window |
| `probe-line-invalid` | not verified | A line broke the probe contract: its form, a field's type or value, a `t` later than the line, or an observation behind a prefix |
| `steady-not-clean` | not verified | The probe reported a failure in the steady window. A failure in the install window is not one |
| `probe-silent` | not verified | The probe wrote no observation for longer than `limits.silence` |
| `no-pod-replaced` | not verified | A transition finished, its command succeeded, and it replaced no pod |
| `restart-failed` | not verified | The API server refused the change by which the tool restarts a workload |
| `interrupted` | not verified | A signal stopped the run |
| `internal-error` | not verified | The tool itself failed, the run did not finish, or the tool could not follow the probe's log without a gap |

### What every report states

- Which probe judged the run, with its target and its settings. A custom probe is named by its
  image digest.
- Which transitions were tested, and what each exercised. A deploy tests the candidate's start-up
  and the previous release's shutdown. A rollback tests the previous release's start-up and the
  candidate's shutdown. A restart tests both, of the one release. A run with a deploy and no
  rollback says that the candidate's shutdown was not exercised, nor the installed release's
  start-up, and a restart-only run cannot be mistaken for a deploy and a rollback.
- Which workloads had pods replaced, which had none, and, when the built-in probe judged the run,
  which of them the probe reaches. A transition in which no probed workload had a pod replaced
  says so under "exercised", in place of the start-up and the shutdown it would name: the probe
  then watched a rollout that it did not reach.
- On a pass, what was not exercised.

A fail and a not verified carry the tables for the windows and transitions that ran, and no
lists of what was exercised.

The list of what a pass did not exercise is fixed, and each line has a fact behind it:

| Not exercised | Why |
| --- | --- |
| The delay before a larger cluster stops routing to a stopping pod | The test cluster applies the change within about a second (section 1) |
| Connections held open, and requests in flight on a pod after new connections left it | The built-in probe opens a new connection for every request (section 6) |
| The path through an ingress, a load balancer or a mesh | The built-in probe enters at the Service. Those components learn of a pod change on a path of their own |
| Routing that depends on where the client is | The probe runs on one node, which has no zone (section 9) |
| Capacity while pods are down, and autoscaling | The probe's load needs no capacity, and an autoscaler has no metrics here |
| Storage that attaches to one node at a time | A volume here is a folder on a node, and a new pod shares it (section 9) |
| Latency below the timeout | A request that answers inside `probe.http.timeout` is a pass. The slowest one is reported |
| Data migrations | The tool runs none and checks none |
| A node drain | A drain evicts pods under a disruption budget, and a rolling update is not limited by one. It is another transition |

The second, third and seventh lines state facts of the built-in probe. With a custom probe one
line stands in their place: "whatever the custom probe does not check".

A line leaves the list when a fixture shows the tool catching what it names.

### Limits

- **No diagnosis.** The report says which pods were starting or stopping when the first failure
  came, and the timeline shows the rest. The tool does not guess the cause.
- **Workload logs are the workload's own text.** The tool masks registry passwords everywhere it
  writes, the pod logs it copies included, and nothing else in them. The caller decides whether
  to upload the evidence.

## 11. Security

- **The change under test runs with the job's power.** The configuration file is a program. Its
  `setup` commands and the commands of the commands driver run on the runner with the job's whole
  environment, the registry password included. The chart, its hooks, the images and a custom
  probe run as pods in nodes that kind runs as privileged containers of the runner's engine, and
  nothing in the tool keeps a pod under test away from its node. The tool therefore belongs only
  where everything the change brings is trusted, or on a runner that is thrown away after the
  job. A workflow must not run it on a pull request's code with the base repository's secrets.
  GitHub's own guidance warns against self-hosted runners on public repositories for this reason.
- **What leaves the runner through the tool.** The nodes' pulls, to the hosts and mirrors that
  the configuration lists, with the credentials of their entries. Helm's pulls, with the caller's
  own logins. Nothing else.
- A registry password arrives only through an environment variable. It is masked, in its plain
  and its encoded form, in everything the tool writes. Nothing else is masked (section 8). This
  rule has not had a security review (section 14).
- The credentials file mounted into the nodes holds only the hosts the configuration lists. It
  lives in the run's folder and is removed with the cluster. While the cluster lives, every pod
  in it can read the file (section 9), so the credentials should be able to read only, and
  should expire soon.
- A registry entry's credentials are offered to that entry's mirror as well as to its host, and
  to the real host when a pull goes on to it (section 9). A caller lists a mirror only when the
  mirror may see them.
- The built-in probe does not verify a server's certificate. It sends nothing secret, unless the
  caller put a credential into a header, and a header's value is not masked.
- The tool does not read, copy or change the runner's Helm logins, its container engine's logins
  or its kubeconfig.
- Every command the tool starts gets the test cluster's kubeconfig, and none of the variables by
  which Helm would act on another cluster or keep its records elsewhere (section 7). A candidate
  under test must not reach a cluster that the job's environment names for a later step.

## 12. How the project tests itself

**Without a cluster.**

- Table tests for every route through `verdict`: each rule of section 10, each pair of conditions
  in one window, the install window with a failure in it and with each condition that makes it
  not verified, on time and placed late, the decision to go on after each window, a late
  failure, a signal and a failure of the tool in each phase of a run, and the exit code of each
  verdict.
- Configuration validation: unknown keys, a missing or unknown `version`, unset and empty
  variables, an unknown `${…}` form, an image name that `images` does not list, a password
  written in the file, a combination of releases or commands that section 7 rejects, both or
  neither of a pair that needs exactly one, a `version` beside a chart path that exists, a chart
  version that is a number, a comma in a values file's path, the limits that section 8 holds
  against each other, a reserved namespace, a host listed twice, and the base of relative paths.
- Probe lines, on a fake clock: the type and the range of each field, an observation behind a
  prefix, a cut last line, an unknown `v`, assignment to windows, a failure that crosses the
  start of a transition, silence at a window's start and at its end, the start timeout, a probe
  that exits before its first observation, and what counts during the drain. And the install
  window: observations that are counted and not judged, one whose time is before it, a failure
  that began there and was written in the steady window, and silence and a fault of the probe
  there.
- The built-in probe against a server in the same process: the rate held while the handler is
  slow, and the lag it reports; each failure class; a status outside the range; a redirect; a
  body cut short; the summary line; the headers and the `Host` as the server received them;
  `https` with a certificate that nobody signed; a host that does not resolve at first and then
  does; and what it does on SIGTERM.
- The rules of `watch`, on recorded API objects and a fake clock: when a transition has finished,
  with a command still running and with pods created and deleted inside it; the settle time and
  what starts it again; when the install has settled, with a first observation before and after
  the moment the install's conditions last came to hold, with a pod that stops being ready, with
  the last watched workload removed, with a workload of a strategy other than the rolling one
  that appears, and with a break after the limit;
  pods counted by UID; a workload added, removed and renamed; nothing to
  watch; a strategy other than the rolling one; a Deployment past its progress deadline; the
  quickest stop, over pods that were ready; which workloads are behind a Service.
- The target check: each form of host, a headless Service, a pod's name, a node-local policy, a
  Service that does not exist.
- Golden files for the report formats, one for each verdict and one for each ending of a run
  that the console list names: before the probe's first observation, in the install window, and
  with the install window deciding the verdict. Golden files, too, for the kind configuration and
  node settings the tool generates. Among them a host that has `as`, a mirror, a `ca` and
  credentials together.
- The deploy drivers against a fake command runner that records the arguments, the environment
  and the folder: the flags of section 7, that the variables it lists are gone, how each ending
  of a command maps to a reason, and a command stopped at the limit.
- Masking: a password planted in every input, a command's output and a pod's log included, and
  then searched for, in both forms, in everything the tool wrote.
- The command line, in a child process whose run is a fake: SIGINT and SIGTERM during a run end
  the process by that signal after the run has returned, and a second signal ends it at once.
- `result.json`: the first file, its replacement, a rejected configuration, a signal, a results
  folder that holds an earlier run, and the install record of a run that ended before the install
  was ready, before the probe's first observation, before the install had settled, and after.
- Names: the cluster's name from the file's path, `--name`, and that a cluster without the prefix
  is never deleted.
- The checks that raise a reason outside a window, each against a fake: every check of the
  runner, the clock check, a node that cannot pull a stored image, a chart that cannot be
  fetched, and a restart that the API server refuses.

**With a cluster.** The real binary runs against a fixture service. The fixture is a small Go HTTP
service, copied from the experiments' test service, with switches for start delay and shutdown
behaviour, and a chart whose values produce each variant. The previous release and the candidate
are two images of it. They differ in content, by a version string built into each: one build
under two tags has one digest, and a deploy from it to itself would replace no pod. The suite
asserts the verdict and the set of reason codes. The two rows with a probe that fails its first
seconds also assert the install record, because they prove that a failed check in the install
window is counted and decides nothing.

| Fixture | Expected |
| --- | --- |
| Both releases report ready only when they can serve, and after SIGTERM serve on and then drain | Pass |
| The candidate has no readiness probe | Fail, in the deploy window; the rollback is not run |
| The candidate exits on SIGTERM | Fail, in the rollback window |
| No node is free for the extra pod | Fail, the deploy stalls; `pod-unschedulable` is listed beside the stall |
| The previous release exits on SIGTERM | Fail, in the deploy window; the rollback is not run |
| A custom probe stops writing during the deploy and never writes again | Not verified |
| A custom probe exits during the deploy | Not verified |
| The deploy replaces no pod | Not verified |
| The previous release never becomes ready | Not verified |
| A `setup` command fails | Not verified |
| No previous release is named | Pass, reported as restart only |
| Both releases serve for 5 s after SIGTERM and then end without draining | Pass. A known miss of the built-in probe (section 6) |
| A second rollout starts 5 s after the tool saw the deploy's first one finish | Pass; the deploy's window covers both rollouts, and `result.json` says what started the settle time again |
| The chart denies traffic by default, and the test's values let the probe in | Pass |
| A custom probe, the TCP loop of section 6 pointed at the fixture's own Service and port, judges the first fixture | Pass |
| The same custom probe reports a failed check about 500 times a second for its first 5 s, and only then checks the Service | Pass; the install record holds at least 2,000 failures, the last of them at least 9 s before the steady window, and neither the steady window nor a transition's holds one |
| The same, with failed checks for its first 20 s | Not verified, `steady-not-clean`; the failures from 15 s after its first observation on fall in the steady window |
| The same custom probe judges a candidate with no readiness probe | Fail, in the deploy window |
| The workload is a StatefulSet behind a Service with a cluster address | Pass |
| The workload is a DaemonSet behind a Service with a cluster address | Pass |
| The first fixture through the commands driver, with all three commands | Pass |
| The same with `install` and `deploy` only | Pass, reported as a deploy with no rollback |
| The chart has a hook after the install and one before the upgrade, each a Job that runs longer than the settle time | Pass; no window closes while Helm runs |
| The candidate's pods serve at first and then fail requests for some seconds, beginning after the rollout has finished | Fail, in the deploy window, counted under "after finish" |
| A value breaks the candidate chart's schema | Fail, `command-failed` |
| The run gets SIGINT during the deploy | Not verified, `interrupted`; `result.json` is final, the cluster stays for the next run, and the tool ends by SIGINT |
| A custom probe ignores SIGTERM | Pass; the report says that the probe did not stop |
| The candidate changes a second workload only, which the probe does not reach | Pass; the probed workload reads "not replaced", and the deploy's line under "exercised" says so |
| A `setup` command makes the probe's namespace enforce the `restricted` level | Pass |
| The previous release's pods report ready, and its handler fails every request | Not verified, `steady-not-clean`, within a minute of the install; the text says that no check has passed since the probe started |

The first row is the fixture that the other rows change one thing of. This document calls it the
first fixture.

The two rows with a probe that fails its first seconds put those failures in the probe, not in
the target. A release that fails its first seconds after it reported ready fails them again when
the rollback starts it, and failures tied to the install alone would need a timer whose right
value depends on how fast the pods and the probe start. A probe's failures begin with its first
observation, as the install window does. The two rows hold the start of the steady window
between 5 and 20 s after that observation, with thousands of observations in the install window,
so that a reader of the probe's lines that starts late shows too. The first fixture alone cannot
show it: the moment the cluster takes to route to a new Service varies, and a run may meet none.

Where a row names no reason code, the set it expects is the one code of its cause in the table of
reasons: `probe-failures` for a fail in a window, `probe-silent` for the probe that stops writing,
`probe-exited` for the one that exits, and `no-pod-replaced`, `install-failed` and `setup-failed`
for the three rows after it.

The third row is why the rollback leg exists. A candidate that shuts down badly passes its own
deploy, because the pods that stop during the deploy belong to the previous release. The fifth row
is the case that rule 1 of section 10 was written for.

The twelfth row is kept so that the limit is tested and stated, and not found by a user. Its
expected verdict is reasoned from how the cluster routes; no run has shown it yet. If it ever
fails, the limit has moved and section 6 is out of date.

In the thirteenth row the suite's harness starts the second rollout, with a restart, when the
tool writes its mark for the first finish into the evidence. Counted from `kubectl rollout
status` instead, the second rollout could begin before the settle time does, and the row would
pass without the rule it is there for.

The rows with a probe that goes silent, exits or ignores SIGTERM use small custom probes, because
the built-in probe has no switch for any of that and must not get one. Four rows use the TCP loop:
in two a custom probe decides the verdict of a real transition, and two prove the install window.
Every other row uses the built-in probe, and every row but the StatefulSet's and the DaemonSet's is
a Deployment.

**The suite's matrix.** The suite runs on Docker, on amd64, with the default node image, once
under Helm 3 and once under Helm 4. The first fixture alone also runs on the oldest node image
that kind lists for the linked release. A release promises what the suite ran on (section 9).

**The mirror job.** A registry inside the job stands for an organization's mirror. It needs a
login, it sits under a path of its host, and it serves TLS from a private authority. The previous
release's image is named after a public host and reaches the nodes only through that mirror. The
candidate's enters through `images`, with `as` on the same host, so the job covers a host that
has `as`, a mirror, a `ca` and credentials together. Once the mirror is filled, the job cuts the
route from the nodes and the engine to public registries, so a pull that goes past the mirror
fails. This is the proof of the mirror, certificate and credential settings. The job ends with
the search for the password, in both forms, in everything the run wrote.

**The action.** A workflow uses the action from its own repository, on the first fixture and on
one that fails. It builds the tool's image in the job, pushes it to a registry container there,
and passes it as `image` with its digest, so that the code under test runs and not the last
release. It asserts the `verdict` and `out` outputs and the job summary for both runs.

**The false-failure bound.** The project states its bound, from repeated runs of the first
fixture, and its catch counts for the broken ones, as the second experiment did. A false failure
is a run of the first fixture that ends other than pass; the statement gives the fails and the
not verified apart. It carries its date, the versions and the runner's size it was measured with.
The default rate rests on a runner with 2 CPUs, so the bound is measured on one of that size. It
is the bound of that fixture on that runner: a small service whose handler sleeps. It is measured
again when something that affects it changes: the built-in probe, a timing default, the fixture,
the linked kind release, the Helm major, or the runner's image or size. A release in between
carries the last statement forward. The first release carries a statement. How many runs is set
when the project has a public repository, and every series waits for the maintainer's word.

Four more measurements go with it. Each setting runs the same number of times as the others, and
that number is set with the series.

- **How long a pod must serve here.** The first fixture runs with its time of serving after
  SIGTERM at 0, 0.5, 1 and 2 s, and the statement gives the shortest setting that passed every
  time. That is the margin this cluster demands; section 1 says why a larger cluster demands
  more.
- **A start that costs CPU.** One cell whose start-up uses a full CPU for some seconds, so that a
  workload that is heavy at its start is in the picture before the README quotes a bound. It
  reports the false failures and the furthest that the probe ran behind.
- **The settle time.** The fixture whose pods fail after they became ready, with that delay
  varied, gives the time from the tool's own finish to the first failure (section 14, point 7).
  Every run of the series also gives how long before the steady window the probe's last failed
  check came, which sizes the install's use of the same time.
- **A large image.** The fixture padded to about 1 GB (section 14, point 16).

## 13. Layout, incubation and graduation

```
projects/rollproof/
  README.md  LICENSE  action.yml  go.mod
  cmd/rollproof/
  internal/   config cluster images deploy watch probe run verdict report
  fixtures/   service/ chart/
  e2e/
  docs/       specs/ plans/ probe-contract.md
```

The project incubates in a host repository, under `projects/rollproof/`, laid out as its own
future repository root. The host repository's decision log records the incubation in a row
proposed together with this spec. The host's own rules do not apply inside the directory, and the
project's files refer to none of them.

During incubation:

- The Go module is named `rollproof`, with no host, until the repository exists. All packages are
  internal, so the rename is mechanical.
- `OWNER` and `COMMIT` in this document stand for the future repository's owner and for a commit
  of that repository.
- The project's workflows sit in the host repository's root workflow folder as `rollproof-*.yml`,
  limited to the project's paths, because GitHub runs workflows only from there. Unit tests run on
  every pull request that touches the project. The cluster suite, the mirror job and the action's
  own test run on manual trigger only, because the host repository's minutes are billed.
- No series of repeated runs is started on GitHub without the maintainer's word.

Graduation:

- **When.** The cluster suite, the mirror job and the action's own test are green on GitHub.
- **How.** Split the directory with its history into a new public repository, move the workflows,
  and cut a first release with its image and binaries. Each step waits for the maintainer's word.
- **Before.** Check the name for collisions, and have the security review of section 14,
  point 13.

### Build order

The design is more than one implementation plan. It is built in this order, and each step from
the fourth on gets a plan of its own. The first plan covers steps 1 to 3.

1. **The host repository.** The decision row, the matching lines in the host's two rule files,
   the change to the host's citation check so that it skips `projects/`, and this spec.
2. **Proofs.** Open points 1 and 8 to 12 of section 14, and what point 5 says of the command
   lines in each Helm major, as throwaway experiments or readings. The other open points are
   proved where they are needed: points 2 and 19 with step 5, points 3, 4 and 20 with step 6, the
   fixture runs of point 5 with steps 3 and 4, and each fact of point 21 with the step that
   builds on it. Step 4 builds the fixture for point 7, and the measurements of points 7 and 16
   come with the fixture series in step 7. A proof that needs a run on GitHub Actions waits for
   the maintainer's word.
3. **A walking skeleton.** `config`, `cluster`, `images` for local images and the probe's own,
   `run`, the Helm driver, the built-in HTTP probe, `watch`, `verdict`, the console report and
   `result.json`. The tool
   creates its own kind cluster and registry on the developer's machine. The first four fixtures
   of section 12 give their expected verdicts. The workflow for the unit tests comes with this
   step.
4. **The rest of the two contracts.** The custom probe, the commands driver, the `setup` commands,
   restart-only runs, and the remaining fixtures.
5. **Enterprise settings.** `registries`, `as` in `images`, and the mirror job.
6. **The action**, the job summary and the remaining workflows.
7. **Graduation**, and in the new repository the fixture series of section 12, before the first
   release. The series settles points 7 and 16.

Step 3 was built on 2026-11-17 with the built-in probe's warm-up, which this text no longer has.
A plan of its own brought the skeleton to this text on 2026-11-18, before step 4: first the rule
for when the install has settled, then the install window, in the same step as the built-in
probe's changes: no warm-up, a schedule that starts when the probe starts, and a failed lookup as
a failed check. Then the counts and the report, and last the nodes' setting of section 9 for an
attempt to connect that had no answer (section 14, points 23 and 24).

## 14. Open points for the plan

The plan settles each open point with a test or a reading before anything is built on what it
decides. Points 6, 17 and 18 are settled and stay here for the record. Points 7 and 16 are settled
by the fixture runs, and points 13 and 14 before the first release. Point 15 is a caveat with
nothing to settle, point 22 records two rules that stand for now, and point 23 records why the
install settles under the probe and what that leaves open. The build order of section 13 says
which step proves which point.

1. **Registry order in the nodes.** That a node tries the run's registry first for a host and then
   falls back to the real registry is what the container runtime's documentation and source say
   (section 16), but it has not been run for this design. First task of the plan. The same task
   proves three more things:
   - that a reference by digest is served by the run's registry when its node entry may serve
     pulls and may not resolve tags, so that a tag is never answered by the run's registry;
   - that a reference with a tag and a digest is pulled by the digest, as the runtime's source
     says;
   - what a mirror under a path needs. The runtime puts the registry API's path after a mirror's
     path. A cache that wants its project name inside the API path needs the runtime's path
     override, and `mirror` then needs a way to say which of the two it names.

   It also settles two things that section 9 needs: the one list of places for a host that has
   `as`, a mirror and a `ca` together; and how the tool has a node pull an image during set-up,
   so that the node uses the registry settings it uses for the kubelet.
2. **Mirrors that need credentials.** Read in source and not run (section 9): an entry's
   credentials reach its mirror, and credentials listed under the mirror's own host are never
   used. The task confirms both with a run. It also finds the mechanism for a mirror whose
   credentials differ from the original host's: the container runtime's own setting per host,
   which its documentation calls deprecated, or a header in the mirror's node entry. Credentials
   that a credential-provider plugin of the kubelet supplies were not read. An authenticated
   mirror is the usual enterprise case. The same task decides whether a caller may forbid the
   last step from a mirror to the real host: while a mirror has no credentials of its own, a
   caller lists them under the real host, and a pull that goes on to that host offers them there.
3. **Environment variables into a composite action.** Whether `env:` on the step that calls a
   composite action reaches the action's inner steps was not found in the pages read. The plan
   settles it with a run. If it does not, the first way out is `env:` at the job's level, which
   needs no new input. An input that carries names and values is the last resort: it is the
   string encoding that the design of section 8 avoids.
4. **Outputs of a step that fails.** Whether the outputs a step wrote are kept when that step then
   exits non-zero, and whether a composite action passes them on, was not checked. The `verdict`
   output depends on it.
5. **Helm's two majors.** GitHub's hosted images carry Helm: 3.22 on the image that `ubuntu-latest`
   names today, and 4.3 on the next one, on x64 and arm64 alike (read twice on 2026-11-16,
   section 16). A job on `ubuntu-latest`
   therefore changes Helm major when that label moves, so the first release is tested with both.
   The command lines of section 7 exist in both with the same meaning. Helm 4 differs in what they
   do: it applies objects on the server by default, a rollback included, and such an apply fails
   when another writer owns a field that the manifest sets, for example a replica count that an
   autoscaler wrote. Its flag for rolling back on failure has another name, and its `--wait` takes
   a value. The plan runs the fixtures under each major and looks for such a conflict.
6. **A Helm rollback with no revision.** Settled by a reading (section 16). With no revision Helm
   goes back to the highest revision less one, whatever its state. That is the install only when
   the deploy stored a revision. The tool names the revision, as section 7 says.
7. **The settle default.** 15 s is a first value. The experiments do not size it: counted from
   the tool's own finish, which waits for old pods to be gone, they may hold no failure that only
   the settle time catches. The fixture whose pods fail some seconds after they became ready, run
   with that delay varied, confirms the default or changes it (section 12). The install settles
   for the same time (section 7), and nothing sizes the default for that use either. Every run
   that reaches its steady window records how long before it the last failed check came, and the
   fixture series reads that figure too.
8. **kind as a library.** kind's project calls itself alpha grade, has not committed to its
   library surface, and has no compatibility plan between node images and kind releases. The plan
   confirms that the linked release creates a cluster through the Docker and nerdctl providers,
   with the node settings section 9 needs.
9. **Which kind release to link**, and the node images kind lists for it. The tool promises those
   of them that its suite ran on (section 12).
10. **The rollout rules.** Whether the Go package that holds the rules of `kubectl rollout status`
    can be imported. Copied rules have to be read again at every Kubernetes minor.
11. **A pod's log across rotation.** The kubelet rotates a container's log at 10 MiB by default.
    Whether following the log through the API survives a rotation was not checked. A custom probe
    that prints a line for every check at a high rate reaches that size within minutes. The tool
    follows the log of every watched pod too (section 10), so the answer matters for both. The
    same task settles how a stream that ended early is opened again with no line lost and none
    counted twice. The log API takes a start time in whole seconds, so the tool has to match
    lines by the runtime's own timestamps.
12. **Keys on a rootless engine.** How many kernel keys one cluster uses through a run without the
    setting of section 9. The experiments met the quota with four clusters at once. If one cluster
    stays well under it, the setting leaves the tool.
13. **Masking, and what leaves the runner.** The rule of section 8 needs a security reviewer's eye
    before the first release. So do four facts of section 11: the change under test runs with the
    job's power, every pod in the cluster can read the nodes' credentials, a registry entry's
    credentials reach its mirror, and the value of a probe header is not masked.
14. **The name.** The probe's namespace and label carry it (section 6). So do the prefix of every
    cluster's name and the registry host `rollproof.test` (section 9), the command itself, the
    default file name `rollproof.yaml`, and the names of the workflow files (section 13).
15. **Prior art.** The search could not read GitHub Marketplace in full text and did not open
    several tools. Finding an existing tool later would change the project's reason to exist, not
    this design.
16. **A large image.** The experiments' image was 5.5 MB. Every node keeps its own copy of an
    image, and kind turns off the kubelet's clean-up of images and its eviction on a full disk,
    so a full disk is not recovered. A hosted runner has a 14 GB disk. The tool pulls the listed
    images into the nodes during set-up (section 9), so the first pull of the candidate no longer
    falls inside the deploy's window, and a disk that fills up shows as a failed set-up. A run
    with the fixture padded to about 1 GB measures the disk and the time that set-up takes. It is
    measured with the fixture series, not among the proofs of the build order's second step.
17. **Four changes to the Helm driver.** Settled. A reading of Helm's source on 2026-11-16 showed
    where the driver, as section 7 then described it, went wrong, and the maintainer accepted
    the four changes that day. Section 7 carries them:
    - the deploy passes `--reset-values`;
    - the tool passes `limits.timeout` to Helm as its `--timeout`;
    - the tool names the test cluster on Helm's command line and takes out of the environment
      the variables that override it. The same variables are taken from the `setup` commands and
      from the commands driver, which may run Helm too. That last step was the drafting
      session's own, and the maintainer accepted it later that day, with the third review;
    - the configuration rejects a `version` beside a chart path that exists, and a comma in a
      values file's path.
18. **A probe on the runner.** Settled. This design first said that the runner cannot reach a
    Service of a kind cluster. A reading on 2026-11-16 showed that to hold for a Service's
    cluster address only (section 15). With that in front of them, the maintainer confirmed the
    same day that such a probe stays out of the first release. The reasons are the size of the
    contract and the one path per kind of engine.
19. **A run behind a proxy.** Read and not run: kind exempts the cluster network's subnets from
    the proxy, and the tool names its registry by address (section 9). A run with the proxy
    variables set shows whether the nodes then pull from the run's registry.
20. **Signals.** GitHub sends a cancel signal to the first process of a step. The action starts
    the tool with `exec`, so that the tool is that process (section 8). Whether a shell would
    pass the signal on to a child was not read. The fixture with a signal covers the tool; a run
    of the action that is cancelled covers the action.
21. **Facts that the third review's changes rest on and that nobody read in a source.**
    - That a shell which runs as the first process of a container sets no handler for SIGTERM.
      The kernel's rule for such a process was read; the shell's side was not. The fixture with
      a probe that ignores SIGTERM passes either way.
    - That a pod's status names the digest of the image each container runs. The report's lines
      for `images` depend on it.
    - That an engine in a virtual machine, as Docker Desktop is on Linux, gives the nodes a clock
      of their own, and what bound the clock check needs.
    - That a privileged node lets a pod under test reach the runner. Section 11 assumes that it
      does.
22. **Two rules taken tentatively.** The maintainer accepted these on 2026-11-16 with the word
    "tentatively". They stand until a fixture or a user's case argues otherwise.
    - A signal that comes while the tool is stopping the probe makes the run not verified, unless
      a window had already failed (section 10). The other way would keep the verdict and print a
      warning.
    - A probe that exits before its first observation is `probe-exited`, at once (section 6). The
      other way would wait out `limits.timeout` and report `probe-not-started`.
23. **The install settles under the probe, a revision of 2026-11-17.** The first plan's
    end-to-end runs found that kube-proxy on the probe's node routed a Service that the install
    had just created about a second after its endpoints were ready. The probe's first requests
    were refused, and the steady window was not verified under both Helm majors. An amendment of
    that day gave the built-in probe a warm-up: it wrote no observation until its target had
    answered without a failure for one second. The maintainer accepted it "for now". It had four
    costs. Nothing reported what the warm-up met. A target that never answered waited out
    `limits.timeout` and was `probe-not-started`, which points at the probe. A custom probe got
    nothing. And one probe knew when judging starts, which the contract says no probe does.

    Two AI agents on a Fable model then each sketched three designs in which no probe warms up,
    one from the run's timeline and one from the install. The maintainer chose this one the same
    day. The install settles under the running probe, a failed check in that time is counted and
    not judged, and the steady window begins when the install has settled
    (sections 4, 6, 7 and 10). It takes away the one exception that section 7 had, an install
    with no settle time, and it adds no key and no reason code. What lost, and why:
    - **A steady window that begins after the last early failure, inside a limit.** The probe's
      own results would then choose where judging starts. A previous release that fails now and
      then would get a clean steady window more often, and its failures would later count
      against the candidate.
    - **A probe that starts only when the install has settled.** It costs the same time, and
      nothing measures that time, so no run can show how much of it was needed. The first
      moments of the probe's own pod stay in the steady window.
    - **A container that runs before the probe, in the probe's pod, and dials the target until
      it answers.** It is the warm-up in another place, and it serves only a probe that names a
      TCP target.
    - **The probe's start judged in `verdict` and not in `probe`.** It can be added to any of
      the others. Nobody would see a difference, and it can come with the custom probe.
    - **A shorter time between kube-proxy's syncs in the test cluster,** which was looked at and
      not sketched. The delay would be shorter and not gone. It is also believed, and not read
      in a source, that the same delay is what lets a transition's window catch a pod that stops
      while the cluster still routes to it.

    Two reviews of the revision followed the same day, one a careful reading and one a critic's,
    by two AI agents on a DeepSeek model. The drafting session checked each finding against the
    text and the sources, and the text carries the findings that held. The maintainer made five
    rulings on them:
    - The time from the probe's first observation until the install has settled is the install
      window. The rules of section 10 treat it as a window, so silence, a fault of the probe, a
      signal, a failure of the tool and a condition placed late follow the rules every window
      follows, and a failed check in it is counted and decides nothing. The report keeps it apart
      from the list of windows. The other way, a window in that list like the others, would have
      given `result.json` a window whose failures do not count, a result of its own, and a trap
      for a wrapper that adds up the windows' failures.
    - Silence is measured from the probe's first observation, in the install window too. A probe
      that stops writing there and then writes again is silent, as one that exits there has a
      fault.
    - A host that the built-in probe cannot resolve yet is a failed check of class `other`, and
      the probe looks it up again. It no longer ends the probe (section 6).
    - An install whose conditions break while it settles and do not hold again keeps the limit of
      section 7: the run waits until `limits.timeout` has passed and is then `install-failed`.
      Ending the wait once the conditions have not held for `limits.settle` would fail a pod that
      restarts once and then serves.
    - Two rows of section 12 prove the install window with a custom probe that fails its first
      seconds, because failures that the target causes at the install come back in the rollback.

    What the revision rests on, and what it leaves open:
    - Nothing had been run with it when it was written. Point 24 has its first runs.
    - Every run that reaches its steady window is at least `limits.settle` longer: 15 s at the
      default, on a job that took about 4.5 minutes in the experiments (section 3). The key now
      also sizes the install's settle time, and nobody has sized it for that (point 7).
    - It is not known that the probe's own pod, in a cluster with no mesh, meets no failure of
      its own that lasts longer than the settle time. The first fixture, run with no warm-up,
      shows whether it does. Its first runs met failures after the settle time from another
      cause, the attempts that the nodes kept (point 24).
    - The install's settle time starts again when a pod stops being ready, and a transition's
      does not (section 7). The chosen design says so, and the maintainer has not ruled on the
      difference. The other way is one rule for both.
    - Two answers are the chosen design's own, and the maintainer did not rule on them
      separately: the install settles for `limits.settle` and has no key of its own, and a
      failed check in the install window never changes a verdict.
    - An installed release that fails only in the first seconds after it reported ready is not
      seen at the install. Its start-up is judged in the rollback's window, or in the restart's
      when no previous release is named. A commands run with `install` and `deploy` and no
      `rollback` starts the installed release only once, and judges its start-up in no window.
    - A run that passes may now print failed checks, on a line that says "failures not judged".
    - In `result.json` the steady window's start is no longer the time of the probe's first
      observation. No release carries the schema yet, so its version stays: a version keeps its
      meaning from the first release that carries it (section 10).
    - The first plan's built-in probe exited when its host could not be resolved at its start.
      The next plan changed that to the failed check that section 6 now describes.
    - The report does not say how many ready endpoints the probe's target had when the steady
      window began. It would sharpen the text for a target that passed no check.

24. **Attempts that the nodes kept, a finding of 2026-11-18.** In the revision's first
    end-to-end runs, seven of the nine runs that reached the probe had checks that timed out
    long after its first seconds, at single moments from 23 s to 102 s after its first
    observation. Five ended `steady-not-clean`, and a fixture that should stall ended
    `probe-failures`. The plan stopped there, as it says it must. A copy of the first plan's
    code with no warm-up and a steady window of 60 s had such timeouts in 5 of 6 runs, and the
    same copy with its warm-up, in the same hour, in none of 6. So they came from the probe's
    start, not from the revision's other changes.

    Two captures of the nodes' connection tracking then showed the cause. kind's kube-proxy
    runs in iptables mode. While it brings in a new Service's rules, in the first 0.7 to 2 s
    after the probe's first observation, a few of the probe's attempts leave the node without
    being sent to a pod. The kernel keeps each one, with that route, as an attempt with no
    answer, for 120 s. The built-in probe opens a connection for each request, 200 a second,
    and it used a source port again about 3,000 times a minute, a median of 18 to 22 s after
    its last use. In the second capture all seven late timeouts made no new entry, and at each
    of them the only entries alive and not closed were the attempts kept from the first
    seconds, on ports beside the one in use. The fixture that should stall is believed to have
    met the same: its five timeouts came 44 to 102 s after the probe's first observation, all
    within 120 s of the attempts kept in its first second, and no capture was made of it. The
    first plan's warm-up is believed to have hidden all of this: it sent one check at a time
    while the Service was not yet routed, and it wrote none of them.

    The maintainer chose the same day to set, on each node, the time that the kernel keeps such
    an attempt to 5 s (section 9). The same copy with no warm-up and that setting had no
    failure after the first second in 6 of 6 runs of 60 s; without it, 5 of 6 had one. What
    lost, and why:
    - **Deleting the kept attempts when the install has settled.** The tool would act on the
      nodes in the middle of a run, at a moment of its choosing. The setting acts once, when
      the cluster is made.
    - **A probe that starts only when the Service routes.** It is point 23's warm-up, moved
      into the tool, with the same costs.
    - **The warm-up again.** Point 23's four costs come back.
    - **An install window longer than 120 s.** Every run would be two minutes longer.

    What the setting rests on, and what it leaves open:
    - 5 s is the one value tried. The kept attempts were made in the first 2 s after the
      probe's first observation, so they are gone about 7 s after it: inside the install window
      at the default `limits.settle` of 15 s. A `limits.settle` shorter than that, which section
      8 allows down to just above `limits.silence`, may leave one for the steady window.
    - The test cluster differs from a node with Linux's defaults in this value. An attempt that
      uses a port again more than 5 s after an attempt from it that had no answer is routed
      afresh here, where a node with the default would send it the old way for up to 120 s.
      The tool does not show such a failure. It is believed that few clients open connections
      from one address as often as the built-in probe does.
    - A connection whose first answer takes longer than 5 s is believed to wait for the
      kernel's next attempt, about 7 s after the first. Nothing in the fixtures does that.
    - The value was set on nodes of rootless nerdctl only. Docker and Podman are not tried.
    - With the setting and the revision's code together, the nine runs of the five fixtures
      that reached the probe had no late timeout, and every failure of the probe's start came
      in its first 2 s. In one run every check of the first 0.7 s timed out, 145 of them, and
      all fell in the install window. In one run of the suite, eight of the ten runs gave their
      expected verdicts. The other two gave theirs when they were run again:
      - One had ended `internal-error` in its rollback window, after the probe had stopped as
        asked: the tool opened the probe's log again and found it removed with the deleted
        pod. That is a fault of the tool at the end of any run, in code from before the
        revision, and the next plan has to remove it.
      - The other had ended `cluster-failed`. Its `limits.timeout` of 90 s also bounds the
        creation of the cluster, which took longer on the workstation in two of three runs
        that day.

      After the plan's final review and its fixes, the whole suite passed in one run, ten of
      ten.

25. **The checks before the install window, a ruling of 2026-11-18.** The next plan's final
    review found that the install's counts missed the probe's first checks. The install window
    opens when the first observation's line was written, and an observation is placed by its
    time, when its check began. The first observation's own check therefore always began before
    the window, and so did every check that the probe's start met before that line: the runs had
    six refused checks in the probe's log and five in `result.json`. A start whose first checks
    hang, as in point 24, could leave a whole request timeout of them out of every count, and the
    report would say less than the log. The maintainer chose that such a check is counted in the
    install window (sections 4, 6 and 10). The window's start does not move, so silence, the
    settle time and the steady window's start are as before, and no verdict changes, because the
    install window judges no failure. What lost: **the same window, with the number of checks
    counted in no window written into the install record.** It would report the gap instead of
    closing it, and a reader would have to add two numbers to learn what the start met.

26. **The end of the probe's log, and the limits of the cluster's creation, rulings of
    2026-11-18.** Point 24 left two faults for the next plan.
    - **The end of the log.** The tool stops the probe by deleting its pod. The log's stream then
      ends, but for a moment the pod still reads the container as running, so the tool opened the
      log again and found it removed with the pod. The run was not verified, once in about twenty
      runs that day. Point 11's proof found that a stream ends only after the container has
      ended, through a rotation of the log too; only the pod's status comes later. The maintainer
      chose that, after the stop was asked, the tool waits until the pod reads the container as
      ended, and that the stream's end is then the end of the log (section 6). A stream that the
      API server ended for another reason while the container was ending reads as the end, as it
      already did when the pod read the container as ended at once. What lost: **a failed opening
      with the kubelet's text for a removed log, taken as the end**, because the kubelet does not
      promise that text; and **a stop that does not delete the pod**, because the API has no call
      that sends a signal, and a `kill` run in the container needs a shell that an image need not
      have.
    - **The limits of the cluster's creation.** Section 8 bounds each wait of the cluster's
      creation by `limits.timeout` on its own, but the code counted one deadline from before kind
      created the nodes, so a slow creation used up the waits for the registry and for the nodes'
      readiness. The code now follows section 8. A creation that hangs can therefore take kind's
      own time and twice `limits.timeout` before it fails. What lost: **a limit of its own for the
      cluster's creation**, a new setting for a fault of the code; and **a larger
      `limits.timeout` in the fixture that met it**, which would hide the fault.

27. **A signal, and a failure of the tool, rulings of 2026-11-18.** The next plan's reviews found
    three things the design did not say, and the maintainer asked whether the tool treats Ctrl-C
    as the convention does: as the user's interrupt.
    - **The exit after a signal.** The tool stops at once, writes the final `result.json` and the
      outputs first, and reports the run as not verified, `interrupted`, which blames neither the
      workload nor the probe. It exited 2, as any run that is not verified. A program that
      catches SIGINT to tidy up is expected to end by that signal afterwards, so that the shell
      learns of it. In a trial on the workstation, a bash script whose child exited 2 after
      SIGINT went on to its next line, and one whose child ended by the signal stopped. The tool
      now ends by the signal, and a second signal ends it at once, as the convention also has it
      (section 10). What lost: **exit 2 after a signal**, which keeps one rule for the exit code
      but lets a script go on after the user stopped it.
    - **The probe's record after a signal.** The tool does not ask the probe to stop after a
      signal, and `result.json` said that the probe had written no line and had stopped when
      asked. It now gives the lines read until then, `stopped_when_asked` null, and a note. The
      same holds after a failure of the tool while it stopped the probe, which the report had
      blamed on the probe (section 10). No release carries schema 1 yet, so the field's change
      breaks no wrapper. What lost: **`false` with a note**, which a reader takes for a probe
      that ignored SIGTERM; and **counts of zero, documented**.
    - **Commands after a failure of the tool.** The tool stopped the probe, and only then a
      command that was still running, which went on changing the cluster for up to 15 seconds.
      It now stops the command first (section 7).

## 15. Not in the first release

- A rollback that interrupts a half-finished deploy.
- A Helm deploy with no rollback, and a restart after a rollback.
- Canary and blue-green controllers, and their custom resources.
- Pods whose chain of controllers does not lead to a Deployment, a StatefulSet or a DaemonSet: an
  operator that creates its pods itself, or a controller with a workload resource of its own.
- A StatefulSet or a DaemonSet with an update strategy other than the rolling one.
- A deployer that begins its change later than the settle time after the transition looked
  finished, such as a controller that polls a Git repository.
- Traffic through an ingress as a built-in probe. A custom probe can do it.
- gRPC as a built-in probe.
- A stream of held connections in the built-in probe. It would see what the probe misses today
  (section 6). It waits for a measurement: on HTTP/1.1 a server that shuts down correctly may
  close an idle connection just as a client sends a request on it, so such a stream can fail
  against a correct server. The experiments' kept-alive client showed nothing either way: its
  library sends such a request again, and no variant ended with connections open.
- A headless Service, one pod of it, or a Service whose traffic policy is local to the node, as
  the built-in probe's target. The tool detects each (section 6).
- A certificate check, a client certificate, or a method other than GET in the built-in probe.
- A rule that makes a run not verified when no pod behind the probed Service was replaced. The
  report shows it (section 7).
- A setting that makes the test cluster slower to stop routing to a stopping pod. The proxy's
  shortest time between syncs looks, from its source, to delay only a change that follows another
  closely, so it would not give a delay that can be set. That is a reading, not a run.
- An image stored under a tag in the run's registry (section 9).
- A runner that is an unprivileged pod, a job that runs inside a container, and a container engine
  on another machine. For a job inside a container the sources show a way that nobody has run:
  the runner mounts the engine's socket into the job's container, and kind can give the cluster's
  internal address on its own network.
- Node labels from the configuration, other than the zone label the tool sets.
- A probe that runs on the runner. From the runner the way in depends on the engine (read, not
  run; section 16):
  - A Service's cluster address is reachable only from within the cluster, with every engine.
  - Where the engine is Docker run as root on Linux, as on GitHub's hosted runners, the runner
    reaches every node at its address on the cluster's network, and so a Service of type
    NodePort. That traffic passes through the node's proxy to any ready endpoint, so it follows a
    rollout.
  - With Docker Desktop, or with Docker run without root, the node addresses cannot be reached.
    The documents of Podman and nerdctl imply the same for their rootless modes. The way in is
    then a port mapping from the runner to one node, which kind sets when it creates the node.
  - A port-forward is tied to one pod and ends when that pod is replaced, so a probe through one
    would report failures that the workload did not cause.

  Such a probe would therefore need a Service of type NodePort from the chart or from the tool,
  and one path for each kind of engine.
- More than one probe in a run.
- A failure budget, and thresholds of other kinds: a share of checks, a longest outage, a latency.
- `junit.xml`. It needs a case for set-up, a case for every reason of section 10, and a suite name
  before anything depends on its names. Until then `result.json` is the one contract for machines.
- Cluster tools other than kind, and an existing cluster.
- Systems other than Linux.
- A table of Kubernetes versions inside the tool.
- Workloads of one release in more than one namespace.
- Copying an image from another registry into the run's registry, and Helm logins made by the
  tool.
- Masking of any value other than a registry password.
- Database migrations.
- A reusable workflow on top of the action.
- A tested fully offline mode.
- A guess at the cause of a failure.
- Extra Helm flags per transition. A chart that needs them uses the commands driver.
- A switch that lets a transition which replaced no pod pass.
- A rule that makes a run not verified when no watched pod ran a listed image. The report shows
  it (section 9).
- A rule that makes a failed window not verified because the probe ran far behind its schedule.
  The report gives the lag for every window.
- A verdict of not verified for a stall that the test cluster's size caused. It is a fail with a
  reason of its own (section 10).
- Environment variables that hand each image's reference to the caller's commands. `${images.…}`
  in a command's arguments does that.
- A mirror that speaks plain HTTP.
- A chart that brings the Namespace object of its own release.

## 16. Sources

Paths in the first table are from the host repository's root. Those files stay behind at
graduation.

| Source | What this design takes from it |
| --- | --- |
| `RESEARCH/2026-11-12/L5-kind-rollout.md` | The experiment on a workstation: the variants, the two-signal gate, the effect of the client's connections, the failures that went on after a rollout had finished (its section 5, item 6), the time a cluster takes to delete, the test service and the load generator that the fixture and the built-in probe are copied from, the kernel key quota on a rootless engine |
| `RESEARCH/2026-11-12/L6-kind-rollout-on-actions.md` | The 160 jobs on hosted runners: the false-failure bound, the job time, 200 requests a second on 2 CPUs, the digest trials and the registry's set-up time, the slowest successful request |
| `RESEARCH/2026-11-12/W16-shift-left-rollout-confidence.md` | That `kubectl rollout status` does not wait for terminating pods, and the share of failed requests that began after it returned (section 7 of that report) |
| `RESEARCH/2026-11-13/W18-rollout-test-prior-art.md` | The prior-art search and its limits |

Pages read on 2026-11-13 from local clones of the projects' documentation:

| Page | Clone | What was read |
| --- | --- | --- |
| GitHub Docs, "Reusing workflow configurations" (reference) | `github/docs` at `d59b263195` | Whole page: access to reusable workflows from GitHub Enterprise Server, their limits, the keywords a calling job may use |
| GitHub Docs, "Workflow dependency lockfiles" (concept) | the same | Whole page: what a lockfile pins and what it does not |
| GitHub Docs, "Metadata syntax" and "Contexts" (reference) | the same | Searched, not read whole: composite action steps, `github.action_ref` and `github.action_path` |
| GitHub Docs, "Reusing workflow configurations" (concept) | the same | Searched: the comparison of reusable workflows and composite actions |
| `gh-actions-lock`, README | `github/gh-actions-lock` at `90ae563` | First 90 lines: supported hosts and limitations |
| kind, "Private Registries" | `kubernetes-sigs/kind` at `1a24c920` | Whole page: pull secrets, a credentials file mounted into nodes, certificates |
| kind, "Quick Start", "Working Offline", "Local Registry" | the same | Searched, not read whole: proxy variables, node images by digest |
| kind, the Go package `sigs.k8s.io/kind/pkg/cluster` (its files `provider.go` and `createoption.go`) | the same | Searched: the library's entry points and its providers |

Pages the reviewing agent read on 2026-11-14 from the same clones, and from a clone of
`kubernetes/website` at `a96b3ebd15`. The drafting session then opened the first four again and
found what the review reported; it did not open the others.

| Page | What was read |
| --- | --- |
| kind, the Go package `sigs.k8s.io/kind/pkg/apis/config/defaults` | That kind's code holds one default node image, pinned by digest |
| Kubernetes documentation, the feature gate `PodLogsQuerySplitStreams` | That choosing one stream of a pod's log is an alpha feature, off by default |
| Kubernetes documentation, "Logging Architecture" | That a pod's log carries both streams, and the default size at which a container's log is rotated |
| GitHub Docs, "Workflow cancellation" (reference) | The time a cancelled step gets before its processes are killed |
| GitHub Docs, "Workflow commands" and "Metadata syntax" (reference) | That any step may write outputs and the job summary, and how a composite action maps its outputs |
| Kubernetes documentation, "Images" | That the kubelet matches a node's credentials against the name of the image |
| Kubernetes documentation, "Well-Known Labels, Annotations and Taints" | The annotation that a restart changes |
| kind, "Design Principles" and "1.0 Roadmap" | That kind calls itself alpha grade and has no compatibility plan between node images and releases |

Read on 2026-11-16 by two AI agents on an Opus model, which looked for evidence for and against
each answer to the second review. Local clones are the ones named above. The other sources were
fetched that day at the tag or commit given. The drafting session then opened about twenty of the
passages that decide a point, in the kubelet, kube-proxy, kubectl, the container runtime, Go, the
runner and the multi-zone job, and found them as reported. Nothing was run. The agents' reports,
which quote each passage, are not filed in the repository.

| Source | What this design takes from it |
| --- | --- |
| Kubernetes source at `v1.37.0`: kubectl's rollout status and restart | The conditions for a complete rollout of each kind, that an unchanged workload is complete at once, that no condition waits for terminating pods, that a strategy other than the rolling one gets no status, and the annotation a restart changes |
| The same: the API server's graceful deletion | That a pod's deletion timestamp is the request time plus the grace period, in whole seconds |
| The same: kube-proxy | That established TCP connections to a removed endpoint are left alone, that its shortest time between syncs is 1 s by default, and that a node with no zone ignores zone hints |
| The same: the kubelet and its credential keyring | That credentials are looked up by the image's own name and passed on with no host attached; that an image is pulled as the first step of starting a container |
| The same: the scheduler's spread and affinity plugins | How a node without the topology key is treated |
| containerd at `v2.3.4`: its registry-hosts document, its resolver and its CRI image pull | The order of hosts and the fall-back, that a tag needs a host that may resolve and a digest does not, that a missing tag falls through to the next host, where the API path goes after a mirror's path, and that credentials with no host attached are offered to every host |
| Go at `go1.27.4`: the HTTP client and server | When the client sends a request again on its own, and what a graceful shutdown does with idle and active connections |
| RFC 9112, section 9.5, and RFC 9110, section 9.2.2 | The race between a server that closes an idle connection and a client that sends on it |
| Kubernetes enhancement 3960, the sleep action for a preStop hook | That "sleep, then stop" is promoted for zero-downtime shutdown, and that it speaks of new connections only |
| Helm at `v3.22.1` and `v4.3.2` | That an upgrade returns before the rollout is complete unless told to wait, the flag names of each major, and that a release stores the merged values and not their sources. That Helm 3, and Helm 4 when it patches from the client, send nothing for an unchanged resource, while Helm 4's default applies every object on the server |
| Stakater Reloader at `v1.4.24`, and Argo CD's documentation at `v3.6.1` | That a reloader changes the pod template some time after a Secret changed; that a GitOps controller polls on an interval of minutes |
| GitHub's runner at `v2.338.2` | That the engine's socket is mounted into a job's container |
| The `kubernetes/test-infra` repository at `51bd8b7f`, the multi-zone job on kind | The cluster shape with a zone on each worker and none on the control-plane node |
| Istio's documentation at `167b0ad6` | How a pod is chosen for injection, the default of two retries, and the start-up order of proxy and application |
| Kubernetes documentation, at the clone named above: pod lifecycle, EndpointSlices, virtual IPs, network policies, StatefulSets, DaemonSet updates, Services, traffic policy, topology spread, topology-aware routing, probes, images, disruptions, Deployments, logging, access control | The facts behind sections 6, 7, 9 and 10 that those sections state without a source of their own |
| kind at `1a24c920`: its network plugin, its node and cluster settings, its guides on rootless engines and local registries | That its plugin enforces network policies, that nodes can carry labels, that the control-plane node keeps its taint in a cluster of several nodes, the one storage class, the API server's default address, and that image clean-up and disk eviction are off |
| GitHub Docs, at the clone named above: hosted runners, the runner controller, secrets | The sizes of hosted runners for public and private repositories, the 14 GB disk, the arm64 runners, the smallest runner's limits, the runner controller's container modes, and what is masked in a job log |
| fortio's and k6's documentation | That open-model load tools report how far they ran behind their schedule, as information |

Read later on 2026-11-16 by one AI agent on an Opus model, which checked what this design said of
Helm from memory, and two statements of section 6. It found two of them wrong, one of those in
part, and eight in need of a correction. This text carries the corrections of wording. Four
changes to what the tool does follow from the reading, and the maintainer accepted them that day
(section 14, point 17). The drafting session opened the passages that decide a point and found
them as reported. Nothing was run, and the agent's report is not filed in the repository.

| Source | What this design takes from it |
| --- | --- |
| Helm at `v3.22.1` and `v4.3.2`: the upgrade, install and rollback commands and their actions | That an install through an upgrade is revision 1 and each later change the highest revision plus one; that a rollback applies the stored manifest and the stored hooks of the revision it goes back to; what a rollback with no revision goes back to; that later values files win; that a path that exists is used and its version ignored; where repositories and logins are read; that every failure exits with the same code |
| The same: hooks and timeouts | That a hook that is a Job or a Pod is waited for without a wait flag, each under its own timeout; that the default timeout is 5 minutes and bounds one operation, not a command; that both flags for rolling back on failure turn waiting on |
| The same: how objects are applied | That Helm 4 applies on the server by default, a rollback included, and that such an apply fails on a field that another writer owns |
| GitHub's runner images, their software lists at `f40a752b` | Helm 3.22.1 on the Ubuntu 24.04 image, which `ubuntu-latest` names, and Helm 4.3.1 on the 26.04 image |
| containerd at `v2.4.2`: its log writer; the kubelet's log reader at `v1.37.0` | That a log line's time is the runtime's clock when it has read the whole line, and how a line over 16 KiB is stored and returned |
| kubectl at `v1.37.0`: port-forward | That a port-forward to a Service picks one pod at its start and ends when that pod ends |

Read next on 2026-11-16 by one AI agent on an Opus model, which took the eight statements that the
earlier readings had left unverified and that a reading can settle. It found one wrong: this
design said that the runner cannot reach a Service of a kind cluster, and that holds for a
Service's cluster address only (section 15). Three needed a correction and one an addition, and
this text carries them. The maintainer then confirmed that a probe on the runner stays out of the
first release (section 14, point 18). The addition widened the third change of section 14,
point 17. The drafting session opened the passages that decide a point and found them as
reported. Nothing was run, and the agent's report is not filed in the repository.

| Source | What this design takes from it |
| --- | --- |
| kind at `1a24c920`: "Configuration", "Known Issues", "Ingress", "LoadBalancer", "Rootless", and its Docker provider | That with Docker on Linux, without Docker Desktop, the host sends traffic to the node addresses with no port mapping; that Docker Desktop does not expose them; that a port mapping is an argument of the node container when it is created, and that kind's own guide uses one to reach a NodePort |
| Docker's documentation at `67007394`: port publishing, and the rootless mode's troubleshooting page | That every port of a container on a bridge network is reachable from the Docker host; that a container's address is not reachable from the host when the engine runs without root |
| Podman at `d8e8bb84` and nerdctl at `e192ef14`: their documents on rootless networking | That the containers' network is in a network namespace of its own. Neither says outright that a container's address cannot be reached from the host |
| Kubernetes documentation, at the clone named above: Services, virtual IPs | That every node listens on a node port and forwards to a ready endpoint, and that a cluster address is reachable only from within the cluster |
| Kubernetes source at `v1.37.0`: the defaults of a Service and of a container, the API server's options, the event recorders | That the external traffic policy defaults to all ready endpoints; that the default pull policy is taken from the tag; that an event carries no owner reference and is kept for one hour by default |
| The same: client-go's `clientcmd` and cli-runtime's configuration flags | That a value Helm passes on from its environment overrides the kubeconfig's own, field by field, and that a context the file does not hold is an error |
| Helm at `v3.22.1` and `v4.3.2`: its environment settings, its storage drivers, its registry transport, its HTTP getter and its chart downloader | Which variables set the cluster, the identity, the namespace and the place of the release records; that Helm sets no certificate authorities of its own unless it is given a file, and that a repository entry's certificate file is used for a chart named through its alias |
| Go at `go1.27.4`: `crypto/x509` and `crypto/tls` | That a client with no authorities set uses the host's, where they are read from on Linux, and that `SSL_CERT_FILE` and `SSL_CERT_DIR` replace those places |
| containerd at `v2.3.4`, the version in kind's node image: its CRI image pull and its log writer; `distribution/reference` at `v0.6.0` | That a reference with a tag and a digest is reduced to the digest before the pull; that the log writer takes the time when it has read a whole line, as was read before at `v2.4.2` |
| Linux man-pages 6.20, `time_namespaces(7)` | That a time namespace does not change the wall clock |
| local-path-provisioner at `v0.0.34`, and kind's manifest for it | That a volume is tied to one node by the host-name label |
| GitHub Docs, at the clone named above: workflow commands, job outputs, secrets, secret scanning | That secrets are masked in job logs, job summaries and job outputs, and that nothing is said of artifacts or of files a step writes |
| GitHub's runner images at `f40a752b`, read a second time | The same Helm versions as the first reading, on x64 and arm64; that the image's Docker is installed as a system service |

Read on 2026-11-16 by the eight AI agents of the third review, on an Opus model, for the findings
that rest on a fact. The drafting session opened again the passages marked so, and found them as
reported. Nothing was run, and the agents' reports are not filed in the repository.

| Source | What this design takes from it |
| --- | --- |
| Helm at `v3.22.1` and `v4.3.2`: the upgrade and install actions, the ownership check, the chart locator, the registry's version matching, the namespace setting | That an upgrade is refused while the release's last revision is pending, and that a release stays pending while the hooks after an install run; that the hooks before an upgrade run before anything is changed; that Helm refuses an object it did not create; that a chart path is taken relative to Helm's working folder; that an empty version means the newest chart; that Helm with no namespace given takes the kubeconfig context's. Opened again |
| kind at `1a24c920`: its Docker provider, its create and delete steps, its kubeconfig code, its wait for readiness, its validation of names, "Known Issues" | What kind adds to `NO_PROXY`; that the library writes each new cluster into the caller's kubeconfig unless it is given a file; that the nodes of a failed create are deleted unless kind is told to keep them; that the wait covers the control-plane node only. Those four were opened again. Also the characters of a cluster's name, that a cluster with no worker loses the control-plane node's taint, and the inotify limits |
| containerd at `v2.3.4`: its registry-hosts code, its resolver, its log writer | That one host has one settings file; that a mirror is asked for the repository without the host; that a `ca` is added to the system's authorities; that the real host is the last in the list; that what is left at the end of a stream is written as a partial line |
| Go at `go1.27.4`: the HTTP client. Go's proxy package, in a local `go1.27.3` | That a `Host` entry among a request's headers is not sent, opened again; that a subnet in `NO_PROXY` matches an address and never a name |
| Kubernetes source at `v1.37.0`: the log options of the API, the kubelet's log reader, kubectl's rollout status | That a log's start time is given in whole seconds; that a partial last line comes back without its line end; that kubectl reports an error for a Deployment past its progress deadline |
| Kubernetes documentation, at the clone named above: DNS for Services and Pods, virtual IPs, traffic policy, environment variables of a container, Deployments, Pod Security admission | That a single label resolves in the pod's own namespace; routing within a node and affinity by client address; that a container's `env` is a list; how the default surge and unavailable counts round; that no Pod Security level is enforced by default |
| GitHub Docs, at the clone named above: workflow cancellation, contexts, workflow syntax, secrets, the security of self-hosted runners | That a cancel signal goes to the first process of a step, and the times after it, opened again; that every step has an `outcome` of its own; that a secret that does not exist gives an empty string, and that a fork gets none; the advice against self-hosted runners on public repositories |
| Linux man-pages, `pid_namespaces(7)` | That the first process of a PID namespace gets only the signals for which it has set a handler |
| `sigs.k8s.io/yaml`, in the copy the reading used | That a number read into a string field is written in its shortest form, so `1.10` becomes `1.1` |

Seen and read on 2026-11-17, for the amendment and the revision of that day. The runs were the
first plan's own end-to-end suite on a workstation, with kind through rootless nerdctl, under
Helm 3 and under Helm 4. Their logs are not filed in the repository. The drafting session read
the kind file and the kubectl file itself.

| Source | What this design takes from it |
| --- | --- |
| The first plan's end-to-end runs | That the built-in probe, started as soon as the install was ready, had its first five checks refused, then one check that took about a second and one that timed out, and no failure after those |
| kind at `v0.33.0`: the kubeadm configuration it writes for a cluster | That kind sets kube-proxy's shortest time between syncs to 1 s in every cluster it creates |
| `k8s.io/kubectl` at `v0.37.2`: the rollout status of a Deployment, a StatefulSet and a DaemonSet | That a rollout is not complete while its controller has not yet seen the workload's latest spec, while fewer pods are updated than the spec asks for, and while an updated pod is not available or, for a StatefulSet, not ready. So a pod created, a spec changed and a pod that stops being ready each make the install's conditions not hold (section 7) |

Seen on 2026-11-18, for section 14, point 24. The runs were on the same workstation, with kind
through rootless nerdctl, under Helm 3 and under Helm 4. Their logs and captures are not filed in
the repository.

| Source | What this design takes from it |
| --- | --- |
| The revision's end-to-end runs, with the next plan's code before the setting | That seven of the nine runs that reached the probe had checks time out long after its first seconds, and that a fixture that should stall failed with `probe-failures` from five such timeouts |
| The first plan's code with no warm-up and a steady window of 60 s, three runs under both Helm majors, then three with the warm-up | That the timeouts came from the probe's start: 5 of 6 runs had them without the warm-up, none of 6 with it |
| Two captures of the nodes' connection tracking, from the moment the probe's Service existed | That a late timeout made no new entry, and that the only entries alive and not closed then were attempts kept with no answer from the first seconds, on ports beside the one in use; that the kernel kept them 120 s; how often the probe used a source port again |
| The same copy with no warm-up, the nodes' value set to 5 s, three runs under both Helm majors | That a node of rootless nerdctl takes the value, and that no failure came after the first second in 6 of 6 runs |
| The revision's end-to-end runs with the setting: the suite once, then its two failed fixtures again | That no late timeout came in the nine runs that reached the probe, and the two failures that section 14, point 24, names |

Still not shown by any run or any page: that 15 s is long enough for an install to settle under
the probe; that a node of Docker or Podman takes the setting of section 14, point 24; that a
shorter time between the proxy's syncs would hide failures that the fixtures catch. And, from
before the
revision: that a node which the tool asks to pull an image uses the
settings it uses for the kubelet; that nodes behind a proxy reach the run's registry by its
address; that a shell as the first process of a container ignores SIGTERM; that a pod's status
names the digest it ran; that Docker Desktop on Linux gives the nodes a clock of their own; that
a pod in a privileged node can reach the runner; that the fixture for the known miss passes; that
a job inside a container can reach the cluster; that a larger sync time of the proxy gives no
delay that can be set; the two rollouts under a reloader; how a mirror under a path behaves; that
Podman run as root lets the runner reach the node addresses as Docker does; that a port mapping
cannot be added to a node after it was created; which Go version Helm's released binaries are
built with; and where a pod goes when the node its volume is tied to cannot take it. That the
hosted runner's Docker runs as root is read from the image's install script, which no page
states. What is known of these is reasoned from the sources above or is the drafting session's
own knowledge, and section 14 lists those of them that need a test.
