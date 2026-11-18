package probe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"rollproof/internal/cluster"
	"rollproof/internal/verdict"
)

// Started is a probe that has started. First is when its first observation was
// written, by the runtime's stamp. The install window opens there.
type Started struct {
	First time.Time
}

// Facts is what the report says of a probe besides its results: how many lines it wrote, how
// many of them were JSON objects with no ok field, whether it exited inside the drain, and what
// the clock check measured.
type Facts struct {
	Lines, JSONTextLines int
	StoppedWhenAsked     bool
	ClockDifference      time.Duration
}

// Probe is one probe of a run, as run holds it: Events, Start and Stop of Pod, with
// the same contracts.
type Probe interface {
	Events() <-chan Event
	Start(ctx context.Context) (Started, error)
	Stop(ctx context.Context) (Facts, error)
}

// ClockBound is the largest difference, either way, between the runtime's timestamp of the first
// observation and the moment the tool read it.
const ClockBound = 2 * time.Second

// stopMargin is how long Stop waits past the drain for the probe's log to end.
var stopMargin = 5 * time.Second

// PodOptions is what the probe's pod needs: its image by digest, its arguments, the drain,
// limits.timeout for the start, and where every raw line goes with its timestamp.
type PodOptions struct {
	Client              kubernetes.Interface
	Image               string
	Args                []string
	Drain, StartTimeout time.Duration
	Raw                 io.Writer
	Now                 func() time.Time
}

// Pod is a probe that runs as a pod on the control-plane node. It implements Probe. Start and
// Stop are called one after the other, as a run does, not at the same time.
type Pod struct {
	o      PodOptions
	open   logSource  // the API's log of the container named probe; a test replaces it
	events chan Event // what Events gives; made by NewPod
	closed sync.Once  // closes events

	mu      sync.Mutex
	state   lifeStage
	run     *probeRun // the goroutines of the probe that Start started
	facts   Facts     // kept after a Start that failed and after Stop
	stopErr error     // the error of the stop that ended the probe, kept with facts
}

// lifeStage is where a Pod is in its life.
type lifeStage int

const (
	stageNew lifeStage = iota
	stageStarting
	stageStarted
	stageEnded // Start failed, or Stop returned
)

var _ Probe = (*Pod)(nil)

// NewPod returns a probe that runs as a pod on the control-plane node. It implements Probe.
func NewPod(o PodOptions) *Pod {
	p := &Pod{o: o, events: make(chan Event, 1024)}
	p.open = p.apiLog
	return p
}

// Events gives every observation and every fault of the probe, in the log's order,
// from its first line on. Read it from before Start is called until it is
// closed: a send waits for its reader. A Start that failed closes it, and so
// does Stop, also before Start and after a Start that panicked.
func (p *Pod) Events() <-chan Event { return p.events }

// closeEvents closes the channel of Events, once.
func (p *Pod) closeEvents() { p.closed.Do(func() { close(p.events) }) }

// pods is the client of the pods of the probe's namespace.
func (p *Pod) pods() typedcorev1.PodInterface {
	return p.o.Client.CoreV1().Pods(cluster.ProbeNamespace)
}

// apiLog is the logSource of the API: the log of the container named probe, followed, with
// timestamps.
func (p *Pod) apiLog(ctx context.Context, since *time.Time) (io.ReadCloser, error) {
	opts := &corev1.PodLogOptions{Container: "probe", Follow: true, Timestamps: true}
	if since != nil {
		t := metav1.NewTime(*since)
		opts.SinceTime = &t
	}
	return p.pods().GetLogs("probe", opts).Stream(ctx)
}

// running says whether the container named probe still runs: false when the pod is not found or
// the container is terminated, true otherwise, an error of the API included.
func (p *Pod) running(ctx context.Context) bool {
	pod, err := p.pods().Get(ctx, "probe", metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		return false
	case err != nil:
		return true
	}
	return terminated(pod) == nil
}

// terminated is the terminated state of the container named probe, or nil while it has not
// ended.
func terminated(pod *corev1.Pod) *corev1.ContainerStateTerminated {
	for _, c := range pod.Status.ContainerStatuses {
		if c.Name == "probe" {
			return c.State.Terminated
		}
	}
	return nil
}

// exit is how the probe ended, as the API shows it: its pod is gone, or its container ended.
type exit struct {
	gone     bool
	code     int32
	reason   string
	finished time.Time
}

// readExit reads the pod once. ended is false while the container runs and when the API gives
// an error other than "not found"; pod is the pod read, nil when none was.
func (p *Pod) readExit(ctx context.Context) (x exit, ended bool, pod *corev1.Pod) {
	pod, err := p.pods().Get(ctx, "probe", metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		return exit{gone: true}, true, nil
	case err != nil:
		return exit{}, false, nil
	}
	if t := terminated(pod); t != nil {
		return exit{code: t.ExitCode, reason: t.Reason, finished: t.FinishedAt.Time}, true, pod
	}
	return exit{}, false, pod
}

// fault is the ProbeExited fault of an exit after the first observation: at the container's
// finishedAt, or at now when the status gives none or the pod is gone.
func (x exit) fault(now func() time.Time) *verdict.Condition {
	if x.gone {
		return &verdict.Condition{Code: verdict.ProbeExited, At: now(), Text: "the probe's pod is gone"}
	}
	at := x.finished
	if at.IsZero() {
		at = now()
	}
	return &verdict.Condition{Code: verdict.ProbeExited, At: at,
		Text: fmt.Sprintf("the probe exited with exit code %d (%s)", x.code, x.reason)}
}

// beforeStart is the error of Start for an exit before the first observation.
func (x exit) beforeStart() verdict.Condition {
	if x.gone {
		return verdict.Condition{Code: verdict.ProbeExited, Text: "the probe's pod is gone"}
	}
	return verdict.Condition{Code: verdict.ProbeExited,
		Text: fmt.Sprintf("the probe exited before its first observation, with exit code %d (%s)", x.code, x.reason)}
}

// podStateText is the pod's state for the text of ProbeNotStarted. A pod that was never read is
// in the phase Unknown.
func podStateText(pod *corev1.Pod) string {
	if pod == nil {
		return "the pod is " + string(corev1.PodUnknown)
	}
	s := "the pod is " + string(pod.Status.Phase)
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse {
			s += "; not scheduled: " + c.Message
		}
	}
	for _, c := range pod.Status.ContainerStatuses {
		if c.Name == "probe" && c.State.Waiting != nil {
			s += "; container probe is waiting: " + c.State.Waiting.Reason
		}
	}
	return s
}

// probeRun is the two goroutines of a started probe, the follower and the watcher, and what
// they share with Start and Stop.
type probeRun struct {
	events      chan Event         // the Pod's events
	ctx         context.Context    // ends every goroutine of the run and every send
	cancel      context.CancelFunc // ends ctx
	stopWatcher context.CancelFunc // ends the watcher alone

	follower     *follower
	followerDone chan struct{}
	endedAt      time.Time // what the follower's run gave: when its last stream ended; read after followerDone
	watcherDone  chan struct{}

	firstCh, exitCh chan struct{} // closed when the first observation, or an exit before it, is noted

	mu           sync.Mutex
	firstNoted   bool
	first        time.Time     // the runtime's stamp of the first observation
	firstRead    time.Time     // o.Now() when the first observation was read
	exitFirst    *exit         // an exit noted before the first observation
	exitReported bool          // the fault of an exit after the first observation went into events
	lastPod      *corev1.Pod   // the pod as the watcher last read it
	clock        time.Duration // the clock check's difference, once measured
}

// send puts e into events, waiting while the channel is full; it gives up when ctx ends, and
// reports whether e went in. While the channel has room e goes in, also after ctx ended, so that
// what the follower emits when the stop cuts the log, the gap included, reaches the reader.
func (r *probeRun) send(ctx context.Context, e Event) bool {
	select {
	case r.events <- e:
		return true
	default:
	}
	select {
	case r.events <- e:
		return true
	case <-ctx.Done():
		return false
	}
}

// afterRunStarted is called by Start right after it started the follower and the watcher. A
// test replaces it to make Start panic there.
var afterRunStarted = func() {}

// Counts gives how many lines of the probe's log were read so far, and how many of them were
// JSON objects with no ok field. Safe at any moment, from any goroutine, also while Start or Stop
// runs or after either panicked. Zeros before Start.
func (p *Pod) Counts() (lines, jsonText int) {
	p.mu.Lock()
	r := p.run
	p.mu.Unlock()
	if r == nil {
		return 0, 0
	}
	return r.follower.counts()
}

// Start creates the probe's pod and returns at its first observation. The error
// is a Condition when the probe did not start in time, exited first, or
// the nodes' clock differs from the runner's; the probe has then been
// stopped as Stop stops it. When ctx ends, the error is ctx's and the pod
// is left as it is.
//
// A pod named probe that an earlier run left is deleted first. When the probe's pod was never
// created, the error is the API's or ctx's, and the Facts kept say that the probe stopped when
// asked, because nothing ran to stop.
//
// Start is called once, and before Stop: a Start after either gives an error and creates no
// pod. The probe's run is kept from the moment it exists, so that Counts reads it and a Stop
// after a Start that panicked stops the follower and the watcher that Start started.
func (p *Pod) Start(ctx context.Context) (Started, error) {
	p.mu.Lock()
	if p.state != stageNew {
		p.mu.Unlock()
		return Started{}, errors.New("the probe was started before")
	}
	p.state = stageStarting
	p.mu.Unlock()

	timeout := time.NewTimer(p.o.StartTimeout)
	defer timeout.Stop()
	if err := p.replace(ctx, timeout.C); err != nil {
		p.end(Facts{StoppedWhenAsked: true}, nil)
		p.closeEvents()
		return Started{}, err
	}

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	watchCtx, stopWatcher := context.WithCancel(runCtx)
	r := &probeRun{
		events: p.events, ctx: runCtx, cancel: cancel, stopWatcher: stopWatcher,
		followerDone: make(chan struct{}), watcherDone: make(chan struct{}),
		firstCh: make(chan struct{}), exitCh: make(chan struct{}),
	}
	r.follower = newFollower(p.open, p.running, p.o.Raw, p.o.Now, func(e Event) { p.emit(r, e) })
	p.mu.Lock()
	p.run = r
	p.mu.Unlock()
	go func() {
		r.endedAt = r.follower.run(runCtx)
		close(r.followerDone)
	}()
	go func() {
		defer close(r.watcherDone)
		p.watch(watchCtx, r)
	}()
	afterRunStarted()

	var err error
	select {
	case <-r.firstCh:
	case <-r.exitCh:
	case <-timeout.C:
	case <-ctx.Done():
		err = ctx.Err()
	}
	if err != nil {
		// The pod is left as it is: a signal leaves it for the next run to replace.
		cancel()
		<-r.followerDone
		<-r.watcherDone
		p.closeEvents()
		lines, jsonText := r.follower.counts()
		r.mu.Lock()
		clock := r.clock
		r.mu.Unlock()
		p.end(Facts{Lines: lines, JSONTextLines: jsonText, ClockDifference: clock}, nil)
		return Started{}, err
	}
	if err := p.judgeStart(r); err != nil {
		p.halt(ctx, r)
		return Started{}, err
	}
	r.mu.Lock()
	first := r.first
	r.mu.Unlock()
	p.mu.Lock()
	p.state = stageStarted
	p.mu.Unlock()
	return Started{First: first}, nil
}

// replace deletes a pod named probe with a grace period of 0, waits until it is gone, and
// creates the probe's pod. It gives up when timeout fires or ctx ends.
func (p *Pod) replace(ctx context.Context, timeout <-chan time.Time) error {
	zero := int64(0)
	err := p.pods().Delete(ctx, "probe", metav1.DeleteOptions{GracePeriodSeconds: &zero})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	for {
		_, err := p.pods().Get(ctx, "probe", metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return errors.New("the probe pod of an earlier run is still there")
		case <-time.After(pollEvery):
		}
	}
	_, err = p.pods().Create(ctx, podSpec(p.o), metav1.CreateOptions{})
	return err
}

// judgeStart decides Start once its wait has ended other than by its context: an exit before the
// first observation, no observation in time, or the clock check on the first observation.
func (p *Pod) judgeStart(r *probeRun) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case r.exitFirst != nil:
		return r.exitFirst.beforeStart()
	case !r.firstNoted:
		return verdict.Condition{Code: verdict.ProbeNotStarted,
			Text: "no observation within limits.timeout; " + podStateText(r.lastPod)}
	}
	r.clock = r.firstRead.Sub(r.first)
	if r.clock.Abs() > ClockBound {
		return verdict.Condition{Code: verdict.RunnerUnsupported,
			Text: fmt.Sprintf("the nodes' clock differs from the runner's by %v", r.clock.Abs())}
	}
	return nil
}

// emit is the follower's emit: the first observation is noted, with the moment it was read, and
// wakes Start; every event then goes into events.
func (p *Pod) emit(r *probeRun, e Event) {
	if e.Obs != nil {
		r.mu.Lock()
		first := !r.firstNoted
		r.mu.Unlock()
		if first {
			read := p.o.Now()
			r.mu.Lock()
			if !r.firstNoted {
				r.firstNoted, r.first, r.firstRead = true, e.Obs.Written, read
				close(r.firstCh)
			}
			r.mu.Unlock()
		}
	}
	r.send(r.ctx, e)
}

// watch reads the pod every pollEvery until the probe has ended or ctx ends. Before the first
// observation an exit wakes Start; after it, an exit is a fault in events, once. Stop ends the
// watcher before it asks the probe to stop.
func (p *Pod) watch(ctx context.Context, r *probeRun) {
	for {
		x, ended, pod := p.readExit(ctx)
		if pod != nil {
			r.mu.Lock()
			r.lastPod = pod
			r.mu.Unlock()
		}
		if ended && ctx.Err() == nil {
			p.reportExit(ctx, r, x)
			return
		}
		if !pause(ctx, pollEvery) {
			return
		}
	}
}

// reportExit reports an exit that the watcher saw: to Start before the first observation, and
// as a fault in events after it. The send gives up when ctx ends.
func (p *Pod) reportExit(ctx context.Context, r *probeRun, x exit) {
	r.mu.Lock()
	if !r.firstNoted {
		r.exitFirst = &x
		close(r.exitCh)
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()
	if r.send(ctx, Event{Fault: x.fault(p.o.Now)}) {
		r.mu.Lock()
		r.exitReported = true
		r.mu.Unlock()
	}
}

// end keeps f and err as the Facts and the error of a Start that failed or of a Stop.
func (p *Pod) end(f Facts, err error) {
	p.mu.Lock()
	p.state, p.facts, p.stopErr = stageEnded, f, err
	p.mu.Unlock()
}

// Stop deletes the probe's pod with the drain as its grace period, reads its
// lines until the container has exited, and closes Events. After a Start
// that failed, and at a second call, it gives the Facts and the error
// that the stop already made left.
//
// It reads the log for at most the drain and stopMargin, and each call it makes to the API waits
// at most stopMargin. After Start, before it asks the probe to stop, it reads the pod once more,
// so that an exit since the watcher's last reading is still a fault. Before Start it closes
// Events, gives no Facts and no error, and ends the Pod, so that a later Start gives an error.
// After a Start that panicked once its follower and watcher ran, it stops them and the pod as
// after Start, without that last reading.
func (p *Pod) Stop(ctx context.Context) (Facts, error) {
	p.mu.Lock()
	stage, r := p.state, p.run
	if stage == stageEnded {
		f, err := p.facts, p.stopErr
		p.mu.Unlock()
		return f, err
	}
	p.state = stageEnded
	p.mu.Unlock()
	switch {
	case r == nil:
		// Before Start, or after a Start that panicked before its run existed: nothing runs.
		p.closeEvents()
		return Facts{}, nil
	case stage == stageStarted:
		r.stopWatcher()
		<-r.watcherDone
		p.lastExitCheck(ctx, r)
	}
	return p.halt(ctx, r)
}

// halt is the stop of a run whose follower and watcher were started: it ends the watcher,
// deletes the pod with the drain as grace, reads the log until it ends or the drain and
// stopMargin pass, closes Events, keeps the Facts and the error. Each call to the API waits at
// most stopMargin.
//
// It serves Stop, a Start that failed other than by its context, and a Stop after a Start that
// panicked; ending the watcher again after Stop ended it is harmless. The probe stopped when
// asked when its log ended before the ask, or within the drain after it, also when the pod's
// status still read the container as running when the wait ran out. A panic inside halt ends
// the follower and the watcher and closes Events before it goes on.
func (p *Pod) halt(ctx context.Context, r *probeRun) (Facts, error) {
	defer func() {
		// A panic leaves nothing of the run behind: the follower and the watcher end, and
		// Events closes, before the panic goes on.
		if v := recover(); v != nil {
			r.cancel()
			<-r.followerDone
			<-r.watcherDone
			p.closeEvents()
			panic(v)
		}
	}()
	endedBefore := false
	select {
	case <-r.followerDone:
		endedBefore = true
	default:
	}

	r.stopWatcher()
	<-r.watcherDone

	r.follower.stopAsked()
	asked := p.o.Now()
	grace := wholeSeconds(p.o.Drain)
	delCtx, cancelDel := context.WithTimeout(ctx, stopMargin)
	delErr := p.pods().Delete(delCtx, "probe", metav1.DeleteOptions{GracePeriodSeconds: &grace})
	cancelDel()
	if apierrors.IsNotFound(delErr) {
		delErr = nil
	}

	var ctxErr error
	var ranOut time.Time // o.Now() when the wait ran out; zero when it did not
	wait := time.NewTimer(p.o.Drain + stopMargin)
	select {
	case <-r.followerDone:
	case <-wait.C:
		ranOut = p.o.Now()
	case <-ctx.Done():
		ctxErr = ctx.Err()
	}
	wait.Stop()
	r.cancel()
	<-r.followerDone
	p.closeEvents()

	// When the wait ran out, the follower's last stream may have ended inside the drain while the
	// pod's status lagged: the rule then reads when it ended. A stream that the cut itself ended
	// did not end before the wait ran out, whatever the clock says. A stop that its context ended
	// is not judged.
	stopped := ctxErr == nil && (endedBefore || r.endedAt.Sub(asked) < p.o.Drain) &&
		(ranOut.IsZero() || r.endedAt.Before(ranOut))

	lines, jsonText := r.follower.counts()
	r.mu.Lock()
	clock := r.clock
	r.mu.Unlock()
	f := Facts{Lines: lines, JSONTextLines: jsonText, StoppedWhenAsked: stopped, ClockDifference: clock}
	err := delErr
	if ctxErr != nil {
		err = ctxErr
	}
	p.end(f, err)
	return f, err
}

// lastExitCheck reads the pod once, after the watcher has ended, and puts the fault of an exit
// into events when the watcher had not. The reading and the send each wait at most stopMargin,
// or until ctx ends.
func (p *Pod) lastExitCheck(ctx context.Context, r *probeRun) {
	r.mu.Lock()
	reported := r.exitReported
	r.mu.Unlock()
	if reported {
		return
	}
	readCtx, cancelRead := context.WithTimeout(ctx, stopMargin)
	x, ended, _ := p.readExit(readCtx)
	cancelRead()
	if !ended || ctx.Err() != nil {
		return
	}
	limit := time.NewTimer(stopMargin)
	defer limit.Stop()
	select {
	case r.events <- Event{Fault: x.fault(p.o.Now)}:
	case <-limit.C:
	case <-ctx.Done():
	}
}

// podSpec is the probe's pod as Start creates it.
func podSpec(o PodOptions) *corev1.Pod {
	no, yes := false, true
	user := int64(65532)
	grace := wholeSeconds(o.Drain)
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "probe",
			Namespace: cluster.ProbeNamespace,
			Labels:    map[string]string{"app.kubernetes.io/name": "rollproof-probe"},
		},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken: &no,
			Containers: []corev1.Container{{
				Name:            "probe",
				Image:           o.Image,
				Args:            o.Args,
				ImagePullPolicy: corev1.PullIfNotPresent,
				SecurityContext: &corev1.SecurityContext{
					RunAsNonRoot:             &yes,
					RunAsUser:                &user,
					AllowPrivilegeEscalation: &no,
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
					SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
				},
			}},
			NodeSelector:                  map[string]string{"node-role.kubernetes.io/control-plane": ""},
			RestartPolicy:                 corev1.RestartPolicyNever,
			TerminationGracePeriodSeconds: &grace,
			Tolerations: []corev1.Toleration{{
				Key:      "node-role.kubernetes.io/control-plane",
				Operator: corev1.TolerationOpExists,
				Effect:   corev1.TaintEffectNoSchedule,
			}},
		},
	}
}

// wholeSeconds is d in whole seconds, rounded up: the grace period of the drain d.
func wholeSeconds(d time.Duration) int64 {
	return int64((d + time.Second - 1) / time.Second)
}
