package run

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
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
}

type fakeTimer struct {
	at time.Time
	ch chan time.Time
}

// Now gives now.
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
}

// fakeCluster is the Cluster that Units.Create returns.
type fakeCluster struct {
	w                                                       *world
	storeErr, deployerErr, watcherErr, targetErr, deleteErr error
	images                                                  map[string]config.ImageRef
	target                                                  probe.Target
	removed                                                 []string
	kept                                                    string
	service                                                 *types.NamespacedName
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

// Watcher records "watcher", runs its hook, keeps the service it was given and
// gives the fake watcher.
func (c *fakeCluster) Watcher(ctx context.Context, cfg *config.Config, service *types.NamespacedName) (Watcher, error) {
	c.w.calls.add("watcher")
	c.w.hook("watcher")
	c.service = service
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

// Probe records "probe", runs its hook and gives the fake probe.
func (c *fakeCluster) Probe(cfg *config.Config, probeImage string, raw io.Writer) probe.Probe {
	c.w.calls.add("probe")
	c.w.hook("probe")
	return c.w.probe
}

// Facts gives the cluster as the record states it, and records nothing.
func (c *fakeCluster) Facts() report.Cluster {
	return report.Cluster{Provider: "kind", ProviderVersion: "v0.33.0", Kubernetes: "v1.37.0",
		NodeImage: "kindest/node:v1.37.0@sha256:ddd", Workers: 3}
}

// Kept gives the sentence for a kept cluster, and records nothing.
func (c *fakeCluster) Kept() string { return c.kept }

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
}

// Fetch records "fetch" and returns the scripted error.
func (d *fakeDeployer) Fetch(ctx context.Context) error {
	d.w.calls.add("fetch")
	d.w.hook("fetch")
	return d.fetchErr
}

// command records name, runs its hook and returns the scripted result, or a
// success when it has none, with the times of the clock.
func (d *fakeDeployer) command(ctx context.Context, name string) deploy.Result {
	d.w.calls.add(name)
	d.w.hook(name)
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

// Transitions gives the two windows of the fake driver.
func (d *fakeDeployer) Transitions() []verdict.WindowName {
	return []verdict.WindowName{verdict.Deploy, verdict.Rollback}
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

// Transition waits until the transition's command has exited or ctx ends, then
// records "transition-" and the window's name and runs its hook. It gives the
// scripted transition with its times filled.
func (f *fakeWatcher) Transition(ctx context.Context, name verdict.WindowName, began time.Time, exited <-chan struct{}) watch.Transition {
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

// PodsAt gives the scripted stopping and starting pods.
func (f *fakeWatcher) PodsAt(at time.Time) (stopping, starting []watch.PodNote) {
	return f.stopping, f.starting
}

// Stop records "watch-stop".
func (f *fakeWatcher) Stop() { f.w.calls.add("watch-stop") }

// fakeProbe is the probe.Probe that fakeCluster.Probe returns.
type fakeProbe struct {
	w        *world
	startErr error
	events   []probe.Event
	ch       chan probe.Event
	once     sync.Once
	facts    probe.Facts
}

// Start records "start", runs its hook and fills a channel with the scripted
// events.
func (p *fakeProbe) Start(ctx context.Context) (probe.Started, error) {
	p.w.calls.add("start")
	p.w.hook("start")
	if p.startErr != nil {
		return probe.Started{}, p.startErr
	}
	p.ch = make(chan probe.Event, len(p.events)+64)
	for _, e := range p.events {
		p.ch <- e
	}
	return probe.Started{First: p.w.clock.Now(), Events: p.ch}, nil
}

// send puts one more event into the probe's channel.
func (p *fakeProbe) send(ev probe.Event) {
	if p.ch != nil {
		p.ch <- ev
	}
}

// Stop records "probe-stop", runs its hook, closes the channel once and gives
// the scripted facts.
func (p *fakeProbe) Stop(ctx context.Context) (probe.Facts, error) {
	p.w.calls.add("probe-stop")
	p.w.hook("probe-stop")
	p.once.Do(func() {
		if p.ch != nil {
			close(p.ch)
		}
	})
	return p.facts, nil
}

// newWorld scripts a run that passes: the probe reports 200 passed checks
// each second from t0 to t0+70 s; the deploy and the rollback each replace
// both pods of deployment/orders, finish 15 s after they begin, and end 30 s
// after.
func newWorld(t *testing.T) *world {
	w := &world{t: t, clock: &fakeClock{now: t0}, calls: &callLog{}, hooks: map[string]func(){},
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
		install: watch.Install{Workloads: []watch.Count{{Workload: orders, Created: 2, Probed: "yes",
			Digests: []string{"sha256:aaa"}}}},
		transitions: map[verdict.WindowName]watch.Transition{verdict.Deploy: replaced, verdict.Rollback: replaced}}
	w.probe = &fakeProbe{w: w, events: passes(0, 70), facts: probe.Facts{Lines: 71, StoppedWhenAsked: true}}
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
	return Run(ctx, Options{ConfigPath: filepath.Join(w.cfg.Dir, "rollproof.yaml"), Out: w.out,
		KeepCluster: w.keep, Stdout: &w.stdout, Stderr: &w.stderr, Version: "test",
		Getenv: func(string) string { return "" }}, w.units())
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
	b, err := os.ReadFile(filepath.Join(w.out, "result.json"))
	if err != nil {
		w.t.Fatalf("read result.json: %v", err)
	}
	var r resultFile
	if err := json.Unmarshal(b, &r); err != nil {
		w.t.Fatalf("decode result.json: %v", err)
	}
	return r
}

// passes is one summary line of 200 passed checks for each second from
// t0+from to t0+to, both included.
func passes(from, to int) []probe.Event {
	var events []probe.Event
	for s := from; s <= to; s++ {
		at := t0.Add(time.Duration(s) * time.Second)
		events = append(events, probe.Event{Obs: &verdict.Observation{Written: at, T: at, OK: true, N: 200, Ms: 30, Lag: 2}})
	}
	return events
}

// failureAt is one refused check that began at t0+at and was written at
// t0+written.
func failureAt(at, written time.Duration) probe.Event {
	return probe.Event{Obs: &verdict.Observation{Written: t0.Add(written), T: t0.Add(at), OK: false, N: 1,
		Class: "refused", Ms: 1}}
}
