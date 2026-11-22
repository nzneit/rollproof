package action

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.yaml.in/yaml/v3"
)

// scriptRun is one run of action.sh in a world of fakes: a folder that is the whole PATH, holding
// links to the commands the script and the fakes need (uname, mktemp, tar, env, cat) and the fake
// engines the test writes; the script's environment beside PATH; the fakes' log, one command line
// a line ("ENGINE ARGS…"); and what the fake tool writes when it is started: its arguments, one a
// line, its environment and its working folder. GITHUB_OUTPUT names an empty file of the run's,
// which the script must leave empty, and RUNNER_TEMP and GITHUB_WORKSPACE folders of its own.
type scriptRun struct {
	t       *testing.T
	bash    string // bash's absolute path, found before the run's PATH holds only the fakes
	script  string // the absolute path of ../../action.sh
	root    string
	path    string // the folder that is the whole PATH
	work    string // the script's working folder
	log     string
	output  string // GITHUB_OUTPUT
	temp    string // RUNNER_TEMP
	space   string // GITHUB_WORKSPACE
	args    string // FAKE_ARGS
	toolEnv string // FAKE_TOOL_ENV
	toolDir string // FAKE_TOOL_DIR
	env     map[string]string
}

func newScriptRun(t *testing.T) *scriptRun {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("../../action.sh")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	r := &scriptRun{
		t:       t,
		bash:    bash,
		script:  script,
		root:    root,
		path:    filepath.Join(root, "path"),
		work:    filepath.Join(root, "work"),
		log:     filepath.Join(root, "log"),
		output:  filepath.Join(root, "github-output"),
		temp:    filepath.Join(root, "runner-temp"),
		space:   filepath.Join(root, "workspace"),
		args:    filepath.Join(root, "fake-args"),
		toolEnv: filepath.Join(root, "fake-tool-env"),
		toolDir: filepath.Join(root, "fake-tool-dir"),
	}
	for _, d := range []string{r.path, r.work, r.temp, r.space} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(r.output, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"uname", "mktemp", "tar", "env", "cat"} {
		p, err := exec.LookPath(c)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(p, filepath.Join(r.path, c)); err != nil {
			t.Fatal(err)
		}
	}
	r.env = map[string]string{
		"PATH":             r.path,
		"GITHUB_OUTPUT":    r.output,
		"RUNNER_TEMP":      r.temp,
		"GITHUB_WORKSPACE": r.space,
		"FAKE_ARGS":        r.args,
		"FAKE_TOOL_ENV":    r.toolEnv,
		"FAKE_TOOL_DIR":    r.toolDir,
	}
	return r
}

// command writes a /bin/sh script of that name, with that body after its first line, into the
// PATH folder, in place of a link of the same name.
func (r *scriptRun) command(name, body string) {
	r.t.Helper()
	p := filepath.Join(r.path, name)
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		r.t.Fatal(err)
	}
}

// engine writes a fake engine of that name into the PATH folder: -v prints banner and exits with
// version; pull, create and export exit with their codes; create prints fake-id; export prints the
// tar it is given; rm exits 0. Each call appends its command line to the log.
func (r *scriptRun) engine(name string, b engineBehaviour) {
	r.t.Helper()
	tarFile := filepath.Join(r.root, name+".tar")
	if err := os.WriteFile(tarFile, b.tar, 0o644); err != nil {
		r.t.Fatal(err)
	}
	r.command(name, fmt.Sprintf(`printf '%%s\n' "%s $*" >> %s
case "$1" in
  -v) printf '%%s\n' %s; exit %d ;;
  pull) exit %d ;;
  create) printf '%%s\n' fake-id; exit %d ;;
  export) cat %s; exit %d ;;
  rm) exit 0 ;;
esac
exit 99
`, name, shQuote(r.log), shQuote(b.banner), b.version, b.pull, b.create, shQuote(tarFile), b.export))
}

type engineBehaviour struct {
	banner                        string
	version, pull, create, export int // exit codes
	tar                           []byte
}

// shQuote quotes s for /bin/sh, in single quotes.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// fakeTool is the binary that the fake engines' image holds; see toolTar.
var fakeTool = []byte(`#!/bin/sh
for a in "$@"; do printf '%s\n' "$a"; done > "$FAKE_ARGS"
env > "$FAKE_TOOL_ENV"
pwd > "$FAKE_TOOL_DIR"
exit 7
`)

// toolTar is a tar of one file, name, with mode and content; fakeTool is a /bin/sh script that
// writes its arguments to FAKE_ARGS, its environment to FAKE_TOOL_ENV and its working folder to
// FAKE_TOOL_DIR, and exits 7.
func toolTar(t *testing.T, name string, mode int64, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// environ is the run's environment as a list of KEY=VALUE.
func (r *scriptRun) environ() []string {
	var env []string
	for k, v := range r.env {
		env = append(env, k+"="+v)
	}
	return env
}

// start runs bash with args in the run's working folder and environment.
func (r *scriptRun) start(args ...string) (stdout string, code int, stderr string) {
	r.t.Helper()
	cmd := exec.Command(r.bash, args...)
	cmd.Dir = r.work
	cmd.Env = r.environ()
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	default:
		r.t.Fatal(err)
	}
	return out.String(), code, errOut.String()
}

// run runs bash with the absolute path of ../../action.sh, with the run's environment, its working
// folder a folder of its own, and gives the exit code and standard error. source runs
// `source PATH; FUNCTION`, PATH being that absolute path, the same way and gives standard output,
// the exit code and standard error. The path is absolute because the working folder is not the
// package's.
func (r *scriptRun) run() (code int, stderr string) {
	r.t.Helper()
	_, code, stderr = r.start(r.script)
	return code, stderr
}

func (r *scriptRun) source(function string) (stdout string, code int, stderr string) {
	r.t.Helper()
	return r.start("-c", `source "$1"; `+function, "bash", r.script)
}

// lines reads a file of the run as its lines, without their line ends; nil for a file that is
// empty or missing.
func (r *scriptRun) lines(path string) []string {
	r.t.Helper()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		r.t.Fatal(err)
	}
	s := strings.TrimSuffix(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// outputIsEmpty fails the test when GITHUB_OUTPUT is not empty.
func (r *scriptRun) outputIsEmpty() {
	r.t.Helper()
	b, err := os.ReadFile(r.output)
	if err != nil {
		r.t.Fatal(err)
	}
	if len(b) != 0 {
		r.t.Errorf("GITHUB_OUTPUT holds %q, want it empty", b)
	}
}

// lastLine is the last line of s, without its line end.
func lastLine(s string) string {
	s = strings.TrimSuffix(s, "\n")
	return s[strings.LastIndex(s, "\n")+1:]
}

const dockerBanner = "Docker version 28.0.4, build b8034c0"

var ref = "127.0.0.1:5000/rollproof@sha256:" + strings.Repeat("a", 64)

// startingRun is a run in which the script starts the fake tool: a fake docker whose export gives
// toolTar("rollproof", 0o755, fakeTool), and ROLLPROOF_ACTION_IMAGE set to ref.
func startingRun(t *testing.T) *scriptRun {
	t.Helper()
	r := newScriptRun(t)
	r.engine("docker", engineBehaviour{banner: dockerBanner, tar: toolTar(t, "rollproof", 0o755, fakeTool)})
	r.env["ROLLPROOF_ACTION_IMAGE"] = ref
	return r
}

// binaryFolder is the base name of the one folder that the script made for the binary under
// RUNNER_TEMP, which begins "rollproof-action-"; it fails the test when there is not exactly one.
func (r *scriptRun) binaryFolder() string {
	r.t.Helper()
	entries, err := os.ReadDir(r.temp)
	if err != nil {
		r.t.Fatal(err)
	}
	if len(entries) != 1 || !entries[0].IsDir() || !strings.HasPrefix(entries[0].Name(), "rollproof-action-") {
		r.t.Fatalf("RUNNER_TEMP holds %v, want one folder rollproof-action-…", entries)
	}
	return entries[0].Name()
}

// createLine is the log's line for the script's create: the container carries the name of the
// folder made for the binary.
func createLine(folder string) string {
	return "docker create --name " + folder + " " + ref + " /rollproof"
}

func TestTheScriptStartsTheToolInItsPlace(t *testing.T) {
	r := startingRun(t)
	r.env["ROLLPROOF_ACTION_CONFIG"] = "projects/rollproof/hack/action/testdata/pass.yaml"
	r.env["ROLLPROOF_ACTION_OUT"] = "/tmp/rollproof-action-test/results"

	code, stderr := r.run()
	if code != 7 {
		t.Fatalf("exit code %d, want 7, the fake tool's; stderr:\n%s", code, stderr)
	}
	wantArgs := []string{"run", "-f", "projects/rollproof/hack/action/testdata/pass.yaml",
		"--out", "/tmp/rollproof-action-test/results"}
	if diff := cmp.Diff(wantArgs, r.lines(r.args)); diff != "" {
		t.Errorf("the tool's arguments (-want +got):\n%s", diff)
	}
	env := r.lines(r.toolEnv)
	found := false
	for _, l := range env {
		if l == "KIND_EXPERIMENTAL_PROVIDER=docker" {
			found = true
		}
		if strings.HasPrefix(l, "ROLLPROOF_ACTION_") {
			t.Errorf("the tool's environment holds %q", l)
		}
	}
	if !found {
		t.Errorf("the tool's environment holds no KIND_EXPERIMENTAL_PROVIDER=docker:\n%s", strings.Join(env, "\n"))
	}
	if diff := cmp.Diff([]string{r.space}, r.lines(r.toolDir)); diff != "" {
		t.Errorf("the tool's working folder (-want +got):\n%s", diff)
	}
	folder := r.binaryFolder()
	wantLog := []string{"docker -v", "docker pull " + ref, createLine(folder),
		"docker export fake-id", "docker rm fake-id"}
	if diff := cmp.Diff(wantLog, r.lines(r.log)); diff != "" {
		t.Errorf("the engine's log (-want +got):\n%s", diff)
	}
	fi, err := os.Stat(filepath.Join(r.temp, folder, "rollproof"))
	if err != nil {
		t.Error(err)
	} else if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("the binary's mode is %v, want an executable file", fi.Mode())
	}
	r.outputIsEmpty()
	if got, want := lastLine(stderr), "rollproof: the tool from "+ref+", through docker"; got != want {
		t.Errorf("stderr's last line is %q, want %q", got, want)
	}
}

func TestTheScriptStartsTheToolWithoutOut(t *testing.T) {
	r := startingRun(t)
	r.env["ROLLPROOF_ACTION_CONFIG"] = ""
	r.env["ROLLPROOF_ACTION_OUT"] = ""

	code, stderr := r.run()
	if code != 7 {
		t.Fatalf("exit code %d, want 7, the fake tool's; stderr:\n%s", code, stderr)
	}
	if diff := cmp.Diff([]string{"run", "-f", "rollproof.yaml"}, r.lines(r.args)); diff != "" {
		t.Errorf("the tool's arguments (-want +got):\n%s", diff)
	}
}

func TestTheScriptsEngineRule(t *testing.T) {
	nerdctl := engineBehaviour{banner: "nerdctl version 2.4.2"}
	for _, tc := range []struct {
		name     string
		provider string // KIND_EXPERIMENTAL_PROVIDER; unset when empty
		engines  map[string]engineBehaviour
		want     string
		code     int
		noLog    bool
	}{
		{name: "the variable names nerdctl", provider: "nerdctl",
			engines: map[string]engineBehaviour{"docker": {banner: dockerBanner}, "nerdctl": nerdctl},
			want:    "nerdctl\n", noLog: true},
		{name: "the variable names finch", provider: "finch", want: "finch\n"},
		{name: "the variable names nerdctl.lima", provider: "nerdctl.lima", want: "nerdctl.lima\n"},
		{name: "the variable names no engine kind takes", provider: "kind",
			engines: map[string]engineBehaviour{"docker": {banner: dockerBanner}}, want: "docker\n"},
		{name: "docker's -v fails",
			engines: map[string]engineBehaviour{"docker": {banner: "Docker version 28.0.4", version: 1}, "nerdctl": nerdctl},
			want:    "nerdctl\n"},
		{name: "docker's banner is another",
			engines: map[string]engineBehaviour{"docker": {banner: "Docker Desktop 4.40"}, "nerdctl": nerdctl},
			want:    "nerdctl\n"},
		{name: "docker's -v prints a second line",
			engines: map[string]engineBehaviour{"docker": {banner: "Docker version 28.0.4\nanother line"}},
			want:    "docker\n"},
		{name: "docker comes before nerdctl",
			engines: map[string]engineBehaviour{"docker": {banner: dockerBanner}, "nerdctl": nerdctl},
			want:    "docker\n"},
		{name: "docker's banner is later in its first line",
			engines: map[string]engineBehaviour{"docker": {banner: "Client: Docker version 28.0.4"}, "nerdctl": nerdctl},
			want:    "nerdctl\n"},
		{name: "only podman",
			engines: map[string]engineBehaviour{"podman": {banner: "podman version 4.9.3"}}, want: "podman\n"},
		{name: "no engine", want: "", code: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newScriptRun(t)
			if tc.provider != "" {
				r.env["KIND_EXPERIMENTAL_PROVIDER"] = tc.provider
			}
			for name, b := range tc.engines {
				r.engine(name, b)
			}
			stdout, code, stderr := r.source("pick_engine")
			if stdout != tc.want || code != tc.code {
				t.Errorf("pick_engine printed %q and exited %d, want %q and %d; stderr:\n%s", stdout, code, tc.want, tc.code, stderr)
			}
			if tc.noLog {
				if log := r.lines(r.log); len(log) != 0 {
					t.Errorf("the engines' log holds %q, want it empty", log)
				}
			}
		})
	}
}

func TestTheScriptsImageReference(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		image, repository, digest string
		stdout, stderr            string
		code                      int
	}{
		{name: "an image with a digest", image: "r.example/rollproof@sha256:aaa",
			stdout: "r.example/rollproof@sha256:aaa\n"},
		{name: "an image and the release's digest", image: "r.example/rollproof", digest: "sha256:bbb",
			stdout: "r.example/rollproof@sha256:bbb\n"},
		{name: "an image with a tag and the release's digest", image: "r.example/rollproof:v1", digest: "sha256:bbb",
			stdout: "r.example/rollproof:v1@sha256:bbb\n"},
		{name: "the release's repository and digest", repository: "ghcr.io/o/rollproof", digest: "sha256:bbb",
			stdout: "ghcr.io/o/rollproof@sha256:bbb\n"},
		{name: "nothing set", code: 2,
			stderr: "rollproof: no image to pull: before its first release this action carries no repository and no digest, so give the input image a reference with @sha256:\n"},
		{name: "an image and no digest", image: "r.example/rollproof", code: 2,
			stderr: "rollproof: the image r.example/rollproof carries no digest, and before its first release this action carries none: give the input image a reference with @sha256:\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newScriptRun(t)
			r.env["ROLLPROOF_ACTION_IMAGE"] = tc.image
			r.env["ROLLPROOF_ACTION_REPOSITORY"] = tc.repository
			r.env["ROLLPROOF_ACTION_DIGEST"] = tc.digest
			stdout, code, stderr := r.source("image_ref")
			if stdout != tc.stdout || code != tc.code || stderr != tc.stderr {
				t.Errorf("image_ref gave stdout %q, exit %d, stderr %q; want %q, %d, %q",
					stdout, code, stderr, tc.stdout, tc.code, tc.stderr)
			}
		})
	}
}

func TestTheScriptFailsWithExit2AndWritesNoOutput(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(t *testing.T, r *scriptRun)
		text    func(r *scriptRun) string
		log     []string // the log exactly, when exact
		logEnds string   // the log's last line, when not empty
		exact   bool
		// creates says that the script reaches the engine's create: the row then finds the one
		// folder rollproof-action-… under RUNNER_TEMP, which the failure leaves, and wants the
		// log's create line built from its base name, compared whole. A row that fails before
		// create (no folder may exist, as in "no folder for the binary") wants no create line.
		creates bool
	}{
		{name: "not Linux",
			change: func(t *testing.T, r *scriptRun) { r.command("uname", "printf '%s\\n' Darwin\n") },
			text: func(*scriptRun) string {
				return "rollproof: the runner is not Linux (uname -s printed Darwin); the action runs only on Linux"
			},
			exact: true},
		{name: "a uname that fails",
			change: func(t *testing.T, r *scriptRun) { r.command("uname", "exit 1\n") },
			text: func(*scriptRun) string {
				return "rollproof: uname -s failed, so the runner's system is not known; the action runs only on Linux"
			},
			exact: true},
		{name: "no engine",
			change: func(t *testing.T, r *scriptRun) {
				if err := os.Remove(filepath.Join(r.path, "docker")); err != nil {
					t.Fatal(err)
				}
			},
			text: func(*scriptRun) string {
				return "rollproof: no container engine that kind supports: KIND_EXPERIMENTAL_PROVIDER names none, and none of docker, nerdctl and podman answers -v"
			}},
		{name: "no image",
			change: func(t *testing.T, r *scriptRun) {
				r.env["ROLLPROOF_ACTION_IMAGE"] = ""
				r.env["ROLLPROOF_ACTION_REPOSITORY"] = ""
				r.env["ROLLPROOF_ACTION_DIGEST"] = ""
			},
			text: func(*scriptRun) string {
				return "rollproof: no image to pull: before its first release this action carries no repository and no digest, so give the input image a reference with @sha256:"
			},
			log: []string{"docker -v"}, exact: true},
		{name: "no digest",
			change: func(t *testing.T, r *scriptRun) {
				r.env["ROLLPROOF_ACTION_IMAGE"] = "r.example/rollproof"
				r.env["ROLLPROOF_ACTION_DIGEST"] = ""
			},
			text: func(*scriptRun) string {
				return "rollproof: the image r.example/rollproof carries no digest, and before its first release this action carries none: give the input image a reference with @sha256:"
			}},
		{name: "a pull that fails",
			change: func(t *testing.T, r *scriptRun) {
				r.engine("docker", engineBehaviour{banner: dockerBanner, pull: 1, tar: toolTar(t, "rollproof", 0o755, fakeTool)})
			},
			text: func(*scriptRun) string { return "rollproof: docker could not pull " + ref }},
		{name: "no folder for the binary",
			change:  func(t *testing.T, r *scriptRun) { r.command("mktemp", "exit 1\n") },
			text:    func(r *scriptRun) string { return "rollproof: no folder for the binary under " + r.temp },
			logEnds: "docker pull " + ref},
		{name: "a create that fails",
			change: func(t *testing.T, r *scriptRun) {
				r.engine("docker", engineBehaviour{banner: dockerBanner, create: 125, tar: toolTar(t, "rollproof", 0o755, fakeTool)})
			},
			text:    func(*scriptRun) string { return "rollproof: docker could not create a container from " + ref },
			creates: true},
		{name: "an export that fails",
			change: func(t *testing.T, r *scriptRun) {
				r.engine("docker", engineBehaviour{banner: dockerBanner, export: 1, tar: toolTar(t, "rollproof", 0o755, fakeTool)})
			},
			text:    func(*scriptRun) string { return "rollproof: the binary /rollproof could not be taken out of " + ref },
			logEnds: "docker rm fake-id", creates: true},
		{name: "an image without the binary",
			change: func(t *testing.T, r *scriptRun) {
				r.engine("docker", engineBehaviour{banner: dockerBanner, tar: toolTar(t, "other", 0o755, fakeTool)})
			},
			text:    func(*scriptRun) string { return "rollproof: the binary /rollproof could not be taken out of " + ref },
			logEnds: "docker rm fake-id", creates: true},
		{name: "a workspace that cannot be entered",
			change: func(t *testing.T, r *scriptRun) {
				r.env["GITHUB_WORKSPACE"] = filepath.Join(r.root, "no-such-workspace")
			},
			text: func(r *scriptRun) string {
				return "rollproof: the workspace " + r.env["GITHUB_WORKSPACE"] + " cannot be entered"
			},
			logEnds: "docker rm fake-id", creates: true},
		// A file that is missing or not executable never reaches the exec: take_binary stops it. A
		// file of the wrong format does, and execve refuses it. Under execfail and the script's
		// "set +e" the line after the exec then fails as the other failures do; without "set +e",
		// bash 5.3.20 ends the script itself at the failed exec with its own code 126 (tried on
		// 2026-11-22), so this row fails too when the line is taken out (decision 17).
		{name: "a binary that cannot start",
			change: func(t *testing.T, r *scriptRun) {
				content := append([]byte("\x7fELF\x02\x01\x01"), make([]byte, 57)...)
				r.engine("docker", engineBehaviour{banner: dockerBanner, tar: toolTar(t, "rollproof", 0o755, content)})
			},
			text:    func(*scriptRun) string { return "rollproof: the binary taken out of " + ref + " could not be started" },
			creates: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := startingRun(t)
			tc.change(t, r)
			code, stderr := r.run()
			if code != 2 {
				t.Errorf("exit code %d, want 2; stderr:\n%s", code, stderr)
			}
			if got, want := lastLine(stderr), tc.text(r); got != want {
				t.Errorf("stderr's last line is %q, want %q; stderr:\n%s", got, want, stderr)
			}
			r.outputIsEmpty()
			log := r.lines(r.log)
			if tc.exact {
				if diff := cmp.Diff(tc.log, log); diff != "" {
					t.Errorf("the engine's log (-want +got):\n%s", diff)
				}
			}
			if tc.logEnds != "" && (len(log) == 0 || log[len(log)-1] != tc.logEnds) {
				t.Errorf("the engine's log is %q, want it to end with %q", log, tc.logEnds)
			}
			var creates []string
			for _, l := range log {
				if strings.HasPrefix(l, "docker create") {
					creates = append(creates, l)
				}
			}
			var wantCreates []string
			if tc.creates {
				wantCreates = []string{createLine(r.binaryFolder())}
			}
			if diff := cmp.Diff(wantCreates, creates); diff != "" {
				t.Errorf("the log's create lines (-want +got):\n%s", diff)
			}
		})
	}
}

func TestActionYAML(t *testing.T) {
	b, err := os.ReadFile("../../action.yml")
	if err != nil {
		t.Fatal(err)
	}
	var a struct {
		Inputs map[string]struct {
			Default *string `yaml:"default"`
		} `yaml:"inputs"`
		Outputs map[string]struct {
			Value string `yaml:"value"`
		} `yaml:"outputs"`
		Runs struct {
			Using string           `yaml:"using"`
			Steps []map[string]any `yaml:"steps"`
		} `yaml:"runs"`
	}
	if err := yaml.Unmarshal(b, &a); err != nil {
		t.Fatal(err)
	}

	inputs := map[string]string{}
	for name, in := range a.Inputs {
		if in.Default == nil {
			t.Errorf("the input %s has no default", name)
			continue
		}
		inputs[name] = *in.Default
	}
	wantInputs := map[string]string{"config": "rollproof.yaml", "image": "", "out": ""}
	if diff := cmp.Diff(wantInputs, inputs); diff != "" {
		t.Errorf("the inputs and their defaults (-want +got):\n%s", diff)
	}

	outputs := map[string]string{}
	for name, out := range a.Outputs {
		outputs[name] = out.Value
	}
	wantOutputs := map[string]string{
		"verdict": "${{ steps.run.outputs.verdict }}",
		"out":     "${{ steps.run.outputs.out }}",
	}
	if diff := cmp.Diff(wantOutputs, outputs); diff != "" {
		t.Errorf("the outputs and their values (-want +got):\n%s", diff)
	}

	if a.Runs.Using != "composite" {
		t.Errorf("runs.using is %q, want composite", a.Runs.Using)
	}
	if len(a.Runs.Steps) != 1 {
		t.Fatalf("the action has %d steps, want 1", len(a.Runs.Steps))
	}
	step := a.Runs.Steps[0]
	if step["id"] != "run" {
		t.Errorf("the step's id is %v, want run", step["id"])
	}
	if step["shell"] != "bash" {
		t.Errorf("the step's shell is %v, want bash", step["shell"])
	}
	if uses, ok := step["uses"]; ok {
		t.Errorf("the step has uses %v, want none", uses)
	}
	run, _ := step["run"].(string)
	if got, want := strings.TrimSpace(run), `exec bash "$GITHUB_ACTION_PATH/action.sh"`; got != want {
		t.Errorf("the step's run is %q, want %q", got, want)
	}
	env := map[string]any{}
	if e, ok := step["env"].(map[string]any); ok {
		env = e
	}
	wantEnv := map[string]any{
		"ROLLPROOF_ACTION_CONFIG":     "${{ inputs.config }}",
		"ROLLPROOF_ACTION_IMAGE":      "${{ inputs.image }}",
		"ROLLPROOF_ACTION_OUT":        "${{ inputs.out }}",
		"ROLLPROOF_ACTION_VERSION":    "",
		"ROLLPROOF_ACTION_REPOSITORY": "",
		"ROLLPROOF_ACTION_DIGEST":     "",
	}
	if diff := cmp.Diff(wantEnv, env); diff != "" {
		t.Errorf("the step's env (-want +got):\n%s", diff)
	}
}

func TestTheDockerfile(t *testing.T) {
	b, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, l := range strings.Split(string(b), "\n") {
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		got = append(got, l)
	}
	want := []string{"FROM scratch", "COPY rollproof /rollproof", "USER 65532:65532", `ENTRYPOINT ["/rollproof"]`}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the Dockerfile's instructions (-want +got):\n%s", diff)
	}
}
