package watch

import (
	"cmp"
	"encoding/json"
	"slices"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"rollproof/internal/verdict"
)

// Finish is one moment at which a transition came to be finished: the transition and
// the Watcher's clock at that moment. The last Finish of a transition whose
// window ended after the settle time is that Transition's Finished.
type Finish struct {
	Name verdict.WindowName
	At   time.Time
}

// Finishes returns the channel on which the Watcher sends a Finish each time the open
// transition comes to be finished, again after its settle time started
// again, in order. Values wait for a reader and never hold up the watch. The
// same channel on every call. It is closed when Stop returns; a value not
// received by then is dropped.
func (w *Watcher) Finishes() <-chan Finish {
	return w.finishes.out
}

// finishQueue holds the Finishes not yet received. push never waits; the queue's goroutine hands
// them to out one at a time, in order, until stop is closed.
type finishQueue struct {
	mu      sync.Mutex
	pending []Finish
	more    chan struct{} // holds a signal when push added a value since the goroutine last looked
	out     chan Finish
	ended   chan struct{} // closed when the goroutine has returned
}

func newFinishQueue() *finishQueue {
	return &finishQueue{more: make(chan struct{}, 1), out: make(chan Finish), ended: make(chan struct{})}
}

// push adds f at the end of the queue.
func (q *finishQueue) push(f Finish) {
	q.mu.Lock()
	q.pending = append(q.pending, f)
	q.mu.Unlock()
	select {
	case q.more <- struct{}{}:
	default: // a signal already waiting stands for this value too
	}
}

// run hands the queued values to out, oldest first, until stop is closed. It never closes out:
// the Watcher's Stop does, after run has returned.
func (q *finishQueue) run(stop <-chan struct{}) {
	defer close(q.ended)
	for {
		q.mu.Lock()
		var next Finish
		waiting := len(q.pending) > 0
		if waiting {
			next = q.pending[0]
		}
		q.mu.Unlock()
		if !waiting {
			select {
			case <-q.more:
				continue
			case <-stop:
				return
			}
		}
		select {
		case q.out <- next:
			q.mu.Lock()
			q.pending = q.pending[1:]
			q.mu.Unlock()
		case <-stop:
			return
		}
	}
}

// noteFinish queues a Finish when the open transition has come to be finished more times than
// the Finishes queued for it, also when it came to be finished again at the moment of the last
// one. Call it under mu after each call into tr that can finish a transition: each such call
// finishes it at most once.
func (w *Watcher) noteFinish() {
	f, n := w.tr.openFinish()
	if n <= w.queued {
		return
	}
	w.queued = n
	w.finishes.push(f)
}

// podList is a PodList of every pod that the Tracker holds and that is not gone, by name. Its
// items are copies of the pods, so that it can be encoded after the caller's lock is released.
func (t *Tracker) podList() corev1.PodList {
	// Items is never nil, so that a namespace with no pod gives a list, as the API server does.
	list := corev1.PodList{TypeMeta: metav1.TypeMeta{Kind: "PodList", APIVersion: "v1"},
		Items: []corev1.Pod{}}
	for _, p := range t.pods {
		if p.gone.IsZero() {
			list.Items = append(list.Items, *p.obj)
		}
	}
	slices.SortFunc(list.Items, func(a, b corev1.Pod) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.UID, b.UID))
	})
	return list
}

// writePods writes list to the evidence file name as JSON indented by two spaces, with a final
// newline. It writes nothing when Options.Evidence is nil, and ignores every error, as
// Options.Evidence says.
func (w *Watcher) writePods(name string, list corev1.PodList) {
	if w.o.Evidence == nil {
		return
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return
	}
	f, err := w.o.Evidence(name)
	if err != nil {
		return
	}
	_, _ = f.Write(append(b, '\n'))
	_ = f.Close()
}
