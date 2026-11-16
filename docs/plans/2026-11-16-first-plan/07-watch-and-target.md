> Part of the plan in `README.md` of this folder. Read that file first: its Global Constraints apply to every task here.

### Task 25: `watch`: the rollout rules, and when a transition has finished

**Files:**
- Create: `internal/watch/rollout.go`, `internal/watch/tracker.go`
- Test: `internal/watch/rollout_test.go`, `internal/watch/tracker_test.go`

**Interfaces:**
- Consumes: `type WindowName` (Task 12); the rules and the decision that `docs/proofs/p10-rollout-rules.md` records (Task 8).
- Produces: `module internal/watch`
  What happens to the watched workloads of one namespace: when an install
  is ready, when a transition has finished or stalled, and which pods were
  replaced. It hides the rollout rules of each kind, pod ownership and the
  settle time. It changes nothing in the cluster.
- Produces: `type Workload struct { Kind, Name string }`
  One watched workload. Kind is deployment, statefulset or daemonset.
- Produces: `func (w Workload) String() string`
  The kind, a slash and the name: deployment/orders.
- Produces: `func Complete(obj runtime.Object) (bool, string)`
  Whether the rollout of a Deployment, StatefulSet or DaemonSet is
  complete, by the rule that kubectl rollout status uses for its kind, and
  a sentence that says what is still missing. A Deployment past its
  progress deadline is not complete and is not an error.
- Produces: `func Rolling(obj runtime.Object) bool`
  Whether the workload updates by rolling. A Deployment always does, with
  either of its strategies; a StatefulSet or DaemonSet with OnDelete does
  not.
- Produces: `type TrackerOptions struct { Namespace string; Selector labels.Selector; Settle, Timeout time.Duration; Workers int }`
  What a Tracker needs: which workloads are watched, limits.settle,
  limits.timeout, and, for the text of its sentences, the namespace's name
  and the number of worker nodes.
- Produces: `func NewTracker(o TrackerOptions) *Tracker`
  A Tracker with nothing in it. It does no I/O and reads no clock: it is
  told every change of an object and the time of each call.
- Produces: `func (t *Tracker) Set(obj runtime.Object, at time.Time)`
  Tells the Tracker that a Deployment, StatefulSet, DaemonSet, ReplicaSet
  or Pod of the namespace was created or changed, and when it learned it.
- Produces: `func (t *Tracker) Gone(obj runtime.Object, at time.Time)`
  Tells the Tracker that such an object no longer exists.
- Produces: `func (t *Tracker) Begin(name verdict.WindowName, at time.Time)`
  Starts a transition at the given moment. The pods that exist then are
  the ones it may replace. One transition at a time.
- Produces: `func (t *Tracker) CommandExited(at time.Time)`
  Tells the Tracker that the transition's command has exited, however it
  ended.
- Produces: `type Transition struct { Name verdict.WindowName; Began, Finished, Ended time.Time; Stalled bool; SettleRestarts, Incomplete, Unschedulable []string; Unsupported []Workload; Workloads []Count; Replaced bool }`
  What happened in one transition. Finished is when it last came to be
  finished, zero for a stall. Ended is when its window ended. The string
  lists are sentences for the report: what started the settle time again,
  what is not complete, which pods could not be scheduled.
- Produces: `type Count struct { Workload Workload; AtStart, Replaced, Created int; Added, Removed bool; QuickestStop time.Duration; Probed string; Digests []string }`
  One workload in one transition: its pods at the start, how many of those
  are gone, how many pods were created, and the image digests those ran.
  QuickestStop is zero when no ready pod was deleted. Probed is yes, no or
  not known.
- Produces: `func (t *Tracker) State(now time.Time) (Transition, bool)`
  The transition so far, and whether its window has ended: it finished and
  the settle time then passed with no change, or it stalled.

- [ ] **Step 1: Read the decision of the rollout record, and add kubectl for the tests**

Read `docs/proofs/p10-rollout-rules.md`. This task assumes that it decided to copy the rules: `rollout.go` holds a copy of kubectl's three rules, and `TestCompleteAgreesWithKubectl` compares the copy with kubectl's package on the same objects. If the record decided to import, `Complete` calls `polymorphichelpers.StatusViewerFor` and applies the two changes named in Step 4 to its answer, `TestCompleteAgreesWithKubectl` is left out, and every other step stays.

Run: `go get k8s.io/kubectl@v0.37`
Expected: `go.mod` requires `k8s.io/kubectl` v0.37.x, and `k8s.io/client-go` stays at v0.37.x. Only test files import kubectl, so the binary does not link it.

- [ ] **Step 2: Write the failing tests for the rollout rules**

`internal/watch/rollout_test.go`. Its object builders and `t0` serve every test file of the package. The rows are the cases (a) to (n) of the rollout record and four more; each expected message is kubectl's, without its line end.

```go
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
		{"(k) statefulset with OnDelete", statefulSet("web", 2, 2, 2, 2, 0, appsv1.OnDeleteStatefulSetStrategyType),
			false, onlyRolling},
		{"(l) daemonset rolled out", daemonSet("agent", 2, 2, 3, 3, 3, dsRolling), true,
			`daemon set "agent" successfully rolled out`},
		{"(m) daemonset, available short", daemonSet("agent", 2, 2, 3, 3, 2, dsRolling), false,
			`Waiting for daemon set "agent" rollout to finish: 2 of 3 updated pods are available...`},
		{"daemonset, updated short", daemonSet("agent", 2, 2, 3, 1, 3, dsRolling), false,
			`Waiting for daemon set "agent" rollout to finish: 1 out of 3 new pods have been updated...`},
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
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/watch/ -run 'TestComplete|TestRolling' -v`
Expected: FAIL, build failed: `undefined: Complete`

- [ ] **Step 4: Implement `Complete` and `Rolling` in `internal/watch/rollout.go`**

`func Complete(obj runtime.Object) (bool, string)` and `func Rolling(obj runtime.Object) bool`, each with its interface comment from the Interfaces block. The file imports no kubectl package. It starts with the licence header of kubectl's file, unchanged, and a comment that names the source:

```go
/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// The rules of Complete are copied from
// k8s.io/kubectl/pkg/polymorphichelpers/rollout_status.go at kubectl v0.37.0
// (Kubernetes v1.37.0), with two changes: they take typed objects, and where
// kubectl returns an error, Complete answers "not complete". Read them again
// at every Kubernetes minor; TestCompleteAgreesWithKubectl compares them.
```

The rules, in kubectl's order; the first that matches decides. `gen` is `metadata.generation`.

Deployment:
1. `gen > status.observedGeneration`: not complete.
2. The `Progressing` condition has reason `ProgressDeadlineExceeded`: kubectl returns an error; Complete returns not complete with `deployment "NAME" exceeded its progress deadline (condition Progressing, reason ProgressDeadlineExceeded): ` followed by the condition's message.
3. `spec.replicas` is set and `status.updatedReplicas < spec.replicas`: not complete.
4. `status.replicas > status.updatedReplicas` (old replicas still counted): not complete.
5. `status.availableReplicas < status.updatedReplicas`: not complete.
6. Otherwise complete.

StatefulSet:
1. `spec.updateStrategy.type` is not `RollingUpdate`: kubectl returns done with an error; Complete returns not complete, with the error's text as the message.
2. `status.observedGeneration == 0` or `gen > status.observedGeneration`: not complete.
3. `spec.replicas` is set and `status.readyReplicas < spec.replicas`: not complete.
4. `spec.updateStrategy.rollingUpdate` is set: when `spec.replicas` and its `partition` are set and `status.updatedReplicas < spec.replicas - partition`, not complete; otherwise complete. The API server always sets `rollingUpdate` with partition 0, so in a cluster this rule decides.
5. Otherwise `status.updateRevision != status.currentRevision`: not complete; else complete.

DaemonSet:
1. `spec.updateStrategy.type` is not `RollingUpdate`: as rule 1 of the StatefulSet.
2. `gen > status.observedGeneration`: not complete.
3. `status.updatedNumberScheduled < status.desiredNumberScheduled`: not complete.
4. `status.numberAvailable < status.desiredNumberScheduled`: not complete.
5. Otherwise complete.

Any other type gives `(false, "not a watched kind")`. `Rolling` is true for every Deployment, true for a StatefulSet or DaemonSet whose `spec.updateStrategy.type` is `RollingUpdate`, and false otherwise.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/watch/ -run 'TestComplete|TestRolling' -v`
Expected: PASS, `TestCompleteAgreesWithKubectl` among the passing tests.

- [ ] **Step 6: Write the failing tests for the Tracker**

`internal/watch/tracker_test.go`. Each row tells a new Tracker the objects of `before` at t0, begins a deploy at t0, and tells it the events in order. `renamed` and `statefulSetBack` serve Task 26's tests as well.

```go
package watch

import (
	"math"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	"rollproof/internal/verdict"
)

// at is t0 plus s seconds.
func at(s float64) time.Time {
	return t0.Add(time.Duration(math.Round(s * float64(time.Second))))
}

// options are the Tracker's settings unless a test changes them.
func options() TrackerOptions {
	return TrackerOptions{Namespace: "orders", Selector: labels.Everything(),
		Settle: 15 * time.Second, Timeout: 300 * time.Second, Workers: 3}
}

// ev is one thing told to the Tracker: Set, Gone, or, with no object, CommandExited.
type ev struct {
	s    float64
	obj  runtime.Object
	gone bool
}

func set(s float64, obj runtime.Object) ev  { return ev{s: s, obj: obj} }
func gone(s float64, obj runtime.Object) ev { return ev{s: s, obj: obj, gone: true} }
func exited(s float64) ev                   { return ev{s: s} }

// base is the release before a deploy: Deployment orders, complete, with two
// ready pods of its ReplicaSet orders-a.
func base() []runtime.Object {
	return []runtime.Object{
		deployment("orders", 1, 1, 2, 2, 2, 2),
		replicaSet("orders-a", "orders"),
		pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, false),
		pod("orders-a-2", "a2", "ReplicaSet", "orders-a", true, false),
	}
}

// feed tells a new Tracker every object of before at t0, begins a deploy at
// t0, and then tells it the events in order.
func feed(o TrackerOptions, before []runtime.Object, events []ev) *Tracker {
	tr := NewTracker(o)
	for _, obj := range before {
		tr.Set(obj, t0)
	}
	tr.Begin(verdict.Deploy, t0)
	for _, e := range events {
		switch {
		case e.obj == nil:
			tr.CommandExited(at(e.s))
		case e.gone:
			tr.Gone(e.obj, at(e.s))
		default:
			tr.Set(e.obj, at(e.s))
		}
	}
	return tr
}

// renamed replaces Deployment orders with orders-v2 after base. The old
// ReplicaSet is gone before the old pods are.
func renamed() []ev {
	a1 := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, true)
	a2 := pod("orders-a-2", "a2", "ReplicaSet", "orders-a", true, true)
	return []ev{
		set(1, deployment("orders-v2", 1, 0, 2, 0, 0, 0)),
		set(1, replicaSet("orders-v2-a", "orders-v2")),
		exited(2),
		gone(3, deployment("orders", 1, 1, 2, 2, 2, 2)),
		set(4, a1),
		set(4, a2),
		set(5, pod("orders-v2-a-1", "c1", "ReplicaSet", "orders-v2-a", true, false)),
		set(5, pod("orders-v2-a-2", "c2", "ReplicaSet", "orders-v2-a", true, false)),
		set(9, deployment("orders-v2", 1, 1, 2, 2, 2, 2)),
		gone(11, replicaSet("orders-a", "orders")),
		gone(12, a1),
		gone(14, a2),
	}
}

// statefulSetBack replaces pod web-0 of StatefulSet web. The new web-0 has a
// new UID and is told before the old one is gone.
func statefulSetBack() ([]runtime.Object, []ev) {
	const rolling = appsv1.RollingUpdateStatefulSetStrategyType
	old := pod("web-0", "w0a", "StatefulSet", "web", true, true)
	before := []runtime.Object{
		statefulSet("web", 1, 1, 1, 1, 1, rolling),
		pod("web-0", "w0a", "StatefulSet", "web", true, false),
	}
	return before, []ev{
		set(1, statefulSet("web", 2, 1, 1, 1, 1, rolling)),
		exited(2),
		set(5, old),
		set(8, pod("web-0", "w0b", "StatefulSet", "web", true, false)),
		set(9, statefulSet("web", 2, 2, 1, 1, 1, rolling)),
		gone(10, old),
	}
}

func TestTransitionFinish(t *testing.T) {
	// none: Finished is zero, or there is no moment at which the window is still open.
	const none = -1.0
	inc := deployment("orders", 2, 1, 2, 2, 2, 2) // a new spec, not observed yet
	done := deployment("orders", 2, 2, 2, 2, 2, 2)
	oldA1 := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, true)
	restarted := func(ready bool) *corev1.Pod {
		p := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", ready, false)
		p.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "app", RestartCount: 1}}
		return p
	}
	stsBefore, stsEvents := statefulSetBack()
	for _, r := range []struct {
		name     string
		selector labels.Selector // nil: every workload
		before   []runtime.Object
		events   []ev
		finished float64
		stalled  bool
		open     float64 // State at this moment has not ended
		end      float64 // State at this moment has ended, and Ended is this moment
		restarts []string
	}{
		{name: "nothing changes and the command exits at +2 s",
			before: base(), events: []ev{exited(2)},
			finished: 2, open: 16, end: 17},
		{name: "the rollout completes at +20 s and the command exits at +30 s",
			before: base(), events: []ev{set(1, inc), set(20, done), exited(30)},
			finished: 30, open: 44, end: 45},
		{name: "an old pod is terminating until +26 s",
			before: base(), events: []ev{set(1, inc), exited(2), set(10, oldA1), set(20, done), gone(26, oldA1)},
			finished: 26, open: 40, end: 41},
		{name: "a pod created and deleted in the transition must be gone too",
			before: base(), events: []ev{set(1, inc), exited(2),
				set(4, replicaSet("orders-b", "orders")),
				set(5, pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false)),
				set(15, pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, true)),
				set(20, done),
				gone(23, pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, true))},
			finished: 23, open: 37, end: 38},
		{name: "a pod created in the settle time starts it again",
			before: base(), events: []ev{set(1, inc), exited(2), set(20, done),
				set(24, deployment("orders", 2, 2, 2, 2, 1, 2)),
				set(25, pod("orders-a-3", "a3", "ReplicaSet", "orders-a", false, false)),
				set(31, done)},
			finished: 31, open: 45, end: 46,
			restarts: []string{"pod orders-a-3 of deployment/orders was created 25.0 s after the transition began"}},
		{name: "a pod deleted in the settle time starts it again",
			before: base(), events: []ev{exited(2), set(5, oldA1), gone(7, oldA1)},
			finished: 7, open: 21, end: 22,
			restarts: []string{"pod orders-a-1 of deployment/orders was deleted 5.0 s after the transition began"}},
		{name: "a new spec in the settle time starts it again",
			before: base(), events: []ev{exited(2), set(5, inc), set(9, done)},
			finished: 9, open: 23, end: 24,
			restarts: []string{"the spec of deployment/orders changed 5.0 s after the transition began"}},
		{name: "readiness and a restarted container do not start it again",
			before: base(), events: []ev{exited(2), set(5, restarted(false)), set(8, restarted(true))},
			finished: 2, open: 16, end: 17},
		{name: "a workload that the selector leaves out counts for nothing",
			selector: labels.SelectorFromSet(labels.Set{"app": "orders"}),
			before: append(base(),
				deployment("redis", 2, 1, 1, 1, 1, 1),
				replicaSet("redis-a", "redis"),
				pod("redis-a-1", "r1", "ReplicaSet", "redis-a", true, true)),
			events:   []ev{exited(2), set(5, pod("redis-a-2", "r2", "ReplicaSet", "redis-a", false, false))},
			finished: 2, open: 16, end: 17},
		{name: "not finished at the timeout: a stall",
			before: base(), events: []ev{set(1, inc), exited(2)},
			finished: none, stalled: true, open: 299, end: 300},
		{name: "a change after the timeout, in the settle time, is a stall at that moment",
			before: base(), events: []ev{set(1, inc), exited(2), set(290, done),
				set(303, pod("orders-a-3", "a3", "ReplicaSet", "orders-a", false, false))},
			finished: none, stalled: true, open: none, end: 303,
			restarts: []string{"pod orders-a-3 of deployment/orders was created 303.0 s after the transition began"}},
		{name: "the settle time may run past the timeout",
			before: base(), events: []ev{set(1, inc), exited(2), set(290, done)},
			finished: 290, open: 304, end: 305},
		{name: "a StatefulSet's pod back under its name is a new pod",
			before: stsBefore, events: stsEvents,
			finished: 10, open: 24, end: 25},
		{name: "a workload deleted in the transition is complete when its pods are gone",
			before: base(), events: renamed(),
			finished: 14, open: 28, end: 29},
		{name: "a workload that updates with OnDelete does not hold the transition open",
			before: append(base(),
				daemonSet("agent", 2, 2, 3, 0, 3, appsv1.OnDeleteDaemonSetStrategyType),
				pod("agent-x", "x1", "DaemonSet", "agent", true, true)),
			events:   []ev{exited(2)},
			finished: 2, open: 16, end: 17},
	} {
		t.Run(r.name, func(t *testing.T) {
			o := options()
			if r.selector != nil {
				o.Selector = r.selector
			}
			tr := feed(o, r.before, r.events)
			if r.open != none {
				if s, ended := tr.State(at(r.open)); ended {
					t.Errorf("State(+%v s) has ended, at %v", r.open, s.Ended)
				}
			}
			s, ended := tr.State(at(r.end))
			if !ended {
				t.Fatalf("State(+%v s) has not ended", r.end)
			}
			want := Transition{Name: verdict.Deploy, Began: t0, Ended: at(r.end),
				Stalled: r.stalled, SettleRestarts: r.restarts}
			if r.finished != none {
				want.Finished = at(r.finished)
			}
			got := Transition{Name: s.Name, Began: s.Began, Finished: s.Finished, Ended: s.Ended,
				Stalled: s.Stalled, SettleRestarts: s.SettleRestarts}
			if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("transition (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBeginTwice(t *testing.T) {
	tr := feed(options(), base(), []ev{exited(2)}) // finished at +2 s, ends at +17 s
	func() {
		defer func() {
			if recover() == nil {
				t.Error("Begin at +10 s, while the deploy's window is open, did not panic")
			}
		}()
		tr.Begin(verdict.Rollback, at(10))
	}()
	tr = feed(options(), base(), []ev{exited(2)})
	tr.Begin(verdict.Rollback, at(20))
	if s, _ := tr.State(at(20)); s.Name != verdict.Rollback || !s.Began.Equal(at(20)) {
		t.Errorf("after the deploy's window ended, Begin gave %+v", s)
	}
}

func TestStateBeforeBegin(t *testing.T) {
	tr := NewTracker(options())
	for _, obj := range base() {
		tr.Set(obj, t0)
	}
	if s, ended := tr.State(at(1)); ended || !cmp.Equal(s, Transition{}) {
		t.Errorf("State before Begin = %+v, %v; want a zero Transition and false", s, ended)
	}
}

func TestWorkloadString(t *testing.T) {
	if got := (Workload{Kind: "deployment", Name: "orders"}).String(); got != "deployment/orders" {
		t.Errorf("String() = %q, want deployment/orders", got)
	}
}
```

- [ ] **Step 7: Run the tests to verify they fail**

Run: `go test ./internal/watch/ -v`
Expected: FAIL, build failed: `undefined: TrackerOptions`

- [ ] **Step 8: Implement the Tracker in `internal/watch/tracker.go`**

`type Workload struct { Kind, Name string }`, `func (w Workload) String() string`, `type TrackerOptions`, `func NewTracker(o TrackerOptions) *Tracker`, `func (t *Tracker) Set(obj runtime.Object, at time.Time)`, `func (t *Tracker) Gone(obj runtime.Object, at time.Time)`, `func (t *Tracker) Begin(name verdict.WindowName, at time.Time)`, `func (t *Tracker) CommandExited(at time.Time)`, `type Transition`, `type Count` and `func (t *Tracker) State(now time.Time) (Transition, bool)`, each with its interface comment from the Interfaces block. The package comment, the text of `module internal/watch` in the Interfaces block, goes at the top of this file. `Transition` and `Count` get all their fields now; this task fills Name, Began, Finished, Ended, Stalled and SettleRestarts, and Task 26 fills the rest. A nil `Selector` selects every workload. The Tracker takes no lock; its caller holds one (Task 27).

What the Tracker keeps:
1. For each workload, by Kind (`deployment`, `statefulset` or `daemonset`) and name: its last object, its generation, whether its labels matched `Selector` at its last Set, when it was first seen, and when it was gone (zero while it exists; a Set after Gone clears it).
2. For each ReplicaSet name: the Deployment that is its controller owner. The entry stays after the ReplicaSet is gone, so that its pods keep their workload.
3. For each pod, by UID and never by name: its name, its controller owner's kind and name, its last object, when it was first seen, when it was first ready (condition Ready True), when a deletion timestamp was first seen and whether that Set showed it ready, and when it was gone. The value of the deletion timestamp is never read.
4. A pod's workload: through its ReplicaSet's entry to a Deployment, or its StatefulSet or DaemonSet directly; none while its ReplicaSet is not known. Objects of any other type are ignored.

A transition:
5. `Begin(name, at)` first applies rule 8 at `at` to the transition before it, and panics when that one's window is still open.
6. The pods it may replace: those recorded when Begin is called whose first sight is at or before `at` and that were not gone by then. A pod first seen after `at` but told before the call is one the transition created.
7. A workload is watched in it when it existed at `at` or appeared later, and its labels matched at its last Set; a workload that a later Set shows matching joins. A watched workload takes part in the finish when `Rolling` holds for its last object; a pod takes part when its workload does.

On every Set, Gone and CommandExited at `at`, in this order:
8. If a transition is open, end its window when its time has come: when it is finished, at Finished + Settle; when it is not, at Began + Timeout, as a stall with Finished zero. A change at that moment or later belongs to no transition.
9. Update the records of rules 1 to 3. CommandExited notes its moment once; a second call, or one after the window has ended, changes nothing.
10. If the window is still open and the transition is finished, a change that starts the settle time again sets Finished to zero and appends its sentence to SettleRestarts. When `at` is at or after Began + Timeout, the transition has then stalled at `at`, and its window ends there. S is the time since Began in seconds, with one decimal:
    - a pod that takes part, seen for the first time: `pod NAME of KIND/NAME was created S s after the transition began`;
    - a pod that takes part, seen with a deletion timestamp for the first time, or Gone without one: `pod NAME of KIND/NAME was deleted S s after the transition began`;
    - a workload that takes part, Set with a generation higher than its last: `the spec of KIND/NAME changed S s after the transition began`.
    Nothing else starts it again: not readiness, not a restarted container, not a change of status.
11. If the window is still open and the transition is not finished, it is finished at `at` when all three hold: CommandExited was called; each workload that takes part exists and is Complete, or is gone and none of its pods is left; no pod that takes part has a deletion timestamp and is not gone.

`State(now)` applies rule 8 at `now` and returns the transition so far and whether its window has ended. Before any Begin it returns a zero Transition and false.

- [ ] **Step 9: Run the tests to verify they pass**

Run: `go test ./internal/watch/ -v`
Expected: PASS

Run: `go vet ./internal/watch/`
Expected: no output.

- [ ] **Step 10: Commit**

```bash
git add go.mod go.sum internal/watch
git commit -m "Track a transition: rollout rules, pods gone, the quiet settle time and a stall"
```

### Task 26: `watch`: pod counts, the quickest stop, a stall's details, and a ready install

**Files:**
- Create: `internal/watch/counts.go`
- Modify: `internal/watch/tracker.go`
- Test: `internal/watch/counts_test.go`

**Interfaces:**
- Consumes: `func NewTracker(o TrackerOptions) *Tracker`, `Set`, `Gone`, `Begin`, `CommandExited`, `func (t *Tracker) State(now time.Time) (Transition, bool)`, `type Transition`, `type Count`, `type Workload`, `func Complete`, `func Rolling` (Task 25); `type Condition` with TransitionStalled, PodUnschedulable, WorkloadUnsupported, NoPodReplaced, InstallFailed, NothingWatched (Task 12).
- Produces: `func (t *Tracker) State(now time.Time) (Transition, bool)`
  As in Task 25, and now the Transition also has its counts for every
  watched workload, whether it replaced pods, what is not complete at a
  stall, which pods could not be scheduled, and which workloads update in
  a way the tool cannot judge.
- Produces: `func (tr Transition) Conditions(commandSucceeded bool) []verdict.Condition`
  What the transition gives the verdict: a stall, with a second condition
  when a pod could not be scheduled; a workload the tool cannot judge; and,
  only when it finished and its command succeeded, that it replaced no pod.
- Produces: `type Install struct { Workloads []Count }`
  What the install left: every watched workload with its pods and the
  image digests they run.
- Produces: `func (t *Tracker) InstallState(began, exited, now time.Time) (Install, bool, error)`
  Whether the install is ready at now. exited is when its command exited,
  zero while it runs. The error is a Condition: nothing to watch one settle
  time after the command exited; a workload the tool cannot judge; or not
  ready at began plus the timeout.
- Produces: `type PodNote struct { Name string; Workload Workload; Old bool; For time.Duration }`
  A pod that was stopping or starting at some moment. Old says it existed
  when the transition that holds that moment began. For is how long it had
  been stopping, or starting, at that moment.
- Produces: `func (t *Tracker) PodsAt(at time.Time) (stopping, starting []PodNote)`
  The watched pods that, at the given moment, were deleted and not yet
  gone, and those that were created and not yet ready. The moment may lie
  in any transition so far.

- [ ] **Step 1: Write the failing tests for the counts and a stall's details**

`internal/watch/counts_test.go`, with the builders and scripts of Task 25's test files. `rollingUpdate` is the deploy that most rows use.

```go
package watch

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"rollproof/internal/verdict"
)

var (
	orders = Workload{Kind: "deployment", Name: "orders"}
	inc    = deployment("orders", 2, 1, 2, 2, 2, 2) // a new spec, not observed yet
)

// rollingUpdate is the deploy after base: ReplicaSet orders-b brings two new
// pods, and an old pod is deleted each time a new one is ready. It finishes
// at +12.1 s and its window ends at +27.1 s.
func rollingUpdate() []ev {
	a1 := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, true)
	a2 := pod("orders-a-2", "a2", "ReplicaSet", "orders-a", true, true)
	return []ev{
		set(0.2, inc),
		exited(0.5),
		set(2, replicaSet("orders-b", "orders")),
		set(2, pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false)),
		set(4, pod("orders-b-1", "b1", "ReplicaSet", "orders-b", true, false)),
		set(4, a1),
		set(5, pod("orders-b-2", "b2", "ReplicaSet", "orders-b", false, false)),
		set(7, pod("orders-b-2", "b2", "ReplicaSet", "orders-b", true, false)),
		set(7, a2),
		set(8, deployment("orders", 2, 2, 2, 2, 2, 2)),
		gone(9.3, a1),
		gone(12.1, a2),
	}
}

func TestCounts(t *testing.T) {
	stsBefore, stsEvents := statefulSetBack()
	for _, r := range []struct {
		name     string
		before   []runtime.Object
		events   []ev
		end      float64 // the window has ended by then
		replaced bool
		counts   []Count
	}{
		{"a rolling update of two pods", base(), rollingUpdate(), 27.1, true,
			[]Count{{Workload: orders, AtStart: 2, Replaced: 2, Created: 2, QuickestStop: 5100 * time.Millisecond}}},
		{"nothing changes", base(), []ev{exited(2)}, 17, false,
			[]Count{{Workload: orders, AtStart: 2}}},
		{"two workloads, only the second changes",
			append(base(), deployment("cache", 1, 1, 1, 1, 1, 1), replicaSet("cache-a", "cache"),
				pod("cache-a-1", "k1", "ReplicaSet", "cache-a", true, false)),
			rollingUpdate(), 27.1, true,
			[]Count{
				{Workload: Workload{Kind: "deployment", Name: "cache"}, AtStart: 1},
				{Workload: orders, AtStart: 2, Replaced: 2, Created: 2, QuickestStop: 5100 * time.Millisecond},
			}},
		{"a renamed Deployment", base(), renamed(), 29, true,
			[]Count{
				{Workload: orders, AtStart: 2, Replaced: 2, Removed: true, QuickestStop: 8 * time.Second},
				{Workload: Workload{Kind: "deployment", Name: "orders-v2"}, Created: 2, Added: true},
			}},
		{"a scale-up replaces nothing", base(), []ev{
			set(1, deployment("orders", 2, 1, 3, 2, 2, 2)),
			exited(1.5),
			set(2, pod("orders-a-3", "a3", "ReplicaSet", "orders-a", false, false)),
			set(4, pod("orders-a-3", "a3", "ReplicaSet", "orders-a", true, false)),
			set(4.5, deployment("orders", 2, 2, 3, 3, 3, 3)),
		}, 19.5, false,
			[]Count{{Workload: orders, AtStart: 2, Created: 1}}},
		{"a StatefulSet's pod back under its name", stsBefore, stsEvents, 25, true,
			[]Count{{Workload: Workload{Kind: "statefulset", Name: "web"},
				AtStart: 1, Replaced: 1, Created: 1, QuickestStop: 5 * time.Second}}},
	} {
		t.Run(r.name, func(t *testing.T) {
			s, ended := feed(options(), r.before, r.events).State(at(r.end))
			if !ended || s.Stalled {
				t.Fatalf("State(+%v s): ended %v, stalled %v; want ended and not stalled", r.end, ended, s.Stalled)
			}
			if s.Replaced != r.replaced {
				t.Errorf("Replaced = %v, want %v", s.Replaced, r.replaced)
			}
			if diff := cmp.Diff(r.counts, s.Workloads, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("counts (-want +got):\n%s", diff)
			}
		})
	}
}

// A pod first seen after the moment the transition began is created by it,
// even when Begin is told later.
func TestBeginToldLate(t *testing.T) {
	tr := NewTracker(options())
	for _, obj := range base() {
		tr.Set(obj, t0)
	}
	tr.Set(replicaSet("orders-b", "orders"), at(0.5))
	tr.Set(pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false), at(0.5))
	tr.Begin(verdict.Deploy, t0)
	s, _ := tr.State(at(1))
	want := []Count{{Workload: orders, AtStart: 2, Created: 1}}
	if diff := cmp.Diff(want, s.Workloads, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("counts (-want +got):\n%s", diff)
	}
}

func TestQuickestStop(t *testing.T) {
	a1 := pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, true)
	a2 := pod("orders-a-2", "a2", "ReplicaSet", "orders-a", true, true)
	neverReady := pod("orders-b-3", "b3", "ReplicaSet", "orders-b", false, true)
	events := []ev{
		set(0.2, inc),
		exited(0.5),
		set(2, replicaSet("orders-b", "orders")),
		set(2, pod("orders-b-1", "b1", "ReplicaSet", "orders-b", true, false)),
		set(3, pod("orders-b-2", "b2", "ReplicaSet", "orders-b", true, false)),
		set(5, pod("orders-b-3", "b3", "ReplicaSet", "orders-b", false, false)),
		set(6, neverReady),
		gone(6.1, neverReady),
		set(10, a1),
		set(12, a2),
		set(13, deployment("orders", 2, 2, 2, 2, 2, 2)),
		gone(15.3, a1),
		gone(17.2, a2),
	}
	s, _ := feed(options(), base(), events).State(at(32.2))
	if got := s.Workloads[0].QuickestStop; got != 5200*time.Millisecond {
		t.Errorf("QuickestStop = %v, want 5.2s: the pod that was never ready does not count", got)
	}
}

func TestDigests(t *testing.T) {
	withImages := func(p *corev1.Pod, imageIDs ...string) *corev1.Pod {
		for i, id := range imageIDs {
			p.Status.ContainerStatuses = append(p.Status.ContainerStatuses,
				corev1.ContainerStatus{Name: []string{"app", "sidecar"}[i], ImageID: id})
		}
		return p
	}
	before := base()
	withImages(before[2].(*corev1.Pod), "rollproof.test/orders@sha256:old")
	events := []ev{
		set(0.2, inc),
		exited(0.5),
		set(1, replicaSet("orders-b", "orders")),
		set(2, withImages(pod("orders-b-1", "b1", "ReplicaSet", "orders-b", true, false),
			"rollproof.test/orders@sha256:aaa", "docker.io/library/x@sha256:bbb")),
		set(3, withImages(pod("orders-b-2", "b2", "ReplicaSet", "orders-b", true, false),
			"rollproof.test/orders@sha256:aaa")),
	}
	s, _ := feed(options(), before, events).State(at(4))
	if diff := cmp.Diff([]string{"sha256:aaa", "sha256:bbb"}, s.Workloads[0].Digests); diff != "" {
		t.Errorf("digests of the created pods (-want +got):\n%s", diff)
	}
}

func TestNoPodReplaced(t *testing.T) {
	noPod := []verdict.Condition{{Code: verdict.NoPodReplaced,
		Text: "the transition finished and replaced no pod: it changed nothing the test can judge"}}
	s, _ := feed(options(), base(), []ev{exited(2)}).State(at(17))
	if diff := cmp.Diff(noPod, s.Conditions(true)); diff != "" {
		t.Errorf("Conditions(true) of a transition that changed nothing (-want +got):\n%s", diff)
	}
	if got := s.Conditions(false); len(got) != 0 {
		t.Errorf("Conditions(false) = %v; a failed command is judged as what it is", got)
	}
	s, _ = feed(options(), base(), rollingUpdate()).State(at(27.1))
	if got := s.Conditions(true); len(got) != 0 {
		t.Errorf("Conditions(true) of a rolling update = %v, want none", got)
	}
}

func TestStallDetails(t *testing.T) {
	const scheduler = "0/4 nodes are available: 1 node(s) had untolerated taint {node-role.kubernetes.io/control-plane: }, 3 node(s) didn't match pod anti-affinity rules."
	const notUpdated = `deployment/orders: Waiting for deployment "orders" rollout to finish: 1 out of 2 new replicas have been updated...`
	// The Transition holds no worker count, so the sentence carries it (Workers is 3).
	const noRoom = "pod orders-b-1 could not be scheduled on the test cluster's 3 worker nodes (cluster.workers): " + scheduler
	pending := pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false)
	pending.Status.Phase = corev1.PodPending
	pending.Status.Conditions = append(pending.Status.Conditions, corev1.PodCondition{Type: corev1.PodScheduled,
		Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable, Message: scheduler})
	crashing := pod("orders-b-1", "b1", "ReplicaSet", "orders-b", false, false)
	crashing.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "app", RestartCount: 4,
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
		LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
			Reason: "Error", ExitCode: 1}}}}
	for _, r := range []struct {
		name          string
		events        []ev
		end           float64
		incomplete    []string
		unschedulable []string
		conditions    []verdict.Condition
	}{
		{name: "a pod that cannot be scheduled",
			events: []ev{set(0.2, inc), exited(0.5), set(2, replicaSet("orders-b", "orders")),
				set(2, pending), set(2.5, deployment("orders", 2, 2, 2, 1, 2, 3))},
			end: 300,
			incomplete: []string{notUpdated,
				"pod orders-b-1: phase Pending, not scheduled (Unschedulable: " + scheduler + ")"},
			unschedulable: []string{noRoom},
			conditions: []verdict.Condition{
				{Code: verdict.TransitionStalled, Text: "did not finish within limits.timeout"},
				{Code: verdict.PodUnschedulable, Text: noRoom + "; a rolling update needs room for one more pod"},
			}},
		{name: "a pod in CrashLoopBackOff",
			events: []ev{set(0.2, inc), exited(0.5), set(2, replicaSet("orders-b", "orders")),
				set(2, crashing), set(3, deployment("orders", 2, 2, 2, 1, 1, 3))},
			end: 300,
			incomplete: []string{notUpdated,
				"pod orders-b-1: phase Running, container app waiting (CrashLoopBackOff) and last ended (Error, exit code 1)"},
			conditions: []verdict.Condition{
				{Code: verdict.TransitionStalled, Text: "did not finish within limits.timeout"},
			}},
		{name: "the settle time started again after the timeout",
			events: []ev{set(1, inc), exited(2), set(290, deployment("orders", 2, 2, 2, 2, 2, 2)),
				set(303, pod("orders-a-3", "a3", "ReplicaSet", "orders-a", false, false))},
			end: 303,
			conditions: []verdict.Condition{
				{Code: verdict.TransitionStalled, Text: "did not finish within limits.timeout; the settle time started again: " +
					"pod orders-a-3 of deployment/orders was created 303.0 s after the transition began"},
			}},
	} {
		t.Run(r.name, func(t *testing.T) {
			s, ended := feed(options(), base(), r.events).State(at(r.end))
			if !ended || !s.Stalled {
				t.Fatalf("State(+%v s): ended %v, stalled %v; want a stall", r.end, ended, s.Stalled)
			}
			if diff := cmp.Diff(r.incomplete, s.Incomplete, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Incomplete (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(r.unschedulable, s.Unschedulable, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Unschedulable (-want +got):\n%s", diff)
			}
			// A stall never gives no-pod-replaced, whether its command succeeded or not.
			for _, ok := range []bool{true, false} {
				if diff := cmp.Diff(r.conditions, s.Conditions(ok)); diff != "" {
					t.Errorf("Conditions(%v) (-want +got):\n%s", ok, diff)
				}
			}
		})
	}
}

func TestUnsupported(t *testing.T) {
	web := statefulSet("web", 1, 0, 1, 0, 0, appsv1.OnDeleteStatefulSetStrategyType)
	events := append([]ev{set(0.1, web)}, rollingUpdate()...)
	s, ended := feed(options(), base(), events).State(at(27.1))
	if !ended || s.Stalled || !s.Finished.Equal(at(12.1)) {
		t.Fatalf("ended %v, stalled %v, finished %v; the OnDelete workload must not hold the transition open",
			ended, s.Stalled, s.Finished)
	}
	if diff := cmp.Diff([]Workload{{Kind: "statefulset", Name: "web"}}, s.Unsupported); diff != "" {
		t.Errorf("Unsupported (-want +got):\n%s", diff)
	}
	want := []verdict.Condition{{Code: verdict.WorkloadUnsupported,
		Text: "statefulset/web updates with OnDelete; the tool judges only a workload that updates by rolling"}}
	if diff := cmp.Diff(want, s.Conditions(true)); diff != "" {
		t.Errorf("Conditions (-want +got):\n%s", diff)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/watch/ -v`
Expected: FAIL, build failed: `s.Conditions undefined (type Transition has no field or method Conditions)`

- [ ] **Step 3: Count the pods and describe a stall, in `internal/watch/counts.go`**

`func (tr Transition) Conditions(commandSucceeded bool) []verdict.Condition`, with its interface comment from the Interfaces block. In `tracker.go`, `State` now fills Workloads, Replaced and Unsupported, and, for a stall, Incomplete and Unschedulable; the functions that compute them live in `counts.go`. "Watched", "takes part" and "the pods it may replace" are as rules 6 and 7 of Task 25 define them.

Counts, at a bound B: the window's end when it has ended, else `now`. One Count for each workload watched in the transition, sorted by Kind and then Name:
1. AtStart: the pods it may replace that are of this workload. Replaced: those of them gone at or before B.
2. Created: its other pods first seen at or after Began and at or before B.
3. Added: it did not exist at Began. Removed: it existed at Began and was gone at or before B.
4. QuickestStop: over its pods whose deletion timestamp was first seen at or after Began, by a Set that showed them ready, and that were gone at or before B, the shortest time from that first sight to gone. Zero when there is none.
5. Digests: of its created pods, every `status.containerStatuses[].imageID` that any of their Sets showed, from `sha256:` on; an ID without `sha256:` is skipped; sorted, no repeats.
6. Probed stays empty; Task 27 sets it.
7. Transition.Replaced: some pod counted in a Replaced, and some pod counted in a Created, over all watched workloads.

A stall's details are taken once, when it stalls, from the records as they are then (for a stall of rule 10 of Task 25, after the change that caused it):
8. Incomplete, for each workload that takes part, in the order of the counts: when it exists and is not Complete, `KIND/NAME: ` and Complete's message, then one sentence for each of its pods that is not gone and not ready, by pod name; when it is gone and N of its pods are left, `KIND/NAME was deleted and N of its pods are not gone`. Then, for each pod that takes part and has a deletion timestamp and is not gone, by name: `pod NAME has been stopping for S s and is not gone`, S being the time from the first sight of its deletion to the moment the details are taken, with one decimal.
9. A pod's sentence: `pod NAME: phase PHASE`; then `, not scheduled (REASON: MESSAGE)` when its PodScheduled condition is False; then, for each container status in order, `, container NAME ` and its state: `waiting (REASON)`, `running` or `terminated (REASON, exit code N)`, and, when it has a last termination, ` and last ended (REASON, exit code N)`.
10. Unschedulable: for each pod that takes part, is not gone, and has PodScheduled False with reason `Unschedulable`, by name: `pod NAME could not be scheduled on the test cluster's N worker nodes (cluster.workers): MESSAGE`, N being TrackerOptions.Workers. The Transition holds no worker count, so the sentence carries it.
11. Unsupported: each workload watched at some moment of the transition whose object at that moment was not Rolling, sorted.

Conditions, in this order, each with At zero:
12. Stalled: TransitionStalled, with the text `did not finish within limits.timeout`, then, when SettleRestarts is not empty, `; the settle time started again: ` and its sentences joined by `; `. The Incomplete sentences are not part of this text: they stay in Transition.Incomplete, and Task 32 carries them as the reason's detail.
13. Stalled and Unschedulable not empty: PodUnschedulable, with the Unschedulable sentences joined by `; `, then `; a rolling update needs room for one more pod`.
14. Unsupported not empty: WorkloadUnsupported, with `KIND/NAME updates with TYPE` for each (TYPE being its `spec.updateStrategy.type`) joined by `; `, then `; the tool judges only a workload that updates by rolling`.
15. Finished not zero, not Stalled, commandSucceeded, and Replaced false: NoPodReplaced, with `the transition finished and replaced no pod: it changed nothing the test can judge`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/watch/ -v`
Expected: PASS

- [ ] **Step 5: Write the failing tests for a ready install and for the pods at a moment**

Add to the imports of `internal/watch/counts_test.go` the packages `errors` and `k8s.io/apimachinery/pkg/labels`, and add these tests at its end:

```go
func TestInstallState(t *testing.T) {
	const running = -1.0 // the install command has not exited
	installed := base()
	for _, obj := range installed[2:] {
		p := obj.(*corev1.Pod)
		p.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "app", ImageID: "rollproof.test/orders@sha256:old"}}
	}
	for _, r := range []struct {
		name        string
		selector    labels.Selector // nil: every workload
		objs        []runtime.Object
		exited, now float64
		ready       bool
		install     Install
		code        verdict.Code // empty: no error
		text        string
	}{
		{name: "ready", objs: installed, exited: 3, now: 3, ready: true,
			install: Install{Workloads: []Count{{Workload: orders, Digests: []string{"sha256:old"}}}}},
		{name: "the command still runs", objs: installed, exited: running, now: 3},
		{name: "nothing watched, one second before the settle time has passed",
			selector: labels.SelectorFromSet(labels.Set{"app": "order"}), objs: installed, exited: 2, now: 16},
		{name: "nothing watched once the settle time has passed",
			selector: labels.SelectorFromSet(labels.Set{"app": "order"}), objs: installed, exited: 2, now: 17,
			code: verdict.NothingWatched,
			text: "nothing to watch\n" +
				"15 s after the install command returned, namespace orders held no Deployment, StatefulSet or DaemonSet that matches the selector app=order"},
		{name: "nothing watched, with no selector", exited: 2, now: 17,
			code: verdict.NothingWatched,
			text: "nothing to watch\n" +
				"15 s after the install command returned, namespace orders held no Deployment, StatefulSet or DaemonSet"},
		{name: "a DaemonSet that updates with OnDelete",
			objs:   append(base(), daemonSet("agent", 1, 1, 3, 3, 3, appsv1.OnDeleteDaemonSetStrategyType)),
			exited: 2, now: 2,
			code: verdict.WorkloadUnsupported,
			text: "daemonset/agent updates with OnDelete; the tool judges only a workload that updates by rolling"},
		{name: "not ready one second before the timeout",
			objs: []runtime.Object{deployment("orders", 1, 0, 2, 0, 0, 0)}, exited: 2, now: 299},
		{name: "not ready at the timeout",
			objs: []runtime.Object{deployment("orders", 1, 0, 2, 0, 0, 0)}, exited: 2, now: 300,
			code: verdict.InstallFailed,
			text: "the install was not ready within limits.timeout\n" +
				"deployment/orders: Waiting for deployment spec update to be observed..."},
	} {
		t.Run(r.name, func(t *testing.T) {
			o := options()
			if r.selector != nil {
				o.Selector = r.selector
			}
			tr := NewTracker(o)
			for _, obj := range r.objs {
				tr.Set(obj, at(1))
			}
			var exitedAt time.Time
			if r.exited != running {
				exitedAt = at(r.exited)
			}
			install, ready, err := tr.InstallState(t0, exitedAt, at(r.now))
			if ready != r.ready {
				t.Errorf("ready = %v, want %v", ready, r.ready)
			}
			if diff := cmp.Diff(r.install, install, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("install (-want +got):\n%s", diff)
			}
			var c verdict.Condition
			switch {
			case r.code == "" && err != nil:
				t.Errorf("err = %v, want none", err)
			case r.code != "" && (!errors.As(err, &c) || c.Code != r.code || c.Text != r.text):
				t.Errorf("err = %v, want %s: %s", err, r.code, r.text)
			}
		})
	}
}

func TestPodsAt(t *testing.T) {
	tr := feed(options(), base(), rollingUpdate())
	stopping, starting := tr.PodsAt(at(4.3))
	want := []PodNote{{Name: "orders-a-1", Workload: orders, Old: true, For: 300 * time.Millisecond}}
	if diff := cmp.Diff(want, stopping); diff != "" {
		t.Errorf("stopping at +4.3 s (-want +got):\n%s", diff)
	}
	if len(starting) != 0 {
		t.Errorf("starting at +4.3 s = %v, want none", starting)
	}
	stopping, starting = tr.PodsAt(at(5.5))
	want = []PodNote{{Name: "orders-a-1", Workload: orders, Old: true, For: 1500 * time.Millisecond}}
	if diff := cmp.Diff(want, stopping); diff != "" {
		t.Errorf("stopping at +5.5 s (-want +got):\n%s", diff)
	}
	want = []PodNote{{Name: "orders-b-2", Workload: orders, For: 500 * time.Millisecond}}
	if diff := cmp.Diff(want, starting); diff != "" {
		t.Errorf("starting at +5.5 s (-want +got):\n%s", diff)
	}
}

// A moment of the deploy, asked during the rollback, is judged against the
// deploy: a pod that the deploy created is not old then, although it is an
// old pod of the rollback.
func TestPodsAtAnEarlierTransition(t *testing.T) {
	tr := feed(options(), base(), rollingUpdate()) // the deploy's window ends at +27.1 s
	tr.Begin(verdict.Rollback, at(30))
	tr.Set(pod("orders-b-1", "b1", "ReplicaSet", "orders-b", true, true), at(32))
	stopping, starting := tr.PodsAt(at(5.5))
	want := []PodNote{{Name: "orders-a-1", Workload: orders, Old: true, For: 1500 * time.Millisecond}}
	if diff := cmp.Diff(want, stopping); diff != "" {
		t.Errorf("stopping at +5.5 s (-want +got):\n%s", diff)
	}
	want = []PodNote{{Name: "orders-b-2", Workload: orders, For: 500 * time.Millisecond}}
	if diff := cmp.Diff(want, starting); diff != "" {
		t.Errorf("starting at +5.5 s, a moment of the deploy (-want +got):\n%s", diff)
	}
	stopping, starting = tr.PodsAt(at(32.4))
	want = []PodNote{{Name: "orders-b-1", Workload: orders, Old: true, For: 400 * time.Millisecond}}
	if diff := cmp.Diff(want, stopping); diff != "" {
		t.Errorf("stopping at +32.4 s, a moment of the rollback (-want +got):\n%s", diff)
	}
	if len(starting) != 0 {
		t.Errorf("starting at +32.4 s = %v, want none", starting)
	}
}
```

- [ ] **Step 6: Run the tests to verify they fail**

Run: `go test ./internal/watch/ -v`
Expected: FAIL, build failed: `undefined: Install`

- [ ] **Step 7: Implement `InstallState` and `PodsAt` in `internal/watch/counts.go`**

`type Install`, `func (t *Tracker) InstallState(began, exited, now time.Time) (Install, bool, error)`, `type PodNote` and `func (t *Tracker) PodsAt(at time.Time) (stopping, starting []PodNote)`, each with its interface comment from the Interfaces block. The error is a `verdict.Condition`.

`InstallState` watches the workloads that exist at `now` and whose labels match; those that are Rolling take part. In this order:
1. `exited` is not zero and a watched workload is not Rolling: WorkloadUnsupported, with the text of Step 3's rule 14 for those workloads.
2. `exited` is not zero, at least one workload is watched, every one that takes part is Complete, and no pod that takes part has a deletion timestamp and is not gone: ready. The Install has one Count for each watched workload, sorted by Kind and then Name, with Workload and Digests only; the digests are those of Step 3's rule 5, over all its pods that are not gone.
3. `exited` is not zero, no workload is watched, and `now` is at or after `exited` + Settle: NothingWatched. Its text has two lines: `nothing to watch`, a line break (`\n`), and the sentence `S s after the install command returned, namespace NAMESPACE held no Deployment, StatefulSet or DaemonSet`, followed, unless `Selector.Empty()`, by ` that matches the selector ` and `Selector.String()`. S is Settle in seconds as `strconv.FormatFloat(s, 'f', -1, 64)` writes it.
4. `now` is at or after `began` + Timeout: InstallFailed. Its text's first line is `the install was not ready within limits.timeout`, and each further line is one sentence of Step 3's rule 8, over the install's workloads that take part. (A condition's first line is its short form; the run's record makes each further line a line of detail.)
5. Otherwise not ready, and no error.

`PodsAt(at)` answers for a moment of any transition so far, because a failure of the deploy's window can arrive during the rollback. In `tracker.go`, the Tracker therefore keeps every transition it has seen, in a list: its name, when it began, when its window ended (zero while open) and its watched workloads; the transition of Task 25's rules is the last of the list. Each pod's four moments (first seen, first ready, deletion first seen, gone) are already in its record.
1. The transition that holds `at` is the one whose window holds it: Began at or before `at`, and `at` before its end, or no end yet. When none holds it (before the first Begin, or between two windows), PodsAt returns nil and nil.
2. Only pods of the workloads watched in that transition count. A pod is old for `at` when it existed when that transition began: first seen at or before its Began, and not gone by then.
3. Stopping: a deletion timestamp first seen at or before `at`, and not gone by `at`. For is the time from that first sight to `at`; Old as rule 2 says.
4. Starting: not old, first seen at or before `at`, not first ready by `at`, no deletion seen by `at`, not gone by `at`. For is the time from its first sight to `at`; Old is false.
5. Both lists are sorted by name.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/watch/ -v`
Expected: PASS

Run: `go vet ./internal/watch/`
Expected: no output.

- [ ] **Step 9: Commit**

```bash
git add internal/watch
git commit -m "Count replaced pods, time the quickest stop, and describe a stall and a ready install"
```

### Task 27: `watch`: the Watcher on a real API, and the workloads behind a Service

**Files:**
- Create: `internal/watch/watcher.go`, `internal/watch/service.go`
- Test: `internal/watch/watcher_test.go`, `internal/watch/watcher_cluster_test.go` (build tag `cluster`)

**Interfaces:**
- Consumes: `func NewTracker(o TrackerOptions) *Tracker` and every Tracker operation (Tasks 25 and 26); `type Transition`, `type Install`, `type PodNote`, `type Count` (Tasks 25 and 26); `func Create(ctx context.Context, s Spec, e Engine) (*Cluster, error)` in the cluster test (Task 21).
- Produces: `type Options struct { Namespace, Selector string; Settle, Timeout time.Duration; Workers int; Service *types.NamespacedName; Now func() time.Time }`
  What a Watcher needs. Selector is a label selector as text, empty for
  every workload. Service is the Service that the built-in probe's URL
  names, nil when it names none or the probe is a custom one.
- Produces: `func Start(ctx context.Context, client kubernetes.Interface, o Options) (*Watcher, error)`
  Begins to follow the namespace's workloads, ReplicaSets and Pods and
  returns when it has seen what exists. It follows until Stop.
- Produces: `func (w *Watcher) InstallReady(ctx context.Context, began time.Time, exited <-chan struct{}) (Install, error)`
  Returns when the install is ready. exited is closed when the install's
  command has exited. The error is a Condition as InstallState gives it,
  or ctx's error.
- Produces: `func (w *Watcher) Transition(ctx context.Context, name verdict.WindowName, began time.Time, exited <-chan struct{}) Transition`
  Follows one transition from began and returns when its window has
  ended. exited is closed when the transition's command has exited. When
  ctx ends first it returns what it has, with Ended set to that moment.
- Produces: `func (w *Watcher) PodsAt(at time.Time) (stopping, starting []PodNote)`
  As the Tracker's PodsAt.
- Produces: `func (w *Watcher) Stop()`
  Stops following. Safe to call twice.

- [ ] **Step 1: Write the failing tests without a cluster**

`internal/watch/watcher_test.go`. The fake client's watches do not replay what was created between an informer's list and its watch, so `startWatcher` waits until all five watches are served before a test changes anything. The tests read the Tracker through the Watcher's unexported fields `mu` and `tr`, and `TestStopTwice` reads `done`.

```go
package watch

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8swatch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"rollproof/internal/verdict"
)

// fakeClock is the Watcher's clock in these tests; only the test moves it.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// startWatcher starts a Watcher on namespace orders of a fake client that
// holds objs, with settle 15 s and timeout 300 s, and stops it when the test
// ends. It returns once the fake client serves all five watches, so that no
// change the test makes falls between an informer's list and its watch.
func startWatcher(t *testing.T, service *types.NamespacedName, objs ...runtime.Object) (*Watcher, *fake.Clientset, *fakeClock) {
	t.Helper()
	client := fake.NewClientset(objs...)
	var watches atomic.Int32
	client.PrependWatchReactor("*", func(k8stesting.Action) (bool, k8swatch.Interface, error) {
		watches.Add(1)
		return false, nil, nil
	})
	clock := &fakeClock{now: t0}
	w, err := Start(t.Context(), client, Options{Namespace: "orders", Settle: 15 * time.Second,
		Timeout: 300 * time.Second, Workers: 3, Service: service, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Stop)
	for deadline := time.Now().Add(5 * time.Second); watches.Load() < 5; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the informers did not start their watches")
		}
	}
	return w, client, clock
}

// drive moves the fake clock on by one second every 10 ms of real time until
// res delivers, and fails the test after 10 s of real time.
func drive[T any](t *testing.T, c *fakeClock, res <-chan T) T {
	t.Helper()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case v := <-res:
			return v
		case <-tick.C:
			c.Advance(time.Second)
		case <-deadline:
			t.Fatal("no result within 10 s of real time")
		}
	}
}

// until waits, for at most 5 s of real time, until cond holds for the
// transition so far, as the Watcher's Tracker gives it at the fake clock's time.
func until(t *testing.T, w *Watcher, c *fakeClock, cond func(Transition) bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		w.mu.Lock()
		s, _ := w.tr.State(c.Now())
		w.mu.Unlock()
		if cond(s) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("not reached within 5 s; the transition so far: %+v", s)
		}
	}
}

func closed() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}

// update stores a Deployment or a Pod in the fake client, whole, status included.
func update(t *testing.T, client *fake.Clientset, obj runtime.Object) {
	t.Helper()
	var err error
	switch o := obj.(type) {
	case *appsv1.Deployment:
		_, err = client.AppsV1().Deployments("orders").Update(t.Context(), o, metav1.UpdateOptions{})
	case *corev1.Pod:
		_, err = client.CoreV1().Pods("orders").Update(t.Context(), o, metav1.UpdateOptions{})
	default:
		t.Fatalf("update: %T", obj)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestStartSeesWhatExists(t *testing.T) {
	w, _, clock := startWatcher(t, nil, base()...)
	w.mu.Lock()
	install, ready, err := w.tr.InstallState(t0, t0, clock.Now())
	w.mu.Unlock()
	if err != nil || !ready || len(install.Workloads) != 1 || install.Workloads[0].Workload != orders {
		t.Fatalf("right after Start: %+v, ready %v, err %v; want deployment/orders, ready", install, ready, err)
	}
}

func TestStartRejectsASelector(t *testing.T) {
	_, err := Start(t.Context(), fake.NewClientset(), Options{Namespace: "orders", Selector: "app in (",
		Settle: 15 * time.Second, Timeout: 300 * time.Second, Workers: 3, Now: time.Now})
	var c verdict.Condition
	if err == nil || errors.As(err, &c) {
		t.Fatalf("err = %v; want a plain error", err)
	}
}

func TestInstallReady(t *testing.T) {
	t.Run("ready once the Deployment is complete", func(t *testing.T) {
		objs := base()
		objs[0] = deployment("orders", 1, 0, 2, 0, 0, 0)
		w, client, _ := startWatcher(t, nil, objs...)
		res := make(chan error, 1)
		go func() {
			_, err := w.InstallReady(t.Context(), t0, closed())
			res <- err
		}()
		select {
		case err := <-res:
			t.Fatalf("InstallReady returned (%v) before the Deployment was complete", err)
		case <-time.After(300 * time.Millisecond):
		}
		update(t, client, deployment("orders", 1, 1, 2, 2, 2, 2))
		select {
		case err := <-res:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("InstallReady did not return once the Deployment was complete")
		}
	})
	t.Run("nothing to watch", func(t *testing.T) {
		w, _, clock := startWatcher(t, nil)
		res := make(chan error, 1)
		go func() {
			_, err := w.InstallReady(t.Context(), t0, closed())
			res <- err
		}()
		err := drive(t, clock, res)
		var c verdict.Condition
		if !errors.As(err, &c) || c.Code != verdict.NothingWatched {
			t.Fatalf("err = %v, want nothing-watched", err)
		}
		if got := clock.Now().Sub(t0); got < 15*time.Second {
			t.Errorf("nothing-watched after %v of the fake clock, before the settle time", got)
		}
	})
}

func TestTransition(t *testing.T) {
	w, client, clock := startWatcher(t, nil, base()...)
	ctx := t.Context()
	exited := make(chan struct{})
	res := make(chan Transition, 1)
	go func() { res <- w.Transition(ctx, verdict.Deploy, clock.Now(), exited) }()
	until(t, w, clock, func(s Transition) bool { return s.Name == verdict.Deploy })
	clock.Advance(time.Second)

	update(t, client, deployment("orders", 2, 1, 2, 2, 2, 2))
	if _, err := client.AppsV1().ReplicaSets("orders").Create(ctx, replicaSet("orders-b", "orders"), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []*corev1.Pod{
		pod("orders-b-1", "b1", "ReplicaSet", "orders-b", true, false),
		pod("orders-b-2", "b2", "ReplicaSet", "orders-b", true, false),
	} {
		if _, err := client.CoreV1().Pods("orders").Create(ctx, p, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, old := range []*corev1.Pod{
		pod("orders-a-1", "a1", "ReplicaSet", "orders-a", true, true),
		pod("orders-a-2", "a2", "ReplicaSet", "orders-a", true, true),
	} {
		update(t, client, old)
		if err := client.CoreV1().Pods("orders").Delete(ctx, old.Name, metav1.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	update(t, client, deployment("orders", 2, 2, 2, 2, 2, 2))
	until(t, w, clock, func(s Transition) bool {
		return len(s.Workloads) == 1 && s.Workloads[0].Replaced == 2 && s.Workloads[0].Created == 2
	})
	close(exited)

	tr := drive(t, clock, res)
	if tr.Stalled || !tr.Replaced {
		t.Fatalf("stalled %v, replaced %v; want a finished transition that replaced pods", tr.Stalled, tr.Replaced)
	}
	if got := tr.Ended.Sub(tr.Finished); got != 15*time.Second {
		t.Errorf("the window ended %v after the transition finished, want the settle time", got)
	}
	want := []Count{{Workload: orders, AtStart: 2, Replaced: 2, Created: 2, Probed: "not known"}}
	if diff := cmp.Diff(want, tr.Workloads, cmpopts.IgnoreFields(Count{}, "QuickestStop"), cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("counts (-want +got):\n%s", diff)
	}
}

func TestTransitionCancelled(t *testing.T) {
	w, _, clock := startWatcher(t, nil, base()...)
	ctx, cancel := context.WithCancel(t.Context())
	res := make(chan Transition, 1)
	go func() { res <- w.Transition(ctx, verdict.Deploy, clock.Now(), make(chan struct{})) }()
	until(t, w, clock, func(s Transition) bool { return s.Name == verdict.Deploy })
	clock.Advance(3 * time.Second)
	cancel()
	select {
	case tr := <-res:
		if !tr.Ended.Equal(at(3)) || tr.Stalled || !tr.Finished.IsZero() {
			t.Errorf("ended %v, stalled %v, finished %v; want ended at +3 s, not stalled, not finished",
				tr.Ended, tr.Stalled, tr.Finished)
		}
	case <-time.After(time.Second):
		t.Fatal("Transition did not return within 1 s of the cancel")
	}
}

func TestProbed(t *testing.T) {
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-x7k2p", Namespace: "orders",
			Labels: map[string]string{discoveryv1.LabelServiceName: "orders"}},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{
			{Addresses: []string{"10.244.1.5"},
				TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "orders", Name: "orders-a-1", UID: "a1"}},
			{Addresses: []string{"10.244.2.7"},
				TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "orders", Name: "orders-a-2", UID: "a2"}},
		},
	}
	objs := append(base(), slice,
		deployment("batch", 1, 1, 1, 1, 1, 1),
		replicaSet("batch-a", "batch"),
		pod("batch-a-1", "k1", "ReplicaSet", "batch-a", true, false))
	for _, r := range []struct {
		name          string
		service       *types.NamespacedName
		batch, orders string
	}{
		{"the Service's endpoints are pods of orders", &types.NamespacedName{Namespace: "orders", Name: "orders"}, "no", "yes"},
		{"no Service", nil, "not known", "not known"},
		{"a Service in another namespace", &types.NamespacedName{Namespace: "shop", Name: "orders"}, "no", "no"},
	} {
		t.Run(r.name, func(t *testing.T) {
			w, _, clock := startWatcher(t, r.service, objs...)
			res := make(chan Transition, 1)
			go func() { res <- w.Transition(t.Context(), verdict.Deploy, clock.Now(), closed()) }()
			got := map[string]string{}
			for _, c := range drive(t, clock, res).Workloads {
				got[c.Workload.Name] = c.Probed
			}
			if diff := cmp.Diff(map[string]string{"batch": r.batch, "orders": r.orders}, got); diff != "" {
				t.Errorf("Probed (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStopTwice(t *testing.T) {
	w, _, _ := startWatcher(t, nil, base()...)
	w.Stop()
	w.Stop()
	select {
	case <-w.done:
	case <-time.After(5 * time.Second):
		t.Fatal("a goroutine of the Watcher still runs after Stop")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/watch/ -v`
Expected: FAIL, build failed: `undefined: Watcher`

- [ ] **Step 3: Implement the Watcher in `internal/watch/watcher.go`**

`type Options`, `func Start(ctx context.Context, client kubernetes.Interface, o Options) (*Watcher, error)`, `func (w *Watcher) InstallReady(ctx context.Context, began time.Time, exited <-chan struct{}) (Install, error)`, `func (w *Watcher) Transition(ctx context.Context, name verdict.WindowName, began time.Time, exited <-chan struct{}) Transition`, `func (w *Watcher) PodsAt(at time.Time) (stopping, starting []PodNote)` and `func (w *Watcher) Stop()`, each with its interface comment from the Interfaces block. The Watcher holds `mu sync.Mutex`, `tr *Tracker` and `done chan struct{}`, and every call into the Tracker is made under `mu`, with `o.Now()` read while `mu` is held, so that the Tracker is told its times in order.

1. Start parses `o.Selector` with `labels.Parse`; an error there is a plain error (the configuration rejects such a selector first). It makes the Tracker from the options, and a shared informer factory from `k8s.io/client-go/informers` limited to `o.Namespace`, with no resync, for Deployments, StatefulSets, DaemonSets, ReplicaSets and Pods.
2. Each informer's handler calls `Set` for an add or an update and `Gone` for a delete (a `cache.DeletedFinalStateUnknown` gives its `Obj`), then sends without blocking on a wake channel of capacity 1.
3. Start starts the factory on a stop channel of its own and waits, with `cache.WaitForCacheSync`, on each handler registration's `HasSynced`, so that the Tracker has been told every object that existed. The wait also ends when ctx ends (use `context.AfterFunc`, so that no goroutine is left waiting on ctx); then Start stops the Watcher and returns ctx's error.
4. Stop, once: closes the stop channel, calls the factory's `Shutdown` (it returns when the informers' goroutines have ended), and closes `done`.
5. InstallReady loops on: ctx ends, which returns ctx's error; `exited` is closed, which notes `o.Now()` as the moment the command exited, once; a 200 ms ticker or the wake channel, which asks `InstallState(began, exitedAt, o.Now())` and returns when it is ready or gives an error.
6. Transition calls `Begin(name, began)` and makes the first reading of Probed (Step 4). It then loops on: ctx ends, which returns `State(o.Now())` with Ended set to that moment and Probed from the first reading; `exited` is closed, which calls `CommandExited(o.Now())`, once; a 200 ms ticker or the wake channel, which asks `State(o.Now())`. When the window has ended it makes the second reading, sets Probed on each Count, and returns the Transition.
7. PodsAt is the Tracker's PodsAt.

- [ ] **Step 4: Find the workloads behind a Service, in `internal/watch/service.go`**

Two unexported functions: `func (w *Watcher) behind(ctx context.Context) (map[Workload]bool, error)` and `func (t *Tracker) podWorkload(uid types.UID) (Workload, bool)`, which gives the workload of the pod with that UID when the Tracker knows it.
1. With `o.Service` nil, every Probed is `not known`, and nothing is read.
2. A Service in a namespace other than `o.Namespace` has no watched workload behind it; nothing is read.
3. Otherwise `behind` lists the EndpointSlices of the Service's namespace with the label selector `kubernetes.io/service-name=NAME`. Each endpoint whose `targetRef` is a Pod puts that pod's workload behind the Service.
4. A Count's Probed is `yes` when its workload was behind the Service at either reading, else `no`. A reading that fails counts as no reading; when none succeeded, Probed is `not known`. After ctx has ended there is no second reading.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/watch/ -v`
Expected: PASS

Run: `go test -race ./internal/watch/`
Expected: `ok`, and no `DATA RACE`. The race detector needs a C compiler on the machine.

- [ ] **Step 6: Write the test with a cluster**

`internal/watch/watcher_cluster_test.go`, with the build tag `cluster`. The kind node image holds the pause image that `pauseImage` names (in kind v0.33.0, `images/base/files/etc/containerd/config.toml` sets it as the sandbox image); if `docs/proofs/p12-keys.md` records another tag, use that one. The transition's `began` is read before the patch, as `run` reads it before it starts a command; the Tracker counts a pod first seen after `began` as created even when Begin is told later.

```go
//go:build cluster

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
```

- [ ] **Step 7: Run the test with a cluster**

`cluster.CheckRunner` refuses a binary that is not static, and the test binary is one only when it is built without cgo.

Run: `env CGO_ENABLED=0 go test -tags cluster -p 1 ./internal/watch/ -run TestWatcherOnACluster -v -timeout 20m`
Expected: PASS, with a line `quickest stop …, digests [sha256:…]`. The test deletes its cluster when it ends.

- [ ] **Step 8: Commit**

```bash
git add internal/watch
git commit -m "Follow the watched workloads through the API, and find the workloads behind a Service"
```

### Task 28: `probe`: the target check

**Files:**
- Create: `internal/probe/target.go`
- Test: `internal/probe/target_test.go`

**Interfaces:**
- Consumes: `const ProbeNamespace = "rollproof-probe"` (Task 20); `type Condition` with TargetUnsupported (Task 12).
- Produces: `type Target struct { Service *types.NamespacedName; Notes []string }`
  What the built-in probe's URL points at. Service is the Service its host
  names, nil when the host names none of the cluster. Notes are sentences
  for the report about how this target is probed.
- Produces: `func CheckTarget(ctx context.Context, client kubernetes.Interface, rawURL string) (Target, error)`
  Reads the URL's host as a Service's name and looks the Service up. An
  error is a Condition with TargetUnsupported: a Service that does not
  exist, is headless, routes only within a node, or a host that names one
  pod of a headless Service. The text names a way out.

- [ ] **Step 1: Write the failing tests**

`internal/probe/target_test.go`, with the fake client of `k8s.io/client-go/kubernetes/fake`. One row for each form of host and each outcome.

```go
package probe

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"rollproof/internal/verdict"
)

// service is a Service with a cluster address; each change edits it.
func service(namespace, name string, changes ...func(*corev1.Service)) *corev1.Service {
	s := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, ClusterIP: "10.96.0.20",
			Ports: []corev1.ServicePort{{Port: 80}}},
	}
	for _, change := range changes {
		change(s)
	}
	return s
}

func TestCheckTarget(t *testing.T) {
	const waysOut = "the ways out: a Service with a cluster address for the test, or a custom probe"
	const oneClient = "the Service sends one client to one pod; every connection of the probe reaches the same pod until that pod is removed"
	const missing = "the host ends in .svc and no such Service exists: orders/orders; " + waysOut
	const onePod = "the host names one pod of a headless Service: pod orders-0 of Service orders/orders; " +
		"the probe would go on dialling the address of a pod that has been replaced; " + waysOut
	orders := &types.NamespacedName{Namespace: "orders", Name: "orders"}
	withAddress := []runtime.Object{service("orders", "orders")}
	headless := []runtime.Object{service("orders", "orders", func(s *corev1.Service) {
		s.Spec.ClusterIP = corev1.ClusterIPNone
	})}
	nodeLocal := []runtime.Object{service("orders", "orders", func(s *corev1.Service) {
		local := corev1.ServiceInternalTrafficPolicyLocal
		s.Spec.InternalTrafficPolicy = &local
	})}
	affinity := []runtime.Object{service("orders", "orders", func(s *corev1.Service) {
		s.Spec.SessionAffinity = corev1.ServiceAffinityClientIP
	})}
	externalName := []runtime.Object{service("orders", "orders", func(s *corev1.Service) {
		s.Spec.Type = corev1.ServiceTypeExternalName
		s.Spec.ExternalName = "orders.example.com"
		s.Spec.ClusterIP = ""
	})}
	for _, r := range []struct {
		name    string
		objs    []runtime.Object
		url     string
		service *types.NamespacedName
		notes   []string
		code    verdict.Code // empty: no error
		text    string
	}{
		{name: "NAME.NS.svc", objs: withAddress, url: "http://orders.orders.svc", service: orders},
		{name: "NAME.NS", objs: withAddress, url: "http://orders.orders", service: orders},
		{name: "NAME.NS.svc and the cluster's domain", objs: withAddress, url: "http://orders.orders.svc.cluster.local", service: orders},
		{name: "a port", objs: withAddress, url: "http://orders.orders.svc:8080", service: orders},
		{name: "a path, a query and a port", objs: withAddress, url: "http://orders.orders.svc:8080/work?ms=20", service: orders},
		{name: "one label names a Service of the probe's namespace",
			objs: []runtime.Object{service("rollproof-probe", "orders")}, url: "http://orders",
			service: &types.NamespacedName{Namespace: "rollproof-probe", Name: "orders"}},
		{name: "one label that names nothing is probed as it is", objs: withAddress, url: "http://orders"},
		{name: "NAME.NS.svc that does not exist", url: "http://orders.orders.svc",
			code: verdict.TargetUnsupported, text: missing},
		{name: "NAME.NS.svc.cluster.local that does not exist", url: "http://orders.orders.svc.cluster.local",
			code: verdict.TargetUnsupported, text: missing},
		{name: "NAME.NS that does not exist is probed as it is", url: "http://orders.orders"},
		{name: "a headless Service", objs: headless, url: "http://orders.orders.svc",
			code: verdict.TargetUnsupported,
			text: "Service orders/orders is headless: a headless Service has no cluster address, and its name resolves to the addresses of pods, which change; " + waysOut},
		{name: "one pod of a headless Service", objs: headless, url: "http://orders-0.orders.orders.svc",
			code: verdict.TargetUnsupported, text: onePod},
		{name: "one pod, with the cluster's domain", url: "http://orders-0.orders.orders.svc.cluster.local",
			code: verdict.TargetUnsupported, text: onePod},
		{name: "a Service that routes only within a node", objs: nodeLocal, url: "http://orders.orders.svc",
			code: verdict.TargetUnsupported,
			text: "Service orders/orders routes only within a node (internalTrafficPolicy Local), so it has no endpoint for the probe's node; " + waysOut},
		{name: "an IPv4 address", objs: withAddress, url: "http://10.96.0.10"},
		{name: "an IPv6 address with a port", objs: withAddress, url: "http://[fd00::1]:8080"},
		{name: "an outside name", objs: withAddress, url: "http://api.example.com"},
		{name: "two labels that name no Service", url: "http://example.com"},
		{name: "https adds no note", objs: withAddress, url: "https://orders.orders.svc", service: orders},
		{name: "affinity by client address", objs: affinity, url: "http://orders.orders.svc", service: orders,
			notes: []string{oneClient}},
		{name: "a Service of type ExternalName names no Service of the cluster", objs: externalName,
			url: "http://orders.orders.svc"},
	} {
		t.Run(r.name, func(t *testing.T) {
			got, err := CheckTarget(t.Context(), fake.NewClientset(r.objs...), r.url)
			if r.code != "" {
				var c verdict.Condition
				if !errors.As(err, &c) || c.Code != r.code || c.Text != r.text {
					t.Fatalf("err = %v\nwant %s: %s", err, r.code, r.text)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := Target{Service: r.service, Notes: r.notes}
			if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("target (-want +got):\n%s", diff)
			}
		})
	}
}

// An error of the API server that is not "not found" is the tool's own failure.
func TestCheckTargetAPIError(t *testing.T) {
	client := fake.NewClientset()
	client.PrependReactor("get", "services", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("the server is currently unable to handle the request")
	})
	_, err := CheckTarget(t.Context(), client, "http://orders.orders.svc")
	var c verdict.Condition
	if err == nil || errors.As(err, &c) {
		t.Fatalf("err = %v; want a plain error", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/probe/ -run TestCheckTarget -v`
Expected: FAIL, build failed: `undefined: CheckTarget`

- [ ] **Step 3: Implement `CheckTarget` in `internal/probe/target.go`**

`type Target` and `func CheckTarget(ctx context.Context, client kubernetes.Interface, rawURL string) (Target, error)`, each with its interface comment from the Interfaces block. The cluster's domain is `cluster.local`: the tool creates the cluster and does not change it.

1. Parse the URL; a URL that does not parse is a plain error. The host is `u.Hostname()` (no port, no brackets), in lower case, without a final dot.
2. An `https` URL adds no note: the report says from another field that the certificate is not checked.
3. Read the host:

| Host | Service looked up | When it does not exist |
| --- | --- | --- |
| an IPv4 or IPv6 address | none | — |
| `NAME` | NAME in `cluster.ProbeNamespace` | Service nil, no error |
| `NAME.NS` | NAME in NS | Service nil, no error |
| `NAME.NS.svc`, `NAME.NS.svc.cluster.local` | NAME in NS | TargetUnsupported |
| `POD.NAME.NS.svc`, `POD.NAME.NS.svc.cluster.local` | none: TargetUnsupported at once | — |
| any other | none | — |

4. A lookup error other than "not found" (`apierrors.IsNotFound`) is a plain error that names the Service; the run treats it as a failure of the tool.
5. A Service of type `ExternalName` names no Service of the cluster: the Target has Service nil, there is no error, and no note is added for it. Any other Service that exists: `spec.clusterIP` `None` is TargetUnsupported (headless); `spec.internalTrafficPolicy` `Local` is TargetUnsupported; otherwise the Target names it, and `spec.sessionAffinity` `ClientIP` adds the note `the Service sends one client to one pod; every connection of the probe reaches the same pod until that pod is removed`.
6. Every TargetUnsupported text ends with `; the ways out: a Service with a cluster address for the test, or a custom probe`, and begins as the test rows show:
   - no such Service: `the host ends in .svc and no such Service exists: NS/NAME`;
   - headless: `Service NS/NAME is headless: a headless Service has no cluster address, and its name resolves to the addresses of pods, which change`;
   - one pod: `the host names one pod of a headless Service: pod POD of Service NS/NAME; the probe would go on dialling the address of a pod that has been replaced`;
   - within a node: `Service NS/NAME routes only within a node (internalTrafficPolicy Local), so it has no endpoint for the probe's node`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/probe/ -run TestCheckTarget -v`
Expected: PASS, both `TestCheckTarget` and `TestCheckTargetAPIError`.

- [ ] **Step 5: Commit**

```bash
git add internal/probe/target.go internal/probe/target_test.go
git commit -m "Check the built-in probe's target before the probe starts"
```
