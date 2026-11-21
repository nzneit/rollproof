//go:build mirror

package e2e

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The mirror job of section 12: two runs of the real binary through a
// mirror that needs a login, sits under a path and serves TLS from a
// private authority, with the nodes' route to public registries cut, each
// ending with the search for the password in everything it wrote.
func TestMirror(t *testing.T) {
	buildImage(standinImage, "./fixtures/standin", "-s -w", "standin")
	certs := t.TempDir()
	ca, err := newAuthority(certs)
	if err != nil {
		t.Fatal(err)
	}
	pw, err := newPassword()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(passwordVar, pw)
	holdRunImages(t)
	in := mirrorInputs{engine: engine, certs: certs, archives: filepath.Dir(archives["previous"]), ca: ca,
		previousDigest: archiveDigests["previous"], candidateDigest: archiveDigests["candidate"]}
	forms := passwordForms(jobUser, pw)

	t.Run("through the mirror", func(t *testing.T) {
		clearStandins(t)
		r := runFixture(t, throughTheMirror(in))
		expect(t, r, result{exit: 0, verdict: "pass",
			windows: map[string]string{"steady": "pass", "deploy": "pass", "rollback": "pass"}})

		mirrorLog := standinLogOf(t, mirrorName)
		_, reqs := parseStandinLog(mirrorLog)
		previous := "/docker-io/v2/rollproof/fixture/manifests/" + in.previousDigest
		if !hasRequest(reqs, previous, jobUser, 200) {
			t.Errorf("the mirror logged no request for %s by %s answered 200; its requests: %+v", previous, jobUser, reqs)
		}
		for _, req := range reqs {
			if strings.Contains(req.path, in.candidateDigest) {
				t.Errorf("the mirror was asked for %s, which holds the candidate's digest that the run's registry served", req.path)
			}
		}
		checkOnlyLoopback(t, realName, standinLogOf(t, realName))
		checkPasswordGone(t, forms)
	})

	t.Run("a mirror replaces its host", func(t *testing.T) {
		clearStandins(t)
		r := runFixture(t, mirrorReplacesItsHost(in))
		expect(t, r, result{exit: 1, verdict: "fail", codes: []string{"transition-stalled"},
			decided: "transition-stalled",
			windows: map[string]string{"steady": "pass", "deploy": "fail", "rollback": "not-run"}})

		note := "only from its mirror " + mirrorHost + "/real"
		named := false
		for _, reason := range readInstall(t, outDir(t)).Reasons {
			if reason.Code != "transition-stalled" {
				continue
			}
			for _, line := range reason.Detail {
				named = named || strings.Contains(line, note)
			}
		}
		if !named {
			t.Errorf("no detail line of the transition-stalled reason holds %q", note)
		}

		_, reqs := parseStandinLog(standinLogOf(t, mirrorName))
		candidate := "/real/v2/team/candidate/manifests/" + in.candidateDigest
		if !hasRequest(reqs, candidate, jobUser, 404) {
			t.Errorf("the mirror logged no request for %s by %s answered 404; its requests: %+v", candidate, jobUser, reqs)
		}
		realLog := standinLogOf(t, realName)
		if load := "LOAD team/candidate@" + in.candidateDigest; !strings.Contains(realLog, load) {
			t.Errorf("the real host's log does not hold %q: it did not hold the candidate", load)
		}
		checkOnlyLoopback(t, realName, realLog)
		checkPasswordGone(t, forms)
	})
}

// holdRunImages makes sure that the engine holds the node image and the registry image that
// bin/rollproof version names on its lines "node image REF" and "registry image REF": each is
// pulled now when `E image inspect REF` fails, so that the run asks the engine for no pull: the
// job cuts the nodes' route to public registries and not the engine's (decision 18). A version
// command that fails, a line that is missing, or a pull that fails, fails the test.
func holdRunImages(t *testing.T) {
	t.Helper()
	out, err := exec.Command(filepath.Join(root, "bin", "rollproof"), "version").CombinedOutput()
	if err != nil {
		t.Fatalf("bin/rollproof version: %v\n%s", err, out)
	}
	var refs []string
	for _, prefix := range []string{"node image ", "registry image "} {
		ref := ""
		for _, line := range strings.Split(string(out), "\n") {
			if r, ok := strings.CutPrefix(line, prefix); ok {
				ref = r
			}
		}
		if ref == "" {
			t.Fatalf("bin/rollproof version printed no line that begins %q:\n%s", prefix, out)
		}
		refs = append(refs, ref)
	}
	for _, ref := range refs {
		if exec.Command(engine, "image", "inspect", ref).Run() == nil {
			continue
		}
		if out, err := exec.Command(engine, "pull", ref).CombinedOutput(); err != nil {
			t.Fatalf("%s pull %s: %v\n%s", engine, ref, err, out)
		}
	}
}

// clearStandins removes the job's stand-ins before a run and again when the test ends; when the
// test failed, it first logs both stand-ins' logs.
func clearStandins(t *testing.T) {
	t.Helper()
	removeStandins(t)
	t.Cleanup(func() {
		if t.Failed() {
			for _, name := range []string{mirrorName, realName} {
				text, err := standinLog(name)
				if err != nil {
					t.Log(err)
					continue
				}
				t.Logf("the log of %s:\n%s", name, text)
			}
		}
		removeStandins(t)
	})
}

// removeStandins removes, with their volumes, the containers mirrorName and realName, each only
// when `E ps -a` lists it, because rm of a name that is absent fails on both engines. It removes
// nothing else. A failure of either command is a test error.
func removeStandins(t *testing.T) {
	t.Helper()
	out, err := exec.Command(engine, "ps", "-a", "--format", "{{.Names}}").CombinedOutput()
	if err != nil {
		t.Errorf("%s ps -a: %v\n%s", engine, err, out)
		return
	}
	for _, name := range strings.Fields(string(out)) {
		if name != mirrorName && name != realName {
			continue
		}
		if out, err := exec.Command(engine, "rm", "-f", "-v", name).CombinedOutput(); err != nil {
			t.Errorf("%s rm -f -v %s: %v\n%s", engine, name, err, out)
		}
	}
}

// standinLog returns what `E logs name` prints, standard output and standard error together.
func standinLog(name string) (string, error) {
	out, err := exec.Command(engine, "logs", name).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s logs %s: %w\n%s", engine, name, err, out)
	}
	return string(out), nil
}

// standinLogOf returns the log of the stand-in name, and fails the test when the engine cannot
// give it.
func standinLogOf(t *testing.T, name string) string {
	t.Helper()
	text, err := standinLog(name)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

// hasRequest reports whether reqs holds a request for path by user that was answered status.
func hasRequest(reqs []standinRequest, path, user string, status int) bool {
	for _, req := range reqs {
		if req.path == path && req.user == user && req.status == status {
			return true
		}
	}
	return false
}

// checkOnlyLoopback fails the test for each problem that loopbackProblems finds in the stand-in
// name's log: no connection from 127.0.0.1, or one from another address.
func checkOnlyLoopback(t *testing.T, name, text string) {
	t.Helper()
	for _, p := range loopbackProblems(name, text) {
		t.Error(p)
	}
}

// checkPasswordGone fails the test when a file of the test's results folder holds a form of the
// password, or when no file under its evidence/commands holds the mask "***".
func checkPasswordGone(t *testing.T, forms []string) {
	t.Helper()
	found, err := findForms(outDir(t), forms)
	if err != nil {
		t.Fatalf("the search for the password: %v", err)
	}
	if len(found) > 0 {
		t.Errorf("the results folder holds the password: %s", strings.Join(found, "; "))
	}
	masked := false
	commands := filepath.Join(outDir(t), "evidence", "commands")
	err = filepath.WalkDir(commands, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		masked = masked || bytes.Contains(b, []byte("***"))
		return nil
	})
	if err != nil {
		t.Errorf("%s: %v", commands, err)
	}
	if !masked {
		t.Errorf("no file under %s holds ***", commands)
	}
}
