//go:build linux

// Proof of point 11: whether following a pod's log through the API loses
// lines when the kubelet rotates the log, how a stream that ended is opened
// again with no line lost and none read twice, and what the last piece of an
// exited container's output looks like.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"

	"rollproof-proofs/internal/lab"
)

const (
	certsDPatch = "[plugins.\"io.containerd.grpc.v1.cri\".registry]\n  config_path = \"/etc/containerd/certs.d\""
	// testHosts is the settings file for rollproof.test that the p01 record
	// decided (its case e): the run's registry alone, and no server line.
	testHosts = "[host.\"http://%s\"]\n  capabilities = [\"pull\"]\n"

	namespace = "p11"
	podName   = "liner"
	container = "probe"

	// sameText is the line that the liner's -same writes.
	sameText = `{"ok":false,"class":"refused"}`
)

// experiments are the slots of the summary line, in its order.
var experiments = []string{"A", "B", "C", "D", "E"}

// infoOnly are the experiments whose results are info lines only. Each must
// run to its end, as the slots must, or the summary counts it as failed.
var infoOnly = []string{"C2"}

func main() { os.Exit(run()) }

// checks counts the failed checks, prints one line for each, and holds what
// the summary line says of each experiment that ran to its end.
type checks struct {
	failed int
	slot   map[string]string
}

func (c *checks) ok(format string, args ...any) {
	fmt.Println("ok: " + shown(fmt.Sprintf(format, args...)))
}

func (c *checks) fail(format string, args ...any) {
	c.failed++
	fmt.Println("FAILED: " + shown(fmt.Sprintf(format, args...)))
}

func info(format string, args ...any) { fmt.Println("info: " + shown(fmt.Sprintf(format, args...))) }

// cond is one condition of a check, stated as what must hold.
type cond struct {
	held bool
	text string
}

// verdict prints the one line of the check what: ok when every condition
// held, else FAILED with the conditions that did not. It returns whether
// every condition held.
func (c *checks) verdict(what string, conds ...cond) bool {
	var held, not []string
	for _, x := range conds {
		if x.held {
			held = append(held, x.text)
		} else {
			not = append(not, x.text)
		}
	}
	if len(not) == 0 {
		c.ok("%s: %s", what, strings.Join(held, "; "))
		return true
	}
	line := what + ": did not hold: " + strings.Join(not, "; ")
	if len(held) > 0 {
		line += "; held: " + strings.Join(held, "; ")
	}
	c.fail("%s", line)
	return false
}

// shown writes the home folder at the start of a path as $HOME.
func shown(s string) string {
	if home := os.Getenv("HOME"); home != "" {
		return strings.ReplaceAll(s, home+string(filepath.Separator), "$HOME"+string(filepath.Separator))
	}
	return s
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c := &checks{slot: map[string]string{}}

	dir, err := os.MkdirTemp("", "rollproof-proof-p11-liner-")
	if err != nil {
		c.fail("a temporary folder for liner: %v", err)
		return summary(ctx, c)
	}
	defer os.RemoveAll(dir)

	prove(ctx, c, dir)
	return summary(ctx, c)
}

// proof is what the experiments share.
type proof struct {
	ctx    context.Context
	c      *checks
	l      *lab.Lab
	worker string
	image  string // rollproof.test/probe/liner@DIGEST
}

// prove does steps 1 to 8. The cluster is deleted before it returns.
func prove(ctx context.Context, c *checks, dir string) {
	// Step 1: liner.
	binary := filepath.Join(dir, "liner")
	build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-ldflags", "-s -w", "-o", binary, "./p11-log-rotation/liner")
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux")
	if out, err := build.CombinedOutput(); err != nil {
		c.fail("building liner: %v\n%s", err, out)
		return
	}

	// Step 2: the cluster.
	cfg := &v1alpha4.Cluster{
		Nodes:                   []v1alpha4.Node{{Role: v1alpha4.ControlPlaneRole}, {Role: v1alpha4.WorkerRole}},
		ContainerdConfigPatches: []string{certsDPatch},
	}
	l, err := lab.Up(ctx, "p11", cfg)
	if err != nil {
		c.fail("lab.Up: %v", err)
		return
	}
	// Step 8, also when a step fails.
	defer func() {
		if err := l.Down(); err != nil {
			c.fail("l.Down(): %v", err)
		} else {
			c.ok("l.Down() returned no error")
		}
	}()
	nodes := l.Nodes()
	if len(nodes) != 2 {
		c.fail("l.Nodes() is %q, want two names", nodes)
		return
	}
	p := &proof{ctx: ctx, c: c, l: l, worker: nodes[1]}
	info("worker %s", p.worker)
	p.kubeletInfo()

	// Step 3: the registry, its settings in every node, the image, the pull.
	reg, err := l.Registry(ctx, "run")
	if err != nil {
		c.fail("l.Registry(ctx, \"run\"): %v", err)
		return
	}
	info("run: Push %s, Node %s", reg.Push, reg.Node)
	text := fmt.Sprintf(testHosts, reg.Node)
	if err := p.writeHosts("rollproof.test", text); err != nil {
		c.fail("writing the settings for rollproof.test: %v", err)
		return
	}
	info("/etc/containerd/certs.d/rollproof.test/hosts.toml, written in every node:")
	for line := range strings.Lines(text) {
		info("  %s", strings.TrimRight(line, "\n"))
	}
	digest, err := lab.PushBinary(ctx, reg.Push, "probe/liner", binary)
	if err != nil {
		c.fail("pushing liner: %v", err)
		return
	}
	p.image = "rollproof.test/probe/liner@" + digest
	info("liner = %s, in run as probe/liner", digest)
	if _, err := l.Exec(p.worker, nil, "crictl", "pull", p.image); err != nil {
		c.fail("crictl pull %s in the worker: %v", p.image, err)
		return
	}
	info("crictl pull %s in the worker succeeded", p.image)

	// Step 4: the namespace.
	if _, err := l.Client().CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}, metav1.CreateOptions{}); err != nil {
		c.fail("creating the namespace %s: %v", namespace, err)
		return
	}
	if err := p.waitServiceAccount(); err != nil {
		c.fail("the default service account of %s: %v", namespace, err)
		return
	}

	// Step 7: the experiments.
	rotating, n := p.experimentA()
	if ctx.Err() != nil {
		return
	}
	p.experimentB()
	if ctx.Err() != nil {
		return
	}
	if rotating != nil {
		p.experimentC(rotating, n)
	} else {
		c.fail("C: A gave no arguments with which the log rotated, so C did not run")
	}
	if ctx.Err() != nil {
		return
	}
	p.experimentC2()
	if ctx.Err() != nil {
		return
	}
	p.experimentD()
	if ctx.Err() != nil {
		return
	}
	p.experimentE()
}

// kubeletInfo prints the server's version and the worker kubelet's log
// settings as its configz gives them.
func (p *proof) kubeletInfo() {
	if v, err := p.l.Client().Discovery().ServerVersion(); err != nil {
		info("the server's version: %v", err)
	} else {
		info("the server's GitVersion is %s", v.GitVersion)
	}
	raw, err := p.l.Client().CoreV1().RESTClient().Get().AbsPath("/api/v1/nodes", p.worker, "proxy", "configz").DoRaw(p.ctx)
	if err != nil {
		info("configz of %s: %v", p.worker, err)
		return
	}
	var cz struct {
		KubeletConfig map[string]any `json:"kubeletconfig"`
	}
	if err := json.Unmarshal(raw, &cz); err != nil {
		info("reading configz of %s: %v", p.worker, err)
		return
	}
	var parts []string
	for _, k := range []string{"containerLogMaxSize", "containerLogMaxFiles", "containerLogMaxWorkers", "containerLogMonitorInterval"} {
		parts = append(parts, fmt.Sprintf("%s %v", k, cz.KubeletConfig[k]))
	}
	info("the worker's kubelet, by configz: %s", strings.Join(parts, ", "))
}

// writeHosts writes text to /etc/containerd/certs.d/HOST/hosts.toml in every node.
func (p *proof) writeHosts(host, text string) error {
	dir := "/etc/containerd/certs.d/" + host
	for _, node := range p.l.Nodes() {
		if _, err := p.l.Exec(node, nil, "mkdir", "-p", dir); err != nil {
			return err
		}
		if _, err := p.l.Exec(node, strings.NewReader(text), "cp", "/dev/stdin", dir+"/hosts.toml"); err != nil {
			return err
		}
	}
	return nil
}

// waitServiceAccount waits, at most 30 seconds, until the namespace has its
// default service account, which a pod needs before it can be created.
func (p *proof) waitServiceAccount() error {
	var getErr error
	err := p.poll(30*time.Second, func() bool {
		_, getErr = p.l.Client().CoreV1().ServiceAccounts(namespace).Get(p.ctx, "default", metav1.GetOptions{})
		return getErr == nil
	})
	if err != nil && getErr != nil {
		return fmt.Errorf("%w (last: %v)", err, getErr)
	}
	return err
}

// errTimeUp is what poll returns when its time is up.
var errTimeUp = errors.New("time is up")

// poll calls done every 250 ms until it returns true, for at most limit and
// while the run lasts.
func (p *proof) poll(limit time.Duration, done func() bool) error {
	deadline := time.Now().Add(limit)
	for {
		if done() {
			return nil
		}
		if p.ctx.Err() != nil {
			return p.ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w after %v", errTimeUp, limit)
		}
		select {
		case <-p.ctx.Done():
			return p.ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// probeStatus returns the status of the container probe in pod, or nil.
func probeStatus(pod *corev1.Pod) *corev1.ContainerStatus {
	if pod == nil {
		return nil
	}
	for i := range pod.Status.ContainerStatuses {
		if pod.Status.ContainerStatuses[i].Name == container {
			return &pod.Status.ContainerStatuses[i]
		}
	}
	return nil
}

// describe gives a pod's phase and, when its container waits or ended, why.
func describe(pod *corev1.Pod) string {
	if pod == nil {
		return "never read"
	}
	s := "phase " + string(pod.Status.Phase)
	if st := probeStatus(pod); st != nil {
		switch {
		case st.State.Waiting != nil:
			s += ", waiting: " + st.State.Waiting.Reason + " " + st.State.Waiting.Message
		case st.State.Running != nil:
			s += ", running"
		case st.State.Terminated != nil:
			s += fmt.Sprintf(", terminated: exit code %d, reason %s", st.State.Terminated.ExitCode, st.State.Terminated.Reason)
		}
	}
	return s
}

// startPod deletes a pod liner that is left and waits until it is gone, then
// creates the pod liner with args and waits, at most 2 minutes, until its
// container runs or has ended. It returns the pod as last read.
func (p *proof) startPod(args ...string) (*corev1.Pod, error) {
	pods := p.l.Client().CoreV1().Pods(namespace)
	zero := int64(0)
	if err := pods.Delete(p.ctx, podName, metav1.DeleteOptions{GracePeriodSeconds: &zero}); err != nil && !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("deleting the pod %s that was left: %w", podName, err)
	}
	var getErr error
	if err := p.poll(2*time.Minute, func() bool {
		_, getErr = pods.Get(p.ctx, podName, metav1.GetOptions{})
		return apierrors.IsNotFound(getErr)
	}); err != nil {
		return nil, fmt.Errorf("waiting until the pod %s that was left is gone: %w (last: %v)", podName, err, getErr)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: namespace},
		Spec: corev1.PodSpec{
			NodeName:      p.worker,
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:            container,
				Image:           p.image,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Args:            args,
			}},
		},
	}
	start := time.Now()
	if _, err := pods.Create(p.ctx, pod, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("creating the pod %s: %w", podName, err)
	}
	var last *corev1.Pod
	err := p.poll(2*time.Minute, func() bool {
		got, err := pods.Get(p.ctx, podName, metav1.GetOptions{})
		if err != nil {
			return false
		}
		last = got
		st := probeStatus(got)
		return st != nil && (st.State.Running != nil || st.State.Terminated != nil)
	})
	if err != nil {
		return last, fmt.Errorf("the container %s did not run or end: %w; last seen: %s", container, err, describe(last))
	}
	info("pod %s with %s: %s, %v after it was created; uid %s", podName, strings.Join(args, " "), describe(last), time.Since(start).Round(time.Millisecond), last.UID)
	return last, nil
}

// ended polls the pod, for at most limit, until its container has ended. It
// returns the container's terminated state, or nil when it still runs.
func (p *proof) ended(limit time.Duration) (*corev1.ContainerStateTerminated, error) {
	var term *corev1.ContainerStateTerminated
	var getErr error
	err := p.poll(limit, func() bool {
		var got *corev1.Pod
		got, getErr = p.l.Client().CoreV1().Pods(namespace).Get(p.ctx, podName, metav1.GetOptions{})
		if st := probeStatus(got); getErr == nil && st != nil && st.State.Terminated != nil {
			term = st.State.Terminated
			return true
		}
		return false
	})
	switch {
	case term != nil:
		return term, nil
	case p.ctx.Err() != nil:
		return nil, p.ctx.Err()
	case getErr != nil:
		return nil, fmt.Errorf("reading the pod %s: %w", podName, getErr)
	case err != nil && !errors.Is(err, errTimeUp):
		return nil, err
	}
	return nil, nil
}

// logDir is the folder in the worker that holds the container's log files.
func logDir(uid types.UID) string {
	return "/var/log/pods/" + namespace + "_" + podName + "_" + string(uid) + "/" + container + "/"
}

// listing runs ls -la DIR in the worker.
func (p *proof) listing(dir string) (string, error) {
	return p.l.Exec(p.worker, nil, "ls", "-la", dir)
}

// rotatedIn returns the names in an ls -la listing that start with 0.log.
func rotatedIn(listing string) []string {
	var names []string
	for line := range strings.Lines(listing) {
		f := strings.Fields(line)
		if len(f) > 0 && strings.HasPrefix(f[len(f)-1], "0.log.") {
			names = append(names, f[len(f)-1])
		}
	}
	return names
}

// printListing prints an ls -la listing, one info line for each of its lines.
func printListing(label, dir, listing string) {
	info("%s: ls -la %s in the worker:", label, dir)
	for line := range strings.Lines(strings.TrimRight(listing, "\n")) {
		info("%s:   %s", label, strings.TrimRight(line, "\n"))
	}
}

// line is one line of a log stream, split at its first space.
type line struct {
	raw   string    // as received, its line end included
	stamp string    // the timestamp as received
	ts    time.Time // stamp, parsed with time.RFC3339Nano
	text  string    // what follows the first space, without the line end
	n     int       // detail.i of text, 0 when it has none
}

// parseLine splits a line at its first space into the timestamp and the text,
// and takes N from detail.i of the text.
func parseLine(s string) (line, error) {
	stamp, text, found := strings.Cut(strings.TrimSuffix(s, "\n"), " ")
	if !found {
		return line{}, fmt.Errorf("no space in %q", short(s))
	}
	ts, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return line{}, fmt.Errorf("the timestamp of %q: %w", short(s), err)
	}
	l := line{raw: s, stamp: stamp, ts: ts, text: text}
	var v struct {
		Detail struct {
			I int `json:"i"`
		} `json:"detail"`
	}
	if json.Unmarshal([]byte(text), &v) == nil {
		l.n = v.Detail.I
	}
	return l, nil
}

// short returns s, cut to 200 bytes.
func short(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// rule is the reopening rule's state, kept across the streams of one
// experiment.
type rule struct {
	last    time.Time // LAST: the timestamp of the last line taken
	at      int       // AT: how many lines taken carry exactly LAST
	lastN   int       // N of the last line taken
	taken   int       // lines taken, in all streams
	streams int       // streams opened
	bad     []string  // lines that could not be split, or whose timestamp could not be parsed
}

// reopen is what the rule noted about one stream that it opened again.
type reopen struct {
	last    time.Time // LAST when the stream was opened
	at      int       // AT when the stream was opened
	lastN   int       // N of the last line taken before the stream was opened
	first   *line     // the first line of the stream that could be read; nil when it returned none
	atLast  int       // how many lines the stream returned that carry exactly last
	skipped int       // how many lines the rule skipped
}

// gap reports whether the rule saw lines missing: the first line returned is
// later than LAST, or fewer than AT lines at LAST came back.
func (r reopen) gap() bool {
	return (r.first != nil && r.first.ts.After(r.last)) || r.atLast < r.at
}

// end is how one stream ended.
type end struct {
	err   error  // what ReadString returned last; io.EOF when the server ended the stream
	piece string // a last piece with no line end, as received; "" when there was none
	cut   bool   // the stream's context was cancelled: the experiment cut it
}

// read opens a stream of the pod's log by the rule, calls opened once it is
// open, and hands each line it takes to take; take cuts the stream by
// returning true. The stream also ends when the server ends it or when ctx
// is cancelled. For a stream opened again, read returns the rule's notes.
func (p *proof) read(ctx context.Context, r *rule, opened func(), take func(line) bool) (end, *reopen, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	opts := &corev1.PodLogOptions{Container: container, Follow: true, Timestamps: true}
	var note *reopen
	if r.streams > 0 {
		since := metav1.NewTime(r.last.Truncate(time.Second))
		opts.SinceTime = &since
		note = &reopen{last: r.last, at: r.at, lastN: r.lastN}
	}
	body, err := p.l.Client().CoreV1().Pods(namespace).GetLogs(podName, opts).Stream(ctx)
	if err != nil {
		return end{}, nil, fmt.Errorf("opening stream %d: %w", r.streams+1, err)
	}
	defer body.Close()
	r.streams++
	if opened != nil {
		opened()
	}
	br := bufio.NewReader(body)
	for {
		s, err := br.ReadString('\n')
		if ctx.Err() != nil {
			// Cut: what arrived after the cut is not taken.
			return end{err: err, cut: true}, note, nil
		}
		if err != nil {
			return end{err: err, piece: s}, note, nil
		}
		l, perr := parseLine(s)
		if perr != nil {
			r.bad = append(r.bad, perr.Error())
			continue
		}
		if note != nil {
			if note.first == nil {
				first := l
				note.first = &first
			}
			if l.ts.Before(note.last) {
				note.skipped++
				continue
			}
			if l.ts.Equal(note.last) {
				note.atLast++
				if note.atLast <= note.at {
					note.skipped++
					continue
				}
			}
		}
		switch {
		case l.ts.After(r.last):
			r.last, r.at = l.ts, 1
		case l.ts.Equal(r.last):
			r.at++
		}
		r.lastN = l.n
		r.taken++
		if take != nil && take(l) {
			return end{cut: true}, note, nil
		}
	}
}

// plan is how an experiment cuts its streams. Every field may be left out.
type plan struct {
	// arm is called once a stream is open, with the number of cuts so far and
	// the stream's cancel function; the function it returns disarms it.
	arm func(cuts int, cancel context.CancelFunc) (disarm func())
	// take is called with the number of cuts so far and each line taken;
	// true cuts the stream.
	take func(cuts int, l line) bool
	// pause is the wait between a cut and the next open.
	pause time.Duration
	// once: a stream that ends while the container runs is not opened again.
	once bool
}

// followed is what following the log by the rule gave.
type followed struct {
	cuts      int
	unplanned int                              // streams that ended while the container still ran
	notes     []reopen                         // one for each stream opened again
	last      end                              // how the last stream ended
	term      *corev1.ContainerStateTerminated // the container's state once it had ended; nil if it had not
}

// follow reads the pod's log by the rule until a stream ends after the
// container has ended. It opens a stream again after each cut, and after a
// stream that ended while the container still ran unless the plan says once.
func (p *proof) follow(r *rule, pl plan) (followed, error) {
	var f followed
	for {
		if r.streams >= 20 {
			return f, errors.New("20 streams were opened and the log has not ended")
		}
		sctx, cancel := context.WithCancel(p.ctx)
		var disarm func()
		cuts := f.cuts
		opened := func() {
			if pl.arm != nil {
				disarm = pl.arm(cuts, cancel)
			}
		}
		take := func(l line) bool { return pl.take != nil && pl.take(cuts, l) }
		e, note, err := p.read(sctx, r, opened, take)
		if disarm != nil {
			disarm()
		}
		cancel()
		if note != nil {
			f.notes = append(f.notes, *note)
		}
		if p.ctx.Err() != nil {
			return f, p.ctx.Err()
		}
		if err != nil {
			return f, err
		}
		f.last = e
		if e.cut {
			f.cuts++
			select {
			case <-p.ctx.Done():
				return f, p.ctx.Err()
			case <-time.After(pl.pause):
			}
			continue
		}
		term, err := p.ended(5 * time.Second)
		if err != nil {
			return f, err
		}
		if term != nil {
			f.term = term
			return f, nil
		}
		f.unplanned++
		info("stream %d ended (%v) while the container still ran", r.streams, e.err)
		if pl.once {
			return f, nil
		}
	}
}

// tally counts how often each N from 1 to n was taken.
type tally struct {
	n         int
	seen      []uint16
	lines     int
	outside   int // lines whose N is not in 1 to n
	outOfStep int // lines whose N is not one more than the N before
	backwards int // lines whose timestamp is before the one before
	prevN     int
	prevTS    time.Time
	first     string // the first timestamp, as received
	last      string // the last timestamp, as received
}

func newTally(n int) *tally { return &tally{n: n, seen: make([]uint16, n+1)} }

func (t *tally) add(l line) {
	if t.lines == 0 {
		t.first = l.stamp
	} else if l.ts.Before(t.prevTS) {
		t.backwards++
	}
	t.last = l.stamp
	t.prevTS = l.ts
	t.lines++
	if l.n != t.prevN+1 {
		t.outOfStep++
	}
	t.prevN = l.n
	if l.n < 1 || l.n > t.n {
		t.outside++
		return
	}
	if t.seen[l.n] < 65535 {
		t.seen[l.n]++
	}
}

// repeats returns how many lines were taken again for an N already taken.
func (t *tally) repeats() int {
	r := 0
	for _, s := range t.seen[1:] {
		if s > 1 {
			r += int(s) - 1
		}
	}
	return r
}

// missing returns how many N from 1 to n were never taken, and the first ten
// ranges of them.
func (t *tally) missing() (int, string) {
	count := 0
	var ranges []string
	more := 0
	for i := 1; i <= t.n; i++ {
		if t.seen[i] != 0 {
			continue
		}
		j := i
		for j < t.n && t.seen[j+1] == 0 {
			j++
		}
		count += j - i + 1
		if len(ranges) < 10 {
			if i == j {
				ranges = append(ranges, strconv.Itoa(i))
			} else {
				ranges = append(ranges, fmt.Sprintf("%d-%d", i, j))
			}
		} else {
			more++
		}
		i = j
	}
	if count == 0 {
		return 0, "none"
	}
	s := strings.Join(ranges, ", ")
	if more > 0 {
		s += fmt.Sprintf(", and %d more ranges", more)
	}
	return count, s
}

// missingBounds returns the first and the last N from 1 to n that were never
// taken, or 0 and 0 when none is missing.
func (t *tally) missingBounds() (first, last int) {
	for i := 1; i <= t.n; i++ {
		if t.seen[i] == 0 {
			if first == 0 {
				first = i
			}
			last = i
		}
	}
	return first, last
}

// everyOnce reports whether every N from 1 to n was taken exactly once, in
// order, and nothing else was taken.
func (t *tally) everyOnce() bool {
	missing, _ := t.missing()
	return t.lines == t.n && missing == 0 && t.repeats() == 0 && t.outside == 0 && t.outOfStep == 0
}

// summarize prints the tally as one info line.
func (t *tally) summarize(label string) {
	missing, ranges := t.missing()
	info("%s: %d lines taken; missing N: %d (%s); repeats: %d; N outside 1 to %d: %d; lines whose N is not one more than the N before: %d; timestamps before the one before: %d",
		label, t.lines, missing, ranges, t.repeats(), t.n, t.outside, t.outOfStep, t.backwards)
}

// printNotes prints the rule's notes of each stream opened again.
func printNotes(label string, notes []reopen) {
	for i, n := range notes {
		info("%s: reopen %d: LAST %s (N %d), AT %d; first line returned: %s; lines at LAST that came back: %d of AT %d; lines skipped: %d; the rule saw a gap: %t",
			label, i+1, n.last.Format(time.RFC3339Nano), n.lastN, n.at, firstLine(n.first), n.atLast, n.at, n.skipped, n.gap())
	}
}

// firstLine describes the first line a stream returned.
func firstLine(l *line) string {
	if l == nil {
		return "none"
	}
	if l.n > 0 {
		return fmt.Sprintf("N %d at %s", l.n, l.stamp)
	}
	return fmt.Sprintf("%q at %s", short(l.text), l.stamp)
}

// experimentA runs A with -n 150000 and doubles -n while no rotated file
// appears, at most twice. It returns the arguments and -n with which the log
// rotated, or nil.
func (p *proof) experimentA() ([]string, int) {
	allHeld := true
	for run, n := 1, 150000; run <= 3; run, n = run+1, n*2 {
		args := []string{"-n", strconv.Itoa(n), "-rate", "15000", "-pad", "180"}
		label := fmt.Sprintf("A, -n %d", n)
		held, rotated, err := p.runA(label, n, args)
		if err != nil {
			p.c.fail("%s: %v", label, err)
			return nil, 0
		}
		allHeld = allHeld && held
		if rotated {
			info("A: the -n that rotated: %d", n)
			p.c.slot["A"] = "FAILED"
			if allHeld {
				p.c.slot["A"] = "ok"
			}
			return args, n
		}
		if run < 3 {
			info("%s: no rotated file; doubling -n", label)
		}
	}
	p.c.fail("A: no rotated file after -n 150000, 300000 and 600000")
	p.c.slot["A"] = "FAILED"
	return nil, 0
}

// runA runs A once with -n n: one stream from the start to its end.
func (p *proof) runA(label string, n int, args []string) (held, rotated bool, err error) {
	pod, err := p.startPod(args...)
	if err != nil {
		return false, false, err
	}
	r := &rule{}
	t := newTally(n)
	f, err := p.follow(r, plan{take: func(_ int, l line) bool { t.add(l); return false }, once: true})
	if err != nil {
		return false, false, err
	}
	dir := logDir(pod.UID)
	listing, err := p.listing(dir)
	if err != nil {
		return false, false, err
	}
	info("%s: first timestamp %s, last timestamp %s, as received", label, t.first, t.last)
	t.summarize(label)
	printListing(label, dir, listing)
	held = p.c.verdict(label,
		cond{f.unplanned == 0 && f.term != nil, "the one stream ended after the container had ended"},
		cond{errors.Is(f.last.err, io.EOF), fmt.Sprintf("the stream ended with io.EOF (it ended with %v)", f.last.err)},
		cond{t.everyOnce() && len(r.bad) == 0, fmt.Sprintf("N runs from 1 to %d with no gap and no repeat", n)},
		cond{t.backwards == 0, "no timestamp is before the one before it"},
	)
	return held, len(rotatedIn(listing)) > 0, nil
}

// experimentB cuts the stream at 5, 12 and 19 seconds after it first opened,
// and opens it again by the rule 2 seconds after each cut.
func (p *proof) experimentB() {
	const n = 60000
	if _, err := p.startPod("-n", strconv.Itoa(n), "-rate", "2000"); err != nil {
		p.c.fail("B: %v", err)
		return
	}
	r := &rule{}
	t := newTally(n)
	marks := []time.Duration{5 * time.Second, 12 * time.Second, 19 * time.Second}
	var t0 time.Time
	f, err := p.follow(r, plan{
		arm: func(cuts int, cancel context.CancelFunc) func() {
			if t0.IsZero() {
				t0 = time.Now()
			}
			if cuts >= len(marks) {
				return nil
			}
			timer := time.AfterFunc(time.Until(t0.Add(marks[cuts])), cancel)
			return func() { timer.Stop() }
		},
		take:  func(_ int, l line) bool { t.add(l); return false },
		pause: 2 * time.Second,
	})
	if err != nil {
		p.c.fail("B: %v", err)
		return
	}
	printNotes("B", f.notes)
	t.summarize("B")
	p.c.slot["B"] = "FAILED"
	if p.c.verdict("B",
		cond{f.cuts == 3, fmt.Sprintf("the stream was cut and opened again three times (cuts: %d)", f.cuts)},
		cond{t.everyOnce() && len(r.bad) == 0, "every N from 1 to 60,000 read exactly once"},
	) {
		p.c.slot["B"] = "ok"
	}
}

// experimentC runs the arguments with which A rotated, cuts the stream when
// a rotated file appears, waits 12 seconds and opens it again by the rule.
// It records what came back; it judges nothing.
func (p *proof) experimentC(args []string, n int) {
	cut, ok := p.cutAtRotation("C", args, n)
	if !ok {
		return
	}
	note := cut.notes[0]
	info("C: the lines lost: %d; the missing N: %s", cut.missing, cut.ranges)
	info("C: the last line read before the cut: N %d at %s; AT %d", note.lastN, note.last.Format(time.RFC3339Nano), note.at)
	info("C: the first line returned after the reopen: %s", firstLine(note.first))
	info("C: the lines at LAST that came back: %d, against AT %d", note.atLast, note.at)
	info("C: the rule saw a gap: %t", cut.gap)
	p.c.slot["C"] = cut.text()
}

// experimentC2 runs C's procedure with A's arguments and -n 300000, so that
// the liner still writes when the kubelet looks again after the cut and the
// lines after the break land in a rotated file. When no line is lost, it
// runs once more with -n 600000. It records what came back; it judges
// nothing, and its results are info lines only.
func (p *proof) experimentC2() {
	for _, n := range []int{300000, 600000} {
		label := fmt.Sprintf("C2, -n %d", n)
		cut, ok := p.cutAtRotation(label, []string{"-n", strconv.Itoa(n), "-rate", "15000", "-pad", "180"}, n)
		if !ok {
			return
		}
		note := cut.notes[0]
		lost := "none"
		if cut.missing > 0 {
			first, last := cut.t.missingBounds()
			lost = fmt.Sprintf("the first missing N %d, the last missing N %d", first, last)
		}
		info("%s: the lines lost: %d; %s", label, cut.missing, lost)
		info("%s: the last line read before the cut: N %d at %s; AT %d", label, note.lastN, note.last.Format(time.RFC3339Nano), note.at)
		info("%s: the first line returned after the reopen: %s", label, firstLine(note.first))
		info("%s: the lines at LAST that came back: %d, against AT %d", label, note.atLast, note.at)
		info("%s: the rule saw a gap: %t", label, cut.gap)
		info("%s: %s", label, cut.text())
		p.c.slot["C2"] = cut.text()
		if cut.missing > 0 {
			return
		}
	}
}

// rotationCut is what one run of C's procedure gave.
type rotationCut struct {
	t       *tally
	notes   []reopen // at least one: the reopen after the cut comes first
	missing int      // how many N from 1 to n were never taken
	ranges  string   // the first ten ranges of them
	gap     bool     // the rule saw a gap at some reopen
}

// text is C's TEXT for the result: no line lost, N lines lost, gap seen, or
// N lines lost, gap not seen.
func (c rotationCut) text() string {
	switch {
	case c.missing == 0:
		return "no line lost"
	case c.gap:
		return fmt.Sprintf("%d lines lost, gap seen", c.missing)
	default:
		return fmt.Sprintf("%d lines lost, gap not seen", c.missing)
	}
}

// cutAtRotation runs C's procedure with args: it follows the log from the
// start and runs ls every 500 ms while the first stream is open; when a
// rotated file appears it cuts the stream, waits 12 seconds, opens it again
// by the rule and follows it to the end. It prints the listings at the cut
// and at the end, the rule's notes and the tally. When the procedure cannot
// run to its end, it prints a FAILED line and returns false.
func (p *proof) cutAtRotation(label string, args []string, n int) (rotationCut, bool) {
	pod, err := p.startPod(args...)
	if err != nil {
		p.c.fail("%s: %v", label, err)
		return rotationCut{}, false
	}
	dir := logDir(pod.UID)
	r := &rule{}
	t := newTally(n)
	var seen string // the listing that showed a rotated file
	var seenAt time.Time
	var polls int
	var pollErr error
	f, err := p.follow(r, plan{
		arm: func(cuts int, cancel context.CancelFunc) func() {
			if cuts > 0 {
				return nil
			}
			stop := make(chan struct{})
			done := make(chan struct{})
			go func() {
				defer close(done)
				tick := time.NewTicker(500 * time.Millisecond)
				defer tick.Stop()
				for {
					select {
					case <-stop:
						return
					case <-tick.C:
					}
					polls++
					out, err := p.listing(dir)
					if err != nil {
						pollErr = err
						continue
					}
					if len(rotatedIn(out)) > 0 {
						seen, seenAt = out, time.Now()
						cancel()
						return
					}
				}
			}()
			return func() { close(stop); <-done }
		},
		take:  func(_ int, l line) bool { t.add(l); return false },
		pause: 12 * time.Second,
	})
	if err != nil {
		p.c.fail("%s: %v", label, err)
		return rotationCut{}, false
	}
	if f.cuts == 0 {
		p.c.fail("%s: no rotated file appeared in %d polls while the first stream was open, so the stream was not cut (last poll error: %v)", label, polls, pollErr)
		return rotationCut{}, false
	}
	info("%s: a rotated file appeared at poll %d, %s", label, polls, seenAt.UTC().Format(time.RFC3339Nano))
	printListing(label+", at the cut", dir, seen)
	if final, err := p.listing(dir); err == nil {
		printListing(label+", at the end", dir, final)
	} else {
		info("%s, at the end: ls -la %s in the worker failed: %v", label, dir, err)
	}
	printNotes(label, f.notes)
	t.summarize(label)
	cut := rotationCut{t: t, notes: f.notes}
	cut.missing, cut.ranges = t.missing()
	for _, note := range f.notes {
		cut.gap = cut.gap || note.gap()
	}
	return cut, true
}

// experimentD writes 50,000 identical lines as fast as it can, cuts the
// stream after 10,000 and after 30,000 lines read, and opens it again by the
// rule each time.
func (p *proof) experimentD() {
	const n = 50000
	if _, err := p.startPod("-same", "-n", strconv.Itoa(n), "-rate", "0"); err != nil {
		p.c.fail("D: %v", err)
		return
	}
	r := &rule{}
	cutAfter := []int{10000, 30000}
	stamps := map[int64]int{}
	other := 0
	f, err := p.follow(r, plan{
		take: func(cuts int, l line) bool {
			stamps[l.ts.UnixNano()]++
			if l.text != sameText {
				other++
			}
			return cuts < len(cutAfter) && r.taken == cutAfter[cuts]
		},
	})
	if err != nil {
		p.c.fail("D: %v", err)
		return
	}
	largest := 0
	for _, k := range stamps {
		largest = max(largest, k)
	}
	printNotes("D", f.notes)
	info("D: %d lines read; %d distinct timestamps; lines whose text is not %s: %d", r.taken, len(stamps), sameText, other)
	info("D: the largest number of lines that shared one timestamp: %d", largest)
	p.c.slot["D"] = "FAILED"
	if p.c.verdict("D",
		cond{f.cuts == 2, fmt.Sprintf("the stream was cut after 10,000 and after 30,000 lines read and opened again each time (cuts: %d)", f.cuts)},
		cond{r.taken == n && len(r.bad) == 0, fmt.Sprintf("exactly 50,000 lines read (read: %d)", r.taken)},
	) {
		p.c.slot["D"] = "ok"
	}
}

// experimentE follows the log of -n 10 -tail to the end of the stream and
// records its last piece and the container's terminated state.
func (p *proof) experimentE() {
	if _, err := p.startPod("-n", "10", "-tail"); err != nil {
		p.c.fail("E: %v", err)
		return
	}
	r := &rule{}
	var firstRaw, lastRaw string
	f, err := p.follow(r, plan{
		take: func(_ int, l line) bool {
			if firstRaw == "" {
				firstRaw = l.raw
			}
			lastRaw = l.raw
			return false
		},
		once: true,
	})
	if err != nil {
		p.c.fail("E: %v", err)
		return
	}
	piece := lastRaw
	if f.last.piece != "" {
		piece = f.last.piece
	}
	info("E: the first line as received: %q", firstRaw)
	info("E: %d lines taken, each with its line end", r.taken)
	info("E: the last piece as received: %q", piece)
	info("E: it ends with \\n: %t", strings.HasSuffix(piece, "\n"))
	stamp, _, _ := strings.Cut(piece, " ")
	if ts, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
		info("E: its timestamp %q could not be parsed: %v", stamp, err)
	} else {
		info("E: its timestamp: %s", ts.Format(time.RFC3339Nano))
	}
	info("E: the error that ended the stream: %v (io.EOF: %t)", f.last.err, errors.Is(f.last.err, io.EOF))
	term := f.term
	if term == nil {
		if term, err = p.ended(30 * time.Second); err != nil {
			p.c.fail("E: %v", err)
			return
		}
	}
	if term == nil {
		p.c.fail("E: the container had not ended 30 seconds after the stream ended")
		return
	}
	info("E: the container's terminated state: exit code %d, reason %s", term.ExitCode, term.Reason)
	p.c.slot["E"] = "recorded"
}

// summary counts each experiment that did not run to its end, and an
// interrupted run, as failed; prints the last line; and returns the exit code.
func summary(ctx context.Context, c *checks) int {
	if ctx.Err() != nil {
		c.fail("the run was interrupted")
	}
	parts := make([]string, 0, len(experiments))
	slotFailed := false
	for _, x := range experiments {
		word, done := c.slot[x]
		if !done {
			c.fail("experiment %s did not run to its end", x)
			word = "FAILED"
		}
		slotFailed = slotFailed || word == "FAILED"
		parts = append(parts, x+" "+word)
	}
	for _, x := range infoOnly {
		if _, done := c.slot[x]; !done {
			c.fail("experiment %s did not run to its end", x)
		}
	}
	last := "p11: " + strings.Join(parts, ", ")
	if c.failed > 0 && !slotFailed {
		last += fmt.Sprintf("; %d other checks failed", c.failed)
	}
	fmt.Println(last)
	if c.failed > 0 {
		return 1
	}
	return 0
}
