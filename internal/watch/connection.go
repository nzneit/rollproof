package watch

import (
	"context"
	"sync"
	"sync/atomic"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
)

// connection is what one informer lists and watches through, so that the Watcher sees when the
// informer's watch of the cluster goes down and whether it broke. It tells the Tracker:
//   - down, when a watch stream ends, cleanly or not;
//   - broke, on an error event, on a call to list or watch that fails, and on any list after the
//     first one. The reflector of client-go lists again only after a watch failed, so the changes
//     since the watch went down are seen only by their sum;
//   - up, when a watch is open again: a plain watch call that succeeds, or, for a watch that lists
//     as it starts, the bookmark that ends its first events.
//
// A stream that ends cleanly and is followed by a watch from the last resource version is a
// renewal: the API server sends the changes since that version, so nothing is missed, unless a
// window's end comes due while the watch is down.
type connection struct {
	w       *Watcher
	kind    string
	example runtime.Object // an object of the kind, as an informer is made with
	list    func(context.Context, metav1.ListOptions) (runtime.Object, error)
	watch   func(context.Context, metav1.ListOptions) (watch.Interface, error)
	// listed is set when a list has succeeded: a list after it is a list again.
	listed atomic.Bool
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
		return &connection{w: w, kind: kind, example: example, list: list, watch: watch}
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

// List lists the objects. A first page of a list after the first list is a list again.
func (c *connection) List(ctx context.Context, opts metav1.ListOptions) (runtime.Object, error) {
	if opts.Continue == "" && c.listed.Load() {
		c.w.link(c.kind, linkBroke)
	}
	obj, err := c.list(ctx, opts)
	if err != nil {
		c.w.link(c.kind, linkBroke)
		return nil, err
	}
	c.listed.Store(true)
	return obj, nil
}

// Watch opens a watch stream. One that sends the initial events first is a list, and is up only
// when those have ended.
func (c *connection) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	lists := opts.SendInitialEvents != nil && *opts.SendInitialEvents
	if lists && c.listed.Load() {
		c.w.link(c.kind, linkBroke)
	}
	in, err := c.watch(ctx, opts)
	if err != nil {
		c.w.link(c.kind, linkBroke)
		return nil, err
	}
	if lists {
		c.listed.Store(true)
	} else {
		c.w.link(c.kind, linkUp)
	}
	s := &stream{c: c, in: in, listing: lists, out: make(chan watch.Event),
		stop: make(chan struct{}), ended: make(chan struct{})}
	c.w.streams.Add(1)
	go s.forward()
	return s, nil
}

// stream passes a watch stream's events on to the informer, and tells the connection when the
// stream breaks or ends.
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
			s.c.w.link(s.c.kind, linkBroke)
		case s.listing && e.Type == watch.Bookmark && initialEventsEnd(e.Object):
			s.listing = false
			s.c.w.link(s.c.kind, linkUp)
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
	linkBroke
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
	case linkBroke:
		w.tr.watchBroke(kind, now)
	case linkUp:
		w.tr.watchUp(kind, now)
	}
}
