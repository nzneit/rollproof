# Tasks 11 and 12: the README and `e2e/doc.go`; step 5's leftovers

Read section 8 ("The action", whole), section 11 (its first bullet), section 12 ("The suite's workflow"), section 13 ("During incubation"), section 14, point 32 (the bullet "What waits for a repository of its own"), and decisions 2 to 4, 10 to 17, 20, 21 and 24 of the README, before Task 11; decision 25 before Task 12.

### Task 11: the README and `e2e/doc.go`

**Files:**
- Modify: `README.md`, `e2e/doc.go`

**Interfaces:**
- Consumes: every public decision of the README; the workflows of Task 10; the variable of Task 7.
- Produces: nothing that code calls.

- [ ] **Step 1: Write `README.md`**

The whole file becomes, exactly (only the paragraph on `registries` is today's, unchanged):

````markdown
# rollproof

`rollproof` is a working name.

It tests, in a CI job or on a laptop, whether deploying a new version of a Kubernetes workload, and rolling it back, causes downtime.

Nothing is released; the design is docs/specs/2026-11-13-rollproof-design.md.

What works now, on Linux: `rollproof run` creates its own kind cluster; deploys through Helm, with or without a previous release (a run with none tests a restart), or through the commands driver; runs the `setup` commands; judges with the built-in HTTP probe or with a custom probe that `docs/probe-contract.md` describes; takes images from the engine or from an archive; and writes `result.json`, the console report and the evidence folder, and, on GitHub Actions, the step's outputs and its job summary. The whole suite has run on rootless nerdctl. On Docker, on GitHub's hosted runners, the tests under the build tag `cluster` of four packages and the suite's first fixture under both Helm majors have run (`docs/proofs/p24-hosted-runner.md`). Podman is not promised: one test has run through it.

`registries` tells the cluster's nodes how to pull from a host. A `mirror` replaces the host: the nodes pull its images from the mirror alone, and a mirror that lacks an image fails the pull. A `ca` adds a certificate authority for the place the nodes pull from. `username` and `password` go into a credentials file that every node mounts; any pod that mounts that path of its node can therefore read it, so give credentials that can only read and expire soon. Write Docker Hub as `docker.io`. `as` in `images` stores an image under the repository that the real manifests name, so the manifest under test is the one that deploys. A registry password is masked as `***` in the console, in `result.json` and in the evidence folder, in its plain form, its JSON spellings (with and without HTML escaping), its Go-quoted spelling, and the base64 of `USERNAME:PASSWORD` (padded and not); nothing else is masked. A mirror that the nodes must reach without a proxy goes into the caller's own `NO_PROXY`, which kind passes into the nodes (read in kind's source, not run).

## On GitHub Actions

`action.yml` is a composite action with three inputs: `config`, the configuration file (default `rollproof.yaml`); `image`, the tool's image; and `out`, the results folder (default: a new folder under the runner's temporary directory). It checks that the runner is Linux, finds the container engine by the tool's own rule, pulls the tool's image by its digest, takes the binary out of it, and starts it with `exec`, in the workspace, so that a relative `config` or `out` is read from there. `env:` on the action's step, or on the job or the workflow, reaches the tool; the action has no input for variables.

It cannot yet be used from another repository: nothing is released, so the action carries no digest and `image` must name a reference with `@sha256:`. Its own test builds the tool's image from `Dockerfile` and uses the action from this folder. Once it is released, a caller's job reads:

    - run: docker build -t orders:candidate .
    - id: rollproof
      uses: OWNER/rollproof@COMMIT
      env:
        PREVIOUS_VERSION: 1.4.2
    - if: always() && steps.rollproof.outputs.out != ''
      uses: actions/upload-artifact@COMMIT
      with:
        name: rollproof-results
        path: ${{ steps.rollproof.outputs.out }}

Two outputs. `verdict` is `pass`, `fail` or `not-verified`, the verdict that decides the step's exit code (0, 1 or 2); a run that was killed before it finished leaves `not-verified`. An empty `verdict` means that the action stopped before the tool started (the runner is not Linux, it has no container engine, or the image could not be pulled) or that the tool could not open its results folder, or that the step's outputs file could not be written, which a warning on the console says: read it as not verified. `out` is the folder that holds `result.json` and the evidence.

The action uploads nothing. The step above uploads the folder, with `if: always()` so that it runs after a fail, after a run that was not verified and after a cancel. The evidence holds the chart's values and the probe's header values as they were given; only a registry password is masked. Keep the uploaded folder to yourself when those values are secret.

The step that calls the action must not carry `if: always()`: GitHub's runner does not cancel a running step whose condition still holds after a cancel, so the tool would not learn of it (read in the runner's source, not run). A cancel, the job's time limit and a `timeout-minutes` on the step reach the tool as SIGINT; it then writes its final `result.json`, its outputs and its job summary and prints its report before anything else, because GitHub's SIGTERM ends it 7.5 s later, and leaves the cluster for the next run to remove.

The tool writes the outputs and the job summary itself, when `GITHUB_ACTIONS` is `true`, each to the file that `GITHUB_OUTPUT` or `GITHUB_STEP_SUMMARY` names. So a runner that already holds the binary needs no action: a step whose script is `exec rollproof run` gives the same outputs and the same summary, and the same caution about `if: always()` holds. Without `exec`, the step's shell gets GitHub's signals and the tool does not. The job summary holds the console's report as Markdown, with every text from the cluster, the commands and the configuration inside code.

The configuration's commands and the chart run with the job's power, its secrets included (design, section 11): do not run the tool on a pull request's code with the base repository's secrets.

## The suite

Every fixture of the design's section 12 gives its expected verdict under Helm 3.22.1 and Helm 4.3.2, and the first fixture also on `kindest/node:v1.34.11`, in one run of the suite on 2026-11-19 and 2026-11-20.

To run the suite: `go run ./hack/fetchhelm` fetches under `.tools/` the Helm binaries and, for linux/amd64 only, kubectl 1.37.0, which the suite needs; the suite's first run pulls busybox 1.37.0 by its digest into `.tools/`; `go test -tags e2e -p 1 ./e2e/ -timeout 360m` runs every row under both Helm majors, in about two and a half hours on a workstation. The variable `ROLLPROOF_E2E_HELM` set to `3` or `4` runs one major, as in `env ROLLPROOF_E2E_HELM=4 go test -tags e2e -p 1 ./e2e/ -timeout 180m`; not set, it runs both. The fourteenth row's control runs under Helm 3 either way. On nerdctl, put `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0` before the command.

The mirror job, `go test -tags mirror -p 1 ./e2e/ -run TestMirror -v -timeout 60m` (with `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0` before it on nerdctl), runs the first fixture through a mirror that needs a login, sits under a path and serves TLS from a private authority, with the nodes' route to public registries cut, and then a run whose candidate the mirror lacks.

The project's workflows sit in the host repository, and all but the unit tests start by hand only: `rollproof-e2e.yml` runs the tests under the build tag `cluster`, and then the suite under Helm 3 on `ubuntu-24.04` and under Helm 4 on `ubuntu-26.04`, side by side, in about one and a half to two hours (about 172 to 205 billed minutes); `rollproof-mirror.yml` runs the mirror job; `rollproof-action.yml` runs the action's own test: a pass, a fail, and a run cut short by a time limit. `rollproof-unit.yml` runs the unit tests on every pull request that touches the project.

Every key of version 1 is built: `registries` and `as` in `images` came with step 5.

Run the unit tests with `go test ./...`.
````

- [ ] **Step 2: Write `e2e/doc.go`**

The package comment becomes, exactly (its paragraph on the mirror job is today's, unchanged):

```go
// Package e2e runs the real rollproof binary against the fixture service,
// one fixture of the design's section 12 at a time, under Helm 3 and Helm 4.
//
// The fixture rows carry the build tag e2e. The tests of config_test.go,
// mirror_helpers_test.go and runenv_test.go carry none: they check the run
// description, the window marks, the actions, the harness's environment and
// the mirror job's helpers with no cluster, so `go test ./...` runs them, and
// `go test -tags e2e ./e2e/` runs them too.
//
// The suite runs every row of section 12 under the Helm majors that the
// variable ROLLPROOF_E2E_HELM selects: 3 or 4 for one of them, both when it is
// not set, as a developer's run leaves it. The fourteenth row's control runs
// under Helm 3 either way. Under both majors the suite takes about two and a
// half hours on a workstation. It needs Linux, a container engine that kind
// supports (docker, nerdctl or podman), the Helm binaries and kubectl under
// .tools/, which `go run ./hack/fetchhelm` puts there (kubectl for linux/amd64
// only), and busybox's archive at .tools/busybox-1.37.0.tar, which the first
// run of the suite pulls and saves.
// Each run creates the cluster rollproof-e2e and deletes it at its end, so
// the suite runs one test at a time:
//
//	go test -tags e2e -p 1 ./e2e/ -v -timeout 360m
//	env ROLLPROOF_E2E_HELM=4 go test -tags e2e -p 1 ./e2e/ -v -timeout 180m
//
// The harness keeps GITHUB_OUTPUT and GITHUB_STEP_SUMMARY out of the runs it
// starts, so that on GitHub a job's summary is not the sum of its runs'. The
// first fixture's test and the mirror job's runs get files of their own for
// both, github-output and github-summary in their results folder, and read
// them. The results folders stay under rollproof-e2e-results in the system's
// temporary folder.
//
// The mirror job carries the build tag mirror, shares harness_test.go with
// the suite, and runs, one test at a time, with its own command, not the
// suite's:
//
//	go test -tags mirror -p 1 ./e2e/ -run TestMirror -v -timeout 60m
//
// with `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0` before it on
// nerdctl, and `CGO_ENABLED=0` in the environment on Docker, as the workflow
// sets it.
package e2e
```

- [ ] **Step 3: Check them**

Read both against the README's decisions 2 to 4, 10 to 17, 20, 21 and 24: every public decision that a caller meets is in the README, and nothing in it says more than the decisions. Run `gofmt -l .`, `go test ./...` and the four vets.
Expected: no output; PASS.

- [ ] **Step 4: Commit**

```
git add README.md e2e/doc.go
git commit -m "Document the action, its outputs and summary, the suite's Helm variable and the workflows"
```

### Task 12: step 5's leftovers

**Files:**
- Modify: `internal/deploy/command.go` (`sink.finish`), `internal/deploy/command_test.go`
- Modify: `internal/watch/counts_test.go`
- Modify: `internal/report/mask_test.go`
- Modify: `internal/mask/mask.go` (`Writer`'s comment), `internal/mask/mask_test.go`

**Interfaces:**
- Consumes: nothing of this plan.
- Produces: nothing that another task uses.

The small faults that step 5's reviews deferred and its final fix wave did not carry, read against the code at `58c1cbf` (no commit after step 5's last comment-only pass has touched the code they name). "Task N mK" is step 5's name for each; its reviews are not filed in the repository.

| Step 5's item | What it is | Answer |
| --- | --- | --- |
| Task 1 m1 | the row for a mirror with a user and a password passes with `mirrorProblem`'s `@` check removed, since `hostProblem` gives the same phrase | Kept: `config` is not changed by this step, and both checks give the right text |
| Task 1 m2 | the `ca` rows: the folder row does not pin the guard for a file that is not regular (a FIFO), no `CERTIFICATE` block with bad DER, the path's absence from the text not asserted | Kept: test gaps in a unit this step does not change |
| Task 1 m3 | `as: https://…` gets "the port must be 1 to 65535", and `as: u:p@…` the digest's text | Kept: the value is rejected; only the text could be clearer |
| Task 1 m4 | a bracketed IPv6 host with an embedded IPv4 address (`[64:ff9b::192.0.2.1]`) is accepted, though an image's name cannot write it | Kept: such a host never matches an image's name, so nothing reaches a node; step 5's ruling to follow the spec's grammar waits for the next plan that touches `config` |
| Task 1 m5 | missing rows: a mirror's segment `.`, an IPv6 address without brackets in an `as` or a mirror, a user or a password alone in a mirror, `[fd00::5]/team/orders` among the accepted `as` | Kept: test gaps |
| Task 1 m6 | an unused parsed certificate in a test helper, and the form `switch rest := …; {` | Kept: style |
| Task 2 m3 | the bound after one write of 1,000 bytes, and `New(nil).Writer` | Carried: this task |
| Task 2 m6 | `Writer`'s comment does not say that a stream it returns is not safe for concurrent use | Carried: this task |
| Task 3 m1 | `SetMasker`'s comment is not step 5's Interfaces text word for word | Kept: the comment is true; step 5's comment pass read it |
| Task 3 m2 | `masked` would panic on a named string type, and the test's fill and walk skip map values, interfaces, arrays and other packages' structs | Kept: `Run` holds no such type, and this plan adds no field to it |
| Task 3 m3 | the test of many goroutines counts line ends, not each writer's line once | Kept: test strength |
| Task 3 m5 | a commit message's table of error paths left out two branches | Kept: a commit message cannot be changed |
| Task 3 m6 | two guards of `TestMaskSharesNoMemoryWithTheRecord` print a pointer, not the value | Carried: this task |
| Task 5 m2 | no test reaches three failure texts of `Create`: the credentials file's write, a `ca` that cannot be read, a node's write | Carried in part: Task 3 tests the last two through `writeNodeSettings` (`TestWriteNodeSettingsFailures`); the credentials file's write is kept, since no seam lies between the run folder's making and that write |
| Task 5 m3 | the cluster test does not read `certs.d/docker.io/ca.pem` | Carried: Task 3 |
| Task 5 m4 | no test that an entry with credentials alone, outside `AsHosts`, writes no `hosts.toml` | Answered already: `TestHostSettingsNeedsNoFile` holds that entry at the function that decides, and `writeRegistryFiles` writes nothing for "" |
| Task 6 m2 | no test sends a `local` entry with `as` through `Store` | Kept: `as` takes the same path for both sources; `images` is not changed |
| Task 6 m3 | a test's registry keeps its default logger, which is noisy under `-v` | Kept: noise in a test's output only |
| Task 7 m2 | no test that `watch.Start` copies `MirrorNote` into the tracker's options | Kept: `watch` is not changed by this step |
| Task 7 m3 | no test of the note after "and last ended", nor of a note function that gives "" | Kept: same |
| Task 7 m4 | the lookup of a container by name is not pinned | Kept: same |
| Task 8 m1 | the probe's lookup of its container by the name `probe` is not pinned | Kept: `probe` is not changed |
| Task 8 m2 | no test of a note function that gives "" for the probe's image | Kept: same |
| Task 11 m2 | the stand-in logs `user=` for an empty user, not `user=-` | Kept: the mirror job's stand-in, a fixture, is not changed |
| Task 11 m3 | the stand-in's `-check` follows redirects | Kept: same |
| Task 11 m4 | `REQ` logs the decoded path and the user unescaped | Kept: same |
| Task 11 m5 | the plain registry's `Serve` error is dropped | Kept: same |
| Task 11 m6 | tests miss a wrong user, a malformed header, a path equal to a prefix and a logged 404 | Kept: same |
| Task 11 m7 | tests do not pin the retry every 200 ms, a push by digest with no tag, or a `CONN` for a failed handshake | Kept: same |
| Task 12 m1 | the console and `TestPasswordForms`' messages are logged whole | Kept: step 5's ruling stands (the job's password is new for each run, and its stand-ins are removed at the job's end) |
| Task 12 m3 | the row with no images does not check the rendered `images: {}` | Kept: a test gap |
| Task 12 m4 | the mirror job's second shape does not check `cfg.Registries` again | Kept: a test gap |
| Task 13 m3 | `holdRunImages` drops the output of an inspect that failed | Kept: a failed inspect only means "not held", and the pull after it reports its own error |
| Task 13 m4 | a run that passes keeps no stand-in log | Kept: a run that fails logs both, and that is when they are read |
| Task 13 m5 | `holdRunImages`' inspect and pull have no deadline | Carried: Task 7 |
| Final f1 | `TestInstallTextNamesTheMirror` sets `o.MirrorNote` twice, the first dead | Carried: this task |
| Final f2 | `sink.finish` glues the two streams' partial lines into one line | Carried: this task |

- [ ] **Step 1: Write the failing tests**

In `internal/deploy/command_test.go`, `TestSinkFinishPassesOnEachPartialLine` wants after `finish` exactly `"the command\nerr one\nout one\nout two\nerr two\nrollproof: exit-nonzero, exit code 1\n"`: a line end now stands between the two partial lines. And:

```go
func TestSinkFinishWithOnePartialLine(t *testing.T) {
	// A sink whose standard error alone holds a partial line "err two" (its standard output none):
	// after finish(ExitedNonZero, 1) the evidence ends "err two\nrollproof: exit-nonzero, exit code 1\n",
	// with no empty line before "err two".
}
```

In `internal/mask/mask_test.go`: `TestNilMasker` also wants `New(nil).Writer(w)` to give `w` itself; `TestWriterAcrossWrites` also writes, to a new writer of the same masker, one write of 1,000 bytes `x`, and wants the writer it wraps to hold at least `1000-(longest-1)` bytes before `Close`, and none of it closed.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/deploy/ ./internal/mask/ -run 'TestSinkFinish|TestNilMasker|TestWriterAcrossWrites' -v`
Expected: FAIL: `TestSinkFinishPassesOnEachPartialLine` gets `out twoerr two`. The two `mask` additions pass already: they pin what step 5's reviews found untested.

- [ ] **Step 3: Make the changes**

- `internal/deploy/command.go`, `sink.finish`: between `s.put(s.stdout)` and `s.put(s.stderr)`, when both partial lines are not empty, `s.put([]byte("\n"))`. Its comment: "finish passes on each stream's partial line, standard output's first, with a line end between them when both hold one, then writes the closing line …".
- `internal/watch/counts_test.go`, `TestInstallTextNamesTheMirror`: the first assignment of `o.MirrorNote`, which the second replaces before it is used, is removed; nothing else changes.
- `internal/report/mask_test.go`, `TestMaskSharesNoMemoryWithTheRecord`: the two messages print the value, `"<nil>"` when the pointer is nil, through a helper `text(p *string) string` with the comment "text is *p quoted, or <nil>, for a message".
- `internal/mask/mask.go`: `Writer`'s comment gains the sentence "A writer it returns is not safe for concurrent use: one goroutine at a time writes to it and closes it."

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/deploy/ ./internal/mask/ ./internal/watch/ ./internal/report/ -v -run 'TestSinkFinish|TestNilMasker|TestWriterAcrossWrites|TestInstallTextNamesTheMirror|TestMaskSharesNoMemoryWithTheRecord'`, then `gofmt -l .`, `go test ./...` and the four vets.
Expected: PASS, no output.

- [ ] **Step 5: Commit**

```
git add internal/deploy/command.go internal/deploy/command_test.go internal/watch/counts_test.go internal/report/mask_test.go internal/mask/mask.go internal/mask/mask_test.go
git commit -m "Put a line end between a command's two partial lines, and clear small test and comment faults that step 5's reviews left"
```
