//go:build linux

package images

import (
	"context"
	"io"
	"os"

	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/nzneit/rollproof/internal/cluster"
	"github.com/nzneit/rollproof/internal/config"
	"github.com/nzneit/rollproof/internal/verdict"
)

// storeArchive pushes the one image of the archive file into the run's registry by digest, under
// at.path, and returns at.repository and the digest. what names the file in an error's text, such
// as "the archive". No tag is written. An error is a Condition with ClusterFailed,
// "images.ENTRY: ...".
func storeArchive(ctx context.Context, c *cluster.Cluster, entry string, at place, file, what string) (config.ImageRef, error) {
	fail := func(text string) (config.ImageRef, error) {
		return config.ImageRef{}, verdict.Condition{Code: verdict.ClusterFailed, Text: "images." + entry + ": " + text}
	}

	m, err := tarball.LoadManifest(func() (io.ReadCloser, error) { return os.Open(file) })
	if err != nil {
		return fail(what + " cannot be read: " + err.Error())
	}
	if len(m) == 0 {
		return fail(what + " holds no image")
	}
	if len(m) > 1 {
		return fail(what + " holds more than one image")
	}
	img, err := tarball.ImageFromPath(file, nil)
	if err != nil {
		return fail(what + " cannot be read: " + err.Error())
	}

	digest, err := pushImage(ctx, c.PushRegistry, at.path, img)
	if err != nil {
		return fail("push to the run's registry failed: " + err.Error())
	}
	return config.ImageRef{Repository: at.repository, Digest: digest}, nil
}
