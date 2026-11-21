package main

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

func TestHandlerStripsThePrefix(t *testing.T) {
	var reached []string
	reg := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = append(reached, r.URL.Path)
		fmt.Fprint(w, r.URL.Path)
	})
	h := newHandler(reg, []string{"/docker-io", "/real"}, "u", "p", io.Discard)

	for _, c := range []struct {
		path, want string
		status     int
	}{
		{"/docker-io/v2/x/manifests/d", "/v2/x/manifests/d", 200},
		{"/real/v2/", "/v2/", 200},
		{"/other/v2/", "", 404},
		{"/really/v2/", "", 404},
		{"/v2/", "", 404},
	} {
		reached = nil
		req := httptest.NewRequest("GET", c.path, nil)
		req.SetBasicAuth("u", "p")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.status {
			t.Errorf("GET %s: status %d, want %d", c.path, rec.Code, c.status)
		}
		if c.status != 200 {
			if len(reached) != 0 {
				t.Errorf("GET %s reached reg as %q, want it not to", c.path, reached)
			}
			continue
		}
		if len(reached) != 1 {
			t.Fatalf("GET %s reached reg %d times, want 1", c.path, len(reached))
		}
		if reached[0] != c.want {
			t.Errorf("GET %s reached reg as %q, want %q", c.path, reached[0], c.want)
		}
		if body := rec.Body.String(); body != c.want {
			t.Errorf("GET %s answered %q, want %q", c.path, body, c.want)
		}
	}

	reached = nil
	plain := newHandler(reg, nil, "u", "p", io.Discard)
	req := httptest.NewRequest("GET", "/v2/", nil)
	req.SetBasicAuth("u", "p")
	rec := httptest.NewRecorder()
	plain.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("with no prefixes, GET /v2/: status %d, want 200", rec.Code)
	}
	if len(reached) != 1 || reached[0] != "/v2/" {
		t.Errorf("with no prefixes, GET /v2/ reached reg as %q, want [\"/v2/\"]", reached)
	}
}

func TestHandlerAsksForALogin(t *testing.T) {
	called := false
	reg := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })

	cases := []struct {
		name string
		auth func(*http.Request)
	}{
		{"none", func(*http.Request) {}},
		{"wrong password", func(r *http.Request) { r.SetBasicAuth("u", "wrong") }},
	}
	h := newHandler(reg, []string{"/docker-io"}, "u", "p", io.Discard)
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/docker-io/v2/", nil)
		c.auth(req)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", c.name, rec.Code)
		}
		if got := rec.Header().Get("WWW-Authenticate"); got != `Basic realm="rollproof-mirrorjob"` {
			t.Errorf("%s: WWW-Authenticate %q, want %q", c.name, got, `Basic realm="rollproof-mirrorjob"`)
		}
	}
	if called {
		t.Error("reg was called for a request without valid credentials")
	}
}

func TestHandlerLogsEachRequest(t *testing.T) {
	reg := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	var log bytes.Buffer
	h := newHandler(reg, []string{"/docker-io"}, "u", "p", &log)

	first := httptest.NewRequest("HEAD", "/docker-io/v2/x/manifests/d?ns=docker.io", nil)
	first.SetBasicAuth("u", "p")
	h.ServeHTTP(httptest.NewRecorder(), first)
	second := httptest.NewRequest("GET", "/real/v2/", nil)
	h.ServeHTTP(httptest.NewRecorder(), second)

	want := "REQ HEAD /docker-io/v2/x/manifests/d user=u status=200\n" +
		"REQ GET /real/v2/ user=- status=401\n"
	if got := log.String(); got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

func TestLogConnections(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	l := logConnections(ln, &log)
	accepted := make(chan net.Conn, 2)
	go func() {
		for range 2 {
			c, err := l.Accept()
			if err != nil {
				return
			}
			accepted <- c
		}
	}()

	first, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	serverFirst := <-accepted
	second, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	serverSecond := <-accepted
	ln.Close()
	first.Close()
	second.Close()
	serverFirst.Close()
	serverSecond.Close()

	want := "CONN " + first.LocalAddr().String() + "\n" +
		"CONN " + second.LocalAddr().String() + "\n"
	if got := log.String(); got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
	for _, line := range strings.Split(strings.TrimSuffix(log.String(), "\n"), "\n") {
		if !strings.HasPrefix(line, "CONN 127.0.0.1:") {
			t.Errorf("a line %q, want one that begins CONN 127.0.0.1:", line)
		}
	}
}

func TestLoadImages(t *testing.T) {
	const repository = "rollproof/fixture"
	reg := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(reg.Close)
	addr := strings.TrimPrefix(reg.URL, "http://")

	img, err := random.Image(1024, 2)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "fixture.tar")
	ref := name.MustParseReference("example.com/" + repository + ":latest")
	if err := tarball.WriteToFile(archive, ref, img); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() error {
		return loadImages(context.Background(), addr, []load{{repository: repository, archive: archive}})
	})
	want := "LOAD " + repository + "@" + digest.String() + "\n"
	if out != want {
		t.Errorf("standard output = %q, want %q", out, want)
	}

	resp, err := http.Get(reg.URL + "/v2/" + repository + "/manifests/" + digest.String())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("GET the manifest by digest: status %d, want 200", resp.StatusCode)
	}

	t.Run("two images", func(t *testing.T) {
		two := filepath.Join(t.TempDir(), "two.tar")
		one, err := random.Image(256, 1)
		if err != nil {
			t.Fatal(err)
		}
		other, err := random.Image(256, 1)
		if err != nil {
			t.Fatal(err)
		}
		refs := map[name.Reference]v1.Image{
			name.MustParseReference("example.com/one:latest"):   one,
			name.MustParseReference("example.com/other:latest"): other,
		}
		if err := tarball.MultiRefWriteToFile(two, refs); err != nil {
			t.Fatal(err)
		}
		err = loadImages(context.Background(), addr, []load{{repository: "x", archive: two}})
		if err == nil || !strings.Contains(err.Error(), two) {
			t.Errorf("loadImages = %v, want an error that names %s", err, two)
		}
	})

	t.Run("no image", func(t *testing.T) {
		empty := filepath.Join(t.TempDir(), "empty.tar")
		if err := writeEmptyManifest(empty); err != nil {
			t.Fatal(err)
		}
		err := loadImages(context.Background(), addr, []load{{repository: "x", archive: empty}})
		if err == nil || !strings.Contains(err.Error(), empty) {
			t.Errorf("loadImages = %v, want an error that names %s", err, empty)
		}
	})
}

func TestCheck(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	if err := check(context.Background(), srv.URL, time.Second); err != nil {
		t.Errorf("check of a TLS server that answers 401 = %v, want nil", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := ln.Addr().String()
	ln.Close()
	if err := check(context.Background(), "http://"+address+"/", 500*time.Millisecond); err == nil {
		t.Error("check of a closed port = nil, want an error")
	}
}

// captureStdout runs fn with standard output replaced and returns what fn wrote, so that a test's
// output stays its own. A failure of fn fails the test.
func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	saved := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fnErr := fn()
	os.Stdout = saved
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if fnErr != nil {
		t.Fatalf("the call = %v, want nil", fnErr)
	}
	return string(out)
}

// writeEmptyManifest writes an archive whose manifest.json holds no image.
func writeEmptyManifest(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	tw := tar.NewWriter(f)
	body := []byte("[]")
	if err := tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o644, Size: int64(len(body))}); err != nil {
		return err
	}
	if _, err := tw.Write(body); err != nil {
		return err
	}
	return tw.Close()
}
