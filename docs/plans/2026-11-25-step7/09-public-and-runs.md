# Task 10: public. Task 11: three hosted runs and their record

Read section 13 ("Graduation", its steps 5 and 6, and the done-when list), section 12 ("The suite's workflow", "The mirror job", "The action"), section 14, point 32 (its first four bullets) and point 34 ("Private before public", "The series' runner"), `docs/proofs/README.md` ("The form of a record"), `docs/proofs/p32-step6-workflows.md`, and `docs/plans/2026-11-22-step6/09-hosted-runs.md` (Steps 3 to 5: what was read in the logs), before these tasks. Both work in `WORK/repo`, at `main` after Task 9's merge.

Each task needs the maintainer's word before its third step.

### Task 10: the repository is made public

**Files:**
- Modify on GitHub: the visibility of `OWNER/rollproof`

**Interfaces:**
- Consumes: Task 9 merged.
- Produces: a public `OWNER/rollproof`, whose workflows then run for nothing.

- [ ] **Step 1: Check the preconditions**

Each alone: `gh repo view OWNER/rollproof --json visibility,defaultBranchRef` prints `PRIVATE` and `main`; from `WORK/repo`, `git rev-parse --abbrev-ref HEAD` prints `main` and `git status --short` prints nothing.

- [ ] **Step 2: Stop for the maintainer's word**

Ask, in these words or plainer: "May I make `OWNER/rollproof` public? Its history, the commit map and the record of the security review become readable by anyone."

- [ ] **Step 3: After the word, change the visibility, and check it**

Each alone:

```
gh repo edit OWNER/rollproof --visibility public --accept-visibility-change-consequences
gh repo view OWNER/rollproof --json visibility,isPrivate
```

Expected: `"visibility": "PUBLIC"` and `"isPrivate": false`.

**Done when:** the repository is public.

### Task 11: the action's test, the mirror job and the suite run once each on `main`, and a record is written and pushed

**Files**, under `WORK/repo/`:
- Create: `docs/proofs/p34-graduation-runs.md`
- Modify: `docs/proofs/README.md` (a row for point 34, the runs), `README.md` (its sentence on what has run, only when all three passed)
- Outside the repository: `WORK/action-runs.log`, `WORK/mirror-runs.log`, `WORK/e2e-runs.log`, and the downloaded results of a job that failed

**Interfaces:**
- Consumes: Task 10.
- Produces: four green runs on `OWNER/rollproof`; the record that the done-when list asks for.

- [ ] **Step 1: Check the preconditions, and note the commit**

Each alone, from `WORK/repo`: `git log -1 --format='%H %s'` (the commit the workflows will run: noted); `gh workflow list --repo OWNER/rollproof` names the five workflows.

- [ ] **Step 2: Stop for the maintainer's word**

Ask, in these words or plainer: "May I start `rollproof-action.yml` and `rollproof-mirror.yml` once each, and, when both pass, `rollproof-e2e.yml` once, on `main` of `OWNER/rollproof`, and then push the commit that records them? A public repository's runs are free."

The times of 2026-11-22, on runners of 2 CPUs: about 11 minutes for the action's test, 12 for the mirror job, 16 for the suite's first job and 77 to 93 for each Helm job. A public repository's runners have 4 CPUs, where nothing has been measured (point 34, "The series' runner").

- [ ] **Step 3: After the word, start the first two together**

Each alone:

```
gh workflow run rollproof-action.yml --repo OWNER/rollproof --ref main
gh workflow run rollproof-mirror.yml --repo OWNER/rollproof --ref main
gh run list --repo OWNER/rollproof --workflow rollproof-action.yml --branch main --limit 1
gh run list --repo OWNER/rollproof --workflow rollproof-mirror.yml --branch main --limit 1
```

With each run's ID in place of ID, each alone: `gh run watch ID --repo OWNER/rollproof --exit-status`; then `gh run view ID --repo OWNER/rollproof --log > /tmp/opencode/rollproof-graduation/action-runs.log`, and the same with `mirror-runs.log`. For a failed run of the action's test, also `gh run download ID --repo OWNER/rollproof --dir /tmp/opencode/rollproof-graduation/action-results`. The mirror job uploads nothing.

When either run did not pass: stop; report to the maintainer with both logs and the downloaded results; do not start the suite. A fix is a task of its own, and its run a new word. The record (Step 5) is written all the same, with `Holds: no` where a belief failed.

- [ ] **Step 4: When both passed, start the suite**

Each alone:

```
gh workflow run rollproof-e2e.yml --repo OWNER/rollproof --ref main
gh run list --repo OWNER/rollproof --workflow rollproof-e2e.yml --branch main --limit 1
```

Then `gh run watch ID --repo OWNER/rollproof --exit-status` and `gh run view ID --repo OWNER/rollproof --log > /tmp/opencode/rollproof-graduation/e2e-runs.log`; for a failed Helm job, `gh run download ID --repo OWNER/rollproof --dir /tmp/opencode/rollproof-graduation/e2e-results`. A job that fails is reported as in Step 3.

- [ ] **Step 5: Write `docs/proofs/p34-graduation-runs.md`**

In the form that `docs/proofs/README.md` gives, its title "Point 34: the first runs of the action's test, the mirror job and the suite in rollproof's own repository". Each statement comes from the saved logs, quoted with their time stamps, or is marked "read" with its source; nothing is written from memory.

- **Question:** do the three workflows pass in the new repository, at its root, and what do they show of the beliefs below?
- **Machine and versions:** for each job, from its log: the runner's version and the image's name and version, the region, Go as `actions/setup-go` found it; Docker's version where a log shows it, else the image's software list (`actions/runner-images`, read) with its commit; Helm and kubectl as `fetchhelm` printed them. The runners' size, 4 CPUs, is GitHub's documentation for a public repository's standard runner, not read from the runner.
- **Method:** the commit that ran (Step 1); the commands of Steps 3 and 4; the runs' and jobs' IDs; each job's time from its first to its last log line.
- **Result:** for each job, its outcome and its time. For the action's test: what `docs/plans/2026-11-22-step6/09-hosted-runs.md`, Steps 3 to 5, read in the logs of 2026-11-22, read again here in the same way: each check step's output; the script's stop line with its token masked and the tool's own lines after it, quoted once with a time stamp; and the notice that shows that the runner reads commands again in the step after the action. For the mirror job and the suite: each test's and subtest's `--- PASS` or `--- FAIL` line and time, and the lines of the fourteenth row's control in both Helm jobs.
- **Decision:** what the design, the README or a workflow does because of the result; a belief that did not hold is a finding for the maintainer.

It ends with four pairs:

```
Plan assumed: "The action's test passes in the new repository: the action finds its configuration file at hack/action/testdata/ from the workspace, and its step's outputs and job summary are read as in the incubating repository."
Holds: yes

Plan assumed: "The mirror job passes there: the nodes resolve the mirror's container name through Docker's DNS, and the cut of their route holds."
Holds: yes

Plan assumed: "The suite passes there: the cluster tests of five packages, and every row under Helm 3 on ubuntu-24.04 and under Helm 4 on ubuntu-26.04, the fourteenth row's control giving steady-not-clean in both jobs."
Holds: yes

Plan assumed: "The three workflows take on a public repository's runners about what they took on a private repository's: about 25 minutes for the first two together and about 3 hours for the suite."
Holds: not measured here
```

The fourth pair's `Holds:` is filled from the times the record gives: `yes` when each is within a tenth of the times of Step 2, else `no`.

- [ ] **Step 6: The row, and the README's sentence**

In `docs/proofs/README.md`, "The list": a row that begins `34, the runs`, filled from the record as its other rows are.

When all three workflows passed, the README's sentence "On Docker, on GitHub's hosted runners, the tests under the build tag `cluster` of five packages, the whole suite under both Helm majors, the mirror job and the action's own test have run (`docs/proofs/p24-hosted-runner.md`, `docs/proofs/p32-step6-workflows.md`)." ends "(`docs/proofs/p24-hosted-runner.md`, `docs/proofs/p32-step6-workflows.md`, `docs/proofs/p34-graduation-runs.md`)". When one did not, the README stays as it is.

- [ ] **Step 7: The checks, the commit, and the push**

From `WORK/repo`: `bash /tmp/opencode/rollproof-graduation/publish-audit.sh HOST LOG tree /tmp/opencode/rollproof-graduation/repo` exits 0. Then, each alone:

```
git add docs/proofs/p34-graduation-runs.md docs/proofs/README.md README.md
git commit -m "Record the first runs of rollproof's action test, mirror job and suite in its own repository"
git push origin main
```

The push is Step 2's word. `README.md` leaves `git add` when it did not change.

**Done when:** the three workflows have run once each on `main` of `OWNER/rollproof`, their record is pushed, and every belief above holds; or, when one did not, the maintainer has the record and the logs and has said what comes next.
