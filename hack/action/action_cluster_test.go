//go:build cluster

package action

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"rollproof/internal/cluster"
	"rollproof/internal/report"
)

// The action's script on this machine's engine: the tool's image built from the Dockerfile, pushed
// by digest to a registry on the loopback, pulled, taken apart and started by action.sh, which runs
// the tool on a configuration that it rejects at once. No cluster is made.
func TestTheScriptRunsTheToolFromItsImage(t *testing.T) {
	// ENGINE: KIND_EXPERIMENTAL_PROVIDER when it is set, else the first of docker, nerdctl and
	// podman whose `info` exits 0; none fails the test.
	engine := testEngine(t)
	do := func(args ...string) error {
		out, err := exec.Command(engine, args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s %s: %w\n%s", engine, strings.Join(args, " "), err, out)
		}
		return nil
	}
	// remove runs one removal of the clean-up: a failure is a test error, unless the engine says
	// that the name does not exist, which is a name the run never made.
	remove := func(args ...string) {
		out, err := exec.Command(engine, args...).CombinedOutput()
		if err != nil && !isMissing(string(out)) {
			t.Errorf("%s %s: %v\n%s", engine, strings.Join(args, " "), err, out)
		}
	}

	// 1. go build (CGO_ENABLED=0, -trimpath, -ldflags "-s -w -X main.version=action-test") of
	//    ../../cmd/rollproof into a temporary folder CTX as rollproof; ../../Dockerfile copied into
	//    CTX.
	ctxDir := t.TempDir()
	build := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w -X main.version=action-test",
		"-o", filepath.Join(ctxDir, "rollproof"), "../../cmd/rollproof")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	dockerfile, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ctxDir, "Dockerfile"), dockerfile, 0o644); err != nil {
		t.Fatal(err)
	}

	// 2. ENGINE build -t rollproof-action-test:dev CTX.
	if err := do("build", "-t", "rollproof-action-test:dev", ctxDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { remove("rmi", "rollproof-action-test:dev") })

	// 3. ENGINE save -o IMAGE.tar rollproof-action-test:dev, the archive in another temporary
	//    folder; tarball.ImageFromPath gives the image and its digest D.
	archive := filepath.Join(t.TempDir(), "image.tar")
	if err := do("save", "-o", archive, "rollproof-action-test:dev"); err != nil {
		t.Fatal(err)
	}
	img, err := tarball.ImageFromPath(archive, nil)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}

	// 4. ENGINE run -d --name rollproof-action-test-registry -p 127.0.0.1:PORT:5000
	//    cluster.DefaultRegistryImage, PORT a free port (net.Listen on 127.0.0.1:0, then closed);
	//    GET http://127.0.0.1:PORT/v2/ answers 200 within 30 s; remote.Write of the image to
	//    127.0.0.1:PORT/rollproof-action-test@D with name.Insecure.
	port := freePort(t)
	registry := "127.0.0.1:" + port
	t.Cleanup(func() { remove("rm", "-f", "-v", "rollproof-action-test-registry") })
	if err := do("run", "-d", "--name", "rollproof-action-test-registry", "-p", registry+":5000",
		cluster.DefaultRegistryImage); err != nil {
		t.Fatal(err)
	}
	waitForRegistry(t, registry, 30*time.Second)
	pushed := registry + "/rollproof-action-test@" + digest.String()
	ref, err := name.NewDigest(pushed, name.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(ref, img); err != nil {
		t.Fatal(err)
	}

	// 5. bash with the absolute path of ../../action.sh, its working folder a temporary one, under
	//    a limit of 5 minutes, with this process's environment without its GITHUB_ variables, and
	//    GITHUB_ACTIONS=true, GITHUB_OUTPUT and GITHUB_STEP_SUMMARY naming two empty files of the
	//    test, GITHUB_WORKSPACE a folder WS that holds rejected.yaml ("version: 99\n"), RUNNER_TEMP
	//    a folder RT,
	//    ROLLPROOF_ACTION_CONFIG=rejected.yaml, ROLLPROOF_ACTION_IMAGE=127.0.0.1:PORT/rollproof-action-test@D,
	//    ROLLPROOF_ACTION_OUT=results and KIND_EXPERIMENTAL_PROVIDER=ENGINE.
	script, err := filepath.Abs("../../action.sh")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	work := filepath.Join(root, "work")
	ws := filepath.Join(root, "workspace")
	rt := filepath.Join(root, "runner-temp")
	output := filepath.Join(root, "github-output")
	summary := filepath.Join(root, "github-summary")
	for _, d := range []string{work, ws, rt} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{output, summary} {
		if err := os.WriteFile(f, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(ws, "rejected.yaml"), []byte("version: 99\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var env []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GITHUB_") {
			env = append(env, entry)
		}
	}
	// os/exec keeps the last value of a name that stands twice, so these win over the process's.
	env = append(env,
		"GITHUB_ACTIONS=true",
		"GITHUB_OUTPUT="+output,
		"GITHUB_STEP_SUMMARY="+summary,
		"GITHUB_WORKSPACE="+ws,
		"RUNNER_TEMP="+rt,
		"ROLLPROOF_ACTION_CONFIG=rejected.yaml",
		"ROLLPROOF_ACTION_IMAGE="+pushed,
		"ROLLPROOF_ACTION_OUT=results",
		"KIND_EXPERIMENTAL_PROVIDER="+engine,
	)
	// The engine holds the image by this reference once the script has pulled it.
	t.Cleanup(func() { remove("rmi", pushed) })
	// A script cut short between its create and its rm leaves its container, named as its folder
	// under RT, rollproof-action- and six characters. Clean-ups run last first, so these go before
	// the image they were made from; only a name of that form is passed to the engine.
	t.Cleanup(func() {
		entries, err := os.ReadDir(rt)
		if err != nil {
			t.Error(err)
			return
		}
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), "rollproof-action-") {
				remove("rm", "-f", e.Name())
			}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", script)
	cmd.Dir = work
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	t.Logf("action.sh's standard output:\n%s\nits standard error:\n%s", stdout.String(), stderr.String())

	// It exits 2.
	if cmd.ProcessState == nil {
		t.Fatalf("action.sh did not start: %v", err)
	}
	if code := cmd.ProcessState.ExitCode(); code != 2 {
		t.Fatalf("action.sh exited %d (%v), want 2", code, err)
	}
	// stdout's first line is matched by scriptStopLine, its second by toolStopLine, and its last
	// is "::" + T + "::"; stderr's first line is stdout's first; the line of stderr after the one
	// that begins "rollproof: the tool from " is stdout's second, and it is stderr's last; no line
	// of either stream is "::" + U + "::".
	outLines, errLines := textLines(stdout.String()), textLines(stderr.String())
	if len(outLines) < 3 || !scriptStopLine.MatchString(outLines[0]) || !toolStopLine.MatchString(outLines[1]) {
		t.Fatalf("stdout's first line is not the script's stop line, or its second not the tool's:\n%s", stdout.String())
	}
	scriptToken := strings.TrimPrefix(outLines[0], "::stop-commands::")
	toolToken := strings.TrimPrefix(outLines[1], "::stop-commands::")
	if got, want := outLines[len(outLines)-1], "::"+toolToken+"::"; got != want {
		t.Errorf("stdout's last line is %q, want %q", got, want)
	}
	if len(errLines) == 0 || errLines[0] != outLines[0] {
		t.Errorf("stderr's first line is not stdout's %q:\n%s", outLines[0], stderr.String())
	}
	toolFrom := -1
	for i, l := range errLines {
		if strings.HasPrefix(l, "rollproof: the tool from ") {
			toolFrom = i
			break
		}
	}
	if toolFrom < 0 || toolFrom+1 != len(errLines)-1 || errLines[toolFrom+1] != outLines[1] {
		t.Errorf("stderr does not end with the line %q and then stdout's second, %q:\n%s",
			"rollproof: the tool from …", outLines[1], stderr.String())
	}
	for name, lines := range map[string][]string{"stdout": outLines, "stderr": errLines} {
		for _, l := range lines {
			if l == "::"+scriptToken+"::" {
				t.Errorf("%s holds the line %q, which resumes the script's commands", name, l)
			}
		}
	}
	// WS/results/result.json is final, its verdict not-verified, its one reason config-rejected.
	results := filepath.Join(ws, "results")
	b, err := os.ReadFile(filepath.Join(results, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r report.Run
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	if !r.Final || r.Verdict != "not-verified" {
		t.Errorf("result.json: final %v, verdict %q; want true, not-verified", r.Final, r.Verdict)
	}
	if len(r.Reasons) != 1 || r.Reasons[0].Code != "config-rejected" {
		t.Errorf("result.json's reasons %+v, want one, config-rejected", r.Reasons)
	}
	// The outputs are exactly {"out": [WS/results], "verdict": ["not-verified",
	// "not-verified"]}.
	outs, err := readGitHubOutputs(output)
	if err != nil {
		t.Fatal(err)
	}
	wantOuts := map[string][]string{"out": {results}, "verdict": {"not-verified", "not-verified"}}
	if diff := cmp.Diff(wantOuts, outs); diff != "" {
		t.Errorf("outputs (-want +got):\n%s", diff)
	}
	// The summary begins "### rollproof: NOT VERIFIED\n" and holds "[config-rejected]".
	s, err := os.ReadFile(summary)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(s), "### rollproof: NOT VERIFIED\n") || !strings.Contains(string(s), "[config-rejected]") {
		t.Errorf("the summary does not begin %q and hold %q:\n%s", "### rollproof: NOT VERIFIED\n", "[config-rejected]", s)
	}
	// RT holds one folder whose name begins "rollproof-action-", and its rollproof, run with the
	// argument version, prints a first line "rollproof action-test".
	entries, err := os.ReadDir(rt)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !entries[0].IsDir() || !strings.HasPrefix(entries[0].Name(), "rollproof-action-") {
		t.Fatalf("RT holds %v, want one folder whose name begins rollproof-action-", entries)
	}
	v, err := exec.Command(filepath.Join(rt, entries[0].Name(), "rollproof"), "version").Output()
	if err != nil {
		t.Fatal(err)
	}
	if first, _, _ := strings.Cut(string(v), "\n"); first != "rollproof action-test" {
		t.Errorf("rollproof version's first line %q, want %q", first, "rollproof action-test")
	}
	// Cleanup, each a test error when it fails: ENGINE rm -f -v rollproof-action-test-registry;
	// ENGINE rmi rollproof-action-test:dev; ENGINE rmi 127.0.0.1:PORT/rollproof-action-test@D (the
	// one name that does not begin rollproof-, Global Constraints).
}

// testEngine returns the engine's command name: KIND_EXPERIMENTAL_PROVIDER when it is set, else the
// first of docker, nerdctl and podman whose info exits 0. It fails the test when there is none.
func testEngine(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("KIND_EXPERIMENTAL_PROVIDER"); p != "" {
		return p
	}
	for _, e := range []string{"docker", "nerdctl", "podman"} {
		if exec.Command(e, "info").Run() == nil {
			return e
		}
	}
	t.Fatal("no container engine: KIND_EXPERIMENTAL_PROVIDER is not set, and none of docker, nerdctl and podman answers info")
	return ""
}

// isMissing says whether an engine's output for a failed removal says only that the name does
// not exist: nerdctl writes "no such container: NAME" and "no such image: NAME" (read on nerdctl
// 2.4.2); Docker "No such container" and "No such image", and Podman "no such container" and
// "image not known", are from memory, not read. Any other output is a removal that failed.
func isMissing(out string) bool {
	out = strings.ToLower(out)
	return strings.Contains(out, "no such container") || strings.Contains(out, "no such image") ||
		strings.Contains(out, "image not known")
}

// freePort returns a port of 127.0.0.1 that was free a moment ago.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

// waitForRegistry fails the test unless GET http://REGISTRY/v2/ answers 200 within limit.
func waitForRegistry(t *testing.T, registry string, limit time.Duration) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(limit)
	for {
		resp, err := client.Get("http://" + registry + "/v2/")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			err = fmt.Errorf("status %s", resp.Status)
		}
		if time.Now().After(deadline) {
			t.Fatalf("the registry at %s did not answer 200 within %s: %v", registry, limit, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// readGitHubOutputs reads an outputs file in the delimiter form and in the plain
// form NAME=VALUE, and gives each name's values in the order they were written. A
// line that is neither, or a delimiter that never comes, is an error that names
// the file and the line.
func readGitHubOutputs(path string) (map[string][]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	outs := map[string][]string{}
	if len(b) == 0 {
		return outs, nil
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if name, delimiter, ok := strings.Cut(line, "<<"); ok && name != "" && delimiter != "" {
			start := i + 1
			var value []string
			closed := false
			for i++; i < len(lines); i++ {
				if lines[i] == delimiter {
					closed = true
					break
				}
				value = append(value, lines[i])
			}
			if !closed {
				return nil, fmt.Errorf("%s line %d: the output %s has no closing delimiter %q", path, start, name, delimiter)
			}
			outs[name] = append(outs[name], strings.Join(value, "\n"))
			continue
		}
		if name, value, ok := strings.Cut(line, "="); ok && name != "" {
			outs[name] = append(outs[name], value)
			continue
		}
		return nil, fmt.Errorf("%s line %d: %q is neither NAME<<DELIMITER nor NAME=VALUE", path, i+1, line)
	}
	return outs, nil
}
