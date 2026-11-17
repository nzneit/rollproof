package run

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"rollproof/internal/config"
	"rollproof/internal/deploy"
	"rollproof/internal/probe"
	"rollproof/internal/report"
	"rollproof/internal/verdict"
	"rollproof/internal/watch"
)

// facts is everything the run has learned, in the units' own types, at the
// moment it writes a record. probeStopped says that the probe was asked to
// stop and that probe holds what its Stop returned.
type facts struct {
	cfg          *config.Config
	decision     verdict.Decision
	transitions  []transitionFacts
	install      watch.Install
	releases     []deploy.Release
	probe        probe.Facts
	probeStopped bool
	target       probe.Target
	images       map[string]config.ImageRef
	removed      []string
	warnings     []string
	cluster      *report.Cluster
	version, out string
}

// transitionFacts is one transition as the units reported it: what the
// watcher saw, how the command ended, and the pods that were stopping or
// starting at the window's first failure.
type transitionFacts struct {
	tr                 watch.Transition
	cmd                deploy.Result
	stopping, starting []watch.PodNote
}

// record returns the report's record of the run so far, final or not. It
// copies and renames; it decides nothing. It never reads a command's output or
// a header's value.
func record(f facts) report.Run {
	r := report.Run{
		Schema:  1,
		Verdict: string(f.decision.Verdict),
		Tool:    report.Tool{Version: f.version},
		Out:     f.out,
		Cluster: f.cluster,
	}
	for _, reason := range f.decision.Reasons {
		r.Reasons = append(r.Reasons, reasonRecord(reason, f.transitions))
	}
	for _, w := range f.decision.Windows {
		r.Windows = append(r.Windows, windowRecord(w))
	}
	r.Warnings = append(r.Warnings, f.warnings...)
	for _, c := range f.decision.Ignored {
		r.Warnings = append(r.Warnings, "after the last window: "+c.Error())
	}
	r.Transitions = transitionRecords(f.transitions)
	for _, x := range f.releases {
		r.Releases = append(r.Releases, report.Release{Role: x.Role, Chart: x.Chart, Version: x.Version,
			Source: x.Source, ValuesFiles: x.ValueFiles, InlineKeys: x.InlineKeys})
	}
	r.RemovedVariables = f.removed
	if f.cfg == nil {
		return r
	}
	if f.cfg.Deploy.Helm != nil {
		r.Driver, r.Name = "helm", f.cfg.Deploy.Helm.Release
	}
	var probeSettings []report.Setting
	for _, s := range f.cfg.Settings() {
		x := report.Setting{Key: s.Key, Value: s.Value, Default: s.Default}
		if strings.HasPrefix(s.Key, "probe.") {
			if strings.HasPrefix(s.Key, "probe.http") {
				probeSettings = append(probeSettings, x)
			}
			continue
		}
		r.Settings = append(r.Settings, x)
	}
	if h := f.cfg.Probe.HTTP; h != nil && started(f.decision) {
		r.Probes = []report.Probe{probeRecord(f, h, probeSettings)}
	}
	r.Images = imageRecords(f)
	return r
}

// started reports whether the steady window has a start: the probe started.
func started(d verdict.Decision) bool {
	for _, w := range d.Windows {
		if w.Name == verdict.Steady {
			return !w.Start.IsZero()
		}
	}
	return false
}

func reasonRecord(reason verdict.Reason, trs []transitionFacts) report.Reason {
	lines := strings.Split(strings.ReplaceAll(reason.Text, "\r\n", "\n"), "\n")
	out := report.Reason{Code: string(reason.Code), Verdict: string(reason.Verdict),
		Window: string(reason.Window), Text: lines[0], Decided: reason.Decided}
	out.Detail = append(out.Detail, lines[1:]...)
	for _, t := range trs {
		if t.tr.Name != reason.Window {
			continue
		}
		switch reason.Code {
		case verdict.ProbeFailures:
			out.Detail = append(out.Detail, podLines(t)...)
		case verdict.TransitionStalled:
			out.Detail = append(out.Detail, t.tr.Incomplete...)
		}
	}
	return out
}

// podLines returns the lines about the pods that were stopping and starting
// when the window's first failure came.
func podLines(t transitionFacts) []string {
	var lines []string
	for _, p := range t.stopping {
		lines = append(lines, "stopping then   "+podText(t.tr.Name, p, "told to stop"))
	}
	if len(t.stopping) == 0 {
		lines = append(lines, "stopping then   none")
	}
	for _, p := range t.starting {
		lines = append(lines, "starting then   "+podText(t.tr.Name, p, "created"))
	}
	if len(t.starting) == 0 {
		lines = append(lines, "starting then   none")
	}
	return lines
}

// podText names the pod and the release it ran. In the deploy an old pod ran
// the previous release; in the rollback the releases change sides. For any
// other window the pod's release is not named.
func podText(w verdict.WindowName, p watch.PodNote, what string) string {
	name := p.Name
	switch {
	case w == verdict.Deploy && p.Old, w == verdict.Rollback && !p.Old:
		name += " (previous release)"
	case w == verdict.Deploy, w == verdict.Rollback:
		name += " (candidate)"
	}
	return fmt.Sprintf("%s, %s %.1f s before", name, what, p.For.Seconds())
}

func windowRecord(w verdict.WindowResult) report.Window {
	out := report.Window{Name: string(w.Name), Start: timePtr(w.Start), End: timePtr(w.End),
		Result: string(w.Result), Checks: w.Checks, Failed: w.Failed, FailedAfterFinish: w.AfterFinish,
		CrossedStart: w.Crossed, FirstFailure: timePtr(w.FirstFailure), SlowestMs: w.SlowestMs, LagMs: w.LagMs}
	if w.ByClass != nil {
		out.FailedByClass = make(map[string]int, len(w.ByClass))
		for k, v := range w.ByClass {
			out.FailedByClass[k] = v
		}
	}
	return out
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func transitionRecords(trs []transitionFacts) []report.Transition {
	var out []report.Transition
	for _, t := range trs {
		x := report.Transition{Name: string(t.tr.Name), Stalled: t.tr.Stalled,
			Command: string(t.cmd.Ending), SettleRestartedBy: t.tr.SettleRestarts}
		for _, c := range t.tr.Workloads {
			w := report.Workload{Workload: c.Workload.String(), PodsAtStart: c.AtStart, PodsReplaced: c.Replaced,
				PodsCreated: c.Created, Added: c.Added, Removed: c.Removed, Probed: c.Probed}
			if c.QuickestStop != 0 {
				ms := c.QuickestStop.Milliseconds()
				w.QuickestStopMs = &ms
			}
			x.Workloads = append(x.Workloads, w)
		}
		out = append(out, x)
	}
	return out
}

func probeRecord(f facts, h *config.HTTP, settings []report.Setting) report.Probe {
	var names []string
	for k := range h.Headers { // names only: a header's value is never read
		names = append(names, k)
	}
	sort.Strings(names)
	return report.Probe{
		Kind: "http", Target: "GET " + h.URL, Settings: settings, Headers: names,
		Lines: f.probe.Lines, JSONLinesWithoutOK: f.probe.JSONTextLines,
		// On http there is no certificate to check, so none is left unchecked.
		CertificateChecked: certificateChecked(h.URL),
		StoppedWhenAsked:   f.probe.StoppedWhenAsked || !f.probeStopped,
		Notes:              f.target.Notes,
	}
}

// certificateChecked says whether the probe checks a certificate. The scheme
// is read as the parser reads it, which lowercases it, so HTTPS:// counts. A
// URL that cannot be read counts as one whose certificate is not checked.
func certificateChecked(rawURL string) bool {
	u, err := url.Parse(rawURL)
	return err == nil && u.Scheme != "https"
}

func imageRecords(f facts) []report.Image {
	var names []string
	for name := range f.cfg.Images {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []report.Image
	for _, name := range names {
		digest := f.images[name].Digest
		im := report.Image{Name: name, Digest: digest}
		if digest != "" {
			for _, c := range f.install.Workloads {
				if hasDigest(c, digest) {
					im.RunBy = append(im.RunBy, report.RunBy{Workload: c.Workload.String(), Transition: "install"})
				}
			}
			for _, t := range f.transitions {
				for _, c := range t.tr.Workloads {
					if hasDigest(c, digest) {
						im.RunBy = append(im.RunBy, report.RunBy{Workload: c.Workload.String(), Transition: string(t.tr.Name)})
					}
				}
			}
		}
		out = append(out, im)
	}
	return out
}

func hasDigest(c watch.Count, digest string) bool {
	for _, d := range c.Digests {
		if d == digest {
			return true
		}
	}
	return false
}
