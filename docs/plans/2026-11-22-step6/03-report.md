# Tasks 4 and 5: GitHub's files, and the job summary

Read section 8 ("The action", the outputs and their two moments, "Secrets"), section 10 ("What a run writes": the console's list, "GitHub job summary"), section 12 (the bullets "Golden files" and "What the tool writes for GitHub"), section 14, point 4, and decisions 2 to 5 of the README, before these tasks.

### Task 4: GitHub's files

**Files:**
- Create: `internal/report/github.go`, `internal/report/github_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `type GitHub struct { outputs, summary string }`
  GitHub is where a run writes for GitHub Actions: the file of the step's outputs
  and the file of its job summary, as GitHubFrom found them. A zero GitHub writes
  nothing.
- Produces: `func GitHubFrom(getenv func(string) string) GitHub`
  GitHubFrom reads the environment through getenv. The tool detects GitHub Actions
  when GITHUB_ACTIONS is "true" (section 8); the GitHub it returns then writes the
  outputs to the file that GITHUB_OUTPUT names and the summary to the file that
  GITHUB_STEP_SUMMARY names, each only when its variable is not empty. Otherwise
  it writes nothing.
- Produces: `type Output struct { Name, Value string }`
  Output is one output of the step: its name and its value.
- Produces: `func (g GitHub) WriteOutputs(outs ...Output) error`
  WriteOutputs appends outs to the step's outputs file in one write, each in the
  delimiter form: NAME<<DELIMITER, the value, the delimiter, each on a line of its
  own, with a delimiter that the value does not hold, so that no value can be
  misread (section 14, point 4). It does nothing when no outputs file is named.
  The file must exist: it is opened for appending and never made.
- Produces: `func (g GitHub) AppendSummary(text string) error`
  AppendSummary appends text to the step's summary file in one write. It does
  nothing when no summary file is named. The file must exist: it is opened for
  appending and never made.

- [ ] **Step 1: Write the failing tests**

In `github_test.go` (package `report`):

```go
func TestGitHubFrom(t *testing.T) {
	// Each row makes two empty files, out and sum, in t.TempDir(); getenv reads the row's map, in
	// which "OUT" and "SUM" stand for the two paths; g := GitHubFrom(getenv);
	// g.WriteOutputs(Output{"verdict", "pass"}) and g.AppendSummary("### s\n") each return nil.
	//   {}                                                            out "", sum ""
	//   {GITHUB_ACTIONS: "false", GITHUB_OUTPUT: OUT, GITHUB_STEP_SUMMARY: SUM}   out "", sum ""
	//   {GITHUB_ACTIONS: "TRUE",  GITHUB_OUTPUT: OUT, GITHUB_STEP_SUMMARY: SUM}   out "", sum ""
	//   {GITHUB_OUTPUT: OUT, GITHUB_STEP_SUMMARY: SUM}                            out "", sum ""
	//   {GITHUB_ACTIONS: "true", GITHUB_OUTPUT: OUT}                  out holds verdict, sum ""
	//   {GITHUB_ACTIONS: "true", GITHUB_STEP_SUMMARY: SUM}            out "", sum "### s\n"
	//   {GITHUB_ACTIONS: "true", GITHUB_OUTPUT: OUT, GITHUB_STEP_SUMMARY: SUM}    both written
	// "out holds verdict" means: out is "verdict<<D1\npass\nD1\n" with newDelimiter replaced as in
	// TestOutputsInTheDelimiterForm.
}

func TestOutputsInTheDelimiterForm(t *testing.T) {
	// newDelimiter replaced, for the test, by one that gives "D1", "D2", "D3" in turn.
	// GitHub{outputs: path} of an empty file; WriteOutputs(Output{"out", "/tmp/r"},
	// Output{"verdict", "not-verified"}): the file is exactly
	// "out<<D1\n/tmp/r\nD1\nverdict<<D2\nnot-verified\nD2\n".
	// WriteOutputs(Output{"verdict", "pass"}) then appends "verdict<<D3\npass\nD3\n".
}

func TestADelimiterThatTheValueHoldsIsDrawnAgain(t *testing.T) {
	// newDelimiter gives "EOF", then "EOF2". WriteOutputs(Output{"x", "a\nEOF\nb"}): the file is
	// exactly "x<<EOF2\na\nEOF\nb\nEOF2\n".
}

func TestTheRealDelimiter(t *testing.T) {
	// newDelimiter() matches ^ROLLPROOF_EOF_[A-Z2-7]{26}$, and two calls differ.
}

func TestGitHubsFilesAreNeverMade(t *testing.T) {
	// GitHub{outputs: dir/missing-out, summary: dir/missing-sum}: WriteOutputs(Output{"verdict",
	// "pass"}) and AppendSummary("s\n") each return an error for which errors.Is(err,
	// fs.ErrNotExist) holds, and neither path exists afterwards.
}

func TestAppendSummaryAppends(t *testing.T) {
	// A file that holds "earlier step\n": AppendSummary("### rollproof: PASS\n") leaves
	// "earlier step\n### rollproof: PASS\n".
}

func TestAZeroGitHubWritesNothing(t *testing.T) {
	// GitHub{}.WriteOutputs(Output{"verdict", "pass"}) and GitHub{}.AppendSummary("x") return nil.
}
```

Each test that replaces `newDelimiter` (`TestGitHubFrom`, `TestOutputsInTheDelimiterForm`, `TestADelimiterThatTheValueHoldsIsDrawnAgain`) restores the real one with `t.Cleanup`, so that `TestTheRealDelimiter` meets the real one in any order.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/report/ -run 'TestGitHubFrom|TestOutputsInTheDelimiterForm|TestADelimiterThatTheValueHoldsIsDrawnAgain|TestTheRealDelimiter|TestGitHubsFilesAreNeverMade|TestAppendSummaryAppends|TestAZeroGitHubWritesNothing' -v`
Expected: FAIL to compile: `GitHub` is not defined.

- [ ] **Step 3: Implement `github.go`, each named one with its interface comment from the Interfaces block**

```go
// newDelimiter gives a delimiter for one output: ROLLPROOF_EOF_ and 26 random characters. A test
// replaces it.
var newDelimiter = func() string { return "ROLLPROOF_EOF_" + rand.Text() }
```

`rand` is `crypto/rand`, as `evidence.go` uses it. `WriteOutputs` draws a delimiter for each output with `newDelimiter`, again while `strings.Contains(value, delimiter)`, builds the whole text, and appends it with one `Write` to the file opened by `os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)`; the error of the open, the write or the close is returned as it is (an `*fs.PathError` names the path). `AppendSummary` opens the same way and writes `text` with one `Write`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: the command of Step 2, then `gofmt -l .`, `go test ./...` and the four vets.
Expected: PASS, no output from `gofmt` and the vets.

- [ ] **Step 5: Commit**

```
git add internal/report/github.go internal/report/github_test.go
git commit -m "Write the step's outputs in the delimiter form and append to its job summary, only when the tool detects GitHub Actions"
```

### Task 5: the job summary

**Files:**
- Create: `internal/report/summary.go`, `internal/report/summary_test.go`, `internal/report/testdata/summary-pass.golden.md`, `summary-fail.golden.md`, `summary-not-verified.golden.md`, `summary-before-first-observation.golden.md`, `summary-in-install-window.golden.md`, `summary-install-decided.golden.md`, `summary-custom-no-ms-or-lag.golden.md`, `summary-stall-mirror.golden.md` (all under `internal/report/testdata/`)
- Modify: `internal/report/console.go` (the console's blocks as data, for both renderings)

**Interfaces:**
- Consumes: nothing of this plan.
- Produces: `func Summary(r Run) string`
  Summary returns the job summary of r: the console's blocks, in the console's
  order, for every verdict and every ending (section 10). The first line is a
  heading; the table of windows and the table of transitions are Markdown tables;
  the lists of a pass are Markdown lists; every other block of lines is a fenced
  code block, line for line as the console prints it. Text from outside the tool,
  every string that Mask masks, stands only inside a code block (fenced) or a code
  span (codeSpan), so that it cannot change how the page reads. r is cleaned as
  Console cleans it; Summary reads nothing else.
- Produces: `func SummaryLine(line string) string`
  SummaryLine returns line as one more block of a summary: a blank line and a
  fenced code block of the line, cleaned as Console cleans text, so that a warning
  that arises after the summary was written is appended to it as a line
  (section 10).

The console's blocks become data that both renderings read. In `console.go`, with these comments:

```go
// verdictWord is the verdict as the first line writes it: PASS, FAIL or NOT VERIFIED.
func verdictWord(r Run) string

// verdictTail is what the first line holds after "rollproof: WORD": the name with its driver and
// the Kubernetes version with its provider, each when the record has it, with the spaces that lead
// it; "" when it has none of them.
func verdictTail(r Run) string

// windowRows is the first table's header and one row of cells for each window, as the console
// prints them before it aligns them.
func windowRows(r Run) (header []string, rows [][]string)

// crossedLines is a line for each window whose failures crossed its start, as the console prints
// it under the first table.
func crossedLines(r Run) []string

// transitionRows is the second table's header and one row of cells for each workload of each
// transition, the transition's name in the first row of its workloads only; rows is nil when no
// transition has a workload.
func transitionRows(r Run) (header []string, rows [][]string)

// exercisedItems is what the lists of a pass hold: for each transition that ran, its name and what
// it exercised, in order; and each line of what was not exercised, the lines of a restart-only run
// or of a deploy with no rollback first.
func exercisedItems(r Run) (exercised [][2]string, notExercised []string)
```

`verdictLine`, `windowsTable`, `transitionsTable` and `exercisedLists` keep their names and give the text of today from these. `setUp` and `last` stay as they are. No golden file of the console changes; the three of section 10 are compared by `sameAsFile` and are never rewritten.

In `summary.go`, besides `Summary` and `SummaryLine`:

```go
// fenced writes lines as a fenced code block: a fence of backticks one longer than the longest run
// of backticks in any line, three at the least, so that no line can close it, then the lines, then
// the fence, each ending with a line end. In a fenced code block GitHub reads no Markdown and no
// HTML.
func fenced(lines []string) string

// codeSpan writes s as a code span: delimited by a run of backticks one longer than the longest run
// in s, with one space inside each delimiter when s begins or ends with a backtick or a space,
// which the renderer takes off again. In a code span GitHub reads no Markdown, no link and no HTML.
// An empty s gives "".
func codeSpan(s string) string

// markdownTable writes a Markdown table: the header, a row of alignments, right-aligned for the
// columns that right marks, and the rows, each line "| CELL | CELL |". A cell of a column that
// outside marks is text from outside the tool and goes into a code span. Every | of a cell is
// written \|, inside a code span too, as GitHub's tables ask. An empty cell stays empty.
func markdownTable(header []string, right []bool, rows [][]string, outside []bool) string
```

`Summary`'s blocks, each ending with a line end and joined by one more line end, so that one blank line separates them:

1. `"### rollproof: " + verdictWord(r)`, and when `strings.TrimSpace(verdictTail(r))` is not empty, a space and its `codeSpan`.
2. `fenced(setUp(r))`, when `setUp(r)` has lines.
3. When `windowStarted(r)`: `markdownTable` of `windowRows(r)`, right-aligned for the six columns from "length" to "behind", no outside column; then `fenced(crossedLines(r))` when it has lines.
4. When `transitionRows(r)` gives rows: `markdownTable` of them, nothing right-aligned, the column "workload" outside.
5. When `r.Verdict == "pass"`: `"exercised\n\n"` and a line `"- NAME: TEXT"` for each exercised item; then, as a block of its own, `"not exercised\n\n"` and a line `"- LINE"` for each line not exercised.
6. `fenced(last(r))`.

- [ ] **Step 1: Write the failing tests**

In `summary_test.go`, reusing `passRun`, `failRun`, `notVerifiedRun`, `beforeFirstRun`, `inInstallRun`, `installDecidedRun`, `customNoMsOrLagRun`, `stallMirrorRun`, `customRun`, `golden` and `t0`:

```go
func TestSummaryGolden(t *testing.T) {
	// golden(t, "testdata/summary-NAME.golden.md", []byte(Summary(RUN))) for the eight pairs:
	//   pass passRun(), fail failRun(), not-verified notVerifiedRun(),
	//   before-first-observation beforeFirstRun(), in-install-window inInstallRun(),
	//   install-decided installDecidedRun(), custom-no-ms-or-lag customNoMsOrLagRun(),
	//   stall-mirror stallMirrorRun().
}

func TestSummaryKeepsTextFromOutsideTheToolInCode(t *testing.T) {
	const planted = "MARK<b>x</b>|*y*_z_[l](http://e.example) www.e.example ```\r\n`` end"
	// fence is a text that, on a reason's detail line, would be a line that closes a fence of
	// three backticks, as GitHub reads one: up to three spaces, then a run of backticks.
	const fence = "````"
	// For each of passRun, failRun, stallMirrorRun, customRun and beforeFirstRun, and for each of
	// planted and fence as the text s (a subtest each): md := Summary(plantFreeText(RUN, s)).
	//   outsideCode(md) returns no error, and its text holds no "MARK" and no backtick;
	//   for planted, md holds "MARK<b>x</b>" (the planted text is there, inside code);
	//   every line of md outside its fenced code blocks that begins "| " has as many | not
	//   preceded by \ as the header line of its table (the first line of that run of "| " lines).
}

func TestFence(t *testing.T) {
	// fenced([]string{"a"})               == "```\na\n```\n"
	// fenced([]string{"x ``` y"})         == "````\nx ``` y\n````\n"
	// fenced([]string{"````", "b"})       == "`````\n````\nb\n`````\n"
}

func TestCodeSpan(t *testing.T) {
	// codeSpan("a b")   == "`a b`"
	// codeSpan("a`b")   == "``a`b``"
	// codeSpan("`a")    == "`` `a ``"
	// codeSpan("a ")    == "` a  `"
	// codeSpan("")      == ""
}

func TestMarkdownTable(t *testing.T) {
	// markdownTable([]string{"h1", "h2"}, []bool{false, true}, [][]string{{"a|b", "c|d"}, {"", "e"}},
	//   []bool{false, true}) ==
	//   "| h1 | h2 |\n| --- | ---: |\n| a\\|b | `c\\|d` |\n|  | `e` |\n"
}

func TestSummaryLine(t *testing.T) {
	// SummaryLine("warning: the cluster was not deleted: a ``` b") ==
	//   "\n````\nwarning: the cluster was not deleted: a ``` b\n````\n"
	// SummaryLine("warning: a\nb\tc") == "\n```\nwarning: a b c\n```\n"
}
```

Two helpers in `summary_test.go`, each with its comment:

```go
// plantFreeText returns a copy of r in which every string of a field without the tag mask:"keep",
// those of lists, pointers and map values included, is s: the text from outside the tool, as Mask
// reads it. Map keys and the vocabulary stay.
func plantFreeText(r Run, s string) Run

// outsideCode returns the text of md that is outside its fenced code blocks and code spans, read by
// the rules that fenced and codeSpan write by, which are GitHub's: a fence opens at a line that
// begins, after at most three spaces, with three or more backticks, and closes at the next line
// that is, after at most three spaces, a run of at least as many backticks and nothing else but
// spaces; outside a fence, a run of n backticks opens a code span that the next run of exactly n
// backticks on its line closes. A fence or a span that is not closed is an error.
func outsideCode(md string) (string, error)
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/report/ -run 'TestSummary|TestFence|TestCodeSpan|TestMarkdownTable' -v`
Expected: FAIL to compile: `Summary`, `SummaryLine`, `fenced`, `codeSpan` and `markdownTable` are not defined.

- [ ] **Step 3: Implement, each named one with its interface comment from the Interfaces block and from the comments above**

The refactor of `console.go` first, with `go test ./internal/report/ -run 'TestConsole|TestClean'` passing after it and no golden file of the console changed (`git status --short internal/report/testdata` prints nothing); then `summary.go`.

- [ ] **Step 4: Write the golden files, and read them**

Run: `go test ./internal/report/ -run TestSummaryGolden -update`, then `go test ./internal/report/ -v`.
Expected: PASS. Then:

- `testdata/summary-pass.golden.md` equals, byte for byte, decision 4's example in the README (the text between its two four-backtick fences, without their indentation of three spaces).
- Each of the other seven is read against decisions 4 and 5: a heading, the set-up block, the tables, the lists only for a pass, the last block; free text only inside code. A file that breaks a rule is a fault of the code, fixed before the commit; the golden file is never edited by hand.

- [ ] **Step 5: Run the checks**

Run: `gofmt -l .`, `go test ./...` and the four vets.
Expected: PASS, no output.

- [ ] **Step 6: Commit**

```
git add internal/report/summary.go internal/report/summary_test.go internal/report/console.go internal/report/testdata/summary-pass.golden.md internal/report/testdata/summary-fail.golden.md internal/report/testdata/summary-not-verified.golden.md internal/report/testdata/summary-before-first-observation.golden.md internal/report/testdata/summary-in-install-window.golden.md internal/report/testdata/summary-install-decided.golden.md internal/report/testdata/summary-custom-no-ms-or-lag.golden.md internal/report/testdata/summary-stall-mirror.golden.md
git commit -m "Render the console's blocks as the job summary, with text from outside the tool only in code blocks and code spans"
```
