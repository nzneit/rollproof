package cluster

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	yaml "go.yaml.in/yaml/v3"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// golden compares got with testdata/file byte for byte, and rewrites the file first with -update.
func golden(t *testing.T, file string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", file)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs (-want +got):\n%s", file, cmp.Diff(string(want), string(got)))
	}
}

// encodeKind writes the description as kind reads it: go.yaml.in/yaml/v3, the library kind's
// own config loader uses, indented by two spaces.
func encodeKind(t *testing.T, c *v1alpha4.Cluster) []byte {
	t.Helper()
	var b bytes.Buffer
	e := yaml.NewEncoder(&b)
	e.SetIndent(2)
	if err := e.Encode(c); err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestKindConfigGolden(t *testing.T) {
	for _, tc := range []struct {
		file     string
		rootless bool
	}{
		{"kind-rootless.golden.yaml", true},
		{"kind-rootful.golden.yaml", false},
	} {
		t.Run(tc.file, func(t *testing.T) {
			c := KindConfig(Spec{Name: "rollproof-x", Workers: 3}, tc.rootless)
			out := encodeKind(t, c)
			golden(t, tc.file, out)

			// kind's own loader decodes the file strictly; it must read back the same description.
			var back v1alpha4.Cluster
			d := yaml.NewDecoder(bytes.NewReader(out))
			d.KnownFields(true)
			if err := d.Decode(&back); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c, &back); diff != "" {
				t.Errorf("read back (-want +got):\n%s", diff)
			}
		})
	}
}

func TestKindConfigShape(t *testing.T) {
	c := KindConfig(Spec{Name: "rollproof-x", Workers: 1}, false)
	if c.Name != "" {
		t.Errorf("Name = %q, want it unset", c.Name)
	}
	want := []v1alpha4.Node{
		{Role: v1alpha4.ControlPlaneRole},
		{Role: v1alpha4.WorkerRole, Labels: map[string]string{"topology.kubernetes.io/zone": "rollproof-zone-1"}},
	}
	if diff := cmp.Diff(want, c.Nodes); diff != "" {
		t.Errorf("nodes (-want +got):\n%s", diff)
	}

	const image = "registry.example/kindest/node:v1.36.1@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	c = KindConfig(Spec{Name: "rollproof-x", Workers: 3, NodeImage: image}, false)
	if len(c.Nodes) != 4 {
		t.Fatalf("%d nodes, want 4", len(c.Nodes))
	}
	for i, n := range c.Nodes {
		if n.Image != image {
			t.Errorf("node %d image = %q, want %q", i, n.Image, image)
		}
	}
}
