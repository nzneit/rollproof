# Task 12: the incubating repository's last pull request for the project

Read section 13 ("Graduation", its step 7 and the done-when list), section 14, point 34 ("The incubating repository afterwards"), the repository's rule file ("How to work", its first rule; "Changing a rule"), `tools/citations/README.md`, and the README's decisions 15 and Review Focus 5, before this task.

The rule file's rules apply to this task whole: it changes the repository's own files. It follows the rule for changing a rule as the first plan's first task did: the row first, then the files that cite it, in one commit.

**The maintainer agrees the row's text before this task starts** (point 34). The text of Step 4 is a proposal: a change to it changes the three entries of Step 5 and the README's sentence of Step 6, which follow from it.

This task needs the maintainer's word before its Step 9, and the maintainer's merge after it.

### Task 12: a row that amends the row of the incubation, the README's line on the folder, the directory and the five workflows removed

**Files**, from the repository root:
- Modify: `LOG` (one row; the marker on an older row), `tools/citations/unbuilt-paths.json` (three entries), `README.md` (one sentence)
- Remove: `projects/rollproof/` and `.github/workflows/rollproof-action.yml`, `rollproof-e2e.yml`, `rollproof-mirror.yml`, `rollproof-proofs.yml`, `rollproof-unit.yml`

**Interfaces:**
- Consumes: Tasks 8 to 11 (the repository exists, is public, and its runs are recorded): the row says so.
- Produces: the last item of the done-when list: "the incubating repository's log says that the project left."

- [ ] **Step 1: Read the placeholders from the log, and stop for the maintainer's agreement**

From the repository root: `LOG`'s newest row in its Decisions table gives the last number used; `NEWROW` is one more. `ROW` and `AMENDROW` are as the README defines them.

Ask, in these words or plainer: "Do you agree the text of the row in Task 12 of the plan: its Decision, its Why, and the three entries and the README's sentence that follow from it?" A change the maintainer asks for is made to Step 4's text before anything else runs.

- [ ] **Step 2: Make the branch**

Each alone, from the repository root:

```
git status --short
git switch main
git pull --ff-only
git switch -c rollproof-last
```

Expected: the first prints nothing; the last prints "Switched to a new branch 'rollproof-last'".

- [ ] **Step 3: Confirm what the citation check will meet**

The row's withdrawn sentence cites two paths under `projects/rollproof/`, and the rule files, the README and the citation tool's own documents cite `projects/`. The check resolves each against tracked files, and the directory's removal leaves none. Decision 15: three entries in the tool's list. On a scratch clone on 2026-11-25, with the row, its marker, the three entries and the README's sentence in place, the check gave `citation check: 0 errors, 0 warnings`.

- [ ] **Step 4: The row, and the marker**

In `LOG`, directly under the header of the Decisions table, above its newest row, one line. `«WITHDRAWN»` is the withdrawn sentence, in quotation marks and exactly as the older row holds it: the last sentence of its Decision cell, the only one that names the project's folder. `OWNER` is the account's name.

```markdown
| NEWROW | DATE | **Amends ROW.** The rule of ROW stands for a later project. One statement of its Decision cell is withdrawn: "«WITHDRAWN»" `rollproof` left this repository on DATE for a public repository of its own, `OWNER/rollproof`, with its history: `git-filter-repo` 2.47.0 split the directory and the five workflow files out of a clone, a search of every version of every file and of every commit message found nothing that may not be published, and a commit map there gives each carried commit's old and new hash. The directory and the workflows are removed here. The research reports stay, and so does the history, through which an old hash of the commit map still resolves. Whether the platform adopts `rollproof` stays a separate decision, with a row of its own. Until a later project makes the folder exist again, `projects/`, `projects/rollproof/` and `projects/rollproof/docs/specs/2026-11-13-rollproof-design.md` are listed in `tools/citations/unbuilt-paths.json`, each with its reason. | A public repository of its own is where a project that has left is maintained; a copy kept here would drift from it. The older row is amended and not superseded because only a statement of fact in it changed: its rule was written for any project meant to leave (AMENDROW). The citation check resolves a path against tracked files, and the folder resolves nowhere once it is empty: on a scratch clone on 2026-11-25 the removal alone made the check report 15 errors, in this log, in the two rule files, in the README and in the citation tool's own documents. The tool's list accepts a path that does not resolve, with its reason, and warns when the path exists again, which tells the next project to take the entries out. Rejected: keeping a mirror of the public repository, which would drift from it; a file kept in the folder to make `projects/` resolve, which would still leave the two paths of the withdrawn sentence; editing the withdrawn sentence in the older row, which AMENDROW rules out; changing the check to skip the log's rows, which would let a row cite a path that never existed. |
```

Then the older row, the one that begins `| ROW |`: its Decision cell gains, at its start, `**Amended by NEWROW** (the project left for a repository of its own). ` before the text it has, which stays as it is. The check requires both markers.

The two rule files do not change: the rule stands for a later project.

- [ ] **Step 5: The three entries**

In `tools/citations/unbuilt-paths.json`, after its last object and before the closing bracket, with a comma at the end of the line that was last, one object per line as the file's others are:

```text
{"path": "projects/", "reason": "No project incubates here since rollproof left on DATE for a repository of its own; the folder is cited by the rule that stands for a later one. Remove this entry when a project arrives."},
{"path": "projects/rollproof/", "reason": "rollproof left on DATE for a repository of its own; the path is cited only by the withdrawn sentence of the row that recorded its incubation."},
{"path": "projects/rollproof/docs/specs/2026-11-13-rollproof-design.md", "reason": "The design of rollproof left with it on DATE; the path is cited only by the withdrawn sentence of the row that recorded its incubation."}
```

- [ ] **Step 6: The README's sentence**

In the row of the table of folders that begins `| `projects/` |`, the last sentence, "The first is `projects/rollproof/`.", becomes "The first, `rollproof`, left on DATE for `OWNER/rollproof`."

- [ ] **Step 7: Remove the directory and the workflows**

Each alone, from the repository root:

```
git rm -r -q projects/rollproof
git rm .github/workflows/rollproof-action.yml .github/workflows/rollproof-e2e.yml .github/workflows/rollproof-mirror.yml .github/workflows/rollproof-proofs.yml .github/workflows/rollproof-unit.yml
```

Expected: no output from either. Files under `projects/rollproof/` that git ignored, the built binary and `.tools/`, stay on disk: they are the maintainer's to delete, and this task reports them and removes none. `git status --short --ignored projects/rollproof` (alone) lists them.

- [ ] **Step 8: The checks**

From the repository root, each alone: `bun tools/citations/cli.ts repo`. Expected: `citation check: 0 errors, 0 warnings`. `bun test tools/citations`. Expected: PASS.

- [ ] **Step 9: Commit**

From the repository root:

```
git add docs/decisions-and-open-items.md tools/citations/unbuilt-paths.json README.md
git commit -m "Record that rollproof left for a repository of its own, and remove it"
```

The hook runs the citation check before the commit and prints the same line.

- [ ] **Step 10: Stop for the maintainer's word; push; the maintainer merges**

Ask, in these words or plainer: "May I push the branch `rollproof-last` and open its pull request to `main`? It runs the citation check (about 1 billed minute)." After the word, each alone, from the repository root:

```
git push origin rollproof-last
gh pr create --base main --head rollproof-last --title "Record that rollproof left for a repository of its own, and remove it" --body-file /tmp/opencode/rollproof-graduation/last-pr.md
gh run list --branch rollproof-last --limit 3
```

`WORK/last-pr.md` is written before the ask: the row's text and one line per other change. With the run's ID: `gh run watch ID --exit-status`. A run that fails: stop and report its log.

Then report the pull request's address and ask: "Please merge it." After the answer: `gh pr view N --json state` prints `"state": "MERGED"`.

- [ ] **Step 11: Read the done-when list, item by item, and report**

Each alone:

1. "The new repository is public and holds the project at its root with its history": `gh repo view OWNER/rollproof --json visibility,defaultBranchRef`; from `WORK/repo`, `git pull --ff-only`, then `git ls-tree --name-only main` (the sixteen names) and `git rev-list --count main` (CARRIED, or more: the commits since).
2. "The audit found nothing and the commit map is there": `git ls-files docs/commit-map.md docs/proofs/p34-split-audit.md` names both.
3. "The unit tests and the three hosted workflows are green there": `gh run list --repo OWNER/rollproof --limit 8 --json conclusion,name,headBranch` shows the unit run of `move-in` and the three runs of Task 11 with `success`.
4. "The record of the security review is there, with every finding and the maintainer's ruling": `git ls-files docs/proofs/p13-security-review.md`.
5. "The incubating repository's log says that the project left": `LOG` on `main` holds the row that begins `**Amends ROW.**`, and the older row its marker.

Then the graduation folder: `rm -rf /tmp/opencode/rollproof-graduation/review-1` and the same for `review-2`, `review-3`, `review-4`, `second-reader` and `split`, which are throwaway. `WORK/repo` stays, and the report says where it is: a clone of the new repository at `main`, for whatever the maintainer wants done there next.

**Done when:** every item of Step 11 holds, with what each command printed in the report; or the maintainer has what failed and has said what comes next. An item whose command did not run is reported as unverified.
