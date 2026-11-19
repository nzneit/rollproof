# Step 4 plan, part 2: the commands driver, a Helm driver with no `previous`, and `setup`

> Part of the plan in `README.md` of this folder. Read that file first: its Global Constraints, its decisions and its "Interfaces between tasks" apply to every task here.

Every test in this part is a unit test of `internal/deploy`, which carries `//go:build linux`. The fake runner is `fakeRunner` of `internal/deploy/fake_test.go`; the real one is `newHarness` of `internal/deploy/command_test.go`, whose commands start in a new folder and write their evidence into a second one.

### Task 2: The commands driver

Design: section 7, "The contract": "Transitions" (the table), "Where commands run", "The commands driver", "When a command fails", "When a command runs too long"; section 10, "What a run writes", the `result.json` item on releases ("The commands driver gives the first word of each command"); README decision 6.

**Files:**
- Create: `internal/deploy/commands.go`
- Modify: `internal/deploy/deploy.go` (`Release.Program`; the comment of `Release`)
- Test: new `internal/deploy/commands_test.go`

**Interfaces:**
- Consumes: `config.Commands{Install, Deploy, Rollback []string}` (as built: `Deploy` and `Rollback` nil when absent); `Runner`, `Command`, `Result` of `internal/deploy/command.go` as built.
- Produces: `type Release struct { Role, Chart, Version, Source string; ValueFiles, InlineKeys []string; Program string }`
  What the report says of one release. For the Helm driver Role is previous
  or candidate, Chart and Version are the chart's own name and version,
  Source is where it came from, ValueFiles are file names and InlineKeys the
  top-level keys of inline maps, and Program is empty. For the commands
  driver Role is install, deploy or rollback, Program is the first word of
  that command as written, and the rest is empty. Never a value.
- Produces: `type CommandsOptions struct { Commands config.Commands; Timeout time.Duration; Runner Runner }`
  What the commands driver needs: the configuration's commands, as Resolve
  left them; limits.timeout, which bounds each command; and the run's runner.
- Produces: `func NewCommands(o CommandsOptions) *CommandsDriver`
  Returns the commands driver, a Deployer. Fetch does nothing. Install,
  Deploy and Rollback each run their one command, named install, deploy or
  rollback, and return its Result unchanged. Transitions follow section 7's
  table; Releases give one Release for each command present, in the order
  install, deploy, rollback. Deploy or Rollback with no such command panics.

- [ ] **Step 1: Write the failing tests**

  ```go
  var _ Deployer = (*CommandsDriver)(nil)

  // allThree names the three commands of section 7's example, with a rollback by a script.
  func allThree() config.Commands {
  	return config.Commands{
  		Install:  []string{"kubectl", "apply", "-f", "rendered/previous.yaml"},
  		Deploy:   []string{"kubectl", "apply", "-f", "rendered/candidate.yaml"},
  		Rollback: []string{"./rollback.sh", "orders"},
  	}
  }

  func TestCommandsCommandLines(t *testing.T) {
  	f := newFake()
  	d := NewCommands(CommandsOptions{Commands: allThree(), Timeout: 5 * time.Minute, Runner: f})
  	if err := d.Fetch(t.Context()); err != nil || len(f.calls) != 0 {
  		t.Fatalf("Fetch: %v and %d commands, want nil and none", err, len(f.calls))
  	}
  	// Install, Deploy and Rollback each return a Result whose Name is install, deploy, rollback.
  	want := []Command{
  		{Name: "install", Args: allThree().Install, Timeout: 5 * time.Minute},
  		{Name: "deploy", Args: allThree().Deploy, Timeout: 5 * time.Minute},
  		{Name: "rollback", Args: allThree().Rollback, Timeout: 5 * time.Minute},
  	}
  	// cmp.Diff(want, f.calls) is empty.
  }

  func TestCommandsTransitions(t *testing.T) {
  	// all three:            []verdict.WindowName{verdict.Deploy, verdict.Rollback}
  	// install and deploy:   []verdict.WindowName{verdict.Deploy}
  	// install only:         []verdict.WindowName{verdict.Restart}
  }

  func TestCommandsReleases(t *testing.T) {
  	// all three: []Release{{Role: "install", Program: "kubectl"},
  	//   {Role: "deploy", Program: "kubectl"}, {Role: "rollback", Program: "./rollback.sh"}}
  	// install only: []Release{{Role: "install", Program: "kubectl"}}
  	// Releases is the same before and after Fetch.
  }

  func TestCommandsEndingsPassThrough(t *testing.T) {
  	// For each of StoppedAtLimit (deploy), ExitedNonZero with ExitCode 2 (install),
  	// NotStarted (rollback) and StoppedBySignal (deploy), scripted in f.results with
  	// Began and Ended set: the step returns that Result unchanged (cmp.Diff empty).
  }

  func TestCommandsProgrammingErrors(t *testing.T) {
  	d := NewCommands(CommandsOptions{Commands: config.Commands{Install: []string{"true"}}, Timeout: time.Minute, Runner: newFake()})
  	mustPanic(t, "deploy command", func() { d.Deploy(t.Context()) })
  	mustPanic(t, "rollback command", func() { d.Rollback(t.Context()) })
  }

  // Section 12: the variables of section 7 are gone, and the command starts in the configuration's folder.
  func TestCommandsRunInTheFolderWithoutHelmVariables(t *testing.T) {
  	env := append(os.Environ(), "HELM_KUBEAPISERVER=https://elsewhere.example:6443", "HELM_DRIVER=sql")
  	h := newHarness(t, env)
  	d := NewCommands(CommandsOptions{Commands: config.Commands{Install: []string{"sh", "-c", "pwd -P; env"}},
  		Timeout: time.Minute, Runner: h.runner})
  	if r := d.Install(t.Context()); r.Ending != Succeeded {
  		t.Fatalf("install ended %s", r.Ending)
  	}
  	out := h.read(t, "install")
  	// out holds the line that filepath.EvalSymlinks(h.dir) gives, the line KUBECONFIG=/run/rollproof-test/kubeconfig,
  	// and neither HELM_KUBEAPISERVER= nor HELM_DRIVER=; h.runner.Removed() is {"HELM_DRIVER", "HELM_KUBEAPISERVER"}.
  }
  ```

- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./internal/deploy/ -run 'TestCommands' -v` / Expected: FAIL to build: `NewCommands`, `CommandsOptions` and `Release.Program` are not defined.

- [ ] **Step 3: Implement `NewCommands` and `*CommandsDriver` in `internal/deploy/commands.go`, and add `Program` to `Release` in `internal/deploy/deploy.go`**, with the interface comments from the Interfaces block. Each step calls `o.Runner.Run(ctx, Command{Name: …, Args: …, Timeout: o.Timeout})`. The panic texts are "deploy: Deploy called with no deploy command" and "deploy: Rollback called with no rollback command". Add one line to the comment of `HelmDriver.Releases`: Program is empty.

- [ ] **Step 4: Run the tests to see them pass**
  Run: `go test ./internal/deploy/ -v` / Expected: PASS.

- [ ] **Step 5: Commit**
  `git add internal/deploy/commands.go internal/deploy/commands_test.go internal/deploy/deploy.go internal/deploy/helm.go` and `git commit -m "Add the commands driver, which runs the caller's install, deploy and rollback commands and derives its transitions from them"`

### Task 3: A Helm driver with no `previous`

Design: section 4, "A run with no previous release"; section 7, "Transitions" (Helm: `candidate` only installs `candidate` and gives restart), "The Helm driver", "Charts are fetched first"; README Interfaces between tasks (`deploy.NewHelm` with `o.Helm.Previous == nil`).

**Files:**
- Modify: `internal/deploy/helm.go` (`NewHelm`, `Fetch`, `Install`, `Deploy`, `Rollback`, `Transitions`, `Releases` and their comments)
- Test: `internal/deploy/helm_test.go` (new tests; `TestHelmProgrammingErrors`)

**Interfaces:**
- Consumes: `config.Helm.Previous` (nil for a run with no previous release, as built).
- Produces: `func NewHelm(o HelmOptions) *HelmDriver`
  Returns the Helm driver for o. With no previous release in o.Helm, Fetch
  fetches the candidate only, Install installs the candidate, Transitions is
  restart alone, and Releases is the candidate alone; Deploy and Rollback
  then panic, because run never calls them for a restart. With a previous
  release it is as before.

- [ ] **Step 1: Write the failing tests**

  ```go
  // candidateOnly is helmConfig with no previous release: its candidate is the chart path /work/app/chart.
  func candidateOnly() config.Helm {
  	h := helmConfig()
  	h.Previous = nil
  	return h
  }

  func TestHelmWithNoPreviousCommandLines(t *testing.T) {
  	f := scripted()
  	drv, runDir := newDriver(t, candidateOnly(), f)
  	// Fetch: nil. Install: Name "install", Ending Succeeded.
  	cl := []string{"--namespace", "orders", "--kubeconfig", kubeconfig, "--kube-context", kubeContext}
  	want := []Command{
  		{Name: "show-candidate", Args: []string{"helm", "show", "chart", "/work/app/chart"}, Timeout: timeout},
  		{Name: "install", Args: slices.Concat([]string{"helm", "upgrade", "--install", "orders", "/work/app/chart"}, cl,
  			[]string{"--timeout", "5m0s", "--values", "/work/app/test/values-kind.yaml",
  				"--values", filepath.Join(runDir, "values", "candidate-2.yaml")}), Timeout: timeout},
  		{Name: "status-install", Args: slices.Concat([]string{"helm", "status", "orders"}, cl, []string{"-o", "json"}),
  			Timeout: timeout, MaxStdout: 64 << 20},
  	}
  	// cmp.Diff(want, f.calls) is empty; no command is named pull-previous or show-previous.
  }

  func TestHelmWithNoPreviousRemoteCandidate(t *testing.T) {
  	h := candidateOnly()
  	h.Candidate = &config.Release{Chart: "oci://registry.example/charts/orders", Version: "1.5.0"}
  	f := scripted()
  	f.effects["pull-candidate"] = writeArchive("orders", "1.5.0")
  	drv, runDir := newDriver(t, h, f)
  	// Fetch: nil; f.names() is {"pull-candidate", "show-candidate"}; Install's command line names
  	// filepath.Join(runDir, "charts", "candidate", "orders-1.5.0.tgz").
  }

  func TestHelmWithNoPreviousTransitionsAndReleases(t *testing.T) {
  	drv, _ := newDriver(t, candidateOnly(), scripted())
  	// Transitions: []verdict.WindowName{verdict.Restart}, before Fetch too.
  	// After Fetch, Releases: []Release{{Role: "candidate", Chart: "orders", Version: "1.5.0", Source: "./chart",
  	//   ValueFiles: []string{"values-kind.yaml"}, InlineKeys: []string{"args", "enabled", "image", "port", "replicas"}}}
  }

  func TestHelmWithNoPreviousRefusesATransition(t *testing.T) {
  	drv, _ := newDriver(t, candidateOnly(), scripted())
  	// after Fetch and Install:
  	mustPanic(t, "no previous release", func() { drv.Deploy(t.Context()) })
  	mustPanic(t, "no previous release", func() { drv.Rollback(t.Context()) })
  }
  ```

  In `TestHelmProgrammingErrors`, drop the first three lines (the panic of `NewHelm` with no previous release); the rest stays.

- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./internal/deploy/ -run 'TestHelmWithNoPrevious|TestHelmProgrammingErrors' -v` / Expected: FAIL: `NewHelm` panics, "the Helm driver without a previous release is not built yet".

- [ ] **Step 3: Change `NewHelm` and the driver's methods in `internal/deploy/helm.go`**, with the interface comment from the Interfaces block. `NewHelm` no longer panics. `Fetch` skips the previous release when `o.Helm.Previous` is nil. `Install` installs `d.previous` when there is one and `d.candidate` otherwise, and reads the revision in both cases, as now. `Deploy` and `Rollback` panic with "deploy: Deploy called on a Helm driver with no previous release" (and "Rollback …") before their present check. `Transitions` returns `[]verdict.WindowName{verdict.Restart}` with no previous release; `Releases` returns the candidate's alone. Update the comments of `Fetch`, `Install`, `Transitions` and `Releases` to say both cases.

- [ ] **Step 4: Run the tests to see them pass**
  Run: `go test ./internal/deploy/ -v` / Expected: PASS.

- [ ] **Step 5: Commit**
  `git add internal/deploy/helm.go internal/deploy/helm_test.go` and `git commit -m "Let the Helm driver run with no previous release: it installs the candidate and gives a restart"`

### Task 4: `deploy.Setup`

Design: section 7, "Where commands run", "The namespace", "Setup commands", "When a command fails", "When a command runs too long", "When a signal stops the run"; section 5 (`deploy` runs the `setup` commands with the commands driver's command runner); README decision 7 and Review Focus 1.

**Files:**
- Create: `internal/deploy/setup.go`
- Test: new `internal/deploy/setup_test.go`

**Interfaces:**
- Consumes: `Runner`, `Command`, `Result`, `Ending` of `internal/deploy/command.go` as built; `NewCommands` (Task 2), for Review Focus 1.
- Produces: `func Setup(ctx context.Context, r Runner, commands [][]string, timeout time.Duration) Result`
  Runs the setup commands in order on r, each named setup and bounded by
  timeout (limits.timeout), and returns the Result of the first that did not
  succeed; the rest are not started. When all succeeded, or there were none,
  it returns a Result named setup that Succeeded, with exit code 0. The
  runner numbers the evidence: setup, setup-2 and so on. A command reads end
  of file from its standard input.

- [ ] **Step 1: Write the failing tests**

  ```go
  func TestSetupRunsInOrder(t *testing.T) {
  	f := newFake()
  	cmds := [][]string{{"kubectl", "apply", "-f", "test/prerequisites.yaml"}, {"kubectl", "create", "secret", "generic", "probe-token"}}
  	r := Setup(t.Context(), f, cmds, 5*time.Minute)
  	// r.Name == "setup", r.Ending == Succeeded;
  	// cmp.Diff([]Command{{Name: "setup", Args: cmds[0], Timeout: 5 * time.Minute},
  	//   {Name: "setup", Args: cmds[1], Timeout: 5 * time.Minute}}, f.calls) is empty.
  }

  func TestSetupWithNoCommands(t *testing.T) {
  	f := newFake()
  	r := Setup(t.Context(), f, nil, time.Minute)
  	// r.Name == "setup", r.Ending == Succeeded, r.ExitCode == 0, len(f.calls) == 0.
  }

  func TestSetupStopsAtTheFirstFailure(t *testing.T) {
  	h := newHarness(t, os.Environ())
  	marker := filepath.Join(t.TempDir(), "third-ran")
  	r := Setup(t.Context(), h.runner, [][]string{{"true"}, {"sh", "-c", "exit 4"}, {"touch", marker}}, time.Minute)
  	// r.Name == "setup", r.Ending == ExitedNonZero, r.ExitCode == 4;
  	// the marker does not exist; h.read(t, "setup") ends with "rollproof: ok, exit code 0\n",
  	// h.read(t, "setup-2") ends with "rollproof: exit-nonzero, exit code 4\n", and no file setup-3 exists.
  }

  func TestSetupEndings(t *testing.T) {
  	setKillAfter(t, 200*time.Millisecond)
  	h := newHarness(t, os.Environ())
  	// a program that cannot start ({"rollproof-test-no-such-program"}): Ending NotStarted, ExitCode -1;
  	// {"sh", "-c", "sleep 30"} with timeout 300 ms: Ending StoppedAtLimit, returned within 2 s;
  	// {"sh", "-c", "sleep 30"} with a ctx cancelled 100 ms after the start: Ending StoppedBySignal,
  	//   returned within 1 s; then h.runner.Close().
  }

  // Review Focus 1: a command that reads its standard input, or asks a question, goes on or fails
  // at once and never waits until the limit.
  func TestSetupCommandsReadNoInput(t *testing.T) {
  	h := newHarness(t, os.Environ())
  	start := time.Now()
  	r := Setup(t.Context(), h.runner, [][]string{
  		{"sh", "-c", `read line; echo "read gave $?"`},
  		{"sh", "-c", `printf "continue? [y/N] "; read answer || exit 3`},
  	}, 30*time.Second)
  	// r.Ending == ExitedNonZero, r.ExitCode == 3; h.read(t, "setup") holds "read gave 1";
  	// time.Since(start) < 5*time.Second.
  	d := NewCommands(CommandsOptions{Commands: config.Commands{Install: []string{"sh", "-c", "read answer || exit 5"}},
  		Timeout: 30 * time.Second, Runner: h.runner})
  	// d.Install(t.Context()): Ending ExitedNonZero, ExitCode 5, Ended.Sub(Began) < 5*time.Second.
  }
  ```

- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./internal/deploy/ -run 'TestSetup' -v` / Expected: FAIL to build: `Setup` is not defined.

- [ ] **Step 3: Implement `Setup` in `internal/deploy/setup.go`**, with the interface comment from the Interfaces block. The runner as built starts each command with no standard input, which Go connects to the null device, so a read gets end of file at once; `Setup` changes nothing in the runner. If `TestSetupCommandsReadNoInput` fails, stop and report: the runner then gives its commands an input, and section 7's commands would wait until the limit.

- [ ] **Step 4: Run the tests to see them pass**
  Run: `go test ./internal/deploy/ -v` / Expected: PASS.

- [ ] **Step 5: Commit**
  `git add internal/deploy/setup.go internal/deploy/setup_test.go` and `git commit -m "Add deploy.Setup, which runs the setup commands in order on the run's runner and stops at the first that does not succeed"`
