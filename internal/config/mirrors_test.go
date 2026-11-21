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
