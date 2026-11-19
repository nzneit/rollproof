//go:build linux

package cluster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	kindcluster "sigs.k8s.io/kind/pkg/cluster"
)

// kindCollectLogs and containerLogs are the kind provider's CollectLogs and Engine.logs, as
// collectLogs calls them. A test replaces them.
var (
	kindCollectLogs = (*kindcluster.Provider).CollectLogs
	containerLogs   = Engine.logs
)

// collectLogs writes kind's logs of the cluster's nodes and the registry container's log into
// evidence: kind's into a folder under runDir, copied file by file to kind/<path> and the folder
// then removed, and the registry's as registry.log. It goes on after an error and joins them. A
// nil evidence collects nothing.
func collectLogs(ctx context.Context, e Engine, p *kindcluster.Provider, name, runDir string, evidence func(string) (io.WriteCloser, error)) error {
	if evidence == nil {
		return nil
	}
	dir := filepath.Join(runDir, "kind-logs")
	errs := []error{kindCollectLogs(p, name, dir), copyLogs(evidence, dir)}
	if err := os.RemoveAll(dir); err != nil {
		errs = append(errs, err)
	}
	log, found, err := containerLogs(e, ctx, name+"-registry")
	switch {
	case err != nil:
		errs = append(errs, err)
	case found:
		errs = append(errs, writeEvidence(evidence, "registry.log", strings.NewReader(log)))
	}
	return join(errs...)
}

// CollectLogs writes kind's logs of the cluster's nodes and the registry container's log into the
// cluster's evidence, for a set-up that failed after Create returned. A registry container that
// the engine does not find gives no registry.log and no error. The error joins what could not be
// collected; what could be is written.
func (c *Cluster) CollectLogs(ctx context.Context) error {
	return collectLogs(ctx, c.Engine, c.provider, c.Name, c.RunDir, c.evidence)
}

// copyLogs writes every regular file under dir into the evidence under kind/<path>. A folder that
// kind's collection did not make is no error: that collection's error says why.
func copyLogs(evidence func(string) (io.WriteCloser, error), dir string) error {
	var errs []error
	walk := func(path string, d fs.DirEntry, err error) error {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil
		case err != nil:
			errs = append(errs, err)
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err == nil {
			var f *os.File
			if f, err = os.Open(path); err == nil {
				defer f.Close()
				err = writeEvidence(evidence, filepath.Join("kind", rel), f)
			}
		}
		if err != nil {
			errs = append(errs, err)
		}
		return nil
	}
	if err := filepath.WalkDir(dir, walk); err != nil {
		errs = append(errs, err)
	}
	return join(errs...)
}

// writeEvidence writes r into the evidence file name.
func writeEvidence(evidence func(string) (io.WriteCloser, error), name string, r io.Reader) error {
	w, err := evidence(name)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	var errs []error
	if _, err := io.Copy(w, r); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", name, err))
	}
	if err := w.Close(); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", name, err))
	}
	return join(errs...)
}
