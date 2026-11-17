//go:build linux

package images

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"rollproof/internal/cluster"
	"rollproof/internal/config"
	"rollproof/internal/verdict"
)

// storeLocal puts the engine's image local, NAME:TAG, into the run's registry by digest, under the
// repository entry, and returns where the nodes pull it from: TestHost/ENTRY and the digest. The
// engine's archive of the image is a file in the run's folder until the push has ended. config.Load
// has checked that entry is a valid component of a repository's path. An error is a Condition
// with ClusterFailed.
func storeLocal(ctx context.Context, c *cluster.Cluster, entry, local string) (config.ImageRef, error) {
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
	out, err := exec.CommandContext(ctx, c.Engine.Binary, "save", local, "-o", file).CombinedOutput()
	if err != nil {
		return fail("the engine could not save " + local + ": " + lastLine(out, err))
	}

	m, err := tarball.LoadManifest(func() (io.ReadCloser, error) { return os.Open(file) })
	if err != nil {
		return fail("the engine's archive of " + local + " cannot be read: " + err.Error())
	}
	if len(m) > 1 {
		return fail("the engine's archive of " + local + " holds more than one image")
	}
	img, err := tarball.ImageFromPath(file, nil)
	if err != nil {
		return fail("the engine's archive of " + local + " cannot be read: " + err.Error())
	}

	digest, err := pushImage(ctx, c.PushRegistry, entry, img)
	if err != nil {
		return fail("push to the run's registry failed: " + err.Error())
	}
	return config.ImageRef{Repository: cluster.TestHost + "/" + entry, Digest: digest}, nil
}
