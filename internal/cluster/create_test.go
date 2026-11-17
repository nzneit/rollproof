//go:build linux

package cluster

import (
	"context"
	"errors"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	kindcluster "sigs.k8s.io/kind/pkg/cluster"
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
			missedLine(1).Error() + "\ndelete the failed attempt 1: exit status 1"},
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
