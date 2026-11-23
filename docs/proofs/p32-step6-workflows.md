# Point 32: the first runs of the action's test, the mirror job and the suite on GitHub's hosted runners

## Question

Do the three workflows of step 6, `rollproof-action.yml`, `rollproof-mirror.yml` and `rollproof-e2e.yml`, pass on GitHub's hosted runners, and what does each show of the beliefs of the plan for step 6 (`docs/plans/2026-11-22-step6/09-hosted-runs.md`, Step 5; the pairs at the end of this record)?

## Machine and versions

Each job ran on a machine of its own. Each log begins with `Current runner version: '2.338.2'` and the lines of the image; the provisioner's lines (`Hosted Compute Agent`, `Version: 20261111.635`) are the same in every job.

| Job | Run | Image, as the log names it | Region, cloud |
| --- | --- | --- | --- |
| `action` | 45076071694 | `Ubuntu`, `24.04.5`, `LTS`, `Image: ubuntu-24.04`, `Version: 20261116.376.1` | `centralus`, `Azure` |
| `mirror` (first) | 45076080467 | the same | `westus`, `Azure` |
| `mirror` (second) | 45080222875 | the same | `westus3`, `Azure` |
| `cluster` | 45081200073 | the same | `northcentralus`, `Azure` |
| `helm-3` | 45081200073 | the same | `centralus`, `Azure` |
| `helm-4` | 45081200073 | `Ubuntu`, `26.04.1`, `LTS`, `Image: ubuntu-26.04`, `Version: 20261116.88.1` | `us-west-2`, `AWS` |

- Go 1.27.4 in every job (`go version go1.27.4 linux/amd64`). On the `ubuntu-24.04` image `actions/setup-go` found it in the runner's tool cache (`Found in cache @ /opt/hostedtoolcache/go/1.27.4/x64`) and restored the module and build cache (`Cache hit for: setup-go-Linux-x64-ubuntu24-go-1.27.4-4c9482d6…`, about 265 MB). On the `ubuntu-26.04` image it downloaded Go (`Acquiring 1.27.4 from https://github.com/actions/go-versions/releases/download/1.27.4-42945153609/go-1.27.4-linux-x64.tar.gz`, 2026-11-22T23:22:10.4907391Z) and found no module cache (`Cache is not found`, 23:22:14.3434804).
- Helm and kubectl, as `go run ./hack/fetchhelm` printed them in both mirror jobs and both Helm jobs: `helm 3.22.1: fetched to .tools/helm-3/helm`, `helm 4.3.2: fetched to .tools/helm-4/helm`, `kubectl 1.37.0: fetched to .tools/kubectl`. The `action` job does not run `fetchhelm`; the tool there used a Helm found on the runner's PATH (`internal/deploy/helm.go`, line 26, `exec.LookPath("helm")`, read), whose version no log shows.
- kind: the tool links `sigs.k8s.io/kind` v0.33.0 (`go.mod`, read).
- The node image: every run of the tool printed `on Kubernetes v1.37.0 in kind`, except the two runs of `TestFirstFixtureOnTheOldestNodeImage`, which printed `on Kubernetes v1.34.11 in kind` (00:04:37.1382378 in `helm-3`, 00:01:18.2999570 in `helm-4`).
- Docker. No log of these jobs shows Docker's version. For the `ubuntu-24.04` image, version 20261116.376.1, the image's software list gives Docker client and server 28.0.4: `p24-hosted-runner` read it at commit `0e63d4f9d2c29043b4cf1d2f40d1812a7cde1369` of `actions/runner-images`, and on 2026-11-22 the list at the tag `ubuntu24/20261116.376` gave the same `Docker Client 28.0.4` and `Docker Server 28.0.4` (read). For the `ubuntu-26.04` image, version 20261116.88.1, the list at the tag that the log names (`ubuntu26/20261116.88`) could not be read on 2026-11-22: the raw file, the file's page and the release's page each answered 404. The list on `main` of `actions/runner-images` (commit `8519dd2664f9f1a290aecb56dba114b2ad8193d1`, "Updating readme file for ubuntu26 version 20261116.202.1") gives `Docker Client 29.4.2` and `Docker Server 29.4.2`, but it describes image version 20261116.202.1, not the version that ran (read). So Docker's version in the `helm-4` job is not shown. No log of these jobs shows whether Docker runs as root either: on the `ubuntu-24.04` image the Docker run of `p08-kind-library` found no `rootless` entry in `docker info` (`p08-kind-library`, read; `p24-hosted-runner` cites it); on `ubuntu-26.04` it is not shown.
- The machines' size: no log of these jobs shows a CPU count or the memory.

## Method

The workflow files are those of the branch `rollproof-step6-design` at the commit that each job checked out, as its checkout's fetch line shows: `d4bd57d3f3a2615df340bf9fded7e55435b46ac2` for the `action` job and the first `mirror` job, and `f0708c60361d3e084ed27dae8c415347f0cfdc36` for the second `mirror` job and the three jobs of the suite. The one commit between them, `f0708c6`, changes only `e2e/harness_test.go`, `e2e/mirror_helpers_test.go` and `e2e/runenv_test.go`; the three workflow files are the same at both commits (`git show --stat f0708c6`, read).

Each run was started once, by hand, with one of these commands (each one command with no shell feature), from the repository's root:

```
gh workflow run rollproof-action.yml --ref rollproof-step6-design
gh workflow run rollproof-mirror.yml --ref rollproof-step6-design
gh workflow run rollproof-e2e.yml --ref rollproof-step6-design
```

The second command ran twice: first at `d4bd57d`, and again at `f0708c6` after Task 15 (below). Each run's log was saved with `gh run view ID --log`, and its jobs' lists of steps as `gh` printed them; the logs are not in this repository.

| Run | Workflow | Commit | Job, ID | First and last log line | Time | Billed (rounded up) |
| --- | --- | --- | --- | --- | --- | --- |
| 45076071694 | `rollproof-action.yml` | `d4bd57d` | `action`, 137373462177 | 22:09:08.5257126 to 22:18:16.7688681 | 548.2 s, 9 min 8.2 s | 10 min |
| 45076080467 | `rollproof-mirror.yml` | `d4bd57d` | `mirror`, 137373488508 | 22:09:13.8003516 to 22:14:29.7969701 | 316.0 s, 5 min 16.0 s | 6 min |
| 45080222875 | `rollproof-mirror.yml` | `f0708c6` | `mirror`, 137386472314 | 22:53:38.6695213 to 23:03:48.2263687 | 609.6 s, 10 min 9.6 s | 11 min |
| 45081200073 | `rollproof-e2e.yml` | `f0708c6` | `cluster`, 137389536957 | 23:04:24.5482095 to 23:21:58.9287628 | 1,054.4 s, 17 min 34.4 s | 18 min |
| 45081200073 | `rollproof-e2e.yml` | `f0708c6` | `helm-3`, 137394721202 | 23:22:04.1591825 to 00:38:04.0714969 | 4,559.9 s, 75 min 59.9 s | 76 min |
| 45081200073 | `rollproof-e2e.yml` | `f0708c6` | `helm-4`, 137394721216 | 23:22:04.3398380 to 00:32:30.6640857 | 4,226.3 s, 70 min 26.3 s | 71 min |

All times are of 2026-11-22, and 2026-11-23 after midnight, in UTC, as the logs stamp them. The billed minutes are each job's time rounded up to a whole minute, as GitHub bills a job (`p24-hosted-runner`, read); GitHub's own bill was not read. A job's log span can be a few seconds shorter than its own time in the jobs' lists (`startedAt` to `completedAt`). By those times every job rounds to the same minutes but `helm-3`, which took 4,563 s, 77 billed minutes; so the suite comes to 166 billed minutes and the four runs to 193.

The jobs' steps are those of the three files. In short: the `action` job builds the tool's image and pushes it to a registry on `127.0.0.1:5000` in the job, builds the fixture's two images, and uses the action from its folder three times (a pass, a fail under `continue-on-error`, and a run cut short by `timeout-minutes: 2` under `continue-on-error`), each followed by a step that runs `hack/actioncheck` on the step's outputs, its `result.json` and its job summary in the runner's masked copy; `actioncheck` prints nothing and exits 0 when every check holds (`hack/actioncheck/main.go`, read). The `mirror` job runs `go test -tags mirror -p 1 ./e2e/ -run TestMirror -v -timeout 60m`. The suite's `cluster` job runs the tests under the build tag `cluster` of `./internal/cluster/`, `./internal/images/`, `./internal/probe/`, `./internal/watch/` and `./hack/action/`; `helm-3` and `helm-4` each run `go test -tags e2e -p 1 -count=1 ./e2e/ -v -timeout 120m` with `ROLLPROOF_E2E_HELM` set to `3` or `4`.

## Result

**Outcomes.** The `action` job passed. The first `mirror` job failed. The second `mirror` job passed. The suite's three jobs passed. By the log times above, the suite's run took 18 + 76 + 71 = 165 billed minutes, the action's test 10, and the two mirror runs 6 and 11; the four runs together 192 (166 and 193 by the jobs' own times, above).

### The action's test (run 45076071694)

The three steps that call the action. In each, the script's stop line stands twice, its token masked; after the engine's pull and the line that names the tool, the tool's own stop line stands twice with a token of 26 characters in plain text; at the end of the pass and of the fail stands the tool's resume line, `::`, its token and `::`.

The pass:

```
2026-11-22T22:11:28.7396379Z ::stop-commands::***
2026-11-22T22:11:28.7397086Z ::stop-commands::***
2026-11-22T22:11:28.7752549Z 127.0.0.1:5000/rollproof@sha256:c0e103693ecb92455fe6d9cb1e0adef218a6548e85a8edf3cb0062be15c1fc3e: Pulling from rollproof
2026-11-22T22:11:28.7757401Z Digest: sha256:c0e103693ecb92455fe6d9cb1e0adef218a6548e85a8edf3cb0062be15c1fc3e
2026-11-22T22:11:28.7758348Z Status: Image is up to date for 127.0.0.1:5000/rollproof@sha256:c0e103693ecb92455fe6d9cb1e0adef218a6548e85a8edf3cb0062be15c1fc3e
2026-11-22T22:11:34.7327659Z rollproof: the tool from 127.0.0.1:5000/rollproof@sha256:c0e103693ecb92455fe6d9cb1e0adef218a6548e85a8edf3cb0062be15c1fc3e, through docker
2026-11-22T22:11:34.7418151Z ::stop-commands::FYHDQBMXZD2HCXMVEDJF5YNNSW
2026-11-22T22:11:34.7418882Z ::stop-commands::FYHDQBMXZD2HCXMVEDJF5YNNSW
2026-11-22T22:14:14.9038318Z rollproof: PASS   orders (Helm) on Kubernetes v1.37.0 in kind
…
2026-11-22T22:14:14.9097633Z results: /home/runner/work/_temp/results-pass
2026-11-22T22:14:18.4232913Z ::FYHDQBMXZD2HCXMVEDJF5YNNSW::
2026-11-22T22:14:18.4288601Z ##[end-action id=pass.run;outcome=success;conclusion=success;duration_ms=169701]
```

The fail:

```
2026-11-22T22:14:18.7039248Z ::stop-commands::***
2026-11-22T22:14:18.7039915Z ::stop-commands::***
2026-11-22T22:14:19.5575095Z rollproof: the tool from 127.0.0.1:5000/rollproof@sha256:c0e103693ecb92455fe6d9cb1e0adef218a6548e85a8edf3cb0062be15c1fc3e, through docker
2026-11-22T22:14:19.5667218Z ::stop-commands::6FFGCM2NBCNVBJYFO4JLJRULMW
2026-11-22T22:14:19.5668231Z ::stop-commands::6FFGCM2NBCNVBJYFO4JLJRULMW
2026-11-22T22:16:11.3503424Z rollproof: FAIL   orders (Helm) on Kubernetes v1.37.0 in kind
…
2026-11-22T22:16:11.3520561Z deploy: 874 checks failed (refused 874), first at 22:15:44.35   [probe-failures]
2026-11-22T22:16:11.3523210Z results: /home/runner/work/_temp/results-fail
2026-11-22T22:16:15.8579538Z ::6FFGCM2NBCNVBJYFO4JLJRULMW::
2026-11-22T22:16:15.8622683Z ##[error]Process completed with exit code 1.
2026-11-22T22:16:15.8638827Z ##[end-action id=fail.run;outcome=failure;conclusion=failure;duration_ms=117172]
```

The run cut short, which has no resume line:

```
2026-11-22T22:16:16.0144486Z Prepare all required actions
2026-11-22T22:16:16.0187549Z ##[start-action display=Run exec bash "$GITHUB_ACTION_PATH/action.sh";id=cut.run]
…
2026-11-22T22:16:16.0286267Z ::stop-commands::***
2026-11-22T22:16:16.0286919Z ::stop-commands::***
2026-11-22T22:16:16.7056688Z rollproof: the tool from 127.0.0.1:5000/rollproof@sha256:c0e103693ecb92455fe6d9cb1e0adef218a6548e85a8edf3cb0062be15c1fc3e, through docker
2026-11-22T22:16:16.7145647Z ::stop-commands::GOCXF4EIZ3NNVPTS5VZKNDK4FG
2026-11-22T22:16:16.7146341Z ::stop-commands::GOCXF4EIZ3NNVPTS5VZKNDK4FG
2026-11-22T22:18:16.0203099Z rollproof: NOT VERIFIED   orders (Helm) on Kubernetes v1.37.0 in kind
…
2026-11-22T22:18:16.0235416Z steady     47.7 s    9,200        0              -     32 ms     4 ms   NOT VERIFIED
2026-11-22T22:18:16.0235916Z deploy          -        -        -              -         -        -   not run
2026-11-22T22:18:16.0236432Z rollback        -        -        -              -         -        -   not run
2026-11-22T22:18:16.0236723Z 
2026-11-22T22:18:16.0236860Z steady: a signal stopped the run   [interrupted]
2026-11-22T22:18:16.0237150Z results: /home/runner/work/_temp/results-cut
2026-11-22T22:18:16.0378455Z ##[error]The action has timed out.
2026-11-22T22:18:16.0382050Z ##[end-action id=cut.run;outcome=failure;conclusion=failure;duration_ms=120019]
```

The run cut short's times. The step's first log line is at 22:16:16.0144486 and its `start-action` line at 22:16:16.0187549; the runner's line `The action has timed out.` is at 22:18:16.0378455, 120.023 s after the first and 120.019 s after the second. The step "Check the run cut short" printed first:

```
2026-11-22T22:18:16.0515173Z /home/runner/work/_temp/results-cut/result.json was last written at 2026-11-22 22:18:16.017808188 +0000
2026-11-22T22:18:16.0537823Z total 28
2026-11-22T22:18:16.0538447Z drwxr-xr-x 2 runner runner 4096 2026-11-22 22:17:12.511587370 +0000 commands
2026-11-22T22:18:16.0539295Z -rw-r--r-- 1 runner runner 2144 2026-11-22 22:18:16.026808352 +0000 events.txt
2026-11-22T22:18:16.0540237Z drwxr-xr-x 4 runner runner 4096 2026-11-22 22:17:13.258435594 +0000 pods
2026-11-22T22:18:16.0541649Z -rw-r--r-- 1 runner runner 7658 2026-11-22 22:18:15.325795602 +0000 probe.log
2026-11-22T22:18:16.0542455Z -rw-r--r-- 1 runner runner 1867 2026-11-22 22:18:16.020071182 +0000 timeline.tsv
2026-11-22T22:18:16.0543186Z -rw-r--r-- 1 runner runner  557 2026-11-22 22:18:16.018808207 +0000 windows.json
```

`result.json` was last written at 22:18:16.0178, 20.0 ms before the runner's line that the action had timed out. That moment is the last write of `result.json`, which a later warning's rewrite would move, so it is an upper bound for the moment the final file was written. The time stamps of `stat` and `ls` come from the file system and those of the log from the runner; that the two use one clock is not shown. The moment of the signal is not in the log. The evidence files `windows.json`, `timeline.tsv` and `events.txt` were last written 1.0, 2.3 and 9.0 ms after `result.json`.

The check steps. "Check the pass" printed one line, the notice, and `actioncheck` printed nothing; "Check the fail" printed nothing; "Check the run cut short" printed the lines above and then nothing. The step's environment, as the log shows it, held `OUTCOME: failure`, `VERDICT: not-verified` for the run cut short, `OUTCOME: failure`, `VERDICT: fail` for the fail, and `VERDICT: pass` for the pass. The run's list of steps gives `success` for all three check steps and for the three steps that called the action, and `skipped` for the upload of the results, which runs only on a failure.

```
2026-11-22T22:14:18.4482049Z ##[notice]workflow commands are read again in the step after the action
```

The run's list of annotations, as `gh run view 45076071694` printed it, holds three:

```
X The action has timed out.
action: .github#56

X Process completed with exit code 1.
action: .github#59

- workflow commands are read again in the step after the action
action: .github#13
```

The image's pull in the three steps printed `Status: Image is up to date`: the engine already held the image, which the same job had built and pushed. In the suite's `cluster` job, `TestTheScriptRunsTheToolFromItsImage` (`hack/action/action_cluster_test.go`) runs the same script on Docker against an image that it wrote to a registry on the loopback itself, with go-containerregistry, and the pull there fetched it:

```
2026-11-22T23:21:57.2617926Z         ::stop-commands::***
2026-11-22T23:21:57.2619196Z         127.0.0.1:42215/rollproof-action-test@sha256:5e2c691595b740823987dd17d277715dff179c3b9ecf1bf8d7776eb158d3a978: Pulling from rollproof-action-test
2026-11-22T23:21:57.2620492Z         Digest: sha256:5e2c691595b740823987dd17d277715dff179c3b9ecf1bf8d7776eb158d3a978
2026-11-22T23:21:57.2623686Z         Status: Downloaded newer image for 127.0.0.1:42215/rollproof-action-test@sha256:5e2c691595b740823987dd17d277715dff179c3b9ecf1bf8d7776eb158d3a978
2026-11-22T23:21:57.2625309Z         127.0.0.1:42215/rollproof-action-test@sha256:5e2c691595b740823987dd17d277715dff179c3b9ecf1bf8d7776eb158d3a978
2026-11-22T23:21:57.2626995Z         rollproof: the tool from 127.0.0.1:42215/rollproof-action-test@sha256:5e2c691595b740823987dd17d277715dff179c3b9ecf1bf8d7776eb158d3a978, through docker
2026-11-22T23:21:57.2628431Z         ::stop-commands::IRPBAU5DZGLLFO7YAEU4LILVPF
2026-11-22T23:21:57.4739508Z --- PASS: TestTheScriptRunsTheToolFromItsImage (136.89s)
```

That test starts the script from a working folder of its own, with `GITHUB_WORKSPACE` naming another folder that holds the configuration, and with a relative config (`rejected.yaml`) and a relative out (`results`); it requires `result.json` under the workspace's `results` (`hack/action/action_cluster_test.go`, lines 111 to 158 and 232 to 247, read). Its standard output, as the test logged it:

```
2026-11-22T23:21:57.2614417Z         could not test: version (line 1): this tool reads version 1 only   [config-rejected]
2026-11-22T23:21:57.2615484Z         results: /tmp/TestTheScriptRunsTheToolFromItsImage3013258898/003/workspace/results
```

### The mirror job, first run (45076080467, the plan's code)

```
2026-11-22T22:14:29.4023400Z --- FAIL: TestMirror (132.40s)
2026-11-22T22:14:29.4024603Z     --- FAIL: TestMirror/through_the_mirror (57.44s)
2026-11-22T22:14:29.4029232Z     --- FAIL: TestMirror/a_mirror_replaces_its_host (60.96s)
2026-11-22T22:14:29.4063377Z FAIL	rollproof/e2e	282.157s
```

Both runs of the tool ended before the install, with one reason:

```
2026-11-22T22:13:28.0520634Z         could not test: setup[10] (docker) exited with code 2   [setup-failed]
2026-11-22T22:14:29.0535599Z         could not test: setup[10] (docker) exited with code 2   [setup-failed]
```

`setup[i]` counts from 0, so the command was the eleventh of `mirrorSetup`'s list, `docker exec rollproof-e2e-worker getent hosts rollproof-mirrorjob-real` (`e2e/mirror_helpers_test.go`, read): after the cut, a node did not resolve the real host's container name. The tool runs the setup commands in order and reports the first that fails (`internal/deploy/setup.go`, lines 10 to 25, read), so the commands before it exited as the job wants, among them `setup[9]`, which fails if a public pull in a node succeeds within 15 s after the cut. The stand-ins logged only the harness's own check from the loopback (`REQ GET /v2/ user=- status=401`, 22:13:28.1290252 and 22:13:28.1497866). The job uploads nothing, and this log did not show what the failed command printed; Task 15 adds that (below).

The cause, as the controller of the build read and tried it, not shown on the runner: a kind node on Docker asks Docker's DNS at the network's gateway, and kind's entrypoint sends that query on to 127.0.0.11 by a nat rule (`sigs.k8s.io/kind@v0.33.0/images/base/files/usr/local/bin/entrypoint`, `enable_network_magic`, read); the filter chain then sees the packet with the destination 127.0.0.11 but leaving by `eth0`, so the cut's `-o lo -j RETURN` does not match it and the chain rejects it. The controller tried this in a private network namespace (`unshare -rn`, no container) on the developer's machine on 2026-11-22: with no cut the query was answered; with the cut as written it was rejected (`SENDER: no reply: PermissionError [Errno 1] Operation not permitted`, and 0 packets on the `-o lo` rule); with one more rule, `-d 127.0.0.0/8 -j RETURN`, it was answered (1 packet on that rule). On the developer's machine the same job had passed before the fix, because rootless nerdctl gives a node the other containers' names in its `/etc/hosts` (the local evidence of that command read `fc00:f853:ccd:e793::ef5 fce7f27c0206 fce7f27c0206.kind rollproof-mirrorjob-real rollproof-mirrorjob-real.kind`).

### The mirror job, second run (45080222875, after Task 15)

```
2026-11-22T23:03:47.6000112Z --- PASS: TestMirror (426.93s)
2026-11-22T23:03:47.6001225Z     --- PASS: TestMirror/through_the_mirror (144.76s)
2026-11-22T23:03:47.6023563Z     --- PASS: TestMirror/a_mirror_replaces_its_host (268.46s)
2026-11-22T23:03:47.6034899Z ok  	rollproof/e2e	575.059s
```

The first subtest's run passed through the mirror (`rollproof: PASS   orders (Helm) on Kubernetes v1.37.0 in kind`, 22:59:18.8210088); the second gave the failure that the test wants:

```
2026-11-22T23:03:47.2942378Z         deploy: did not finish within limits.timeout   [transition-stalled]
…
2026-11-22T23:03:47.2950241Z           the nodes pull rollproof-mirrorjob-real:5443 only from its mirror
2026-11-22T23:03:47.2952201Z           rollproof-mirrorjob-mirror:8443/real, and a pull's error may name the image alone
```

With both subtests passing, every setup command exited as wanted, the `getent` of the real host's name among them; in the first subtest the mirror logged the request for the previous image by the job's user, answered 200; and in both the real host's stand-in logged connections from the loopback only (`checkOnlyLoopback`; `e2e/mirror_test.go`, lines 39 to 94, read).

### The suite (run 45081200073)

The `cluster` job: 793 `--- PASS` lines, and no `--- FAIL` or `--- SKIP` line. The 793 lines are not listed here; the count and the package lines below stand for them, and the saved log keeps them. The `gh run view --log` output names this job's step `UNKNOWN STEP` on every line; the run's list of steps names it "The tests under the build tag cluster", `success`.

```
2026-11-22T23:12:44.6586011Z ok  	rollproof/internal/cluster	372.204s
2026-11-22T23:13:40.0233827Z ok  	rollproof/internal/images	52.976s
2026-11-22T23:16:37.1615159Z ok  	rollproof/internal/probe	173.409s
2026-11-22T23:19:38.5468674Z ok  	rollproof/internal/watch	107.871s
2026-11-22T23:21:57.6653229Z ok  	rollproof/hack/action	137.088s
```

The `helm-3` and `helm-4` jobs: each has 138 `--- PASS` lines, and no `--- FAIL` or `--- SKIP` line; both ran the same tests in the same order. Each ran the 32 fixture subtests under its own Helm major and the fourteenth row's control, which runs under Helm 3 in both jobs by design (`e2e/fixtures_test.go`, lines 274 to 294, read). The 32 fixture subtests took 4,299.14 s under Helm 3 and 3,978.61 s under Helm 4 by their own times; `p24-hosted-runner` gives 4,297 s and 4,411 s for them on the developer's machine. The harness's start, from the step's first line to the first test (`=== RUN   TestSetupCommandFails`), took 144.7 s in `helm-3` (23:22:24.0540366 to 23:24:48.7520264) and 124.0 s in `helm-4` (23:22:31.6259355 to 23:24:35.6438639).

The fourteenth row's control, in `helm-3`:

```
2026-11-23T00:18:02.5804989Z === RUN   TestChartDeniesTrafficByDefault/control
2026-11-23T00:19:37.7607473Z     fixtures_test.go:285: chart-denies-traffic-control (helm-3):
2026-11-23T00:19:37.7653567Z         rollproof: NOT VERIFIED   orders (Helm) on Kubernetes v1.37.0 in kind
2026-11-23T00:19:37.7722930Z                     3,001 checks, 3,001 failed (timeout 2,995, refused 6), the last 0.0 s before the steady window
2026-11-23T00:19:37.7728294Z         steady     10.0 s    2,000    2,000              -         -     3 ms   NOT VERIFIED
2026-11-23T00:19:37.7733824Z         steady: 2000 checks failed (timeout 2000), first at 00:19:21.23   [steady-not-clean]
2026-11-23T00:19:37.7735307Z           no check has passed since the probe started: every check failed for 25.0 s
2026-11-23T00:22:11.5063565Z     --- PASS: TestChartDeniesTrafficByDefault/control (95.20s)
```

In `helm-4`:

```
2026-11-23T00:13:49.8643034Z === RUN   TestChartDeniesTrafficByDefault/control
2026-11-23T00:15:15.5184267Z     fixtures_test.go:285: chart-denies-traffic-control (helm-3):
2026-11-23T00:15:15.5186441Z         rollproof: NOT VERIFIED   orders (Helm) on Kubernetes v1.37.0 in kind
2026-11-23T00:15:15.5195419Z                     3,002 checks, 3,002 failed (timeout 2,996, refused 6), the last 0.0 s before the steady window
2026-11-23T00:15:15.5197810Z         steady     10.0 s    2,000    2,000              -         -     3 ms   NOT VERIFIED
2026-11-23T00:15:15.5200202Z         steady: 2000 checks failed (timeout 2000), first at 00:15:02.27   [steady-not-clean]
2026-11-23T00:15:15.5201026Z           no check has passed since the probe started: every check failed for 25.0 s
2026-11-23T00:17:28.5315343Z     --- PASS: TestChartDeniesTrafficByDefault/control (85.67s)
```

Every test and subtest of `helm-3`:

```
2026-11-22T23:26:18.7854978Z --- PASS: TestSetupCommandFails (90.03s)
2026-11-22T23:26:18.7857589Z     --- PASS: TestSetupCommandFails/helm-3 (90.03s)
2026-11-22T23:28:46.4076862Z --- PASS: TestCommandsDriverWithAllThreeCommands (147.62s)
2026-11-22T23:28:46.4078765Z     --- PASS: TestCommandsDriverWithAllThreeCommands/helm-3 (147.62s)
2026-11-22T23:30:46.4798829Z --- PASS: TestCommandsDriverWithInstallAndDeployOnly (120.07s)
2026-11-22T23:30:46.4800937Z     --- PASS: TestCommandsDriverWithInstallAndDeployOnly/helm-3 (120.07s)
2026-11-22T23:33:11.3860437Z --- PASS: TestSetupMakesTheProbeNamespaceRestricted (144.91s)
2026-11-22T23:33:11.3863329Z     --- PASS: TestSetupMakesTheProbeNamespaceRestricted/helm-3 (144.91s)
2026-11-22T23:33:11.4041723Z --- PASS: TestConfigOfEachShapeLoads (0.02s)
2026-11-22T23:33:11.4043253Z     --- PASS: TestConfigOfEachShapeLoads/both_releases (0.00s)
2026-11-22T23:33:11.4044412Z     --- PASS: TestConfigOfEachShapeLoads/no_previous (0.00s)
2026-11-22T23:33:11.4045658Z     --- PASS: TestConfigOfEachShapeLoads/candidate_runs_previous (0.00s)
2026-11-22T23:33:11.4046958Z     --- PASS: TestConfigOfEachShapeLoads/node_image (0.00s)
2026-11-22T23:33:11.4048023Z     --- PASS: TestConfigOfEachShapeLoads/setup (0.00s)
2026-11-22T23:33:11.4049082Z     --- PASS: TestConfigOfEachShapeLoads/three_commands (0.00s)
2026-11-22T23:33:11.4050076Z     --- PASS: TestConfigOfEachShapeLoads/two_commands (0.00s)
2026-11-22T23:33:11.4051095Z     --- PASS: TestConfigOfEachShapeLoads/fixture_probe (0.00s)
2026-11-22T23:33:11.4052265Z     --- PASS: TestConfigOfEachShapeLoads/TCP_loop (0.00s)
2026-11-22T23:33:11.4053099Z     --- PASS: TestConfigOfEachShapeLoads/limits (0.00s)
2026-11-22T23:33:11.4053998Z     --- PASS: TestConfigOfEachShapeLoads/through_the_mirror (0.00s)
2026-11-22T23:33:11.4054937Z     --- PASS: TestConfigOfEachShapeLoads/the_mirror_replaces_its_host (0.00s)
2026-11-22T23:33:11.4056145Z --- PASS: TestReadMarks (0.00s)
2026-11-22T23:33:11.4058170Z --- PASS: TestRenderConfigRejectsAnUnknownName (0.00s)
2026-11-22T23:33:11.4058947Z     --- PASS: TestRenderConfigRejectsAnUnknownName/custom_image (0.00s)
2026-11-22T23:33:11.4059619Z     --- PASS: TestRenderConfigRejectsAnUnknownName/command_role (0.00s)
2026-11-22T23:33:11.4060654Z --- PASS: TestManifestValuesLetTheFixtureSetImage (0.00s)
2026-11-22T23:33:11.7237025Z --- PASS: TestActionLoop (0.32s)
2026-11-22T23:33:11.7242300Z     --- PASS: TestActionLoop/the_action_reports_done (0.00s)
2026-11-22T23:33:11.7243259Z     --- PASS: TestActionLoop/the_action_fails (0.00s)
2026-11-22T23:33:11.7243970Z     --- PASS: TestActionLoop/the_run_ends_first (0.00s)
2026-11-22T23:33:11.7244925Z     --- PASS: TestActionLoop/the_run_ends_between_two_calls (0.31s)
2026-11-22T23:33:11.7247185Z --- PASS: TestInterruptWaitsForTheDeploysStart (0.00s)
2026-11-22T23:33:11.7248620Z --- PASS: TestRestartComesAfterTheDelay (0.00s)
2026-11-22T23:35:12.7811693Z --- PASS: TestCustomProbeGoesSilentOnTheCandidate (121.06s)
2026-11-22T23:35:12.7813801Z     --- PASS: TestCustomProbeGoesSilentOnTheCandidate/helm-3 (121.06s)
2026-11-22T23:37:09.6258715Z --- PASS: TestCustomProbeExitsOnTheCandidate (116.84s)
2026-11-22T23:37:09.6260209Z     --- PASS: TestCustomProbeExitsOnTheCandidate/helm-3 (116.84s)
2026-11-22T23:39:43.7056742Z --- PASS: TestTCPLoopJudgesTheFirstFixture (154.08s)
2026-11-22T23:39:43.7063579Z     --- PASS: TestTCPLoopJudgesTheFirstFixture/helm-3 (154.08s)
2026-11-22T23:42:12.7569316Z --- PASS: TestProbeFailsItsFirstFiveSeconds (149.05s)
2026-11-22T23:42:12.7577358Z     --- PASS: TestProbeFailsItsFirstFiveSeconds/helm-3 (149.05s)
2026-11-22T23:43:42.0748357Z --- PASS: TestProbeFailsItsFirstTwentySeconds (89.32s)
2026-11-22T23:43:42.0750890Z     --- PASS: TestProbeFailsItsFirstTwentySeconds/helm-3 (89.32s)
2026-11-22T23:45:43.6414119Z --- PASS: TestTCPLoopJudgesACandidateWithoutReadiness (121.57s)
2026-11-22T23:45:43.6418800Z     --- PASS: TestTCPLoopJudgesACandidateWithoutReadiness/helm-3 (121.57s)
2026-11-22T23:48:22.0278764Z --- PASS: TestCustomProbeIgnoresSIGTERM (158.39s)
2026-11-22T23:48:22.0287098Z     --- PASS: TestCustomProbeIgnoresSIGTERM/helm-3 (158.39s)
2026-11-22T23:50:47.0073027Z --- PASS: TestTCPLoopInARestrictedProbeNamespace (144.98s)
2026-11-22T23:50:47.0075111Z     --- PASS: TestTCPLoopInARestrictedProbeNamespace/helm-3 (144.98s)
2026-11-22T23:53:13.9113361Z --- PASS: TestFirstFixture (146.90s)
2026-11-22T23:53:13.9114762Z     --- PASS: TestFirstFixture/helm-3 (146.90s)
2026-11-22T23:55:12.8191049Z --- PASS: TestCandidateWithoutReadiness (118.91s)
2026-11-22T23:55:12.8193246Z     --- PASS: TestCandidateWithoutReadiness/helm-3 (118.91s)
2026-11-22T23:57:29.5484883Z --- PASS: TestCandidateExitsOnSIGTERM (136.73s)
2026-11-22T23:57:29.5486963Z     --- PASS: TestCandidateExitsOnSIGTERM/helm-3 (136.73s)
2026-11-23T00:00:31.4760476Z --- PASS: TestNoRoomForTheExtraPod (181.93s)
2026-11-23T00:00:31.4767033Z     --- PASS: TestNoRoomForTheExtraPod/helm-3 (181.93s)
2026-11-23T00:02:04.7964893Z --- PASS: TestPreviousFailsEveryRequest (93.32s)
2026-11-23T00:02:04.7967676Z     --- PASS: TestPreviousFailsEveryRequest/helm-3 (93.32s)
2026-11-23T00:04:37.1570656Z --- PASS: TestFirstFixtureOnTheOldestNodeImage (152.36s)
2026-11-23T00:04:37.1577145Z     --- PASS: TestFirstFixtureOnTheOldestNodeImage/helm-3 (152.36s)
2026-11-23T00:06:24.1011991Z --- PASS: TestPreviousExitsOnSIGTERM (106.92s)
2026-11-23T00:06:24.1032229Z     --- PASS: TestPreviousExitsOnSIGTERM/helm-3 (106.92s)
2026-11-23T00:08:13.0796620Z --- PASS: TestDeployReplacesNoPod (109.00s)
2026-11-23T00:08:13.0803548Z     --- PASS: TestDeployReplacesNoPod/helm-3 (109.00s)
2026-11-23T00:10:48.6164580Z --- PASS: TestPreviousNeverReady (155.54s)
2026-11-23T00:10:48.6166810Z     --- PASS: TestPreviousNeverReady/helm-3 (155.54s)
2026-11-23T00:12:47.6707432Z --- PASS: TestNoPreviousRelease (119.05s)
2026-11-23T00:12:47.6708156Z     --- PASS: TestNoPreviousRelease/helm-3 (119.05s)
2026-11-23T00:15:16.0903512Z --- PASS: TestBothReleasesEndWithoutDraining (148.42s)
2026-11-23T00:15:16.0907993Z     --- PASS: TestBothReleasesEndWithoutDraining/helm-3 (148.42s)
2026-11-23T00:18:02.5800036Z --- PASS: TestSecondRolloutAfterTheFirstFinished (166.49s)
2026-11-23T00:18:02.5802794Z     --- PASS: TestSecondRolloutAfterTheFirstFinished/helm-3 (166.49s)
2026-11-23T00:22:11.5060683Z --- PASS: TestChartDeniesTrafficByDefault (248.93s)
2026-11-23T00:22:11.5063565Z     --- PASS: TestChartDeniesTrafficByDefault/control (95.20s)
2026-11-23T00:22:11.5064440Z     --- PASS: TestChartDeniesTrafficByDefault/helm-3 (153.73s)
2026-11-23T00:24:35.4016208Z --- PASS: TestStatefulSet (143.90s)
2026-11-23T00:24:35.4020778Z     --- PASS: TestStatefulSet/helm-3 (143.90s)
2026-11-23T00:26:48.9403247Z --- PASS: TestDaemonSet (133.54s)
2026-11-23T00:26:48.9408765Z     --- PASS: TestDaemonSet/helm-3 (133.54s)
2026-11-23T00:30:03.6010227Z --- PASS: TestHooksLongerThanTheSettleTime (194.66s)
2026-11-23T00:30:03.6022290Z     --- PASS: TestHooksLongerThanTheSettleTime/helm-3 (194.66s)
2026-11-23T00:32:28.1930955Z --- PASS: TestCandidateFailsAfterTheRolloutFinished (144.59s)
2026-11-23T00:32:28.1935698Z     --- PASS: TestCandidateFailsAfterTheRolloutFinished/helm-3 (144.59s)
2026-11-23T00:34:13.1815641Z --- PASS: TestCandidateValueBreaksTheSchema (104.99s)
2026-11-23T00:34:13.1817306Z     --- PASS: TestCandidateValueBreaksTheSchema/helm-3 (104.99s)
2026-11-23T00:35:44.8486965Z --- PASS: TestSIGINTDuringTheDeploy (91.67s)
2026-11-23T00:35:44.8502605Z     --- PASS: TestSIGINTDuringTheDeploy/helm-3 (91.67s)
2026-11-23T00:38:03.4148715Z --- PASS: TestCandidateChangesOnlyAnUnprobedWorkload (138.57s)
2026-11-23T00:38:03.4155259Z     --- PASS: TestCandidateChangesOnlyAnUnprobedWorkload/helm-3 (138.57s)
2026-11-23T00:38:03.4163029Z --- PASS: TestLoopbackProblems (0.00s)
2026-11-23T00:38:03.4163947Z     --- PASS: TestLoopbackProblems/loopback_only (0.00s)
2026-11-23T00:38:03.4164888Z     --- PASS: TestLoopbackProblems/no_CONN_line (0.00s)
2026-11-23T00:38:03.4165826Z     --- PASS: TestLoopbackProblems/loopback_and_a_node (0.00s)
2026-11-23T00:38:03.4166872Z     --- PASS: TestLoopbackProblems/a_node_only (0.00s)
2026-11-23T00:38:03.4167776Z     --- PASS: TestLoopbackProblems/an_address_with_no_port (0.00s)
2026-11-23T00:38:03.4169325Z --- PASS: TestCutScript (0.00s)
2026-11-23T00:38:03.4175523Z --- PASS: TestNewAuthority (0.00s)
2026-11-23T00:38:03.4177626Z --- PASS: TestPasswordForms (0.00s)
2026-11-23T00:38:03.4192294Z --- PASS: TestFindForms (0.00s)
2026-11-23T00:38:03.4193329Z     --- PASS: TestFindForms/names_the_index_and_skips_an_empty_form (0.00s)
2026-11-23T00:38:03.4194516Z     --- PASS: TestFindForms/sorts_every_file_and_form (0.00s)
2026-11-23T00:38:03.4196381Z --- PASS: TestParseStandinLog (0.00s)
2026-11-23T00:38:03.4197915Z --- PASS: TestHelmMajors (0.00s)
2026-11-23T00:38:03.4199635Z --- PASS: TestRunEnvKeepsTheJobsGitHubFilesOut (0.00s)
2026-11-23T00:38:03.4210924Z --- PASS: TestSummaryFaults (0.00s)
2026-11-23T00:38:03.4211993Z     --- PASS: TestSummaryFaults/a_good_summary (0.00s)
2026-11-23T00:38:03.4213086Z     --- PASS: TestSummaryFaults/a_warning_appended_after_the_results_block (0.00s)
2026-11-23T00:38:03.4214312Z     --- PASS: TestSummaryFaults/the_summary_twice (0.00s)
2026-11-23T00:38:03.4215209Z     --- PASS: TestSummaryFaults/no_results_line (0.00s)
2026-11-23T00:38:03.4216172Z     --- PASS: TestSummaryFaults/a_first_line_that_is_not_the_heading (0.00s)
2026-11-23T00:38:03.4216959Z     --- PASS: TestSummaryFaults/another_word (0.00s)
2026-11-23T00:38:03.4217760Z     --- PASS: TestSummaryFaults/a_results_line_that_names_another_folder (0.00s)
2026-11-23T00:38:03.4219022Z --- PASS: TestEnvValue (0.00s)
2026-11-23T00:38:03.4230756Z --- PASS: TestWorkflowCommandFaults (0.00s)
2026-11-23T00:38:03.4231651Z     --- PASS: TestWorkflowCommandFaults/off_GitHub (0.00s)
2026-11-23T00:38:03.4232464Z     --- PASS: TestWorkflowCommandFaults/off_GitHub,_a_stop_line (0.00s)
2026-11-23T00:38:03.4233548Z     --- PASS: TestWorkflowCommandFaults/off_GitHub,_an_indented_line_on_stderr (0.00s)
2026-11-23T00:38:03.4234366Z     --- PASS: TestWorkflowCommandFaults/on_GitHub (0.00s)
2026-11-23T00:38:03.4235133Z     --- PASS: TestWorkflowCommandFaults/on_GitHub,_after_a_signal (0.00s)
2026-11-23T00:38:03.4235899Z     --- PASS: TestWorkflowCommandFaults/no_stop_line (0.00s)
2026-11-23T00:38:03.4236664Z     --- PASS: TestWorkflowCommandFaults/a_token_of_another_form (0.00s)
2026-11-23T00:38:03.4237496Z     --- PASS: TestWorkflowCommandFaults/nothing_on_stdout (0.00s)
2026-11-23T00:38:03.4238353Z     --- PASS: TestWorkflowCommandFaults/stderr_begins_with_another_line (0.00s)
2026-11-23T00:38:03.4238942Z     --- PASS: TestWorkflowCommandFaults/the_stop_line_twice (0.00s)
2026-11-23T00:38:03.4239749Z     --- PASS: TestWorkflowCommandFaults/no_resume_line (0.00s)
2026-11-23T00:38:03.4240943Z     --- PASS: TestWorkflowCommandFaults/the_resume_line_twice (0.00s)
2026-11-23T00:38:03.4242035Z     --- PASS: TestWorkflowCommandFaults/a_resume_line_after_a_signal (0.00s)
2026-11-23T00:38:03.4242940Z     --- PASS: TestWorkflowCommandFaults/a_resume_line_on_stderr (0.00s)
2026-11-23T00:38:03.4243821Z     --- PASS: TestWorkflowCommandFaults/off_GitHub,_a_line_after_a_lone_carriage_return (0.00s)
2026-11-23T00:38:03.4244957Z --- PASS: TestFailedCommands (0.00s)
2026-11-23T00:38:03.4250024Z --- PASS: TestReadGitHubOutputs (0.00s)
2026-11-23T00:38:03.4250711Z     --- PASS: TestReadGitHubOutputs/a_value_of_two_lines (0.00s)
2026-11-23T00:38:03.4256315Z     --- PASS: TestReadGitHubOutputs/one_name_written_twice (0.00s)
2026-11-23T00:38:03.4257024Z     --- PASS: TestReadGitHubOutputs/an_empty_file (0.00s)
2026-11-23T00:38:03.4257626Z     --- PASS: TestReadGitHubOutputs/the_plain_form_after_the_delimiter_form (0.00s)
2026-11-23T00:38:03.4258458Z     --- PASS: TestReadGitHubOutputs/a_line_in_the_plain_form (0.00s)
2026-11-23T00:38:03.4259409Z     --- PASS: TestReadGitHubOutputs/a_delimiter_that_never_comes (0.00s)
2026-11-23T00:38:03.4260735Z ok  	rollproof/e2e	4528.287s
```

Every test and subtest of `helm-4`:

```
2026-11-22T23:25:50.5135056Z --- PASS: TestSetupCommandFails (74.87s)
2026-11-22T23:25:50.5136537Z     --- PASS: TestSetupCommandFails/helm-4 (74.87s)
2026-11-22T23:28:02.1037672Z --- PASS: TestCommandsDriverWithAllThreeCommands (131.59s)
2026-11-22T23:28:02.1038943Z     --- PASS: TestCommandsDriverWithAllThreeCommands/helm-4 (131.59s)
2026-11-22T23:29:53.6063953Z --- PASS: TestCommandsDriverWithInstallAndDeployOnly (111.50s)
2026-11-22T23:29:53.6066743Z     --- PASS: TestCommandsDriverWithInstallAndDeployOnly/helm-4 (111.50s)
2026-11-22T23:32:13.1503259Z --- PASS: TestSetupMakesTheProbeNamespaceRestricted (139.54s)
2026-11-22T23:32:13.1504791Z     --- PASS: TestSetupMakesTheProbeNamespaceRestricted/helm-4 (139.54s)
2026-11-22T23:32:13.1615448Z --- PASS: TestConfigOfEachShapeLoads (0.01s)
2026-11-22T23:32:13.1616509Z     --- PASS: TestConfigOfEachShapeLoads/both_releases (0.00s)
2026-11-22T23:32:13.1617331Z     --- PASS: TestConfigOfEachShapeLoads/no_previous (0.00s)
2026-11-22T23:32:13.1618111Z     --- PASS: TestConfigOfEachShapeLoads/candidate_runs_previous (0.00s)
2026-11-22T23:32:13.1618826Z     --- PASS: TestConfigOfEachShapeLoads/node_image (0.00s)
2026-11-22T23:32:13.1619510Z     --- PASS: TestConfigOfEachShapeLoads/setup (0.00s)
2026-11-22T23:32:13.1620121Z     --- PASS: TestConfigOfEachShapeLoads/three_commands (0.00s)
2026-11-22T23:32:13.1621163Z     --- PASS: TestConfigOfEachShapeLoads/two_commands (0.00s)
2026-11-22T23:32:13.1621729Z     --- PASS: TestConfigOfEachShapeLoads/fixture_probe (0.00s)
2026-11-22T23:32:13.1622970Z     --- PASS: TestConfigOfEachShapeLoads/TCP_loop (0.00s)
2026-11-22T23:32:13.1623458Z     --- PASS: TestConfigOfEachShapeLoads/limits (0.00s)
2026-11-22T23:32:13.1623945Z     --- PASS: TestConfigOfEachShapeLoads/through_the_mirror (0.00s)
2026-11-22T23:32:13.1624498Z     --- PASS: TestConfigOfEachShapeLoads/the_mirror_replaces_its_host (0.00s)
2026-11-22T23:32:13.1625225Z --- PASS: TestReadMarks (0.00s)
2026-11-22T23:32:13.1626807Z --- PASS: TestRenderConfigRejectsAnUnknownName (0.00s)
2026-11-22T23:32:13.1627374Z     --- PASS: TestRenderConfigRejectsAnUnknownName/custom_image (0.00s)
2026-11-22T23:32:13.1628004Z     --- PASS: TestRenderConfigRejectsAnUnknownName/command_role (0.00s)
2026-11-22T23:32:13.1628981Z --- PASS: TestManifestValuesLetTheFixtureSetImage (0.00s)
2026-11-22T23:32:13.4842774Z --- PASS: TestActionLoop (0.32s)
2026-11-22T23:32:13.4845205Z     --- PASS: TestActionLoop/the_action_reports_done (0.00s)
2026-11-22T23:32:13.4845982Z     --- PASS: TestActionLoop/the_action_fails (0.00s)
2026-11-22T23:32:13.4846686Z     --- PASS: TestActionLoop/the_run_ends_first (0.00s)
2026-11-22T23:32:13.4847381Z     --- PASS: TestActionLoop/the_run_ends_between_two_calls (0.32s)
2026-11-22T23:32:13.4848909Z --- PASS: TestInterruptWaitsForTheDeploysStart (0.00s)
2026-11-22T23:32:13.4851212Z --- PASS: TestRestartComesAfterTheDelay (0.00s)
2026-11-22T23:34:05.4861316Z --- PASS: TestCustomProbeGoesSilentOnTheCandidate (112.00s)
2026-11-22T23:34:05.4862422Z     --- PASS: TestCustomProbeGoesSilentOnTheCandidate/helm-4 (112.00s)
2026-11-22T23:35:55.8016221Z --- PASS: TestCustomProbeExitsOnTheCandidate (110.32s)
2026-11-22T23:35:55.8020372Z     --- PASS: TestCustomProbeExitsOnTheCandidate/helm-4 (110.32s)
2026-11-22T23:38:13.8652393Z --- PASS: TestTCPLoopJudgesTheFirstFixture (138.06s)
2026-11-22T23:38:13.8654049Z     --- PASS: TestTCPLoopJudgesTheFirstFixture/helm-4 (138.06s)
2026-11-22T23:40:32.6163242Z --- PASS: TestProbeFailsItsFirstFiveSeconds (138.75s)
2026-11-22T23:40:32.6164065Z     --- PASS: TestProbeFailsItsFirstFiveSeconds/helm-4 (138.75s)
2026-11-22T23:41:58.2360755Z --- PASS: TestProbeFailsItsFirstTwentySeconds (85.62s)
2026-11-22T23:41:58.2361551Z     --- PASS: TestProbeFailsItsFirstTwentySeconds/helm-4 (85.62s)
2026-11-22T23:43:45.8003131Z --- PASS: TestTCPLoopJudgesACandidateWithoutReadiness (107.57s)
2026-11-22T23:43:45.8004832Z     --- PASS: TestTCPLoopJudgesACandidateWithoutReadiness/helm-4 (107.57s)
2026-11-22T23:46:09.0699343Z --- PASS: TestCustomProbeIgnoresSIGTERM (143.27s)
2026-11-22T23:46:09.0703410Z     --- PASS: TestCustomProbeIgnoresSIGTERM/helm-4 (143.27s)
2026-11-22T23:48:28.3613061Z --- PASS: TestTCPLoopInARestrictedProbeNamespace (139.29s)
2026-11-22T23:48:28.3624779Z     --- PASS: TestTCPLoopInARestrictedProbeNamespace/helm-4 (139.29s)
2026-11-22T23:50:43.4281010Z --- PASS: TestFirstFixture (135.07s)
2026-11-22T23:50:43.4281591Z     --- PASS: TestFirstFixture/helm-4 (135.07s)
2026-11-22T23:52:31.8910939Z --- PASS: TestCandidateWithoutReadiness (108.46s)
2026-11-22T23:52:31.8912319Z     --- PASS: TestCandidateWithoutReadiness/helm-4 (108.46s)
2026-11-22T23:54:34.6314607Z --- PASS: TestCandidateExitsOnSIGTERM (122.74s)
2026-11-22T23:54:34.6315094Z     --- PASS: TestCandidateExitsOnSIGTERM/helm-4 (122.74s)
2026-11-22T23:57:29.3049408Z --- PASS: TestNoRoomForTheExtraPod (174.67s)
2026-11-22T23:57:29.3051003Z     --- PASS: TestNoRoomForTheExtraPod/helm-4 (174.67s)
2026-11-22T23:58:55.5721769Z --- PASS: TestPreviousFailsEveryRequest (86.27s)
2026-11-22T23:58:55.5723395Z     --- PASS: TestPreviousFailsEveryRequest/helm-4 (86.27s)
2026-11-23T00:01:18.3111301Z --- PASS: TestFirstFixtureOnTheOldestNodeImage (142.74s)
2026-11-23T00:01:18.3113168Z     --- PASS: TestFirstFixtureOnTheOldestNodeImage/helm-4 (142.74s)
2026-11-23T00:02:59.7767932Z --- PASS: TestPreviousExitsOnSIGTERM (101.47s)
2026-11-23T00:02:59.7769000Z     --- PASS: TestPreviousExitsOnSIGTERM/helm-4 (101.47s)
2026-11-23T00:04:37.0096128Z --- PASS: TestDeployReplacesNoPod (97.23s)
2026-11-23T00:04:37.0097393Z     --- PASS: TestDeployReplacesNoPod/helm-4 (97.23s)
2026-11-23T00:07:04.8661436Z --- PASS: TestPreviousNeverReady (147.86s)
2026-11-23T00:07:04.8665055Z     --- PASS: TestPreviousNeverReady/helm-4 (147.86s)
2026-11-23T00:08:54.1944353Z --- PASS: TestNoPreviousRelease (109.33s)
2026-11-23T00:08:54.1945488Z     --- PASS: TestNoPreviousRelease/helm-4 (109.33s)
2026-11-23T00:11:14.0477728Z --- PASS: TestBothReleasesEndWithoutDraining (139.85s)
2026-11-23T00:11:14.0478765Z     --- PASS: TestBothReleasesEndWithoutDraining/helm-4 (139.85s)
2026-11-23T00:13:49.8639211Z --- PASS: TestSecondRolloutAfterTheFirstFinished (155.82s)
2026-11-23T00:13:49.8640383Z     --- PASS: TestSecondRolloutAfterTheFirstFinished/helm-4 (155.82s)
2026-11-23T00:17:28.5312021Z --- PASS: TestChartDeniesTrafficByDefault (218.67s)
2026-11-23T00:17:28.5315343Z     --- PASS: TestChartDeniesTrafficByDefault/control (85.67s)
2026-11-23T00:17:28.5315976Z     --- PASS: TestChartDeniesTrafficByDefault/helm-4 (133.00s)
2026-11-23T00:19:47.8014266Z --- PASS: TestStatefulSet (139.27s)
2026-11-23T00:19:47.8015264Z     --- PASS: TestStatefulSet/helm-4 (139.27s)
2026-11-23T00:21:50.6050306Z --- PASS: TestDaemonSet (122.80s)
2026-11-23T00:21:50.6051637Z     --- PASS: TestDaemonSet/helm-4 (122.80s)
2026-11-23T00:24:54.2643885Z --- PASS: TestHooksLongerThanTheSettleTime (183.66s)
2026-11-23T00:24:54.2645114Z     --- PASS: TestHooksLongerThanTheSettleTime/helm-4 (183.66s)
2026-11-23T00:27:10.9366242Z --- PASS: TestCandidateFailsAfterTheRolloutFinished (136.67s)
2026-11-23T00:27:10.9367503Z     --- PASS: TestCandidateFailsAfterTheRolloutFinished/helm-4 (136.67s)
2026-11-23T00:28:49.9432404Z --- PASS: TestCandidateValueBreaksTheSchema (99.01s)
2026-11-23T00:28:49.9436106Z     --- PASS: TestCandidateValueBreaksTheSchema/helm-4 (99.01s)
2026-11-23T00:30:15.0957604Z --- PASS: TestSIGINTDuringTheDeploy (85.15s)
2026-11-23T00:30:15.0960561Z     --- PASS: TestSIGINTDuringTheDeploy/helm-4 (85.15s)
2026-11-23T00:32:20.2514568Z --- PASS: TestCandidateChangesOnlyAnUnprobedWorkload (125.16s)
2026-11-23T00:32:20.2515662Z     --- PASS: TestCandidateChangesOnlyAnUnprobedWorkload/helm-4 (125.16s)
2026-11-23T00:32:20.2519950Z --- PASS: TestLoopbackProblems (0.00s)
2026-11-23T00:32:20.2520535Z     --- PASS: TestLoopbackProblems/loopback_only (0.00s)
2026-11-23T00:32:20.2521129Z     --- PASS: TestLoopbackProblems/no_CONN_line (0.00s)
2026-11-23T00:32:20.2521721Z     --- PASS: TestLoopbackProblems/loopback_and_a_node (0.00s)
2026-11-23T00:32:20.2522540Z     --- PASS: TestLoopbackProblems/a_node_only (0.00s)
2026-11-23T00:32:20.2523177Z     --- PASS: TestLoopbackProblems/an_address_with_no_port (0.00s)
2026-11-23T00:32:20.2524249Z --- PASS: TestCutScript (0.00s)
2026-11-23T00:32:20.2535342Z --- PASS: TestNewAuthority (0.00s)
2026-11-23T00:32:20.2536810Z --- PASS: TestPasswordForms (0.00s)
2026-11-23T00:32:20.2540016Z --- PASS: TestFindForms (0.00s)
2026-11-23T00:32:20.2540673Z     --- PASS: TestFindForms/names_the_index_and_skips_an_empty_form (0.00s)
2026-11-23T00:32:20.2541434Z     --- PASS: TestFindForms/sorts_every_file_and_form (0.00s)
2026-11-23T00:32:20.2543263Z --- PASS: TestParseStandinLog (0.00s)
2026-11-23T00:32:20.2544469Z --- PASS: TestHelmMajors (0.00s)
2026-11-23T00:32:20.2545683Z --- PASS: TestRunEnvKeepsTheJobsGitHubFilesOut (0.00s)
2026-11-23T00:32:20.2551584Z --- PASS: TestSummaryFaults (0.00s)
2026-11-23T00:32:20.2552339Z     --- PASS: TestSummaryFaults/a_good_summary (0.00s)
2026-11-23T00:32:20.2553116Z     --- PASS: TestSummaryFaults/a_warning_appended_after_the_results_block (0.00s)
2026-11-23T00:32:20.2553946Z     --- PASS: TestSummaryFaults/the_summary_twice (0.00s)
2026-11-23T00:32:20.2554571Z     --- PASS: TestSummaryFaults/no_results_line (0.00s)
2026-11-23T00:32:20.2555266Z     --- PASS: TestSummaryFaults/a_first_line_that_is_not_the_heading (0.00s)
2026-11-23T00:32:20.2556112Z     --- PASS: TestSummaryFaults/another_word (0.00s)
2026-11-23T00:32:20.2556876Z     --- PASS: TestSummaryFaults/a_results_line_that_names_another_folder (0.00s)
2026-11-23T00:32:20.2558014Z --- PASS: TestEnvValue (0.00s)
2026-11-23T00:32:20.2567483Z --- PASS: TestWorkflowCommandFaults (0.00s)
2026-11-23T00:32:20.2568379Z     --- PASS: TestWorkflowCommandFaults/off_GitHub (0.00s)
2026-11-23T00:32:20.2568892Z     --- PASS: TestWorkflowCommandFaults/off_GitHub,_a_stop_line (0.00s)
2026-11-23T00:32:20.2569476Z     --- PASS: TestWorkflowCommandFaults/off_GitHub,_an_indented_line_on_stderr (0.00s)
2026-11-23T00:32:20.2570014Z     --- PASS: TestWorkflowCommandFaults/on_GitHub (0.00s)
2026-11-23T00:32:20.2570590Z     --- PASS: TestWorkflowCommandFaults/on_GitHub,_after_a_signal (0.00s)
2026-11-23T00:32:20.2571130Z     --- PASS: TestWorkflowCommandFaults/no_stop_line (0.00s)
2026-11-23T00:32:20.2571655Z     --- PASS: TestWorkflowCommandFaults/a_token_of_another_form (0.00s)
2026-11-23T00:32:20.2572390Z     --- PASS: TestWorkflowCommandFaults/nothing_on_stdout (0.00s)
2026-11-23T00:32:20.2572974Z     --- PASS: TestWorkflowCommandFaults/stderr_begins_with_another_line (0.00s)
2026-11-23T00:32:20.2573542Z     --- PASS: TestWorkflowCommandFaults/the_stop_line_twice (0.00s)
2026-11-23T00:32:20.2574094Z     --- PASS: TestWorkflowCommandFaults/no_resume_line (0.00s)
2026-11-23T00:32:20.2574602Z     --- PASS: TestWorkflowCommandFaults/the_resume_line_twice (0.00s)
2026-11-23T00:32:20.2575218Z     --- PASS: TestWorkflowCommandFaults/a_resume_line_after_a_signal (0.00s)
2026-11-23T00:32:20.2575760Z     --- PASS: TestWorkflowCommandFaults/a_resume_line_on_stderr (0.00s)
2026-11-23T00:32:20.2576404Z     --- PASS: TestWorkflowCommandFaults/off_GitHub,_a_line_after_a_lone_carriage_return (0.00s)
2026-11-23T00:32:20.2577248Z --- PASS: TestFailedCommands (0.00s)
2026-11-23T00:32:20.2583375Z --- PASS: TestReadGitHubOutputs (0.00s)
2026-11-23T00:32:20.2583767Z     --- PASS: TestReadGitHubOutputs/a_value_of_two_lines (0.00s)
2026-11-23T00:32:20.2584203Z     --- PASS: TestReadGitHubOutputs/one_name_written_twice (0.00s)
2026-11-23T00:32:20.2584621Z     --- PASS: TestReadGitHubOutputs/an_empty_file (0.00s)
2026-11-23T00:32:20.2585099Z     --- PASS: TestReadGitHubOutputs/the_plain_form_after_the_delimiter_form (0.00s)
2026-11-23T00:32:20.2585590Z     --- PASS: TestReadGitHubOutputs/a_line_in_the_plain_form (0.00s)
2026-11-23T00:32:20.2586062Z     --- PASS: TestReadGitHubOutputs/a_delimiter_that_never_comes (0.00s)
2026-11-23T00:32:20.2586650Z ok  	rollproof/e2e	4176.995s
```

### The tool's lines in the logs of `go test`

In every step that runs `go test` and the tool, the harness logs each run's output, and so the runner reads the tool's lines there too. The first run of the tool in such a step shows its token masked, in its stop line, its resume line and the stop line of standard error that follows; every later run shows its token in plain text. The first mirror run:

```
2026-11-22T22:13:28.0504166Z         ::stop-commands::***
…
2026-11-22T22:13:28.0523966Z         ::***::
2026-11-22T22:13:28.0525661Z         ::stop-commands::***
…
2026-11-22T22:14:29.0510405Z         ::stop-commands::XWS3EDYZ2WXDQBUAPYZEZ2P4HV
…
2026-11-22T22:14:29.0538518Z         ::XWS3EDYZ2WXDQBUAPYZEZ2P4HV::
2026-11-22T22:14:29.0539788Z         ::stop-commands::XWS3EDYZ2WXDQBUAPYZEZ2P4HV
```

The same holds in the second mirror run (`::stop-commands::***` at 22:59:18.8209115, `::stop-commands::5GX3NA5Z7YQFX75WYD6K4I2A27` at 23:03:47.2827741), in `helm-3` (`::stop-commands::***` at 23:26:18.7706182, in `TestSetupCommandFails`, the job's first run of the tool), in `helm-4` (23:25:50.5032013), and in the `cluster` job, where the script's stop line is masked and the tool's stands in plain text after it (23:21:57.2611626 and 23:21:57.2612772).

What this shows: the runner masked the first token and not the later ones. The runner's source adds the token of a stop line that it reads as a command to the values it masks (`ActionCommandManager.cs`, lines 116 to 121, as the plan for step 6 reads it); so the first line was read as a command, and the later ones, which came while the runner's commands were stopped, were not. The first run's resume line, masked, and the stop line right after it fit the plan's reading that a logged resume line is followed at once by the stop line of standard error, which stops the commands again. What it does not show: that a workflow command printed after a stop line is ignored, since no run plants one; where the runner applies its masker to a line; and the order in which the runner reads the lines of a step's two streams. The steps print no workflow command of their own, so this changes nothing in them.

## Decision

- **The mirror job on Docker failed on the plan's code**, at `setup[10]`, the resolution of a container's name in a node after the cut. With the maintainer's word of 2026-11-22, Task 15 was added during Task 13 and built as `f0708c6`: the cut passes the IPv4 destination 127.0.0.0/8 before it rejects, so that the nodes' queries to Docker's DNS, which a nat rule sends on to 127.0.0.11, are not rejected; and when a fixture fails, the harness logs the last 40 lines of each command's evidence that does not end `rollproof: ok, exit code 0`, since a hosted job that uploads nothing keeps that evidence nowhere else. Its review approved it. The mirror job passed with it on the next run. This is a finding for the maintainer: the plan's belief did not hold.
- **The action's test passed**: the binary is taken out of the image on Docker, the pull by digest from the job's registry needs no setting, the tool runs in the workspace, the run cut short by the step's limit wrote its final files as `not-verified` with the reason `interrupted` and its step's outcome reached the next step, and the masked copy of the job summary was where the test looks.
- **The suite passed** under Helm 3 on `ubuntu-24.04` and under Helm 4 on `ubuntu-26.04`, with the fourteenth row's control giving `steady-not-clean` in both jobs. Docker's version in the Helm 4 job is not shown (see "Machine and versions"). The Helm 4 job ran in AWS's region `us-west-2`, where the other jobs ran in Azure's; nothing in the plan names the cloud.
- **The time** was below the plan's estimate: 165 billed minutes for the suite against 172 to 205, and 10 for the action's test against about 11, by the jobs' log times (166 and 10 by the jobs' own times); GitHub's bill was not read. The fixture subtests under Helm 4 took 0.90 times as long as on the developer's machine, and those under Helm 3 1.00 times. The two mirror runs took 6 and 11 minutes. This is a finding for the maintainer: the plan's belief did not hold, in the cheaper direction.
- **Left open:** the action under Podman, and its way of taking the binary out of its image there; the 7.5 s on a runner that persists; how long an `always()` step may run after a cancel; a job's time limit on a step that calls a composite action; a workflow command printed after the action's stop line, which no run plants, and so whether the step reads no command after it; that a node's query for a container's name reaches Docker's DNS through 127.0.0.11 on the runner, which was read and tried off the runner; the order in which the runner reads the lines of a step's two streams; Docker's version on the `ubuntu-26.04` image that ran; a cancel of the action's step by a user; the Helm that the action's job found on the runner's PATH; and a second run of anything here.

What decides each pair, in the order of the pairs below:

1. The export. The script prints `rollproof: the tool from …, through docker` only after `take_binary` has run `docker create`, `docker export … | tar -x -C DIR -f - rollproof` and checked that `DIR/rollproof` is executable (`action.sh`, lines 109 to 118 and 131 to 139, read); that line stands in all three steps (22:11:34.7327659, 22:14:19.5575095, 22:16:16.7056688) and in the `cluster` job (23:21:57.2626995), and the tool then ran. The member's name in the export is printed by no log; what the runs show is that `tar` found a member by the name `rollproof`. That this shows the name has no `./` in front rests on GNU tar's rule that an operand names a member exactly: with GNU tar 1.35 on the developer's machine on 2026-11-22, `tar -x -C DIR -f - rollproof` on an archive whose one member is `./rollproof` failed with `tar: rollproof: Not found in archive` and exit 2, and on an archive whose member is `rollproof` extracted it. The runner's tar and its version are not shown.
2. The pull. The workflow files set nothing for the daemon (`.github/workflows/rollproof-action.yml` and `rollproof-e2e.yml`, read). The action's job pulled by digest from `127.0.0.1:5000` with no error (`Status: Image is up to date`, 22:11:28.7758348), and the `cluster` job pulled by digest from `127.0.0.1:42215` an image that its engine did not hold by that digest (`Status: Downloaded newer image`, 23:21:57.2623686).
3. The workspace. The action's job gave a relative config, `projects/rollproof/hack/action/testdata/pass.yaml`, which the tool read, and absolute outs; the `cluster` job's test gave both relative and passed, with the results under the workspace (23:21:57.2615484).
4. The cut. The console shows the run in its steady window when a signal stopped it (`steady     47.7 s …   NOT VERIFIED`, `steady: a signal stopped the run   [interrupted]`, 22:18:16.0235416 and 22:18:16.0236860); `actioncheck` found `result.json` final with the verdict `not-verified` and the deciding reason `interrupted`, the step's outputs `not-verified` and the results folder, and one masked summary that begins `### rollproof: NOT VERIFIED`; the step ended 120.019 s after it started, 20.0 ms after the last write of `result.json`, far less than 7.5 s after the 2 minutes. That the first signal was SIGINT is `p03-p04-p20-github-runner`'s finding; this log does not name the signal. That record measured this shape on runner 2.338.2, the release here: a step's `timeout-minutes` on a `uses:` step whose inner step `exec`s a program, which got SIGINT and then SIGTERM 7.50 to 7.55 s later. The action's inner step `exec`s the script, and the script `exec`s the tool (`action.sh`, line 147, read).
5. The step after the cut ran with `OUTCOME: failure` (22:18:16.0439444), and the job ended `success`.
6. The masked copy. `actioncheck` reads only `step_summary_*-scrubbed` files under `$RUNNER_TEMP/_runner_file_commands` and fails when the folder is not there or no such file holds the results line; it exited 0 after each of the three runs (each check step `success`).
7. The first mirror run: `setup[10] (docker) exited with code 2   [setup-failed]` in both subtests, and `--- FAIL: TestMirror (132.40s)`.
8. The second mirror run: `--- PASS: TestMirror (426.93s)` and both subtests, at `f0708c6`. The real host's name is resolved by `getent hosts` in a node (`setup[10]`), and the mirror's by the nodes' pulls through it; that the answers came from Docker's DNS is the controller's reading of the cause, not shown on the runner, so the pair does not name it.
9. The suite: `ok  	rollproof/e2e	4528.287s` (00:38:03.4260735) and `ok  	rollproof/e2e	4176.995s` (00:32:20.2586650), each with no `--- FAIL` line, and the control's `--- PASS` lines above. The pair names Docker 29, and Docker's version on `ubuntu-26.04` is not shown (see "Machine and versions"), so the pair as a whole is not measured here.
10. The same suite without the version: the lines of 9; it holds for the Docker that the `ubuntu-26.04` image held, whatever its version.
11. The time: 18 + 76 + 71 = 165 minutes for the suite, 10 for the action's test, from the jobs' first and last log lines; 166 and 10 by the jobs' own times. Both are below the plan's figures.
12. The stop lines: the three blocks of the action's test above show every part but the last clause. That the step reads no command after the script's stop line is not shown, since no run plants a command (see "The tool's lines in the logs of `go test`"). What the logs show is narrower: the tool's own stop lines, workflow commands printed after the script's stop line, stand with their tokens unmasked, so by the runner's source as the plan reads it (`ActionCommandManager.cs`, lines 116 to 121) they were not read as commands. So the pair as a whole is not measured here.
13. The same pair without its last clause: the three blocks of the action's test above.
14. The notice: the run's list of annotations, and the log's line at 22:14:18.4482049.

Plan assumed: "Docker's export of the tool's image names its one file rollproof, with no ./ in front, and the action's script takes the binary out with create, export and tar."
Holds: yes
Plan assumed: "Docker pulls the tool's image by digest from a registry on 127.0.0.1 in the job without TLS and with no setting of the daemon."
Holds: yes
Plan assumed: "The action starts the tool in the workspace, so that a relative config and a relative out are read from there."
Holds: yes
Plan assumed: "A timeout-minutes of 2 on the action's step sends SIGINT while cut.yaml's run is in its steady window or in set-up, and the tool writes its final result.json with the reason interrupted, its outputs and its job summary before SIGTERM, 7.5 s later."
Holds: yes
Plan assumed: "A step that timeout-minutes cut, under continue-on-error, lets the next step run with its outcome failure, and does not fail the job."
Holds: yes
Plan assumed: "The runner keeps the masked copy of the action's inner step's job summary under $RUNNER_TEMP/_runner_file_commands/ with the suffix -scrubbed, and a later step of the job reads it."
Holds: yes
Plan assumed: "The mirror job passes on Docker: the nodes resolve the mirror's container name through Docker's DNS, and the cut of the nodes' route holds."
Holds: no
Plan assumed: "After Task 15 (f0708c6), the mirror job passes on Docker: with the cut's added rule for the destination 127.0.0.0/8, the nodes resolve the stand-ins' container names, and the cut of the nodes' route still holds."
Holds: yes
Plan assumed: "The suite passes under Helm 3 on ubuntu-24.04 and under Helm 4 on ubuntu-26.04 with Docker 29, the fourteenth row's control passing in both jobs."
Holds: not measured here
Plan assumed: "The suite passes under Helm 3 on ubuntu-24.04 and under Helm 4 on ubuntu-26.04, with the Docker that each image holds, the fourteenth row's control passing in both jobs."
Holds: yes
Plan assumed: "One run of rollproof-e2e.yml takes 172 to 205 billed minutes, and one of rollproof-action.yml about 11."
Holds: no
Plan assumed: "In each step that calls the action, the runner's log shows the script's stop line, its token masked, and after it the tool's own stop lines and, unless a signal ended the run, its resume line, which name another token, as plain lines; the step reads no command after the script's stop line."
Holds: not measured here
Plan assumed: "In each step that calls the action, the runner's log shows the script's stop line, its token masked, and after it the tool's own stop lines and, unless a signal ended the run, its resume line, which name another token, as plain lines."
Holds: yes
Plan assumed: "A stop of the runner's workflow commands ends with its step: the notice that the step after the action's first run prints shows as an annotation of the run."
Holds: yes

