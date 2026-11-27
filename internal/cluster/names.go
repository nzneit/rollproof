// Package cluster handles a run's kind cluster and its registry. It checks the runner, creates
// both, makes the nodes pull through the run's registry, waits until the cluster is ready,
// creates the two namespaces, and deletes all of it. It hides kind, the container engine and the
// nodes' registry settings.
package cluster

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"

	"github.com/nzneit/rollproof/internal/verdict"
)

// ProbeNamespace is the namespace of every probe pod.
const ProbeNamespace = "rollproof-probe"

// TestHost is the registry host, known only inside a run, under which the tool stores images that
// name no host of their own.
const TestHost = "rollproof.test"

// namePattern allows 1 to 40 characters, so that rollproof-NAME-control-plane stays within the 64
// characters of a Linux host name.
var namePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,38}[a-z0-9])?$`)

// Name returns the cluster's name. With name it is rollproof-NAME; name may hold lower case
// letters, digits, dots and hyphens. Without, it is rollproof- and ten hexadecimal digits that
// depend only on the file's real absolute path.
func Name(configPath, name string) (string, error) {
	if name != "" {
		if !namePattern.MatchString(name) {
			return "", verdict.Condition{Code: verdict.ConfigRejected, Text: "--name may hold 1 to 40 lower-case letters, digits, dots and hyphens, and must start and end with a letter or a digit"}
		}
		return "rollproof-" + name, nil
	}
	abs, err := filepath.Abs(configPath)
	if err == nil {
		abs, err = filepath.EvalSymlinks(abs)
	}
	if err != nil {
		return "", verdict.Condition{Code: verdict.ConfigRejected, Text: "the configuration file's path cannot be resolved: " + err.Error()}
	}
	sum := sha256.Sum256([]byte(abs))
	return "rollproof-" + hex.EncodeToString(sum[:])[:10], nil
}

// RunDir returns the run's folder: one under the system's temporary directory that carries the
// cluster's name. It is not created here.
func RunDir(clusterName string) string {
	return filepath.Join(os.TempDir(), clusterName)
}
