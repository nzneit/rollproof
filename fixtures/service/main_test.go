package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"
)

// client opens a new connection for every request, as the built-in probe does.
var client = &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 5 * time.Second}

func get(addr, path string) (int, string, error) {
	resp, err := client.Get("http://" + addr + path)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), err
}

// launch starts serve with env in a goroutine. stop stands for SIGTERM.
func launch(t *testing.T, env map[string]string) (ready <-chan string, stop context.CancelFunc, done <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r := make(chan string, 1)
	errc := make(chan error, 1)
	go func() { errc <- serve(ctx, func(k string) string { return env[k] }, r) }()
	return r, cancel, errc
}

// listening waits until serve listens and returns its address.
func listening(t *testing.T, ready <-chan string, done <-chan error) string {
	t.Helper()
	select {
	case addr := <-ready:
		return addr
	case err := <-done:
		t.Fatalf("serve returned before it listened: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not listen within 5 s")
	}
	return ""
}

func refused(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err == nil {
		conn.Close()
	}
	return errors.Is(err, syscall.ECONNREFUSED)
}

func TestWorkAndReadyz(t *testing.T) {
	version = "v-test"
	ready, _, done := launch(t, map[string]string{"ADDR": "127.0.0.1:0", "SHUTDOWN": "drain-now"})
	addr := listening(t, ready, done)
	began := time.Now()
	code, body, err := get(addr, "/work?ms=20")
	if err != nil || code != 200 {
		t.Fatalf("/work?ms=20: %d, %v; want 200", code, err)
	}
	if d := time.Since(began); d < 20*time.Millisecond {
		t.Errorf("/work?ms=20 answered in %v, want 20 ms or more", d)
	}
	if !strings.Contains(body, "v-test") {
		t.Errorf("the body %q does not hold the version", body)
	}
	if code, _, err := get(addr, "/readyz"); err != nil || code != 200 {
		t.Errorf("/readyz: %d, %v; want 200", code, err)
	}
}

// With FAIL_WORK the handler fails every request while the pod still reports ready, and does
// nothing else: it does not wait on ms, set the pod's header, or add a line end to the body.
func TestWorkFailsWhenAsked(t *testing.T) {
	ready, _, done := launch(t, map[string]string{"ADDR": "127.0.0.1:0", "SHUTDOWN": "drain-now", "FAIL_WORK": "true"})
	addr := listening(t, ready, done)
	began := time.Now()
	resp, err := client.Get("http://" + addr + "/work?ms=2000")
	if err != nil {
		t.Fatalf("/work?ms=2000: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("/work?ms=2000: %v", err)
	}
	if resp.StatusCode != 500 || string(body) != "failing" {
		t.Errorf("/work?ms=2000: %d %q, want 500 %q", resp.StatusCode, body, "failing")
	}
	if d := time.Since(began); d >= 2*time.Second {
		t.Errorf("/work?ms=2000 answered in %v: the failing answer waited on ms", d)
	}
	if h := resp.Header.Get("X-Pod"); h != "" {
		t.Errorf("/work?ms=2000 has X-Pod %q, want none", h)
	}
	if code, _, err := get(addr, "/readyz"); err != nil || code != 200 {
		t.Errorf("/readyz: %d, %v; want 200", code, err)
	}
}

// GET /version answers the built-in version and a line end, also when FAIL_WORK is set.
func TestVersion(t *testing.T) {
	version = "v-test"
	ready, _, done := launch(t, map[string]string{"ADDR": "127.0.0.1:0", "SHUTDOWN": "drain-now", "FAIL_WORK": "true"})
	addr := listening(t, ready, done)
	code, body, err := get(addr, "/version")
	if err != nil || code != 200 || body != "v-test\n" {
		t.Errorf("/version: %d %q, %v; want 200 %q", code, body, err, "v-test\n")
	}
}

// After it listens, the server fails /work for FAIL_FOR, counted from FAIL_AFTER, while /readyz
// and /version answer as before, and the failures end on their own.
func TestFailsForAWhileAfterItListens(t *testing.T) {
	ready, _, done := launch(t, map[string]string{"ADDR": "127.0.0.1:0", "SHUTDOWN": "drain-now",
		"FAIL_AFTER": "300ms", "FAIL_FOR": "300ms"})
	addr := listening(t, ready, done)
	began := time.Now()

	time.Sleep(100*time.Millisecond - time.Since(began))
	if code, _, err := get(addr, "/work?ms=0"); err != nil || code != 200 {
		t.Fatalf("/work 100 ms after it listened: %d, %v; want 200", code, err)
	}

	time.Sleep(400*time.Millisecond - time.Since(began))
	code, body, err := get(addr, "/work?ms=0")
	if err != nil || code != 500 || body != "failing" {
		t.Errorf("/work 400 ms after it listened: %d %q, %v; want 500 %q", code, body, err, "failing")
	}
	if code, _, err := get(addr, "/readyz"); err != nil || code != 200 {
		t.Errorf("/readyz 400 ms after it listened: %d, %v; want 200", code, err)
	}
	if code, _, err := get(addr, "/version"); err != nil || code != 200 {
		t.Errorf("/version 400 ms after it listened: %d, %v; want 200", code, err)
	}

	time.Sleep(800*time.Millisecond - time.Since(began))
	if code, _, err := get(addr, "/work?ms=0"); err != nil || code != 200 {
		t.Errorf("/work 800 ms after it listened: %d, %v; want 200", code, err)
	}
}

// With JOB_SLEEP, serve sleeps and exits 0 as a Job does, and never listens.
func TestJobSleepEndsWithoutListening(t *testing.T) {
	began := time.Now()
	ready, _, done := launch(t, map[string]string{"ADDR": "127.0.0.1:0", "JOB_SLEEP": "200ms"})
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v, want nil", err)
		}
		if d := time.Since(began); d < 200*time.Millisecond {
			t.Errorf("serve returned after %v, want 200 ms or more", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return within 5 s")
	}
	select {
	case addr := <-ready:
		t.Errorf("serve listened on %s, want no listen", addr)
	default:
	}
}

// A FAIL_AFTER that does not parse is an error that names the variable.
func TestBadFailAfter(t *testing.T) {
	env := map[string]string{"ADDR": "127.0.0.1:0", "FAIL_AFTER": "soon"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := serve(ctx, func(k string) string { return env[k] }, nil)
	if err == nil || !strings.Contains(err.Error(), "FAIL_AFTER") {
		t.Errorf("serve with FAIL_AFTER=soon = %v, want an error that holds FAIL_AFTER", err)
	}
}

func TestStartDelay(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	began := time.Now()
	ready, _, done := launch(t, map[string]string{"ADDR": addr, "START_DELAY": "300ms", "SHUTDOWN": "drain-now"})
	time.Sleep(100 * time.Millisecond)
	if !refused(addr) {
		t.Error("a dial 100 ms after the start was not refused")
	}
	listening(t, ready, done)
	if d := time.Since(began); d < 300*time.Millisecond {
		t.Errorf("it listened after %v, want 300 ms or more", d)
	}
	if code, _, err := get(addr, "/readyz"); err != nil || code != 200 {
		t.Errorf("/readyz: %d, %v; want 200", code, err)
	}
}

func TestHelperServesOnThenDrains(t *testing.T) {
	ready, stop, done := launch(t, map[string]string{"ADDR": "127.0.0.1:0", "SHUTDOWN": "helper",
		"SERVE_AFTER_TERM": "300ms", "DRAIN_DEADLINE": "2s"})
	addr := listening(t, ready, done)
	stop()
	time.Sleep(100 * time.Millisecond)
	if code, _, err := get(addr, "/work?ms=0"); err != nil || code != 200 {
		t.Fatalf("a new request 100 ms after the stop: %d, %v; want 200", code, err)
	}
	inFlight := make(chan int, 1)
	go func() {
		code, _, _ := get(addr, "/work?ms=200") // still running when the 300 ms are over
		inFlight <- code
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not return within 3 s of the stop")
	}
	if code := <-inFlight; code != 200 {
		t.Errorf("the request in flight ended with %d, want 200", code)
	}
	if !refused(addr) {
		t.Error("a dial after serve returned was not refused")
	}
}

func TestDrainNow(t *testing.T) {
	ready, stop, done := launch(t, map[string]string{"ADDR": "127.0.0.1:0", "SHUTDOWN": "drain-now",
		"DRAIN_DEADLINE": "2s"})
	addr := listening(t, ready, done)
	inFlight := make(chan int, 1)
	go func() {
		code, _, _ := get(addr, "/work?ms=200")
		inFlight <- code
	}()
	time.Sleep(50 * time.Millisecond)
	stop()
	time.Sleep(50 * time.Millisecond)
	if !refused(addr) {
		t.Error("a new connection right after the stop was not refused")
	}
	if code := <-inFlight; code != 200 {
		t.Errorf("the request in flight ended with %d, want 200", code)
	}
	if err := <-done; err != nil {
		t.Errorf("serve: %v", err)
	}
}

func TestExitModeRegistersNoHandler(t *testing.T) {
	if handlesTerm("exit") {
		t.Error("handlesTerm(exit) is true")
	}
	if !handlesTerm("drain-now") || !handlesTerm("helper") {
		t.Error("handlesTerm is false for drain-now or helper")
	}
}

func TestBadSettings(t *testing.T) {
	for _, env := range []map[string]string{
		{"ADDR": "127.0.0.1:0", "SHUTDOWN": "linger"},
		{"ADDR": "127.0.0.1:0", "SHUTDOWN": "helper", "START_DELAY": "soon"},
		{"ADDR": "127.0.0.1:0", "SHUTDOWN": "helper", "DRAIN_DEADLINE": "10"},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := serve(ctx, func(k string) string { return env[k] }, nil)
		cancel()
		if err == nil {
			t.Errorf("%v: serve returned nil, want an error", env)
		}
	}
}

// TestNoReadyChannel starts serve as main does, with nothing reading the
// address it listens on, and stops it.
func TestNoReadyChannel(t *testing.T) {
	env := map[string]string{"ADDR": "127.0.0.1:0", "SHUTDOWN": "drain-now"}
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	done := make(chan error, 1)
	go func() { done <- serve(ctx, func(k string) string { return env[k] }, nil) }()
	stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return within 5 s when no channel reads its address")
	}
}
