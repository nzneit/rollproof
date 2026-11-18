//go:build linux

package cluster

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	kindcluster "sigs.k8s.io/kind/pkg/cluster"

	"rollproof/internal/verdict"
)

// missedLine returns the error of kind's Create when its wait for a node's log line ended, as
// proof p08 recorded it, marked with the attempt so that a test can tell the attempts apart.
func missedLine(attempt int) error {
	return fmt.Errorf(`could not find a log line that matches "Reached target .*Multi-User System.*|detected cgroup v1" (attempt %d)`, attempt)
}

// Step 3 of Create: kind's Create is made again only after a missed log line, at most four times
// in all, and each failed attempt is deleted before the next, also when the nodes are to be kept.
func TestCreateKindRetries(t *testing.T) {
	const name, kubeconfig = "rollproof-test-retry", "/tmp/rollproof-test-retry/kubeconfig"
	other := errors.New(`command "nerdctl run --name rollproof-test-retry-worker" failed with error: exit status 1`)
	for _, tc := range []struct {
		name             string
		results          []error // what each attempt of kind's Create returns, in order
		deleteErr        error   // what each delete of a failed attempt returns
		cancel           bool    // ctx ends while the first attempt runs
		creates, deletes int
		want             string // the error's text; empty for none
	}{
		{"the first attempt succeeds", []error{nil}, nil, false, 1, 0, ""},
		{"a missed line twice, then success", []error{missedLine(1), missedLine(2), nil}, nil, false, 3, 2, ""},
		{"a missed line four times", []error{missedLine(1), missedLine(2), missedLine(3), missedLine(4), nil}, nil, false, 4, 3,
			missedLine(4).Error()},
		{"another failure is not tried again", []error{other, nil}, nil, false, 1, 0, other.Error()},
		{"another failure after a missed line", []error{missedLine(1), other, nil}, nil, false, 2, 1, other.Error()},
		{"ctx has ended", []error{missedLine(1), nil}, nil, true, 1, 0, missedLine(1).Error()},
		{"the delete of a failed attempt fails", []error{missedLine(1), nil}, errors.New("exit status 1"), false, 1, 1,
			missedLine(1).Error() + "; delete the failed attempt 1: exit status 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			oldCreate, oldDelete := kindCreate, kindDelete
			t.Cleanup(func() { kindCreate, kindDelete = oldCreate, oldDelete })
			var creates, deletes int
			kindCreate = func(_ *kindcluster.Provider, n string, options ...kindcluster.CreateOption) error {
				if creates == len(tc.results) {
					t.Fatalf("attempt %d of kind's Create, want at most %d", creates+1, len(tc.results))
				}
				if n != name || len(options) != 1 {
					t.Errorf("kind's Create(%q, %d options), want (%q, 1 option)", n, len(options), name)
				}
				err := tc.results[creates]
				creates++
				if tc.cancel {
					cancel()
				}
				return err
			}
			kindDelete = func(_ *kindcluster.Provider, n, k string) error {
				deletes++
				if n != name || k != kubeconfig {
					t.Errorf("kind's Delete(%q, %q), want (%q, %q)", n, k, name, kubeconfig)
				}
				return tc.deleteErr
			}

			// The option to keep the nodes does not stop the delete of a failed attempt.
			err := createKind(ctx, nil, name, kubeconfig, kindcluster.CreateWithRetain(true))
			if creates != tc.creates || deletes != tc.deletes {
				t.Errorf("%d attempts and %d deletes, want %d and %d", creates, deletes, tc.creates, tc.deletes)
			}
			if tc.want == "" {
				if err != nil {
					t.Errorf("createKind: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("createKind gave no error, want %q", tc.want)
			}
			if err.Error() != tc.want {
				t.Errorf("error %q, want %q", err, tc.want)
			}
		})
	}
}

// Step 6 of Create: the wait for the nodes counts limits.timeout from its own start, so a cluster
// whose creation was slow does not shorten it.
func TestWaitReadyCountsItsOwnLimit(t *testing.T) {
	const limit = 1500 * time.Millisecond
	start := time.Now()
	err := waitReady(context.Background(), k8sfake.NewClientset(), 1, limit)
	took := time.Since(start)
	if err == nil {
		t.Fatal("waitReady gave no error")
	}
	if !strings.Contains(err.Error(), "at the deadline 0 of 1 nodes were Ready") {
		t.Errorf("error %q, want it to give the counts at the deadline", err)
	}
	if took < limit || took >= 3*time.Second {
		t.Errorf("waitReady took %v, want at least %v and less than 3s", took, limit)
	}
}

// Step 7 of Create: a namespace that exists already, such as default, counts as made; any other
// failure is the step's.
func TestEnsureNamespaces(t *testing.T) {
	ctx := context.Background()
	client := k8sfake.NewClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}})
	if err := ensureNamespaces(ctx, client, ProbeNamespace, "default"); err != nil {
		t.Fatalf("ensureNamespaces(%s, default): %v", ProbeNamespace, err)
	}
	for _, name := range []string{ProbeNamespace, "default"} {
		if _, err := client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{}); err != nil {
			t.Errorf("namespace %s: %v", name, err)
		}
	}

	refused := k8sfake.NewClientset()
	refused.PrependReactor("create", "namespaces", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("the server is shutting down")
	})
	if err := ensureNamespaces(ctx, refused, ProbeNamespace); err == nil {
		t.Error("ensureNamespaces gave no error when the server refused the create")
	}
}

// Kept names what a kept cluster holds: its kubeconfig file and that file's context, kind's context
// name before Create set one, and no path when kind wrote no file.
func TestKept(t *testing.T) {
	dir := t.TempDir()
	c := &Cluster{Name: "rollproof-test-kept", RunDir: dir, Kubeconfig: filepath.Join(dir, "kubeconfig")}
	if got, want := c.Kept(), "the cluster is kept, and kind wrote no kubeconfig file"; got != want {
		t.Errorf("Kept() = %q, want %q", got, want)
	}
	if err := os.WriteFile(c.Kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := c.Kept(), "the cluster is kept: kubeconfig "+c.Kubeconfig+", context kind-rollproof-test-kept"; got != want {
		t.Errorf("Kept() = %q, want %q", got, want)
	}
	c.Context = "kind-rollproof-test-kept-app"
	if got, want := c.Kept(), "the cluster is kept: kubeconfig "+c.Kubeconfig+", context kind-rollproof-test-kept-app"; got != want {
		t.Errorf("Kept() = %q, want %q", got, want)
	}
}

// Create's failure texts, with kind's Create and Delete replaced and an engine command that does
// not exist (which remove reads as a registry container that is gone): each reason stays on one
// line, and a clean-up that fails says that parts of the run may be left behind.
func TestCreateFailureTexts(t *testing.T) {
	const name = "rollproof-test-unit"
	gone := errors.New("the engine is gone")
	for _, tc := range []struct {
		name            string
		setup           func(t *testing.T, runDir string)
		keep            bool    // Spec.Keep
		writeKubeconfig bool    // what kind's Create writes before it fails
		creates         []error // what each attempt of kind's Create returns, in order
		deletes         []error // what each call of kind's Delete returns, in order: step 1's first
		want            func(runDir string) string
		joined          error // an error that the returned error holds beside the Condition, for the log
	}{
		{
			name:    "the clean-up after a failed step fails",
			creates: []error{errors.New("exit status 125")},
			deletes: []error{nil, gone},
			want: func(string) string {
				return "create the kind cluster: exit status 125" + inotifyHint + LeftBehind
			},
			joined: gone,
		},
		{
			name:            "a kept failure after kind's creation began",
			keep:            true,
			writeKubeconfig: true,
			creates:         []error{errors.New("exit status 125")},
			deletes:         []error{nil}, // step 1's removal of leftovers
			want: func(runDir string) string {
				return "create the kind cluster: exit status 125" + inotifyHint + ". The cluster is kept: kubeconfig " +
					filepath.Join(runDir, "kubeconfig") + ", context kind-" + name
			},
		},
		{
			name:    "the delete of a failed attempt fails",
			creates: []error{missedLine(1)},
			deletes: []error{nil, errors.New("exit status 1"), nil},
			want: func(string) string {
				return "create the kind cluster: " + missedLine(1).Error() + "; delete the failed attempt 1: exit status 1" + inotifyHint
			},
		},
		{
			name: "two removals of leftovers fail",
			setup: func(t *testing.T, runDir string) {
				if err := os.MkdirAll(filepath.Join(runDir, "kubeconfig.lock", "x"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
			deletes: []error{gone},
			want: func(runDir string) string {
				return "remove leftovers: remove " + filepath.Join(runDir, "kubeconfig.lock") + ": directory not empty; delete the kind cluster " + name + ": the engine is gone"
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldCreate, oldDelete := kindCreate, kindDelete
			t.Cleanup(func() { kindCreate, kindDelete = oldCreate, oldDelete })
			var creates, deletes int
			runDir := filepath.Join(t.TempDir(), name)
			kindCreate = func(*kindcluster.Provider, string, ...kindcluster.CreateOption) error {
				if creates == len(tc.creates) {
					t.Fatalf("attempt %d of kind's Create, want %d", creates+1, len(tc.creates))
				}
				creates++
				if tc.writeKubeconfig {
					if err := os.WriteFile(filepath.Join(runDir, "kubeconfig"), []byte("apiVersion: v1\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				return tc.creates[creates-1]
			}
			kindDelete = func(*kindcluster.Provider, string, string) error {
				if deletes == len(tc.deletes) {
					t.Fatalf("delete %d of kind's Delete, want %d", deletes+1, len(tc.deletes))
				}
				deletes++
				return tc.deletes[deletes-1]
			}
			if tc.setup != nil {
				tc.setup(t, runDir)
			}
			s := Spec{Name: name, RunDir: runDir, Workers: 1, Namespace: "app", Timeout: time.Minute, Keep: tc.keep}

			_, err := Create(context.Background(), s, Engine{Binary: "rollproof-test-no-such-engine"})
			if err == nil {
				t.Fatal("Create gave no error")
			}
			c := verdict.AsCondition(err)
			if c.Code != verdict.ClusterFailed || c.Text != tc.want(runDir) {
				t.Errorf("Create = %s: %q\nwant cluster-failed: %q", c.Code, c.Text, tc.want(runDir))
			}
			if strings.Contains(c.Text, "\n") {
				t.Errorf("the reason spans lines: %q", c.Text)
			}
			if tc.joined != nil && !errors.Is(err, tc.joined) {
				t.Errorf("the error %q does not hold %q", err, tc.joined)
			}
			if creates != len(tc.creates) || deletes != len(tc.deletes) {
				t.Errorf("%d creates and %d deletes, want %d and %d", creates, deletes, len(tc.creates), len(tc.deletes))
			}
		})
	}
}

// What a node said when it refused a setting goes into a reason on one line.
func TestOneLine(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"nothing said", "", ""},
		{"one line with a line end", "sysctl: permission denied on key \"net.netfilter.x\"\n", "sysctl: permission denied on key \"net.netfilter.x\""},
		{"two lines", "sysctl: cannot stat /proc/sys/net/x:\n  No such file or directory\n", "sysctl: cannot stat /proc/sys/net/x: No such file or directory"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := oneLine([]byte(c.in)); got != c.want {
				t.Errorf("oneLine(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
