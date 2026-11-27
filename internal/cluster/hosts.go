package cluster

import (
	"strings"

	"github.com/nzneit/rollproof/internal/config"
)

// HostSettings returns the settings file every node needs for entry.Host, by decision 5: the
// mirror, when there is one, as the default place with its path override; the ca, for the
// default place, the mirror or else the host itself; and the run's registry, at runRegistry, as
// the one place tried before it. runRegistry is empty when no entry of images names the host in
// as. An empty result means the node needs no file for the host.
func HostSettings(entry config.Registry, runRegistry string) string {
	var b strings.Builder
	if entry.Mirror != "" {
		server, override := mirrorServer(entry.Mirror)
		b.WriteString("server = \"" + server + "\"\n")
		if override {
			b.WriteString("override_path = true\n")
		}
	}
	if entry.CA != "" {
		b.WriteString("ca = \"" + caPath(entry.Host) + "\"\n")
	}
	if runRegistry != "" {
		b.WriteString("[host.\"http://" + runRegistry + "\"]\n  capabilities = [\"pull\"]\n")
	}
	return b.String()
}

// HostsTOML returns the registry settings of a node for TestHost, whose entry holds only that
// host: the run's registry, at its address on the cluster's network, may serve a pull and may not
// resolve a tag.
func HostsTOML(runRegistry string) string {
	return HostSettings(config.Registry{Host: TestHost}, runRegistry)
}

// caPath returns the path, in every node, of the certificate authority file of host, as written.
func caPath(host string) string {
	return "/etc/containerd/certs.d/" + host + "/ca.pem"
}

// mirrorServer returns the server line's address for mirror, and whether the runtime's path
// override is needed: a path whose first segment is v2 holds the API's root and is used as
// written, and a prefix whose last segment is v2 is written with /v2 after it.
func mirrorServer(mirror string) (server string, override bool) {
	server = "https://" + mirror
	_, path, ok := strings.Cut(mirror, "/")
	if !ok {
		return server, false
	}
	segments := strings.Split(path, "/")
	if segments[0] == "v2" {
		return server, true
	}
	if segments[len(segments)-1] == "v2" {
		return server + "/v2", true
	}
	return server, false
}
