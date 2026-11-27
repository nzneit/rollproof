package cluster

import (
	"fmt"
	"io"
	"path/filepath"
	"time"

	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"

	"github.com/nzneit/rollproof/internal/config"
)

// Spec is what Create needs. Empty image names mean the default; Namespace is
// the workload's namespace. Timeout bounds, each on its own: the engine's pull
// of the node image and of the registry image when the store lacks them, the
// wait for the registry to answer, counted from its container's start, and the
// nodes' readiness. It is not one deadline over the whole creation, and kind's
// own waits are not bounded by it. After a failure Keep leaves the nodes, the
// run's folder, its kubeconfig and the registry container in place; the tool
// deletes them otherwise. Evidence holds a failed set-up's logs, kind's under
// kind/ and the registry's as registry.log; nil collects nothing.
type Spec struct {
	Name, RunDir, NodeImage, RegistryImage string
	Workers                                int
	Namespace                              string
	Timeout                                time.Duration
	Keep                                   bool
	// Evidence is where a failed set-up's logs go: kind's under kind/ and the
	// registry's as registry.log. Nil collects nothing.
	Evidence func(name string) (io.WriteCloser, error)
	// Registries are the configuration's entries; AsHosts are the hosts that an
	// entry of images names in as. Create writes, into every node, each host's
	// settings and ca, with the run's registry first for each host of AsHosts,
	// and mounts the credentials file in every node when one is needed.
	Registries []config.Registry
	AsHosts    []string
}

const (
	registryPatch = "[plugins.\"io.containerd.grpc.v1.cri\".registry]\n  config_path = \"/etc/containerd/certs.d\""
	keyringPatch  = "[plugins.\"io.containerd.grpc.v1.cri\".containerd.runtimes.runc.options]\n  NoNewKeyring = true"
)

// KindConfig returns the kind description of the run's cluster: one control-plane node and
// s.Workers workers, a zone label on each worker, and the runtime settings the nodes need.
// rootless adds the setting for an engine without root. With an entry that has credentials,
// every node also mounts RUNDIR/node-credentials.json read-only at /var/lib/kubelet/config.json.
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
	if Credentials(s.Registries) != nil {
		for i := range c.Nodes {
			c.Nodes[i].ExtraMounts = []v1alpha4.Mount{{
				ContainerPath: kubeletCredentials,
				HostPath:      filepath.Join(s.RunDir, credentialsName),
				Readonly:      true,
			}}
		}
	}
	return c
}
