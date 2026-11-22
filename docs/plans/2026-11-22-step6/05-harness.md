# Task 7: the suite's harness

Read section 12 ("The suite's matrix", "The suite's workflow", "The mirror job", the fourteenth row and its control), section 14, point 32 (the bullet "The suite's workflow"), and decisions 19 to 21 of the README, before this task.

### Task 7: GitHub's variables kept out, files of their own for the first fixture and the mirror job, and the Helm major by a variable

**Files:**
- Create: `e2e/runenv_test.go` (no build tag, so that `go test ./...` runs its tests)
- Modify: `e2e/harness_test.go` (`majors`; `TestMain`; `runFixture`; new `checkGitHubFiles`), `e2e/config_test.go` (`fixture.github`), `e2e/fixtures_test.go` (`underBothHelms` becomes `underEachHelm`; `TestFirstFixture`; the comment of `TestChartDeniesTrafficByDefault`), `e2e/custom_test.go` and `e2e/commands_test.go` (the calls of `underEachHelm`), `e2e/mirror_helpers_test.go` (`throughTheMirror`), `e2e/mirror_test.go` (`TestMirror`; `holdRunImages`)

**Interfaces:**
- Consumes: the outputs file and the summary file as the README's "Interfaces between tasks" gives them (Tasks 4 to 6).
- Produces, in `runenv_test.go`:
  - `const helmVar = "ROLLPROOF_E2E_HELM"`
    helmVar names the variable from which the harness takes the Helm majors to run:
    3 or 4 for one, both when it is not set or empty (section 12, "The suite's
    workflow").
  - `func helmMajors(value string) ([]string, error)`
    helmMajors gives the Helm binaries under .tools/ that value selects: helm-3 for
    "3", helm-4 for "4", both, in that order, for "". Any other value is an error
    that names the variable and the values it takes.
  - `type githubFiles struct { output, summary string }`
    githubFiles is a run's own files for GitHub's outputs and job summary, in its
    results folder.
  - `func runEnv(base []string, helmDir string, gh *githubFiles) []string`
    runEnv is the environment of one run of the tool: base without GITHUB_OUTPUT
    and GITHUB_STEP_SUMMARY, so that the run never writes into the files of the job
    that runs the suite; when gh is not nil, also without GITHUB_ACTIONS, and then
    with GITHUB_ACTIONS=true and the two variables naming gh's files; and, last,
    PATH with helmDir in front of base's own PATH.
  - `func readGitHubOutputs(path string) (map[string][]string, error)`
    readGitHubOutputs reads an outputs file in the delimiter form and in the plain
    form NAME=VALUE, and gives each name's values in the order they were written. A
    line that is neither, or a delimiter that never comes, is an error that names
    the file and the line.
- Produces, in `harness_test.go`:
  - `var majors []string`, set by `TestMain` from `helmMajors(os.Getenv(helmVar))`.
  - `func checkGitHubFiles(t *testing.T, out, word, value string) (summary string)`
    checkGitHubFiles fails the test unless the run's GitHub files in out say what a
    run whose verdict is value (pass, fail or not-verified), and whose first line
    writes word (PASS, FAIL or NOT VERIFIED), writes: the output out once, as out,
    and verdict as not-verified and then value; and a summary whose first line
    begins "### rollproof: " + word and which ends with the line "results: " + out
    and the fence that closes its block. It returns the summary.
- Produces, in `config_test.go`: the field `github bool` of `fixture`, with the comment "github gives the run its own files for GitHub's outputs and job summary, github-output and github-summary in its results folder, with GITHUB_ACTIONS true; every other run gets none (section 12)."
- Produces, in `fixtures_test.go`: `func underEachHelm(t *testing.T, f fixture, check func(t *testing.T, r result))`
  underEachHelm runs f once with each Helm major that ROLLPROOF_E2E_HELM selects
  (majors), each as a subtest named after its binary, helm-3 or helm-4, and hands
  each result to check.

- [ ] **Step 1: Write the failing tests**

In `runenv_test.go`:

```go
func TestHelmMajors(t *testing.T) {
	// helmMajors("")  == ["helm-3", "helm-4"], nil
	// helmMajors("3") == ["helm-3"], nil
	// helmMajors("4") == ["helm-4"], nil
	// helmMajors("5") gives the error `ROLLPROOF_E2E_HELM is "5": it must be 3, 4 or not set`,
	// and so does helmMajors("helm-3") with its own value quoted.
}

func TestRunEnvKeepsTheJobsGitHubFilesOut(t *testing.T) {
	base := []string{"HOME=/home/u", "PATH=/usr/bin:/bin", "GITHUB_ACTIONS=true",
		"GITHUB_OUTPUT=/runner/out", "GITHUB_STEP_SUMMARY=/runner/sum", "CI=true"}
	// runEnv(base, "/helm", nil) == ["HOME=/home/u", "PATH=/usr/bin:/bin", "GITHUB_ACTIONS=true",
	//   "CI=true", "PATH=/helm:/usr/bin:/bin"]
	// runEnv(base, "/helm", &githubFiles{"/r/github-output", "/r/github-summary"}) ==
	//   ["HOME=/home/u", "PATH=/usr/bin:/bin", "CI=true", "GITHUB_ACTIONS=true",
	//    "GITHUB_OUTPUT=/r/github-output", "GITHUB_STEP_SUMMARY=/r/github-summary",
	//    "PATH=/helm:/usr/bin:/bin"]
	// runEnv([]string{"HOME=/h"}, "/helm", nil) == ["HOME=/h", "PATH=/helm:"]
	// base is not changed by any of the three calls.
}

func TestReadGitHubOutputs(t *testing.T) {
	// A file "out<<D1\n/tmp/r\nD1\nverdict<<D2\nnot-verified\nD2\nverdict=pass\n" gives
	//   {"out": ["/tmp/r"], "verdict": ["not-verified", "pass"]}.
	// A file "x<<E\na\nb\nE\n" gives {"x": ["a\nb"]}.
	// A file "good=1\nno separator\n" gives an error that holds the file's path and "line 2".
	// A file "x<<E\na\n" gives an error that holds the file's path and "E".
	// An empty file gives an empty map and no error.
}
```

In `fixtures_test.go`, `TestFirstFixture` gives its fixture `github: true`, and its check, after what it asserts today, calls `sum := checkGitHubFiles(t, outDir(t), "PASS", "pass")` and wants `sum` to hold the line `| window | length | checks | failed | after finish | slowest | behind | result |` and the line `| transition | workload | probed | pods replaced | quickest stop |`.

In `mirror_test.go`, after each run of `TestMirror` and before `checkPasswordGone`: `checkGitHubFiles(t, outDir(t), "PASS", "pass")` in "through the mirror", and `checkGitHubFiles(t, outDir(t), "FAIL", "fail")` in "a mirror replaces its host"; so the search for the password, which reads every file of the results folder, reads two files that are not empty.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./e2e/ -run 'TestHelmMajors|TestRunEnvKeepsTheJobsGitHubFilesOut|TestReadGitHubOutputs' -v`, then `go vet -tags e2e ./...` and `go vet -tags mirror ./...`
Expected: FAIL to compile: `helmMajors`, `runEnv`, `githubFiles` and `readGitHubOutputs` are not defined; the vets fail on `fixture.github` and `checkGitHubFiles`.

- [ ] **Step 3: Implement, each named one with its interface comment from the Interfaces block**

- `runenv_test.go`: the five names above.
- `harness_test.go`: `TestMain` first sets `majors`, before anything else it does; a `helmMajors` error prints `e2e: ` and the error to standard error and exits 1. It still checks both Helm binaries and both majors, with the comment "Both, whatever ROLLPROOF_E2E_HELM selects: the fourteenth row's control runs under Helm 3 in every run of the suite." `runFixture`, once the results folder is made: for a fixture with `github`, `gh := &githubFiles{filepath.Join(out, "github-output"), filepath.Join(out, "github-summary")}`, both written empty with mode 0644, a failure being `t.Fatalf`; then `env := runEnv(os.Environ(), helmDir, gh)` in place of `helmEnv(helmDir)` (gh is nil for every other fixture). `helmEnv` stays for `checkHelmMajor`. `runFixture`'s comment gains: "The tool never sees the job's GITHUB_OUTPUT or GITHUB_STEP_SUMMARY; a fixture with github gets files of its own."
- `checkGitHubFiles`: reads both files with `readGitHubOutputs` and `os.ReadFile`; wants the outputs exactly `{"out": [out], "verdict": ["not-verified", value]}`; wants the summary's first line to begin `"### rollproof: " + word` and the summary to end `"results: " + out + "\n```\n"`.
- `fixtures_test.go`: `underBothHelms` becomes `underEachHelm`, over `majors`; every call in `fixtures_test.go`, `custom_test.go` and `commands_test.go` follows (32 calls; `grep -rn underBothHelms e2e` prints nothing afterwards). In the comment of `TestChartDeniesTrafficByDefault`, the sentence "The control runs once, under Helm 3: it tests the cluster's network plugin, not Helm." becomes, exactly: "The control runs in every run of this test, under Helm 3, whatever ROLLPROOF_E2E_HELM selects: it tests the cluster's network plugin, not Helm, and each job of the suite's workflow runs on its own runner image and shows there that the plugin enforces the policy." Its code does not change.
- `mirror_helpers_test.go`: `throughTheMirror` sets `github: true` (and so `mirrorReplacesItsHost`, which starts from it); its comment adds "with files of its own for GitHub's outputs and summary, which the search for the password reads".
- `mirror_test.go`: the checks of Step 1; and `holdRunImages` runs the engine's `image inspect` under a limit of 1 minute and its `pull` under a limit of 10 minutes (`exec.CommandContext` with `context.WithTimeout`), a pull cut at its limit failing the test with "ENGINE pull REF did not end within 10m0s" (a leftover of step 5, its Task 13's m5; see Task 12's table).

- [ ] **Step 4: Run the unit tests and the vets**

Run: `go test ./e2e/ -v -run 'TestHelmMajors|TestRunEnvKeepsTheJobsGitHubFilesOut|TestReadGitHubOutputs'`, `gofmt -l .`, `go test ./...` and the four vets.
Expected: PASS, no output from `gofmt` and the vets.

- [ ] **Step 5: Run on this machine**

Each command alone; after each, `nerdctl ps -a --format "{{.Names}}"` prints no name that begins `rollproof-e2e-` or `rollproof-mirrorjob-`.

1. `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags e2e -p 1 -count=1 ./e2e/ -run '^TestFirstFixture$' -v -timeout 40m`
   Expected: PASS, with the subtests `helm-3` and `helm-4`; each results folder holds `github-output` and `github-summary`.
2. `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 ROLLPROOF_E2E_HELM=4 go test -tags e2e -p 1 -count=1 ./e2e/ -run '^TestChartDeniesTrafficByDefault$' -v -timeout 40m`
   Expected: PASS, with the subtests `control` and `helm-4`, and no `helm-3`.
3. `env ROLLPROOF_E2E_HELM=5 go test -tags e2e -p 1 -count=1 ./e2e/ -run '^TestFirstFixture$'`
   Expected: FAIL at once, before any build, with `e2e: ROLLPROOF_E2E_HELM is "5": it must be 3, 4 or not set`.
4. `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags mirror -p 1 -count=1 ./e2e/ -run TestMirror -v -timeout 60m`
   Expected: PASS for both subtests.

**Stop rule.** A run that fails for a reason this task did not touch (kind's missed log line after four attempts, an engine command that hangs) is reported to the maintainer with its results folder, and is not run again without the maintainer's word; the expected results are never changed to make it pass.

- [ ] **Step 6: Commit**

```
git add e2e/runenv_test.go e2e/harness_test.go e2e/config_test.go e2e/fixtures_test.go e2e/custom_test.go e2e/commands_test.go e2e/mirror_helpers_test.go e2e/mirror_test.go
git commit -m "Keep the job's GitHub files out of the suite's runs, give the first fixture and the mirror job files of their own, and choose the Helm major by ROLLPROOF_E2E_HELM"
```
