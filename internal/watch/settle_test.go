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
	two := func(a, b []verdict.Condition) []verdict.Condition { return append(a, b...) }

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
		{name: "a workload the tool cannot judge seen before the first observation, and a failure before it",
			short: true, events: []ev{set(3, agent), set(18, oneDown())}, first: 30, now: 30, ended: true, settled: -1, end: 30,
			restarts: []string{notComplete(16)},
			before: two(cond(verdict.InstallFailed, 20, atLimit+notComplete(16)),
				cond(verdict.WorkloadUnsupported, 3, agentText))},
		{name: "and a break that began after the first observation is not counted",
			events: []ev{set(3, agent), set(8, oneDown())}, first: 5, now: 20, ended: true, settled: -1, end: 5,
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
