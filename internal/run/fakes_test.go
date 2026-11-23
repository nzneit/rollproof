package run

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"rollproof/internal/config"
	"rollproof/internal/deploy"
	"rollproof/internal/probe"
	"rollproof/internal/report"
	"rollproof/internal/verdict"
	"rollproof/internal/watch"
)

// callLog is the order of the calls that the fakes received, safe for use
// from several goroutines.
type callLog struct {
	mu    sync.Mutex
	names []string
}

// add appends a name.
func (c *callLog) add(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.names = append(c.names, name)
}

// list returns a copy of the names in order.
func (c *callLog) list() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.names...)
}

// count says how often a name was added.
func (c *callLog) count(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, x := range c.names {
		if x == name {
			n++
		}
	}
	return n
}

// fakeClock is a Clock that moves only by Advance, which also fires every
// timer that is due.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []fakeTimer
	// jump is how far now moves past each wait that After settles, so that a test can see
	// whether a window begins where the last one ended. Zero by default.
	jump time.Duration
}

type fakeTimer struct {
	at time.Time
	ch chan time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves now by d and sends the new time on the channel (capacity 1) of
// every timer that is due.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	due := c.timers[:0]
	for _, t := range c.timers {
		if t.at.After(c.now) {
			due = append(due, t)
			continue
		}
		select {
		case t.ch <- c.now:
		default:
		}
	}
	c.timers = due
}

// After registers a timer at now plus d and then calls Advance(d) itself, so
// that a run never waits in a test.
func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	ch := make(chan time.Time, 1)
	c.timers = append(c.timers, fakeTimer{at: c.now.Add(d), ch: ch})
	c.mu.Unlock()
	c.Advance(d)
	if c.jump != 0 {
		c.Advance(c.jump)
	}
	return ch
}

// world is one scripted run: the clock, the call log, the hooks, what each
// fake returns, and what the run printed.
type world struct {
	t                            *testing.T
	clock                        *fakeClock
	calls                        *callLog
	hooks                        map[string]func()
	out                          string
	keep                         bool
	cfg                          *config.Config
	loadErr, checkErr, createErr error
	cluster                      *fakeCluster
	deployer                     *fakeDeployer
	watcher                      *fakeWatcher
	probe                        *fakeProbe
	stdout, stderr               bytes.Buffer
	// evidence is the opener of evidence files that the run gave Units.Create, which the real
	// cluster unit hands to the watcher for its pod logs.
	evidence func(string) (io.WriteCloser, error)
	// onStdout, when set, is called after each write to the run's standard output, with what was
	// written.
	onStdout func(p []byte)
	// env is the run's environment: what Options.Getenv gives. A name it does not hold is empty.
	env map[string]string
}

// hookedWriter writes to w and then calls after with what was written.
type hookedWriter struct {
	w     io.Writer
	after func(p []byte)
}

func (h hookedWriter) Write(p []byte) (int, error) {
	n, err := h.w.Write(p)
	h.after(p)
	return n, err
}

// fakeCluster is the Cluster that Units.Create returns.
type fakeCluster struct {
	w                                                       *world
	storeErr, deployerErr, watcherErr, targetErr, deleteErr error
	probeErr                                                error
	images                                                  map[string]config.ImageRef
	setup                                                   deploy.Result
	setupIndex                                              int
	target                                                  probe.Target
	removed                                                 []string
	kept                                                    string
	// rawText is what Probe writes into the probe's raw lines, as the probe's log would; raw is the
	// writer of those lines that Probe was handed, for a test that writes to it later.
	rawText string
	raw     io.Writer
}

// Store records "store", runs its hook and returns the scripted image and map.
func (c *fakeCluster) Store(ctx context.Context) (string, map[string]config.ImageRef, error) {
	c.w.calls.add("store")
	c.w.hook("store")
	return "rollproof.test/rollproof@sha256:fff", c.images, c.storeErr
}

// Deployer records "deployer", runs its hook and gives the fake deployer and
// the names of the variables taken out of the commands' environment.
func (c *fakeCluster) Deployer(cfg *config.Config) (deploy.Deployer, []string, error) {
	c.w.calls.add("deployer")
	c.w.hook("deployer")
	if c.deployerErr != nil {
		return nil, c.removed, c.deployerErr
	}
	return c.w.deployer, c.removed, nil
}

// Setup records "setup", runs its hook and gives the scripted result and
// index, or a success and the index -1 when none was scripted.
func (c *fakeCluster) Setup(ctx context.Context) (deploy.Result, int) {
	c.w.calls.add("setup")
	c.w.hook("setup")
	if c.setup.Ending == "" {
		return deploy.Result{Name: "setup", Ending: deploy.Succeeded}, -1
	}
	return c.setup, c.setupIndex
}

// Watcher records "watcher", runs its hook and gives the fake watcher.
func (c *fakeCluster) Watcher(ctx context.Context, cfg *config.Config) (Watcher, error) {
	c.w.calls.add("watcher")
	c.w.hook("watcher")
	if c.watcherErr != nil {
		return nil, c.watcherErr
	}
	return c.w.watcher, nil
}

// Target records "target", runs its hook and gives the scripted target.
func (c *fakeCluster) Target(ctx context.Context, cfg *config.Config) (probe.Target, error) {
	c.w.calls.add("target")
	c.w.hook("target")
	return c.target, c.targetErr
}

// Probe records "probe", keeps raw, runs its hook, writes rawText into raw when it is set, and gives
// the fake probe and the scripted error.
func (c *fakeCluster) Probe(cfg *config.Config, probeImage string, raw io.Writer) (probe.Probe, error) {
	c.w.calls.add("probe")
	c.raw = raw
	c.w.hook("probe")
	if c.rawText != "" {
		if _, err := io.WriteString(raw, c.rawText); err != nil {
			c.w.t.Errorf("writing the probe's raw lines: %v", err)
		}
	}
	return c.w.probe, c.probeErr
}

// Facts gives the cluster as the record states it, and records nothing.
func (c *fakeCluster) Facts() report.Cluster {
	return report.Cluster{Provider: "kind", ProviderVersion: "v0.33.0", Kubernetes: "v1.37.0",
		NodeImage: "kindest/node:v1.37.0@sha256:ddd", Workers: 3}
}

// Kept gives the sentence for a kept cluster, and records nothing.
func (c *fakeCluster) Kept() string { return c.kept }

// EndCommands records "end-commands", runs its hook, and fails the test when a
// command goroutine is still inside a command: the run must join them first.
func (c *fakeCluster) EndCommands() {
	c.w.calls.add("end-commands")
	if n := c.w.deployer.busy(); n > 0 {
		c.w.t.Errorf("EndCommands was called while %d command goroutines were still running", n)
	}
	c.w.hook("end-commands")
}

// Delete records "delete", runs its hook and returns the scripted error.
func (c *fakeCluster) Delete(ctx context.Context) error {
	c.w.calls.add("delete")
	c.w.hook("delete")
	return c.deleteErr
}

// fakeDeployer is the Deployer that fakeCluster.Deployer returns.
type fakeDeployer struct {
	w        *world
	fetchErr error
	results  map[string]deploy.Result
	releases []deploy.Release
	block    map[string]chan struct{} // commands that wait before they return
	// orCtx names the commands that wait on their block or their context, whichever comes first,
	// as a real command ends when the tool stops it; the others wait on their block alone.
	orCtx map[string]bool
	// transitions is what Transitions gives; nil gives a deploy and then a rollback.
	transitions []verdict.WindowName
	busyN       atomic.Int32 // command goroutines inside a command now

	mu   sync.Mutex
	ctxs map[string]context.Context // the context each command was given
}

// busy is the number of command goroutines that are inside a command now.
func (d *fakeDeployer) busy() int { return int(d.busyN.Load()) }

// cancelled says whether the context that the command name was given has ended. Safe from any
// goroutine.
func (d *fakeDeployer) cancelled(name string) bool {
	d.mu.Lock()
	ctx := d.ctxs[name]
	d.mu.Unlock()
	return ctx != nil && ctx.Err() != nil
}

// Fetch records "fetch" and returns the scripted error.
func (d *fakeDeployer) Fetch(ctx context.Context) error {
	d.w.calls.add("fetch")
	d.w.hook("fetch")
	return d.fetchErr
}

// command records name and its context, runs its hook, waits for the command's block when it has
// one (or for its context, when orCtx names it), and returns the scripted result, or a success
// when it has none, with the times of the clock.
func (d *fakeDeployer) command(ctx context.Context, name string) deploy.Result {
	d.busyN.Add(1)
	defer d.busyN.Add(-1)
	d.mu.Lock()
	if d.ctxs == nil {
		d.ctxs = map[string]context.Context{}
	}
	d.ctxs[name] = ctx
	d.mu.Unlock()
	d.w.calls.add(name)
	d.w.hook(name)
	if hold := d.block[name]; hold != nil {
		if d.orCtx[name] {
			select {
			case <-hold:
			case <-ctx.Done():
			}
		} else {
			<-hold
		}
	}
	res, ok := d.results[name]
	if !ok {
		res = deploy.Result{Ending: deploy.Succeeded}
	}
	now := d.w.clock.Now()
	res.Name, res.Began, res.Ended = name, now, now
	return res
}

// Install records "install" and gives its result.
func (d *fakeDeployer) Install(ctx context.Context) deploy.Result { return d.command(ctx, "install") }

// Deploy records "deploy" and gives its result.
func (d *fakeDeployer) Deploy(ctx context.Context) deploy.Result { return d.command(ctx, "deploy") }

// Rollback records "rollback" and gives its result.
func (d *fakeDeployer) Rollback(ctx context.Context) deploy.Result { return d.command(ctx, "rollback") }

// Transitions gives the scripted transitions, or a deploy and then a rollback when none were
// scripted.
func (d *fakeDeployer) Transitions() []verdict.WindowName {
	if d.transitions == nil {
		return []verdict.WindowName{verdict.Deploy, verdict.Rollback}
	}
	return d.transitions
}

// Releases gives the scripted releases.
func (d *fakeDeployer) Releases() []deploy.Release { return d.releases }

// fakeWatcher is the Watcher that fakeCluster.Watcher returns.
type fakeWatcher struct {
	w                  *world
	install            watch.Install
	installErr         error
	transitions        map[verdict.WindowName]watch.Transition
	stopping, starting []watch.PodNote
	podsAt             []time.Time // every moment PodsAt was asked about
	// service is the Service UseService named, and useCalls the calls made
	// before it: what the test that pins its place reads.
	service  *types.NamespacedName
	useCalls []string
	// before, when set, runs at the start of Transition, before it waits for the
	// command: a test's way to fail while the command is still running.
	before func(verdict.WindowName)
	// settle, when set, gives what InstallSettled returns for the first observation it is handed.
	settle func(first time.Time) watch.Settling
	// late is how far the clock runs past the settling's end before InstallSettled returns, as
	// when the tool looks at the cluster a moment after the install settled.
	late time.Duration
	// restart is what Restart gives, with its times at the clock's; restartPanics makes Restart
	// panic instead; restartWaits makes it wait until its context ends and then give a restart cut
	// short, in the form watch gives one.
	restart       watch.Restart
	restartPanics bool
	restartWaits  bool
	// finishes is what Finishes gives, and finishAt the moments, after the transition began, of
	// each Finish that Transition sends on it before it waits for the command.
	finishes chan watch.Finish
	finishAt map[verdict.WindowName][]time.Duration
	// podEvents is what PodEvents gives. events is what WriteEvents writes, and eventsErr what it
	// returns; eventsWait makes it wait until its context ends and return the context's error
	// instead; eventsCalls is the call log when WriteEvents was called.
	podEvents   []watch.PodEvent
	events      string
	eventsErr   error
	eventsWait  bool
	eventsCalls []string
	// eventsPanic makes WriteEvents panic with "the event list broke" once it kept the call log.
	eventsPanic bool
}

// Finishes gives the scripted channel.
func (f *fakeWatcher) Finishes() <-chan watch.Finish { return f.finishes }

// PodEvents gives the scripted pod events.
func (f *fakeWatcher) PodEvents() []watch.PodEvent { return f.podEvents }

// WriteEvents keeps the call log and runs the hook "write-events", then, with eventsPanic, panics; with eventsWait, waits until
// ctx ends and returns its error; otherwise it writes the scripted events and returns eventsErr.
func (f *fakeWatcher) WriteEvents(ctx context.Context, w io.Writer) error {
	f.eventsCalls = f.w.calls.list()
	f.w.hook("write-events")
	if f.eventsPanic {
		panic("the event list broke")
	}
	if f.eventsWait {
		<-ctx.Done()
		return ctx.Err()
	}
	if _, err := io.WriteString(w, f.events); err != nil {
		return err
	}
	return f.eventsErr
}

// Restart records "restart", runs its hook, and panics with "the restart broke" when
// restartPanics is set. With restartWaits it waits until ctx ends and gives Accepted false with
// ctx's error as its Text and no Workload; otherwise it gives the scripted restart. Began and Ended
// are at the clock's time.
func (f *fakeWatcher) Restart(ctx context.Context) watch.Restart {
	f.w.calls.add("restart")
	f.w.hook("restart")
	if f.restartPanics {
		panic("the restart broke")
	}
	r := f.restart
	if f.restartWaits {
		<-ctx.Done()
		r = watch.Restart{Accepted: false, Text: ctx.Err().Error()}
	}
	now := f.w.clock.Now()
	r.Began, r.Ended = now, now
	return r
}

// InstallSettled records "install-settled" and runs its hook. When ctx has ended it gives a
// settling cut at the clock's time. Otherwise it gives the scripted settling, or one that settles
// limits.settle after first, and moves the clock to the settling's end and late past it.
func (f *fakeWatcher) InstallSettled(ctx context.Context, first time.Time) watch.Settling {
	f.w.calls.add("install-settled")
	f.w.hook("install-settled")
	if ctx.Err() != nil {
		return watch.Settling{First: first, Ended: f.w.clock.Now()}
	}
	end := first.Add(f.w.cfg.Limits.Settle)
	s := watch.Settling{First: first, Settled: end, Ended: end}
	if f.settle != nil {
		s = f.settle(first)
	}
	f.w.clock.Advance(s.Ended.Sub(f.w.clock.Now()) + f.late)
	return s
}

// UseService keeps the Service and the calls made before it.
func (f *fakeWatcher) UseService(service *types.NamespacedName) {
	f.service = service
	f.useCalls = f.w.calls.list()
}

// InstallReady waits until the install's command has exited or ctx ends, then
// records "install-ready" and runs its hook.
func (f *fakeWatcher) InstallReady(ctx context.Context, began time.Time, exited <-chan struct{}) (watch.Install, error) {
	f.wait(ctx, exited)
	f.w.calls.add("install-ready")
	f.w.hook("install-ready")
	if f.installErr != nil {
		return watch.Install{}, f.installErr
	}
	if ctx.Err() != nil {
		return watch.Install{}, ctx.Err()
	}
	return f.install, nil
}

// Transition sends a Finish on finishes for each moment of finishAt[name], then
// waits until the transition's command has exited or ctx ends, then records
// "transition-" and the window's name and runs its hook. It gives the scripted
// transition with its times filled.
func (f *fakeWatcher) Transition(ctx context.Context, name verdict.WindowName, began time.Time, exited <-chan struct{}) watch.Transition {
	if f.before != nil {
		f.before(name)
	}
	for _, d := range f.finishAt[name] {
		f.finishes <- watch.Finish{Name: name, At: began.Add(d)}
	}
	f.wait(ctx, exited)
	f.w.calls.add("transition-" + string(name))
	f.w.hook("transition-" + string(name))
	tr := f.transitions[name]
	tr.Name, tr.Began = name, began
	if ctx.Err() != nil {
		tr.Ended = f.w.clock.Now()
		tr.Finished = time.Time{}
		return tr
	}
	if tr.Stalled {
		tr.Ended = began.Add(f.w.cfg.Limits.Timeout)
		f.w.clock.Advance(f.w.cfg.Limits.Timeout)
		return tr
	}
	tr.Finished = began.Add(15 * time.Second)
	tr.Ended = began.Add(30 * time.Second)
	f.w.clock.Advance(30 * time.Second)
	return tr
}

// wait waits until exited is closed or ctx ends.
func (f *fakeWatcher) wait(ctx context.Context, exited <-chan struct{}) {
	select {
	case <-exited:
	case <-ctx.Done():
	}
}

// PodsAt records the moment it was asked about, runs the hook "pods-at" and gives the scripted
// pods.
func (f *fakeWatcher) PodsAt(at time.Time) (stopping, starting []watch.PodNote) {
	f.podsAt = append(f.podsAt, at)
	f.w.hook("pods-at")
	return f.stopping, f.starting
}

// Stop records "watch-stop" and runs its hook.
func (f *fakeWatcher) Stop() {
	f.w.calls.add("watch-stop")
	f.w.hook("watch-stop")
}

// fakeProbe is the probe.Probe that fakeCluster.Probe returns.
type fakeProbe struct {
	w          *world
	startErr   error
	stopErr    error
	events     []probe.Event
	unbuffered bool // Events gives a channel with no buffer: a send returns only when a reader takes it
	ch         chan probe.Event
	made, once sync.Once
	facts      probe.Facts
	last       []probe.Event // the lines that the drain gives: Stop sends them before it closes the channel
	// linesRead and jsonTextRead are what Counts gives.
	linesRead, jsonTextRead int
	// startWaits makes Start wait, once it has sent the scripted events, until its context ends,
	// and return the context's error, as a Start that a signal cut does.
	startWaits bool
	// eventsPanic makes Events panic once Start has returned.
	eventsPanic bool
	returned    atomic.Bool // Start has returned
}

// Counts gives the scripted counts.
func (p *fakeProbe) Counts() (lines, jsonText int) { return p.linesRead, p.jsonTextRead }

// Events gives the probe's channel, made at the first call: with room for the scripted events and
// 64 more, or with no buffer when unbuffered is set. With eventsPanic, once Start has returned, it
// runs the hook "events" and panics.
func (p *fakeProbe) Events() <-chan probe.Event {
	if p.eventsPanic && p.returned.Load() {
		p.w.hook("events")
		panic("the probe's events broke")
	}
	p.made.Do(func() {
		if p.unbuffered {
			p.ch = make(chan probe.Event)
		} else {
			p.ch = make(chan probe.Event, len(p.events)+64)
		}
	})
	return p.ch
}

// Start records "start", runs its hook and sends every scripted event into the probe's channel.
// With startWaits it then waits until ctx ends, closes the channel, as a Start that failed does,
// and returns ctx's error; with startErr it closes the channel and returns that error.
func (p *fakeProbe) Start(ctx context.Context) (probe.Started, error) {
	defer p.returned.Store(true)
	p.w.calls.add("start")
	p.w.hook("start")
	p.Events()
	for _, e := range p.events {
		p.ch <- e
	}
	if p.startWaits {
		<-ctx.Done()
		p.once.Do(func() { close(p.ch) })
		return probe.Started{}, ctx.Err()
	}
	if p.startErr != nil {
		p.once.Do(func() { close(p.ch) })
		return probe.Started{}, p.startErr
	}
	return probe.Started{First: p.w.clock.Now()}, nil
}

// send puts one more event into the probe's channel.
func (p *fakeProbe) send(ev probe.Event) {
	if p.ch != nil {
		p.ch <- ev
	}
}

// Stop records "probe-stop", runs its hook, sends the drain's lines, closes the channel once and
// gives the scripted facts and error.
func (p *fakeProbe) Stop(ctx context.Context) (probe.Facts, error) {
	p.w.calls.add("probe-stop")
	p.w.hook("probe-stop")
	p.once.Do(func() {
		if p.ch != nil {
			for _, e := range p.last {
				p.ch <- e
			}
			close(p.ch)
		}
	})
	return p.facts, p.stopErr
}

// newWorld scripts a run that passes: the probe's first observation is at
// t0-15 s, and it reports 200 passed checks each second from there to t0+70 s;
// the install settles limits.settle later, at t0, where the steady window
// begins; the deploy and the rollback each replace both pods of
// deployment/orders, finish 15 s after they begin, and end 30 s after.
func newWorld(t *testing.T) *world {
	w := &world{t: t, clock: &fakeClock{now: t0.Add(-15 * time.Second)}, calls: &callLog{}, hooks: map[string]func(){},
		out: t.TempDir(), cfg: testConfig(t)}
	replaced := watch.Transition{Replaced: true, Workloads: []watch.Count{{Workload: orders,
		AtStart: 2, Replaced: 2, Created: 2, QuickestStop: 5200 * time.Millisecond, Probed: "yes",
		Digests: []string{"sha256:aaa"}}}}
	w.cluster = &fakeCluster{w: w,
		images: map[string]config.ImageRef{"orders": {Repository: "rollproof.test/orders", Digest: "sha256:aaa"}},
		target: probe.Target{Service: &types.NamespacedName{Namespace: "orders", Name: "orders"}},
		kept:   "the cluster is kept: kubeconfig /tmp/rollproof-test/kubeconfig, context kind-rollproof-test"}
	w.deployer = &fakeDeployer{w: w, results: map[string]deploy.Result{}, releases: []deploy.Release{
		{Role: "previous", Chart: "orders", Version: "0.1.0", Source: "./chart"},
		{Role: "candidate", Chart: "orders", Version: "0.1.0", Source: "./chart", InlineKeys: []string{"image"}}}}
	w.watcher = &fakeWatcher{w: w,
		install: watch.Install{Ready: t0.Add(-21 * time.Second), Workloads: []watch.Count{{Workload: orders, Created: 2, Probed: "yes",
			Digests: []string{"sha256:aaa"}}}},
		transitions: map[verdict.WindowName]watch.Transition{verdict.Deploy: replaced, verdict.Rollback: replaced}}
	w.probe = &fakeProbe{w: w, events: passes(-15, 70), facts: probe.Facts{Lines: 86, StoppedWhenAsked: true}}
	return w
}

// units gives a Load, a Check and a Create that record "load", "check" and
// "create", call the hook of that name, and return what the world scripts, with
// the fake clock.
func (w *world) units() Units {
	return Units{
		Load: func(path string) (*config.Config, error) {
			w.calls.add("load")
			w.hook("load")
			return w.cfg, w.loadErr
		},
		Check: func(ctx context.Context, cfg *config.Config) error {
			w.calls.add("check")
			w.hook("check")
			return w.checkErr
		},
		Create: func(ctx context.Context, cfg *config.Config, name string, evidence func(string) (io.WriteCloser, error)) (Cluster, error) {
			w.calls.add("create")
			w.evidence = evidence
			w.hook("create")
			if w.createErr != nil {
				return nil, w.createErr
			}
			return w.cluster, nil
		},
		Clock: w.clock,
	}
}

// run calls Run with the world's options and units.
func (w *world) run(ctx context.Context) int {
	var stdout io.Writer = &w.stdout
	if w.onStdout != nil {
		stdout = hookedWriter{w: &w.stdout, after: w.onStdout}
	}
	return Run(ctx, Options{ConfigPath: filepath.Join(w.cfg.Dir, "rollproof.yaml"), Out: w.out,
		KeepCluster: w.keep, Stdout: stdout, Stderr: &w.stderr, Version: "test",
		Getenv: func(k string) string { return w.env[k] }}, w.units())
}

// gitHubFiles makes two empty files, output and summary, in a folder of their own, names them to the
// run as GITHUB_OUTPUT and GITHUB_STEP_SUMMARY with GITHUB_ACTIONS true, and returns their paths.
func (w *world) gitHubFiles(t *testing.T) (output, summary string) {
	t.Helper()
	dir := t.TempDir()
	output, summary = filepath.Join(dir, "output"), filepath.Join(dir, "summary")
	for _, path := range []string{output, summary} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if w.env == nil {
		w.env = map[string]string{}
	}
	w.env["GITHUB_ACTIONS"] = "true"
	w.env["GITHUB_OUTPUT"] = output
	w.env["GITHUB_STEP_SUMMARY"] = summary
	return output, summary
}

// readOutputs reads the outputs file at path, in the delimiter form and in the plain form
// NAME=VALUE, and returns each name's values in the order they were written. A line that is
// neither fails the test.
func readOutputs(t *testing.T, path string) map[string][]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	outs := map[string][]string{}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(b) == 0 {
		lines = nil
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if name, delimiter, ok := strings.Cut(line, "<<"); ok && name != "" && delimiter != "" {
			var value []string
			closed := false
			for i++; i < len(lines); i++ {
				if lines[i] == delimiter {
					closed = true
					break
				}
				value = append(value, lines[i])
			}
			if !closed {
				t.Fatalf("the output %s in %s has no closing delimiter %q:\n%s", name, path, delimiter, b)
			}
			outs[name] = append(outs[name], strings.Join(value, "\n"))
			continue
		}
		if name, value, ok := strings.Cut(line, "="); ok && name != "" {
			outs[name] = append(outs[name], value)
			continue
		}
		t.Fatalf("line %q of %s is no output:\n%s", line, path, b)
	}
	return outs
}

// gitHubRecorder stands for GitHub's files: it adds each write to the world's call log, as
// "outputs NAMES FINALNESS" (the names joined by commas) or "summary FINALNESS", FINALNESS being
// "final" when result.json was final at that moment and "not final" otherwise.
type gitHubRecorder struct{ w *world }

// WriteOutputs adds "outputs NAMES FINALNESS" to the call log.
func (g *gitHubRecorder) WriteOutputs(outs ...report.Output) error {
	names := make([]string, 0, len(outs))
	for _, o := range outs {
		names = append(names, o.Name)
	}
	g.w.calls.add("outputs " + strings.Join(names, ",") + " " + g.finalness())
	return nil
}

// AppendSummary adds "summary FINALNESS" to the call log.
func (g *gitHubRecorder) AppendSummary(text string) error {
	g.w.calls.add("summary " + g.finalness())
	return nil
}

// Summarizes is true: the recorder stands for a step with a file for the summary.
func (g *gitHubRecorder) Summarizes() bool { return true }

// finalness is "final" when result.json is final now, and "not final" otherwise, a file that
// cannot be read among them.
func (g *gitHubRecorder) finalness() string {
	if r, err := g.w.readResult(); err == nil && r.Final {
		return "final"
	}
	return "not final"
}

// hook runs hooks[name] when it is set.
func (w *world) hook(name string) {
	if h := w.hooks[name]; h != nil {
		h()
	}
}

// result reads w.out/result.json and decodes it, and fails the test when it
// cannot.
func (w *world) result() resultFile {
	w.t.Helper()
	r, err := w.readResult()
	if err != nil {
		w.t.Fatal(err)
	}
	return r
}

// resultFromHook is result for a hook that may run off the test's goroutine: it reports a file
// that cannot be read with t.Error, never t.Fatal, and says whether it read the file.
func (w *world) resultFromHook() (resultFile, bool) {
	w.t.Helper()
	r, err := w.readResult()
	if err != nil {
		w.t.Error(err)
		return resultFile{}, false
	}
	return r, true
}

// readResult reads w.out/result.json and decodes it.
func (w *world) readResult() (resultFile, error) {
	b, err := os.ReadFile(filepath.Join(w.out, "result.json"))
	if err != nil {
		return resultFile{}, fmt.Errorf("read result.json: %w", err)
	}
	var r resultFile
	if err := json.Unmarshal(b, &r); err != nil {
		return resultFile{}, fmt.Errorf("decode result.json: %w", err)
	}
	return r, nil
}

// passes is one summary line of 200 passed checks for each second from
// t0+from to t0+to, both included.
func passes(from, to int) []probe.Event {
	var events []probe.Event
	for s := from; s <= to; s++ {
		at := t0.Add(time.Duration(s) * time.Second)
		events = append(events, probe.Event{Obs: &verdict.Observation{Written: at, T: at, OK: true, N: 200, Ms: 30, HasMs: true, Lag: 2, HasLag: true}})
	}
	return events
}

// failureAt is one refused check that began at t0+at and was written at
// t0+written.
func failureAt(at, written time.Duration) probe.Event {
	return probe.Event{Obs: &verdict.Observation{Written: t0.Add(written), T: t0.Add(at), OK: false, N: 1,
		Class: "refused", Ms: 1, HasMs: true}}
}
