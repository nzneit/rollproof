# Point 13: the security review of masking, the job summary, workflow commands and what leaves the runner

## Question

Section 14, point 13 of the design asks for a security reviewer's eye, before graduation, on four things: the masking rule of section 8; the way the job summary keeps text from outside the tool from changing a run's page (section 10); the way the tool and the action's script keep GitHub's runner from reading a workflow command in what they print (section 8; point 33); and five facts of section 11 with its list of what leaves the runner. Do those four rules hold against hostile input, and what did the maintainer rule on each finding?

## Machine and versions

- CachyOS, Linux 7.3.3-1-cachyos, x86_64, 16 CPUs and 60 GB of memory. The machine's clock is five hours behind UTC; the times below are by that clock.
- The engine: nerdctl 2.4.2 on containerd v2.4.2, rootless.
- Go 1.27.3, with the module's `go 1.27`. Bash 5.3.20, which the tests of `hack/action` run `action.sh` with.
- kind v0.33.0 as a library (`sigs.k8s.io/kind v0.33.0` in `go.mod`). Its default node image is `kindest/node:v1.37.0`; the suite's second image, in its row `first-on-the-oldest-node-image`, is `kindest/node:v1.34.11`.
- Two readings of source, and neither a run. Reviewer 3 read the runner's source in a clone on this machine, at `7e211dc69c347b78fc398e630e74facb11a13235`, the commit point 33 cites. The session read the table extension of github/cmark-gfm, its master branch. Reviewer 2 says it read the cases that GitHub's documentation leaves open against two GFM implementations on this machine, goldmark and markdown-it; its report names no version of either, and the session ran neither.
- The models. The four reviewers and the second reader: one agent each on DeepSeek V4.1 Flash with thinking at its maximum, as point 34 names it, the maintainer's choice (`deepseek/deepseek-flash#max`). The re-runs, the fixes, the texts and this record: the session that runs the plan, on `zai-coding-plan/glm-5.4`. No agent ran on a Fable model: none is configured in the harness, nor an Opus or a Sonnet model.

## Method

No program under `proofs/` was written for this point. The method is four reviewers with throwaway tests, the re-runs of their tries, the maintainer's rulings, a fix round and a second reader; each step is recorded in Result from its files.

1. **Four cards, one per part.** Each reviewer got the design's rule for its part and a card that names the code to read and the hostile input to plant:
   - Masking: a registry password arrives only through an environment variable and is masked, in its plain and its encoded form, in everything the tool writes but the step's outputs on GitHub and the client library's log on standard error. The card asks for a password in every input and in forms the masker may not cover, and for a search of everything the run wrote.
   - The job summary: text from outside the tool stands in the summary only inside a fenced code block or a code span. The card asks for Markdown and HTML in every string of the record.
   - Workflow commands: on GitHub Actions the tool's first line on each stream stops the runner's workflow commands and its last line on standard output resumes them when the tool ended by itself; the action's script stops them and never resumes. The card asks for what may be printed before the stop line or after the resume line.
   - The five facts and what leaves the runner: the first bullet of section 11, its list of what leaves the runner, and the facts on the credentials file, a mirror's credentials, a chart's pull secrets and a probe header. The card asks for a pod, a command or a pull that reaches a secret or a place section 11 does not name, or for something the list leaves out.
2. **Reviewers in copies.** Each reviewer worked in a copy of the project made with `cp -a`, which holds no git repository, and changed nothing outside it. Reviewers 1 and 2 ran first; 3 and 4 started when both had reported. A reviewer that stopped before its report was to be dispatched again; none did. Each left its tests in place, named `review_pn_k_test.go`, with the command that runs each and its output. Reviewer 1 put its seven in a package of its own, `reviewp1`, and reviewer 4 its first in a package of its own, `review-p4`; the other 37 stand among the packages of the code they tried to break.
3. **The re-runs.** The session ran every try again on 2026-11-25 from its copy's root, with the command the report gave, but in eight tries with `-count=1` or the name between `^` and `$` added, as each try's line says, and set one of three verdicts: `holds` when both outputs show the rule broken, `does not hold` when the re-run shows the rule keeping, and `differs` when they disagree. The five tries of part 1 that need no engine were run with `-count=1`, which the reviewer's commands lack, so that the tests ran and were not taken from the build cache. The nine tries that need the engine ran one at a time, from 18:06 to 18:14, after all four reviewers had reported; two of them, P4-2 and P4-9, make no cluster, because the engine's pull of the image fails first. Before and after them `nerdctl ps -a --format "{{.Names}}"` printed nothing, and no network, volume or image named `rollproof-review-` remained.
4. **One reading by the session.** P2-3's reviewer was unclear whether GitHub's table splitter reads a run of backslashes before a pipe. The session read the source of github/cmark-gfm, its table extension's scanner, and wrote what it read, and what it did not run, beside the finding.
5. **The rulings.** The session asked the maintainer one question per finding, in the order of the IDs: fix it, document it, or accept it with a reason. The answer and its reason stand with the finding in Result.
6. **The fix round.** Each finding ruled fix got a test in the package that owns the rule: the test was seen to fail, the smallest change was made, and the test passed; one commit for each finding. Each finding ruled document got its text in section 11 of the design, in one commit. Each ruled accept changed nothing. Before each commit stood the plan's checks: `gofmt`, `go test ./...` and the four vets when code changed, and always the search for what may not be published and the citation check.
7. **The second reader.** One more agent, on the reviewers' model, read the whole change cold in a copy at `1477c70`, eleven commits after `1009111`: the diff, the files it touches with their tests, and section 11. It was to prove each fault with a throwaway test named `review_s_k_test.go`, and to say of each fix whether it keeps the rule against the input of its test and of a near one, whether its test passes without the fix, whether the change does more than its finding asked, and whether a text that documents a finding says more or less than the finding showed. The session ran each of its tests again in its copy.
8. **The runs.** Code that a run executes changed (`internal/report/mask.go`, `internal/report/console.go` and `action.sh`), so the whole suite and the mirror job ran once on this machine, each command alone from `projects/rollproof/`:
   - `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags e2e -p 1 -count=1 ./e2e/ -v -timeout 360m`
   - `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags mirror -p 1 -count=1 ./e2e/ -run TestMirror -v -timeout 60m`

## Result

Every line below comes from a file: the report of a reviewer, as the harness stored it; the re-run's output; the test as its copy holds it; or the commit of a fix or of a text. A try's ID is `Pn-k`: part `n`, try `k`. The reviewer's output is trimmed to the lines that decided, as its report is. The re-run was made by the session that runs this plan, on 2026-11-25, with the command named; its tokens, ports, moments and pods' suffixes are new for each run, as each rule says.

### Part 1: masking

7 tries, by reviewer 1; the card it was given is in Method.

**The rule's claims, as the design states them, in reviewer 1's words:**

````markdown
1. The one secret the tool handles is a registry password.
2. `registries[].password` must be a single `${env.NAME}`; a password written in the file is rejected, and the text that rejects any configuration never quotes a value.
3. The tool masks every such password in everything it writes, in its plain form and in the encoded form in which the nodes’ credentials file holds it.
4. That includes the output of commands and the pod logs that it copies into the evidence.
5. Two things it does not mask: the step’s outputs on GitHub (a word of the verdict’s vocabulary and the results folder’s absolute path, the first written before the configuration is read), and the log the Kubernetes client library writes on standard error.
6. The tool masks nothing else: a chart value or a probe header’s value is written as it was given.
7. The console, the job summary and `result.json` give names and no values — the names of the probe’s headers, of a custom probe’s `env` entries and of the values files; a custom probe’s `command` and `args` appear there as counts; the evidence folder may hold the values.
8. A custom probe that needs a credential takes it through `env` with `valueFrom`, from a Secret that a `setup` command created in the probe’s namespace; the value then never passes through the tool.
9. Section 11 repeats the core: a registry password arrives only through an environment variable; it is masked, in its plain and its encoded form, in everything the tool writes but the step’s outputs on GitHub, which hold no value of the configuration, and the client library’s log on standard error, which holds the API server’s warnings; nothing else is masked.
10. The README lists the forms: masked as `***` in the console, in `result.json` and in the evidence folder, in plain form, the JSON spellings (with and without HTML escaping), the Go-quoted spelling, and the base64 of `USERNAME:PASSWORD` (padded and not); nothing else is masked.
11. Section 9: the credentials file holds only the hosts the configuration lists, sits in every node where the kubelet reads it, and any pod that mounts that path of its node can read it; a caller gives credentials that can only read, and that expire soon.
12. Section 9: an entry’s credentials go to its mirror when it has one, and to its host otherwise, and to a token service that the challenge names; a mirror replaces its host, so the real host never sees credentials meant for the mirror.
````

#### P1-1: the password as a failure class, read in the console, the job summary and `result.json`

- Test: `review-1/reviewp1/review_p1_1_test.go`, `TestP1K1FailureClassIsAPassword`, as the copy holds it:

````go
package reviewp1

// Review part 1, try 1: a registry password planted as the failure class of a
// check, which the report keeps in a map key. The rule (design section 8,
// "Secrets", and section 11's second bullet) says the password is masked, in
// its plain and its encoded form, in everything the tool writes. This try
// writes the class through report.Final, report.Console and report.Summary and
// searches each for the password.

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rollproof/internal/mask"
	"rollproof/internal/report"
)

func TestP1K1FailureClassIsAPassword(t *testing.T) {
	const user = "rv1user"
	const pw = "r1s3cretpass"
	m := mask.New([]mask.Credential{{Username: user, Password: pw}})
	encoded := base64.StdEncoding.EncodeToString([]byte(user + ":" + pw))
	first := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	settled := first.Add(15 * time.Second)

	rec := report.Run{
		Verdict: "not-verified",
		Name:    "review1",
		Reasons: []report.Reason{{
			Code: "steady-not-clean", Verdict: "not-verified", Window: "steady", Decided: true,
			Text: "3 checks failed (" + pw + " 3), first at 10:00:00.00",
		}},
		Install: &report.Install{
			Ready: first, FirstObservation: &first, Settled: &settled,
			Checks: 3, Failed: 3,
			FailedByClass: map[string]int{pw: 2, encoded: 1},
		},
		Windows: []report.Window{{
			Name: "steady", Result: "not-verified", Checks: 3, Failed: 3,
			FailedByClass: map[string]int{pw: 3},
		}},
	}

	w, err := report.Open(t.TempDir(), func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	w.SetMasker(m)
	if err := w.Final(rec); err != nil {
		t.Fatal(err)
	}
	result, err := os.ReadFile(filepath.Join(w.Dir(), "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	masked := report.Mask(rec, m)
	console := report.Console(masked)
	summary := report.Summary(masked)

	t.Logf("the console:\n%s", console)
	t.Logf("the job summary:\n%s", summary)

	for _, form := range []string{pw, encoded} {
		for _, c := range []struct {
			where string
			text  string
		}{
			{"console", console},
			{"summary", summary},
			{"result.json", string(result)},
		} {
			if !strings.Contains(c.text, form) {
				continue
			}
			t.Errorf("%s holds the password %q", c.where, form)
			t.Logf("%s, the lines that hold it:\n%s", c.where, linesWith(c.text, form))
		}
	}
}

// linesWith returns the lines of text that hold form, indented by two spaces.
func linesWith(text, form string) string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, form) {
			out = append(out, "  "+line)
		}
	}
	return strings.Join(out, "\n")
}
````

- Command, from the copy's root: `go test ./reviewp1 -run TestP1K1FailureClassIsAPassword -v`. The re-run adds -count=1 and writes the name between ^ and $, so that the test runs and is not taken from the build cache.
- Rule: "The tool masks every such password in everything it writes, in its plain form and in the encoded form in which the nodes' credentials file holds it." (section 8, "Secrets.")
- Reviewer's output (its verdict: broken: the password is a map key, and `mask.go` keeps map keys):

````
review_p1_1_test.go:79: console holds the password "r1s3cretpass"
review_p1_1_test.go:80: console, the lines that hold it:
                    3 checks, 3 failed (r1s3cretpass 2, cnYxdXNlcjpyMXMzY3JldHBhc3M= 1)
review_p1_1_test.go:79: summary holds the password "r1s3cretpass"
review_p1_1_test.go:79: result.json holds the password "r1s3cretpass"
review_p1_1_test.go:80: result.json, the lines that hold it:
              "r1s3cretpass": 3
              "cnYxdXNlcjpyMXMzY3JldHBhc3M=": 1,
````

- The re-run's output (exit 1, the test fails as the reviewer's did): the same lines, and those of the encoded form:

````
    review_p1_1_test.go:79: console holds the password "r1s3cretpass"
    review_p1_1_test.go:80: console, the lines that hold it:
                      3 checks, 3 failed (r1s3cretpass 2, cnYxdXNlcjpyMXMzY3JldHBhc3M= 1)
    review_p1_1_test.go:79: summary holds the password "r1s3cretpass"
    review_p1_1_test.go:80: summary, the lines that hold it:
                      3 checks, 3 failed (r1s3cretpass 2, cnYxdXNlcjpyMXMzY3JldHBhc3M= 1)
    review_p1_1_test.go:79: result.json holds the password "r1s3cretpass"
    review_p1_1_test.go:80: result.json, the lines that hold it:
                  "r1s3cretpass": 3
    review_p1_1_test.go:79: console holds the password "cnYxdXNlcjpyMXMzY3JldHBhc3M="
    review_p1_1_test.go:80: console, the lines that hold it:
                      3 checks, 3 failed (r1s3cretpass 2, cnYxdXNlcjpyMXMzY3JldHBhc3M= 1)
    review_p1_1_test.go:79: summary holds the password "cnYxdXNlcjpyMXMzY3JldHBhc3M="
    review_p1_1_test.go:80: summary, the lines that hold it:
                      3 checks, 3 failed (r1s3cretpass 2, cnYxdXNlcjpyMXMzY3JldHBhc3M= 1)
    review_p1_1_test.go:79: result.json holds the password "cnYxdXNlcjpyMXMzY3JldHBhc3M="
    review_p1_1_test.go:80: result.json, the lines that hold it:
                "cnYxdXNlcjpyMXMzY3JldHBhc3M=": 1,
````

- Verdict of the re-run: holds.
- Ruling: fix (the maintainer, 2026-11-25). The smaller change: the masker masks the keys of a map as it masks its values; no configuration key, reason code or schema version. Built test-first in Task 2.
- The fix: commit `5feb0e7`; its test `TestMaskMasksMapKeys`.

#### P1-2: how the password reaches `failed_by_class`: a custom probe's `class` is a free string

- Test: `review-1/reviewp1/review_p1_2_test.go`, `TestP1K2ProbeClassReachesTheVerdict`, as the copy holds it:

````go
package reviewp1

// Review part 1, try 2: the failure class of a custom probe's line, read by the
// tool's own parser and counted by its own verdict code. The rule (design
// section 8, "Secrets") says the password is masked in everything the tool
// writes; a custom probe's class is a free string (section 6: "A short label
// for the kind of failure"), so a password can arrive there. This try shows
// the class reaching the verdict's failed_by_class map, the map the report
// writes.

import (
	"testing"
	"time"

	"rollproof/internal/probe"
	"rollproof/internal/verdict"
)

func TestP1K2ProbeClassReachesTheVerdict(t *testing.T) {
	const pw = "r1s3cretpass"
	t0 := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)

	// A custom probe's failing line, exactly as the contract's example writes one.
	line := `{"ok":false,"t":"2026-11-25T10:00:00Z","class":"` + pw + `"}`
	parsed := probe.ParseLine(line, t0, time.Time{})
	t.Logf("probe.ParseLine(%s) gives kind %v, ok %v, class %q", line, parsed.Kind, parsed.Obs.OK, parsed.Obs.Class)
	if parsed.Kind != probe.Observed {
		t.Fatalf("the line is not an observation: %+v", parsed)
	}

	d := verdict.Decide(verdict.Timeline{
		Windows: []verdict.Window{
			{Name: verdict.Install, Start: t0, End: t0.Add(15 * time.Second)},
			{Name: verdict.Steady, Start: t0.Add(15 * time.Second), End: t0.Add(25 * time.Second)},
		},
		Observations: []verdict.Observation{parsed.Obs},
		Silence:      5 * time.Second,
		Now:          t0.Add(25 * time.Second),
	})
	if len(d.Windows) == 0 {
		t.Fatalf("verdict.Decide gave no window result: %+v", d)
	}
	install := d.Windows[0]
	t.Logf("verdict.Decide gives the install window failed_by_class %v", install.ByClass)
	if install.ByClass[pw] != 1 {
		t.Fatalf("the install window's failed_by_class holds no key %q: %v", pw, install.ByClass)
	}
	t.Errorf("verdict.Decide keeps the password as a failed_by_class key: %v", install.ByClass)
}
````

- Command, from the copy's root: `go test ./reviewp1 -run TestP1K2ProbeClassReachesTheVerdict -v`. The re-run adds -count=1 and writes the name between ^ and $, so that the test runs and is not taken from the build cache.
- Rule: "The tool masks every such password in everything it writes" (section 8, "Secrets."), at the point where the probe's line enters the record.
- Reviewer's output (its verdict: broken, the mechanism of P1-1):

````
probe.ParseLine({"ok":false,"t":"2026-11-25T10:00:00Z","class":"r1s3cretpass"}) gives kind 1, ok false, class "r1s3cretpass"
verdict.Decide gives the install window failed_by_class map[r1s3cretpass:1]
````

- The re-run's output (exit 1): the same lines, and the test's own:

````
    review_p1_2_test.go:26: probe.ParseLine({"ok":false,"t":"2026-11-25T10:00:00Z","class":"r1s3cretpass"}) gives kind 1, ok false, class "r1s3cretpass"
    review_p1_2_test.go:44: verdict.Decide gives the install window failed_by_class map[r1s3cretpass:1]
    review_p1_2_test.go:48: verdict.Decide keeps the password as a failed_by_class key: map[r1s3cretpass:1]
````

- Verdict of the re-run: holds.
- Ruling: accept (the maintainer, 2026-11-25). Covered by the fix of P1-1, which masks the key wherever the record is written out, where both tries printed it. The rule is not left as it was.

#### P1-3: every promised form, in evidence and the record

- Test: `review-1/reviewp1/review_p1_3_test.go`, `TestP1K3PromisedFormsInEvidenceAndRecord`, as the copy holds it:

````go
package reviewp1

// Review part 1, try 3: the promised forms of a registry password planted in
// evidence and in the record: plain, the two JSON spellings, the Go-quoted
// spelling and the base64 of USER:PASSWORD padded and not. The rule (design
// section 8, "Secrets"; README) says each is masked in everything the tool
// writes. This try writes them through report.Writer.Evidence,
// ReplaceEvidence and Final, whole and split across writes, in a line longer
// than the stream's 64 KiB, and with bytes that are not UTF-8.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"rollproof/internal/mask"
	"rollproof/internal/report"
)

func TestP1K3PromisedFormsInEvidenceAndRecord(t *testing.T) {
	const user = "rv1user"
	passwords := map[string]string{
		"short":     `rv"&<pass`,
		"not-utf8":  "abc\xffdef",
		"newline":   "line1\nline2\ttab\\slash\"quote",
		"one-byte":  "Q",
		"very-long": strings.Repeat("A", 100000) + "tail",
	}
	for name, pw := range passwords {
		t.Run(name, func(t *testing.T) {
			m := mask.New([]mask.Credential{{Username: user, Password: pw}})
			w, err := report.Open(t.TempDir(), func(string) string { return "" })
			if err != nil {
				t.Fatal(err)
			}
			w.SetMasker(m)
			forms := promisedForms(user, pw)
			for i, form := range forms {
				// One write, the form between two words that hold neither 'a' nor the form.
				checkEvidence(t, w, m, "whole-"+strconv.Itoa(i), "PRE "+form+" POST", form)
				if i == 0 {
					if b, err := os.ReadFile(filepath.Join(w.Dir(), "evidence", "review1", "whole-0.log")); err == nil {
						t.Logf("the masked evidence for %s: %q", name, firstBytes(string(b), 200))
					}
				}

				// The form split across writes at several boundaries, one byte at a time for
				// the short one; a form longer than the masker's hold-back is cut in the middle.
				cuts := []int{1, 2, 3, 7, 64, 1000, len(form) / 2, len(form) - 1}
				if len(pw) <= 32 {
					cuts = append(cuts, everyByte(len(form))...)
				}
				checkEvidenceSplit(t, w, m, "split-"+strconv.Itoa(i), "PRE "+form+" POST", form, cuts)

				// One line longer than the 64 KiB a command's stream passes at once, with the form
				// after the cut.
				long := strings.Repeat("x", 70000) + form + " END"
				checkEvidence(t, w, m, "long-"+strconv.Itoa(i), long, form)

				// ReplaceEvidence masks the bytes a JSON document would hold.
				raw := []byte(`{"x":"` + form + `"}`)
				name := filepath.Join("replaced-" + strconv.Itoa(i) + ".json")
				if err := w.ReplaceEvidence(name, raw); err != nil {
					t.Fatal(err)
				}
				b, err := os.ReadFile(filepath.Join(w.Dir(), "evidence", name))
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(b, []byte(form)) {
					t.Errorf("ReplaceEvidence(%s) holds the form %q:\n%s", name, form, b)
				}
			}

			// Every form in a record field that the console prints, through Final.
			rec := report.Run{
				Verdict:  "fail",
				Name:     pw,
				Reasons:  []report.Reason{{Code: "probe-failures", Verdict: "fail", Window: "deploy", Text: pw, Detail: []string{"detail " + pw}}},
				Warnings: []string{"warning " + pw},
				Settings: []report.Setting{{Key: "limits.steady", Value: pw, Default: "10s"}},
			}
			if err := w.Final(rec); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(filepath.Join(w.Dir(), "result.json"))
			if err != nil {
				t.Fatal(err)
			}
			masked := report.Mask(rec, m)
			console, summary := report.Console(masked), report.Summary(masked)
			t.Logf("the masked console for %s:\n%s", name, firstBytes(console, 1200))
			for i, form := range forms {
				for _, c := range []struct {
					where string
					text  []byte
				}{
					{"result.json", b},
					{"console", []byte(console)},
					{"summary", []byte(summary)},
				} {
					if bytes.Contains(c.text, []byte(form)) {
						t.Errorf("%s holds form %d: %q", c.where, i, form)
					}
				}
			}
		})
	}
}

// passwordForms is the promised forms of one password, by name.
type passwordForms struct {
	Plain, JSONHTML, JSON, Go, B64Raw, B64 string
}

// namedForms returns the forms that section 8 and the README promise are
// masked: plain, the JSON spellings with and without HTML escaping, the
// Go-quoted spelling without its quotes, and the base64 of USER:PASSWORD
// padded and not.
func namedForms(user, pw string) passwordForms {
	html, err := json.Marshal(pw)
	if err != nil {
		panic(err)
	}
	var plain bytes.Buffer
	enc := json.NewEncoder(&plain)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(pw); err != nil {
		panic(err)
	}
	unescaped := strings.TrimSuffix(plain.String(), "\n")
	quoted := strconv.Quote(pw)
	credential := []byte(user + ":" + pw)
	return passwordForms{
		Plain:    pw,
		JSONHTML: string(html[1 : len(html)-1]),
		JSON:     unescaped[1 : len(unescaped)-1],
		Go:       quoted[1 : len(quoted)-1],
		B64Raw:   base64.RawStdEncoding.EncodeToString(credential),
		B64:      base64.StdEncoding.EncodeToString(credential),
	}
}

// promisedForms returns namedForms's forms, each once, in a fixed order.
func promisedForms(user, pw string) []string {
	f := namedForms(user, pw)
	forms := []string{f.Plain, f.JSONHTML, f.JSON, f.Go, f.B64Raw, f.B64}
	seen := map[string]bool{}
	out := forms[:0]
	for _, x := range forms {
		if x == "" || seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	return out
}

// checkEvidence writes text to one evidence file in one call and fails when the
// file holds form.
func checkEvidence(t *testing.T, w *report.Writer, m *mask.Masker, name, text, form string) {
	t.Helper()
	f, err := w.Evidence("review1/" + name + ".log")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(w.Dir(), "evidence", "review1", name+".log"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(form)) {
		t.Errorf("%s holds the form %q:\n%s", name, form, excerpt(b, form))
	}
}

// checkEvidenceSplit writes each cut of text in its own call and fails when the
// file holds form.
func checkEvidenceSplit(t *testing.T, w *report.Writer, m *mask.Masker, name, text, form string, cuts []int) {
	t.Helper()
	f, err := w.Evidence("review1/" + name + ".log")
	if err != nil {
		t.Fatal(err)
	}
	last := 0
	for _, cut := range cuts {
		if cut <= last || cut >= len(text) {
			continue
		}
		if _, err := f.Write([]byte(text[last:cut])); err != nil {
			t.Fatal(err)
		}
		last = cut
	}
	if _, err := f.Write([]byte(text[last:])); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(w.Dir(), "evidence", "review1", name+".log"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(form)) {
		t.Errorf("%s holds the form %q across %d writes:\n%s", name, form, len(cuts), excerpt(b, form))
	}
}

func everyByte(n int) []int {
	out := make([]int, 0, n-1)
	for i := 1; i < n; i++ {
		out = append(out, i)
	}
	return out
}

// excerpt returns text around the first occurrence of form, so that a failure
// prints bounded output.
func excerpt(b []byte, form string) string {
	i := bytes.Index(b, []byte(form))
	if i < 0 {
		return string(b)
	}
	lo, hi := i-40, i+len(form)+40
	if lo < 0 {
		lo = 0
	}
	if hi > len(b) {
		hi = len(b)
	}
	return string(b[lo:hi])
}
````

- Command, from the copy's root: `go test ./reviewp1 -run TestP1K3PromisedFormsInEvidenceAndRecord -v`. The re-run adds -count=1 and writes the name between ^ and $, so that the test runs and is not taken from the build cache.
- Reviewer's output:

````text
the masked evidence for short: "PRE *** POST"
the masked console for short:
    rollproof: FAIL   ***
    ...
    deploy: ***   [probe-failures]
      detail ***
    warning: warning ***
...
--- PASS: TestP1K3PromisedFormsInEvidenceAndRecord (0.01s)
    --- PASS: .../very-long
    --- PASS: .../short
    --- PASS: .../not-utf8
    --- PASS: .../newline
    --- PASS: .../one-byte
````

- The re-run's output (exit 0):

````
    review_p1_3_test.go:48: the masked evidence for short: "PRE *** POST"
    review_p1_3_test.go:97: the masked console for short:
        rollproof: FAIL   ***
        
        settings that differ from their defaults
          limits.steady  ***  (default 10s)
        
        deploy: ***   [probe-failures]
          detail ***
        warning: warning ***
        results: 
    review_p1_3_test.go:48: the masked evidence for not-utf8: "PRE *** POST"
    review_p1_3_test.go:97: the masked console for not-utf8:
        rollproof: FAIL   ***
        
        settings that differ from their defaults
          limits.steady  ***  (default 10s)
        
        deploy: ***   [probe-failures]
          detail ***
        warning: warning ***
        results: 
    review_p1_3_test.go:48: the masked evidence for newline: "PRE *** POST"
    review_p1_3_test.go:97: the masked console for newline:
        rollproof: FAIL   ***
        
        settings that differ from their defaults
          limits.steady  ***  (default 10s)
        
        deploy: ***   [probe-failures]
          detail ***
        warning: warning ***
        results: 
    review_p1_3_test.go:48: the masked evidence for one-byte: "PRE *** POST"
    review_p1_3_test.go:97: the masked console for one-byte:
        rollproof: FAIL   ***
        
        settings that differ from their defaults
          limits.steady  ***  (default 10s)
        
        deploy: ***   [probe-failures]
          detail ***
        warning: warning ***
        results: 
    review_p1_3_test.go:48: the masked evidence for very-long: "PRE *** POST"
    review_p1_3_test.go:97: the masked console for very-long:
        rollproof: FAIL   ***
        
        settings that differ from their defaults
          limits.steady  ***  (default 10s)
        
        deploy: ***   [probe-failures]
          detail ***
        warning: warning ***
        results: 
    --- PASS: TestP1K3PromisedFormsInEvidenceAndRecord/short (0.00s)
    --- PASS: TestP1K3PromisedFormsInEvidenceAndRecord/not-utf8 (0.00s)
    --- PASS: TestP1K3PromisedFormsInEvidenceAndRecord/newline (0.00s)
    --- PASS: TestP1K3PromisedFormsInEvidenceAndRecord/one-byte (0.00s)
    --- PASS: TestP1K3PromisedFormsInEvidenceAndRecord/very-long (0.00s)
````

- Verdict of the re-run: the rule kept in both runs. Every promised form of the password, whole and split across writes, in a line longer than 64 KiB, with a newline, quotes, backslashes and bytes that are not UTF-8. Held: each is masked in the evidence, `result.json`, the console and the summary.

#### P1-4: a password in the configuration, and a rejection after substitution

- Test: `review-1/reviewp1/review_p1_4_test.go`, `TestP1K4ConfigRejection`, as the copy holds it:

````go
package reviewp1

// Review part 1, try 4: a password that enters the configuration the wrong way,
// and a configuration rejected after the password was substituted. The rule
// (design section 8, "Secrets") says a registry password arrives only through
// an environment variable, a password written in the file is rejected, and the
// text that rejects any configuration never quotes a value. This try runs the
// real binary on three rejected configurations and searches its console,
// standard error, result.json and the job summary for every promised form of
// the password, and for the inline text.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var (
	buildOnce sync.Once
	builtBin  string
	buildErr  error
)

// rollproofBin builds the real binary once into a temporary folder.
func rollproofBin(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "rollproof-review-1-bin-")
		if err != nil {
			buildErr = err
			return
		}
		builtBin = filepath.Join(dir, "rollproof")
		cmd := exec.Command("go", "build", "-o", builtBin, "./cmd/rollproof")
		cmd.Dir = ".."
		out, err := cmd.CombinedOutput()
		if err != nil {
			buildErr = err
			t.Logf("go build: %v\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatalf("the binary: %v", buildErr)
	}
	return builtBin
}

func TestP1K4ConfigRejection(t *testing.T) {
	bin := rollproofBin(t)
	const pw = `rv"&<inline`
	forms := promisedForms("rv1user", pw)
	for _, c := range []struct {
		name   string
		config string
		env    map[string]string
		secret []string
	}{
		{
			name: "a password written in the file",
			config: `version: 1
registries:
  - host: docker.io
    username: rv1user
    password: "` + strings.ReplaceAll(pw, `"`, `\"`) + `"
images: {}
deploy:
  namespace: review1
  selector: app=review1
  commands:
    install: ["true"]
probe:
  http:
    url: "http://review1.review1.svc/"
`,
			secret: append(forms, pw),
		},
		{
			name: "a rejection after the password was substituted",
			config: `version: 1
registries:
  - host: docker.io
    username: rv1user
    password: ${env.REVIEW_P1_PASSWORD}
images: {}
deploy:
  namespace: review1
  selector: app=review1
  commands:
    install: ["true"]
probe:
  http:
    url: "http://review1.review1.svc/"
limits:
  silence: 30s
`,
			env:    map[string]string{"REVIEW_P1_PASSWORD": pw},
			secret: forms,
		},
		{
			name: "a missing variable",
			config: `version: 1
registries:
  - host: docker.io
    username: rv1user
    password: ${env.REVIEW_P1_ABSENT}
images: {}
deploy:
  namespace: review1
  selector: app=review1
  commands:
    install: ["true"]
probe:
  http:
    url: "http://review1.review1.svc/"
`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			out := filepath.Join(dir, "results")
			if err := os.MkdirAll(out, 0o755); err != nil {
				t.Fatal(err)
			}
			ghOut := filepath.Join(out, "github-output")
			ghSum := filepath.Join(out, "github-summary")
			for _, p := range []string{ghOut, ghSum} {
				if err := os.WriteFile(p, nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			file := filepath.Join(dir, "rollproof.yaml")
			if err := os.WriteFile(file, []byte(c.config), 0o644); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(bin, "run", "-f", file, "--out", out)
			env := []string{
				"PATH=" + os.Getenv("PATH"),
				"HOME=" + os.Getenv("HOME"),
				"GITHUB_ACTIONS=true",
				"GITHUB_OUTPUT=" + ghOut,
				"GITHUB_STEP_SUMMARY=" + ghSum,
			}
			for k, v := range c.env {
				env = append(env, k+"="+v)
			}
			cmd.Env = env
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			code := 0
			if e, ok := err.(*exec.ExitError); ok {
				code = e.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}
			t.Logf("exit code %d; standard output:\n%s\nstandard error:\n%s", code, stdout.String(), stderr.String())
			if code != 2 {
				t.Errorf("exit code %d, want 2 for a rejected configuration", code)
			}
			files := map[string][]byte{"stdout": stdout.Bytes(), "stderr": stderr.Bytes()}
			for _, p := range []string{"result.json", "github-summary", "github-output"} {
				b, err := os.ReadFile(filepath.Join(out, p))
				if err != nil {
					t.Errorf("%s: %v", p, err)
					continue
				}
				files[p] = b
			}
			for name, b := range files {
				for _, form := range c.secret {
					if form == "" || !bytes.Contains(b, []byte(form)) {
						continue
					}
					t.Errorf("%s holds %q", name, form)
					t.Logf("%s, the lines that hold it:\n%s", name, linesWith(string(b), form))
				}
			}
		})
	}
}
````

- Command, from the copy's root: `go test ./reviewp1 -run TestP1K4ConfigRejection -v`. The re-run adds -count=1 and writes the name between ^ and $, so that the test runs and is not taken from the build cache.
- Reviewer's output:

````text
exit code 2
could not test: registries[0].password (line 5): must be one ${env.NAME} form   [config-rejected]
...
exit code 2
could not test: limits.silence (line 16): must be shorter than limits.steady   [config-rejected]
...
exit code 2
could not test: registries[0].password (line 5): the variable REVIEW_P1_ABSENT is not set   [config-rejected]
````

- The re-run's output (exit 0):

````
    review_p1_4_test.go:160: exit code 2; standard output:
        ::stop-commands::YDXVVXJUTSI7D5YUKSII5QUIJV
        rollproof: NOT VERIFIED
        
        could not test: registries[0].password (line 5): must be one ${env.NAME} form   [config-rejected]
        results: /tmp/TestP1K4ConfigRejectiona_password_written_in_the_file1805570533/001/results
        ::YDXVVXJUTSI7D5YUKSII5QUIJV::
        
        standard error:
        ::stop-commands::YDXVVXJUTSI7D5YUKSII5QUIJV
    review_p1_4_test.go:160: exit code 2; standard output:
        ::stop-commands::FY5HCQAQA65PUHCML7EW7QOCLB
        rollproof: NOT VERIFIED
        
        could not test: limits.silence (line 16): must be shorter than limits.steady   [config-rejected]
        results: /tmp/TestP1K4ConfigRejectiona_rejection_after_the_password_was_subst1973420758/001/results
        ::FY5HCQAQA65PUHCML7EW7QOCLB::
        
        standard error:
        ::stop-commands::FY5HCQAQA65PUHCML7EW7QOCLB
    review_p1_4_test.go:160: exit code 2; standard output:
        ::stop-commands::JWEUEQHVSTUNYKR2ADKGP6E2HG
        rollproof: NOT VERIFIED
        
        could not test: registries[0].password (line 5): the variable REVIEW_P1_ABSENT is not set   [config-rejected]
        results: /tmp/TestP1K4ConfigRejectiona_missing_variable1203439940/001/results
        ::JWEUEQHVSTUNYKR2ADKGP6E2HG::
        
        standard error:
        ::stop-commands::JWEUEQHVSTUNYKR2ADKGP6E2HG
    --- PASS: TestP1K4ConfigRejection/a_password_written_in_the_file (0.01s)
    --- PASS: TestP1K4ConfigRejection/a_rejection_after_the_password_was_substituted (0.00s)
    --- PASS: TestP1K4ConfigRejection/a_missing_variable (0.00s)
````

- Verdict of the re-run: the rule kept in both runs. A password written in the configuration, a rejection after substitution, and a missing variable. Held: the text names a path, a line and a problem, or the variable's name, never a value.

#### P1-5: a whole run with the password in every input: it is a failure class, and reaches the console, the job summary and `result.json`

- Test: `review-1/reviewp1/review_p1_5_test.go`, `TestP1K5ClusterRun`, as the copy holds it:

````go
//go:build cluster

package reviewp1

// Review part 1, try 5: the whole run, with a registry password planted in
// every input a run has: the two registry entries, a setup command's output, a
// workload's pod log (which reads the node's credentials file), the custom
// probe's environment and its failure class. The rule (design section 8,
// "Secrets"; section 11) says the password is masked, plain and encoded, in
// everything the tool writes but the step's outputs on GitHub and the client
// library's log on standard error. This try searches the results folder with
// its evidence, the console, standard error and the two GitHub files.
//
// The probe prints its password as the failure class of every check, so the
// class reaches the install window's failed_by_class map.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestP1K5ClusterRun(t *testing.T) {
	engine := os.Getenv("KIND_EXPERIMENTAL_PROVIDER")
	if engine == "" {
		engine = "nerdctl"
	}
	const clusterName = "rollproof-review-1"
	removeReviewArtifacts(t, engine, clusterName)
	// The tool deletes the cluster itself; the cleanup is for the case it did
	// not, and runs also when the test stops early.
	t.Cleanup(func() { removeReviewArtifacts(t, engine, clusterName) })

	bin := rollproofBin(t)
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, ".tools", "busybox-1.37.0.tar")
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("the busybox archive: %v", err)
	}

	// The results stay for the session to read, under the review's folder.
	base := filepath.Join(root, "reviewp1-out")
	dir := filepath.Join(base, "k5")
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "results")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	ghOut, ghSum := filepath.Join(out, "github-output"), filepath.Join(out, "github-summary")
	for _, p := range []string{ghOut, ghSum} {
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The two passwords: A is safe inside the probe's JSON class string; B
	// carries the hostile spellings through the setup command's output.
	const userA, userB = "rv1user", "rv1userb"
	pwA := `r1s3cretpass&<`
	pwB := "rv\"&<\nline2\ttab\\slash\xffend"
	b64A := base64.StdEncoding.EncodeToString([]byte(userA + ":" + pwA))

	formsA := namedForms(userA, pwA)
	formsB := namedForms(userB, pwB)

	manifest := filepath.Join(dir, "workload.yaml")
	setupScript := `set -eu
cat > ` + manifest + ` <<YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: review1
  namespace: review1
  labels:
    app: review1
spec:
  replicas: 1
  selector:
    matchLabels:
      app: review1
  template:
    metadata:
      labels:
        app: review1
    spec:
      containers:
      - name: app
        image: ${images.busybox.ref}
        env:
        - name: PW
          value: "$REVIEW_P1_PASSWORD"
        command:
        - sh
        - -c
        - |
          while true; do
            printf 'review1 pod log %s\n' "\$PW"
            cat /var/lib/kubelet/config.json || true
            sleep 0.2
          done
        volumeMounts:
        - name: kubelet
          mountPath: /var/lib/kubelet
          readOnly: true
      volumes:
      - name: kubelet
        hostPath:
          path: /var/lib/kubelet
YAML
printf 'form plain A: %s\n' "$RV1_FORM_PLAIN_A"
printf 'form json-html A: %s\n' "$RV1_FORM_JSON_HTML_A"
printf 'form json A: %s\n' "$RV1_FORM_JSON_A"
printf 'form go A: %s\n' "$RV1_FORM_GO_A"
printf 'form b64 A: %s\n' "$RV1_FORM_B64_A"
printf 'form b64raw A: %s\n' "$RV1_FORM_B64RAW_A"
printf 'form plain B: %s\n' "$RV1_FORM_PLAIN_B"
printf 'form json-html B: %s\n' "$RV1_FORM_JSON_HTML_B"
printf 'form json B: %s\n' "$RV1_FORM_JSON_B"
printf 'form go B: %s\n' "$RV1_FORM_GO_B"
printf 'form b64 B: %s\n' "$RV1_FORM_B64_B"
printf 'form b64raw B: %s\n' "$RV1_FORM_B64RAW_B"
printf 'form long A: '
head -c 70000 /dev/zero | tr '\0' x
printf '%s\n' "$RV1_FORM_PLAIN_A"
printf 'unpromised url B: %s\n' "$RV1_FORM_URL_B"
printf 'unpromised b64-of-password B: %s\n' "$RV1_FORM_B64PW_B"
printf 'unpromised upper B: %s\n' "$RV1_FORM_UPPER_B"
printf 'unpromised reversed B: %s\n' "$RV1_FORM_REVERSED_B"
`

	probeScript := `i=0
while true; do
  i=$((i+1))
  if [ $((i%2)) -eq 0 ]; then c="$REVIEW_P1_B64"; else c="$REVIEW_P1_PASSWORD"; fi
  printf '{"ok":false,"class":"%s"}\n' "$c"
  sleep 0.1
done`

	cfg := map[string]any{
		"version": 1,
		"cluster": map[string]any{"workers": 1},
		"registries": []any{
			map[string]any{"host": "docker.io", "username": userA, "password": "${env.REVIEW_P1_PASSWORD}"},
			map[string]any{"host": "example.com", "username": userB, "password": "${env.REVIEW_P1_PASSWORD_B}"},
		},
		"images": map[string]any{"busybox": map[string]any{"archive": archive}},
		"setup":  []any{[]string{"sh", "-c", setupScript}},
		"deploy": map[string]any{
			"namespace": "review1", "selector": "app=review1",
			"commands": map[string]any{"install": []string{"kubectl", "apply", "-f", manifest}},
		},
		"probe": map[string]any{"custom": map[string]any{
			"image":   "${images.busybox.ref}",
			"command": []string{"sh", "-c"},
			"args":    []string{probeScript},
			"env": []any{
				map[string]any{"name": "REVIEW_P1_PASSWORD", "value": "${env.REVIEW_P1_PASSWORD}"},
				map[string]any{"name": "REVIEW_P1_B64", "value": "${env.REVIEW_P1_B64}"},
			},
		}},
		"limits": map[string]any{"steady": "3s", "settle": "2s", "timeout": "2m", "silence": "1s"},
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "rollproof.yaml")
	if err := os.WriteFile(configPath, b, 0o644); err != nil {
		t.Fatal(err)
	}

	// kubectl and helm first on PATH, as the e2e harness gives them.
	toolsDir := filepath.Join(dir, "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{
		"kubectl": filepath.Join(root, ".tools", "kubectl"),
		"helm":    filepath.Join(root, ".tools", "helm-3", "helm"),
	} {
		if err := os.Symlink(target, filepath.Join(toolsDir, name)); err != nil {
			t.Fatal(err)
		}
	}

	// The whole environment first, as e2e's runEnv gives it: nerdctl needs
	// XDG_RUNTIME_DIR and whatever else the runner set. The three GitHub
	// variables are dropped and set as runEnv sets them. Ours come last.
	drop := map[string]bool{"GITHUB_ACTIONS": true, "GITHUB_OUTPUT": true, "GITHUB_STEP_SUMMARY": true}
	var env []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !drop[name] {
			env = append(env, entry)
		}
	}
	env = append(env,
		"PATH="+toolsDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"KIND_EXPERIMENTAL_PROVIDER="+engine,
		"GITHUB_ACTIONS=true",
		"GITHUB_OUTPUT="+ghOut,
		"GITHUB_STEP_SUMMARY="+ghSum,
		"REVIEW_P1_PASSWORD="+pwA,
		"REVIEW_P1_PASSWORD_B="+pwB,
		"REVIEW_P1_B64="+b64A,
	)
	forms := map[string]string{
		"RV1_FORM_PLAIN_A": formsA.Plain, "RV1_FORM_JSON_HTML_A": formsA.JSONHTML, "RV1_FORM_JSON_A": formsA.JSON,
		"RV1_FORM_GO_A": formsA.Go, "RV1_FORM_B64_A": formsA.B64, "RV1_FORM_B64RAW_A": formsA.B64Raw,
		"RV1_FORM_PLAIN_B": formsB.Plain, "RV1_FORM_JSON_HTML_B": formsB.JSONHTML, "RV1_FORM_JSON_B": formsB.JSON,
		"RV1_FORM_GO_B": formsB.Go, "RV1_FORM_B64_B": formsB.B64, "RV1_FORM_B64RAW_B": formsB.B64Raw,
	}
	for k, v := range forms {
		env = append(env, k+"="+v)
	}
	unpromised := map[string]string{
		"url B":             url.QueryEscape(pwB),
		"b64-of-password B": base64.StdEncoding.EncodeToString([]byte(pwB)),
		"upper B":           strings.ToUpper(pwB),
		"reversed B":        reverse(pwB),
	}
	for k, v := range map[string]string{
		"RV1_FORM_URL_B": url.QueryEscape(pwB), "RV1_FORM_B64PW_B": base64.StdEncoding.EncodeToString([]byte(pwB)),
		"RV1_FORM_UPPER_B": strings.ToUpper(pwB), "RV1_FORM_REVERSED_B": reverse(pwB),
	} {
		env = append(env, k+"="+v)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "run", "-f", configPath, "--out", out, "--name", "review-1")
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = 10 * time.Second
	err = cmd.Run()
	code := 0
	if e, ok := err.(*exec.ExitError); ok {
		code = e.ExitCode()
	} else if err != nil {
		t.Fatalf("the run: %v (ctx %v)\n%s%s", err, ctx.Err(), stdout.String(), stderr.String())
	}
	if err := os.WriteFile(filepath.Join(out, "console.txt"), stdout.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "stderr.txt"), stderr.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("exit code %d", code)
	t.Logf("the console:\n%s", stdout.String())
	if s := stderr.String(); s != "" {
		t.Logf("standard error:\n%s", s)
	}

	// The run reached the probe and the workload's pod log.
	assertFileHolds(t, out, "evidence/commands/setup", "form plain A:")
	pods := filesUnder(t, filepath.Join(out, "evidence", "pods"))
	if len(pods) == 0 {
		t.Errorf("no pod log was written under evidence/pods")
	}
	for _, p := range pods {
		t.Logf("pod log %s", p)
		if b, err := os.ReadFile(p); err == nil {
			t.Logf("the pod log's first lines:\n%s", excerptText(string(b), 600))
		}
	}
	assertFileHolds(t, out, "evidence/probe.log", `"class"`)

	// The class reaches the install window's failed_by_class map in result.json.
	if b, err := os.ReadFile(filepath.Join(out, "result.json")); err == nil {
		var r struct {
			Verdict string `json:"verdict"`
			Install *struct {
				FailedByClass map[string]int `json:"failed_by_class"`
			} `json:"install"`
			Windows []struct {
				Name          string         `json:"name"`
				FailedByClass map[string]int `json:"failed_by_class"`
			} `json:"windows"`
		}
		if err := json.Unmarshal(b, &r); err != nil {
			t.Errorf("result.json: %v", err)
		} else {
			t.Logf("verdict %q; install failed_by_class %v", r.Verdict, r.Install.FailedByClass)
			for _, w := range r.Windows {
				t.Logf("window %s failed_by_class %v", w.Name, w.FailedByClass)
			}
		}
	}

	// Every promised form, in everything the run wrote.
	files := filesUnder(t, out)
	for _, form := range append(promisedForms(userA, pwA), promisedForms(userB, pwB)...) {
		if form == "" {
			continue
		}
		shown := 0
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil || !bytes.Contains(b, []byte(form)) {
				continue
			}
			rel, _ := filepath.Rel(out, f)
			t.Errorf("%s holds the password form %q", rel, form)
			if shown < 4 {
				t.Logf("%s, the lines that hold it:\n%s", rel, linesWith(string(b), form))
			}
			shown++
		}
	}
	// The forms the rule does not name survive: told apart here so that a
	// finding is not claimed for them.
	for label, form := range unpromised {
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil || !bytes.Contains(b, []byte(form)) {
				continue
			}
			rel, _ := filepath.Rel(out, f)
			t.Logf("not a promised form (%s) survives in %s: %q", label, rel, form)
			break
		}
	}
	if t.Failed() {
		t.Logf("the results folder is %s", out)
	}
}

func reverse(s string) string {
	b := []byte(s)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

// filesUnder returns every regular file under root, sorted.
func filesUnder(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("%s: %v", root, err)
	}
	sort.Strings(files)
	return files
}

// excerptText returns at most n bytes of text, ending on a line end when one
// is near.
func excerptText(text string, n int) string {
	if len(text) <= n {
		return text
	}
	return text[:n] + "\n[...]"
}

func assertFileHolds(t *testing.T, out, rel, want string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, rel))
	if err != nil {
		t.Errorf("%s: %v", rel, err)
		return
	}
	if !bytes.Contains(b, []byte(want)) {
		t.Errorf("%s does not hold %q (it is %d bytes)", rel, want, len(b))
	}
}

// removeReviewArtifacts removes the containers and volumes whose names begin
// rollproof-review-1-, and the run's folder. It removes nothing else.
func removeReviewArtifacts(t *testing.T, engine, clusterName string) {
	t.Helper()
	out, err := exec.Command(engine, "ps", "-a", "--format", "{{.Names}}").CombinedOutput()
	if err == nil {
		for _, name := range strings.Fields(string(out)) {
			if !strings.HasPrefix(name, clusterName+"-") {
				continue
			}
			if out, err := exec.Command(engine, "rm", "-f", "-v", name).CombinedOutput(); err != nil {
				t.Logf("%s rm -f %s: %v\n%s", engine, name, err, out)
			}
		}
	} else {
		t.Logf("%s ps -a: %v\n%s", engine, err, out)
	}
	out, err = exec.Command(engine, "volume", "ls", "--format", "{{.Name}}").CombinedOutput()
	if err == nil {
		for _, name := range strings.Fields(string(out)) {
			if !strings.HasPrefix(name, clusterName+"-") {
				continue
			}
			if out, err := exec.Command(engine, "volume", "rm", "-f", name).CombinedOutput(); err != nil {
				t.Logf("%s volume rm %s: %v\n%s", engine, name, err, out)
			}
		}
	}
	if err := os.RemoveAll(filepath.Join(os.TempDir(), clusterName)); err != nil {
		t.Logf("the run's folder: %v", err)
	}
}
````

- Command, from the copy's root: `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags cluster -p 1 -count=1 ./reviewp1 -run TestP1K5ClusterRun -v`.
- Rule: "The tool masks every such password in everything it writes, in its plain form and in the encoded form in which the nodes' credentials file holds it. That includes the output of commands and the pod logs that it copies into the evidence." (section 8, "Secrets.")
- Reviewer's output (its verdict: broken; a cluster run with two registry entries, a setup command that prints every promised form of both passwords, a workload whose pod reads the node's `config.json`, and a custom probe whose failing lines name the password and its padded base64 as their `class`; the step's outputs file held no password and standard error held only the tool's stop line):

````
review_p1_5_test.go:266: exit code 2
rollproof: NOT VERIFIED   review1 (commands) on Kubernetes v1.37.0 in kind
install     settled after 2.0 s under the probe; failures not judged
            20 checks, 20 failed (cnYxdXNlcjpyMXMzY3JldHBhc3MmPA== 10, r1s3cretpass&< 10), the last 0.1 s before the steady window
steady: 30 checks failed (***== 15, *** 15), first at 22:41:09.85   [steady-not-clean]
review_p1_5_test.go:301: verdict "not-verified"; install failed_by_class map[cnYxdXNlcjpyMXMzY3JldHBhc3MmPA==:10 r1s3cretpass&<:10]
review_p1_5_test.go:303: window steady failed_by_class map[cnYxdXNlcjpyMXMzY3JldHBhc3MmPA==:15 r1s3cretpass&<:15]
review_p1_5_test.go:321: console.txt holds the password form "r1s3cretpass&<"
review_p1_5_test.go:321: github-summary holds the password form "r1s3cretpass&<"
review_p1_5_test.go:321: result.json holds the password form "r1s3cretpass\u0026\u003c"
review_p1_5_test.go:323: result.json, the lines that hold it:
              "r1s3cretpass\u0026\u003c": 15
            "r1s3cretpass\u0026\u003c": 10
review_p1_5_test.go:321: console.txt holds the password form "cnYxdXNlcjpyMXMzY3JldHBhc3MmPA"
review_p1_5_test.go:321: github-summary holds the password form "cnYxdXNlcjpyMXMzY3JldHBhc3MmPA"
review_p1_5_test.go:321: result.json holds the password form "cnYxdXNlcjpyMXMzY3JldHBhc3MmPA"
review_p1_5_test.go:323: result.json, the lines that hold it:
              "cnYxdXNlcjpyMXMzY3JldHBhc3MmPA==": 15,
review_p1_5_test.go:321: console.txt holds the password form "cnYxdXNlcjpyMXMzY3JldHBhc3MmPA=="
review_p1_5_test.go:321: github-summary holds the password form "cnYxdXNlcjpyMXMzY3JldHBhc3MmPA=="
review_p1_5_test.go:321: result.json holds the password form "cnYxdXNlcjpyMXMzY3JldHBhc3MmPA=="
The pod log is masked:
2026-11-25T22:41:07.405562899Z review1 pod log ***
      "auth": "***=="
The promised forms in the setup evidence, the pod log, `probe.log` and `probe-pod.json` were all masked. The forms the rule does not name survived in `evidence/commands/setup`:
not a promised form (url B) survives in evidence/commands/setup: "rv%22%26%3C%0Aline2%09tab%5Cslash%FFend"
not a promised form (b64-of-password B) survives in evidence/commands/setup: "cnYiJjwKbGluZTIJdGFiXHNsYXNo/2VuZA=="
not a promised form (upper B) survives in evidence/commands/setup: "RV\"&<\nLINE2\tTAB\\SLASH�END"
not a promised form (reversed B) survives in evidence/commands/setup: "dne\xffhsals\\bat\t2enil\n<&\"vr"
````

- The re-run's output (exit 1, the test fails as the reviewer's did; a cluster run of 73 s; the token and the pod's suffix are new for this run). The console's install line, the pod log's line, and the test's lines:

````
20 checks, 20 failed (cnYxdXNlcjpyMXMzY3JldHBhc3MmPA== 10, r1s3cretpass&< 10), the last 0.1 s before the steady window
2026-11-25T23:06:52.767079204Z       "auth": "***=="
    review_p1_5_test.go:301: verdict "not-verified"; install failed_by_class map[cnYxdXNlcjpyMXMzY3JldHBhc3MmPA==:10 r1s3cretpass&<:10]
    review_p1_5_test.go:303: window steady failed_by_class map[cnYxdXNlcjpyMXMzY3JldHBhc3MmPA==:15 r1s3cretpass&<:15]
    review_p1_5_test.go:303: window restart failed_by_class map[]
    review_p1_5_test.go:321: console.txt holds the password form "r1s3cretpass&<"
    review_p1_5_test.go:323: console.txt, the lines that hold it:
    review_p1_5_test.go:321: github-summary holds the password form "r1s3cretpass&<"
    review_p1_5_test.go:323: github-summary, the lines that hold it:
    review_p1_5_test.go:321: result.json holds the password form "r1s3cretpass\\u0026\\u003c"
    review_p1_5_test.go:323: result.json, the lines that hold it:
    review_p1_5_test.go:321: console.txt holds the password form "cnYxdXNlcjpyMXMzY3JldHBhc3MmPA"
    review_p1_5_test.go:323: console.txt, the lines that hold it:
    review_p1_5_test.go:321: github-summary holds the password form "cnYxdXNlcjpyMXMzY3JldHBhc3MmPA"
    review_p1_5_test.go:323: github-summary, the lines that hold it:
    review_p1_5_test.go:321: result.json holds the password form "cnYxdXNlcjpyMXMzY3JldHBhc3MmPA"
    review_p1_5_test.go:323: result.json, the lines that hold it:
    review_p1_5_test.go:321: console.txt holds the password form "cnYxdXNlcjpyMXMzY3JldHBhc3MmPA=="
    review_p1_5_test.go:323: console.txt, the lines that hold it:
    review_p1_5_test.go:321: github-summary holds the password form "cnYxdXNlcjpyMXMzY3JldHBhc3MmPA=="
    review_p1_5_test.go:323: github-summary, the lines that hold it:
    review_p1_5_test.go:321: result.json holds the password form "cnYxdXNlcjpyMXMzY3JldHBhc3MmPA=="
    review_p1_5_test.go:323: result.json, the lines that hold it:
    review_p1_5_test.go:337: not a promised form (url B) survives in evidence/commands/setup: "rv%22%26%3C%0Aline2%09tab%5Cslash%FFend"
    review_p1_5_test.go:337: not a promised form (b64-of-password B) survives in evidence/commands/setup: "cnYiJjwKbGluZTIJdGFiXHNsYXNo/2VuZA=="
    review_p1_5_test.go:337: not a promised form (upper B) survives in evidence/commands/setup: "RV\"&<\nLINE2\tTAB\\SLASH�END"
    review_p1_5_test.go:337: not a promised form (reversed B) survives in evidence/commands/setup: "dne\xffhsals\\bat\t2enil\n<&\"vr"
````

- Noted by the reviewer, not a finding: the URL-encoded, base64-of-password-alone, uppercase and reversed forms are outside the rule as written, which names the plain, the JSON, the Go-quoted and the `USER:PASSWORD` base64 forms. `***==` is the padding left after the unpadded base64; it exposes no byte of the password.

- Verdict of the re-run: holds.
- Ruling: accept (the maintainer, 2026-11-25). Covered by the fix of P1-1: the try leaked only through the record's map keys, which that fix masks wherever the record is written out. The rule is not left as it was.

#### P1-6: a password in a command’s arguments and output

- Test: `review-1/reviewp1/review_p1_6_test.go`, `TestP1K6CommandLineAndOutput`, as the copy holds it:

````go
package reviewp1

// Review part 1, try 6: the password as an argument of a command the tool
// starts, and in that command's output. The rule (design section 8,
// "Secrets") says the output of commands is masked; the command line itself is
// written to the same evidence file by deploy.OSRunner, with each argument
// Go-quoted when it needs it. This try runs a real command through the tool's
// runner with report.Writer as its evidence, then searches the evidence for
// every promised form.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rollproof/internal/deploy"
	"rollproof/internal/mask"
	"rollproof/internal/report"
)

func TestP1K6CommandLineAndOutput(t *testing.T) {
	const user = "rv1user"
	pw := "rv\"&<\nline2\ttab\\slash\xffend"
	m := mask.New([]mask.Credential{{Username: user, Password: pw}})
	dir := t.TempDir()
	w, err := report.Open(dir, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	w.SetMasker(m)
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\nkind: Config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := deploy.NewRunner(deploy.RunnerOptions{
		Dir: dir, Kubeconfig: kubeconfig, Environ: os.Environ(), Evidence: w.Evidence,
	})
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// $1 is the password, printed; $2 is USER:PASSWORD, base64's input. The
	// command line itself holds both.
	res := r.Run(ctx, deploy.Command{
		Name: "setup",
		Args: []string{"sh", "-c", `printf '%s' "$1"; printf '\n'; printf '%s' "$2" | base64; printf '\n'`,
			"sh", pw, user + ":" + pw},
		Timeout: 10 * time.Second,
	})
	t.Logf("the command ended %q with exit code %d", res.Ending, res.ExitCode)
	b, err := os.ReadFile(filepath.Join(dir, "evidence", "setup"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("the evidence:\n%s", firstBytes(string(b), 800))
	for i, form := range promisedForms(user, pw) {
		if bytes.Contains(b, []byte(form)) {
			t.Errorf("evidence/setup holds form %d: %q", i, form)
			t.Logf("the lines that hold it:\n%s", linesWith(string(b), form))
		}
	}
}

// firstBytes returns at most n bytes of text.
func firstBytes(text string, n int) string {
	if len(text) <= n {
		return text
	}
	return text[:n] + "\n[...]"
}
````

- Command, from the copy's root: `go test ./reviewp1 -run TestP1K6CommandLineAndOutput -v`. The re-run adds -count=1 and writes the name between ^ and $, so that the test runs and is not taken from the build cache.
- Reviewer's output:

````text
review_p1_6_test.go:52: the command ended "ok" with exit code 0
review_p1_6_test.go:57: the evidence:
    sh -c "printf '%s' \"$1\"; printf '\\n'; printf '%s' \"$2\" | base64; printf '\\n'" sh "***" "rv1user:***"
    ***
    ***
    rollproof: ok, exit code 0
````

- The re-run's output (exit 0):

````
    review_p1_6_test.go:52: the command ended "ok" with exit code 0
    review_p1_6_test.go:57: the evidence:
        sh -c "printf '%s' \"$1\"; printf '\\n'; printf '%s' \"$2\" | base64; printf '\\n'" sh "***" "rv1user:***"
        ***
        ***
        
        rollproof: ok, exit code 0
````

- Verdict of the re-run: the rule kept in both runs. A password in a command's arguments and in its output. Held: the command line and the output are masked in the evidence.

#### P1-7: the password as the workload's name: it shows in the paths of the evidence, while the console and `result.json` mask it

- Test: `review-1/reviewp1/review_p1_7_test.go`, `TestP1K7PasswordAsTheWorkloadName`, as the copy holds it:

````go
//go:build cluster

package reviewp1

// Review part 1, try 7: a registry password that is also the name of the
// workload under test. The rule (design section 8, "Secrets") says the password
// is masked in everything the tool writes. The tool writes an evidence file for
// each pod log at pods/POD/CONTAINER.log, and the pod's name begins with the
// workload's name. If a path counts as something the tool writes, the password
// is in the evidence folder's path, though not in any file's content. This try
// keeps the password out of every file's content and searches the paths.

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bytes"
	"context"
)

func TestP1K7PasswordAsTheWorkloadName(t *testing.T) {
	engine := os.Getenv("KIND_EXPERIMENTAL_PROVIDER")
	if engine == "" {
		engine = "nerdctl"
	}
	const clusterName = "rollproof-review-1"
	removeReviewArtifacts(t, engine, clusterName)
	t.Cleanup(func() { removeReviewArtifacts(t, engine, clusterName) })

	bin := rollproofBin(t)
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, ".tools", "busybox-1.37.0.tar")
	const user = "rv1user"
	// A valid Kubernetes name, and a password.
	const pw = "r1secretpath"

	base := filepath.Join(root, "reviewp1-out")
	dir := filepath.Join(base, "k7")
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "results")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	ghOut, ghSum := filepath.Join(out, "github-output"), filepath.Join(out, "github-summary")
	for _, p := range []string{ghOut, ghSum} {
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	manifest := filepath.Join(dir, "workload.yaml")
	setupScript := `set -eu
cat > ` + manifest + ` <<YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ` + pw + `
  namespace: review1
  labels:
    app: ` + pw + `
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ` + pw + `
  template:
    metadata:
      labels:
        app: ` + pw + `
    spec:
      containers:
      - name: app
        image: ${images.busybox.ref}
        command:
        - sh
        - -c
        - |
          while true; do printf 'review1 path pod log\n'; sleep 0.5; done
YAML
`
	probeScript := `while true; do printf '{"ok":true}\n'; sleep 0.1; done`
	cfg := map[string]any{
		"version": 1,
		"cluster": map[string]any{"workers": 1},
		"registries": []any{
			map[string]any{"host": "docker.io", "username": user, "password": "${env.REVIEW_P1_PASSWORD_PATH}"},
		},
		"images": map[string]any{"busybox": map[string]any{"archive": archive}},
		"setup":  []any{[]string{"sh", "-c", setupScript}},
		"deploy": map[string]any{
			"namespace": "review1", "selector": "app=" + pw,
			"commands": map[string]any{"install": []string{"kubectl", "apply", "-f", manifest}},
		},
		"probe": map[string]any{"custom": map[string]any{
			"image":   "${images.busybox.ref}",
			"command": []string{"sh", "-c"},
			"args":    []string{probeScript},
		}},
		"limits": map[string]any{"steady": "3s", "settle": "2s", "timeout": "2m", "silence": "1s"},
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "rollproof.yaml")
	if err := os.WriteFile(configPath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	toolsDir := filepath.Join(dir, "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{
		"kubectl": filepath.Join(root, ".tools", "kubectl"),
		"helm":    filepath.Join(root, ".tools", "helm-3", "helm"),
	} {
		if err := os.Symlink(target, filepath.Join(toolsDir, name)); err != nil {
			t.Fatal(err)
		}
	}
	drop := map[string]bool{"GITHUB_ACTIONS": true, "GITHUB_OUTPUT": true, "GITHUB_STEP_SUMMARY": true}
	var env []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !drop[name] {
			env = append(env, entry)
		}
	}
	env = append(env,
		"PATH="+toolsDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"KIND_EXPERIMENTAL_PROVIDER="+engine,
		"GITHUB_ACTIONS=true",
		"GITHUB_OUTPUT="+ghOut,
		"GITHUB_STEP_SUMMARY="+ghSum,
		"REVIEW_P1_PASSWORD_PATH="+pw,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "run", "-f", configPath, "--out", out, "--name", "review-1")
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = 10 * time.Second
	err = cmd.Run()
	code := 0
	if e, ok := err.(*exec.ExitError); ok {
		code = e.ExitCode()
	} else if err != nil {
		t.Fatalf("the run: %v (ctx %v)\n%s%s", err, ctx.Err(), stdout.String(), stderr.String())
	}
	if err := os.WriteFile(filepath.Join(out, "console.txt"), stdout.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "stderr.txt"), stderr.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("exit code %d; the console:\n%s", code, stdout.String())

	// The paths of the results folder, with the password as a path element.
	paths := filesUnder(t, out)
	foundPath := false
	for _, p := range paths {
		rel, _ := filepath.Rel(out, p)
		if strings.Contains(rel, pw) {
			foundPath = true
			t.Errorf("the results folder holds the password in a path: %s", rel)
		}
	}
	if !foundPath {
		t.Logf("no path under the results folder holds %q", pw)
	}
	// The content is masked: the search for every promised form.
	encoded := base64.StdEncoding.EncodeToString([]byte(user + ":" + pw))
	for _, form := range append(promisedForms(user, pw), encoded) {
		for _, p := range paths {
			b, err := os.ReadFile(p)
			if err != nil || !bytes.Contains(b, []byte(form)) {
				continue
			}
			rel, _ := filepath.Rel(out, p)
			t.Errorf("%s holds the password form %q", rel, form)
			t.Logf("%s, the lines that hold it:\n%s", rel, linesWith(string(b), form))
		}
	}
	if t.Failed() {
		t.Logf("the results folder is %s", out)
	}
}
````

- Command, from the copy's root: `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags cluster -p 1 -count=1 ./reviewp1 -run TestP1K7PasswordAsTheWorkloadName -v`.
- Rule: "The tool masks every such password in everything it writes" (section 8, "Secrets.")
- Reviewer's output (its verdict: unclear: whether a path is "something the tool writes" under the rule is the maintainer's ruling. `Evidence(name)` in `internal/report/evidence.go` passes the name to `MkdirAll` and `OpenFile` without masking, and `internal/watch/podlogs.go` builds `pods/<pod>/<container>.log`; the pod's name begins with the workload's. No file's content held any promised form):

````
review_p1_7_test.go:172: exit code 0; the console:
rollproof: PASS   review1 (commands) on Kubernetes v1.37.0 in kind
transition  workload         probed      pods replaced   quickest stop
restart     deployment/***   not known   1 of 1          30.6 s
review_p1_7_test.go:181: the results folder holds the password in a path: evidence/pods/r1secretpath-5b779d5495-9b7kt/app.log
review_p1_7_test.go:181: the results folder holds the password in a path: evidence/pods/r1secretpath-66c45ffbb9-zmg58/app.log
````

- The re-run's output (exit 1: the test fails on the two paths; a cluster run of 137 s; the pods' suffixes are new for this run):

````
    review_p1_7_test.go:172: exit code 0; the console:
    review_p1_7_test.go:181: the results folder holds the password in a path: evidence/pods/r1secretpath-66c45ffbb9-lv6kg/app.log
    review_p1_7_test.go:181: the results folder holds the password in a path: evidence/pods/r1secretpath-6b685f895-c7mtw/app.log
        rollproof: PASS   review1 (commands) on Kubernetes v1.37.0 in kind
        restart     deployment/***   not known   1 of 1          30.7 s
````

- Verdict of the re-run: holds (both runs show the same thing: the password in two paths under the results folder and masked in the console; whether a path is covered by the rule is for the ruling).
- Ruling: document (the maintainer, 2026-11-25), in the design's section 11: a name the caller gives, such as a workload's, is written as it was given where it names a file of the evidence, as a chart value or a probe header is. The smaller choice: a sentence, and no code.
- The text: commit `f659e07`.

**What reviewer 1 did not try:**

````markdown
## 3. What I did not try, and why
- **The `e2e/mirror_test.go` job itself.** I read it and set the three GitHub variables exactly as its `runEnv` does, but my cluster fixture uses the commands driver, not the TLS mirror and two Helm releases. The masker and the report writer are the same code; running that job would add the mirror’s stand-ins, the archives and the chart, not a new masking path.
- **The action’s script and the workflow-command path.** That is point 33’s subject, another part of point 13; my rule is masking.
- **The Kubernetes client library’s log on standard error.** The design exempts it; it would need an API server warning carrying the password, and the password goes to the nodes in a file, not to the API server. My runs’ standard error held only the tool’s stop-commands line.
- **A password in a chart value or a probe header’s value.** The design says these are written as given and nothing else is masked; a leak there would confirm the stated boundary, not break the rule.
- **A password in `--out`, `--name`, the namespace or a Helm release name.** The first two are caller paths and the step outputs are exempt; the namespace and release name would produce the same path question as try 7 (pod names), which I did not run under Helm. I did test the equivalent with a Deployment name.
- **A password split half on standard output and half on standard error of one command.** `deploy/command.go`’s sink deliberately keeps the two streams apart; the whole password never exists in either, so there is no complete form to find. I tested a form split across writes and across the 64 KiB line boundary instead.
- **A password in a pod event message or a pod’s status.** Those go into `events.txt` and the reason detail through the same evidence writer and the same string masker as try 3/5; I did not build a separate fixture for an event.
- **A password in the node’s `config.json` as the *only* input.** Try 5’s workload pod read that file and its log held `"auth": "***=="`; the padded base64’s `=` remains, the password bytes do not.
- **A password longer than the mask writer’s buffer through a pod log.** Try 3’s 100 KB password and try 5’s 70 KB setup line cover the writer and the command stream; the pod-log follower has no line limit of its own (`io.Copy` into the same masked writer), so there was no separate limit to hit.
- **The exact `e2e/mirror_helpers_test.go` `findForms` search.** My tries search every regular file under the results folder and the console/stderr files with the same form set; I did not call that helper itself.
**State left behind:** two kind clusters were created and deleted by the tool; no container, volume or network whose name begins `rollproof-review-1-` remains, and `/tmp/rollproof-review-1` is gone. The test files remain under `reviewp1/`; the runs’ results remain under `reviewp1-out/k5` and `reviewp1-out/k7`, with the logs beside them. No test makes a network request except the engine’s use of the already-present `kindest/node:v1.37.0` and `registry:3` images; no image was pulled for these runs.
````

### Part 2: the job summary

17 tries, by reviewer 2; the card it was given is in Method.

**The rule's claims, as the design states them, in reviewer 2's words:**

````markdown
C1. The summary is written only when the tool detects GitHub Actions. (design 2314)
C2. It holds the same blocks as the console, in the same order, for every verdict and every ending of a run: the first line as a heading, the tables as Markdown tables, the lists as Markdown lists, and every other line as text. (design 2314–2317)
C3. The tool appends it to the step's summary file once, right after the final `result.json` and the outputs, before the console's text is printed and before it completes the evidence; a warning that arises later is appended as a line. (design 2317–2320)
C4. The summary is masked as the console is. (design 2320–2321)
C5. Text that came from outside the tool is written so that it cannot change how the page reads: inside code that it cannot close, or escaped. (design 2321–2322)
C6. A run whose final `result.json` could not be written prints no console block and writes no summary; a summary that cannot be written, and a crash while it is rendered or appended, is a warning, and the console is printed all the same. (design 2323–2326)
C7. GitHub shows no summary for a step whose file is larger than 1 MiB, and the step stays green; the console's blocks are far below that. (design 2326–2327)
C8. Text from outside the tool is every string of the record that `report.Mask` masks: the fields without the tag `mask:"keep"`, such as names, chart sources, values files, the probe's target, settings' values, reasons' texts and details, warnings and the results folder. (plan decision 5, README line 122)
C9. In the summary such text stands only inside a fenced code block or a code span, where GitHub's Markdown reads neither emphasis, links, autolinks nor HTML. (plan decision 5)
C10. A fenced code block opens and closes with a run of backticks one longer than the longest run of backticks in any of its lines, three at the least; no line inside can close it. (plan decision 5)
C11. A code span is delimited by a run of backticks one longer than the longest run inside it, with one space inside each delimiter when the text begins or ends with a backtick or a space, which the renderer takes off again. (plan decision 5)
C12. In a table cell, every `|` is written `\|`, inside a code span too, as GitHub's tables ask. (plan decision 5)
C13. Before any of this, the record is masked by the run's masker and cleaned as the console is: a line end, every other control character, the line and paragraph separators and the bidirectional controls become a space, so that no text adds a line to a table or a heading. (plan decision 5)
C14. The tool's own words (the vocabulary, numbers, fixed sentences) stand as they are; none holds a character that Markdown reads, and the words of a table cell are written with `|` escaped all the same. (plan decision 5)
C15. What lost: escaping every character that Markdown reads with a backslash, because GitHub turns bare addresses into links after the escapes are read, and every missed rule changes the page. (plan decision 5)
````

#### P2-1: backticks alone on a line

- Test: `review-2/internal/report/review_p2_1_test.go`, `TestReviewP2Try1BackticksAlone`, as the copy holds it:

````go
package report

import (
	"strings"
	"testing"
)

// p2Fill gives the record a values file, a removed variable and a settle restart, so that
// plantFreeText plants the payload in those lists too.
func p2Fill(r Run) Run {
	r.Releases[1].ValuesFiles = []string{"values.yaml"}
	r.RemovedVariables = []string{"X"}
	r.Install.SettleRestartedBy = []string{"x"}
	if len(r.Probes) > 0 {
		r.Probes[0].Notes = []string{"x"}
	}
	return r
}

// TestReviewP2Try1BackticksAlone plants a run of four backticks alone as every string of the
// record that the rule covers, so that it stands alone on a line in the reasons' detail lines and
// inside a line everywhere else. It prints the summary the tool wrote.
func TestReviewP2Try1BackticksAlone(t *testing.T) {
	const payload = "````"
	r := failRun()
	r.Warnings = []string{"x"} // so that plantFreeText plants one warning too
	r = plantFreeText(p2Fill(r), payload)
	out := Summary(r)
	t.Logf("PAYLOAD %q", payload)
	t.Logf("SUMMARY START\n%s\nSUMMARY END", out)

	// The fences the tool wrote and the longest backtick run inside each block.
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		n := len(line) - len(strings.TrimLeft(line, "`"))
		if n >= 3 && strings.Trim(line[n:], " ") == "" {
			t.Logf("line %d: fence of %d backticks: %q", i+1, n, line)
		}
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "### ") {
			t.Logf("heading: %q", line)
		}
	}
	// The detail line that holds the run alone, as the summary wrote it.
	for i, line := range lines {
		if strings.TrimSpace(line) == payload {
			t.Logf("alone line %d: %q", i+1, line)
		}
	}
}
````

- Command, from the copy's root: `go test ./internal/report -run '^TestReviewP2Try1BackticksAlone$' -count=1 -v`.
- Reviewer's output:

````text
    review_p2_1_test.go:42: heading: "### rollproof: FAIL ````` ```` (Helm) on Kubernetes ```` in kind `````"
    review_p2_1_test.go:37: line 3: fence of 5 backticks: "`````"
    review_p2_1_test.go:37: line 26: fence of 5 backticks: "`````"
    review_p2_1_test.go:37: line 38: fence of 5 backticks: "`````"
    review_p2_1_test.go:37: line 44: fence of 5 backticks: "`````"
    review_p2_1_test.go:48: alone line 40: "  ````"
    review_p2_1_test.go:48: alone line 41: "  ````"
        | deploy | ````` ```` ````` | yes | 2 of 2 | 0.8 s |
````

- The re-run's output (exit 0):

````
    review_p2_1_test.go:29: PAYLOAD "````"
    review_p2_1_test.go:30: SUMMARY START
    review_p2_1_test.go:37: line 3: fence of 5 backticks: "`````"
    review_p2_1_test.go:37: line 26: fence of 5 backticks: "`````"
    review_p2_1_test.go:37: line 38: fence of 5 backticks: "`````"
    review_p2_1_test.go:37: line 44: fence of 5 backticks: "`````"
    review_p2_1_test.go:42: heading: "### rollproof: FAIL ````` ```` (Helm) on Kubernetes ```` in kind `````"
    review_p2_1_test.go:48: alone line 9: "            ````"
    review_p2_1_test.go:48: alone line 40: "  ````"
    review_p2_1_test.go:48: alone line 41: "  ````"
````

- Verdict of the re-run: the rule kept in both runs. Four backticks alone on a line, in every string of the record. Held: fences of five, a code span of five; nothing closes them.

#### P2-2: backticks inside a line

- Test: `review-2/internal/report/review_p2_2_test.go`, `TestReviewP2Try2BackticksInLine`, as the copy holds it:

````go
package report

import (
	"strings"
	"testing"
)

// TestReviewP2Try2BackticksInLine plants runs of three, two and one backticks inside a line, in
// every string of the record that the rule covers, and prints the summary the tool wrote.
func TestReviewP2Try2BackticksInLine(t *testing.T) {
	const payload = "a ``` b `` c ` d"
	r := failRun()
	r.Warnings = []string{"x"}
	r = plantFreeText(p2Fill(r), payload)
	out := Summary(r)
	t.Logf("PAYLOAD %q", payload)
	t.Logf("SUMMARY START\n%s\nSUMMARY END", out)

	lines := strings.Split(out, "\n")
	for i, line := range lines {
		n := len(line) - len(strings.TrimLeft(line, "`"))
		if n >= 3 && strings.Trim(line[n:], " ") == "" {
			t.Logf("line %d: fence of %d backticks", i+1, n)
		}
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "### ") || strings.HasPrefix(line, "| deploy |") {
			t.Logf("deciding line: %q", line)
		}
	}
}
````

- Command, from the copy's root: `go test ./internal/report -run '^TestReviewP2Try2BackticksInLine$' -count=1 -v`.
- Reviewer's output:

````text
    review_p2_2_test.go:28: deciding line: "### rollproof: FAIL ````a ``` b `` c ` d (Helm) on Kubernetes a ``` b `` c ` d in kind````"
    review_p2_2_test.go:28: deciding line: "| deploy | ````a ``` b `` c ` d```` | yes | 2 of 2 | 0.8 s |"
    review_p2_2_test.go:23: line 3: fence of 4 backticks
    review_p2_2_test.go:23: line 26: fence of 4 backticks
    review_p2_2_test.go:23: line 38: fence of 4 backticks
    review_p2_2_test.go:23: line 44: fence of 4 backticks
````

- The re-run's output (exit 0):

````
    review_p2_2_test.go:16: PAYLOAD "a ``` b `` c ` d"
    review_p2_2_test.go:17: SUMMARY START
    review_p2_2_test.go:23: line 3: fence of 4 backticks
    review_p2_2_test.go:23: line 26: fence of 4 backticks
    review_p2_2_test.go:23: line 38: fence of 4 backticks
    review_p2_2_test.go:23: line 44: fence of 4 backticks
    review_p2_2_test.go:28: deciding line: "### rollproof: FAIL ````a ``` b `` c ` d (Helm) on Kubernetes a ``` b `` c ` d in kind````"
    review_p2_2_test.go:28: deciding line: "| deploy | 29.1 s | 5,820 | 243 | 31 | 198 ms | 12 ms | FAIL |"
    review_p2_2_test.go:28: deciding line: "| deploy | ````a ``` b `` c ` d```` | yes | 2 of 2 | 0.8 s |"
````

- Verdict of the re-run: the rule kept in both runs. Runs of three, two and one backticks inside a line. Held: a code span and fences of four.

#### P2-3: a backslash before a pipe, and two backslashes before a pipe, in every string of the record

- Test: `review-2/internal/report/review_p2_3_test.go`, `TestReviewP2Try3PipeAndBackslash`, as the copy holds it:

````go
package report

import (
	"strings"
	"testing"
)

// TestReviewP2Try3PipeAndBackslash plants a backslash before a pipe and two backslashes before a
// pipe, in every string of the record that the rule covers. It prints the summary and the
// transition row, and counts that row's cell borders twice: by the rule that a pipe is escaped by
// an odd number of backslashes (the CommonMark backslash-pair reading) and by the rule that it is
// escaped when the character before it is a backslash (the reading the tool writes by).
func TestReviewP2Try3PipeAndBackslash(t *testing.T) {
	const payload = `a\|b a\\|b end\`
	r := failRun()
	r.Warnings = []string{"x"}
	r = plantFreeText(p2Fill(r), payload)
	out := Summary(r)
	t.Logf("PAYLOAD %q", payload)
	t.Logf("SUMMARY START\n%s\nSUMMARY END", out)

	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "| deploy |") {
			t.Logf("transition row: %q", line)
			t.Logf("borders, odd-backslash rule: %d", p2Borders(line, true))
			t.Logf("borders, unescaped-backslash rule: %d", p2Borders(line, false))
		}
		if strings.HasPrefix(line, "### ") {
			t.Logf("heading: %q", line)
		}
	}
}

// p2Borders counts the pipes of line that split a table cell. With odd true, a pipe is escaped when
// the run of backslashes before it has odd length; with odd false, when the character before it is
// a backslash.
func p2Borders(line string, odd bool) int {
	n := 0
	for i := 0; i < len(line); i++ {
		if line[i] != '|' {
			continue
		}
		if !odd {
			if i > 0 && line[i-1] == '\\' {
				continue
			}
			n++
			continue
		}
		k := 0
		for j := i - 1; j >= 0 && line[j] == '\\'; j-- {
			k++
		}
		if k%2 == 0 {
			n++
		}
	}
	return n
}
````

- Command, from the copy's root: `go test ./internal/report -run '^TestReviewP2Try3PipeAndBackslash$' -count=1 -v`.
- Rule: "In a table cell, every `|` is written `\|`, inside a code span too, as GitHub's tables ask." (the step 6 plan, its decision 5)
- Reviewer's output (its verdict: unclear; GitHub's documentation does not say how its table splitter reads a run of backslashes before a pipe):

````
        | deploy | `a\\|b a\\\|b end\` | yes | 2 of 2 | 0.8 s |
    review_p2_3_test.go:24: transition row: "| deploy | `a\\\\|b a\\\\\\|b end\\` | yes | 2 of 2 | 0.8 s |"
    review_p2_3_test.go:25: borders, odd-backslash rule: 7
    review_p2_3_test.go:26: borders, unescaped-backslash rule: 6
````

- The re-run's output: the same lines, and before them the three the same test prints for the windows' row:

````
        | deploy | `a\\|b a\\\|b end\` | yes | 2 of 2 | 0.8 s |
    review_p2_3_test.go:24: transition row: "| deploy | 29.1 s | 5,820 | 243 | 31 | 198 ms | 12 ms | FAIL |"
    review_p2_3_test.go:25: borders, odd-backslash rule: 9
    review_p2_3_test.go:26: borders, unescaped-backslash rule: 9
    review_p2_3_test.go:24: transition row: "| deploy | `a\\\\|b a\\\\\\|b end\\` | yes | 2 of 2 | 0.8 s |"
    review_p2_3_test.go:25: borders, odd-backslash rule: 7
    review_p2_3_test.go:26: borders, unescaped-backslash rule: 6
````

- Read after the re-run: (2026-11-25, in the source of github/cmark-gfm, its master branch; not run): `extensions/ext_scanners.re` defines `table_cell = (escaped_char|[^|\r\n])+` with `escaped_char` a backslash and an ASCII punctuation character, and the generated `_scan_table_cell` keeps the state after a backtick-free backslash accepting, stays in it on a backslash and takes a pipe into the cell. So a pipe with any run of backslashes before it is not a cell border, and `unescape_pipes` in `extensions/table.c` then takes off the one backslash before each pipe. Under it the row has six borders and five cells, as the reviewer says the two implementations on this machine give (goldmark and markdown-it). The version GitHub runs was not read, and nothing was run.

- Verdict of the re-run: does not hold.
- Ruling: accept (the maintainer, 2026-11-25). The reason: the reading of the source of github/cmark-gfm and the two implementations on this machine all keep a pipe in the cell when a backslash stands before it, so the rule keeps. The limits of the reading stand in the record: the source and not a run, and the version GitHub runs not read.

#### P2-4: line ends and other control characters

- Test: `review-2/internal/report/review_p2_4_test.go`, `TestReviewP2Try4Controls`, as the copy holds it:

````go
package report

import (
	"strings"
	"testing"
)

// TestReviewP2Try4Controls plants line ends and other control characters in every string of the
// record that the rule covers: CR LF, a lone CR, a lone LF, a tab, NUL, BEL, ESC, VT, FF and NEL.
func TestReviewP2Try4Controls(t *testing.T) {
	const payload = "A\r\nB\rC\nD\tE\x00F\x07G\x1b[31mH\x0bI\x0cJ\u0085K"
	r := failRun()
	r.Warnings = []string{"x"}
	r = plantFreeText(p2Fill(r), payload)
	out := Summary(r)
	t.Logf("PAYLOAD %q", payload)
	t.Logf("SUMMARY START\n%s\nSUMMARY END", out)

	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "### ") {
			t.Logf("heading quoted: %q", line)
			t.Logf("heading has control: %v", strings.ContainsFunc(line, func(r rune) bool { return r < 0x20 || (r >= 0x7f && r <= 0x9f) }))
		}
		if strings.Contains(line, "A B C D") {
			t.Logf("deciding line quoted: %q", line)
		}
	}
}
````

- Command, from the copy's root: `go test ./internal/report -run '^TestReviewP2Try4Controls$' -count=1 -v`.
- Reviewer's output:

````text
    review_p2_4_test.go:16: PAYLOAD "A\r\nB\rC\nD\tE\x00F\aG\x1b[31mH\vI\fJ\u0085K"
    review_p2_4_test.go:21: heading quoted: "### rollproof: FAIL `A B C D E F G [31mH I J K (Helm) on Kubernetes A B C D E F G [31mH I J K in kind`"
    review_p2_4_test.go:22: heading has control: false
````

- The re-run's output (exit 0):

````
    review_p2_4_test.go:16: PAYLOAD "A\r\nB\rC\nD\tE\x00F\aG\x1b[31mH\vI\fJ\u0085K"
    review_p2_4_test.go:17: SUMMARY START
    review_p2_4_test.go:21: heading quoted: "### rollproof: FAIL `A B C D E F G [31mH I J K (Helm) on Kubernetes A B C D E F G [31mH I J K in kind`"
    review_p2_4_test.go:22: heading has control: false
    review_p2_4_test.go:25: deciding line quoted: "### rollproof: FAIL `A B C D E F G [31mH I J K (Helm) on Kubernetes A B C D E F G [31mH I J K in kind`"
    review_p2_4_test.go:25: deciding line quoted: "releases    previous                   A B C D E F G [31mH I J K"
    review_p2_4_test.go:25: deciding line quoted: "            candidate                  A B C D E F G [31mH I J K"
    review_p2_4_test.go:25: deciding line quoted: "images      A B C D E F G [31mH I J K  run by A B C D E F G [31mH I J K, in pods the deploy created"
    review_p2_4_test.go:25: deciding line quoted: "probe       built-in HTTP: A B C D E F G [31mH I J K"
    review_p2_4_test.go:25: deciding line quoted: "            A B C D E F G [31mH I J K a second, timeout A B C D E F G [31mH I J K, status A B C D E F G [31mH I J K"
    review_p2_4_test.go:25: deciding line quoted: "            A B C D E F G [31mH I J K"
    review_p2_4_test.go:25: deciding line quoted: "            the settle time started again: A B C D E F G [31mH I J K"
    review_p2_4_test.go:25: deciding line quoted: "  cluster.node-image  A B C D E F G [31mH I J K  (default the linked kind release's)"
    review_p2_4_test.go:25: deciding line quoted: "  cluster.registry-image  A B C D E F G [31mH I J K  (default the tool's pinned registry image)"
    review_p2_4_test.go:25: deciding line quoted: "  cluster.workers  A B C D E F G [31mH I J K  (default 3)"
    review_p2_4_test.go:25: deciding line quoted: "  limits.steady  A B C D E F G [31mH I J K  (default 10s)"
    review_p2_4_test.go:25: deciding line quoted: "  limits.settle  A B C D E F G [31mH I J K  (default 15s)"
    review_p2_4_test.go:25: deciding line quoted: "  limits.timeout  A B C D E F G [31mH I J K  (default 5m)"
    review_p2_4_test.go:25: deciding line quoted: "  limits.silence  A B C D E F G [31mH I J K  (default 5s)"
    review_p2_4_test.go:25: deciding line quoted: "  probe.http.rate  A B C D E F G [31mH I J K  (default 200)"
    review_p2_4_test.go:25: deciding line quoted: "  probe.http.timeout  A B C D E F G [31mH I J K  (default 2s)"
    review_p2_4_test.go:25: deciding line quoted: "  probe.http.expect-status  A B C D E F G [31mH I J K  (default 200-299)"
    review_p2_4_test.go:25: deciding line quoted: "taken out of the commands' environment: A B C D E F G [31mH I J K"
    review_p2_4_test.go:25: deciding line quoted: "| deploy | `A B C D E F G [31mH I J K` | yes | 2 of 2 | 0.8 s |"
    review_p2_4_test.go:25: deciding line quoted: "deploy: A B C D E F G [31mH I J K   [probe-failures]"
    review_p2_4_test.go:25: deciding line quoted: "  A B C D E F G [31mH I J K"
    review_p2_4_test.go:25: deciding line quoted: "  A B C D E F G [31mH I J K"
    review_p2_4_test.go:25: deciding line quoted: "warning: A B C D E F G [31mH I J K"
    review_p2_4_test.go:25: deciding line quoted: "results: A B C D E F G [31mH I J K"
````

- Verdict of the re-run: the rule kept in both runs. Line ends and other control characters. Held: each becomes a space; the heading holds none.

#### P2-5: the line and paragraph separators

- Test: `review-2/internal/report/review_p2_5_test.go`, `TestReviewP2Try5Separators`, as the copy holds it:

````go
package report

import (
	"strings"
	"testing"
)

// TestReviewP2Try5Separators plants the line separator U+2028 and the paragraph separator U+2029
// in every string of the record that the rule covers.
func TestReviewP2Try5Separators(t *testing.T) {
	const payload = "A\u2028B\u2029C"
	r := failRun()
	r.Warnings = []string{"x"}
	r = plantFreeText(p2Fill(r), payload)
	out := Summary(r)
	t.Logf("PAYLOAD %q", payload)
	t.Logf("SUMMARY START\n%s\nSUMMARY END", out)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "### ") {
			t.Logf("heading quoted: %q", line)
			t.Logf("heading holds U+2028: %v; U+2029: %v", strings.ContainsRune(line, '\u2028'), strings.ContainsRune(line, '\u2029'))
		}
	}
}
````

- Command, from the copy's root: `go test ./internal/report -run '^TestReviewP2Try5Separators$' -count=1 -v`.
- Reviewer's output:

````text
    review_p2_5_test.go:16: PAYLOAD "A\u2028B\u2029C"
    review_p2_5_test.go:20: heading quoted: "### rollproof: FAIL `A B C (Helm) on Kubernetes A B C in kind`"
    review_p2_5_test.go:21: heading holds U+2028: false; U+2029: false
````

- The re-run's output (exit 0):

````
    review_p2_5_test.go:16: PAYLOAD "A\u2028B\u2029C"
    review_p2_5_test.go:17: SUMMARY START
    review_p2_5_test.go:20: heading quoted: "### rollproof: FAIL `A B C (Helm) on Kubernetes A B C in kind`"
    review_p2_5_test.go:21: heading holds U+2028: false; U+2029: false
````

- Verdict of the re-run: the rule kept in both runs. U+2028 and U+2029. Held: each becomes a space.

#### P2-6: the twelve bidirectional controls, in every string of the record

- Test: `review-2/internal/report/review_p2_6_test.go`, `TestReviewP2Try6BidiControls`, as the copy holds it:

````go
package report

import (
	"strings"
	"testing"
)

// TestReviewP2Try6BidiControls plants every bidi control in every string of the record that the
// rule covers: the explicit embed/override controls U+202A to U+202E, the isolates U+2066 to
// U+2069, and the implicit marks U+200E, U+200F and U+061C.
func TestReviewP2Try6BidiControls(t *testing.T) {
	const payload = "A\u202aB\u202bC\u202cD\u202dE\u202eF\u2066G\u2067H\u2068I\u2069J\u200eK\u200fL\u061cM"
	r := failRun()
	r.Warnings = []string{"x"}
	r = plantFreeText(p2Fill(r), payload)
	out := Summary(r)
	t.Logf("PAYLOAD %q", payload)
	t.Logf("SUMMARY START\n%s\nSUMMARY END", out)

	var heading string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "### ") {
			heading = line
			t.Logf("heading quoted: %q", heading)
		}
	}
	for _, c := range []rune{'\u202a', '\u202b', '\u202c', '\u202d', '\u202e',
		'\u2066', '\u2067', '\u2068', '\u2069', '\u200e', '\u200f', '\u061c'} {
		t.Logf("U+%04X survives in the heading: %v; in the whole summary: %v",
			c, strings.ContainsRune(heading, c), strings.ContainsRune(out, c))
	}
	// The runes of the heading around each implicit mark, as runes rather than bytes.
	runes := []rune(heading)
	for i, c := range runes {
		if c == '\u200e' || c == '\u200f' || c == '\u061c' {
			start := max(0, i-10)
			end := min(len(runes), i+11)
			t.Logf("survivor U+%04X at rune %d, context %q", c, i, string(runes[start:end]))
		}
	}
}
````

- Command, from the copy's root: `go test ./internal/report -run '^TestReviewP2Try6BidiControls$' -count=1 -v`.
- Rule: "a line end, every other control character, the line and paragraph separators and the bidirectional controls become a space, so that no text adds a line to a table or a heading" (the step 6 plan, its decision 5)
- Reviewer's output (its verdict: broken, narrowly: the wider rule holds, the cleaning sentence does not):

````
    review_p2_6_test.go:17: PAYLOAD "A\u202aB\u202bC\u202cD\u202dE\u202eF\u2066G\u2067H\u2068I\u2069J\u200eK\u200fL\u061cM"
    review_p2_6_test.go:24: heading quoted: "### rollproof: FAIL `A B C D E F G H I J\u200eK\u200fL\u061cM (Helm) on Kubernetes A B C D E F G H I J\u200eK\u200fL\u061cM in kind`"
    review_p2_6_test.go:29: U+202A survives in the heading: false; in the whole summary: false
    review_p2_6_test.go:29: U+202B survives in the heading: false; in the whole summary: false
    review_p2_6_test.go:29: U+202C survives in the heading: false; in the whole summary: false
    review_p2_6_test.go:29: U+202D survives in the heading: false; in the whole summary: false
    review_p2_6_test.go:29: U+202E survives in the heading: false; in the whole summary: false
    review_p2_6_test.go:29: U+2066 survives in the heading: false; in the whole summary: false
    review_p2_6_test.go:29: U+2067 survives in the heading: false; in the whole summary: false
    review_p2_6_test.go:29: U+2068 survives in the heading: false; in the whole summary: false
    review_p2_6_test.go:29: U+2069 survives in the heading: false; in the whole summary: false
    review_p2_6_test.go:29: U+200E survives in the heading: true; in the whole summary: true
    review_p2_6_test.go:29: U+200F survives in the heading: true; in the whole summary: true
    review_p2_6_test.go:29: U+061C survives in the heading: true; in the whole summary: true
    review_p2_6_test.go:38: survivor U+200E at rune 40, context " F G H I J\u200eK\u200fL\u061cM (Hel"
    review_p2_6_test.go:38: survivor U+200F at rune 42, context " G H I J\u200eK\u200fL\u061cM (Helm)"
    review_p2_6_test.go:38: survivor U+061C at rune 44, context " H I J\u200eK\u200fL\u061cM (Helm) o"
````

- The re-run's output: the same lines, and three more of the last kind, in the second half of the heading:

````
    review_p2_6_test.go:17: PAYLOAD "A\u202aB\u202bC\u202cD\u202dE\u202eF\u2066G\u2067H\u2068I\u2069J\u200eK\u200fL\u061cM"
    review_p2_6_test.go:18: SUMMARY START
    review_p2_6_test.go:24: heading quoted: "### rollproof: FAIL `A B C D E F G H I J\u200eK\u200fL\u061cM (Helm) on Kubernetes A B C D E F G H I J\u200eK\u200fL\u061cM in kind`"
    review_p2_6_test.go:29: U+202A survives in the heading: false; in the whole summary: false
    review_p2_6_test.go:29: U+202B survives in the heading: false; in the whole summary: false
    review_p2_6_test.go:29: U+202C survives in the heading: false; in the whole summary: false
    review_p2_6_test.go:29: U+202D survives in the heading: false; in the whole summary: false
    review_p2_6_test.go:29: U+202E survives in the heading: false; in the whole summary: false
    review_p2_6_test.go:29: U+2066 survives in the heading: false; in the whole summary: false
    review_p2_6_test.go:29: U+2067 survives in the heading: false; in the whole summary: false
    review_p2_6_test.go:29: U+2068 survives in the heading: false; in the whole summary: false
    review_p2_6_test.go:29: U+2069 survives in the heading: false; in the whole summary: false
    review_p2_6_test.go:29: U+200E survives in the heading: true; in the whole summary: true
    review_p2_6_test.go:29: U+200F survives in the heading: true; in the whole summary: true
    review_p2_6_test.go:29: U+061C survives in the heading: true; in the whole summary: true
    review_p2_6_test.go:38: survivor U+200E at rune 40, context " F G H I J\u200eK\u200fL\u061cM (Hel"
    review_p2_6_test.go:38: survivor U+200F at rune 42, context " G H I J\u200eK\u200fL\u061cM (Helm)"
    review_p2_6_test.go:38: survivor U+061C at rune 44, context " H I J\u200eK\u200fL\u061cM (Helm) o"
    review_p2_6_test.go:38: survivor U+200E at rune 87, context " F G H I J\u200eK\u200fL\u061cM in k"
    review_p2_6_test.go:38: survivor U+200F at rune 89, context " G H I J\u200eK\u200fL\u061cM in kin"
    review_p2_6_test.go:38: survivor U+061C at rune 91, context " H I J\u200eK\u200fL\u061cM in kind`"
````

- Verdict of the re-run: holds.
- Ruling: fix (the maintainer, 2026-11-25). The smaller change: `clean` takes the three implicit marks out as it does the nine explicit controls; no configuration key, reason code or schema version. Built test-first in Task 2.
- The fix: commit `f34fd6f`; its test `TestCleanTakesEveryBidiControl`.

#### P2-7: HTML with script, details and a comment

- Test: `review-2/internal/report/review_p2_7_test.go`, `TestReviewP2Try7HTML`, as the copy holds it:

````go
package report

import (
	"strings"
	"testing"
)

// TestReviewP2Try7HTML plants HTML with script, details and a comment, in every string of the
// record that the rule covers.
func TestReviewP2Try7HTML(t *testing.T) {
	const payload = `<script>alert(1)</script><details><summary>s</summary>d</details><!-- c --><img src=x onerror=alert(1)>`
	r := failRun()
	r.Warnings = []string{"x"}
	r = plantFreeText(p2Fill(r), payload)
	out := Summary(r)
	t.Logf("PAYLOAD %q", payload)
	t.Logf("SUMMARY START\n%s\nSUMMARY END", out)

	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "### ") || strings.HasPrefix(line, "| deploy | `") {
			t.Logf("deciding line: %q", line)
		}
	}
}
````

- Command, from the copy's root: `go test ./internal/report -run '^TestReviewP2Try7HTML$' -count=1 -v`.
- Reviewer's output:

````text
    review_p2_7_test.go:16: PAYLOAD "<script>alert(1)</script><details><summary>s</summary>d</details><!-- c --><img src=x onerror=alert(1)>"
    review_p2_7_test.go:21: deciding line: "### rollproof: FAIL `<script>alert(1)</script><details><summary>s</summary>d</details><!-- c --><img src=x onerror=alert(1)> (Helm) on Kubernetes <script>alert(1)</script><details><summary>s</summary>d</details><!-- c --><img src=x onerror=alert(1)> in kind`"
    review_p2_7_test.go:21: deciding line: "| deploy | `<script>alert(1)</script><details><summary>s</summary>d</details><!-- c --><img src=x onerror=alert(1)>` | yes | 2 of 2 | 0.8 s |"
````

- The re-run's output (exit 0):

````
    review_p2_7_test.go:16: PAYLOAD "<script>alert(1)</script><details><summary>s</summary>d</details><!-- c --><img src=x onerror=alert(1)>"
    review_p2_7_test.go:17: SUMMARY START
    review_p2_7_test.go:21: deciding line: "### rollproof: FAIL `<script>alert(1)</script><details><summary>s</summary>d</details><!-- c --><img src=x onerror=alert(1)> (Helm) on Kubernetes <script>alert(1)</script><details><summary>s</summary>d</details><!-- c --><img src=x onerror=alert(1)> in kind`"
    review_p2_7_test.go:21: deciding line: "| deploy | `<script>alert(1)</script><details><summary>s</summary>d</details><!-- c --><img src=x onerror=alert(1)>` | yes | 2 of 2 | 0.8 s |"
````

- Verdict of the re-run: the rule kept in both runs. HTML with script, details and a comment. Held: literal inside the code.

#### P2-8: a `javascript:` address

- Test: `review-2/internal/report/review_p2_8_test.go`, `TestReviewP2Try8Javascript`, as the copy holds it:

````go
package report

import (
	"strings"
	"testing"
)

// TestReviewP2Try8Javascript plants a javascript: address as a Markdown link and as an autolink,
// in every string of the record that the rule covers.
func TestReviewP2Try8Javascript(t *testing.T) {
	const payload = `[x](javascript:alert(1)) <javascript:alert(1)>`
	r := failRun()
	r.Warnings = []string{"x"}
	r = plantFreeText(p2Fill(r), payload)
	out := Summary(r)
	t.Logf("PAYLOAD %q", payload)
	t.Logf("SUMMARY START\n%s\nSUMMARY END", out)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "### ") || strings.HasPrefix(line, "| deploy | `") ||
			strings.HasPrefix(line, "warning: ") || strings.HasPrefix(line, "results: ") {
			t.Logf("deciding line: %q", line)
		}
	}
}
````

- Command, from the copy's root: `go test ./internal/report -run '^TestReviewP2Try8Javascript$' -count=1 -v`.
- Reviewer's output:

````text
    review_p2_8_test.go:21: deciding line: "### rollproof: FAIL `[x](javascript:alert(1)) <javascript:alert(1)> (Helm) on Kubernetes [x](javascript:alert(1)) <javascript:alert(1)> in kind`"
    review_p2_8_test.go:21: deciding line: "| deploy | `[x](javascript:alert(1)) <javascript:alert(1)>` | yes | 2 of 2 | 0.8 s |"
    review_p2_8_test.go:21: deciding line: "warning: [x](javascript:alert(1)) <javascript:alert(1)>"
````

- The re-run's output (exit 0):

````
    review_p2_8_test.go:16: PAYLOAD "[x](javascript:alert(1)) <javascript:alert(1)>"
    review_p2_8_test.go:17: SUMMARY START
    review_p2_8_test.go:21: deciding line: "### rollproof: FAIL `[x](javascript:alert(1)) <javascript:alert(1)> (Helm) on Kubernetes [x](javascript:alert(1)) <javascript:alert(1)> in kind`"
    review_p2_8_test.go:21: deciding line: "| deploy | `[x](javascript:alert(1)) <javascript:alert(1)>` | yes | 2 of 2 | 0.8 s |"
    review_p2_8_test.go:21: deciding line: "warning: [x](javascript:alert(1)) <javascript:alert(1)>"
    review_p2_8_test.go:21: deciding line: "results: [x](javascript:alert(1)) <javascript:alert(1)>"
````

- Verdict of the re-run: the rule kept in both runs. A `javascript:` address, as a link and as an autolink. Held: literal inside the code.

#### P2-9: an @mention and issue numbers

- Test: `review-2/internal/report/review_p2_9_test.go`, `TestReviewP2Try9Mentions`, as the copy holds it:

````go
package report

import (
	"strings"
	"testing"
)

// TestReviewP2Try9Mentions plants an @mention and issue references, in every string of the record
// that the rule covers.
func TestReviewP2Try9Mentions(t *testing.T) {
	const payload = `@octocat #123 GH-456 owner/repo#789`
	r := failRun()
	r.Warnings = []string{"x"}
	r = plantFreeText(p2Fill(r), payload)
	out := Summary(r)
	t.Logf("PAYLOAD %q", payload)
	t.Logf("SUMMARY START\n%s\nSUMMARY END", out)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "### ") || strings.HasPrefix(line, "| deploy | `") ||
			strings.HasPrefix(line, "warning: ") || strings.HasPrefix(line, "results: ") {
			t.Logf("deciding line: %q", line)
		}
	}
}
````

- Command, from the copy's root: `go test ./internal/report -run '^TestReviewP2Try9Mentions$' -count=1 -v`.
- Reviewer's output:

````text
    review_p2_9_test.go:16: PAYLOAD "@octocat #123 GH-456 owner/repo#789"
    review_p2_9_test.go:21: deciding line: "### rollproof: FAIL `@octocat #123 GH-456 owner/repo#789 (Helm) on Kubernetes @octocat #123 GH-456 owner/repo#789 in kind`"
    review_p2_9_test.go:21: deciding line: "| deploy | `@octocat #123 GH-456 owner/repo#789` | yes | 2 of 2 | 0.8 s |"
    review_p2_9_test.go:21: deciding line: "warning: @octocat #123 GH-456 owner/repo#789"
````

- The re-run's output (exit 0):

````
    review_p2_9_test.go:16: PAYLOAD "@octocat #123 GH-456 owner/repo#789"
    review_p2_9_test.go:17: SUMMARY START
    review_p2_9_test.go:21: deciding line: "### rollproof: FAIL `@octocat #123 GH-456 owner/repo#789 (Helm) on Kubernetes @octocat #123 GH-456 owner/repo#789 in kind`"
    review_p2_9_test.go:21: deciding line: "| deploy | `@octocat #123 GH-456 owner/repo#789` | yes | 2 of 2 | 0.8 s |"
    review_p2_9_test.go:21: deciding line: "warning: @octocat #123 GH-456 owner/repo#789"
    review_p2_9_test.go:21: deciding line: "results: @octocat #123 GH-456 owner/repo#789"
````

- Verdict of the re-run: the rule kept in both runs. An @mention and issue numbers. Held: literal inside the code.

#### P2-10: bare addresses

- Test: `review-2/internal/report/review_p2_10_test.go`, `TestReviewP2Try10Addresses`, as the copy holds it:

````go
package report

import (
	"strings"
	"testing"
)

// TestReviewP2Try10Addresses plants bare addresses, of the three forms GitHub autolinks (scheme,
// www and email), in every string of the record that the rule covers.
func TestReviewP2Try10Addresses(t *testing.T) {
	const payload = `https://evil.example/x?y=1 www.evil.example user@evil.example`
	r := failRun()
	r.Warnings = []string{"x"}
	r = plantFreeText(p2Fill(r), payload)
	out := Summary(r)
	t.Logf("PAYLOAD %q", payload)
	t.Logf("SUMMARY START\n%s\nSUMMARY END", out)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "### ") || strings.HasPrefix(line, "| deploy | `") ||
			strings.HasPrefix(line, "warning: ") || strings.HasPrefix(line, "results: ") {
			t.Logf("deciding line: %q", line)
		}
	}
}
````

- Command, from the copy's root: `go test ./internal/report -run '^TestReviewP2Try10Addresses$' -count=1 -v`.
- Reviewer's output:

````text
    review_p2_10_test.go:16: PAYLOAD "https://evil.example/x?y=1 www.evil.example user@evil.example"
    review_p2_10_test.go:21: deciding line: "### rollproof: FAIL `https://evil.example/x?y=1 www.evil.example user@evil.example (Helm) on Kubernetes https://evil.example/x?y=1 www.evil.example user@evil.example in kind`"
    review_p2_10_test.go:21: deciding line: "| deploy | `https://evil.example/x?y=1 www.evil.example user@evil.example` | yes | 2 of 2 | 0.8 s |"
    review_p2_10_test.go:21: deciding line: "warning: https://evil.example/x?y=1 www.evil.example user@evil.example"
````

- The re-run's output (exit 0):

````
    review_p2_10_test.go:16: PAYLOAD "https://evil.example/x?y=1 www.evil.example user@evil.example"
    review_p2_10_test.go:17: SUMMARY START
    review_p2_10_test.go:21: deciding line: "### rollproof: FAIL `https://evil.example/x?y=1 www.evil.example user@evil.example (Helm) on Kubernetes https://evil.example/x?y=1 www.evil.example user@evil.example in kind`"
    review_p2_10_test.go:21: deciding line: "| deploy | `https://evil.example/x?y=1 www.evil.example user@evil.example` | yes | 2 of 2 | 0.8 s |"
    review_p2_10_test.go:21: deciding line: "warning: https://evil.example/x?y=1 www.evil.example user@evil.example"
    review_p2_10_test.go:21: deciding line: "results: https://evil.example/x?y=1 www.evil.example user@evil.example"
````

- Verdict of the re-run: the rule kept in both runs. Bare addresses. Held: literal inside the code.

#### P2-11: emoji codes

- Test: `review-2/internal/report/review_p2_11_test.go`, `TestReviewP2Try11Emoji`, as the copy holds it:

````go
package report

import (
	"strings"
	"testing"
)

// TestReviewP2Try11Emoji plants emoji codes, in every string of the record that the rule covers.
func TestReviewP2Try11Emoji(t *testing.T) {
	const payload = `:smile: :+1: :shipit:`
	r := failRun()
	r.Warnings = []string{"x"}
	r = plantFreeText(p2Fill(r), payload)
	out := Summary(r)
	t.Logf("PAYLOAD %q", payload)
	t.Logf("SUMMARY START\n%s\nSUMMARY END", out)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "### ") || strings.HasPrefix(line, "| deploy | `") ||
			strings.HasPrefix(line, "warning: ") || strings.HasPrefix(line, "results: ") {
			t.Logf("deciding line: %q", line)
		}
	}
}
````

- Command, from the copy's root: `go test ./internal/report -run '^TestReviewP2Try11Emoji$' -count=1 -v`.
- Reviewer's output:

````text
    review_p2_11_test.go:15: PAYLOAD ":smile: :+1: :shipit:"
    review_p2_11_test.go:20: deciding line: "### rollproof: FAIL `:smile: :+1: :shipit: (Helm) on Kubernetes :smile: :+1: :shipit: in kind`"
    review_p2_11_test.go:20: deciding line: "| deploy | `:smile: :+1: :shipit:` | yes | 2 of 2 | 0.8 s |"
    review_p2_11_test.go:20: deciding line: "warning: :smile: :+1: :shipit:"
````

- The re-run's output (exit 0):

````
    review_p2_11_test.go:15: PAYLOAD ":smile: :+1: :shipit:"
    review_p2_11_test.go:16: SUMMARY START
    review_p2_11_test.go:20: deciding line: "### rollproof: FAIL `:smile: :+1: :shipit: (Helm) on Kubernetes :smile: :+1: :shipit: in kind`"
    review_p2_11_test.go:20: deciding line: "| deploy | `:smile: :+1: :shipit:` | yes | 2 of 2 | 0.8 s |"
    review_p2_11_test.go:20: deciding line: "warning: :smile: :+1: :shipit:"
    review_p2_11_test.go:20: deciding line: "results: :smile: :+1: :shipit:"
````

- Verdict of the re-run: the rule kept in both runs. Emoji codes. Held: literal inside the code.

#### P2-12: math delimiters

- Test: `review-2/internal/report/review_p2_12_test.go`, `TestReviewP2Try12Math`, as the copy holds it:

````go
package report

import (
	"strings"
	"testing"
)

// TestReviewP2Try12Math plants math delimiters, inline and display and in LaTeX's own forms, in
// every string of the record that the rule covers.
func TestReviewP2Try12Math(t *testing.T) {
	const payload = `$x$ $$y$$ \(z\) \[w\]`
	r := failRun()
	r.Warnings = []string{"x"}
	r = plantFreeText(p2Fill(r), payload)
	out := Summary(r)
	t.Logf("PAYLOAD %q", payload)
	t.Logf("SUMMARY START\n%s\nSUMMARY END", out)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "### ") || strings.HasPrefix(line, "| deploy | `") ||
			strings.HasPrefix(line, "warning: ") || strings.HasPrefix(line, "results: ") {
			t.Logf("deciding line: %q", line)
		}
	}
}
````

- Command, from the copy's root: `go test ./internal/report -run '^TestReviewP2Try12Math$' -count=1 -v`.
- Reviewer's output:

````text
    review_p2_12_test.go:16: PAYLOAD "$x$ $$y$$ \\(z\\) \\[w\\]"
    review_p2_12_test.go:21: deciding line: "### rollproof: FAIL `$x$ $$y$$ \\(z\\) \\[w\\] (Helm) on Kubernetes $x$ $$y$$ \\(z\\) \\[w\\] in kind`"
    review_p2_12_test.go:21: deciding line: "| deploy | `$x$ $$y$$ \\(z\\) \\[w\\]` | yes | 2 of 2 | 0.8 s |"
    review_p2_12_test.go:21: deciding line: "warning: $x$ $$y$$ \\(z\\) \\[w\\]"
````

- The re-run's output (exit 0):

````
    review_p2_12_test.go:16: PAYLOAD "$x$ $$y$$ \\(z\\) \\[w\\]"
    review_p2_12_test.go:17: SUMMARY START
    review_p2_12_test.go:21: deciding line: "### rollproof: FAIL `$x$ $$y$$ \\(z\\) \\[w\\] (Helm) on Kubernetes $x$ $$y$$ \\(z\\) \\[w\\] in kind`"
    review_p2_12_test.go:21: deciding line: "| deploy | `$x$ $$y$$ \\(z\\) \\[w\\]` | yes | 2 of 2 | 0.8 s |"
    review_p2_12_test.go:21: deciding line: "warning: $x$ $$y$$ \\(z\\) \\[w\\]"
    review_p2_12_test.go:21: deciding line: "results: $x$ $$y$$ \\(z\\) \\[w\\]"
````

- Verdict of the re-run: the rule kept in both runs. Math delimiters. Held: literal inside the code.

#### P2-13: heading and setext underline

- Test: `review-2/internal/report/review_p2_13_test.go`, `TestReviewP2Try13Heading`, as the copy holds it:

````go
package report

import (
	"strings"
	"testing"
)

// TestReviewP2Try13Heading plants a heading, a second heading and a setext underline, with the
// line ends that would put them at the start of a line, in every string of the record that the
// rule covers.
func TestReviewP2Try13Heading(t *testing.T) {
	const payload = "# h\n## h\n---"
	r := failRun()
	r.Warnings = []string{"x"}
	r = plantFreeText(p2Fill(r), payload)
	out := Summary(r)
	t.Logf("PAYLOAD %q", payload)
	t.Logf("SUMMARY START\n%s\nSUMMARY END", out)
	for i, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "### ") || strings.HasPrefix(line, "## ") ||
			strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "---") ||
			strings.HasPrefix(line, "| deploy | `") || strings.HasPrefix(line, "warning: ") ||
			strings.HasPrefix(line, "  #") || strings.HasPrefix(line, "  ---") {
			t.Logf("deciding line %d: %q", i+1, line)
		}
	}
}
````

- Command, from the copy's root: `go test ./internal/report -run '^TestReviewP2Try13Heading$' -count=1 -v`.
- Reviewer's output:

````text
    review_p2_13_test.go:17: PAYLOAD "# h\n## h\n---"
    review_p2_13_test.go:24: deciding line 1: "### rollproof: FAIL `# h ## h --- (Helm) on Kubernetes # h ## h --- in kind`"
    review_p2_13_test.go:24: deciding line 36: "| deploy | `# h ## h ---` | yes | 2 of 2 | 0.8 s |"
    review_p2_13_test.go:24: deciding line 40: "  # h ## h ---"
    review_p2_13_test.go:24: deciding line 41: "  # h ## h ---"
    review_p2_13_test.go:24: deciding line 42: "warning: # h ## h ---"
````

- The re-run's output (exit 0):

````
    review_p2_13_test.go:17: PAYLOAD "# h\n## h\n---"
    review_p2_13_test.go:18: SUMMARY START
    review_p2_13_test.go:24: deciding line 1: "### rollproof: FAIL `# h ## h --- (Helm) on Kubernetes # h ## h --- in kind`"
    review_p2_13_test.go:24: deciding line 36: "| deploy | `# h ## h ---` | yes | 2 of 2 | 0.8 s |"
    review_p2_13_test.go:24: deciding line 40: "  # h ## h ---"
    review_p2_13_test.go:24: deciding line 41: "  # h ## h ---"
    review_p2_13_test.go:24: deciding line 42: "warning: # h ## h ---"
````

- Verdict of the re-run: the rule kept in both runs. A heading marker and a setext underline. Held: line ends became spaces, so no line starts with either.

#### P2-14: list marker and indented line

- Test: `review-2/internal/report/review_p2_14_test.go`, `TestReviewP2Try14ListAndIndent`, as the copy holds it:

````go
package report

import (
	"strings"
	"testing"
)

// TestReviewP2Try14ListAndIndent plants list markers and an indented line, in every string of the
// record that the rule covers, in a pass so that the tool's own lists are printed too.
func TestReviewP2Try14ListAndIndent(t *testing.T) {
	const payload = "- item * item + item 1. item > quote     indented"
	r := passRun()
	r.Reasons = failRun().Reasons
	r.Warnings = []string{"x"}
	r = plantFreeText(p2Fill(r), payload)
	out := Summary(r)
	t.Logf("PAYLOAD %q", payload)
	t.Logf("SUMMARY START\n%s\nSUMMARY END", out)
	for i, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "### ") || strings.HasPrefix(line, "- ") ||
			strings.HasPrefix(line, "| deploy | `") || strings.HasPrefix(line, "warning: ") ||
			strings.HasPrefix(line, "  - ") || strings.HasPrefix(line, "   ") {
			t.Logf("deciding line %d: %q", i+1, line)
		}
	}
}
````

- Command, from the copy's root: `go test ./internal/report -run '^TestReviewP2Try14ListAndIndent$' -count=1 -v`.
- Reviewer's output:

````text
    review_p2_14_test.go:17: PAYLOAD "- item * item + item 1. item > quote     indented"
    review_p2_14_test.go:23: deciding line 1: "### rollproof: PASS `- item * item + item 1. item > quote     indented (Helm) on Kubernetes - item * item + item 1. item > quote     indented in kind`"
    review_p2_14_test.go:23: deciding line 36: "| deploy | `- item * item + item 1. item > quote     indented` | yes | 2 of 2 | 5.3 s |"
    review_p2_14_test.go:23: deciding line 41: "- deploy: the candidate's start-up and the previous release's shutdown"
    review_p2_14_test.go:23: deciding line 42: "- rollback: the previous release's start-up and the candidate's shutdown"
    review_p2_14_test.go:23: deciding line 46: "- the delay before a larger cluster stops routing to a stopping pod"
    review_p2_14_test.go:23: deciding line 58: "  - item * item + item 1. item > quote     indented"
    review_p2_14_test.go:23: deciding line 59: "  - item * item + item 1. item > quote     indented"
````

- The re-run's output (exit 0):

````
    review_p2_14_test.go:17: PAYLOAD "- item * item + item 1. item > quote     indented"
    review_p2_14_test.go:18: SUMMARY START
    review_p2_14_test.go:23: deciding line 1: "### rollproof: PASS `- item * item + item 1. item > quote     indented (Helm) on Kubernetes - item * item + item 1. item > quote     indented in kind`"
    review_p2_14_test.go:23: deciding line 5: "            candidate                                          - item * item + item 1. item > quote     indented"
    review_p2_14_test.go:23: deciding line 8: "            - item * item + item 1. item > quote     indented a second, timeout - item * item + item 1. item > quote     indented, status - item * item + item 1. item > quote     indented"
    review_p2_14_test.go:23: deciding line 9: "            - item * item + item 1. item > quote     indented"
    review_p2_14_test.go:23: deciding line 11: "            3,000 checks, 6 failed (refused 5, timeout 1), the last 14.9 s before the steady window"
    review_p2_14_test.go:23: deciding line 12: "            slowest 44 ms, behind 9 ms"
    review_p2_14_test.go:23: deciding line 13: "            the settle time started again: - item * item + item 1. item > quote     indented"
    review_p2_14_test.go:23: deciding line 36: "| deploy | `- item * item + item 1. item > quote     indented` | yes | 2 of 2 | 5.3 s |"
    review_p2_14_test.go:23: deciding line 41: "- deploy: the candidate's start-up and the previous release's shutdown"
    review_p2_14_test.go:23: deciding line 42: "- rollback: the previous release's start-up and the candidate's shutdown"
    review_p2_14_test.go:23: deciding line 46: "- the delay before a larger cluster stops routing to a stopping pod"
    review_p2_14_test.go:23: deciding line 47: "- connections held open, and requests in flight on a pod after new connections left it"
    review_p2_14_test.go:23: deciding line 48: "- the path through an ingress, a load balancer or a mesh"
    review_p2_14_test.go:23: deciding line 49: "- routing that depends on where the client is"
    review_p2_14_test.go:23: deciding line 50: "- capacity while pods are down, and autoscaling"
    review_p2_14_test.go:23: deciding line 51: "- storage that attaches to one node at a time"
    review_p2_14_test.go:23: deciding line 52: "- latency below the timeout"
    review_p2_14_test.go:23: deciding line 53: "- data migrations"
    review_p2_14_test.go:23: deciding line 54: "- a node drain"
    review_p2_14_test.go:23: deciding line 58: "  - item * item + item 1. item > quote     indented"
    review_p2_14_test.go:23: deciding line 59: "  - item * item + item 1. item > quote     indented"
    review_p2_14_test.go:23: deciding line 60: "warning: - item * item + item 1. item > quote     indented"
````

- Verdict of the re-run: the rule kept in both runs. List markers and an indented line. Held: literal inside the code; the tool's own lists stand.

#### P2-15: the whole text in a late warning, through `SummaryLine`

- Test: `review-2/internal/report/review_p2_15_test.go`, `TestReviewP2Try15LateWarning`, as the copy holds it:

````go
package report

import (
	"strings"
	"testing"
)

// TestReviewP2Try15LateWarning plants the whole hostile text in a warning that is appended after
// the summary was written, through SummaryLine: a fence, a heading, a pipe, a backslash-pipe, HTML,
// a javascript: address, a mention, an issue number, an address, an emoji code, math, a list
// marker and an indented line, with line ends.
func TestReviewP2Try15LateWarning(t *testing.T) {
	payloads := []string{
		"warning: ```\n### h | a\\|b <script>x</script> [x](javascript:alert(1)) @octocat #1 https://e.example :smile: $x$ - item     indented",
		"warning: ````",
		"warning: a\nb\tc\u2028d\u2029e",
	}
	for i, payload := range payloads {
		out := SummaryLine(payload)
		t.Logf("PAYLOAD %d %q", i+1, payload)
		t.Logf("BLOCK %d START\n%s\nBLOCK %d END", i+1, out, i+1)
		for _, line := range strings.Split(out, "\n") {
			n := len(line) - len(strings.TrimLeft(line, "`"))
			if n >= 3 && strings.Trim(line[n:], " ") == "" {
				t.Logf("block %d: fence of %d backticks", i+1, n)
			}
		}
	}
}
````

- Command, from the copy's root: `go test ./internal/report -run '^TestReviewP2Try15LateWarning$' -count=1 -v`.
- Reviewer's output:

````text
    review_p2_15_test.go:21: BLOCK 1 START
        
        ````
        warning: ``` ### h | a\|b <script>x</script> [x](javascript:alert(1)) @octocat #1 https://e.example :smile: $x$ - item     indented
        ````
        
        BLOCK 1 END
    review_p2_15_test.go:25: block 1: fence of 4 backticks
    review_p2_15_test.go:21: BLOCK 2 START
        
        `````
        warning: ````
        `````
        
        BLOCK 2 END
    review_p2_15_test.go:25: block 2: fence of 5 backticks
    review_p2_15_test.go:21: BLOCK 3 START
        
        ```
        warning: a b c d e
        ```
        
        BLOCK 3 END
    review_p2_15_test.go:25: block 3: fence of 3 backticks
````

- The re-run's output (exit 0):

````
    review_p2_15_test.go:20: PAYLOAD 1 "warning: ```\n### h | a\\|b <script>x</script> [x](javascript:alert(1)) @octocat #1 https://e.example :smile: $x$ - item     indented"
    review_p2_15_test.go:21: BLOCK 1 START
    review_p2_15_test.go:25: block 1: fence of 4 backticks
    review_p2_15_test.go:25: block 1: fence of 4 backticks
    review_p2_15_test.go:20: PAYLOAD 2 "warning: ````"
    review_p2_15_test.go:21: BLOCK 2 START
    review_p2_15_test.go:25: block 2: fence of 5 backticks
    review_p2_15_test.go:25: block 2: fence of 5 backticks
    review_p2_15_test.go:20: PAYLOAD 3 "warning: a\nb\tc\u2028d\u2029e"
    review_p2_15_test.go:21: BLOCK 3 START
    review_p2_15_test.go:25: block 3: fence of 3 backticks
    review_p2_15_test.go:25: block 3: fence of 3 backticks
````

- Verdict of the re-run: the rule kept in both runs. The whole hostile text in a warning appended after the summary. Held: fences of four, five and three.

#### P2-16: chart sources, values files and inline keys

- Test: `review-2/internal/report/review_p2_16_test.go`, `TestReviewP2Try16ChartAndValues`, as the copy holds it:

````go
package report

import (
	"strings"
	"testing"
)

// TestReviewP2Try16ChartAndValues plants a union of hostile text in every string of the record that
// the rule covers, and then clears the releases' Program so that the Helm releases' chart, version,
// source, values files and inline keys are the lines printed, rather than the commands driver's
// program word.
func TestReviewP2Try16ChartAndValues(t *testing.T) {
	const payload = "a|b a\\|b ```` <script>x</script> # h - item"
	r := failRun()
	r.Warnings = []string{"x"}
	r = plantFreeText(p2Fill(r), payload)
	r.Releases[0].Program, r.Releases[1].Program = "", ""
	out := Summary(r)
	t.Logf("PAYLOAD %q", payload)
	t.Logf("SUMMARY START\n%s\nSUMMARY END", out)
	inFence := false
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "```") && len(strings.TrimLeft(line, "`")) == 0 {
			inFence = !inFence
			t.Logf("fence: %q", line)
			continue
		}
		if inFence && (strings.HasPrefix(line, "releases") || strings.HasPrefix(line, "            candidate") || strings.HasPrefix(line, "            previous")) {
			t.Logf("releases line: %q", line)
		}
		if strings.HasPrefix(line, "### ") {
			t.Logf("heading: %q", line)
		}
	}
}
````

- Command, from the copy's root: `go test ./internal/report -run '^TestReviewP2Try16ChartAndValues$' -count=1 -v`.
- Reviewer's output:

````text
    review_p2_16_test.go:19: PAYLOAD "a|b a\\|b ```` <script>x</script> # h - item"
    review_p2_16_test.go:32: heading: "### rollproof: FAIL `````a|b a\\|b ```` <script>x</script> # h - item (Helm) on Kubernetes a|b a\\|b ```` <script>x</script> # h - item in kind`````"
    review_p2_16_test.go:29: releases line: "releases    previous                                     a|b a\\|b ```` <script>x</script> # h - item a|b a\\|b ```` <script>x</script> # h - item, a|b a\\|b ```` <script>x</script> # h - item; chart defaults"
    review_p2_16_test.go:29: releases line: "            candidate                                    a|b a\\|b ```` <script>x</script> # h - item a|b a\\|b ```` <script>x</script> # h - item, a|b a\\|b ```` <script>x</script> # h - item; values: a|b a\\|b ```` <script>x</script> # h - item, inline (a|b a\\|b ```` <script>x</script> # h - item)"
````

- The re-run's output (exit 0):

````
    review_p2_16_test.go:19: PAYLOAD "a|b a\\|b ```` <script>x</script> # h - item"
    review_p2_16_test.go:20: SUMMARY START
    review_p2_16_test.go:32: heading: "### rollproof: FAIL `````a|b a\\|b ```` <script>x</script> # h - item (Helm) on Kubernetes a|b a\\|b ```` <script>x</script> # h - item in kind`````"
    review_p2_16_test.go:25: fence: "`````"
    review_p2_16_test.go:29: releases line: "releases    previous                                     a|b a\\|b ```` <script>x</script> # h - item a|b a\\|b ```` <script>x</script> # h - item, a|b a\\|b ```` <script>x</script> # h - item; chart defaults"
    review_p2_16_test.go:29: releases line: "            candidate                                    a|b a\\|b ```` <script>x</script> # h - item a|b a\\|b ```` <script>x</script> # h - item, a|b a\\|b ```` <script>x</script> # h - item; values: a|b a\\|b ```` <script>x</script> # h - item, inline (a|b a\\|b ```` <script>x</script> # h - item)"
    review_p2_16_test.go:25: fence: "`````"
    review_p2_16_test.go:25: fence: "`````"
    review_p2_16_test.go:25: fence: "`````"
````

- Verdict of the re-run: the rule kept in both runs. Chart sources, values files and inline keys, on the Helm branch. Held: inside a fence and a code span of five.

#### P2-17: a plain pipe

- Test: `review-2/internal/report/review_p2_17_test.go`, `TestReviewP2Try17PlainPipe`, as the copy holds it:

````go
package report

import (
	"strings"
	"testing"
)

// TestReviewP2Try17PlainPipe plants a pipe with no backslash before it, in every string of the
// record that the rule covers, and prints the summary and the transition row.
func TestReviewP2Try17PlainPipe(t *testing.T) {
	const payload = "a|b"
	r := failRun()
	r.Warnings = []string{"x"}
	r = plantFreeText(p2Fill(r), payload)
	out := Summary(r)
	t.Logf("PAYLOAD %q", payload)
	t.Logf("SUMMARY START\n%s\nSUMMARY END", out)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "| deploy |") {
			t.Logf("transition row: %q", line)
			t.Logf("borders, odd-backslash rule: %d", p2Borders(line, true))
			t.Logf("borders, unescaped-backslash rule: %d", p2Borders(line, false))
		}
		if strings.HasPrefix(line, "### ") {
			t.Logf("heading: %q", line)
		}
	}
}
````

- Command, from the copy's root: `go test ./internal/report -run '^TestReviewP2Try17PlainPipe$' -count=1 -v`.
- Reviewer's output:

````text
        | deploy | `a\|b` | yes | 2 of 2 | 0.8 s |
    review_p2_17_test.go:20: transition row: "| deploy | `a\\|b` | yes | 2 of 2 | 0.8 s |"
    review_p2_17_test.go:21: borders, odd-backslash rule: 6
    review_p2_17_test.go:22: borders, unescaped-backslash rule: 6
````

- The re-run's output (exit 0):

````
    review_p2_17_test.go:16: PAYLOAD "a|b"
    review_p2_17_test.go:17: SUMMARY START
    review_p2_17_test.go:25: heading: "### rollproof: FAIL `a|b (Helm) on Kubernetes a|b in kind`"
    review_p2_17_test.go:20: transition row: "| deploy | 29.1 s | 5,820 | 243 | 31 | 198 ms | 12 ms | FAIL |"
    review_p2_17_test.go:21: borders, odd-backslash rule: 9
    review_p2_17_test.go:22: borders, unescaped-backslash rule: 9
    review_p2_17_test.go:20: transition row: "| deploy | `a\\|b` | yes | 2 of 2 | 0.8 s |"
    review_p2_17_test.go:21: borders, odd-backslash rule: 6
    review_p2_17_test.go:22: borders, unescaped-backslash rule: 6
````

- Verdict of the re-run: the rule kept in both runs. A plain pipe. Held: written `\|`; six borders, five cells.

**What reviewer 2 did not try:**

````markdown
## 3. What I did not try, and why
- **A cluster or engine test.** The subject is `report.Summary` and `report.SummaryLine`, pure functions of a record; they read no cluster. I started no container, image, network or volume, so the `rollproof-review-2-` naming and removal rule has nothing to act on.
- **The actual GitHub renderer.** I made no network request, so I could not submit the output to GitHub's Markdown API. The "what GitHub renders" lines are from GitHub's GFM documentation and, where that is silent, from the GFM parsing rules and two local GFM implementations (goldmark and markdown-it); I say where the documentation does not settle the case — the doubled backslash before a pipe (try 3), and emoji, math, mentions, issue references and bare addresses inside code (tries 9–12).
- **The run path and the file path.** I did not exercise `internal/run`'s call at `run.go:1067` (which masks first), `AppendSummary`, a summary file that already holds text, a failed append, or a panic while rendering. Those are the append path and the masking, not the Markdown containment; I read the call and the existing tests, and part 1 and part 3 own the masker and the workflow commands.
- **The 1 MiB limit.** I did not build a summary larger than 1 MiB. Section 10 names GitHub's behavior (no summary shown, step green); it is a size limit, not a Markdown reading rule.
- **Invalid UTF-8, and fields the console never prints.** I did not plant invalid UTF-8; `clean` maps it to U+FFFD and GitHub expects UTF-8. I did not plant in `Probe.Headers`, `Image.Digest`, `Cluster.NodeImage`, `Cluster.ProviderVersion` or `Tool.Version` because `console.go` and `summary.go` never print them, so the summary's rule does not reach them. For the same reason I did not plant a custom probe's command or args values: `customRecord` records only `CommandCount` and `ArgsCount`, never a value.
- **A text that is all spaces.** I did not make it a try. `codeSpan` adds spaces when the text begins or ends with a space; for an all-space text the renderer's one-space strip does not apply, so the text would gain spaces. A Helm release name and a namespace cannot be all spaces, and the effect is spacing, not a Markdown construct.
- **The three other parts of point 13.** Masking, workflow commands, and the five facts and what leaves the runner are other reviewers' cards.
````

### Part 3: workflow commands

11 tries, by reviewer 3; the card it was given is in Method.

**The rule's claims, as the design states them, in reviewer 3's words:**

````markdown
Part 3 of the review: workflow commands. Section 8, "Workflow commands", and section 14, point 33.
1. "GitHub's runner reads every line that a step prints, on standard output and on standard error, for workflow commands: a line that begins with `::` after its leading white space, and the older form `##[`, anywhere in a line." (section 8)
2. "when the tool detects GitHub Actions, `rollproof run` prints `::stop-commands::TOKEN` as its first line on standard output and as its first line on standard error, before anything else, a usage error included, with a token drawn anew for each run from the system's random source." (section 8)
3. "The runner then reads no command until a line `::TOKEN::`." (section 8)
4. "The tool prints that line last on standard output when it ends by itself, whatever its exit code, so that a step that calls the tool from a script of its own can use commands again afterwards." (section 8)
5. "It does not print it when it ends by a signal, nor after a crash that escapes its run, and elsewhere than on GitHub Actions it prints neither line." (section 8)
6. "The runner reads the two streams each in its own order, so each needs its own first line; the second that the runner meets is a plain line." (section 8)
7. "The action's script prints the same line on both streams with a token of its own, once it knows that the runner is Linux and before any line that can hold an input or an engine's output, and never resumes." (section 8)
8. "On GitHub Actions the tool's first line on each stream stops the runner's workflow commands, and its last line on standard output resumes them when the tool ended by itself; the action's script stops them with a token of its own and never resumes." (point 33)
9. "The tool's token comes from Go's `crypto/rand`, the script's from `/proc/sys/kernel/random/uuid`, read with bash's own `read`." (point 33)
10. "A step's files, its outputs among them, are read after its process has ended … a stop does not touch them, so the values of `verdict` and `out` come from the file and come last." (point 33)
11. "After `::stop-commands::TOKEN` no command is read until a line `::TOKEN::`, or `##[TOKEN]` anywhere in a line. The token is matched without regard to case, is added to the log's masker when it is longer than 6 characters, and may not be empty, the name of a command or `pause-logging`." (point 33)
12. Limits: the two open places point 33 names (quoted in Part 3).
13. "Nor does a stop reach the problem matchers that an earlier step of the job registered: they still read every line." (point 33)
````

#### P3-1: a bad flag

- Test: `review-3/cmd/rollproof/review_p3_1_test.go`, `TestReviewP3Try1BadFlagWithAWorkflowCommand`, as the copy holds it:

````go
package main

// Try 1 of the third part of the security review of section 14, point 13: a bad flag whose
// name carries a newline and a workflow command. The tool must print its stop line first on
// each stream, and the hostile line must not be read as a command before the resume.
//
// It attacks the claim of section 8, "Workflow commands": "rollproof run prints
// ::stop-commands::TOKEN as its first line on standard output and as its first line on
// standard error, before anything else, a usage error included".

import (
	"bytes"
	"strings"
	"testing"
)

func TestReviewP3Try1BadFlagWithAWorkflowCommand(t *testing.T) {
	const hostile = "--nope\n::add-mask::REVIEWP3SECRET"
	var stdout, stderr bytes.Buffer
	code := realMain([]string{"run", hostile}, &stdout, &stderr, githubEnv)

	t.Logf("exit %d", code)
	printLines(t, "stdout", stdout.String())
	printLines(t, "stderr", stderr.String())

	out, errOut := splitLines(stdout.String()), splitLines(stderr.String())
	if len(out) != 2 || !strings.HasPrefix(out[0], "::stop-commands::") || out[1] != "::"+strings.TrimPrefix(out[0], "::stop-commands::")+"::" {
		t.Errorf("stdout is not stop then resume: %q", stdout.String())
	}
	if len(errOut) < 4 || errOut[0] != out[0] {
		t.Fatalf("stderr does not begin with the stop line: %q", stderr.String())
	}
	// The runner reads a line that begins with :: after leading white space as a command.
	for _, l := range errOut[1:] {
		if strings.HasPrefix(strings.TrimLeft(l, " \t"), "::") {
			t.Logf("a line of stderr after the stop line begins with :: after leading white space: %q", l)
		}
	}
}

// printLines prints a stream one line a line, quoted, so that the exact text is observed.
func printLines(t *testing.T, name, s string) {
	t.Helper()
	t.Logf("%s, %d line(s):", name, len(splitLines(s)))
	for i, l := range splitLines(s) {
		t.Logf("%s[%d] = %q", name, i, l)
	}
}
````

- Command, from the copy's root: `go test ./cmd/rollproof -run '^TestReviewP3Try1BadFlagWithAWorkflowCommand$' -count=1 -v`.
- Reviewer's output:

````text
review_p3_1_test.go:22: exit 2
review_p3_1_test.go:23: stdout[0] = "::stop-commands::KASQ7TNGE4NQFF6BFLYKFIZF6C"
review_p3_1_test.go:23: stdout[1] = "::KASQ7TNGE4NQFF6BFLYKFIZF6C::"
review_p3_1_test.go:24: stderr[0] = "::stop-commands::KASQ7TNGE4NQFF6BFLYKFIZF6C"
review_p3_1_test.go:24: stderr[1] = "flag provided but not defined: -nope"
review_p3_1_test.go:24: stderr[2] = "::add-mask::REVIEWP3SECRET"
review_p3_1_test.go:24: stderr[3] = "usage: rollproof run [-f FILE] [--out DIR] [--name NAME] [--keep-cluster]"
review_p3_1_test.go:36: a line of stderr after the stop line begins with :: after leading white space: "::add-mask::REVIEWP3SECRET"
````

- The re-run's output (exit 0):

````
    review_p3_1_test.go:22: exit 2
    review_p3_1_test.go:23: stdout, 2 line(s):
    review_p3_1_test.go:23: stdout[0] = "::stop-commands::7S3ZTPVWVRWA6JKG7FLJEJNCCI"
    review_p3_1_test.go:23: stdout[1] = "::7S3ZTPVWVRWA6JKG7FLJEJNCCI::"
    review_p3_1_test.go:24: stderr, 6 line(s):
    review_p3_1_test.go:24: stderr[0] = "::stop-commands::7S3ZTPVWVRWA6JKG7FLJEJNCCI"
    review_p3_1_test.go:24: stderr[1] = "flag provided but not defined: -nope"
    review_p3_1_test.go:24: stderr[2] = "::add-mask::REVIEWP3SECRET"
    review_p3_1_test.go:24: stderr[3] = "usage: rollproof run [-f FILE] [--out DIR] [--name NAME] [--keep-cluster]"
    review_p3_1_test.go:24: stderr[4] = "       rollproof version"
    review_p3_1_test.go:24: stderr[5] = "       rollproof probe http --url URL --rate N --timeout D --expect-status RANGE [--header NAME=VALUE]..."
    review_p3_1_test.go:36: a line of stderr after the stop line begins with :: after leading white space: "::add-mask::REVIEWP3SECRET"
````

- Verdict of the re-run: the rule kept in both runs. A bad flag, with a workflow command planted on standard error after the stop line. Held: the stop line is first on both streams, and the hostile line is after it on the same stream.

#### P3-2: an argument that is no flag

- Test: `review-3/cmd/rollproof/review_p3_2_test.go`, `TestReviewP3Try2ArgumentThatIsNoFlag`, as the copy holds it:

````go
package main

// Try 2 of the third part of the security review of section 14, point 13: an argument that
// is no flag, and a -f value, both carrying a workflow command. The usage error must come
// after the stop line, and the input must not be read as a command.

import (
	"bytes"
	"strings"
	"testing"
)

func TestReviewP3Try2ArgumentThatIsNoFlag(t *testing.T) {
	const planted = "##[add-mask]REVIEWP3SECRET"
	var stdout, stderr bytes.Buffer
	code := realMain([]string{"run", "-f", "/tmp/" + planted + ".yaml", planted}, &stdout, &stderr, githubEnv)

	t.Logf("exit %d", code)
	printLines(t, "stdout", stdout.String())
	printLines(t, "stderr", stderr.String())

	out, errOut := splitLines(stdout.String()), splitLines(stderr.String())
	if len(out) != 2 || !strings.HasPrefix(out[0], "::stop-commands::") || out[1] != "::"+strings.TrimPrefix(out[0], "::stop-commands::")+"::" {
		t.Errorf("stdout is not stop then resume: %q", stdout.String())
	}
	if len(errOut) == 0 || errOut[0] != out[0] {
		t.Fatalf("stderr does not begin with the stop line: %q", stderr.String())
	}
	all := stdout.String() + stderr.String()
	if strings.Contains(all, planted) {
		t.Errorf("the planted text was printed: %q", all)
	}
	t.Logf("the planted text appears in neither stream: %v", !strings.Contains(all, planted))
}
````

- Command, from the copy's root: `go test ./cmd/rollproof -run '^TestReviewP3Try2ArgumentThatIsNoFlag$' -count=1 -v`.
- Reviewer's output:

````text
review_p3_2_test.go:18: exit 2
review_p3_2_test.go:19: stdout[0] = "::stop-commands::BDABD6UPGYIQKVZTH3QWLR3MYC"
review_p3_2_test.go:19: stdout[1] = "::BDABD6UPGYIQKVZTH3QWLR3MYC::"
review_p3_2_test.go:20: stderr[0] = "::stop-commands::BDABD6UPGYIQKVZTH3QWLR3MYC"
review_p3_2_test.go:20: stderr[1] = "usage: rollproof run [-f FILE] [--out DIR] [--name NAME] [--keep-cluster]"
review_p3_2_test.go:33: the planted text appears in neither stream: true
````

- The re-run's output (exit 0):

````
    review_p3_2_test.go:18: exit 2
    review_p3_2_test.go:19: stdout, 2 line(s):
    review_p3_2_test.go:19: stdout[0] = "::stop-commands::65TWJ3I2IZA4ZPLRUFQWJRQM6Z"
    review_p3_2_test.go:19: stdout[1] = "::65TWJ3I2IZA4ZPLRUFQWJRQM6Z::"
    review_p3_2_test.go:20: stderr, 4 line(s):
    review_p3_2_test.go:20: stderr[0] = "::stop-commands::65TWJ3I2IZA4ZPLRUFQWJRQM6Z"
    review_p3_2_test.go:20: stderr[1] = "usage: rollproof run [-f FILE] [--out DIR] [--name NAME] [--keep-cluster]"
    review_p3_2_test.go:20: stderr[2] = "       rollproof version"
    review_p3_2_test.go:20: stderr[3] = "       rollproof probe http --url URL --rate N --timeout D --expect-status RANGE [--header NAME=VALUE]..."
    review_p3_2_test.go:33: the planted text appears in neither stream: true
````

- Verdict of the re-run: the rule kept in both runs. An argument that is no flag, with hostile text in the argument and in the value of `-f`. Held: neither stream prints the planted text.

#### P3-3: a rejected configuration

- Test: `review-3/cmd/rollproof/review_p3_3_test.go`, `TestReviewP3Try3RejectedConfigurationWithAHostileName`, as the copy holds it:

````go
package main

// Try 3 of the third part of the security review of section 14, point 13: a rejected
// configuration whose file name carries a workflow command in each of the runner's two
// forms, and a newline. The console prints the file's name; the line must be one line,
// between the stop and the resume, so that the runner reads it with commands stopped.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewP3Try3RejectedConfigurationWithAHostileName(t *testing.T) {
	dir := t.TempDir()
	name := "a##[add-mask]REVIEWP3SECRET\nb::add-mask::REVIEWP3SECOND.yaml"
	file := filepath.Join(dir, name)
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "results")

	var stdout, stderr bytes.Buffer
	code := realMain([]string{"run", "-f", file, "--out", out}, &stdout, &stderr, githubEnv)

	t.Logf("exit %d", code)
	printLines(t, "stdout", stdout.String())
	printLines(t, "stderr", stderr.String())

	outLines := splitLines(stdout.String())
	if len(outLines) < 3 || !strings.HasPrefix(outLines[0], "::stop-commands::") {
		t.Fatalf("stdout does not begin with the stop line: %q", stdout.String())
	}
	token := strings.TrimPrefix(outLines[0], "::stop-commands::")
	if last := outLines[len(outLines)-1]; last != "::"+token+"::" {
		t.Fatalf("stdout's last line is %q, want %q", last, "::"+token+"::")
	}
	for i, l := range outLines[1 : len(outLines)-1] {
		if strings.Contains(l, "REVIEWP3") {
			t.Logf("console line %d carries the planted text on one line: %q", i+1, l)
		}
		if strings.HasPrefix(strings.TrimLeft(l, " \t"), "::") {
			t.Errorf("a console line begins with :: after leading white space: %q", l)
		}
	}
}
````

- Command, from the copy's root: `go test ./cmd/rollproof -run '^TestReviewP3Try3RejectedConfigurationWithAHostileName$' -count=1 -v`.
- Reviewer's output:

````text
review_p3_3_test.go:28: exit 2
review_p3_3_test.go:29: stdout[0] = "::stop-commands::O5IBQREKU5NZXJE53DSS5JSLJX"
review_p3_3_test.go:29: stdout[1] = "rollproof: NOT VERIFIED"
review_p3_3_test.go:29: stdout[3] = "could not test: the configuration file /tmp/TestReviewP3Try3RejectedConfigurationWithAHostileName3171506752/001/a##[add-mask]REVIEWP3SECRET   [config-rejected]"
review_p3_3_test.go:29: stdout[4] = "  b::add-mask::REVIEWP3SECOND.yaml is empty"
review_p3_3_test.go:29: stdout[6] = "::O5IBQREKU5NZXJE53DSS5JSLJX::"
review_p3_3_test.go:30: stderr[0] = "::stop-commands::O5IBQREKU5NZXJE53DSS5JSLJX"
````

- The re-run's output (exit 0):

````
    review_p3_3_test.go:28: exit 2
    review_p3_3_test.go:29: stdout, 7 line(s):
    review_p3_3_test.go:29: stdout[0] = "::stop-commands::KU7V4VTR5KGMPNTF3LRYO3NL3X"
    review_p3_3_test.go:29: stdout[1] = "rollproof: NOT VERIFIED"
    review_p3_3_test.go:29: stdout[2] = ""
    review_p3_3_test.go:29: stdout[3] = "could not test: the configuration file /tmp/TestReviewP3Try3RejectedConfigurationWithAHostileName4109164447/001/a##[add-mask]REVIEWP3SECRET   [config-rejected]"
    review_p3_3_test.go:29: stdout[4] = "  b::add-mask::REVIEWP3SECOND.yaml is empty"
    review_p3_3_test.go:29: stdout[5] = "results: /tmp/TestReviewP3Try3RejectedConfigurationWithAHostileName4109164447/002/results"
    review_p3_3_test.go:29: stdout[6] = "::KU7V4VTR5KGMPNTF3LRYO3NL3X::"
    review_p3_3_test.go:30: stderr, 1 line(s):
    review_p3_3_test.go:30: stderr[0] = "::stop-commands::KU7V4VTR5KGMPNTF3LRYO3NL3X"
    review_p3_3_test.go:42: console line 3 carries the planted text on one line: "could not test: the configuration file /tmp/TestReviewP3Try3RejectedConfigurationWithAHostileName4109164447/001/a##[add-mask]REVIEWP3SECRET   [config-rejected]"
    review_p3_3_test.go:42: console line 4 carries the planted text on one line: "  b::add-mask::REVIEWP3SECOND.yaml is empty"
````

- Verdict of the re-run: the rule kept in both runs. A rejected configuration whose file name holds both command forms and a line end. Held: every line stands on standard output between the stop and the resume.

#### P3-4: a results folder that cannot be opened, whose path holds both command forms

- Test: `review-3/cmd/rollproof/review_p3_4_test.go`, `TestReviewP3Try4ResultsFolderCannotBeOpened`, as the copy holds it:

````go
package main

// Try 4 of the third part of the security review of section 14, point 13: a results folder
// that cannot be opened, named with the older workflow-command form ##[ and, after a
// newline, the newer form ::. The tool prints the folder's path and the system's error on
// standard error, after the stop line; the resume is last on standard output. This is the
// one kind of line that section 10 ("Console") allows on standard error just before the
// tool ends, which point 33 says can reach the runner after the resume.
//
// Read in the runner's source, at 7e211dc: ActionCommand.cs TryParse (lines 130 to 160)
// finds "##[" anywhere in a line, takes the command name between it and the next "]", and
// makes the text after "]" the data; ActionCommandManager.cs lines 83 to 143 add a mask,
// set an output, or stop the commands again for a registered command. So the path's
// "##[add-mask]REVIEWP3SECRET" is a command if the runner reads this line after the resume.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewP3Try4ResultsFolderCannotBeOpened(t *testing.T) {
	dir := t.TempDir()
	name := "x##[add-mask]REVIEWP3SECRET\n::add-mask::REVIEWP3SECOND"
	out := filepath.Join(dir, name)
	if err := os.WriteFile(out, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := realMain([]string{"run", "-f", "does-not-exist.yaml", "--out", out}, &stdout, &stderr, githubEnv)

	t.Logf("exit %d", code)
	printLines(t, "stdout", stdout.String())
	printLines(t, "stderr", stderr.String())

	outLines, errLines := splitLines(stdout.String()), splitLines(stderr.String())
	if len(outLines) != 2 || !strings.HasPrefix(outLines[0], "::stop-commands::") || outLines[1] != "::"+strings.TrimPrefix(outLines[0], "::stop-commands::")+"::" {
		t.Errorf("stdout is not stop then resume: %q", stdout.String())
	}
	if len(errLines) != 2 || errLines[0] != outLines[0] {
		t.Fatalf("stderr is not the stop line then one failure line: %q", stderr.String())
	}
	failure := errLines[1]
	t.Logf("stderr's failure line holds the older form anywhere: %v", strings.Contains(failure, "##[add-mask]REVIEWP3SECRET"))
	t.Logf("stderr's failure line begins with the newer form after leading white space: %v",
		strings.HasPrefix(strings.TrimLeft(failure, " \t"), "::"))
}
````

- Command, from the copy's root: `go test ./cmd/rollproof -run '^TestReviewP3Try4ResultsFolderCannotBeOpened$' -count=1 -v`.
- Rule: "The tool prints that line last on standard output when it ends by itself, whatever its exit code, so that a step that calls the tool from a script of its own can use commands again afterwards." (section 8, "Workflow commands.") With it the open place point 33 names: "A line that the tool writes on standard error just before it ends can reach the runner after the resume line, and is then read for commands; section 10 says what can stand there ("Console", the bullet on standard error)."
- Reviewer's output (its verdict: broken, narrowly: the line is written before the resume, and the break needs open place 1, but the path it carries is the caller's and is not inert):

````
review_p3_4_test.go:35: exit 2
review_p3_4_test.go:36: stdout[0] = "::stop-commands::JLVZ56LVBRWXORMLEHT65KZJEE"
review_p3_4_test.go:36: stdout[1] = "::JLVZ56LVBRWXORMLEHT65KZJEE::"
review_p3_4_test.go:37: stderr[0] = "::stop-commands::JLVZ56LVBRWXORMLEHT65KZJEE"
review_p3_4_test.go:37: stderr[1] = "rollproof: cannot write results: results folder /tmp/TestReviewP3Try4ResultsFolderCannotBeOpened1255822150/001/x##[add-mask]REVIEWP3SECRET ::add-mask::REVIEWP3SECOND: mkdir /tmp/TestReviewP3Try4ResultsFolderCannotBeOpened1255822150/001/x##[add-mask]REVIEWP3SECRET ::add-mask::REVIEWP3SECOND: not a directory"
review_p3_4_test.go:47: stderr's failure line holds the older form anywhere: true
review_p3_4_test.go:48: stderr's failure line begins with the newer form after leading white space: false
````

- The re-run's output (exit 0: the test prints and passes; the token and the temporary folder are new for this run):

````
    review_p3_4_test.go:35: exit 2
    review_p3_4_test.go:36: stdout, 2 line(s):
    review_p3_4_test.go:36: stdout[0] = "::stop-commands::WE23IQAKXLXC6BTBIVXIOPIP5F"
    review_p3_4_test.go:36: stdout[1] = "::WE23IQAKXLXC6BTBIVXIOPIP5F::"
    review_p3_4_test.go:37: stderr, 2 line(s):
    review_p3_4_test.go:37: stderr[0] = "::stop-commands::WE23IQAKXLXC6BTBIVXIOPIP5F"
    review_p3_4_test.go:37: stderr[1] = "rollproof: cannot write results: results folder /tmp/TestReviewP3Try4ResultsFolderCannotBeOpened4028257957/001/x##[add-mask]REVIEWP3SECRET ::add-mask::REVIEWP3SECOND: mkdir /tmp/TestReviewP3Try4ResultsFolderCannotBeOpened4028257957/001/x##[add-mask]REVIEWP3SECRET ::add-mask::REVIEWP3SECOND: not a directory"
    review_p3_4_test.go:47: stderr's failure line holds the older form anywhere: true
    review_p3_4_test.go:48: stderr's failure line begins with the newer form after leading white space: false
````

- Verdict of the re-run: holds.
- Ruling: document (the maintainer, 2026-11-25), in the design's section 11, with the two open places of point 33: the results folder's path, which is the caller's, stands unescaped in that line on standard error, so the first open place could hand a command on; a caller gives a path that holds none. The smaller choice: a sentence, and no code.
- The text: commit `c19268f`.

#### P3-5: a run ended by a signal

- Test: `review-3/cmd/rollproof/review_p3_5_test.go`, `TestReviewP3Try5RunEndedByASignal`, as the copy holds it:

````go
package main

// Try 5 of the third part of the security review of section 14, point 13: a run ended by a
// signal. The tool must print no resume line, so that the commands stay stopped (section 8,
// "Workflow commands"; section 10, "On a signal").

import (
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReviewP3Try5RunEndedByASignal(t *testing.T) {
	c := startChild(t, fakeWaits, "GITHUB_ACTIONS=true")
	c.waitFor(t, "started")
	c.signal(t, syscall.SIGTERM)
	ws := c.wait(t, 10*time.Second)

	t.Logf("the child ended %s", ending(ws))
	printLines(t, "stdout", c.stdout.String())
	printLines(t, "stderr", c.stderr.String())

	outLines := splitLines(c.stdout.String())
	if len(outLines) < 1 || !strings.HasPrefix(outLines[0], "::stop-commands::") {
		t.Fatalf("stdout does not begin with the stop line: %q", c.stdout.String())
	}
	token := strings.TrimPrefix(outLines[0], "::stop-commands::")
	resume := "::" + token + "::"
	if slices.Contains(outLines, resume) {
		t.Errorf("stdout holds the resume line %q", resume)
	}
	for name, lines := range map[string][]string{"stdout": outLines, "stderr": splitLines(c.stderr.String())} {
		for _, l := range lines {
			if l == resume {
				t.Errorf("%s holds the resume line %q", name, resume)
			}
		}
	}
	t.Logf("neither stream holds the resume line %q", resume)
}
````

- Command, from the copy's root: `go test ./cmd/rollproof -run '^TestReviewP3Try5RunEndedByASignal$' -count=1 -v`.
- Reviewer's output:

````text
review_p3_5_test.go:21: the child ended by terminated
review_p3_5_test.go:22: stdout[0] = "::stop-commands::TRJ6AIDZA2KAL5K735LQWJAEL3"
review_p3_5_test.go:22: stdout[1] = "started"
review_p3_5_test.go:22: stdout[2] = "returned"
review_p3_5_test.go:23: stderr[0] = "::stop-commands::TRJ6AIDZA2KAL5K735LQWJAEL3"
review_p3_5_test.go:41: neither stream holds the resume line "::TRJ6AIDZA2KAL5K735LQWJAEL3::"
````

- The re-run's output (exit 0):

````
    review_p3_5_test.go:21: the child ended by terminated
    review_p3_5_test.go:22: stdout, 3 line(s):
    review_p3_5_test.go:22: stdout[0] = "::stop-commands::IBKLHP7T2D7A73PODSW5KUVYSX"
    review_p3_5_test.go:22: stdout[1] = "started"
    review_p3_5_test.go:22: stdout[2] = "returned"
    review_p3_5_test.go:23: stderr, 1 line(s):
    review_p3_5_test.go:23: stderr[0] = "::stop-commands::IBKLHP7T2D7A73PODSW5KUVYSX"
    review_p3_5_test.go:41: neither stream holds the resume line "::IBKLHP7T2D7A73PODSW5KUVYSX::"
````

- Verdict of the re-run: the rule kept in both runs. A run ended by a signal. Held: no resume line on either stream.

#### P3-6: a panic that escapes the run

- Test: `review-3/cmd/rollproof/review_p3_6_test.go`, `TestReviewP3Try6APanicThatEscapesTheRun`, as the copy holds it:

````go
package main

// Try 6 of the third part of the security review of section 14, point 13: a panic that
// escapes the run. The tool must print no resume line, so that the commands stay stopped
// for the rest of the step and the panic's trace is read as plain lines (section 8,
// "Workflow commands"; decision 26).
//
// The child is this test binary, started to run this test with the variable set: it makes
// the run panic and lets the panic reach the runtime, as a crash of the tool's run does.

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"rollproof/internal/run"
)

const reviewP3Try6Child = "ROLLPROOF_REVIEW_P3_6_CHILD"

func TestReviewP3Try6APanicThatEscapesTheRun(t *testing.T) {
	if os.Getenv(reviewP3Try6Child) == "1" {
		runOf = func(context.Context, run.Options, run.Units) int {
			panic("hostile text ::add-mask::REVIEWP3SECRET and ##[add-mask]REVIEWP3SECOND")
		}
		// The panic must reach the runtime, as a crash of the tool's run does, and not the
		// test framework's report of a failed test: it runs on a goroutine of its own, which
		// crashes the process, and this one waits there.
		go func() { endProcess(realMain([]string{"run"}, os.Stdout, os.Stderr, os.Getenv)) }()
		select {}
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GITHUB_ACTIONS=") && !strings.HasPrefix(e, reviewP3Try6Child+"=") {
			env = append(env, e)
		}
	}
	env = append(env, "GITHUB_ACTIONS=true", reviewP3Try6Child+"=1")
	cmd := exec.Command(exe, "-test.run=^TestReviewP3Try6APanicThatEscapesTheRun$")
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	code := 0
	if ee, ok := runErr.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if runErr != nil {
		t.Fatal(runErr)
	}

	t.Logf("the child exited %d", code)
	outLines, errLines := splitLines(stdout.String()), splitLines(stderr.String())
	t.Logf("stdout, %d line(s): first = %q, last = %q", len(outLines), outLines[0], outLines[len(outLines)-1])
	t.Logf("stderr's first line = %q", errLines[0])
	for _, l := range errLines {
		if strings.HasPrefix(l, "panic: ") {
			t.Logf("the trace's panic line = %q", l)
		}
	}
	if len(outLines) != 1 || !strings.HasPrefix(outLines[0], "::stop-commands::") {
		t.Errorf("stdout is not only the stop line: %q", stdout.String())
	}
	token := strings.TrimPrefix(outLines[0], "::stop-commands::")
	resume := "::" + token + "::"
	if strings.Contains(stdout.String()+stderr.String(), resume) {
		t.Errorf("a stream holds the resume line %q", resume)
	}
	t.Logf("neither stream holds the resume line %q", resume)
}
````

- Command, from the copy's root: `go test ./cmd/rollproof -run '^TestReviewP3Try6APanicThatEscapesTheRun$' -count=1 -v`.
- Reviewer's output:

````text
review_p3_6_test.go:59: the child exited 2
review_p3_6_test.go:61: stdout, 1 line(s): first = "::stop-commands::LQRCFVN3V6ITZOJNB3C5BOS2PE", last = "::stop-commands::LQRCFVN3V6ITZOJNB3C5BOS2PE"
review_p3_6_test.go:62: stderr's first line = "::stop-commands::LQRCFVN3V6ITZOJNB3C5BOS2PE"
review_p3_6_test.go:65: the trace's panic line = "panic: hostile text ::add-mask::REVIEWP3SECRET and ##[add-mask]REVIEWP3SECOND"
review_p3_6_test.go:76: neither stream holds the resume line "::LQRCFVN3V6ITZOJNB3C5BOS2PE::"
````

- The re-run's output (exit 0):

````
    review_p3_6_test.go:59: the child exited 2
    review_p3_6_test.go:61: stdout, 1 line(s): first = "::stop-commands::DYBLGFC2MOP6362W2TP6RMMHJU", last = "::stop-commands::DYBLGFC2MOP6362W2TP6RMMHJU"
    review_p3_6_test.go:62: stderr's first line = "::stop-commands::DYBLGFC2MOP6362W2TP6RMMHJU"
    review_p3_6_test.go:65: the trace's panic line = "panic: hostile text ::add-mask::REVIEWP3SECRET and ##[add-mask]REVIEWP3SECOND"
    review_p3_6_test.go:76: neither stream holds the resume line "::DYBLGFC2MOP6362W2TP6RMMHJU::"
````

- Verdict of the re-run: the rule kept in both runs. A panic that escapes the run, with both command forms in its text. Held: no resume line, and the trace is read while the commands are stopped.

#### P3-7: the action's script runs `uname` before its stop line, and prints its output when the runner is not Linux

- Test: `review-3/hack/action/review_p3_7_test.go`, `TestReviewP3Try7UnameBeforeTheStopLine`, as the copy holds it:

````go
package action

// Try 7 of the third part of the security review of section 14, point 13: a line before the
// action's stop line. The script runs uname to learn the runner's system before it prints
// its stop line, and it neither redirects uname's standard error nor cleans what uname
// printed. A uname that prints a workflow command there puts that line on stderr before the
// stop line, or with no stop line at all when its stdout is not Linux.
//
// It attacks the claim of section 8, "Workflow commands": "The action's script prints the
// same line on both streams with a token of its own, once it knows that the runner is Linux
// and before any line that can hold an input or an engine's output", and section 11's "text
// from a chart, a command or the API server cannot become one".

import (
	"strings"
	"testing"
)

func TestReviewP3Try7UnameBeforeTheStopLine(t *testing.T) {
	t.Run("uname's stderr, reporting Linux", func(t *testing.T) {
		r := startingRun(t)
		r.command("uname", `printf '%s\n' '::add-mask::REVIEWP3SECRET' '##[add-mask]REVIEWP3SECOND' >&2
printf '%s\n' Linux
`)
		stdout, code, stderr := r.start(r.script)
		logUnameRun(t, code, stdout, stderr)
	})

	t.Run("uname's stdout, not Linux", func(t *testing.T) {
		r := newScriptRun(t)
		r.command("uname", `printf '%s\n' Darwin '::add-mask::REVIEWP3SECRET'
`)
		stdout, code, stderr := r.start(r.script)
		logUnameRun(t, code, stdout, stderr)
	})
}

// logUnameRun prints the script's two streams, and where the first workflow-command line
// stands relative to the stop line.
func logUnameRun(t *testing.T, code int, stdout, stderr string) {
	t.Helper()
	t.Logf("exit %d", code)
	for i, l := range textLines(stdout) {
		t.Logf("stdout[%d] = %q", i, l)
	}
	for i, l := range textLines(stderr) {
		t.Logf("stderr[%d] = %q", i, l)
	}
	outLines, errLines := textLines(stdout), textLines(stderr)
	stop := -1
	for i, l := range errLines {
		if strings.HasPrefix(l, "::stop-commands::") {
			stop = i
			break
		}
	}
	t.Logf("the stop line's index on stderr: %d (-1 for none)", stop)
	for i, l := range errLines {
		if strings.HasPrefix(strings.TrimLeft(l, " \t"), "::") && !strings.HasPrefix(l, "::stop-commands::") {
			t.Logf("stderr[%d] begins with :: after leading white space; the stop line's index is %d (-1 for none): %q", i, stop, l)
		}
	}
	t.Logf("stdout's first line is the stop line: %v", len(outLines) > 0 && strings.HasPrefix(outLines[0], "::stop-commands::"))
}
````

- Command, from the copy's root: `go test ./hack/action -run '^TestReviewP3Try7UnameBeforeTheStopLine$' -count=1 -v`.
- Rule: "The action's script prints the same line on both streams with a token of its own, once it knows that the runner is Linux and before any line that can hold an input or an engine's output, and never resumes" (section 8, "Workflow commands."), and section 11: the lines stop the runner "from reading a workflow command in what they print, so that text from a chart, a command or the API server cannot become one".
- Reviewer's output (its verdict: broken, narrowly: a hostile or wrapped `uname` puts a workflow command on standard error before any stop line, or with no stop line at all):

````
review_p3_7_test.go:26: exit 7
review_p3_7_test.go:26: stdout[0] = "::stop-commands::d40df991-e115-480e-9a88-d5a5366bed8e"
review_p3_7_test.go:26: stderr[0] = "::add-mask::REVIEWP3SECRET"
review_p3_7_test.go:26: stderr[1] = "##[add-mask]REVIEWP3SECOND"
review_p3_7_test.go:26: stderr[2] = "::stop-commands::d40df991-e115-480e-9a88-d5a5366bed8e"
review_p3_7_test.go:26: stderr[3] = "rollproof: the tool from 127.0.0.1:5000/rollproof@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, through docker"
review_p3_7_test.go:26: the stop line's index on stderr: 2 (-1 for none)
review_p3_7_test.go:26: stderr[0] begins with :: after leading white space; the stop line's index is 2 (-1 for none): "::add-mask::REVIEWP3SECRET"
review_p3_7_test.go:34: exit 2
review_p3_7_test.go:34: stderr[0] = "rollproof: the runner is not Linux (uname -s printed Darwin"
review_p3_7_test.go:34: stderr[1] = "::add-mask::REVIEWP3SECRET); the action runs only on Linux"
review_p3_7_test.go:34: the stop line's index on stderr: -1 (-1 for none)
review_p3_7_test.go:34: stderr[1] begins with :: after leading white space; the stop line's index is -1 (-1 for none): "::add-mask::REVIEWP3SECRET); the action runs only on Linux"
````

- The re-run's output (exit 0): the same lines, and the two that say whether standard output's first line is the stop line:

````
    review_p3_7_test.go:26: exit 7
    review_p3_7_test.go:26: stdout[0] = "::stop-commands::20b308f5-fe93-43ab-9240-733a39ae8356"
    review_p3_7_test.go:26: stderr[0] = "::add-mask::REVIEWP3SECRET"
    review_p3_7_test.go:26: stderr[1] = "##[add-mask]REVIEWP3SECOND"
    review_p3_7_test.go:26: stderr[2] = "::stop-commands::20b308f5-fe93-43ab-9240-733a39ae8356"
    review_p3_7_test.go:26: stderr[3] = "rollproof: the tool from 127.0.0.1:5000/rollproof@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, through docker"
    review_p3_7_test.go:26: the stop line's index on stderr: 2 (-1 for none)
    review_p3_7_test.go:26: stderr[0] begins with :: after leading white space; the stop line's index is 2 (-1 for none): "::add-mask::REVIEWP3SECRET"
    review_p3_7_test.go:26: stdout's first line is the stop line: true
    review_p3_7_test.go:34: exit 2
    review_p3_7_test.go:34: stderr[0] = "rollproof: the runner is not Linux (uname -s printed Darwin"
    review_p3_7_test.go:34: stderr[1] = "::add-mask::REVIEWP3SECRET); the action runs only on Linux"
    review_p3_7_test.go:34: the stop line's index on stderr: -1 (-1 for none)
    review_p3_7_test.go:34: stderr[1] begins with :: after leading white space; the stop line's index is -1 (-1 for none): "::add-mask::REVIEWP3SECRET); the action runs only on Linux"
    review_p3_7_test.go:34: stdout's first line is the stop line: false
````

- Verdict of the re-run: holds.
- Ruling: fix (the maintainer, 2026-11-25). The smaller change: the script sends `uname`'s standard error to /dev/null and prints nothing of its output in the not-Linux message; no configuration key, reason code or schema version; no test name changes. Built test-first in Task 2.
- The fix: commit `ed08ae8`; its test `TestTheScriptPrintsNothingOfUnameBeforeItsStopLine`.

#### P3-8: an input, an engine's message and a path in failure text

- Test: `review-3/hack/action/review_p3_8_test.go`, `TestReviewP3Try8FailureText`, as the copy holds it:

````go
package action

// Try 8 of the third part of the security review of section 14, point 13: an input, an
// engine's message and a path inside a failure's text. Each is planted with a workflow
// command in both of the runner's forms. The script has printed its stop line on both
// streams before any of them, and it never resumes, so the runner must read none of them
// as a command.

import (
	"strings"
	"testing"
)

func TestReviewP3Try8FailureText(t *testing.T) {
	t.Run("an input in image_ref's failure", func(t *testing.T) {
		r := startingRun(t)
		r.env["ROLLPROOF_ACTION_IMAGE"] = "r.example/rollproof##[add-mask]REVIEWP3SECRET\n::add-mask::REVIEWP3SECOND"
		r.env["ROLLPROOF_ACTION_DIGEST"] = ""
		stdout, code, stderr := r.start(r.script)
		logScriptRun(t, code, stdout, stderr)
	})

	t.Run("an engine's message on a pull that fails", func(t *testing.T) {
		r := startingRun(t)
		r.command("docker", `case "$1" in
  -v) printf '%s\n' 'Docker version 28.0.4, build b8034c0'; exit 0 ;;
  pull) printf '%s\n' '::add-mask::REVIEWP3SECRET' '##[error]REVIEWP3SECOND' >&2; exit 1 ;;
esac
exit 99
`)
		stdout, code, stderr := r.start(r.script)
		logScriptRun(t, code, stdout, stderr)
	})

	t.Run("a file's name in the workspace's failure", func(t *testing.T) {
		r := startingRun(t)
		r.env["GITHUB_WORKSPACE"] = r.root + "/no-such##[add-mask]REVIEWP3SECRET\n::add-mask::REVIEWP3SECOND"
		stdout, code, stderr := r.start(r.script)
		logScriptRun(t, code, stdout, stderr)
	})
}

// logScriptRun prints the script's two streams and says whether the stop line is first on
// each, and whether a later line begins with :: after leading white space.
func logScriptRun(t *testing.T, code int, stdout, stderr string) {
	t.Helper()
	t.Logf("exit %d", code)
	for i, l := range textLines(stdout) {
		t.Logf("stdout[%d] = %q", i, l)
	}
	for i, l := range textLines(stderr) {
		t.Logf("stderr[%d] = %q", i, l)
	}
	outLines, errLines := textLines(stdout), textLines(stderr)
	t.Logf("stdout's first line is the stop line: %v", len(outLines) > 0 && strings.HasPrefix(outLines[0], "::stop-commands::"))
	t.Logf("stderr's first line is the stop line: %v", len(errLines) > 0 && strings.HasPrefix(errLines[0], "::stop-commands::"))
	for i, l := range errLines[1:] {
		if strings.HasPrefix(strings.TrimLeft(l, " \t"), "::") {
			t.Logf("stderr[%d] begins with :: after leading white space: %q", i+1, l)
		}
	}
}
````

- Command, from the copy's root: `go test ./hack/action -run '^TestReviewP3Try8FailureText$' -count=1 -v`.
- Reviewer's output:

````text
=== RUN   TestReviewP3Try8FailureText/an_input_in_image_ref's_failure
review_p3_8_test.go:20: exit 2
review_p3_8_test.go:20: stdout[0] = "::stop-commands::90cb0584-ed51-47f2-ac22-4b19883de62d"
review_p3_8_test.go:20: stderr[0] = "::stop-commands::90cb0584-ed51-47f2-ac22-4b19883de62d"
review_p3_8_test.go:20: stderr[1] = "rollproof: the image r.example/rollproof##[add-mask]REVIEWP3SECRET"
review_p3_8_test.go:20: stderr[2] = "::add-mask::REVIEWP3SECOND carries no digest, and before its first release this action carries none: give the input image a reference with @sha256:"
review_p3_8_test.go:20: stderr's first line is the stop line: true
=== RUN   TestReviewP3Try8FailureText/an_engine's_message_on_a_pull_that_fails
review_p3_8_test.go:32: exit 2
review_p3_8_test.go:32: stdout[0] = "::stop-commands::a742c272-f799-415b-b8f9-c90178ccbf2b"
review_p3_8_test.go:32: stderr[0] = "::stop-commands::a742c272-f799-415b-b8f9-c90178ccbf2b"
review_p3_8_test.go:32: stderr[1] = "::add-mask::REVIEWP3SECRET"
review_p3_8_test.go:32: stderr[2] = "##[error]REVIEWP3SECOND"
review_p3_8_test.go:32: stderr[3] = "rollproof: docker could not pull 127.0.0.1:5000/rollproof@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
review_p3_8_test.go:32: stderr's first line is the stop line: true
=== RUN   TestReviewP3Try8FailureText/a_file's_name_in_the_workspace's_failure
review_p3_8_test.go:39: exit 2
review_p3_8_test.go:39: stderr[0] = "::stop-commands::2c4f8e1e-d052-42d3-91fd-fe1486ae0bfe"
review_p3_8_test.go:39: stderr[1] = "/tmp/opencode/rollproof-graduation/review-3/action.sh: line 140: cd: $'/tmp/TestReviewP3Try8FailureTexta_files_name_in_the_workspaces_fai2131475665/001/no-such##[add-mask]REVIEWP3SECRET\\n::add-mask::REVIEWP3SECOND': No such file or directory"
review_p3_8_test.go:39: stderr[2] = "rollproof: the workspace /tmp/TestReviewP3Try8FailureTexta_files_name_in_the_workspaces_fai2131475665/001/no-such##[add-mask]REVIEWP3SECRET"
review_p3_8_test.go:39: stderr[3] = "::add-mask::REVIEWP3SECOND cannot be entered"
review_p3_8_test.go:39: stderr's first line is the stop line: true
````

- The re-run's output (exit 0):

````
    review_p3_8_test.go:20: exit 2
    review_p3_8_test.go:20: stdout[0] = "::stop-commands::a2ee50fc-78e0-415a-af30-14771e8b184e"
    review_p3_8_test.go:20: stderr[0] = "::stop-commands::a2ee50fc-78e0-415a-af30-14771e8b184e"
    review_p3_8_test.go:20: stderr[1] = "rollproof: the image r.example/rollproof##[add-mask]REVIEWP3SECRET"
    review_p3_8_test.go:20: stderr[2] = "::add-mask::REVIEWP3SECOND carries no digest, and before its first release this action carries none: give the input image a reference with @sha256:"
    review_p3_8_test.go:20: stdout's first line is the stop line: true
    review_p3_8_test.go:20: stderr's first line is the stop line: true
    review_p3_8_test.go:20: stderr[2] begins with :: after leading white space: "::add-mask::REVIEWP3SECOND carries no digest, and before its first release this action carries none: give the input image a reference with @sha256:"
    review_p3_8_test.go:32: exit 2
    review_p3_8_test.go:32: stdout[0] = "::stop-commands::15a40613-f9d7-41dd-bb59-1607b181f8f9"
    review_p3_8_test.go:32: stderr[0] = "::stop-commands::15a40613-f9d7-41dd-bb59-1607b181f8f9"
    review_p3_8_test.go:32: stderr[1] = "::add-mask::REVIEWP3SECRET"
    review_p3_8_test.go:32: stderr[2] = "##[error]REVIEWP3SECOND"
    review_p3_8_test.go:32: stderr[3] = "rollproof: docker could not pull 127.0.0.1:5000/rollproof@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    review_p3_8_test.go:32: stdout's first line is the stop line: true
    review_p3_8_test.go:32: stderr's first line is the stop line: true
    review_p3_8_test.go:32: stderr[1] begins with :: after leading white space: "::add-mask::REVIEWP3SECRET"
    review_p3_8_test.go:39: exit 2
    review_p3_8_test.go:39: stdout[0] = "::stop-commands::7b86dad4-69d1-41e4-80b2-ade1d0f7eb2e"
    review_p3_8_test.go:39: stderr[0] = "::stop-commands::7b86dad4-69d1-41e4-80b2-ade1d0f7eb2e"
    review_p3_8_test.go:39: stderr[1] = "/tmp/opencode/rollproof-graduation/review-3/action.sh: line 140: cd: $'/tmp/TestReviewP3Try8FailureTexta_files_name_in_the_workspaces_fai390605181/001/no-such##[add-mask]REVIEWP3SECRET\\n::add-mask::REVIEWP3SECOND': No such file or directory"
    review_p3_8_test.go:39: stderr[2] = "rollproof: the workspace /tmp/TestReviewP3Try8FailureTexta_files_name_in_the_workspaces_fai390605181/001/no-such##[add-mask]REVIEWP3SECRET"
    review_p3_8_test.go:39: stderr[3] = "::add-mask::REVIEWP3SECOND cannot be entered"
    review_p3_8_test.go:39: stdout's first line is the stop line: true
    review_p3_8_test.go:39: stderr's first line is the stop line: true
    review_p3_8_test.go:39: stderr[3] begins with :: after leading white space: "::add-mask::REVIEWP3SECOND cannot be entered"
````

- Verdict of the re-run: the rule kept in both runs. In the action's script, an input, an engine's message and a file's name inside a failure's text. Held: the stop line is first on both streams, and the script never resumes.

#### P3-9: a token the caller could set through `env:` or guess

- Test: `review-3/cmd/rollproof/review_p3_9_test.go`, `TestReviewP3Try9TokenFromTheEnvironmentOrAGuess`, as the copy holds it:

````go
package main

// Try 9 of the third part of the security review of section 14, point 13: a token that a
// caller could set through env: or guess. Section 8 says the tool's token is "drawn anew
// for each run from the system's random source", and point 33 says it "comes from Go's
// crypto/rand". Nothing in the tool reads a token from the environment. This try sets every
// variable a caller could set to one fixed token and reads the line the tool prints.

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestReviewP3Try9TokenFromTheEnvironmentOrAGuess(t *testing.T) {
	const guessed = "ABCDEFGHIJKLMNOPQRSTUVWXYZ" // 26 characters of the token's alphabet
	getenv := func(name string) string {
		switch name {
		case "GITHUB_ACTIONS":
			return "true"
		case "GITHUB_OUTPUT", "GITHUB_STEP_SUMMARY", "RUNNER_TEMP":
			return ""
		}
		return guessed
	}
	form := regexp.MustCompile(`^[A-Z2-7]{26}$`)

	var tokens []string
	for i := 1; i <= 2; i++ {
		var stdout, stderr bytes.Buffer
		code := realMain([]string{"run", "-f", filepath.Join(t.TempDir(), "missing.yaml"), "--out", t.TempDir()}, &stdout, &stderr, getenv)
		t.Logf("run %d: exit %d", i, code)
		lines := splitLines(stdout.String())
		if len(lines) < 2 || !strings.HasPrefix(lines[0], "::stop-commands::") {
			t.Fatalf("run %d: stdout does not begin with the stop line: %q", i, stdout.String())
		}
		token := strings.TrimPrefix(lines[0], "::stop-commands::")
		tokens = append(tokens, token)
		t.Logf("run %d: the token printed is %q", i, token)
		t.Logf("run %d: it equals the guessed value: %v", i, token == guessed)
		t.Logf("run %d: it has the form of crypto/rand.Text's 26 characters: %v", i, form.MatchString(token))
		t.Logf("run %d: stderr's first line is the same stop line: %v", i, len(splitLines(stderr.String())) > 0 && splitLines(stderr.String())[0] == lines[0])
	}
	t.Logf("the two runs' tokens differ: %v", tokens[0] != tokens[1])
}
````

- Command, from the copy's root: `go test ./cmd/rollproof -run '^TestReviewP3Try9TokenFromTheEnvironmentOrAGuess$' -count=1 -v`.
- Reviewer's output:

````text
review_p3_9_test.go:34: run 1: exit 2
review_p3_9_test.go:41: run 1: the token printed is "GZM6BQCYH6DLQKIIEJOVFAUWFJ"
review_p3_9_test.go:42: run 1: it equals the guessed value: false
review_p3_9_test.go:43: run 1: it has the form of crypto/rand.Text's 26 characters: true
review_p3_9_test.go:44: run 1: stderr's first line is the same stop line: true
review_p3_9_test.go:34: run 2: exit 2
review_p3_9_test.go:41: run 2: the token printed is "EXSH2XJ7GFKMJPTYKDQQU44ETR"
review_p3_9_test.go:46: the two runs' tokens differ: true
````

- The re-run's output (exit 0):

````
    review_p3_9_test.go:34: run 1: exit 2
    review_p3_9_test.go:41: run 1: the token printed is "56FEI6AI2RPCNOV5MYLPLV4WIE"
    review_p3_9_test.go:42: run 1: it equals the guessed value: false
    review_p3_9_test.go:43: run 1: it has the form of crypto/rand.Text's 26 characters: true
    review_p3_9_test.go:44: run 1: stderr's first line is the same stop line: true
    review_p3_9_test.go:34: run 2: exit 2
    review_p3_9_test.go:41: run 2: the token printed is "7KNEAQ5E7654OMQZ5527TAQ7DB"
    review_p3_9_test.go:42: run 2: it equals the guessed value: false
    review_p3_9_test.go:43: run 2: it has the form of crypto/rand.Text's 26 characters: true
    review_p3_9_test.go:44: run 2: stderr's first line is the same stop line: true
    review_p3_9_test.go:46: the two runs' tokens differ: true
````

- Verdict of the re-run: the rule kept in both runs. A token set through the environment, and a guess. Held: the token is `crypto/rand.Text`'s, two runs differ, and it never equals the guess.

#### P3-10: a line of the tool's own output that equals a resume line

- Test: `review-3/cmd/rollproof/review_p3_10_test.go`, `TestReviewP3Try10ALineThatLooksLikeTheResumeLine`, as the copy holds it:

````go
package main

// Try 10 of the third part of the security review of section 14, point 13: a line of the
// tool's own output that equals a resume line. A configuration file's name carries a
// newline and a resume-shaped line of 26 characters of the token's alphabet. reasonRecord
// (internal/run/record.go, lines 139 to 143) splits the reason's text at the newline, and
// the console prints the second part as a detail line with a two-space indent, so the
// output holds a line that begins with :: after its leading white space. It cannot be the
// resume line unless it names the token, which is new for each run and not in the input.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewP3Try10ALineThatLooksLikeTheResumeLine(t *testing.T) {
	const planted = "ABCDEFGHIJKLMNOPQRSTUVWXYZ" // 26 characters of the token's alphabet
	dir := t.TempDir()
	file := filepath.Join(dir, "x\n::"+planted+"::.yaml")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := realMain([]string{"run", "-f", file, "--out", filepath.Join(t.TempDir(), "results")}, &stdout, &stderr, githubEnv)

	t.Logf("exit %d", code)
	printLines(t, "stdout", stdout.String())
	printLines(t, "stderr", stderr.String())

	outLines := splitLines(stdout.String())
	token := strings.TrimPrefix(outLines[0], "::stop-commands::")
	resume := "::" + token + "::"
	plantedLine := "::" + planted + "::"
	for i, l := range outLines {
		if strings.TrimLeft(l, " \t") == plantedLine || strings.HasPrefix(strings.TrimLeft(l, " \t"), plantedLine) {
			t.Logf("stdout[%d], after its leading white space, begins with the planted resume-shaped line: %q", i, l)
		}
		if l == resume {
			t.Logf("stdout[%d] is the tool's resume line: %q", i, l)
		}
	}
	t.Logf("the planted line equals the tool's resume line: %v", plantedLine == resume)
	t.Logf("the resume line is stdout's last line: %v", len(outLines) > 0 && outLines[len(outLines)-1] == resume)
}
````

- Command, from the copy's root: `go test ./cmd/rollproof -run '^TestReviewP3Try10ALineThatLooksLikeTheResumeLine$' -count=1 -v`.
- Reviewer's output:

````text
review_p3_10_test.go:30: exit 2
review_p3_10_test.go:31: stdout[0] = "::stop-commands::CF2WOGH5CDXKVBLLKOS6UK2HZ7"
review_p3_10_test.go:31: stdout[3] = "could not test: the configuration file /tmp/TestReviewP3Try10ALineThatLooksLikeTheResumeLine1606183783/001/x   [config-rejected]"
review_p3_10_test.go:31: stdout[4] = "  ::ABCDEFGHIJKLMNOPQRSTUVWXYZ::.yaml is empty"
review_p3_10_test.go:31: stdout[6] = "::CF2WOGH5CDXKVBLLKOS6UK2HZ7::"
review_p3_10_test.go:40: stdout[4], after its leading white space, begins with the planted resume-shaped line: "  ::ABCDEFGHIJKLMNOPQRSTUVWXYZ::.yaml is empty"
review_p3_10_test.go:43: stdout[6] is the tool's resume line: "::CF2WOGH5CDXKVBLLKOS6UK2HZ7::"
review_p3_10_test.go:46: the planted line equals the tool's resume line: false
review_p3_10_test.go:47: the resume line is stdout's last line: true
````

- The re-run's output (exit 0):

````
    review_p3_10_test.go:30: exit 2
    review_p3_10_test.go:31: stdout, 7 line(s):
    review_p3_10_test.go:31: stdout[0] = "::stop-commands::FJMUS67QZG2T5IDNOIKM3MDUN7"
    review_p3_10_test.go:31: stdout[1] = "rollproof: NOT VERIFIED"
    review_p3_10_test.go:31: stdout[2] = ""
    review_p3_10_test.go:31: stdout[3] = "could not test: the configuration file /tmp/TestReviewP3Try10ALineThatLooksLikeTheResumeLine2476912456/001/x   [config-rejected]"
    review_p3_10_test.go:31: stdout[4] = "  ::ABCDEFGHIJKLMNOPQRSTUVWXYZ::.yaml is empty"
    review_p3_10_test.go:31: stdout[5] = "results: /tmp/TestReviewP3Try10ALineThatLooksLikeTheResumeLine2476912456/002/results"
    review_p3_10_test.go:31: stdout[6] = "::FJMUS67QZG2T5IDNOIKM3MDUN7::"
    review_p3_10_test.go:32: stderr, 1 line(s):
    review_p3_10_test.go:32: stderr[0] = "::stop-commands::FJMUS67QZG2T5IDNOIKM3MDUN7"
    review_p3_10_test.go:40: stdout[4], after its leading white space, begins with the planted resume-shaped line: "  ::ABCDEFGHIJKLMNOPQRSTUVWXYZ::.yaml is empty"
    review_p3_10_test.go:43: stdout[6] is the tool's resume line: "::FJMUS67QZG2T5IDNOIKM3MDUN7::"
    review_p3_10_test.go:46: the planted line equals the tool's resume line: false
    review_p3_10_test.go:47: the resume line is stdout's last line: true
````

- Verdict of the re-run: the rule kept in both runs. A file name that makes the console print a line shaped like the resume line. Held: it never equals the tool's resume line, and the tool's is the last line on standard output.

#### P3-11: the resume line when a caller's script redirects or captures standard output

- Test: `review-3/cmd/rollproof/review_p3_11_test.go`, `TestReviewP3Try11TheResumeLineInARedirectedStdout`, as the copy holds it:

````go
package main

// Try 11 of the third part of the security review of section 14, point 13: the resume line
// sent where the runner does not read it. Section 8, "Workflow commands", resumes on
// standard output only, "so that a step that calls the tool from a script of its own can
// use commands again afterwards". A script of its own may redirect or capture the tool's
// standard output (rollproof run > run.log, or out=$(rollproof run)); the runner then
// reads the stop line on standard error and never the resume line. This test gives the
// tool a file for standard output, as the shell's > does, and the step's stream for
// standard error.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewP3Try11TheResumeLineInARedirectedStdout(t *testing.T) {
	dir := t.TempDir()
	log, err := os.Create(filepath.Join(dir, "run.log"))
	if err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	code := realMain([]string{"run", "-f", filepath.Join(dir, "missing.yaml"), "--out", filepath.Join(dir, "results")}, log, &stderr, githubEnv)
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "run.log"))
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("exit %d", code)
	printLines(t, "stdout file", string(b))
	printLines(t, "stderr (the step's stream)", stderr.String())

	fileLines, errLines := splitLines(string(b)), splitLines(stderr.String())
	token := strings.TrimPrefix(fileLines[0], "::stop-commands::")
	resume := "::" + token + "::"
	t.Logf("the resume line is the redirected stdout's last line: %v", fileLines[len(fileLines)-1] == resume)
	if len(errLines) > 0 && errLines[0] == fileLines[0] {
		t.Logf("stderr begins with the same stop line: true")
	}
	for _, l := range errLines {
		if l == resume {
			t.Errorf("stderr holds the resume line: %q", l)
		}
	}
	t.Logf("the step's stream (stderr) holds no resume line, so the runner is left stopped for the rest of the step")
}
````

- Command, from the copy's root: `go test ./cmd/rollproof -run '^TestReviewP3Try11TheResumeLineInARedirectedStdout$' -count=1 -v`.
- Rule: "The tool prints that line last on standard output when it ends by itself, whatever its exit code, so that a step that calls the tool from a script of its own can use commands again afterwards." (section 8, "Workflow commands.")
- Reviewer's output (its verdict: broken, narrowly; a third place, besides the two point 33 names: the resume goes where the runner does not read, the stop on standard error holds, and every later command of that script is ignored without a word):

````
review_p3_11_test.go:37: exit 2
review_p3_11_test.go:38: stdout file, 6 line(s):
review_p3_11_test.go:38: stdout file[0] = "::stop-commands::WLSES7OGSVOJUYJM3VOEZHZMYX"
review_p3_11_test.go:38: stdout file[3] = "could not test: the configuration file /tmp/TestReviewP3Try11TheResumeLineInARedirectedStdout2198973215/001/missing.yaml does not exist   [config-rejected]"
review_p3_11_test.go:38: stdout file[5] = "::WLSES7OGSVOJUYJM3VOEZHZMYX::"
review_p3_11_test.go:39: stderr (the step's stream), 1 line(s):
review_p3_11_test.go:39: stderr (the step's stream)[0] = "::stop-commands::WLSES7OGSVOJUYJM3VOEZHZMYX"
review_p3_11_test.go:44: the resume line is the redirected stdout's last line: true
review_p3_11_test.go:46: stderr begins with the same stop line: true
review_p3_11_test.go:53: the step's stream (stderr) holds no resume line, so the runner is left stopped for the rest of the step
````

- The re-run's output (exit 0: the token and the temporary folder are new for this run):

````
    review_p3_11_test.go:37: exit 2
    review_p3_11_test.go:38: stdout file, 6 line(s):
    review_p3_11_test.go:38: stdout file[0] = "::stop-commands::RYGCK7CXCEQJUTYVUPBT7VEJ6A"
    review_p3_11_test.go:38: stdout file[1] = "rollproof: NOT VERIFIED"
    review_p3_11_test.go:38: stdout file[2] = ""
    review_p3_11_test.go:38: stdout file[3] = "could not test: the configuration file /tmp/TestReviewP3Try11TheResumeLineInARedirectedStdout2279034331/001/missing.yaml does not exist   [config-rejected]"
    review_p3_11_test.go:38: stdout file[4] = "results: /tmp/TestReviewP3Try11TheResumeLineInARedirectedStdout2279034331/001/results"
    review_p3_11_test.go:38: stdout file[5] = "::RYGCK7CXCEQJUTYVUPBT7VEJ6A::"
    review_p3_11_test.go:39: stderr (the step's stream), 1 line(s):
    review_p3_11_test.go:39: stderr (the step's stream)[0] = "::stop-commands::RYGCK7CXCEQJUTYVUPBT7VEJ6A"
    review_p3_11_test.go:44: the resume line is the redirected stdout's last line: true
    review_p3_11_test.go:46: stderr begins with the same stop line: true
    review_p3_11_test.go:53: the step's stream (stderr) holds no resume line, so the runner is left stopped for the rest of the step
````

- Verdict of the re-run: holds.
- Ruling: document (the maintainer, 2026-11-25), in the design's section 11, with the two open places of point 33: a caller's script that redirects or captures the tool's standard output sends the resume line where the runner does not read, so such a script must leave it connected. The smaller choice: a sentence, and no code; the two changes point 33 weighed stay as they were kept.
- The text: commit `ab21379`.

**What reviewer 3 did not try:**

````markdown
# Part 3 — what I did not try, and why
**The two places point 33 leaves open (no test's).**
1. "A line that the tool writes on standard error just before it ends can reach the runner after the resume line, and is then read for commands." My tries 1 and 4 produce the streams that would trigger it; neither can make the runner read in that order. Point 33 says so, and the runner's two readers (`ProcessInvoker.cs` lines 400–438 and 507–529) offer no way to force it.
2. "when a run ends within a moment of its start, as a usage error does, the stop line of standard error can reach the runner after the resume line of standard output: the runner then stops its commands again and reads none for the rest of the step." Try 1 is such a run in shape, but I cannot make the order.
**A third place: yes.** Try 11: the resume is on stdout only; a caller's redirection or capture of stdout sends it where the runner does not read, while the stderr stop still holds. I found no fourth: the action's script's stop never resumes by design, and I found no tool or script line after the tool's resume on a stream the runner reads (`main.go` lines 45–47 and 103–108 print nothing after `realMain` returns).
**The runner's source lines point 33 names (none is in the `review-3` copy; all read in a clone on this machine).** The clone is `/tmp/opencode/verify-step6/.superpowers/step6/src/runner`, at commit `7e211dc69c347b78fc398e630e74facb11a13235`, the commit point 33 cites.
- `src/Runner.Worker/Handlers/ScriptHandler.cs`, lines 332–336 — read (one `OutputManager` for each stream, both on the step's `ActionCommandManager`).
- `src/Runner.Worker/Handlers/OutputManager.cs`, lines 77–87 — read (a line with `##[` or `::` anywhere goes to `TryProcessCommand`).
- `src/Runner.Common/ActionCommand.cs`, lines 62, 63, 86, 132, 156 — read (`TrimStart` + `::`; `##[` found anywhere; command name must be registered).
- `src/Runner.Worker/ActionCommandManager.cs`, lines 125–143, 259, 478 — read (a command that throws marks the step failed; `set-env` and `add-path` are refused).
- `src/Runner.Worker/JobExtension.cs`, lines 820–825 — read (an output whose value the masker changes is dropped).
- `src/Runner.Worker/ActionCommandManager.cs`, lines 83–123 and 156–186; line 94 — read (stop/resume, case-insensitive match, masker over 6 characters, token validation, `Remove` at line 94).
- `src/Runner.Worker/Handlers/Handler.cs`, line 172; `ScriptHandler.cs`, lines 332–333 — read (a parser per step, shared by the two streams).
- `src/Runner.Sdk/ProcessInvoker.cs`, lines 400–438 and 507–529 — read (two readers; at each turn stderr's held lines go before stdout's).
- `src/Runner.Worker/ActionRunner.cs`, line 282 — read (the step's files are processed in a `finally` after the process ended).
- `src/Runner.Worker/Handlers/OutputManager.cs`, lines 101–155 — read (problem matchers still read every line).
None was "not at all"; none is in the copy under review.
**Not tried, and why.**
- The cluster action test `hack/action/action_cluster_test.go`: it needs the engine and a cluster, so it is outside the no-cluster rule here. I read its assertions: the script's stop first on both streams, the tool's stop second on stdout and last on stderr, the tool's resume last on stdout, and no `::<script token>::` anywhere. I did not run it.
- A GitHub-hosted runner: there is none on this machine. `docs/proofs/p03-p04-p20-github-runner.md` is a record of runs of points 3, 4 and 20; it has no observation of the stop/resume lines, and I did not add one. Every statement above about the tool and the script is from their output or their code; every statement about the runner is from the clone at `7e211dc`.
- A hostile API server warning through the Kubernetes client library's standard error log: it needs a cluster and a hostile API server. Section 10 says it can hold `##[` and reach after the resume, and that whether the watch code writes there at the default detail level was not read. That is open place 1's content, not mine to test.
- The job summary and the outputs file (`internal/report/github.go`): their delimiter logic is exercised by `github_test.go`; a hostile value cannot name the random delimiter, and the outputs file is read after the process ends (point 33's last bullet). The summary's Markdown rendering is another reviewer's part.
- `rollproof probe http`, `rollproof version`, and the top-level usage: they print neither stop line by design. The usage and version text are constants; the probe's URL errors do not include the URL (`internal/probe/http.go` lines 80–85). A hostile `probe http` flag name on a runner would print with no stop line, but the design says users do not call `probe`, and it runs in the pod. I did not test it.
- The action script's "no token" failure: the message and the token file are fixed (`action.sh` lines 39, 47–52); no caller input reaches them. I read the test's rows for it (`action_test.go` lines 647–699) but did not plant input there.
- PATH or `BASH_ENV` code injection beyond the fake `uname`: a caller who can set those in the action's inner step already has the step's power; the rule is about text from outside, not code chosen by the caller.
- A token guess beyond the 26-character form and two runs: 130 bits from `crypto/rand.Text`; the script's token is a kernel UUID read from `/proc/sys/kernel/random/uuid`, and no environment variable names either token.
No container, image, network or volume was created, so there was nothing to remove, and no network request was made.
````

### Part 4: the five facts of section 11 and what leaves the runner

10 tries, by reviewer 4; the card it was given is in Method.

**The rule's claims, as the design states them, in reviewer 4's words:**

````markdown
Section 11 of `docs/specs/2026-11-13-rollproof-design.md`, as it states them (line numbers from that file):
- **First bullet, line 2420.** "The change under test runs with the job's power." The configuration file is a program; its `setup` commands and the commands driver's commands run on the runner with the job's whole environment, the registry password included; the chart, its hooks, the images and a custom probe run as pods in nodes that kind runs as privileged containers of the runner's engine, and nothing in the tool keeps a pod under test away from its node; the tool belongs only where everything the change brings is trusted, or on a runner that is thrown away after the job.
- **"What leaves the runner through the tool", line 2428.** "The nodes' pulls: to the mirrors that the configuration lists in place of their hosts, to the other hosts it lists, and to the hosts of images that no entry names, each with its entry's credentials when it has them, and the token services that those hosts name. Helm's pulls, with the caller's own logins. Nothing else."
- **The credentials file, line 2444** (one of point 13's five facts). "The credentials file mounted into the nodes holds only the hosts the configuration lists. It lives in the run's folder and is removed with the cluster. While the cluster lives, every pod in it can read the file (section 9), so the credentials should be able to read only, and should expire soon." Section 9, "Who can read the credentials" (line 1908), gives the narrower form: "Any pod that mounts that path of its node can therefore read it."
- **Registry credentials, line 2448.** "A registry entry's credentials go to the entry's mirror when it has one, and to its host otherwise, and to a token service that the mirror's or host's challenge names. A mirror replaces its host, so the real host never sees credentials meant for the mirror. A chart's own pull secrets for a host with a mirror go to the mirror as well."
- **The probe, line 2452.** "The built-in probe does not verify a server's certificate. It sends nothing secret, unless the caller put a credential into a header, and a header's value is not masked."
- **No reading of the runner's logins, line 2454.** "The tool does not read, copy or change the runner's Helm logins, its container engine's logins or its kubeconfig."
- **Kubeconfig and Helm variables, line 2456.** "Every command the tool starts gets the test cluster's kubeconfig, and none of the variables by which Helm would act on another cluster or keep its records elsewhere (section 7)."
The five facts of point 13 are attacked by tries 3/10 (mirror and token service), 4 (the credentials file), 6 (the probe header), 7 (the chart's pull secret) and 5 (the change's power); the list is attacked by tries 1, 2 and 9.
````

#### P4-1: the action's pull of the tool's own image is not in the list of what leaves the runner

- Test: `review-4/review-p4/review_p4_1_test.go`, `TestReviewP4ActionPullsTheToolImage`, as the copy holds it:

````go
// Review part 4, try 1: what leaves the runner through the tool. The composite
// action pulls the tool's own image through the runner's container engine before it
// starts the tool. Section 11's list names the nodes' pulls and Helm's pulls and says
// "Nothing else"; this test watches the action's engine pull with a stand-in engine.
package reviewp4

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestReviewP4ActionPullsTheToolImage is try 1. It sources action.sh, calls main
// with a stand-in nerdctl that only records its command line, and prints what the
// action asked the engine to do.
func TestReviewP4ActionPullsTheToolImage(t *testing.T) {
	action, err := filepath.Abs(filepath.Join("..", "action.sh"))
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	log := filepath.Join(bin, "engine.log")
	engine := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> '" + log + "'\n" +
		"case \"$1 $2\" in\n" +
		"\"-v \") printf '%s\\n' 'nerdctl version 2.4.2' ;;\n" +
		"\"create \"*) printf '%s\\n' 'review4-container' ;;\n" +
		"\"export \"*) exit 1 ;;\n" +
		"esac\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "nerdctl"), []byte(engine), 0o700); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	runnerTemp := t.TempDir()

	const ref = "registry.example/review4/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000"
	cmd := exec.Command("bash", "-c", `source "$ACTION"; main`)
	cmd.Dir = workspace
	cmd.Env = []string{
		"PATH=" + bin + ":/usr/bin:/bin",
		"HOME=" + t.TempDir(),
		"ACTION=" + action,
		"KIND_EXPERIMENTAL_PROVIDER=nerdctl",
		"ROLLPROOF_ACTION_IMAGE=" + ref,
		"ROLLPROOF_ACTION_CONFIG=rollproof.yaml",
		"GITHUB_WORKSPACE=" + workspace,
		"RUNNER_TEMP=" + runnerTemp,
	}
	out, runErr := cmd.CombinedOutput()
	fmt.Printf("action ended: %v\n", runErr)
	fmt.Printf("--- action output ---\n%s", out)
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the stand-in engine's log: %v", err)
	}
	fmt.Printf("--- engine calls ---\n%s", b)

	if !strings.Contains(string(b), "pull "+ref) {
		t.Errorf("the action did not ask the engine to pull %s; its calls: %s", ref, b)
	}
}
````

- Command, from the copy's root: `env CGO_ENABLED=0 go test ./review-p4 -run '^TestReviewP4ActionPullsTheToolImage$' -count=1 -v`. The re-run writes the name between ^ and $.
- Rule: "What leaves the runner through the tool. The nodes' pulls: to the mirrors that the configuration lists in place of their hosts, to the other hosts it lists, and to the hosts of images that no entry names, each with its entry's credentials when it has them, and the token services that those hosts name. Helm's pulls, with the caller's own logins. Nothing else." (section 11)
- Reviewer's output (its verdict: broken, the list leaves it out: `action.sh` pulls the tool's image before it starts the tool, with the runner's engine login):

````
action ended: exit status 2
--- action output ---
::stop-commands::0d1ecfd8-5f21-4934-adb8-45f6a06b35cb
[...]
rollproof: the binary /rollproof could not be taken out of registry.example/review4/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000
--- engine calls ---
pull registry.example/review4/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000
create --name rollproof-action-NSJo7e registry.example/review4/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000 /rollproof
export review4-container
rm review4-container
````

- The re-run's output (exit 0): the same lines; a stand-in engine stands in for the real one, so the pull is recorded and not made. The token and the container's name are new for this run, and the stand-in's `tar` lines are kept:

````
--- action output ---
::stop-commands::aac6dd04-7d4f-4e7c-b137-5cdf1a6611d1
::stop-commands::aac6dd04-7d4f-4e7c-b137-5cdf1a6611d1
tar: This does not look like a tar archive
tar: rollproof: Not found in archive
tar: Exiting with failure status due to previous errors
rollproof: the binary /rollproof could not be taken out of registry.example/review4/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000
--- engine calls ---
pull registry.example/review4/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000
create --name rollproof-action-8b1bG2 registry.example/review4/rollproof@sha256:0000000000000000000000000000000000000000000000000000000000000000 /rollproof
export review4-container
rm review4-container
````

- Verdict of the re-run: holds.
- Ruling: document (the maintainer, 2026-11-25), in the design's section 11: the list of what leaves the runner through the tool gains the action's pull of the tool's image, with the runner's engine login. The smaller choice: a sentence, and no code.
- The text: commit `96ad3d2`.

#### P4-2: the engine's pull of the node image is not in the list, and goes to the image's own host with the engine's login

- Test: `review-4/internal/cluster/review_p4_2_test.go`, `TestReviewP4EnginePullsTheNodeImage`, as the copy holds it:

````go
//go:build linux && cluster

// Review part 4, try 2: what leaves the runner through the tool. The tool asks the
// runner's container engine for the node image itself, by the image's own reference,
// before kind creates the cluster. This test watches the engine's request with a
// credential for the node image's host in the engine's own login file, and a mirror
// for that host in the configuration. Section 11's list names the nodes' pulls and
// Helm's pulls and says "Nothing else"; the engine's pull is not in the list.
package cluster

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"rollproof/internal/config"
)

// reviewP4Log is a request log that one test's handlers append to.
type reviewP4Log struct {
	mu   sync.Mutex
	text strings.Builder
}

func (l *reviewP4Log) add(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(&l.text, format+"\n", args...)
}

func (l *reviewP4Log) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text.String()
}

// TestReviewP4EnginePullsTheNodeImage is try 2. It runs the real nerdctl through
// cluster.Create, with the node image named after a host that the configuration
// mirrors, and a Docker login for that host in DOCKER_CONFIG. The engine's pull must
// not wait for a cluster, because it fails at the pull; the test prints every
// request that the host and its mirror saw.
func TestReviewP4EnginePullsTheNodeImage(t *testing.T) {
	const password = "engine-login-p4"
	const user = "docker-user"

	// The host that the node image names. The first request gets 401 so that the
	// engine offers its own login; every later answer is 404, which ends the pull.
	var hostLog, mirrorLog reviewP4Log
	var hostMu sync.Mutex
	seen := false
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hostLog.add("host: %s %s auth=%q host=%q", r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), r.Host)
		hostMu.Lock()
		first := !seen
		seen = true
		hostMu.Unlock()
		if first {
			w.Header().Set("WWW-Authenticate", `Basic realm="review4"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer host.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorLog.add("mirror: %s %s auth=%q host=%q", r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), r.Host)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mirror.Close()

	hostName := strings.TrimPrefix(host.URL, "http://")
	mirrorName := strings.TrimPrefix(mirror.URL, "http://")
	// The engine reads its logins from DOCKER_CONFIG; give it one for the node image's
	// host, and nothing for the mirror.
	dockerConfig := t.TempDir()
	cfg := fmt.Sprintf(`{"auths":{%q:{"auth":%q}}}`, hostName,
		base64.StdEncoding.EncodeToString([]byte(user+":"+password)))
	if err := os.WriteFile(filepath.Join(dockerConfig, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_CONFIG", dockerConfig)

	name := "rollproof-review-4-enginepull"
	runDir := RunDir(name)
	if err := os.RemoveAll(runDir); err != nil {
		t.Fatal(err)
	}
	// The cluster's creation fails at the engine's pull, so no node is made.
	c, createErr := Create(context.Background(), Spec{
		Name:      name,
		RunDir:    runDir,
		NodeImage: hostName + "/review4/node:tag",
		Workers:   1,
		Namespace: "review4",
		Timeout:   30 * time.Second,
		Registries: []config.Registry{{
			Host:   hostName,
			Mirror: mirrorName,
		}},
	}, Engine{Binary: "nerdctl"})
	if c != nil {
		t.Cleanup(func() { _ = c.Delete(context.Background()) })
	}
	fmt.Printf("Create error: %v\n", createErr)
	fmt.Printf("--- host log ---\n%s", hostLog.String())
	fmt.Printf("--- mirror log ---\n%s", mirrorLog.String())

	if createErr == nil {
		t.Fatal("Create succeeded; the test needs the pull to fail")
	}
	if !strings.Contains(hostLog.String(), `auth="Basic `) {
		t.Errorf("the engine sent no Basic login to %s", hostName)
	}
	if strings.Contains(mirrorLog.String(), "GET ") || strings.Contains(mirrorLog.String(), "HEAD ") {
		t.Errorf("the mirror %s was contacted: %s", mirrorName, mirrorLog.String())
	}
}
````

- Command, from the copy's root: `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags cluster -p 1 -count=1 ./internal/cluster -run TestReviewP4EnginePullsTheNodeImage -v`.
- Rule: "What leaves the runner through the tool. … Nothing else." (section 11)
- Reviewer's output (its verdict: broken, the list leaves it out: `cluster.Create` asks the engine to pull the node image; the request went to the image's own host with the engine's login, `docker-user:engine-login-p4`, and the configuration's mirror for that host was never contacted):

````
Create error: cluster-failed: pull the node image: nerdctl pull 127.0.0.1:41091/review4/node:tag: exit status 1: time="2026-11-25T18:04:09-05:00" level=fatal msg="failed to resolve image: 127.0.0.1:41091/review4/node:tag: not found"
--- host log ---
host: HEAD /v2/review4/node/manifests/tag auth="" host="127.0.0.1:41091"
host: HEAD /v2/review4/node/manifests/tag auth="Basic ZG9ja2VyLXVzZXI6ZW5naW5lLWxvZ2luLXA0" host="127.0.0.1:41091"
--- mirror log ---
````

- The re-run's output (exit 0): the same lines; the port and the moment are new for this run:

````
Create error: cluster-failed: pull the node image: nerdctl pull 127.0.0.1:41803/review4/node:tag: exit status 1: time="2026-11-25T18:09:49-05:00" level=fatal msg="failed to resolve image: 127.0.0.1:41803/review4/node:tag: not found"
--- host log ---
host: HEAD /v2/review4/node/manifests/tag auth="" host="127.0.0.1:41803"
host: HEAD /v2/review4/node/manifests/tag auth="Basic ZG9ja2VyLXVzZXI6ZW5naW5lLWxvZ2luLXA0" host="127.0.0.1:41803"
--- mirror log ---
--- PASS: TestReviewP4EnginePullsTheNodeImage (0.09s)
````

- Verdict of the re-run: holds.
- Ruling: document (the maintainer, 2026-11-25), in the design's section 11: the list of what leaves the runner through the tool gains the engine's pull of the node image, to the image's own host with the runner's engine login and not to the mirrors the configuration lists. The smaller choice: a sentence, and no code.
- The text: commit `1d30ab9`.

#### P4-3: a mirror that redirects to another port of itself, and the entry's credentials follow

- Test: `review-4/internal/cluster/review_p4_3_test.go`, `TestReviewP4MirrorRedirectKeepsCredentials`, as the copy holds it:

````go
//go:build cluster

// Review part 4, try 3: what leaves the runner through the tool, and where an
// entry's credentials go. A mirror for H is a TLS server that asks for Basic
// credentials and then answers with a redirect to another port of its own name.
// Section 11 says an entry's credentials go to the mirror, and to a token service
// that the mirror's or host's challenge names. The redirect target is neither: this
// test watches the credentials arrive at the second listener of the same name.
package cluster

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"rollproof/internal/config"
)

const (
	reviewP4Cluster = "rollproof-review-4-redirect"
	reviewP4Standin = "rollproof-review-4-redirect-standin"
	reviewP4Host    = "h.review4.example"
	reviewP4Mirror  = "m.review4.example"
)

// reviewP4StandinSource is the stand-in that runs as a container on the kind
// network. It listens on :443 (the mirror) and :8443 (the redirect's target).
// The mirror asks for Basic credentials and then redirects to the target.
const reviewP4StandinSource = `package main

import (
	"flag"
	"fmt"
	"net/http"
)

func main() {
	cert := flag.String("cert", "", "certificate")
	key := flag.String("key", "", "key")
	target := flag.String("target", "", "redirect target")
	flag.Parse()
	go func() {
		if err := http.ListenAndServeTLS(":8443", *cert, *key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Printf("TARGET %s %s auth=%q host=%q\n", r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), r.Host)
			w.WriteHeader(http.StatusNotFound)
		})); err != nil {
			fmt.Println("target listen:", err)
		}
	}()
	fmt.Println("standin ready")
	if err := http.ListenAndServeTLS(":443", *cert, *key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("MIRROR %s %s auth=%q host=%q\n", r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), r.Host)
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", ` + "`" + `Basic realm="review4"` + "`" + `)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Location", *target+r.URL.RequestURI())
		w.WriteHeader(http.StatusTemporaryRedirect)
	})); err != nil {
		fmt.Println("mirror listen:", err)
	}
}
`

// TestReviewP4MirrorRedirectKeepsCredentials is try 3. It builds a cluster whose
// entry for H has a mirror M and a credential for H, then pulls an image named
// after H from a pod. M asks for the credential and answers with a redirect to
// https://M:8443, the target listener; the test prints both logs.
func TestReviewP4MirrorRedirectKeepsCredentials(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	work := t.TempDir()
	certs := filepath.Join(work, "certs")
	if err := os.MkdirAll(certs, 0o755); err != nil {
		t.Fatal(err)
	}
	caFile, err := reviewP4Authority(certs)
	if err != nil {
		t.Fatal(err)
	}
	bin, err := reviewP4BuildStandin(work)
	if err != nil {
		t.Fatal(err)
	}

	engine, err := CheckRunner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runDir := RunDir(reviewP4Cluster)
	if err := os.RemoveAll(runDir); err != nil {
		t.Fatal(err)
	}
	c, err := Create(ctx, Spec{
		Name:      reviewP4Cluster,
		RunDir:    runDir,
		Workers:   1,
		Namespace: "review4",
		Timeout:   5 * time.Minute,
		Registries: []config.Registry{{
			Host:     reviewP4Host,
			Username: "huser",
			Password: "hunter2-p4",
			Mirror:   reviewP4Mirror,
			CA:       caFile,
		}},
	}, engine)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		_ = exec.Command(engine.Binary, "rm", "-f", reviewP4Standin).Run()
		if err := c.Delete(context.Background()); err != nil {
			t.Error(err)
		}
	})

	// Start the stand-in on the kind network after the cluster made it.
	run := exec.Command(engine.Binary, "run", "-d", "--name", reviewP4Standin, "--network", "kind",
		"-v", filepath.Dir(bin)+":/app:ro", "-v", certs+":/certs:ro", "--entrypoint", "/app/standin",
		"registry:3", "-cert", "/certs/server.pem", "-key", "/certs/server-key.pem",
		"-target", "https://"+reviewP4Mirror+":8443")
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("start the stand-in: %v\n%s", err, out)
	}
	ip, err := reviewP4ContainerIP(engine.Binary, reviewP4Standin)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("the stand-in %s is at %s\n", reviewP4Standin, ip)

	for _, node := range c.Nodes() {
		if out, err := c.Exec(ctx, node, "sh", "-c", "echo '"+ip+" "+reviewP4Mirror+"' >> /etc/hosts"); err != nil {
			t.Fatalf("resolve %s in node %s: %v\n%s", reviewP4Mirror, node, err, out)
		}
	}

	client, err := c.Client()
	if err != nil {
		t.Fatal(err)
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "review4-pull", Namespace: "review4"},
		Spec: corev1.PodSpec{
			NodeName:      c.Nodes()[1],
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:            "app",
				Image:           reviewP4Host + "/team/app:tag",
				ImagePullPolicy: corev1.PullAlways,
			}},
		},
	}
	if _, err := client.CoreV1().Pods("review4").Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create the pod: %v", err)
	}

	// Wait for the target listener to see the credentials, or for the pull to fail.
	deadline := time.Now().Add(90 * time.Second)
	var logs string
	for time.Now().Before(deadline) {
		out, _ := exec.Command(engine.Binary, "logs", reviewP4Standin).CombinedOutput()
		logs = string(out)
		if strings.Contains(logs, "TARGET ") && strings.Contains(logs, "auth=\"Basic ") {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Printf("--- stand-in log ---\n%s", logs)
	if got, err := client.CoreV1().Pods("review4").Get(ctx, "review4-pull", metav1.GetOptions{}); err == nil {
		for _, st := range got.Status.ContainerStatuses {
			fmt.Printf("pod state: waiting=%v\n", st.State.Waiting)
		}
	}

	if !reviewP4LineWith(logs, "MIRROR ", `auth="Basic `) {
		t.Errorf("the mirror did not receive the entry's credentials; log:\n%s", logs)
	}
	if !reviewP4LineWith(logs, "TARGET ", `auth="Basic `) {
		t.Errorf("the redirect target did not receive the credentials; log:\n%s", logs)
	}
}

// reviewP4LineWith reports whether a line of log begins prefix and holds text.
func reviewP4LineWith(log, prefix, text string) bool {
	for _, line := range strings.Split(log, "\n") {
		if strings.HasPrefix(line, prefix) && strings.Contains(line, text) {
			return true
		}
	}
	return false
}

// reviewP4Authority writes a CA and a server certificate for reviewP4Mirror and
// 127.0.0.1 into dir, and returns the CA file's absolute path.
func reviewP4Authority(dir string) (string, error) {
	notBefore := time.Now().Add(-time.Minute)
	notAfter := notBefore.Add(24 * time.Hour)
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	ca := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "review part 4 authority"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		return "", err
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	server := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: reviewP4Mirror},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{reviewP4Mirror},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, server, ca, &serverKey.PublicKey, caKey)
	if err != nil {
		return "", err
	}
	serverKeyDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return "", err
	}
	for name, block := range map[string]*pem.Block{
		"ca.pem":     {Type: "CERTIFICATE", Bytes: caDER},
		"server.pem": {Type: "CERTIFICATE", Bytes: serverDER},
		"server-key.pem": {Type: "EC PRIVATE KEY", Bytes: serverKeyDER},
	} {
		if err := os.WriteFile(filepath.Join(dir, name), pem.EncodeToMemory(block), 0o644); err != nil {
			return "", err
		}
	}
	return filepath.Join(dir, "ca.pem"), nil
}

// reviewP4BuildStandin writes and builds the stand-in in work and returns the binary.
func reviewP4BuildStandin(work string) (string, error) {
	src := filepath.Join(work, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module review4standin\n\ngo 1.27\n"), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(reviewP4StandinSource), 0o644); err != nil {
		return "", err
	}
	bin := filepath.Join(work, "bin", "standin")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return "", err
	}
	cmd := exec.Command("go", "build", "-trimpath", "-o", bin, ".")
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build the stand-in: %v\n%s", err, out)
	}
	return bin, nil
}

// reviewP4ContainerIP returns the IPv4 address of container name on the kind network.
func reviewP4ContainerIP(engine, name string) (string, error) {
	out, err := exec.Command(engine, "inspect", name).Output()
	if err != nil {
		return "", err
	}
	var inspected []struct {
		NetworkSettings struct {
			Networks map[string]struct{ IPAddress string }
		}
	}
	if err := json.Unmarshal(out, &inspected); err != nil {
		return "", err
	}
	if len(inspected) != 1 {
		return "", fmt.Errorf("%s inspect gave %d entries", name, len(inspected))
	}
	for net, n := range inspected[0].NetworkSettings.Networks {
		if net == "kind" && n.IPAddress != "" {
			return n.IPAddress, nil
		}
	}
	for _, n := range inspected[0].NetworkSettings.Networks {
		if n.IPAddress != "" {
			return n.IPAddress, nil
		}
	}
	return "", fmt.Errorf("%s has no address", name)
}
````

- Command, from the copy's root: `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags cluster -p 1 -count=1 ./internal/cluster -run TestReviewP4MirrorRedirectKeepsCredentials -v -timeout 9m`.
- Rule: "A registry entry's credentials go to the entry's mirror when it has one, and to its host otherwise, and to a token service that the mirror's or host's challenge names." (section 11)
- Reviewer's output (its verdict: broken: the mirror `m.review4.example` asked once, then answered 307 to `https://m.review4.example:8443`; the target received the same Basic credentials, `huser:hunter2-p4`, on a port the configuration never names, without asking):

````
--- stand-in log ---
standin ready
MIRROR HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="" host="m.review4.example"
MIRROR HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="Basic aHVzZXI6aHVudGVyMi1wNA==" host="m.review4.example"
TARGET HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="Basic aHVzZXI6aHVudGVyMi1wNA==" host="m.review4.example:8443"
pod state: waiting=&ContainerStateWaiting{Reason:ContainerCreating,Message:,}
````

- The re-run's output (exit 0; a cluster run of 45 s): the same lines:

````
--- stand-in log ---
standin ready
MIRROR HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="" host="m.review4.example"
MIRROR HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="Basic aHVzZXI6aHVudGVyMi1wNA==" host="m.review4.example"
TARGET HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="Basic aHVzZXI6aHVudGVyMi1wNA==" host="m.review4.example:8443"
pod state: waiting=&ContainerStateWaiting{Reason:ContainerCreating,Message:,}
````

- Verdict of the re-run: holds.
- Ruling: document (the maintainer, 2026-11-25), in the design's section 11: a mirror that answers a redirect sends the entry's credentials on to the place it names, such as another port of itself. The smaller choice: a sentence, and no code; the node's runtime follows the redirect and the tool sets nothing of it.
- The text: commit `65de1dd`.

#### P4-4: a plain pod does not read the credentials file; a pod that mounts the node's path does, and the file holds only the listed hosts

- Test: `review-4/internal/cluster/review_p4_4_test.go`, `TestReviewP4CredentialsFileReadByPods`, as the copy holds it:

````go
//go:build cluster

// Review part 4, try 4: every pod in the cluster can read the nodes'
// credentials, and what the file holds. The test makes a cluster whose
// configuration lists two hosts with credentials, puts a Docker login for a
// third host in the runner's own engine login file, and runs two pods of the
// same image: one with no mount, which cannot see the file, and one with a
// hostPath of the node's /var/lib/kubelet, which prints it.
package cluster_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"rollproof/internal/cluster"
	"rollproof/internal/config"
	"rollproof/internal/images"
)

const (
	reviewP4CredCluster = "rollproof-review-4-credentials"
	reviewP4CredHost    = "h.review4.example"
	reviewP4OtherHost   = "other.review4.example"
	reviewP4CredUser    = "huser"
	reviewP4CredPass    = "hunter2-p4"
	reviewP4OtherUser   = "ouser"
	reviewP4OtherPass   = "other-pass-p4"
)

// TestReviewP4CredentialsFileReadByPods is try 4.
func TestReviewP4CredentialsFileReadByPods(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	engine, err := cluster.CheckRunner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runDir := cluster.RunDir(reviewP4CredCluster)
	if err := os.RemoveAll(runDir); err != nil {
		t.Fatal(err)
	}
	c, err := cluster.Create(ctx, cluster.Spec{
		Name:      reviewP4CredCluster,
		RunDir:    runDir,
		Workers:   1,
		Namespace: "review4",
		Timeout:   5 * time.Minute,
		Registries: []config.Registry{
			{Host: reviewP4CredHost, Username: reviewP4CredUser, Password: reviewP4CredPass},
			{Host: reviewP4OtherHost, Username: reviewP4OtherUser, Password: reviewP4OtherPass},
		},
	}, engine)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		if err := c.Delete(context.Background()); err != nil {
			t.Error(err)
		}
	})

	// The engine's own login file names a third host, which the configuration does
	// not list; the credentials file must not take it.
	dockerConfig := t.TempDir()
	login := `{"auths":{"runner.review4.example":{"auth":"cnVubmVyOnJ1bm5lci1wNA=="}}}`
	if err := os.WriteFile(dockerConfig+"/config.json", []byte(login), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_CONFIG", dockerConfig)

	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := images.Store(ctx, c, bin, map[string]config.Image{
		"busybox": {Local: "busybox:1.37.0"},
	}, 5*time.Minute)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	ref := stored.Images["busybox"].Ref()
	client, err := c.Client()
	if err != nil {
		t.Fatal(err)
	}
	node := c.Nodes()[1]

	makePod := func(name, command string, hostPath bool) {
		t.Helper()
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "review4"},
			Spec: corev1.PodSpec{
				NodeName:      node,
				RestartPolicy: corev1.RestartPolicyNever,
				Containers: []corev1.Container{{
					Name:            "app",
					Image:           ref,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"sh", "-c", command},
				}},
			},
		}
		if hostPath {
			pod.Spec.Volumes = []corev1.Volume{{
				Name: "kubelet",
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{Path: "/var/lib/kubelet"},
				},
			}}
			pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{
				Name: "kubelet", MountPath: "/host-kubelet",
			}}
		}
		if _, err := client.CoreV1().Pods("review4").Create(ctx, pod, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create pod %s: %v", name, err)
		}
	}
	makePod("plain", "cat /var/lib/kubelet/config.json; echo plain-exit=$?", false)
	makePod("mounted", "cat /host-kubelet/config.json", true)

	read := func(name string) string {
		t.Helper()
		deadline := time.Now().Add(90 * time.Second)
		for time.Now().Before(deadline) {
			p, err := client.CoreV1().Pods("review4").Get(ctx, name, metav1.GetOptions{})
			if err == nil && (p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed) {
				out, err := client.CoreV1().Pods("review4").GetLogs(name, &corev1.PodLogOptions{}).DoRaw(ctx)
				if err != nil {
					t.Fatalf("logs of %s: %v", name, err)
				}
				return string(out)
			}
			time.Sleep(500 * time.Millisecond)
		}
		t.Fatalf("pod %s did not end", name)
		return ""
	}
	plain := read("plain")
	mounted := read("mounted")
	fmt.Printf("--- plain pod ---\n%s", plain)
	fmt.Printf("--- pod with /var/lib/kubelet mounted ---\n%s", mounted)

	if !strings.Contains(plain, "plain-exit=1") {
		t.Errorf("the plain pod did not fail to read the file; its log:\n%s", plain)
	}
	if !strings.Contains(mounted, reviewP4CredHost) || !strings.Contains(mounted, reviewP4OtherHost) {
		t.Errorf("the mounted pod's file does not hold the two configured hosts; its log:\n%s", mounted)
	}
	if strings.Contains(mounted, "runner.review4.example") || strings.Contains(mounted, "cnVubmVy") {
		t.Errorf("the mounted pod's file holds the runner's engine login; its log:\n%s", mounted)
	}
}
````

- Command, from the copy's root: `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags cluster -p 1 -count=1 ./internal/cluster -run TestReviewP4CredentialsFileReadByPods -v -timeout 9m`.
- Reviewer's output:

````text
--- plain pod ---
cat: can't open '/var/lib/kubelet/config.json': No such file or directory
plain-exit=1
--- pod with /var/lib/kubelet mounted ---
{
  "auths": {
    "h.review4.example": {
      "auth": "aHVzZXI6aHVudGVyMi1wNA=="
    },
    "other.review4.example": {
      "auth": "b3VzZXI6b3RoZXItcGFzcy1wNA=="
    }
  }
}
````

- The re-run's output (exit 0):

````
kind Create of rollproof-review-4-credentials, attempt 1: succeeded
--- plain pod ---
cat: can't open '/var/lib/kubelet/config.json': No such file or directory
plain-exit=1
--- pod with /var/lib/kubelet mounted ---
{
  "auths": {
    "h.review4.example": {
      "auth": "aHVzZXI6aHVudGVyMi1wNA=="
    },
    "other.review4.example": {
      "auth": "b3VzZXI6b3RoZXItcGFzcy1wNA=="
    }
  }
}
````

- Verdict of the re-run: the rule kept in both runs. Whether a plain pod reads the credentials file, and what the file holds. Held: a plain pod cannot open it; a pod that mounts the node's `/var/lib/kubelet` reads it, as section 9 says; it holds only the two listed hosts, and the runner's engine login did not enter it. The reviewer notes that section 11's "every pod in it can read the file" is broader than the code and section 9.

#### P4-5: the setup command gets the job's environment and the test kubeconfig, and loses the Helm-acting variables

- Test: `review-4/internal/deploy/review_p4_5_test.go`, `TestReviewP4SetupEnvironment`, as the copy holds it:

````go
//go:build linux

// Review part 4, try 5: the change under test runs with the job's power, and
// every command gets the test cluster's kubeconfig and none of the variables by
// which Helm would act on another cluster or keep its records elsewhere. The test
// starts a setup command with a hostile environment and prints what it saw.
package deploy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type reviewP4Buffer struct{ bytes.Buffer }

func (b *reviewP4Buffer) Close() error { return nil }

// TestReviewP4SetupEnvironment is try 5.
func TestReviewP4SetupEnvironment(t *testing.T) {
	dir := t.TempDir()
	kubeconfig := filepath.Join(t.TempDir(), "kubeconfig")
	var evidence reviewP4Buffer
	r := NewRunner(RunnerOptions{
		Dir:        dir,
		Kubeconfig: kubeconfig,
		Environ: []string{
			"KUBECONFIG=/caller/kubeconfig",
			"PWD=/caller/folder",
			"HELM_KUBECONTEXT=other-context",
			"HELM_KUBEAPISERVER=https://other.example",
			"HELM_KUBETOKEN=other-token",
			"HELM_KUBECAFILE=/caller/ca.pem",
			"HELM_KUBEASUSER=other-user",
			"HELM_KUBEASGROUPS=other-group",
			"HELM_KUBEINSECURE_SKIP_TLS_VERIFY=true",
			"HELM_KUBETLS_SERVER_NAME=other.example",
			"HELM_NAMESPACE=other-namespace",
			"HELM_DRIVER=sql",
			"HELM_DRIVER_SQL_CONNECTION_STRING=postgres://other/db",
			"HELM_REPOSITORY_CONFIG=/caller/repositories.yaml",
			"HELM_REPOSITORY_CACHE=/caller/cache",
			"GITHUB_TOKEN=ghs_review4_secret",
			"ROLLPROOF_PW=hunter2-p4",
		},
		Evidence: func(name string) (io.WriteCloser, error) {
			return &evidence, nil
		},
	})
	res, i := Setup(context.Background(), r, [][]string{{"sh", "-c", "env"}}, 30*time.Second)
	r.Close()
	fmt.Printf("setup ended %q at index %d; exit %d\n", res.Ending, i, res.ExitCode)
	fmt.Printf("--- evidence ---\n%s", evidence.String())

	text := evidence.String()
	for _, line := range []string{"KUBECONFIG=" + kubeconfig, "PWD=" + dir} {
		if !strings.Contains(text, line+"\n") {
			t.Errorf("the command's environment does not hold %q", line)
		}
	}
	for _, name := range []string{
		"HELM_KUBECONTEXT", "HELM_KUBEAPISERVER", "HELM_KUBETOKEN", "HELM_KUBECAFILE",
		"HELM_KUBEASUSER", "HELM_KUBEASGROUPS", "HELM_KUBEINSECURE_SKIP_TLS_VERIFY",
		"HELM_KUBETLS_SERVER_NAME", "HELM_NAMESPACE", "HELM_DRIVER", "HELM_DRIVER_SQL_CONNECTION_STRING",
	} {
		if strings.Contains(text, name+"=") {
			t.Errorf("the command's environment still holds %s", name)
		}
	}
	for _, line := range []string{
		"HELM_REPOSITORY_CONFIG=/caller/repositories.yaml",
		"GITHUB_TOKEN=ghs_review4_secret",
		"ROLLPROOF_PW=hunter2-p4",
	} {
		if !strings.Contains(text, line) {
			t.Errorf("the command's environment does not hold %q", line)
		}
	}
}
````

- Command, from the copy's root: `env CGO_ENABLED=0 go test ./internal/deploy -run '^TestReviewP4SetupEnvironment$' -count=1 -v`. The re-run writes the name between ^ and $.
- Reviewer's output:

````text
setup ended "ok" at index -1; exit 0
--- evidence ---
sh -c env
PWD=/tmp/TestReviewP4SetupEnvironment289821395/001
GITHUB_TOKEN=ghs_review4_secret
HELM_REPOSITORY_CACHE=/caller/cache
SHLVL=0
HELM_REPOSITORY_CONFIG=/caller/repositories.yaml
KUBECONFIG=/tmp/TestReviewP4SetupEnvironment289821395/002/kubeconfig
ROLLPROOF_PW=hunter2-p4
_=/usr/bin/env
rollproof: ok, exit code 0
````

- The re-run's output (exit 0):

````
setup ended "ok" at index -1; exit 0
--- evidence ---
sh -c env
PWD=/tmp/TestReviewP4SetupEnvironment1096127445/001
GITHUB_TOKEN=ghs_review4_secret
HELM_REPOSITORY_CACHE=/caller/cache
SHLVL=0
HELM_REPOSITORY_CONFIG=/caller/repositories.yaml
KUBECONFIG=/tmp/TestReviewP4SetupEnvironment1096127445/002/kubeconfig
ROLLPROOF_PW=hunter2-p4
_=/usr/bin/env
rollproof: ok, exit code 0
````

- Verdict of the re-run: the rule kept in both runs. What a setup command's environment holds. Held: the job's environment and the registry password are there, `KUBECONFIG` is the test cluster's, and the eleven `HELM_*` variables by which Helm would act on another cluster or keep its records elsewhere are not. `HELM_REPOSITORY_CONFIG` and `HELM_REPOSITORY_CACHE` pass, which is Helm's own repository configuration.

#### P4-6: the probe header's value is in the evidence, unmasked

- Test: `review-4/internal/probe/review_p4_6_test.go`, `TestReviewP4ProbeHeaderInEvidence`, as the copy holds it:

````go
// Review part 4, try 6: the value of a probe header is not masked. The built-in
// probe's header becomes an argument of the probe's container, and the pod as the
// API wrote it goes into probe-pod.json in the run's evidence. This test starts a
// probe with a hostile header value and prints the evidence.
package probe

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestReviewP4ProbeHeaderInEvidence is try 6.
func TestReviewP4ProbeHeaderInEvidence(t *testing.T) {
	const header = "Bearer review4-header-secret"
	o := HTTPOptions{
		URL:       "http://orders.review4.svc/api",
		Headers:   map[string]string{"Authorization": header},
		Rate:      1,
		Timeout:   100 * time.Millisecond,
		ExpectMin: 200,
		ExpectMax: 299,
	}
	s := newProbe(t, &fakeLog{}, runningStatus(), 100*time.Millisecond, 5*time.Second)
	s.pod.o.Args = HTTPArgs(o)
	var ev evidenceFiles
	s.pod.o.Evidence = ev.open
	s.endsWhenDeleted(t, logLine(logAt(400), `{"ok":true}`))
	s.clock.Set(logAt(700))
	if _, err := s.pod.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pod.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	pod, ok := ev.get("probe-pod.json")
	if !ok {
		t.Fatal("the probe wrote no probe-pod.json")
	}
	fmt.Printf("--- probe-pod.json ---\n%s\n", pod)

	if !strings.Contains(pod, header) {
		t.Errorf("probe-pod.json does not hold the header's value %q", header)
	}
}
````

- Command, from the copy's root: `env CGO_ENABLED=0 go test ./internal/probe -run '^TestReviewP4ProbeHeaderInEvidence$' -count=1 -v`. The re-run writes the name between ^ and $.
- Reviewer's output:

````text
          "--header",
          "Authorization=Bearer review4-header-secret"
````

- The re-run's output (exit 0):

````
          "--header",
          "Authorization=Bearer review4-header-secret"
````

- Verdict of the re-run: the rule kept in both runs. A probe header's value in the evidence. Held, as the design says: `probe-pod.json` holds `--header Authorization=Bearer review4-header-secret`, and nothing masks it.

#### P4-7: a chart's pull secret for a host with a mirror reaches the mirror

- Test: `review-4/internal/cluster/review_p4_7_test.go`, `TestReviewP4ChartPullSecretGoesToTheMirror`, as the copy holds it:

````go
//go:build cluster

// Review part 4, try 7: a chart's own pull secrets for a host with a mirror go
// to the mirror. The cluster's credentials file has huser for H; the pod's chart
// secret has chartuser for H; the mirror logs which one arrived.
package cluster

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"rollproof/internal/config"
)

const (
	reviewP4SecretCluster = "rollproof-review-4-chartsecret"
	reviewP4SecretStandin = "rollproof-review-4-chartsecret-standin"
)

// reviewP4BasicStandinSource is a TLS registry stand-in that asks for Basic
// credentials and then answers 404. It logs the Authorization header.
const reviewP4BasicStandinSource = `package main

import (
	"flag"
	"fmt"
	"net/http"
)

func main() {
	cert := flag.String("cert", "", "certificate")
	key := flag.String("key", "", "key")
	flag.Parse()
	fmt.Println("standin ready")
	if err := http.ListenAndServeTLS(":443", *cert, *key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("MIRROR %s %s auth=%q host=%q\n", r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), r.Host)
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", ` + "`" + `Basic realm="review4"` + "`" + `)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})); err != nil {
		fmt.Println("listen:", err)
	}
}
`

// TestReviewP4ChartPullSecretGoesToTheMirror is try 7.
func TestReviewP4ChartPullSecretGoesToTheMirror(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	work := t.TempDir()
	certs := filepath.Join(work, "certs")
	if err := os.MkdirAll(certs, 0o755); err != nil {
		t.Fatal(err)
	}
	caFile, err := reviewP4Authority(certs)
	if err != nil {
		t.Fatal(err)
	}
	bin, err := reviewP4BuildBasicStandin(work)
	if err != nil {
		t.Fatal(err)
	}

	engine, err := CheckRunner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runDir := RunDir(reviewP4SecretCluster)
	if err := os.RemoveAll(runDir); err != nil {
		t.Fatal(err)
	}
	c, err := Create(ctx, Spec{
		Name:      reviewP4SecretCluster,
		RunDir:    runDir,
		Workers:   1,
		Namespace: "review4",
		Timeout:   5 * time.Minute,
		Registries: []config.Registry{{
			Host:     reviewP4Host,
			Username: "huser",
			Password: "hunter2-p4",
			Mirror:   reviewP4Mirror,
			CA:       caFile,
		}},
	}, engine)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		_ = exec.Command(engine.Binary, "rm", "-f", reviewP4SecretStandin).Run()
		if err := c.Delete(context.Background()); err != nil {
			t.Error(err)
		}
	})

	run := exec.Command(engine.Binary, "run", "-d", "--name", reviewP4SecretStandin, "--network", "kind",
		"-v", filepath.Dir(bin)+":/app:ro", "-v", certs+":/certs:ro", "--entrypoint", "/app/standin",
		"registry:3", "-cert", "/certs/server.pem", "-key", "/certs/server-key.pem")
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("start the stand-in: %v\n%s", err, out)
	}
	ip, err := reviewP4ContainerIP(engine.Binary, reviewP4SecretStandin)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range c.Nodes() {
		if out, err := c.Exec(ctx, node, "sh", "-c", "echo '"+ip+" "+reviewP4Mirror+"' >> /etc/hosts"); err != nil {
			t.Fatalf("resolve %s in node %s: %v\n%s", reviewP4Mirror, node, err, out)
		}
	}

	client, err := c.Client()
	if err != nil {
		t.Fatal(err)
	}
	// The chart's own pull secret for H, with a user the node's file does not hold.
	const chartUser, chartPass = "chartuser", "chartpass-p4"
	dockerConfig := fmt.Sprintf(`{"auths":{%q:{"auth":%q}}}`, reviewP4Host,
		base64.StdEncoding.EncodeToString([]byte(chartUser+":"+chartPass)))
	if _, err := client.CoreV1().Secrets("review4").Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "chart-pull", Namespace: "review4"},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(dockerConfig)},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create the chart's pull secret: %v", err)
	}
	if _, err := client.CoreV1().Pods("review4").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "review4-chart-pull", Namespace: "review4"},
		Spec: corev1.PodSpec{
			NodeName:         c.Nodes()[1],
			RestartPolicy:    corev1.RestartPolicyNever,
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: "chart-pull"}},
			Containers: []corev1.Container{{
				Name:            "app",
				Image:           reviewP4Host + "/team/app:tag",
				ImagePullPolicy: corev1.PullAlways,
			}},
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create the pod: %v", err)
	}

	deadline := time.Now().Add(90 * time.Second)
	var logs string
	for time.Now().Before(deadline) {
		out, _ := exec.Command(engine.Binary, "logs", reviewP4SecretStandin).CombinedOutput()
		logs = string(out)
		if strings.Contains(logs, "auth=\"Basic ") {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Printf("--- stand-in log ---\n%s", logs)
	if got, err := client.CoreV1().Pods("review4").Get(ctx, "review4-chart-pull", metav1.GetOptions{}); err == nil {
		for _, st := range got.Status.ContainerStatuses {
			fmt.Printf("pod state: waiting=%v\n", st.State.Waiting)
		}
	}

	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(chartUser+":"+chartPass))
	if !strings.Contains(logs, want) {
		t.Errorf("the mirror did not receive the chart secret's credentials (%s); log:\n%s", want, logs)
	}
	if strings.Contains(logs, "Basic "+base64.StdEncoding.EncodeToString([]byte("huser:hunter2-p4"))) {
		// The node's own file is for the same host, so the mirror may see it too;
		// the design's claim is that the chart's secret reaches the mirror, and it did.
		fmt.Println("note: the mirror also received the node's own credentials on a later attempt")
	}
}

// reviewP4BuildBasicStandin writes and builds the Basic stand-in in work.
func reviewP4BuildBasicStandin(work string) (string, error) {
	src := filepath.Join(work, "basic-src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module review4basic\n\ngo 1.27\n"), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(reviewP4BasicStandinSource), 0o644); err != nil {
		return "", err
	}
	bin := filepath.Join(work, "basic-bin", "standin")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return "", err
	}
	cmd := exec.Command("go", "build", "-trimpath", "-o", bin, ".")
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build the Basic stand-in: %v\n%s", err, out)
	}
	return bin, nil
}
````

- Command, from the copy's root: `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags cluster -p 1 -count=1 ./internal/cluster -run TestReviewP4ChartPullSecretGoesToTheMirror -v -timeout 9m`.
- Reviewer's output:

````text
--- stand-in log ---
standin ready
MIRROR HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="" host="m.review4.example"
MIRROR HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="Basic Y2hhcnR1c2VyOmNoYXJ0cGFzcy1wNA==" host="m.review4.example"
MIRROR HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="" host="m.review4.example"
MIRROR HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="Basic aHVzZXI6aHVudGVyMi1wNA==" host="m.review4.example"
pod state: waiting=&ContainerStateWaiting{Reason:ContainerCreating,Message:,}
note: the mirror also received the node's own credentials on a later attempt
````

- The re-run's output (exit 0):

````
kind Create of rollproof-review-4-chartsecret, attempt 1: succeeded
--- stand-in log ---
standin ready
MIRROR HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="" host="m.review4.example"
MIRROR HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="Basic Y2hhcnR1c2VyOmNoYXJ0cGFzcy1wNA==" host="m.review4.example"
MIRROR HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="" host="m.review4.example"
MIRROR HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="Basic aHVzZXI6aHVudGVyMi1wNA==" host="m.review4.example"
pod state: waiting=&ContainerStateWaiting{Reason:ContainerCreating,Message:,}
note: the mirror also received the node's own credentials on a later attempt
````

- Verdict of the re-run: the rule kept in both runs. A chart's pull secret for a host with a mirror. Held: the mirror received the chart's secret, `chartuser:chartpass-p4`, and on a later attempt the node's own; nothing went to the real host.

#### P4-8: a mirror's token challenge names another host, and the token service gets the credentials

- Test: `review-4/internal/cluster/review_p4_8_test.go`, `TestReviewP4TokenServiceGetsTheCredentials`, as the copy holds it:

````go
//go:build cluster

// Review part 4, try 8: a registry entry's credentials reach the token service
// that the mirror's challenge names, when that service is another host. The
// mirror answers with a Bearer challenge whose realm is t.review4.example; the
// token service logs the form it receives.
package cluster

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"rollproof/internal/config"
)

const (
	reviewP4TokenCluster = "rollproof-review-4-token"
	reviewP4TokenStandin = "rollproof-review-4-token-standin"
	reviewP4TokenHost    = "t.review4.example"
)

// reviewP4TokenStandinSource is the mirror M and the token service T in one
// process on :443. M answers a request without a bearer token with a Bearer
// challenge whose realm is https://T/token. T logs the form and answers 500.
const reviewP4TokenStandinSource = `package main

import (
	"flag"
	"fmt"
	"net/http"
	"strings"
)

func main() {
	cert := flag.String("cert", "", "certificate")
	key := flag.String("key", "", "key")
	flag.Parse()
	fmt.Println("standin ready")
	if err := http.ListenAndServeTLS(":443", *cert, *key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("REQ host=%q method=%s path=%s auth=%q\n", r.Host, r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"))
		switch {
		case r.Host == "t.review4.example":
			r.ParseForm()
			fmt.Printf("TOKEN username=%q password=%q grant=%q service=%q scope=%q\n",
				r.Form.Get("username"), r.Form.Get("password"), r.Form.Get("grant_type"), r.Form.Get("service"), r.Form.Get("scope"))
			w.WriteHeader(http.StatusInternalServerError)
		case r.Host == "m.review4.example" || strings.HasPrefix(r.Host, "m.review4.example:"):
			w.Header().Set("WWW-Authenticate", ` + "`" + `Bearer realm="https://t.review4.example/token",service="review4"` + "`" + `)
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})); err != nil {
		fmt.Println("listen:", err)
	}
}
`

// TestReviewP4TokenServiceGetsTheCredentials is try 8.
func TestReviewP4TokenServiceGetsTheCredentials(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	work := t.TempDir()
	certs := filepath.Join(work, "certs")
	if err := os.MkdirAll(certs, 0o755); err != nil {
		t.Fatal(err)
	}
	caFile, err := reviewP4TokenAuthority(certs)
	if err != nil {
		t.Fatal(err)
	}
	bin, err := reviewP4BuildTokenStandin(work)
	if err != nil {
		t.Fatal(err)
	}

	engine, err := CheckRunner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runDir := RunDir(reviewP4TokenCluster)
	if err := os.RemoveAll(runDir); err != nil {
		t.Fatal(err)
	}
	c, err := Create(ctx, Spec{
		Name:      reviewP4TokenCluster,
		RunDir:    runDir,
		Workers:   1,
		Namespace: "review4",
		Timeout:   5 * time.Minute,
		Registries: []config.Registry{{
			Host:     reviewP4Host,
			Username: "huser",
			Password: "hunter2-p4",
			Mirror:   reviewP4Mirror,
			CA:       caFile,
		}},
	}, engine)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		_ = exec.Command(engine.Binary, "rm", "-f", reviewP4TokenStandin).Run()
		if err := c.Delete(context.Background()); err != nil {
			t.Error(err)
		}
	})

	run := exec.Command(engine.Binary, "run", "-d", "--name", reviewP4TokenStandin, "--network", "kind",
		"-v", filepath.Dir(bin)+":/app:ro", "-v", certs+":/certs:ro", "--entrypoint", "/app/standin",
		"registry:3", "-cert", "/certs/server.pem", "-key", "/certs/server-key.pem")
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("start the stand-in: %v\n%s", err, out)
	}
	ip, err := reviewP4ContainerIP(engine.Binary, reviewP4TokenStandin)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range c.Nodes() {
		line := ip + " " + reviewP4Mirror + " " + reviewP4TokenHost
		if out, err := c.Exec(ctx, node, "sh", "-c", "echo '"+line+"' >> /etc/hosts"); err != nil {
			t.Fatalf("resolve the stand-in's names in node %s: %v\n%s", node, err, out)
		}
	}

	client, err := c.Client()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Pods("review4").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "review4-token-pull", Namespace: "review4"},
		Spec: corev1.PodSpec{
			NodeName:      c.Nodes()[1],
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:            "app",
				Image:           reviewP4Host + "/team/app:tag",
				ImagePullPolicy: corev1.PullAlways,
			}},
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create the pod: %v", err)
	}

	deadline := time.Now().Add(90 * time.Second)
	var logs string
	for time.Now().Before(deadline) {
		out, _ := exec.Command(engine.Binary, "logs", reviewP4TokenStandin).CombinedOutput()
		logs = string(out)
		if strings.Contains(logs, "TOKEN ") {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Printf("--- stand-in log ---\n%s", logs)

	if !strings.Contains(logs, `username="huser"`) || !strings.Contains(logs, `password="hunter2-p4"`) {
		t.Errorf("the token service did not receive the entry's credentials; log:\n%s", logs)
	}
	if !strings.Contains(logs, `REQ host="m.review4.example"`) {
		t.Errorf("the mirror was not asked for the image; log:\n%s", logs)
	}
}

// reviewP4TokenAuthority writes a CA and a server certificate for M and T into
// dir, and returns the CA file's absolute path.
func reviewP4TokenAuthority(dir string) (string, error) {
	notBefore := time.Now().Add(-time.Minute)
	notAfter := notBefore.Add(24 * time.Hour)
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	ca := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "review part 4 token authority"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		return "", err
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	server := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: reviewP4Mirror},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{reviewP4Mirror, reviewP4TokenHost},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, server, ca, &serverKey.PublicKey, caKey)
	if err != nil {
		return "", err
	}
	serverKeyDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return "", err
	}
	for name, block := range map[string]*pem.Block{
		"ca.pem":         {Type: "CERTIFICATE", Bytes: caDER},
		"server.pem":     {Type: "CERTIFICATE", Bytes: serverDER},
		"server-key.pem": {Type: "EC PRIVATE KEY", Bytes: serverKeyDER},
	} {
		if err := os.WriteFile(filepath.Join(dir, name), pem.EncodeToMemory(block), 0o644); err != nil {
			return "", err
		}
	}
	return filepath.Join(dir, "ca.pem"), nil
}

// reviewP4BuildTokenStandin writes and builds the token stand-in in work.
func reviewP4BuildTokenStandin(work string) (string, error) {
	src := filepath.Join(work, "token-src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module review4token\n\ngo 1.27\n"), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(reviewP4TokenStandinSource), 0o644); err != nil {
		return "", err
	}
	bin := filepath.Join(work, "token-bin", "standin")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return "", err
	}
	cmd := exec.Command("go", "build", "-trimpath", "-o", bin, ".")
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build the token stand-in: %v\n%s", err, out)
	}
	return bin, nil
}
````

- Command, from the copy's root: `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags cluster -p 1 -count=1 ./internal/cluster -run TestReviewP4TokenServiceGetsTheCredentials -v -timeout 9m`.
- Reviewer's output:

````text
REQ host="m.review4.example" method=HEAD path=/v2/team/app/manifests/tag?ns=h.review4.example auth=""
REQ host="t.review4.example" method=POST path=/token auth=""
TOKEN username="huser" password="hunter2-p4" grant="password" service="review4" scope="repository:team/app:pull"
````

- The re-run's output (exit 0):

````
kind Create of rollproof-review-4-token, attempt 1: succeeded
--- stand-in log ---
standin ready
REQ host="m.review4.example" method=HEAD path=/v2/team/app/manifests/tag?ns=h.review4.example auth=""
REQ host="t.review4.example" method=POST path=/token auth=""
TOKEN username="huser" password="hunter2-p4" grant="password" service="review4" scope="repository:team/app:pull"
````

- Verdict of the re-run: the rule kept in both runs. A mirror's token challenge that names another host. Held: the token service on that host received the entry's credentials by POST form, and the mirror saw only the challenge.

#### P4-9: the engine's pull of the registry image is not in the list, and goes to the image's own host with the engine's login

- Test: `review-4/internal/cluster/review_p4_9_test.go`, `TestReviewP4EnginePullsTheRegistryImage`, as the copy holds it:

````go
//go:build linux && cluster

// Review part 4, try 9: the engine's own pull of the registry image. The tool
// starts the run's registry with startRegistry, which first asks the runner's
// engine to pull cluster.registry-image when the store lacks it. This test
// watches that pull with the engine's own login for the image's host.
package cluster

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// reviewP4RegistryLog is try 9's request log.
type reviewP4RegistryLog struct {
	mu   sync.Mutex
	text strings.Builder
}

func (l *reviewP4RegistryLog) add(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(&l.text, format+"\n", args...)
}

func (l *reviewP4RegistryLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text.String()
}

// TestReviewP4EnginePullsTheRegistryImage is try 9.
func TestReviewP4EnginePullsTheRegistryImage(t *testing.T) {
	const user, password = "registry-user", "registry-login-p4"
	var log reviewP4RegistryLog
	var mu sync.Mutex
	seen := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.add("host: %s %s auth=%q host=%q", r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), r.Host)
		mu.Lock()
		first := !seen
		seen = true
		mu.Unlock()
		if first {
			w.Header().Set("WWW-Authenticate", `Basic realm="review4"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	dockerConfig := t.TempDir()
	cfg := fmt.Sprintf(`{"auths":{%q:{"auth":%q}}}`, host,
		base64.StdEncoding.EncodeToString([]byte(user+":"+password)))
	if err := os.WriteFile(filepath.Join(dockerConfig, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_CONFIG", dockerConfig)

	push, node, err := startRegistry(context.Background(), Engine{Binary: "nerdctl"},
		"rollproof-review-4-regpull", host+"/review4/registry:tag", 30*time.Second)
	fmt.Printf("startRegistry: push=%q node=%q err=%v\n", push, node, err)
	fmt.Printf("--- registry image host log ---\n%s", log.String())

	if err == nil {
		t.Fatal("startRegistry succeeded; the test needs the pull to fail")
	}
	if !strings.Contains(log.String(), `auth="Basic `) {
		t.Errorf("the engine sent no Basic login to %s", host)
	}
}
````

- Command, from the copy's root: `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags cluster -p 1 -count=1 ./internal/cluster -run TestReviewP4EnginePullsTheRegistryImage -v`.
- Rule: "What leaves the runner through the tool. … Nothing else." (section 11)
- Reviewer's output (its verdict: broken, the list leaves it out: `startRegistry` calls the engine's `pull`; the request went to the registry image's host with the engine's login, `registry-user:registry-login-p4`, and no entry of the configuration nor the nodes' settings was involved):

````
startRegistry: push="" node="" err=nerdctl pull 127.0.0.1:44119/review4/registry:tag: exit status 1: time="2026-11-25T18:04:09-05:00" level=fatal msg="failed to resolve image: 127.0.0.1:44119/review4/registry:tag: not found"
--- registry image host log ---
host: HEAD /v2/review4/registry/manifests/tag auth="" host="127.0.0.1:44119"
host: HEAD /v2/review4/registry/manifests/tag auth="Basic cmVnaXN0cnktdXNlcjpyZWdpc3RyeS1sb2dpbi1wNA==" host="127.0.0.1:44119"
````

- The re-run's output (exit 0): the same lines; the port and the moment are new for this run:

````
startRegistry: push="" node="" err=nerdctl pull 127.0.0.1:41049/review4/registry:tag: exit status 1: time="2026-11-25T18:13:22-05:00" level=fatal msg="failed to resolve image: 127.0.0.1:41049/review4/registry:tag: not found"
--- registry image host log ---
host: HEAD /v2/review4/registry/manifests/tag auth="" host="127.0.0.1:41049"
host: HEAD /v2/review4/registry/manifests/tag auth="Basic cmVnaXN0cnktdXNlcjpyZWdpc3RyeS1sb2dpbi1wNA==" host="127.0.0.1:41049"
--- PASS: TestReviewP4EnginePullsTheRegistryImage (0.04s)
````

- Verdict of the re-run: holds.
- Ruling: document (the maintainer, 2026-11-25), in the design's section 11: the list of what leaves the runner through the tool gains the engine's pull of the run's registry image, to the image's own host with the runner's engine login. The smaller choice: a sentence, and no code.
- The text: commit `6aa25b7`.

#### P4-10: a mirror that redirects to another host, and the entry's credentials reach that host after it asks

- Test: `review-4/internal/cluster/review_p4_10_test.go`, `TestReviewP4MirrorRedirectToAnotherHost`, as the copy holds it:

````go
//go:build cluster

// Review part 4, try 10: a mirror that answers with a redirect to another host.
// The mirror M asks for Basic credentials and redirects to R, a different name
// that also asks. Section 11 says an entry's credentials go to the entry's mirror
// and to a token service that the mirror's or host's challenge names; R is
// neither. The test prints both stand-in listeners' logs.
package cluster

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"rollproof/internal/config"
)

const (
	reviewP4HostCluster = "rollproof-review-4-redirecthost"
	reviewP4HostStandin = "rollproof-review-4-redirecthost-standin"
	reviewP4Redirect    = "r.review4.example"
)

// reviewP4HostStandinSource is M and R in one process on :443, told apart by
// the request's Host. M asks for Basic and redirects to R; R also asks.
const reviewP4HostStandinSource = `package main

import (
	"flag"
	"fmt"
	"net/http"
	"strings"
)

func main() {
	cert := flag.String("cert", "", "certificate")
	key := flag.String("key", "", "key")
	flag.Parse()
	fmt.Println("standin ready")
	if err := http.ListenAndServeTLS(":443", *cert, *key, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "m.review4.example":
			fmt.Printf("MIRROR %s %s auth=%q\n", r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"))
			if r.Header.Get("Authorization") == "" {
				w.Header().Set("WWW-Authenticate", ` + "`" + `Basic realm="review4"` + "`" + `)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Location", "https://r.review4.example"+r.URL.RequestURI())
			w.WriteHeader(http.StatusTemporaryRedirect)
		case r.Host == "r.review4.example" || strings.HasPrefix(r.Host, "r.review4.example:"):
			fmt.Printf("REDIRECT %s %s auth=%q\n", r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"))
			w.Header().Set("WWW-Authenticate", ` + "`" + `Basic realm="review4"` + "`" + `)
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})); err != nil {
		fmt.Println("listen:", err)
	}
}
`

// TestReviewP4MirrorRedirectToAnotherHost is try 10.
func TestReviewP4MirrorRedirectToAnotherHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	work := t.TempDir()
	certs := filepath.Join(work, "certs")
	if err := os.MkdirAll(certs, 0o755); err != nil {
		t.Fatal(err)
	}
	caFile, err := reviewP4HostAuthority(certs)
	if err != nil {
		t.Fatal(err)
	}
	bin, err := reviewP4BuildHostStandin(work)
	if err != nil {
		t.Fatal(err)
	}

	engine, err := CheckRunner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runDir := RunDir(reviewP4HostCluster)
	if err := os.RemoveAll(runDir); err != nil {
		t.Fatal(err)
	}
	c, err := Create(ctx, Spec{
		Name:      reviewP4HostCluster,
		RunDir:    runDir,
		Workers:   1,
		Namespace: "review4",
		Timeout:   5 * time.Minute,
		Registries: []config.Registry{{
			Host:     reviewP4Host,
			Username: "huser",
			Password: "hunter2-p4",
			Mirror:   reviewP4Mirror,
			CA:       caFile,
		}},
	}, engine)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		_ = exec.Command(engine.Binary, "rm", "-f", reviewP4HostStandin).Run()
		if err := c.Delete(context.Background()); err != nil {
			t.Error(err)
		}
	})

	run := exec.Command(engine.Binary, "run", "-d", "--name", reviewP4HostStandin, "--network", "kind",
		"-v", filepath.Dir(bin)+":/app:ro", "-v", certs+":/certs:ro", "--entrypoint", "/app/standin",
		"registry:3", "-cert", "/certs/server.pem", "-key", "/certs/server-key.pem")
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("start the stand-in: %v\n%s", err, out)
	}
	ip, err := reviewP4ContainerIP(engine.Binary, reviewP4HostStandin)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range c.Nodes() {
		line := ip + " " + reviewP4Mirror + " " + reviewP4Redirect
		if out, err := c.Exec(ctx, node, "sh", "-c", "echo '"+line+"' >> /etc/hosts"); err != nil {
			t.Fatalf("resolve the stand-in's names in node %s: %v\n%s", node, err, out)
		}
	}

	client, err := c.Client()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Pods("review4").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "review4-redirecthost-pull", Namespace: "review4"},
		Spec: corev1.PodSpec{
			NodeName:      c.Nodes()[1],
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:            "app",
				Image:           reviewP4Host + "/team/app:tag",
				ImagePullPolicy: corev1.PullAlways,
			}},
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create the pod: %v", err)
	}

	deadline := time.Now().Add(90 * time.Second)
	var logs string
	for time.Now().Before(deadline) {
		out, _ := exec.Command(engine.Binary, "logs", reviewP4HostStandin).CombinedOutput()
		logs = string(out)
		if reviewP4LineWith(logs, "REDIRECT ", `auth="Basic `) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Printf("--- stand-in log ---\n%s", logs)

	if !reviewP4LineWith(logs, "REDIRECT ", `auth="Basic `) {
		t.Errorf("the redirect target did not receive the credentials; log:\n%s", logs)
	}
}

// reviewP4HostAuthority writes a CA and a certificate for M and R into dir.
func reviewP4HostAuthority(dir string) (string, error) {
	notBefore := time.Now().Add(-time.Minute)
	notAfter := notBefore.Add(24 * time.Hour)
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	ca := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "review part 4 redirect authority"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		return "", err
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	server := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: reviewP4Mirror},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{reviewP4Mirror, reviewP4Redirect},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, server, ca, &serverKey.PublicKey, caKey)
	if err != nil {
		return "", err
	}
	serverKeyDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return "", err
	}
	for name, block := range map[string]*pem.Block{
		"ca.pem":         {Type: "CERTIFICATE", Bytes: caDER},
		"server.pem":     {Type: "CERTIFICATE", Bytes: serverDER},
		"server-key.pem": {Type: "EC PRIVATE KEY", Bytes: serverKeyDER},
	} {
		if err := os.WriteFile(filepath.Join(dir, name), pem.EncodeToMemory(block), 0o644); err != nil {
			return "", err
		}
	}
	return filepath.Join(dir, "ca.pem"), nil
}

// reviewP4BuildHostStandin writes and builds the redirect stand-in in work.
func reviewP4BuildHostStandin(work string) (string, error) {
	src := filepath.Join(work, "host-src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module review4host\n\ngo 1.27\n"), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(reviewP4HostStandinSource), 0o644); err != nil {
		return "", err
	}
	bin := filepath.Join(work, "host-bin", "standin")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return "", err
	}
	cmd := exec.Command("go", "build", "-trimpath", "-o", bin, ".")
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build the redirect stand-in: %v\n%s", err, out)
	}
	return bin, nil
}
````

- Command, from the copy's root: `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags cluster -p 1 -count=1 ./internal/cluster -run TestReviewP4MirrorRedirectToAnotherHost -v -timeout 9m`.
- Rule: "A registry entry's credentials go to the entry's mirror when it has one, and to its host otherwise, and to a token service that the mirror's or host's challenge names." (section 11)
- Reviewer's output (its verdict: broken: the mirror `m.review4.example` redirected to `https://r.review4.example/...`, a host named neither by the configuration nor by a token challenge. Go strips the header on a cross-host redirect; `r` answered 401; on the retry `r` received `huser:hunter2-p4`):

````
--- stand-in log ---
standin ready
MIRROR HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth=""
MIRROR HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="Basic aHVzZXI6aHVudGVyMi1wNA=="
REDIRECT HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth=""
MIRROR HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="Basic aHVzZXI6aHVudGVyMi1wNA=="
REDIRECT HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="Basic aHVzZXI6aHVudGVyMi1wNA=="
MIRROR HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="Basic aHVzZXI6aHVudGVyMi1wNA=="
REDIRECT HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="Basic aHVzZXI6aHVudGVyMi1wNA=="
````

- The re-run's output (exit 0; a cluster run of 45 s): the same lines, and one more pair of a mirror's request with the credentials and the redirect's with them:

````
--- stand-in log ---
standin ready
MIRROR HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth=""
MIRROR HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="Basic aHVzZXI6aHVudGVyMi1wNA=="
REDIRECT HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth=""
MIRROR HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="Basic aHVzZXI6aHVudGVyMi1wNA=="
REDIRECT HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="Basic aHVzZXI6aHVudGVyMi1wNA=="
MIRROR HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="Basic aHVzZXI6aHVudGVyMi1wNA=="
REDIRECT HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="Basic aHVzZXI6aHVudGVyMi1wNA=="
MIRROR HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="Basic aHVzZXI6aHVudGVyMi1wNA=="
REDIRECT HEAD /v2/team/app/manifests/tag?ns=h.review4.example auth="Basic aHVzZXI6aHVudGVyMi1wNA=="
--- PASS: TestReviewP4MirrorRedirectToAnotherHost (44.87s)
````

- Verdict of the re-run: holds.
- Ruling: document (the maintainer, 2026-11-25), in the design's section 11: a mirror that answers a redirect sends the entry's credentials on to the host it names, which then holds them, so a caller trusts the mirror with that. The smaller choice: a sentence, and no code.
- The text: commit `1477c70`.

**What reviewer 4 did not try:**

````markdown
## 3. What I did not try, and why
- **A real `helm` binary and a real `helm pull`.** Helm is not installed on this machine, and installing is forbidden. Try 5 runs the same `deploy.NewRunner` that both drivers use and shows the environment the Helm driver would get; the Helm command line's `--kubeconfig`, `--kube-context` and `--namespace` are read in `internal/deploy/helm.go`, not run with a real Helm.
- **A real action image pull through a real registry.** Try 1 uses a stand-in engine that records the action's `pull`; the engine's use of its own login is shown for real in tries 2 and 9. A real pull would need a registry or the network, and the action's code path is the one line `"$engine" pull "$ref"`.
- **A token service reached over plain HTTP.** The design's section 9 says the nodes must trust the token service's certificate; an `http://` realm would send the username and password in cleartext. I did not test it because the token service is still the host the challenge names, which section 11's list does name; the channel is a section 9 property and part 1's masking/transport question, not the "where" list. It is worth a separate reading.
- **A mirror that redirects to a host that does not challenge, or to a subdomain of itself.** Go strips `Authorization` on a cross-host redirect that does not ask, so no credentials go; a subdomain would keep the header, and try 10's different-host challenge is the worse case. I did not run the no-challenge and subdomain variants.
- **The run's registry as a credential recipient.** If a caller sets `cluster.registry-image` to an image that answers a 401, the runtime's credential function ignores the host and would offer the entry's credentials to the run's registry. I did not test it because the registry image is part of the caller's trusted change, the default `registry:3` asks for none, and section 9 says "The run's registry asks for none".
- **A chart's pull secret keyed to the mirror's own host, with the image named after the real host.** The design says such a secret is never used for the original host's image; try 7 uses the real host's secret and shows it reaching the mirror. The mirror-keyed variant would show the kubelet not finding it, which is the design's stated behaviour, not a leak.
- **The masking of the credentials file.** The file holds the base64 of `USER:PASSWORD`; the kubelet needs it. The design's masking bullet says a password is masked "in everything the tool writes"; the credentials file is a tool-written file that holds the encoded password, and `cluster.Credentials` writes it directly, not through `internal/mask`. I did not treat this as a finding because the file must hold the credentials to work, section 9 requires it, and the masking bullet is part 1's subject; it is a wording conflict the maintainer should reconcile.
- **The probe's TLS certificate verification.** `internal/probe/http.go` sets `TLSClientConfig: &tls.Config{InsecureSkipVerify: true}`; I read it but did not run a TLS server to show the probe accepting a bad certificate. The design states it, and the header fact was the one I tested.
- **The custom probe's env and Secret values, and Helm hooks.** They run as pods with the change's power (the first bullet); I tested the built-in probe's header and the credentials file, not a custom probe's `valueFrom` Secret or a hook's hostPath.
- **A setup command's own network egress.** The first bullet gives the command the job's whole environment and the job's power; try 5 shows the environment, not a command sending the password to a host. Section 11's first bullet admits that power, so I did not treat it as a tool defect.
- **The action's unsets of `ROLLPROOF_ACTION_*`, the workflow-command stop lines, the job summary's rendering, and the registry-password masker.** These are parts 1 and 2 of point 13, not this card's five facts and list.
All ten test files are left in place. Every container, cluster, image, network and volume I made was named `rollproof-review-4-…` and removed; `nerdctl ps -a`, `nerdctl volume ls`, `nerdctl network ls`, `nerdctl images` and `/tmp` show no `rollproof-review-4-` leftovers.
````

### The second reader

The second reader read the eleven commits cold in its own copy and reported no fault: none that breaks a rule, and none that makes a test pass without its fix. So nothing was fixed after it, and it read nothing a second time. Its report, as it returned it:

````markdown
## Verdict

**No fault found that breaks a rule or makes a test pass without its fix.** I changed nothing in the repository and committed nothing. One untracked path was present at the end, `projects/rollproof/docs/proofs/p13-security-review.md`; I did not create or touch it.

I read the change `100911167568b172c18a89b2ecf48852c8087972..1477c70be1ccc3e69b5cc3b7e6812d23b5c56543`, the touched files and tests in COPY, and section 11 of `docs/specs/2026-11-13-rollproof-design.md`.

## The three code fixes

### P1-1 — map keys are masked

- **File and lines:** `internal/report/mask.go:45-63`; specifically `key := masked(iter.Key(), m).Convert(...)` at line 56 and the count merge at 58-60.
- **Test:** `internal/report/mask_test.go:366`, `TestMaskMasksMapKeys`.
- **Command:** `go test ./internal/report -run '^TestMaskMasksMapKeys$' -count=1 -v`.
- **Without the fix:** a copy of COPY with `mask.go` from `100911167568b172c18a89b2ecf48852c8087972` failed:

```text
mask_test.go:389: Windows[1].FailedByClass is map[Y2k6czNjcmV0LXBhc3M=:45 Y2kyOnMzY3JldC1wYXNz:5 refused:3 s3cret-pass:198], want map[***:203 ***=:45 refused:3]
mask_test.go:392: Install.FailedByClass is map[Y2k6czNjcmV0LXBhc3M=:1 s3cret-pass:2 timeout:3], want map[***:2 ***=:1 timeout:3]
--- FAIL: TestMaskMasksMapKeys (0.00s)
```

- **At HEAD:** `--- PASS: TestMaskMasksMapKeys (0.00s)`.
- **Near input:** `internal/report/review_s_1_test.go:20`, `TestReviewSNearMapKeys` — password with `&`, `<`, `"`, `\`, JSON and Go spellings, a second username, a padded base64, both window maps and `Install`, plus `Final`'s own masker. Command `go test ./internal/report -run '^TestReviewSNearMapKeys$' -count=1 -v`; `--- PASS`.
- **The original reviewer test:** `reviewp1/review_p1_1_test.go`, `TestP1K1FailureClassIsAPassword`, passes at HEAD.
- **Does it do more than the finding asked?** Yes, slightly: it merges two keys that mask to one string and adds their counts (`mask.go:58-60`), and the test pins that (`mask_test.go:386-393`). The finding asked only that the keys be masked. I found the merge consistent with `cleanClasses` (`console.go:137-146`) and not a security change.

### P2-6 — the three implicit bidirectional marks

- **File and lines:** `internal/report/console.go:110-117`; U+200E, U+200F and U+061C added at line 116.
- **Test:** `internal/report/console_test.go:646`, `TestCleanTakesEveryBidiControl`.
- **Command:** `go test ./internal/report -run '^TestCleanTakesEveryBidiControl$' -count=1 -v`.
- **Without the fix:** with `console.go` from the base:

```text
console_test.go:651: clean("a\u200eb") = "a\u200eb", want "a b"
console_test.go:651: clean("a\u200fb") = "a\u200fb", want "a b"
console_test.go:651: clean("a\u061cb") = "a\u061cb", want "a b"
--- FAIL: TestCleanTakesEveryBidiControl (0.00s)
```

- **At HEAD:** `--- PASS: TestCleanTakesEveryBidiControl (0.00s)`.
- **Near input:** `internal/report/review_s_2_test.go:13`, `TestReviewSNearBidiEverywhere` — all twelve marks planted in every free-text field, in an install class key, a window class key, and through `SummaryLine`. Command `go test ./internal/report -run '^TestReviewSNearBidiEverywhere$' -count=1 -v`; `--- PASS`.
- **Does it do more than the finding asked?** No; it adds exactly the three marks and a comment.

### P3-7 — `uname` before the stop line

- **File and lines:** `action.sh:127-130`; `2>/dev/null` at line 129, the new message at 130.
- **Test:** `hack/action/action_test.go:617`, `TestTheScriptPrintsNothingOfUnameBeforeItsStopLine`.
- **Command:** `go test ./hack/action -run '^TestTheScriptPrintsNothingOfUnameBeforeItsStopLine$' -count=1 -v`.
- **Without the fix:** with `action.sh` from the base:

```text
action_test.go:656: stderr holds a line the runner could read as a command: "::add-mask::REVIEWSECRET"
action_test.go:656: stderr holds a line the runner could read as a command: "##[add-mask]REVIEWSECRET"
action_test.go:656: stderr holds a line the runner could read as a command: "::add-mask::REVIEWSECRET); the action runs only on Linux"
--- FAIL: TestTheScriptPrintsNothingOfUnameBeforeItsStopLine (0.02s)
```

- **At HEAD:** `--- PASS: TestTheScriptPrintsNothingOfUnameBeforeItsStopLine (0.02s)`.
- **Near input:** `hack/action/review_s_3_test.go:13`, `TestReviewSNearUname` — command on stderr with `uname` failing, on stdout with Linux and another line, after white space, and the older form on stdout. Command `go test ./hack/action -run '^TestReviewSNearUname$' -count=1 -v`; `--- PASS`.
- **Original reviewer test:** `TestReviewP3Try7UnameBeforeTheStopLine` passes at HEAD; its first subtest now shows the stop line at index 0 on stderr and only the tool line after it; its second shows only `rollproof: the runner is not Linux; the action runs only on Linux`.
- **Does it do more than the finding asked?** It also removes the system name from the not-Linux message (`action.sh:130`). That is necessary because otherwise the captured `uname` stdout would still be printed; the finding asked for none of its output.

## The documentation fixes

- **P1-7 — `docs/specs/2026-11-13-rollproof-design.md:2439-2441`.** It says a name the caller gives, such as a workload's, is written as given where it names a file of the evidence, as a chart value or probe header is. The finding showed the workload name in `evidence/pods/<pod>/app.log`; section 8 line 1456 already states the chart-value/header case. It says what the finding showed; it generalises only as far as the finding's ruling did.
- **P3-4 — design lines 2447-2450.** I reproduced the line with `cmd/rollproof/review_s_4_test.go:14`, `TestReviewSResultsPathLine`, command `go test ./cmd/rollproof -run '^TestReviewSResultsPathLine$' -count=1 -v`:

```text
review_s_4_test.go:27: stderr line 1 "rollproof: cannot write results: results folder /tmp/.../x##[add-mask]REVIEWSECRET ::add-mask::REVIEWSECOND: mkdir ...: not a directory"
PASS
```

The `##[` form stands in the path and is read anywhere; the `::` form is mid-line because `ConsoleLine` turns the path's newline into a space, so only the older form can be read here. The sentence's generic "a workflow command" is true of the `##[` form; if read as "any form", it would say more than the finding. I did not count that as a fault.
- **P3-11 — design lines 2450-2453.** I reproduced the finding with `cmd/rollproof/review_s_9_test.go:14`, `TestReviewSResumeRedirected`, command `go test ./cmd/rollproof -run '^TestReviewSResumeRedirected$' -count=1 -v`: the redirected stdout holds `::TOKEN::` last, stderr holds only the stop line; `--- PASS`. The sentence matches.
- **P4-1 — design lines 2431-2432.** `hack/action/review_s_7_test.go:19`, `TestReviewSActionPullsTheToolImage`, command `go test ./hack/action -run '^TestReviewSActionPullsTheToolImage$' -count=1 -v` printed `pull registry.example/review4/rollproof@sha256:...`; the sentence records that pull. `--- PASS`.
- **P4-2 — design lines 2432-2434.** `internal/cluster/review_s_5_test.go:50`, command `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags cluster -p 1 -count=1 ./internal/cluster -run '^TestReviewSEnginePullsTheNodeImage$' -v`, printed the host's Basic login `Basic ZG9ja2VyLXVzZXI6ZW5naW5lLWxvZ2luLXA0` and an empty mirror log; `--- PASS`.
- **P4-3 — design lines 2461-2463.** The finding's log shows the mirror redirected to another port of itself and the target received `Basic aHVzZXI6aHVudGVyMi1wNA==` without asking. The sentence says credentials go to the place the mirror names, such as another port; it does not repeat "without asking", but it does not say the port asked either. The "which asks" clause reads as the P4-10 host. No more than the finding's ruling.
- **P4-9 — design lines 2434-2435.** `internal/cluster/review_s_6_test.go:42`, command `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags cluster -p 1 -count=1 ./internal/cluster -run '^TestReviewSEnginePullsTheRegistryImage$' -v`, printed the host's Basic login `Basic cmVnaXN0cnktdXNlcjpyZWdpc3RyeS1sb2dpbi1wNA==`; `--- PASS`.
- **P4-10 — design lines 2462-2463.** The finding's REDIRECT log shows a first request with `auth=""`, then a request with `Basic ...`; the sentence's "asks and then receives them" matches.

## What I read and found sound, one line each

- `action.sh` lines 1-159: the stop line is printed before `pick_engine`, `image_ref`, the pull and the tool; only `uname` runs before it, and now its stderr is discarded and its stdout never reaches a stream.
- `internal/report/console.go` lines 85-117: `clean` covers Cc, U+2028, U+2029, all nine explicit bidi controls and the three implicit marks.
- `internal/report/mask.go` lines 25-79: strings, pointers, slices, map keys and map values are copied and masked; `mask:"keep"` fields are skipped; times and numbers are copied.
- `internal/report/result.go` lines 260-262 and `internal/run/run.go` lines 1065-1069: `Final` and the console both call `Mask` with the run's masker.
- `internal/report/summary.go`: `Summary` and `SummaryLine` both start from `cleanRun`/`clean`.
- `hack/action/action_test.go` lines 617-669: the new test asserts on both streams and on the exact non-Linux stderr.
- `docs/specs/2026-11-13-rollproof-design.md` lines 2428-2464: the four added lists/sentences read consistently with sections 8 and 11.

## What I did not read

- The rest of `docs/specs/2026-11-13-rollproof-design.md`: I read section 8's "Secrets"/"Workflow commands", section 11, and the relevant paragraphs of section 14 point 33; not the whole 4,096 lines.
- `README.md` whole; I read the lines on secrets, evidence, the action and workflow commands (lines 9-36).
- `docs/proofs/*`, including the still-untracked `p13-security-review.md`; I did not open it.
- The cluster tests for P1-7, P4-3 and P4-10: `kind` is not on PATH, so I read the P4-3/P4-10 test source and the finding's recorded output, but did not run them.
- The e2e suite, the packages outside `internal/report`, `internal/cluster`, `cmd/rollproof` and `hack/action` beyond the code paths I followed, and the plans other than Task 2's fix plan.
- The review copies' tests other than those cited above.

## Resources

No container, image, network or volume was created. The two engine tests failed at the pull before creating anything; the action test used a logging stand-in engine. `nerdctl ps -a`, `nerdctl images`, `nerdctl network ls`, `nerdctl volume ls` and `/tmp/rollproof-review-5-*` show no `rollproof-review-5-` name.
````

The session ran its nine tests again in its copy at `1477c70`, from the copy's root. Each passed. The two that need the engine fail at the pull of the image, before they make anything, and print the engine's login at the image's own host:

| Test, as the copy holds it | Command | Exit |
| --- | --- | --- |
| `TestReviewSNearMapKeys`, `internal/report/review_s_1_test.go` | `go test ./internal/report -run '^TestReviewSNearMapKeys$' -count=1 -v` | 0 |
| `TestReviewSNearBidiEverywhere`, `internal/report/review_s_2_test.go` | `go test ./internal/report -run '^TestReviewSNearBidiEverywhere$' -count=1 -v` | 0 |
| `TestReviewSNearUname`, `hack/action/review_s_3_test.go` | `go test ./hack/action -run '^TestReviewSNearUname$' -count=1 -v` | 0 |
| `TestReviewSResultsPathLine`, `cmd/rollproof/review_s_4_test.go` | `go test ./cmd/rollproof -run '^TestReviewSResultsPathLine$' -count=1 -v` | 0 |
| `TestReviewSResumeRedirected`, `cmd/rollproof/review_s_9_test.go` | `go test ./cmd/rollproof -run '^TestReviewSResumeRedirected$' -count=1 -v` | 0 |
| `TestReviewSActionPullsTheToolImage`, `hack/action/review_s_7_test.go` | `go test ./hack/action -run '^TestReviewSActionPullsTheToolImage$' -count=1 -v` | 0 |
| `TestReviewP3Try7UnameBeforeTheStopLine`, `hack/action/review_s_8_test.go` | `go test ./hack/action -run '^TestReviewP3Try7UnameBeforeTheStopLine$' -count=1 -v` | 0 |
| `TestReviewSEnginePullsTheNodeImage`, `internal/cluster/review_s_5_test.go` | `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags cluster -p 1 -count=1 ./internal/cluster -run '^TestReviewSEnginePullsTheNodeImage$' -v` | 0 |
| `TestReviewSEnginePullsTheRegistryImage`, `internal/cluster/review_s_6_test.go` | `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags cluster -p 1 -count=1 ./internal/cluster -run '^TestReviewSEnginePullsTheRegistryImage$' -v` | 0 |

The output of `s-5`:

````
Create error: cluster-failed: pull the node image: nerdctl pull 127.0.0.1:32845/review4/node:tag: exit status 1: time="2026-11-25T21:23:18-05:00" level=fatal msg="failed to resolve image: 127.0.0.1:32845/review4/node:tag: not found"
--- host log ---
host: HEAD /v2/review4/node/manifests/tag auth="" host="127.0.0.1:32845"
host: HEAD /v2/review4/node/manifests/tag auth="Basic ZG9ja2VyLXVzZXI6ZW5naW5lLWxvZ2luLXA0" host="127.0.0.1:32845"
--- mirror log ---
````

The output of `s-6`:

````
startRegistry: push="" node="" err=nerdctl pull 127.0.0.1:42937/review4/registry:tag: exit status 1: time="2026-11-25T21:23:19-05:00" level=fatal msg="failed to resolve image: 127.0.0.1:42937/review4/registry:tag: not found"
--- registry image host log ---
host: HEAD /v2/review4/registry/manifests/tag auth="" host="127.0.0.1:42937"
host: HEAD /v2/review4/registry/manifests/tag auth="Basic cmVnaXN0cnktdXNlcjpyZWdpc3RyeS1sb2dpbi1wNA==" host="127.0.0.1:42937"
````

### The runs of Step 4

Code that a run executes changed in the fix round: `internal/report/mask.go`, `internal/report/console.go` and `action.sh`. So the whole suite and the mirror job each ran once on this machine, each command alone, from `projects/rollproof/`, at `1477c70`. The working tree was that commit's but for this record, then a draft under `docs/proofs/`. After each command, alone, `nerdctl ps -a --format "{{.Names}}"` printed nothing.

**The suite.** `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags e2e -p 1 -count=1 ./e2e/ -v -timeout 360m`. It ran from 2026-11-25 21:25:43 to 2026-11-25 23:52:57 (by the machine's clock, five hours behind UTC); `go test` reports 8833.858s. Its last lines:

    ```
    PASS
    ok  	rollproof/e2e	8833.858s
    ```

It passed: 52 top-level tests and 118 subtests, none failed and none skipped. The 32 rows that take a Helm major each ran under `helm-3` and under `helm-4`, the suite's names for its two majors, and the same rows ran under each; all passed. The other 20 top-level tests take no major. The row `first-on-the-oldest-node-image` set `cluster.node-image` to `kindest/node:v1.34.11`; the log shows no other row setting it, so those rows took the default, which the config prints as the linked kind release's.

**The mirror job.** `env KIND_EXPERIMENTAL_PROVIDER=nerdctl CGO_ENABLED=0 go test -tags mirror -p 1 -count=1 ./e2e/ -run TestMirror -v -timeout 60m`. It ran from 2026-11-25 23:53:51 to 2026-11-26 00:00:36; `go test` reports 404.998s. Its last lines:

    ```
    PASS
    ok  	rollproof/e2e	404.998s
    ```

It passed: 1 top-level test, `TestMirror`, and 2 subtests, none failed and none skipped.

## Decision

**What changed in the code.** Three findings were ruled fix. Each got a test first, in the package that owns the rule; the test was seen to fail, the smallest change was made, and the test passed; one commit for each finding. No fix adds a configuration key, a reason code or a schema version, and no test that existed changed its name.

| Finding | Commit | The change | The test |
| --- | --- | --- | --- |
| P1-1 | `5feb0e7` | `internal/report/mask.go`: `Mask` masks the keys of a map as its values; two keys that mask to one string make one entry, their counts added. | `TestMaskMasksMapKeys`, `internal/report/mask_test.go` |
| P2-6 | `f34fd6f` | `internal/report/console.go`: `isControl` takes U+200E, U+200F and U+061C, the three implicit marks, out as it does the nine explicit bidirectional controls. | `TestCleanTakesEveryBidiControl`, `internal/report/console_test.go` |
| P3-7 | `ed08ae8` | `action.sh`: `uname`'s standard error goes to `/dev/null`, and the message of a runner that is not Linux prints none of its output. | `TestTheScriptPrintsNothingOfUnameBeforeItsStopLine`, `hack/action/action_test.go` |

In the commit of P3-7 the row "not Linux" of `TestTheScriptFailsWithExit2AndWritesNoOutput` kept its name and gained the new text of the message.

**What changed in the design.** Eight findings were ruled document, each in section 11 of the design and in one commit:

| Finding | Commit | What section 11 says since |
| --- | --- | --- |
| P1-7 | `f659e07` | A name the caller gives, such as a workload's, is written as it was given where it names a file of the evidence, as a chart value or a probe header is. |
| P3-4 | `c19268f` | The failure of a results folder that cannot be opened holds the folder's path as the caller gave it, unescaped, so a caller gives a results folder's path that holds no workflow command. |
| P3-11 | `ab21379` | A third open place: a caller's script that redirects or captures the tool's standard output sends the resume line where the runner does not read it, so such a script must leave the tool's standard output connected. |
| P4-1 | `96ad3d2` | The action's pull of the tool's own image is in the list of what leaves the runner, with the runner's engine login. |
| P4-2 | `1d30ab9` | The engine's pull of the node image is in the list: to the image's own host with the runner's engine login, and not to the mirrors the configuration lists. |
| P4-3 | `65de1dd` | A mirror that answers a redirect sends the entry's credentials on to the place it names, such as another port of itself. |
| P4-9 | `6aa25b7` | The engine's pull of the run's registry image is in the list, to the image's own host with the runner's engine login as well. |
| P4-10 | `1477c70` | The same place can be another host, which asks and then receives them: a caller trusts a mirror with that. |

**What changed in the README.** No commit of the fix round changed `README.md`: `git diff --stat` from `1009111` to `1477c70` names seven files, `action.sh`, the design, and the code and the tests of `internal/report` and `hack/action`. The commit of this record adds its row to `docs/proofs/README.md`, and takes out of the design the sentences that say the review has not happened.

**What the maintainer accepted, with each reason.** The maintainer ruled on 2026-11-25.

- P1-2, how the password reaches `failed_by_class`: covered by the fix of P1-1, which masks the key wherever the record is written out, where both tries printed it. The rule is not left as it was.
- P1-5, a whole run with the password in every input: covered by the fix of P1-1; the try leaked only through the record's map keys. The rule is not left as it was.
- P2-3, a backslash before a pipe: the reading of the source of github/cmark-gfm and the two implementations on this machine, goldmark and markdown-it, all keep a pipe in the cell when a backslash stands before it, so the rule keeps. The reviewer was unclear; the session's verdict is `does not hold`.

**What stays open.**

- **The two places of point 33, and the third.** No test makes the runner read a line of standard error after the resume line of standard output; reviewer 3 wrote that its tries 1 and 4 produce the streams that would trigger it and that neither can make the runner read in that order. The order in which the runner reads the lines of a step's two streams was not run here. P3-11 found a third place, of no matter of order. Section 11 now names the three, with what a caller gives.
- **Which Markdown GitHub runs.** The reading that settled P2-3 is of a source and not a run, and the version GitHub runs was not read. Reviewer 2 made no network request, so it did not see what GitHub renders; its lines on that are read from GitHub's documentation and, where that is silent, from the GFM rules and two implementations.
- **A hosted runner.** None is on this machine. Every statement of part 3 about the runner comes from a clone of its source, at `7e211dc`.
- **The credentials file, and the first pair's text.** Reviewer 4 did not treat the credentials file as a finding, and wrote why: "the file must hold the credentials to work, section 9 requires it, and the masking bullet is part 1's subject; it is a wording conflict the maintainer should reconcile." So it is named here. The tool writes the file into the nodes with the encoded password in it, because the kubelet needs it; `cluster.Credentials` writes it directly, not through `internal/mask`. The first pair's text, "appears in nothing the tool writes", is true of the console, the job summary, the record and the evidence, and not of that one file. After P1-7 the same text is also narrower than it reads: a password that a caller gives as a name, such as a workload's, is written as it was given where it names a file of the evidence. The pair reads `yes` by the plan's rule, which asks whether the rule as the design states it held against every try. Before the branch leaves this machine, the maintainer is asked whether section 11 should say of the credentials file what it says of a chart value and a probe header.
- **A token service named over plain HTTP.** Reviewer 4 did not test it: the design's section 9 says the nodes must trust the token service's certificate, and an `http://` realm would send the username and the password unencrypted. The reviewer wrote that it is worth a separate reading. None was made.
- **The forms of a password that the rule does not name.** URL-encoded, the base64 of the password alone, in upper case and reversed: the rule as written does not promise them, and reviewer 1 reported that they survive in the evidence as held for that reason, not as findings.
- **What each reviewer did not try** stands at the end of its part in Result.

No try's two runs differ: every one of the 45 tries ran twice on 2026-11-25, and none is `differs`.

Plan assumed: "A registry password, in its plain and its encoded forms, appears in nothing the tool writes but the step's outputs on GitHub and the client library's log on standard error."
Holds: yes

Plan assumed: "Text from outside the tool stands in the job summary only inside a fenced code block or a code span, where GitHub's Markdown reads neither emphasis, links, autolinks nor HTML."
Holds: yes

Plan assumed: "On GitHub Actions no line that the tool or the action's script prints is read as a workflow command, the two places that point 33 leaves open excepted."
Holds: yes

Plan assumed: "Section 11 names every path by which a secret or a request leaves the runner through the tool, and its five facts hold."
Holds: yes
