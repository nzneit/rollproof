//go:build linux

/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// podmanName and stripTag are copied from sigs.k8s.io/kind v0.33.0,
// pkg/cluster/internal/providers/podman/images.go (sanitizeImage and stripTag), with one change:
// podmanName keeps only the name that kind's Podman provider asks the engine for, not the name it
// prints. Read them again when the linked kind release changes; TestPodmanNameOnKindsCases holds
// kind's own cases.
package cluster

import "strings"

// podmanName returns the name by which kind's Podman provider asks the engine for the node image
// image: docker.io/ in front of a name with no registry host, docker.io/library/ in front of a name
// of one segment, and no tag when the name has a digest.
func podmanName(image string) string {
	const (
		defaultDomain    = "docker.io/"
		officialRepoName = "library"
	)

	var remainder string
	var friendlyImageName string

	if strings.Contains(image, "@sha256:") {
		splits := strings.SplitN(image, "@sha256:", 2)
		friendlyImageName = splits[0]
		remainder = stripTag(splits[0]) + "@sha256:" + splits[1]
	} else {
		friendlyImageName = image
		remainder = image
	}

	if !strings.ContainsRune(remainder, '/') {
		remainder = officialRepoName + "/" + remainder
	}

	i := strings.IndexRune(friendlyImageName, '/')
	if i == -1 || (!strings.ContainsAny(friendlyImageName[:i], ".:") && friendlyImageName[:i] != "localhost") {
		return defaultDomain + remainder
	}
	return remainder
}

// stripTag returns image without its tag, as kind's Podman provider drops it from a name that
// carries a digest.
func stripTag(image string) string {
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon > lastSlash {
		return image[:lastColon]
	}
	return image
}
