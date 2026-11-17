//go:build linux

package cluster

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"
)

// DefaultRegistryImage is the registry image the tool starts when the configuration names none,
// pinned by digest.
//
// The digest is the one that `nerdctl images --digests` listed for docker.io/library/registry:3
// in the engine's store on 2026-11-16.
const DefaultRegistryImage = "docker.io/library/registry:3@sha256:82f6ff4a7f982960c8aa51617fea3aabc485df6e55cce80d22deb1de36f1bc3c"

// registryPoll is how often startRegistry asks the registry whether it answers; registryRequest is
// how long one request may take, so that a request that hangs does not hold up the next.
const (
	registryPoll    = 200 * time.Millisecond
	registryRequest = time.Second
)

// startRegistry starts the container NAME-registry from image on the engine's network for kind,
// published on a free port of the runner's loopback, and returns when GET /v2/ answers 200. push
// is the registry as the runner reaches it, 127.0.0.1:PORT; node is the same registry as the
// nodes reach it, ADDRESS:5000. Past deadline it returns an error that says the registry did not
// answer.
func startRegistry(ctx context.Context, e Engine, name, image string, deadline time.Time) (push, node string, err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", "", err
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	ln.Close()

	container, network := name+"-registry", e.network()
	if _, err := e.run(ctx, "run", "-d", "--name", container, "--network", network, "-p", "127.0.0.1:"+port+":5000", image); err != nil {
		return "", "", err
	}
	address, err := e.address(ctx, container, network)
	if err != nil {
		return "", "", err
	}
	push = "127.0.0.1:" + port
	if err := waitForRegistry(ctx, "http://"+push+"/v2/", deadline); err != nil {
		return "", "", err
	}
	return push, net.JoinHostPort(address, "5000"), nil
}

// waitForRegistry sends GET url every registryPoll until one answers 200. Past deadline it returns
// an error that says the registry did not answer, with the last result.
func waitForRegistry(ctx context.Context, url string, deadline time.Time) error {
	waitCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	client := &http.Client{Timeout: registryRequest}
	tick := time.NewTicker(registryPoll)
	defer tick.Stop()
	for {
		req, err := http.NewRequestWithContext(waitCtx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			err = errors.New(resp.Status)
		}
		select {
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("the registry did not answer GET %s with 200 in time; the last answer: %v", url, err)
		case <-tick.C:
		}
	}
}
