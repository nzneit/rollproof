# Point 1, second half: a mirror under a path, with a private authority, and the one list for a host

## Question

Point 1 of section 14, its last part, and the list that section 9 needs. Can one settings file in a node list, for one registry host, the run's registry, a mirror over HTTPS whose certificate a private authority signed, and the real host? In which order does a node try them? What does a mirror whose registry API sits under a path prefix need, and what does a cache that wants its project name inside the API path need?

## Machine and versions

- System: the developer's machine, Linux 7.3.3, amd64, 16 CPUs.
- Engine: nerdctl 2.4.2, rootless. The p08 record lists its containerd, runc and rootlesskit. Docker is absent. The engine is rootless, so the lab adds the `NoNewKeyring = true` patch.
- Go 1.27.3, for the program and for the three binaries. The certificates are made with Go's `crypto/x509`, with ECDSA P-256 keys.
- kind: `sigs.k8s.io/kind` v0.33.0; `k8s.io/client-go` v0.37.2; `github.com/google/go-containerregistry` v0.22.1, which pushes and copies the images.
- Node image: kind's default, `kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5`, as the p08 record read it. In the worker, `containerd --version` gives `containerd github.com/containerd/containerd/v2 v2.3.4 db8809540e1a7a9da5d518876894933ff55692ab` and `crictl --version` gives `crictl version 1.36.0`.
- Registries: `docker.io/library/registry:3`, held in the engine's store as `registry@sha256:82f6ff4a7f982960c8aa51617fea3aabc485df6e55cce80d22deb1de36f1bc3c`. Its log gives `version=3.1.2` and `go.version=go1.26.8`.
- Sources read: section 9 ("Registries: credentials, mirrors and certificate authorities", "The order for one host") and section 14 point 1 of the design; `docs/hosts.md` of containerd at v2.3.4. To explain the results: `core/remotes/docker/config/hosts.go`, `core/remotes/docker/resolver.go` and `core/remotes/docker/fetcher.go` of containerd at v2.3.4.

## Method

The program is `proofs/p01b-mirror/main.go`.

**Three images.** As in the p01 record, in a new temporary folder `$TMP/…`, with the module named `p01bapp`, for V = `a`, `b` and `f`:

```
go build -trimpath -ldflags "-s -w -X main.v=V" -o $TMP/…/V .
```

Both runs gave the same three digests:

| Image | Digest | Pushed to run as | Copied to |
| --- | --- | --- | --- |
| A | `sha256:2953a63ff9ef0aae384ae5919493bcfb2cc215bc9833e03c7d9f8ffec32f14f2` | `team/app` | nowhere |
| B | `sha256:0cef4d015846b3771eee2462b69c2283a96f870c380cb312f81630d556927436` | `stage/b` | prefixed, as `team/app:v1` |
| F | `sha256:476a4cc69eb6ee61d3db4e71c87ede08bbb7c028abeca3f8396d89a0fb8f9497` | `stage/f` | project, as `proxy-project/team/app:v1` |

No case asks run for `stage/…`.

**The cluster.** `lab.Up(ctx, "p01b", cfg)` makes one control-plane node and one worker, with the same patch as p01:

```toml
[plugins."io.containerd.grpc.v1.cri".registry]
  config_path = "/etc/containerd/certs.d"
```

**The certificates.** In a new temporary folder, CERTS (`$TMP/…`): a CA, `ca.pem`, and one server certificate signed by it, `server.pem` with its key `server-key.pem` (PKCS #8). The server certificate names `mirror.corp.example`, `cache.corp.example` and the address `127.0.0.1`, which the runner's pushes use. Both are valid for one day, from one minute before they were made. The CA's key is never written. The program removes the folder when it ends.

**The registries.** `l.Registry(ctx, "run")` starts run, plain HTTP, as in p01. Two TLS registries go through `l.Container` with the image `docker.io/library/registry:3`, each with a port of its own, found by listening on `127.0.0.1:0` and closing:

```
-p 127.0.0.1:PORT:443 -v CERTS:/certs:ro -e REGISTRY_HTTP_ADDR=0.0.0.0:443 -e REGISTRY_HTTP_TLS_CERTIFICATE=/certs/server.pem -e REGISTRY_HTTP_TLS_KEY=/certs/server-key.pem
```

- `project` has these arguments and `--tmpfs /var/lib/registry`. registry:3 declares `VOLUME /var/lib/registry`, and the tmpfs keeps the engine from making an anonymous volume there. Its repository `proxy-project/team/app` stands for a cache that wants its project name inside the API path.
- `prefixed` has these arguments and `-v rollproof-proof-p01b-prefixed-data:/var/lib/registry`. The program runs `nerdctl volume rm -f rollproof-proof-p01b-prefixed-data` before it starts the container, and again after `Down`. nerdctl does not implement `-f` and fails when the volume is missing, so the program judges each removal by whether `nerdctl volume ls -q` still lists the volume.

For each, the program waits until `GET https://127.0.0.1:PORT/v2/` answers 200, with a client whose `RootCAs` hold the CA.

**How prefixed was filled.** A reference names a host and a repository, so a push cannot reach a registry API under a path prefix. prefixed was therefore filled without its prefix, and started again with it:

1. B was pushed to run as `stage/b` with `lab.PushBinary`, read back with `remote.Image` of its digest reference, and written with `remote.Write` and `remote.WithTransport` (a transport that trusts the CA) to `127.0.0.1:PORT/team/app:v1` in prefixed. F went the same way to `127.0.0.1:PORT/proxy-project/team/app:v1` in project. The proof uses tags here; the tool never does.
2. `nerdctl rm -f rollproof-proof-p01b-prefixed`, then `l.Container` with the same arguments, `-e REGISTRY_HTTP_PREFIX=/registry-example/` and the same volume. The program waits until `GET https://127.0.0.1:PORT/registry-example/v2/` answers 200, and prints `info: prefixed was filled without its prefix and started again with it, on the same volume`.

The second container had a new address. The names below point at it.

**The names and the authority, in every node,** through `l.Exec`, after the restart:

```
sh -c "echo PREFIXED_ADDRESS mirror.corp.example >> /etc/hosts"
sh -c "echo PROJECT_ADDRESS cache.corp.example >> /etc/hosts"
mkdir -p /etc/containerd/certs.d/registry.example
cp /dev/stdin /etc/containerd/certs.d/registry.example/ca.pem
```

The last command gets the CA's PEM on its standard input. In run 1, prefixed was at `10.4.1.214`, project at `10.4.1.212`, and run at `10.4.1.211`.

**Each case** writes its settings file to `/etc/containerd/certs.d/registry.example/hosts.toml` in every node (`cp /dev/stdin`), runs `crictl rmi REFERENCE` in the worker (an error ignored), reads the three registries' logs until two reads 500 ms apart agree, and notes their lengths and the time in UTC as TIME (`2006-01-02 15:04:05 UTC`). It then runs `crictl pull REFERENCE` in the worker, reads the logs again in the same way, and prints every new access-log request of each registry as `info: CASE REGISTRY: METHOD PATH STATUS`, and any new log line of a registry that names a TLS handshake. Last, it prints the lines of `journalctl -u containerd --no-pager --since TIME` in the worker that contain `trying next host` or `fetch failed`. A check compares a path without its query, because containerd adds `?ns=registry.example` to every request it sends to a host other than `registry.example`. The printed path keeps the query. Nothing restarts containerd.

**Form A, the mirror under a path,** as run 1 wrote it (`RUN_NODE` is `10.4.1.211:5000`):

```toml
server = "https://registry.example"
[host."http://10.4.1.211:5000"]
  capabilities = ["pull"]
[host."https://mirror.corp.example/registry-example"]
  capabilities = ["pull", "resolve"]
  ca = "/etc/containerd/certs.d/registry.example/ca.pem"
```

- a: pull `registry.example/team/app@A`.
- b: pull `registry.example/team/app:v1`.
- c: form A without its `ca` line, and the pull of b.
- d: pull `registry.example/team/app@sha256:` followed by 64 zeros.
- e: form A with its two `[host…]` blocks swapped, and the pull of a.

**Form B, the project inside the API path:** form A with the mirror's block replaced:

```toml
server = "https://registry.example"
[host."http://10.4.1.211:5000"]
  capabilities = ["pull"]
[host."https://cache.corp.example/v2/proxy-project"]
  capabilities = ["pull", "resolve"]
  ca = "/etc/containerd/certs.d/registry.example/ca.pem"
  override_path = true
```

- f: pull `registry.example/team/app:v1`.
- g: form A's way of writing pointed at project, the block `[host."https://cache.corp.example/proxy-project"]` with the same `capabilities` and `ca` and no `override_path`; pull `registry.example/team/app:v1`.

Cases a, b, c, d and f are judged. Cases e and g are recorded: their `ok:` line says only that the case ran and was observed. A case that did not run to its end counts as a failed check, and so does an interrupt. Either one stops the line `p01b: all checks passed`. The program then calls `Down` and removes the volume.

The command, from `projects/rollproof/proofs/`:

```
go run ./p01b-mirror
```

The program ran twice on 2026-11-17, between 01:06 and 01:09 UTC, one cluster at a time. `nerdctl ps -a` listed no container whose name begins with `rollproof-` before or after either run. Run 1 is the plain command on the program as committed. Run 2 tested an interrupt: the committed program, built as a binary in `$TMP/…`, ran from a script that sent SIGINT to the program's process alone once case b's line appeared.

## Result

| Run | Program | Create attempts of `Up` | Last line | Exit | `nerdctl volume ls -q` before, after |
| --- | --- | --- | --- | --- | --- |
| 1 | as committed | 1 | `p01b: all checks passed` | 0 | 57, 57 |
| 2 | as committed; SIGINT to the process after case b | 1 | `p01b: 5 checks failed` | 1 | 57, 57 |

Neither run printed the lab's line `lab: Create attempt K of 4 failed …`, so each `Up` took one attempt. These are run 1's lab lines:

```
lab: Create returned after 16.129s; 0 of 2 nodes Ready
lab: every node Ready after 28.127s
```

The quotes below are from run 1. Every line `info: CASE: crictl pull … succeeded` is followed by crictl's own line `Image is up to date for sha256:…`, which is left out here.

**a. By digest, an image that the run's registry holds.** run served the manifest and both blobs. Neither mirror was asked, and containerd logged no host it passed over:

```
info: a run: HEAD /v2/team/app/manifests/sha256:2953a63ff9ef0aae384ae5919493bcfb2cc215bc9833e03c7d9f8ffec32f14f2?ns=registry.example 200
info: a run: GET /v2/team/app/manifests/sha256:2953a63ff9ef0aae384ae5919493bcfb2cc215bc9833e03c7d9f8ffec32f14f2?ns=registry.example 200
info: a run: GET /v2/team/app/blobs/sha256:3395034911cbaff8d4da0531a780d1bc294ea2531f94b619d6c527e45a247979?ns=registry.example 200
info: a run: GET /v2/team/app/blobs/sha256:6dbfa2c30644f263f43e4527cad917f542b58beac1deda80aeab1c4e62462394?ns=registry.example 200
info: a project: no new request
info: a prefixed: no new request
info: a containerd: no line since 2026-11-17 01:06:36 UTC contains "trying next host" or "fetch failed"
ok: a: crictl pull registry.example/team/app@sha256:2953a63ff9ef0aae384ae5919493bcfb2cc215bc9833e03c7d9f8ffec32f14f2 succeeded; run answered /v2/team/app/manifests/A with 200
```

**b. By tag, through the mirror under a path.** The tag was resolved only by prefixed, at `/registry-example/v2/team/app/manifests/v1`: containerd put the registry API's path after the mirror's path. run was never asked for the tag. It was asked first for B's manifest and blobs by digest and answered 404, and prefixed then served them:

```
info: b run: GET /v2/team/app/manifests/sha256:0cef4d015846b3771eee2462b69c2283a96f870c380cb312f81630d556927436?ns=registry.example 404
info: b run: GET /v2/team/app/blobs/sha256:c41adfaec7ecfcefd4c5d55ea362b98f6e051b0b3f288cedac3183b9e4f48773?ns=registry.example 404
info: b run: GET /v2/team/app/blobs/sha256:be2f8a43a0319d577dfb75b4f24139c2d492a21a58faa9771563626ddc02c2f8?ns=registry.example 404
info: b project: no new request
info: b prefixed: HEAD /registry-example/v2/team/app/manifests/v1?ns=registry.example 200
info: b prefixed: GET /registry-example/v2/team/app/manifests/sha256:0cef4d015846b3771eee2462b69c2283a96f870c380cb312f81630d556927436?ns=registry.example 200
info: b prefixed: GET /registry-example/v2/team/app/blobs/sha256:c41adfaec7ecfcefd4c5d55ea362b98f6e051b0b3f288cedac3183b9e4f48773?ns=registry.example 200
info: b prefixed: GET /registry-example/v2/team/app/blobs/sha256:be2f8a43a0319d577dfb75b4f24139c2d492a21a58faa9771563626ddc02c2f8?ns=registry.example 200
info: b containerd: no line since 2026-11-17 01:06:39 UTC contains "trying next host" or "fetch failed"
ok: b: crictl pull registry.example/team/app:v1 succeeded; run had no request for /manifests/v1; prefixed answered a path that starts with /registry-example/v2/team/app/ with 200
```

containerd logs a host it passes over only while it resolves a reference (resolver.go, lines 288 to 341). The fetches of the manifest and the blobs by digest go on to the next host with no line (fetcher.go, lines 276 to 319), so the ones that passed over run are not in its log. run's 404s show them.

**c. Form A without its `ca` line.** The pull failed at the mirror's certificate. The CA's file sat in the same folder as `hosts.toml`, and containerd did not use it. No registry logged a request, and prefixed logged the handshake that failed:

```
info: c: crictl pull registry.example/team/app:v1 failed; its output:
info: c:   E1117 01:06:43.192230     933 remote_image.go:250] "PullImage from image service failed" err="rpc error: code = Unknown desc = failed to pull and unpack image \"registry.example/team/app:v1\": failed to resolve reference \"registry.example/team/app:v1\": failed to do request: Head \"https://mirror.corp.example/registry-example/v2/team/app/manifests/v1?ns=%5BREDACTED%5D\": tls: failed to verify certificate: x509: certificate signed by unknown authority" image="registry.example/team/app:v1"
info: c run: no new request
info: c project: no new request
info: c prefixed: no new request
info: c prefixed: log: 2026/11/17 01:06:43 http: TLS handshake error from 10.4.1.210:51004: remote error: tls: bad certificate
info: c containerd: Nov 17 01:06:43 rollproof-proof-p01b-worker containerd[240]: time="2026-11-17T01:06:43.126858083Z" level=info msg="trying next host" error="failed to do request: Head \"https://mirror.corp.example/registry-example/v2/team/app/manifests/v1?ns=registry.example\": tls: failed to verify certificate: x509: certificate signed by unknown authority" host=mirror.corp.example method=HEAD url="https://mirror.corp.example/registry-example/v2/team/app/manifests/v1?ns=registry.example"
info: c containerd: Nov 17 01:06:43 rollproof-proof-p01b-worker containerd[240]: time="2026-11-17T01:06:43.190603919Z" level=info msg="fetch failed" error="failed to do request: Head \"https://registry.example/v2/team/app/manifests/v1\": dial tcp: lookup registry.example on 10.0.2.3:53: no such host" host=registry.example method=HEAD url="https://registry.example/v2/team/app/manifests/v1"
ok: c: crictl pull registry.example/team/app:v1 failed; its error contains "certificate"
```

Two more output lines are left out here: crictl's `level=fatal` line, which repeats the error, and nerdctl's `exec failed with exit code 1`. run was not asked, because a tag is resolved only by a host that may resolve (resolver.go, lines 264 to 270). After the mirror, the pull went on to `https://registry.example`, which did not resolve. The error names the certificate and not that lookup: in the resolver, the first failure to connect is kept over a later one (resolver.go, lines 321 to 330). The error that crictl printed shows the query as `?ns=%5BREDACTED%5D`; containerd's journal shows `?ns=registry.example`.

The CA's file is used only through a `ca` line. hosts.md ("Support for Docker's Certificate File Pattern") says that containerd falls back to `.crt` files in the folder only when the folder has no `hosts.toml`; this file was named `ca.pem`, and the folder had a `hosts.toml`.

**d. A digest that no registry holds.** The pull failed with "not found". The containerd lines show the order of attempts, run, then prefixed, then `registry.example`, first for the manifest path and then for the blob path:

```
info: d: crictl pull registry.example/team/app@sha256:0000000000000000000000000000000000000000000000000000000000000000 failed; its output:
info: d:   E1117 01:06:46.652956     974 remote_image.go:250] "PullImage from image service failed" err="rpc error: code = NotFound desc = failed to pull and unpack image \"registry.example/team/app@sha256:0000000000000000000000000000000000000000000000000000000000000000\": failed to resolve reference \"registry.example/team/app@sha256:0000000000000000000000000000000000000000000000000000000000000000\": registry.example/team/app@sha256:0000000000000000000000000000000000000000000000000000000000000000: not found" image="registry.example/team/app@sha256:0000000000000000000000000000000000000000000000000000000000000000"
info: d run: HEAD /v2/team/app/manifests/sha256:0000000000000000000000000000000000000000000000000000000000000000?ns=registry.example 404
info: d run: HEAD /v2/team/app/blobs/sha256:0000000000000000000000000000000000000000000000000000000000000000?ns=registry.example 404
info: d project: no new request
info: d prefixed: HEAD /registry-example/v2/team/app/manifests/sha256:0000000000000000000000000000000000000000000000000000000000000000?ns=registry.example 404
info: d prefixed: HEAD /registry-example/v2/team/app/blobs/sha256:0000000000000000000000000000000000000000000000000000000000000000?ns=registry.example 404
info: d containerd: Nov 17 01:06:46 rollproof-proof-p01b-worker containerd[240]: time="2026-11-17T01:06:46.648846254Z" level=info msg="trying next host after status: 404 Not Found" host="10.4.1.211:5000" method=HEAD url="http://10.4.1.211:5000/v2/team/app/manifests/sha256:0000000000000000000000000000000000000000000000000000000000000000?ns=registry.example"
info: d containerd: Nov 17 01:06:46 rollproof-proof-p01b-worker containerd[240]: time="2026-11-17T01:06:46.650085258Z" level=info msg="trying next host after status: 404 Not Found" host=mirror.corp.example method=HEAD url="https://mirror.corp.example/registry-example/v2/team/app/manifests/sha256:0000000000000000000000000000000000000000000000000000000000000000?ns=registry.example"
info: d containerd: Nov 17 01:06:46 rollproof-proof-p01b-worker containerd[240]: time="2026-11-17T01:06:46.650672940Z" level=info msg="fetch failed" error="failed to do request: Head \"https://registry.example/v2/team/app/manifests/sha256:0000000000000000000000000000000000000000000000000000000000000000\": dial tcp: lookup registry.example on 10.0.2.3:53: no such host" host=registry.example method=HEAD url="https://registry.example/v2/team/app/manifests/sha256:0000000000000000000000000000000000000000000000000000000000000000"
info: d containerd: Nov 17 01:06:46 rollproof-proof-p01b-worker containerd[240]: time="2026-11-17T01:06:46.650977251Z" level=info msg="trying next host after status: 404 Not Found" host="10.4.1.211:5000" method=HEAD url="http://10.4.1.211:5000/v2/team/app/blobs/sha256:0000000000000000000000000000000000000000000000000000000000000000?ns=registry.example"
info: d containerd: Nov 17 01:06:46 rollproof-proof-p01b-worker containerd[240]: time="2026-11-17T01:06:46.651225987Z" level=info msg="trying next host after status: 404 Not Found" host=mirror.corp.example method=HEAD url="https://mirror.corp.example/registry-example/v2/team/app/blobs/sha256:0000000000000000000000000000000000000000000000000000000000000000?ns=registry.example"
info: d containerd: Nov 17 01:06:46 rollproof-proof-p01b-worker containerd[240]: time="2026-11-17T01:06:46.651580452Z" level=info msg="fetch failed" error="failed to do request: Head \"https://registry.example/v2/team/app/blobs/sha256:0000000000000000000000000000000000000000000000000000000000000000\": dial tcp: lookup registry.example on 10.0.2.3:53: no such host" host=registry.example method=HEAD url="https://registry.example/v2/team/app/blobs/sha256:0000000000000000000000000000000000000000000000000000000000000000"
info: d: the hosts in containerd's lines, in order: 10.4.1.211:5000, mirror.corp.example, registry.example, 10.4.1.211:5000, mirror.corp.example, registry.example
ok: d: crictl pull registry.example/team/app@sha256:0000000000000000000000000000000000000000000000000000000000000000 failed; its error names registry.example; the last host tried, in containerd's lines, is registry.example (it is "registry.example")
```

The error names `registry.example` only because it quotes the reference. It says "not found" and says nothing about the failed lookup of `registry.example`: a 404 outranks a failure to connect (resolver.go, lines 321 to 341), as in p01's case e. The `server` line was the last host, as written, and containerd sent it no `?ns=` query. The six attempts took 3 ms by the journal's times.

**e. Form A with its two `[host…]` blocks swapped, and the pull of a.** prefixed was asked first. containerd's one line names it, and its log shows a 404 for the manifest and for each blob, each of which run then served with 200. The file's order is the order of attempts:

```
info: e: /etc/containerd/certs.d/registry.example/hosts.toml, written in every node:
info: e:   server = "https://registry.example"
info: e:   [host."https://mirror.corp.example/registry-example"]
info: e:     capabilities = ["pull", "resolve"]
info: e:     ca = "/etc/containerd/certs.d/registry.example/ca.pem"
info: e:   [host."http://10.4.1.211:5000"]
info: e:     capabilities = ["pull"]
info: e: crictl pull registry.example/team/app@sha256:2953a63ff9ef0aae384ae5919493bcfb2cc215bc9833e03c7d9f8ffec32f14f2 succeeded; its output:
info: e run: HEAD /v2/team/app/manifests/sha256:2953a63ff9ef0aae384ae5919493bcfb2cc215bc9833e03c7d9f8ffec32f14f2?ns=registry.example 200
info: e run: GET /v2/team/app/manifests/sha256:2953a63ff9ef0aae384ae5919493bcfb2cc215bc9833e03c7d9f8ffec32f14f2?ns=registry.example 200
info: e run: GET /v2/team/app/blobs/sha256:3395034911cbaff8d4da0531a780d1bc294ea2531f94b619d6c527e45a247979?ns=registry.example 200
info: e run: GET /v2/team/app/blobs/sha256:6dbfa2c30644f263f43e4527cad917f542b58beac1deda80aeab1c4e62462394?ns=registry.example 200
info: e project: no new request
info: e prefixed: HEAD /registry-example/v2/team/app/manifests/sha256:2953a63ff9ef0aae384ae5919493bcfb2cc215bc9833e03c7d9f8ffec32f14f2?ns=registry.example 404
info: e prefixed: GET /registry-example/v2/team/app/manifests/sha256:2953a63ff9ef0aae384ae5919493bcfb2cc215bc9833e03c7d9f8ffec32f14f2?ns=registry.example 404
info: e prefixed: GET /registry-example/v2/team/app/blobs/sha256:3395034911cbaff8d4da0531a780d1bc294ea2531f94b619d6c527e45a247979?ns=registry.example 404
info: e prefixed: GET /registry-example/v2/team/app/blobs/sha256:6dbfa2c30644f263f43e4527cad917f542b58beac1deda80aeab1c4e62462394?ns=registry.example 404
info: e containerd: Nov 17 01:06:50 rollproof-proof-p01b-worker containerd[240]: time="2026-11-17T01:06:50.120199244Z" level=info msg="trying next host after status: 404 Not Found" host=mirror.corp.example method=HEAD url="https://mirror.corp.example/registry-example/v2/team/app/manifests/sha256:2953a63ff9ef0aae384ae5919493bcfb2cc215bc9833e03c7d9f8ffec32f14f2?ns=registry.example"
info: e: asked first, by the logs: prefixed: it was asked for /registry-example/v2/team/app/manifests/sha256:2953a63ff9ef0aae384ae5919493bcfb2cc215bc9833e03c7d9f8ffec32f14f2, and run answered /v2/team/app/manifests/sha256:2953a63ff9ef0aae384ae5919493bcfb2cc215bc9833e03c7d9f8ffec32f14f2 with 200, which ends the search for that path
info: e: asked first, by containerd's lines: the first line names host=mirror.corp.example (prefixed), and the hosts in order are mirror.corp.example
ok: e: the pull ran and was observed; it succeeded
```

The program does not compare times across logs. The logs show the order for every path because a 200 ends the search for a path: had run been asked first, prefixed would not have been asked at all, as in case a. The source agrees: hosts.go keeps the `[host]` entries in the order of the file (lines 550 to 585) and puts the `server` line after them (lines 404 to 419), and the resolver and the fetcher try the hosts in that order (resolver.go, lines 295 to 304; fetcher.go, lines 280 and 303).

**f. Form B, the project inside the API path.** With `override_path = true`, containerd used the block's path as the API root and put only the repository and the object after it. project resolved the tag and served F. run was asked first for F's content by digest and answered 404:

```
info: f run: GET /v2/team/app/manifests/sha256:476a4cc69eb6ee61d3db4e71c87ede08bbb7c028abeca3f8396d89a0fb8f9497?ns=registry.example 404
info: f run: GET /v2/team/app/blobs/sha256:889e0135952033178acf80d3fed71ece5eda53f5c259cdf64dc75af14a096c3c?ns=registry.example 404
info: f run: GET /v2/team/app/blobs/sha256:9641972b96ab5a8190a86c662808c3d35c243bea63b9847c8569944c616be415?ns=registry.example 404
info: f project: HEAD /v2/proxy-project/team/app/manifests/v1?ns=registry.example 200
info: f project: GET /v2/proxy-project/team/app/manifests/sha256:476a4cc69eb6ee61d3db4e71c87ede08bbb7c028abeca3f8396d89a0fb8f9497?ns=registry.example 200
info: f project: GET /v2/proxy-project/team/app/blobs/sha256:889e0135952033178acf80d3fed71ece5eda53f5c259cdf64dc75af14a096c3c?ns=registry.example 200
info: f project: GET /v2/proxy-project/team/app/blobs/sha256:9641972b96ab5a8190a86c662808c3d35c243bea63b9847c8569944c616be415?ns=registry.example 200
info: f prefixed: no new request
info: f containerd: no line since 2026-11-17 01:06:53 UTC contains "trying next host" or "fetch failed"
ok: f: crictl pull registry.example/team/app:v1 succeeded; project answered a path that starts with /v2/proxy-project/team/app/ with 200
```

**g. The cache written in form A's way, with no `override_path`.** The node asked project for `/proxy-project/v2/team/app/manifests/v1?ns=registry.example`: containerd put `/v2` after the block's path, before the repository. project answered 404, the pull went on to `https://registry.example`, which did not resolve, and it failed with "not found":

```
info: g: crictl pull registry.example/team/app:v1 failed; its output:
info: g:   E1117 01:06:57.136356    1110 remote_image.go:250] "PullImage from image service failed" err="rpc error: code = NotFound desc = failed to pull and unpack image \"registry.example/team/app:v1\": failed to resolve reference \"registry.example/team/app:v1\": registry.example/team/app:v1: not found" image="registry.example/team/app:v1"
info: g run: no new request
info: g project: HEAD /proxy-project/v2/team/app/manifests/v1?ns=registry.example 404
info: g prefixed: no new request
info: g containerd: Nov 17 01:06:57 rollproof-proof-p01b-worker containerd[240]: time="2026-11-17T01:06:57.134547456Z" level=info msg="trying next host after status: 404 Not Found" host=cache.corp.example method=HEAD url="https://cache.corp.example/proxy-project/v2/team/app/manifests/v1?ns=registry.example"
info: g containerd: Nov 17 01:06:57 rollproof-proof-p01b-worker containerd[240]: time="2026-11-17T01:06:57.135146430Z" level=info msg="fetch failed" error="failed to do request: Head \"https://registry.example/v2/team/app/manifests/v1\": dial tcp: lookup registry.example on 10.0.2.3:53: no such host" host=registry.example method=HEAD url="https://registry.example/v2/team/app/manifests/v1"
info: g: project was asked for /proxy-project/v2/team/app/manifests/v1?ns=registry.example (/proxy-project/v2/team/app/manifests/v1 without the query), and answered 404
info: g: the pull failed
ok: g: the pull ran and was observed; it failed
ok: l.Down() returned no error
ok: after Down, the volume rollproof-proof-p01b-prefixed-data is gone (it existed: true)
p01b: all checks passed
```

Cases b, f and g follow the rule in hosts.go (lines 440 to 447): a block's path that does not end in `/v2` gets `/v2` added, unless `override_path` is set; with `override_path`, the path is used as written. The registry API's own path, the repository and the object, comes after it.

**Interrupt.** In run 2 the signal came while case c ran. The process alone got the signal, so c's pull finished and passed. No later case started, and `Down` and the removal of the volume ran:

```
ok: c: crictl pull registry.example/team/app:v1 failed; its error contains "certificate"
ok: l.Down() returned no error
ok: after Down, the volume rollproof-proof-p01b-prefixed-data is gone (it existed: true)
FAILED: case d did not run
FAILED: case e did not run
FAILED: case f did not run
FAILED: case g did not run
FAILED: the run was interrupted
p01b: 5 checks failed
```

**Volumes.** `nerdctl volume ls -q` counted 57 volumes before and after each run. The named volume existed during each run and was gone after it; neither TLS registry left an anonymous volume.

## Decision

**The one settings file, for the plan of step 5.** For a host HOST that an entry of `images` names in `as`, and whose `registries` entry has a `mirror` and a `ca`, the tool writes `/etc/containerd/certs.d/HOST/hosts.toml` in every node in the form of case b, and the `ca` file beside it:

```toml
server = "https://HOST"
[host."http://ADDRESS:5000"]
  capabilities = ["pull"]
[host."https://MIRROR"]
  capabilities = ["pull", "resolve"]
  ca = "/etc/containerd/certs.d/HOST/ca.pem"
```

ADDRESS is the run's registry's address on the network `kind`, and `https://MIRROR` is the mirror's block as below. The `ca` line is needed: the file next to `hosts.toml` is not read without it (case c). A node tries the places in the order of the file (cases d and e). A tag is resolved by the mirror, never by the run's registry (case b), and a digest goes to the run's registry first (cases a and d).

Three limits of what was run, for step 5:

- **The `server` line.** This proof wrote `server = "https://registry.example"`, a name that does not resolve, and no pull reached a real host over HTTPS. The p01 record decided that Task 20 writes no `server` line, because a `server` line is used as written and `https://docker.io` would not name `registry-1.docker.io`. Without the line, containerd adds its own default for HOST as the last host, and keeps the file's top-level fields for it (hosts.go, lines 95 to 135; read, not run). Step 5 either leaves the line out, as Task 20 does, or writes `server = "https://registry-1.docker.io"` for `docker.io`.
- **The `ca` for the real host.** Section 9 says the nodes trust `ca` for the entry's host as well as for its mirror. In the file above, the `ca` line applies to the mirror's block only. For the real host, a `ca` line goes at the top of the file, where hosts.go reads it into the last host's settings (lines 414 to 419). That was read, not run.
- **The names.** The proof gave the mirrors their names through `/etc/hosts` in the nodes. The tool writes no `/etc/hosts`: a caller's mirror resolves through the nodes' DNS, which kind takes from the engine. Both mirrors ignored the `?ns=HOST` query; a cache that reads it was not tried.

**The `mirror` key: a proposal.** The two kinds of mirror path differ only in where the registry API's root is. The proposal is that `mirror` is an `https://` URL with an optional path, and that the tool reads the path as follows:

- A path whose first segment is not `v2` is a prefix in front of the API, and the tool writes the URL as the block, as in form A. containerd then puts `/v2/` and the repository after it. Example: `https://mirror.corp.example/registry-example` gives `[host."https://mirror.corp.example/registry-example"]`, and the node asks for `/registry-example/v2/team/app/…` (case b).
- A path whose first segment is `v2` already holds the API's root, with what the cache wants inside it, and the tool writes the URL as the block with `override_path = true`, as in form B. Example: `https://cache.corp.example/v2/proxy-project` gives `[host."https://cache.corp.example/v2/proxy-project"]` with `override_path = true`, and the node asks for `/v2/proxy-project/team/app/…` (case f).

The caller then writes the URL that the mirror's own documentation gives, and needs no second key. Written the other way, the cache fails with no sign at the mirror but a 404 (case g). A URL with the path `/v2` alone gives the same requests either way. A URL with no path is a mirror at the host's root and is written as in form A: with `override_path` and no path, containerd would leave out `/v2` altogether (hosts.go, lines 440 to 447). The alternative is an explicit key beside `mirror` that names the kind; it costs the caller a second key and a way to get the two out of step. A path whose first segment only starts with `v2`, such as `/v2cache`, counts as a prefix. The configuration would reject a `mirror` that is not `https://`, or that has a query or a fragment.

Plan assumed: "For one host the node's settings file lists, in this order, the run's registry (pull only), the mirror over HTTPS with its `ca` (pull and resolve), and the real host as `server`. A node tries them in that order. A mirror whose registry API sits under a path prefix is written as the host with that prefix; a cache that wants its project name inside the API path needs `override_path = true`."
Holds: yes
