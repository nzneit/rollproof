//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"text/template"
	"time"
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

// TestMain picks the engine, checks the Helm binaries, builds the binary and
// the two fixture images once, and then runs the tests. A failure prints the
// command and its output to standard error and exits 1.
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
	checkHelmMajor("helm-3", "v3.")
	checkHelmMajor("helm-4", "v4.")
	goBuild("build", "-trimpath", "-ldflags", "-s -w -X main.version=dev", "-o", filepath.Join("bin", "rollproof"), "./cmd/rollproof")
	for _, version := range []string{"previous", "candidate"} {
		buildFixture(version)
	}
	os.Exit(m.Run())
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

// goBuild runs go with args in root, with CGO_ENABLED=0 in its environment.
func goBuild(args ...string) {
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: go %s: %v\n%s", strings.Join(args, " "), err, out)
		os.Exit(1)
	}
}

// engineBuild builds the image tag from the folder dir.
func engineBuild(tag, dir string) {
	args := []string{"build", "-t", tag, dir}
	cmd := exec.Command(engine, args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: %s %s: %v\n%s", engine, strings.Join(args, " "), err, out)
		os.Exit(1)
	}
}

// buildFixture builds the fixture service as the image
// rollproof-fixture:version.
func buildFixture(version string) {
	dir, err := os.MkdirTemp("", "rollproof-fixture-"+version+"-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: a temporary folder for the %s fixture: %v\n", version, err)
		os.Exit(1)
	}
	goBuild("build", "-trimpath", "-ldflags", "-s -w -X main.version="+version, "-o", filepath.Join(dir, "svc"), "./fixtures/service")
	dockerfile, err := os.ReadFile(filepath.Join(root, "fixtures", "service", "Dockerfile"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: fixtures/service/Dockerfile: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), dockerfile, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: %s/Dockerfile: %v\n", dir, err)
		os.Exit(1)
	}
	engineBuild("rollproof-fixture:"+version, dir)
}

// configTemplate is the run description of one fixture, for text/template.
// Workers is f.workers, or 3 when it is 0. Chart is the absolute path of
// fixtures/chart as a JSON string. Previous and Candidate are "" when the
// fixture gives that release no values, else ", " and the values as a JSON
// object. Limits is "" when the fixture changes no limit, else "limits: " and
// f.limits as a JSON object.
const configTemplate = `version: 1
cluster: { workers: {{.Workers}} }
images:
  previous:  { local: "rollproof-fixture:previous" }
  candidate: { local: "rollproof-fixture:candidate" }
deploy:
  namespace: orders
  helm:
    release: orders
    previous:  { chart: {{.Chart}}, values: [ { image: { repository: "${images.previous.repository}", digest: "${images.previous.digest}" } }{{.Previous}} ] }
    candidate: { chart: {{.Chart}}, values: [ { image: { repository: "${images.candidate.repository}", digest: "${images.candidate.digest}" } }{{.Candidate}} ] }
probe:
  http: { url: "http://orders.orders.svc/work?ms=20" }
{{.Limits}}
`

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

// fixture is one row of section 12: the chart values of each release, the
// cluster's size, the limits that differ from the defaults, and which Helm
// binary.
type fixture struct {
	name                string
	previous, candidate map[string]any
	workers             int
	limits              map[string]string
	helm                string
}

// result is what a fixture run gave: the exit code, the verdict, the set of
// reason codes, the deciding code, and each window's result by name.
type result struct {
	exit    int
	verdict string
	codes   []string
	decided string
	windows map[string]string
}

// runFixture runs the real binary once on one fixture, with one Helm binary
// first on the PATH, and returns what result.json says and the exit code. It
// builds the binary and the two fixture images once for the whole test run.
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

	workers := f.workers
	if workers == 0 {
		workers = 3
	}
	var previous, candidate, limits string
	if len(f.previous) > 0 {
		previous = ", " + jsonText(t, f.previous)
	}
	if len(f.candidate) > 0 {
		candidate = ", " + jsonText(t, f.candidate)
	}
	if len(f.limits) > 0 {
		limits = "limits: " + jsonText(t, f.limits)
	}
	var description bytes.Buffer
	err := template.Must(template.New("rollproof.yaml").Parse(configTemplate)).Execute(&description, struct {
		Workers   int
		Chart     string
		Previous  string
		Candidate string
		Limits    string
	}{
		Workers:   workers,
		Chart:     jsonText(t, filepath.Join(root, "fixtures", "chart")),
		Previous:  previous,
		Candidate: candidate,
		Limits:    limits,
	})
	if err != nil {
		t.Fatalf("%s: the run description: %v", f.name, err)
	}
	file := filepath.Join(t.TempDir(), "rollproof.yaml")
	if err := os.WriteFile(file, description.Bytes(), 0o644); err != nil {
		t.Fatalf("%s: the run description: %v", f.name, err)
	}

	helmDir := t.TempDir()
	if err := os.Symlink(filepath.Join(root, ".tools", f.helm, "helm"), filepath.Join(helmDir, "helm")); err != nil {
		t.Fatalf("%s: the Helm link: %v", f.name, err)
	}
	env := helmEnv(helmDir)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(root, "bin", "rollproof"),
		"run", "-f", file, "--out", out, "--name", "e2e")
	cmd.Dir = root
	cmd.Env = env
	var log bytes.Buffer
	cmd.Stdout = &log
	cmd.Stderr = &log
	runErr := cmd.Run()
	t.Logf("%s (%s):\n%s", f.name, f.helm, log.String())
	if ctx.Err() != nil {
		t.Fatalf("%s (%s): the run did not end within 15m", f.name, f.helm)
	}

	r := result{}
	switch e := runErr.(type) {
	case nil:
	case *exec.ExitError:
		r.exit = e.ExitCode()
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
	checkClusterGone(t, f)
	return r
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

// outDir returns where one test's results stay: under the system's temporary
// folder, named for the test. It is not a t.TempDir(), because the folder stays
// after the test, so that the path printed on a failure still leads to the
// evidence. It is not rollproof-e2e, because that is the run folder of the
// cluster rollproof-e2e, which the tool removes when it starts.
func outDir(t *testing.T) string {
	return filepath.Join(os.TempDir(), "rollproof-e2e-results", strings.ReplaceAll(t.Name(), "/", "-"))
}

// jsonText returns v as JSON text: a path as a quoted string, and a fixture's
// values as an object.
func jsonText(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("the fixture's values: %v", err)
	}
	return string(b)
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
