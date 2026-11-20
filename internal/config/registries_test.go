package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"rollproof/internal/verdict"
)

// rejectedQuiet fails the test unless the rejection names path and does not quote the planted
// value in either of its cases, and unless its text holds problem.
func rejectedQuiet(t *testing.T, err error, path, problem string) {
	t.Helper()
	rejected(t, err, path)
	var c verdict.Condition
	if !errors.As(err, &c) {
		t.Fatalf("got error %v, want a verdict.Condition", err)
	}
	if !strings.Contains(c.Text, problem) {
		t.Errorf("text %q does not hold %q", c.Text, problem)
	}
	for _, value := range []string{planted, strings.ToLower(planted)} {
		if strings.Contains(c.Text, value) {
			t.Errorf("text %q quotes a value", c.Text)
		}
	}
}

// newCertificate returns a self-signed ECDSA P-256 certificate and its DER bytes.
func newCertificate(t *testing.T) (*x509.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "rollproof test authority"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert, der
}

// writeCA writes at path a self-signed ECDSA P-256 certificate in PEM form, mode 0644. Nothing
// checks its dates.
func writeCA(t *testing.T, path string) {
	t.Helper()
	_, der := newCertificate(t)
	writeFile(t, path, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})))
}

// writeKeyPEM writes at path a PEM EC PRIVATE KEY block alone, mode 0644.
func writeKeyPEM(t *testing.T, path string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})))
}

// writeDER writes at path one certificate's DER bytes alone, mode 0644.
func writeDER(t *testing.T, path string) {
	t.Helper()
	_, der := newCertificate(t)
	if err := os.WriteFile(path, der, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRegistriesRejects(t *testing.T) {
	entry := func(lines string) string { return variant(t, base, "deploy:\n", "registries:\n"+lines+"deploy:\n") }
	as := func(v string) string {
		return variant(t, base, "deploy:\n", "images:\n  orders:\n    local: orders:candidate\n    as: \""+v+"\"\ndeploy:\n")
	}
	const own = "names the entry's own host"
	const tool = "rollproof.test is the tool's own host inside the run"
	const hub = "write docker.io for Docker Hub"
	const noHost = "a name with neither a dot nor a port is not a registry host"
	for _, tc := range []struct{ name, text, path, problem string }{
		{"a host with a scheme", entry("  - host: \"https://s3cret-value.example\"\n"), "registries[0].host", "must not carry a scheme"},
		{"a host with a path", entry("  - host: \"registry.example/s3cret-value\"\n"), "registries[0].host", "must not carry a path"},
		{"a host with a user", entry("  - host: \"s3cret-value@registry.example\"\n"), "registries[0].host", "must not carry a user name or a password"},
		{"a host in upper case", entry("  - host: \"Registry.S3CRET-VALUE.example\"\n"), "registries[0].host", "must be lower case"},
		{"a host with neither a dot nor a port", entry("  - host: \"s3cret-value\"\n"), "registries[0].host", noHost},
		{"an IPv6 host without brackets", entry("  - host: \"fd00::5\"\n"), "registries[0].host", "an IPv6 address must be written in brackets"},
		{"a port out of range", entry("  - host: \"s3cret-value.example:99999\"\n"), "registries[0].host", "the port must be 1 to 65535"},
		{"an empty label", entry("  - host: \"s3cret-value..example\"\n"), "registries[0].host", "is not a registry host as an image's name writes it"},
		{"a dot at the end", entry("  - host: \"s3cret-value.example.\"\n"), "registries[0].host", "is not a registry host as an image's name writes it"},
		{"index.docker.io as a host", entry("  - host: \"index.docker.io\"\n    mirror: \"s3cret-value.example\"\n"), "registries[0].host", hub},
		{"registry-1.docker.io as a host", entry("  - host: \"registry-1.docker.io\"\n    mirror: \"s3cret-value.example\"\n"), "registries[0].host", hub},
		{"rollproof.test as a host", entry("  - host: \"rollproof.test\"\n    mirror: \"s3cret-value.example\"\n"), "registries[0].host", tool},
		{"a username with no password", entry("  - host: \"registry.example\"\n    username: s3cret-value\n"), "registries[0]", "username and password come together or not at all"},
		{"a password with no username", entry("  - host: \"registry.example\"\n    password: ${env.P}\n"), "registries[0]", "username and password come together or not at all"},
		{"a mirror with a scheme", entry("  - host: \"registry.example\"\n    mirror: \"https://mirror.example/s3cret-value\"\n"), "registries[0].mirror", "must not carry a scheme; HTTPS is implied"},
		{"a mirror with a user and a password", entry("  - host: \"registry.example\"\n    mirror: \"u:s3cret-value@mirror.example\"\n"), "registries[0].mirror", "must not carry a user name or a password"},
		{"a mirror with a query", entry("  - host: \"registry.example\"\n    mirror: \"mirror.example/x?s3cret-value\"\n"), "registries[0].mirror", "must not carry a query"},
		{"a mirror with a fragment", entry("  - host: \"registry.example\"\n    mirror: \"mirror.example/x#s3cret-value\"\n"), "registries[0].mirror", "must not carry a fragment"},
		{"a mirror's host in upper case", entry("  - host: \"registry.example\"\n    mirror: \"Mirror.example/s3cret-value\"\n"), "registries[0].mirror", "must be lower case"},
		{"a mirror's host with neither a dot nor a port", entry("  - host: \"registry.example\"\n    mirror: \"mirror/s3cret-value\"\n"), "registries[0].mirror", noHost},
		{"a mirror's path with a .. segment", entry("  - host: \"registry.example\"\n    mirror: \"mirror.example/a/../s3cret-value\"\n"), "registries[0].mirror", "the path must not have a . or .. segment"},
		{"a mirror's path with an escaped character", entry("  - host: \"registry.example\"\n    mirror: \"mirror.example/s3cret%2Dvalue\"\n"), "registries[0].mirror", "the path may hold only letters, digits, -, ., _ and ~"},
		{"a mirror's path with a / at its end", entry("  - host: \"registry.example\"\n    mirror: \"mirror.example/s3cret-value/\"\n"), "registries[0].mirror", "the path must have no empty segment and no / at its end"},
		{"a mirror's path with an empty segment", entry("  - host: \"registry.example\"\n    mirror: \"mirror.example//s3cret-value\"\n"), "registries[0].mirror", "the path must have no empty segment and no / at its end"},
		{"a mirror on its own host", entry("  - host: \"registry.example\"\n    mirror: \"registry.example/s3cret-value\"\n"), "registries[0].mirror", own},
		{"a mirror on its own host with :443", entry("  - host: \"registry.example\"\n    mirror: \"registry.example:443/s3cret-value\"\n"), "registries[0].mirror", own},
		{"a host with :443 and its mirror without", entry("  - host: \"registry.example:443\"\n    mirror: \"registry.example/s3cret-value\"\n"), "registries[0].mirror", own},
		{"a mirror on rollproof.test", entry("  - host: \"registry.example\"\n    mirror: \"rollproof.test/s3cret-value\"\n"), "registries[0].mirror", tool},
		{"an as with a tag", as("registry.example/team/orders:s3cret-value"), "images.orders.as", "must not carry a tag"},
		{"an as with a digest", as("registry.example/team/s3cret-value@sha256:" + strings.Repeat("a", 64)), "images.orders.as", "must not carry a digest"},
		{"an as with no host", as("team/s3cret-value"), "images.orders.as", noHost},
		{"an as with no path", as("s3cret-value.example"), "images.orders.as", "must name a repository with its host written out"},
		{"an as in upper case", as("registry.example/Team/S3CRET-VALUE"), "images.orders.as", "must be lower case"},
		{"an as whose path is not a repository's", as("registry.example/team/-s3cret-value"), "images.orders.as", "the path is not a repository path as an image's name writes it"},
		{"an as on docker.io with one segment", as("docker.io/s3cret-value"), "images.orders.as", "on docker.io the path needs two segments or more; write docker.io/library/NAME"},
		{"an as on index.docker.io", as("index.docker.io/library/s3cret-value"), "images.orders.as", hub},
		{"an as on registry-1.docker.io", as("registry-1.docker.io/library/s3cret-value"), "images.orders.as", hub},
		{"an as on rollproof.test", as("rollproof.test/s3cret-value"), "images.orders.as", tool},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadWith(t, t.TempDir(), tc.text, envOf(map[string]string{"P": planted}))
			rejectedQuiet(t, err, tc.path, tc.problem)
		})
	}
}

func TestCARejects(t *testing.T) {
	// For each case, a file or folder at certs/ca.pem, then the entry
	//   - host: "registry.example"
	//     ca: certs/ca.pem
	// rejected at registries[0].ca with the problem:
	//   nothing at the path                 -> "does not exist"
	//   a folder                            -> "is not a file the tool can read"
	//   an empty file                       -> "holds no certificate in PEM form"
	//   writeKeyPEM, a private key alone    -> "holds no certificate in PEM form"
	//   writeDER, a certificate in DER      -> "holds no certificate in PEM form"
	const entry = "registries:\n  - host: \"registry.example\"\n    ca: certs/ca.pem\ndeploy:\n"
	for _, tc := range []struct {
		name    string
		write   func(t *testing.T, path string)
		problem string
	}{
		{"nothing at the path", nil, "does not exist"},
		{"a folder", func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}, "is not a file the tool can read"},
		{"an empty file", func(t *testing.T, path string) { writeFile(t, path, "") }, "holds no certificate in PEM form"},
		{"a private key alone", writeKeyPEM, "holds no certificate in PEM form"},
		{"a certificate in DER", writeDER, "holds no certificate in PEM form"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, _ := project(t, "certs")
			if tc.write != nil {
				tc.write(t, filepath.Join(dir, "certs", "ca.pem"))
			}
			_, err := loadWith(t, dir, variant(t, base, "deploy:\n", entry), noEnv)
			rejectedQuiet(t, err, "registries[0].ca", tc.problem)
		})
	}
}

func TestRegistriesAccepts(t *testing.T) {
	// Each loads with no error; the planted CA is writeCA's, at certs/ca.pem.
	dir, _ := project(t, "certs")
	writeCA(t, filepath.Join(dir, "certs", "ca.pem"))
	load := func(t *testing.T, lines string) {
		t.Helper()
		_, err := loadWith(t, dir, variant(t, base, "deploy:\n", "registries:\n"+lines+"deploy:\n"), noEnv)
		if err != nil {
			t.Fatal(err)
		}
	}
	as := func(t *testing.T, value string) {
		t.Helper()
		_, err := loadWith(t, dir, variant(t, base, "deploy:\n",
			"images:\n  orders:\n    local: orders:candidate\n    as: \""+value+"\"\ndeploy:\n"), noEnv)
		if err != nil {
			t.Fatal(err)
		}
	}
	hosts := []string{"registry.example", "registry.example:5000", "10.1.2.3", "10.1.2.3:5000",
		"localhost", "localhost:5000", "[fd00::5]", "[fd00::5]:5000", "myregistry:5000", "docker.io",
		"index.docker.io:443", "rollproof.test:5000"} // the last two: decision 13
	mirrors := []string{"mirror.example", "mirror.example/registry-example",
		"cache.example/v2/proxy-project", "mirror.example:8443/A_b.c~d-e", "mirror.example/cache/v2"}
	ases := []string{"registry.example/team/orders", "registry.example:5000/orders",
		"docker.io/library/orders", "docker.io/team/orders", "localhost:5000/a/b", "[fd00::5]:5000/a",
		"registry.example/a__b/c.d/e-f"}
	for _, host := range hosts {
		t.Run("a host "+strconv.Quote(host), func(t *testing.T) {
			load(t, "  - host: \""+host+"\"\n    ca: certs/ca.pem\n")
		})
	}
	for _, mirror := range mirrors {
		t.Run("a mirror "+strconv.Quote(mirror), func(t *testing.T) {
			load(t, "  - host: \"registry.example\"\n    mirror: \""+mirror+"\"\n    ca: certs/ca.pem\n")
		})
	}
	for _, value := range ases {
		t.Run("an as "+strconv.Quote(value), func(t *testing.T) {
			as(t, value)
		})
	}
	// Also accepted, as two entries each:
	//   registry.example and registry.example:443 (compared as written);
	//   a.example with mirror b.example/x, beside the entry b.example (a mirror on another
	//   entry's host takes nothing from it, section 9);
	//   an entry with a host alone, and one with a ca of two certificates and text around them.
	t.Run("registry.example and registry.example:443", func(t *testing.T) {
		load(t, "  - host: \"registry.example\"\n  - host: \"registry.example:443\"\n")
	})
	t.Run("a.example with a mirror on b.example, beside b.example", func(t *testing.T) {
		load(t, "  - host: \"a.example\"\n    mirror: \"b.example/x\"\n  - host: \"b.example\"\n")
	})
	t.Run("a host alone", func(t *testing.T) {
		load(t, "  - host: \"registry.example\"\n")
	})
	t.Run("a ca of two certificates and text around them", func(t *testing.T) {
		one, two := filepath.Join(dir, "certs", "one.pem"), filepath.Join(dir, "certs", "two.pem")
		writeCA(t, one)
		writeCA(t, two)
		first, err := os.ReadFile(one)
		if err != nil {
			t.Fatal(err)
		}
		second, err := os.ReadFile(two)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "certs", "both.pem"),
			"text before the certificates\n"+string(first)+"text between them\n"+string(second)+"text after them\n")
		load(t, "  - host: \"registry.example\"\n    ca: certs/both.pem\n")
	})
}

func TestAsHosts(t *testing.T) {
	// images: a with as registry.example/team/a, b with as registry.example/team/b,
	// c with as docker.io/library/c, d with no as -> []string{"docker.io", "registry.example"};
	// no as at all -> nil.
	text := variant(t, base, "deploy:\n", "images:\n"+
		"  a:\n    local: a:candidate\n    as: registry.example/team/a\n"+
		"  b:\n    local: b:candidate\n    as: registry.example/team/b\n"+
		"  c:\n    local: c:candidate\n    as: docker.io/library/c\n"+
		"  d:\n    local: d:candidate\ndeploy:\n")
	c, err := loadText(t, text)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff([]string{"docker.io", "registry.example"}, c.AsHosts()); d != "" {
		t.Errorf("AsHosts (-want +got):\n%s", d)
	}

	without := variant(t, base, "deploy:\n", "images:\n  d:\n    local: d:candidate\ndeploy:\n")
	c, err = loadText(t, without)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.AsHosts(); got != nil {
		t.Errorf("AsHosts() = %q, want nil", got)
	}
}
