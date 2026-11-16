# rollproof: design

Date: 2026-11-13. Revised twice on 2026-11-16.

Status: the design below was agreed section by section in conversation on 2026-11-13. Two reviews
followed.

- A review on 2026-11-14 proposed changes that make the public surface smaller and close gaps in
  the contracts. The maintainer accepted them on 2026-11-16.
- A review on 2026-11-16 asked how general the tool is and how far a pass can be trusted. Two
  further agents then looked in documentation and source code for evidence for and against each
  answer to it, and three answers changed. The maintainer accepted the result on 2026-11-16.

Two readings of source code on 2026-11-16 then checked what the text said of Helm and of eight
further points. The maintainer accepted the four changes to the Helm driver that followed, and
confirmed that a probe on the runner stays out of the first release (section 14, points 17
and 18).

This text includes all of it. This written form waits for the maintainer's review. No code exists
yet.

An AI session on a Fable model drafted this document with the maintainer. A separate AI agent on a
Fable model wrote each review, and AI agents on an Opus model gathered the evidence and did the
two readings. Where a
statement rests on a measurement, a page or a source file that was read, section 16 names the
source. Everything else is design, not fact, and nothing here has been run.

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
and shutdown behaviour of the workloads that the report lists kept the probe free of failures
while their pods were replaced, once by the deploy and once by the rollback.

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
- **A custom probe is the caller's.** The tool vouches that it ran and reported, and for nothing
  that it checks.

### Who can use it

The tool runs on Linux, beside a container engine that may run privileged containers (section 8).
It tests Deployments, StatefulSets and DaemonSets that update by rolling (section 7). The built-in
probe sends HTTP GET requests; anything else is a custom probe (section 6).

It cannot be used, in the first release:

- on a runner that is an unprivileged pod or container, or with a container engine on another
  machine;
- from a job that itself runs inside a container;
- for pods that no Deployment, StatefulSet or DaemonSet manages;
- with a deployer that changes the workload minutes after its command returned, such as a
  controller that polls a Git repository.

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
  exactly as many nodes as the workload needed. A real cluster usually has spare nodes.
- **Capacity and latency.** No variant broke the surge numbers, the replica count or the grace
  period alone, and the load was light: peak load and tail latency were not tested.

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
   caller's images into it, create the probe's namespace, and run the caller's `setup` commands.
2. **Install** the previous release and wait until it is ready (section 7).
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
done (section 3). It has to be quiet: a pod change inside it puts the transition back in progress.

### When a run passes

A run passes only when all four hold:

1. Every transition finished inside `limits.timeout`.
2. The probe reported no failure in any window.
3. The probe produced observations for every window, with no silence longer than `limits.silence`.
4. Every transition replaced at least one pod.

The report lists, for every transition, how many pods of each watched workload were replaced. A
workload of which none was replaced is marked "not exercised" and does not change the outcome
(section 7).

### Three outcomes

- **Pass.**
- **Fail.** Downtime was seen, a transition stalled, or the command that performs a deploy or a
  rollback failed.
- **Not verified.** The run could not judge: set-up broke, a `setup` command failed, the previous
  release never became ready, the install left nothing to watch or a workload of a form the tool
  cannot judge, the steady window was not clean, the probe gave no verdict for a window, or a
  transition replaced no pod.

Not verified fails the job, as fail does. It is reported apart from fail, as "could not test", so
that nobody reads a broken test as downtime or a skipped test as a pass. Section 10 gives the
rules.

### A run with no previous release

A repository that adopts the tool before its second release has nothing to upgrade from. A
configuration that names no previous release gets a run that installs the candidate and restarts
it under the probe, which is what the experiments did. Nothing has to be switched on for this: the
transitions follow from what the configuration names (section 7). Every report of such a run says
that only a restart was tested.

The restart-only run is also the way in for a service that is broken today. While the previous
release shuts down badly, every deploy fails in its window, whatever the candidate does, until a
fixed release exists to deploy from. A restart of the candidate alone tests the candidate.

## 5. Components

The tool is one Go binary. Each unit has one job.

| Unit | What it does | What it knows about |
| --- | --- | --- |
| `config` | Reads and validates the run description | Nothing else |
| `cluster` | Creates and deletes the kind cluster and the run's registry, and writes mirror, certificate and credential settings into the nodes | kind, the container engine |
| `images` | Puts the caller's images into the run's registry and returns their digests | The container engine, image archives |
| `deploy` | The deploy contract: install, deploy, roll back, restart. A Helm driver and a commands driver. It also runs the caller's `setup` commands, with the commands driver's runner | Helm, or the caller's commands |
| `watch` | Observes the watched workloads: when a transition finished, whether it stalled, which pods were replaced and how long they took to stop. It follows the pods' status and logs while they live | The Kubernetes API |
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

A probe that runs on the runner is not in the first release. One form keeps the contract small,
and a pod reaches a Service's cluster address in the same way with every container engine. From
the runner the way in depends on the engine (read, not run; section 16):

- A Service's cluster address is reachable only from within the cluster, with every engine.
- Where the engine is Docker run as root on Linux, as on GitHub's hosted runners, the runner
  reaches every node at its address on the cluster's network, and so a Service of type NodePort.
  That traffic passes through the node's proxy to any ready endpoint, so it follows a rollout.
- With Docker Desktop, or with Docker run without root, the node addresses cannot be reached. The
  documents of Podman and nerdctl imply the same for their rootless modes. The way in is then a
  port mapping from the runner to one node, which kind sets when it creates the node.
- A port-forward is tied to one pod and ends when that pod is replaced, so a probe through one
  would report failures that the workload did not cause.

A probe on the runner would therefore need a Service of type NodePort from the chart or from the
tool, and one path for each kind of engine.

**Lines.** The probe writes one JSON object per line. The tool reads the pod's log, which carries
the container's standard output and standard error as one stream, so it does not matter which of
the two a line was written to.

| Field | Required | Meaning |
| --- | --- | --- |
| `ok` | yes | `true` when the check passed, `false` when it failed |
| `t` | no | When the check began, RFC 3339. Default: the time the line was written |
| `n` | no | How many identical checks this line stands for. Default 1 |
| `class` | no | A short label for the kind of failure, such as `refused` or `timeout` |
| `ms` | no | How long the check took, in milliseconds. On a line with `n`, the longest of them |
| `lag` | no | How far behind its own schedule the check began, in milliseconds. On a line with `n`, the furthest of them |
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

**Time.** The default time of a line is the timestamp the node's container runtime puts on it in
the pod's log. The runtime takes it from its own clock when it has read the whole line from the
container's output, which is not the moment the probe wrote it. A probe therefore writes each
line as soon as its check ends and does not buffer its output; a probe that cannot do that gives
`t`. A line longer than 16 KiB is stored in pieces and comes back with the time of its first
piece. Putting the probe's lines and the tool's own window marks on one clock is the tool's job,
never the probe author's. With kind, the runner and the nodes share a clock: the nodes are
containers on the runner's kernel, and Linux gives a container no wall clock of its own.

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
- A probe whose results can arrive up to D seconds after a check began needs `limits.settle` of
  at least D. Otherwise a failure that a transition caused is reported after that transition's
  window has closed (rule 5 of section 10), or, for the last window, later than the 10 seconds
  above, and then it is lost. The contract has no value for "not known yet".
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

The tool creates the namespace before the `setup` commands run (section 7), so that `setup` can
add to it what a probe needs: rights for its default service account, when a custom probe reads
the Kubernetes API, or the label by which a mesh injects its proxy.

A workload whose chart denies traffic by default needs a rule that lets the probe in. That rule
belongs in the workload's namespace, beside the rule that denies. It selects the probe's namespace
by the label `kubernetes.io/metadata.name`, which Kubernetes puts on every namespace, and the
probe's pod by the label above. The test's values or a `setup` command add it. kind's own network
plugin enforces such policies.

The tool reads the log of the container named `probe` and judges an early exit by that container
alone, so a proxy that a mesh injects into the pod does not break the contract. It does change
what is tested: the probe's requests then pass through the mesh. A mesh's proxy may send a failed
request again before the probe sees the failure (Istio does so twice by default), which hides
failures, and a probe that starts before its proxy is ready reports failures in the steady window.

Probe pods run on the control-plane node, so that the worker nodes hold only the workload and the
tool never watches a probe pod. One consequence: a Service whose traffic policy is local to the
node has no endpoint for a client on that node, and cannot be probed from there.

The built-in probe's pod meets the `restricted` Pod Security level, so a label that `setup` puts
on the namespace cannot keep it out. For a custom probe the image decides.

### The built-in HTTP probe

It speaks the same contract. It is the tool's own binary, started as `rollproof probe http`.

- **Open model.** Request *i* starts at start + *i*/rate, whatever earlier requests did.
- **Requests.** HTTP GET to `probe.http.url`, with the headers of `probe.http.headers` when the
  configuration gives any. A `Host` header sets the host that the request names. A URL may be
  `https`. The probe then does not verify the server's certificate, as the kubelet's own HTTP
  check does not, and the report says so.
- **Connections.** It opens a new connection for every request. In the experiments only such a
  client caught a service with no readiness probe; a client that kept connections alive passed it
  in 15 of 15 runs.
- **What it cannot see.** Once the cluster has stopped sending new connections to a stopping pod,
  the probe has no connection to that pod. A connection that a real client kept open to it, and a
  request still in flight on it, can then be cut with no trace in the probe. A pod that serves for
  some seconds after SIGTERM and then ends without draining therefore passes. Section 12 keeps a
  fixture for this known miss, and section 15 says why a stream of held connections waits.
- **A failure** is a response outside the expected status range (default 200 to 299), a connection
  error, or no answer within the request timeout. The class is the status code, or one of
  `refused`, `reset`, `eof`, `timeout`, `other`.
- **Output.** One line per failed request, with `t`, `class` and `ms`, and one summary line per
  second for the requests that passed: `n`, the longest of them in `ms`, and in `lag` the furthest
  that any request of that second began behind its schedule.
- **Names.** It resolves the URL's host once at start and then dials that address, so cluster DNS
  is not on the path of every request. A Service's cluster address does not change during a
  rollout. A headless Service has no cluster address: its name resolves to the addresses of pods,
  and those change. Before it starts the probe, the tool looks up the Service that the URL's host
  names, when it names one in the cluster, and a headless one makes the run not verified. The
  text names the two ways out: a Service with a cluster address for the test, or a custom probe.

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
`KUBECONFIG` set to the test cluster. They run in order, after the cluster is ready, the images
are in the run's registry and the probe's namespace exists, and before the install. They are for
what a probe needs in that namespace (section 6), and for what the workload needs and its
chart does not bring: a Secret, a custom resource definition, a database, a broker. Whatever they
create in `deploy.namespace` is watched like the workload, unless `deploy.selector` leaves it out,
so a dependency belongs in a namespace of its own.

**The Helm driver.**

| Transition | What the tool runs |
| --- | --- |
| install | `helm upgrade --install` of the release that the table above names, into `deploy.namespace`, with `--create-namespace` |
| deploy | `helm upgrade` to the candidate, with `--reset-values` |
| rollback | `helm rollback` to the revision the tool read after the install |

Each of the three also gets `--timeout` with the value of `limits.timeout`, and the flags that
name the test cluster (see "The cluster Helm acts on" below).

On a new cluster the install is revision 1, the deploy revision 2 and the rollback revision 3. A
rollback applies the manifest that Helm stored for the revision it goes back to, without rendering
it again, as a new revision. The pod templates are then those of the install, so Kubernetes brings
the previous release's pods back. The tool names the revision. With none, Helm goes back to the
highest revision less one, whatever state that one is in.

A release description has `chart`, `version` and `values`.

- `chart` is a path, an `oci://` reference, or a name that the runner's Helm configuration
  resolves, such as a repository alias with a chart name. An alias needs that repository's index
  on the runner, from an earlier `helm repo add` or `helm repo update`. When `chart` is a path
  that exists, Helm uses it and ignores `version`, so the configuration rejects a `version`
  beside such a `chart`.
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
`deploy.namespace` with `--namespace`. Like every command the tool starts, Helm also gets
`KUBECONFIG` set to that file.

That is not enough by itself. Helm takes cluster settings from its environment as well, and each
one that is set overrides what the kubeconfig file says, whichever file the command line names. A
server address among them would send Helm, and the candidate with it, to a cluster other than the
test cluster. `HELM_DRIVER` moves the place where Helm keeps its release records: with `sql` to a
database outside the cluster, and with `memory` into the memory of one Helm process. The tool
therefore takes these variables out of the environment of every command it starts, which are
Helm, the `setup` commands and the commands of the commands driver:

`HELM_KUBECONTEXT`, `HELM_KUBEAPISERVER`, `HELM_KUBETOKEN`, `HELM_KUBECAFILE`, `HELM_KUBEASUSER`,
`HELM_KUBEASGROUPS`, `HELM_KUBEINSECURE_SKIP_TLS_VERIFY`, `HELM_KUBETLS_SERVER_NAME`,
`HELM_NAMESPACE`, `HELM_DRIVER` and `HELM_DRIVER_SQL_CONNECTION_STRING`.

The console names each variable that was set and taken out. Every other variable passes through
unchanged.

Helm is called without `--wait` and without the flag that rolls a failed upgrade back (`--atomic`
in Helm 3, `--rollback-on-failure` in Helm 4; in both majors that flag also turns waiting on). In
both majors Helm then returns before the rollout is complete. `watch` alone judges when a
transition has finished, so both drivers are judged the same way.

Helm hooks run as Helm runs them. Three facts about them matter here:

- Helm waits for a hook that is a Job or a Pod even when it is not told to wait, and it bounds
  each such hook by its `--timeout`, which is 5 minutes when none is given. The tool passes
  `limits.timeout` there, so that a limit raised for a slow chart reaches Helm too. Helm applies
  it to each hook on its own. The tool's limit covers the whole transition from its start, hooks
  included, and it alone decides a stall.
- A rollback runs the rollback hooks that were stored with the revision it goes back to.
- A hook that fails makes the command exit non-zero, as every other failure of Helm does.

**The commands driver.** Each command is an argument list. The tool starts it directly, with no
shell, on the runner, with `KUBECONFIG` set to the test cluster and without the variables listed
under "The cluster Helm acts on". `install` is required. `deploy` and `rollback` are optional, as
the table above shows.

A command must itself bring the change to the watched workloads. It may return as soon as it has
submitted the change; the tool watches from there. A command that hands the change to a controller
is covered only when the controller begins within `limits.settle` of the moment the transition
looked finished (see "When a transition has finished"). A controller that polls a Git repository
every few minutes is outside that. A team that deploys through one applies, with this driver, the
manifests that the controller would apply: the readiness, shutdown and strategy under test are in
the manifests, not in the controller.

**When a command fails.** For both drivers, a non-zero exit of a `setup` command or of the install
makes the run not verified, and a non-zero exit of a deploy or a rollback is a fail. Helm exits
with the same code for every failure. A deploy that failed because a chart could not be fetched,
or because a value broke the chart's schema, is therefore a fail like any other, and the command's
output in the evidence says which it was.

**The restart.** The tool performs it itself, the same way for both drivers: it changes the pod
template annotation that `kubectl rollout restart` changes, on every watched workload. A
dependency that the chart bundles into the same namespace is restarted with the rest, unless
`deploy.selector` leaves it out.

**What is watched.** The Deployments, StatefulSets and DaemonSets in `deploy.namespace`,
optionally narrowed by `deploy.selector`, a label selector. The rule is the same for both drivers.
A chart that places workloads in a second namespace has only the named one watched.

- **Nothing to watch.** When the install leaves no such workload, the run is not verified, and
  the text names the namespace, the selector and the three kinds. Pods that some other controller
  manages are not watched (section 15).
- **Strategies.** A watched StatefulSet or DaemonSet must update by rolling. With `OnDelete`, a
  changed template replaces no pod until someone deletes one, and `kubectl rollout status` gives
  no status for any strategy but the rolling one. Such a workload makes the run not verified.
- **Identity.** A pod is identified by its UID, never by its name. A StatefulSet's pod comes back
  under the same name and is a new pod.
- **Tests.** The project's own fixtures are Deployments, with one StatefulSet and one DaemonSet
  (section 12).

**When a transition has finished.** Both must hold:

- every watched workload reports its rollout complete, by the conditions `kubectl rollout status`
  uses for its kind;
- every pod of a watched workload that existed when the transition began and has been deleted is
  gone, not merely terminating.

The transition's window then stays open for `limits.settle`, and that time has to be quiet. If,
during it, a pod of a watched workload is created or deleted, or the spec of a watched workload
changes, the transition has not finished after all: both conditions have to hold again, and the
settle time starts again from there. A change of readiness or a restarted container does not
start it again, because neither creates a pod or changes a spec.

The quiet time is what covers a second rollout that follows the first, for example when a
controller restarts a workload after a Secret changed, and a command whose change begins a little
after it returned. It does not cover a change that begins later than `limits.settle` after the
transition looked finished.

A transition that has not finished within `limits.timeout`, counted from its start, has stalled.
When the settle time started again, the stall's text says what started it. The same limit applies
to the install and to the probe's start.

**When the install is ready.** When at least one workload is watched and both conditions hold. The
settle time does not apply to the install; the steady window follows it.

**Pods replaced.** For each watched workload the tool counts how many of the pods that existed at
the start of a transition are gone at its end with new ones in their place. A transition replaced
pods when that count is at least one for at least one workload. A transition that replaced none
is not verified: it changed nothing the test can judge.

The report lists the count for every workload, and a workload with none is marked "not exercised"
(section 10). A run still passes when the workload behind the probe is one of those. A deploy that
leaves a workload alone caused it no downtime, and the report says that its rollout was not
tested.

**How long old pods took to stop.** For each pod that a transition deleted, the tool notes when
its own watch first saw the deletion and when the pod was gone. The report gives the shortest of
these for each transition. It is an upper bound: that pod served for at most this long after it
was told to stop, and the time includes the kubelet's clean-up. A small figure is a warning for a
larger cluster (section 1). A large one proves nothing. The pod's own deletion timestamp is not
used for this: it is the deadline, the time of the request plus the grace period, in whole
seconds.

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
    url: http://orders.orders.svc/api/orders/latest
    headers: { Host: orders.example }   # optional
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

`headers` is a map of header names to values. The built-in probe sends them with every request.
They are for what the workload needs in order to answer as it answers a caller: a host name, or
the header that says the caller's connection was encrypted. They are not for a credential (see
"Secrets").

**Substitution.** It applies inside string values, never to keys. Two forms exist: `${env.NAME}`
for an environment variable, and `${images.NAME.digest}` or `${images.NAME.ref}` for an image the
`images` section put into the run's registry. Any other `${…}` is a configuration error, so a form
added later cannot change the meaning of a file that exists today. `$${` writes a literal `${`. A
variable that is not set is a configuration error, never an empty string.

**Secrets.** The one secret the tool handles is a registry password. `registries[].password` must
be a single `${env.NAME}`; a password written in the file is rejected. The tool masks the value of
every such password in everything it writes. The tool masks nothing else. A chart value, a
probe's environment value or a probe header's value is written as it was given, so a caller who
passes a real secret there keeps the evidence to themselves. GitHub's own masking does not cover
the evidence: its documentation promises it for job logs, job summaries and job outputs, and says
nothing of an uploaded artifact or of a file that a step writes. The console, the job summary and
`result.json` give the names of the probe's headers and of the values files, and none of their
values; the evidence folder may hold the values.

### Defaults, and the reason for each

| Setting | Default | Reason |
| --- | --- | --- |
| `cluster.node-image` | The default node image of the linked kind release | kind pins it by digest and maintains it. A table of the tool's own would go stale with every kind release |
| `cluster.workers` | 3 | A rolling update that adds a pod before it removes one needs a node for the extra pod. Three workers give that room to two replicas that must not share a node, which was the experiments' shape. A workload with three such replicas needs four workers (section 9) |
| `probe.http.rate` | 200 a second | Measured to fit a hosted runner with 2 CPUs, with no false failure in 150 jobs, against a handler that sleeps. A handler that costs CPU may not hold this rate on a small runner. The steady window is then not clean, and the rate is lowered |
| `probe.http.timeout` | 2 s | It is the caller's own latency rule: a slower request is a failure. 2 s is the experiments' value, where the slowest request that passed on a hosted runner took 247 ms. A service with a latency budget writes its budget here |
| `probe.http.expect-status` | 200 to 299 | A redirect or an error page is not an answer from the workload |
| `limits.steady` | 10 s | Long enough to show that the probe and the workload work before anything changes: 2,000 checks at the default rate. It is not a control for a failure that is rare without any rollout; the paragraph below the table is for that |
| `limits.settle` | 15 s | It has to catch the failures that begin only after a transition has finished. In the experiments those ended within about 3 s, in the variant with no readiness probe, and the tool also waits for old pods to be gone before the settle time starts. It does not have to outlast a failure that is already under way: one failure fails the window, and a failed window ends the run. 15 s is several times the 3 s; the fixture runs are to confirm it. It is also how long the tool waits for a second change to begin (section 7) |
| `limits.timeout` | 5 minutes | The length of Helm's default timeout for one operation. It is not the same bound: Helm applies its timeout to each hook separately, and this limit covers a whole transition from its start, hooks included. The tool passes the value to Helm as its `--timeout` (section 7). One limit covers the install, the probe's start and each transition |
| `limits.silence` | 5 s | The built-in probe writes at least one line a second, so 5 s is five missed lines. A custom probe that checks less often raises it |

There is no failure budget. A pass means that the probe reported no failure, which is the claim
the tool is named for. Thresholds of other kinds are not in the first release (section 15).

**A failure that no pod event explains.** With no budget, one failed request fails a run, and a
workload may fail one request in some minutes with no rollout at all, or a small runner may
starve the probe. The check for that is one run with `limits.steady: 5m`. A failure in that
steady window is the workload's own baseline or the runner, not the rollout. The built-in probe
also reports how far it ran behind its schedule (section 6), which shows a starved runner.

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

What the runner must hold: Linux; a container engine that kind supports, on the same host and in
the same network namespace as the tool, that may run privileged containers; and `helm` on the PATH
when the Helm driver is used. On a system other than Linux `run` stops with a message and exits 2.
Section 9 lists the runners that this rules out.

Each release says which CPU architectures it is built for and on which of them its cluster suite
ran. The first release is built for amd64 and arm64, and its suite runs on amd64.

## 9. Cluster, images and enterprise settings

### The cluster

- **Shape.** One control-plane node and `cluster.workers` worker nodes. Each worker carries a zone
  label of its own (`topology.kubernetes.io/zone`), and the control-plane node carries none. A
  workload that must spread by zone can then be scheduled. The probe's traffic starts on the
  control-plane node, and a node with no zone ignores zone hints, so that traffic reaches every
  endpoint as it would with no zones at all. Kubernetes' own multi-zone test job on kind has this
  shape. One thing does change: a Service that asks for zone-local routing gets it between the
  workload's own pods.
- **What the shape decides.** A rolling update that adds a pod before it removes one needs room
  for the extra pod. A workload with as many replicas as there are workers, and a rule that
  forbids two of its pods on one node, has no room here and stalls. That stall shows that the
  manifests cannot roll in a cluster of this shape. A real cluster usually has spare nodes. The
  stall's text names `cluster.workers` when a pod could not be scheduled.
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
- **Storage.** kind has one storage class, `standard`. A volume of that class is a folder on one
  node, and it is tied to that node by the node's host name. A StatefulSet's pod that is deleted
  and created again keeps its claim, so it returns to the same node, whatever the zone labels
  say.
- **Rootless engines.** When the container engine runs without root, the tool sets the option in
  the nodes' container runtime that stops each container from taking a kernel session key. Without
  it, a few clusters exhaust the user's key quota. Whether one cluster needs it is open
  (section 14).
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
digest resolve. The registry's own image is `cluster.registry-image`.

### How the caller's image gets in

`images` lists the images that are not in a registry yet. Each entry is named, and one key says
where the image is now:

- `local: NAME:TAG`: in the runner's container engine;
- `archive: PATH`: an image archive file.

The tool puts the image into the run's registry and records its digest.

The image is stored by digest only, never under a tag. A tag can come to name another image with
no sign of it. When the run's registry does not hold a tag, or cannot be reached, a node goes on
to the real registry and runs whatever that tag names there; and a node that already holds an
image under a tag does not pull that tag again. A digest names one content, wherever it is pulled
from. A chart that takes only a tag writes the tag with the digest after it,
`candidate@${images.orders.digest}`. The node's container runtime drops the tag from such a
reference and asks the registry for the digest alone. Kubernetes still takes the default pull
policy from the tag: `latest` before the digest gives `Always`, and any other tag gives
`IfNotPresent`. The pull is by digest in both cases.

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

`mirror` is a host, with or without a path. The node's container runtime puts the registry API's
own path after it. A cache that expects its project name inside that API path, and not before it,
needs a different node setting, and how `mirror` says which of the two it names is open
(section 14).

What the source of the kubelet and of the container runtime shows about credentials (read, not
run; section 16): the kubelet looks credentials up by the registry host in the image's own name,
and passes them on with no host attached. The container runtime then offers them to every host it
contacts for that pull. Two things follow.

- An entry's username and password reach the entry's mirror too. The caller must trust the mirror
  with them.
- Credentials listed under a mirror's own host are never used for an image that is named after the
  original host. A mirror that needs credentials of its own needs another mechanism in the node,
  and that is open (section 14).

Pods may still use `imagePullSecrets` from their own chart. The tool does nothing for them and
does not get in their way.

### What stays outside the tool

- **The engine's own pulls.** The tool cannot configure the runner's container engine. The node
  image and the registry image are pulled by that engine, so the caller gives them as full mirror
  references and logs the engine in beforehand when that is needed.
- **Helm's own pulls.** A chart registry that needs a login or a private certificate authority is
  set up for Helm by the caller, before the step. The tool passes Helm no login and no
  certificate flag. Helm reads its repositories and its registry logins from its own files on the
  runner, and falls back to the logins that Docker stored. Given no certificate flag, Helm trusts
  the certificate authorities of the runner's system store. A caller adds a private authority to
  that store, or names a bundle in `SSL_CERT_FILE` or a folder in `SSL_CERT_DIR`. A bundle named
  that way replaces the system's, so it must also hold the public authorities that are still
  needed. A repository that was added under an alias with a certificate file keeps that file.
  The tool passes both variables on to Helm.
- **Proxies.** kind passes `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY` into its nodes. This is
  documentation only.
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

A run that passed:

```
rollproof: PASS   orders (Helm) on Kubernetes v1.37.0 in kind

probe       built-in HTTP: GET http://orders.orders.svc/api/orders/latest
            200 a second (one check every 5 ms), timeout 2 s, status 200-299
            slowest passed request 212 ms; furthest behind schedule 38 ms

window      length   checks   failed   after finish   result
steady      10.0 s    2,000        0              -   clean
deploy      33.4 s    6,680        0              0   pass
roll back   33.9 s    6,780        0              0   pass

transition  workload            pods replaced   quickest stop
deploy      deployment/orders   2 of 2          5.3 s
roll back   deployment/orders   2 of 2          5.2 s

exercised
  deploy      the candidate's start-up and the previous release's shutdown
  roll back   the previous release's start-up and the candidate's shutdown
  values      previous: chart defaults; candidate: inline (image)
not exercised
  the delay before a larger cluster stops routing to a stopping pod
  connections held open, and requests in flight on a pod after new connections left it
  the path through an ingress, a load balancer or a mesh
  capacity while pods are down, and autoscaling
  latency below the timeout
  data migrations
  a node drain

results: /tmp/rollproof-orders-7f3a
```

A run that failed:

```
rollproof: FAIL   orders (Helm) on Kubernetes v1.37.0 in kind

probe       built-in HTTP: GET http://orders.orders.svc/api/orders/latest
            200 a second (one check every 5 ms), timeout 2 s, status 200-299

window      length   checks   failed   after finish   result
steady      10.0 s    2,000        0              -   clean
deploy      29.1 s    5,820      243             31   FAIL
roll back        -        -        -              -   not run

deploy: 243 checks failed (refused 198, timeout 45), first at 10:00:07.31
  stopping then   orders-6d5f9c-x2k4q (previous release), told to stop 0.3 s before
  starting then   none
results: /tmp/rollproof-orders-7f3a
```

- **Console.** The summaries above.
  - The probe's lines give its target and its settings. A probe or limit value that differs from
    its default is printed with the default beside it, so that a weakened test shows. A custom
    probe is named by its image digest, with the line "a custom probe judged this run; the tool
    does not know what it checks".
  - The column "after finish" counts the failures that fell in the settle time.
  - The second table lists every watched workload for every transition. A workload of which no
    pod was replaced reads "not exercised". "Quickest stop" is the figure of section 7: the old
    pod that was gone soonest served for at most that long after it was told to stop.
  - On probe failures, the lines under the count name the watched pods that were stopping
    (deleted, not gone) or starting (created, not ready) at the moment of the first failure, and
    the release each belongs to. They say what was happening and name no cause.
  - On a stall, the console lists each watched workload that is not complete, with its counts,
    and each pod that is not ready: its phase, why it is not scheduled, the state of its
    containers and how each last ended. Those come from the pod's status. The last event message
    follows them; Kubernetes keeps events as best effort, and for one hour by default. When a pod
    could not be scheduled, the text names the cluster's shape and `cluster.workers`. When the
    settle time started again, the text says what started it.
  - Every pass ends with the lists "exercised" and "not exercised" (see "What every report
    states").
- **`result.json`.** The same facts for machines. It is the stable contract for wrappers, and it
  is written on every exit, a rejected configuration included. It holds:
  - a schema version;
  - the outcome, and its reasons, each one a code from the table below with a text beside it;
  - every window, with its counts, the slowest request that passed and the furthest lag that the
    probe reported;
  - every transition, with each watched workload's pods replaced and pods at the start, the
    quickest stop, and what started the settle time again, if anything did;
  - the probes that judged the run, as a list that has one entry in the first release, each with
    its settings and their defaults, and with the names of its headers in place of the headers;
  - for the Helm driver, the names of the values files and the top-level keys of the inline maps
    that the tool passed for each release, and never a value. Helm keeps the merged values and
    not where they came from, so this is the tool's own record. The commands driver gives none;
  - the tool's version;
  - the cluster: its provider, which is `kind`, the provider's version, the Kubernetes version,
    the node image digest and the number of workers.

  Within a schema version, fields and reason codes are added and never changed. The list of what
  a pass did not exercise is text for people: `result.json` carries the facts behind it and no
  codes for it.
- **GitHub job summary.** The same tables and lists as Markdown, written only when the tool
  detects GitHub Actions.
- **Evidence folder.** The probe's raw lines; the window boundaries; a per-second timeline that
  puts failure counts beside pod events (created, ready, deleted, gone); the namespace's events,
  which stay after their pod is gone, for the one hour that the API server keeps an event by
  default; the pod lists before and after each transition; the logs of every pod of a watched
  workload, old and new; the commands the tool ran with their output; and kind's own logs when
  set-up failed. The tool follows each pod's log while the pod lives, because a pod's log cannot
  be read once the pod is gone, and it adds the previous run of a restarted container where the
  kubelet still holds it. The folder's layout is not a contract.

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
| `nothing-watched` | not verified | After the install, no Deployment, StatefulSet or DaemonSet matched `deploy.namespace` and `deploy.selector` |
| `workload-unsupported` | not verified | A watched StatefulSet or DaemonSet has an update strategy other than the rolling one, or the built-in probe's URL names a headless Service |
| `probe-not-started` | not verified | No observation arrived inside `limits.timeout` |
| `probe-exited` | not verified | The probe exited before the tool stopped it |
| `probe-line-invalid` | not verified | A line broke the probe contract |
| `steady-not-clean` | not verified | The probe reported a failure in the steady window |
| `probe-silent` | not verified | The probe wrote nothing for longer than `limits.silence` |
| `no-pod-replaced` | not verified | A transition replaced no pod |
| `interrupted` | not verified | A signal stopped the run |
| `internal-error` | not verified | The tool itself failed |

### What every report states

- Which probe judged the run, with its target and its settings. A custom probe is named by its
  image digest.
- Which transitions were tested, and what each exercised. A deploy tests the candidate's start-up
  and the previous release's shutdown. A rollback tests the previous release's start-up and the
  candidate's shutdown. A restart tests both, of the one release. A run with a deploy and no
  rollback says that the candidate's shutdown was not exercised, and a restart-only run cannot be
  mistaken for a deploy and a rollback.
- Which workloads had pods replaced, and which had none.
- On a pass, what was not exercised.

The list of what a pass did not exercise is fixed, and each line has a fact behind it:

| Not exercised | Why |
| --- | --- |
| The delay before a larger cluster stops routing to a stopping pod | The test cluster applies the change within about a second (section 1) |
| Connections held open, and requests in flight on a pod after new connections left it | The built-in probe opens a new connection for every request (section 6). With a custom probe the line reads "whatever the custom probe does not check" |
| The path through an ingress, a load balancer or a mesh | The built-in probe enters at the Service. Those components learn of a pod change on a path of their own |
| Capacity while pods are down, and autoscaling | The probe's load needs no capacity, and an autoscaler has no metrics here |
| Latency below the timeout | A request that answers inside `probe.http.timeout` is a pass. The slowest one is reported |
| Data migrations | The tool runs none and checks none |
| A node drain | A drain evicts pods under a disruption budget, and a rolling update is not limited by one. It is another transition |

A line leaves the list when a fixture shows the tool catching what it names.

### Limits

- **No diagnosis.** The report says which pods were starting or stopping when the first failure
  came, and the timeline shows the rest. The tool does not guess the cause.
- **Workload logs are not masked.** The tool masks registry passwords everywhere it writes, but
  the logs of the watched pods are the workload's own text. The caller decides whether to upload
  the evidence.

## 11. Security

- A registry password arrives only through an environment variable and is masked in all output.
  Nothing else is masked (section 8). This rule has not had a security review (section 14).
- The credentials file mounted into the nodes holds only the hosts the configuration lists.
  Temporary files that hold credentials are removed when the run ends.
- A registry entry's credentials are offered to that entry's mirror as well as to its host
  (section 9). A caller lists a mirror only when the mirror may see them.
- The built-in probe does not verify a server's certificate. It sends nothing secret, unless the
  caller put a credential into a header, and a header's value is not masked.
- The tool does not read, copy or change the runner's Helm logins or its container engine's.
- Every command the tool starts gets the test cluster's kubeconfig, and none of the variables by
  which Helm would act on another cluster or keep its records elsewhere (section 7). A candidate
  under test must not reach a cluster that the job's environment names for a later step.
- kind runs its node containers privileged. That is a property of kind, and it is why the tool
  belongs on a runner that is discarded or trusted.
- The `setup` commands and the commands driver run the caller's own commands with the caller's own
  rights. The tool adds none.

## 12. How the project tests itself

**Without a cluster.**

- Table tests for every route through `verdict`.
- Configuration validation: unknown keys, unset variables, an unknown `${…}` form, a password
  written in the file, a combination of releases or commands that section 7 rejects, both or
  neither of a pair that needs exactly one, a `version` beside a chart path that exists, a comma
  in a values file's path.
- Probe-line parsing, assignment to windows, and silence detection, on a fake clock.
- The rules of `watch`, on recorded API objects and a fake clock: when a transition has finished,
  the quiet time and what starts it again, pods counted by UID, nothing to watch, a strategy
  other than the rolling one, a headless Service, the quickest stop.
- Golden files for the report formats and for the kind configuration and node settings the tool
  generates.
- The deploy drivers against a fake command runner that records the arguments and the
  environment: the flags of section 7, and that the variables it lists are gone.

**With a cluster.** The real binary runs against a fixture service. The fixture is a small Go HTTP
service, copied from the experiments' test service, with switches for start delay and shutdown
behaviour, and a chart whose values produce each variant. The previous release and the candidate
are two images of it. The suite asserts the outcome and its reason code.

| Fixture | Expected outcome |
| --- | --- |
| Both releases report ready only when they can serve, and after SIGTERM serve on and then drain | Pass |
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
| Both releases serve for 5 s after SIGTERM and then end without draining | Pass. A known miss of the built-in probe (section 6) |
| A second rollout starts 5 s after the deploy's first one has finished | Pass, and the deploy's window covers both rollouts |
| The chart denies traffic by default, and the test's values let the probe in | Pass |
| A custom probe, the TCP loop of section 6, judges the first fixture | Pass |
| The same custom probe judges a candidate with no readiness probe | Fail, in the deploy window |
| The workload is a StatefulSet behind a Service with a cluster address | Pass |
| The workload is a DaemonSet behind a Service with a cluster address | Pass |

The first row is the fixture that the other rows change one thing of. This document calls it the
first fixture.

The third row is why the rollback leg exists. A candidate that shuts down badly passes its own
deploy, because the pods that stop during the deploy belong to the previous release. The fifth row
is the case that rule 1 of section 10 was written for.

The twelfth row is kept so that the limit is tested and stated, and not found by a user. Its
expected outcome is reasoned from how the cluster routes; no run has shown it yet. If it ever
fails, the limit has moved and section 6 is out of date.

The custom-probe rows are the only ones in which a probe the tool did not write judges a real
transition. Every other fixture uses the built-in probe, and every fixture but the last two is a
Deployment.

**The mirror job.** Every image comes from a mirror inside the job, and the job fails if anything
reached a public registry. This is the proof of the mirror and certificate settings. It includes a
mirror that sits under a path of its host.

**The action.** A workflow uses the action from its own repository on the first fixture.

**The false-failure bound.** The project states its bound, from repeated runs of the first
fixture, and its catch counts for the broken ones, as the second experiment did. The statement
carries its date, the versions and the runner's size it was measured with. It is the bound of
that fixture on that runner: a small service whose handler sleeps. It is measured again when
something that affects it changes: the built-in probe, a timing default, the fixture, or the size
of the runner. A release in between carries the last statement forward. How many runs is set when
the project has a public repository.

Two more measurements go with it:

- **How long a pod must serve here.** The first fixture runs with its time of serving after
  SIGTERM at 0, 0.5, 1 and 2 s, and the statement gives the shortest that passed. That is the
  margin this cluster demands; section 1 says why a larger cluster demands more.
- **A start that costs CPU.** One cell whose start-up uses a full CPU for some seconds, so that a
  workload that is heavy at its start is in the picture before the README quotes a bound.

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
2. **Mirrors that need credentials.** Read in source and not run (section 9): an entry's
   credentials reach its mirror, and credentials listed under the mirror's own host are never
   used. The task confirms both with a run. It also finds the mechanism for a mirror whose
   credentials differ from the original host's: the container runtime's own setting per host,
   which its documentation calls deprecated, or a header in the mirror's node entry. Credentials
   that a credential-provider plugin of the kubelet supplies were not read. An authenticated
   mirror is the usual enterprise case. Same task.
3. **Environment variables into a composite action.** Whether `env:` on the step that calls a
   composite action reaches the action's inner steps was not found in the pages read. The plan
   settles it with a run. If it does not, the values are passed as inputs.
4. **Outputs of a step that fails.** Whether the outputs a step wrote are kept when that step then
   exits non-zero, and whether a composite action passes them on, was not checked. The `outcome`
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
    that prints a line for every check at a high rate reaches that size within minutes. The tool
    follows the log of every watched pod too (section 10), so the answer matters for both.
12. **Keys on a rootless engine.** How many kernel keys one cluster uses through a run without the
    setting of section 9. The experiments met the quota with four clusters at once. If one cluster
    stays well under it, the setting leaves the tool.
13. **Masking, and what leaves the runner.** The rule of section 8 needs a security reviewer's eye
    before the first release. So do two facts of section 11: a registry entry's credentials reach
    its mirror, and the value of a probe header is not masked.
14. **The name.** The probe's namespace and label carry it (section 6).
15. **Prior art.** The search could not read GitHub Marketplace in full text and did not open
    several tools. Finding an existing tool later would change the project's reason to exist, not
    this design.
16. **A large image.** The experiments' image was 5.5 MB. Every node keeps its own copy of an
    image, and kind turns off the kubelet's clean-up of images and its eviction on a full disk,
    so a full disk is not recovered. A hosted runner has a 14 GB disk. The first pull of the
    candidate on each node falls inside the deploy's window. A run with the fixture padded to
    about 1 GB measures disk, pull time and false failures. After it the project decides whether
    the tool pulls the listed images into the nodes before the steady window. It is measured with
    the fixture series, not among the proofs of the build order's second step.
17. **Four changes to the Helm driver.** Settled. A reading of Helm's source on 2026-11-16 showed
    where the driver, as section 7 then described it, went wrong, and the maintainer accepted
    the four changes that day. Section 7 carries them:
    - the deploy passes `--reset-values`;
    - the tool passes `limits.timeout` to Helm as its `--timeout`;
    - the tool names the test cluster on Helm's command line and takes out of the environment
      the variables that override it. The same variables are taken from the `setup` commands and
      from the commands driver, which may run Helm too. That last step is the drafting
      session's, for the same reason, and the maintainer has not looked at it;
    - the configuration rejects a `version` beside a chart path that exists, and a comma in a
      values file's path.
18. **A probe on the runner.** Settled. This design first said that the runner cannot reach a
    Service of a kind cluster. A reading on 2026-11-16 showed that to hold for a Service's
    cluster address only (section 6). With that in front of them, the maintainer confirmed the
    same day that such a probe stays out of the first release. The reasons are the size of the
    contract and the one path per kind of engine.

## 15. Not in the first release

- A rollback that interrupts a half-finished deploy.
- A Helm deploy with no rollback, and a restart after a rollback.
- Canary and blue-green controllers, and their custom resources.
- Pods whose chain of controllers does not lead to a Deployment, a StatefulSet or a DaemonSet: an
  operator that creates its pods itself, or a controller with a workload resource of its own.
- A StatefulSet or a DaemonSet with an update strategy other than the rolling one.
- A deployer that begins its change later than the settle time after its command returned, such
  as a controller that polls a Git repository.
- Traffic through an ingress as a built-in probe. A custom probe can do it.
- gRPC as a built-in probe.
- A stream of held connections in the built-in probe. It would see what the probe misses today
  (section 6). It waits for a measurement: on HTTP/1.1 a server that shuts down correctly may
  close an idle connection just as a client sends a request on it, so such a stream can fail
  against a correct server. The experiments' kept-alive client showed nothing either way: its
  library sends such a request again, and no variant ended with connections open.
- A headless Service, or a Service whose traffic policy is local to the node, as the built-in
  probe's target.
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
- A probe that runs on the runner. The sources show a way that nobody has run (section 6): a
  Service of type NodePort at a node's address where the engine is Docker run as root, and a port
  mapping that the tool sets when it creates the cluster for the other engines.
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

Read last on 2026-11-16 by one AI agent on an Opus model, which took the eight statements that the
earlier readings had left unverified and that a reading can settle. It found one wrong: this
design said that the runner cannot reach a Service of a kind cluster, and that holds for a
Service's cluster address only (section 6). Three needed a correction and one an addition, and
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

Still not shown by any run or any page: that the fixture for the known miss passes; that a job
inside a container can reach the cluster; that a larger sync time of the proxy gives no delay that
can be set; the two rollouts under a reloader; how a mirror under a path behaves; that Podman run
as root lets the runner reach the node addresses as Docker does; that a port mapping cannot be
added to a node after it was created; which Go version Helm's released binaries are built with;
and where a pod goes when the node its volume is tied to cannot take it. That the hosted runner's
Docker runs as root is read from the image's install script, which no page states. What is known
of these is reasoned from the sources above or is the drafting session's own knowledge, and
section 14 lists the points that need a test.
