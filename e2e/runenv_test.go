package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// helmVar names the variable from which the harness takes the Helm majors to run:
// 3 or 4 for one, both when it is not set or empty (section 12, "The suite's
// workflow").
const helmVar = "ROLLPROOF_E2E_HELM"

// helmMajors gives the Helm binaries under .tools/ that value selects: helm-3 for
// "3", helm-4 for "4", both, in that order, for "". Any other value is an error
// that names the variable and the values it takes.
func helmMajors(value string) ([]string, error) {
	switch value {
	case "":
		return []string{"helm-3", "helm-4"}, nil
	case "3":
		return []string{"helm-3"}, nil
	case "4":
		return []string{"helm-4"}, nil
	}
	return nil, fmt.Errorf("%s is %q: it must be 3, 4 or not set", helmVar, value)
}

// githubFiles is a run's own files for GitHub's outputs and job summary, in its
// results folder.
type githubFiles struct {
	output, summary string
}

// runEnv is the environment of one run of the tool: base without GITHUB_OUTPUT
// and GITHUB_STEP_SUMMARY, so that the run never writes into the files of the job
// that runs the suite; when gh is not nil, also without GITHUB_ACTIONS, and then
// with GITHUB_ACTIONS=true and the two variables naming gh's files; and, last,
// PATH with helmDir in front of base's own PATH.
func runEnv(base []string, helmDir string, gh *githubFiles) []string {
	drop := []string{"GITHUB_OUTPUT", "GITHUB_STEP_SUMMARY"}
	if gh != nil {
		drop = append(drop, "GITHUB_ACTIONS")
	}
	env := make([]string, 0, len(base)+4)
	path := ""
	for _, entry := range base {
		name, value, _ := strings.Cut(entry, "=")
		if name == "PATH" {
			path = value
		}
		if !slices.Contains(drop, name) {
			env = append(env, entry)
		}
	}
	if gh != nil {
		env = append(env, "GITHUB_ACTIONS=true", "GITHUB_OUTPUT="+gh.output, "GITHUB_STEP_SUMMARY="+gh.summary)
	}
	// An empty last element of PATH would mean the current folder in a command lookup.
	if path != "" {
		path = string(os.PathListSeparator) + path
	}
	// base's own PATH stays in place: os/exec uses the last value of a key that stands twice in
	// Cmd.Env, so this entry is the one the run sees.
	return append(env, "PATH="+helmDir+path)
}

// readGitHubOutputs reads an outputs file in the delimiter form, the only form the
// tool writes, and gives each name's values in the order they were written. A line
// outside a value that is not NAME<<DELIMITER, the plain form NAME=VALUE among
// them, or a delimiter that never comes, is an error that names the file and the
// line.
func readGitHubOutputs(path string) (map[string][]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	outs := map[string][]string{}
	if len(b) == 0 {
		return outs, nil
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if name, delimiter, ok := strings.Cut(line, "<<"); ok && name != "" && delimiter != "" {
			start := i + 1
			var value []string
			closed := false
			for i++; i < len(lines); i++ {
				if lines[i] == delimiter {
					closed = true
					break
				}
				value = append(value, lines[i])
			}
			if !closed {
				return nil, fmt.Errorf("%s line %d: the output %s has no closing delimiter %q", path, start, name, delimiter)
			}
			outs[name] = append(outs[name], strings.Join(value, "\n"))
			continue
		}
		return nil, fmt.Errorf("%s line %d: %q is not NAME<<DELIMITER", path, i+1, line)
	}
	return outs, nil
}

// summaryFaults judges the text of a run's job summary against the results folder
// out and the word of its first line (PASS, FAIL or NOT VERIFIED), and gives the
// faults it finds, none for a good summary: the first line does not begin
// "### rollproof: " + word; more or fewer than one line begins "### rollproof: ";
// the line "results: " + out followed by the line of the fence does not stand
// exactly once. Blocks after the results block, such as a warning's, are no fault.
func summaryFaults(summary, out, word string) []string {
	var faults []string
	first, _, _ := strings.Cut(summary, "\n")
	if heading := "### rollproof: " + word; !strings.HasPrefix(first, heading) {
		faults = append(faults, fmt.Sprintf("the summary's first line is %q, want one that begins %q", first, heading))
	}
	// A leading line end puts the first line, too, at a line's start for the counts.
	lines := "\n" + summary
	if n := strings.Count(lines, "\n### rollproof: "); n != 1 {
		faults = append(faults, fmt.Sprintf("the summary holds %d lines that begin %q, want exactly one", n, "### rollproof: "))
	}
	// The fence is three backticks unless a line of the results block holds a run of three or
	// more backticks; no run that is judged here is expected to hold one.
	if n := strings.Count(lines, "\nresults: "+out+"\n```\n"); n != 1 {
		faults = append(faults, fmt.Sprintf("the summary holds the line %q followed by the fence %d times, want exactly once", "results: "+out, n))
	}
	return faults
}

// stopLine matches the line by which the tool stops GitHub's workflow commands:
// "::stop-commands::" and 26 characters of crypto/rand.Text's alphabet.
var stopLine = regexp.MustCompile(`^::stop-commands::[A-Z2-7]{26}$`)

// envValue gives the value of name in env, the last one when it stands twice, as
// os/exec uses it, and "" when it is not there.
func envValue(env []string, name string) string {
	value := ""
	for _, entry := range env {
		if n, v, ok := strings.Cut(entry, "="); ok && n == name {
			value = v
		}
	}
	return value
}

// workflowCommandFaults judges a run's standard output and standard error against
// the lines for GitHub's workflow commands (design, section 8), and gives the
// faults it finds, none when they hold. onGitHub is whether the run's
// GITHUB_ACTIONS was "true", and bySignal whether a signal ended it. Off GitHub,
// no line of either stream begins "::" after its leading white space, which is
// how the runner reads a line. On GitHub, stdout's first line is
// "::stop-commands::TOKEN" with a token of the tool's form; stderr's first line is
// the same; each stream holds it once; "::TOKEN::" stands on stderr never, and on
// stdout exactly once, as its last line, unless a signal ended the run, when it
// stands nowhere. It stops at a first line of stdout that is not a stop line,
// since the other checks need its token.
func workflowCommandFaults(stdout, stderr string, onGitHub, bySignal bool) []string {
	var faults []string
	outLines, errLines := streamLines(stdout), streamLines(stderr)
	if !onGitHub {
		for _, s := range []struct {
			name  string
			lines []string
		}{{"stdout", outLines}, {"stderr", errLines}} {
			for _, l := range s.lines {
				if strings.HasPrefix(strings.TrimLeftFunc(l, unicode.IsSpace), "::") {
					faults = append(faults, fmt.Sprintf("%s holds the line %q, which begins %q, though GITHUB_ACTIONS is not true", s.name, l, "::"))
					break
				}
			}
		}
		return faults
	}
	first := ""
	if len(outLines) > 0 {
		first = outLines[0]
	}
	if !stopLine.MatchString(first) {
		return append(faults, fmt.Sprintf("stdout's first line is %q, want %q and a token of 26 characters", first, "::stop-commands::"))
	}
	stop := first
	resume := "::" + strings.TrimPrefix(stop, "::stop-commands::") + "::"
	errFirst := ""
	if len(errLines) > 0 {
		errFirst = errLines[0]
	}
	if errFirst != stop {
		faults = append(faults, fmt.Sprintf("stderr's first line is %q, want %q, stdout's", errFirst, stop))
	}
	// The first-line checks find a stop line that is missing; the counts find one that stands
	// again later, which would stop the commands anew after the resume line.
	if n := countLine(outLines, stop); n > 1 {
		faults = append(faults, fmt.Sprintf("stdout holds the stop line %d times, want once", n))
	}
	if n := countLine(errLines, stop); n > 1 {
		faults = append(faults, fmt.Sprintf("stderr holds the stop line %d times, want once", n))
	}
	if slices.Contains(errLines, resume) {
		faults = append(faults, fmt.Sprintf("stderr holds the line %q, which only stdout's last line may be", resume))
	}
	if bySignal {
		if slices.Contains(outLines, resume) {
			faults = append(faults, fmt.Sprintf("stdout holds the line %q, though a signal ended the run", resume))
		}
		return faults
	}
	if last := outLines[len(outLines)-1]; last != resume {
		faults = append(faults, fmt.Sprintf("stdout's last line is %q, want %q", last, resume))
	}
	// The last-line check finds a resume line that is missing; the count finds one that stands
	// before the last line as well.
	if n := countLine(outLines, resume); n > 1 {
		faults = append(faults, fmt.Sprintf("stdout holds the line %q %d times, want once", resume, n))
	}
	return faults
}

// streamLines gives the lines of a stream's text: the text without its last line
// end, split at each line end; none for an empty text. A line ends at CR LF, at
// LF and at a lone CR, as the runner's reader ends a line, so that a line after a
// lone carriage return is judged as the runner would read it.
func streamLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// countLine gives how many of lines are exactly line.
func countLine(lines []string, line string) int {
	n := 0
	for _, l := range lines {
		if l == line {
			n++
		}
	}
	return n
}

func TestHelmMajors(t *testing.T) {
	for _, c := range []struct {
		value string
		want  []string
	}{
		{"", []string{"helm-3", "helm-4"}},
		{"3", []string{"helm-3"}},
		{"4", []string{"helm-4"}},
	} {
		got, err := helmMajors(c.value)
		if err != nil {
			t.Errorf("helmMajors(%q): %v", c.value, err)
		}
		if diff := cmp.Diff(c.want, got); diff != "" {
			t.Errorf("helmMajors(%q) (-want +got):\n%s", c.value, diff)
		}
	}
	for _, value := range []string{"5", "helm-3"} {
		got, err := helmMajors(value)
		want := `ROLLPROOF_E2E_HELM is "` + value + `": it must be 3, 4 or not set`
		if err == nil || err.Error() != want {
			t.Errorf("helmMajors(%q) gave %v and the error %v, want the error %q", value, got, err, want)
		}
	}
}

func TestRunEnvKeepsTheJobsGitHubFilesOut(t *testing.T) {
	base := []string{"HOME=/home/u", "PATH=/usr/bin:/bin", "GITHUB_ACTIONS=true",
		"GITHUB_OUTPUT=/runner/out", "GITHUB_STEP_SUMMARY=/runner/sum", "CI=true"}
	for _, c := range []struct {
		name string
		base []string
		gh   *githubFiles
		want []string
	}{
		{"no files of its own", base, nil,
			[]string{"HOME=/home/u", "PATH=/usr/bin:/bin", "GITHUB_ACTIONS=true", "CI=true", "PATH=/helm:/usr/bin:/bin"}},
		{"files of its own", base, &githubFiles{"/r/github-output", "/r/github-summary"},
			[]string{"HOME=/home/u", "PATH=/usr/bin:/bin", "CI=true", "GITHUB_ACTIONS=true",
				"GITHUB_OUTPUT=/r/github-output", "GITHUB_STEP_SUMMARY=/r/github-summary", "PATH=/helm:/usr/bin:/bin"}},
		{"no PATH in base", []string{"HOME=/h"}, nil, []string{"HOME=/h", "PATH=/helm"}},
		{"an empty PATH in base", []string{"HOME=/h", "PATH="}, nil, []string{"HOME=/h", "PATH=", "PATH=/helm"}},
	} {
		before := slices.Clone(c.base)
		if diff := cmp.Diff(c.want, runEnv(c.base, "/helm", c.gh)); diff != "" {
			t.Errorf("%s: runEnv (-want +got):\n%s", c.name, diff)
		}
		if diff := cmp.Diff(before, c.base); diff != "" {
			t.Errorf("%s: runEnv changed base (-before +after):\n%s", c.name, diff)
		}
	}
}

func TestSummaryFaults(t *testing.T) {
	const out = "/tmp/r"
	good := "### rollproof: PASS `orders (Helm) on Kubernetes v1.37.0 in kind`\n" +
		"\n```\nreleases    previous   orders 1.4.2\n```\n" +
		"\n| window | length | result |\n| --- | ---: | --- |\n| steady | 10.0 s | pass |\n" +
		"\n```\nresults: " + out + "\n```\n"
	warning := "\n```\nwarning: the run's cluster was not deleted: gone\n```\n"
	const heading = `the summary holds 2 lines that begin "### rollproof: ", want exactly one`
	const results = "the summary holds the line \"results: /tmp/r\" followed by the fence %d times, want exactly once"
	for _, c := range []struct {
		name, summary, word string
		want                []string
	}{
		{"a good summary", good, "PASS", nil},
		{"a warning appended after the results block", good + warning, "PASS", nil},
		{"the summary twice", good + good, "PASS", []string{heading, fmt.Sprintf(results, 2)}},
		{"no results line", strings.Replace(good, "results: "+out+"\n", "", 1), "PASS",
			[]string{fmt.Sprintf(results, 0)}},
		{"a first line that is not the heading", "notice\n" + good, "PASS",
			[]string{"the summary's first line is \"notice\", want one that begins \"### rollproof: PASS\""}},
		{"another word", good, "FAIL",
			[]string{"the summary's first line is \"### rollproof: PASS `orders (Helm) on Kubernetes v1.37.0 in kind`\", want one that begins \"### rollproof: FAIL\""}},
		{"a results line that names another folder", strings.Replace(good, "results: "+out, "results: /tmp/other", 1), "PASS",
			[]string{fmt.Sprintf(results, 0)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if diff := cmp.Diff(c.want, summaryFaults(c.summary, out, c.word)); diff != "" {
				t.Errorf("summaryFaults (-want +got):\n%s", diff)
			}
		})
	}
}

func TestEnvValue(t *testing.T) {
	for _, c := range []struct {
		name string
		env  []string
		want string
	}{
		{"set", []string{"A=1", "GITHUB_ACTIONS=true"}, "true"},
		{"set twice, the last empty", []string{"GITHUB_ACTIONS=true", "GITHUB_ACTIONS="}, ""},
		{"set twice, the last true", []string{"GITHUB_ACTIONS=", "GITHUB_ACTIONS=true"}, "true"},
		{"not there", []string{"A=1"}, ""},
		{"nil", nil, ""},
	} {
		if got := envValue(c.env, "GITHUB_ACTIONS"); got != c.want {
			t.Errorf("%s: envValue(%q, GITHUB_ACTIONS) is %q, want %q", c.name, c.env, got, c.want)
		}
	}
}

func TestWorkflowCommandFaults(t *testing.T) {
	const (
		token  = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
		stop   = "::stop-commands::" + token
		resume = "::" + token + "::"
		out    = stop + "\nrollproof: PASS\nresults: /r\n" + resume + "\n"
	)
	for _, c := range []struct {
		name             string
		stdout, stderr   string
		onGitHub, signal bool
		want             []string
	}{
		{"off GitHub", "rollproof: PASS\nresults: /r\n", "", false, false, nil},
		{"off GitHub, a stop line", stop + "\nrollproof: PASS\n", "", false, false,
			[]string{`stdout holds the line "::stop-commands::ABCDEFGHIJKLMNOPQRSTUVWXYZ", which begins "::", though GITHUB_ACTIONS is not true`}},
		{"off GitHub, an indented line on stderr", "rollproof: PASS\n", "  ::error::x\n", false, false,
			[]string{`stderr holds the line "  ::error::x", which begins "::", though GITHUB_ACTIONS is not true`}},
		{"on GitHub", out, stop + "\n", true, false, nil},
		{"on GitHub, after a signal", stop + "\nrollproof: NOT VERIFIED\nresults: /r\n", stop + "\n", true, true, nil},
		{"no stop line", "rollproof: PASS\n" + resume + "\n", stop + "\n", true, false,
			[]string{`stdout's first line is "rollproof: PASS", want "::stop-commands::" and a token of 26 characters`}},
		{"a token of another form", "::stop-commands::abc\n" + resume + "\n", stop + "\n", true, false,
			[]string{`stdout's first line is "::stop-commands::abc", want "::stop-commands::" and a token of 26 characters`}},
		{"nothing on stdout", "", stop + "\n", true, false,
			[]string{`stdout's first line is "", want "::stop-commands::" and a token of 26 characters`}},
		{"stderr begins with another line", out, "warning\n" + stop + "\n", true, false,
			[]string{`stderr's first line is "warning", want "::stop-commands::ABCDEFGHIJKLMNOPQRSTUVWXYZ", stdout's`}},
		{"the stop line twice", stop + "\n" + out, stop + "\n", true, false,
			[]string{"stdout holds the stop line 2 times, want once"}},
		{"no resume line", stop + "\nrollproof: PASS\n", stop + "\n", true, false,
			[]string{`stdout's last line is "rollproof: PASS", want "::ABCDEFGHIJKLMNOPQRSTUVWXYZ::"`}},
		{"the resume line twice", out + resume + "\n", stop + "\n", true, false,
			[]string{`stdout holds the line "::ABCDEFGHIJKLMNOPQRSTUVWXYZ::" 2 times, want once`}},
		{"a resume line after a signal", out, stop + "\n", true, true,
			[]string{`stdout holds the line "::ABCDEFGHIJKLMNOPQRSTUVWXYZ::", though a signal ended the run`}},
		{"a resume line on stderr", out, stop + "\n" + resume + "\n", true, false,
			[]string{`stderr holds the line "::ABCDEFGHIJKLMNOPQRSTUVWXYZ::", which only stdout's last line may be`}},
		{"off GitHub, a line after a lone carriage return", "rollproof: PASS\rnext\r::error::x\n", "", false, false,
			[]string{`stdout holds the line "::error::x", which begins "::", though GITHUB_ACTIONS is not true`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if diff := cmp.Diff(c.want, workflowCommandFaults(c.stdout, c.stderr, c.onGitHub, c.signal)); diff != "" {
				t.Errorf("workflowCommandFaults (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReadGitHubOutputs(t *testing.T) {
	write := func(t *testing.T, content string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "github-output")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for _, c := range []struct {
		name, content string
		want          map[string][]string
	}{
		{"a value of two lines", "x<<E\na\nb\nE\n", map[string][]string{"x": {"a\nb"}}},
		{"an empty file", "", map[string][]string{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := readGitHubOutputs(write(t, c.content))
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("readGitHubOutputs (-want +got):\n%s", diff)
			}
			if got == nil {
				t.Errorf("readGitHubOutputs gave a nil map, want an empty one")
			}
		})
	}
	for _, c := range []struct {
		name, content, holds string
	}{
		{"the plain form after the delimiter form", "out<<D1\n/tmp/r\nD1\nverdict<<D2\nnot-verified\nD2\nverdict=pass\n",
			`line 7: "verdict=pass" is not NAME<<DELIMITER`},
		{"a line in the plain form", "good=1\nno separator\n", `line 1: "good=1" is not NAME<<DELIMITER`},
		// The delimiter quoted, so that a path that holds the letter cannot pass for it.
		{"a delimiter that never comes", "x<<E\na\n", `"E"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := write(t, c.content)
			got, err := readGitHubOutputs(path)
			if err == nil {
				t.Fatalf("readGitHubOutputs gave %v and no error", got)
			}
			for _, want := range []string{path, c.holds} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the error %q does not hold %q", err, want)
				}
			}
		})
	}
}
