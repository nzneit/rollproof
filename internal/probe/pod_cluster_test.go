//go:build linux && cluster

package probe

import (
	"bytes"
	"context"
	"debug/elf"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"rollproof/internal/cluster"
	"rollproof/internal/images"
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
		case e := <-st.Events:
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
	for range st.Events {
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
