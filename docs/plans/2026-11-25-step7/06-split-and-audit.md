# Task 7: the split on a throwaway clone, and its audit before and after

Read section 13 ("Graduation", its step 2 and the done-when list), section 14, point 34 ("What may be published", "Private before public", "The tool of the split"), `docs/proofs/README.md` ("The form of a record"), and the README's Review Focus 1, 2 and 5, before this task. The manual of `git-filter-repo` 2.47.0, for `--path`, `--path-rename`, `--replace-text`, `--replace-message`, `--prune-empty`, `--prune-degenerate` and the files it writes under `.git/filter-repo/`.

This task needs the maintainer's word before its Step 3: `uv` downloads the tool into its cache. Nothing is pushed here or in the next task without a later word.

### Task 7: a fresh clone of `main` is split, and the search runs over it before and after

**Files:**
- Create, outside the repository: `WORK/split/` (the clone), `WORK/replace.txt`, `WORK/audit-before-split.txt`, `WORK/audit-after-split-history.txt`, `WORK/audit-after-split-head.txt`, `WORK/commit-map`
- Modify: nothing in any repository

**Interfaces:**
- Consumes: `main` (Task 6), `WORK/publish-audit.sh` and `WORK/replace-text.sh` (Task 1).
- Produces: `WORK/split/`: the clone after the split, its `main` the history that Task 8 pushes. `WORK/commit-map`: a copy of `.git/filter-repo/commit-map`, for Task 9's table. The four files of the search's output, for Task 9's record.

- [ ] **Step 1: Stop for the maintainer's word**

Ask, in these words or plainer: "May I let `uv` download git-filter-repo 2.47.0 into its cache and run it on a throwaway clone? Nothing is pushed."

- [ ] **Step 2: Make the clone**

An earlier `WORK/split` is not reused: when the folder exists, `rm -rf /tmp/opencode/rollproof-graduation/split` comes first. From the repository root, with `HOST` the repository root's path:

```
git clone --no-local --single-branch --branch main HOST /tmp/opencode/rollproof-graduation/split
```

Expected: `git clone` names the folder it clones into and counts its objects, and prints no warning. Then, each alone: `git -C /tmp/opencode/rollproof-graduation/split rev-parse --abbrev-ref HEAD` prints `main`; `git -C /tmp/opencode/rollproof-graduation/split log -1 --format='%H %s'` prints the merge of Task 6's pull request.

- [ ] **Step 3: Run the search over the clone before the split: the control**

Each alone; the two outputs go to their files by the shell's redirection:

```
bash /tmp/opencode/rollproof-graduation/publish-audit.sh HOST LOG history /tmp/opencode/rollproof-graduation/split > /tmp/opencode/rollproof-graduation/audit-before-split.txt
bash /tmp/opencode/rollproof-graduation/publish-audit.sh HOST LOG head /tmp/opencode/rollproof-graduation/split
```

Expected: the second exits 0 with `0 distinct lines in 0 files`: the hand edits are in `main`. The first exits 1, and its last line, read with `tail -1 /tmp/opencode/rollproof-graduation/audit-before-split.txt`, names at least one line for each needle, at least eight for the name and at least one for the row's text. It takes about a minute. On 2026-11-25, on a clone of `main` made before this plan, it read: 2674 lines in 150 files; name 23, row text 2, row IDs 2535, research 1492.

When the control finds nothing, or too little, the search is blind or reads a needle wrongly: stop; nothing below runs; report the script's output.

- [ ] **Step 4: Write the fixed list**

```
bash /tmp/opencode/rollproof-graduation/replace-text.sh HOST LOG > /tmp/opencode/rollproof-graduation/replace.txt
```

Expected: exit 0, and `wc -l /tmp/opencode/rollproof-graduation/replace.txt` prints 4. Each line holds `==>`. The file is not shown in any report and not committed: it holds the needles in plain text.

- [ ] **Step 5: Split the clone**

From `WORK/split`:

```
uvx --from 'git-filter-repo==2.47.0' git-filter-repo --path projects/rollproof/ --path .github/workflows/rollproof-action.yml --path .github/workflows/rollproof-e2e.yml --path .github/workflows/rollproof-mirror.yml --path .github/workflows/rollproof-proofs.yml --path .github/workflows/rollproof-unit.yml --path-rename projects/rollproof/: --replace-text /tmp/opencode/rollproof-graduation/replace.txt --replace-message /tmp/opencode/rollproof-graduation/replace.txt
```

Expected: it ends with "Completely finished after". When it begins a line with "Aborting", it has refused the clone as not fresh: stop and report its message. `--force` is never used.

- [ ] **Step 6: Check the clone**

Each alone. `W` stands for `/tmp/opencode/rollproof-graduation/split`.

1. `git -C W remote -v` prints nothing: the tool took `origin` out.
2. `git -C W rev-parse --abbrev-ref HEAD` prints `main`.
3. `git -C W ls-tree --name-only HEAD` prints exactly sixteen names: `.github`, `.gitignore`, `Dockerfile`, `LICENSE`, `README.md`, `action.sh`, `action.yml`, `cmd`, `docs`, `e2e`, `fixtures`, `go.mod`, `go.sum`, `hack`, `internal`, `proofs`.
4. `git -C W ls-files .github` prints the five workflow files and nothing else.
5. `git -C W ls-files projects` prints nothing.
6. `git -C W tag -l` prints nothing. A tag left is reported in Task 9's record; it is not pushed.
7. `git -C W rev-list --count HEAD` prints CARRIED: noted for Task 9.
8. `git -C W log -3 --format='%h %s'`: noted for the record.
9. From `W`: `go test ./...` passes. The project at the root is whole.

- [ ] **Step 7: Run the search over the clone after the split**

Each alone:

```
bash /tmp/opencode/rollproof-graduation/publish-audit.sh HOST LOG history W > /tmp/opencode/rollproof-graduation/audit-after-split-history.txt
bash /tmp/opencode/rollproof-graduation/publish-audit.sh HOST LOG head W > /tmp/opencode/rollproof-graduation/audit-after-split-head.txt
```

Expected: both exit 0, and each file's last line reads `0 distinct lines in 0 files`.

When either finds something, the split stops. Nothing is pushed. The lines name what the hand edits (Task 4) or the four rules (decision 7) missed: a fix is a commit on the graduation branch of the incubating repository, with its pull request and its word, and this task is then repeated from its Step 2.

- [ ] **Step 8: Keep what the next tasks need**

```
cp /tmp/opencode/rollproof-graduation/split/.git/filter-repo/commit-map /tmp/opencode/rollproof-graduation/commit-map
```

Expected: the file's first line is `old` and `new`, and `wc -l` on it is one more than the commits of the clone before the split. `rm /tmp/opencode/rollproof-graduation/replace.txt`: the fixed list has done its work, and Task 8 needs none of it.

- [ ] **Step 9: Report**

The two summary lines of the control, the two of Step 7, CARRIED, the three commits of Step 6's item 8, and whether a tag was left.

**Done when:** the clone passes every check of Step 6, the search over its history and over its HEAD finds nothing, and `WORK/commit-map` is there. Nothing has left the machine but the download of Step 1's word.
