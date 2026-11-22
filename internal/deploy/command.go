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
// shell. MaxStdout is the most bytes of standard output that Result.Stdout
// keeps; zero means the runner's default of 1 MiB.
type Command struct {
	Name      string
	Args      []string
	Timeout   time.Duration
	MaxStdout int
}

// Result is how one command went. Name is the Name of the command it is the
// result of. ExitCode is -1 unless the command exited by itself. Stdout is
// what it wrote to standard output, at most Command.MaxStdout bytes of it, or 1 MiB
// when that is zero, for commands whose answer the tool reads.
type Result struct {
	Name         string
	Ending       Ending
	ExitCode     int
	Began, Ended time.Time
	Stdout       string
}

// Runner starts one command and waits for it. When ctx ends, the command is
// stopped and Run returns at once, with StoppedAtLimit if the command's limit
// had already fired and StoppedBySignal otherwise.
type Runner interface {
	Run(ctx context.Context, c Command) Result
}

// RunnerOptions is how every command of a run is started: its folder, the
// test cluster's kubeconfig, the environment to start from, and where each
// command's output is kept. Kubeconfig must be an absolute path: a command is
// not started without one. When Dir is set, the command's PWD is the absolute
// path of Dir, and a Dir that has none keeps the command from starting. Run
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

// stdoutCap is how much standard output Result keeps when a command sets no MaxStdout.
const stdoutCap = 1 << 20

// helmActingVariables are the variables by which Helm would act on another cluster or in another
// namespace, or keep its release records somewhere else.
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
	dirErr     error
	env        []string
	removed    []string
	evidence   func(name string) (io.WriteCloser, error)

	mu      sync.Mutex
	uses    map[string]int
	pending map[*pendingStop]struct{}
}

// NewRunner returns an OSRunner, a Runner that starts real processes. Each
// gets KUBECONFIG set to o.Kubeconfig and none of the variables by which Helm
// would act on another cluster or in another namespace, or keep its release
// records somewhere else.
func NewRunner(o RunnerOptions) *OSRunner {
	abs, dirErr := o.Dir, error(nil)
	if o.Dir != "" {
		abs, dirErr = filepath.Abs(o.Dir)
	}
	r := &OSRunner{dirErr: dirErr, dir: o.Dir, kubeconfig: o.Kubeconfig, evidence: o.Evidence, uses: map[string]int{}, pending: map[*pendingStop]struct{}{}}
	seen := map[string]bool{}
	for _, entry := range o.Environ {
		name, _, _ := strings.Cut(entry, "=")
		switch {
		case name == "KUBECONFIG", name == "PWD" && o.Dir != "" && dirErr == nil:
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
	if o.Dir != "" && dirErr == nil {
		r.env = append(r.env, "PWD="+abs)
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

// sink is the evidence of one command. The tool's own lines go in through
// Write as they are; each output stream of the command goes in through its
// own stream, which passes on only whole lines, so the two streams interleave
// only at line ends and a password that one stream prints across two reads of
// its pipe reaches the masking writer with no byte of the other stream inside
// it. It remembers the last byte the evidence received and never fails the
// command.
type sink struct {
	mu             sync.Mutex
	w              io.WriteCloser
	last           byte
	closed         bool
	stdout, stderr []byte // each stream's partial line
}

// lineMax is the most a stream's partial line holds: at this size, with no
// newline, it is passed on as it is, so that a stream that never writes a
// newline neither holds all its output in memory nor keeps it from the
// evidence until the command ends.
const lineMax = 64 << 10

// put passes p to the evidence unless the evidence is closed. s.mu is held.
func (s *sink) put(p []byte) {
	if s.closed || len(p) == 0 {
		return
	}
	s.last = p[len(p)-1]
	_, _ = s.w.Write(p)
}

func (s *sink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.put(p)
	return len(p), nil
}

// stream is one output stream of a command. A write is added to the stream's
// partial line, and every complete line, up to and including its last
// newline, is passed to the evidence; a partial line of lineMax bytes is
// passed on as it is. What comes after the evidence is closed, from a process
// the command left behind, is dropped and reported as written: a failed write
// would only stop the copy from draining the pipe.
type stream struct {
	s       *sink
	partial *[]byte // &s.stdout or &s.stderr
}

func (w stream) Write(p []byte) (int, error) {
	s := w.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return len(p), nil
	}
	b := append(*w.partial, p...)
	rest := b
	if i := bytes.LastIndexByte(rest, '\n'); i >= 0 {
		s.put(rest[:i+1])
		rest = rest[i+1:]
	}
	for len(rest) >= lineMax {
		s.put(rest[:lineMax])
		rest = rest[lineMax:]
	}
	*w.partial = append(b[:0], rest...)
	return len(p), nil
}

// finish passes on each stream's partial line, standard output's first, with a
// line end between them when both hold one, then writes the closing line,
// after a newline if the evidence's last byte was none, and closes the
// evidence.
func (s *sink) finish(e Ending, code int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.put(s.stdout)
	if len(s.stdout) > 0 && len(s.stderr) > 0 {
		s.put([]byte("\n"))
	}
	s.put(s.stderr)
	s.stdout, s.stderr = nil, nil
	if s.last != '\n' {
		_, _ = io.WriteString(s.w, "\n")
	}
	_, _ = fmt.Fprintf(s.w, "rollproof: %s, exit code %d\n", e, code)
	_ = s.w.Close()
	s.closed = true
}

// capture keeps the first limit bytes written to it.
type capture struct {
	mu    sync.Mutex
	limit int
	buf   bytes.Buffer
}

func (c *capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if room := c.limit - c.buf.Len(); room > 0 {
		c.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

func (c *capture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// stdoutWriter sends standard output to its stream of the evidence and to the
// capture, which sees the bytes as they come.
type stdoutWriter struct {
	s stream
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
// command's evidence is closed, then returns. Each wait is bounded by twice the
// wait delay: the time for the SIGKILL to end the group, and then the time Go
// waits for the output to close, which a process outside the group (one that
// called setsid) may hold open. A group whose command exited by itself is not touched. Call it once,
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
		case <-time.After(2 * p.delay): // SIGKILL to the group gone, then Go's own WaitDelay
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
	case r.dirErr != nil:
		return notStarted(r.dirErr)
	case len(c.Args) == 0:
		return notStarted(errors.New("no program"))
	}
	if ctx.Err() != nil {
		res.Ending, res.Ended = StoppedBySignal, time.Now()
		s.finish(res.Ending, res.ExitCode)
		return res
	}

	out := &capture{limit: stdoutCap}
	if c.MaxStdout > 0 {
		out.limit = c.MaxStdout
	}
	cmd := exec.Command(c.Args[0], c.Args[1:]...)
	cmd.Dir = r.dir
	cmd.Env = r.env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = delay
	cmd.Stdout = stdoutWriter{stream{s, &s.stdout}, out}
	cmd.Stderr = stream{s, &s.stderr}
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
