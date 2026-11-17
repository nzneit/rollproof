//go:build linux

package images

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"rollproof/internal/cluster"
	"rollproof/internal/config"
	"rollproof/internal/verdict"
)

func TestOwnImage(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "probe-binary")
	content := []byte("\x7fELF stands in for the tool's binary")
	if err := os.WriteFile(bin, content, 0o644); err != nil {
		t.Fatal(err)
	}

	img, err := ownImage(bin)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OS != "linux" || cfg.Architecture != runtime.GOARCH {
		t.Errorf("platform %s/%s, want linux/%s", cfg.OS, cfg.Architecture, runtime.GOARCH)
	}
	if diff := cmp.Diff([]string{"/rollproof"}, cfg.Config.Entrypoint); diff != "" {
		t.Errorf("entrypoint (-want +got):\n%s", diff)
	}
	if cfg.Config.User != "65532:65532" {
		t.Errorf("user %q, want 65532:65532", cfg.Config.User)
	}

	layers, err := img.Layers()
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) != 1 {
		t.Fatalf("%d layers, want 1", len(layers))
	}
	rc, err := layers[0].Uncompressed()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	tr := tar.NewReader(rc)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Name != "rollproof" || hdr.Typeflag != tar.TypeReg || hdr.Mode != 0o755 {
		t.Errorf("entry %q, type %c, mode %o; want rollproof, a regular file, 0755", hdr.Name, hdr.Typeflag, hdr.Mode)
	}
	if !hdr.ModTime.Equal(time.Unix(0, 0)) || hdr.Uid != 0 || hdr.Gid != 0 {
		t.Errorf("entry dated %v and owned by %d:%d; want the Unix epoch and 0:0", hdr.ModTime, hdr.Uid, hdr.Gid)
	}
	got, err := io.ReadAll(tr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Error("the layer's file differs from the binary")
	}
	if _, err := tr.Next(); err != io.EOF {
		t.Errorf("the layer holds a second entry: %v", err)
	}

	again, err := ownImage(bin)
	if err != nil {
		t.Fatal(err)
	}
	d1, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}
	d2, err := again.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Errorf("two builds from one file gave %s and %s", d1, d2)
	}
}

// Store refuses what config.NotBuilt stops, before it touches the cluster: the empty Cluster and
// the missing binary would otherwise give cluster-failed.
func TestStoreRefusesWhatIsNotBuilt(t *testing.T) {
	for _, img := range []config.Image{
		{Archive: "/tmp/orders.tar"},
		{Local: "orders:candidate", As: "registry.example/team/orders"},
	} {
		_, err := Store(context.Background(), &cluster.Cluster{}, "/does/not/exist",
			map[string]config.Image{"orders": img}, time.Minute)
		if err == nil {
			t.Fatalf("Store(%+v) gave no error, want internal-error naming images.orders", img)
		}
		if c := verdict.AsCondition(err); c.Code != verdict.InternalError || !strings.Contains(c.Text, "images.orders") {
			t.Errorf("Store(%+v) = %v, want internal-error naming images.orders", img, err)
		}
	}
}

// Section 12: a node that cannot pull a stored image, against fakes of the node's command runner.
func TestNodeThatCannotPull(t *testing.T) {
	const (
		node = "rollproof-x-worker"
		ref  = "rollproof.test/app@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	)
	ctx := context.Background()

	full := func(context.Context, string, ...string) ([]byte, error) {
		return []byte("pulling\nfatal: no space left on device\n\n"), errors.New("exit status 1")
	}
	err := pull(ctx, full, node, ref, time.Minute)
	if err == nil {
		t.Fatal("a full disk gave no error, want cluster-failed")
	}
	if c := verdict.AsCondition(err); c.Code != verdict.ClusterFailed || !strings.Contains(c.Text, node) ||
		!strings.Contains(c.Text, ref) || !strings.Contains(c.Text, "no space left on device") {
		t.Errorf("a full disk: %v, want cluster-failed naming the node, the reference and the last line", err)
	}

	// Under nerdctl, the node's exec ends the output with nerdctl's own report of the exit code,
	// after crictl's line.
	trailer := func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`time="2026-11-17T05:18:55Z" level=fatal msg="pulling image: failed to pull and unpack image: write /var/lib/containerd/x: no space left on device"` + "\n" +
			`time="2026-11-17T00:18:55-05:00" level=fatal msg="exec failed with exit code 1"` + "\n"), errors.New("exit status 1")
	}
	err = pull(ctx, trailer, node, ref, time.Minute)
	if err == nil {
		t.Fatal("a full disk under nerdctl gave no error, want cluster-failed")
	}
	if c := verdict.AsCondition(err); c.Code != verdict.ClusterFailed || !strings.Contains(c.Text, "no space left on device") ||
		strings.Contains(c.Text, "exec failed") {
		t.Errorf("a full disk under nerdctl: %v, want cluster-failed with crictl's line and not nerdctl's", err)
	}

	stuck := func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	start := time.Now()
	err = pull(ctx, stuck, node, ref, 50*time.Millisecond)
	if err == nil {
		t.Fatal("a pull that never ends gave no error, want cluster-failed")
	}
	if c := verdict.AsCondition(err); c.Code != verdict.ClusterFailed || c.Text != node+" could not pull "+ref+" within 50ms" {
		t.Errorf("a pull that never ends: %v, want cluster-failed saying it did not end within 50ms", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("a pull with a timeout of 50ms returned after %v", d)
	}

	// A pull that the caller's ctx ends, by a cancel or at the caller's own deadline, did not run
	// out of its timeout: it keeps the usual text.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	expired, cancelExpired := context.WithDeadline(ctx, time.Now())
	defer cancelExpired()
	for _, tc := range []struct {
		name string
		ctx  context.Context
		want string
	}{
		{"cancelled", cancelled, ": context canceled"},
		{"past the caller's deadline", expired, ": context deadline exceeded"},
	} {
		err = pull(tc.ctx, stuck, node, ref, time.Minute)
		if err == nil {
			t.Fatalf("a pull whose ctx is %s gave no error, want cluster-failed", tc.name)
		}
		if c := verdict.AsCondition(err); c.Code != verdict.ClusterFailed || c.Text != node+" could not pull "+ref+tc.want {
			t.Errorf("a pull whose ctx is %s: %v, want cluster-failed with the usual text", tc.name, err)
		}
	}

	var got []string
	fine := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		got = args
		return []byte("Image is up to date for sha256:0123\n"), nil
	}
	if err := pull(ctx, fine, node, ref, time.Minute); err != nil {
		t.Errorf("a pull that succeeds: %v", err)
	}
	if diff := cmp.Diff([]string{"crictl", "pull", ref}, got); diff != "" {
		t.Errorf("the command run in the node (-want +got):\n%s", diff)
	}
}
