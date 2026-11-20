# Point 2: mirrors that need credentials, and forbidding the last step

## Question

Point 2 of section 14. When a pod pulls an image named after a host H whose node settings list a mirror M and then the real host R, which credentials does each of M and R receive? Are credentials listed under M's own host used? What mechanism in the node serves a mirror whose credentials differ from H's: containerd's own setting per host, or a header in M's block of `hosts.toml`? Can a node's settings forbid the last step from M to R? Two more questions ride along: the `ca` at the top of `hosts.toml` for the real host, which `docs/proofs/p01b-mirror.md` read and did not run; and section 9's "Into the nodes", where the run's registry is first for an authenticated host and `crictl pull` passes no credentials.

## Machine and versions

- System: the developer's machine, Linux 7.3.3, amd64, 16 CPUs.
- Engine: nerdctl 2.4.2, rootless. The p08 record lists its containerd, runc and rootlesskit. Docker is absent. The engine is rootless, so the lab adds the `NoNewKeyring = true` patch.
- Go 1.27.3, for the program, the test binaries and the logging proxy. The certificates are made with Go's `crypto/x509`, with ECDSA P-256 keys.
- kind: `sigs.k8s.io/kind` v0.33.0; `k8s.io/client-go` v0.37.2; `github.com/google/go-containerregistry` v0.22.1, which pushes and copies the images.
- Node image: kind's default, `kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5`. In the worker, `containerd --version` gives `containerd github.com/containerd/containerd/v2 v2.3.4 db8809540e1a7a9da5d518876894933ff55692ab`, `crictl --version` gives `crictl version 1.36.0`, and `kubelet --version` gives `Kubernetes v1.37.0`.
- Registries: `docker.io/library/registry:3`, held in the engine's store as `registry@sha256:82f6ff4a7f982960c8aa51617fea3aabc485df6e55cce80d22deb1de36f1bc3c` (3.1.2, as the p01 record read its log). Its `htpasswd` authentication accepts bcrypt hashes only. The three hashes in the program were made once with perl 5.44.0's `crypt`, which hands `$2b$` salts to libxcrypt 4.5.2; the program checks them against both registries before any case.
- Sources read, at the node's versions. containerd v2.3.4: `internal/cri/server/images/image_pull.go`, `internal/cri/config/config.go`, `plugins/cri/images/plugin.go`, `core/remotes/docker/config/hosts.go`, `core/remotes/docker/resolver.go`, `core/remotes/docker/authorizer.go`, `core/remotes/docker/fetcher.go`. Kubernetes v1.37.0: `pkg/credentialprovider/keyring.go`, `config.go` and `provider.go`, `pkg/kubelet/images/image_manager.go`, `pkg/kubelet/kuberuntime/kuberuntime_image.go`, `cmd/kubelet/app/server.go`. kind v0.33.0: `images/base/files/etc/containerd/config.toml`.

## Method

The program is `proofs/p02-mirror-credentials/main.go`.

**The stand-ins.** All of them run on the network `kind`, named `rollproof-proof-p02-…`:

| Name | What it is | Users it accepts |
| --- | --- | --- |
| `stage` | `l.Registry`, plain HTTP; every image is pushed here first | none needed |
| `run` | `l.Registry`, plain HTTP: the run's registry | none needed |
| `mreg` | registry:3 with `REGISTRY_AUTH=htpasswd`, realm `mirror`, plain HTTP on 5000, a tmpfs at `/var/lib/registry` | `auser`, `muser` |
| `rreg` | the same with realm `real` | `auser`, `huser` |
| `mproxy` | M: the logging proxy over TLS on 443, in front of `mreg`; it serves only paths under `/registry-example/` and strips that prefix before it passes a request on; any other path gets 404 | as `mreg` |
| `rproxy` | R: the logging proxy over plain HTTP on 5000 and over TLS on 443, in front of `rreg`, paths unchanged | as `rreg` |

The users are made up for the run: `auser` (password `a-pass-p02`) stands for credentials of H that both M and R accept; `muser` (`m-pass-p02`) is M's own, which R refuses; `huser` (`h-pass-p02`) is the real host's, which M refuses. Before the cases the program asks each registry for `GET /v2/` as each user and requires 200 or 401 as the table says; all six answers were as required in every run.

**The logging proxy** is a Go program of about 100 lines, held as a string in the program, built with `CGO_ENABLED=0` into a temporary folder, and run by the engine from that folder, mounted read-only into a container of the registry:3 image with `--entrypoint`. It is `httputil.NewSingleHostReverseProxy` behind a handler that writes one line for each request, after it has been answered:

```
REQ NAME LISTENER METHOD PATH?QUERY user=USER auth=SCHEME status=CODE
```

USER is the user of a Basic `Authorization` header, `-` when the request had none. The certificates are those of p01b: a CA, and a server certificate for `mirror.corp.example`, `e.registry.example` and `127.0.0.1`, valid for one day.

**The kubelet's file.** The program writes a Docker `config.json` with an `auths` map whose entries carry `auth` (base64 of USER:PASSWORD), and the lab mounts it read-only at `/var/lib/kubelet/config.json` in both nodes, through kind's `extraMounts`, as kind's guide for private registries does. Its entries:

| Key | User | For |
| --- | --- | --- |
| `a.registry.example` | auser | cases x and a |
| `mirror.corp.example` | auser | case b: M's own host only |
| `c1.registry.example` | huser | case c with credentials for H |
| `d.registry.example` | auser | cases d-m and d |
| `e.registry.example` | huser | case e |

`b.registry.example`, `c2.registry.example`, `d2.registry.example` and `f.registry.example` have no entry. The kubelet keeps what it read from the file for 5 minutes (`pkg/credentialprovider/provider.go`, lines 104 to 113), so the file is written once, before the cluster exists, and each case that needs another set of credentials uses another host name.

**The cluster.** `lab.Up(ctx, "p02", cfg)` makes one control-plane node and one worker, each with the mount, and with p01's patch that sets `config_path = "/etc/containerd/certs.d"`. The worker's containerd journal shows, at its start, that the CRI plugin pulls in its local mode and not through the transfer service, because kind's configuration sets `discard_unpacked_layers = true`:

```
level=warning msg="Found 'DiscardUnpackedLayers' in CRI config which is incompatible with transfer service (not supported with transfer service). Falling back to local image pull mode."
```

**The names and the authority, in every node:** `MPROXY mirror.corp.example` and `RPROXY e.registry.example` appended to `/etc/hosts`, and the CA as `/etc/containerd/certs.d/HOST/ca.pem` for each of `a`, `b`, `c1`, `c2`, `d`, `d2`, `e` and `f.registry.example`.

**Nineteen images.** As in p01b, in a new temporary folder, with the module named `p02app`, one binary for each image, built with `-ldflags "-s -w -X main.v=V"`. `lab.PushBinary` pushes each to `stage` as `stage/V`; the program then copies it by digest with `remote.Write` to where the table says, as `auser` into `mreg`, as `huser` into `rreg`, and with no credentials into `run`. Each pull has its own image, so that no pull finds content that an earlier one brought into the node. Every run gave the same digests:

| Image | Digest | Repository | Held by |
| --- | --- | --- | --- |
| x | `sha256:61cefd806cc5a042b8fa49111244e2ad244ea0f28f160ab071db5adfa16f6312` | `team/x` | M |
| a | `sha256:33fa982d8093db2b74c99f976dc3c5d897397ed7253ef6d63a6bcb14908fd94f` | `team/a` | M |
| b | `sha256:283c5137cf76ecbd95fd80e40a5e68d25ae13657ea52bb4b0666e5c5d0f26091` | `team/b` | M |
| hdr-c1-m | `sha256:6e9d56fd81dbcf6e782f2fd1a3605459efe3c0b6fafbe5b8c979e33eabc723a7` | `team/c-hdr-c1-m` | M |
| hdr-c1-r | `sha256:34be6e3aaa3b40faf909ece9df7cb2e28a9d356e6ed59345636ba219a0b41999` | `team/c-hdr-c1-r` | R |
| hdr-c2-m | `sha256:f5ff05e96d436f37228883a461c4638c93586422db893152e26bdc715af3671d` | `team/c-hdr-c2-m` | M |
| hdr-c2-r | `sha256:4014d0cdf8589ad69ba17eba5c78e0837d876cec1dc915a8c79abede562ed898` | `team/c-hdr-c2-r` | R |
| cfg-c1-m | `sha256:a6074aec842899c49240daf317e4861ae9a205d62e968fe40d4c645964cef0bb` | `team/c-cfg-c1-m` | M |
| cfg-c1-r | `sha256:6582fa5b01944e59e0540811fe4300618a2aafbd66039dee894f1b28a3389db2` | `team/c-cfg-c1-r` | R |
| cfg-c2-m | `sha256:2e3149f21afb90c28585046e113cdf545ae87b904754064c5d3cb517fcc9fc79` | `team/c-cfg-c2-m` | M |
| cfg-c2-r | `sha256:091ac06a65c43030d0303d7c1f2da0c18e7598006d50a7786407df72eef32112` | `team/c-cfg-c2-r` | R |
| d-m | `sha256:cd722b9679d6eb10dacc85e081f55c82d3813a3ebb22015d834dbb48ab4d1bfe` | `team/d-m` | M |
| d | `sha256:e5b57c33eef30de0d57c42a55bb65250a6bae5ac9ebf3ecb03d3ed4278564603` | `team/d` | R |
| d-hdr-m | `sha256:f97c10f28970fab61935dd3c2bc3010f9987258a8b334dab739185365ce9c7e6` | `team/d-hdr-m` | M |
| d-hdr | `sha256:bd00d984147d437e40277df15dbf542a8e7bfc6a696bdbb2ff5550756f2b1c3a` | `team/d-hdr` | R |
| e0 | `sha256:0632a5c59541d2df283832f480246996c72fb74076493caa72bdf01dffc54a95` | `team/e0` | R |
| e1 | `sha256:5b5bd93ca8d26caa699b74dcfcfd15292b5178c29d65d15a118d86ba98838051` | `team/e1` | R |
| e2 | `sha256:266013ab52567d7d4af4fb3064d33b8e912ca71282241f5ca810c64399ae38f0` | `team/e2` | R |
| f | `sha256:a97e3894f91748662bbdacaa41f4dc439ef54dc98a9fc9e2efd93943148d069b` | `team/f` | run |

**The settings files.** Each case writes its file to `/etc/containerd/certs.d/HOST/hosts.toml` in every node (`cp /dev/stdin`). Nothing restarts containerd, except before the four cases with containerd's own setting. As run 3 wrote them, with R at `10.4.1.45` and the run's registry at `10.4.1.41`:

Form MR, cases x, a, b and the `cfg` cases of c (p01b's form A with M and then R as the server line):

```toml
server = "http://10.4.1.45:5000"
[host."https://mirror.corp.example/registry-example"]
  capabilities = ["pull", "resolve"]
  ca = "/etc/containerd/certs.d/HOST/ca.pem"
```

Form MR with a header, the `hdr` cases of c:

```toml
server = "http://10.4.1.45:5000"
[host."https://mirror.corp.example/registry-example"]
  capabilities = ["pull", "resolve"]
  ca = "/etc/containerd/certs.d/HOST/ca.pem"
  [host."https://mirror.corp.example/registry-example".header]
    Authorization = "Basic bXVzZXI6bS1wYXNzLXAwMg=="
```

The header's value is `Basic` and the base64 of `muser:m-pass-p02`.

Form D, cases d-m and d (no place for R; the run's registry first, then M as the server line, its `ca` at the top):

```toml
server = "https://mirror.corp.example/registry-example"
ca = "/etc/containerd/certs.d/d.registry.example/ca.pem"
[host."http://10.4.1.41:5000"]
  capabilities = ["pull"]
```

Form D with a header, cases d-hdr-m and d-hdr: form D for `d2.registry.example`, with a top-level header table after the `ca` line, which belongs to the server line:

```toml
[header]
  Authorization = "Basic bXVzZXI6bS1wYXNzLXAwMg=="
```

Case e: e0 `server = "https://e.registry.example"` alone; e1 `ca = "/etc/containerd/certs.d/e.registry.example/ca.pem"` alone; e2 both lines.

Case f, for `f.registry.example`: the server line for R, the run's registry with `capabilities = ["pull"]`, then M's block as in form MR.

**containerd's own setting**, before the four `cfg` cases, appended to `/etc/containerd/config.toml` in the worker only, followed by `systemctl restart containerd` and a wait until `crictl info` answers and the node is Ready:

```toml
[plugins."io.containerd.grpc.v1.cri".registry.configs."mirror.corp.example".auth]
  username = "muser"
  password = "m-pass-p02"
```

**How a pull is made.** A credential case is pulled by a pod: the kubelet looks credentials up in its file and passes them with the CRI pull, which `crictl pull` does not do. The pod has `nodeName` set to the worker, one container `app` of the case's reference `HOST/REPOSITORY@DIGEST`, `imagePullPolicy: IfNotPresent` and `restartPolicy: Never`. The program polls the pod every 250 ms, for at most 2 minutes, until its container starts or waits with `ErrImagePull` or `ImagePullBackOff`, and then deletes the pod at once with a grace period of 0, so that the kubelet does not try the pull again. Cases x and f use `crictl pull REFERENCE` in the worker, with no `--creds`.

**What each case observes**, as in p01b: before the pull, the logs of `mproxy`, `rproxy` and `run` are read until two reads 500 ms apart agree; after it they are read again in the same way, and every new request is printed as `info: CASE m: …`, `info: CASE r: …` or `info: CASE run: …`. Then the lines of the worker's containerd journal since the case began that contain `trying next host` or `fetch failed`.

**The cases**, in this order: x; a; b; `c-hdr-c1-m`, `c-hdr-c1-r`, `c-hdr-c2-m`, `c-hdr-c2-r`; d-m; d; d-hdr-m; d-hdr; e0; e1; e2; f; containerd's own setting and its restart; `c-cfg-c1-m`, `c-cfg-c1-r`, `c-cfg-c2-m`, `c-cfg-c2-r`. In a c case's name, `hdr` or `cfg` is the mechanism, `c1` is the host with `huser` in the kubelet's file and `c2` the host with nothing, and `m` means the image is only on M, `r` that it is missing from M and only on R.

Cases a, b, d-m, d, d-hdr-m and d-hdr are judged, against the expectations of the brief for this proof: a, that the pod pulls from M and M's log names `auser`, and R has no request; b, that the pull fails, every request to M and to R has no user, M answers 401 and R has a request; d-m and d-hdr-m, that the pod pulls, M answers the manifest with 200 to the expected user, and R has no request; d and d-hdr, that the pull fails, M answers the manifest with 404 to the expected user, and R has no request. Cases x, c, e and f are recorded: their `ok:` line says only that the pull ran and was observed, and gives what M and R saw. A case that did not run to its end counts as a failed check, and so does an interrupt. Either one stops the line `p02: all checks passed`. The program then calls `Down`.

The command, from `projects/rollproof/proofs/`:

```
env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go run ./p02-mirror-credentials
```

The program ran four times on 2026-11-20, between 11:12 and 11:24 UTC, one cluster at a time. `nerdctl ps -a` listed no container whose name begins with `rollproof-` before or after any run. Runs 1 and 2 used the program before one edit: it had no cases d-hdr-m and d-hdr, and nothing else in what it did differed. Runs 3 and 4 are the program as committed. No run was interrupted on purpose.

## Result

| Run | Program | Create attempts of `Up` | Last line | Exit | `nerdctl volume ls -q` before, after |
| --- | --- | --- | --- | --- | --- |
| 1 | before the d-hdr cases | 1 | `p02: all checks passed` | 0 | 57, 57 |
| 2 | before the d-hdr cases | 1 | `p02: all checks passed` | 0 | 57, 57 |
| 3 | as committed | 2 | `p02: all checks passed` | 0 | 57, 57 |
| 4 | as committed | 3 | `p02: all checks passed` | 0 | 57, 57 |

Each case printed the same numbers of requests, users and statuses for M, R and the run's registry in every run that had it. The quotes below are from run 3; digests are cut to their first eight hex digits here, and the full ones are in the table above.

**x. `crictl pull` with no `--creds`, of an image that only M holds.** The pull failed with `no basic auth credentials`. M and R were each asked twice, with no user, and answered 401:

```
info: x m: https HEAD /registry-example/v2/team/x/manifests/sha256:61cefd80…?ns=a.registry.example user=- 401
info: x m: https HEAD /registry-example/v2/team/x/blobs/sha256:61cefd80…?ns=a.registry.example user=- 401
info: x r: http HEAD /v2/team/x/manifests/sha256:61cefd80…?ns=a.registry.example user=- 401
info: x r: http HEAD /v2/team/x/blobs/sha256:61cefd80…?ns=a.registry.example user=- 401
```

crictl's error ended: `failed to resolve reference "a.registry.example/team/x@sha256:61cefd80…": pull access denied, repository does not exist or may require authorization: authorization failed: no basic auth credentials`. A `crictl pull` with no `--creds` sends a CRI pull with no credentials; the CRI plugin then looks in its own `registry.configs` for the host it contacts (`image_pull.go`, lines 105 to 114), finds nothing, and the authorizer answers a Basic challenge with no user by an error (`authorizer.go`, lines 195 to 203). The resolver goes on to the next host after such an error (`resolver.go`, lines 320 to 330), and after both hosts it tries the blobs path (lines 259 to 263 and 295 to 303), which is why each host was asked twice.

**a. Credentials for H in the kubelet's file; M accepts them; the image only on M.** The pod ran 506 ms after it was created. M was asked first with no user and answered 401, then answered 200 to `auser`, for the manifest and each blob. R had no request:

```
info: a m: https HEAD /registry-example/v2/team/a/manifests/sha256:33fa982d…?ns=a.registry.example user=- 401
info: a m: https HEAD /registry-example/v2/team/a/manifests/sha256:33fa982d…?ns=a.registry.example user=auser 200
info: a m: https GET /registry-example/v2/team/a/manifests/sha256:33fa982d…?ns=a.registry.example user=- 401
info: a m: https GET /registry-example/v2/team/a/manifests/sha256:33fa982d…?ns=a.registry.example user=auser 200
info: a m: https GET /registry-example/v2/team/a/blobs/sha256:ae0f0b9f…?ns=a.registry.example user=auser 200
info: a m: https GET /registry-example/v2/team/a/blobs/sha256:8bf34de8…?ns=a.registry.example user=auser 200
info: a r: no new request
ok: a: the pod pulled the image (pulled); M answered /registry-example/v2/team/a/manifests/A with 200 to the user auser; R had no request
```

The kubelet looks credentials up by the image's repository (`image_manager.go`, lines 293 and 319), matching the keys of its file by host and path (`keyring.go`, lines 294 to 316). It keeps only the user and the password of an entry (lines 137 to 141; `config.go`, lines 256 to 275 decode `auth`), so the CRI request carries no `ServerAddress` (`kuberuntime_image.go`, lines 51 to 61). The CRI plugin's credential function returns the request's credentials for every host it is asked about (`image_pull.go`, lines 105 to 114), and `ParseAuth` withholds them from another host only when `ServerAddress` is set (lines 341 to 354). The resolver and the fetcher each build their hosts anew, with a new authorizer (`resolver.go`, lines 525 to 527; `hosts.go`, lines 161 to 166), so each starts with no `Authorization` header and answers M's first challenge (`authorizer.go`, lines 153 to 209). That is the 401 before each 200.

**b. Credentials listed only under M's own host; the image only on M.** The pull failed 1.274 s after the pod was created, with `no basic auth credentials`. No request to M or to R carried a user:

```
info: b m: https HEAD /registry-example/v2/team/b/manifests/sha256:283c5137…?ns=b.registry.example user=- 401
info: b m: https HEAD /registry-example/v2/team/b/blobs/sha256:283c5137…?ns=b.registry.example user=- 401
info: b r: http HEAD /v2/team/b/manifests/sha256:283c5137…?ns=b.registry.example user=- 401
info: b r: http HEAD /v2/team/b/blobs/sha256:283c5137…?ns=b.registry.example user=- 401
ok: b: the pod's pull failed (failed: ErrImagePull: failed to pull and unpack image "b.registry.example/team/b@sha256:283c5137…": failed to resolve reference "b.registry.example/team/b@sha256:283c5137…": pull access denied, repository does not exist or may require authorization: authorization failed: no basic auth credentials); M had requests, none with a user; M answered 401; the pull went on to R: R had a request; no request to R carried a user
```

The kubelet looked up `b.registry.example/team/b`, and the key `mirror.corp.example` does not match it (`keyring.go`, lines 261 to 291), so it pulled with no credentials (`kuberuntime_image.go`, lines 38 to 48), as in case x.

**c. M needs its own credentials, `muser`.** What each of the eight pulls gave:

| Case | Mechanism | Kubelet's file for H | Image | Pull | M saw | R saw |
| --- | --- | --- | --- | --- | --- | --- |
| c-hdr-c1-m | header | huser | on M | pulled | 4 requests, all `muser`, all 200 | none |
| c-hdr-c1-r | header | huser | on R | pulled | 4 requests, all `muser`, all 404 | 6: `-` 401, then `huser` 200 |
| c-hdr-c2-m | header | none | on M | pulled | 4 requests, all `muser`, all 200 | none |
| c-hdr-c2-r | header | none | on R | failed, `not found` | 2 requests, `muser`, 404 | 2, `-`, 401 |
| c-cfg-c1-m | containerd's setting | huser | on M | failed, `401 Unauthorized` from M | 5 requests: `-` 401, then `huser` 401 four times | 2: `-` 401, `huser` 404 |
| c-cfg-c1-r | containerd's setting | huser | on R | pulled | 20 requests, `-` or `huser`, all 401 | 6: `-` 401, then `huser` 200 |
| c-cfg-c2-m | containerd's setting | none | on M | pulled | 6: `-` 401, then `muser` 200 | none |
| c-cfg-c2-r | containerd's setting | none | on R | failed, `not found` | 3: `-` 401, then `muser` 404 | 2, `-`, 401 |

With the header, M received `muser` on every request and no other user, and never challenged; R never received the header, and received the kubelet's credentials for H when it challenged:

```
info: c-hdr-c1-r m: https HEAD /registry-example/v2/team/c-hdr-c1-r/manifests/sha256:34be6e3a…?ns=c1.registry.example user=muser 404
info: c-hdr-c1-r m: https GET /registry-example/v2/team/c-hdr-c1-r/manifests/sha256:34be6e3a…?ns=c1.registry.example user=muser 404
info: c-hdr-c1-r m: https GET /registry-example/v2/team/c-hdr-c1-r/blobs/sha256:34e7fd6b…?ns=c1.registry.example user=muser 404
info: c-hdr-c1-r m: https GET /registry-example/v2/team/c-hdr-c1-r/blobs/sha256:c41f2155…?ns=c1.registry.example user=muser 404
info: c-hdr-c1-r r: http HEAD /v2/team/c-hdr-c1-r/manifests/sha256:34be6e3a…?ns=c1.registry.example user=- 401
info: c-hdr-c1-r r: http HEAD /v2/team/c-hdr-c1-r/manifests/sha256:34be6e3a…?ns=c1.registry.example user=huser 200
info: c-hdr-c1-r r: http GET /v2/team/c-hdr-c1-r/manifests/sha256:34be6e3a…?ns=c1.registry.example user=- 401
info: c-hdr-c1-r r: http GET /v2/team/c-hdr-c1-r/manifests/sha256:34be6e3a…?ns=c1.registry.example user=huser 200
info: c-hdr-c1-r r: http GET /v2/team/c-hdr-c1-r/blobs/sha256:34e7fd6b…?ns=c1.registry.example user=huser 200
info: c-hdr-c1-r r: http GET /v2/team/c-hdr-c1-r/blobs/sha256:c41f2155…?ns=c1.registry.example user=huser 200
```

A `header` table of a `[host]` block goes into that host's headers only (`hosts.go`, lines 521 to 537 and 234), and every request to that host starts from them (`resolver.go`, lines 550 to 557). The authorizer sets an `Authorization` header of its own only after a challenge from that host (`authorizer.go`, lines 121 to 133), and M, given valid credentials from the start, never challenged. So the kubelet's credentials for H do not override the header at M, and the header never leaves M's block.

With containerd's own setting, M received `muser` only when the kubelet passed no credentials (c2). When the kubelet passed `huser` (c1), M received `huser`, never `muser`, refused it five times on each path, and the pull went on to R:

```
info: c-cfg-c1-m m: https HEAD /registry-example/v2/team/c-cfg-c1-m/manifests/sha256:a6074aec…?ns=c1.registry.example user=- 401
info: c-cfg-c1-m m: https HEAD /registry-example/v2/team/c-cfg-c1-m/manifests/sha256:a6074aec…?ns=c1.registry.example user=huser 401
info: c-cfg-c1-m m: https HEAD /registry-example/v2/team/c-cfg-c1-m/manifests/sha256:a6074aec…?ns=c1.registry.example user=huser 401
info: c-cfg-c1-m m: https HEAD /registry-example/v2/team/c-cfg-c1-m/manifests/sha256:a6074aec…?ns=c1.registry.example user=huser 401
info: c-cfg-c1-m m: https HEAD /registry-example/v2/team/c-cfg-c1-m/manifests/sha256:a6074aec…?ns=c1.registry.example user=huser 401
info: c-cfg-c1-m r: http HEAD /v2/team/c-cfg-c1-m/manifests/sha256:a6074aec…?ns=c1.registry.example user=- 401
info: c-cfg-c1-m r: http HEAD /v2/team/c-cfg-c1-m/manifests/sha256:a6074aec…?ns=c1.registry.example user=huser 404
```

The pod's error named M's answer: `unexpected status from HEAD request to https://mirror.corp.example/registry-example/v2/team/c-cfg-c1-m/manifests/sha256:a6074aec…?ns=c1.registry.example: 401 Unauthorized`. The CRI plugin's credential function reads `registry.configs` only when the request has no credentials (`image_pull.go`, lines 105 to 114: `hostauth := r.GetAuth()`, and only when it is nil, `c.config.Registry.Configs[host]`). A Basic challenge answered with wrong credentials is retried with the same ones until the five attempts are spent (`resolver.go`, lines 733 and 821 to 835; `authorizer.go`, lines 195 to 209), and a 401 then sends the resolver to the next host (`resolver.go`, lines 343 to 366). After the restart the journal showed the deprecation and that the plugin still pulled in its local mode, and `containerd config dump` showed the setting under the plugin's version 3 name:

```
info: cfg: containerd: … level=warning msg="`configs` is deprecated, please use `config_path` instead"
info: cfg: containerd: … level=warning msg="Found 'DiscardUnpackedLayers' in CRI config which is incompatible with transfer service (not supported with transfer service). Falling back to local image pull mode."
info: cfg: containerd config dump: [plugins.'io.containerd.cri.v1.images'.registry.configs.'mirror.corp.example'.auth]
info: cfg: containerd config dump: username = 'muser'
```

containerd v2.3.4 still reads the setting, though its source says `DEPRECATED: Use ConfigPath instead. Remove in containerd 2.3.` (`internal/cri/config/config.go`, lines 239 to 243). It also turns the transfer service off for the CRI plugin (lines 592 to 596); on kind's nodes that changes nothing, because `discard_unpacked_layers` has turned it off already.

Whenever a pull went on to R, R received the kubelet's credentials for H when there were some (c-hdr-c1-r, c-cfg-c1-m, c-cfg-c1-r), and nothing otherwise (c-hdr-c2-r, c-cfg-c2-r: two requests with no user, each answered 401, and the pull failed). R never received `muser`, by either mechanism. Where both M's 404 and R's refusal happened, the error says only `not found`: in the resolver a 404 outranks an authorization error (`resolver.go`, lines 320 to 341).

**d. Forbidding the last step.** With form D, the image missing from M and from the run's registry and held by R (which would have accepted `auser`), the pull failed 1.27 s after the pod was created. The run's registry and M answered 404, M to `auser`, and R had no request:

```
info: d m: https HEAD /registry-example/v2/team/d/manifests/sha256:e5b57c33…?ns=d.registry.example user=- 401
info: d m: https HEAD /registry-example/v2/team/d/manifests/sha256:e5b57c33…?ns=d.registry.example user=auser 404
info: d m: https HEAD /registry-example/v2/team/d/blobs/sha256:e5b57c33…?ns=d.registry.example user=auser 404
info: d r: no new request
info: d run: HEAD /v2/team/d/manifests/sha256:e5b57c33…?ns=d.registry.example 404
info: d run: HEAD /v2/team/d/blobs/sha256:e5b57c33…?ns=d.registry.example 404
ok: d: the pod's pull failed (failed: ErrImagePull: rpc error: code = NotFound desc = failed to pull and unpack image "d.registry.example/team/d@sha256:e5b57c33…": failed to resolve reference "d.registry.example/team/d@sha256:e5b57c33…": d.registry.example/team/d@sha256:e5b57c33…: not found); M answered /registry-example/v2/team/d/manifests/D with 404 to the user auser; R had no request, so no credentials
```

containerd's journal named the run's registry with `trying next host after status: 404 Not Found` and M with `fetch failed after status: 404 Not Found`, for the manifest path and then for the blobs path: M was the last host. The same form with an image that M holds (d-m) pulled from M, with the run's registry asked first (404) and R not asked, so the form reaches M and d's failure is the missing last step:

```
info: d-m m: https HEAD /registry-example/v2/team/d-m/manifests/sha256:cd722b96…?ns=d.registry.example user=auser 200
info: d-m r: no new request
info: d-m run: HEAD /v2/team/d-m/manifests/sha256:cd722b96…?ns=d.registry.example 404
ok: d-m: the pod pulled the image (pulled); M answered /registry-example/v2/team/d-m/manifests/D-M with 200 to the user auser; R had no request
```

(The first two M lines and the GETs are as in case a.) With the header at the top instead, for `d2.registry.example`, which has no entry in the kubelet's file, M received `muser` on every request: d-hdr-m pulled from M (four requests, all `muser`, all 200; the run's registry asked first, 404; R not asked), and d-hdr failed with `not found` (M two requests, `muser`, 404; R not asked):

```
info: d-hdr m: https HEAD /registry-example/v2/team/d-hdr/manifests/sha256:bd00d984…?ns=d2.registry.example user=muser 404
info: d-hdr m: https HEAD /registry-example/v2/team/d-hdr/blobs/sha256:bd00d984…?ns=d2.registry.example user=muser 404
info: d-hdr r: no new request
ok: d-hdr-m: the pod pulled the image (pulled); M answered /registry-example/v2/team/d-hdr-m/manifests/D-HDR-M with 200 to the user muser; R had no request
ok: d-hdr: the pod's pull failed (failed: ErrImagePull: rpc error: code = NotFound desc = failed to pull and unpack image "d2.registry.example/team/d-hdr@sha256:bd00d984…": … not found); M answered /registry-example/v2/team/d-hdr/manifests/D-HDR with 404 to the user muser; R had no request, so no credentials
```

hosts.go puts the server line last, as written, with the top-level fields (`ca`, `header`) as its settings (lines 414 to 419), and fills in containerd's default host only when the last entry has no host (lines 98 to 135). A path on the server line gets `/v2` after it (lines 440 to 447), as on a `[host]` block. So a file whose server line is the mirror has no place for the real host.

**e. The `ca` at the top, for the real host, with no mirror.** e0, a server line and no `ca`, failed at R's certificate before any request reached R: `tls: failed to verify certificate: x509: certificate signed by unknown authority`. e1, the `ca` line alone, and e2, a server line and the `ca` line, both pulled from R over TLS, with six requests each (`-` 401, then `huser` 200, as in case a):

```
info: e1 r: https HEAD /v2/team/e1/manifests/sha256:5b5bd93c… user=- 401
info: e1 r: https HEAD /v2/team/e1/manifests/sha256:5b5bd93c… user=huser 200
info: e1 r: https GET /v2/team/e1/manifests/sha256:5b5bd93c… user=- 401
info: e1 r: https GET /v2/team/e1/manifests/sha256:5b5bd93c… user=huser 200
info: e1 r: https GET /v2/team/e1/blobs/sha256:fca1cfec… user=huser 200
info: e1 r: https GET /v2/team/e1/blobs/sha256:59801ac3… user=huser 200
```

No request carried `?ns=`: the host was the image's own. A file with only top-level fields gives one host with no name and those fields (`hosts.go`, lines 414 to 419); containerd then fills in `https://e.registry.example` and keeps the `ca` (lines 101 to 135), and a host with its own `ca` gets its own TLS client (lines 170 to 186).

**f. An image on the authenticated host's name, only in the run's registry, listed first with pull only; `crictl pull` with no `--creds`.** The pull succeeded. The run's registry served the manifest and both blobs, and M and R had no request:

```
info: crictl pull f.registry.example/team/f@sha256:a97e3894…: exit 0; its output:
info:   Image is up to date for sha256:dd99430d…
info: f m: no new request
info: f r: no new request
info: f run: HEAD /v2/team/f/manifests/sha256:a97e3894…?ns=f.registry.example 200
info: f run: GET /v2/team/f/manifests/sha256:a97e3894…?ns=f.registry.example 200
info: f run: GET /v2/team/f/blobs/sha256:dd99430d…?ns=f.registry.example 200
info: f run: GET /v2/team/f/blobs/sha256:0f442393…?ns=f.registry.example 200
```

The run's registry asks for no credentials, so no challenge came and none were needed.

**Volumes.** `nerdctl volume ls -q` counted 57 volumes before and after each run. The registries and the proxies run with a tmpfs at `/var/lib/registry`, so the image's `VOLUME` made no anonymous volume.

## Decision

These are proposals for the design of step 5; the spec is not changed by this record.

**A mirror with credentials of its own: a header in the mirror's block.** For a `registries` entry whose mirror needs credentials that differ from the host's, the tool writes them into the mirror's block of `hosts.toml` in every node:

```toml
[host."https://MIRROR"]
  capabilities = ["pull", "resolve"]
  ca = "/etc/containerd/certs.d/HOST/ca.pem"
  [host."https://MIRROR".header]
    Authorization = "Basic BASE64(USER:PASSWORD)"
```

It is the one mechanism of the two that worked in all four runs both with and without credentials for H in the kubelet's file (c-hdr-c1-m, c-hdr-c2-m), and whose credentials never reached the real host (c-hdr-c1-r, c-hdr-c2-r). It needs no restart: the file is read on the next pull. Its costs: the credentials sit in a file in every node, like the kubelet's file, and section 9's "Who can read the credentials" applies to it too; they go with every request to the mirror, so the mirror must be `https://`, which section 9 requires already; and it is a fixed header, which a mirror with token authentication may not accept (see the limits below).

containerd's own setting (`registry.configs."MIRROR".auth`) is not recommended. It was ignored whenever the kubelet had credentials for the image's host (c-cfg-c1-m failed, and M saw only `huser`), which is exactly the case where an entry lists credentials for its host. It needs a change to containerd's configuration and a restart, or a patch at cluster creation. It is deprecated, and its source comment names 2.3 for its removal, though 2.3.4 still reads it.

The kubelet's credentials for H still reach the mirror whenever the mirror challenges (case a) and the real host whenever a pull goes on to it (c-hdr-c1-r). With the header, the mirror does not challenge, so they no longer reach it.

**Forbidding the last step: it works, written with the mirror as the server line.** A node file that names the mirror on the server line, with its `ca` (and its header, if any) at the top, and no other place for the real host, never sends a pull of H to the real host: the pull of an image that the mirror lacks fails with `not found`, and the real host had no request (cases d and d-hdr). The run's registry, when the host has `as`, stays first as a `[host]` block:

```toml
server = "https://MIRROR"
ca = "/etc/containerd/certs.d/HOST/ca.pem"
[header]
  Authorization = "Basic BASE64(USER:PASSWORD)"
[host."http://ADDRESS:5000"]
  capabilities = ["pull"]
```

TOML requires the two top-level keys before any table. Whether the caller may forbid the last step remains a decision for the design; this record shows only that a node file can.

**The `ca` for the real host: at the top of the file, with or without a server line.** Both forms pulled over TLS from a host whose certificate the private authority signed (e1, e2); without the `ca` the pull fails at the certificate (e0). This settles p01b's second limit.

**Into the nodes, behind an authenticated host: no credentials needed.** When an entry of `images` names an authenticated host in `as`, the tool's `crictl pull` of the stored digest needs no `--creds`, because the run's registry answers first and does not challenge (case f). Section 9's set-up pull stands as written.

**Limits of what was run.**

- **No Bearer token authentication.** Every registry here answered with a Basic challenge. A mirror or real host that answers with a Bearer challenge (a token service, as Harbor, Artifactory or Docker Hub use) was not tried. For such a mirror, a static Basic header may be refused, and containerd's authorizer would then fetch a token with the kubelet's credentials or none (`authorizer.go`, lines 159 to 194); that was read, not run.
- **No credential-provider plugin of the kubelet.** Only the kubelet's `config.json` was used. A plugin's credentials, and `imagePullSecrets`, were not tried; the second goes through the same keyring (`pkg/credentialprovider/secrets`), read and not run.
- **One mirror, one real host, digests only.** No tag was pulled, and every image had one manifest. The mirror ignored the `?ns=` query.
- **The kubelet's file is read once in 5 minutes.** A file changed while the cluster runs is not seen at once (`provider.go`, lines 104 to 113); the tool writes it before the cluster exists, so this does not touch the design.
- **containerd's own setting was set in the worker only,** after a restart, and only for M's host. With it set at cluster creation, it would also apply to every other case that pulls with no kubelet credentials through M (case b's host, for one).
- **The mirror form with no server line,** which step 5 proposes for the usual case, was not run here either; every file with a mirror had a server line (form MR, form D, case f). p01b's first limit stands.
- **Names through `/etc/hosts`.** As in p01b, M and `e.registry.example` resolved through lines the program wrote into the nodes' `/etc/hosts`.

Plan assumed: "An entry's username and password, listed under the host in the kubelet's file, reach the entry's mirror too: the kubelet passes them with no host attached, and the container runtime offers them to every host it contacts for that pull."
Holds: yes
Plan assumed: "Credentials listed under a mirror's own host are never used for an image that is named after the original host."
Holds: yes
Plan assumed: "Credentials that a caller lists under the real host are offered to the real host whenever a pull goes on to it."
Holds: yes
Plan assumed: "A header in the mirror's block of `hosts.toml` gives a mirror credentials of its own, whether or not the kubelet's file has credentials for the image's host, and they do not reach the real host."
Holds: yes
Plan assumed: "The container runtime's own setting per host (`registry.configs.\"MIRROR\".auth`) gives a mirror credentials of its own, whether or not the kubelet's file has credentials for the image's host."
Holds: no
Plan assumed: "A node's settings can forbid the last step from the mirror to the real host."
Holds: yes
Plan assumed: "For the real host, a `ca` line at the top of `hosts.toml` is used, with or without a `server` line."
Holds: yes
Plan assumed: "The tool's `crictl pull` of an image that the run's registry holds, by its digest and under an authenticated host's name, succeeds with no credentials, because the run's registry answers first."
Holds: yes
