package report

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the golden files that the code produces")

var t0 = time.Date(2026, 11, 13, 9, 59, 52, 0, time.UTC)

const (
	ordersDigest  = "sha256:4f0ddb145f76c84c30f552c4e642ff87f49aaa01f83b09f89e34144d2af22d52"
	kindNodeImage = "kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5"
)

// at is t0 plus ms milliseconds, as the record holds a time.
func at(ms int) *time.Time { t := t0.Add(time.Duration(ms) * time.Millisecond); return &t }

// stopMs is a quickest stop as the record holds it.
func stopMs(ms int64) *int64 { return &ms }

func noEnv(string) string { return "" }

// golden compares got with the file at path; -update rewrites the file first.
func golden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sameAsFile(t, path, got)
}

// sameAsFile compares got with the file at path and never rewrites it.
func sameAsFile(t *testing.T, path string, got []byte) {
	t.Helper()
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs\n--- got\n%s--- want\n%s", path, got, want)
	}
}

// write makes the file at path, and its folders, with text in it.
func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// names lists the entries of a folder.
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func ordersReplaced(quickest int64) Workload {
	return Workload{Workload: "deployment/orders", PodsAtStart: 2, PodsReplaced: 2, PodsCreated: 2, Probed: "yes", QuickestStopMs: stopMs(quickest)}
}

// defaultSettings is what the configuration's Settings gives for the
// examples' file, without the probe's three.
func defaultSettings() []Setting {
	return []Setting{
		{Key: "cluster.node-image", Value: "the linked kind release's", Default: "the linked kind release's"},
		{Key: "cluster.registry-image", Value: "the tool's pinned registry image", Default: "the tool's pinned registry image"},
		{Key: "cluster.workers", Value: "3", Default: "3"},
		{Key: "limits.steady", Value: "10s", Default: "10s"},
		{Key: "limits.settle", Value: "15s", Default: "15s"},
		{Key: "limits.timeout", Value: "5m", Default: "5m"},
		{Key: "limits.silence", Value: "5s", Default: "5s"},
	}
}

// passRun is the record of section 10's run that passed.
func passRun() Run {
	return Run{
		Schema: 1, Final: true, Verdict: "pass", Driver: "helm", Name: "orders",
		Windows: []Window{
			{Name: "steady", Start: at(0), End: at(10_000), Result: "pass", Checks: 2000, SlowestMs: 41, LagMs: 6},
			{Name: "deploy", Start: at(10_000), End: at(43_400), Result: "pass", Checks: 6680, SlowestMs: 212, LagMs: 38},
			{Name: "rollback", Start: at(43_400), End: at(77_300), Result: "pass", Checks: 6780, SlowestMs: 187, LagMs: 35},
		},
		Transitions: []Transition{
			{Name: "deploy", Command: "ok", Workloads: []Workload{ordersReplaced(5300)}},
			{Name: "rollback", Command: "ok", Workloads: []Workload{ordersReplaced(5200)}},
		},
		Probes: []Probe{{
			Kind:   "http",
			Target: "GET http://orders.orders.svc/api/orders/latest",
			Settings: []Setting{
				{Key: "probe.http.rate", Value: "200", Default: "200"},
				{Key: "probe.http.timeout", Value: "2s", Default: "2s"},
				{Key: "probe.http.expect-status", Value: "200-299", Default: "200-299"},
			},
			Lines: 78, CertificateChecked: true, StoppedWhenAsked: true,
		}},
		Releases: []Release{
			{Role: "previous", Chart: "orders", Version: "1.4.2", Source: "oci://registry.example/charts/orders"},
			{Role: "candidate", Chart: "orders", Version: "1.5.0", Source: "./chart", InlineKeys: []string{"image"}},
		},
		Settings: defaultSettings(),
		Images:   []Image{{Name: "orders", Digest: ordersDigest, RunBy: []RunBy{{Workload: "deployment/orders", Transition: "deploy"}}}},
		Tool:     Tool{Version: "dev"},
		Cluster:  &Cluster{Provider: "kind", ProviderVersion: "v0.33.0", Kubernetes: "v1.37.0", NodeImage: kindNodeImage, Workers: 3},
		Out:      "/tmp/rollproof-results-7f3a",
	}
}

func TestFirst(t *testing.T) {
	got, err := encode(First())
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "testdata/result-first.golden.json", got)
}

func TestGoldenPass(t *testing.T) {
	got, err := encode(passRun())
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "testdata/result-pass.golden.json", got)
}

func TestOpenWritesTheFirstFile(t *testing.T) {
	out := t.TempDir()
	w, err := Open(out, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if w.Dir() != out {
		t.Errorf("Dir() = %q, want %q", w.Dir(), out)
	}
	got, err := os.ReadFile(filepath.Join(out, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	sameAsFile(t, "testdata/result-first.golden.json", got)
	if n := names(t, filepath.Join(out, "evidence")); len(n) != 0 {
		t.Errorf("evidence holds %v, want nothing", n)
	}
}

func TestOpenReplacesAnEarlierRun(t *testing.T) {
	out := t.TempDir()
	write(t, filepath.Join(out, "result.json"), `{"schema":1,"final":true,"verdict":"pass"}`)
	write(t, filepath.Join(out, "evidence", "probe.log"), "old\n")
	write(t, filepath.Join(out, "notes.txt"), "mine\n")
	if _, err := Open(out, noEnv); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	sameAsFile(t, "testdata/result-first.golden.json", got)
	if n := names(t, filepath.Join(out, "evidence")); len(n) != 0 {
		t.Errorf("evidence holds %v, want nothing", n)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "notes.txt")); string(b) != "mine\n" {
		t.Errorf("notes.txt = %q, want it untouched", b)
	}
}

func TestOpenDefaultFolder(t *testing.T) {
	runner := t.TempDir()
	actions := func(k string) string {
		switch k {
		case "GITHUB_ACTIONS":
			return "true"
		case "RUNNER_TEMP":
			return runner
		}
		return ""
	}
	a, err := Open("", actions)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open("", actions)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []*Writer{a, b} {
		if filepath.Dir(w.Dir()) != runner || !strings.HasPrefix(filepath.Base(w.Dir()), "rollproof-results-") {
			t.Errorf("Dir() = %q, want a rollproof-results- folder under %q", w.Dir(), runner)
		}
	}
	if a.Dir() == b.Dir() {
		t.Errorf("two runs share %q", a.Dir())
	}

	system := t.TempDir()
	t.Setenv("TMPDIR", system)
	c, err := Open("", noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(c.Dir()) != system {
		t.Errorf("without GitHub Actions Dir() = %q, want a folder under %q", c.Dir(), system)
	}
}

func TestOpenFails(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a-file")
	write(t, file, "x")
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o700) })
	for _, c := range []struct {
		name, out string
		notAsRoot bool
	}{
		{"out names a file", file, false},
		{"out under a folder the tool cannot write", filepath.Join(locked, "results"), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.notAsRoot && os.Geteuid() == 0 {
				t.Skip("root writes into any folder")
			}
			_, err := Open(c.out, noEnv)
			if err == nil || !strings.Contains(err.Error(), c.out) {
				t.Errorf("Open(%q) = %v, want an error that names the path", c.out, err)
			}
		})
	}
}

func TestFinalIsOneStep(t *testing.T) {
	out := t.TempDir()
	w, err := Open(out, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	r := passRun()
	r.Final = false
	read := func() Run {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(out, "result.json"))
		if err != nil {
			t.Fatal(err)
		}
		var got Run
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("result.json does not parse: %v", err)
		}
		return got
	}
	if err := w.Final(r); err != nil {
		t.Fatal(err)
	}
	if got := read(); !got.Final || got.Verdict != "pass" {
		t.Errorf("final %v, verdict %q; want true, pass", got.Final, got.Verdict)
	}
	r.Warnings = []string{"deleting the cluster: it was already gone"}
	if err := w.Final(r); err != nil {
		t.Fatal(err)
	}
	if got := read(); !slices.Equal(got.Warnings, r.Warnings) {
		t.Errorf("warnings %v, want %v", got.Warnings, r.Warnings)
	}
	if n := names(t, out); !slices.Equal(n, []string{"evidence", "result.json"}) {
		t.Errorf("the folder holds %v, want only evidence and result.json", n)
	}
}

// nulls collects the keys whose value is null, at any depth.
func nulls(v any, key string, found *[]string) {
	switch x := v.(type) {
	case nil:
		*found = append(*found, key)
	case map[string]any:
		for k, e := range x {
			nulls(e, k, found)
		}
	case []any:
		for _, e := range x {
			nulls(e, key, found)
		}
	}
}

func TestNoNulls(t *testing.T) {
	timesAndCluster := []string{"cluster", "end", "first_failure", "quickest_stop_ms", "start"}
	for _, r := range []Run{
		{},
		{
			Reasons:     []Reason{{Code: "internal-error"}},
			Windows:     []Window{{Name: "steady"}},
			Transitions: []Transition{{Name: "deploy", Workloads: []Workload{{Workload: "deployment/orders"}}}},
			Probes:      []Probe{{Kind: "http"}},
			Releases:    []Release{{Role: "candidate"}},
			Images:      []Image{{Name: "orders"}},
		},
	} {
		b, err := encode(r)
		if err != nil {
			t.Fatal(err)
		}
		var v any
		if err := json.Unmarshal(b, &v); err != nil {
			t.Fatal(err)
		}
		var found []string
		nulls(v, "", &found)
		for _, k := range found {
			if !slices.Contains(timesAndCluster, k) {
				t.Errorf("%s is null in\n%s", k, b)
			}
		}
		if !strings.Contains(string(b), `"schema": 1,`) {
			t.Errorf("schema is not 1 in\n%s", b)
		}
		if len(r.Windows) > 0 && !strings.Contains(string(b), `"failed_by_class": {}`) {
			t.Errorf("failed_by_class is not {} in\n%s", b)
		}
	}
}

func TestEvidenceNames(t *testing.T) {
	w, err := Open(t.TempDir(), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"commands/install", "commands/install-2", "commands/install-3"} {
		f, err := w.Evidence("commands/install")
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		if _, err := os.Stat(filepath.Join(w.Dir(), "evidence", want)); err != nil {
			t.Errorf("no file %s: %v", want, err)
		}
	}
	for _, bad := range []string{"", "../escape", "commands/../../escape", "/etc/escape"} {
		if f, err := w.Evidence(bad); err == nil {
			f.Close()
			t.Errorf("Evidence(%q) was allowed", bad)
		}
	}
}

// listKeys are the keys of result.json whose value is a list. Each one must be
// written as [] when the record's list is nil.
var listKeys = []string{
	"reasons", "windows", "transitions", "probes", "releases", "settings", "images",
	"removed_variables", "warnings", "detail", "settle_restarted_by", "workloads",
	"headers", "env_names", "notes", "values_files", "inline_keys", "run_by",
}

// lists collects, for each key, how many times its value was a list and how
// many times it was not, at any depth.
func lists(v any, key string, isList, notList map[string]int) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			lists(e, k, isList, notList)
		}
	case []any:
		isList[key]++
		for _, e := range x {
			lists(e, key, isList, notList)
		}
	default:
		notList[key]++
	}
}

func TestNilListsAreWrittenAsEmptyLists(t *testing.T) {
	// Every list of the record is nil: each struct has only its name set.
	r := Run{
		Reasons:     []Reason{{Code: "internal-error"}},
		Windows:     []Window{{Name: "steady"}},
		Transitions: []Transition{{Name: "deploy", Workloads: nil}, {Name: "rollback", Workloads: []Workload{{Workload: "deployment/orders"}}}},
		Probes:      []Probe{{Kind: "http"}},
		Releases:    []Release{{Role: "candidate"}},
		Images:      []Image{{Name: "orders"}},
	}
	b, err := encode(r)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	isList, notList := map[string]int{}, map[string]int{}
	lists(v, "", isList, notList)
	for _, k := range listKeys {
		if isList[k] == 0 {
			t.Errorf("%q is never written as a list in\n%s", k, b)
		}
		if notList[k] != 0 {
			t.Errorf("%q is written as a non-list %d times in\n%s", k, notList[k], b)
		}
	}
	// The caller's record keeps its nil lists.
	if r.Reasons[0].Detail != nil || r.Warnings != nil || r.Transitions[0].Workloads != nil {
		t.Errorf("encode changed the caller's record: %+v", r)
	}
}

func TestEvidenceNameRefused(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "out")
	w, err := Open(out, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	before := tree(t, root)
	for _, bad := range []string{"../x", "a/../../x", "/etc/x", "x\x00y", "evidence/../../x", "a/..", ".."} {
		f, err := w.Evidence(bad)
		if err == nil {
			f.Close()
			t.Errorf("Evidence(%q) was allowed", bad)
		}
	}
	if after := tree(t, root); !slices.Equal(before, after) {
		t.Errorf("a refused name changed the folder:\nbefore %v\nafter  %v", before, after)
	}
}

// tree lists every path under dir, with its type.
func tree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out = append(out, rel+" "+d.Type().String())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestEvidenceNameMayHoldFolders(t *testing.T) {
	w, err := Open(t.TempDir(), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	f, err := w.Evidence("probe/pod/log.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if b, err := os.ReadFile(filepath.Join(w.Dir(), "evidence", "probe", "pod", "log.txt")); err != nil || string(b) != "x" {
		t.Errorf("file = %q, %v", b, err)
	}
}

func TestOpenTouchesNothingElse(t *testing.T) {
	out := t.TempDir()
	write(t, filepath.Join(out, "notes.txt"), "mine\n")
	write(t, filepath.Join(out, "sub", "deep.txt"), "deep\n")
	write(t, filepath.Join(out, "evidence.bak"), "kept\n")
	write(t, filepath.Join(out, ".result-keep.json"), "kept\n")
	if _, err := Open(out, noEnv); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]string{"notes.txt": "mine\n", "sub/deep.txt": "deep\n", "evidence.bak": "kept\n", ".result-keep.json": "kept\n"} {
		if b, err := os.ReadFile(filepath.Join(out, p)); err != nil || string(b) != want {
			t.Errorf("%s = %q, %v; want it untouched", p, b, err)
		}
	}
}

func TestOpenRefusesWhatIsNotOurs(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, out, elsewhere string)
	}{
		{"evidence is a link", func(t *testing.T, out, elsewhere string) {
			if err := os.Symlink(elsewhere, filepath.Join(out, "evidence")); err != nil {
				t.Fatal(err)
			}
		}},
		{"evidence is a file", func(t *testing.T, out, elsewhere string) {
			write(t, filepath.Join(out, "evidence"), "x")
		}},
		{"result.json is a folder", func(t *testing.T, out, elsewhere string) {
			write(t, filepath.Join(out, "result.json", "inside"), "x")
		}},
		{"result.json is a link", func(t *testing.T, out, elsewhere string) {
			if err := os.Symlink(filepath.Join(elsewhere, "target.json"), filepath.Join(out, "result.json")); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, elsewhere := t.TempDir(), t.TempDir()
			write(t, filepath.Join(elsewhere, "target.json"), "target\n")
			write(t, filepath.Join(elsewhere, "keep.txt"), "keep\n")
			c.setup(t, out, elsewhere)
			before := tree(t, out)
			_, err := Open(out, noEnv)
			if err == nil || !strings.Contains(err.Error(), out) {
				t.Errorf("Open = %v, want an error that names %q", err, out)
			}
			if after := tree(t, out); !slices.Equal(before, after) {
				t.Errorf("the folder changed:\nbefore %v\nafter  %v", before, after)
			}
			for p, want := range map[string]string{"target.json": "target\n", "keep.txt": "keep\n"} {
				if b, _ := os.ReadFile(filepath.Join(elsewhere, p)); string(b) != want {
					t.Errorf("%s = %q, want it untouched", p, b)
				}
			}
		})
	}
}

func TestFinalFailsWhole(t *testing.T) {
	t.Run("rename fails", func(t *testing.T) {
		out := t.TempDir()
		w, err := Open(out, noEnv)
		if err != nil {
			t.Fatal(err)
		}
		// A folder that holds a file cannot be replaced by a rename.
		if err := os.Remove(filepath.Join(out, "result.json")); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(out, "result.json", "inside"), "x")
		if err := w.Final(passRun()); err == nil || !strings.Contains(err.Error(), out) {
			t.Errorf("Final = %v, want an error that names %q", err, out)
		}
		if n := names(t, out); !slices.Equal(n, []string{"evidence", "result.json"}) {
			t.Errorf("the folder holds %v, want no temporary file", n)
		}
	})
	t.Run("temporary file cannot be made", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root writes into any folder")
		}
		out := t.TempDir()
		w, err := Open(out, noEnv)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(out, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(out, 0o700) })
		if err := w.Final(passRun()); err == nil {
			t.Error("Final succeeded in a folder that cannot be written")
		}
		got, err := os.ReadFile(filepath.Join(out, "result.json"))
		if err != nil {
			t.Fatal(err)
		}
		sameAsFile(t, "testdata/result-first.golden.json", got)
	})
}

func TestModes(t *testing.T) {
	w, err := Open(t.TempDir(), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	f, err := w.Evidence("a/log")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	// The brief gives folders 0755 and evidence files 0644; the process umask
	// may remove bits, never add them. result.json is written through a
	// temporary file and keeps its 0600.
	for p, max := range map[string]os.FileMode{"evidence": 0o755, "evidence/a": 0o755, "evidence/a/log": 0o644} {
		fi, err := os.Stat(filepath.Join(w.Dir(), p))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm()&^max != 0 {
			t.Errorf("%s has mode %v, more than %v", p, fi.Mode().Perm(), max)
		}
	}
	if fi, err := os.Stat(filepath.Join(w.Dir(), "result.json")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("result.json mode = %v, %v; want 0600", fi.Mode().Perm(), err)
	}
}
