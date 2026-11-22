# Task 13: the runs on GitHub, and their record

Read section 12 ("The suite's workflow", "The mirror job", "The action"), section 13 ("During incubation", "Graduation"), section 14, point 33, section 16 (the last list, of what no run or page has shown), `docs/proofs/README.md` ("The form of a record"), and the README's section "The runs on GitHub, and what they cost", before this task.

This task needs the maintainer's word before its Step 3. Until that word, it stops at Step 2, and its state is: everything built and checked on this machine; every check that only a hosted runner can make unverified, by name.

### Task 13: the whole suite, the mirror job and the cluster tests on this machine; then one run each of the action's test, the mirror job and the suite on GitHub; and their record

**Files:**
- Create: `docs/proofs/p32-step6-workflows.md`
- Modify: `docs/proofs/README.md` (a row in its list; its "Still unproved"), `README.md` (its sentence on Docker, only when the runs passed)

**Interfaces:**
- Consumes: every task before it, and Task 14, which comes before it; Task 1 merged and taken in.
- Produces: the record of the first runs of `rollproof-action.yml`, `rollproof-mirror.yml` and `rollproof-e2e.yml` on GitHub's hosted runners.

- [ ] **Step 1: Run everything that this machine can run, once**

Each command alone, from `projects/rollproof/`; after each cluster run, `nerdctl ps -a --format "{{.Names}}"` prints no name that begins `rollproof-`:

1. `gofmt -l .`, `go test ./...`, `go test -race ./internal/run/ ./internal/cluster/ ./internal/report/ ./cmd/rollproof/` and the four vets: no output, PASS.
2. `bash -n action.sh`: no output.
3. `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags cluster -p 1 -count=1 ./internal/cluster/ ./internal/images/ ./internal/probe/ ./internal/watch/ ./hack/action/ -timeout 60m`: PASS.
4. `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags e2e -p 1 -count=1 ./e2e/ -v -timeout 360m`: PASS, every row under both Helm majors, about two and a half hours.
5. `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags mirror -p 1 -count=1 ./e2e/ -run TestMirror -v -timeout 60m`: PASS.
6. From the repository root: `bun tools/citations/cli.ts repo`: exit 0 (an exit code of 2 means the check did not run: report it as unchecked).

**Stop rule.** A subtest that fails is reported to the maintainer with its results folder, its console and its standard error, and is not run again and not changed without the maintainer's word. kind's missed log line after four attempts on nerdctl (`docs/proofs/p08-kind-library.md`, its dated note) is reported the same way, as what it is.

- [ ] **Step 2: Check the preconditions, and stop for the maintainer's word**

- `main` holds `.github/workflows/rollproof-e2e.yml` and `.github/workflows/rollproof-action.yml`, and the step's branch has taken `main` in (Task 1, Step 7). Under another way of decision 1, its own precondition instead (decision 1).
- `git status --short` (alone) prints nothing.

Then ask the maintainer, in these words or plainer: "Step 6 is built and checked on this machine. May I push the branch `rollproof-step6-design`, start `rollproof-action.yml` and `rollproof-mirror.yml` once each, and, when both have passed, start `rollproof-e2e.yml` once? About 11, 12 and 172 to 205 billed minutes, about 195 to 230 in all (about $1.20 to $1.40), at most about 410 if every job ran to its limit. If the first two do not both pass, I stop there and report."

**State while it waits:** as this task's opening says. Nothing else in the plan waits for it.

- [ ] **Step 3: After the word, push and start the first two**

Each command alone, from the repository root:

```
git push origin rollproof-step6-design
gh workflow run rollproof-action.yml --ref rollproof-step6-design
gh workflow run rollproof-mirror.yml --ref rollproof-step6-design
gh run list --workflow rollproof-action.yml --branch rollproof-step6-design --limit 1
gh run list --workflow rollproof-mirror.yml --branch rollproof-step6-design --limit 1
```

With each run's ID from the listing, in place of ID, each alone: `gh run watch ID --exit-status`, then `gh run view ID --log > LOG`, LOG a file of the system's temporary folder named after the workflow, never a file of the repository. For a failed run of the action's test, also `gh run download ID --dir DIR`, DIR a new folder of the system's temporary folder, for its uploaded results. The mirror job uploads nothing: its failure is read from its log.

In the action's test's LOG, find for each of its three steps that call the action: the script's stop line, `::stop-commands::` and its token, where the runner's source has it masked as `***` (`ActionCommandManager.cs`, lines 116 to 121; where the masker is applied to a line of the log was not read), on both streams; after it the tool's own `::stop-commands::` and a token of 26 characters, as plain lines; and, at the end of the pass's and the fail's steps, `::`, the tool's token and `::`, as a plain line. The run cut short ends by a signal and has no such last line. In the mirror job's LOG and, in Step 4, the suite's, each run's output that the harness logs holds the tool's lines too; the first of them stops the runner's commands for the rest of that `go test` step, which runs no command, so this changes nothing there. The record quotes one example of each, with its time stamp.

When either run did not pass: stop; report to the maintainer with both logs and, for the action's test, the downloaded results; do not start the suite. A fix is a task of its own, and its run a new word. The record (Step 5) is written all the same, with `Holds: no` where a belief failed.

- [ ] **Step 4: When both passed, start the suite**

Each command alone:

```
gh workflow run rollproof-e2e.yml --ref rollproof-step6-design
gh run list --workflow rollproof-e2e.yml --branch rollproof-step6-design --limit 1
```

Then `gh run watch ID --exit-status` and `gh run view ID --log > LOG`, as in Step 3; for a failed Helm job, `gh run download ID --dir DIR`. The `cluster` job uploads nothing: its failure is read from the log. A job that fails is reported as in Step 3.

- [ ] **Step 5: Write the record**

`docs/proofs/p32-step6-workflows.md`, in the form that `docs/proofs/README.md` gives ("The form of a record"), its title "Point 32: the first runs of the action's test, the mirror job and the suite on GitHub's hosted runners". Each statement comes from the saved logs, quoted with their time stamps, or is marked "read" with its source; nothing is written from memory.

- **Question:** do the three workflows pass on GitHub's hosted runners, and what does each show of the beliefs of the plan for step 6 (below)?
- **Machine and versions:** for each job, from its log: the runner's version and the image's name and version (`Current runner version`, `Image`, `Version`), the region, Go as `actions/setup-go` found it; Docker's version where a log shows it, else the image's software list (`actions/runner-images`, read) with its commit; Helm and kubectl as `fetchhelm` printed them.
- **Method:** the three files at the commit that ran (`git rev-parse HEAD`, alone, before the push), the commands of Steps 3 and 4, the runs' and jobs' IDs, and each job's times from its first to its last log line.
- **Result:** for each job, its outcome and its time, and its billed minutes (each job's time rounded up to a whole minute, as GitHub bills it; GitHub's own bill is not read). For the action's test: each check step's output; for the run cut short, the time of the step's start and of the runner's line "has timed out", from the log, and the moment of `result.json`'s last write, from the lines that the step "Check the run cut short" prints first (`stat` of `result.json`, `ls -l` of the evidence). That moment is the last write of `result.json`, which a later warning's rewrite moves, so it is an upper bound for the moment the final file was written; the record says so. For the mirror job and the suite: each test's and subtest's `--- PASS` or `--- FAIL` lines and times, and the lines of the fourteenth row's control in both Helm jobs.
- **Decision:** what the plan or the design does because of the result; a belief that did not hold is a finding for the maintainer.
- The pairs, one for each belief, each `Holds:` followed by `yes`, `no` or `not measured here`:

```
Plan assumed: "Docker's export of the tool's image names its one file rollproof, with no ./ in front, and the action's script takes the binary out with create, export and tar."
Plan assumed: "Docker pulls the tool's image by digest from a registry on 127.0.0.1 in the job without TLS and with no setting of the daemon."
Plan assumed: "The action starts the tool in the workspace, so that a relative config and a relative out are read from there."
Plan assumed: "A timeout-minutes of 2 on the action's step sends SIGINT while cut.yaml's run is in its steady window or in set-up, and the tool writes its final result.json with the reason interrupted, its outputs and its job summary before SIGTERM, 7.5 s later."
Plan assumed: "A step that timeout-minutes cut, under continue-on-error, lets the next step run with its outcome failure, and does not fail the job."
Plan assumed: "The runner keeps the masked copy of the action's inner step's job summary under $RUNNER_TEMP/_runner_file_commands/ with the suffix -scrubbed, and a later step of the job reads it."
Plan assumed: "The mirror job passes on Docker: the nodes resolve the mirror's container name through Docker's DNS, and the cut of the nodes' route holds."
Plan assumed: "The suite passes under Helm 3 on ubuntu-24.04 and under Helm 4 on ubuntu-26.04 with Docker 29, the fourteenth row's control passing in both jobs."
Plan assumed: "One run of rollproof-e2e.yml takes 172 to 205 billed minutes, and one of rollproof-action.yml about 11."
Plan assumed: "In each step that calls the action, the runner's log shows the script's stop line, its token masked, and after it the tool's own stop lines and, unless a signal ended the run, its resume line, which name another token, as plain lines; the step reads no command after the script's stop line."
```

Then `docs/proofs/README.md`: a row in its list for point 32, filled from the record as its other rows are; in "Still unproved", the items the runs showed are taken out, and what they left (Podman, a runner that persists, `always()` on a cancelled step, a job's time limit through a composite action, a workflow command printed after the stop line, which no run plants, and the order in which the runner reads the lines of a step's two streams) stays or is added. When all three workflows passed, the README's sentence "On Docker, on GitHub's hosted runners, the tests under the build tag `cluster` of four packages and the suite's first fixture under both Helm majors have run (`docs/proofs/p24-hosted-runner.md`)." becomes "On Docker, on GitHub's hosted runners, the tests under the build tag `cluster` of five packages, the whole suite under both Helm majors, the mirror job and the action's own test have run (`docs/proofs/p24-hosted-runner.md`, `docs/proofs/p32-step6-workflows.md`)."

- [ ] **Step 6: Commit**

```
git add docs/proofs/p32-step6-workflows.md docs/proofs/README.md README.md
git commit -m "Record the first runs of rollproof's action test, mirror job and suite on GitHub's hosted runners"
```

The commit stays local. Pushing it, and the step's pull request, need the maintainer's word; the pull request runs `rollproof-unit.yml` and `citations` (README, "The runs on GitHub").

**Done when:** the three workflows have run once each on GitHub, their record is committed, and every belief above holds; or, when one did not, the maintainer has the record and the logs and has said what comes next.
