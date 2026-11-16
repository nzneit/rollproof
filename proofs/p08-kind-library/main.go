//go:build linux

// Proof of point 8: whether kind v0.33.0, linked as a library, creates through
// the engine on this machine a cluster with the node settings of section 9: one
// control-plane node and three workers, a zone label on each worker, a file
// of the runner mounted into every node, the containerd patch that points the
// registry settings at /etc/containerd/certs.d, and a kubeconfig written only
// to the path the lab gives. It also reads the versions the plan assumes.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/kind/pkg/apis/config/defaults"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/kind/pkg/cmd/kind/version"

	"rollproof-proofs/internal/lab"
)

const (
	clusterName  = "rollproof-proof-p08"
	controlPlane = clusterName + "-control-plane"
	zoneLabel    = "topology.kubernetes.io/zone"
	authsJSON    = `{"auths":{}}`
	certsDPatch  = "[plugins.\"io.containerd.grpc.v1.cri\".registry]\n  config_path = \"/etc/containerd/certs.d\""
)

// configPathLine matches a line of `containerd config dump` that sets
// config_path to /etc/containerd/certs.d, in either TOML quote style, with
// nothing after the closing quote.
var configPathLine = regexp.MustCompile(`^\s*config_path = ("/etc/containerd/certs\.d"|'/etc/containerd/certs\.d')\s*$`)

func main() { os.Exit(run()) }

// checks counts the failed checks and prints one line for each check.
type checks struct{ failed int }

func (c *checks) ok(format string, args ...any) { fmt.Printf("ok: "+format+"\n", args...) }

func (c *checks) fail(format string, args ...any) {
	c.failed++
	fmt.Printf("FAILED: "+format+"\n", args...)
}

func info(format string, args ...any) { fmt.Printf("info: "+format+"\n", args...) }

// file is what action 1 notes about one kubeconfig file of the caller.
type file struct {
	path  string
	state string // "does not exist", "has SHA-256 HEX" or "cannot be read: ERROR"
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c := &checks{}

	// Action 1: the caller's kubeconfig files, the provider variable, docker.
	paths := []string{filepath.Join(os.Getenv("HOME"), ".kube", "config")}
	for _, p := range filepath.SplitList(os.Getenv("KUBECONFIG")) {
		if p != "" {
			paths = append(paths, p)
		}
	}
	before := snapshot(paths)
	for _, f := range before {
		info("%s", describe(f))
	}
	provider, providerSet := os.LookupEnv("KIND_EXPERIMENTAL_PROVIDER")
	if providerSet {
		info("KIND_EXPERIMENTAL_PROVIDER=%s", provider)
	} else {
		info("KIND_EXPERIMENTAL_PROVIDER is unset")
	}
	dockerErr := exec.CommandContext(ctx, "docker", "-v").Run()
	if dockerErr == nil {
		info("docker -v exits 0")
	} else {
		info("docker -v does not exit 0: %v", dockerErr)
	}

	// Action 2: the credentials file that every node mounts.
	dir, err := os.MkdirTemp("", "rollproof-proof-p08-auth-")
	if err != nil {
		c.fail("a temporary folder for config.json: %v", err)
		return summary(c)
	}
	defer os.RemoveAll(dir)
	configJSON := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configJSON, []byte(authsJSON), 0o600); err != nil {
		c.fail("writing config.json: %v", err)
		return summary(c)
	}

	// Action 3: the description.
	mounts := func() []v1alpha4.Mount {
		return []v1alpha4.Mount{{HostPath: configJSON, ContainerPath: "/var/lib/kubelet/config.json", Readonly: true}}
	}
	cfg := &v1alpha4.Cluster{
		Nodes:                   []v1alpha4.Node{{Role: v1alpha4.ControlPlaneRole, ExtraMounts: mounts()}},
		ContainerdConfigPatches: []string{certsDPatch},
	}
	for n := 1; n <= 3; n++ {
		cfg.Nodes = append(cfg.Nodes, v1alpha4.Node{
			Role:        v1alpha4.WorkerRole,
			Labels:      map[string]string{zoneLabel: fmt.Sprintf("rollproof-zone-%d", n)},
			ExtraMounts: mounts(),
		})
	}

	// Action 4: the cluster.
	l, err := lab.Up(ctx, "p08", cfg)
	if err != nil {
		c.fail("lab.Up: %v", err)
		return summary(c)
	}
	defer func() {
		if err := l.Down(); err != nil {
			fmt.Fprintf(os.Stderr, "p08: %v\n", err)
		}
	}()
	engine := l.Engine()

	// Action 5: checks after Up.
	if !providerSet && dockerErr != nil {
		if engine == "nerdctl" {
			c.ok("KIND_EXPERIMENTAL_PROVIDER is unset and docker -v failed, and the lab picked nerdctl")
		} else {
			c.fail("KIND_EXPERIMENTAL_PROVIDER is unset and docker -v failed, but the lab picked %s, not nerdctl", engine)
		}
	} else {
		info("the lab picked %s", engine)
	}
	inspected, err := exec.CommandContext(ctx, engine, "inspect", controlPlane).CombinedOutput()
	if err != nil {
		c.fail("%s inspect %s: %v\n%s", engine, controlPlane, err, inspected)
	} else {
		c.ok("%s inspect %s exits 0", engine, controlPlane)
	}

	names := l.Nodes()
	if len(names) == 4 && names[0] == controlPlane {
		c.ok("l.Nodes() has four names, the first %s: %s", controlPlane, strings.Join(names, " "))
	} else {
		c.fail("l.Nodes() is %q, want four names, the first %s", names, controlPlane)
	}

	compareFiles(c, before, "after Up")
	if kc, err := clientcmd.LoadFromFile(l.Kubeconfig()); err != nil {
		c.fail("loading l.Kubeconfig(): %v", err)
	} else if kc.CurrentContext != "kind-"+clusterName {
		c.fail("the current context of l.Kubeconfig() is %q, want kind-%s", kc.CurrentContext, clusterName)
	} else {
		c.ok("l.Kubeconfig() exists at %s and its current context is %s", l.Kubeconfig(), kc.CurrentContext)
	}

	checkNodeObjects(ctx, c, l, names)

	for _, node := range names {
		checkInNode(c, l, node)
	}

	// Action 6: facts and the plan's versions.
	out, err := exec.CommandContext(ctx, engine, "info", "--format", "{{json .}}").Output()
	var engineInfo struct{ SecurityOptions []string }
	if err == nil {
		err = json.Unmarshal(out, &engineInfo)
	}
	if err != nil {
		info("%s info --format {{json .}} could not be read: %v", engine, err)
	} else {
		rootless := slices.ContainsFunc(engineInfo.SecurityOptions, func(s string) bool { return strings.Contains(s, "rootless") })
		info("%s info SecurityOptions: %q; one contains rootless: %t", engine, engineInfo.SecurityOptions, rootless)
	}

	if v := version.Version(); v == "0.33.0" {
		c.ok("version.Version() is %s", v)
	} else {
		c.fail("version.Version() is %q, want 0.33.0", v)
	}
	if strings.HasPrefix(defaults.Image, "kindest/node:v1.37.0@sha256:") {
		c.ok("defaults.Image is %s", defaults.Image)
	} else {
		c.fail("defaults.Image is %q, want kindest/node:v1.37.0@sha256:…", defaults.Image)
	}
	var containers []struct {
		Image  string
		Config struct{ Image string }
	}
	if err := json.Unmarshal(inspected, &containers); err != nil {
		info("the image of %s could not be read from %s inspect: %v", controlPlane, engine, err)
	}
	for _, ct := range containers {
		info("%s inspect %s: Image %q, Config.Image %q", engine, controlPlane, ct.Image, ct.Config.Image)
	}
	if sv, err := l.Client().Discovery().ServerVersion(); err != nil {
		c.fail("the server's version: %v", err)
	} else if strings.HasPrefix(sv.GitVersion, "v1.37.") {
		c.ok("the server's GitVersion is %s", sv.GitVersion)
	} else {
		c.fail("the server's GitVersion is %q, want v1.37.…", sv.GitVersion)
	}

	// Action 7: Down, and what it leaves.
	kubeconfigDir := filepath.Dir(l.Kubeconfig())
	if err := l.Down(); err != nil {
		c.fail("l.Down(): %v", err)
	} else {
		c.ok("l.Down() returned no error")
	}
	if _, err := os.Stat(kubeconfigDir); errors.Is(err, fs.ErrNotExist) {
		c.ok("the folder of l.Kubeconfig() is gone")
	} else {
		c.fail("the folder of l.Kubeconfig() is still there, or cannot be read: %v", err)
	}
	if out, err := exec.CommandContext(ctx, engine, "ps", "-a", "--format", "{{.Names}}").CombinedOutput(); err != nil {
		c.fail("%s ps -a: %v\n%s", engine, err, out)
	} else {
		var left []string
		for _, n := range strings.Fields(string(out)) {
			if strings.HasPrefix(n, clusterName) {
				left = append(left, n)
			}
		}
		if len(left) == 0 {
			c.ok("%s ps -a lists no name that starts with %s", engine, clusterName)
		} else {
			c.fail("%s ps -a still lists %s", engine, strings.Join(left, " "))
		}
	}
	compareFiles(c, before, "after Down")

	return summary(c)
}

// checkNodeObjects checks the zone labels and the control-plane taint through
// the API. names are the node names, the control-plane node first.
func checkNodeObjects(ctx context.Context, c *checks, l *lab.Lab, names []string) {
	zones := map[string]int{}
	for i, name := range names {
		n, err := l.Client().CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			c.fail("reading node %s: %v", name, err)
			continue
		}
		zone, has := n.Labels[zoneLabel]
		if i == 0 {
			if has {
				c.fail("the control-plane node %s has %s=%s, want no such label", name, zoneLabel, zone)
			} else {
				c.ok("the control-plane node %s has no label %s", name, zoneLabel)
			}
			if slices.ContainsFunc(n.Spec.Taints, func(t corev1.Taint) bool {
				return t.Key == "node-role.kubernetes.io/control-plane" && t.Effect == corev1.TaintEffectNoSchedule
			}) {
				c.ok("the control-plane node %s has the taint node-role.kubernetes.io/control-plane:NoSchedule", name)
			} else {
				c.fail("the control-plane node %s lacks the taint node-role.kubernetes.io/control-plane:NoSchedule; its taints: %v", name, n.Spec.Taints)
			}
			continue
		}
		if has {
			c.ok("worker %s has %s=%s", name, zoneLabel, zone)
			zones[zone]++
		} else {
			c.fail("worker %s has no label %s", name, zoneLabel)
		}
	}
	want := map[string]int{"rollproof-zone-1": 1, "rollproof-zone-2": 1, "rollproof-zone-3": 1}
	if maps.Equal(zones, want) {
		c.ok("the workers' zones are rollproof-zone-1, rollproof-zone-2 and rollproof-zone-3, once each")
	} else {
		c.fail("the workers' zones, with how often each occurs, are %v, want %v", zones, want)
	}
}

// checkInNode runs the three commands of action 5 in one node.
func checkInNode(c *checks, l *lab.Lab, node string) {
	if out, err := l.Exec(node, nil, "cat", "/var/lib/kubelet/config.json"); err != nil {
		c.fail("%s: cat /var/lib/kubelet/config.json: %v", node, err)
	} else if got := strings.TrimSpace(out); got != authsJSON {
		c.fail("%s: /var/lib/kubelet/config.json holds %q, want %s", node, got, authsJSON)
	} else {
		c.ok("%s: /var/lib/kubelet/config.json holds %s", node, got)
	}

	if out, err := l.Exec(node, nil, "containerd", "config", "dump"); err != nil {
		c.fail("%s: containerd config dump: %v", node, err)
	} else {
		var matching []string
		for line := range strings.Lines(out) {
			if configPathLine.MatchString(strings.TrimRight(line, "\n")) {
				matching = append(matching, strings.TrimSpace(line))
			}
		}
		if len(matching) > 0 {
			c.ok("%s: containerd config dump has %s", node, strings.Join(matching, " | "))
		} else {
			c.fail("%s: containerd config dump has no line config_path = \"/etc/containerd/certs.d\"", node)
		}
	}

	if out, err := l.Exec(node, nil, "crictl", "version"); err != nil {
		c.fail("%s: crictl version: %v", node, err)
	} else {
		runtimeVersion := "no RuntimeVersion line"
		for line := range strings.Lines(out) {
			if strings.HasPrefix(line, "RuntimeVersion") {
				runtimeVersion = strings.TrimSpace(line)
			}
		}
		c.ok("%s: crictl version exits 0: %s", node, runtimeVersion)
	}
}

// snapshot notes, for each path, whether the file exists and its SHA-256.
func snapshot(paths []string) []file {
	files := make([]file, len(paths))
	for i, p := range paths {
		data, err := os.ReadFile(p)
		var state string
		switch {
		case errors.Is(err, fs.ErrNotExist):
			state = "does not exist"
		case err != nil:
			state = "cannot be read: " + err.Error()
		default:
			state = fmt.Sprintf("has SHA-256 %x", sha256.Sum256(data))
		}
		files[i] = file{path: p, state: state}
	}
	return files
}

// compareFiles checks that each file of before is as it was.
func compareFiles(c *checks, before []file, when string) {
	paths := make([]string, len(before))
	for i, f := range before {
		paths[i] = f.path
	}
	for i, now := range snapshot(paths) {
		if now.state == before[i].state {
			c.ok("%s, %s", when, describe(now))
		} else {
			c.fail("%s, %s; before Up it %s", when, describe(now), before[i].state)
		}
	}
}

// describe names a file, with the home folder shown as $HOME, and says what
// was noted about it.
func describe(f file) string {
	name := f.path
	if home := os.Getenv("HOME"); home != "" {
		if rest, ok := strings.CutPrefix(name, home+string(filepath.Separator)); ok {
			name = filepath.Join("$HOME", rest)
		}
	}
	return name + " " + f.state
}

// summary prints the last line and returns the exit code.
func summary(c *checks) int {
	if c.failed == 0 {
		fmt.Println("p08: all checks passed")
		return 0
	}
	fmt.Printf("p08: %d checks failed\n", c.failed)
	return 1
}
