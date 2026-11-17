package watch

import (
	"context"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
)

// watchTimeout is the timeout that every watch call asks the API server for, so that no routine
// renewal of a watch falls inside a run: a renewal leaves the watch down for a moment, and a
// window in which a watch was down is not verified. A window lasts at most limits.timeout plus
// the settle time, and a job on a hosted runner at most 6 hours; a day is longer than any run.
// The API server takes a client's timeout as given (k8s.io/apiserver v0.37.2,
// pkg/endpoints/handlers/get.go). A test may shorten it.
var watchTimeout = 24 * time.Hour

// connection is what one informer lists and watches through, so that the Watcher sees when the
// informer's watch of the cluster is down. It tells the Tracker:
//   - down, when a watch stream ends, cleanly or not, or brings an error event;
//   - up, when a plain watch call succeeds, or, for a watch that lists as it starts, when the
//     bookmark that ends its first events has passed.
//
// Between the two the informer may list again and its calls may fail; the watch stays down.
type connection struct {
	w       *Watcher
	kind    string
	example runtime.Object // an object of the kind, as an informer is made with
	list    func(context.Context, metav1.ListOptions) (runtime.Object, error)
	watch   func(context.Context, metav1.ListOptions) (watch.Interface, error)
	// firstUp is closed when the watch is up for the first time.
	firstUp chan struct{}
	upOnce  sync.Once
}

// connections returns a connection for each kind that the Watcher follows, each limited to the
// watched namespace.
func (w *Watcher) connections() []*connection {
	ns, apps, core := w.o.Namespace, w.client.AppsV1(), w.client.CoreV1()
	type (
		lister  = func(context.Context, metav1.ListOptions) (runtime.Object, error)
		watcher = func(context.Context, metav1.ListOptions) (watch.Interface, error)
	)
	conn := func(kind string, example runtime.Object, list lister, watch watcher) *connection {
		return &connection{w: w, kind: kind, example: example, list: list, watch: watch,
			firstUp: make(chan struct{})}
	}
	return []*connection{
		conn("Deployment", &appsv1.Deployment{},
			func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) {
				return apps.Deployments(ns).List(ctx, o)
			},
			func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) {
				return apps.Deployments(ns).Watch(ctx, o)
			}),
		conn("StatefulSet", &appsv1.StatefulSet{},
			func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) {
				return apps.StatefulSets(ns).List(ctx, o)
			},
			func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) {
				return apps.StatefulSets(ns).Watch(ctx, o)
			}),
		conn("DaemonSet", &appsv1.DaemonSet{},
			func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) {
				return apps.DaemonSets(ns).List(ctx, o)
			},
			func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) {
				return apps.DaemonSets(ns).Watch(ctx, o)
			}),
		conn("ReplicaSet", &appsv1.ReplicaSet{},
			func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) {
				return apps.ReplicaSets(ns).List(ctx, o)
			},
			func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) {
				return apps.ReplicaSets(ns).Watch(ctx, o)
			}),
		conn("Pod", &corev1.Pod{},
			func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) {
				return core.Pods(ns).List(ctx, o)
			},
			func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) {
				return core.Pods(ns).Watch(ctx, o)
			}),
	}
}

// Watch opens a watch stream with the timeout watchTimeout. A plain watch is up at once; one that
// sends the initial events first is up when those have ended. A call that fails leaves the watch
// as it was.
func (c *connection) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	timeout := int64(watchTimeout / time.Second)
	opts.TimeoutSeconds = &timeout
	lists := opts.SendInitialEvents != nil && *opts.SendInitialEvents
	in, err := c.watch(ctx, opts)
	if err != nil {
		return nil, err
	}
	if !lists {
		c.up()
	}
	s := &stream{c: c, in: in, listing: lists, out: make(chan watch.Event),
		stop: make(chan struct{}), ended: make(chan struct{})}
	c.w.streams.Add(1)
	go s.forward()
	return s, nil
}

// up tells the Tracker that the watch is up, and marks the first time.
func (c *connection) up() {
	c.w.link(c.kind, linkUp)
	c.upOnce.Do(func() { close(c.firstUp) })
}

// stream passes a watch stream's events on to the informer, and tells the Tracker when the stream
// brings an error or ends.
type stream struct {
	c       *connection
	in      watch.Interface
	listing bool // its initial events have not ended yet
	out     chan watch.Event
	stop    chan struct{} // closed by Stop
	ended   chan struct{} // closed when forward has returned
	once    sync.Once
}

func (s *stream) ResultChan() <-chan watch.Event { return s.out }

// Stop stops the stream and returns when the connection has been told that it ended, so that it is
// told before any call that the informer makes next.
func (s *stream) Stop() {
	s.once.Do(func() {
		close(s.stop)
		s.in.Stop()
	})
	<-s.ended
}

// forward passes each event on until the stream ends or is stopped. The end is told before the
// informer sees it, so that it comes before the informer's next call.
func (s *stream) forward() {
	defer close(s.ended)
	defer s.c.w.streams.Add(-1)
	defer close(s.out)
	defer s.c.w.link(s.c.kind, linkDown)
	for {
		var e watch.Event
		var ok bool
		select {
		case e, ok = <-s.in.ResultChan():
		case <-s.stop:
			return
		}
		if !ok {
			return
		}
		switch {
		case e.Type == watch.Error:
			s.c.w.link(s.c.kind, linkDown)
		case s.listing && e.Type == watch.Bookmark && initialEventsEnd(e.Object):
			s.listing = false
			s.c.up()
		}
		select {
		case s.out <- e:
		case <-s.stop:
			return
		}
	}
}

// initialEventsEnd reports whether a bookmark marks the end of a watch's initial events.
func initialEventsEnd(obj runtime.Object) bool {
	m, ok := obj.(metav1.Object)
	return ok && m.GetAnnotations()[metav1.InitialEventsAnnotationKey] == "true"
}

// linkEvent is what a connection tells the Tracker about its watch.
type linkEvent int

const (
	linkDown linkEvent = iota
	linkUp
)

// link tells the Tracker what happened to the watch of one informer, at the clock's time.
func (w *Watcher) link(kind string, e linkEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.o.Now()
	switch e {
	case linkDown:
		w.tr.watchDown(kind, now)
	case linkUp:
		w.tr.watchUp(kind, now)
	}
}
