//go:build linux

// Proof of point 5: whether the Helm command lines of section 7 exist and
// mean the same under Helm 3.22.1 and Helm 4.3.2. In a kind cluster, with one
// Helm binary named by -helm, it runs an install, an upgrade with
// --reset-values, a rollback, a pull from a registry and an upgrade that
// fails, and reads each revision from helm status. It also reads which of an
// environment variable and the command line wins, and which flags each
// command's help lists.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"

	"rollproof-proofs/internal/lab"
)

const (
	namespace = "p05"
	release   = "p05"
	contrast  = "p05b"
	timeout   = "2m0s"
	values1   = "marker: one\n"
	archive   = "p05-0.1.0.tgz"
)

// takenOut are the variables taken out of every helm command's environment
// (section 7).
var takenOut = []string{
	"HELM_KUBECONTEXT", "HELM_KUBEAPISERVER", "HELM_KUBETOKEN", "HELM_KUBECAFILE",
	"HELM_KUBEASUSER", "HELM_KUBEASGROUPS", "HELM_KUBEINSECURE_SKIP_TLS_VERIFY",
	"HELM_KUBETLS_SERVER_NAME", "HELM_NAMESPACE", "HELM_DRIVER",
	"HELM_DRIVER_SQL_CONNECTION_STRING",
}

// helpFlags are the flags that check 8 looks for in the help texts.
var helpFlags = []string{"--reset-values", "--timeout", "--install", "--wait", "--atomic", "--rollback-on-failure"}

// checkNames are the checks of step 2. Each must run to its end, or the
// summary counts it as failed.
var checkNames = []string{"1", "2", "3", "4", "5", "6", "7", "8"}

func main() { os.Exit(run()) }

// checks counts the failed checks, prints one line for each check, and notes
// which checks ran to their end.
type checks struct {
	failed int
	done   map[string]bool
}

func (c *checks) ok(format string, args ...any) {
	fmt.Println("ok: " + shown(fmt.Sprintf(format, args...)))
}

func (c *checks) fail(format string, args ...any) {
	c.failed++
	fmt.Println("FAILED: " + shown(fmt.Sprintf(format, args...)))
}

// expect prints ok when held, else FAILED.
func (c *checks) expect(held bool, format string, args ...any) {
	if held {
		c.ok(format, args...)
	} else {
		c.fail(format, args...)
	}
}

func info(format string, args ...any) { fmt.Println("info: " + shown(fmt.Sprintf(format, args...))) }

// shown writes the home folder and the temporary folder at the start of a
// path as $HOME and $TMP.
func shown(s string) string {
	if home := os.Getenv("HOME"); home != "" {
		s = strings.ReplaceAll(s, home+string(filepath.Separator), "$HOME"+string(filepath.Separator))
	}
	return strings.ReplaceAll(s, os.TempDir()+string(filepath.Separator), "$TMP"+string(filepath.Separator))
}

// helm runs one Helm binary as section 7 runs it: with no shell, in the
// folder of this proof, and with the variables of takenOut taken out of its
// environment.
type helm struct {
	path string   // absolute
	dir  string   // the folder p05-helm-cli, so that ./chart names the chart
	env  []string // the program's environment less takenOut, with Helm's three homes in the run's folder
	cl   []string // --namespace p05 --kubeconfig F --kube-context CTX
}

// result is what one helm command did.
type result struct {
	code   int // the exit code; -1 when err is set
	stdout string
	stderr string
	err    error // the command did not start, or a signal ended it
}

// exited reports whether the command ran and exited by itself.
func (r result) exited() bool { return r.err == nil }

// run starts helm with args and with extra added to its environment, prints
// the command line, the exit code and the output, and returns them.
func (h *helm) run(ctx context.Context, extra []string, args ...string) result {
	line := "helm " + strings.Join(args, " ")
	if len(extra) > 0 {
		line = "env " + strings.Join(extra, " ") + " " + line
	}
	info("run: %s", line)
	cmd := exec.CommandContext(ctx, h.path, args...)
	cmd.Dir = h.dir
	cmd.Env = append(slices.Clone(h.env), extra...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	r := result{stdout: stdout.String(), stderr: stderr.String()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr) && exitErr.ExitCode() >= 0:
		r.code = exitErr.ExitCode()
	default:
		r.code, r.err = -1, err
	}
	if r.err != nil {
		info("did not exit by itself: %v", r.err)
	} else {
		info("exit code %d", r.code)
	}
	printOutput("out", r.stdout)
	printOutput("err", r.stderr)
	return r
}

// printOutput prints each line of s, indented and marked with name.
func printOutput(name, s string) {
	if s == "" {
		fmt.Printf("  %s| (empty)\n", name)
		return
	}
	for line := range strings.Lines(s) {
		fmt.Printf("  %s| %s\n", name, shown(strings.TrimRight(line, "\n")))
	}
}

// with returns args followed by the flags that name the test cluster.
func (h *helm) with(args ...string) []string { return append(args, h.cl...) }

// releaseStatus is what this proof reads from helm status -o json.
type releaseStatus struct {
	Namespace string `json:"namespace"`
	Version   *int   `json:"version"`
	Info      struct {
		Status string `json:"status"`
	} `json:"info"`
}

// status runs helm status RELEASE CL -o json, with extra added to the
// environment, and returns what it printed. err says why there is no status:
// the command failed, or its output is not that JSON.
func (h *helm) status(ctx context.Context, extra []string) (releaseStatus, result, error) {
	r := h.run(ctx, extra, append(h.with("status", release), "-o", "json")...)
	var s releaseStatus
	switch {
	case !r.exited():
		return s, r, r.err
	case r.code != 0:
		return s, r, fmt.Errorf("exit code %d: %s", r.code, firstLine(r.stderr))
	}
	if err := json.Unmarshal([]byte(r.stdout), &s); err != nil {
		return s, r, fmt.Errorf("the output is not JSON: %w", err)
	}
	if s.Version == nil {
		return s, r, errors.New("the output has no field version")
	}
	return s, r, nil
}

// expectRevision runs helm status and checks that its version is want.
func (h *helm) expectRevision(ctx context.Context, c *checks, want int, when string) {
	s, _, err := h.status(ctx, nil)
	if err != nil {
		c.fail("%s, helm status: %v", when, err)
		return
	}
	c.expect(*s.Version == want, "%s, helm status -o json gives version %d (want %d), info.status %s", when, *s.Version, want, s.Info.Status)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func run() int {
	helmFlag := flag.String("helm", "", "the Helm binary to prove")
	flag.Parse()
	if *helmFlag == "" {
		fmt.Fprintln(os.Stderr, "p05: -helm PATH is required")
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c := &checks{done: map[string]bool{}}
	version := ""

	helmPath, err := filepath.Abs(*helmFlag)
	if err != nil {
		c.fail("making -helm %s absolute: %v", *helmFlag, err)
		return summary(ctx, c, version)
	}
	info("helm is %s, made absolute", *helmFlag)
	proofDir, err := filepath.Abs("p05-helm-cli")
	if err == nil {
		_, err = os.Stat(filepath.Join(proofDir, "chart", "Chart.yaml"))
	}
	if err != nil {
		c.fail("p05-helm-cli/chart/Chart.yaml (run from proofs/): %v", err)
		return summary(ctx, c, version)
	}

	// The run's folder: Helm's three homes, the values file, the packaged
	// chart and the pulled one.
	tmp, err := os.MkdirTemp("", "rollproof-proof-p05-helm-")
	if err != nil {
		c.fail("a temporary folder for the run: %v", err)
		return summary(ctx, c, version)
	}
	defer os.RemoveAll(tmp)
	for _, d := range []string{"cache", "config", "data", "package", "pull"} {
		if err := os.Mkdir(filepath.Join(tmp, d), 0o700); err != nil {
			c.fail("creating %s: %v", filepath.Join(tmp, d), err)
			return summary(ctx, c, version)
		}
	}
	valuesFile := filepath.Join(tmp, "values1.yaml")
	if err := os.WriteFile(valuesFile, []byte(values1), 0o600); err != nil {
		c.fail("writing %s: %v", valuesFile, err)
		return summary(ctx, c, version)
	}

	h := &helm{path: helmPath, dir: proofDir}
	var wasSet []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch {
		case slices.Contains(takenOut, name):
			wasSet = append(wasSet, name)
		case name == "HELM_CACHE_HOME" || name == "HELM_CONFIG_HOME" || name == "HELM_DATA_HOME":
		default:
			h.env = append(h.env, kv)
		}
	}
	h.env = append(h.env,
		"HELM_CACHE_HOME="+filepath.Join(tmp, "cache"),
		"HELM_CONFIG_HOME="+filepath.Join(tmp, "config"),
		"HELM_DATA_HOME="+filepath.Join(tmp, "data"))
	if len(wasSet) == 0 {
		info("none of the %d variables to take out was set", len(takenOut))
	} else {
		info("taken out of the environment: %s", strings.Join(wasSet, " "))
	}
	info("HELM_CACHE_HOME, HELM_CONFIG_HOME and HELM_DATA_HOME are cache, config and data in %s", tmp)

	// Step 1: the cluster, the namespace and the registry.
	cfg := &v1alpha4.Cluster{Nodes: []v1alpha4.Node{{Role: v1alpha4.ControlPlaneRole}, {Role: v1alpha4.WorkerRole}}}
	l, err := lab.Up(ctx, "p05", cfg)
	if err != nil {
		c.fail("lab.Up: %v", err)
		return summary(ctx, c, version)
	}
	defer func() {
		if err := l.Down(); err != nil {
			fmt.Fprintf(os.Stderr, "p05: %v\n", err)
		}
	}()
	kc, err := clientcmd.LoadFromFile(l.Kubeconfig())
	if err != nil || kc.CurrentContext == "" {
		c.fail("the current context of %s: %v", l.Kubeconfig(), err)
		return summary(ctx, c, version)
	}
	h.cl = []string{"--namespace", namespace, "--kubeconfig", l.Kubeconfig(), "--kube-context", kc.CurrentContext}
	if _, err := l.Client().CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}, metav1.CreateOptions{}); err != nil {
		c.fail("creating the namespace %s: %v", namespace, err)
		return summary(ctx, c, version)
	}
	info("created the namespace %s; the current context is %s", namespace, kc.CurrentContext)
	charts, err := l.Registry(ctx, "charts")
	if err != nil {
		c.fail("the registry: %v", err)
		return summary(ctx, c, version)
	}
	info("the registry %s answers at %s", charts.Name, charts.Push)

	// Step 2: the checks, in order; an interrupt stops them.
	steps := []func(){
		func() { version = checkVersion(ctx, c, h) },
		func() { checkInstall(ctx, c, h, l, valuesFile) },
		func() { checkReset(ctx, c, h, l, valuesFile) },
		func() { checkRollback(ctx, c, h, l) },
		func() { checkPull(ctx, c, h, tmp, charts.Push) },
		func() { checkFailure(ctx, c, h) },
		func() { checkOverrides(ctx, c, h) },
		func() { checkFlags(ctx, c, h) },
	}
	for i, step := range steps {
		if ctx.Err() != nil {
			break
		}
		fmt.Printf("info: check %d\n", i+1)
		step()
	}

	// Step 3: Down.
	if err := l.Down(); err != nil {
		c.fail("l.Down(): %v", err)
	} else {
		c.ok("l.Down() returned no error")
	}
	return summary(ctx, c, version)
}

// checkVersion is check 1. It returns the version without its build
// metadata, or "" when the check failed.
func checkVersion(ctx context.Context, c *checks, h *helm) string {
	defer func() { c.done["1"] = true }()
	r := h.run(ctx, nil, "version", "--short")
	line := firstLine(r.stdout)
	if !r.exited() || r.code != 0 || !strings.HasPrefix(line, "v") {
		c.fail("helm version --short: exit code %d, first line %q; want exit code 0 and a line that starts with v", r.code, line)
		return ""
	}
	c.ok("helm version --short exits 0 and prints %s", line)
	v, _, _ := strings.Cut(line, "+")
	return v
}

// checkInstall is check 2.
func checkInstall(ctx context.Context, c *checks, h *helm, l *lab.Lab, valuesFile string) {
	defer func() { c.done["2"] = true }()
	r := h.run(ctx, nil, append(h.with("upgrade", "--install", release, "./chart"), "--timeout", timeout, "--values", valuesFile)...)
	c.expect(r.exited() && r.code == 0, "helm upgrade --install %s ./chart … --values VALUES1 exits %d (want 0)", release, r.code)
	if d, err := deployment(ctx, l, release); err != nil {
		c.fail("reading the Deployment %s when the install returned: %v", release, err)
	} else {
		info("when the install returned, readyReplicas was 2: %s; %s", yesNo(d.Status.ReadyReplicas == 2), rollout(d))
	}
	s, _, err := h.status(ctx, nil)
	if err != nil {
		c.fail("after the install, helm status: %v", err)
		return
	}
	c.expect(*s.Version == 1 && s.Info.Status == "deployed",
		"after the install, helm status -o json gives version %d and info.status %s (want 1 and deployed)", *s.Version, s.Info.Status)
}

// checkReset is check 3, with its contrast.
func checkReset(ctx context.Context, c *checks, h *helm, l *lab.Lab, valuesFile string) {
	defer func() { c.done["3"] = true }()
	r := h.run(ctx, nil, append(h.with("upgrade", release, "./chart", "--reset-values"), "--timeout", timeout)...)
	c.expect(r.exited() && r.code == 0, "helm upgrade %s ./chart --reset-values … exits %d (want 0)", release, r.code)
	if d, err := deployment(ctx, l, release); err != nil {
		c.fail("reading the Deployment %s when the upgrade returned: %v", release, err)
	} else {
		info("when the upgrade returned: %s", rollout(d))
	}
	h.expectRevision(ctx, c, 2, "after the upgrade")

	r = h.run(ctx, nil, append(h.with("get", "values", release), "-o", "json")...)
	got := strings.TrimSpace(r.stdout)
	c.expect(r.exited() && r.code == 0 && (got == "null" || got == "{}"),
		"helm get values %s -o json exits %d and prints %q (want 0, and null or {})", release, r.code, got)
	if d, err := deployment(ctx, l, release); err != nil {
		c.fail("reading the Deployment %s: %v", release, err)
	} else {
		m := d.Spec.Template.Annotations["marker"]
		c.expect(m == "chart", "the pod template annotation marker of Deployment %s is %q (want chart)", release, m)
	}

	// The contrast: an upgrade with no values and no --reset-values.
	r = h.run(ctx, nil, append(h.with("upgrade", "--install", contrast, "./chart"), "--timeout", timeout, "--values", valuesFile)...)
	c.expect(r.exited() && r.code == 0, "contrast: helm upgrade --install %s ./chart … --values VALUES1 exits %d (want 0)", contrast, r.code)
	r = h.run(ctx, nil, append(h.with("upgrade", contrast, "./chart"), "--timeout", timeout)...)
	c.expect(r.exited() && r.code == 0, "contrast: helm upgrade %s ./chart … with no values exits %d (want 0)", contrast, r.code)
	if d, err := deployment(ctx, l, contrast); err != nil {
		c.fail("contrast: reading the Deployment %s: %v", contrast, err)
	} else {
		info("contrast: after an upgrade with no values and no --reset-values, the annotation marker of Deployment %s is %q", contrast, d.Spec.Template.Annotations["marker"])
	}
}

// checkRollback is check 4.
func checkRollback(ctx context.Context, c *checks, h *helm, l *lab.Lab) {
	defer func() { c.done["4"] = true }()
	r := h.run(ctx, nil, append(h.with("rollback", release, "1"), "--timeout", timeout)...)
	c.expect(r.exited() && r.code == 0, "helm rollback %s 1 … exits %d (want 0)", release, r.code)
	if d, err := deployment(ctx, l, release); err != nil {
		c.fail("reading the Deployment %s when the rollback returned: %v", release, err)
	} else {
		info("when the rollback returned: %s", rollout(d))
	}
	h.expectRevision(ctx, c, 3, "after the rollback")

	m1 := h.run(ctx, nil, append(h.with("get", "manifest", release), "--revision", "1")...)
	m3 := h.run(ctx, nil, append(h.with("get", "manifest", release), "--revision", "3")...)
	c.expect(m1.exited() && m1.code == 0 && m3.exited() && m3.code == 0 && m1.stdout != "" && m1.stdout == m3.stdout,
		"helm get manifest --revision 1 and --revision 3 exit %d and %d and print the same text: %s", m1.code, m3.code, yesNo(m1.stdout != "" && m1.stdout == m3.stdout))
}

// checkPull is check 5.
func checkPull(ctx context.Context, c *checks, h *helm, tmp, push string) {
	defer func() { c.done["5"] = true }()
	pkg := filepath.Join(tmp, "package")
	dir := filepath.Join(tmp, "pull")
	r := h.run(ctx, nil, "package", "./chart", "-d", pkg)
	_, statErr := os.Stat(filepath.Join(pkg, archive))
	c.expect(r.exited() && r.code == 0 && statErr == nil, "helm package ./chart -d TMP exits %d and writes TMP/%s: %s", r.code, archive, yesNo(statErr == nil))

	r = h.run(ctx, nil, "push", filepath.Join(pkg, archive), "oci://"+push+"/charts", "--plain-http")
	c.expect(r.exited() && r.code == 0, "helm push TMP/%s oci://PUSH/charts --plain-http exits %d (want 0)", archive, r.code)

	r = h.run(ctx, nil, "pull", "oci://"+push+"/charts/p05", "--version", "0.1.0", "--destination", dir, "--plain-http")
	var names []string
	entries, err := os.ReadDir(dir)
	for _, e := range entries {
		names = append(names, e.Name())
	}
	c.expect(r.exited() && r.code == 0 && err == nil && slices.Contains(names, archive),
		"helm pull oci://PUSH/charts/p05 --version 0.1.0 --destination DIR --plain-http exits %d; DIR holds %q (want %s; read error: %v)", r.code, names, archive, err)

	r = h.run(ctx, nil, "show", "chart", filepath.Join(dir, archive))
	var lines []string
	for line := range strings.Lines(r.stdout) {
		lines = append(lines, strings.TrimSpace(line))
	}
	hasName, hasVersion := slices.Contains(lines, "name: p05"), slices.Contains(lines, "version: 0.1.0")
	c.expect(r.exited() && r.code == 0 && hasName && hasVersion,
		"helm show chart DIR/%s exits %d; a line name: p05: %s; a line version: 0.1.0: %s", archive, r.code, yesNo(hasName), yesNo(hasVersion))
}

// checkFailure is check 6.
func checkFailure(ctx context.Context, c *checks, h *helm) {
	defer func() { c.done["6"] = true }()
	r := h.run(ctx, nil, append(h.with("upgrade", release, "./chart", "--reset-values", "--set", "replicas=two"), "--timeout", timeout)...)
	c.expect(r.exited() && r.code != 0, "helm upgrade %s ./chart --reset-values --set replicas=two … exits non-zero: %s", release, yesNo(r.exited() && r.code != 0))
	if r.exited() {
		info("the failed upgrade's exit code is %d", r.code)
	}
	h.expectRevision(ctx, c, 3, "after the failed upgrade")
}

// checkOverrides is check 7.
func checkOverrides(ctx context.Context, c *checks, h *helm) {
	defer func() { c.done["7"] = true }()
	s, r, err := h.status(ctx, []string{"HELM_NAMESPACE=elsewhere"})
	switch {
	case !r.exited():
		c.fail("with HELM_NAMESPACE=elsewhere, helm status did not run: %v", r.err)
	case err == nil && s.Namespace == namespace:
		info("with HELM_NAMESPACE=elsewhere, helm status exits %d and finds the release in namespace %s: the command line's --namespace wins", r.code, s.Namespace)
	default:
		info("with HELM_NAMESPACE=elsewhere, helm status exits %d and does not find the release in %s (%v): HELM_NAMESPACE wins", r.code, namespace, err)
	}

	r = h.run(ctx, []string{"HELM_KUBEAPISERVER=https://127.0.0.1:1"}, append(h.with("status", release), "-o", "json")...)
	if !r.exited() {
		c.fail("with HELM_KUBEAPISERVER=https://127.0.0.1:1, helm status did not run: %v", r.err)
	} else {
		info("with HELM_KUBEAPISERVER=https://127.0.0.1:1, helm status exits %d; its error names 127.0.0.1:1: %s; error: %s",
			r.code, yesNo(strings.Contains(r.stderr, "127.0.0.1:1")), firstLine(r.stderr))
	}
}

// checkFlags is check 8.
func checkFlags(ctx context.Context, c *checks, h *helm) {
	defer func() { c.done["8"] = true }()
	var unlisted []string
	for _, cmd := range []string{"upgrade", "rollback"} {
		r := h.run(ctx, nil, cmd, "--help")
		if !r.exited() || r.code != 0 {
			c.fail("helm %s --help exits %d (want 0)", cmd, r.code)
			continue
		}
		for _, f := range helpFlags {
			isListed := listed(r.stdout, f)
			info("helm %s --help: %s listed as a flag: %s; named in the text: %s", cmd, f, yesNo(isListed), yesNo(named(r.stdout, f)))
			if cmd == "upgrade" && !isListed {
				unlisted = append(unlisted, f)
			}
		}
	}
	for _, f := range unlisted {
		r := h.run(ctx, nil, "upgrade", f, "--help")
		if !r.exited() {
			c.fail("helm upgrade %s --help did not run: %v", f, r.err)
			continue
		}
		info("helm upgrade %s --help exits %d; Helm answers unknown flag: %s; first line of its error output: %s",
			f, r.code, yesNo(strings.Contains(r.stderr, "unknown flag")), firstLine(r.stderr))
	}
}

// listed reports whether help lists flag in its Flags list: a line that
// starts with spaces, perhaps a short flag, and flag.
func listed(help, flag string) bool {
	return regexp.MustCompile(`(?m)^\s+(-[a-zA-Z], )?` + regexp.QuoteMeta(flag) + `([\s=\[]|$)`).MatchString(help)
}

// named reports whether help holds flag anywhere, as a whole flag and not as
// the start of a longer one.
func named(help, flag string) bool {
	return regexp.MustCompile(regexp.QuoteMeta(flag) + `([^a-zA-Z0-9-]|$)`).MatchString(help)
}

func deployment(ctx context.Context, l *lab.Lab, name string) (*appsv1.Deployment, error) {
	return l.Client().AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
}

// rollout describes a Deployment's progress, and whether its rollout is
// complete by the rule of kubectl rollout status: the new generation is
// observed, and every replica is updated and available.
func rollout(d *appsv1.Deployment) string {
	want := int32(1)
	if d.Spec.Replicas != nil {
		want = *d.Spec.Replicas
	}
	s := d.Status
	complete := s.ObservedGeneration >= d.Generation && s.UpdatedReplicas == want && s.Replicas == s.UpdatedReplicas && s.AvailableReplicas == s.UpdatedReplicas
	return fmt.Sprintf("Deployment %s: generation %d, observedGeneration %d, replicas %d, updatedReplicas %d, readyReplicas %d, availableReplicas %d; complete: %s",
		d.Name, d.Generation, s.ObservedGeneration, s.Replicas, s.UpdatedReplicas, s.ReadyReplicas, s.AvailableReplicas, yesNo(complete))
}

// summary prints the last line and returns the exit code.
func summary(ctx context.Context, c *checks, version string) int {
	for _, n := range checkNames {
		if !c.done[n] {
			c.fail("check %s did not run to its end", n)
		}
	}
	if ctx.Err() != nil {
		c.fail("the run was interrupted")
	}
	if c.failed == 0 {
		fmt.Printf("p05: all checks passed for %s\n", version)
		return 0
	}
	if version == "" {
		fmt.Printf("p05: %d checks failed\n", c.failed)
	} else {
		fmt.Printf("p05: %d checks failed for %s\n", c.failed, version)
	}
	return 1
}
