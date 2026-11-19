//go:build linux && cluster

package probe

import (
	"bytes"
	"context"
	"debug/elf"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"rollproof/internal/cluster"
	"rollproof/internal/images"
	"rollproof/internal/verdict"
)

// TestMain lets this test binary be the probe's image: started as
// "probe http …" it runs the built-in probe, as the tool's binary does.
func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "probe" && os.Args[2] == "http" {
		os.Exit(probeMain(os.Args[1:])) // the whole list, as ParseHTTPArgs wants it (ruling A1/C1)
	}
	os.Exit(m.Run())
}

func probeMain(args []string) int {
	o, err := ParseHTTPArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	if err := RunHTTP(ctx, o, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func TestPodOnACluster(t *testing.T) {
	ctx := context.Background()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f, err := elf.Open(bin)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			t.Fatal("the test binary is not static: run this test with env CGO_ENABLED=0")
		}
	}
	f.Close()

	engine, err := cluster.CheckRunner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const name = "rollproof-test-probe"
	c, err := cluster.Create(ctx, cluster.Spec{Name: name, RunDir: cluster.RunDir(name), Workers: 1, Namespace: "probe-test", Timeout: 5 * time.Minute}, engine)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Delete(context.Background()); err != nil {
			t.Error(err)
		}
	})
	stored, err := images.Store(ctx, c, bin, nil, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	client, err := c.Client()
	if err != nil {
		t.Fatal(err)
	}

	o := HTTPOptions{URL: "http://" + c.NodeRegistry + "/v2/", Rate: 20, Timeout: 2 * time.Second, ExpectMin: 200, ExpectMax: 299}
	var raw bytes.Buffer
	p := NewPod(PodOptions{
		Client: client, Image: stored.Probe.Ref(), Args: HTTPArgs(o),
		Drain: o.Timeout + 5*time.Second, StartTimeout: 60 * time.Second, Raw: &raw, Now: time.Now,
	})
	st, err := p.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Stop(context.Background()) }) // a test that fails below leaves no probe running

	// Store pulls the probe's image into c.Nodes()[0] alone, so the pod must run there, and that
	// node must be the control-plane node.
	pod, err := client.CoreV1().Pods(cluster.ProbeNamespace).Get(ctx, "probe", metav1.GetOptions{})
	if err != nil {
		t.Error(err)
	} else if pod.Spec.NodeName != c.Nodes()[0] {
		t.Errorf("the probe's pod runs on %q, want %q, the node that holds its image", pod.Spec.NodeName, c.Nodes()[0])
	}
	node, err := client.CoreV1().Nodes().Get(ctx, c.Nodes()[0], metav1.GetOptions{})
	if err != nil {
		t.Error(err)
	} else if _, ok := node.Labels["node-role.kubernetes.io/control-plane"]; !ok {
		t.Errorf("node %s has no control-plane label: %v", node.Name, node.Labels)
	}

	passed, failed := 0, 0
	until := time.After(6 * time.Second)
read:
	for {
		select {
		case e := <-p.Events():
			switch {
			case e.Fault != nil:
				t.Errorf("fault: %v", *e.Fault)
			case !e.Obs.Written.Before(st.First.Add(5 * time.Second)):
				// written after the first 5 s: not counted
			case e.Obs.OK:
				passed += e.Obs.N
			default:
				failed++
			}
		case <-until:
			break read
		}
	}

	began := time.Now()
	facts, err := p.Stop(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for e := range p.Events() {
		if e.Fault != nil {
			t.Errorf("fault in the drain: %v", *e.Fault)
		}
	}
	if took := time.Since(began); took > o.Timeout+5*time.Second {
		t.Errorf("Stop took %v, longer than the drain", took)
	}
	if !facts.StoppedWhenAsked {
		t.Error("the probe did not stop when asked")
	}
	if passed < 80 || passed > 120 || failed != 0 {
		t.Errorf("%d checks passed and %d failed in the first 5 s, want 80 to 120 and none", passed, failed)
	}
	if facts.ClockDifference.Abs() > ClockBound {
		t.Errorf("clock difference %v, more than %v", facts.ClockDifference, ClockBound)
	}
	t.Logf("clock check: the first observation was read %v after the runtime stamped it", facts.ClockDifference)
}

// TestCustomPodOnACluster runs a custom probe in a namespace that enforces the restricted Pod
// Security level, one whose env the API server refuses, and one whose env names a Secret that does
// not exist. The first shows the pod's settings, its token's mount and its evidence; the second is
// a refused pod at once; the third names the kubelet's message for the missing Secret.
func TestCustomPodOnACluster(t *testing.T) {
	ctx := context.Background()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f, err := elf.Open(bin)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			t.Fatal("the test binary is not static: run this test with env CGO_ENABLED=0")
		}
	}
	f.Close()

	engine, err := cluster.CheckRunner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const name = "rollproof-test-custom-probe"
	c, err := cluster.Create(ctx, cluster.Spec{Name: name, RunDir: cluster.RunDir(name), Workers: 1, Namespace: "custom-probe-test", Timeout: 5 * time.Minute}, engine)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Delete(context.Background()); err != nil {
			t.Error(err)
		}
	})
	stored, err := images.Store(ctx, c, bin, nil, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	client, err := c.Client()
	if err != nil {
		t.Fatal(err)
	}

	// The namespace enforces the restricted Pod Security level (section 6): a pod the tool writes
	// with the built-in probe's settings is admitted there.
	ns, err := client.CoreV1().Namespaces().Get(ctx, cluster.ProbeNamespace, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ns.Labels["pod-security.kubernetes.io/enforce"] = "restricted"
	if _, err := client.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	o := HTTPOptions{URL: "http://" + c.NodeRegistry + "/v2/", Rate: 20, Timeout: 2 * time.Second, ExpectMin: 200, ExpectMax: 299}
	custom := func() PodOptions {
		return PodOptions{
			Client: client, Image: stored.Probe.Ref(), Command: []string{"/rollproof"}, Args: HTTPArgs(o),
			ServiceAccountToken: true, Drain: 10 * time.Second, StartTimeout: 30 * time.Second,
			Raw: &bytes.Buffer{}, Now: time.Now,
		}
	}

	// 1. A pod that meets the restricted level starts, mounts the default service account's
	// token, and its evidence holds the pod as the API wrote it.
	var ev evidenceFiles
	o1 := custom()
	o1.StartTimeout = 60 * time.Second
	o1.Evidence = ev.open
	p := NewPod(o1)
	if _, err := p.Start(ctx); err != nil {
		t.Fatalf("Start = %v; the pod does not meet the restricted Pod Security level", err)
	}
	t.Cleanup(func() { p.Stop(context.Background()) })
	pod, err := client.CoreV1().Pods(cluster.ProbeNamespace).Get(ctx, "probe", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mounted := false
	for _, c := range pod.Spec.Containers {
		if c.Name != "probe" {
			continue
		}
		for _, m := range c.VolumeMounts {
			if m.MountPath == "/var/run/secrets/kubernetes.io/serviceaccount" {
				mounted = true
			}
		}
	}
	if !mounted {
		t.Errorf("the probe's container mounts %+v, want the default service account's token", pod.Spec.Containers)
	}
	facts, err := p.Stop(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !facts.StoppedWhenAsked {
		t.Error("the probe did not stop when asked")
	}
	if got, _ := ev.get("probe-pod.json"); !strings.Contains(got, `"automountServiceAccountToken": true`) {
		t.Errorf("probe-pod.json = %q, want the pod with the token mounted", got)
	}

	// 2. A valueFrom with two sources is a pod the API server refuses.
	o2 := custom()
	o2.Env = []corev1.EnvVar{{Name: "TOKEN", ValueFrom: &corev1.EnvVarSource{
		SecretKeyRef:    &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "probe-token"}, Key: "token"},
		ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "probe"}, Key: "token"},
	}}}
	p2 := NewPod(o2)
	t.Cleanup(func() { p2.Stop(context.Background()) })
	done := make(chan error, 1)
	go func() {
		_, err := p2.Start(ctx)
		done <- err
	}()
	select {
	case err := <-done:
		got := condition(err)
		if got.Code != verdict.ProbeNotStarted || !strings.HasPrefix(got.Text, "the API server refused the probe's pod: ") ||
			!strings.Contains(got.Text, "may not have more than one field specified at a time") {
			t.Errorf("Start = %v, want probe-not-started with the API server's text", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Start did not return within 10 s")
	}

	// 3. A Secret that does not exist keeps the container waiting; the text names its reason and
	// the kubelet's message (Review Focus 2).
	o3 := custom()
	o3.Env = []corev1.EnvVar{{Name: "TOKEN", ValueFrom: &corev1.EnvVarSource{
		SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "probe-token"}, Key: "token"},
	}}}
	p3 := NewPod(o3)
	t.Cleanup(func() { p3.Stop(context.Background()) })
	_, err = p3.Start(ctx)
	got := condition(err)
	if got.Code != verdict.ProbeNotStarted || !strings.Contains(got.Text, "CreateContainerConfigError") ||
		!strings.Contains(got.Text, `secret "probe-token" not found`) {
		t.Fatalf("Start = %v, want probe-not-started that names the missing Secret", err)
	}
	t.Logf("case 3: %s", got.Text)
}
