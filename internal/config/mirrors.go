package config

import (
	"fmt"
	"strings"
)

// ImageHost returns the registry host of an image's name as the nodes read it: the part before
// the first slash when that part holds a dot or a colon, is localhost, or holds an upper-case
// letter; docker.io for any other name, and for index.docker.io. A tag or a digest does not
// change it. The rule is splitDockerDomain's in github.com/distribution/reference v0.6.0
// (normalize.go), which the nodes use, written out here; a change there changes it.
func ImageHost(image string) string {
	first, _, hasSlash := strings.Cut(image, "/")
	if !hasSlash {
		// A familiar name, such as "orders" or "orders:1.0", is Docker Hub's.
		return "docker.io"
	}
	switch {
	case first == "localhost":
		return first
	case first == "index.docker.io":
		return "docker.io"
	case strings.ContainsAny(first, ".:"):
		return first
	case strings.ToLower(first) != first:
		return first
	}
	return "docker.io"
}

// MirrorNote returns "the nodes pull HOST only from its mirror MIRROR, and a pull's error may
// name the image alone", HOST and MIRROR as the entry writes them, when the entry whose host is
// ImageHost(image) has a mirror; "" otherwise. Hosts are compared as written (section 8).
func (c *Config) MirrorNote(image string) string {
	host := ImageHost(image)
	for _, r := range c.Registries {
		if r.Host != host || r.Mirror == "" {
			continue
		}
		return fmt.Sprintf("the nodes pull %s only from its mirror %s, and a pull's error may name the image alone",
			r.Host, r.Mirror)
	}
	return ""
}
