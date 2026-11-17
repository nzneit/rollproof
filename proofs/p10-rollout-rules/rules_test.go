package rules

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kubectl/pkg/polymorphichelpers"
)

func int32p(n int32) *int32 { return &n }

func baseDeployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Generation: 2},
		Spec:       appsv1.DeploymentSpec{Replicas: int32p(3)},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 2, Replicas: 3, UpdatedReplicas: 3, ReadyReplicas: 3, AvailableReplicas: 3,
		},
	}
}

func baseStatefulSet() *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Generation: 2},
		Spec: appsv1.StatefulSetSpec{
			Replicas: int32p(3),
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type:          appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{Partition: int32p(0)},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 2, Replicas: 3, ReadyReplicas: 3, CurrentReplicas: 3, UpdatedReplicas: 3,
			CurrentRevision: "db-2", UpdateRevision: "db-2",
		},
	}
}

func baseDaemonSet() *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Generation: 2},
		Spec: appsv1.DaemonSetSpec{
			UpdateStrategy: appsv1.DaemonSetUpdateStrategy{Type: appsv1.RollingUpdateDaemonSetStrategyType},
		},
		Status: appsv1.DaemonSetStatus{
			ObservedGeneration: 2, DesiredNumberScheduled: 3, UpdatedNumberScheduled: 3, NumberReady: 3, NumberAvailable: 3,
		},
	}
}

// status runs kubectl's rule for the object's kind, as kubectl rollout status does with revision 0.
func status(t *testing.T, obj runtime.Object, gk schema.GroupKind) (string, bool, error) {
	t.Helper()
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	u := &unstructured.Unstructured{Object: m}
	v, err := polymorphichelpers.StatusViewerFor(gk)
	if err != nil {
		t.Fatalf("viewer: %v", err)
	}
	return v.Status(u, 0)
}

var (
	depGK = appsv1.SchemeGroupVersion.WithKind("Deployment").GroupKind()
	stsGK = appsv1.SchemeGroupVersion.WithKind("StatefulSet").GroupKind()
	dsGK  = appsv1.SchemeGroupVersion.WithKind("DaemonSet").GroupKind()
)

func TestRolloutRules(t *testing.T) {
	type tc struct {
		name   string
		obj    runtime.Object
		gk     schema.GroupKind
		assert func(t *testing.T, done bool, err error)
	}
	doneTrue := func(t *testing.T, done bool, err error) {
		if !done || err != nil {
			t.Errorf("want done true and no error, got done %v, error %v", done, err)
		}
	}
	doneFalse := func(t *testing.T, done bool, err error) {
		if done {
			t.Errorf("want done false, got true (error %v)", err)
		}
	}
	logOnly := func(t *testing.T, done bool, err error) {}
	wantErr := func(t *testing.T, done bool, err error) {
		if err == nil {
			t.Errorf("want an error, got none (done %v)", done)
		}
	}

	b := func(f func(d *appsv1.Deployment)) *appsv1.Deployment { d := baseDeployment(); f(d); return d }
	s := func(f func(d *appsv1.StatefulSet)) *appsv1.StatefulSet { d := baseStatefulSet(); f(d); return d }
	d := func(f func(d *appsv1.DaemonSet)) *appsv1.DaemonSet { x := baseDaemonSet(); f(x); return x }

	cases := []tc{
		{"a", baseDeployment(), depGK, doneTrue},
		{"b", b(func(x *appsv1.Deployment) { x.Status.UpdatedReplicas = 2 }), depGK, doneFalse},
		{"c", b(func(x *appsv1.Deployment) { x.Status.Replicas = 4 }), depGK, doneFalse},
		{"d", b(func(x *appsv1.Deployment) { x.Status.AvailableReplicas = 2 }), depGK, doneFalse},
		{"e", b(func(x *appsv1.Deployment) { x.Generation = 3 }), depGK, doneFalse},
		{"f", b(func(x *appsv1.Deployment) {
			x.Status.Conditions = []appsv1.DeploymentCondition{{
				Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse, Reason: "ProgressDeadlineExceeded",
			}}
		}), depGK, wantErr},
		{"g", b(func(x *appsv1.Deployment) { x.Generation = 1; x.Status.ObservedGeneration = 1 }), depGK, doneTrue},
		{"h", baseStatefulSet(), stsGK, doneTrue},
		{"i", s(func(x *appsv1.StatefulSet) { x.Status.UpdatedReplicas = 2; x.Status.CurrentRevision = "db-1" }), stsGK, doneFalse},
		{"j", s(func(x *appsv1.StatefulSet) {
			x.Spec.UpdateStrategy.RollingUpdate.Partition = int32p(2)
			x.Status.UpdatedReplicas = 1
			x.Status.CurrentRevision = "db-1"
		}), stsGK, logOnly},
		{"k", s(func(x *appsv1.StatefulSet) {
			x.Spec.UpdateStrategy = appsv1.StatefulSetUpdateStrategy{Type: appsv1.OnDeleteStatefulSetStrategyType}
		}), stsGK, logOnly},
		{"l", baseDaemonSet(), dsGK, doneTrue},
		{"m", d(func(x *appsv1.DaemonSet) { x.Status.NumberAvailable = 2 }), dsGK, doneFalse},
		{"n", d(func(x *appsv1.DaemonSet) {
			x.Spec.UpdateStrategy = appsv1.DaemonSetUpdateStrategy{Type: appsv1.OnDeleteDaemonSetStrategyType}
		}), dsGK, logOnly},
		{"o", b(func(x *appsv1.Deployment) { x.Status.TerminatingReplicas = int32p(2) }), depGK, doneTrue},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msg, done, err := status(t, c.obj, c.gk)
			t.Logf("message %q, done %v, error %v", msg, done, err)
			c.assert(t, done, err)
		})
	}
}
