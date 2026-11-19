package watch

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// restartedAtAnnotation is the pod-template annotation that kubectl rollout restart changes.
const restartedAtAnnotation = "kubectl.kubernetes.io/restartedAt"

// Restart is how the tool's attempt to restart the watched workloads went. Began is the clock
// before the first change and Ended the moment the last change returned or the first refusal
// came: the restart's "command has exited". Accepted says that the API server took every change.
// When it refused one, Workload names that workload, as deployment/orders, and Text is the
// server's answer.
type Restart struct {
	Began, Ended   time.Time
	Accepted       bool
	Workload, Text string
}

// Restart restarts every workload that exists and matches the selector when it is called, as
// README decision 3 says: one at a time, stopping at the first refusal. Accepted is true only when
// the API server took every change, and so also when there was nothing to change. When ctx ends
// first, Accepted is false and Text is ctx's error: the caller tells that from a refusal.
func (w *Watcher) Restart(ctx context.Context) Restart {
	w.mu.Lock()
	watched, _, _ := w.tr.installWatched()
	r := Restart{Began: w.o.Now(), Accepted: true}
	w.mu.Unlock()
	r.Ended = r.Began

	patch := restartPatchBody(r.Began)
	for _, workload := range watched {
		if err := ctx.Err(); err != nil {
			r.Accepted, r.Text = false, err.Error()
			break
		}
		if err := w.restart(ctx, workload, patch); err != nil {
			r.Accepted, r.Workload, r.Text = false, workload.String(), err.Error()
			break
		}
	}

	w.mu.Lock()
	r.Ended = w.o.Now()
	w.mu.Unlock()
	return r
}

// restart writes the pod-template annotation that kubectl rollout restart writes on one workload,
// with the strategic merge patch decision 3 names.
func (w *Watcher) restart(ctx context.Context, workload Workload, patch []byte) error {
	opts := metav1.PatchOptions{FieldManager: "rollproof"}
	var err error
	switch workload.Kind {
	case "deployment":
		_, err = w.client.AppsV1().Deployments(w.o.Namespace).Patch(ctx, workload.Name,
			types.StrategicMergePatchType, patch, opts)
	case "statefulset":
		_, err = w.client.AppsV1().StatefulSets(w.o.Namespace).Patch(ctx, workload.Name,
			types.StrategicMergePatchType, patch, opts)
	case "daemonset":
		_, err = w.client.AppsV1().DaemonSets(w.o.Namespace).Patch(ctx, workload.Name,
			types.StrategicMergePatchType, patch, opts)
	default:
		return fmt.Errorf("restart %s: no such kind is watched", workload)
	}
	return err
}

// restartPatchBody is the strategic merge patch that gives a workload's pod template the
// restartedAt annotation with at, read once at the restart's start.
func restartPatchBody(at time.Time) []byte {
	return fmt.Appendf(nil, `{"spec":{"template":{"metadata":{"annotations":{%q:%q}}}}}`,
		restartedAtAnnotation, at.Format(time.RFC3339))
}
