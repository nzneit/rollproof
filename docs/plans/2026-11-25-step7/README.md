# rollproof: graduation (build-order step 7, first half) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Graduate `rollproof` out of the folder in which it incubates into a public repository of its own, with its history: the security review of section 14, point 13, with the maintainer's ruling on each finding, a fix round and a second reader on the fixes; the hand edits that take the incubating repository's name, its row and its research paths out of the files; the maintainer's five trademark searches; a split on a throwaway clone and its audit; a private repository, one pull request that moves the project in, the repository made public, three hosted runs with a record, and the incubating repository's last pull request for the project.

**Architecture:** Nothing is built that a user runs, except what a ruling of the security review asks for. The rest is order and proof. Every commit in the incubating repository passes a search for what may not be published before it is made; the same search runs over every version of every file and every commit message of a fresh clone, before and after `git-filter-repo` splits it; nothing is pushed until the search is clean; and each step that leaves the machine waits for the maintainer's word.

**Tech Stack:** As built. `git` 2.56.0; `gh` 2.103.3; `uv` 0.11.25, which runs `git-filter-repo` 2.47.0 from its cache; `bun` 1.3.14 for the incubating repository's citation check; Python 3.14 with PyYAML 6.0.3, which parses the workflow files; bash for the two scripts of the graduation folder. The reviewers are four AI agents on DeepSeek V4.1 Flash with thinking at its maximum, and the second reader of the fixes is on it too.

**Spec:** `projects/rollproof/docs/specs/2026-11-13-rollproof-design.md`, at `5eb4cd0` of the branch `rollproof-step7-design`. Section numbers in this plan are that document's. Section 13 ("Graduation": the order of the work and the list of what is done when) and section 14, point 34, hold every decision this plan rests on and override anything older. Where both are silent, this plan's decisions below stand.

## Global Constraints

Every task's requirements include these.

- **The publication rule** (point 34, verbatim): "Nothing published may name the incubating repository, quote its decision log or cite its research files. Generic mentions of the host repository and of its rule files by name may stay." This plan's files are carried by the split, so they follow the rule from their first commit, as does every file a task writes.
- **What may not be published, as the search of this plan defines it** (decision 4): four things. The incubating repository's name, in either spelling with a hyphen or an underscore, and ignoring case. The ID of any row of its log: a capital D, G or O, a hyphen and three digits. The first 120 characters of the Decision cell of the row of the incubation. Any path under the research folder of that repository. The name of the log's file, the names of its rule files and of its citation tool, the maintainer's account and `OWNER/rollproof` may stay.
- **No needle is written in this plan.** The two scripts of Task 1 read the needles from the incubating repository when they run. Where a command of this plan needs the research folder's path in a regular expression, the plan writes it with a bracket inside one word (`r[e]search`), which matches the same text and is not the path.
- **Placeholders.** `HOST` is the incubating repository's working copy. `LOG` is the path of its decision log from `HOST`'s root: the file that its rule file names under "Changing a rule". `WORK` is `/tmp/opencode/rollproof-graduation`, a folder outside the repository that the tasks make; nothing under it is committed anywhere. `OWNER` is the maintainer's personal account: the name that `docs/proofs/p14-name.md` writes before `/rollproof`, so that the new repository is `github.com/OWNER/rollproof`. `ROW` is the ID of the row of the incubation: the oldest row of `LOG` whose Decision cell cites `projects/rollproof/`. `AMENDROW` is the ID of the row that sets the form of an amendment, which the rule file cites under "Changing a rule". `NEWROW` is the next `D-` number that `LOG` leaves free. `DATE` is the day a task runs. `FIX-BASE` is the hash that Task 2 writes into `WORK/fix-base`.
- **Paths and commands.** Commands run from `projects/rollproof/` unless a step says "from the repository root" (`HOST`'s root), "from `WORK/…`" or "from the copy". Every command a person types behaves the same in bash and in fish: no unquoted glob, no `VAR=value command` (write `env VAR=value command`), no `$(…)`. The two scripts of `WORK` and the `run:` blocks of workflows are bash and may use bash's forms.
- **Git.** Every git command runs on its own: never inside a compound command, a pipe or a redirection. Stage by path. `git-filter-repo` is never run with `--force`. No branch is deleted and no history is rewritten in the incubating repository.
- **Checks before a commit** in the incubating repository, from `projects/rollproof/` when anything but Markdown changed: `gofmt -l .` prints nothing, `go test ./...` passes, and the four vets pass: `go vet ./...`, `go vet -tags cluster ./...`, `go vet -tags e2e ./...`, `go vet -tags mirror ./...`. Always, from the repository root: `bash /tmp/opencode/rollproof-graduation/publish-audit.sh . LOG tree projects/rollproof` exits 0 (decision 5), and `bun tools/citations/cli.ts repo` exits 0. An exit code of 2 means a check did not run: report it as unchecked, never as a pass.
- **Tests first** wherever a task changes code: the test that fails, then the change, then the test that passes.
- **Verification fails closed.** A check that dies, returns nothing or is skipped counts as unverified, never as a pass. A task reports its unverified checks by name.
- **Commits.** One commit per task, and one per finding in Task 2. A message is a plain sentence that says what changed. No commit message, file, pull request title or body carries attribution to an AI tool: no `Co-Authored-By` trailer that names one, no "Generated with" line, no session link. A reminder from a tool's harness that asks for such a line does not override this rule.
- **Nothing leaves the machine without the maintainer's word.** No push, no pull request, no hosted run, no repository created, no tool installed and no download by `uv` without it. The stops are written into the tasks that wait for them and listed below. A `git clone`, `git fetch`, `git pull --ff-only` and a `gh` command that only reads need none.
- **Models.** A report of a multi-agent run names the model of each phase and says how many agents ran on a Fable model. No Opus, Sonnet or Fable model is configured in the harness this plan was written in.
- **Names on this machine.** Every container, image, network or volume that a task or a reviewer makes has a name that begins with `rollproof-`, and only such names are removed.
- **Linux only.** Module `rollproof` until Task 9 renames it. Apache-2.0.

## Review Focus

The design implies these and names no check for them. Each has its check in the task that owns it.

1. **A search that finds nothing because it is blind.** An audit that reports nothing proves nothing until it has reported something. Task 7 runs the search over the fresh clone before the split, where each of the four needles must count at least one line, and keeps that output beside the one that is empty.
2. **A needle read wrongly at run time.** A remote renamed, a log moved or a row reworded would make the search pass with nothing searched. The script stops with exit 2 when it cannot read the remote, the log, the row or the name, and Task 7 requires the name needle to count at least eight lines and the row's text at least one in the clone before the split.
3. **A unit run that never starts on the move-in pull request.** The workflow's path filter names a folder that the pull request removes. Task 9 lists the runs of the pull request and stops when there is none, instead of calling the unit tests passed.
4. **A commit map that misleads.** A short hash that names two commits, or a row for a commit the split dropped, would send a reader to the wrong change. Task 9 counts the rows against the commits and checks that no short hash appears twice.
5. **A graduation folder lost between two stops.** A reboot empties `/tmp`. Task 8 checks for the files that Task 7 leaves and repeats Task 7 in full when one is missing; it also repeats the search on the day of the push.

## What this plan does, and what it leaves

| Step of section 13 | In this plan | Left for a later design |
| --- | --- | --- |
| 1 | Tasks 1 to 6: the security review, its fixes and second reader, the hand edits, the trademark searches, one branch and one pull request | Nothing |
| 2 | Task 7: the split and its audit | Nothing |
| 3 | Task 8: the private repository | Nothing |
| 4 | Task 9: the module path, the workflows' paths, one paragraph of the README, the commit map; the unit tests on the pull request | The digest, version and repository written into `action.yml`; the image's index for amd64 and arm64 |
| 5 | Task 10 | Nothing |
| 6 | Task 11: three hosted runs and their record | A second run of any workflow; the fixture series and its bound |
| 7 | Task 12: the amending row, the README's line, the directory and the five workflows removed | Whether the platform adopts `rollproof` |
| After | Nothing | The series, the first release, the automation of kind's updates; a `SECURITY.md`, a `CONTRIBUTING.md` and branch protection |

**Done when** (section 13, verbatim): "the new repository is public and holds the project at its root with its history; the audit found nothing and the commit map is there; the unit tests and the three hosted workflows are green there; the record of the security review is there, with every finding and the maintainer's ruling; and the incubating repository's log says that the project left."

## Decisions made in this plan

The design left these open or silent. Each is the smaller choice. The maintainer reviews them with the plan; the ones marked **public** change something a user sees or depends on.

1. **The graduation branch is the one the design is on**, `rollproof-step7-design`. The plan is committed there; every task of the incubating repository commits there; it is pushed once, in Task 6, and merged by the maintainer. An earlier push is the maintainer's to ask for. What lost: a branch of its own for the plan, which would need a second merge before the split.
2. **Reviewers work in copies.** Each of the four reviewers and the second reader gets a copy of `projects/rollproof/` under `WORK`, made with `cp -a`, which holds no `.git`: nothing a reviewer writes reaches the working copy, and two reviewers who run at the same time cannot break each other's build. What lost: the working copy itself, where a leftover test file could be committed; and a git worktree per reviewer, which registers each in the incubating repository and gives the reviewer a place to commit.
3. **A re-run gives one of three verdicts.** The session re-runs each try the reviewer reported and records: `holds` when both outputs show the rule broken, `differs` when they disagree, and `does not hold` when the re-run shows the rule keeping. Point 34 names the first and the second; the third is the case it left out, and it is recorded and not fixed unless the maintainer rules otherwise.
4. **What may not be published** is the four things of the Global Constraints. Row IDs count because the incubating repository's rule for a project says that a project's files cite no row of its log, and the first plan's first task file cites four. The research folder's own path counts because a quote of the citation tool's list of folders is a path under it. What lost: forbidding only dated paths under the folder, which would leave ten lines of that file citing the folder.
5. **The search is one script outside the repository**, `WORK/publish-audit.sh` (Task 1), and its needles are read from the incubating repository at each run. What lost: a program kept in the project, which would have to hold its own needles and so would fail its own search; needles written in this plan, which is published.
6. **The hand edits take out exactly the four things, and no more.** The first plan's first task file keeps what it says about the incubating repository's citation tool and rule files, with `ROW` in place of every row ID and `RESEARCH/` in place of the research folder's path, and a sentence that says so. What lost: cutting the task down to a note, which publishes less and rewrites more than point 34's hand edits ask.
7. **The fixed list of the split has four rules**, one per needle, with `REPOSITORY`, a sentence, `ROW` and `RESEARCH/` in place of what they match (Task 7). The same file goes to `--replace-text` and to `--replace-message`. What lost: leaving commit messages alone, which relies on no carried merge having cited a row, a check the audit makes anyway.
8. **The split keeps what git-filter-repo does by default**: hashes cited in commit messages are rewritten to the new ones, commits that become empty are dropped, and so are merge commits that become degenerate. What lost: `--preserve-commit-hashes`, which would leave a message citing a hash that no longer exists.
9. **The new repository**: `OWNER/rollproof`, created empty and private, default branch `main`, the workflow files keep their names, the module path becomes `github.com/OWNER/rollproof`, and the proofs module keeps `rollproof-proofs`, which nothing imports. **public**. What lost: renaming the workflows, which the records cite; and renaming the proofs module, which the design's one sentence on the rename does not cover.
10. **public: the unit workflow loses its path filter.** At the root of a repository that holds nothing else, every pull request touches the project.
11. **public: `hack/action/action_test.go` follows the workflow.** Its test of the script's arguments uses the `config` path that the hosted workflow passes; both become `hack/action/testdata/pass.yaml`.
12. **public: the README's paragraph on the workflows** loses "in the host repository" and the billed minutes, which a public repository does not pay.
13. **The commit map is a table made by one awk program** from the file that git-filter-repo writes, seven characters per hash. The documents keep the hashes they cite; the map translates them. What lost: rewriting every cited hash in the documents, which point 34 rejected by choosing a map; pushing replace refs, which GitHub's pages do not read.
14. **Two records, in the form of the proofs**: `docs/proofs/p34-split-audit.md` for the split and its audit, in the move-in pull request, and `docs/proofs/p34-graduation-runs.md` for the three hosted runs. What lost: one record written in two sittings, which breaks the form's rule that nothing is written from memory later.
15. **The incubating repository's citation check passes by three entries** in its list of paths it accepts without resolving (Task 12): the maintainer's choice of 2026-11-25. What lost: a file kept in the folder to make it resolve, which would still leave the two paths of the withdrawn sentence.
16. **`uv`'s download of git-filter-repo waits for the maintainer's word** with the split, because it puts the tool into `uv`'s cache. What lost: treating point 34's choice of the tool as the word to download it.
17. **The trademark task waits only for the maintainer.** It can be asked for as soon as Task 1 has dispatched its reviewers; nothing waits for it but Task 6.

## What writing this plan found in the spec

None blocks the plan; each has the smaller choice above or the ruling that the maintainer gave.

1. **Section 13, step 7, and the citation check.** Removing the directory alone makes the incubating repository's citation check report 15 errors: the folder `projects/` resolves only while it holds a tracked file, and the incubation row's last sentence cites two paths under it. Tried on a scratch clone of `main` on 2026-11-25. The maintainer ruled the same day: three entries in the tool's list (decision 15).
2. **Section 13, step 2, and the audit's own needles.** A search whose patterns are written in the plan would find the plan. The needles are read from the incubating repository at each run (decision 5).
3. **Section 13, step 4, "the workflows' paths".** One test under `hack/action/` holds the old `config` path too (decision 11). The unit workflow's path filter names a folder that the pull request removes; whether GitHub then reads the filter of the base or of the pull request was not read, so Task 9 checks that the run starts and stops when it does not (Review Focus 3).
4. **Section 14, point 34, "replacing the same strings".** The row's text can be matched as one line only by a regular expression with no anchor, because git-filter-repo compiles its expressions without the multi-line flag (read in its source, 2.47.0, `get_replace_text`).
5. **Section 13, step 2.** A clone made as the maintainer gave carries the incubating repository's one tag, which points at a commit before the project. Task 7 checks that no tag survives the split.

## File structure

Paths under `projects/rollproof/` unless they begin with `WORK/` or `.github/`, or are read "from the repository root".

| Path | Change | Task |
| --- | --- | --- |
| `WORK/publish-audit.sh`, `WORK/replace-text.sh` | New, outside the repository: the search, and the script that prints the split's fixed list | 1 |
| `WORK/review-1` to `WORK/review-4`, `WORK/findings.md` | New: the reviewers' copies and the list of tries, verdicts and rulings | 1 |
| The package that owns each rule fixed, its test file; `README.md`; the design's section 11 | Per ruling: a test and the fix, or a text | 2 |
| `WORK/second-reader/`; the same files again | The second reader's copy; a fault that holds, fixed | 3 |
| `docs/proofs/p13-security-review.md` (new), `docs/proofs/README.md`, the design (its status paragraph, section 11, point 13, its first line's dates) | The record of the review; its row; the sentences that said the review had not happened | 3 |
| `internal/probe/http.go`, `docs/specs/2026-11-13-rollproof-design.md`, `docs/plans/2026-11-16-first-plan/01-host-repository.md`, `03-verdict-and-probe-lines.md`, `10-fixtures-and-workflow.md`, `docs/proofs/p03-p04-p20-github-runner.md`, `docs/proofs/p19-proxy.md` | The hand edits, in one commit | 4 |
| `docs/proofs/p14-name.md`, `docs/proofs/README.md`, the design (the paragraph on the name, section 13 "Before", point 14) | The five searches and what they showed | 5 |
| `WORK/split/`, `WORK/replace.txt`, `WORK/audit-before-split.txt`, `WORK/audit-after-split-history.txt`, `WORK/audit-after-split-head.txt`, `WORK/commit-map` | New, outside the repository: the clone, the fixed list, the outputs of the search, the map | 7 |
| `.github/workflows/rollproof-*.yml` (five), `go.mod`, every `.go` file that imports `rollproof/internal/…`, `hack/action/action_test.go`, `README.md`, `docs/commit-map.md` (new), `docs/proofs/p34-split-audit.md` (new), `docs/proofs/README.md` | In the new repository: the move-in | 9 |
| `docs/proofs/p34-graduation-runs.md` (new), `docs/proofs/README.md`, `README.md` | In the new repository: the record of the runs | 11 |
| From the repository root: `LOG`, `tools/citations/unbuilt-paths.json`, `README.md`; `projects/rollproof/` and the five workflows removed | The incubating repository's last pull request | 12 |

## The tasks

One plan in eleven files. Read this file first, then the file of the task at hand. Tasks are numbered through all files and are done in order, except Task 5, which goes as far as its stop and then waits for the maintainer while Tasks 1 to 4 go on.

| File | Tasks | What |
| --- | --- | --- |
| `01-security-review.md` | 1 | Four reviewers, two at a time; the re-runs; the stop for the rulings |
| `02-security-fixes.md` | 2 to 3 | The fix round; the second reader, the runs on this machine and the record |
| `03-hand-edits.md` | 4 | The name, the row and the research paths out of the files as they are |
| `04-trademark-searches.md` | 5 | The maintainer's five searches, recorded |
| `05-graduation-pr.md` | 6 | The branch pushed, one pull request, merged |
| `06-split-and-audit.md` | 7 | The throwaway clone split, and audited before and after |
| `07-private-repository.md` | 8 | The repository created private; the clone pushed |
| `08-move-in.md` | 9 | In the new repository: the module path, the workflows, the README, the commit map; one pull request |
| `09-public-and-runs.md` | 10 to 11 | Public; three hosted runs and their record |
| `10-incubator-cleanup.md` | 12 | The amending row, the README's line, the directory and the workflows removed |

| Task | Title | Rests on | How it is verified | Waits for the maintainer |
| --- | --- | --- | --- | --- |
| 1 | The security review | nothing | every try run twice; the findings file complete | The ruling on each finding |
| 2 | The fix round | 1 | each fix: a test that fails, then passes; the checks before a commit | Nothing new |
| 3 | The second reader, the runs on this machine, the record | 2 | the reader's faults re-run; the suite and the mirror job once; the record read against the findings file | Nothing new |
| 4 | The hand edits | 1 | the search over the working tree: 35 lines in 7 files before, none after; the citation check | Nothing |
| 5 | The trademark searches | nothing | the record holds the maintainer's report, cell by cell | The report of the five searches; a mark for software stops the plan |
| 6 | The graduation pull request | 1 to 5 | the unit tests and the citation check on the pull request | The word for its push; the merge |
| 7 | The split and its audit | 6 | the search before the split counts each needle; after it finds nothing; the tree, the tags and the unit tests of the clone | The word for `uv`'s download |
| 8 | The private repository | 5, 7 | `gh repo view`: private, `main`; `git ls-remote` | The word to create and to push |
| 9 | The move-in pull request | 8 | the checks in the new repository; the unit tests on the pull request | The word for its push; the merge |
| 10 | Public | 9 | `gh repo view`: public | The word |
| 11 | Three hosted runs and their record | 10 | each run watched to its end; the record read against the saved logs | The word for the runs and for the push of their record |
| 12 | The incubating repository's last pull request | 11 | the citation check; the amendment's two markers | The row's text agreed; the word for its push; the merge |

### The stops, and what each asks

| Task | The ask, in these words or plainer |
| --- | --- |
| 1 | One question per finding: "Finding Pn-k attacks this rule: …. Its test is …. The output shows: …. Fix it, document it, or accept it with a reason?" |
| 5 | "What did each of the five searches show for the word `rollproof`: TMview, EUIPO's eSearch, WIPO's Global Brand Database, the USPTO's search and the United Kingdom's? For each: no mark, or the marks and their classes." |
| 6 | "May I push the branch `rollproof-step7-design` and open its pull request to `main`? It runs the unit tests once (about 3 to 5 billed minutes) and the citation check (about 1)." |
| 7 | "May I let `uv` download git-filter-repo 2.47.0 into its cache and run it on a throwaway clone? Nothing is pushed." |
| 8 | "The split is audited and clean. May I create the private repository `OWNER/rollproof` and push the clone's `main` to it? Nothing else is pushed." |
| 9 | "May I push the branch `move-in` to `OWNER/rollproof` and open its pull request to `main`? It runs the unit tests once (about 3 to 5 billed minutes while the repository is private)." Then: "Please merge it." |
| 10 | "May I make `OWNER/rollproof` public? Its history, the commit map and the record of the security review become readable by anyone." |
| 11 | "May I start `rollproof-action.yml` and `rollproof-mirror.yml` once each, and, when both pass, `rollproof-e2e.yml` once, on `main` of `OWNER/rollproof`, and then push the commit that records them? A public repository's runs are free." |
| 12 | "Do you agree the text of the row in Task 12?" Then: "May I push the branch `rollproof-last` and open its pull request to `main`? It runs the citation check (about 1 billed minute)." Then: "Please merge it." |

## Interfaces between tasks

The names that one task produces and a later one uses.

- **`bash /tmp/opencode/rollproof-graduation/publish-audit.sh HOST LOG MODE [ARG] [SCOPE]`** (Task 1): `MODE` is `tree` (ARG's working tree and HEAD's commit message), `head` (ARG's HEAD tree and commit message), `history` (every commit of ARG) or `replace` (in the file ARG, the name becomes `REPOSITORY`). Exit 0 when nothing was found, 1 when something was, 2 when the search could not run. Its last line is `publish-audit (MODE): N distinct lines in M files; by needle: name a, row text b, row IDs c, research d`.
- **`bash /tmp/opencode/rollproof-graduation/replace-text.sh HOST LOG`** (Task 1): prints the split's four rules, one per line, in the form git-filter-repo reads.
- **`WORK/findings.md`** (Task 1): one block per finding, with its ID `Pn-k` (part `n`, try `k`), the rule, the test's file and name, the command, the reviewer's output, the re-run's output, the verdict and, from Task 1's stop on, the ruling; then one line per try that held.
- **`WORK/fix-base`** (Task 2): one line, the hash of the last commit before the fix round.
- **`docs/proofs/p13-security-review.md`** (Task 3): the form of the proofs; four `Plan assumed:` pairs, one per part of point 13.
- **`WORK/split/`** (Task 7): the filtered clone; `WORK/commit-map`: a copy of its `.git/filter-repo/commit-map`, whose first line is a header and whose other lines are an old and a new hash.
- **`WORK/repo/`** (Task 9): a clone of `OWNER/rollproof`, where Tasks 9 to 11 work.
- **`docs/commit-map.md`** (Task 9): one paragraph, the counts, and a table `| There | Here |` with one row per carried commit.
- **The placeholders of Task 12**: `NEWROW`, `ROW`, `AMENDROW`, `DATE`, filled from `LOG` when the task runs.

## Alternatives considered

### Where the needles of the search live

Common case: every commit in the incubating repository, and every version of every file of the clone, is searched for the same four things, none of which may appear in the plan that is published.

**A. One script outside the repository, which reads the needles from the incubating repository at each run**

**B. The needles written in the plan** (expected to lose)

**C. A program kept in the project, with its needles beside them**

| | A. Derived | B. In the plan | C. Kept program |
| --- | --- | --- | --- |
| Ease of use for the common caller | One command, two arguments that never change | One command | One command |
| Interface size | Two scripts of about a hundred lines | None | A Go program and its test |
| Generality | Any rename of the remote or the row is followed | Breaks at the first rename | Same |
| Information hidden | The needles appear in no file that is published | The needles are published | The needles are published |
| Implementation simplicity | Bash, read once | Trivial | More code in a repository that needs none |

Third design: not needed.

Pick: A.

Losers: B, because the plan is carried by the split and would fail its own search; C, for the same reason, and because the program would have no use after graduation.

### How the commit map is made

Common case: git-filter-repo writes a map of every commit's old and new hash into the clone, and a reader of a document that cites an old hash needs the new one.

**A. An awk program over the map, run once, its output a table in `docs/commit-map.md`**

**B. A Go program under `hack/` that writes the document** (expected to lose)

| | A. awk, once | B. A program |
| --- | --- | --- |
| Ease of use for the common caller | Reads the document | Reads the document |
| Interface size | One command line | A program and a test in a public repository |
| Generality | The map is made once | Can be run again, for a map that is never made again |
| Information hidden | The map's format is read in one line | Same |
| Implementation simplicity | One line | More code to carry |

Third design: not needed.

Pick: A.

Losers: B, because nothing runs it a second time.

Design delta: the same software, published; and each thing that leaves the machine has a check behind it that was seen to fail first.
