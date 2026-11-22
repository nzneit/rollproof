package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// wholeRun writes the results folder OUT and the folder of step files STEPS of a whole run, a
// pass, and returns the options that describe it.
func wholeRun(t *testing.T) options {
	t.Helper()
	root := t.TempDir()
	out := filepath.Join(root, "out")
	steps := filepath.Join(root, "steps")
	for _, d := range []string{out, steps} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(out, "result.json"), `{"final": true, "verdict": "pass", "reasons": []}`)
	summary := "### rollproof: PASS `orders (Helm) on Kubernetes v1.37.0 in kind`\n\n```\nresults: " + out + "\n```\n"
	write(t, filepath.Join(steps, "step_summary_1-scrubbed"), summary)
	write(t, filepath.Join(steps, "step_summary_1"), summary)
	write(t, filepath.Join(steps, "step_summary_2-scrubbed"), "### other\n")
	return options{verdict: "pass", out: out, wantVerdict: "pass", wantOut: out, stepFiles: steps}
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckAcceptsAWholeRun(t *testing.T) {
	// A results folder OUT holding result.json {"final": true, "verdict": "pass", "reasons": []};
	// a folder STEPS holding step_summary_1-scrubbed with
	//   "### rollproof: PASS `orders (Helm) on Kubernetes v1.37.0 in kind`\n\n```\nresults: OUT\n```\n",
	// step_summary_1 (the raw file, the same text), and step_summary_2-scrubbed with
	// "### other\n". check(options{verdict: "pass", out: OUT, wantVerdict: "pass", wantOut: OUT,
	// stepFiles: STEPS}) gives none.
	o := wholeRun(t)
	if faults := check(o); len(faults) != 0 {
		t.Errorf("check gave %q, want none", faults)
	}
}

func TestCheckReportsEachFault(t *testing.T) {
	// From the whole run above, one change a row, and exactly the one fault it gives:
	//   verdict "fail"                      `verdict is "fail"; want "pass"`
	//   out "/elsewhere"                    `out is "/elsewhere"; want "OUT"`
	//   result.json missing                 a fault that begins "result.json: "
	//   result.json final false             "result.json is not final"
	//   result.json verdict "fail"          `result.json's verdict is "fail"; want "pass"`
	//   wantDecided "interrupted", the file's decided reason internal-error
	//                                       `the reason that decided is "internal-error"; want "interrupted"`
	//   the summary's first line "### rollproof: FAIL"
	//                                       "the job summary begins \"### rollproof: FAIL\"; want \"### rollproof: PASS\""
	//   no scrubbed file holds the results line
	//                                       `no masked job summary under STEPS holds the line "results: OUT"`
	//   two scrubbed files hold it          `2 masked job summaries under STEPS hold the line "results: OUT"; want one`
	// (OUT and STEPS stand for the two folders' paths.)
	rows := []struct {
		name   string
		change func(t *testing.T, o *options)
		want   func(o options) string // the fault, or its beginning when prefix is set
		prefix bool
	}{
		{"verdict", func(t *testing.T, o *options) { o.verdict = "fail" },
			func(options) string { return `verdict is "fail"; want "pass"` }, false},
		{"out", func(t *testing.T, o *options) { o.out = "/elsewhere" },
			func(o options) string { return `out is "/elsewhere"; want "` + o.wantOut + `"` }, false},
		{"result.json missing", func(t *testing.T, o *options) {
			if err := os.Remove(filepath.Join(o.wantOut, "result.json")); err != nil {
				t.Fatal(err)
			}
		}, func(options) string { return "result.json: " }, true},
		{"result.json not final", func(t *testing.T, o *options) {
			write(t, filepath.Join(o.wantOut, "result.json"), `{"final": false, "verdict": "pass", "reasons": []}`)
		}, func(options) string { return "result.json is not final" }, false},
		{"result.json's verdict", func(t *testing.T, o *options) {
			write(t, filepath.Join(o.wantOut, "result.json"), `{"final": true, "verdict": "fail", "reasons": []}`)
		}, func(options) string { return `result.json's verdict is "fail"; want "pass"` }, false},
		{"the reason that decided", func(t *testing.T, o *options) {
			o.wantDecided = "interrupted"
			write(t, filepath.Join(o.wantOut, "result.json"), `{"final": true, "verdict": "pass", "reasons": [`+
				`{"code": "probe-failures", "decided": false}, {"code": "internal-error", "decided": true}]}`)
		}, func(options) string { return `the reason that decided is "internal-error"; want "interrupted"` }, false},
		{"the summary's first line", func(t *testing.T, o *options) {
			write(t, filepath.Join(o.stepFiles, "step_summary_1-scrubbed"), "### rollproof: FAIL\n\n```\nresults: "+o.wantOut+"\n```\n")
		}, func(options) string {
			return "the job summary begins \"### rollproof: FAIL\"; want \"### rollproof: PASS\""
		}, false},
		{"no summary holds the results line", func(t *testing.T, o *options) {
			write(t, filepath.Join(o.stepFiles, "step_summary_1-scrubbed"), "### rollproof: PASS\n\n```\nresults: ***\n```\n")
		}, func(o options) string {
			return `no masked job summary under ` + o.stepFiles + ` holds the line "results: ` + o.wantOut + `"`
		}, false},
		{"two summaries hold the results line", func(t *testing.T, o *options) {
			write(t, filepath.Join(o.stepFiles, "step_summary_3-scrubbed"), "### rollproof: PASS\n\n```\nresults: "+o.wantOut+"\n```\n")
		}, func(o options) string {
			return `2 masked job summaries under ` + o.stepFiles + ` hold the line "results: ` + o.wantOut + `"; want one`
		}, false},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			o := wholeRun(t)
			row.change(t, &o)
			faults := check(o)
			want := row.want(o)
			if len(faults) != 1 {
				t.Fatalf("check gave %q, want exactly one fault %q", faults, want)
			}
			if row.prefix && !strings.HasPrefix(faults[0], want) || !row.prefix && faults[0] != want {
				t.Errorf("check gave %q, want %q", faults[0], want)
			}
		})
	}
}

func TestCheckFailsLoudOnWhatItCannotTell(t *testing.T) {
	// From the whole run, one change a row, and exactly the one fault it gives:
	//   wantDecided "interrupted", result.json with no reason
	//                                       `no reason in result.json decided; want "interrupted"`
	//   stepFiles a regular file            "the runner's folder of step files, PATH, cannot be read: "
	//                                       and os.ReadDir's error for PATH
	//   step_summary_9-scrubbed a folder, step_summary_1-scrubbed still holding the results line
	//                                       "the masked job summary step_summary_9-scrubbed cannot be
	//                                       read: " and os.ReadFile's error for it
	rows := []struct {
		name   string
		change func(t *testing.T, o *options)
		want   func(t *testing.T, o options) string
	}{
		{"no reason decided", func(t *testing.T, o *options) {
			o.wantDecided = "interrupted"
		}, func(*testing.T, options) string { return `no reason in result.json decided; want "interrupted"` }},
		{"the step files a regular file", func(t *testing.T, o *options) {
			o.stepFiles = filepath.Join(t.TempDir(), "a-file")
			write(t, o.stepFiles, "not a folder\n")
		}, func(t *testing.T, o options) string {
			_, err := os.ReadDir(o.stepFiles)
			if err == nil {
				t.Fatal("os.ReadDir of a regular file gave no error")
			}
			return "the runner's folder of step files, " + o.stepFiles + ", cannot be read: " + err.Error()
		}},
		{"a masked copy that is a folder", func(t *testing.T, o *options) {
			if err := os.Mkdir(filepath.Join(o.stepFiles, "step_summary_9-scrubbed"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, func(t *testing.T, o options) string {
			_, err := os.ReadFile(filepath.Join(o.stepFiles, "step_summary_9-scrubbed"))
			if err == nil {
				t.Fatal("os.ReadFile of a folder gave no error")
			}
			return "the masked job summary step_summary_9-scrubbed cannot be read: " + err.Error()
		}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			o := wholeRun(t)
			row.change(t, &o)
			want := []string{row.want(t, o)}
			if diff := cmp.Diff(want, check(o)); diff != "" {
				t.Errorf("faults (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCheckFailsLoudWithoutTheStepFiles(t *testing.T) {
	// stepFiles a path that does not exist, everything else whole: exactly the fault
	// "the runner's folder of step files, PATH, is not there: GitHub does not promise it, so the job
	// summary was not read".
	o := wholeRun(t)
	o.stepFiles = filepath.Join(t.TempDir(), "_runner_file_commands")
	want := []string{"the runner's folder of step files, " + o.stepFiles +
		", is not there: GitHub does not promise it, so the job summary was not read"}
	if diff := cmp.Diff(want, check(o)); diff != "" {
		t.Errorf("faults (-want +got):\n%s", diff)
	}
}

func TestCheckWordsOfEachVerdict(t *testing.T) {
	// The heading wanted for wantVerdict "pass", "fail" and "not-verified" is "### rollproof: PASS",
	// "### rollproof: FAIL" and "### rollproof: NOT VERIFIED".
	for v, want := range map[string]string{
		"pass":         "### rollproof: PASS",
		"fail":         "### rollproof: FAIL",
		"not-verified": "### rollproof: NOT VERIFIED",
	} {
		if got := heading(v); got != want {
			t.Errorf("heading(%q) = %q, want %q", v, got, want)
		}
	}
}

func TestTheCommandsExitCodes(t *testing.T) {
	// run with the flags of the whole run of TestCheckAcceptsAWholeRun ("-verdict", "pass",
	// "-out", OUT, "-want-verdict", "pass", "-want-out", OUT, "-step-files", STEPS): 0, and stdout
	// and stderr empty.
	// The same with "-verdict", "fail": 1; stdout empty; stderr exactly one line, the fault,
	//   `verdict is "fail"; want "pass"` and a line end.
	// The same with a flag it does not know added, "-no-such-flag": 2; stdout empty.
	// The same with a word that is no flag added, "extra": 2; stdout empty; stderr exactly one
	//   line that names it, `actioncheck: "extra" is no flag; every input is a flag` and a line end.
	o := wholeRun(t)
	args := func(verdict string, more ...string) []string {
		return append([]string{"-verdict", verdict, "-out", o.out, "-want-verdict", "pass",
			"-want-out", o.wantOut, "-step-files", o.stepFiles}, more...)
	}
	rows := []struct {
		name   string
		args   []string
		code   int
		stderr *string // nil: not checked
	}{
		{"whole", args("pass"), 0, ptr("")},
		{"a fault", args("fail"), 1, ptr(`verdict is "fail"; want "pass"` + "\n")},
		{"a flag it does not know", args("pass", "-no-such-flag"), 2, nil},
		{"a word that is no flag", args("pass", "extra"), 2, ptr(`actioncheck: "extra" is no flag; every input is a flag` + "\n")},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(row.args, &stdout, &stderr); code != row.code {
				t.Errorf("exit code %d, want %d; stderr %q", code, row.code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout %q, want empty", stdout.String())
			}
			if row.stderr != nil && stderr.String() != *row.stderr {
				t.Errorf("stderr %q, want %q", stderr.String(), *row.stderr)
			}
		})
	}
}

func ptr(s string) *string { return &s }
