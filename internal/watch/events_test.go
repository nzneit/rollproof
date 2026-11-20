package watch

import (
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"

	"rollproof/internal/verdict"
)

// event is a core/v1 Event about KIND/NAME in ns, counted once, with the given time fields
// (a zero time leaves its field unset).
func event(ns, name, kind, object, typ, reason, message string, last, eventTime, first time.Time) *corev1.Event {
	e := &corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		InvolvedObject: corev1.ObjectReference{Kind: kind, Name: object, Namespace: ns},
		Type:           typ, Reason: reason, Message: message, Count: 1}
	if !last.IsZero() {
		e.LastTimestamp = metav1.NewTime(last)
	}
	if !eventTime.IsZero() {
		e.EventTime = metav1.NewMicroTime(eventTime)
	}
	if !first.IsZero() {
		e.FirstTimestamp = metav1.NewTime(first)
	}
	return e
}

// eventObjects: two events of orders, one of the probe's namespace and one of another, each with
// its time in a different field.
func eventObjects() []runtime.Object {
	return append(base(),
		event("orders", "e1", "Pod", "orders-b-1", "Warning", "FailedScheduling",
			"0/4 nodes are available:\n4 Insufficient cpu.", at(2), time.Time{}, time.Time{}),
		event("orders", "e2", "Deployment", "orders", "Normal", "ScalingReplicaSet",
			"Scaled up replica set orders-b from 0 to 1", time.Time{}, t0, time.Time{}),
		event("rollproof-probe", "e3", "Pod", "probe", "Normal", "Pulled",
			"Container image already present on machine", time.Time{}, time.Time{}, at(1)),
		event("elsewhere", "e4", "Pod", "x", "Normal", "Pulled", "not ours", at(3), time.Time{}, time.Time{}))
}

const (
	scaledLine = "2026-11-13T10:00:00.000Z\torders\tNormal\tScalingReplicaSet\tdeployment/orders\t1\tScaled up replica set orders-b from 0 to 1\n"
	pulledLine = "2026-11-13T10:00:01.000Z\trollproof-probe\tNormal\tPulled\tpod/probe\t1\tContainer image already present on machine\n"
	failedLine = "2026-11-13T10:00:02.000Z\torders\tWarning\tFailedScheduling\tpod/orders-b-1\t1\t0/4 nodes are available: 4 Insufficient cpu.\n"
)

func TestWriteEvents(t *testing.T) {
	w, _, _ := startWatcherWith(t, func(o *Options) { o.ProbeNamespace = "rollproof-probe" }, nil, eventObjects()...)
	var b strings.Builder
	if err := w.WriteEvents(t.Context(), &b); err != nil {
		t.Fatal(err)
	}
	if got, want := b.String(), scaledLine+pulledLine+failedLine; got != want {
		t.Errorf("events:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteEventsWhenAListFails(t *testing.T) {
	w, client, _ := startWatcherWith(t, func(o *Options) { o.ProbeNamespace = "rollproof-probe" }, nil, eventObjects()...)
	client.PrependReactor("list", "events", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetNamespace() == "rollproof-probe" {
			return true, nil, errors.New("connection refused")
		}
		return false, nil, nil
	})
	var b strings.Builder
	err := w.WriteEvents(t.Context(), &b)
	if err == nil || !strings.Contains(err.Error(), "rollproof-probe") || !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error %v, want one that names rollproof-probe and holds connection refused", err)
	}
	if got, want := b.String(), scaledLine+failedLine; got != want {
		t.Errorf("events:\n%s\nwant:\n%s", got, want)
	}
}

func TestTransitionStallNamesTheLastEvent(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []runtime.Object
		want   string
	}{
		{"the pod's event is the last", []runtime.Object{
			event("orders", "e1", "Deployment", "orders", "Normal", "ScalingReplicaSet", "Scaled up replica set orders-b from 0 to 1", at(1), time.Time{}, time.Time{}),
			event("orders", "e2", "Pod", "orders-b-1", "Warning", "FailedScheduling", "0/4 nodes are available:\n4 Insufficient cpu.", at(2), time.Time{}, time.Time{}),
			event("orders", "e3", "Pod", "unrelated-1", "Warning", "BackOff", "not named by the stall", at(3), time.Time{}, time.Time{}),
			event("elsewhere", "e4", "Pod", "orders-b-1", "Warning", "BackOff", "another namespace", at(4), time.Time{}, time.Time{}),
		}, "pod/orders-b-1: FailedScheduling: 0/4 nodes are available: 4 Insufficient cpu."},
		{"the ReplicaSet's event is the last", []runtime.Object{
			event("orders", "e2", "Pod", "orders-b-1", "Warning", "FailedScheduling", "0/4 nodes are available", at(2), time.Time{}, time.Time{}),
			event("orders", "e5", "ReplicaSet", "orders-b", "Warning", "FailedCreate", `pods "orders-b-2" is forbidden: violates PodSecurity`, at(5), time.Time{}, time.Time{}),
		}, `replicaset/orders-b: FailedCreate: pods "orders-b-2" is forbidden: violates PodSecurity`},
		{"no event names it", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, client, clock := startWatcher(t, nil, append(base(), tc.events...)...)
			ctx := t.Context()
			exited := make(chan struct{})
			res := make(chan Transition, 1)
			go func() { res <- w.Transition(ctx, verdict.Deploy, clock.Now(), exited) }()
			until(t, w, clock, func(s Transition) bool { return s.Name == verdict.Deploy })
			update(t, client, deployment("orders", 2, 2, 2, 0, 2, 3)) // one new pod wanted, none updated
			if _, err := client.AppsV1().ReplicaSets("orders").Create(ctx, replicaSet("orders-b", "orders"), metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := client.CoreV1().Pods("orders").Create(ctx, pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false), metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
			close(exited)
			tr := drive(t, clock, res)
			if !tr.Stalled || tr.LastEvent != tc.want {
				t.Errorf("stalled %v, last event %q; want a stall and %q", tr.Stalled, tr.LastEvent, tc.want)
			}
			if got := tr.Conditions(true)[0].Text; got != "did not finish within limits.timeout" {
				t.Errorf("the stall's text %q changed", got)
			}
		})
	}
}

// A stall can name only a stopping pod of a complete workload (counts_test.go's "the details are
// taken after the change that caused the stall"); the event about that pod is then the last event.
func TestTransitionStallNamesAStoppingPodsLastEvent(t *testing.T) {
	w, client, clock := startWatcher(t, nil, append(base(),
		event("orders", "e1", "Pod", "orders-a-1", "Warning", "FailedKillPod", "kill failed", at(1), time.Time{}, time.Time{}))...)
	ctx := t.Context()
	exited := make(chan struct{})
	res := make(chan Transition, 1)
	go func() { res <- w.Transition(ctx, verdict.Deploy, clock.Now(), exited) }()
	until(t, w, clock, func(s Transition) bool { return s.Name == verdict.Deploy })
	update(t, client, deployment("orders", 2, 1, 2, 2, 2, 2)) // a new spec, not observed yet
	close(exited)
	clock.Advance(289 * time.Second)
	update(t, client, deployment("orders", 2, 2, 2, 2, 2, 2)) // complete at +289 s
	until(t, w, clock, func(s Transition) bool { return s.Finished.Equal(at(289)) })
	// The old pod is told to stop just past limits.timeout: the settle time starts again and the
	// window stalls, with the Deployment complete and only that pod in the details.
	clock.Advance(12 * time.Second) // +301 s
	update(t, client, pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, true))
	tr := receive(t, res)
	if !tr.Stalled {
		t.Fatalf("stalled %v; want a stall", tr.Stalled)
	}
	wantIncomplete := "pod orders-a-1 has been stopping for 0.0 s and is not gone"
	if len(tr.Incomplete) != 1 || tr.Incomplete[0] != wantIncomplete {
		t.Errorf("incomplete %q; want just %q", tr.Incomplete, wantIncomplete)
	}
	if got, want := tr.LastEvent, "pod/orders-a-1: FailedKillPod: kill failed"; got != want {
		t.Errorf("last event %q, want %q", got, want)
	}
}

// A stall whose events cannot be listed gives no last event and the same stall.
func TestTransitionStallWithoutListableEvents(t *testing.T) {
	w, client, clock := startWatcher(t, nil, base()...)
	client.PrependReactor("list", "events", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("connection refused")
	})
	ctx := t.Context()
	exited := make(chan struct{})
	res := make(chan Transition, 1)
	go func() { res <- w.Transition(ctx, verdict.Deploy, clock.Now(), exited) }()
	until(t, w, clock, func(s Transition) bool { return s.Name == verdict.Deploy })
	update(t, client, deployment("orders", 2, 2, 2, 0, 2, 3)) // one new pod wanted, none updated
	if _, err := client.AppsV1().ReplicaSets("orders").Create(ctx, replicaSet("orders-b", "orders"), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Pods("orders").Create(ctx, pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	close(exited)
	tr := drive(t, clock, res)
	if !tr.Stalled || tr.LastEvent != "" {
		t.Errorf("stalled %v, last event %q; want a stall and no last event", tr.Stalled, tr.LastEvent)
	}
	if got := tr.Conditions(true)[0].Text; got != "did not finish within limits.timeout" {
		t.Errorf("the stall's text %q changed", got)
	}
}
