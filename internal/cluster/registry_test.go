//go:build linux

package cluster

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// startRegistry's wait for the registry counts limit from its own call: a registry that answers
// nothing is given the whole limit, and no more than one poll beyond it.
func TestStartRegistryCountsItsOwnLimit(t *testing.T) {
	// The fake engine starts no container: it exits 0 for every command, and its inspect names an
	// address for the nodes. The port that startRegistry picked on the runner stays free, so
	// nothing answers there.
	script := filepath.Join(t.TempDir(), "engine")
	body := "#!/bin/sh\n" +
		"if [ \"$1\" = inspect ]; then\n" +
		"  echo '[{\"NetworkSettings\":{\"Networks\":{\"kind\":{\"IPAddress\":\"10.0.0.2\"}}}}]'\n" +
		"fi\n" +
		"exit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}

	const limit = 300 * time.Millisecond
	start := time.Now()
	push, node, err := startRegistry(context.Background(), Engine{Binary: script},
		"rollproof-test-registry", "registry:3", limit)
	took := time.Since(start)
	if err == nil {
		t.Fatalf("startRegistry = %q, %q, nil; want an error", push, node)
	}
	if !strings.Contains(err.Error(), "the registry did not answer") {
		t.Errorf("error %q, want it to say that the registry did not answer", err)
	}
	// The closed port answers every request with a refused connection, so the error takes the
	// branch that names the last answer.
	if !strings.Contains(err.Error(), "with 200 in time; the last answer") {
		t.Errorf("error %q, want the last answer's branch", err)
	}
	if took < limit || took >= limit+registryPoll+time.Second {
		t.Errorf("startRegistry took %v, want at least %v and less than %v", took, limit, limit+registryPoll+time.Second)
	}
}

// The wait for the registry's answer counts limit from the container's start: a slow start does
// not eat into it, so the whole limit is still waited.
func TestStartRegistryCountsItsWaitFromTheContainersStart(t *testing.T) {
	// The fake engine's run returns only after 0.4 s; its inspect names an address for the nodes
	// and says that the image is held, so that no pull happens.
	script, _ := fakeEngine(t, "if [ \"$1\" = run ]; then\n"+
		"  sleep 0.4\n"+
		"fi\n"+
		"if [ \"$1\" = inspect ]; then\n"+
		"  echo '[{\"NetworkSettings\":{\"Networks\":{\"kind\":{\"IPAddress\":\"10.0.0.2\"}}}}]'\n"+
		"fi\n")
	const limit = 300 * time.Millisecond
	start := time.Now()
	push, node, err := startRegistry(context.Background(), Engine{Binary: script},
		"rollproof-test-registry", "registry:3", limit)
	took := time.Since(start)
	if err == nil {
		t.Fatalf("startRegistry = %q, %q, nil; want an error", push, node)
	}
	if !strings.Contains(err.Error(), "the registry did not answer") {
		t.Errorf("error %q, want it to say that the registry did not answer", err)
	}
	if took < 700*time.Millisecond || took >= 400*time.Millisecond+limit+registryPoll+time.Second {
		t.Errorf("startRegistry took %v, want at least 700ms and less than %v",
			took, 400*time.Millisecond+limit+registryPoll+time.Second)
	}
}

// The registry's image is pulled within limit too: a pull that uses the whole limit is cut short,
// and no container starts.
func TestStartRegistryCutsTheImagesPullAtItsLimit(t *testing.T) {
	script, log := fakeEngine(t, "if [ \"$1\" = inspect ]; then\n  exit 1\nfi\n"+
		"if [ \"$1\" = pull ]; then\n  exec sleep 5\nfi\n")
	const limit = 300 * time.Millisecond
	push, node, err := startRegistry(context.Background(), Engine{Binary: script},
		"rollproof-test-registry", "registry:3", limit)
	want := "registry:3 could not be pulled within 300ms"
	if err == nil || err.Error() != want {
		t.Errorf("startRegistry = %q, %q, %v; want the error %q", push, node, err, want)
	}
	for _, call := range engineCalls(t, log) {
		if strings.HasPrefix(call, "run") {
			t.Errorf("the engine ran the registry's container after the pull's limit: %q", call)
		}
	}
}

// The registry wait reports the last answer the registry gave, not the deadline's end of a request
// in flight; it names the deadline only when no answer came at all.
func TestWaitForRegistryReportsTheLastAnswer(t *testing.T) {
	// hangs returns a server that answers its first `answered` requests with 503 and holds every
	// later one until the client gives up.
	hangs := func(t *testing.T, answered int32) string {
		done := make(chan struct{})
		var n atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if n.Add(1) <= answered {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			select {
			case <-r.Context().Done():
			case <-done:
			}
		}))
		t.Cleanup(srv.Close)
		t.Cleanup(func() { close(done) }) // runs before srv.Close
		return srv.URL + "/v2/"
	}
	refused := func(t *testing.T) string {
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL + "/v2/"
		srv.Close()
		return url
	}
	for _, tc := range []struct {
		name string
		url  func(t *testing.T) string
		want string // in the error's text
	}{
		{"a 503, then requests that hang past the deadline", func(t *testing.T) string { return hangs(t, 1) },
			"with 200 in time; the last answer: 503 Service Unavailable"},
		{"connections refused", refused, "connection refused"},
		{"no answer at all", func(t *testing.T) string { return hangs(t, 0) }, "before the deadline"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url := tc.url(t)
			err := waitForRegistry(context.Background(), url, time.Now().Add(700*time.Millisecond))
			if err == nil {
				t.Fatal("waitForRegistry gave no error")
			}
			if !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "context deadline exceeded") {
				t.Errorf("error %q, want it to contain %q and not the deadline's end of a request", err, tc.want)
			}
		})
	}
}
