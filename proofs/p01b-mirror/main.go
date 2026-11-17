//go:build linux

// Proof of point 1, second half: the one settings file for a registry host
// that has the run's registry, a mirror under a path and a private
// certificate authority together; what each of the two kinds of mirror path
// needs (a registry API under a path prefix, and a cache that wants its
// project name inside the API path); and the order in which a node tries the
// places that the file lists.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"

	"rollproof-proofs/internal/lab"
)

const (
	certsDPatch = "[plugins.\"io.containerd.grpc.v1.cri\".registry]\n  config_path = \"/etc/containerd/certs.d\""

	appGoMod  = "module p01bapp\n\ngo 1.27\n"
	appMainGo = `package main

import (
	"fmt"
	"time"
)

var v = "unset"

func main() {
	fmt.Println(v)
	time.Sleep(time.Hour)
}
`

	registryImage  = "docker.io/library/registry:3"
	prefixedVolume = "rollproof-proof-p01b-prefixed-data"
	prefixedName   = "rollproof-proof-p01b-prefixed" // the engine's name for the container prefixed
	hostsDir       = "/etc/containerd/certs.d/registry.example"

	// The parts of the settings files of actions 10 and 11.
	serverLine = "server = \"https://registry.example\"\n"
	runBlock   = "[host.\"http://%s\"]\n  capabilities = [\"pull\"]\n"
	// mirrorBlock is form A's mirror under a path.
	mirrorBlock = `[host."https://mirror.corp.example/registry-example"]
  capabilities = ["pull", "resolve"]
  ca = "/etc/containerd/certs.d/registry.example/ca.pem"
`
	// mirrorBlockNoCA is mirrorBlock without its ca line, for case c.
	mirrorBlockNoCA = `[host."https://mirror.corp.example/registry-example"]
  capabilities = ["pull", "resolve"]
`
	// cacheBlock is form B's cache with its project inside the API path.
	cacheBlock = `[host."https://cache.corp.example/v2/proxy-project"]
  capabilities = ["pull", "resolve"]
  ca = "/etc/containerd/certs.d/registry.example/ca.pem"
  override_path = true
`
	// cacheBlockAsA is form A's way of writing pointed at the cache, for case g.
	cacheBlockAsA = `[host."https://cache.corp.example/proxy-project"]
  capabilities = ["pull", "resolve"]
  ca = "/etc/containerd/certs.d/registry.example/ca.pem"
`
)

var zeroDigest = "sha256:" + strings.Repeat("0", 64)

// caseNames are the cases of actions 10 and 11. Each must run to its end, or
// the summary counts it as failed.
var caseNames = []string{"a", "b", "c", "d", "e", "f", "g"}

// accessLine matches the request and the status of a registry's access-log
// line, over HTTP/1.1 or HTTP/2.
var accessLine = regexp.MustCompile(`"([A-Z]+) (\S+) HTTP/[0-9.]+" (\d{3})`)

func main() { os.Exit(run()) }

// checks counts the failed checks, prints one line for each check, and notes
// which cases ran to their end.
type checks struct {
	failed int
	done   map[string]bool
}

func (c *checks) ok(format string, args ...any) {
	fmt.Println("ok: " + shown(fmt.Sprintf(format, args...)))
}

func (c *checks) fail(format string, args ...any) {
	c.failed++
	fmt.Println("FAILED: " + shown(fmt.Sprintf(format, args...)))
}

func info(format string, args ...any) { fmt.Println("info: " + shown(fmt.Sprintf(format, args...))) }

// cond is one condition of a case's check, stated as what must hold.
type cond struct {
	held bool
	text string
}

// verdict prints the one line of the case name: ok when every condition
// held, else FAILED with the conditions that did not. Either way the case
// has run to its end.
func (c *checks) verdict(name string, conds ...cond) {
	c.done[name] = true
	var held, not []string
	for _, x := range conds {
		if x.held {
			held = append(held, x.text)
		} else {
			not = append(not, x.text)
		}
	}
	if len(not) == 0 {
		c.ok("%s: %s", name, strings.Join(held, "; "))
		return
	}
	line := name + ": did not hold: " + strings.Join(not, "; ")
	if len(held) > 0 {
		line += "; held: " + strings.Join(held, "; ")
	}
	c.fail("%s", line)
}

// shown writes the home folder at the start of a path as $HOME.
func shown(s string) string {
	if home := os.Getenv("HOME"); home != "" {
		return strings.ReplaceAll(s, home+string(filepath.Separator), "$HOME"+string(filepath.Separator))
	}
	return s
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c := &checks{done: map[string]bool{}}

	bins, err := os.MkdirTemp("", "rollproof-proof-p01b-app-")
	if err != nil {
		c.fail("a temporary folder for the three binaries: %v", err)
		return summary(ctx, c)
	}
	defer os.RemoveAll(bins)
	// The registries mount this folder, so it is removed after Down.
	certs, err := os.MkdirTemp("", "rollproof-proof-p01b-certs-")
	if err != nil {
		c.fail("a temporary folder for the certificates: %v", err)
		return summary(ctx, c)
	}
	defer os.RemoveAll(certs)

	prove(ctx, c, bins, certs)
	return summary(ctx, c)
}

// proof is what the cases share.
type proof struct {
	ctx      context.Context
	c        *checks
	l        *lab.Lab
	worker   string
	run      lab.Registry
	project  lab.Container
	prefixed lab.Container
	a, b, f  string // the three digests
}

// prove does actions 1 to 12. The cluster and the volume are removed before
// it returns.
func prove(ctx context.Context, c *checks, bins, certs string) {
	// Action 1: three binaries.
	if err := build(ctx, bins); err != nil {
		c.fail("building the three binaries: %v", err)
		return
	}

	// Action 2: the cluster.
	cfg := &v1alpha4.Cluster{
		Nodes:                   []v1alpha4.Node{{Role: v1alpha4.ControlPlaneRole}, {Role: v1alpha4.WorkerRole}},
		ContainerdConfigPatches: []string{certsDPatch},
	}
	l, err := lab.Up(ctx, "p01b", cfg)
	if err != nil {
		c.fail("lab.Up: %v", err)
		return
	}
	// Action 12, also when a step fails: Down, then the volume, which a
	// container holds until Down removes it.
	defer func() {
		if err := l.Down(); err != nil {
			c.fail("l.Down(): %v", err)
		} else {
			c.ok("l.Down() returned no error")
		}
		if existed, err := removeVolume(l.Engine()); err != nil {
			c.fail("removing the volume %s after Down: %v", prefixedVolume, err)
		} else {
			c.ok("after Down, the volume %s is gone (it existed: %t)", prefixedVolume, existed)
		}
	}()
	nodes := l.Nodes()
	if len(nodes) != 2 {
		c.fail("l.Nodes() is %q, want two names", nodes)
		return
	}
	p := &proof{ctx: ctx, c: c, l: l, worker: nodes[1]}
	info("worker %s", p.worker)
	for _, tool := range []string{"containerd", "crictl"} {
		if out, err := l.Exec(p.worker, nil, tool, "--version"); err != nil {
			info("%s --version in the worker: %s", tool, oneLine(err.Error()))
		} else {
			info("%s --version in the worker: %s", tool, strings.TrimSpace(out))
		}
	}

	// Action 3: the certificates.
	caPEM, pool, err := makeCerts(certs)
	if err != nil {
		c.fail("making the certificates: %v", err)
		return
	}
	info("in %s: ca.pem, server.pem and server-key.pem, for mirror.corp.example, cache.corp.example and 127.0.0.1, valid for one day", certs)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{RootCAs: pool}
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second}

	// Action 4: the run's registry.
	if p.run, err = l.Registry(ctx, "run"); err != nil {
		c.fail("l.Registry(ctx, \"run\"): %v", err)
		return
	}
	info("run: Push %s, Node %s", p.run.Push, p.run.Node)
	if log, err := p.run.Logs(); err == nil {
		for line := range strings.Lines(log) {
			if strings.Contains(line, "version=") {
				info("run's log: %s", strings.TrimSpace(line))
				break
			}
		}
	}

	// Action 5: the two TLS registries.
	projectPort, err := freePort()
	if err != nil {
		c.fail("a port for project: %v", err)
		return
	}
	prefixedPort, err := freePort()
	if err != nil {
		c.fail("a port for prefixed: %v", err)
		return
	}
	// registry:3 declares VOLUME /var/lib/registry; the tmpfs keeps the
	// engine from creating an anonymous volume there (ruling T5-2).
	projectArgs := append(tlsArgs(projectPort, certs), "--tmpfs", "/var/lib/registry", registryImage)
	if p.project, err = l.Container(ctx, "project", projectArgs...); err != nil {
		c.fail("l.Container(ctx, \"project\", …): %v", err)
		return
	}
	if err := waitTLS(ctx, client, "https://127.0.0.1:"+projectPort+"/v2/"); err != nil {
		c.fail("project: %v", err)
		return
	}
	info("project: %s, port %s, address %s", p.project.Name, projectPort, p.project.Address)

	if existed, err := removeVolume(l.Engine()); err != nil {
		c.fail("removing the volume %s before prefixed starts: %v", prefixedVolume, err)
		return
	} else if existed {
		info("removed a leftover volume %s", prefixedVolume)
	}
	prefixedArgs := append(tlsArgs(prefixedPort, certs), "-v", prefixedVolume+":/var/lib/registry")
	if p.prefixed, err = l.Container(ctx, "prefixed", append(slices.Clone(prefixedArgs), registryImage)...); err != nil {
		c.fail("l.Container(ctx, \"prefixed\", …): %v", err)
		return
	}
	if err := waitTLS(ctx, client, "https://127.0.0.1:"+prefixedPort+"/v2/"); err != nil {
		c.fail("prefixed, without its prefix: %v", err)
		return
	}
	info("prefixed: %s, port %s, address %s, on the volume %s, without a prefix", p.prefixed.Name, prefixedPort, p.prefixed.Address, prefixedVolume)

	// Action 6: the three images.
	for _, push := range []struct {
		v, repository string
		digest        *string
	}{
		{"a", "team/app", &p.a},
		{"b", "stage/b", &p.b},
		{"f", "stage/f", &p.f},
	} {
		d, err := lab.PushBinary(ctx, p.run.Push, push.repository, filepath.Join(bins, push.v))
		if err != nil {
			c.fail("pushing %s: %v", push.v, err)
			return
		}
		*push.digest = d
		info("%s = %s, in run as %s", strings.ToUpper(push.v), d, push.repository)
	}
	for _, cp := range []struct {
		v, from, digest, to, toName string
	}{
		{"b", "stage/b", p.b, "127.0.0.1:" + prefixedPort + "/team/app:v1", "prefixed as team/app:v1"},
		{"f", "stage/f", p.f, "127.0.0.1:" + projectPort + "/proxy-project/team/app:v1", "project as proxy-project/team/app:v1"},
	} {
		if err := copyImage(ctx, p.run.Push+"/"+cp.from+"@"+cp.digest, cp.to, tr); err != nil {
			c.fail("copying %s to %s: %v", strings.ToUpper(cp.v), cp.toName, err)
			return
		}
		info("%s copied to %s", strings.ToUpper(cp.v), cp.toName)
	}

	// Action 7: prefixed again, with its prefix, on the same volume.
	if out, err := exec.CommandContext(ctx, l.Engine(), "rm", "-f", prefixedName).CombinedOutput(); err != nil {
		c.fail("%s rm -f %s: %v\n%s", l.Engine(), prefixedName, err, out)
		return
	}
	prefixedArgs = append(prefixedArgs, "-e", "REGISTRY_HTTP_PREFIX=/registry-example/", registryImage)
	if p.prefixed, err = l.Container(ctx, "prefixed", prefixedArgs...); err != nil {
		c.fail("l.Container(ctx, \"prefixed\", …) with its prefix: %v", err)
		return
	}
	if err := waitTLS(ctx, client, "https://127.0.0.1:"+prefixedPort+"/registry-example/v2/"); err != nil {
		c.fail("prefixed, with its prefix: %v", err)
		return
	}
	info("prefixed was filled without its prefix and started again with it, on the same volume")
	info("prefixed: address %s", p.prefixed.Address)

	// Action 8: the names, and the authority, in every node.
	for _, node := range l.Nodes() {
		for _, args := range [][]string{
			{"sh", "-c", "echo " + p.prefixed.Address + " mirror.corp.example >> /etc/hosts"},
			{"sh", "-c", "echo " + p.project.Address + " cache.corp.example >> /etc/hosts"},
			{"mkdir", "-p", hostsDir},
		} {
			if _, err := l.Exec(node, nil, args...); err != nil {
				c.fail("in node %s: %v", node, err)
				return
			}
		}
		if _, err := l.Exec(node, strings.NewReader(string(caPEM)), "cp", "/dev/stdin", hostsDir+"/ca.pem"); err != nil {
			c.fail("writing %s/ca.pem in node %s: %v", hostsDir, node, err)
			return
		}
	}
	info("in every node: %s mirror.corp.example and %s cache.corp.example in /etc/hosts; the CA at %s/ca.pem", p.prefixed.Address, p.project.Address, hostsDir)

	// Actions 9 to 11: the cases.
	for _, cs := range []func(){p.caseA, p.caseB, p.caseC, p.caseD, p.caseE, p.caseF, p.caseG} {
		if ctx.Err() != nil {
			return
		}
		cs()
	}
}

// build writes the module p01bapp into dir and builds it there three times,
// one binary for each value of v.
func build(ctx context.Context, dir string) error {
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(appGoMod), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(appMainGo), 0o644); err != nil {
		return err
	}
	for _, v := range []string{"a", "b", "f"} {
		cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-ldflags", "-s -w -X main.v="+v, "-o", filepath.Join(dir, v), ".")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("go build for %s: %w\n%s", v, err, out)
		}
	}
	return nil
}

// makeCerts writes into dir a CA (ca.pem) and a server certificate signed by
// it (server.pem, key server-key.pem) for mirror.corp.example,
// cache.corp.example and 127.0.0.1, both valid for one day. The CA's key is
// never written. It returns the CA's PEM and a pool that holds the CA.
func makeCerts(dir string) ([]byte, *x509.CertPool, error) {
	notBefore := time.Now().Add(-time.Minute)
	notAfter := notBefore.Add(24 * time.Hour)

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	caSerial, err := serial()
	if err != nil {
		return nil, nil, err
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          caSerial,
		Subject:               pkix.Name{CommonName: "rollproof proof p01b CA"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, nil, err
	}

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serverSerial, err := serial()
	if err != nil {
		return nil, nil, err
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: serverSerial,
		Subject:      pkix.Name{CommonName: "mirror.corp.example"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"mirror.corp.example", "cache.corp.example"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}
	serverKeyDER, err := x509.MarshalPKCS8PrivateKey(serverKey)
	if err != nil {
		return nil, nil, err
	}

	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	for _, f := range []struct {
		name string
		data []byte
	}{
		{"ca.pem", caPEM},
		{"server.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER})},
		{"server-key.pem", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: serverKeyDER})},
	} {
		if err := os.WriteFile(filepath.Join(dir, f.name), f.data, 0o600); err != nil {
			return nil, nil, err
		}
	}
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	return caPEM, pool, nil
}

// serial returns a random serial number of 128 bits.
func serial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
}

// freePort returns a port of 127.0.0.1 that was free a moment ago.
func freePort() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	ln.Close()
	return port, nil
}

// tlsArgs are the engine's run arguments that both TLS registries share.
func tlsArgs(port, certs string) []string {
	return []string{
		"-p", "127.0.0.1:" + port + ":443",
		"-v", certs + ":/certs:ro",
		"-e", "REGISTRY_HTTP_ADDR=0.0.0.0:443",
		"-e", "REGISTRY_HTTP_TLS_CERTIFICATE=/certs/server.pem",
		"-e", "REGISTRY_HTTP_TLS_KEY=/certs/server-key.pem",
	}
}

// waitTLS returns when GET url answers 200 through client, or an error after
// 30 seconds.
func waitTLS(ctx context.Context, client *http.Client, url string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
		case <-ctx.Done():
			return fmt.Errorf("GET %s did not answer 200 within 30 seconds: %v", url, err)
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// copyImage reads the image src, a digest reference in the run's registry,
// and writes it under the tag dst through tr, which trusts the CA. The proof
// uses a tag here; the tool never does.
func copyImage(ctx context.Context, src, dst string, tr http.RoundTripper) error {
	from, err := name.NewDigest(src, name.Insecure)
	if err != nil {
		return err
	}
	img, err := remote.Image(from, remote.WithContext(ctx))
	if err != nil {
		return err
	}
	to, err := name.NewTag(dst)
	if err != nil {
		return err
	}
	return remote.Write(to, img, remote.WithContext(ctx), remote.WithTransport(tr))
}

// removeVolume runs ENGINE volume rm -f on prefixed's volume. nerdctl's -f is
// not implemented and a missing volume makes it fail, so the result is judged
// by whether the volume is still listed afterwards. existed says whether the
// command removed it.
func removeVolume(engine string) (existed bool, err error) {
	out, rmErr := exec.Command(engine, "volume", "rm", "-f", prefixedVolume).CombinedOutput()
	if rmErr == nil {
		return true, nil
	}
	ls, err := exec.Command(engine, "volume", "ls", "-q").Output()
	if err != nil {
		return false, fmt.Errorf("%s volume ls -q: %w (after %s volume rm -f %s: %v\n%s)", engine, err, engine, prefixedVolume, rmErr, out)
	}
	if slices.Contains(strings.Fields(string(ls)), prefixedVolume) {
		return true, fmt.Errorf("%s volume rm -f %s: %w\n%s", engine, prefixedVolume, rmErr, out)
	}
	return false, nil
}

// writeHosts writes text to hostsDir/hosts.toml in every node.
func (p *proof) writeHosts(text string) error {
	for _, node := range p.l.Nodes() {
		if _, err := p.l.Exec(node, strings.NewReader(text), "cp", "/dev/stdin", hostsDir+"/hosts.toml"); err != nil {
			return err
		}
	}
	return nil
}

// showHosts prints the settings file that case label wrote.
func showHosts(label, text string) {
	info("%s: %s/hosts.toml, written in every node:", label, hostsDir)
	for line := range strings.Lines(text) {
		info("%s:   %s", label, strings.TrimRight(line, "\n"))
	}
}

// pull runs crictl pull REFERENCE in the worker.
func (p *proof) pull(ref string) (string, error) {
	return p.l.Exec(p.worker, nil, "crictl", "pull", ref)
}

// rmi removes the image ref from the worker; an error is ignored.
func (p *proof) rmi(ref string) { _, _ = p.l.Exec(p.worker, nil, "crictl", "rmi", ref) }

// formA is form A with mirror as its mirror's block: the server line, the
// run's registry, then mirror.
func (p *proof) formA(mirror string) string {
	return serverLine + fmt.Sprintf(runBlock, p.run.Node) + mirror
}

// watched is one of the three registries whose logs a case reads.
type watched struct {
	name string
	c    lab.Container
}

func (p *proof) registries() []watched {
	return []watched{{"run", p.run.Container}, {"project", p.project}, {"prefixed", p.prefixed}}
}

// outcome is what one case's pull did, and what the three registries and
// containerd logged during it.
type outcome struct {
	out     string               // crictl's output, standard error included
	err     error                // non-nil when crictl pull exited non-zero
	reqs    map[string][]request // each registry's new requests, by its name
	journal []string             // containerd's lines that contain "trying next host" or "fetch failed"
}

// pullCase does action 9 for the case label: it writes text as the settings
// file in every node, removes ref from the worker, notes where the three
// registries' logs end and the time, pulls ref in the worker, and prints the
// new requests of each registry and containerd's lines that name each host
// tried, in order.
func (p *proof) pullCase(label, text, ref string) (outcome, error) {
	var o outcome
	if err := p.writeHosts(text); err != nil {
		return o, fmt.Errorf("writing the settings file: %w", err)
	}
	showHosts(label, text)
	p.rmi(ref)
	regs := p.registries()
	before := make([]int, len(regs))
	for i, r := range regs {
		log, err := settledLogs(r.c)
		if err != nil {
			return o, err
		}
		before[i] = len(log)
	}
	since := time.Now().UTC().Format("2006-01-02 15:04:05") + " UTC"

	o.out, o.err = p.pull(ref)
	if o.err != nil {
		info("%s: crictl pull %s failed; its output:", label, ref)
	} else {
		info("%s: crictl pull %s succeeded; its output:", label, ref)
	}
	for line := range strings.Lines(strings.TrimSpace(o.out)) {
		info("%s:   %s", label, strings.TrimRight(line, "\n"))
	}

	o.reqs = map[string][]request{}
	for i, r := range regs {
		log, err := settledLogs(r.c)
		if err != nil {
			return o, err
		}
		if len(log) < before[i] {
			return o, fmt.Errorf("the log of %s is shorter than before case %s", r.name, label)
		}
		added := log[before[i]:]
		reqs := requests(added)
		o.reqs[r.name] = reqs
		if len(reqs) == 0 {
			info("%s %s: no new request", label, r.name)
		}
		for _, req := range reqs {
			info("%s %s: %s %s %d", label, r.name, req.Method, req.Path, req.Status)
		}
		for line := range strings.Lines(added) {
			if strings.Contains(line, "handshake") {
				info("%s %s: log: %s", label, r.name, strings.TrimSpace(line))
			}
		}
	}

	journal, err := p.l.Exec(p.worker, nil, "journalctl", "-u", "containerd", "--no-pager", "--since", since)
	if err != nil {
		return o, fmt.Errorf("journalctl -u containerd in the worker: %w", err)
	}
	for line := range strings.Lines(journal) {
		if strings.Contains(line, "trying next host") || strings.Contains(line, "fetch failed") {
			line = strings.TrimSpace(line)
			o.journal = append(o.journal, line)
			info("%s containerd: %s", label, line)
		}
	}
	if len(o.journal) == 0 {
		info("%s containerd: no line since %s contains \"trying next host\" or \"fetch failed\"", label, since)
	}
	return o, nil
}

// observed is the failed condition for a case that could not be observed.
func observed(err error) cond {
	return cond{false, "the case was observed (" + oneLine(err.Error()) + ")"}
}

// pulled is the condition that crictl pull of ref succeeded.
func pulled(ref string, o outcome) cond {
	text := "crictl pull " + ref + " succeeded"
	if o.err != nil {
		text += " (it did not: " + oneLine(o.out) + ")"
	}
	return cond{o.err == nil, text}
}

// pullFailed is the condition that crictl pull of ref failed.
func pullFailed(ref string, o outcome) cond {
	text := "crictl pull " + ref + " failed"
	if o.err == nil {
		text += " (it succeeded)"
	}
	return cond{o.err != nil, text}
}

// hostsTried returns the host field of each of containerd's lines, in order.
func hostsTried(lines []string) []string {
	var hosts []string
	for _, line := range lines {
		h, _ := field(line, "host")
		hosts = append(hosts, h)
	}
	return hosts
}

// caseA pulls by digest an image that the run's registry holds, with form A.
func (p *proof) caseA() {
	ref := "registry.example/team/app@" + p.a
	o, err := p.pullCase("a", p.formA(mirrorBlock), ref)
	if err != nil {
		p.c.verdict("a", observed(err))
		return
	}
	p.c.verdict("a",
		pulled(ref, o),
		cond{hasPath(o.reqs["run"], "/v2/team/app/manifests/"+p.a, 200), "run answered /v2/team/app/manifests/A with 200"},
	)
}

// caseB pulls by a tag that only the mirror holds, with form A.
func (p *proof) caseB() {
	ref := "registry.example/team/app:v1"
	o, err := p.pullCase("b", p.formA(mirrorBlock), ref)
	if err != nil {
		p.c.verdict("b", observed(err))
		return
	}
	p.c.verdict("b",
		pulled(ref, o),
		cond{!hasSuffix(o.reqs["run"], "/manifests/v1", 0), "run had no request for /manifests/v1"},
		cond{hasPrefix(o.reqs["prefixed"], "/registry-example/v2/team/app/", 200), "prefixed answered a path that starts with /registry-example/v2/team/app/ with 200"},
	)
}

// caseC is case b with form A without its ca line.
func (p *proof) caseC() {
	ref := "registry.example/team/app:v1"
	o, err := p.pullCase("c", p.formA(mirrorBlockNoCA), ref)
	if err != nil {
		p.c.verdict("c", observed(err))
		return
	}
	p.c.verdict("c",
		pullFailed(ref, o),
		cond{strings.Contains(o.out, "certificate"), "its error contains \"certificate\""},
	)
}

// caseD pulls a digest that no registry holds, with form A.
func (p *proof) caseD() {
	ref := "registry.example/team/app@" + zeroDigest
	o, err := p.pullCase("d", p.formA(mirrorBlock), ref)
	if err != nil {
		p.c.verdict("d", observed(err))
		return
	}
	hosts := hostsTried(o.journal)
	info("d: the hosts in containerd's lines, in order: %s", strings.Join(hosts, ", "))
	last := ""
	if len(hosts) > 0 {
		last = hosts[len(hosts)-1]
	}
	p.c.verdict("d",
		pullFailed(ref, o),
		cond{strings.Contains(o.out, "registry.example"), "its error names registry.example"},
		cond{last == "registry.example", fmt.Sprintf("the last host tried, in containerd's lines, is registry.example (it is %q)", last)},
	)
}

// caseE is case a with form A's two [host…] blocks swapped. What it shows is
// recorded, not judged.
func (p *proof) caseE() {
	ref := "registry.example/team/app@" + p.a
	text := serverLine + mirrorBlock + fmt.Sprintf(runBlock, p.run.Node)
	o, err := p.pullCase("e", text, ref)
	if err != nil {
		p.c.verdict("e", observed(err))
		return
	}
	manifest := "/v2/team/app/manifests/" + p.a
	var byLogs string
	switch prefixedAsked, runAsked := hasPath(o.reqs["prefixed"], "/registry-example"+manifest, 0), hasPath(o.reqs["run"], manifest, 0); {
	case prefixedAsked && hasPath(o.reqs["run"], manifest, 200):
		byLogs = "prefixed: it was asked for /registry-example" + manifest + ", and run answered " + manifest + " with 200, which ends the search for that path"
	case prefixedAsked:
		byLogs = "prefixed was asked for /registry-example" + manifest + "; run did not answer " + manifest + " with 200"
	case runAsked:
		byLogs = "run: it was asked for " + manifest + ", and prefixed was not asked for the manifest"
	default:
		byLogs = "neither registry was asked for the manifest"
	}
	byJournal := "no containerd line names a host tried"
	if hosts := hostsTried(o.journal); len(hosts) > 0 {
		byJournal = fmt.Sprintf("the first line names host=%s (%s), and the hosts in order are %s", hosts[0], p.registryOf(hosts[0]), strings.Join(hosts, ", "))
	}
	info("e: asked first, by the logs: %s", byLogs)
	info("e: asked first, by containerd's lines: %s", byJournal)
	result := "succeeded"
	if o.err != nil {
		result = "failed"
	}
	p.c.verdict("e", cond{true, "the pull ran and was observed; it " + result})
}

// registryOf names the registry that a host of containerd's lines is.
func (p *proof) registryOf(host string) string {
	switch host {
	case p.run.Node:
		return "run"
	case "mirror.corp.example":
		return "prefixed"
	case "cache.corp.example":
		return "project"
	case "registry.example":
		return "the server line"
	}
	return "unknown"
}

// caseF pulls by a tag that only the cache holds, with form B.
func (p *proof) caseF() {
	ref := "registry.example/team/app:v1"
	o, err := p.pullCase("f", p.formA(cacheBlock), ref)
	if err != nil {
		p.c.verdict("f", observed(err))
		return
	}
	p.c.verdict("f",
		pulled(ref, o),
		cond{hasPrefix(o.reqs["project"], "/v2/proxy-project/team/app/", 200), "project answered a path that starts with /v2/proxy-project/team/app/ with 200"},
	)
}

// caseG writes the cache in form A's way, with no override_path. What it
// shows is recorded, not judged.
func (p *proof) caseG() {
	ref := "registry.example/team/app:v1"
	o, err := p.pullCase("g", p.formA(cacheBlockAsA), ref)
	if err != nil {
		p.c.verdict("g", observed(err))
		return
	}
	if len(o.reqs["project"]) == 0 {
		info("g: project was asked for no path")
	}
	for _, r := range o.reqs["project"] {
		info("g: project was asked for %s (%s without the query), and answered %d", r.Path, bare(r.Path), r.Status)
	}
	result := "succeeded"
	if o.err != nil {
		result = "failed"
	}
	info("g: the pull %s", result)
	p.c.verdict("g", cond{true, "the pull ran and was observed; it " + result})
}

// oneLine joins the lines of s with " | ".
func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(strings.TrimSpace(s), "\n", " | ")), " ")
}

// request is one request that a registry logged.
type request struct {
	Method, Path string
	Status       int
}

// requests returns one request for each access-log line of log, of the form
// "METHOD PATH HTTP/VERSION" STATUS; when log has none, one for each line
// with the fields http.request.method, http.request.uri and
// http.response.status.
func requests(log string) []request {
	var reqs []request
	for _, m := range accessLine.FindAllStringSubmatch(log, -1) {
		status, _ := strconv.Atoi(m[3])
		reqs = append(reqs, request{m[1], m[2], status})
	}
	if len(reqs) > 0 {
		return reqs
	}
	for line := range strings.Lines(log) {
		method, okMethod := field(line, "http.request.method")
		uri, okURI := field(line, "http.request.uri")
		st, okStatus := field(line, "http.response.status")
		status, err := strconv.Atoi(st)
		if okMethod && okURI && okStatus && err == nil {
			reqs = append(reqs, request{method, uri, status})
		}
	}
	return reqs
}

// field returns the value of key in a log line written as logfmt
// (key=value or key="value") or as JSON ("key":"value" or "key":123).
func field(line, key string) (string, bool) {
	line = " " + line
	for _, prefix := range []string{" " + key + "=", `"` + key + `":`} {
		i := strings.Index(line, prefix)
		if i < 0 {
			continue
		}
		rest := line[i+len(prefix):]
		if strings.HasPrefix(rest, `"`) {
			q, err := strconv.QuotedPrefix(rest)
			if err != nil {
				return "", false
			}
			v, err := strconv.Unquote(q)
			return v, err == nil
		}
		end := strings.IndexAny(rest, " ,}\n")
		if end < 0 {
			end = len(rest)
		}
		return rest[:end], true
	}
	return "", false
}

// bare returns path without its query. containerd adds ?ns=HOST to each
// request it sends to a host other than the one in the image's name.
func bare(path string) string {
	p, _, _ := strings.Cut(path, "?")
	return p
}

// hasPath reports whether reqs hold a request for path, its query left out,
// answered with status; a status of 0 matches any.
func hasPath(reqs []request, path string, status int) bool {
	return slices.ContainsFunc(reqs, func(r request) bool {
		return bare(r.Path) == path && (status == 0 || r.Status == status)
	})
}

// hasPrefix reports whether reqs hold a request whose path, its query left
// out, starts with prefix, answered with status; a status of 0 matches any.
func hasPrefix(reqs []request, prefix string, status int) bool {
	return slices.ContainsFunc(reqs, func(r request) bool {
		return strings.HasPrefix(bare(r.Path), prefix) && (status == 0 || r.Status == status)
	})
}

// hasSuffix reports whether reqs hold a request whose path, its query left
// out, ends in suffix, answered with status; a status of 0 matches any.
func hasSuffix(reqs []request, suffix string, status int) bool {
	return slices.ContainsFunc(reqs, func(r request) bool {
		return strings.HasSuffix(bare(r.Path), suffix) && (status == 0 || r.Status == status)
	})
}

// settledLogs returns the container's log once two reads 500 ms apart agree,
// or the last read after 5 seconds: a line may reach the log a moment after
// the request it records was answered.
func settledLogs(c lab.Container) (string, error) {
	prev, err := c.Logs()
	if err != nil {
		return "", err
	}
	for range 10 {
		time.Sleep(500 * time.Millisecond)
		cur, err := c.Logs()
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

// summary counts each case that did not run to its end, and an interrupted
// run, as failed; prints the last line; and returns the exit code.
func summary(ctx context.Context, c *checks) int {
	for _, n := range caseNames {
		if !c.done[n] {
			c.fail("case %s did not run", n)
		}
	}
	if ctx.Err() != nil {
		c.fail("the run was interrupted")
	}
	if c.failed == 0 {
		fmt.Println("p01b: all checks passed")
		return 0
	}
	fmt.Printf("p01b: %d checks failed\n", c.failed)
	return 1
}
