package report

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Evidence returns a new file of the evidence folder. name may hold folders. A
// name that is already taken gets a number after it. A name that is empty,
// absolute, holds a NUL byte or a .. element, or would leave the evidence
// folder, by its text or by a link inside the folder, is refused, and nothing
// is written.
func (w *Writer) Evidence(name string) (io.WriteCloser, error) {
	root, err := w.evidenceRoot(name)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for n := 1; ; n++ {
		taken := name
		if n > 1 {
			taken = fmt.Sprintf("%s-%d", name, n)
		}
		if err := w.staysInside(name, taken); err != nil {
			return nil, err
		}
		inside := taken
		if err := root.MkdirAll(path.Dir(inside), 0o755); err != nil {
			return nil, fmt.Errorf("evidence name %q: %w", name, err)
		}
		f, err := root.OpenFile(inside, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("evidence name %q: %w", name, err)
		}
		return f, nil
	}
}

// ReplaceEvidence replaces the evidence file name with b in one step: a reader
// sees the old content or the new, never half, and the name is never numbered.
// Names are refused as Evidence refuses them. An error leaves the old content.
func (w *Writer) ReplaceEvidence(name string, b []byte) error {
	root, err := w.evidenceRoot(name)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := w.staysInside(name, name); err != nil {
		return err
	}
	if err := root.MkdirAll(path.Dir(name), 0o755); err != nil {
		return fmt.Errorf("evidence name %q: %w", name, err)
	}
	// The new content is written beside the name under a name of its own, and then renamed over
	// it, so that the file under the name is always whole.
	tmp := path.Join(path.Dir(name), "."+path.Base(name)+"-"+rand.Text())
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("evidence name %q: %w", name, err)
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = root.Rename(tmp, name)
	}
	if err != nil {
		_ = root.Remove(tmp) // the error below is the one that matters
		return fmt.Errorf("evidence name %q: %w", name, err)
	}
	return nil
}

// evidenceRoot checks name and opens the evidence folder as a root, so that no
// link inside it leads out of it, not even to another place in the results
// folder. A link or file where the folder was is refused, also when the link
// leads to another place in the results folder. The caller closes the root.
func (w *Writer) evidenceRoot(name string) (*os.Root, error) {
	if err := checkEvidenceName(name); err != nil {
		return nil, err
	}
	results, err := os.OpenRoot(w.dir)
	if err != nil {
		return nil, fmt.Errorf("evidence name %q: %w", name, err)
	}
	defer results.Close()
	if fi, err := results.Lstat("evidence"); err != nil {
		return nil, fmt.Errorf("evidence name %q: %w", name, err)
	} else if !fi.IsDir() {
		return nil, fmt.Errorf("evidence name %q: evidence is not a folder", name)
	}
	root, err := results.OpenRoot("evidence")
	if err != nil {
		return nil, fmt.Errorf("evidence name %q: %w", name, err)
	}
	return root, nil
}

// staysInside refuses taken, the file that stands for name, when its text
// leaves the evidence folder or names the folder itself.
func (w *Writer) staysInside(name, taken string) error {
	evidence := filepath.Join(w.dir, "evidence")
	full := filepath.Join(evidence, filepath.FromSlash(taken))
	if rel, err := filepath.Rel(evidence, full); err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("evidence name %q leaves the evidence folder", name)
	}
	return nil
}

// checkEvidenceName refuses a name that is empty, absolute, holds a NUL byte,
// or has .. as one of its elements.
func checkEvidenceName(name string) error {
	if name == "" || strings.ContainsRune(name, 0) || filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
		return fmt.Errorf("evidence name %q is not allowed", name)
	}
	for _, e := range strings.Split(name, "/") {
		if e == ".." {
			return fmt.Errorf("evidence name %q is not allowed", name)
		}
	}
	return nil
}
