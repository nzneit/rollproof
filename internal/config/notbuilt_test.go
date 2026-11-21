package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestNotBuilt(t *testing.T) {
	dir := t.TempDir()
	writeArchive(t, filepath.Join(dir, "orders.tar"), 1)
	for _, tc := range []struct {
		name, old, new string
		want           []string
	}{
		{"everything built", "deploy:\n", "deploy:\n", nil},
		{"registries", "deploy:\n", "registries:\n  - host: registry.example\ndeploy:\n", nil},
		{"setup", "deploy:\n", "setup:\n  - [kubectl, apply, -f, test/prerequisites.yaml]\ndeploy:\n", nil},
		{"the commands driver", helmBlock, commandsBlock("    install: [kubectl, apply, -k, overlays/previous]\n" +
			"    deploy: [kubectl, apply, -k, overlays/candidate]\n    rollback: [kubectl, apply, -k, overlays/previous]\n"), nil},
		{"a custom probe", httpProbe, customProbe(""), nil},
		{"an image from an archive", "deploy:\n", "images:\n  orders:\n    archive: orders.tar\ndeploy:\n", nil},
		{"an image from an archive with as", "deploy:\n", "images:\n  orders:\n    archive: orders.tar\n    as: registry.example/team/orders\ndeploy:\n", []string{"images.orders.as"}},
		{"an image with as", "deploy:\n", "images:\n  orders:\n    local: orders:candidate\n    as: registry.example/team/orders\ndeploy:\n", []string{"images.orders.as"}},
		{"a local image", "deploy:\n", "images:\n  orders:\n    local: orders:candidate\ndeploy:\n", nil},
		{"no previous release", previousBlock, "", nil},
		{"several at once", "deploy:\n", "registries:\n  - host: registry.example\nsetup:\n  - [kubectl, apply, -f, test/prerequisites.yaml]\n" +
			"images:\n  orders:\n    local: orders:candidate\n    as: registry.example/team/orders\ndeploy:\n", []string{"images.orders.as"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := loadWith(t, dir, variant(t, base, tc.old, tc.new), noEnv)
			if err != nil {
				t.Fatal(err)
			}
			if d := cmp.Diff(tc.want, c.NotBuilt(), cmpopts.EquateEmpty()); d != "" {
				t.Errorf("NotBuilt (-want +got):\n%s", d)
			}
		})
	}
}

func TestNotBuiltMinimalWithAPrevious(t *testing.T) {
	path := fromTestdata(t, "minimal.yaml")
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	withPrevious := variant(t, string(text), "    candidate:\n", previousBlock+"    candidate:\n")
	c, err := loadWith(t, filepath.Dir(path), withPrevious, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.NotBuilt(); len(got) != 0 {
		t.Errorf("NotBuilt = %q, want none", got)
	}
}
