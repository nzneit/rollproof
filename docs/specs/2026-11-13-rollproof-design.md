# rollproof: design

Date: 2026-11-13. Revised on 2026-11-16.

Status: the design below was agreed section by section in conversation on 2026-11-13. A review on
2026-11-14 proposed changes that make the public surface smaller and close gaps in the contracts.
The maintainer accepted them on 2026-11-16, and this text includes them. This written form waits
for the maintainer's review. No code exists yet.

An AI session on a Fable model drafted this document with the maintainer, and a second AI agent on
a Fable model reviewed it. Where a statement rests on a measurement or on a page that was read,
section 16 names the source. Everything else is design, not fact, and nothing here has been run.

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

A pass shows that, in a real Kubernetes control plane with real kubelets, the workload's manifests,
probes and shutdown behaviour kept the probe free of failures while every pod was replaced twice.

A pass in kind is necessary, not sufficient. A kind cluster has a handful of Services and applies
endpoint changes within about a second. A production cluster can take longer to stop routing to a
terminating pod. Nodes in kind are containers on one machine: there is no real network between
them, no real node capacity and no cloud load balancer. Every report repeats this in one line.

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
  (section 6).
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
  every user one local command that reproduces a CI failure.

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
- **The client decides what is caught.** A client that opened a new connection per request caught
  every broken variant in every run. A client that kept connections alive passed a service with no
  readiness probe in 15 of 15 runs.
- **Failures continue after the rollout reports done.** For the variants that failed, about half or
  more of the failed requests began after `kubectl rollout status` had returned. That command does
  not wait for old pods to finish terminating. For the variants whose process exits as soon as it
  is told to stop, requests on new connections kept timing out until the load ended, up to about
  40 s after the rollout had finished.
- **It fits a small runner.** On a 2-CPU hosted runner, 200 requests a second ran with no false
  failure in 150 jobs. That bounds the false-failure rate at 2.0 % per job (95 %), for that
  service, load and day. A job took about 4.5 minutes.
- **A digest reference needs a registry.** A pod whose image was referenced by digest did not start
  after either of kind's image-load commands. With a registry container in the job it started; the
  registry cost about 8 seconds to set up.

What the experiments did not cover, and this design adds: a change of version, a rollback, a
workload that is not their own test service, a chart, any registry that needs credentials, and
anything other than HTTP GET.

### Prior art

A web search on 2026-11-13 found no reusable tool, action or workflow that does this. It found four
small demonstration repositories that run the same loop once for their own sample application, and
Helm's chart-testing, whose upgrade test runs `helm test` after an upgrade and sends no traffic
during it. The search has gaps that its report lists; the verdict is the absence of a finding.

## 4. What one run does

One run is one cluster, one workload and one Kubernetes version. Testing several versions is the
caller's job matrix.

```
set up     cluster, registry, images, setup commands
install    previous release, wait until ready
probe on   | steady | deploy and settle | roll back and settle |
verdict    report, evidence, exit code
```

1. **Set up.** Create a kind cluster with several nodes, start a registry container, put the
   caller's images into it, and run the caller's `setup` commands.
2. **Install** the previous release and wait until it is ready.
3. **Start the probe** and hold a steady window before anything changes.
4. **Deploy the candidate.** Wait for the transition to finish, under a timeout, and keep the
   probe running for the settle time after it.
5. **Roll back** to the previous release, with the same wait and the same settle time. This step
   runs only when the deploy passed.
6. **Decide and report.**

### Windows

The time between the probe's start and its stop is divided into windows with no gap between them:
the steady window, then one window for each transition. A transition's window runs from the moment
the tool starts the transition until `limits.settle` after the transition has finished
(section 7). The settle time is part of the window because failures go on after a rollout reports
done (section 3).

### When a run passes

A run passes only when all four hold:

1. Every transition finished inside `limits.timeout`.
2. The probe reported no failure in any window.
3. The probe produced observations for every window, with no silence longer than `limits.silence`.
4. Every transition replaced at least one pod.

### Three outcomes

- **Pass.**
- **Fail.** Downtime was seen, a transition stalled, or the command that performs a deploy or a
  rollback failed.
- **Not verified.** The run could not judge: set-up broke, a `setup` command failed, the previous
  release never became ready, the steady window was not clean, the probe gave no verdict for a
  window, or a transition replaced no pod.

Not verified fails the job, as fail does. It is reported apart from fail, as "could not test", so
that nobody reads a broken test as downtime or a skipped test as a pass. Section 10 gives the
rules.

### A run with no previous release

A repository that adopts the tool before its second release has nothing to upgrade from. A
configuration that names no previous release gets a run that installs the candidate and restarts
it under the probe, which is what the experiments did. Nothing has to be switched on for this: the
transitions follow from what the configuration names (section 7). Every report of such a run says
that only a restart was tested.

## 5. Components

The tool is one Go binary. Each unit has one job.

| Unit | What it does | What it knows about |
| --- | --- | --- |
| `config` | Reads and validates the run description | Nothing else |
| `cluster` | Creates and deletes the kind cluster and the run's registry, and writes mirror, certificate and credential settings into the nodes | kind, the container engine |
| `images` | Puts the caller's images into the run's registry and returns their digests | The container engine, image archives |
| `deploy` | The deploy contract: install, deploy, roll back, restart. A Helm driver and a commands driver. It also runs the caller's `setup` commands, with the commands driver's runner | Helm, or the caller's commands |
| `watch` | Observes a transition: when it finished, whether it stalled, whether pods were replaced | The Kubernetes API |
| `probe` | The probe contract: start, stop, collect observations. The built-in HTTP probe and the custom probe | The probe's pod |
| `run` | Sequences the phases and marks the windows on the timeline | Only the two contracts, `cluster` and `watch` |
| `verdict` | Turns observations and transition results into pass, fail or not verified, with reasons | Nothing. It is a pure function |
| `report` | Writes the console text, `result.json`, the GitHub job summary and the evidence folder | File formats |

Flow of one run: `config`, then `cluster`, then `images`, then the `setup` commands, then `deploy`
installs. `probe` starts. For each transition `run` marks the window, `deploy` acts and `watch`
waits. `probe` stops, `verdict` decides, `report` writes.

Three properties the plan must keep:

- `run` never learns Helm or HTTP. It sees only the two contracts, so a new probe or a new deploy
  driver changes one unit.
- `verdict` does no I/O. Every route to each outcome is a table test that needs no cluster.
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
while true; do
  if nc -z -w1 orders-db 5432; then echo '{"ok":true}'
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
container. A stock image and a shell loop are therefore a complete probe, with no image to build.

A probe that runs on the runner is not in the first release. The runner cannot reach a Service of
a kind cluster directly, and a port-forward is tied to one pod and breaks when that pod is
replaced, so such a probe would report failures that the workload did not cause.

**Lines.** The probe writes one JSON object per line. The tool reads the pod's log, which carries
the container's standard output and standard error as one stream, so it does not matter which of
the two a line was written to.

| Field | Required | Meaning |
| --- | --- | --- |
| `ok` | yes | `true` when the check passed, `false` when it failed |
| `t` | no | When the check began, RFC 3339. Default: the time the line was written |
| `n` | no | How many identical checks this line stands for. Default 1 |
| `class` | no | A short label for the kind of failure, such as `refused` or `timeout` |
| `ms` | no | How long the check took, in milliseconds |
| `detail` | no | Free text for the evidence |

Fields are only ever added to this table. The tool ignores a field it does not know and keeps it
in the evidence. Every top-level name is reserved for the contract; a probe puts data of its own
into `detail`.

How a line is read:

- A line that does not start with `{` is log text. It is kept in the evidence and not counted.
- A line that starts with `{` must be a valid JSON object, or the run is not verified. A failure
  line with a formatting fault must not vanish.
- A valid object with no `ok` field is log text too, so a library that logs JSON does no harm. The
  report says how many such lines there were.
- An object whose `ok` is not `true` or `false`, or whose known field has the wrong type, makes
  the run not verified.

**Time.** The default time of a line is the timestamp the container runtime puts on it in the
pod's log. Putting the probe's lines and the tool's own window marks on one clock is the tool's
job, never the probe author's. With kind, the runner and the nodes share a clock.

An observation is assigned to the window that holds its time. One whose time is before the steady
window began, or after the last window ended, is not counted; the evidence keeps it. One whose `t`
is later than the moment its line was written makes the run not verified, because a check cannot
begin after its result.

**Lifecycle.**

- The tool starts the probe before the steady window. The probe counts as started when its first
  observation arrives. If none arrives within `limits.timeout`, the run is not verified.
- While the probe runs, a gap longer than `limits.silence` between the moments two successive
  lines were written makes the window it falls in not verified. A hung probe therefore cannot
  pass. Silence is measured on when lines were written, never on `t`, so a probe that reports
  late results is not silent.
- The tool stops the probe when the run's last window has ended, whether or not every transition
  was run. It sends SIGTERM and goes on reading the probe's lines until it exits, for at most 10
  seconds. A late result written in that time counts when its `t` falls inside a window. A probe
  that exits before the tool stops it makes the run not verified.
- The probe knows nothing of windows, transitions or thresholds.

**Where probe pods run.** In a namespace of the tool's own, on the control-plane node, so that
the worker nodes hold only the workload and the tool never watches a probe pod.

### The built-in HTTP probe

It speaks the same contract. It is the tool's own binary, started as `rollproof probe http`.

- **Open model.** Request *i* starts at start + *i*/rate, whatever earlier requests did.
- **Connections.** It opens a new connection for every request. In the experiments only such a
  client caught a service with no readiness probe; a client that kept connections alive passed it
  in 15 of 15 runs.
- **A failure** is a response outside the expected status range (default 200 to 299), a connection
  error, or no answer within the request timeout. The class is the status code, or one of
  `refused`, `reset`, `eof`, `timeout`, `other`.
- **Output.** One line per failed request, with `t`, `class` and `ms`, and one summary line per
  second for the requests that passed, with `n`.
- **Names.** It resolves the URL's host once at start and then dials that address, so cluster DNS
  is not on the path of every request. A Service's cluster address does not change during a
  rollout.

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
| Interface size | A namespace, and three Helm fields or three commands | A step grammar times each driver's actions | Two paths |
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

**Setup commands.** `setup` is a list of commands. Each is an argument list, and the tool runs it
the way the commands driver runs its own: directly, with no shell, on the runner, with
`KUBECONFIG` set to the test cluster. They run in order, after the cluster is ready and the images
are in the run's registry, and before the install. They are for what the workload needs and its
chart does not bring: a Secret, a custom resource definition, a database, a broker. Whatever they
create in `deploy.namespace` is watched like the workload, unless `deploy.selector` leaves it out,
so a dependency belongs in a namespace of its own.

**The Helm driver.**

| Transition | What the tool runs |
| --- | --- |
| install | `helm upgrade --install` of the release that the table above names, into `deploy.namespace`, with `--create-namespace` |
| deploy | `helm upgrade` to the candidate |
| rollback | `helm rollback` to the revision the tool read after the install |

A release description has `chart`, `version` and `values`.

- `chart` is a path, an `oci://` reference, or a name that the runner's Helm configuration
  resolves, such as a repository alias with a chart name.
- `values` is a list whose items are files or inline maps, applied in the order written. The tool
  writes each inline map to a values file of its own, so types are kept and no key needs a path
  grammar.

Helm runs with the runner's own configuration: its repositories and its registry logins are the
ones the caller set up before the step. The tool adds the cluster and the namespace, and nothing
else. The first release names the Helm major versions it is tested with (section 14).

Helm is called without `--wait` and without `--atomic`. `watch` alone judges when a transition has
finished, so both drivers are judged the same way. Helm hooks run as Helm runs them.

**The commands driver.** Each command is an argument list. The tool starts it directly, with no
shell, on the runner, with `KUBECONFIG` set to the test cluster. The command returns when the
change has been applied; whether it also waits does not matter, because the tool watches either
way. `install` is required. `deploy` and `rollback` are optional, as the table above shows.

**When a command fails.** For both drivers, a non-zero exit of a `setup` command or of the install
makes the run not verified, and a non-zero exit of a deploy or a rollback is a fail.

**The restart.** The tool performs it itself, the same way for both drivers: it changes the pod
template annotation that `kubectl rollout restart` changes, on every watched workload.

**What is watched.** The Deployments, StatefulSets and DaemonSets in `deploy.namespace`,
optionally narrowed by `deploy.selector`, a label selector. The rule is the same for both drivers.
A chart that places workloads in a second namespace has only the named one watched. The project's
own tests cover Deployments; StatefulSets and DaemonSets are watched and not tested in the first
release.

**When a transition has finished.** Both must hold:

- every watched workload reports its rollout complete, by the conditions `kubectl rollout status`
  uses for its kind;
- every pod of a watched workload that existed when the transition began and has been deleted is
  gone, not merely terminating.

The transition's window then stays open for `limits.settle`.

A transition that has not finished within `limits.timeout` has stalled. The same limit applies to
the install and to the probe's start.

**Pods replaced.** A transition replaced pods when at least one pod of a watched workload that
existed at its start is gone at its end and a new one has taken its place. A transition that
replaced none is not verified: it changed nothing the test can judge.

Inside `run`, a deploy driver is this (a sketch; the plan may adjust it):

```go
// Deployer performs the transitions of one workload.
type Deployer interface {
    Install(ctx context.Context) error
    Deploy(ctx context.Context) error
    Rollback(ctx context.Context) error
}
```

## 8. Configuration and the action

This is a public interface. The block under the next heading is copied unchanged into the
implementation plan.

### `internal/config` and `action.yml`

Common case: a repo's workflow builds its image, then runs the test with one step; a developer reproduces that run on a laptop with one command.

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
  http: { url: http://orders.orders.svc/healthz }

# the workflow
- run: docker build -t orders:candidate .
- uses: OWNER/rollproof@COMMIT
  env: { PREVIOUS_VERSION: 1.4.2 }

# a laptop, the same in bash and fish
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
    probe-url: http://orders.orders.svc/healthz

# a laptop
rollproof run --release orders --namespace orders --chart ./chart --previous-chart oci://registry.example/charts/orders --previous-version 1.4.2 --set image.digest=sha256:… --image orders:candidate --image-as registry.example/team/orders --probe-url http://orders.orders.svc/healthz
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
| Generality | The same file on a laptop, on GitHub and on any other CI | Shaped by GitHub; another CI needs its own mapping | Anything Go can express |
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
version: 1

cluster:
  node-image: …             # a full reference; default: the linked kind release's own
  registry-image: …         # the image of the run's registry
  workers: 3

registries:                 # section 9: what the nodes need in order to pull
  - host: registry.example
    username: ${env.REGISTRY_USER}
    password: ${env.REGISTRY_PASSWORD}
    ca: certs/corp-ca.pem
    mirror: mirror.corp.example/registry-example

images:                     # section 9: images that are not in a registry yet
  orders:
    local: orders:candidate # or: archive: PATH
    as: registry.example/team/orders

setup:                      # section 7: commands run before the install
  - [kubectl, apply, -f, test/prerequisites.yaml]

deploy:                     # section 7
  namespace: orders
  selector: app=orders      # optional
  helm: { … }               # or: commands: { … }

probe:                      # section 6
  http:
    url: http://orders.orders.svc/healthz
    rate: 200
    timeout: 2s
    expect-status: 200-299
  # or:
  # custom:
  #   image: registry.example/tools/busybox@sha256:…
  #   command: [sh, -c, "…"]
  #   args: []
  #   env: {}

limits:
  steady: 10s
  settle: 15s
  timeout: 5m               # the install, the probe's start and each transition
  silence: 5s
```

Exactly one of each pair must be present: `deploy.helm` or `deploy.commands`, `probe.http` or
`probe.custom`, and `local` or `archive` in each entry of `images`.

`expect-status` is one status code, or one range written with a hyphen, both ends included.

**Substitution.** It applies inside string values, never to keys. Two forms exist: `${env.NAME}`
for an environment variable, and `${images.NAME.digest}` or `${images.NAME.ref}` for an image the
`images` section put into the run's registry. Any other `${…}` is a configuration error, so a form
added later cannot change the meaning of a file that exists today. `$${` writes a literal `${`. A
variable that is not set is a configuration error, never an empty string.

**Secrets.** The one secret the tool handles is a registry password. `registries[].password` must
be a single `${env.NAME}`; a password written in the file is rejected. The tool masks the value of
every such password in everything it writes. The tool masks nothing else. A chart value or a
probe's environment value is written as it was given, so a caller who passes a real secret there
keeps the evidence to themselves.

### Defaults, and the reason for each

| Setting | Default | Reason |
| --- | --- | --- |
| `cluster.node-image` | The default node image of the linked kind release | kind pins it by digest and maintains it. A table of the tool's own would go stale with every kind release |
| `cluster.workers` | 3 | The experiments' shape. It leaves a node free for the extra pod of a two-replica workload that spreads by node |
| `probe.http.rate` | 200 a second | Measured to fit a 2-CPU hosted runner with no false failure in 150 jobs |
| `probe.http.timeout` | 2 s | The experiments' value; the slowest request that succeeded on a hosted runner took 247 ms |
| `probe.http.expect-status` | 200 to 299 | A redirect or an error page is not an answer from the workload |
| `limits.steady` | 10 s | The experiments restarted 10 s after the load began |
| `limits.settle` | 15 s | It has to catch the failures that begin only after a transition has finished. In the experiments those ended within about 3 s, in the variant with no readiness probe, and the tool also waits for old pods to be gone before the settle time starts. It does not have to outlast a failure that is already under way: one failure fails the window, and a failed window ends the run. 15 s is several times the 3 s; the fixture runs are to confirm it |
| `limits.timeout` | 5 minutes | Helm's own default wait, so a chart that deploys under Helm's default does not stall here. One limit covers the install, the probe's start and each transition |
| `limits.silence` | 5 s | The built-in probe writes at least one line a second, so 5 s is five missed lines. A custom probe that checks less often raises it |

There is no failure budget. A pass means that the probe reported no failure, which is the claim
the tool is named for. Thresholds of other kinds are not in the first release (section 15).

### The command line

- `rollproof run`, with `-f FILE`, `--out DIR`, `--name NAME` and `--keep-cluster`.
- `rollproof probe http`, which the probe pod runs. Users do not call it.
- `rollproof version`.

Exit codes of `run`: 0 for pass, 1 for fail, 2 for not verified. A configuration that is rejected
exits 2.

`--out` names the folder for the results. It holds `result.json` and a folder named `evidence`.
The default is a new folder under the system's temporary directory, and the console prints its
path.

### The action

`action.yml` is a composite action. It uses no other action, so an organization allows and syncs
one action, and a lockfile pins one.

| Input | Meaning | Default |
| --- | --- | --- |
| `config` | Path of the config file | `rollproof.yaml` |
| `image` | Where to pull the tool's own image from | The project's image, by digest, written into `action.yml` at each release |
| `out` | Folder for the results | a folder under the runner's temporary directory |

| Output | Meaning |
| --- | --- |
| `outcome` | `pass`, `fail` or `not-verified` |
| `out` | The folder that holds `result.json` and the evidence |

How the tool reaches the runner: the action pulls the tool's image by digest and copies the binary
out of it. Everything the tool needs from the network is then an image, so an organization mirrors
one kind of thing, and a digest names content, not a location. Each release also attaches plain
Linux binaries, for laptops and for other CI systems.

The version and the image digest are written into `action.yml` at release time. A composite action
cannot rely on reading its own ref at run time.

The tool writes the outputs and the job summary itself when it detects GitHub Actions, so the
action holds no logic beyond fetching the binary and calling it. A caller whose runner already
holds the binary needs no action: a plain step that runs `rollproof run` gives the same outputs
and the same summary.

The action uploads nothing. The documentation shows the caller's own upload step, which uses the
`out` output.

What the runner must hold: Linux, a container engine that kind supports, and `helm` on the PATH
when the Helm driver is used. On any other system `run` stops with a message and exits 2.

## 9. Cluster, images and enterprise settings

### The cluster

- **Shape.** One control-plane node and `cluster.workers` worker nodes.
- **Kubernetes version.** The node image decides it. The default is the default node image of the
  kind release the tool links, which kind pins by digest. `cluster.node-image` names another by
  full reference; that is how a mirror is used, and how a job matrix covers several versions. The
  tool promises only the node images that kind lists for the release it links. Another image may
  work and is not tested. Every report states the Kubernetes version that was read from the
  cluster.
- **Name and lifetime.** A run's cluster and registry carry a name that is stable for the working
  directory and the configuration file. `run` first deletes any cluster and registry of that
  name, then creates its own, and deletes them at the end, after the evidence is collected,
  whatever the outcome. `--keep-cluster` leaves them for debugging, and the next run replaces
  them. `--name` sets the name, for two runs at once in one folder. The stable name is what
  cleans up after a cancelled job: on a runner that persists, a cancelled step has about 10
  seconds before its processes are killed, and deleting a cluster took 12 to 22 s in the
  experiments.
- **Rootless engines.** When the container engine runs without root, the tool sets the option in
  the nodes' container runtime that stops each container from taking a kernel session key. Without
  it, a few clusters exhaust the user's key quota. Whether one cluster needs it is open
  (section 14).
- **What linking kind costs.** A new Kubernetes minor reaches users only after a kind release and
  then a release of this tool. The project accepts that rhythm and automates it: a dependency
  update that moves kind runs the cluster suite, and a green run is released.

### The run's registry

The tool starts a registry container on the cluster's network, after the cluster exists, and
writes its address into every node's registry settings. This is what makes an image reference by
digest resolve. The registry's own image is `cluster.registry-image`.

### How the caller's image gets in

`images` lists the images that are not in a registry yet. Each entry is named, and one key says
where the image is now:

- `local: NAME:TAG`: in the runner's container engine;
- `archive: PATH`: an image archive file.

The tool puts the image into the run's registry and records its digest.

`as` names the repository that the real manifests use, for example
`registry.example/team/orders`. The tool stores the image under that path in the run's registry
and tells the nodes, for that registry host, to try the run's registry first and the real registry
after it. The manifest under test is then the manifest that deploys: the same repository and the
same digest form, with nothing rewritten.

When `as` is left out, the tool uses a repository under `rollproof.test`, a host that exists only
inside the run. `${images.NAME.ref}` is therefore always the `as` repository with the digest, and
`${images.NAME.digest}` is the digest alone. The address of the run's registry never appears in a
caller's file.

An image that is already in a registry is not listed. The nodes pull it from there, through the
`registries` settings, as they pull the previous release's images. A caller who needs its digest
passes it in through the environment.

**The probe's own image.** The tool packages its own running binary as an image and pushes it into
the run's registry. Nothing is pulled, and the probe cannot be a different version from the tool.
This is one reason the tool runs on Linux only.

### Registries: credentials, mirrors and certificate authorities

A `registries` entry says what the cluster's nodes need in order to pull from one host. Each node
gets the mirror and certificate settings for the host, and a credentials file, mounted when the
cluster is created, that holds only the hosts listed. The runner's own Docker login file is never
copied into a node.

The entry configures nothing else. Helm runs with the runner's own configuration (section 7), and
the tool itself pulls from no registry.

A mirror that itself needs credentials is listed as a host of its own. Whether that is enough is
open (section 14).

Pods may still use `imagePullSecrets` from their own chart. The tool does nothing for them and
does not get in their way.

### What stays outside the tool

- **The engine's own pulls.** The tool cannot configure the runner's container engine. The node
  image and the registry image are pulled by that engine, so the caller gives them as full mirror
  references and logs the engine in beforehand when that is needed.
- **Helm's own pulls.** A chart registry that needs a login or a private certificate authority is
  set up for Helm by the caller, before the step.
- **Proxies.** kind passes `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY` into its nodes. This is
  documentation only.
- **Runners inside Kubernetes pods.** kind needs privileged containers. This is documented as a
  requirement and not solved.
- **Engines other than Docker.** kind marks its Podman and nerdctl providers experimental. Docker
  is the supported engine. The project is developed on nerdctl, so nerdctl is expected to work and
  is not promised.

## 10. Verdict and report

### Rules of the verdict

1. The windows are judged in order: the steady window, then each transition's. The first window
   that is not a pass decides the outcome and ends the run. The transitions after it are not run.
   A window in which the probe reported a failure is still run to its end, so that the evidence
   is whole.
2. The steady window is not verified when the probe reported a failure in it, or was silent in it
   for longer than `limits.silence`. A failure before anything changed points at the previous
   release or at the probe, not at a transition.
3. A transition's window fails when the probe reported a failure in it, when the transition
   stalled, or when its command failed.
4. A transition's window is not verified when the probe was silent in it for longer than
   `limits.silence`, or when the transition replaced no pod. In one window fail outranks not
   verified, and the report lists both.
5. A probe may report a failure late, for a window the run has already left. That window is then
   judged by it all the same, and the window after it is reported as not judged.

Why a failed transition ends the run: in the experiments, a service that exits as soon as it is
told to stop kept failing new connections for up to about 40 s after its rollout had finished.
After a deploy that failed this way, those failures would fall into the rollback's window and be
counted against the rollback. The cost is that after a failed deploy the rollback is tested only
in the next run.

### What a run writes

```
rollproof: FAIL   orders (Helm) on Kubernetes v1.37.0 in kind, probe: built-in HTTP

window      length   checks   failed   after finish   result
steady      10.0 s    2,000        0              -   clean
deploy      29.1 s    5,820      243             31   FAIL
roll back        -        -        -              -   not run

deploy: 243 checks failed (refused 198, timeout 45), first at 10:00:07.31
results: /tmp/rollproof-orders-7f3a
```

- **Console.** The summary above. The column "after finish" counts the failures that fell in the
  settle time.
- **`result.json`.** The same facts for machines. It is the stable contract for wrappers, and it
  is written on every exit, a rejected configuration included. It holds:
  - a schema version;
  - the outcome, and its reasons, each one a code from the table below with a text beside it;
  - every window and every transition;
  - the probes that judged the run, as a list that has one entry in the first release, each with
    its settings;
  - the tool's version;
  - the cluster: its provider, which is `kind`, the provider's version, the Kubernetes version and
    the node image digest.

  Within a schema version, fields and reason codes are added and never changed.
- **GitHub job summary.** The same table as Markdown, written only when the tool detects GitHub
  Actions.
- **Evidence folder.** The probe's raw lines; the window boundaries; a per-second timeline that
  puts failure counts beside pod events (created, ready, deleted, gone); the namespace's events;
  the pod lists before and after each transition; the logs of the replaced pods; the commands the
  tool ran with their output; and kind's own logs when set-up failed. Its layout is not a
  contract.

### Reasons

| Code | Outcome | When |
| --- | --- | --- |
| `probe-failures` | fail | The probe reported a failure in a transition's window |
| `transition-stalled` | fail | A transition did not finish inside `limits.timeout` |
| `command-failed` | fail | The command for a deploy or a rollback exited non-zero |
| `config-rejected` | not verified | The configuration was rejected |
| `unsupported-system` | not verified | The tool was run on a system other than Linux |
| `cluster-failed` | not verified | The cluster, the run's registry or an image could not be set up |
| `setup-failed` | not verified | A `setup` command exited non-zero |
| `install-failed` | not verified | The install exited non-zero, or the release was not ready inside `limits.timeout` |
| `probe-not-started` | not verified | No observation arrived inside `limits.timeout` |
| `probe-exited` | not verified | The probe exited before the tool stopped it |
| `probe-line-invalid` | not verified | A line broke the probe contract |
| `steady-not-clean` | not verified | The probe reported a failure in the steady window |
| `probe-silent` | not verified | The probe wrote nothing for longer than `limits.silence` |
| `no-pod-replaced` | not verified | A transition replaced no pod |
| `interrupted` | not verified | A signal stopped the run |
| `internal-error` | not verified | The tool itself failed |

### What every report states

- Which probe judged the run. A custom probe is named by its image digest.
- Which transitions were tested, so a restart-only run cannot be mistaken for a deploy and a
  rollback.
- The caveat of section 1: a pass in kind is necessary, not sufficient.

### Limits

- **No diagnosis.** The timeline shows when failures happened relative to pods starting and
  stopping. The tool does not guess the cause.
- **Workload logs are not masked.** The tool masks registry passwords everywhere it writes, but
  the logs of the replaced pods are the workload's own text. The caller decides whether to upload
  the evidence.

## 11. Security

- A registry password arrives only through an environment variable and is masked in all output.
  Nothing else is masked (section 8). This rule has not had a security review (section 14).
- The credentials file mounted into the nodes holds only the hosts the configuration lists.
  Temporary files that hold credentials are removed when the run ends.
- The tool does not read, copy or change the runner's Helm logins or its container engine's.
- kind runs its node containers privileged. That is a property of kind, and it is why the tool
  belongs on a runner that is discarded or trusted.
- The `setup` commands and the commands driver run the caller's own commands with the caller's own
  rights. The tool adds none.

## 12. How the project tests itself

**Without a cluster.**

- Table tests for every route through `verdict`.
- Configuration validation: unknown keys, unset variables, an unknown `${…}` form, a password
  written in the file, a combination of releases or commands that section 7 rejects, both or
  neither of a pair that needs exactly one.
- Probe-line parsing, assignment to windows, and silence detection, on a fake clock.
- Golden files for the report formats and for the kind configuration and node settings the tool
  generates.
- The deploy drivers against a fake command runner that records the arguments.

**With a cluster.** The real binary runs against a fixture service. The fixture is a small Go HTTP
service, copied from the experiments' test service, with switches for start delay and shutdown
behaviour, and a chart whose values produce each variant. The previous release and the candidate
are two images of it. The suite asserts the outcome and its reason code.

| Fixture | Expected outcome |
| --- | --- |
| Both releases follow the rules | Pass |
| The candidate has no readiness probe | Fail, in the deploy window; the rollback is not run |
| The candidate exits on SIGTERM | Fail, in the rollback window |
| No node is free for the extra pod | Fail, the deploy stalls |
| The previous release exits on SIGTERM | Fail, in the deploy window; the rollback is not run |
| The probe goes silent | Not verified |
| The probe exits early | Not verified |
| The deploy replaces no pod | Not verified |
| The previous release never becomes ready | Not verified |
| A `setup` command fails | Not verified |
| No previous release is named | Pass, reported as restart only |

The third row is why the rollback leg exists. A candidate that shuts down badly passes its own
deploy, because the pods that stop during the deploy belong to the previous release. The fifth row
is the case that rule 1 of section 10 was written for.

Every fixture is a Deployment. No fixture uses a StatefulSet or a DaemonSet.

**The mirror job.** Every image comes from a mirror inside the job, and the job fails if anything
reached a public registry. This is the proof of the mirror and certificate settings.

**The action.** A workflow uses the action from its own repository on the compliant fixture.

**The false-failure bound.** The project states its bound, from repeated runs of the compliant
fixture, and its catch counts for the broken ones, as the second experiment did. The statement
carries its date and the versions it was measured with. It is measured again when something that
affects it changes: the built-in probe, a timing default, the fixture, or the size of the runner.
A release in between carries the last statement forward. How many runs is set when the project has
a public repository.

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

- **When.** The cluster suite and the mirror job are green on GitHub.
- **How.** Split the directory with its history into a new public repository, move the workflows,
  and cut a first release with its image and binaries. Each step waits for the maintainer's word.
- **Before.** Check the name for collisions.

### Build order

The design is more than one implementation plan. It is built in this order, and each step from
the third on gets a plan of its own. The first plan covers steps 1 to 3.

1. **The host repository.** The decision row, the change to the host's citation check so that it
   skips `projects/`, and this spec.
2. **Proofs.** Open points 1 to 6 and 8 to 12 of section 14, as throwaway experiments or readings.
3. **A walking skeleton.** The Helm driver, the built-in HTTP probe, `watch`, `verdict`, the
   console report and `result.json`, on a local cluster. The first four fixtures of section 12
   give their expected outcomes.
4. **The rest of the two contracts.** The custom probe, the commands driver, the `setup` commands,
   restart-only runs, and the remaining fixtures.
5. **Enterprise settings.** `registries`, `as` in `images`, and the mirror job.
6. **The action**, the job summary and the workflows.
7. **Graduation.**

## 14. Open points for the plan

The plan settles each of these with a test or a reading before anything is built on it.

1. **Registry order in the nodes.** That a node tries the run's registry first for a host and then
   falls back to the real registry is known container-runtime behaviour, but it has not been run
   for this design. First task of the plan.
2. **Mirrors that need credentials.** Whether the credentials file in a node is used for a mirror
   host as it is for the original host has not been run. The kubelet matches a node's credentials
   against the name of the image being pulled, so credentials listed for a mirror's host may never
   be offered for an image named after the original host, and a second mechanism in the container
   runtime may be needed. An authenticated mirror is the usual enterprise case. Same task.
3. **Environment variables into a composite action.** Whether `env:` on the step that calls a
   composite action reaches the action's inner steps was not found in the pages read. The plan
   settles it with a run. If it does not, the values are passed as inputs.
4. **Outputs of a step that fails.** Whether the outputs a step wrote are kept when that step then
   exits non-zero, and whether a composite action passes them on, was not checked. The `outcome`
   output depends on it.
5. **Helm on hosted runners.** The experiments recorded Docker, kind and kubectl on the hosted
   runner image, not Helm. The plan checks it, and names the Helm major versions the first release
   is tested with.
6. **A Helm rollback with no revision.** If `helm rollback` without a revision goes back to the
   release before the last one, the tool need not read the revision after the install.
7. **The settle default.** 15 s rests on one variant of one service. The fixture runs confirm it
   or change it.
8. **kind as a library.** kind's project calls itself alpha grade, has not committed to its
   library surface, and has no compatibility plan between node images and kind releases. The plan
   confirms that the linked release creates a cluster through the Docker and nerdctl providers,
   with the node settings section 9 needs.
9. **Which kind release to link**, and the node images kind lists for it, which are the ones the
   tool promises.
10. **The rollout rules.** Whether the Go package that holds the rules of `kubectl rollout status`
    can be imported. Copied rules have to be read again at every Kubernetes minor.
11. **A pod's log across rotation.** The kubelet rotates a container's log at 10 MiB by default.
    Whether following the log through the API survives a rotation was not checked. A custom probe
    that prints a line for every check at a high rate reaches that size within minutes.
12. **Keys on a rootless engine.** How many kernel keys one cluster uses through a run without the
    setting of section 9. The experiments met the quota with four clusters at once. If one cluster
    stays well under it, the setting leaves the tool.
13. **Masking.** The rule of section 8 needs a security reviewer's eye before the first release.
14. **The name.**
15. **Prior art.** The search could not read GitHub Marketplace in full text and did not open
    several tools. Finding an existing tool later would change the project's reason to exist, not
    this design.

## 15. Not in the first release

- A rollback that interrupts a half-finished deploy.
- A Helm deploy with no rollback, and a restart after a rollback.
- Canary and blue-green controllers, and their custom resources.
- Traffic through an ingress as a built-in probe. A custom probe can do it.
- gRPC as a built-in probe.
- A built-in probe that keeps connections alive. In the experiments it caught nothing that a new
  connection per request missed.
- A probe that runs on the runner.
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

Not read for this design: any container-runtime documentation, any Helm documentation, any kubectl
source, and no Kubernetes documentation beyond the pages above. What sections 6, 7 and 9 say about
those is from the drafting session's own knowledge, and section 14 lists the points that need a
test.
