# Task 10: the workflows

Read section 12 ("The suite's workflow", "The action"), section 13 ("During incubation", the workflows), section 14, point 32, and decisions 1, 18, 19, 22 and 23 of the README, before this task.

### Task 10: `rollproof-e2e.yml`, `rollproof-action.yml`, and the unit workflow's new checks

**Files (from the repository root):**
- Create, or replace Task 1's placeholder when the step's branch has taken `main` in: `.github/workflows/rollproof-e2e.yml`, `.github/workflows/rollproof-action.yml`
- Modify: `.github/workflows/rollproof-unit.yml`
- Not changed: `.github/workflows/rollproof-mirror.yml`, `.github/workflows/rollproof-proofs.yml` (decision 22)

**Interfaces:**
- Consumes: `ROLLPROOF_E2E_HELM` and the results folders under `rollproof-e2e-results` (Task 7); `action.yml`, `Dockerfile` (Task 8); `hack/action/testdata/*.yaml` and `hack/actioncheck` (Task 9); `./hack/action/`'s cluster test (Task 9).
- Produces: two workflows started by hand only (`workflow_dispatch`), whose runs are Task 13's; every job names its runner's image.

The actions are pinned as the host's other workflows pin them: `actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1`, `actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0`, `actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2`.

- [ ] **Step 1: Write `.github/workflows/rollproof-e2e.yml`**

Exactly:

```yaml
name: rollproof e2e suite

# The suite of rollproof's design, section 12, on Docker, started by hand only. First the tests
# under the build tag cluster; then, when they have passed, the suite under Helm 3 on ubuntu-24.04
# and under Helm 4 on ubuntu-26.04, side by side. The harness takes the Helm major from
# ROLLPROOF_E2E_HELM and runs the fourteenth row's control in both jobs. Each go test has a limit
# below its job's, so that its report is not cut off. A Helm job that fails uploads the results
# folders that the harness keeps, for three days; a job that passes uploads nothing.

on:
  workflow_dispatch:

permissions:
  contents: read

defaults:
  run:
    shell: bash
    working-directory: projects/rollproof

jobs:
  cluster:
    runs-on: ubuntu-24.04
    timeout-minutes: 45
    env:
      CGO_ENABLED: "0"
      KIND_EXPERIMENTAL_PROVIDER: docker
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version: "1.27"
          cache-dependency-path: projects/rollproof/go.sum
      - name: The tests under the build tag cluster
        run: go test -tags cluster -p 1 -count=1 -v -timeout 35m ./internal/cluster/ ./internal/images/ ./internal/probe/ ./internal/watch/ ./hack/action/

  helm-3:
    needs: cluster
    runs-on: ubuntu-24.04
    timeout-minutes: 130
    env:
      CGO_ENABLED: "0"
      KIND_EXPERIMENTAL_PROVIDER: docker
      ROLLPROOF_E2E_HELM: "3"
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version: "1.27"
          cache-dependency-path: projects/rollproof/go.sum
      - name: Helm and kubectl
        run: go run ./hack/fetchhelm
      - name: The suite under Helm 3
        run: go test -tags e2e -p 1 -count=1 ./e2e/ -v -timeout 120m
      - name: Where the harness keeps the results
        id: results
        if: failure()
        run: echo "dir=${TMPDIR:-/tmp}/rollproof-e2e-results" >> "$GITHUB_OUTPUT"
      - uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2
        if: failure()
        with:
          name: e2e-helm-3-results
          path: ${{ steps.results.outputs.dir }}
          retention-days: 3
          if-no-files-found: warn

  helm-4:
    needs: cluster
    runs-on: ubuntu-26.04
    timeout-minutes: 130
    env:
      CGO_ENABLED: "0"
      KIND_EXPERIMENTAL_PROVIDER: docker
      ROLLPROOF_E2E_HELM: "4"
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version: "1.27"
          cache-dependency-path: projects/rollproof/go.sum
      - name: Helm and kubectl
        run: go run ./hack/fetchhelm
      - name: The suite under Helm 4
        run: go test -tags e2e -p 1 -count=1 ./e2e/ -v -timeout 120m
      - name: Where the harness keeps the results
        id: results
        if: failure()
        run: echo "dir=${TMPDIR:-/tmp}/rollproof-e2e-results" >> "$GITHUB_OUTPUT"
      - uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2
        if: failure()
        with:
          name: e2e-helm-4-results
          path: ${{ steps.results.outputs.dir }}
          retention-days: 3
          if-no-files-found: warn
```

- [ ] **Step 2: Write `.github/workflows/rollproof-action.yml`**

Exactly:

```yaml
name: rollproof action test

# The action's own test (rollproof's design, section 12, "The action"), started by hand only. The
# job builds the tool's image and pushes it to a registry in the job, builds the fixture's images as
# the suite's harness builds them, and uses the action from its folder three times: a pass, from a
# file whose release name only env: on the step sets; a fail, read by a later step; and a run cut
# short by the step's time limit. After each run, hack/actioncheck checks the step's outputs, its
# results folder and its job summary, read in the runner's masked copy, and fails loudly when that
# copy is not where it was. A job that fails uploads the three results folders, for three days.

on:
  workflow_dispatch:

permissions:
  contents: read

defaults:
  run:
    shell: bash
    working-directory: projects/rollproof

jobs:
  action:
    runs-on: ubuntu-24.04
    timeout-minutes: 30
    env:
      CGO_ENABLED: "0"
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version: "1.27"
          cache-dependency-path: projects/rollproof/go.sum
      - name: The job's registry
        run: docker run -d --name rollproof-action-registry -p 127.0.0.1:5000:5000 docker.io/library/registry:3@sha256:82f6ff4a7f982960c8aa51617fea3aabc485df6e55cce80d22deb1de36f1bc3c
      - name: Wait until the job's registry answers
        run: |
          for attempt in $(seq 1 30); do
            if curl --silent --fail --output /dev/null http://127.0.0.1:5000/v2/; then
              exit 0
            fi
            sleep 1
          done
          echo "the job's registry did not answer on 127.0.0.1:5000 within 30 seconds" >&2
          exit 1
      - name: The tool's image, pushed to the job's registry and named by its digest
        id: image
        run: |
          dir=$(mktemp -d "$RUNNER_TEMP/rollproof-image-XXXXXX")
          go build -trimpath -ldflags "-s -w -X main.version=dev" -o "$dir/rollproof" ./cmd/rollproof
          cp Dockerfile "$dir/Dockerfile"
          docker build -t 127.0.0.1:5000/rollproof:ci "$dir"
          docker push 127.0.0.1:5000/rollproof:ci
          ref=$(docker image inspect --format '{{index .RepoDigests 0}}' 127.0.0.1:5000/rollproof:ci)
          case $ref in
            127.0.0.1:5000/rollproof@sha256:*) ;;
            *) echo "the pushed image has no digest of the job's registry: $ref" >&2; exit 1 ;;
          esac
          echo "ref=$ref" >> "$GITHUB_OUTPUT"
      - name: The fixture's images, built as the suite's harness builds them
        run: |
          for version in previous candidate; do
            dir=$(mktemp -d "$RUNNER_TEMP/rollproof-image-XXXXXX")
            go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$dir/svc" ./fixtures/service
            cp fixtures/service/Dockerfile "$dir/Dockerfile"
            docker build -t "rollproof-fixture:$version" "$dir"
          done

      - name: A pass, with a variable that only the step's env sets
        id: pass
        uses: ./projects/rollproof
        env:
          ROLLPROOF_ACTION_TEST_RELEASE: orders
        with:
          config: projects/rollproof/hack/action/testdata/pass.yaml
          image: ${{ steps.image.outputs.ref }}
          out: ${{ runner.temp }}/results-pass
      - name: Check the pass
        env:
          VERDICT: ${{ steps.pass.outputs.verdict }}
          OUT: ${{ steps.pass.outputs.out }}
        run: |
          # The action's step stopped the runner's workflow commands for itself alone. This notice
          # shows, as an annotation of the run, that the next step's commands are read again.
          echo "::notice title=rollproof action test::workflow commands are read again in the step after the action"
          go run ./hack/actioncheck -verdict "$VERDICT" -out "$OUT" -want-verdict pass -want-out "$RUNNER_TEMP/results-pass" -step-files "$RUNNER_TEMP/_runner_file_commands"

      - name: A fail, read by a later step
        id: fail
        uses: ./projects/rollproof
        continue-on-error: true
        with:
          config: projects/rollproof/hack/action/testdata/fail.yaml
          image: ${{ steps.image.outputs.ref }}
          out: ${{ runner.temp }}/results-fail
      - name: Check the fail
        env:
          OUTCOME: ${{ steps.fail.outcome }}
          VERDICT: ${{ steps.fail.outputs.verdict }}
          OUT: ${{ steps.fail.outputs.out }}
        run: |
          if [ "$OUTCOME" != failure ]; then echo "the step's outcome is $OUTCOME; want failure" >&2; exit 1; fi
          go run ./hack/actioncheck -verdict "$VERDICT" -out "$OUT" -want-verdict fail -want-decided probe-failures -want-out "$RUNNER_TEMP/results-fail" -step-files "$RUNNER_TEMP/_runner_file_commands"

      - name: A run cut short by the step's time limit
        id: cut
        uses: ./projects/rollproof
        continue-on-error: true
        timeout-minutes: 2
        with:
          config: projects/rollproof/hack/action/testdata/cut.yaml
          image: ${{ steps.image.outputs.ref }}
          out: ${{ runner.temp }}/results-cut
      - name: Check the run cut short
        env:
          OUTCOME: ${{ steps.cut.outcome }}
          VERDICT: ${{ steps.cut.outputs.verdict }}
          OUT: ${{ steps.cut.outputs.out }}
        run: |
          stat -c '%n was last written at %y' "$OUT/result.json" || true
          ls -l --time-style=full-iso "$OUT/evidence" || true
          if [ "$OUTCOME" != failure ]; then echo "the step's outcome is $OUTCOME; want failure" >&2; exit 1; fi
          go run ./hack/actioncheck -verdict "$VERDICT" -out "$OUT" -want-verdict not-verified -want-decided interrupted -want-out "$RUNNER_TEMP/results-cut" -step-files "$RUNNER_TEMP/_runner_file_commands"

      - uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2
        if: failure()
        with:
          name: action-test-results
          path: |
            ${{ runner.temp }}/results-pass
            ${{ runner.temp }}/results-fail
            ${{ runner.temp }}/results-cut
          retention-days: 3
          if-no-files-found: warn
```

The `config` inputs are relative to the workspace, which the action enters before it starts the tool (decision 16); the pass run shows it. The outcome of a step that a time limit on its `uses:` cut short is `failure` (`docs/proofs/p03-p04-p20-github-runner.md`, the `stcomp` row). Under `continue-on-error`, such a step lets the next step run with its outcome `failure`, and does not fail the job: GitHub's runner (`actions/runner`) gives a step that timed out the result Failed (`src/Runner.Worker/StepsRunner.cs`), and `ExecutionContext.ApplyContinueOnError` applies to a Failed result, setting the outcome to failure and the conclusion to success (source read at `7e211dc` on 2026-11-22, not run). Task 13's record holds the belief as a pair. The step "Check the run cut short" prints the moment `result.json` and the evidence's files were last written before it checks the outcome, so that the log of a job that passed keeps them.

- [ ] **Step 3: Add the new checks to `.github/workflows/rollproof-unit.yml`**

After the step "Vet", insert:

```yaml
      - name: Vet the code under each build tag
        run: |
          go vet -tags cluster ./...
          go vet -tags e2e ./...
          go vet -tags mirror ./...
```

After the step "The binary is statically linked", append:

```yaml
      - name: Build for arm64
        run: env CGO_ENABLED=0 GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.version=dev" -o bin/rollproof-arm64 ./cmd/rollproof
      - name: The arm64 binary is statically linked
        run: |
          file bin/rollproof-arm64
          file bin/rollproof-arm64 | grep -q "ARM aarch64"
          file bin/rollproof-arm64 | grep -q "statically linked"
```

Nothing else in the file changes. The arm64 binary is built and never run (section 8: the first release is built for amd64 and arm64).

- [ ] **Step 4: Nothing to do: the maintainer chose the first way of decision 1**

The maintainer chose the first way on 2026-11-22, so this step changes nothing. What the other two ways would have changed here:

- **The second way:** `.github/workflows/rollproof-proofs.yml` gains the values `e2e` and `action` of its input `part`, and jobs `e2e-cluster`, `e2e-helm-3`, `e2e-helm-4` (`if: inputs.part == 'e2e'`, with the `needs:` of Step 1) and `action` (`if: inputs.part == 'action'`), each a copy of its job above; the two new files stay as written, unrun until after the merge.
- **The third way:** both new files gain, under `on:`, `push: { branches: [rollproof-step6-design] }`, and every job `if: github.event_name == 'workflow_dispatch'`; Task 13's last commit takes both out again before the merge.

- [ ] **Step 5: Check the files**

Run, from `projects/rollproof/`, the arm64 build once on this machine, to see that it builds (it is never run here):

```
env CGO_ENABLED=0 GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.version=dev" -o bin/rollproof-arm64 ./cmd/rollproof
file bin/rollproof-arm64
```

Expected: the build succeeds, and `file` names an ARM aarch64 executable, statically linked. `bin/` is ignored by git.

Then read each workflow back against Steps 1 to 3, line by line: the same text, two spaces for each level of indentation, no tab.

Run, from the repository root, each command alone:

```
grep -nE '[DGO]-[0-9]{3}' .github/workflows/rollproof-e2e.yml .github/workflows/rollproof-action.yml .github/workflows/rollproof-unit.yml
python3 -c 'import sys, yaml; [yaml.safe_load(open(p)) for p in sys.argv[1:]]' .github/workflows/rollproof-e2e.yml .github/workflows/rollproof-action.yml .github/workflows/rollproof-unit.yml
bun tools/citations/cli.ts repo
```

Expected: the search prints nothing and exits 1, which is the pass: the files hold nothing that looks like one of the host's row IDs, which the citation check does not read for them, since it reads tracked Markdown only. The parse prints nothing and exits 0; when `python3` or its `yaml` module is missing, the parse is reported as unchecked, never as a pass. The citation check exits 0, as the host asks for any change. For the search and the citation check, an exit code of 2 means the check did not run: report it as unchecked, never as a pass.

The parse shows the YAML's syntax and nothing more. Nothing on this machine parses a workflow as GitHub does; GitHub's own parse comes when Task 13 starts the runs, and a fault it reports is fixed in these files.

Then, from `projects/rollproof/`: `gofmt -l .`, `go test ./...` and the four vets.

- [ ] **Step 6: Commit**

From `projects/rollproof/`, each command alone:

```
git add ../../.github/workflows/rollproof-e2e.yml ../../.github/workflows/rollproof-action.yml ../../.github/workflows/rollproof-unit.yml
git commit -m "Add rollproof's e2e suite and action test workflows, started by hand, and vet every build tag and build for arm64 in the unit workflow"
```

**State after this task:** the workflows are written and checked on this machine; none has run. Their runs are Task 13's.
