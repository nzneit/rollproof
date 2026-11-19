package watch

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WriteEvents lists the events of Namespace and of ProbeNamespace once and writes them,
// oldest first, one line each, tab-separated: the time in RFC 3339 with
// milliseconds in UTC, the namespace, the type, the reason, the object as
// kind/name in lower case, the count, and the message with line breaks as
// spaces. It writes what it could list; the error names each namespace whose
// list failed, or the failed write. It names no file; the caller bounds ctx.
func (w *Watcher) WriteEvents(ctx context.Context, out io.Writer) error {
	namespaces := []string{w.o.Namespace}
	if ns := w.o.ProbeNamespace; ns != "" && ns != w.o.Namespace {
		namespaces = append(namespaces, ns)
	}
	var events []corev1.Event
	var errs []error
	for _, ns := range namespaces {
		list, err := w.client.CoreV1().Events(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			errs = append(errs, fmt.Errorf("list the events of namespace %s: %w", ns, err))
			continue
		}
		events = append(events, list.Items...)
	}
	slices.SortStableFunc(events, func(a, b corev1.Event) int {
		return cmp.Or(eventTime(&a).Compare(eventTime(&b)),
			cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Name, b.Name))
	})
	for i := range events {
		e := &events[i]
		line := fmt.Sprintf("%s\t%s\t%s\t%s\t%s/%s\t%d\t%s\n",
			eventTime(e).UTC().Format("2006-01-02T15:04:05.000Z07:00"), e.Namespace, e.Type, e.Reason,
			strings.ToLower(e.InvolvedObject.Kind), e.InvolvedObject.Name, e.Count, oneLine(e.Message))
		if _, err := io.WriteString(out, line); err != nil {
			errs = append(errs, fmt.Errorf("write the events: %w", err))
			break
		}
	}
	return errors.Join(errs...)
}

// eventTime is the moment an event tells of: its LastTimestamp, else its EventTime, else its
// FirstTimestamp, else its creation time. An event that the core API's recorder wrote carries the
// first and the last; one that the events API wrote may carry EventTime alone.
func eventTime(e *corev1.Event) time.Time {
	switch {
	case !e.LastTimestamp.IsZero():
		return e.LastTimestamp.Time
	case !e.EventTime.IsZero():
		return e.EventTime.Time
	case !e.FirstTimestamp.IsZero():
		return e.FirstTimestamp.Time
	}
	return e.CreationTimestamp.Time
}

// oneLine is s with each line break, CR LF, LF or CR, made a space.
func oneLine(s string) string {
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
}
