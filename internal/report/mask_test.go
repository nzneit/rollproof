package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"rollproof/internal/mask"
)

// TestWriterMasksWhatItWrites checks that a Writer with a masker hides the
// password in result.json, in an evidence file written in two calls, and in an
// evidence file that ReplaceEvidence writes.
func TestWriterMasksWhatItWrites(t *testing.T) {
	const pw = "s3cret-pass"
	w, err := Open(t.TempDir(), func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	w.SetMasker(mask.New([]mask.Credential{{Username: "ci", Password: pw}}))

	// Final of a record with pw in a reason's Text and Detail and in Warnings:
	// result.json parses, its verdict is unchanged, it holds "***" and no
	// "s3cret".
	r := Run{Verdict: "fail",
		Reasons: []Reason{{
			Code:    "probe-failures",
			Verdict: "fail",
			Text:    "the registry refused " + pw,
			Detail:  []string{"curl was given " + pw},
		}},
		Warnings: []string{"deleting the cluster: " + pw + " was still set"},
	}
	if err := w.Final(r); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(w.Dir(), "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got Run
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("result.json does not parse: %v", err)
	}
	if got.Verdict != "fail" {
		t.Errorf("verdict is %q, want fail", got.Verdict)
	}
	if !strings.Contains(string(b), mask.Replacement) {
		t.Errorf("result.json holds no %q:\n%s", mask.Replacement, b)
	}
	if strings.Contains(string(b), "s3cret") {
		t.Errorf("result.json holds the password:\n%s", b)
	}

	// Evidence("pods/orders-1/orders.log"), written as "a s3c" then "ret-pass b",
	// then closed: the file is "a *** b".
	f, err := w.Evidence("pods/orders-1/orders.log")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("a s3c")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("ret-pass b")); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(w.Dir(), "evidence", "pods", "orders-1", "orders.log")
	if b, err := os.ReadFile(log); err != nil || string(b) != "a *** b" {
		t.Errorf("orders.log is %q, %v; want %q", b, err, "a *** b")
	}

	// ReplaceEvidence("windows.json", []byte(`{"x":"`+pw+`"}`)): the file is
	// `{"x":"***"}`.
	if err := w.ReplaceEvidence("windows.json", []byte(`{"x":"`+pw+`"}`)); err != nil {
		t.Fatal(err)
	}
	win := filepath.Join(w.Dir(), "evidence", "windows.json")
	if b, err := os.ReadFile(win); err != nil || string(b) != `{"x":"***"}` {
		t.Errorf("windows.json is %q, %v; want %q", b, err, `{"x":"***"}`)
	}
}

var (
	timeType    = reflect.TypeOf(time.Time{})
	timePtrType = reflect.TypeOf((*time.Time)(nil))
)

// fillValue sets every string of v to s: each string field, *string and list
// item. It gives every list one element, every map the one key "k", and every
// time.Time and *time.Time the moment. v must be settable.
func fillValue(v reflect.Value, s string, moment time.Time) {
	switch {
	case v.Type() == timeType:
		v.Set(reflect.ValueOf(moment))
		return
	case v.Type() == timePtrType:
		p := reflect.New(timeType)
		p.Elem().Set(reflect.ValueOf(moment))
		v.Set(p)
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Ptr:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		fillValue(v.Elem(), s, moment)
	case reflect.Slice:
		out := reflect.MakeSlice(v.Type(), 1, 1)
		fillValue(out.Index(0), s, moment)
		v.Set(out)
	case reflect.Map:
		out := reflect.MakeMap(v.Type())
		out.SetMapIndex(reflect.ValueOf("k").Convert(v.Type().Key()), reflect.New(v.Type().Elem()).Elem())
		v.Set(out)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			fillValue(v.Field(i), s, moment)
		}
	}
}

// filledRun returns a Run of one of everything, every string of it s, every
// time the moment and every map the key "k".
func filledRun(s string, moment time.Time) Run {
	var r Run
	fillValue(reflect.ValueOf(&r).Elem(), s, moment)
	return r
}

// stillHolding walks v and appends to out the path of every string that is s.
// A path names the Go fields of the record, from the top: Reasons[0].Code.
func stillHolding(v reflect.Value, s, path string, out *[]string) {
	switch v.Kind() {
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			p := t.Field(i).Name
			if path != "" {
				p = path + "." + p
			}
			stillHolding(v.Field(i), s, p, out)
		}
	case reflect.Ptr:
		if !v.IsNil() {
			stillHolding(v.Elem(), s, path, out)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			stillHolding(v.Index(i), s, fmt.Sprintf("%s[%d]", path, i), out)
		}
	case reflect.String:
		if v.String() == s {
			*out = append(*out, path)
		}
	}
}

// TestMaskReachesEveryFreeTextField fills every string of a Run with the
// password, masks it, and checks that exactly the fields of decision 22's
// vocabulary still hold it: Mask masks every other string, wherever it sits.
func TestMaskReachesEveryFreeTextField(t *testing.T) {
	const pw = "s3cret-pass"
	moment := t0.Add(time.Minute)
	m := mask.New([]mask.Credential{{Username: "ci", Password: pw}})

	filled := filledRun(pw, moment)
	untouched := filledRun(pw, moment)
	got := Mask(filled, m)

	var found []string
	stillHolding(reflect.ValueOf(got), pw, "", &found)
	want := []string{
		"Verdict", "Driver",
		"Reasons[0].Code", "Reasons[0].Verdict", "Reasons[0].Window",
		"Windows[0].Name", "Windows[0].Result",
		"Transitions[0].Name", "Transitions[0].Command", "Transitions[0].Restart",
		"Transitions[0].Workloads[0].Probed",
		"Probes[0].Kind",
		"Releases[0].Role",
		"Settings[0].Key", "Settings[0].Default",
		"Probes[0].Settings[0].Key", "Probes[0].Settings[0].Default",
		"Images[0].RunBy[0].Transition",
		"Cluster.Provider",
	}
	if !samePaths(found, want) {
		t.Errorf("the fields that still hold the password are\n%v\nwant\n%v", found, want)
	}

	// Every time of the copy equals the moment, and each map of the copy still
	// has the key "k".
	for _, c := range []struct {
		name string
		got  *time.Time
	}{
		{"Windows[0].Start", got.Windows[0].Start},
		{"Windows[0].End", got.Windows[0].End},
		{"Windows[0].FirstFailure", got.Windows[0].FirstFailure},
		{"Install.FirstObservation", got.Install.FirstObservation},
		{"Install.Settled", got.Install.Settled},
		{"Install.LastFailure", got.Install.LastFailure},
	} {
		if c.got == nil || !c.got.Equal(moment) {
			t.Errorf("%s is %v, want %v", c.name, c.got, moment)
		}
	}
	if !got.Install.Ready.Equal(moment) {
		t.Errorf("Install.Ready is %v, want %v", got.Install.Ready, moment)
	}
	for _, c := range []struct {
		name string
		got  map[string]int
	}{
		{"Windows[0].FailedByClass", got.Windows[0].FailedByClass},
		{"Install.FailedByClass", got.Install.FailedByClass},
	} {
		if _, ok := c.got["k"]; !ok || len(c.got) != 1 {
			t.Errorf("%s is %v, want the key k alone", c.name, c.got)
		}
	}

	// The filled Run is unchanged after the call.
	if !reflect.DeepEqual(filled, untouched) {
		t.Error("Mask changed the record it was given")
	}
}

// samePaths reports whether got and want hold the same paths, whatever their
// order.
func samePaths(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	count := map[string]int{}
	for _, p := range got {
		count[p]++
	}
	for _, p := range want {
		count[p]--
		if count[p] < 0 {
			return false
		}
	}
	return true
}

// TestMaskKeepsTheVocabulary checks that a word of the fixed vocabulary is
// never masked, whatever a password spells.
func TestMaskKeepsTheVocabulary(t *testing.T) {
	m := mask.New([]mask.Credential{{Username: "ci", Password: "pass"}, {Username: "ci2", Password: "rate"}})
	r := Mask(Run{Verdict: "pass", Windows: []Window{{Name: "steady", Result: "pass"}},
		Reasons:  []Reason{{Code: "probe-failures", Verdict: "fail", Text: "the probe said pass"}},
		Settings: []Setting{{Key: "probe.http.rate", Value: "100", Default: "200"}},
		Probes:   []Probe{{Kind: "http", Settings: []Setting{{Key: "probe.http.rate", Value: "100", Default: "200"}}}}}, m)
	if r.Verdict != "pass" {
		t.Errorf("Verdict is %q, want pass", r.Verdict)
	}
	if r.Windows[0].Result != "pass" {
		t.Errorf("Windows[0].Result is %q, want pass", r.Windows[0].Result)
	}
	if r.Reasons[0].Text != "the probe said ***" {
		t.Errorf("Reasons[0].Text is %q, want %q", r.Reasons[0].Text, "the probe said ***")
	}
	if r.Settings[0].Key != "probe.http.rate" {
		t.Errorf("Settings[0].Key is %q, want probe.http.rate", r.Settings[0].Key)
	}
	if r.Probes[0].Settings[0].Key != "probe.http.rate" {
		t.Errorf("Probes[0].Settings[0].Key is %q, want probe.http.rate", r.Probes[0].Settings[0].Key)
	}
}

// TestNoMaskerChangesNothing checks that a Writer that never took a masker
// writes the files it wrote before: result.json is the golden file's bytes and
// evidence holds its text as it was written.
func TestNoMaskerChangesNothing(t *testing.T) {
	w, err := Open(t.TempDir(), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	r := passRun()
	if err := w.Final(r); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(w.Dir(), "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	sameAsFile(t, "testdata/result-pass.golden.json", b)

	const text = "a s3cret-pass b"
	f, err := w.Evidence("windows.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(w.Dir(), "evidence", "windows.json")); err != nil || string(b) != text {
		t.Errorf("evidence is %q, %v; want %q", b, err, text)
	}
	if err := w.ReplaceEvidence("raw.json", []byte(text)); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(w.Dir(), "evidence", "raw.json")); err != nil || string(b) != text {
		t.Errorf("raw.json is %q, %v; want %q", b, err, text)
	}

	if got := Mask(r, nil); !reflect.DeepEqual(got, r) {
		t.Errorf("Mask(r, nil) = %+v, want r", got)
	}
}

// TestEvidenceIsSafeForManyGoroutines writes one evidence file from several
// goroutines with a masker set: the file holds every line and no password.
func TestEvidenceIsSafeForManyGoroutines(t *testing.T) {
	const (
		pw      = "s3cret-pass"
		writers = 8
		lines   = 50
	)
	w, err := Open(t.TempDir(), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	w.SetMasker(mask.New([]mask.Credential{{Username: "ci", Password: pw}}))
	f, err := w.Evidence("many.log")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for g := 0; g < writers; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < lines; i++ {
				if _, err := fmt.Fprintf(f, "writer %d line %d holds %s\n", g, i, pw); err != nil {
					t.Errorf("writer %d: %v", g, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(w.Dir(), "evidence", "many.log"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(b), "\n"); got != writers*lines {
		t.Errorf("many.log holds %d lines, want %d", got, writers*lines)
	}
	if strings.Contains(string(b), "s3cret") {
		t.Errorf("many.log holds the password")
	}
}
