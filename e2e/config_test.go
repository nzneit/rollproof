package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/nzneit/rollproof/internal/config"
)

// fixture is one row of section 12: the chart values of each release, the
// cluster's size, the limits that differ from the defaults, which Helm
// binary, and what the later rows change.
//
// The fields after helm: cluster.node-image ("" is kind's default); a
// Helm run with no previous release; a candidate whose image values name the
// previous image; the setup commands; the commands driver's roles in order
// (nil is the Helm driver); a custom probe (nil is the built-in one); an
// action on the running run; a run that is to leave its cluster behind.
//
// registries is the run's registries block, left out when empty. images,
// when not nil, is the images block as it is, an empty map included. literal
// gives, by role (previous or candidate), a release image named outright in
// place of the images entry's forms.
type fixture struct {
	name                              string
	previous, candidate               map[string]any
	workers                           int
	limits                            map[string]string
	helm                              string
	nodeImage                         string
	noPrevious, candidateRunsPrevious bool
	setup                             [][]string
	commands                          []string
	custom                            *customProbe
	act                               action
	keepsCluster                      bool
	registries                        []map[string]string
	images                            map[string]map[string]string
	literal                           map[string]literalImage
	// github gives the run its own files for GitHub's outputs and job summary, github-output and
	// github-summary in its results folder, with GITHUB_ACTIONS true; every other run gets none
	// (section 12).
	github bool
}

// literalImage is an image a release's values name outright: its repository and digest.
type literalImage struct {
	repository, digest string
}

// customProbe is a fixture's probe.custom. image is the images entry it runs: "probe", the
// fixture probe built as rollproof-fixture-probe:dev, or "busybox", busybox
// 1.37.0 from its archive. command and env are the container's; env's
// entries go into the file sorted by name.
type customProbe struct {
	image   string
	command []string
	env     map[string]string
}

// action is something the harness does to a running run. It is called every 100 ms
// with the latest whole reading of evidence/windows.json and the harness's
// clock, until it reports done, returns an error, or the run ends. A run
// that ends before its action reported done fails the test.
type action func(r running, marks []mark, now time.Time) (done bool, err error)

// running is a run in progress as an action reaches it: signal sends a signal to the
// tool's process; kubectl runs .tools/kubectl with args against the test
// cluster's kubeconfig and returns its error with its output.
type running struct {
	signal  func(os.Signal) error
	kubectl func(args ...string) error
}

// mark is one window of evidence/windows.json, as far as the harness reads it: its
// name, when it began (nil before it did), and each moment its transition
// came to be finished, in order.
type mark struct {
	Name     string      `json:"name"`
	Start    *time.Time  `json:"start"`
	Finishes []time.Time `json:"finishes"`
}

// inputs is what renderConfig needs from outside the fixture: the fixture chart's absolute
// path, and the path of each archive TestMain prepared, by images entry.
type inputs struct {
	chart    string
	archives map[string]string
}

// tcpLoop is the TCP check of section 6, unchanged but for its target: the fixture's own
// Service and port.
const tcpLoop = `trap 'exit 0' TERM
while true; do
  if nc -z -w1 orders.orders 80; then echo '{"ok":true}'
  else echo '{"ok":false,"class":"refused"}'; fi
  sleep 0.1
done`

// configTemplate is the run description of one fixture, for text/template. Each field is a
// JSON text, or empty when the fixture leaves that block out.
const configTemplate = `version: 1
cluster: {{.Cluster}}
{{- if .Registries}}
registries: {{.Registries}}
{{- end}}
images: {{.Images}}
{{- if .Setup}}
setup: {{.Setup}}
{{- end}}
deploy:
  namespace: orders
{{- if .Commands}}
  commands: {{.Commands}}
{{- else}}
  helm:
    release: orders
{{- if .Previous}}
    previous: {{.Previous}}
{{- end}}
    candidate: {{.Candidate}}
{{- end}}
probe: {{.Probe}}
{{- if .Limits}}
limits: {{.Limits}}
{{- end}}
`

// renderConfig returns the text of f's rollproof.yaml, every block as JSON: the cluster, the
// registries when the fixture has entries, images, setup, the Helm releases or the commands, the
// probe and the limits. in gives the chart's absolute path and the archives TestMain prepared. A
// role in f.literal names its image outright. The commands driver applies previous.yaml and
// candidate.yaml beside the file.
func renderConfig(f fixture, in inputs) (string, error) {
	var blocks struct {
		Cluster, Registries, Images, Setup, Commands, Previous, Candidate, Probe, Limits string
	}
	text := func(dst *string, v any) error {
		b, err := json.Marshal(v)
		*dst = string(b)
		return err
	}
	workers := f.workers
	if workers == 0 {
		workers = 3
	}
	cluster := map[string]any{"workers": workers}
	if f.nodeImage != "" {
		cluster["node-image"] = f.nodeImage
	}
	images := map[string]map[string]string{}
	if f.images != nil {
		for name, entry := range f.images {
			images[name] = entry
		}
	} else {
		for _, e := range []string{"previous", "candidate"} {
			if f.commands != nil {
				images[e] = map[string]string{"archive": in.archives[e]}
			} else {
				images[e] = map[string]string{"local": "rollproof-fixture:" + e}
			}
		}
	}
	probe := map[string]any{"http": map[string]string{"url": "http://orders.orders.svc/work?ms=20"}}
	if c := f.custom; c != nil {
		switch c.image {
		case "probe":
			images["probe"] = map[string]string{"local": "rollproof-fixture-probe:dev"}
		case "busybox":
			images["busybox"] = map[string]string{"archive": in.archives["busybox"]}
		default:
			return "", fmt.Errorf("%s: the custom probe's image %q is neither probe nor busybox", f.name, c.image)
		}
		custom := map[string]any{"image": "${images." + c.image + ".ref}"}
		if len(c.command) > 0 {
			custom["command"] = c.command
		}
		if len(c.env) > 0 {
			names := slices.Sorted(maps.Keys(c.env))
			env := make([]map[string]string, 0, len(names))
			for _, n := range names {
				env = append(env, map[string]string{"name": n, "value": c.env[n]})
			}
			custom["env"] = env
		}
		probe = map[string]any{"custom": custom}
	}
	errs := []error{text(&blocks.Cluster, cluster), text(&blocks.Images, images), text(&blocks.Probe, probe)}
	if len(f.registries) > 0 {
		errs = append(errs, text(&blocks.Registries, f.registries))
	}
	if len(f.setup) > 0 {
		errs = append(errs, text(&blocks.Setup, f.setup))
	}
	if len(f.limits) > 0 {
		errs = append(errs, text(&blocks.Limits, f.limits))
	}
	if f.commands != nil {
		commands := map[string][]string{}
		for _, role := range f.commands {
			switch role {
			case "install", "rollback":
				commands[role] = []string{"kubectl", "apply", "-f", "previous.yaml"}
			case "deploy":
				commands[role] = []string{"kubectl", "apply", "-f", "candidate.yaml"}
			default:
				return "", fmt.Errorf("%s: the command role %q is none of install, deploy and rollback", f.name, role)
			}
		}
		errs = append(errs, text(&blocks.Commands, commands))
	} else {
		release := func(entry string, values map[string]any) map[string]any {
			image := map[string]string{
				"repository": "${images." + entry + ".repository}",
				"digest":     "${images." + entry + ".digest}",
			}
			if literal, ok := f.literal[entry]; ok {
				image = map[string]string{"repository": literal.repository, "digest": literal.digest}
			}
			list := []any{map[string]any{"image": image}}
			if len(values) > 0 {
				list = append(list, values)
			}
			return map[string]any{"chart": in.chart, "values": list}
		}
		if !f.noPrevious {
			errs = append(errs, text(&blocks.Previous, release("previous", f.previous)))
		}
		errs = append(errs, text(&blocks.Candidate, release(candidateImage(f), f.candidate)))
	}
	if err := errors.Join(errs...); err != nil {
		return "", fmt.Errorf("%s: the run description: %w", f.name, err)
	}
	var out strings.Builder
	if err := template.Must(template.New("rollproof.yaml").Parse(configTemplate)).Execute(&out, blocks); err != nil {
		return "", fmt.Errorf("%s: the run description: %w", f.name, err)
	}
	return out.String(), nil
}

// candidateImage is the images entry whose image the candidate runs: the previous one for a
// fixture whose candidate runs the previous image, else the candidate's own.
func candidateImage(f fixture) string {
	if f.candidateRunsPrevious {
		return "previous"
	}
	return "candidate"
}

// readMarks reads the window marks at path. A file that is missing, empty or cut
// short (being rewritten) is an error, which the action loop skips.
func readMarks(path string) ([]mark, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var marks []mark
	if err := json.Unmarshal(b, &marks); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return marks, nil
}

// findMark returns the mark of the window name, or nil when marks has none.
func findMark(marks []mark, name string) *mark {
	for i := range marks {
		if marks[i].Name == name {
			return &marks[i]
		}
	}
	return nil
}

// restartAfterTheDeploysFirstFinish is the action of row 13: once the deploy's first finish plus delay has come,
// it runs `kubectl rollout restart deployment/orders --namespace orders`,
// sets *at to that moment and reports done.
func restartAfterTheDeploysFirstFinish(delay time.Duration, at *time.Time) action {
	return func(r running, marks []mark, now time.Time) (bool, error) {
		deploy := findMark(marks, "deploy")
		if deploy == nil || len(deploy.Finishes) == 0 || now.Before(deploy.Finishes[0].Add(delay)) {
			return false, nil
		}
		if err := r.kubectl("rollout", "restart", "deployment/orders", "--namespace", "orders"); err != nil {
			return false, err
		}
		*at = now
		return true, nil
	}
}

// interruptDuringTheDeploy is the action of row 26: once the deploy window has begun, it sends the tool
// SIGINT and reports done.
func interruptDuringTheDeploy(r running, marks []mark, now time.Time) (bool, error) {
	deploy := findMark(marks, "deploy")
	if deploy == nil || deploy.Start == nil {
		return false, nil
	}
	if err := r.signal(os.Interrupt); err != nil {
		return false, err
	}
	return true, nil
}

// act calls f.act every interval, while the run goes on, with the latest whole reading of the
// window marks at marksPath, until the action reports done or returns an error, or the run ends.
// ended receives the run's end; act puts it back for the caller. It reports whether the action
// reported done, and the action's error. For a fixture with no action it returns (false, nil) at
// once. runFixture calls it with an interval of 100 ms.
func act(f fixture, r running, marksPath string, ended chan error, interval time.Duration) (bool, error) {
	if f.act == nil {
		return false, nil
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case err := <-ended:
			ended <- err
			return false, nil
		case <-tick.C:
		}
		marks, err := readMarks(marksPath)
		if err != nil {
			continue
		}
		// A tick and the run's end may both have been ready; the marks of a run that has ended
		// are not acted on.
		select {
		case err := <-ended:
			ended <- err
			return false, nil
		default:
		}
		if done, err := f.act(r, marks, time.Now()); done || err != nil {
			return done, err
		}
	}
}

// manifestValues returns the values with which renderManifests renders one manifest: image set
// to entry's archive in the run's registry, rollproof.test/ENTRY at digest, and then the fixture's
// values for that release over it, so that a fixture's own image wins as a later value does in
// renderConfig.
func manifestValues(values map[string]any, entry, digest string) map[string]any {
	out := map[string]any{"image": map[string]string{"repository": "rollproof.test/" + entry, "digest": digest}}
	for k, v := range values {
		out[k] = v
	}
	return out
}

// randomArchive writes a random image as an archive, as an engine's save writes one, and returns
// its path.
func randomArchive(t *testing.T, tag string) string {
	t.Helper()
	img, err := random.Image(1024, 1)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := name.NewTag(tag)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "image.tar")
	if err := tarball.WriteToFile(path, ref, img); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConfigOfEachShapeLoads(t *testing.T) {
	t.Setenv(passwordVar, "pw")
	chart, err := filepath.Abs(filepath.Join("..", "fixtures", "chart"))
	if err != nil {
		t.Fatal(err)
	}
	ca, err := newAuthority(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mirrorIn := mirrorInputs{
		engine:          "nerdctl",
		certs:           t.TempDir(),
		archives:        t.TempDir(),
		ca:              ca,
		previousDigest:  "sha256:" + strings.Repeat("0", 64),
		candidateDigest: "sha256:" + strings.Repeat("1", 64),
	}
	a := randomArchive(t, "previous:test")
	b := randomArchive(t, "candidate:test")
	c := randomArchive(t, "busybox:test")
	in := inputs{chart: chart, archives: map[string]string{"previous": a, "candidate": b, "busybox": c}}
	previousApply := []string{"kubectl", "apply", "-f", "previous.yaml"}
	candidateApply := []string{"kubectl", "apply", "-f", "candidate.yaml"}
	setup := [][]string{{"kubectl", "get", "namespace", "orders"}}
	const nodeImage = "kindest/node:v1.34.11@sha256:44e222ee2132dab25ff87301682f89eb82c7880ea3a1bf543bfe9708fd08d67d"

	for _, tc := range []struct {
		name  string
		f     fixture
		check func(t *testing.T, text string, cfg *config.Config)
	}{
		{"both releases", fixture{}, func(t *testing.T, text string, cfg *config.Config) {
			if cfg.Deploy.Helm == nil || cfg.Deploy.Helm.Previous == nil {
				t.Errorf("no previous release")
			}
			if cfg.Probe.HTTP == nil || cfg.Probe.HTTP.URL != "http://orders.orders.svc/work?ms=20" {
				t.Errorf("probe.http %+v", cfg.Probe.HTTP)
			}
			if got := cfg.Images["previous"].Local; got != "rollproof-fixture:previous" {
				t.Errorf("images.previous.local %q", got)
			}
			if cfg.Cluster.Workers != 3 {
				t.Errorf("cluster.workers %d, want 3", cfg.Cluster.Workers)
			}
		}},
		{"no previous", fixture{noPrevious: true}, func(t *testing.T, text string, cfg *config.Config) {
			if cfg.Deploy.Helm == nil || cfg.Deploy.Helm.Previous != nil || cfg.Deploy.Helm.Candidate == nil {
				t.Errorf("deploy.helm %+v, want a candidate and no previous", cfg.Deploy.Helm)
			}
		}},
		{"candidate runs previous", fixture{candidateRunsPrevious: true}, func(t *testing.T, text string, cfg *config.Config) {
			if n := strings.Count(text, `"${images.previous.digest}"`); n != 2 {
				t.Errorf("the text names the previous digest %d times, want 2:\n%s", n, text)
			}
			if strings.Contains(text, `"${images.candidate.digest}"`) {
				t.Errorf("the text names the candidate's digest:\n%s", text)
			}
		}},
		{"node image", fixture{nodeImage: nodeImage}, func(t *testing.T, text string, cfg *config.Config) {
			if cfg.Cluster.NodeImage != nodeImage {
				t.Errorf("cluster.node-image %q", cfg.Cluster.NodeImage)
			}
		}},
		{"setup", fixture{setup: setup}, func(t *testing.T, text string, cfg *config.Config) {
			if diff := cmp.Diff(setup, cfg.Setup); diff != "" {
				t.Errorf("setup (-want +got):\n%s", diff)
			}
		}},
		{"three commands", fixture{commands: []string{"install", "deploy", "rollback"}}, func(t *testing.T, text string, cfg *config.Config) {
			if cfg.Deploy.Helm != nil || cfg.Deploy.Commands == nil {
				t.Fatalf("deploy %+v, want commands and no helm", cfg.Deploy)
			}
			c := cfg.Deploy.Commands
			if diff := cmp.Diff(previousApply, c.Install); diff != "" {
				t.Errorf("install (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(candidateApply, c.Deploy); diff != "" {
				t.Errorf("deploy (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(previousApply, c.Rollback); diff != "" {
				t.Errorf("rollback (-want +got):\n%s", diff)
			}
			if got := cfg.Images["previous"].Archive; got != a {
				t.Errorf("images.previous.archive %q, want %q", got, a)
			}
		}},
		{"two commands", fixture{commands: []string{"install", "deploy"}}, func(t *testing.T, text string, cfg *config.Config) {
			if cfg.Deploy.Commands == nil || cfg.Deploy.Commands.Rollback != nil {
				t.Errorf("deploy.commands %+v, want no rollback", cfg.Deploy.Commands)
			}
		}},
		{"fixture probe", fixture{custom: &customProbe{image: "probe", env: map[string]string{"VERSION": "candidate", "ON_VERSION": "silent"}}},
			func(t *testing.T, text string, cfg *config.Config) {
				if cfg.Probe.HTTP != nil || cfg.Probe.Custom == nil {
					t.Fatalf("probe %+v, want a custom probe only", cfg.Probe)
				}
				if got := cfg.Probe.Custom.Image; got != "${images.probe.ref}" {
					t.Errorf("probe.custom.image %q", got)
				}
				var names []string
				for _, e := range cfg.Probe.Custom.Env {
					names = append(names, e.Name)
				}
				if diff := cmp.Diff([]string{"ON_VERSION", "VERSION"}, names); diff != "" {
					t.Errorf("env names (-want +got):\n%s", diff)
				}
				if got := cfg.Images["probe"].Local; got != "rollproof-fixture-probe:dev" {
					t.Errorf("images.probe.local %q", got)
				}
			}},
		{"TCP loop", fixture{custom: &customProbe{image: "busybox", command: []string{"sh", "-c", tcpLoop}}},
			func(t *testing.T, text string, cfg *config.Config) {
				if cfg.Probe.Custom == nil {
					t.Fatalf("probe %+v, want a custom probe", cfg.Probe)
				}
				if got := cfg.Probe.Custom.Image; got != "${images.busybox.ref}" {
					t.Errorf("probe.custom.image %q", got)
				}
				if diff := cmp.Diff([]string{"sh", "-c", tcpLoop}, cfg.Probe.Custom.Command); diff != "" {
					t.Errorf("probe.custom.command (-want +got):\n%s", diff)
				}
				if got := cfg.Images["busybox"].Archive; got != c {
					t.Errorf("images.busybox.archive %q, want %q", got, c)
				}
			}},
		{"limits", fixture{limits: map[string]string{"settle": "30s", "timeout": "90s"}}, func(t *testing.T, text string, cfg *config.Config) {
			if cfg.Limits.Settle != 30*time.Second || cfg.Limits.Timeout != 90*time.Second {
				t.Errorf("limits %+v, want settle 30s and timeout 90s", cfg.Limits)
			}
		}},
		{"through the mirror", throughTheMirror(mirrorIn), func(t *testing.T, text string, cfg *config.Config) {
			want := []config.Registry{
				{Host: "docker.io", Username: jobUser, Password: "pw", CA: mirrorIn.ca, Mirror: mirrorHost + "/docker-io"},
				{Host: realHost, Username: jobUser, Password: "pw", CA: mirrorIn.ca, Mirror: mirrorHost + "/real"},
			}
			if diff := cmp.Diff(want, cfg.Registries); diff != "" {
				t.Errorf("registries (-want +got):\n%s", diff)
			}
			if got := cfg.Images["candidate"].As; got != "docker.io/rollproof/fixture" {
				t.Errorf("images.candidate.as %q, want docker.io/rollproof/fixture", got)
			}
		}},
		{"the mirror replaces its host", mirrorReplacesItsHost(mirrorIn), func(t *testing.T, text string, cfg *config.Config) {
			if len(cfg.Images) != 0 {
				t.Errorf("images holds %d entries, want none: %v", len(cfg.Images), cfg.Images)
			}
			candidate := cfg.Deploy.Helm.Candidate
			if candidate == nil || len(candidate.Values) == 0 {
				t.Fatalf("deploy.helm.candidate %+v, want a values map", candidate)
			}
			image, ok := candidate.Values[0].Inline["image"].(map[string]any)
			if !ok {
				t.Fatalf("the candidate's values name no image: %v", candidate.Values[0].Inline)
			}
			if got := image["repository"]; got != realHost+"/team/candidate" {
				t.Errorf("the candidate's repository %v, want %q", got, realHost+"/team/candidate")
			}
			if got := image["digest"]; got != mirrorIn.candidateDigest {
				t.Errorf("the candidate's digest %v, want %q", got, mirrorIn.candidateDigest)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, err := renderConfig(tc.f, in)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "rollproof.yaml")
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(path, os.LookupEnv)
			if err != nil {
				t.Fatalf("%v\n%s", err, text)
			}
			tc.check(t, text, cfg)
		})
	}
}

func TestReadMarks(t *testing.T) {
	t0 := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	t1, t2, t3 := t0.Add(10*time.Second), t0.Add(20*time.Second), t0.Add(30*time.Second)
	at := func(t time.Time) string { return `"` + t.Format(time.RFC3339Nano) + `"` }
	text := `[{"name":"steady","start":` + at(t0) + `,"finishes":[]},` +
		`{"name":"deploy","start":` + at(t1) + `,"finishes":[` + at(t2) + `,` + at(t3) + `]},` +
		`{"name":"rollback","start":null}]`
	dir := t.TempDir()
	path := filepath.Join(dir, "windows.json")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	marks, err := readMarks(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(marks) != 3 {
		t.Fatalf("%d marks, want 3: %+v", len(marks), marks)
	}
	if diff := cmp.Diff([]time.Time{t2, t3}, marks[1].Finishes); marks[1].Name != "deploy" || diff != "" {
		t.Errorf("the second mark %+v; want deploy with the finishes %v and %v", marks[1], t2, t3)
	}
	if marks[2].Name != "rollback" || marks[2].Start != nil {
		t.Errorf("the third mark %+v; want rollback with no start", marks[2])
	}

	cut := filepath.Join(dir, "cut.json")
	if err := os.WriteFile(cut, []byte(text[:len(text)/2]), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readMarks(cut); err == nil {
		t.Errorf("readMarks of a file cut short gave no error")
	}
	if _, err := readMarks(filepath.Join(dir, "missing.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("readMarks of a missing file = %v, want fs.ErrNotExist", err)
	}
	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readMarks(empty); err == nil {
		t.Errorf("readMarks of an empty file gave no error")
	}

	// Two finishes may share one moment; both are kept.
	same := filepath.Join(dir, "same.json")
	if err := os.WriteFile(same, []byte(`[{"name":"deploy","start":`+at(t1)+`,"finishes":[`+at(t2)+`,`+at(t2)+`]}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	marks, err = readMarks(same)
	if err != nil {
		t.Fatal(err)
	}
	if len(marks) != 1 || len(marks[0].Finishes) != 2 || !marks[0].Finishes[0].Equal(t2) || !marks[0].Finishes[1].Equal(t2) {
		t.Errorf("marks %+v; want deploy with two finishes at %v", marks, t2)
	}
}

func TestRenderConfigRejectsAnUnknownName(t *testing.T) {
	in := inputs{chart: "/chart", archives: map[string]string{}}
	for _, tc := range []struct {
		name, want string
		f          fixture
	}{
		{"custom image", "alpine", fixture{name: "row", custom: &customProbe{image: "alpine"}}},
		{"command role", "upgrade", fixture{name: "row", commands: []string{"install", "upgrade"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, err := renderConfig(tc.f, in)
			if err == nil || !strings.Contains(err.Error(), "row") || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("renderConfig = %q, %v; want an error that names the fixture and %s", text, err, tc.want)
			}
		})
	}
}

func TestManifestValuesLetTheFixtureSetImage(t *testing.T) {
	const digest = "sha256:0123"
	got := manifestValues(map[string]any{"replicas": 3}, "previous", digest)
	want := map[string]any{"replicas": 3, "image": map[string]string{"repository": "rollproof.test/previous", "digest": digest}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("without an image of the fixture's (-want +got):\n%s", diff)
	}
	own := map[string]any{"repository": "elsewhere/orders"}
	got = manifestValues(map[string]any{"image": own}, "previous", digest)
	if diff := cmp.Diff(map[string]any{"image": own}, got); diff != "" {
		t.Errorf("with an image of the fixture's, which wins as a later value does in renderConfig (-want +got):\n%s", diff)
	}
}

// TestActionLoop runs act against a fake run: a marks file, a running whose calls go nowhere, and
// an ended channel that the test fills when the fake run ends.
func TestActionLoop(t *testing.T) {
	began := time.Date(2026, 11, 19, 10, 0, 20, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "windows.json")
	if err := os.WriteFile(path, []byte(`[{"name":"deploy","start":"`+began.Format(time.RFC3339Nano)+`","finishes":[]}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	const interval = time.Millisecond
	ended := func(t *testing.T, c chan error) bool {
		t.Helper()
		select {
		case err := <-c:
			c <- err
			return true
		default:
			return false
		}
	}

	t.Run("the action reports done", func(t *testing.T) {
		calls := 0
		f := fixture{act: func(r running, marks []mark, now time.Time) (bool, error) {
			calls++
			if len(marks) != 1 || marks[0].Name != "deploy" {
				t.Errorf("marks %+v, want the deploy's", marks)
			}
			return calls == 3, nil
		}}
		c := make(chan error, 1)
		done, err := act(f, running{}, path, c, interval)
		if !done || err != nil || calls != 3 {
			t.Errorf("act = (%v, %v) after %d calls, want (true, nil) after 3", done, err, calls)
		}
		if ended(t, c) {
			t.Errorf("act put an end of the run that never came")
		}
	})

	t.Run("the action fails", func(t *testing.T) {
		want := errors.New("kubectl failed")
		f := fixture{act: func(r running, marks []mark, now time.Time) (bool, error) { return false, want }}
		done, err := act(f, running{}, path, make(chan error, 1), interval)
		if done || !errors.Is(err, want) {
			t.Errorf("act = (%v, %v), want (false, %v)", done, err, want)
		}
	})

	t.Run("the run ends first", func(t *testing.T) {
		calls := 0
		f := fixture{act: func(r running, marks []mark, now time.Time) (bool, error) { calls++; return true, nil }}
		c := make(chan error, 1)
		c <- errors.New("exit status 2")
		done, err := act(f, running{}, path, c, interval)
		if done || err != nil || calls != 0 {
			t.Errorf("act = (%v, %v) after %d calls, want (false, nil) and no call", done, err, calls)
		}
		if !ended(t, c) {
			t.Errorf("act did not put the run's end back")
		}
	})

	// The run ends while the action waits: by the next call a tick and the end are both ready, and
	// no action is called on the marks of a run that has ended.
	t.Run("the run ends between two calls", func(t *testing.T) {
		for range 50 {
			calls := 0
			c := make(chan error, 1)
			f := fixture{act: func(r running, marks []mark, now time.Time) (bool, error) {
				calls++
				if calls == 1 {
					c <- nil
					time.Sleep(5 * interval)
					return false, nil
				}
				return true, nil
			}}
			done, err := act(f, running{}, path, c, interval)
			if done || err != nil || calls != 1 {
				t.Fatalf("act = (%v, %v) after %d calls, want (false, nil) after 1", done, err, calls)
			}
			if !ended(t, c) {
				t.Fatalf("act did not put the run's end back")
			}
		}
	})
}

// deployMarks returns the marks of a run in its deploy window: the steady window, and the deploy
// with start and finishes.
func deployMarks(start *time.Time, finishes ...time.Time) []mark {
	steady := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	return []mark{{Name: "steady", Start: &steady}, {Name: "deploy", Start: start, Finishes: finishes}, {Name: "rollback"}}
}

func TestInterruptWaitsForTheDeploysStart(t *testing.T) {
	var sent []os.Signal
	r := running{signal: func(s os.Signal) error { sent = append(sent, s); return nil }}
	now := time.Date(2026, 11, 19, 10, 0, 30, 0, time.UTC)
	if done, err := interruptDuringTheDeploy(r, deployMarks(nil), now); done || err != nil || len(sent) != 0 {
		t.Errorf("before the deploy began: (%v, %v), sent %v; want (false, nil) and nothing sent", done, err, sent)
	}
	began := now.Add(-time.Second)
	if done, err := interruptDuringTheDeploy(r, deployMarks(&began), now); !done || err != nil {
		t.Errorf("once the deploy began: (%v, %v), want (true, nil)", done, err)
	}
	if len(sent) != 1 || sent[0] != os.Interrupt {
		t.Errorf("sent %v, want os.Interrupt once", sent)
	}
}

func TestRestartComesAfterTheDelay(t *testing.T) {
	var calls [][]string
	r := running{kubectl: func(args ...string) error { calls = append(calls, args); return nil }}
	var at time.Time
	act := restartAfterTheDeploysFirstFinish(5*time.Second, &at)
	began := time.Date(2026, 11, 19, 10, 0, 20, 0, time.UTC)
	f := began.Add(10 * time.Second)

	if done, err := act(r, deployMarks(&began), f.Add(time.Minute)); done || err != nil || len(calls) != 0 {
		t.Errorf("with no finish yet: (%v, %v), kubectl called %v; want (false, nil) and no call", done, err, calls)
	}
	if done, err := act(r, deployMarks(&began, f), f.Add(4900*time.Millisecond)); done || err != nil || len(calls) != 0 {
		t.Errorf("4.9 s after the finish: (%v, %v), kubectl called %v; want (false, nil) and no call", done, err, calls)
	}
	now := f.Add(5100 * time.Millisecond)
	if done, err := act(r, deployMarks(&began, f), now); !done || err != nil {
		t.Errorf("5.1 s after the finish: (%v, %v), want (true, nil)", done, err)
	}
	want := [][]string{{"rollout", "restart", "deployment/orders", "--namespace", "orders"}}
	if diff := cmp.Diff(want, calls); diff != "" {
		t.Errorf("kubectl calls (-want +got):\n%s", diff)
	}
	if !at.Equal(now) {
		t.Errorf("at = %v, want %v", at, now)
	}
}
