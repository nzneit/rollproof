# Point 24 and the tool's own tests on a hosted runner: the nodes' setting under Docker and Podman, the engine's `save` format, and the first fixture

## Question

Three things that waited for a hosted runner. (1) Point 24 of section 14: the setting `net.netfilter.nf_conntrack_tcp_timeout_syn_sent=5`, which the tool writes in every node, was set on nodes of rootless nerdctl only; do rootful Docker and rootful Podman accept it? (2) The engine's `save` format under Docker, which this folder's README listed as unproved: does the tool's image store read an archive that `docker save` writes? (3) Does the suite's first fixture pass on Docker, on a runner of 2 CPUs, under both Helm majors, and how long does it take there, as the basis for what the suite's workflow will cost?

## Machine and versions

- Three jobs on GitHub's hosted runner, each on a machine of its own: `cluster-tests` (region `westus`), `first-fixture` (`westus3`) and `podman-node-setting` (`westus3`). Each log begins with the same lines: `Current runner version: '2.338.2'`, `Ubuntu`, `24.04.5`, `LTS`, `Image: ubuntu-24.04`, `Version: 20261116.376.1`.
- Go 1.27.4, which `actions/setup-go` found in the runner's tool cache in each job (`Found in cache @ /opt/hostedtoolcache/go/1.27.4/x64`, `go version go1.27.4 linux/amd64`). Its module and build cache was empty in each job (`Cache is not found`).
- The tool's modules, as `go test` downloaded them: `sigs.k8s.io/kind v0.33.0`, `k8s.io/client-go v0.37.2`, `github.com/google/go-containerregistry v0.22.1`. The node image is kind's default, which `TestCreateAndDelete` requires (`c.NodeImage != DefaultNodeImage()` fails it): `kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5`, as `p08-kind-library` printed it on the same day's Docker runner.
- Docker, rootful (the p08 record's Docker run on the same image shows no `rootless` entry in `docker info`). No log of these jobs shows Docker's version. The image's software list gives Docker client and server 28.0.4: `images/ubuntu/Ubuntu2404-Readme.md` of `actions/runner-images` at commit `0e63d4f9d2c29043b4cf1d2f40d1812a7cde1369`, lines 76 and 77. That list describes image version 20261116.376.1 (its line 10), the version the runs used; they do not differ. Read, not shown by the run.
- Helm and kubectl for the first fixture, as `go run ./hack/fetchhelm` printed them: `helm 3.22.1: fetched to .tools/helm-3/helm`, `helm 4.3.2: fetched to .tools/helm-4/helm`, `kubectl 1.37.0: fetched to .tools/kubectl`.
- Podman, rootful, as `sudo podman version` and `sudo podman info` printed it in `podman-node-setting`: `Version:      4.9.3`, `API Version:  4.9.3`, `Go Version:   go1.22.2`; `buildahVersion: 1.33.7`; `rootless: false`; `ociRuntime` `crun version 1.14.1`; conmon `conmon version 2.1.10, commit: unknown`; `networkBackend: netavark`, `version: netavark 1.4.0`, with `aardvark-dns 1.4.0`; `kernel: 7.0.0-1017-azure`; `cgroupVersion: v2`, `cgroupManager: systemd`; `graphDriverName: overlay`; and, under `registries`, `search:` `docker.io` and `quay.io`.
- The machine's size. Podman's info gave `cpus: 2` and `memTotal: 8317931520`. The two Docker jobs' logs show no CPU count; GitHub's documentation gives 2 CPUs and 8 GB for a standard Linux runner of a private repository (`data/reusables/actions/supported-github-runners.md` of `github/docs` at commit `d7487f5debb5a5a2a6e65e6f9fa539266f5f8db6`, lines 113 to 117; read).

## Method

The workflow file is `.github/workflows/rollproof-proofs.yml` at commit `cd7bc1f6eb0635c75832de73c47464ed9c026b32` of the branch `rollproof-step6-design`. Each job checks out the branch, sets up Go 1.27, and runs from `projects/rollproof`. The runs, each once on 2026-11-22, started by these commands (each one command with no shell feature):

```
gh workflow run rollproof-proofs.yml --ref rollproof-step6-design -f part=all
gh workflow run rollproof-proofs.yml --ref rollproof-step6-design -f part=podman-node-setting
```

Run 45014226495 (`part=all`) held `cluster-tests` (job 137182961247) and `first-fixture` (job 137182961220). Run 45014240819 (`part=podman-node-setting`) held `podman-node-setting` (job 137183005365). All three ended in success.

**`cluster-tests`**, with `CGO_ENABLED: "0"` and `KIND_EXPERIMENTAL_PROVIDER: docker` at the job's level:

```
go test -tags cluster -p 1 -count=1 -v -timeout 35m ./internal/cluster/ ./internal/images/ ./internal/probe/ ./internal/watch/
```

- The setting. The tool's `cluster.Create` writes `sysctl -w net.netfilter.nf_conntrack_tcp_timeout_syn_sent=5` in every node after it has written the node settings, and fails with "write the node settings" when the command fails (`internal/cluster/cluster.go`, the constant at lines 45 to 48, the command at lines 280 to 285). So every cluster that `Create` made in these tests took the setting. `TestCreateAndDelete` (`internal/cluster/cluster_test.go`, lines 136 to 255) makes a cluster of one control-plane node and three workers and checks the nodes, their zones, CoreDNS, the namespaces, the kubeconfig and the node image; it pushes an image to the run's registry and pulls it with `crictl pull` in a worker; and then, in every node, it runs `sysctl -n net.netfilter.nf_conntrack_tcp_timeout_syn_sent` and fails unless the output is `5` (lines 229 to 234). It ends with `Delete` and checks that no container and no run folder is left. The package's `TestMain` prints a line `kind Create of NAME, attempt N: RESULT` for each call of kind's `Create`, where N counts the calls for that name (lines 44 to 63).
- The `save` format. The images tests (`internal/images/images_test.go`) share one cluster with one worker, whose nodes try the run's registry first for the `as` hosts `registry.example:5000` and `docker.io` (lines 34 to 53). Each test builds, with the engine, an image `FROM scratch` that holds a small static Go program (lines 66 to 86):
  - `TestStoreOwnAndLocal` (lines 88 to 161) gives the tool's `Store` the image as `local:`; `Store` has the engine write it with `docker save -o FILE -- IMAGE` (`internal/images/local.go`, line 34) and reads that archive. The test requires the repository `rollproof.test/app` and a `sha256:` digest, the image in every node (`crictl inspecti`), the tool's own probe image in the control-plane node, no tag in the run's registry, and a pod of the image that runs with an `imageID` that holds the digest.
  - `TestStoreArchiveIntoEveryNode` (lines 163 to 199) writes the archive itself with `docker save -o app.tar IMAGE` and gives it as `archive:`, with the same requirements apart from the pod and the probe image.
  - `TestStoreAsIntoEveryNode` (lines 204 to 243) stores one such archive under two `as` names, `registry.example:5000/team/app` and `docker.io/rollproof/app`, and requires each in every node.

  `Store` reads an archive with go-containerregistry's `tarball` package (`internal/images/archive.go`, lines 26 and 36).
- The rest: the other tests of the four packages, among them the probe's cluster tests, one of which, `TestPodOnACluster`, prints the clock check.

**`first-fixture`**, with the same job-level variables:

```
go run ./hack/fetchhelm
go test -tags e2e -p 1 -count=1 ./e2e/ -run '^TestFirstFixture$' -v -timeout 40m
```

The harness's `TestMain` (`e2e/harness_test.go`, lines 60 to 96) builds the tool, builds the two fixture images and the fixture probe's image with the engine, fetches busybox's archive, and saves the fixture images as archives. `TestFirstFixture` (`e2e/fixtures_test.go`, lines 25 to 66) runs the tool's binary, `rollproof run -f FILE --out DIR --name e2e`, once with Helm 3 and once with Helm 4 linked first on the PATH beside kubectl, each run under a limit of 15 minutes (`runFixture`, `e2e/harness_test.go`, from line 304, the limit at line 353). The first fixture names its two images as `local:` (`e2e/config_test.go`, lines 167 to 174), so they too reach the tool through `docker save`. The test requires exit 0, the verdict `pass` with the windows steady, deploy and rollback passing; `deployment/orders` in both transitions with 2 of 2 pods replaced, probed, and a quickest stop of 5000 ms or more; an install that settled 15 s or more after the probe's first observation; and a steady window that starts when the install settled. It logs the install window's failed checks, which the tool does not judge.

**`podman-node-setting`**, with no engine variable at the job's level. Its steps, after the checkout and Go: `go mod download` as the runner's user; then

```
sudo podman version
sudo podman info
sudo mkdir -p /etc/containers/registries.conf.d
printf '[aliases]\n"kindest/node" = "docker.io/kindest/node"\n' | sudo tee /etc/containers/registries.conf.d/99-rollproof-kind-node.conf
```

and then the one test, as root, with the user's PATH and Go settings and a build cache of its own:

```
sudo env "PATH=$PATH" "GOPATH=$(go env GOPATH)" "GOMODCACHE=$(go env GOMODCACHE)" \
  "GOCACHE=$RUNNER_TEMP/go-build-root" "GOTOOLCHAIN=$(go env GOTOOLCHAIN)" \
  CGO_ENABLED=0 KIND_EXPERIMENTAL_PROVIDER=podman \
  go test -tags cluster -p 1 -count=1 -run '^TestCreateAndDelete$' -v -timeout 25m ./internal/cluster/
```

These are the workflow's lines, run by bash in the job; the record shows them as the job ran them, not as a command for a person. `KIND_EXPERIMENTAL_PROVIDER=podman` picks the engine. The alias file was written because the image's `registries.conf` lists two search registries (`docker.io` and `quay.io`, above), and the tool hands kind's default node image to the engine's pull by its short name. `cluster.Create` pulls `DefaultNodeImage()` when the run names no node image (`internal/cluster/cluster.go`, lines 225 to 233), which is kind's `defaults.Image` (lines 117 to 120), `kindest/node:v1.37.0@sha256:…` (kind v0.33.0, `pkg/apis/config/defaults/image.go`, line 21); the pull is `ENGINE pull IMAGE` with that string as it is (`internal/cluster/engine.go`, lines 49 to 64, the pull at line 59). kind's own Podman provider, by contrast, puts `docker.io/` in front of a name with no registry host before it pulls (kind v0.33.0, `pkg/cluster/internal/providers/podman/images.go`, lines 84 to 115, `defaultDomain = "docker.io/"` at line 88). All read in the source.

## Result

**The setting under rootful Docker.** Every test of the four packages passed, and none was skipped: the log has 715 `--- PASS` lines and no `--- FAIL` or `--- SKIP` line. The four packages:

```
ok  	rollproof/internal/cluster	316.254s
ok  	rollproof/internal/images	59.538s
ok  	rollproof/internal/probe	166.644s
ok  	rollproof/internal/watch	101.723s
```

`TestCreateAndDelete`, whose loop read `5` in each of the four nodes, and the six other clusters of `internal/cluster`, each made on kind's first call for its name (the second line for `rollproof-test-leftovers` is that test's second cluster):

```
2026-11-22T10:51:55.9736143Z === RUN   TestCreateAndDelete
2026-11-22T10:52:44.7416988Z kind Create of rollproof-test-create, attempt 1: succeeded
2026-11-22T10:53:09.9464439Z --- PASS: TestCreateAndDelete (73.97s)
2026-11-22T10:53:36.4921887Z kind Create of rollproof-test-leftovers, attempt 1: succeeded
2026-11-22T10:54:16.1950123Z kind Create of rollproof-test-leftovers, attempt 2: succeeded
2026-11-22T10:54:58.2590412Z kind Create of rollproof-test-delete, attempt 1: succeeded
2026-11-22T10:55:42.4542084Z kind Create of rollproof-test-nokeep, attempt 1: succeeded
2026-11-22T10:56:12.3030982Z kind Create of rollproof-test-keep, attempt 1: succeeded
2026-11-22T10:56:46.9710990Z kind Create of rollproof-test-registries, attempt 1: succeeded
```

The clusters of the images, probe and watch tests were made by the same `Create`, so they took the setting too; only `TestCreateAndDelete` reads it back.

**The setting under rootful Podman.** The one test passed, with kind's `Create` on its first call:

```
2026-11-22T10:52:04.5503520Z === RUN   TestCreateAndDelete
2026-11-22T10:53:00.2976924Z kind Create of rollproof-test-create, attempt 1: succeeded
2026-11-22T10:53:30.6824988Z --- PASS: TestCreateAndDelete (86.13s)
2026-11-22T10:53:30.6833820Z ok  	rollproof/internal/cluster	86.147s
```

So under Podman too the cluster was made, every node read `5`, a worker pulled from the run's registry with `crictl pull`, and `Delete` left no container. The build as root, with an empty build cache, took 174.4 s, from the step's first line (10:49:10.1658829) to the test's start.

**The `save` format under Docker.** The three tests that store an archive written by `docker save`, and the one that stores an image the engine lacks, passed:

```
--- PASS: TestStoreOwnAndLocal (55.06s)
--- PASS: TestStoreArchiveIntoEveryNode (0.93s)
--- PASS: TestStoreAsIntoEveryNode (1.35s)
--- PASS: TestStoreUnknownLocalImage (0.33s)
```

`TestStoreOwnAndLocal`'s time holds the creation of the package's cluster.

**The first fixture on Docker.** Both subtests passed:

```
2026-11-22T10:51:02.3842317Z === RUN   TestFirstFixture
--- PASS: TestFirstFixture (304.08s)
    --- PASS: TestFirstFixture/helm-3 (166.16s)
    --- PASS: TestFirstFixture/helm-4 (137.92s)
ok  	rollproof/e2e	403.526s
```

The console under Helm 3, in short:

```
rollproof: PASS   orders (Helm) on Kubernetes v1.37.0 in kind
install     settled after 15.0 s under the probe; failures not judged
            3,396 checks, 0 failed
            slowest 28 ms, behind 3 ms

window     length   checks   failed   after finish   slowest   behind   result
steady     10.0 s    2,000        0              -    191 ms   122 ms   pass
deploy     28.1 s    5,600        0              0     62 ms    36 ms   pass
rollback   27.3 s    5,292        0              0    122 ms    96 ms   pass

transition  workload            probed   pods replaced   quickest stop
deploy      deployment/orders   yes      2 of 2          5.9 s
rollback    deployment/orders   yes      2 of 2          5.2 s
```

Under Helm 4, in short:

```
rollproof: PASS   orders (Helm) on Kubernetes v1.37.0 in kind
install     settled after 15.0 s under the probe; failures not judged
            3,196 checks, 7 failed (refused 6, timeout 1), the last 14.6 s before the steady window
            slowest 1085 ms, behind 27 ms

window     length   checks   failed   after finish   slowest   behind   result
steady     10.0 s    2,000        0              -     52 ms    17 ms   pass
deploy     27.8 s    5,400        0              0     70 ms    43 ms   pass
rollback   26.1 s    5,200        0              0     70 ms    47 ms   pass

transition  workload            probed   pods replaced   quickest stop
deploy      deployment/orders   yes      2 of 2          5.4 s
rollback    deployment/orders   yes      2 of 2          5.2 s
```

The test's own lines on the install window:

```
fixtures_test.go:60: install window: 3396 checks, none failed
fixtures_test.go:62: install window: 7 of 3196 checks failed map[refused:6 timeout:1], the last 14.617s before the steady window
```

The times, against the developer's machine. Step 5's last run of the whole suite on the developer's machine (rootless nerdctl 2.4.2, 16 CPUs) gave `--- PASS: TestFirstFixture (274.35s)`, with `helm-3` 137.77 s and `helm-4` 136.58 s; over the whole suite it gave 4,297 s for the 32 `helm-3` subtests, 4,411 s for the 32 `helm-4` subtests and 119 s for the one `control` subtest. Those figures are from that run's log, which is not in this repository. Here `helm-3` took 166.16 / 137.77 = 1.21 times as long, `helm-4` 137.92 / 136.58 = 1.01 times, and the whole test 304.08 / 274.35 = 1.11 times.

The job's wall time, by its log, from its first line (10:48:44.1453219) to its last (10:56:10.8320113): 446.7 s, 7 min 26.7 s, billed as 8 minutes, because GitHub rounds each job up to a whole minute (`content/billing/reference/actions-runner-pricing.md` of `github/docs` at the commit above, line 14; read). Its part that does not depend on the number of fixtures: 138.2 s from its first line to the test's start (6.2 s for the checkout and Go, 20.2 s for `fetchhelm`, from 10:48:50.3252745 to 10:49:10.5536151, and 111.8 s, from the test step's first line at 10:49:10.6309002, to build the test and run the harness's `TestMain`), and 4.4 s after the test (from 10:56:06.4627992), 142.6 s in all, with an empty Go cache. The other two jobs, by the same count: `cluster-tests` from 10:48:45.0869651 to 11:04:21.7345143, 15 min 36.6 s, billed as 16 minutes; `podman-node-setting` from 10:48:56.1637072 to 10:53:36.9538895, 4 min 40.8 s, billed as 5 minutes.

**The clock check under Docker.** `TestPodOnACluster` printed:

```
pod_cluster_test.go:159: clock check: the first observation was read 1.342107ms after the runtime stamped it
```

The README's figure for rootless nerdctl is 176.461 µs; the bound is 2 s.

## Decision

- **Point 24.** The sentence "Docker and Podman are not tried" is settled. Every node read 5 under rootful Docker, where all the tests with the `cluster` tag passed, and under rootful Podman 4.9.3, in one test: the cluster's creation, the setting, a pull from the run's registry, and the delete. Podman's standing in section 9 of the design is not changed by one test.
- **The `save` format under Docker** holds, as far as the images tests assert it: an archive that `docker save` wrote, given as `local:` or as `archive:`, and stored under the tool's own repository or under an `as` name, was stored by `Store` with a `sha256:` digest, was held by every node, left no tag in the run's registry, and ran in a pod whose `imageID` held the digest. The first fixture's two images reached the tool by the same route.
- **The first fixture** passes on Docker on a 2-CPU hosted runner under both Helm majors. The time for each Helm major is about the developer's machine's (1.21 and 1.01 times), so the plan for step 6 sizes the suite's workflow from the suite's time there. With two jobs, one for each Helm major, and the `control` subtest in the Helm 3 job: at 1.0 times the developer's machine, 4,297 + 119 + 142.6 = 4,558.6 s (76 billed minutes) and 4,411 + 142.6 = 4,553.6 s (76 minutes), 152 minutes for a run of the suite; at 1.2 times, 1.2 × 4,416 + 142.6 = 5,441.8 s (91 minutes) and 1.2 × 4,411 + 142.6 = 5,435.8 s (91 minutes), 182 minutes. At GitHub's rate of $0.006 a minute for a Linux runner of 2 cores (`data/reusables/billing/actions-standard-runner-prices.md` of `github/docs` at the commit above, line 4; read), that is about $0.91 and $1.09 a run in a private repository. This rests on one fixture and one run.
- **A finding for the plan for step 6.** Under Podman the tool pulls kind's default node image by its short name, `kindest/node:…`, where kind's own Podman provider adds `docker.io/`. The run had an alias for it, so whether the pull fails without one was not measured.

What was not measured: the rest of the suite on Docker; every test under Podman but `TestCreateAndDelete`; Podman's pull of the node image with no alias; rootless Docker; Docker 29 on the `ubuntu-26.04` image; a second run of anything here.

Plan assumed: "Rootful Docker on GitHub's hosted runner accepts the nodes' setting net.netfilter.nf_conntrack_tcp_timeout_syn_sent=5, and every node reads 5."
Holds: yes
Plan assumed: "Rootful Podman accepts the same setting."
Holds: yes
Plan assumed: "An image archive that `docker save` writes is one that the tool's image store reads."
Holds: yes
Plan assumed: "The first fixture passes on Docker, on a hosted runner of 2 CPUs, under Helm 3.22.1 and under Helm 4.3.2."
Holds: yes
Plan assumed: "Under Podman the tool's pull of kind's default node image works with no alias for its short name."
Holds: not measured here
