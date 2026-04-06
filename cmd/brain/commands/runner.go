package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

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
	if len(flags.Include) > 0 {
		cfg.IncludeProjects = append(cfg.IncludeProjects, flags.Include...)
	}
	if len(flags.Exclude) > 0 {
		cfg.ExcludeProjects = append(cfg.ExcludeProjects, flags.Exclude...)
	}
}

// RunnerFlags holds runner command flags.
type RunnerFlags struct {
	TUI          bool
	Foreground   bool
	Headless     bool
	Dashboard    bool
	Monitor      bool
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

// Execute starts the runner, dispatching to TUI, headless, or other modes based on flags.
func (c *RunnerTUICommand) Execute() error {
	// Determine mode from flags (same pattern as RunCommand.runStart)
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
	projects, err := c.resolveProjects(cfg)
	if err != nil {
		return err
	}

	// Build runner options
	opts := runnercli.RunnerOptions{
		Projects:    projects,
		Config:      cfg,
		Mode:        mode,
		StartPaused: true,
		KeyBindings: c.Config.TUI.KeyBindings,
	}

	ctx := context.Background()

	// Monitor mode: TUI without local runner (pure control panel)
	if c.Flags.Monitor {
		return runnercli.RunMonitorTUI(ctx, opts)
	}

	// Non-TUI modes (headless, foreground, dashboard) go through RunTaskRunner
	if mode != "tui" {
		return runnercli.RunTaskRunner(ctx, opts)
	}

	// Default: run with TUI
	return runnercli.RunTUI(ctx, opts)
}

func (c *RunnerTUICommand) resolveProjects(cfg runner.RunnerConfig) ([]string, error) {
	return resolveProjectList(c.Project, cfg)
}

// resolveProjectList fetches and filters the project list from the Brain API.
// If project is not "all", it returns a single-element list without calling the API.
// For "all", it fetches all projects from the API and applies include/exclude filtering.
func resolveProjectList(project string, cfg runner.RunnerConfig) ([]string, error) {
	if project != "all" {
		return []string{project}, nil
	}

	client := runner.NewAPIClient(cfg)
	projects, err := client.ListProjects(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to list projects: %w", err)
	}
	if len(projects) == 0 {
		return nil, fmt.Errorf("no projects found")
	}

	// Apply include/exclude filters (already merged with CLI flags by applyRunnerFlagOverrides)
	filtered := runner.FilterProjects(projects, cfg.IncludeProjects, cfg.ExcludeProjects)
	if len(filtered) == 0 {
		return nil, fmt.Errorf("all projects filtered out by include/exclude patterns")
	}

	return filtered, nil
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
	projects, err := resolveProjectList(c.Project, cfg)
	if err != nil {
		return err
	}

	// Build runner options
	opts := runnercli.RunnerOptions{
		Projects:    projects,
		Config:      cfg,
		Mode:        mode,
		StartPaused: false,
		KeyBindings: c.Config.TUI.KeyBindings,
	}

	ctx := context.Background()

	// Run based on mode
	if c.Flags.Monitor {
		return runnercli.RunMonitorTUI(ctx, opts)
	}
	if mode == "tui" {
		return runnercli.RunTUI(ctx, opts)
	}
	return runnercli.RunTaskRunner(ctx, opts)
}

// makeAPIClient creates an APIClient from the command's config,
// falling back to MCP.APIURL if Runner.BrainAPIURL is empty.
func (c *RunCommand) makeAPIClient() (*runner.APIClient, error) {
	cfg := c.Config.Runner
	if cfg.BrainAPIURL == "" {
		cfg.BrainAPIURL = c.Config.MCP.APIURL
	}
	if cfg.BrainAPIURL == "" {
		return nil, fmt.Errorf("BRAIN_API_URL not configured; set it via config file or environment variable")
	}
	return runner.NewAPIClient(cfg), nil
}

func (c *RunCommand) runList() error {
	client, err := c.makeAPIClient()
	if err != nil {
		return err
	}
	ctx := context.Background()

	// "all" or empty → list projects with task counts
	if c.Project == "" || c.Project == "all" {
		projects, err := client.ListProjects(ctx)
		if err != nil {
			return fmt.Errorf("failed to list projects: %w", err)
		}
		if len(projects) == 0 {
			fmt.Println("No projects found.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "PROJECT\tTASKS\tREADY\tBLOCKED")
		for _, p := range projects {
			tasks, err := client.GetAllTasks(ctx, p)
			if err != nil {
				fmt.Fprintf(w, "%s\t(error)\t-\t-\n", p)
				continue
			}
			ready, blocked := 0, 0
			for _, t := range tasks {
				switch t.Classification {
				case "ready":
					ready++
				case "blocked":
					blocked++
				}
			}
			fmt.Fprintf(w, "%s\t%d\t%d\t%d\n", p, len(tasks), ready, blocked)
		}
		w.Flush()
		return nil
	}

	// Specific project → list tasks
	tasks, err := client.GetAllTasks(ctx, c.Project)
	if err != nil {
		return fmt.Errorf("failed to list tasks for %s: %w", c.Project, err)
	}
	if len(tasks) == 0 {
		fmt.Printf("No tasks found for project %s.\n", c.Project)
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTITLE\tSTATUS\tPRIORITY\tFEATURE")
	for _, t := range tasks {
		title := t.Title
		if len(title) > 50 {
			title = title[:47] + "..."
		}
		feature := t.FeatureID
		if feature == "" {
			feature = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", t.ID, title, t.Status, t.Priority, feature)
	}
	w.Flush()
	return nil
}

func (c *RunCommand) runStop() error {
	client, err := c.makeAPIClient()
	if err != nil {
		return err
	}
	ctx := context.Background()

	if err := client.PauseAll(ctx); err != nil {
		return fmt.Errorf("failed to pause runners: %w", err)
	}
	fmt.Println("All runners paused.")
	return nil
}

func (c *RunCommand) runStatus() error {
	client, err := c.makeAPIClient()
	if err != nil {
		return err
	}
	ctx := context.Background()

	// Get runner pause/resume state
	status, err := client.GetRunnerStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to get runner status: %w", err)
	}

	if status.Paused {
		fmt.Println("Runner state: PAUSED")
	} else {
		fmt.Println("Runner state: RUNNING")
	}
	if len(status.PausedProjects) > 0 {
		fmt.Printf("Paused projects: %s\n", strings.Join(status.PausedProjects, ", "))
	}

	// List registered runners
	runners, err := client.ListRunners(ctx)
	if err != nil {
		fmt.Printf("\nRunners: (unavailable: %v)\n", err)
		return nil
	}

	fmt.Printf("\nRegistered runners: %d\n", runners.Total)
	if runners.Total > 0 {
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "RUNNER_ID\tHOSTNAME\tSTATUS\tMAX_PARALLEL\tLAST_HEARTBEAT")
		for _, r := range runners.Runners {
			age := "-"
			if r.LastHeartbeat != "" {
				if t, err := time.Parse(time.RFC3339, r.LastHeartbeat); err == nil {
					age = time.Since(t).Truncate(time.Second).String() + " ago"
				} else {
					age = r.LastHeartbeat
				}
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", r.RunnerID, r.Hostname, r.Status, r.MaxParallel, age)
		}
		w.Flush()
	}

	return nil
}

func (c *RunCommand) runReady() error {
	client, err := c.makeAPIClient()
	if err != nil {
		return err
	}
	ctx := context.Background()

	project := c.Project
	if project == "" {
		return fmt.Errorf("project required: brain run ready <project>")
	}

	tasks, err := client.GetReadyTasks(ctx, project, nil)
	if err != nil {
		return fmt.Errorf("failed to get ready tasks for %s: %w", project, err)
	}
	if len(tasks) == 0 {
		fmt.Printf("No ready tasks for project %s.\n", project)
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTITLE\tPRIORITY\tFEATURE")
	for _, t := range tasks {
		title := t.Title
		if len(title) > 50 {
			title = title[:47] + "..."
		}
		feature := t.FeatureID
		if feature == "" {
			feature = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", t.ID, title, t.Priority, feature)
	}
	w.Flush()
	return nil
}

func (c *RunCommand) runFeatures() error {
	return fmt.Errorf("run features: not yet implemented; use the API directly: GET /api/v1/tasks/<project>/features")
}

func (c *RunCommand) runLogs() error {
	return fmt.Errorf("run logs: not yet implemented; use the API directly: GET /api/v1/tasks/<project>/<taskId>/logs")
}

func (c *RunCommand) runConfig() error {
	return fmt.Errorf("run config: not yet implemented; use the API directly: GET /api/v1/config/task-defaults")
}
