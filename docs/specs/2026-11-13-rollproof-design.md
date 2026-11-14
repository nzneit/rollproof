# rollproof: design

Date: 2026-11-13.

Status: the design below was agreed section by section in conversation on 2026-11-13. This written
form waits for the maintainer's review. No code exists yet.

An AI session on a Fable model drafted this document with the maintainer. Where a statement rests
on a measurement or on a page that was read, section 16 names the source. Everything else is
design, not fact, and nothing here has been run.

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
  not wait for old pods to finish terminating.
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
set up     cluster, registry, images
install    previous release, wait until ready
probe on   | steady | deploy | settle | roll back | settle |
verdict    report, evidence, exit code
```

1. **Set up.** Create a kind cluster with several nodes, start a registry container, and put the
   caller's images into it.
2. **Install** the previous release and wait until it is ready.
3. **Start the probe** and hold a steady window before anything changes.
4. **Deploy the candidate.** Wait for the transition to finish, under a timeout. Keep the probe
   running through a settle window afterwards.
5. **Roll back** to the previous release, with the same wait and the same settle window.
6. **Decide and report.**

### Windows

The time between the probe's start and its stop is divided into windows with no gap between them:
the steady window, then for each transition a transition window followed by a settle window. A
transition window runs from the moment the tool starts the transition until the transition has
finished (section 7). The settle window belongs to the transition before it: a failure there counts
against that transition.

### When a run passes

A run passes only when all four hold:

1. Every transition finished inside its timeout.
2. The probe reported no more failures than `limits.max-failures` in the steady window, and no
   more than that in each transition, counting its transition window and its settle window
   together. The default is zero.
3. The probe produced observations for every window, with no silence longer than `limits.silence`.
4. Every transition replaced at least one pod.

### Three outcomes

- **Pass.**
- **Fail.** Downtime was seen, a transition stalled, or the command that performs a deploy or a
  rollback failed.
- **Not verified.** The run could not judge: set-up broke, the previous release never became ready,
  the steady window was not clean, the probe gave no verdict for a window, or a transition replaced
  no pod.

Not verified fails the job, as fail does. It is reported apart from fail, as "could not test", so
that nobody reads a broken test as downtime or a skipped test as a pass. Section 10 gives the
rules.

### A run with no previous release

A repository that adopts the tool before its second release has nothing to upgrade from. Such a run
installs the candidate and restarts it under the probe, which is what the experiments did. It must
be asked for with `transitions: [restart]`; a configuration that names no previous release and does
not say this is rejected. The report says that only a restart was tested.

## 5. Components

The tool is one Go binary. Each unit has one job.

| Unit | What it does | What it knows about |
| --- | --- | --- |
| `config` | Reads and validates the run description | Nothing else |
| `cluster` | Creates and deletes the kind cluster and the run's registry, and writes mirror, certificate and credential settings into the nodes | kind, the container engine |
| `images` | Copies the caller's images into the run's registry and returns their digests | Registries, image archives |
| `deploy` | The deploy contract: install, deploy, roll back, restart. A Helm driver and a commands driver | Helm, or the caller's commands |
| `watch` | Observes a transition: when it finished, whether it stalled, whether pods were replaced | The Kubernetes API |
| `probe` | The probe contract: start, stop, collect observations. The built-in HTTP probe and the custom probe | The probe's pod or process |
| `run` | Sequences the phases and marks the windows on the timeline | Only the two contracts, `cluster` and `watch` |
| `verdict` | Turns observations and transition results into pass, fail or not verified, with reasons | Nothing. It is a pure function |
| `report` | Writes the console text, `result.json`, `junit.xml`, the GitHub job summary and the evidence folder | File formats |

Flow of one run: `config`, then `cluster`, then `images`, then `deploy` installs. `probe` starts.
For each transition `run` marks the window, `deploy` acts and `watch` waits. `probe` stops,
`verdict` decides, `report` writes.

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
- The probe prints one JSON line per check to stdout: `{"ok": true}` or `{"ok": false}`, with optional fields.
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

**Form.** A custom probe is a container image, which the tool runs as a pod in the cluster, or a
command, which the tool runs on the runner with `KUBECONFIG` set to the test cluster.

**Lines.** The probe writes one JSON object per line to stdout.

| Field | Required | Meaning |
| --- | --- | --- |
| `ok` | yes | `true` when the check passed, `false` when it failed |
| `t` | no | When the check began, RFC 3339. Default: the time the line was written |
| `n` | no | How many identical checks this line stands for. Default 1 |
| `class` | no | A short label for the kind of failure, such as `refused` or `timeout` |
| `ms` | no | How long the check took, in milliseconds |
| `detail` | no | Free text for the evidence |

A line that starts with `{` must parse as such an object, or the run is not verified. Any other
line is kept as log text in the evidence and is not counted.

For a pod, the default time of a line is the timestamp the container runtime puts on it. For a
command, it is the time the tool read it. The runner and the nodes of a kind cluster share one
clock, so the two agree. An observation is assigned to the window that holds its time. A `t`
outside every window of the run makes the run not verified.

**Lifecycle.**

- The tool starts the probe before the steady window. The probe counts as started when its first
  observation arrives. If none arrives within `limits.probe-start-timeout`, the run is not
  verified.
- While the probe runs, a gap longer than `limits.silence` between the moments two successive
  lines were written makes the window it falls in not verified. A hung probe therefore cannot
  pass. Silence is measured on when lines were written, never on `t`, so a probe that reports
  late results is not silent.
- The tool stops the probe after the last settle window, with SIGTERM. A probe that exits before
  that makes the run not verified.
- The probe knows nothing of windows, transitions or thresholds.

**Where probe pods run.** In a namespace of the tool's own, on the control-plane node, so that
the worker nodes hold only the workload and the commands driver never watches a probe pod.

### The built-in HTTP probe

It speaks the same contract. It is the tool's own binary, started as `rollproof probe http`.

- **Open model.** Request *i* starts at start + *i*/rate, whatever earlier requests did.
- **Connections.** `new` opens a connection per request and is the default. `reuse` keeps
  connections alive. `both` runs one stream of each at the configured rate.
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

- Helm driver: `release`, `namespace`, `previous`, `candidate`.
- Commands driver: `namespace`, and three commands named `install`, `deploy` and `rollback`.
- The tool owns the order and the meaning of each transition.

```yaml
# a Helm chart
deploy:
  helm:
    release: orders
    namespace: orders
    previous:  { chart: oci://registry.example/charts/orders, version: 1.4.2 }
    candidate: { chart: ./chart, set: { image.digest: "sha256:…" } }

# Kustomize, through commands
deploy:
  commands:
    namespace: orders
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
| Interface size | Four Helm fields, or three commands | A step grammar times each driver's actions | Two paths |
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

**Transitions.** `deploy.transitions` is a list drawn from `deploy`, `rollback` and `restart`. The
tool runs the ones listed in that fixed order. The default is `[deploy, rollback]`. `rollback`
needs `deploy`, and `deploy` needs a previous release.

**The Helm driver.**

| Transition | What the tool runs |
| --- | --- |
| install | `helm upgrade --install` of the previous release, with `--create-namespace`. With `transitions: [restart]` it installs the candidate |
| deploy | `helm upgrade` to the candidate |
| rollback | `helm rollback` to the revision the tool read after the install |

A release description has `chart` (a path, an `oci://` reference, or a chart name together with
`repo`), `version`, `repo`, `values` (a list of files) and `set` (a map). The tool writes `set` to
a values file of its own and passes it last, so types are kept and nothing needs escaping.

Helm is called without `--wait` and without `--atomic`. `watch` alone judges when a transition has
finished, so both drivers are judged the same way. Helm hooks run as Helm runs them.

**The commands driver.** Each command is an argument list. The tool starts it directly, with no
shell, on the runner, with `KUBECONFIG` set to the test cluster. The command returns when the
change has been applied; whether it also waits does not matter, because the tool watches either
way. `deploy` and `rollback` are required only when their transitions are listed; with
`transitions: [restart]`, `install` installs the candidate.

**When a driver's command fails.** For both drivers, a non-zero exit of the install makes the run
not verified, and a non-zero exit of a deploy or a rollback is a fail.

**The restart.** The tool performs it itself, the same way for both drivers: it changes the pod
template annotation that `kubectl rollout restart` changes, on every watched workload.

**What is watched.** With the Helm driver: the Deployments, StatefulSets and DaemonSets in the
release's own manifest, read before and after each transition. With the commands driver: those in
the named namespace, optionally narrowed by `selector`, a label selector.

**When a transition has finished.** Both must hold:

- every watched workload reports its rollout complete, by the conditions `kubectl rollout status`
  uses for its kind;
- every pod of a watched workload that existed when the transition began and has been deleted is
  gone, not merely terminating.

A transition that has not finished within `limits.transition-timeout` has stalled. The same limit
applies to the install.

**Pods replaced.** A transition replaced pods when at least one pod of a watched workload that
existed at its start is gone at its end and a new one has taken its place. A transition that
replaced none is not verified: it changed nothing the test can judge.

Inside `run`, a deploy driver is this (a sketch; the plan may adjust it):

```go
// Deployer performs the transitions of one workload and says which workloads to watch.
type Deployer interface {
    Install(ctx context.Context) error
    Deploy(ctx context.Context) error
    Rollback(ctx context.Context) error
    Targets(ctx context.Context) ([]Workload, error)
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
- The action has four inputs.

```yaml
# rollproof.yaml
version: 1
images:
  - name: orders
    from: local:orders:candidate
    as: registry.example/team/orders
deploy:
  helm:
    release: orders
    namespace: orders
    previous:  { chart: oci://registry.example/charts/orders, version: "${env.PREVIOUS_VERSION}" }
    candidate: { chart: ./chart, set: { image.digest: "${images.orders.digest}" } }
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
| Interface size | One schema, four action inputs, three flags | About thirty inputs, each duplicated as a flag | A Go API |
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
  kubernetes: "1.37"        # picks a node image from the tool's built-in table
  node-image: …             # a full reference; replaces the table's choice
  registry-image: …         # the image of the run's registry
  workers: 3

registries:                 # section 9
  - host: registry.example
    username: ${env.REGISTRY_USER}
    password: ${env.REGISTRY_PASSWORD}
    ca: certs/corp-ca.pem
    mirror: mirror.corp.example/registry-example

images:                     # section 9
  - name: orders
    from: local:orders:candidate
    as: registry.example/team/orders

deploy:                     # section 7
  transitions: [deploy, rollback]
  helm: { … }               # or: commands: { … }

probe:                      # section 6
  http:
    url: http://orders.orders.svc/healthz
    rate: 200
    timeout: 2s
    connections: new
    expect-status: 200-299
  # or:
  # custom:
  #   image: registry.example/team/consumer-probe@sha256:…   # or: command: [./probe]
  #   args: []
  #   env: {}

limits:
  steady: 10s
  transition-timeout: 5m
  settle: 15s
  max-failures: 0
  silence: 5s
  probe-start-timeout: 60s
```

Exactly one of `deploy.helm` and `deploy.commands`, and exactly one of `probe.http` and
`probe.custom`, must be present.

**Substitution.** Two forms exist and no others: `${env.NAME}` for an environment variable, and
`${images.NAME.digest}` or `${images.NAME.ref}` for an image the `images` section put into the
run's registry. A variable that is not set is a configuration error, never an empty string.

**Secrets.** A secret reaches the tool only through `${env.NAME}`. The tool masks every value that
arrived this way in everything it writes.

### Defaults, and the reason for each

| Setting | Default | Reason |
| --- | --- | --- |
| `cluster.kubernetes` | The newest version in the built-in table | One obvious choice; a caller who must cover a range uses a job matrix |
| `cluster.workers` | 3 | The experiments' shape. It leaves a node free for the extra pod of a two-replica workload that spreads by node |
| `probe.http.rate` | 200 a second | Measured to fit a 2-CPU hosted runner with no false failure in 150 jobs |
| `probe.http.timeout` | 2 s | The experiments' value; the slowest request that succeeded on a hosted runner took 247 ms |
| `probe.http.connections` | `new` | Only a new connection per request caught the missing readiness probe |
| `probe.http.expect-status` | 200 to 299 | A redirect or an error page is not an answer from the workload |
| `limits.steady` | 10 s | The experiments restarted 10 s after the load began |
| `limits.transition-timeout` | 5 minutes | Helm's own default wait, so a chart that deploys under Helm's default does not stall here |
| `limits.settle` | 15 s | In the experiments the failures that began after the rollout reported done, in the variant with no readiness probe, ended within about 3 s. The tool also waits for old pods to be gone before the settle window starts. 15 s is several times that margin; the fixture runs are to confirm it |
| `limits.max-failures` | 0 | The claim under test is no downtime |
| `limits.silence` | 5 s | The built-in probe writes at least one line a second, so 5 s is five missed lines. A custom probe that checks less often raises it |
| `limits.probe-start-timeout` | 60 s | Covers scheduling the probe pod and pulling its image |

### The command line

- `rollproof run`, with `-f FILE`, `--out DIR` and `--keep-cluster`.
- `rollproof probe http`, which the probe pod runs. Users do not call it.
- `rollproof version`.

Exit codes of `run`: 0 for pass, 1 for fail, 2 for not verified. A configuration that is rejected
exits 2.

### The action

`action.yml` is a composite action. It uses no other action, so an organization allows and syncs
one action, and a lockfile pins one.

| Input | Meaning | Default |
| --- | --- | --- |
| `config` | Path of the config file | `rollproof.yaml` |
| `image` | Where to pull the tool's own image from | The project's image, by digest, written into `action.yml` at each release |
| `binary` | Path of a pre-installed `rollproof`. When set, nothing is pulled | unset |
| `out` | Folder for the results | a folder under the runner's temporary directory |

| Output | Meaning |
| --- | --- |
| `outcome` | `pass`, `fail` or `not-verified` |
| `result` | Path of `result.json` |
| `evidence` | Path of the evidence folder |

How the tool reaches the runner: the action pulls the tool's image by digest and copies the binary
out of it. Everything the tool needs from the network is then an image, so an organization mirrors
one kind of thing, and a digest names content, not a location. Each release also attaches plain
binaries, for laptops.

The version and the image digest are written into `action.yml` at release time. A composite action
cannot rely on reading its own ref at run time.

The action uploads nothing. The documentation shows the caller's own upload step, which uses the
`evidence` output. The tool writes the outputs and the job summary itself when it detects GitHub
Actions, so the action holds no logic beyond finding the binary and calling it.

What the runner must hold: Linux, a container engine that kind supports, and `helm` on the PATH
when the Helm driver is used.

## 9. Cluster, images and enterprise settings

### The cluster

- **Shape.** One control-plane node and `cluster.workers` worker nodes.
- **Kubernetes version.** `cluster.kubernetes` selects a node image from a table built into the
  tool, pinned by digest, for the kind version the tool links. `cluster.node-image` replaces it
  with a full reference, which is how a mirror is used.
- **Lifetime.** Each run creates a cluster with a unique name and deletes it at the end, after the
  evidence is collected, whatever the outcome. `--keep-cluster` leaves it for debugging.
- **Rootless engines.** When the container engine runs without root, the tool sets the option in
  the nodes' container runtime that stops each container from taking a kernel session key. Without
  it, a few clusters exhaust the user's key quota.

### The run's registry

The tool starts a registry container on the cluster's network, after the cluster exists, and
writes its address into every node's registry settings. This is what makes an image reference by
digest resolve. The registry's own image is `cluster.registry-image`.

### How the caller's image gets in

`images[].from` says where the image is now:

- `local:NAME:TAG`: in the runner's container engine;
- `archive:PATH`: an image archive file;
- any other value: a registry reference the runner can pull.

The tool copies the image into the run's registry and records its digest.

`images[].as` names the repository that the real manifests use, for example
`registry.example/team/orders`. The tool then stores the image under that path in the run's
registry and tells the nodes, for that registry host, to try the run's registry first and the real
registry after it. The manifest under test is then the manifest that deploys: the same repository
and the same digest form, with nothing rewritten. `${images.NAME.ref}` is that repository with the
digest.

Without `as`, `${images.NAME.ref}` points at the run's registry directly.

The previous release's images are pulled by the nodes from wherever its manifests point, through
the `registries` settings.

**The probe's own image.** On Linux the tool packages its own running binary as an image and
pushes it into the run's registry. Nothing is pulled, and the probe cannot be a different version
from the tool. On another system the tool pulls its published image; that path is not tested in
the first release.

### Registries: credentials, mirrors and certificate authorities

One `registries` entry configures three places:

- **The nodes.** Each node gets the mirror and certificate settings for the host, and a
  credentials file, mounted when the cluster is created, that holds only the hosts listed. The
  runner's own Docker login file is never copied into a node.
- **Helm.** The tool logs Helm in to a configuration folder that exists only for the run, so
  private chart registries work and the runner's Helm settings are untouched.
- **The tool's own copying** of images between registries.

A mirror that itself needs credentials is listed as a host of its own.

Pods may still use `imagePullSecrets` from their own chart. The tool does nothing for them and
does not get in their way.

### What stays outside the tool

- **The engine's own pulls.** The tool cannot configure the runner's container engine. The node
  image and the registry image are pulled by that engine, so the caller gives them as full mirror
  references and logs the engine in beforehand when that is needed.
- **Proxies.** kind passes `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY` into its nodes. This is
  documentation only.
- **Runners inside Kubernetes pods.** kind needs privileged containers. This is documented as a
  requirement and not solved.
- **Engines other than Docker.** kind marks its Podman and nerdctl providers experimental. Docker
  is the supported engine. The project is developed on nerdctl, so nerdctl is expected to work and
  is not promised.

## 10. Verdict and report

### Rules of the verdict

1. Fail outranks not verified. If one window shows downtime and another could not be judged, the
   run fails, and the report lists both.
2. A steady window with more failures than `limits.max-failures` ends the run as not verified.
   A failure before anything changed points at the previous release or at the probe, not at a
   transition.
3. A stall, or a failing `deploy` or `rollback` command, ends the run as a fail. The transitions
   after it are reported as not run.
4. Probe failures do not end the run. A deploy that dropped requests still gets its rollback
   tested, so one run answers both.
5. Failures in a settle window count against the transition before it.

### What a run writes

```
rollproof: FAIL   orders (Helm) on Kubernetes v1.37.0 in kind, probe: built-in HTTP

window      length   checks   failed   result
steady      10.0 s    2,000        0   clean
deploy      14.1 s    2,820      212   FAIL
settle      15.0 s    3,000       31   FAIL
roll back   13.8 s    2,760        0   pass
settle      15.0 s    3,000        0   pass

deploy: 243 checks failed (refused 198, timeout 45), first at 10:00:07.31
evidence: /tmp/rollproof/run-7f3a
```

- **Console.** The summary above.
- **`result.json`.** The same facts for machines, with a schema version. It is the stable contract
  for wrappers. It holds the outcome and its reasons, every window and every transition, which
  probe judged the run and with which settings, the tool, kind and Kubernetes versions, and the
  node image digest.
- **`junit.xml`.** One case per check, with fixed names: `steady`, `deploy.completed`,
  `deploy.no-downtime`, `rollback.completed`, `rollback.no-downtime`, `restart.completed`,
  `restart.no-downtime`. A fail is a JUnit failure. Not verified is a JUnit error. A transition
  that was not run is skipped. Only the cases of the transitions that were asked for appear.
- **GitHub job summary.** The same table as Markdown, written only when the tool detects GitHub
  Actions.
- **Evidence folder.** The probe's raw lines; the window boundaries; a per-second timeline that
  puts failure counts beside pod events (created, ready, deleted, gone); the namespace's events;
  the pod lists before and after each transition; the logs of the replaced pods; the deploy
  commands with their output; and kind's own logs when set-up failed.

### What every report states

- Which probe judged the run. A custom probe is named by its image digest or its command.
- Which transitions were tested, so a restart-only run cannot be mistaken for a deploy and a
  rollback.
- The caveat of section 1: a pass in kind is necessary, not sufficient.

### Limits

- **No diagnosis.** The timeline shows when failures happened relative to pods starting and
  stopping. The tool does not guess the cause.
- **Workload logs are not masked.** The tool masks its own secrets everywhere, but the logs of the
  replaced pods are the workload's own text. The caller decides whether to upload the evidence
  folder.

## 11. Security

- Credentials arrive only through environment variables and are masked in all output.
- The credentials file mounted into the nodes holds only the hosts the configuration lists.
  Temporary files that hold credentials are removed when the run ends.
- Helm's registry logins go to a folder that exists only for the run.
- kind runs its node containers privileged. That is a property of kind, and it is why the tool
  belongs on a runner that is discarded or trusted.
- The commands driver and a command probe run the caller's own commands with the caller's own
  rights. The tool adds none.

## 12. How the project tests itself

**Without a cluster.**

- Table tests for every route through `verdict`.
- Configuration validation: unknown keys, unset variables, a missing previous release without
  `transitions: [restart]`, both or neither of a pair that needs exactly one.
- Probe-line parsing, assignment to windows, and silence detection, on a fake clock.
- Golden files for the report formats and for the kind configuration and node settings the tool
  generates.
- The deploy drivers against a fake command runner that records the arguments.

**With a cluster.** The real binary runs against a fixture service. The fixture is a small Go HTTP
service, copied from the experiments' test service, with switches for start delay and shutdown
behaviour, and a chart whose values produce each variant. The previous release and the candidate
are two images of it. The suite asserts the outcome and its reason.

| Fixture | Expected outcome |
| --- | --- |
| Both releases follow the rules | Pass |
| The candidate has no readiness probe | Fail, in the deploy window |
| The candidate exits on SIGTERM | Fail, in the rollback window |
| No node is free for the extra pod | Fail, the deploy stalls |
| The probe goes silent | Not verified |
| The probe exits early | Not verified |
| The deploy replaces no pod | Not verified |
| The previous release never becomes ready | Not verified |
| No previous release, `transitions: [restart]` | Pass, reported as restart only |

The third row is why the rollback leg exists. A candidate that shuts down badly passes its own
deploy, because the pods that stop during the deploy belong to the previous release.

**The mirror job.** Every image comes from a mirror inside the job, and the job fails if anything
reached a public registry. This is the proof of the mirror and certificate settings.

**The action.** A workflow uses the action from its own repository on the compliant fixture.

**The false-failure bound.** Each release states its bound, from repeated runs of the compliant
fixture, and its catch counts for the broken ones, as the second experiment did. How many runs is
set when the project has a public repository.

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
2. **Proofs.** Open points 1, 2, 3, 4, 6 and 7 of section 14, as throwaway experiments.
3. **A walking skeleton.** The Helm driver, the built-in HTTP probe, `watch`, `verdict`, the
   console report and `result.json`, on a local cluster. The first four fixtures of section 12
   give their expected outcomes.
4. **The rest of the two contracts.** The custom probe, the commands driver, restart-only runs,
   and the fixtures that expect not verified.
5. **Enterprise settings.** `registries`, `images[].as`, and the mirror job.
6. **The action**, `junit.xml`, the job summary and the workflows.
7. **Graduation.**

## 14. Open points for the plan

The plan settles each of these with a test or a reading before anything is built on it.

1. **Registry order in the nodes.** That a node tries the run's registry first for a host and then
   falls back to the real registry is known container-runtime behaviour, but it has not been run
   for this design. First task of the plan.
2. **Mirrors that need credentials.** Whether the credentials file in a node is used for a mirror
   host as it is for the original host has not been run. Same task.
3. **Environment variables into a composite action.** Whether `env:` on the step that calls a
   composite action reaches the action's inner steps was not found in the pages read. The plan
   settles it with a run. If it does not, secrets are passed as inputs.
4. **Helm on hosted runners.** The experiments recorded Docker, kind and kubectl on the hosted
   runner image, not Helm. The plan checks it.
5. **The settle default.** 15 s rests on one variant of one service. The fixture runs confirm it
   or change it.
6. **kind as a library.** kind's project calls itself alpha grade, and its library surface is
   used by few. The plan confirms that the linked release creates a cluster through the Docker and
   nerdctl providers, with the node settings section 9 needs.
7. **Which kind release to link**, and the node images it lists by digest.
8. **The name.**
9. **Prior art.** The search could not read GitHub Marketplace in full text and did not open
   several tools. Finding an existing tool later would change the project's reason to exist, not
   this design.

## 15. Not in the first release

- A rollback that interrupts a half-finished deploy.
- Canary and blue-green controllers, and their custom resources.
- Traffic through an ingress as a built-in probe. A custom probe can do it.
- gRPC as a built-in probe.
- More than one probe in a run.
- Cluster tools other than kind, and an existing cluster.
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
| `RESEARCH/2026-11-12/L5-kind-rollout.md` | The experiment on a workstation: the variants, the two-signal gate, the effect of the client's connections, the test service and the load generator that the fixture and the built-in probe are copied from, the kernel key quota on a rootless engine |
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

Not read for this design: any container-runtime documentation, any Helm documentation, and any
Kubernetes documentation. What sections 7 and 9 say about them is from the drafting session's own
knowledge, and section 14 lists the points that need a test.
