package report

import (
	"cmp"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// detailWidth is how wide a reason's detail line or an install restart line may be, indent
// included, before it wraps. It was the widest line of section 10's examples when it was picked;
// the install's checks line, at 99 characters, is written whole.
const detailWidth = 92

// notExercised is what a pass did not exercise, in the design's order.
var notExercised = []string{
	"the delay before a larger cluster stops routing to a stopping pod",
	"connections held open, and requests in flight on a pod after new connections left it",
	"the path through an ingress, a load balancer or a mesh",
	"routing that depends on where the client is",
	"capacity while pods are down, and autoscaling",
	"storage that attaches to one node at a time",
	"latency below the timeout",
	"data migrations",
	"a node drain",
}

// Console returns the text a run prints at its end, as section 10 shows it:
// the verdict line, the releases, images, probe and install, one row for each
// window and for each workload of each transition, every reason with its code,
// the lists of what a pass exercised and did not, every warning, and the
// results folder. It prints what the run reached: the images' and the probe's
// lines once the record has a probe, the install's lines once the install has a
// first observation, and the windows' table once a window of the list began. It
// stays a pure function of r: no clock, no environment, no terminal width.
func Console(r Run) string {
	r = cleanRun(r)
	var blocks [][]string
	blocks = append(blocks, []string{verdictLine(r)})
	blocks = append(blocks, setUp(r))
	if windowStarted(r) {
		blocks = append(blocks, windowsTable(r))
	}
	blocks = append(blocks, transitionsTable(r))
	if r.Verdict == "pass" {
		blocks = append(blocks, exercisedLists(r))
	}
	blocks = append(blocks, last(r))

	var b strings.Builder
	first := true
	for _, block := range blocks {
		if len(block) == 0 {
			continue
		}
		if !first {
			b.WriteString("\n")
		}
		first = false
		for _, line := range block {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// clean writes s as text for one line of a terminal: CR LF, CR and LF each
// become one space, and so does every other control character (category Cc),
// the line and paragraph separators, and the bidirectional controls. Text from
// the cluster, from commands and from the configuration is cleaned with it
// before it is printed, so that none of it can redraw or split a line.
func clean(s string) string {
	if !strings.ContainsFunc(s, isControl) {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", " ")
	return strings.Map(func(r rune) rune {
		if isControl(r) {
			return ' '
		}
		return r
	}, s)
}

func isControl(r rune) bool {
	return unicode.Is(unicode.Cc, r) || r == '\u2028' || r == '\u2029' ||
		(r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069')
}

func cleanAll(s []string) []string {
	out := make([]string, len(s))
	for i, x := range s {
		out[i] = clean(x)
	}
	return out
}

func cleanSettings(s []Setting) []Setting {
	out := make([]Setting, len(s))
	for i, x := range s {
		out[i] = Setting{Key: clean(x.Key), Value: clean(x.Value), Default: clean(x.Default)}
	}
	return out
}

// cleanClasses cleans the names of the classes. Classes whose names clean to one name are one
// class, with their counts added, so that the counts still sum to the failed checks.
func cleanClasses(m map[string]int) map[string]int {
	if m == nil {
		return nil
	}
	out := make(map[string]int, len(m))
	for name, n := range m {
		out[clean(name)] += n
	}
	return out
}

// cleanRun is a copy of r with every string that the console prints cleaned.
// Console prints only from this copy, so no print site can miss a field.
func cleanRun(r Run) Run {
	c := r
	c.Verdict, c.Driver, c.Name, c.Out = clean(r.Verdict), clean(r.Driver), clean(r.Name), clean(r.Out)
	c.RemovedVariables, c.Warnings = cleanAll(r.RemovedVariables), cleanAll(r.Warnings)
	c.Settings = cleanSettings(r.Settings)
	c.Reasons = nil
	for _, x := range r.Reasons {
		x.Code, x.Verdict, x.Window, x.Text, x.Detail = clean(x.Code), clean(x.Verdict), clean(x.Window), clean(x.Text), cleanAll(x.Detail)
		c.Reasons = append(c.Reasons, x)
	}
	c.Windows = nil
	for _, x := range r.Windows {
		x.Name, x.Result = clean(x.Name), clean(x.Result)
		c.Windows = append(c.Windows, x)
	}
	c.Transitions = nil
	for _, x := range r.Transitions {
		x.Name = clean(x.Name)
		x.Workloads = nil
		for _, w := range r.Transitions[len(c.Transitions)].Workloads {
			w.Workload, w.Probed = clean(w.Workload), clean(w.Probed)
			x.Workloads = append(x.Workloads, w)
		}
		c.Transitions = append(c.Transitions, x)
	}
	c.Probes = nil
	for _, x := range r.Probes {
		x.Kind, x.Target = clean(x.Kind), clean(x.Target)
		x.Settings, x.Notes = cleanSettings(x.Settings), cleanAll(x.Notes)
		c.Probes = append(c.Probes, x)
	}
	c.Releases = nil
	for _, x := range r.Releases {
		x.Role, x.Chart, x.Version, x.Source = clean(x.Role), clean(x.Chart), clean(x.Version), clean(x.Source)
		x.ValuesFiles, x.InlineKeys = cleanAll(x.ValuesFiles), cleanAll(x.InlineKeys)
		c.Releases = append(c.Releases, x)
	}
	c.Images = nil
	for _, x := range r.Images {
		x.Name = clean(x.Name)
		x.RunBy = nil
		for _, b := range r.Images[len(c.Images)].RunBy {
			x.RunBy = append(x.RunBy, RunBy{Workload: clean(b.Workload), Transition: clean(b.Transition)})
		}
		c.Images = append(c.Images, x)
	}
	if r.Cluster != nil {
		cl := *r.Cluster
		cl.Provider, cl.Kubernetes = clean(cl.Provider), clean(cl.Kubernetes)
		c.Cluster = &cl
	}
	if r.Install != nil {
		in := *r.Install
		in.SettleRestartedBy = cleanAll(r.Install.SettleRestartedBy)
		in.FailedByClass = cleanClasses(r.Install.FailedByClass)
		c.Install = &in
	}
	return c
}

func verdictLine(r Run) string {
	word := "NOT VERIFIED"
	switch r.Verdict {
	case "pass":
		word = "PASS"
	case "fail":
		word = "FAIL"
	}
	s := "rollproof: " + word
	if r.Name != "" {
		s += "   " + r.Name
		switch r.Driver {
		case "helm":
			s += " (Helm)"
		case "":
		default:
			s += " (" + r.Driver + ")"
		}
	}
	if r.Cluster != nil {
		s += " on Kubernetes " + r.Cluster.Kubernetes + " in " + r.Cluster.Provider
	}
	return s
}

func probeStarted(r Run) bool {
	return len(r.Probes) > 0
}

func windowStarted(r Run) bool {
	for _, w := range r.Windows {
		if w.Start != nil {
			return true
		}
	}
	return false
}

// setUp is the block of releases, images, probe, install, changed settings and
// removed variables. Its first column is 12 characters wide.
func setUp(r Run) []string {
	hasProbe := probeStarted(r)
	var lines []string
	add := func(word, text string) {
		lines = append(lines, pad(word, 12)+text)
	}

	roleWidth := 0
	for _, rel := range r.Releases {
		roleWidth = max(roleWidth, len(rel.Role))
	}
	if hasProbe {
		for _, img := range r.Images {
			roleWidth = max(roleWidth, len(img.Name))
		}
	}
	roleWidth += 2

	for i, rel := range r.Releases {
		word := ""
		if i == 0 {
			word = "releases"
		}
		values := "chart defaults"
		if len(rel.ValuesFiles) > 0 || len(rel.InlineKeys) > 0 {
			parts := append([]string(nil), rel.ValuesFiles...)
			if len(rel.InlineKeys) > 0 {
				parts = append(parts, "inline ("+strings.Join(rel.InlineKeys, ", ")+")")
			}
			values = "values: " + strings.Join(parts, ", ")
		}
		add(word, pad(rel.Role, roleWidth)+rel.Chart+" "+rel.Version+", "+rel.Source+"; "+values)
	}

	if hasProbe {
		for i, img := range r.Images {
			word := ""
			if i == 0 {
				word = "images"
			}
			if len(img.RunBy) == 0 {
				add(word, pad(img.Name, roleWidth)+"run by no watched pod")
				continue
			}
			for j, by := range img.RunBy {
				name := ""
				if j == 0 {
					name = img.Name
				}
				add(word, pad(name, roleWidth)+"run by "+by.Workload+", in pods the "+by.Transition+" created")
				word = ""
			}
		}
		word := "probe"
		for _, p := range r.Probes {
			if p.Kind != "http" {
				continue
			}
			add(word, "built-in HTTP: "+p.Target)
			word = ""
			lines = append(lines, probeLines(p)...)
		}
	}
	lines = append(lines, installLines(r)...)

	var differ []string
	collect := func(s []Setting) {
		for _, x := range s {
			if x.Value != x.Default {
				differ = append(differ, "  "+x.Key+"  "+x.Value+"  (default "+x.Default+")")
			}
		}
	}
	collect(r.Settings)
	for _, p := range r.Probes {
		collect(p.Settings)
	}
	if len(differ) > 0 {
		lines = append(lines, "settings that differ from their defaults")
		lines = append(lines, differ...)
	}
	if len(r.RemovedVariables) > 0 {
		lines = append(lines, "taken out of the commands' environment: "+strings.Join(r.RemovedVariables, ", "))
	}
	return lines
}

var durationWithUnit = regexp.MustCompile(`^([0-9.]+)([a-zµ]+)$`)

// spaced writes a duration with a space before its unit: 2s as 2 s.
func spaced(d string) string {
	if m := durationWithUnit.FindStringSubmatch(d); m != nil {
		return m[1] + " " + m[2]
	}
	return d
}

func settingValue(s []Setting, key string) string {
	for _, x := range s {
		if x.Key == key {
			return x.Value
		}
	}
	return ""
}

// probeLines are the lines under the probe's target, each indented 12 spaces.
func probeLines(p Probe) []string {
	const indent = "            "
	rate := settingValue(p.Settings, "probe.http.rate")
	every := ""
	if n, err := strconv.ParseFloat(rate, 64); err == nil && n > 0 {
		ms := 1000 / n
		if ms == math.Trunc(ms) {
			every = fmt.Sprintf(" (one check every %d ms)", int(ms))
		} else {
			every = fmt.Sprintf(" (one check every %.1f ms)", ms)
		}
	}
	lines := []string{indent + rate + " a second" + every + ", timeout " +
		spaced(settingValue(p.Settings, "probe.http.timeout")) + ", status " +
		settingValue(p.Settings, "probe.http.expect-status")}
	switch {
	case p.JSONLinesWithoutOK == 1:
		lines = append(lines, indent+"1 line was a JSON object with no ok field")
	case p.JSONLinesWithoutOK != 0:
		lines = append(lines, indent+strconv.Itoa(p.JSONLinesWithoutOK)+" lines were JSON objects with no ok field")
	}
	if !p.CertificateChecked {
		lines = append(lines, indent+"the server's certificate was not verified")
	}
	for _, n := range p.Notes {
		lines = append(lines, indent+clean(n))
	}
	if !p.StoppedWhenAsked {
		lines = append(lines, indent+"the probe did not stop when asked; checks still in flight were not reported")
	}
	return lines
}

// installLines are the install's lines: the first has "install" in the first
// column, and every further line is indented 12 spaces. There are none before
// the probe's first observation.
func installLines(r Run) []string {
	in := r.Install
	if in == nil || in.FirstObservation == nil {
		return nil
	}
	const indent = "            "
	head := "had not settled under the probe when the run ended"
	if in.Settled != nil {
		head = fmt.Sprintf("settled after %.1f s under the probe",
			float64(in.Settled.Sub(*in.FirstObservation).Milliseconds())/1000)
	}
	decided := "failures not judged"
	for _, rs := range r.Reasons {
		if rs.Decided && rs.Window == "install" {
			decided = "NOT VERIFIED"
			break
		}
	}
	lines := []string{pad("install", 12) + head + "; " + decided}

	checks := thousands(in.Checks) + " checks"
	if in.Checks == 1 {
		checks = "1 check"
	}
	line := indent + checks + ", " + thousands(in.Failed) + " failed"
	if in.Failed > 0 {
		if classes := classCounts(in.FailedByClass); classes != "" {
			line += " (" + classes + ")"
		}
		switch {
		case in.Settled != nil && in.LastFailure != nil:
			line += fmt.Sprintf(", the last %.1f s before the steady window",
				float64(in.Settled.Sub(*in.LastFailure).Milliseconds())/1000)
		case in.LastFailure != nil:
			line += ", the last at " + in.LastFailure.UTC().Format("15:04:05.00")
		}
	}
	lines = append(lines, line)

	slowest, behind := "-", "-"
	if in.Checks != 0 {
		behind = millis(in.LagMs)
		if in.Checks != in.Failed {
			slowest = millis(in.SlowestMs)
		}
	}
	lines = append(lines, indent+"slowest "+slowest+", behind "+behind)

	for _, s := range in.SettleRestartedBy {
		lines = append(lines, wrap(indent, "the settle time started again: "+s)...)
	}
	return lines
}

// classCounts writes the classes of failures with their counts: the most
// frequent first, and equal counts by name.
func classCounts(m map[string]int) string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b string) int {
		if c := cmp.Compare(m[b], m[a]); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	parts := make([]string, len(names))
	for i, name := range names {
		parts[i] = name + " " + thousands(m[name])
	}
	return strings.Join(parts, ", ")
}

func pad(s string, width int) string {
	if n := utf8.RuneCountInString(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

func padLeft(s string, width int) string {
	if n := utf8.RuneCountInString(s); n < width {
		return strings.Repeat(" ", width-n) + s
	}
	return s
}

// thousands writes n with a comma between groups of three digits.
func thousands(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		s = "-" + s
	}
	return s
}

func millis(ms float64) string { return strconv.FormatFloat(math.Round(ms), 'f', 0, 64) + " ms" }

func resultWord(s string) string {
	switch s {
	case "pass":
		return "pass"
	case "fail":
		return "FAIL"
	case "not-verified":
		return "NOT VERIFIED"
	case "not-run":
		return "not run"
	case "not-judged":
		return "not judged"
	}
	return s
}

// windowsTable is the table of windows and, under it, the failures that
// crossed a start. Every window of the record has its row.
func windowsTable(r Run) []string {
	header := []string{"window", "length", "checks", "failed", "after finish", "slowest", "behind", "result"}
	rows := [][]string{header}
	for _, w := range r.Windows {
		if w.Start == nil {
			rows = append(rows, []string{w.Name, "-", "-", "-", "-", "-", "-", resultWord(w.Result)})
			continue
		}
		length := "-"
		if w.End != nil {
			length = fmt.Sprintf("%.1f s", float64(w.End.Sub(*w.Start).Milliseconds())/1000)
		}
		after := thousands(w.FailedAfterFinish)
		if w.Name == "steady" {
			after = "-"
		}
		slowest := "-"
		if w.Checks != w.Failed {
			slowest = millis(w.SlowestMs)
		}
		behind := "-"
		if w.Checks != 0 {
			behind = millis(w.LagMs)
		}
		rows = append(rows, []string{w.Name, length, thousands(w.Checks), thousands(w.Failed), after, slowest, behind, resultWord(w.Result)})
	}
	widths := make([]int, len(header))
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], utf8.RuneCountInString(cell))
		}
	}
	var lines []string
	for _, row := range rows {
		var b strings.Builder
		for i, cell := range row {
			if i > 0 {
				b.WriteString("   ")
			}
			switch i {
			case 0:
				b.WriteString(pad(cell, widths[i]))
			case len(row) - 1:
				b.WriteString(cell)
			default:
				b.WriteString(padLeft(cell, widths[i]))
			}
		}
		lines = append(lines, b.String())
	}
	for _, w := range r.Windows {
		switch {
		case w.CrossedStart == 1:
			lines = append(lines, "1 failed check began before the "+w.Name+" started and ended in it")
		case w.CrossedStart != 0:
			lines = append(lines, strconv.Itoa(w.CrossedStart)+" failed checks began before the "+w.Name+" started and ended in it")
		}
	}
	return lines
}

func podsReplaced(w Workload) string {
	switch {
	case w.Added:
		return "added"
	case w.Removed:
		return "removed"
	case w.PodsReplaced == 0:
		return "not replaced"
	}
	return fmt.Sprintf("%d of %d", w.PodsReplaced, w.PodsAtStart)
}

// transitionsTable has one row for each workload of each transition.
func transitionsTable(r Run) []string {
	header := []string{"transition", "workload", "probed", "pods replaced", "quickest stop"}
	rows := [][]string{header}
	for _, tr := range r.Transitions {
		for i, w := range tr.Workloads {
			name := ""
			if i == 0 {
				name = tr.Name
			}
			stop := "-"
			if w.QuickestStopMs != nil {
				stop = fmt.Sprintf("%.1f s", float64(*w.QuickestStopMs)/1000)
			}
			rows = append(rows, []string{name, w.Workload, w.Probed, podsReplaced(w), stop})
		}
	}
	if len(rows) == 1 {
		return nil
	}
	widths := make([]int, len(header))
	widths[0] = 12
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], utf8.RuneCountInString(cell))
		}
	}
	var lines []string
	for _, row := range rows {
		var b strings.Builder
		for i, cell := range row {
			switch {
			case i == 0:
				b.WriteString(pad(cell, 12))
			case i == len(row)-1:
				b.WriteString(cell)
			default:
				b.WriteString(pad(cell, widths[i]) + "   ")
			}
		}
		lines = append(lines, b.String())
	}
	return lines
}

// exercisedLists are what a pass exercised and did not.
func exercisedLists(r Run) []string {
	lines := []string{"exercised"}
	for _, tr := range r.Transitions {
		if windowNotRun(r, tr.Name) {
			continue
		}
		text := ""
		switch tr.Name {
		case "deploy":
			text = "the candidate's start-up and the previous release's shutdown"
		case "rollback":
			text = "the previous release's start-up and the candidate's shutdown"
		case "restart":
			text = "the start-up and the shutdown of the release"
		}
		if unreached(tr) {
			text = "no probed workload had a pod replaced: the probe watched a rollout that it did not reach"
		}
		lines = append(lines, "  "+pad(tr.Name, 12)+text)
	}
	lines = append(lines, "not exercised")
	if hasWindowThatRan(r, "deploy") && !hasWindowThatRan(r, "rollback") {
		lines = append(lines, "  the candidate's shutdown and the installed release's start-up, because the run has no rollback")
	}
	for _, n := range notExercised {
		lines = append(lines, "  "+n)
	}
	return lines
}

// unreached reports whether every workload of tr has Probed yes or no and no
// workload with yes had a pod replaced. A workload that is not known keeps
// the ordinary line.
func unreached(tr Transition) bool {
	for _, w := range tr.Workloads {
		switch w.Probed {
		case "yes":
			if w.PodsReplaced > 0 {
				return false
			}
		case "no":
		default:
			return false
		}
	}
	return true
}

// last is every reason, every warning and the results folder.
func last(r Run) []string {
	var lines []string
	for _, rs := range r.Reasons {
		head := "could not test: "
		if rs.Window != "" {
			head = rs.Window + ": "
		}
		lines = append(lines, head+clean(rs.Text)+"   ["+rs.Code+"]")
		for _, d := range rs.Detail {
			lines = append(lines, wrap("  ", clean(d))...)
		}
	}
	for _, w := range r.Warnings {
		lines = append(lines, "warning: "+clean(w))
	}
	return append(lines, "results: "+r.Out)
}

// wrap breaks indent+text at the last space that keeps a line within
// detailWidth, and goes on in a new line with the same indent. A word longer
// than that stays whole, and a shorter line is returned as it is.
func wrap(indent, text string) []string {
	var out []string
	for {
		line := []rune(indent + text)
		if len(line) <= detailWidth {
			return append(out, indent+text)
		}
		cut := -1
		for i := min(detailWidth, len(line)-1); i > len(indent); i-- {
			if line[i] == ' ' && strings.TrimSpace(string(line[len(indent):i])) != "" {
				cut = i
				break
			}
		}
		if cut < 0 {
			for i := detailWidth + 1; i < len(line); i++ {
				if line[i] == ' ' {
					cut = i
					break
				}
			}
		}
		if cut < 0 {
			return append(out, indent+text)
		}
		out = append(out, string(line[:cut]))
		text = string(line[cut+1:])
	}
}

// hasWindowThatRan reports whether the record has a window with the given
// name whose result is not "not-run".
func hasWindowThatRan(r Run, name string) bool {
	for _, w := range r.Windows {
		if w.Name == name && w.Result != "not-run" {
			return true
		}
	}
	return false
}

// windowNotRun reports whether the record has a window with the given name
// whose result is "not-run".
func windowNotRun(r Run, name string) bool {
	for _, w := range r.Windows {
		if w.Name == name && w.Result == "not-run" {
			return true
		}
	}
	return false
}
