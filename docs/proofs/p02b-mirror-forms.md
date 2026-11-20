# Step 5, before the plan: the mirror forms the tool will write

## Question

Section 14, point 30 ruled that a listed mirror replaces its host: the node's settings file for host H has the mirror as its `server` line, the mirror's `ca` at the top, and the run's registry as the only `[host]` block, with no place for the real host (form D of `docs/proofs/p02-mirror-credentials.md`). Credentials come only from the kubelet's file, keyed by host. Before the plan for step 5, eight questions about that form: (A) does it work with a `v2` path and `override_path` on the `server` line; (B) does a tag go through it; (C) can the nodes reach a mirror named by its container name, and how do they resolve it; (D) can the nodes' route to everything else be cut; (E) how are Docker Hub's names, settings folders and kubelet keys matched; (F) where does a pod's own pull secret go; (G) what does a failed pull say; (H) where do the credentials go when the mirror answers with a token challenge?

## Machine and versions

- System: the developer's machine, Linux 7.3.3, amd64, 16 CPUs.
- Engine: nerdctl 2.4.2, rootless, with rootlesskit and slirp4netns (`--net=slirp4netns`, `--disable-host-loopback`, as `ps` lists the processes). The p08 record lists its containerd, runc and rootlesskit. Docker is absent. The engine is rootless, so the lab adds the `NoNewKeyring = true` patch.
- The network `kind`: the subnets `10.4.1.0/24` and `fc00:f853:ccd:e793::/64`, as `nerdctl network inspect kind` gives them.
- Go 1.27.3, for the program, the test binaries and the stand-in binary. The certificates are made with Go's `crypto/x509`, with ECDSA P-256 keys.
- kind: `sigs.k8s.io/kind` v0.33.0; `k8s.io/client-go` v0.37.2; `github.com/google/go-containerregistry` v0.22.1, which pushes and tags the images.
- Node image: kind's default, `kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5`. In the worker: `containerd github.com/containerd/containerd/v2 v2.3.4 db8809540e1a7a9da5d518876894933ff55692ab`, `crictl version 1.36.0`, `Kubernetes v1.37.0`, `iptables v1.8.11 (legacy)`, `iptables v1.8.11 (nf_tables)` for `iptables-nft`, and `nftables v1.1.3`.
- Registries: `docker.io/library/registry:3`, held in the engine's store as `registry@sha256:82f6ff4a7f982960c8aa51617fea3aabc485df6e55cce80d22deb1de36f1bc3c` (3.1.2, as the p01 record read its log).
- Public image, pulled once and tried once in case D: `docker.io/library/busybox@sha256:a7dbc319b1438115a207e11930dc4bb3e643dcb3ec0bdd694bb36cd8fad289ec` (busybox 1.37.0's index).
- Sources read, at the node's versions. Kubernetes v1.37.0: `pkg/credentialprovider/keyring.go`, `provider.go` and `secrets/secrets.go`, `pkg/util/parsers/parsers.go`, `pkg/kubelet/images/image_manager.go`, `pkg/kubelet/kuberuntime/kuberuntime_image.go` and `kuberuntime_manager.go`. `github.com/distribution/reference` v0.6.0 (both go.mod files name it): `normalize.go`. containerd v2.3.4: `internal/cri/server/images/image_pull.go`, `core/remotes/docker/config/hosts.go` and `config_unix.go`, `core/remotes/docker/resolver.go`, `core/remotes/docker/authorizer.go`. kind v0.33.0: `pkg/cluster/internal/providers/nerdctl/provision.go`, `images/base/files/usr/local/bin/entrypoint`. Go 1.27.3: `src/net/dnsclient_unix.go`, `src/net/addrselect.go`. Docker's documentation, "Networking overview" (`https://docs.docker.com/engine/network/`), sections "User-defined networks", "DNS services" and "Custom hosts", read on 2026-11-20.

## Method

The program is `proofs/p02b-mirror-forms/main.go`. It reuses the shape of p02's program: its pod pulls, its log reading and its checks, and its logging proxy, extended as below.

**The stand-ins.** All run on the network `kind`, and the engine names each `rollproof-proof-p02b-NAME`:

| Name | What it is | Reached by the nodes as |
| --- | --- | --- |
| `run` | `l.Registry`, plain HTTP: the run's registry, empty | through `runproxy` |
| `runproxy` | the logging proxy, plain HTTP on 5000, in front of `run` | `http://ADDRESS:5000`, by address, as the tool names its registry |
| `mreg` | `l.Registry`, open | through `mproxy` |
| `mproxy` | M: the logging proxy over TLS on 443 in front of `mreg`. It serves only paths under `/registry-example/`, `/folder-docker.io/` and `/folder-index.docker.io/`, and strips the prefix before it passes a request on; any other path gets 404. It demands Basic credentials itself, from the users `auser`, `dkuser`, `ikuser`, `vkuser`, `rkuser` and `fuser`, and answers others with 401 and `WWW-Authenticate: Basic realm="m"` | `https://rollproof-proof-p02b-mproxy/…` |
| `creg` | `l.Registry`, open; its repositories begin with `proxy-project/`, as p01b's project | through `cproxy` |
| `cproxy` | C: the logging proxy over TLS on 443 in front of `creg`, paths unchanged, no credentials | `https://rollproof-proof-p02b-cproxy/v2/proxy-project` |
| `m2fill` | registry:3, open, on a folder of the runner; case H pushes its image through it | not reached |
| `m2` | registry:3 on the same folder with `REGISTRY_AUTH=token`, realm `https://rollproof-proof-p02b-tok/token`, service `rollproof-p02b-m2`, issuer `rollproof-p02b-tok`, and the CA as its root certificate bundle | through `m2proxy` |
| `m2proxy` | M2: the logging proxy over TLS on 443 in front of `m2`, paths and headers unchanged | `https://rollproof-proof-p02b-m2proxy` |
| `tok` | T: the token service over TLS on 443 | `https://rollproof-proof-p02b-tok/token`, from M2's challenge |

Case H starts the last four, when it begins.

**The logging proxy** is p02's, held as a string in the program, built with `CGO_ENABLED=0` into a temporary folder, and run by the engine from that folder in a container of the registry:3 image with `--entrypoint`. It takes several path prefixes, and with `-users` it checks Basic credentials itself and removes the `Authorization` header before it passes a request on, so that M needs no htpasswd file. It writes one line for each request after it has been answered:

```
REQ NAME LISTENER METHOD PATH?QUERY user=USER auth=SCHEME sub=SUB status=CODE
```

USER is the user of a Basic `Authorization` header; SUB is the `sub` claim of a Bearer token, read without a check; each is `-` when absent.

**The token service** is the same binary in its second mode. On `GET /token` it reads Basic credentials, `scope` and `service` from the query; on `POST /token` it reads `grant_type`, `username`, `password`, `scope` and `service` from the form. It accepts the user `huser` only. For an accepted request it returns `{"token", "access_token", "expires_in": 300, "issued_at"}` with a JWT signed with ES256 by a signing key whose certificate, signed by the run's CA, is in the token's `x5c` header; the claims are `iss`, `sub` (the user), `aud` (the service), `exp`, `nbf`, `iat`, `jti` and `access`, which grants `pull` on each repository the scope names. It writes one line for each request:

```
TOK NAME METHOD PATH via=basic|form|none user=USER grant=GRANT service=SERVICE scope=SCOPES status=CODE
```

Checks by hand, outside the program: before the first run, two containers of the registry:3 image showed that nerdctl adds a starting container to the `/etc/hosts` of a running one on the same network, and takes it out when that container is removed; two more showed that T's token, fetched with `curl` by GET and by POST, was accepted by registry:3 with token authentication (`404 NAME_UNKNOWN` for a repository it did not hold, where a request with no token got 401). After run 1, privileged containers of the node image showed that `iptables` (legacy) cannot make its `filter` table there, and that `iptables-nft`, `ip6tables-nft` and `nft` can, with REJECT rules that refused a connection at once.

**The certificates.** A CA; one server certificate signed by it for the four names the nodes reach (`rollproof-proof-p02b-mproxy`, `-cproxy`, `-m2proxy` and `-tok`) and `127.0.0.1`; and the token signing certificate. All are valid for one day. The CA's key is never written.

**The kubelet's file.** As in p02: a Docker `config.json` whose `auths` entries carry `auth`, mounted read-only at `/var/lib/kubelet/config.json` in both nodes through kind's `extraMounts`. Outside case E it lists `b.registry.example` (auser), `g.registry.example` (auser) and `h.registry.example` (huser). `a.registry.example` and `f.registry.example` have no entry. Case E rewrites the runner's file in place, checks that the worker reads the new text at the mount, and restarts the worker's kubelet (`systemctl restart kubelet`, then waits for `healthz` to answer `ok` and the node to be Ready), because the kubelet keeps what it read for 5 minutes in memory (`provider.go`, lines 102 to 113; `kuberuntime_manager.go`, line 359).

**The cluster.** `lab.Up(ctx, "p02b", cfg)` makes one control-plane node and one worker, each with the mount, with p01's patch that sets `config_path = "/etc/containerd/certs.d"`, and with the pod subnet `10.244.0.0/16` and the service subnet `10.96.0.0/16` written out. The program writes no line into a node's `/etc/hosts`. It writes the CA as `/etc/containerd/certs.d/HOST/ca.pem` in every node, for `a`, `b`, `f`, `g` and `h.registry.example` after the cluster is up, and for `docker.io` and `index.docker.io` in case E.

**Eight images,** each one static binary that prints its name and sleeps, built with `-ldflags "-s -w -X main.v=V"` and pushed with `lab.PushBinary`; `dial` first dials the addresses in `$DIAL` for at most 3 seconds each and prints one line for each. Every run gave the same digests:

| Image | Digest | Held by, as |
| --- | --- | --- |
| a | `sha256:87bacb0648c20f248f4bbb64ad65bc61e1571a86d0fdd31979a41af9082fa9eb` | C, `proxy-project/team/app` and `:v1` |
| dial | `sha256:c7b5530dd2f6263a81c23f32add32b16989ea4e4ed9365c723eb4d101368f8b8` | C, `proxy-project/team/dial` |
| b | `sha256:96055f09fca06cea7b0df67265c9c6c79bcacee1de22981fc97f4cb344d53e4c` | M, `team/app` and `:v1` |
| d | `sha256:54e0677136347b38e8d2dfa032d9596e1ad7fc405f4e0cc204774890a0fa7766` | M, `team/d` |
| e | `sha256:cae876c1d75dc4fa82452c8d42bb7ff875bb15d7cdc0b640b9d8e4b191be9210` | M, `library/app` |
| f | `sha256:4fe60cca3cd9e64d62765c5f32fa6fc73fa8f3288eb409bbbfc538c437990796` | M, `team/f` |
| g | `sha256:36ff04736c01b1a5f94dafd98540088d727fc65d44d49988446bfbe7375ce3d9` | M, `team/g` and `:v1` |
| h | `sha256:1e5be056672941f630c036f588e9e4d87bfffec19ad2536d91c8ceb06d337886` | M2, `team/h` |

Below, digests are cut to their first eight hex digits; D is image e's digest.

**The settings files.** Every case writes form D to `/etc/containerd/certs.d/HOST/hosts.toml` in every node (`cp /dev/stdin`); nothing restarts containerd. As run 3 wrote them, with the run's proxy at `10.4.1.103`:

```toml
server = "https://rollproof-proof-p02b-cproxy/v2/proxy-project"
override_path = true
ca = "/etc/containerd/certs.d/a.registry.example/ca.pem"
[host."http://10.4.1.103:5000"]
  capabilities = ["pull"]
```

That is case A with `override_path` (a1, a2, and the dialing pod of D); a3 and a4 leave out the second line. The other files differ only in the `server` line and the folder of the `ca`:

| Host | `server` | Cases |
| --- | --- | --- |
| `b.registry.example` | `https://rollproof-proof-p02b-mproxy/registry-example` | B, D |
| `docker.io` | `https://rollproof-proof-p02b-mproxy/folder-docker.io` | E |
| `index.docker.io` | `https://rollproof-proof-p02b-mproxy/folder-index.docker.io` | E |
| `f.registry.example` | `https://rollproof-proof-p02b-mproxy/registry-example` | F |
| `g.registry.example` | `https://rollproof-proof-p02b-mproxy/registry-example` | G |
| `h.registry.example` | `https://rollproof-proof-p02b-m2proxy` | H |

**How a pull is made.** By a pod, as in p02, so that the kubelet looks up its credentials: `nodeName` the worker, one container of the reference, `restartPolicy: Never`, and `imagePullPolicy: Always`, after a `crictl rmi` of the image in the worker (repeated until `crictl inspecti` no longer finds it, for at most 10 seconds), so that each pull fetches again. The program polls the pod every 250 ms for at most 2 minutes until its container starts or waits with `ErrImagePull` or `ImagePullBackOff`; in case G it goes on polling until `ImagePullBackOff` or for 45 seconds after the first failure, then lists the pod's events. It deletes the pod with a grace period of 0. Case D's two pulls of the public image and its pull from M use `crictl pull` in the worker, the last with `--creds auser:a-pass-p02b`.

**What each case observes,** as in p02: the new lines of every stand-in's log that has started, read until two reads 500 ms apart agree, printed as `info: CASE WHO: …` with WHO `m`, `c`, `run`, `m2` or `t`; then the lines of the worker's containerd journal since the case began that contain `trying next host` or `fetch failed`.

**The cut, case D.** The program reads the subnets of the network `kind` from `nerdctl network inspect kind`, and runs this script in every node with `sh -s` (as run 3 printed it):

```
set -e
iptables-nft -N ROLLPROOF-CUT
iptables-nft -A ROLLPROOF-CUT -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
iptables-nft -A ROLLPROOF-CUT -o lo -j RETURN
iptables-nft -A ROLLPROOF-CUT -d 10.4.1.0/24 -j RETURN
iptables-nft -A ROLLPROOF-CUT -d 10.244.0.0/16 -j RETURN
iptables-nft -A ROLLPROOF-CUT -d 10.96.0.0/16 -j RETURN
iptables-nft -A ROLLPROOF-CUT -p tcp -j REJECT --reject-with tcp-reset
iptables-nft -A ROLLPROOF-CUT -j REJECT --reject-with icmp-port-unreachable
iptables-nft -I OUTPUT 1 -j ROLLPROOF-CUT
iptables-nft -I FORWARD 1 -j ROLLPROOF-CUT
ip6tables-nft -N ROLLPROOF-CUT
ip6tables-nft -A ROLLPROOF-CUT -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
ip6tables-nft -A ROLLPROOF-CUT -o lo -j RETURN
ip6tables-nft -A ROLLPROOF-CUT -d fc00:f853:ccd:e793::/64 -j RETURN
ip6tables-nft -A ROLLPROOF-CUT -d fe80::/10 -j RETURN
ip6tables-nft -A ROLLPROOF-CUT -d ff00::/8 -j RETURN
ip6tables-nft -A ROLLPROOF-CUT -p tcp -j REJECT --reject-with tcp-reset
ip6tables-nft -A ROLLPROOF-CUT -j REJECT --reject-with icmp6-port-unreachable
ip6tables-nft -I OUTPUT 1 -j ROLLPROOF-CUT
ip6tables-nft -I FORWARD 1 -j ROLLPROOF-CUT
```

A packet of a connection that already exists passes, so the runner's connections into the nodes (the API server's port mapping, the pod logs) keep working; a new connection, from the node or from a pod it forwards, passes only to loopback, the network's subnets, the pod and service subnets, and IPv6 link-local and multicast addresses.

**The cases**, in this order: a1, a2, a3, a4; b; c; d (the public pull before the cut, the cut, the public pull after it, the pull from M, C's resolution after the cut, the dialing pod, the counters); e (16 pulls); f; g1, g1t, g2, g2t; h. Cases E to H run only when D's gate held: the cut was applied in every node, the public pull after it failed within 15 seconds, and the pull from M worked. Otherwise the program stops after D, and E to H count as not run.

Judged: a1 to a4, b, c, d, f and h, each against what the brief for this proof expects, as their `ok:` lines state. Recorded: e, whose line says that all 16 pulls ran and were observed, and g1 to g2t, whose lines say that the pull failed and the pod had events. A case that did not run to its end counts as a failed check, and so does an interrupt; either stops the line `p02b: all checks passed`. The program then calls `Down`.

The command, from `projects/rollproof/proofs/`:

```
env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go run ./p02b-mirror-forms
```

The program ran four times on 2026-11-20, between 12:17 and 12:36 UTC, one cluster at a time. `nerdctl ps -a` listed no container whose name begins with `rollproof-` before or after any run. Run 1 used `iptables` for the cut. Run 2 used `iptables-nft`, and its check of the dialing pod counted every occurrence of `dial ` in the log, error texts included. Runs 3 and 4 are the program as committed. No run was interrupted.

## Result

| Run | Program | Create attempts of `Up` | Last line | Exit | `nerdctl volume ls -q` before, after |
| --- | --- | --- | --- | --- | --- |
| 1 | `iptables` for the cut | 1 | `p02b: 9 checks failed` | 1 | 59, 59 |
| 2 | `iptables-nft`; the dial check before its fix | 2 | `p02b: 1 checks failed` | 1 | 59, 59 |
| 3 | as committed | 2 | `p02b: all checks passed` | 0 | 59, 59 |
| 4 | as committed | 1 | `p02b: all checks passed` | 0 | 59, 59 |

Run 1 passed a1 to c, and failed at the cut: in the control-plane node the script's first command printed `modprobe: ERROR: could not insert 'ip_tables': Operation not permitted` and `iptables v1.8.11 (legacy): can't initialize iptables table 'filter': Table does not exist (do you need to insmod?)`. kind's entrypoint had set the nodes' `iptables` to the legacy backend, and a rootless node cannot load the kernel module that backend needs. D failed its gate, and E to H were not run, as the program intends. Run 2 failed one check of D, the dialing pod's: its log held the three lines expected, and the check also counted `dial tcp` in two error texts; every other check, D's gate included, held. Runs 2 to 4 printed the same requests, users and statuses for each case, apart from addresses and times, with one difference: in case E with the key `https://index.docker.io/v1/`, runs 2 and 4 asked neither the run's registry nor M for one of the image's two blobs, which the node still held from an earlier pull, and run 3 asked for both. The quotes below are from run 3.

**A. The `v2` path on the `server` line.** With `override_path = true` at the top, a1 (by digest) and a2 (by tag) pulled, and C received the path as written, with the repository after it:

```
info: a1 c: https HEAD /v2/proxy-project/team/app/manifests/sha256:87bacb06…?ns=a.registry.example user=- auth=- 200
info: a1 c: https GET /v2/proxy-project/team/app/manifests/sha256:87bacb06…?ns=a.registry.example user=- auth=- 200
info: a1 c: https GET /v2/proxy-project/team/app/blobs/sha256:94026498…?ns=a.registry.example user=- auth=- 200
info: a1 c: https GET /v2/proxy-project/team/app/blobs/sha256:6b6ece3f…?ns=a.registry.example user=- auth=- 200
info: a1 run: http HEAD /v2/team/app/manifests/sha256:87bacb06…?ns=a.registry.example user=- auth=- 404
info: a2 c: https HEAD /v2/proxy-project/team/app/manifests/v1?ns=a.registry.example user=- auth=- 200
info: a2 run: http GET /v2/team/app/manifests/sha256:87bacb06…?ns=a.registry.example user=- auth=- 404
```

(a1's run's registry also answered 404 to the GETs of the manifest and both blobs; a2's C lines for the manifest and blobs by digest are as a1's.) The run's registry was asked for nothing by tag. Without `override_path`, a3 and a4 failed with `not found`, and C received `/v2` after the server line's path:

```
info: a3 c: https HEAD /v2/proxy-project/v2/team/app/manifests/sha256:87bacb06…?ns=a.registry.example user=- auth=- 404
info: a3 c: https HEAD /v2/proxy-project/v2/team/app/blobs/sha256:87bacb06…?ns=a.registry.example user=- auth=- 404
info: a4 c: https HEAD /v2/proxy-project/v2/team/app/manifests/v1?ns=a.registry.example user=- auth=- 404
info: a4 run: no new request
```

a3's message: `rpc error: code = NotFound desc = failed to pull and unpack image "a.registry.example/team/app@sha256:87bacb06…": failed to resolve reference "a.registry.example/team/app@sha256:87bacb06…": a.registry.example/team/app@sha256:87bacb06…: not found`. The top of the file is parsed into the same structure as a `[host]` block, `override_path` included, and given to the `server` line (`hosts.go`, lines 380 to 421); a path that does not end in `/v2` gets `/v2` added unless `override_path` is set (lines 440 to 448).

**B. A tag through form D, a prefix mirror with credentials.** The pod pulled `b.registry.example/team/app:v1`. M resolved the tag, with the kubelet's credentials for H after its challenge; the run's registry was asked only for the manifest and the blobs by digest, after M had resolved them:

```
info: b m: https HEAD /registry-example/v2/team/app/manifests/v1?ns=b.registry.example user=- auth=- 401
info: b m: https HEAD /registry-example/v2/team/app/manifests/v1?ns=b.registry.example user=auser auth=basic 200
info: b m: https GET /registry-example/v2/team/app/manifests/sha256:96055f09…?ns=b.registry.example user=- auth=- 401
info: b m: https GET /registry-example/v2/team/app/manifests/sha256:96055f09…?ns=b.registry.example user=auser auth=basic 200
info: b m: https GET /registry-example/v2/team/app/blobs/sha256:238771b7…?ns=b.registry.example user=auser auth=basic 200
info: b m: https GET /registry-example/v2/team/app/blobs/sha256:939c681b…?ns=b.registry.example user=auser auth=basic 200
info: b run: http GET /v2/team/app/manifests/sha256:96055f09…?ns=b.registry.example user=- auth=- 404
info: b run: http GET /v2/team/app/blobs/sha256:238771b7…?ns=b.registry.example user=- auth=- 404
info: b run: http GET /v2/team/app/blobs/sha256:939c681b…?ns=b.registry.example user=- auth=- 404
```

A tag is resolved only by a host with `resolve` (`resolver.go`, lines 264 to 270); the run's block has `pull` only.

**C. The mirror's name, resolved as the nodes resolve names.** Right after `Up`, the worker's `/etc/hosts` held only nerdctl's block with the two nodes; after the stand-ins started, nerdctl had rewritten it in both nodes with a line for each container on the network, by its name and its ID, for each of its two addresses (run 3, the worker, trimmed to M's and C's lines):

```
# <nerdctl>
127.0.0.1	localhost localhost.localdomain
::1		localhost localhost.localdomain
…
10.4.1.106      92d60e61bb04 92d60e61bb04.kind rollproof-proof-p02b-mproxy rollproof-proof-p02b-mproxy.kind
fc00:f853:ccd:e793::b49 92d60e61bb04 92d60e61bb04.kind rollproof-proof-p02b-mproxy rollproof-proof-p02b-mproxy.kind
10.4.1.107      d5811a80a224 d5811a80a224.kind rollproof-proof-p02b-cproxy rollproof-proof-p02b-cproxy.kind
fc00:f853:ccd:e793::b4a d5811a80a224 d5811a80a224.kind rollproof-proof-p02b-cproxy rollproof-proof-p02b-cproxy.kind
…
# </nerdctl>
```

The worker's `/etc/nsswitch.conf` has `hosts:          files dns`; its `/etc/resolv.conf` has `search localdomain`, `nameserver 10.0.2.3` (slirp4netns's built-in forwarder to the machine's resolvers) and a second name server on the machine's own network, left out here. `getent -s files ahosts` gave both of M's addresses from the file alone. After the cut (case D), `getent hosts rollproof-proof-p02b-mproxy` still answered, and `getent -s dns hosts` for the same name failed with exit code 2. Every pull of B to H named its mirror this way. The check of case C held in every run: no stand-in's name was in a node's `/etc/hosts` right after `Up`, each was there afterwards inside nerdctl's block with its address, and B's pull succeeded.

The stand-ins started after the nodes, so this shows nerdctl updating the file of every running container on the network when another one starts. When M was stopped in case G, its two lines stayed. The order of a container's two lines varies (in run 2 M's IPv6 line came first, and `getent hosts` gave its IPv6 address in every run), yet every connection to M in the runs went to its IPv4 address (`dial tcp 10.4.1.106:443` in case G; the counters of case D): Go's resolver sorts the addresses from the file by RFC 6724 (`dnsclient_unix.go`, line 627), in which `fc00::/7` has precedence 3 and IPv4 35 (`addrselect.go`, lines 227 to 271). The node's containerd is a Go program; with which Go version it was built was not read.

How Docker differs, read and not run: on a user-defined network a container resolves the others by name through Docker's embedded DNS server at `127.0.0.11`, which forwards other names to the host's resolvers, and its `/etc/hosts` holds only its own name, `localhost` and a few others (Docker's "Networking overview", sections "User-defined networks", "DNS services" and "Custom hosts"). kind's entrypoint rewrites a node's `resolv.conf` from `127.0.0.11` to the network's gateway address and turns Docker's DNAT rules to that address, for the node and its pods (`entrypoint`, lines 446 to 487); with nerdctl the `resolv.conf` holds no `127.0.0.11`, and the rewrite changes nothing. So on Docker a node would resolve M's container name through DNS, not through `/etc/hosts`, and so would a pod, through CoreDNS; with nerdctl a pod cannot (CoreDNS forwards to `10.0.2.3`, which does not know the names), which no pull needs.

**D. Cutting the nodes' route.** Before the cut, `crictl pull` of the public image succeeded in 1.443 s. After it:

```
info: crictl pull docker.io/library/busybox@sha256:a7dbc319…: exit non-zero after 45ms; its output:
…failed to do request: Head "https://registry-1.docker.io/v2/library/busybox/manifests/sha256:a7dbc319…": dial tcp: lookup registry-1.docker.io on <the second name server>:53: write udp 10.4.1.101:53213-><the second name server>:53: write: operation not permitted
```

The lookup failed at the node: a REJECT rule turns a local UDP send into an error at once. The pull from M then worked (`crictl pull --creds auser:… b.registry.example/team/d@sha256:54e06771…: exit 0 after 105ms`; M answered the manifest and both blobs with 200 to `auser`, the run's registry 404 to each). The rule stood first in the worker's chains, before kube-proxy's:

```
-A OUTPUT -j ROLLPROOF-CUT
-A OUTPUT -j KUBE-FIREWALL
…
-A FORWARD -j ROLLPROOF-CUT
-A FORWARD -m conntrack --ctstate NEW -m comment --comment "kubernetes load balancer firewall" -j KUBE-PROXY-FIREWALL
```

The dialing pod, pulled from C through form D with the cut in place, logged:

```
dial 10.0.2.3:53: failed after 0s: dial tcp 10.0.2.3:53: connect: connection refused
dial registry-1.docker.io:443: failed after 3.002s: dial tcp: lookup registry-1.docker.io: i/o timeout
dial 10.4.1.107:443: connected after 0s
```

The first is the forwarded rule's reset; the second, CoreDNS's upstream query cut at the node, so the lookup timed out; the third, C's address. At the end of D the IPv4 REJECT rules had counted 23 packets in the control-plane node and 41 in the worker (1 of them a TCP reset); the IPv6 REJECT rules counted none in either node. Cases E to H all ran with the cut in place.

**E. Docker Hub.** The node files for `docker.io` and `index.docker.io` named M under `/folder-docker.io` and `/folder-index.docker.io`. For each key alone in the kubelet's file, the pods pulled the four names; the user at M shows the key, the prefix the folder. The same table came out in runs 2, 3 and 4:

| Key in the kubelet's file | `docker.io/library/app@D` | `library/app@D` | `app@D` | `index.docker.io/library/app@D` |
| --- | --- | --- | --- | --- |
| `docker.io` | pulled; M: `dkuser` | pulled; M: `dkuser` | pulled; M: `dkuser` | pulled; M: `dkuser` |
| `index.docker.io` | pulled; M: `ikuser` | pulled; M: `ikuser` | pulled; M: `ikuser` | pulled; M: `ikuser` |
| `https://index.docker.io/v1/` | pulled; M: `vkuser` | pulled; M: `vkuser` | pulled; M: `vkuser` | pulled; M: `vkuser` |
| `registry-1.docker.io` (extra) | failed; M: no user | failed; M: no user | failed; M: no user | failed; M: no user |

Every request to M, for all 16 pulls, came through the folder `docker.io`: no request carried `/folder-index.docker.io`. The `imageID` of each of the 12 pods that pulled was `docker.io/library/app@sha256:cae876c1…`. One pull, the key `index.docker.io` and the name `app@D`:

```
info: e-k2-n3 m: https HEAD /folder-docker.io/v2/library/app/manifests/sha256:cae876c1…?ns=docker.io user=- auth=- 401
info: e-k2-n3 m: https HEAD /folder-docker.io/v2/library/app/manifests/sha256:cae876c1…?ns=docker.io user=ikuser auth=basic 200
info: e-k2-n3 run: http HEAD /v2/library/app/manifests/sha256:cae876c1…?ns=docker.io user=- auth=- 404
```

With the key `registry-1.docker.io`, M answered both requests with 401, containerd logged `fetch failed` with `pull access denied, repository does not exist or may require authorization: authorization failed: no basic auth credentials`, and the pod's message was only `… docker.io/library/app@sha256:cae876c1…: not found` (see G for why).

Why, from the source. The kubelet looks credentials up by the repository that `parsers.ParseImageName` gives (`image_manager.go`, lines 293 and 319), which is the name of `reference.ParseNormalizedNamed` (`parsers.go`, lines 32 to 38): every one of the four names becomes `docker.io/library/app`, because a name with no domain gets `docker.io` and `library/`, and `index.docker.io` is turned into `docker.io` (`normalize.go`, lines 126 to 167). The keyring stores a key with its scheme and a `/v1/` or `/v2/` path removed, so `https://index.docker.io/v1/` is stored as `index.docker.io` (`keyring.go`, lines 143 to 167). `Lookup` first matches each key against the repository by host and path, so `docker.io` matches; when no key matches, it uses the key `index.docker.io` for any name that `isDefaultRegistryMatch` takes for Docker Hub, which `docker.io/library/app` is (lines 192 to 214 and 294 to 316). `registry-1.docker.io` matches neither way. containerd turns the pod's name into `docker.io/library/app@D` with `ParseDockerRef` (`image_pull.go`, line 153; `normalize.go`, line 107), asks for the hosts of `refspec.Hostname()` (`resolver.go`, lines 525 to 527), and looks for `certs.d/docker.io`, then `certs.d/_default` (`config_unix.go`, lines 25 to 37; `hosts.go`, lines 293 to 305). It never forms the name `index.docker.io`, so that folder is never read.

**F. A pod's own pull secret.** With no entry for `f.registry.example` in the kubelet's file and the Secret `pullf` (type `kubernetes.io/dockerconfigjson`, one `auths` entry for `f.registry.example` with `fuser`) in the pod's `imagePullSecrets`, the pod pulled. M received `fuser` after its challenge, as with the kubelet's file; the run's registry received no credentials; nothing else was asked:

```
info: f m: https HEAD /registry-example/v2/team/f/manifests/sha256:4fe60cca…?ns=f.registry.example user=- auth=- 401
info: f m: https HEAD /registry-example/v2/team/f/manifests/sha256:4fe60cca…?ns=f.registry.example user=fuser auth=basic 200
info: f run: http HEAD /v2/team/f/manifests/sha256:4fe60cca…?ns=f.registry.example user=- auth=- 404
info: f c: no new request
```

(M's GETs of the manifest and the blobs, with `fuser`, and the run's registry's 404s by GET are left out.) The kubelet adds a pull secret's `auths` to a keyring of its own with the same `Add` as the file's, and joins it to the node's (`secrets/secrets.go`, lines 29 to 57); the entry keeps only its user and password, so the CRI request carries no server address and containerd offers it to any host that challenges in the pull (`image_pull.go`, lines 105 to 114 and 341 to 360), as p02 found for the file.

**G. What a failed pull says.** All four pulls failed. Each pod had the same five events in its own words; for g1:

```
Normal Pulling: Pulling image "g.registry.example/team/g@sha256:00000000…"
Warning Failed: Failed to pull image "g.registry.example/team/g@sha256:00000000…": rpc error: code = NotFound desc = failed to pull and unpack image "g.registry.example/team/g@sha256:00000000…": failed to resolve reference "g.registry.example/team/g@sha256:00000000…": g.registry.example/team/g@sha256:00000000…: not found
Warning Failed: Error: ErrImagePull
Normal BackOff: Back-off pulling image "g.registry.example/team/g@sha256:00000000…"
Warning Failed: Error: ImagePullBackOff
```

The container's status first waited with `ErrImagePull` and the message of the second event, after `rpc error: …`, then with `ImagePullBackOff` and `Back-off pulling image "…": ErrImagePull: ` followed by the same message. The four cases, the digest written out in full in the program's output:

| Case | M | Reference | First failure after (run 3) | What the status and the events name |
| --- | --- | --- | --- | --- |
| g1 | running, lacks the digest | `g.registry.example/team/g@sha256:0…0` | 1.27 s | H only: `…: not found` |
| g1t | running, lacks the repository | `g.registry.example/team/missing:v1` | 0.506 s | H only: `…: not found` |
| g2 | stopped | `g.registry.example/team/g@sha256:36ff0473…` | 45.578 s | H only: `…: not found` |
| g2t | stopped | `g.registry.example/team/g:v1` | 6.816 s | H and M |

g2t's status: `failed to pull and unpack image "g.registry.example/team/g:v1": failed to resolve reference "g.registry.example/team/g:v1": failed to do request: Head "https://rollproof-proof-p02b-mproxy/registry-example/v2/team/g/manifests/v1?ns=%5BREDACTED%5D": dial tcp 10.4.1.106:443: connect: no route to host`. The `ns` query, which names H, is shown as `%5BREDACTED%5D` in every message; containerd's journal writes it out. In g2, M had no request and the run's registry answered 404; containerd's journal shows why the first failure took 45.6 s (34.5 s in run 2, 53.4 s in run 4): the connection to M's stale address timed out after 30 s, then a later one failed with `no route to host`:

```
… msg="trying next host after status: 404 Not Found" host="10.4.1.103:5000" method=HEAD url="http://10.4.1.103:5000/v2/team/g/manifests/sha256:36ff0473…?ns=g.registry.example"
… msg="fetch failed" error="failed to do request: Head \"https://rollproof-proof-p02b-mproxy/registry-example/v2/team/g/manifests/sha256:36ff0473…?ns=g.registry.example\": dial tcp 10.4.1.106:443: i/o timeout" host=rollproof-proof-p02b-mproxy …
```

The resolver keeps the error of the highest priority among its hosts: a failure to connect or to authorize has priority 1, a 404 priority 2, another status of 400 or more priority 3 (`resolver.go`, lines 280 to 368). A pull by digest through form D asks the run's registry first, which answers 404, so the message is `not found` with H's reference whenever M's failure is a 404, a failure to connect, or a challenge it had no credentials for: a missing image (g1), a stopped mirror (g2), and missing credentials at M (case E, key `registry-1.docker.io`). M's answer outranks the 404 only when it comes back as another status of 400 or more; p02's case c-cfg-c1-m, where M refused wrong credentials, ended with `unexpected status from HEAD request to https://mirror.corp.example/…: 401 Unauthorized`, which this proof did not run again. By tag, M is the only host asked, and a failure to connect names it (g2t); its 404 reads `not found` (g1t).

**H. A token challenge.** From the runner first, M2 answered a request with no token with 401 and `WWW-Authenticate: Bearer realm="https://rollproof-proof-p02b-tok/token",service="rollproof-p02b-m2",scope="repository:team/h:pull"`, and accepted T's token for `huser` with 200. Then the pod pulled `h.registry.example/team/h@sha256:1e5be056…`. T received the kubelet's credentials for H by POST, as a form, and M2 received only bearer tokens whose subject is `huser`:

```
info: h m2: https HEAD /v2/team/h/manifests/sha256:1e5be056…?ns=h.registry.example user=- auth=- 401
info: h m2: https HEAD /v2/team/h/manifests/sha256:1e5be056…?ns=h.registry.example user=- auth=bearer sub=huser 200
info: h m2: https GET /v2/team/h/manifests/sha256:1e5be056…?ns=h.registry.example user=- auth=- 401
info: h m2: https GET /v2/team/h/manifests/sha256:1e5be056…?ns=h.registry.example user=- auth=bearer sub=huser 200
info: h m2: https GET /v2/team/h/blobs/sha256:489bf63a…?ns=h.registry.example user=- auth=bearer sub=huser 200
info: h m2: https GET /v2/team/h/blobs/sha256:f9d90a69…?ns=h.registry.example user=- auth=bearer sub=huser 200
info: h t: POST /token via=form user=huser grant=password service=rollproof-p02b-m2 scope=repository:team/h:pull 200
info: h t: POST /token via=form user=huser grant=password service=rollproof-p02b-m2 scope=repository:team/h:pull 200
info: h run: http HEAD /v2/team/h/manifests/sha256:1e5be056…?ns=h.registry.example user=- auth=- 404
```

(The run's registry also answered 404 to the GETs by digest.) On a Bearer challenge containerd asks the credential function for M2's host, gets the kubelet's credentials, and fetches a token from the realm: by POST with `grant_type=password` when it has a password, falling back to GET with Basic credentials only on 400, 401, 404, or 405 with a user (`authorizer.go`, lines 160 to 194 and 308 to 340). The resolver and the fetcher each start a new authorizer, so each challenged once and fetched its own token, as p02 found for Basic challenges. The token is fetched with the client of M2's host, which carries M2's `ca` (`hosts.go`, lines 152 to 212); T's certificate was signed by the same authority as M2's.

**Volumes.** `nerdctl volume ls -q` counted 59 volumes before and after each run. The machine had 57 when this proof began; two anonymous volumes were made, empty, at 12:01 UTC by two containers of the registry:3 image that a check before the program started without a tmpfs at `/var/lib/registry`, and they were left in place, because their names do not begin with `rollproof-`. The program's registries and proxies run with a tmpfs there, and M2's storage is a folder of the runner, so the program made no volume.

## Decision

These are proposals for the plan of step 5; the spec is not changed by this record.

**A `v2` path: form D takes `override_path` at the top.** For a `mirror` whose path begins with the segment `v2`, the tool writes `override_path = true` as a top-level key, beside `ca` and before the run's block:

```toml
server = "https://cache.corp.example/v2/proxy-project"
override_path = true
ca = "/etc/containerd/certs.d/HOST/ca.pem"
[host."http://ADDRESS:5000"]
  capabilities = ["pull"]
```

This was point 30's "read and not run"; it is now run, by digest and by tag (a1, a2). Without the line the node asks for `/v2/proxy-project/v2/…`, and the pull fails `not found` (a3, a4).

**Tags.** Form D resolves a tag at the mirror only; the run's registry is asked for content by digest and never for a tag (a2, b), as section 9 says.

**Docker Hub: the caller writes `docker.io`; the tool writes the folder `docker.io` and the key `docker.io`.** Every Docker Hub name a pod can use reaches the node as `docker.io/library/…`, containerd reads only `certs.d/docker.io/` for it, and the kubelet gives the credentials of the key `docker.io` to every such name (E). The keys `index.docker.io` and `https://index.docker.io/v1/` work too, through the keyring's fallback; `registry-1.docker.io` matches nothing. Proposal: the configuration accepts Docker Hub's entry only as `host: docker.io`, and rejects `index.docker.io` and `registry-1.docker.io` as a `host`, with a message that names `docker.io`: a folder `index.docker.io` is never read, and a key `registry-1.docker.io` is never used, so either would fail with no sign but a pull that fails `not found`. Not run: `as` naming Docker Hub, and a Docker Hub path other than `library/`.

**The mirror job: the mirror is named by its container name, and the route is cut with `iptables-nft` rules.** The job gives its mirror a certificate for its container name and names it so in `mirror`. Through nerdctl the nodes resolve it from the `/etc/hosts` that the engine rewrites when a container on the network starts (C, where the mirror started after the nodes; a mirror started before the cluster was not run); through Docker they would resolve it by the embedded DNS (read, not run). The job cuts the route in every node after the cluster is made, with the script of the Method: `iptables-nft` and `ip6tables-nft`, because kind sets the nodes' `iptables` to the legacy backend, which a rootless node cannot use (run 1); connections that exist pass, and new ones pass only to loopback, the engine network's subnets, the pod and service subnets, and IPv6 link-local and multicast. A public pull then fails in well under a second, and pulls from mirrors on the engine's network work (D). The rules allow everything on the engine's network, so a token service the job uses must sit there too. With Docker, DNS goes to the network's gateway address, inside the subnet, and is turned to `127.0.0.11` on loopback, both of which the rules allow; read, not run.

**Pull secrets go to the mirror.** A chart's `imagePullSecrets` entry for H reaches the mirror after its challenge, and not the real host, which has no place (F). Section 9's "the tool does nothing for them and does not get in their way" stands, and should say where they now go.

**A token service gets the entry's credentials; the mirror gets only tokens.** With a mirror that answers with a Bearer challenge, the entry's username and password go to the token service the challenge names, by POST, and the mirror receives bearer tokens (H). The token service must be reachable from the nodes and trusted through the entry's `ca`, which is the authority used for the mirror; a token service whose certificate another authority signed was not run.

**A failed pull names H, not the mirror, whenever the run's registry is asked first.** For a host that has `as`, every pull by digest goes to the run's registry first, which answers 404, and the pod's events and status then say `… H/…@DIGEST: not found` for a missing image, a stopped mirror and missing credentials alike (G, E); wrong credentials, refused with 401, would name the mirror, as in p02. The mirror's address appears only when it is the only place asked and the failure is a connection error (g2t). A stopped mirror also took 30 s and more to fail. Section 9's "the pod's events give the mirror's answer" does not hold. Proposal for the maintainer: the tool's report, for an image that could not be pulled from a host with a mirror, names the mirror the node was set to use, and says that the node's message cannot tell a missing image from a mirror that is down or refuses the credentials.

**Limits of what was run.**

- **nerdctl only.** Docker's DNS and the cut on a Docker node are read, not run.
- **One token flow.** T accepted POST; the GET fallback, a token service that refuses the credentials, and one whose certificate another authority signed were not tried. M2 is registry:3; Harbor, Artifactory and Docker Hub's token services were not tried.
- **The kubelet was restarted** between the keys of case E. The tool writes the file once, before the cluster exists.
- **One image per name, no image index,** and the mirrors ignored the `?ns=` query, as in p02.
- **`_default`** and a folder for a host with a port were not tried.
- **The cut's ESTABLISHED rule** lets packets of an existing connection out. No connection to a public address existed when the cut was made, and none could start after it.

Plan assumed: "With `override_path = true` at the top of the node's settings file, beside the `ca`, a `server` line whose path begins with `/v2` is used as written, by digest and by tag; without it, the node puts `/v2` after that path and the pull fails."
Holds: yes
Plan assumed: "Through form D, a tag is resolved by the mirror only, and the run's registry is not asked for it."
Holds: yes
Plan assumed: "Through nerdctl, a node resolves a mirror named by its container name on the engine's network, started after the cluster, from the /etc/hosts that the engine writes, with nothing written into the node by the tool."
Holds: yes
Plan assumed: "Through Docker, a node resolves a mirror named by its container name through Docker's embedded DNS."
Holds: not measured here
Plan assumed: "Rules in every node can cut the nodes' route, their own and the one they forward, to every address outside the engine network's subnets and the cluster's pod and service subnets, so that a public pull fails quickly and a pull from a mirror on the engine's network works."
Holds: yes
Plan assumed: "Every name of a Docker Hub image (`docker.io/library/app`, `library/app`, `app`, `index.docker.io/library/app`) is pulled through the settings folder `docker.io`, and the kubelet gives the mirror the credentials of a key `docker.io`, `index.docker.io` or `https://index.docker.io/v1/`."
Holds: yes
Plan assumed: "A pod's own pull secret for host H reaches the mirror, and no other place."
Holds: yes
Plan assumed: "A mirror that lacks an image, or is down, fails the pull, and the pod's events give the mirror's answer."
Holds: no
Plan assumed: "When a mirror answers with a Bearer challenge, the kubelet's credentials for H go to the token service that the challenge names, and the mirror receives only the token."
Holds: yes
