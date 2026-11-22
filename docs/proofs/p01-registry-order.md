# Point 1, first half: registry order in the nodes, and how a node pulls

## Question

Point 1 of section 14: a node's containerd settings for a registry host can list the run's registry first, with `capabilities = ["pull"]`, and the real host after it. What does a pull then do with a reference by digest, by tag, and by both, and with an image that the run's registry lacks? Which command has a node pull through those settings, as the kubelet's pulls do? And does a pod's status name the digest that its container runs?

## Machine and versions

- System: the developer's machine, Linux 7.3.3, amd64, 16 CPUs.
- Engine: nerdctl 2.4.2, rootless. The p08 record lists its containerd, runc and rootlesskit. Docker is absent. The engine is rootless, so the lab adds the `NoNewKeyring = true` patch.
- Go 1.27.3, for the program and for the five binaries.
- kind: `sigs.k8s.io/kind` v0.33.0; `k8s.io/client-go` v0.37.2; `github.com/google/go-containerregistry` v0.22.1, which pushes the images.
- Node image: kind's default, `kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5`, which is Kubernetes v1.37.0, as the p08 record read it. In the worker, `ctr --version` gives `ctr github.com/containerd/containerd/v2 v2.3.4` and `crictl --version` gives `crictl version 1.36.0`.
- Registries: `docker.io/library/registry:3`, held in the engine's store as `registry@sha256:82f6ff4a7f982960c8aa51617fea3aabc485df6e55cce80d22deb1de36f1bc3c`. Its log gives `version=3.1.2` and `go.version=go1.26.8`.
- Sources read: section 9 and section 14 point 1 of the design; `docs/hosts.md` of containerd at v2.3.4; `site/static/examples/kind-with-registry.sh` of kind at v0.33.0. To explain case e and the settings form: `core/remotes/docker/resolver.go`, `core/remotes/docker/config/hosts.go` and `internal/cri/server/images/image_pull.go` of containerd at v2.3.4.

## Method

The program is `proofs/p01-registry-order/main.go`.

**Five images.** In a new temporary folder, `$TMP/…`, the program writes `go.mod` (`module p01app`, a blank line, `go 1.27`) and a `main.go` that prints the variable `v` (default `unset`) and then sleeps for an hour. It builds that module five times in the folder, with `CGO_ENABLED=0` and `GOOS=linux` added to the environment, for V = `x`, `y`, `y2`, `z` and `w`:

```
go build -trimpath -ldflags "-s -w -X main.v=V" -o $TMP/…/V .
```

`lab.PushBinary` packs each binary as `/app` in an image of one layer, with a Docker schema 2 manifest, and pushes it by digest. Runs 1 and 4 gave the same five digests:

| Image | Digest | Registry | Repository |
| --- | --- | --- | --- |
| X | `sha256:8ed9d7d8f2218a59c7fa3fb3b9d59843dfba0f58794fc7902293dc1d25c198ce` | run | `team/app` |
| Y | `sha256:a257ab89a6eb1c8ea9355d8346a60765b7f5d7633f72b842019e923da6e9750a` | run | `team/app` |
| Y2 | `sha256:b24a7ca6014846cd9870a3ad8613195f18e20ae04ddfa1549230292c05d44978` | real | `team/app` |
| Z | `sha256:a78dd21637928e1865d3e49bb37a890463329b340b493bf3746258748ec1ef97` | real | `team/app` |
| W | `sha256:e21e3313a4f09fb3bc0a9a5f8142d791568e7b08d795a7a627f4a4702e6686e6` | run | `probe/rollproof` |

The program then writes two tags, with `remote.Image` of the digest reference and `remote.Write` to `name.NewTag(PUSH+"/team/app:v1", name.Insecure)`. After that, `team/app:v1` names Y in run and Y2 in real. The proof uses a tag here; the tool never does.

**The cluster.** `lab.Up(ctx, "p01", cfg)` makes one control-plane node and one worker, with the patch that the p08 record found working:

```toml
[plugins."io.containerd.grpc.v1.cri".registry]
  config_path = "/etc/containerd/certs.d"
```

**The registries.** `l.Registry(ctx, "run")` and `l.Registry(ctx, "real")` start the containers `rollproof-proof-p01-run` and `rollproof-proof-p01-real` on the network `kind`. Each has a tmpfs at `/var/lib/registry`, from this task's change to the lab. `real` stands for the real registry of the made-up host `registry.example`.

**The settings.** After every node is Ready, the program runs `mkdir -p /etc/containerd/certs.d/registry.example` in every node and writes `hosts.toml` there with `cp /dev/stdin`, both through `l.Exec`. Nothing restarts containerd. This is the file as run 4 wrote it, where `10.4.1.207` is run and `10.4.1.208` is real:

```toml
server = "http://10.4.1.208:5000"
[host."http://10.4.1.207:5000"]
  capabilities = ["pull"]
```

Case e writes `/etc/containerd/certs.d/rollproof.test/hosts.toml` in the same way, with only:

```toml
[host."http://10.4.1.207:5000"]
  capabilities = ["pull"]
```

**What each case observes.** Before a pull, the program reads both registries' logs (`nerdctl logs`) until two reads 500 ms apart agree, and notes their lengths. After the pull it reads them again in the same way. It then prints each new request under `/v2/` as `info: CASE run: METHOD PATH STATUS`, or `info: CASE real: …`. registry:3 writes an access-log line for every request, and those lines are what the program reads. A check compares a path without its query, because containerd adds `?ns=HOST` to every request it sends to a host other than the one in the image's name (hosts.md, "Registry Host Namespace"). The printed path keeps the query.

**The pulls.** Before each case from a to f, `crictl rmi REFERENCE` in the worker removes the image that the case pulls, and an error is ignored. Every pull is `crictl pull REFERENCE` in the worker, through `l.Exec`. The exceptions are the `ctr` of case f and the kubelet's pulls in case g.

**The cases.**

- a: pull `registry.example/team/app@X`.
- b: pull `registry.example/team/app:v1`, then read `status.repoDigests` from `crictl inspecti -o json`.
- c: pull `registry.example/team/app@Z`.
- d: pull `registry.example/team/app:whatever@X`.
- e: write the settings for `rollproof.test`. Pull `rollproof.test/probe/rollproof@W`, then the same repository at `sha256:` followed by 64 zeros. Time the second pull, and print the lines of containerd's journal in the worker that name that digest.
- f: `ctr -n k8s.io images pull registry.example/team/app@X` in the worker.
- g: create the namespace `p01` and a pod `probe-w` with `nodeName` set to the worker, one container `app` of `rollproof.test/probe/rollproof@W`, `imagePullPolicy: IfNotPresent` and `restartPolicy: Never`. Wait, at most 2 minutes, until it runs, and read `status.containerStatuses[0].imageID`. Then run `crictl rmi registry.example/team/app@X`, and create a second pod, `kubelet-x`, the same but with `registry.example/team/app@X`.

The program then calls `Down`. A case that did not run to its end counts as a failed check, and so does an interrupt. Either one stops the line `p01: all checks passed`.

The command, from `projects/rollproof/proofs/`:

```
go run ./p01-registry-order
```

The program ran four times on 2026-11-17, between 00:32 and 00:39 UTC, one cluster at a time. `nerdctl ps -a` listed no container whose name begins with `rollproof-` before or after any run. Run 1 used the program before one edit: in case e it did not yet print containerd's journal, and nothing else differed. Runs 2 and 3 tested an interrupt, with the committed program built as a binary in `$TMP/…`. Run 2 ran under `timeout --preserve-status --signal=INT 55`, which sends SIGINT to the program's process group after 55 s. Run 3 ran from a script that sent SIGINT to the program's process alone once case b's line appeared. Run 4 is the plain command on the program as committed.

## Result

| Run | Program | Create attempts of `Up` | Last line | Exit | `nerdctl volume ls -q` before, after |
| --- | --- | --- | --- | --- | --- |
| 1 | before the journal lines | 1 | `p01: all checks passed` | 0 | 57, 57 |
| 2 | as committed; SIGINT to the group at 55 s | 2, and `Up` was then interrupted while it waited for Ready nodes | `p01: 9 checks failed` | 1 | 57, 57 |
| 3 | as committed; SIGINT to the process after case b | 1 | `p01: 5 checks failed` | 1 | 57, 57 |
| 4 | as committed | 2 | `p01: all checks passed` | 0 | 57, 57 |

The Create attempts are counted from the lab's lines `lab: Create attempt K of 4 failed at kind's wait for a node's log line; trying again`. A run with no such line took one attempt. These are run 4's lab lines:

```
lab: Create attempt 1 of 4 failed at kind's wait for a node's log line; trying again
lab: Create returned after 19.007s; 0 of 2 nodes Ready
lab: every node Ready after 31.005s
```

Runs 1 and 4 print the same lines, apart from addresses, ports, times, run 4's retry line from the lab, and the journal lines that run 1 lacks. The quotes below are from run 4.

**a. By digest, an image that the run's registry holds.** The run's registry served the manifest and both blobs, and the real host was not asked:

```
info: a run: HEAD /v2/team/app/manifests/sha256:8ed9d7d8f2218a59c7fa3fb3b9d59843dfba0f58794fc7902293dc1d25c198ce?ns=registry.example 200
info: a run: GET /v2/team/app/manifests/sha256:8ed9d7d8f2218a59c7fa3fb3b9d59843dfba0f58794fc7902293dc1d25c198ce?ns=registry.example 200
info: a run: GET /v2/team/app/blobs/sha256:ce8561bccaa50fe732388994597cd92c6a337d3063b2ae4981e799137713bdf4?ns=registry.example 200
info: a run: GET /v2/team/app/blobs/sha256:8186882a23aab01c72209ccbc8f41b22e9b20b06f8190a4e14339100b3963ec1?ns=registry.example 200
info: a real: no new request
ok: a: crictl pull registry.example/team/app@sha256:8ed9d7d8f2218a59c7fa3fb3b9d59843dfba0f58794fc7902293dc1d25c198ce succeeded; run answered /v2/team/app/manifests/X with 200; real had no new request
```

**b. By tag.** The tag was resolved only at the real host, by `HEAD /v2/team/app/manifests/v1`, and the node ran Y2. The run's registry holds `team/app:v1` (as Y), but it was never asked for the tag. It was asked first for the content of Y2 by digest, and it answered 404 each time:

```
info: b run: GET /v2/team/app/manifests/sha256:b24a7ca6014846cd9870a3ad8613195f18e20ae04ddfa1549230292c05d44978?ns=registry.example 404
info: b run: GET /v2/team/app/blobs/sha256:2e8d9cdd6a00ea1f4642c6d0972c871ad2e811d58355dcc9dc7cc11ed3ae6087?ns=registry.example 404
info: b run: GET /v2/team/app/blobs/sha256:8f9aaafa139e10db0917b1f7747d7e2a7f617f90870809b7a163c7808c2413ea?ns=registry.example 404
info: b real: HEAD /v2/team/app/manifests/v1?ns=registry.example 200
info: b real: GET /v2/team/app/manifests/sha256:b24a7ca6014846cd9870a3ad8613195f18e20ae04ddfa1549230292c05d44978?ns=registry.example 200
info: b real: GET /v2/team/app/blobs/sha256:2e8d9cdd6a00ea1f4642c6d0972c871ad2e811d58355dcc9dc7cc11ed3ae6087?ns=registry.example 200
info: b real: GET /v2/team/app/blobs/sha256:8f9aaafa139e10db0917b1f7747d7e2a7f617f90870809b7a163c7808c2413ea?ns=registry.example 200
info: b: status.repoDigests: registry.example/team/app@sha256:b24a7ca6014846cd9870a3ad8613195f18e20ae04ddfa1549230292c05d44978
ok: b: crictl pull registry.example/team/app:v1 succeeded; status.repoDigests lists registry.example/team/app@Y2; run had no request for /v2/team/app/manifests/v1
```

**c. By digest, an image that only the real host holds.** For the manifest and for each blob, the run's registry was asked first and answered 404, and the real host then answered 200:

```
info: c run: HEAD /v2/team/app/manifests/sha256:a78dd21637928e1865d3e49bb37a890463329b340b493bf3746258748ec1ef97?ns=registry.example 404
info: c run: GET /v2/team/app/manifests/sha256:a78dd21637928e1865d3e49bb37a890463329b340b493bf3746258748ec1ef97?ns=registry.example 404
info: c run: GET /v2/team/app/blobs/sha256:7cf09579d7fdcd9797951a5f822db366103e6f80d8b64b18804822df49d3edad?ns=registry.example 404
info: c run: GET /v2/team/app/blobs/sha256:106ee136b416a84f16b60c3c6de5e883ab1bc13e575b5819b89cda6d32398afc?ns=registry.example 404
info: c real: HEAD /v2/team/app/manifests/sha256:a78dd21637928e1865d3e49bb37a890463329b340b493bf3746258748ec1ef97?ns=registry.example 200
info: c real: GET /v2/team/app/manifests/sha256:a78dd21637928e1865d3e49bb37a890463329b340b493bf3746258748ec1ef97?ns=registry.example 200
info: c real: GET /v2/team/app/blobs/sha256:7cf09579d7fdcd9797951a5f822db366103e6f80d8b64b18804822df49d3edad?ns=registry.example 200
info: c real: GET /v2/team/app/blobs/sha256:106ee136b416a84f16b60c3c6de5e883ab1bc13e575b5819b89cda6d32398afc?ns=registry.example 200
ok: c: crictl pull registry.example/team/app@sha256:a78dd21637928e1865d3e49bb37a890463329b340b493bf3746258748ec1ef97 succeeded; run answered a path that ends in Z with 404; real answered /v2/team/app/manifests/Z with 200
```

The program does not compare times across the two logs. The order of the hosts is the order of the settings file: containerd's resolver tries the hosts in that order for each path (resolver.go, lines 295 to 304).

**d. A tag and a digest.** The pull asked only for X by digest, from the run's registry, and no registry was asked for the tag. containerd recorded no tag for the image:

```
info: d run: HEAD /v2/team/app/manifests/sha256:8ed9d7d8f2218a59c7fa3fb3b9d59843dfba0f58794fc7902293dc1d25c198ce?ns=registry.example 200
info: d run: GET /v2/team/app/manifests/sha256:8ed9d7d8f2218a59c7fa3fb3b9d59843dfba0f58794fc7902293dc1d25c198ce?ns=registry.example 200
info: d run: GET /v2/team/app/blobs/sha256:ce8561bccaa50fe732388994597cd92c6a337d3063b2ae4981e799137713bdf4?ns=registry.example 200
info: d run: GET /v2/team/app/blobs/sha256:8186882a23aab01c72209ccbc8f41b22e9b20b06f8190a4e14339100b3963ec1?ns=registry.example 200
info: d real: no new request
info: d: status.repoTags: []; status.repoDigests: ["registry.example/team/app@sha256:8ed9d7d8f2218a59c7fa3fb3b9d59843dfba0f58794fc7902293dc1d25c198ce"]
ok: d: crictl pull registry.example/team/app:whatever@sha256:8ed9d7d8f2218a59c7fa3fb3b9d59843dfba0f58794fc7902293dc1d25c198ce succeeded; neither registry had a request whose path ends in /manifests/whatever
```

**e. `rollproof.test`, with only the run's registry listed.** The settings file, written into the running nodes just before the pull, was used, and the run's registry served W:

```
info: e run: HEAD /v2/probe/rollproof/manifests/sha256:e21e3313a4f09fb3bc0a9a5f8142d791568e7b08d795a7a627f4a4702e6686e6?ns=rollproof.test 200
info: e run: GET /v2/probe/rollproof/manifests/sha256:e21e3313a4f09fb3bc0a9a5f8142d791568e7b08d795a7a627f4a4702e6686e6?ns=rollproof.test 200
info: e run: GET /v2/probe/rollproof/blobs/sha256:0e8d26e595561b39be4572dfc13af23f70d50f8ded3465e96045f3bd780fd0ab?ns=rollproof.test 200
info: e run: GET /v2/probe/rollproof/blobs/sha256:e18fb82e878aac8294c940d2d7fb00ff3c9567fe1b4260f8beacebc54e0d7eac?ns=rollproof.test 200
info: e real: no new request
ok: e: crictl pull rollproof.test/probe/rollproof@sha256:e21e3313a4f09fb3bc0a9a5f8142d791568e7b08d795a7a627f4a4702e6686e6 succeeded
```

The pull of the zero digest failed. The program's timer covers the whole `l.Exec`, the `nerdctl exec` of `crictl pull`, which returned non-zero after 56 ms (92 ms in run 1). containerd's own PullImage took about 20 ms, by the journal below. The run's registry answered 404 for the manifest path and then for the blob path:

```
info: e-zero run: HEAD /v2/probe/rollproof/manifests/sha256:0000000000000000000000000000000000000000000000000000000000000000?ns=rollproof.test 404
info: e-zero run: HEAD /v2/probe/rollproof/blobs/sha256:0000000000000000000000000000000000000000000000000000000000000000?ns=rollproof.test 404
info: e-zero real: no new request
info: e-zero: crictl pull rollproof.test/probe/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000 failed after 56ms; its output:
```

The error text, as `crictl` printed it:

```
E1117 00:38:53.633656    1059 remote_image.go:250] "PullImage from image service failed" err="rpc error: code = NotFound desc = failed to pull and unpack image \"rollproof.test/probe/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000\": failed to resolve reference \"rollproof.test/probe/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000\": rollproof.test/probe/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000: not found" image="rollproof.test/probe/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000"
time="2026-11-17T00:38:53Z" level=fatal msg="pulling image: rpc error: code = NotFound desc = failed to pull and unpack image \"rollproof.test/probe/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000\": failed to resolve reference \"rollproof.test/probe/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000\": rollproof.test/probe/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000: not found"
time="…" level=fatal msg="exec failed with exit code 1"
```

The last line is nerdctl's, and its time is left out here. The error says only "not found". containerd's journal shows that the pull also tried `https://rollproof.test`, where the name did not resolve. From the `PullImage` line to its `failed` line is 20 ms:

```
info: e-zero: containerd: time="2026-11-17T00:38:53.613515796Z" level=info msg="PullImage \"rollproof.test/probe/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000\""
info: e-zero: containerd: time="2026-11-17T00:38:53.626068804Z" level=info msg="trying next host after status: 404 Not Found" host="10.4.1.207:5000" method=HEAD url="http://10.4.1.207:5000/v2/probe/rollproof/manifests/sha256:0000000000000000000000000000000000000000000000000000000000000000?ns=rollproof.test"
info: e-zero: containerd: time="2026-11-17T00:38:53.628989491Z" level=info msg="fetch failed" error="failed to do request: Head \"https://rollproof.test/v2/probe/rollproof/manifests/sha256:0000000000000000000000000000000000000000000000000000000000000000\": dial tcp: lookup rollproof.test on 10.0.2.3:53: no such host" host=rollproof.test method=HEAD url="https://rollproof.test/v2/probe/rollproof/manifests/sha256:0000000000000000000000000000000000000000000000000000000000000000"
info: e-zero: containerd: time="2026-11-17T00:38:53.629225503Z" level=info msg="trying next host after status: 404 Not Found" host="10.4.1.207:5000" method=HEAD url="http://10.4.1.207:5000/v2/probe/rollproof/blobs/sha256:0000000000000000000000000000000000000000000000000000000000000000?ns=rollproof.test"
info: e-zero: containerd: time="2026-11-17T00:38:53.632056882Z" level=info msg="fetch failed" error="failed to do request: Head \"https://rollproof.test/v2/probe/rollproof/blobs/sha256:0000000000000000000000000000000000000000000000000000000000000000\": dial tcp: lookup rollproof.test on 10.0.2.3:53: no such host" host=rollproof.test method=HEAD url="https://rollproof.test/v2/probe/rollproof/blobs/sha256:0000000000000000000000000000000000000000000000000000000000000000"
info: e-zero: containerd: time="2026-11-17T00:38:53.633553859Z" level=error msg="PullImage \"rollproof.test/probe/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000\" failed" error="rpc error: code = NotFound desc = failed to pull and unpack image \"rollproof.test/probe/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000\": failed to resolve reference \"rollproof.test/probe/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000\": rollproof.test/probe/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000: not found"
```

The source explains both parts. A settings file with no `server` line gets, as its last host, containerd's own default for the name in the image, with every capability (hosts.go, lines 95 to 135). For `rollproof.test`, which is neither `docker.io` nor a localhost name, that default is `https://rollproof.test`. In the resolver, a 404 outranks a later failure to connect when the error is chosen (resolver.go, lines 280 to 341). So the error names the run registry's 404 and says nothing about `https://rollproof.test`.

**f. `ctr`.** `ctr` did not use the node's settings. It went straight to `https://registry.example`, whose name did not resolve, and neither registry had a request:

```
info: f run: no new request
info: f real: no new request
info: f: ctr -n k8s.io images pull registry.example/team/app@sha256:8ed9d7d8f2218a59c7fa3fb3b9d59843dfba0f58794fc7902293dc1d25c198ce: exit code 1 (nerdctl reports 1 for any non-zero code); its output:
info: f:   ctr: failed to resolve image: failed to do request: Head "https://registry.example/v2/team/app/manifests/sha256:8ed9d7d8f2218a59c7fa3fb3b9d59843dfba0f58794fc7902293dc1d25c198ce": dial tcp: lookup registry.example on 10.0.2.3:53: no such host
ok: f: ctr ran in the worker and both logs were read: exit code 1; 0 new requests in run, 0 in real
```

The exit code is 1 as nerdctl reports it. nerdctl reports 1 for any non-zero code, so `ctr`'s own code is not known. A third output line, nerdctl's `level=fatal msg="exec failed with exit code 1"`, is left out here. The `ok:` line says only that the case ran and was observed; case f judges nothing.

**g. Pods.** `probe-w` ran 1.005 s after it was created. The worker already held W from case e, so the kubelet did not pull. Its status names the image's repository and digest:

```
info: g: probe-w runs, 1.005s after it was created
info: g-probe-w run: no new request
info: g-probe-w real: no new request
info: g: probe-w status.containerStatuses[0].imageID: rollproof.test/probe/rollproof@sha256:e21e3313a4f09fb3bc0a9a5f8142d791568e7b08d795a7a627f4a4702e6686e6
```

For `kubelet-x` the kubelet pulled X, and that pull went through the same settings as `crictl pull`. The same requests, with the same `?ns=registry.example`, went to the run's registry:

```
info: g: kubelet-x runs, 1.015s after it was created
info: g-kubelet-x run: HEAD /v2/team/app/manifests/sha256:8ed9d7d8f2218a59c7fa3fb3b9d59843dfba0f58794fc7902293dc1d25c198ce?ns=registry.example 200
info: g-kubelet-x run: GET /v2/team/app/manifests/sha256:8ed9d7d8f2218a59c7fa3fb3b9d59843dfba0f58794fc7902293dc1d25c198ce?ns=registry.example 200
info: g-kubelet-x run: GET /v2/team/app/blobs/sha256:ce8561bccaa50fe732388994597cd92c6a337d3063b2ae4981e799137713bdf4?ns=registry.example 200
info: g-kubelet-x run: GET /v2/team/app/blobs/sha256:8186882a23aab01c72209ccbc8f41b22e9b20b06f8190a4e14339100b3963ec1?ns=registry.example 200
info: g-kubelet-x real: no new request
info: g: kubelet-x status.containerStatuses[0].imageID: registry.example/team/app@sha256:8ed9d7d8f2218a59c7fa3fb3b9d59843dfba0f58794fc7902293dc1d25c198ce
ok: g: probe-w ran; run had no new request for /manifests/W while probe-w started; probe-w's imageID contains W; kubelet-x ran; run answered /v2/team/app/manifests/X with 200 while kubelet-x started
ok: l.Down() returned no error
p01: all checks passed
```

In both pods, the digest in `imageID` is the manifest digest that `lab.PushBinary` returned.

**Interrupts.** Run 2 was interrupted while `Up` waited for the nodes. The lab deleted the cluster, and every case counted as not run:

```
lab: Create attempt 1 of 4 failed at kind's wait for a node's log line; trying again
lab: Create returned after 14.434s; 0 of 2 nodes Ready
FAILED: lab.Up: lab: waiting for 2 Ready nodes in rollproof-proof-p01: context canceled (last poll: 2 nodes, 0 Ready)
FAILED: case a did not run
FAILED: case b did not run
FAILED: case c did not run
FAILED: case d did not run
FAILED: case e did not run
FAILED: case f did not run
FAILED: case g did not run
FAILED: the run was interrupted
p01: 9 checks failed
```

In run 3 the signal came while case c ran. The process alone got the signal, so c's pull finished and passed. No later case started, and `Down` ran:

```
ok: c: crictl pull registry.example/team/app@sha256:a78dd21637928e1865d3e49bb37a890463329b340b493bf3746258748ec1ef97 succeeded; run answered a path that ends in Z with 404; real answered /v2/team/app/manifests/Z with 200
ok: l.Down() returned no error
FAILED: case d did not run
FAILED: case e did not run
FAILED: case f did not run
FAILED: case g did not run
FAILED: the run was interrupted
p01: 5 checks failed
```

**Volumes.** `nerdctl volume ls -q` counted 57 volumes before and after each of the four runs, and before and after the lab's cluster test. Runs 1, 3 and 4 started two registries each, six in all, and none left a volume. Run 2 was interrupted inside `Up` and started no registry.

## Decision

**The settings form, for Task 20.** For each host that an entry of `images` names in `as`, the tool writes `/etc/containerd/certs.d/HOST/hosts.toml` in every node, through the node's exec (`mkdir -p`, then `cp /dev/stdin`). It does this after the nodes are Ready and before the first pull, and no restart is needed. The file lists the run's registry first, as in action 5:

```toml
[host."http://ADDRESS:5000"]
  capabilities = ["pull"]
```

ADDRESS is the run's registry's address on the network `kind`.

**Task 20 writes no `server` line.** The last host is then containerd's own default for the host name. The CRI plugin sets no default scheme when `config_path` is set (`internal/cri/server/images/image_pull.go` at v2.3.4, lines 497 to 509), so hosts.go at v2.3.4 (lines 95 to 135) gives:

- for `docker.io`: `https://registry-1.docker.io`;
- for `localhost` or a loopback address, with or without a port: the name itself, over `https` when the port is absent or 443 and over `http` for any other port. Certificate checks are skipped unless the port is 80;
- for any other name: `https://NAME`, as `https://rollproof.test` in case e.

A `server` line is not the same. hosts.go puts it, as written, after the `[host]` entries (lines 414 to 419), and puts `https://` in front of a value that has no scheme (lines 430 to 433). So `server = "https://docker.io"` would name `docker.io`, not `registry-1.docker.io`. In this proof, `server` named the `http://` stand-in for the real host (case c), and in case e the default last host failed at DNS. **No pull that fell through to a real HTTPS host was run.** A mirror's `[host]` entry would go between the run's registry and the last host; this proof did not try a mirror.

For `rollproof.test`, the tool writes the form of case e, with the run's registry alone. A pull of a digest that the run's registry lacks then fails with "not found". The `nerdctl exec` of `crictl pull` returned after 56 ms and 92 ms, and containerd's own PullImage took about 20 ms in run 4. On the way, the pull also looks up the name `rollproof.test` for `https://rollproof.test`. The name does not resolve, and the error text does not show that. What the lookup costs depends on the node's resolver. The form `server = "http://ADDRESS:5000"` for `rollproof.test`, which would avoid the lookup, was not tried.

**The pull command, for Tasks 21 and 22.** The tool has a node pull with `crictl pull REPOSITORY@sha256:…`, run in the node through the engine's exec (in the lab, `l.Exec(node, nil, "crictl", "pull", ref)`). That pull uses the node's registry settings, the ones that the kubelet's pulls use. The tool judges the command only by whether it exits zero, because nerdctl reports exit 1 for any non-zero code. **The tool must not use `ctr -n k8s.io images pull`.** As run in case f, it ignored `/etc/containerd/certs.d` and went straight to the host in the image's name. Its `--hosts-dir` option was not tried.

**The report's lines on images can rest on `imageID`.** `status.containerStatuses[].imageID` is `REPOSITORY@sha256:DIGEST`, where DIGEST is the manifest digest that the push returned. It had this form both when the kubelet pulled the image and when the node already held it. In both pods, REPOSITORY was the repository of the pod's image reference, and each image had only one name on the node. An image held under two names was not tried, and its `imageID` may name the other repository. So the report compares only the digest after the `@` with the digest it recorded, never the whole string. This was measured only with images of one manifest each (an image index was not tried), and with one container in each pod.

## Docker

Run once on 2026-11-22, in run 45014226495 of the workflow `.github/workflows/rollproof-proofs.yml` at commit `cd7bc1f6eb0635c75832de73c47464ed9c026b32`: job `proofs` (job 137182961058), its step "The registry order in the nodes, on Docker", after the p08 record's Docker run in the same job, with the command `go run ./p01-registry-order` from `projects/rollproof/proofs/`. The machine is that run's: GitHub's hosted runner, image `ubuntu-24.04` version 20261116.376.1, Go 1.27.4, rootful Docker (28.0.4 by the image's software list, read; the p08 record says where). The worker gave `crictl version 1.36.0` and `ctr github.com/containerd/containerd/v2 v2.3.4`, and the registries' log `version=3.1.2` and `go.version=go1.26.8`, as on nerdctl.

Cases a to g passed, and the program ended `p01: all checks passed`. Each case asked the same registries for the same paths, apart from the digests, in the same order and with the same answers as run 4 above; the `ok:` lines are the same apart from the digests. What differs:

- `Create` took one attempt, with no retry line:

  ```
  lab: Create returned after 27.969s; 0 of 2 nodes Ready
  lab: every node Ready after 39.965s
  ```

- The five digests differ from the nerdctl runs' (X is `sha256:7436f28c5524875280f89857a28821da3927630cd94beb88010f27b7313fc733`, against `sha256:8ed9d7d8…`); the binaries were built here by Go 1.27.4, there by Go 1.27.3.
- The addresses: run and real were `172.18.0.4:5000` and `172.18.0.5:5000` on Docker's network `kind`, and the nodes asked `172.18.0.1:53` for names, where nerdctl's asked `10.0.2.3:53`.
- The pull of the zero digest in case e took longer, because the lookups of `rollproof.test` did. The program's timer, around the exec of `crictl pull` in the node, gave 199 ms, against 56 ms and 92 ms on nerdctl. containerd's journal gives 153.8 ms from `PullImage` to its `failed` line, against about 20 ms; of that, the two failed lookups of `rollproof.test`, each from a `trying next host` line to the next `fetch failed` line, took 106.2 ms (from 10:52:25.070616459 to 10:52:25.176808995) and 44.2 ms (from 10:52:25.177898198 to 10:52:25.222112120), against 2.9 ms and 2.8 ms in run 4's journal above:

  ```
  info: e-zero: crictl pull rollproof.test/probe/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000 failed after 199ms; its output:
  info: e-zero: containerd: time="2026-11-22T10:52:25.068726173Z" level=info msg="PullImage \"rollproof.test/probe/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000\""
  info: e-zero: containerd: time="2026-11-22T10:52:25.070616459Z" level=info msg="trying next host after status: 404 Not Found" host="172.18.0.4:5000" method=HEAD url="http://172.18.0.4:5000/v2/probe/rollproof/manifests/sha256:0000000000000000000000000000000000000000000000000000000000000000?ns=rollproof.test"
  info: e-zero: containerd: time="2026-11-22T10:52:25.176808995Z" level=info msg="fetch failed" error="failed to do request: Head \"https://rollproof.test/v2/probe/rollproof/manifests/sha256:0000000000000000000000000000000000000000000000000000000000000000\": dial tcp: lookup rollproof.test on 172.18.0.1:53: no such host" host=rollproof.test method=HEAD url="https://rollproof.test/v2/probe/rollproof/manifests/sha256:0000000000000000000000000000000000000000000000000000000000000000"
  info: e-zero: containerd: time="2026-11-22T10:52:25.177898198Z" level=info msg="trying next host after status: 404 Not Found" host="172.18.0.4:5000" method=HEAD url="http://172.18.0.4:5000/v2/probe/rollproof/blobs/sha256:0000000000000000000000000000000000000000000000000000000000000000?ns=rollproof.test"
  info: e-zero: containerd: time="2026-11-22T10:52:25.222112120Z" level=info msg="fetch failed" error="failed to do request: Head \"https://rollproof.test/v2/probe/rollproof/blobs/sha256:0000000000000000000000000000000000000000000000000000000000000000\": dial tcp: lookup rollproof.test on 172.18.0.1:53: no such host" host=rollproof.test method=HEAD url="https://rollproof.test/v2/probe/rollproof/blobs/sha256:0000000000000000000000000000000000000000000000000000000000000000"
  info: e-zero: containerd: time="2026-11-22T10:52:25.222524705Z" level=error msg="PullImage \"rollproof.test/probe/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000\" failed" error="rpc error: code = NotFound desc = failed to pull and unpack image \"rollproof.test/probe/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000\": failed to resolve reference \"rollproof.test/probe/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000\": rollproof.test/probe/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000: not found"
  ```

  The error text, as on nerdctl, names only "not found".
- In case f, `ctr` went straight to `https://registry.example` here too, and the lookup failed on `172.18.0.1:53`. The program's line still says "nerdctl reports 1 for any non-zero code"; that text is fixed in the program whatever the engine, and here the 1 came through Docker.
- In case g, `probe-w` ran 1.01 s and `kubelet-x` 1.007 s after they were created, and each `imageID` named the pushed digest.

Plan assumed: "For a host, the node's settings file lists the run's registry as `[host.\"http://ADDRESS:5000\"]` with `capabilities = [\"pull\"]`, before the real host. A reference by digest is then served by the run's registry; a reference by tag is not answered by it and goes to the real host; a reference with a tag and a digest is pulled by the digest alone; an image the run's registry lacks is fetched from the real host."
Holds: yes
Plan assumed: "The tool has a node pull with `crictl pull REFERENCE`, run in the node. That pull uses the node's registry settings, the ones the kubelet's pulls use."
Holds: yes
Plan assumed: "A settings file written into a running node is used by the next pull, with no restart."
Holds: yes
Plan assumed: "A pod's status gives, in imageID, the digest of the image each container runs."
Holds: yes
