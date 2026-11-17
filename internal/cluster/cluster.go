//go:build linux

package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/kind/pkg/apis/config/defaults"
	kindcluster "sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cluster/constants"
	"sigs.k8s.io/kind/pkg/cluster/nodes"
	"sigs.k8s.io/kind/pkg/cluster/nodeutils"

	"rollproof/internal/verdict"
)

// namePrefix begins the name of every cluster, registry and run's folder that the tool makes, and
// so every name that it removes.
const namePrefix = "rollproof-"

// inotifyHint ends the failure texts of the steps that create the cluster and wait until it is
// ready.
const inotifyHint = "; a cluster of several nodes also fails when the kernel's inotify limits are low (fs.inotify.max_user_watches, fs.inotify.max_user_instances)"

// Through rootless nerdctl, kind's Create sometimes fails at its 30-second wait for a node's log
// line, although the log holds the line (proof p08). Step 3 of Create tries kind's Create again
// for that failure alone, createAttempts times in all.
const (
	createAttempts = 4
	logLineMissed  = "could not find a log line that matches"
)

// leftBehind ends the failure text of Create when the clean-up after a failed step failed too.
const leftBehind = ". Parts of the run may be left behind; the next run with this name removes them."

// kindCreate and kindDelete are the kind provider's Create and Delete, as step 3 of Create and
// remove call them. A test replaces them.
var (
	kindCreate = (*kindcluster.Provider).Create
	kindDelete = (*kindcluster.Provider).Delete
)

// joined is the error of several errors, like errors.Join, with "; " between their texts instead
// of a newline, so that a reason made from it stays on one line.
type joined []error

func (j joined) Error() string {
	texts := make([]string, len(j))
	for i, err := range j {
		texts[i] = err.Error()
	}
	return strings.Join(texts, "; ")
}

func (j joined) Unwrap() []error { return j }

// join returns the errors of errs that are not nil as one error, or nil when there is none.
func join(errs ...error) error {
	var j joined
	for _, err := range errs {
		if err != nil {
			j = append(j, err)
		}
	}
	if len(j) == 0 {
		return nil
	}
	return j
}

// DefaultNodeImage returns the node image of the linked kind release, with its digest.
func DefaultNodeImage() string {
	return defaults.Image
}

// KindVersion returns the version of the kind release the tool links.
func KindVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, d := range info.Deps {
			if d.Path == "sigs.k8s.io/kind" {
				return d.Version
			}
		}
	}
	return "unknown"
}

// Cluster is a run's cluster, ready. Kubeconfig is its own file and Context that file's one
// context. PushRegistry is the run's registry as the runner reaches it; NodeRegistry is the same
// registry as the nodes reach it.
type Cluster struct {
	Name, RunDir, Kubeconfig, Context string
	Engine                            Engine
	PushRegistry, NodeRegistry        string
	KubernetesVersion, NodeImage      string
	Workers                           int

	provider *kindcluster.Provider
	nodes    []nodes.Node // in the order of Nodes
}

// Create removes whatever an earlier run of this name left, then creates the cluster and its
// registry and returns when every node is ready, DNS runs and both namespaces exist. On an error
// nothing of the run is left behind, unless s.Keep is set or ctx has ended. An error is a
// Condition with ClusterFailed. A name without the prefix rollproof- is an InternalError.
func Create(ctx context.Context, s Spec, e Engine) (*Cluster, error) {
	deadline := time.Now().Add(s.Timeout)

	if err := remove(ctx, e, s.Name, s.RunDir); err != nil {
		var c verdict.Condition
		if errors.As(err, &c) && c.Code == verdict.InternalError {
			return nil, err
		}
		return nil, verdict.Condition{Code: verdict.ClusterFailed, Text: "remove leftovers: " + err.Error()}
	}

	// fail is the failure of a step after the first: it removes what the run made, unless s.Keep
	// is set or ctx has ended. When that removal fails, the text says that parts may be left, and
	// the removal's error is joined to the Condition for the log.
	fail := func(step string, err error) (*Cluster, error) {
		failure := verdict.Condition{Code: verdict.ClusterFailed, Text: step + ": " + err.Error()}
		if s.Keep || ctx.Err() != nil {
			return nil, failure
		}
		if rerr := remove(ctx, e, s.Name, s.RunDir); rerr != nil {
			failure.Text += leftBehind
			return nil, errors.Join(failure, fmt.Errorf("the clean-up after it failed: %w", rerr))
		}
		return nil, failure
	}

	c := &Cluster{
		Name:       s.Name,
		RunDir:     s.RunDir,
		Kubeconfig: filepath.Join(s.RunDir, "kubeconfig"),
		Engine:     e,
		Workers:    s.Workers,
		provider:   e.provider(),
	}

	if err := os.Mkdir(s.RunDir, 0o700); err != nil {
		return fail("make the run's folder", err)
	}

	err := createKind(ctx, c.provider, s.Name, c.Kubeconfig,
		kindcluster.CreateWithV1Alpha4Config(KindConfig(s, e.Rootless)),
		kindcluster.CreateWithKubeconfigPath(c.Kubeconfig),
		kindcluster.CreateWithWaitForReady(0),
		kindcluster.CreateWithRetain(s.Keep),
		kindcluster.CreateWithDisplayUsage(false),
		kindcluster.CreateWithDisplaySalutation(false),
	)
	if err != nil {
		return fail("create the kind cluster", fmt.Errorf("%w%s", err, inotifyHint))
	}

	image := s.RegistryImage
	if image == "" {
		image = DefaultRegistryImage
	}
	if c.PushRegistry, c.NodeRegistry, err = startRegistry(ctx, e, s.Name, image, deadline); err != nil {
		return fail("start the registry", err)
	}

	listed, err := c.provider.ListNodes(s.Name)
	if err != nil {
		return fail("write the node settings", err)
	}
	for _, n := range listed {
		if err := nodeutils.WriteFile(n, "/etc/containerd/certs.d/"+TestHost+"/hosts.toml", HostsTOML(c.NodeRegistry)); err != nil {
			return fail("write the node settings", fmt.Errorf("node %s: %w", n, err))
		}
	}
	if c.nodes, err = byRole(listed); err != nil {
		return fail("write the node settings", err)
	}

	client, err := c.Client()
	if err == nil {
		err = waitReady(ctx, client, 1+s.Workers, deadline)
	}
	if err != nil {
		return fail("wait until ready", fmt.Errorf("%w%s", err, inotifyHint))
	}

	if err := ensureNamespaces(ctx, client, ProbeNamespace, s.Namespace); err != nil {
		return fail("create the namespaces", err)
	}

	if c.Context, err = setNamespace(c.Kubeconfig, s.Namespace); err != nil {
		return fail("set the kubeconfig's namespace", err)
	}

	version, err := discovery.ToServerVersionInterfaceWithContext(client.Discovery()).ServerVersionWithContext(ctx)
	if err != nil {
		return fail("read the versions", err)
	}
	c.KubernetesVersion = version.GitVersion
	if c.NodeImage, err = nodeImage(ctx, e, s.NodeImage); err != nil {
		return fail("read the versions", err)
	}
	return c, nil
}

// createKind is step 3 of Create: kind's Create of name with options. While kind's Create fails
// at its wait for a node's log line and ctx has not ended, it deletes the failed attempt with
// kubeconfig, whatever the options say about keeping nodes, and tries again, createAttempts
// times in all. The error is that of the last attempt, its text unchanged; a delete that fails
// ends the attempts, and its error is joined, after "; ", to that of the attempt before it.
func createKind(ctx context.Context, p *kindcluster.Provider, name, kubeconfig string, options ...kindcluster.CreateOption) error {
	for attempt := 1; ; attempt++ {
		err := kindCreate(p, name, options...)
		if err == nil || attempt == createAttempts || ctx.Err() != nil || !strings.Contains(err.Error(), logLineMissed) {
			return err
		}
		if derr := kindDelete(p, name, kubeconfig); derr != nil {
			return join(err, fmt.Errorf("delete the failed attempt %d: %w", attempt, derr))
		}
	}
}

// byRole returns the nodes with the control-plane node first and the others sorted by name.
func byRole(listed []nodes.Node) ([]nodes.Node, error) {
	controlPlane := map[string]bool{}
	for _, n := range listed {
		role, err := n.Role()
		if err != nil {
			return nil, fmt.Errorf("the role of node %s: %w", n, err)
		}
		controlPlane[n.String()] = role == constants.ControlPlaneNodeRoleValue
	}
	sorted := slices.Clone(listed)
	slices.SortFunc(sorted, func(a, b nodes.Node) int {
		if ca, cb := controlPlane[a.String()], controlPlane[b.String()]; ca != cb {
			if ca {
				return -1
			}
			return 1
		}
		return strings.Compare(a.String(), b.String())
	})
	return sorted, nil
}

// waitReady polls once a second, until deadline, until the cluster has want nodes, each Ready,
// and kube-system/coredns has all its replicas available. The error at the deadline gives both
// counts.
func waitReady(ctx context.Context, client kubernetes.Interface, want int, deadline time.Time) error {
	waitCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var ready, total, available, replicas int
	for {
		err := func() error {
			list, err := client.CoreV1().Nodes().List(waitCtx, metav1.ListOptions{})
			if err != nil {
				return err
			}
			ready, total = 0, len(list.Items)
			for _, n := range list.Items {
				for _, cond := range n.Status.Conditions {
					if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
						ready++
					}
				}
			}
			dns, err := client.AppsV1().Deployments("kube-system").Get(waitCtx, "coredns", metav1.GetOptions{})
			if err != nil {
				return err
			}
			available, replicas = int(dns.Status.AvailableReplicas), 1 // 1 is the API server's default
			if dns.Spec.Replicas != nil {
				replicas = int(*dns.Spec.Replicas)
			}
			return nil
		}()
		if err == nil && total == want && ready == want && available >= replicas {
			return nil
		}
		select {
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			text := fmt.Sprintf("at the deadline %d of %d nodes were Ready and coredns had %d of %d replicas available", ready, want, available, replicas)
			if err != nil {
				text += "; the last poll failed: " + err.Error()
			}
			return errors.New(text)
		case <-tick.C:
		}
	}
}

// ensureNamespaces makes sure that each namespace of names exists. A namespace that exists already,
// such as default, counts as made.
func ensureNamespaces(ctx context.Context, client kubernetes.Interface, names ...string) error {
	for _, name := range names {
		_, err := client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}, metav1.CreateOptions{})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	}
	return nil
}

// setNamespace sets the namespace of the current context of the kubeconfig file at path, keeps
// the file's mode at 0600, and returns that context's name.
func setNamespace(path, namespace string) (string, error) {
	cfg, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return "", err
	}
	current, ok := cfg.Contexts[cfg.CurrentContext]
	if !ok {
		return "", errors.New("the kubeconfig has no current context")
	}
	current.Namespace = namespace
	if err := clientcmd.WriteToFile(*cfg, path); err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	return cfg.CurrentContext, nil
}

// nodeImage returns the node image with its digest: named, or DefaultNodeImage when named is
// empty. A reference without a digest gets the one of the first entry of RepoDigests in the
// engine's store.
func nodeImage(ctx context.Context, e Engine, named string) (string, error) {
	if named == "" {
		named = DefaultNodeImage()
	}
	if strings.Contains(named, "@sha256:") {
		return named, nil
	}
	out, err := e.run(ctx, "image", "inspect", "--format", "{{json .RepoDigests}}", named)
	if err != nil {
		return "", err
	}
	var digests []string
	if err := json.Unmarshal([]byte(out), &digests); err != nil {
		return "", fmt.Errorf("the digests of the node image: %w", err)
	}
	if len(digests) == 0 {
		return "", errors.New("the engine lists no digest for the node image")
	}
	_, digest, ok := strings.Cut(digests[0], "@")
	if !ok {
		return "", errors.New("the engine lists the node image's digest in a form without @")
	}
	return named + "@" + digest, nil
}

// Client returns a client for the cluster's API server.
func (c *Cluster) Client() (kubernetes.Interface, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", c.Kubeconfig)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}

// Nodes returns the node names, the control-plane node first.
func (c *Cluster) Nodes() []string {
	names := make([]string, len(c.nodes))
	for i, n := range c.nodes {
		names[i] = n.String()
	}
	return names
}

// Exec runs a command in a node and returns its output, standard error included. A non-zero exit
// is an error that carries the output.
func (c *Cluster) Exec(ctx context.Context, node string, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("no command given to Exec")
	}
	i := slices.IndexFunc(c.nodes, func(n nodes.Node) bool { return n.String() == node })
	if i < 0 {
		return nil, fmt.Errorf("the cluster %s has no node %s", c.Name, node)
	}
	var out bytes.Buffer
	if err := c.nodes[i].CommandContext(ctx, args[0], args[1:]...).SetStdout(&out).SetStderr(&out).Run(); err != nil {
		return out.Bytes(), fmt.Errorf("%s in node %s: %w\n%s", strings.Join(args, " "), node, err, out.Bytes())
	}
	return out.Bytes(), nil
}

// Delete deletes the cluster, the registry with its volumes, and the run's folder. Deleting what
// is already gone is not an error.
func (c *Cluster) Delete(ctx context.Context) error {
	return remove(ctx, c.Engine, c.Name, c.RunDir)
}

// remove deletes the kind cluster name, the registry container NAME-registry with its volumes,
// and runDir, going on after an error and joining the errors with "; ". It refuses, with
// InternalError, a name without namePrefix and a runDir whose last element is not the name.
func remove(ctx context.Context, e Engine, name, runDir string) error {
	if !strings.HasPrefix(name, namePrefix) {
		return verdict.Condition{Code: verdict.InternalError, Text: "refused to remove a cluster whose name does not start with " + namePrefix}
	}
	if filepath.Base(runDir) != name {
		return verdict.Condition{Code: verdict.InternalError, Text: "refused to remove a run's folder that is not named after its cluster"}
	}
	kubeconfig := filepath.Join(runDir, "kubeconfig")
	var errs []error
	// A killed run may have left the lock that kind's delete takes with an exclusive create.
	if err := os.Remove(kubeconfig + ".lock"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, err)
	}
	if err := kindDelete(e.provider(), name, kubeconfig); err != nil {
		errs = append(errs, fmt.Errorf("delete the kind cluster %s: %w", name, err))
	}
	if err := e.removeContainer(ctx, name+"-registry"); err != nil {
		errs = append(errs, err)
	}
	// Last, because kind's delete makes the kubeconfig's folder again when it is missing.
	if err := os.RemoveAll(runDir); err != nil {
		errs = append(errs, err)
	}
	return join(errs...)
}
