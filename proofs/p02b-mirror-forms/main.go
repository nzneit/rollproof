//go:build linux

// Proof p02b for step 5: the registry-mirror forms that the tool will write
// into kind nodes. Every settings file names the mirror as its server line,
// with its ca (and override_path where the mirror's path holds the registry
// API's root) at the top, and the run's registry as the only [host] block;
// the real host has no place. The cases: a v2 path on the server line (A), a
// tag through that form (B), the mirror named by its container name (C), the
// nodes' route to anywhere else cut (D), Docker Hub's names and the kubelet's
// keys for it (E), a pod's own pull secret (F), what a failed pull says (G),
// and a mirror that answers with a token challenge (H).
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
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

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

	appGoMod = "module p02bapp\n\ngo 1.27\n"
	// appMainGo prints v and sleeps. With DIAL set to a comma-separated list
	// of addresses, it first dials each over TCP, for at most 3 seconds, and
	// prints one line for each.
	appMainGo = `package main

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

var v = "unset"

func main() {
	fmt.Println(v)
	if list := os.Getenv("DIAL"); list != "" {
		for _, a := range strings.Split(list, ",") {
			start := time.Now()
			c, err := net.DialTimeout("tcp", a, 3*time.Second)
			if err != nil {
				fmt.Printf("dial %s: failed after %v: %v\n", a, time.Since(start).Round(time.Millisecond), err)
				continue
			}
			c.Close()
			fmt.Printf("dial %s: connected after %v\n", a, time.Since(start).Round(time.Millisecond))
		}
	}
	time.Sleep(time.Hour)
}
`

	// standinMainGo is two stand-ins in one binary. "proxy" is the logging
	// reverse proxy of p01b and p02, which may also demand Basic credentials
	// itself and accept several path prefixes; "token" is a token service
	// that checks Basic or form credentials and returns a JWT signed with a
	// key whose certificate goes in the token's x5c header.
	standinGoMod  = "module p02bstandin\n\ngo 1.27\n"
	standinMainGo = `package main

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
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

func logf(format string, args ...any) {
	mu.Lock()
	fmt.Printf(format+"\n", args...)
	mu.Unlock()
}

func or(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// users reads USER:PASSWORD,USER:PASSWORD.
func users(list string) map[string]string {
	m := map[string]string{}
	for _, e := range strings.Split(list, ",") {
		if u, p, ok := strings.Cut(e, ":"); ok && u != "" {
			m[u] = p
		}
	}
	return m
}

// bearerSub returns the sub claim of a JWT, read without a check, or "?".
func bearerSub(tok string) string {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return "?"
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "?"
	}
	var c map[string]any
	if json.Unmarshal(b, &c) != nil {
		return "?"
	}
	if s, ok := c["sub"].(string); ok && s != "" {
		return s
	}
	return "?"
}

func serve(httpAddr, httpsAddr, cert, key string, h func(listener string) http.Handler) {
	errc := make(chan error, 2)
	if httpAddr != "" {
		go func() { errc <- http.ListenAndServe(httpAddr, h("http")) }()
	}
	if httpsAddr != "" {
		go func() { errc <- http.ListenAndServeTLS(httpsAddr, cert, key, h("https")) }()
	}
	err := <-errc
	fmt.Fprintf(os.Stderr, "standin: %v\n", err)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "standin: proxy or token")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "proxy":
		proxy(os.Args[2:])
	case "token":
		token(os.Args[2:])
	default:
		fmt.Fprintln(os.Stderr, "standin: proxy or token")
		os.Exit(2)
	}
}

// proxy writes one line for each request, after it has been answered:
// REQ NAME LISTENER METHOD PATH?QUERY user=USER auth=SCHEME sub=SUB status=CODE
func proxy(args []string) {
	fs := flag.NewFlagSet("proxy", flag.ExitOnError)
	name := fs.String("name", "", "the name that each log line carries")
	backend := fs.String("backend", "", "the registry behind the proxy")
	strip := fs.String("strip", "", "comma-separated path prefixes; a request must have one, which is removed before it is passed on")
	httpAddr := fs.String("http", "", "the address of the plain HTTP listener")
	httpsAddr := fs.String("https", "", "the address of the TLS listener")
	cert := fs.String("cert", "", "the TLS certificate")
	key := fs.String("key", "", "the TLS key")
	userList := fs.String("users", "", "USER:PASSWORD,...; when set, a request needs one of them as Basic credentials")
	fs.Parse(args)
	target, err := url.Parse(*backend)
	if err != nil || target.Host == "" {
		fmt.Fprintf(os.Stderr, "proxy: bad -backend %q: %v\n", *backend, err)
		os.Exit(2)
	}
	var prefixes []string
	if *strip != "" {
		prefixes = strings.Split(*strip, ",")
	}
	allowed := users(*userList)
	rp := httputil.NewSingleHostReverseProxy(target)
	h := func(listener string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &recorder{ResponseWriter: w}
			uri := r.URL.RequestURI()
			user, scheme, sub := "-", "-", "-"
			if hd := r.Header.Get("Authorization"); hd != "" {
				s, rest, _ := strings.Cut(hd, " ")
				scheme = strings.ToLower(s)
				if u, _, ok := r.BasicAuth(); ok && u != "" {
					user = u
				}
				if scheme == "bearer" {
					sub = bearerSub(strings.TrimSpace(rest))
				}
			}
			defer func() {
				status := rec.status
				if status == 0 {
					status = http.StatusOK
				}
				logf("REQ %s %s %s %s user=%s auth=%s sub=%s status=%d", *name, listener, r.Method, uri, user, scheme, sub, status)
			}()
			out := r
			if len(prefixes) > 0 {
				matched := ""
				for _, p := range prefixes {
					if strings.HasPrefix(r.URL.Path, p+"/") {
						matched = p
						break
					}
				}
				if matched == "" {
					http.NotFound(rec, r)
					return
				}
				out = r.Clone(r.Context())
				out.URL.Path = strings.TrimPrefix(r.URL.Path, matched)
				out.URL.RawPath = ""
			}
			if len(allowed) > 0 {
				u, pw, ok := r.BasicAuth()
				if want, known := allowed[u]; !ok || !known || want != pw {
					rec.Header().Set("WWW-Authenticate", "Basic realm=\""+*name+"\"")
					rec.Header().Set("Content-Type", "application/json")
					rec.WriteHeader(http.StatusUnauthorized)
					fmt.Fprint(rec, "{\"errors\":[{\"code\":\"UNAUTHORIZED\",\"message\":\"authentication required\"}]}\n")
					return
				}
				if out == r {
					out = r.Clone(r.Context())
				}
				out.Header.Del("Authorization")
			}
			rp.ServeHTTP(rec, out)
		})
	}
	fmt.Printf("proxy %s: backend %s, strip %q, users %d, http %q, https %q\n", *name, *backend, *strip, len(allowed), *httpAddr, *httpsAddr)
	serve(*httpAddr, *httpsAddr, *cert, *key, h)
}

// token writes one line for each request:
// TOK NAME METHOD PATH via=basic|form|none user=USER grant=GRANT service=SERVICE scope=SCOPES status=CODE
func token(args []string) {
	fs := flag.NewFlagSet("token", flag.ExitOnError)
	name := fs.String("name", "", "the name that each log line carries")
	httpsAddr := fs.String("https", "", "the address of the TLS listener")
	cert := fs.String("cert", "", "the TLS certificate")
	key := fs.String("key", "", "the TLS key")
	signCert := fs.String("sign-cert", "", "the certificate of the signing key, put in each token's x5c header")
	signKey := fs.String("sign-key", "", "the signing key, PKCS #8, ECDSA P-256")
	issuer := fs.String("issuer", "", "the iss claim")
	service := fs.String("service", "", "the aud claim when a request names no service")
	userList := fs.String("users", "", "USER:PASSWORD,...: the credentials that get a token")
	fs.Parse(args)
	certPEM, err := os.ReadFile(*signCert)
	if err != nil {
		fmt.Fprintf(os.Stderr, "token: %v\n", err)
		os.Exit(2)
	}
	cb, _ := pem.Decode(certPEM)
	keyPEM, err := os.ReadFile(*signKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "token: %v\n", err)
		os.Exit(2)
	}
	kb, _ := pem.Decode(keyPEM)
	if cb == nil || kb == nil {
		fmt.Fprintln(os.Stderr, "token: the signing certificate or key is not PEM")
		os.Exit(2)
	}
	k, err := x509.ParsePKCS8PrivateKey(kb.Bytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "token: %v\n", err)
		os.Exit(2)
	}
	signer, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		fmt.Fprintln(os.Stderr, "token: the signing key is not ECDSA")
		os.Exit(2)
	}
	allowed := users(*userList)
	h := func(listener string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			via, user, pass, grant, svc := "none", "", "", "", ""
			var scopes []string
			switch r.Method {
			case http.MethodGet:
				if u, p, ok := r.BasicAuth(); ok {
					via, user, pass = "basic", u, p
				}
				for _, s := range r.URL.Query()["scope"] {
					scopes = append(scopes, strings.Fields(s)...)
				}
				svc = r.URL.Query().Get("service")
			case http.MethodPost:
				if r.ParseForm() == nil {
					grant = r.PostForm.Get("grant_type")
					if u := r.PostForm.Get("username"); u != "" {
						via, user, pass = "form", u, r.PostForm.Get("password")
					}
					scopes = strings.Fields(r.PostForm.Get("scope"))
					svc = r.PostForm.Get("service")
				}
			}
			status := http.StatusOK
			want, known := allowed[user]
			switch {
			case r.URL.Path != "/token":
				status = http.StatusNotFound
			case user == "" || !known || want != pass:
				status = http.StatusUnauthorized
			}
			defer func() {
				logf("TOK %s %s %s via=%s user=%s grant=%s service=%s scope=%s status=%d", *name, r.Method, r.URL.Path, via, or(user), or(grant), or(svc), or(strings.Join(scopes, ",")), status)
			}()
			if status != http.StatusOK {
				if status == http.StatusUnauthorized {
					w.Header().Set("WWW-Authenticate", "Basic realm=\""+*name+"\"")
				}
				w.WriteHeader(status)
				fmt.Fprint(w, "{\"details\":\"no token\"}\n")
				return
			}
			if svc == "" {
				svc = *service
			}
			access := []map[string]any{}
			for _, sc := range scopes {
				typ, rest, ok := strings.Cut(sc, ":")
				i := strings.LastIndex(rest, ":")
				if !ok || typ != "repository" || i < 0 {
					continue
				}
				acts := []string{}
				for _, a := range strings.Split(rest[i+1:], ",") {
					if a == "pull" {
						acts = append(acts, a)
					}
				}
				access = append(access, map[string]any{"type": typ, "name": rest[:i], "actions": acts})
			}
			now := time.Now()
			tok, err := sign(signer, cb.Bytes, *issuer, svc, user, access, now)
			if err != nil {
				status = http.StatusInternalServerError
				w.WriteHeader(status)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"token": tok, "access_token": tok, "expires_in": 300, "issued_at": now.UTC().Format(time.RFC3339)})
		})
	}
	fmt.Printf("token %s: issuer %s, users %d, https %q\n", *name, *issuer, len(allowed), *httpsAddr)
	serve("", *httpsAddr, *cert, *key, h)
}

// sign returns a JWT signed with ES256, with the signing certificate as x5c.
func sign(k *ecdsa.PrivateKey, certDER []byte, iss, aud, sub string, access []map[string]any, now time.Time) (string, error) {
	hdr, err := json.Marshal(map[string]any{"alg": "ES256", "typ": "JWT", "x5c": []string{base64.StdEncoding.EncodeToString(certDER)}})
	if err != nil {
		return "", err
	}
	jti := make([]byte, 8)
	rand.Read(jti)
	claims, err := json.Marshal(map[string]any{"iss": iss, "sub": sub, "aud": aud, "exp": now.Add(5 * time.Minute).Unix(), "nbf": now.Add(-time.Minute).Unix(), "iat": now.Unix(), "jti": hex.EncodeToString(jti), "access": access})
	if err != nil {
		return "", err
	}
	input := base64.RawURLEncoding.EncodeToString(hdr) + "." + base64.RawURLEncoding.EncodeToString(claims)
	sum := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, k, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return input + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}
`

	registryImage = "docker.io/library/registry:3"
	clusterName   = "p02b"
	namespace     = "p02b"
	// prefix begins the name of every container that the lab starts.
	prefix        = "rollproof-proof-" + clusterName + "-"
	certsRoot     = "/etc/containerd/certs.d"
	kubeletConfig = "/var/lib/kubelet/config.json"
	podSubnet     = "10.244.0.0/16"
	serviceSubnet = "10.96.0.0/16"
	cutChain      = "ROLLPROOF-CUT"

	// publicRef is a small public image, busybox 1.37.0's index. It is pulled
	// once before the cut and tried once after it, with no credentials.
	publicRef = "docker.io/library/busybox@sha256:a7dbc319b1438115a207e11930dc4bb3e643dcb3ec0bdd694bb36cd8fad289ec"

	// mirrorPrefix is the path under which M's registry API sits, as in p02;
	// folderDocker and folderIndex are two more prefixes of M, one for each
	// settings folder that case E writes, so that M's log shows which
	// folder a node used.
	mirrorPrefix = "/registry-example"
	folderDocker = "/folder-docker.io"
	folderIndex  = "/folder-index.docker.io"

	m2Service = "rollproof-p02b-m2"
	tokIssuer = "rollproof-p02b-tok"

	hostA = "a.registry.example"
	hostB = "b.registry.example"
	hostF = "f.registry.example"
	hostG = "g.registry.example"
	hostH = "h.registry.example"

	zeroDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
)

// The stand-ins that the nodes reach by name: the engine's name for each.
var (
	mproxyName  = prefix + "mproxy"
	cproxyName  = prefix + "cproxy"
	m2proxyName = prefix + "m2proxy"
	tokName     = prefix + "tok"
	// byName are the names in the server certificate.
	byName = []string{mproxyName, cproxyName, m2proxyName, tokName}
)

// cred is one made-up user. The values are throwaway, for stand-ins that live
// only during the run.
type cred struct{ user, pass string }

func (c cred) basic() string { return base64.StdEncoding.EncodeToString([]byte(c.user + ":" + c.pass)) }
func (c cred) pair() string  { return c.user + ":" + c.pass }

var (
	aCred  = cred{"auser", "a-pass-p02b"}   // H's credentials for M (cases B, D, G)
	dkCred = cred{"dkuser", "dk-pass-p02b"} // under the key docker.io
	ikCred = cred{"ikuser", "ik-pass-p02b"} // under the key index.docker.io
	vkCred = cred{"vkuser", "vk-pass-p02b"} // under the key https://index.docker.io/v1/
	rkCred = cred{"rkuser", "rk-pass-p02b"} // under the key registry-1.docker.io
	fCred  = cred{"fuser", "f-pass-p02b"}   // in case F's pull secret
	hCred  = cred{"huser", "h-pass-p02b"}   // H's credentials for T (case H)

	// mUsers are the users M's proxy accepts; tUsers those T accepts.
	mUsers = []cred{aCred, dkCred, ikCred, vkCred, rkCred, fCred}
	tUsers = []cred{hCred}
)

// entry is one entry of the kubelet's config.json.
type entry struct {
	key string
	c   cred
}

// baseAuths are the entries the kubelet's file holds outside case E.
var baseAuths = []entry{{hostB, aCred}, {hostG, aCred}, {hostH, hCred}}

// dockerKeys are the keys case E tries, one at a time, beside baseAuths.
var dockerKeys = []entry{
	{"docker.io", dkCred},
	{"index.docker.io", ikCred},
	{"https://index.docker.io/v1/", vkCred},
	{"registry-1.docker.io", rkCred},
}

// dockerNames are the names case E pulls for the digest d.
func dockerNames(d string) []string {
	return []string{"docker.io/library/app@" + d, "library/app@" + d, "app@" + d, "index.docker.io/library/app@" + d}
}

// image is one test image: the value of v in its binary, the stand-in it is
// pushed to ("c" for C, "m" for M, "m2" for M2), its repository there, a tag
// when it has one, and its digest once pushed.
type image struct {
	v, to, repository, tag string
	digest                 string
}

var images = []*image{
	{v: "a", to: "c", repository: "proxy-project/team/app", tag: "v1"},
	{v: "dial", to: "c", repository: "proxy-project/team/dial"},
	{v: "b", to: "m", repository: "team/app", tag: "v1"},
	{v: "d", to: "m", repository: "team/d"},
	{v: "e", to: "m", repository: "library/app"},
	{v: "f", to: "m", repository: "team/f"},
	{v: "g", to: "m", repository: "team/g", tag: "v1"},
	{v: "h", to: "m2", repository: "team/h"},
}

// caseNames are the cases. Each must run to its end, or the summary counts it as failed.
var caseNames = []string{"a1", "a2", "a3", "a4", "b", "c", "d", "e", "f", "g1", "g1t", "g2", "g2t", "h"}

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

	// work holds the binaries, the certificates, the kubelet's config.json
	// and M2's storage. The containers mount parts of it, so it is removed
	// after Down.
	work, err := os.MkdirTemp("", "rollproof-proof-p02b-")
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
	ctx    context.Context
	c      *checks
	l      *lab.Lab
	worker string
	dirs   map[string]string
	pool   *x509.CertPool

	kubeletFile string
	run         lab.Registry
	runproxy    lab.Container
	mproxy      lab.Container
	cproxy      lab.Container
	m2proxy     lab.Container // started by case H
	tok         lab.Container // started by case H
	img         map[string]*image
	hostsAtUp   map[string]string // each node's /etc/hosts right after Up
	tlsClient   *http.Client
	bPulled     bool // case B's pod pulled its image
}

// prove builds, starts and fills everything, and runs the cases. The cluster
// and its containers are removed before it returns.
func prove(ctx context.Context, c *checks, work string) {
	dirs := map[string]string{}
	for _, d := range []string{"app", "standin-src", "standin", "certs", "kubelet", "m2data"} {
		dirs[d] = filepath.Join(work, d)
		if err := os.MkdirAll(dirs[d], 0o755); err != nil {
			c.fail("the folder %s: %v", dirs[d], err)
			return
		}
	}
	if err := buildApps(ctx, dirs["app"]); err != nil {
		c.fail("building the test binaries: %v", err)
		return
	}
	if err := buildStandin(ctx, dirs["standin-src"], dirs["standin"]); err != nil {
		c.fail("building the stand-in binary: %v", err)
		return
	}
	caPEM, pool, err := makeCerts(dirs["certs"])
	if err != nil {
		c.fail("making the certificates: %v", err)
		return
	}
	info("the server certificate names %s and 127.0.0.1", strings.Join(byName, ", "))

	kubeletFile := filepath.Join(dirs["kubelet"], "config.json")
	b, err := kubeletJSON(baseAuths)
	if err == nil {
		err = os.WriteFile(kubeletFile, b, 0o644)
	}
	if err != nil {
		c.fail("writing the kubelet's config.json: %v", err)
		return
	}
	info("the kubelet's config.json lists: %s", keysOf(baseAuths))

	mount := v1alpha4.Mount{HostPath: kubeletFile, ContainerPath: kubeletConfig, Readonly: true}
	cfg := &v1alpha4.Cluster{
		Nodes: []v1alpha4.Node{
			{Role: v1alpha4.ControlPlaneRole, ExtraMounts: []v1alpha4.Mount{mount}},
			{Role: v1alpha4.WorkerRole, ExtraMounts: []v1alpha4.Mount{mount}},
		},
		Networking:              v1alpha4.Networking{PodSubnet: podSubnet, ServiceSubnet: serviceSubnet},
		ContainerdConfigPatches: []string{certsDPatch},
	}
	l, err := lab.Up(ctx, clusterName, cfg)
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
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{RootCAs: pool}
	p := &proof{ctx: ctx, c: c, l: l, worker: nodes[1], dirs: dirs, pool: pool, kubeletFile: kubeletFile,
		img: map[string]*image{}, hostsAtUp: map[string]string{}, tlsClient: &http.Client{Transport: tr, Timeout: 10 * time.Second}}
	info("worker %s", p.worker)
	for _, args := range [][]string{{"containerd", "--version"}, {"crictl", "--version"}, {"kubelet", "--version"}, {"iptables", "--version"}, {"iptables-nft", "--version"}, {"nft", "--version"}} {
		if out, err := l.Exec(p.worker, nil, args...); err != nil {
			info("%s in the worker: %s", strings.Join(args, " "), oneLine(err.Error()))
		} else {
			info("%s in the worker: %s", strings.Join(args, " "), strings.TrimSpace(out))
		}
	}
	for _, node := range nodes {
		out, err := l.Exec(node, nil, "cat", "/etc/hosts")
		if err != nil {
			c.fail("reading /etc/hosts in %s: %v", node, err)
			return
		}
		p.hostsAtUp[node] = out
	}
	info("c: /etc/hosts in the worker right after Up, before any stand-in started:")
	printLines("c:   ", p.hostsAtUp[p.worker])

	// The stand-ins: the run's registry behind a logging proxy; M (a logging
	// proxy over TLS that demands Basic credentials, under three prefixes)
	// in front of an open registry; C (a logging proxy over TLS) in front of
	// an open registry whose repositories begin with proxy-project/.
	if err := p.startStandins(); err != nil {
		c.fail("starting the stand-ins: %v", err)
		return
	}

	// The images.
	push := map[string]string{"c": "", "m": ""}
	var creg, mreg lab.Registry
	if creg, err = l.Registry(ctx, "creg"); err != nil {
		c.fail("l.Registry(ctx, \"creg\"): %v", err)
		return
	}
	if mreg, err = l.Registry(ctx, "mreg"); err != nil {
		c.fail("l.Registry(ctx, \"mreg\"): %v", err)
		return
	}
	push["c"], push["m"] = creg.Push, mreg.Push
	if err := p.startProxies(creg, mreg); err != nil {
		c.fail("starting the proxies: %v", err)
		return
	}
	for _, im := range images {
		p.img[im.v] = im
		if im.to == "m2" {
			continue // case H pushes it
		}
		if err := pushImage(ctx, push[im.to], im, filepath.Join(dirs["app"], im.v)); err != nil {
			c.fail("pushing %s: %v", im.v, err)
			return
		}
		info("%s = %s, in %s as %s%s", im.v, im.digest, im.to, im.repository, map[bool]string{true: " and :" + im.tag, false: ""}[im.tag != ""])
	}

	// The authority, in every node, for the hosts that cases A to D and F to
	// H name. Case E writes its own folders after the cut.
	for _, node := range nodes {
		for _, h := range []string{hostA, hostB, hostF, hostG, hostH} {
			if err := p.writeCA(node, h, caPEM); err != nil {
				c.fail("%v", err)
				return
			}
		}
	}
	info("in every node: the CA at %s/HOST/ca.pem for each of %s", certsRoot, strings.Join([]string{hostA, hostB, hostF, hostG, hostH}, ", "))

	if _, err := l.Client().CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}, metav1.CreateOptions{}); err != nil {
		c.fail("creating the namespace %s: %v", namespace, err)
		return
	}
	if err := p.waitServiceAccount(); err != nil {
		c.fail("the default service account of %s: %v", namespace, err)
		return
	}

	for _, cs := range []func(){p.caseA, p.caseB, p.caseC} {
		if ctx.Err() != nil {
			return
		}
		cs()
	}
	if ctx.Err() != nil {
		return
	}
	// Case D cuts the route; E to H run only when the cut held, so that
	// nothing in them can reach a public host.
	if !p.caseD() {
		c.fail("the cut did not hold, or could not be shown; cases E to H were not run")
		return
	}
	for _, cs := range []func(){func() { p.caseE(caPEM) }, p.caseF, p.caseG, p.caseH} {
		if ctx.Err() != nil {
			return
		}
		cs()
	}
}

// startStandins starts the run's registry and its logging proxy.
func (p *proof) startStandins() error {
	var err error
	if p.run, err = p.l.Registry(p.ctx, "run"); err != nil {
		return err
	}
	port, err := freePort()
	if err != nil {
		return err
	}
	args := append([]string{"-p", "127.0.0.1:" + port + ":5000"}, p.standinArgs()...)
	args = append(args, "proxy", "-name", "run", "-backend", "http://"+p.run.Address+":5000", "-http", ":5000")
	if p.runproxy, err = p.l.Container(p.ctx, "runproxy", args...); err != nil {
		return err
	}
	if err := waitStatus(p.ctx, http.DefaultClient, "http://127.0.0.1:"+port+"/v2/", 200, nil); err != nil {
		return fmt.Errorf("the run's proxy: %w", err)
	}
	info("run: %s at %s; runproxy: %s at %s, which the nodes reach as http://%s:5000", p.run.Name, p.run.Address, p.runproxy.Name, p.runproxy.Address, p.runproxy.Address)
	return nil
}

// standinArgs are the engine's run arguments that run the stand-in binary in
// a container of the registry image, with the certificates mounted.
func (p *proof) standinArgs() []string {
	return []string{"--tmpfs", "/var/lib/registry", "-v", p.dirs["standin"] + ":/opt/p02b:ro", "-v", p.dirs["certs"] + ":/certs:ro", "--entrypoint", "/opt/p02b/standin", registryImage}
}

var tlsFlags = []string{"-https", ":443", "-cert", "/certs/server.pem", "-key", "/certs/server-key.pem"}

// startProxies starts M's proxy and C's proxy and checks each from the runner.
func (p *proof) startProxies(creg, mreg lab.Registry) error {
	mport, err := freePort()
	if err != nil {
		return err
	}
	cport, err := freePort()
	if err != nil {
		return err
	}
	var us []string
	for _, u := range mUsers {
		us = append(us, u.pair())
	}
	margs := append([]string{"-p", "127.0.0.1:" + mport + ":443"}, p.standinArgs()...)
	margs = append(append(margs, "proxy", "-name", "m", "-backend", "http://"+mreg.Address+":5000",
		"-strip", strings.Join([]string{mirrorPrefix, folderDocker, folderIndex}, ","), "-users", strings.Join(us, ",")), tlsFlags...)
	if p.mproxy, err = p.l.Container(p.ctx, "mproxy", margs...); err != nil {
		return err
	}
	cargs := append([]string{"-p", "127.0.0.1:" + cport + ":443"}, p.standinArgs()...)
	cargs = append(append(cargs, "proxy", "-name", "c", "-backend", "http://"+creg.Address+":5000"), tlsFlags...)
	if p.cproxy, err = p.l.Container(p.ctx, "cproxy", cargs...); err != nil {
		return err
	}
	if p.mproxy.Name != mproxyName || p.cproxy.Name != cproxyName {
		return fmt.Errorf("the proxies are named %s and %s, want %s and %s", p.mproxy.Name, p.cproxy.Name, mproxyName, cproxyName)
	}
	for _, w := range []struct {
		what string
		url  string
		u    *cred
		want int
	}{
		{"M under its prefix, no credentials", "https://127.0.0.1:" + mport + mirrorPrefix + "/v2/", nil, 401},
		{"M under its prefix, as auser", "https://127.0.0.1:" + mport + mirrorPrefix + "/v2/", &aCred, 200},
		{"M outside its prefixes", "https://127.0.0.1:" + mport + "/v2/", &aCred, 404},
		{"C", "https://127.0.0.1:" + cport + "/v2/", nil, 200},
	} {
		if err := waitStatus(p.ctx, p.tlsClient, w.url, w.want, w.u); err != nil {
			return fmt.Errorf("%s: %w", w.what, err)
		}
		info("%s: GET %s answers %d", w.what, w.url, w.want)
	}
	info("mproxy: %s at %s, backend %s (%s); cproxy: %s at %s, backend %s (%s)", p.mproxy.Name, p.mproxy.Address, mreg.Name, mreg.Address, p.cproxy.Name, p.cproxy.Address, creg.Name, creg.Address)
	return nil
}

// buildApps writes the module p02bapp into dir and builds one binary in it
// for each test image, named after the image's value of v.
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

// buildStandin writes the module p02bstandin into src and builds it as out/standin.
func buildStandin(ctx context.Context, src, out string) error {
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte(standinGoMod), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(standinMainGo), 0o644); err != nil {
		return err
	}
	return goBuild(ctx, src, "-ldflags", "-s -w", "-o", filepath.Join(out, "standin"), ".")
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

// pushImage pushes the binary as an image to the registry at push, by digest,
// and tags it when im has a tag.
func pushImage(ctx context.Context, push string, im *image, binary string) error {
	d, err := lab.PushBinary(ctx, push, im.repository, binary)
	if err != nil {
		return err
	}
	im.digest = d
	if im.tag == "" {
		return nil
	}
	ref, err := name.NewDigest(push+"/"+im.repository+"@"+d, name.Insecure)
	if err != nil {
		return err
	}
	img, err := remote.Image(ref, remote.WithContext(ctx))
	if err != nil {
		return err
	}
	tag, err := name.NewTag(push+"/"+im.repository+":"+im.tag, name.Insecure)
	if err != nil {
		return err
	}
	return remote.Tag(tag, img, remote.WithContext(ctx))
}

// kubeletJSON is the kubelet's config.json, as Docker writes one: an auths
// map whose entries carry auth, the base64 of USER:PASSWORD.
func kubeletJSON(entries []entry) ([]byte, error) {
	auths := map[string]map[string]string{}
	for _, e := range entries {
		auths[e.key] = map[string]string{"auth": e.c.basic()}
	}
	return json.MarshalIndent(map[string]any{"auths": auths}, "", "  ")
}

func keysOf(entries []entry) string {
	var parts []string
	for _, e := range entries {
		parts = append(parts, e.key+" ("+e.c.user+")")
	}
	return strings.Join(parts, ", ")
}

// makeCerts writes into dir a CA (ca.pem); a server certificate signed by it
// (server.pem, key server-key.pem) for the names in byName and 127.0.0.1; and
// a signing certificate for T's tokens (signer.pem, key signer-key.pem). All
// are valid for one day. The CA's key is never written. It returns the CA's
// PEM and a pool that holds the CA.
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
		Subject:               pkix.Name{CommonName: "rollproof proof p02b CA"},
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
	leaf := func(cn string, dns []string, ips []net.IP, eku []x509.ExtKeyUsage) ([]byte, []byte, error) {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, nil, err
		}
		s, err := serial()
		if err != nil {
			return nil, nil, err
		}
		t := &x509.Certificate{SerialNumber: s, Subject: pkix.Name{CommonName: cn}, NotBefore: notBefore, NotAfter: notAfter,
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: eku, DNSNames: dns, IPAddresses: ips}
		der, err := x509.CreateCertificate(rand.Reader, t, caCert, &k.PublicKey, caKey)
		if err != nil {
			return nil, nil, err
		}
		kder, err := x509.MarshalPKCS8PrivateKey(k)
		if err != nil {
			return nil, nil, err
		}
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kder}), nil
	}
	serverPEM, serverKey, err := leaf(byName[0], byName, []net.IP{net.ParseIP("127.0.0.1")}, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	if err != nil {
		return nil, nil, err
	}
	signerPEM, signerKey, err := leaf("rollproof proof p02b token signer", nil, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	for _, f := range []struct {
		name string
		data []byte
	}{{"ca.pem", caPEM}, {"server.pem", serverPEM}, {"server-key.pem", serverKey}, {"signer.pem", signerPEM}, {"signer-key.pem", signerKey}} {
		if err := os.WriteFile(filepath.Join(dir, f.name), f.data, 0o644); err != nil {
			return nil, nil, err
		}
	}
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	return caPEM, pool, nil
}

func serial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
}

func freePort() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	ln.Close()
	return port, nil
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
func waitStatus(ctx context.Context, client *http.Client, url string, want int, u *cred) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		got, err := status(ctx, client, url, u)
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

// writeCA writes the CA to certsRoot/HOST/ca.pem in node.
func (p *proof) writeCA(node, host string, caPEM []byte) error {
	if _, err := p.l.Exec(node, nil, "mkdir", "-p", certsRoot+"/"+host); err != nil {
		return fmt.Errorf("in node %s: %w", node, err)
	}
	if _, err := p.l.Exec(node, strings.NewReader(string(caPEM)), "cp", "/dev/stdin", certsRoot+"/"+host+"/ca.pem"); err != nil {
		return fmt.Errorf("writing the CA in node %s: %w", node, err)
	}
	return nil
}

// hostsFile is one settings file: the host it is for, and its text.
type hostsFile struct{ host, text string }

// formD is the settings file for host: server as the server line, with
// override_path when override is set and the ca at the top, then the run's
// registry as the only [host] block. The real host has no place.
func (p *proof) formD(host, server string, override bool) hostsFile {
	text := fmt.Sprintf("server = %q\n", server)
	if override {
		text += "override_path = true\n"
	}
	text += fmt.Sprintf("ca = %q\n", certsRoot+"/"+host+"/ca.pem")
	text += fmt.Sprintf("[host.%q]\n  capabilities = [\"pull\"]\n", "http://"+p.runproxy.Address+":5000")
	return hostsFile{host, text}
}

// writeHosts writes f's text to certsRoot/HOST/hosts.toml in every node.
func (p *proof) writeHosts(f hostsFile) error {
	for _, node := range p.l.Nodes() {
		if _, err := p.l.Exec(node, strings.NewReader(f.text), "cp", "/dev/stdin", certsRoot+"/"+f.host+"/hosts.toml"); err != nil {
			return err
		}
	}
	return nil
}

// request is one line that a proxy (REQ) or the token service (TOK) logged.
type request struct {
	kind, listener, method, path string
	kv                           map[string]string
	status                       int
}

func (r request) user() string { return r.kv["user"] }

func (r request) String() string {
	if r.kind == "TOK" {
		return fmt.Sprintf("%s %s via=%s user=%s grant=%s service=%s scope=%s %d", r.method, r.path, r.kv["via"], r.kv["user"], r.kv["grant"], r.kv["service"], r.kv["scope"], r.status)
	}
	s := fmt.Sprintf("%s %s %s user=%s auth=%s", r.listener, r.method, r.path, r.kv["user"], r.kv["auth"])
	if sub := r.kv["sub"]; sub != "" && sub != "-" {
		s += " sub=" + sub
	}
	return s + " " + strconv.Itoa(r.status)
}

// parseRequests returns the REQ and TOK lines in a stretch of a log.
func parseRequests(text string) []request {
	var reqs []request
	for line := range strings.Lines(text) {
		f := strings.Fields(line)
		var r request
		var rest []string
		switch {
		case len(f) >= 5 && f[0] == "REQ":
			r, rest = request{kind: "REQ", listener: f[2], method: f[3], path: f[4]}, f[5:]
		case len(f) >= 4 && f[0] == "TOK":
			r, rest = request{kind: "TOK", method: f[2], path: f[3]}, f[4:]
		default:
			continue
		}
		r.kv = map[string]string{}
		for _, x := range rest {
			if k, v, ok := strings.Cut(x, "="); ok {
				r.kv[k] = v
			}
		}
		r.status, _ = strconv.Atoi(r.kv["status"])
		reqs = append(reqs, r)
	}
	return reqs
}

// watched is one container whose log a case reads.
type watched struct {
	name string
	c    lab.Container
}

// watched returns the stand-ins that have started: M, C, the run's proxy,
// M2's proxy and T.
func (p *proof) watched() []watched {
	var ws []watched
	for _, w := range []watched{{"m", p.mproxy}, {"c", p.cproxy}, {"run", p.runproxy}, {"m2", p.m2proxy}, {"t", p.tok}} {
		if w.c.Name != "" {
			ws = append(ws, w)
		}
	}
	return ws
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
		printLines(label+":   ", f.text)
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
		reqs := parseRequests(log[before[i]:])
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

// podOpts are the optional parts of a pod pull.
type podOpts struct {
	secret   string          // an imagePullSecrets entry
	env      []corev1.EnvVar // the container's environment
	backoff  bool            // after the first failure, wait for ImagePullBackOff and print the pod's events
	logLines int             // after the container starts, wait until its log holds this many lines
}

// pullResult is how a pull ended: ran when the pod's container started (or
// crictl pull exited 0), else detail says why it did not. err is set when the
// pull could not be observed.
type pullResult struct {
	ran      bool
	detail   string
	statuses []string // each distinct waiting state of the container, as REASON: MESSAGE
	events   []string // the pod's events, when backoff was set
	logs     string
	took     time.Duration
	err      error
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

// rmi removes ref from the worker's image store, waiting at most 10 seconds
// for a container that still holds it to go.
func (p *proof) rmi(ref string) {
	for range 20 {
		if _, err := p.l.Exec(p.worker, nil, "crictl", "inspecti", ref); err != nil {
			return
		}
		p.l.Exec(p.worker, nil, "crictl", "rmi", ref)
		time.Sleep(500 * time.Millisecond)
	}
	info("%s is still in the worker's image store after 10 seconds of crictl rmi", ref)
}

// podPull removes held as the image from the worker's store, then creates
// the pod name on the worker, with one container of ref and imagePullPolicy
// Always, and waits at most 2 minutes until its container runs or its pull
// fails. It then deletes the pod at once, so that the kubelet does not try
// the pull again, and waits until the pod is gone.
func (p *proof) podPull(name, ref, held string, o podOpts) pullResult {
	p.rmi(held)
	pods := p.l.Client().CoreV1().Pods(namespace)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: corev1.PodSpec{
			NodeName:      p.worker,
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:            "app",
				Image:           ref,
				ImagePullPolicy: corev1.PullAlways,
				Env:             o.env,
			}},
		},
	}
	if o.secret != "" {
		pod.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: o.secret}}
	}
	start := time.Now()
	if _, err := pods.Create(p.ctx, pod, metav1.CreateOptions{}); err != nil {
		return pullResult{err: err}
	}
	res := p.waitPull(name, start, o)
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
func (p *proof) waitPull(name string, start time.Time, o podOpts) pullResult {
	pods := p.l.Client().CoreV1().Pods(namespace)
	ctx, cancel := context.WithTimeout(p.ctx, 2*time.Minute)
	defer cancel()
	var res pullResult
	note := func(w *corev1.ContainerStateWaiting) {
		s := w.Reason + ": " + w.Message
		if !slices.Contains(res.statuses, s) {
			res.statuses = append(res.statuses, s)
			info("pod %s: %v after it was created, its container waits with reason %s and message: %s", name, time.Since(start).Round(time.Millisecond), w.Reason, w.Message)
		}
	}
	last := "never read"
	failedAt := time.Time{}
	for {
		got, err := pods.Get(ctx, name, metav1.GetOptions{})
		if err == nil && len(got.Status.ContainerStatuses) > 0 {
			cs := got.Status.ContainerStatuses[0]
			st := cs.State
			switch {
			case st.Running != nil || st.Terminated != nil:
				res.took = time.Since(start)
				info("pod %s: its container started %v after the pod was created; imageID %s", name, res.took.Round(time.Millisecond), cs.ImageID)
				res.ran = true
				if o.logLines > 0 {
					res.logs = p.waitLogs(name, o.logLines)
				}
				return res
			case st.Waiting != nil && (st.Waiting.Reason == "ErrImagePull" || st.Waiting.Reason == "ImagePullBackOff"):
				note(st.Waiting)
				if failedAt.IsZero() {
					failedAt = time.Now()
					res.took = time.Since(start)
					res.detail = st.Waiting.Reason + ": " + st.Waiting.Message
				}
				if !o.backoff || st.Waiting.Reason == "ImagePullBackOff" || time.Since(failedAt) > 45*time.Second {
					if o.backoff {
						time.Sleep(time.Second)
						res.events = p.events(name)
					}
					return res
				}
			case st.Waiting != nil:
				last = "waiting: " + st.Waiting.Reason + " " + st.Waiting.Message
			}
		} else if err == nil {
			last = "phase " + string(got.Status.Phase)
		}
		select {
		case <-ctx.Done():
			res.err = fmt.Errorf("pod %s neither ran nor failed its pull within 2 minutes: %w; last seen: %s", name, ctx.Err(), last)
			return res
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// waitLogs returns the pod's log once it holds n lines, or what it holds after 30 seconds.
func (p *proof) waitLogs(name string, n int) string {
	deadline := time.Now().Add(30 * time.Second)
	var text string
	for time.Now().Before(deadline) {
		b, err := p.l.Client().CoreV1().Pods(namespace).GetLogs(name, &corev1.PodLogOptions{}).DoRaw(p.ctx)
		if err == nil {
			text = string(b)
			if strings.Count(text, "\n") >= n {
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return text
}

// events returns and prints the pod's events, oldest first, as
// TYPE REASON: MESSAGE.
func (p *proof) events(name string) []string {
	list, err := p.l.Client().CoreV1().Events(namespace).List(p.ctx, metav1.ListOptions{FieldSelector: "involvedObject.name=" + name})
	if err != nil {
		info("pod %s: listing its events: %v", name, err)
		return nil
	}
	items := list.Items
	slices.SortStableFunc(items, func(a, b corev1.Event) int {
		return a.CreationTimestamp.Compare(b.CreationTimestamp.Time)
	})
	var out []string
	for _, e := range items {
		s := fmt.Sprintf("%s %s: %s", e.Type, e.Reason, e.Message)
		out = append(out, s)
		info("pod %s: event (count %d): %s", name, e.Count, s)
	}
	return out
}

// crictlPull runs crictl pull ref in the worker, with --creds when c is not nil.
func (p *proof) crictlPull(ref string, c *cred) pullResult {
	args := []string{"crictl", "pull"}
	if c != nil {
		args = append(args, "--creds", c.pair())
	}
	start := time.Now()
	out, err := p.l.Exec(p.worker, nil, append(args, ref)...)
	took := time.Since(start)
	shownCreds := ""
	if c != nil {
		shownCreds = " --creds " + c.user + ":…"
	}
	info("crictl pull%s %s: exit %s after %v; its output:", shownCreds, ref, map[bool]string{true: "0", false: "non-zero"}[err == nil], took.Round(time.Millisecond))
	printLines("  ", strings.TrimSpace(out))
	if err != nil {
		return pullResult{detail: out, took: took}
	}
	return pullResult{ran: true, took: took}
}

// nodeRepository is the repository that a node names for the image: C
// holds its images under proxy-project/, which the server line's path
// supplies, so a node names them without it.
func (im *image) nodeRepository() string {
	if im.to == "c" {
		return strings.TrimPrefix(im.repository, "proxy-project/")
	}
	return im.repository
}

// ref is the reference of the test image v, named after host, by digest.
func (p *proof) ref(host, v string) string {
	im := p.img[v]
	return host + "/" + im.nodeRepository() + "@" + im.digest
}

// tagRef is the reference of the test image v, named after host, by its tag.
func (p *proof) tagRef(host, v string) string {
	im := p.img[v]
	return host + "/" + im.nodeRepository() + ":" + im.tag
}

func observed(err error) cond {
	return cond{false, "the case was observed (" + oneLine(err.Error()) + ")"}
}

func pullObserved(r pullResult) []cond {
	if r.err != nil {
		return []cond{{false, "the pull was observed (" + oneLine(r.err.Error()) + ")"}}
	}
	return nil
}

func users(reqs []request) []string {
	var us []string
	for _, r := range reqs {
		if !slices.Contains(us, r.user()) {
			us = append(us, r.user())
		}
	}
	return us
}

func statuses(reqs []request) []int {
	var ss []int
	for _, r := range reqs {
		if !slices.Contains(ss, r.status) {
			ss = append(ss, r.status)
		}
	}
	return ss
}

func anyReq(reqs []request, f func(request) bool) bool { return slices.ContainsFunc(reqs, f) }

func allReq(reqs []request, f func(request) bool) bool {
	return !slices.ContainsFunc(reqs, func(r request) bool { return !f(r) })
}

// sum prints one line for each watched stand-in in the case label.
func sum(label string, o outcome, r pullResult) {
	info("%s: the pull %s", label, r)
	for _, who := range []string{"m", "c", "run", "m2", "t"} {
		if reqs, ok := o.reqs[who]; ok {
			info("%s: %s had %d requests; users %v; statuses %v", label, who, len(reqs), users(reqs), statuses(reqs))
		}
	}
}

func bare(path string) string {
	p, _, _ := strings.Cut(path, "?")
	return p
}

// caseA: C serves the registry API under /v2/proxy-project/…; the server
// line holds that path, with and without override_path, and a pod pulls by
// digest and by tag.
func (p *proof) caseA() {
	server := "https://" + cproxyName + "/v2/proxy-project"
	da := p.img["a"].digest
	held := p.ref(hostA, "a")
	for _, x := range []struct {
		label     string
		override  bool
		byTag     bool
		path      string
		wantPull  bool
		wantState int
	}{
		{"a1", true, false, "/v2/proxy-project/team/app/manifests/" + da, true, 200},
		{"a2", true, true, "/v2/proxy-project/team/app/manifests/v1", true, 200},
		{"a3", false, false, "/v2/proxy-project/v2/team/app/manifests/" + da, false, 404},
		{"a4", false, true, "/v2/proxy-project/v2/team/app/manifests/v1", false, 404},
	} {
		if p.ctx.Err() != nil {
			return
		}
		ref := p.ref(hostA, "a")
		if x.byTag {
			ref = p.tagRef(hostA, "a")
		}
		var r pullResult
		o, err := p.observe(x.label, []hostsFile{p.formD(hostA, server, x.override)}, func() { r = p.podPull(x.label, ref, held, podOpts{}) })
		if err != nil {
			p.c.verdict(x.label, observed(err))
			continue
		}
		sum(x.label, o, r)
		pathText := strings.ReplaceAll(x.path, da, "A")
		conds := append(pullObserved(r),
			cond{r.ran == x.wantPull, fmt.Sprintf("the pod's pull %s (%s)", map[bool]string{true: "succeeded", false: "failed"}[x.wantPull], r)},
			cond{anyReq(o.reqs["c"], func(q request) bool { return bare(q.path) == x.path && q.status == x.wantState }),
				fmt.Sprintf("C answered %s with %d", pathText, x.wantState)},
		)
		if x.byTag {
			conds = append(conds, cond{!anyReq(o.reqs["run"], func(q request) bool { return strings.HasSuffix(bare(q.path), "/manifests/v1") }),
				"the run's registry was not asked for the tag"})
		}
		p.c.verdict(x.label, conds...)
	}
}

// caseB: a tag through form D with a prefix mirror that needs H's
// credentials from the kubelet's file.
func (p *proof) caseB() {
	server := "https://" + mproxyName + mirrorPrefix
	ref := p.tagRef(hostB, "b")
	var r pullResult
	o, err := p.observe("b", []hostsFile{p.formD(hostB, server, false)}, func() { r = p.podPull("b", ref, p.ref(hostB, "b"), podOpts{}) })
	if err != nil {
		p.c.verdict("b", observed(err))
		return
	}
	sum("b", o, r)
	p.bPulled = r.ran && r.err == nil
	tagPath := mirrorPrefix + "/v2/team/app/manifests/v1"
	p.c.verdict("b", append(pullObserved(r),
		cond{r.ran, "the pod pulled the image (" + r.String() + ")"},
		cond{!anyReq(o.reqs["run"], func(q request) bool { return strings.HasSuffix(bare(q.path), "/manifests/v1") }), "the run's registry was not asked for the tag"},
		cond{anyReq(o.reqs["m"], func(q request) bool {
			return bare(q.path) == tagPath && q.status == 200 && q.user() == aCred.user
		}), "M answered " + tagPath + " with 200 to the user auser"},
		cond{anyReq(o.reqs["m"], func(q request) bool { return strings.Contains(q.path, "/blobs/") && q.status == 200 }), "M served a blob"},
	)...)
}

// hostsLine returns the lines of an /etc/hosts text that name n as a whole field.
func hostsLines(text, n string) []string {
	var out []string
	for line := range strings.Lines(text) {
		if slices.Contains(strings.Fields(line), n) {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return out
}

// caseC: the mirror named by its container name, with nothing written into
// the nodes' /etc/hosts by the program.
func (p *proof) caseC() {
	var conds []cond
	for _, node := range p.l.Nodes() {
		out, err := p.l.Exec(node, nil, "cat", "/etc/hosts")
		if err != nil {
			p.c.verdict("c", observed(err))
			return
		}
		if node == p.worker {
			info("c: /etc/hosts in the worker after the stand-ins started:")
			printLines("c:   ", out)
		}
		for _, w := range []lab.Container{p.mproxy, p.cproxy} {
			before := hostsLines(p.hostsAtUp[node], w.Name)
			after := hostsLines(out, w.Name)
			inBlock := false
			if i, j := strings.Index(out, "# <nerdctl>"), strings.Index(out, "# </nerdctl>"); i >= 0 && j > i {
				inBlock = len(hostsLines(out[i:j], w.Name)) > 0
			}
			conds = append(conds,
				cond{len(before) == 0, fmt.Sprintf("%s's /etc/hosts named no %s right after Up", node, w.Name)},
				cond{inBlock && slices.ContainsFunc(after, func(l string) bool {
					return strings.HasPrefix(l, w.Address+" ") || strings.HasPrefix(l, w.Address+"\t")
				}),
					fmt.Sprintf("%s's /etc/hosts now names %s with its address %s inside nerdctl's block", node, w.Name, w.Address)})
		}
	}
	for _, args := range [][]string{
		{"sh", "-c", "grep '^hosts:' /etc/nsswitch.conf"},
		{"cat", "/etc/resolv.conf"},
	} {
		out, err := p.l.Exec(p.worker, nil, args...)
		info("c: %s in the worker (%v):", strings.Join(args, " "), err == nil)
		printLines("c:   ", out)
	}
	for _, w := range []lab.Container{p.mproxy, p.cproxy} {
		out, err := p.l.Exec(p.worker, nil, "getent", "-s", "files", "ahosts", w.Name)
		info("c: getent -s files ahosts %s in the worker (%v):", w.Name, err == nil)
		printLines("c:   ", out)
		conds = append(conds, cond{err == nil && strings.Contains(out, w.Address), "the worker resolves " + w.Name + " from files alone, to " + w.Address + " among its addresses"})
	}
	conds = append(conds, cond{p.bPulled, "case B's pull, which named M by its container name, succeeded"})
	p.c.verdict("c", conds...)
}

// engineSubnets returns the subnets of the engine's network kind.
func (p *proof) engineSubnets() (v4, v6 []string, err error) {
	out, err := exec.CommandContext(p.ctx, p.l.Engine(), "network", "inspect", "kind").Output()
	if err != nil {
		return nil, nil, fmt.Errorf("%s network inspect kind: %w", p.l.Engine(), err)
	}
	var nets []struct {
		IPAM struct{ Config []struct{ Subnet string } }
	}
	if err := json.Unmarshal(out, &nets); err != nil || len(nets) != 1 {
		return nil, nil, fmt.Errorf("reading %s network inspect kind: %v\n%s", p.l.Engine(), err, out)
	}
	for _, c := range nets[0].IPAM.Config {
		ip, _, err := net.ParseCIDR(c.Subnet)
		if err != nil {
			return nil, nil, err
		}
		if ip.To4() != nil {
			v4 = append(v4, c.Subnet)
		} else {
			v6 = append(v6, c.Subnet)
		}
	}
	if len(v4) == 0 {
		return nil, nil, fmt.Errorf("the network kind has no IPv4 subnet: %s", out)
	}
	return v4, v6, nil
}

// cutScript is the shell script that cuts a node's route: in the chain
// ROLLPROOF-CUT, which OUTPUT and FORWARD jump to first, a packet to any
// address outside the allowed ones is rejected, unless it belongs to a
// connection that already exists. It uses iptables-nft: the node's iptables
// is set to the legacy backend, whose filter table a rootless node cannot
// make (run 1 of this proof).
func cutScript(v4, v6 []string) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	w("set -e")
	for _, fam := range []struct {
		cmd   string
		allow []string
		icmp  string
	}{
		{"iptables-nft", append(append([]string{}, v4...), podSubnet, serviceSubnet), "icmp-port-unreachable"},
		{"ip6tables-nft", append(append([]string{}, v6...), "fe80::/10", "ff00::/8"), "icmp6-port-unreachable"},
	} {
		w("%s -N %s", fam.cmd, cutChain)
		w("%s -A %s -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN", fam.cmd, cutChain)
		w("%s -A %s -o lo -j RETURN", fam.cmd, cutChain)
		for _, a := range fam.allow {
			w("%s -A %s -d %s -j RETURN", fam.cmd, cutChain, a)
		}
		w("%s -A %s -p tcp -j REJECT --reject-with tcp-reset", fam.cmd, cutChain)
		w("%s -A %s -j REJECT --reject-with %s", fam.cmd, cutChain, fam.icmp)
		w("%s -I OUTPUT 1 -j %s", fam.cmd, cutChain)
		w("%s -I FORWARD 1 -j %s", fam.cmd, cutChain)
	}
	return b.String()
}

// rejected returns the packets counted by the REJECT rules of the chain in
// node, for iptables and ip6tables together, and prints the chain.
func (p *proof) rejected(node string) (int, error) {
	total := 0
	for _, cmd := range []string{"iptables-nft", "ip6tables-nft"} {
		out, err := p.l.Exec(node, nil, cmd, "-L", cutChain, "-n", "-v", "-x")
		if err != nil {
			return 0, err
		}
		info("d: %s -L %s -n -v -x in %s:", cmd, cutChain, node)
		printLines("d:   ", out)
		for line := range strings.Lines(out) {
			f := strings.Fields(line)
			if len(f) > 2 && f[2] == "REJECT" {
				n, _ := strconv.Atoi(f[0])
				total += n
			}
		}
	}
	return total, nil
}

// caseD cuts the nodes' route to every address outside the engine network's
// subnets and the cluster's pod and service subnets, and shows that a public
// pull fails and a pull from M works. It reports whether the cut held, which
// E to H need.
func (p *proof) caseD() bool {
	var conds []cond
	finish := func() bool {
		p.c.verdict("d", conds...)
		for _, x := range conds {
			if !x.held && strings.HasPrefix(x.text, "gate: ") {
				return false
			}
		}
		return true
	}
	// Before the cut: the small public image, with no credentials.
	before := p.crictlPull(publicRef, nil)
	conds = append(conds, cond{before.ran, fmt.Sprintf("before the cut, crictl pull of the public image succeeded (%s, %v)", before, before.took.Round(time.Millisecond))})
	p.rmi(publicRef)

	v4, v6, err := p.engineSubnets()
	if err != nil {
		conds = append(conds, cond{false, "gate: the engine network's subnets were read (" + oneLine(err.Error()) + ")"})
		return finish()
	}
	info("d: the network kind has the subnets %s; the cluster's pod subnet is %s and its service subnet %s", strings.Join(append(append([]string{}, v4...), v6...), ", "), podSubnet, serviceSubnet)
	script := cutScript(v4, v6)
	info("d: the script run in every node:")
	printLines("d:   ", script)
	for _, node := range p.l.Nodes() {
		if _, err := p.l.Exec(node, strings.NewReader(script), "sh", "-s"); err != nil {
			conds = append(conds, cond{false, "gate: the cut was applied in " + node + " (" + oneLine(err.Error()) + ")"})
			return finish()
		}
	}
	conds = append(conds, cond{true, "gate: the cut was applied in every node"})
	for _, cmd := range []string{"iptables-nft", "ip6tables-nft"} {
		for _, chain := range []string{"OUTPUT", "FORWARD"} {
			out, err := p.l.Exec(p.worker, nil, cmd, "-S", chain)
			info("d: %s -S %s in the worker (%v):", cmd, chain, err == nil)
			printLines("d:   ", out)
		}
	}

	after := p.crictlPull(publicRef, nil)
	conds = append(conds, cond{!after.ran && after.took < 15*time.Second,
		fmt.Sprintf("gate: after the cut, crictl pull of the public image failed within 15 seconds (%s, %v)", after, after.took.Round(time.Millisecond))})
	if after.ran {
		return finish()
	}

	// A pull from M still works.
	var mr pullResult
	o, err := p.observe("d", nil, func() { mr = p.crictlPull(p.ref(hostB, "d"), &aCred) })
	if err != nil {
		conds = append(conds, cond{false, "gate: the pull from M was observed (" + oneLine(err.Error()) + ")"})
		return finish()
	}
	sum("d", o, mr)
	mpath := mirrorPrefix + "/v2/team/d/manifests/" + p.img["d"].digest
	conds = append(conds, cond{mr.ran && anyReq(o.reqs["m"], func(q request) bool { return bare(q.path) == mpath && q.status == 200 }),
		"gate: after the cut, crictl pull --creds of " + hostB + "/team/d@D succeeded and M answered " + mirrorPrefix + "/v2/team/d/manifests/D with 200"})

	// Case C, after the cut: M still resolves, from files; DNS alone does not.
	hostsOut, herr := p.l.Exec(p.worker, nil, "getent", "hosts", mproxyName)
	info("c, after the cut: getent hosts %s in the worker (%v):", mproxyName, herr == nil)
	printLines("c:   ", hostsOut)
	dnsOut, derr := p.l.Exec(p.worker, nil, "getent", "-s", "dns", "hosts", mproxyName)
	info("c, after the cut: getent -s dns hosts %s in the worker (exit zero: %v):", mproxyName, derr == nil)
	printLines("c:   ", dnsOut)
	conds = append(conds, cond{herr == nil && derr != nil, "after the cut, the worker resolves M by getent hosts and not by DNS alone"})

	// The cut for traffic the node forwards: a pod dials three addresses.
	resolv, _ := p.l.Exec(p.worker, nil, "cat", "/etc/resolv.conf")
	ns := ""
	for line := range strings.Lines(resolv) {
		if f := strings.Fields(line); len(f) == 2 && f[0] == "nameserver" && ns == "" {
			ns = f[1]
		}
	}
	targets := []string{net.JoinHostPort(ns, "53"), "registry-1.docker.io:443", net.JoinHostPort(p.cproxy.Address, "443")}
	info("d: the pod d-dial dials %s: the worker's first name server, a public name through the cluster's DNS, and C's address", strings.Join(targets, ", "))
	var dr pullResult
	do, err := p.observe("d-dial", []hostsFile{p.formD(hostA, "https://"+cproxyName+"/v2/proxy-project", true)}, func() {
		dr = p.podPull("d-dial", p.ref(hostA, "dial"), p.ref(hostA, "dial"), podOpts{env: []corev1.EnvVar{{Name: "DIAL", Value: strings.Join(targets, ",")}}, logLines: 1 + len(targets)})
	})
	if err != nil {
		conds = append(conds, cond{false, "the dialing pod was observed (" + oneLine(err.Error()) + ")"})
	} else {
		sum("d-dial", do, dr)
		info("d-dial: the pod's log:")
		printLines("d-dial:   ", dr.logs)
		dialLines := 0
		for line := range strings.Lines(dr.logs) {
			if strings.HasPrefix(line, "dial ") {
				dialLines++
			}
		}
		conds = append(conds, cond{dr.ran && dialLines == len(targets), "the dialing pod ran and logged one line for each address"})
	}

	total := 0
	for _, node := range p.l.Nodes() {
		n, err := p.rejected(node)
		if err != nil {
			conds = append(conds, cond{false, "the chain's counters were read in " + node + " (" + oneLine(err.Error()) + ")"})
			continue
		}
		total += n
	}
	conds = append(conds, cond{total > 0, fmt.Sprintf("the REJECT rules counted %d packets in all", total)})
	return finish()
}

// setKubeletKeys writes the kubelet's file with entries, in place, checks
// what the worker reads at kubeletConfig, and restarts the worker's kubelet,
// whose cache of the file then starts empty.
func (p *proof) setKubeletKeys(entries []entry) error {
	b, err := kubeletJSON(entries)
	if err != nil {
		return err
	}
	if err := os.WriteFile(p.kubeletFile, b, 0o644); err != nil {
		return err
	}
	out, err := p.l.Exec(p.worker, nil, "cat", kubeletConfig)
	if err != nil {
		return err
	}
	if out != string(b) {
		return fmt.Errorf("the worker reads at %s something other than what was written:\n%s", kubeletConfig, out)
	}
	if _, err := p.l.Exec(p.worker, nil, "systemctl", "restart", "kubelet"); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(p.ctx, 2*time.Minute)
	defer cancel()
	for {
		out, err := p.l.Exec(p.worker, nil, "curl", "-fsS", "http://127.0.0.1:10248/healthz")
		if err == nil && strings.TrimSpace(out) == "ok" {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the worker's kubelet did not answer ok on its healthz within 2 minutes of its restart: %v", err)
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
			return fmt.Errorf("the worker was not Ready within 2 minutes of the kubelet's restart (last: %v)", err)
		case <-time.After(500 * time.Millisecond):
		}
	}
	info("the worker reads %s with: %s; its kubelet restarted and is Ready", kubeletConfig, keysOf(entries))
	return nil
}

// folderOf names the settings folder that a request to M came through, by its prefix.
func folderOf(path string) string {
	switch {
	case strings.HasPrefix(path, folderDocker+"/"):
		return "docker.io"
	case strings.HasPrefix(path, folderIndex+"/"):
		return "index.docker.io"
	case strings.HasPrefix(path, mirrorPrefix+"/"):
		return "(" + mirrorPrefix + ")"
	}
	return "(none)"
}

// caseE: Docker Hub. A settings file for docker.io and another for
// index.docker.io, each naming M under its own prefix; for each key the
// kubelet's file may use, alone, a pod pulls each name for the image.
func (p *proof) caseE(caPEM []byte) {
	for _, node := range p.l.Nodes() {
		for _, h := range []string{"docker.io", "index.docker.io"} {
			if err := p.writeCA(node, h, caPEM); err != nil {
				p.c.verdict("e", observed(err))
				return
			}
		}
	}
	files := []hostsFile{
		p.formD("docker.io", "https://"+mproxyName+folderDocker, false),
		p.formD("index.docker.io", "https://"+mproxyName+folderIndex, false),
	}
	for _, f := range files {
		if err := p.writeHosts(f); err != nil {
			p.c.verdict("e", observed(err))
			return
		}
		info("e: %s/%s/hosts.toml, written in every node:", certsRoot, f.host)
		printLines("e:   ", f.text)
	}
	d := p.img["e"].digest
	held := "docker.io/library/app@" + d
	var conds []cond
	var table []string
	for ki, k := range dockerKeys {
		if p.ctx.Err() != nil {
			return
		}
		if err := p.setKubeletKeys(append(append([]entry{}, baseAuths...), k)); err != nil {
			conds = append(conds, cond{false, "the kubelet's file with the key " + k.key + " was read after a restart (" + oneLine(err.Error()) + ")"})
			continue
		}
		for ni, n := range dockerNames(d) {
			if p.ctx.Err() != nil {
				return
			}
			label := fmt.Sprintf("e-k%d-n%d", ki+1, ni+1)
			info("%s: key %s (%s), name %s", label, k.key, k.c.user, n)
			var r pullResult
			o, err := p.observe(label, nil, func() { r = p.podPull(label, n, held, podOpts{}) })
			if err != nil {
				conds = append(conds, cond{false, label + " was observed (" + oneLine(err.Error()) + ")"})
				continue
			}
			sum(label, o, r)
			conds = append(conds, pullObserved(r)...)
			var folders []string
			for _, q := range o.reqs["m"] {
				if f := folderOf(q.path); !slices.Contains(folders, f) {
					folders = append(folders, f)
				}
			}
			var okUsers []string
			for _, q := range o.reqs["m"] {
				if strings.Contains(q.path, "/manifests/") && q.status == 200 && !slices.Contains(okUsers, q.user()) {
					okUsers = append(okUsers, q.user())
				}
			}
			row := fmt.Sprintf("key %s | name %s | %s | M's users %v, manifest answered 200 to %v | folders %v | run %d requests", k.key, n, map[bool]string{true: "pulled", false: "failed"}[r.ran], users(o.reqs["m"]), okUsers, folders, len(o.reqs["run"]))
			table = append(table, row)
			info("%s: %s", label, row)
		}
	}
	if err := p.setKubeletKeys(baseAuths); err != nil {
		conds = append(conds, cond{false, "the kubelet's file was set back to its base entries (" + oneLine(err.Error()) + ")"})
	}
	info("e: the table:")
	for _, row := range table {
		info("e:   %s", row)
	}
	conds = append(conds, cond{len(table) == len(dockerKeys)*4, fmt.Sprintf("%d of %d pulls ran and were observed", len(table), len(dockerKeys)*4)})
	p.c.verdict("e", conds...)
}

// caseF: no kubelet entry for H; the pod carries a pull secret with M's
// credentials for H.
func (p *proof) caseF() {
	cfg, err := json.Marshal(map[string]any{"auths": map[string]any{hostF: map[string]string{"auth": fCred.basic()}}})
	if err == nil {
		_, err = p.l.Client().CoreV1().Secrets(namespace).Create(p.ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "pullf", Namespace: namespace},
			Type:       corev1.SecretTypeDockerConfigJson,
			Data:       map[string][]byte{corev1.DockerConfigJsonKey: cfg},
		}, metav1.CreateOptions{})
	}
	if err != nil {
		p.c.verdict("f", observed(err))
		return
	}
	info("f: the Secret pullf, of type %s, holds an auths entry for %s with the user %s; the kubelet's file has no entry for %s", corev1.SecretTypeDockerConfigJson, hostF, fCred.user, hostF)
	var r pullResult
	o, err := p.observe("f", []hostsFile{p.formD(hostF, "https://"+mproxyName+mirrorPrefix, false)}, func() {
		r = p.podPull("f", p.ref(hostF, "f"), p.ref(hostF, "f"), podOpts{secret: "pullf"})
	})
	if err != nil {
		p.c.verdict("f", observed(err))
		return
	}
	sum("f", o, r)
	mpath := mirrorPrefix + "/v2/team/f/manifests/" + p.img["f"].digest
	p.c.verdict("f", append(pullObserved(r),
		cond{r.ran, "the pod pulled the image (" + r.String() + ")"},
		cond{anyReq(o.reqs["m"], func(q request) bool { return bare(q.path) == mpath && q.status == 200 && q.user() == fCred.user }), "M answered " + mirrorPrefix + "/v2/team/f/manifests/F with 200 to the user fuser"},
		cond{allReq(o.reqs["m"], func(q request) bool { return q.user() == "-" || q.user() == fCred.user }), "M saw no user other than fuser"},
		cond{len(o.reqs["run"]) > 0 && allReq(o.reqs["run"], func(q request) bool { return q.kv["auth"] == "-" }), "the run's registry was asked and no request to it carried credentials"},
		cond{len(o.reqs["c"]) == 0, "C had no request"},
	)...)
}

// caseG: what a failed pull says, with form D: the image missing from M, by
// digest (g1) and by tag (g1t); then M's container stopped (g2, g2t).
func (p *proof) caseG() {
	server := "https://" + mproxyName + mirrorPrefix
	f := p.formD(hostG, server, false)
	one := func(label, ref, held string) {
		var r pullResult
		o, err := p.observe(label, []hostsFile{f}, func() { r = p.podPull(label, ref, held, podOpts{backoff: true}) })
		if err != nil {
			p.c.verdict(label, observed(err))
			return
		}
		sum(label, o, r)
		names := func(s string) string {
			var n []string
			if strings.Contains(s, mproxyName) {
				n = append(n, "M")
			}
			if strings.Contains(s, hostG) {
				n = append(n, "H")
			}
			if strings.Contains(s, p.runproxy.Address) {
				n = append(n, "the run's registry")
			}
			if len(n) == 0 {
				return "none of M, H and the run's registry"
			}
			return strings.Join(n, " and ")
		}
		for _, s := range r.statuses {
			info("%s: the status %q names %s", label, s, names(s))
		}
		for _, e := range r.events {
			info("%s: the event %q names %s", label, e, names(e))
		}
		p.c.verdict(label, append(pullObserved(r),
			cond{!r.ran, "the pod's pull failed (" + r.String() + ")"},
			cond{len(r.events) > 0, fmt.Sprintf("the pod had %d events", len(r.events))},
		)...)
	}
	one("g1", hostG+"/team/g@"+zeroDigest, hostG+"/team/g@"+zeroDigest)
	if p.ctx.Err() != nil {
		return
	}
	one("g1t", hostG+"/team/missing:v1", hostG+"/team/missing:v1")
	if p.ctx.Err() != nil {
		return
	}
	if out, err := exec.CommandContext(p.ctx, p.l.Engine(), "stop", p.mproxy.Name).CombinedOutput(); err != nil {
		p.c.verdict("g2", observed(fmt.Errorf("%s stop %s: %v\n%s", p.l.Engine(), p.mproxy.Name, err, out)))
		p.c.verdict("g2t", observed(fmt.Errorf("M was not stopped")))
		return
	}
	info("g: %s stop %s returned; the worker's /etc/hosts lines that name it now: %v", p.l.Engine(), p.mproxy.Name, func() []string {
		out, _ := p.l.Exec(p.worker, nil, "cat", "/etc/hosts")
		return hostsLines(out, mproxyName)
	}())
	one("g2", p.ref(hostG, "g"), p.ref(hostG, "g"))
	if p.ctx.Err() != nil {
		return
	}
	one("g2t", p.tagRef(hostG, "g"), p.ref(hostG, "g"))
}

// caseH: a mirror M2 (registry:3 with token authentication, behind a logging
// proxy over TLS) whose challenge names T, a token service on another host.
// The kubelet's file has credentials for H.
func (p *proof) caseH() {
	notRun := func(format string, args ...any) {
		p.c.verdict("h", cond{false, "case H ran: " + fmt.Sprintf(format, args...)})
	}
	// M2's storage is a folder of the runner, filled through an open registry
	// before M2 starts on it.
	fillPort, err := freePort()
	if err != nil {
		notRun("%v", err)
		return
	}
	fill, err := p.l.Container(p.ctx, "m2fill", "-p", "127.0.0.1:"+fillPort+":5000", "-v", p.dirs["m2data"]+":/var/lib/registry", registryImage)
	if err != nil {
		notRun("%v", err)
		return
	}
	if err := waitStatus(p.ctx, http.DefaultClient, "http://127.0.0.1:"+fillPort+"/v2/", 200, nil); err != nil {
		notRun("m2fill: %v", err)
		return
	}
	im := p.img["h"]
	if err := pushImage(p.ctx, "127.0.0.1:"+fillPort, im, filepath.Join(p.dirs["app"], im.v)); err != nil {
		notRun("pushing h: %v", err)
		return
	}
	info("h = %s, in M2's storage as %s, pushed through %s", im.digest, im.repository, fill.Name)
	m2, err := p.l.Container(p.ctx, "m2", "-v", p.dirs["m2data"]+":/var/lib/registry", "-v", p.dirs["certs"]+":/certs:ro",
		"-e", "REGISTRY_AUTH=token",
		"-e", "REGISTRY_AUTH_TOKEN_REALM=https://"+tokName+"/token",
		"-e", "REGISTRY_AUTH_TOKEN_SERVICE="+m2Service,
		"-e", "REGISTRY_AUTH_TOKEN_ISSUER="+tokIssuer,
		"-e", "REGISTRY_AUTH_TOKEN_ROOTCERTBUNDLE=/certs/ca.pem",
		registryImage)
	if err != nil {
		notRun("%v", err)
		return
	}
	m2port, err := freePort()
	if err != nil {
		notRun("%v", err)
		return
	}
	tokPort, err := freePort()
	if err != nil {
		notRun("%v", err)
		return
	}
	pargs := append([]string{"-p", "127.0.0.1:" + m2port + ":443"}, p.standinArgs()...)
	pargs = append(append(pargs, "proxy", "-name", "m2", "-backend", "http://"+m2.Address+":5000"), tlsFlags...)
	if p.m2proxy, err = p.l.Container(p.ctx, "m2proxy", pargs...); err != nil {
		notRun("%v", err)
		return
	}
	var tus []string
	for _, u := range tUsers {
		tus = append(tus, u.pair())
	}
	targs := append([]string{"-p", "127.0.0.1:" + tokPort + ":443"}, p.standinArgs()...)
	targs = append(append(targs, "token", "-name", "t", "-sign-cert", "/certs/signer.pem", "-sign-key", "/certs/signer-key.pem",
		"-issuer", tokIssuer, "-service", m2Service, "-users", strings.Join(tus, ",")), tlsFlags...)
	if p.tok, err = p.l.Container(p.ctx, "tok", targs...); err != nil {
		notRun("%v", err)
		return
	}
	if p.m2proxy.Name != m2proxyName || p.tok.Name != tokName {
		notRun("M2's proxy and T are named %s and %s, want %s and %s", p.m2proxy.Name, p.tok.Name, m2proxyName, tokName)
		return
	}
	info("m2: %s at %s; m2proxy: %s at %s; tok: %s at %s", m2.Name, m2.Address, p.m2proxy.Name, p.m2proxy.Address, p.tok.Name, p.tok.Address)

	// From the runner: M2 challenges, T gives a token for huser, and M2
	// accepts it.
	if err := waitStatus(p.ctx, p.tlsClient, "https://127.0.0.1:"+m2port+"/v2/", 401, nil); err != nil {
		notRun("M2 through its proxy: %v", err)
		return
	}
	req, _ := http.NewRequestWithContext(p.ctx, http.MethodGet, "https://127.0.0.1:"+m2port+"/v2/team/h/manifests/"+im.digest, nil)
	resp, err := p.tlsClient.Do(req)
	if err != nil {
		notRun("M2's challenge: %v", err)
		return
	}
	resp.Body.Close()
	info("h: from the runner, GET /v2/team/h/manifests/H at M2 with no credentials: %d, WWW-Authenticate: %s", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	if err := waitStatus(p.ctx, p.tlsClient, "https://127.0.0.1:"+tokPort+"/token?service="+m2Service, 200, &hCred); err != nil {
		notRun("T: %v", err)
		return
	}
	q := url.Values{"service": {m2Service}, "scope": {"repository:team/h:pull"}}
	req, _ = http.NewRequestWithContext(p.ctx, http.MethodGet, "https://127.0.0.1:"+tokPort+"/token?"+q.Encode(), nil)
	req.SetBasicAuth(hCred.user, hCred.pass)
	resp, err = p.tlsClient.Do(req)
	if err != nil {
		notRun("T: %v", err)
		return
	}
	var tr struct {
		Token string `json:"token"`
	}
	err = json.NewDecoder(resp.Body).Decode(&tr)
	resp.Body.Close()
	if err != nil || tr.Token == "" {
		notRun("T gave no token: %v", err)
		return
	}
	req, _ = http.NewRequestWithContext(p.ctx, http.MethodGet, "https://127.0.0.1:"+m2port+"/v2/team/h/manifests/"+im.digest, nil)
	req.Header.Set("Authorization", "Bearer "+tr.Token)
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json")
	resp, err = p.tlsClient.Do(req)
	if err != nil {
		notRun("M2 with T's token: %v", err)
		return
	}
	resp.Body.Close()
	info("h: from the runner, T's token for huser, then GET /v2/team/h/manifests/H at M2 with it: %d", resp.StatusCode)
	if resp.StatusCode != 200 {
		logs, _ := m2.Logs()
		info("h: M2's own log, its last lines:")
		lines := strings.Split(strings.TrimSpace(logs), "\n")
		printLines("h:   ", strings.Join(lines[max(0, len(lines)-15):], "\n"))
		notRun("M2 refused T's token from the runner with %d", resp.StatusCode)
		return
	}

	var r pullResult
	o, err := p.observe("h", []hostsFile{p.formD(hostH, "https://"+m2proxyName, false)}, func() { r = p.podPull("h", p.ref(hostH, "h"), p.ref(hostH, "h"), podOpts{}) })
	if err != nil {
		p.c.verdict("h", observed(err))
		return
	}
	sum("h", o, r)
	mpath := "/v2/team/h/manifests/" + im.digest
	p.c.verdict("h", append(pullObserved(r),
		cond{r.ran, "the pod pulled the image (" + r.String() + ")"},
		cond{anyReq(o.reqs["t"], func(q request) bool { return q.user() == hCred.user && q.status == 200 }), "T gave a token to the user huser"},
		cond{anyReq(o.reqs["m2"], func(q request) bool { return q.kv["auth"] == "-" && q.status == 401 }), "M2 answered a request with no credentials with 401"},
		cond{anyReq(o.reqs["m2"], func(q request) bool {
			return bare(q.path) == mpath && q.kv["auth"] == "bearer" && q.kv["sub"] == hCred.user && q.status == 200
		}), "M2 answered /v2/team/h/manifests/H with 200 to a bearer token whose sub is huser"},
		cond{allReq(o.reqs["m2"], func(q request) bool { return q.kv["auth"] != "basic" }), "no request to M2 carried Basic credentials"},
	)...)
}

func printLines(lead, text string) {
	for line := range strings.Lines(strings.TrimRight(text, "\n")) {
		info("%s%s", lead, strings.TrimRight(line, "\n"))
	}
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(strings.TrimSpace(s), "\n", " | ")), " ")
}

// settledLogs returns the container's log once two reads 500 ms apart agree,
// or the last read after 5 seconds.
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
		fmt.Println("p02b: all checks passed")
		return 0
	}
	fmt.Printf("p02b: %d checks failed\n", c.failed)
	return 1
}
