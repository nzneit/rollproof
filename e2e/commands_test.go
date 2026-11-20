//go:build e2e

package e2e

import (
	"slices"
	"strings"
	"testing"
)

// The commands driver's rows apply, with `kubectl apply -f` and no `--namespace`, the manifests
// that runFixture rendered from the fixture chart with `helm template`. The probe's URL names
// orders.orders.svc, so a pass shows that such a command acted in deploy.namespace (section 7,
// "The namespace"; decision 18): the target check refuses a host under .svc whose Service does
// not exist, and an install that put nothing in orders would find nothing to watch.

// checkCommandsRecord checks the record of a run through the commands driver.
//
// Fails the test unless rec's driver is commands, its name is orders, its
// releases are the given roles in order, each with the program kubectl, and
// the images previous and candidate carry the digests of their archives.
func checkCommandsRecord(t *testing.T, rec record, roles ...string) {
	t.Helper()
	if rec.Driver != "commands" || rec.Name != "orders" {
		t.Errorf("driver %q, name %q; want commands and orders", rec.Driver, rec.Name)
	}
	var got []string
	for _, rel := range rec.Releases {
		got = append(got, rel.Role)
		if rel.Program != "kubectl" {
			t.Errorf("the release %s has the program %q, want kubectl", rel.Role, rel.Program)
		}
	}
	if !slices.Equal(got, roles) {
		t.Errorf("releases %+v; want the roles %v in order", rec.Releases, roles)
	}
	for _, entry := range []string{"previous", "candidate"} {
		want := archiveDigests[entry]
		if want == "" {
			t.Fatalf("TestMain recorded no digest for the archive of %s", entry)
		}
		found := false
		for _, im := range rec.Images {
			if im.Name != entry {
				continue
			}
			found = true
			if im.Digest != want {
				t.Errorf("the image %s has the digest %q, and its archive %s; the harness believes that the tool stores an archive under the digest tarball.ImageFromPath gives",
					entry, im.Digest, want)
			}
		}
		if !found {
			t.Errorf("result.json has no image %s: %+v", entry, rec.Images)
		}
	}
}

// Row 10. A setup command fails: not verified. The run ends before the install window, so
// result.json lists no window.
func TestSetupCommandFails(t *testing.T) {
	f := fixture{name: "setup-command-fails", setup: [][]string{{"kubectl", "get", "configmap", "rollproof-fixture-absent"}}}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 2, verdict: "not-verified", codes: []string{"setup-failed"}, decided: "setup-failed"})
		checkSetupOutput(t, `configmaps "rollproof-fixture-absent" not found`)
	})
}

// Row 21. The first fixture through the commands driver, with all three commands: pass.
func TestCommandsDriverWithAllThreeCommands(t *testing.T) {
	f := fixture{name: "commands-driver-with-all-three-commands", commands: []string{"install", "deploy", "rollback"}}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass", windows: allPass})
		rec := readRecord(t, outDir(t))
		checkCommandsRecord(t, rec, "install", "deploy", "rollback")
		for _, name := range []string{"deploy", "rollback"} {
			if w := rec.workload(t, name, "deployment/orders"); w.PodsReplaced != 2 || w.Probed != "yes" {
				t.Errorf("%s: deployment/orders replaced %d, probed %q; want 2 and yes", name, w.PodsReplaced, w.Probed)
			}
		}
	})
}

// Row 22. The same with install and deploy only: pass, reported as a deploy with no rollback.
func TestCommandsDriverWithInstallAndDeployOnly(t *testing.T) {
	f := fixture{name: "commands-driver-with-install-and-deploy-only", commands: []string{"install", "deploy"}}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass", windows: map[string]string{"steady": "pass", "deploy": "pass"}})
		rec := readRecord(t, outDir(t))
		checkCommandsRecord(t, rec, "install", "deploy")
		if len(rec.Transitions) != 1 || rec.Transitions[0].Name != "deploy" {
			t.Errorf("transitions %+v; want the deploy alone", rec.Transitions)
		}
		want := "  the candidate's shutdown and the installed release's start-up, because the run has no rollback\n"
		if c := readConsole(t); !strings.Contains(c, want) {
			t.Errorf("the console does not hold %q:\n%s", want, c)
		}
	})
}

// Row 29. A setup command makes the probe's namespace enforce the restricted level: pass.
func TestSetupMakesTheProbeNamespaceRestricted(t *testing.T) {
	f := fixture{name: "setup-makes-the-probe-namespace-restricted", setup: restrictProbeNamespace}
	underBothHelms(t, f, func(t *testing.T, r result) {
		expect(t, r, result{exit: 0, verdict: "pass", windows: allPass})
		checkSetupOutput(t, "namespace/rollproof-probe labeled")
		checkProbePodRestricted(t)
	})
}
