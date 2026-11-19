package cluster

import (
	"fmt"
	"io"
	"time"

	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// Spec is what Create needs. Empty image names mean the default; Namespace is
// the workload's namespace. Timeout bounds, each on its own, the engine's pull,
// the registry's wait and the nodes' readiness; kind's waits are not bounded by
// it. kind keeps the nodes of a failed attempt whatever Keep says; the tool
// deletes them unless Keep is set. Evidence holds a failed set-up's logs, and
// kind's under kind/ and the registry's as registry.log; nil collects nothing.
type Spec struct {
	Name, RunDir, NodeImage, RegistryImage string
	Workers                                int
	Namespace                              string
	Timeout                                time.Duration
	Keep                                   bool
	// Evidence is where a failed set-up's logs go: kind's under kind/ and the registry's as
	// registry.log. Nil collects nothing.
	Evidence func(name string) (io.WriteCloser, error)
}

const (
	registryPatch = "[plugins.\"io.containerd.grpc.v1.cri\".registry]\n  config_path = \"/etc/containerd/certs.d\""
	keyringPatch  = "[plugins.\"io.containerd.grpc.v1.cri\".containerd.runtimes.runc.options]\n  NoNewKeyring = true"
)

// KindConfig returns the kind description of the run's cluster: one control-plane node and
// s.Workers workers, a zone label on each worker, and the runtime settings the nodes need.
// rootless adds the setting for an engine without root.
func KindConfig(s Spec, rootless bool) *v1alpha4.Cluster {
	c := &v1alpha4.Cluster{
		TypeMeta:                v1alpha4.TypeMeta{Kind: "Cluster", APIVersion: "kind.x-k8s.io/v1alpha4"},
		Nodes:                   []v1alpha4.Node{{Role: v1alpha4.ControlPlaneRole, Image: s.NodeImage}},
		ContainerdConfigPatches: []string{registryPatch},
	}
	for i := 1; i <= s.Workers; i++ {
		c.Nodes = append(c.Nodes, v1alpha4.Node{
			Role:   v1alpha4.WorkerRole,
			Image:  s.NodeImage,
			Labels: map[string]string{"topology.kubernetes.io/zone": fmt.Sprintf("rollproof-zone-%d", i)},
		})
	}
	if rootless {
		c.ContainerdConfigPatches = append(c.ContainerdConfigPatches, keyringPatch)
	}
	return c
}
