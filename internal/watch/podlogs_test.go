package watch

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// logPod is pod() with the given containers in its spec, each with a status: terminated with
// exit code 0 after restarts restarts. A terminated container's log is read once.
func logPod(name, uid, ownerKind, ownerName string, restarts int32, containers ...string) *corev1.Pod {
	p := pod(name, uid, ownerKind, ownerName, true, false)
	for _, c := range containers {
		p.Spec.Containers = append(p.Spec.Containers, corev1.Container{Name: c})
		p.Status.ContainerStatuses = append(p.Status.ContainerStatuses, corev1.ContainerStatus{Name: c, RestartCount: restarts,
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0, Reason: "Completed"}}})
	}
	return p
}

// waiting is p with each container waiting to start.
func waiting(p *corev1.Pod) *corev1.Pod {
	for i := range p.Status.ContainerStatuses {
		p.Status.ContainerStatuses[i].State = corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}}
	}
	return p
}

// serveLogs answers every GetLogs of client with "log of CONTAINER\n", or "previous run of
// CONTAINER\n" when the options ask for the previous run; with fail set, it fails each with fail.
// The fake's log action holds no pod name, so the text names the container only.
func serveLogs(client *fake.Clientset, fail error) {
	client.PrependReactor("get", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "log" {
			return false, nil, nil
		}
		if fail != nil {
			return true, nil, fail
		}
		o := a.(k8stesting.GenericAction).GetValue().(*corev1.PodLogOptions)
		text := "log of " + o.Container + "\n"
		if o.Previous {
			text = "previous run of " + o.Container + "\n"
		}
		return true, &runtime.Unknown{Raw: []byte(text)}, nil
	})
}

// logActions counts the GetLogs the client has been asked for.
func logActions(client *fake.Clientset) int {
	n := 0
	for _, a := range client.Actions() {
		if a.GetSubresource() == "log" {
			n++
		}
	}
	return n
}

// create stores Deployments, ReplicaSets and Pods in the fake client, in order.
func create(t *testing.T, client *fake.Clientset, objs ...runtime.Object) {
	t.Helper()
	var err error
	for _, obj := range objs {
		switch o := obj.(type) {
		case *appsv1.Deployment:
			_, err = client.AppsV1().Deployments("orders").Create(t.Context(), o, metav1.CreateOptions{})
		case *appsv1.ReplicaSet:
			_, err = client.AppsV1().ReplicaSets("orders").Create(t.Context(), o, metav1.CreateOptions{})
		case *corev1.Pod:
			_, err = client.CoreV1().Pods("orders").Create(t.Context(), o, metav1.CreateOptions{})
		default:
			t.Fatalf("create: %T", obj)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

func deletePod(t *testing.T, client *fake.Clientset, name string) {
	t.Helper()
	if err := client.CoreV1().Pods("orders").Delete(t.Context(), name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
}

// watchLogs starts a Watcher on an empty namespace with the selector app=orders and an evidence
// recorder, and has the client serve logs (or fail them with fail) before any object exists.
func watchLogs(t *testing.T, fail error) (*Watcher, *fake.Clientset, *evidenceFiles) {
	t.Helper()
	ev := newEvidence()
	w, client, _ := startWatcherWith(t, func(o *Options) { o.Selector = "app=orders"; o.Evidence = ev.open }, nil)
	serveLogs(client, fail)
	return w, client, ev
}

// seen waits up to 5 s of real time until the Watcher has seen the pod name created.
func seen(t *testing.T, w *Watcher, name string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		for _, e := range w.PodEvents() {
			if e.Pod == name && e.Kind == "created" {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the Watcher did not see pod %s within 5 s", name)
		}
	}
}

// A3: the pod's workload may become known after the pod, when the ReplicaSet's Set comes second.
func TestPodLogsOfWatchedPods(t *testing.T) {
	orders, rs := deployment("orders", 1, 1, 1, 1, 1, 1), replicaSet("orders-a", "orders")
	pa := logPod("orders-a-1", "a1", "ReplicaSet", "orders-a", 0, "app", "sidecar")
	for _, tc := range []struct {
		name  string
		order []runtime.Object
	}{
		{"the ReplicaSet first", []runtime.Object{orders, rs, pa}},
		{"the pod first", []runtime.Object{orders, pa}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, client, ev := watchLogs(t, nil)
			create(t, client, tc.order...)
			if len(tc.order) == 2 {
				seen(t, w, "orders-a-1") // a pod whose ReplicaSet the Watcher does not know yet
				create(t, client, rs)
			}
			create(t, client, deployment("batch", 1, 1, 1, 1, 1, 1), replicaSet("batch-a", "batch"),
				logPod("batch-a-1", "k1", "ReplicaSet", "batch-a", 0, "app"))
			ev.holds(t, "pods/orders-a-1/app.log", "log of app\n")
			ev.holds(t, "pods/orders-a-1/sidecar.log", "log of sidecar\n")
			deletePod(t, client, "orders-a-1")
			if got := ev.text(t, "pods/orders-a-1/app.log"); got != "log of app\n" {
				t.Errorf("app.log %q", got)
			}
			if got := ev.text(t, "pods/orders-a-1/sidecar.log"); got != "log of sidecar\n" {
				t.Errorf("sidecar.log %q", got)
			}
			for _, n := range ev.names() {
				if strings.HasPrefix(n, "pods/batch-a-1/") || strings.HasSuffix(n, ".previous.log") {
					t.Errorf("evidence file %s, want none for batch-a-1 and no previous run", n)
				}
			}
		})
	}
}

func TestPodLogsKeepThePreviousRun(t *testing.T) {
	_, client, ev := watchLogs(t, nil)
	create(t, client, deployment("orders", 1, 1, 1, 1, 1, 1), replicaSet("orders-a", "orders"),
		logPod("orders-a-1", "a1", "ReplicaSet", "orders-a", 1, "app"))
	if got := ev.text(t, "pods/orders-a-1/app.previous.log"); got != "previous run of app\n" {
		t.Errorf("app.previous.log %q", got)
	}
	ev.holds(t, "pods/orders-a-1/app.log", "log of app\n")
	deletePod(t, client, "orders-a-1")
	if got := ev.text(t, "pods/orders-a-1/app.log"); got != "log of app\n" {
		t.Errorf("app.log %q", got)
	}
}

func TestPodLogsWaitForTheContainerToStart(t *testing.T) {
	_, client, ev := watchLogs(t, nil)
	create(t, client, deployment("orders", 1, 1, 1, 1, 1, 1), replicaSet("orders-a", "orders"),
		waiting(logPod("orders-a-1", "a1", "ReplicaSet", "orders-a", 0, "app")))
	time.Sleep(500 * time.Millisecond)
	if n := logActions(client); n != 0 {
		t.Errorf("%d GetLogs for a container that has not started", n)
	}
	update(t, client, logPod("orders-a-1", "a1", "ReplicaSet", "orders-a", 0, "app"))
	ev.holds(t, "pods/orders-a-1/app.log", "log of app\n")
	deletePod(t, client, "orders-a-1")
	if got := ev.text(t, "pods/orders-a-1/app.log"); got != "log of app\n" {
		t.Errorf("app.log %q", got)
	}
}

// Review Focus 4: a log that cannot be read is told in its own file.
func TestPodLogsOfAPodGoneBeforeItsLogWasRead(t *testing.T) {
	const prefix = "rollproof: the log of container app could not be read: "
	notFound := errors.New(`container "app" not found`)

	t.Run("the container never started", func(t *testing.T) {
		_, client, ev := watchLogs(t, nil)
		create(t, client, deployment("orders", 1, 1, 1, 1, 1, 1), replicaSet("orders-a", "orders"),
			waiting(logPod("orders-a-1", "a1", "ReplicaSet", "orders-a", 0, "app")))
		time.Sleep(500 * time.Millisecond)
		deletePod(t, client, "orders-a-1")
		if got, want := ev.text(t, "pods/orders-a-1/app.log"), prefix+"the pod was gone before the container started\n"; got != want {
			t.Errorf("app.log %q, want %q", got, want)
		}
	})

	for _, restarts := range []int32{0, 1} {
		t.Run(fmt.Sprintf("every opening fails, %d restarts", restarts), func(t *testing.T) {
			_, client, ev := watchLogs(t, notFound)
			create(t, client, deployment("orders", 1, 1, 1, 1, 1, 1), replicaSet("orders-a", "orders"),
				logPod("orders-a-1", "a1", "ReplicaSet", "orders-a", restarts, "app"))
			time.Sleep(500 * time.Millisecond)
			deletePod(t, client, "orders-a-1")
			files := map[string]string{"pods/orders-a-1/app.log": prefix}
			if restarts > 0 {
				files["pods/orders-a-1/app.previous.log"] = "rollproof: the previous run of container app could not be read: "
			}
			for name, start := range files {
				got := ev.text(t, name)
				if !strings.HasPrefix(got, start) || !strings.Contains(got, notFound.Error()) ||
					strings.Count(got, "\n") != 1 || !strings.HasSuffix(got, "\n") {
					t.Errorf("%s %q, want one line that begins %q and holds %q", name, got, start, notFound)
				}
			}
		})
	}
}

func TestStopClosesThePodLogs(t *testing.T) {
	w, client, ev := watchLogs(t, nil)
	create(t, client, deployment("orders", 1, 1, 1, 1, 1, 1), replicaSet("orders-a", "orders"),
		logPod("orders-a-1", "a1", "ReplicaSet", "orders-a", 0, "app", "sidecar"))
	ev.holds(t, "pods/orders-a-1/app.log", "log of app\n")
	ev.holds(t, "pods/orders-a-1/sidecar.log", "log of sidecar\n")
	w.Stop()
	for _, n := range ev.names() {
		if text, _, closed := ev.state(n); !closed {
			t.Errorf("%s is open when Stop has returned (it holds %q)", n, text)
		}
	}
}

func TestPodEvents(t *testing.T) {
	w, client, clock := startWatcher(t, nil, base()...)
	exited, res := startDeploy(t, w, client, clock)
	close(exited)
	drive(t, clock, res)
	end := clock.Now()
	loose := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "loose-1", Namespace: "orders", UID: "l1"}}
	create(t, client, loose)
	wl := "deployment/orders"
	want := []PodEvent{
		{t0, "orders-a-1", wl, "created"}, {t0, "orders-a-1", wl, "ready"},
		{t0, "orders-a-2", wl, "created"}, {t0, "orders-a-2", wl, "ready"},
		{at(1), "orders-a-1", wl, "deleted"}, {at(1), "orders-a-1", wl, "gone"},
		{at(1), "orders-a-2", wl, "deleted"}, {at(1), "orders-a-2", wl, "gone"},
		{at(1), "orders-b-1", wl, "created"}, {at(1), "orders-b-1", wl, "ready"},
		{at(1), "orders-b-2", wl, "created"}, {at(1), "orders-b-2", wl, "ready"},
		{end, "loose-1", "", "created"}, // no owner, so no workload; no Ready condition, so no ready
	}
	var got []PodEvent
	for deadline := time.Now().Add(5 * time.Second); len(got) < len(want) && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		got = w.PodEvents()
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("PodEvents (-want +got):\n%s", d)
	}
}
