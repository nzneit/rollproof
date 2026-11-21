// Command standin is a stand-in registry for the mirror job: an in-memory registry behind TLS, a
// Basic login and path prefixes, filled from image archives before it listens, writing a line for
// each connection and each request. It stands for an organization's mirror or for a real host, and
// is never released.
//
// Its flags are -listen ADDR (TLS), -cert FILE, -key FILE, -user NAME, -prefix PATH and
// -load REPOSITORY=ARCHIVE, each of the last two repeated, and -check URL. The password comes from
// STANDIN_PASSWORD, and an empty one is an error. With -check the program asks URL until any HTTP
// answer comes, for at most 30 s, and exits 0 or 1; otherwise it loads every archive into the
// registry, serves it on the TLS listener, and writes READY.
package main

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// realm is the challenge the stand-in answers a request without valid credentials with.
const realm = `Basic realm="rollproof-mirrorjob"`

// newHandler serves reg behind a Basic login; with prefixes, only a path equal to a
// prefix or that begins with a prefix and /, the prefix taken off, and 404 for any
// other. No or wrong credentials get 401 and WWW-Authenticate:
// Basic realm="rollproof-mirrorjob". After each answer it writes REQ METHOD
// PATH user=USER status=CODE, with the path as received and no query, and
// USER - when absent.
func newHandler(reg http.Handler, prefixes []string, user, password string, log io.Writer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPassword, hasLogin := r.BasicAuth()
		okUser := subtle.ConstantTimeCompare([]byte(gotUser), []byte(user)) == 1
		okPassword := subtle.ConstantTimeCompare([]byte(gotPassword), []byte(password)) == 1
		who := "-"
		if hasLogin {
			who = gotUser
		}
		path := r.URL.Path
		if !hasLogin || !okUser || !okPassword {
			w.Header().Set("WWW-Authenticate", realm)
			w.WriteHeader(http.StatusUnauthorized)
			writeRequestLine(log, r.Method, path, who, http.StatusUnauthorized)
			return
		}
		if len(prefixes) > 0 {
			stripped, ok := stripPrefix(path, prefixes)
			if !ok {
				http.NotFound(w, r)
				writeRequestLine(log, r.Method, path, who, http.StatusNotFound)
				return
			}
			r.URL.Path = stripped
			r.URL.RawPath = ""
		}
		rec := &statusWriter{ResponseWriter: w}
		reg.ServeHTTP(rec, r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		writeRequestLine(log, r.Method, path, who, status)
	})
}

// stripPrefix returns path with the first prefix that matches it taken off, and false when none
// does. A prefix matches only the whole path or a path that goes on with /; a path equal to a
// prefix leaves /.
func stripPrefix(path string, prefixes []string) (string, bool) {
	for _, prefix := range prefixes {
		if path == prefix {
			return "/", true
		}
		if strings.HasPrefix(path, prefix+"/") {
			return path[len(prefix):], true
		}
	}
	return "", false
}

// writeRequestLine writes one REQ line.
func writeRequestLine(log io.Writer, method, path, user string, status int) {
	fmt.Fprintf(log, "REQ %s %s user=%s status=%d\n", method, path, user, status)
}

// statusWriter is a ResponseWriter that remembers the status the handler answered with.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

// logConnections returns l, writing CONN ADDRESS for each connection it accepts, before any byte
// of it is read.
func logConnections(l net.Listener, log io.Writer) net.Listener {
	return &connListener{Listener: l, log: log}
}

// connListener writes a line for each connection its listener accepts.
type connListener struct {
	net.Listener
	mu  sync.Mutex
	log io.Writer
}

func (l *connListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	fmt.Fprintf(l.log, "CONN %s\n", c.RemoteAddr())
	l.mu.Unlock()
	return c, nil
}

// load is one image to put in place before listening: the archive's one image, under the
// repository.
type load struct {
	repository, archive string
}

// loadImages pushes each archive's one image to the plain-HTTP registry at registry (HOST:PORT)
// under its repository, by digest, writing LOAD REPO@DIGEST on standard output for each. An
// archive with no image, or more than one, is an error that names it.
func loadImages(ctx context.Context, registry string, loads []load) error {
	for _, ld := range loads {
		img, err := archiveImage(ld.archive)
		if err != nil {
			return err
		}
		digest, err := img.Digest()
		if err != nil {
			return fmt.Errorf("%s: %w", ld.archive, err)
		}
		ref, err := name.NewDigest(registry+"/"+ld.repository+"@"+digest.String(), name.Insecure)
		if err != nil {
			return fmt.Errorf("%s: %w", ld.archive, err)
		}
		if err := remote.Write(ref, img, remote.WithContext(ctx)); err != nil {
			return fmt.Errorf("%s: %w", ld.archive, err)
		}
		fmt.Fprintf(os.Stdout, "LOAD %s@%s\n", ld.repository, digest)
	}
	return nil
}

// archiveImage returns the one image of the archive at path, or an error that names the archive
// when it holds no image or more than one.
func archiveImage(path string) (v1.Image, error) {
	manifest, err := tarball.LoadManifest(func() (io.ReadCloser, error) { return os.Open(path) })
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(manifest) != 1 {
		return nil, fmt.Errorf("%s holds %d images, want 1", path, len(manifest))
	}
	img, err := tarball.ImageFromPath(path, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return img, nil
}

// check asks GET url every 200 ms, with no certificate check, until any HTTP answer comes, and
// returns nil then; an error when limit passes first.
func check(ctx context.Context, url string, limit time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	// The certificate is the job's own, signed by an authority this machine does not hold.
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}
	var last error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			return nil
		}
		last = err
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s did not answer within %s: %w", url, limit, last)
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// list is a flag.Value that collects each occurrence of a flag.
type list []string

func (l *list) String() string { return strings.Join(*l, ",") }

func (l *list) Set(value string) error {
	*l = append(*l, value)
	return nil
}

// loads turns the values of -load into the images to put in place.
func loads(values []string) ([]load, error) {
	out := make([]load, 0, len(values))
	for _, value := range values {
		repository, archive, ok := strings.Cut(value, "=")
		if !ok || repository == "" || archive == "" {
			return nil, fmt.Errorf("bad -load %q: want REPOSITORY=ARCHIVE", value)
		}
		out = append(out, load{repository: repository, archive: archive})
	}
	return out, nil
}

// syncWriter writes one piece at a time, so that the lines of the stand-in's requests and
// connections do not interleave.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

// fatal writes err to standard error and exits 1.
func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func main() {
	listenAddress := flag.String("listen", "", "`ADDR` to serve TLS on")
	certFile := flag.String("cert", "", "`FILE` holding the server certificate")
	keyFile := flag.String("key", "", "`FILE` holding the certificate's key")
	user := flag.String("user", "", "`NAME` of the Basic login")
	checkURL := flag.String("check", "", "`URL` to wait for, instead of serving")
	var prefixes, loadValues list
	flag.Var(&prefixes, "prefix", "`PATH` the registry serves under and strips; repeated")
	flag.Var(&loadValues, "load", "`REPOSITORY=ARCHIVE` to put in place before listening; repeated")
	flag.Parse()

	if *checkURL != "" {
		if err := check(context.Background(), *checkURL, 30*time.Second); err != nil {
			fatal(err)
		}
		return
	}

	password := os.Getenv("STANDIN_PASSWORD")
	if password == "" {
		fatal(fmt.Errorf("STANDIN_PASSWORD is empty"))
	}
	ls, err := loads(loadValues)
	if err != nil {
		fatal(err)
	}

	out := &syncWriter{w: os.Stdout}
	reg := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	plain, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatal(err)
	}
	go func() { _ = http.Serve(plain, reg) }()
	if err := loadImages(context.Background(), plain.Addr().String(), ls); err != nil {
		fatal(err)
	}

	cert, err := tls.LoadX509KeyPair(*certFile, *keyFile)
	if err != nil {
		fatal(err)
	}
	ln, err := tls.Listen("tcp", *listenAddress, &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		fatal(err)
	}
	fmt.Fprintln(out, "READY")
	if err := http.Serve(logConnections(ln, out), newHandler(reg, prefixes, *user, password, out)); err != nil {
		fatal(err)
	}
}
