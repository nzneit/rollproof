package watch

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"rollproof/internal/verdict"
)

// pollEvery is how often InstallReady, InstallSettled and Transition look again when no change
// wakes them, so that a settle time or a timeout that passes with no change is noticed.
const pollEvery = 200 * time.Millisecond

// Options is what a Watcher needs. Selector is a label selector as text, empty for every
// workload. Service is the initial value of the Service whose endpoints Transition reads, which
// UseService may replace; it is nil when the probe names no Service or is a custom one. The tool
// itself passes nil and calls UseService after the target check.
type Options struct {
	Namespace, Selector string
	Settle, Timeout     time.Duration
	Workers             int
	Service             *types.NamespacedName
	Now                 func() time.Time
	// Evidence opens a file of the evidence folder by a name that may hold folders, such
	// as pods/deploy-before.json. The Watcher writes the pod lists through it,
	// and ignores its errors and its writers' errors: a caller that needs them
	// wraps the writer, as run's evidence does. Nil writes nothing.
	Evidence func(name string) (io.WriteCloser, error)
}

// Watcher follows the Deployments, StatefulSets, DaemonSets, ReplicaSets and Pods of one
// namespace through the API server's watches, and tells its Tracker each change at the moment its
// clock gives when the change arrives.
type Watcher struct {
	o       Options
	client  kubernetes.Interface
	factory informers.SharedInformerFactory

	// mu guards tr, last, service and queued. Every call into tr is made under it, with o.Now read
	// while it is held, so that tr is told its times in order. Nothing that waits is done under it.
	mu sync.Mutex
	tr *Tracker
	// queued is the last moment queued on finishes for the open transition; zero when none was.
	queued time.Time
	// finishes is the queue behind Finishes. Its goroutine runs from Start until Stop.
	finishes *finishQueue
	// service is the Service whose endpoints Transition reads: Options.Service unless UseService
	// named another.
	service *types.NamespacedName
	// last holds, by informer and key, the last object tr was told under that key and not yet told
	// gone, so that a deletion whose final state the informer does not know is still told.
	last map[objectKey]runtime.Object

	// conns holds each informer's connection, by kind; Start waits until each is up once.
	conns map[string]*connection
	// streams counts the watch streams whose events are still passed on.
	streams atomic.Int32

	// wake holds a signal when tr has been told a change since InstallReady, InstallSettled or
	// Transition last looked; one signal stands for any number of changes.
	wake     chan struct{}
	stop     chan struct{} // closed by Stop; the informers run until then
	done     chan struct{} // closed when Stop has returned and no goroutine of the Watcher runs
	stopOnce sync.Once
}

// objectKey names one object of one informer: the informer's kind and the object's
// namespace/name key.
type objectKey struct {
	kind, key string
}

// Start begins to follow the namespace's workloads, ReplicaSets and Pods and returns when it has
// seen what exists. It follows until Stop.
func Start(ctx context.Context, client kubernetes.Interface, o Options) (*Watcher, error) {
	selector, err := labels.Parse(o.Selector)
	if err != nil {
		return nil, fmt.Errorf("parse the selector: %w", err)
	}
	w := &Watcher{
		o:      o,
		client: client,
		// The factory starts and stops the informers. Each informer lists and watches the
		// namespace through a connection of the Watcher, which sees when its watch breaks.
		factory: informers.NewSharedInformerFactoryWithOptions(client, 0),
		tr: NewTracker(TrackerOptions{Namespace: o.Namespace, Selector: selector,
			Settle: o.Settle, Timeout: o.Timeout, Workers: o.Workers}),
		service:  o.Service,
		last:     map[objectKey]runtime.Object{},
		conns:    map[string]*connection{},
		wake:     make(chan struct{}, 1),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		finishes: newFinishQueue(),
	}
	go w.finishes.run(w.stop)
	var synced []cache.InformerSynced
	for _, c := range w.connections() {
		w.conns[c.kind] = c
		informer := w.factory.InformerFor(c.example, func(client kubernetes.Interface, resync time.Duration) cache.SharedIndexInformer {
			lw := cache.ToListWatcherWithWatchListSemantics(&cache.ListWatch{
				ListWithContextFunc: c.list, WatchFuncWithContext: c.Watch}, client)
			return cache.NewSharedIndexInformerWithOptions(lw, c.example,
				cache.SharedIndexInformerOptions{ResyncPeriod: resync})
		})
		reg, err := informer.AddEventHandler(handler{w: w, kind: c.kind})
		if err != nil {
			w.Stop()
			return nil, fmt.Errorf("follow the %ss: %w", c.kind, err)
		}
		synced = append(synced, reg.HasSynced)
	}
	w.factory.Start(w.stop)

	// A registration has synced when its handler has been told every object of the first list.
	// Each watch must then be up once, so that a window begins with every watch up.
	ended := make(chan struct{})
	stopWaiting := context.AfterFunc(ctx, func() { close(ended) })
	defer stopWaiting()
	if !cache.WaitForCacheSync(ended, synced...) {
		w.Stop()
		return nil, ctx.Err()
	}
	for _, c := range w.conns {
		select {
		case <-c.firstUp:
		case <-ended:
			w.Stop()
			return nil, ctx.Err()
		}
	}
	return w, nil
}

// Stop stops following. It returns when no goroutine of the Watcher runs and the channel of
// Finishes is closed; a Finish not received by then is dropped. It is safe to call twice.
func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.stop)
		// Shutdown returns when every informer has ended, its handlers included, so that no
		// Finish is queued after it.
		w.factory.Shutdown()
		<-w.finishes.ended
		close(w.finishes.out)
		close(w.done)
	})
}

// InstallReady returns when the install is ready. exited is closed when the install's command has
// exited. The error is a Condition as InstallState gives it, or ctx's error.
func (w *Watcher) InstallReady(ctx context.Context, began time.Time, exited <-chan struct{}) (Install, error) {
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	var exitedAt time.Time
	for {
		select {
		case <-ctx.Done():
			return Install{}, ctx.Err()
		case <-exited:
			exited = nil // noted once; a nil channel is never ready
			w.mu.Lock()
			exitedAt = w.o.Now()
			w.mu.Unlock()
		case <-tick.C:
		case <-w.wake:
		}
		w.mu.Lock()
		inst, ready, err := w.tr.InstallState(began, exitedAt, w.o.Now())
		w.mu.Unlock()
		if ready || err != nil {
			return inst, err
		}
	}
}

// InstallSettled returns when the settling has ended, as InstallSettling says. When ctx ends
// first, it returns what it has: the settling as it ended, when it had ended by the Tracker's
// times, or else Settled zero, Ended that moment, and no condition. Call it once, after
// InstallReady has returned with no error.
func (w *Watcher) InstallSettled(ctx context.Context, first time.Time) Settling {
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			w.mu.Lock()
			now := w.o.Now()
			s, ended := w.tr.InstallSettling(first, now)
			w.mu.Unlock()
			if !ended {
				s.Ended = later(now, first)
			}
			return s
		case <-tick.C:
		case <-w.wake:
		}
		w.mu.Lock()
		s, ended := w.tr.InstallSettling(first, w.o.Now())
		w.mu.Unlock()
		if ended {
			return s
		}
	}
}

// Transition follows one transition from began and returns when its window has ended. exited is
// closed when the transition's command has exited. When ctx ends first it returns what it has,
// with Ended set to that moment. Finished is then zero, whatever the transition did before, so
// that its window is never judged as one that finished.
//
// It writes the pod list pods/NAME-before.json when it has begun the transition and
// pods/NAME-after.json when the window has ended or ctx ended: each a PodList, in JSON, of every
// pod of the namespace that the watch holds and that is not gone, by name. Each is written beside
// the watch, so that a slow write delays neither the command's exit nor the last reading, and
// Transition returns when both are written.
func (w *Watcher) Transition(ctx context.Context, name verdict.WindowName, began time.Time, exited <-chan struct{}) Transition {
	w.mu.Lock()
	w.tr.Begin(name, began)
	w.queued = time.Time{}
	before := w.tr.podList()
	w.mu.Unlock()
	var writes sync.WaitGroup
	defer writes.Wait()
	writes.Go(func() { w.writePods("pods/"+string(name)+"-before.json", before) })

	// The readings of the Service's endpoints stay off the path that tells the Tracker anything,
	// so that a slow one cannot delay the command's exit. The first runs beside follow until the
	// window ends; one that has not returned by then counts as not made.
	firstCtx, endFirst := context.WithCancel(ctx)
	defer endFirst()
	firstRead := make(chan map[Workload]bool, 1)
	go func() {
		r := w.reading(firstCtx)
		w.mu.Lock()
		if w.tr.windowEnded(w.o.Now()) {
			r = nil // it returned after the window's end, before follow saw the end
		}
		w.mu.Unlock()
		firstRead <- r
	}()
	s, ended := w.follow(ctx, exited)
	w.mu.Lock()
	after := w.tr.podList()
	w.mu.Unlock()
	writes.Go(func() { w.writePods("pods/"+string(name)+"-after.json", after) })
	endFirst()
	first := <-firstRead
	if !ended || ctx.Err() != nil {
		return withProbed(s, first)
	}
	// The last reading is bounded by the settle time; one that has not returned by then counts as
	// not made.
	lastCtx, endLast := context.WithTimeout(ctx, w.o.Settle)
	defer endLast()
	return withProbed(s, first, w.reading(lastCtx))
}

// follow waits until the open transition's window has ended, and tells the Tracker when the
// command exited. It returns the transition and true then. When ctx ends, it returns the
// transition at that moment: as it ended and true when its window had ended by then, and
// otherwise with Finished zero, Ended at that moment, and false.
func (w *Watcher) follow(ctx context.Context, exited <-chan struct{}) (Transition, bool) {
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			w.mu.Lock()
			now := w.o.Now()
			s, ended := w.tr.State(now)
			w.mu.Unlock()
			if !ended {
				// A window cut short in its settle time has a Finished; a Transition holds no settle
				// time, so Conditions would read it as a finished window.
				s.Finished, s.Ended = time.Time{}, now
			}
			return s, ended
		case <-exited:
			exited = nil // told once; a nil channel is never ready
			w.mu.Lock()
			w.tr.CommandExited(w.o.Now())
			w.noteFinish()
			w.mu.Unlock()
		case <-tick.C:
		case <-w.wake:
		}
		w.mu.Lock()
		now := w.o.Now()
		ended := w.tr.windowEnded(now)
		var s Transition
		if ended {
			s, _ = w.tr.State(now)
		}
		w.mu.Unlock()
		if ended {
			return s, true
		}
	}
}

// windowEnded reports whether the window of the open or last transition has ended by now, and
// ends it when its time has come, as State does. Unlike State it works out no counts and no
// details, so it costs nothing to ask after every change.
func (t *Tracker) windowEnded(now time.Time) bool {
	t.endIfDue(now)
	return !t.windowOpen()
}

// PodsAt answers as the Tracker's PodsAt does.
func (w *Watcher) PodsAt(at time.Time) (stopping, starting []PodNote) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.tr.PodsAt(at)
}

// handler tells the Watcher what one informer sees. kind names the informer's type.
type handler struct {
	w    *Watcher
	kind string
}

func (h handler) OnAdd(obj any, _ bool) { h.w.set(h.kind, nil, obj.(runtime.Object)) }

func (h handler) OnUpdate(old, obj any) { h.w.set(h.kind, old.(runtime.Object), obj.(runtime.Object)) }

func (h handler) OnDelete(obj any) { h.w.gone(h.kind, obj) }

// set tells the Tracker that obj was created or changed. An update whose resource version is old's
// comes only from a list again, because the informers run with no resync: the Tracker is told
// that the watch may have missed changes. That sign is client-go's shared informer, read at
// v0.37.2: an object it already holds comes back as an update with an equal version only after a
// resync, whose period is zero here. An empty version is no sign of one; the API server gives
// every object a version, and only a fake client gives none. An update whose UID differs from
// old's is a new object under the old name: the informer missed a deletion and a creation between
// the two, so old is told gone first.
func (w *Watcher) set(kind string, old, obj runtime.Object) {
	key := objectKey{kind, cache.MetaObjectToName(obj.(metav1.Object)).String()}
	w.mu.Lock()
	now := w.o.Now()
	if old != nil {
		was, is := old.(metav1.Object), obj.(metav1.Object)
		if v := was.GetResourceVersion(); v != "" && v == is.GetResourceVersion() {
			w.tr.Missed(now)
		}
		if was.GetUID() != is.GetUID() {
			w.tr.Gone(old, now)
			w.noteFinish()
		}
	}
	w.tr.Set(obj, now)
	w.noteFinish()
	w.last[key] = obj
	w.mu.Unlock()
	w.signal()
}

// gone tells the Tracker that an object no longer exists. When the informer missed the deletion
// and found it at a later list, obj is a DeletedFinalStateUnknown: the Tracker is told that the
// watch may have missed changes, and the object the tombstone holds is told gone; when it holds
// none, the object last told under its key is. No object was told under the key only when no add
// of it was told, and the informer delivers every object's add before its deletion, so then
// nothing is left to tell.
func (w *Watcher) gone(kind string, obj any) {
	var key string
	d, tombstone := obj.(cache.DeletedFinalStateUnknown)
	if tombstone {
		key, obj = d.Key, d.Obj
	} else {
		key = cache.MetaObjectToName(obj.(metav1.Object)).String()
	}
	k := objectKey{kind, key}
	w.mu.Lock()
	now := w.o.Now()
	if tombstone {
		w.tr.Missed(now)
	}
	o, ok := obj.(runtime.Object)
	if !ok {
		o = w.last[k]
	}
	delete(w.last, k)
	if o != nil {
		w.tr.Gone(o, now)
	}
	w.noteFinish()
	w.mu.Unlock()
	w.signal()
}

// signal wakes InstallReady, InstallSettled or Transition without blocking: a signal already
// waiting stands for this change too, because the Tracker was told it before.
func (w *Watcher) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}
