//go:build linux

// Package images gets images into the run's registry by digest and into the nodes, and says
// where each can be pulled from. It hides image archives, the registry's protocol and the way a
// node is asked to pull.
package images

import (
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"rollproof/internal/cluster"
	"rollproof/internal/config"
	"rollproof/internal/verdict"
)

// Stored is what Store put in place: the probe's own image, and each entry of the
// configuration's images under its name.
type Stored struct {
	Probe  config.ImageRef
	Images map[string]config.ImageRef
}

// Store packages the tool's binary at the given path as the probe's image and has the
// control-plane node pull it; stores each local image and has every node pull it. Nothing is
// stored under a tag. timeout bounds each node's pull. An error is a Condition with
// ClusterFailed. An entry with archive or as is an InternalError.
func Store(ctx context.Context, c *cluster.Cluster, binary string, images map[string]config.Image, timeout time.Duration) (Stored, error) {
	names := slices.Sorted(maps.Keys(images))
	for _, n := range names {
		if im := images[n]; im.Archive != "" || im.As != "" {
			return Stored{}, verdict.Condition{Code: verdict.InternalError, Text: "images." + n + ": archive and as are not built yet"}
		}
	}

	probeFailed := func(err error) (Stored, error) {
		return Stored{}, verdict.Condition{Code: verdict.ClusterFailed, Text: "the probe's image: " + err.Error()}
	}
	img, err := ownImage(binary)
	if err != nil {
		return probeFailed(err)
	}
	digest, err := pushImage(ctx, c.PushRegistry, "rollproof", img)
	if err != nil {
		return probeFailed(err)
	}
	s := Stored{
		Probe:  config.ImageRef{Repository: cluster.TestHost + "/rollproof", Digest: digest},
		Images: make(map[string]config.ImageRef, len(images)),
	}
	if err := pull(ctx, c.Exec, c.Nodes()[0], s.Probe.Ref(), timeout); err != nil {
		return Stored{}, err
	}

	for _, n := range names {
		ref, err := storeLocal(ctx, c, n, images[n].Local)
		if err != nil {
			return Stored{}, err
		}
		s.Images[n] = ref
		for _, node := range c.Nodes() {
			if err := pull(ctx, c.Exec, node, ref.Ref(), timeout); err != nil {
				return Stored{}, err
			}
		}
	}
	return s, nil
}

// pushImage pushes img to the registry at registry, as the runner reaches it, under repository
// and by its digest alone, and returns the digest with the sha256: prefix. No tag is written.
func pushImage(ctx context.Context, registry, repository string, img v1.Image) (string, error) {
	digest, err := img.Digest()
	if err != nil {
		return "", err
	}
	ref, err := name.NewDigest(registry+"/"+repository+"@"+digest.String(), name.Insecure)
	if err != nil {
		return "", err
	}
	if err := remote.Write(ref, img, remote.WithContext(ctx)); err != nil {
		return "", err
	}
	return digest.String(), nil
}

// pull has node pull ref through the node's own registry settings, the ones the kubelet's pulls
// use; exec runs a command in a node, and timeout bounds the pull. A failure is a Condition with
// ClusterFailed whose text names the node and ref and says that the pull failed. When timeout
// ended the pull while ctx had not ended, the text is "NODE could not pull REF within TIMEOUT";
// otherwise the line that lastLine picks from the output follows. The text gives no cause of its
// own: for an image under TestHost the failure may come from the run's registry or from the
// node's settings, and the node's runtime says only "not found" and hides that it also tried
// https://rollproof.test (proof p01, case e).
func pull(ctx context.Context, exec func(ctx context.Context, node string, args ...string) ([]byte, error), node, ref string, timeout time.Duration) error {
	pullCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// crictl pull uses the node's registry settings; ctr -n k8s.io images pull ignores them (proof p01).
	out, err := exec(pullCtx, node, "crictl", "pull", ref)
	if err == nil {
		return nil
	}
	if pullCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
		return verdict.Condition{Code: verdict.ClusterFailed, Text: node + " could not pull " + ref + " within " + timeout.String()}
	}
	return verdict.Condition{Code: verdict.ClusterFailed, Text: node + " could not pull " + ref + ": " + lastLine(out, err)}
}

// execReport is part of the line that nerdctl writes after the output of a command it ran in a
// node, when that command exits non-zero (proof p01, cases e and f).
const execReport = `msg="exec failed with exit code `

// lastLine returns the last line of out that is not empty, with the space around it removed, or
// the text of err, so trimmed, when out has none. It first skips the trailing lines that hold
// execReport: they are the engine's, so the command's own last line is the one before them.
func lastLine(out []byte, err error) string {
	lines := strings.Split(string(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" && !strings.Contains(l, execReport) {
			return l
		}
	}
	return strings.TrimSpace(err.Error())
}
