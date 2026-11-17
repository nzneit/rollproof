package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"rollproof/internal/verdict"
)

const (
	defaultWorkers     = 3
	defaultRate        = 200
	defaultHTTPTimeout = 2 * time.Second
	defaultExpectMin   = 200
	defaultExpectMax   = 299
	defaultSteady      = 10 * time.Second
	defaultSettle      = 15 * time.Second
	defaultTimeout     = 5 * time.Minute
	defaultSilence     = 5 * time.Second
)

type fileConfig struct {
	Version    yaml.Node            `yaml:"version"`
	Cluster    fileCluster          `yaml:"cluster"`
	Registries []fileRegistry       `yaml:"registries"`
	Images     map[string]fileImage `yaml:"images"`
	Setup      [][]string           `yaml:"setup"`
	Deploy     fileDeploy           `yaml:"deploy"`
	Probe      fileProbe            `yaml:"probe"`
	Limits     fileLimits           `yaml:"limits"`
}

type fileCluster struct {
	NodeImage     string `yaml:"node-image"`
	RegistryImage string `yaml:"registry-image"`
	Workers       *int   `yaml:"workers"`
}

type fileRegistry struct {
	Host     string `yaml:"host"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	CA       string `yaml:"ca"`
	Mirror   string `yaml:"mirror"`
}

type fileImage struct {
	Local   string `yaml:"local"`
	Archive string `yaml:"archive"`
	As      string `yaml:"as"`
}

type fileDeploy struct {
	Namespace string        `yaml:"namespace"`
	Selector  string        `yaml:"selector"`
	Helm      *fileHelm     `yaml:"helm"`
	Commands  *fileCommands `yaml:"commands"`
}

type fileHelm struct {
	Release   string       `yaml:"release"`
	Previous  *fileRelease `yaml:"previous"`
	Candidate *fileRelease `yaml:"candidate"`
}

type fileRelease struct {
	Chart   string       `yaml:"chart"`
	Version chartVersion `yaml:"version"`
	Values  []valueItem  `yaml:"values"`
}

type fileCommands struct {
	Install  []string `yaml:"install"`
	Deploy   []string `yaml:"deploy"`
	Rollback []string `yaml:"rollback"`
}

type fileProbe struct {
	HTTP   *fileHTTP   `yaml:"http"`
	Custom *fileCustom `yaml:"custom"`
}

type fileHTTP struct {
	URL          string            `yaml:"url"`
	Headers      map[string]string `yaml:"headers"`
	Rate         *int              `yaml:"rate"`
	Timeout      *duration         `yaml:"timeout"`
	ExpectStatus *status           `yaml:"expect-status"`
}

type fileCustom struct {
	Image   string       `yaml:"image"`
	Command []string     `yaml:"command"`
	Args    []string     `yaml:"args"`
	Env     []fileEnvVar `yaml:"env"`
}

type fileEnvVar struct {
	Name      string         `yaml:"name"`
	Value     *string        `yaml:"value"`
	ValueFrom map[string]any `yaml:"valueFrom"`
}

type fileLimits struct {
	Steady  *duration `yaml:"steady"`
	Settle  *duration `yaml:"settle"`
	Timeout *duration `yaml:"timeout"`
	Silence *duration `yaml:"silence"`
}

// leafError is a value that one of the four value types cannot read. The decoder hands it back
// unchanged, and decodeError turns it into a rejection.
type leafError struct {
	line, column int
	problem      string
}

func (e *leafError) Error() string { return fmt.Sprintf("line %d: %s", e.line, e.problem) }

func leaf(n *yaml.Node, problem string) error {
	return &leafError{line: n.Line, column: n.Column, problem: problem}
}

var durationRe = regexp.MustCompile(`^([0-9]+)(ms|s|m)$`)

// duration is a whole number of milliseconds, seconds or minutes, more than zero.
type duration time.Duration

func (d *duration) UnmarshalYAML(n *yaml.Node) error {
	const problem = "must be a whole number followed by ms, s or m, more than zero"
	if n.Kind != yaml.ScalarNode {
		return leaf(n, problem)
	}
	m := durationRe.FindStringSubmatch(n.Value)
	if m == nil {
		return leaf(n, problem)
	}
	count, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || count <= 0 {
		return leaf(n, problem)
	}
	unit := map[string]time.Duration{"ms": time.Millisecond, "s": time.Second, "m": time.Minute}[m[2]]
	if count > math.MaxInt64/int64(unit) {
		return leaf(n, problem)
	}
	*d = duration(time.Duration(count) * unit)
	return nil
}

var statusRe = regexp.MustCompile(`^([0-9]{3})(?:-([0-9]{3}))?$`)

// status is one status code or a range of them, from 100 to 599.
type status struct{ min, max int }

func (s *status) UnmarshalYAML(n *yaml.Node) error {
	const problem = "must be one status code from 100 to 599, or a range MIN-MAX of them"
	if n.Kind != yaml.ScalarNode {
		return leaf(n, problem)
	}
	m := statusRe.FindStringSubmatch(n.Value)
	if m == nil {
		return leaf(n, problem)
	}
	lo, _ := strconv.Atoi(m[1]) // three digits: cannot fail
	hi := lo
	if m[2] != "" {
		hi, _ = strconv.Atoi(m[2])
	}
	if lo < 100 || hi > 599 || lo > hi {
		return leaf(n, problem)
	}
	*s = status{min: lo, max: hi}
	return nil
}

// chartVersion is a chart version that the file wrote as a string.
type chartVersion string

func (v *chartVersion) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return leaf(n, "must be a string in quotes")
	}
	switch tag := n.ShortTag(); {
	case tag == "!!int" || tag == "!!float":
		return leaf(n, "YAML reads this as a number; write the version in quotes")
	case tag != "!!str":
		return leaf(n, "must be a string in quotes")
	case n.Value == "":
		return leaf(n, "must not be empty")
	}
	*v = chartVersion(n.Value)
	return nil
}

// valueItem is one item of a release's values: a file path or an inline map.
type valueItem struct {
	file   string
	inline map[string]any
}

func (v *valueItem) UnmarshalYAML(n *yaml.Node) error {
	const problem = "must be a file path or a map"
	switch {
	case n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" && n.Value != "":
		v.file = n.Value
	case n.Kind == yaml.MappingNode:
		var m map[string]any
		if err := n.Decode(&m); err != nil {
			return leaf(n, problem)
		}
		v.inline = m
	default:
		return leaf(n, problem)
	}
	return nil
}

// walk visits every value node under n with its key path, depth first. A mapping's value has
// the parent's path, a dot and the key (no dot at the top); a sequence item has the parent's
// path and [i], and its key is nil.
func walk(n *yaml.Node, path string, visit func(path string, key, value *yaml.Node)) {
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			walk(c, path, visit)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			p := k.Value
			if path != "" {
				p = path + "." + k.Value
			}
			visit(p, k, v)
			walk(v, p, visit)
		}
	case yaml.SequenceNode:
		for i, v := range n.Content {
			p := path + "[" + strconv.Itoa(i) + "]"
			visit(p, nil, v)
			walk(v, p, visit)
		}
	}
}

type entry struct {
	path         string
	line, column int
	tag          string
	value        string
	keyLine      int
	keyText      string
	hasKey       bool
}

// index finds the key path and the line of a node of the file.
type index struct {
	entries []entry
	byPath  map[string]int
}

func newIndex(root *yaml.Node) *index {
	x := &index{byPath: map[string]int{}}
	walk(root, "", func(path string, key, value *yaml.Node) {
		e := entry{path: path, line: value.Line, column: value.Column, tag: value.ShortTag(), value: value.Value}
		if key != nil {
			e.hasKey, e.keyLine, e.keyText = true, key.Line, key.Value
		}
		if _, dup := x.byPath[path]; !dup {
			x.byPath[path] = len(x.entries)
		}
		x.entries = append(x.entries, e)
	})
	return x
}

func rejection(text string) error {
	return verdict.Condition{Code: verdict.ConfigRejected, Text: text}
}

// reject returns the rejection "PATH (line L): PROBLEM", or "PATH: PROBLEM" when the path is not
// in the file.
func (x *index) reject(path, problem string) error {
	i, ok := x.byPath[path]
	if !ok {
		return rejection(path + ": " + problem)
	}
	e := x.entries[i]
	line := e.line
	if e.hasKey {
		line = e.keyLine
	}
	return rejection(fmt.Sprintf("%s (line %d): %s", path, line, problem))
}

var (
	typeErrLineRe = regexp.MustCompile(`^line (\d+): (.*)$`)
	unknownKeyRe  = regexp.MustCompile(`^field (.+?) not found in type `)
	unmarshalRe   = regexp.MustCompile(`^cannot unmarshal (!![a-z]+) `)
)

// decodeError turns an error of the strict decode into a rejection. It never uses the decoder's
// text, which quotes values.
func (x *index) decodeError(err error) error {
	var le *leafError
	if errors.As(err, &le) {
		for _, e := range x.entries {
			if e.line == le.line && e.column == le.column {
				return x.reject(e.path, le.problem)
			}
		}
		return rejection(fmt.Sprintf("line %d: %s", le.line, le.problem))
	}
	var te *yaml.TypeError
	if !errors.As(err, &te) || len(te.Errors) == 0 {
		return rejection("the configuration could not be decoded")
	}
	m := typeErrLineRe.FindStringSubmatch(te.Errors[0])
	if m == nil {
		return rejection("the configuration could not be decoded")
	}
	line, _ := strconv.Atoi(m[1])
	rest := m[2]
	if k := unknownKeyRe.FindStringSubmatch(rest); k != nil {
		for _, e := range x.entries {
			if e.hasKey && e.keyLine == line && e.keyText == k[1] {
				return x.reject(e.path, "unknown key")
			}
		}
		return rejection(fmt.Sprintf("line %d: unknown key", line))
	}
	if t := unmarshalRe.FindStringSubmatch(rest); t != nil {
		goType := rest[strings.LastIndex(rest, " into ")+len(" into "):]
		problem := "has the wrong type"
		switch {
		case goType == "int":
			problem = "must be a whole number"
		case strings.HasPrefix(goType, "[]"):
			problem = "must be a list"
		case strings.HasPrefix(goType, "map[") || strings.HasPrefix(goType, "config."):
			problem = "must be a map"
		case goType == "string":
			problem = "must be a single value"
		}
		quoted, hasQuote := quotedText(rest, t[1])
		best, bestAny := -1, -1
		for i, e := range x.entries {
			if e.line != line || e.tag != t[1] {
				continue
			}
			if bestAny < 0 || e.column < x.entries[bestAny].column {
				bestAny = i
			}
			if hasQuote && matchesQuoted(e.value, quoted) && (best < 0 || e.column < x.entries[best].column) {
				best = i
			}
		}
		if best < 0 {
			best = bestAny
		}
		if best >= 0 {
			return x.reject(x.entries[best].path, problem)
		}
		return rejection(fmt.Sprintf("line %d: %s", line, problem))
	}
	if strings.Contains(rest, "already defined") {
		return rejection(fmt.Sprintf("line %d: a key appears twice in one map", line))
	}
	return rejection(fmt.Sprintf("line %d: rejected", line))
}

var syntaxLineRe = regexp.MustCompile(`line (\d+)`)

// Load reads and validates the file at path. env looks up an environment variable. An error is
// a verdict.Condition with ConfigRejected whose text names the key and never quotes a value.
func Load(path string, env func(name string) (string, bool)) (*Config, error) {
	real, err := filepath.Abs(path)
	if err == nil {
		real, err = filepath.EvalSymlinks(real)
	}
	var data []byte
	if err == nil {
		data, err = os.ReadFile(real)
	}
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, rejection("the configuration file " + path + " does not exist")
		}
		return nil, rejection("the configuration file " + path + " cannot be read")
	}

	var root yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&root); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, rejection("the configuration file " + path + " is empty")
		}
		text := "the configuration file " + path + " is not valid YAML"
		if m := syntaxLineRe.FindStringSubmatch(err.Error()); m != nil {
			text += " (line " + m[1] + ")"
		}
		return nil, rejection(text)
	}
	if err := dec.Decode(new(yaml.Node)); !errors.Is(err, io.EOF) {
		return nil, rejection("the configuration file " + path + " holds more than one YAML document")
	}
	if len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return nil, rejection("the configuration file " + path + " does not hold a map of keys")
	}

	x := newIndex(&root)
	var f fileConfig
	strict := yaml.NewDecoder(bytes.NewReader(data))
	strict.KnownFields(true)
	if err := strict.Decode(&f); err != nil {
		return nil, x.decodeError(err)
	}
	c, err := convert(&f, x)
	if err != nil {
		return nil, err
	}
	c.Path, c.Dir = real, filepath.Dir(real)
	return c, nil
}

func convert(f *fileConfig, x *index) (*Config, error) {
	switch v := &f.Version; {
	case v.Kind == 0:
		return nil, x.reject("version", "required; this tool reads version 1")
	case v.Kind != yaml.ScalarNode || v.ShortTag() != "!!int" || v.Value != "1":
		return nil, x.reject("version", "this tool reads version 1 only")
	}

	c := &Config{
		Cluster: Cluster{NodeImage: f.Cluster.NodeImage, RegistryImage: f.Cluster.RegistryImage, Workers: defaultWorkers},
		Setup:   f.Setup,
		Deploy:  Deploy{Namespace: f.Deploy.Namespace, Selector: f.Deploy.Selector},
		Limits: Limits{
			Steady: orDefault(f.Limits.Steady, defaultSteady), Settle: orDefault(f.Limits.Settle, defaultSettle),
			Timeout: orDefault(f.Limits.Timeout, defaultTimeout), Silence: orDefault(f.Limits.Silence, defaultSilence),
		},
	}
	if w := f.Cluster.Workers; w != nil {
		if *w < 1 {
			return nil, x.reject("cluster.workers", "must be 1 or more")
		}
		c.Cluster.Workers = *w
	}

	for i, r := range f.Registries {
		if (r.Username == "") != (r.Password == "") {
			return nil, x.reject(fmt.Sprintf("registries[%d]", i), "username and password come together or not at all")
		}
		c.Registries = append(c.Registries, Registry{Host: r.Host, Username: r.Username, Password: r.Password, CA: r.CA, Mirror: r.Mirror})
	}
	if f.Images != nil {
		c.Images = make(map[string]Image, len(f.Images))
		for name, im := range f.Images {
			c.Images[name] = Image{Local: im.Local, Archive: im.Archive, As: im.As}
		}
	}

	if h := f.Deploy.Helm; h != nil {
		c.Deploy.Helm = &Helm{Release: h.Release, Previous: release(h.Previous), Candidate: release(h.Candidate)}
	}
	if k := f.Deploy.Commands; k != nil {
		c.Deploy.Commands = &Commands{Install: k.Install, Deploy: k.Deploy, Rollback: k.Rollback}
	}

	if h := f.Probe.HTTP; h != nil {
		out := &HTTP{URL: h.URL, Headers: h.Headers, Rate: defaultRate, Timeout: orDefault(h.Timeout, defaultHTTPTimeout),
			ExpectMin: defaultExpectMin, ExpectMax: defaultExpectMax}
		if h.Rate != nil {
			if *h.Rate < 1 {
				return nil, x.reject("probe.http.rate", "must be 1 or more")
			}
			out.Rate = *h.Rate
		}
		if h.ExpectStatus != nil {
			out.ExpectMin, out.ExpectMax = h.ExpectStatus.min, h.ExpectStatus.max
		}
		c.Probe.HTTP = out
	}
	if cu := f.Probe.Custom; cu != nil {
		out := &Custom{Image: cu.Image, Command: cu.Command, Args: cu.Args}
		for i, e := range cu.Env {
			path := fmt.Sprintf("probe.custom.env[%d]", i)
			if e.Name == "" {
				return nil, x.reject(path, "needs a name")
			}
			if (e.Value != nil) == (e.ValueFrom != nil) {
				return nil, x.reject(path, "needs exactly one of value and valueFrom")
			}
			v := EnvVar{Name: e.Name, ValueFrom: e.ValueFrom}
			if e.Value != nil {
				v.Value = *e.Value
			}
			out.Env = append(out.Env, v)
		}
		c.Probe.Custom = out
	}
	return c, nil
}

func release(r *fileRelease) *Release {
	if r == nil {
		return nil
	}
	out := &Release{Chart: r.Chart, Version: string(r.Version)}
	for _, v := range r.Values {
		out.Values = append(out.Values, Value{File: v.file, Inline: v.inline})
	}
	return out
}

func orDefault(d *duration, def time.Duration) time.Duration {
	if d == nil {
		return def
	}
	return time.Duration(*d)
}

// quotedText returns the text that the decoder quoted between backticks after the tag in a
// "cannot unmarshal" entry, and false when it quoted none (a map or a sequence). It is used only
// to find the node, never written to a text.
func quotedText(rest, tag string) (string, bool) {
	start := strings.Index(rest, tag+" `")
	end := strings.LastIndex(rest, "` into ")
	if start < 0 || end < 0 {
		return "", false
	}
	start += len(tag) + len(" `")
	if end < start {
		return "", false
	}
	return rest[start:end], true
}

// matchesQuoted says whether a node's value is what the decoder quoted: the whole value when it
// has 10 bytes or fewer, otherwise its first 7 bytes and "...".
func matchesQuoted(value, quoted string) bool {
	if len(value) > 10 {
		return quoted == value[:7]+"..."
	}
	return quoted == value
}
