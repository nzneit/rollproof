//go:build linux

package images

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"rollproof/internal/cluster"
	"rollproof/internal/config"
	"rollproof/internal/verdict"
)

// archiveRegistry starts a registry in the process and returns a Cluster whose run's registry it is.
func archiveRegistry(t *testing.T) *cluster.Cluster {
	t.Helper()
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	return &cluster.Cluster{PushRegistry: strings.TrimPrefix(srv.URL, "http://")}
}

// archiveOf writes n random images to a new archive and returns its path.
func archiveOf(t *testing.T, n int) string {
	t.Helper()
	refs := map[name.Reference]v1.Image{}
	for i := range n {
		img, err := random.Image(64, 1)
		if err != nil {
			t.Fatal(err)
		}
		ref, err := name.ParseReference("rollproof.test/img:" + strconv.Itoa(i))
		if err != nil {
			t.Fatal(err)
		}
		refs[ref] = img
	}
	file := filepath.Join(t.TempDir(), "orders.tar")
	if err := tarball.MultiRefWriteToFile(file, refs); err != nil {
		t.Fatal(err)
	}
	return file
}

// tags returns the tags the run's registry lists for repository.
func tags(t *testing.T, push, repository string) []string {
	t.Helper()
	resp, err := http.Get("http://" + push + "/v2/" + repository + "/tags/list")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct{ Tags []string }
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
	}
	return body.Tags
}

func TestStoreArchivePushesByDigest(t *testing.T) {
	c := archiveRegistry(t)
	file := archiveOf(t, 1)
	img, err := tarball.ImageFromPath(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := img.Digest()
	if err != nil {
		t.Fatal(err)
	}

	ref, err := storeArchive(t.Context(), c, "orders", file, "the archive")
	if err != nil {
		t.Fatal(err)
	}
	want := config.ImageRef{Repository: "rollproof.test/orders", Digest: digest.String()}
	if d := cmp.Diff(want, ref); d != "" {
		t.Errorf("stored as (-want +got):\n%s", d)
	}
	pushed, err := name.NewDigest(c.PushRegistry+"/orders@"+digest.String(), name.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Head(pushed); err != nil {
		t.Errorf("the run's registry does not hold %s: %v", pushed, err)
	}
	if got := tags(t, c.PushRegistry, "orders"); len(got) != 0 {
		t.Errorf("the run's registry has tags %v for orders", got)
	}
}

func TestStoreArchiveFailures(t *testing.T) {
	c := archiveRegistry(t)
	notTar := filepath.Join(t.TempDir(), "orders.tar")
	if err := os.WriteFile(notTar, []byte("not a tar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, file, text string
		exact            bool
	}{
		{"two images", archiveOf(t, 2), "images.orders: the archive holds more than one image", true},
		{"not a tar", notTar, "images.orders: the archive cannot be read: ", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := storeArchive(t.Context(), c, "orders", tc.file, "the archive")
			cond := verdict.AsCondition(err)
			if err == nil || cond.Code != verdict.ClusterFailed {
				t.Fatalf("storeArchive: %v, want cluster-failed", err)
			}
			if tc.exact && cond.Text != tc.text || !tc.exact && !strings.HasPrefix(cond.Text, tc.text) {
				t.Errorf("text %q, want %q", cond.Text, tc.text)
			}
		})
	}
}
