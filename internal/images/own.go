package images

import (
	"archive/tar"
	"io"
	"os"
	"runtime"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// ownImage returns the probe's image: one layer that holds the file at binary as /rollproof, mode
// 0755, owned by root and dated at the Unix epoch, so that two builds from one file give one
// digest. The image runs /rollproof as 65532:65532 on linux and the runner's architecture.
func ownImage(binary string) (v1.Image, error) {
	info, err := os.Stat(binary)
	if err != nil {
		return nil, err
	}
	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return binaryTar(binary, info.Size())
	})
	if err != nil {
		return nil, err
	}
	img, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		return nil, err
	}
	cfg, err := img.ConfigFile() // read after the append, so that it holds the layer's diff ID
	if err != nil {
		return nil, err
	}
	cfg = cfg.DeepCopy()
	cfg.OS = "linux"
	cfg.Architecture = runtime.GOARCH
	cfg.Config.Entrypoint = []string{"/rollproof"}
	cfg.Config.User = "65532:65532"
	return mutate.ConfigFile(img, cfg)
}

// binaryTar returns a tar stream of one entry, rollproof, which holds the size bytes of the file
// at binary. The stream is written as it is read, so the binary is never held in memory. A file
// whose length is no longer size makes the stream end with an error.
func binaryTar(binary string, size int64) (io.ReadCloser, error) {
	f, err := os.Open(binary)
	if err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	go func() {
		defer f.Close()
		tw := tar.NewWriter(pw)
		err := tw.WriteHeader(&tar.Header{
			Name:     "rollproof",
			Typeflag: tar.TypeReg,
			Mode:     0o755,
			Size:     size,
			ModTime:  time.Unix(0, 0),
			Uid:      0,
			Gid:      0,
		})
		if err == nil {
			_, err = io.Copy(tw, f)
		}
		if err == nil {
			err = tw.Close()
		}
		pw.CloseWithError(err) // a nil err ends the stream with io.EOF
	}()
	return pr, nil
}
