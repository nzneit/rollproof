# Task 14: the lines for GitHub's workflow commands, in the tool, the action's script and the suite's harness

Read section 8 ("The action", whole, and its paragraph "Workflow commands"), section 10 ("Console", its bullet on standard error; "On a signal"), section 12 (the bullet "The lines for GitHub's workflow commands"), section 14, point 33, and decisions 17, 21 and 26 of the README, before this task. It comes after Task 12 and before Task 13, whose local runs (its Step 1) are the first fixture runs after it.

Unlike Tasks 2 to 12, every command of this task runs from the repository root: Go commands as `go -C projects/rollproof …`, paths in full.

### Task 14: stop the runner's workflow commands around what `rollproof run` and `action.sh` print, resume them when the tool ends by itself, and check both lines in the harness

**Files:**
- Create: `projects/rollproof/cmd/rollproof/workflowcommands.go`, `projects/rollproof/cmd/rollproof/workflowcommands_test.go`
- Modify: `projects/rollproof/cmd/rollproof/main.go` (`realMain`), `projects/rollproof/cmd/rollproof/main_test.go` (`startChild`)
- Modify: `projects/rollproof/internal/report/github.go` (new `GitHubActions`; `GitHubFrom` asks it), `projects/rollproof/internal/report/github_test.go`, `projects/rollproof/internal/report/result.go` (`Open` asks `GitHubActions`)
- Modify: `projects/rollproof/action.sh` (its opening comment; new `token_file` and `stop_commands`; `main`)
- Modify: `projects/rollproof/hack/action/action_test.go`, `projects/rollproof/hack/action/action_cluster_test.go`
- Modify: `projects/rollproof/e2e/runenv_test.go` (new `stopLine`, `envValue`, `workflowCommandFaults`), `projects/rollproof/e2e/harness_test.go` (`runFixture`)
- Modify: `projects/rollproof/README.md` (two sentences in "On GitHub Actions")

**Interfaces:**
- Consumes: `realMain`, `runCommand`, `runContext`, `keptSignal`, `endProcess` (as built); `report.GitHubFrom` (Task 4); `action.sh`'s `fail` and `main` (Task 8); `runFixture`, `runEnv` (Task 7); the README's paragraph of Task 11.
- Produces: `func GitHubActions(getenv func(string) string) bool` in `internal/report`
  GitHubActions reports whether the tool runs on GitHub Actions: GITHUB_ACTIONS is
  exactly "true" (section 8), which a caller's env: cannot change there.
  GitHubFrom, Open and the run command's lines for workflow commands all ask it,
  so that the rule is written once.
- Produces: `var newStopToken = func() string { return rand.Text() }` in `cmd/rollproof`
  newStopToken gives the token of one run's lines for GitHub's workflow commands:
  26 random characters of crypto/rand.Text, which no command of the runner is
  named. A test replaces it, and puts it back with t.Cleanup.
- Produces: `func withWorkflowCommandsStopped(stdout, stderr io.Writer, getenv func(string) string, signalled func() bool, command func() int) int` in `cmd/rollproof`
  withWorkflowCommandsStopped runs command, and returns its exit code, between the
  lines that stop and resume the workflow commands of GitHub's runner (design,
  section 8, "Workflow commands"). On GitHub Actions, as report.GitHubActions
  detects it, it first prints "::stop-commands::TOKEN" on stdout and then on
  stderr, TOKEN new for each call: the runner reads each stream in its own order,
  so each needs the line first, and the one that the runner meets second is a
  plain line. The runner then reads no line of the command as a command, but for
  the two things that the design's section 8 leaves open. When
  command has returned and signalled says that no signal came, it prints
  "::TOKEN::" on stdout, so that a step that runs the tool from a script of its
  own can use workflow commands again. After a signal it prints nothing more: the
  process ends by the signal, and the step with it. A panic in command passes
  through with no resume line. Elsewhere it only runs command. A nil getenv is
  not GitHub Actions.
- Produces: `func signalCame() bool` in `cmd/rollproof`
  signalCame says whether runContext kept a signal, by which endProcess will end
  the process. runCommand's release has returned by the time
  withWorkflowCommandsStopped asks, so the answer is final; a signal that comes
  later takes its default action and ends the process at once.
- Changes: `func startChild(t *testing.T, fake string, env ...string) *helperChild` in `cmd/rollproof/main_test.go`
  startChild starts the child with the fake run that fake names, and env added to
  its environment. The child gets no GITHUB_ACTIONS from this process, so that it
  prints the lines for GitHub's workflow commands only when env asks for them, on
  this machine and on GitHub's runner alike. The child is killed at the end of
  the test if it still runs.
- Produces, in `action.sh`: the variable `token_file` and the function `stop_commands`, with their comments in Step 3; the text of failure `rollproof: no token for the line that stops the runner's workflow commands: PATH gave none` (exit 2, PATH the file it read), and the two texts `rollproof: the line that stops the runner's workflow commands could not be written to standard output` and `… to standard error` (exit 2). `fail` changes too: it exits 2 also when its message cannot be written.
- Produces, in `e2e/runenv_test.go` (no build tag): ``var stopLine = regexp.MustCompile(`^::stop-commands::[A-Z2-7]{26}$`)``, `func envValue(env []string, name string) string` and `func workflowCommandFaults(stdout, stderr string, onGitHub, bySignal bool) []string`, with their comments in Step 3.
- Produces: the lines themselves (public, decision 26). The tool, on GitHub Actions: `::stop-commands::TOKEN` first on stdout and first on stderr, TOKEN 26 characters of `A` to `Z` and `2` to `7`; `::TOKEN::` last on stdout when it ends by itself. The script: `::stop-commands::UUID` first on stdout and first on stderr, right after its check for Linux, UUID the kernel's form `xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx`; never a resume line.

Why the code lives in `cmd/rollproof`, around the `run` case of `realMain`, and not in `run.Run` (decision 26): the flag package prints a usage error on stderr before `run.Run` is called, so a stop line printed in `run.Run` would not be first on stderr for a bad flag or an argument that is no flag, and those texts hold the caller's arguments; and whether the process ends by a signal is known for certain only after `runCommand`'s `release` has returned, which is after `run.Run` returned. In `run.Run` it would also cost a seam in the run's fakes and a GitHub concern in a package that knows no streams but the two it is given. `version`, `probe` and the top-level usage print no text from outside the tool, so they print neither line. A panic which escapes the run prints no resume line: the commands then stay stopped for the rest of the step, and the panic's trace on stderr is not read as commands.

What the `run` command writes on standard error, read at this task's start (the residue that section 10 names): the flag package's error and the usage text, before a run (`cmd/rollproof/main.go:147-160`); `rollproof: cannot write results: …` when the results folder cannot be opened, before anything else of the run (`internal/run/run.go:137`), when the final `result.json` is refused, at the verdict (`run.go:1048`, `run.go:1059`), and when `result.json` cannot be written again after a late warning, which can be the last line before the run returns (`run.go:1276`); and, outside `run`'s own writes, the log lines of `k8s.io/client-go` through `klog`, which writes to the process's standard error, synchronously, by default: a warning that the API server sends with a response (`client-go@v0.37.2/rest/warnings.go:103-107`, `klog/v2@v2.140.0/klog.go:441, 916-921`), its message quoted on one line (`klog.go:835-840`). After the verdict the tool's own line is the only one, and its text is the results folder's path and the system's error. No test of this task covers that residue, nor the second thing that section 8 leaves open (the stop line of standard error read after the resume line of a run that ended at once): both lie in the order in which the runner reads the two streams, which no test here can set.

The existing tests, read at `a048765`, and what each needs: none breaks. `TestUsage`, `TestUsageTextIsExact`, `TestRunWithoutAFile`, `TestRunWithKeepCluster` and `TestRunWithoutAFileFlag` (`cmd/rollproof/main_test.go:87-198`) pass `noEnv`, so no line is added. The signal tests in a child (`main_test.go:976-1069`) inherit this process's environment, which holds `GITHUB_ACTIONS=true` in the unit workflow on GitHub: they hold there too, since `waitFor` and their checks read with `strings.Contains`, but `TestNoSignalExitsWithTheCode` checks that the child's output holds no `FAIL`, which a random token can hold; `startChild` therefore drops `GITHUB_ACTIONS` (Interfaces). In `hack/action/action_test.go` every check of a run reads standard error's last line (`:327`, `:545`) or a sourced function, which prints no stop line, and `r.run` keeps no standard output; the rows "not Linux" and "a uname that fails" end before the stop line. `TestTheScriptRunsTheToolFromItsImage` (`hack/action/action_cluster_test.go:180`) reads the script's output with `CombinedOutput` and asserts nothing of it. In `e2e`, every reading of a run's console is `strings.Contains` (`fixtures_test.go:210, 459`, `commands_test.go:101`, `custom_test.go:326`), `checkGitHubFiles` reads the two files and not the streams, and the mirror job's search for the password reads `console.txt` and `stderr.txt`, which hold no form of it. `internal/run`'s tests and the console's golden files do not change: the lines are not the run's. The tests that change are named in Step 1.

- [ ] **Step 1: Write the failing tests**

In `internal/report/github_test.go`:

```go
func TestGitHubActions(t *testing.T) {
	// GitHubActions with a getenv that gives GITHUB_ACTIONS the row's value, one row each:
	//   "true" -> true; unset -> false; "" -> false; "false" -> false; "TRUE" -> false;
	//   "1" -> false; " true" -> false; "true\n" -> false.
	// TestGitHubFrom stays as it is and still passes.
}
```

In `cmd/rollproof/workflowcommands_test.go` (new, no build tag), a helper and the tests:

```go
// stopToken replaces newStopToken, for the test, by one that gives token, and puts it back with
// t.Cleanup.
func stopToken(t *testing.T, token string)

// githubEnv is a getenv that gives GITHUB_ACTIONS "true" and nothing else.
func githubEnv(name string) string

// fakeCommand writes "a line of the run\n" to stdout and "an error of the run\n" to stderr and
// returns 1.
func fakeCommand(stdout, stderr io.Writer) func() int
```

```go
func TestWorkflowCommandsAreStoppedAroundTheRun(t *testing.T) {
	// stopToken(t, "TESTTOKEN"); getenv githubEnv; signalled gives false and counts its calls; the
	// command first records what stdout and stderr hold, then does what fakeCommand does.
	// At the command's start stdout and stderr each hold exactly "::stop-commands::TESTTOKEN\n".
	// withWorkflowCommandsStopped returns 1; stdout is exactly
	// "::stop-commands::TESTTOKEN\na line of the run\n::TESTTOKEN::\n"; stderr exactly
	// "::stop-commands::TESTTOKEN\nan error of the run\n"; signalled was asked once, after the
	// command returned.
}

func TestNoResumeLineAfterASignal(t *testing.T) {
	// As above with signalled giving true: returns 1; stdout exactly
	// "::stop-commands::TESTTOKEN\na line of the run\n"; stderr exactly
	// "::stop-commands::TESTTOKEN\nan error of the run\n".
}

func TestNoWorkflowCommandLinesOffGitHub(t *testing.T) {
	// newStopToken and signalled replaced by functions that fail the test when called; the command
	// fakeCommand. One row each for getenv: nil; GITHUB_ACTIONS unset; "", "false", "TRUE", "1",
	// " true", "true ". Each returns 1; stdout exactly "a line of the run\n"; stderr exactly
	// "an error of the run\n".
}

// A panic that escapes the run is no end by itself that the step can trust: no resume line is
// printed, so the commands stay stopped. That the runner then reads the panic's trace on stderr
// as plain lines is read in its source, not shown here (decision 26).
func TestAPanicInTheRunLeavesNoResumeLine(t *testing.T) {
	// stopToken(t, "TESTTOKEN"); getenv githubEnv; signalled fails the test when called; the
	// command panics with "the run broke". The panic reaches the test, with that value; stdout and
	// stderr are each exactly "::stop-commands::TESTTOKEN\n".
}

func TestEachRunHasItsOwnToken(t *testing.T) {
	// The real newStopToken; getenv githubEnv; signalled gives false; two calls, each with a
	// command that prints nothing and returns 0. In each, stdout is exactly two lines,
	// "::stop-commands::" + T and "::" + T + "::", T matching ^[A-Z2-7]{26}$, and stderr is
	// exactly the first of them and a line end. The two calls' T differ.
}

// The lines come first and last on every path where the run command ends by itself, its usage
// errors included, through realMain and the real units.
func TestTheRunCommandsLinesComeFirstAndLast(t *testing.T) {
	// stopToken(t, "TESTTOKEN"); realMain with the row's args and getenv githubEnv. Each row exits 2.
	//   "a flag it does not know": ["run", "--nope"]. stdout exactly
	//     "::stop-commands::TESTTOKEN\n::TESTTOKEN::\n"; stderr's first line
	//     "::stop-commands::TESTTOKEN", its second "flag provided but not defined: -nope", and it
	//     holds "usage: rollproof run".
	//   "an argument that is no flag": ["run", "-f", "rollproof.yaml", "extra"]. stdout as above;
	//     stderr's first line "::stop-commands::TESTTOKEN", and it holds "usage: rollproof run".
	//   "a rejected configuration": ["run", "-f", "does-not-exist.yaml", "--out", OUT], OUT
	//     t.TempDir(). stdout's first line "::stop-commands::TESTTOKEN", its second exactly
	//     "rollproof: NOT VERIFIED", its last "::TESTTOKEN::" and the one before it "results: " +
	//     OUT; stderr exactly "::stop-commands::TESTTOKEN\n".
	//   "a results folder that cannot be opened": ["run", "-f", "does-not-exist.yaml", "--out",
	//     FILE], FILE a file that the test made. stdout exactly
	//     "::stop-commands::TESTTOKEN\n::TESTTOKEN::\n"; stderr exactly two lines, the first
	//     "::stop-commands::TESTTOKEN", the second beginning "rollproof: cannot write results: ".
}

func TestOnlyTheRunCommandStopsWorkflowCommands(t *testing.T) {
	// newStopToken replaced by a function that fails the test when called; realMain with getenv
	// githubEnv. One row each: ["version"] exits 0; no args exits 2; ["probe", "http", "--url",
	// "http://127.0.0.1:1/", "--rate", "0"] exits 2. In each row no line of stdout or stderr
	// begins "::" after its leading white space, and none holds "##[".
}

// After a signal the process ends by it, and the step with it: no resume line (section 8,
// "Workflow commands").
func TestASignalLeavesWorkflowCommandsStopped(t *testing.T) {
	// startChild(t, fakeWaits, "GITHUB_ACTIONS=true"); waitFor "started"; SIGTERM. The child ends
	// within 10 s, by SIGTERM. Its stdout is exactly three lines: "::stop-commands::" + T, T
	// matching ^[A-Z2-7]{26}$, then "started", then "returned"; its stderr's first line is
	// "::stop-commands::" + T; no line of either stream is "::" + T + "::".
}

func TestARunThatEndsByItselfResumesWorkflowCommands(t *testing.T) {
	// startChild(t, fakeFails, "GITHUB_ACTIONS=true"). The child ends within 20 s with exit 1. Its
	// stdout is exactly three lines: "::stop-commands::" + T, T matching ^[A-Z2-7]{26}$, then
	// fakeFailsLine, then "::" + T + "::"; its stderr's first line is "::stop-commands::" + T.
}
```

In `hack/action/action_test.go`, two patterns beside `ref`, which `action_cluster_test.go` uses too (add `regexp` to the imports):

```go
// scriptStopLine is the line by which action.sh stops the runner's workflow commands: its token
// is the kernel's random UUID.
var scriptStopLine = regexp.MustCompile(`^::stop-commands::[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// toolStopLine is the line by which the tool stops them: 26 characters of crypto/rand.Text.
var toolStopLine = regexp.MustCompile(`^::stop-commands::[A-Z2-7]{26}$`)
```

and the tests:

```go
func TestTheScriptStopsWorkflowCommandsFirst(t *testing.T) {
	// Two runs, each startingRun(t) and then r.start(r.script), so that stdout is kept too. Each
	// exits 7. Its stdout is exactly one line, which scriptStopLine matches; with U its token,
	// stderr's first line is that same line, and its last line
	// "rollproof: the tool from " + ref + ", through docker"; no line of either stream is
	// "::" + U + "::". The two runs' tokens differ.
}

func TestTheScriptsStopLine(t *testing.T) {
	// r.source("token_file=" + shQuote(PATH) + "; stop_commands"), one row each:
	//   PATH a file holding "6d1e3c9a-2b7f-4c1e-9a0d-3f5b8e2c7a14\n" -> stdout
	//     "::stop-commands::6d1e3c9a-2b7f-4c1e-9a0d-3f5b8e2c7a14\n", exit 0, stderr the same as stdout
	//   PATH a file holding "tok-1", with no line end -> stdout "::stop-commands::tok-1\n", 0,
	//     stderr the same as stdout
	//   PATH a path that does not exist -> stdout "", 2, stderr
	//     "rollproof: no token for the line that stops the runner's workflow commands: " + PATH + " gave none\n"
	//   PATH an empty file -> stdout "", 2, stderr the same text with that PATH
	//   PATH the first file, and standard output closed for stop_commands (" >&-" after it) ->
	//     stdout "", 2, stderr's last line "rollproof: the line that stops the runner's workflow
	//     commands could not be written to standard output" (bash's own message for the failed
	//     write may stand before it)
	//   PATH the first file, and standard error closed (" 2>&-") -> stdout the stop line, 2;
	//     stderr is not judged
}
```

`TestTheScriptFailsWithExit2AndWritesNoOutput` changes: its rows gain `beforeStop bool`, true for "not Linux" and "a uname that fails" and false for the others; each row runs `stdout, code, stderr := r.start(r.script)` in place of `r.run()`, keeps every check it has, and adds: for a row with `beforeStop`, stdout is empty and stderr is exactly the row's text and a line end; for the others, stdout is exactly one line, which `scriptStopLine` matches, and stderr's first line is that same line.

`TestTheScriptRunsTheToolFromItsImage` (`action_cluster_test.go`) keeps the script's standard output and standard error apart (two `bytes.Buffer`, `cmd.Run`, `bytes` added to the imports), logs both as `t.Logf("action.sh's standard output:\n%s\nits standard error:\n%s", …)`, and, after its check of the exit code, adds, with U the script's token and T the tool's:

```go
	// stdout's first line is matched by scriptStopLine, its second by toolStopLine, and its last
	// is "::" + T + "::"; stderr's first line is stdout's first; the line of stderr after the one
	// that begins "rollproof: the tool from " is stdout's second, and it is stderr's last; no line
	// of either stream is "::" + U + "::".
```

In `e2e/runenv_test.go`:

```go
func TestEnvValue(t *testing.T) {
	// envValue(env, "GITHUB_ACTIONS"), one row each:
	//   ["A=1", "GITHUB_ACTIONS=true"] -> "true"
	//   ["GITHUB_ACTIONS=true", "GITHUB_ACTIONS="] -> ""
	//   ["GITHUB_ACTIONS=", "GITHUB_ACTIONS=true"] -> "true"
	//   ["A=1"] -> ""
	//   nil -> ""
}

func TestWorkflowCommandFaults(t *testing.T) {
	// T is "ABCDEFGHIJKLMNOPQRSTUVWXYZ", STOP "::stop-commands::" + T, RESUME "::" + T + "::";
	// OUT is STOP + "\nrollproof: PASS\nresults: /r\n" + RESUME + "\n". One row each, the arguments
	// stdout, stderr, onGitHub, bySignal, and exactly the faults wanted (none is nil):
	//   "off GitHub": "rollproof: PASS\nresults: /r\n", "", false, false -> none
	//   "off GitHub, a stop line": STOP + "\nrollproof: PASS\n", "", false, false ->
	//     [`stdout holds the line "::stop-commands::ABCDEFGHIJKLMNOPQRSTUVWXYZ", which begins "::", though GITHUB_ACTIONS is not true`]
	//   "off GitHub, an indented line on stderr": "rollproof: PASS\n", "  ::error::x\n", false, false ->
	//     [`stderr holds the line "  ::error::x", which begins "::", though GITHUB_ACTIONS is not true`]
	//   "on GitHub": OUT, STOP + "\n", true, false -> none
	//   "on GitHub, after a signal": STOP + "\nrollproof: NOT VERIFIED\nresults: /r\n", STOP + "\n", true, true -> none
	//   "no stop line": "rollproof: PASS\n" + RESUME + "\n", STOP + "\n", true, false ->
	//     [`stdout's first line is "rollproof: PASS", want "::stop-commands::" and a token of 26 characters`]
	//   "a token of another form": "::stop-commands::abc\n" + RESUME + "\n", STOP + "\n", true, false ->
	//     [`stdout's first line is "::stop-commands::abc", want "::stop-commands::" and a token of 26 characters`]
	//   "nothing on stdout": "", STOP + "\n", true, false ->
	//     [`stdout's first line is "", want "::stop-commands::" and a token of 26 characters`]
	//   "stderr begins with another line": OUT, "warning\n" + STOP + "\n", true, false ->
	//     [`stderr's first line is "warning", want "::stop-commands::ABCDEFGHIJKLMNOPQRSTUVWXYZ", stdout's`]
	//   "the stop line twice": STOP + "\n" + OUT, STOP + "\n", true, false ->
	//     ["stdout holds the stop line 2 times, want once"]
	//   "no resume line": STOP + "\nrollproof: PASS\n", STOP + "\n", true, false ->
	//     [`stdout's last line is "rollproof: PASS", want "::ABCDEFGHIJKLMNOPQRSTUVWXYZ::"`]
	//   "the resume line twice": OUT + RESUME + "\n", STOP + "\n", true, false ->
	//     [`stdout holds the line "::ABCDEFGHIJKLMNOPQRSTUVWXYZ::" 2 times, want once`]
	//   "a resume line after a signal": OUT, STOP + "\n", true, true ->
	//     [`stdout holds the line "::ABCDEFGHIJKLMNOPQRSTUVWXYZ::", though a signal ended the run`]
	//   "a resume line on stderr": OUT, STOP + "\n" + RESUME + "\n", true, false ->
	//     [`stderr holds the line "::ABCDEFGHIJKLMNOPQRSTUVWXYZ::", which only stdout's last line may be`]
	//   "off GitHub, a line after a lone carriage return": "rollproof: PASS\rnext\r::error::x\n", "", false, false ->
	//     [`stdout holds the line "::error::x", which begins "::", though GITHUB_ACTIONS is not true`]
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run, from the repository root: `go -C projects/rollproof test ./internal/report/ ./cmd/rollproof/ ./hack/action/ ./e2e/ -run 'TestGitHubActions|TestWorkflowCommandsAreStoppedAroundTheRun|TestNoResumeLineAfterASignal|TestNoWorkflowCommandLinesOffGitHub|TestAPanicInTheRunLeavesNoResumeLine|TestEachRunHasItsOwnToken|TestTheRunCommandsLinesComeFirstAndLast|TestOnlyTheRunCommandStopsWorkflowCommands|TestASignalLeavesWorkflowCommandsStopped|TestARunThatEndsByItselfResumesWorkflowCommands|TestTheScriptStopsWorkflowCommandsFirst|TestTheScriptsStopLine|TestTheScriptFailsWithExit2AndWritesNoOutput|TestEnvValue|TestWorkflowCommandFaults' -v`
Expected: FAIL: `GitHubActions` is not defined in `internal/report`; `newStopToken`, `withWorkflowCommandsStopped` and `signalCame` are not defined in `cmd/rollproof`; `envValue`, `workflowCommandFaults` and `stopLine` are not defined in `e2e`. `hack/action` compiles and fails: `TestTheScriptStopsWorkflowCommandsFirst` and the rows of `TestTheScriptFailsWithExit2AndWritesNoOutput` that are not `beforeStop` find stdout empty, and `TestTheScriptsStopLine` finds no function `stop_commands` (bash exits 127).

- [ ] **Step 3: Implement**

`internal/report/github.go`: `GitHubActions` with its interface comment, placed before `GitHubFrom`; `GitHubFrom` begins `if !GitHubActions(getenv) {` in place of `if getenv("GITHUB_ACTIONS") != "true" {`, and its comment's second sentence becomes "The tool detects GitHub Actions as GitHubActions does (section 8); the GitHub it returns then writes the outputs to the file that GITHUB_OUTPUT names and the summary to the file that GITHUB_STEP_SUMMARY names, each only when its variable is not empty." `internal/report/result.go`: in `Open`, the condition `getenv("GITHUB_ACTIONS") == "true" && getenv("RUNNER_TEMP") != ""` becomes `GitHubActions(getenv) && getenv("RUNNER_TEMP") != ""`, so that the rule is written once.

`cmd/rollproof/workflowcommands.go`, exactly but for the interface comments, which are those of the Interfaces block:

```go
package main

import (
	"crypto/rand"
	"fmt"
	"io"
	"syscall"

	"rollproof/internal/report"
)

// newStopToken … (the Interfaces block's comment)
var newStopToken = func() string { return rand.Text() }

// withWorkflowCommandsStopped … (the Interfaces block's comment)
func withWorkflowCommandsStopped(stdout, stderr io.Writer, getenv func(string) string, signalled func() bool, command func() int) int {
	if getenv == nil || !report.GitHubActions(getenv) {
		return command()
	}
	token := newStopToken()
	// Each stream is read in its own order, so each gets the stop line before anything else. The
	// lines are written as they are, not through the run's masker: they hold no value of the
	// configuration.
	fmt.Fprintln(stdout, "::stop-commands::"+token)
	fmt.Fprintln(stderr, "::stop-commands::"+token)
	code := command()
	if !signalled() {
		fmt.Fprintln(stdout, "::"+token+"::")
	}
	return code
}

// signalCame … (the Interfaces block's comment)
func signalCame() bool {
	_, ok := keptSignal.Load().(syscall.Signal)
	return ok
}
```

`cmd/rollproof/main.go`, in `realMain`, the case `"run"` becomes:

```go
	case "run":
		// The lines for GitHub's workflow commands go around the whole command, its usage errors
		// included (section 8, "Workflow commands").
		return withWorkflowCommandsStopped(stdout, stderr, getenv, signalCame, func() int {
			return runCommand(args[1:], stdout, stderr, getenv)
		})
```

and `realMain`'s comment gains the sentence "For run, it prints the lines of withWorkflowCommandsStopped around the command." `runCommand` does not change: its `defer release()` runs before it returns, so `signalCame` reads the final state.

`cmd/rollproof/main_test.go`: `startChild` takes `env ...string`, builds the child's environment from `os.Environ()` without any entry that begins `GITHUB_ACTIONS=`, then `ROLLPROOF_HELPER=` + fake, then env, and carries the comment of the Interfaces block. Its existing calls do not change.

`action.sh`. In its opening comment, after the line "the step's verdict stays empty. Sourced, it sets its shell options and defines its functions, and" and the line after it, "does not run main.", insert:

```bash
#
# Once it knows that the runner is Linux, and before any line that can hold an input or an
# engine's output, it prints the line that stops the runner's workflow commands, with a token of
# its own, on both streams, and it never prints the line that resumes them: the runner then reads
# no line of the step as a command, the tool's own lines included (design, section 8, "Workflow
# commands").
```

and the line "# Besides bash and the engine, it uses uname, mktemp and tar." becomes "# Besides bash and the engine, it uses uname, mktemp and tar, and reads /proc/sys/kernel/random/uuid.". `fail` becomes, so that the script's own failures exit 2 also when standard error cannot be written (under `set -e` a `printf` that fails would end the script with 1, the tool's code for a fail, before `exit 2`):

```bash
# fail prints its message, after "rollproof: ", to standard error, and exits 2, also when the
# message cannot be written.
fail() {
  printf 'rollproof: %s\n' "$1" >&2 || true
  exit 2
}
```

After `fail`, insert:

```bash
# token_file is where stop_commands reads its token: the kernel gives a new random UUID at each
# read. A test that sources this script sets it to a file of its own.
token_file=/proc/sys/kernel/random/uuid

# stop_commands prints "::stop-commands::TOKEN" on standard output and on standard error, TOKEN
# the first line of token_file, read with bash's own read. GitHub's runner then reads no workflow
# command in the step until a line that names TOKEN as a command ("::TOKEN::", or the older form
# "##[TOKEN]" anywhere in a line), which this script never prints. Each stream gets the line,
# because the runner reads each in its own order. Without a token, or when it cannot write one
# of the two lines, it fails.
stop_commands() {
  local token=
  read -r token 2>/dev/null <"$token_file" || true
  [[ -n $token ]] || fail "no token for the line that stops the runner's workflow commands: $token_file gave none"
  printf '::stop-commands::%s\n' "$token" || fail "the line that stops the runner's workflow commands could not be written to standard output"
  printf '::stop-commands::%s\n' "$token" >&2 || fail "the line that stops the runner's workflow commands could not be written to standard error"
}
```

In `main`, after the line `[[ $system == Linux ]] || fail "the runner is not Linux (uname -s printed $system); the action runs only on Linux"`, insert:

```bash
  # From here on a line may hold an input or an engine's output.
  stop_commands
```

(`2>/dev/null` stands before the redirection of the file, so that bash's own message for a missing file goes nowhere; a file with no line end still gives `read` its token, as tried with bash 5.3.20 on 2026-11-22.)

`e2e/runenv_test.go`, beside `summaryFaults` (add `regexp` to the imports):

```go
// stopLine matches the line by which the tool stops GitHub's workflow commands:
// "::stop-commands::" and 26 characters of crypto/rand.Text's alphabet.
var stopLine = regexp.MustCompile(`^::stop-commands::[A-Z2-7]{26}$`)

// envValue gives the value of name in env, the last one when it stands twice, as
// os/exec uses it, and "" when it is not there.
func envValue(env []string, name string) string

// workflowCommandFaults judges a run's standard output and standard error against
// the lines for GitHub's workflow commands (design, section 8), and gives the
// faults it finds, none when they hold. onGitHub is whether the run's
// GITHUB_ACTIONS was "true", and bySignal whether a signal ended it. Off GitHub,
// no line of either stream begins "::" after its leading white space, which is
// how the runner reads a line. On GitHub, stdout's first line is
// "::stop-commands::TOKEN" with a token of the tool's form; stderr's first line is
// the same; each stream holds it once; "::TOKEN::" stands on stderr never, and on
// stdout exactly once, as its last line, unless a signal ended the run, when it
// stands nowhere. It stops at a first line of stdout that is not a stop line,
// since the other checks need its token.
func workflowCommandFaults(stdout, stderr string, onGitHub, bySignal bool) []string
```

The faults' texts are those of `TestWorkflowCommandFaults`, in this order: each stream's first line that begins `::` off GitHub (stdout, then stderr); on GitHub, stdout's first line; stderr's first line; the stop line's count in stdout, then in stderr; the resume line on stderr; then, after a signal, the resume line on stdout, and otherwise stdout's last line and the resume line's count in stdout. A stream's lines are its text without its last line end, split at each line end; an empty text has none. A line ends at CR LF, at LF and at a lone CR, as the runner's reader ends a line, so that a line after a lone carriage return is judged as the runner would read it (`streamLines`, whose comment says so).

`e2e/harness_test.go`, in `runFixture`, between the end of the `switch` on `runErr` and the reading of `result.json`:

```go
	// The run's own GITHUB_ACTIONS decides which lines it prints; on GitHub every run has it, and
	// on a developer's machine only the runs with files of their own.
	for _, fault := range workflowCommandFaults(stdout.String(), stderr.String(), envValue(env, "GITHUB_ACTIONS") == "true", r.signal != "") {
		t.Errorf("%s (%s): %s", f.name, f.helm, fault)
	}
```

and `runFixture`'s comment gains, after "…its standard error as stderr.txt.", the sentence "It checks the lines for GitHub's workflow commands in both streams by the run's own GITHUB_ACTIONS (workflowCommandFaults)." Above the `t.Logf` that logs the run's two streams stands, exactly:

```go
	// On GitHub's runner the lines of this log are read for workflow commands too: go test
	// indents them, and the runner trims that. So the logged stop lines of a run with
	// GITHUB_ACTIONS true keep the runner's commands stopped for the rest of the go test step,
	// which prints none: the logged resume line is followed at once by the run's stop line of
	// standard error, which stops them again (design, section 12, "The suite's workflow").
```

Above the one in `TestTheScriptRunsTheToolFromItsImage` (`hack/action/action_cluster_test.go`), a comment says that on GitHub's runner the lines of that log are read for workflow commands too, since `go test` indents them and the runner trims that, so the script's stop line, logged there, stops the runner's commands for the rest of the `go test` step, which prints none (design, section 12, "The suite's workflow").

`README.md`, in the section "On GitHub Actions", after the sentence "The job summary holds the console's report as Markdown, with every text from the cluster, the commands and the configuration inside code.", in the same paragraph:

```markdown
GitHub's runner reads a line that a step prints as a workflow command when it begins with `::` after its leading white space, or holds `##[` anywhere, so on GitHub Actions the tool's first line on standard output and on standard error is `::stop-commands::` and a token, and its last line on standard output, when it ends by itself, is `::`, the token and `::` again: no line in between, a pod's message included, is read as a command, and a script that ran the tool can use commands afterwards. Two things are left open, because the runner reads the two streams apart: a line that the tool writes on standard error just before it ends may reach the runner after that last line, and is then read for commands, and after a run that ends at once, as a usage error does, the runner may read no command for the rest of the step. The action's script stops commands the same way, with a token of its own, and never resumes them, so its step reads none and neither thing arises there.
```

If Task 11's paragraph does not hold that sentence word for word, put the text after the paragraph's last sentence and say so in the report.

Then the helpers and tests of Step 1 in full, each named one with its interface comment.

- [ ] **Step 4: Run the tests to verify they pass**

Run, each alone, from the repository root:
- `go -C projects/rollproof test ./internal/report/ ./cmd/rollproof/ ./hack/action/ ./e2e/ -v`
- `go -C projects/rollproof test -race ./cmd/rollproof/`
- `bash -n projects/rollproof/action.sh`
- `gofmt -l projects/rollproof`
- `go -C projects/rollproof test ./...`
- `go -C projects/rollproof vet ./...`, `go -C projects/rollproof vet -tags cluster ./...`, `go -C projects/rollproof vet -tags e2e ./...`, `go -C projects/rollproof vet -tags mirror ./...`
- `grep -rnE '[DGO]-[0-9]{3}' projects/rollproof/cmd/rollproof projects/rollproof/internal/report projects/rollproof/action.sh projects/rollproof/hack/action projects/rollproof/e2e projects/rollproof/README.md`
- `bun tools/citations/cli.ts repo`

Expected: PASS, no race; `bash -n`, `gofmt` and the vets print nothing; the search prints nothing and exits 1, which is the pass; the citation check exits 0. An exit code of 2 from the search or the citation check means it did not run: report it as unchecked, never as a pass.

- [ ] **Step 5: Run the script against this machine's engine**

Run, from the repository root: `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go -C projects/rollproof test -tags cluster -p 1 -count=1 ./hack/action/ -run TestTheScriptRunsTheToolFromItsImage -v -timeout 15m`
Expected: PASS, its log showing the script's stop line and the tool's on both streams, and the tool's resume line last on standard output. Afterwards `nerdctl ps -a --format "{{.Names}}"` prints no `rollproof-action-test-registry`, and `nerdctl images --format "{{.Repository}}"` prints no line that holds `rollproof-action-test`.

What this shows and what it does not: the order of the script's and the tool's lines in a real run of the script and the real binary, on rootless nerdctl. Not here: GitHub's runner reading them, which only a hosted run shows (Task 13), and then only in its log; a command planted after the stop line, which no run of the plan prints; the suite's fixtures, which Task 13's Step 1 runs.

**Stop rule.** When the script's pull fails here, stop and report as Task 9's stop rule says; it is not this task's change.

- [ ] **Step 6: Commit**

From the repository root, each command alone:

```
git add projects/rollproof/cmd/rollproof/workflowcommands.go projects/rollproof/cmd/rollproof/workflowcommands_test.go projects/rollproof/cmd/rollproof/main.go projects/rollproof/cmd/rollproof/main_test.go projects/rollproof/internal/report/github.go projects/rollproof/internal/report/github_test.go projects/rollproof/internal/report/result.go projects/rollproof/action.sh projects/rollproof/hack/action/action_test.go projects/rollproof/hack/action/action_cluster_test.go projects/rollproof/e2e/runenv_test.go projects/rollproof/e2e/harness_test.go projects/rollproof/README.md
git commit -F MESSAGE
```

MESSAGE is a file outside the tracked tree that holds the subject, "Stop GitHub's workflow commands before anything the tool or the action's script prints, resume them when the tool ends by itself, and check both lines in the suite's harness", an empty line, and the table of the error paths and special-case branches that the change adds (the script's failure for no token; the wrapper's paths off GitHub, after a signal and after a panic), as the run's rules for a commit ask. The fix round after this task's reviews is a commit of its own, with a table of its own: the script's two failed writes of the stop line, and `fail` when its own message cannot be written.

The message carries no attribution to an AI tool: no `Co-Authored-By` trailer that names one, no "Generated with" line, no session link, whatever a tool's harness asks (Global Constraints).
