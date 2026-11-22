# Task 1: the two workflows' names on `main`

Read section 13 ("During incubation", the paragraph on the workflows), section 14, point 32 (its last bullet) and section 16 (the row on GitHub Docs of 2026-11-22, and the list of what is not shown), and decision 1 of the README, before this task.

This task writes the first of the three ways of decision 1. The maintainer has not yet chosen among them. When the maintainer chooses the second or the third way, this task is not done, and Task 10 and Task 13 change as decision 1 says. No other task waits for this one: only Task 13's runs on GitHub depend on it.

### Task 1: a pull request to `main` that adds `rollproof-e2e.yml` and `rollproof-action.yml` as files that do nothing yet

**Files (on a branch of their own, from `main`, never on `rollproof-step6-design`):**
- Create: `.github/workflows/rollproof-e2e.yml`
- Create: `.github/workflows/rollproof-action.yml`

**Interfaces:**
- Consumes: nothing.
- Produces: on `main`, once the maintainer has merged the pull request, a file of each name, so that GitHub starts the workflow of that name by hand with `gh workflow run FILE --ref rollproof-step6-design`, and runs the branch's file (the case seen in `docs/proofs/p03-p04-p20-github-runner.md`, its Method).

- [ ] **Step 1: Make the branch in a worktree of its own**

Run each command alone, from the root of this worktree (`rollproof-step6-design`):

```
git fetch origin main
git worktree add ../rollproof-step6-stubs -b rollproof-step6-stubs origin/main
```

`git fetch` only reads from GitHub. The new worktree is the folder `rollproof-step6-stubs` beside this worktree's folder; every later step of this task runs from its root.

- [ ] **Step 2: Write the two files**

`.github/workflows/rollproof-e2e.yml`, exactly:

```yaml
name: rollproof e2e suite

# A placeholder. The default branch holds a file of this name so that the workflow can be started
# by hand on the branch of rollproof's build-order step 6, which writes its jobs. Its one job never
# runs.

on:
  workflow_dispatch:

permissions:
  contents: read

jobs:
  placeholder:
    if: false
    runs-on: ubuntu-24.04
    steps:
      - run: "true"
```

`.github/workflows/rollproof-action.yml`, the same with `name: rollproof action test` in its first line, and nothing else changed.

- [ ] **Step 3: Check them**

Run, from the root of the new worktree, each command alone:

```
grep -nE '[DGO]-[0-9]{3}' .github/workflows/rollproof-e2e.yml .github/workflows/rollproof-action.yml
python3 -c 'import sys, yaml; [yaml.safe_load(open(p)) for p in sys.argv[1:]]' .github/workflows/rollproof-e2e.yml .github/workflows/rollproof-action.yml
bun tools/citations/cli.ts repo
```

Expected: the search prints nothing and exits 1, which is the pass: the files hold nothing that looks like one of the host's row IDs, which the citation check does not read for them, since it reads tracked Markdown only. The parse prints nothing and exits 0; when `python3` or its `yaml` module is missing, the parse is reported as unchecked, never as a pass. The citation check exits 0, as the host asks for any change. For the search and the citation check, an exit code of 2 means the check did not run: report it as unchecked, never as a pass.

Then read both files back and compare them with Step 2, line by line: the same text, two spaces for each level of indentation, no tab. A file that differs is fixed before the commit. The parse shows the YAML's syntax and nothing more. Nothing on this machine runs a workflow or parses one as GitHub does; GitHub reads them only when the pull request is opened, which needs the maintainer's word (Step 6).

- [ ] **Step 4: Commit**

Each command alone, from the root of the new worktree:

```
git add .github/workflows/rollproof-e2e.yml .github/workflows/rollproof-action.yml
git commit -m "Add placeholders for rollproof's e2e and action workflows, so that they can be started by hand from the step 6 branch"
```

- [ ] **Step 5: Write the pull request's description, and check it**

Write this text into a file outside both worktrees, in the system's temporary folder, named `rollproof-step6-stubs-pr.md`:

> Adds `.github/workflows/rollproof-e2e.yml` and `.github/workflows/rollproof-action.yml` as placeholders whose one job never runs (`if: false`).
>
> GitHub's documentation says that a workflow is started by hand only when the default branch holds a file of its name. rollproof's build-order step 6 writes both workflows on the branch `rollproof-step6-design` and has to run them there before that branch is merged; with these files on `main`, `gh workflow run FILE --ref rollproof-step6-design` starts the branch's version, as it did for `rollproof-proofs.yml`.
>
> Nothing runs: the workflows start only by hand, and their job is skipped. When step 6 is merged, its own versions replace these.

Run, from the root of the new worktree, with the path of that file in place of FILE:

```
bun tools/citations/cli.ts doc FILE --path-base .
```

Expected: exit 0.

- [ ] **Step 6: Stop, and ask the maintainer**

Ask for the maintainer's word for these three, and do none of them before it:

1. `git push -u origin rollproof-step6-stubs` (alone, from the new worktree).
2. `gh pr create --base main --head rollproof-step6-stubs --title "Add placeholders for rollproof's e2e and action workflows" --body-file FILE`, with the checked file as FILE.
3. The merge, which the maintainer makes.

The pull request runs the host's `citations` workflow, and the merge runs it once more on `main`: about 1 billed minute each.

**State while it waits:** the commit exists on the local branch `rollproof-step6-stubs`; nothing is on GitHub. Tasks 2 to 12 go on.

- [ ] **Step 7: After the merge, take `main` into the step's branch**

Only once the maintainer has merged the pull request. Each command alone, from the root of this worktree (`rollproof-step6-design`):

```
git fetch origin main
git merge origin/main
```

When Task 10 has already written the real files, the merge stops with an add/add conflict on `.github/workflows/rollproof-e2e.yml` and `.github/workflows/rollproof-action.yml`. Keep the step's version of both, each command alone:

```
git checkout --ours .github/workflows/rollproof-e2e.yml .github/workflows/rollproof-action.yml
git add .github/workflows/rollproof-e2e.yml .github/workflows/rollproof-action.yml
git commit -m "Take main in, with the placeholders of rollproof's e2e and action workflows, keeping this branch's workflows"
```

Then remove the stubs' worktree (alone): `git worktree remove ../rollproof-step6-stubs`. The local branch `rollproof-step6-stubs` and its copy on GitHub stay until the maintainer says to delete them.

Run, from the repository root: `bun tools/citations/cli.ts repo`. Expected: exit 0.

**Done when:** `main` holds both files, and the step's branch has taken `main` in. Task 13 starts its runs only after this.
