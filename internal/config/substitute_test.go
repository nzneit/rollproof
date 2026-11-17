package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// envOf looks names up in m.
func envOf(m map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := m[name]
		return v, ok
	}
}

// withInline adds one inline values map, written in flow style, to the candidate.
func withInline(t *testing.T, text, inline string) string {
	t.Helper()
	return variant(t, text, candidateBlock, candidateBlock+"      values:\n        - "+inline+"\n")
}

// withImage adds the images entry orders, a local image, to text.
func withImage(t *testing.T, text string) string {
	t.Helper()
	return variant(t, text, "deploy:\n", "images:\n  orders:\n    local: orders:candidate\ndeploy:\n")
}

func TestSubstituteFills(t *testing.T) {
	inline := func(c *Config) any { return c.Deploy.Helm.Candidate.Values[0].Inline }
	for _, tc := range []struct {
		name, text string
		env        map[string]string
		get        func(c *Config) any
		want       any
	}{
		{"a whole value", variant(t, base, "  namespace: orders\n", "  namespace: ${env.NS}\n"),
			map[string]string{"NS": "orders"}, func(c *Config) any { return c.Deploy.Namespace }, "orders"},
		{"text around a form", withInline(t, base, `{ note: "a-${env.A}-b" }`),
			map[string]string{"A": "x"}, inline, map[string]any{"note": "a-x-b"}},
		{"$${ is a literal ${", withInline(t, base, `{ note: "$${env.X}" }`),
			nil, inline, map[string]any{"note": "${env.X}"}},
		{"one pass", withInline(t, base, `{ note: "${env.A}" }`),
			map[string]string{"A": "${env.OTHER}"}, inline, map[string]any{"note": "${env.OTHER}"}},
		{"any depth, and no key", withInline(t, base, `{ "${env.K}": { list: ["${env.V}", 3] } }`),
			map[string]string{"K": "key", "V": "value"}, inline, map[string]any{"${env.K}": map[string]any{"list": []any{"value", 3}}}},
		{"a chart version", variant(t, base, "      version: \"1.4.2\"\n", "      version: ${env.PREVIOUS_VERSION}\n"),
			map[string]string{"PREVIOUS_VERSION": "1.10"}, func(c *Config) any { return c.Deploy.Helm.Previous.Version }, "1.10"},
		{"a password", variant(t, base, "deploy:\n", "registries:\n  - host: registry.example\n    username: ci\n    password: ${env.REG_PW}\ndeploy:\n"),
			map[string]string{"REG_PW": planted}, func(c *Config) any { return c.Registries[0].Password }, planted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := loadWith(t, t.TempDir(), tc.text, envOf(tc.env))
			if err != nil {
				t.Fatal(err)
			}
			if d := cmp.Diff(tc.want, tc.get(c)); d != "" {
				t.Errorf("(-want +got):\n%s", d)
			}
		})
	}
}

func TestSubstituteRejects(t *testing.T) {
	note := func(form string) string { return withInline(t, base, `{ note: "`+form+`" }`) }
	image := withImage(t, base)
	registry := func(password string) string {
		return variant(t, base, "deploy:\n", "registries:\n  - host: registry.example\n    username: ci\n    password: "+password+"\ndeploy:\n")
	}
	for _, tc := range []struct {
		name, text string
		env        map[string]string
		want       string
	}{
		{"a form in a number field", variant(t, base, "  workers: 3\n", "  workers: ${env.N}\n"), map[string]string{"N": planted}, "cluster.workers"},
		{"a variable that is not set", variant(t, base, "  namespace: orders\n", "  namespace: ${env.NS}\n"), nil, "NS"},
		{"a variable set to nothing", variant(t, base, "  namespace: orders\n", "  namespace: ${env.NS}\n"), map[string]string{"NS": ""}, "NS"},
		{"${foo}", note("${foo}"), nil, "values[0].note"},
		{"${env.}", note("${env.}"), nil, "values[0].note"},
		{"${ENV.X}", note("${ENV.X}"), map[string]string{"X": planted}, "values[0].note"},
		{"${images.orders}", withImage(t, note("${images.orders}")), nil, "values[0].note"},
		{"${images.orders.tag}", withImage(t, note("${images.orders.tag}")), nil, "values[0].note"},
		{"an unclosed form", note("S3CRET-VALUE ${env.X"), map[string]string{"X": planted}, "values[0].note"},
		{"an image form under cluster", variant(t, image, "  workers: 3\n", "  node-image: ${images.orders.ref}\n  workers: 3\n"), nil, "cluster.node-image"},
		{"an image form under registries", variant(t, image, "images:\n", "registries:\n  - host: registry.example\n    mirror: ${images.orders.repository}\nimages:\n"), nil, "registries[0].mirror"},
		{"an image form under images", variant(t, image, "    local: orders:candidate\n", "    local: orders:candidate\n    as: ${images.orders.repository}\n"), nil, "images.orders.as"},
		{"an image form under limits", variant(t, image, "  steady: 10s\n", "  steady: ${images.orders.digest}\n"), nil, "limits.steady"},
		{"an image that images does not list", withImage(t, note("${images.nope.digest}")), nil, "nope"},
		{"a password written in the file", registry(planted), nil, "registries[0].password"},
		{"a password with text beside the form", registry("x${env.P}"), map[string]string{"P": planted}, "registries[0].password"},
		{"a password from a variable that is not set", registry("${env.P}"), nil, "P"},
		{"a later rejection after a password was taken", variant(t, registry("${env.P}"), "deploy:\n", "  - host: registry.example\ndeploy:\n"),
			map[string]string{"P": planted}, "registries[1].host"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadWith(t, t.TempDir(), tc.text, envOf(tc.env))
			rejected(t, err, tc.want)
		})
	}
}

func TestValuesFilesAreNotRead(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "values.yaml"), []byte("image: ${env.NOPE}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	text := variant(t, base, candidateBlock, candidateBlock+"      values:\n        - values.yaml\n")
	if _, err := loadWith(t, dir, text, noEnv); err != nil {
		t.Fatal(err)
	}
}

func TestResolve(t *testing.T) {
	text := withImage(t, withInline(t, base, `{ image: { digest: "${images.orders.digest}", repository: "${images.orders.repository}",`+
		` ref: "${images.orders.ref}", tagged: "candidate@${images.orders.digest}" } }`))
	text = variant(t, text, "deploy:\n", "setup:\n  - [kubectl, set, image, deploy/x, \"app=${images.orders.ref}\"]\ndeploy:\n")
	text = variant(t, text, "      X-Planted: S3CRET-VALUE\n", "      X-Planted: S3CRET-VALUE\n      X-Image: ${images.orders.digest}\n")
	dir := t.TempDir()
	load := func() *Config {
		c, err := loadWith(t, dir, text, noEnv)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	stored := map[string]ImageRef{"orders": {Repository: "rollproof.test/orders", Digest: "sha256:abc"}}

	c := load()
	image := func(c *Config) any { return c.Deploy.Helm.Candidate.Values[0].Inline["image"] }
	before := map[string]any{"digest": "${images.orders.digest}", "repository": "${images.orders.repository}",
		"ref": "${images.orders.ref}", "tagged": "candidate@${images.orders.digest}"}
	if d := cmp.Diff(before, image(c)); d != "" {
		t.Errorf("Load filled an image form (-want +got):\n%s", d)
	}
	if err := c.Resolve(stored); err != nil {
		t.Fatal(err)
	}
	after := map[string]any{"digest": "sha256:abc", "repository": "rollproof.test/orders",
		"ref": "rollproof.test/orders@sha256:abc", "tagged": "candidate@sha256:abc"}
	if d := cmp.Diff(after, image(c)); d != "" {
		t.Errorf("after Resolve (-want +got):\n%s", d)
	}
	if got := c.Setup[0][4]; got != "app=rollproof.test/orders@sha256:abc" {
		t.Errorf("setup argument %q", got)
	}
	if got := c.Probe.HTTP.Headers["X-Image"]; got != "sha256:abc" {
		t.Errorf("header %q", got)
	}

	twice := load()
	for range 2 {
		if err := twice.Resolve(stored); err != nil {
			t.Fatal(err)
		}
	}
	if d := cmp.Diff(c, twice); d != "" {
		t.Errorf("a second Resolve changed the configuration (-once +twice):\n%s", d)
	}

	if err := load().Resolve(map[string]ImageRef{}); err == nil || !strings.Contains(err.Error(), "orders") {
		t.Errorf("Resolve with no stored image: %v, want an error that names orders", err)
	}
	if got := (ImageRef{Repository: "rollproof.test/orders", Digest: "sha256:abc"}).Ref(); got != "rollproof.test/orders@sha256:abc" {
		t.Errorf("Ref() = %q", got)
	}
}

func TestResolveKeepsALiteralBesideAForm(t *testing.T) {
	c, err := loadWith(t, t.TempDir(), withImage(t, withInline(t, base, `{ both: "$${images.orders.digest} is ${images.orders.digest}" }`)), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	both := func() any { return c.Deploy.Helm.Candidate.Values[0].Inline["both"] }
	if got := both(); got != "$${images.orders.digest} is ${images.orders.digest}" {
		t.Errorf("after Load %q, want $${ kept beside the form", got)
	}
	if err := c.Resolve(map[string]ImageRef{"orders": {Repository: "rollproof.test/orders", Digest: "sha256:abc"}}); err != nil {
		t.Fatal(err)
	}
	if got := both(); got != "${images.orders.digest} is sha256:abc" {
		t.Errorf("after Resolve %q", got)
	}
}

func TestSettings(t *testing.T) {
	c, err := Load(fromTestdata(t, "minimal.yaml"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	want := []Setting{
		{"cluster.node-image", "the linked kind release's", "the linked kind release's"},
		{"cluster.registry-image", "the tool's pinned registry image", "the tool's pinned registry image"},
		{"cluster.workers", "3", "3"},
		{"probe.http.rate", "200", "200"},
		{"probe.http.timeout", "2s", "2s"},
		{"probe.http.expect-status", "200-299", "200-299"},
		{"limits.steady", "10s", "10s"},
		{"limits.settle", "15s", "15s"},
		{"limits.timeout", "5m", "5m"},
		{"limits.silence", "5s", "5s"},
	}
	if d := cmp.Diff(want, c.Settings()); d != "" {
		t.Errorf("minimal.yaml (-want +got):\n%s", d)
	}

	text := variant(t, base, "  workers: 3\n", "  node-image: kindest/node:v1.37.0@"+digestA+"\n  workers: 4\n")
	text = variant(t, text, "    rate: 200\n", "    rate: 100\n")
	text = variant(t, text, "  steady: 10s\n", "  steady: 5m\n")
	c, err = loadText(t, text)
	if err != nil {
		t.Fatal(err)
	}
	var changed []Setting
	for _, s := range c.Settings() {
		if strings.Contains(s.Value, planted) {
			t.Errorf("setting %s holds a header's value", s.Key)
		}
		if s.Value != s.Default {
			changed = append(changed, s)
		}
	}
	wantChanged := []Setting{
		{"cluster.node-image", "kindest/node:v1.37.0@" + digestA, "the linked kind release's"},
		{"cluster.workers", "4", "3"},
		{"probe.http.rate", "100", "200"},
		{"limits.steady", "5m", "10s"},
	}
	if d := cmp.Diff(wantChanged, changed); d != "" {
		t.Errorf("changed settings (-want +got):\n%s", d)
	}

	c, err = loadText(t, variant(t, base, "    expect-status: 200-299\n", "    expect-status: 200\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Settings()[5]; got.Key != "probe.http.expect-status" || got.Value != "200" {
		t.Errorf("one status code prints as %+v", got)
	}

	c, err = loadText(t, variant(t, base, httpProbe, customProbe("")))
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, s := range c.Settings() {
		keys = append(keys, s.Key)
	}
	wantKeys := []string{"cluster.node-image", "cluster.registry-image", "cluster.workers", "limits.steady", "limits.settle", "limits.timeout", "limits.silence"}
	if d := cmp.Diff(wantKeys, keys); d != "" {
		t.Errorf("custom probe (-want +got):\n%s", d)
	}
}

func TestResolveKeepsAnEscapedFormAlone(t *testing.T) {
	text := withImage(t, withInline(t, base, `{ lone: "$${images.orders.digest}", fromEnv: "${env.A}" }`))
	c, err := loadWith(t, t.TempDir(), text, envOf(map[string]string{"A": "${images.orders.digest}"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Resolve(map[string]ImageRef{"orders": {Repository: "rollproof.test/orders", Digest: "sha256:abc"}}); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"lone": "${images.orders.digest}", "fromEnv": "${images.orders.digest}"}
	if d := cmp.Diff(want, c.Deploy.Helm.Candidate.Values[0].Inline); d != "" {
		t.Errorf("(-want +got):\n%s", d)
	}
}

func TestSubstituteRejectsAProgramMadeEmpty(t *testing.T) {
	text := variant(t, base, "deploy:\n", "setup:\n  - [\"${env.CMD}\"]\ndeploy:\n")
	_, err := loadWith(t, t.TempDir(), text, envOf(map[string]string{"CMD": ""}))
	rejected(t, err, "CMD")
}

func TestSubstituteRejectsAliasesAndTheRest(t *testing.T) {
	anchored := variant(t, base, "  namespace: orders\n", "  namespace: &ref \"${images.orders.ref}\"\n")
	toNodeImage := variant(t, anchored, "cluster:\n  workers: 3\n", "")
	toNodeImage = withImage(t, toNodeImage+"cluster:\n  node-image: *ref\n  workers: 3\n")
	registries := "registries:\n  - &r { host: registry.example, username: ci, password: S3CRET-VALUE }\n  - <<: *r\n"
	for _, tc := range []struct {
		name, text string
		env        map[string]string
		want       string
	}{
		{"an aliased password", variant(t, variant(t, base, "  workers: 3\n", "  workers: 3\n  node-image: &pw S3CRET-VALUE\n"), "deploy:\n",
			"registries:\n  - host: registry.example\n    username: ci\n    password: *pw\ndeploy:\n"), nil, "registries[0].password"},
		{"a merge key", variant(t, base, "deploy:\n", registries+"deploy:\n"), nil, "registries[1]"},
		{"an alias into cluster.node-image", toNodeImage, nil, "cluster.node-image"},
		{"an alias into deploy", variant(t, variant(t, base, "  workers: 3\n", "  workers: 3\n  node-image: &lit S3CRET-VALUE\n"),
			"  namespace: orders\n", "  namespace: *lit\n"), nil, "deploy.namespace"},
		{"an alias used as a key", variant(t, variant(t, base, "  workers: 3\n", "  workers: 3\n  node-image: &lit S3CRET-VALUE\n"),
			"  namespace: orders\n", "  namespace: orders\n  *lit : x\n"), nil, "deploy."},
		{"a variable that ends in $ before {", variant(t, base, "deploy:\n", "setup:\n  - [\"a${env.A}{b\"]\ndeploy:\n"),
			map[string]string{"A": planted + "$"}, "the variable A ends in $ before {"},
		{"an image name that is not a name", withImage(t, withInline(t, base, `{ note: "${images.S3CRET-VALUE.digest}" }`)), nil, "values[0].note"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadWith(t, t.TempDir(), tc.text, envOf(tc.env))
			rejected(t, err, tc.want)
			if strings.HasPrefix(tc.name, "an alias") || tc.name == "a merge key" {
				rejected(t, err, "YAML aliases and merge keys are not accepted")
			}
		})
	}
}

func TestAnUnusedAnchorIsAccepted(t *testing.T) {
	if _, err := loadText(t, variant(t, base, "  workers: 3\n", "  workers: &w 3\n")); err != nil {
		t.Fatal(err)
	}
}

func TestResolveFillsEveryPath(t *testing.T) {
	const form, filled = "${images.orders.ref}", "rollproof.test/orders@sha256:abc"
	stored := map[string]ImageRef{"orders": {Repository: "rollproof.test/orders", Digest: "sha256:abc"}}
	deepMap := func() map[string]any {
		return map[string]any{"a": []any{map[string]any{"b": form}}}
	}
	deepWant := map[string]any{"a": []any{map[string]any{"b": filled}}}
	for _, tc := range []struct {
		name string
		set  func(c *Config)
		get  func(c *Config) any
		want any
	}{
		{"setup", func(c *Config) { c.Setup = [][]string{{"kubectl", form}} }, func(c *Config) any { return c.Setup[0][1] }, filled},
		{"namespace", func(c *Config) { c.Deploy.Namespace = form }, func(c *Config) any { return c.Deploy.Namespace }, filled},
		{"selector", func(c *Config) { c.Deploy.Selector = "app=" + form }, func(c *Config) any { return c.Deploy.Selector }, "app=" + filled},
		{"release", func(c *Config) { c.Deploy.Helm.Release = form }, func(c *Config) any { return c.Deploy.Helm.Release }, filled},
		{"chart", func(c *Config) { c.Deploy.Helm.Candidate.Chart = form }, func(c *Config) any { return c.Deploy.Helm.Candidate.Chart }, filled},
		{"version", func(c *Config) { c.Deploy.Helm.Previous.Version = form }, func(c *Config) any { return c.Deploy.Helm.Previous.Version }, filled},
		{"values file", func(c *Config) { c.Deploy.Helm.Candidate.Values = []Value{{File: "/x/" + form}} },
			func(c *Config) any { return c.Deploy.Helm.Candidate.Values[0].File }, "/x/" + filled},
		{"inline at depth", func(c *Config) { c.Deploy.Helm.Candidate.Values = []Value{{Inline: deepMap()}} },
			func(c *Config) any { return c.Deploy.Helm.Candidate.Values[0].Inline }, deepWant},
		{"commands install", func(c *Config) { c.Deploy.Commands = &Commands{Install: []string{"a", form}} },
			func(c *Config) any { return c.Deploy.Commands.Install[1] }, filled},
		{"commands deploy", func(c *Config) { c.Deploy.Commands = &Commands{Deploy: []string{"a", form}} },
			func(c *Config) any { return c.Deploy.Commands.Deploy[1] }, filled},
		{"commands rollback", func(c *Config) { c.Deploy.Commands = &Commands{Rollback: []string{"a", form}} },
			func(c *Config) any { return c.Deploy.Commands.Rollback[1] }, filled},
		{"probe url", func(c *Config) { c.Probe.HTTP.URL = "http://h/" + form }, func(c *Config) any { return c.Probe.HTTP.URL }, "http://h/" + filled},
		{"header value", func(c *Config) { c.Probe.HTTP.Headers["X-Planted"] = form },
			func(c *Config) any { return c.Probe.HTTP.Headers["X-Planted"] }, filled},
		{"custom image", func(c *Config) { c.Probe.Custom = &Custom{Image: form} }, func(c *Config) any { return c.Probe.Custom.Image }, filled},
		{"custom command", func(c *Config) { c.Probe.Custom = &Custom{Command: []string{"a", form}} },
			func(c *Config) any { return c.Probe.Custom.Command[1] }, filled},
		{"custom args", func(c *Config) { c.Probe.Custom = &Custom{Args: []string{form}} }, func(c *Config) any { return c.Probe.Custom.Args[0] }, filled},
		{"env name", func(c *Config) { c.Probe.Custom = &Custom{Env: []EnvVar{{Name: form}}} }, func(c *Config) any { return c.Probe.Custom.Env[0].Name }, filled},
		{"env value", func(c *Config) { c.Probe.Custom = &Custom{Env: []EnvVar{{Name: "N", Value: form}}} },
			func(c *Config) any { return c.Probe.Custom.Env[0].Value }, filled},
		{"env valueFrom", func(c *Config) { c.Probe.Custom = &Custom{Env: []EnvVar{{Name: "N", ValueFrom: deepMap()}}} },
			func(c *Config) any { return c.Probe.Custom.Env[0].ValueFrom }, deepWant},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := loadText(t, withImage(t, base))
			if err != nil {
				t.Fatal(err)
			}
			tc.set(c)
			if err := c.Resolve(stored); err != nil {
				t.Fatal(err)
			}
			if d := cmp.Diff(tc.want, tc.get(c)); d != "" {
				t.Errorf("(-want +got):\n%s", d)
			}
			raw, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "${images.") {
				t.Errorf("a form is left in the configuration: %s", raw)
			}
		})
	}

	t.Run("a missing name fills nothing", func(t *testing.T) {
		c, err := loadText(t, withImage(t, base))
		if err != nil {
			t.Fatal(err)
		}
		c.Deploy.Namespace = form
		c.Setup = [][]string{{"a", "${images.nope.ref}"}}
		err = c.Resolve(stored)
		if err == nil || !strings.Contains(err.Error(), "nope") {
			t.Fatalf("error %v, want one that names nope", err)
		}
		if c.Deploy.Namespace != form || c.Setup[0][1] != "${images.nope.ref}" {
			t.Errorf("Resolve filled part of the configuration: %q, %q", c.Deploy.Namespace, c.Setup[0][1])
		}
	})

	t.Run("an environment value that holds $${ in keep mode", func(t *testing.T) {
		const value = "$${images.orders.ref}"
		text := withImage(t, withInline(t, base, `{ note: "${env.A} ${images.orders.ref}" }`))
		c, err := loadWith(t, t.TempDir(), text, envOf(map[string]string{"A": value}))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Resolve(stored); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"note": value + " " + filled}
		if d := cmp.Diff(want, c.Deploy.Helm.Candidate.Values[0].Inline); d != "" {
			t.Errorf("(-want +got):\n%s", d)
		}
	})
}

func TestFormatDuration(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Minute, "5m"},
		{2 * time.Minute, "2m"},
		{10 * time.Second, "10s"},
		{90 * time.Second, "90s"},
		{500 * time.Millisecond, "500ms"},
		{1500 * time.Millisecond, "1500ms"},
	} {
		if got := formatDuration(tc.d); got != tc.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
