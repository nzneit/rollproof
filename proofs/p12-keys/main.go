//go:build linux

// Proof of point 12: how many kernel keys of the user one cluster of four
// nodes holds on a rootless engine through an install and two rollouts,
// without and with NoNewKeyring = true for the runc runtime in the nodes'
// containerd. Each round makes its own cluster; the last line gives both
// peaks above the round's baseline and the user's key quota.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"

	"rollproof-proofs/internal/lab"
)

const (
	// keyringOff is round 1's patch. Because it mentions NoNewKeyring, the
	// lab adds no patch of its own.
	keyringOff = "[plugins.\"io.containerd.grpc.v1.cri\".containerd.runtimes.runc.options]\n  NoNewKeyring = false\n"

	cutOff = 180 // a round stops early when QNKEYS passes this
	// sampleEvery is the sampler's interval. While a node shuts down, the
	// user's keys rise by about 50 for a few tens of milliseconds, which a
	// sampler every 500 ms mostly misses (see the record).
	sampleEvery = 5 * time.Millisecond

	namespace  = "p12"
	deployment = "load"
	replicas   = 6
	pauseRepo  = "registry.k8s.io/pause"
)

func main() { os.Exit(run()) }

func info(format string, args ...any) { fmt.Printf("info: "+format+"\n", args...) }

// reading is the user's line of /proc/key-users: the keys the user holds
// (QNKEYS) and the most the user may hold (MAXKEYS).
type reading struct{ keys, max int }

func (k reading) String() string { return fmt.Sprintf("%d/%d", k.keys, k.max) }

// readKeys reads QNKEYS/MAXKEYS from the line of /proc/key-users whose first
// field is the user's UID and a colon. The line's form is
// "UID: USAGE NKEYS/NIKEYS QNKEYS/MAXKEYS QNBYTES/MAXBYTES".
func readKeys() (reading, error) {
	f, err := os.Open("/proc/key-users")
	if err != nil {
		return reading{}, err
	}
	defer f.Close()
	uid := strconv.Itoa(os.Getuid()) + ":"
	for sc := bufio.NewScanner(f); sc.Scan(); {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 || fields[0] != uid {
			continue
		}
		keys, quota, ok := strings.Cut(fields[3], "/")
		k, kerr := strconv.Atoi(keys)
		m, merr := strconv.Atoi(quota)
		if !ok || kerr != nil || merr != nil {
			return reading{}, fmt.Errorf("/proc/key-users: cannot read QNKEYS/MAXKEYS from %q", sc.Text())
		}
		return reading{keys: k, max: m}, nil
	}
	return reading{}, fmt.Errorf("/proc/key-users has no line for UID %d", os.Getuid())
}

// round is one cluster's measurement.
type round struct {
	n        int
	patches  []string // the description's containerd patches
	failed   int
	baseline reading
	peak     int  // the highest QNKEYS of every reading and sample of the round
	max      int  // MAXKEYS of the last reading
	early    bool // the sampler stopped the round
}

func (r *round) fail(format string, args ...any) {
	r.failed++
	fmt.Printf("FAILED: round %d: "+format+"\n", append([]any{r.n}, args...)...)
}

// read takes the reading of one stage and prints it.
func (r *round) read(stage string) {
	k, err := readKeys()
	if err != nil {
		r.fail("%s: %v", stage, err)
		return
	}
	r.peak, r.max = max(r.peak, k.keys), k.max
	info("round %d, %s: %s", r.n, stage, k)
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rounds := []*round{
		{n: 1, patches: []string{keyringOff}}, // without the option
		{n: 2},                                // with the option
	}
	failed := 0
	for _, r := range rounds {
		if notRootless := r.measure(ctx); notRootless {
			fmt.Println("not a rootless engine: nothing to measure")
			return 0
		}
		early := "no"
		if r.early {
			early = "yes"
		}
		info("round %d peak %d, above baseline %d", r.n, r.peak, r.peak-r.baseline.keys)
		info("round %d stopped early: %s", r.n, early)
		failed += r.failed
		if ctx.Err() != nil {
			break
		}
	}
	if failed > 0 {
		fmt.Printf("p12: %d steps failed\n", failed)
		return 1
	}
	fmt.Printf("p12: peak without the option %d, with the option %d, quota %d\n",
		rounds[0].peak-rounds[0].baseline.keys, rounds[1].peak-rounds[1].baseline.keys, rounds[1].max)
	return 0
}

// measure runs one round from the baseline to the reading after Down. It
// reports whether round 1 found an engine that runs as root.
func (r *round) measure(parent context.Context) (notRootless bool) {
	base, err := readKeys()
	if err != nil {
		r.fail("baseline: %v", err)
		return false
	}
	r.baseline, r.peak, r.max = base, base.keys, base.max
	info("round %d, baseline: %s", r.n, base)

	cfg := &v1alpha4.Cluster{
		Nodes:                   []v1alpha4.Node{{Role: v1alpha4.ControlPlaneRole}},
		ContainerdConfigPatches: r.patches,
	}
	for range 3 {
		cfg.Nodes = append(cfg.Nodes, v1alpha4.Node{Role: v1alpha4.WorkerRole})
	}

	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	l, err := lab.Up(ctx, "p12", cfg)
	if err != nil {
		r.fail("lab.Up: %v", err)
		r.read("after Down")
		return false
	}
	// The sampler may call down too; a later call returns the first one's
	// result. Every return below calls down and reports its error, so the
	// deferred call is there for a panic.
	down := sync.OnceValue(l.Down)
	defer down()
	r.read("after Up")

	if r.n == 1 {
		rootless, err := isRootless(ctx, l.Engine())
		switch {
		case err != nil:
			r.fail("%v", err)
		case !rootless:
			if err := down(); err != nil {
				fmt.Fprintf(os.Stderr, "p12: %v\n", err)
			}
			return true
		}
	}

	s := startSampler(cancel, down)
	if err := r.load(ctx, l); err != nil {
		if s.early.Load() {
			info("round %d: QNKEYS passed %d; the round stopped: %v", r.n, cutOff, err)
		} else {
			r.fail("%v", err)
		}
	}
	if err := down(); err != nil {
		r.fail("l.Down(): %v", err)
	}
	s.stop()
	if s.err != nil {
		r.fail("the sampler: %v", s.err)
	}
	r.peak, r.early = max(r.peak, s.peak), s.early.Load()
	r.read("after Down")
	return false
}

// isRootless reports whether an entry of SecurityOptions in
// `ENGINE info --format "{{json .}}"` contains rootless.
func isRootless(ctx context.Context, engine string) (bool, error) {
	out, err := exec.CommandContext(ctx, engine, "info", "--format", "{{json .}}").Output()
	if err != nil {
		return false, fmt.Errorf("%s info --format {{json .}}: %w", engine, err)
	}
	var engineInfo struct{ SecurityOptions []string }
	if err := json.Unmarshal(out, &engineInfo); err != nil {
		return false, fmt.Errorf("reading %s info: %w", engine, err)
	}
	return slices.ContainsFunc(engineInfo.SecurityOptions, func(s string) bool { return strings.Contains(s, "rootless") }), nil
}

// load runs the install and the two rollouts, with a reading after each. It
// returns at the first step that fails.
func (r *round) load(ctx context.Context, l *lab.Lab) error {
	tag, err := pauseTag(l)
	if err != nil {
		return err
	}
	image := pauseRepo + ":" + tag
	info("pause image %s", image)

	if _, err := l.Client().CoreV1().Namespaces().Create(ctx,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("creating the namespace %s: %w", namespace, err)
	}
	labels := map[string]string{"app": "load"}
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: deployment, Namespace: namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: new(int32(replicas)),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name:            "pause",
					Image:           image,
					ImagePullPolicy: corev1.PullIfNotPresent,
				}}},
			},
		},
	}
	deployments := l.Client().AppsV1().Deployments(namespace)
	if _, err := deployments.Create(ctx, d, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("creating the Deployment %s: %w", deployment, err)
	}
	if err := waitFor(ctx, l, "readyReplicas is 6", func(d *appsv1.Deployment) bool {
		return d.Status.ReadyReplicas == replicas
	}); err != nil {
		return err
	}
	r.read("6 ready")

	for k := 1; k <= 2; k++ {
		patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`,
			time.Now().Format(time.RFC3339))
		if _, err := deployments.Patch(ctx, deployment, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{}); err != nil {
			return fmt.Errorf("restart %d: patching the Deployment: %w", k, err)
		}
		if err := waitFor(ctx, l, fmt.Sprintf("restart %d is rolled out", k), func(d *appsv1.Deployment) bool {
			st := d.Status
			return st.ObservedGeneration >= d.Generation &&
				st.Replicas == replicas && st.UpdatedReplicas == replicas && st.AvailableReplicas == replicas
		}); err != nil {
			return err
		}
		r.read(fmt.Sprintf("after restart %d", k))
	}
	return nil
}

// pauseTag returns the tag of the line for registry.k8s.io/pause in
// `crictl images` in the first worker.
func pauseTag(l *lab.Lab) (string, error) {
	worker := l.Nodes()[1]
	out, err := l.Exec(worker, nil, "crictl", "images")
	if err != nil {
		return "", err
	}
	for line := range strings.Lines(out) {
		if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == pauseRepo {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("crictl images in %s lists no %s\n%s", worker, pauseRepo, out)
}

// waitFor polls the Deployment every second, for at most 5 minutes and while
// ctx lasts, until done is true of it.
func waitFor(ctx context.Context, l *lab.Lab, what string, done func(*appsv1.Deployment) bool) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		d, err := l.Client().AppsV1().Deployments(namespace).Get(ctx, deployment, metav1.GetOptions{})
		var last string
		if err != nil {
			last = err.Error()
		} else if done(d) {
			return nil
		} else {
			st := d.Status
			last = fmt.Sprintf("generation %d, observedGeneration %d, replicas %d, updatedReplicas %d, readyReplicas %d, availableReplicas %d",
				d.Generation, st.ObservedGeneration, st.Replicas, st.UpdatedReplicas, st.ReadyReplicas, st.AvailableReplicas)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting until %s: %w (last poll: %s)", what, ctx.Err(), last)
		case <-tick.C:
		}
	}
}

// sampler reads the user's keys every sampleEvery until stop. peak and err
// may be read only after stop has returned.
type sampler struct {
	early atomic.Bool // QNKEYS passed cutOff
	peak  int
	err   error // the first reading that failed
	quit  chan struct{}
	done  chan struct{}
}

// startSampler starts a sampler. When QNKEYS passes cutOff, it calls cancel
// and down at once.
func startSampler(cancel context.CancelFunc, down func() error) *sampler {
	s := &sampler{quit: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		tick := time.NewTicker(sampleEvery)
		defer tick.Stop()
		for {
			select {
			case <-s.quit:
				return
			case <-tick.C:
			}
			k, err := readKeys()
			if err != nil {
				if s.err == nil {
					s.err = err
				}
				continue
			}
			s.peak = max(s.peak, k.keys)
			if k.keys > cutOff && !s.early.Load() {
				s.early.Store(true)
				cancel()
				_ = down() // the round's own call to down reports its error
			}
		}
	}()
	return s
}

func (s *sampler) stop() {
	close(s.quit)
	<-s.done
}
