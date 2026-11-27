# Point 34: the split and its audit

## Question

Does the split keep the project and the five workflow files and nothing else, move the project to
the root and drop the commits that become empty? And after the hand edits and the four
replacements, does any version of any file, or any commit message, still hold what may not be
published (section 14, point 34)?

This proof has no cluster. It is a split of a throwaway clone and a search over it, made before
anything was pushed.

## Machine and versions

- The system: Linux 7.3.3-1-cachyos, x86_64, CachyOS.
- `git` 2.56.0.
- `git-filter-repo` 2.47.0, run through `uv` 0.11.25 as `uvx --from 'git-filter-repo==2.47.0'`,
  on Python 3.12.13, read with `uvx --from 'git-filter-repo==2.47.0' python --version`.
- The commit of `main` that was cloned: `e0c67be20bff91a9062cf48a238b5487528ac5fe`, "Merge pull
  request #17 from nzneit/rollproof-step7-design".
- The search: `publish-audit.sh`, a script outside both repositories. It reads its four needles
  from the repository the project left each time it runs, and writes them nowhere.
- Run on 2026-11-27, between 03:37 and 03:41 by the machine's clock (UTC-5), by the session on its
  default model (GLM-5.4), at the maintainer's word.

## Method

`HOST` is the working copy of the repository the project left, and `LOG` the path of its decision
log; neither path is published. `W` is the clone, `/tmp/opencode/rollproof-graduation/split`. Each
command ran on its own.

**The clone.** `git clone --no-local --single-branch --branch main HOST W`. Its branch was `main`
and its head the commit above. It held 393 commits and one tag, `seed-v2`, in one pack of 6755
objects.

**The control: the search before the split.** `bash publish-audit.sh HOST LOG history W` searches
every commit's tree and commit message; its output went to a file outside both repositories. `bash
publish-audit.sh HOST LOG head W projects/rollproof` searches the head's tree in the project's
folder: the scope the script's usage asks for inside the repository the project left.

**The fixed list.** `bash replace-text.sh HOST LOG` printed four rules, each a regular expression,
which the tool applies in this order: the name, the row of the incubation as one line, the ID of any
row, and the research folder's path. With the four things they match named and not written, they
are:

```
regex:(?i)the name==>REPOSITORY
regex:\| the row \| [^\n]*==>| the row | (the text of this row is not published) |
regex:a row's ID==>ROW
regex:the research folder's path==>RESEARCH/
```

The rule for the ID of any row, which follows the row's, then turns the row's ID in that sentence
into `ROW`, as the hand edits had. The same list went to `--replace-text` and to
`--replace-message`. Its file was removed after the split.

**The split,** from `W`, with no `--force`:

```
uvx --from 'git-filter-repo==2.47.0' git-filter-repo --path projects/rollproof/ --path .github/workflows/rollproof-action.yml --path .github/workflows/rollproof-e2e.yml --path .github/workflows/rollproof-mirror.yml --path .github/workflows/rollproof-proofs.yml --path .github/workflows/rollproof-unit.yml --path-rename projects/rollproof/: --replace-text /tmp/opencode/rollproof-graduation/replace.txt --replace-message /tmp/opencode/rollproof-graduation/replace.txt
```

It parsed the 393 commits and ended "Completely finished after 0.87 seconds." It wrote "No
filtering problems encountered." into `suboptimal-issues` under `.git/filter-repo/`.

**The search after it.** The same two commands in mode `history` and in mode `head`, the second over
the whole tree of the clone's head. Both outputs went to files outside both repositories.

**The checks of the clone,** each with what it printed:

- `git remote -v`: nothing. The tool had taken `origin` out, as its notice said.
- `git rev-parse --abbrev-ref HEAD`: `main`.
- `git ls-tree --name-only HEAD`: sixteen names, `.github`, `.gitignore`, `Dockerfile`, `LICENSE`,
  `README.md`, `action.sh`, `action.yml`, `cmd`, `docs`, `e2e`, `fixtures`, `go.mod`, `go.sum`,
  `hack`, `internal`, `proofs`.
- `git ls-files .github`: the five files `rollproof-action.yml`, `rollproof-e2e.yml`,
  `rollproof-mirror.yml`, `rollproof-proofs.yml` and `rollproof-unit.yml`, each under
  `.github/workflows/`.
- `git ls-files projects`: nothing.
- `git tag -l`: nothing.
- `git rev-list --count HEAD`: 328.
- `git log -3 --format='%h %s'`: `6c3c76e`, "Merge pull request #17 from
  nzneit/rollproof-step7-design"; `149635b`, "Add 2026-11-27 to the design's list of the days of
  amendment"; `88454fc`, "Make section 10 leave the exceptions of the masking rule to section 8,
  and record the maintainer's ruling on it".
- `go test ./...` at the root: `ok` in 18 packages.
- `git count-objects -v`: one pack of 2841 objects and no loose object.

**The commit messages.** Each carried commit's message was read before the split, in the repository
the project left, and after it, in the clone, and the two were compared.

## Result

- **Before the split,** the control over the history printed `publish-audit (history): 2990
  distinct lines in 198 files; by needle: name 23, row text 2, row IDs 2831, research 1807`: the
  search was not blind. The head's tree, in the project's folder, printed `publish-audit (head): 0
  distinct lines in 0 files; by needle: name 0, row text 0, row IDs 0, research 0`: the hand edits
  were in.
- **The commits.** Of the 393 cloned, 328 were carried and 65 dropped. Each dropped commit changed
  nothing the split kept, or was a merge that lost a parent. `main` held two merge commits more than
  the graduation's own 26 commits: the merge of `main` into the graduation's branch was dropped, and
  the graduation pull request's merge was carried, as `6c3c76e`.
- **The tags.** None was left: `seed-v2` pointed at a commit before the project.
- **The root** held the sixteen names above, and `.github/workflows/` the five files and nothing
  else.
- **The search after the split** printed `publish-audit (history): 0 distinct lines in 0 files; by
  needle: name 0, row text 0, row IDs 0, research 0` over every commit, and `publish-audit (head):
  0 distinct lines in 0 files; by needle: name 0, row text 0, row IDs 0, research 0` over the head.
  On the day of the push it ran once more over the history and printed the same line.
- **The unit tests** passed at the root: `ok` in 18 packages.
- **The commit messages.** Five carried commits cited a hash before the split. The tool rewrote the
  five, each to the new hash, abbreviated as the old one was; each new message is its old one with
  the hashes mapped, and no other message changed.
- **The commit map** lists all 393 commits: 328 with their new hash and 65 with none.

## Decision

- **The audit found nothing,** so the clone was pushed to the private repository `nzneit/rollproof`
  on 2026-11-27: one branch, `main` at `6c3c76e525c908e79d7cbaa1da6332a18f921dd1`, and no tag.
- **What stays open:** nothing. No tag was left.

Plan assumed: "git-filter-repo 2.47.0, run on a fresh clone, keeps the project and the five workflow files and nothing else, moves the project to the root, and drops the commits that become empty."
Holds: yes

Plan assumed: "After the hand edits and the four replacements, no version of any file and no commit message of the clone holds the incubating repository's name, the ID of a row of its log, the text of the row of the incubation or a path under its research folder."
Holds: yes

Plan assumed: "The tree at the root is the project whole: its unit tests pass there."
Holds: yes

Plan assumed: "The commit map has one row for each commit the split carried, and each of its short hashes names one commit."
Holds: yes
