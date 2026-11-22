# Point 8: kind as a library

## Question

Point 8 of section 14: kind calls itself alpha grade and has not committed to its library surface. Does the linked release, `sigs.k8s.io/kind` v0.33.0 used as a library, create a cluster through the engine on this machine with the node settings of section 9, and which exact settings worked? This record covers nerdctl and, since 2026-11-22, Docker on GitHub's hosted runner (section `## Docker`).

## Machine and versions

- System: the developer's machine, Linux 7.3.3, amd64, 16 CPUs, 60 GiB memory; cgroup v2 with the systemd driver.
- Engine: nerdctl 2.4.2, with containerd v2.4.2, runc 1.5.2 and rootlesskit 3.3.0 (network slirp4netns). Docker is absent: `docker -v` fails because no `docker` is on the PATH.
- Rootless: yes. The `SecurityOptions` of `nerdctl info --format "{{json .}}"` are `name=seccomp,profile=builtin`, `name=cgroupns` and `name=rootless`.
- Go 1.27.3.
- kind: `sigs.k8s.io/kind` v0.33.0 (`version.Version()` gives `0.33.0`); `k8s.io/client-go` v0.37.2.
- Node image: `kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5`, kind's default, already in the engine's store.
- Kubernetes: v1.37.0, the server's `GitVersion`.
- Runtime in the nodes: containerd v2.3.4 (`RuntimeVersion` of `crictl version`), as the spec read.

## Method

The program is `proofs/p08-kind-library/main.go`. It notes the caller's kubeconfig files (`$HOME/.kube/config` and each path in `$KUBECONFIG`) with their SHA-256, writes `{"auths":{}}` to `config.json` in a temporary folder of its own (mode 0600, in a folder of mode 0700), and builds this description, where `configJSON` is the path of that file:

```go
mounts := func() []v1alpha4.Mount {
	return []v1alpha4.Mount{{HostPath: configJSON, ContainerPath: "/var/lib/kubelet/config.json", Readonly: true}}
}
cfg := &v1alpha4.Cluster{
	Nodes:                   []v1alpha4.Node{{Role: v1alpha4.ControlPlaneRole, ExtraMounts: mounts()}},
	ContainerdConfigPatches: []string{"[plugins.\"io.containerd.grpc.v1.cri\".registry]\n  config_path = \"/etc/containerd/certs.d\""},
}
for n := 1; n <= 3; n++ {
	cfg.Nodes = append(cfg.Nodes, v1alpha4.Node{
		Role:        v1alpha4.WorkerRole,
		Labels:      map[string]string{"topology.kubernetes.io/zone": fmt.Sprintf("rollproof-zone-%d", n)},
		ExtraMounts: mounts(),
	})
}
```

The description names no image, so kind uses its default. The program then calls `lab.Up(ctx, "p08", cfg)`. The lab picks the engine as kind's `cluster.DetectNodeProvider` does (`KIND_EXPERIMENTAL_PROVIDER` when set, else the first of docker, nerdctl and podman whose `-v` passes kind's own test) and makes kind's provider with `cluster.ProviderWithNerdctl("nerdctl")`. Because the engine is rootless, it adds a second containerd patch, which sets `NoNewKeyring = true` in `[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc.options]`. It calls `Create` with `CreateWithV1Alpha4Config`, `CreateWithKubeconfigPath` (a file `kubeconfig` in a new temporary folder), `CreateWithWaitForReady(0)`, `CreateWithRetain(false)`, `CreateWithDisplayUsage(false)` and `CreateWithDisplaySalutation(false)`, then polls the nodes every second until all four are Ready. The program then makes the checks of the brief, reads the versions, calls `l.Down()`, and checks what is left.

The command, from `projects/rollproof/proofs/`:

```
go run ./p08-kind-library
```

It ran nine times on 2026-11-16, between 23:09 and 23:33 UTC, one cluster at a time. The first run failed, so the next runs were made to see how often, and where, it fails. Runs 1 to 7 used the program as first written. Before run 8 it had three edits that change no line that a run prints here: how it holds the state of a caller's file, how it reads the output of `nerdctl inspect`, and a guard taken out. Run 8 tried a caller's kubeconfig that exists, beside one that does not:

```
env KUBECONFIG=/tmp/rollproof-p08-kubeconfig/config-a:/tmp/rollproof-p08-kubeconfig/config-b go run ./p08-kind-library
```

Run 9 is the plain command on the program as committed. Runs 2 to 7 ran beside a shell loop that, every 2 s, saved `nerdctl logs -t` of each node container and `nerdctl ps -a`; in runs 6 and 7 it also saved the list of running nerdctl processes. Kind's own code for the failing step was read in the source of v0.33.0: `pkg/cluster/internal/providers/nerdctl/provider.go` creates the node containers one after another (its comment cites nerdctl issue 2908), and `provision.go`, after the `nerdctl run` of each node, follows `nerdctl logs -f NODE` for at most 30 s until a line matches `Reached target .*Multi-User System.*|detected cgroup v1`.

What kind leaves on the machine was read after the runs with `nerdctl network ls --format "{{.ID}} {{.Name}}"`, `nerdctl images --digests` and `nerdctl ps -a`.

## Result

Four of nine runs passed every check. Five failed in `lab.Up`, inside kind's `Create`, before Kubernetes started:

| Run | Last line | `lab:` lines |
| --- | --- | --- |
| 1 | `p08: 1 checks failed` | none |
| 2 | `p08: all checks passed` | Create returned after 25.998s, 0 of 4 Ready; every node Ready after 37.996s |
| 3 | `p08: 1 checks failed` | none |
| 4 | `p08: 1 checks failed` | none |
| 5 | `p08: all checks passed` | Create returned after 21.906s, 0 of 4 Ready; every node Ready after 33.905s |
| 6 | `p08: all checks passed` | Create returned after 35.931s, 0 of 4 Ready; every node Ready after 47.93s |
| 7 | `p08: 1 checks failed` | none |
| 8 | `p08: all checks passed` | Create returned after 21.407s, 0 of 4 Ready; every node Ready after 33.404s |
| 9 | `p08: 1 checks failed` | none |

**The part of the plan's assumption that differs:** "kind v0.33.0 as a library creates one control-plane node and three workers" through nerdctl holds in four runs of nine. Every other part held in every run where `Create` returned.

The output of run 1, which runs 3, 4, 7 and 9 repeat line for line:

```
info: $HOME/.kube/config does not exist
info: KIND_EXPERIMENTAL_PROVIDER is unset
info: docker -v does not exit 0: exec: "docker": executable file not found in $PATH
FAILED: lab.Up: lab: creating the cluster rollproof-proof-p08: could not find a log line that matches "Reached target .*Multi-User System.*|detected cgroup v1"
p08: 1 checks failed
```

Where it fails. In runs 3, 4 and 7 the saved logs show that every node created so far printed `Reached target multi-user.target - Multi-User System.` about 0.3 s after its first line (0.24 s to 0.31 s). In run 7 the worker's first line is at 23:15:42.562 and that line at 23:15:42.805. The process list of run 7 shows kind's `nerdctl logs -f rollproof-proof-p08-worker` running from 23:15:43.6 until 23:16:10.7, the last snapshot before the cluster was deleted, while `nerdctl logs` of the same container already held the line; no later node was created. Run 3 failed at the first worker, run 4 at the second. So kind's follow of a node's log did not deliver, within its 30 s, a line that the log already held. The cause was not found: a plain container that printed one line, with or without a terminal, and with 100 lines in quick succession before it, had that line delivered by `nerdctl logs -f` 45 times of 45. A one-node cluster through the same lab came up four times of four in Task 3. Kind follows the log once for each node, so a cluster of four nodes meets this step four times.

The output of run 2; runs 5, 6 and 8 print the same lines, apart from the kubeconfig folder's random suffix and, in run 8, the lines for its two kubeconfig files (below):

```
info: $HOME/.kube/config does not exist
info: KIND_EXPERIMENTAL_PROVIDER is unset
info: docker -v does not exit 0: exec: "docker": executable file not found in $PATH
ok: KIND_EXPERIMENTAL_PROVIDER is unset and docker -v failed, and the lab picked nerdctl
ok: nerdctl inspect rollproof-proof-p08-control-plane exits 0
ok: l.Nodes() has four names, the first rollproof-proof-p08-control-plane: rollproof-proof-p08-control-plane rollproof-proof-p08-worker rollproof-proof-p08-worker2 rollproof-proof-p08-worker3
ok: after Up, $HOME/.kube/config does not exist
ok: l.Kubeconfig() exists at /tmp/rollproof-proof-p08-3393885364/kubeconfig and its current context is kind-rollproof-proof-p08
ok: the control-plane node rollproof-proof-p08-control-plane has no label topology.kubernetes.io/zone
ok: the control-plane node rollproof-proof-p08-control-plane has the taint node-role.kubernetes.io/control-plane:NoSchedule
ok: worker rollproof-proof-p08-worker has topology.kubernetes.io/zone=rollproof-zone-1
ok: worker rollproof-proof-p08-worker2 has topology.kubernetes.io/zone=rollproof-zone-2
ok: worker rollproof-proof-p08-worker3 has topology.kubernetes.io/zone=rollproof-zone-3
ok: the workers' zones are rollproof-zone-1, rollproof-zone-2 and rollproof-zone-3, once each
ok: rollproof-proof-p08-control-plane: /var/lib/kubelet/config.json holds {"auths":{}}
ok: rollproof-proof-p08-control-plane: containerd config dump has config_path = '/etc/containerd/certs.d'
ok: rollproof-proof-p08-control-plane: crictl version exits 0: RuntimeVersion:  v2.3.4
ok: rollproof-proof-p08-worker: /var/lib/kubelet/config.json holds {"auths":{}}
ok: rollproof-proof-p08-worker: containerd config dump has config_path = '/etc/containerd/certs.d'
ok: rollproof-proof-p08-worker: crictl version exits 0: RuntimeVersion:  v2.3.4
ok: rollproof-proof-p08-worker2: /var/lib/kubelet/config.json holds {"auths":{}}
ok: rollproof-proof-p08-worker2: containerd config dump has config_path = '/etc/containerd/certs.d'
ok: rollproof-proof-p08-worker2: crictl version exits 0: RuntimeVersion:  v2.3.4
ok: rollproof-proof-p08-worker3: /var/lib/kubelet/config.json holds {"auths":{}}
ok: rollproof-proof-p08-worker3: containerd config dump has config_path = '/etc/containerd/certs.d'
ok: rollproof-proof-p08-worker3: crictl version exits 0: RuntimeVersion:  v2.3.4
info: nerdctl info SecurityOptions: ["name=seccomp,profile=builtin" "name=cgroupns" "name=rootless"]; one contains rootless: true
ok: version.Version() is 0.33.0
ok: defaults.Image is kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5
info: nerdctl inspect rollproof-proof-p08-control-plane: Image "sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5", Config.Image "docker.io/kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5"
ok: the server's GitVersion is v1.37.0
ok: l.Down() returned no error
ok: the folder of l.Kubeconfig() is gone
ok: nerdctl ps -a lists no name that starts with rollproof-proof-p08
ok: after Down, $HOME/.kube/config does not exist
p08: all checks passed
```

Its two `lab:` lines, on standard error:

```
lab: Create returned after 25.998s; 0 of 4 nodes Ready
lab: every node Ready after 37.996s
```

`Create` with a wait time of zero returned while no node was Ready, in each of the four runs where it returned; the lab's own wait found every node Ready 12 s later.

The engine check judged the lab's choice, which copies the test of `cluster.DetectNodeProvider` (docker first, then nerdctl, then podman, each by its `-v`, as kind's v0.33.0 source has it); the program does not call `DetectNodeProvider`, whose result names no engine. With `KIND_EXPERIMENTAL_PROVIDER` unset and no Docker, that test picks nerdctl.

The matching `config_path` line, the same in each of the four nodes: containerd 2.3's `config dump` writes it with single quotes.

```
config_path = '/etc/containerd/certs.d'
```

The caller's kubeconfig. `$HOME/.kube/config` did not exist before any run, after Up, or after Down: kind created no such file. In runs 1 to 7 and 9, `$KUBECONFIG` was unset. In run 8 it named a file that existed and one that did not, and both were as before after Up and after Down:

```
info: /tmp/rollproof-p08-kubeconfig/config-a has SHA-256 fe51319cb4765e3994322355d525a143c5788bda0dda4a74891f7e8d32861be2
info: /tmp/rollproof-p08-kubeconfig/config-b does not exist
ok: after Up, /tmp/rollproof-p08-kubeconfig/config-a has SHA-256 fe51319cb4765e3994322355d525a143c5788bda0dda4a74891f7e8d32861be2
ok: after Up, /tmp/rollproof-p08-kubeconfig/config-b does not exist
ok: after Down, /tmp/rollproof-p08-kubeconfig/config-a has SHA-256 fe51319cb4765e3994322355d525a143c5788bda0dda4a74891f7e8d32861be2
ok: after Down, /tmp/rollproof-p08-kubeconfig/config-b does not exist
```

`config-a` held an empty kubeconfig (`apiVersion: v1`, `kind: Config`, no clusters, contexts or users).

What kind leaves on the machine. After the last run, `nerdctl ps -a` lists no container. The engine keeps the network `kind` and the node image:

```
$ nerdctl network ls --format "{{.ID}} {{.Name}}"
0b27c158feb3 kind
17f29b073143 bridge
 host
 none
$ nerdctl images --digests
REPOSITORY            TAG                        DIGEST                                                                     IMAGE ID        CREATED           PLATFORM       SIZE       BLOB SIZE
kindest/node          v1.37.0                    sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5    a1ed56cfb0e7    4 days ago        linux/amd64    953.5MB    388.2MB
kindest/node          v1.37.0                    sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5    a1ed56cfb0e7    4 days ago        linux/amd64    953.5MB    388.2MB
```

Only the lines for kind are shown from `nerdctl images`; the node image is listed twice with one image ID. Both the network and the image were on the machine before these runs, from earlier clusters: kind creates the network with its first cluster and never deletes it. Kind gives each node an anonymous volume at `/var` and deletes the nodes with `nerdctl rm -f -v`; no volume was created during the runs.

## Decision

The settings that worked, for Tasks 20 and 21:

- The provider: `cluster.NewProvider(cluster.ProviderWithNerdctl("nerdctl"))`, after the engine is picked as `cluster.DetectNodeProvider` picks it.
- The description of the Method: no labels on the control-plane node; on each worker `topology.kubernetes.io/zone: rollproof-zone-N`; on every node the mount of the runner's credentials file at `/var/lib/kubelet/config.json`, read-only; the patch that sets `config_path = "/etc/containerd/certs.d"`. On a rootless engine, the `NoNewKeyring = true` patch as well. The credentials file, mode 0600 in a folder of mode 0700, is readable in every node through rootless nerdctl.
- `Create` with `CreateWithV1Alpha4Config`, `CreateWithKubeconfigPath` set to a file in the run's own folder, `CreateWithWaitForReady(0)`, `CreateWithRetain(false)`, `CreateWithDisplayUsage(false)` and `CreateWithDisplaySalutation(false)`. The tool then waits for every node itself: `Create` returned with no node Ready.
- `Delete` with the same kubeconfig path, and the folder removed after the delete: kind's delete creates the folder again, as Task 3 found. The check after Down found the folder gone.
- The nodes are named `CLUSTER-control-plane`, `CLUSTER-worker`, `CLUSTER-worker2` and `CLUSTER-worker3`, and the Kubernetes node names are the same. The control-plane node carries the taint `node-role.kubernetes.io/control-plane:NoSchedule`.
- A check of `config_path` in `containerd config dump` accepts either quote.

The plan's assumption does not hold as written: through nerdctl, `Create` failed in five runs of nine, at kind's own 30-second wait for each node's log. This task changes nothing in the plan, and Task 11 reports the result. What the tool does when `Create` fails this way is not decided here; it is the maintainer's choice.

The cause, found on 2026-11-21 by reading nerdctl's source after step 5's e2e run met this failure four attempts in a row in two subtests, and shown outside a container but not yet caught in a failing run: nerdctl's viewer of a json-file log loses its place when it reads a line that the logger has not finished writing. In nerdctl v2.4.2, and on its main branch at v2.4.3-0.20261118011822-52b705e52dc8, `Decode` in `pkg/logging/jsonfile/jsonfile.go` declares a new `err` in its error branch (`line, err := io.ReadAll(dec.Buffered())`) and returns that one, which is nil. So `viewLogsJSONFileDirect` in `pkg/logging/json_logger.go` never seeks back over the partial line; every later read begins inside a line, fails at once and drops what it read, and `nerdctl logs -f` never prints the lines in those reads. A kind node writes about a hundred lines in its first 0.3 s, as kind's follow starts, and nothing after its boot: when the dropped lines hold `Reached target … Multi-User System`, kind's wait runs out. Shown with nerdctl's own `Decode` and the viewer's handling of its result, outside any container: a file with one line and half of the next, then the rest and the Multi-User line, gives the first line alone. Docker is not affected: kind follows a node's log there with `docker logs -f`.

## Docker

Run once on 2026-11-22, in run 45014226495 of the workflow `.github/workflows/rollproof-proofs.yml` at commit `cd7bc1f6eb0635c75832de73c47464ed9c026b32`, started with `gh workflow run rollproof-proofs.yml --ref rollproof-step6-design -f part=all`: job `proofs` (job 137182961058), its step "kind as a library, on Docker", from 10:48:46 to 10:51:19 UTC.

**Machine and versions.**

- GitHub's hosted runner, image `ubuntu-24.04` version 20261116.376.1, Ubuntu 24.04.5 LTS, runner 2.338.2, by the job's first lines (`Current runner version: '2.338.2'`, `24.04.5`, `Image: ubuntu-24.04`, `Version: 20261116.376.1`).
- Go 1.27.4, which `actions/setup-go` found in the runner's tool cache (`Found in cache @ /opt/hostedtoolcache/go/1.27.4/x64`, `go version go1.27.4 linux/amd64`).
- Engine: Docker, rootful. The log shows `info: docker -v exits 0` and the `SecurityOptions` of `docker info` (below), not Docker's version. The image's software list gives Docker client and server 28.0.4 (`images/ubuntu/Ubuntu2404-Readme.md` of `actions/runner-images` at commit `0e63d4f9d2c29043b4cf1d2f40d1812a7cde1369`, which describes image version 20261116.376.1, lines 76 and 77): read, not shown by the run.
- kind v0.33.0 and its default node image, as the checks print them below; in the nodes, containerd v2.3.4 (`RuntimeVersion` of `crictl version`); Kubernetes v1.37.0, the server's `GitVersion`.

**Method.** The same command, from `projects/rollproof/proofs/` (the job's working folder), with `KIND_EXPERIMENTAL_PROVIDER` unset and no path in `KUBECONFIG` (the program printed a line for no such path):

```
go run ./p08-kind-library
```

The program is the one of the nerdctl runs, unchanged. The lab has changed since: it tries kind's `Create` again, at most 4 attempts in all, when the error is kind's wait for a node's log line, and prints `lab: Create attempt K of 4 failed …` for each retry; and its registries run with a tmpfs. This run printed no retry line, so `Create` took one attempt.

**Result.** Every check passed. The output, with the twelve lines that check each node shown once for the control-plane node; each worker printed the same three lines with its own name:

```
info: $HOME/.kube/config does not exist
info: KIND_EXPERIMENTAL_PROVIDER is unset
info: docker -v exits 0
lab: Create returned after 59.35s; 0 of 4 nodes Ready
lab: every node Ready after 1m14.345s
info: the lab picked docker
ok: docker inspect rollproof-proof-p08-control-plane exits 0
ok: l.Nodes() has four names, the first rollproof-proof-p08-control-plane: rollproof-proof-p08-control-plane rollproof-proof-p08-worker rollproof-proof-p08-worker2 rollproof-proof-p08-worker3
ok: after Up, $HOME/.kube/config does not exist
ok: l.Kubeconfig() exists at /tmp/rollproof-proof-p08-2459203191/kubeconfig and its current context is kind-rollproof-proof-p08
ok: the control-plane node rollproof-proof-p08-control-plane has no label topology.kubernetes.io/zone
ok: the control-plane node rollproof-proof-p08-control-plane has the taint node-role.kubernetes.io/control-plane:NoSchedule
ok: worker rollproof-proof-p08-worker has topology.kubernetes.io/zone=rollproof-zone-1
ok: worker rollproof-proof-p08-worker2 has topology.kubernetes.io/zone=rollproof-zone-2
ok: worker rollproof-proof-p08-worker3 has topology.kubernetes.io/zone=rollproof-zone-3
ok: the workers' zones are rollproof-zone-1, rollproof-zone-2 and rollproof-zone-3, once each
ok: rollproof-proof-p08-control-plane: /var/lib/kubelet/config.json holds {"auths":{}}
ok: rollproof-proof-p08-control-plane: containerd config dump has config_path = '/etc/containerd/certs.d'
ok: rollproof-proof-p08-control-plane: crictl version exits 0: RuntimeVersion:  v2.3.4
info: docker info SecurityOptions: ["name=apparmor" "name=seccomp,profile=builtin" "name=cgroupns"]; one contains rootless: false
ok: version.Version() is 0.33.0
ok: defaults.Image is kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5
info: docker inspect rollproof-proof-p08-control-plane: Image "sha256:24f646398ba8fe575fbdcf6f48831e3a88d0853da8ecc9d2774168070e5a3bea", Config.Image "kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5"
ok: the server's GitVersion is v1.37.0
ok: l.Down() returned no error
ok: the folder of l.Kubeconfig() is gone
ok: docker ps -a lists no name that starts with rollproof-proof-p08
ok: after Down, $HOME/.kube/config does not exist
p08: all checks passed
```

- The engine. With `KIND_EXPERIMENTAL_PROVIDER` unset and `docker -v` passing, the lab picked Docker, as the test of `cluster.DetectNodeProvider` that it copies does (docker first). The program judges the choice only where Docker is absent, so here the line is `info:`, not `ok:`.
- `Create` with a wait time of zero returned after 59.35 s with no node Ready, and the lab's own wait found every node Ready after 1 min 14.345 s, 14.995 s later. The nerdctl runs returned after 21.407 s to 35.931 s and waited 12 s more. Whether the 59.35 s include a pull of the node image, the log does not show.
- Every setting of the Method held: no zone label and the taint on the control-plane node, one zone label on each worker, the credentials file in every node, `config_path` in every node's containerd, and the kubeconfig only in the lab's folder, with `$HOME/.kube/config` absent before, after Up and after Down.
- Rootful. No `SecurityOptions` entry contains `rootless`, so the lab added no `NoNewKeyring` patch (its rule, in the Method above).
- The node image's ID differs by engine. `docker inspect` gives an `Image` ID, `sha256:24f6463…`, that is not the digest of the reference, where nerdctl gave the digest `sha256:a1ed56c…` itself; `Config.Image` names the reference without `docker.io/`.
- `Down` returned no error, its folder was gone, and `docker ps -a` listed no container of the cluster.

The same job then ran `p01-registry-order` on Docker; its record says how.

Plan assumed: "Through nerdctl, kind v0.33.0 as a library creates one control-plane node and three workers with: a zone label on each worker from the cluster description; the containerd patch that sets config_path to /etc/containerd/certs.d; a file of the runner mounted into every node; a kubeconfig written only to the path the tool gives. cluster.DetectNodeProvider picks nerdctl where Docker is absent. With a wait time of zero, Create waits for no node, so the tool waits for every node itself. A rootless engine is shown by a SecurityOptions entry of `ENGINE info` that contains rootless."
Holds: no
Plan assumed: "Through Docker, kind v0.33.0 as a library creates one control-plane node and three workers with: a zone label on each worker from the cluster description; the containerd patch that sets config_path to /etc/containerd/certs.d; a file of the runner mounted into every node; a kubeconfig written only to the path the tool gives. cluster.DetectNodeProvider picks docker where it is present. With a wait time of zero, Create waits for no node, so the tool waits for every node itself."
Holds: yes
