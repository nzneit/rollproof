# Point 34: the first runs of the action's test, the mirror job and the suite in rollproof's own repository

## Question

Do the three workflows, `rollproof-action.yml`, `rollproof-mirror.yml` and `rollproof-e2e.yml`, pass in rollproof's own repository, at its root, and what does each show of the beliefs of the plan for its graduation (`docs/plans/2026-11-25-step7/09-public-and-runs.md`, Step 5; the pairs at the end of this record)?

## Machine and versions

Each job ran on a machine of its own, on a hosted runner of the repository, which is public: `gh repo view nzneit/rollproof --json visibility,isPrivate` gave `{"isPrivate":false,"visibility":"PUBLIC"}` on 2026-11-27, read after the runs. Each log begins with `Current runner version: '2.338.2'` and the lines of the image; the provisioner's lines (`Hosted Compute Agent`, `Version: 20261111.635`) are the same in every job.

| Job | Run | Image, as the log names it | Region, cloud |
| --- | --- | --- | --- |
| `action` | 46049598437 | `Ubuntu`, `24.04.5`, `LTS`, `Image: ubuntu-24.04`, `Version: 20261122.23.1` | `us-west-2`, `AWS` |
| `mirror` | 46049608795 | `Ubuntu`, `24.04.5`, `LTS`, `Image: ubuntu-24.04`, `Version: 20261122.23.1` | `centralus`, `Azure` |
| `cluster` | 46050688949 | `Ubuntu`, `24.04.5`, `LTS`, `Image: ubuntu-24.04`, `Version: 20261122.23.1` | `centralus`, `Azure` |
| `helm-3` | 46050688949 | `Ubuntu`, `24.04.5`, `LTS`, `Image: ubuntu-24.04`, `Version: 20261122.23.1` | `us-east-1`, `AWS` |
| `helm-4` | 46050688949 | `Ubuntu`, `26.04.1`, `LTS`, `Image: ubuntu-26.04`, `Version: 20261122.9.1` | `southcentralus`, `Azure` |

- Go 1.27.4 in every job (`go version go1.27.4 linux/amd64`): `actions/setup-go` found it in the runner's tool cache in every job (`Found in cache @ /opt/hostedtoolcache/go/1.27.4/x64`). Its step asks for `go env GOMODCACHE` and `GOCACHE`, with the input `cache-dependency-path: go.sum`, and of the cache it wrote: `action`: `Cache is not found` (18:27:35.7846932); `action`: `Cache saved with the key: setup-go-Linux-x64-ubuntu24-go-1.27.4-4c9482d653f41157cf051fb3842c45ac237a76528fb30a766426eb870964f8ea` (18:34:48.3253693); `mirror`: `Cache is not found` (18:27:35.4488251); `mirror`: `Failed to save: Unable to reserve cache with key setup-go-Linux-x64-ubuntu24-go-1.27.4-4c9482d653f41157cf051fb3842c45ac237a76528fb30a766426eb870964f8ea, another job may be creating this cache.` (18:35:37.1570613); `cluster`: `Cache hit for: setup-go-Linux-x64-ubuntu24-go-1.27.4-4c9482d653f41157cf051fb3842c45ac237a76528fb30a766426eb870964f8ea` (18:36:13.4397836); `cluster`: `Cache Size: ~349 MB (366015784 B)` (18:36:15.4738622); `cluster`: `Cache restored successfully` (18:36:17.4665005); `cluster`: `Cache restored from key: setup-go-Linux-x64-ubuntu24-go-1.27.4-4c9482d653f41157cf051fb3842c45ac237a76528fb30a766426eb870964f8ea` (18:36:17.5769746); `helm-3`: `Cache hit for: setup-go-Linux-x64-ubuntu24-go-1.27.4-4c9482d653f41157cf051fb3842c45ac237a76528fb30a766426eb870964f8ea` (18:48:33.2379522); `helm-3`: `Cache Size: ~349 MB (366015784 B)` (18:48:34.6164970); `helm-3`: `Cache restored successfully` (18:48:35.9646055); `helm-3`: `Cache restored from key: setup-go-Linux-x64-ubuntu24-go-1.27.4-4c9482d653f41157cf051fb3842c45ac237a76528fb30a766426eb870964f8ea` (18:48:35.9764077); `helm-4`: `Cache is not found` (18:48:29.5470157); `helm-4`: `Cache saved with the key: setup-go-Linux-x64-ubuntu26-go-1.27.4-4c9482d653f41157cf051fb3842c45ac237a76528fb30a766426eb870964f8ea` (19:52:17.6582974). By these lines, the `action` job found none and saved one for the `ubuntu-24.04` image; the `mirror` job found none and could not save one for the `ubuntu-24.04` image; the `cluster` job found the cache saved for the `ubuntu-24.04` image; the `helm-3` job found the cache saved for the `ubuntu-24.04` image; the `helm-4` job found none and saved one for the `ubuntu-26.04` image.

- Helm and kubectl, as `go run ./hack/fetchhelm` printed them in the mirror job and both Helm jobs: `helm 3.22.1: fetched to .tools/helm-3/helm`, `helm 4.3.2: fetched to .tools/helm-4/helm`, `kubectl 1.37.0: fetched to .tools/kubectl`. The `action` job does not run `fetchhelm`; the tool there used a Helm found on the runner's PATH (`internal/deploy/helm.go`, line 26, `exec.LookPath("helm")`, read), whose version no log shows.

- kind: the tool links `sigs.k8s.io/kind` v0.33.0 (`go.mod`, read).

- The node image: every run of the tool printed `on Kubernetes v1.37.0 in kind`, 69 runs, except the 1 run of `TestFirstFixtureOnTheOldestNodeImage/helm-3` under v1.34.11 (19:21:31.3871625 in `helm-3`) and the 1 run of `TestFirstFixtureOnTheOldestNodeImage/helm-4` under v1.34.11 (19:23:42.7364578 in `helm-4`).

- Docker. No log of these jobs shows Docker's version. For the `ubuntu-24.04` image, version 20261122.23.1, the image's software list at the tag `ubuntu24/20261122.23`, commit `710e8ff2142ab0ff47974681dba1ae59b2ac4982` of `actions/runner-images`, gives Docker client 28.0.4 and Docker server 28.0.4, and the kernel `7.0.0-1020-azure (AWS:  7.0.0-1018-aws)` (read on 2026-11-27 with `gh api`). For the `ubuntu-26.04` image, version 20261122.9.1, the list at the tag `ubuntu26/20261122.9`, commit `5a6c8c9684029934a7260368a01f4787a4289bc3` of `actions/runner-images`, gives Docker client 29.4.2 and Docker server 29.4.2, and the kernel `7.0.0-1020-azure (AWS:  7.0.0-1018-aws)` (read on 2026-11-27 with `gh api`). No log of these jobs shows whether Docker runs as root.

- The machines' size: no log of these jobs shows a CPU count or the memory. That a public repository's standard runner has 4 CPUs is GitHub's documentation (point 34, "The series' runner"), not read from the runner.

## Method

The workflow files are those of `main` at the commit that each job checked out, as each checkout's lines show: `97219aefa836354621de2430c07542be800d70cc`, fetched as `origin/main` into `Working directory is '/home/runner/work/rollproof/rollproof'`, for all 5 jobs.

Each run was started once, by hand, with one of these commands (each one command with no shell feature), from the clone of the repository:

```
gh workflow run rollproof-action.yml --repo nzneit/rollproof --ref main
gh workflow run rollproof-mirror.yml --repo nzneit/rollproof --ref main
gh workflow run rollproof-e2e.yml --repo nzneit/rollproof --ref main
```

Each run's log was saved with `gh run view ID --log`, and its view and its jobs' with `gh run view ID --json`; the logs are not in this repository.

| Run | Workflow | Commit | Job, ID | First and last log line | Time |
| --- | --- | --- | --- | --- | --- |
| 46049598437 | `rollproof-action.yml` | `97219ae` | `action`, 140677331642 | 18:27:28.3948172 to 18:34:48.4990467 | 440.1 s, 7 min 20.1 s |
| 46049608795 | `rollproof-mirror.yml` | `97219ae` | `mirror`, 140677365941 | 18:27:32.3686849 to 18:35:37.3574590 | 485.0 s, 8 min 5.0 s |
| 46050688949 | `rollproof-e2e.yml` | `97219ae` | `cluster`, 140680945917 | 18:36:10.8596510 to 18:48:21.2807856 | 730.4 s, 12 min 10.4 s |
| 46050688949 | `rollproof-e2e.yml` | `97219ae` | `helm-3`, 140685893394 | 18:48:26.4613034 to 19:49:11.8656682 | 3,645.4 s, 1 h 0 min 45.4 s |
| 46050688949 | `rollproof-e2e.yml` | `97219ae` | `helm-4`, 140685893430 | 18:48:26.5381171 to 19:52:17.8279510 | 3,831.3 s, 1 h 3 min 51.3 s |

All times are of 2026-11-27, in UTC, as the logs stamp them. The suite's two Helm jobs started when its first job had passed and ran side by side, so its run is as long as its first job, the 5.3 s before the longer Helm job `helm-4` began, and that job: 4,567.0 s, 1 h 16 min 7.0 s from the run's first to its last log line (the spans of those two jobs sum to 4,561.7 s). That a public repository's runs cost nothing is the plan's statement (its Task 11, Step 2); no bill was read. A job's log span can be a few seconds shorter than its own time in the jobs' lists (`startedAt` to `completedAt`).

The jobs' steps are those of the three files, read from the root of the repository. In short: the `action` job builds the tool's image and pushes it to a registry on `127.0.0.1:5000` in the job, builds the fixture's two images, and uses the action from the repository's root (`uses: ./`) three times: a pass, a fail under `continue-on-error`, and a run cut short by `timeout-minutes: 2` under `continue-on-error`; each is followed by a step that runs `hack/actioncheck` on the step's outputs, its `result.json` and its job summary in the runner's masked copy; `actioncheck` prints nothing and exits 0 when every check holds (`hack/actioncheck/main.go`, read). The `mirror` job runs `go test -tags mirror -p 1 ./e2e/ -run TestMirror -v -timeout 60m`. The suite's `cluster` job runs the tests under the build tag `cluster` of `./internal/cluster/`, `./internal/images/`, `./internal/probe/`, `./internal/watch/` and `./hack/action/`; `helm-3` and `helm-4` each run `go test -tags e2e -p 1 -count=1 ./e2e/ -v -timeout 120m` with `ROLLPROOF_E2E_HELM` set to `3` or `4` (the three files, read).

## Result

**Outcomes.** The five jobs passed.

### The action's test (run 46049598437)

The three steps that call the action, each with `uses: ./`, the action at the root of the repository, and each config relative to the workspace. In each, the script's stop line stands twice, its token masked; after the engine's pull and the line that names the tool, the tool's own stop line stands twice with a token of 26 characters in plain text; at the end of the pass and of the fail stands the tool's resume line, `::`, its token and `::`. The pass:

```
2026-11-27T18:28:41.7918169Z ##[group]Run ./
2026-11-27T18:28:41.7918929Z   config: hack/action/testdata/pass.yaml
2026-11-27T18:28:41.7919363Z   image: 127.0.0.1:5000/rollproof@sha256:8064c6b9af53dcfc4f2bc8b078518255734b6a3ac06492df78490ca160514ebb
2026-11-27T18:28:41.7919817Z   out: /home/runner/work/_temp/results-pass
2026-11-27T18:28:41.7967694Z ##[start-action display=Run exec bash "$GITHUB_ACTION_PATH/action.sh";id=pass.run]
2026-11-27T18:28:41.8077889Z ::stop-commands::***
2026-11-27T18:28:41.8078908Z ::stop-commands::***
2026-11-27T18:28:41.8392398Z 127.0.0.1:5000/rollproof@sha256:8064c6b9af53dcfc4f2bc8b078518255734b6a3ac06492df78490ca160514ebb: Pulling from rollproof
2026-11-27T18:28:41.8397308Z Digest: sha256:8064c6b9af53dcfc4f2bc8b078518255734b6a3ac06492df78490ca160514ebb
2026-11-27T18:28:41.8398618Z Status: Image is up to date for 127.0.0.1:5000/rollproof@sha256:8064c6b9af53dcfc4f2bc8b078518255734b6a3ac06492df78490ca160514ebb
2026-11-27T18:28:41.8407941Z 127.0.0.1:5000/rollproof@sha256:8064c6b9af53dcfc4f2bc8b078518255734b6a3ac06492df78490ca160514ebb
2026-11-27T18:28:44.2996388Z rollproof: the tool from 127.0.0.1:5000/rollproof@sha256:8064c6b9af53dcfc4f2bc8b078518255734b6a3ac06492df78490ca160514ebb, through docker
2026-11-27T18:28:44.3065184Z ::stop-commands::QB2S35KUVQGAOTFWEA6HXO4TJX
2026-11-27T18:28:44.3065822Z ::stop-commands::QB2S35KUVQGAOTFWEA6HXO4TJX
…
2026-11-27T18:31:02.0903334Z rollproof: PASS   orders (Helm) on Kubernetes v1.37.0 in kind
…
2026-11-27T18:31:02.0924299Z results: /home/runner/work/_temp/results-pass
2026-11-27T18:31:02.7674189Z ::QB2S35KUVQGAOTFWEA6HXO4TJX::
2026-11-27T18:31:02.7726127Z ##[end-action id=pass.run;outcome=success;conclusion=success;duration_ms=140975]
```

The fail:

```
2026-11-27T18:31:06.7245361Z ##[start-action display=Run exec bash "$GITHUB_ACTION_PATH/action.sh";id=fail.run]
2026-11-27T18:31:06.7349179Z ::stop-commands::***
2026-11-27T18:31:06.7349873Z ::stop-commands::***
2026-11-27T18:31:06.7636466Z 127.0.0.1:5000/rollproof@sha256:8064c6b9af53dcfc4f2bc8b078518255734b6a3ac06492df78490ca160514ebb: Pulling from rollproof
2026-11-27T18:31:06.7642025Z Digest: sha256:8064c6b9af53dcfc4f2bc8b078518255734b6a3ac06492df78490ca160514ebb
2026-11-27T18:31:06.7642756Z Status: Image is up to date for 127.0.0.1:5000/rollproof@sha256:8064c6b9af53dcfc4f2bc8b078518255734b6a3ac06492df78490ca160514ebb
2026-11-27T18:31:06.7645721Z 127.0.0.1:5000/rollproof@sha256:8064c6b9af53dcfc4f2bc8b078518255734b6a3ac06492df78490ca160514ebb
2026-11-27T18:31:06.8789004Z rollproof: the tool from 127.0.0.1:5000/rollproof@sha256:8064c6b9af53dcfc4f2bc8b078518255734b6a3ac06492df78490ca160514ebb, through docker
2026-11-27T18:31:06.8859483Z ::stop-commands::U4KQR7DAEF7L4UML2BX2REHJMC
2026-11-27T18:31:06.8860078Z ::stop-commands::U4KQR7DAEF7L4UML2BX2REHJMC
…
2026-11-27T18:32:41.1899528Z rollproof: FAIL   orders (Helm) on Kubernetes v1.37.0 in kind
…
2026-11-27T18:32:41.1922366Z deploy: 922 checks failed (refused 922), first at 18:32:14.34   [probe-failures]
2026-11-27T18:32:41.1924192Z results: /home/runner/work/_temp/results-fail
2026-11-27T18:32:41.8799609Z ::U4KQR7DAEF7L4UML2BX2REHJMC::
2026-11-27T18:32:41.8824570Z ##[error]Process completed with exit code 1.
2026-11-27T18:32:41.8841419Z ##[end-action id=fail.run;outcome=failure;conclusion=failure;duration_ms=95159]
```

The run cut short, which has no resume line:

```
2026-11-27T18:32:41.9333602Z ##[start-action display=Run exec bash "$GITHUB_ACTION_PATH/action.sh";id=cut.run]
2026-11-27T18:32:41.9427325Z ::stop-commands::***
2026-11-27T18:32:41.9427890Z ::stop-commands::***
2026-11-27T18:32:41.9687298Z 127.0.0.1:5000/rollproof@sha256:8064c6b9af53dcfc4f2bc8b078518255734b6a3ac06492df78490ca160514ebb: Pulling from rollproof
2026-11-27T18:32:41.9694331Z Digest: sha256:8064c6b9af53dcfc4f2bc8b078518255734b6a3ac06492df78490ca160514ebb
2026-11-27T18:32:41.9695502Z Status: Image is up to date for 127.0.0.1:5000/rollproof@sha256:8064c6b9af53dcfc4f2bc8b078518255734b6a3ac06492df78490ca160514ebb
2026-11-27T18:32:41.9698446Z 127.0.0.1:5000/rollproof@sha256:8064c6b9af53dcfc4f2bc8b078518255734b6a3ac06492df78490ca160514ebb
2026-11-27T18:32:42.0760719Z rollproof: the tool from 127.0.0.1:5000/rollproof@sha256:8064c6b9af53dcfc4f2bc8b078518255734b6a3ac06492df78490ca160514ebb, through docker
2026-11-27T18:32:42.0828854Z ::stop-commands::EHCA5I6VZ6GFFWGKXQ66HDTOKW
2026-11-27T18:32:42.0829573Z ::stop-commands::EHCA5I6VZ6GFFWGKXQ66HDTOKW
…
2026-11-27T18:34:41.9312416Z rollproof: NOT VERIFIED   orders (Helm) on Kubernetes v1.37.0 in kind
2026-11-27T18:34:41.9326183Z steady     62.8 s   12,200        0              -     40 ms     3 ms   NOT VERIFIED
2026-11-27T18:34:41.9326805Z deploy          -        -        -              -         -        -   not run
2026-11-27T18:34:41.9327241Z rollback        -        -        -              -         -        -   not run
2026-11-27T18:34:41.9327496Z 
2026-11-27T18:34:41.9327659Z steady: a signal stopped the run   [interrupted]
2026-11-27T18:34:41.9328046Z results: /home/runner/work/_temp/results-cut
2026-11-27T18:34:41.9409342Z ##[error]The action has timed out.
2026-11-27T18:34:41.9412601Z ##[end-action id=cut.run;outcome=failure;conclusion=failure;duration_ms=120007]
```

The run cut short's times. The step's first log line is at 18:32:41.9292243 and its `start-action` line at 18:32:41.9333602; the runner's line `The action has timed out.` is at 18:34:41.9409342, 120.012 s after the first and 120.008 s after the second. The step "Check the run cut short" printed first:

```
2026-11-27T18:34:41.9672537Z /home/runner/work/_temp/results-cut/result.json was last written at 2026-11-27 18:34:41.929338046 +0000
2026-11-27T18:34:41.9683763Z total 32
2026-11-27T18:34:41.9684111Z drwxr-xr-x 2 runner runner 4096 2026-11-27 18:33:23.787632977 +0000 commands
2026-11-27T18:34:41.9684598Z -rw-r--r-- 1 runner runner 2144 2026-11-27 18:34:41.934338091 +0000 events.txt
2026-11-27T18:34:41.9685133Z drwxr-xr-x 4 runner runner 4096 2026-11-27 18:33:24.010634979 +0000 pods
2026-11-27T18:34:41.9685583Z -rw-r--r-- 1 runner runner 9457 2026-11-27 18:34:41.165331146 +0000 probe.log
2026-11-27T18:34:41.9686045Z -rw-r--r-- 1 runner runner 2212 2026-11-27 18:34:41.931173879 +0000 timeline.tsv
2026-11-27T18:34:41.9686459Z -rw-r--r-- 1 runner runner  557 2026-11-27 18:34:41.930338055 +0000 windows.json
```

`result.json` was last written at 18:34:41.9293, 11.6 ms before the runner's line that the action had timed out. That moment is the last write of `result.json`, which a later warning's rewrite would move (`internal/run/run.go`, lines 269 to 271 and 1255 to 1261, read), so it is an upper bound for the moment the final file was written. The time stamps of `stat` and `ls` come from the file system and those of the log from the runner; that the two use one clock is not shown. The moment of the signal is not in the log. The evidence files `windows.json`, `timeline.tsv` and `events.txt` were last written 1.0, 1.8 and 5.0 ms after `result.json`.

The check steps. "Check the pass" printed one line, the notice, and `actioncheck` printed nothing; "Check the fail" printed nothing; "Check the run cut short" printed the lines above and then nothing. The step's environment, as the log shows it, held `OUTCOME: failure`, `VERDICT: not-verified` for the run cut short, `OUTCOME: failure`, `VERDICT: fail` for the fail, and `VERDICT: pass` for the pass. The run's list of steps gives `success` for the three steps that called the action and for the three check steps, and `skipped` for the upload of the results (Run actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9), which runs only on a failure (`if: failure()`, `.github/workflows/rollproof-action.yml`, lines 126 to 127, read).

```
2026-11-27T18:31:02.7846738Z ##[notice]workflow commands are read again in the step after the action
```

The run's list of annotations, as `gh run watch` printed it at the run's end, holds three:

```
X The action has timed out.
action: .github#56

X Process completed with exit code 1.
action: .github#59

- workflow commands are read again in the step after the action
action: .github#13
```

The image's pull in the three steps printed `Status: Image is up to date`: the engine already held the image, which the same job had built and pushed.

In the suite's `cluster` job, `TestTheScriptRunsTheToolFromItsImage` (`hack/action/action_cluster_test.go`, lines 20 to 22 and 85 to 100, read) runs the same script on Docker against an image that it wrote to a registry on the loopback itself, with go-containerregistry, and the pull there fetched it:

```
2026-11-27T18:48:19.8489247Z         ::stop-commands::***
2026-11-27T18:48:19.8490156Z         127.0.0.1:36623/rollproof-action-test@sha256:28091c4fe351637561d245d5eeee228e57b431a2e7a5070c9b44a0c608d43a66: Pulling from rollproof-action-test
2026-11-27T18:48:19.8491069Z         Digest: sha256:28091c4fe351637561d245d5eeee228e57b431a2e7a5070c9b44a0c608d43a66
2026-11-27T18:48:19.8492316Z         Status: Downloaded newer image for 127.0.0.1:36623/rollproof-action-test@sha256:28091c4fe351637561d245d5eeee228e57b431a2e7a5070c9b44a0c608d43a66
2026-11-27T18:48:19.8493306Z         127.0.0.1:36623/rollproof-action-test@sha256:28091c4fe351637561d245d5eeee228e57b431a2e7a5070c9b44a0c608d43a66
2026-11-27T18:48:19.8494355Z         rollproof: the tool from 127.0.0.1:36623/rollproof-action-test@sha256:28091c4fe351637561d245d5eeee228e57b431a2e7a5070c9b44a0c608d43a66, through docker
2026-11-27T18:48:19.8495165Z         ::stop-commands::V4366TZC4SEEBTKH7XHOCSCTL2
2026-11-27T18:48:20.0489726Z --- PASS: TestTheScriptRunsTheToolFromItsImage (4.60s)
```

That test starts the script from a working folder of its own, with `GITHUB_WORKSPACE` naming another folder that holds the configuration, and with a relative config (`rejected.yaml`) and a relative out (`results`); it requires `result.json` under the workspace's `results` (`hack/action/action_cluster_test.go`, lines 112 to 113, 127, 141 to 145, 168 and 220, read). Its standard output, as the test logged it:

```
2026-11-27T18:48:19.8486704Z         could not test: version (line 1): this tool reads version 1 only   [config-rejected]
2026-11-27T18:48:19.8487618Z         results: /tmp/TestTheScriptRunsTheToolFromItsImage2131326603/003/workspace/results
```

### The mirror job (run 46049608795)

```
2026-11-27T18:35:34.7022530Z --- PASS: TestMirror (398.10s)
2026-11-27T18:35:34.7023222Z     --- PASS: TestMirror/through_the_mirror (132.09s)
2026-11-27T18:35:34.7023496Z     --- PASS: TestMirror/a_mirror_replaces_its_host (252.47s)
2026-11-27T18:35:34.7023684Z PASS
2026-11-27T18:35:34.7051254Z ok  	github.com/nzneit/rollproof/e2e	455.687s
```

The first subtest's run passed through the mirror (`rollproof: PASS   orders (Helm) on Kubernetes v1.37.0 in kind`, 18:31:21.9679757); the second gave the failure that the test wants:

```
2026-11-27T18:35:34.4459807Z         rollproof: FAIL   orders (Helm) on Kubernetes v1.37.0 in kind
2026-11-27T18:35:34.4467583Z         deploy: did not finish within limits.timeout   [transition-stalled]
2026-11-27T18:35:34.4469430Z           the nodes pull rollproof-mirrorjob-real:5443 only from its mirror
2026-11-27T18:35:34.4469905Z           rollproof-mirrorjob-mirror:8443/real, and a pull's error may name the image alone
```

With both subtests passing, every setup command exited as wanted: the `getent hosts` of the real host's name in a node, which is the eleventh of the list, among them, and the public pull that must fail within 15 s after the cut failed (`e2e/mirror_helpers_test.go`, lines 82 to 109, read). And the test's own checks held in both subtests: that the real host's stand-in saw connections from the loopback only, and that the password is gone from the results (`e2e/mirror_test.go`, lines 39 to 93, read). The stand-ins' logs are not in this run's log: the harness logs a command's evidence when a fixture fails, and none did, as `docs/proofs/p32-step6-workflows.md` records. That the names were resolved through Docker's DNS is not shown on the runner, as that record says of the runs of 2026-11-22.

### The suite (run 46050688949)

The `gh run view --log` output names the step of every line of the `cluster` job `UNKNOWN STEP`, as on 2026-11-22 (`docs/proofs/p32-step6-workflows.md`, read); the run's list of steps names the job's step "The tests under the build tag cluster", `success`. To read this job's set-up, its checkout, `setup-go` and its tests, this record tells its steps apart by the runner's own lines that open them: `##[group]Run actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1` at 18:36:11.8318681, `##[group]Run actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e` at 18:36:13.1166928, `##[group]Run go test -tags cluster -p 1 -count=1 -v -timeout 35m ./internal/cluster/ ./internal/images/ ./internal/probe/ ./internal/watch/ ./hack/action/` at 18:36:17.6882435; the lines before the first of them are the set-up. The saved log is as `gh` wrote it.

The `cluster` job: 796 `--- PASS` lines, and no `--- FAIL` or `--- SKIP` line; its five packages ended `ok`:

```
2026-11-27T18:42:37.3485725Z ok  	github.com/nzneit/rollproof/internal/cluster	303.792s
2026-11-27T18:43:20.7636776Z ok  	github.com/nzneit/rollproof/internal/images	42.013s
2026-11-27T18:45:58.9175069Z ok  	github.com/nzneit/rollproof/internal/probe	155.912s
2026-11-27T18:48:14.2846883Z ok  	github.com/nzneit/rollproof/internal/watch	93.015s
2026-11-27T18:48:20.2539547Z ok  	github.com/nzneit/rollproof/hack/action	4.813s
```

Every test and subtest of `cluster`:

```
2026-11-27T18:38:37.2006494Z --- PASS: TestCreateAndDelete (63.63s)
2026-11-27T18:39:54.4441340Z --- PASS: TestCreateReplacesLeftovers (77.24s)
2026-11-27T18:39:54.6786721Z --- PASS: TestRefusesANameWithoutThePrefix (0.23s)
2026-11-27T18:40:33.1059271Z --- PASS: TestDeleteTwice (38.43s)
2026-11-27T18:41:24.3993620Z --- PASS: TestKeepAfterFailure (51.29s)
2026-11-27T18:42:04.8305742Z --- PASS: TestCreateWritesRegistrySettings (40.43s)
2026-11-27T18:42:30.8440763Z --- PASS: TestAnAbandonedCreateIsRemovedByTheNextRun (26.01s)
2026-11-27T18:42:30.8443127Z --- PASS: TestCreateKindRetries (0.00s)
2026-11-27T18:42:30.8443382Z     --- PASS: TestCreateKindRetries/the_first_attempt_succeeds (0.00s)
2026-11-27T18:42:30.8443678Z     --- PASS: TestCreateKindRetries/a_missed_line_twice,_then_success (0.00s)
2026-11-27T18:42:30.8443971Z     --- PASS: TestCreateKindRetries/a_missed_line_four_times (0.00s)
2026-11-27T18:42:30.8444264Z     --- PASS: TestCreateKindRetries/another_failure_is_not_tried_again (0.00s)
2026-11-27T18:42:30.8444576Z     --- PASS: TestCreateKindRetries/another_failure_after_a_missed_line (0.00s)
2026-11-27T18:42:30.8444846Z     --- PASS: TestCreateKindRetries/ctx_has_ended (0.00s)
2026-11-27T18:42:30.8445131Z     --- PASS: TestCreateKindRetries/the_delete_of_a_failed_attempt_fails (0.00s)
2026-11-27T18:42:32.3919069Z --- PASS: TestWaitReadyCountsItsOwnLimit (1.55s)
2026-11-27T18:42:32.3941694Z --- PASS: TestEnsureNamespaces (0.00s)
2026-11-27T18:42:32.3944255Z --- PASS: TestKept (0.00s)
2026-11-27T18:42:32.3991724Z --- PASS: TestCreatePullsTheNodeImageByTheEnginesName (0.00s)
2026-11-27T18:42:32.3992142Z     --- PASS: TestCreatePullsTheNodeImageByTheEnginesName/podman,_no_node_image (0.00s)
2026-11-27T18:42:32.3992725Z     --- PASS: TestCreatePullsTheNodeImageByTheEnginesName/podman,_a_node_image_with_a_digest (0.00s)
2026-11-27T18:42:32.3993109Z     --- PASS: TestCreatePullsTheNodeImageByTheEnginesName/nerdctl,_no_node_image (0.00s)
2026-11-27T18:42:32.4011567Z --- PASS: TestCreateFailureTexts (0.00s)
2026-11-27T18:42:32.4012231Z     --- PASS: TestCreateFailureTexts/the_clean-up_after_a_failed_step_fails (0.00s)
2026-11-27T18:42:32.4012841Z     --- PASS: TestCreateFailureTexts/a_kept_failure_after_kind's_creation_began (0.00s)
2026-11-27T18:42:32.4013432Z     --- PASS: TestCreateFailureTexts/the_logs_cannot_be_collected,_and_the_cluster_is_kept (0.00s)
2026-11-27T18:42:32.4014194Z     --- PASS: TestCreateFailureTexts/the_delete_of_a_failed_attempt_fails (0.00s)
2026-11-27T18:42:32.4014696Z     --- PASS: TestCreateFailureTexts/the_node_image's_pull_fails (0.00s)
2026-11-27T18:42:32.4015161Z     --- PASS: TestCreateFailureTexts/two_removals_of_leftovers_fail (0.00s)
2026-11-27T18:42:32.4019863Z --- PASS: TestCreateWritesTheCredentialsFileBeforeKind (0.00s)
2026-11-27T18:42:32.4020440Z     --- PASS: TestCreateWritesTheCredentialsFileBeforeKind/an_entry_with_credentials (0.00s)
2026-11-27T18:42:32.4021070Z     --- PASS: TestCreateWritesTheCredentialsFileBeforeKind/no_entry_with_credentials (0.00s)
2026-11-27T18:42:32.4023525Z --- PASS: TestOneLine (0.00s)
2026-11-27T18:42:32.4023808Z     --- PASS: TestOneLine/nothing_said (0.00s)
2026-11-27T18:42:32.4024083Z     --- PASS: TestOneLine/one_line_with_a_line_end (0.00s)
2026-11-27T18:42:32.4024291Z     --- PASS: TestOneLine/two_lines (0.00s)
2026-11-27T18:42:32.4024748Z --- PASS: TestCredentialsGolden (0.00s)
2026-11-27T18:42:32.4029759Z --- PASS: TestPullIfMissingKeepsAHeldImage (0.00s)
2026-11-27T18:42:32.4043352Z --- PASS: TestPullIfMissingPullsAMissingImage (0.00s)
2026-11-27T18:42:32.7057261Z --- PASS: TestPullIfMissingCutsAPullAtItsLimit (0.30s)
2026-11-27T18:42:32.7071612Z --- PASS: TestPullIfMissingGivesTheEnginesError (0.00s)
2026-11-27T18:42:32.7080159Z --- PASS: TestEngineLogsGivesTheOutput (0.00s)
2026-11-27T18:42:32.7094734Z --- PASS: TestEngineLogsWithNoContainer (0.00s)
2026-11-27T18:42:32.7108469Z --- PASS: TestEngineLogsGivesTheEnginesErrorWhenTheContainerExists (0.00s)
2026-11-27T18:42:32.7109223Z --- PASS: TestNodeImageNameUnderEachEngine (0.00s)
2026-11-27T18:42:32.7125058Z --- PASS: TestNodeImageAsksTheEngineByItsName (0.00s)
2026-11-27T18:42:32.7128809Z --- PASS: TestHostsTOMLGolden (0.00s)
2026-11-27T18:42:32.7132481Z --- PASS: TestHostSettingsGolden (0.00s)
2026-11-27T18:42:32.7132781Z     --- PASS: TestHostSettingsGolden/hosts-as-mirror-ca-credentials.golden.toml (0.00s)
2026-11-27T18:42:32.7133122Z     --- PASS: TestHostSettingsGolden/hosts-mirror-prefix.golden.toml (0.00s)
2026-11-27T18:42:32.7133421Z     --- PASS: TestHostSettingsGolden/hosts-mirror-v2.golden.toml (0.00s)
2026-11-27T18:42:32.7133725Z     --- PASS: TestHostSettingsGolden/hosts-mirror-v2-only.golden.toml (0.00s)
2026-11-27T18:42:32.7134047Z     --- PASS: TestHostSettingsGolden/hosts-mirror-prefix-v2.golden.toml (0.00s)
2026-11-27T18:42:32.7134352Z     --- PASS: TestHostSettingsGolden/hosts-mirror-root.golden.toml (0.00s)
2026-11-27T18:42:32.7134661Z     --- PASS: TestHostSettingsGolden/hosts-docker-io-mirror.golden.toml (0.00s)
2026-11-27T18:42:32.7134974Z     --- PASS: TestHostSettingsGolden/hosts-docker-io-ca.golden.toml (0.00s)
2026-11-27T18:42:32.7135252Z     --- PASS: TestHostSettingsGolden/hosts-ca.golden.toml (0.00s)
2026-11-27T18:42:32.7135518Z     --- PASS: TestHostSettingsGolden/hosts-as-ca.golden.toml (0.00s)
2026-11-27T18:42:32.7135802Z     --- PASS: TestHostSettingsGolden/hosts-as-port-ca.golden.toml (0.00s)
2026-11-27T18:42:32.7136099Z     --- PASS: TestHostSettingsGolden/hosts-ipv6-port-ca.golden.toml (0.00s)
2026-11-27T18:42:32.7136482Z --- PASS: TestHostSettingsNeedsNoFile (0.00s)
2026-11-27T18:42:32.7136874Z --- PASS: TestHostSettingsOfAnAsHostWithNoEntry (0.00s)
2026-11-27T18:42:32.7137317Z --- PASS: TestHostSettingsTakesNothingFromAnotherEntry (0.00s)
2026-11-27T18:42:32.8633799Z --- PASS: TestUntilDone (0.15s)
2026-11-27T18:42:32.8634050Z     --- PASS: TestUntilDone/the_call_returns_first (0.00s)
2026-11-27T18:42:32.8634461Z     --- PASS: TestUntilDone/a_context_that_has_ended (0.00s)
2026-11-27T18:42:32.8634896Z     --- PASS: TestUntilDone/the_context_ends_during_the_call (0.05s)
2026-11-27T18:42:32.8635165Z     --- PASS: TestUntilDone/a_panic_of_a_call_that_returns_first (0.00s)
2026-11-27T18:42:32.8635482Z     --- PASS: TestUntilDone/a_panic_after_the_context_won (0.10s)
2026-11-27T18:42:32.8658196Z --- PASS: TestASignalDuringEachCallIntoKind (0.00s)
2026-11-27T18:42:32.8668217Z     --- PASS: TestASignalDuringEachCallIntoKind/the_removal_of_an_earlier_run's_cluster (0.00s)
2026-11-27T18:42:32.8668872Z     --- PASS: TestASignalDuringEachCallIntoKind/kind's_creation (0.00s)
2026-11-27T18:42:32.8669185Z     --- PASS: TestASignalDuringEachCallIntoKind/the_delete_of_a_failed_attempt (0.00s)
2026-11-27T18:42:32.8669543Z     --- PASS: TestASignalDuringEachCallIntoKind/the_log_collection_of_a_failed_set-up (0.00s)
2026-11-27T18:42:32.8669930Z     --- PASS: TestASignalDuringEachCallIntoKind/the_removal_of_a_cluster_whose_set-up_failed (0.00s)
2026-11-27T18:42:32.8670256Z     --- PASS: TestASignalDuringEachCallIntoKind/the_node_list (0.00s)
2026-11-27T18:42:32.8670529Z     --- PASS: TestASignalDuringEachCallIntoKind/a_node's_file (0.00s)
2026-11-27T18:42:32.8670801Z     --- PASS: TestASignalDuringEachCallIntoKind/a_node's_role (0.00s)
2026-11-27T18:42:32.8671959Z --- PASS: TestWriteNodeSettingsFailures (0.00s)
2026-11-27T18:42:32.8672218Z     --- PASS: TestWriteNodeSettingsFailures/a_ca_that_cannot_be_read (0.00s)
2026-11-27T18:42:32.8672542Z     --- PASS: TestWriteNodeSettingsFailures/a_node's_file_that_cannot_be_written (0.00s)
2026-11-27T18:42:32.8672851Z     --- PASS: TestWriteNodeSettingsFailures/the_nodes_in_order (0.00s)
2026-11-27T18:42:32.8673763Z --- PASS: TestKindConfigGolden (0.00s)
2026-11-27T18:42:32.8673993Z     --- PASS: TestKindConfigGolden/kind-rootless.golden.yaml (0.00s)
2026-11-27T18:42:32.8674285Z     --- PASS: TestKindConfigGolden/kind-rootful.golden.yaml (0.00s)
2026-11-27T18:42:32.8675328Z --- PASS: TestKindConfigMountsTheCredentials (0.00s)
2026-11-27T18:42:32.8675616Z     --- PASS: TestKindConfigMountsTheCredentials/kind-rootless.golden.yaml (0.00s)
2026-11-27T18:42:32.8675949Z     --- PASS: TestKindConfigMountsTheCredentials/kind-rootful.golden.yaml (0.00s)
2026-11-27T18:42:32.8676320Z --- PASS: TestKindConfigShape (0.00s)
2026-11-27T18:42:32.8676754Z --- PASS: TestCollectLogsCopiesEveryFile (0.00s)
2026-11-27T18:42:32.8677092Z --- PASS: TestCopyLogsClosesEachFile (0.00s)
2026-11-27T18:42:32.8677718Z --- PASS: TestCollectLogsWithNoRegistry (0.00s)
2026-11-27T18:42:32.8680300Z --- PASS: TestCollectLogsGoesOnAfterAnError (0.00s)
2026-11-27T18:42:32.8681755Z --- PASS: TestCollectLogsWithNoEvidence (0.00s)
2026-11-27T18:42:32.8682228Z --- PASS: TestContractNames (0.00s)
2026-11-27T18:42:32.8686997Z --- PASS: TestNameFromPath (0.00s)
2026-11-27T18:42:32.8688837Z --- PASS: TestNameGiven (0.00s)
2026-11-27T18:42:32.8690246Z --- PASS: TestNameMissingFile (0.00s)
2026-11-27T18:42:32.8690655Z --- PASS: TestRunDir (0.00s)
2026-11-27T18:42:32.8692510Z --- PASS: TestPodmanNameOnKindsCases (0.00s)
2026-11-27T18:42:32.8693220Z --- PASS: TestPodmanNameOfTheSuitesNodeImages (0.00s)
2026-11-27T18:42:33.1709275Z --- PASS: TestStartRegistryCountsItsOwnLimit (0.30s)
2026-11-27T18:42:33.8736033Z --- PASS: TestStartRegistryCountsItsWaitFromTheContainersStart (0.70s)
2026-11-27T18:42:34.1750217Z --- PASS: TestStartRegistryCutsTheImagesPullAtItsLimit (0.30s)
2026-11-27T18:42:36.2770459Z --- PASS: TestWaitForRegistryReportsTheLastAnswer (2.10s)
2026-11-27T18:42:36.2771207Z     --- PASS: TestWaitForRegistryReportsTheLastAnswer/a_503,_then_requests_that_hang_past_the_deadline (0.70s)
2026-11-27T18:42:36.2771795Z     --- PASS: TestWaitForRegistryReportsTheLastAnswer/connections_refused (0.70s)
2026-11-27T18:42:36.2772158Z     --- PASS: TestWaitForRegistryReportsTheLastAnswer/no_answer_at_all (0.70s)
2026-11-27T18:42:36.2779017Z --- PASS: TestRemoveRefuses (0.00s)
2026-11-27T18:42:36.2779371Z     --- PASS: TestRemoveRefuses/a_name_without_the_prefix (0.00s)
2026-11-27T18:42:36.2779840Z     --- PASS: TestRemoveRefuses/a_folder_not_named_after_the_cluster (0.00s)
2026-11-27T18:42:36.2788265Z --- PASS: TestCheckRunner (0.00s)
2026-11-27T18:42:36.2788593Z     --- PASS: TestCheckRunner/nerdctl,_rootless (0.00s)
2026-11-27T18:42:36.2788821Z     --- PASS: TestCheckRunner/docker_before_nerdctl (0.00s)
2026-11-27T18:42:36.2789035Z     --- PASS: TestCheckRunner/podman_last (0.00s)
2026-11-27T18:42:36.2789425Z     --- PASS: TestCheckRunner/a_docker_command_that_is_podman_does_not_count_as_docker (0.00s)
2026-11-27T18:42:36.2789896Z     --- PASS: TestCheckRunner/the_first_engine_found_has_no_working_info (0.00s)
2026-11-27T18:42:36.2790245Z     --- PASS: TestCheckRunner/the_variable_picks_podman (0.00s)
2026-11-27T18:42:36.2790567Z     --- PASS: TestCheckRunner/the_variable_picks_finch (0.00s)
2026-11-27T18:42:36.2790988Z     --- PASS: TestCheckRunner/the_variable_picks_nerdctl.lima (0.00s)
2026-11-27T18:42:36.2791490Z     --- PASS: TestCheckRunner/the_variable_picks_nerdctl_and_its_info_fails (0.00s)
2026-11-27T18:42:36.2791775Z     --- PASS: TestCheckRunner/an_unknown_value_is_ignored (0.00s)
2026-11-27T18:42:36.2791994Z     --- PASS: TestCheckRunner/no_engine (0.00s)
2026-11-27T18:42:36.2792245Z     --- PASS: TestCheckRunner/info_that_is_not_JSON_counts_as_failed (0.00s)
2026-11-27T18:42:36.2792587Z     --- PASS: TestCheckRunner/DOCKER_HOST_over_tcp (0.00s)
2026-11-27T18:42:36.2792817Z     --- PASS: TestCheckRunner/CONTAINER_HOST_over_ssh (0.00s)
2026-11-27T18:42:36.2793109Z     --- PASS: TestCheckRunner/DOCKER_HOST_on_a_local_socket (0.00s)
2026-11-27T18:42:36.2793381Z     --- PASS: TestCheckRunner/inside_Docker (0.00s)
2026-11-27T18:42:36.2793588Z     --- PASS: TestCheckRunner/inside_Podman (0.00s)
2026-11-27T18:42:36.2793813Z     --- PASS: TestCheckRunner/a_binary_that_is_not_static (0.00s)
2026-11-27T18:42:36.2794662Z --- PASS: TestNetwork (0.00s)
2026-11-27T18:42:36.2794873Z     --- PASS: TestNetwork/docker (0.00s)
2026-11-27T18:42:36.2795053Z     --- PASS: TestNetwork/podman (0.00s)
2026-11-27T18:42:36.2795228Z     --- PASS: TestNetwork/nerdctl (0.00s)
2026-11-27T18:42:36.2795614Z --- PASS: TestRootlessFromInfo (0.00s)
2026-11-27T18:42:37.3452351Z --- PASS: TestIsStatic (1.07s)
2026-11-27T18:42:37.3454417Z --- PASS: TestDefaults (0.00s)
2026-11-27T18:42:38.7646465Z --- PASS: TestStoreArchivePushesByDigest (0.01s)
2026-11-27T18:42:38.7646971Z --- PASS: TestPlaceOf (0.00s)
2026-11-27T18:42:38.7695652Z --- PASS: TestStoreArchiveUnderAs (0.00s)
2026-11-27T18:42:38.7707099Z --- PASS: TestStoreArchiveFailures (0.00s)
2026-11-27T18:42:38.7707551Z     --- PASS: TestStoreArchiveFailures/two_images (0.00s)
2026-11-27T18:42:38.7707917Z     --- PASS: TestStoreArchiveFailures/not_a_tar (0.00s)
2026-11-27T18:43:16.6925113Z --- PASS: TestStoreOwnAndLocal (37.92s)
2026-11-27T18:43:17.1577907Z --- PASS: TestStoreArchiveIntoEveryNode (0.47s)
2026-11-27T18:43:17.8670395Z --- PASS: TestStoreAsIntoEveryNode (0.71s)
2026-11-27T18:43:18.0264697Z --- PASS: TestStoreUnknownLocalImage (0.16s)
2026-11-27T18:43:18.0287608Z --- PASS: TestOwnImage (0.00s)
2026-11-27T18:43:18.0789066Z --- PASS: TestNodeThatCannotPull (0.05s)
2026-11-27T18:43:23.0149274Z --- PASS: TestCustomEnv (0.00s)
2026-11-27T18:43:23.0162050Z --- PASS: TestCustomEnvRefuses (0.00s)
2026-11-27T18:43:23.0162431Z     --- PASS: TestCustomEnvRefuses/a_misspelt_source (0.00s)
2026-11-27T18:43:23.0162885Z     --- PASS: TestCustomEnvRefuses/a_source_in_another_case (0.00s)
2026-11-27T18:43:23.0163328Z     --- PASS: TestCustomEnvRefuses/a_misspelt_key_inside (0.00s)
2026-11-27T18:43:23.0163766Z     --- PASS: TestCustomEnvRefuses/a_value_of_another_type (0.00s)
2026-11-27T18:43:23.0164168Z     --- PASS: TestCustomEnvRefuses/two_sources (0.00s)
2026-11-27T18:43:23.0164550Z     --- PASS: TestCustomEnvRefuses/no_source (0.00s)
2026-11-27T18:43:23.0165029Z --- PASS: TestFollowLineKinds (0.00s)
2026-11-27T18:43:23.0165388Z --- PASS: TestFollowFailedCheckAndFloor (0.00s)
2026-11-27T18:43:23.0167798Z --- PASS: TestFollowLongLine (0.00s)
2026-11-27T18:43:23.0168562Z --- PASS: TestFollowReopensWithoutLossOrRepeat (0.00s)
2026-11-27T18:43:23.0170242Z --- PASS: TestFollowReopensWithStampsOutOfOrder (0.00s)
2026-11-27T18:43:23.0170794Z     --- PASS: TestFollowReopensWithStampsOutOfOrder/a_line_stamped_before_LAST_after_the_lines_at_LAST_is_taken (0.00s)
2026-11-27T18:43:23.0171379Z     --- PASS: TestFollowReopensWithStampsOutOfOrder/a_pair_out_of_order_before_the_break_gives_a_gap (0.00s)
2026-11-27T18:43:23.0375623Z --- PASS: TestFollowRetriesAnOpeningThatFails (0.02s)
2026-11-27T18:43:23.0378780Z --- PASS: TestFollowGap (0.00s)
2026-11-27T18:43:23.0379312Z     --- PASS: TestFollowGap/the_first_line_after_the_break_is_later_than_the_last_line_read (0.00s)
2026-11-27T18:43:23.0379972Z     --- PASS: TestFollowGap/fewer_lines_of_the_last_timestamp_came_back_than_were_read (0.00s)
2026-11-27T18:43:23.0380681Z     --- PASS: TestFollowGap/the_follower_lags_behind_the_log:_LAST,_the_first_line_after_and_now_fall_far_apart (0.00s)
2026-11-27T18:43:23.0381415Z     --- PASS: TestFollowGap/the_first_line_after_the_gap_is_read_at_the_moment_it_was_written (0.00s)
2026-11-27T18:43:23.0382782Z --- PASS: TestFollowLastPiece (0.00s)
2026-11-27T18:43:23.0383183Z     --- PASS: TestFollowLastPiece/before_Stop_it_is_a_line (0.00s)
2026-11-27T18:43:23.0383598Z     --- PASS: TestFollowLastPiece/after_Stop_it_is_kept_and_not_judged (0.00s)
2026-11-27T18:43:23.0384148Z --- PASS: TestFollowLineWithoutTimestamp (0.00s)
2026-11-27T18:43:23.0385381Z --- PASS: TestFollowDropsAPieceWhenTheLogIsOpenedAgain (0.00s)
2026-11-27T18:43:23.0385730Z     --- PASS: TestFollowDropsAPieceWhenTheLogIsOpenedAgain/the_stream_ends_with_io.EOF (0.00s)
2026-11-27T18:43:23.0386104Z     --- PASS: TestFollowDropsAPieceWhenTheLogIsOpenedAgain/the_stream_breaks (0.00s)
2026-11-27T18:43:23.0389396Z --- PASS: TestFollowGapAtTheEnd (0.00s)
2026-11-27T18:43:23.0389673Z     --- PASS: TestFollowGapAtTheEnd/a_stream_breaks_and_the_container_has_ended (0.00s)
2026-11-27T18:43:23.0390053Z     --- PASS: TestFollowGapAtTheEnd/a_stream_breaks_before_its_first_line:_there_is_no_LAST (0.00s)
2026-11-27T18:43:23.0390437Z     --- PASS: TestFollowGapAtTheEnd/a_stream_breaks_and_the_log_cannot_be_opened_again (0.00s)
2026-11-27T18:43:23.0390868Z     --- PASS: TestFollowGapAtTheEnd/a_stream_breaks_at_the_moment_of_its_last_line:_LAST_and_now_are_one_moment (0.00s)
2026-11-27T18:43:23.0391334Z     --- PASS: TestFollowGapAtTheEnd/a_stream_opened_again_ends_before_the_lines_of_the_last_timestamp_came_back (0.00s)
2026-11-27T18:43:23.0391801Z     --- PASS: TestFollowGapAtTheEnd/a_stream_opened_again_breaks_inside_a_repeat_and_the_container_has_ended (0.00s)
2026-11-27T18:43:23.0392229Z     --- PASS: TestFollowGapAtTheEnd/a_stream_ends_with_io.EOF_and_the_log_cannot_be_opened_again (0.00s)
2026-11-27T18:43:23.0392649Z     --- PASS: TestFollowGapAtTheEnd/a_stream_opened_again_gives_back_every_line_of_the_last_timestamp (0.00s)
2026-11-27T18:43:23.0393307Z --- PASS: TestFollowGapWhenTheContextEndsBeforeAReopen (0.00s)
2026-11-27T18:43:23.0393807Z --- PASS: TestFollowGapWhenACleanStreamCannotBeOpenedAgain (0.00s)
2026-11-27T18:43:23.0394210Z --- PASS: TestFollowGoesOnWhenRawFails (0.00s)
2026-11-27T18:43:23.0586961Z --- PASS: TestFollowAfterStopTakesTheEndWhenTheContainerEnds (0.02s)
2026-11-27T18:43:23.3598656Z --- PASS: TestFollowAfterStopTheContainerDoesNotEnd (0.30s)
2026-11-27T18:43:23.3807777Z --- PASS: TestFollowAfterStopABrokenStream (0.02s)
2026-11-27T18:43:23.3809200Z --- PASS: TestFollowAfterStopTheStopCutsTheLog (0.00s)
2026-11-27T18:43:23.3809698Z     --- PASS: TestFollowAfterStopTheStopCutsTheLog/after_the_stop_was_asked (0.00s)
2026-11-27T18:43:23.3810028Z     --- PASS: TestFollowAfterStopTheStopCutsTheLog/a_start_that_ended (0.00s)
2026-11-27T18:43:24.6825018Z --- PASS: TestRunHTTPOpenModel (1.30s)
2026-11-27T18:43:26.1839266Z --- PASS: TestRunHTTPLag (1.50s)
2026-11-27T18:43:26.6843977Z --- PASS: TestRunHTTPNewConnectionForEachRequest (0.50s)
2026-11-27T18:43:29.6925484Z --- PASS: TestRunHTTPFailureClasses (3.01s)
2026-11-27T18:43:29.6925949Z     --- PASS: TestRunHTTPFailureClasses/a_status_outside_the_range (0.30s)
2026-11-27T18:43:29.6926503Z     --- PASS: TestRunHTTPFailureClasses/a_redirect_is_judged_and_not_followed (0.30s)
2026-11-27T18:43:29.6926835Z     --- PASS: TestRunHTTPFailureClasses/connection_refused (0.30s)
2026-11-27T18:43:29.6927107Z     --- PASS: TestRunHTTPFailureClasses/connection_reset (0.30s)
2026-11-27T18:43:29.6927641Z     --- PASS: TestRunHTTPFailureClasses/a_body_cut_short_after_the_headers (0.30s)
2026-11-27T18:43:29.6927970Z     --- PASS: TestRunHTTPFailureClasses/no_answer_within_the_timeout (0.60s)
2026-11-27T18:43:29.6928328Z     --- PASS: TestRunHTTPFailureClasses/headers,_then_a_body_that_stalls_past_the_timeout (0.60s)
2026-11-27T18:43:29.6928666Z     --- PASS: TestRunHTTPFailureClasses/an_answer_that_is_not_HTTP (0.30s)
2026-11-27T18:43:32.2138718Z --- PASS: TestRunHTTPSummaryLines (2.52s)
2026-11-27T18:43:32.7145202Z --- PASS: TestRunHTTPHeadersAndHost (0.50s)
2026-11-27T18:43:33.2168245Z --- PASS: TestRunHTTPSWithoutCertificateCheck (0.50s)
2026-11-27T18:43:34.2185611Z --- PASS: TestRunHTTPResolvesOnce (1.00s)
2026-11-27T18:43:35.0195977Z --- PASS: TestRunHTTPKeepsTheFirstAddress (0.80s)
2026-11-27T18:43:36.2017127Z --- PASS: TestRunHTTPDrainsOnCancel (1.18s)
2026-11-27T18:43:39.8025194Z --- PASS: TestRunHTTPNeverSilentWhileItWorks (3.60s)
2026-11-27T18:43:40.1029855Z --- PASS: TestRunHTTPSendsQueryAndPort (0.30s)
2026-11-27T18:43:43.6036274Z --- PASS: TestRunHTTPScheduleSurvivesABlockedWrite (3.50s)
2026-11-27T18:43:44.0045511Z --- PASS: TestRunHTTPFailureLineBeginsWhenTheRequestBegan (0.40s)
2026-11-27T18:43:44.0046193Z --- PASS: TestRunHTTPBadURL (0.00s)
2026-11-27T18:43:45.0051779Z --- PASS: TestRunHTTPReportsFromItsFirstRequest (1.00s)
2026-11-27T18:43:46.0063266Z --- PASS: TestRunHTTPHostResolvesLater (1.00s)
2026-11-27T18:43:47.0070145Z --- PASS: TestRunHTTPHostNeverResolves (1.00s)
2026-11-27T18:43:47.3073696Z --- PASS: TestRunHTTPLookupTimeoutIsOther (0.30s)
2026-11-27T18:43:48.5885386Z --- PASS: TestRunHTTPLookupsThatHang (1.28s)
2026-11-27T18:43:48.5885985Z --- PASS: TestHTTPArgs (0.00s)
2026-11-27T18:43:48.5886465Z --- PASS: TestHTTPArgsRoundTrip (0.00s)
2026-11-27T18:43:48.5886970Z --- PASS: TestParseHTTPArgsRejects (0.00s)
2026-11-27T18:43:48.5924991Z --- PASS: TestParseLine (0.00s)
2026-11-27T18:43:48.5925275Z     --- PASS: TestParseLine/{"ok":true} (0.00s)
2026-11-27T18:43:48.5925709Z     --- PASS: TestParseLine/{"ok":false,"class":"refused","ms":12.5,"lag":3} (0.00s)
2026-11-27T18:43:48.5926136Z     --- PASS: TestParseLine/___{"ok":true} (0.00s)
2026-11-27T18:43:48.5926465Z     --- PASS: TestParseLine/_{"ok":true}___ (0.00s)
2026-11-27T18:43:48.5926785Z     --- PASS: TestParseLine/{"ok":true}_ (0.00s)
2026-11-27T18:43:48.5927285Z     --- PASS: TestParseLine/{"ok":true,"n":200,"t":"2026-11-13T10:00:07.120Z","ms":41,"lag":6} (0.00s)
2026-11-27T18:43:48.5927937Z     --- PASS: TestParseLine/{"ok":true,"t":"2026-11-13T11:00:07.12+01:00"} (0.00s)
2026-11-27T18:43:48.5928416Z     --- PASS: TestParseLine/{"ok":true,"t":"2026-11-13T10:00:07.5Z"} (0.00s)
2026-11-27T18:43:48.5928859Z     --- PASS: TestParseLine/{"ok":true,"ms":0,"lag":0} (0.00s)
2026-11-27T18:43:48.5929200Z     --- PASS: TestParseLine/starting (0.00s)
2026-11-27T18:43:48.5929555Z     --- PASS: TestParseLine/2026/11/13_10:00:00_listening (0.00s)
2026-11-27T18:43:48.5929898Z     --- PASS: TestParseLine/__ok:_ready (0.00s)
2026-11-27T18:43:48.5930260Z     --- PASS: TestParseLine/the_word_"ok"_and_no_colon (0.00s)
2026-11-27T18:43:48.5930703Z     --- PASS: TestParseLine/#00 (0.00s)
2026-11-27T18:43:48.5930979Z     --- PASS: TestParseLine/___ (0.00s)
2026-11-27T18:43:48.5931328Z     --- PASS: TestParseLine/2026-11-13_10:00:07_{"ok":false} (0.00s)
2026-11-27T18:43:48.5931698Z     --- PASS: TestParseLine/INFO_"ok"_:_true (0.00s)
2026-11-27T18:43:48.5932055Z     --- PASS: TestParseLine/level=info_"ok"_:false (0.00s)
2026-11-27T18:43:48.5932387Z     --- PASS: TestParseLine/{"ok":tru (0.00s)
2026-11-27T18:43:48.5932731Z     --- PASS: TestParseLine/{"ok":true}_trailing (0.00s)
2026-11-27T18:43:48.5933099Z     --- PASS: TestParseLine/{"ok":true}{"ok":true} (0.00s)
2026-11-27T18:43:48.5933421Z     --- PASS: TestParseLine/{ (0.00s)
2026-11-27T18:43:48.5933765Z     --- PASS: TestParseLine/{"level":"info","msg":"x"} (0.00s)
2026-11-27T18:43:48.5934141Z     --- PASS: TestParseLine/{"detail":{"ok":true}} (0.00s)
2026-11-27T18:43:48.5934514Z     --- PASS: TestParseLine/{"msg":"the_\"ok\":_field"} (0.00s)
2026-11-27T18:43:48.5934847Z     --- PASS: TestParseLine/{} (0.00s)
2026-11-27T18:43:48.5935141Z     --- PASS: TestParseLine/{"ok":"yes"} (0.00s)
2026-11-27T18:43:48.5935442Z     --- PASS: TestParseLine/{"ok":1} (0.00s)
2026-11-27T18:43:48.5935745Z     --- PASS: TestParseLine/{"ok":null} (0.00s)
2026-11-27T18:43:48.5936084Z     --- PASS: TestParseLine/{"ok":"S3CRET-VALUE"} (0.00s)
2026-11-27T18:43:48.5936548Z     --- PASS: TestParseLine/{"ok":true,"n":0} (0.00s)
2026-11-27T18:43:48.5936886Z     --- PASS: TestParseLine/{"ok":true,"n":-1} (0.00s)
2026-11-27T18:43:48.5937224Z     --- PASS: TestParseLine/{"ok":true,"n":1.5} (0.00s)
2026-11-27T18:43:48.5937797Z     --- PASS: TestParseLine/{"ok":true,"n":"3"} (0.00s)
2026-11-27T18:43:48.5938147Z     --- PASS: TestParseLine/{"ok":true,"n":null} (0.00s)
2026-11-27T18:43:48.5938561Z     --- PASS: TestParseLine/{"ok":false,"n":1} (0.00s)
2026-11-27T18:43:48.5938896Z     --- PASS: TestParseLine/{"ok":false,"n":5} (0.00s)
2026-11-27T18:43:48.5939231Z     --- PASS: TestParseLine/{"ok":true,"t":12} (0.00s)
2026-11-27T18:43:48.5939603Z     --- PASS: TestParseLine/{"ok":true,"t":"yesterday"} (0.00s)
2026-11-27T18:43:48.5940018Z     --- PASS: TestParseLine/{"ok":true,"t":"2026-11-13T10:00:07"} (0.00s)
2026-11-27T18:43:48.5940484Z     --- PASS: TestParseLine/{"ok":true,"t":"2026-11-13T10:00:07.500000001Z"} (0.00s)
2026-11-27T18:43:48.5940973Z     --- PASS: TestParseLine/{"ok":false,"t":"2026-11-14T10:00:00Z"} (0.00s)
2026-11-27T18:43:48.5941388Z     --- PASS: TestParseLine/{"ok":false,"class":5} (0.00s)
2026-11-27T18:43:48.5941737Z     --- PASS: TestParseLine/{"ok":true,"ms":-1} (0.00s)
2026-11-27T18:43:48.5942081Z     --- PASS: TestParseLine/{"ok":true,"ms":"41"} (0.00s)
2026-11-27T18:43:48.5942429Z     --- PASS: TestParseLine/{"ok":true,"ms":null} (0.00s)
2026-11-27T18:43:48.5942784Z     --- PASS: TestParseLine/{"ok":true,"lag":-0.5} (0.00s)
2026-11-27T18:43:48.5943138Z     --- PASS: TestParseLine/{"ok":true,"lag":true} (0.00s)
2026-11-27T18:43:48.5943513Z     --- PASS: TestParseLine/{"ok":true,"detail":"text"} (0.00s)
2026-11-27T18:43:48.5943943Z     --- PASS: TestParseLine/{"ok":true,"detail":[1,{"ok":"x"}]} (0.00s)
2026-11-27T18:43:48.5944355Z     --- PASS: TestParseLine/{"ok":true,"detail":null} (0.00s)
2026-11-27T18:43:48.5944716Z     --- PASS: TestParseLine/{"ok":true,"v":1} (0.00s)
2026-11-27T18:43:48.5945044Z     --- PASS: TestParseLine/{"ok":true,"v":2} (0.00s)
2026-11-27T18:43:48.5945388Z     --- PASS: TestParseLine/{"ok":true,"v":"1"} (0.00s)
2026-11-27T18:43:48.5945726Z     --- PASS: TestParseLine/{"ok":true,"v":1.5} (0.00s)
2026-11-27T18:43:48.5946068Z     --- PASS: TestParseLine/{"ok":true,"extra":1} (0.00s)
2026-11-27T18:43:48.5946423Z     --- PASS: TestParseLine/{"ok":false,"ok":true} (0.00s)
2026-11-27T18:43:48.5946825Z     --- PASS: TestParseLine/{"ok":true,"class":"a","class":"b"} (0.00s)
2026-11-27T18:43:48.5947253Z     --- PASS: TestParseLine/{"ok":true,"detail":{"a":1,"a":2}} (0.00s)
2026-11-27T18:43:48.5949997Z --- PASS: TestParseLineFloorOfT (0.00s)
2026-11-27T18:43:48.5950334Z     --- PASS: TestParseLineFloorOfT/after_the_ask (0.00s)
2026-11-27T18:43:48.5950774Z     --- PASS: TestParseLineFloorOfT/less_than_the_bound_before_the_ask (0.00s)
2026-11-27T18:43:48.5951245Z     --- PASS: TestParseLineFloorOfT/the_bound_before_the_ask (0.00s)
2026-11-27T18:43:48.5951719Z     --- PASS: TestParseLineFloorOfT/more_than_the_bound_before_the_ask (0.00s)
2026-11-27T18:43:48.5952226Z     --- PASS: TestParseLineFloorOfT/an_hour_before_the_ask (0.00s)
2026-11-27T18:43:48.5952618Z     --- PASS: TestParseLineFloorOfT/no_ask_known (0.00s)
2026-11-27T18:43:48.5954573Z --- PASS: TestParseLineHasMsAndLag (0.00s)
2026-11-27T18:43:48.5954917Z     --- PASS: TestParseLineHasMsAndLag/{"ok":true} (0.00s)
2026-11-27T18:43:48.5955334Z     --- PASS: TestParseLineHasMsAndLag/{"ok":true,"ms":0,"lag":0} (0.00s)
2026-11-27T18:43:48.5955779Z     --- PASS: TestParseLineHasMsAndLag/{"ok":false,"ms":12.5} (0.00s)
2026-11-27T18:43:48.5956225Z     --- PASS: TestParseLineHasMsAndLag/{"ok":true,"n":200,"lag":6} (0.00s)
2026-11-27T18:43:48.5962440Z --- PASS: TestParseLineFailedCheck (0.00s)
2026-11-27T18:43:48.5962792Z     --- PASS: TestParseLineFailedCheck/{"ok":false} (0.00s)
2026-11-27T18:43:48.5963177Z     --- PASS: TestParseLineFailedCheck/{"ok":true} (0.00s)
2026-11-27T18:43:48.5963576Z     --- PASS: TestParseLineFailedCheck/{"ok":false,"n":2} (0.00s)
2026-11-27T18:43:48.5964060Z     --- PASS: TestParseLineFailedCheck/{"ok":false,"t":"2026-11-14T10:00:00Z"} (0.00s)
2026-11-27T18:43:48.5964546Z     --- PASS: TestParseLineFailedCheck/{"ok":false,"ms":-1} (0.00s)
2026-11-27T18:43:48.5965141Z     --- PASS: TestParseLineFailedCheck/{"ok":false,"v":2} (0.00s)
2026-11-27T18:43:48.5965566Z     --- PASS: TestParseLineFailedCheck/{"ok":false,"ok":true} (0.00s)
2026-11-27T18:43:48.5966003Z     --- PASS: TestParseLineFailedCheck/{"ok":true,"ok":false} (0.00s)
2026-11-27T18:43:48.5966441Z     --- PASS: TestParseLineFailedCheck/{"ok":true,"ok":true} (0.00s)
2026-11-27T18:43:48.5966856Z     --- PASS: TestParseLineFailedCheck/{"ok":true,"n":0} (0.00s)
2026-11-27T18:43:48.5967261Z     --- PASS: TestParseLineFailedCheck/{"ok":"false"} (0.00s)
2026-11-27T18:43:48.5967778Z     --- PASS: TestParseLineFailedCheck/{"ok":null,"ok":false} (0.00s)
2026-11-27T18:43:48.5968213Z     --- PASS: TestParseLineFailedCheck/{"ok":false}_trailing (0.00s)
2026-11-27T18:43:48.5968613Z     --- PASS: TestParseLineFailedCheck/{"ok":fals (0.00s)
2026-11-27T18:43:48.5969055Z     --- PASS: TestParseLineFailedCheck/2026-11-13_10:00:07_{"ok":false} (0.00s)
2026-11-27T18:43:48.5969529Z     --- PASS: TestParseLineFailedCheck/{"detail":{"ok":false}} (0.00s)
2026-11-27T18:43:48.5969939Z     --- PASS: TestParseLineFailedCheck/starting (0.00s)
2026-11-27T18:44:37.0212888Z --- PASS: TestPodOnACluster (48.43s)
2026-11-27T18:45:50.6608577Z --- PASS: TestCustomPodOnACluster (73.64s)
2026-11-27T18:45:50.6612612Z --- PASS: TestPodSpec (0.00s)
2026-11-27T18:45:50.6614845Z --- PASS: TestPodSpecCustom (0.00s)
2026-11-27T18:45:50.6616669Z --- PASS: TestCustomPodKeepsTheBuiltInSettings (0.00s)
2026-11-27T18:45:50.6619525Z --- PASS: TestNewPodNeedsRaw (0.00s)
2026-11-27T18:45:50.8616131Z --- PASS: TestStartReplacesALeftoverPod (0.20s)
2026-11-27T18:45:50.8631565Z --- PASS: TestStartReturnsAtTheFirstObservation (0.00s)
2026-11-27T18:45:50.8643170Z --- PASS: TestStartFloorsTAtTheCreate (0.00s)
2026-11-27T18:45:51.3198867Z --- PASS: TestClockCheck (0.46s)
2026-11-27T18:45:51.3199304Z     --- PASS: TestClockCheck/read_0.3_s_after_the_stamp (0.15s)
2026-11-27T18:45:51.3199592Z     --- PASS: TestClockCheck/read_2.5_s_after_the_stamp (0.15s)
2026-11-27T18:45:51.3199851Z     --- PASS: TestClockCheck/read_2.5_s_before_the_stamp (0.15s)
2026-11-27T18:45:51.4721183Z --- PASS: TestStartWhenTheProbeExitsFirst (0.15s)
2026-11-27T18:45:51.4721916Z --- PASS: TestPodStateTextNamesTheMirror (0.00s)
2026-11-27T18:45:53.2758418Z --- PASS: TestStartTimesOut (1.80s)
2026-11-27T18:45:53.2759288Z     --- PASS: TestStartTimesOut/not_scheduled (0.45s)
2026-11-27T18:45:53.2759547Z     --- PASS: TestStartTimesOut/image_not_pulled (0.45s)
2026-11-27T18:45:53.2759831Z     --- PASS: TestStartTimesOut/image_not_pulled,_through_a_mirror (0.45s)
2026-11-27T18:45:53.2760094Z     --- PASS: TestStartTimesOut/log_text_only (0.45s)
2026-11-27T18:45:53.3769605Z --- PASS: TestStartEndsWithItsContext (0.10s)
2026-11-27T18:45:53.7032035Z --- PASS: TestProbeExitsAfterStart (0.33s)
2026-11-27T18:45:53.7032567Z     --- PASS: TestProbeExitsAfterStart/at_the_container's_finishedAt (0.16s)
2026-11-27T18:45:53.7033160Z     --- PASS: TestProbeExitsAfterStart/at_now_when_the_status_gives_no_time (0.16s)
2026-11-27T18:45:53.7262003Z --- PASS: TestStopDrains (0.02s)
2026-11-27T18:45:53.7262422Z     --- PASS: TestStopDrains/the_stream_ends_inside_the_drain (0.01s)
2026-11-27T18:45:53.7263101Z     --- PASS: TestStopDrains/the_stream_ends_after_the_drain_by_the_clock (0.01s)
2026-11-27T18:45:53.9792894Z --- PASS: TestStopKillsAProbeThatDoesNotStop (0.25s)
2026-11-27T18:45:54.0811923Z --- PASS: TestStopEndsWithItsContext (0.10s)
2026-11-27T18:45:54.2341057Z --- PASS: TestStopBeforeStartAndTwice (0.15s)
2026-11-27T18:45:54.2489095Z --- PASS: TestStopReportsAnExitOnce (0.01s)
2026-11-27T18:45:54.2489521Z     --- PASS: TestStopReportsAnExitOnce/the_watcher_did_not_see_it (0.00s)
2026-11-27T18:45:54.2489881Z     --- PASS: TestStopReportsAnExitOnce/the_watcher_saw_it (0.01s)
2026-11-27T18:45:54.4017110Z --- PASS: TestStartWhenThePodIsGone (0.15s)
2026-11-27T18:45:54.4281801Z --- PASS: TestExitAroundTheFirstObservation (0.03s)
2026-11-27T18:45:54.7418330Z --- PASS: TestStopWhenNobodyReadsEvents (0.31s)
2026-11-27T18:45:55.3460724Z --- PASS: TestAnAPIErrorIsNoExit (0.60s)
2026-11-27T18:45:55.3461088Z     --- PASS: TestAnAPIErrorIsNoExit/the_follower_opens_the_log_again (0.15s)
2026-11-27T18:45:55.3461441Z     --- PASS: TestAnAPIErrorIsNoExit/the_pod's_state_is_not_known (0.45s)
2026-11-27T18:45:55.3575283Z --- PASS: TestStopWhenThePodIsGone (0.01s)
2026-11-27T18:45:55.3686142Z --- PASS: TestEventsAreReadFromTheStart (0.01s)
2026-11-27T18:45:56.6764168Z --- PASS: TestAFailedStartStopsTheProbe (1.31s)
2026-11-27T18:45:56.6764777Z     --- PASS: TestAFailedStartStopsTheProbe/no_observation_in_time (0.45s)
2026-11-27T18:45:56.6765322Z     --- PASS: TestAFailedStartStopsTheProbe/the_clock_check_fails (0.15s)
2026-11-27T18:45:56.6765810Z     --- PASS: TestAFailedStartStopsTheProbe/the_probe_exits_first (0.15s)
2026-11-27T18:45:56.6766297Z     --- PASS: TestAFailedStartStopsTheProbe/the_delete_fails (0.45s)
2026-11-27T18:45:56.6766769Z     --- PASS: TestAFailedStartStopsTheProbe/the_context_ends (0.10s)
2026-11-27T18:45:56.6767689Z --- PASS: TestAStartThatCreatedNoPodStoppedWhenAsked (0.00s)
2026-11-27T18:45:56.6769437Z --- PASS: TestStartRefusedPod (0.00s)
2026-11-27T18:45:56.6770328Z     --- PASS: TestStartRefusedPod/Pod_Security (0.00s)
2026-11-27T18:45:56.6770753Z     --- PASS: TestStartRefusedPod/validation (0.00s)
2026-11-27T18:45:56.6771167Z     --- PASS: TestStartRefusedPod/a_bad_request (0.00s)
2026-11-27T18:45:56.6771857Z --- PASS: TestStartCreateFailsOtherwise (0.00s)
2026-11-27T18:45:57.1272581Z --- PASS: TestStartNamesAMissingSecret (0.45s)
2026-11-27T18:45:57.1299696Z --- PASS: TestStopWritesThePodAndItsOtherContainers (0.00s)
2026-11-27T18:45:57.5806084Z --- PASS: TestStartTimeoutWritesThePod (0.45s)
2026-11-27T18:45:57.5832047Z --- PASS: TestPodEvidenceThatCannotBeRead (0.00s)
2026-11-27T18:45:57.5832722Z --- PASS: TestRefusedPodWritesNoEvidence (0.00s)
2026-11-27T18:45:57.7352347Z --- PASS: TestPodEvidenceWhenThePodCannotBeRead (0.15s)
2026-11-27T18:45:57.7391047Z --- PASS: TestContainerLogCutWhileCopied (0.00s)
2026-11-27T18:45:57.7391461Z     --- PASS: TestContainerLogCutWhileCopied/no_line_break_yet (0.00s)
2026-11-27T18:45:57.7391793Z     --- PASS: TestContainerLogCutWhileCopied/after_a_line_break (0.00s)
2026-11-27T18:45:57.7392293Z --- PASS: TestStopBeforeStartClosesEvents (0.00s)
2026-11-27T18:45:57.7392748Z --- PASS: TestEventsClosedWhenNoPodWasCreated (0.00s)
2026-11-27T18:45:57.7401817Z --- PASS: TestStopAfterAStartThatPanicked (0.00s)
2026-11-27T18:45:57.7412609Z --- PASS: TestStopAfterAStartThatPanickedBeforeItsRun (0.00s)
2026-11-27T18:45:58.2431996Z --- PASS: TestStopBoundsItsAPICalls (0.50s)
2026-11-27T18:45:58.3949609Z --- PASS: TestCountsWhileTheProbeRuns (0.15s)
2026-11-27T18:45:58.3974963Z --- PASS: TestCountsAfterAStopThatPanicked (0.00s)
2026-11-27T18:45:58.6493885Z --- PASS: TestStopWhileThePodsStatusLags (0.25s)
2026-11-27T18:45:58.9128539Z --- PASS: TestStopWhenNoStreamEndedTheLog (0.26s)
2026-11-27T18:45:58.9129388Z     --- PASS: TestStopWhenNoStreamEndedTheLog/a_stream_ends_before_the_ask,_and_every_opening_fails_until_the_pod_is_gone_after_the_drain (0.01s)
2026-11-27T18:45:58.9130245Z     --- PASS: TestStopWhenNoStreamEndedTheLog/no_opening_succeeds,_and_the_wait_runs_out (0.25s)
2026-11-27T18:45:58.9131034Z --- PASS: TestSendAfterTheContextEnded (0.00s)
2026-11-27T18:45:58.9148258Z --- PASS: TestCheckTarget (0.00s)
2026-11-27T18:45:58.9148451Z     --- PASS: TestCheckTarget/NAME.NS.svc (0.00s)
2026-11-27T18:45:58.9148649Z     --- PASS: TestCheckTarget/NAME.NS (0.00s)
2026-11-27T18:45:58.9148897Z     --- PASS: TestCheckTarget/NAME.NS.svc_and_the_cluster's_domain (0.00s)
2026-11-27T18:45:58.9149129Z     --- PASS: TestCheckTarget/a_port (0.00s)
2026-11-27T18:45:58.9149350Z     --- PASS: TestCheckTarget/a_path,_a_query_and_a_port (0.00s)
2026-11-27T18:45:58.9149646Z     --- PASS: TestCheckTarget/one_label_names_a_Service_of_the_probe's_namespace (0.00s)
2026-11-27T18:45:58.9149970Z     --- PASS: TestCheckTarget/one_label_that_names_nothing_is_probed_as_it_is (0.00s)
2026-11-27T18:45:58.9150257Z     --- PASS: TestCheckTarget/NAME.NS.svc_that_does_not_exist (0.00s)
2026-11-27T18:45:58.9150554Z     --- PASS: TestCheckTarget/NAME.NS.svc.cluster.local_that_does_not_exist (0.00s)
2026-11-27T18:45:58.9150885Z     --- PASS: TestCheckTarget/NAME.NS_that_does_not_exist_is_probed_as_it_is (0.00s)
2026-11-27T18:45:58.9151148Z     --- PASS: TestCheckTarget/a_headless_Service (0.00s)
2026-11-27T18:45:58.9151386Z     --- PASS: TestCheckTarget/one_pod_of_a_headless_Service (0.00s)
2026-11-27T18:45:58.9151758Z     --- PASS: TestCheckTarget/one_pod,_with_the_cluster's_domain (0.00s)
2026-11-27T18:45:58.9152050Z     --- PASS: TestCheckTarget/a_Service_that_routes_only_within_a_node (0.00s)
2026-11-27T18:45:58.9152300Z     --- PASS: TestCheckTarget/an_IPv4_address (0.00s)
2026-11-27T18:45:58.9152531Z     --- PASS: TestCheckTarget/an_IPv6_address_with_a_port (0.00s)
2026-11-27T18:45:58.9152762Z     --- PASS: TestCheckTarget/an_outside_name (0.00s)
2026-11-27T18:45:58.9153002Z     --- PASS: TestCheckTarget/two_labels_that_name_no_Service (0.00s)
2026-11-27T18:45:58.9153235Z     --- PASS: TestCheckTarget/upper_case,_found (0.00s)
2026-11-27T18:45:58.9153472Z     --- PASS: TestCheckTarget/upper_case,_no_such_Service (0.00s)
2026-11-27T18:45:58.9153709Z     --- PASS: TestCheckTarget/upper_case,_headless (0.00s)
2026-11-27T18:45:58.9153953Z     --- PASS: TestCheckTarget/trailing_dot,_found (0.00s)
2026-11-27T18:45:58.9154198Z     --- PASS: TestCheckTarget/trailing_dot,_no_such_Service (0.00s)
2026-11-27T18:45:58.9154446Z     --- PASS: TestCheckTarget/trailing_dot,_headless (0.00s)
2026-11-27T18:45:58.9154696Z     --- PASS: TestCheckTarget/domain_and_trailing_dot,_found (0.00s)
2026-11-27T18:45:58.9154975Z     --- PASS: TestCheckTarget/domain_and_trailing_dot,_no_such_Service (0.00s)
2026-11-27T18:45:58.9155259Z     --- PASS: TestCheckTarget/domain_and_trailing_dot,_headless (0.00s)
2026-11-27T18:45:58.9155503Z     --- PASS: TestCheckTarget/domain_without_.svc (0.00s)
2026-11-27T18:45:58.9155731Z     --- PASS: TestCheckTarget/svc.cluster.local_alone (0.00s)
2026-11-27T18:45:58.9155967Z     --- PASS: TestCheckTarget/empty_label_before_svc (0.00s)
2026-11-27T18:45:58.9156193Z     --- PASS: TestCheckTarget/empty_first_label (0.00s)
2026-11-27T18:45:58.9156406Z     --- PASS: TestCheckTarget/empty_middle_label (0.00s)
2026-11-27T18:45:58.9156652Z     --- PASS: TestCheckTarget/a_user_part_that_looks_like_a_host (0.00s)
2026-11-27T18:45:58.9156896Z     --- PASS: TestCheckTarget/https_adds_no_note (0.00s)
2026-11-27T18:45:58.9157131Z     --- PASS: TestCheckTarget/affinity_by_client_address (0.00s)
2026-11-27T18:45:58.9157569Z     --- PASS: TestCheckTarget/a_Service_of_type_ExternalName_names_no_Service_of_the_cluster (0.00s)
2026-11-27T18:45:58.9158003Z --- PASS: TestCheckTargetAPIError (0.00s)
2026-11-27T18:45:58.9161980Z --- PASS: TestCheckTargetKeepsSecretsOut (0.00s)
2026-11-27T18:45:58.9162234Z     --- PASS: TestCheckTargetKeepsSecretsOut/user,_missing_Service (0.00s)
2026-11-27T18:45:58.9162625Z     --- PASS: TestCheckTargetKeepsSecretsOut/password,_missing_Service (0.00s)
2026-11-27T18:45:58.9162930Z     --- PASS: TestCheckTargetKeepsSecretsOut/query,_missing_Service (0.00s)
2026-11-27T18:45:58.9163209Z     --- PASS: TestCheckTargetKeepsSecretsOut/user,_headless (0.00s)
2026-11-27T18:45:58.9163484Z     --- PASS: TestCheckTargetKeepsSecretsOut/password,_headless (0.00s)
2026-11-27T18:45:58.9163755Z     --- PASS: TestCheckTargetKeepsSecretsOut/query,_headless (0.00s)
2026-11-27T18:45:58.9164021Z     --- PASS: TestCheckTargetKeepsSecretsOut/user,_one_pod (0.00s)
2026-11-27T18:45:58.9164290Z     --- PASS: TestCheckTargetKeepsSecretsOut/password,_one_pod (0.00s)
2026-11-27T18:45:58.9164566Z     --- PASS: TestCheckTargetKeepsSecretsOut/query,_one_pod (0.00s)
2026-11-27T18:45:58.9164822Z     --- PASS: TestCheckTargetKeepsSecretsOut/user,_notes (0.00s)
2026-11-27T18:45:58.9165081Z     --- PASS: TestCheckTargetKeepsSecretsOut/password,_notes (0.00s)
2026-11-27T18:45:58.9165340Z     --- PASS: TestCheckTargetKeepsSecretsOut/query,_notes (0.00s)
2026-11-27T18:45:58.9165605Z     --- PASS: TestCheckTargetKeepsSecretsOut/fragment,_notes (0.00s)
2026-11-27T18:45:58.9165910Z     --- PASS: TestCheckTargetKeepsSecretsOut/password,_URL_that_does_not_parse (0.00s)
2026-11-27T18:45:58.9166241Z     --- PASS: TestCheckTargetKeepsSecretsOut/query,_URL_that_does_not_parse (0.00s)
2026-11-27T18:45:58.9166533Z     --- PASS: TestCheckTargetKeepsSecretsOut/user,_no_host (0.00s)
2026-11-27T18:45:58.9166891Z --- PASS: TestCheckTargetUnreadable (0.00s)
2026-11-27T18:46:41.6370651Z --- PASS: TestAWatchBreakInTheSettleTime (0.35s)
2026-11-27T18:46:41.7581844Z --- PASS: TestAnErrorEventIsABreak (0.12s)
2026-11-27T18:46:41.8810234Z --- PASS: TestAFailedWatchCallKeepsTheWatchDown (0.12s)
2026-11-27T18:46:42.1825776Z --- PASS: TestARenewalInsideAWindowMakesItNotVerified (0.30s)
2026-11-27T18:46:42.4844576Z --- PASS: TestAWindowThatEndsWhileAWatchIsDown (0.30s)
2026-11-27T18:46:42.7065963Z --- PASS: TestAListAgainKeepsTheMomentTheWatchWentDown (0.22s)
2026-11-27T18:46:42.7066559Z     --- PASS: TestAListAgainKeepsTheMomentTheWatchWentDown/a_list (0.11s)
2026-11-27T18:46:42.7067256Z     --- PASS: TestAListAgainKeepsTheMomentTheWatchWentDown/a_watch_that_sends_the_initial_events (0.11s)
2026-11-27T18:46:42.8073273Z --- PASS: TestNoStreamAfterStop (0.10s)
2026-11-27T18:46:42.9082132Z --- PASS: TestAStreamsStopTellsItsEndFirst (0.10s)
2026-11-27T18:46:44.8434547Z --- PASS: TestAGapAcrossTheStartThroughTheWatcher (1.94s)
2026-11-27T18:46:44.8435142Z     --- PASS: TestAGapAcrossTheStartThroughTheWatcher/a_renewal (0.31s)
2026-11-27T18:46:44.8435637Z     --- PASS: TestAGapAcrossTheStartThroughTheWatcher/a_break (1.62s)
2026-11-27T18:46:44.9446600Z --- PASS: TestTheWatchAsksForADay (0.10s)
2026-11-27T18:46:45.2452641Z --- PASS: TestStartWaitsForEveryWatch (0.30s)
2026-11-27T18:46:45.2456840Z --- PASS: TestCounts (0.00s)
2026-11-27T18:46:45.2457180Z     --- PASS: TestCounts/a_rolling_update_of_two_pods (0.00s)
2026-11-27T18:46:45.2457647Z     --- PASS: TestCounts/nothing_changes (0.00s)
2026-11-27T18:46:45.2458051Z     --- PASS: TestCounts/two_workloads,_only_the_second_changes (0.00s)
2026-11-27T18:46:45.2458451Z     --- PASS: TestCounts/a_renamed_Deployment (0.00s)
2026-11-27T18:46:45.2458808Z     --- PASS: TestCounts/a_scale-up_replaces_nothing (0.00s)
2026-11-27T18:46:45.2459222Z     --- PASS: TestCounts/a_StatefulSet's_pod_back_under_its_name (0.00s)
2026-11-27T18:46:45.2459791Z --- PASS: TestBeginToldLate (0.00s)
2026-11-27T18:46:45.2460237Z --- PASS: TestQuickestStop (0.00s)
2026-11-27T18:46:45.2460658Z --- PASS: TestDigests (0.00s)
2026-11-27T18:46:45.2461096Z --- PASS: TestNoPodReplaced (0.00s)
2026-11-27T18:46:45.2464999Z --- PASS: TestStallDetails (0.00s)
2026-11-27T18:46:45.2465417Z     --- PASS: TestStallDetails/a_pod_that_cannot_be_scheduled (0.00s)
2026-11-27T18:46:45.2465893Z     --- PASS: TestStallDetails/a_pod_in_CrashLoopBackOff (0.00s)
2026-11-27T18:46:45.2466417Z     --- PASS: TestStallDetails/the_settle_time_started_again_after_the_timeout (0.00s)
2026-11-27T18:46:45.2470162Z --- PASS: TestStallDetailsNameTheMirror (0.00s)
2026-11-27T18:46:45.2470463Z     --- PASS: TestStallDetailsNameTheMirror/an_init_container_in_ImagePullBackOff (0.00s)
2026-11-27T18:46:45.2470779Z     --- PASS: TestStallDetailsNameTheMirror/ImagePullBackOff (0.00s)
2026-11-27T18:46:45.2471044Z     --- PASS: TestStallDetailsNameTheMirror/ErrImagePull (0.00s)
2026-11-27T18:46:45.2471310Z     --- PASS: TestStallDetailsNameTheMirror/ContainerCreating (0.00s)
2026-11-27T18:46:45.2471581Z     --- PASS: TestStallDetailsNameTheMirror/no_MirrorNote (0.00s)
2026-11-27T18:46:45.2471900Z     --- PASS: TestStallDetailsNameTheMirror/a_running_container_after_the_waiting_one (0.00s)
2026-11-27T18:46:45.2473087Z --- PASS: TestInstallTextNamesTheMirror (0.00s)
2026-11-27T18:46:45.2473367Z     --- PASS: TestInstallTextNamesTheMirror/a_container_in_ImagePullBackOff (0.00s)
2026-11-27T18:46:45.2473708Z     --- PASS: TestInstallTextNamesTheMirror/an_init_container_in_ImagePullBackOff (0.00s)
2026-11-27T18:46:45.2474087Z --- PASS: TestUnsupported (0.00s)
2026-11-27T18:46:45.2476131Z --- PASS: TestInstallState (0.00s)
2026-11-27T18:46:45.2476311Z     --- PASS: TestInstallState/ready (0.00s)
2026-11-27T18:46:45.2476533Z     --- PASS: TestInstallState/the_command_still_runs (0.00s)
2026-11-27T18:46:45.2476855Z     --- PASS: TestInstallState/nothing_watched,_one_second_before_the_settle_time_has_passed (0.00s)
2026-11-27T18:46:45.2477206Z     --- PASS: TestInstallState/nothing_watched_once_the_settle_time_has_passed (0.00s)
2026-11-27T18:46:45.2477673Z     --- PASS: TestInstallState/nothing_watched,_with_no_selector (0.00s)
2026-11-27T18:46:45.2478135Z     --- PASS: TestInstallState/a_DaemonSet_that_updates_with_OnDelete (0.00s)
2026-11-27T18:46:45.2478521Z     --- PASS: TestInstallState/not_ready_one_second_before_the_timeout (0.00s)
2026-11-27T18:46:45.2478952Z     --- PASS: TestInstallState/not_ready_at_the_timeout (0.00s)
2026-11-27T18:46:45.2479573Z --- PASS: TestInstallStateKeepsTheReadyMoment (0.00s)
2026-11-27T18:46:45.2480046Z --- PASS: TestPodsAt (0.00s)
2026-11-27T18:46:45.2480416Z --- PASS: TestPodsAtAnEarlierTransition (0.00s)
2026-11-27T18:46:45.2480918Z --- PASS: TestCountsAfterARenameAndARollback (0.00s)
2026-11-27T18:46:45.2481512Z --- PASS: TestBeginToldLateWorkload (0.00s)
2026-11-27T18:46:45.2483850Z --- PASS: TestCountsFollowTheWatchedSet (0.00s)
2026-11-27T18:46:45.2484431Z     --- PASS: TestCountsFollowTheWatchedSet/a_workload_that_joins_in_the_settle_time_is_counted (0.00s)
2026-11-27T18:46:45.2485015Z     --- PASS: TestCountsFollowTheWatchedSet/a_workload_that_stops_matching_while_it_rolls_out_stays_counted (0.00s)
2026-11-27T18:46:45.2485561Z     --- PASS: TestCountsFollowTheWatchedSet/a_workload_deleted_in_the_settle_time_is_removed,_and_its_pods_replaced (0.00s)
2026-11-27T18:46:45.2486048Z --- PASS: TestCountsKeptWhenTheWindowEnds (0.00s)
2026-11-27T18:46:45.2486468Z --- PASS: TestCountsLeaveOutAPodKnownOnlyFromItsGone (0.00s)
2026-11-27T18:46:45.2486998Z --- PASS: TestQuickestStopCountsFromTheStart (0.00s)
2026-11-27T18:46:45.2487394Z --- PASS: TestQuickestStopOfZero (0.00s)
2026-11-27T18:46:45.2487963Z --- PASS: TestDigestsOfEverySet (0.00s)
2026-11-27T18:46:45.2489808Z --- PASS: TestStateReturnsCopies (0.00s)
2026-11-27T18:46:45.2490271Z     --- PASS: TestStateReturnsCopies/a_stall_with_a_pod_that_cannot_be_scheduled (0.00s)
2026-11-27T18:46:45.2490870Z     --- PASS: TestStateReturnsCopies/a_stall_after_the_settle_time_started_again (0.00s)
2026-11-27T18:46:45.2491496Z     --- PASS: TestStateReturnsCopies/an_open_window_after_the_settle_time_started_again (0.00s)
2026-11-27T18:46:45.2497318Z --- PASS: TestMoreStallDetails (0.00s)
2026-11-27T18:46:45.2497932Z     --- PASS: TestMoreStallDetails/a_pod_is_described_as_its_last_Set_shows_it (0.00s)
2026-11-27T18:46:45.2498610Z     --- PASS: TestMoreStallDetails/a_pod_held_by_a_scheduling_gate_is_not_one_that_could_not_be_scheduled (0.00s)
2026-11-27T18:46:45.2499260Z     --- PASS: TestMoreStallDetails/a_container_running_and_one_that_ended (0.00s)
2026-11-27T18:46:45.2499815Z     --- PASS: TestMoreStallDetails/a_container_with_no_state_is_waiting,_as_the_API_defines (0.00s)
2026-11-27T18:46:45.2500558Z     --- PASS: TestMoreStallDetails/a_deleted_workload_with_pods_left,_and_a_pod_stopping,_asked_long_after_the_stall (0.00s)
2026-11-27T18:46:45.2501327Z     --- PASS: TestMoreStallDetails/a_deleted_workload_counts_only_its_pods_that_are_not_gone (0.00s)
2026-11-27T18:46:45.2501961Z     --- PASS: TestMoreStallDetails/a_deleted_workload_with_no_pod_left_gives_no_sentence (0.00s)
2026-11-27T18:46:45.2502626Z     --- PASS: TestMoreStallDetails/a_pod_that_is_gone_is_neither_described_nor_named_as_not_scheduled (0.00s)
2026-11-27T18:46:45.2503295Z     --- PASS: TestMoreStallDetails/the_details_are_taken_after_the_change_that_caused_the_stall (0.00s)
2026-11-27T18:46:45.2503919Z     --- PASS: TestMoreStallDetails/a_workload_that_does_not_take_part_gives_no_details (0.00s)
2026-11-27T18:46:45.2504606Z --- PASS: TestUnschedulableNamesTheWorkers (0.00s)
2026-11-27T18:46:45.2509089Z --- PASS: TestUnsupportedAtSomeMoment (0.00s)
2026-11-27T18:46:45.2509523Z     --- PASS: TestUnsupportedAtSomeMoment/a_workload_watched_from_the_start (0.00s)
2026-11-27T18:46:45.2510185Z     --- PASS: TestUnsupportedAtSomeMoment/a_workload_that_updates_by_rolling_later_in_the_transition_stays_listed (0.00s)
2026-11-27T18:46:45.2510797Z     --- PASS: TestUnsupportedAtSomeMoment/two_workloads,_sorted (0.00s)
2026-11-27T18:46:45.2511405Z     --- PASS: TestUnsupportedAtSomeMoment/a_workload_that_joins_the_watched_set_while_it_does_not_roll (0.00s)
2026-11-27T18:46:45.2512017Z     --- PASS: TestUnsupportedAtSomeMoment/a_workload_the_selector_leaves_out_is_not_listed (0.00s)
2026-11-27T18:46:45.2512653Z     --- PASS: TestUnsupportedAtSomeMoment/a_workload_the_selector_leaves_out_and_that_a_Set_shows_in_the_window_is_not_listed (0.00s)
2026-11-27T18:46:45.2513192Z     --- PASS: TestUnsupportedAtSomeMoment/a_workload_told_when_the_window_ends_is_not_listed (0.00s)
2026-11-27T18:46:45.2513587Z     --- PASS: TestUnsupportedAtSomeMoment/a_workload_first_told_gone_while_it_does_not_roll (0.00s)
2026-11-27T18:46:45.2514021Z --- PASS: TestNoStallDetailsForAFinish (0.00s)
2026-11-27T18:46:45.2515401Z --- PASS: TestConditionsOfAWindowThatDidNotEnd (0.00s)
2026-11-27T18:46:45.2515817Z     --- PASS: TestConditionsOfAWindowThatDidNotEnd/read_while_open,_before_it_finishes (0.00s)
2026-11-27T18:46:45.2516264Z     --- PASS: TestConditionsOfAWindowThatDidNotEnd/read_while_open,_in_its_settle_time,_which_gives_no_no-pod-replaced (0.00s)
2026-11-27T18:46:45.2516677Z     --- PASS: TestConditionsOfAWindowThatDidNotEnd/cut_short_before_the_timeout (0.00s)
2026-11-27T18:46:45.2517811Z --- PASS: TestConditionsOfASignalDuringTheDeploy (0.00s)
2026-11-27T18:46:45.2518103Z     --- PASS: TestConditionsOfASignalDuringTheDeploy/before_it_finishes (0.00s)
2026-11-27T18:46:45.2518422Z     --- PASS: TestConditionsOfASignalDuringTheDeploy/in_its_settle_time (0.00s)
2026-11-27T18:46:45.2518833Z --- PASS: TestCountsLeaveOutUnwatchedPods (0.00s)
2026-11-27T18:46:45.2519225Z --- PASS: TestUnsupportedAfterItStopsMatching (0.00s)
2026-11-27T18:46:45.2521675Z --- PASS: TestInstallStateFromItsRecords (0.00s)
2026-11-27T18:46:45.2521980Z     --- PASS: TestInstallStateFromItsRecords/a_pod_that_is_stopping_holds_the_install (0.00s)
2026-11-27T18:46:45.2522399Z     --- PASS: TestInstallStateFromItsRecords/ready_once_that_pod_is_gone,_with_the_digests_of_the_pods_left (0.00s)
2026-11-27T18:46:45.2522805Z     --- PASS: TestInstallStateFromItsRecords/the_timeout_names_a_pod_that_is_stopping (0.00s)
2026-11-27T18:46:45.2523157Z     --- PASS: TestInstallStateFromItsRecords/a_deleted_workload_is_not_watched (0.00s)
2026-11-27T18:46:45.2523592Z     --- PASS: TestInstallStateFromItsRecords/a_workload_that_does_not_roll,_while_the_command_runs,_is_left_to_the_timeout (0.00s)
2026-11-27T18:46:45.2524054Z     --- PASS: TestInstallStateFromItsRecords/nothing_watched_while_the_command_runs_is_not_yet_an_error (0.00s)
2026-11-27T18:46:45.2524528Z --- PASS: TestNothingWatchedNamesTheOptions (0.00s)
2026-11-27T18:46:45.2524876Z --- PASS: TestPodsAtOutsideAnyWindow (0.00s)
2026-11-27T18:46:45.2525196Z --- PASS: TestPodsAtWatchedPods (0.00s)
2026-11-27T18:46:45.2525524Z --- PASS: TestCountsAtTheMomentAsked (0.00s)
2026-11-27T18:46:45.2527852Z --- PASS: TestMissed (0.00s)
2026-11-27T18:46:45.2528067Z     --- PASS: TestMissed/the_first_of_two_moments_in_the_window (0.00s)
2026-11-27T18:46:45.2528324Z     --- PASS: TestMissed/before_the_transition_began (0.00s)
2026-11-27T18:46:45.2528599Z     --- PASS: TestMissed/at_the_transition's_start,_told_before_its_Begin (0.00s)
2026-11-27T18:46:45.2528934Z     --- PASS: TestMissed/the_first_of_the_moments_after_the_start,_told_before_its_Begin (0.00s)
2026-11-27T18:46:45.2529209Z     --- PASS: TestMissed/after_the_window_ended (0.00s)
2026-11-27T18:46:45.2529481Z     --- PASS: TestMissed/at_the_end_of_the_window,_which_belongs_to_no_window (0.00s)
2026-11-27T18:46:45.2529720Z     --- PASS: TestMissed/in_a_stall (0.00s)
2026-11-27T18:46:45.2530004Z     --- PASS: TestMissed/a_stall_decided_by_a_change_that_a_list_again_told_past_the_limit (0.00s)
2026-11-27T18:46:45.2530883Z --- PASS: TestMissedAfterAWindowEnded (0.00s)
2026-11-27T18:46:45.2531219Z     --- PASS: TestMissedAfterAWindowEnded/before_the_next_start (0.00s)
2026-11-27T18:46:45.2531517Z     --- PASS: TestMissedAfterAWindowEnded/before_and_after_the_next_start (0.00s)
2026-11-27T18:46:45.2534005Z --- PASS: TestWatchDown (0.00s)
2026-11-27T18:46:45.2534242Z     --- PASS: TestWatchDown/down_in_the_settle_time,_at_the_window's_end (0.00s)
2026-11-27T18:46:45.2534527Z     --- PASS: TestWatchDown/down_and_up_again_inside_the_window (0.00s)
2026-11-27T18:46:45.2534830Z     --- PASS: TestWatchDown/down_at_the_window's_end,_which_belongs_to_no_window (0.00s)
2026-11-27T18:46:45.2535133Z     --- PASS: TestWatchDown/down_from_before_the_start_to_the_end (0.00s)
2026-11-27T18:46:45.2535426Z     --- PASS: TestWatchDown/down_from_before_the_start,_up_in_the_window (0.00s)
2026-11-27T18:46:45.2535759Z     --- PASS: TestWatchDown/down_from_before_the_start,_up_after_it,_told_before_Begin (0.00s)
2026-11-27T18:46:45.2536084Z     --- PASS: TestWatchDown/a_stretch_down_that_ended_before_the_start (0.00s)
2026-11-27T18:46:45.2536411Z     --- PASS: TestWatchDown/down_twice_in_the_window,_timed_when_it_first_went_down (0.00s)
2026-11-27T18:46:45.2536731Z     --- PASS: TestWatchDown/down_in_the_window,_timed_when_it_went_down (0.00s)
2026-11-27T18:46:45.2537695Z --- PASS: TestAGapAcrossTheStart (0.00s)
2026-11-27T18:46:45.2537944Z     --- PASS: TestAGapAcrossTheStart/Begin_told_after_the_watch_is_up (0.00s)
2026-11-27T18:46:45.2538340Z     --- PASS: TestAGapAcrossTheStart/Begin_told_while_the_watch_is_down (0.00s)
2026-11-27T18:46:45.2538717Z --- PASS: TestARenewalAcrossTheLimit (0.00s)
2026-11-27T18:46:45.3519641Z --- PASS: TestWriteEvents (0.10s)
2026-11-27T18:46:45.4527851Z --- PASS: TestWriteEventsWhenAListFails (0.10s)
2026-11-27T18:46:55.3586490Z --- PASS: TestTransitionStallNamesTheLastEvent (9.91s)
2026-11-27T18:46:55.3586904Z     --- PASS: TestTransitionStallNamesTheLastEvent/the_pod's_event_is_the_last (3.30s)
2026-11-27T18:46:55.3587343Z     --- PASS: TestTransitionStallNamesTheLastEvent/the_ReplicaSet's_event_is_the_last (3.30s)
2026-11-27T18:46:55.3587982Z     --- PASS: TestTransitionStallNamesTheLastEvent/no_event_names_it (3.30s)
2026-11-27T18:46:55.4841975Z --- PASS: TestTransitionStallNamesAStoppingPodsLastEvent (0.13s)
2026-11-27T18:46:58.7855799Z --- PASS: TestTransitionStallWithoutListableEvents (3.30s)
2026-11-27T18:46:59.4883705Z --- PASS: TestFinishesAsTheyHappen (0.70s)
2026-11-27T18:46:59.5891803Z --- PASS: TestFinishesIsClosedByStop (0.10s)
2026-11-27T18:46:59.8924122Z --- PASS: TestPodListsBeforeAndAfter (0.30s)
2026-11-27T18:47:00.1938619Z --- PASS: TestFinishesWhenTheSettleTimeStartsAgainAtTheSameMoment (0.30s)
2026-11-27T18:47:00.4955434Z --- PASS: TestFinishesWhenAPodIsGone (0.30s)
2026-11-27T18:47:00.6070757Z --- PASS: TestPodListAfterWhenCtxEnds (0.11s)
2026-11-27T18:47:00.7180157Z --- PASS: TestPodListsOfAnEmptyNamespace (0.11s)
2026-11-27T18:47:01.3533106Z --- PASS: TestPodLogsOfWatchedPods (0.64s)
2026-11-27T18:47:01.3536450Z     --- PASS: TestPodLogsOfWatchedPods/the_ReplicaSet_first (0.31s)
2026-11-27T18:47:01.3537083Z     --- PASS: TestPodLogsOfWatchedPods/the_pod_first (0.32s)
2026-11-27T18:47:01.6616141Z --- PASS: TestPodLogsKeepThePreviousRun (0.31s)
2026-11-27T18:47:02.5734719Z --- PASS: TestPodLogsWaitForTheContainerToStart (0.91s)
2026-11-27T18:47:04.6963480Z --- PASS: TestPodLogsOfAPodGoneBeforeItsLogWasRead (2.12s)
2026-11-27T18:47:04.6964122Z     --- PASS: TestPodLogsOfAPodGoneBeforeItsLogWasRead/the_container_never_started (0.71s)
2026-11-27T18:47:04.6965230Z     --- PASS: TestPodLogsOfAPodGoneBeforeItsLogWasRead/every_opening_fails,_0_restarts (0.71s)
2026-11-27T18:47:04.6965649Z     --- PASS: TestPodLogsOfAPodGoneBeforeItsLogWasRead/every_opening_fails,_1_restarts (0.71s)
2026-11-27T18:47:04.8110622Z --- PASS: TestStopClosesThePodLogs (0.11s)
2026-11-27T18:47:05.1346205Z --- PASS: TestPodEvents (0.32s)
2026-11-27T18:47:05.3833227Z --- PASS: TestPodLogsWhenTheWorkloadComesLast (0.25s)
2026-11-27T18:47:05.3833750Z     --- PASS: TestPodLogsWhenTheWorkloadComesLast/a_Deployment_after_its_ReplicaSet_and_pod (0.12s)
2026-11-27T18:47:05.3834193Z     --- PASS: TestPodLogsWhenTheWorkloadComesLast/a_StatefulSet_after_its_pod (0.12s)
2026-11-27T18:47:05.6909163Z --- PASS: TestPodLogsOfAStreamCutWhileRead (0.31s)
2026-11-27T18:47:05.9984199Z --- PASS: TestPodLogsKeepACutLineApart (0.31s)
2026-11-27T18:47:06.3280283Z --- PASS: TestPodLogsOfARestartedContainer (0.33s)
2026-11-27T18:47:06.6352023Z --- PASS: TestPodLogsOpenTheSameRunAgain (0.31s)
2026-11-27T18:47:06.7419690Z --- PASS: TestRestartChangesEveryWatchedWorkload (0.11s)
2026-11-27T18:47:06.8448567Z --- PASS: TestRestartStopsAtTheFirstRefusal (0.10s)
2026-11-27T18:47:06.9457941Z --- PASS: TestRestartWhenCtxHasEnded (0.10s)
2026-11-27T18:47:07.0465087Z --- PASS: TestRestartWhenCtxHasEndedWithNothingToRestart (0.10s)
2026-11-27T18:47:07.1488400Z --- PASS: TestRestartWhenCtxEndsDuringAPatch (0.10s)
2026-11-27T18:47:07.2499430Z --- PASS: TestRestartWithNothingToRestart (0.10s)
2026-11-27T18:47:07.2499899Z --- PASS: TestComplete (0.00s)
2026-11-27T18:47:07.2511933Z --- PASS: TestCompleteAgreesWithKubectl (0.00s)
2026-11-27T18:47:07.2512639Z --- PASS: TestRolling (0.00s)
2026-11-27T18:47:07.2554456Z --- PASS: TestInstallSettling (0.00s)
2026-11-27T18:47:07.2554865Z     --- PASS: TestInstallSettling/a_tenth_of_a_second_before_the_settle_time_has_passed (0.00s)
2026-11-27T18:47:07.2555312Z     --- PASS: TestInstallSettling/the_first_observation_is_after_the_conditions_last_came_to_hold (0.00s)
2026-11-27T18:47:07.2555793Z     --- PASS: TestInstallSettling/the_first_observation_is_before_the_conditions_last_came_to_hold (0.00s)
2026-11-27T18:47:07.2556264Z     --- PASS: TestInstallSettling/a_break_that_was_over_before_the_first_observation_is_not_counted (0.00s)
2026-11-27T18:47:07.2556702Z     --- PASS: TestInstallSettling/a_pod_that_stops_being_ready_starts_the_time_again (0.00s)
2026-11-27T18:47:07.2557145Z     --- PASS: TestInstallSettling/and_the_install_settles_limits.settle_after_the_pod_is_ready_again (0.00s)
2026-11-27T18:47:07.2557762Z     --- PASS: TestInstallSettling/a_deleted_pod_that_is_not_gone_is_a_break (0.00s)
2026-11-27T18:47:07.2558505Z     --- PASS: TestInstallSettling/the_last_watched_workload_is_removed:_a_break,_and_never_nothing-watched (0.00s)
2026-11-27T18:47:07.2559222Z     --- PASS: TestInstallSettling/and_the_install_has_failed_when_the_limit_passes (0.00s)
2026-11-27T18:47:07.2559892Z     --- PASS: TestInstallSettling/a_workload_of_a_strategy_other_than_the_rolling_one_appears (0.00s)
2026-11-27T18:47:07.2560776Z     --- PASS: TestInstallSettling/and_one_that_appeared_before_the_first_observation_ends_the_settling_there,_with_no_window (0.00s)
2026-11-27T18:47:07.2561806Z     --- PASS: TestInstallSettling/a_workload_the_tool_cannot_judge_seen_before_the_first_observation,_and_a_failure_before_it (0.00s)
2026-11-27T18:47:07.2562661Z     --- PASS: TestInstallSettling/and_a_break_that_began_after_the_first_observation_is_not_counted (0.00s)
2026-11-27T18:47:07.2563260Z     --- PASS: TestInstallSettling/the_settle_time_runs_past_the_limit (0.00s)
2026-11-27T18:47:07.2563673Z     --- PASS: TestInstallSettling/a_break_after_the_limit,_before_the_install_has_settled,_fails_the_install (0.00s)
2026-11-27T18:47:07.2564145Z     --- PASS: TestInstallSettling/a_break_that_is_over_when_the_limit_passes_does_not_fail_the_install (0.00s)
2026-11-27T18:47:07.2564596Z     --- PASS: TestInstallSettling/a_break_that_lasts_across_the_limit_fails_the_install_at_the_limit (0.00s)
2026-11-27T18:47:07.2565327Z     --- PASS: TestInstallSettling/an_install_that_failed_before_the_first_observation_has_no_window (0.00s)
2026-11-27T18:47:07.2566128Z     --- PASS: TestInstallSettling/a_change_after_the_install_had_settled,_told_before_anyone_asked,_is_not_counted (0.00s)
2026-11-27T18:47:07.2566563Z     --- PASS: TestInstallSettling/the_watch_missed_changes_in_the_install_window (0.00s)
2026-11-27T18:47:07.2567077Z     --- PASS: TestInstallSettling/the_watch_missed_changes_before_the_first_observation_only (0.00s)
2026-11-27T18:47:07.2567921Z     --- PASS: TestInstallSettling/a_watch_that_went_down_before_the_first_observation_and_is_still_down (0.00s)
2026-11-27T18:47:07.2568769Z --- PASS: TestInstallSettlingBeforeTheInstallIsReady (0.00s)
2026-11-27T18:47:07.2569439Z --- PASS: TestInstallSettlingOwnsItsLists (0.00s)
2026-11-27T18:47:07.2591017Z --- PASS: TestTransitionFinish (0.00s)
2026-11-27T18:47:07.2591639Z     --- PASS: TestTransitionFinish/nothing_changes_and_the_command_exits_at_+2_s (0.00s)
2026-11-27T18:47:07.2592319Z     --- PASS: TestTransitionFinish/the_rollout_completes_at_+20_s_and_the_command_exits_at_+30_s (0.00s)
2026-11-27T18:47:07.2592933Z     --- PASS: TestTransitionFinish/an_old_pod_is_terminating_until_+26_s (0.00s)
2026-11-27T18:47:07.2593555Z     --- PASS: TestTransitionFinish/a_pod_created_and_deleted_in_the_transition_must_be_gone_too (0.00s)
2026-11-27T18:47:07.2594198Z     --- PASS: TestTransitionFinish/a_pod_created_in_the_settle_time_starts_it_again (0.00s)
2026-11-27T18:47:07.2594808Z     --- PASS: TestTransitionFinish/a_pod_deleted_in_the_settle_time_starts_it_again (0.00s)
2026-11-27T18:47:07.2595383Z     --- PASS: TestTransitionFinish/a_new_spec_in_the_settle_time_starts_it_again (0.00s)
2026-11-27T18:47:07.2596017Z     --- PASS: TestTransitionFinish/readiness_and_a_restarted_container_do_not_start_it_again (0.00s)
2026-11-27T18:47:07.2596688Z     --- PASS: TestTransitionFinish/a_workload_that_the_selector_leaves_out_counts_for_nothing (0.00s)
2026-11-27T18:47:07.2597315Z     --- PASS: TestTransitionFinish/not_finished_at_the_timeout:_a_stall (0.00s)
2026-11-27T18:47:07.2598156Z     --- PASS: TestTransitionFinish/a_change_after_the_timeout,_in_the_settle_time,_is_a_stall_at_that_moment (0.00s)
2026-11-27T18:47:07.2598831Z     --- PASS: TestTransitionFinish/the_settle_time_may_run_past_the_timeout (0.00s)
2026-11-27T18:47:07.2599450Z     --- PASS: TestTransitionFinish/a_StatefulSet's_pod_back_under_its_name_is_a_new_pod (0.00s)
2026-11-27T18:47:07.2600208Z     --- PASS: TestTransitionFinish/a_workload_deleted_in_the_transition_is_complete_when_its_pods_are_gone (0.00s)
2026-11-27T18:47:07.2601037Z     --- PASS: TestTransitionFinish/a_workload_that_updates_with_OnDelete_does_not_hold_the_transition_open (0.00s)
2026-11-27T18:47:07.2601866Z     --- PASS: TestTransitionFinish/a_StatefulSet's_new_pod_under_the_old_name_is_deleted_in_the_settle_time (0.00s)
2026-11-27T18:47:07.2602702Z     --- PASS: TestTransitionFinish/a_workload_deleted_in_the_transition_waits_for_pods_not_yet_marked_for_deletion (0.00s)
2026-11-27T18:47:07.2603432Z     --- PASS: TestTransitionFinish/a_pod_gone_with_no_deletion_seen_is_deleted_then,_once (0.00s)
2026-11-27T18:47:07.2604134Z     --- PASS: TestTransitionFinish/a_ReplicaSet_that_loses_its_Deployment_takes_its_pods_out_of_it (0.00s)
2026-11-27T18:47:07.2604802Z     --- PASS: TestTransitionFinish/a_workload_created_in_the_settle_time_starts_it_again (0.00s)
2026-11-27T18:47:07.2605582Z     --- PASS: TestTransitionFinish/a_workload_made_again_under_its_old_name_in_the_settle_time_starts_it_again (0.00s)
2026-11-27T18:47:07.2606467Z     --- PASS: TestTransitionFinish/a_workload_whose_UID_changes_with_no_Gone_told_starts_it_again (0.00s)
2026-11-27T18:47:07.2607242Z     --- PASS: TestTransitionFinish/a_workload_told_again_after_its_Gone_starts_it_again,_with_its_old_UID_and_generation (0.00s)
2026-11-27T18:47:07.2608058Z     --- PASS: TestTransitionFinish/a_workload_first_told_gone_waits_for_its_pods (0.00s)
2026-11-27T18:47:07.2608630Z     --- PASS: TestTransitionFinish/an_object_of_another_kind_changes_nothing (0.00s)
2026-11-27T18:47:07.2609366Z     --- PASS: TestTransitionFinish/a_workload_that_stops_matching_while_it_rolls_out_stays_watched_in_the_transition (0.00s)
2026-11-27T18:47:07.2610163Z     --- PASS: TestTransitionFinish/a_workload_that_joins_in_the_settle_time_must_complete_its_rollout (0.00s)
2026-11-27T18:47:07.2610964Z     --- PASS: TestTransitionFinish/a_workload_that_a_Set_before_the_transition_shows_matching_is_watched_from_its_start (0.00s)
2026-11-27T18:47:07.2611846Z     --- PASS: TestTransitionFinish/a_workload_outside_the_selector_that_changes_in_the_transition_counts_for_nothing (0.00s)
2026-11-27T18:47:07.2612575Z     --- PASS: TestTransitionFinish/a_workload_told_gone_twice_is_deleted_once (0.00s)
2026-11-27T18:47:07.2613251Z     --- PASS: TestTransitionFinish/the_rollout_completes_after_the_timeout:_a_stall_at_the_timeout (0.00s)
2026-11-27T18:47:07.2614105Z     --- PASS: TestTransitionFinish/the_last_old_pod_is_gone_after_the_timeout:_a_stall_at_the_timeout (0.00s)
2026-11-27T18:47:07.2614818Z     --- PASS: TestTransitionFinish/the_command_exits_after_the_timeout:_a_stall_at_the_timeout (0.00s)
2026-11-27T18:47:07.2615519Z     --- PASS: TestTransitionFinish/a_pod_created_when_the_settle_time_ends_belongs_to_no_transition (0.00s)
2026-11-27T18:47:07.2616240Z     --- PASS: TestTransitionFinish/a_change_exactly_at_the_timeout,_in_the_settle_time,_is_a_stall (0.00s)
2026-11-27T18:47:07.2617002Z     --- PASS: TestTransitionFinish/a_workload_deleted_in_the_settle_time_holds_the_window_until_its_pods_are_gone (0.00s)
2026-11-27T18:47:07.2617912Z --- PASS: TestBeginTwice (0.00s)
2026-11-27T18:47:07.2618408Z --- PASS: TestStateBeforeBegin (0.00s)
2026-11-27T18:47:07.2618899Z --- PASS: TestWorkloadString (0.00s)
2026-11-27T18:48:02.1738863Z --- PASS: TestWatcherOnACluster (54.92s)
2026-11-27T18:48:02.2747623Z --- PASS: TestStartSeesWhatExists (0.10s)
2026-11-27T18:48:02.2748333Z --- PASS: TestStartRejectsASelector (0.00s)
2026-11-27T18:48:02.9805486Z --- PASS: TestInstallReady (0.71s)
2026-11-27T18:48:02.9806096Z     --- PASS: TestInstallReady/ready_once_the_Deployment_is_complete (0.40s)
2026-11-27T18:48:02.9806642Z     --- PASS: TestInstallReady/nothing_to_watch (0.30s)
2026-11-27T18:48:03.4852506Z --- PASS: TestInstallSettled (0.50s)
2026-11-27T18:48:03.4853058Z     --- PASS: TestInstallSettled/it_settles_limits.settle_after_the_first_observation (0.30s)
2026-11-27T18:48:03.4853475Z     --- PASS: TestInstallSettled/when_ctx_ends_it_returns_what_it_has (0.10s)
2026-11-27T18:48:03.4853945Z     --- PASS: TestInstallSettled/when_ctx_ends_before_the_first_observation,_Ended_is_the_first_observation (0.10s)
2026-11-27T18:48:03.7877671Z --- PASS: TestTransition (0.30s)
2026-11-27T18:48:03.8992181Z --- PASS: TestTransitionCancelled (0.11s)
2026-11-27T18:48:04.8051769Z --- PASS: TestProbed (0.91s)
2026-11-27T18:48:04.8052211Z     --- PASS: TestProbed/the_Service's_endpoints_are_pods_of_orders (0.30s)
2026-11-27T18:48:04.8052608Z     --- PASS: TestProbed/no_Service (0.30s)
2026-11-27T18:48:04.8053012Z     --- PASS: TestProbed/a_Service_in_another_namespace (0.30s)
2026-11-27T18:48:05.4094373Z --- PASS: TestUseService (0.60s)
2026-11-27T18:48:05.5112528Z --- PASS: TestStopTwice (0.10s)
2026-11-27T18:48:05.8127082Z --- PASS: TestStartWaitsForWhatExists (0.30s)
2026-11-27T18:48:06.2170440Z --- PASS: TestNothingIsToldAfterStop (0.40s)
2026-11-27T18:48:06.8206254Z --- PASS: TestStopWaitsForAHandler (0.60s)
2026-11-27T18:48:06.9315795Z --- PASS: TestTransitionCancelledInTheSettleTime (0.11s)
2026-11-27T18:48:07.0430217Z --- PASS: TestTransitionCancelledAfterItsWindowEnded (0.11s)
2026-11-27T18:48:07.5443458Z --- PASS: TestTransitionsBackToBack (0.50s)
2026-11-27T18:48:07.8469079Z --- PASS: TestTransitionThatNeedsNoNewPod (0.30s)
2026-11-27T18:48:08.5153041Z --- PASS: TestWhatAnInformerFindsAtAListAgain (0.67s)
2026-11-27T18:48:08.5153528Z     --- PASS: TestWhatAnInformerFindsAtAListAgain/a_pod's_deletion_with_its_last_state (0.11s)
2026-11-27T18:48:08.5154010Z     --- PASS: TestWhatAnInformerFindsAtAListAgain/a_pod's_deletion_with_no_state:_the_object_last_told_under_its_key (0.11s)
2026-11-27T18:48:08.5154513Z     --- PASS: TestWhatAnInformerFindsAtAListAgain/the_deletion_of_a_pod_never_told,_with_its_last_state (0.11s)
2026-11-27T18:48:08.5154940Z     --- PASS: TestWhatAnInformerFindsAtAListAgain/a_Deployment's_deletion_with_no_state (0.11s)
2026-11-27T18:48:08.5155326Z     --- PASS: TestWhatAnInformerFindsAtAListAgain/a_pod_made_again_under_its_name (0.11s)
2026-11-27T18:48:08.5156153Z     --- PASS: TestWhatAnInformerFindsAtAListAgain/a_Deployment_made_again_under_its_name (0.11s)
2026-11-27T18:48:09.5345648Z --- PASS: TestProbedFromEitherReading (1.02s)
2026-11-27T18:48:09.5346312Z     --- PASS: TestProbedFromEitherReading/behind_the_Service_at_the_second_reading_alone (0.30s)
2026-11-27T18:48:09.5346877Z     --- PASS: TestProbedFromEitherReading/both_readings_fail (0.30s)
2026-11-27T18:48:09.5347183Z     --- PASS: TestProbedFromEitherReading/the_first_reading_fails (0.30s)
2026-11-27T18:48:09.5347721Z     --- PASS: TestProbedFromEitherReading/no_second_reading_after_ctx_ended (0.11s)
2026-11-27T18:48:09.6358469Z --- PASS: TestInstallReadyGivesCtxsError (0.10s)
2026-11-27T18:48:09.6360450Z --- PASS: TestStartStopsWhenCtxEnds (0.00s)
2026-11-27T18:48:11.4479954Z --- PASS: TestMissedChanges (1.81s)
2026-11-27T18:48:11.4480331Z     --- PASS: TestMissedChanges/an_update_with_the_same_resource_version (0.30s)
2026-11-27T18:48:11.4480622Z     --- PASS: TestMissedChanges/a_tombstone (0.30s)
2026-11-27T18:48:11.4480958Z     --- PASS: TestMissedChanges/an_update_with_the_same_resource_version_and_no_window_open (0.30s)
2026-11-27T18:48:11.4481430Z     --- PASS: TestMissedChanges/an_update_with_the_same_resource_version_after_the_start_and_before_Begin (0.30s)
2026-11-27T18:48:11.4481816Z     --- PASS: TestMissedChanges/an_update_whose_resource_version_changed (0.30s)
2026-11-27T18:48:11.4482135Z     --- PASS: TestMissedChanges/an_update_with_no_resource_version (0.30s)
2026-11-27T18:48:11.5695974Z --- PASS: TestAListAgainIsMissed (0.12s)
2026-11-27T18:48:11.8718603Z --- PASS: TestAHeldReadingHoldsNothing (0.30s)
2026-11-27T18:48:12.4735182Z --- PASS: TestReadingsEndWithTheWindow (0.60s)
2026-11-27T18:48:12.7751236Z --- PASS: TestTheExitIsToldWhenItComes (0.30s)
2026-11-27T18:48:13.3789030Z --- PASS: TestOnlyTheNamespaceIsWatched (0.60s)
2026-11-27T18:48:13.3790028Z     --- PASS: TestOnlyTheNamespaceIsWatched/a_transition_lists_deployment/orders_alone (0.30s)
2026-11-27T18:48:13.3791080Z     --- PASS: TestOnlyTheNamespaceIsWatched/nothing_in_the_namespace_is_nothing_to_watch (0.30s)
2026-11-27T18:48:14.2811627Z --- PASS: TestALateLastReadingCountsForNothing (0.90s)
2026-11-27T18:48:20.0489726Z --- PASS: TestTheScriptRunsTheToolFromItsImage (4.60s)
2026-11-27T18:48:20.0615497Z --- PASS: TestTheScriptStartsTheToolInItsPlace (0.01s)
2026-11-27T18:48:20.0739422Z --- PASS: TestTheScriptStartsTheToolWithoutOut (0.01s)
2026-11-27T18:48:20.1105092Z --- PASS: TestTheScriptsEngineRule (0.04s)
2026-11-27T18:48:20.1105487Z     --- PASS: TestTheScriptsEngineRule/the_variable_names_nerdctl (0.00s)
2026-11-27T18:48:20.1105839Z     --- PASS: TestTheScriptsEngineRule/the_variable_names_finch (0.00s)
2026-11-27T18:48:20.1106168Z     --- PASS: TestTheScriptsEngineRule/the_variable_names_nerdctl.lima (0.00s)
2026-11-27T18:48:20.1106543Z     --- PASS: TestTheScriptsEngineRule/the_variable_names_no_engine_kind_takes (0.00s)
2026-11-27T18:48:20.1106895Z     --- PASS: TestTheScriptsEngineRule/docker's_-v_fails (0.00s)
2026-11-27T18:48:20.1107219Z     --- PASS: TestTheScriptsEngineRule/docker's_banner_is_another (0.00s)
2026-11-27T18:48:20.1108025Z     --- PASS: TestTheScriptsEngineRule/docker's_-v_prints_a_second_line (0.00s)
2026-11-27T18:48:20.1108381Z     --- PASS: TestTheScriptsEngineRule/docker_comes_before_nerdctl (0.00s)
2026-11-27T18:48:20.1108769Z     --- PASS: TestTheScriptsEngineRule/docker's_banner_is_later_in_its_first_line (0.00s)
2026-11-27T18:48:20.1109109Z     --- PASS: TestTheScriptsEngineRule/only_podman (0.00s)
2026-11-27T18:48:20.1109370Z     --- PASS: TestTheScriptsEngineRule/no_engine (0.00s)
2026-11-27T18:48:20.1212193Z --- PASS: TestTheScriptsImageReference (0.01s)
2026-11-27T18:48:20.1212716Z     --- PASS: TestTheScriptsImageReference/an_image_with_a_digest (0.00s)
2026-11-27T18:48:20.1213393Z     --- PASS: TestTheScriptsImageReference/an_image_and_the_release's_digest (0.00s)
2026-11-27T18:48:20.1213812Z     --- PASS: TestTheScriptsImageReference/an_image_with_a_tag_and_the_release's_digest (0.00s)
2026-11-27T18:48:20.1214223Z     --- PASS: TestTheScriptsImageReference/the_release's_repository_and_digest (0.00s)
2026-11-27T18:48:20.1214551Z     --- PASS: TestTheScriptsImageReference/nothing_set (0.00s)
2026-11-27T18:48:20.1214863Z     --- PASS: TestTheScriptsImageReference/an_image_and_no_digest (0.00s)
2026-11-27T18:48:20.2037866Z --- PASS: TestTheScriptFailsWithExit2AndWritesNoOutput (0.08s)
2026-11-27T18:48:20.2038402Z     --- PASS: TestTheScriptFailsWithExit2AndWritesNoOutput/not_Linux (0.00s)
2026-11-27T18:48:20.2038903Z     --- PASS: TestTheScriptFailsWithExit2AndWritesNoOutput/a_uname_that_fails (0.00s)
2026-11-27T18:48:20.2039329Z     --- PASS: TestTheScriptFailsWithExit2AndWritesNoOutput/no_engine (0.01s)
2026-11-27T18:48:20.2039715Z     --- PASS: TestTheScriptFailsWithExit2AndWritesNoOutput/no_image (0.00s)
2026-11-27T18:48:20.2040128Z     --- PASS: TestTheScriptFailsWithExit2AndWritesNoOutput/no_digest (0.00s)
2026-11-27T18:48:20.2040563Z     --- PASS: TestTheScriptFailsWithExit2AndWritesNoOutput/a_pull_that_fails (0.01s)
2026-11-27T18:48:20.2041060Z     --- PASS: TestTheScriptFailsWithExit2AndWritesNoOutput/no_folder_for_the_binary (0.01s)
2026-11-27T18:48:20.2041789Z     --- PASS: TestTheScriptFailsWithExit2AndWritesNoOutput/a_create_that_fails (0.01s)
2026-11-27T18:48:20.2042273Z     --- PASS: TestTheScriptFailsWithExit2AndWritesNoOutput/an_export_that_fails (0.01s)
2026-11-27T18:48:20.2042759Z     --- PASS: TestTheScriptFailsWithExit2AndWritesNoOutput/an_image_without_the_binary (0.01s)
2026-11-27T18:48:20.2043297Z     --- PASS: TestTheScriptFailsWithExit2AndWritesNoOutput/a_workspace_that_cannot_be_entered (0.01s)
2026-11-27T18:48:20.2043775Z     --- PASS: TestTheScriptFailsWithExit2AndWritesNoOutput/a_binary_that_cannot_start (0.01s)
2026-11-27T18:48:20.2179404Z --- PASS: TestTheScriptPrintsNothingOfUnameBeforeItsStopLine (0.01s)
2026-11-27T18:48:20.2180111Z     --- PASS: TestTheScriptPrintsNothingOfUnameBeforeItsStopLine/uname_reports_Linux_and_prints_a_command_on_standard_error (0.01s)
2026-11-27T18:48:20.2180797Z     --- PASS: TestTheScriptPrintsNothingOfUnameBeforeItsStopLine/uname_reports_another_system,_with_a_command_on_each_stream (0.00s)
2026-11-27T18:48:20.2389074Z --- PASS: TestTheScriptStopsWorkflowCommandsFirst (0.02s)
2026-11-27T18:48:20.2499896Z --- PASS: TestTheScriptsStopLine (0.01s)
2026-11-27T18:48:20.2500201Z     --- PASS: TestTheScriptsStopLine/a_UUID (0.00s)
2026-11-27T18:48:20.2500512Z     --- PASS: TestTheScriptsStopLine/no_line_end (0.00s)
2026-11-27T18:48:20.2500822Z     --- PASS: TestTheScriptsStopLine/a_path_that_does_not_exist (0.00s)
2026-11-27T18:48:20.2501162Z     --- PASS: TestTheScriptsStopLine/an_empty_file (0.00s)
2026-11-27T18:48:20.2501459Z     --- PASS: TestTheScriptsStopLine/standard_output_is_closed (0.00s)
2026-11-27T18:48:20.2501777Z     --- PASS: TestTheScriptsStopLine/standard_error_is_closed (0.00s)
2026-11-27T18:48:20.2502378Z --- PASS: TestActionYAML (0.00s)
2026-11-27T18:48:20.2503060Z --- PASS: TestTheDockerfile (0.00s)
2026-11-27T18:48:20.2523094Z --- PASS: TestTheActionTestsInputsLoad (0.00s)
2026-11-27T18:48:20.2523593Z     --- PASS: TestTheActionTestsInputsLoad/pass (0.00s)
2026-11-27T18:48:20.2524111Z     --- PASS: TestTheActionTestsInputsLoad/pass_without_the_variable (0.00s)
2026-11-27T18:48:20.2524529Z     --- PASS: TestTheActionTestsInputsLoad/fail (0.00s)
2026-11-27T18:48:20.2524928Z     --- PASS: TestTheActionTestsInputsLoad/cut (0.00s)
```

The `helm-3` and `helm-4` jobs: each has 138 `--- PASS` lines, and no `--- FAIL` or `--- SKIP` line; both ran the same tests in the same order. Each ran the 32 fixture subtests under its own Helm major and the fourteenth row's control, which runs under Helm 3 in both jobs by design (`e2e/fixtures_test.go`, lines 274 to 294, read). The fixture subtests took 3,539.26 s under Helm 3 and 3,670.03 s under Helm 4 by their own times; `docs/proofs/p32-step6-workflows.md` gives 4,299.14 s and 3,978.61 s for them on 2026-11-22 (read there), so these took 0.82 and 0.92 of those times.

The harness's start, from the step's `go test` line to the first test (`=== RUN   TestSetupCommandFails`), took 15.5 s in `helm-3` and 60.8 s in `helm-4`; `docs/proofs/p32-step6-workflows.md` gives 144.7 s and 124.0 s for it on 2026-11-22 (read there). Before the tests ran, `actions/setup-go` had, as its lines under "Machine and versions" say, found the cache saved for the `ubuntu-24.04` image in `helm-3` and found none and saved one for the `ubuntu-26.04` image in `helm-4`.

The fourteenth row's control, in `helm-3`. Its lines end where the row's own subtest opens; `go test` prints a subtest's `--- PASS` line when its parent test ends, so that line is logged after the subtest's, and is quoted last:

```
2026-11-27T19:32:38.4094613Z === RUN   TestChartDeniesTrafficByDefault/control
2026-11-27T19:33:49.2511748Z     fixtures_test.go:285: chart-denies-traffic-control (helm-3):
2026-11-27T19:33:49.2513076Z         rollproof: NOT VERIFIED   orders (Helm) on Kubernetes v1.37.0 in kind
2026-11-27T19:33:49.2518937Z                     3,001 checks, 3,001 failed (timeout 2,995, refused 6), the last 0.0 s before the steady window
2026-11-27T19:33:49.2520396Z         steady     10.0 s    2,000    2,000              -         -     1 ms   NOT VERIFIED
2026-11-27T19:33:49.2522022Z         steady: 2000 checks failed (timeout 2000), first at 19:33:35.29   [steady-not-clean]
2026-11-27T19:33:49.2522522Z           no check has passed since the probe started: every check failed for 25.0 s
2026-11-27T19:35:51.2822797Z     --- PASS: TestChartDeniesTrafficByDefault/control (70.85s)
```

In `helm-4`:

```
2026-11-27T19:35:09.7966116Z === RUN   TestChartDeniesTrafficByDefault/control
2026-11-27T19:36:29.4173761Z     fixtures_test.go:285: chart-denies-traffic-control (helm-3):
2026-11-27T19:36:29.4175238Z         rollproof: NOT VERIFIED   orders (Helm) on Kubernetes v1.37.0 in kind
2026-11-27T19:36:29.4179654Z                     3,001 checks, 3,000 failed (timeout 2,994, refused 6), the last 0.0 s before the steady window
2026-11-27T19:36:29.4180715Z         steady     10.0 s    2,000    2,000              -         -     2 ms   NOT VERIFIED
2026-11-27T19:36:29.4181780Z         steady: 2000 checks failed (timeout 2000), first at 19:36:13.94   [steady-not-clean]
2026-11-27T19:38:37.5872524Z     --- PASS: TestChartDeniesTrafficByDefault/control (79.63s)
```

Both controls gave `steady-not-clean`, as the plan's pair asks: in `helm-3`, 3,001 of the control's 3,001 checks failed and the tool also printed that no check had passed; in `helm-4`, 3,000 of the control's 3,001 checks failed, 1 did not, so the tool did not print that no check had passed (the blocks above).

Every test and subtest of `helm-3`:

```
2026-11-27T18:50:03.2027462Z --- PASS: TestSetupCommandFails (62.07s)
2026-11-27T18:50:03.2028397Z     --- PASS: TestSetupCommandFails/helm-3 (62.07s)
2026-11-27T18:52:05.5036658Z --- PASS: TestCommandsDriverWithAllThreeCommands (122.30s)
2026-11-27T18:52:05.5038166Z     --- PASS: TestCommandsDriverWithAllThreeCommands/helm-3 (122.30s)
2026-11-27T18:53:40.4362290Z --- PASS: TestCommandsDriverWithInstallAndDeployOnly (94.93s)
2026-11-27T18:53:40.4362973Z     --- PASS: TestCommandsDriverWithInstallAndDeployOnly/helm-3 (94.93s)
2026-11-27T18:55:43.5926742Z --- PASS: TestSetupMakesTheProbeNamespaceRestricted (123.16s)
2026-11-27T18:55:43.5928201Z     --- PASS: TestSetupMakesTheProbeNamespaceRestricted/helm-3 (123.16s)
2026-11-27T18:55:43.6014964Z --- PASS: TestConfigOfEachShapeLoads (0.01s)
2026-11-27T18:55:43.6015533Z     --- PASS: TestConfigOfEachShapeLoads/both_releases (0.00s)
2026-11-27T18:55:43.6016125Z     --- PASS: TestConfigOfEachShapeLoads/no_previous (0.00s)
2026-11-27T18:55:43.6016659Z     --- PASS: TestConfigOfEachShapeLoads/candidate_runs_previous (0.00s)
2026-11-27T18:55:43.6017100Z     --- PASS: TestConfigOfEachShapeLoads/node_image (0.00s)
2026-11-27T18:55:43.6017599Z     --- PASS: TestConfigOfEachShapeLoads/setup (0.00s)
2026-11-27T18:55:43.6018293Z     --- PASS: TestConfigOfEachShapeLoads/three_commands (0.00s)
2026-11-27T18:55:43.6018838Z     --- PASS: TestConfigOfEachShapeLoads/two_commands (0.00s)
2026-11-27T18:55:43.6019383Z     --- PASS: TestConfigOfEachShapeLoads/fixture_probe (0.00s)
2026-11-27T18:55:43.6020066Z     --- PASS: TestConfigOfEachShapeLoads/TCP_loop (0.00s)
2026-11-27T18:55:43.6020611Z     --- PASS: TestConfigOfEachShapeLoads/limits (0.00s)
2026-11-27T18:55:43.6021050Z     --- PASS: TestConfigOfEachShapeLoads/through_the_mirror (0.00s)
2026-11-27T18:55:43.6021533Z     --- PASS: TestConfigOfEachShapeLoads/the_mirror_replaces_its_host (0.00s)
2026-11-27T18:55:43.6022189Z --- PASS: TestReadMarks (0.00s)
2026-11-27T18:55:43.6023609Z --- PASS: TestRenderConfigRejectsAnUnknownName (0.00s)
2026-11-27T18:55:43.6024046Z     --- PASS: TestRenderConfigRejectsAnUnknownName/custom_image (0.00s)
2026-11-27T18:55:43.6024533Z     --- PASS: TestRenderConfigRejectsAnUnknownName/command_role (0.00s)
2026-11-27T18:55:43.6025432Z --- PASS: TestManifestValuesLetTheFixtureSetImage (0.00s)
2026-11-27T18:55:43.9233555Z --- PASS: TestActionLoop (0.32s)
2026-11-27T18:55:43.9234406Z     --- PASS: TestActionLoop/the_action_reports_done (0.00s)
2026-11-27T18:55:43.9234812Z     --- PASS: TestActionLoop/the_action_fails (0.00s)
2026-11-27T18:55:43.9235170Z     --- PASS: TestActionLoop/the_run_ends_first (0.00s)
2026-11-27T18:55:43.9235706Z     --- PASS: TestActionLoop/the_run_ends_between_two_calls (0.32s)
2026-11-27T18:55:43.9236597Z --- PASS: TestInterruptWaitsForTheDeploysStart (0.00s)
2026-11-27T18:55:43.9237276Z --- PASS: TestRestartComesAfterTheDelay (0.00s)
2026-11-27T18:57:20.4710388Z --- PASS: TestCustomProbeGoesSilentOnTheCandidate (96.55s)
2026-11-27T18:57:20.4711189Z     --- PASS: TestCustomProbeGoesSilentOnTheCandidate/helm-3 (96.55s)
2026-11-27T18:58:56.8388183Z --- PASS: TestCustomProbeExitsOnTheCandidate (96.37s)
2026-11-27T18:58:56.8388904Z     --- PASS: TestCustomProbeExitsOnTheCandidate/helm-3 (96.37s)
2026-11-27T19:01:00.9635989Z --- PASS: TestTCPLoopJudgesTheFirstFixture (124.12s)
2026-11-27T19:01:00.9636655Z     --- PASS: TestTCPLoopJudgesTheFirstFixture/helm-3 (124.12s)
2026-11-27T19:03:02.5502829Z --- PASS: TestProbeFailsItsFirstFiveSeconds (121.59s)
2026-11-27T19:03:02.5503302Z     --- PASS: TestProbeFailsItsFirstFiveSeconds/helm-3 (121.59s)
2026-11-27T19:04:13.9724334Z --- PASS: TestProbeFailsItsFirstTwentySeconds (71.42s)
2026-11-27T19:04:13.9724964Z     --- PASS: TestProbeFailsItsFirstTwentySeconds/helm-3 (71.42s)
2026-11-27T19:05:50.7207240Z --- PASS: TestTCPLoopJudgesACandidateWithoutReadiness (96.75s)
2026-11-27T19:05:50.7207998Z     --- PASS: TestTCPLoopJudgesACandidateWithoutReadiness/helm-3 (96.75s)
2026-11-27T19:08:01.7878739Z --- PASS: TestCustomProbeIgnoresSIGTERM (131.07s)
2026-11-27T19:08:01.7879534Z     --- PASS: TestCustomProbeIgnoresSIGTERM/helm-3 (131.07s)
2026-11-27T19:10:04.3459693Z --- PASS: TestTCPLoopInARestrictedProbeNamespace (122.56s)
2026-11-27T19:10:04.3460434Z     --- PASS: TestTCPLoopInARestrictedProbeNamespace/helm-3 (122.56s)
2026-11-27T19:12:04.3145147Z --- PASS: TestFirstFixture (119.97s)
2026-11-27T19:12:04.3146761Z     --- PASS: TestFirstFixture/helm-3 (119.97s)
2026-11-27T19:13:41.5113523Z --- PASS: TestCandidateWithoutReadiness (97.20s)
2026-11-27T19:13:41.5114637Z     --- PASS: TestCandidateWithoutReadiness/helm-3 (97.20s)
2026-11-27T19:15:34.6427422Z --- PASS: TestCandidateExitsOnSIGTERM (113.13s)
2026-11-27T19:15:34.6428239Z     --- PASS: TestCandidateExitsOnSIGTERM/helm-3 (113.13s)
2026-11-27T19:18:15.4582685Z --- PASS: TestNoRoomForTheExtraPod (160.82s)
2026-11-27T19:18:15.4583244Z     --- PASS: TestNoRoomForTheExtraPod/helm-3 (160.82s)
2026-11-27T19:19:24.2146220Z --- PASS: TestPreviousFailsEveryRequest (68.76s)
2026-11-27T19:19:24.2146858Z     --- PASS: TestPreviousFailsEveryRequest/helm-3 (68.76s)
2026-11-27T19:21:31.3970406Z --- PASS: TestFirstFixtureOnTheOldestNodeImage (127.18s)
2026-11-27T19:21:31.3971071Z     --- PASS: TestFirstFixtureOnTheOldestNodeImage/helm-3 (127.18s)
2026-11-27T19:22:58.7674327Z --- PASS: TestPreviousExitsOnSIGTERM (87.37s)
2026-11-27T19:22:58.7674913Z     --- PASS: TestPreviousExitsOnSIGTERM/helm-3 (87.37s)
2026-11-27T19:24:22.5227636Z --- PASS: TestDeployReplacesNoPod (83.76s)
2026-11-27T19:24:22.5228718Z     --- PASS: TestDeployReplacesNoPod/helm-3 (83.76s)
2026-11-27T19:26:38.5010990Z --- PASS: TestPreviousNeverReady (135.98s)
2026-11-27T19:26:38.5011731Z     --- PASS: TestPreviousNeverReady/helm-3 (135.98s)
2026-11-27T19:28:15.1843400Z --- PASS: TestNoPreviousRelease (96.68s)
2026-11-27T19:28:15.1843812Z     --- PASS: TestNoPreviousRelease/helm-3 (96.68s)
2026-11-27T19:30:19.8155660Z --- PASS: TestBothReleasesEndWithoutDraining (124.63s)
2026-11-27T19:30:19.8156209Z     --- PASS: TestBothReleasesEndWithoutDraining/helm-3 (124.63s)
2026-11-27T19:32:38.4093157Z --- PASS: TestSecondRolloutAfterTheFirstFinished (138.59s)
2026-11-27T19:32:38.4093786Z     --- PASS: TestSecondRolloutAfterTheFirstFinished/helm-3 (138.59s)
2026-11-27T19:35:51.2822244Z --- PASS: TestChartDeniesTrafficByDefault (192.87s)
2026-11-27T19:35:51.2822797Z     --- PASS: TestChartDeniesTrafficByDefault/control (70.85s)
2026-11-27T19:35:51.2823307Z     --- PASS: TestChartDeniesTrafficByDefault/helm-3 (122.02s)
2026-11-27T19:37:57.8821477Z --- PASS: TestStatefulSet (126.60s)
2026-11-27T19:37:57.8821924Z     --- PASS: TestStatefulSet/helm-3 (126.60s)
2026-11-27T19:39:56.1423743Z --- PASS: TestDaemonSet (118.26s)
2026-11-27T19:39:56.1424134Z     --- PASS: TestDaemonSet/helm-3 (118.26s)
2026-11-27T19:42:44.4314548Z --- PASS: TestHooksLongerThanTheSettleTime (168.29s)
2026-11-27T19:42:44.4315032Z     --- PASS: TestHooksLongerThanTheSettleTime/helm-3 (168.29s)
2026-11-27T19:44:49.1385784Z --- PASS: TestCandidateFailsAfterTheRolloutFinished (124.71s)
2026-11-27T19:44:49.1386331Z     --- PASS: TestCandidateFailsAfterTheRolloutFinished/helm-3 (124.71s)
2026-11-27T19:46:08.1040603Z --- PASS: TestCandidateValueBreaksTheSchema (78.97s)
2026-11-27T19:46:08.1041070Z     --- PASS: TestCandidateValueBreaksTheSchema/helm-3 (78.97s)
2026-11-27T19:47:16.5187303Z --- PASS: TestSIGINTDuringTheDeploy (68.41s)
2026-11-27T19:47:16.5188073Z     --- PASS: TestSIGINTDuringTheDeploy/helm-3 (68.41s)
2026-11-27T19:49:11.5606485Z --- PASS: TestCandidateChangesOnlyAnUnprobedWorkload (115.04s)
2026-11-27T19:49:11.5607142Z     --- PASS: TestCandidateChangesOnlyAnUnprobedWorkload/helm-3 (115.04s)
2026-11-27T19:49:11.5609648Z --- PASS: TestLoopbackProblems (0.00s)
2026-11-27T19:49:11.5609945Z     --- PASS: TestLoopbackProblems/loopback_only (0.00s)
2026-11-27T19:49:11.5610284Z     --- PASS: TestLoopbackProblems/no_CONN_line (0.00s)
2026-11-27T19:49:11.5610612Z     --- PASS: TestLoopbackProblems/loopback_and_a_node (0.00s)
2026-11-27T19:49:11.5610929Z     --- PASS: TestLoopbackProblems/a_node_only (0.00s)
2026-11-27T19:49:11.5611255Z     --- PASS: TestLoopbackProblems/an_address_with_no_port (0.00s)
2026-11-27T19:49:11.5613973Z --- PASS: TestCutScript (0.00s)
2026-11-27T19:49:11.5623239Z --- PASS: TestNewAuthority (0.00s)
2026-11-27T19:49:11.5627891Z --- PASS: TestPasswordForms (0.00s)
2026-11-27T19:49:11.5629169Z --- PASS: TestFindForms (0.00s)
2026-11-27T19:49:11.5629574Z     --- PASS: TestFindForms/names_the_index_and_skips_an_empty_form (0.00s)
2026-11-27T19:49:11.5630021Z     --- PASS: TestFindForms/sorts_every_file_and_form (0.00s)
2026-11-27T19:49:11.5630582Z --- PASS: TestParseStandinLog (0.00s)
2026-11-27T19:49:11.5631087Z --- PASS: TestHelmMajors (0.00s)
2026-11-27T19:49:11.5631719Z --- PASS: TestRunEnvKeepsTheJobsGitHubFilesOut (0.00s)
2026-11-27T19:49:11.5634375Z --- PASS: TestSummaryFaults (0.00s)
2026-11-27T19:49:11.5634648Z     --- PASS: TestSummaryFaults/a_good_summary (0.00s)
2026-11-27T19:49:11.5635025Z     --- PASS: TestSummaryFaults/a_warning_appended_after_the_results_block (0.00s)
2026-11-27T19:49:11.5635577Z     --- PASS: TestSummaryFaults/the_summary_twice (0.00s)
2026-11-27T19:49:11.5635967Z     --- PASS: TestSummaryFaults/no_results_line (0.00s)
2026-11-27T19:49:11.5636450Z     --- PASS: TestSummaryFaults/a_first_line_that_is_not_the_heading (0.00s)
2026-11-27T19:49:11.5636875Z     --- PASS: TestSummaryFaults/another_word (0.00s)
2026-11-27T19:49:11.5637307Z     --- PASS: TestSummaryFaults/a_results_line_that_names_another_folder (0.00s)
2026-11-27T19:49:11.5638029Z --- PASS: TestEnvValue (0.00s)
2026-11-27T19:49:11.5643365Z --- PASS: TestWorkflowCommandFaults (0.00s)
2026-11-27T19:49:11.5643692Z     --- PASS: TestWorkflowCommandFaults/off_GitHub (0.00s)
2026-11-27T19:49:11.5644074Z     --- PASS: TestWorkflowCommandFaults/off_GitHub,_a_stop_line (0.00s)
2026-11-27T19:49:11.5644506Z     --- PASS: TestWorkflowCommandFaults/off_GitHub,_an_indented_line_on_stderr (0.00s)
2026-11-27T19:49:11.5644906Z     --- PASS: TestWorkflowCommandFaults/on_GitHub (0.00s)
2026-11-27T19:49:11.5645272Z     --- PASS: TestWorkflowCommandFaults/on_GitHub,_after_a_signal (0.00s)
2026-11-27T19:49:11.5645637Z     --- PASS: TestWorkflowCommandFaults/no_stop_line (0.00s)
2026-11-27T19:49:11.5645999Z     --- PASS: TestWorkflowCommandFaults/a_token_of_another_form (0.00s)
2026-11-27T19:49:11.5646373Z     --- PASS: TestWorkflowCommandFaults/nothing_on_stdout (0.00s)
2026-11-27T19:49:11.5646767Z     --- PASS: TestWorkflowCommandFaults/stderr_begins_with_another_line (0.00s)
2026-11-27T19:49:11.5647164Z     --- PASS: TestWorkflowCommandFaults/the_stop_line_twice (0.00s)
2026-11-27T19:49:11.5647514Z     --- PASS: TestWorkflowCommandFaults/no_resume_line (0.00s)
2026-11-27T19:49:11.5648288Z     --- PASS: TestWorkflowCommandFaults/the_resume_line_twice (0.00s)
2026-11-27T19:49:11.5648687Z     --- PASS: TestWorkflowCommandFaults/a_resume_line_after_a_signal (0.00s)
2026-11-27T19:49:11.5649182Z     --- PASS: TestWorkflowCommandFaults/a_resume_line_on_stderr (0.00s)
2026-11-27T19:49:11.5649771Z     --- PASS: TestWorkflowCommandFaults/off_GitHub,_a_line_after_a_lone_carriage_return (0.00s)
2026-11-27T19:49:11.5650380Z --- PASS: TestFailedCommands (0.00s)
2026-11-27T19:49:11.5652603Z --- PASS: TestReadGitHubOutputs (0.00s)
2026-11-27T19:49:11.5652903Z     --- PASS: TestReadGitHubOutputs/a_value_of_two_lines (0.00s)
2026-11-27T19:49:11.5653247Z     --- PASS: TestReadGitHubOutputs/one_name_written_twice (0.00s)
2026-11-27T19:49:11.5653573Z     --- PASS: TestReadGitHubOutputs/an_empty_file (0.00s)
2026-11-27T19:49:11.5653965Z     --- PASS: TestReadGitHubOutputs/the_plain_form_after_the_delimiter_form (0.00s)
2026-11-27T19:49:11.5654368Z     --- PASS: TestReadGitHubOutputs/a_line_in_the_plain_form (0.00s)
2026-11-27T19:49:11.5654747Z     --- PASS: TestReadGitHubOutputs/a_delimiter_that_never_comes (0.00s)
2026-11-27T19:49:11.5663145Z ok  	github.com/nzneit/rollproof/e2e	3617.830s
```

Every test and subtest of `helm-4`:

```
2026-11-27T18:50:47.6313025Z --- PASS: TestSetupCommandFails (64.09s)
2026-11-27T18:50:47.6313402Z     --- PASS: TestSetupCommandFails/helm-4 (64.09s)
2026-11-27T18:52:48.8540364Z --- PASS: TestCommandsDriverWithAllThreeCommands (121.22s)
2026-11-27T18:52:48.8540949Z     --- PASS: TestCommandsDriverWithAllThreeCommands/helm-4 (121.22s)
2026-11-27T18:54:36.4870647Z --- PASS: TestCommandsDriverWithInstallAndDeployOnly (107.63s)
2026-11-27T18:54:36.4871203Z     --- PASS: TestCommandsDriverWithInstallAndDeployOnly/helm-4 (107.63s)
2026-11-27T18:56:47.6039420Z --- PASS: TestSetupMakesTheProbeNamespaceRestricted (131.12s)
2026-11-27T18:56:47.6039999Z     --- PASS: TestSetupMakesTheProbeNamespaceRestricted/helm-4 (131.12s)
2026-11-27T18:56:47.6138064Z --- PASS: TestConfigOfEachShapeLoads (0.01s)
2026-11-27T18:56:47.6138452Z     --- PASS: TestConfigOfEachShapeLoads/both_releases (0.00s)
2026-11-27T18:56:47.6153172Z     --- PASS: TestConfigOfEachShapeLoads/no_previous (0.00s)
2026-11-27T18:56:47.6153479Z     --- PASS: TestConfigOfEachShapeLoads/candidate_runs_previous (0.00s)
2026-11-27T18:56:47.6153989Z     --- PASS: TestConfigOfEachShapeLoads/node_image (0.00s)
2026-11-27T18:56:47.6154294Z     --- PASS: TestConfigOfEachShapeLoads/setup (0.00s)
2026-11-27T18:56:47.6154627Z     --- PASS: TestConfigOfEachShapeLoads/three_commands (0.00s)
2026-11-27T18:56:47.6155035Z     --- PASS: TestConfigOfEachShapeLoads/two_commands (0.00s)
2026-11-27T18:56:47.6155434Z     --- PASS: TestConfigOfEachShapeLoads/fixture_probe (0.00s)
2026-11-27T18:56:47.6155723Z     --- PASS: TestConfigOfEachShapeLoads/TCP_loop (0.00s)
2026-11-27T18:56:47.6156009Z     --- PASS: TestConfigOfEachShapeLoads/limits (0.00s)
2026-11-27T18:56:47.6156262Z     --- PASS: TestConfigOfEachShapeLoads/through_the_mirror (0.00s)
2026-11-27T18:56:47.6156565Z     --- PASS: TestConfigOfEachShapeLoads/the_mirror_replaces_its_host (0.00s)
2026-11-27T18:56:47.6157099Z --- PASS: TestReadMarks (0.00s)
2026-11-27T18:56:47.6157938Z --- PASS: TestRenderConfigRejectsAnUnknownName (0.00s)
2026-11-27T18:56:47.6158209Z     --- PASS: TestRenderConfigRejectsAnUnknownName/custom_image (0.00s)
2026-11-27T18:56:47.6158499Z     --- PASS: TestRenderConfigRejectsAnUnknownName/command_role (0.00s)
2026-11-27T18:56:47.6158943Z --- PASS: TestManifestValuesLetTheFixtureSetImage (0.00s)
2026-11-27T18:56:47.9315151Z --- PASS: TestActionLoop (0.32s)
2026-11-27T18:56:47.9315687Z     --- PASS: TestActionLoop/the_action_reports_done (0.00s)
2026-11-27T18:56:47.9316218Z     --- PASS: TestActionLoop/the_action_fails (0.00s)
2026-11-27T18:56:47.9316491Z     --- PASS: TestActionLoop/the_run_ends_first (0.00s)
2026-11-27T18:56:47.9316814Z     --- PASS: TestActionLoop/the_run_ends_between_two_calls (0.31s)
2026-11-27T18:56:47.9317268Z --- PASS: TestInterruptWaitsForTheDeploysStart (0.00s)
2026-11-27T18:56:47.9317652Z --- PASS: TestRestartComesAfterTheDelay (0.00s)
2026-11-27T18:58:33.6572720Z --- PASS: TestCustomProbeGoesSilentOnTheCandidate (105.73s)
2026-11-27T18:58:33.6573265Z     --- PASS: TestCustomProbeGoesSilentOnTheCandidate/helm-4 (105.73s)
2026-11-27T19:00:17.1192001Z --- PASS: TestCustomProbeExitsOnTheCandidate (103.46s)
2026-11-27T19:00:17.1192750Z     --- PASS: TestCustomProbeExitsOnTheCandidate/helm-4 (103.46s)
2026-11-27T19:02:24.7586867Z --- PASS: TestTCPLoopJudgesTheFirstFixture (127.64s)
2026-11-27T19:02:24.7587638Z     --- PASS: TestTCPLoopJudgesTheFirstFixture/helm-4 (127.64s)
2026-11-27T19:04:26.9636022Z --- PASS: TestProbeFailsItsFirstFiveSeconds (122.21s)
2026-11-27T19:04:26.9636424Z     --- PASS: TestProbeFailsItsFirstFiveSeconds/helm-4 (122.21s)
2026-11-27T19:05:40.4172187Z --- PASS: TestProbeFailsItsFirstTwentySeconds (73.45s)
2026-11-27T19:05:40.4172621Z     --- PASS: TestProbeFailsItsFirstTwentySeconds/helm-4 (73.45s)
2026-11-27T19:07:17.3152133Z --- PASS: TestTCPLoopJudgesACandidateWithoutReadiness (96.90s)
2026-11-27T19:07:17.3152835Z     --- PASS: TestTCPLoopJudgesACandidateWithoutReadiness/helm-4 (96.90s)
2026-11-27T19:09:30.9876264Z --- PASS: TestCustomProbeIgnoresSIGTERM (133.67s)
2026-11-27T19:09:30.9876743Z     --- PASS: TestCustomProbeIgnoresSIGTERM/helm-4 (133.67s)
2026-11-27T19:11:42.8253353Z --- PASS: TestTCPLoopInARestrictedProbeNamespace (131.84s)
2026-11-27T19:11:42.8253949Z     --- PASS: TestTCPLoopInARestrictedProbeNamespace/helm-4 (131.84s)
2026-11-27T19:13:51.2768925Z --- PASS: TestFirstFixture (128.45s)
2026-11-27T19:13:51.2769215Z     --- PASS: TestFirstFixture/helm-4 (128.45s)
2026-11-27T19:15:31.1767934Z --- PASS: TestCandidateWithoutReadiness (99.90s)
2026-11-27T19:15:31.1768421Z     --- PASS: TestCandidateWithoutReadiness/helm-4 (99.90s)
2026-11-27T19:17:27.5504596Z --- PASS: TestCandidateExitsOnSIGTERM (116.37s)
2026-11-27T19:17:27.5505027Z     --- PASS: TestCandidateExitsOnSIGTERM/helm-4 (116.37s)
2026-11-27T19:20:12.1459586Z --- PASS: TestNoRoomForTheExtraPod (164.60s)
2026-11-27T19:20:12.1460007Z     --- PASS: TestNoRoomForTheExtraPod/helm-4 (164.60s)
2026-11-27T19:21:26.6039930Z --- PASS: TestPreviousFailsEveryRequest (74.46s)
2026-11-27T19:21:26.6040405Z     --- PASS: TestPreviousFailsEveryRequest/helm-4 (74.46s)
2026-11-27T19:23:42.7484197Z --- PASS: TestFirstFixtureOnTheOldestNodeImage (136.14s)
2026-11-27T19:23:42.7484975Z     --- PASS: TestFirstFixtureOnTheOldestNodeImage/helm-4 (136.14s)
2026-11-27T19:25:07.8039951Z --- PASS: TestPreviousExitsOnSIGTERM (85.06s)
2026-11-27T19:25:07.8040478Z     --- PASS: TestPreviousExitsOnSIGTERM/helm-4 (85.06s)
2026-11-27T19:26:36.2861098Z --- PASS: TestDeployReplacesNoPod (88.48s)
2026-11-27T19:26:36.2861548Z     --- PASS: TestDeployReplacesNoPod/helm-4 (88.48s)
2026-11-27T19:28:53.8294545Z --- PASS: TestPreviousNeverReady (137.54s)
2026-11-27T19:28:53.8294893Z     --- PASS: TestPreviousNeverReady/helm-4 (137.54s)
2026-11-27T19:30:34.7483553Z --- PASS: TestNoPreviousRelease (100.92s)
2026-11-27T19:30:34.7484186Z     --- PASS: TestNoPreviousRelease/helm-4 (100.92s)
2026-11-27T19:32:43.7880614Z --- PASS: TestBothReleasesEndWithoutDraining (129.04s)
2026-11-27T19:32:43.7881125Z     --- PASS: TestBothReleasesEndWithoutDraining/helm-4 (129.04s)
2026-11-27T19:35:09.7965175Z --- PASS: TestSecondRolloutAfterTheFirstFinished (146.01s)
2026-11-27T19:35:09.7965694Z     --- PASS: TestSecondRolloutAfterTheFirstFinished/helm-4 (146.01s)
2026-11-27T19:38:37.5871824Z --- PASS: TestChartDeniesTrafficByDefault (207.79s)
2026-11-27T19:38:37.5872524Z     --- PASS: TestChartDeniesTrafficByDefault/control (79.63s)
2026-11-27T19:38:37.5873049Z     --- PASS: TestChartDeniesTrafficByDefault/helm-4 (128.16s)
2026-11-27T19:40:45.3059523Z --- PASS: TestStatefulSet (127.72s)
2026-11-27T19:40:45.3059974Z     --- PASS: TestStatefulSet/helm-4 (127.72s)
2026-11-27T19:42:41.1414727Z --- PASS: TestDaemonSet (115.84s)
2026-11-27T19:42:41.1415153Z     --- PASS: TestDaemonSet/helm-4 (115.84s)
2026-11-27T19:45:38.1847292Z --- PASS: TestHooksLongerThanTheSettleTime (177.04s)
2026-11-27T19:45:38.1847632Z     --- PASS: TestHooksLongerThanTheSettleTime/helm-4 (177.04s)
2026-11-27T19:47:41.5633556Z --- PASS: TestCandidateFailsAfterTheRolloutFinished (123.38s)
2026-11-27T19:47:41.5634278Z     --- PASS: TestCandidateFailsAfterTheRolloutFinished/helm-4 (123.38s)
2026-11-27T19:49:09.1061609Z --- PASS: TestCandidateValueBreaksTheSchema (87.54s)
2026-11-27T19:49:09.1062067Z     --- PASS: TestCandidateValueBreaksTheSchema/helm-4 (87.54s)
2026-11-27T19:50:20.3138361Z --- PASS: TestSIGINTDuringTheDeploy (71.21s)
2026-11-27T19:50:20.3138832Z     --- PASS: TestSIGINTDuringTheDeploy/helm-4 (71.21s)
2026-11-27T19:52:13.5195501Z --- PASS: TestCandidateChangesOnlyAnUnprobedWorkload (113.21s)
2026-11-27T19:52:13.5195902Z     --- PASS: TestCandidateChangesOnlyAnUnprobedWorkload/helm-4 (113.21s)
2026-11-27T19:52:13.5198215Z --- PASS: TestLoopbackProblems (0.00s)
2026-11-27T19:52:13.5198426Z     --- PASS: TestLoopbackProblems/loopback_only (0.00s)
2026-11-27T19:52:13.5198651Z     --- PASS: TestLoopbackProblems/no_CONN_line (0.00s)
2026-11-27T19:52:13.5198886Z     --- PASS: TestLoopbackProblems/loopback_and_a_node (0.00s)
2026-11-27T19:52:13.5199116Z     --- PASS: TestLoopbackProblems/a_node_only (0.00s)
2026-11-27T19:52:13.5199363Z     --- PASS: TestLoopbackProblems/an_address_with_no_port (0.00s)
2026-11-27T19:52:13.5204527Z --- PASS: TestCutScript (0.00s)
2026-11-27T19:52:13.5213138Z --- PASS: TestNewAuthority (0.00s)
2026-11-27T19:52:13.5214354Z --- PASS: TestPasswordForms (0.00s)
2026-11-27T19:52:13.5219502Z --- PASS: TestFindForms (0.00s)
2026-11-27T19:52:13.5219878Z     --- PASS: TestFindForms/names_the_index_and_skips_an_empty_form (0.00s)
2026-11-27T19:52:13.5220513Z     --- PASS: TestFindForms/sorts_every_file_and_form (0.00s)
2026-11-27T19:52:13.5224431Z --- PASS: TestParseStandinLog (0.00s)
2026-11-27T19:52:13.5224800Z --- PASS: TestHelmMajors (0.00s)
2026-11-27T19:52:13.5225225Z --- PASS: TestRunEnvKeepsTheJobsGitHubFilesOut (0.00s)
2026-11-27T19:52:13.5227002Z --- PASS: TestSummaryFaults (0.00s)
2026-11-27T19:52:13.5227197Z     --- PASS: TestSummaryFaults/a_good_summary (0.00s)
2026-11-27T19:52:13.5227636Z     --- PASS: TestSummaryFaults/a_warning_appended_after_the_results_block (0.00s)
2026-11-27T19:52:13.5228068Z     --- PASS: TestSummaryFaults/the_summary_twice (0.00s)
2026-11-27T19:52:13.5228427Z     --- PASS: TestSummaryFaults/no_results_line (0.00s)
2026-11-27T19:52:13.5228752Z     --- PASS: TestSummaryFaults/a_first_line_that_is_not_the_heading (0.00s)
2026-11-27T19:52:13.5229002Z     --- PASS: TestSummaryFaults/another_word (0.00s)
2026-11-27T19:52:13.5229263Z     --- PASS: TestSummaryFaults/a_results_line_that_names_another_folder (0.00s)
2026-11-27T19:52:13.5229606Z --- PASS: TestEnvValue (0.00s)
2026-11-27T19:52:13.5234759Z --- PASS: TestWorkflowCommandFaults (0.00s)
2026-11-27T19:52:13.5234978Z     --- PASS: TestWorkflowCommandFaults/off_GitHub (0.00s)
2026-11-27T19:52:13.5235320Z     --- PASS: TestWorkflowCommandFaults/off_GitHub,_a_stop_line (0.00s)
2026-11-27T19:52:13.5235806Z     --- PASS: TestWorkflowCommandFaults/off_GitHub,_an_indented_line_on_stderr (0.00s)
2026-11-27T19:52:13.5236237Z     --- PASS: TestWorkflowCommandFaults/on_GitHub (0.00s)
2026-11-27T19:52:13.5236497Z     --- PASS: TestWorkflowCommandFaults/on_GitHub,_after_a_signal (0.00s)
2026-11-27T19:52:13.5236786Z     --- PASS: TestWorkflowCommandFaults/no_stop_line (0.00s)
2026-11-27T19:52:13.5237239Z     --- PASS: TestWorkflowCommandFaults/a_token_of_another_form (0.00s)
2026-11-27T19:52:13.5237781Z     --- PASS: TestWorkflowCommandFaults/nothing_on_stdout (0.00s)
2026-11-27T19:52:13.5238204Z     --- PASS: TestWorkflowCommandFaults/stderr_begins_with_another_line (0.00s)
2026-11-27T19:52:13.5238630Z     --- PASS: TestWorkflowCommandFaults/the_stop_line_twice (0.00s)
2026-11-27T19:52:13.5239042Z     --- PASS: TestWorkflowCommandFaults/no_resume_line (0.00s)
2026-11-27T19:52:13.5239442Z     --- PASS: TestWorkflowCommandFaults/the_resume_line_twice (0.00s)
2026-11-27T19:52:13.5239730Z     --- PASS: TestWorkflowCommandFaults/a_resume_line_after_a_signal (0.00s)
2026-11-27T19:52:13.5240017Z     --- PASS: TestWorkflowCommandFaults/a_resume_line_on_stderr (0.00s)
2026-11-27T19:52:13.5240346Z     --- PASS: TestWorkflowCommandFaults/off_GitHub,_a_line_after_a_lone_carriage_return (0.00s)
2026-11-27T19:52:13.5240748Z --- PASS: TestFailedCommands (0.00s)
2026-11-27T19:52:13.5242342Z --- PASS: TestReadGitHubOutputs (0.00s)
2026-11-27T19:52:13.5242565Z     --- PASS: TestReadGitHubOutputs/a_value_of_two_lines (0.00s)
2026-11-27T19:52:13.5242816Z     --- PASS: TestReadGitHubOutputs/one_name_written_twice (0.00s)
2026-11-27T19:52:13.5243053Z     --- PASS: TestReadGitHubOutputs/an_empty_file (0.00s)
2026-11-27T19:52:13.5243330Z     --- PASS: TestReadGitHubOutputs/the_plain_form_after_the_delimiter_form (0.00s)
2026-11-27T19:52:13.5243618Z     --- PASS: TestReadGitHubOutputs/a_line_in_the_plain_form (0.00s)
2026-11-27T19:52:13.5244061Z     --- PASS: TestReadGitHubOutputs/a_delimiter_that_never_comes (0.00s)
2026-11-27T19:52:13.5258152Z ok  	github.com/nzneit/rollproof/e2e	3801.828s
```

### The tool's lines in the logs of `go test`

In every step that runs `go test` and the tool, the harness logs each run's output, and so the runner reads the tool's lines there too. The first run of the tool in such a step shows its token masked, in its stop line, its resume line and the stop line of standard error that follows; every later run shows its token in plain text. The mirror job:

```
2026-11-27T18:31:21.9679397Z         ::stop-commands::***
…
2026-11-27T18:31:21.9690729Z         ::***::
2026-11-27T18:31:21.9691079Z         ::stop-commands::***
2026-11-27T18:35:34.4459376Z         ::stop-commands::V527J73DS5JUUB4UWJJMOCZBVQ
…
2026-11-27T18:35:34.4471034Z         ::V527J73DS5JUUB4UWJJMOCZBVQ::
2026-11-27T18:35:34.4471443Z         ::stop-commands::V527J73DS5JUUB4UWJJMOCZBVQ
```

The same holds in `helm-3` (`::stop-commands::***` at 18:50:03.1927188, in `TestSetupCommandFails`, the job's first run of the tool, and `::stop-commands::3NPO6Q7AI7HW7B7QOHR7GCEAAT` at 18:52:05.4927593, in `TestCommandsDriverWithAllThreeCommands`, the next), in `helm-4` (`::stop-commands::***` at 18:50:47.6210348, in `TestSetupCommandFails`, the job's first run of the tool, and `::stop-commands::LRZ2LKSSWJ6NOW5DJ5PFXFNYUL` at 18:52:48.8419334, in `TestCommandsDriverWithAllThreeCommands`, the next), in the `cluster` job, where the script's stop line is masked and the tool's stands in plain text after it (2026-11-27T18:48:19.8489247 and 2026-11-27T18:48:19.8495165).

What this shows: the runner masked the first token and not the later ones. The runner's source adds the token of a stop line that it reads as a command to the values it masks (`ActionCommandManager.cs`, lines 116 to 121, as `docs/proofs/p32-step6-workflows.md` reads it); so the first line was read as a command, and the later ones, which came while the runner's commands were stopped, were not. The first run's resume line, masked, and the stop line right after it fit that record's reading: a logged resume line is followed at once by the stop line of standard error, which stops the commands again. What it does not show: that a workflow command printed after a stop line is ignored, since no run plants one; where the runner applies its masker to a line; and the order in which the runner reads the lines of a step's two streams. The steps print no workflow command of their own, so this changes nothing in them.

## Decision

- **All three workflows passed in the new repository, at its root**, on the commit `97219ae`. So the README's sentence on what has run names this record, and the list of the proofs has its row.

- **The action's test passed**: the runner read `uses: ./` as the action at the root (the group `Run ./`, 18:28:41.7918169); the tool ran in the workspace with each config relative to it; the run cut short by the step's limit wrote its final files as `not-verified` with the reason `interrupted`, and its step's outcome reached the next step; and the masked copy of the job summary was where the test looks.

- **The mirror job passed**: both subtests, and with them every setup command, the `getent hosts` of the real host's name in a node and the pull that must fail after the cut among them (`e2e/mirror_helpers_test.go`, lines 99 to 106, read).

- **The suite passed** under Helm 3 on `ubuntu-24.04` and under Helm 4 on `ubuntu-26.04`, with the fourteenth row's control giving `steady-not-clean` in both jobs. Docker's version on each image is read from the image's list, not shown by a log (see "Machine and versions").

- **The time.** By the jobs' log times the action's test took 7.3 minutes, the mirror job 8.1, the suite's first job 12.2, and the Helm jobs 60.8 and 63.9. The fourth pair says "about 25 minutes for the first two together and about 3 hours for the suite": the first two together took 15.4 minutes, and the suite 136.8 minutes by the sum of its three jobs' times, 4,567.0 s, 1 h 16 min 7.0 s from the run's first to its last log line. Step 2 of the plan gives 11, 12, 16 and 77 to 93 minutes for them as "the times of 2026-11-22". Those are not the times that day's record gives: 9.1 minutes for `action`, 10.2 minutes for `mirror`, 17.6 minutes for `cluster`, 76.0 minutes for `helm-3`, 70.4 minutes for `helm-4` (`docs/proofs/p32-step6-workflows.md`, its table of jobs, read); the 11 and 12 are the figures of the ask that was written before those runs (`docs/plans/2026-11-22-step6/09-hosted-runs.md`, line 35, read). Against that record the jobs of 2026-11-27 took, as a share of the time then, 0.80 for `action`, 0.80 for `mirror`, 0.69 for `cluster`, 0.80 for `helm-3`, 0.91 for `helm-4`: less than nine tenths in `action`, `mirror`, `cluster`, `helm-3`, and within a tenth in `helm-4`. The fourth pair is filled by the plan's rule, against Step 2's figures: `action` outside a tenth of 11; `mirror` outside a tenth of 12; `cluster` outside a tenth of 16; `helm-3` outside a tenth of 77 to 93; `helm-4` outside a tenth of 77 to 93. So it reads `no`. This is a finding for the maintainer: the pair's figures are Step 2's estimates, not the times of that day's runs, and each run of 2026-11-27 took less than both (4 of the 5 jobs more than a tenth below that day's record). One run of each was made, so what a second run takes is not known.

- **The second pair's `Holds:` line read `yes`, the plan's own line, until it was amended on 2026-11-27, by the maintainer's ruling, to `not measured here`.** The pair's text is the plan's own too; both lines were written before the runs, and the maintainer kept them on 2026-11-27, knowing that no runner's log shows the pair's clause on Docker's DNS. The amendment rests on the saved log of the mirror job, quoted in Result: `--- PASS: TestMirror (398.10s)` and both subtests, and no line of that log on Docker's DNS. The pair names Docker's DNS, as the plan wrote it. What the runner's log shows is narrower: the names resolved in a node after the cut, and the nodes pulled through the mirror. The cause is the reading recorded on 2026-11-22 (`docs/proofs/p32-step6-workflows.md`, its Decision), read there and tried off the runner.

- **Left open:** a second run of anything here; the false-failure bound on these runners, which the series measures (a later design); the action under Podman; whether Docker runs as root; the machines' size, which no log shows; a workflow command printed after the action's stop line, which no run plants; the order in which the runner reads the lines of a step's two streams; the Helm that the action's job found on the runner's PATH; and what the record of 2026-11-22 left open (its Decision).

What decides each pair, in the order of the pairs below:

1. The action's test: the group `Run ./` and the config `hack/action/testdata/pass.yaml` from the workspace (18:28:41.7918929); the tool's line through docker (18:28:44.2996388); the pass's console names the chart at `/home/runner/work/rollproof/rollproof/fixtures/chart`; `actioncheck` found `result.json` final with the verdict `not-verified` and the deciding reason `interrupted` for the run cut short, the step's outputs `not-verified` and the results folder, and one masked summary; the step after it ran with `OUTCOME: failure`, and the job ended `success`.

2. The mirror job: `--- PASS: TestMirror (398.10s)` (18:35:34.7022530) and both subtests. The real host's name is resolved by `getent hosts` in a node, the eleventh setup command, and the pull that must fail after the cut failed; that the answers came from Docker's DNS is not shown on the runner (above), so the pair reads `not measured here`.

3. The suite: `ok  	github.com/nzneit/rollproof/e2e	3617.830s` and `ok  	github.com/nzneit/rollproof/e2e	3801.828s`, and the five packages of the `cluster` job (`ok` lines above), each job with no `--- FAIL` line, and the control's `--- PASS` lines above.

4. The time: 7.3 minutes for `action`, 8.1 minutes for `mirror`, 12.2 minutes for `cluster`, 60.8 minutes for `helm-3`, 63.9 minutes for `helm-4` against Step 2's 11, 12, 16, 77 to 93, 77 to 93; not each within a tenth.



Plan assumed: "The action's test passes in the new repository: the action finds its configuration file at hack/action/testdata/ from the workspace, and its step's outputs and job summary are read as in the incubating repository."
Holds: yes

Plan assumed: "The mirror job passes there: the nodes resolve the mirror's container name through Docker's DNS, and the cut of their route holds."
Holds: not measured here

Plan assumed: "The suite passes there: the cluster tests of five packages, and every row under Helm 3 on ubuntu-24.04 and under Helm 4 on ubuntu-26.04, the fourteenth row's control giving steady-not-clean in both jobs."
Holds: yes

Plan assumed: "The three workflows take on a public repository's runners about what they took on a private repository's: about 25 minutes for the first two together and about 3 hours for the suite."
Holds: no
