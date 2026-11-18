> Part of the plan in `README.md` of this folder. Read that file first: its Global Constraints and its rulings apply to every task here.

### Task 1: `watch`: when the install has settled

Read section 7 from "When the install is ready" to "Pods replaced", and "What is watched".

**Files:**
- Create: `internal/watch/settle.go`
- Modify: `internal/watch/counts.go` (the type `Install`, and `InstallState`), `internal/watch/tracker.go` (the `Tracker` gains one field; `Set` and `Gone` each end with one call), `internal/watch/watcher.go` (`InstallSettled`)
- Test: `internal/watch/settle_test.go` (new), `internal/watch/counts_test.go`, `internal/watch/watcher_test.go`

**Interfaces:**
- Consumes: `(*Tracker).InstallState`, `Complete`, `Rolling`, the unexported `unsupported(ws, updateType)` and `(*Tracker).updateType`, and the Tracker's two records of what its watch missed, `missedOutside` and `down` (all built, in `counts.go` and `tracker.go`).
- Produces: `type Install struct { Ready time.Time; Workloads []Count }`
  What the install left when it was ready: the moment the Tracker first
  found it ready, and every watched workload with the digests its pods run.
- Produces: `func (t *Tracker) InstallState(began, exited, now time.Time) (Install, bool, error)`
  As built, and one thing more: from the first call that reports ready,
  the Tracker notes every break of the install's conditions, and began is
  the install's start for good. Nothing-watched is reported only before
  that call.
- Produces: `type Settling struct { First, Settled, Ended time.Time; Restarts []string; Before, InWindow []verdict.Condition }`
  The install from ready until it settled under the probe. First is the
  probe's first observation, as given. Settled is zero when the install
  did not settle. Ended is when the wait ended, never before First.
  Restarts say what started the settle time again. Before holds what
  happened before First, InWindow what happened from it on, each with its
  moment in At.
- Produces: `func (t *Tracker) InstallSettling(first, now time.Time) (Settling, bool)`
  The settling so far, and whether it has ended: the install settled or
  failed, or a workload the tool cannot judge was seen before first. first
  is when the probe's first observation was written, the same at every
  call. Once it has ended, every later call gives the same answer. It
  panics when InstallState has not reported ready.
- Produces: `func (w *Watcher) InstallSettled(ctx context.Context, first time.Time) Settling`
  Returns when the settling has ended, as InstallSettling says.
  When ctx ends first, it returns what it has: Settled zero, Ended that
  moment, and no condition. Call it once, after InstallReady has returned
  with no error.

**Error surface** (audited before the comments above were written):

| Item | Disposition | Reason | Test |
| --- | --- | --- | --- |
| The install's conditions do not hold when the limit passes, or break after it (`install-failed`) | expose | `run` places it before the first observation or in the install window, and the run ends not verified (section 7) | `TestInstallSettling`, four rows |
| A workload that does not roll is seen after the install was ready (`workload-unsupported`) | expose | `run` places it the same way (rule 4, rule 9); one seen before the first observation ends the settling there (ruling 5) | `TestInstallSettling`, two rows |
| The watch missed changes in the install window (`internal-error`) | expose | `run` places it in the install window (ruling 8) | `TestInstallSettling`, three rows |
| No workload is watched after the install was ready | define-out | Section 7 makes it a break like any other; `nothing-watched` is a reason of the time before readiness only | `TestInstallSettling`, "the last watched workload is removed" |
| `ctx` ends while `InstallSettled` waits | define-out | The operation is "wait until settled, failed or told to stop"; the caller that ended ctx reports the signal itself | `TestInstallSettled`, "when ctx ends" |
| `InstallSettling` before the install was ready | crash | A mistake of the caller's order, as `Begin` twice is | `TestInstallSettlingBeforeTheInstallIsReady` |

Handlers callers must write: 1. `run` appends `Before` and `InWindow` to their two places; it reads no code.

- [ ] **Step 1: Write the failing tests for the rule**

Create `internal/watch/settle_test.go`. It uses the builders of `rollout_test.go` and `tracker_test.go` (`t0`, `at`, `options`, `base`, `set`, `gone`, `deployment`, `daemonSet`, `pod`) and `play` of `counts_test.go`.

```go
package watch

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"rollproof/internal/verdict"
)

// readyAt2 is a Tracker that was told the release of base() at +1 s and found its install,
// begun at t0, ready at +2 s.
func readyAt2(t *testing.T, o TrackerOptions) *Tracker {
	t.Helper()
	tr := NewTracker(o)
	for _, obj := range base() {
		tr.Set(obj, at(1))
	}
	inst, ready, err := tr.InstallState(t0, at(2), at(2))
	if err != nil || !ready || !inst.Ready.Equal(at(2)) {
		t.Fatalf("InstallState = %+v, %v, %v; want ready, with Ready at +2 s", inst, ready, err)
	}
	return tr
}

func TestInstallSettling(t *testing.T) {
	whole := func() runtime.Object { return deployment("orders", 1, 1, 2, 2, 2, 2) }
	oneDown := func() runtime.Object { return deployment("orders", 1, 1, 2, 2, 1, 2) } // a pod stopped being ready
	_, waiting := Complete(oneDown())
	notComplete := func(s float64) string {
		return fmt.Sprintf("deployment/orders was not complete %.1f s after the install was ready: %s", s, waiting)
	}
	const noWorkload = "no workload was watched 8.0 s after the install was ready"
	agent := daemonSet("agent", 1, 1, 3, 3, 3, appsv1.OnDeleteDaemonSetStrategyType)
	const agentText = "daemonset/agent updates with OnDelete; the tool judges only a workload that updates by rolling"
	stopping := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, true)
	const atLimit = "the install was not ready when limits.timeout passed\n"
	const afterLimit = "the install stopped being ready after limits.timeout, while it still settled\n"
	cond := func(code verdict.Code, s float64, text string) []verdict.Condition {
		return []verdict.Condition{{Code: code, At: at(s), Text: text}}
	}

	for _, r := range []struct {
		name     string
		short    bool    // limits.timeout is 20 s, so the install's limit is at +20 s; otherwise +300 s
		events   []ev    // told after the install was ready at +2 s
		missed   float64 // above zero: Missed is told at this moment, after the events
		down     float64 // above zero: a watch goes down at this moment, after the events, and stays down
		first    float64 // the probe's first observation
		now      float64
		ended    bool
		settled  float64 // negative: it did not settle
		end      float64 // Ended, when ended
		restarts []string
		before   []verdict.Condition
		inWindow []verdict.Condition
	}{
		{name: "a tenth of a second before the settle time has passed", first: 5, now: 19.9, settled: -1},
		{name: "the first observation is after the conditions last came to hold",
			first: 5, now: 20, ended: true, settled: 20, end: 20},
		{name: "the first observation is before the conditions last came to hold",
			events: []ev{set(4, oneDown()), set(8, whole())}, first: 5, now: 23, ended: true, settled: 23, end: 23,
			restarts: []string{notComplete(2)}},
		{name: "a break that was over before the first observation is not counted",
			events: []ev{set(3, oneDown()), set(4, whole())}, first: 5, now: 20, ended: true, settled: 20, end: 20},
		{name: "a pod that stops being ready starts the time again",
			events: []ev{set(10, oneDown()), set(12, whole())}, first: 5, now: 26.9, settled: -1,
			restarts: []string{notComplete(8)}},
		{name: "and the install settles limits.settle after the pod is ready again",
			events: []ev{set(10, oneDown()), set(12, whole())}, first: 5, now: 27, ended: true, settled: 27, end: 27,
			restarts: []string{notComplete(8)}},
		{name: "a deleted pod that is not gone is a break",
			events: []ev{set(10, stopping), gone(13, stopping)}, first: 5, now: 28, ended: true, settled: 28, end: 28,
			restarts: []string{"pod orders-a-1 of deployment/orders was deleted and not gone 8.0 s after the install was ready"}},
		{name: "the last watched workload is removed: a break, and never nothing-watched",
			events: []ev{gone(10, whole())}, first: 5, now: 299.9, settled: -1, restarts: []string{noWorkload}},
		{name: "and the install has failed when the limit passes",
			events: []ev{gone(10, whole())}, first: 5, now: 300, ended: true, settled: -1, end: 300,
			restarts: []string{noWorkload}, inWindow: cond(verdict.InstallFailed, 300, atLimit+noWorkload)},
		{name: "a workload of a strategy other than the rolling one appears",
			events: []ev{set(9, agent)}, first: 5, now: 20, ended: true, settled: 20, end: 20,
			inWindow: cond(verdict.WorkloadUnsupported, 9, agentText)},
		{name: "and one that appeared before the first observation ends the settling there, with no window",
			events: []ev{set(3, agent)}, first: 5, now: 5, ended: true, settled: -1, end: 5,
			before: cond(verdict.WorkloadUnsupported, 3, agentText)},
		{name: "the settle time runs past the limit", short: true, first: 10, now: 25, ended: true, settled: 25, end: 25},
		{name: "a break after the limit, before the install has settled, fails the install",
			short: true, events: []ev{set(22, oneDown())}, first: 10, now: 22, ended: true, settled: -1, end: 22,
			restarts: []string{notComplete(20)}, inWindow: cond(verdict.InstallFailed, 22, afterLimit+notComplete(20))},
		{name: "a break that is over when the limit passes does not fail the install",
			short: true, events: []ev{set(18, oneDown()), set(19, whole())}, first: 5, now: 34, ended: true, settled: 34, end: 34,
			restarts: []string{notComplete(16)}},
		{name: "a break that lasts across the limit fails the install at the limit",
			short: true, events: []ev{set(18, oneDown())}, first: 5, now: 20, ended: true, settled: -1, end: 20,
			restarts: []string{notComplete(16)}, inWindow: cond(verdict.InstallFailed, 20, atLimit+notComplete(16))},
		{name: "an install that failed before the first observation has no window",
			short: true, events: []ev{set(18, oneDown())}, first: 30, now: 30, ended: true, settled: -1, end: 30,
			restarts: []string{notComplete(16)}, before: cond(verdict.InstallFailed, 20, atLimit+notComplete(16))},
		{name: "a change after the install had settled, told before anyone asked, is not counted",
			events: []ev{set(21, oneDown())}, first: 5, now: 25, ended: true, settled: 20, end: 20},
		{name: "the watch missed changes in the install window",
			missed: 9, first: 5, now: 20, ended: true, settled: 20, end: 20,
			inWindow: cond(verdict.InternalError, 9, "the watch of the cluster missed changes 4.0 s after the probe's first observation")},
		{name: "the watch missed changes before the first observation only",
			missed: 3, first: 5, now: 20, ended: true, settled: 20, end: 20},
		{name: "a watch that went down before the first observation and is still down",
			down: 3, first: 5, now: 20, ended: true, settled: 20, end: 20,
			inWindow: cond(verdict.InternalError, 5, "the watch of the cluster missed changes 0.0 s after the probe's first observation")},
	} {
		t.Run(r.name, func(t *testing.T) {
			o := options()
			if r.short {
				o.Timeout = 20 * time.Second
			}
			tr := readyAt2(t, o)
			play(tr, r.events)
			if r.missed > 0 {
				tr.Missed(at(r.missed))
			}
			if r.down > 0 {
				tr.watchDown("pods", at(r.down))
			}
			want := Settling{First: at(r.first), Restarts: r.restarts, Before: r.before, InWindow: r.inWindow}
			if r.settled >= 0 {
				want.Settled = at(r.settled)
			}
			if r.ended {
				want.Ended = at(r.end)
			}
			got, ended := tr.InstallSettling(at(r.first), at(r.now))
			if ended != r.ended {
				t.Errorf("ended = %v, want %v", ended, r.ended)
			}
			if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("settling (-want +got):\n%s", diff)
			}
			if !ended {
				return
			}
			// Once it has ended, neither a later change nor a later call alters the answer.
			tr.Set(oneDown(), at(r.now+1))
			again, still := tr.InstallSettling(at(r.first), at(r.now+60))
			if diff := cmp.Diff(got, again, cmpopts.EquateEmpty()); diff != "" || !still {
				t.Errorf("a later call: ended %v, (-first +later):\n%s", still, diff)
			}
		})
	}
}

func TestInstallSettlingBeforeTheInstallIsReady(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("InstallSettling did not panic before InstallState had reported ready")
		}
	}()
	NewTracker(options()).InstallSettling(at(5), at(20))
}
```

In `internal/watch/counts_test.go`, two rows gain the moment of readiness: in `TestInstallState` the row "ready" expects `Install{Ready: at(3), Workloads: …}`, and in `TestInstallStateFromItsRecords` the row "ready once that pod is gone, with the digests of the pods left" expects `Install{Ready: at(4), Workloads: …}`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/watch/ -run 'TestInstallSettling|TestInstallState' -count=1`
Expected: FAIL at the build, with "undefined: Settling", "tr.InstallSettling undefined" and "unknown field Ready".

- [ ] **Step 3: Implement the rule in `internal/watch/settle.go`, with its interface comments from the Interfaces block**

The Tracker keeps this from the first ready `InstallState` on, in a new field `install *installTrack`:

```go
// installTrack is what the Tracker keeps of the install from the moment it was ready.
type installTrack struct {
	began, ready time.Time
	breaks       []installBreak         // in the order they began; only the last can be open
	unsupported  map[Workload]time.Time // each watched workload that does not roll, with when it was first seen so
	over         *Settling              // the answer, once the settling has ended
}

// installBreak is a stretch in which the install's conditions did not hold. until is zero while
// it lasts. sentence says what did not hold when it began.
type installBreak struct {
	from, until time.Time
	sentence    string
}
```

- `InstallState`, at the first call that reports ready, sets `Install.Ready` to `now` and makes the track with `began` and `ready: now`. Every ready answer carries that same `Ready`.
- `Set` and `Gone` each end with `t.noteInstall(at)`. It does nothing before the install was ready and after the settling has ended. Otherwise it tests the conditions that `InstallState` tests for "ready" (at least one workload exists whose labels matched at its last Set; each of those that rolls is complete; none of their pods has been deleted and is not gone), opens a break at `at` when they stopped holding, closes the open break at `at` when they hold again, and notes each watched workload that does not roll with the first `at` it was seen so.
- A break's sentence names the first thing that does not hold, in this order, with the seconds from `ready` to the break's beginning: `no workload was watched %.1f s after the install was ready`; for the first workload, by kind and then name, that is not complete, `%s was not complete %.1f s after the install was ready: %s` with the workload and the message of `Complete`; for the first pod, by name, that was deleted and is not gone, `pod %s of %s was deleted and not gone %.1f s after the install was ready`.
- `InstallSettling` walks the breaks. This is the whole rule of section 7, limit included:

```go
// outcome gives the moment the install settled, or the moment it failed and the break that
// failed it. Both moments are zero while neither has come by now. counted are the breaks that
// started the settle time again.
func (in *installTrack) outcome(first, now time.Time, settle, timeout time.Duration) (settled, failed time.Time, cause *installBreak, counted []installBreak) {
	limit := in.began.Add(timeout)
	from := first // the settle time is counted from here
	for i := range in.breaks {
		b := &in.breaks[i]
		if !b.from.Before(from.Add(settle)) {
			break // the install had settled before this break began
		}
		open := b.until.IsZero()
		if open || b.until.After(first) {
			counted = append(counted, *b)
		}
		switch {
		case b.from.After(limit):
			return time.Time{}, b.from, b, counted // it broke after the limit, before it had settled
		case open && !now.Before(limit), !open && b.until.After(limit):
			return time.Time{}, limit, b, counted // it did not hold when the limit passed
		case open:
			return time.Time{}, time.Time{}, nil, counted // broken now, inside the limit
		}
		from = later(from, b.until)
	}
	if end := from.Add(settle); !now.Before(end) {
		return end, time.Time{}, nil, counted
	}
	return time.Time{}, time.Time{}, nil, counted
}
```

- From that outcome: `Restarts` are the sentences of `counted`, at every call. `Settled` and `Ended` are the settle moment. After a failure `Ended` is the later of the failure's moment and `first`.
- A watched workload that does not roll, first seen before `first`, ends the settling at `first`, whatever the breaks show: `Settled` is zero and `Ended` is `first`. Section 7: found before the probe's first observation, it ends the run (ruling 5).
- The conditions, added only when the settling has ended. Each goes to `Before` when its `At` is before `first`, and to `InWindow` otherwise. Within a list the order is `install-failed`, `workload-unsupported`, `internal-error`.
  - `install-failed`, `At` the failure's moment. Its text is `the install was not ready when limits.timeout passed` for a failure at the limit, or `the install stopped being ready after limits.timeout, while it still settled` for a break after it; then a line end and the sentence of `cause`.
  - `workload-unsupported`, from `unsupported(ws, t.updateType)`: one condition for the workloads first seen before `first`, and one for those first seen from `first` to `Ended`, each with the earliest of its moments in `At`, and `ws` in order of kind and then name.
  - `internal-error`, with the text `the watch of the cluster missed changes %.1f s after the probe's first observation` and `At` the first moment m with `first` ≤ m ≤ `Ended` that the watch may have missed: for a stretch of `missedOutside` that ends at or after `first` and begins at or before `Ended`, the later of its beginning and `first`; for a watch in `down` since d, with d at or before `Ended`, the later of d and `first`. It reads those two records and changes neither.
- When the settling has ended, the answer is kept in `over` and returned by every later call.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/watch/ -run 'TestInstallSettling|TestInstallState' -count=1`
Expected: PASS.

- [ ] **Step 5: Write the failing tests for the Watcher**

Add to `internal/watch/watcher_test.go`. `startWatcher`, `drive`, `receive` and `closed` are that file's.

```go
func TestInstallSettled(t *testing.T) {
	t.Run("it settles limits.settle after the first observation", func(t *testing.T) {
		w, _, clock := startWatcher(t, nil, base()...)
		if _, err := w.InstallReady(t.Context(), t0, closed()); err != nil {
			t.Fatal(err)
		}
		first := clock.Now().Add(3 * time.Second)
		res := make(chan Settling, 1)
		go func() { res <- w.InstallSettled(t.Context(), first) }()
		s := drive(t, clock, res)
		if !s.Settled.Equal(first.Add(15*time.Second)) || !s.Ended.Equal(s.Settled) || len(s.Before)+len(s.InWindow) != 0 {
			t.Errorf("settling = %+v; want Settled and Ended 15 s after the first observation, and no condition", s)
		}
	})
	t.Run("when ctx ends it returns what it has", func(t *testing.T) {
		w, _, clock := startWatcher(t, nil, base()...)
		if _, err := w.InstallReady(t.Context(), t0, closed()); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		res := make(chan Settling, 1)
		go func() { res <- w.InstallSettled(ctx, clock.Now()) }()
		cancel()
		s := receive(t, res)
		if !s.Settled.IsZero() || !s.Ended.Equal(clock.Now()) || len(s.Before)+len(s.InWindow) != 0 {
			t.Errorf("settling = %+v; want Settled zero, Ended at the clock's time, and no condition", s)
		}
	})
}
```

- [ ] **Step 6: Run the tests to verify they fail**

Run: `go test ./internal/watch/ -run TestInstallSettled -count=1`
Expected: FAIL at the build, with "w.InstallSettled undefined".

- [ ] **Step 7: Implement `InstallSettled` in `internal/watch/watcher.go`, with its interface comment from the Interfaces block**

It waits as `follow` does: it asks the Tracker under `w.mu` with `w.o.Now()`, and asks again on a tick of `pollEvery`, on `w.wake`, or when ctx ends. The settle moment comes from the Tracker's times, never from the moment of asking.

- [ ] **Step 8: Run the package's tests**

Run: `go test ./internal/watch/ -count=1`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/watch/settle.go internal/watch/settle_test.go internal/watch/counts.go internal/watch/counts_test.go internal/watch/tracker.go internal/watch/watcher.go internal/watch/watcher_test.go
git commit -m "Add the rule for when the install has settled to watch"
```

### Task 2: `verdict`: the install window

Read section 10, "Rules of the verdict", and in section 6 the paragraphs "Time" and "Lifecycle".

**Files:**
- Modify: `internal/verdict/vocab.go`, `internal/verdict/decide.go`, `internal/verdict/observe.go`
- Test: `internal/verdict/decide_test.go`, `internal/verdict/observe_test.go`

**Interfaces:**
- Consumes: nothing from another task.
- Produces: `const Install WindowName = "install"`
  The install window: from the probe's first observation until the
  install has settled. It is a window for the rules and for a reason's
  Window. A report lists it apart from the other windows.
- Produces: `func Decide(t Timeline) Decision`
  As built, and the install window is judged as the first of
  t.Windows when the caller puts it there: a failed check in it is
  counted and gives no condition, and any condition makes it not
  verified. Before is what happened before the first window. Crossed is
  counted for a transition's window only.
- Produces: `type WindowResult struct { Name WindowName; Start, End time.Time; Result Result; Checks, Failed, AfterFinish, Crossed int; ByClass map[string]int; FirstFailure, LastFailure time.Time; SlowestMs, LagMs float64 }`
  As built, with LastFailure: the latest time at which a check that
  failed in this window began, zero when nothing failed.

- [ ] **Step 1: Write the failing tests for the rules**

Add to `internal/verdict/decide_test.go`, beside `steady`, `deploy` and `rollback`:

```go
// The install window of the tables: [-15 s, 0 s), before the steady window.
func install(cs ...Condition) Window {
	return Window{Name: Install, Start: at(-15 * sec), End: at(0), Conditions: cs}
}

// withInstall puts the install window before ws.
func withInstall(ws []Window) []Window { return append([]Window{install()}, ws...) }
```

Add these rows to `placedRows()`:

```go
	}, {
		name: "the install window and the three windows ended with no condition",
		tl:   Timeline{Windows: []Window{install(), steady(), deploy(), rollback()}, Now: at(70 * sec)},
		want: summary{Verdict: Pass, Results: []Result{ResultPass, ResultPass, ResultPass, ResultPass}, GoOn: true},
	}, {
		name: "the install window ended clean and the steady window has not begun: the run goes on",
		tl:   Timeline{Windows: []Window{install(), notBegun(Steady), notBegun(Deploy), notBegun(Rollback)}, Now: at(0)},
		want: summary{Verdict: Pass, Results: []Result{ResultPass, NotRun, NotRun, NotRun}, GoOn: true},
	}, {
		name: "the install window holds only a fail code; it is not verified all the same",
		tl:   Timeline{Windows: []Window{install(cond(CommandFailed)), notBegun(Steady), notBegun(Deploy), notBegun(Rollback)}, Now: at(0)},
		want: summary{Verdict: NotVerified, Results: []Result{ResultNotVerified, NotRun, NotRun, NotRun}, Reasons: []string{"install:command-failed"}, Decided: CommandFailed},
	}, {
		name: "two conditions in the install window; the earlier in Codes decides",
		tl:   Timeline{Windows: []Window{install(cond(ProbeSilent), cond(InstallFailed)), notBegun(Steady), notBegun(Deploy), notBegun(Rollback)}, Now: at(0)},
		want: summary{Verdict: NotVerified, Results: []Result{ResultNotVerified, NotRun, NotRun, NotRun}, Reasons: []string{"install:install-failed", "install:probe-silent"}, Decided: InstallFailed},
	}, {
		name: "a timed fault of the probe inside the install window",
		tl:   Timeline{Windows: []Window{install(), notBegun(Steady), notBegun(Deploy), notBegun(Rollback)}, Timed: []Condition{timed(ProbeExited, -5*sec)}, Now: at(0)},
		want: summary{Verdict: NotVerified, Results: []Result{ResultNotVerified, NotRun, NotRun, NotRun}, Reasons: []string{"install:probe-exited"}, Decided: ProbeExited},
	}, {
		name: "a timed fault before the probe's first observation has no window",
		tl:   Timeline{Windows: []Window{install(), steady(), notBegun(Deploy), notBegun(Rollback)}, Timed: []Condition{timed(ProbeLineInvalid, -16*sec)}, Now: at(10 * sec)},
		want: summary{Verdict: NotVerified, Results: []Result{NotJudged, NotJudged, NotRun, NotRun}, Reasons: []string{":probe-line-invalid"}, Decided: ProbeLineInvalid},
	}, {
		name: "a signal while the probe stops, after a run that ended in the install window",
		tl:   Timeline{Windows: []Window{install(cond(Interrupted), cond(ProbeSilent)), notBegun(Steady), notBegun(Deploy), notBegun(Rollback)}, Now: at(2 * sec)},
		want: summary{Verdict: NotVerified, Results: []Result{ResultNotVerified, NotRun, NotRun, NotRun}, Reasons: []string{"install:probe-silent", "install:interrupted"}, Decided: ProbeSilent},
	}, {
		name: "the steady window began, but the install window before it never did",
		tl:   Timeline{Windows: []Window{notBegun(Install), steady(), notBegun(Deploy), notBegun(Rollback)}, Now: at(10 * sec)},
		want: summary{Verdict: NotVerified, Results: []Result{NotRun, NotJudged, NotRun, NotRun}, Reasons: []string{":internal-error"}, Decided: InternalError},
```

And this test, for rule 4 on time and rule 8 late:

```go
// Each of these makes the install window not verified, whether it is placed while the window is
// the last that began or after the steady window has ended.
func TestInstallWindowNotVerified(t *testing.T) {
	for _, c := range []Code{ProbeSilent, ProbeExited, ProbeLineInvalid, InstallFailed, WorkloadUnsupported, Interrupted, InternalError} {
		rest := []Window{notBegun(Deploy), notBegun(Rollback)}
		for name, row := range map[string]struct {
			windows []Window
			now     time.Duration
			results []Result
		}{
			"on time":     {append([]Window{install(cond(c)), notBegun(Steady)}, rest...), 0, []Result{ResultNotVerified, NotRun, NotRun, NotRun}},
			"placed late": {append([]Window{install(cond(c)), steady()}, rest...), 10 * sec, []Result{ResultNotVerified, NotJudged, NotRun, NotRun}},
		} {
			t.Run(string(c)+", "+name, func(t *testing.T) {
				want := summary{Verdict: NotVerified, Results: row.results, Reasons: []string{"install:" + string(c)}, Decided: c}
				got := summarize(t, Decide(worked(Timeline{Windows: row.windows, Now: at(row.now)})))
				if !reflect.DeepEqual(got, want) {
					t.Errorf("got  %+v\nwant %+v", got, want)
				}
			})
		}
	}
}
```

- [ ] **Step 2: Write the failing tests for the probe's lines in the install window**

Add to `internal/verdict/observe_test.go`:

```go
// failures gives one failed check a second, written when it began, from `from` up to but not
// including `to`: class 503.
func failures(from, to time.Duration) []Observation {
	var obs []Observation
	for d := from; d < to; d += sec {
		obs = append(obs, failure(d, d, "503"))
	}
	return obs
}
```

Add these rows to `TestObserve`. In them the install window is `[-15 s, 0 s)`, so `passes(-15*sec, 10*sec)` gives it 3,000 checks and the steady window 2,000.

```go
	}, {
		name: "failed checks in the install window are counted and decide nothing",
		tl: timeline(10*sec, withInstall(steadyOnly()), passes(-15*sec, 10*sec),
			[]Observation{failure(-14900*msec, -14900*msec, "refused"), failure(-14800*msec, -12800*msec, "timeout")}),
		want: summary{Verdict: Pass, Results: []Result{ResultPass, ResultPass, NotRun, NotRun}, GoOn: true},
		tallies: map[WindowName]tally{
			Install: {Checks: 3002, Failed: 2, ByClass: map[string]int{"refused": 1, "timeout": 1}, FirstFailure: at(-14900 * msec), SlowestMs: 30, LagMs: 5},
			Steady:  clean,
		},
	}, {
		name: "an observation that began before the install window is not counted",
		tl: timeline(10*sec, withInstall(steadyOnly()), passes(-15*sec, 10*sec),
			[]Observation{{Written: at(-14500 * msec), T: at(-15500 * msec), OK: true, N: 50, Ms: 30, Lag: 5}}),
		want:      summary{Verdict: Pass, Results: []Result{ResultPass, ResultPass, NotRun, NotRun}, GoOn: true},
		tallies:   map[WindowName]tally{Install: {Checks: 3000, SlowestMs: 30, LagMs: 5}, Steady: clean},
		uncounted: 1,
	}, {
		name: "a failed check that began in the install window and was written in the steady window",
		tl:   timeline(10*sec, withInstall(steadyOnly()), passes(-15*sec, 10*sec), []Observation{failure(-200*msec, 300*msec, "timeout")}),
		want: summary{Verdict: Pass, Results: []Result{ResultPass, ResultPass, NotRun, NotRun}, GoOn: true},
		tallies: map[WindowName]tally{
			Install: {Checks: 3001, Failed: 1, ByClass: map[string]int{"timeout": 1}, FirstFailure: at(-200 * msec), SlowestMs: 30, LagMs: 5},
			Steady:  clean, // its Crossed stays 0: only a transition's start is crossed
		},
	}, {
		name:  "the probe is silent for 7 s in the install window and then writes again",
		tl:    timeline(10*sec, withInstall(steadyOnly()), passes(-15*sec, -9*sec), passes(-3*sec, 10*sec)),
		want:  summary{Verdict: NotVerified, Results: []Result{ResultNotVerified, NotJudged, NotRun, NotRun}, Reasons: []string{"install:probe-silent"}, Decided: ProbeSilent},
		texts: map[string]string{"install:probe-silent": "no observation for 7.0 s, from 09:59:50.00"},
	}, {
		name: "a silence that begins in the install window and ends in the steady window; the install window decides",
		tl:   timeline(10*sec, withInstall(steadyOnly()), passes(-15*sec, -2*sec), passes(4*sec, 10*sec)),
		want: summary{Verdict: NotVerified, Results: []Result{ResultNotVerified, NotJudged, NotRun, NotRun}, Reasons: []string{"install:probe-silent"}, Decided: ProbeSilent},
	}, {
		name: "a line that breaks the contract in the install window",
		tl:   withTimed(timeline(10*sec, withInstall(steadyOnly()), passes(-15*sec, 10*sec)), timed(ProbeLineInvalid, -5*sec)),
		want: summary{Verdict: NotVerified, Results: []Result{ResultNotVerified, NotJudged, NotRun, NotRun}, Reasons: []string{"install:probe-line-invalid"}, Decided: ProbeLineInvalid},
	}, {
		name: "a failed check in the install window and one in the steady window: only the second is judged",
		tl: timeline(10*sec, withInstall(steadyOnly()), passes(-15*sec, 10*sec),
			[]Observation{failure(-10*sec, -10*sec, "refused"), failure(4500*msec, 4500*msec, "refused")}),
		want:  summary{Verdict: NotVerified, Results: []Result{ResultPass, ResultNotVerified, NotRun, NotRun}, Reasons: []string{"steady:steady-not-clean"}, Decided: SteadyNotClean},
		texts: map[string]string{"steady:steady-not-clean": "1 check failed (refused 1), first at 10:00:04.50"},
	}, {
		name: "no check has passed since the probe started",
		tl:   timeline(10*sec, withInstall(steadyOnly()), failures(-15*sec, 10*sec)),
		want: summary{Verdict: NotVerified, Results: []Result{ResultPass, ResultNotVerified, NotRun, NotRun}, Reasons: []string{"steady:steady-not-clean"}, Decided: SteadyNotClean},
		texts: map[string]string{"steady:steady-not-clean": "10 checks failed (503 10), first at 10:00:00.00\n" +
			"no check has passed since the probe started: every check failed for 25.0 s"},
	}, {
		name: "one check passed, before the install window: the line is left out",
		tl: timeline(10*sec, withInstall(steadyOnly()), failures(-15*sec, 10*sec),
			[]Observation{{Written: at(-14500 * msec), T: at(-15500 * msec), OK: true, N: 1, Ms: 30, Lag: 5}}),
		want:      summary{Verdict: NotVerified, Results: []Result{ResultPass, ResultNotVerified, NotRun, NotRun}, Reasons: []string{"steady:steady-not-clean"}, Decided: SteadyNotClean},
		texts:     map[string]string{"steady:steady-not-clean": "10 checks failed (503 10), first at 10:00:00.00"},
		uncounted: 1,
	}, {
		name: "no check has passed in a run with no install window",
		tl:   timeline(10*sec, steadyOnly(), failures(0, 10*sec)),
		want: summary{Verdict: NotVerified, Results: []Result{ResultNotVerified, NotRun, NotRun}, Reasons: []string{"steady:steady-not-clean"}, Decided: SteadyNotClean},
		texts: map[string]string{"steady:steady-not-clean": "10 checks failed (503 10), first at 10:00:00.00\n" +
			"no check has passed since the probe started: every check failed for 10.0 s"},
```

And this test:

```go
// LastFailure goes by when a failed check began, not by when its line was written or read.
func TestLastFailure(t *testing.T) {
	tl := timeline(10*sec, withInstall(steadyOnly()), passes(-15*sec, 10*sec), []Observation{
		failure(-14900*msec, -14900*msec, "refused"), failure(-100*msec, 300*msec, "timeout"), failure(-7*sec, -7*sec, "refused")})
	d := Decide(tl)
	if got := d.Windows[0].LastFailure; !got.Equal(at(-100 * msec)) {
		t.Errorf("the install window's LastFailure = %v, want %v", got, at(-100*msec))
	}
	if got := d.Windows[1].LastFailure; !got.IsZero() {
		t.Errorf("the steady window's LastFailure = %v, want zero", got)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/verdict/ -count=1`
Expected: FAIL at the build, with "undefined: Install" and "LastFailure undefined".

- [ ] **Step 4: Implement, with the interface comments from the Interfaces block**

- `vocab.go`: the constant `Install`, first in the block of window names, and its sentence in the comment of `WindowName`.
- `decide.go`: `WindowResult.LastFailure`. `resultOf` judges the install window as it judges the steady window: not verified by any condition. The comments of `Timeline`, `Decide` and `judge` say "the first window" and "the probe's first observation" where they said "the steady window".
- `observe.go`:
  - `observe` does nothing when the first window has not begun; that is the test it has, with a new comment.
  - `countObservations` sets `LastFailure` to the latest `T` of a failure, and adds to `Crossed` only for a window that is neither the install window nor the steady window.
  - `addFailures` adds no condition for the install window.
  - For the steady window it adds a second line to the text when no observation of the timeline has `OK` and a `T` before the end of the steady window: a line end, then `no check has passed since the probe started: every check failed for %.1f s`, with the seconds from the first window's `Start` to the steady window's end as the placement holds it.

Nothing else changes: placing by time, silence from the first window's start, a late condition and "not judged" already walk the list of windows.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/verdict/ -count=1`
Expected: PASS, the rows that existing tests had included.

- [ ] **Step 6: Commit**

```bash
git add internal/verdict/vocab.go internal/verdict/decide.go internal/verdict/observe.go internal/verdict/decide_test.go internal/verdict/observe_test.go
git commit -m "Judge the install window in verdict: failed checks counted and not judged"
```

### Task 3: The probe's events from its start, and a failed start that stops the probe

Read section 6, "Lifecycle", and in section 5 the row for `run`.

`internal/run/run.go` starts the goroutine that reads the probe's events only once the windows are fixed, and the probe's channel holds 1,024 events with a send that waits while it is full. With an install window, thousands of events come before the steady window. From this task on the reader exists before the probe does.

**Files:**
- Modify: `internal/probe/pod.go`, `internal/run/run.go` (`startProbe`, `windows`, `stopProbe`)
- Test: `internal/probe/pod_test.go`, `internal/probe/pod_cluster_test.go` (two lines, so that it builds), `internal/run/fakes_test.go`, `internal/run/run_test.go`

**Interfaces:**
- Consumes: nothing from another task.
- Produces: `type Started struct { First time.Time }`
  A probe that has started. First is when its first observation was
  written, by the runtime's stamp. The install window opens there.
- Produces: `func (p *Pod) Events() <-chan Event`
  Every observation and every fault of the probe, in the log's order,
  from its first line on. Read it from before Start is called until it is
  closed: a send waits for its reader. It is closed when a Start that
  failed, or Stop, has returned.
- Produces: `func (p *Pod) Start(ctx context.Context) (Started, error)`
  Creates the probe's pod and returns at its first observation. The error
  is a Condition when the probe did not start in time, exited first, or
  the nodes' clock differs from the runner's; the probe has then been
  stopped as Stop stops it. When ctx ends, the error is ctx's and the pod
  is left as it is.
- Produces: `func (p *Pod) Stop(ctx context.Context) (Facts, error)`
  Deletes the probe's pod with the drain as its grace period, reads its
  lines until the container has exited, and closes Events. After a Start
  that failed, and at a second call, it gives the Facts and the error
  that the stop already made left.
- Produces: `type Probe interface { Events() <-chan Event; Start(ctx context.Context) (Started, error); Stop(ctx context.Context) (Facts, error) }`
  One probe of a run, as run holds it: the three operations of Pod, with
  the same contracts.

**Error surface:**

| Item | Disposition | Reason | Test |
| --- | --- | --- | --- |
| The pod cannot be deleted when a failed `Start` stops the probe | expose | `Stop` gives that error afterwards, and `run` prints the warning it has for a probe that could not be stopped | `TestAFailedStartStopsTheProbe`, "the delete fails" |
| `Start` after its context ended does not stop the probe | define-out | A signal leaves the cluster and the probe's pod for the next run (section 10, "On a signal") | `TestAFailedStartStopsTheProbe`, "the context ends" |

Handlers callers must write: 0 new; `run` has the warning.

- [ ] **Step 1: Write the failing tests for the pod**

In `internal/probe/pod_test.go`, every `st.Events` becomes `s.pod.Events()`, and the two uses in `internal/probe/pod_cluster_test.go` become `p.Events()`. Add:

```go
// The follower's sends have a reader from the probe's first line: 3,000 lines that break the
// contract come before the first observation, more than the channel holds, and Start returns.
func TestEventsAreReadFromTheStart(t *testing.T) {
	var log strings.Builder
	for i := range 3000 {
		log.WriteString(logLine(logAt(100+i), `{"ok":"yes"}`))
	}
	log.WriteString(logLine(logAt(4000), `{"ok":true}`))
	l := &fakeLog{streams: []io.Reader{openStream(t, log.String())}}
	s := newProbe(t, l, runningStatus(), 100*time.Millisecond, 5*time.Second)
	s.clock.Set(logAt(4000))
	read := make(chan []Event, 1)
	go func() { read <- allEvents(s.pod.Events()) }()
	st, err := s.pod.Start(context.Background())
	if err != nil {
		t.Fatalf("Start = %v; with a reader of Events from before Start, the follower never waits", err)
	}
	if !st.First.Equal(logAt(4000)) {
		t.Errorf("First = %v, want %v", st.First, logAt(4000))
	}
	if _, err := s.pod.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	faults, observations := 0, 0
	for _, e := range <-read {
		if e.Fault != nil {
			faults++
		} else {
			observations++
		}
	}
	if faults != 3000 || observations != 1 {
		t.Errorf("%d faults and %d observations were read, want 3000 and 1", faults, observations)
	}
}

// A Start that failed leaves no probe running, unless its context ended: the pod is deleted with
// the drain as its grace period, Events is closed, and Stop gives what that stop left.
func TestAFailedStartStopsTheProbe(t *testing.T) {
	background := func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }
	soon := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), 100*time.Millisecond)
	}
	refuse := errors.New("the API server refused the delete")
	for _, c := range []struct {
		name       string
		log        *fakeLog
		status     corev1.PodStatus
		clock      time.Time
		ctx        func() (context.Context, context.CancelFunc)
		failDelete bool
		code       verdict.Code // empty: the context's error
		grace      int64        // of the last delete: 1 for the drain of 100 ms, 0 for the delete Start begins with
	}{
		{"no observation in time", newFakeLog(logLine(logAt(100), "starting")), runningStatus(), logAt(700), background, false, verdict.ProbeNotStarted, 1},
		{"the clock check fails", newFakeLog(logLine(logAt(400), `{"ok":true}`)), runningStatus(), logAt(2900), background, false, verdict.RunnerUnsupported, 1},
		{"the probe exits first", &fakeLog{}, terminatedStatus(2, "Error", logAt(300)), logAt(700), background, false, verdict.ProbeExited, 1},
		{"the delete fails", newFakeLog(logLine(logAt(100), "starting")), runningStatus(), logAt(700), background, true, verdict.ProbeNotStarted, 1},
		{"the context ends", &fakeLog{}, runningStatus(), logAt(700), soon, false, "", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newProbe(t, c.log, c.status, 100*time.Millisecond, 300*time.Millisecond)
			s.clock.Set(c.clock)
			if c.failDelete {
				deletes := 0
				s.client.PrependReactor("delete", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
					if deletes++; deletes == 1 {
						return false, nil, nil // the delete of a leftover pod, which Start begins with
					}
					return true, nil, refuse
				})
			}
			read := make(chan []Event, 1)
			go func() { read <- allEvents(s.pod.Events()) }()
			ctx, cancel := c.ctx()
			defer cancel()
			_, err := s.pod.Start(ctx)
			switch {
			case c.code == "" && !errors.Is(err, context.DeadlineExceeded):
				t.Errorf("Start = %v, want the context's error", err)
			case c.code != "" && condition(err).Code != c.code:
				t.Errorf("Start = %v, want %s", err, c.code)
			}
			select {
			case <-read:
			case <-time.After(2 * time.Second):
				t.Fatal("Events was not closed when Start had failed")
			}
			if g := s.lastDeleteGrace(); g == nil || *g != c.grace {
				t.Errorf("the last delete of the pod had the grace period %v, want %d", g, c.grace)
			}
			before := len(s.client.Actions())
			_, stopErr := s.pod.Stop(context.Background())
			if n := len(s.client.Actions()); n != before {
				t.Errorf("Stop after a failed Start made %d more calls of the API, want none", n-before)
			}
			if c.failDelete != errors.Is(stopErr, refuse) {
				t.Errorf("Stop = %v; it gives the error of the delete that the failed Start made, and no other", stopErr)
			}
		})
	}
}

// A Start that failed before its pod was created ran nothing, so the probe did not fail to stop.
func TestAStartThatCreatedNoPodStoppedWhenAsked(t *testing.T) {
	s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 300*time.Millisecond)
	s.client.PrependReactor("create", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("the API server refused the pod")
	})
	go allEvents(s.pod.Events())
	if _, err := s.pod.Start(context.Background()); err == nil {
		t.Fatal("Start succeeded although the API server refused the pod")
	}
	if f, _ := s.pod.Stop(context.Background()); !f.StoppedWhenAsked {
		t.Errorf("facts %+v; a probe whose pod was never created stopped when asked", f)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/probe/ -run 'TestEventsAreReadFromTheStart|TestAFailedStartStopsTheProbe|TestAStartThatCreatedNoPodStoppedWhenAsked' -count=1`
Expected: FAIL at the build, with "s.pod.Events undefined".

- [ ] **Step 3: Implement in `internal/probe/pod.go`, with the interface comments from the Interfaces block**

- `NewPod` makes the channel, with the 1,024 places it has, and `Events` returns it. `Started` loses its `Events` field. The `Probe` interface gains `Events`.
- Every path on which the `Pod` comes to its ended stage closes the channel, once: a `Start` that failed, whether before or after the pod was created, and `Stop`.
- `Start`, when it has failed for a reason other than its context, does what `Stop` does before it returns: it ends the watcher, deletes the pod with the drain as its grace period, reads the log until it ends or the drain and `stopMargin` have passed, and keeps the `Facts` and the delete's error for `Stop`. One unexported function holds that sequence for both.
- The `Facts` that a failed `Start` keeps say that the probe stopped when asked when its pod was never created, because nothing ran to stop. Once the pod was created, that fact is measured as `Stop` measures it, from the delete.
- `Start`, when its context ended, ends its goroutines and closes the channel as it does today, and deletes nothing.

- [ ] **Step 4: Run the probe's tests**

Run: `go test ./internal/probe/ -count=1`
Expected: PASS.

Run: `go vet -tags cluster ./internal/probe/`
Expected: no output.

- [ ] **Step 5: Write the failing tests for the run**

In `internal/run/fakes_test.go` the fake probe gets its channel before `Start`:

```go
// in fakeProbe, in place of ch and once:
	unbuffered bool // Events gives a channel with no buffer: a send returns only when a reader takes it
	ch         chan probe.Event
	made, once sync.Once

// Events gives the probe's channel, made at the first call: with room for the scripted events and
// 64 more, or with no buffer when unbuffered is set.
func (p *fakeProbe) Events() <-chan probe.Event {
	p.made.Do(func() {
		if p.unbuffered {
			p.ch = make(chan probe.Event)
		} else {
			p.ch = make(chan probe.Event, len(p.events)+64)
		}
	})
	return p.ch
}
```

Its `Start` records "start", runs the hook, sends every scripted event into `p.Events()`'s channel, and then returns `probe.Started{First: p.w.clock.Now()}`; with `startErr` it sends the events, closes the channel through `once`, and returns the error.

In `internal/run/run_test.go`, `resultFile.Windows` gains `Checks int` with the tag `json:"checks"` and `Start *time.Time` with the tag `json:"start"`. In `TestEachBeforeFailure` the row "probe not started" expects "probe-stop" after "start" in its calls. Add:

```go
// The run reads the probe's events from before the probe starts (section 5). Start sends 5,000
// observations and the scripted ones on a channel with no buffer, so it returns only while a
// reader takes them.
func TestTheReaderStartsWithTheProbe(t *testing.T) {
	w := newWorld(t)
	w.probe.unbuffered = true
	first := w.clock.Now()
	var early []probe.Event
	for i := range 5000 { // one check every 2 ms for the probe's first 10 s
		at := first.Add(time.Duration(i) * 2 * time.Millisecond)
		early = append(early, probe.Event{Obs: &verdict.Observation{Written: at, T: at, OK: true, N: 1, Ms: 30, Lag: 2}})
	}
	w.probe.events = append(early, w.probe.events...)
	done := make(chan int, 1)
	go func() { done <- w.run(context.Background()) }()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d, want 0\n%s", code, w.stdout.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Start is waiting for a reader of the probe's events: the run must read them from the moment the probe starts")
	}
	checks := 0
	for _, x := range w.result().Windows {
		checks += x.Checks
	}
	if checks != 19000 { // 5,000 early, and 200 a second for the 70 s of the three windows
		t.Errorf("the windows hold %d checks, want 19000: none may be lost", checks)
	}
}

// Rule 9: a fault of the probe before its first observation makes the run not verified, also when
// the probe then never starts. Both reasons are listed, and neither has a window.
func TestFaultsBeforeAFailedStartAreReasons(t *testing.T) {
	w := newWorld(t)
	w.probe.events = []probe.Event{{Fault: &verdict.Condition{Code: verdict.ProbeLineInvalid,
		Text: "the line is not one JSON object", At: w.clock.Now()}}}
	w.probe.startErr = verdict.Condition{Code: verdict.ProbeNotStarted, Text: "no observation within limits.timeout"}
	if code := w.run(context.Background()); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	r := w.result()
	if diff := cmp.Diff([]string{"probe-not-started", "probe-line-invalid"}, r.codes()); diff != "" {
		t.Errorf("reasons (-want +got):\n%s", diff)
	}
	for _, x := range r.Reasons {
		if x.Window != "" {
			t.Errorf("%s has the window %q, want none", x.Code, x.Window)
		}
	}
	if r.decided() != "probe-not-started" {
		t.Errorf("decided by %q, want probe-not-started", r.decided())
	}
	if n := w.calls.count("probe-stop"); n != 1 {
		t.Errorf("the probe's Stop was called %d times after a failed start, want 1", n)
	}
}
```

- [ ] **Step 6: Run the tests to verify they fail**

Run: `go test ./internal/run/ -count=1`
Expected: FAIL at the build, with "s.probeStart.Events undefined" in `run.go`. With only that line changed so that it compiles, and the reader still started in `windows`, `TestTheReaderStartsWithTheProbe` fails with its own message after 10 s.

- [ ] **Step 7: Implement in `internal/run/run.go`**

- `startProbe` makes the probe, starts the reader on `p.Events()`, and only then calls `p.Start`. `windows` no longer starts the reader.
- When `Start` has failed and no signal ended the run, `startProbe` asks the probe to stop before it places the failure: `Stop` then gives what the failed start left. One unexported function holds what `stopProbe` already does with `Stop` (keep the `Facts`, mark the probe as stopped, warn when `Stop` gave an error), and both call it.
- The comments of `readEvents`, `windows` and `runState` say where the reader starts now.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/run/ ./internal/probe/ ./cmd/rollproof/ -count=1`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/probe/pod.go internal/probe/pod_test.go internal/probe/pod_cluster_test.go internal/run/run.go internal/run/fakes_test.go internal/run/run_test.go
git commit -m "Give the probe's events a reader from its start, and stop a probe whose start failed"
```

### Task 4: `run`: the install window

Read section 4, the flow of one run in section 5, and section 7 from "When the install has settled" to "Pods replaced".

**Files:**
- Modify: `internal/run/run.go` (`Watcher`, `runState`, `windows`, `transitionsStep`), `internal/run/record.go` (the install window stays out of `windows`)
- Test: `internal/run/fakes_test.go`, `internal/run/run_test.go`

**Interfaces:**
- Consumes: `func (w *Watcher) InstallSettled(ctx context.Context, first time.Time) Settling` and `type Settling` of `internal/watch` (Task 1); `const Install WindowName` (Task 2); `Started.First` (Task 3).
- Produces: `Watcher.InstallSettled(ctx context.Context, first time.Time) watch.Settling`
  One more operation of the interface that run holds the watch unit by.
  Run calls it once, when the probe's first observation has arrived, and
  it returns when the install window has ended.
- Produces: `func Run(ctx context.Context, o Options, u Units) int`
  As built. The windows of a run are now the install window, the steady
  window and one for each transition. The run begins a window only when
  verdict says that it goes on, and it stops the probe whenever a window
  began.

**Error surface:**

| Item | Disposition | Reason | Test |
| --- | --- | --- | --- |
| `InstallSettled` returns with no settle moment and no condition, and no signal came | crash | It cannot happen while `watch` keeps its contract; the run panics, and the handler it has for a unit's panic makes it `internal-error` in the install window | `TestASettlingWithNoResultIsTheToolsFailure` |

Handlers callers must write: 0.

- [ ] **Step 1: Change the fakes and write the failing tests**

In `internal/run/fakes_test.go`:

- `newWorld` starts its clock at `t0.Add(-15 * time.Second)`, scripts `passes(-15, 70)` and `Lines: 86`. The probe's first observation is then 15 s before `t0`, the install window is `[t0-15 s, t0)`, and the steady window still begins at `t0`, so no existing test that places an event by its time moves.
- The fake watcher gets the new operation:

```go
// in fakeWatcher:
	// settle, when set, gives what InstallSettled returns for the first observation it is handed.
	settle func(first time.Time) watch.Settling
	// late is how far the clock runs past the settling's end before InstallSettled returns, as
	// when the tool looks at the cluster a moment after the install settled.
	late time.Duration

// InstallSettled records "install-settled" and runs its hook. When ctx has ended it gives a
// settling cut at the clock's time. Otherwise it gives the scripted settling, or one that settles
// limits.settle after first, and moves the clock to the settling's end and late past it.
func (f *fakeWatcher) InstallSettled(ctx context.Context, first time.Time) watch.Settling {
	f.w.calls.add("install-settled")
	f.w.hook("install-settled")
	if ctx.Err() != nil {
		return watch.Settling{First: first, Ended: f.w.clock.Now()}
	}
	end := first.Add(f.w.cfg.Limits.Settle)
	s := watch.Settling{First: first, Settled: end, Ended: end}
	if f.settle != nil {
		s = f.settle(first)
	}
	f.w.clock.Advance(s.Ended.Sub(f.w.clock.Now()) + f.late)
	return s
}
```

In `internal/run/run_test.go`, five existing tests change with the sequence, and nothing else in them does: `TestPass` expects "install-settled" after "start" in its calls; `TestWindowMarks` and `TestNoGapBetweenWindows` expect four marks, `install` first, and read the other three one place later; `TestStall`, which scripts its own lines, scripts `passes(-15, 320)`, so that the install window has observations; `TestTheReaderStartsWithTheProbe` expects 14,000 checks in the windows, because the 5,000 early ones now fall in the install window. Any other test that lists the calls of a run that reached its first observation gains "install-settled" after "start" in the same way.

Add:

```go
// lateClock is the fake clock except that it runs before once, ahead of the run's first wait,
// which is the steady window's.
type lateClock struct {
	*fakeClock
	before func()
	once   sync.Once
}

func (c *lateClock) After(d time.Duration) <-chan time.Time {
	c.once.Do(c.before)
	return c.fakeClock.After(d)
}

// when reports whether a mark's time is there and is want.
func when(got *time.Time, want time.Time) bool { return got != nil && got.Equal(want) }

// The install window runs from the first observation until the install has settled, the steady
// window begins there, and result.json's windows do not list it (sections 4 and 10).
func TestTheSteadyWindowBeginsWhenTheInstallHasSettled(t *testing.T) {
	w := newWorld(t)
	w.watcher.late = 3 * time.Second // the tool looks 3 s after the install settled
	if code := w.run(context.Background()); code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, w.stdout.String())
	}
	marks := w.windowMarks()
	if len(marks) != 4 || marks[0].Name != "install" || marks[1].Name != "steady" {
		t.Fatalf("marks %+v, want install, steady, deploy, rollback", marks)
	}
	if !when(marks[0].Start, t0.Add(-15*time.Second)) || !when(marks[0].End, t0) {
		t.Errorf("the install window is [%v, %v), want [%v, %v)", marks[0].Start, marks[0].End, t0.Add(-15*time.Second), t0)
	}
	if !when(marks[1].Start, t0) || !when(marks[1].End, t0.Add(10*time.Second)) {
		t.Errorf("the steady window is [%v, %v), want [%v, %v)", marks[1].Start, marks[1].End, t0, t0.Add(10*time.Second))
	}
	if !when(marks[2].Start, t0.Add(10*time.Second)) {
		t.Errorf("the deploy's window starts at %v, want the steady window's end, %v", marks[2].Start, t0.Add(10*time.Second))
	}
	r := w.result()
	if diff := cmp.Diff([]string{"pass", "pass", "pass"}, r.results()); diff != "" {
		t.Errorf("window results (-want +got):\n%s", diff)
	}
	if r.Windows[0].Name != "steady" || r.Windows[0].Start == nil || !r.Windows[0].Start.Equal(t0) {
		t.Errorf("result.json's first window is %+v, want steady from %v", r.Windows[0], t0)
	}
}

func TestAFailedCheckInTheInstallWindowDecidesNothing(t *testing.T) {
	w := newWorld(t)
	w.probe.events = append(w.probe.events, failureAt(-10*time.Second, -10*time.Second))
	if code := w.run(context.Background()); code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, w.stdout.String())
	}
	if r := w.result(); len(r.Reasons) != 0 {
		t.Errorf("reasons %v, want none", r.codes())
	}
}

// Rule 4, and Review Focus 4: a run that ends in the install window stops the probe, deletes the
// cluster and writes a final result.
func TestTheInstallWindowNotVerified(t *testing.T) {
	first := t0.Add(-15 * time.Second)
	failed := verdict.Condition{Code: verdict.InstallFailed, Text: "the install was not ready when limits.timeout passed"}
	agent := verdict.Condition{Code: verdict.WorkloadUnsupported, At: t0.Add(-6 * time.Second),
		Text: "daemonset/agent updates with OnDelete; the tool judges only a workload that updates by rolling"}
	settling := func(s watch.Settling) func(*world) {
		return func(w *world) { w.watcher.settle = func(time.Time) watch.Settling { return s } }
	}
	for _, row := range []struct {
		name         string
		script       func(w *world)
		code, window string
		end          time.Time // the install window's end in windows.json
	}{
		{"the probe exits", func(w *world) {
			w.probe.events = append(w.probe.events, probe.Event{Fault: &verdict.Condition{Code: verdict.ProbeExited,
				Text: "the probe exited with exit code 1 (Error)", At: t0.Add(-5 * time.Second)}})
		}, "probe-exited", "install", t0},
		{"the probe is silent and then writes again", func(w *world) {
			w.probe.events = append(passes(-15, -12), passes(-5, 70)...)
		}, "probe-silent", "install", t0},
		{"the install fails while it settles",
			settling(watch.Settling{First: first, Ended: t0.Add(25 * time.Second), InWindow: []verdict.Condition{failed}}),
			"install-failed", "install", t0.Add(25 * time.Second)},
		{"a workload the tool cannot judge appears",
			settling(watch.Settling{First: first, Settled: t0, Ended: t0, InWindow: []verdict.Condition{agent}}),
			"workload-unsupported", "install", t0},
		{"the install had failed before the first observation",
			settling(watch.Settling{First: first, Ended: first, Before: []verdict.Condition{failed}}),
			"install-failed", "", first},
	} {
		t.Run(row.name, func(t *testing.T) {
			w := newWorld(t)
			row.script(w)
			if code := w.run(context.Background()); code != 2 {
				t.Fatalf("exit %d, want 2\n%s", code, w.stdout.String())
			}
			r := w.result()
			if !r.Final || r.Verdict != "not-verified" {
				t.Errorf("final %v, verdict %q; want a final not-verified", r.Final, r.Verdict)
			}
			if diff := cmp.Diff([]string{row.code}, r.codes()); diff != "" {
				t.Errorf("reasons (-want +got):\n%s", diff)
			}
			if window, _ := r.windowOf(row.code); window != row.window {
				t.Errorf("%s in window %q, want %q", row.code, window, row.window)
			}
			if marks := w.windowMarks(); len(marks) < 2 || !when(marks[0].End, row.end) || marks[1].Start != nil {
				t.Errorf("marks %+v; want the install window to end at %v, and no steady window", marks, row.end)
			}
			if diff := cmp.Diff([]string{"not-run", "not-run", "not-run"}, r.results()); diff != "" {
				t.Errorf("window results (-want +got):\n%s", diff)
			}
			for name, want := range map[string]int{"deploy": 0, "probe-stop": 1, "delete": 1} {
				if n := w.calls.count(name); n != want {
					t.Errorf("%s was called %d times, want %d", name, n, want)
				}
			}
		})
	}
}

func TestSignalInTheInstallWindow(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.hooks["install-settled"] = func() {
		w.clock.Advance(4 * time.Second)
		cancel()
	}
	if code := w.run(ctx); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	r := w.result()
	if window, _ := r.windowOf("interrupted"); window != "install" || !r.Final {
		t.Errorf("interrupted in window %q, final %v; want install and final", window, r.Final)
	}
	if marks := w.windowMarks(); marks[0].End == nil || !marks[0].End.Equal(t0.Add(-11*time.Second)) || marks[1].Start != nil {
		t.Errorf("marks %+v; want the install window cut at the signal and no steady window", marks)
	}
}

func TestAToolFailureInTheInstallWindow(t *testing.T) {
	w := newWorld(t)
	w.hooks["install-settled"] = func() { panic("the watch unit broke") }
	if code := w.run(context.Background()); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if window, text := w.result().windowOf("internal-error"); window != "install" || text != "the watch unit broke" {
		t.Errorf("internal-error in window %q with text %q", window, text)
	}
}

func TestASettlingWithNoResultIsTheToolsFailure(t *testing.T) {
	w := newWorld(t)
	w.watcher.settle = func(first time.Time) watch.Settling {
		return watch.Settling{First: first, Ended: first.Add(time.Second)}
	}
	if code := w.run(context.Background()); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if window, _ := w.result().windowOf("internal-error"); window != "install" {
		t.Errorf("internal-error in window %q, want install", window)
	}
}

// Rule 8: a condition of the install window that the tool learns of only after the steady window
// began is placed late. The steady window runs to its end, and the run stops there.
func TestAnInstallWindowJudgedLate(t *testing.T) {
	w := newWorld(t)
	u := w.units()
	u.Clock = &lateClock{fakeClock: w.clock, before: func() {
		w.probe.send(probe.Event{Fault: &verdict.Condition{Code: verdict.ProbeLineInvalid,
			Text: "the line is not one JSON object", At: t0.Add(-5 * time.Second)}})
	}}
	code := Run(context.Background(), Options{ConfigPath: filepath.Join(w.cfg.Dir, "rollproof.yaml"), Out: w.out,
		Stdout: &w.stdout, Stderr: &w.stderr, Version: "test", Getenv: func(string) string { return "" }}, u)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	r := w.result()
	if window, _ := r.windowOf("probe-line-invalid"); window != "install" {
		t.Errorf("probe-line-invalid in window %q, want install", window)
	}
	if diff := cmp.Diff([]string{"not-judged", "not-run", "not-run"}, r.results()); diff != "" {
		t.Errorf("window results (-want +got):\n%s", diff)
	}
	if end := r.Windows[0].End; end == nil || !end.Equal(t0.Add(10*time.Second)) {
		t.Errorf("the steady window ended at %v, want its whole length, %v", end, t0.Add(10*time.Second))
	}
	if n := w.calls.count("deploy"); n != 0 {
		t.Errorf("deploy was called %d times, want 0", n)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/run/ -count=1`
Expected: FAIL. The package builds, because the fake's new method is not asked for yet. `TestPass` fails on its calls, `TestWindowMarks` and `TestNoGapBetweenWindows` on their marks, and `TestTheSteadyWindowBeginsWhenTheInstallHasSettled` with three marks, because the steady window still begins at the first observation, 15 s before `t0`. A test that places an event by its time fails only where that event now falls in another window.

- [ ] **Step 3: Implement in `internal/run/run.go` and `internal/run/record.go`**

- `Watcher` gains `InstallSettled`, with its interface comment from the Interfaces block.
- `windows` builds the timeline as the install window (its `Start` is the probe's `First`), the steady window, and one window for each transition. Then, in this order:
  1. It waits for `s.watcher.InstallSettled(ctx, first)` and keeps the settling in a new field of `runState`, for the record of Task 6.
  2. A signal ends the run there, as it does in a transition: the settling's own conditions are not placed.
  3. It appends the settling's `Before` to the timeline's `Before` and its `InWindow` to the install window's conditions, and ends the install window at the settling's `Ended`.
  4. A settling with a zero `Settled` and no condition in either list is a panic, with the text "the install neither settled nor failed".
  5. It asks `decide(false)`. When the run does not go on, `windows` returns true, so that the probe is stopped.
  6. The steady window begins at the settling's `Settled` and lasts `limits.steady` from there; the wait, the signal and the decision after it stay as built.
- `transitionsStep` finds the window of transition `i` one place later than before.
- `record.go`: the install window is not copied into the record's `Windows`, and `started` reports whether any window began.
- The step comments of `setUp`, `windows` and `stopProbe` name the install window where they named the steady window as the first.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/run/ ./cmd/rollproof/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/run/run.go internal/run/record.go internal/run/fakes_test.go internal/run/run_test.go
git commit -m "Run the install window before the steady window"
```

### Task 5: The built-in probe: no warm-up, and a failed lookup as a failed check

Read section 6, "The built-in HTTP probe".

**Files:**
- Modify: `internal/probe/http.go`, `internal/probe/httpargs.go`
- Test: `internal/probe/http_test.go`, `internal/probe/httpargs_test.go`

**Interfaces:**
- Consumes: nothing from another task.
- Produces: `type HTTPOptions struct { URL string; Headers map[string]string; Rate int; Timeout time.Duration; ExpectMin, ExpectMax int }`
  The settings of the built-in probe, as built, without Warmup: the probe
  has none.
- Produces: `func ParseHTTPArgs(args []string) (HTTPOptions, error)`
  The reverse of HTTPArgs: for every o, ParseHTTPArgs(HTTPArgs(o)) gives
  o back.
- Produces: `func RunHTTP(ctx context.Context, o HTTPOptions, out io.Writer) error`
  Sends GET requests to o.URL on an open schedule that starts when it is
  called, each on a new connection, and writes a line for every request,
  the first included, until ctx ends. Each request looks the host up
  until one lookup has succeeded; a failed lookup is a failed check of
  class other. The error is only for a URL that does not parse or names
  no host.

**Error surface:**

| Item | Disposition | Reason | Test |
| --- | --- | --- | --- |
| The host cannot be resolved | define-out | Section 6 makes it a failed check; `RunHTTP` no longer has that error, and the probe's pod no longer exits on it | `TestRunHTTPHostResolvesLater`, `TestRunHTTPHostNeverResolves` |
| A lookup gives no address | aggregate | It takes the path of a lookup that failed | `TestRunHTTPHostResolvesLater` |

Handlers callers must write: 0.

- [ ] **Step 1: Write the failing tests**

In `internal/probe/http_test.go`, delete the five tests whose names begin `TestRunHTTPWarmup`, and `TestRunHTTPUnresolvableHost`: six tests in all. In `TestRunHTTPResolvesOnce`, the count of lookups may be 1 or 2: `if n := calls.Load(); n < 1 || n > 2`, with the message "the host was looked up %d times, want 1 or 2" and the comment `// Each request looks the host up until one lookup has succeeded. The stub answers at once and requests are 20 ms apart, so a second lookup comes only from a request that began before the first had its answer.` Its old `1` came from the lookup before the schedule, which this task removes. In `internal/probe/httpargs_test.go`, delete `TestParseHTTPArgsGivesTheWarmup` and the two lines of `TestHTTPArgsRoundTrip` that set `Warmup`. Add to `http_test.go`:

```go
// replaceLookup puts f in place of lookupHost until the test ends.
func replaceLookup(t *testing.T, f func(ctx context.Context, host string) ([]string, error)) {
	t.Helper()
	saved := lookupHost
	lookupHost = f
	t.Cleanup(func() { lookupHost = saved })
}

// The probe reports from its first request on, and its schedule starts when it starts
// (section 6): the server's first five answers are failures, and every one of them is a line.
func TestRunHTTPReportsFromItsFirstRequest(t *testing.T) {
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) <= 5 {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()
	began := time.Now()
	obs := runFor(t, testOptions(srv.URL), time.Second).observations(t)
	if n := checks(obs); int64(n) != requests.Load() {
		t.Errorf("the lines stand for %d checks, the server saw %d: every request is reported", n, requests.Load())
	}
	if len(obs) < 5 {
		t.Fatalf("%d lines, want the five failures and more", len(obs))
	}
	for i, o := range obs[:5] {
		if o.OK || o.Class != "503" {
			t.Errorf("line %d is %+v, want a failure of class 503", i, o)
		}
	}
	if d := obs[0].T.Sub(began); d > 100*time.Millisecond {
		t.Errorf("the first check began %v after the probe started, want at once", d)
	}
}

// A host that does not resolve at first and then does (section 12): a request whose lookup fails
// is a failed check of class other, and no request looks the name up once one lookup succeeded.
func TestRunHTTPHostResolvesLater(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	var calls atomic.Int64
	replaceLookup(t, func(context.Context, string) ([]string, error) {
		switch n := calls.Add(1); {
		case n <= 2:
			return nil, &net.DNSError{Err: "no such host", Name: "probe-target.test", IsNotFound: true}
		case n == 3:
			return nil, nil // no address, and no error
		}
		return []string{"127.0.0.1"}, nil
	})
	obs := runFor(t, testOptions("http://probe-target.test:"+port+"/"), time.Second).observations(t)
	failed := 0
	for _, o := range obs {
		if !o.OK {
			failed++
			if o.Class != "other" {
				t.Errorf("a failed lookup has the class %q, want other", o.Class)
			}
		}
	}
	if failed != 3 {
		t.Errorf("%d checks failed, want the 3 whose lookup failed", failed)
	}
	if n := calls.Load(); n < 4 || n > 8 {
		t.Errorf("the host was looked up %d times, want 4, or a few more for requests that began together; about 50 means every request looked it up", n)
	}
	if n := checks(obs); n < 40 {
		t.Errorf("%d checks, want about 50", n)
	}
}

// A name that never resolves does not end the probe: it writes a failed check for each request on
// its schedule, and returns nil when it is stopped.
func TestRunHTTPHostNeverResolves(t *testing.T) {
	replaceLookup(t, func(context.Context, string) ([]string, error) {
		return nil, &net.DNSError{Err: "no such host", Name: "nowhere.test", IsNotFound: true}
	})
	obs := runFor(t, testOptions("http://nowhere.test/"), time.Second).observations(t)
	if len(obs) < 40 {
		t.Errorf("%d lines in 1 s at rate 50, want 45 to 55", len(obs))
	}
	for _, o := range obs {
		if o.OK || o.Class != "other" {
			t.Fatalf("got %+v, want a failure of class other", o)
		}
	}
}

// Review Focus 3, and ruling 9: a lookup that hangs does not hold the schedule, and a request
// whose timeout passes during its lookup is a timeout, written within that timeout.
func TestRunHTTPLookupsThatHang(t *testing.T) {
	var started atomic.Int64
	replaceLookup(t, func(ctx context.Context, _ string) ([]string, error) {
		started.Add(1)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	o := testOptions("http://slow-dns.test/")
	o.Timeout = 300 * time.Millisecond
	began := time.Now()
	out := runFor(t, o, time.Second)
	if n := started.Load(); n < 40 {
		t.Errorf("%d lookups started in 1 s at rate 50, want 45 to 55: a lookup must not hold the schedule", n)
	}
	obs := out.observations(t)
	if len(obs) == 0 {
		t.Fatal("no line")
	}
	for _, ob := range obs {
		if ob.OK || ob.Class != "timeout" {
			t.Fatalf("got %+v, want a failure of class timeout", ob)
		}
	}
	if d := obs[0].Written.Sub(began); d > o.Timeout+200*time.Millisecond {
		t.Errorf("the first line came %v after the probe started, want within the request timeout", d)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/probe/ -run 'TestRunHTTP|TestHTTPArgs|TestParseHTTPArgs' -count=1`
Expected: FAIL. `TestRunHTTPHostResolvesLater`, `TestRunHTTPHostNeverResolves` and `TestRunHTTPLookupsThatHang` fail with "RunHTTP: the probe's host cannot be resolved", and `TestHTTPArgsRoundTrip` fails on the `Warmup` of one second that parsing still adds. `TestRunHTTPReportsFromItsFirstRequest` passes already: `RunHTTP` warmed up only when its options asked, and it was `ParseHTTPArgs` that asked. It stays, as the test that pins what section 6 now says.

- [ ] **Step 3: Implement, with the interface comments from the Interfaces block**

- `httpargs.go`: `HTTPOptions` loses `Warmup`, and `ParseHTTPArgs` no longer sets it.
- `http.go`:
  - `warmUp` and its call go. The schedule's start is the moment `RunHTTP` has read the URL; nothing is sent or awaited before it.
  - `RunHTTP` resolves nothing before the schedule. The transport's dial does it: while no address is kept, it looks the URL's host up with the request's own context, so the lookup is inside the request's timeout; the first address that any request gets is kept, and every later dial uses it. An address, once kept, is never looked up again.
  - A lookup that returns an error while the request's context has not ended, or that returns no address, fails the dial with an unexported error type, and `classify` gives `other` for that type before it tries any other class. A lookup that returns because the request's context ended fails the dial with the context's error, which `classify` already reads as `timeout` (ruling 9).
  - The comment of `lookupHost` says that it resolves the host for a request until one lookup has succeeded.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/probe/ ./cmd/rollproof/ -count=1`
Expected: PASS. `TestRunHTTPResolvesOnce` passes with its new count.

Run: `go build ./... && go vet ./...`
Expected: no output. No file names `Warmup` any more: `grep -rn Warmup cmd internal` prints nothing.

- [ ] **Step 5: Commit**

```bash
git add internal/probe/http.go internal/probe/httpargs.go internal/probe/http_test.go internal/probe/httpargs_test.go
git commit -m "Take the warm-up out of the built-in probe and make a failed lookup a failed check"
```
