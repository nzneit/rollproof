//go:build linux

package cluster

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	kindcluster "sigs.k8s.io/kind/pkg/cluster"
)

// evidenceDir is an evidence function that writes each file under dir.
func evidenceDir(dir string) func(string) (io.WriteCloser, error) {
	return func(name string) (io.WriteCloser, error) {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		return os.Create(path)
	}
}

// writeTestFile writes text to path, making its folders.
func writeTestFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// filesUnder returns the paths of the regular files under dir, relative to it and sorted.
func filesUnder(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	return files
}

// replaceLogSeams replaces kindCollectLogs and containerLogs for one test and restores them after
// it.
func replaceLogSeams(t *testing.T) {
	t.Helper()
	oldKind, oldContainer := kindCollectLogs, containerLogs
	t.Cleanup(func() { kindCollectLogs, containerLogs = oldKind, oldContainer })
}

func TestCollectLogsCopiesEveryFile(t *testing.T) {
	replaceLogSeams(t)
	runDir, evDir := t.TempDir(), t.TempDir()
	kindCollectLogs = func(_ *kindcluster.Provider, name, dir string) error {
		if name != "rollproof-x" {
			t.Errorf("kind's collection was given the cluster %q, want rollproof-x", name)
		}
		writeTestFile(t, filepath.Join(dir, "kind-version.txt"), "v0.33.0")
		writeTestFile(t, filepath.Join(dir, "rollproof-x-control-plane", "kubelet.log"), "kubelet says")
		return nil
	}
	var containers []string
	containerLogs = func(_ Engine, _ context.Context, container string) (string, bool, error) {
		containers = append(containers, container)
		return "registry says", true, nil
	}

	if err := collectLogs(context.Background(), Engine{}, nil, "rollproof-x", runDir, evidenceDir(evDir)); err != nil {
		t.Fatalf("collectLogs: %v", err)
	}
	want := []string{"kind/kind-version.txt", "kind/rollproof-x-control-plane/kubelet.log", "registry.log"}
	if got := filesUnder(t, evDir); !slices.Equal(got, want) {
		t.Fatalf("the evidence holds %v, want %v", got, want)
	}
	for name, text := range map[string]string{
		"kind/kind-version.txt":                      "v0.33.0",
		"kind/rollproof-x-control-plane/kubelet.log": "kubelet says",
		"registry.log":                               "registry says",
	} {
		b, err := os.ReadFile(filepath.Join(evDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != text {
			t.Errorf("%s holds %q, want %q", name, b, text)
		}
	}
	if want := []string{"rollproof-x-registry"}; !slices.Equal(containers, want) {
		t.Errorf("the container's log was read for %v, want %v", containers, want)
	}
	if _, err := os.Stat(filepath.Join(runDir, "kind-logs")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("kind's log folder is left under the run's folder: %v", err)
	}
}

// openUnder counts the process's open file descriptors whose path is under dir.
func openUnder(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if target, err := os.Readlink(filepath.Join("/proc/self/fd", e.Name())); err == nil &&
			strings.HasPrefix(target, dir+string(filepath.Separator)) {
			n++
		}
	}
	return n
}

// Each of kind's files is closed once it is copied, not when the walk ends.
func TestCopyLogsClosesEachFile(t *testing.T) {
	dir, evDir := t.TempDir(), t.TempDir()
	for _, name := range []string{"a.log", "b.log", filepath.Join("node", "c.log"), filepath.Join("node", "d.log")} {
		writeTestFile(t, filepath.Join(dir, name), "says "+name)
	}
	copies, most := 0, 0
	ev := evidenceDir(evDir)
	evidence := func(name string) (io.WriteCloser, error) {
		copies++
		most = max(most, openUnder(t, dir))
		return ev(name)
	}
	if err := copyLogs(evidence, dir); err != nil {
		t.Fatalf("copyLogs: %v", err)
	}
	if copies != 4 || most != 1 {
		t.Errorf("%d copies with at most %d of kind's files open, want 4 with 1: the file being copied", copies, most)
	}
	if n := openUnder(t, dir); n != 0 {
		t.Errorf("%d of kind's files are open after the copy, want none", n)
	}
}

func TestCollectLogsWithNoRegistry(t *testing.T) {
	replaceLogSeams(t)
	runDir, evDir := t.TempDir(), t.TempDir()
	kindCollectLogs = func(_ *kindcluster.Provider, _, dir string) error {
		writeTestFile(t, filepath.Join(dir, "kind-version.txt"), "v0.33.0")
		return nil
	}
	containerLogs = func(Engine, context.Context, string) (string, bool, error) {
		return "", false, nil
	}

	if err := collectLogs(context.Background(), Engine{}, nil, "rollproof-x", runDir, evidenceDir(evDir)); err != nil {
		t.Fatalf("collectLogs: %v", err)
	}
	if got, want := filesUnder(t, evDir), []string{"kind/kind-version.txt"}; !slices.Equal(got, want) {
		t.Errorf("the evidence holds %v, want %v and no registry.log", got, want)
	}
}

func TestCollectLogsGoesOnAfterAnError(t *testing.T) {
	replaceLogSeams(t)
	runDir, evDir := t.TempDir(), t.TempDir()
	kindCollectLogs = func(*kindcluster.Provider, string, string) error {
		return errors.New("no nodes")
	}
	containerLogs = func(Engine, context.Context, string) (string, bool, error) {
		return "registry says", true, nil
	}

	err := collectLogs(context.Background(), Engine{}, nil, "rollproof-x", runDir, evidenceDir(evDir))
	if err == nil || !strings.Contains(err.Error(), "no nodes") {
		t.Fatalf("collectLogs = %v, want an error that holds no nodes", err)
	}
	b, err := os.ReadFile(filepath.Join(evDir, "registry.log"))
	if err != nil {
		t.Fatalf("the registry's log was not written after kind's error: %v", err)
	}
	if string(b) != "registry says" {
		t.Errorf("registry.log holds %q, want %q", b, "registry says")
	}
}

func TestCollectLogsWithNoEvidence(t *testing.T) {
	replaceLogSeams(t)
	kindCollectLogs = func(*kindcluster.Provider, string, string) error {
		t.Error("kind's logs were collected with no evidence function")
		return nil
	}
	containerLogs = func(Engine, context.Context, string) (string, bool, error) {
		t.Error("the registry's log was read with no evidence function")
		return "", false, nil
	}

	if err := collectLogs(context.Background(), Engine{}, nil, "rollproof-x", t.TempDir(), nil); err != nil {
		t.Errorf("collectLogs with no evidence = %v, want nil", err)
	}
}
