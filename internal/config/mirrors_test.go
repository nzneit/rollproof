package config

import (
	"strings"
	"testing"
)

func TestImageHost(t *testing.T) {
	for image, want := range map[string]string{
		"orders":                         "docker.io",
		"orders:1.0":                     "docker.io",
		"library/orders":                 "docker.io",
		"myregistry/team/orders":         "docker.io",
		"docker.io/library/orders":       "docker.io",
		"index.docker.io/library/orders": "docker.io",
		"registry.example/team/orders@sha256:" + strings.Repeat("a", 64): "registry.example",
		"registry.example:5000/orders":                                   "registry.example:5000",
		"localhost/orders":                                               "localhost",
		"[fd00::5]:5000/orders":                                          "[fd00::5]:5000",
		"[fd00::5]/team/orders":                                          "[fd00::5]",
		"registry.example:443/team/orders":                               "registry.example:443",
		"index.docker.io:443/library/orders":                             "index.docker.io:443",
		"Registry/orders":                                                "Registry",
	} {
		if got := ImageHost(image); got != want {
			t.Errorf("ImageHost(%q) = %q, want %q", image, got, want)
		}
	}
}

func TestMirrorNote(t *testing.T) {
	c := &Config{Registries: []Registry{
		{Host: "registry.example", Mirror: "mirror.corp.example/registry-example"},
		{Host: "other.example", CA: "/ca.pem"},
	}}
	const want = "the nodes pull registry.example only from its mirror mirror.corp.example/registry-example, and a pull's error may name the image alone"
	image := "registry.example/team/orders@sha256:" + strings.Repeat("a", 64)
	if got := c.MirrorNote(image); got != want {
		t.Errorf("MirrorNote(%q) = %q, want %q", image, got, want)
	}
	for _, other := range []string{"other.example/x", "unlisted.example/x"} {
		if got := c.MirrorNote(other); got != "" {
			t.Errorf("MirrorNote(%q) = %q, want none", other, got)
		}
	}
	if got := (&Config{}).MirrorNote("registry.example/x"); got != "" {
		t.Errorf("MirrorNote with no entries = %q, want none", got)
	}
}

// A host is compared as the image's name writes it, with its port (section 8): a port of 443 is a
// host of its own, and only index.docker.io exactly is Docker Hub.
func TestMirrorNoteComparesHostsAsWritten(t *testing.T) {
	note := func(host, mirror string) string {
		return "the nodes pull " + host + " only from its mirror " + mirror + ", and a pull's error may name the image alone"
	}
	for _, r := range []struct {
		name, host, image, want string
	}{
		{"an image with :443, an entry without", "registry.example", "registry.example:443/team/orders", ""},
		{"an entry with :443, an image without", "registry.example:443", "registry.example/team/orders", ""},
		{"an entry with :443, an image with it", "registry.example:443", "registry.example:443/team/orders",
			note("registry.example:443", "mirror.corp.example/r")},
		{"index.docker.io, a docker.io entry", "docker.io", "index.docker.io/library/orders",
			note("docker.io", "mirror.corp.example/r")},
		{"index.docker.io:443, a docker.io entry", "docker.io", "index.docker.io:443/library/orders", ""},
		{"index.docker.io:443, its own entry", "index.docker.io:443", "index.docker.io:443/library/orders",
			note("index.docker.io:443", "mirror.corp.example/r")},
		{"an IPv6 address with no port", "[fd00::5]", "[fd00::5]/team/orders", note("[fd00::5]", "mirror.corp.example/r")},
	} {
		t.Run(r.name, func(t *testing.T) {
			c := &Config{Registries: []Registry{{Host: r.host, Mirror: "mirror.corp.example/r"}}}
			if got := c.MirrorNote(r.image); got != r.want {
				t.Errorf("with an entry for %s, MirrorNote(%q) = %q, want %q", r.host, r.image, got, r.want)
			}
		})
	}
}

func TestMirrorNoteForEachDockerHubName(t *testing.T) {
	c := &Config{Registries: []Registry{{Host: "docker.io", Mirror: "mirror.corp.example/docker-io"}}}
	const want = "the nodes pull docker.io only from its mirror mirror.corp.example/docker-io, and a pull's error may name the image alone"
	for _, image := range []string{
		"orders",
		"library/orders",
		"docker.io/library/orders",
		"index.docker.io/library/orders",
		"orders@sha256:" + strings.Repeat("a", 64),
	} {
		if got := c.MirrorNote(image); got != want {
			t.Errorf("MirrorNote(%q) = %q, want %q", image, got, want)
		}
	}
}
