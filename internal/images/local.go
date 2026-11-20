//go:build linux

package images

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"rollproof/internal/cluster"
	"rollproof/internal/config"
	"rollproof/internal/verdict"
)

// storeLocal puts the engine's image local, NAME:TAG, into the run's registry by digest, under
// at.path, and returns where the nodes pull it from: at.repository and the digest. The engine's
// archive of the image is a file in the run's folder, named after entry, until the push has ended.
// config.Load has checked that entry is a valid component of a repository's path. An error is a
// Condition with ClusterFailed.
func storeLocal(ctx context.Context, c *cluster.Cluster, entry string, at place, local string) (config.ImageRef, error) {
	fail := func(text string) (config.ImageRef, error) {
		return config.ImageRef{}, verdict.Condition{Code: verdict.ClusterFailed, Text: "images." + entry + ": " + text}
	}

	dir := filepath.Join(c.RunDir, "images")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fail("the folder for the engine's archive cannot be made: " + err.Error())
	}
	file := filepath.Join(dir, entry+".tar")
	// A file that cannot be removed stays in the run's folder, which Delete removes.
	defer os.Remove(file)
	// After --, a value of local that begins with - is an image's name, not an option.
	out, err := exec.CommandContext(ctx, c.Engine.Binary, "save", "-o", file, "--", local).CombinedOutput()
	if err != nil {
		return fail("the engine could not save " + local + ": " + lastLine(out, err))
	}

	return storeArchive(ctx, c, entry, at, file, "the engine's archive of "+local)
}
