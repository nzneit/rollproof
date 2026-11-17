package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// timeLayout is RFC 3339 with milliseconds; it is used on a time in UTC.
const timeLayout = "2006-01-02T15:04:05.000Z07:00"

// MarshalJSON writes the window's keys in the order of its fields, each time in
// UTC as RFC 3339 with milliseconds, a nil time as null, and a nil
// FailedByClass as {}.
func (w Window) MarshalJSON() ([]byte, error) {
	stamp := func(t *time.Time) *string {
		if t == nil {
			return nil
		}
		s := t.UTC().Format(timeLayout)
		return &s
	}
	byClass := w.FailedByClass
	if byClass == nil {
		byClass = map[string]int{}
	}
	return json.Marshal(struct {
		Name              string         `json:"name"`
		Start             *string        `json:"start"`
		End               *string        `json:"end"`
		Result            string         `json:"result"`
		Checks            int            `json:"checks"`
		Failed            int            `json:"failed"`
		FailedAfterFinish int            `json:"failed_after_finish"`
		CrossedStart      int            `json:"crossed_start"`
		FailedByClass     map[string]int `json:"failed_by_class"`
		FirstFailure      *string        `json:"first_failure"`
		SlowestMs         float64        `json:"slowest_ms"`
		LagMs             float64        `json:"lag_ms"`
	}{
		w.Name, stamp(w.Start), stamp(w.End), w.Result, w.Checks, w.Failed,
		w.FailedAfterFinish, w.CrossedStart, byClass, stamp(w.FirstFailure),
		w.SlowestMs, w.LagMs,
	})
}

// list returns s, or an empty list when s is nil, so that it is written as [].
func list[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// encode returns the bytes of result.json for r. It works on copies: the
// caller's record does not change.
func encode(r Run) ([]byte, error) {
	r.Schema = 1
	r.Reasons = append([]Reason{}, r.Reasons...)
	for i := range r.Reasons {
		r.Reasons[i].Detail = list(r.Reasons[i].Detail)
	}
	r.Windows = list(r.Windows)
	r.Transitions = append([]Transition{}, r.Transitions...)
	for i := range r.Transitions {
		t := &r.Transitions[i]
		t.SettleRestartedBy = list(t.SettleRestartedBy)
		t.Workloads = list(t.Workloads)
	}
	r.Probes = append([]Probe{}, r.Probes...)
	for i := range r.Probes {
		p := &r.Probes[i]
		p.Settings = list(p.Settings)
		p.Headers = list(p.Headers)
		p.EnvNames = list(p.EnvNames)
		p.Notes = list(p.Notes)
	}
	r.Releases = append([]Release{}, r.Releases...)
	for i := range r.Releases {
		x := &r.Releases[i]
		x.ValuesFiles = list(x.ValuesFiles)
		x.InlineKeys = list(x.InlineKeys)
	}
	r.Settings = list(r.Settings)
	r.Images = append([]Image{}, r.Images...)
	for i := range r.Images {
		r.Images[i].RunBy = list(r.Images[i].RunBy)
	}
	r.RemovedVariables = list(r.RemovedVariables)
	r.Warnings = list(r.Warnings)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// First returns the record a run starts with: not final, not-verified, with
// the one reason internal-error and the text "the run did not finish".
func First() Run {
	return Run{
		Schema:  1,
		Verdict: "not-verified",
		Reasons: []Reason{{Code: "internal-error", Verdict: "not-verified", Text: "the run did not finish", Decided: true}},
	}
}

// Writer writes the files of one results folder.
type Writer struct {
	dir string
}

// Open makes the results folder ready and writes the first result.json into
// it. An empty out means a new folder under the runner's temporary directory on
// GitHub Actions, and under the system's otherwise. It replaces result.json and
// the evidence folder there and touches nothing else.
func Open(out string, getenv func(string) string) (*Writer, error) {
	var dir string
	if out == "" {
		base := os.TempDir()
		if getenv("GITHUB_ACTIONS") == "true" && getenv("RUNNER_TEMP") != "" {
			base = getenv("RUNNER_TEMP")
		}
		d, err := os.MkdirTemp(base, "rollproof-results-")
		if err != nil {
			return nil, fmt.Errorf("results folder %s: %w", base, err)
		}
		dir = d
	} else {
		abs, err := filepath.Abs(out)
		if err != nil {
			return nil, fmt.Errorf("results folder %s: %w", out, err)
		}
		dir = abs
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("results folder %s: %w", dir, err)
		}
	}
	w := &Writer{dir: dir}

	// Only a real folder named evidence, and no link or folder named result.json,
	// is ours to replace.
	evidence := filepath.Join(dir, "evidence")
	if fi, err := os.Lstat(evidence); err == nil {
		if !fi.IsDir() {
			return nil, fmt.Errorf("results folder %s: evidence is not a folder, and is left as it is", dir)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("results folder %s: %w", dir, err)
	}
	if fi, err := os.Lstat(filepath.Join(dir, "result.json")); err == nil {
		if !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("results folder %s: result.json is not a file, and is left as it is", dir)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("results folder %s: %w", dir, err)
	}
	if err := os.RemoveAll(evidence); err != nil {
		return nil, fmt.Errorf("results folder %s: %w", dir, err)
	}
	if err := os.Mkdir(evidence, 0o755); err != nil {
		return nil, fmt.Errorf("results folder %s: %w", dir, err)
	}
	if err := w.write(First()); err != nil {
		return nil, err
	}
	return w, nil
}

// Dir returns the results folder's absolute path.
func (w *Writer) Dir() string { return w.dir }

// Final replaces result.json with r, marked final, in one step. It may be
// called again when a warning arises after the verdict.
func (w *Writer) Final(r Run) error {
	r.Final = true
	return w.write(r)
}

// write replaces result.json with r: a temporary file in the same folder is
// written and renamed over it, so a reader never sees half a file.
func (w *Writer) write(r Run) error {
	b, err := encode(r)
	if err != nil {
		return fmt.Errorf("results folder %s: %w", w.dir, err)
	}
	f, err := os.CreateTemp(w.dir, ".result-*.json")
	if err != nil {
		return fmt.Errorf("results folder %s: %w", w.dir, err)
	}
	tmp := f.Name()
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, filepath.Join(w.dir, "result.json"))
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("results folder %s: %w", w.dir, err)
	}
	return nil
}
