//go:build linux

package cluster

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"testing"

	"sigs.k8s.io/kind/pkg/apis/config/defaults"

	"github.com/nzneit/rollproof/internal/verdict"
)

// Recorded on 2026-11-16: `nerdctl info --format "{{json .}}"` on the developer's machine (rootless
// nerdctl 2.4.2), and the form Docker gives on a host where it runs as root. Fields that the tool
// does not read are left out.
const (
	nerdctlInfo = `{"SecurityOptions":["name=seccomp,profile=builtin","name=cgroupns","name=rootless"]}`
	dockerInfo  = `{"SecurityOptions":["name=apparmor","name=seccomp,profile=builtin","name=cgroupns"]}`
)

// The first line that `NAME -v` prints for each engine.
const (
	dockerV  = "Docker version 28.5.1, build e180ab8"
	nerdctlV = "nerdctl version 2.4.2"
	podmanV  = "podman version 5.6.2"
)

// machine is what the fake probes of CheckRunner report.
type machine struct {
	env      map[string]string
	files    []string          // paths that exist
	versions map[string]string // engine binary -> what `BINARY -v` prints; absent means it fails
	infos    map[string]string // engine binary -> output of its info command; absent means it fails
	static   bool
}

// fake installs m in the package's probe variables until the test ends.
func fake(t *testing.T, m machine) {
	t.Helper()
	oldEnv, oldExists, oldVersion, oldInfo, oldExe, oldStatic := lookupEnv, fileExists, engineVersion, engineInfo, executablePath, binaryIsStatic
	t.Cleanup(func() {
		lookupEnv, fileExists, engineVersion, engineInfo, executablePath, binaryIsStatic = oldEnv, oldExists, oldVersion, oldInfo, oldExe, oldStatic
	})
	lookupEnv = func(name string) string { return m.env[name] }
	fileExists = func(path string) bool {
		for _, f := range m.files {
			if f == path {
				return true
			}
		}
		return false
	}
	engineVersion = func(_ context.Context, binary string) ([]byte, error) {
		out, ok := m.versions[binary]
		if !ok {
			return nil, errors.New("executable file not found in $PATH")
		}
		return []byte(out + "\n"), nil
	}
	engineInfo = func(_ context.Context, binary string) ([]byte, error) {
		out, ok := m.infos[binary]
		if !ok {
			return nil, errors.New("exit status 1")
		}
		return []byte(out), nil
	}
	executablePath = func() (string, error) { return "/usr/local/bin/rollproof", nil }
	binaryIsStatic = func(string) (bool, error) { return m.static, nil }
}

func TestCheckRunner(t *testing.T) {
	provider := func(name string) map[string]string { return map[string]string{"KIND_EXPERIMENTAL_PROVIDER": name} }
	for _, tc := range []struct {
		name string
		m    machine
		want Engine
		text string // the RunnerUnsupported text; empty when CheckRunner succeeds
	}{
		{"nerdctl, rootless", machine{versions: map[string]string{"nerdctl": nerdctlV},
			infos: map[string]string{"nerdctl": nerdctlInfo}, static: true},
			Engine{Binary: "nerdctl", Rootless: true}, ""},
		{"docker before nerdctl", machine{versions: map[string]string{"docker": dockerV, "nerdctl": nerdctlV},
			infos: map[string]string{"docker": dockerInfo, "nerdctl": nerdctlInfo}, static: true},
			Engine{Binary: "docker", Rootless: false}, ""},
		{"podman last", machine{versions: map[string]string{"podman": podmanV},
			infos: map[string]string{"podman": `{}`}, static: true},
			Engine{Binary: "podman"}, ""},
		{"a docker command that is podman does not count as docker", machine{
			versions: map[string]string{"docker": podmanV, "podman": podmanV},
			infos:    map[string]string{"docker": `{}`, "podman": `{}`}, static: true},
			Engine{Binary: "podman"}, ""},
		{"the first engine found has no working info", machine{versions: map[string]string{"docker": dockerV, "nerdctl": nerdctlV},
			infos: map[string]string{"nerdctl": nerdctlInfo}, static: true},
			Engine{}, "no container engine that kind supports"},
		{"the variable picks podman", machine{env: provider("podman"), versions: map[string]string{"docker": dockerV},
			infos: map[string]string{"docker": dockerInfo, "podman": `{}`}, static: true},
			Engine{Binary: "podman"}, ""},
		{"the variable picks finch", machine{env: provider("finch"),
			infos: map[string]string{"finch": nerdctlInfo}, static: true},
			Engine{Binary: "finch", Rootless: true}, ""},
		{"the variable picks nerdctl.lima", machine{env: provider("nerdctl.lima"),
			infos: map[string]string{"nerdctl.lima": nerdctlInfo}, static: true},
			Engine{Binary: "nerdctl.lima", Rootless: true}, ""},
		{"the variable picks nerdctl and its info fails", machine{env: provider("nerdctl"),
			versions: map[string]string{"docker": dockerV}, infos: map[string]string{"docker": dockerInfo}, static: true},
			Engine{}, "no container engine that kind supports"},
		{"an unknown value is ignored", machine{env: provider("something"), versions: map[string]string{"nerdctl": nerdctlV},
			infos: map[string]string{"nerdctl": nerdctlInfo}, static: true},
			Engine{Binary: "nerdctl", Rootless: true}, ""},
		{"no engine", machine{static: true},
			Engine{}, "no container engine that kind supports"},
		{"info that is not JSON counts as failed", machine{versions: map[string]string{"docker": dockerV},
			infos: map[string]string{"docker": "Client: Docker Engine"}, static: true},
			Engine{}, "no container engine that kind supports"},
		{"DOCKER_HOST over tcp", machine{env: map[string]string{"DOCKER_HOST": "tcp://10.0.0.1:2375"},
			versions: map[string]string{"docker": dockerV}, infos: map[string]string{"docker": dockerInfo}, static: true},
			Engine{}, "the container engine is on another machine"},
		{"CONTAINER_HOST over ssh", machine{env: map[string]string{"CONTAINER_HOST": "ssh://user@build.example/run/podman/podman.sock"},
			versions: map[string]string{"podman": podmanV}, infos: map[string]string{"podman": `{}`}, static: true},
			Engine{}, "the container engine is on another machine"},
		{"DOCKER_HOST on a local socket", machine{env: map[string]string{"DOCKER_HOST": "unix:///run/user/1000/docker.sock"},
			versions: map[string]string{"docker": dockerV}, infos: map[string]string{"docker": dockerInfo}, static: true},
			Engine{Binary: "docker"}, ""},
		{"inside Docker", machine{files: []string{"/.dockerenv"},
			versions: map[string]string{"docker": dockerV}, infos: map[string]string{"docker": dockerInfo}, static: true},
			Engine{}, "the tool runs inside a container"},
		{"inside Podman", machine{files: []string{"/run/.containerenv"},
			versions: map[string]string{"podman": podmanV}, infos: map[string]string{"podman": `{}`}, static: true},
			Engine{}, "the tool runs inside a container"},
		{"a binary that is not static", machine{versions: map[string]string{"docker": dockerV},
			infos: map[string]string{"docker": dockerInfo}, static: false},
			Engine{}, "the tool's binary is not statically linked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake(t, tc.m)
			got, err := CheckRunner(context.Background())
			if tc.text == "" {
				if err != nil {
					t.Fatalf("CheckRunner: %v", err)
				}
				if got != tc.want {
					t.Errorf("engine = %+v, want %+v", got, tc.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("CheckRunner gave %+v and no error, want %q", got, tc.text)
			}
			if c := verdict.AsCondition(err); c.Code != verdict.RunnerUnsupported || c.Text != tc.text {
				t.Errorf("error = %v, want runner-unsupported: %s", err, tc.text)
			}
		})
	}
}

func TestNetwork(t *testing.T) {
	both := map[string]string{"KIND_EXPERIMENTAL_DOCKER_NETWORK": "docker-net", "KIND_EXPERIMENTAL_PODMAN_NETWORK": "podman-net"}
	for _, tc := range []struct {
		binary string
		env    map[string]string
		want   string
	}{
		{"docker", both, "docker-net"},
		{"podman", both, "podman-net"},
		{"nerdctl", both, "kind"},
	} {
		t.Run(tc.binary, func(t *testing.T) {
			fake(t, machine{env: tc.env})
			e := Engine{Binary: tc.binary}
			if got := e.network(); got != tc.want {
				t.Errorf("network() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRootlessFromInfo(t *testing.T) {
	for _, tc := range []struct {
		name, info string
		want       bool
	}{
		{"nerdctl rootless", nerdctlInfo, true},
		{"docker as root", dockerInfo, false},
		{"docker rootless", `{"SecurityOptions":["name=seccomp,profile=builtin","name=rootless","name=cgroupns"]}`, true},
		{"podman rootless", `{"host":{"security":{"rootless":true,"seccompEnabled":true}}}`, true},
		{"no security options", `{}`, false},
	} {
		got, err := rootlessFromInfo([]byte(tc.info))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: rootless = %v, want %v", tc.name, got, tc.want)
		}
	}
	if _, err := rootlessFromInfo([]byte("Client: Docker Engine")); err == nil {
		t.Error("no error for output that is not JSON")
	}
}

// build compiles a one-file main package with CGO_ENABLED set to cgo and returns the binary's path.
func build(t *testing.T, src, cgo string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module probe\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "probe")
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED="+cgo)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, b)
	}
	return out
}

func TestIsStatic(t *testing.T) {
	static := build(t, "package main\n\nfunc main() {}\n", "0")
	if ok, err := isStatic(static); err != nil || !ok {
		t.Errorf("isStatic(static) = %v, %v; want true", ok, err)
	}

	cc, err := exec.Command("go", "env", "CC").Output()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath(strings.TrimSpace(string(cc))); err != nil {
		t.Skip("no C compiler: the dynamically linked case is not tested")
	}
	dynamic := build(t, "package main\n\nimport \"net\"\n\nfunc main() { _, _ = net.LookupHost(\"localhost\") }\n", "1")
	if ok, err := isStatic(dynamic); err != nil || ok {
		t.Errorf("isStatic(dynamic) = %v, %v; want false", ok, err)
	}

	text := filepath.Join(t.TempDir(), "not-elf")
	if err := os.WriteFile(text, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := isStatic(text); err == nil {
		t.Error("isStatic of a file that is not ELF gave no error")
	}
}

func TestDefaults(t *testing.T) {
	if got := DefaultNodeImage(); got != defaults.Image || !strings.Contains(got, "@sha256:") {
		t.Errorf("DefaultNodeImage = %q, want kind's %q", got, defaults.Image)
	}
	if !regexp.MustCompile(`^docker\.io/library/registry:3@sha256:[0-9a-f]{64}$`).MatchString(DefaultRegistryImage) {
		t.Errorf("DefaultRegistryImage = %q, not pinned by digest", DefaultRegistryImage)
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Skip("no build information in this binary")
	}
	for _, d := range info.Deps {
		if d.Path == "sigs.k8s.io/kind" {
			if got := KindVersion(); got != d.Version || got == "" {
				t.Errorf("KindVersion = %q, want %q", got, d.Version)
			}
			return
		}
	}
	t.Skip("sigs.k8s.io/kind is not listed in this binary's build information")
}
