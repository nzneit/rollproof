//go:build linux && cluster

// These tests need a container engine and make one kind cluster.
//
// Under Docker the engine's archive has another layout than under nerdctl. This test is the proof
// that Store reads it; it runs on a hosted runner when the maintainer says so.
package images

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"rollproof/internal/cluster"
	"rollproof/internal/config"
	"rollproof/internal/verdict"
)

var (
	shared     *cluster.Cluster
	sharedErr  error
	sharedOnce sync.Once
)

// testCluster creates, once for the package, a cluster with one worker; TestMain deletes it.
func testCluster(t *testing.T) *cluster.Cluster {
	t.Helper()
	sharedOnce.Do(func() {
		e, err := cluster.CheckRunner(context.Background())
		if err != nil {
			sharedErr = err
			return
		}
		n := "rollproof-test-images"
		shared, sharedErr = cluster.Create(context.Background(), cluster.Spec{
			Name: n, RunDir: cluster.RunDir(n), Workers: 1, Namespace: "app", Timeout: 5 * time.Minute}, e)
	})
	if sharedErr != nil {
		t.Fatalf("test cluster: %v", sharedErr)
	}
	return shared
}

func TestMain(m *testing.M) {
	code := m.Run()
	if shared != nil {
		if err := shared.Delete(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, "delete the test cluster:", err)
			code = 1
		}
	}
	os.Exit(code)
}

// tinyProgram builds a static program that prints ok and sleeps, and returns its folder and path.
// -trimpath keeps the temporary folder's name out of the binary, so that its bytes repeat from
// run to run and BuildKit reuses its record of the image's COPY.
func tinyProgram(t *testing.T) (dir, bin string) {
	t.Helper()
	dir = t.TempDir()
	src := "package main\n\nimport (\n\t\"fmt\"\n\t\"time\"\n)\n\nfunc main() {\n\tfmt.Println(\"ok\")\n\ttime.Sleep(time.Hour)\n}\n"
	for f, body := range map[string]string{"go.mod": "module app\n\ngo 1.27\n", "main.go": src} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin = filepath.Join(dir, "app")
	cmd := exec.Command("go", "build", "-trimpath", "-o", bin, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return dir, bin
}

func TestStoreOwnAndLocal(t *testing.T) {
	ctx := context.Background()
	c := testCluster(t)
	dir, bin := tinyProgram(t)
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\nCOPY app /app\nENTRYPOINT [\"/app\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(c.Engine.Binary, "build", "-t", "rollproof-test-img:one", dir).CombinedOutput(); err != nil {
		t.Fatalf("build the local image: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		if out, err := exec.Command(c.Engine.Binary, "rmi", "rollproof-test-img:one").CombinedOutput(); err != nil {
			t.Errorf("remove the local image rollproof-test-img:one: %v\n%s", err, out)
		}
	})

	s, err := Store(ctx, c, bin, map[string]config.Image{"app": {Local: "rollproof-test-img:one"}}, 2*time.Minute)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	app := s.Images["app"]
	if app.Repository != "rollproof.test/app" || !strings.HasPrefix(app.Digest, "sha256:") {
		t.Errorf("app stored as %+v", app)
	}
	for _, n := range c.Nodes() {
		if out, err := c.Exec(ctx, n, "crictl", "inspecti", app.Ref()); err != nil {
			t.Errorf("%s does not hold %s: %v\n%s", n, app.Ref(), err, out)
		}
	}
	if s.Probe.Repository != "rollproof.test/rollproof" || !strings.HasPrefix(s.Probe.Digest, "sha256:") {
		t.Errorf("probe stored as %+v", s.Probe)
	}
	if out, err := c.Exec(ctx, c.Nodes()[0], "crictl", "inspecti", s.Probe.Ref()); err != nil {
		t.Errorf("the control-plane node does not hold %s: %v\n%s", s.Probe.Ref(), err, out)
	}
	for _, repo := range []string{"app", "rollproof"} {
		if got := tags(t, c.PushRegistry, repo); len(got) != 0 {
			t.Errorf("the run's registry has tags %v for %s", got, repo)
		}
	}

	// Section 14, point 21: a pod's status names the digest it runs. If this fails, stop and report.
	client, err := c.Client()
	if err != nil {
		t.Fatal(err)
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "stored-app", Namespace: "app"},
		Spec: corev1.PodSpec{
			NodeName:   c.Nodes()[1],
			Containers: []corev1.Container{{Name: "app", Image: app.Ref(), ImagePullPolicy: corev1.PullIfNotPresent}},
		},
	}
	if _, err := client.CoreV1().Pods("app").Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		p, err := client.CoreV1().Pods("app").Get(ctx, "stored-app", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if p.Status.Phase == corev1.PodRunning && len(p.Status.ContainerStatuses) == 1 {
			if id := p.Status.ContainerStatuses[0].ImageID; !strings.Contains(id, app.Digest) {
				t.Errorf("imageID %q does not hold the digest %s", id, app.Digest)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the pod is %s after 2 minutes: %+v", p.Status.Phase, p.Status.ContainerStatuses)
		}
		time.Sleep(time.Second)
	}
}

func TestStoreArchiveIntoEveryNode(t *testing.T) {
	ctx := context.Background()
	c := testCluster(t)
	dir, bin := tinyProgram(t)
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\nCOPY app /app\nENTRYPOINT [\"/app\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(c.Engine.Binary, "build", "-t", "rollproof-test-img:archive", dir).CombinedOutput(); err != nil {
		t.Fatalf("build the image: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		if out, err := exec.Command(c.Engine.Binary, "rmi", "rollproof-test-img:archive").CombinedOutput(); err != nil {
			t.Errorf("remove the local image rollproof-test-img:archive: %v\n%s", err, out)
		}
	})
	file := filepath.Join(t.TempDir(), "app.tar")
	if out, err := exec.Command(c.Engine.Binary, "save", "-o", file, "rollproof-test-img:archive").CombinedOutput(); err != nil {
		t.Fatalf("save the image: %v\n%s", err, out)
	}

	s, err := Store(ctx, c, bin, map[string]config.Image{"app": {Archive: file}}, 2*time.Minute)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	app := s.Images["app"]
	if app.Repository != "rollproof.test/app" || !strings.HasPrefix(app.Digest, "sha256:") {
		t.Errorf("app stored as %+v", app)
	}
	for _, n := range c.Nodes() {
		if out, err := c.Exec(ctx, n, "crictl", "inspecti", app.Ref()); err != nil {
			t.Errorf("%s does not hold %s: %v\n%s", n, app.Ref(), err, out)
		}
	}
	if got := tags(t, c.PushRegistry, "app"); len(got) != 0 {
		t.Errorf("the run's registry has tags %v for app", got)
	}
}

func TestStoreUnknownLocalImage(t *testing.T) {
	c := testCluster(t)
	_, bin := tinyProgram(t)
	_, err := Store(context.Background(), c, bin, map[string]config.Image{"app": {Local: "rollproof-test-img:absent"}}, time.Minute)
	if err == nil {
		t.Fatal("Store of an image the engine does not hold gave no error")
	}
	if cond := verdict.AsCondition(err); cond.Code != verdict.ClusterFailed || !strings.Contains(cond.Text, "rollproof-test-img:absent") {
		t.Errorf("error = %v, want cluster-failed naming the image", err)
	}
}
