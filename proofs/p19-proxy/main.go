//go:build linux

// Proof of point 19: a run behind an HTTP proxy. The tool runs the first
// fixture, as the e2e suite runs it, with HTTP_PROXY and HTTPS_PROXY naming a
// logging forward proxy on the engine's network for kind. The proof shows the
// verdict; that the proxy had no request for the run's registry; what the
// container runtime in a node holds in its environment; and that a node's pull
// of a public image did go through the proxy. The kept cluster, its registry
// and the proxy are removed at the end.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"text/template"
	"time"
)

const (
	// fwdProxyGo is the logging forward proxy: CONNECT for HTTPS, and plain
	// HTTP requests in the absolute form. It writes one line for each request
	// with its target.
	fwdProxyGoMod = "module p19proxy\n\ngo 1.27\n"
	fwdProxyGo    = `package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

var mu sync.Mutex

func logf(format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	fmt.Printf(format+"\n", args...)
}

func main() {
	listen := flag.String("listen", ":3128", "the address to listen on")
	flag.Parse()
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			logf("REQ CONNECT %s from %s", r.Host, r.RemoteAddr)
			dst, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
			if err != nil {
				logf("ERR CONNECT %s: %v", r.Host, err)
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			hj, ok := w.(http.Hijacker)
			if !ok {
				dst.Close()
				http.Error(w, "cannot hijack", http.StatusInternalServerError)
				return
			}
			src, buf, err := hj.Hijack()
			if err != nil {
				dst.Close()
				return
			}
			if _, err := src.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n")); err != nil {
				src.Close()
				dst.Close()
				return
			}
			done := make(chan struct{}, 2)
			go func() { io.Copy(dst, buf.Reader); done <- struct{}{} }()
			go func() { io.Copy(src, dst); done <- struct{}{} }()
			<-done
			src.Close()
			dst.Close()
			<-done
			return
		}
		logf("REQ %s %s host=%s from %s", r.Method, r.URL.String(), r.Host, r.RemoteAddr)
		if !r.URL.IsAbs() {
			http.Error(w, "not a proxy request", http.StatusBadRequest)
			return
		}
		out := r.Clone(r.Context())
		out.RequestURI = ""
		out.Header.Del("Proxy-Connection")
		out.Header.Del("Proxy-Authorization")
		resp, err := transport.RoundTrip(out)
		if err != nil {
			logf("ERR %s: %v", r.URL, err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	})
	logf("fwdproxy listening on %s", *listen)
	if err := http.ListenAndServe(*listen, h); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`

	runName       = "proof-p19"           // the tool's --name
	clusterName   = "rollproof-proof-p19" // the cluster that the tool makes
	prefix        = clusterName + "-"     // every container of this proof
	proxyName     = prefix + "proxy"
	registryName  = prefix + "registry"
	proxyPort     = "3128"
	carrierImage  = "docker.io/library/registry:3"
	publicImage   = "docker.io/library/busybox:1.37.0"
	network       = "kind"
	helmMajor     = "helm-3"
	runLimit      = 20 * time.Minute
	serviceSubnet = "10.96.0.0/16"  // kind's default
	podSubnet     = "10.244.0.0/16" // kind's default

	// configTemplate is the e2e suite's run description (e2e/config_test.go), unchanged.
	configTemplate = `version: 1
cluster: {{.Cluster}}
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
)

func main() { os.Exit(run()) }

// checks counts the failed checks and prints one line for each.
type checks struct{ failed int }

func (c *checks) ok(format string, args ...any) {
	fmt.Println("ok: " + shown(fmt.Sprintf(format, args...)))
}

func (c *checks) fail(format string, args ...any) {
	c.failed++
	fmt.Println("FAILED: " + shown(fmt.Sprintf(format, args...)))
}

// check prints ok or FAILED for one condition.
func (c *checks) check(held bool, format string, args ...any) {
	if held {
		c.ok(format, args...)
	} else {
		c.fail(format, args...)
	}
}

func info(format string, args ...any) { fmt.Println("info: " + shown(fmt.Sprintf(format, args...))) }

// shown writes the home folder at the start of a path as $HOME.
func shown(s string) string {
	if home := os.Getenv("HOME"); home != "" {
		return strings.ReplaceAll(s, home+string(filepath.Separator), "$HOME"+string(filepath.Separator))
	}
	return s
}

// the checks that must have run, by name; a missing one counts as failed.
var required = []string{"verdict", "registry-not-proxied", "node-env", "public-pull", "cleanup"}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c := &checks{}
	done := map[string]bool{}
	root, err := filepath.Abs("..")
	if err != nil {
		c.fail("the project's folder: %v", err)
		return summary(ctx, c, done)
	}
	work, err := os.MkdirTemp("", "rollproof-proof-p19-")
	if err != nil {
		c.fail("a temporary folder: %v", err)
		return summary(ctx, c, done)
	}
	defer os.RemoveAll(work)
	prove(ctx, c, done, root, work)
	return summary(ctx, c, done)
}

// engine runs nerdctl with args and returns its standard output; an error
// carries everything it wrote.
func engine(ctx context.Context, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "nerdctl", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("nerdctl %s: %w\n%s%s", strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), nil
}

// ours returns the engine's containers whose names begin with prefix.
func ours(ctx context.Context) ([]string, error) {
	out, err := engine(ctx, "ps", "-a", "--format", "{{.Names}}")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, n := range strings.Fields(out) {
		if strings.HasPrefix(n, prefix) {
			names = append(names, n)
		}
	}
	return names, nil
}

// volumes counts the engine's volumes.
func volumes(ctx context.Context) (int, error) {
	out, err := engine(ctx, "volume", "ls", "-q")
	return len(strings.Fields(out)), err
}

// address returns the IPv4 address of the container on the network kind.
func address(ctx context.Context, container string) (string, error) {
	out, err := engine(ctx, "inspect", container)
	if err != nil {
		return "", err
	}
	var inspected []struct {
		NetworkSettings struct {
			Networks map[string]struct{ IPAddress string }
		}
		Config struct{ Env []string }
	}
	if err := json.Unmarshal([]byte(out), &inspected); err != nil || len(inspected) != 1 {
		return "", fmt.Errorf("reading nerdctl inspect %s: %v", container, err)
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

// containerEnv returns the environment that the engine gave the container.
func containerEnv(ctx context.Context, container string) ([]string, error) {
	out, err := engine(ctx, "inspect", "--format", "{{json .Config.Env}}", container)
	if err != nil {
		return nil, err
	}
	var env []string
	err = json.Unmarshal([]byte(strings.TrimSpace(out)), &env)
	return env, err
}

// proxyLog returns the proxy container's log.
func proxyLog(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "nerdctl", "logs", proxyName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("nerdctl logs %s: %w\n%s", proxyName, err, out)
	}
	return string(out), nil
}

// settledProxyLog returns the proxy's log once two reads 500 ms apart agree,
// or the last read after 5 seconds.
func settledProxyLog(ctx context.Context) (string, error) {
	prev, err := proxyLog(ctx)
	if err != nil {
		return "", err
	}
	for range 10 {
		time.Sleep(500 * time.Millisecond)
		cur, err := proxyLog(ctx)
		if err != nil {
			return "", err
		}
		if cur == prev {
			return cur, nil
		}
		prev = cur
	}
	return prev, nil
}

// requestLines returns the proxy's REQ and ERR lines of text.
func requestLines(text string) []string {
	var lines []string
	for line := range strings.Lines(text) {
		if strings.HasPrefix(line, "REQ ") || strings.HasPrefix(line, "ERR ") {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return lines
}

// goBuild runs go build -trimpath with args in dir, with CGO_ENABLED=0.
func goBuild(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "go", append([]string{"build", "-trimpath"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}

// prove does the work; everything it starts is removed before it returns.
func prove(ctx context.Context, c *checks, done map[string]bool, root, work string) {
	for _, tool := range []string{filepath.Join(root, ".tools", helmMajor, "helm"), filepath.Join(root, ".tools", "kubectl")} {
		if fi, err := os.Stat(tool); err != nil || !fi.Mode().IsRegular() {
			c.fail("%s is missing; run: go run ./hack/fetchhelm", tool)
			return
		}
	}
	if left, err := ours(ctx); err != nil {
		c.fail("listing the engine's containers: %v", err)
		return
	} else if len(left) > 0 {
		c.fail("containers of an earlier run are still there: %s", strings.Join(left, ", "))
		return
	}
	volsBefore, err := volumes(ctx)
	if err != nil {
		c.fail("counting the volumes: %v", err)
		return
	}
	info("nerdctl volume ls -q before: %d volumes", volsBefore)
	// The command of getSubnets in kind's nerdctl provider (provision.go).
	subnets, err := engine(ctx, "network", "inspect", "-f", `{{range (index (index . "IPAM") "Config")}}{{index . "Subnet"}} {{end}}`, network)
	if err != nil {
		c.fail("the subnets of the network %s: %v", network, err)
		return
	}
	info("the subnets of the network %s, as kind's nerdctl provider reads them: %s", network, strings.TrimSpace(subnets))

	// Everything this proof starts goes at the end, also when a step fails.
	defer cleanup(c, done, volsBefore)

	// The proxy.
	for _, d := range []string{"proxy-src", "proxy", "images", "helm", "out"} {
		if err := os.MkdirAll(filepath.Join(work, d), 0o755); err != nil {
			c.fail("the folder %s: %v", d, err)
			return
		}
	}
	src := filepath.Join(work, "proxy-src")
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte(fwdProxyGoMod), 0o644); err == nil {
		err = os.WriteFile(filepath.Join(src, "main.go"), []byte(fwdProxyGo), 0o644)
	}
	if err := goBuild(ctx, src, "-ldflags", "-s -w", "-o", filepath.Join(work, "proxy", "fwdproxy"), "."); err != nil {
		c.fail("building the proxy: %v", err)
		return
	}
	if _, err := engine(ctx, "run", "-d", "--name", proxyName, "--network", network, "--tmpfs", "/var/lib/registry",
		"-v", filepath.Join(work, "proxy")+":/opt/p19:ro", "--entrypoint", "/opt/p19/fwdproxy", carrierImage, "-listen", ":"+proxyPort); err != nil {
		c.fail("starting the proxy: %v", err)
		return
	}
	proxyAddr, err := address(ctx, proxyName)
	if err != nil {
		c.fail("the proxy's address: %v", err)
		return
	}
	proxyURL := "http://" + proxyAddr + ":" + proxyPort
	for start := time.Now(); ; {
		log, _ := proxyLog(ctx)
		if strings.Contains(log, "fwdproxy listening") {
			break
		}
		if time.Since(start) > 30*time.Second {
			c.fail("the proxy did not say it listens within 30 seconds; its log: %s", log)
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	info("the proxy %s listens at %s", proxyName, proxyURL)

	// The tool and the fixture images, as the e2e suite's TestMain builds them.
	bin := filepath.Join(work, "rollproof")
	if err := goBuild(ctx, root, "-ldflags", "-s -w -X main.version=dev", "-o", bin, "./cmd/rollproof"); err != nil {
		c.fail("building the tool: %v", err)
		return
	}
	for _, version := range []string{"previous", "candidate"} {
		dir := filepath.Join(work, "images", version)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			c.fail("%v", err)
			return
		}
		if err := goBuild(ctx, root, "-ldflags", "-s -w -X main.version="+version, "-o", filepath.Join(dir, "svc"), "./fixtures/service"); err != nil {
			c.fail("building the fixture service %s: %v", version, err)
			return
		}
		dockerfile, err := os.ReadFile(filepath.Join(root, "fixtures", "service", "Dockerfile"))
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, "Dockerfile"), dockerfile, 0o644)
		}
		if err != nil {
			c.fail("the fixture's Dockerfile: %v", err)
			return
		}
		if _, err := engine(ctx, "build", "-t", "rollproof-fixture:"+version, dir); err != nil {
			c.fail("building rollproof-fixture:%s: %v", version, err)
			return
		}
		info("built rollproof-fixture:%s", version)
	}

	// The run description of the first fixture, and the PATH with Helm 3 and kubectl.
	description, err := firstFixture(filepath.Join(root, "fixtures", "chart"))
	if err != nil {
		c.fail("the run description: %v", err)
		return
	}
	file := filepath.Join(work, "rollproof.yaml")
	if err := os.WriteFile(file, []byte(description), 0o644); err != nil {
		c.fail("the run description: %v", err)
		return
	}
	info("the run description %s:", file)
	for line := range strings.Lines(description) {
		info("  %s", strings.TrimRight(line, "\n"))
	}
	helmDir := filepath.Join(work, "helm")
	for name, target := range map[string]string{
		"helm":    filepath.Join(root, ".tools", helmMajor, "helm"),
		"kubectl": filepath.Join(root, ".tools", "kubectl"),
	} {
		if err := os.Symlink(target, filepath.Join(helmDir, name)); err != nil {
			c.fail("the %s link: %v", name, err)
			return
		}
	}
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(k) {
		case "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "PATH", "KIND_EXPERIMENTAL_PROVIDER":
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"PATH="+helmDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"KIND_EXPERIMENTAL_PROVIDER=nerdctl",
		"HTTP_PROXY="+proxyURL,
		"HTTPS_PROXY="+proxyURL,
	)
	info("the tool's environment: HTTP_PROXY=%s HTTPS_PROXY=%s KIND_EXPERIMENTAL_PROVIDER=nerdctl, no NO_PROXY, PATH starting with the folder of helm (%s) and kubectl", proxyURL, proxyURL, helmMajor)

	// The run.
	before, err := settledProxyLog(ctx)
	if err != nil {
		c.fail("the proxy's log before the run: %v", err)
		return
	}
	out := filepath.Join(work, "out")
	rctx, cancel := context.WithTimeout(ctx, runLimit)
	defer cancel()
	cmd := exec.CommandContext(rctx, bin, "run", "-f", file, "--out", out, "--name", runName, "--keep-cluster")
	cmd.Dir = root
	cmd.Env = env
	var console bytes.Buffer
	cmd.Stdout = &console
	cmd.Stderr = &console
	start := time.Now()
	runErr := cmd.Run()
	info("the tool ran for %v; its exit: %v", time.Since(start).Round(time.Second), exitOf(runErr))
	for line := range strings.Lines(console.String()) {
		info("tool: %s", strings.TrimRight(line, "\n"))
	}
	if rctx.Err() != nil {
		c.fail("the tool did not end within %v", runLimit)
		return
	}
	verdict, final, windows, err := readResult(filepath.Join(out, "result.json"))
	if err != nil {
		c.fail("result.json: %v", err)
		return
	}
	info("result.json: final %t, verdict %q, windows %v", final, verdict, windows)
	c.check(runErr == nil && final && verdict == "pass", "verdict: the tool exited 0 and result.json is final with the verdict pass (exit %v, final %t, verdict %q)", exitOf(runErr), final, verdict)
	done["verdict"] = true

	// The proxy's log during the run, and the run's registry.
	after, err := settledProxyLog(ctx)
	if err != nil || len(after) < len(before) {
		c.fail("the proxy's log after the run: %v", err)
		return
	}
	runLines := requestLines(after[len(before):])
	info("the proxy's requests during the run: %d", len(runLines))
	for _, line := range runLines {
		info("proxy during the run: %s", line)
	}
	regAddr, err := address(ctx, registryName)
	if err != nil {
		c.fail("the address of the kept registry %s: %v", registryName, err)
		return
	}
	info("the run's registry %s: %s:5000 on the network %s", registryName, regAddr, network)
	named := slices.ContainsFunc(runLines, func(l string) bool {
		return strings.Contains(l, regAddr) || strings.Contains(l, "127.0.0.1") || strings.Contains(l, "rollproof.test") || strings.Contains(l, "localhost")
	})
	c.check(!named, "registry-not-proxied: no request in the proxy's log during the run names %s, 127.0.0.1, localhost or rollproof.test (%d requests in all)", regAddr, len(runLines))
	done["registry-not-proxied"] = true

	// The nodes.
	nodes, err := ours(ctx)
	if err != nil {
		c.fail("listing the nodes: %v", err)
		return
	}
	nodes = slices.DeleteFunc(nodes, func(n string) bool { return n == proxyName || n == registryName })
	slices.Sort(nodes)
	info("the kept nodes: %s", strings.Join(nodes, ", "))
	if len(nodes) != 4 {
		c.fail("node-env: the run kept %d nodes, want 4", len(nodes))
		return
	}
	if env, err := containerEnv(ctx, nodes[0]); err != nil {
		c.fail("the environment of %s: %v", nodes[0], err)
		return
	} else {
		for _, kv := range env {
			if strings.Contains(strings.ToUpper(kv), "_PROXY=") {
				info("%s, from the engine: %s", nodes[0], kv)
			}
		}
	}
	envOK := true
	var noProxy string
	for _, node := range nodes {
		got, err := engine(ctx, "exec", node, "sh", "-c", `tr '\0' '\n' < /proc/$(pidof containerd)/environ`)
		if err != nil {
			c.fail("node-env: containerd's environment in %s: %v", node, err)
			envOK = false
			continue
		}
		vars := map[string]string{}
		for line := range strings.Lines(got) {
			k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
			if ok && strings.HasSuffix(strings.ToUpper(k), "_PROXY") {
				vars[k] = v
				info("%s: containerd's environment: %s=%s", node, k, v)
			}
		}
		noProxy = vars["NO_PROXY"]
		parts := strings.Split(noProxy, ",")
		want := append(strings.Fields(subnets), serviceSubnet, podSubnet, ".svc", ".svc.cluster", ".svc.cluster.local")
		want = append(want, nodes...)
		var missing []string
		for _, w := range want {
			if !slices.Contains(parts, w) {
				missing = append(missing, w)
			}
		}
		held := vars["HTTP_PROXY"] == proxyURL && vars["HTTPS_PROXY"] == proxyURL && len(missing) == 0
		envOK = envOK && held
		info("%s: HTTP_PROXY and HTTPS_PROXY are %s: %t; NO_PROXY lacks %v", node, proxyURL, vars["HTTP_PROXY"] == proxyURL && vars["HTTPS_PROXY"] == proxyURL, missing)
	}
	if hosts, err := engine(ctx, "exec", nodes[0], "cat", "/etc/containerd/certs.d/rollproof.test/hosts.toml"); err == nil {
		info("%s: /etc/containerd/certs.d/rollproof.test/hosts.toml:", nodes[0])
		for line := range strings.Lines(hosts) {
			info("  %s", strings.TrimRight(line, "\n"))
		}
	} else {
		info("%s: /etc/containerd/certs.d/rollproof.test/hosts.toml: %s", nodes[0], oneLine(err.Error()))
	}
	// The Go release that built the node's containerd, whose net/http reads the proxy variables.
	ctrd := filepath.Join(work, "containerd")
	if _, err := engine(ctx, "cp", nodes[0]+":/usr/local/bin/containerd", ctrd); err != nil {
		info("copying containerd out of %s: %s", nodes[0], oneLine(err.Error()))
	} else if out, err := exec.CommandContext(ctx, "go", "version", ctrd).CombinedOutput(); err != nil {
		info("go version of the node's containerd: %s", oneLine(err.Error()+" "+string(out)))
	} else {
		info("go version of %s:/usr/local/bin/containerd: %s", nodes[0], strings.TrimSpace(strings.TrimPrefix(string(out), ctrd+": ")))
	}
	c.check(envOK, "node-env: in each of the 4 nodes, containerd's HTTP_PROXY and HTTPS_PROXY are %s and its NO_PROXY holds the network's subnets, the service and pod subnets, the node names and the .svc names", proxyURL)
	done["node-env"] = true

	// A pull of a public image in a worker, through the proxy.
	worker := nodes[len(nodes)-1]
	before, err = settledProxyLog(ctx)
	if err != nil {
		c.fail("the proxy's log before the public pull: %v", err)
		return
	}
	pullOut, pullErr := engine(ctx, "exec", worker, "crictl", "pull", publicImage)
	info("crictl pull %s in %s: %v; output: %s", publicImage, worker, exitOf(pullErr), oneLine(pullOut))
	after, err = settledProxyLog(ctx)
	if err != nil || len(after) < len(before) {
		c.fail("the proxy's log after the public pull: %v", err)
		return
	}
	pullLines := requestLines(after[len(before):])
	for _, line := range pullLines {
		info("proxy during the public pull: %s", line)
	}
	connect := slices.ContainsFunc(pullLines, func(l string) bool { return strings.HasPrefix(l, "REQ CONNECT registry-1.docker.io:443 ") })
	c.check(pullErr == nil && connect, "public-pull: crictl pull %s in %s succeeded and the proxy logged CONNECT registry-1.docker.io:443 during it (pull error %v; %d proxy lines)", publicImage, worker, pullErr, len(pullLines))
	done["public-pull"] = true
}

// cleanup removes every container whose name begins with prefix, with its
// anonymous volumes, and the tool's run folder; then checks that none is
// left and that the volume count is what it was.
func cleanup(c *checks, done map[string]bool, volsBefore int) {
	ctx := context.Background()
	names, err := ours(ctx)
	if err != nil {
		c.fail("cleanup: listing the containers: %v", err)
		return
	}
	for _, n := range names {
		if _, err := engine(ctx, "rm", "-f", "-v", n); err != nil {
			c.fail("cleanup: %v", err)
		}
	}
	info("cleanup: removed %s", strings.Join(names, ", "))
	runDir := filepath.Join(os.TempDir(), clusterName)
	if err := os.RemoveAll(runDir); err != nil {
		c.fail("cleanup: the run folder %s: %v", runDir, err)
	}
	left, err := ours(ctx)
	vols, verr := volumes(ctx)
	info("nerdctl volume ls -q after: %d volumes", vols)
	c.check(err == nil && len(left) == 0 && verr == nil && vols == volsBefore,
		"cleanup: no container whose name begins %s is left (%v), and the volume count is %d as before (now %d)", prefix, left, volsBefore, vols)
	done["cleanup"] = true
}

// firstFixture renders the e2e suite's run description of the fixture "first"
// with Helm, as renderConfig in e2e/config_test.go does.
func firstFixture(chart string) (string, error) {
	var blocks struct {
		Cluster, Images, Setup, Commands, Previous, Candidate, Probe, Limits string
	}
	text := func(dst *string, v any) error {
		b, err := json.Marshal(v)
		*dst = string(b)
		return err
	}
	release := func(entry string) map[string]any {
		return map[string]any{"chart": chart, "values": []any{map[string]any{"image": map[string]string{
			"repository": "${images." + entry + ".repository}",
			"digest":     "${images." + entry + ".digest}",
		}}}}
	}
	images := map[string]any{}
	for _, e := range []string{"previous", "candidate"} {
		images[e] = map[string]string{"local": "rollproof-fixture:" + e}
	}
	if err := errors.Join(
		text(&blocks.Cluster, map[string]any{"workers": 3}),
		text(&blocks.Images, images),
		text(&blocks.Probe, map[string]any{"http": map[string]string{"url": "http://orders.orders.svc/work?ms=20"}}),
		text(&blocks.Previous, release("previous")),
		text(&blocks.Candidate, release("candidate")),
	); err != nil {
		return "", err
	}
	var out strings.Builder
	if err := template.Must(template.New("rollproof.yaml").Parse(configTemplate)).Execute(&out, blocks); err != nil {
		return "", err
	}
	return out.String(), nil
}

// readResult reads the verdict, whether the file is final, and each window's
// result from result.json.
func readResult(path string) (string, bool, map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false, nil, err
	}
	var got struct {
		Final   bool   `json:"final"`
		Verdict string `json:"verdict"`
		Windows []struct {
			Name   string `json:"name"`
			Result string `json:"result"`
		} `json:"windows"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		return "", false, nil, err
	}
	windows := map[string]string{}
	for _, w := range got.Windows {
		windows[w.Name] = w.Result
	}
	return got.Verdict, got.Final, windows, nil
}

// exitOf describes how a command ended.
func exitOf(err error) string {
	var ee *exec.ExitError
	switch {
	case err == nil:
		return "exit 0"
	case errors.As(err, &ee):
		return fmt.Sprintf("exit %d", ee.ExitCode())
	default:
		return oneLine(err.Error())
	}
}

// oneLine joins the lines of s with " | ".
func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(strings.TrimSpace(s), "\n", " | ")), " ")
}

// summary counts each required check that did not run, and an interrupted
// run, as failed; prints the last line; and returns the exit code.
func summary(ctx context.Context, c *checks, done map[string]bool) int {
	for _, n := range required {
		if !done[n] {
			c.fail("the check %s did not run", n)
		}
	}
	if ctx.Err() != nil {
		c.fail("the run was interrupted")
	}
	if c.failed == 0 {
		fmt.Println("p19: all checks passed")
		return 0
	}
	fmt.Printf("p19: %d checks failed\n", c.failed)
	return 1
}
