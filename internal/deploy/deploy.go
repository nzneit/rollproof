//go:build linux

package deploy

import (
	"context"

	"rollproof/internal/verdict"
)

// Deployer is a deploy driver. Fetch gets everything from outside the cluster that the
// later calls need, before the probe starts. Install, Deploy and Rollback each
// run the commands of one step and return how it ended; a command may return
// before its rollout is complete. Transitions are the windows this driver gives
// a run, in order.
type Deployer interface {
	Fetch(ctx context.Context) error
	Install(ctx context.Context) Result
	Deploy(ctx context.Context) Result
	Rollback(ctx context.Context) Result
	Transitions() []verdict.WindowName
	Releases() []Release
}

// Release is what the report says of one release: Role is previous or candidate; Chart
// and Version are the chart's own name and version; Source is where it came
// from; ValueFiles are file names and InlineKeys the top-level keys of
// inline maps. Never a value.
type Release struct {
	Role, Chart, Version, Source string
	ValueFiles, InlineKeys       []string
}
