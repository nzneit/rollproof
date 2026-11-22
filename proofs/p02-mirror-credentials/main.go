//go:build linux

// Proof of point 2: mirrors that need credentials. Which credentials a node's
// container runtime offers to a mirror and to the real host when a pod pulls,
// what a mirror whose credentials differ from the real host's needs, whether
// a node's settings can forbid the last step from the mirror to the real host,
// whether the `ca` at the top of a settings file is used for the real host,
// and whether a node pulls an image from the run's registry with no
// credentials when the image's host needs them.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
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

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"

	"rollproof-proofs/internal/lab"
)

const (
	certsDPatch = "[plugins.\"io.containerd.grpc.v1.cri\".registry]\n  config_path = \"/etc/containerd/certs.d\""

	appGoMod  = "module p02app\n\ngo 1.27\n"
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

	// proxyMainGo is the logging reverse proxy that stands in front of M and
	// R. It writes one line for each request: the listener, the method, the
	// path with its query, the user of a Basic Authorization header (- when
	// there is none), the header's scheme, and the status that went back.
	proxyGoMod  = "module p02proxy\n\ngo 1.27\n"
	proxyMainGo = `package main

import (
	"flag"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
)

// recorder keeps the status that the handler wrote.
type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

var mu sync.Mutex

func main() {
	name := flag.String("name", "", "the name that each log line carries")
	backend := flag.String("backend", "", "the registry behind the proxy")
	strip := flag.String("strip", "", "a path prefix that every request must have; it is removed before the request is passed on")
	httpAddr := flag.String("http", "", "the address of the plain HTTP listener")
	httpsAddr := flag.String("https", "", "the address of the TLS listener")
	cert := flag.String("cert", "", "the TLS certificate")
	key := flag.String("key", "", "the TLS key")
	flag.Parse()
	target, err := url.Parse(*backend)
	if err != nil || target.Host == "" {
		fmt.Fprintf(os.Stderr, "logproxy: bad -backend %q: %v\n", *backend, err)
		os.Exit(2)
	}
	rp := httputil.NewSingleHostReverseProxy(target)
	handler := func(listener string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &recorder{ResponseWriter: w}
			uri := r.URL.RequestURI()
			switch {
			case *strip == "":
				rp.ServeHTTP(rec, r)
			case strings.HasPrefix(r.URL.Path, *strip+"/"):
				out := r.Clone(r.Context())
				out.URL.Path = strings.TrimPrefix(r.URL.Path, *strip)
				out.URL.RawPath = ""
				rp.ServeHTTP(rec, out)
			default:
				http.NotFound(rec, r)
			}
			user, auth := "-", "-"
			if h := r.Header.Get("Authorization"); h != "" {
				scheme, _, _ := strings.Cut(h, " ")
				auth = strings.ToLower(scheme)
				if u, _, ok := r.BasicAuth(); ok && u != "" {
					user = u
				}
			}
			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}
			mu.Lock()
			fmt.Printf("REQ %s %s %s %s user=%s auth=%s status=%d\n", *name, listener, r.Method, uri, user, auth, status)
			mu.Unlock()
		})
	}
	errc := make(chan error, 2)
	if *httpAddr != "" {
		go func() { errc <- http.ListenAndServe(*httpAddr, handler("http")) }()
	}
	if *httpsAddr != "" {
		go func() { errc <- http.ListenAndServeTLS(*httpsAddr, *cert, *key, handler("https")) }()
	}
	fmt.Printf("logproxy %s: backend %s, strip %q, http %q, https %q\n", *name, *backend, *strip, *httpAddr, *httpsAddr)
	err = <-errc
	fmt.Fprintf(os.Stderr, "logproxy %s: %v\n", *name, err)
	os.Exit(1)
}
`

	registryImage  = "docker.io/library/registry:3"
	namespace      = "p02"
	certsRoot      = "/etc/containerd/certs.d"
	kubeletConfig  = "/var/lib/kubelet/config.json"
	containerdTOML = "/etc/containerd/config.toml"

	// mirrorHost is M's name, which the nodes resolve through /etc/hosts, and
	// mirrorPrefix the path prefix under which M's registry API sits.
	mirrorHost   = "mirror.corp.example"
	mirrorPrefix = "/registry-example"
	mirrorURL    = "https://" + mirrorHost + mirrorPrefix
	// realTLSName is R's name over TLS, for case e.
	realTLSName = "e.registry.example"
)

// cred is one made-up user of the stand-in registries. The passwords are throwaway values for
// registries that live only during the run. hash is the bcrypt hash, cost 5, that the registry's
// htpasswd file holds; it was made once with perl's crypt (libxcrypt) and is checked against the
// registries before the cases.
type cred struct{ user, pass, hash string }

// basic is the base64 of USER:PASSWORD, as a Basic Authorization header and Docker's config.json carry it.
func (c cred) basic() string {
	return base64.StdEncoding.EncodeToString([]byte(c.user + ":" + c.pass))
}

var (
	// aCred is a credential for H that both M and R accept.
	aCred = cred{"auser", "a-pass-p02", "$2b$05$Qm9sbHByb29mUDAyQXVzZOPAPLSe7fSbp5y1mrRgJOL227Nsuia4q"}
	// mCred is M's own credential, which R does not accept.
	mCred = cred{"muser", "m-pass-p02", "$2b$05$Qm9sbHByb29mUDAyTXVzZOGnPlkHP9.erL1vXSzbcrq.WfpDK9nLG"}
	// hCred is the real host's credential, which M does not accept.
	hCred = cred{"huser", "h-pass-p02", "$2b$05$Qm9sbHByb29mUDAySHVzZOXL0bk6fDhqTk7ghls4gccRUjA4.KPgS"}
)

// mUsers and rUsers are the htpasswd files of M's and R's registries.
var (
	mUsers = []cred{aCred, mCred}
	rUsers = []cred{aCred, hCred}
)

// kubeletAuths are the entries of the kubelet's config.json in every node, by the key that the
// file lists them under.
var kubeletAuths = []struct {
	key string
	c   cred
}{
	{"a.registry.example", aCred},
	{mirrorHost, aCred}, // case b: listed under M's own host only
	{"c1.registry.example", hCred},
	{"d.registry.example", aCred},
	{realTLSName, hCred},
}

// hosts are the registry hosts that the cases name images after. Each gets a folder under
// certsRoot in every node, with the CA as ca.pem.
var hosts = []string{"a.registry.example", "b.registry.example", "c1.registry.example", "c2.registry.example", "d.registry.example", "d2.registry.example", realTLSName, "f.registry.example"}

// image is one test image: the value of v in its binary, its repository, the registries it is
// copied to ("m", "r", "run"), and its digest once pushed.
type image struct {
	v, repository string
	to            []string
	digest        string
}

// images are the test images, one for each pull, so that no pull finds content that an earlier
// one brought into the node.
var images = []*image{
	{v: "x", repository: "team/x", to: []string{"m"}},
	{v: "a", repository: "team/a", to: []string{"m"}},
	{v: "b", repository: "team/b", to: []string{"m"}},
	{v: "hdr-c1-m", repository: "team/c-hdr-c1-m", to: []string{"m"}},
	{v: "hdr-c1-r", repository: "team/c-hdr-c1-r", to: []string{"r"}},
	{v: "hdr-c2-m", repository: "team/c-hdr-c2-m", to: []string{"m"}},
	{v: "hdr-c2-r", repository: "team/c-hdr-c2-r", to: []string{"r"}},
	{v: "cfg-c1-m", repository: "team/c-cfg-c1-m", to: []string{"m"}},
	{v: "cfg-c1-r", repository: "team/c-cfg-c1-r", to: []string{"r"}},
	{v: "cfg-c2-m", repository: "team/c-cfg-c2-m", to: []string{"m"}},
	{v: "cfg-c2-r", repository: "team/c-cfg-c2-r", to: []string{"r"}},
	{v: "d-m", repository: "team/d-m", to: []string{"m"}},
	{v: "d", repository: "team/d", to: []string{"r"}},
	{v: "d-hdr-m", repository: "team/d-hdr-m", to: []string{"m"}},
	{v: "d-hdr", repository: "team/d-hdr", to: []string{"r"}},
	{v: "e0", repository: "team/e0", to: []string{"r"}},
	{v: "e1", repository: "team/e1", to: []string{"r"}},
	{v: "e2", repository: "team/e2", to: []string{"r"}},
	{v: "f", repository: "team/f", to: []string{"run"}},
}

// caseNames are the cases. Each must run to its end, or the summary counts it as failed.
var caseNames = []string{
	"x", "a", "b",
	"c-hdr-c1-m", "c-hdr-c1-r", "c-hdr-c2-m", "c-hdr-c2-r",
	"d-m", "d", "d-hdr-m", "d-hdr", "e0", "e1", "e2", "f",
	"c-cfg-c1-m", "c-cfg-c1-r", "c-cfg-c2-m", "c-cfg-c2-r",
}

// accessLine matches the request and the status of registry:3's access-log line.
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

	// work holds the binaries, the certificates, the htpasswd files and the
	// kubelet's config.json. The containers mount parts of it, so it is
	// removed after Down.
	work, err := os.MkdirTemp("", "rollproof-proof-p02-")
	if err != nil {
		c.fail("a temporary folder: %v", err)
		return summary(ctx, c)
	}
	defer os.RemoveAll(work)
	prove(ctx, c, work)
	return summary(ctx, c)
}

// proof is what the cases share.
type proof struct {
	ctx            context.Context
	c              *checks
	l              *lab.Lab
	worker         string
	stage, run     lab.Registry
	mproxy, rproxy lab.Container
	img            map[string]*image
}

// prove builds, starts and fills everything, and runs the cases. The cluster
// and its containers are removed before it returns.
func prove(ctx context.Context, c *checks, work string) {
	dirs := map[string]string{}
	for _, d := range []string{"app", "proxy-src", "proxy", "certs", "auth", "kubelet"} {
		dirs[d] = filepath.Join(work, d)
		if err := os.MkdirAll(dirs[d], 0o755); err != nil {
			c.fail("the folder %s: %v", dirs[d], err)
			return
		}
	}

	// The binaries.
	if err := buildApps(ctx, dirs["app"]); err != nil {
		c.fail("building the test binaries: %v", err)
		return
	}
	if err := buildProxy(ctx, dirs["proxy-src"], dirs["proxy"]); err != nil {
		c.fail("building the logging proxy: %v", err)
		return
	}

	// The certificates, the htpasswd files and the kubelet's config.json.
	caPEM, pool, err := makeCerts(dirs["certs"])
	if err != nil {
		c.fail("making the certificates: %v", err)
		return
	}
	for file, users := range map[string][]cred{"m.htpasswd": mUsers, "r.htpasswd": rUsers} {
		var b strings.Builder
		for _, u := range users {
			b.WriteString(u.user + ":" + u.hash + "\n")
		}
		if err := os.WriteFile(filepath.Join(dirs["auth"], file), []byte(b.String()), 0o644); err != nil {
			c.fail("writing %s: %v", file, err)
			return
		}
	}
	kubeletFile := filepath.Join(dirs["kubelet"], "config.json")
	if err := writeKubeletConfig(kubeletFile); err != nil {
		c.fail("writing the kubelet's config.json: %v", err)
		return
	}
	info("the kubelet's config.json lists: %s", kubeletKeys())

	// The cluster, with the kubelet's file mounted into every node.
	mount := v1alpha4.Mount{HostPath: kubeletFile, ContainerPath: kubeletConfig, Readonly: true}
	cfg := &v1alpha4.Cluster{
		Nodes: []v1alpha4.Node{
			{Role: v1alpha4.ControlPlaneRole, ExtraMounts: []v1alpha4.Mount{mount}},
			{Role: v1alpha4.WorkerRole, ExtraMounts: []v1alpha4.Mount{mount}},
		},
		ContainerdConfigPatches: []string{certsDPatch},
	}
	l, err := lab.Up(ctx, "p02", cfg)
	if err != nil {
		c.fail("lab.Up: %v", err)
		return
	}
	defer func() {
		if err := l.Down(); err != nil {
			c.fail("l.Down(): %v", err)
		} else {
			c.ok("l.Down() returned no error")
		}
	}()
	nodes := l.Nodes()
	if len(nodes) != 2 {
		c.fail("l.Nodes() is %q, want two names", nodes)
		return
	}
	p := &proof{ctx: ctx, c: c, l: l, worker: nodes[1], img: map[string]*image{}}
	info("worker %s", p.worker)
	for _, args := range [][]string{{"containerd", "--version"}, {"crictl", "--version"}, {"kubelet", "--version"}} {
		if out, err := l.Exec(p.worker, nil, args...); err != nil {
			info("%s in the worker: %s", strings.Join(args, " "), oneLine(err.Error()))
		} else {
			info("%s in the worker: %s", strings.Join(args, " "), strings.TrimSpace(out))
		}
	}
	if out, err := l.Exec(p.worker, nil, "sh", "-c", "wc -c < "+kubeletConfig); err != nil {
		c.fail("the kubelet's file in the worker: %v", err)
		return
	} else {
		info("%s in the worker: %s bytes", kubeletConfig, strings.TrimSpace(out))
	}
	p.showPullMode()

	// The registries.
	if p.stage, err = l.Registry(ctx, "stage"); err != nil {
		c.fail("l.Registry(ctx, \"stage\"): %v", err)
		return
	}
	if p.run, err = l.Registry(ctx, "run"); err != nil {
		c.fail("l.Registry(ctx, \"run\"): %v", err)
		return
	}
	info("stage: Push %s, Node %s", p.stage.Push, p.stage.Node)
	info("run: Push %s, Node %s", p.run.Push, p.run.Node)
	mregPort, mreg, err := p.authRegistry("mreg", dirs["auth"], "m.htpasswd", "mirror")
	if err != nil {
		c.fail("M's registry: %v", err)
		return
	}
	rregPort, rreg, err := p.authRegistry("rreg", dirs["auth"], "r.htpasswd", "real")
	if err != nil {
		c.fail("R's registry: %v", err)
		return
	}
	for _, chk := range []struct {
		who, port string
		u         cred
		want      int
	}{
		{"M", mregPort, aCred, 200}, {"M", mregPort, mCred, 200}, {"M", mregPort, hCred, 401},
		{"R", rregPort, aCred, 200}, {"R", rregPort, hCred, 200}, {"R", rregPort, mCred, 401},
	} {
		got, err := status(ctx, http.DefaultClient, "http://127.0.0.1:"+chk.port+"/v2/", &chk.u)
		if err != nil || got != chk.want {
			c.fail("%s's registry answered GET /v2/ as %s with %d (%v), want %d", chk.who, chk.u.user, got, err, chk.want)
			return
		}
		info("%s's registry answers GET /v2/ as %s with %d", chk.who, chk.u.user, got)
	}

	// The two logging proxies: M over TLS under its prefix; R over HTTP on 5000 and TLS on 443.
	mproxyPort, err := freePort()
	if err != nil {
		c.fail("a port: %v", err)
		return
	}
	rproxyPort, err := freePort()
	if err != nil {
		c.fail("a port: %v", err)
		return
	}
	common := []string{"--tmpfs", "/var/lib/registry", "-v", dirs["proxy"] + ":/opt/p02:ro", "-v", dirs["certs"] + ":/certs:ro", "--entrypoint", "/opt/p02/logproxy"}
	tlsFlags := []string{"-https", ":443", "-cert", "/certs/server.pem", "-key", "/certs/server-key.pem"}
	margs := append(append([]string{"-p", "127.0.0.1:" + mproxyPort + ":443"}, common...), registryImage, "-name", "m", "-backend", "http://"+mreg.Address+":5000", "-strip", mirrorPrefix)
	if p.mproxy, err = l.Container(ctx, "mproxy", append(margs, tlsFlags...)...); err != nil {
		c.fail("l.Container(ctx, \"mproxy\", …): %v", err)
		return
	}
	rargs := append(append([]string{"-p", "127.0.0.1:" + rproxyPort + ":5000"}, common...), registryImage, "-name", "r", "-backend", "http://"+rreg.Address+":5000", "-http", ":5000")
	if p.rproxy, err = l.Container(ctx, "rproxy", append(rargs, tlsFlags...)...); err != nil {
		c.fail("l.Container(ctx, \"rproxy\", …): %v", err)
		return
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{RootCAs: pool}
	tlsClient := &http.Client{Transport: tr, Timeout: 5 * time.Second}
	for _, w := range []struct {
		what   string
		client *http.Client
		url    string
		want   int
	}{
		{"M through its proxy, under the prefix", tlsClient, "https://127.0.0.1:" + mproxyPort + mirrorPrefix + "/v2/", 401},
		{"M through its proxy, outside the prefix", tlsClient, "https://127.0.0.1:" + mproxyPort + "/v2/", 404},
		{"R through its proxy", http.DefaultClient, "http://127.0.0.1:" + rproxyPort + "/v2/", 401},
	} {
		if err := waitStatus(ctx, w.client, w.url, w.want); err != nil {
			c.fail("%s: %v", w.what, err)
			return
		}
		info("%s: GET %s answers %d", w.what, w.url, w.want)
	}
	info("mproxy: %s, address %s; rproxy: %s, address %s", p.mproxy.Name, p.mproxy.Address, p.rproxy.Name, p.rproxy.Address)

	// The images: pushed to stage, then copied by digest to where each belongs.
	for _, im := range images {
		d, err := lab.PushBinary(ctx, p.stage.Push, "stage/"+im.v, filepath.Join(dirs["app"], im.v))
		if err != nil {
			c.fail("pushing %s: %v", im.v, err)
			return
		}
		im.digest = d
		p.img[im.v] = im
		src := p.stage.Push + "/stage/" + im.v + "@" + d
		for _, to := range im.to {
			var dst string
			var auth authn.Authenticator
			switch to {
			case "m":
				dst, auth = "127.0.0.1:"+mregPort, &authn.Basic{Username: aCred.user, Password: aCred.pass}
			case "r":
				dst, auth = "127.0.0.1:"+rregPort, &authn.Basic{Username: hCred.user, Password: hCred.pass}
			case "run":
				dst = p.run.Push
			}
			if err := copyImage(ctx, src, dst+"/"+im.repository+"@"+d, auth); err != nil {
				c.fail("copying %s to %s: %v", im.v, to, err)
				return
			}
		}
		info("%s = %s, in %s as %s", im.v, d, strings.Join(im.to, ", "), im.repository)
	}

	// The names and the authority, in every node.
	for _, node := range l.Nodes() {
		for _, line := range []string{p.mproxy.Address + " " + mirrorHost, p.rproxy.Address + " " + realTLSName} {
			if _, err := l.Exec(node, nil, "sh", "-c", "echo "+line+" >> /etc/hosts"); err != nil {
				c.fail("in node %s: %v", node, err)
				return
			}
		}
		for _, h := range hosts {
			if _, err := l.Exec(node, nil, "mkdir", "-p", certsRoot+"/"+h); err != nil {
				c.fail("in node %s: %v", node, err)
				return
			}
			if _, err := l.Exec(node, strings.NewReader(string(caPEM)), "cp", "/dev/stdin", certsRoot+"/"+h+"/ca.pem"); err != nil {
				c.fail("writing the CA in node %s: %v", node, err)
				return
			}
		}
	}
	info("in every node: %s %s and %s %s in /etc/hosts; the CA at %s/HOST/ca.pem for each of %s", p.mproxy.Address, mirrorHost, p.rproxy.Address, realTLSName, certsRoot, strings.Join(hosts, ", "))

	// The namespace for the pods.
	if _, err := l.Client().CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}, metav1.CreateOptions{}); err != nil {
		c.fail("creating the namespace %s: %v", namespace, err)
		return
	}
	if err := p.waitServiceAccount(); err != nil {
		c.fail("the default service account of %s: %v", namespace, err)
		return
	}

	// The cases with the node's settings as kind writes them.
	for _, cs := range []func(){
		p.caseX, p.caseA, p.caseB,
		func() { p.caseC("hdr", "c1", "m") }, func() { p.caseC("hdr", "c1", "r") },
		func() { p.caseC("hdr", "c2", "m") }, func() { p.caseC("hdr", "c2", "r") },
		func() { p.caseD("d-m", false, true) }, func() { p.caseD("d", false, false) },
		func() { p.caseD("d-hdr-m", true, true) }, func() { p.caseD("d-hdr", true, false) },
		func() { p.caseE("e0") }, func() { p.caseE("e1") }, func() { p.caseE("e2") }, p.caseF,
	} {
		if ctx.Err() != nil {
			return
		}
		cs()
	}
	if ctx.Err() != nil {
		return
	}

	// The cases with containerd's own setting for M's host, after a restart.
	if err := p.enableConfigAuth(); err != nil {
		c.fail("containerd's setting registry.configs.%q.auth in the worker: %v", mirrorHost, err)
		return
	}
	for _, cs := range []func(){
		func() { p.caseC("cfg", "c1", "m") }, func() { p.caseC("cfg", "c1", "r") },
		func() { p.caseC("cfg", "c2", "m") }, func() { p.caseC("cfg", "c2", "r") },
	} {
		if ctx.Err() != nil {
			return
		}
		cs()
	}
}

// buildApps writes the module p02app into dir and builds one binary in it for
// each test image, named after the image's value of v.
func buildApps(ctx context.Context, dir string) error {
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(appGoMod), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(appMainGo), 0o644); err != nil {
		return err
	}
	for _, im := range images {
		if err := goBuild(ctx, dir, "-ldflags", "-s -w -X main.v="+im.v, "-o", filepath.Join(dir, im.v), "."); err != nil {
			return err
		}
	}
	return nil
}

// buildProxy writes the module p02proxy into src and builds it as out/logproxy.
func buildProxy(ctx context.Context, src, out string) error {
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte(proxyGoMod), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(proxyMainGo), 0o644); err != nil {
		return err
	}
	return goBuild(ctx, src, "-ldflags", "-s -w", "-o", filepath.Join(out, "logproxy"), ".")
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

// writeKubeletConfig writes the kubelet's config.json, as Docker writes one:
// an auths map whose entries carry auth, the base64 of USER:PASSWORD.
func writeKubeletConfig(path string) error {
	auths := map[string]map[string]string{}
	for _, e := range kubeletAuths {
		auths[e.key] = map[string]string{"auth": e.c.basic()}
	}
	b, err := json.MarshalIndent(map[string]any{"auths": auths}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// kubeletKeys names each entry of the kubelet's file and its user.
func kubeletKeys() string {
	var parts []string
	for _, e := range kubeletAuths {
		parts = append(parts, e.key+" ("+e.c.user+")")
	}
	return strings.Join(parts, ", ")
}

// showPullMode prints the lines of the worker's containerd journal that say
// which way the CRI plugin pulls, and the related lines of its configuration.
func (p *proof) showPullMode() {
	if out, err := p.l.Exec(p.worker, nil, "journalctl", "-u", "containerd", "--no-pager"); err != nil {
		info("journalctl -u containerd in the worker: %s", oneLine(err.Error()))
	} else {
		found := false
		for line := range strings.Lines(out) {
			if strings.Contains(line, "local image pull") || strings.Contains(line, "transfer service") {
				info("containerd at its start: %s", strings.TrimSpace(line))
				found = true
			}
		}
		if !found {
			info("containerd at its start: no journal line names the local image pull or the transfer service")
		}
	}
	if out, err := p.l.Exec(p.worker, nil, "containerd", "config", "dump"); err != nil {
		info("containerd config dump in the worker: %s", oneLine(err.Error()))
	} else {
		for line := range strings.Lines(out) {
			for _, key := range []string{"config_path", "use_local_image_pull", "discard_unpacked_layers", "disable_snapshot_annotations"} {
				if strings.Contains(line, key) {
					info("containerd config dump: %s", strings.TrimSpace(line))
				}
			}
		}
	}
}

// authRegistry starts registry:3 as name, with htpasswd authentication from
// dir/file and realm, plain HTTP on 5000, published on a loopback port, and
// waits until GET /v2/ answers 401. It returns the port.
func (p *proof) authRegistry(name, dir, file, realm string) (string, lab.Container, error) {
	port, err := freePort()
	if err != nil {
		return "", lab.Container{}, err
	}
	c, err := p.l.Container(p.ctx, name,
		"-p", "127.0.0.1:"+port+":5000",
		"--tmpfs", "/var/lib/registry",
		"-v", dir+":/auth:ro",
		"-e", "REGISTRY_AUTH=htpasswd",
		"-e", "REGISTRY_AUTH_HTPASSWD_REALM="+realm,
		"-e", "REGISTRY_AUTH_HTPASSWD_PATH=/auth/"+file,
		registryImage)
	if err != nil {
		return "", lab.Container{}, err
	}
	if err := waitStatus(p.ctx, http.DefaultClient, "http://127.0.0.1:"+port+"/v2/", 401); err != nil {
		return "", lab.Container{}, err
	}
	info("%s: %s, port %s, address %s, users from %s", name, c.Name, port, c.Address, file)
	return port, c, nil
}

// status sends GET url, with Basic credentials when u is not nil, and returns the status.
func status(ctx context.Context, client *http.Client, url string, u *cred) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	if u != nil {
		req.SetBasicAuth(u.user, u.pass)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

// waitStatus returns when GET url answers want, or an error after 30 seconds.
func waitStatus(ctx context.Context, client *http.Client, url string, want int) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		got, err := status(ctx, client, url, nil)
		if err == nil && got == want {
			return nil
		}
		if err == nil {
			err = fmt.Errorf("status %d", got)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("GET %s did not answer %d within 30 seconds: %v", url, want, err)
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// makeCerts writes into dir a CA (ca.pem) and a server certificate signed by
// it (server.pem, key server-key.pem) for mirror.corp.example,
// e.registry.example and 127.0.0.1, both valid for one day. The CA's key is
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
		Subject:               pkix.Name{CommonName: "rollproof proof p02 CA"},
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
		Subject:      pkix.Name{CommonName: mirrorHost},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{mirrorHost, realTLSName},
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
		if err := os.WriteFile(filepath.Join(dir, f.name), f.data, 0o644); err != nil {
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

// copyImage reads the image src, a digest reference in stage, and writes it
// by digest to dst, with auth when it is not nil.
func copyImage(ctx context.Context, src, dst string, auth authn.Authenticator) error {
	from, err := name.NewDigest(src, name.Insecure)
	if err != nil {
		return err
	}
	img, err := remote.Image(from, remote.WithContext(ctx))
	if err != nil {
		return err
	}
	to, err := name.NewDigest(dst, name.Insecure)
	if err != nil {
		return err
	}
	opts := []remote.Option{remote.WithContext(ctx)}
	if auth != nil {
		opts = append(opts, remote.WithAuth(auth))
	}
	return remote.Write(to, img, opts...)
}

// waitServiceAccount waits, at most 30 seconds, until the namespace has its
// default service account, which a pod needs before it can be created.
func (p *proof) waitServiceAccount() error {
	ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
	defer cancel()
	for {
		_, err := p.l.Client().CoreV1().ServiceAccounts(namespace).Get(ctx, "default", metav1.GetOptions{})
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w (last: %v)", ctx.Err(), err)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// hostsFile is one settings file: the host it is for, and its text.
type hostsFile struct{ host, text string }

// writeHosts writes f's text to certsRoot/HOST/hosts.toml in every node.
func (p *proof) writeHosts(f hostsFile) error {
	for _, node := range p.l.Nodes() {
		if _, err := p.l.Exec(node, strings.NewReader(f.text), "cp", "/dev/stdin", certsRoot+"/"+f.host+"/hosts.toml"); err != nil {
			return err
		}
	}
	return nil
}

// mirrorBlock is M's block for host, with its ca, and with a header that
// carries M's own credentials when header is set.
func mirrorBlock(host string, header bool) string {
	s := fmt.Sprintf("[host.%q]\n  capabilities = [\"pull\", \"resolve\"]\n  ca = %q\n", mirrorURL, certsRoot+"/"+host+"/ca.pem")
	if header {
		s += fmt.Sprintf("  [host.%q.header]\n    Authorization = %q\n", mirrorURL, "Basic "+mCred.basic())
	}
	return s
}

// serverR is the server line that names R over HTTP.
func (p *proof) serverR() string {
	return fmt.Sprintf("server = %q\n", "http://"+p.rproxy.Address+":5000")
}

// formMR is the settings file of cases x, a, b and c: M, then R as the server.
func (p *proof) formMR(host string, header bool) hostsFile {
	return hostsFile{host, p.serverR() + mirrorBlock(host, header)}
}

// request is one request that a proxy or a registry logged. user is - when
// the request carried no Basic credentials.
type request struct {
	listener, method, path, user string
	status                       int
}

func (r request) String() string {
	if r.listener == "" {
		return fmt.Sprintf("%s %s %d", r.method, r.path, r.status)
	}
	return fmt.Sprintf("%s %s %s user=%s %d", r.listener, r.method, r.path, r.user, r.status)
}

// parseRequests returns the requests in a stretch of a log: the proxies'
// REQ lines, or registry:3's access-log lines for the run's registry.
func parseRequests(who, text string) []request {
	var reqs []request
	if who == "run" {
		for _, m := range accessLine.FindAllStringSubmatch(text, -1) {
			st, _ := strconv.Atoi(m[3])
			reqs = append(reqs, request{method: m[1], path: m[2], user: "-", status: st})
		}
		return reqs
	}
	for line := range strings.Lines(text) {
		f := strings.Fields(line)
		if len(f) != 8 || f[0] != "REQ" {
			continue
		}
		st, _ := strconv.Atoi(strings.TrimPrefix(f[7], "status="))
		reqs = append(reqs, request{listener: f[2], method: f[3], path: f[4], user: strings.TrimPrefix(f[5], "user="), status: st})
	}
	return reqs
}

// watched is one container whose log a case reads.
type watched struct {
	name string
	c    lab.Container
}

func (p *proof) watched() []watched {
	return []watched{{"m", p.mproxy}, {"r", p.rproxy}, {"run", p.run.Container}}
}

// outcome is what the watched logs and containerd's journal in the worker
// recorded during one case.
type outcome struct {
	reqs    map[string][]request
	journal []string
}

// observe writes the settings files of the case label in every node, notes
// where the watched logs end, runs act, and then prints each watched log's
// new requests and the lines of the worker's containerd journal that contain
// "trying next host" or "fetch failed".
func (p *proof) observe(label string, files []hostsFile, act func()) (outcome, error) {
	var o outcome
	for _, f := range files {
		if err := p.writeHosts(f); err != nil {
			return o, fmt.Errorf("writing the settings file for %s: %w", f.host, err)
		}
		info("%s: %s/%s/hosts.toml, written in every node:", label, certsRoot, f.host)
		for line := range strings.Lines(f.text) {
			info("%s:   %s", label, strings.TrimRight(line, "\n"))
		}
	}
	ws := p.watched()
	before := make([]int, len(ws))
	for i, w := range ws {
		log, err := settledLogs(w.c)
		if err != nil {
			return o, err
		}
		before[i] = len(log)
	}
	since := time.Now().UTC().Format("2006-01-02 15:04:05.000000") + " UTC"

	act()

	o.reqs = map[string][]request{}
	for i, w := range ws {
		log, err := settledLogs(w.c)
		if err != nil {
			return o, err
		}
		if len(log) < before[i] {
			return o, fmt.Errorf("the log of %s is shorter than before case %s", w.name, label)
		}
		reqs := parseRequests(w.name, log[before[i]:])
		o.reqs[w.name] = reqs
		if len(reqs) == 0 {
			info("%s %s: no new request", label, w.name)
		}
		for _, r := range reqs {
			info("%s %s: %s", label, w.name, r)
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

// pullResult is how a pull ended: ran when the pod's container started (or
// crictl pull exited 0), else detail says why it did not. err is set when the
// pull could not be observed.
type pullResult struct {
	ran    bool
	detail string
	err    error
}

func (r pullResult) String() string {
	switch {
	case r.err != nil:
		return "not observed: " + oneLine(r.err.Error())
	case r.ran:
		return "pulled"
	default:
		return "failed: " + oneLine(r.detail)
	}
}

// podPull creates the pod name on the worker, with one container of ref and
// imagePullPolicy IfNotPresent, and waits at most 2 minutes until its
// container runs or its pull fails (ErrImagePull or ImagePullBackOff). It then
// deletes the pod at once, so that the kubelet does not try the pull again,
// and waits until the pod is gone.
func (p *proof) podPull(name, ref string) pullResult {
	pods := p.l.Client().CoreV1().Pods(namespace)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: corev1.PodSpec{
			NodeName:      p.worker,
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:            "app",
				Image:           ref,
				ImagePullPolicy: corev1.PullIfNotPresent,
			}},
		},
	}
	start := time.Now()
	if _, err := pods.Create(p.ctx, pod, metav1.CreateOptions{}); err != nil {
		return pullResult{err: err}
	}
	res := p.waitPull(name, start)
	zero := int64(0)
	if err := pods.Delete(context.Background(), name, metav1.DeleteOptions{GracePeriodSeconds: &zero}); err != nil && !apierrors.IsNotFound(err) {
		res.err = errors.Join(res.err, fmt.Errorf("deleting pod %s: %w", name, err))
		return res
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for {
		_, err := pods.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return res
		}
		select {
		case <-ctx.Done():
			res.err = errors.Join(res.err, fmt.Errorf("pod %s was not gone within a minute of its deletion", name))
			return res
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// waitPull polls the pod name every 250 ms, for at most 2 minutes.
func (p *proof) waitPull(name string, start time.Time) pullResult {
	pods := p.l.Client().CoreV1().Pods(namespace)
	ctx, cancel := context.WithTimeout(p.ctx, 2*time.Minute)
	defer cancel()
	last := "never read"
	for {
		got, err := pods.Get(ctx, name, metav1.GetOptions{})
		if err == nil && len(got.Status.ContainerStatuses) > 0 {
			st := got.Status.ContainerStatuses[0].State
			switch {
			case st.Running != nil || st.Terminated != nil:
				info("pod %s: its container started %v after the pod was created; imageID %s", name, time.Since(start).Round(time.Millisecond), got.Status.ContainerStatuses[0].ImageID)
				return pullResult{ran: true}
			case st.Waiting != nil && (st.Waiting.Reason == "ErrImagePull" || st.Waiting.Reason == "ImagePullBackOff"):
				detail := st.Waiting.Reason + ": " + st.Waiting.Message
				info("pod %s: its pull failed %v after the pod was created: %s", name, time.Since(start).Round(time.Millisecond), detail)
				return pullResult{detail: detail}
			case st.Waiting != nil:
				last = "waiting: " + st.Waiting.Reason + " " + st.Waiting.Message
			}
		} else if err == nil {
			last = "phase " + string(got.Status.Phase)
		}
		select {
		case <-ctx.Done():
			return pullResult{err: fmt.Errorf("pod %s neither ran nor failed its pull within 2 minutes: %w; last seen: %s", name, ctx.Err(), last)}
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// crictlPull runs crictl pull ref in the worker, with no --creds.
func (p *proof) crictlPull(ref string) pullResult {
	out, err := p.l.Exec(p.worker, nil, "crictl", "pull", ref)
	info("crictl pull %s: exit %s; its output:", ref, map[bool]string{true: "0", false: "non-zero"}[err == nil])
	for line := range strings.Lines(strings.TrimSpace(out)) {
		info("  %s", strings.TrimRight(line, "\n"))
	}
	if err != nil {
		return pullResult{detail: out}
	}
	return pullResult{ran: true}
}

// ref is the reference of the test image v, named after host.
func (p *proof) ref(host, v string) string {
	im := p.img[v]
	return host + "/" + im.repository + "@" + im.digest
}

// observed is the failed condition of a case that could not be observed.
func observed(err error) cond {
	return cond{false, "the case was observed (" + oneLine(err.Error()) + ")"}
}

// pullObserved is the failed condition of a case whose pull could not be observed, or nil.
func pullObserved(r pullResult) []cond {
	if r.err != nil {
		return []cond{{false, "the pull was observed (" + oneLine(r.err.Error()) + ")"}}
	}
	return nil
}

// users returns the distinct users of reqs, in their first order.
func users(reqs []request) []string {
	var us []string
	for _, r := range reqs {
		if !slices.Contains(us, r.user) {
			us = append(us, r.user)
		}
	}
	return us
}

// statuses returns the distinct statuses of reqs, in their first order.
func statuses(reqs []request) []int {
	var ss []int
	for _, r := range reqs {
		if !slices.Contains(ss, r.status) {
			ss = append(ss, r.status)
		}
	}
	return ss
}

// any reports whether one of reqs meets f.
func anyReq(reqs []request, f func(request) bool) bool { return slices.ContainsFunc(reqs, f) }

// allReq reports whether every one of reqs meets f; true for none.
func allReq(reqs []request, f func(request) bool) bool {
	return !slices.ContainsFunc(reqs, func(r request) bool { return !f(r) })
}

// sum prints one line for each of M and R in the case label: how many
// requests, which users, which statuses.
func sum(label string, o outcome, r pullResult) {
	info("%s: the pull %s", label, r)
	for _, who := range []string{"m", "r", "run"} {
		reqs := o.reqs[who]
		info("%s: %s had %d requests; users %v; statuses %v", label, who, len(reqs), users(reqs), statuses(reqs))
	}
}

// bare returns path without its query.
func bare(path string) string {
	p, _, _ := strings.Cut(path, "?")
	return p
}

// caseX: crictl pull with no --creds of an image that only M holds.
func (p *proof) caseX() {
	ref := p.ref("a.registry.example", "x")
	var r pullResult
	o, err := p.observe("x", []hostsFile{p.formMR("a.registry.example", false)}, func() { r = p.crictlPull(ref) })
	if err != nil {
		p.c.verdict("x", observed(err))
		return
	}
	sum("x", o, r)
	p.c.verdict("x", cond{true, "crictl pull with no --creds ran and was observed; " + r.String()})
}

// caseA: credentials for H in the kubelet's file, which M accepts; the image only on M.
func (p *proof) caseA() {
	ref := p.ref("a.registry.example", "a")
	var r pullResult
	o, err := p.observe("a", []hostsFile{p.formMR("a.registry.example", false)}, func() { r = p.podPull("a", ref) })
	if err != nil {
		p.c.verdict("a", observed(err))
		return
	}
	sum("a", o, r)
	manifest := mirrorPrefix + "/v2/team/a/manifests/" + p.img["a"].digest
	p.c.verdict("a", append(pullObserved(r),
		cond{r.ran, "the pod pulled the image (" + r.String() + ")"},
		cond{anyReq(o.reqs["m"], func(q request) bool {
			return bare(q.path) == manifest && q.status == 200 && q.user == aCred.user
		}), "M answered " + mirrorPrefix + "/v2/team/a/manifests/A with 200 to the user auser"},
		cond{len(o.reqs["r"]) == 0, "R had no request"},
	)...)
}

// caseB: credentials listed only under M's own host; the image only on M.
func (p *proof) caseB() {
	ref := p.ref("b.registry.example", "b")
	var r pullResult
	o, err := p.observe("b", []hostsFile{p.formMR("b.registry.example", false)}, func() { r = p.podPull("b", ref) })
	if err != nil {
		p.c.verdict("b", observed(err))
		return
	}
	sum("b", o, r)
	noUser := func(q request) bool { return q.user == "-" }
	p.c.verdict("b", append(pullObserved(r),
		cond{!r.ran, "the pod's pull failed (" + r.String() + ")"},
		cond{len(o.reqs["m"]) > 0 && allReq(o.reqs["m"], noUser), "M had requests, none with a user"},
		cond{anyReq(o.reqs["m"], func(q request) bool { return q.status == 401 }), "M answered 401"},
		cond{len(o.reqs["r"]) > 0, "the pull went on to R: R had a request"},
		cond{allReq(o.reqs["r"], noUser), "no request to R carried a user"},
	)...)
}

// caseC: M needs its own credentials (muser), which differ from H's. mech is
// "hdr" (a header in M's block) or "cfg" (containerd's registry.configs, set
// by enableConfigAuth); kubelet is "c1" (the kubelet's file has huser for H)
// or "c2" (it has nothing for H); where is "m" (the image only on M) or "r"
// (the image missing from M, only on R). Recorded, not judged.
func (p *proof) caseC(mech, kubelet, where string) {
	label := "c-" + mech + "-" + kubelet + "-" + where
	host := kubelet + ".registry.example"
	ref := p.ref(host, mech+"-"+kubelet+"-"+where)
	var r pullResult
	o, err := p.observe(label, []hostsFile{p.formMR(host, mech == "hdr")}, func() { r = p.podPull(label, ref) })
	if err != nil {
		p.c.verdict(label, observed(err))
		return
	}
	sum(label, o, r)
	p.c.verdict(label, append(pullObserved(r), cond{true, "the pull ran and was observed; " + r.String() +
		fmt.Sprintf("; M's users %v, statuses %v; R's users %v, statuses %v", users(o.reqs["m"]), statuses(o.reqs["m"]), users(o.reqs["r"]), statuses(o.reqs["r"]))})...)
}

// formD is the settings file of cases d-m, d, d-hdr-m and d-hdr for host: M
// as the server line, with the ca at the top, then the run's registry as the
// one [host] block; no place for R. With header, a top-level header table
// carries M's own credentials, which then belong to the server line.
func (p *proof) formD(host string, header bool) hostsFile {
	text := fmt.Sprintf("server = %q\nca = %q\n", mirrorURL, certsRoot+"/"+host+"/ca.pem")
	if header {
		text += fmt.Sprintf("[header]\n  Authorization = %q\n", "Basic "+mCred.basic())
	}
	text += fmt.Sprintf("[host.%q]\n  capabilities = [\"pull\"]\n", "http://"+p.run.Node)
	return hostsFile{host, text}
}

// caseD pulls the image label with the settings file of formD. With header,
// the host is d2.registry.example, for which the kubelet's file has nothing,
// and M must get muser from the header; else it is d.registry.example, for
// which the kubelet's file has auser. onM says whether M holds the image:
// when it does, the case shows that the form reaches M; when it does not, R
// holds it, and the case shows that the pull fails and never reaches R.
func (p *proof) caseD(label string, header, onM bool) {
	host, user := "d.registry.example", aCred.user
	if header {
		host, user = "d2.registry.example", mCred.user
	}
	ref := p.ref(host, label)
	var r pullResult
	o, err := p.observe(label, []hostsFile{p.formD(host, header)}, func() { r = p.podPull(label, ref) })
	if err != nil {
		p.c.verdict(label, observed(err))
		return
	}
	sum(label, o, r)
	im := p.img[label]
	manifest := mirrorPrefix + "/v2/" + im.repository + "/manifests/" + im.digest
	conds := pullObserved(r)
	if onM {
		conds = append(conds,
			cond{r.ran, "the pod pulled the image (" + r.String() + ")"},
			cond{anyReq(o.reqs["m"], func(q request) bool {
				return bare(q.path) == manifest && q.status == 200 && q.user == user
			}), "M answered " + mirrorPrefix + "/v2/" + im.repository + "/manifests/" + strings.ToUpper(label) + " with 200 to the user " + user},
			cond{len(o.reqs["r"]) == 0, "R had no request"},
		)
	} else {
		conds = append(conds,
			cond{!r.ran, "the pod's pull failed (" + r.String() + ")"},
			cond{anyReq(o.reqs["m"], func(q request) bool {
				return bare(q.path) == manifest && q.status == 404 && q.user == user
			}), "M answered " + mirrorPrefix + "/v2/" + im.repository + "/manifests/" + strings.ToUpper(label) + " with 404 to the user " + user},
			cond{len(o.reqs["r"]) == 0, "R had no request, so no credentials"},
		)
	}
	p.c.verdict(label, conds...)
}

// caseE: R over TLS from the private authority, no mirror. e0: a server line
// and no ca; e1: the ca at the top and no server line; e2: both. Recorded,
// not judged.
func (p *proof) caseE(v string) {
	ca := fmt.Sprintf("ca = %q\n", certsRoot+"/"+realTLSName+"/ca.pem")
	server := fmt.Sprintf("server = %q\n", "https://"+realTLSName)
	text := map[string]string{"e0": server, "e1": ca, "e2": server + ca}[v]
	ref := p.ref(realTLSName, v)
	var r pullResult
	o, err := p.observe(v, []hostsFile{{realTLSName, text}}, func() { r = p.podPull(v, ref) })
	if err != nil {
		p.c.verdict(v, observed(err))
		return
	}
	sum(v, o, r)
	var tlsReqs []request
	for _, q := range o.reqs["r"] {
		if q.listener == "https" {
			tlsReqs = append(tlsReqs, q)
		}
	}
	p.c.verdict(v, append(pullObserved(r), cond{true, "the pull ran and was observed; " + r.String() +
		fmt.Sprintf("; R's TLS listener had %d requests, users %v, statuses %v", len(tlsReqs), users(tlsReqs), statuses(tlsReqs))})...)
}

// caseF: an image on the authenticated host's name, only in the run's
// registry, which is listed first with pull only; crictl pull with no
// --creds. Recorded, not judged.
func (p *proof) caseF() {
	host := "f.registry.example"
	text := p.serverR() + fmt.Sprintf("[host.%q]\n  capabilities = [\"pull\"]\n", "http://"+p.run.Node) + mirrorBlock(host, false)
	ref := p.ref(host, "f")
	var r pullResult
	o, err := p.observe("f", []hostsFile{{host, text}}, func() { r = p.crictlPull(ref) })
	if err != nil {
		p.c.verdict("f", observed(err))
		return
	}
	sum("f", o, r)
	manifest := "/v2/team/f/manifests/" + p.img["f"].digest
	runServed := anyReq(o.reqs["run"], func(q request) bool { return bare(q.path) == manifest && q.status == 200 })
	p.c.verdict("f", cond{true, fmt.Sprintf("the pull ran and was observed; %s; run answered /v2/team/f/manifests/F with 200: %t; M had %d requests, R %d",
		r, runServed, len(o.reqs["m"]), len(o.reqs["r"]))})
}

// enableConfigAuth adds containerd's own credentials for M's host to the
// worker's containerd configuration, restarts containerd, waits until the CRI
// answers and the node is Ready, and prints what containerd logged about the
// setting and how the setting reads in its loaded configuration.
func (p *proof) enableConfigAuth() error {
	text := fmt.Sprintf("\n[plugins.\"io.containerd.grpc.v1.cri\".registry.configs.%q.auth]\n  username = %q\n  password = %q\n", mirrorHost, mCred.user, mCred.pass)
	if _, err := p.l.Exec(p.worker, strings.NewReader(text), "sh", "-c", "cat >> "+containerdTOML); err != nil {
		return err
	}
	info("cfg: appended to %s in the worker:", containerdTOML)
	for line := range strings.Lines(text) {
		info("cfg:   %s", strings.TrimRight(line, "\n"))
	}
	since := time.Now().UTC().Format("2006-01-02 15:04:05.000000") + " UTC"
	if _, err := p.l.Exec(p.worker, nil, "systemctl", "restart", "containerd"); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(p.ctx, 2*time.Minute)
	defer cancel()
	for {
		_, err := p.l.Exec(p.worker, nil, "crictl", "info")
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("crictl info did not succeed within 2 minutes of the restart: %v", err)
		case <-time.After(500 * time.Millisecond):
		}
	}
	for {
		n, err := p.l.Client().CoreV1().Nodes().Get(ctx, p.worker, metav1.GetOptions{})
		if err == nil && slices.ContainsFunc(n.Status.Conditions, func(c corev1.NodeCondition) bool {
			return c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue
		}) {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the worker was not Ready within 2 minutes of the restart (last: %v)", err)
		case <-time.After(500 * time.Millisecond):
		}
	}
	info("cfg: containerd restarted in the worker; crictl info answers and the node is Ready")
	journal, err := p.l.Exec(p.worker, nil, "journalctl", "-u", "containerd", "--no-pager", "--since", since)
	if err != nil {
		return err
	}
	for line := range strings.Lines(journal) {
		for _, key := range []string{"deprecated", "local image pull", "configs"} {
			if strings.Contains(line, key) {
				info("cfg: containerd: %s", strings.TrimSpace(line))
				break
			}
		}
	}
	dump, err := p.l.Exec(p.worker, nil, "containerd", "config", "dump")
	if err != nil {
		return err
	}
	lines := strings.Split(dump, "\n")
	found := false
	for i, line := range lines {
		if strings.Contains(line, mirrorHost) {
			found = true
			for _, l := range lines[i:min(i+4, len(lines))] {
				info("cfg: containerd config dump: %s", strings.TrimSpace(l))
			}
		}
	}
	if !found {
		return fmt.Errorf("containerd config dump names no %s after the restart", mirrorHost)
	}
	return nil
}

// oneLine joins the lines of s with " | ".
func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(strings.TrimSpace(s), "\n", " | ")), " ")
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
		fmt.Println("p02: all checks passed")
		return 0
	}
	fmt.Printf("p02: %d checks failed\n", c.failed)
	return 1
}
