package run

import (
	"context"
	"encoding/json"
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
	Transition(ctx context.Context, name verdict.WindowName, began time.Time, exited <-chan struct{}) watch.Transition
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

// Run runs one test from the results folder to the exit code: 0 for a pass, 1
// for a fail, 2 for not verified. When ctx ends, the run is interrupted: it
// writes its final result first and leaves the cluster.
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
	}
	s.cancel()
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
// events and the goroutine of a command touch it besides the run itself.
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
	stopped    bool
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
	// The run stopped before the probe was stopped: a step before the steady
	// window failed, or a signal came. The verdict is still written.
	s.decide(true)
	return nil
}

// setUp performs steps 1 to 9: everything before the steady window.
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
	c := s.command(installCtx, s.deployer.Install, func(res deploy.Result) {
		if _, ok := installCondition(res); ok {
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
	cond, gives := installCondition(o.result)
	if !gives && err != nil {
		cond, gives = verdict.AsCondition(err), true
	}
	if !gives {
		s.install = inst
		return true
	}
	s.tl.Before = append(s.tl.Before, cond)
	return false
}

// startProbe performs step 9: the probe and its first observation.
func (s *runState) startProbe(ctx context.Context) bool {
	p := s.cluster.Probe(s.cfg, s.probeImage, s.ev.file("probe.log"))
	s.probe = p
	st, err := p.Start(ctx)
	if err != nil {
		return s.beforeFail(ctx, err)
	}
	if s.interrupted(ctx) {
		return false
	}
	s.probeStart = st
	return true
}

// windows performs steps 10 to 12: the windows of the run, each judged as it
// ends. It reports false when a signal ended the run.
func (s *runState) windows(ctx context.Context) bool {
	s.names = s.deployer.Transitions()
	s.tl.Windows = make([]verdict.Window, 1, len(s.names)+1)
	s.tl.Windows[0] = verdict.Window{Name: verdict.Steady, Start: s.probeStart.First}
	for _, name := range s.names {
		s.tl.Windows = append(s.tl.Windows, verdict.Window{Name: name})
	}
	// Step 10: the windows are fixed, and the goroutine that reads the probe's events starts here.
	// It is the only reader of them until the probe is stopped.
	s.readEvents(s.probeStart.Events)

	// Step 11: the steady window runs to its end, whatever happened in it, unless a signal
	// comes first: a signal stops the run at once (section 10), and limits.steady has no upper
	// bound.
	end := s.probeStart.First.Add(s.cfg.Limits.Steady)
	select {
	case <-s.u.Clock.After(end.Sub(s.u.Clock.Now())):
	case <-ctx.Done():
	}
	if s.interrupted(ctx) {
		return false
	}
	s.tl.Windows[0].End = end
	if !s.decide(false).GoOn {
		return true
	}
	return s.transitionsStep(ctx)
}

// transitionsStep performs step 12: each transition of the deployer, in order,
// until one of them does not pass.
func (s *runState) transitionsStep(ctx context.Context) bool {
	for i, name := range s.names {
		began := s.tl.Windows[i].End // no gap between windows (section 4)
		if name != verdict.Deploy && name != verdict.Rollback {
			// This build has no way to start any other transition: the config
			// rejects the runs that would have one (carry-in T31-2).
			s.tl.Before = append(s.tl.Before, verdict.Condition{Code: verdict.InternalError,
				Text: "the tool cannot start a " + string(name) + " transition"})
			return true
		}
		s.tl.Windows[i+1].Start = began
		s.transitions = append(s.transitions, transitionFacts{tr: watch.Transition{Name: name}})
		s.podsKept = append(s.podsKept, false)
		fn := func(ctx context.Context) deploy.Result { return s.deployer.Deploy(ctx) }
		if name == verdict.Rollback {
			fn = func(ctx context.Context) deploy.Result { return s.deployer.Rollback(ctx) }
		}
		c := s.command(ctx, fn, nil)
		tr := s.watcher.Transition(ctx, name, began, c.exited)
		o := s.await(c)
		// The command ended before the interrupted check below, so its record carries its ending
		// even when a signal cut the transition (report.Transition's command set).
		s.transitions[len(s.transitions)-1].cmd = o.result
		s.transitions[len(s.transitions)-1].tr = tr
		if s.interrupted(ctx) {
			// The transition that a signal cut has none of its own conditions.
			return false
		}
		if o.panicked != nil {
			panic(o.panicked)
		}
		s.placeTransition(i+1, tr, o.result)
		if !s.decide(false).GoOn {
			return true
		}
	}
	return true
}

// placeTransition fills one transition's window: its finish and end, its
// conditions from the watcher and from the command, and the pods that were
// stopping and starting at its first failure.
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

// stopProbe performs step 13: the probe is stopped, its last lines are read,
// and the verdict is decided. A signal that comes while the probe is stopping
// belongs to the last window that began.
func (s *runState) stopProbe(ctx context.Context) {
	if s.probe != nil {
		f, err := s.probe.Stop(ctx)
		s.probeFacts = f
		s.stopped = true
		if err != nil {
			// Stop's error is the pod delete's, or the context's: the probe's pod may still run.
			s.warnings.add("the probe could not be stopped: " + err.Error() + "; its pod may still be there")
		}
	}
	if s.reader != nil {
		<-s.reader.done // the probe has closed its events
		s.reader.panicIfAny()
	}
	s.interrupted(ctx) // a signal that came while the probe was stopped
	s.decide(true)
}

// beforeFail places the condition of a step that failed before the steady
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
	s.decision = d
	s.keepFirstFailures(d)
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
// signal, and decides the verdict that the run reached.
func (s *runState) toolFailed(caught any) {
	s.place(verdict.Condition{Code: verdict.InternalError, Text: panicText(caught)})
	s.decide(true)
}

// finish performs steps 14 to 16: the final result.json, the window marks, the
// console text, the clean-up and the exit code. A panic here is a warning: the
// verdict was decided already. The clean-up runs on every path, so that a
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
	return record(facts{
		cfg:          s.cfg,
		decision:     s.decision,
		transitions:  s.transitions,
		install:      s.install,
		releases:     s.releases,
		probe:        s.probeFacts,
		probeStopped: s.stopped,
		target:       s.target,
		images:       s.images,
		removed:      s.removed,
		warnings:     s.warnings.all(),
		cluster:      s.clusterFacts(),
		version:      s.o.Version,
		out:          s.w.Dir(),
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

// Close does nothing.
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

// readEvents starts the goroutine that reads the probe's events.
func (s *runState) readEvents(events <-chan probe.Event) {
	if events == nil {
		return
	}
	r := &reader{events: events, add: s.addEvent, ask: make(chan chan struct{}),
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
