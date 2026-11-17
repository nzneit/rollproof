package report

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Evidence returns a new file of the evidence folder. name may hold folders. A
// name that is already taken gets a number after it. A name that is empty,
// absolute, holds a NUL byte or a .. element, or would leave the evidence
// folder is refused, and nothing is written.
func (w *Writer) Evidence(name string) (io.WriteCloser, error) {
	if err := checkEvidenceName(name); err != nil {
		return nil, err
	}
	root := filepath.Join(w.dir, "evidence")
	for n := 1; ; n++ {
		taken := name
		if n > 1 {
			taken = fmt.Sprintf("%s-%d", name, n)
		}
		path := filepath.Join(root, filepath.FromSlash(taken))
		if rel, err := filepath.Rel(root, path); err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("evidence name %q leaves the evidence folder", name)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("evidence name %q: %w", name, err)
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("evidence name %q: %w", name, err)
		}
		return f, nil
	}
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
