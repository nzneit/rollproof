// Command actioncheck checks one run of the action in the action's own test
// (.github/workflows/rollproof-action.yml in the host repository): the step's two outputs, the
// results folder's result.json, and the step's job summary, read in the runner's masked copy. It
// prints one line for each fault and exits 1, or exits 0 when every check holds; a flag it cannot
// read exits 2.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// options is one run to check: the step's outputs verdict and out, and what the workflow wants of
// them; wantDecided, when set, the code of the reason that must have decided; and stepFiles, the
// runner's folder of step files, which holds the masked copies of the job summaries.
type options struct {
	verdict, out, wantVerdict, wantOut, wantDecided, stepFiles string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the command: it reads args, the command line without the program's
// name, into options, and returns the exit code. A flag it cannot read gives 2,
// with the flag package's message on stderr. An argument left after the flags
// gives 2 as well, with one line on stderr that names the first of them, since
// a flag written after it would not be read. Otherwise it runs check and gives
// 0 when check gives no fault, and 1 when it gives any, each fault a line on
// stderr. It writes nothing to stdout. main is
// os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)).
func run(args []string, stdout, stderr io.Writer) int {
	var o options
	f := flag.NewFlagSet("actioncheck", flag.ContinueOnError)
	f.SetOutput(stderr)
	f.StringVar(&o.verdict, "verdict", "", "the step's output verdict")
	f.StringVar(&o.out, "out", "", "the step's output out")
	f.StringVar(&o.wantVerdict, "want-verdict", "", "the verdict wanted: pass, fail or not-verified")
	f.StringVar(&o.wantOut, "want-out", "", "the results folder wanted")
	f.StringVar(&o.wantDecided, "want-decided", "", "the code of the reason that must have decided, if any")
	f.StringVar(&o.stepFiles, "step-files", "", "the runner's folder of step files")
	if err := f.Parse(args); err != nil {
		return 2
	}
	if f.NArg() > 0 {
		fmt.Fprintf(stderr, "actioncheck: %q is no flag; every input is a flag\n", f.Arg(0))
		return 2
	}
	faults := check(o)
	for _, fault := range faults {
		fmt.Fprintln(stderr, fault)
	}
	if len(faults) > 0 {
		return 1
	}
	return 0
}

// check returns the faults of the run that o describes, one sentence each, and none
// when every check holds: verdict and out as wanted; wantOut/result.json final, with
// the wanted verdict and, when wantDecided is set, the reason that decided; and
// exactly one masked summary under stepFiles (step_summary_*-scrubbed) that holds
// the line "results: " + wantOut, whose first line begins "### rollproof: " and
// the verdict's word. GitHub does not promise the folder or the copy, so a folder
// that is not there is a fault that names it, never a pass.
func check(o options) []string {
	var faults []string
	if o.verdict != o.wantVerdict {
		faults = append(faults, fmt.Sprintf("verdict is %q; want %q", o.verdict, o.wantVerdict))
	}
	if o.out != o.wantOut {
		faults = append(faults, fmt.Sprintf("out is %q; want %q", o.out, o.wantOut))
	}
	faults = append(faults, resultFaults(o)...)
	return append(faults, summaryFaults(o)...)
}

// resultFaults checks wantOut/result.json: that it reads, is final, holds the wanted verdict and,
// when wantDecided is set, that the reason that decided is that one.
func resultFaults(o options) []string {
	var r struct {
		Final   bool     `json:"final"`
		Verdict string   `json:"verdict"`
		Reasons []reason `json:"reasons"`
	}
	b, err := os.ReadFile(filepath.Join(o.wantOut, "result.json"))
	if err == nil {
		err = json.Unmarshal(b, &r)
	}
	if err != nil {
		return []string{"result.json: " + err.Error()}
	}
	var faults []string
	if !r.Final {
		faults = append(faults, "result.json is not final")
	}
	if r.Verdict != o.wantVerdict {
		faults = append(faults, fmt.Sprintf("result.json's verdict is %q; want %q", r.Verdict, o.wantVerdict))
	}
	if o.wantDecided != "" {
		i := slices.IndexFunc(r.Reasons, func(r reason) bool { return r.Decided })
		switch {
		case i < 0:
			faults = append(faults, fmt.Sprintf("no reason in result.json decided; want %q", o.wantDecided))
		case r.Reasons[i].Code != o.wantDecided:
			faults = append(faults, fmt.Sprintf("the reason that decided is %q; want %q", r.Reasons[i].Code, o.wantDecided))
		}
	}
	return faults
}

// reason is what check reads of one reason of result.json: its code, and whether it is the one
// that decided.
type reason struct {
	Code    string `json:"code"`
	Decided bool   `json:"decided"`
}

// summaryFaults finds the one masked job summary under stepFiles that holds the line
// "results: " + wantOut, and checks its first line against the heading of wantVerdict.
func summaryFaults(o options) []string {
	entries, err := os.ReadDir(o.stepFiles)
	if errors.Is(err, fs.ErrNotExist) {
		return []string{"the runner's folder of step files, " + o.stepFiles +
			", is not there: GitHub does not promise it, so the job summary was not read"}
	}
	if err != nil {
		return []string{"the runner's folder of step files, " + o.stepFiles + ", cannot be read: " + err.Error()}
	}
	results := "results: " + o.wantOut
	var holding []string // the first lines of the summaries that hold the results line
	var faults []string
	for _, e := range entries {
		if ok, _ := filepath.Match("step_summary_*-scrubbed", e.Name()); !ok {
			continue
		}
		b, err := os.ReadFile(filepath.Join(o.stepFiles, e.Name()))
		if err != nil {
			faults = append(faults, "the masked job summary "+e.Name()+" cannot be read: "+err.Error())
			continue
		}
		lines := strings.Split(string(b), "\n")
		if slices.Contains(lines, results) {
			holding = append(holding, lines[0])
		}
	}
	switch len(holding) {
	case 0:
		return append(faults, fmt.Sprintf("no masked job summary under %s holds the line %q", o.stepFiles, results))
	case 1:
	default:
		return append(faults, fmt.Sprintf("%d masked job summaries under %s hold the line %q; want one", len(holding), o.stepFiles, results))
	}
	want := heading(o.wantVerdict)
	if first := holding[0]; first != want && !strings.HasPrefix(first, want+" ") {
		faults = append(faults, fmt.Sprintf("the job summary begins %q; want %q", first, want))
	}
	return faults
}

// heading is the first line, or its beginning, of a job summary whose verdict is the word v:
// "### rollproof: " and the word in capitals, with a space for its hyphen.
func heading(v string) string {
	return "### rollproof: " + strings.ToUpper(strings.ReplaceAll(v, "-", " "))
}
