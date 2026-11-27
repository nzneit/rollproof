# Task 9: in the new repository: the module path, the workflows' paths, one paragraph of the README, the commit map, and one pull request

Read section 13 ("Graduation", its step 4; "During incubation", its three bullets), section 14, point 34 ("What may be published", "Private before public"), `docs/proofs/README.md` ("The form of a record"), and the README's decisions 9 to 14 and Review Focus 3 and 4, before this task.

This task needs the maintainer's word before its Step 12, and the maintainer's merge after it. Until the word it stops at Step 11.

### Task 9: one branch of the new repository moves the project in; the unit tests pass on its pull request

**Files**, under `WORK/repo/` (a clone of `OWNER/rollproof`):
- Modify: `go.mod`; every `.go` file of the main module that imports `rollproof/internal/…`; the five files under `.github/workflows/`; `hack/action/action_test.go`; `README.md`; `docs/proofs/README.md`
- Create: `docs/commit-map.md`, `docs/proofs/p34-split-audit.md`
- Outside the repository: `WORK/unit-move-in.log`

**Interfaces:**
- Consumes: `OWNER/rollproof` (Task 8); `WORK/commit-map` and the four outputs of Task 7's search; `WORK/publish-audit.sh` (Task 1).
- Produces: `main` of `OWNER/rollproof` with the project at its root under the module path `github.com/OWNER/rollproof`, its workflows reading from the root, `docs/commit-map.md` and the record of the split. Tasks 10 and 11 start there.

- [ ] **Step 1: Clone, and make the branch**

Each alone:

```
git clone https://github.com/OWNER/rollproof.git /tmp/opencode/rollproof-graduation/repo
git -C /tmp/opencode/rollproof-graduation/repo switch -c move-in
```

Expected from the first: it reports the objects it received and names no warning that the repository is empty.

- [ ] **Step 2: The module path**

From `WORK/repo`, with `OWNER` replaced by the account's name:

```
go mod edit -module github.com/OWNER/rollproof
grep -rl --include='*.go' --exclude-dir=proofs '"rollproof/internal/' . | xargs -r sed -i 's#"rollproof/internal/#"github.com/OWNER/rollproof/internal/#g'
```

Expected from the second: no output. `grep -rn '"rollproof/' --include='*.go' .` then prints two lines, each a test's string that names an image repository, `"rollproof/fixture`, in `fixtures/standin/main_test.go` and `e2e/mirror_helpers_test.go`: they stay. `go build ./...` passes. The proofs module is not touched: nothing imports it.

- [ ] **Step 3: The workflows' paths**

Line numbers are those of the split's tree, which are those of `5eb4cd0`.

| File | Lines | Now | Becomes |
| --- | --- | --- | --- |
| `rollproof-unit.yml` | 5 to 7 | `paths:` and its two items | nothing, so that `on:` holds `pull_request:` alone |
| `rollproof-unit.yml` | 15 | `working-directory: projects/rollproof` | nothing: the line goes |
| `rollproof-action.yml` | 20 | the same | nothing |
| `rollproof-e2e.yml` | 19 | the same | nothing |
| `rollproof-mirror.yml` | 12 | the same | nothing |
| `rollproof-proofs.yml` | 682, 712, 757 | the same | nothing |
| `rollproof-proofs.yml` | 73 | `working-directory: projects/rollproof/proofs` | `working-directory: proofs` |
| unit 28, action 35, e2e 35, 54, 86, mirror 29, proofs 693, 723, 765 | each | `cache-dependency-path: projects/rollproof/go.sum` | `cache-dependency-path: go.sum` |
| `rollproof-proofs.yml` | 81 | `cache-dependency-path: projects/rollproof/proofs/go.sum` | `cache-dependency-path: proofs/go.sum` |
| `rollproof-action.yml` | 73, 92, 109 | `uses: ./projects/rollproof` | `uses: ./` |
| `rollproof-action.yml` | 77, 95, 113 | `config: projects/rollproof/hack/action/testdata/pass.yaml`, `fail.yaml`, `cut.yaml` | `config: hack/action/testdata/pass.yaml`, `fail.yaml`, `cut.yaml` |

Check: `grep -n 'projects/rollproof' .github/workflows/rollproof-unit.yml .github/workflows/rollproof-action.yml .github/workflows/rollproof-e2e.yml .github/workflows/rollproof-mirror.yml .github/workflows/rollproof-proofs.yml` prints nothing.

- [ ] **Step 4: The test that follows the workflow**

In `hack/action/action_test.go`, in `TestTheScriptStartsTheToolInItsPlace`, both lines that hold `projects/rollproof/hack/action/testdata/pass.yaml` hold `hack/action/testdata/pass.yaml` instead. Check: `go test ./hack/action/ -run TestTheScriptStartsTheToolInItsPlace` passes.

- [ ] **Step 5: One paragraph of the README**

The paragraph that begins "The project's workflows sit in the host repository" becomes:

```text
The workflows are in `.github/workflows/`, and all but the unit tests start by hand only: `rollproof-e2e.yml` runs the tests under the build tag `cluster`, and then the suite under Helm 3 on `ubuntu-24.04` and under Helm 4 on `ubuntu-26.04`, side by side, in about 1 hour 30 minutes to 1 hour 50 minutes; `rollproof-mirror.yml` runs the mirror job; `rollproof-action.yml` runs the action's own test: a pass, a fail, and a run cut short by a time limit; `rollproof-proofs.yml` runs the proofs that need a hosted runner. `rollproof-unit.yml` runs the unit tests on every pull request.
```

- [ ] **Step 6: `docs/commit-map.md`**

CARRIED and DROPPED come from the map, each alone:

```
awk 'NR > 1 && $2 !~ /^0+$/ { n++ } END { print n }' /tmp/opencode/rollproof-graduation/commit-map
awk 'NR > 1 && $2 ~ /^0+$/ { n++ } END { print n }' /tmp/opencode/rollproof-graduation/commit-map
```

The first prints CARRIED, which is the number Task 7's Step 6 noted from the clone. When they differ, stop and report. Write the file's head with this text, its three placeholders filled:

```markdown
# Commit map

`rollproof` incubated in a folder of another repository and was split out of it with its history on DATE. The split changed every commit's hash. The documents under `docs/` cite commits by the hash they had there; this table gives, for each commit the split carried, that hash and the one it has here. An older hash resolves only in the repository the project left.

The table was made from the map that `git-filter-repo` 2.47.0 wrote, with the short hashes git prints for this repository. CARRIED commits were carried. DROPPED were left out: each changed nothing the split kept, or was a merge that lost a parent.

| There | Here |
| --- | --- |
```

Then the rows, from `WORK/repo`:

```
awk 'NR > 1 && $2 !~ /^0+$/ { printf "| `%s` | `%s` |\n", substr($1, 1, 7), substr($2, 1, 7) }' /tmp/opencode/rollproof-graduation/commit-map >> docs/commit-map.md
```

Checks, each alone: `grep -c '^| `' docs/commit-map.md` prints CARRIED; and this prints nothing, so that no short hash names two commits:

```
awk 'NR > 1 && $2 !~ /^0+$/ { print substr($1, 1, 7); print substr($2, 1, 7) }' /tmp/opencode/rollproof-graduation/commit-map | sort | uniq -d
```

When it prints a hash, the rows that hold it are rewritten by hand with the shortest hash that is unique, from `git -C /tmp/opencode/rollproof-graduation/split rev-parse --short=8 HASH`, and the file's second paragraph says so.

- [ ] **Step 7: `docs/proofs/p34-split-audit.md`**

In the form of the proofs, its title "Point 34: the split and its audit". The repository the project left is not named, and no needle is written: its four things are written `the name`, `the row`, a row's ID and the research folder's path.

- **Question:** does the split keep the project and the five workflow files and nothing else, move the project to the root and drop the commits that become empty? And after the hand edits and the four replacements, does any version of any file, or any commit message, still hold what may not be published?
- **Machine and versions:** the system; `git` 2.56.0; `git-filter-repo` 2.47.0 through `uv` 0.11.25; the Python it ran on, read with `uvx --from 'git-filter-repo==2.47.0' python --version`, or not measured when that answers nothing; the commit of `main` that was cloned, by hash and subject.
- **Method:** the clone command of Task 7's Step 2; the search over the clone before the split, with the script's summary line; the four rules of the split as `replace-text.sh` prints them, with their needles replaced by the four names above; the command of Task 7's Step 5; the search after it; the checks of Task 7's Step 6, each with what it printed.
- **Result:** before the split, the summary line of the control and that HEAD's tree was clean; after it, the commits cloned, carried and dropped, the tags left (none, or their names), the sixteen names of the root, the five workflow files, the two summary lines of the search, `go test ./...` at the root, and how many commit messages cited a hash before the split, which the tool rewrote to the new ones.
- **Decision:** that the audit found nothing, so the clone was pushed to the private repository (Task 8); or what it found and the commit that took it out. What stays open: a tag that was left, if any.

It ends with four pairs:

```
Plan assumed: "git-filter-repo 2.47.0, run on a fresh clone, keeps the project and the five workflow files and nothing else, moves the project to the root, and drops the commits that become empty."
Holds: yes

Plan assumed: "After the hand edits and the four replacements, no version of any file and no commit message of the clone holds the incubating repository's name, the ID of a row of its log, the text of the row of the incubation or a path under its research folder."
Holds: yes

Plan assumed: "The tree at the root is the project whole: its unit tests pass there."
Holds: yes

Plan assumed: "The commit map has one row for each commit the split carried, and each of its short hashes names one commit."
Holds: yes
```

- [ ] **Step 8: The row of the record**

In `docs/proofs/README.md`, "The list": a row that begins `34, the split`, filled from the record as its other rows are, with `none in this plan (Task 8 rests on it)` for the tasks.

- [ ] **Step 9: The checks**

Each alone. From `WORK/repo`: `gofmt -l .` prints nothing; `go test ./...` passes; the four vets pass; `bash -n action.sh` prints nothing; and the five workflows parse:

```
python3 -c 'import sys, yaml; [yaml.safe_load(open(f)) for f in sys.argv[1:]]; print("yaml ok")' .github/workflows/rollproof-unit.yml .github/workflows/rollproof-action.yml .github/workflows/rollproof-e2e.yml .github/workflows/rollproof-mirror.yml .github/workflows/rollproof-proofs.yml
```

Expected: `yaml ok`. Then, with `HOST` and `LOG` as everywhere:

```
bash /tmp/opencode/rollproof-graduation/publish-audit.sh HOST LOG tree /tmp/opencode/rollproof-graduation/repo
```

Expected: exit 0 and `0 distinct lines in 0 files`. The new files of this task hold no needle: a line that does is rewritten.

- [ ] **Step 10: Commit**

From `WORK/repo`, staging by path; every change of this task is under these paths:

```
git add go.mod cmd docs e2e fixtures hack internal .github README.md
git commit -m "Move rollproof to the root of its repository: the module path, the workflows, the README and a commit map"
```

`git status --short` (alone) then prints nothing.

- [ ] **Step 11: Write the pull request's body, and stop for the maintainer's word**

Write `WORK/move-in-pr.md`: one line per change of Steps 2 to 8, and the hash of Step 10's commit. No attribution to an AI tool.

Ask, in these words or plainer: "May I push the branch `move-in` to `OWNER/rollproof` and open its pull request to `main`? It runs the unit tests once (about 3 to 5 billed minutes while the repository is private)."

**State while it waits:** the move-in committed in `WORK/repo`; nothing pushed.

- [ ] **Step 12: After the word, push, open, and watch the unit tests**

Each alone, from `WORK/repo`:

```
git push origin move-in
gh pr create --base main --head move-in --title "Move rollproof to the root of its repository" --body-file /tmp/opencode/rollproof-graduation/move-in-pr.md
gh run list --repo OWNER/rollproof --workflow rollproof-unit.yml --branch move-in --limit 3
```

Expected from the listing: one run. When there is none after a few minutes, stop and report: the unit tests are not called passed (Review Focus 3). With the run's ID: `gh run watch ID --exit-status`, then `gh run view ID --log > /tmp/opencode/rollproof-graduation/unit-move-in.log`. A run that fails: stop; report the log. A fix is a commit on `move-in`, and its push a new word.

- [ ] **Step 13: The maintainer merges**

Report the pull request's address and ask: "Please merge it." After the answer: `gh pr view N --json state`, N its number: `"state": "MERGED"`. Then, each alone: `git -C /tmp/opencode/rollproof-graduation/repo switch main`; `git -C /tmp/opencode/rollproof-graduation/repo pull --ff-only`.

**Done when:** `main` of `OWNER/rollproof` holds the project at its root under the new module path, with the commit map and the record; the unit tests passed on its pull request; and the publication search over `WORK/repo` finds nothing.
