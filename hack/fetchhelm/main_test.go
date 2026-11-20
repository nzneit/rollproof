package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

// archive returns a gzipped tar that holds linux-ARCH/helm with content, and its SHA-256.
func archive(t *testing.T, content string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "linux-" + runtime.GOARCH + "/helm", Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

// server serves one archive for each Helm version, as get.helm.sh names them, and counts requests.
func server(t *testing.T) (string, map[key]string, *atomic.Int32) {
	t.Helper()
	three, sum3 := archive(t, "helm three\n")
	four, sum4 := archive(t, "helm four\n")
	files := map[string][]byte{
		"/helm-v3.22.1-linux-" + runtime.GOARCH + ".tar.gz": three,
		"/helm-v4.3.2-linux-" + runtime.GOARCH + ".tar.gz":  four,
	}
	hits := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		b, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	sums := map[key]string{
		{"3.22.1", runtime.GOARCH}: sum3,
		{"4.3.2", runtime.GOARCH}:  sum4,
	}
	return srv.URL, sums, hits
}

func TestFetchUnpacksBothVersions(t *testing.T) {
	base, sums, _ := server(t)
	dir := t.TempDir()
	if err := fetch(base, sums, dir); err != nil {
		t.Fatal(err)
	}
	for sub, want := range map[string]string{"helm-3": "helm three\n", "helm-4": "helm four\n"} {
		p := filepath.Join(dir, sub, "helm")
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o755 {
			t.Errorf("%s: mode %v, want 0755", p, fi.Mode().Perm())
		}
		if got, _ := os.ReadFile(p); string(got) != want {
			t.Errorf("%s holds %q, want %q", p, got, want)
		}
	}
}

func TestFetchRejectsAWrongChecksum(t *testing.T) {
	base, sums, _ := server(t)
	sums[key{"3.22.1", runtime.GOARCH}] = strings.Repeat("0", 64)
	dir := t.TempDir()
	err := fetch(base, sums, dir)
	if err == nil || !strings.Contains(err.Error(), "3.22.1") {
		t.Fatalf("fetch = %v, want an error that names 3.22.1", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("%s holds %v after a mismatch, want nothing", dir, entries)
	}
}

func TestFetchSkipsAPresentBinary(t *testing.T) {
	base, sums, hits := server(t)
	dir := t.TempDir()
	if err := fetch(base, sums, dir); err != nil {
		t.Fatal(err)
	}
	before := hits.Load()
	if err := fetch(base, sums, dir); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load() - before; n != 0 {
		t.Errorf("the second fetch made %d requests, want none", n)
	}
}

func TestFetchReplacesAChangedBinary(t *testing.T) {
	base, sums, hits := server(t)
	dir := t.TempDir()
	if err := fetch(base, sums, dir); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "helm-3", "helm")
	if err := os.WriteFile(p, []byte("changed\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := hits.Load()
	if err := fetch(base, sums, dir); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load() - before; n != 1 {
		t.Errorf("fetch made %d requests, want 1 (helm 3.22.1 only)", n)
	}
	if got, _ := os.ReadFile(p); string(got) != "helm three\n" {
		t.Errorf("%s holds %q after the second fetch, want the archive's content", p, got)
	}
}

// kubectlServer serves kubectl 1.37.0 for amd64 with the bytes "kubectl\n", as dl.k8s.io names
// it, and counts requests. It returns the base URL, the binary's SHA-256 and the counter.
func kubectlServer(t *testing.T) (string, string, *atomic.Int32) {
	t.Helper()
	body := []byte("kubectl\n")
	hits := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/release/v1.37.0/bin/linux/amd64/kubectl" {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(body)
	return srv.URL, hex.EncodeToString(sum[:]), hits
}

func TestFetchKubectl(t *testing.T) {
	base, sum, _ := kubectlServer(t)
	dir := t.TempDir()
	if err := fetchKubectl(base, "amd64", sum, dir); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "kubectl")
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Errorf("%s: mode %v, want 0755", p, fi.Mode().Perm())
	}
	if got, _ := os.ReadFile(p); string(got) != "kubectl\n" {
		t.Errorf("%s holds %q, want %q", p, got, "kubectl\n")
	}
}

func TestFetchKubectlRejectsAWrongChecksum(t *testing.T) {
	base, _, _ := kubectlServer(t)
	dir := t.TempDir()
	err := fetchKubectl(base, "amd64", strings.Repeat("0", 64), dir)
	if err == nil || !strings.Contains(err.Error(), "kubectl 1.37.0") || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("fetchKubectl = %v, want an error that names kubectl 1.37.0 and a checksum mismatch", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("%s holds %v after a mismatch, want nothing", dir, entries)
	}
}

func TestFetchKubectlSkipsAPresentBinary(t *testing.T) {
	base, sum, hits := kubectlServer(t)
	dir := t.TempDir()
	if err := fetchKubectl(base, "amd64", sum, dir); err != nil {
		t.Fatal(err)
	}
	before := hits.Load()
	if err := fetchKubectl(base, "amd64", sum, dir); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load() - before; n != 0 {
		t.Errorf("the second fetch made %d requests, want none", n)
	}
}

func TestFetchKubectlRefusesAnotherArchitecture(t *testing.T) {
	base, sum, hits := kubectlServer(t)
	err := fetchKubectl(base, "arm64", sum, t.TempDir())
	const want = "kubectl 1.37.0: no checksum for architecture arm64"
	if err == nil || err.Error() != want {
		t.Fatalf("fetchKubectl = %v, want %q", err, want)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("fetchKubectl made %d requests, want none", n)
	}
}
