package watch

import (
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/kubectl/pkg/polymorphichelpers"
)

// The builders below serve every test file of the package. Every object is in
// namespace orders, a workload is labelled app: NAME, and a UID is "uid-NAME"
// unless the test gives one.

var t0 = time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)

func i32(v int32) *int32 { return &v }

func objectMeta(name string, generation int64) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: "orders", UID: types.UID("uid-" + name),
		Generation: generation, Labels: map[string]string{"app": name}}
}

// deployment is a Deployment that rolls; available also sets readyReplicas.
func deployment(name string, generation, observed int64, spec, updated, available, total int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: objectMeta(name, generation),
		Spec: appsv1.DeploymentSpec{Replicas: i32(spec),
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RollingUpdateDeploymentStrategyType}},
		Status: appsv1.DeploymentStatus{ObservedGeneration: observed, Replicas: total,
			UpdatedReplicas: updated, AvailableReplicas: available, ReadyReplicas: available},
	}
}

// statefulSet with RollingUpdate carries partition 0, as the API server defaults it.
func statefulSet(name string, generation, observed int64, replicas, ready, updated int32,
	strategy appsv1.StatefulSetUpdateStrategyType) *appsv1.StatefulSet {
	s := &appsv1.StatefulSet{
		ObjectMeta: objectMeta(name, generation),
		Spec: appsv1.StatefulSetSpec{Replicas: i32(replicas),
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{Type: strategy}},
		Status: appsv1.StatefulSetStatus{ObservedGeneration: observed, Replicas: replicas,
			ReadyReplicas: ready, UpdatedReplicas: updated,
			CurrentRevision: name + "-1", UpdateRevision: name + "-1"},
	}
	if strategy == appsv1.RollingUpdateStatefulSetStrategyType {
		s.Spec.UpdateStrategy.RollingUpdate = &appsv1.RollingUpdateStatefulSetStrategy{Partition: i32(0)}
	}
	return s
}

func daemonSet(name string, generation, observed int64, desired, updated, available int32,
	strategy appsv1.DaemonSetUpdateStrategyType) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: objectMeta(name, generation),
		Spec:       appsv1.DaemonSetSpec{UpdateStrategy: appsv1.DaemonSetUpdateStrategy{Type: strategy}},
		Status: appsv1.DaemonSetStatus{ObservedGeneration: observed, DesiredNumberScheduled: desired,
			UpdatedNumberScheduled: updated, NumberAvailable: available},
	}
}

func controller(kind, name string) []metav1.OwnerReference {
	yes := true
	return []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: kind, Name: name,
		UID: types.UID("uid-" + name), Controller: &yes}}
}

// replicaSet is a ReplicaSet whose controller is the Deployment ownerDeployment.
func replicaSet(name, ownerDeployment string) *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "orders",
		UID: types.UID("uid-" + name), OwnerReferences: controller("Deployment", ownerDeployment)}}
}

// pod is a running pod whose controller is ownerKind/ownerName. deleting sets
// a deletion timestamp an hour after t0: the deadline that the API server
// writes, which the Tracker never reads as a time.
func pod(name, uid, ownerKind, ownerName string, ready, deleting bool) *corev1.Pod {
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "orders", UID: types.UID(uid),
			OwnerReferences: controller(ownerKind, ownerName)},
		Status: corev1.PodStatus{Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: status}}},
	}
	if deleting {
		d := metav1.NewTime(t0.Add(time.Hour))
		p.DeletionTimestamp = &d
	}
	return p
}

type rolloutCase struct {
	name string
	obj  runtime.Object
	done bool
	msg  string
}

// rolloutCases are the cases (a) to (n) of the rollout record, and a few more.
func rolloutCases() []rolloutCase {
	const stsRolling = appsv1.RollingUpdateStatefulSetStrategyType
	const dsRolling = appsv1.RollingUpdateDaemonSetStrategyType
	const onlyRolling = "rollout status is only available for RollingUpdate strategy type"
	deadline := deployment("orders", 2, 2, 2, 1, 1, 2)
	deadline.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentProgressing,
		Status: corev1.ConditionFalse, Reason: "ProgressDeadlineExceeded",
		Message: `ReplicaSet "orders-2" has timed out progressing.`}}
	partitioned := statefulSet("web", 2, 2, 3, 3, 1, stsRolling)
	partitioned.Spec.UpdateStrategy.RollingUpdate.Partition = i32(2)
	// Without rollingUpdate, kubectl's rule compares the revisions; the builder leaves
	// currentReplicas at 0, which kubectl prints when the revisions are equal.
	revisionShort := statefulSet("web", 2, 2, 2, 2, 1, stsRolling)
	revisionShort.Spec.UpdateStrategy.RollingUpdate = nil
	revisionShort.Status.UpdateRevision = "web-2"
	revisionDone := statefulSet("web", 2, 2, 2, 2, 2, stsRolling)
	revisionDone.Spec.UpdateStrategy.RollingUpdate = nil
	return []rolloutCase{
		{"(a) deployment rolled out", deployment("orders", 2, 2, 2, 2, 2, 2), true,
			`deployment "orders" successfully rolled out`},
		{"(b) deployment, updated short", deployment("orders", 2, 2, 2, 1, 1, 2), false,
			`Waiting for deployment "orders" rollout to finish: 1 out of 2 new replicas have been updated...`},
		{"(c) deployment, old replicas still counted", deployment("orders", 2, 2, 2, 2, 2, 3), false,
			`Waiting for deployment "orders" rollout to finish: 1 old replicas are pending termination...`},
		{"(d) deployment, available short", deployment("orders", 2, 2, 2, 2, 1, 2), false,
			`Waiting for deployment "orders" rollout to finish: 1 of 2 updated replicas are available...`},
		{"(e) deployment, generation not observed", deployment("orders", 2, 1, 2, 2, 2, 2), false,
			"Waiting for deployment spec update to be observed..."},
		{"(f) deployment past its progress deadline", deadline, false,
			`deployment "orders" exceeded its progress deadline (condition Progressing, reason ProgressDeadlineExceeded): ReplicaSet "orders-2" has timed out progressing.`},
		{"(g) deployment unchanged", deployment("orders", 1, 1, 2, 2, 2, 2), true,
			`deployment "orders" successfully rolled out`},
		{"(h) statefulset rolled out", statefulSet("web", 2, 2, 2, 2, 2, stsRolling), true,
			"partitioned roll out complete: 2 new pods have been updated..."},
		{"(i) statefulset, updated short", statefulSet("web", 2, 2, 2, 2, 1, stsRolling), false,
			"Waiting for partitioned roll out to finish: 1 out of 2 new pods have been updated..."},
		{"statefulset, ready short", statefulSet("web", 2, 2, 2, 1, 2, stsRolling), false,
			"Waiting for 1 pods to be ready..."},
		{"statefulset never observed", statefulSet("web", 1, 0, 2, 0, 0, stsRolling), false,
			"Waiting for statefulset spec update to be observed..."},
		{"(j) statefulset with a partition", partitioned, true,
			"partitioned roll out complete: 1 new pods have been updated..."},
		{"statefulset without rollingUpdate, revisions differ", revisionShort, false,
			"waiting for statefulset rolling update to complete 1 pods at revision web-2..."},
		{"statefulset without rollingUpdate, revisions equal", revisionDone, true,
			"statefulset rolling update complete 0 pods at revision web-1..."},
		{"(k) statefulset with OnDelete", statefulSet("web", 2, 2, 2, 2, 0, appsv1.OnDeleteStatefulSetStrategyType),
			false, onlyRolling},
		{"(l) daemonset rolled out", daemonSet("agent", 2, 2, 3, 3, 3, dsRolling), true,
			`daemon set "agent" successfully rolled out`},
		{"(m) daemonset, available short", daemonSet("agent", 2, 2, 3, 3, 2, dsRolling), false,
			`Waiting for daemon set "agent" rollout to finish: 2 of 3 updated pods are available...`},
		{"daemonset, updated short", daemonSet("agent", 2, 2, 3, 1, 3, dsRolling), false,
			`Waiting for daemon set "agent" rollout to finish: 1 out of 3 new pods have been updated...`},
		{"daemonset, generation not observed", daemonSet("agent", 2, 1, 3, 3, 3, dsRolling), false,
			"Waiting for daemon set spec update to be observed..."},
		{"(n) daemonset with OnDelete", daemonSet("agent", 2, 2, 3, 0, 3, appsv1.OnDeleteDaemonSetStrategyType),
			false, onlyRolling},
		{"a pod", pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, false), false,
			"not a watched kind"},
	}
}

func TestComplete(t *testing.T) {
	for _, c := range rolloutCases() {
		if done, msg := Complete(c.obj); done != c.done || msg != c.msg {
			t.Errorf("%s: Complete = (%v, %q), want (%v, %q)", c.name, done, msg, c.done, c.msg)
		}
	}
}

// TestCompleteAgreesWithKubectl holds the copy to kubectl's own rules.
func TestCompleteAgreesWithKubectl(t *testing.T) {
	for _, c := range rolloutCases() {
		var kind string
		switch c.obj.(type) {
		case *appsv1.Deployment:
			kind = "Deployment"
		case *appsv1.StatefulSet:
			kind = "StatefulSet"
		case *appsv1.DaemonSet:
			kind = "DaemonSet"
		default:
			continue // kubectl has no rule for this type
		}
		viewer, err := polymorphichelpers.StatusViewerFor(schema.GroupKind{Group: "apps", Kind: kind})
		if err != nil {
			t.Fatal(err)
		}
		m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(c.obj)
		if err != nil {
			t.Fatal(err)
		}
		msg, done, err := viewer.Status(&unstructured.Unstructured{Object: m}, 0)
		gotDone, gotMsg := Complete(c.obj)
		if err != nil {
			// kubectl gives up with an error; the copy says "not complete".
			if gotDone {
				t.Errorf("%s: kubectl: %v; Complete: true", c.name, err)
			}
			continue
		}
		if gotDone != done || gotMsg != strings.TrimSuffix(msg, "\n") {
			t.Errorf("%s: Complete = (%v, %q); kubectl = (%v, %q)", c.name, gotDone, gotMsg, done, msg)
		}
	}
}

func TestRolling(t *testing.T) {
	recreate := deployment("orders", 1, 1, 2, 2, 2, 2)
	recreate.Spec.Strategy = appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
	for _, c := range []struct {
		name string
		obj  runtime.Object
		want bool
	}{
		{"deployment, RollingUpdate", deployment("orders", 1, 1, 2, 2, 2, 2), true},
		{"deployment, Recreate", recreate, true},
		{"statefulset, RollingUpdate", statefulSet("web", 1, 1, 2, 2, 2, appsv1.RollingUpdateStatefulSetStrategyType), true},
		{"statefulset, OnDelete", statefulSet("web", 1, 1, 2, 2, 2, appsv1.OnDeleteStatefulSetStrategyType), false},
		{"daemonset, RollingUpdate", daemonSet("agent", 1, 1, 3, 3, 3, appsv1.RollingUpdateDaemonSetStrategyType), true},
		{"daemonset, OnDelete", daemonSet("agent", 1, 1, 3, 3, 3, appsv1.OnDeleteDaemonSetStrategyType), false},
		{"a pod", pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, false), false},
	} {
		if got := Rolling(c.obj); got != c.want {
			t.Errorf("Rolling(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}
