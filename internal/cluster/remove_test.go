//go:build linux

package cluster

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	kindcluster "sigs.k8s.io/kind/pkg/cluster"

	"rollproof/internal/verdict"
)

// remove refuses a name without the prefix and a run folder that is not named after the cluster,
// before it calls kind or the engine: neither is reached, and the folder with its file stays.
func TestRemoveRefuses(t *testing.T) {
	for _, tc := range []struct {
		name, cluster, folder, text string
	}{
		{"a name without the prefix", "other", "other",
			"refused to remove a cluster whose name does not start with rollproof-"},
		{"a folder not named after the cluster", "rollproof-test-remove", "rollproof-test-other",
			"refused to remove a run's folder that is not named after its cluster"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := filepath.Join(t.TempDir(), "called")
			script := filepath.Join(t.TempDir(), "engine")
			if err := os.WriteFile(script, []byte("#!/bin/sh\n: > "+strconv.Quote(called)+"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			oldDelete := kindDelete
			deletes := 0
			kindDelete = func(*kindcluster.Provider, string, string) error {
				deletes++
				return nil
			}
			t.Cleanup(func() { kindDelete = oldDelete })

			dir := filepath.Join(t.TempDir(), tc.folder)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(dir, "left")
			if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}

			err := remove(context.Background(), Engine{Binary: script}, tc.cluster, dir)
			if err == nil {
				t.Fatal("remove gave no error")
			}
			if c := verdict.AsCondition(err); c.Code != verdict.InternalError || c.Text != tc.text {
				t.Errorf("remove = %s: %q, want internal-error: %q", c.Code, c.Text, tc.text)
			}
			if deletes != 0 {
				t.Errorf("kind's Delete was called %d times, want 0", deletes)
			}
			if _, err := os.Stat(called); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("the engine was called: %v", err)
			}
			if _, err := os.Stat(file); err != nil {
				t.Errorf("the folder's file is gone: %v", err)
			}
		})
	}
}
