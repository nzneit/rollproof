//go:build linux

package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	kindcluster "sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/log"
)

// network returns the name of the engine's network for kind, read as kind's providers read it:
// KIND_EXPERIMENTAL_DOCKER_NETWORK for docker and KIND_EXPERIMENTAL_PODMAN_NETWORK for podman,
// each "kind" when it is not set, and "kind" for every other engine (nerdctl's provider).
func (e Engine) network() string {
	var n string
	switch e.Binary {
	case "docker":
		n = lookupEnv("KIND_EXPERIMENTAL_DOCKER_NETWORK")
	case "podman":
		n = lookupEnv("KIND_EXPERIMENTAL_PODMAN_NETWORK")
	}
	if n == "" {
		return "kind"
	}
	return n
}

// run runs the engine's command line with args on the runner and returns the combined output,
// trimmed. A failure is an error that holds the command line and the last line of the output.
func (e Engine) run(ctx context.Context, args ...string) (string, error) {
	b, err := exec.CommandContext(ctx, e.Binary, args...).CombinedOutput()
	out := strings.TrimSpace(string(b))
	if err != nil {
		last := out
		if i := strings.LastIndexByte(out, '\n'); i >= 0 {
			last = out[i+1:]
		}
		return out, fmt.Errorf("%s %s: %w: %s", e.Binary, strings.Join(args, " "), err, last)
	}
	return out, nil
}

// provider returns kind's provider for the engine, with a logger that discards, made with the
// option that kind's own command line picks for the engine's name.
func (e Engine) provider() *kindcluster.Provider {
	var option kindcluster.ProviderOption
	switch e.Binary {
	case "docker":
		option = kindcluster.ProviderWithDocker()
	case "podman":
		option = kindcluster.ProviderWithPodman()
	default: // nerdctl, finch, nerdctl.lima
		option = kindcluster.ProviderWithNerdctl(e.Binary)
	}
	return kindcluster.NewProvider(kindcluster.ProviderWithLogger(log.NoopLogger{}), option)
}

// address returns the IPv4 address of container on network, from the engine's inspect. When no
// entry is keyed by network and there is exactly one entry, that entry's address: nerdctl may key
// it otherwise.
func (e Engine) address(ctx context.Context, container, network string) (string, error) {
	out, err := e.run(ctx, "inspect", container)
	if err != nil {
		return "", err
	}
	var inspected []struct {
		NetworkSettings struct {
			Networks map[string]struct{ IPAddress string }
		}
	}
	if err := json.Unmarshal([]byte(out), &inspected); err != nil {
		return "", fmt.Errorf("%s inspect %s: %w", e.Binary, container, err)
	}
	if len(inspected) == 0 {
		return "", fmt.Errorf("%s inspect %s gave no entry", e.Binary, container)
	}
	networks := inspected[0].NetworkSettings.Networks
	n, ok := networks[network]
	if !ok && len(networks) == 1 {
		for _, only := range networks {
			n = only
		}
	}
	if n.IPAddress == "" {
		return "", fmt.Errorf("%s has no address on the network %s", container, network)
	}
	return n.IPAddress, nil
}

// removeContainer removes the container name with its anonymous volumes. A container that is
// already gone, which the engine's inspect no longer finds, is not an error.
func (e Engine) removeContainer(ctx context.Context, name string) error {
	_, err := e.run(ctx, "rm", "-f", "-v", name)
	if err == nil {
		return nil
	}
	// An inspect that failed because ctx ended says nothing about the container.
	if _, ierr := e.run(ctx, "inspect", name); ierr != nil && ctx.Err() == nil {
		return nil
	}
	return err
}
