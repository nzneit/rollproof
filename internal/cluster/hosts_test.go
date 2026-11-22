package cluster

import (
	"testing"

	"rollproof/internal/config"
)

func TestHostsTOMLGolden(t *testing.T) {
	golden(t, "hosts-test.golden.toml", []byte(HostsTOML("10.89.0.5:5000")))
}

func TestHostSettingsGolden(t *testing.T) {
	const run, ca = "10.89.0.5:5000", "/home/u/certs/corp-ca.pem"
	for _, tc := range []struct {
		file        string
		entry       config.Registry
		runRegistry string
	}{
		{"hosts-as-mirror-ca-credentials.golden.toml", config.Registry{Host: "registry.example", Username: "ci", Password: "pw", CA: ca, Mirror: "mirror.corp.example/registry-example"}, run},
		{"hosts-mirror-prefix.golden.toml", config.Registry{Host: "registry.example", Mirror: "mirror.corp.example/registry-example"}, ""},
		{"hosts-mirror-v2.golden.toml", config.Registry{Host: "registry.example", CA: ca, Mirror: "cache.corp.example/v2/proxy-project"}, ""},
		{"hosts-mirror-v2-only.golden.toml", config.Registry{Host: "registry.example", Mirror: "mirror.example/v2"}, ""},
		{"hosts-mirror-prefix-v2.golden.toml", config.Registry{Host: "registry.example", Mirror: "mirror.corp.example/cache/v2"}, ""},
		{"hosts-mirror-root.golden.toml", config.Registry{Host: "registry.example", Mirror: "mirror.corp.example:8443"}, ""},
		{"hosts-docker-io-mirror.golden.toml", config.Registry{Host: "docker.io", Username: "ci", Password: "pw", CA: ca, Mirror: "mirror.corp.example/docker-io"}, run},
		{"hosts-docker-io-ca.golden.toml", config.Registry{Host: "docker.io", CA: ca}, ""},
		{"hosts-ca.golden.toml", config.Registry{Host: "registry.example", CA: ca}, ""},
		{"hosts-as-ca.golden.toml", config.Registry{Host: "registry.example", CA: ca}, run},
		{"hosts-as-port-ca.golden.toml", config.Registry{Host: "registry.example:5000", CA: ca}, run},
		{"hosts-ipv6-port-ca.golden.toml", config.Registry{Host: "[fd00::5]:5000", CA: ca}, ""},
	} {
		t.Run(tc.file, func(t *testing.T) { golden(t, tc.file, []byte(HostSettings(tc.entry, tc.runRegistry))) })
	}
}

func TestHostSettingsNeedsNoFile(t *testing.T) {
	for _, entry := range []config.Registry{
		{Host: "docker.io", Username: "ci", Password: "pw"},
		{Host: "registry.example"},
	} {
		if got := HostSettings(entry, ""); got != "" {
			t.Errorf("HostSettings(%+v, \"\") = %q, want no file", entry, got)
		}
	}
}

func TestHostSettingsOfAnAsHostWithNoEntry(t *testing.T) {
	if got, want := HostSettings(config.Registry{Host: "registry.example"}, "10.89.0.5:5000"), HostsTOML("10.89.0.5:5000"); got != want {
		t.Errorf("HostSettings = %q, want HostsTOML %q", got, want)
	}
}

func TestHostSettingsTakesNothingFromAnotherEntry(t *testing.T) {
	// Whatever an entry for b.example holds gives the entry that names it as a mirror nothing
	// (section 9).
	const want = "server = \"https://b.example/x\"\n"
	if got := HostSettings(config.Registry{Host: "a.example", Mirror: "b.example/x"}, ""); got != want {
		t.Errorf("HostSettings = %q, want %q", got, want)
	}
}
