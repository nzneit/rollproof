package report

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestReplaceEvidence(t *testing.T) {
	w, err := Open(t.TempDir(), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"one", "two"} {
		if err := w.ReplaceEvidence("windows.json", []byte(text)); err != nil {
			t.Fatalf("ReplaceEvidence(%q): %v", text, err)
		}
	}
	evidence := filepath.Join(w.Dir(), "evidence")
	if b, err := os.ReadFile(filepath.Join(evidence, "windows.json")); err != nil || string(b) != "two" {
		t.Errorf("windows.json is %q, %v; want two", b, err)
	}
	// The name is never numbered, and no temporary file is left beside it.
	if got := names(t, evidence); !slices.Equal(got, []string{"windows.json"}) {
		t.Errorf("the evidence folder holds %q, want windows.json alone", got)
	}

	before := tree(t, w.Dir())
	for _, bad := range []string{"../x", ""} {
		if err := w.ReplaceEvidence(bad, []byte("x")); err == nil {
			t.Errorf("ReplaceEvidence(%q) was allowed", bad)
		}
	}
	if after := tree(t, w.Dir()); !slices.Equal(before, after) {
		t.Errorf("a refused name changed the folder:\nbefore %v\nafter  %v", before, after)
	}

	if err := os.RemoveAll(evidence); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(evidence, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := w.ReplaceEvidence("windows.json", []byte("three")); err == nil {
		t.Error("ReplaceEvidence wrote with evidence a file, not a folder")
	}
}
