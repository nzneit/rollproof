> Part of the plan in `README.md` of this folder. Read that file first: its Global Constraints apply to every task here.

### Task 23: `deploy`: the command runner

**Files:**
- Create: `internal/deploy/command.go`
- Test: `internal/deploy/command_test.go`

**Interfaces:**
- Consumes: nothing from another task but the module.
- Produces: `module internal/deploy`
  The deploy contract of section 7 as the tool keeps it: the commands the
  tool starts, and the drivers that install, deploy and roll back. It hides
  Helm's command lines and how a command is started, bounded and stopped.
- Produces: `type Ending string`
  How a command ended: Succeeded "ok", ExitedNonZero "exit-nonzero",
  NotStarted "not-started", StoppedAtLimit "stopped-at-limit" or
  StoppedBySignal "stopped-by-signal".
- Produces: `type Command struct { Name string; Args []string; Timeout time.Duration }`
  One command to start. Name labels it in the evidence, such as install or
  deploy. Args are the program and its arguments, used with no shell.
- Produces: `type Result struct { Name string; Ending Ending; ExitCode int; Began, Ended time.Time; Stdout string }`
  How one command went. Name is the Name of the command it is the result
  of. ExitCode is -1 unless the command exited by itself. Stdout is what
  it wrote to standard output, at most 1 MiB of it, for commands whose
  answer the tool reads.
- Produces: `type Runner interface { Run(ctx context.Context, c Command) Result }`
  Starts one command and waits for it. When ctx ends, the command is
  stopped and Run returns at once with StoppedBySignal.
- Produces: `type RunnerOptions struct { Dir, Kubeconfig string; Environ []string; Evidence func(name string) (io.WriteCloser, error) }`
  How every command of a run is started: its folder, the test cluster's
  kubeconfig, the environment to start from, and where each command's
  output is kept.
- Produces: `func NewRunner(o RunnerOptions) *OSRunner`
  A Runner that starts real processes. Each gets KUBECONFIG set to
  o.Kubeconfig and none of the variables by which Helm would act on another
  cluster.
- Produces: `func (r *OSRunner) Removed() []string`
  The names of the variables that were set in o.Environ and taken out,
  sorted.

- [ ] **Step 1: Write the failing tests**

Create `internal/deploy/command_test.go`. The tests start real small processes (`sh`, `sleep`, `env`, `pwd`, `head`, `true`); a script writes its own process id, which with `Setpgid` is also its group's id, so that a test can ask whether the group is gone.

```go
//go:build linux

package deploy

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

const testKubeconfig = "/run/rollproof-test/kubeconfig"

// helmVariables are the eleven variables of section 7, sorted.
var helmVariables = []string{
	"HELM_DRIVER", "HELM_DRIVER_SQL_CONNECTION_STRING", "HELM_KUBEAPISERVER", "HELM_KUBEASGROUPS",
	"HELM_KUBEASUSER", "HELM_KUBECAFILE", "HELM_KUBECONTEXT", "HELM_KUBEINSECURE_SKIP_TLS_VERIFY",
	"HELM_KUBETLS_SERVER_NAME", "HELM_KUBETOKEN", "HELM_NAMESPACE",
}

type harness struct {
	dir, evidence string
	runner        *OSRunner
}

// newHarness gives a runner whose commands start in a new folder and whose
// evidence files go to a second new folder.
func newHarness(t *testing.T, environ []string) harness {
	t.Helper()
	h := harness{dir: t.TempDir(), evidence: t.TempDir()}
	evidence := h.evidence
	h.runner = NewRunner(RunnerOptions{
		Dir:        h.dir,
		Kubeconfig: testKubeconfig,
		Environ:    environ,
		Evidence: func(name string) (io.WriteCloser, error) {
			return os.Create(filepath.Join(evidence, name))
		},
	})
	return h
}

func (h harness) read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.evidence, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// setKillAfter shortens the wait between SIGTERM and SIGKILL for one test.
func setKillAfter(t *testing.T, d time.Duration) {
	old := killAfter
	killAfter = d
	t.Cleanup(func() { killAfter = old })
}

// setWaitDelay shortens how long Run waits for the output to close after the child exited.
func setWaitDelay(t *testing.T, d time.Duration) {
	old := waitDelay
	waitDelay = d
	t.Cleanup(func() { waitDelay = old })
}

// waitFor fails the test unless cond becomes true within d.
func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("%s did not happen within %v", what, d)
}

// groupOf reads the process id that a script wrote to path; with Setpgid it is also the group's id.
func groupOf(t *testing.T, path string) int {
	t.Helper()
	var pid int
	waitFor(t, 2*time.Second, "the script writing its pid", func() bool {
		b, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(b)))
		return err == nil
	})
	return pid
}

func groupGone(pgid int) bool { return syscall.Kill(-pgid, 0) == syscall.ESRCH }

func TestRunStartsInDir(t *testing.T) {
	h := newHarness(t, os.Environ())
	r := h.runner.Run(context.Background(), Command{Name: "pwd", Args: []string{"pwd", "-P"}})
	want, err := filepath.EvalSymlinks(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Ending != Succeeded || strings.TrimSpace(r.Stdout) != want {
		t.Errorf("got %+v, want %q on standard output", r, want)
	}
}

func TestRunUsesNoShell(t *testing.T) {
	h := newHarness(t, os.Environ())
	r := h.runner.Run(context.Background(), Command{Name: "echo", Args: []string{"echo", "$HOME"}})
	if r.Stdout != "$HOME\n" {
		t.Errorf("standard output %q, want the text $HOME", r.Stdout)
	}
}

func TestRunEnvironment(t *testing.T) {
	environ := append(os.Environ(), "KUBECONFIG=/home/someone/.kube/config", "ROLLPROOF_KEEP=yes", "HELM_CACHE_HOME=/tmp/helm-cache")
	for _, name := range helmVariables {
		environ = append(environ, name+"=set-by-the-caller")
	}
	h := newHarness(t, environ)
	r := h.runner.Run(context.Background(), Command{Name: "env", Args: []string{"env"}})
	got := map[string]string{}
	for _, line := range strings.Split(r.Stdout, "\n") {
		if name, value, ok := strings.Cut(line, "="); ok {
			got[name] = value
		}
	}
	if got["KUBECONFIG"] != testKubeconfig {
		t.Errorf("KUBECONFIG is %q, want %q", got["KUBECONFIG"], testKubeconfig)
	}
	for _, name := range helmVariables {
		if _, ok := got[name]; ok {
			t.Errorf("%s reached the command", name)
		}
	}
	if got["ROLLPROOF_KEEP"] != "yes" || got["HELM_CACHE_HOME"] != "/tmp/helm-cache" {
		t.Errorf("a variable not on the list was changed: %q, %q", got["ROLLPROOF_KEEP"], got["HELM_CACHE_HOME"])
	}
	if d := cmp.Diff(helmVariables, h.runner.Removed()); d != "" {
		t.Errorf("Removed (-want +got):\n%s", d)
	}

	two := newHarness(t, []string{"PATH=" + os.Getenv("PATH"), "HELM_NAMESPACE=elsewhere", "HELM_DRIVER=memory"})
	if d := cmp.Diff([]string{"HELM_DRIVER", "HELM_NAMESPACE"}, two.runner.Removed()); d != "" {
		t.Errorf("Removed (-want +got):\n%s", d)
	}
}

func TestRunEndings(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		timeout time.Duration
		ending  Ending
		code    int
	}{
		{"exit 0", []string{"true"}, 0, Succeeded, 0},
		{"exit 3", []string{"sh", "-c", "exit 3"}, 0, ExitedNonZero, 3},
		{"a signal the tool did not send", []string{"sh", "-c", "kill -KILL $$"}, 0, ExitedNonZero, -1},
		{"no such program", []string{"rollproof-test-no-such-program"}, 0, NotStarted, -1},
		{"no program at all", nil, 0, NotStarted, -1},
		{"timeout 0 is no limit", []string{"sleep", "0.5"}, 0, Succeeded, 0},
		{"stopped at the limit", []string{"sleep", "30"}, 300 * time.Millisecond, StoppedAtLimit, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, os.Environ())
			r := h.runner.Run(context.Background(), Command{Name: "c", Args: tc.args, Timeout: tc.timeout})
			if r.Name != "c" || r.Ending != tc.ending || r.ExitCode != tc.code {
				t.Errorf("got %q %s with code %d, want \"c\" %s with code %d", r.Name, r.Ending, r.ExitCode, tc.ending, tc.code)
			}
			if r.Began.IsZero() || r.Ended.Before(r.Began) {
				t.Errorf("Began %v, Ended %v", r.Began, r.Ended)
			}
		})
	}
}

func TestRunSendsSIGTERMAtTheLimit(t *testing.T) {
	h := newHarness(t, os.Environ())
	termFile := filepath.Join(t.TempDir(), "term")
	script := `trap 'echo term > "$0"; exit 0' TERM; sleep 30 & wait`
	r := h.runner.Run(context.Background(), Command{Name: "c", Args: []string{"sh", "-c", script, termFile}, Timeout: 300 * time.Millisecond})
	if r.Ending != StoppedAtLimit || r.ExitCode != -1 {
		t.Errorf("got %s with code %d, want stopped-at-limit with code -1", r.Ending, r.ExitCode)
	}
	if b, err := os.ReadFile(termFile); err != nil || string(b) != "term\n" {
		t.Errorf("the command did not get SIGTERM: %q, %v", b, err)
	}
}

func TestRunKillsAfterKillAfter(t *testing.T) {
	setKillAfter(t, 300*time.Millisecond)
	h := newHarness(t, os.Environ())
	start := time.Now()
	r := h.runner.Run(context.Background(), Command{Name: "c", Args: []string{"sh", "-c", `trap "" TERM; sleep 30`}, Timeout: 300 * time.Millisecond})
	if took := time.Since(start); took < 550*time.Millisecond || took > 1500*time.Millisecond {
		t.Errorf("Run took %v, want about 600ms", took)
	}
	if r.Ending != StoppedAtLimit {
		t.Errorf("ending %s, want stopped-at-limit", r.Ending)
	}
}

func TestRunStopsTheWholeGroup(t *testing.T) {
	h := newHarness(t, os.Environ())
	pidFile := filepath.Join(t.TempDir(), "pid")
	r := h.runner.Run(context.Background(), Command{Name: "c", Args: []string{"sh", "-c", `echo $$ > "$0"; sleep 30 & wait`, pidFile}, Timeout: 300 * time.Millisecond})
	if r.Ending != StoppedAtLimit {
		t.Fatalf("ending %s, want stopped-at-limit", r.Ending)
	}
	pgid := groupOf(t, pidFile)
	waitFor(t, 2*time.Second, "the end of every process of the group", func() bool { return groupGone(pgid) })
}

func TestRunReturnsAtOnceOnSignal(t *testing.T) {
	setKillAfter(t, 500*time.Millisecond)
	h := newHarness(t, os.Environ())
	pidFile, termFile := filepath.Join(t.TempDir(), "pid"), filepath.Join(t.TempDir(), "term")
	script := `echo $$ > "$0"; trap 'echo term > "$1"' TERM; while :; do sleep 0.05; done`
	ctx, cancel := context.WithCancel(context.Background())
	var cancelled time.Time
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancelled = time.Now()
		cancel()
	}()
	r := h.runner.Run(ctx, Command{Name: "c", Args: []string{"sh", "-c", script, pidFile, termFile}})
	returned := time.Now()
	if r.Name != "c" || r.Ending != StoppedBySignal || r.ExitCode != -1 {
		t.Errorf("got %q %s with code %d, want \"c\" stopped-by-signal with code -1", r.Name, r.Ending, r.ExitCode)
	}
	if took := returned.Sub(cancelled); took > 200*time.Millisecond {
		t.Errorf("Run returned %v after the cancel, want at once", took)
	}
	pgid := groupOf(t, pidFile)
	if groupGone(pgid) {
		t.Error("the command ended before killAfter; Run cannot have returned without waiting")
	}
	waitFor(t, 2*time.Second, "SIGTERM reaching the command", func() bool { _, err := os.Stat(termFile); return err == nil })
	waitFor(t, 3*time.Second, "the kill after killAfter", func() bool { return groupGone(pgid) })
	waitFor(t, 3*time.Second, "the evidence's last line", func() bool {
		return strings.HasSuffix(h.read(t, "c"), "rollproof: stopped-by-signal, exit code -1\n")
	})
}

func TestRunDoesNotWaitForABackgroundProcess(t *testing.T) {
	setWaitDelay(t, 200*time.Millisecond)
	h := newHarness(t, os.Environ())
	pidFile := filepath.Join(t.TempDir(), "pid")
	start := time.Now()
	// The background sleep keeps standard output open after the shell has exited.
	r := h.runner.Run(context.Background(), Command{Name: "c", Args: []string{"sh", "-c", `echo $$ > "$0"; sleep 30 & exit 0`, pidFile}, Timeout: 5 * time.Second})
	took := time.Since(start)
	pgid := groupOf(t, pidFile)
	t.Cleanup(func() { syscall.Kill(-pgid, syscall.SIGKILL) })
	if r.Ending != Succeeded || r.ExitCode != 0 || took > 2*time.Second {
		t.Errorf("got %s with code %d after %v, want ok with code 0 soon after the wait delay", r.Ending, r.ExitCode, took)
	}
}

func TestRunEvidence(t *testing.T) {
	h := newHarness(t, os.Environ())
	script := "echo one; sleep 0.2; echo two >&2; sleep 0.2; printf three"
	h.runner.Run(context.Background(), Command{Name: "mixed", Args: []string{"sh", "-c", script}})
	want := `sh -c "echo one; sleep 0.2; echo two >&2; sleep 0.2; printf three"` + "\none\ntwo\nthree\nrollproof: ok, exit code 0\n"
	if d := cmp.Diff(want, h.read(t, "mixed")); d != "" {
		t.Errorf("evidence (-want +got):\n%s", d)
	}

	h.runner.Run(context.Background(), Command{Name: "same", Args: []string{"sh", "-c", "exit 3"}})
	h.runner.Run(context.Background(), Command{Name: "same", Args: []string{"true"}})
	if got := h.read(t, "same"); !strings.HasSuffix(got, "rollproof: exit-nonzero, exit code 3\n") {
		t.Errorf("first evidence %q", got)
	}
	if got := h.read(t, "same-2"); got != "true\nrollproof: ok, exit code 0\n" {
		t.Errorf("second evidence %q", got)
	}

	h.runner.Run(context.Background(), Command{Name: "absent", Args: []string{"rollproof-test-no-such-program"}})
	if got := h.read(t, "absent"); !strings.HasSuffix(got, "rollproof: not-started, exit code -1\n") {
		t.Errorf("evidence of a command that did not start %q", got)
	}
}

func TestRunLargeOutput(t *testing.T) {
	h := newHarness(t, os.Environ())
	script := "head -c 20971520 /dev/zero; head -c 20971520 /dev/zero >&2"
	r := h.runner.Run(context.Background(), Command{Name: "large", Args: []string{"sh", "-c", script}})
	if r.Ending != Succeeded {
		t.Fatalf("ending %s", r.Ending)
	}
	if len(r.Stdout) != 1<<20 {
		t.Errorf("Stdout holds %d bytes, want the first 1 MiB", len(r.Stdout))
	}
	info, err := os.Stat(filepath.Join(h.evidence, "large"))
	if err != nil {
		t.Fatal(err)
	}
	if size := info.Size(); size < 40<<20 || size > 40<<20+1024 {
		t.Errorf("evidence holds %d bytes, want all 40 MiB of the output", size)
	}
}

func TestRunWritingAfterSIGTERM(t *testing.T) {
	setKillAfter(t, 300*time.Millisecond)
	h := newHarness(t, os.Environ())
	start := time.Now()
	r := h.runner.Run(context.Background(), Command{Name: "c", Args: []string{"sh", "-c", `trap "" TERM; while :; do echo x; done`}, Timeout: 300 * time.Millisecond})
	if took := time.Since(start); r.Ending != StoppedAtLimit || took > 1500*time.Millisecond {
		t.Errorf("got %s after %v, want stopped-at-limit after about 600ms", r.Ending, took)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/deploy/`
Expected: FAIL, the package does not build: `undefined: OSRunner`.

- [ ] **Step 3: Write the command runner in `internal/deploy/command.go`**

The file starts with `//go:build linux`. The package comment is the text under `module internal/deploy` in the Interfaces block. Write, each with its interface comment from the Interfaces block: `type Ending string` and its five constants, `type Command`, `type Result`, `type Runner interface`, `type RunnerOptions`, `func NewRunner(o RunnerOptions) *OSRunner` and `func (r *OSRunner) Removed() []string`. `OSRunner` implements Runner with `func (r *OSRunner) Run(ctx context.Context, c Command) Result`. Also:

```go
// killAfter is how long a command has between SIGTERM and SIGKILL (section 7).
var killAfter = 10 * time.Second

// waitDelay is how long Run waits for the output to close after the child
// exited: a process the child left behind may hold it open.
var waitDelay = time.Second
```

`NewRunner` builds the environment once: `o.Environ` without any `KUBECONFIG` entry and without the eleven variables of the Global Constraints, then `KUBECONFIG=` and `o.Kubeconfig`. Every other entry passes unchanged. `Removed` returns the names of the eleven that were present, each once, sorted.

`Run`, in order:

1. Read `killAfter` and `waitDelay` once, into locals; a goroutine that outlives Run uses the local.
2. Every Result that Run returns has Name `c.Name`. Began is now. The evidence name is `c.Name` for its first use in this runner and `c.Name-N` for its Nth (N from 2), under a mutex. Open it with `o.Evidence`; when that is nil or fails, the output goes to `io.Discard` and the command still runs.
3. Write the command line as the evidence's first line: the arguments joined by one space, each written as it is when it is not empty and holds only letters, digits and `-_./:=@,+%`, else with `strconv.Quote`.
4. With no argument, or when `Start` fails: write `rollproof: could not start: ` and the error on a line; the Result is NotStarted with ExitCode -1.
5. `exec.Command(c.Args[0], c.Args[1:]...)` (not `exec.CommandContext`, whose cancel sends SIGKILL at once), with `Dir` o.Dir, `Env` the built environment, `SysProcAttr: &syscall.SysProcAttr{Setpgid: true}` and `WaitDelay` the local wait delay. Standard output goes to an `io.MultiWriter` of the evidence and a buffer that keeps the first 1 MiB; standard error goes to the evidence. Both pass through one mutex-guarded writer that remembers the last byte written. Nothing holds the whole output.
6. Wait for the child in a goroutine. Then the first of:
   - the child exits: ExitCode is `ProcessState.ExitCode()` (-1 when a signal killed it); Succeeded for 0, else ExitedNonZero. `exec.ErrWaitDelay` from `Wait` is read as the child's own exit: the output was still open when the wait delay ran out.
   - `c.Timeout` passes (no limit when it is 0): stop the group, wait for the child; StoppedAtLimit.
   - ctx ends: stop the group and return at once, StoppedBySignal, with the output kept so far. The waiting goroutine writes the evidence's last line and closes it when the child has gone.
   To stop the group: SIGTERM to `-pid`, and a goroutine that sends SIGKILL to `-pid` after the local killAfter unless the child has exited first. ExitCode is -1 for both stopped endings, however the child then ended.
7. Ended is now. The evidence ends with a newline if the output did not end with one, then `rollproof: ENDING, exit code N`, and is closed.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/deploy/ -v`
Expected: PASS

Run: `go test ./internal/deploy/ -count=3`
Expected: `ok`, the timings holding on every run.

- [ ] **Step 5: Commit**

```bash
git add internal/deploy
git commit -m "Start, bound and stop the commands of a run"
```

### Task 24: `deploy`: the Helm driver

**Files:**
- Create: `internal/deploy/deploy.go`, `internal/deploy/helm.go`
- Test: `internal/deploy/helm_test.go`, `internal/deploy/fake_test.go`

**Interfaces:**
- Consumes: `type Runner interface { Run(ctx context.Context, c Command) Result }`, `type Command`, `type Result`, `type Ending` (Task 23); `type Helm struct { Release string; Previous, Candidate *Release }`, `type Release`, `type Value` (Task 17); `type WindowName`, ChartFetchFailed, RunnerUnsupported (Task 12); the command lines that `docs/proofs/p05-helm-cli.md` records (Task 10).
- Produces: `type Deployer interface { Fetch(ctx context.Context) error; Install(ctx context.Context) Result; Deploy(ctx context.Context) Result; Rollback(ctx context.Context) Result; Transitions() []verdict.WindowName; Releases() []Release }`
  A deploy driver. Fetch gets everything from outside the cluster that the
  later calls need, before the probe starts. Install, Deploy and Rollback
  each run one command and return how it ended; a command may return before
  its rollout is complete. Transitions are the windows this driver gives a
  run, in order.
- Produces: `type Release struct { Role, Chart, Version, Source string; ValueFiles, InlineKeys []string }`
  What the report says of one release: Role is previous or candidate; Chart
  and Version are the chart's own name and version; Source is where it came
  from; ValueFiles are file names and InlineKeys the top-level keys of
  inline maps. Never a value.
- Produces: `func CheckHelm() error`
  Checks that a helm command is on the PATH. An error is a Condition with
  RunnerUnsupported.
- Produces: `type HelmOptions struct { Helm config.Helm; Namespace, Kubeconfig, Context, RunDir, Dir string; Timeout time.Duration; Runner Runner }`
  What the Helm driver needs: the two releases, the test cluster as Helm's
  command line names it, the run's folder for fetched charts and written
  values, the configuration's folder, and limits.timeout.
- Produces: `func NewHelm(o HelmOptions) *HelmDriver`
  The Helm driver. It implements Deployer.

- [ ] **Step 1: Write the fake runner**

Create `internal/deploy/fake_test.go`:

```go
package deploy

import (
	"context"
	"os"
	"path/filepath"
	"sync"
)

// fakeRunner records every Command it is given and answers with the Result
// scripted for the Command's Name. A Name with no script succeeds with exit
// code 0. An effect scripted for a Name runs before the answer. Like the
// real runner, it sets the Result's Name to the Command's.
type fakeRunner struct {
	mu      sync.Mutex
	calls   []Command
	results map[string]Result
	effects map[string]func(c Command) error
}

func newFake() *fakeRunner {
	return &fakeRunner{results: map[string]Result{}, effects: map[string]func(Command) error{}}
}

func (f *fakeRunner) Run(ctx context.Context, c Command) Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
	if effect := f.effects[c.Name]; effect != nil {
		if err := effect(c); err != nil {
			panic(err)
		}
	}
	r, ok := f.results[c.Name]
	if !ok {
		r = Result{Ending: Succeeded}
	}
	r.Name = c.Name
	return r
}

// names are the Names of the recorded Commands, in order.
func (f *fakeRunner) names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		out = append(out, c.Name)
	}
	return out
}

// argAfter is the argument that follows flag in args, or "" when there is none.
func argAfter(args []string, flag string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

// writeArchive is what a pull does: it writes NAME-VERSION.tgz into the
// folder that the command's --destination names.
func writeArchive(name, version string) func(c Command) error {
	return func(c Command) error {
		dest := argAfter(c.Args, "--destination")
		return os.WriteFile(filepath.Join(dest, name+"-"+version+".tgz"), []byte("archive"), 0o644)
	}
}
```

- [ ] **Step 2: Write the failing tests**

Create `internal/deploy/helm_test.go`. Every command line is asserted whole. Where `docs/proofs/p05-helm-cli.md` (Task 10) records a command line that differs, the record wins: change the expected arguments and the code together, and say so in the commit message. The candidate's inline map holds an integer, a boolean, a list and a quoted number, so that the test sees every type survive the written values file.

```go
package deploy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.yaml.in/yaml/v3"

	"rollproof/internal/config"
	"rollproof/internal/verdict"
)

const (
	kubeconfig  = "/run/rollproof-test/kubeconfig"
	kubeContext = "kind-rollproof-test"
	timeout     = 5 * time.Minute
)

var _ Deployer = (*HelmDriver)(nil)

// helmConfig names a remote previous release with one inline map and a
// candidate chart in the configuration's folder /work/app with a values
// file and an inline map.
func helmConfig() config.Helm {
	return config.Helm{
		Release: "orders",
		Previous: &config.Release{
			Chart: "oci://registry.example/charts/orders", Version: "1.4.2",
			Values: []config.Value{{Inline: map[string]any{"replicas": 2}}},
		},
		Candidate: &config.Release{
			Chart: "/work/app/chart", IsPath: true,
			Values: []config.Value{
				{File: "/work/app/test/values-kind.yaml"},
				{Inline: map[string]any{
					"image": map[string]any{"digest": "sha256:abc"}, "replicas": 3,
					"enabled": true, "args": []any{"--fast", 2}, "port": "8080",
				}},
			},
		},
	}
}

// scripted is a fake that answers a whole run: it pulls the previous chart,
// shows both charts, and gives revision 1 as the install's status.
func scripted() *fakeRunner {
	f := newFake()
	f.effects["pull-previous"] = writeArchive("orders", "1.4.2")
	f.results["show-previous"] = Result{Ending: Succeeded, Stdout: "apiVersion: v2\nname: orders\nversion: 1.4.2\n"}
	f.results["show-candidate"] = Result{Ending: Succeeded, Stdout: "apiVersion: v2\nname: orders\nversion: 1.5.0\ndescription: the candidate\n"}
	f.results["status-install"] = Result{Ending: Succeeded, Stdout: `{"name":"orders","namespace":"orders","version":1,"info":{"status":"deployed"}}`}
	return f
}

func newDriver(t *testing.T, h config.Helm, f *fakeRunner) (*HelmDriver, string) {
	t.Helper()
	runDir := t.TempDir()
	return NewHelm(HelmOptions{
		Helm: h, Namespace: "orders", Kubeconfig: kubeconfig, Context: kubeContext,
		RunDir: runDir, Dir: "/work/app", Timeout: timeout, Runner: f,
	}), runDir
}

// mustPanic fails the test unless f panics with a message that holds want.
func mustPanic(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		if got := fmt.Sprint(recover()); !strings.Contains(got, want) {
			t.Errorf("panic %q, want one that holds %q", got, want)
		}
	}()
	f()
}

func TestHelmCommandLines(t *testing.T) {
	f := scripted()
	drv, runDir := newDriver(t, helmConfig(), f)
	ctx := context.Background()
	if err := drv.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		name string
		run  func(context.Context) Result
	}{{"install", drv.Install}, {"deploy", drv.Deploy}, {"rollback", drv.Rollback}} {
		if r := step.run(ctx); r.Name != step.name || r.Ending != Succeeded {
			t.Fatalf("got %+v, want %s to succeed", r, step.name)
		}
	}
	archive := filepath.Join(runDir, "charts", "previous", "orders-1.4.2.tgz")
	cl := []string{"--namespace", "orders", "--kubeconfig", kubeconfig, "--kube-context", kubeContext}
	want := []Command{
		{Name: "pull-previous", Args: []string{"helm", "pull", "oci://registry.example/charts/orders", "--version", "1.4.2",
			"--destination", filepath.Join(runDir, "charts", "previous")}, Timeout: timeout},
		{Name: "show-previous", Args: []string{"helm", "show", "chart", archive}, Timeout: timeout},
		{Name: "show-candidate", Args: []string{"helm", "show", "chart", "/work/app/chart"}, Timeout: timeout},
		{Name: "install", Args: slices.Concat([]string{"helm", "upgrade", "--install", "orders", archive}, cl,
			[]string{"--timeout", "5m0s", "--values", filepath.Join(runDir, "values", "previous-1.yaml")}), Timeout: timeout},
		{Name: "status-install", Args: slices.Concat([]string{"helm", "status", "orders"}, cl, []string{"-o", "json"}), Timeout: timeout},
		{Name: "deploy", Args: slices.Concat([]string{"helm", "upgrade", "orders", "/work/app/chart", "--reset-values"}, cl,
			[]string{"--timeout", "5m0s", "--values", "/work/app/test/values-kind.yaml", "--values", filepath.Join(runDir, "values", "candidate-2.yaml")}), Timeout: timeout},
		{Name: "rollback", Args: slices.Concat([]string{"helm", "rollback", "orders", "1"}, cl, []string{"--timeout", "5m0s"}), Timeout: timeout},
	}
	if d := cmp.Diff(want, f.calls); d != "" {
		t.Errorf("commands (-want +got):\n%s", d)
	}
	for _, c := range f.calls {
		for _, flag := range []string{"--wait", "--atomic", "--rollback-on-failure", "--create-namespace"} {
			if slices.Contains(c.Args, flag) {
				t.Errorf("%s passes %s", c.Name, flag)
			}
		}
	}
}

func TestHelmBothChartsRemote(t *testing.T) {
	h := helmConfig()
	h.Candidate = &config.Release{Chart: "oci://registry.example/charts/orders", Version: "1.5.0"}
	f := scripted()
	f.effects["pull-candidate"] = writeArchive("orders", "1.5.0")
	drv, runDir := newDriver(t, h, f)
	if err := drv.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	drv.Install(context.Background())
	drv.Deploy(context.Background())
	if d := cmp.Diff([]string{"pull-previous", "show-previous", "pull-candidate", "show-candidate", "install", "status-install", "deploy"}, f.names()); d != "" {
		t.Errorf("names (-want +got):\n%s", d)
	}
	if got, want := f.calls[6].Args[3], filepath.Join(runDir, "charts", "candidate", "orders-1.5.0.tgz"); got != want {
		t.Errorf("deploy names %q, want %q", got, want)
	}
}

func TestHelmInlineValuesKeepTheirTypes(t *testing.T) {
	drv, runDir := newDriver(t, helmConfig(), scripted())
	if err := drv.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(runDir, "values", "candidate-2.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(text, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"image": map[string]any{"digest": "sha256:abc"}, "replicas": 3, "enabled": true, "args": []any{"--fast", 2}, "port": "8080"}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("candidate-2.yaml (-want +got):\n%s", d)
	}
}

func TestHelmFetchFailures(t *testing.T) {
	const output = "PULL-OUTPUT"
	for _, tc := range []struct {
		name  string
		setup func(f *fakeRunner)
		role  string
		chart string
	}{
		{"the pull exits non-zero", func(f *fakeRunner) {
			delete(f.effects, "pull-previous")
			f.results["pull-previous"] = Result{Ending: ExitedNonZero, ExitCode: 1, Stdout: output}
		}, "previous", "oci://registry.example/charts/orders"},
		{"the pull is stopped at the limit", func(f *fakeRunner) {
			delete(f.effects, "pull-previous")
			f.results["pull-previous"] = Result{Ending: StoppedAtLimit, ExitCode: -1, Stdout: output}
		}, "previous", "oci://registry.example/charts/orders"},
		{"the pull leaves no archive", func(f *fakeRunner) { delete(f.effects, "pull-previous") }, "previous", "oci://registry.example/charts/orders"},
		{"show cannot start", func(f *fakeRunner) {
			f.results["show-candidate"] = Result{Ending: NotStarted, ExitCode: -1, Stdout: output}
		}, "candidate", "/work/app/chart"},
		{"show gives no name", func(f *fakeRunner) {
			f.results["show-candidate"] = Result{Ending: Succeeded, Stdout: "version: 1.5.0\n" + output + "\n"}
		}, "candidate", "/work/app/chart"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := scripted()
			tc.setup(f)
			drv, _ := newDriver(t, helmConfig(), f)
			var c verdict.Condition
			if err := drv.Fetch(context.Background()); !errors.As(err, &c) || c.Code != verdict.ChartFetchFailed {
				t.Fatalf("got %v, want a chart-fetch-failed condition", err)
			}
			if !strings.Contains(c.Text, tc.role) || !strings.Contains(c.Text, tc.chart) || strings.Contains(c.Text, output) {
				t.Errorf("text %q, want the role %q and the chart %q and not the output", c.Text, tc.role, tc.chart)
			}
		})
	}
}

func TestHelmFetchStoppedBySignal(t *testing.T) {
	f := scripted()
	delete(f.effects, "pull-previous")
	f.results["pull-previous"] = Result{Ending: StoppedBySignal, ExitCode: -1}
	drv, _ := newDriver(t, helmConfig(), f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := drv.Fetch(ctx)
	var c verdict.Condition
	if !errors.Is(err, context.Canceled) || errors.As(err, &c) {
		t.Errorf("got %v, want the context's error and no condition", err)
	}
}

func TestHelmInstallReadsTheRevision(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status Result
		of     string
		ending Ending
		arg    string
	}{
		{"revision 4", Result{Ending: Succeeded, Stdout: `{"version":4}`}, "install", Succeeded, "4"},
		{"status fails", Result{Ending: ExitedNonZero, ExitCode: 1}, "status-install", ExitedNonZero, ""},
		{"no version", Result{Ending: Succeeded, Stdout: `{"name":"orders"}`}, "status-install", ExitedNonZero, ""},
		{"not JSON", Result{Ending: Succeeded, Stdout: "Error: something"}, "status-install", ExitedNonZero, ""},
		{"status stopped by a signal", Result{Ending: StoppedBySignal, ExitCode: -1}, "status-install", StoppedBySignal, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := scripted()
			f.results["status-install"] = tc.status
			drv, _ := newDriver(t, helmConfig(), f)
			if err := drv.Fetch(context.Background()); err != nil {
				t.Fatal(err)
			}
			if r := drv.Install(context.Background()); r.Name != tc.of || r.Ending != tc.ending {
				t.Fatalf("Install gave the result of %q, ended %s; want %q, %s", r.Name, r.Ending, tc.of, tc.ending)
			}
			if tc.arg == "" {
				return
			}
			drv.Rollback(context.Background())
			if got := f.calls[len(f.calls)-1].Args[3]; got != tc.arg {
				t.Errorf("rollback to %q, want %q", got, tc.arg)
			}
		})
	}
}

func TestHelmEndingsPassThrough(t *testing.T) {
	f := scripted()
	stalled := Result{Name: "deploy", Ending: StoppedAtLimit, ExitCode: -1, Began: time.Unix(100, 0), Ended: time.Unix(400, 0)}
	f.results["deploy"] = stalled
	drv, _ := newDriver(t, helmConfig(), f)
	if err := drv.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	drv.Install(context.Background())
	if d := cmp.Diff(stalled, drv.Deploy(context.Background())); d != "" {
		t.Errorf("Deploy (-want +got):\n%s", d)
	}

	f = scripted()
	failed := Result{Name: "install", Ending: ExitedNonZero, ExitCode: 1}
	f.results["install"] = failed
	drv, _ = newDriver(t, helmConfig(), f)
	if err := drv.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(failed, drv.Install(context.Background())); d != "" {
		t.Errorf("Install (-want +got):\n%s", d)
	}
	if slices.Contains(f.names(), "status-install") {
		t.Error("the status was read after a failed install")
	}
}

func TestHelmTransitionsAndReleases(t *testing.T) {
	h := helmConfig()
	h.Candidate.Values = append(h.Candidate.Values, config.Value{Inline: map[string]any{"image": map[string]any{"tag": "x"}, "zone": "a"}})
	drv, _ := newDriver(t, h, scripted())
	if d := cmp.Diff([]verdict.WindowName{verdict.Deploy, verdict.Rollback}, drv.Transitions()); d != "" {
		t.Errorf("Transitions (-want +got):\n%s", d)
	}
	if err := drv.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []Release{
		{Role: "previous", Chart: "orders", Version: "1.4.2", Source: "oci://registry.example/charts/orders", InlineKeys: []string{"replicas"}},
		{Role: "candidate", Chart: "orders", Version: "1.5.0", Source: "./chart", ValueFiles: []string{"values-kind.yaml"},
			InlineKeys: []string{"args", "enabled", "image", "port", "replicas", "zone"}},
	}
	if d := cmp.Diff(want, drv.Releases()); d != "" {
		t.Errorf("Releases (-want +got):\n%s", d)
	}

	outside := helmConfig()
	outside.Candidate.Chart = "/elsewhere/chart"
	drv, _ = newDriver(t, outside, scripted())
	if err := drv.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := drv.Releases()[1].Source; got != "/elsewhere/chart" {
		t.Errorf("Source of a chart outside the folder %q, want the absolute path", got)
	}
}

func TestHelmProgrammingErrors(t *testing.T) {
	h := helmConfig()
	h.Previous = nil
	mustPanic(t, "previous", func() { newDriver(t, h, scripted()) })

	drv, _ := newDriver(t, helmConfig(), scripted())
	mustPanic(t, "Install", func() { drv.Install(context.Background()) })
	if err := drv.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	mustPanic(t, "Deploy", func() { drv.Deploy(context.Background()) })
	mustPanic(t, "Rollback", func() { drv.Rollback(context.Background()) })
}

func TestCheckHelm(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var c verdict.Condition
	if err := CheckHelm(); !errors.As(err, &c) || c.Code != verdict.RunnerUnsupported || c.Text != "helm is not on the PATH" {
		t.Errorf("got %v, want runner-unsupported: helm is not on the PATH", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "helm"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if err := CheckHelm(); err != nil {
		t.Errorf("got %v with helm on the PATH", err)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/deploy/`
Expected: FAIL, the package does not build: `undefined: HelmDriver`.

- [ ] **Step 4: Write `Deployer` and `Release` in `internal/deploy/deploy.go`**

`type Deployer interface { … }` and `type Release struct { … }` exactly as the Interfaces block gives them, each with its interface comment from the Interfaces block.

- [ ] **Step 5: Write the Helm driver in `internal/deploy/helm.go`**

`func CheckHelm() error`, `type HelmOptions struct { … }`, `func NewHelm(o HelmOptions) *HelmDriver` and the six methods of Deployer on `*HelmDriver`, each with its interface comment from the Interfaces block.

- `CheckHelm`: `exec.LookPath("helm")`; on an error `verdict.Condition{Code: verdict.RunnerUnsupported, Text: "helm is not on the PATH"}`.
- `NewHelm` panics when `o.Helm.Previous` is nil: "deploy: the Helm driver without a previous release is not built yet". `config.NotBuilt` keeps that configuration from reaching it.
- CL is `--namespace o.Namespace --kubeconfig o.Kubeconfig --kube-context o.Context`. TIMEOUT is `o.Timeout.String()` (`5m0s`). Every Command has Timeout `o.Timeout`. No Command carries `--wait`, `--atomic`, `--rollback-on-failure` or `--create-namespace`.

`Fetch`, for previous and then candidate (ROLE), stopping at the first error:

1. A chart that is not a path: make `RUNDIR/charts/ROLE` with mode 0700 (`helm pull` writes into the folder and does not make it), then run `pull-ROLE`: `helm pull CHART --version VERSION --destination RUNDIR/charts/ROLE`, with no CL. CHARTREF is the one `.tgz` file the folder then holds. A path chart's CHARTREF is its Chart.
2. Run `show-ROLE`: `helm show chart CHARTREF`. Read `name` and `version` from its standard output as YAML.
3. Number the release's values items from 1. A file adds `--values FILE`. An inline map is written with `yaml.Marshal` to `RUNDIR/values/ROLE-INDEX.yaml` (folder 0700, file 0600) and adds `--values` and that path. The types of the map are kept as YAML has them.
4. Keep the release for `Releases`: Role; Chart and Version from step 2; Source is CHART as the configuration gives it for a remote chart, and for a path chart `./` and its path relative to `o.Dir` (`.` for `o.Dir` itself), or the absolute path when it is not under `o.Dir`; ValueFiles the base names of the files in order; InlineKeys the top-level keys of all inline maps, each once, sorted. A list with nothing in it stays nil.

When ctx has ended after a command, Fetch returns `fmt.Errorf("fetch the ROLE chart: %w", ctx.Err())`. Otherwise a pull that did not end Succeeded, or that left not exactly one archive, returns `verdict.Condition{Code: verdict.ChartFetchFailed, Text: "could not pull the ROLE release's chart CHART; the command's output is in the evidence"}`; a show that did not end Succeeded, or whose output has no name or no version, the same with "read" in place of "pull". The text never holds the command's output.

`Install` (panics before a successful Fetch: "deploy: Install called before a successful Fetch"): run `install`: `helm upgrade --install RELEASE CHARTREF CL --timeout TIMEOUT` and the previous release's `--values` pairs. A Result that is not Succeeded is returned as it is. Then run `status-install`: `helm status RELEASE CL -o json`, and read the integer `version` of its JSON as the revision.

- The status did not end Succeeded: return the status's own Result, whose Name is `status-install` (a StoppedBySignal ending stays one: the signal's ending is not judged).
- It succeeded, but its standard output is not JSON or its `version` is missing or below 1: return the status's own Result with Ending ExitedNonZero.
- Else keep the revision and return the install's Result.

`Deploy` (panics before a successful Install: "deploy: Deploy called before a successful Install"): run `deploy`: `helm upgrade RELEASE CHARTREF --reset-values CL --timeout TIMEOUT` and the candidate's `--values` pairs. `Rollback` (panics likewise, naming Rollback): run `rollback`: `helm rollback RELEASE REVISION CL --timeout TIMEOUT`. Each returns its command's Result unchanged.

`Transitions` returns `[]verdict.WindowName{verdict.Deploy, verdict.Rollback}`. `Releases` returns previous and then candidate, as Fetch kept them.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/deploy/ -v`
Expected: PASS, Task 23's tests included.

- [ ] **Step 7: Commit**

```bash
git add internal/deploy
git commit -m "Drive Helm: fetch charts, install, deploy with reset values, and roll back to the install's revision"
```
