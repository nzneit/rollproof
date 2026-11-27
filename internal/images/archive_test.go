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

	"github.com/nzneit/rollproof/internal/cluster"
	"github.com/nzneit/rollproof/internal/config"
	"github.com/nzneit/rollproof/internal/verdict"
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

	ref, err := storeArchive(t.Context(), c, "orders", placeOf("orders", config.Image{Archive: file}), file, "the archive")
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

func TestPlaceOf(t *testing.T) {
	for _, tc := range []struct {
		entry      string
		im         config.Image
		path, repo string
	}{
		{"orders", config.Image{Local: "orders:candidate"}, "orders", "rollproof.test/orders"},
		{"orders", config.Image{Local: "orders:candidate", As: "registry.example/team/orders"}, "team/orders", "registry.example/team/orders"},
		{"x", config.Image{Archive: "/a.tar", As: "docker.io/library/orders"}, "library/orders", "docker.io/library/orders"},
		{"x", config.Image{Local: "a:b", As: "registry.example:5000/a/b"}, "a/b", "registry.example:5000/a/b"},
	} {
		want := place{path: tc.path, repository: tc.repo}
		if got := placeOf(tc.entry, tc.im); got != want {
			t.Errorf("placeOf(%q, %+v) = %+v, want %+v", tc.entry, tc.im, got, want)
		}
	}
}

// status returns the status of a GET of path from the run's registry at push.
func status(t *testing.T, push, path string) int {
	t.Helper()
	resp, err := http.Get("http://" + push + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestStoreArchiveUnderAs(t *testing.T) {
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

	at := placeOf("orders", config.Image{Archive: file, As: "registry.example/team/orders"})
	ref, err := storeArchive(t.Context(), c, "orders", at, file, "the archive")
	if err != nil {
		t.Fatal(err)
	}
	want := config.ImageRef{Repository: "registry.example/team/orders", Digest: digest.String()}
	if d := cmp.Diff(want, ref); d != "" {
		t.Errorf("stored as (-want +got):\n%s", d)
	}
	for path, code := range map[string]int{
		"/v2/team/orders/manifests/" + digest.String(): http.StatusOK,
		"/v2/orders/manifests/" + digest.String():      http.StatusNotFound,
	} {
		if got := status(t, c.PushRegistry, path); got != code {
			t.Errorf("GET %s: %d, want %d", path, got, code)
		}
	}
	if got := tags(t, c.PushRegistry, "team/orders"); len(got) != 0 {
		t.Errorf("the run's registry has tags %v for team/orders", got)
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
			_, err := storeArchive(t.Context(), c, "orders", placeOf("orders", config.Image{Archive: tc.file}), tc.file, "the archive")
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
