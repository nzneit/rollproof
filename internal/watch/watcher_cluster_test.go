//go:build linux && cluster

package watch

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"rollproof/internal/cluster"
	"rollproof/internal/verdict"
)

// pauseImage is held by every node of the linked kind release, so no pull is
// needed; docs/proofs/p12-keys.md records the tag.
const pauseImage = "registry.k8s.io/pause:3.10"

func TestWatcherOnACluster(t *testing.T) {
	ctx := t.Context()
	engine, err := cluster.CheckRunner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const name = "rollproof-test-watch"
	c, err := cluster.Create(ctx, cluster.Spec{Name: name, RunDir: cluster.RunDir(name), Workers: 2,
		Namespace: "w", Timeout: 5 * time.Minute}, engine)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Delete(context.Background()); err != nil {
			t.Error(err)
		}
	})
	client, err := c.Client()
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{"app": "w"}
	replicas := int32(2)
	_, err = client.AppsV1().Deployments("w").Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "w", Labels: labels},
		Spec: appsv1.DeploymentSpec{Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "pause", Image: pauseImage,
					ImagePullPolicy: corev1.PullIfNotPresent}}}}},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CoreV1().Services("w").Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "w"},
		Spec: corev1.ServiceSpec{Selector: labels,
			Ports: []corev1.ServicePort{{Port: 80, TargetPort: intstr.FromInt32(80)}}},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	w, err := Start(ctx, client, Options{Namespace: "w", Settle: 5 * time.Second, Timeout: 5 * time.Minute,
		Workers: 2, Service: &types.NamespacedName{Namespace: "w", Name: "w"}, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	exited := make(chan struct{})
	close(exited)
	if _, err := w.InstallReady(ctx, time.Now(), exited); err != nil {
		t.Fatal(err)
	}

	began := time.Now()
	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`,
		began.UTC().Format(time.RFC3339))
	if _, err := client.AppsV1().Deployments("w").Patch(ctx, "w", types.StrategicMergePatchType, []byte(patch),
		metav1.PatchOptions{}); err != nil {
		t.Fatal(err)
	}
	tr := w.Transition(ctx, verdict.Deploy, began, exited)
	if tr.Stalled || !tr.Replaced || len(tr.Workloads) != 1 {
		t.Fatalf("transition: %+v", tr)
	}
	// A rollout on a live API server is no sign of missed changes (T27-1).
	if !tr.Missed.IsZero() {
		t.Errorf("Missed = %v; want zero, as the watch missed nothing", tr.Missed)
	}
	got := tr.Workloads[0]
	t.Logf("quickest stop %v, digests %v", got.QuickestStop, got.Digests)
	if got.Workload.String() != "deployment/w" || got.AtStart != 2 || got.Replaced != 2 || got.Created != 2 ||
		got.Probed != "yes" {
		t.Errorf("count: %+v; want deployment/w, 2 at the start, 2 replaced, 2 created, probed yes", got)
	}
	if got.QuickestStop <= 0 {
		t.Errorf("QuickestStop = %v, want more than zero", got.QuickestStop)
	}
	if len(got.Digests) != 1 || !strings.HasPrefix(got.Digests[0], "sha256:") {
		t.Errorf("Digests = %v, want one digest that starts sha256:", got.Digests)
	}
}
