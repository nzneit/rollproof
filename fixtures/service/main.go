// Command svc is the service of the experiments that the project grew from
// (section 3), changed so that a test can run it: a small HTTP server whose
// start-up delay and SIGTERM behavior are set by environment variables.
//
//	GET /work?ms=N  sleeps N ms, answers 200 "<pod> <version>"
//	GET /readyz     answers 200 (the server only listens once it can serve)
//	GET /version    answers 200 "<version>", whatever FAIL_WORK or FAIL_AFTER say
//
// START_DELAY       time after process start before it listens (warm-up)
// SHUTDOWN          exit | drain-now | helper
// SERVE_AFTER_TERM  helper: keep serving this long after SIGTERM
// DRAIN_DEADLINE    drain-now, helper: deadline given to http.Server.Shutdown
// FAIL_WORK         true: GET /work answers 500 with "failing" and does nothing else
// FAIL_AFTER        time after it listens before GET /work begins to fail; empty never fails
// FAIL_FOR          how long GET /work fails from FAIL_AFTER on; a set FAIL_AFTER needs it
// JOB_SLEEP         set, 0s included: sleep that long and exit 0 without listening, as a Job does
//
// A negative FAIL_AFTER, FAIL_FOR or JOB_SLEEP is an error, and so is a set FAIL_AFTER with no
// FAIL_FOR: the service fails loud rather than never failing or never ending.
// version is built in: -ldflags "-X main.version=…" (default dev), so that two builds are two images.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

var version = "dev"

func durEnv(env func(string) string, name string, def time.Duration) (time.Duration, error) {
	v := env(name)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("bad %s=%q: %v", name, v, err)
	}
	return d, nil
}

// nonNegative is the error for a duration below zero: as bad as one that does not parse.
func nonNegative(name string, d time.Duration) error {
	if d < 0 {
		return fmt.Errorf("bad %s: negative duration", name)
	}
	return nil
}

func handlesTerm(mode string) bool {
	return mode == "drain-now" || mode == "helper"
}

func serve(ctx context.Context, env func(string) string, ready chan<- string) error {
	pod, _ := os.Hostname()
	// JOB_SLEEP is read before anything listens: a Job sleeps and ends 0. Its
	// presence makes a Job, so 0s sleeps no time and still never listens.
	jobSleep, err := durEnv(env, "JOB_SLEEP", 0)
	if err != nil {
		return err
	}
	if err := nonNegative("JOB_SLEEP", jobSleep); err != nil {
		return err
	}
	if env("JOB_SLEEP") != "" {
		log.Printf("job_sleep=%s", jobSleep)
		time.Sleep(jobSleep)
		return nil
	}
	mode := env("SHUTDOWN")
	if mode == "" {
		mode = "exit"
	}
	if !handlesTerm(mode) && mode != "exit" {
		return fmt.Errorf("bad SHUTDOWN=%q", mode)
	}
	startDelay, err := durEnv(env, "START_DELAY", 0)
	if err != nil {
		return err
	}
	serveAfter, err := durEnv(env, "SERVE_AFTER_TERM", 5*time.Second)
	if err != nil {
		return err
	}
	drain, err := durEnv(env, "DRAIN_DEADLINE", 10*time.Second)
	if err != nil {
		return err
	}
	failAfter, err := durEnv(env, "FAIL_AFTER", 0)
	if err != nil {
		return err
	}
	if err := nonNegative("FAIL_AFTER", failAfter); err != nil {
		return err
	}
	failFor, err := durEnv(env, "FAIL_FOR", 0)
	if err != nil {
		return err
	}
	if err := nonNegative("FAIL_FOR", failFor); err != nil {
		return err
	}
	if env("FAIL_AFTER") != "" && env("FAIL_FOR") == "" {
		// No row asks for a window of no length: a set FAIL_AFTER needs FAIL_FOR.
		return fmt.Errorf("bad FAIL_FOR: empty while FAIL_AFTER is set")
	}
	addr := env("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	failWork := env("FAIL_WORK") == "true"

	log.Printf("pod=%s version=%s shutdown=%s start_delay=%s serve_after_term=%s drain_deadline=%s fail_work=%t fail_after=%s fail_for=%s",
		pod, version, mode, startDelay, serveAfter, drain, failWork, failAfter, failFor)

	// failFrom and failUntil are written once, after listening and before the
	// server's goroutine starts, so the handlers read them without a race.
	var failFrom, failUntil time.Time
	failing := func() bool {
		now := time.Now()
		return failWork || (!failFrom.IsZero() && !now.Before(failFrom) && now.Before(failUntil))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /work", func(w http.ResponseWriter, r *http.Request) {
		if failing() {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "failing")
			return
		}
		ms, _ := strconv.Atoi(r.URL.Query().Get("ms"))
		if ms > 0 {
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}
		w.Header().Set("X-Pod", pod)
		fmt.Fprintf(w, "%s %s\n", pod, version)
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, version)
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	// Warm-up: nothing listens until START_DELAY has passed.
	time.Sleep(startDelay)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	if ready != nil {
		ready <- ln.Addr().String()
	}
	if env("FAIL_AFTER") != "" {
		failFrom = time.Now().Add(failAfter)
		failUntil = failFrom.Add(failFor)
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	log.Printf("listening on %s", addr)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		log.Printf("stopping")
	}
	if mode == "helper" {
		log.Printf("serving for %s after SIGTERM", serveAfter)
		time.Sleep(serveAfter)
	}
	log.Printf("shutdown with deadline %s", drain)
	sctx, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Printf("drained, exiting")
	return nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.LUTC)
	ctx := context.Background()
	mode := os.Getenv("SHUTDOWN")
	if mode == "" {
		mode = "exit"
	}
	// Only modes that handle SIGTERM register for it. In exit mode no handler
	// is registered, so the Go runtime ends the process on SIGTERM.
	if handlesTerm(mode) {
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
		defer stop()
	}
	if err := serve(ctx, os.Getenv, nil); err != nil {
		log.Fatal(err)
	}
}
