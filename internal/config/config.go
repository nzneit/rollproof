// Package config is the run description. It reads rollproof.yaml, rejects what is wrong with
// it, and gives every other unit typed values with defaults filled in and paths made absolute.
// It hides YAML, the ${…} forms and the defaults.
package config

import "time"

// Config is one run description, valid. Path is the file's real absolute path and Dir the
// folder that holds it. SetupWords[i] is the first word of setup[i] as the file writes it, before
// any ${…} form is filled, so that a text can name the command without quoting a value. Every
// other field is one top-level key of the file, as section 8 gives it.
type Config struct {
	Path, Dir  string
	Cluster    Cluster
	Registries []Registry
	Images     map[string]Image
	Setup      [][]string
	SetupWords []string
	Deploy     Deploy
	Probe      Probe
	Limits     Limits
}

// Cluster is the cluster key. An empty image means the tool's default. Workers is 1 or more.
type Cluster struct {
	NodeImage, RegistryImage string
	Workers                  int
}

// Registry is one entry of registries. CA is an absolute path. Password is the value of the one
// environment variable it named.
type Registry struct {
	Host, Username, Password, CA, Mirror string
}

// Image is one entry of images. Exactly one of Local and Archive is set; Archive is an absolute
// path.
type Image struct {
	Local, Archive, As string
}

// Deploy is the deploy key. Exactly one of Helm and Commands is set.
type Deploy struct {
	Namespace, Selector string
	Helm                *Helm
	Commands            *Commands
}

// Helm is the Helm driver's settings. Previous is nil for a run with no previous release.
type Helm struct {
	Release             string
	Previous, Candidate *Release
}

// Release is one release description. IsPath says that Chart is a path that exists; Chart is
// then absolute and Version is empty.
type Release struct {
	Chart, Version string
	IsPath         bool
	Values         []Value
}

// Value is one item of a release's values: a file by absolute path, or an inline map. Exactly
// one is set.
type Value struct {
	File   string
	Inline map[string]any
}

// Commands is the commands driver's settings. Deploy and Rollback are nil when absent.
type Commands struct {
	Install, Deploy, Rollback []string
}

// Probe is the probe key. Exactly one of HTTP and Custom is set.
type Probe struct {
	HTTP   *HTTP
	Custom *Custom
}

// HTTP is the built-in probe's settings, defaults filled in.
type HTTP struct {
	URL                  string
	Headers              map[string]string
	Rate                 int
	Timeout              time.Duration
	ExpectMin, ExpectMax int
}

// Custom is a custom probe's settings. Env entries are as in a pod's container.
type Custom struct {
	Image         string
	Command, Args []string
	Env           []EnvVar
}

// EnvVar is one entry of a custom probe's env: a name with a value, or with a valueFrom that is
// passed to the pod as it is.
type EnvVar struct {
	Name, Value string
	ValueFrom   map[string]any
}

// Limits is the limits key, defaults filled in.
type Limits struct {
	Steady, Settle, Timeout, Silence time.Duration
}
