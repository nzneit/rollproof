// Package probe is the probe contract of section 6 as the tool keeps it: reading a probe's
// lines, running a probe as a pod, and the built-in HTTP probe. It hides the pod, its log stream
// and the line format. It judges nothing: it hands observations and faults to its caller.
package probe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/nzneit/rollproof/internal/verdict"
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
// which rule the line broke; it names the field and never quotes the line. FailedCheck says the
// line is one JSON object, on a line of its own, with a top-level ok that is false, or with ok
// twice and one of them false. It is set whatever the Kind, so an Invalid line says whether it
// was a failed check's.
type Line struct {
	Kind        Kind
	Obs         verdict.Observation
	Fault       string
	FailedCheck bool
}

var okBehindPrefix = regexp.MustCompile(`"ok"\s*:`)

func invalid(fault string) Line { return Line{Kind: Invalid, Fault: fault} }

// ParseLine reads one whole line of a probe's output. text has no line end. written is the
// runtime's timestamp of the line; it becomes the observation's Written and, when the line has no
// t, its T. A t later than written is Invalid.
//
// asked is the moment the tool asked for the probe's pod, on the tool's clock. A line whose t is
// earlier than asked by more than ClockBound is Invalid, because no check can have begun before
// the probe existed (section 14, point 29); a t exactly ClockBound before asked is not. A zero
// asked skips this check. It is made here, not in the follower, because ParseLine is the one
// reader of a line.
func ParseLine(text string, written, asked time.Time) Line {
	s := strings.TrimLeftFunc(text, unicode.IsSpace)
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
	failed := failedCheck(s)
	bad := func(fault string) Line { return Line{Kind: Invalid, Fault: fault, FailedCheck: failed} }
	if hasDuplicateName(s) {
		return bad("a top-level name is there twice")
	}

	okRaw, has := fields["ok"]
	if !has {
		return Line{Kind: JSONText, FailedCheck: failed}
	}
	obs := verdict.Observation{Written: written, T: written, N: 1}
	if !unmarshalStrict(okRaw, &obs.OK) {
		return bad("ok is not true or false")
	}

	if r, ok := fields["t"]; ok {
		var ts string
		if !unmarshalStrict(r, &ts) {
			return bad("t is not a string")
		}
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			return bad("t is not RFC 3339 with a zone")
		}
		if t.After(written) {
			return bad("t is later than the line")
		}
		if !asked.IsZero() && t.Before(asked.Add(-ClockBound)) {
			return bad(fmt.Sprintf("t is more than %v earlier than the moment the tool asked for the probe's pod", ClockBound))
		}
		obs.T = t
	}

	if r, ok := fields["n"]; ok {
		if !obs.OK {
			return bad("n is not allowed beside ok false")
		}
		var n int
		if !isNumber(r) || !unmarshalStrict(r, &n) || n < 1 {
			return bad("n is not an integer of 1 or more")
		}
		obs.N = n
	}

	if r, ok := fields["class"]; ok {
		if !unmarshalStrict(r, &obs.Class) {
			return bad("class is not a string")
		}
	}
	if r, ok := fields["ms"]; ok {
		if !isNumber(r) || !unmarshalStrict(r, &obs.Ms) || obs.Ms < 0 {
			return bad("ms is not a number of 0 or more")
		}
		obs.HasMs = true
	}
	if r, ok := fields["lag"]; ok {
		if !isNumber(r) || !unmarshalStrict(r, &obs.Lag) || obs.Lag < 0 {
			return bad("lag is not a number of 0 or more")
		}
		obs.HasLag = true
	}
	if r, ok := fields["v"]; ok {
		var v int
		if !isNumber(r) || !unmarshalStrict(r, &v) || v != 1 {
			return bad("v is not 1, the one version of the contract this tool reads")
		}
	}
	return Line{Kind: Observed, Obs: obs, FailedCheck: failed}
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

// failedCheck reports whether a top-level ok of the JSON object s is the JSON literal false. s is
// already known to be one valid object, with nothing after it. The names are walked as
// hasDuplicateName walks them, so that a repeated ok is read, not just the last of them.
func failedCheck(s string) bool {
	dec := json.NewDecoder(strings.NewReader(s))
	if _, err := dec.Token(); err != nil { // the opening brace
		return false
	}
	failed := false
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return failed
		}
		name, _ := tok.(string)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return failed
		}
		if name == "ok" && bytes.Equal(bytes.TrimSpace(value), []byte("false")) {
			failed = true
		}
	}
	return failed
}
