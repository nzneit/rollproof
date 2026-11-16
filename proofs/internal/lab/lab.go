//go:build linux

// A throwaway cluster for one proof: made through kind's library with the
// engine that kind would pick, with a kubeconfig file of its own, reachable
// by a Kubernetes client and by commands run in its nodes. For proofs only.
package lab

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cluster/constants"
	"sigs.k8s.io/kind/pkg/cluster/nodes"
	kindexec "sigs.k8s.io/kind/pkg/exec"
)

// noNewKeyring is the containerd patch that Up adds when the engine runs
// without root.
const noNewKeyring = `[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc.options]
  NoNewKeyring = true
`

// Lab is one proof's cluster, from Up to Down.
type Lab struct {
	provider   *cluster.Provider
	full       string // rollproof-proof-NAME
	dir        string // the temporary folder that holds the kubeconfig
	kubeconfig string
	client     *kubernetes.Clientset
	engine     string
	nodes      []nodes.Node // the control-plane node first
	containers []string     // the names that Container and Registry started
	down       bool
}

// Container is a container that Lab.Container started on the cluster's network.
type Container struct {
	Name    string // the engine's name for it: the cluster's name, a hyphen, and the name given
	Address string // its IPv4 address on the network kind
	engine  string
}

// Registry is a registry container that Lab.Registry started.
type Registry struct {
	Container
	Push string // 127.0.0.1:PORT, where the runner reaches it
	Node string // ADDRESS:5000, where the nodes reach it
}

// Logs returns what the container has written so far, standard output and
// standard error together.
func (c Container) Logs() (string, error) {
	out, err := exec.Command(c.engine, "logs", c.Name).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("lab: %s logs %s: %w\n%s", c.engine, c.Name, err, out)
	}
	return string(out), nil
}

// Creates the cluster rollproof-proof-NAME from cfg and returns when every
// node is ready. It first deletes a cluster of that name. The kubeconfig is
// a new file in a new temporary folder; the caller's own is not touched.
// When kind's Create fails at its wait for a node's log line, Up deletes the
// cluster and tries again, four attempts in all, and says so on standard error.
func Up(ctx context.Context, name string, cfg *v1alpha4.Cluster) (*Lab, error) {
	engine, err := pickEngine(ctx)
	if err != nil {
		return nil, err
	}
	if out, err := exec.CommandContext(ctx, engine, "info").CombinedOutput(); err != nil {
		return nil, fmt.Errorf("lab: %s info: %w\n%s", engine, err, out)
	}

	full := "rollproof-proof-" + name
	dir, err := os.MkdirTemp("", full+"-")
	if err != nil {
		return nil, fmt.Errorf("lab: %w", err)
	}
	l := &Lab{
		provider:   newProvider(engine),
		full:       full,
		dir:        dir,
		kubeconfig: filepath.Join(dir, "kubeconfig"),
		engine:     engine,
	}
	if err := l.up(ctx, cfg); err != nil {
		if derr := l.Down(); derr != nil {
			err = errors.Join(err, fmt.Errorf("lab: cleaning up after that: %w", derr))
		}
		return nil, err
	}
	return l, nil
}

// pickEngine returns the engine's command name, chosen as kind chooses it.
func pickEngine(ctx context.Context) (string, error) {
	switch p := os.Getenv("KIND_EXPERIMENTAL_PROVIDER"); p {
	case "docker", "podman", "nerdctl", "finch", "nerdctl.lima":
		return p, nil
	}
	// The test of IsAvailable in kind's docker, nerdctl and podman providers
	// (pkg/cluster/internal/providers/*/util.go at v0.33.0), in the order of
	// DetectNodeProvider: NAME -v exits 0 and prints one line, which starts
	// with the engine's banner.
	for _, e := range []struct{ name, banner string }{
		{"docker", "Docker version"},
		{"nerdctl", "nerdctl version"},
		{"podman", "podman version"},
	} {
		out, err := exec.CommandContext(ctx, e.name, "-v").Output()
		if err != nil {
			continue
		}
		var lines []string
		for sc := bufio.NewScanner(bytes.NewReader(out)); sc.Scan(); {
			lines = append(lines, sc.Text())
		}
		if len(lines) == 1 && strings.HasPrefix(lines[0], e.banner) {
			return e.name, nil
		}
	}
	return "", errors.New("lab: no container engine: none of docker, nerdctl and podman passes kind's test")
}

// newProvider returns kind's provider for the engine named.
func newProvider(engine string) *cluster.Provider {
	switch engine {
	case "docker":
		return cluster.NewProvider(cluster.ProviderWithDocker())
	case "podman":
		return cluster.NewProvider(cluster.ProviderWithPodman())
	default: // nerdctl, finch, nerdctl.lima
		return cluster.NewProvider(cluster.ProviderWithNerdctl(engine))
	}
}

// up does Up's work from the removal of leftovers on. Up calls Down when it
// fails.
func (l *Lab) up(ctx context.Context, cfg *v1alpha4.Cluster) error {
	if err := l.provider.Delete(l.full, l.kubeconfig); err != nil {
		names, lerr := l.provider.List()
		if lerr != nil || slices.Contains(names, l.full) {
			return fmt.Errorf("lab: deleting the leftover cluster %s: %w", l.full, withOutput(err))
		}
	}
	out, err := l.run(ctx, "ps", "-a", "--format", "{{.Names}}")
	if err != nil {
		return err
	}
	var leftovers []string
	for _, n := range strings.Fields(out) {
		if strings.HasPrefix(n, l.full+"-") {
			leftovers = append(leftovers, n)
		}
	}
	if len(leftovers) > 0 {
		if _, err := l.run(ctx, append([]string{"rm", "-f"}, leftovers...)...); err != nil {
			return err
		}
	}

	c := cfg.DeepCopy() // kind's defaulting writes into what it is given
	out, err = l.run(ctx, "info", "--format", "{{json .}}")
	if err != nil {
		return err
	}
	var info struct{ SecurityOptions []string }
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		return fmt.Errorf("lab: reading %s info: %w", l.engine, err)
	}
	rootless := slices.ContainsFunc(info.SecurityOptions, func(s string) bool { return strings.Contains(s, "rootless") })
	patched := slices.ContainsFunc(c.ContainerdConfigPatches, func(s string) bool { return strings.Contains(s, "NoNewKeyring") })
	if rootless && !patched {
		c.ContainerdConfigPatches = append(c.ContainerdConfigPatches, noNewKeyring)
	}

	// Through rootless nerdctl, kind's 30-second follow of a node's log
	// sometimes misses a line that the log already holds (proof p08). Only
	// that failure is tried again; start is the start of the last attempt.
	const attempts = 4
	var start time.Time
	for attempt := 1; ; attempt++ {
		start = time.Now()
		err := l.provider.Create(l.full,
			cluster.CreateWithV1Alpha4Config(c),
			cluster.CreateWithKubeconfigPath(l.kubeconfig),
			cluster.CreateWithWaitForReady(0),
			cluster.CreateWithRetain(false),
			cluster.CreateWithDisplayUsage(false),
			cluster.CreateWithDisplaySalutation(false),
		)
		if err == nil {
			break
		}
		createErr := fmt.Errorf("lab: creating the cluster %s: %w", l.full, withOutput(err))
		if attempt == attempts || !strings.Contains(err.Error(), "could not find a log line that matches") {
			return createErr
		}
		fmt.Fprintf(os.Stderr, "lab: Create attempt %d of %d failed at kind's wait for a node's log line; trying again\n", attempt, attempts)
		if err := l.provider.Delete(l.full, l.kubeconfig); err != nil {
			return errors.Join(createErr, fmt.Errorf("lab: deleting %s before attempt %d: %w", l.full, attempt+1, withOutput(err)))
		}
	}

	rc, err := clientcmd.BuildConfigFromFlags("", l.kubeconfig)
	if err != nil {
		return fmt.Errorf("lab: %w", err)
	}
	if l.client, err = kubernetes.NewForConfig(rc); err != nil {
		return fmt.Errorf("lab: %w", err)
	}
	// Create has defaulted c, so c.Nodes counts a node that kind added.
	if err := l.waitReady(ctx, start, len(c.Nodes)); err != nil {
		return err
	}

	ns, err := l.provider.ListNodes(l.full)
	if err != nil {
		return fmt.Errorf("lab: listing the nodes of %s: %w", l.full, withOutput(err))
	}
	controlPlane := map[string]bool{}
	for _, n := range ns {
		role, err := n.Role()
		if err != nil {
			return fmt.Errorf("lab: the role of node %s: %w", n, withOutput(err))
		}
		controlPlane[n.String()] = role == constants.ControlPlaneNodeRoleValue
	}
	slices.SortFunc(ns, func(a, b nodes.Node) int {
		if ca, cb := controlPlane[a.String()], controlPlane[b.String()]; ca != cb {
			if ca {
				return -1
			}
			return 1
		}
		return strings.Compare(a.String(), b.String())
	})
	l.nodes = ns
	return nil
}

// waitReady polls the nodes every second, for at most 5 minutes and while
// ctx lasts, until want nodes exist and each is Ready. It prints the two lab
// lines that Task 4 reads, with times counted from start.
func (l *Lab) waitReady(ctx context.Context, start time.Time, want int) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for first := true; ; first = false {
		ready, total, err := l.readyNodes(ctx)
		if first {
			fmt.Fprintf(os.Stderr, "lab: Create returned after %v; %d of %d nodes Ready\n", since(start), ready, want)
		}
		if err == nil && total == want && ready == want {
			fmt.Fprintf(os.Stderr, "lab: every node Ready after %v\n", since(start))
			return nil
		}
		select {
		case <-ctx.Done():
			last := fmt.Sprintf("%d nodes, %d Ready", total, ready)
			if err != nil {
				last = err.Error()
			}
			return fmt.Errorf("lab: waiting for %d Ready nodes in %s: %w (last poll: %s)", want, l.full, ctx.Err(), last)
		case <-tick.C:
		}
	}
}

// readyNodes returns how many nodes have the condition Ready with status
// True, and how many nodes there are.
func (l *Lab) readyNodes(ctx context.Context) (ready, total int, err error) {
	list, err := l.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, 0, err
	}
	for _, n := range list.Items {
		for _, c := range n.Status.Conditions {
			if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
				ready++
			}
		}
	}
	return ready, len(list.Items), nil
}

func since(start time.Time) time.Duration {
	return time.Since(start).Round(time.Millisecond)
}

// The path of this cluster's kubeconfig file.
func (l *Lab) Kubeconfig() string { return l.kubeconfig }

// A client for this cluster's API server.
func (l *Lab) Client() *kubernetes.Clientset { return l.client }

// The engine's command name: docker, nerdctl or podman.
func (l *Lab) Engine() string { return l.engine }

// The node names, the control-plane node first.
func (l *Lab) Nodes() []string {
	names := make([]string, len(l.nodes))
	for i, n := range l.nodes {
		names[i] = n.String()
	}
	return names
}

// Runs a command in a node and returns its output, standard error included.
// A non-zero exit is an error that carries the output.
func (l *Lab) Exec(node string, stdin io.Reader, args ...string) (string, error) {
	i := slices.IndexFunc(l.nodes, func(n nodes.Node) bool { return n.String() == node })
	if i < 0 {
		return "", fmt.Errorf("lab: %s has no node %s", l.full, node)
	}
	var out bytes.Buffer
	cmd := l.nodes[i].Command(args[0], args[1:]...)
	if stdin != nil {
		cmd.SetStdin(stdin)
	}
	cmd.SetStdout(&out)
	cmd.SetStderr(&out)
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("lab: %s in node %s: %w\n%s", strings.Join(args, " "), node, err, out.String())
	}
	return out.String(), nil
}

// Starts a container on the cluster's network. args are the engine's run
// arguments after the name and the network, the image included. Down
// removes it. The result has its address on that network and a Logs method.
func (l *Lab) Container(ctx context.Context, name string, args ...string) (Container, error) {
	full := l.full + "-" + name
	l.containers = append(l.containers, full)
	if _, err := l.run(ctx, append([]string{"run", "-d", "--name", full, "--network", "kind"}, args...)...); err != nil {
		return Container{}, err
	}
	out, err := l.run(ctx, "inspect", full)
	if err != nil {
		return Container{}, err
	}
	var inspected []struct {
		NetworkSettings struct {
			Networks map[string]struct{ IPAddress string }
		}
	}
	if err := json.Unmarshal([]byte(out), &inspected); err != nil {
		return Container{}, fmt.Errorf("lab: reading %s inspect %s: %w\n%s", l.engine, full, err, out)
	}
	if len(inspected) != 1 {
		return Container{}, fmt.Errorf("lab: %s inspect %s gave %d entries, want one\n%s", l.engine, full, len(inspected), out)
	}
	// nerdctl may name the entry after the interface instead of the network.
	networks := inspected[0].NetworkSettings.Networks
	n, ok := networks["kind"]
	if !ok && len(networks) == 1 {
		for _, only := range networks {
			n = only
		}
	}
	if n.IPAddress == "" {
		return Container{}, fmt.Errorf("lab: %s has no address on the network kind\n%s", full, out)
	}
	return Container{Name: full, Address: n.IPAddress, engine: l.engine}, nil
}

// Starts a registry container on the cluster's network, with env set in it.
// Push is its address on the runner's loopback, Node its address for the
// nodes, and Logs returns what it has logged so far.
func (l *Lab) Registry(ctx context.Context, name string, env ...string) (Registry, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Registry{}, fmt.Errorf("lab: %w", err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	ln.Close()

	args := []string{"-p", "127.0.0.1:" + port + ":5000"}
	for _, e := range env {
		args = append(args, "-e", e)
	}
	args = append(args, "docker.io/library/registry:3")
	c, err := l.Container(ctx, name, args...)
	if err != nil {
		return Registry{}, err
	}
	push := "127.0.0.1:" + port
	if err := waitForRegistry(ctx, push); err != nil {
		return Registry{}, err
	}
	return Registry{Container: c, Push: push, Node: net.JoinHostPort(c.Address, "5000")}, nil
}

// waitForRegistry returns when GET http://PUSH/v2/ answers 200, or an error
// after 30 seconds.
func waitForRegistry(ctx context.Context, push string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	url := "http://" + push + "/v2/"
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("lab: %w", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			err = errors.New(resp.Status)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("lab: GET %s did not answer 200 within 30 seconds: %v", url, err)
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// Deletes the cluster, every container that Container or Registry started,
// and the temporary folder. Safe to call twice.
func (l *Lab) Down() error {
	if l.down {
		return nil
	}
	l.down = true
	var first error
	keep := func(err error) {
		if first == nil {
			first = err
		}
	}
	if err := l.provider.Delete(l.full, l.kubeconfig); err != nil {
		keep(fmt.Errorf("lab: deleting the cluster %s: %w", l.full, withOutput(err)))
	}
	ctx := context.Background()
	removed := map[string]bool{}
	for _, name := range l.containers {
		if removed[name] {
			continue
		}
		removed[name] = true
		if _, err := l.run(ctx, "rm", "-f", name); err != nil {
			if _, ierr := l.run(ctx, "inspect", name); ierr == nil {
				keep(err)
			}
		}
	}
	// After the delete: kind's delete recreates the kubeconfig's folder.
	if err := os.RemoveAll(l.dir); err != nil {
		keep(fmt.Errorf("lab: %w", err))
	}
	return first
}

// run runs the engine with args and returns its standard output. An error
// carries the command and everything it wrote.
func (l *Lab) run(ctx context.Context, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, l.engine, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("lab: %s %s: %w\n%s%s", l.engine, strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), nil
}

// withOutput adds to an error from kind the output of the command that
// failed, which kind's own message leaves out.
func withOutput(err error) error {
	if re := kindexec.RunErrorForError(err); re != nil && len(re.Output) > 0 {
		return fmt.Errorf("%w\n%s", err, re.Output)
	}
	return err
}
