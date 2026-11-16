package lab

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// Builds an image that holds the one static binary as /app, pushes it to
// the registry at push by digest, and returns "sha256:…". No tag is written.
func PushBinary(ctx context.Context, push, repository, binary string) (string, error) {
	data, err := os.ReadFile(binary)
	if err != nil {
		return "", fmt.Errorf("lab: %w", err)
	}
	var layerTar bytes.Buffer
	tw := tar.NewWriter(&layerTar)
	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     "app",
		Mode:     0o755,
		Size:     int64(len(data)),
		ModTime:  time.Unix(0, 0),
	}); err != nil {
		return "", fmt.Errorf("lab: %w", err)
	}
	if _, err := tw.Write(data); err != nil {
		return "", fmt.Errorf("lab: %w", err)
	}
	if err := tw.Close(); err != nil {
		return "", fmt.Errorf("lab: %w", err)
	}
	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(layerTar.Bytes())), nil
	})
	if err != nil {
		return "", fmt.Errorf("lab: %w", err)
	}

	img, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		return "", fmt.Errorf("lab: %w", err)
	}
	cf, err := img.ConfigFile() // read after the append, so it keeps the layer's diff ID
	if err != nil {
		return "", fmt.Errorf("lab: %w", err)
	}
	cf = cf.DeepCopy()
	cf.OS = "linux"
	cf.Architecture = runtime.GOARCH
	cf.Config.Entrypoint = []string{"/app"}
	cf.Config.User = "65532"
	if img, err = mutate.ConfigFile(img, cf); err != nil {
		return "", fmt.Errorf("lab: %w", err)
	}

	digest, err := img.Digest()
	if err != nil {
		return "", fmt.Errorf("lab: %w", err)
	}
	ref, err := name.NewDigest(push+"/"+repository+"@"+digest.String(), name.Insecure)
	if err != nil {
		return "", fmt.Errorf("lab: %w", err)
	}
	if err := remote.Write(ref, img, remote.WithContext(ctx)); err != nil {
		return "", fmt.Errorf("lab: pushing to %s: %w", ref, err)
	}
	return digest.String(), nil
}
