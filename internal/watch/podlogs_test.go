package watch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8swatch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	fakerest "k8s.io/client-go/rest/fake"
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

// create stores Deployments, ReplicaSets, StatefulSets and Pods in the fake client, in order.
func create(t *testing.T, client *fake.Clientset, objs ...runtime.Object) {
	t.Helper()
	var err error
	for _, obj := range objs {
		switch o := obj.(type) {
		case *appsv1.Deployment:
			_, err = client.AppsV1().Deployments("orders").Create(t.Context(), o, metav1.CreateOptions{})
		case *appsv1.ReplicaSet:
			_, err = client.AppsV1().ReplicaSets("orders").Create(t.Context(), o, metav1.CreateOptions{})
		case *appsv1.StatefulSet:
			_, err = client.AppsV1().StatefulSets("orders").Create(t.Context(), o, metav1.CreateOptions{})
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

// watched waits up to 5 s of real time until cond holds of the Watcher's Tracker.
func watched(t *testing.T, w *Watcher, what string, cond func(*Tracker) bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		w.mu.Lock()
		ok := cond(w.tr)
		w.mu.Unlock()
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the Watcher did not see %s within 5 s", what)
		}
	}
}

// A pod whose workload the watch learns after the pod, and after its ReplicaSet, is followed
// from the workload's Set.
func TestPodLogsWhenTheWorkloadComesLast(t *testing.T) {
	t.Run("a Deployment after its ReplicaSet and pod", func(t *testing.T) {
		w, client, ev := watchLogs(t, nil)
		create(t, client, replicaSet("orders-a", "orders"), logPod("orders-a-1", "a1", "ReplicaSet", "orders-a", 0, "app"))
		seen(t, w, "orders-a-1")
		watched(t, w, "ReplicaSet orders-a", func(tr *Tracker) bool { return tr.owners["orders-a"] == "orders" })
		create(t, client, deployment("orders", 1, 1, 1, 1, 1, 1))
		ev.holds(t, "pods/orders-a-1/app.log", "log of app\n")
	})
	t.Run("a StatefulSet after its pod", func(t *testing.T) {
		w, client, ev := watchLogs(t, nil)
		create(t, client, logPod("orders-0", "s0", "StatefulSet", "orders", 0, "app"))
		seen(t, w, "orders-0")
		create(t, client, statefulSet("orders", 1, 1, 1, 1, 1, appsv1.RollingUpdateStatefulSetStrategyType))
		ev.holds(t, "pods/orders-0/app.log", "log of app\n")
	})
}

// running is p with each container running, after restarts restarts.
func running(p *corev1.Pod, restarts int32) *corev1.Pod {
	for i := range p.Status.ContainerStatuses {
		p.Status.ContainerStatuses[i].RestartCount = restarts
		p.Status.ContainerStatuses[i].State = corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	}
	return p
}

// logStream is one log stream that a scriptedLogs serves: text, then, when open is not nil, no
// more until open is closed or the request ends, then err, or the end of the stream when err is nil.
type logStream struct {
	text string
	open chan struct{}
	err  error
}

// scriptedLogs is a fake client whose pods' logs are the streams of its script, in order, one at
// each opening; once they are used, a stream stays open with nothing in it until the request
// ends. The fake clientset's own GetLogs serves a whole text and its end, and cannot fail after
// the opening. It records the options of each opening.
type scriptedLogs struct {
	*fake.Clientset
	mu     sync.Mutex
	script []logStream
	asked  []corev1.PodLogOptions
}

func (s *scriptedLogs) CoreV1() corev1client.CoreV1Interface {
	return scriptedCore{s.Clientset.CoreV1(), s}
}

func (s *scriptedLogs) options() []corev1.PodLogOptions {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]corev1.PodLogOptions(nil), s.asked...)
}

type scriptedCore struct {
	corev1client.CoreV1Interface
	s *scriptedLogs
}

func (c scriptedCore) Pods(namespace string) corev1client.PodInterface {
	return scriptedPods{c.CoreV1Interface.Pods(namespace), c.s}
}

type scriptedPods struct {
	corev1client.PodInterface
	s *scriptedLogs
}

func (p scriptedPods) GetLogs(name string, opts *corev1.PodLogOptions) *restclient.Request {
	p.s.mu.Lock()
	p.s.asked = append(p.s.asked, *opts)
	next := logStream{open: make(chan struct{})}
	if len(p.s.script) > 0 {
		next, p.s.script = p.s.script[0], p.s.script[1:]
	}
	p.s.mu.Unlock()
	client := &fakerest.RESTClient{
		Client: fakerest.CreateHTTPClient(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(io.MultiReader(
				strings.NewReader(next.text), &streamEnd{ctx: r.Context(), open: next.open, err: next.err}))}, nil
		}),
		NegotiatedSerializer: scheme.Codecs.WithoutConversion(),
		GroupVersion:         corev1.SchemeGroupVersion,
		VersionedAPIPath:     "/api/v1/namespaces/orders/pods/" + name + "/log",
	}
	return client.Request()
}

// streamEnd is the end of a scripted stream: it waits until open is closed or ctx ends, and then
// gives err, or io.EOF when err is nil.
type streamEnd struct {
	ctx  context.Context
	open chan struct{}
	err  error
}

func (e *streamEnd) Read([]byte) (int, error) {
	if e.open != nil {
		select {
		case <-e.open:
		case <-e.ctx.Done():
			return 0, e.ctx.Err()
		}
	}
	if e.err != nil {
		return 0, e.err
	}
	return 0, io.EOF
}

// watchScripted starts a Watcher on an empty namespace with the selector app=orders, an evidence
// recorder and a client whose logs are the script's streams, and stops it when the test ends.
func watchScripted(t *testing.T, script ...logStream) (*Watcher, *scriptedLogs, *evidenceFiles) {
	t.Helper()
	s := &scriptedLogs{Clientset: fake.NewClientset(), script: script}
	var watches atomic.Int32
	s.PrependWatchReactor("*", func(k8stesting.Action) (bool, k8swatch.Interface, error) {
		watches.Add(1)
		return false, nil, nil
	})
	ev := newEvidence()
	clock := &fakeClock{now: t0}
	w, err := Start(t.Context(), s, Options{Namespace: "orders", Selector: "app=orders", Settle: 15 * time.Second,
		Timeout: 300 * time.Second, Workers: 3, Now: clock.Now, Evidence: ev.open})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Stop)
	waitFor(t, &watches, 5, "watches")
	return w, s, ev
}

// A stream cut while it is read, and a pod then gone: the file ends with the reason.
func TestPodLogsOfAStreamCutWhileRead(t *testing.T) {
	cut := errors.New("connection reset by peer")
	_, client, ev := watchScripted(t, logStream{text: "one\n", err: cut})
	create(t, client.Clientset, deployment("orders", 1, 1, 1, 1, 1, 1), replicaSet("orders-a", "orders"),
		logPod("orders-a-1", "a1", "ReplicaSet", "orders-a", 0, "app"))
	ev.holds(t, "pods/orders-a-1/app.log", "one\n")
	deletePod(t, client.Clientset, "orders-a-1")
	got := ev.text(t, "pods/orders-a-1/app.log")
	const start = "one\nrollproof: the log of container app could not be read: "
	if !strings.HasPrefix(got, start) || !strings.HasSuffix(got, cut.Error()+"\n") || strings.Count(got, "\n") != 2 {
		t.Errorf("app.log %q, want %q, the error %q and a line end", got, start, cut)
	}
}

// A stream that ends inside its first line is opened again from the start: the cut line and the
// next stream's first line stay apart.
func TestPodLogsKeepACutLineApart(t *testing.T) {
	_, client, ev := watchScripted(t, logStream{text: "2026-11-13T10:00:00.5Z par"},
		logStream{text: "2026-11-13T10:00:00.5Z part\n", open: make(chan struct{})})
	create(t, client.Clientset, deployment("orders", 1, 1, 1, 1, 1, 1), replicaSet("orders-a", "orders"),
		running(logPod("orders-a-1", "a1", "ReplicaSet", "orders-a", 0, "app"), 0))
	ev.holds(t, "pods/orders-a-1/app.log", "2026-11-13T10:00:00.5Z par\n2026-11-13T10:00:00.5Z part\n")
	if o := client.options(); len(o) != 2 || o[1].SinceTime != nil {
		t.Errorf("openings %+v, want two, the second from the start", o)
	}
}

// A running container whose stream ends and whose restart count then rises: the new run is read
// from its start, after a line that tells the restart.
func TestPodLogsOfARestartedContainer(t *testing.T) {
	first := make(chan struct{})
	w, client, ev := watchScripted(t, logStream{text: "first run\n", open: first},
		logStream{text: "second run\n", open: make(chan struct{})})
	create(t, client.Clientset, deployment("orders", 1, 1, 1, 1, 1, 1), replicaSet("orders-a", "orders"),
		running(logPod("orders-a-1", "a1", "ReplicaSet", "orders-a", 0, "app"), 0))
	ev.holds(t, "pods/orders-a-1/app.log", "first run\n")
	update(t, client.Clientset, running(logPod("orders-a-1", "a1", "ReplicaSet", "orders-a", 0, "app"), 1))
	watched(t, w, "the restart", func(tr *Tracker) bool {
		return tr.pods["a1"].obj.Status.ContainerStatuses[0].RestartCount == 1
	})
	close(first)
	ev.holds(t, "pods/orders-a-1/app.log", "first run\nrollproof: the container started again (restart 1)\nsecond run\n")
	o := client.options()
	if len(o) != 2 || o[1].SinceTime != nil || o[1].Previous || !o[1].Follow {
		t.Errorf("openings %+v, want two, the second following the new run from its start", o)
	}
}

// A running container whose stream ends with the same restart count is opened again from the
// second of the last line read.
func TestPodLogsOpenTheSameRunAgain(t *testing.T) {
	_, client, ev := watchScripted(t, logStream{text: "2026-11-13T10:00:01.25Z one\n"},
		logStream{text: "2026-11-13T10:00:01.25Z one\n2026-11-13T10:00:02Z two\n", open: make(chan struct{})})
	create(t, client.Clientset, deployment("orders", 1, 1, 1, 1, 1, 1), replicaSet("orders-a", "orders"),
		running(logPod("orders-a-1", "a1", "ReplicaSet", "orders-a", 0, "app"), 0))
	ev.holds(t, "pods/orders-a-1/app.log", "2026-11-13T10:00:01.25Z one\n"+
		"rollproof: the log was opened again from 2026-11-13T10:00:01Z; lines of that second may repeat\n"+
		"2026-11-13T10:00:01.25Z one\n2026-11-13T10:00:02Z two\n")
	o := client.options()
	if len(o) != 2 || o[1].SinceTime == nil || !o[1].SinceTime.Time.Equal(at(1)) {
		t.Errorf("openings %+v, want two, the second since %v", o, at(1))
	}
}
