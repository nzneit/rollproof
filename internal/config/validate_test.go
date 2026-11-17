package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
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
		{"a comma in a values file's path", candidateBlock, candidateBlock + "      values:\n        - test/values,S3CRET-VALUE.yaml\n", "deploy.helm.candidate.values[0]"},
		{"a selector that does not parse", ns, ns + "  selector: app in S3CRET-VALUE\n", "deploy.selector"},
		{"a custom probe with no image", httpProbe, "probe:\n  custom:\n    command: [sh, -c, \"echo S3CRET-VALUE\"]\n", "probe.custom.image"},
		{"a header name with an equals sign", "      X-Planted: S3CRET-VALUE\n", "      \"S3CRET-VALUE=x\": v\n", "probe.http.headers (line 17): a header name must be a token"},
		{"a header name with a space", "      X-Planted: S3CRET-VALUE\n", "      \"X Planted\": v\n", "probe.http.headers (line 17): a header name must be a token"},
		{"a header value with a line break", "      X-Planted: S3CRET-VALUE\n", "      X-Planted: \"a\\nS3CRET-VALUE\"\n", "probe.http.headers.X-Planted (line 18): must not hold a line break or NUL"},
		{"a header value with NUL", "      X-Planted: S3CRET-VALUE\n", "      X-Planted: \"a\\0S3CRET-VALUE\"\n", "probe.http.headers.X-Planted (line 18): must not hold a line break or NUL"},
		{"a null values item", candidateBlock, candidateBlock + "      values:\n        - ~\n        - a.yaml\n", "deploy.helm.candidate.values[0] (line 15): must not be empty"},
		{"an empty map as a values item", candidateBlock, candidateBlock + "      values:\n        - {}\n", "deploy.helm.candidate.values[0] (line 15): must not be empty"},
		{"a null registries item", "deploy:\n", registries("  - ~\n"), "registries[0]"},
		{"a null env item", httpProbe, customProbe("    env:\n      - ~\n"), "probe.custom.env[0]"},
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
	dir, real := project(t, "chart")
	t.Chdir(t.TempDir())
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
	// Task 22's later plan reads the archive; this build only makes its path absolute.
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
