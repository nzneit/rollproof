package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/labels"
)

// absolutePaths makes every path of the file absolute from the file's folder, and finds out
// which charts are paths. Commands start in Dir, so their arguments stay as written.
func absolutePaths(c *Config) {
	abs := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(c.Dir, p)
	}
	for i := range c.Registries {
		c.Registries[i].CA = abs(c.Registries[i].CA)
	}
	for name, im := range c.Images {
		im.Archive = abs(im.Archive)
		c.Images[name] = im
	}
	if c.Deploy.Helm == nil {
		return
	}
	for _, r := range []*Release{c.Deploy.Helm.Previous, c.Deploy.Helm.Candidate} {
		if r == nil {
			continue
		}
		if r.Chart != "" && !strings.HasPrefix(r.Chart, "oci://") {
			if p := filepath.Clean(abs(r.Chart)); exists(p) {
				r.Chart, r.IsPath = p, true
			}
		}
		for i := range r.Values {
			r.Values[i].File = abs(r.Values[i].File)
		}
	}
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

var (
	imageNameRe   = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*$`)
	headerTokenRe = regexp.MustCompile("^[A-Za-z0-9!#$%&'*+.^_`|~-]+$")
)

var reservedNamespaces = map[string]bool{
	"kube-system": true, "kube-public": true, "kube-node-lease": true,
	"local-path-storage": true, "rollproof-probe": true,
}

// validate returns the first rejection of sections 7 and 8, in the order of the design's list.
//
// An image archive that holds more than one image is rejected too (section 8). That check needs
// the file, so the plan that builds `archive` reads it.
func validate(c *Config, x *index) error {
	d := c.Deploy
	if (d.Helm != nil) == (d.Commands != nil) {
		return x.reject("deploy", "exactly one of deploy.helm and deploy.commands")
	}
	if (c.Probe.HTTP != nil) == (c.Probe.Custom != nil) {
		return x.reject("probe", "exactly one of probe.http and probe.custom")
	}

	names := make([]string, 0, len(c.Images))
	for name := range c.Images {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := "images." + name
		im := c.Images[name]
		if !imageNameRe.MatchString(name) {
			return x.reject(path, "the name must be lower-case letters and digits, joined by a dot, one or two underscores, or dashes")
		}
		if (im.Local != "") == (im.Archive != "") {
			return x.reject(path, "exactly one of local and archive")
		}
	}

	if h := d.Helm; h != nil {
		if h.Release == "" {
			return x.reject("deploy.helm.release", "required")
		}
		if h.Candidate == nil {
			return x.reject("deploy.helm.candidate", "required")
		}
		for _, role := range []struct {
			name string
			r    *Release
		}{{"previous", h.Previous}, {"candidate", h.Candidate}} {
			if role.r == nil {
				continue
			}
			if err := validateRelease("deploy.helm."+role.name, role.r, x); err != nil {
				return err
			}
		}
	}
	if k := d.Commands; k != nil {
		if len(k.Install) == 0 {
			return x.reject("deploy.commands.install", "required")
		}
		if len(k.Rollback) > 0 && len(k.Deploy) == 0 {
			return x.reject("deploy.commands.deploy", "required beside deploy.commands.rollback")
		}
	}

	if d.Namespace == "" {
		return x.reject("deploy.namespace", "required")
	}
	if reservedNamespaces[d.Namespace] {
		return x.reject("deploy.namespace", "reserved for the cluster or the probe")
	}
	if _, err := labels.Parse(d.Selector); err != nil {
		return x.reject("deploy.selector", "not a valid label selector")
	}

	if c.Limits.Silence >= c.Limits.Steady {
		return x.reject("limits.silence", "must be shorter than limits.steady")
	}
	if c.Limits.Silence >= c.Limits.Settle {
		return x.reject("limits.silence", "must be shorter than limits.settle")
	}

	if h := c.Probe.HTTP; h != nil {
		if err := validateHTTP(h, c.Limits.Silence, x); err != nil {
			return err
		}
	}
	if cu := c.Probe.Custom; cu != nil && cu.Image == "" {
		return x.reject("probe.custom.image", "required")
	}

	seen := map[string]int{}
	for i, r := range c.Registries {
		path := fmt.Sprintf("registries[%d].host", i)
		if r.Host == "" {
			return x.reject(path, "required")
		}
		if j, ok := seen[r.Host]; ok {
			return x.reject(path, fmt.Sprintf("the same host as registries[%d]", j))
		}
		seen[r.Host] = i
	}
	return nil
}

func validateRelease(path string, r *Release, x *index) error {
	if r.Chart == "" {
		return x.reject(path+".chart", "required")
	}
	switch {
	case r.IsPath && r.Version != "":
		return x.reject(path+".version", "not allowed beside a chart that is a path")
	case !r.IsPath && r.Version == "":
		return x.reject(path+".version", "required for a chart that is not a path")
	}
	for i, v := range r.Values {
		vp := fmt.Sprintf("%s.values[%d]", path, i)
		if v.File == "" && len(v.Inline) == 0 {
			return x.reject(vp, "must not be empty")
		}
		if strings.Contains(v.File, ",") {
			return x.reject(vp, "Helm splits a values file's path at a comma")
		}
	}
	return nil
}

func validateHTTP(h *HTTP, silence time.Duration, x *index) error {
	if h.URL == "" {
		return x.reject("probe.http.url", "required")
	}
	u, err := url.Parse(h.URL)
	if err != nil {
		return x.reject("probe.http.url", "not a URL")
	}
	switch {
	case u.User != nil:
		return x.reject("probe.http.url", "must not carry a user name or a password")
	case u.Scheme != "http" && u.Scheme != "https":
		return x.reject("probe.http.url", "the scheme must be http or https")
	case u.Hostname() == "":
		return x.reject("probe.http.url", "names no host")
	}

	// The probe gets a header as --header NAME=VALUE and splits it at the first "=", so a name
	// that is not a token, or a value with a line break, would send another header than the
	// one written.
	headerNames := make([]string, 0, len(h.Headers))
	for name := range h.Headers {
		headerNames = append(headerNames, name)
	}
	sort.Strings(headerNames)
	for _, name := range headerNames {
		if !headerTokenRe.MatchString(name) {
			return x.reject("probe.http.headers", "a header name must be a token")
		}
		if strings.ContainsAny(h.Headers[name], "\r\n\x00") {
			return x.reject("probe.http.headers."+name, "must not hold a line break or NUL")
		}
	}

	if h.Timeout+time.Second >= silence {
		return x.reject("probe.http.timeout", "plus one second must be shorter than limits.silence")
	}
	return nil
}
