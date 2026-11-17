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
			obs: verdict.Observation{Written: written, T: written, N: 1, Class: "refused", Ms: 12.5, Lag: 3}},
		{raw: `   {"ok":true}`, kind: Observed, obs: pass},
		{raw: "\t{\"ok\":true}   ", kind: Observed, obs: pass},
		{raw: "{\"ok\":true}\r", kind: Observed, obs: pass},
		{raw: `{"ok":true,"n":200,"t":"2026-11-13T10:00:07.120Z","ms":41,"lag":6}`, kind: Observed,
			obs: verdict.Observation{Written: written, T: given, OK: true, N: 200, Ms: 41, Lag: 6}},
		{raw: `{"ok":true,"t":"2026-11-13T11:00:07.12+01:00"}`, kind: Observed,
			obs: verdict.Observation{Written: written, T: given, OK: true, N: 1}},
		{raw: `{"ok":true,"t":"2026-11-13T10:00:07.5Z"}`, kind: Observed, obs: pass},
		{raw: `{"ok":true,"ms":0,"lag":0}`, kind: Observed, obs: pass},

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
			got := ParseLine(tc.raw, written)
			if got.Kind != tc.kind {
				t.Fatalf("Kind = %v, want %v (fault %q)", got.Kind, tc.kind, got.Fault)
			}
			switch tc.kind {
			case Observed:
				o := got.Obs
				if !o.Written.Equal(tc.obs.Written) || !o.T.Equal(tc.obs.T) || o.OK != tc.obs.OK ||
					o.N != tc.obs.N || o.Class != tc.obs.Class || o.Ms != tc.obs.Ms || o.Lag != tc.obs.Lag {
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
