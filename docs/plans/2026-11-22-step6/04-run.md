# Task 6: the order of what a run writes

Read section 8 ("The command line", the exit after a signal; "The action", the outputs' two moments and the refused final write), section 10 ("When it is written", "On a signal", "GitHub job summary", rule 10), section 12 (the bullets "The command line" and "What the tool writes for GitHub"), section 14, points 27 and 32 (the bullet "7.5 s after a cancel"), and decisions 2, 3 and 6 of the README, before this task.

### Task 6: the outputs at both moments, the summary, the console before the evidence, late warnings as lines

**Files:**
- Modify: `internal/run/run.go` (`Run`; `runState`; `finish`; `lateWarnings`; `warnAfterConsole`; new: `gitHubFiles`, `gitHubOf`, `writeOutputs`, `appendSummary`, `refusedWrite`, the two warning prefixes)
- Modify: `internal/run/run_test.go`, `internal/run/fakes_test.go`
- Modify: `cmd/rollproof/main_test.go`

**Interfaces:**
- Consumes: `report.GitHub`, `report.GitHubFrom`, `report.Output`, `(report.GitHub).WriteOutputs`, `(report.GitHub).AppendSummary` (Task 4); `report.Summary`, `report.SummaryLine` (Task 5).
- Produces: `type gitHubFiles interface { WriteOutputs(outs ...report.Output) error; AppendSummary(text string) error }`
  What a run asks of GitHub's files: the step's outputs and its job summary.
  report.GitHub is the real one.
- Produces: `var gitHubOf = func(getenv func(string) string) gitHubFiles { return report.GitHubFrom(getenv) }`
  gitHubOf gives the run's GitHub files from the environment. A test replaces it,
  to see the moment of each write.
- Produces: the constants `outputsNotWritten = "the step's outputs were not written: "` and `summaryNotWritten = "the job summary was not written: "`.

How `finish`, `lateWarnings` and `warnAfterConsole` change (decision 6):

- `Run`, right after `report.Open` succeeded: `s.gh = gitHubOf(o.Getenv)` when `o.Getenv` is not nil, and then the first moment of the outputs, `s.writeOutputs(report.Output{Name: "out", Value: w.Dir()}, report.Output{Name: "verdict", Value: string(verdict.NotVerified)})`, with the comment "The first moment of the outputs (section 8): the results folder, and verdict as not verified, with the first result.json, so that a run that is killed leaves an answer." A results folder that cannot be opened returns before this, and writes no output.
- `runState` gains `gh gitHubFiles` (nil writes nothing), `outputsWarned, summaryWarned bool`, and `warningsTold int`, with the comment "how many of the run's warnings the console and the job summary have told; warningsWritten is how many the last result.json holds".
- `finish` becomes:

```go
// finish performs steps 14 to 16 in the order section 10 gives: the final result.json, the second
// verdict of the outputs, the job summary and the console's text, all from one record; then the
// evidence completed, and the warnings that this added told as lines of the console and of the
// summary, with result.json written again; then the clean-up and the exit code. A refused final
// write, returned or panicked, prints no console and writes no summary, and the outputs still say
// the verdict that the exit code gives (section 8); the run's warnings are then told as lines alone,
// in the clean-up. A panic after the final file was written is a warning: the verdict was decided
// already. The clean-up runs on every path, so that a report that cannot be written does not leave
// a cluster behind.
func (s *runState) finish() (code int) {
	refused := false
	written := false // the final result.json is in place
	defer func() {
		s.cleanUpQuietly()
		if !refused {
			code = s.decision.Verdict.ExitCode()
		}
	}()
	defer func() {
		p := recover()
		if p == nil {
			return
		}
		if written {
			s.warnAfterConsole("the tool failed after the verdict was decided: " + panicText(p))
			return
		}
		refused = true
		s.println(s.o.Stderr, "rollproof: cannot write results: "+panicText(p))
		code = s.refusedWrite()
	}()
	// A refused final write comes before completeEvidence, which closes probe.log: it is closed
	// here then, so that its held-back tail reaches the file all the same.
	defer s.closeProbeLog()
	rec, n := s.recordAndWarnings()
	if err := s.writeFinal(rec, n); err != nil {
		refused = true
		s.println(s.o.Stderr, "rollproof: cannot write results: "+err.Error())
		return s.refusedWrite()
	}
	written = true
	s.finalWritten = true
	s.writeOutputs(report.Output{Name: "verdict", Value: rec.Verdict})
	masked := report.Mask(rec, s.mask)
	s.appendSummary(report.Summary(masked))
	fmt.Fprint(s.o.Stdout, report.Console(masked))
	s.warningsTold = n
	s.completeEvidence()
	s.lateWarnings()
	return s.decision.Verdict.ExitCode()
}

// refusedWrite gives the exit code of a run whose final result.json could not be written, 1 for a
// fail that was already decided and 2 otherwise, and writes the verdict that code stands for as the
// step's output: fail or not-verified (section 8).
func (s *runState) refusedWrite() int
```

- `lateWarnings` becomes:

```go
// lateWarnings tells the warnings of the run that nothing has told yet, in order, each as a line
// "warning: TEXT" of standard output. When the final result.json was written, those are the ones
// that came after the console was printed: each is also appended, masked, as a line of the job
// summary, and result.json is then written again, once, with all of them. When the final write was
// refused, no console was printed, so they are every warning of the run: they are told as lines
// alone, with no summary and no second try of result.json, so that the first file, the step's
// output and the exit code say the same (section 8; decision 6). A warning that arises while it
// tells them, such as that the summary could not be written, is told in the same call.
func (s *runState) lateWarnings() {
	told := false
	for {
		all := s.warnings.all()
		if len(all) <= s.warningsTold {
			break
		}
		for _, text := range all[s.warningsTold:] {
			s.println(s.o.Stdout, "warning: "+text)
			if s.finalWritten {
				s.appendSummary(report.SummaryLine("warning: " + s.mask.String(text)))
			}
		}
		s.warningsTold = len(all)
		told = true
	}
	if told && s.finalWritten {
		s.rewriteFinal()
	}
}
```

- `warnAfterConsole` becomes:

```go
// warnAfterConsole adds a warning that arose once the console text was printed, or once the final
// result.json was refused, and tells it as lateWarnings tells one.
func (s *runState) warnAfterConsole(text string) {
	s.warnings.add(text)
	s.lateWarnings()
}
```

- `Run` ends, in place of `return s.finish()`:

```go
	code := s.finish()
	if !s.finalWritten {
		// After a refused final write completeEvidence did not run, so windows.json is written a last
		// time here, as before, and a warning that this gives is told as a line, as the others were.
		s.marks.end()
		s.lateWarnings()
	}
	return code
```

Today's `warnAfterConsole` (`internal/run/run.go:1177-1181`) calls `rewriteFinal` on every path, so, with this step's outputs, a later warning after a refused final write could write a final `result.json` that the exit code and the step's output contradict, once the folder could be written again. The existing tests of a refused write, read at `58c1cbf`, and what each needs: none asserts that second try. `TestARefusedFinalKeepsTheVerdictsClass` (`internal/run/run_test.go:1653`) refuses the write at the probe's stop, and its cluster's delete succeeds; its results folder is gone, so the last write of `windows.json`, which `Run` makes after the clean-up, fails, and its two rows, which wanted stdout empty, want exactly that one warning line (`isWindowsWarningAlone`); `TestAPanicInTheFinalWriteIsARefusedWrite` (`:1775`) and `TestARefusedFinalWriteClosesTheProbeLog` (`:3339`) call `finish` with no cluster, no watcher and no signal, so `warnAfterConsole` is never reached; `TestResultsFolderCannotBeWritten` (`:370`) ends before `finish`, when `report.Open` fails. The new `TestAWarningAfterARefusedFinalWriteWritesNoFinalFile` (Step 1) is the first test to reach `warnAfterConsole` after a refused write.

- New:

```go
// writeOutputs appends outs to the step's outputs through s.gh. A write that fails is a warning,
// outputsNotWritten and its error, once in a run: later failures add none.
func (s *runState) writeOutputs(outs ...report.Output)

// appendSummary appends text to the step's job summary through s.gh. A write that fails is a
// warning, summaryNotWritten and its error, once in a run: later failures add none, so that a
// summary that cannot be written does not warn again for each warning appended to it.
func (s *runState) appendSummary(text string)
```

`cleanUp` keeps its call of `lateWarnings` after the watcher's stop, and its call of `warnAfterConsole` for a cluster that was not deleted.

The tests that touch the order of the output, read at `58c1cbf`, and what each needs: `TestWarningAtTheWatchersStop` (the console's last line is a warning of the watcher's stop: holds, the stop comes in the clean-up); `TestWarningBetweenTheConsoleAndTheCleanUp` (its hook fires at the console's text, now before the evidence is completed: holds, and its comment becomes "… after the last result.json was written and the console printed, and before the evidence is completed and the clean-up begins …"); `TestEvidenceFailuresAreWarnings` (wants `result.json`'s warnings and stdout to begin with the console: holds, the warnings are now lines after it); `TestEvidenceThatCannotBeOpenedIsAWarning` (the order of four warnings in `result.json`: holds); `TestTimelineAndEventsAreWritten` (the events listed after `end-commands`, before `watch-stop` and `delete`: holds); `TestPasswordsAreMaskedEverywhere` (gains both files, below). No test asserts a warning of the evidence inside the console's block, and no test of `e2e` reads the order of the console's lines (`readConsole` is read with `strings.Contains` only).

- [ ] **Step 1: Write the failing tests**

In `fakes_test.go`: the `world` gains `env map[string]string`, which `w.run` gives the run as `Getenv: func(k string) string { return w.env[k] }`; `fakeWatcher.WriteEvents` runs the hook `"write-events"` right after it keeps its call log; and three helpers:

```go
// gitHubFiles makes two empty files, output and summary, in a folder of their own, names them to the
// run as GITHUB_OUTPUT and GITHUB_STEP_SUMMARY with GITHUB_ACTIONS true, and returns their paths.
func (w *world) gitHubFiles(t *testing.T) (output, summary string)

// readOutputs reads the outputs file at path, in the delimiter form and in the plain form
// NAME=VALUE, and returns each name's values in the order they were written. A line that is
// neither fails the test.
func readOutputs(t *testing.T, path string) map[string][]string

// gitHubRecorder stands for GitHub's files: it adds each write to the world's call log, as
// "outputs NAMES FINALNESS" (the names joined by commas) or "summary FINALNESS", FINALNESS being
// "final" when result.json was final at that moment and "not final" otherwise.
type gitHubRecorder struct{ w *world }
```

In `run_test.go`:

```go
func TestTheOutputsAtBothMoments(t *testing.T) {
	// w.gitHubFiles; the hook "load" reads the outputs.
	// At "load": {"out": [w.out], "verdict": ["not-verified"]}.
	// After the run (exit 0): {"out": [w.out], "verdict": ["not-verified", "pass"]}.
}

func TestWhatVerdictSaysAfterEachEnding(t *testing.T) {
	// Each row: newWorld, w.gitHubFiles, the row's set-up, w.run. It wants Run's return, the
	// outputs' verdict values in order (none for the last row), and whether the summary file holds
	// a summary (begins "### rollproof: ") or is empty.
	//   "a pass"                      nothing                                 0  [not-verified pass]          summary
	//   "a fail"                      failureAt(20s, 20s) added to the probe  1  [not-verified fail]          summary
	//   "not verified"                failureAt(5s, 5s) added (steady)        2  [not-verified not-verified]  summary
	//   "a rejected configuration"    w.loadErr = verdict.Condition{Code: verdict.ConfigRejected,
	//                                 Text: "version: must be 1"}             2  [not-verified not-verified]  summary
	//   "a signal during the deploy"  as TestSignalDuringDeploy               2  [not-verified not-verified]  summary
	//   "a signal after the deploy failed"
	//                                 as TestSignalAfterDeployFailed          1  [not-verified fail]          summary
	//   "a refused final write after a fail"
	//                                 failureAt(20s, 20s); the hook "end-commands" removes w.out
	//                                                                         1  [not-verified fail]          empty
	//   "a refused final write, no fail"
	//                                 the hook "end-commands" removes w.out   2  [not-verified not-verified]  empty
	//   "a signal and a refused final write after a fail"
	//                                 as TestSignalAfterDeployFailed, and the hook "end-commands"
	//                                 removes w.out                           1  [not-verified fail]          empty
	//   "a failure of the tool"       the hook "deployer" panics with "the deployer broke"
	//                                                                         2  [not-verified not-verified]  summary
	//   "a results folder that cannot be opened"
	//                                 w.out is a file, as in TestResultsFolderCannotBeWritten
	//                                                                         2  none                          empty
	// For the three refused rows, stdout is exactly one line, the warning that windows.json was not
	// written (isWindowsWarningAlone); for the row with no verdict values, the outputs file is
	// empty. After a signal Run returns the code of the
	// verdict, and the process then ends by the signal (TestASignalEndsTheToolByItAfterARefusedWrite).
}

func TestNothingIsWrittenForGitHubWithoutItsVariables(t *testing.T) {
	// Three rows, each with two empty files made in a folder of their own:
	//   GITHUB_OUTPUT and GITHUB_STEP_SUMMARY name them, GITHUB_ACTIONS is not set: both stay empty.
	//   GITHUB_ACTIONS "true" and GITHUB_OUTPUT only: the outputs are
	//     {"out": [w.out], "verdict": ["not-verified", "pass"]}; the other file stays empty.
	//   GITHUB_ACTIONS "true" and GITHUB_STEP_SUMMARY only: the summary begins
	//     "### rollproof: PASS"; the other file stays empty.
	// Each run exits 0.
}

func TestAGitHubFileThatCannotBeWrittenIsAWarning(t *testing.T) {
	// GITHUB_ACTIONS "true"; GITHUB_OUTPUT names a path in t.TempDir() that does not exist;
	// GITHUB_STEP_SUMMARY names a folder. Exit 0; result.json's verdict is "pass" and its warnings
	// are exactly two, the first beginning "the step's outputs were not written: " and the second
	// "the job summary was not written: "; stdout holds each of the two prefixes exactly once; the
	// outputs' path still does not exist.
}

func TestTheSummaryIsAppendedOnce(t *testing.T) {
	// w.gitHubFiles; the summary file holds "earlier step\n" before the run. Exit 0; the file
	// begins "earlier step\n### rollproof: PASS", holds "### rollproof" exactly once, and ends
	// "results: " + w.out + "\n```\n". The fence is three backticks here because no line of that
	// block holds a backtick: w.out is a path the test made.
}

func TestALateWarningIsALineOfTheSummary(t *testing.T) {
	// w.gitHubFiles; w.watcher.eventsErr = errors.New("the list broke");
	// w.cluster.deleteErr = errors.New("the engine did not answer"). Exit 0. The summary ends
	// "\n```\nwarning: the events were not written: the list broke\n```\n" +
	// "\n```\nwarning: the cluster was not deleted: the engine did not answer\n```\n";
	// stdout's last two lines are those two warnings, in that order; result.json's warnings are
	// exactly those two texts, in that order.
}

func TestTheResultsComeBeforeTheEvidence(t *testing.T) {
	// gitHubOf replaced, for the test, by one that gives &gitHubRecorder{w}; w.onStdout adds
	// "console" to the call log at the first write that holds "results: ". Exit 0.
	// In the call log: "outputs out,verdict not final" comes before "load"; then, in this order,
	// "outputs verdict final", "summary final", "console", with nothing else between them; and
	// w.watcher.eventsCalls (the log when the events were listed, the last step of the evidence)
	// ends with "console".
}

// A warning after a refused final write is a line of its own, and result.json is not written
// again: the file, the step's output and the exit code say the same (decision 6).
func TestAWarningAfterARefusedFinalWriteWritesNoFinalFile(t *testing.T) {
	// w.gitHubFiles; the hook "end-commands" removes w.out, so that the final write is refused, as
	// in the refused rows of TestWhatVerdictSaysAfterEachEnding; the hook "delete" makes w.out
	// again (os.MkdirAll, mode 0o700), before the clean-up's warning arises;
	// w.cluster.deleteErr = errors.New("the engine did not answer").
	// Exit 2; stdout holds the line "warning: the cluster was not deleted: the engine did not
	// answer" exactly once; the outputs' verdict values are [not-verified not-verified]; w.out
	// holds no final result.json (none, or one that is not final).
}

// After a refused final write, a warning that arises later, here at the watcher's stop, is still
// told once as a line of its own; no console block is printed, no summary written, and result.json
// is not written again (section 8; decision 6).
func TestAWarningAtTheWatchersStopAfterARefusedFinalWriteIsALine(t *testing.T) {
	// w.gitHubFiles; the hook "watcher" opens the evidence pods/orders-a-1.log; the hook
	// "end-commands" removes w.out; the hook "watch-stop" closes that file and writes to it.
	// Exit 2; stdout holds exactly one line that begins "warning: the evidence pods/orders-a-1.log
	// could not be written: ", and neither "rollproof: " nor "results: "; the summary file is empty;
	// the outputs' verdict values are [not-verified not-verified]; w.out holds no final result.json.
}

// After a refused final write, the warning that the outputs could not be written at the first
// moment is still told, once, so that an empty verdict is explained (section 8).
func TestAnOutputsWarningAfterARefusedFinalWriteIsALine(t *testing.T) {
	// GITHUB_ACTIONS "true", GITHUB_OUTPUT a path that does not exist; the hook "end-commands"
	// removes w.out. Exit 2; stdout holds exactly one line that begins "warning: the step's
	// outputs were not written: ".
}
```

`TestPasswordsAreMaskedEverywhere` calls `w.gitHubFiles(t)` before the run, adds the contents of both files to `texts` under the names `GITHUB_OUTPUT` and `GITHUB_STEP_SUMMARY`, so that no form of either password may be in them, and wants the summary to hold "warning: the cluster was not deleted: the engine said ***".

In `cmd/rollproof/main_test.go`, a fake run `fakeRefusedFail` (`"refused-fail"`) for `TestHelperProcess`: it prints "started", waits for its context, prints "returned" and returns 1, the code of a decided fail whose final `result.json` could not be written. And:

```go
// After a signal, the process ends by it also when the run returned 1, as a run does whose final
// result.json could not be written after a fail was decided (section 8; section 10, "On a signal").
func TestASignalEndsTheToolByItAfterARefusedWrite(t *testing.T) {
	// startChild(t, fakeRefusedFail); waitFor "started"; SIGTERM; the child ends within 10 s, by
	// SIGTERM, and its stdout holds "returned".
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/run/ ./cmd/rollproof/ -run 'TestTheOutputsAtBothMoments|TestWhatVerdictSaysAfterEachEnding|TestNothingIsWrittenForGitHubWithoutItsVariables|TestAGitHubFileThatCannotBeWrittenIsAWarning|TestTheSummaryIsAppendedOnce|TestALateWarningIsALineOfTheSummary|TestTheResultsComeBeforeTheEvidence|TestAWarningAfterARefusedFinalWriteWritesNoFinalFile|TestPasswordsAreMaskedEverywhere|TestASignalEndsTheToolByItAfterARefusedWrite' -v`
Expected: FAIL: `gitHubOf` and `gitHubFiles` are not defined in `internal/run`. `TestASignalEndsTheToolByItAfterARefusedWrite` passes already: it pins `endProcess`'s rule for this case.

- [ ] **Step 3: Implement the changes above, each named one with its interface comment**

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/run/ ./cmd/rollproof/ -v`, `go test -race ./internal/run/ ./cmd/rollproof/`, then `gofmt -l .`, `go test ./...` and the four vets.
Expected: PASS, no race, no output from `gofmt` and the vets.

- [ ] **Step 5: Run the first fixture once**

Run: `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags e2e -p 1 -count=1 ./e2e/ -run '^TestFirstFixture$/^helm-3$' -v -timeout 30m`
Expected: PASS. Its `console.txt` in `/tmp/rollproof-e2e-results/TestFirstFixture-helm-3` (under the system's temporary folder) begins with the console's first line and holds its results line, which is its last line unless a warning came after the console; the run, which has no GitHub variables here, writes no outputs and no summary. Afterwards `nerdctl ps -a --format "{{.Names}}"` prints no name that begins `rollproof-e2e-`.

- [ ] **Step 6: Commit**

```
git add internal/run/run.go internal/run/run_test.go internal/run/fakes_test.go cmd/rollproof/main_test.go
git commit -m "Write the outputs at both moments and the job summary, print the console before the evidence, and tell later warnings as lines of both"
```
