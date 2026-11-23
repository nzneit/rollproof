//go:build linux

package cluster

import "testing"

// Kind's own 13 cases of Test_sanitizeImage, with kind's image and kind's pullImageName: the name
// by which kind's Podman provider asks the engine for the node image.
func TestPodmanNameOnKindsCases(t *testing.T) {
	const d = "@sha256:69860bda5563ac81e3c0057d654b5253219618a22ec3a346306239bba8cfa1a6"
	for _, c := range []struct{ image, want string }{
		{"kindest/node:v1.21.1" + d, "docker.io/kindest/node" + d},
		{"kindest/node:v1.21.1", "docker.io/kindest/node:v1.21.1"},
		{"kindest/node" + d, "docker.io/kindest/node" + d},
		{"foo.bar/kindest/node:v1.21.1" + d, "foo.bar/kindest/node" + d},
		{"foo.bar/kindest/node:v1.21.1", "foo.bar/kindest/node:v1.21.1"},
		{"foo.bar/kindest/node" + d, "foo.bar/kindest/node" + d},
		{"foo.bar/baz:quux" + d, "foo.bar/baz" + d},
		{"foo.bar/baz:quux", "foo.bar/baz:quux"},
		{"foo.bar/baz" + d, "foo.bar/baz" + d},
		{"localhost:5000/kindest/node:v1.21.1" + d, "localhost:5000/kindest/node" + d},
		{"baz:quux" + d, "docker.io/library/baz" + d},
		{"baz:quux", "docker.io/library/baz:quux"},
		{"baz" + d, "docker.io/library/baz" + d},
	} {
		if got := podmanName(c.image); got != c.want {
			t.Errorf("podmanName(%q) = %q, want %q", c.image, got, c.want)
		}
	}
}

// The suite's own node image, and a node image named with a tag and a digest: a name with a digest
// is asked for without its tag.
func TestPodmanNameOfTheSuitesNodeImages(t *testing.T) {
	for _, c := range []struct{ image, want string }{
		{DefaultNodeImage(),
			"docker.io/kindest/node@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5"},
		{"kindest/node:v1.34.11@sha256:44e222ee2132dab25ff87301682f89eb82c7880ea3a1bf543bfe9708fd08d67d",
			"docker.io/kindest/node@sha256:44e222ee2132dab25ff87301682f89eb82c7880ea3a1bf543bfe9708fd08d67d"},
	} {
		if got := podmanName(c.image); got != c.want {
			t.Errorf("podmanName(%q) = %q, want %q", c.image, got, c.want)
		}
	}
}
