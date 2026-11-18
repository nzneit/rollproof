> Part of the plan in `README.md` of this folder. Read that file first: its Global Constraints and its rulings apply to every task here.

### Task 10: The install window counts the checks that began before it

Read section 6, the paragraph that begins "An observation is assigned to the window that holds its time", section 4's paragraph on the time before the first observation, section 10's install item of `result.json`, and section 14, point 25. This task was added after the final review (ruling 17). It needs no cluster until its last step.

**Files:**
- Modify: `internal/verdict/observe.go`, `internal/verdict/decide.go` (the comment of `Decision.Uncounted` only)
- Test: `internal/verdict/observe_test.go`

**Interfaces:**
- Consumes: the window name `Install` and the install window as `Timeline.Windows[0]` (Task 2).
- Produces: no new operation. `Decision.Uncounted` keeps its type; it no longer counts an observation that began before the install window.

- [ ] **Step 1: Write the failing tests**

In `internal/verdict/observe_test.go`, `TestObserve`, change two rows and add two. Keep the rows' other fields as they are.

- The row "an observation that began before the install window is not counted" becomes "an observation that began before the install window is counted in it". Its `Install` tally becomes `{Checks: 3050, SlowestMs: 30, LagMs: 5}`, and it has no `uncounted` (zero).
- The row "one check passed, before the install window: the line is left out" becomes "one check passed, before the install window: it is counted there, and the steady window's reason has no second line". It has no `uncounted` (zero). Its `want` and `texts` stay.
- A new row after the first of those: "a failed check that began before the install window and was written in it is counted there", with `timeline(10*sec, withInstall(steadyOnly()), passes(-15*sec, 10*sec), []Observation{failure(-15500*msec, -13500*msec, "timeout")})`, `want` a pass (`Results: []Result{ResultPass, ResultPass, NotRun, NotRun}`, `GoOn: true`), tallies `Install: {Checks: 3001, Failed: 1, ByClass: map[string]int{"timeout": 1}, FirstFailure: at(-15500 * msec), SlowestMs: 30, LagMs: 5}` and `Steady: clean`, and no `uncounted`. (`tally` does not carry `LastFailure`; `TestLastFailure` covers that time.)
- A new row after that: "with no install window, an observation before the steady window is not counted", with `timeline(10*sec, steadyOnly(), passes(0, 10*sec), []Observation{{Written: at(500 * msec), T: at(-500 * msec), OK: true, N: 50, Ms: 30, Lag: 5}})`, `want` as the row "ten summary lines of N 200 in steady", tallies `Steady: clean`, and `uncounted: 1`.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/verdict/ -run TestObserve -count=1`
Expected: FAIL in the first three rows above (the install tally short by the early observations, and `uncounted` 1 where 0 is wanted). The fourth row passes already.

- [ ] **Step 3: Count an early observation in the install window**

In `internal/verdict/observe.go`, `countObservations`: an observation that no begun window holds, whose time is before the first window's `Start`, is counted in that window when the first window is the install window (`t.Windows[0].Name == Install`). Every other observation that no window holds still adds to `p.uncounted`. The steps after the counting (the time of the last failure, `AfterFinish`, the crossings) treat it as one of the install window's. Say so in the comment of `countObservations`, and in `observe`'s comment where it names "the lines that no window holds". In `internal/verdict/decide.go`, the comment of `Decision.Uncounted` says: the number of observations that no window counts, which are those after the last window ended, and those before the first window when the run has no install window.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: PASS for every package.

- [ ] **Step 5: Commit**

```bash
git add internal/verdict/observe.go internal/verdict/decide.go internal/verdict/observe_test.go
git commit -m "Count in the install window the checks that began before it"
```

- [ ] **Step 6: See it in a cluster**

Run, on a machine whose engine is nerdctl, after building the binary as Task 8's Step 5 does: `env KIND_EXPERIMENTAL_PROVIDER=nerdctl go test -tags e2e -p 1 -count=1 -timeout 60m -v -run TestFirstFixture ./e2e/`
Expected: PASS under `helm-3` and `helm-4`. In each run's results folder, the failed checks in `evidence/probe.log` whose `t` is before the steady window's start number as many as `result.json`'s `install.failed`, class by class.
