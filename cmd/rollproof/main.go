// Package main is the rollproof command: it runs one test, runs the built-in
// probe as its pod does, and prints what the tool links.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/nzneit/rollproof/internal/cluster"
	"github.com/nzneit/rollproof/internal/probe"
	"github.com/nzneit/rollproof/internal/run"
)

// version is the tool's version, set at build time with -ldflags "-X main.version=…".
var version = "dev"

// probeContext gives the probe's context and the function that ends it. A test
// replaces it, so that the probe can be ended without a signal.
var probeContext = func() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
}

const usageText = `usage: rollproof run [-f FILE] [--out DIR] [--name NAME] [--keep-cluster]
       rollproof version
       rollproof probe http --url URL --rate N --timeout D --expect-status RANGE [--header NAME=VALUE]...
`

// runOf is the run that `rollproof run` starts. A test replaces it.
var runOf = run.Run

// keptSignal holds the first SIGINT or SIGTERM that runContext caught, as a
// syscall.Signal, for endProcess.
var keptSignal atomic.Value

// main runs the command line and ends the process: by the signal that stopped
// a run, if one did, and otherwise with the command line's exit code.
func main() {
	endProcess(realMain(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

// runContext gives the run's context. It ends at the first SIGINT or SIGTERM,
// which it keeps for endProcess; from then on a second one ends the process at
// once, by the signal's default action. release stops watching for signals. A
// signal that came before release is kept too, so the process ends by it after
// the run has returned.
func runContext() (ctx context.Context, release func()) {
	ctx, cancel := context.WithCancel(context.Background())
	// Room for two signals, so that a second one that comes before the first
	// was handled waits there too.
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	released := make(chan struct{})
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		var first os.Signal
		select {
		case first = <-ch:
		case <-released:
			// release has stopped the channel, so a signal that came before
			// is on it: it is kept, never lost.
			select {
			case first = <-ch:
			default:
				return
			}
		}
		keptSignal.Store(first.(syscall.Signal))
		// Stop returns once no signal is on its way to ch. From then on SIGINT
		// and SIGTERM take their default action (a SIGINT that was ignored as
		// the process started is ignored again: see raise), and one that came
		// meanwhile is on ch. Reset would give no such promise.
		signal.Stop(ch)
		select {
		case second := <-ch:
			raise(second.(syscall.Signal))
		default:
		}
		cancel()
	}()
	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			signal.Stop(ch)
			close(released)
			<-handled
			cancel()
		})
	}
}

// endProcess ends the process: by the signal that runContext kept, if one
// came, raised again with its default action; otherwise with code. Never
// returns.
func endProcess(code int) {
	if sig, ok := keptSignal.Load().(syscall.Signal); ok {
		raise(sig)
	}
	os.Exit(code)
}

// raise ends the process by sig: it gives sig its default action again and
// sends it to the process. When the process outlives it for 2 s, as it does
// when sig is SIGINT and SIGINT was ignored as the process started (a
// background job of a script), it exits with 128 and the signal's number
// instead, the status a shell gives a program that a signal ended. Never
// returns.
func raise(sig syscall.Signal) {
	signal.Reset(sig)
	_ = syscall.Kill(os.Getpid(), sig)
	time.Sleep(2 * time.Second)
	os.Exit(128 + int(sig))
}

// realMain runs the command line args: the subcommand in args[0], writing to
// stdout and stderr and reading the environment through getenv. It returns the
// exit code. For run, it prints the lines of withWorkflowCommandsStopped around
// the command.
func realMain(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return 2
	}
	switch args[0] {
	case "run":
		// The lines for GitHub's workflow commands go around the whole command, its usage errors
		// included (section 8, "Workflow commands").
		return withWorkflowCommandsStopped(stdout, stderr, getenv, signalCame, func() int {
			return runCommand(args[1:], stdout, stderr, getenv)
		})
	case "probe":
		return probeCommand(args, stdout, stderr)
	case "version":
		return versionCommand(args[1:], stdout, stderr)
	}
	fmt.Fprint(stderr, usageText)
	return 2
}

// runCommand runs one test of the configuration that -f names, in the context
// that runContext gives: a signal stops the run, and endProcess then ends the
// process by it.
func runCommand(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usageText) }
	file := fs.String("f", "rollproof.yaml", "the configuration file")
	out := fs.String("out", "", "the folder for the results")
	name := fs.String("name", "", "the cluster's name after the rollproof- prefix")
	keepCluster := fs.Bool("keep-cluster", false, "keep the cluster for debugging")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return 2
	}
	ctx, release := runContext()
	defer release()
	return runOf(ctx, run.Options{ConfigPath: *file, Out: *out, Name: *name, KeepCluster: *keepCluster,
		Stdout: stdout, Stderr: stderr, Version: version, Getenv: getenv}, realUnits(*keepCluster))
}

// probeCommand runs the built-in probe as its pod runs it.
func probeCommand(args []string, stdout, stderr io.Writer) int {
	o, err := probe.ParseHTTPArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, "rollproof: "+err.Error())
		fmt.Fprint(stderr, usageText)
		return 2
	}
	ctx, stop := probeContext()
	defer stop()
	if err := probe.RunHTTP(ctx, o, stdout); err != nil {
		fmt.Fprintln(stderr, "rollproof: "+err.Error())
		return 1
	}
	return 0
}

// versionCommand prints the tool's version and the references of what it uses by
// default, one a line.
func versionCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprint(stderr, usageText)
		return 2
	}
	fmt.Fprintln(stdout, "rollproof "+version)
	fmt.Fprintln(stdout, "kind "+cluster.KindVersion())
	fmt.Fprintln(stdout, "node image "+cluster.DefaultNodeImage())
	fmt.Fprintln(stdout, "registry image "+cluster.DefaultRegistryImage)
	return 0
}
