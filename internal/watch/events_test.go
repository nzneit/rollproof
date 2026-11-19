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
