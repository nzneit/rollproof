//go:build linux

package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/nzneit/rollproof/internal/config"
	"github.com/nzneit/rollproof/internal/verdict"
)

// CheckHelm checks that a helm command is on the PATH. An error is a Condition with
// RunnerUnsupported.
func CheckHelm() error {
	if _, err := exec.LookPath("helm"); err != nil {
		return verdict.Condition{Code: verdict.RunnerUnsupported, Text: "helm is not on the PATH"}
	}
	return nil
}

// HelmOptions is what the Helm driver needs: the two releases, the test cluster as Helm's
// command line names it, the run's folder for fetched charts and written
// values, the configuration's folder, and limits.timeout.
type HelmOptions struct {
	Helm                                        config.Helm
	Namespace, Kubeconfig, Context, RunDir, Dir string
	Timeout                                     time.Duration
	Runner                                      Runner
}

// HelmDriver is the Helm driver. It implements Deployer.
type HelmDriver struct {
	o         HelmOptions
	previous  *fetchedRelease
	candidate *fetchedRelease
	ready     bool
	revision  int
	// installed is true once Install has read the revision.
	installed bool
}

// fetchedRelease is a release after Fetch: the chart reference for Helm, the
// --values arguments, and what the report says.
type fetchedRelease struct {
	ref     string
	values  []string
	release Release
}

// NewHelm returns the Helm driver for o. With no previous release in o.Helm, Fetch
// fetches the candidate only, Install installs the candidate, Transitions is
// restart alone, and Releases is the candidate alone; Deploy and Rollback
// then panic, because run never calls them for a restart. With a previous
// release it fetches both, installs the previous and gives deploy and rollback.
func NewHelm(o HelmOptions) *HelmDriver {
	return &HelmDriver{o: o}
}

func (d *HelmDriver) cl() []string {
	return []string{"--namespace", d.o.Namespace, "--kubeconfig", d.o.Kubeconfig, "--kube-context", d.o.Context}
}

func (d *HelmDriver) run(ctx context.Context, name string, args ...string) Result {
	return d.o.Runner.Run(ctx, Command{Name: name, Args: slices.Concat([]string{"helm"}, args), Timeout: d.o.Timeout})
}

// Fetch pulls and reads both charts and writes the inline values; with no
// previous release it reads the candidate alone. It stops at the first error.
func (d *HelmDriver) Fetch(ctx context.Context) error {
	d.ready = false
	if !filepath.IsAbs(d.o.RunDir) {
		return verdict.Condition{Code: verdict.InternalError, Text: "the run folder must be an absolute path"}
	}
	// It removes <RunDir>/charts/<role> below, so the folder must be the run's own, as the
	// cluster's clean-up requires of the folder it removes.
	if !strings.HasPrefix(filepath.Base(d.o.RunDir), "rollproof-") {
		return verdict.Condition{Code: verdict.InternalError, Text: "the run folder's name must begin with rollproof-"}
	}
	var prev *fetchedRelease
	if d.o.Helm.Previous != nil {
		var err error
		prev, err = d.fetch(ctx, "previous", d.o.Helm.Previous)
		if err != nil {
			return err
		}
	}
	cand, err := d.fetch(ctx, "candidate", d.o.Helm.Candidate)
	if err != nil {
		return err
	}
	d.previous, d.candidate, d.ready = prev, cand, true
	return nil
}

func chartFailure(verb, role, chart string) error {
	return verdict.Condition{Code: verdict.ChartFetchFailed, Text: fmt.Sprintf(
		"could not %s the %s release's chart %s; the command's output is in the evidence", verb, role, chart)}
}

func (d *HelmDriver) fetch(ctx context.Context, role string, r *config.Release) (*fetchedRelease, error) {
	ref := r.Chart
	if !r.IsPath {
		dest := filepath.Join(d.o.RunDir, "charts", role)
		// The folder is emptied first, so that a second Fetch finds only the archive it pulled;
		// the listing below uses os.ReadDir, not a glob, because a run folder's name may hold a
		// pattern character.
		if err := os.RemoveAll(dest); err != nil {
			return nil, fmt.Errorf("fetch the %s chart: empty its folder: %w", role, err)
		}
		if err := os.MkdirAll(dest, 0o700); err != nil {
			return nil, fmt.Errorf("fetch the %s chart: make its folder: %w", role, err)
		}
		res := d.run(ctx, "pull-"+role, "pull", r.Chart, "--version", r.Version, "--destination", dest)
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("fetch the %s chart: %w", role, err)
		}
		if res.Ending != Succeeded {
			return nil, chartFailure("pull", role, r.Chart)
		}
		entries, err := os.ReadDir(dest)
		if err != nil || len(entries) != 1 || entries[0].IsDir() || !strings.HasSuffix(entries[0].Name(), ".tgz") {
			return nil, chartFailure("pull", role, r.Chart)
		}
		ref = filepath.Join(dest, entries[0].Name())
	}
	res := d.run(ctx, "show-"+role, "show", "chart", ref)
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("fetch the %s chart: %w", role, err)
	}
	var meta struct{ Name, Version string }
	if res.Ending != Succeeded || yaml.Unmarshal([]byte(res.Stdout), &meta) != nil || meta.Name == "" || meta.Version == "" {
		return nil, chartFailure("read", role, r.Chart)
	}

	f := &fetchedRelease{ref: ref, release: Release{Role: role, Chart: meta.Name, Version: meta.Version, Source: d.source(r)}}
	keys := map[string]bool{}
	for i, v := range r.Values {
		if v.File != "" {
			f.values = append(f.values, "--values", v.File)
			f.release.ValueFiles = append(f.release.ValueFiles, filepath.Base(v.File))
			continue
		}
		path, err := d.writeValues(role, i+1, v.Inline)
		if err != nil {
			return nil, err
		}
		f.values = append(f.values, "--values", path)
		for k := range v.Inline {
			keys[k] = true
		}
	}
	for k := range keys {
		f.release.InlineKeys = append(f.release.InlineKeys, k)
	}
	slices.Sort(f.release.InlineKeys)
	return f, nil
}

// writeValues writes one inline map as a values file. The error never holds a value.
func (d *HelmDriver) writeValues(role string, index int, m map[string]any) (string, error) {
	dir := filepath.Join(d.o.RunDir, "values")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("fetch the %s chart: make the values folder: %w", role, err)
	}
	text, err := yaml.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("fetch the %s chart: the inline values of item %d cannot be written as YAML", role, index)
	}
	path := filepath.Join(dir, role+"-"+strconv.Itoa(index)+".yaml")
	if err := os.WriteFile(path, text, 0o600); err != nil {
		return "", fmt.Errorf("fetch the %s chart: write the values file %s: %w", role, filepath.Base(path), err)
	}
	return path, nil
}

// source is where a chart came from, as the report says it.
func (d *HelmDriver) source(r *config.Release) string {
	if !r.IsPath {
		return r.Chart
	}
	rel, err := filepath.Rel(d.o.Dir, r.Chart)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return r.Chart
	}
	if rel == "." {
		return "."
	}
	return "./" + rel
}

// statusStdoutCap is how much of the status's JSON is kept: the JSON holds the release's
// values and manifest, which can be large.
const statusStdoutCap = 64 << 20

// Install installs the previous release, or the candidate when there is no
// previous release, then reads the revision it made.
func (d *HelmDriver) Install(ctx context.Context) Result {
	if !d.ready {
		panic("deploy: Install called before a successful Fetch")
	}
	release := d.previous
	if release == nil {
		release = d.candidate
	}
	args := slices.Concat([]string{"upgrade", "--install", d.o.Helm.Release, release.ref}, d.cl(),
		[]string{"--timeout", d.o.Timeout.String()}, release.values)
	res := d.run(ctx, "install", args...)
	if res.Ending != Succeeded {
		return res
	}
	st := d.o.Runner.Run(ctx, Command{Name: "status-install", Timeout: d.o.Timeout, MaxStdout: statusStdoutCap,
		Args: slices.Concat([]string{"helm", "status", d.o.Helm.Release}, d.cl(), []string{"-o", "json"})})
	if st.Ending != Succeeded {
		return st
	}
	var status struct {
		Version *int `json:"version"`
	}
	if json.Unmarshal([]byte(st.Stdout), &status) != nil || status.Version == nil || *status.Version < 1 {
		// An unreadable status ends the install like a command that did not end well: the run
		// has one handler for such a command.
		st.Ending = ExitedNonZero
		return st
	}
	d.revision, d.installed = *status.Version, true
	return res
}

// Deploy upgrades the release to the candidate with the chart's own values reset.
func (d *HelmDriver) Deploy(ctx context.Context) Result {
	if d.o.Helm.Previous == nil {
		panic("deploy: Deploy called on a Helm driver with no previous release")
	}
	if !d.installed {
		panic("deploy: Deploy called before a successful Install")
	}
	args := slices.Concat([]string{"upgrade", d.o.Helm.Release, d.candidate.ref, "--reset-values"}, d.cl(),
		[]string{"--timeout", d.o.Timeout.String()}, d.candidate.values)
	return d.run(ctx, "deploy", args...)
}

// Rollback rolls the release back to the revision that Install made.
func (d *HelmDriver) Rollback(ctx context.Context) Result {
	if d.o.Helm.Previous == nil {
		panic("deploy: Rollback called on a Helm driver with no previous release")
	}
	if !d.installed {
		panic("deploy: Rollback called before a successful Install")
	}
	args := slices.Concat([]string{"rollback", d.o.Helm.Release, strconv.Itoa(d.revision)}, d.cl(),
		[]string{"--timeout", d.o.Timeout.String()})
	return d.run(ctx, "rollback", args...)
}

// Transitions are the windows of this driver: deploy, then rollback; restart
// alone when there is no previous release.
func (d *HelmDriver) Transitions() []verdict.WindowName {
	if d.o.Helm.Previous == nil {
		return []verdict.WindowName{verdict.Restart}
	}
	return []verdict.WindowName{verdict.Deploy, verdict.Rollback}
}

// Releases are the previous and then the candidate release, as Fetch kept them,
// or the candidate alone when there is no previous release; Program is empty.
func (d *HelmDriver) Releases() []Release {
	if !d.ready {
		return nil
	}
	if d.previous == nil {
		return []Release{d.candidate.release}
	}
	return []Release{d.previous.release, d.candidate.release}
}
