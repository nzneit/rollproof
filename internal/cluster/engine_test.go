//go:build linux

package cluster

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeEngine writes the script of a fake engine, which appends its arguments to the log beside it,
// on one line per call, then runs body, then exits 0. It returns the script's path and the log's.
func fakeEngine(t *testing.T, body string) (script, log string) {
	t.Helper()
	dir := t.TempDir()
	script, log = filepath.Join(dir, "engine"), filepath.Join(dir, "log")
	text := "#!/bin/sh\n" +
		"echo \"$*\" >> \"" + log + "\"\n" +
		body +
		"exit 0\n"
	if err := os.WriteFile(script, []byte(text), 0o700); err != nil {
		t.Fatal(err)
	}
	return script, log
}

// fakeEngineOnPath writes into a folder of its own an executable file named name, which appends its
// arguments to a log in that folder and answers an image inspect with one digest, and puts that
// folder first on the PATH, so that a test never reaches a real engine of that name, also on a
// runner that has one. It returns the log's path.
func fakeEngineOnPath(t *testing.T, name string) (log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "log")
	text := strings.ReplaceAll(`#!/bin/sh
printf '%s\n' "$*" >> 'LOG'
case "$1 $2" in
"image inspect") printf '%s\n' '["docker.io/kindest/node@sha256:abc"]' ;;
esac
exit 0
`, "LOG", log)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// engineCalls returns the lines of a fake engine's log.
func engineCalls(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// pullIfMissing asks the engine the question kind's own providers ask, and pulls nothing when the
// answer is that the store holds the image.
func TestPullIfMissingKeepsAHeldImage(t *testing.T) {
	const image = "docker.io/library/registry:3@sha256:0"
	script, log := fakeEngine(t, "exit 0\n")
	if err := (Engine{Binary: script}).pullIfMissing(context.Background(), image, time.Minute); err != nil {
		t.Fatalf("pullIfMissing: %v", err)
	}
	calls := engineCalls(t, log)
	if want := "inspect --type=image " + image; !hasCall(calls, want) {
		t.Errorf("the engine's calls %q, want them to hold %q", calls, want)
	}
	for _, call := range calls {
		if strings.HasPrefix(call, "pull") {
			t.Errorf("the engine pulled an image that its store holds: %q", call)
		}
	}
}

// pullIfMissing pulls an image that the engine's store lacks.
func TestPullIfMissingPullsAMissingImage(t *testing.T) {
	const image = "docker.io/library/registry:3@sha256:0"
	script, log := fakeEngine(t, "if [ \"$1\" = inspect ]; then\n  exit 1\nfi\n")
	if err := (Engine{Binary: script}).pullIfMissing(context.Background(), image, time.Minute); err != nil {
		t.Fatalf("pullIfMissing: %v", err)
	}
	calls := engineCalls(t, log)
	if want := "pull " + image; calls[len(calls)-1] != want {
		t.Errorf("the engine's last call %q, want %q; calls %q", calls[len(calls)-1], want, calls)
	}
	if want := "inspect --type=image " + image; !hasCall(calls[:len(calls)-1], want) {
		t.Errorf("the engine's calls %q, want the call %q before the pull", calls, want)
	}
}

// pullIfMissing cuts a pull at its own limit, and says so with the image and the limit.
func TestPullIfMissingCutsAPullAtItsLimit(t *testing.T) {
	const image = "docker.io/kindest/node:v0@sha256:0"
	script, _ := fakeEngine(t, "if [ \"$1\" = inspect ]; then\n  exit 1\nfi\n"+
		"if [ \"$1\" = pull ]; then\n  exec sleep 5\nfi\n")
	const limit = 300 * time.Millisecond
	start := time.Now()
	err := (Engine{Binary: script}).pullIfMissing(context.Background(), image, limit)
	took := time.Since(start)
	want := "docker.io/kindest/node:v0@sha256:0 could not be pulled within 300ms"
	if err == nil || err.Error() != want {
		t.Errorf("pullIfMissing = %v, want the error %q", err, want)
	}
	if took >= 2*time.Second {
		t.Errorf("pullIfMissing took %v, want less than 2s", took)
	}
}

// pullIfMissing gives the engine's own error when the pull fails for another reason.
func TestPullIfMissingGivesTheEnginesError(t *testing.T) {
	const image = "docker.io/kindest/node:v0@sha256:0"
	script, _ := fakeEngine(t, "if [ \"$1\" = inspect ]; then\n  exit 1\nfi\n"+
		"if [ \"$1\" = pull ]; then\n  echo 'no such host'\n  exit 1\nfi\n")
	err := (Engine{Binary: script}).pullIfMissing(context.Background(), image, time.Minute)
	if err == nil {
		t.Fatal("pullIfMissing gave no error")
	}
	for _, want := range []string{"pull " + image, "no such host"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q, want it to hold %q", err, want)
		}
	}
}

// logs gives the container's output, standard error included, and found.
func TestEngineLogsGivesTheOutput(t *testing.T) {
	script, log := fakeEngine(t, "if [ \"$1\" = logs ]; then\n  echo 'registry says'\nfi\n")
	got, found, err := (Engine{Binary: script}).logs(context.Background(), "rollproof-x-registry")
	if err != nil || !found || got != "registry says" {
		t.Errorf("logs = %q, %v, %v; want registry says, true, nil", got, found, err)
	}
	if calls := engineCalls(t, log); !hasCall(calls, "logs rollproof-x-registry") {
		t.Errorf("the engine's calls %q, want them to hold logs rollproof-x-registry", calls)
	}
}

// logs with no container to read is found false and no error, when ctx has not ended.
func TestEngineLogsWithNoContainer(t *testing.T) {
	script, log := fakeEngine(t, "if [ \"$1\" = logs ]; then\n  exit 1\nfi\n"+
		"if [ \"$1\" = inspect ]; then\n  exit 1\nfi\n")
	got, found, err := (Engine{Binary: script}).logs(context.Background(), "rollproof-x-registry")
	if err != nil || found || got != "" {
		t.Errorf("logs = %q, %v, %v; want no output, false, nil", got, found, err)
	}
	if calls := engineCalls(t, log); !hasCall(calls, "inspect rollproof-x-registry") {
		t.Errorf("the engine's calls %q, want them to hold inspect rollproof-x-registry", calls)
	}
}

// logs gives the engine's own error when the container is there but its log cannot be read.
func TestEngineLogsGivesTheEnginesErrorWhenTheContainerExists(t *testing.T) {
	script, _ := fakeEngine(t, "if [ \"$1\" = logs ]; then\n  echo 'no such log' >&2\n  exit 1\nfi\n")
	got, found, err := (Engine{Binary: script}).logs(context.Background(), "rollproof-x-registry")
	if err == nil || !found || got != "no such log" {
		t.Fatalf("logs = %q, %v, %v; want no such log, true, an error", got, found, err)
	}
	if !strings.Contains(err.Error(), "logs rollproof-x-registry") {
		t.Errorf("error %q, want it to name the failed call", err)
	}
}

// nodeImageName asks Podman for the name kind's Podman provider uses, and every other engine for
// the name as it is written.
func TestNodeImageNameUnderEachEngine(t *testing.T) {
	const image = "kindest/node:v1.37.0"
	for _, c := range []struct{ binary, want string }{
		{"podman", "docker.io/kindest/node:v1.37.0"},
		{"docker", image},
		{"nerdctl", image},
		{"finch", image},
		{"nerdctl.lima", image},
	} {
		if got := (Engine{Binary: c.binary}).nodeImageName(image); got != c.want {
			t.Errorf("Engine{Binary: %q}.nodeImageName(%q) = %q, want %q", c.binary, image, got, c.want)
		}
	}
}

// nodeImage asks the engine for the digest by nodeImageName, and returns the name as it was written
// with the digest after it.
func TestNodeImageAsksTheEngineByItsName(t *testing.T) {
	const image = "kindest/node:v1.37.0"
	const withDigest = image + "@sha256:abc"
	for _, c := range []struct {
		binary string
		asked  string // what nodeImageName gives, and so what the engine is asked for
	}{
		{"podman", "docker.io/kindest/node:v1.37.0"},
		{"nerdctl", "kindest/node:v1.37.0"},
	} {
		log := fakeEngineOnPath(t, c.binary)
		got, err := nodeImage(context.Background(), Engine{Binary: c.binary}, image)
		if err != nil {
			t.Fatalf("nodeImage(%s): %v", c.binary, err)
		}
		if got != withDigest {
			t.Errorf("nodeImage(%s) = %q, want %q", c.binary, got, withDigest)
		}
		want := "image inspect --format {{json .RepoDigests}} " + c.asked
		calls := engineCalls(t, log)
		if len(calls) != 1 || calls[0] != want {
			t.Errorf("%s was asked %q, want exactly [%q]", c.binary, calls, want)
		}
	}

	// A name with a digest needs no look-up: it is returned as it is, and the engine is not asked.
	const named = "kindest/node:v1.34.11@sha256:44e222ee2132dab25ff87301682f89eb82c7880ea3a1bf543bfe9708fd08d67d"
	for _, binary := range []string{"podman", "nerdctl"} {
		log := fakeEngineOnPath(t, binary)
		got, err := nodeImage(context.Background(), Engine{Binary: binary}, named)
		if err != nil {
			t.Fatalf("nodeImage(%s): %v", binary, err)
		}
		if got != named {
			t.Errorf("nodeImage(%s) = %q, want %q", binary, got, named)
		}
		if b, err := os.ReadFile(log); err == nil && len(b) != 0 {
			t.Errorf("%s was asked %q, want nothing for a name with a digest", binary, b)
		}
	}
}

// hasCall reports whether calls holds want, a whole line.
func hasCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}
