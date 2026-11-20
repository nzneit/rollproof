package mask

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
)

func TestFormsOfAPassword(t *testing.T) {
	const user, pw = "ci", "pa\"ss\\w<&>\x01"
	m := New([]Credential{{Username: user, Password: pw}})
	padded := base64.StdEncoding.EncodeToString([]byte(user + ":" + pw))
	forms := map[string]string{
		"plain":                   pw,
		"JSON with HTML escaping": jsonForm(t, pw, true),                           // pa\"ss\\w\u003c\u0026\u003e\u0001
		"JSON":                    jsonForm(t, pw, false),                          // pa\"ss\\w<&>\u0001
		"Go":                      strconv.Quote(pw)[1 : len(strconv.Quote(pw))-1], // pa\"ss\\w<&>\x01
		"base64 without padding":  base64.RawStdEncoding.EncodeToString([]byte(user + ":" + pw)),
	}
	// The three escaped spellings are the ones the comments name, so each is a form of its own.
	for name, want := range map[string]string{
		"JSON with HTML escaping": `pa\"ss\\w\u003c\u0026\u003e\u0001`,
		"JSON":                    `pa\"ss\\w<&>\u0001`,
		"Go":                      `pa\"ss\\w<&>\x01`,
	} {
		if forms[name] != want {
			t.Fatalf("%s spelling is %q, want %q", name, forms[name], want)
		}
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
	if got := m.String("a pw"); got != "a pw" {
		t.Errorf("a nil Masker's String gives %q, want %q", got, "a pw")
	}
	w := &recorder{}
	if got := m.Writer(w); got != w {
		t.Errorf("a nil Masker's Writer gives %T, want the writer it was given", got)
	}
	if got := New(nil).String("a pw"); got != "a pw" {
		t.Errorf("New(nil).String gives %q, want %q", got, "a pw")
	}
	if got := New([]Credential{{Username: "pw"}}).String("pw"); got != "pw" {
		t.Errorf("a credential with no password masked %q into %q", "pw", got)
	}
}

func TestWriterAcrossWrites(t *testing.T) {
	const user, pw = "ci", "s3cret-pass"
	m := New([]Credential{{Username: user, Password: pw}})
	text := "log: " + pw + " and " + base64.StdEncoding.EncodeToString([]byte(user+":"+pw)) + "\n"
	want := m.String(text)
	if strings.Count(want, Replacement) != 2 || strings.Contains(want, "s3cret") {
		t.Fatalf("String(%q) = %q, want %q twice and no s3cret", text, want, Replacement)
	}
	for size := 1; size <= len(text); size++ {
		r := &recorder{}
		w := m.Writer(r)
		for rest := text; rest != ""; {
			piece := rest[:min(size, len(rest))]
			rest = rest[len(piece):]
			if n, err := io.WriteString(w, piece); n != len(piece) || err != nil {
				t.Fatalf("size %d: Write(%q) = %d, %v", size, piece, n, err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatalf("size %d: Close: %v", size, err)
		}
		if got := r.String(); got != want {
			t.Errorf("size %d: the writer holds %q, want %q", size, got, want)
		}
		if r.closes != 1 {
			t.Errorf("size %d: the writer was closed %d times, want once", size, r.closes)
		}
	}
}

func TestWriterAtClose(t *testing.T) {
	const user, pw = "ci", "s3cret-pass"
	m := New([]Credential{{Username: user, Password: pw}})
	longest := max(len(pw), len(base64.RawStdEncoding.EncodeToString([]byte(user+":"+pw))))

	for _, c := range []struct{ in, want string }{
		{"end " + pw[:len(pw)-1], "end " + pw[:len(pw)-1]},
		{"end " + pw, "end ***"},
	} {
		r := &recorder{}
		w := m.Writer(r)
		if _, err := io.WriteString(w, c.in); err != nil {
			t.Fatalf("Write(%q): %v", c.in, err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("Close after %q: %v", c.in, err)
		}
		if got := r.String(); got != c.want {
			t.Errorf("after %q and Close the writer holds %q, want %q", c.in, got, c.want)
		}
		if r.closes != 1 {
			t.Errorf("after %q the writer was closed %d times, want once", c.in, r.closes)
		}
	}

	r := &recorder{}
	w := m.Writer(r)
	for range 1000 {
		if _, err := io.WriteString(w, "x"); err != nil {
			t.Fatal(err)
		}
	}
	if got, least := r.Len(), 1000-(longest-1); got < least {
		t.Errorf("after 1,000 bytes with no form the writer holds %d, want at least %d", got, least)
	}
	if r.closes != 0 {
		t.Errorf("the writer was closed before Close")
	}
}

func TestOverlappingPasswords(t *testing.T) {
	m := New([]Credential{{Username: "u1", Password: "abc"}, {Username: "u2", Password: "xabcx"},
		{Username: "same", Password: "same"}})
	if got := m.String("xabcx abc same"); got != "*** *** ***" {
		t.Errorf("String(%q) = %q, want %q", "xabcx abc same", got, "*** *** ***")
	}
	if got := m.String("xabc"); got != "x***" {
		t.Errorf("String(%q) = %q, want %q", "xabc", got, "x***")
	}
}

func TestWriterErrors(t *testing.T) {
	const pw = "s3cret-pass"
	m := New([]Credential{{Username: "ci", Password: pw}})
	boom := errors.New("boom")

	// A Write of w that fails: the masker's Write returns that error.
	w := m.Writer(&failing{write: boom})
	if _, err := io.WriteString(w, strings.Repeat("x", 100)); !errors.Is(err, boom) {
		t.Errorf("Write over a failing writer returned %v, want %v", err, boom)
	}

	// Held-back bytes that fail at Close: Close returns that error, and w is still closed.
	f := &failing{write: boom}
	w = m.Writer(f)
	if _, err := io.WriteString(w, "end"); err != nil {
		t.Errorf("Write of held-back bytes returned %v, want nil", err)
	}
	if err := w.Close(); !errors.Is(err, boom) {
		t.Errorf("Close with held-back bytes over a failing writer returned %v, want %v", err, boom)
	}
	if f.closes != 1 {
		t.Errorf("the failing writer was closed %d times, want once", f.closes)
	}

	// A Close of w that fails is returned by Close.
	w = m.Writer(&failing{close: boom})
	if _, err := io.WriteString(w, "end "+pw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); !errors.Is(err, boom) {
		t.Errorf("Close over a writer whose Close fails returned %v, want %v", err, boom)
	}
}

// jsonForm returns s as encoding/json spells it inside a string's quotes, with
// HTML escaping as json.Marshal writes it, or without it.
func jsonForm(t *testing.T, s string, html bool) string {
	t.Helper()
	var b []byte
	if html {
		var err error
		if b, err = json.Marshal(s); err != nil {
			t.Fatal(err)
		}
	} else {
		var buf bytes.Buffer
		e := json.NewEncoder(&buf)
		e.SetEscapeHTML(false)
		if err := e.Encode(s); err != nil {
			t.Fatal(err)
		}
		b = bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	}
	return string(b[1 : len(b)-1])
}

// recorder is a buffer that counts its closes.
type recorder struct {
	bytes.Buffer
	closes int
}

func (r *recorder) Close() error {
	r.closes++
	return nil
}

// failing is a writer whose Write fails with write and whose Close fails with
// close, each when it is set; it counts its closes.
type failing struct {
	write, close error
	closes       int
}

func (f *failing) Write(p []byte) (int, error) {
	if f.write != nil {
		return 0, f.write
	}
	return len(p), nil
}

func (f *failing) Close() error {
	f.closes++
	return f.close
}
