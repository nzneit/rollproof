# Task 2: the fix round. Task 3: the second reader, the runs on this machine and the record

Read `WORK/findings.md`, section 11, section 14 point 13 and the two bullets of point 34 on the review, and `docs/proofs/README.md` ("The form of a record"), before these tasks.

### Task 2: each finding ruled "fix" gets a test that fails and then passes; each ruled "document" gets its text

**Files:**
- Create or Modify, for each finding ruled `fix`: the test file of the package that owns the rule, and the file that implements it
- Modify, for each finding ruled `document`: `README.md`, or the design's section 11
- Create, outside the repository: `WORK/fix-base`

**Interfaces:**
- Consumes: `WORK/findings.md` (Task 1), with a ruling on every finding.
- Produces: `WORK/fix-base`: one line, the hash of the last commit before this task's first. The test of each fix, by its name; Task 3's record cites it. Whether any commit of this task changed code that a run executes; Task 3's Step 4 reads it.

- [ ] **Step 1: Mark the base**

From the repository root: `git rev-parse HEAD` (alone). Write the hash as the one line of `WORK/fix-base`. `git status --short` (alone) prints nothing.

- [ ] **Step 2: For each finding ruled `fix`, in the order of its ID**

1. Write the failing test. It goes in the package that owns the rule, with the package's style of test names, and its name says what it pins. It takes the try's hostile input, and where the try printed it asserts: a password in none of its forms in anything the run wrote; text from outside the tool only inside code; no line but the tool's two that the runner could read as a command; nothing leaving the runner that section 11 does not name. The try's file is not copied as it is: it was a reviewer's probe, and this is the package's test.
2. Run: `go test ./PACKAGE/ -run TestName -v`. Expected: FAIL, at the assertion that pins the rule.
3. Make the smallest change that keeps the rule, in the package that owns it. It adds no configuration key, no reason code and no schema version (Task 1's Step 5 asked again when a fix needs one), and it changes no test name that exists.
4. Run: `go test ./PACKAGE/` and the command of step 2. Expected: PASS.
5. The checks before a commit (the README, Global Constraints).
6. From the repository root, staging by path:

   ```
   git add projects/rollproof/PATH-OF-THE-TEST projects/rollproof/PATH-OF-THE-CHANGE
   git commit -m "WHAT CHANGED, in the plainest words: finding Pn-k of the security review"
   ```

- [ ] **Step 3: For each finding ruled `document`, in the order of its ID**

Change the text the maintainer named so that it says what the finding showed, and no more. The checks before a commit that read Markdown: the publication search and the citation check. One commit:

```
git add projects/rollproof/README.md
git commit -m "Document WHAT THE FINDING SHOWED: finding Pn-k of the security review"
```

- [ ] **Step 4: For each finding ruled `accept`**

Nothing. Task 3's record holds the maintainer's reason.

- [ ] **Step 5: Report**

For each finding: its ID, its ruling and its commit's hash, or that it has none. Whether a commit of this task changed code that a run executes: anything but a comment, `README.md` and a file under `docs/`.

**Done when:** every finding ruled `fix` has its test and one commit, every one ruled `document` has one commit, and `WORK/fix-base` holds the base.

### Task 3: a second reader reads the whole change cold; the suite and the mirror job run on this machine; the record is written

**Files:**
- Create: `docs/proofs/p13-security-review.md`; outside the repository, `WORK/second-reader/`
- Modify: `docs/proofs/README.md` (a row for point 13 in "The list"; its "Still unproved"), the design (its first line's list of dates; the last sentence of its status paragraph; section 11, its second and third bullets; point 13, its last two sentences); the files of a fault that holds

**Interfaces:**
- Consumes: `WORK/findings.md`, `WORK/fix-base` (Tasks 1 and 2), the commits of Task 2.
- Produces: `docs/proofs/p13-security-review.md`, in the form of the proofs, with four `Plan assumed:` pairs, one per part of point 13; its row in `docs/proofs/README.md`.

- [ ] **Step 1: Ask whether anything was committed**

From the repository root: `git rev-parse HEAD` (alone). When it prints the hash in `WORK/fix-base`, Steps 2 to 4 fall away: there is no change to read, no code to run, and the record says so. Go to Step 5.

- [ ] **Step 2: Dispatch the second reader**

`git status --short` (alone) prints nothing: the working tree is HEAD's. `cp -a projects/rollproof /tmp/opencode/rollproof-graduation/second-reader`. One `subagent` call, `agent: "general"`, `model: "deepseek/deepseek-flash#max"`, the DeepSeek provider's, no `sessionID`, its prompt this brief with `HOST`, `FIX-BASE` and `HEAD` replaced by the repository root's path and the two hashes:

```text
You are the second reader of the fixes of a security review of rollproof. You read the whole
change cold: nobody tells you what the session was thinking. Change nothing in the repository and
commit nothing. Your folder, COPY, is /tmp/opencode/rollproof-graduation/second-reader, a copy of
the project's folder at HEAD; it holds no git repository. Install nothing.

Read the change: git -C HOST diff FIX-BASE HEAD. Then, in COPY, the files it touches, with their
tests, and docs/specs/2026-11-13-rollproof-design.md, section 11.

For each fix, say: does it keep the rule against the input of its test, and against a near one;
does its test pass without the fix; does the change do more than its finding asked; and, for a
text that documents a finding, does it say more or less than the finding showed. Prove each fault
with a throwaway test in COPY, named review_s_k_test.go (k from 1), that you run: with go test, or
for a test that needs the engine with `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go
test -tags cluster -p 1 -count=1` before the package, -run and the name, and -v. Report nothing
from memory: every statement comes from a line you read or from output you saw. Give every
container, image, network and volume you make a name that begins with rollproof-review-5-, and
remove each before you end.

Return a report: for each fault, the file and line, the test's path and name, the command that
runs it, its output verbatim trimmed to the lines that decide, and what you expected instead;
then what you read and found sound, in one line each; then what you did not read.
```

- [ ] **Step 3: Run each fault again, and fix the ones that hold**

As in Task 1's Step 4: run each test in the reader's copy and set a verdict. A fault that `holds` is fixed as in Task 2's Step 2, in one commit whose message ends "found by the second reader of the security review". When all its faults are settled, continue the same reader (its `sessionID`) with the hash of the new HEAD and one line per commit since; it reads again what changed. Stop when it reports no fault.

- [ ] **Step 4: When code that a run executes changed, run the suite and the mirror job once on this machine**

It runs when any commit since `FIX-BASE` changed anything but a comment, `README.md` and a file under `docs/`. Each command alone, from `projects/rollproof/`; after each, `nerdctl ps -a --format "{{.Names}}"` prints no name that begins with `rollproof-`:

1. `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags e2e -p 1 -count=1 ./e2e/ -v -timeout 360m`: PASS, every row under both Helm majors, about two and a half hours.
2. `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags mirror -p 1 -count=1 ./e2e/ -run TestMirror -v -timeout 60m`: PASS.

**Stop rule.** A subtest that fails is reported to the maintainer with its results folder, its console and its standard error, and is not run again and not changed without the maintainer's word.

- [ ] **Step 5: Write `docs/proofs/p13-security-review.md`**

In the form that `docs/proofs/README.md` gives, its title "Point 13: the security review of masking, the job summary, workflow commands and what leaves the runner". Every statement comes from `WORK/findings.md`, from a test file in a copy, or from a commit; nothing is written from memory.

- **Question:** point 13 in one or two sentences: do the four rules hold against hostile input, and what did the maintainer rule on each finding?
- **Machine and versions:** the system, the engine and whether it runs without root, Go, kind and the node image; the model of each phase and how many agents ran on it; that no agent on a Fable model ran.
- **Method:** the four cards of Task 1 in a line each; two reviewers at a time, each in a copy; the re-runs and the three verdicts; the rulings, one finding at a time; the fix round, a test first and one commit per finding; the second reader and its rounds; the runs of Step 4, with their times, or why there were none.
- **Result:** per part, the rule's claims as the design states them; each try with its ID, the code of its test as the copy holds it, its command, the reviewer's output, the re-run's output and its verdict; the ruling with the maintainer's reason; for a fix, its commit and the name of its test; for a documented finding, its commit. Then the second reader's faults and what became of each, and the runs of Step 4.
- **Decision:** what changed in the code, the README and the design because of the review; what the maintainer accepted, with each reason; what stays open: the two places of point 33, and any try whose two runs differ.

It ends with four pairs. `Holds:` is `yes` when, after the fixes and the documented changes, the rule as the design now states it held against every try of its part; `no` when a finding of that part was accepted with the rule left as it was; `not measured here` when a try of that part could not be run twice and nobody settled it.

```
Plan assumed: "A registry password, in its plain and its encoded forms, appears in nothing the tool writes but the step's outputs on GitHub and the client library's log on standard error."
Holds: yes

Plan assumed: "Text from outside the tool stands in the job summary only inside a fenced code block or a code span, where GitHub's Markdown reads neither emphasis, links, autolinks nor HTML."
Holds: yes

Plan assumed: "On GitHub Actions no line that the tool or the action's script prints is read as a workflow command, the two places that point 33 leaves open excepted."
Holds: yes

Plan assumed: "Section 11 names every path by which a secret or a request leaves the runner through the tool, and its five facts hold."
Holds: yes
```

- [ ] **Step 6: The row, and the design's sentences**

In `docs/proofs/README.md`, "The list": a row for point 13, filled from the record as its other rows are: the question in one line; the tries and the findings per part; the rulings; `p13-security-review, 4 assumptions`; the `Holds` that the four pairs give, by the rule that a row holds only when all of them do; and "none (graduation)" for the tasks that rest on it. In "Still unproved": what the record's Decision says stays open.

In the design:

1. Its first lines, the list of the days of amendment: `DATE` joins it.
2. The last sentence of the status paragraph, "The security review of point 13 has not happened when this is written; point 34 says its form.", becomes "The security review of point 13 was had on DATE, in the form that point 34 gives; its record is `docs/proofs/p13-security-review.md`."
3. Section 11, its second bullet: "This rule has not had a security review (section 14)." is taken out. When the record's Decision for masking names something accepted or documented, one sentence follows "Nothing else is masked.": "The security review of DATE (section 14, point 13) found COUNT thing or things that this rule does not cover; they are accepted or documented in `docs/proofs/p13-security-review.md`."
4. Section 11, its third bullet: "This has not had a security review either (section 14, point 13)." is taken out, and the same sentence stands in its place when the record's Decision for workflow commands names something.
5. Point 13, its last two sentences, "The review is had before graduation, in the form that point 34 gives, and its record is `docs/proofs/p13-security-review.md`. Until it exists, the two sentences of section 11 that say a rule has not had a security review stand.", become "The review was had on DATE, in the form that point 34 gives. Its record is `docs/proofs/p13-security-review.md`, with every finding and the maintainer's ruling."

- [ ] **Step 7: The checks, and the commit**

The publication search and the citation check, from the repository root: both exit 0. The record quotes tests and outputs; a line of it that holds a needle is a line the search names, and it is rewritten until none does. Then, from the repository root:

```
git add projects/rollproof/docs/proofs/p13-security-review.md projects/rollproof/docs/proofs/README.md projects/rollproof/docs/specs/2026-11-13-rollproof-design.md
git commit -m "Record the security review of masking, the job summary, workflow commands and what leaves the runner"
```

**Done when:** the record is committed with every finding, both outputs of each, the maintainer's ruling and the four pairs; the design no longer says that the review has not happened; and either the second reader reported no fault, or each fault that held is fixed, and the suite and the mirror job passed once on this machine when they had to run. A check that did not run is named as unverified in the report.
