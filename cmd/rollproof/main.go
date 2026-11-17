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
	"syscall"

	"rollproof/internal/cluster"
	"rollproof/internal/probe"
	"rollproof/internal/run"
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

// main runs the command line and exits with its code.
func main() {
	os.Exit(realMain(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

// realMain runs the command line args: the subcommand in args[0], writing to
// stdout and stderr and reading the environment through getenv. It returns the
// exit code.
func realMain(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return 2
	}
	switch args[0] {
	case "run":
		return runCommand(args[1:], stdout, stderr, getenv)
	case "probe":
		return probeCommand(args, stdout, stderr)
	case "version":
		return versionCommand(args[1:], stdout, stderr)
	}
	fmt.Fprint(stderr, usageText)
	return 2
}

// runCommand runs one test of the configuration that -f names.
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return run.Run(ctx, run.Options{ConfigPath: *file, Out: *out, Name: *name, KeepCluster: *keepCluster,
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
