// Package report is what a run writes: result.json, the console text and the
// evidence files. It hides the file formats and the moments of writing. It is
// given one record of plain values and reads nothing from the cluster.
package report

import "time"

// Run is the record of one run: everything result.json holds, and Out, the
// results folder, for the console's last line. Install is the install record,
// or nil for a run that ended before the install was ready. It holds no values
// but strings, numbers, booleans and times.
type Run struct {
	Schema           int          `json:"schema"`
	Final            bool         `json:"final"`
	Verdict          string       `json:"verdict" mask:"keep"`
	Driver           string       `json:"driver" mask:"keep"`
	Name             string       `json:"name"`
	Reasons          []Reason     `json:"reasons"`
	Windows          []Window     `json:"windows"`
	Install          *Install     `json:"install"`
	Transitions      []Transition `json:"transitions"`
	Probes           []Probe      `json:"probes"`
	Releases         []Release    `json:"releases"`
	Settings         []Setting    `json:"settings"`
	Images           []Image      `json:"images"`
	RemovedVariables []string     `json:"removed_variables"`
	Warnings         []string     `json:"warnings"`
	Tool             Tool         `json:"tool"`
	Cluster          *Cluster     `json:"cluster"`
	Out              string       `json:"-"`
}

// Reason is one reason of the verdict. Window is empty for a reason with no
// window. Detail holds lines for people that follow the text. Decided marks
// the one reason that decided.
type Reason struct {
	Code    string   `json:"code" mask:"keep"`
	Verdict string   `json:"verdict" mask:"keep"`
	Window  string   `json:"window,omitempty" mask:"keep"`
	Text    string   `json:"text"`
	Detail  []string `json:"detail"`
	Decided bool     `json:"decided"`
}

// Window is one window with its result and its counts. A nil time is one
// that never came: the start of a window that did not run, or the first
// failure of a window with none. A nil SlowestMs or LagMs is a figure that
// the probe did not give: result.json writes it as null and the console as
// "-".
type Window struct {
	Name              string         `json:"name" mask:"keep"`
	Start             *time.Time     `json:"start"`
	End               *time.Time     `json:"end"`
	Result            string         `json:"result" mask:"keep"`
	Checks            int            `json:"checks"`
	Failed            int            `json:"failed"`
	FailedAfterFinish int            `json:"failed_after_finish"`
	CrossedStart      int            `json:"crossed_start"`
	FailedByClass     map[string]int `json:"failed_by_class"`
	FirstFailure      *time.Time     `json:"first_failure"`
	SlowestMs         *float64       `json:"slowest_ms"`
	LagMs             *float64       `json:"lag_ms"`
}

// Install is the install as the report states it, apart from the windows. A
// nil time is one that never came. The counts are the install window's, all
// zero when the probe gave no observation. A nil SlowestMs or LagMs is a
// figure that the probe did not give: result.json writes it as null and the
// console as "-".
type Install struct {
	Ready             time.Time      `json:"ready"`
	FirstObservation  *time.Time     `json:"first_observation"`
	Settled           *time.Time     `json:"settled"`
	SettleRestartedBy []string       `json:"settle_restarted_by"`
	Checks            int            `json:"checks"`
	Failed            int            `json:"failed"`
	FailedByClass     map[string]int `json:"failed_by_class"`
	LastFailure       *time.Time     `json:"last_failure"`
	SlowestMs         *float64       `json:"slowest_ms"`
	LagMs             *float64       `json:"lag_ms"`
}

// Transition is one transition that ran.
type Transition struct {
	Name    string `json:"name" mask:"keep"`
	Stalled bool   `json:"stalled"`
	// Command is how the transition's command ended: ok, exit-nonzero,
	// not-started, stopped-at-limit or stopped-by-signal. Nil, null in
	// result.json, for a restart, which has no command.
	Command *string `json:"command" mask:"keep"`
	// Restart is whether the API server took the restart's change:
	// "accepted" or "refused". Nil, null in result.json, for a deploy and
	// a rollback, and for a restart that the run's context cut short or
	// that limits.timeout stopped, in which the server refused nothing.
	Restart           *string    `json:"restart" mask:"keep"`
	SettleRestartedBy []string   `json:"settle_restarted_by"`
	Workloads         []Workload `json:"workloads"`
}

// Workload is one watched workload in one transition. Workload is the kind,
// a slash and the name. Probed is yes, no or not known. QuickestStopMs is
// nil when the stop is zero: no ready pod was deleted, or one was gone at
// the moment its deletion was first seen.
type Workload struct {
	Workload       string `json:"workload"`
	PodsAtStart    int    `json:"pods_at_start"`
	PodsReplaced   int    `json:"pods_replaced"`
	PodsCreated    int    `json:"pods_created"`
	Added          bool   `json:"added"`
	Removed        bool   `json:"removed"`
	Probed         string `json:"probed" mask:"keep"`
	QuickestStopMs *int64 `json:"quickest_stop_ms"`
}

// Probe is one probe that judged the run. Headers and EnvNames are names,
// never values.
type Probe struct {
	Kind               string    `json:"kind" mask:"keep"`
	Target             string    `json:"target"`
	Settings           []Setting `json:"settings"`
	Headers            []string  `json:"headers"`
	EnvNames           []string  `json:"env_names"`
	CommandCount       int       `json:"command_count"`
	ArgsCount          int       `json:"args_count"`
	Lines              int       `json:"lines"`
	JSONLinesWithoutOK int       `json:"json_lines_without_ok"`
	// CertificateChecked is whether the probe checked the server's certificate: false for the
	// built-in probe on https, true on http. Nil, null in result.json, for a custom probe, whose
	// checks the tool does not know.
	CertificateChecked *bool `json:"certificate_checked"`
	// StoppedWhenAsked is true when the probe stopped within the drain after the tool asked it;
	// false when it did not. Null when the tool did not ask it, stopped waiting for it because a
	// signal came, or failed while it stopped it; Notes then says which.
	StoppedWhenAsked *bool    `json:"stopped_when_asked"`
	Notes            []string `json:"notes"`
}

// Release is one release of the Helm driver: the chart's name and the
// version Helm resolved, where it came from, and the names of the values
// files and the top-level keys of the inline maps. Never a value. For the
// commands driver it is one command: its role, install, deploy or rollback,
// and its Program.
type Release struct {
	Role string `json:"role" mask:"keep"`
	// Program is the first word of the commands driver's command for this
	// role; empty for the Helm driver, whose chart fields are empty for the
	// commands driver.
	Program     string   `json:"program"`
	Chart       string   `json:"chart"`
	Version     string   `json:"version"`
	Source      string   `json:"source"`
	ValuesFiles []string `json:"values_files"`
	InlineKeys  []string `json:"inline_keys"`
}

// Setting is one setting as text, with its default.
type Setting struct {
	Key     string `json:"key" mask:"keep"`
	Value   string `json:"value"`
	Default string `json:"default" mask:"keep"`
}

// Image is one entry of the configuration's images, with its digest and the
// watched workloads whose pods ran it.
type Image struct {
	Name   string  `json:"name"`
	Digest string  `json:"digest"`
	RunBy  []RunBy `json:"run_by"`
}

// RunBy is one watched workload whose pods ran an image, and the transition
// that created those pods: install, deploy, rollback or restart.
type RunBy struct {
	Workload   string `json:"workload"`
	Transition string `json:"transition" mask:"keep"`
}

// Tool is the tool that wrote the record.
type Tool struct {
	Version string `json:"version"`
}

// Cluster is the test cluster. Provider is kind.
type Cluster struct {
	Provider        string `json:"provider" mask:"keep"`
	ProviderVersion string `json:"provider_version"`
	Kubernetes      string `json:"kubernetes"`
	NodeImage       string `json:"node_image"`
	Workers         int    `json:"workers"`
}
