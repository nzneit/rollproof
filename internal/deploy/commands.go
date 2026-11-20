//go:build linux

package deploy

import (
	"context"
	"time"

	"rollproof/internal/config"
	"rollproof/internal/verdict"
)

// CommandsOptions is what the commands driver needs: the configuration's commands, as Resolve
// left them; limits.timeout, which bounds each command; and the run's runner.
type CommandsOptions struct {
	Commands config.Commands
	Timeout  time.Duration
	Runner   Runner
}

// CommandsDriver is the commands driver. It implements Deployer.
type CommandsDriver struct {
	o CommandsOptions
}

// NewCommands returns the commands driver, a Deployer. Fetch does nothing. Install,
// Deploy and Rollback each run their one command, named install, deploy or
// rollback, and return its Result unchanged. Transitions follow section 7's
// table; Releases give one Release for each command present, in the order
// install, deploy, rollback. Deploy or Rollback with no such command panics.
func NewCommands(o CommandsOptions) *CommandsDriver {
	return &CommandsDriver{o: o}
}

// Fetch does nothing: every command the driver runs is one of the configuration's own.
func (d *CommandsDriver) Fetch(ctx context.Context) error {
	return nil
}

func (d *CommandsDriver) run(ctx context.Context, name string, args []string) Result {
	return d.o.Runner.Run(ctx, Command{Name: name, Args: args, Timeout: d.o.Timeout})
}

// Install runs the install command and returns its Result unchanged.
func (d *CommandsDriver) Install(ctx context.Context) Result {
	return d.run(ctx, "install", d.o.Commands.Install)
}

// Deploy runs the deploy command. With none configured it panics: Transitions then gives no
// deploy window, so the call is the caller's mistake, not the run's.
func (d *CommandsDriver) Deploy(ctx context.Context) Result {
	if len(d.o.Commands.Deploy) == 0 {
		panic("deploy: Deploy called with no deploy command")
	}
	return d.run(ctx, "deploy", d.o.Commands.Deploy)
}

// Rollback runs the rollback command. With none configured it panics, as Deploy does: no
// rollback window is derived without the command.
func (d *CommandsDriver) Rollback(ctx context.Context) Result {
	if len(d.o.Commands.Rollback) == 0 {
		panic("deploy: Rollback called with no rollback command")
	}
	return d.run(ctx, "rollback", d.o.Commands.Rollback)
}

// Transitions are the windows of this driver, by section 7's table: deploy and then
// rollback when both commands are present, deploy alone when there is no rollback,
// and a restart when there is no deploy command.
func (d *CommandsDriver) Transitions() []verdict.WindowName {
	switch {
	case len(d.o.Commands.Deploy) == 0:
		return []verdict.WindowName{verdict.Restart}
	case len(d.o.Commands.Rollback) == 0:
		return []verdict.WindowName{verdict.Deploy}
	default:
		return []verdict.WindowName{verdict.Deploy, verdict.Rollback}
	}
}

// Releases give one Release for each command the configuration names, in the order
// install, deploy, rollback, with the first word of that command as its Program.
func (d *CommandsDriver) Releases() []Release {
	var out []Release
	for _, step := range []struct {
		role string
		args []string
	}{{"install", d.o.Commands.Install}, {"deploy", d.o.Commands.Deploy}, {"rollback", d.o.Commands.Rollback}} {
		if len(step.args) > 0 {
			out = append(out, Release{Role: step.role, Program: step.args[0]})
		}
	}
	return out
}
