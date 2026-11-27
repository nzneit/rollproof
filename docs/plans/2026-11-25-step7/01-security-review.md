# Task 1: the security review: four reviewers, two at a time, and the re-runs

Read section 11 whole, section 14 point 13 and point 34 (its bullets "The security review's form" and "Who reviews"), section 8 ("Secrets", "Workflow commands"), section 9 ("Registries: credentials, mirrors and certificate authorities", "Who can read the credentials"), section 10 ("What a run writes", the bullet "GitHub job summary"), and `docs/plans/2026-11-22-step6/README.md`, decisions 5 and 26, before this task.

This task changes nothing in the repository and commits nothing. It ends at a stop: the maintainer rules on each finding. Until that word, Task 2 does not start; Task 4 may.

### Task 1: four reviewers try to break the four rules of point 13; the session runs each try again

**Files:**
- Create, outside the repository: `WORK/publish-audit.sh`, `WORK/replace-text.sh`, `WORK/review-1` to `WORK/review-4`, `WORK/findings.md`
- Modify: nothing

**Interfaces:**
- Consumes: nothing from another task.
- Produces: `publish-audit.sh` and `replace-text.sh` (the README, "Interfaces between tasks"), which every later task uses. `WORK/findings.md`: one block per finding, in the form of Step 4, and one line per try that held.

- [ ] **Step 1: Write the two scripts of the graduation folder**

Make the folder: `mkdir -p /tmp/opencode/rollproof-graduation`. Write `WORK/publish-audit.sh` with this text:

```bash
#!/usr/bin/env bash
# publish-audit.sh: finds what may not be published with rollproof.
#
#   bash publish-audit.sh HOST LOG MODE [ARG] [SCOPE]
#
#   HOST   the incubating repository's working copy. Its remote and its log give the needles.
#   LOG    the decision log's path, from HOST's root.
#   MODE   tree      ARG's working tree, tracked files and untracked files alike, and HEAD's commit
#                    message: what a commit would hold. ARG is a working copy; left out, HOST.
#          head      ARG's HEAD: its tree and its commit message. ARG is a working copy; left out,
#                    HOST.
#          history   every commit of ARG: its tree and its commit message. ARG is a working copy.
#          replace   in the file ARG, the incubating repository's name becomes REPOSITORY.
#   SCOPE  the paths searched, as a path specification from ARG's root. Left out: everything. In
#          the incubating repository itself, pass the project's folder.
#
# Nothing is written under ARG or HOST, and in mode replace nothing outside the file ARG.
# Exit 0: nothing was found. Exit 1: something was; its lines are on standard output.
# Exit 2: the search could not run, so nothing was checked.
#
# Four needles, each read from HOST when the script runs and written nowhere else, because the
# plan that holds this script is published with the project: the repository's name, in either of
# its spellings with a hyphen or an underscore; the first 120 characters of the Decision cell of
# the row of the incubation; the ID of any row of the log; and the path of the research folder,
# which is written below with a bracket inside it for the same reason. Run it before the
# incubating repository's last pull request: the marker its row gains there comes before the
# text that the second needle holds.
set -uo pipefail

host=${1:?HOST}
log=${2:?LOG}
mode=${3:?MODE}
arg=${4:-$host}
scope=${5:-.}

fail() { printf 'publish-audit: %s\n' "$*" >&2; exit 2; }

[ -d "$host" ] || fail "no folder $host"
[ -f "$host/$log" ] || fail "no log $host/$log"
url=$(git -C "$host" remote get-url origin) || fail "cannot read the remote origin of $host"
name=$(basename "$url" .git)
[ -n "$name" ] || fail "the remote names no repository"
# The row of the incubation: the oldest row of the log that cites the project's folder.
row_line=$(grep -F '`projects/rollproof/`' "$host/$log" | tail -n 1)
[ -n "$row_line" ] || fail "no row of the log cites the project's folder"
row_text=$(printf '%s\n' "$row_line" | cut -d'|' -f4 | sed -E 's/^[ *]+//' | cut -c1-120)
[ -n "$row_text" ] || fail "the row's Decision cell is empty"

name_re=$(printf '%s' "$name" | sed -E 's/[.[\*^$()+?{}|]/\\&/g; s/[-_]/[-_ ]?/g')
squashed=$(printf '%s' "$name" | sed -E 's/[-_]//g')
[ -n "$squashed" ] || fail "the name is nothing but separators"
row_re='\b[DGO]-[0-9]{3}\b'
research_re='docs/r[e]search/'

if [ "$mode" = replace ]; then
  [ -f "$arg" ] || fail "no file $arg"
  sed -i -E "s/($name_re|$squashed)/REPOSITORY/g" "$arg"
  exit 0
fi
[ -d "$arg/.git" ] || fail "no repository $arg"

# greps REPO [git grep arguments]: the four needles over one tree or over the working copy.
greps() {
  local repo=$1
  shift
  git -C "$repo" grep -a -n -i -E -e "$name_re" -e "$squashed" -e "$row_re" -e "$research_re" "$@" -- "$scope" || true
  git -C "$repo" grep -a -n -F -e "$row_text" "$@" -- "$scope" || true
}
# text: the four needles over the lines of a message.
text() {
  grep -a -i -E -e "$name_re" -e "$squashed" -e "$row_re" -e "$research_re" || true
  grep -a -F -e "$row_text" || true
}
# messages REPO RANGE: the commits of RANGE whose message holds a needle, one line each.
messages() {
  git -C "$1" log "$2" -i -E --grep="$name_re" --grep="$squashed" --grep="$row_re" --grep="$research_re" --format='COMMIT %h %s' || true
  git -C "$1" log "$2" -F --grep="$row_text" --format='COMMIT %h %s' || true
}
# tree: git grep's lines without the commit and the line number, so that one text counts once.
tree() { sed -E 's/^(HEAD|[0-9a-f]{40})://' | sed -E 's/^([^:]+):[0-9]+:/\1:/'; }
# report MODE: prints each distinct line once, then a summary; fails when there is any.
report() {
  local all lines files
  all=$(sort -u)
  lines=$(printf '%s\n' "$all" | grep -c . || true)
  files=$(printf '%s\n' "$all" | sed -E 's/^COMMIT .*/COMMIT/' | cut -d: -f1 | sort -u | grep -c . || true)
  printf '%s\n' "$all" | grep . || true
  printf 'publish-audit (%s): %s distinct lines in %s files; by needle: name %s, row text %s, row IDs %s, research %s\n' \
    "$1" "$lines" "$files" \
    "$(printf '%s\n' "$all" | grep -c -i -E -e "$name_re" -e "$squashed" || true)" \
    "$(printf '%s\n' "$all" | grep -c -F -e "$row_text" || true)" \
    "$(printf '%s\n' "$all" | grep -c -E -e "$row_re" || true)" \
    "$(printf '%s\n' "$all" | grep -c -E -e "$research_re" || true)"
  [ "$lines" -eq 0 ]
}

case "$mode" in
  tree)
    { greps "$arg" --untracked | tree; git -C "$arg" log -1 --format='%h %s%n%b' | sed 's/^/COMMIT /' | text; } | report tree
    ;;
  head)
    { greps "$arg" HEAD | tree; git -C "$arg" log -1 --format='%h %s%n%b' | sed 's/^/COMMIT /' | text; } | report head
    ;;
  history)
    { for rev in $(git -C "$arg" rev-list --all); do greps "$arg" "$rev"; done | tree; messages "$arg" --all; } | report history
    ;;
  *) fail "no mode $mode" ;;
esac
```

Write `WORK/replace-text.sh` with this text:

```bash
#!/usr/bin/env bash
# replace-text.sh: prints the fixed list of the split, in the form git-filter-repo reads for
# --replace-text and --replace-message.
#
#   bash replace-text.sh HOST LOG
#
# HOST is the incubating repository's working copy and LOG the path of its decision log from
# HOST's root. The needles are read from them as publish-audit.sh reads them, and written nowhere
# else: the plan that holds this script is published with the project. git-filter-repo 2.47.0
# applies the rules in this order, each a regular expression: the name, the row of the
# incubation as one line, the ID of any row, the research folder's path.
# Exit 0 with four lines on standard output; exit 2 when a needle cannot be read.
set -uo pipefail

host=${1:?HOST}
log=${2:?LOG}

[ -d "$host" ] || exit 2
[ -f "$host/$log" ] || exit 2
url=$(git -C "$host" remote get-url origin) || exit 2
name=$(basename "$url" .git)
[ -n "$name" ] || exit 2
row_line=$(grep -F '`projects/rollproof/`' "$host/$log" | tail -n 1)
[ -n "$row_line" ] || exit 2
row=$(printf '%s\n' "$row_line" | cut -d'|' -f2 | tr -d ' ')
case "$row" in
  [DGO]-[0-9][0-9][0-9]) ;;
  *) exit 2 ;;
esac
name_re=$(printf '%s' "$name" | sed -E 's/[.[\*^$()+?{}|]/\\&/g; s/[-_]/[-_ ]?/g')
squashed=$(printf '%s' "$name" | sed -E 's/[-_]//g')
[ -n "$squashed" ] || exit 2

# 1. The name, in either spelling and in either case.
printf 'regex:(?i)%s|%s==>REPOSITORY\n' "$name_re" "$squashed"
# 2. The row of the incubation: from its ID to the end of its line. The next rule then turns the
#    ID in the sentence into ROW, as the hand edits did.
printf 'regex:\\| %s \\| [^\\n]*==>| %s | (the text of this row is not published) |\n' "$row" "$row"
# 3. The ID of any row of the log.
printf 'regex:\\b[DGO]-[0-9]{3}\\b==>ROW\n'
# 4. The research folder's path, bare or under a date.
printf 'regex:docs/r[e]search/==>RESEARCH/\n'
```

Each script was run on 2026-11-25 against the incubating repository and against a clone of its `main` branch: `publish-audit.sh` in its four modes, and the four rules of `replace-text.sh` in a Python program that applied them as git-filter-repo does. `replace-text.sh` itself was not run through git-filter-repo: that waits for the maintainer's word (Task 7).

- [ ] **Step 2: Show that the search sees something, then make the four copies**

From the repository root, with `LOG` in place of the log's path:

```
bash /tmp/opencode/rollproof-graduation/publish-audit.sh . LOG tree projects/rollproof
```

Expected: exit 1, and a last line that ends `name 8, row text 1, row IDs 11, research 18` and names 35 distinct lines in 7 files (Task 4's table lists them). On 2026-11-25 it did. Another count means a file changed since: Task 4's table grows by what the output names.

Then four commands:

```
cp -a projects/rollproof /tmp/opencode/rollproof-graduation/review-1
cp -a projects/rollproof /tmp/opencode/rollproof-graduation/review-2
cp -a projects/rollproof /tmp/opencode/rollproof-graduation/review-3
cp -a projects/rollproof /tmp/opencode/rollproof-graduation/review-4
```

Expected: each copy holds `go.mod` and no `.git`. `git status --short` (alone) prints nothing.

- [ ] **Step 3: Dispatch reviewers 1 and 2, then reviewers 3 and 4**

Two `subagent` calls in one message, each with `agent: "general"`, `model: "deepseek/deepseek-flash#max"`, no `sessionID`, a label of three words, and for its prompt the common brief below with `N` replaced by its number, `COPY` by `/tmp/opencode/rollproof-graduation/review-N`, and its part's card. The model is the DeepSeek provider's, as the first part of its ID says, and not the harness's free copy of it. Wait for both to report. Then the same for reviewers 3 and 4. A reviewer that stops before its report is dispatched again with the same brief; nothing of a stopped reviewer counts. A reviewer that stops twice is reported to the maintainer with what it left in its copy, and its part is recorded as not reviewed: the record's pair for that part then says `not measured here`, and nothing of that part is called reviewed.

**The common brief.**

```text
You are the security reviewer of part N of the four in section 14, point 13 of the design of
rollproof. You work only in the folder COPY, which holds a copy of the project and no git
repository. Change nothing outside it, commit nothing, install nothing, and make no request to
the network except a pull of an image that a test needs.

Read first, in COPY: docs/specs/2026-11-13-rollproof-design.md, section 11 whole; section 14,
point 13, and in point 34 the bullets "The security review's form" and "Who reviews"; and the
passages your card names. Then read the code your card names.

Your job is to break one rule, the rule your card gives. Try with throwaway tests: Go test files
that you add under COPY, each named review_pN_k_test.go (N is your part and k the number of the
try, from 1), each planting hostile input where text or a secret enters the tool, and each
printing what the tool wrote. Run each one. A test that needs no cluster runs with go test; one
that needs the engine runs with `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test
-tags cluster -p 1 -count=1` before the package, -run and the test's name, and -v. Give every
container, image, network and volume you make a name that begins with rollproof-review-N-, and
remove each before you end.

A try is a finding only with its test and its observed output. A try whose output shows the rule
keeping is not a finding: report it as a try that held. Report nothing from memory: every
statement about what the tool does comes from a line of code you read or from output you saw.

Leave every test file in place: the session runs each one again.

Return a report in three parts. (1) The rule's claims, one line each, as the design states them.
(2) For each try, in order: its number; the claim it attacks; the path of its test file under
COPY and the test's name; the exact command that runs it; its observed output, verbatim, trimmed
to the lines that decide; and your verdict on it: broken, held, or unclear. (3) What you did not
try, and why.
```

**Card 1, masking.**

```text
Your part is the first: masking. The rule is in section 8, the paragraph that begins
"Secrets.", and in section 11, its second bullet: a registry password arrives only through an
environment variable, and it is masked, in its plain and its encoded form, in everything the tool
writes but the step's outputs on GitHub and the client library's log on standard error; nothing
else is masked. Read also section 9, from "Registries: credentials, mirrors and certificate
authorities" to the end of "Who can read the credentials."; the README's sentence that lists the
forms masked; and the code: internal/mask/, internal/report/mask.go, evidence.go and record.go,
internal/run/evidence.go and record.go, internal/cluster/credentials.go,
internal/deploy/commands.go, internal/watch/podlogs.go and e2e/mirror_test.go.

Plant a password in every input, and in forms the masker may not cover: URL-encoded,
JSON-escaped, HTML-escaped, the base64 of user:password with padding and without, in another
case, or with the parts reversed; cut by the log's limit for one line; wrapped by the console's
columns; split across two fields; with quotes, backslashes and line ends; with bytes that are
not UTF-8; and longer than any buffer you find. Then search everything the run wrote: the
results folder with its evidence, the console, standard error, and the two files of GitHub, with
the three variables of section 8 set as e2e/mirror_test.go sets them.
```

**Card 2, the job summary.**

```text
Your part is the second: the job summary. The rule is in section 10, "What a run writes", the
bullet "GitHub job summary.", and in docs/plans/2026-11-22-step6/README.md, decision 5: text from
outside the tool stands in the summary only inside a fenced code block or a code span, where
GitHub's Markdown reads neither emphasis, links, autolinks nor HTML; the tool's own words stand
as they are. Read internal/report/summary.go, console.go, record.go and result.go, with their
tests and the golden files under internal/report/testdata/.

Plant, in every string of the record that the rule covers (workloads' names, chart sources,
values files, the probe's target, settings' values, reasons' texts and details, warnings, the
results folder, and the rest of the console's first line where it goes on): a run of backticks
alone on a line and inside a line; a pipe; a backslash before a pipe; line ends and other control
characters; the line and paragraph separators; bidirectional controls; HTML with script, details
and a comment; a javascript: address; an @mention; an issue number; a bare address; an emoji's
code; math delimiters; a heading; a list marker; and an indented line. For each try say what
GitHub would render. Where you cannot read it in GitHub's documentation for Markdown, say so.
```

**Card 3, workflow commands.**

```text
Your part is the third: workflow commands. The rule is in section 8, the paragraph that begins
"Workflow commands.", and in section 14, point 33: on GitHub Actions the tool's first line on each
stream stops the runner's workflow commands, and its last line on standard output resumes them
when the tool ended by itself; the action's script stops them with a token of its own and never
resumes. Read cmd/rollproof/workflowcommands.go and main.go, action.sh,
internal/report/github.go, their tests, hack/action/action_test.go, and docs/proofs/
p03-p04-p20-github-runner.md. The lines of the runner's source that point 33 names are not in
COPY: say for each whether you read it in a clone on this machine or not at all.

Plant what the tool or the script may print before its stop line or after its resume line: a bad
flag; an argument that is no flag; a rejected configuration; a results folder that cannot be
opened; a run ended by a signal; a panic that escapes the run; the script's own failures before
its line, uname's output among them; an input, an engine's message and a file's name inside a
failure's text; a token that a caller could set through env: or guess; and a line of the tool's
own output that equals a resume line. The two places that point 33 leaves open are no test's:
name them, and say whether you found a third.
```

**Card 4, the five facts and what leaves the runner.**

```text
Your part is the fourth: the five facts of section 11 and its list of what leaves the runner.
The rule is section 11, its first bullet, its bullet "What leaves the runner through the tool"
and its last four bullets: the change under test runs with the job's power; every pod in the
cluster can read the nodes' credentials; a registry entry's credentials reach its mirror and the
token service that a challenge names; a chart's own pull secrets for a host with a mirror reach
the mirror; the value of a probe header is not masked; and the list names everything that leaves.
Read section 9, "Registries: credentials, mirrors and certificate authorities" and "Who can read
the credentials."; section 7, "Where commands run." and "The cluster Helm acts on."; and the code:
internal/cluster/credentials.go, hosts.go, registry.go and kindconfig.go, internal/deploy/,
internal/probe/http.go, internal/config/, action.sh, e2e/mirror_test.go, and the programs
proofs/p02-mirror-credentials/main.go and proofs/p02b-mirror-forms/main.go.

Try to show either that a pod, a command or a pull reaches a secret or a place that section 11
does not name, or that the list leaves something out: what a setup command's environment holds;
whether a plain pod reads the credentials file, and what the file holds; a mirror whose token
challenge names another host, and one that answers with a redirect; where a chart's pull secret
goes; the engine's own pulls of the node image and the registry image; a probe header in the
evidence; and the variables of Helm and of the kubeconfig in the environment of a command.
```

- [ ] **Step 4: Run every try again, and write `WORK/findings.md`**

For each try a reviewer reported, in the order of its ID `Pn-k`, from its copy's root: run the command the report gives. Then write one block per finding in this form; a try whose verdict was `held` gets one line at the end of the file instead.

```text
## Pn-k: one line that says what the try plants
- Rule: the claim of the design, quoted
- Test: WORK/review-N/PATH/review_pn_k_test.go, TestName
- Command: the command, from the copy's root
- Reviewer saw: the lines of output that decide, verbatim
- Re-run saw: the same lines from this run, verbatim
- Verdict: holds | differs | does not hold
- Ruling: (filled in at Step 5)
```

The verdict: `holds` when both outputs show the rule broken; `does not hold` when the re-run shows the rule keeping; `differs` when they disagree, and both are kept. A try whose test does not run again is `differs`, with the re-run's error for its output. Number the findings by part and order of report: `P1-1`, `P1-2`, `P2-1`.

Then `nerdctl ps -a --format "{{.Names}}"` (alone) prints no name that begins `rollproof-review-`. A container a reviewer left is removed by that name, with `nerdctl rm -f NAME`, and an image it left by its name, with `nerdctl rmi NAME`; nothing else is removed.

- [ ] **Step 5: Stop for the maintainer's ruling on each finding**

Ask one question per finding, in these words or plainer: "Finding Pn-k attacks this rule: …. Its test is …. The output shows: …. Fix it, document it, or accept it with a reason?" Write the answer and the maintainer's reason into the block's `Ruling` line. A finding whose verdict is `differs` or `does not hold` is asked for too: the maintainer may still rule on it.

A ruling of `fix` that would need a new configuration key, a new reason code or a new schema version is asked for again with that cost named before Task 2 starts.

**State while it waits:** every try run twice; `WORK/findings.md` complete but for its rulings; nothing committed. Task 2 waits. Task 4 and Task 5 may go on.

- [ ] **Step 6: Report the run**

Report: the phases and the model of each. Four reviewer agents on DeepSeek V4.1 Flash with thinking at its maximum, two at a time, one per part; the re-runs by the session that runs this plan, on that session's model, named; no agent on a Fable model. For each part: the tries, the findings by verdict, and what the reviewer did not try. Any reviewer that was dispatched again, and why.

**Done when:** every try a reviewer reported has run twice or is recorded as `differs`, `WORK/findings.md` holds every block with its verdict, and the maintainer has ruled on each finding.
