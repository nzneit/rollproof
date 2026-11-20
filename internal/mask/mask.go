// Package mask hides the registry passwords of one run in what the tool
// writes. A Masker holds every form of every password of one configuration
// and puts Replacement in place of each, in a text or in a stream. It knows
// nothing of files, of the run or of the configuration's types.
package mask

import (
	"bytes"
	"cmp"
	"encoding/base64"
	"encoding/json"
	"io"
	"slices"
	"strconv"
)

// Replacement is the text that stands in for every form of a password.
const Replacement = "***"

// Credential is one registry entry's username and password, after
// substitution.
type Credential struct {
	Username, Password string
}

// Masker holds the forms of the passwords it hides. A nil Masker masks
// nothing.
type Masker struct {
	forms [][]byte // longest first, each once
}

// New returns the masker of creds. A credential with an empty password is
// skipped; with none left, the masker masks nothing. Each password has the
// forms of decision 2: plain, its two JSON spellings, its Go spelling, and
// the base64 of USERNAME:PASSWORD without padding; equal spellings count once.
func New(creds []Credential) *Masker {
	seen := map[string]bool{}
	m := &Masker{}
	for _, c := range creds {
		if c.Password == "" {
			continue
		}
		quoted := strconv.Quote(c.Password)
		for _, f := range []string{
			c.Password,
			jsonSpelling(c.Password, true),
			jsonSpelling(c.Password, false),
			quoted[1 : len(quoted)-1],
			base64.RawStdEncoding.EncodeToString([]byte(c.Username + ":" + c.Password)),
		} {
			if !seen[f] {
				seen[f] = true
				m.forms = append(m.forms, []byte(f))
			}
		}
	}
	// Longest first, so the first form that matches at a place is the longest
	// there: two forms of one length that both match are equal, and held once.
	slices.SortFunc(m.forms, func(a, b []byte) int {
		return cmp.Or(cmp.Compare(len(b), len(a)), bytes.Compare(a, b))
	})
	return m
}

// jsonSpelling returns s as encoding/json writes it inside a string's quotes,
// with HTML escaping (as json.Marshal does) or without it.
func jsonSpelling(s string, html bool) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(html)
	if err := e.Encode(s); err != nil {
		// encoding/json refuses no string, and a bytes.Buffer fails no write.
		panic("mask: encoding/json refused a string: " + err.Error())
	}
	t := b.String() // "…" and a newline
	return t[1 : len(t)-2]
}

// String returns s with each complete form replaced by Replacement, read left
// to right, taking the longest form that begins at each place; what was put in
// is not read again. A nil Masker, or one with no form, returns s.
func (m *Masker) String(s string) string {
	if m.empty() {
		return s
	}
	out, _ := m.scan(nil, []byte(s), len(s))
	return string(out)
}

// Writer returns a writer that masks what passes through it into w, a form
// split across writes included. It holds back fewer bytes than the longest
// form until more come; Close writes them, masked where a form is whole, and
// then closes w. An error of w is returned by the call that met it. A nil
// Masker returns w itself.
func (m *Masker) Writer(w io.WriteCloser) io.WriteCloser {
	if m.empty() {
		return w
	}
	return &writer{m: m, w: w}
}

func (m *Masker) empty() bool { return m == nil || len(m.forms) == 0 }

// scan appends src to dst, deciding each place of src before end: the longest
// form that begins there is replaced and the scan goes on after it, or else
// the byte is kept. A place is decided by the bytes from it to the end of src
// alone. It returns dst and the first place it did not decide, which is end
// or, after a form that reaches past end, the place after that form.
func (m *Masker) scan(dst, src []byte, end int) ([]byte, int) {
	i := 0
next:
	for i < end {
		for _, f := range m.forms {
			if bytes.HasPrefix(src[i:], f) {
				dst = append(dst, Replacement...)
				i += len(f)
				continue next
			}
		}
		dst = append(dst, src[i])
		i++
	}
	return dst, i
}

// writer is the stream of Writer. held is what it has not decided yet: fewer
// bytes than the longest form. It is not safe for concurrent use.
type writer struct {
	m    *Masker
	w    io.WriteCloser
	held []byte
	out  []byte // reused for each write's masked bytes
}

// Write decides each place from which the longest form would still fit in
// what it holds, which a later byte cannot change, writes the result to w and
// keeps the rest.
func (x *writer) Write(p []byte) (int, error) {
	x.held = append(x.held, p...)
	var next int
	x.out, next = x.m.scan(x.out[:0], x.held, len(x.held)-len(x.m.forms[0])+1)
	x.held = x.held[:copy(x.held, x.held[next:])]
	if len(x.out) > 0 {
		if _, err := x.w.Write(x.out); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// Close decides what it holds to the end, as String would, writes it to w and
// closes w. It closes w also when that write fails, and returns the write's
// error then.
func (x *writer) Close() error {
	out, _ := x.m.scan(x.out[:0], x.held, len(x.held))
	x.held = x.held[:0]
	var err error
	if len(out) > 0 {
		_, err = x.w.Write(out)
	}
	if cerr := x.w.Close(); err == nil {
		err = cerr
	}
	return err
}
