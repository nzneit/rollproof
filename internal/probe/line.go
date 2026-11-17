// Package probe is the probe contract of section 6 as the tool keeps it: reading a probe's
// lines, running a probe as a pod, and the built-in HTTP probe. It hides the pod, its log stream
// and the line format. It judges nothing: it hands observations and faults to its caller.
package probe

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"

	"rollproof/internal/verdict"
)

// Kind is what one line of a probe's output is: Observed (it carries ok), Text (log text),
// JSONText (a JSON object with no ok field) or Invalid (it breaks the contract).
type Kind int

// Kinds of a line. The zero Kind is no kind.
const (
	Observed Kind = iota + 1
	Text
	JSONText
	Invalid
)

// Line is one line as the contract reads it. Obs is set for Observed. Fault says, for Invalid,
// which rule the line broke; it names the field and never quotes the line.
type Line struct {
	Kind  Kind
	Obs   verdict.Observation
	Fault string
}

var okBehindPrefix = regexp.MustCompile(`"ok"\s*:`)

func invalid(fault string) Line { return Line{Kind: Invalid, Fault: fault} }

// ParseLine reads one whole line of a probe's output. raw has no line end. written is the
// runtime's timestamp of the line; it becomes the observation's Written and, when the line has no
// t, its T. A t later than written is Invalid.
func ParseLine(raw string, written time.Time) Line {
	s := strings.TrimLeftFunc(raw, unicode.IsSpace)
	if !strings.HasPrefix(s, "{") {
		if okBehindPrefix.MatchString(s) {
			return invalid(`an observation behind a prefix: "ok" and a colon in log text`)
		}
		return Line{Kind: Text}
	}

	dec := json.NewDecoder(strings.NewReader(s))
	var fields map[string]json.RawMessage
	if err := dec.Decode(&fields); err != nil {
		return invalid("the line is not one JSON object")
	}
	if _, err := dec.Token(); err != io.EOF {
		return invalid("the line is not one JSON object")
	}
	if hasDuplicateName(s) {
		return invalid("a top-level name is there twice")
	}

	okRaw, has := fields["ok"]
	if !has {
		return Line{Kind: JSONText}
	}
	obs := verdict.Observation{Written: written, T: written, N: 1}
	if !unmarshalStrict(okRaw, &obs.OK) {
		return invalid("ok is not true or false")
	}

	if r, ok := fields["t"]; ok {
		var ts string
		if !unmarshalStrict(r, &ts) {
			return invalid("t is not a string")
		}
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			return invalid("t is not RFC 3339 with a zone")
		}
		if t.After(written) {
			return invalid("t is later than the line")
		}
		obs.T = t
	}

	if r, ok := fields["n"]; ok {
		if !obs.OK {
			return invalid("n is not allowed beside ok false")
		}
		var n int
		if !isNumber(r) || !unmarshalStrict(r, &n) || n < 1 {
			return invalid("n is not an integer of 1 or more")
		}
		obs.N = n
	}

	if r, ok := fields["class"]; ok {
		if !unmarshalStrict(r, &obs.Class) {
			return invalid("class is not a string")
		}
	}
	if r, ok := fields["ms"]; ok {
		if !isNumber(r) || !unmarshalStrict(r, &obs.Ms) || obs.Ms < 0 {
			return invalid("ms is not a number of 0 or more")
		}
	}
	if r, ok := fields["lag"]; ok {
		if !isNumber(r) || !unmarshalStrict(r, &obs.Lag) || obs.Lag < 0 {
			return invalid("lag is not a number of 0 or more")
		}
	}
	if r, ok := fields["v"]; ok {
		var v int
		if !isNumber(r) || !unmarshalStrict(r, &v) || v != 1 {
			return invalid("v is not 1, the one version of the contract this tool reads")
		}
	}
	return Line{Kind: Observed, Obs: obs}
}

// unmarshalStrict decodes raw into v and reports whether it fit. A JSON null never fits: the
// standard library would accept it and change nothing.
func unmarshalStrict(raw json.RawMessage, v any) bool {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return false
	}
	return json.Unmarshal(raw, v) == nil
}

// isNumber reports whether raw is a JSON number, so that a string such as "3" is not taken
// for one.
func isNumber(raw json.RawMessage) bool {
	b := bytes.TrimSpace(raw)
	return len(b) > 0 && (b[0] == '-' || (b[0] >= '0' && b[0] <= '9'))
}

// hasDuplicateName reports whether a top-level name of the JSON object s is there twice.
// encoding/json would keep the last of them without a word. s is already known to be one
// valid object.
func hasDuplicateName(s string) bool {
	dec := json.NewDecoder(strings.NewReader(s))
	if _, err := dec.Token(); err != nil { // the opening brace
		return false
	}
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		name, _ := tok.(string)
		if seen[name] {
			return true
		}
		seen[name] = true
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return false
		}
	}
	return false
}
