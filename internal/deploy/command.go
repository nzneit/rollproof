//go:build linux

// Package deploy is the deploy contract of section 7 as the tool keeps it: the commands the
// tool starts, and the drivers that install, deploy and roll back. It hides
// Helm's command lines and how a command is started, bounded and stopped.
package deploy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Ending is how a command ended: Succeeded "ok", ExitedNonZero "exit-nonzero",
// NotStarted "not-started", StoppedAtLimit "stopped-at-limit" or
// StoppedBySignal "stopped-by-signal".
type Ending string

const (
	Succeeded       Ending = "ok"
	ExitedNonZero   Ending = "exit-nonzero"
	NotStarted      Ending = "not-started"
	StoppedAtLimit  Ending = "stopped-at-limit"
	StoppedBySignal Ending = "stopped-by-signal"
)

// Command is one command to start. Name labels it in the evidence, such as
// install or deploy. Args are the program and its arguments, used with no
// shell.
type Command struct {
	Name    string
	Args    []string
	Timeout time.Duration
}

// Result is how one command went. Name is the Name of the command it is the
// result of. ExitCode is -1 unless the command exited by itself. Stdout is
// what it wrote to standard output, at most 1 MiB of it, for commands whose
// answer the tool reads.
type Result struct {
	Name         string
	Ending       Ending
	ExitCode     int
	Began, Ended time.Time
	Stdout       string
}

// Runner starts one command and waits for it. When ctx ends, the command is
// stopped and Run returns at once with StoppedBySignal.
type Runner interface {
	Run(ctx context.Context, c Command) Result
}

// RunnerOptions is how every command of a run is started: its folder, the
// test cluster's kubeconfig, the environment to start from, and where each
// command's output is kept. Kubeconfig must be an absolute path: a command is
// not started without one. When Dir is set, the command's PWD is Dir. Run
// ignores the errors of the evidence: a failed Evidence call, nil writer or
// failed write or close do not stop the command. A caller who needs them wraps
// the writer that Evidence returns.
type RunnerOptions struct {
	Dir, Kubeconfig string
	Environ         []string
	Evidence        func(name string) (io.WriteCloser, error)
}

// killAfter is how long a command has between SIGTERM and SIGKILL (section 7).
var killAfter = 10 * time.Second

// waitDelay is how long Run waits for the output to close after the child
// exited: a process the child left behind may hold it open.
var waitDelay = time.Second

// stdoutCap is how much of a command's standard output Result keeps.
const stdoutCap = 1 << 20

// helmActingVariables are the variables by which Helm would act on another cluster.
var helmActingVariables = map[string]bool{
	"HELM_KUBECONTEXT":                  true,
	"HELM_KUBEAPISERVER":                true,
	"HELM_KUBETOKEN":                    true,
	"HELM_KUBECAFILE":                   true,
	"HELM_KUBEASUSER":                   true,
	"HELM_KUBEASGROUPS":                 true,
	"HELM_KUBEINSECURE_SKIP_TLS_VERIFY": true,
	"HELM_KUBETLS_SERVER_NAME":          true,
	"HELM_NAMESPACE":                    true,
	"HELM_DRIVER":                       true,
	"HELM_DRIVER_SQL_CONNECTION_STRING": true,
}

// OSRunner is the Runner that NewRunner returns.
type OSRunner struct {
	dir        string
	kubeconfig string
	env        []string
	removed    []string
	evidence   func(name string) (io.WriteCloser, error)

	mu      sync.Mutex
	uses    map[string]int
	pending map[*pendingStop]struct{}
}

// NewRunner returns an OSRunner, a Runner that starts real processes. Each
// gets KUBECONFIG set to o.Kubeconfig and none of the variables by which Helm
// would act on another cluster.
func NewRunner(o RunnerOptions) *OSRunner {
	r := &OSRunner{dir: o.Dir, kubeconfig: o.Kubeconfig, evidence: o.Evidence, uses: map[string]int{}, pending: map[*pendingStop]struct{}{}}
	seen := map[string]bool{}
	for _, entry := range o.Environ {
		name, _, _ := strings.Cut(entry, "=")
		switch {
		case name == "KUBECONFIG", name == "PWD" && filepath.IsAbs(o.Dir):
		case helmActingVariables[name]:
			if !seen[name] {
				seen[name] = true
				r.removed = append(r.removed, name)
			}
		default:
			r.env = append(r.env, entry)
		}
	}
	sort.Strings(r.removed)
	r.env = append(r.env, "KUBECONFIG="+o.Kubeconfig)
	if filepath.IsAbs(o.Dir) {
		r.env = append(r.env, "PWD="+o.Dir)
	}
	return r
}

// Removed returns the names of the variables that were set in o.Environ and
// taken out, sorted.
func (r *OSRunner) Removed() []string {
	return append([]string(nil), r.removed...)
}

type discardCloser struct{}

func (discardCloser) Write(p []byte) (int, error) { return len(p), nil }
func (discardCloser) Close() error                { return nil }

// sink is the evidence of one command: one writer for both output streams,
// which remembers the last byte written and never fails the command.
type sink struct {
	mu     sync.Mutex
	w      io.WriteCloser
	last   byte
	closed bool
}

func (s *sink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(p) > 0 {
		s.last = p[len(p)-1]
	}
	if !s.closed {
		_, _ = s.w.Write(p)
	}
	return len(p), nil
}

// finish writes the closing line, after a newline if the output had none, and
// closes the evidence.
func (s *sink) finish(e Ending, code int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if s.last != '\n' {
		_, _ = io.WriteString(s.w, "\n")
	}
	_, _ = fmt.Fprintf(s.w, "rollproof: %s, exit code %d\n", e, code)
	_ = s.w.Close()
	s.closed = true
}

// capture keeps the first stdoutCap bytes written to it.
type capture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if room := stdoutCap - c.buf.Len(); room > 0 {
		c.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

func (c *capture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// stdoutWriter sends standard output to the evidence and to the capture.
type stdoutWriter struct {
	s *sink
	c *capture
}

func (w stdoutWriter) Write(p []byte) (int, error) {
	n, err := w.c.Write(p)
	_, _ = w.s.Write(p)
	return n, err
}

// commandLine writes the arguments joined by one space, each quoted unless it
// is not empty and holds only safe characters.
func commandLine(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		if a != "" && strings.Trim(a, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./:=@,+%") == "" {
			parts[i] = a
		} else {
			parts[i] = strconv.Quote(a)
		}
	}
	return strings.Join(parts, " ")
}

// evidenceFor opens the evidence of the next use of name.
func (r *OSRunner) evidenceFor(name string) io.WriteCloser {
	r.mu.Lock()
	r.uses[name]++
	file := name
	if n := r.uses[name]; n > 1 {
		file = name + "-" + strconv.Itoa(n)
	}
	r.mu.Unlock()
	if r.evidence == nil {
		return discardCloser{}
	}
	w, err := r.evidence(file)
	if err != nil || w == nil {
		return discardCloser{}
	}
	return w
}

// pendingStop is a command that was sent SIGTERM and whose group is not yet
// known to be gone.
type pendingStop struct {
	killNow chan struct{} // closed by Close
	closed  bool          // killNow is closed; guarded by OSRunner.mu
	gone    chan struct{} // closed when the group is gone and the evidence closed
	delay   time.Duration
}

func groupEnded(pid int) bool { return syscall.Kill(-pid, 0) == syscall.ESRCH }

// stop sends SIGTERM to the group of pid, and starts the work that goes on
// after Run may have returned: SIGKILL to the group kill later unless the group
// is gone, whether or not the direct child has exited, then the closing of the
// evidence with ending e once the child has been waited for.
func (r *OSRunner) stop(pid int, kill, delay time.Duration, done <-chan struct{}, s *sink, e Ending) *pendingStop {
	p := &pendingStop{killNow: make(chan struct{}), gone: make(chan struct{}), delay: delay}
	r.mu.Lock()
	r.pending[p] = struct{}{}
	r.mu.Unlock()
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	go func() {
		defer func() {
			r.mu.Lock()
			delete(r.pending, p)
			r.mu.Unlock()
			close(p.gone)
		}()
		timer := time.NewTimer(kill)
		defer timer.Stop()
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		var giveUp <-chan time.Time
		killed := false
		sendKill := func() {
			if !killed {
				killed = true
				_ = syscall.Kill(-pid, syscall.SIGKILL)
				giveUp = time.After(delay)
			}
		}
	wait:
		for !groupEnded(pid) {
			select {
			case <-tick.C:
			case <-timer.C:
				sendKill()
			case <-p.killNow:
				sendKill()
			case <-giveUp:
				break wait
			}
		}
		<-done
		s.finish(e, -1)
	}()
	return p
}

// Close stops at once every command whose stop is still pending: it sends
// SIGKILL to the command's group, waits until the group is gone and the
// command's evidence is closed, then returns. Each wait is bounded by the wait
// delay. A group whose command exited by itself is not touched. Call it once,
// after the last Run.
func (r *OSRunner) Close() {
	r.mu.Lock()
	var ps []*pendingStop
	for p := range r.pending {
		ps = append(ps, p)
		if !p.closed {
			p.closed = true
			close(p.killNow)
		}
	}
	r.mu.Unlock()
	for _, p := range ps {
		select {
		case <-p.gone:
		case <-time.After(p.delay):
		}
	}
}

// Run starts c and waits for it. When ctx ends, it stops the command's
// process group and returns at once with StoppedBySignal, also when ctx ends
// during the grace after the limit, where the Ending stays StoppedAtLimit
// because the limit came first. The group is sent SIGTERM and, after the kill
// delay, SIGKILL unless it is gone; that and the closing of the evidence go on
// in the background, and Close waits for them. At the limit, Run returns when
// the group is gone.
func (r *OSRunner) Run(ctx context.Context, c Command) Result {
	kill, delay := killAfter, waitDelay
	res := Result{Name: c.Name, ExitCode: -1, Began: time.Now()}
	s := &sink{w: r.evidenceFor(c.Name)}
	_, _ = io.WriteString(s, commandLine(c.Args)+"\n")

	notStarted := func(err error) Result {
		_, _ = io.WriteString(s, "rollproof: could not start: "+strings.ReplaceAll(err.Error(), "\n", " ")+"\n")
		res.Ending, res.Ended = NotStarted, time.Now()
		s.finish(res.Ending, res.ExitCode)
		return res
	}
	switch {
	case r.kubeconfig == "":
		return notStarted(errors.New("no kubeconfig was given"))
	case !filepath.IsAbs(r.kubeconfig):
		return notStarted(errors.New("the kubeconfig path is not absolute"))
	case len(c.Args) == 0:
		return notStarted(errors.New("no program"))
	}
	if ctx.Err() != nil {
		res.Ending, res.Ended = StoppedBySignal, time.Now()
		s.finish(res.Ending, res.ExitCode)
		return res
	}

	out := &capture{}
	cmd := exec.Command(c.Args[0], c.Args[1:]...)
	cmd.Dir = r.dir
	cmd.Env = r.env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = delay
	cmd.Stdout = stdoutWriter{s, out}
	cmd.Stderr = s
	if err := cmd.Start(); err != nil {
		return notStarted(err)
	}

	done := make(chan struct{})
	go func() {
		_ = cmd.Wait() // an exec.ErrWaitDelay still means the child exited
		close(done)
	}()
	pid := cmd.Process.Pid

	var limit <-chan time.Time
	if c.Timeout > 0 {
		t := time.NewTimer(c.Timeout)
		defer t.Stop()
		limit = t.C
	}

	select {
	case <-done:
		res.ExitCode = cmd.ProcessState.ExitCode()
		if res.ExitCode == 0 {
			res.Ending = Succeeded
		} else {
			res.Ending = ExitedNonZero
		}
		res.Ended, res.Stdout = time.Now(), out.String()
		s.finish(res.Ending, res.ExitCode)
	case <-limit:
		p := r.stop(pid, kill, delay, done, s, StoppedAtLimit)
		res.Ending = StoppedAtLimit
		select {
		case <-p.gone:
		case <-ctx.Done():
		}
		res.Ended, res.Stdout = time.Now(), out.String()
	case <-ctx.Done():
		r.stop(pid, kill, delay, done, s, StoppedBySignal)
		res.Ending, res.Ended, res.Stdout = StoppedBySignal, time.Now(), out.String()
	}
	return res
}
