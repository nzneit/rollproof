# Point 19: a run behind an HTTP proxy

## Question

Point 19 of section 14. Section 9 says that kind passes `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY` into its nodes and exempts the network's subnets, the node names and the cluster's service names, and that the tool names its registry by address so that those exemptions cover it. Does kind v0.33.0's nerdctl provider do that, and how does it differ from its Docker provider? With the proxy variables set, does a run of the tool pass, with no request for the run's registry going through the proxy, while the nodes' container runtime does use the proxy? And are the tool's own clients, which reach the API server and the run's registry on the runner's loopback, ever proxied?

## Machine and versions

- System: the developer's machine, Linux 7.3.3, amd64, 16 CPUs.
- Engine: nerdctl 2.4.2, rootless, with buildkit for `nerdctl build`. Docker is absent. The runner has no route to the network `kind`: `ip route get` sends an address of it to the default gateway, not to the engine.
- Go 1.27.3, for the program, the tool, the fixture service and the proxy.
- The tool: `cmd/rollproof` of this branch, built as the e2e suite builds it. It links `sigs.k8s.io/kind` v0.33.0, `k8s.io/client-go` v0.37.2 and `github.com/google/go-containerregistry` v0.22.1. Helm 3.22.1 (`.tools/helm-3/helm`, `v3.22.1+gd6c1a0e`) and kubectl v1.37.0 (`.tools/kubectl`).
- Node image: the tool's default, `kindest/node:v1.37.0`; the tool's console names Kubernetes v1.37.0. The node's `/usr/local/bin/containerd` is containerd v2.3.4 (p02 record), built with go1.26.7 (`go version` of the file copied out of the node).
- Images: `docker.io/library/registry:3` from the engine's store, which carries the proxy; `docker.io/library/busybox:1.37.0`, pulled from Docker Hub by a node.
- Sources read: kind v0.33.0, `pkg/cluster/internal/providers/nerdctl/provision.go`, `docker/provision.go`, `docker/proxyconfig.go`, `podman/provision.go`, `common/proxy.go`, and `images/base/files/usr/local/bin/entrypoint`; Go 1.27.3, `src/net/http/transport.go` and `src/vendor/golang.org/x/net/http/httpproxy/proxy.go`; client-go v0.37.2, `transport/cache.go`; go-containerregistry v0.22.1, `pkg/v1/remote/options.go`; containerd v2.3.4, `core/remotes/docker/registry.go`; the tool's `internal/cluster/registry.go`, `internal/cluster/hosts.go`, `internal/probe/http.go`; and `e2e/harness_test.go` and `e2e/config_test.go` for how the suite runs the first fixture.

## Method

**Read first, not run: kind's source.** The nerdctl provider passes the variables. For each node, `planCreation` gathers every node's name first, for `NO_PROXY` (`nerdctl/provision.go`, lines 38 to 50), and the node's run arguments add `-e KEY=VALUE` for each entry of `getProxyEnv` (lines 163 to 170). `getProxyEnv` (lines 262 to 283) starts from `common.GetProxyEnvs` and, when that is not empty, sets `NO_PROXY` and `no_proxy` to, in this order: the subnets of the engine's network, read by `getSubnets` with `nerdctl network inspect -f '{{range (index (index . "IPAM") "Config")}}{{index . "Subnet"}} {{end}}' kind` (lines 286 to 293); the `NO_PROXY` from `common`; the node names; and `.svc`, `.svc.cluster`, `.svc.cluster.local`. `common.getProxyEnvs` (`common/proxy.go`, lines 41 to 61) reads `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY` from the environment, each falling back to its lower-case name, sets both cases, and adds the cluster's service subnet and pod subnet to `NO_PROXY`.

The Docker provider does the same, and one thing more. Its `getProxyEnv` (`docker/provision.go`, lines 293 to 318) builds `NO_PROXY` the same way, from its own `getSubnets` (lines 342 on). When neither `HTTP_PROXY` nor `HTTPS_PROXY` is set in either case (`docker/proxyconfig.go`, lines 139 to 146), it also reads the proxies of Docker's client configuration, `$DOCKER_CONFIG/config.json` or `~/.docker/config.json`, for the current Docker host or `default` (lines 45 to 75 and 77 on), and merges them in (`docker/provision.go`, lines 320 to 340). The nerdctl provider has no such fallback. The Podman provider is the same as nerdctl's (`podman/provision.go`, lines 264 to 285).

Inside the node the variables are the container's environment. kind's entrypoint makes systemd hand the upper-case three to every service it starts, containerd and the kubelet among them (`images/base/files/usr/local/bin/entrypoint`, `configure_proxy`, lines 150 to 158, called at line 563).

**Read first, not run: Go's proxy rules.** Loopback is never proxied by Go. `net/http`'s `ProxyFromEnvironment` uses `httpproxy.FromEnvironment().ProxyFunc()` (`transport.go`, lines 1038 to 1040). Its `useProxy` returns false for the host `localhost` and for any loopback address before it looks at `NO_PROXY` at all (`httpproxy/proxy.go`, lines 170 to 187). `NO_PROXY` entries that parse as a CIDR exempt every address inside them (lines 226 to 230, matched at 192 to 197). The tool's clients all use `ProxyFromEnvironment`: client-go's transport (`transport/cache.go`, lines 142 to 148), go-containerregistry's default transport (`remote/options.go`, line 119), and the tool's wait for its registry, whose `http.Client` has no transport of its own and so uses `http.DefaultTransport` (`internal/cluster/registry.go`, line 68; `transport.go`, line 48). Each of them reaches `127.0.0.1`: kind's kubeconfig names the API server there, and the tool publishes its registry on a loopback port (`internal/cluster/registry.go`, lines 40 to 59). The built-in probe runs in the cluster and sets `Proxy: nil` (`internal/probe/http.go`, line 125). containerd's own client for registries also uses `http.ProxyFromEnvironment` (`core/remotes/docker/registry.go`, line 256); the httpproxy source above is Go 1.27.3's, and the node's containerd was built with go1.26.7, whose copy was not read.

**The run.** The program is `proofs/p19-proxy/main.go`. It makes no cluster of its own: the tool makes the cluster.

1. **Checks.** `.tools/helm-3/helm` and `.tools/kubectl` exist; no container whose name begins with `rollproof-proof-p19-` exists; it counts `nerdctl volume ls -q`; it runs `getSubnets`'s command and prints the subnets.
2. **The proxy.** A Go forward proxy of about 80 lines, held as a string in the program and built with `CGO_ENABLED=0` into a temporary folder. It answers `CONNECT host:port` by dialling the target and copying both ways, and forwards a plain-HTTP request in the absolute form. Before it handles a request it writes `REQ CONNECT HOST:PORT from ADDRESS:PORT`, or `REQ METHOD URL host=HOST from ADDRESS:PORT`. The engine runs it:

   ```
   nerdctl run -d --name rollproof-proof-p19-proxy --network kind --tmpfs /var/lib/registry -v $TMP/…/proxy:/opt/p19:ro --entrypoint /opt/p19/fwdproxy docker.io/library/registry:3 -listen :3128
   ```

   The program reads the container's address on the network `kind` and waits for the proxy's line `fwdproxy listening on :3128`.
3. **The tool and the fixture images**, as the suite's `TestMain` builds them, from the project's folder with `CGO_ENABLED=0`: `go build -trimpath -ldflags "-s -w -X main.version=dev" -o $TMP/…/rollproof ./cmd/rollproof`; and for V = `previous` and `candidate`, `go build -trimpath -ldflags "-s -w -X main.version=V" -o DIR/svc ./fixtures/service`, the fixture's Dockerfile beside it, and `nerdctl build -t rollproof-fixture:V DIR`.
4. **The run description of the first fixture**, rendered as the suite's `renderConfig` renders `fixture{name: "first"}`, with the suite's template unchanged. As run 1 printed it:

   ```yaml
   version: 1
   cluster: {"workers":3}
   images: {"candidate":{"local":"rollproof-fixture:candidate"},"previous":{"local":"rollproof-fixture:previous"}}
   deploy:
     namespace: orders
     helm:
       release: orders
       previous: {"chart":"$HOME/Projects/REPOSITORY/projects/rollproof/fixtures/chart","values":[{"image":{"digest":"${images.previous.digest}","repository":"${images.previous.repository}"}}]}
       candidate: {"chart":"$HOME/Projects/REPOSITORY/projects/rollproof/fixtures/chart","values":[{"image":{"digest":"${images.candidate.digest}","repository":"${images.candidate.repository}"}}]}
   probe: {"http":{"url":"http://orders.orders.svc/work?ms=20"}}
   ```

5. **The run.** The tool runs from the project's folder as the suite's `runFixture` runs it, with `--keep-cluster` added and a name of this proof's:

   ```
   $TMP/…/rollproof run -f $TMP/…/rollproof.yaml --out $TMP/…/out --name proof-p19 --keep-cluster
   ```

   Its environment is the program's, without any `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY` or `ALL_PROXY` in either case, with `PATH` starting with a folder that links `helm` to `.tools/helm-3/helm` and `kubectl` to `.tools/kubectl`, `KIND_EXPERIMENTAL_PROVIDER=nerdctl`, and `HTTP_PROXY` and `HTTPS_PROXY` set to `http://PROXY:3128`. The program waits at most 20 minutes, and reads `result.json`.
6. **What it checks**, each printed as `ok:` or `FAILED:`:
   - `verdict`: the tool exited 0, and `result.json` is final with the verdict `pass`.
   - `registry-not-proxied`: no line the proxy wrote during the run names the kept registry's address on the network `kind`, `127.0.0.1`, `localhost` or `rollproof.test`. The program also prints every line the proxy wrote during the run.
   - `node-env`: in each of the four kept nodes, `tr '\0' '\n' < /proc/$(pidof containerd)/environ` holds `HTTP_PROXY` and `HTTPS_PROXY` equal to `http://PROXY:3128`, and a `NO_PROXY` that holds, as entries, every subnet from step 1, `10.96.0.0/16`, `10.244.0.0/16` (kind's default service and pod subnets), each node's name, `.svc`, `.svc.cluster` and `.svc.cluster.local`. It also prints the control-plane node's proxy variables as the engine gave them, the tool's `hosts.toml` for `rollproof.test` in that node, and the Go release of the node's containerd.
   - `public-pull`: `crictl pull docker.io/library/busybox:1.37.0` in the last worker succeeds, and the proxy writes `REQ CONNECT registry-1.docker.io:443 …` during it.
   - `cleanup`: last, also when a step failed, it runs `nerdctl rm -f -v` on every container whose name begins with `rollproof-proof-p19-` (the four nodes, the tool's registry and the proxy), removes the tool's run folder `/tmp/rollproof-proof-p19`, and then requires that no such container is left and that the volume count is what it was.

   A check that did not run counts as failed, and so does an interrupt. Either one stops the line `p19: all checks passed`.

The command, from `projects/rollproof/proofs/`:

```
env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go run ./p19-proxy
```

The program ran twice on 2026-11-20, between 11:24 and 11:30 UTC, one cluster at a time, both times as committed. `nerdctl ps -a` listed no container whose name begins with `rollproof-` before or after either run. No run was interrupted on purpose.

## Result

| Run | The tool's time and exit | Verdict and windows | Proxy lines during the run | Proxy lines during the public pull | Last line | Exit | `nerdctl volume ls -q` before, after |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | 1 min 55 s, exit 0 | pass; steady, deploy, rollback pass | 0 | 6 `CONNECT` | `p19: all checks passed` | 0 | 57, 57 |
| 2 | 2 min 28 s, exit 0 | pass; steady, deploy, rollback pass | 0 | 6 `CONNECT` | `p19: all checks passed` | 0 | 57, 57 |

Run 2 printed the same lines as run 1, apart from addresses, ports, times and the probe's counts. The quotes below are from run 1.

**The verdict.** The tool ran for 1 min 55 s and passed:

```
info: the tool ran for 1m55s; its exit: exit 0
info: result.json: final true, verdict "pass", windows map[deploy:pass rollback:pass steady:pass]
ok: verdict: the tool exited 0 and result.json is final with the verdict pass (exit exit 0, final true, verdict "pass")
```

Its console began `rollproof: PASS   orders (Helm) on Kubernetes v1.37.0 in kind`; the steady, deploy and rollback windows had 2,000, 5,200 and 5,137 checks with none failed, and both transitions replaced 2 of 2 pods of `deployment/orders`. The install window had 3,196 checks and 8 failures (refused 6, timeout 2), which the tool does not judge, as in the suite's runs.

**The run's registry was not reached through the proxy.** The proxy wrote no request line at all while the tool ran:

```
info: the proxy's requests during the run: 0
info: the run's registry rollproof-proof-p19-registry: 10.4.1.61:5000 on the network kind
ok: registry-not-proxied: no request in the proxy's log during the run names 10.4.1.61, 127.0.0.1, localhost or rollproof.test (0 requests in all)
```

The tool's settings file for `rollproof.test` in the control-plane node names the registry by that address, which lies inside the subnet `10.4.1.0/24` of `NO_PROXY`:

```
info: rollproof-proof-p19-control-plane: /etc/containerd/certs.d/rollproof.test/hosts.toml:
info:   [host."http://10.4.1.61:5000"]
info:     capabilities = ["pull"]
```

The pods of `deployment/orders` ran both fixture images (the console's `images` lines), which exist under `rollproof.test` only in that registry, and the proxy wrote no line. On the runner, every request of the tool's own clients went to `127.0.0.1`, which Go never proxies. Had one gone to the proxy instead, it would have failed and the run with it: the runner has no route to the proxy's address.

**The nodes' container runtime holds the variables.** The engine gave the control-plane node both cases of each variable; containerd in each node holds the upper-case three, with the same values in all four nodes:

```
info: rollproof-proof-p19-control-plane, from the engine: HTTP_PROXY=http://10.4.1.56:3128
info: rollproof-proof-p19-control-plane, from the engine: HTTPS_PROXY=http://10.4.1.56:3128
info: rollproof-proof-p19-control-plane, from the engine: NO_PROXY=fc00:f853:ccd:e793::/64,10.4.1.0/24,10.96.0.0/16,10.244.0.0/16,rollproof-proof-p19-control-plane,rollproof-proof-p19-worker,rollproof-proof-p19-worker2,rollproof-proof-p19-worker3,.svc,.svc.cluster,.svc.cluster.local
info: rollproof-proof-p19-worker3: containerd's environment: HTTPS_PROXY=http://10.4.1.56:3128
info: rollproof-proof-p19-worker3: containerd's environment: HTTP_PROXY=http://10.4.1.56:3128
info: rollproof-proof-p19-worker3: containerd's environment: NO_PROXY=fc00:f853:ccd:e793::/64,10.4.1.0/24,10.96.0.0/16,10.244.0.0/16,rollproof-proof-p19-control-plane,rollproof-proof-p19-worker,rollproof-proof-p19-worker2,rollproof-proof-p19-worker3,.svc,.svc.cluster,.svc.cluster.local
ok: node-env: in each of the 4 nodes, containerd's HTTP_PROXY and HTTPS_PROXY are http://10.4.1.56:3128 and its NO_PROXY holds the network's subnets, the service and pod subnets, the node names and the .svc names
```

The order of `NO_PROXY` is the source's: the network's two subnets (the IPv6 one first, as `nerdctl network inspect` lists them), the service and pod subnets that `common` added to an empty `NO_PROXY`, the four node names, and the three `.svc` names. The engine also passed `http_proxy`, `https_proxy` and `no_proxy`; containerd's environment holds only the upper-case names, which kind's entrypoint gives to systemd.

**The proxy was in effect.** `crictl pull docker.io/library/busybox:1.37.0` in `rollproof-proof-p19-worker3` succeeded, and the proxy wrote six `CONNECT` lines from that worker's address:

```
info: crictl pull docker.io/library/busybox:1.37.0 in rollproof-proof-p19-worker3: exit 0; output: Image is up to date for sha256:30ecbe1509090b38ef3260f3609d64d8261230464eaa4d235901db496e9647fb
info: proxy during the public pull: REQ CONNECT registry-1.docker.io:443 from 10.4.1.60:54800
info: proxy during the public pull: REQ CONNECT auth.docker.io:443 from 10.4.1.60:54816
info: proxy during the public pull: REQ CONNECT registry-1.docker.io:443 from 10.4.1.60:54820
info: proxy during the public pull: REQ CONNECT auth.docker.io:443 from 10.4.1.60:54832
info: proxy during the public pull: REQ CONNECT registry-1.docker.io:443 from 10.4.1.60:54848
info: proxy during the public pull: REQ CONNECT production.cloudfront.docker.com:443 from 10.4.1.60:54858
ok: public-pull: crictl pull docker.io/library/busybox:1.37.0 in rollproof-proof-p19-worker3 succeeded and the proxy logged CONNECT registry-1.docker.io:443 during it (pull error <nil>; 6 proxy lines)
```

**Cleanup.**

```
info: cleanup: removed rollproof-proof-p19-registry, rollproof-proof-p19-worker3, rollproof-proof-p19-worker2, rollproof-proof-p19-worker, rollproof-proof-p19-control-plane, rollproof-proof-p19-proxy
info: nerdctl volume ls -q after: 57 volumes
ok: cleanup: no container whose name begins rollproof-proof-p19- is left ([]), and the volume count is 57 as before (now 57)
p19: all checks passed
```

## Decision

These are proposals for the design of step 5; the spec is not changed by this record.

**Section 9's paragraph on proxies stands for nerdctl, as written.** kind v0.33.0's nerdctl provider passes the three variables and exempts the network's subnets, the service and pod subnets, the node names and the `.svc` names. The tool names its registry by its address on the network, inside an exempted subnet, so the nodes reach it directly, and the tool adds nothing to the variables. Section 9 could name the service and pod subnets beside the network's subnets, and say that the names exempted for services are the suffixes `.svc`, `.svc.cluster` and `.svc.cluster.local`.

**The tool's own clients need nothing either.** They reach only `127.0.0.1`, which Go does not proxy whatever the variables say. This holds as long as the tool reaches the API server and its registry on the loopback address, which section 9 requires for the first release.

**Limits of what was run.**

- **nerdctl only.** The Docker provider builds `NO_PROXY` the same way (read), and also takes proxies from Docker's client configuration when the environment sets none. Neither was run; a run behind a proxy with Docker waits for a hosted runner, with point 8's Docker half.
- **One run of the first fixture, under Helm 3 only.** Helm 4 was not run behind the proxy. The chart was a local folder, so no chart was pulled through the proxy.
- **The engine pulled nothing.** The node image and `registry:3` were in the engine's store; the engine's own pulls behind a proxy are the caller's (section 9) and were not tried.
- **A plain proxy.** It needed no authentication and was reached over plain HTTP. A proxy with credentials in its URL, a proxy over TLS, a caller's own `NO_PROXY`, and variables given only in lower case were not tried. A node's pull from a public registry went through the proxy (the busybox pull), but no pull from a mirror or from a registry with credentials behind the proxy was made.
- **Pods.** Only containerd's environment was read. Whether a workload's pods see the variables was not looked at; the tool's built-in probe sets no proxy.

Plan assumed: "kind v0.33.0 with the nerdctl provider passes HTTP_PROXY, HTTPS_PROXY and NO_PROXY into its nodes, where the container runtime holds them, and adds to NO_PROXY the network's subnets, the node names and the cluster's service names."
Holds: yes
Plan assumed: "kind v0.33.0 with the Docker provider does the same."
Holds: not measured here
Plan assumed: "Behind a proxy, the nodes pull from the run's registry, which the tool names by its address on the network, and no request for it goes through the proxy; the run passes."
Holds: yes
Plan assumed: "The tool's own clients reach the API server and the run's registry on the runner's loopback address, which is never proxied."
Holds: yes
