package probe

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHTTPArgs(t *testing.T) {
	o := HTTPOptions{
		URL:       "http://orders.orders.svc/api/orders/latest",
		Headers:   map[string]string{"X-Query": "a=b", "Host": "orders.example"},
		Rate:      50,
		Timeout:   500 * time.Millisecond,
		ExpectMin: 200,
		ExpectMax: 200,
	}
	want := []string{"probe", "http",
		"--url", "http://orders.orders.svc/api/orders/latest",
		"--rate", "50",
		"--timeout", "500ms",
		"--expect-status", "200-200",
		"--header", "Host=orders.example",
		"--header", "X-Query=a=b",
	}
	if got := HTTPArgs(o); !reflect.DeepEqual(got, want) {
		t.Errorf("HTTPArgs:\ngot  %q\nwant %q", got, want)
	}
}

func TestHTTPArgsRoundTrip(t *testing.T) {
	for _, o := range []HTTPOptions{
		{URL: "http://orders.orders.svc/api/orders/latest", Rate: 200, Timeout: 2 * time.Second, ExpectMin: 200, ExpectMax: 299},
		{URL: "http://orders.orders.svc/", Headers: map[string]string{"Host": "orders.example", "X-Query": "a=b"},
			Rate: 50, Timeout: 500 * time.Millisecond, ExpectMin: 200, ExpectMax: 200},
		{URL: "https://orders.orders.svc:8443/health?full=1", Rate: 1, Timeout: 90 * time.Second, ExpectMin: 200, ExpectMax: 399},
	} {
		got, err := ParseHTTPArgs(HTTPArgs(o))
		if err != nil {
			t.Errorf("ParseHTTPArgs(HTTPArgs(%+v)): %v", o, err)
			continue
		}
		if !reflect.DeepEqual(got, o) {
			t.Errorf("round trip:\ngot  %+v\nwant %+v", got, o)
		}
	}
}

func TestParseHTTPArgsRejects(t *testing.T) {
	base := []string{"probe", "http", "--url", "http://orders.orders.svc/", "--rate", "200", "--timeout", "2s", "--expect-status", "200-299"}
	// with gives base with the value after flag replaced, or without the flag when value is "".
	with := func(flag, value string) []string {
		var args []string
		for i := 0; i < len(base); i++ {
			if base[i] == flag {
				if value != "" {
					args = append(args, flag, value)
				}
				i++
				continue
			}
			args = append(args, base[i])
		}
		return args
	}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"no --url", with("--url", "")},
		{"rate 0", with("--rate", "0")},
		{"a negative rate", with("--rate", "-5")},
		{"a timeout that does not parse", with("--timeout", "2 s")},
		{"a timeout of zero", with("--timeout", "0s")},
		{"expect-status with min above max", with("--expect-status", "299-200")},
		{"expect-status that is not a range", with("--expect-status", "2xx")},
		{"expect-status with one code", with("--expect-status", "200")},
		{"a header with no =", append(with("--url", "http://orders.orders.svc/"), "--header", "Host")},
		{"a flag it does not know", append(with("--url", "http://orders.orders.svc/"), "--keep-alive")},
		{"not the probe http command", base[2:]},
	} {
		if _, err := ParseHTTPArgs(tc.args); err == nil {
			t.Errorf("%s: %q was accepted", tc.name, strings.Join(tc.args, " "))
		}
	}
}
