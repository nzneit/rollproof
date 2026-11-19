package cluster

import (
	"fmt"
	"time"

	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// Spec is what Create needs. An empty NodeImage or RegistryImage means the default. Namespace is
// the workload's namespace. Timeout bounds, each on its own: the engine's pull of the node image
// and of the registry image when the store lacks them, the wait for the registry to answer,
// counted from its container's start, and the nodes' readiness. It is not one deadline over the
// whole creation, and kind's own waits are not bounded by it. Keep leaves in place what a failed
// step left.
type Spec struct {
	Name, RunDir, NodeImage, RegistryImage string
	Workers                                int
	Namespace                              string
	Timeout                                time.Duration
	Keep                                   bool
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
