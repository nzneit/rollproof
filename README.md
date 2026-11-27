# rollproof

`rollproof` is a working name.

It tests, in a CI job or on a laptop, whether deploying a new version of a Kubernetes workload, and rolling it back, causes downtime.

Nothing is released; the design is docs/specs/2026-11-13-rollproof-design.md.

What works now, on Linux: `rollproof run` creates its own kind cluster; deploys through Helm, with or without a previous release (a run with none tests a restart), or through the commands driver; runs the `setup` commands; judges with the built-in HTTP probe or with a custom probe that `docs/probe-contract.md` describes; takes images from the engine or from an archive; and writes `result.json`, the console report and the evidence folder, and, on GitHub Actions, the step's outputs and its job summary. The whole suite has run on rootless nerdctl. On Docker, on GitHub's hosted runners, the tests under the build tag `cluster` of five packages, the whole suite under both Helm majors, the mirror job and the action's own test have run (`docs/proofs/p24-hosted-runner.md`, `docs/proofs/p32-step6-workflows.md`, `docs/proofs/p34-graduation-runs.md`). Podman is not promised: one test has run through it.

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

Two outputs. `verdict` is `pass`, `fail` or `not-verified`, the verdict that decides the tool's exit code: 0, 1 or 2. After a signal, as after a cancel, `verdict` is the verdict of the final `result.json`, and the tool ends by the signal instead, which a shell reports as status 130 for SIGINT and 143 for SIGTERM. The tool writes `verdict` as `not-verified` as soon as it has its results folder, and again when the verdict exists, so a run that was killed in between leaves `not-verified`. An empty `verdict` means that the action stopped before the tool started (for example, the runner is not Linux, it has no container engine, or the image could not be pulled) or that the tool could not open its results folder, or that the step's outputs file could not be written, which a warning on the console says: read it as not verified. `out` is the folder that holds `result.json` and the evidence.

The action uploads nothing. The step above uploads the folder, with `if: always()` so that it runs after a fail, after a run that was not verified and after a cancel. The evidence holds the chart's values and the probe's header values as they were given; only a registry password is masked. Keep the uploaded folder to yourself when those values are secret.

The step that calls the action must not carry `if: always()`: GitHub's runner does not cancel a running step whose condition still holds after a cancel, so the tool would not learn of it (read in the runner's source, not run). A cancel, the job's time limit and a `timeout-minutes` on the step reach the tool as SIGINT; it then writes its final `result.json`, its outputs and its job summary and prints its report before anything else, because GitHub's SIGTERM ends it 7.5 s later, and leaves the cluster for the next run to remove.

The tool writes the outputs and the job summary itself, when `GITHUB_ACTIONS` is `true`, each to the file that `GITHUB_OUTPUT` or `GITHUB_STEP_SUMMARY` names. So a runner that already holds the binary needs no action: a step whose script is `exec rollproof run` gives the same outputs and the same summary, and the same caution about `if: always()` holds. Without `exec`, the step's shell gets GitHub's signals and the tool does not. The job summary holds the console's report as Markdown, with every text from the cluster, the commands and the configuration inside code. GitHub's runner reads a line that a step prints as a workflow command when it begins with `::` after its leading white space, or holds `##[` anywhere, so on GitHub Actions the tool's first line on standard output and on standard error is `::stop-commands::` and a token, and its last line on standard output, when it ends by itself, is `::`, the token and `::` again: no line in between, a pod's message included, is read as a command, and a script that ran the tool can use commands afterwards. Two things are left open, because the runner reads the two streams apart: a line that the tool writes on standard error just before it ends may reach the runner after that last line, and is then read for commands, and after a run that ends at once, as a usage error does, the runner may read no command for the rest of the step. The action's script stops commands the same way, with a token of its own, and never resumes them, so its step reads none and neither thing arises there.

The configuration's commands and the chart run with the job's power, its secrets included (design, section 11): do not run the tool on a pull request's code with the base repository's secrets.

## The suite

Every fixture of the design's section 12 gives its expected verdict under Helm 3.22.1 and Helm 4.3.2, and the first fixture also on `kindest/node:v1.34.11`, in one run of the suite on 2026-11-19 and 2026-11-20.

To run the suite: `go run ./hack/fetchhelm` fetches under `.tools/` the Helm binaries and, for linux/amd64 only, kubectl 1.37.0, which the suite needs; the suite's first run pulls busybox 1.37.0 by its digest into `.tools/`; `go test -tags e2e -p 1 ./e2e/ -timeout 360m` runs every row under both Helm majors, in about two and a half hours on a workstation. The variable `ROLLPROOF_E2E_HELM` set to `3` or `4` runs one major, as in `env ROLLPROOF_E2E_HELM=4 go test -tags e2e -p 1 ./e2e/ -timeout 180m`; not set, it runs both. The fourteenth row's control runs under Helm 3 either way. On nerdctl, put `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0` before the command.

The mirror job, `go test -tags mirror -p 1 ./e2e/ -run TestMirror -v -timeout 60m` (with `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0` before it on nerdctl), runs the first fixture through a mirror that needs a login, sits under a path and serves TLS from a private authority, with the nodes' route to public registries cut, and then a run whose candidate the mirror lacks.

The workflows are in `.github/workflows/`, and all but the unit tests start by hand only: `rollproof-e2e.yml` runs the tests under the build tag `cluster`, and then the suite under Helm 3 on `ubuntu-24.04` and under Helm 4 on `ubuntu-26.04`, side by side, in about 1 hour 30 minutes to 1 hour 50 minutes; `rollproof-mirror.yml` runs the mirror job; `rollproof-action.yml` runs the action's own test: a pass, a fail, and a run cut short by a time limit; `rollproof-proofs.yml` runs the proofs that need a hosted runner. `rollproof-unit.yml` runs the unit tests on every pull request.

Every key of version 1 is built: `registries` and `as` in `images` came with step 5.

Run the unit tests with `go test ./...`.
