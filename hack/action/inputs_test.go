package action

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"rollproof/internal/config"
	"rollproof/internal/verdict"
)

// releaseVar is the variable that only pass.yaml reads, which the action's own test sets on the
// action's step alone.
const releaseVar = "ROLLPROOF_ACTION_TEST_RELEASE"

// noVariable gives no variable at all; releaseOrders gives releaseVar as orders and nothing else.
func noVariable(string) (string, bool) { return "", false }

func releaseOrders(name string) (string, bool) {
	if name == releaseVar {
		return "orders", true
	}
	return "", false
}

// imageValue is the one inline value of a release in the three files: the image named by the
// ${images…} forms of the entry for that release, which Load leaves for later.
func imageValue(entry string) config.Value {
	return config.Value{Inline: map[string]any{"image": map[string]any{
		"repository": "${images." + entry + ".repository}",
		"digest":     "${images." + entry + ".digest}",
	}}}
}

// checkInputs checks c against what the three files share, with the candidate's values and the
// steady window given.
func checkInputs(t *testing.T, c *config.Config, candidate []config.Value, steady time.Duration) {
	t.Helper()
	chart, err := filepath.Abs("../../fixtures/chart")
	if err != nil {
		t.Fatal(err)
	}
	if c.Deploy.Namespace != "orders" {
		t.Errorf("Deploy.Namespace %q, want orders", c.Deploy.Namespace)
	}
	h := c.Deploy.Helm
	if h == nil {
		t.Fatal("Deploy.Helm is nil")
	}
	if h.Release != "orders" {
		t.Errorf("Deploy.Helm.Release %q, want orders", h.Release)
	}
	want := map[string]struct {
		got    *config.Release
		values []config.Value
	}{
		"previous":  {h.Previous, []config.Value{imageValue("previous")}},
		"candidate": {h.Candidate, candidate},
	}
	for name, w := range want {
		if w.got == nil {
			t.Errorf("Deploy.Helm's %s is nil", name)
			continue
		}
		if !w.got.IsPath || w.got.Chart != chart {
			t.Errorf("%s: IsPath %v, Chart %q; want true, %q", name, w.got.IsPath, w.got.Chart, chart)
		}
		if diff := cmp.Diff(w.values, w.got.Values); diff != "" {
			t.Errorf("%s's values (-want +got):\n%s", name, diff)
		}
	}
	images := map[string]config.Image{
		"previous":  {Local: "rollproof-fixture:previous"},
		"candidate": {Local: "rollproof-fixture:candidate"},
	}
	if diff := cmp.Diff(images, c.Images); diff != "" {
		t.Errorf("Images (-want +got):\n%s", diff)
	}
	if c.Probe.HTTP == nil || c.Probe.HTTP.URL != "http://orders.orders.svc/work?ms=20" {
		t.Errorf("Probe.HTTP %+v, want the URL http://orders.orders.svc/work?ms=20", c.Probe.HTTP)
	}
	limits := config.Limits{Steady: steady, Settle: 15 * time.Second, Timeout: 5 * time.Minute, Silence: 5 * time.Second}
	if c.Limits != limits {
		t.Errorf("Limits %+v, want %+v", c.Limits, limits)
	}
	if c.Cluster.Workers != 3 {
		t.Errorf("Cluster.Workers %d, want 3", c.Cluster.Workers)
	}
}

func TestTheActionTestsInputsLoad(t *testing.T) {
	// config.Load("testdata/pass.yaml", env), env giving ROLLPROOF_ACTION_TEST_RELEASE=orders and
	// nothing else: Deploy.Namespace "orders"; Deploy.Helm.Release "orders"; Previous and Candidate
	// both IsPath with Chart the absolute path of ../../fixtures/chart; each with exactly one value,
	// inline, whose "image" is {"repository": "${images.previous.repository}", "digest":
	// "${images.previous.digest}"} (the candidate's with candidate); Images exactly
	// {"previous": {Local: "rollproof-fixture:previous"}, "candidate": {Local:
	// "rollproof-fixture:candidate"}}; Probe.HTTP.URL "http://orders.orders.svc/work?ms=20";
	// Limits steady 10s, settle 15s, timeout 5m, silence 5s; Cluster.Workers 3.
	t.Run("pass", func(t *testing.T) {
		c, err := config.Load("testdata/pass.yaml", releaseOrders)
		if err != nil {
			t.Fatal(err)
		}
		checkInputs(t, c, []config.Value{imageValue("candidate")}, 10*time.Second)
	})
	// testdata/pass.yaml with no variable: a config-rejected condition whose text names
	// ROLLPROOF_ACTION_TEST_RELEASE.
	t.Run("pass without the variable", func(t *testing.T) {
		_, err := config.Load("testdata/pass.yaml", noVariable)
		var c verdict.Condition
		if !errors.As(err, &c) {
			t.Fatalf("got error %v, want a verdict.Condition", err)
		}
		if c.Code != verdict.ConfigRejected {
			t.Errorf("code %q, want %q", c.Code, verdict.ConfigRejected)
		}
		if !strings.Contains(c.Text, releaseVar) {
			t.Errorf("text %q does not name %s", c.Text, releaseVar)
		}
	})
	// testdata/fail.yaml with no variable: as pass.yaml with the variable, but the candidate has a
	// second value, inline, {"readiness": false, "startDelay": "5s"}.
	t.Run("fail", func(t *testing.T) {
		c, err := config.Load("testdata/fail.yaml", noVariable)
		if err != nil {
			t.Fatal(err)
		}
		checkInputs(t, c, []config.Value{
			imageValue("candidate"),
			{Inline: map[string]any{"readiness": false, "startDelay": "5s"}},
		}, 10*time.Second)
	})
	// testdata/cut.yaml with no variable: as pass.yaml with the variable, but Limits.Steady is 10m.
	t.Run("cut", func(t *testing.T) {
		c, err := config.Load("testdata/cut.yaml", noVariable)
		if err != nil {
			t.Fatal(err)
		}
		checkInputs(t, c, []config.Value{imageValue("candidate")}, 10*time.Minute)
	})
}
