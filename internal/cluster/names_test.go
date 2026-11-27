package cluster

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nzneit/rollproof/internal/verdict"
)

func TestContractNames(t *testing.T) {
	if ProbeNamespace != "rollproof-probe" {
		t.Errorf("ProbeNamespace = %q", ProbeNamespace)
	}
	if TestHost != "rollproof.test" {
		t.Errorf("TestHost = %q", TestHost)
	}
}

// writeConfig creates dir/rollproof.yaml and returns its path.
func writeConfig(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "rollproof.yaml")
	if err := os.WriteFile(p, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNameFromPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := writeConfig(t, dir)
	real, err := filepath.EvalSymlinks(file)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(real))
	want := "rollproof-" + hex.EncodeToString(sum[:])[:10]

	link := filepath.Join(t.TempDir(), "link.yaml")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	for _, p := range []string{file, link, "rollproof.yaml", filepath.Join(dir, "sub", "..", "rollproof.yaml")} {
		got, err := Name(p, "")
		if err != nil {
			t.Fatalf("Name(%q): %v", p, err)
		}
		if got != want {
			t.Errorf("Name(%q) = %q, want %q", p, got, want)
		}
	}

	other := writeConfig(t, t.TempDir())
	got, err := Name(other, "")
	if err != nil {
		t.Fatal(err)
	}
	if got == want {
		t.Errorf("two files gave the same name %q", got)
	}

	// Review Focus 4: a folder whose name holds a space is no different.
	spaced := filepath.Join(t.TempDir(), "with space")
	if err := os.Mkdir(spaced, 0o755); err != nil {
		t.Fatal(err)
	}
	spacedFile := writeConfig(t, spaced)
	spacedReal, err := filepath.EvalSymlinks(spacedFile)
	if err != nil {
		t.Fatal(err)
	}
	spacedSum := sha256.Sum256([]byte(spacedReal))
	if got, err := Name(spacedFile, ""); err != nil || got != "rollproof-"+hex.EncodeToString(spacedSum[:])[:10] {
		t.Errorf("Name(%q) = %q, %v; want the same name as for any other folder", spacedFile, got, err)
	}
}

func TestNameGiven(t *testing.T) {
	file := writeConfig(t, t.TempDir())
	for _, n := range []string{"pr-12.a", "a", strings.Repeat("a", 40)} {
		got, err := Name(file, n)
		if err != nil {
			t.Errorf("Name(file, %q): %v", n, err)
			continue
		}
		if got != "rollproof-"+n {
			t.Errorf("Name(file, %q) = %q", n, got)
		}
	}
	for _, n := range []string{"Upper", "has space", "under_score", strings.Repeat("a", 41), "-lead", "trail-", ".lead", "trail."} {
		_, err := Name(file, n)
		if err == nil {
			t.Errorf("Name(file, %q) gave no error", n)
			continue
		}
		if c := verdict.AsCondition(err); c.Code != verdict.ConfigRejected || !strings.Contains(c.Text, "--name") {
			t.Errorf("Name(file, %q): %v, want config-rejected naming --name", n, err)
		}
	}
}

func TestNameMissingFile(t *testing.T) {
	_, err := Name(filepath.Join(t.TempDir(), "absent.yaml"), "")
	if err == nil {
		t.Fatal("no error for a file that does not exist")
	}
	if c := verdict.AsCondition(err); c.Code != verdict.ConfigRejected {
		t.Errorf("code = %s, want config-rejected", c.Code)
	}
}

func TestRunDir(t *testing.T) {
	if got, want := RunDir("rollproof-pr-12.a"), filepath.Join(os.TempDir(), "rollproof-pr-12.a"); got != want {
		t.Errorf("RunDir = %q, want %q", got, want)
	}
}
