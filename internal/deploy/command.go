//go:build linux

// Package deploy is the deploy contract as the tool keeps it: the commands the
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
// command's output is kept.
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

// OSRunner is a Runner that starts real processes. Each gets KUBECONFIG set
// to o.Kubeconfig and none of the variables by which Helm would act on
// another cluster.
type OSRunner struct {
	dir      string
	env      []string
	removed  []string
	evidence func(name string) (io.WriteCloser, error)

	mu   sync.Mutex
	uses map[string]int
}

// NewRunner returns an OSRunner that starts every command as o says.
func NewRunner(o RunnerOptions) *OSRunner {
	r := &OSRunner{dir: o.Dir, evidence: o.Evidence, uses: map[string]int{}}
	seen := map[string]bool{}
	for _, entry := range o.Environ {
		name, _, _ := strings.Cut(entry, "=")
		switch {
		case name == "KUBECONFIG":
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
	_, _ = w.s.Write(p)
	return w.c.Write(p)
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

// Run starts c and waits for it. When ctx ends, it stops the command's
// process group and returns at once with StoppedBySignal; the evidence is
// closed when the command has gone.
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
	if len(c.Args) == 0 {
		return notStarted(errors.New("no program"))
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

	pid := cmd.Process.Pid
	exited := make(chan struct{})
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait() // an exec.ErrWaitDelay still means the child exited
		close(exited)
		close(done)
	}()
	stop := func() {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		go func() {
			select {
			case <-exited:
			case <-time.After(kill):
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
		}()
	}

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
	case <-limit:
		stop()
		<-done
		res.Ending = StoppedAtLimit
	case <-ctx.Done():
		stop()
		res.Ending, res.Ended, res.Stdout = StoppedBySignal, time.Now(), out.String()
		go func() {
			<-done
			s.finish(StoppedBySignal, -1)
		}()
		return res
	}
	res.Ended, res.Stdout = time.Now(), out.String()
	s.finish(res.Ending, res.ExitCode)
	return res
}
