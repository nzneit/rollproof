# Tasks 2 and 3: `mask`, and the report's writer

Read section 8 ("Secrets"), section 10 ("Limits": workload logs) and section 11 before these tasks. Decisions 2, 3, 4 and 22 of the README fix what is masked, where, and what is never masked.

### Task 2: `mask`

**Files:**
- Create: `internal/mask/mask.go`
- Create: `internal/mask/mask_test.go`

**Interfaces:**
- Consumes: nothing of the project.
- Produces: `module internal/mask`
  Hides the registry passwords of one run in what the tool writes. A
  Masker holds every form of every password of one configuration and puts
  Replacement in place of each, in a text or in a stream. It knows nothing
  of files, of the run or of the configuration's types.
- Produces: `const Replacement = "***"`
  The text that stands in for every form of a password.
- Produces: `type Credential struct { Username, Password string }`
  One registry entry's username and password, after substitution.
- Produces: `func New(creds []Credential) *Masker`
  Returns the masker of creds. A credential with an empty password is
  skipped; with none left, the masker masks nothing. Each password has the
  forms of decision 2: plain, its two JSON spellings, its Go spelling, and
  the base64 of USERNAME:PASSWORD without padding; equal spellings count once.
- Produces: `func (m *Masker) String(s string) string`
  Returns s with each complete form replaced by Replacement, read left to
  right, taking the longest form that begins at each place; what was put in
  is not read again. A nil Masker, or one with no form, returns s.
- Produces: `func (m *Masker) Writer(w io.WriteCloser) io.WriteCloser`
  Returns a writer that masks what passes through it into w, a form split
  across writes included. It holds back fewer bytes than the longest form
  until more come; Close writes them, masked where a form is whole, and then
  closes w. An error of w is returned by the call that met it. A nil Masker
  returns w itself.

- [ ] **Step 1: Write the failing tests**

```go
func TestFormsOfAPassword(t *testing.T) {
	const user, pw = "ci", "pa\"ss\\w<&>\x01"
	m := New([]Credential{{Username: user, Password: pw}})
	padded := base64.StdEncoding.EncodeToString([]byte(user + ":" + pw))
	forms := map[string]string{
		"plain":                  pw,
		"JSON with HTML escaping": jsonForm(t, pw, true),  // pa\"ss\\w<&>\u0001
		"JSON":                   jsonForm(t, pw, false), // pa\"ss\\w<&>\u0001
		"Go":                     strconv.Quote(pw)[1 : len(strconv.Quote(pw))-1], // pa\"ss\\w<&>\x01
		"base64 without padding": base64.RawStdEncoding.EncodeToString([]byte(user + ":" + pw)),
	}
	for name, form := range forms {
		if got := m.String("before " + form + " after"); got != "before *** after" {
			t.Errorf("%s: %q", name, got)
		}
	}
	// The padded form keeps its padding after the replacement.
	if got, want := m.String(padded), "***"+strings.TrimLeft(padded, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"); got != want {
		t.Errorf("padded: %q, want %q", got, want)
	}
}

func TestNilMasker(t *testing.T) {
	var m *Masker
	// m.String("a pw") == "a pw"; m.Writer(w) == w;
	// New(nil).String("a pw") == "a pw";
	// New([]Credential{{Username: "pw"}}).String("pw") == "pw" (no password, nothing masked).
}

func TestWriterAcrossWrites(t *testing.T) {
	const user, pw = "ci", "s3cret-pass"
	m := New([]Credential{{Username: user, Password: pw}})
	text := "log: " + pw + " and " + base64.StdEncoding.EncodeToString([]byte(user+":"+pw)) + "\n"
	for size := 1; size <= len(text); size++ {
		// Write text in pieces of size bytes into m.Writer over a buffer that records Close,
		// then Close: the buffer holds m.String(text), which holds "***" twice and no "s3cret".
	}
}

func TestWriterAtClose(t *testing.T) {
	// pw "s3cret-pass". Writing "end " + pw[:len(pw)-1] and closing gives that text unchanged;
	// writing "end " + pw and closing gives "end ***"; the underlying writer is closed once.
	// After 1,000 bytes with no form and before Close, the underlying writer already holds at
	// least 1,000 - (len(longest form) - 1) of them.
}

func TestOverlappingPasswords(t *testing.T) {
	m := New([]Credential{{Username: "u1", Password: "abc"}, {Username: "u2", Password: "xabcx"},
		{Username: "same", Password: "same"}})
	// m.String("xabcx abc same") == "*** *** ***"; m.String("xabc") == "x***".
}

func TestWriterErrors(t *testing.T) {
	// A Write of w that fails makes the masker's Write (or Close, for held-back bytes) return
	// that error; a Close of w that fails is returned by Close.
}
```

`jsonForm(t, s, html)` is a test helper: `json.Marshal` of s for `html`, an encoder with `SetEscapeHTML(false)` otherwise, with the quotes and the newline taken off.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/mask/ -v`
Expected: FAIL: the package does not compile.

- [ ] **Step 3: Implement `internal/mask/mask.go`, each name with its interface comment from the Interfaces block**

One scanner serves both operations: at each place, the longest form that begins there is replaced and the scan jumps past it; else one byte is kept. `String` scans to the end. The writer appends each write to what it held, scans only the places from which a whole form would still fit in what it holds, writes the result, and keeps the rest, which is fewer bytes than the longest form; `Close` scans what is left to its end. Its `Write` returns `len(p)` when the bytes it could write were written.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/mask/ -v` and `go vet ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mask/mask.go internal/mask/mask_test.go
git commit -m "Add the masker of registry passwords, for texts and for streams"
```

### Task 3: The report's writer masks what it writes

**Files:**
- Create: `internal/report/mask.go`
- Modify: `internal/report/record.go` (the `mask:"keep"` tags of decision 22), `internal/report/result.go` (`Writer`, `Final`), `internal/report/evidence.go` (`Evidence`, `ReplaceEvidence`)
- Create: `internal/report/mask_test.go`

**Interfaces:**
- Consumes: `mask.New`, `(*mask.Masker).String`, `(*mask.Masker).Writer` (Task 2).
- Produces: `func Mask(r Run, m *mask.Masker) Run`
  Returns a copy of r in which m has masked every string, the strings of
  lists, pointers and map values included, except map keys and the fields
  tagged mask:"keep", which hold a fixed vocabulary. r is not changed. The
  console prints the copy; Final writes it.
- Produces: `func (w *Writer) SetMasker(m *mask.Masker)`
  From this call on, Final writes Mask of its record, Evidence's files mask
  what is written to them, and ReplaceEvidence masks its bytes. The run
  calls it once, after the configuration loads and before anything that may
  hold a password is written. Safe while other goroutines write.

- [ ] **Step 1: Write the failing tests**

```go
func TestWriterMasksWhatItWrites(t *testing.T) {
	const pw = "s3cret-pass"
	w, err := Open(t.TempDir(), func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	w.SetMasker(mask.New([]mask.Credential{{Username: "ci", Password: pw}}))
	// Final of a record with pw in a reason's Text and Detail and in Warnings: result.json
	// parses, its verdict is unchanged, it holds "***" and no "s3cret".
	// Evidence("pods/orders-1/orders.log"), written as "a s3c" then "ret-pass b", then closed:
	// the file is "a *** b".
	// ReplaceEvidence("windows.json", []byte(`{"x":"`+pw+`"}`)): the file is `{"x":"***"}`.
}

func TestMaskReachesEveryFreeTextField(t *testing.T) {
	// fillStrings, a reflective test helper, sets every string of a Run to "s3cret-pass": each
	// string field, *string, list item and map value, with one map key "k". After Mask, a
	// reflective walk lists the paths that still hold it. Want exactly these, from decision 22:
	// Verdict, Driver, Reasons[0].Code, .Verdict, .Window, Windows[0].Name, .Result,
	// Transitions[0].Name, .Command, .Restart, Transitions[0].Workloads[0].Probed,
	// Probes[0].Kind, Releases[0].Role, Images[0].RunBy[0].Transition, Cluster.Provider.
	// The filled Run is unchanged after the call.
}

func TestMaskKeepsTheVocabulary(t *testing.T) {
	m := mask.New([]mask.Credential{{Username: "ci", Password: "pass"}})
	r := Mask(Run{Verdict: "pass", Windows: []Window{{Name: "steady", Result: "pass"}},
		Reasons: []Reason{{Code: "probe-failures", Verdict: "fail", Text: "the probe said pass"}}}, m)
	// r.Verdict == "pass", r.Windows[0].Result == "pass", r.Reasons[0].Text == "the probe said ***".
}

func TestNoMaskerChangesNothing(t *testing.T) {
	// A Writer with no SetMasker writes result.json and evidence as before: the golden
	// result files of result_test.go still match (run the existing tests). Mask(r, nil) equals r.
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/report/ -run 'TestWriterMasksWhatItWrites|TestMaskReachesEveryFreeTextField|TestMaskKeepsTheVocabulary|TestNoMaskerChangesNothing' -v`
Expected: FAIL: `Mask` and `SetMasker` are not defined.

- [ ] **Step 3: Implement `Mask` in `internal/report/mask.go` and `SetMasker` in `result.go`, each with its interface comment from the Interfaces block**

`Mask` copies by reflection: a struct of this package is walked field by field, skipping a field tagged `mask:"keep"`; a string is masked with `m.String`; a pointer, a list and a map are copied with their contents walked, a map's keys kept; any other value, such as a `time.Time`, is copied as it is. Tag the fields that decision 22 lists in `record.go`. `Writer` holds the masker in an `atomic.Pointer[mask.Masker]`; `write` encodes `Mask(r, …)`; `Evidence` returns `m.Writer(f)`; `ReplaceEvidence` writes `[]byte(m.String(string(b)))`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/report/ -v` and `go vet ./...`
Expected: PASS, the existing golden files unchanged.

- [ ] **Step 5: Commit**

```bash
git add internal/report/mask.go internal/report/mask_test.go internal/report/record.go internal/report/result.go internal/report/evidence.go
git commit -m "Mask registry passwords in result.json and every evidence file"
```
