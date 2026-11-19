# The probe contract

This is the contract between `rollproof` and a probe that someone else writes. It is written for
a probe author who has not read the design.

## 1. Status

This page is written from section 6 of `docs/specs/2026-11-13-rollproof-design.md`. Until the
project graduates (section 13), that section is the authority;
where the page and the design differ, the design wins.

Fields are only ever added to the line format. The tool ignores a field it does not know, and a
field that changes how a line counts comes with a new `v` (section 6).

## 2. A probe in one minute

A probe is a container image, named with its digest, that the tool runs as a pod with the
`command`, `args` and `env` that your `probe.custom` entry gives; they mean what they mean in a
pod's container. `env` is therefore a list, and an entry may take its value from a Secret. The
probe prints one JSON line per check. A stock image and a shell loop are a complete probe, with no
image to build (sections 6 and 8).

This loop checks a TCP port:

```sh
trap 'exit 0' TERM
while true; do
  if nc -z -w1 orders-db.orders 5432; then echo '{"ok":true}'
  else echo '{"ok":false,"class":"refused"}'; fi
  sleep 0.1
done
```

This `probe.custom` block runs it. Its image line, `args` and `env` list follow section 8's
example: the image carries its digest, and an `env` entry takes a value from a Secret. Its
`command` is the same argument list as section 8's `[sh, -c, "…"]`, written as a block so that the
loop can be read.

```yaml
probe:
  custom:
    image: registry.example/tools/busybox@sha256:…   # required, with a digest
    command:
      - sh
      - -c
      - |
        trap 'exit 0' TERM
        while true; do
          if nc -z -w1 orders-db.orders 5432; then echo '{"ok":true}'
          else echo '{"ok":false,"class":"refused"}'; fi
          sleep 0.1
        done
    args: []
    env:                  # a list, as in a pod's container
      - name: TOKEN
        valueFrom: { secretKeyRef: { name: probe-token, key: token } }
```

The loop works in busybox 1.37.0 as user 65532 (section 16). The `trap` matters: a shell that
runs as the first process of its container ignores SIGTERM without one (sections 6 and 16), and
the tool stops the probe with SIGTERM (section 6).

## 3. Lines

The probe writes one JSON object per line. The tool reads the pod's log, which carries the
container's standard output and standard error as one stream, so it does not matter which of the
two a line was written to (section 6).

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

Every top-level name is reserved for the contract; a probe puts data of its own into `detail`. A
field the tool does not know is kept in the evidence and ignored. A line whose `v` the tool does
not know makes its window not verified, so an older tool never counts a newer line wrongly
(section 6).

## 4. How a line is read

In this order (section 6):

1. White space before the first character is skipped.
2. A line that does not start with `{` is log text. It is kept in the evidence and not counted.
   The exception is log text that holds `"ok"` followed by a colon: it looks like an observation
   behind a prefix, such as the date that a logging library puts first, and it makes its window
   not verified. A failure must not vanish because of the way it was printed.
3. A line that starts with `{` must be one valid JSON object, or its window is not verified.
4. An object that holds a top-level name twice is not valid, and its window is not verified.
5. A valid object with no top-level `ok` field is log text, so a library that logs JSON does no
   harm. The report says how many such lines there were.
6. An object whose `ok` is not `true` or `false`, or whose known field has a type or a value
   other than the table's, makes its window not verified.
7. The last line of a probe that was stopped may be cut short. A final piece with no line end,
   written after the tool asked the probe to stop, is kept in the evidence and not judged.

## 5. Where a fault lands

A fault of a line is placed in the window that holds the moment the line was written. Before the
probe's first observation there is no window, and the run is not verified. After the last window
has ended there is none either, and the fault is a warning: it changes no verdict. The exception
is a whole line whose top-level `ok` is `false`, also when the line holds `ok` twice and one of
them is `false`; its fault belongs to the last window, because a failure must not vanish because
of the way it was printed (section 6).

A `t` is a fault of its line in either of two cases. One is a `t` later than the moment its line
was written, because a check cannot begin after its result. The other is a `t` earlier than the
moment the tool asked for the probe's pod by more than 2 s, the bound of the clock check, because
no check can have begun before the probe existed. Like every such fault, either makes the window
in which the line was written not verified; for a failed check written during the drain, that
window is the last one (section 6).

## 6. Time

The default time of a line is the timestamp that the node's container runtime puts on it in the
pod's log. The runtime takes it from its own clock when it has read the whole line from the
container's output, which is not the moment the probe wrote it (section 6).

A probe writes each line as soon as its check ends, without buffering its output: in Python,
`flush=True` or `-u` (section 6). `t` is for a result that is known late, such as a consumer's. It
does not make up for buffered output, because silence is measured on when observation lines were
written (section 6).

```sh
echo '{"ok":false,"t":"2026-11-13T10:00:07.120Z","class":"lost","detail":"message 4411 not consumed within 5s"}'
```

A line longer than 16 KiB is stored in pieces and comes back with the time of its first piece
(section 6).

With kind the runner and the nodes share a clock: the nodes are containers on the runner's
kernel, and Linux gives a container no wall clock of its own. Putting the probe's lines and the
tool's own window marks on one clock is the tool's job, never the probe author's. The tool checks
this when the probe starts: it compares the time that the runtime put on the first observation
with the moment it read that line, and a difference above 2 s, the bound of the clock check, ends
the run as not verified. The check is part of the probe's start: the install window opens only
when it has passed. An engine that runs in a virtual machine can have a clock of its own
(section 6).

An observation is assigned to the window that holds its time, the install window included. One
whose time is before the install window began is counted in the install window. One whose time is
after the last window ended is not counted; the evidence keeps it (section 6).

A failed check that began in one window and whose line was written in a later one is counted where
it began. The report says how many failures crossed the start of a transition in this way, because
the transition may be what cut them (section 6).

## 7. Lifecycle

**Start.** The tool starts the probe when the install is ready. The probe counts as started when
its first observation arrives. If none arrives within `limits.timeout`, the run is not verified,
and the report shows the state of the probe's pod. A probe that exits before its first observation
ends that wait at once. When the API server refuses the probe's pod, the run is not verified at
once as `probe-not-started`, with the API server's text (section 6).

**The install window.** The first observation opens the install window, at the moment its line was
written by the runtime's timestamp and not by a `t` on the line. A failed check in the install
window is counted and is not judged. The steady window begins when the install has settled, and
from there on a failed check is judged: in the steady window it makes the run not verified. A
probe therefore needs no warm-up of its own (section 6).

**Exit.** The probe's pod is never restarted: its restart policy is `Never`. The tool reads an
exit from the state of the container named `probe`. A probe that exits before the tool stops it
makes the window in which it exited not verified, the install window included. One that exits
before its first observation makes the run not verified. The silence that follows an exit is not
reported beside it: the exit is the reason (section 6).

**Silence.** Silence is time with no observation. Log text does not end it. It is measured on the
tool's clock, by when observation lines were written and never by `t`, from the probe's first
observation to the end of the last window. A stretch longer than `limits.silence` with no
observation makes every window it overlaps not verified. `limits.silence` is 5 s by default, and
the configuration keeps it shorter than `limits.steady` and `limits.settle`, so a window with no
observation at all is always silent. A custom probe that checks less often raises `limits.silence`,
and raises `limits.steady` and `limits.settle` with it (sections 6 and 8).

**Stop.** The tool stops the probe when the run's last window has ended, when a failure of the
tool has cut a window short, or when the run ends before the probe's first observation, unless a
signal ended the run. It deletes the probe's pod with a grace period as long as the drain, so the
probe gets SIGTERM. The drain is 10 s for a custom probe (section 6).

On SIGTERM a probe starts no new check, writes the line of every check that is still in flight,
and exits. A probe that is still running when the drain ends is killed with its pod; that changes
no verdict, and the report says that the probe did not stop when asked and that checks still in
flight were not reported (section 6).

**Late results.** A probe whose results can arrive up to D seconds after a check began needs
`limits.settle` of at least D. Otherwise a failure that a transition caused is reported after that
transition's window has closed, or, for the last window, later than the drain, and then it is
lost. The contract has no value for "not known yet" (section 6).

**The log stream.** The tool follows the probe's log for the whole run. When the stream ends while
the container still runs, the tool opens it again, and it must neither lose a line nor count one
twice. After the tool has asked the probe to stop, a stream that ends while the pod still reads the
container as running is not opened again: the tool waits until the pod reads the container as
ended, and the end of that stream is then the end of the log (section 6).

## 8. What the author owns

The rate and the shape of the load are the probe author's. A loop that runs one check after
another slows down when the workload does and reports at most one failure per timeout, where the
built-in probe keeps its rate. What a probe cannot see, the tool cannot judge: a probe that opens
a new connection for every check sees nothing of the connections that real clients hold, and for
gRPC those are what a rollout breaks (section 6).

## 9. Where probe pods run

These names are part of the contract (section 6):

| What | Name |
| --- | --- |
| The namespace of every probe pod | `rollproof-probe` |
| A label on every probe pod | `app.kubernetes.io/name: rollproof-probe` |
| The probe's container | `probe` |

They follow the tool's name, which is a working name (sections 6 and 14).

A probe pod is not in the workload's namespace. A probe therefore names its target with the
namespace: `orders-db.orders`, and not `orders-db`, which resolves only inside the probe's own
namespace (section 6).

The tool creates the namespace before the `setup` commands run, so that `setup` can add to it what
a probe needs: a Secret that the probe's `env` reads, rights for its default service account when
a custom probe reads the Kubernetes API, or the label by which a mesh injects its proxy (section
6).

A workload whose chart denies traffic by default needs a rule that lets the probe in. That rule
belongs in the workload's namespace, beside the rule that denies. It selects the probe's namespace
by the label `kubernetes.io/metadata.name`, which Kubernetes puts on every namespace, and the
probe's pod by the label above (section 6).

The tool reads the log of the container named `probe` and judges an early exit by that container
alone, so a proxy that a mesh injects into the pod does not break the contract. It does change
what is tested: the probe's requests then pass through the mesh, and a mesh's proxy may send a
failed request again before the probe sees the failure (Istio does so twice by default), which
hides failures. A probe that starts before its proxy is ready reports failures at first; they fall
in the install window and are not judged when the proxy is ready within the time the install
settles (section 6).

Probe pods run on the control-plane node, so that the worker nodes hold only the workload and the
tool never watches a probe pod. One consequence: a Service whose traffic policy is local to the
node has no endpoint for a client on that node, and cannot be probed from there (section 6).

## 10. The custom probe's pod

The tool writes a custom probe's pod with the built-in probe's security settings: the container
runs as user 65532 and not as root, may gain no privilege, drops every capability and uses the
runtime's default seccomp profile. The pod meets the `restricted` Pod Security level (section 6).

A probe's program must therefore work as user 65532, whatever user its image names. The shell loop
in section 2 does, in a stock busybox image (section 6).

Besides its image, `command`, `args`, `env` and drain, one thing differs: a custom probe's pod
mounts the token of the namespace's default service account, so that a probe can read the
Kubernetes API with the rights that `setup` gave it. The built-in probe's pod mounts none (section
6).

An image that `images` does not list is pulled by the kubelet with the probe's pod, by the pull
policy `IfNotPresent` (sections 6 and 9).

## 11. What the report says of a custom probe

The console, the job summary and `result.json` give names and no values: the probe's image
reference as resolved, which carries a digest (section 8), and the names of its `env` entries. Its
`command` and `args` appear there as counts. The evidence folder may hold the values (section 8).

The report's lines about a custom probe say "a custom probe judged this run; the tool does not
know what it checks". The second table's "probed" column reads "not known", because the tool
cannot say which workloads the probe reaches. On a pass, one line stands in the place of the
built-in probe's three facts in the list of what the run did not exercise: "whatever the custom
probe does not check" (section 10).

The tool vouches that the probe ran and reported, and for nothing that it checks (section 1).
