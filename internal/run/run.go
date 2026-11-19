package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"rollproof/internal/config"
	"rollproof/internal/deploy"
	"rollproof/internal/probe"
	"rollproof/internal/report"
	"rollproof/internal/verdict"
	"rollproof/internal/watch"
)

// Options is what the run command was given. Version is the tool's own, for the
// record.
type Options struct {
	ConfigPath, Out, Name string
	KeepCluster           bool
	Stdout, Stderr        io.Writer
	Version               string
	Getenv                func(string) string
}

// Clock is the run's source of time, so that a test can drive it.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// Watcher is what the run needs of the watch unit.
type Watcher interface {
	InstallReady(ctx context.Context, began time.Time, exited <-chan struct{}) (watch.Install, error)
	// InstallSettled returns when the install window has ended, or, sooner, when ctx does; the
	// Settling says how it ended. Run calls it once, when the probe's first observation has
	// arrived.
	InstallSettled(ctx context.Context, first time.Time) watch.Settling
	Transition(ctx context.Context, name verdict.WindowName, began time.Time, exited <-chan struct{}) watch.Transition
	// Restart restarts every watched workload as decision 3 says and returns when the
	// last change was taken or the first was refused. run calls it once for a
	// restart's window, beside Transition, and hands its return to Transition as
	// the end of the transition's command.
	Restart(ctx context.Context) watch.Restart
	// UseService names the Service whose endpoints Transition reads.
	UseService(service *types.NamespacedName)
	PodsAt(at time.Time) (stopping, starting []watch.PodNote)
	Stop()
}

// Cluster is a created cluster as the run uses it: each method is one unit's
// step. Deployer also returns the names of the variables taken out of the
// commands' environment. Kept is the sentence for a kept cluster: its
// kubeconfig file and context.
type Cluster interface {
	Store(ctx context.Context) (probeImage string, images map[string]config.ImageRef, err error)
	Deployer(cfg *config.Config) (deploy.Deployer, []string, error)
	// Setup runs the configuration's setup commands in order on the run's command runner, each
	// bounded by limits.timeout, and returns the Result of the first that did not succeed, or a
	// Result named setup with Ending Succeeded when all did or there were none. run calls it once,
	// after Deployer and Fetch and before Watcher; a context that ends stops the command that runs.
	Setup(ctx context.Context) deploy.Result
	Watcher(ctx context.Context, cfg *config.Config) (Watcher, error)
	Target(ctx context.Context, cfg *config.Config) (probe.Target, error)
	Probe(cfg *config.Config, probeImage string, raw io.Writer) probe.Probe
	Facts() report.Cluster
	Kept() string
	// EndCommands ends every command of the run that is still being stopped and closes its
	// evidence, and returns when they are gone. run calls it once, after its last command and
	// before the final result.
	EndCommands()
	Delete(ctx context.Context) error
}

// Units are the steps that other packages perform. main supplies the real ones
// and a test supplies fakes.
type Units struct {
	Load   func(path string) (*config.Config, error)
	Check  func(ctx context.Context, cfg *config.Config) error
	Create func(ctx context.Context, cfg *config.Config, name string, evidence func(string) (io.WriteCloser, error)) (Cluster, error)
	Clock  Clock
}

// The two texts of a condition the tool's own behaviour places.
const (
	signalText = "a signal stopped the run"
	afterText  = "interrupted after the verdict was decided"
)

// windowsBeforeTransitions is how many windows come before the first transition's: the install
// window and the steady window.
const windowsBeforeTransitions = 2

// Run runs one test from the results folder to the exit code: 0 for a pass, 1
// for a fail, 2 for not verified. When ctx ends, the run is interrupted: it
// writes its final result first and leaves the cluster, with the probe's pod in
// it unless a stop of the probe had already begun. The windows of a run are the
// install window, the steady window and one for each transition. The run begins
// a window only when verdict says that it goes on. Once the probe was created,
// the run stops it before the final verdict, whether the run's last window
// ended, the run ended before the probe's first observation or the tool failed,
// unless a signal ended the run. After a failure of the tool, a signal that had
// already come is a reason beside the failure, and a command that still runs is
// stopped before the probe.
func Run(ctx context.Context, o Options, u Units) int {
	s := &runState{o: o, u: u, warnings: &warnings{}}
	s.parent = ctx
	s.ctx, s.cancel = context.WithCancel(ctx)
	defer s.cancel()

	// Step 0: the results folder, and the first result.json in it, come before
	// anything else, so that even a rejected configuration leaves a final file.
	w, err := report.Open(o.Out, o.Getenv)
	if err != nil {
		fmt.Fprintln(o.Stderr, "rollproof: cannot write results: "+err.Error())
		return 2
	}
	s.w = w
	s.ev = &evidence{files: w.Evidence, warn: s.warnings.add}

	caught := s.sequence(s.ctx)
	if caught == nil {
		// A panic in the event reader that no request had handed over is the tool's
		// failure too: the events it did not read are missing from the verdict.
		caught = s.readerPanic()
	}
	if caught != nil {
		s.toolFailed(caught)
		// A signal that came before the failure was handled belongs to the same window
		// (section 10 rule 3), and is no warning after the verdict.
		if !s.signalSeen && s.interrupted(s.parent) {
			s.decideAgain(true)
		}
	}
	// The run's own context ends here, before the probe's stop, so that a command that still runs
	// is stopped first (section 7, "When the tool fails"). The probe's stop keeps the caller's.
	s.cancel()
	if caught != nil {
		s.stopAfterFailure()
	}
	s.endReading()
	s.waitCommands()
	s.endCommands()
	// A reader that failed while it was being ended is the tool's failure too.
	if p := s.readerPanic(); p != nil {
		s.toolFailed(p)
	}
	return s.finish()
}

// writer is what a run asks of the results folder: the final result.json, an evidence file, and
// the folder's path. report.Writer is the real one, and a test replaces it.
type writer interface {
	Final(report.Run) error
	Evidence(name string) (io.WriteCloser, error)
	Dir() string
}

// runState is everything one run knows: the units it has, what each returned,
// and the timeline of its windows. Only the goroutine that reads the probe's
// events, from just before the probe starts, the goroutine of a command, and
// the goroutine that asks the probe to stop touch it besides the run itself.
type runState struct {
	o        Options
	u        Units
	parent   context.Context // the caller's context: a signal
	ctx      context.Context // derived from parent and cancelled when the run ends
	cancel   context.CancelFunc
	w        writer
	ev       *evidence
	warnings *warnings

	cfg        *config.Config
	cluster    Cluster
	deployer   deploy.Deployer
	watcher    Watcher
	probe      probe.Probe
	target     probe.Target
	install    watch.Install
	releases   []deploy.Release
	probeImage string
	images     map[string]config.ImageRef
	removed    []string
	probeFacts probe.Facts
	probeStart probe.Started
	settling   watch.Settling // the install window as watch reported it, for the record
	stop       probeStop      // notAsked until the tool begins to stop the probe
	countsSeen *[2]int        // the probe's lines and JSON text lines from Counts, read once
	names      []verdict.WindowName

	mu          sync.Mutex // guards the timeline, which the event reader extends
	tl          verdict.Timeline
	transitions []transitionFacts
	podsKept    []bool
	reader      *reader
	decision    verdict.Decision
	signalSeen  bool // a signal was already reported as the interrupted reason
	commands    []*command
	clusterSeen *report.Cluster
}

// sequence performs steps 1 to 13 and returns the value of a panic, if one
// stopped it.
func (s *runState) sequence(ctx context.Context) (caught any) {
	defer func() { caught = recover() }()
	if s.setUp(ctx) && s.windows(ctx) {
		s.stopProbe(ctx)
		return nil
	}
	// The run stopped before the probe was stopped: a step before the install
	// window failed, or a signal came. The verdict is still written.
	s.decide(true)
	return nil
}

// setUp performs steps 1 to 9: everything before the install window.
func (s *runState) setUp(ctx context.Context) bool {
	cfg, err := s.u.Load(s.o.ConfigPath)
	if err != nil {
		return s.beforeFail(ctx, err)
	}
	s.cfg = cfg
	if s.interrupted(ctx) {
		return false
	}
	if keys := cfg.NotBuilt(); len(keys) > 0 {
		s.tl.Before = append(s.tl.Before, verdict.Condition{Code: verdict.ConfigRejected,
			Text: "not built yet: " + strings.Join(keys, ", ")})
		return false
	}
	if err := s.u.Check(ctx, cfg); err != nil {
		return s.beforeFail(ctx, err)
	}
	if s.interrupted(ctx) {
		return false
	}
	c, err := s.u.Create(ctx, cfg, s.o.Name, s.ev.funcOf)
	if err != nil {
		return s.beforeFail(ctx, err)
	}
	s.cluster = c
	if s.interrupted(ctx) {
		return false
	}
	probeImage, images, err := c.Store(ctx)
	if err != nil {
		return s.beforeFail(ctx, err)
	}
	if s.interrupted(ctx) {
		return false
	}
	s.probeImage, s.images = probeImage, images
	if err := cfg.Resolve(images); err != nil {
		return s.beforeFail(ctx, verdict.Condition{Code: verdict.InternalError, Text: err.Error()})
	}
	if s.interrupted(ctx) {
		return false
	}
	d, removed, err := c.Deployer(cfg)
	if err != nil {
		return s.beforeFail(ctx, err)
	}
	s.deployer, s.removed = d, removed
	if s.interrupted(ctx) {
		return false
	}
	if err := d.Fetch(ctx); err != nil {
		return s.beforeFail(ctx, err)
	}
	if s.interrupted(ctx) {
		return false
	}
	s.releases = d.Releases()
	// The setup commands run after the charts are fetched and before the watch
	// starts (section 7, "Setup commands").
	res := c.Setup(ctx)
	if s.interrupted(ctx) {
		return false
	}
	if cond, failed := setupCondition(res); failed {
		s.tl.Before = append(s.tl.Before, cond)
		return false
	}
	// Step 6: the watcher, before the install. It needs the probe's target, and
	// so the target's Service, only in a transition.
	wt, err := c.Watcher(ctx, cfg)
	if err != nil {
		return s.beforeFail(ctx, err)
	}
	s.watcher = wt
	if s.interrupted(ctx) {
		return false
	}
	if !s.installStep(ctx) || s.interrupted(ctx) {
		return false
	}
	// Step 8: the probe's target, checked after the install, because the chart's
	// Service exists only once the previous release is installed.
	t, err := c.Target(ctx, cfg)
	if err != nil {
		return s.beforeFail(ctx, err)
	}
	s.target = t
	if s.interrupted(ctx) {
		return false
	}
	s.watcher.UseService(t.Service)
	if s.interrupted(ctx) {
		return false
	}
	return s.startProbe(ctx)
}

// installStep performs step 7: the install and the wait for it to be ready.
func (s *runState) installStep(ctx context.Context) bool {
	began := s.u.Clock.Now()
	installCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	restartOnly := s.restartOnly()
	c := s.command(installCtx, s.deployer.Install, func(res deploy.Result) {
		if _, ok := installCondition(res, restartOnly); ok {
			// A failed install has nothing left to wait for.
			cancel()
		}
	})
	inst, err := s.watcher.InstallReady(installCtx, began, c.exited)
	o := s.await(c)
	if s.interrupted(ctx) {
		return false
	}
	if o.panicked != nil {
		panic(o.panicked)
	}
	cond, gives := installCondition(o.result, restartOnly)
	if !gives && err != nil {
		cond, gives = ofTheCandidate(verdict.AsCondition(err), restartOnly), true
	}
	if !gives {
		s.install = inst
		return true
	}
	s.tl.Before = append(s.tl.Before, cond)
	return false
}

// restartOnly reports whether the deployer's transitions are a restart alone: a run with no
// previous release, whose install installs the candidate (section 4).
func (s *runState) restartOnly() bool {
	return slices.Equal(s.deployer.Transitions(), []verdict.WindowName{verdict.Restart})
}

// startProbe performs step 9: the probe and its first observation. The goroutine that reads the
// probe's events starts before the probe does, so that no event waits for a reader; it is the
// only reader of them until the probe is stopped. A start that failed other than by its
// context has ended the probe itself, and Stop gives the Facts and the error that it kept, also
// after a signal; a start that gave its context's error left the pod as it is (section 6), and
// Stop is not asked.
func (s *runState) startProbe(ctx context.Context) bool {
	p := s.cluster.Probe(s.cfg, s.probeImage, s.ev.file("probe.log"))
	s.probe = p
	s.readEvents(p.Events())
	st, err := p.Start(ctx)
	if err != nil {
		if ctx.Err() == nil || !errors.Is(err, ctx.Err()) {
			s.takeStop(ctx)
		}
		return s.beforeFail(ctx, err)
	}
	if s.interrupted(ctx) {
		return false
	}
	s.probeStart = st
	return true
}

// windows performs steps 10 to 12: the windows of the run, the install window first, each
// judged as it ends. It reports false when a signal ended the run, and true when the probe is to
// be stopped.
func (s *runState) windows(ctx context.Context) bool {
	s.names = s.deployer.Transitions()
	first := s.probeStart.First
	s.tl.Windows = make([]verdict.Window, windowsBeforeTransitions, len(s.names)+windowsBeforeTransitions)
	s.tl.Windows[0] = verdict.Window{Name: verdict.Install, Start: first}
	s.tl.Windows[1] = verdict.Window{Name: verdict.Steady}
	for _, name := range s.names {
		s.tl.Windows = append(s.tl.Windows, verdict.Window{Name: name})
	}
	// Step 10: the windows are fixed. The goroutine that reads the probe's events has run since
	// the probe started, and the events it took before are judged against these windows.

	// Step 11: the install window runs from the first observation until the install has settled
	// or failed, whatever the probe reports meanwhile (section 7). A signal ends the run there,
	// as it does in a transition: the settling's own conditions are not placed.
	st := s.watcher.InstallSettled(ctx, first)
	s.settling = st
	if s.interrupted(ctx) {
		return false
	}
	restartOnly := s.restartOnly()
	for _, c := range st.Before {
		s.tl.Before = append(s.tl.Before, ofTheCandidate(c, restartOnly))
	}
	for _, c := range st.InWindow {
		s.tl.Windows[0].Conditions = append(s.tl.Windows[0].Conditions, ofTheCandidate(c, restartOnly))
	}
	s.tl.Windows[0].End = st.Ended
	if st.Settled.IsZero() && len(st.Before) == 0 && len(st.InWindow) == 0 {
		panic("the install neither settled nor failed")
	}
	if !s.decide(false).GoOn {
		return true
	}

	// The steady window begins when the install has settled, not when the tool looked, and runs
	// to its end, whatever happened in it, unless a signal comes first: a signal stops the run at
	// once (section 10), and limits.steady has no upper bound.
	s.tl.Windows[1].Start = st.Settled
	end := st.Settled.Add(s.cfg.Limits.Steady)
	select {
	case <-s.u.Clock.After(end.Sub(s.u.Clock.Now())):
	case <-ctx.Done():
	}
	if s.interrupted(ctx) {
		return false
	}
	s.tl.Windows[1].End = end
	if !s.decide(false).GoOn {
		return true
	}
	return s.transitionsStep(ctx)
}

// transitionsStep performs step 12: each transition of the deployer, in order,
// until one of them does not pass. A deploy runs the deployer's Deploy, a
// rollback its Rollback, and a restart the watcher's Restart, which stands in
// for the command; a name that is none of the three is the tool's failure.
func (s *runState) transitionsStep(ctx context.Context) bool {
	for i, name := range s.names {
		// Transition i's window follows the install window, the steady window and the
		// transitions before it.
		w := i + windowsBeforeTransitions
		began := s.tl.Windows[w-1].End // no gap between windows (section 4)
		var rs *watch.Restart          // the restart's outcome, for a restart's window
		var fn func(ctx context.Context) deploy.Result
		switch name {
		case verdict.Deploy:
			fn = s.deployer.Deploy
		case verdict.Rollback:
			fn = s.deployer.Rollback
		case verdict.Restart:
			// The restart runs as a command does, so that a panic in it is caught as a command's
			// is; its end is the end of the transition's command (decision 4). Like a command, it is
			// bounded by limits.timeout from its start (section 7, "When a command runs too long").
			rs = &watch.Restart{}
			fn = func(ctx context.Context) deploy.Result {
				bounded, cancel := context.WithTimeout(ctx, s.cfg.Limits.Timeout)
				defer cancel()
				*rs = s.watcher.Restart(bounded)
				return deploy.Result{Name: "restart", Ending: deploy.Succeeded, Began: rs.Began, Ended: rs.Ended}
			}
		default:
			// A name this build cannot start never falls through to another transition.
			s.tl.Before = append(s.tl.Before, verdict.Condition{Code: verdict.InternalError,
				Text: "the tool cannot start a " + string(name) + " transition"})
			return true
		}
		s.tl.Windows[w].Start = began
		s.transitions = append(s.transitions, transitionFacts{tr: watch.Transition{Name: name}})
		s.podsKept = append(s.podsKept, false)
		c := s.command(ctx, fn, nil)
		tr := s.watcher.Transition(ctx, name, began, c.exited)
		o := s.await(c)
		// The command ended before the interrupted check below, so its record carries its ending,
		// or the restart's answer, even when a signal cut the transition (report.Transition).
		last := &s.transitions[len(s.transitions)-1]
		last.tr = tr
		if rs != nil {
			last.restart = rs
		} else {
			last.cmd = o.result
		}
		if s.interrupted(ctx) {
			// The transition that a signal cut has none of its own conditions.
			return false
		}
		if o.panicked != nil {
			panic(o.panicked)
		}
		if rs != nil {
			s.placeRestart(w, tr, *rs)
		} else {
			s.placeTransition(w, tr, o.result)
		}
		if !s.decide(false).GoOn {
			return true
		}
	}
	return true
}

// placeTransition fills one transition's window: its finish and end, and its
// conditions from the watcher and from the command.
func (s *runState) placeTransition(i int, tr watch.Transition, res deploy.Result) {
	w := &s.tl.Windows[i]
	w.Finished, w.End = tr.Finished, tr.Ended
	cmd, cmdGives := commandCondition(res)
	for _, c := range tr.Conditions(res.Ending == deploy.Succeeded) {
		if cmdGives && c.Code == verdict.TransitionStalled && cmd.Code == verdict.TransitionStalled {
			// One stall, told once: the watcher's text and then the command's.
			c.Text += "; " + cmd.Text
			cmdGives = false
		}
		w.Conditions = append(w.Conditions, c)
	}
	if cmdGives {
		w.Conditions = append(w.Conditions, cmd)
	}
}

// placeRestart fills a restart's window: its finish and end, and the watcher's
// conditions. The change counts as the command's success only when it was
// accepted, so that a restart that was not is never judged as a transition
// that replaced no pod. A refusal, which names its workload, adds
// restart-failed. A restart cut short with no workload named was stopped by
// limits.timeout, since a signal's never reaches here: it is a stall, told
// once with the watcher's own as placeTransition tells a command's, and never
// a refusal.
func (s *runState) placeRestart(i int, tr watch.Transition, rs watch.Restart) {
	const atLimit = "the restart was still running at limits.timeout and the tool stopped it"
	w := &s.tl.Windows[i]
	w.Finished, w.End = tr.Finished, tr.Ended
	stalled := !rs.Accepted && rs.Workload == ""
	for _, c := range tr.Conditions(rs.Accepted) {
		if stalled && c.Code == verdict.TransitionStalled {
			// One stall, told once: the watcher's text and then the restart's.
			c.Text += "; " + atLimit
			stalled = false
		}
		w.Conditions = append(w.Conditions, c)
	}
	switch {
	case stalled:
		w.Conditions = append(w.Conditions, verdict.Condition{Code: verdict.TransitionStalled, Text: atLimit})
	case !rs.Accepted && rs.Workload != "":
		w.Conditions = append(w.Conditions, verdict.Condition{Code: verdict.RestartFailed,
			Text: "the API server refused the restart of " + rs.Workload + ": " + rs.Text})
	}
}

// stopProbe performs step 13 when the run's last window has ended, whether or
// not every transition was run: the probe is stopped, its last lines are read, and the
// verdict is decided. A signal that comes while the probe is stopping belongs to
// the last window that began. After a failure of the tool, stopAfterFailure
// stops the probe in the same way.
func (s *runState) stopProbe(ctx context.Context) {
	if s.probe != nil {
		if p := s.haltProbe(ctx); p != nil {
			panic(p)
		}
	}
	if s.reader != nil {
		<-s.reader.done // the probe has closed its events
		s.reader.panicIfAny()
	}
	s.interrupted(ctx) // a signal that came while the probe was stopped
	s.decide(true)
}

// stopAfterFailure stops the probe after a failure of the tool, as stopProbe does, when the
// probe was created, its stop has not begun and no signal ended the run: section 6 stops the
// probe at every end of a run but a signal. Run has ended its own context first, so that a
// command that still runs is stopped before the probe (section 7). The stop's context is the
// caller's, so the run's own cancel does not cut it short and a signal does; Stop bounds its own
// wait by the drain and stopMargin. Its last lines are read while the stop runs, or dropped when
// the event reader is gone, as haltProbe says. A signal that came meanwhile is placed as
// stopProbe places one, and the verdict is decided again with the internal-error that the
// failure placed. A panic in Stop, or one of the reader, is the tool's failure too: it is placed
// as one more internal-error beside the first before that decision. A panic of the stop before
// Stop was called, or of that decision, is placed the same way, with a signal that came and was
// not yet placed, and decideAgain decides once more.
func (s *runState) stopAfterFailure() {
	if s.probe == nil || s.stop != notAsked || s.parent.Err() != nil {
		return
	}
	signal := false // a signal placed here, which only a decision made since holds
	defer func() {
		if p := recover(); p != nil {
			// A panic of the stop before Stop was called, or of the last decision; a reader's panic
			// that it met was taken by then.
			s.place(verdict.Condition{Code: verdict.InternalError, Text: panicText(p)})
			if !s.signalSeen && s.interrupted(s.parent) {
				signal = true
			}
			s.decideAgain(signal)
		}
	}()
	p := s.haltProbe(s.parent)
	if p == nil {
		p = s.readerPanic()
	}
	if p != nil {
		s.place(verdict.Condition{Code: verdict.InternalError, Text: panicText(p)})
	}
	signal = s.interrupted(s.parent) // a signal that came while the probe was stopped
	s.decide(true)
}

// decideAgain decides the verdict again once the tool's first failure was decided, with what was
// placed since. A panic of that decision is the warning that a failure after the verdict was
// decided gives, and the verdict kept is the one decided at the first failure, which already holds
// that failure. signal says that a signal was placed for this decision: when the decision panics,
// the verdict kept does not hold that signal, so it no longer counts as reported, and cleanUp
// reports it as a signal after the verdict.
func (s *runState) decideAgain(signal bool) {
	defer func() {
		if p := recover(); p != nil {
			s.warnings.add("the tool failed after the verdict was decided: " + panicText(p))
			if signal {
				s.signalSeen = false
			}
		}
	}()
	s.decide(true)
}

// haltProbe asks the probe to stop and returns when Stop has returned, with the value of a panic
// in Stop, if one came. The stop counts as the tool's failure from its first step until takeStop
// learns how Stop went, and the probe's events are taken before Stop's goroutine starts: a panic
// there leaves no stop running behind it, and the stop is not begun again. The event reader
// reads the probe's last lines meanwhile. When the reader is gone before Stop has returned,
// because a panic stopped it, the lines are taken here and dropped, so that the probe never waits
// on a channel that nobody reads.
func (s *runState) haltProbe(ctx context.Context) any {
	s.stop = stopFailed
	events := s.probe.Events()
	stopped := make(chan any, 1)
	go func() {
		defer func() { stopped <- recover() }()
		s.takeStop(ctx)
	}()
	var gone <-chan struct{} // the reader's end, until it has come
	if s.reader != nil {
		gone = s.reader.done
	}
	var drop <-chan probe.Event // the probe's events, once no reader takes them
	for {
		select {
		case p := <-stopped:
			return p
		case <-gone:
			gone, drop = nil, events
		case _, open := <-drop:
			if !open {
				drop = nil
			}
		}
	}
}

// probeStop is how the tool's stop of the probe went: not asked because a signal ended the run;
// Stop returned its Facts and no error; Stop returned an error after its context had ended; the
// tool failed while it stopped the probe.
type probeStop int

const (
	notAsked probeStop = iota
	asked
	cutBySignal
	stopFailed
)

// takeStop asks the probe to stop, keeps the Facts it gives, records how the stop went, and
// warns when Stop gave an error. After a start that failed, Stop gives what that stop left. The
// stop counts as the tool's failure from the moment the probe was asked until Stop returns, so
// that a Stop that panicked is recorded as one and is not asked again. An error of Stop after its
// context ended is a signal's doing (cutBySignal); any other, a refused or timed-out delete or a
// start that panicked before its log was followed, is the tool's failure (stopFailed).
func (s *runState) takeStop(ctx context.Context) {
	s.stop = stopFailed
	f, err := s.probe.Stop(ctx)
	s.probeFacts = f
	switch {
	case err == nil:
		s.stop = asked
	case ctx.Err() != nil:
		s.stop = cutBySignal
	}
	if err != nil {
		// Stop's error is the pod delete's, the context's, or that of a start that panicked before
		// its log was followed: the probe's pod may still run.
		s.warnings.add("the probe could not be stopped: " + err.Error() + "; its pod may still be there")
	}
}

// beforeFail places the condition of a step that failed before the install
// window, unless a signal ended the run first, and reports false for the
// caller.
func (s *runState) beforeFail(ctx context.Context, err error) bool {
	if !s.interrupted(ctx) {
		s.tl.Before = append(s.tl.Before, verdict.AsCondition(err))
	}
	return false
}

// interrupted reports whether a signal ended the run, and when it did, places
// the interrupted condition where the step that just returned belongs.
func (s *runState) interrupted(ctx context.Context) bool {
	if ctx.Err() == nil {
		return false
	}
	s.signalSeen = true
	s.place(verdict.Condition{Code: verdict.Interrupted, Text: signalText})
	return true
}

// place puts a condition of the tool's own where section 10 rule 3 puts it: in
// the window in progress, whose end is then the moment of judging; or in Before
// when no window has begun; or in the last window that began.
func (s *runState) place(c verdict.Condition) {
	last := -1
	for i, w := range s.tl.Windows {
		if !w.Start.IsZero() {
			last = i
		}
	}
	if last < 0 {
		s.tl.Before = append(s.tl.Before, c)
		return
	}
	if s.tl.Windows[last].End.IsZero() {
		s.tl.Windows[last].End = s.u.Clock.Now()
	}
	s.tl.Windows[last].Conditions = append(s.tl.Windows[last].Conditions, c)
}

// decide judges everything the run knows, after asking the event reader for
// what waits, and keeps the pods of each window's first failure. final says
// that this is the run's last call, which places the tool's own faults and
// checks that no window was left open.
func (s *runState) decide(final bool) verdict.Decision {
	if h := decideHook; h != nil {
		h()
	}
	s.ask()
	t := s.timeline()
	t.Now = s.u.Clock.Now()
	if s.cfg != nil {
		t.Silence = s.cfg.Limits.Silence
	}
	if final {
		s.moveToolFaults(&t)
	}
	d := verdict.Decide(t)
	if final {
		d = s.passedWithAWindowOpen(t, d)
	}
	// The decision is stored last, so that one that panicked, in Decide or while it kept the pods,
	// leaves the decision before it in place, as decideAgain expects.
	s.keepFirstFailures(d)
	s.decision = d
	return d
}

// moveToolFaults places a failure of the tool that came after the last window
// ended in that window: one that arrives while the probe is being stopped
// belongs to the last window (section 10 rule 3, carry-in T29-1). Only the
// tool's own faults move; any other condition after the last window stays
// where Decide puts it, and is reported as a warning.
func (s *runState) moveToolFaults(t *verdict.Timeline) {
	last := -1
	for i, w := range t.Windows {
		if !w.Start.IsZero() {
			last = i
		}
	}
	if last < 0 || t.Windows[last].End.IsZero() {
		return
	}
	end := t.Windows[last].End
	var kept []verdict.Condition
	for _, c := range t.Timed {
		if c.Code == verdict.InternalError && !c.At.Before(end) {
			t.Windows[last].Conditions = append(t.Windows[last].Conditions, c)
			continue
		}
		kept = append(kept, c)
	}
	t.Timed = kept
}

// passedWithAWindowOpen turns a decision that would pass although a window
// never ran or never ended into not verified. The last call of Decide cannot
// tell an end-of-window call from the end of the run, so the run checks what it
// knows itself (carry-in W1).
func (s *runState) passedWithAWindowOpen(t verdict.Timeline, d verdict.Decision) verdict.Decision {
	if !d.GoOn {
		return d
	}
	changed := false
	for i, w := range t.Windows {
		switch {
		case w.Start.IsZero():
			t.Before = append(t.Before, verdict.Condition{Code: verdict.InternalError,
				Text: "the run ended although the " + string(w.Name) + " window never ran"})
		case w.End.IsZero():
			t.Windows[i].Conditions = append(t.Windows[i].Conditions, verdict.Condition{Code: verdict.InternalError,
				Text: "the run ended while the " + string(w.Name) + " window was still open"})
		default:
			continue
		}
		changed = true
	}
	if !changed {
		return d
	}
	return verdict.Decide(t)
}

// keepFirstFailures keeps, for each transition whose window has a first
// failure, the pods that were stopping and starting at that moment.
func (s *runState) keepFirstFailures(d verdict.Decision) {
	if s.watcher == nil {
		return
	}
	for _, w := range d.Windows {
		if w.FirstFailure.IsZero() {
			continue
		}
		for i := range s.transitions {
			if s.transitions[i].tr.Name != w.Name || s.podsKept[i] {
				continue
			}
			s.transitions[i].stopping, s.transitions[i].starting = s.watcher.PodsAt(w.FirstFailure)
			s.podsKept[i] = true
		}
	}
}

// toolFailed places the condition of a panic that stopped the run, like a
// signal, and decides the verdict that the run reached. Its decision has no
// recover: the decision kept from before the failure may be a pass, and writing
// it would hide the failure. A panic here ends the process with Go's exit 2, and
// the first result.json, which is not final, reads as not verified.
func (s *runState) toolFailed(caught any) {
	s.place(verdict.Condition{Code: verdict.InternalError, Text: panicText(caught)})
	s.decide(true)
}

// finish performs steps 14 to 16: the final result.json, the window marks, the
// console text, the clean-up and the exit code. A panic here after the final
// file was written is a warning: the verdict was decided already. A panic
// before it is the refused write. The clean-up runs on every path, so that a
// report that cannot be written does not leave a cluster behind.
func (s *runState) finish() (code int) {
	refused := false
	written := false // the final result.json is in place
	defer func() {
		s.cleanUpQuietly()
		if !refused {
			code = s.decision.Verdict.ExitCode()
		}
	}()
	defer func() {
		p := recover()
		if p == nil {
			return
		}
		if written {
			s.warnAfterConsole("the tool failed after the verdict was decided: " + panicText(p))
			return
		}
		// A panic before the final file was written leaves the first file, which says that the run
		// did not finish. That is the refused write (section 8): exit 1 only for a decided fail.
		refused = true
		fmt.Fprintln(s.o.Stderr, "rollproof: cannot write results: "+panicText(p))
		if s.decision.Verdict == verdict.Fail {
			code = 1
		} else {
			code = 2
		}
	}()
	if err := s.w.Final(s.record()); err != nil {
		// The decided verdict keeps its exit code (section 8): a window that had
		// already failed is a fail, and everything else is not verified, because a
		// pass whose result.json could not be written cannot be vouched for. The
		// console is not printed: it would claim more than the folder holds.
		refused = true
		fmt.Fprintln(s.o.Stderr, "rollproof: cannot write results: "+err.Error())
		if s.decision.Verdict == verdict.Fail {
			return 1
		}
		return 2
	}
	written = true
	if err := s.writeMarks(); err != nil {
		s.warnings.add("the window marks were not written: " + err.Error())
		if err := s.w.Final(s.record()); err != nil {
			fmt.Fprintln(s.o.Stderr, "rollproof: cannot write results: "+err.Error())
		}
	}
	fmt.Fprint(s.o.Stdout, report.Console(s.record()))
	return s.decision.Verdict.ExitCode()
}

// record returns the report's record of the run so far.
func (s *runState) record() report.Run {
	lines, jsonText := s.probeCounts()
	return record(facts{
		cfg:           s.cfg,
		decision:      s.decision,
		transitions:   s.transitions,
		install:       s.install,
		settling:      s.settling,
		releases:      s.releases,
		probe:         s.probeFacts,
		probeStop:     s.stop,
		probeLines:    lines,
		probeJSONText: jsonText,
		probeBegun:    s.probe != nil,
		target:        s.target,
		images:        s.images,
		removed:       s.removed,
		warnings:      s.warnings.all(),
		cluster:       s.clusterFacts(),
		version:       s.o.Version,
		out:           s.w.Dir(),
	})
}

// clusterFacts is what the cluster says of itself, read once.
func (s *runState) clusterFacts() *report.Cluster {
	if s.cluster != nil && s.clusterSeen == nil {
		c := s.cluster.Facts()
		s.clusterSeen = &c
	}
	return s.clusterSeen
}

// probeCounts is what the probe's Counts gives, for a probe whose stop gave no Facts that the
// record can use: read once, by the first record, which the final result.json is written from, so
// that a later rewrite for a warning gives the same counts. Zeros when there is no probe or its
// stop was asked.
func (s *runState) probeCounts() (lines, jsonText int) {
	if s.probe == nil || s.stop == asked {
		return 0, 0
	}
	if s.countsSeen == nil {
		l, j := s.probe.Counts()
		s.countsSeen = &[2]int{l, j}
	}
	return s.countsSeen[0], s.countsSeen[1]
}

// mark is one window's boundaries in windows.json.
type mark struct {
	Name     string     `json:"name"`
	Start    *time.Time `json:"start"`
	Finished *time.Time `json:"finished"`
	End      *time.Time `json:"end"`
}

// writeMarks writes the windows' boundaries to the evidence folder.
func (s *runState) writeMarks() error {
	marks := make([]mark, 0, len(s.tl.Windows))
	for _, w := range s.tl.Windows {
		marks = append(marks, mark{Name: string(w.Name), Start: timePtr(w.Start),
			Finished: timePtr(w.Finished), End: timePtr(w.End)})
	}
	b, err := json.MarshalIndent(marks, "", "  ")
	if err != nil {
		return err
	}
	f, err := s.w.Evidence("windows.json")
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// cleanUpQuietly performs the clean-up and turns a panic of a unit into the
// warning that a failure after the verdict was decided gives.
func (s *runState) cleanUpQuietly() {
	defer func() {
		if p := recover(); p != nil {
			s.warnAfterConsole("the tool failed after the verdict was decided: " + panicText(p))
		}
	}()
	s.cleanUp()
}

// cleanUp performs step 15: the watcher is stopped, the cluster is deleted
// unless it was asked for or a signal ended the run, and an error is a
// warning.
func (s *runState) cleanUp() {
	if s.watcher != nil {
		s.watcher.Stop()
	}
	switch {
	case s.o.KeepCluster:
		if s.cluster != nil {
			fmt.Fprintln(s.o.Stdout, s.cluster.Kept())
		}
		// A kept cluster does not end the search for a signal.
		if s.parent.Err() != nil {
			s.signalAfterTheVerdict()
		}
	case s.cluster == nil:
		// A run that never made a cluster still ends with the signal that came.
		if s.parent.Err() != nil {
			s.signalAfterTheVerdict()
		}
	case s.parent.Err() != nil:
		s.signalAfterTheVerdict()
	default:
		// A context that no signal ends: the cluster goes even when the caller
		// gives up while it is being deleted. A signal that comes meanwhile is
		// found when Delete returns, whether it failed or not.
		if err := s.cluster.Delete(context.WithoutCancel(s.ctx)); err != nil {
			s.warnAfterConsole("the cluster was not deleted: " + err.Error())
			if s.parent.Err() != nil {
				s.signalAfterTheVerdict()
			}
			return
		}
		if s.parent.Err() != nil {
			s.signalAfterTheVerdict()
		}
	}
}

// signalAfterTheVerdict is a signal that came once the verdict was decided: it
// changes nothing and is a warning (section 10 rule 10).
func (s *runState) signalAfterTheVerdict() {
	if s.signalSeen {
		return // already reported as the window's interrupted reason
	}
	s.warnAfterConsole(afterText)
}

// warnAfterConsole adds a warning that arose once the console text was
// printed, prints its line, and replaces result.json with it. A failure of that
// second write is not allowed to escape: it would panic inside a deferred
// function.
func (s *runState) warnAfterConsole(text string) {
	s.warnings.add(text)
	fmt.Fprintln(s.o.Stdout, "warning: "+text)
	func() {
		defer func() { _ = recover() }()
		if err := s.w.Final(s.record()); err != nil {
			fmt.Fprintln(s.o.Stderr, "rollproof: cannot write results: "+err.Error())
		}
	}()
}

// readerPanic gives the value of a panic that stopped the event reader, once.
func (s *runState) readerPanic() any {
	if s.reader == nil {
		return nil
	}
	return s.reader.take()
}

// panicText is the text of the value a panic carried.
func panicText(caught any) string {
	switch v := caught.(type) {
	case string:
		return v
	case error:
		return v.Error()
	}
	return fmt.Sprint(caught)
}

// timeline is a copy of the run's timeline, taken under the lock that the
// event reader holds.
func (s *runState) timeline() verdict.Timeline {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tl
	t.Before = slices.Clone(s.tl.Before)
	t.Timed = slices.Clone(s.tl.Timed)
	t.Observations = slices.Clone(s.tl.Observations)
	t.Windows = slices.Clone(s.tl.Windows)
	for i := range t.Windows {
		t.Windows[i].Conditions = slices.Clone(s.tl.Windows[i].Conditions)
	}
	return t
}

// warnings holds what went wrong once the verdict could still change, safe for
// the run's goroutines to add to.
type warnings struct {
	mu    sync.Mutex
	lines []string
}

// add keeps one warning.
func (w *warnings) add(text string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lines = append(w.lines, text)
}

// all gives the warnings in order.
func (w *warnings) all() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.lines...)
}

// discardCloser is written to and closed without effect, for evidence that
// cannot be opened.
type discardCloser struct{}

// Write drops what is written and reports it as written.
func (discardCloser) Write(p []byte) (int, error) { return len(p), nil }

func (discardCloser) Close() error { return nil }

// evidence is where the run's evidence files come from. A file that cannot be
// opened or written is a warning and not the tool's failure: the run goes on
// without it.
type evidence struct {
	files func(string) (io.WriteCloser, error)
	warn  func(string)
}

// open opens the evidence file name. A file that cannot be opened gives a
// writer that drops what is written, a nil error, and a warning, so that the
// run and its commands go on without it.
func (e *evidence) open(name string) (io.WriteCloser, error) {
	w, err := e.files(name)
	if err != nil {
		e.warn("the evidence " + name + " was not opened: " + err.Error())
		return discardCloser{}, nil
	}
	return &evidenceFile{w: w, name: name, warn: e.warn}, nil
}

// file opens an evidence file that the run keeps for itself.
func (e *evidence) file(name string) io.WriteCloser {
	w, _ := e.open(name)
	return w
}

// funcOf opens the evidence of a command, in the form that the cluster unit and
// the deploy driver ask for.
func (e *evidence) funcOf(name string) (io.WriteCloser, error) {
	return e.open(name)
}

// evidenceFile is one evidence file, which warns once when it cannot be
// written or closed.
type evidenceFile struct {
	w    io.WriteCloser
	name string
	warn func(string)
	once sync.Once
}

// Write writes to the file and warns when the write failed.
func (f *evidenceFile) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if err != nil {
		f.once.Do(func() { f.warn("the evidence " + f.name + " could not be written: " + err.Error()) })
	}
	return n, err
}

// Close closes the file and warns when that failed.
func (f *evidenceFile) Close() error {
	err := f.w.Close()
	if err != nil {
		f.once.Do(func() { f.warn("the evidence " + f.name + " could not be written: " + err.Error()) })
	}
	return err
}

// command is one command in a goroutine: the channel its outcome comes on and
// the channel that is closed when the command has ended.
type command struct {
	exited chan struct{}
	out    chan outcome
}

// outcome is how one command ended, or the value that its panic carried.
type outcome struct {
	result   deploy.Result
	panicked any
}

// command starts fn in a goroutine. onResult, when set, runs before the outcome
// is handed over. A panic of fn is caught and handed to the caller, so that the
// run can fail in the step the command belongs to.
func (s *runState) command(ctx context.Context, fn func(context.Context) deploy.Result, onResult func(deploy.Result)) *command {
	c := &command{exited: make(chan struct{}), out: make(chan outcome, 1)}
	s.commands = append(s.commands, c)
	go func() {
		defer close(c.exited)
		var o outcome
		defer func() {
			o.panicked = recover()
			c.out <- o
		}()
		o.result = fn(ctx)
		if onResult != nil {
			onResult(o.result)
		}
	}()
	return c
}

// await waits for a command's outcome and stops tracking it.
func (s *runState) await(c *command) outcome {
	o := <-c.out
	for i, x := range s.commands {
		if x == c {
			s.commands = append(s.commands[:i], s.commands[i+1:]...)
			break
		}
	}
	return o
}

// waitCommands waits for every command that a panic left running. The run's
// context has been cancelled first, so each one ends at once.
func (s *runState) waitCommands() {
	for _, c := range s.commands {
		<-c.out
	}
	s.commands = nil
}

// endCommands ends the commands that were still being stopped when the run's last
// command returned, so that none of them outlives the final result. A panic here
// comes after the verdict was decided and is a warning, like any unit's panic at
// that point.
func (s *runState) endCommands() {
	if s.cluster == nil {
		return
	}
	defer func() {
		if p := recover(); p != nil {
			s.warnings.add("the tool failed after the verdict was decided: " + panicText(p))
		}
	}()
	s.cluster.EndCommands()
}

// reader is the goroutine that reads the probe's events into the timeline.
type reader struct {
	events <-chan probe.Event
	add    func(probe.Event)  // the step that puts one event into the timeline
	onAsk  func()             // after a request is taken, before its drain: a test's seam
	ask    chan chan struct{} // a reply channel: the reader drains, then closes it
	stop   chan struct{}      // closed when the run ends
	done   chan struct{}      // closed when the reader has returned
	once   sync.Once

	mu     sync.Mutex
	caught any // the value of a panic that stopped the reader
}

// eventHook, when a test sets it, runs in the event reader on each event before the event is put
// into the timeline: a test's way to make the reader fail, as only a runtime fault could. It is nil
// in the tool.
var eventHook func(probe.Event)

// decideHook, when a test sets it, runs at the start of each decision: a test's way to make a
// decision fail, as only a runtime fault could. It is nil in the tool.
var decideHook func()

// readEvents starts the goroutine that reads the probe's events. startProbe calls it before the
// probe starts, so that the probe's sends have a reader from its first line on.
func (s *runState) readEvents(events <-chan probe.Event) {
	if events == nil {
		return
	}
	add := s.addEvent
	if h := eventHook; h != nil {
		add = func(ev probe.Event) {
			h(ev)
			s.addEvent(ev)
		}
	}
	r := &reader{events: events, add: add, ask: make(chan chan struct{}),
		stop: make(chan struct{}), done: make(chan struct{})}
	s.reader = r
	go s.read(r)
}

// read reads the probe's events until the channel is closed or the run ends. A
// request takes everything that waits, so that Decide sees it. A panic is caught
// and handed to the main flow, and the channels are closed, as a command
// goroutine does (brief step 8, point 5).
func (s *runState) read(r *reader) {
	defer close(r.done)
	defer func() {
		if p := recover(); p != nil {
			r.mu.Lock()
			r.caught = p
			r.mu.Unlock()
		}
	}()
	for {
		select {
		case ev, ok := <-r.events:
			if !ok {
				return
			}
			r.add(ev)
		case reply := <-r.ask:
			if r.onAsk != nil {
				r.onAsk()
			}
			open := s.takeReady(r)
			close(reply)
			if !open {
				return
			}
		case <-r.stop:
			s.takeReady(r)
			return
		}
	}
}

// takeReady takes every event that already waits on the channel, and reports
// whether the channel is still open.
func (s *runState) takeReady(r *reader) bool {
	for {
		select {
		case ev, ok := <-r.events:
			if !ok {
				return false
			}
			r.add(ev)
		default:
			return true
		}
	}
}

// take gives the value of a panic that stopped the reader, once.
func (r *reader) take() any {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.caught
	r.caught = nil
	return p
}

// panicIfAny panics with the value of a panic that stopped the reader, once, so
// that the main flow fails in the step that was waiting for it.
func (r *reader) panicIfAny() {
	if p := r.take(); p != nil {
		panic(p)
	}
}

// addEvent puts one event of the probe into the timeline.
func (s *runState) addEvent(ev probe.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ev.Obs != nil {
		s.tl.Observations = append(s.tl.Observations, *ev.Obs)
	}
	if ev.Fault != nil {
		s.tl.Timed = append(s.tl.Timed, *ev.Fault)
	}
}

// ask has the reader take every event that waits, and waits until it has.
func (s *runState) ask() {
	if s.reader == nil {
		return
	}
	reply := make(chan struct{})
	select {
	case s.reader.ask <- reply:
	case <-s.reader.done:
		s.reader.panicIfAny()
		return
	}
	// A reader that panics while it answers never closes the reply; its done
	// channel ends the wait instead.
	select {
	case <-reply:
	case <-s.reader.done:
	}
	s.reader.panicIfAny()
}

// endReading ends the reader and waits until it has returned. On a run that a
// signal stopped, the reader is ended without the probe's channel being closed.
func (s *runState) endReading() {
	if s.reader == nil {
		return
	}
	s.reader.once.Do(func() { close(s.reader.stop) })
	<-s.reader.done
}
