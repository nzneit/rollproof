//go:build linux

package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"time"
	"unicode"

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

	"rollproof/internal/config"
	"rollproof/internal/verdict"
)

// namePrefix begins the name of every cluster, registry and run's folder that the tool makes, and
// so every name that it removes.
const namePrefix = "rollproof-"

// inotifyHint ends the failure texts of the steps that create the cluster and wait until it is
// ready.
const inotifyHint = "; a cluster of several nodes also fails when the kernel's inotify limits are low (fs.inotify.max_user_watches, fs.inotify.max_user_instances)"

// unansweredAttempt is the kernel setting, as sysctl -w takes it, that Create writes in each node:
// how long, in seconds, connection tracking keeps an attempt to connect that has had no answer
// (section 9).
const unansweredAttempt = "net.netfilter.nf_conntrack_tcp_timeout_syn_sent=5"

// Through rootless nerdctl, kind's Create sometimes fails at its 30-second wait for a node's log
// line, although the log holds the line (proof p08): when `nerdctl logs -f` reads a line that the
// logger has not finished writing, it loses its place and drops the lines after it (nerdctl
// v2.4.2, pkg/logging/jsonfile Decode returns a nil error for a partial line). Step 3 of Create
// tries kind's Create again for that failure alone, createAttempts times in all.
const (
	createAttempts = 4
	logLineMissed  = "could not find a log line that matches"
)

// LeftBehind ends the failure text of Create when the clean-up after a failed step failed too. It
// is exported for the command's own failure text, which must not drift from this one.
const LeftBehind = ". Parts of the run may be left behind; the next run with this name removes them."

// LogsNotCollected is added to a failure's text before the error of a collection that could not
// finish. It is exported for the command's own failure text, which must not drift from this one.
const LogsNotCollected = "; the cluster's logs were not all collected: "

// kindCreate and kindDelete are the kind provider's Create and Delete, as step 3 of Create and
// remove call them. A test replaces them.
var (
	kindCreate = (*kindcluster.Provider).Create
	kindDelete = (*kindcluster.Provider).Delete
)

// The calls into kind that Create's node settings make. A test replaces them.
var (
	kindListNodes = (*kindcluster.Provider).ListNodes
	nodeRole      = nodes.Node.Role
	writeNodeFile = nodeutils.WriteFile
)

// pullImage is Create's pull of the node image, with the engine's check for an image that its
// store holds. A test replaces it.
var pullImage = Engine.pullIfMissing

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

// capitalised returns s with its first letter upper-cased, for a sentence that follows a full stop.
func capitalised(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
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
	evidence func(name string) (io.WriteCloser, error)
}

// Create removes leftovers, then has the engine pull the node image if its
// store lacks it, makes the cluster and its registry and returns when every
// node is ready, DNS runs and both namespaces exist. Each node keeps an
// attempt to connect that had no answer for 5 s (section 9). A failure after
// kind's creation began takes kind's logs and the registry's log when its
// container ran, into s.Evidence, unless ctx has ended. On an error it removes
// the run, unless s.Keep is set or ctx has ended; when that removal fails too,
// parts stay behind and the text says so.
// A step's error is a ClusterFailed Condition, and a name without rollproof-
// an InternalError.
// When an entry has credentials, the credentials file is written, mode 0600,
// into the run's folder before kind creates the nodes; each node gets every ca
// and settings file with rollproof.test's. A file that cannot be read or
// written fails the step it belongs to.
// Each call into kind runs through untilDone: when ctx ends while kind works,
// Create stops waiting for kind and returns, and the next run removes what kind
// made (section 10, "On a signal").
func Create(ctx context.Context, s Spec, e Engine) (*Cluster, error) {
	if err := remove(ctx, e, s.Name, s.RunDir); err != nil {
		var c verdict.Condition
		if errors.As(err, &c) && c.Code == verdict.InternalError {
			return nil, err
		}
		return nil, verdict.Condition{Code: verdict.ClusterFailed, Text: "remove leftovers: " + err.Error()}
	}

	// c is the cluster as much as the run knows it so far, so that a failure can name what a kept
	// cluster holds.
	c := &Cluster{
		Name:       s.Name,
		RunDir:     s.RunDir,
		Kubeconfig: filepath.Join(s.RunDir, "kubeconfig"),
		Engine:     e,
		Workers:    s.Workers,
		provider:   e.provider(),
		evidence:   s.Evidence,
	}
	// kindBegun says that kind's own creation of the cluster has begun: from then on a kept
	// cluster has a kubeconfig file and a context to name, and a failure has logs to take.
	kindBegun := false

	// fail is the failure of a step: it takes the cluster's logs into the evidence and removes
	// what the run made, unless s.Keep is set or ctx has ended. A kept cluster whose kind creation
	// began ends the text with the sentence Kept gives, so that the console names its kubeconfig
	// and context (section 9). When the removal fails, the text says that parts may be left, and
	// the removal's error is joined to the Condition for the log. A signal during the collection
	// ends the clean-up there: the cluster stays for the next run to remove.
	fail := func(step string, err error) (*Cluster, error) {
		failure := verdict.Condition{Code: verdict.ClusterFailed, Text: step + ": " + err.Error()}
		if ctx.Err() != nil {
			return nil, failure
		}
		if kindBegun {
			if cerr := collectLogs(ctx, e, c.provider, s.Name, s.RunDir, s.Evidence); cerr != nil {
				failure.Text += LogsNotCollected + cerr.Error()
			}
		}
		if ctx.Err() != nil {
			return nil, failure
		}
		if s.Keep {
			if kindBegun {
				failure.Text += ". " + capitalised(c.Kept())
			}
			return nil, failure
		}
		if rerr := remove(ctx, e, s.Name, s.RunDir); rerr != nil {
			failure.Text += LeftBehind
			return nil, errors.Join(failure, fmt.Errorf("the clean-up after it failed: %w", rerr))
		}
		return nil, failure
	}

	if err := os.Mkdir(s.RunDir, 0o700); err != nil {
		return fail("make the run's folder", err)
	}
	// Kind mounts the file into every node when it creates them (decision 9).
	if creds := Credentials(s.Registries); creds != nil {
		if err := os.WriteFile(filepath.Join(s.RunDir, credentialsName), creds, 0o600); err != nil {
			return fail("write the nodes' credentials file", err)
		}
	}

	// kind is given the reference as written, and the engine is asked for it by nodeImageName, so
	// that kind's own check finds the image there and does not pull it again, outside the limit.
	ref := s.NodeImage
	if ref == "" {
		ref = DefaultNodeImage()
	}
	if err := pullImage(e, ctx, e.nodeImageName(ref), s.Timeout); err != nil {
		return fail("pull the node image", err)
	}

	kindBegun = true
	err := createKind(ctx, c.provider, s.Name, c.Kubeconfig,
		kindcluster.CreateWithV1Alpha4Config(KindConfig(s, e.Rootless)),
		kindcluster.CreateWithKubeconfigPath(c.Kubeconfig),
		kindcluster.CreateWithWaitForReady(0),
		// Always kept by kind, whatever s.Keep says: a failed set-up takes the nodes' logs
		// before the tool removes them itself.
		kindcluster.CreateWithRetain(true),
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
	if c.PushRegistry, c.NodeRegistry, err = startRegistry(ctx, e, s.Name, image, s.Timeout); err != nil {
		return fail("start the registry", err)
	}

	if c.nodes, err = writeNodeSettings(ctx, c.provider, s.Name, s.Registries, s.AsHosts, c.NodeRegistry); err != nil {
		return fail("write the node settings", err)
	}

	client, err := c.Client()
	if err == nil {
		err = waitReady(ctx, client, 1+s.Workers, s.Timeout)
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

// writeNodeSettings writes the node settings into every node of the cluster name:
// each entry's ca and settings file, rollproof.test's settings file, and the
// kernel's setting for an attempt to connect that had no answer; and returns the
// nodes, the control-plane node first. A ca that cannot be read fails it before
// any node is listed, with the entry's index. Its calls into kind take no
// context, so they run through untilDone: when ctx ends, it returns ctx's error at
// once.
func writeNodeSettings(ctx context.Context, p *kindcluster.Provider, name string, regs []config.Registry, asHosts []string, runRegistry string) ([]nodes.Node, error) {
	// cas holds each entry's ca, at the entry's index, read once for every node.
	cas := make([][]byte, len(regs))
	for i, r := range regs {
		if r.CA == "" {
			continue
		}
		var err error
		if cas[i], err = os.ReadFile(r.CA); err != nil {
			return nil, fmt.Errorf("registries[%d].ca: %w", i, err)
		}
	}
	// sorted is written by untilDone's goroutine and read only when untilDone returned nil, after
	// that goroutine handed its result back.
	var sorted []nodes.Node
	err := untilDone(ctx, func() error {
		listed, err := kindListNodes(p, name)
		if err != nil {
			return err
		}
		for _, n := range listed {
			if err := writeRegistryFiles(n, regs, cas, asHosts, runRegistry); err != nil {
				return fmt.Errorf("node %s: %w", n, err)
			}
			if err := writeNodeFile(n, settingsPath(TestHost), HostsTOML(runRegistry)); err != nil {
				return fmt.Errorf("node %s: %w", n, err)
			}
			var out bytes.Buffer
			if err := n.CommandContext(ctx, "sysctl", "-w", unansweredAttempt).SetStdout(&out).SetStderr(&out).Run(); err != nil {
				err = fmt.Errorf("node %s: sysctl -w %s: %w", n, unansweredAttempt, err)
				if said := oneLine(out.Bytes()); said != "" {
					err = fmt.Errorf("%w: %s", err, said)
				}
				return err
			}
		}
		sorted, err = byRole(listed)
		return err
	})
	if err != nil {
		return nil, err
	}
	return sorted, nil
}

// writeRegistryFiles writes into node n, by decision 10, each entry's ca, then the settings file
// of each entry's host and of each host of asHosts that no entry names. cas holds each entry's ca
// as read, at the entry's index. The settings of a host of asHosts try the run's registry, at
// runRegistry, first; a host whose settings are empty gets no file.
func writeRegistryFiles(n nodes.Node, regs []config.Registry, cas [][]byte, asHosts []string, runRegistry string) error {
	for i, r := range regs {
		if r.CA != "" {
			if err := writeNodeFile(n, caPath(r.Host), string(cas[i])); err != nil {
				return err
			}
		}
	}
	entries := slices.Clone(regs)
	for _, h := range asHosts {
		if !slices.ContainsFunc(regs, func(r config.Registry) bool { return r.Host == h }) {
			entries = append(entries, config.Registry{Host: h})
		}
	}
	for _, r := range entries {
		run := ""
		if slices.Contains(asHosts, r.Host) {
			run = runRegistry
		}
		if text := HostSettings(r, run); text != "" {
			if err := writeNodeFile(n, settingsPath(r.Host), text); err != nil {
				return err
			}
		}
	}
	return nil
}

// settingsPath returns the path, in every node, of the settings file of host, as written.
func settingsPath(host string) string {
	return "/etc/containerd/certs.d/" + host + "/hosts.toml"
}

// createKind is step 3 of Create: kind's Create of name with options. While kind's Create fails
// at its wait for a node's log line and ctx has not ended, it deletes the failed attempt with
// kubeconfig, whatever the options say about keeping nodes, and tries again, createAttempts
// times in all. The error is that of the last attempt, its text unchanged; a delete that fails
// ends the attempts, and its error is joined, after "; ", to that of the attempt before it.
// kind's Create and Delete run through untilDone: when ctx ends first during one, its error is ctx's.
func createKind(ctx context.Context, p *kindcluster.Provider, name, kubeconfig string, options ...kindcluster.CreateOption) error {
	for attempt := 1; ; attempt++ {
		err := untilDone(ctx, func() error { return kindCreate(p, name, options...) })
		if err == nil || attempt == createAttempts || ctx.Err() != nil || !strings.Contains(err.Error(), logLineMissed) {
			return err
		}
		if derr := untilDone(ctx, func() error { return kindDelete(p, name, kubeconfig) }); derr != nil {
			return join(err, fmt.Errorf("delete the failed attempt %d: %w", attempt, derr))
		}
	}
}

// byRole returns the nodes with the control-plane node first and the others sorted by name.
func byRole(listed []nodes.Node) ([]nodes.Node, error) {
	controlPlane := map[string]bool{}
	for _, n := range listed {
		role, err := nodeRole(n)
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

// waitReady polls once a second, for at most limit from its own call, until the cluster has want
// nodes, each Ready, and kube-system/coredns has all its replicas available. The error at the
// deadline gives both counts.
func waitReady(ctx context.Context, client kubernetes.Interface, want int, limit time.Duration) error {
	waitCtx, cancel := context.WithDeadline(ctx, time.Now().Add(limit))
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
// engine's store. The engine is asked by nodeImageName; the name returned is named as written, with
// the digest after it, so that the report and rollproof version keep it.
func nodeImage(ctx context.Context, e Engine, named string) (string, error) {
	if named == "" {
		named = DefaultNodeImage()
	}
	if strings.Contains(named, "@sha256:") {
		return named, nil
	}
	out, err := e.run(ctx, "image", "inspect", "--format", "{{json .RepoDigests}}", e.nodeImageName(named))
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

// Kept is the sentence for a kept cluster: its kubeconfig file and the name of that file's
// context. The context is the one Create set, or, before Create set it, the one kind writes for a
// cluster of this name. A cluster whose kubeconfig file kind never wrote says that, and names no
// path.
func (c *Cluster) Kept() string {
	if _, err := os.Stat(c.Kubeconfig); err != nil {
		return "the cluster is kept, and kind wrote no kubeconfig file"
	}
	context := c.Context
	if context == "" {
		context = "kind-" + c.Name
	}
	return "the cluster is kept: kubeconfig " + c.Kubeconfig + ", context " + context
}

// Delete deletes the cluster, the registry with its volumes, and the run's folder. Deleting what
// is already gone is not an error.
func (c *Cluster) Delete(ctx context.Context) error {
	return remove(ctx, c.Engine, c.Name, c.RunDir)
}

// remove deletes the kind cluster name, the registry container NAME-registry with its volumes,
// and runDir, going on after an error and joining the errors with "; ". It refuses, with
// InternalError, a name without namePrefix and a runDir whose last element is not the name.
// kind's delete runs through untilDone: when ctx ends during it, its error is ctx's, and kind's
// delete goes on alone.
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
	if err := untilDone(ctx, func() error { return kindDelete(e.provider(), name, kubeconfig) }); err != nil {
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

// oneLine is what a command said, on one line, so that a reason that quotes it stays on one line:
// its fields separated by single spaces, and "" when it said nothing.
func oneLine(b []byte) string {
	return strings.Join(strings.Fields(string(b)), " ")
}
