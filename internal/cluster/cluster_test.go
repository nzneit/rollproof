//go:build linux && cluster

package cluster

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	kindcluster "sigs.k8s.io/kind/pkg/cluster"

	"rollproof/internal/verdict"
)

// TestMain counts the attempts of kind's Create that step 3 of Create makes: with -v, each
// attempt prints a line with the cluster's name and how it ended.
func TestMain(m *testing.M) {
	flag.Parse()
	create := kindCreate
	attempts := map[string]int{}
	kindCreate = func(p *kindcluster.Provider, name string, options ...kindcluster.CreateOption) error {
		attempts[name]++
		err := create(p, name, options...)
		if testing.Verbose() {
			result := "succeeded"
			if err != nil {
				result = "failed: " + err.Error()
			}
			fmt.Fprintf(os.Stderr, "kind Create of %s, attempt %d: %s\n", name, attempts[name], result)
		}
		return err
	}
	os.Exit(m.Run())
}

// testSpec is a cluster with the given workers, named rollproof-test-SUFFIX.
func testSpec(suffix string, workers int) Spec {
	n := "rollproof-test-" + suffix
	return Spec{Name: n, RunDir: RunDir(n), Workers: workers, Namespace: "app", Timeout: 5 * time.Minute}
}

// testEngine is this machine's engine, found as run finds it.
func testEngine(t *testing.T) Engine {
	t.Helper()
	e, err := CheckRunner(context.Background())
	if err != nil {
		t.Fatalf("CheckRunner: %v", err)
	}
	return e
}

// engine runs the engine's command line on the runner and returns its combined output, trimmed.
func engine(t *testing.T, e Engine, args ...string) (string, error) {
	t.Helper()
	out, err := exec.Command(e.Binary, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// fileHash is the SHA-256 of the file at path, or "absent".
func fileHash(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "absent"
	}
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

// pushTestImage pushes a one-layer image by digest to the registry at push, under repository,
// and returns the digest.
func pushTestImage(t *testing.T, push, repository string) string {
	t.Helper()
	layer, err := random.Layer(512, types.DockerLayer)
	if err != nil {
		t.Fatal(err)
	}
	img, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	cfg = cfg.DeepCopy()
	cfg.OS, cfg.Architecture = "linux", runtime.GOARCH
	if img, err = mutate.ConfigFile(img, cfg); err != nil {
		t.Fatal(err)
	}
	d, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}
	ref, err := name.NewDigest(push+"/"+repository+"@"+d.String(), name.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(ref, img); err != nil {
		t.Fatal(err)
	}
	return d.String()
}

func TestCreateAndDelete(t *testing.T) {
	ctx := context.Background()
	e := testEngine(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	callers := filepath.Join(home, ".kube", "config")
	before := fileHash(t, callers)

	s := testSpec("create", 3)
	c, err := Create(ctx, s, e)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer c.Delete(ctx)

	nodes := c.Nodes()
	if len(nodes) != 4 || nodes[0] != s.Name+"-control-plane" {
		t.Fatalf("Nodes() = %v, want 4 names with %s-control-plane first", nodes, s.Name)
	}
	client, err := c.Client()
	if err != nil {
		t.Fatal(err)
	}
	var zones []string
	for i, n := range nodes {
		node, err := client.CoreV1().Nodes().Get(ctx, n, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		ready := false
		for _, cond := range node.Status.Conditions {
			ready = ready || cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue
		}
		if !ready {
			t.Errorf("node %s is not ready", n)
		}
		zone, has := node.Labels["topology.kubernetes.io/zone"]
		if i == 0 && has {
			t.Errorf("the control-plane node has the zone %q", zone)
		}
		if i > 0 {
			zones = append(zones, zone)
		}
	}
	slices.Sort(zones)
	if want := []string{"rollproof-zone-1", "rollproof-zone-2", "rollproof-zone-3"}; !slices.Equal(zones, want) {
		t.Errorf("worker zones %v, want %v", zones, want)
	}
	dns, err := client.AppsV1().Deployments("kube-system").Get(ctx, "coredns", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if dns.Status.AvailableReplicas < *dns.Spec.Replicas {
		t.Errorf("coredns has %d of %d replicas available", dns.Status.AvailableReplicas, *dns.Spec.Replicas)
	}
	for _, ns := range []string{ProbeNamespace, "app"} {
		if _, err := client.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{}); err != nil {
			t.Errorf("namespace %s: %v", ns, err)
		}
	}

	kc, err := clientcmd.LoadFromFile(c.Kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	if len(kc.Contexts) != 1 || kc.CurrentContext != c.Context || kc.Contexts[c.Context].Namespace != "app" {
		t.Errorf("kubeconfig: current %q, %d contexts; want the one context %q with namespace app",
			kc.CurrentContext, len(kc.Contexts), c.Context)
	}
	if fi, err := os.Stat(c.Kubeconfig); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("the kubeconfig's mode: %v; want 0600", err)
	}
	if fi, err := os.Stat(c.RunDir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("the run folder's mode: %v; want 0700", err)
	}
	if c.NodeImage != DefaultNodeImage() || !strings.HasPrefix(c.KubernetesVersion, "v1.") {
		t.Errorf("NodeImage %q, KubernetesVersion %q", c.NodeImage, c.KubernetesVersion)
	}

	digest := pushTestImage(t, c.PushRegistry, "probe/x")
	if out, err := c.Exec(ctx, nodes[1], "crictl", "pull", TestHost+"/probe/x@"+digest); err != nil {
		t.Errorf("crictl pull in %s: %v\n%s", nodes[1], err, out)
	}
	out, err := c.Exec(ctx, nodes[1], "sh", "-c", "echo to-out; echo to-err >&2")
	if err != nil || !strings.Contains(string(out), "to-out") || !strings.Contains(string(out), "to-err") {
		t.Errorf("Exec = %q, %v; want both streams", out, err)
	}
	if _, err := c.Exec(ctx, nodes[1], "false"); err == nil {
		t.Error("Exec of a command that exits 1 gave no error")
	}

	if got := fileHash(t, callers); got != before {
		t.Errorf("the caller's kubeconfig changed: %s before, %s after", before, got)
	}

	if err := c.Delete(ctx); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	names, err := engine(t, e, "ps", "-a", "--format", "{{.Names}}")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range strings.Fields(names) {
		if strings.HasPrefix(n, s.Name+"-") {
			t.Errorf("container %s is left after Delete", n)
		}
	}
	if _, err := os.Stat(s.RunDir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the run's folder is left after Delete: %v", err)
	}
}

// Review Focus 1: a killed run left a folder and a registry container, and no cluster.
func TestCreateReplacesLeftovers(t *testing.T) {
	ctx := context.Background()
	e := testEngine(t)
	s := testSpec("leftovers", 1)
	if err := os.MkdirAll(s.RunDir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(s.RunDir, "left-by-a-killed-run")
	if err := os.WriteFile(old, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := engine(t, e, "run", "-d", "--name", s.Name+"-registry", DefaultRegistryImage); err != nil {
		t.Fatalf("start a leftover registry: %v\n%s", err, out)
	}

	c, err := Create(ctx, s, e)
	if err != nil {
		t.Fatalf("Create over leftovers: %v", err)
	}
	t.Cleanup(func() { c.Delete(ctx) })
	if _, err := os.Stat(old); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the old file is still there: %v", err)
	}

	created := func() map[string]time.Time {
		m := map[string]time.Time{}
		for _, n := range c.Nodes() {
			out, err := engine(t, e, "inspect", "--format", "{{.Created}}", n)
			if err != nil {
				t.Fatalf("inspect %s: %v\n%s", n, err, out)
			}
			at, err := time.Parse(time.RFC3339Nano, out)
			if err != nil {
				t.Fatalf("the creation time of %s: %v", n, err)
			}
			m[n] = at
		}
		return m
	}
	first := created()
	// The second cluster has the first one's name and node names, so c and the clean-up above
	// stand for it too.
	if _, err := Create(ctx, s, e); err != nil {
		t.Fatalf("Create over a running cluster: %v", err)
	}
	for n, at := range created() {
		if !at.After(first[n]) {
			t.Errorf("node %s was created at %v, not after %v", n, at, first[n])
		}
	}
}

func TestRefusesANameWithoutThePrefix(t *testing.T) {
	ctx := context.Background()
	e := testEngine(t)
	if out, err := engine(t, e, "run", "-d", "--name", "other-registry", DefaultRegistryImage); err != nil {
		t.Fatalf("start other-registry: %v\n%s", err, out)
	}
	t.Cleanup(func() { engine(t, e, "rm", "-f", "-v", "other-registry") })

	_, err := Create(ctx, Spec{Name: "other", RunDir: RunDir("other"), Workers: 1, Namespace: "app", Timeout: time.Minute}, e)
	if err == nil {
		t.Fatal("Create(other) gave no error, want internal-error")
	}
	if c := verdict.AsCondition(err); c.Code != verdict.InternalError {
		t.Fatalf("Create(other) = %v, want internal-error", err)
	}
	if out, err := engine(t, e, "inspect", "--format", "{{.State.Running}}", "other-registry"); err != nil || out != "true" {
		t.Errorf("other-registry: running %q, %v; want true", out, err)
	}
}

func TestDeleteTwice(t *testing.T) {
	ctx := context.Background()
	c, err := Create(ctx, testSpec("delete", 1), testEngine(t))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := c.Delete(ctx); err != nil {
		t.Fatalf("first Delete: %v", err)
	}
	if err := c.Delete(ctx); err != nil {
		t.Errorf("second Delete: %v", err)
	}
}

// Keep: a step that fails after kind has made the cluster (a registry image that exists nowhere)
// leaves nothing without Keep, and leaves the nodes and the run's folder with it.
func TestKeepAfterFailure(t *testing.T) {
	ctx := context.Background()
	e := testEngine(t)
	const missing = "rollproof.test/no-such-registry@sha256:0000000000000000000000000000000000000000000000000000000000000000"
	containers := func(name string) []string {
		out, err := engine(t, e, "ps", "-a", "--format", "{{.Names}}")
		if err != nil {
			t.Fatal(err)
		}
		var left []string
		for _, n := range strings.Fields(out) {
			if strings.HasPrefix(n, name+"-") {
				left = append(left, n)
			}
		}
		slices.Sort(left)
		return left
	}

	s := testSpec("nokeep", 1)
	s.RegistryImage = missing
	_, err := Create(ctx, s, e)
	if err == nil {
		t.Fatal("Create gave no error, want cluster-failed in start the registry")
	}
	if c := verdict.AsCondition(err); c.Code != verdict.ClusterFailed || !strings.HasPrefix(c.Text, "start the registry") {
		t.Fatalf("Create = %v, want cluster-failed in start the registry", err)
	}
	if left := containers(s.Name); len(left) != 0 {
		t.Errorf("containers left without Keep: %v", left)
	}
	if _, err := os.Stat(s.RunDir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the run's folder is left without Keep: %v", err)
	}

	kept := testSpec("keep", 1)
	kept.RegistryImage = missing
	kept.Keep = true
	t.Cleanup(func() { remove(ctx, e, kept.Name, kept.RunDir) })
	_, err = Create(ctx, kept, e)
	if err == nil {
		t.Fatal("Create with Keep gave no error, want cluster-failed in start the registry")
	}
	if c := verdict.AsCondition(err); c.Code != verdict.ClusterFailed || !strings.HasPrefix(c.Text, "start the registry") {
		t.Fatalf("Create with Keep = %v, want cluster-failed in start the registry", err)
	}
	if got, want := containers(kept.Name), []string{kept.Name + "-control-plane", kept.Name + "-worker"}; !slices.Equal(got, want) {
		t.Errorf("containers with Keep: %v, want %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(kept.RunDir, "kubeconfig")); err != nil {
		t.Errorf("the run's folder and its kubeconfig are not kept: %v", err)
	}
}
