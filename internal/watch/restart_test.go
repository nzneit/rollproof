package watch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// restartObjects: four workloads that update by rolling, and deployment/batch, which the
// selector app!=batch leaves out.
func restartObjects() []runtime.Object {
	const rollingSet, rollingDaemon = appsv1.RollingUpdateStatefulSetStrategyType, appsv1.RollingUpdateDaemonSetStrategyType
	return []runtime.Object{
		deployment("orders", 1, 1, 2, 2, 2, 2), deployment("api", 1, 1, 1, 1, 1, 1), deployment("batch", 1, 1, 1, 1, 1, 1),
		statefulSet("web", 1, 1, 1, 1, 1, rollingSet), daemonSet("logs", 1, 1, 3, 3, 3, rollingDaemon),
	}
}

const (
	restartedAtKey = "kubectl.kubernetes.io/restartedAt"
	restartPatch   = `{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"2026-11-13T10:00:00Z"}}}}}`
)

// patches gives each patch the fake client recorded, as RESOURCE/NAME, and fails the test unless
// every one is a strategic merge patch whose body is restartPatch.
func patches(t *testing.T, client *fake.Clientset) []string {
	t.Helper()
	var out []string
	for _, a := range client.Actions() {
		p, ok := a.(k8stesting.PatchAction)
		if !ok || a.GetVerb() != "patch" {
			continue
		}
		if p.GetPatchType() != types.StrategicMergePatchType || string(p.GetPatch()) != restartPatch {
			t.Errorf("patch of %s/%s: type %s, body %s", a.GetResource().Resource, p.GetName(), p.GetPatchType(), p.GetPatch())
		}
		out = append(out, a.GetResource().Resource+"/"+p.GetName())
	}
	return out
}

// restartedAt reads the annotation from the pod template of RESOURCE/NAME in the fake client.
func restartedAt(t *testing.T, client *fake.Clientset, workload string) string {
	t.Helper()
	resource, name, _ := strings.Cut(workload, "/")
	apps := client.AppsV1()
	var ann map[string]string
	var err error
	switch resource {
	case "deployments":
		var o *appsv1.Deployment
		o, err = apps.Deployments("orders").Get(t.Context(), name, metav1.GetOptions{})
		if err == nil {
			ann = o.Spec.Template.Annotations
		}
	case "statefulsets":
		var o *appsv1.StatefulSet
		o, err = apps.StatefulSets("orders").Get(t.Context(), name, metav1.GetOptions{})
		if err == nil {
			ann = o.Spec.Template.Annotations
		}
	case "daemonsets":
		var o *appsv1.DaemonSet
		o, err = apps.DaemonSets("orders").Get(t.Context(), name, metav1.GetOptions{})
		if err == nil {
			ann = o.Spec.Template.Annotations
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	return ann[restartedAtKey]
}

func TestRestartChangesEveryWatchedWorkload(t *testing.T) {
	w, client, _ := startWatcherWith(t, func(o *Options) { o.Selector = "app!=batch" }, nil, restartObjects()...)
	got := w.Restart(t.Context())
	if d := cmp.Diff(Restart{Began: t0, Ended: t0, Accepted: true}, got); d != "" {
		t.Errorf("Restart (-want +got):\n%s", d)
	}
	want := []string{"daemonsets/logs", "deployments/api", "deployments/orders", "statefulsets/web"}
	if d := cmp.Diff(want, patches(t, client)); d != "" {
		t.Errorf("patches (-want +got):\n%s", d)
	}
	for _, wl := range want {
		if got := restartedAt(t, client, wl); got != "2026-11-13T10:00:00Z" {
			t.Errorf("%s has %s %q", wl, restartedAtKey, got)
		}
	}
	if got := restartedAt(t, client, "deployments/batch"); got != "" {
		t.Errorf("deployment/batch, which the selector leaves out, has %s %q", restartedAtKey, got)
	}
}

// Review Focus 5: one workload is refused after another was already changed.
func TestRestartStopsAtTheFirstRefusal(t *testing.T) {
	w, client, clock := startWatcherWith(t, func(o *Options) { o.Selector = "app!=batch" }, nil, restartObjects()...)
	client.PrependReactor("patch", "deployments", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.(k8stesting.PatchAction).GetName() != "orders" {
			return false, nil, nil
		}
		clock.Advance(2 * time.Second)
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, "orders",
			errors.New(`admission webhook "policy.example" denied the request`))
	})
	got := w.Restart(t.Context())
	if got.Accepted || got.Workload != "deployment/orders" || !got.Began.Equal(t0) || !got.Ended.Equal(at(2)) {
		t.Errorf("Restart gave %+v; want refused by deployment/orders, begun at t0, ended at +2 s", got)
	}
	if !strings.Contains(got.Text, `admission webhook "policy.example" denied the request`) {
		t.Errorf("text %q does not hold the API server's answer", got.Text)
	}
	want := []string{"daemonsets/logs", "deployments/api", "deployments/orders"}
	if d := cmp.Diff(want, patches(t, client)); d != "" {
		t.Errorf("patches (-want +got):\n%s", d)
	}
	for _, wl := range []string{"daemonsets/logs", "deployments/api"} {
		if got := restartedAt(t, client, wl); got != "2026-11-13T10:00:00Z" {
			t.Errorf("%s, changed before the refusal, has %s %q", wl, restartedAtKey, got)
		}
	}
	if got := restartedAt(t, client, "statefulsets/web"); got != "" {
		t.Errorf("statefulset/web, after the refusal, has %s %q", restartedAtKey, got)
	}
}

func TestRestartWhenCtxHasEnded(t *testing.T) {
	w, client, _ := startWatcherWith(t, func(o *Options) { o.Selector = "app!=batch" }, nil, restartObjects()...)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got := w.Restart(ctx)
	if got.Accepted || got.Workload != "" || got.Text != context.Canceled.Error() {
		t.Errorf("Restart with an ended ctx gave %+v; want not accepted, no workload, ctx's error", got)
	}
	if p := patches(t, client); len(p) != 0 {
		t.Errorf("patches %v after ctx ended", p)
	}
}

func TestRestartWithNothingToRestart(t *testing.T) {
	w, client, _ := startWatcher(t, nil)
	if d := cmp.Diff(Restart{Began: t0, Ended: t0, Accepted: true}, w.Restart(t.Context())); d != "" {
		t.Errorf("Restart (-want +got):\n%s", d)
	}
	if p := patches(t, client); len(p) != 0 {
		t.Errorf("patches %v with nothing to restart", p)
	}
}
