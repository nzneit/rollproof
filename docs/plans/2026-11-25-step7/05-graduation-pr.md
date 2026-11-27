# Task 6: the graduation pull request: pushed, opened, merged

Read section 13 ("Graduation", its step 1) and `docs/proofs/README.md` ("The form of a record"), before this task.

This task needs the maintainer's word before its Step 3. Until that word it stops at Step 2.

### Task 6: the branch is pushed, its pull request runs the unit tests and the citation check, and the maintainer merges it

**Files:**
- Create, outside the repository: `WORK/graduation-pr.md` (the pull request's body), `WORK/unit-graduation.log`, `WORK/citations-graduation.log`
- Modify: nothing in the repository

**Interfaces:**
- Consumes: the commits of Tasks 2 to 5 on the branch, and Task 1's scripts.
- Produces: `main` holding every commit of the graduation, which Task 7 clones.

- [ ] **Step 1: Check that step 1 of section 13 is complete**

From the repository root, each alone: `git status --short` prints nothing; `git rev-parse --abbrev-ref HEAD` prints `rollproof-step7-design`; `git log --oneline -10` shows the record of the security review, the hand edits and the trademark searches; `bash /tmp/opencode/rollproof-graduation/publish-audit.sh . LOG tree projects/rollproof` exits 0; `bun tools/citations/cli.ts repo` exits 0. When Task 1's stop or Task 5's stop has not been answered, this task does not start.

- [ ] **Step 2: Write the pull request's body, and stop for the maintainer's word**

Write `WORK/graduation-pr.md`: one line per task of this plan with the hash of its commit, read from `git log --oneline main..HEAD` (alone, from the repository root), and one line that names the record of the security review and what its four pairs say. No attribution to an AI tool.

Then ask, in these words or plainer: "May I push the branch `rollproof-step7-design` and open its pull request to `main`? It runs the unit tests once (about 3 to 5 billed minutes) and the citation check (about 1)."

**State while it waits:** everything of step 1 committed and local; Task 7 does not start.

- [ ] **Step 3: After the word, push and open**

Each alone, from the repository root:

```
git push origin rollproof-step7-design
gh pr create --base main --head rollproof-step7-design --title "rollproof: graduate: the security review, its fixes, the hand edits and the trademark searches" --body-file /tmp/opencode/rollproof-graduation/graduation-pr.md
gh run list --branch rollproof-step7-design --limit 5
```

Expected from the listing: two runs, `rollproof unit tests` and the citation check. With each run's ID in place of ID, each alone: `gh run watch ID --exit-status`; then, for each, `gh run view ID --log > /tmp/opencode/rollproof-graduation/unit-graduation.log` and the same with `citations-graduation.log`.

When either run fails: stop; report both logs to the maintainer. A fix is a commit on the branch, and its push a new word.

- [ ] **Step 4: The maintainer merges**

Report the pull request's address and ask: "Please merge it." The session does not merge it. After the maintainer's answer, from the repository root: `gh pr view N --json state,mergedAt`, N the pull request's number. Expected: `"state": "MERGED"` and a time.

**Done when:** the pull request is merged and its two runs passed, or the maintainer has the logs of a run that failed and has said what comes next.
