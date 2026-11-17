//go:build linux

package cluster

import (
	"bytes"
	"context"
	"debug/elf"
	"encoding/json"
	"os"
	"os/exec"
	"strings"

	"rollproof/internal/verdict"
)

// Engine is the container engine the run uses: its command name, as kind's library would pick
// it, and whether it runs without root.
type Engine struct {
	Binary   string
	Rootless bool
}

// The probes of CheckRunner. A test replaces them.
var (
	lookupEnv  = os.Getenv
	fileExists = func(path string) bool { _, err := os.Stat(path); return err == nil }
	// engineVersion runs BINARY -v and returns its standard output; an error when it does not
	// exit 0.
	engineVersion = func(ctx context.Context, binary string) ([]byte, error) {
		return exec.CommandContext(ctx, binary, "-v").Output()
	}
	// engineInfo runs BINARY info --format "{{json .}}" and returns its standard output.
	engineInfo = func(ctx context.Context, binary string) ([]byte, error) {
		return exec.CommandContext(ctx, binary, "info", "--format", "{{json .}}").Output()
	}
	executablePath = os.Executable
	binaryIsStatic = isStatic
)

// CheckRunner checks the runner before anything is created: the engine, that it is on this
// machine, that the tool is not inside a container, and that the tool's own binary is static. An
// error is a Condition with RunnerUnsupported that names what is missing.
func CheckRunner(ctx context.Context) (Engine, error) {
	unsupported := func(text string) (Engine, error) {
		return Engine{}, verdict.Condition{Code: verdict.RunnerUnsupported, Text: text}
	}

	binary := pickEngine(ctx)
	if binary == "" {
		return unsupported("no container engine that kind supports")
	}
	info, err := engineInfo(ctx, binary)
	if err != nil {
		return unsupported("no container engine that kind supports")
	}
	rootless, err := rootlessFromInfo(info)
	if err != nil {
		return unsupported("no container engine that kind supports")
	}

	for _, v := range []string{"DOCKER_HOST", "CONTAINER_HOST"} {
		if h := lookupEnv(v); strings.HasPrefix(h, "tcp://") || strings.HasPrefix(h, "ssh://") {
			return unsupported("the container engine is on another machine")
		}
	}

	if fileExists("/.dockerenv") || fileExists("/run/.containerenv") {
		return unsupported("the tool runs inside a container")
	}

	exe, err := executablePath()
	if err != nil {
		return unsupported("the tool's binary cannot be read: " + err.Error())
	}
	static, err := binaryIsStatic(exe)
	if err != nil {
		return unsupported("the tool's binary cannot be read: " + err.Error())
	}
	if !static {
		return unsupported("the tool's binary is not statically linked")
	}
	return Engine{Binary: binary, Rootless: rootless}, nil
}

// pickEngine returns the engine's command name as kind chooses it, or "" when there is none.
// KIND_EXPERIMENTAL_PROVIDER names it when it holds a value that kind's own command line takes
// (pkg/internal/runtime/runtime.go at v0.33.0). Otherwise it is the first of docker, nerdctl and
// podman whose -v succeeds and whose first line starts with the engine's banner: the test of
// IsAvailable in kind's three providers, in the order of cluster.DetectNodeProvider.
func pickEngine(ctx context.Context) string {
	switch p := lookupEnv("KIND_EXPERIMENTAL_PROVIDER"); p {
	case "docker", "podman", "nerdctl", "finch", "nerdctl.lima":
		return p
	}
	for _, e := range []struct{ binary, banner string }{
		{"docker", "Docker version"},
		{"nerdctl", "nerdctl version"},
		{"podman", "podman version"},
	} {
		out, err := engineVersion(ctx, e.binary)
		if err != nil {
			continue
		}
		first, _, _ := bytes.Cut(out, []byte("\n"))
		if strings.HasPrefix(string(first), e.banner) {
			return e.binary
		}
	}
	return ""
}

// rootlessFromInfo reads the output of the engine's info command: true when an entry of
// SecurityOptions contains rootless (the form of Docker and nerdctl), or when host.security.rootless
// is true (the form of Podman). An error when the text is not JSON.
func rootlessFromInfo(info []byte) (bool, error) {
	var v struct {
		SecurityOptions []string
		Host            struct {
			Security struct {
				Rootless bool `json:"rootless"`
			} `json:"security"`
		} `json:"host"`
	}
	if err := json.Unmarshal(info, &v); err != nil {
		return false, err
	}
	for _, o := range v.SecurityOptions {
		if strings.Contains(o, "rootless") {
			return true, nil
		}
	}
	return v.Host.Security.Rootless, nil
}

// isStatic reports whether the ELF file at path is statically linked: false when a program header
// names an interpreter (PT_INTERP). An error when the file is not ELF.
func isStatic(path string) (bool, error) {
	f, err := elf.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			return false, nil
		}
	}
	return true, nil
}
