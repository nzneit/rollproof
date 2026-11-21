package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"rollproof/internal/mask"
)

// timeLayout is RFC 3339 with milliseconds; it is used on a time in UTC.
const timeLayout = "2006-01-02T15:04:05.000Z07:00"

// stamp writes a time of the record in UTC as RFC 3339 with milliseconds, and
// a nil time as null.
func stamp(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(timeLayout)
	return &s
}

// MarshalJSON writes the window's keys in the order of its fields, each time in
// UTC as RFC 3339 with milliseconds, a nil time as null, and a nil
// FailedByClass as {}.
func (w Window) MarshalJSON() ([]byte, error) {
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
		SlowestMs         *float64       `json:"slowest_ms"`
		LagMs             *float64       `json:"lag_ms"`
	}{
		w.Name, stamp(w.Start), stamp(w.End), w.Result, w.Checks, w.Failed,
		w.FailedAfterFinish, w.CrossedStart, byClass, stamp(w.FirstFailure),
		w.SlowestMs, w.LagMs,
	})
}

// MarshalJSON writes the install's keys in the order of its fields, each time
// in UTC as RFC 3339 with milliseconds, a nil time as null, a nil
// FailedByClass as {} and a nil SettleRestartedBy as [].
func (i Install) MarshalJSON() ([]byte, error) {
	byClass := i.FailedByClass
	if byClass == nil {
		byClass = map[string]int{}
	}
	return json.Marshal(struct {
		Ready             string         `json:"ready"`
		FirstObservation  *string        `json:"first_observation"`
		Settled           *string        `json:"settled"`
		SettleRestartedBy []string       `json:"settle_restarted_by"`
		Checks            int            `json:"checks"`
		Failed            int            `json:"failed"`
		FailedByClass     map[string]int `json:"failed_by_class"`
		LastFailure       *string        `json:"last_failure"`
		SlowestMs         *float64       `json:"slowest_ms"`
		LagMs             *float64       `json:"lag_ms"`
	}{
		i.Ready.UTC().Format(timeLayout), stamp(i.FirstObservation), stamp(i.Settled),
		list(i.SettleRestartedBy), i.Checks, i.Failed, byClass, stamp(i.LastFailure),
		i.SlowestMs, i.LagMs,
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
	if r.Install != nil {
		install := *r.Install
		r.Install = &install
	}
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

// Writer writes the files of one results folder. After SetMasker, what it
// writes is masked by that masker, except what goes into an evidence file
// opened before.
type Writer struct {
	dir    string
	masker atomic.Pointer[mask.Masker]
}

// SetMasker sets the masker of the writer. From this call on, Final writes
// Mask of its record, Evidence's files mask what is written to them, and
// ReplaceEvidence masks its bytes. The run calls it once, after the
// configuration loads and before anything that may hold a password is written.
// Safe while other goroutines write; a file that Evidence opened before the
// call keeps writing unmasked.
func (w *Writer) SetMasker(m *mask.Masker) {
	w.masker.Store(m)
}

// maskerOf returns the masker set through SetMasker, nil before it is called.
func (w *Writer) maskerOf() *mask.Masker { return w.masker.Load() }

// Open makes the results folder ready and writes the first result.json into
// it. An empty out means a new folder under the runner's temporary directory on
// GitHub Actions, and under the system's otherwise. It replaces result.json and
// the evidence folder there and touches nothing else. It writes the first
// result.json before it touches the evidence, so a failure after that point
// leaves a file that says the run did not finish. One limit: when result.json
// cannot be replaced (a folder that cannot be written, say), an earlier run's
// result.json stays, because nothing can replace it; Open then returns its
// error.
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
		if dir, err = filepath.Abs(d); err != nil {
			return nil, fmt.Errorf("results folder %s: %w", d, err)
		}
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

	// result.json comes first: whatever fails after this leaves a file that says
	// the run did not finish, never an earlier run's pass. A link named
	// result.json is replaced by the rename, and its target is never touched; a
	// folder or another non-file is not ours and is refused.
	if fi, err := os.Lstat(filepath.Join(dir, "result.json")); err == nil {
		if !fi.Mode().IsRegular() && fi.Mode()&os.ModeSymlink == 0 {
			return nil, fmt.Errorf("results folder %s: result.json is not a file, and is left as it is", dir)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("results folder %s: %w", dir, err)
	}
	if err := w.write(First()); err != nil {
		return nil, err
	}

	// Only a real folder named evidence is ours to replace.
	evidence := filepath.Join(dir, "evidence")
	if fi, err := os.Lstat(evidence); err == nil {
		if !fi.IsDir() {
			return nil, fmt.Errorf("results folder %s: evidence is not a folder, and is left as it is", dir)
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
	return w, nil
}

// Dir returns the results folder's absolute path.
func (w *Writer) Dir() string { return w.dir }

// Final replaces result.json with r, masked by the masker set through
// SetMasker, marked final, in one step. It may be called again when a warning
// arises after the verdict.
func (w *Writer) Final(r Run) error {
	switch r.Verdict {
	case "pass", "fail", "not-verified":
	default:
		return fmt.Errorf("results folder %s: verdict %q is not pass, fail or not-verified", w.dir, r.Verdict)
	}
	r.Final = true
	return w.write(r)
}

// write replaces result.json with r, masked by the masker set through
// SetMasker: a temporary file in the same folder is written and renamed over
// it, so a reader never sees half a file.
func (w *Writer) write(r Run) error {
	b, err := encode(Mask(r, w.maskerOf()))
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
