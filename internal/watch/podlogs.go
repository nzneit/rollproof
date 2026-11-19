package watch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

// followPods starts the log followers that the Set of obj makes due: for a pod, those of that
// pod; for a ReplicaSet, those of each pod of it that the Tracker holds, since a pod that came
// before its ReplicaSet had no workload at its own Set. Call it under mu, after the Tracker was
// told obj. It starts nothing when Options.Evidence is nil.
func (w *Watcher) followPods(obj runtime.Object) {
	if w.o.Evidence == nil {
		return
	}
	switch o := obj.(type) {
	case *corev1.Pod:
		w.followPod(o.UID)
	case *appsv1.ReplicaSet:
		for uid, p := range w.tr.pods {
			if p.ownerKind == "ReplicaSet" && p.ownerName == o.Name {
				w.followPod(uid)
			}
		}
	}
}

// followPod starts one follower for each container of the pod's spec, init containers first,
// when the pod is not followed yet, is not gone, and belongs to a workload whose labels matched
// the selector at its last Set. Call it under mu.
func (w *Watcher) followPod(uid types.UID) {
	p := w.tr.pods[uid]
	if w.followed[uid] || p == nil || !p.shown || !p.gone.IsZero() {
		return
	}
	wl, ok := w.tr.podWorkload(p)
	if !ok || w.tr.workloads[wl] == nil || !w.tr.workloads[wl].matched {
		return
	}
	w.followed[uid] = true
	for _, c := range slices.Concat(p.obj.Spec.InitContainers, p.obj.Spec.Containers) {
		f := &logFollower{w: w, uid: uid, pod: p.name, container: c.Name}
		w.logs.Go(func() { f.follow(w.logCtx) })
	}
}

// logFollower writes the log of one container of one pod to pods/POD/CONTAINER.log, and the
// previous run of a restarted container to pods/POD/CONTAINER.previous.log.
type logFollower struct {
	w              *Watcher
	uid            types.UID
	pod, container string
	// file is the current run's file, opened at the first stream or at the final line, whichever
	// comes first, and kept for the pod's life. skipped is set when Evidence could not open it.
	file    *logFile
	skipped bool
}

// follow waits until the container has started, reads its previous run once when it has
// restarted, and follows the current run until the pod is gone or ctx ends. A stream that ends
// while the pod lives is opened again, from the start of a new run or from the second of the last
// line read; an opening that fails is tried again every pollEvery. When the pod is gone and the
// current run was not read to its end, a final "rollproof:" line says why. When ctx ends, the
// file ends where the stream did.
func (f *logFollower) follow(ctx context.Context) {
	defer f.close()
	st, gone := f.status()
	for !started(st) {
		if gone {
			f.end("the pod was gone before the container started")
			return
		}
		if !pause(ctx) {
			return
		}
		st, gone = f.status()
	}
	if st.RestartCount > 0 {
		f.previous(ctx)
	}
	opts := &corev1.PodLogOptions{Container: f.container, Follow: true, Timestamps: true}
	var last time.Time // the timestamp of the last whole line read of the current run
	for {
		restarts := st.RestartCount
		stamp, err := f.read(ctx, opts)
		if ctx.Err() != nil || f.skipped {
			return
		}
		if !stamp.IsZero() {
			last = stamp
		}
		for {
			if !pause(ctx) {
				return
			}
			st, gone = f.status()
			if gone {
				if err != nil {
					f.end(oneLine(err.Error()))
				}
				return
			}
			if err != nil {
				break // the same opening, again
			}
			if st.RestartCount > restarts {
				f.note(fmt.Sprintf("rollproof: the container started again (restart %d)", st.RestartCount))
				opts.SinceTime, last = nil, time.Time{}
				break
			}
			if st.State.Running != nil {
				// With no line read of this run, the run is opened from its start and nothing
				// repeats.
				if !last.IsZero() {
					from := last.Truncate(time.Second)
					f.note("rollproof: the log was opened again from " + from.UTC().Format(time.RFC3339) +
						"; lines of that second may repeat")
					opts.SinceTime = &metav1.Time{Time: from}
				}
				break
			}
			// A terminated run that has not restarted, or one waiting to: wait again.
		}
	}
}

// read opens the current run with opts and copies it into the file until the stream ends. It
// returns the timestamp of the last whole line it read, zero when none, and the opening's error.
func (f *logFollower) read(ctx context.Context, opts *corev1.PodLogOptions) (time.Time, error) {
	stream, err := f.w.client.CoreV1().Pods(f.w.o.Namespace).GetLogs(f.pod, opts).Stream(ctx)
	if err != nil {
		return time.Time{}, err
	}
	defer stream.Close()
	file := f.open()
	if file == nil {
		return time.Time{}, nil
	}
	file.last = time.Time{}
	_, _ = io.Copy(file, stream) // a read that fails ends the stream as its end does
	return file.last, nil
}

// previous reads the previous run of the container once into its own file. When the opening
// fails, or the stream fails before its end, the file ends with a line that says so.
func (f *logFollower) previous(ctx context.Context) {
	stream, err := f.w.client.CoreV1().Pods(f.w.o.Namespace).GetLogs(f.pod,
		&corev1.PodLogOptions{Container: f.container, Previous: true, Timestamps: true}).Stream(ctx)
	if err == nil {
		defer stream.Close()
	}
	if ctx.Err() != nil {
		return
	}
	file := openLog(f.w.o.Evidence, "pods/"+f.pod+"/"+f.container+".previous.log")
	if file == nil {
		return
	}
	defer file.Close()
	if err == nil {
		if _, err = io.Copy(file, stream); ctx.Err() != nil {
			return
		}
	}
	if err != nil {
		file.note(fmt.Sprintf("rollproof: the previous run of container %s could not be read: %s",
			f.container, oneLine(err.Error())))
	}
}

// open returns the current run's file, opening it at the first call. It returns nil, and sets
// skipped, when Evidence cannot open it.
func (f *logFollower) open() *logFile {
	if f.file == nil && !f.skipped {
		f.file = openLog(f.w.o.Evidence, "pods/"+f.pod+"/"+f.container+".log")
		f.skipped = f.file == nil
	}
	return f.file
}

// note writes a line of the tool's own to the current run's file.
func (f *logFollower) note(text string) {
	if file := f.open(); file != nil {
		file.note(text)
	}
}

// end writes the final line of a current run that was not read to its end.
func (f *logFollower) end(why string) {
	f.note(fmt.Sprintf("rollproof: the log of container %s could not be read: %s", f.container, why))
}

func (f *logFollower) close() {
	if f.file != nil {
		_ = f.file.Close()
	}
}

// status gives the container's status in the watch's last object of the pod, zero when that
// object has none, and whether the pod is gone.
func (f *logFollower) status() (corev1.ContainerStatus, bool) {
	f.w.mu.Lock()
	rec := f.w.tr.pods[f.uid]
	p, gone := rec.obj, !rec.gone.IsZero()
	f.w.mu.Unlock()
	for _, s := range slices.Concat(p.Status.InitContainerStatuses, p.Status.ContainerStatuses) {
		if s.Name == f.container {
			return s, gone
		}
	}
	return corev1.ContainerStatus{}, gone
}

// started reports whether a container's status shows it running or terminated: it has a log.
func started(s corev1.ContainerStatus) bool {
	return s.State.Running != nil || s.State.Terminated != nil
}

// pause waits pollEvery of real time, and reports false when ctx ends first.
func pause(ctx context.Context) bool {
	t := time.NewTimer(pollEvery)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// logFile is an evidence file of a log. It ignores its writer's errors, as Options.Evidence
// says, and keeps the timestamp at the start of the last whole line written and whether a line
// is unfinished.
type logFile struct {
	out  io.WriteCloser
	line []byte // the first bytes of the unfinished line, enough for a timestamp
	last time.Time
}

// openLog opens the evidence file name, or returns nil when it cannot be opened.
func openLog(evidence func(string) (io.WriteCloser, error), name string) *logFile {
	out, err := evidence(name)
	if err != nil {
		return nil
	}
	return &logFile{out: out}
}

// stampLen is the length of the longest timestamp the kubelet writes, in RFC 3339 with
// nanoseconds and an offset.
const stampLen = len("2006-01-02T15:04:05.999999999+07:00")

// Write writes p and never fails, so that a copy into it ends only with its stream.
func (l *logFile) Write(p []byte) (int, error) {
	_, _ = l.out.Write(p)
	for _, b := range p {
		if b != '\n' {
			if len(l.line) <= stampLen {
				l.line = append(l.line, b)
			}
			continue
		}
		stamp, _, _ := bytes.Cut(l.line, []byte(" "))
		if t, err := time.Parse(time.RFC3339Nano, string(stamp)); err == nil {
			l.last = t
		}
		l.line = l.line[:0]
	}
	return len(p), nil
}

// note writes text as a line of its own, after a line break when the last line is unfinished.
func (l *logFile) note(text string) {
	if len(l.line) > 0 {
		_, _ = l.Write([]byte("\n"))
	}
	_, _ = l.Write([]byte(text + "\n"))
}

func (l *logFile) Close() error { return l.out.Close() }
