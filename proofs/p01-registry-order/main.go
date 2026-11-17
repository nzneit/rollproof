//go:build linux

// Proof of point 1, first half: what a kind node does with an image reference
// for a host whose containerd settings list the run's registry before the
// real host: by digest, by tag, by both, and when the run's registry lacks
// the image. Also which command has a node pull through those settings, and
// whether a pod's status names the digest it runs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	kindexec "sigs.k8s.io/kind/pkg/exec"

	"rollproof-proofs/internal/lab"
)

const (
	certsDPatch = "[plugins.\"io.containerd.grpc.v1.cri\".registry]\n  config_path = \"/etc/containerd/certs.d\""

	appGoMod  = "module p01app\n\ngo 1.27\n"
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

	// exampleHosts is the settings file of action 5: real.Node, then run.Node.
	exampleHosts = "server = \"http://%s\"\n[host.\"http://%s\"]\n  capabilities = [\"pull\"]\n"
	// testHosts is the settings file of case e: run.Node.
	testHosts = "[host.\"http://%s\"]\n  capabilities = [\"pull\"]\n"

	namespace = "p01"
)

var zeroDigest = "sha256:" + strings.Repeat("0", 64)

// caseNames are the cases of action 8. Each must run to its end, or the
// summary counts it as failed.
var caseNames = []string{"a", "b", "c", "d", "e", "f", "g"}

// accessLine matches the request and the status of a registry's access-log line.
var accessLine = regexp.MustCompile(`"([A-Z]+) (\S+) HTTP/1\.1" (\d{3})`)

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

	dir, err := os.MkdirTemp("", "rollproof-proof-p01-app-")
	if err != nil {
		c.fail("a temporary folder for the five binaries: %v", err)
		return summary(ctx, c)
	}
	defer os.RemoveAll(dir)

	prove(ctx, c, dir)
	return summary(ctx, c)
}

// proof is what the cases share.
type proof struct {
	ctx            context.Context
	c              *checks
	l              *lab.Lab
	worker         string
	run, real      lab.Registry
	x, y, y2, z, w string // the five digests
}

// prove does actions 1 to 9. The cluster is deleted before it returns.
func prove(ctx context.Context, c *checks, dir string) {
	// Action 1: five binaries.
	if err := build(ctx, dir); err != nil {
		c.fail("building the five binaries: %v", err)
		return
	}

	// Action 2: the cluster.
	cfg := &v1alpha4.Cluster{
		Nodes:                   []v1alpha4.Node{{Role: v1alpha4.ControlPlaneRole}, {Role: v1alpha4.WorkerRole}},
		ContainerdConfigPatches: []string{certsDPatch},
	}
	l, err := lab.Up(ctx, "p01", cfg)
	if err != nil {
		c.fail("lab.Up: %v", err)
		return
	}
	// Action 9, also when a step fails.
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
	p := &proof{ctx: ctx, c: c, l: l, worker: nodes[1]}
	info("worker %s", p.worker)
	for _, tool := range []string{"crictl", "ctr"} {
		if out, err := l.Exec(p.worker, nil, tool, "--version"); err != nil {
			info("%s --version in the worker: %v", tool, err)
		} else {
			info("%s --version in the worker: %s", tool, strings.TrimSpace(out))
		}
	}

	// Action 3: the two registries.
	if p.run, err = l.Registry(ctx, "run"); err != nil {
		c.fail("l.Registry(ctx, \"run\"): %v", err)
		return
	}
	if p.real, err = l.Registry(ctx, "real"); err != nil {
		c.fail("l.Registry(ctx, \"real\"): %v", err)
		return
	}
	info("run: Push %s, Node %s; real: Push %s, Node %s", p.run.Push, p.run.Node, p.real.Push, p.real.Node)
	if log, err := p.run.Logs(); err == nil {
		for line := range strings.Lines(log) {
			if strings.Contains(line, "version=") {
				info("run's log: %s", strings.TrimSpace(line))
				break
			}
		}
	}

	// Action 4: the five images, and the two tags.
	for _, push := range []struct {
		v, repository string
		to            lab.Registry
		toName        string
		digest        *string
	}{
		{"x", "team/app", p.run, "run", &p.x},
		{"y", "team/app", p.run, "run", &p.y},
		{"y2", "team/app", p.real, "real", &p.y2},
		{"z", "team/app", p.real, "real", &p.z},
		{"w", "probe/rollproof", p.run, "run", &p.w},
	} {
		d, err := lab.PushBinary(ctx, push.to.Push, push.repository, filepath.Join(dir, push.v))
		if err != nil {
			c.fail("pushing %s: %v", push.v, err)
			return
		}
		*push.digest = d
		info("%s = %s, in %s as %s", strings.ToUpper(push.v), d, push.toName, push.repository)
	}
	for _, t := range []struct {
		to     lab.Registry
		toName string
		digest string
	}{{p.run, "run", p.y}, {p.real, "real", p.y2}} {
		if err := tagV1(ctx, t.to.Push, t.digest); err != nil {
			c.fail("tagging %s as team/app:v1 in %s: %v", t.digest, t.toName, err)
			return
		}
	}
	info("team/app:v1 is Y in run and Y2 in real")

	// Action 5: the settings for registry.example in every node.
	text := fmt.Sprintf(exampleHosts, p.real.Node, p.run.Node)
	if err := p.writeHosts("registry.example", text); err != nil {
		c.fail("writing the settings for registry.example: %v", err)
		return
	}
	showHosts("registry.example", text)

	// Actions 7 and 8: the cases.
	for _, cs := range []func(){p.caseA, p.caseB, p.caseC, p.caseD, p.caseE, p.caseF, p.caseG} {
		if ctx.Err() != nil {
			return
		}
		cs()
	}
}

// build writes the module p01app into dir and builds it there five times,
// one binary for each value of v.
func build(ctx context.Context, dir string) error {
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(appGoMod), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(appMainGo), 0o644); err != nil {
		return err
	}
	for _, v := range []string{"x", "y", "y2", "z", "w"} {
		cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-ldflags", "-s -w -X main.v="+v, "-o", filepath.Join(dir, v), ".")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("go build for %s: %w\n%s", v, err, out)
		}
	}
	return nil
}

// tagV1 writes the image PUSH/team/app@digest under the tag team/app:v1 in
// the same registry. The proof uses a tag here; the tool never does.
func tagV1(ctx context.Context, push, digest string) error {
	src, err := name.NewDigest(push+"/team/app@"+digest, name.Insecure)
	if err != nil {
		return err
	}
	img, err := remote.Image(src, remote.WithContext(ctx))
	if err != nil {
		return err
	}
	dst, err := name.NewTag(push+"/team/app:v1", name.Insecure)
	if err != nil {
		return err
	}
	return remote.Write(dst, img, remote.WithContext(ctx))
}

// writeHosts writes text to /etc/containerd/certs.d/HOST/hosts.toml in every node.
func (p *proof) writeHosts(host, text string) error {
	dir := "/etc/containerd/certs.d/" + host
	for _, node := range p.l.Nodes() {
		if _, err := p.l.Exec(node, nil, "mkdir", "-p", dir); err != nil {
			return err
		}
		if _, err := p.l.Exec(node, strings.NewReader(text), "cp", "/dev/stdin", dir+"/hosts.toml"); err != nil {
			return err
		}
	}
	return nil
}

// showHosts prints the settings file written for host.
func showHosts(host, text string) {
	info("/etc/containerd/certs.d/%s/hosts.toml, written in every node:", host)
	for line := range strings.Lines(text) {
		info("  %s", strings.TrimRight(line, "\n"))
	}
}

// pull runs crictl pull REFERENCE in the worker.
func (p *proof) pull(ref string) (string, error) {
	return p.l.Exec(p.worker, nil, "crictl", "pull", ref)
}

// rmi removes the image ref from the worker; an error is ignored.
func (p *proof) rmi(ref string) { _, _ = p.l.Exec(p.worker, nil, "crictl", "rmi", ref) }

// inspect returns status.repoTags and status.repoDigests of the image ref in
// the worker.
func (p *proof) inspect(ref string) (tags, digests []string, err error) {
	out, err := p.l.Exec(p.worker, nil, "crictl", "inspecti", "-o", "json", ref)
	if err != nil {
		return nil, nil, err
	}
	i := strings.Index(out, "{")
	if i < 0 {
		return nil, nil, fmt.Errorf("crictl inspecti %s printed no JSON object:\n%s", ref, out)
	}
	var v struct {
		Status struct{ RepoTags, RepoDigests []string } `json:"status"`
	}
	if err := json.NewDecoder(strings.NewReader(out[i:])).Decode(&v); err != nil {
		return nil, nil, fmt.Errorf("reading crictl inspecti %s: %w\n%s", ref, err, out)
	}
	return v.Status.RepoTags, v.Status.RepoDigests, nil
}

// pulled is the condition that crictl pull of ref succeeded.
func pulled(ref, out string, err error) cond {
	text := "crictl pull " + ref + " succeeded"
	if err != nil {
		text += " (it did not: " + oneLine(out) + ")"
	}
	return cond{err == nil, text}
}

// logsRead is the failed condition for a case whose registry logs could not
// be read.
func logsRead(err error) cond {
	return cond{false, "both registries' logs were read (" + oneLine(err.Error()) + ")"}
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
// "METHOD PATH HTTP/1.1" STATUS; when log has none, one for each line with
// the fields http.request.method, http.request.uri and http.response.status.
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

// hasSuffix reports whether reqs hold a request whose path, its query left
// out, ends in suffix, answered with status; a status of 0 matches any.
func hasSuffix(reqs []request, suffix string, status int) bool {
	return slices.ContainsFunc(reqs, func(r request) bool {
		return strings.HasSuffix(bare(r.Path), suffix) && (status == 0 || r.Status == status)
	})
}

// settledLogs returns the registry's log once two reads 500 ms apart agree,
// or the last read after 5 seconds: a line may reach the log a moment after
// the request it records was answered.
func settledLogs(r lab.Registry) (string, error) {
	prev, err := r.Logs()
	if err != nil {
		return "", err
	}
	for range 10 {
		time.Sleep(500 * time.Millisecond)
		cur, err := r.Logs()
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

// observe notes where both registries' logs end, runs act, and returns the
// requests under /v2/ that each registry logged since. It prints each of
// them as an info line labelled label.
func (p *proof) observe(label string, act func()) (inRun, inReal []request, err error) {
	runBefore, err := settledLogs(p.run)
	if err != nil {
		return nil, nil, err
	}
	realBefore, err := settledLogs(p.real)
	if err != nil {
		return nil, nil, err
	}
	act()
	for _, r := range []struct {
		name   string
		reg    lab.Registry
		before int
		into   *[]request
	}{{"run", p.run, len(runBefore), &inRun}, {"real", p.real, len(realBefore), &inReal}} {
		log, err := settledLogs(r.reg)
		if err != nil {
			return nil, nil, err
		}
		if len(log) < r.before {
			return nil, nil, fmt.Errorf("the log of %s is shorter than before %s", r.name, label)
		}
		for _, req := range requests(log[r.before:]) {
			if strings.HasPrefix(req.Path, "/v2/") {
				*r.into = append(*r.into, req)
			}
		}
		if len(*r.into) == 0 {
			info("%s %s: no new request", label, r.name)
		}
		for _, req := range *r.into {
			info("%s %s: %s %s %d", label, r.name, req.Method, req.Path, req.Status)
		}
	}
	return inRun, inReal, nil
}

// caseA pulls by digest an image that the run's registry holds.
func (p *proof) caseA() {
	ref := "registry.example/team/app@" + p.x
	p.rmi(ref)
	var out string
	var pullErr error
	runReqs, realReqs, err := p.observe("a", func() { out, pullErr = p.pull(ref) })
	if err != nil {
		p.c.verdict("a", logsRead(err))
		return
	}
	p.c.verdict("a",
		pulled(ref, out, pullErr),
		cond{hasPath(runReqs, "/v2/team/app/manifests/"+p.x, 200), "run answered /v2/team/app/manifests/X with 200"},
		cond{len(realReqs) == 0, "real had no new request"},
	)
}

// caseB pulls by a tag that both registries hold, for different images.
func (p *proof) caseB() {
	ref := "registry.example/team/app:v1"
	p.rmi(ref)
	var out string
	var pullErr error
	runReqs, _, err := p.observe("b", func() { out, pullErr = p.pull(ref) })
	if err != nil {
		p.c.verdict("b", logsRead(err))
		return
	}
	_, digests, ierr := p.inspect(ref)
	if ierr != nil {
		info("b: crictl inspecti -o json %s: %s", ref, oneLine(ierr.Error()))
	} else {
		info("b: status.repoDigests: %s", strings.Join(digests, " "))
	}
	p.c.verdict("b",
		pulled(ref, out, pullErr),
		cond{slices.Contains(digests, "registry.example/team/app@"+p.y2), "status.repoDigests lists registry.example/team/app@Y2"},
		cond{!hasPath(runReqs, "/v2/team/app/manifests/v1", 0), "run had no request for /v2/team/app/manifests/v1"},
	)
}

// caseC pulls by digest an image that only the real registry holds.
func (p *proof) caseC() {
	ref := "registry.example/team/app@" + p.z
	p.rmi(ref)
	var out string
	var pullErr error
	runReqs, realReqs, err := p.observe("c", func() { out, pullErr = p.pull(ref) })
	if err != nil {
		p.c.verdict("c", logsRead(err))
		return
	}
	p.c.verdict("c",
		pulled(ref, out, pullErr),
		cond{hasSuffix(runReqs, p.z, 404), "run answered a path that ends in Z with 404"},
		cond{hasPath(realReqs, "/v2/team/app/manifests/"+p.z, 200), "real answered /v2/team/app/manifests/Z with 200"},
	)
}

// caseD pulls a reference with a tag and a digest.
func (p *proof) caseD() {
	ref := "registry.example/team/app:whatever@" + p.x
	p.rmi(ref)
	var out string
	var pullErr error
	runReqs, realReqs, err := p.observe("d", func() { out, pullErr = p.pull(ref) })
	if err != nil {
		p.c.verdict("d", logsRead(err))
		return
	}
	if tags, digests, ierr := p.inspect(ref); ierr != nil {
		info("d: crictl inspecti -o json %s: %s", ref, oneLine(ierr.Error()))
	} else {
		info("d: status.repoTags: %q; status.repoDigests: %q", tags, digests)
	}
	p.c.verdict("d",
		pulled(ref, out, pullErr),
		cond{!hasSuffix(runReqs, "/manifests/whatever", 0) && !hasSuffix(realReqs, "/manifests/whatever", 0),
			"neither registry had a request whose path ends in /manifests/whatever"},
	)
}

// caseE writes settings for rollproof.test that list only the run's
// registry, pulls an image it holds, then a digest that no registry holds.
func (p *proof) caseE() {
	text := fmt.Sprintf(testHosts, p.run.Node)
	if err := p.writeHosts("rollproof.test", text); err != nil {
		p.c.verdict("e", cond{false, "the settings for rollproof.test were written (" + oneLine(err.Error()) + ")"})
		return
	}
	showHosts("rollproof.test", text)

	ref := "rollproof.test/probe/rollproof@" + p.w
	zero := "rollproof.test/probe/rollproof@" + zeroDigest
	p.rmi(ref)
	p.rmi(zero)
	var out string
	var pullErr error
	if _, _, err := p.observe("e", func() { out, pullErr = p.pull(ref) }); err != nil {
		p.c.verdict("e", logsRead(err))
		return
	}

	var zeroOut string
	var zeroErr error
	var start time.Time
	var took time.Duration
	_, _, err := p.observe("e-zero", func() {
		start = time.Now()
		zeroOut, zeroErr = p.pull(zero)
		took = time.Since(start).Round(time.Millisecond)
	})
	if zeroErr != nil {
		info("e-zero: crictl pull %s failed after %v; its output:", zero, took)
	} else {
		info("e-zero: crictl pull %s succeeded after %v; its output:", zero, took)
	}
	for line := range strings.Lines(strings.TrimSpace(zeroOut)) {
		info("e-zero:   %s", strings.TrimRight(line, "\n"))
	}
	// The error text names a 404 even when a later host failed otherwise, so
	// containerd's own log is what shows which hosts the pull tried.
	journal, jerr := p.l.Exec(p.worker, nil, "journalctl", "-u", "containerd", "--no-pager", "-o", "cat", "--since", fmt.Sprintf("@%d", start.Unix()-1))
	if jerr != nil {
		info("e-zero: journalctl -u containerd in the worker: %s", oneLine(jerr.Error()))
	}
	for line := range strings.Lines(journal) {
		if strings.Contains(line, zeroDigest) {
			info("e-zero: containerd: %s", strings.TrimSpace(line))
		}
	}
	conds := []cond{pulled(ref, out, pullErr)}
	if err != nil {
		conds = append(conds, logsRead(err))
	}
	p.c.verdict("e", conds...)
}

// caseF pulls with ctr, which the tool must not use if it ignores the node's
// settings. What ctr does is recorded, not judged.
func (p *proof) caseF() {
	ref := "registry.example/team/app@" + p.x
	p.rmi(ref)
	var out string
	var ctrErr error
	runReqs, realReqs, err := p.observe("f", func() {
		out, ctrErr = p.l.Exec(p.worker, nil, "ctr", "-n", "k8s.io", "images", "pull", ref)
	})
	code := 0
	if ctrErr != nil {
		code = exitCode(ctrErr)
	}
	info("f: ctr -n k8s.io images pull %s: exit code %d (nerdctl reports 1 for any non-zero code); its output:", ref, code)
	for line := range strings.Lines(strings.TrimSpace(out)) {
		info("f:   %s", strings.TrimRight(line, "\n"))
	}
	if err != nil {
		p.c.verdict("f", logsRead(err))
		return
	}
	p.c.verdict("f", cond{true, fmt.Sprintf("ctr ran in the worker and both logs were read: exit code %d; %d new requests in run, %d in real", code, len(runReqs), len(realReqs))})
}

// exitCode returns the exit code of the engine's exec that err reports, or -1.
func exitCode(err error) int {
	var re *kindexec.RunError
	if errors.As(err, &re) {
		var ee *exec.ExitError
		if errors.As(re.Inner, &ee) {
			return ee.ExitCode()
		}
	}
	return -1
}

// caseG runs two pods on the worker: one of an image the worker holds, whose
// status should name its digest, and one that the kubelet must pull.
func (p *proof) caseG() {
	client := p.l.Client()
	if _, err := client.CoreV1().Namespaces().Create(p.ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}, metav1.CreateOptions{}); err != nil {
		p.c.verdict("g", cond{false, "the namespace " + namespace + " was created (" + oneLine(err.Error()) + ")"})
		return
	}
	if err := p.waitServiceAccount(); err != nil {
		p.c.verdict("g", cond{false, "the namespace's default service account exists (" + oneLine(err.Error()) + ")"})
		return
	}

	wRef := "rollproof.test/probe/rollproof@" + p.w
	var probeW *corev1.Pod
	var wErr error
	runW, _, err := p.observe("g-probe-w", func() { probeW, wErr = p.runPod("probe-w", wRef) })
	if err != nil {
		p.c.verdict("g", logsRead(err))
		return
	}
	wID := imageID(probeW)
	info("g: probe-w status.containerStatuses[0].imageID: %s", wID)

	xRef := "registry.example/team/app@" + p.x
	p.rmi(xRef)
	var kubeletX *corev1.Pod
	var xErr error
	runX, _, err := p.observe("g-kubelet-x", func() { kubeletX, xErr = p.runPod("kubelet-x", xRef) })
	if err != nil {
		p.c.verdict("g", logsRead(err))
		return
	}
	info("g: kubelet-x status.containerStatuses[0].imageID: %s", imageID(kubeletX))

	p.c.verdict("g",
		cond{wErr == nil, ran("probe-w", wErr)},
		cond{!hasSuffix(runW, "/manifests/"+p.w, 0), "run had no new request for /manifests/W while probe-w started"},
		cond{strings.Contains(wID, p.w), "probe-w's imageID contains W"},
		cond{xErr == nil, ran("kubelet-x", xErr)},
		cond{hasPath(runX, "/v2/team/app/manifests/"+p.x, 200), "run answered /v2/team/app/manifests/X with 200 while kubelet-x started"},
	)
}

// ran states that the pod name ran, and why not when err says it did not.
func ran(name string, err error) string {
	if err != nil {
		return name + " ran (it did not: " + oneLine(err.Error()) + ")"
	}
	return name + " ran"
}

// imageID returns status.containerStatuses[0].imageID of pod, or "" when it
// has none.
func imageID(pod *corev1.Pod) string {
	if pod == nil || len(pod.Status.ContainerStatuses) == 0 {
		return ""
	}
	return pod.Status.ContainerStatuses[0].ImageID
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

// runPod creates the pod name in the namespace p01, on the worker, with one
// container app of image, and waits at most 2 minutes until it runs. It
// returns the pod as last read.
func (p *proof) runPod(name, image string) (*corev1.Pod, error) {
	pods := p.l.Client().CoreV1().Pods(namespace)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: corev1.PodSpec{
			NodeName:      p.worker,
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:            "app",
				Image:           image,
				ImagePullPolicy: corev1.PullIfNotPresent,
			}},
		},
	}
	start := time.Now()
	if _, err := pods.Create(p.ctx, pod, metav1.CreateOptions{}); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(p.ctx, 2*time.Minute)
	defer cancel()
	var last *corev1.Pod
	for {
		got, err := pods.Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			last = got
			if got.Status.Phase == corev1.PodRunning && len(got.Status.ContainerStatuses) > 0 && got.Status.ContainerStatuses[0].State.Running != nil {
				info("g: %s runs, %v after it was created", name, time.Since(start).Round(time.Millisecond))
				return got, nil
			}
			if got.Status.Phase == corev1.PodFailed || got.Status.Phase == corev1.PodSucceeded {
				return got, fmt.Errorf("pod %s ended in phase %s: %s", name, got.Status.Phase, describe(got))
			}
		}
		select {
		case <-ctx.Done():
			return last, fmt.Errorf("pod %s did not run within 2 minutes: %w; last seen: %s", name, ctx.Err(), describe(last))
		case <-time.After(time.Second):
		}
	}
}

// describe gives a pod's phase and, when its container waits or ended, why.
func describe(pod *corev1.Pod) string {
	if pod == nil {
		return "never read"
	}
	s := "phase " + string(pod.Status.Phase)
	if len(pod.Status.ContainerStatuses) > 0 {
		st := pod.Status.ContainerStatuses[0].State
		switch {
		case st.Waiting != nil:
			s += ", waiting: " + st.Waiting.Reason + " " + st.Waiting.Message
		case st.Terminated != nil:
			s += ", terminated: " + st.Terminated.Reason + " " + st.Terminated.Message
		}
	}
	return s
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
		fmt.Println("p01: all checks passed")
		return 0
	}
	fmt.Printf("p01: %d checks failed\n", c.failed)
	return 1
}
