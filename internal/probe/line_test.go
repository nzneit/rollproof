package probe

import (
	"strings"
	"testing"
	"time"

	"rollproof/internal/verdict"
)

func TestParseLine(t *testing.T) {
	written := time.Date(2026, 11, 13, 10, 0, 7, 500_000_000, time.UTC)
	given := time.Date(2026, 11, 13, 10, 0, 7, 120_000_000, time.UTC)
	pass := verdict.Observation{Written: written, T: written, OK: true, N: 1}
	tests := []struct {
		raw   string
		kind  Kind
		obs   verdict.Observation // for Observed
		fault string              // for Invalid: a part of Fault
	}{
		{raw: `{"ok":true}`, kind: Observed, obs: pass},
		{raw: `{"ok":false,"class":"refused","ms":12.5,"lag":3}`, kind: Observed,
			obs: verdict.Observation{Written: written, T: written, N: 1, Class: "refused", Ms: 12.5, Lag: 3, HasMs: true, HasLag: true}},
		{raw: `   {"ok":true}`, kind: Observed, obs: pass},
		{raw: "\t{\"ok\":true}   ", kind: Observed, obs: pass},
		{raw: "{\"ok\":true}\r", kind: Observed, obs: pass},
		{raw: `{"ok":true,"n":200,"t":"2026-11-13T10:00:07.120Z","ms":41,"lag":6}`, kind: Observed,
			obs: verdict.Observation{Written: written, T: given, OK: true, N: 200, Ms: 41, Lag: 6, HasMs: true, HasLag: true}},
		{raw: `{"ok":true,"t":"2026-11-13T11:00:07.12+01:00"}`, kind: Observed,
			obs: verdict.Observation{Written: written, T: given, OK: true, N: 1}},
		{raw: `{"ok":true,"t":"2026-11-13T10:00:07.5Z"}`, kind: Observed, obs: pass},
		{raw: `{"ok":true,"ms":0,"lag":0}`, kind: Observed,
			obs: verdict.Observation{Written: written, T: written, OK: true, N: 1, HasMs: true, HasLag: true}},

		// Log text.
		{raw: `starting`, kind: Text},
		{raw: `2026/11/13 10:00:00 listening`, kind: Text},
		{raw: `  ok: ready`, kind: Text},
		{raw: `the word "ok" and no colon`, kind: Text},
		{raw: ``, kind: Text},
		{raw: `   `, kind: Text},

		// An observation behind a prefix.
		{raw: `2026-11-13 10:00:07 {"ok":false}`, kind: Invalid, fault: "behind a prefix"},
		{raw: `INFO "ok" : true`, kind: Invalid, fault: "behind a prefix"},
		{raw: "level=info \"ok\"\t:false", kind: Invalid, fault: "behind a prefix"},

		// Not one JSON object.
		{raw: `{"ok":tru`, kind: Invalid, fault: "not one JSON object"},
		{raw: `{"ok":true} trailing`, kind: Invalid, fault: "not one JSON object"},
		{raw: `{"ok":true}{"ok":true}`, kind: Invalid, fault: "not one JSON object"},
		{raw: `{`, kind: Invalid, fault: "not one JSON object"},

		// JSON objects with no top-level ok are log text.
		{raw: `{"level":"info","msg":"x"}`, kind: JSONText},
		{raw: `{"detail":{"ok":true}}`, kind: JSONText},
		{raw: `{"msg":"the \"ok\": field"}`, kind: JSONText},
		{raw: `{}`, kind: JSONText},

		// ok.
		{raw: `{"ok":"yes"}`, kind: Invalid, fault: "ok is not"},
		{raw: `{"ok":1}`, kind: Invalid, fault: "ok is not"},
		{raw: `{"ok":null}`, kind: Invalid, fault: "ok is not"},
		{raw: `{"ok":"S3CRET-VALUE"}`, kind: Invalid, fault: "ok is not"},

		// n.
		{raw: `{"ok":true,"n":0}`, kind: Invalid, fault: "n is not an integer of 1 or more"},
		{raw: `{"ok":true,"n":-1}`, kind: Invalid, fault: "n is not an integer of 1 or more"},
		{raw: `{"ok":true,"n":1.5}`, kind: Invalid, fault: "n is not an integer of 1 or more"},
		{raw: `{"ok":true,"n":"3"}`, kind: Invalid, fault: "n is not an integer of 1 or more"},
		{raw: `{"ok":true,"n":null}`, kind: Invalid, fault: "n is not an integer of 1 or more"},
		{raw: `{"ok":false,"n":1}`, kind: Invalid, fault: "n is not allowed beside ok false"},
		{raw: `{"ok":false,"n":5}`, kind: Invalid, fault: "n is not allowed beside ok false"},

		// t.
		{raw: `{"ok":true,"t":12}`, kind: Invalid, fault: "t is not a string"},
		{raw: `{"ok":true,"t":"yesterday"}`, kind: Invalid, fault: "t is not RFC 3339"},
		{raw: `{"ok":true,"t":"2026-11-13T10:00:07"}`, kind: Invalid, fault: "t is not RFC 3339"},
		{raw: `{"ok":true,"t":"2026-11-13T10:00:07.500000001Z"}`, kind: Invalid, fault: "t is later than the line"},
		{raw: `{"ok":false,"t":"2026-11-14T10:00:00Z"}`, kind: Invalid, fault: "t is later than the line"},

		// class, ms, lag.
		{raw: `{"ok":false,"class":5}`, kind: Invalid, fault: "class is not a string"},
		{raw: `{"ok":true,"ms":-1}`, kind: Invalid, fault: "ms is not a number of 0 or more"},
		{raw: `{"ok":true,"ms":"41"}`, kind: Invalid, fault: "ms is not a number of 0 or more"},
		{raw: `{"ok":true,"ms":null}`, kind: Invalid, fault: "ms is not a number of 0 or more"},
		{raw: `{"ok":true,"lag":-0.5}`, kind: Invalid, fault: "lag is not a number of 0 or more"},
		{raw: `{"ok":true,"lag":true}`, kind: Invalid, fault: "lag is not a number of 0 or more"},

		// detail is never read.
		{raw: `{"ok":true,"detail":"text"}`, kind: Observed, obs: pass},
		{raw: `{"ok":true,"detail":[1,{"ok":"x"}]}`, kind: Observed, obs: pass},
		{raw: `{"ok":true,"detail":null}`, kind: Observed, obs: pass},

		// v.
		{raw: `{"ok":true,"v":1}`, kind: Observed, obs: pass},
		{raw: `{"ok":true,"v":2}`, kind: Invalid, fault: "v is not 1"},
		{raw: `{"ok":true,"v":"1"}`, kind: Invalid, fault: "v is not 1"},
		{raw: `{"ok":true,"v":1.5}`, kind: Invalid, fault: "v is not 1"},

		// A top-level name the tool does not know is ignored.
		{raw: `{"ok":true,"extra":1}`, kind: Observed, obs: pass},

		// A top-level name that is there twice makes the line invalid, so
		// that a failure cannot be hidden behind a later "ok": true. Inside
		// detail the tool reads nothing, so a repeated name there is not seen.
		{raw: `{"ok":false,"ok":true}`, kind: Invalid, fault: "a top-level name is there twice"},
		{raw: `{"ok":true,"class":"a","class":"b"}`, kind: Invalid, fault: "a top-level name is there twice"},
		{raw: `{"ok":true,"detail":{"a":1,"a":2}}`, kind: Observed, obs: pass},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			got := ParseLine(tc.raw, written, time.Time{})
			if got.Kind != tc.kind {
				t.Fatalf("Kind = %v, want %v (fault %q)", got.Kind, tc.kind, got.Fault)
			}
			switch tc.kind {
			case Observed:
				o := got.Obs
				if !o.Written.Equal(tc.obs.Written) || !o.T.Equal(tc.obs.T) || o.OK != tc.obs.OK ||
					o.N != tc.obs.N || o.Class != tc.obs.Class || o.Ms != tc.obs.Ms || o.Lag != tc.obs.Lag ||
					o.HasMs != tc.obs.HasMs || o.HasLag != tc.obs.HasLag {
					t.Errorf("Obs = %+v, want %+v", o, tc.obs)
				}
			case Invalid:
				if !strings.Contains(got.Fault, tc.fault) {
					t.Errorf("Fault = %q, want it to hold %q", got.Fault, tc.fault)
				}
				if strings.Contains(got.Fault, strings.TrimSpace(tc.raw)) || strings.Contains(got.Fault, "S3CRET") {
					t.Errorf("Fault %q quotes the line", got.Fault)
				}
			default:
				if got.Fault != "" || got.Obs != (verdict.Observation{}) {
					t.Errorf("a %v line carries %+v", got.Kind, got)
				}
			}
		})
	}
}

func TestParseLineFloorOfT(t *testing.T) {
	asked := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	written := asked.Add(30 * time.Second)
	line := func(tt time.Time) string { return `{"ok":false,"t":"` + tt.Format(time.RFC3339Nano) + `"}` }
	for _, c := range []struct {
		name  string
		t     time.Time
		asked time.Time
		kind  Kind
	}{
		{"after the ask", asked.Add(time.Second), asked, Observed},
		{"less than the bound before the ask", asked.Add(-time.Second), asked, Observed},
		{"the bound before the ask", asked.Add(-ClockBound), asked, Observed},
		{"more than the bound before the ask", asked.Add(-ClockBound - time.Nanosecond), asked, Invalid},
		{"an hour before the ask", asked.Add(-time.Hour), asked, Invalid},
		{"no ask known", asked.Add(-time.Hour), time.Time{}, Observed},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := ParseLine(line(c.t), written, c.asked)
			if got.Kind != c.kind {
				t.Fatalf("Kind = %v, want %v (fault %q)", got.Kind, c.kind, got.Fault)
			}
			if c.kind == Invalid && got.Fault != "t is more than 2s earlier than the moment the tool asked for the probe's pod" {
				t.Errorf("Fault = %q", got.Fault)
			}
			if !got.FailedCheck {
				t.Error("FailedCheck = false for a line whose ok is false")
			}
		})
	}
}

func TestParseLineHasMsAndLag(t *testing.T) {
	written := time.Date(2026, 11, 13, 10, 0, 7, 500_000_000, time.UTC)
	for _, c := range []struct {
		raw           string
		hasMs, hasLag bool
		ms, lag       float64
	}{
		{`{"ok":true}`, false, false, 0, 0},
		{`{"ok":true,"ms":0,"lag":0}`, true, true, 0, 0},
		{`{"ok":false,"ms":12.5}`, true, false, 12.5, 0},
		{`{"ok":true,"n":200,"lag":6}`, false, true, 0, 6},
	} {
		t.Run(c.raw, func(t *testing.T) {
			got := ParseLine(c.raw, written, time.Time{})
			if got.Kind != Observed {
				t.Fatalf("Kind = %v, want Observed (fault %q)", got.Kind, got.Fault)
			}
			o := got.Obs
			if o.HasMs != c.hasMs || o.HasLag != c.hasLag || o.Ms != c.ms || o.Lag != c.lag {
				t.Errorf("HasMs %v, HasLag %v, Ms %v, Lag %v; want %v, %v, %v, %v",
					o.HasMs, o.HasLag, o.Ms, o.Lag, c.hasMs, c.hasLag, c.ms, c.lag)
			}
		})
	}
}

func TestParseLineFailedCheck(t *testing.T) {
	written := time.Date(2026, 11, 13, 10, 0, 7, 500_000_000, time.UTC)
	for _, c := range []struct {
		raw    string
		kind   Kind
		failed bool
	}{
		{`{"ok":false}`, Observed, true},
		{`{"ok":true}`, Observed, false},
		{`{"ok":false,"n":2}`, Invalid, true},
		{`{"ok":false,"t":"2026-11-14T10:00:00Z"}`, Invalid, true},
		{`{"ok":false,"ms":-1}`, Invalid, true},
		{`{"ok":false,"v":2}`, Invalid, true},
		{`{"ok":false,"ok":true}`, Invalid, true},
		{`{"ok":true,"ok":false}`, Invalid, true},
		{`{"ok":true,"ok":true}`, Invalid, false},
		{`{"ok":true,"n":0}`, Invalid, false},
		{`{"ok":"false"}`, Invalid, false},
		{`{"ok":null,"ok":false}`, Invalid, true},
		{`{"ok":false} trailing`, Invalid, false},
		{`{"ok":fals`, Invalid, false},
		{`2026-11-13 10:00:07 {"ok":false}`, Invalid, false},
		{`{"detail":{"ok":false}}`, JSONText, false},
		{`starting`, Text, false},
	} {
		t.Run(c.raw, func(t *testing.T) {
			got := ParseLine(c.raw, written, time.Time{})
			if got.Kind != c.kind || got.FailedCheck != c.failed {
				t.Errorf("Kind %v, FailedCheck %v; want %v, %v", got.Kind, got.FailedCheck, c.kind, c.failed)
			}
		})
	}
}
