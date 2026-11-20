//go:build linux

package deploy

import (
	"context"
	"time"
)

// Setup runs the setup commands in order on r, each named setup and bounded by
// timeout (limits.timeout), and returns the Result of the first that did not
// succeed, with its index in commands; the rest are not started. When all
// succeeded, or there were none, it returns a Result named setup that
// Succeeded, with exit code 0, and the index -1. The runner numbers the
// evidence: setup, setup-2 and so on. A command reads end of file from its
// standard input.
func Setup(ctx context.Context, r Runner, commands [][]string, timeout time.Duration) (Result, int) {
	for i, args := range commands {
		res := r.Run(ctx, Command{Name: "setup", Args: args, Timeout: timeout})
		if res.Ending != Succeeded {
			return res, i
		}
	}
	return Result{Name: "setup", Ending: Succeeded, ExitCode: 0}, -1
}
