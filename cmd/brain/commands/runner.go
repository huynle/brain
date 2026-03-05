package commands

import (
	"context"
	"fmt"

	"github.com/huynle/brain-api/internal/runnercli"
)

// RunnerFlags holds runner command flags.
type RunnerFlags struct {
	TUI          bool
	Foreground   bool
	Background   bool
	Dashboard    bool
	MaxParallel  int
	PollInterval int
	Workdir      string
	Agent        string
	Model        string
	Include      []string
	Exclude      []string
	Follow       bool
}

// RunnerTUICommand starts runner in TUI mode.
type RunnerTUICommand struct {
	Project string
	Config  *UnifiedConfig
	Flags   *RunnerFlags
}

// Type returns the command type identifier.
func (c *RunnerTUICommand) Type() string {
	return "runner_tui"
}

// Execute starts the runner in TUI mode.
func (c *RunnerTUICommand) Execute() error {
	// Build runner config from unified config
	runnerCfg := runnercli.RunnerConfig{
		BrainAPIURL:  c.Config.MCP.APIURL,
		MaxParallel:  c.Config.Runner.MaxParallel,
		PollInterval: c.Config.Runner.PollInterval,
		WorkDir:      c.Config.Runner.WorkDir,
		StateDir:     c.Config.Runner.StateDir,
		LogDir:       c.Config.Runner.LogDir,
	}

	// Apply flags
	if c.Flags.MaxParallel != 0 {
		runnerCfg.MaxParallel = c.Flags.MaxParallel
	}
	if c.Flags.PollInterval != 0 {
		runnerCfg.PollInterval = c.Flags.PollInterval
	}
	if c.Flags.Workdir != "" {
		runnerCfg.WorkDir = c.Flags.Workdir
	}

	// Resolve projects
	projects, err := c.resolveProjects(runnerCfg)
	if err != nil {
		return err
	}

	// Build runner options
	opts := runnercli.RunnerOptions{
		Projects:    projects,
		Config:      runnerCfg,
		Mode:        "tui",
		StartPaused: true,
	}

	ctx := context.Background()

	// Run with TUI
	return runnercli.RunTUI(ctx, opts)
}

func (c *RunnerTUICommand) resolveProjects(cfg runnercli.RunnerConfig) ([]string, error) {
	if c.Project != "all" {
		return []string{c.Project}, nil
	}

	// TODO: Fetch all projects from API in future phase
	// For now, just return "all" as a single project identifier
	return []string{"all"}, nil
}

// RunCommand handles explicit `brain run` subcommands.
type RunCommand struct {
	Subcommand string
	Project    string
	Config     *UnifiedConfig
	Flags      *RunnerFlags
}

// Type returns the command type identifier.
func (c *RunCommand) Type() string {
	return "run_" + c.Subcommand
}

// Execute runs the specified subcommand.
func (c *RunCommand) Execute() error {
	switch c.Subcommand {
	case "start":
		return c.runStart()
	case "stop":
		return c.runStop()
	case "status":
		return c.runStatus()
	case "list":
		return c.runList()
	case "ready":
		return c.runReady()
	case "features":
		return c.runFeatures()
	case "logs":
		return c.runLogs()
	case "config":
		return c.runConfig()
	default:
		return fmt.Errorf("unknown run subcommand: %s", c.Subcommand)
	}
}

func (c *RunCommand) runStart() error {
	// Determine mode from flags
	mode := "tui"
	if c.Flags.Foreground {
		mode = "foreground"
	} else if c.Flags.Background {
		mode = "background"
	} else if c.Flags.Dashboard {
		mode = "dashboard"
	}

	// Build runner config
	runnerCfg := runnercli.RunnerConfig{
		BrainAPIURL:  c.Config.MCP.APIURL,
		MaxParallel:  c.Config.Runner.MaxParallel,
		PollInterval: c.Config.Runner.PollInterval,
		WorkDir:      c.Config.Runner.WorkDir,
		StateDir:     c.Config.Runner.StateDir,
		LogDir:       c.Config.Runner.LogDir,
	}

	// Apply flags
	if c.Flags.MaxParallel != 0 {
		runnerCfg.MaxParallel = c.Flags.MaxParallel
	}
	if c.Flags.PollInterval != 0 {
		runnerCfg.PollInterval = c.Flags.PollInterval
	}
	if c.Flags.Workdir != "" {
		runnerCfg.WorkDir = c.Flags.Workdir
	}

	// Resolve projects
	projects := []string{c.Project}
	if c.Project == "all" {
		// TODO: Fetch all projects from API
		projects = []string{"all"}
	}

	// Build runner options
	opts := runnercli.RunnerOptions{
		Projects:    projects,
		Config:      runnerCfg,
		Mode:        mode,
		StartPaused: false,
	}

	ctx := context.Background()

	// Run based on mode
	if mode == "tui" {
		return runnercli.RunTUI(ctx, opts)
	}
	return runnercli.RunTaskRunner(ctx, opts)
}

func (c *RunCommand) runStop() error {
	return fmt.Errorf("run stop not yet implemented (Phase 3)")
}

func (c *RunCommand) runStatus() error {
	return fmt.Errorf("run status not yet implemented (Phase 3)")
}

func (c *RunCommand) runList() error {
	return fmt.Errorf("run list not yet implemented (Phase 3)")
}

func (c *RunCommand) runReady() error {
	return fmt.Errorf("run ready not yet implemented (Phase 3)")
}

func (c *RunCommand) runFeatures() error {
	return fmt.Errorf("run features not yet implemented (Phase 3)")
}

func (c *RunCommand) runLogs() error {
	return fmt.Errorf("run logs not yet implemented (Phase 3)")
}

func (c *RunCommand) runConfig() error {
	return fmt.Errorf("run config not yet implemented (Phase 3)")
}
