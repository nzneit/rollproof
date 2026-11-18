package probe

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
)

// HTTPOptions are the settings of the built-in probe. Rate is requests a second, 1 or more.
// ExpectMin and ExpectMax are the status range that passes, both ends included. A Host entry of
// Headers sets the host the request names. The probe has no warm-up: it reports from its first
// request on.
type HTTPOptions struct {
	URL       string
	Headers   map[string]string
	Rate      int
	Timeout   time.Duration
	ExpectMin int
	ExpectMax int
}

// HTTPArgs gives the arguments that make the binary's probe http command run o: what the probe's
// pod is started with.
func HTTPArgs(o HTTPOptions) []string {
	args := []string{"probe", "http",
		"--url", o.URL,
		"--rate", strconv.Itoa(o.Rate),
		"--timeout", o.Timeout.String(),
		"--expect-status", fmt.Sprintf("%d-%d", o.ExpectMin, o.ExpectMax),
	}
	for _, name := range slices.Sorted(maps.Keys(o.Headers)) {
		args = append(args, "--header", name+"="+o.Headers[name])
	}
	return args
}

// headerFlag is a flag.Value that may repeat, one NAME=VALUE for each use.
type headerFlag map[string]string

func (h *headerFlag) String() string { return "" }

func (h *headerFlag) Set(s string) error {
	name, value, ok := strings.Cut(s, "=")
	if !ok || name == "" {
		return errors.New("want NAME=VALUE")
	}
	if *h == nil {
		*h = headerFlag{}
	}
	(*h)[name] = value
	return nil
}

// ParseHTTPArgs is the reverse of HTTPArgs: for every o, ParseHTTPArgs(HTTPArgs(o)) gives o back,
// except that an empty Headers map comes back nil, because no --header is written for one.
func ParseHTTPArgs(args []string) (HTTPOptions, error) {
	if len(args) < 2 || args[0] != "probe" || args[1] != "http" {
		return HTTPOptions{}, errors.New("the arguments are not those of the probe http command")
	}
	fs := flag.NewFlagSet("probe http", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		url     = fs.String("url", "", "")
		rate    = fs.Int("rate", 0, "")
		timeout = fs.String("timeout", "", "")
		expect  = fs.String("expect-status", "", "")
		headers headerFlag
	)
	fs.Var(&headers, "header", "")
	if err := fs.Parse(args[2:]); err != nil {
		return HTTPOptions{}, fmt.Errorf("probe http: %w", err)
	}
	if fs.NArg() > 0 {
		return HTTPOptions{}, errors.New("probe http: an argument is left over that no flag takes")
	}
	if *url == "" {
		return HTTPOptions{}, errors.New("--url is missing")
	}
	if *rate < 1 {
		return HTTPOptions{}, errors.New("--rate is not 1 or more")
	}
	d, err := time.ParseDuration(*timeout)
	if err != nil || d <= 0 {
		return HTTPOptions{}, errors.New("--timeout is not a duration above zero")
	}
	lo, hi, ok := strings.Cut(*expect, "-")
	min, err1 := strconv.Atoi(lo)
	max, err2 := strconv.Atoi(hi)
	if !ok || err1 != nil || err2 != nil || min > max {
		return HTTPOptions{}, errors.New("--expect-status is not MIN-MAX with MIN not above MAX")
	}
	return HTTPOptions{URL: *url, Headers: headers, Rate: *rate, Timeout: d, ExpectMin: min, ExpectMax: max}, nil
}
