package report

import (
	"crypto/rand"
	"os"
	"strings"
)

// GitHub is where a run writes for GitHub Actions: the file of the step's outputs
// and the file of its job summary, as GitHubFrom found them. A zero GitHub writes
// nothing.
type GitHub struct {
	outputs string
	summary string
}

// GitHubActions reports whether the tool runs on GitHub Actions: GITHUB_ACTIONS is
// exactly "true" (section 8), which a caller's env: cannot change there.
// GitHubFrom and the run command's lines for workflow commands both ask it, so
// that the rule is written once.
func GitHubActions(getenv func(string) string) bool {
	return getenv("GITHUB_ACTIONS") == "true"
}

// GitHubFrom reads the environment through getenv. The tool detects GitHub Actions
// as GitHubActions does (section 8); the GitHub it returns then writes the outputs
// to the file that GITHUB_OUTPUT names and the summary to the file that
// GITHUB_STEP_SUMMARY names, each only when its variable is not empty. Otherwise
// it writes nothing.
func GitHubFrom(getenv func(string) string) GitHub {
	if !GitHubActions(getenv) {
		return GitHub{}
	}
	return GitHub{outputs: getenv("GITHUB_OUTPUT"), summary: getenv("GITHUB_STEP_SUMMARY")}
}

// Output is one output of the step: its name and its value.
type Output struct {
	Name, Value string
}

// WriteOutputs appends outs to the step's outputs file in one write, each in the
// delimiter form: NAME<<DELIMITER, the value, the delimiter, each on a line of its
// own, with a delimiter that the value does not hold, so that no value can be
// misread (section 14, point 4). It does nothing when no outputs file is named.
// The file must exist: it is opened for appending and never made.
func (g GitHub) WriteOutputs(outs ...Output) error {
	if g.outputs == "" {
		return nil
	}
	var b strings.Builder
	for _, o := range outs {
		d := newDelimiter()
		for strings.Contains(o.Value, d) {
			d = newDelimiter()
		}
		b.WriteString(o.Name)
		b.WriteString("<<")
		b.WriteString(d)
		b.WriteString("\n")
		b.WriteString(o.Value)
		b.WriteString("\n")
		b.WriteString(d)
		b.WriteString("\n")
	}
	return appendTo(g.outputs, b.String())
}

// AppendSummary appends text to the step's summary file in one write. It does
// nothing when no summary file is named. The file must exist: it is opened for
// appending and never made.
func (g GitHub) AppendSummary(text string) error {
	if g.summary == "" {
		return nil
	}
	return appendTo(g.summary, text)
}

// newDelimiter gives a delimiter for one output: ROLLPROOF_EOF_ and 26 random
// characters. A test replaces it.
var newDelimiter = func() string { return "ROLLPROOF_EOF_" + rand.Text() }

// appendTo appends text to the file at path in one write, and returns the error
// of the open, the write or the close as it is. The file must exist: it is
// opened for appending and never made.
func appendTo(path, text string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	_, err = f.Write([]byte(text))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
