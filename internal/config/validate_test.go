package config

import (
	"archive/tar"
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

const previousBlock = "    previous:\n      chart: oci://registry.example/charts/orders\n      version: \"1.4.2\"\n"

const candidateBlock = "    candidate:\n      chart: oci://registry.example/charts/orders\n      version: \"1.5.0\"\n"

// commandsBlock is a deploy.commands block with the given command lines.
func commandsBlock(lines string) string { return "  commands:\n" + lines }

func TestValidateRejects(t *testing.T) {
	const url, ns = "    url: http://orders.orders.svc/api/S3CRET-VALUE\n", "  namespace: orders\n"
	registries := func(entries string) string { return "registries:\n" + entries + "deploy:\n" }
	image := func(name string) string { return "images:\n  " + name + ":\n    local: orders:S3CRET-VALUE\ndeploy:\n" }
	for _, tc := range []struct{ name, old, new, want string }{
		{"helm and commands", helmBlock, helmBlock + commandsBlock("    install: [kubectl, apply, -k, overlays/S3CRET-VALUE]\n"), "deploy.commands"},
		{"neither helm nor commands", helmBlock, "", "deploy.helm"},
		{"an http and a custom probe", "  http:\n", "  custom:\n    image: registry.example/tools/S3CRET-VALUE\n  http:\n", "probe.custom"},
		{"no probe", httpProbe, "", "probe.http"},
		{"an image with local and archive", "deploy:\n", "images:\n  orders:\n    local: orders:candidate\n    archive: S3CRET-VALUE.tar\ndeploy:\n", "images.orders"},
		{"an image with neither", "deploy:\n", "images:\n  orders:\n    as: registry.example/S3CRET-VALUE\ndeploy:\n", "images.orders"},
		{"an image named Orders", "deploy:\n", image("Orders"), "images.Orders"},
		{"an image named my image", "deploy:\n", image("my image"), "images.my image"},
		{"an image named -x", "deploy:\n", image(`"-x"`), "images.-x"},
		{"an image named x-", "deploy:\n", image("x-"), "images.x-"},
		{"an image named a..b", "deploy:\n", image("a..b"), "images.a..b"},
		{"helm with no candidate", candidateBlock, "", "deploy.helm.candidate"},
		{"helm with no release", "    release: orders\n", "", "deploy.helm.release"},
		{"a release with no chart", candidateBlock, "    candidate:\n      version: \"1.5.0\"\n", "deploy.helm.candidate.chart"},
		{"commands with no install", helmBlock, commandsBlock("    deploy: [kubectl, apply, -k, overlays/S3CRET-VALUE]\n"), "deploy.commands.install"},
		{"a rollback with no deploy", helmBlock, commandsBlock("    install: [kubectl, apply, -k, overlays/previous]\n    rollback: [kubectl, apply, -k, overlays/S3CRET-VALUE]\n"), "deploy.commands.deploy"},
		{"silence equal to steady", "  steady: 10s\n", "  steady: 5s\n", "limits.silence"},
		{"silence longer than steady", "  steady: 10s\n", "  steady: 4s\n", "limits.silence"},
		{"silence equal to settle", "  settle: 15s\n", "  settle: 5s\n", "limits.silence"},
		{"silence longer than settle", "  settle: 15s\n", "  settle: 3s\n", "limits.silence"},
		{"probe timeout plus one second equal to silence", "    timeout: 2s\n", "    timeout: 4s\n",
			"probe.http.timeout (line 20): plus one second must be shorter than limits.silence"},
		{"probe timeout longer than silence", "    timeout: 2s\n", "    timeout: 6s\n", "probe.http.timeout"},
		{"one host twice", "deploy:\n", registries("  - host: registry.example\n  - host: registry.example\n    mirror: S3CRET-VALUE\n"), "registries[1].host"},
		{"a registry with no host", "deploy:\n", registries("  - mirror: S3CRET-VALUE\n"), "registries[0].host"},
		{"namespace kube-system", ns, "  namespace: kube-system\n", "deploy.namespace"},
		{"namespace kube-public", ns, "  namespace: kube-public\n", "deploy.namespace"},
		{"namespace kube-node-lease", ns, "  namespace: kube-node-lease\n", "deploy.namespace"},
		{"namespace local-path-storage", ns, "  namespace: local-path-storage\n", "deploy.namespace"},
		{"the probe's namespace", ns, "  namespace: rollproof-probe\n", "deploy.namespace"},
		{"an empty namespace", ns, "  namespace: \"\"\n", "deploy.namespace"},
		{"no namespace", ns, "", "deploy.namespace"},
		{"a URL with a user and a password", url, "    url: http://user:S3CRET-VALUE@orders.orders.svc/\n", "probe.http.url"},
		{"a URL with a user", url, "    url: http://S3CRET-VALUE@orders.orders.svc/\n", "probe.http.url"},
		{"a URL with another scheme", url, "    url: ftp://orders.orders.svc/S3CRET-VALUE\n", "probe.http.url"},
		{"a URL with no scheme", url, "    url: orders.orders.svc/S3CRET-VALUE\n", "probe.http.url"},
		{"a URL with no host", url, "    url: http:///S3CRET-VALUE\n", "probe.http.url"},
		{"no URL", url, "", "probe.http.url"},
		{"a remote chart with no version", candidateBlock, "    candidate:\n      chart: oci://registry.example/charts/S3CRET-VALUE\n", "deploy.helm.candidate.version"},
		{"an alias chart with no version", candidateBlock, "    candidate:\n      chart: stable/S3CRET-VALUE\n", "deploy.helm.candidate.version"},
		{"a candidate chart with a user and password", candidateBlock, "    candidate:\n      chart: oci://u:S3CRET-VALUE@registry.example/x\n      version: \"1.5.0\"\n", "deploy.helm.candidate.chart (line 12): must not hold a user name or password"},
		{"a previous chart with a user and password", previousBlock, "    previous:\n      chart: oci://u:S3CRET-VALUE@registry.example/x\n      version: \"1.4.2\"\n", "deploy.helm.previous.chart (line 9): must not hold a user name or password"},
		{"a candidate chart with a user only", candidateBlock, "    candidate:\n      chart: https://S3CRET-VALUE@registry.example/x.tgz\n      version: \"1.5.0\"\n", "deploy.helm.candidate.chart (line 12): must not hold a user name or password"},
		{"a candidate chart with a user and a bad port", candidateBlock, "    candidate:\n      chart: oci://u:S3CRET-VALUE@registry.example:port/x\n      version: \"1.5.0\"\n", "deploy.helm.candidate.chart (line 12): must not hold a user name or password"},
		{"a candidate chart with a line break", candidateBlock, "    candidate:\n      chart: \"oci://registry.example/x\\nS3CRET-VALUE\"\n      version: \"1.5.0\"\n", "deploy.helm.candidate.chart (line 12): must not hold a control character"},
		{"a previous chart with a line break", previousBlock, "    previous:\n      chart: \"stable/x\\nS3CRET-VALUE\"\n      version: \"1.4.2\"\n", "deploy.helm.previous.chart (line 9): must not hold a control character"},
		{"a candidate path chart with a control character", candidateBlock, "    candidate:\n      chart: \"./S3CRET-VALUE\\u0007\"\n", "deploy.helm.candidate.chart (line 12): must not hold a control character"},
		{"a candidate chart with a query", candidateBlock, "    candidate:\n      chart: oci://reg/x?token=S3CRET-VALUE\n      version: \"1.5.0\"\n", "deploy.helm.candidate.chart (line 12): must not hold a query"},
		{"a previous chart with a signed URL", previousBlock, "    previous:\n      chart: https://h/c.tgz?X-Sig=S3CRET-VALUE\n      version: \"1.4.2\"\n", "deploy.helm.previous.chart (line 9): must not hold a query"},
		{"a candidate chart that does not parse, with a query", candidateBlock, "    candidate:\n      chart: oci://reg:port/x?token=S3CRET-VALUE\n      version: \"1.5.0\"\n", "deploy.helm.candidate.chart (line 12): must not hold a query"},
		{"a comma in a values file's path", candidateBlock, candidateBlock + "      values:\n        - test/values,S3CRET-VALUE.yaml\n", "deploy.helm.candidate.values[0]"},
		{"a selector that does not parse", ns, ns + "  selector: app in S3CRET-VALUE\n", "deploy.selector"},
		{"a custom probe with no image", httpProbe, "probe:\n  custom:\n    command: [sh, -c, \"echo S3CRET-VALUE\"]\n", "probe.custom.image"},
		{"a header name with an equals sign", "      X-Planted: S3CRET-VALUE\n", "      \"S3CRET-VALUE=x\": v\n", "probe.http.headers (line 17): a header name must be a token"},
		{"a header name with a space", "      X-Planted: S3CRET-VALUE\n", "      \"X Planted\": v\n", "probe.http.headers (line 17): a header name must be a token"},
		{"a header value with a line break", "      X-Planted: S3CRET-VALUE\n", "      X-Planted: \"a\\nS3CRET-VALUE\"\n", "probe.http.headers.X-Planted (line 18): must not hold a line break or NUL"},
		{"a header value with NUL", "      X-Planted: S3CRET-VALUE\n", "      X-Planted: \"a\\0S3CRET-VALUE\"\n", "probe.http.headers.X-Planted (line 18): must not hold a line break or NUL"},
		{"a null values item", candidateBlock, candidateBlock + "      values:\n        - ~\n        - S3CRET-VALUE.yaml\n", "deploy.helm.candidate.values[0] (line 15): must not be empty"},
		{"an empty map as a values item", candidateBlock, candidateBlock + "      values:\n        - {}\n        - S3CRET-VALUE.yaml\n", "deploy.helm.candidate.values[0] (line 15): must not be empty"},
		{"a null registries item", "deploy:\n", registries("  - ~\n  - host: S3CRET-VALUE\n"), "registries[0]"},
		{"a null env item", httpProbe, customProbe("    env:\n      - ~\n      - name: S3CRET-VALUE\n        value: v\n"), "probe.custom.env[0]"},
		{"a null setup entry", "deploy:\n", "setup:\n  - ~\n  - [echo, S3CRET-VALUE]\ndeploy:\n", "setup[0] (line 5): must not be empty"},
		{"a null setup argument", "deploy:\n", "setup:\n  - [kubectl, ~, S3CRET-VALUE]\ndeploy:\n", "setup[0][1] (line 5): must not be empty"},
		{"an empty setup entry", "deploy:\n", "setup:\n  - []\n  - [echo, S3CRET-VALUE]\ndeploy:\n", "setup[0] (line 5): must not be empty"},
		{"a setup entry with an empty program", "deploy:\n", "setup:\n  - [\"\", S3CRET-VALUE]\ndeploy:\n", "setup[0][0] (line 5): must not be empty"},
		{"a null install argument", helmBlock, commandsBlock("    install: [kubectl, ~, S3CRET-VALUE]\n"), "deploy.commands.install[1] (line 7): must not be empty"},
		{"an install with an empty program", helmBlock, commandsBlock("    install: [\"\", S3CRET-VALUE]\n"), "deploy.commands.install[0] (line 7): must not be empty"},
		{"a null deploy argument", helmBlock, commandsBlock("    install: [kubectl, apply]\n    deploy: [kubectl, ~, S3CRET-VALUE]\n"), "deploy.commands.deploy[1] (line 8): must not be empty"},
		{"an empty deploy list", helmBlock, commandsBlock("    install: [kubectl, apply, S3CRET-VALUE]\n    deploy: []\n"), "deploy.commands.deploy (line 8): must not be empty"},
		{"a null rollback argument", helmBlock, commandsBlock("    install: [kubectl, apply]\n    deploy: [kubectl, apply]\n    rollback: [~, S3CRET-VALUE]\n"), "deploy.commands.rollback[0] (line 9): must not be empty"},
		{"an empty rollback list", helmBlock, commandsBlock("    install: [kubectl, apply]\n    deploy: [kubectl, apply, S3CRET-VALUE]\n    rollback: []\n"), "deploy.commands.rollback (line 9): must not be empty"},
		{"a rollback with an empty program", helmBlock, commandsBlock("    install: [kubectl, apply]\n    deploy: [kubectl, apply]\n    rollback: [\"\", S3CRET-VALUE]\n"), "deploy.commands.rollback[0] (line 9): must not be empty"},
		{"a null custom command item", httpProbe, "probe:\n  custom:\n    image: registry.example/tools/busybox\n    command: [sh, ~, S3CRET-VALUE]\n", "probe.custom.command[1] (line 17): must not be empty"},
		{"a custom command with an empty program", httpProbe, "probe:\n  custom:\n    image: registry.example/tools/busybox\n    command: [\"\", S3CRET-VALUE]\n", "probe.custom.command[0] (line 17): must not be empty"},
		{"a null custom arg", httpProbe, "probe:\n  custom:\n    image: registry.example/tools/busybox\n    args: [~, S3CRET-VALUE]\n", "probe.custom.args[0] (line 17): must not be empty"},
		{"a password written in the file", "deploy:\n", registries("  - host: registry.example\n    username: ci\n    password: S3CRET-VALUE\n"), "registries[0].password"},
		{"a password with text beside the form", "deploy:\n", registries("  - host: registry.example\n    username: ci\n    password: S3CRET-VALUE${env.P}\n"), "registries[0].password"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadText(t, variant(t, base, tc.old, tc.new))
			rejected(t, err, tc.want)
		})
	}
}

func TestValidateAccepts(t *testing.T) {
	for _, tc := range []struct{ name, old, new string }{
		{"an empty selector", "  namespace: orders\n", "  namespace: orders\n  selector: \"\"\n"},
		{"a selector with a set", "  namespace: orders\n", "  namespace: orders\n  selector: \"app=orders,tier in (a,b)\"\n"},
		{"an https URL", "    url: http://orders.orders.svc/api/S3CRET-VALUE\n", "    url: https://orders.orders.svc:8443/api\n"},
		{"probe timeout 2s with silence 5s", "    timeout: 2s\n", "    timeout: 2s\n"},
		{"probe timeout 3900ms with silence 5s", "    timeout: 2s\n", "    timeout: 3900ms\n"},
		{"an empty argument after the program", "deploy:\n", "setup:\n  - [echo, \"\"]\ndeploy:\n"},
		{"an image named orders", "deploy:\n", "images:\n  orders:\n    local: orders:candidate\ndeploy:\n"},
		{"an image named orders-v2", "deploy:\n", "images:\n  orders-v2:\n    local: orders:candidate\ndeploy:\n"},
		{"an image named a.b_c", "deploy:\n", "images:\n  a.b_c:\n    local: orders:candidate\ndeploy:\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := loadText(t, variant(t, base, tc.old, tc.new)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// project makes a folder named "my project" under a new temporary folder,
// with the given subfolders, and returns the folder and its real path.
func project(t *testing.T, subfolders ...string) (dir, real string) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), "my project")
	for _, sub := range append([]string{"."}, subfolders...) {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir, realPath(t, dir)
}

func TestVersionBesideAChartPath(t *testing.T) {
	dir, _ := project(t, "chart")
	_, err := loadWith(t, dir, variant(t, base, candidateBlock, "    candidate:\n      chart: ./chart\n      version: \"1.5.0\"\n"), noEnv)
	rejected(t, err, "deploy.helm.candidate.version")
}

func TestChartPaths(t *testing.T) {
	dir, real := project(t, "chart")
	t.Chdir(t.TempDir())
	for _, chart := range []string{"./chart", "chart", "chart/", filepath.Join(dir, "chart")} {
		c, err := loadWith(t, dir, variant(t, base, candidateBlock, "    candidate:\n      chart: "+chart+"\n"), noEnv)
		if err != nil {
			t.Fatalf("chart %q: %v", chart, err)
		}
		want := &Release{Chart: filepath.Join(real, "chart"), IsPath: true}
		if d := cmp.Diff(want, c.Deploy.Helm.Candidate); d != "" {
			t.Errorf("chart %q (-want +got):\n%s", chart, d)
		}
	}
	for _, chart := range []string{"oci://registry.example/charts/orders", "stable/orders"} {
		c, err := loadWith(t, dir, variant(t, base, candidateBlock, "    candidate:\n      chart: "+chart+"\n      version: \"1.5.0\"\n"), noEnv)
		if err != nil {
			t.Fatalf("chart %q: %v", chart, err)
		}
		want := &Release{Chart: chart, Version: "1.5.0"}
		if d := cmp.Diff(want, c.Deploy.Helm.Candidate); d != "" {
			t.Errorf("chart %q (-want +got):\n%s", chart, d)
		}
	}
}

func TestPathsAreRelativeToTheFile(t *testing.T) {
	dir, real := project(t, "chart", "images")
	t.Chdir(t.TempDir())
	writeArchive(t, filepath.Join(dir, "images", "orders.tar"), 1)
	text := variant(t, base, candidateBlock, "    candidate:\n      chart: ./chart\n      values:\n        - test/values-kind.yaml\n        - { replicas: 2 }\n")
	text = variant(t, text, "deploy:\n", "registries:\n  - host: registry.example\n    ca: certs/corp-ca.pem\n"+
		"images:\n  orders:\n    archive: images/orders.tar\nsetup:\n  - [kubectl, apply, -f, test/prerequisites.yaml]\ndeploy:\n")
	c, err := loadWith(t, dir, text, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	wantValues := []Value{{File: filepath.Join(real, "test", "values-kind.yaml")}, {Inline: map[string]any{"replicas": 2}}}
	if d := cmp.Diff(wantValues, c.Deploy.Helm.Candidate.Values); d != "" {
		t.Errorf("values (-want +got):\n%s", d)
	}
	if got, want := c.Registries[0].CA, filepath.Join(real, "certs", "corp-ca.pem"); got != want {
		t.Errorf("ca %q, want %q", got, want)
	}
	if got, want := c.Images["orders"].Archive, filepath.Join(real, "images", "orders.tar"); got != want {
		t.Errorf("archive %q, want %q", got, want)
	}
	if d := cmp.Diff([][]string{{"kubectl", "apply", "-f", "test/prerequisites.yaml"}}, c.Setup); d != "" {
		t.Errorf("setup changed (-want +got):\n%s", d)
	}

	commands := variant(t, base, helmBlock, commandsBlock("    install: [kubectl, apply, -k, overlays/previous]\n"))
	c, err = loadWith(t, dir, commands, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff([]string{"kubectl", "apply", "-k", "overlays/previous"}, c.Deploy.Commands.Install); d != "" {
		t.Errorf("install changed (-want +got):\n%s", d)
	}
}

func TestPathsThroughALink(t *testing.T) {
	dir, real := project(t, "chart")
	target := filepath.Join(dir, "rollproof.yaml")
	if err := os.WriteFile(target, []byte(variant(t, base, candidateBlock, "    candidate:\n      chart: ./chart\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "rollproof.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	c, err := Load(link, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := c.Deploy.Helm.Candidate.Chart, filepath.Join(real, "chart"); got != want {
		t.Errorf("chart %q, want %q", got, want)
	}
}

// writeArchive writes at path a tar such as docker save writes, holding n small images.
func writeArchive(t *testing.T, path string, n int) {
	t.Helper()
	refs := map[name.Reference]v1.Image{}
	for i := range n {
		img, err := random.Image(64, 1)
		if err != nil {
			t.Fatal(err)
		}
		ref, err := name.ParseReference(fmt.Sprintf("rollproof.test/img:%d", i))
		if err != nil {
			t.Fatal(err)
		}
		refs[ref] = img
	}
	if err := tarball.MultiRefWriteToFile(path, refs); err != nil {
		t.Fatal(err)
	}
}

// writeTar writes at path a tar that holds the given files, in the order of their names.
func writeTar(t *testing.T, path string, files map[string]string) {
	t.Helper()
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	for _, n := range slices.Sorted(maps.Keys(files)) {
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: int64(len(files[n]))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(files[n])); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, b.String())
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// archiveConfig is base with one image entry whose archive is the file S3CRET-VALUE.tar.
func archiveConfig(t *testing.T) string {
	return variant(t, base, "deploy:\n", "images:\n  orders:\n    archive: S3CRET-VALUE.tar\ndeploy:\n")
}

// Review Focus 3.
func TestArchiveThatIsNotAnImage(t *testing.T) {
	for _, tc := range []struct {
		name string
		make func(t *testing.T, path string)
	}{
		{"a folder", func(t *testing.T, p string) { mkdir(t, p) }},
		{"an empty file", func(t *testing.T, p string) { writeFile(t, p, "") }},
		{"not a tar", func(t *testing.T, p string) { writeFile(t, p, "not a tar S3CRET-VALUE\n") }},
		{"an OCI layout with no manifest.json", func(t *testing.T, p string) {
			writeTar(t, p, map[string]string{"oci-layout": `{"imageLayoutVersion":"1.0.0"}`, "index.json": `{"schemaVersion":2,"manifests":[]}`})
		}},
		{"a manifest.json that lists no image", func(t *testing.T, p string) {
			writeTar(t, p, map[string]string{"manifest.json": "[]"})
		}},
		{"a manifest.json whose config is not in the tar", func(t *testing.T, p string) {
			writeTar(t, p, map[string]string{"manifest.json": `[{"Config":"blobs/sha256/absent","RepoTags":[],"Layers":[]}]`})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.make(t, filepath.Join(dir, "S3CRET-VALUE.tar"))
			_, err := loadWith(t, dir, archiveConfig(t), noEnv)
			rejected(t, err, "images.orders.archive (line ")
			rejected(t, err, "): is not an image archive the tool can read")
		})
	}
}

func TestArchiveWithTwoImages(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, filepath.Join(dir, "S3CRET-VALUE.tar"), 2)
	_, err := loadWith(t, dir, archiveConfig(t), noEnv)
	rejected(t, err, "images.orders.archive (line ")
	rejected(t, err, "): holds more than one image")
}

func TestArchiveThatDoesNotExist(t *testing.T) {
	_, err := loadWith(t, t.TempDir(), archiveConfig(t), noEnv)
	rejected(t, err, "images.orders.archive (line ")
	rejected(t, err, "): does not exist")
}

func TestArchiveWithOneImage(t *testing.T) {
	dir, real := project(t)
	writeArchive(t, filepath.Join(dir, "S3CRET-VALUE.tar"), 1)
	c, err := loadWith(t, dir, archiveConfig(t), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := c.Images["orders"].Archive, filepath.Join(real, "S3CRET-VALUE.tar"); got != want {
		t.Errorf("archive %q, want %q", got, want)
	}
}
