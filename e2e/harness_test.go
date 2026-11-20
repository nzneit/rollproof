//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// root is the project's folder, one level above this package's.
var root = func() string {
	abs, err := filepath.Abs("..")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: the project's folder: %v\n", err)
		os.Exit(1)
	}
	return abs
}()

// engine is the container engine's command name, chosen in TestMain.
var engine string

// busyboxRef is busybox 1.37.0 by the digest of its tag's index, the image of the TCP loop.
// Measured on 2026-11-19 with
//
//	nerdctl pull --platform linux/amd64 docker.io/library/busybox:1.37.0
//	nerdctl image inspect --format '{{index .RepoDigests 0}}' docker.io/library/busybox:1.37.0
const busyboxRef = "docker.io/library/busybox@sha256:a7dbc319b1438115a207e11930dc4bb3e643dcb3ec0bdd694bb36cd8fad289ec"

// busyboxArchive is where busybox's archive stays between runs of the suite.
var busyboxArchive = filepath.Join(root, ".tools", "busybox-1.37.0.tar")

// archives holds the path of each archive that TestMain prepared, by images entry: previous and
// candidate in a temporary folder, busybox under .tools/.
var archives = map[string]string{"busybox": busyboxArchive}

// archiveDigests is the digest of each fixture image's archive that TestMain saved, by images
// entry: previous and candidate, with the sha256: prefix, as the tool stores
// an archive under it.
var archiveDigests = map[string]string{}

// TestMain picks the engine, checks the Helm binaries and kubectl, builds the binary, the two
// fixture images and the fixture probe's image once, saves the fixture images as archives into a
// temporary folder that it removes after the tests, fetches busybox's archive when .tools/ does
// not hold it, and then runs the tests. A failure prints the command and its output to standard
// error and exits 1.
func TestMain(m *testing.M) {
	engine = pickEngine()
	if engine == "" {
		fmt.Fprintln(os.Stderr, "e2e: no container engine: set KIND_EXPERIMENTAL_PROVIDER, or install docker, nerdctl or podman")
		os.Exit(1)
	}
	for _, helm := range []string{"helm-3", "helm-4"} {
		if fi, err := os.Stat(filepath.Join(root, ".tools", helm, "helm")); err != nil || !fi.Mode().IsRegular() {
			fmt.Fprintln(os.Stderr, "the Helm binaries are missing under .tools/; run: go run ./hack/fetchhelm")
			os.Exit(1)
		}
	}
	if fi, err := os.Stat(filepath.Join(root, ".tools", "kubectl")); err != nil || !fi.Mode().IsRegular() {
		fmt.Fprintln(os.Stderr, "kubectl is missing under .tools/; run: go run ./hack/fetchhelm")
		os.Exit(1)
	}
	checkHelmMajor("helm-3", "v3.")
	checkHelmMajor("helm-4", "v4.")
	if err := goBuild("build", "-trimpath", "-ldflags", "-s -w -X main.version=dev", "-o", filepath.Join("bin", "rollproof"), "./cmd/rollproof"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, version := range []string{"previous", "candidate"} {
		buildImage("rollproof-fixture:"+version, "./fixtures/service", "-s -w -X main.version="+version, "svc")
	}
	buildImage("rollproof-fixture-probe:dev", "./fixtures/probe", "-s -w", "probe")
	fetchBusybox()
	dir := saveArchives()
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// pickEngine returns the engine's command name: KIND_EXPERIMENTAL_PROVIDER when
// it names docker, podman or nerdctl, else the first of docker, nerdctl and
// podman whose info command exits 0, else "".
func pickEngine() string {
	switch p := os.Getenv("KIND_EXPERIMENTAL_PROVIDER"); p {
	case "docker", "podman", "nerdctl":
		return p
	}
	for _, name := range []string{"docker", "nerdctl", "podman"} {
		if err := exec.Command(name, "info").Run(); err == nil {
			return name
		}
	}
	return ""
}

// helmEnv is the environment of a command that runs Helm from dir, with dir first on the PATH.
func helmEnv(dir string) []string {
	return append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// checkHelmMajor fails unless the Helm binary of major prints a version that begins prefix. A
// failure names the binary and what it printed, and exits 1.
func checkHelmMajor(major, prefix string) {
	dir := filepath.Join(root, ".tools", major)
	binary := filepath.Join(dir, "helm")
	cmd := exec.Command(binary, "version", "--short")
	cmd.Dir = dir
	cmd.Env = helmEnv(dir)
	out, err := cmd.CombinedOutput()
	got := strings.TrimSpace(string(out))
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: %s: %v\n%s", binary, err, out)
		os.Exit(1)
	}
	if !strings.HasPrefix(got, prefix) {
		fmt.Fprintf(os.Stderr, "e2e: %s printed %q, which does not begin %q\n", binary, got, prefix)
		os.Exit(1)
	}
}

// goBuild runs go with args in root, with CGO_ENABLED=0 in its environment. An error names the
// command and holds its output.
func goBuild(args ...string) error {
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("e2e: go %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}

// engineDo runs the engine with args in root. A failure prints the command and its output and
// exits 1, after removing each path in cleanup.
func engineDo(cleanup []string, args ...string) {
	cmd := exec.Command(engine, args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		for _, p := range cleanup {
			os.RemoveAll(p)
		}
		fmt.Fprintf(os.Stderr, "e2e: %s %s: %v\n%s", engine, strings.Join(args, " "), err, out)
		os.Exit(1)
	}
}

// buildImage builds the image tag from the Go package pkg, built with ldflags as the binary bin,
// and the Dockerfile in pkg's folder, in a temporary folder that it removes when the image is
// built. The image of the fixture service is rollproof-fixture:VERSION, and that of the fixture
// probe rollproof-fixture-probe:dev.
func buildImage(tag, pkg, ldflags, bin string) {
	dir, err := os.MkdirTemp("", "rollproof-image-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: a temporary folder for %s: %v\n", tag, err)
		os.Exit(1)
	}
	if err := goBuild("build", "-trimpath", "-ldflags", ldflags, "-o", filepath.Join(dir, bin), pkg); err != nil {
		os.RemoveAll(dir)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	dockerfile, err := os.ReadFile(filepath.Join(root, pkg, "Dockerfile"))
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "Dockerfile"), dockerfile, 0o644)
	}
	if err != nil {
		os.RemoveAll(dir)
		fmt.Fprintf(os.Stderr, "e2e: the Dockerfile of %s: %v\n", pkg, err)
		os.Exit(1)
	}
	engineDo([]string{dir}, "build", "-t", tag, dir)
	os.RemoveAll(dir)
}

// fetchBusybox puts busybox 1.37.0 for linux/amd64 at busyboxArchive unless a whole image
// archive is there: it pulls busyboxRef, tags it rollproof-fixture-busybox:1.37.0, and saves it to
// a temporary name beside the archive, renamed into place once the save succeeded.
func fetchBusybox() {
	err := readableArchive(busyboxArchive)
	if err == nil {
		return
	}
	if !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "e2e: %s cannot be used (%v); making it again\n", busyboxArchive, err)
	}
	const tag = "rollproof-fixture-busybox:1.37.0"
	engineDo(nil, "pull", "--platform", "linux/amd64", busyboxRef)
	engineDo(nil, "tag", busyboxRef, tag)
	tmp := busyboxArchive + ".partial"
	engineDo([]string{tmp}, "save", "-o", tmp, tag)
	if err := os.Rename(tmp, busyboxArchive); err != nil {
		os.Remove(tmp)
		fmt.Fprintf(os.Stderr, "e2e: busybox's archive: %v\n", err)
		os.Exit(1)
	}
}

// readableArchive returns nil when path is a regular file that holds one image whose digest
// tarball.ImageFromPath can compute, which reads every layer; else the reason.
func readableArchive(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	img, err := tarball.ImageFromPath(path, nil)
	if err == nil {
		_, err = img.Digest()
	}
	return err
}

// saveArchives saves rollproof-fixture:previous and :candidate as archives into a new temporary
// folder, records their paths in archives and their digests in archiveDigests, and returns the
// folder. A failure removes the folder and exits 1.
func saveArchives() string {
	dir, err := os.MkdirTemp("", "rollproof-fixture-archives-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: a temporary folder for the archives: %v\n", err)
		os.Exit(1)
	}
	for _, entry := range []string{"previous", "candidate"} {
		path := filepath.Join(dir, entry+".tar")
		engineDo([]string{dir}, "save", "-o", path, "rollproof-fixture:"+entry)
		var digest v1.Hash
		img, err := tarball.ImageFromPath(path, nil)
		if err == nil {
			digest, err = img.Digest()
		}
		if err != nil {
			os.RemoveAll(dir)
			fmt.Fprintf(os.Stderr, "e2e: the archive of rollproof-fixture:%s: %v\n", entry, err)
			os.Exit(1)
		}
		archives[entry] = path
		archiveDigests[entry] = digest.String()
	}
	return dir
}

// transitionResult is one entry of result.json's transitions, as far as the
// fixtures read it.
type transitionResult struct {
	Name      string `json:"name"`
	Workloads []struct {
		Workload       string `json:"workload"`
		PodsReplaced   int    `json:"pods_replaced"`
		Probed         string `json:"probed"`
		QuickestStopMs *int64 `json:"quickest_stop_ms"`
	} `json:"workloads"`
}

// result is what a fixture run gave: the exit code, the verdict, the set of
// reason codes, the deciding code, each window's result by name, and the
// signal that ended the tool.
type result struct {
	exit    int
	verdict string
	codes   []string
	decided string
	windows map[string]string
	// signal is the name of the signal that ended the tool, such as "interrupt"; empty
	// when it exited by itself. exit is then -1.
	signal string
}

// runFixture runs the real binary once on one fixture, with one Helm binary
// and kubectl first on the PATH, and returns what result.json says and the
// exit code. For a fixture with commands it first renders the manifests that
// they apply. While the tool runs, it calls the fixture's action, if any, with
// the window marks, and fails the test when the run ended before the action
// reported done. It keeps the tool's standard output in the results folder as
// console.txt. TestMain builds the binary and the images once for the whole
// test run.
func runFixture(t *testing.T, f fixture) result {
	t.Helper()
	out := outDir(t)
	if err := os.RemoveAll(out); err != nil {
		t.Fatalf("%s: the results folder %s: %v", f.name, out, err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatalf("%s: the results folder %s: %v", f.name, out, err)
	}
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		t.Logf("%s: the results folder is %s", f.name, out)
		b, err := os.ReadFile(filepath.Join(out, "evidence", "probe.log"))
		if err != nil {
			t.Logf("%s: evidence/probe.log: %v", f.name, err)
			return
		}
		lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
		if len(lines) > 40 {
			lines = lines[len(lines)-40:]
		}
		t.Logf("%s: the last %d lines of evidence/probe.log:\n%s", f.name, len(lines), strings.Join(lines, "\n"))
	})

	description, err := renderConfig(f, inputs{chart: filepath.Join(root, "fixtures", "chart"), archives: archives})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "rollproof.yaml")
	if err := os.WriteFile(file, []byte(description), 0o644); err != nil {
		t.Fatalf("%s: the run description: %v", f.name, err)
	}

	helmDir := t.TempDir()
	for name, target := range map[string]string{
		"helm":    filepath.Join(root, ".tools", f.helm, "helm"),
		"kubectl": filepath.Join(root, ".tools", "kubectl"),
	} {
		if err := os.Symlink(target, filepath.Join(helmDir, name)); err != nil {
			t.Fatalf("%s: the %s link: %v", f.name, name, err)
		}
	}
	env := helmEnv(helmDir)
	if f.commands != nil {
		renderManifests(t, f, filepath.Dir(file), env)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(root, "bin", "rollproof"),
		"run", "-f", file, "--out", out, "--name", "e2e")
	cmd.Dir = root
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// While SIGINT is handled here, the tool starts with SIGINT at its default action, also when
	// go test was started with SIGINT ignored.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	startErr := cmd.Start()
	signal.Stop(c)
	if startErr != nil {
		t.Fatalf("%s (%s): %v", f.name, f.helm, startErr)
	}
	// Registered once the tool runs, so that a kept cluster goes also when the test fails before
	// it reaches checkClusterKept.
	if f.keepsCluster {
		t.Cleanup(func() { removeKeptCluster(t) })
	}
	ended := make(chan error, 1)
	go func() { ended <- cmd.Wait() }()
	acted, actErr := act(f, newRunning(cmd.Process, env), filepath.Join(out, "evidence", "windows.json"), ended, 100*time.Millisecond)
	runErr := <-ended
	t.Logf("%s (%s):\n%s%s", f.name, f.helm, stdout.String(), stderr.String())
	if err := os.WriteFile(filepath.Join(out, "console.txt"), stdout.Bytes(), 0o644); err != nil {
		t.Errorf("%s (%s): console.txt: %v", f.name, f.helm, err)
	}
	if ctx.Err() != nil {
		t.Fatalf("%s (%s): the run did not end within 15m", f.name, f.helm)
	}
	if actErr != nil {
		t.Errorf("%s (%s): the action: %v", f.name, f.helm, actErr)
	} else if f.act != nil && !acted {
		t.Errorf("%s (%s): the run ended before its action reported done", f.name, f.helm)
	}

	r := result{}
	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
	case errors.As(runErr, &exitErr):
		r.exit = exitErr.ExitCode()
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			r.signal = ws.Signal().String()
		}
	default:
		t.Fatalf("%s (%s): %v", f.name, f.helm, runErr)
	}

	b, err := os.ReadFile(filepath.Join(out, "result.json"))
	if err != nil {
		t.Fatalf("%s (%s): result.json: %v", f.name, f.helm, err)
	}
	var got struct {
		Final   bool   `json:"final"`
		Verdict string `json:"verdict"`
		Reasons []struct {
			Code    string `json:"code"`
			Decided bool   `json:"decided"`
		} `json:"reasons"`
		Windows []struct {
			Name   string `json:"name"`
			Result string `json:"result"`
		} `json:"windows"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("%s (%s): result.json: %v", f.name, f.helm, err)
	}
	if !got.Final {
		t.Fatalf("%s (%s): result.json is not final; its verdict is %q", f.name, f.helm, got.Verdict)
	}
	r.verdict = got.Verdict
	seen := make(map[string]bool, len(got.Reasons))
	for _, reason := range got.Reasons {
		if !seen[reason.Code] {
			seen[reason.Code] = true
			r.codes = append(r.codes, reason.Code)
		}
		if reason.Decided {
			r.decided = reason.Code
		}
	}
	sort.Strings(r.codes)
	r.windows = make(map[string]string, len(got.Windows))
	for _, w := range got.Windows {
		r.windows[w.Name] = w.Result
	}
	if f.keepsCluster {
		checkClusterKept(t, f)
	} else {
		checkClusterGone(t, f)
	}
	return r
}

// kubectlLimit bounds each kubectl command that an action runs.
const kubectlLimit = time.Minute

// newRunning returns the run in progress of process as an action reaches it: its signal goes to
// the tool's process, and its kubectl runs .tools/kubectl with env and the test cluster's
// kubeconfig, killed after kubectlLimit, which is then its error.
func newRunning(process *os.Process, env []string) running {
	return running{
		signal: process.Signal,
		kubectl: func(args ...string) error {
			ctx, cancel := context.WithTimeout(context.Background(), kubectlLimit)
			defer cancel()
			cmd := exec.CommandContext(ctx, filepath.Join(root, ".tools", "kubectl"), args...)
			cmd.Dir = root
			cmd.Env = append(slices.Clip(env), "KUBECONFIG="+filepath.Join(os.TempDir(), "rollproof-e2e", "kubeconfig"))
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				return fmt.Errorf("kubectl %s: did not end within %v\n%s", strings.Join(args, " "), kubectlLimit, out)
			}
			if err != nil {
				return fmt.Errorf("kubectl %s: %w\n%s", strings.Join(args, " "), err, out)
			}
			return nil
		},
	}
}

// renderManifests writes previous.yaml and candidate.yaml into dir, the manifests that a fixture's
// commands apply: the fixture chart rendered by helm template with no namespace, with the release's
// values and its image set to the archive's digest in the run's registry.
func renderManifests(t *testing.T, f fixture, dir string, env []string) {
	t.Helper()
	for _, m := range []struct {
		file, entry string
		values      map[string]any
	}{
		{"previous.yaml", "previous", f.previous},
		{"candidate.yaml", candidateImage(f), f.candidate},
	} {
		b, err := json.Marshal(manifestValues(m.values, m.entry, archiveDigests[m.entry]))
		if err != nil {
			t.Fatalf("%s: the values of %s: %v", f.name, m.file, err)
		}
		valuesFile := filepath.Join(t.TempDir(), "values.json")
		if err := os.WriteFile(valuesFile, b, 0o644); err != nil {
			t.Fatalf("%s: the values of %s: %v", f.name, m.file, err)
		}
		cmd := exec.Command(filepath.Join(root, ".tools", f.helm, "helm"), "template", "orders",
			filepath.Join(root, "fixtures", "chart"), "-f", valuesFile)
		cmd.Dir = root
		cmd.Env = env
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		manifest, err := cmd.Output()
		if err != nil {
			t.Fatalf("%s: helm template for %s: %v\n%s", f.name, m.file, err, stderr.String())
		}
		if err := os.WriteFile(filepath.Join(dir, m.file), manifest, 0o644); err != nil {
			t.Fatalf("%s: %s: %v", f.name, m.file, err)
		}
	}
}

// checkClusterGone fails the test when the engine still holds a container of the cluster the run
// made. It only reads: the harness removes nothing.
func checkClusterGone(t *testing.T, f fixture) {
	t.Helper()
	out, err := exec.Command(engine, "ps", "-a", "--format", "{{.Names}}").CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %s ps -a: %v\n%s", f.name, engine, err, out)
	}
	var left []string
	for _, name := range strings.Fields(string(out)) {
		if strings.HasPrefix(name, "rollproof-e2e-") {
			left = append(left, name)
		}
	}
	if len(left) > 0 {
		t.Errorf("%s: the run left these containers behind: %s", f.name, strings.Join(left, ", "))
	}
}

// checkClusterKept fails the test unless the engine lists the control-plane node, each worker
// node of the fixture's number of workers (3 when it names none) and the registry of the cluster
// the run made. It removes nothing: runFixture has registered removeKeptCluster when the tool
// started.
func checkClusterKept(t *testing.T, f fixture) {
	t.Helper()
	out, err := exec.Command(engine, "ps", "-a", "--format", "{{.Names}}").CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %s ps -a: %v\n%s", f.name, engine, err, out)
	}
	names := strings.Fields(string(out))
	workers := f.workers
	if workers == 0 {
		workers = 3
	}
	wants := []string{"rollproof-e2e-control-plane", "rollproof-e2e-registry", "rollproof-e2e-worker"}
	for i := 2; i <= workers; i++ {
		wants = append(wants, fmt.Sprintf("rollproof-e2e-worker%d", i))
	}
	for _, want := range wants {
		found := false
		for _, name := range names {
			found = found || name == want
		}
		if !found {
			t.Errorf("%s: the run did not leave %s behind", f.name, want)
		}
	}
}

// removeKeptCluster removes, with their volumes, every container whose name begins
// rollproof-e2e-, and then the run folder of the cluster rollproof-e2e. It removes nothing else.
func removeKeptCluster(t *testing.T) {
	out, err := exec.Command(engine, "ps", "-a", "--format", "{{.Names}}").CombinedOutput()
	if err != nil {
		t.Errorf("%s ps -a: %v\n%s", engine, err, out)
		return
	}
	for _, name := range strings.Fields(string(out)) {
		if !strings.HasPrefix(name, "rollproof-e2e-") {
			continue
		}
		if out, err := exec.Command(engine, "rm", "-f", "-v", name).CombinedOutput(); err != nil {
			t.Errorf("%s rm -f -v %s: %v\n%s", engine, name, err, out)
		}
	}
	if err := os.RemoveAll(filepath.Join(os.TempDir(), "rollproof-e2e")); err != nil {
		t.Errorf("the run folder of rollproof-e2e: %v", err)
	}
}

// outDir returns where one test's results stay: under the system's temporary
// folder, named for the test. It is not a t.TempDir(), because the folder stays
// after the test, so that the path printed on a failure still leads to the
// evidence. It is not rollproof-e2e, because that is the run folder of the
// cluster rollproof-e2e, which the tool removes when it starts.
func outDir(t *testing.T) string {
	return filepath.Join(os.TempDir(), "rollproof-e2e-results", strings.ReplaceAll(t.Name(), "/", "-"))
}

// readTransitions reads the key transitions of out/result.json.
func readTransitions(t *testing.T, out string) []transitionResult {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, "result.json"))
	if err != nil {
		t.Fatalf("result.json: %v", err)
	}
	var r struct {
		Transitions []transitionResult `json:"transitions"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("result.json: %v", err)
	}
	return r.Transitions
}

// installResult is result.json's install, the steady window's times, and each reason's detail,
// as far as the fixtures read them.
type installResult struct {
	Install *struct {
		Ready            time.Time      `json:"ready"`
		FirstObservation *time.Time     `json:"first_observation"`
		Settled          *time.Time     `json:"settled"`
		Checks           int            `json:"checks"`
		Failed           int            `json:"failed"`
		FailedByClass    map[string]int `json:"failed_by_class"`
		LastFailure      *time.Time     `json:"last_failure"`
	} `json:"install"`
	Windows []struct {
		Name  string     `json:"name"`
		Start *time.Time `json:"start"`
		End   *time.Time `json:"end"`
	} `json:"windows"`
	Reasons []struct {
		Code   string   `json:"code"`
		Detail []string `json:"detail"`
	} `json:"reasons"`
}

// readInstall reads out/result.json as an installResult, and fails the test when the file has no
// install, or no steady window first in its list.
func readInstall(t *testing.T, out string) installResult {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, "result.json"))
	if err != nil {
		t.Fatalf("result.json: %v", err)
	}
	var r installResult
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("result.json: %v", err)
	}
	if r.Install == nil {
		t.Fatalf("result.json has no install")
	}
	if len(r.Windows) == 0 || r.Windows[0].Name != "steady" {
		t.Fatalf("result.json's windows do not begin with the steady window: %+v", r.Windows)
	}
	return r
}

// record is out/result.json as the step 4 rows read it.
type record struct {
	Driver      string             `json:"driver"`
	Name        string             `json:"name"`
	Reasons     []recordReason     `json:"reasons"`
	Windows     []recordWindow     `json:"windows"`
	Install     *recordInstall     `json:"install"`
	Transitions []recordTransition `json:"transitions"`
	Probes      []struct {
		Kind             string `json:"kind"`
		StoppedWhenAsked *bool  `json:"stopped_when_asked"`
	} `json:"probes"`
	Releases []struct {
		Role    string `json:"role"`
		Program string `json:"program"`
	} `json:"releases"`
	Images []struct {
		Name   string `json:"name"`
		Digest string `json:"digest"`
	} `json:"images"`
	Cluster struct {
		Kubernetes string `json:"kubernetes"`
		NodeImage  string `json:"node_image"`
	} `json:"cluster"`
}

// recordReason is one entry of result.json's reasons, as the step 4 rows read it.
type recordReason struct {
	Code   string   `json:"code"`
	Window string   `json:"window"`
	Text   string   `json:"text"`
	Detail []string `json:"detail"`
}

// recordWindow is one entry of result.json's windows, as the step 4 rows read it.
type recordWindow struct {
	Name              string     `json:"name"`
	Start             *time.Time `json:"start"`
	End               *time.Time `json:"end"`
	Result            string     `json:"result"`
	Failed            int        `json:"failed"`
	FailedAfterFinish int        `json:"failed_after_finish"`
	FirstFailure      *time.Time `json:"first_failure"`
}

// recordInstall is result.json's install, as the step 4 rows read it.
type recordInstall struct {
	Ready             time.Time  `json:"ready"`
	FirstObservation  *time.Time `json:"first_observation"`
	Settled           *time.Time `json:"settled"`
	LastFailure       *time.Time `json:"last_failure"`
	SettleRestartedBy []string   `json:"settle_restarted_by"`
	Checks            int        `json:"checks"`
	Failed            int        `json:"failed"`
}

// recordTransition is one entry of result.json's transitions, as the step 4 rows read it.
type recordTransition struct {
	Name              string           `json:"name"`
	Command           json.RawMessage  `json:"command"`
	Restart           *string          `json:"restart"`
	SettleRestartedBy []string         `json:"settle_restarted_by"`
	Workloads         []recordWorkload `json:"workloads"`
}

// recordWorkload is one workload of a transition in result.json, as the step 4 rows read it.
type recordWorkload struct {
	Workload     string `json:"workload"`
	PodsReplaced int    `json:"pods_replaced"`
	PodsCreated  int    `json:"pods_created"`
	Probed       string `json:"probed"`
}

// readRecord reads out/result.json as the step 4 rows read it, and fails the test when the
// file cannot be read or decoded.
func readRecord(t *testing.T, out string) record {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, "result.json"))
	if err != nil {
		t.Fatalf("result.json: %v", err)
	}
	var r record
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("result.json: %v", err)
	}
	return r
}

// window returns the record's window name, and fails the test when it has none.
func (r record) window(t *testing.T, name string) recordWindow {
	t.Helper()
	for _, w := range r.Windows {
		if w.Name == name {
			return w
		}
	}
	t.Fatalf("result.json has no window %s", name)
	return recordWindow{}
}

// transition returns the record's transition name, and fails the test when it has none.
func (r record) transition(t *testing.T, name string) recordTransition {
	t.Helper()
	for _, tr := range r.Transitions {
		if tr.Name == name {
			return tr
		}
	}
	t.Fatalf("result.json has no transition %s", name)
	return recordTransition{}
}

// workload returns the workload of the record's transition, and fails the test when either is
// missing.
func (r record) workload(t *testing.T, transition, workload string) recordWorkload {
	t.Helper()
	for _, w := range r.transition(t, transition).Workloads {
		if w.Workload == workload {
			return w
		}
	}
	t.Fatalf("result.json's transition %s has no workload %s", transition, workload)
	return recordWorkload{}
}

// readConsole returns the console text of the test's last run: the tool's standard output alone,
// which runFixture keeps in the results folder as console.txt.
func readConsole(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(outDir(t), "console.txt"))
	if err != nil {
		t.Fatalf("console.txt: %v", err)
	}
	return string(b)
}
