# Step 4 plan, part 5: the run, the report and the cluster's logs

### Task 13: `setup` in the run

Design: section 7, "Setup commands", "Where commands run", "When a command fails" and "When a command runs too long"; section 10, "Reasons" (`setup-failed`). This plan's decisions 1 and 7.

**Files:**
- Modify: `internal/run/run.go` (`Cluster` gains `Setup`; `setUp` calls it between `Fetch` and `Watcher`)
- Modify: `internal/run/conditions.go` (`setupCondition`)
- Modify: `cmd/rollproof/wire.go` (`(*session).Setup`)
- Modify: `internal/config/notbuilt.go` (`setup` leaves the list)
- Test: `internal/run/conditions_test.go`, `internal/run/run_test.go`, `internal/run/fakes_test.go`, `cmd/rollproof/main_test.go`, `internal/config/notbuilt_test.go`

**Interfaces:**
- Consumes: `deploy.Setup(ctx context.Context, r Runner, commands [][]string, timeout time.Duration) Result` (Task 4)
- Produces: `run.Cluster.Setup(ctx context.Context) deploy.Result`
  Runs the configuration's setup commands in order on the run's command
  runner, each bounded by limits.timeout, and returns the Result of the first
  that did not succeed, or a Result named setup with Ending Succeeded when all
  did or there were none. run calls it once, after Deployer and Fetch and
  before Watcher; a context that ends stops the command that runs.
- Produces: `func setupCondition(r deploy.Result) (verdict.Condition, bool)`
  The condition that a setup Result gives, if any: setup-failed when the
  command exited non-zero, could not be started or was stopped at
  limits.timeout; internal-error when it gave an answer the tool could not
  read or ended in a way this build does not know. A success and a stop by a
  signal give none: run reports the signal as interrupted.
- Produces: `func (s *session) Setup(ctx context.Context) deploy.Result`
  run.Cluster.Setup on the runner that Deployer made, so that setup commands
  lose the same variables and write their evidence under commands/ as setup,
  setup-2 and so on. Deployer must have been called first.

- [ ] **Step 1: Write the failing tests**
  - `conditions_test.go`, `TestSetupCondition`, a table:
    ```go
    {deploy.Result{Name: "setup", Ending: deploy.Succeeded}, false, "", ""},
    {deploy.Result{Name: "setup", Ending: deploy.StoppedBySignal, ExitCode: -1}, false, "", ""},
    {deploy.Result{Name: "setup", Ending: deploy.ExitedNonZero, ExitCode: 3}, true, verdict.SetupFailed, "a setup command exited with code 3"},
    {deploy.Result{Name: "setup", Ending: deploy.ExitedNonZero, ExitCode: -1}, true, verdict.SetupFailed, "a setup command was killed by a signal"},
    {deploy.Result{Name: "setup", Ending: deploy.NotStarted, ExitCode: -1}, true, verdict.SetupFailed, "a setup command could not be started"},
    {deploy.Result{Name: "setup", Ending: deploy.StoppedAtLimit, ExitCode: -1}, true, verdict.SetupFailed, "a setup command was still running at limits.timeout and the tool stopped it"},
    {deploy.Result{Name: "setup", Ending: deploy.ExitedNonZero, ExitCode: 0}, true, verdict.InternalError, "a setup command gave an answer the tool could not read"},
    {deploy.Result{Name: "setup", Ending: "gone"}, true, verdict.InternalError, "a setup command ended in a way the tool does not know"},
    ```
    Assert the bool, the code and the exact text of each row.
  - `fakes_test.go`: `fakeCluster` gains `setup deploy.Result`; its `Setup` records `"setup"`, runs the hook `"setup"`, and returns `c.setup`, or `deploy.Result{Name: "setup", Ending: deploy.Succeeded}` when `c.setup.Ending` is empty.
  - `run_test.go`, `TestPass`: the calls gain `"setup"` between `"fetch"` and `"watcher"`.
  - `TestEachBeforeFailure`: `setUp` gains `"setup"` after `"fetch"`; the row `"fetch"` gives its calls in full, `[]string{"load", "check", "create", "store", "deployer", "fetch", "end-commands", "delete"}`; a new row `"setup fails"` sets `w.cluster.setup = deploy.Result{Name: "setup", Ending: deploy.ExitedNonZero, ExitCode: 1}` and expects `"setup-failed"` with `then("end-commands", "delete")`: no watcher, no install.
  - `TestSetupStoppedBySignal`: the hook `"setup"` cancels the run's context, and `w.cluster.setup` is `{Ending: deploy.StoppedBySignal, ExitCode: -1}`. Assert: exit 2, reasons `[]string{"interrupted"}`, no `"watcher"` and no `"install"` in the calls.
  - `main_test.go`, `TestSessionSetup`: a `session` as `TestEndCommands` builds one, with `cfg.Setup = [][]string{{"sh", "-c", "exit 0"}, {"sh", "-c", "exit 3"}, {"sh", "-c", "exit 0"}}`, `cfg.Limits.Timeout = time.Minute`, and an evidence function that records each name it is given. After `s.Deployer(cfg)`, `s.Setup(ctx)` gives `Ending` `deploy.ExitedNonZero` and `ExitCode` 3, and the names are exactly `[]string{"commands/setup", "commands/setup-2"}`: the third command never ran.
  - `notbuilt_test.go`, `TestNotBuilt`: the row `"setup"` wants `nil`; the row `"several at once"` wants `[]string{"images.orders.as", "registries"}`.
- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./internal/run/ ./cmd/rollproof/ ./internal/config/ -run 'TestSetupCondition|TestPass$|TestEachBeforeFailure|TestSetupStoppedBySignal|TestSessionSetup|TestNotBuilt$' -v` / Expected: FAIL to build (`setupCondition`, `Cluster.Setup` and `session.Setup` are not defined).
- [ ] **Step 3: Implement `run.Cluster.Setup`, `setupCondition` and `(*session).Setup`** with the interface comments from the Interfaces block. `setupCondition` uses `classify` and the subject "a setup command". In `setUp`, after `s.releases = d.Releases()` and its interrupted check:
  ```go
  res := c.Setup(ctx)
  if s.interrupted(ctx) {
  	return false
  }
  if cond, failed := setupCondition(res); failed {
  	s.tl.Before = append(s.tl.Before, cond)
  	return false
  }
  ```
  `(*session).Setup` returns `deploy.Setup(ctx, s.runner, s.cfg.Setup, s.cfg.Limits.Timeout)`. Take `setup` out of `NotBuilt` and its comment's example.
- [ ] **Step 4: Run the tests to see them pass**
  Run: `go test ./internal/run/ ./cmd/rollproof/ ./internal/config/` / Expected: PASS
- [ ] **Step 5: Commit**
  `git add internal/run/run.go internal/run/conditions.go internal/run/conditions_test.go internal/run/run_test.go internal/run/fakes_test.go cmd/rollproof/wire.go cmd/rollproof/main_test.go internal/config/notbuilt.go internal/config/notbuilt_test.go` and `git commit -m "Run the setup commands after the charts are fetched and before the watch starts"`

### Task 14: Restart transitions and the commands driver in the run

Design: section 7, "Transitions", "The restart", "When a transition has finished", "The commands driver" and the last paragraph of "The contract"; section 10, "What a run writes" (the console's first line, the lines for the releases, `result.json`'s transitions and releases), "Reasons" (`restart-failed`), "What every report states". This plan's decisions 1, 3, 4, 5 and 6.

**Files:**
- Modify: `internal/run/run.go` (`Watcher` gains `Restart`; `transitionsStep` runs a restart; the refusal of any other transition goes)
- Modify: `internal/run/record.go` (`transitionFacts.restart`; the restart's and the commands driver's records)
- Modify: `internal/run/conditions.go` (`installCondition` names the candidate in a restart-only run)
- Modify: `internal/report/record.go` (`Transition.Command` and `Transition.Restart`; `Release.Program`)
- Modify: `internal/report/console.go` (the commands driver's release lines; the line for a restart-only pass)
- Modify: `cmd/rollproof/wire.go` (`Deployer` chooses the driver)
- Modify: `internal/config/notbuilt.go` (`deploy.commands` and `deploy.helm.previous` leave the list)
- Test: `internal/run/run_test.go`, `internal/run/fakes_test.go`, `internal/run/record_test.go`, `internal/report/console_test.go`, `internal/report/result_test.go`, `internal/report/testdata/console-restart.golden.txt` (new), `internal/report/testdata/console-commands.golden.txt` (new), `internal/report/testdata/result-pass.golden.json`, `cmd/rollproof/main_test.go`, `internal/config/notbuilt_test.go`

**Interfaces:**
- Consumes: `deploy.NewCommands(o CommandsOptions) *CommandsDriver` and `deploy.Release.Program string` (Task 2); `deploy.NewHelm(o HelmOptions) *HelmDriver` with no `previous` (Task 3); `watch.Restart{Began, Ended time.Time; Accepted bool; Workload string; Text string}` and `(*watch.Watcher).Restart(ctx context.Context) watch.Restart` (Task 5)
- Produces: `run.Watcher.Restart(ctx context.Context) watch.Restart`
  Restarts every watched workload as decision 3 says and returns when the
  last change was taken or the first was refused. run calls it once for a
  restart's window, beside Transition, and hands its return to Transition as
  the end of the transition's command.
- Produces: `report.Transition.Command *string`
  How the transition's command ended: ok, exit-nonzero, not-started,
  stopped-at-limit or stopped-by-signal. Nil, null in result.json, for a
  restart, which has no command.
- Produces: `report.Transition.Restart *string`
  Whether the API server took the restart's change: "accepted" or "refused".
  Nil, null in result.json, for a deploy and a rollback.
- Produces: `report.Release.Program string`
  The first word of the commands driver's command for this role; empty for
  the Helm driver, whose chart fields are empty for the commands driver.
- Produces: `func (s *session) Deployer(cfg *config.Config) (deploy.Deployer, []string, error)`
  As now, with the driver that the configuration names: the commands driver
  for deploy.commands, bounded by limits.timeout, and the Helm driver for
  deploy.helm, with or without a previous release. Both run on the one
  runner that Setup and EndCommands use.

- [ ] **Step 1: Write the failing tests**
  - `fakes_test.go`: `fakeDeployer` gains `transitions []verdict.WindowName`; `Transitions` gives it, or `{verdict.Deploy, verdict.Rollback}` when it is nil. `fakeWatcher` gains `restart watch.Restart` and `restartPanics bool`; its `Restart` records `"restart"`, runs the hook `"restart"`, panics with `"the restart broke"` when `restartPanics` is set, and otherwise returns `f.restart` with `Began` and `Ended` at the clock's time.
  - `run_test.go`: `resultFile.Transitions` reads `Command *string` and `Restart *string`; the test at `run_test.go` that checks `Command` is `"stopped-by-signal"` reads `*r.Transitions[0].Command`.
  - `TestRestartTransition`: `w.deployer.transitions = []verdict.WindowName{verdict.Restart}`, `w.watcher.restart = watch.Restart{Accepted: true}`, and `w.watcher.transitions[verdict.Restart]` the world's replaced transition. Assert: exit 0; the calls after `"install-settled"` are `"restart", "transition-restart", "probe-stop"`; window results `[]string{"pass", "pass"}` for `steady` and `restart`; in `result.json`, `transitions[0].command` is null and `transitions[0].restart` is `"accepted"`; stdout holds `"\n  a deploy to another release and a rollback, because only a restart was tested\n"`.
  - `TestRestartStopsAtARefusal`: as above, with `w.watcher.restart = watch.Restart{Accepted: false, Workload: "statefulset/db", Text: "admission webhook \"deny\" denied the request"}` and `w.watcher.transitions[verdict.Restart] = watch.Transition{}`, which replaced nothing. Assert: exit 2; reasons exactly `[]string{"restart-failed"}`, in the window `restart`, with the text `the API server refused the restart of statefulset/db: admission webhook "deny" denied the request`; no `no-pod-replaced`; `transitions[0].restart` is `"refused"`.
  - `TestRestartOnlyInstallNamesTheCandidate` (section 4, "A run with no previous release": the text names the candidate): with `w.deployer.transitions = []verdict.WindowName{verdict.Restart}` and an install that exits 1, the reason is `install-failed` and its text begins `the install of the candidate`; with transitions `[]verdict.WindowName{verdict.Deploy, verdict.Rollback}` the same install gives the text as today, which names no release.
  - `TestRestartPanics`: `w.watcher.restartPanics = true`. Assert: exit 2; `internal-error` in the window `restart`, with `the restart broke` in its text.
  - `TestDeployWithoutRollback`: `w.deployer.transitions = []verdict.WindowName{verdict.Deploy}`. Assert: exit 0; no `"rollback"` call; window results `[]string{"pass", "pass"}`; stdout holds `"the candidate's shutdown and the installed release's start-up, because the run has no rollback"`.
  - `record_test.go`, `TestRecordOfARestart`: `transitionFacts{tr: watch.Transition{Name: verdict.Restart}, restart: &watch.Restart{Accepted: true}}` gives `report.Transition{Name: "restart", Command: nil, Restart: new("accepted")}`; with `Accepted: false`, `Restart: new("refused")`; a deploy's facts with `cmd.Ending` `deploy.Succeeded` give `Command: new("ok")` and `Restart: nil`.
  - `TestRecordOfTheCommandsDriver`: `cfg.Deploy = config.Deploy{Namespace: "orders", Commands: &config.Commands{Install: []string{"kubectl", "apply", "-k", "overlays/previous"}, Deploy: []string{"kubectl", "apply", "-k", "overlays/candidate"}}}` and `releases` `{Role: "install", Program: "kubectl"}`, `{Role: "deploy", Program: "kubectl"}`. Assert: `Driver` `"commands"`, `Name` `"orders"`, and `Releases` `[]report.Release{{Role: "install", Program: "kubectl"}, {Role: "deploy", Program: "kubectl"}}`.
  - `console_test.go`: every `Command: "ok"` becomes `Command: new("ok")`. `TestConsoleExamples` gains two rows:
    - `{"testdata/console-restart.golden.txt", restartRun()}`: `passRun()` with `Releases` the candidate alone, `Windows` steady and `{Name: "restart", Start: at(10_000), End: at(43_400), Result: "pass", Checks: 6680, SlowestMs: new(212.0), LagMs: new(38.0)}` (the pointer form of Task 12), and `Transitions` `{Name: "restart", Restart: new("accepted"), Workloads: []Workload{ordersReplaced(5300)}}`. The golden file holds:
      ```
      releases    candidate  orders 1.5.0, ./chart; values: inline (image)
      ```
      ```
      transition  workload            probed   pods replaced   quickest stop
      restart     deployment/orders   yes      2 of 2          5.3 s

      exercised
        restart     the start-up and the shutdown of the release
      not exercised
        a deploy to another release and a rollback, because only a restart was tested
        the delay before a larger cluster stops routing to a stopping pod
      ```
    - `{"testdata/console-commands.golden.txt", commandsRun()}`: `passRun()` with `Driver: "commands"`, `Name: "orders"`, `Releases` `{Role: "install", Program: "kubectl"}`, `{Role: "deploy", Program: "kubectl"}`, and the deploy's window and transition only. The golden file begins:
      ```
      rollproof: PASS   orders (commands) on Kubernetes v1.37.0 in kind

      releases    install  kubectl
                  deploy   kubectl
      images      orders   run by deployment/orders, in pods the deploy created
      ```
      and its "not exercised" list begins with `  the candidate's shutdown and the installed release's start-up, because the run has no rollback`.
  - `TestConsoleRestartOnlyLineOnlyOnAPass`: the line `a deploy to another release and a rollback, because only a restart was tested` is not in `Console(passRun())`, nor in `restartRun()` with `Verdict` `"fail"` or `"not-verified"`.
  - `result_test.go`: `Command: "ok"` becomes `Command: new("ok")`; regenerate `result-pass.golden.json` with `-update` and check that the diff only adds `"restart": null` to each transition and `"program": ""` to each release.
  - `main_test.go`, `TestDeployerChoosesTheDriver`: a `session` as `TestEndCommands` builds one. With `cfg.Deploy.Commands = &config.Commands{Install: []string{"kubectl", "apply", "-k", "x"}}` the Deployer is a `*deploy.CommandsDriver` whose `Transitions()` is `[]verdict.WindowName{verdict.Restart}`; with `cfg.Deploy.Helm = &config.Helm{Release: "orders", Candidate: &config.Release{Chart: "./chart", IsPath: true}}` it is a `*deploy.HelmDriver` whose `Transitions()` is the same.
  - `notbuilt_test.go`, `TestNotBuilt`: the rows `"the commands driver"` and `"no previous release"` want `nil`.
- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./internal/run/ ./internal/report/ ./cmd/rollproof/ ./internal/config/ -run 'TestRestart|TestDeployWithoutRollback|TestRecordOf|TestConsole|TestResult|TestDeployerChoosesTheDriver|TestNotBuilt$' -v` / Expected: FAIL to build (`Watcher.Restart`, `Transition.Restart` and `Release.Program` are not defined; `Command` is not a pointer).
- [ ] **Step 3: Implement the five signatures** with the interface comments from the Interfaces block.
  - `transitionsStep`: remove the block that places "the tool cannot start a … transition". For `verdict.Restart`, run the restart through `s.command`, so that a panic in it is caught as a command's is:
    ```go
    var rs watch.Restart
    c := s.command(ctx, func(ctx context.Context) deploy.Result {
    	rs = s.watcher.Restart(ctx)
    	return deploy.Result{Name: "restart", Ending: deploy.Succeeded, Began: rs.Began, Ended: rs.Ended}
    }, nil)
    ```
    then `Transition` and `await` as for a command; the transition's facts keep `restart: &rs` and no `cmd`. After the interrupted and panic checks, a restart's window is filled by `placeRestart(w, tr, rs)`: `tr.Conditions(rs.Accepted)`, then, when `!rs.Accepted`, `verdict.Condition{Code: verdict.RestartFailed, Text: "the API server refused the restart of " + rs.Workload + ": " + rs.Text}`. A refused restart is so never judged as a transition that replaced no pod.
  - `record.go`: `transitionFacts` gains `restart *watch.Restart`; `transitionRecords` gives `Restart` from it and `Command` nil for a restart, and `Command: new(string(t.cmd.Ending))` otherwise; releases copy `Program`; `cfg.Deploy.Commands != nil` gives `r.Driver, r.Name = "commands", f.cfg.Deploy.Namespace`.
  - `console.go`: `cleanRun` cleans `Program`; a release with a `Program` prints `pad(rel.Role, roleWidth)+rel.Program` and nothing else; `exercisedLists` puts `  a deploy to another release and a rollback, because only a restart was tested` first under "not exercised" when `hasWindowThatRan(r, "restart") && !hasWindowThatRan(r, "deploy")`.
  - `conditions.go`: `installCondition` takes a second argument, `restartOnly bool` (the deployer's transitions are `[restart]`); when it is true, the text's subject "the install command" reads "the install of the candidate", and the install's texts of `watch` that `run` places (not ready when the limit passed, stopped being ready) get "of the candidate" after "the install" in the same way.
  - `wire.go`: `Deployer` returns `deploy.NewCommands(deploy.CommandsOptions{Commands: *cfg.Deploy.Commands, Timeout: cfg.Limits.Timeout, Runner: runner})` when `cfg.Deploy.Commands != nil`, and the Helm driver otherwise.
  - Take `deploy.commands` and `deploy.helm.previous` out of `NotBuilt`.
- [ ] **Step 4: Run the tests to see them pass**
  Run: `go test ./internal/run/ ./internal/report/ ./cmd/rollproof/ ./internal/config/` and `go test -race ./internal/run/` / Expected: PASS
- [ ] **Step 5: Commit**
  `git add internal/run/run.go internal/run/record.go internal/run/conditions.go internal/run/conditions_test.go internal/run/run_test.go internal/run/fakes_test.go internal/run/record_test.go internal/report/record.go internal/report/console.go internal/report/console_test.go internal/report/result_test.go internal/report/testdata/console-restart.golden.txt internal/report/testdata/console-commands.golden.txt internal/report/testdata/result-pass.golden.json cmd/rollproof/wire.go cmd/rollproof/main_test.go internal/config/notbuilt.go internal/config/notbuilt_test.go` and `git commit -m "Run restart transitions and the commands driver, and record a restart's answer and each command's first word"`

### Task 15: The custom probe in the run

Design: section 6, "The contract" ("Form", "Where probe pods run", the last paragraph) and "Lifecycle" ("Stop"); section 8, "Types", "What else is rejected" and "Secrets"; section 10, "What a run writes" (the probe's lines, the second table's "probed", `result.json`'s probes) and "What every report states" (the custom probe's line in place of three). This plan's decisions 1, 8 and 9.

**Files:**
- Modify: `internal/config/validate.go` (a custom image needs a digest), `internal/config/notbuilt.go` (`probe.custom` leaves the list)
- Modify: `internal/run/run.go` (no target check for a custom probe; `Cluster.Probe` returns an error)
- Modify: `internal/run/record.go` (the custom probe's entry)
- Modify: `internal/report/record.go` (`Probe.CertificateChecked` is a pointer), `internal/report/console.go` (the custom probe's lines and its not-exercised line)
- Modify: `cmd/rollproof/wire.go` (`podOptions`; `check` reads the custom `env`; `Probe` passes `Evidence`)
- Test: `internal/config/validate_test.go`, `internal/config/notbuilt_test.go`, `internal/run/run_test.go`, `internal/run/fakes_test.go`, `internal/run/record_test.go`, `internal/report/console_test.go`, `internal/report/result_test.go`, `internal/report/testdata/console-custom.golden.txt` (new), `cmd/rollproof/main_test.go`

**Interfaces:**
- Consumes: `probe.PodOptions.Command []string`, `Env []corev1.EnvVar`, `ServiceAccountToken bool` and `probe.CustomEnv(env []config.EnvVar) ([]corev1.EnvVar, error)` (Task 9); `probe.PodOptions.Evidence func(name string) (io.WriteCloser, error)` (Task 10)
- Produces: `run.Cluster.Probe(cfg *config.Config, probeImage string, raw io.Writer) (probe.Probe, error)`
  The probe that the configuration names, not yet started: the built-in
  probe on probeImage, or the custom probe on its own image. raw receives the
  probe's lines. An error is a configuration that the probe's pod cannot be
  written from; run places it before the install window and starts no probe.
- Produces: `func podOptions(cfg *config.Config, probeImage string) (probe.PodOptions, error)`
  The image, command, arguments, environment, token and drain of the probe's
  pod. The built-in probe: probeImage, its HTTP arguments, no token, a drain
  of the request timeout plus 5 s. A custom probe: its image as resolved, its
  command and args, its env with valueFrom kept, the namespace's default
  token, a drain of 10 s. An env that a pod cannot hold is config-rejected.
- Produces: `report.Probe.CertificateChecked *bool`
  Whether the probe checked the server's certificate: false for the built-in
  probe on https, true on http. Nil, null in result.json, for a custom probe,
  whose checks the tool does not know.
- Produces: `func imageHasDigest(image string) bool`
  Whether probe.custom.image, as Load reads it before the images are stored,
  names a digest: a reference with @ and a digest, the form
  ${images.NAME.ref} alone, or a reference that ends in @${images.NAME.digest}.

- [ ] **Step 1: Write the failing tests**
  - `validate_test.go`, `TestValidateRejects` gains rows; each wants `"probe.custom.image"` and none quotes the planted value:
    ```go
    {"a custom image with no digest", httpProbe, "probe:\n  custom:\n    image: registry.example/tools/S3CRET-VALUE\n    command: [sh]\n", "probe.custom.image"},
    {"a custom image with a tag", httpProbe, "probe:\n  custom:\n    image: registry.example/tools/busybox:S3CRET-VALUE\n    command: [sh]\n", "probe.custom.image"},
    {"a custom image with a short digest", httpProbe, "probe:\n  custom:\n    image: registry.example/tools/S3CRET-VALUE@sha256:abc\n    command: [sh]\n", "probe.custom.image"},
    ```
    The rows `"a null custom command item"`, `"a custom command with an empty program"` and `"a null custom arg"` keep their texts; if one now gives the digest's text, add `@` and `digestD` to its image.
  - `TestCustomImageForms`: a base text with `images: { probe: { local: "probe:dev" } }` and a custom probe. `image: "${images.probe.ref}"` and `image: "registry.example/tools/busybox@${images.probe.digest}"` load; `image: "${images.probe.repository}"` and `image: "x${images.probe.ref}"` are rejected with `probe.custom.image`.
  - `notbuilt_test.go`, `TestNotBuilt`: the row `"a custom probe"` wants `nil`.
  - `main_test.go`, `TestRunNotBuilt`: the file has `probe.http` (`url: http://orders.orders.svc/`) and `registries: [{ host: registry.example }]` in place of the custom probe; the text wants `"not built yet: registries"`.
  - `TestPodOptions`:
    ```go
    custom := &config.Config{Limits: config.Limits{Timeout: time.Minute}, Probe: config.Probe{Custom: &config.Custom{
    	Image: "registry.example/tools/busybox@sha256:" + strings.Repeat("0", 64),
    	Command: []string{"sh", "-c", "loop"}, Args: []string{"a"},
    	Env: []config.EnvVar{{Name: "TOKEN", ValueFrom: map[string]any{"secretKeyRef": map[string]any{"name": "probe-token", "key": "token"}}}}}}}
    o, err := podOptions(custom, "rollproof.test/rollproof@sha256:fff")
    // err nil; o.Image is the custom image; o.Command and o.Args as given; o.Env[0].ValueFrom.SecretKeyRef.Name == "probe-token";
    // o.ServiceAccountToken true; o.Drain == 10*time.Second
    ```
    The built-in probe from `cfg.Probe.HTTP` with `Timeout: 2 * time.Second`: `o.Image` is the probe image, `o.Command` nil, `o.Args` `probe.HTTPArgs(httpOptions(cfg.Probe.HTTP))`, `o.ServiceAccountToken` false, `o.Drain` 7 s. An env entry with `ValueFrom: map[string]any{"secretKeyRef": "S3CRET-VALUE"}` gives a `verdict.Condition` with `ConfigRejected`, whose text begins `probe.custom.env` and does not hold `S3CRET-VALUE`.
  - `fakes_test.go`: `fakeCluster.Probe` returns `(c.w.probe, c.probeErr)`; `fakeCluster` gains `probeErr error`.
  - `run_test.go`, `TestEachBeforeFailure` gains the row `"probe options"`: `w.cluster.probeErr = verdict.Condition{Code: verdict.ConfigRejected, Text: "probe.custom.env[0]: not a form a pod's env takes"}`, code `"config-rejected"`, calls `then("setup", "watcher", "install", "install-ready", "target", "probe", "end-commands", "watch-stop", "delete")` with `setUp` as Task 13 left it (so without the duplicate `"setup"`).
  - `TestNotBuilt` (run): `w.cfg.Registries = []config.Registry{{Host: "registry.example"}}` in place of the custom probe; the text wants `"not built yet: registries"`.
  - `TestCustomProbeHasNoTargetCheck`: `w.cfg.Probe = config.Probe{Custom: &config.Custom{Image: "registry.example/tools/busybox@sha256:" + strings.Repeat("0", 64), Command: []string{"sh", "-c", "echo S3CRET-VALUE"}, Env: []config.EnvVar{{Name: "TOKEN", ValueFrom: map[string]any{"secretKeyRef": map[string]any{"name": "probe-token", "key": "token"}}}}}}`, and the fake transitions' workloads read `Probed: "not known"`. Assert: exit 0; no `"target"` call; `w.watcher.service` is nil and `w.watcher.useCalls` ends with `"install-ready"`; `result.json`, decoded into `map[string]any`, has `probes[0]` with `kind` `"custom"`, `target` the image, `env_names` `["TOKEN"]`, `command_count` 3, `args_count` 0 and `certificate_checked` null; the file holds neither `S3CRET-VALUE` nor `probe-token`.
  - `record_test.go`, `TestCustomProbeRecord`: the facts of a custom probe whose stop was asked, with `probe.Facts{Lines: 40, StoppedWhenAsked: true}`, give `report.Probe{Kind: "custom", Target: image, EnvNames: []string{"TOKEN", "TARGET"}, CommandCount: 3, ArgsCount: 1, Lines: 40, CertificateChecked: nil, StoppedWhenAsked: new(true)}`, the names in the order of the configuration; with `probeStop` `notAsked`, `StoppedWhenAsked` nil and the note `the run was interrupted, so the tool did not ask the probe to stop`. The existing HTTP rows read `*record(f).Probes[0].CertificateChecked`.
  - `console_test.go`, `TestConsoleExamples` gains `{"testdata/console-custom.golden.txt", customRun()}`: `passRun()` with `Probes` `{Kind: "custom", Target: "registry.example/tools/busybox@sha256:" + strings.Repeat("0", 64), EnvNames: []string{"TOKEN", "TARGET"}, CommandCount: 3, Lines: 86, StoppedWhenAsked: new(true)}` and every workload's `Probed` `"not known"`. The golden file holds:
    ```
    probe       custom: registry.example/tools/busybox@sha256:0000000000000000000000000000000000000000000000000000000000000000
                a custom probe judged this run; the tool does not know what it checks
                command: 3 items; args: 0 items; env: TOKEN, TARGET
    ```
    ```
    deploy      deployment/orders   not known   2 of 2          5.3 s
    ```
    ```
    not exercised
      the delay before a larger cluster stops routing to a stopping pod
      whatever the custom probe does not check
      routing that depends on where the client is
      capacity while pods are down, and autoscaling
      storage that attaches to one node at a time
      data migrations
      a node drain
    ```
  - `TestConsoleLines` gains `{"a custom probe with one item and no env", customRun, func(r *Run) { r.Probes[0].CommandCount, r.Probes[0].ArgsCount, r.Probes[0].EnvNames = 1, 1, nil }, "            command: 1 item; args: 1 item; env: none\n"}`, and the row `"https"` sets `CertificateChecked = new(false)`.
  - `result_test.go`: `CertificateChecked: true` becomes `new(true)`; `result-pass.golden.json` does not change.
- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./internal/config/ ./internal/run/ ./internal/report/ ./cmd/rollproof/ -run 'TestValidateRejects|TestCustomImageForms|TestNotBuilt|TestRunNotBuilt|TestPodOptions|TestEachBeforeFailure|TestCustomProbe|TestConsole|TestResult' -v` / Expected: FAIL to build (`podOptions` is not defined; `Cluster.Probe` returns one value; `CertificateChecked` is not a pointer).
- [ ] **Step 3: Implement the five signatures** with the interface comments from the Interfaces block.
  - `validate.go`: `imageHasDigest` is true when the image matches `^\$\{images\.[^}]+\.ref\}$`, or matches `^[^$]+@\$\{images\.[^}]+\.digest\}$`, or `name.NewDigest(image)` from `github.com/google/go-containerregistry/pkg/name` succeeds. After the custom command's check: `return x.reject("probe.custom.image", "must name a digest: REPOSITORY@sha256:…, ${images.NAME.ref}, or a reference that ends in @${images.NAME.digest}")`.
  - `run.go`: `setUp` calls `c.Target` only when `cfg.Probe.HTTP != nil`; a custom probe keeps `s.target` empty and `UseService(nil)` is still called. `startProbe` places a `Cluster.Probe` error with `beforeFail` and creates nothing.
  - `record.go`: a custom probe that began gets `report.Probe{Kind: "custom", Target: cu.Image, EnvNames, CommandCount: len(cu.Command), ArgsCount: len(cu.Args)}`, no settings, `CertificateChecked` nil, and the counts, the stop and the notes by the rule `probeRecord` uses today, taken out into a helper both entries share. The HTTP entry's `CertificateChecked` is `new(certificateChecked(h.URL))`.
  - `console.go`: for `Kind == "custom"` the probe's lines are `custom: TARGET`, the sentence `a custom probe judged this run; the tool does not know what it checks`, and `command: N items; args: M items; env: NAMES` (`1 item`; `env: none` with no names), followed by the lines `probeLines` gives after its settings line today, now shared. The certificate line comes only when `CertificateChecked` is non-nil and false. With a custom probe the not-exercised list puts `whatever the custom probe does not check` in place of the design's second line and drops its third and seventh.
  - `wire.go`: `const customDrain = 10 * time.Second`. `podOptions` turns `probe.CustomEnv`'s error into `verdict.Condition{Code: verdict.ConfigRejected, Text: "probe.custom.env: " + err.Error()}`. `check` calls `probe.CustomEnv(cfg.Probe.Custom.Env)` for a custom probe and returns the same condition, so that a bad env is rejected before a cluster exists. `session.Probe` takes `podOptions` and adds `Client`, `StartTimeout: cfg.Limits.Timeout`, `Raw`, `Now: time.Now` and `Evidence: s.evidence`, for both probes.
  - Take `probe.custom` out of `NotBuilt`.
- [ ] **Step 4: Run the tests to see them pass**
  Run: `go test ./internal/config/ ./internal/run/ ./internal/report/ ./cmd/rollproof/` / Expected: PASS
- [ ] **Step 5: Commit**
  `git add internal/config/validate.go internal/config/validate_test.go internal/config/notbuilt.go internal/config/notbuilt_test.go internal/run/run.go internal/run/record.go internal/run/run_test.go internal/run/fakes_test.go internal/run/record_test.go internal/report/record.go internal/report/console.go internal/report/console_test.go internal/report/result_test.go internal/report/testdata/console-custom.golden.txt cmd/rollproof/wire.go cmd/rollproof/main_test.go` and `git commit -m "Run a custom probe with no target check, require its image's digest, and report it by names and counts"`

### Task 16: A failed check's bad line written during the drain

Design: section 6, "The contract", the paragraph that begins "A fault of a line is placed in the window", and "Time", the paragraph on a `t` earlier than the moment the tool asked for the probe's pod; section 10, "Rules of the verdict" (rule 3); section 14, point 29. This plan's decisions 10 and 11.

**Files:**
- Modify: `internal/run/run.go` (`addEvent` keeps which faults were a failed check's; `timeline` gives them; `moveToolFaults` moves them)
- Test: `internal/run/run_test.go`

**Interfaces:**
- Consumes: `probe.Event.FailedCheck bool` (Task 11): the event's `Fault`, a `*verdict.Condition`, is a failed check's line's
- Produces: `func (s *runState) moveToolFaults(t *verdict.Timeline, failedCheck []bool)`
  Places in the last window that began each condition of t.Timed at or
  after that window's end that is a failure of the tool, or the fault of a
  failed check's line (failedCheck[i] true for t.Timed[i]). Every other
  condition after the last window stays where Decide puts it, and the report
  shows it as a warning. With no begun window, or one still open, it does
  nothing.

- [ ] **Step 1: Write the failing tests**
  A drain fault is one event of `w.probe.last`, written at `t0 + 71 s`, after the rollback's end at `t0 + 70 s`:
  ```go
  drainFault := func(failedCheck bool) probe.Event {
  	return probe.Event{Fault: &verdict.Condition{Code: verdict.ProbeLineInvalid,
  		At: t0.Add(71 * time.Second), Text: "the line holds ok twice"}, FailedCheck: failedCheck}
  }
  ```
  - `TestAFailedChecksBadLineInTheDrainIsJudged`: `w.probe.last = []probe.Event{drainFault(true)}`. Assert: exit 2; window results `[]string{"pass", "pass", "not-verified"}`; `r.windowOf("probe-line-invalid")` gives `"rollback"`; no warning.
  - `TestAnotherFaultInTheDrainIsAWarning`: `drainFault(false)`. Assert: exit 0; a pass; exactly one warning, `after the last window: ` followed by the condition's text.
  - `TestAFailedChecksBadLineGoesToTheLastWindowThatBegan`: the deploy fails by `failureAt(20*time.Second, 20*time.Second)`, so the rollback never runs, and the drain fault is at `t0 + 41 s`, after the deploy's end at `t0 + 40 s`. Assert: exit 1; `probe-line-invalid` in the window `deploy`.
  - `TestAToolFaultInTheDrainStillMoves`: the drain fault's code is `verdict.InternalError` and `FailedCheck` false. Assert: exit 2; `internal-error` in the window `rollback` (the behaviour of carry-in T29-1 stays).
- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./internal/run/ -run 'TestAFailedChecksBadLine|TestAnotherFaultInTheDrain|TestAToolFaultInTheDrain' -v` / Expected: FAIL: `TestAFailedChecksBadLineInTheDrainIsJudged` and `TestAFailedChecksBadLineGoesToTheLastWindowThatBegan` find a pass or a fail and a warning in place of the reason.
- [ ] **Step 3: Implement `moveToolFaults(t *verdict.Timeline, failedCheck []bool)` in `internal/run/run.go`**, with its interface comment from the Interfaces block, which replaces the old comment. `runState` gains `failedCheck []bool`, which runs beside `tl.Timed`: `addEvent` appends `*ev.Fault` to `tl.Timed` and `ev.FailedCheck` to `failedCheck` under the same lock; `timeline` returns `(verdict.Timeline, []bool)` with a copy of both taken under that lock; `decide` passes the copy to `moveToolFaults`. The final piece of a stopped probe never reaches `run` as a fault (Task 11), so nothing here judges it.
- [ ] **Step 4: Run the tests to see them pass**
  Run: `go test ./internal/run/` and `go test -race ./internal/run/` / Expected: PASS
- [ ] **Step 5: Commit**
  `git add internal/run/run.go internal/run/run_test.go` and `git commit -m "Judge in the last window a failed check's bad line written during the drain"`

### Task 17: The evidence completed

Design: section 10, "What a run writes" ("When it is written", "On a signal", "Evidence folder", the rule that a failure after the verdict is a warning); section 12, row 13 (its harness reads the window marks while the run goes). This plan's decisions 12, 13 and 14, and Review Focus 4.

**Files:**
- Create: `internal/run/evidence.go` (the window marks, the timeline, the completion)
- Modify: `internal/run/run.go` (the marks written as they happen; `Watcher` gains `Finishes`, `PodEvents`, `WriteEvents`; `finish` completes the evidence; the `writer` interface gains `ReplaceEvidence`)
- Modify: `internal/report/evidence.go` (`ReplaceEvidence`)
- Test: `internal/run/evidence_test.go` (new), `internal/run/run_test.go`, `internal/run/fakes_test.go`, `internal/report/evidence_test.go`

**Interfaces:**
- Consumes: `watch.Finish{Name verdict.WindowName; At time.Time}` and `(*watch.Watcher).Finishes() <-chan watch.Finish` (Task 6); `watch.Options.Evidence func(name string) (io.WriteCloser, error)` (Task 6); `watch.PodEvent{At time.Time; Pod, Workload string; Kind string}`, `(*watch.Watcher).PodEvents() []watch.PodEvent` and `(*watch.Watcher).WriteEvents(ctx context.Context, w io.Writer) error` (Task 7)
- Produces: `func (w *report.Writer) ReplaceEvidence(name string, b []byte) error`
  Replaces the evidence file name with b in one step: a reader sees the old
  content or the new, never half, and the name is never numbered. Names are
  refused as Evidence refuses them. An error leaves the old content.
- Produces: `func newMarks(replace func(name string, b []byte) error, warn func(string)) *marks`
  The window marks of one run, written whole to windows.json by replace at
  each mark. The first failure to write is one warning through warn; later
  failures are not repeated.
- Produces: `func (m *marks) set(ws []verdict.Window)`
  Records each window's start, finish and end as ws gives them, takes every
  finish that waits, and writes the file. Called by the run's own flow when a
  window begins or ends.
- Produces: `func (m *marks) follow(finishes <-chan watch.Finish)`
  Writes the file again for each finish that arrives, while the run waits,
  until end. Each transition's entry keeps every finish in the order they
  came, also one that its settle time undid.
- Produces: `func (m *marks) end()`
  Stops following, takes every finish that waits and writes the file a last
  time. Safe to call more than once.
- Produces: `func writeTimeline(w io.Writer, obs []verdict.Observation, pods []watch.PodEvent, from, to time.Time) error`
  Writes one tab-separated row for each whole second from from's second up
  to to: the checks that began in it (an observation counts n), the failed
  ones, and the pod events of that second. A header comes first. What lies
  before from's second or at or after to is in no row.
- Produces: `func (s *runState) completeEvidence()`
  Writes the marks a last time, timeline.tsv and the events of both
  namespaces, after the final result.json and before the cluster is deleted.
  Each failure is one warning and changes no verdict. The events' listing is
  bounded by completionLimit, also after a signal.

- [ ] **Step 1: Write the failing tests**
  - `report/evidence_test.go`, `TestReplaceEvidence`: `ReplaceEvidence("windows.json", []byte("one"))`, then `[]byte("two")`: the file holds `two`, and the evidence folder holds that one file (no `windows.json-2`, no temporary file left). `ReplaceEvidence("../x", …)` and `ReplaceEvidence("", …)` are refused and write nothing; with `evidence` replaced by a file, it gives an error.
  - `run/evidence_test.go`, `TestWriteTimeline`:
    ```go
    from := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
    obs := []verdict.Observation{
    	{T: from.Add(200 * time.Millisecond), OK: true, N: 200},
    	{T: from.Add(1500 * time.Millisecond), OK: false, N: 1, Class: "refused"},
    	{T: from.Add(1700 * time.Millisecond), OK: true, N: 150},
    	{T: from.Add(-time.Second), OK: true, N: 5},     // before the first row
    	{T: from.Add(3 * time.Second), OK: true, N: 9},  // at to
    }
    pods := []watch.PodEvent{
    	{At: from.Add(1100 * time.Millisecond), Pod: "orders-7f8e9d-k3m2p", Workload: "deployment/orders", Kind: "created"},
    	{At: from.Add(1900 * time.Millisecond), Pod: "orders-6d5f9c-x2k4q", Workload: "deployment/orders", Kind: "deleted"},
    	{At: from.Add(2500 * time.Millisecond), Pod: "orders-7f8e9d-k3m2p", Workload: "deployment/orders", Kind: "ready"},
    }
    want := "second\tchecks\tfailed\tpod events\n" +
    	"2026-11-19T10:00:00Z\t200\t0\t\n" +
    	"2026-11-19T10:00:01Z\t151\t1\tcreated orders-7f8e9d-k3m2p (deployment/orders), deleted orders-6d5f9c-x2k4q (deployment/orders)\n" +
    	"2026-11-19T10:00:02Z\t0\t0\tready orders-7f8e9d-k3m2p (deployment/orders)\n"
    ```
    `writeTimeline(&b, obs, pods, from.Add(400*time.Millisecond), from.Add(3*time.Second))` writes exactly `want`.
  - `TestMarks`: a `replace` that keeps the last bytes per name. `set` with an install window that has begun writes `[{"name":"install","start":…,"finished":null,"end":null,"finishes":null}, …]`; two finishes of `deploy` sent on a channel given to `follow` appear, in order, in the deploy entry's `finishes` before `end` returns; a `replace` that always fails gives exactly one warning over five writes, which names `windows.json`.
  - `fakes_test.go`: `fakeWatcher` gains `finishes chan watch.Finish`, `finishAt map[verdict.WindowName][]time.Duration`, `podEvents []watch.PodEvent`, `events string`, `eventsErr error`, `eventsWait bool` and `eventsCalls []string`. `Transition` sends `Finish{Name: name, At: began.Add(d)}` on `finishes` for each `d` of `finishAt[name]` before it waits for the command. `Finishes` returns `f.finishes`. `PodEvents` returns `f.podEvents`. `WriteEvents` keeps `f.w.calls.list()` in `eventsCalls`, waits for its context to end when `eventsWait` is set and returns the context's error, and otherwise writes `f.events` and returns `f.eventsErr`. `panicWriter` gains `ReplaceEvidence` that returns nil.
  - `run_test.go`, `TestWindowMarksAsTheyHappen`: `finishAt` `{deploy: {15 * time.Second, 22 * time.Second}}`, `finishes` with room for 8. The hook `"rollback"` reads `windows.json` (with `w.windowMarks()`, whose `mark` now has `Finishes []time.Time`): the deploy entry has its start, its end and `finishes` exactly `[t0+25s, t0+32s]`, and the rollback entry has its start and no end. After the run, the rollback entry has its end.
  - `TestAFinishIsWrittenWhileTheWindowIsOpen`: `finishAt` `{deploy: {15 * time.Second}}`, the deploy command blocked by `w.deployer.block["deploy"]`. A goroutine reads `windows.json` every 10 ms until the deploy entry has one finish, then closes the block; more than 5 s fails the test. Assert: exit 0.
  - `TestTimelineAndEventsAreWritten`: `podEvents` one `created` at `t0 + 12 s`, `events` `"Normal Scheduled pod/orders-7f8e9d-k3m2p\n"`. Assert: `evidence/timeline.tsv` begins with `second\tchecks\tfailed\tpod events\n`, has 85 rows after the header (from `t0 - 15 s` to the rollback's end at `t0 + 70 s`), and its row `2026-11-13T10:00:12Z` ends with `created …`; `evidence/events.txt` holds the events text; `eventsCalls` ends with `"end-commands"` and holds neither `"watch-stop"` nor `"delete"`.
  - `TestEvidenceFailuresAreWarnings` (Review Focus 4), two subtests:
    - `"the evidence folder is gone"`: the hook `"probe-stop"` replaces `evidence` with a file, as `TestEvidenceThatCannotBeOpenedIsAWarning` does. Assert: exit 0; a final pass; exactly three warnings, which name `windows.json`, `timeline.tsv` and `events.txt` in that order.
    - `"the API server stops answering"`: `eventsWait` set and `completionLimit` set to 50 ms for the test (restored after it). Assert: exit 0; a final pass; exactly one warning, `the events were not written: context deadline exceeded`; the run returns within 5 s.
  - `TestEvidenceThatCannotBeOpenedIsAWarning`: four warnings now, naming `probe.log`, `windows.json`, `timeline.tsv` and `events.txt` in that order.
- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./internal/report/ ./internal/run/ -run 'TestReplaceEvidence|TestWriteTimeline|TestMarks|TestWindowMarks|TestAFinishIsWritten|TestTimelineAndEvents|TestEvidence' -v` / Expected: FAIL to build (`ReplaceEvidence`, `newMarks`, `writeTimeline` and the three `Watcher` methods are not defined).
- [ ] **Step 3: Implement the eight signatures** with the interface comments from the Interfaces block.
  - `report/evidence.go`: `ReplaceEvidence` takes the root checks of `Evidence`, writes `b` to a new file `.NAME-RANDOM` beside the name with `O_EXCL`, and renames it over the name with `os.Root.Rename`; on an error it removes the temporary file.
  - `run/evidence.go`: a mark is `{name, start, finished, end, finishes}`, JSON keys `name`, `start`, `finished`, `end`, `finishes`, times as pointers so that a missing one is null. `marks` has its own mutex; `set` and the following goroutine both take every finish that waits on the channel before they write, so a finish sent before `Transition` returned is in the file that the next `set` writes. Warnings read `the evidence windows.json was not written: ` and the error. The timeline's pod events are written `KIND POD (WORKLOAD)`, joined by `, `.
  - `run.go`: `var completionLimit = 10 * time.Second`. `Run` makes `s.marks = newMarks(w.ReplaceEvidence, s.warnings.add)`; `setUp` calls `s.marks.follow(wt.Finishes())` once the watcher exists. `s.marks.set(s.tl.Windows)` follows each change of a window's start or end: when the windows are fixed, at the install window's end, at the steady window's start and end, at each transition's start, after `placeTransition` or `placeRestart`, and in `place` when it ends a window. `writeMarks` and its call go. `finish`, after the first `Final` that succeeded, calls `completeEvidence` and writes `Final` again when it added a warning, then prints the console. `completeEvidence` calls `s.marks.end()`; writes `timeline.tsv` from `s.probeStart.First` to the end of the last window that began (the clock's time when that end is zero) when the probe gave a first observation; and, when the watcher exists, lists the events into `events.txt` under `context.WithTimeout(context.WithoutCancel(s.parent), completionLimit)`, with the warning `the events were not written: ` and the error.
- [ ] **Step 4: Run the tests to see them pass**
  Run: `go test ./internal/report/ ./internal/run/ ./cmd/rollproof/` and `go test -race ./internal/run/ ./internal/report/` / Expected: PASS
- [ ] **Step 5: Commit**
  `git add internal/run/evidence.go internal/run/evidence_test.go internal/run/run.go internal/run/run_test.go internal/run/fakes_test.go internal/report/evidence.go internal/report/evidence_test.go` and `git commit -m "Write the window marks as they happen, and the timeline and the events before the cluster is deleted"`

### Task 18: kind's logs and the registry's log after a failed set-up

Design: section 10, "What a run writes", "Evidence folder" (the last item); section 9, "The cluster"; section 10, "Reasons" (`cluster-failed`). This plan's decision 17.

**Files:**
- Create: `internal/cluster/logs.go` (`collectLogs`, `(*Cluster).CollectLogs`, `Engine.logs`)
- Modify: `internal/cluster/kindconfig.go` (`Spec.Evidence`), `internal/cluster/cluster.go` (nodes always kept by kind; `fail` takes the logs; `Cluster` keeps the evidence function)
- Modify: `cmd/rollproof/wire.go` (`create` passes the evidence; `Store` takes the logs after a failure)
- Test: `internal/cluster/logs_test.go` (new), `internal/cluster/cluster_test.go`, `cmd/rollproof/main_test.go`

**Interfaces:**
- Produces: `cluster.Spec.Evidence func(name string) (io.WriteCloser, error)`
  Where a failed set-up's logs go: kind's under kind/ and the registry's as
  registry.log. Nil collects nothing.
- Produces: `func (c *Cluster) CollectLogs(ctx context.Context) error`
  Writes kind's logs of the cluster's nodes and the registry container's log
  into Spec.Evidence, for a set-up that failed after Create returned. A
  registry container that the engine does not find gives no registry.log and
  no error. The error joins what could not be collected; what could be is
  written.
- Produces: `func collectLogs(ctx context.Context, e Engine, p *kindcluster.Provider, name, runDir string, evidence func(string) (io.WriteCloser, error)) error`
  The collection that Create's failure and CollectLogs share: kind's logs into
  a folder under runDir, copied file by file to kind/<path>, the folder then
  removed, and the registry's log.
- Produces: `func (e Engine) logs(ctx context.Context, container string) (log string, found bool, err error)`
  The container's output, standard error included. found is false, with no
  error, when the engine's inspect does not find the container.

- [ ] **Step 1: Write the failing tests**
  - `logs_test.go` (build tag `linux`), with two seams set in each test and restored after it, `kindCollectLogs` (default `(*kindcluster.Provider).CollectLogs`) and `containerLogs` (default `Engine.logs`):
    - `TestCollectLogsCopiesEveryFile`: `kindCollectLogs` writes `DIR/kind-version.txt` (`v0.33.0`) and `DIR/rollproof-x-control-plane/kubelet.log` (`kubelet says`); `containerLogs` gives `"registry says"`, true, nil. Assert: nil; the evidence holds exactly `kind/kind-version.txt`, `kind/rollproof-x-control-plane/kubelet.log` and `registry.log`, with those contents; the seam was given the container `rollproof-x-registry`; no folder of kind's logs is left under `runDir`.
    - `TestCollectLogsWithNoRegistry`: `containerLogs` gives `"", false, nil`. Assert: nil; no `registry.log`.
    - `TestCollectLogsGoesOnAfterAnError`: `kindCollectLogs` returns `errors.New("no nodes")`. Assert: the error holds `no nodes`; `registry.log` is written.
    - `TestCollectLogsWithNoEvidence`: a nil evidence function. Assert: nil, and neither seam was called.
  - `cluster_test.go` (tag `cluster`), `TestKeepAfterFailure`: both specs get an `Evidence` that writes files into a `t.TempDir()`. After each failure, that folder holds `kind/kind-version.txt` and at least one file under `kind/` + `s.Name` + `-control-plane/`, and no `registry.log` (the registry image exists nowhere, so its container never started). The present assertions on containers and the run's folder stay.
  - `main_test.go`, `TestStoreFailedTakesTheLogs`, for `storeFailed(ctx context.Context, err error, collect func(context.Context) error) error`:
    - a `cluster-failed` error and a `collect` that returns nil: `collect` was called once, and the error is unchanged;
    - a `collect` that returns `errors.New("no nodes")`: the condition's code stays `cluster-failed` and its text ends with `; the cluster's logs were not all collected: no nodes`;
    - an `internal-error` error, or a ctx that has ended: `collect` is not called.
- [ ] **Step 2: Run them to see them fail**
  Run: `go test ./internal/cluster/ ./cmd/rollproof/ -run 'TestCollectLogs|TestStoreFailedTakesTheLogs' -v` / Expected: FAIL to build (`collectLogs`, the two seams and `storeFailed` are not defined).
- [ ] **Step 3: Implement the four signatures** with the interface comments from the Interfaces block.
  - `logs.go`: `var kindCollectLogs = (*kindcluster.Provider).CollectLogs` and `var containerLogs = Engine.logs`. `collectLogs` runs kind's collection into `filepath.Join(runDir, "kind-logs")`, copies each regular file to `"kind/" + path` by `evidence`, removes the folder, and writes `registry.log` from `containerLogs(e, ctx, name+"-registry")`; it goes on after each error and joins them. `Engine.logs` runs `logs CONTAINER`, and after a failure reads the engine's `inspect CONTAINER` as `removeContainer` does: a failed inspect with ctx not ended is `found` false.
  - `cluster.go`: `Create` passes `kindcluster.CreateWithRetain(true)` whatever `s.Keep` says, and keeps `s.Evidence` on the `Cluster`. In `fail`, after the check of `ctx.Err()` and before the check of `s.Keep`, when `kindBegun` is true it calls `collectLogs(ctx, e, c.provider, s.Name, s.RunDir, s.Evidence)`; an error adds `; the cluster's logs were not all collected: ` and the error to the failure's text. `createKind` still deletes each failed attempt before the next. Rewrite `Create`'s and `Spec`'s comments to say so.
  - `wire.go`: `create` passes `Evidence: evidence` in the `Spec`. `session.Store` returns `storeFailed(ctx, err, s.c.CollectLogs)` when `images.Store` fails: the logs are taken for a `cluster-failed` error while ctx has not ended, and a collection's error adds the same sentence to the condition's text.
- [ ] **Step 4: Run the tests to see them pass**
  Run: `go test ./internal/cluster/ ./cmd/rollproof/` / Expected: PASS. Then `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags cluster -p 1 -count=1 ./internal/cluster/ -run TestKeepAfterFailure -v` / Expected: PASS
- [ ] **Step 5: Commit**
  `git add internal/cluster/logs.go internal/cluster/logs_test.go internal/cluster/kindconfig.go internal/cluster/cluster.go internal/cluster/cluster_test.go cmd/rollproof/wire.go cmd/rollproof/main_test.go` and `git commit -m "Keep kind's logs and the registry's log in the evidence when the set-up fails"`

### Task 19: `docs/probe-contract.md`

Design: section 6 in full, its block and "The contract"; section 8, the `probe.custom` lines of the file's example, "Types" (`env`), "What else is rejected" (the digest) and "Secrets"; section 13 (graduation); section 16, the runs of busybox's shell. This plan's decisions 10 and 11.

**Files:**
- Create: `docs/probe-contract.md`

**Interfaces:**
- Produces: `docs/probe-contract.md`
  The probe contract for authors of custom probes, written from section 6 of
  the design. Until the project graduates, the design is the authority, and
  where the two differ the design wins.

The page is for a person who writes a probe and has not read the design. It states the contract and its reasons in short, gives no history of how it was settled, cites the design by section for the reasons, and names no proof file. Its outline, with the facts each part must hold:

1. **Status.** The page is written from section 6 of `docs/specs/2026-11-13-rollproof-design.md`. Until the project graduates (section 13), that section is the authority, and where the page and it differ, it wins. Fields are only ever added to the line format.
2. **A probe in one minute.** A probe is a container image named with its digest that the tool runs as a pod, with `command`, `args` and `env` as in a pod's container. It prints one JSON line per check. A stock image and a shell loop are a complete probe. The TCP loop of section 6 (with `trap 'exit 0' TERM`, `nc -z -w1 orders-db.orders 5432` and `sleep 0.1`), and the `probe.custom` block that runs it in busybox 1.37.0 by digest, as section 8's example writes it, with an `env` entry that takes a value from a Secret with `valueFrom.secretKeyRef`. Why the `trap` is there: a shell that runs as the first process of its container ignores SIGTERM without one.
3. **Lines.** The table of section 6 unchanged: `ok`, `t`, `n`, `class`, `ms`, `lag`, `detail`, `v`, with type, whether required, and meaning. Standard output and standard error are one stream. Every top-level name is reserved; a probe's own data goes in `detail`. A field the tool does not know is kept and ignored; a `v` it does not know makes the window not verified.
4. **How a line is read.** The seven rules of section 6, in its order: leading white space; log text and the `"ok":` exception; one valid JSON object; a name twice; no `ok` is log text, counted; a bad `ok` or a known field of a wrong type or value; the cut final piece, not judged.
5. **Where a fault lands.** The window that holds the moment the line was written; before the first observation, the run is not verified; after the last window, a warning, except a whole line whose top-level `ok` is `false` (also when it holds `ok` twice and one is `false`), which belongs to the last window. A `t` later than its line, or earlier than the moment the tool asked for the probe's pod by more than 2 s (the clock check's bound), is a fault of the line.
6. **Time.** The default time is the container runtime's timestamp, taken when the runtime has read the whole line. Flush each line as its check ends (Python: `flush=True` or `-u`). `t` is for a result known late, such as a consumer's; it does not make up for buffered output. A line longer than 16 KiB comes back with the time of its first piece. The tool checks at the start that the nodes' clock is the runner's, and owns putting both on one clock. Where an observation is counted: its time's window; before the install window, in the install window; after the last window, not counted. A failed check that began in one window and was written in a later one is counted where it began, and the report says how many crossed a transition's start. The consumer example of section 6, with `t`, `class` and `detail`.
7. **Lifecycle.** Start: started at the first observation, `limits.timeout` to arrive, an exit before it ends the wait at once, a pod that the API server refuses is `probe-not-started` with its text. The install window: failures counted, not judged; the probe needs no warm-up. Exit: restart policy `Never`, judged by the container named `probe`. Silence: measured on when observation lines were written, `limits.silence`, default 5 s, kept shorter than `limits.steady` and `limits.settle`; log text does not end it. Stop: SIGTERM by a pod deletion whose grace period is the drain, 10 s for a custom probe; on SIGTERM start no new check, write every check in flight, exit. A probe still running at the drain's end is killed, and the report says it did not stop when asked. Late results: `limits.settle` at least the longest delay D. The tool follows the log across rotations without losing or doubling a line.
8. **What the author owns.** The rate and the shape of the load; a sequential loop slows with the workload and reports at most one failure per timeout; what a probe cannot see, the tool cannot judge (a new connection per check sees nothing of held connections, which gRPC rollouts break).
9. **Where probe pods run.** The names table: namespace `rollproof-probe`, label `app.kubernetes.io/name: rollproof-probe`, container `probe`; they follow the tool's working name. A target is named with its namespace (`orders-db.orders`). The namespace exists before `setup` runs, so `setup` can add a Secret, rights for the default service account, or a mesh's label. A network policy that denies by default needs a rule beside it that selects `kubernetes.io/metadata.name: rollproof-probe` and the probe's label. A mesh's proxy: only the container `probe` is read and judged; requests then pass through the mesh, whose retries can hide failures (Istio twice by default). The control-plane node, so a Service whose traffic policy is local to the node has no endpoint for the probe.
10. **The custom probe's pod.** User 65532, not root, no privilege escalation, every capability dropped, the runtime's default seccomp profile: the `restricted` Pod Security level. The program must work as user 65532 whatever user its image names; the busybox loop does. The pod mounts the namespace's default service account's token, so a probe can read the API with the rights `setup` gave it. An image that `images` does not list is pulled by the kubelet with the pod, `IfNotPresent`.
11. **What the report says of a custom probe.** Its image reference with the digest, the names of its `env` entries and the counts of its `command` and `args`, never their values; "a custom probe judged this run; the tool does not know what it checks"; "probed" reads "not known"; "whatever the custom probe does not check" stands in the list of what a pass did not exercise.

- [ ] **Step 1: Write the page** from the outline, each fact copied from the design's text, never from memory, at a line width of 100 like the design.
- [ ] **Step 2: Check the facts that are copied by hand**
  Run: `grep -c -e 'rollproof-probe' -e 'app.kubernetes.io/name' -e '65532' -e '16 KiB' -e 'flush=True' -e "trap 'exit 0' TERM" -e 'the design wins' docs/probe-contract.md` / Expected: a count of 7 or more, and every one of the eight field names of step 3 of the outline in its table.
- [ ] **Step 3: Read the page against section 6** line by line: each number (2 s, 5 s, 10 s, 16 KiB, 65532, Istio's two retries) and each name equals the design's.
- [ ] **Step 4: Commit**
  `git add docs/probe-contract.md` and `git commit -m "Add the probe contract for authors of custom probes"`
