# Tasks 8 and 9: the tool's image and the action; its test's inputs, its checker, and a run on this machine

Read section 8 ("The action", whole), section 9 ("The probe's own image"), section 12 ("The action"), section 14, points 3, 4 and 20, `docs/proofs/p03-p04-p20-github-runner.md` (its Method and Decision), and decisions 9 to 18 of the README, before these tasks.

### Task 8: the tool's image, `action.yml` and `action.sh`, with tests that use fake engines

**Files:**
- Create: `Dockerfile`, `action.yml`, `action.sh` (at the project's root, `projects/rollproof/`)
- Create: `hack/action/doc.go`, `hack/action/action_test.go`

**Interfaces:**
- Consumes: the tool's command line `rollproof run -f FILE [--out DIR]` (as built); the outputs that the tool writes (Task 6).
- Produces: `action.yml`'s inputs `config` (default `rollproof.yaml`), `image` (default empty), `out` (default empty); its outputs `verdict` and `out`, mapped from `steps.run.outputs`.
- Produces: the step's variables, which `action.sh` reads: `ROLLPROOF_ACTION_CONFIG`, `ROLLPROOF_ACTION_IMAGE`, `ROLLPROOF_ACTION_OUT`, and the three a release writes, `ROLLPROOF_ACTION_VERSION`, `ROLLPROOF_ACTION_REPOSITORY`, `ROLLPROOF_ACTION_DIGEST`.
- Produces: `action.sh`'s functions `pick_engine`, `image_ref`, `take_binary` and `main`, each with its comment below, and its texts of failure:

| Failure | Text on standard error (each followed by a line end; exit 2) |
| --- | --- |
| uname | `rollproof: uname -s failed, so the runner's system is not known; the action runs only on Linux` |
| Not Linux | `rollproof: the runner is not Linux (uname -s printed NAME); the action runs only on Linux` |
| No engine | `rollproof: no container engine that kind supports: KIND_EXPERIMENTAL_PROVIDER names none, and none of docker, nerdctl and podman answers -v` |
| No image | `rollproof: no image to pull: before its first release this action carries no repository and no digest, so give the input image a reference with @sha256:` |
| No digest | `rollproof: the image IMAGE carries no digest, and before its first release this action carries none: give the input image a reference with @sha256:` |
| Pull | `rollproof: ENGINE could not pull REF` |
| Folder | `rollproof: no folder for the binary under DIR` |
| Create | `rollproof: ENGINE could not create a container from REF` |
| Take out | `rollproof: the binary /rollproof could not be taken out of REF` |
| Workspace | `rollproof: the workspace DIR cannot be entered` |
| Start | `rollproof: the binary taken out of REF could not be started` |

- [ ] **Step 1: Write the failing tests**

`hack/action/doc.go`:

```go
// Package action tests action.sh, the script of the project's composite action, and action.yml
// around it: with fake engines and no container, and, under the build tag cluster, with this
// machine's engine and the tool's own image. Its testdata holds the fixed files of the action's
// own test.
package action
```

In `action_test.go` (no build tag), a helper and the tests:

```go
// scriptRun is one run of action.sh in a world of fakes: a folder that is the whole PATH, holding
// links to the commands the script and the fakes need (uname, mktemp, tar, env, cat) and the fake
// engines the test writes; the script's environment beside PATH; the fakes' log, one command line
// a line ("ENGINE ARGS…"); and what the fake tool writes when it is started: its arguments, one a
// line, its environment and its working folder. GITHUB_OUTPUT names an empty file of the run's,
// which the script must leave empty, and RUNNER_TEMP and GITHUB_WORKSPACE folders of its own.
type scriptRun struct{ /* … */ }

func newScriptRun(t *testing.T) *scriptRun

// engine writes a fake engine of that name into the PATH folder: -v prints banner and exits with
// version; pull, create and export exit with their codes; create prints fake-id; export prints the
// tar it is given; rm exits 0. Each call appends its command line to the log.
func (r *scriptRun) engine(name string, b engineBehaviour)

type engineBehaviour struct {
	banner                                      string
	version, pull, create, export               int // exit codes
	tar                                         []byte
}

// toolTar is a tar of one file, name, with mode and content; fakeTool is a /bin/sh script that
// writes its arguments to FAKE_ARGS, its environment to FAKE_TOOL_ENV and its working folder to
// FAKE_TOOL_DIR, and exits 7.
func toolTar(t *testing.T, name string, mode int64, content []byte) []byte

// run runs bash with the absolute path of ../../action.sh, with the run's environment, its working
// folder a folder of its own, and gives the exit code and standard error. source runs
// `source PATH; FUNCTION`, PATH being that absolute path, the same way and gives standard output,
// the exit code and standard error. The path is absolute because the working folder is not the
// package's.
func (r *scriptRun) run() (code int, stderr string)
func (r *scriptRun) source(function string) (stdout string, code int, stderr string)
```

```go
func TestTheScriptStartsTheToolInItsPlace(t *testing.T) {
	// A fake docker whose -v prints "Docker version 28.0.4, build b8034c0" and whose export gives
	// toolTar("rollproof", 0o755, fakeTool). REF = "127.0.0.1:5000/rollproof@sha256:" + 64 a's.
	// ROLLPROOF_ACTION_CONFIG "projects/rollproof/hack/action/testdata/pass.yaml",
	// ROLLPROOF_ACTION_IMAGE REF, ROLLPROOF_ACTION_OUT "/tmp/rollproof-action-test/results",
	// KIND_EXPERIMENTAL_PROVIDER not set.
	// The exit code is 7, the fake tool's. Its arguments are exactly
	//   ["run", "-f", "projects/rollproof/hack/action/testdata/pass.yaml",
	//    "--out", "/tmp/rollproof-action-test/results"];
	// its environment holds "KIND_EXPERIMENTAL_PROVIDER=docker" and no line that begins
	// "ROLLPROOF_ACTION_"; its working folder is GITHUB_WORKSPACE.
	// The log is exactly ["docker -v", "docker pull " + REF,
	//   "docker create --name " + FOLDER + " " + REF + " /rollproof", "docker export fake-id",
	//   "docker rm fake-id"], FOLDER the base name of the folder below.
	// RUNNER_TEMP holds one folder, whose name begins "rollproof-action-", holding an executable
	// file rollproof. GITHUB_OUTPUT is empty. stderr's last line is
	// "rollproof: the tool from " + REF + ", through docker".
}

func TestTheScriptStartsTheToolWithoutOut(t *testing.T) {
	// As above with ROLLPROOF_ACTION_OUT empty and ROLLPROOF_ACTION_CONFIG empty: the arguments are
	// exactly ["run", "-f", "rollproof.yaml"].
}

func TestTheScriptsEngineRule(t *testing.T) {
	// r.source("pick_engine"), one row each:
	//   KIND_EXPERIMENTAL_PROVIDER "nerdctl", fakes docker and nerdctl   -> "nerdctl\n", 0, log empty
	//   KIND_EXPERIMENTAL_PROVIDER "finch", no fake                      -> "finch\n", 0
	//   KIND_EXPERIMENTAL_PROVIDER "nerdctl.lima", no fake               -> "nerdctl.lima\n", 0
	//   KIND_EXPERIMENTAL_PROVIDER "kind", fake docker                   -> "docker\n", 0
	//   docker's -v exits 1, nerdctl's prints "nerdctl version 2.4.2"    -> "nerdctl\n", 0
	//   docker's -v prints "Docker Desktop 4.40", nerdctl's as above     -> "nerdctl\n", 0
	//   docker's -v prints "Docker version 28.0.4\nanother line"         -> "docker\n", 0
	//   docker's -v as above, nerdctl's prints "nerdctl version 2.4.2"   -> "docker\n", 0
	//   docker's -v prints "Client: Docker version 28.0.4", nerdctl's as above
	//                                                                    -> "nerdctl\n", 0
	//   only podman, "podman version 4.9.3"                              -> "podman\n", 0
	//   no fake                                                          -> "", 1
}

func TestTheScriptsImageReference(t *testing.T) {
	// r.source("image_ref"), one row each:
	//   IMAGE "r.example/rollproof@sha256:aaa"                           -> that, 0
	//   IMAGE "r.example/rollproof", DIGEST "sha256:bbb"                 -> "r.example/rollproof@sha256:bbb", 0
	//   IMAGE "r.example/rollproof:v1", DIGEST "sha256:bbb"              -> "r.example/rollproof:v1@sha256:bbb", 0
	//   IMAGE "", REPOSITORY "ghcr.io/o/rollproof", DIGEST "sha256:bbb"  -> "ghcr.io/o/rollproof@sha256:bbb", 0
	//   nothing set                                                      -> "", 2, stderr
	//     "rollproof: no image to pull: before its first release this action carries no repository and no digest, so give the input image a reference with @sha256:\n"
	//     (the table's text of the row "No image")
	//   IMAGE "r.example/rollproof", no DIGEST                           -> "", 2, stderr
	//     "rollproof: the image r.example/rollproof carries no digest, and before its first release this action carries none: give the input image a reference with @sha256:\n"
	//     (the table's text of the row "No digest", with r.example/rollproof for IMAGE)
	// (IMAGE, REPOSITORY and DIGEST stand for ROLLPROOF_ACTION_IMAGE, _REPOSITORY and _DIGEST; each
	// output ends with a line end.)
}

func TestTheScriptFailsWithExit2AndWritesNoOutput(t *testing.T) {
	// Each row: exit 2, stderr's last line exactly the row's text of the table above, and
	// GITHUB_OUTPUT still empty. REF as in TestTheScriptStartsTheToolInItsPlace.
	// A row that reaches the engine's create wants exactly one create line in the log,
	// "docker create --name " + FOLDER + " " + REF + " /rollproof", FOLDER the one folder
	// rollproof-action-… under RUNNER_TEMP; a row that fails before it wants none.
	//   "not Linux": a fake uname that prints "Darwin"; the log stays empty.
	//   "a uname that fails": a fake uname that exits 1; the log stays empty.
	//   "no engine": no fake engine.
	//   "no image": ROLLPROOF_ACTION_IMAGE, ROLLPROOF_ACTION_REPOSITORY and ROLLPROOF_ACTION_DIGEST
	//     empty; the log holds "docker -v" only.
	//   "no digest": ROLLPROOF_ACTION_IMAGE "r.example/rollproof", ROLLPROOF_ACTION_DIGEST empty;
	//     the text's IMAGE is r.example/rollproof.
	//   "a pull that fails": docker's pull exits 1.
	//   "no folder for the binary": the PATH folder's mktemp is a fake that exits 1; the log ends
	//     with "docker pull " + REF; the text's DIR is the run's RUNNER_TEMP.
	//   "a create that fails": docker's create exits 125.
	//   "an export that fails": docker's export exits 1; the log ends "docker rm fake-id".
	//   "an image without the binary": export gives toolTar("other", 0o755, fakeTool); the log ends
	//     "docker rm fake-id".
	//   "a workspace that cannot be entered": GITHUB_WORKSPACE names a path that does not exist; the
	//     log ends "docker rm fake-id"; the text's DIR is that path.
	//   "a binary that cannot start": export gives toolTar("rollproof", 0o755, CONTENT), CONTENT the
	//     seven bytes "\x7fELF\x02\x01\x01" and 57 zero bytes, which execve refuses as a file of the
	//     wrong format. (A file that is missing or not executable never reaches the exec: take_binary
	//     stops it. A file of the wrong format does, and execve refuses it. Under execfail and the
	//     script's "set +e" the line after the exec then fails as the other failures do; without
	//     "set +e", bash 5.3.20 ends the script itself at the failed exec with its own code 126
	//     (tried on 2026-11-22), so this row fails too when the line is taken out (decision 17).)
}

func TestActionYAML(t *testing.T) {
	// ../../action.yml read with go.yaml.in/yaml/v3: inputs config (default "rollproof.yaml"),
	// image (default ""), out (default ""), and no other; outputs verdict with value
	// "${{ steps.run.outputs.verdict }}" and out with value "${{ steps.run.outputs.out }}", and no
	// other; runs.using "composite"; exactly one step, with id "run", shell "bash", no uses, run
	// (trimmed) exactly `exec bash "$GITHUB_ACTION_PATH/action.sh"`, and env exactly
	// {ROLLPROOF_ACTION_CONFIG: "${{ inputs.config }}", ROLLPROOF_ACTION_IMAGE: "${{ inputs.image }}",
	//  ROLLPROOF_ACTION_OUT: "${{ inputs.out }}", ROLLPROOF_ACTION_VERSION: "",
	//  ROLLPROOF_ACTION_REPOSITORY: "", ROLLPROOF_ACTION_DIGEST: ""}.
}

func TestTheDockerfile(t *testing.T) {
	// ../../Dockerfile's lines that are neither empty nor begin with "#" are exactly
	// ["FROM scratch", "COPY rollproof /rollproof", "USER 65532:65532", `ENTRYPOINT ["/rollproof"]`].
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./hack/action/ -v`
Expected: FAIL: `../../action.sh`, `../../action.yml` and `../../Dockerfile` do not exist.

- [ ] **Step 3: Write the three files**

`Dockerfile`, exactly:

```dockerfile
# The tool's own image: the static binary as /rollproof and nothing else, the layout of the probe's
# image (design, section 9). It is built from a folder that holds this file and the binary,
# rollproof: ENGINE build -t IMAGE FOLDER.
FROM scratch
COPY rollproof /rollproof
USER 65532:65532
ENTRYPOINT ["/rollproof"]
```

`action.yml`, exactly:

```yaml
name: rollproof
description: Test whether deploying a new version of a Kubernetes workload, and rolling it back, causes downtime.
inputs:
  config:
    description: Path of the config file; a relative path is read from the workspace.
    required: false
    default: rollproof.yaml
  image:
    description: >-
      The repository to pull the tool's own image from; the action adds the digest written into it
      at the release. A reference that already carries a digest is used as it is. Before the first
      release a reference with a digest is required.
    required: false
    default: ""
  out:
    description: Folder for the results; empty for a new folder under the runner's temporary directory.
    required: false
    default: ""
outputs:
  verdict:
    description: >-
      pass, fail or not-verified; empty when the tool never started, could not open its results
      folder or could not write the outputs, which a caller treats as not verified.
    value: ${{ steps.run.outputs.verdict }}
  out:
    description: The folder that holds result.json and the evidence.
    value: ${{ steps.run.outputs.out }}
runs:
  using: composite
  steps:
    - id: run
      shell: bash
      env:
        ROLLPROOF_ACTION_CONFIG: ${{ inputs.config }}
        ROLLPROOF_ACTION_IMAGE: ${{ inputs.image }}
        ROLLPROOF_ACTION_OUT: ${{ inputs.out }}
        # Written at each release; empty before the first.
        ROLLPROOF_ACTION_VERSION: ""
        ROLLPROOF_ACTION_REPOSITORY: ""
        ROLLPROOF_ACTION_DIGEST: ""
      run: exec bash "$GITHUB_ACTION_PATH/action.sh"
```

`action.sh`, exactly:

```bash
#!/usr/bin/env bash
# action.sh is the script of rollproof's composite action (action.yml). It checks that the runner
# is Linux, finds the container engine by the tool's own rule, pulls the tool's image by its
# digest, takes the binary out of it, and starts it with exec, in the workspace, so that the tool
# is the step's first process and gets the runner's signals (design, sections 8 and 14, point 20).
# It never writes an output: when it fails itself, it says why on standard error and exits 2, and
# the step's verdict stays empty. Sourced, it sets its shell options and defines its functions, and
# does not run main.
#
# Its input is the environment that action.yml's step gives it:
#   ROLLPROOF_ACTION_CONFIG      the input config
#   ROLLPROOF_ACTION_IMAGE       the input image
#   ROLLPROOF_ACTION_OUT         the input out
#   ROLLPROOF_ACTION_VERSION     written at a release; nothing reads it
#   ROLLPROOF_ACTION_REPOSITORY  written at a release: the image's repository
#   ROLLPROOF_ACTION_DIGEST      written at a release: the image's digest, sha256:…
# Besides bash and the engine, it uses uname, mktemp and tar.

set -euo pipefail
shopt -s execfail

# fail prints its message, after "rollproof: ", to standard error, and exits 2.
fail() {
  printf 'rollproof: %s\n' "$1" >&2
  exit 2
}

# pick_engine prints the engine's command name as the tool's pickEngine chooses it
# (internal/cluster/runner.go): KIND_EXPERIMENTAL_PROVIDER when it is one of the names that kind's
# command line takes, else the first of docker, nerdctl and podman whose -v exits 0 with a first
# line that begins with its banner. It returns 1 when there is none.
pick_engine() {
  case "${KIND_EXPERIMENTAL_PROVIDER-}" in
    docker | podman | nerdctl | finch | nerdctl.lima)
      printf '%s\n' "$KIND_EXPERIMENTAL_PROVIDER"
      return 0
      ;;
  esac
  local e banner out
  for e in docker nerdctl podman; do
    case $e in
      docker) banner='Docker version' ;;
      nerdctl) banner='nerdctl version' ;;
      podman) banner='podman version' ;;
    esac
    out=$("$e" -v 2>/dev/null) || continue
    case "${out%%$'\n'*}" in
      "$banner"*)
        printf '%s\n' "$e"
        return 0
        ;;
    esac
  done
  return 1
}

# image_ref prints the reference to pull: the input image when it holds @sha256:, else the input
# image, or the repository written at the release when the input is empty, with @ and the digest
# written at the release. Before the first release neither is written, and it fails.
image_ref() {
  local image=${ROLLPROOF_ACTION_IMAGE:-${ROLLPROOF_ACTION_REPOSITORY:-}}
  if [[ -z $image ]]; then
    fail "no image to pull: before its first release this action carries no repository and no digest, so give the input image a reference with @sha256:"
  fi
  case $image in
    *@sha256:*)
      printf '%s\n' "$image"
      ;;
    *)
      if [[ -z ${ROLLPROOF_ACTION_DIGEST:-} ]]; then
        fail "the image $image carries no digest, and before its first release this action carries none: give the input image a reference with @sha256:"
      fi
      printf '%s\n' "$image@$ROLLPROOF_ACTION_DIGEST"
      ;;
  esac
}

# take_binary takes /rollproof out of the image ref into the folder dir through the engine: it
# creates a container that never starts, with /rollproof as its command for an engine that wants
# one, exports its file system as a tar, keeps the one file rollproof, and removes the container.
# The route is export and tar because rootless nerdctl refuses cp from a container that never ran.
# The container carries the base name of dir, which begins rollproof-action- and is new for each
# run, so that a container left by a cancel between create and rm is known as the project's.
take_binary() {
  local engine=$1 ref=$2 dir=$3 id
  id=$("$engine" create --name "${dir##*/}" "$ref" /rollproof) || fail "$engine could not create a container from $ref"
  if ! "$engine" export "$id" | tar -x -C "$dir" -f - rollproof; then
    "$engine" rm "$id" >/dev/null 2>&1 || true
    fail "the binary /rollproof could not be taken out of $ref"
  fi
  "$engine" rm "$id" >/dev/null 2>&1 || true
  [[ -x $dir/rollproof ]] || fail "the binary /rollproof could not be taken out of $ref"
}

# main runs the action: Linux, the engine, the image, the binary, and then the tool in place of
# this script, in the workspace, with KIND_EXPERIMENTAL_PROVIDER set to the engine it found and
# its own variables unset.
main() {
  local system engine ref dir temp=${RUNNER_TEMP:-${TMPDIR:-/tmp}}
  system=$(uname -s) || fail "uname -s failed, so the runner's system is not known; the action runs only on Linux"
  [[ $system == Linux ]] || fail "the runner is not Linux (uname -s printed $system); the action runs only on Linux"
  engine=$(pick_engine) || fail "no container engine that kind supports: KIND_EXPERIMENTAL_PROVIDER names none, and none of docker, nerdctl and podman answers -v"
  ref=$(image_ref) || exit 2
  "$engine" pull "$ref" >&2 || fail "$engine could not pull $ref"
  dir=$(mktemp -d "$temp/rollproof-action-XXXXXX") || fail "no folder for the binary under $temp"
  take_binary "$engine" "$ref" "$dir"
  local args=(run -f "${ROLLPROOF_ACTION_CONFIG:-rollproof.yaml}")
  if [[ -n ${ROLLPROOF_ACTION_OUT:-} ]]; then
    args+=(--out "$ROLLPROOF_ACTION_OUT")
  fi
  cd "${GITHUB_WORKSPACE:-.}" || fail "the workspace ${GITHUB_WORKSPACE:-.} cannot be entered"
  printf 'rollproof: the tool from %s, through %s\n' "$ref" "$engine" >&2 || true
  unset ROLLPROOF_ACTION_CONFIG ROLLPROOF_ACTION_IMAGE ROLLPROOF_ACTION_OUT \
    ROLLPROOF_ACTION_VERSION ROLLPROOF_ACTION_REPOSITORY ROLLPROOF_ACTION_DIGEST
  export KIND_EXPERIMENTAL_PROVIDER=$engine
  # With set -e on, bash itself ends the script at most execs that fail, also before ||, with its
  # own code. With it off, execfail lets a failed exec return, and the next line fails as the
  # other failures do.
  set +e
  exec "$dir/rollproof" "${args[@]}"
  fail "the binary taken out of $ref could not be started"
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  main
fi
```

Then the helpers and tests of Step 1 in full.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./hack/action/ -v`, then `bash -n action.sh`, `gofmt -l .`, `go test ./...` and the four vets.
Expected: PASS; `bash -n` prints nothing; no output from `gofmt` and the vets.

- [ ] **Step 5: Commit**

```
git add Dockerfile action.yml action.sh hack/action/doc.go hack/action/action_test.go
git commit -m "Add the tool's image, the composite action and its script, which starts the tool with exec, with tests that run the script with fake engines"
```

### Task 9: the action's test inputs, its checker, and the script on this machine's engine

**Files:**
- Create: `hack/action/testdata/pass.yaml`, `hack/action/testdata/fail.yaml`, `hack/action/testdata/cut.yaml`, `hack/action/inputs_test.go`
- Create: `hack/action/action_cluster_test.go` (`//go:build cluster`)
- Create: `hack/actioncheck/main.go`, `hack/actioncheck/main_test.go`

**Interfaces:**
- Consumes: `action.sh`, `Dockerfile` (Task 8); the outputs and the summary (Tasks 4 to 6); `config.Load`; `cluster.DefaultRegistryImage`.
- Produces: the three fixed files of the action's test (below), and the variable `ROLLPROOF_ACTION_TEST_RELEASE`, which `pass.yaml` alone reads.
- Produces: `func check(o options) []string` in `hack/actioncheck`
  check returns the faults of the run that o describes, one sentence each, and none
  when every check holds: verdict and out as wanted; out/result.json final, with
  the wanted verdict and, when wantDecided is set, the reason that decided; and
  exactly one masked summary under stepFiles (step_summary_*-scrubbed) that holds
  the line "results: " + wantOut, whose first line begins "### rollproof: " and
  the verdict's word. GitHub does not promise the folder or the copy, so a folder
  that is not there is a fault that names it, never a pass.
- Produces: `type options struct { verdict, out, wantVerdict, wantOut, wantDecided, stepFiles string }` and the command line `go run ./hack/actioncheck -verdict V -out PATH -want-verdict WORD -want-out PATH [-want-decided CODE] -step-files DIR`; exit 0 with no output when `check` gives none, 1 with one fault a line on standard error, 2 for a flag it cannot read.
- Produces: `func run(args []string, stdout, stderr io.Writer) int` in `hack/actioncheck`
  run is the command: it reads args, the command line without the program's
  name, into options, and returns the exit code. A flag it cannot read gives 2,
  with the flag package's message on stderr. Otherwise it runs check and gives 0
  when check gives no fault, and 1 when it gives any, each fault a line on
  stderr. It writes nothing to stdout. main is
  os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)).

The fixed files, exactly. `hack/action/testdata/pass.yaml`:

```yaml
# The action's own test, its first run: the suite's first fixture, whose Helm release is named by
# a variable that only env: on the action's step sets (design, section 12, "The action"). The job
# builds the two images as the suite's harness does.
version: 1
images:
  previous: { local: "rollproof-fixture:previous" }
  candidate: { local: "rollproof-fixture:candidate" }
deploy:
  namespace: orders
  helm:
    release: "${env.ROLLPROOF_ACTION_TEST_RELEASE}"
    previous:
      chart: ../../../fixtures/chart
      values:
        - image: { repository: "${images.previous.repository}", digest: "${images.previous.digest}" }
    candidate:
      chart: ../../../fixtures/chart
      values:
        - image: { repository: "${images.candidate.repository}", digest: "${images.candidate.digest}" }
probe:
  http: { url: "http://orders.orders.svc/work?ms=20" }
```

`fail.yaml` is `pass.yaml` with the first comment line's "its first run: the suite's first fixture, whose Helm release is named by a variable that only env: on the action's step sets" replaced by "its second run: the candidate with no readiness probe, which fails in its deploy", `release: orders`, and the candidate's `values` list followed by a second item, `- { readiness: false, startDelay: "5s" }`.

`cut.yaml` is `pass.yaml` with the first comment's run described as "its third run: the first fixture with a steady window far longer than the step's time limit of 2 minutes, which ends it before the verdict", `release: orders`, and at its end:

```yaml
limits:
  steady: 10m
```

- [ ] **Step 1: Write the failing tests**

In `inputs_test.go` (no build tag):

```go
func TestTheActionTestsInputsLoad(t *testing.T) {
	// config.Load("testdata/pass.yaml", env), env giving ROLLPROOF_ACTION_TEST_RELEASE=orders and
	// nothing else: Deploy.Namespace "orders"; Deploy.Helm.Release "orders"; Previous and Candidate
	// both IsPath with Chart the absolute path of ../../fixtures/chart; each with exactly one value,
	// inline, whose "image" is {"repository": "${images.previous.repository}", "digest":
	// "${images.previous.digest}"} (the candidate's with candidate); Images exactly
	// {"previous": {Local: "rollproof-fixture:previous"}, "candidate": {Local:
	// "rollproof-fixture:candidate"}}; Probe.HTTP.URL "http://orders.orders.svc/work?ms=20";
	// Limits steady 10s, settle 15s, timeout 5m, silence 5s; Cluster.Workers 3.
	// testdata/pass.yaml with no variable: a config-rejected condition whose text names
	// ROLLPROOF_ACTION_TEST_RELEASE.
	// testdata/fail.yaml with no variable: as pass.yaml with the variable, but the candidate has a
	// second value, inline, {"readiness": false, "startDelay": "5s"}.
	// testdata/cut.yaml with no variable: as pass.yaml with the variable, but Limits.Steady is 10m.
}
```

In `hack/actioncheck/main_test.go`:

```go
func TestCheckAcceptsAWholeRun(t *testing.T) {
	// A results folder OUT holding result.json {"final": true, "verdict": "pass", "reasons": []};
	// a folder STEPS holding step_summary_1-scrubbed with
	//   "### rollproof: PASS `orders (Helm) on Kubernetes v1.37.0 in kind`\n\n```\nresults: OUT\n```\n",
	// step_summary_1 (the raw file, the same text), and step_summary_2-scrubbed with
	// "### other\n". check(options{verdict: "pass", out: OUT, wantVerdict: "pass", wantOut: OUT,
	// stepFiles: STEPS}) gives none.
}

func TestCheckReportsEachFault(t *testing.T) {
	// From the whole run above, one change a row, and exactly the one fault it gives:
	//   verdict "fail"                      `verdict is "fail"; want "pass"`
	//   out "/elsewhere"                    `out is "/elsewhere"; want "OUT"`
	//   result.json missing                 a fault that begins "result.json: "
	//   result.json final false             "result.json is not final"
	//   result.json verdict "fail"          `result.json's verdict is "fail"; want "pass"`
	//   wantDecided "interrupted", the file's decided reason internal-error
	//                                       `the reason that decided is "internal-error"; want "interrupted"`
	//   the summary's first line "### rollproof: FAIL"
	//                                       "the job summary begins \"### rollproof: FAIL\"; want \"### rollproof: PASS\""
	//   no scrubbed file holds the results line
	//                                       `no masked job summary under STEPS holds the line "results: OUT"`
	//   two scrubbed files hold it          `2 masked job summaries under STEPS hold the line "results: OUT"; want one`
	// (OUT and STEPS stand for the two folders' paths.)
}

func TestCheckFailsLoudWithoutTheStepFiles(t *testing.T) {
	// stepFiles a path that does not exist, everything else whole: exactly the fault
	// "the runner's folder of step files, PATH, is not there: GitHub does not promise it, so the job
	// summary was not read".
}

func TestCheckWordsOfEachVerdict(t *testing.T) {
	// The heading wanted for wantVerdict "pass", "fail" and "not-verified" is "### rollproof: PASS",
	// "### rollproof: FAIL" and "### rollproof: NOT VERIFIED".
}

func TestTheCommandsExitCodes(t *testing.T) {
	// run with the flags of the whole run of TestCheckAcceptsAWholeRun ("-verdict", "pass",
	// "-out", OUT, "-want-verdict", "pass", "-want-out", OUT, "-step-files", STEPS): 0, and stdout
	// and stderr empty.
	// The same with "-verdict", "fail": 1; stdout empty; stderr exactly one line, the fault,
	//   `verdict is "fail"; want "pass"` and a line end.
	// The same with a flag it does not know added, "-no-such-flag": 2; stdout empty.
}
```

In `action_cluster_test.go` (`//go:build cluster`):

```go
// The action's script on this machine's engine: the tool's image built from the Dockerfile, pushed
// by digest to a registry on the loopback, pulled, taken apart and started by action.sh, which runs
// the tool on a configuration that it rejects at once. No cluster is made.
func TestTheScriptRunsTheToolFromItsImage(t *testing.T) {
	// ENGINE: KIND_EXPERIMENTAL_PROVIDER when it is set, else the first of docker, nerdctl and
	// podman whose `info` exits 0; none fails the test.
	// 1. go build (CGO_ENABLED=0, -trimpath, -ldflags "-s -w -X main.version=action-test") of
	//    ../../cmd/rollproof into a temporary folder CTX as rollproof; ../../Dockerfile copied into
	//    CTX.
	// 2. ENGINE build -t rollproof-action-test:dev CTX.
	// 3. ENGINE save -o IMAGE.tar rollproof-action-test:dev, the archive in another temporary
	//    folder; tarball.ImageFromPath gives the image and its digest D.
	// 4. ENGINE run -d --name rollproof-action-test-registry -p 127.0.0.1:PORT:5000
	//    cluster.DefaultRegistryImage, PORT a free port (net.Listen on 127.0.0.1:0, then closed);
	//    GET http://127.0.0.1:PORT/v2/ answers 200 within 30 s; remote.Write of the image to
	//    127.0.0.1:PORT/rollproof-action-test@D with name.Insecure.
	// 5. bash with the absolute path of ../../action.sh, its working folder a temporary one, under
	//    a limit of 5 minutes, with this process's environment without its GITHUB_ variables, and
	//    GITHUB_ACTIONS=true, GITHUB_OUTPUT and GITHUB_STEP_SUMMARY naming two empty files of the
	//    test, GITHUB_WORKSPACE a folder WS that holds rejected.yaml ("version: 99\n"), RUNNER_TEMP
	//    a folder RT,
	//    ROLLPROOF_ACTION_CONFIG=rejected.yaml, ROLLPROOF_ACTION_IMAGE=127.0.0.1:PORT/rollproof-action-test@D,
	//    ROLLPROOF_ACTION_OUT=results and KIND_EXPERIMENTAL_PROVIDER=ENGINE.
	// It exits 2. WS/results/result.json is final, its verdict not-verified, its one reason
	// config-rejected. The outputs are exactly {"out": [WS/results], "verdict": ["not-verified",
	// "not-verified"]}. The summary begins "### rollproof: NOT VERIFIED\n" and holds
	// "[config-rejected]". RT holds one folder whose name begins "rollproof-action-", and its
	// rollproof, run with the argument version, prints a first line "rollproof action-test".
	// Cleanup, each a test error when it fails: ENGINE rm -f -v rollproof-action-test-registry;
	// ENGINE rmi rollproof-action-test:dev; ENGINE rmi 127.0.0.1:PORT/rollproof-action-test@D (the
	// one name that does not begin rollproof-, Global Constraints).
}
```

The outputs are read by a copy, in `action_cluster_test.go`, of `e2e`'s `readGitHubOutputs` (Task 7), with its comment; `e2e`'s is in a package that this one cannot import. The copy is the strict one: it reads the delimiter form only, and a line outside a value that is not `NAME<<DELIMITER`, the plain form `NAME=VALUE` among them, is an error.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./hack/action/ ./hack/actioncheck/ -v`, then `go vet -tags cluster ./...`
Expected: FAIL: `testdata/pass.yaml` does not exist; `check`, `options` and `run` are not defined.

- [ ] **Step 3: Write the files, each named one with its interface comment from the Interfaces block**

`hack/actioncheck/main.go` begins:

```go
// Command actioncheck checks one run of the action in the action's own test
// (.github/workflows/rollproof-action.yml in the host repository): the step's two outputs, the
// results folder's result.json, and the step's job summary, read in the runner's masked copy. It
// prints one line for each fault and exits 1, or exits 0 when every check holds; a flag it cannot
// read exits 2.
package main
```

Its `main` is one line, `os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))`, so that the tests reach every exit code through `run`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./hack/action/ ./hack/actioncheck/ -v`, then `gofmt -l .`, `go test ./...` and the four vets.
Expected: PASS, no output.

- [ ] **Step 5: Run the script against this machine's engine**

Run: `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags cluster -p 1 -count=1 ./hack/action/ -run TestTheScriptRunsTheToolFromItsImage -v -timeout 15m`
Expected: PASS. Afterwards `nerdctl ps -a --format "{{.Names}}"` prints no `rollproof-action-test-registry`, and `nerdctl images --format "{{.Repository}}"` prints no line that holds `rollproof-action-test`.

What this shows and what it does not: the route `create`, `export`, `tar` and the `exec` on rootless nerdctl, the pull by digest from a registry on the loopback, a relative `config` and `out` read from the workspace, and the outputs and the summary of a run started by the script. Not here: Docker, which Task 10's cluster job runs this test on and the action's own test runs as a whole (Task 13); Podman; a run cut short.

**Stop rule.** When the script's pull fails here, stop and report to the maintainer with the test's output. It was read, and not run, that nerdctl v2.4.2 asks a registry on a loopback address over plain HTTP (`pkg/imgutil/dockerconfigresolver/dockerconfigresolver.go`, lines 128 to 136), and that this machine's rootless containerd runs with a detached network namespace (its state folder holds `netns`), in which nerdctl's pulls use the host's network (from memory, not read). Do not add a flag to the script's pull to make this test pass.

- [ ] **Step 6: Commit**

```
git add hack/action/testdata/pass.yaml hack/action/testdata/fail.yaml hack/action/testdata/cut.yaml hack/action/inputs_test.go hack/action/action_cluster_test.go hack/actioncheck/main.go hack/actioncheck/main_test.go
git commit -m "Add the action's own test inputs and its checker, and run the action's script against this machine's engine and the tool's image"
```
