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
	// A form that overlaps the start of another credential's form, side by side: the longest
	// form at the first place is masked, and the rest of the second is written as it is.
	m = New([]Credential{{Username: "u1", Password: "xa"}, {Username: "u2", Password: "abcdef"}})
	if got := m.String("xabcdef"); got != "***bcdef" {
		t.Errorf("String(%q) = %q, want %q", "xabcdef", got, "***bcdef")
	}
}

// throughWriter writes the pieces one by one through m's writer, closes it, and returns what
// reached the writer below.
func throughWriter(t *testing.T, m *Masker, pieces ...string) string {
	t.Helper()
	r := &recorder{}
	w := m.Writer(r)
	for _, p := range pieces {
		if n, err := io.WriteString(w, p); n != len(p) || err != nil {
			t.Fatalf("Write(%q) = %d, %v", p, n, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return r.String()
}

// splitEvery cuts text into pieces of size bytes, the last one shorter.
func splitEvery(text string, size int) []string {
	var pieces []string
	for len(text) > size {
		pieces, text = append(pieces, text[:size]), text[size:]
	}
	return append(pieces, text)
}

// checkEverySplit fails unless String of text, and text written through m's writer in pieces of
// every size, each give want.
func checkEverySplit(t *testing.T, m *Masker, text, want string) {
	t.Helper()
	if got := m.String(text); got != want {
		t.Errorf("String(%q) = %q, want %q", text, got, want)
	}
	for size := 1; size <= len(text); size++ {
		if got := throughWriter(t, m, splitEvery(text, size)...); got != want {
			t.Errorf("%q in pieces of %d: the writer gives %q, want %q", text, size, got, want)
		}
	}
}

func TestWriterPrefixPair(t *testing.T) {
	// The forms abc and abcdef: the writer holds xabc back, since abcdef may still follow.
	m := New([]Credential{{Username: "u1", Password: "abc"}, {Username: "u2", Password: "abcdef"}})
	for _, c := range []struct {
		name   string
		pieces []string
		want   string
	}{
		{"xabc, then def", []string{"xabc", "def"}, "x***"},
		{"xabc, then Close", []string{"xabc"}, "x***"},
	} {
		whole := strings.Join(c.pieces, "")
		if got := m.String(whole); got != c.want {
			t.Errorf("%s: String(%q) = %q, want %q", c.name, whole, got, c.want)
		}
		if got := throughWriter(t, m, c.pieces...); got != c.want {
			t.Errorf("%s: the writer gives %q, want %q", c.name, got, c.want)
		}
	}
	checkEverySplit(t, m, "xabcdef xabc yabcde", "x*** x*** y***de")
}

func TestWriterPasswordsOfAsterisks(t *testing.T) {
	// What is put in is not read again, so a password made of the replacement's bytes, or
	// holding the replacement, is masked once per occurrence.
	for _, c := range []struct{ name, pw, text, want string }{
		{"a password *", "*", "a*b**c", "a***b******c"},
		{"a password **", "**", "a**b***c", "a***b****c"},
		{"a password holding ***", "p***q", "p***q and *** and p***", "*** and *** and p***"},
	} {
		t.Run(c.name, func(t *testing.T) {
			checkEverySplit(t, New([]Credential{{Username: "u", Password: c.pw}}), c.text, c.want)
		})
	}
}

func TestWriterPasswordInsideAnotherBase64Form(t *testing.T) {
	// The second password is a part of the first credential's base64 form: that form, the
	// longer, is masked whole where it stands, and the second password alone elsewhere.
	const inside = "czNjcmV0"
	if b64 := base64.RawStdEncoding.EncodeToString([]byte("ci:s3cret-pass")); !strings.Contains(b64, inside) {
		t.Fatalf("%q does not hold %q", b64, inside)
	}
	m := New([]Credential{{Username: "ci", Password: "s3cret-pass"}, {Username: "u2", Password: inside}})
	checkEverySplit(t, m, "auth Y2k6czNjcmV0LXBhc3M= and czNjcmV0 end", "auth ***= and *** end")
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

func TestWriterShortWrite(t *testing.T) {
	m := New([]Credential{{Username: "ci", Password: "s3cret-pass"}})

	// A short write with no error, in Write: io.ErrShortWrite.
	w := m.Writer(&short{})
	if _, err := io.WriteString(w, strings.Repeat("x", 100)); !errors.Is(err, io.ErrShortWrite) {
		t.Errorf("Write over a writer that writes short returned %v, want %v", err, io.ErrShortWrite)
	}

	// The same for the held-back bytes at Close, and w is still closed.
	s := &short{}
	w = m.Writer(s)
	if _, err := io.WriteString(w, "end"); err != nil {
		t.Errorf("Write of held-back bytes returned %v, want nil", err)
	}
	if err := w.Close(); !errors.Is(err, io.ErrShortWrite) {
		t.Errorf("Close with held-back bytes over a writer that writes short returned %v, want %v", err, io.ErrShortWrite)
	}
	if s.closes != 1 {
		t.Errorf("the writer was closed %d times, want once", s.closes)
	}
}

func TestWriterCloseReturnsBothErrors(t *testing.T) {
	m := New([]Credential{{Username: "ci", Password: "s3cret-pass"}})
	writeErr, closeErr := errors.New("write failed"), errors.New("close failed")
	f := &failing{write: writeErr, close: closeErr}
	w := m.Writer(f)
	if _, err := io.WriteString(w, "end"); err != nil {
		t.Fatalf("Write of held-back bytes returned %v, want nil", err)
	}
	err := w.Close()
	if !errors.Is(err, writeErr) || !errors.Is(err, closeErr) {
		t.Errorf("Close returned %v, want both %v and %v", err, writeErr, closeErr)
	}
	if f.closes != 1 {
		t.Errorf("the failing writer was closed %d times, want once", f.closes)
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

// short is a writer that writes one byte less than it is given, with no error; it counts its
// closes.
type short struct {
	closes int
}

func (s *short) Write(p []byte) (int, error) { return max(len(p)-1, 0), nil }

func (s *short) Close() error {
	s.closes++
	return nil
}
