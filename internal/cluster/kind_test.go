//go:build linux

package cluster

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	kindcluster "sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cluster/nodes"
	"sigs.k8s.io/kind/pkg/exec"

	"github.com/nzneit/rollproof/internal/config"
	"github.com/nzneit/rollproof/internal/verdict"
)

func TestUntilDone(t *testing.T) {
	t.Run("the call returns first", func(t *testing.T) {
		want := errors.New("kind said no")
		if err := untilDone(context.Background(), func() error { return want }); err != want {
			t.Errorf("untilDone = %v, want %v", err, want)
		}
	})

	t.Run("a context that has ended", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		called := false
		err := untilDone(ctx, func() error {
			called = true
			return nil
		})
		if err != context.Canceled {
			t.Errorf("untilDone = %v, want %v", err, context.Canceled)
		}
		if called {
			t.Error("the call was made with a context that had ended")
		}
	})

	t.Run("the context ends during the call", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		began, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
		cancelled := make(chan time.Time, 1)
		go func() {
			<-began
			time.Sleep(50 * time.Millisecond)
			cancelled <- time.Now()
			cancel()
		}()
		err := untilDone(ctx, func() error {
			defer close(returned)
			close(began)
			<-release
			return nil
		})
		back := time.Now()
		if err != context.Canceled {
			t.Errorf("untilDone = %v, want %v", err, context.Canceled)
		}
		if took := back.Sub(<-cancelled); took >= time.Second {
			t.Errorf("untilDone returned %v after the cancel, want within 1s", took)
		}
		close(release)
		select {
		case <-returned:
		case <-time.After(time.Second):
			t.Error("the call did not return within 1s of its release")
		}
	})

	t.Run("a panic of a call that returns first", func(t *testing.T) {
		var got any
		func() {
			defer func() { got = recover() }()
			_ = untilDone(context.Background(), func() error { panic("kind broke") })
		}()
		if got != "kind broke" {
			t.Errorf("untilDone panicked with %v, want %q", got, "kind broke")
		}
	})

	t.Run("a panic after the context won", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		began, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
		go func() {
			<-began
			cancel()
		}()
		err := untilDone(ctx, func() error {
			defer close(finished)
			close(began)
			<-release
			panic("kind broke late")
		})
		if err != context.Canceled {
			t.Errorf("untilDone = %v, want %v", err, context.Canceled)
		}
		close(release)
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("the call did not end within 1s of its release")
		}
		// A panic that the goroutine did not recover would end the test binary meanwhile.
		time.Sleep(100 * time.Millisecond)
	})
}

// fakeNode is a node of kind with a name and a role, whose commands do nothing and succeed.
type fakeNode struct{ name, role string }

func (n fakeNode) String() string                                             { return n.name }
func (n fakeNode) Role() (string, error)                                      { return n.role, nil }
func (n fakeNode) IP() (string, string, error)                                { return "", "", nil }
func (n fakeNode) SerialLogs(io.Writer) error                                 { return nil }
func (n fakeNode) Command(string, ...string) exec.Cmd                         { return fakeCmd{} }
func (n fakeNode) CommandContext(context.Context, string, ...string) exec.Cmd { return fakeCmd{} }

// fakeCmd is a command of a fakeNode: it runs and succeeds, and its setters change nothing.
type fakeCmd struct{}

func (c fakeCmd) Run() error                   { return nil }
func (c fakeCmd) SetEnv(...string) exec.Cmd    { return c }
func (c fakeCmd) SetStdin(io.Reader) exec.Cmd  { return c }
func (c fakeCmd) SetStdout(io.Writer) exec.Cmd { return c }
func (c fakeCmd) SetStderr(io.Writer) exec.Cmd { return c }

// replaceKindSeams saves every seam of kind's calls and the node image's pull, and restores them
// after the test.
func replaceKindSeams(t *testing.T) {
	t.Helper()
	oldCreate, oldDelete, oldCollect, oldContainer := kindCreate, kindDelete, kindCollectLogs, containerLogs
	oldPull, oldList, oldRole, oldWrite := pullImage, kindListNodes, nodeRole, writeNodeFile
	t.Cleanup(func() {
		kindCreate, kindDelete, kindCollectLogs, containerLogs = oldCreate, oldDelete, oldCollect, oldContainer
		pullImage, kindListNodes, nodeRole, writeNodeFile = oldPull, oldList, oldRole, oldWrite
	})
}

// blocker is a call into kind that does not return until the test ends.
type blocker struct {
	entered, release chan struct{}
	once             sync.Once
}

// newBlocker returns a blocker whose release comes in a t.Cleanup. Called after replaceKindSeams,
// that release runs before the seams are restored.
func newBlocker(t *testing.T) *blocker {
	b := &blocker{entered: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(func() { close(b.release) })
	return b
}

// block closes entered and waits until the test ends, and then returns the error "released".
func (b *blocker) block() error {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return errors.New("released")
}

// cancelWhenEntered runs f in a goroutine with a context that it cancels as soon as b is entered,
// and fails the test unless f returns within 2 s of the cancel. It returns f's error.
func cancelWhenEntered(t *testing.T, b *blocker, f func(ctx context.Context) error) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- f(ctx) }()
	select {
	case <-b.entered:
	case err := <-result:
		t.Fatalf("the function returned %v before the call that blocks was entered", err)
	case <-time.After(10 * time.Second):
		t.Fatal("the call that blocks was not entered within 10s")
	}
	cancel()
	select {
	case err := <-result:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("the function did not return within 2s of the cancel")
		return nil
	}
}

func TestASignalDuringEachCallIntoKind(t *testing.T) {
	const name = "rollproof-test-unit"
	spec := func(t *testing.T) Spec {
		return Spec{Name: name, RunDir: filepath.Join(t.TempDir(), name), Workers: 1, Namespace: "app",
			Timeout: time.Minute}
	}
	engine := Engine{Binary: "rollproof-test-no-such-engine"}
	// create runs Create with s until b is entered, cancels it then, and returns its condition.
	create := func(t *testing.T, b *blocker, s Spec) verdict.Condition {
		t.Helper()
		pullImage = func(Engine, context.Context, string, time.Duration) error { return nil }
		err := cancelWhenEntered(t, b, func(ctx context.Context) error {
			_, err := Create(ctx, s, engine)
			return err
		})
		c := verdict.AsCondition(err)
		if c.Code != verdict.ClusterFailed {
			t.Errorf("Create = %s: %q, want cluster-failed", c.Code, c.Text)
		}
		return c
	}
	wantPrefix := func(t *testing.T, c verdict.Condition, want string) {
		t.Helper()
		if !strings.HasPrefix(c.Text, want) {
			t.Errorf("Create's text is %q, want it to begin %q", c.Text, want)
		}
	}

	t.Run("the removal of an earlier run's cluster", func(t *testing.T) {
		replaceKindSeams(t)
		b := newBlocker(t)
		var deletes, creates atomic.Int32
		kindDelete = func(*kindcluster.Provider, string, string) error {
			if deletes.Add(1) == 1 {
				return b.block()
			}
			return nil
		}
		kindCreate = func(*kindcluster.Provider, string, ...kindcluster.CreateOption) error {
			creates.Add(1)
			return nil
		}
		c := create(t, b, spec(t))
		wantPrefix(t, c, "remove leftovers: delete the kind cluster rollproof-test-unit: context canceled")
		if n := creates.Load(); n != 0 {
			t.Errorf("kind's Create was called %d times, want 0", n)
		}
	})

	t.Run("kind's creation", func(t *testing.T) {
		replaceKindSeams(t)
		b := newBlocker(t)
		var deletes, collects atomic.Int32
		kindDelete = func(*kindcluster.Provider, string, string) error {
			deletes.Add(1)
			return nil
		}
		kindCreate = func(*kindcluster.Provider, string, ...kindcluster.CreateOption) error { return b.block() }
		kindCollectLogs = func(*kindcluster.Provider, string, string) error {
			collects.Add(1)
			return nil
		}
		c := create(t, b, spec(t))
		wantPrefix(t, c, "create the kind cluster: context canceled")
		if n := deletes.Load(); n != 1 {
			t.Errorf("kind's Delete was called %d times, want 1", n)
		}
		if n := collects.Load(); n != 0 {
			t.Errorf("kind's log collection was called %d times, want 0", n)
		}
	})

	t.Run("the delete of a failed attempt", func(t *testing.T) {
		replaceKindSeams(t)
		b := newBlocker(t)
		var deletes, creates atomic.Int32
		kindDelete = func(*kindcluster.Provider, string, string) error {
			if deletes.Add(1) == 2 {
				return b.block()
			}
			return nil
		}
		kindCreate = func(*kindcluster.Provider, string, ...kindcluster.CreateOption) error {
			creates.Add(1)
			return missedLine(1)
		}
		c := create(t, b, spec(t))
		wantPrefix(t, c, "create the kind cluster: "+missedLine(1).Error()+"; delete the failed attempt 1: context canceled")
		if n := creates.Load(); n != 1 {
			t.Errorf("kind's Create was called %d times, want 1", n)
		}
	})

	// logsRow is the set-up of the two rows whose kind's Create fails with exit status 125 and whose
	// evidence is written into a folder of its own; it returns that folder and a count of the calls
	// of containerLogs.
	logsRow := func(t *testing.T, s *Spec) (evDir string, containers *atomic.Int32) {
		evDir = t.TempDir()
		s.Evidence = evidenceDir(evDir)
		kindCreate = func(*kindcluster.Provider, string, ...kindcluster.CreateOption) error {
			return errors.New("exit status 125")
		}
		containers = &atomic.Int32{}
		containerLogs = func(Engine, context.Context, string) (string, bool, error) {
			containers.Add(1)
			return "", false, nil
		}
		return evDir, containers
	}

	t.Run("the log collection of a failed set-up", func(t *testing.T) {
		replaceKindSeams(t)
		b := newBlocker(t)
		s := spec(t)
		evDir, containers := logsRow(t, &s)
		var deletes atomic.Int32
		kindDelete = func(*kindcluster.Provider, string, string) error {
			deletes.Add(1)
			return nil
		}
		kindCollectLogs = func(_ *kindcluster.Provider, _, dir string) error {
			if err := writeLogFile(filepath.Join(dir, "kind-version.txt"), "v0.33.0"); err != nil {
				t.Errorf("kind's log collection: %v", err)
				return err
			}
			return b.block()
		}
		c := create(t, b, s)
		wantPrefix(t, c, "create the kind cluster: exit status 125")
		if got, err := os.ReadFile(filepath.Join(s.RunDir, "kind-logs", "kind-version.txt")); err != nil || string(got) != "v0.33.0" {
			t.Errorf("kind-logs/kind-version.txt holds %q, %v; want it kept with v0.33.0", got, err)
		}
		if got := filesUnder(t, evDir); len(got) != 0 {
			t.Errorf("the evidence holds %v, want no file", got)
		}
		if n := containers.Load(); n != 0 {
			t.Errorf("the registry's log was read %d times, want 0", n)
		}
		if n := deletes.Load(); n != 1 {
			t.Errorf("kind's Delete was called %d times, want 1 (the leftovers')", n)
		}
	})

	t.Run("the removal of a cluster whose set-up failed", func(t *testing.T) {
		replaceKindSeams(t)
		b := newBlocker(t)
		s := spec(t)
		logsRow(t, &s)
		var deletes atomic.Int32
		kindDelete = func(*kindcluster.Provider, string, string) error {
			if deletes.Add(1) == 2 {
				return b.block()
			}
			return nil
		}
		kindCollectLogs = func(*kindcluster.Provider, string, string) error { return nil }
		c := create(t, b, s)
		wantPrefix(t, c, "create the kind cluster: exit status 125")
		if !strings.HasSuffix(c.Text, LeftBehind) {
			t.Errorf("Create's text is %q, want it to end %q", c.Text, LeftBehind)
		}
	})

	// settings runs writeNodeSettings until b is entered, cancels it then, and checks that it returns
	// no nodes and context.Canceled.
	settings := func(t *testing.T, b *blocker) {
		t.Helper()
		var listed []nodes.Node
		err := cancelWhenEntered(t, b, func(ctx context.Context) error {
			var err error
			listed, err = writeNodeSettings(ctx, nil, name, nil, nil, "10.89.0.5:5000")
			return err
		})
		if listed != nil || err != context.Canceled {
			t.Errorf("writeNodeSettings = %v, %v; want nil, %v", listed, err, context.Canceled)
		}
	}
	controlPlane := []nodes.Node{fakeNode{"rollproof-test-unit-control-plane", "control-plane"}}

	t.Run("the node list", func(t *testing.T) {
		replaceKindSeams(t)
		b := newBlocker(t)
		kindListNodes = func(*kindcluster.Provider, string) ([]nodes.Node, error) { return nil, b.block() }
		settings(t, b)
	})

	t.Run("a node's file", func(t *testing.T) {
		replaceKindSeams(t)
		b := newBlocker(t)
		kindListNodes = func(*kindcluster.Provider, string) ([]nodes.Node, error) { return controlPlane, nil }
		writeNodeFile = func(nodes.Node, string, string) error { return b.block() }
		settings(t, b)
	})

	t.Run("a node's role", func(t *testing.T) {
		replaceKindSeams(t)
		b := newBlocker(t)
		kindListNodes = func(*kindcluster.Provider, string) ([]nodes.Node, error) { return controlPlane, nil }
		writeNodeFile = func(nodes.Node, string, string) error { return nil }
		nodeRole = func(nodes.Node) (string, error) { return "", b.block() }
		settings(t, b)
	})
}

func TestWriteNodeSettingsFailures(t *testing.T) {
	const name = "rollproof-test-unit"
	t.Run("a ca that cannot be read", func(t *testing.T) {
		replaceKindSeams(t)
		listed := false
		kindListNodes = func(*kindcluster.Provider, string) ([]nodes.Node, error) {
			listed = true
			return nil, nil
		}
		ca := filepath.Join(t.TempDir(), "missing.pem")
		_, err := writeNodeSettings(context.Background(), nil, name,
			[]config.Registry{{Host: "registry.example", CA: ca}}, nil, "10.89.0.5:5000")
		want := "registries[0].ca: open " + ca + ": no such file or directory"
		if err == nil || err.Error() != want {
			t.Errorf("writeNodeSettings = %v, want %q", err, want)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("the error %v does not wrap fs.ErrNotExist", err)
		}
		if listed {
			t.Error("the nodes were listed before the ca was read")
		}
	})

	t.Run("a node's file that cannot be written", func(t *testing.T) {
		replaceKindSeams(t)
		kindListNodes = func(*kindcluster.Provider, string) ([]nodes.Node, error) {
			return []nodes.Node{fakeNode{"n1", "worker"}}, nil
		}
		writeNodeFile = func(nodes.Node, string, string) error { return errors.New("disk full") }
		_, err := writeNodeSettings(context.Background(), nil, name, nil, nil, "10.89.0.5:5000")
		if want := "node n1: disk full"; err == nil || err.Error() != want {
			t.Errorf("writeNodeSettings = %v, want %q", err, want)
		}
	})

	t.Run("the nodes in order", func(t *testing.T) {
		replaceKindSeams(t)
		kindListNodes = func(*kindcluster.Provider, string) ([]nodes.Node, error) {
			return []nodes.Node{fakeNode{"b", "worker"}, fakeNode{"c", "control-plane"}, fakeNode{"a", "worker"}}, nil
		}
		var mu sync.Mutex
		var written []string
		writeNodeFile = func(n nodes.Node, path, _ string) error {
			mu.Lock()
			defer mu.Unlock()
			written = append(written, n.String()+" "+path)
			return nil
		}
		got, err := writeNodeSettings(context.Background(), nil, name, nil, nil, "10.89.0.5:5000")
		if err != nil {
			t.Fatalf("writeNodeSettings: %v", err)
		}
		var names []string
		for _, n := range got {
			names = append(names, n.String())
		}
		if want := []string{"c", "a", "b"}; !slices.Equal(names, want) {
			t.Errorf("the nodes are %v, want %v", names, want)
		}
		const path = "/etc/containerd/certs.d/rollproof.test/hosts.toml"
		mu.Lock()
		defer mu.Unlock()
		slices.Sort(written)
		if want := []string{"a " + path, "b " + path, "c " + path}; !slices.Equal(written, want) {
			t.Errorf("the files written are %q, want %q", written, want)
		}
	})
}
