// Command probe is the fixtures' custom probe: a program that checks the
// fixture service's GET /version, one check after another, and writes a line
// of the probe contract for each. Its environment sets the mode.
//
// TARGET       the Service's base URL, default http://orders.orders.svc
// VERSION      the version a check looks for in the body, white space trimmed
// ON_VERSION   silent: the check that first reads VERSION writes no line, and nothing more is written until SIGTERM
// ON_VERSION   exit: the check that first reads VERSION writes no line, and the probe exits 0
// FAIL_FIRST   a duration of failed-check lines, one every 2 ms, before the first check
// IGNORE_TERM  true: ignore SIGTERM
//
// A check passes on a status of 200 to 299. Its line is {"ok":true,"ms":N} or
// {"ok":false,"class":C,"ms":N}, C being the status code, timeout or other. On
// SIGTERM the probe starts no new check, writes the line of the one in flight
// and exits 0. A bad setting is printed to standard error, and the probe exits 1.
package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// pause is the time between the end of one check and the start of the next.
const pause = 20 * time.Millisecond

// ignoresTerm reports whether IGNORE_TERM is true, so that main ignores SIGTERM and
// passes run a nil term.
func ignoresTerm(env func(string) string) bool {
	return env("IGNORE_TERM") == "true"
}

// run is the fixture probe: one check of GET TARGET/version after another, a line
// of the probe contract on out for each, each in one write. It returns nil
// when term is closed (a nil term never is) or when ON_VERSION=exit has read
// VERSION; an unknown ON_VERSION or a bad duration is an error at once.
func run(env func(string) string, out io.Writer, term <-chan struct{}) error {
	onVersion := env("ON_VERSION")
	if onVersion != "" && onVersion != "silent" && onVersion != "exit" {
		return fmt.Errorf("bad ON_VERSION=%q: want silent or exit", onVersion)
	}
	var failFirst time.Duration
	if v := env("FAIL_FIRST"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("bad FAIL_FIRST=%q: %v", v, err)
		}
		if d < 0 {
			return fmt.Errorf("bad FAIL_FIRST=%q: a negative duration", v)
		}
		failFirst = d
	}
	target := env("TARGET")
	if target == "" {
		target = "http://orders.orders.svc"
	}
	version := env("VERSION")

	if failFirst > 0 {
		began := time.Now()
		tick := time.NewTicker(2 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-term:
				return nil
			case <-tick.C:
			}
			if time.Since(began) >= failFirst {
				break
			}
			io.WriteString(out, `{"ok":false,"class":"fixture"}`+"\n")
		}
	}

	// A new connection for every check, as a probe that sees a rollout's new pods needs.
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: time.Second}
	for {
		select {
		case <-term:
			return nil
		default:
		}
		began := time.Now()
		ok, class, body := check(client, target+"/version")
		ms := time.Since(began).Milliseconds()
		if ok && onVersion != "" && strings.TrimSpace(body) == version {
			if onVersion == "exit" {
				return nil
			}
			<-term
			return nil
		}
		if ok {
			fmt.Fprintf(out, `{"ok":true,"ms":%d}`+"\n", ms)
		} else {
			fmt.Fprintf(out, `{"ok":false,"class":%s,"ms":%d}`+"\n", strconv.Quote(class), ms)
		}
		select {
		case <-term:
			return nil
		case <-time.After(pause):
		}
	}
}

// check makes one GET of url. It passes on a status of 200 to 299; a failed
// check's class is the status code, "timeout" for a request that timed out, or
// "other" for any other error. body is what the answer carried.
func check(client *http.Client, url string) (ok bool, class, body string) {
	resp, err := client.Get(url)
	if err != nil {
		return false, errorClass(err), ""
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return false, errorClass(err), ""
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return false, strconv.Itoa(resp.StatusCode), string(b)
	}
	return true, "", string(b)
}

// errorClass is "timeout" for an error that is a timeout, and "other" otherwise.
func errorClass(err error) string {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	return "other"
}

func main() {
	var term chan struct{}
	if ignoresTerm(os.Getenv) {
		signal.Ignore(syscall.SIGTERM)
	} else {
		term = make(chan struct{})
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGTERM)
		go func() {
			<-c
			close(term)
		}()
	}
	if err := run(os.Getenv, os.Stdout, term); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
