package e2e

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io/fs"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// The mirror job's names: its two stand-ins and the ports they serve on, the login both ask
// for, the variable that holds the password, and the image they run from.
const (
	mirrorName   = "rollproof-mirrorjob-mirror"
	realName     = "rollproof-mirrorjob-real"
	mirrorHost   = mirrorName + ":8443"
	realHost     = realName + ":5443"
	jobUser      = "rollproof"
	passwordVar  = "ROLLPROOF_MIRROR_PASSWORD"
	standinImage = "rollproof-fixture-standin:dev"
)

// mirrorInputs is what the job's two fixtures need from outside: the engine's command, the
// folder of the authority's files, the folder of the fixture archives, the CA's path, and the
// digests of the previous and candidate images.
type mirrorInputs struct {
	engine, certs, archives, ca, previousDigest, candidateDigest string
}

// throughTheMirror is the first fixture through the mirror, by decisions 16 to 21: docker.io and
// the real host's stand-in with mirrors, a login and the CA; the previous image named after
// docker.io; the candidate through images with as on docker.io; the setup commands of
// mirrorSetup; Helm 3; limits.timeout 3m.
func throughTheMirror(in mirrorInputs) fixture {
	return fixture{
		name: "through the mirror",
		registries: []map[string]string{
			{"host": "docker.io", "username": jobUser, "password": "${env." + passwordVar + "}", "ca": in.ca, "mirror": mirrorHost + "/docker-io"},
			{"host": realHost, "username": jobUser, "password": "${env." + passwordVar + "}", "ca": in.ca, "mirror": mirrorHost + "/real"},
		},
		images: map[string]map[string]string{
			"candidate": {"local": "rollproof-fixture:candidate", "as": "docker.io/rollproof/fixture"},
		},
		literal: map[string]literalImage{
			"previous": {"docker.io/rollproof/fixture", in.previousDigest},
		},
		setup:  mirrorSetup(in),
		helm:   "helm-3",
		limits: map[string]string{"timeout": "3m"},
	}
}

// mirrorReplacesItsHost is as throughTheMirror, with no images and a candidate named after the
// real host's stand-in, which the mirror lacks (decision 19).
func mirrorReplacesItsHost(in mirrorInputs) fixture {
	f := throughTheMirror(in)
	f.name = "the mirror replaces its host"
	f.images = map[string]map[string]string{}
	f.literal["candidate"] = literalImage{realHost + "/team/candidate", in.candidateDigest}
	return f
}

// mirrorSetup is the job's setup commands, in order: start both stand-ins, wait for each, a
// public pull in a node, which must pass, the cut in each of the four nodes, the same pull, which
// must fail within 15 s, the resolution of the real host's name in a node, and the password
// printed plainly and in base64.
func mirrorSetup(in mirrorInputs) [][]string {
	password := "${env." + passwordVar + "}"
	return [][]string{
		{in.engine, "run", "-d", "--name", mirrorName, "--network", "kind", "-v", in.certs + ":/certs:ro", "-v", in.archives + ":/images:ro",
			"-e", "STANDIN_PASSWORD=" + password, standinImage, "-listen", ":8443", "-cert", "/certs/server.pem",
			"-key", "/certs/server-key.pem", "-user", jobUser, "-prefix", "/docker-io", "-prefix", "/real",
			"-load", "rollproof/fixture=/images/previous.tar"},
		{in.engine, "run", "-d", "--name", realName, "--network", "kind", "-v", in.certs + ":/certs:ro", "-v", in.archives + ":/images:ro",
			"-e", "STANDIN_PASSWORD=" + password, standinImage, "-listen", ":5443", "-cert", "/certs/server.pem",
			"-key", "/certs/server-key.pem", "-user", jobUser,
			"-load", "team/candidate=/images/candidate.tar"},
		{in.engine, "exec", mirrorName, "/standin", "-check", "https://127.0.0.1:8443/v2/"},
		{in.engine, "exec", realName, "/standin", "-check", "https://127.0.0.1:5443/v2/"},
		{in.engine, "exec", "rollproof-e2e-worker", "crictl", "pull", "registry.k8s.io/pause:3.10"},
		{in.engine, "exec", "rollproof-e2e-control-plane", "sh", "-c", cutScript},
		{in.engine, "exec", "rollproof-e2e-worker", "sh", "-c", cutScript},
		{in.engine, "exec", "rollproof-e2e-worker2", "sh", "-c", cutScript},
		{in.engine, "exec", "rollproof-e2e-worker3", "sh", "-c", cutScript},
		{in.engine, "exec", "rollproof-e2e-worker", "sh", "-c",
			`if timeout 15 crictl pull registry.k8s.io/pause:3.10; then echo "rollproof: a public pull passed the cut" >&2; exit 1; fi`},
		{in.engine, "exec", "rollproof-e2e-worker", "getent", "hosts", realName},
		{"sh", "-c", "printf '%s\\n' \"$" + passwordVar + "\"; printf '%s' \"" + jobUser + ":$" + passwordVar + "\" | base64 -w0; echo"},
	}
}

// cutScript is the script that cuts a node's route to everything but the engine network's
// subnets, kind's pod and service subnets, loopback, and IPv6 link-local and multicast; it fails
// when it finds no IPv4 subnet or when its chain is missing afterwards.
const cutScript = `set -e
v4=$(ip -4 -o route show dev eth0 scope link | cut -d' ' -f1)
v6=$(ip -6 -o route show dev eth0 | cut -d' ' -f1)
[ -n "$v4" ] || { echo "rollproof: no IPv4 subnet on eth0" >&2; exit 1; }
iptables-nft -N ROLLPROOF-CUT
iptables-nft -A ROLLPROOF-CUT -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
iptables-nft -A ROLLPROOF-CUT -o lo -j RETURN
for net in $v4 10.244.0.0/16 10.96.0.0/16; do
  case "$net" in */*) iptables-nft -A ROLLPROOF-CUT -d "$net" -j RETURN;; esac
done
iptables-nft -A ROLLPROOF-CUT -p tcp -j REJECT --reject-with tcp-reset
iptables-nft -A ROLLPROOF-CUT -j REJECT --reject-with icmp-port-unreachable
iptables-nft -I OUTPUT 1 -j ROLLPROOF-CUT
iptables-nft -I FORWARD 1 -j ROLLPROOF-CUT
ip6tables-nft -N ROLLPROOF-CUT
ip6tables-nft -A ROLLPROOF-CUT -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
ip6tables-nft -A ROLLPROOF-CUT -o lo -j RETURN
for net in $v6 fe80::/10 ff00::/8; do
  case "$net" in */*) ip6tables-nft -A ROLLPROOF-CUT -d "$net" -j RETURN;; esac
done
ip6tables-nft -A ROLLPROOF-CUT -p tcp -j REJECT --reject-with tcp-reset
ip6tables-nft -A ROLLPROOF-CUT -j REJECT --reject-with icmp6-port-unreachable
ip6tables-nft -I OUTPUT 1 -j ROLLPROOF-CUT
ip6tables-nft -I FORWARD 1 -j ROLLPROOF-CUT
iptables-nft -S ROLLPROOF-CUT >/dev/null
ip6tables-nft -S ROLLPROOF-CUT >/dev/null
`

// newAuthority writes ca.pem, server.pem and server-key.pem into dir, mode 0644: a CA and a
// server certificate it signed for both stand-ins' names and 127.0.0.1, valid for one day. It
// returns ca.pem's path. The CA's key is never written.
func newAuthority(dir string) (caPath string, err error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	now := time.Now()
	ca := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "rollproof mirror job's authority"},
		NotBefore:             now,
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		return "", err
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	server := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: mirrorName},
		NotBefore:    now,
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{mirrorName, realName},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, server, ca, &serverKey.PublicKey, caKey)
	if err != nil {
		return "", err
	}
	serverKeyDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return "", err
	}
	files := []struct {
		name  string
		block *pem.Block
	}{
		{"ca.pem", &pem.Block{Type: "CERTIFICATE", Bytes: caDER}},
		{"server.pem", &pem.Block{Type: "CERTIFICATE", Bytes: serverDER}},
		{"server-key.pem", &pem.Block{Type: "EC PRIVATE KEY", Bytes: serverKeyDER}},
	}
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if err := os.WriteFile(path, pem.EncodeToMemory(f.block), 0o644); err != nil {
			return "", err
		}
		if err := os.Chmod(path, 0o644); err != nil {
			return "", err
		}
	}
	return filepath.Join(dir, "ca.pem"), nil
}

// newPassword returns 20 random lower-case letters and digits followed by "&<, so that the JSON
// and Go spellings differ from the plain one.
func newPassword() (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	random := make([]byte, 20)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, c := range random {
		b.WriteByte(alphabet[int(c)%len(alphabet)])
	}
	b.WriteString(`"&<`)
	return b.String(), nil
}

// passwordForms returns every form to search for, each once: plain, the base64 of USER:PASSWORD
// padded and not, the JSON spellings with and without HTML escaping, and the Go spelling.
func passwordForms(user, password string) []string {
	credential := user + ":" + password
	quoted, _ := json.Marshal(password) // a string always encodes
	var plain strings.Builder
	enc := json.NewEncoder(&plain)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(password) // encoding a string into a Builder cannot fail
	unescaped := strings.TrimSuffix(plain.String(), "\n")
	forms := []string{
		password,
		base64.StdEncoding.EncodeToString([]byte(credential)),
		base64.RawStdEncoding.EncodeToString([]byte(credential)),
		string(quoted[1 : len(quoted)-1]),
		unescaped[1 : len(unescaped)-1],
		strconv.Quote(password),
	}
	seen := make(map[string]bool, len(forms))
	unique := forms[:0]
	for _, form := range forms {
		if !seen[form] {
			seen[form] = true
			unique = append(unique, form)
		}
	}
	return unique
}

// findForms walks every regular file under root and returns, sorted, "PATH holds form I" for each
// file that holds a form, PATH relative to root; none is empty. I is the form's 0-based index in
// forms, never its text, and an empty form is skipped rather than searched.
func findForms(root string, forms []string) ([]string, error) {
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		content := string(b)
		for i, form := range forms {
			if form == "" {
				continue
			}
			if strings.Contains(content, form) {
				found = append(found, rel+" holds form "+strconv.Itoa(i))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(found)
	return found, nil
}

// standinRequest is one REQ line of a stand-in's log.
type standinRequest struct {
	method, path, user string
	status             int
}

// parseStandinLog returns the addresses of the CONN lines and the REQ lines of a stand-in's log,
// in order; other lines are skipped.
func parseStandinLog(text string) (conns []string, reqs []standinRequest) {
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "CONN "):
			conns = append(conns, strings.TrimPrefix(line, "CONN "))
		case strings.HasPrefix(line, "REQ "):
			fields := strings.Fields(line)
			if len(fields) != 5 || !strings.HasPrefix(fields[3], "user=") || !strings.HasPrefix(fields[4], "status=") {
				continue
			}
			status, err := strconv.Atoi(strings.TrimPrefix(fields[4], "status="))
			if err != nil {
				continue
			}
			reqs = append(reqs, standinRequest{
				method: fields[1],
				path:   fields[2],
				user:   strings.TrimPrefix(fields[3], "user="),
				status: status,
			})
		}
	}
	return conns, reqs
}

// loopbackProblems returns what the CONN lines of the stand-in name's log show against the check
// that nothing outside its own container connected to it: a sentence when no line is from
// 127.0.0.1, which the setup's -check exec makes, so that a log with no connection at all proves
// nothing; then a sentence for each line from another address, a node or anything else.
func loopbackProblems(name, text string) []string {
	conns, _ := parseStandinLog(text)
	var problems []string
	loopback := false
	for _, addr := range conns {
		if host, _, err := net.SplitHostPort(addr); err == nil && host == "127.0.0.1" {
			loopback = true
			continue
		}
		problems = append(problems, name+" logged a connection from "+addr+", want none from an address other than 127.0.0.1")
	}
	if !loopback {
		problems = append([]string{name + " logged no connection from 127.0.0.1, which the setup's -check makes, so its log proves nothing"}, problems...)
	}
	return problems
}

func TestLoopbackProblems(t *testing.T) {
	const noLoopback = "real logged no connection from 127.0.0.1, which the setup's -check makes, so its log proves nothing"
	for _, r := range []struct {
		name, text string
		want       []string
	}{
		{"loopback only", "READY\nCONN 127.0.0.1:40000\nCONN 127.0.0.1:40002\n", nil},
		{"no CONN line", "READY\nLOAD team/candidate@sha256:d\n", []string{noLoopback}},
		{"loopback and a node", "READY\nCONN 127.0.0.1:40000\nCONN 10.89.0.3:51000\n",
			[]string{"real logged a connection from 10.89.0.3:51000, want none from an address other than 127.0.0.1"}},
		{"a node only", "CONN 10.89.0.3:51000\n",
			[]string{noLoopback, "real logged a connection from 10.89.0.3:51000, want none from an address other than 127.0.0.1"}},
		{"an address with no port", "CONN 127.0.0.1:40000\nCONN 127.0.0.1\n",
			[]string{"real logged a connection from 127.0.0.1, want none from an address other than 127.0.0.1"}},
	} {
		t.Run(r.name, func(t *testing.T) {
			if diff := cmp.Diff(r.want, loopbackProblems("real", r.text)); diff != "" {
				t.Errorf("loopbackProblems (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCutScript(t *testing.T) {
	// exec.Command("sh", "-n", "-c", cutScript) exits 0; the script holds "-I OUTPUT 1 -j ROLLPROOF-CUT"
	// and "-I FORWARD 1 -j ROLLPROOF-CUT" for both families, "10.244.0.0/16", "10.96.0.0/16",
	// "fe80::/10", "ff00::/8", "--reject-with tcp-reset", and its last line checks the IPv6 chain.
	if out, err := exec.Command("sh", "-n", "-c", cutScript).CombinedOutput(); err != nil {
		t.Fatalf("sh -n rejected the cut: %v\n%s", err, out)
	}
	for _, want := range []string{"-I OUTPUT 1 -j ROLLPROOF-CUT", "-I FORWARD 1 -j ROLLPROOF-CUT"} {
		if got := strings.Count(cutScript, want); got != 2 {
			t.Errorf("the cut holds %q %d times, want 2, once for each family", want, got)
		}
	}
	for _, want := range []string{"10.244.0.0/16", "10.96.0.0/16", "fe80::/10", "ff00::/8", "--reject-with tcp-reset"} {
		if !strings.Contains(cutScript, want) {
			t.Errorf("the cut does not hold %q", want)
		}
	}
	lines := strings.Split(strings.TrimSuffix(cutScript, "\n"), "\n")
	if last := lines[len(lines)-1]; last != "ip6tables-nft -S ROLLPROOF-CUT >/dev/null" {
		t.Errorf("the cut's last line is %q, want the IPv6 chain's check", last)
	}
}

func TestNewAuthority(t *testing.T) {
	// The folder holds exactly ca.pem, server.pem and server-key.pem, so no key of the authority
	// is written; the server certificate verifies against ca.pem for the DNS names mirrorName and
	// realName and for the IP 127.0.0.1; server-key.pem is its key (tls.LoadX509KeyPair); every
	// file is 0644.
	dir := t.TempDir()
	caPath, err := newAuthority(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "ca.pem"); caPath != want {
		t.Errorf("newAuthority returned %q, want %q", caPath, want)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if diff := cmp.Diff([]string{"ca.pem", "server-key.pem", "server.pem"}, names); diff != "" {
		t.Errorf("the folder's files (-want +got):\n%s", diff)
	}
	for _, name := range []string{"ca.pem", "server.pem", "server-key.pem"} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if perm := fi.Mode().Perm(); perm != 0o644 {
			t.Errorf("%s has mode %v, want 0644", name, perm)
		}
	}
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatalf("%s holds no certificate", caPath)
	}
	serverPEM, err := os.ReadFile(filepath.Join(dir, "server.pem"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(serverPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("server.pem holds no certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{mirrorName, realName} {
		if _, err := cert.Verify(x509.VerifyOptions{DNSName: name, Roots: roots}); err != nil {
			t.Errorf("the server certificate does not verify for %s: %v", name, err)
		}
	}
	if _, err := cert.Verify(x509.VerifyOptions{DNSName: "127.0.0.1", Roots: roots}); err != nil {
		t.Errorf("the server certificate does not verify for 127.0.0.1: %v", err)
	}
	if span := cert.NotAfter.Sub(cert.NotBefore); span != 24*time.Hour {
		t.Errorf("the server certificate is valid for %v, want one day", span)
	}
	if _, err := tls.LoadX509KeyPair(filepath.Join(dir, "server.pem"), filepath.Join(dir, "server-key.pem")); err != nil {
		t.Errorf("server-key.pem is not the server certificate's key: %v", err)
	}
}

func TestPasswordForms(t *testing.T) {
	// newPassword gives 23 bytes ending in `"&<`, a new one each call; passwordForms("rollproof", pw)
	// holds pw, base64 of "rollproof:"+pw padded and not, json.Marshal's spelling without its quotes,
	// the spelling without HTML escaping, and strconv.Quote's, each once.
	pw, err := newPassword()
	if err != nil {
		t.Fatal(err)
	}
	if len(pw) != 23 || !strings.HasSuffix(pw, `"&<`) {
		t.Errorf("newPassword gave %q, want 23 bytes ending in \"&<", pw)
	}
	for i := range []byte(pw[:20]) {
		if c := pw[i]; !('a' <= c && c <= 'z' || '0' <= c && c <= '9') {
			t.Errorf("byte %d of newPassword's %q is not a lower-case letter or a digit", i, pw)
		}
	}
	again, err := newPassword()
	if err != nil {
		t.Fatal(err)
	}
	if again == pw {
		t.Errorf("newPassword gave %q twice", pw)
	}

	var plain strings.Builder
	enc := json.NewEncoder(&plain)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(pw); err != nil {
		t.Fatal(err)
	}
	escaped := strings.TrimSuffix(plain.String(), "\n")
	quoted, err := json.Marshal(pw)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		pw,
		base64.StdEncoding.EncodeToString([]byte("rollproof:" + pw)),
		base64.RawStdEncoding.EncodeToString([]byte("rollproof:" + pw)),
		string(quoted[1 : len(quoted)-1]),
		escaped[1 : len(escaped)-1],
		strconv.Quote(pw),
	}
	// The credential is 33 bytes, so its base64 form carries no padding and the two base64 forms
	// are one; the spellings are held once, in order.
	seen := make(map[string]bool, len(want))
	unique := want[:0]
	for _, form := range want {
		if !seen[form] {
			seen[form] = true
			unique = append(unique, form)
		}
	}
	forms := passwordForms("rollproof", pw)
	if diff := cmp.Diff(unique, forms); diff != "" {
		t.Errorf("passwordForms (-want +got):\n%s", diff)
	}
}

func TestFindForms(t *testing.T) {
	// A finding names the form's 0-based index, never its text, and an empty form is never
	// searched. With forms {"alpha", "bravo", ""}, a/b.log holding "before bravo after" and c.txt
	// holding none, findForms gives exactly {"a/b.log holds form 1"}: a finding that printed the
	// text would say "holds form bravo", and an empty form searched would add both files.
	//
	// The second case pins the sort and every file and form: with forms {"alpha", "bravo"}, z.txt
	// holding "alpha" and a/b.log holding "bravo and alpha", findForms gives exactly
	// {"a/b.log holds form 0", "a/b.log holds form 1", "z.txt holds form 0"}.
	t.Run("names the index and skips an empty form", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "a"), 0o755); err != nil {
			t.Fatal(err)
		}
		for path, text := range map[string]string{
			filepath.Join("a", "b.log"): "before bravo after",
			"c.txt":                     "nothing to find",
		} {
			if err := os.WriteFile(filepath.Join(dir, path), []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		got, err := findForms(dir, []string{"alpha", "bravo", ""})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"a/b.log holds form 1"}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("findForms (-want +got):\n%s", diff)
		}
	})
	t.Run("sorts every file and form", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "a"), 0o755); err != nil {
			t.Fatal(err)
		}
		for path, text := range map[string]string{
			"z.txt":                     "alpha",
			filepath.Join("a", "b.log"): "bravo and alpha",
		} {
			if err := os.WriteFile(filepath.Join(dir, path), []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		got, err := findForms(dir, []string{"alpha", "bravo"})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"a/b.log holds form 0", "a/b.log holds form 1", "z.txt holds form 0"}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("findForms (-want +got):\n%s", diff)
		}
	})
}

func TestParseStandinLog(t *testing.T) {
	// "READY\nCONN 10.4.1.2:40000\nREQ HEAD /docker-io/v2/x/manifests/d user=rollproof status=200\nLOAD x@d\n"
	// gives conns []string{"10.4.1.2:40000"} and one request {HEAD, /docker-io/v2/x/manifests/d, rollproof, 200}.
	text := "READY\nCONN 10.4.1.2:40000\nREQ HEAD /docker-io/v2/x/manifests/d user=rollproof status=200\nLOAD x@d\n"
	conns, reqs := parseStandinLog(text)
	if diff := cmp.Diff([]string{"10.4.1.2:40000"}, conns); diff != "" {
		t.Errorf("conns (-want +got):\n%s", diff)
	}
	want := []standinRequest{{method: "HEAD", path: "/docker-io/v2/x/manifests/d", user: "rollproof", status: 200}}
	if diff := cmp.Diff(want, reqs, cmp.AllowUnexported(standinRequest{})); diff != "" {
		t.Errorf("requests (-want +got):\n%s", diff)
	}
}
