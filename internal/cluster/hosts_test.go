package cluster

import "testing"

func TestHostsTOMLGolden(t *testing.T) {
	golden(t, "hosts-test.golden.toml", []byte(HostsTOML("10.89.0.5:5000")))
}
