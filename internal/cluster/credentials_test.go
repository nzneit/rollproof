package cluster

import (
	"testing"

	"rollproof/internal/config"
)

func TestCredentialsGolden(t *testing.T) {
	regs := []config.Registry{
		{Host: "registry.example", Username: "ci2", Password: "pw2"},
		{Host: "docker.io", Username: "ci", Password: "pw1"},
		{Host: "mirror-only.example", Mirror: "mirror.corp.example/x"},
	}
	golden(t, "credentials.golden.json", Credentials(regs))

	if got := Credentials(nil); got != nil {
		t.Errorf("Credentials(nil) = %q, want nil", got)
	}
	if got := Credentials(regs[2:]); got != nil {
		t.Errorf("Credentials of an entry without credentials = %q, want nil", got)
	}
}
