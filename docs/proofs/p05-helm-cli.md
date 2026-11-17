# Point 5: Helm's command lines in both majors

## Question

Point 5 of section 14: a job on `ubuntu-latest` changes Helm major when that label moves, so the first release is tested with Helm 3.22 and Helm 4.3. Do the command lines of section 7 ("The Helm driver", "The candidate's values", "The cluster Helm acts on") exist in both majors and mean the same? This record also gives the revisions that an install, an upgrade and a rollback give, how the revision is read, the exit code of a failure, and which environment variables override the command line.

## Machine and versions

- System: the developer's machine, Linux 7.3.3, amd64.
- Engine: nerdctl 2.4.2, rootless (the p08 and p12 records give its parts). Docker is absent.
- Go 1.27.3.
- kind: `sigs.k8s.io/kind` v0.33.0; `k8s.io/client-go` v0.37.2.
- Node image: kind's default, `kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5`.
- Helm 3.22.1 (`helm version --short` prints `v3.22.1+gd6c1a0e`) and Helm 4.3.2 (`v4.3.2+g8346aaa`), linux-amd64. `go run ./hack/fetchhelm` put them at `.tools/helm-3/helm` and `.tools/helm-4/helm` after it checked the SHA-256 of each release archive.
- The registry: `docker.io/library/registry:3`, as the lab starts it, without TLS.
- The chart's image: `registry.k8s.io/pause:3.10`, which the p12 record found in the nodes.

## Method

The program is `proofs/p05-helm-cli/main.go`, and its chart is `proofs/p05-helm-cli/chart/`. The chart is `p05` 0.1.0. Its values are `replicas: 2` and `marker: chart`, and its schema requires `replicas` as an integer. It makes one Deployment named after the release, with `replicas` replicas of `registry.k8s.io/pause:3.10` (`imagePullPolicy: IfNotPresent`) and the pod template annotation `marker`.

The program takes `-helm PATH` and makes it absolute. Every helm command runs with no shell and in the folder `p05-helm-cli`, so `./chart` names the chart. Its environment is the program's own without the eleven variables of section 7: `HELM_KUBECONTEXT`, `HELM_KUBEAPISERVER`, `HELM_KUBETOKEN`, `HELM_KUBECAFILE`, `HELM_KUBEASUSER`, `HELM_KUBEASGROUPS`, `HELM_KUBEINSECURE_SKIP_TLS_VERIFY`, `HELM_KUBETLS_SERVER_NAME`, `HELM_NAMESPACE`, `HELM_DRIVER` and `HELM_DRIVER_SQL_CONNECTION_STRING`. None of them was set in these runs. The program also sets `HELM_CACHE_HOME`, `HELM_CONFIG_HOME` and `HELM_DATA_HOME` for every helm command, to folders in the run's temporary folder `$TMP/…`, so that Helm writes nothing in the user's home. These three are not among the eleven: the tool passes them through as the runner has them (section 7: Helm runs with the runner's own configuration). The program removes the temporary folder at the end.

For each helm command the program prints the command line, the exit code, and the standard output and standard error, each line indented and marked `out|` or `err|`. CL below stands for `--namespace p05 --kubeconfig F --kube-context CTX`. F is the lab's kubeconfig file and CTX that file's current context, `kind-rollproof-proof-p05`.

1. `lab.Up(ctx, "p05", cfg)` with one control-plane node and one worker. The program creates the namespace `p05` with the client, because the tool never passes `--create-namespace`. It starts the registry container `rollproof-proof-p05-charts` with `l.Registry`. PUSH below is its address on the runner, `127.0.0.1:PORT`.
2. The checks:
   1. `helm version --short`: exit 0 and a line that starts with `v`. VERSION is that line up to its `+`.
   2. The install. The program writes VALUES1, a file that holds `marker: one`, and runs `helm upgrade --install p05 ./chart CL --timeout 2m0s --values VALUES1`: exit 0. At once it reads the Deployment `p05`. Then `helm status p05 CL -o json`: `version` 1 and `info.status` `deployed`.
   3. The upgrade with reset: `helm upgrade p05 ./chart --reset-values CL --timeout 2m0s`: exit 0. At once it reads the Deployment. Then `helm status` gives `version` 2, `helm get values p05 CL -o json` prints `null` or `{}`, and the Deployment's annotation `marker` is `chart`. The contrast: `helm upgrade --install p05b ./chart CL --timeout 2m0s --values VALUES1`, then `helm upgrade p05b ./chart CL --timeout 2m0s` with no values and no `--reset-values`. Each must exit 0, and the program prints the annotation of the Deployment `p05b`.
   4. The rollback: `helm rollback p05 1 CL --timeout 2m0s`: exit 0. At once it reads the Deployment. Then `helm status` gives `version` 3, and `helm get manifest p05 CL --revision 1` and `--revision 3` print the same text.
   5. The pull: `helm package ./chart -d TMP` writes `TMP/p05-0.1.0.tgz`. Then `helm push TMP/p05-0.1.0.tgz oci://PUSH/charts --plain-http` and `helm pull oci://PUSH/charts/p05 --version 0.1.0 --destination DIR --plain-http`; DIR then holds `p05-0.1.0.tgz`. `helm show chart DIR/p05-0.1.0.tgz` prints the lines `name: p05` and `version: 0.1.0`. The tool does not pass `--plain-http`; the proof needs it for a registry without TLS.
   6. A failure: `helm upgrade p05 ./chart --reset-values --set replicas=two CL --timeout 2m0s` exits non-zero, and the program prints the code. `helm status` still gives `version` 3.
   7. The overrides. `helm status p05 CL -o json` with `HELM_NAMESPACE=elsewhere` added to the environment: does it find the release? The same command with `HELM_KUBEAPISERVER=https://127.0.0.1:1` added: its exit code and its error.
   8. The flags. In the help of `helm upgrade` and of `helm rollback`, for each of `--reset-values`, `--timeout`, `--install`, `--wait`, `--atomic` and `--rollback-on-failure`: is it listed as a flag, and is it named in the text? For each flag that `helm upgrade --help` does not list, the program runs `helm upgrade --FLAG --help` and prints whether Helm answers `unknown flag`.
3. `Down`.

The program prints `ok:` or `FAILED:` for each check of the brief and `info:` for what it only reads. Its last line is `p05: all checks passed for VERSION` only when no check failed, each of the eight checks ran to its end, and the run was not interrupted. Otherwise its last line gives the number of failed checks, and it exits 1.

**Three additions to the brief.** The plan's assumption names the install, the upgrade and the rollback, so the program reads the Deployment at once after each of the three, not only after the install. It prints the generation, the counts of the status, and whether the rollout is complete by the rule of `kubectl rollout status`: the new generation is observed, and every replica is updated and available. The brief made check 3's contrast an `info:` line; the program makes each of its two commands `FAILED:` when it does not exit 0, because the contrast is then not measured. Check 8 separates "listed as a flag" (a line of the Flags list that starts with the flag) from "named in the text" (the flag anywhere, as a whole flag), because the help of one flag can name another: Helm 3's help for `--atomic` names `--wait`. "Does not show" in the brief is taken as "not listed".

The commands, from `projects/rollproof/proofs/`:

```
go run ./p05-helm-cli -helm ../.tools/helm-3/helm
go run ./p05-helm-cli -helm ../.tools/helm-4/helm
```

The program ran three times on 2026-11-17, one cluster at a time: under Helm 3 from 02:10:44 to 02:12:02 UTC, under Helm 4 from 02:12:12 to 02:12:57 UTC, and a third run that tested an interrupt, from 02:13:44 to 02:14:29 UTC. For the third run, the program as committed was built as a binary in `$TMP/…` and run with `-helm ../.tools/helm-3/helm` from a script. The script sent SIGINT to the program's process alone once the line `info: check 3` appeared. Before and after each run, `nerdctl ps -a` listed no container and `nerdctl volume ls -q` listed 57 volumes. After the runs, `$HOME/.cache/helm`, `$HOME/.config/helm` and `$HOME/.local/share/helm` did not exist, as before them.

To explain two results, this proof read Helm's source with `curl -sS https://raw.githubusercontent.com/helm/helm/TAG/PATH` and no token: `pkg/cli/environment.go` and `cmd/helm/helm.go` at `v3.22.1` and `v4.3.2`, and `pkg/cmd/root.go` and `pkg/cmd/load_plugins.go` at `v4.3.2`.

## Result

Both runs passed every check. Their last lines:

```
p05: all checks passed for v3.22.1
p05: all checks passed for v4.3.2
```

| Check | Helm 3.22.1 | Helm 4.3.2 |
| --- | --- | --- |
| 1, version | exit 0; `v3.22.1+gd6c1a0e` | exit 0; `v4.3.2+g8346aaa` |
| 2, install | exit 0; revision 1, `deployed` | exit 0; revision 1, `deployed` |
| 2, ready when the install returned | no: readyReplicas 0, observedGeneration 0 of generation 1 | no: readyReplicas 0, observedGeneration 0 of generation 1 |
| 3, upgrade with `--reset-values` | exit 0; revision 2; `get values` prints `null`; annotation `chart` | exit 0; revision 2; `get values` prints `null`; annotation `chart` |
| 3, complete when the upgrade returned | no: observedGeneration 1 of generation 2 | no: observedGeneration 1 of generation 2 |
| 3, contrast: upgrade with no values and no `--reset-values` | both commands exit 0; `p05b` keeps `one` | both commands exit 0; `p05b` keeps `one` |
| 4, rollback to 1 | exit 0; revision 3; manifests of revisions 1 and 3 are the same text | exit 0; revision 3; manifests of revisions 1 and 3 are the same text |
| 4, complete when the rollback returned | no: replicas 3, updatedReplicas 2, availableReplicas 0 | no: replicas 3, updatedReplicas 2, availableReplicas 0 |
| 5, package, push, pull, show | each exits 0; DIR holds `p05-0.1.0.tgz` only; `name: p05`, `version: 0.1.0` | each exits 0; DIR holds `p05-0.1.0.tgz` only; `name: p05`, `version: 0.1.0` |
| 5, where push and pull write their `Pushed:` and `Pulled:` lines | standard error | standard output |
| 6, failure | exit 1; revision stays 3 | exit 1; revision stays 3 |
| How the revision was read | the integer field `version` of `helm status RELEASE CL -o json` | the integer field `version` of `helm status RELEASE CL -o json` |
| 7, `HELM_NAMESPACE=elsewhere` | exit 0, release found in `p05`: `--namespace` wins | exit 0, release found in `p05`: `--namespace` wins |
| 7, `HELM_KUBEAPISERVER=https://127.0.0.1:1` | exit 1, error names `127.0.0.1:1`: the variable wins over the kubeconfig file | exit 1, error names `127.0.0.1:1`: the variable wins over the kubeconfig file |
| 8, `helm upgrade --help` lists | `--reset-values`, `--timeout`, `--install` (`-i`), `--wait`, `--atomic` | `--reset-values`, `--timeout`, `--install` (`-i`), `--wait`, `--rollback-on-failure` |
| 8, flag not listed by `helm upgrade --help` | `--rollback-on-failure`: `helm upgrade --rollback-on-failure --help` exits 1, `Error: unknown flag: --rollback-on-failure` | `--atomic`: `helm upgrade --atomic --help` exits 0 and prints the help, with `Flag --atomic has been deprecated, use --rollback-on-failure instead` on standard error |
| 8, `helm rollback --help` lists | `--timeout`, `--wait`; none of the other four | `--timeout`, `--wait`; none of the other four |

Every `--timeout 2m0s` was accepted: each command that carried it exited 0, apart from check 6, which failed on the schema.

`Up` took two Create attempts in the Helm 3 run, which printed `lab: Create attempt 1 of 4 failed at kind's wait for a node's log line; trying again` once. It took one attempt in the Helm 4 run and one in the interrupt run.

The install's output, with the date taken out, was the same in both majors except for one line that Helm 4 adds:

```
Release "p05" does not exist. Installing it now.
NAME: p05
NAMESPACE: p05
STATUS: deployed
REVISION: 1
DESCRIPTION: Install complete     (Helm 4 only)
TEST SUITE: None
```

The output of the program's check lines of the Helm 4 run, from check 2 to check 4, with the indented output of each command left out:

```
info: run: helm upgrade --install p05 ./chart --namespace p05 --kubeconfig $TMP/rollproof-proof-p05-1429468741/kubeconfig --kube-context kind-rollproof-proof-p05 --timeout 2m0s --values $TMP/rollproof-proof-p05-helm-2677400543/values1.yaml
info: exit code 0
ok: helm upgrade --install p05 ./chart … --values VALUES1 exits 0 (want 0)
info: when the install returned, readyReplicas was 2: no; Deployment p05: generation 1, observedGeneration 0, replicas 0, updatedReplicas 0, readyReplicas 0, availableReplicas 0; complete: no
ok: after the install, helm status -o json gives version 1 and info.status deployed (want 1 and deployed)
ok: helm upgrade p05 ./chart --reset-values … exits 0 (want 0)
info: when the upgrade returned: Deployment p05: generation 2, observedGeneration 1, replicas 2, updatedReplicas 2, readyReplicas 0, availableReplicas 0; complete: no
ok: after the upgrade, helm status -o json gives version 2 (want 2), info.status deployed
ok: helm get values p05 -o json exits 0 and prints "null" (want 0, and null or {})
ok: the pod template annotation marker of Deployment p05 is "chart" (want chart)
ok: contrast: helm upgrade --install p05b ./chart … --values VALUES1 exits 0 (want 0)
ok: contrast: helm upgrade p05b ./chart … with no values exits 0 (want 0)
info: contrast: after an upgrade with no values and no --reset-values, the annotation marker of Deployment p05b is "one"
ok: helm rollback p05 1 … exits 0 (want 0)
info: when the rollback returned: Deployment p05: generation 3, observedGeneration 3, replicas 3, updatedReplicas 2, readyReplicas 0, availableReplicas 0; complete: no
ok: after the rollback, helm status -o json gives version 3 (want 3), info.status deployed
ok: helm get manifest --revision 1 and --revision 3 exit 0 and 0 and print the same text: yes
```

The Helm 3 run printed the same lines for these checks, apart from the temporary folders' names. From check 5 to the end, the two runs' lines that are not indented command output differ only in the registry's port and the temporary folders' names, the capital letter of check 7's error, the two flags of check 8, and the version in the last line.

The failure of check 6, on standard error, the same in both majors:

```
Error: UPGRADE FAILED: values don't meet the specifications of the schema(s) in the following chart(s):
p05:
- at '/replicas': got string, want integer
```

Check 7's second command, the first line of standard error:

```
Error: Kubernetes cluster unreachable: Get "https://127.0.0.1:1/version": dial tcp 127.0.0.1:1: connect: connection refused     (Helm 3)
Error: kubernetes cluster unreachable: Get "https://127.0.0.1:1/version": dial tcp 127.0.0.1:1: connect: connection refused     (Helm 4)
```

Check 5, the push and the pull in the Helm 4 run (Helm 3 wrote the same two pairs of lines to standard error, with its own port and digest):

```
  out| Pushed: 127.0.0.1:33435/charts/p05:0.1.0
  out| Digest: sha256:3263031c85d42687572ae81ba4debc9a2cd21b82e0a8082a693dfa86055bb666
  out| Pulled: 127.0.0.1:33435/charts/p05:0.1.0
  out| Digest: sha256:3263031c85d42687572ae81ba4debc9a2cd21b82e0a8082a693dfa86055bb666
```

Check 8, the lines of the Flags lists for the flags this proof looked for, with the spaces between flag and description narrowed and parts of the longer descriptions replaced by `…`:

```
Helm 3, helm upgrade --help:
      --atomic                if set, upgrade process rolls back changes made in case of failed upgrade. …
  -i, --install               if a release by this name doesn't already exist, run an install
      --reset-values          when upgrading, reset the values to the ones built into the chart
      --timeout duration      time to wait for any individual Kubernetes operation (like Jobs for hooks) (default 5m0s)
      --wait                  if set, will wait until all Pods, PVCs, Services, and minimum number of Pods … are in a ready state …
Helm 4, helm upgrade --help:
  -i, --install                    if a release by this name doesn't already exist, run an install
      --reset-values               when upgrading, reset the values to the ones built into the chart
      --rollback-on-failure        if set, Helm will rollback the upgrade to previous success release upon failure. …
      --timeout duration           time to wait for any individual Kubernetes operation (like Jobs for hooks) (default 5m0s)
      --wait WaitStrategy[=watcher]   wait until resources are ready (up to --timeout). … Default when flag is omitted: 'hookOnly'. (default hookOnly)
```

The JSON of `helm status -o json` has the fields `name`, `namespace`, `version`, `config`, `manifest` and `info` (with `status`, `description`, `first_deployed` and `last_deployed`) in both majors. Helm 3 adds `info.deleted`. Helm 4 adds `apply_method` and `info.resources`, which holds the release's objects. `apply_method` was `ssa` for revisions 1, 2 and 3: Helm 4 applied the install, the upgrade and the rollback server-side. `info.description` was `Install complete`, `Upgrade complete` and `Rollback to 1` in both majors.

**Why the overrides go as they do.** In both majors `pkg/cli/environment.go` reads each variable in `New()` as the first value of a setting, and `AddFlags` binds the matching flag to the same setting, with that value as the flag's default:

```go
fs.StringVarP(&s.namespace, "namespace", "n", s.namespace, "namespace scope for this request")
fs.StringVar(&s.KubeAPIServer, "kube-apiserver", s.KubeAPIServer, "the address and the port for the Kubernetes API server")
```

A flag given on the command line therefore wins over its variable, and a variable whose flag is not given is used. The tool gives `--namespace`, `--kubeconfig` and `--kube-context`. It gives none of `--kube-apiserver`, `--kube-token`, `--kube-ca-file`, `--kube-as-user`, `--kube-as-group`, `--kube-insecure-skip-tls-verify` and `--kube-tls-server-name`, so their variables would be used.

**Why every failure exits 1.** In Helm 3's `cmd/helm/helm.go`, `main` exits 1 for every error that the command returns, except a plugin's error, which carries the plugin's own exit code. Helm 4's `main` does the same with `CommandError`. Only the code that runs a plugin returns a `CommandError` (`pkg/cmd/load_plugins.go`).

**The interrupt run.** It passed checks 1 and 2 and the first command of check 3. SIGINT came while `helm status` ran: that command was killed (`did not exit by itself: signal: killed`), each command after it did not start (`context canceled`), and the program stopped after check 3. `Down` returned no error. The output ended:

```
FAILED: check 4 did not run to its end
FAILED: check 5 did not run to its end
FAILED: check 6 did not run to its end
FAILED: check 7 did not run to its end
FAILED: check 8 did not run to its end
FAILED: the run was interrupted
p05: 12 checks failed for v3.22.1
```

It exited 1, and no container, volume or temporary folder was left.

## Decision

The command lines of section 7 stand, and they are the same for both majors. Task 24 uses these, with CHART an archive that the pull fetched or a chart path, and D `limits.timeout` as Go prints a `time.Duration`:

```
helm pull CHART --version V --destination DIR
helm show chart ARCHIVE
helm upgrade --install RELEASE CHART --namespace NS --kubeconfig FILE --kube-context CTX --timeout D --values FILE …
helm upgrade RELEASE CHART --reset-values --namespace NS --kubeconfig FILE --kube-context CTX --timeout D --values FILE …
helm rollback RELEASE REVISION --namespace NS --kubeconfig FILE --kube-context CTX --timeout D
helm status RELEASE --namespace NS --kubeconfig FILE --kube-context CTX -o json
```

- The revision is the integer field `version` of `helm status … -o json`. On a new cluster the install, the deploy and the rollback give 1, 2 and 3 in both majors. The tool reads `version` after the install and names it in the rollback. Helm 4 adds fields to that JSON, so the tool ignores the fields it does not read.
- Helm 3 writes the pull's `Pulled:` line to standard error, and Helm 4 to standard output. So the tool does not read the chart's name and version from the pull's output. It reads them from the lines `name:` and `version:` of `helm show chart` on the archive. After the pull, the proof's DIR held one file, the archive `p05-0.1.0.tgz`.
- The tool judges a command by a zero or non-zero exit, never by the code. Every failure measured here exited 1 in both majors.
- The eleven variables stay out of every command's environment. Check 7 shows in both majors that a variable whose flag the tool does not give (`HELM_KUBEAPISERVER`) sends Helm to another server, and that a variable whose flag the tool gives (`HELM_NAMESPACE`) loses to the flag. A caller's script can run Helm without those flags, so section 7 takes out both kinds.
- The tool passes no `--wait`, `--atomic` or `--rollback-on-failure`, as section 7 says. In both majors the install, the upgrade and the rollback each returned before the Deployment's rollout was complete. Helm 4 still accepts `--atomic` as a deprecated name, and Helm 3 does not know `--rollback-on-failure`. This does not affect a tool that passes neither.
- No difference between the majors changes a command line. The one difference that changes how the tool reads output is the stream of the pull's lines, above. These differences change nothing that the tool reads: Helm 4 applies objects server-side (`apply_method: ssa` for the install, the upgrade and the rollback), adds a `DESCRIPTION:` line to the output of an install or an upgrade, and writes `kubernetes cluster unreachable` where Helm 3 writes `Kubernetes cluster unreachable`. A server-side apply can conflict with another writer of a field (section 14, point 5). Nothing else wrote to the Deployment here, so no conflict came; the fixtures look for one.
- The proof's push and pull used `--plain-http`, for a registry without TLS. The tool does not pass it. A pull from a registry with TLS was not run here.

Plan assumed: "Under Helm 3.22.1 and under Helm 4.3.2 the same command lines work: `helm pull CHART --version V --destination DIR`; `helm upgrade --install RELEASE CHART --namespace NS --kubeconfig FILE --kube-context CTX --timeout D --values FILE`; `helm upgrade RELEASE CHART --reset-values …`; `helm rollback RELEASE REVISION …`; `helm status RELEASE -o json`, whose `version` is the revision; `helm show chart ARCHIVE`, which gives the chart's name and version. They give revisions 1, 2 and 3. Each returns before the rollout is complete. Any failure exits non-zero. A duration is accepted as Go prints it (2m0s)."
Holds: yes
