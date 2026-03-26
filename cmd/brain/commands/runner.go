package commands

import (
	"context"
	"fmt"

	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/runnercli"
)

// applyRunnerFlagOverrides applies CLI flag values on top of the runner config.
// Only non-zero/non-empty flag values override the config file values.
func applyRunnerFlagOverrides(cfg *runner.RunnerConfig, flags *RunnerFlags) {
	if flags.MaxParallel != 0 {
		cfg.MaxParallel = flags.MaxParallel
	}
	if flags.PollInterval != 0 {
		cfg.PollInterval = flags.PollInterval
	}
	if flags.Workdir != "" {
		cfg.WorkDir = flags.Workdir
	}
	if flags.Agent != "" {
		cfg.Opencode.Agent = flags.Agent
	}
	if flags.Model != "" {
		cfg.Opencode.Model = flags.Model
	}
	if len(flags.FeatureIDs) > 0 {
		cfg.FeatureIDs = flags.FeatureIDs
	}
}

// RunnerFlags holds runner command flags.
type RunnerFlags struct {
	TUI          bool
	Foreground   bool
	Headless     bool
	Dashboard    bool
	MaxParallel  int
	PollInterval int
	Workdir      string
	Agent        string
	Model        string
	Include      []string
	Exclude      []string
	FeatureIDs   []string
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
	// Start with the full runner config (all fields preserved)
	cfg := c.Config.Runner

	// Fallback: if BrainAPIURL not set, use MCP.APIURL
	if cfg.BrainAPIURL == "" {
		cfg.BrainAPIURL = c.Config.MCP.APIURL
	}

	// Apply CLI flag overrides
	applyRunnerFlagOverrides(&cfg, c.Flags)

	// Resolve projects
	projects, err := c.resolveProjects()
	if err != nil {
		return err
	}

	// Build runner options
	opts := runnercli.RunnerOptions{
		Projects:    projects,
		Config:      cfg,
		Mode:        "tui",
		StartPaused: true,
	}

	ctx := context.Background()

	// Run with TUI
	return runnercli.RunTUI(ctx, opts)
}

func (c *RunnerTUICommand) resolveProjects() ([]string, error) {
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
	} else if c.Flags.Headless {
		mode = "headless"
	} else if c.Flags.Dashboard {
		mode = "dashboard"
	}

	// Start with the full runner config (all fields preserved)
	cfg := c.Config.Runner

	// Fallback: if BrainAPIURL not set, use MCP.APIURL
	if cfg.BrainAPIURL == "" {
		cfg.BrainAPIURL = c.Config.MCP.APIURL
	}

	// Apply CLI flag overrides
	applyRunnerFlagOverrides(&cfg, c.Flags)

	// Resolve projects
	projects := []string{c.Project}
	if c.Project == "all" {
		// TODO: Fetch all projects from API
		projects = []string{"all"}
	}

	// Build runner options
	opts := runnercli.RunnerOptions{
		Projects:    projects,
		Config:      cfg,
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
