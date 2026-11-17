# Point 5: Helm's command lines in both majors

## Question

Point 5 of section 14: a job on `ubuntu-latest` changes Helm major when that label moves, so the first release is tested with Helm 3.22 and Helm 4.3. Do the command lines of section 7 ("The Helm driver", "The candidate's values", "The cluster Helm acts on") exist in both majors and mean the same? This record also gives the revisions that an install, an upgrade and a rollback give, how the revision is read, the exit code of a failure, and which environment variables override the command line.

## Machine and versions

- System: the developer's machine, Linux 7.3.3, amd64.
- Engine: nerdctl 2.4.2, rootless (the p08 and p12 records give its parts). Docker is absent.
- Go 1.27.3.
- kind: `sigs.k8s.io/kind` v0.33.0; `k8s.io/client-go` v0.37.2; `sigs.k8s.io/yaml` v1.6.0, which parses the output of `helm show chart`.
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
   5. The pull: `helm package ./chart -d TMP` writes `TMP/p05-0.1.0.tgz`. Then `helm push TMP/p05-0.1.0.tgz oci://PUSH/charts --plain-http` and `helm pull oci://PUSH/charts/p05 --version 0.1.0 --destination DIR --plain-http`; DIR then holds `p05-0.1.0.tgz`. The program parses what `helm show chart DIR/p05-0.1.0.tgz` prints as YAML, with `sigs.k8s.io/yaml`, and its top-level keys `name` and `version` are `p05` and `0.1.0`. The tool does not pass `--plain-http`; the proof needs it for a registry without TLS. Then the program writes into `TMP/dependency` a `Chart.yaml` for a chart `p05dep` 1.4.0 with one dependency (`common` 2.31.0, from `oci://example.invalid/charts`) and one maintainer. `helm show chart TMP/dependency` reads `Chart.yaml` alone, so the dependency need not exist. The top-level keys of its output must be `p05dep` and `1.4.0`, not the dependency's version. The program also prints the first line that starts with `version:` once its leading spaces are taken off.
   6. A failure: `helm upgrade p05 ./chart --reset-values --set replicas=two CL --timeout 2m0s` exits non-zero, and the program prints the code. `helm status` still gives `version` 3.
   7. The overrides. `helm status p05 CL -o json` with `HELM_NAMESPACE=elsewhere` added to the environment: does it find the release? The program reads "`--namespace` wins" when the command exits 0 with the release in `p05`, and "`HELM_NAMESPACE` wins" when it exits non-zero and its standard error holds `release: not found`, Helm's error for a missing release (`pkg/storage/driver/driver.go` in both majors). Any other outcome is `FAILED:`. The same command with `HELM_KUBEAPISERVER=https://127.0.0.1:1` added: its exit code and its error.
   8. The flags. In the help of `helm upgrade` and of `helm rollback`, for each of `--reset-values`, `--timeout`, `--install`, `--wait`, `--atomic` and `--rollback-on-failure`: is it listed as a flag, and is it named in the text? For each flag that `helm upgrade --help` does not list, the program runs `helm upgrade --FLAG --help` and prints whether Helm answers `unknown flag`.
3. `Down`.

The program prints `ok:` or `FAILED:` for each check of the brief and `info:` for what it only reads. Its last line is `p05: all checks passed for VERSION` only when no check failed, each of the eight checks ran to its end, and the run was not interrupted. Otherwise its last line gives the number of failed checks, and it exits 1.

**Additions to the brief.** The plan's assumption names the install, the upgrade and the rollback, so the program reads the Deployment at once after each of the three, not only after the install. It prints the generation and the counts of the status. It also prints "complete", which it takes to hold when four conditions hold: observedGeneration is at least generation, updatedReplicas equals `spec.replicas` (1 when that is unset), replicas equals updatedReplicas, and availableReplicas equals updatedReplicas. This is close to the rule of `kubectl rollout status` that the p10 record gives, but not the same. The helper requires each pair to be equal. kubectl calls a rollout not complete only when updatedReplicas is less than `spec.replicas`, replicas is more than updatedReplicas, or availableReplicas is less than updatedReplicas. kubectl also skips the replica test when `spec.replicas` is unset, and it fails a rollout whose progress deadline was exceeded. No result below depends on the difference. Each read that the helper calls not complete also fails kubectl's rule 3 or rule 6. The one read that it calls complete, in run 4, also passes kubectl's rules 3 and 5 to 7. The brief made check 3's contrast an `info:` line; the program makes each of its two commands `FAILED:` when it does not exit 0, because the contrast is then not measured. Check 5's chart with a dependency, and the rule by which check 7 reads "`HELM_NAMESPACE` wins", came from the review of the first version of this record. Check 8 separates "listed as a flag" (a line of the Flags list that starts with the flag) from "named in the text" (the flag anywhere, as a whole flag), because the help of one flag can name another: Helm 3's help for `--atomic` names `--wait`. "Does not show" in the brief is taken as "not listed".

The commands, from `projects/rollproof/proofs/`:

```
go run ./p05-helm-cli -helm ../.tools/helm-3/helm
go run ./p05-helm-cli -helm ../.tools/helm-4/helm
```

The program ran six times on 2026-11-17, one cluster at a time, all times UTC:

| Run | Program | Helm | Time | Last line |
| --- | --- | --- | --- | --- |
| 1 | first version (commit `86e1a53`) | 3.22.1 | 02:10:44 to 02:12:02 | `p05: all checks passed for v3.22.1` |
| 2 | first version | 4.3.2 | 02:12:12 to 02:12:57 | `p05: all checks passed for v4.3.2` |
| 3 | first version, interrupted | 3.22.1 | 02:13:44 to 02:14:29 | `p05: 12 checks failed for v3.22.1` |
| 4 | as committed | 3.22.1 | 02:32:53 to 02:33:38 | `p05: all checks passed for v3.22.1` |
| 5 | as committed | 4.3.2 | 02:33:44 to 02:34:24 | `p05: all checks passed for v4.3.2` |
| 6 | as committed, interrupted | 3.22.1 | 02:34:31 to 02:35:14 | `p05: 14 checks failed for v3.22.1` |

The first version read `helm show chart` by trimmed lines and had no chart with a dependency. Its check 7 read every failed `helm status` as "`HELM_NAMESPACE` wins". The code of checks 1 to 4, 6 and 8 is the same in both versions. Runs 4 to 6 are the program as committed. For runs 3 and 6, the program was built as a binary in `$TMP/…` and run with `-helm ../.tools/helm-3/helm` from a script, which sent SIGINT to the program's process alone once the line `info: check 3` appeared. Before and after each run, `nerdctl ps -a` listed no container and `nerdctl volume ls -q` listed 57 volumes. After the runs, `$HOME/.cache/helm`, `$HOME/.config/helm` and `$HOME/.local/share/helm` did not exist, as before them.

To explain results, this proof read Helm's source with `curl -sS https://raw.githubusercontent.com/helm/helm/TAG/PATH` and no token: `pkg/cli/environment.go`, `cmd/helm/helm.go` and `pkg/storage/driver/driver.go` at `v3.22.1` and `v4.3.2`, and `pkg/cmd/root.go` and `pkg/cmd/load_plugins.go` at `v4.3.2`.

## Result

Runs 4 and 5, the program as committed, passed every check, as runs 1 and 2 had. Their last lines:

```
p05: all checks passed for v3.22.1
p05: all checks passed for v4.3.2
```

The table gives runs 4 (Helm 3) and 5 (Helm 4). Where run 1 or run 2 differs, the table says so.

| Check | Helm 3.22.1 | Helm 4.3.2 |
| --- | --- | --- |
| 1, version | exit 0; `v3.22.1+gd6c1a0e` | exit 0; `v4.3.2+g8346aaa` |
| 2, install | exit 0; revision 1, `deployed` | exit 0; revision 1, `deployed` |
| 2, ready when the install returned | no: readyReplicas 0, observedGeneration 0 of generation 1 | no: readyReplicas 0, observedGeneration 0 of generation 1 |
| 3, upgrade with `--reset-values` | exit 0; revision 2; `get values` prints `null`; annotation `chart` | exit 0; revision 2; `get values` prints `null`; annotation `chart` |
| 3, complete when the upgrade returned | no: observedGeneration 1 of generation 2 | no: observedGeneration 1 of generation 2 |
| 3, contrast: upgrade with no values and no `--reset-values` | both commands exit 0; `p05b` keeps `one` | both commands exit 0; `p05b` keeps `one` |
| 4, rollback to 1 | exit 0; revision 3; manifests of revisions 1 and 3 are the same text | exit 0; revision 3; manifests of revisions 1 and 3 are the same text |
| 4, complete when the rollback returned | run 4: yes, with replicas 2, updatedReplicas 2 and availableReplicas 2 (see below); run 1: no, with replicas 3, updatedReplicas 2 and availableReplicas 0 | no in runs 2 and 5: replicas 3, updatedReplicas 2, availableReplicas 0 |
| 5, package, push, pull, show | each exits 0; DIR holds `p05-0.1.0.tgz` only; top-level `name` `p05` and `version` `0.1.0` | each exits 0; DIR holds `p05-0.1.0.tgz` only; top-level `name` `p05` and `version` `0.1.0` |
| 5, `helm show chart` on the chart with one dependency | exit 0; top-level `name` `p05dep` and `version` `1.4.0`; the first trimmed `version:` line is the dependency's, `version: 2.31.0` | exit 0; top-level `name` `p05dep` and `version` `1.4.0`; the first trimmed `version:` line is the dependency's, `version: 2.31.0` |
| 5, where push and pull write their `Pushed:` and `Pulled:` lines | standard error | standard output |
| 6, failure | exit 1; revision stays 3 | exit 1; revision stays 3 |
| How the revision was read | the integer field `version` of `helm status RELEASE CL -o json` | the integer field `version` of `helm status RELEASE CL -o json` |
| 7, `HELM_NAMESPACE=elsewhere` | exit 0, release found in `p05`: `--namespace` wins | exit 0, release found in `p05`: `--namespace` wins |
| 7, `HELM_KUBEAPISERVER=https://127.0.0.1:1` | exit 1, error names `127.0.0.1:1`: the variable wins over the kubeconfig file | exit 1, error names `127.0.0.1:1`: the variable wins over the kubeconfig file |
| 8, `helm upgrade --help` lists | `--reset-values`, `--timeout`, `--install` (`-i`), `--wait`, `--atomic` | `--reset-values`, `--timeout`, `--install` (`-i`), `--wait`, `--rollback-on-failure` |
| 8, flag not listed by `helm upgrade --help` | `--rollback-on-failure`: `helm upgrade --rollback-on-failure --help` exits 1, `Error: unknown flag: --rollback-on-failure` | `--atomic`: `helm upgrade --atomic --help` exits 0 and prints the help, with `Flag --atomic has been deprecated, use --rollback-on-failure instead` on standard error |
| 8, `helm rollback --help` lists | `--timeout`, `--wait`; none of the other four | `--timeout`, `--wait`; none of the other four |

Every `--timeout 2m0s` was accepted: each command that carried it exited 0, apart from check 6, which failed on the schema.

`Up` took two Create attempts in run 1, which printed `lab: Create attempt 1 of 4 failed at kind's wait for a node's log line; trying again` once. It took one attempt in each of runs 2 to 6.

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

The output of the program's check lines of run 5 (Helm 4), from check 2 to check 4, with the indented output of each command left out:

```
info: run: helm upgrade --install p05 ./chart --namespace p05 --kubeconfig $TMP/rollproof-proof-p05-4116348148/kubeconfig --kube-context kind-rollproof-proof-p05 --timeout 2m0s --values $TMP/rollproof-proof-p05-helm-1664339581/values1.yaml
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

Runs 1 and 2 printed the same lines for these checks, apart from the temporary folders' names. So did run 4, except for the line after the rollback:

```
info: when the rollback returned: Deployment p05: generation 3, observedGeneration 3, replicas 2, updatedReplicas 2, readyReplicas 2, availableReplicas 2; complete: yes
```

In run 4, Helm 3's rollback found the rollout already complete when it returned. The program did not look at the ReplicaSets, so the cause is inferred from the counts. Two pods were available at the moment Helm returned, under a template that Helm had changed back in the same command, so they must have existed before the rollback. A Deployment of 2 replicas with the default strategy has maxUnavailable 0 (25% of 2, rounded down). The deploy therefore kept revision 1's two pods while revision 2's new pod was not yet ready. In run 4 those two pods had become ready before the rollback, and the rollback, which goes back to revision 1's template, needed no new pod. In runs 1, 2 and 5 they were not yet ready at the rollback (readyReplicas 0). This read cannot show whether Helm waited. Run 1 shows Helm 3's rollback returning before its rollout was complete, with the same code for check 4.

From check 5 to the end, the lines of runs 4 and 5 that are not indented command output differ only in the registry's port and the temporary folders' names, the capital letter of check 7's error, the two flags of check 8, and the version in the last line.

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

Check 5, the push and the pull in run 5 (Helm 4). Helm 3 wrote the same two pairs of lines to standard error, with its own port and digest:

```
  out| Pushed: 127.0.0.1:34101/charts/p05:0.1.0
  out| Digest: sha256:36dbef5d518254327423a7991409d9b5187b331a2edc80e5e3745a3471c68f32
  out| Pulled: 127.0.0.1:34101/charts/p05:0.1.0
  out| Digest: sha256:36dbef5d518254327423a7991409d9b5187b331a2edc80e5e3745a3471c68f32
```

Check 5, `helm show chart` on the chart with one dependency. The output was the same in both majors: the keys are sorted, so the dependency's and the maintainer's nested keys come before the chart's own `name` and `version`. Then the two lines of run 5 that read it:

```
  out| apiVersion: v2
  out| dependencies:
  out| - name: common
  out|   repository: oci://example.invalid/charts
  out|   version: 2.31.0
  out| maintainers:
  out| - name: someone
  out| name: p05dep
  out| version: 1.4.0
ok: helm show chart TMP/dependency (one dependency, one maintainer) exits 0; its top-level name is "p05dep" and version "1.4.0" (want p05dep and 1.4.0, not the dependency's 2.31.0; YAML error: <nil>)
info: helm show chart TMP/dependency: the first line that starts with version: once its spaces are taken off is "version: 2.31.0"
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

The JSON of `helm status -o json` has the fields `name`, `namespace`, `version`, `config`, `manifest` and `info` (with `status`, `description`, `first_deployed` and `last_deployed`) in both majors. Helm 3 adds `info.deleted`. Helm 4 adds `apply_method`, `info.resources`, which holds the release's objects, and after the rollback `info.rollback_revision`, which was 1. `apply_method` was `ssa` for revisions 1, 2 and 3: Helm 4 applied the install, the upgrade and the rollback server-side. `info.description` was `Install complete`, `Upgrade complete` and `Rollback to 1` in both majors.

**Why the overrides go as they do.** In both majors `pkg/cli/environment.go` reads each variable in `New()` as the first value of a setting, and `AddFlags` binds the matching flag to the same setting, with that value as the flag's default:

```go
fs.StringVarP(&s.namespace, "namespace", "n", s.namespace, "namespace scope for this request")
fs.StringVar(&s.KubeAPIServer, "kube-apiserver", s.KubeAPIServer, "the address and the port for the Kubernetes API server")
```

A flag given on the command line therefore wins over its variable, and a variable whose flag is not given is used. The tool gives `--namespace`, `--kubeconfig` and `--kube-context`. It gives none of `--kube-apiserver`, `--kube-token`, `--kube-ca-file`, `--kube-as-user`, `--kube-as-group`, `--kube-insecure-skip-tls-verify` and `--kube-tls-server-name`, so their variables would be used.

**Why every failure exits 1.** In Helm 3's `cmd/helm/helm.go`, `main` exits 1 for every error that the command returns, except a plugin's error, which carries the plugin's own exit code. Helm 4's `main` does the same with `CommandError`. Only the code that runs a plugin returns a `CommandError` (`pkg/cmd/load_plugins.go`).

**The interrupt runs.** Run 6 passed checks 1 and 2. SIGINT came while the first command of check 3, the upgrade, ran. That command was killed (`did not exit by itself: signal: killed`), each command after it did not start (`context canceled`), and the program stopped after check 3. `Down` returned no error. The output ended:

```
FAILED: check 4 did not run to its end
FAILED: check 5 did not run to its end
FAILED: check 6 did not run to its end
FAILED: check 7 did not run to its end
FAILED: check 8 did not run to its end
FAILED: the run was interrupted
p05: 14 checks failed for v3.22.1
```

Run 3 was the same, except that SIGINT came one command later, during the first `helm status` of check 3, so its last line was `p05: 12 checks failed for v3.22.1`. Both runs exited 1 and left no container, volume or temporary folder.

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
- Helm 3 writes the pull's `Pulled:` line to standard error, and Helm 4 to standard output. So the tool does not read the chart's name and version from the pull's output. It reads them from the top-level keys `name` and `version` of what `helm show chart` prints for the archive, by parsing that output as YAML, as the program does with `sigs.k8s.io/yaml`. It must not read the first line that holds `name:` or `version:`. The keys of a chart's `dependencies` and `maintainers` entries carry nested `name` and `version` keys, and both majors print them first: for the chart with one dependency, the first trimmed `version:` line was the dependency's `2.31.0`, not the chart's `1.4.0`. After the pull, the proof's DIR held one file, the archive `p05-0.1.0.tgz`.
- The tool judges a command by a zero or non-zero exit, never by the code. Every failure measured here exited 1 in both majors.
- The eleven variables stay out of every command's environment. Check 7 shows in both majors that a variable whose flag the tool does not give (`HELM_KUBEAPISERVER`) sends Helm to another server, and that a variable whose flag the tool gives (`HELM_NAMESPACE`) loses to the flag. A caller's script can run Helm without those flags, so section 7 takes out both kinds.
- The tool passes no `--wait`, `--atomic` or `--rollback-on-failure`, as section 7 says. In both majors the install, the upgrade and the rollback each returned before the Deployment's rollout was complete. The one exception was run 4's rollback, whose rollout was already complete when Helm 3 returned, because the pods it needed had never been replaced (see the Result). Helm 4 still accepts `--atomic` as a deprecated name, and Helm 3 does not know `--rollback-on-failure`. This does not affect a tool that passes neither.
- No difference between the majors changes a command line. The one difference that changes how the tool reads output is the stream of the pull's lines, above. These differences change nothing that the tool reads: Helm 4 applies objects server-side (`apply_method: ssa` for the install, the upgrade and the rollback), adds a `DESCRIPTION:` line to the output of an install or an upgrade, and writes `kubernetes cluster unreachable` where Helm 3 writes `Kubernetes cluster unreachable`. A server-side apply can conflict with another writer of a field (section 14, point 5). Nothing else wrote to the Deployment here, so no conflict came; the fixtures look for one.
- These forms that the Decision relies on were read and not run here:
  - A pull from a registry with TLS. The proof's push and pull used `--plain-http`, for a registry without TLS, and the tool does not pass it.
  - A pulled archive as CHART for the install and the deploy. Every `upgrade` here named the folder `./chart`. Both majors' help for `helm upgrade` says that a packaged chart is accepted.
  - A repository alias (`ALIAS/CHART`) for the pull. Only `oci://` was pulled.
  - `--reset-values` together with `--values FILE` in one upgrade. No passing command combined them. Check 6 combined `--reset-values` with `--set`, and the value it set reached the schema.

Plan assumed: "Under Helm 3.22.1 and under Helm 4.3.2 the same command lines work: `helm pull CHART --version V --destination DIR`; `helm upgrade --install RELEASE CHART --namespace NS --kubeconfig FILE --kube-context CTX --timeout D --values FILE`; `helm upgrade RELEASE CHART --reset-values …`; `helm rollback RELEASE REVISION …`; `helm status RELEASE -o json`, whose `version` is the revision; `helm show chart ARCHIVE`, which gives the chart's name and version. They give revisions 1, 2 and 3. Each returns before the rollout is complete. Any failure exits non-zero. A duration is accepted as Go prints it (2m0s)."
Holds: yes
