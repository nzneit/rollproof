# Task 4: the hand edits: the name, the row and the research paths out of the files as they are

Read section 13 ("Graduation", its step 1) and section 14, point 34 ("What may be published"), before this task. It rests on Task 1's scripts and on nothing else: it can go on while Task 1 waits for its rulings.

The rule covers the files as they are, which are fixed by hand in one commit; every old version of them is the split's (Task 7). This task is that one commit.

### Task 4: every place in the files as they are that holds a needle is edited by hand

**Files:**
- Modify: `internal/probe/http.go`, `docs/specs/2026-11-13-rollproof-design.md`, `docs/plans/2026-11-16-first-plan/01-host-repository.md`, `docs/plans/2026-11-16-first-plan/03-verdict-and-probe-lines.md`, `docs/plans/2026-11-16-first-plan/10-fixtures-and-workflow.md`, `docs/proofs/p03-p04-p20-github-runner.md`, `docs/proofs/p19-proxy.md`

**Interfaces:**
- Consumes: `WORK/publish-audit.sh` (Task 1).
- Produces: files that the publication search finds nothing in; the split's four rules (Task 7) take the same four things out of their old versions.

- [ ] **Step 1: Run the search, and read its list against this table**

From the repository root: `bash /tmp/opencode/rollproof-graduation/publish-audit.sh . LOG tree projects/rollproof`. Expected: exit 1 and 35 distinct lines in 7 files, as Task 1's Step 2 saw. On 2026-11-25 the places were these; line numbers are those of `5eb4cd0`:

| File | Lines | What they hold |
| --- | --- | --- |
| `docs/plans/2026-11-16-first-plan/01-host-repository.md` | 14, 35, 37, 62, 63, 76, 79, 87, 93, 99, 105 | the row as one line (79); eleven row IDs |
| the same | 25, 41, 44, 46, 47, 61, 62, 66, 79, 104, 105 | the research folder's path: bare in ten lines, three times under a date in the row |
| `internal/probe/http.go` | 291 | a path under the research folder |
| `docs/specs/2026-11-13-rollproof-design.md` | 3806 to 3809 | four paths under the research folder |
| `docs/plans/2026-11-16-first-plan/03-verdict-and-probe-lines.md` | 1759 | one path under the research folder |
| `docs/plans/2026-11-16-first-plan/10-fixtures-and-workflow.md` | 35, 229 | three paths under the research folder |
| `docs/proofs/p03-p04-p20-github-runner.md` | 144, 147, 352, 365, 380, 392 | the repository's name inside paths |
| `docs/proofs/p19-proxy.md` | 48, 49 | the repository's name inside paths |

A line that the search names and this table does not is a file changed since this plan was written. It is edited by the rules of Steps 2 to 7, whatever it holds: the name by Step 7's command, a row ID or the folder's path by Step 6's two substitutions, the row by Step 6's paragraph, a path under the folder by a sentence that says what the source is and that it is not published. It is named in the commit's report.

- [ ] **Step 2: `internal/probe/http.go`: the comment above `classify`**

Its three lines become two:

```go
// classify names how a request failed. The names come from the load generator of the experiment L5
// (section 3), whose report is not published; a failed lookup is class other, as section 6 says.
```

- [ ] **Step 3: The design, section 16: its first paragraph and its first table**

The paragraph "Paths in the first table are from the host repository's root. …" and the four rows of the table below it become:

```markdown
The first table's sources are research reports of the incubating repository. They are not published
and stay behind at graduation. Each is named by what it is and by the date of its folder.

| Source | What this design takes from it |
| --- | --- |
| Experiment L5, on a workstation: its report of 2026-11-12 | The experiment on a workstation: … |
| Experiment L6, on hosted runners: its report of 2026-11-12 | The 160 jobs on hosted runners: … |
| The reading W16: its report of 2026-11-12 | That `kubectl rollout status` does not wait for terminating pods, … |
| The search W18: its report of 2026-11-13 | The prior-art search and its limits |
```

The four cells of the second column stay as they are, whole.

- [ ] **Step 4: The first plan, `10-fixtures-and-workflow.md`: two lines**

The line that begins `Read first: section 12 (` ends, from `and from the repository root`, with "the Deployment's shape." That part becomes:

```text
and the experiment's service and its two manifests, `A-compliant.yaml` and `base.yaml`, which are
files of the incubating repository and are not published, for the service and the Deployment's shape.
```

The line that begins `Copy ` and ends `then make exactly these changes:` becomes:

```text
Copy the experiment's service source, a file of the incubating repository that is not published, unchanged, then make exactly these changes:
```

- [ ] **Step 5: The first plan, `03-verdict-and-probe-lines.md`: one line**

In the line of the first plan's Task 16 ("The built-in HTTP probe"), Step 7, that begins `With its interface comment from the Interfaces block.`, all after that first sentence becomes:

```text
Start from the experiment's load generator, a file of the incubating repository that is not published: its open-model loop, its `classify` function, unchanged, and its transport settings. Put a comment above `classify` that says it comes from the load generator of the experiment L5.
```

- [ ] **Step 6: The first plan, `01-host-repository.md`: the row, its IDs and the folder's path**

a. After the paragraph that begins "This plan's folder is committed with this task", one paragraph:

   ```text
   This file quotes the incubating repository's citation tool and its rule files. In what follows, `ROW` stands for the ID of a row of that repository's log and `RESEARCH/` for the path of its research folder; neither is published with the project.
   ```
b. In the file's first `Modify:` line, the text in parentheses becomes `(one row, directly under the table's header, above its newest row)`.
c. The sentence that begins "Insert this one line directly under the header of the Decisions table" becomes "Insert one line directly under the header of the Decisions table, above its newest row. It is one line in the file."
d. The fenced block that follows it, which holds the row as one line, becomes the paragraph "The row's text is not published with the project: it belongs to the incubating repository's log. Section 13 of the design says what it decided."
e. Two substitutions over the whole file, each alone, from `projects/rollproof/`:

   ```
   sed -i -E 's/\b[DGO]-[0-9]{3}\b/ROW/g' docs/plans/2026-11-16-first-plan/01-host-repository.md
   sed -i -E 's#docs/r[e]search/#RESEARCH/#g' docs/plans/2026-11-16-first-plan/01-host-repository.md
   ```

   Expected: `grep -c -E 'ROW|RESEARCH/' docs/plans/2026-11-16-first-plan/01-host-repository.md` prints 17 or more.

What stays in this file on purpose (decision 6): its description of the citation tool's change, its quotations of the two rule files' lines and of the README's row, and the name of the log's file.

- [ ] **Step 7: The two proof records: the name inside paths**

In `docs/proofs/p03-p04-p20-github-runner.md`, after the heading `## Method` and its blank line, one sentence: "The name of the repository in which the jobs ran is written `REPOSITORY` in the lines quoted below." In `docs/proofs/p19-proxy.md`, in the same place: "The name of the repository in which the run was made is written `REPOSITORY` in the file below."

Then, each alone, from the repository root:

```
bash /tmp/opencode/rollproof-graduation/publish-audit.sh . LOG replace projects/rollproof/docs/proofs/p03-p04-p20-github-runner.md
bash /tmp/opencode/rollproof-graduation/publish-audit.sh . LOG replace projects/rollproof/docs/proofs/p19-proxy.md
```

Expected: `grep -c REPOSITORY` on each file prints 7 and 3: the lines that held the name, and the sentence.

- [ ] **Step 8: The checks**

1. From the repository root: `bash /tmp/opencode/rollproof-graduation/publish-audit.sh . LOG tree projects/rollproof`. Expected: exit 0 and `0 distinct lines in 0 files`. On 2026-11-25 these edits, made in a clone's working tree, gave that line.
2. From `projects/rollproof/`: `gofmt -l .` prints nothing; `go test ./...` passes; the four vets pass.
3. From the repository root: `bun tools/citations/cli.ts repo`. Expected: `citation check: 0 errors, 0 warnings`.

- [ ] **Step 9: Commit**

From the repository root:

```
git add projects/rollproof/internal/probe/http.go projects/rollproof/docs/specs/2026-11-13-rollproof-design.md projects/rollproof/docs/plans/2026-11-16-first-plan/01-host-repository.md projects/rollproof/docs/plans/2026-11-16-first-plan/03-verdict-and-probe-lines.md projects/rollproof/docs/plans/2026-11-16-first-plan/10-fixtures-and-workflow.md projects/rollproof/docs/proofs/p03-p04-p20-github-runner.md projects/rollproof/docs/proofs/p19-proxy.md
git commit -m "Take the incubating repository's name, its row and its research paths out of the files as they are"
```

`git add` grows by any file Step 1 named beyond the table.

**Done when:** the publication search over the working tree finds nothing, the citation check passes, and the edits are one commit local to the branch.
