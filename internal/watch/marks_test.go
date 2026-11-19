package watch

import (
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"rollproof/internal/verdict"
)

// evidenceFiles records what a Watcher writes through Options.Evidence. One lock guards every file.
type evidenceFiles struct {
	mu    sync.Mutex
	files map[string]*recordedFile // by name
}

type recordedFile struct {
	mu     *sync.Mutex
	buf    bytes.Buffer
	closed bool
}

func (f *recordedFile) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.buf.Write(p)
}

func (f *recordedFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func newEvidence() *evidenceFiles { return &evidenceFiles{files: map[string]*recordedFile{}} }

func (e *evidenceFiles) open(name string) (io.WriteCloser, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	f := &recordedFile{mu: &e.mu}
	e.files[name] = f
	return f, nil
}

// names lists the names opened so far, sorted.
func (e *evidenceFiles) names() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Sorted(maps.Keys(e.files))
}

// state gives the text of the file name, whether it was opened, and whether it is closed.
func (e *evidenceFiles) state(name string) (text string, opened, closed bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	f, ok := e.files[name]
	if !ok {
		return "", false, false
	}
	return f.buf.String(), true, f.closed
}

// text waits up to 5 s of real time until the file name exists and is closed, and returns its text.
func (e *evidenceFiles) text(t *testing.T, name string) string {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if s, _, closed := e.state(name); closed {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s was not written and closed within 5 s; files: %v", name, e.names())
		}
	}
}

// holds waits up to 5 s of real time until the file name, open or closed, holds exactly want.
func (e *evidenceFiles) holds(t *testing.T, name, want string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if s, _, _ := e.state(name); s == want {
			return
		}
		if time.Now().After(deadline) {
			s, _, _ := e.state(name)
			t.Fatalf("%s holds %q after 5 s, want %q", name, s, want)
		}
	}
}

// startDeploy begins a deploy on w and makes, through the client and at +1 s, the changes of
// TestTransition: a new spec, ReplicaSet orders-b with two ready pods, the two old pods deleted,
// the rollout complete. It returns when the Watcher has seen them, with the channel that tells
// the command's exit, still open, and the channel of Transition's result.
func startDeploy(t *testing.T, w *Watcher, client *fake.Clientset, clock *fakeClock) (chan struct{}, chan Transition) {
	t.Helper()
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
	return exited, res
}

func TestFinishesAsTheyHappen(t *testing.T) {
	w, client, clock := startWatcher(t, nil, base()...)
	exited, res := startDeploy(t, w, client, clock)
	close(exited)
	var f1, f2 time.Time
	until(t, w, clock, func(s Transition) bool { f1 = s.Finished; return !f1.IsZero() })
	clock.Advance(5 * time.Second)
	if _, err := client.CoreV1().Pods("orders").Create(t.Context(),
		pod("orders-b-3", "b3", "ReplicaSet", "orders-b", true, false), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	until(t, w, clock, func(s Transition) bool { f2 = s.Finished; return f2.Equal(clock.Now()) })

	tr := drive(t, clock, res) // nobody has read Finishes yet: the watch was not held up
	if !tr.Finished.Equal(f2) || len(tr.SettleRestarts) != 1 {
		t.Errorf("finished %v with %d settle restarts; want %v and 1", tr.Finished, len(tr.SettleRestarts), f2)
	}
	for _, want := range []Finish{{Name: verdict.Deploy, At: f1}, {Name: verdict.Deploy, At: f2}} {
		select {
		case got := <-w.Finishes():
			if got.Name != want.Name || !got.At.Equal(want.At) {
				t.Errorf("finish %+v, want %+v", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("no finish %+v within 1 s", want)
		}
	}
	select {
	case got := <-w.Finishes():
		t.Errorf("a third finish %+v", got)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestFinishesIsClosedByStop(t *testing.T) {
	w, _, _ := startWatcher(t, nil, base()...)
	w.Stop()
	select {
	case _, ok := <-w.Finishes():
		if ok {
			t.Error("a value came from Finishes after Stop")
		}
	case <-time.After(time.Second):
		t.Fatal("Finishes was not closed within 1 s of Stop")
	}
}

func TestPodListsBeforeAndAfter(t *testing.T) {
	ev := newEvidence()
	w, client, clock := startWatcherWith(t, func(o *Options) { o.Evidence = ev.open }, nil, base()...)
	exited, res := startDeploy(t, w, client, clock)
	close(exited)
	drive(t, clock, res)
	if d := cmp.Diff([]string{"pods/deploy-after.json", "pods/deploy-before.json"}, ev.names()); d != "" {
		t.Errorf("evidence files (-want +got):\n%s", d)
	}
	for name, want := range map[string][]string{
		"pods/deploy-before.json": {"orders-a-1", "orders-a-2"},
		"pods/deploy-after.json":  {"orders-b-1", "orders-b-2"},
	} {
		var list corev1.PodList
		if err := json.Unmarshal([]byte(ev.text(t, name)), &list); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if list.Kind != "PodList" || list.APIVersion != "v1" {
			t.Errorf("%s is a %s of %s, want a PodList of v1", name, list.Kind, list.APIVersion)
		}
		var got []string
		for _, p := range list.Items {
			got = append(got, p.Name)
		}
		if d := cmp.Diff(want, got); d != "" {
			t.Errorf("%s (-want +got):\n%s", name, d)
		}
	}
}
