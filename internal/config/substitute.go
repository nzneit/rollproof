package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

var (
	envNameRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	imageFormRe = regexp.MustCompile(`^\$\{images\.([^}]+)\.(digest|repository|ref)\}`)
	passwordKey = regexp.MustCompile(`^registries\[[0-9]+\]\.password$`)
)

// imagesAllowed says whether a ${images.…} form may stand at path: under setup, deploy and probe,
// the strings that Resolve fills.
func imagesAllowed(path string) bool {
	return strings.HasPrefix(path, "setup[") || strings.HasPrefix(path, "deploy.") || strings.HasPrefix(path, "probe.")
}

// expand reads s once, left to right, and returns the text with every ${env.NAME} filled. An
// ${images.…} form is written back unchanged. With keep set, a $${ stays $${ and a value taken
// from the environment has its ${ written as $${, so that Resolve reads the text back to what a
// plain pass gives. The second result is the problem, or "" when there is none; it names a
// variable or an image and never quotes a value.
func expand(s, path string, keep bool, images map[string]fileImage, env func(string) (string, bool)) (out, problem string) {
	var b strings.Builder
	for i := 0; i < len(s); {
		rest := s[i:]
		if strings.HasPrefix(rest, "$${") {
			if keep {
				b.WriteString("$${")
			} else {
				b.WriteString("${")
			}
			i += 3
			continue
		}
		if !strings.HasPrefix(rest, "${") {
			b.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(rest, '}')
		if end < 0 {
			return "", "a ${ with no closing }"
		}
		form := rest[2:end]
		switch {
		case strings.HasPrefix(form, "env.") && envNameRe.MatchString(form[len("env."):]):
			name := form[len("env."):]
			v, ok := env(name)
			switch {
			case !ok:
				return "", "the variable " + name + " is not set"
			case v == "":
				return "", "the variable " + name + " is empty"
			}
			if strings.HasSuffix(v, "$") && strings.HasPrefix(rest[end+1:], "{") {
				return "", "the variable " + name + " ends in $ before {"
			}
			if keep {
				v = strings.ReplaceAll(v, "${", "$${")
			}
			b.WriteString(v)
		case imageFormRe.MatchString(rest) && imageNameRe.MatchString(imageFormRe.FindStringSubmatch(rest)[1]):
			if !imagesAllowed(path) {
				return "", "${images.…} forms are allowed under setup, deploy and probe only"
			}
			name := imageFormRe.FindStringSubmatch(rest)[1]
			if _, ok := images[name]; !ok {
				return "", "images has no entry " + name
			}
			b.WriteString(rest[:end+1])
		default:
			return "", "a ${…} form that this tool does not know; write $${ for a literal ${"
		}
		i += end + 1
	}
	return b.String(), ""
}

// substitute fills every ${env.NAME} form in the string values of root and checks every
// ${images.…} form, which it leaves for Resolve. Keys are never read as values, and a scalar that
// YAML typed as a number or a boolean is left alone. It stops at the first rejection.
func substitute(root *yaml.Node, images map[string]fileImage, env func(string) (string, bool), x *index) error {
	if env == nil {
		env = func(string) (string, bool) { return "", false }
	}
	var first error
	walk(root, "", func(path string, _, v *yaml.Node) {
		if first != nil || v.Kind != yaml.ScalarNode {
			return
		}
		if passwordKey.MatchString(path) {
			if v.Value != "" && !passwordForm.MatchString(v.Value) {
				first = x.reject(path, "must be one ${env.NAME} form")
				return
			}
		}
		if v.ShortTag() != "!!str" {
			return
		}
		out, problem := expand(v.Value, path, false, images, env)
		if problem == "" && imagesAllowed(path) && (strings.Contains(out, "${images.") || strings.Contains(out, "$${")) {
			// Resolve reads this value, so a literal that it could read as an escape or a form
			// has to be written in the form that Resolve reads back to the same text.
			out, problem = expand(v.Value, path, true, images, env)
		}
		if problem != "" {
			first = x.reject(path, problem)
			return
		}
		v.Value = out
	})
	return first
}

// ImageRef is where one image of the run can be pulled from: its repository, host included,
// and its digest with the sha256: prefix.
type ImageRef struct{ Repository, Digest string }

// Ref returns the repository and the digest joined by @.
func (r ImageRef) Ref() string { return r.Repository + "@" + r.Digest }

// fill reads s once, left to right: $${ writes ${ and an images form is replaced by its value.
func fill(s string, images map[string]ImageRef) (string, error) {
	if !strings.Contains(s, "${") {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		rest := s[i:]
		if strings.HasPrefix(rest, "$${") {
			b.WriteString("${")
			i += 3
			continue
		}
		if m := imageFormRe.FindStringSubmatch(rest); m != nil {
			im, ok := images[m[1]]
			if !ok {
				return "", fmt.Errorf("the configuration uses the image %s, which has no stored image", m[1])
			}
			switch m[2] {
			case "digest":
				b.WriteString(im.Digest)
			case "repository":
				b.WriteString(im.Repository)
			default:
				b.WriteString(im.Ref())
			}
			i += len(m[0])
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String(), nil
}

// Resolve fills every ${images.NAME.digest}, .repository and .ref form under setup, deploy and
// probe. images must have every name the forms use; a missing one is an error, and then nothing
// is filled. After Resolve no form is left. Call it once per run: it reads a $${ that Load kept
// as a literal ${, so a second call would read that literal again.
func (c *Config) Resolve(images map[string]ImageRef) error {
	var first error
	c.transform(func(s string) string {
		if _, err := fill(s, images); err != nil && first == nil {
			first = err
		}
		return s
	})
	if first != nil {
		return first
	}
	c.transform(func(s string) string {
		out, _ := fill(s, images)
		return out
	})
	return nil
}

// transform replaces every string that Resolve fills by tr of it.
func (c *Config) transform(tr func(string) string) {
	list := func(l []string) {
		for i := range l {
			l[i] = tr(l[i])
		}
	}
	for _, cmd := range c.Setup {
		list(cmd)
	}
	d := &c.Deploy
	d.Namespace, d.Selector = tr(d.Namespace), tr(d.Selector)
	if h := d.Helm; h != nil {
		h.Release = tr(h.Release)
		for _, r := range []*Release{h.Previous, h.Candidate} {
			if r == nil {
				continue
			}
			r.Chart, r.Version = tr(r.Chart), tr(r.Version)
			for i := range r.Values {
				r.Values[i].File = tr(r.Values[i].File)
				if r.Values[i].Inline != nil {
					r.Values[i].Inline = deep(r.Values[i].Inline, tr).(map[string]any)
				}
			}
		}
	}
	if k := d.Commands; k != nil {
		list(k.Install)
		list(k.Deploy)
		list(k.Rollback)
	}
	if h := c.Probe.HTTP; h != nil {
		h.URL = tr(h.URL)
		for name, v := range h.Headers {
			h.Headers[name] = tr(v)
		}
	}
	if cu := c.Probe.Custom; cu != nil {
		cu.Image = tr(cu.Image)
		list(cu.Command)
		list(cu.Args)
		for i := range cu.Env {
			e := &cu.Env[i]
			e.Name, e.Value = tr(e.Name), tr(e.Value)
			if e.ValueFrom != nil {
				e.ValueFrom = deep(e.ValueFrom, tr).(map[string]any)
			}
		}
	}
}

// deep returns v with tr applied to every string in it, at any depth. Keys stay as they are.
func deep(v any, tr func(string) string) any {
	switch t := v.(type) {
	case string:
		return tr(t)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = deep(e, tr)
		}
		return out
	case map[any]any:
		out := make(map[any]any, len(t))
		for k, e := range t {
			out[k] = deep(e, tr)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = deep(e, tr)
		}
		return out
	}
	return v
}

// Setting is one setting as text for the report: its key as the file spells it, its value, and
// its default.
type Setting struct{ Key, Value, Default string }

const (
	defaultNodeImageText     = "the linked kind release's"
	defaultRegistryImageText = "the tool's pinned registry image"
)

// Settings returns every setting of cluster, probe.http and limits with its value and its
// default, in a fixed order. It never holds a header's value, a password or a command.
func (c *Config) Settings() []Setting {
	orText := func(v, def string) string {
		if v == "" {
			return def
		}
		return v
	}
	out := []Setting{
		{"cluster.node-image", orText(c.Cluster.NodeImage, defaultNodeImageText), defaultNodeImageText},
		{"cluster.registry-image", orText(c.Cluster.RegistryImage, defaultRegistryImageText), defaultRegistryImageText},
		{"cluster.workers", strconv.Itoa(c.Cluster.Workers), strconv.Itoa(defaultWorkers)},
	}
	if h := c.Probe.HTTP; h != nil {
		out = append(out,
			Setting{"probe.http.rate", strconv.Itoa(h.Rate), strconv.Itoa(defaultRate)},
			Setting{"probe.http.timeout", formatDuration(h.Timeout), formatDuration(defaultHTTPTimeout)},
			Setting{"probe.http.expect-status", formatStatus(h.ExpectMin, h.ExpectMax), formatStatus(defaultExpectMin, defaultExpectMax)},
		)
	}
	l := c.Limits
	return append(out,
		Setting{"limits.steady", formatDuration(l.Steady), formatDuration(defaultSteady)},
		Setting{"limits.settle", formatDuration(l.Settle), formatDuration(defaultSettle)},
		Setting{"limits.timeout", formatDuration(l.Timeout), formatDuration(defaultTimeout)},
		Setting{"limits.silence", formatDuration(l.Silence), formatDuration(defaultSilence)},
	)
}

func formatStatus(lo, hi int) string {
	if lo == hi {
		return strconv.Itoa(lo)
	}
	return strconv.Itoa(lo) + "-" + strconv.Itoa(hi)
}

// formatDuration writes whole minutes as Nm, else whole seconds as Ns, else Nms.
func formatDuration(d time.Duration) string {
	switch {
	case d%time.Minute == 0:
		return strconv.FormatInt(int64(d/time.Minute), 10) + "m"
	case d%time.Second == 0:
		return strconv.FormatInt(int64(d/time.Second), 10) + "s"
	}
	return strconv.FormatInt(d.Milliseconds(), 10) + "ms"
}
