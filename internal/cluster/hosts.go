package cluster

// HostsTOML returns the registry settings of a node for TestHost: the run's registry, at its
// address on the cluster's network, may serve a pull and may not resolve a tag.
func HostsTOML(runRegistry string) string {
	return "[host.\"http://" + runRegistry + "\"]\n  capabilities = [\"pull\"]\n"
}
