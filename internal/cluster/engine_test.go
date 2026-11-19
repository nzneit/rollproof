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

// hasCall reports whether calls holds want, a whole line.
func hasCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}
