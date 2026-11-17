//go:build linux

package cluster

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

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
