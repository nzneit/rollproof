package config

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// The texts that reject a host, an as or a mirror of registries. Each quotes no value.
const (
	dockerHubProblem = "write docker.io for Docker Hub"
	toolHostProblem  = "rollproof.test is the tool's own host inside the run"
	notHostProblem   = "is not a registry host as an image's name writes it"
	asPathProblem    = "the path is not a repository path as an image's name writes it"
)

// dnsLabelRe is one label of a DNS name: lower-case letters and digits, with hyphens inside.
var dnsLabelRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

// AsHosts returns the registry hosts that the entries of images name in as, each once, sorted;
// nil when no entry has as. A host is the part of as before its first slash, as written.
func (c *Config) AsHosts() []string {
	seen := map[string]bool{}
	var hosts []string
	for _, im := range c.Images {
		if im.As == "" {
			continue
		}
		host, _, _ := strings.Cut(im.As, "/")
		if !seen[host] {
			seen[host] = true
			hosts = append(hosts, host)
		}
	}
	sort.Strings(hosts)
	return hosts
}

// hostProblem returns the problem with a registry host as section 8 gives its form, as a phrase
// that quotes no value; it is empty when the host has that form. Docker Hub's other names and
// rollproof.test are sound here: the callers that must reject them do.
func hostProblem(host string) string {
	switch {
	case strings.Contains(host, "://"):
		return "must not carry a scheme"
	case strings.Contains(host, "/"):
		return "must not carry a path"
	case strings.Contains(host, "@"):
		return "must not carry a user name or a password"
	case strings.IndexFunc(host, unicode.IsUpper) >= 0:
		return "must be lower case"
	case strings.Count(host, ":") > 1 && !strings.Contains(host, "["):
		return "an IPv6 address must be written in brackets"
	}

	name, port, hasPort := host, "", false
	if strings.HasPrefix(host, "[") {
		end := strings.IndexByte(host, ']')
		if end < 0 {
			return notHostProblem
		}
		name = host[:end+1]
		switch rest := host[end+1:]; {
		case rest == "":
		case strings.HasPrefix(rest, ":"):
			port, hasPort = rest[1:], true
		default:
			return notHostProblem
		}
		ip := net.ParseIP(name[1 : len(name)-1])
		if ip == nil || ip.To4() != nil {
			return notHostProblem
		}
	} else {
		if i := strings.IndexByte(host, ':'); i >= 0 {
			name, port, hasPort = host[:i], host[i+1:], true
		}
		if !dnsName(name) && net.ParseIP(name) == nil {
			return notHostProblem
		}
	}
	if hasPort && !portNumber(port) {
		return "the port must be 1 to 65535"
	}
	if name == "localhost" || strings.Contains(name, ".") || hasPort || strings.HasPrefix(name, "[") {
		return ""
	}
	return "a name with neither a dot nor a port is not a registry host"
}

// dnsName reports whether name is a DNS name: labels of lower-case letters and digits with
// hyphens inside, none of them empty and no dot at the end.
func dnsName(name string) bool {
	for _, label := range strings.Split(name, ".") {
		if !dnsLabelRe.MatchString(label) {
			return false
		}
	}
	return true
}

// portNumber reports whether port has a port's form: digits, 1 to 65535.
func portNumber(port string) bool {
	if port == "" {
		return false
	}
	n := 0
	for _, r := range port {
		if r < '0' || r > '9' {
			return false
		}
		n = n*10 + int(r-'0')
		if n > 65535 {
			return false
		}
	}
	return n > 0
}

// asProblem returns the problem with an as as section 8 gives its form, as a phrase that quotes
// no value; it is empty when the as has that form.
func asProblem(as string) string {
	if strings.IndexFunc(as, unicode.IsUpper) >= 0 {
		return "must be lower case"
	}
	if strings.Contains(as, "@") {
		return "must not carry a digest"
	}
	host, path, hasPath := strings.Cut(as, "/")
	if !hasPath {
		return "must name a repository with its host written out"
	}
	if strings.Contains(path[strings.LastIndex(path, "/")+1:], ":") {
		return "must not carry a tag"
	}
	if problem := hostProblem(host); problem != "" {
		return problem
	}
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if !imageNameRe.MatchString(part) {
			return asPathProblem
		}
	}
	if problem := namedHostProblem(host); problem != "" {
		return problem
	}
	if host == "docker.io" && len(parts) == 1 {
		return "on docker.io the path needs two segments or more; write docker.io/library/NAME"
	}
	return ""
}

// mirrorProblem returns the problem with an entry's mirror as section 8 gives its form, given the
// entry's host, as a phrase that quotes no value; it is empty when the mirror has that form.
func mirrorProblem(mirror, host string) string {
	switch {
	case strings.Contains(mirror, "://"):
		return "must not carry a scheme; HTTPS is implied"
	case strings.Contains(mirror, "@"):
		return "must not carry a user name or a password"
	case strings.Contains(mirror, "?"):
		return "must not carry a query"
	case strings.Contains(mirror, "#"):
		return "must not carry a fragment"
	}
	h, path, hasPath := strings.Cut(mirror, "/")
	if problem := hostProblem(h); problem != "" {
		return problem
	}
	if hasPath {
		for _, segment := range strings.Split(path, "/") {
			switch {
			case segment == "":
				return "the path must have no empty segment and no / at its end"
			case segment == "." || segment == "..":
				return "the path must not have a . or .. segment"
			}
			for _, r := range segment {
				if !mirrorPathRune(r) {
					return "the path may hold only letters, digits, -, ., _ and ~"
				}
			}
		}
	}
	if h == "rollproof.test" {
		return toolHostProblem
	}
	if trimPort443(h) == trimPort443(host) {
		return "names the entry's own host"
	}
	return ""
}

// mirrorPathRune reports whether r may sit in a mirror's path.
func mirrorPathRune(r rune) bool {
	if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
		return true
	}
	return strings.ContainsRune("-._~", r)
}

// trimPort443 takes a port of 443 off a host: only the check of a mirror against its own entry's
// host reads that port as none.
func trimPort443(host string) string { return strings.TrimSuffix(host, ":443") }

// caProblem returns the problem with the certificate file at path, an absolute path, as a phrase
// that quotes no value; it is empty when the file holds a certificate in PEM form. A path that is
// not a regular file is never read, so that a FIFO cannot block the load.
func caProblem(path string) string {
	const noCertificate = "holds no certificate in PEM form"
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "does not exist"
		}
		return "is not a file the tool can read"
	}
	if !fi.Mode().IsRegular() {
		return "is not a file the tool can read"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "is not a file the tool can read"
	}
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			return noCertificate
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		if _, err := x509.ParseCertificate(block.Bytes); err == nil {
			return ""
		}
	}
}

// validateRegistries returns the first rejection of the registries key: each entry's host, its
// mirror and its ca in turn, and a host that an earlier entry already names. It reads the entries
// in the file's order, so that the text names the first one at fault.
func validateRegistries(c *Config, x *index) error {
	seen := map[string]int{}
	for i, r := range c.Registries {
		hostPath := fmt.Sprintf("registries[%d].host", i)
		if r.Host == "" {
			return x.reject(hostPath, "required")
		}
		if problem := hostProblem(r.Host); problem != "" {
			return x.reject(hostPath, problem)
		}
		if problem := namedHostProblem(r.Host); problem != "" {
			return x.reject(hostPath, problem)
		}
		if j, ok := seen[r.Host]; ok {
			return x.reject(hostPath, fmt.Sprintf("the same host as registries[%d]", j))
		}
		seen[r.Host] = i
		if r.Mirror != "" {
			if problem := mirrorProblem(r.Mirror, r.Host); problem != "" {
				return x.reject(fmt.Sprintf("registries[%d].mirror", i), problem)
			}
		}
		if r.CA != "" {
			if problem := caProblem(r.CA); problem != "" {
				return x.reject(fmt.Sprintf("registries[%d].ca", i), problem)
			}
		}
	}
	return nil
}

// namedHostProblem rejects Docker Hub's other names and the tool's own host as written, where a
// host or an as's host may stand. A port makes another host, which is accepted (decision 13).
func namedHostProblem(host string) string {
	switch host {
	case "index.docker.io", "registry-1.docker.io":
		return dockerHubProblem
	case "rollproof.test":
		return toolHostProblem
	}
	return ""
}
