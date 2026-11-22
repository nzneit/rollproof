package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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

// readGitHubOutputs reads an outputs file in the delimiter form and in the plain
// form NAME=VALUE, and gives each name's values in the order they were written. A
// line that is neither, or a delimiter that never comes, is an error that names
// the file and the line.
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
		if name, value, ok := strings.Cut(line, "="); ok && name != "" {
			outs[name] = append(outs[name], value)
			continue
		}
		return nil, fmt.Errorf("%s line %d: %q is neither NAME<<DELIMITER nor NAME=VALUE", path, i+1, line)
	}
	return outs, nil
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
	before := slices.Clone(base)
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
	} {
		if diff := cmp.Diff(c.want, runEnv(c.base, "/helm", c.gh)); diff != "" {
			t.Errorf("%s: runEnv (-want +got):\n%s", c.name, diff)
		}
	}
	if diff := cmp.Diff(before, base); diff != "" {
		t.Errorf("runEnv changed base (-before +after):\n%s", diff)
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
		{"both forms", "out<<D1\n/tmp/r\nD1\nverdict<<D2\nnot-verified\nD2\nverdict=pass\n",
			map[string][]string{"out": {"/tmp/r"}, "verdict": {"not-verified", "pass"}}},
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
		{"a line that is no output", "good=1\nno separator\n", "line 2"},
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
