//go:build cluster

package lab

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/kind/pkg/cluster"
)

// provider returns kind's provider for the engine that Lab.Engine names.
func provider(engine string) *cluster.Provider {
	switch engine {
	case "docker":
		return cluster.NewProvider(cluster.ProviderWithDocker())
	case "podman":
		return cluster.NewProvider(cluster.ProviderWithPodman())
	default:
		return cluster.NewProvider(cluster.ProviderWithNerdctl(""))
	}
}

func TestUpDown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	l, err := Up(ctx, "lab", &v1alpha4.Cluster{Nodes: []v1alpha4.Node{{Role: v1alpha4.ControlPlaneRole}}})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Down()

	nodes := l.Nodes()
	if len(nodes) != 1 {
		t.Fatalf("Nodes() = %v, want one name", nodes)
	}
	if out, err := l.Exec(nodes[0], nil, "crictl", "version"); err != nil {
		t.Fatalf("crictl version in %s: %v\n%s", nodes[0], err, out)
	}
	dir := filepath.Dir(l.Kubeconfig())

	if err := l.Down(); err != nil {
		t.Fatal(err)
	}
	names, err := provider(l.Engine()).List()
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(names, "rollproof-proof-lab") {
		t.Errorf("kind still lists rollproof-proof-lab after Down: %v", names)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the kubeconfig's folder %s is still there after Down (%v)", dir, err)
	}
	if err := l.Down(); err != nil {
		t.Errorf("a second Down: %v", err)
	}
}
