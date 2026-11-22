package report

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// delimiterFrom replaces newDelimiter, for one test, by one that gives the
// delimiters in order, and restores the real one when the test ends. A call the
// test does not name fails rather than hanging on a delimiter the value holds.
func delimiterFrom(t *testing.T, delimiters ...string) {
	t.Helper()
	real := newDelimiter
	i := 0
	newDelimiter = func() string {
		if i == len(delimiters) {
			t.Fatalf("newDelimiter was called %d times; the test names %d", i+1, len(delimiters))
		}
		d := delimiters[i]
		i++
		return d
	}
	t.Cleanup(func() { newDelimiter = real })
}

func TestGitHubActions(t *testing.T) {
	for _, tc := range []struct {
		name  string
		set   bool
		value string
		want  bool
	}{
		{`"true"`, true, "true", true},
		{"unset", false, "", false},
		{`""`, true, "", false},
		{`"false"`, true, "false", false},
		{`"TRUE"`, true, "TRUE", false},
		{`"1"`, true, "1", false},
		{`" true"`, true, " true", false},
		{`"true\n"`, true, "true\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(key string) string {
				if key == "GITHUB_ACTIONS" && tc.set {
					return tc.value
				}
				return ""
			}
			if got := GitHubActions(getenv); got != tc.want {
				t.Errorf("GitHubActions with GITHUB_ACTIONS %q (set: %v) is %v, want %v", tc.value, tc.set, got, tc.want)
			}
		})
	}
}

func TestGitHubFrom(t *testing.T) {
	const written = "verdict<<D1\npass\nD1\n"
	for _, tc := range []struct {
		name string
		env  map[string]string
		out  string
		sum  string
	}{
		{"no variables", map[string]string{}, "", ""},
		{"GITHUB_ACTIONS is false", map[string]string{"GITHUB_ACTIONS": "false", "GITHUB_OUTPUT": "OUT", "GITHUB_STEP_SUMMARY": "SUM"}, "", ""},
		{"GITHUB_ACTIONS is TRUE", map[string]string{"GITHUB_ACTIONS": "TRUE", "GITHUB_OUTPUT": "OUT", "GITHUB_STEP_SUMMARY": "SUM"}, "", ""},
		{"no GITHUB_ACTIONS", map[string]string{"GITHUB_OUTPUT": "OUT", "GITHUB_STEP_SUMMARY": "SUM"}, "", ""},
		{"an outputs file alone", map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_OUTPUT": "OUT"}, written, ""},
		{"a summary file alone", map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_STEP_SUMMARY": "SUM"}, "", "### s\n"},
		{"both files", map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_OUTPUT": "OUT", "GITHUB_STEP_SUMMARY": "SUM"}, written, "### s\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			delimiterFrom(t, "D1")
			dir := t.TempDir()
			out, sum := filepath.Join(dir, "out"), filepath.Join(dir, "sum")
			write(t, out, "")
			write(t, sum, "")
			getenv := func(key string) string {
				switch tc.env[key] {
				case "OUT":
					return out
				case "SUM":
					return sum
				}
				return tc.env[key]
			}

			g := GitHubFrom(getenv)
			if err := g.WriteOutputs(Output{"verdict", "pass"}); err != nil {
				t.Fatalf("WriteOutputs: %v", err)
			}
			if err := g.AppendSummary("### s\n"); err != nil {
				t.Fatalf("AppendSummary: %v", err)
			}
			if b, err := os.ReadFile(out); err != nil || string(b) != tc.out {
				t.Errorf("the outputs file is %q, %v; want %q", b, err, tc.out)
			}
			if b, err := os.ReadFile(sum); err != nil || string(b) != tc.sum {
				t.Errorf("the summary file is %q, %v; want %q", b, err, tc.sum)
			}
		})
	}
}

func TestOutputsInTheDelimiterForm(t *testing.T) {
	delimiterFrom(t, "D1", "D2", "D3")
	path := filepath.Join(t.TempDir(), "outputs")
	write(t, path, "")
	g := GitHub{outputs: path}

	if err := g.WriteOutputs(Output{"out", "/tmp/r"}, Output{"verdict", "not-verified"}); err != nil {
		t.Fatal(err)
	}
	want := "out<<D1\n/tmp/r\nD1\nverdict<<D2\nnot-verified\nD2\n"
	if b, err := os.ReadFile(path); err != nil || string(b) != want {
		t.Errorf("the outputs file is %q, %v; want %q", b, err, want)
	}

	if err := g.WriteOutputs(Output{"verdict", "pass"}); err != nil {
		t.Fatal(err)
	}
	want += "verdict<<D3\npass\nD3\n"
	if b, err := os.ReadFile(path); err != nil || string(b) != want {
		t.Errorf("the outputs file is %q, %v; want %q", b, err, want)
	}
}

func TestADelimiterThatTheValueHoldsIsDrawnAgain(t *testing.T) {
	delimiterFrom(t, "EOF", "EOF2")
	path := filepath.Join(t.TempDir(), "outputs")
	write(t, path, "")
	g := GitHub{outputs: path}

	if err := g.WriteOutputs(Output{"x", "a\nEOF\nb"}); err != nil {
		t.Fatal(err)
	}
	want := "x<<EOF2\na\nEOF\nb\nEOF2\n"
	if b, err := os.ReadFile(path); err != nil || string(b) != want {
		t.Errorf("the outputs file is %q, %v; want %q", b, err, want)
	}
}

func TestTheRealDelimiter(t *testing.T) {
	want := regexp.MustCompile(`^ROLLPROOF_EOF_[A-Z2-7]{26}$`)
	a, b := newDelimiter(), newDelimiter()
	if !want.MatchString(a) {
		t.Errorf("newDelimiter() is %q, want ROLLPROOF_EOF_ and 26 characters of the base32 alphabet", a)
	}
	if !want.MatchString(b) {
		t.Errorf("newDelimiter() is %q, want ROLLPROOF_EOF_ and 26 characters of the base32 alphabet", b)
	}
	if a == b {
		t.Errorf("two calls of newDelimiter() both gave %q", a)
	}
}

func TestGitHubsFilesAreNeverMade(t *testing.T) {
	dir := t.TempDir()
	out, sum := filepath.Join(dir, "missing-out"), filepath.Join(dir, "missing-sum")
	g := GitHub{outputs: out, summary: sum}

	if err := g.WriteOutputs(Output{"verdict", "pass"}); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("WriteOutputs: %v, want an error that is fs.ErrNotExist", err)
	}
	if err := g.AppendSummary("s\n"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("AppendSummary: %v, want an error that is fs.ErrNotExist", err)
	}
	for _, path := range []string{out, sum} {
		if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s exists after the writes; the file is never made", path)
		}
	}
}

func TestAppendSummaryAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary")
	write(t, path, "earlier step\n")
	g := GitHub{summary: path}

	if err := g.AppendSummary("### rollproof: PASS\n"); err != nil {
		t.Fatal(err)
	}
	want := "earlier step\n### rollproof: PASS\n"
	if b, err := os.ReadFile(path); err != nil || string(b) != want {
		t.Errorf("the summary file is %q, %v; want %q", b, err, want)
	}
}

func TestAZeroGitHubWritesNothing(t *testing.T) {
	var g GitHub
	if err := g.WriteOutputs(Output{"verdict", "pass"}); err != nil {
		t.Errorf("GitHub{}.WriteOutputs: %v", err)
	}
	if err := g.AppendSummary("x"); err != nil {
		t.Errorf("GitHub{}.AppendSummary: %v", err)
	}
}
