// Package main is the entry point for the Brain task runner.
// The runner processes queued tasks by spawning OpenCode instances.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/tui"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Help Text
// =============================================================================

const helpText = `Brain Runner CLI - Process tasks from Brain API using OpenCode

Usage:
  brain-runner [command] [project] [options]
  brain-runner <project>            Start TUI for a project
  brain-runner                      Start TUI for all projects

Commands:
  start [project]     Start the runner (default: all projects, TUI mode)
  stop [project]      Stop running daemon
  status [project]    Show runner status
  list [project]      List all tasks for project
  ready [project]     List ready tasks
  config              Show current configuration

Options:
  --tui               Use interactive TUI (default for start)
  -b, --background    Run as background daemon
  --dashboard         Create tmux dashboard
  -p, --max-parallel N  Max concurrent tasks
  --poll-interval N   Seconds between polls
  -w, --workdir DIR   Working directory
  --agent NAME        OpenCode agent to use
  -m, --model NAME    Model to use
  -i, --include PAT   Include project pattern (repeatable)
  -e, --exclude PAT   Exclude project pattern (repeatable)
  -v, --verbose       Enable debug logging
  -h, --help          Show this help message

Examples:
  brain-runner                        Start TUI for all projects
  brain-runner my-project             Start TUI for my-project
  brain-runner start my-project       Same as above
  brain-runner start all -p 5         All projects, 5 concurrent tasks
  brain-runner start my-project -b    Background daemon
  brain-runner ready my-project       Show ready tasks
  brain-runner config                 Show configuration
`

// =============================================================================
// Parsed CLI Arguments
// =============================================================================

type parsedArgs struct {
	command  string
	project  string
	help     bool
	verbose  bool
	tui      bool
	bg       bool
	dash     bool
	parallel int
	poll     int
	workdir  string
	agent    string
	model    string
	include  []string
	exclude  []string
}

// =============================================================================
// Argument Parsing
// =============================================================================

func parseArgs(argv []string) parsedArgs {
	p := parsedArgs{
		project: "all",
	}

	i := 0
	for i < len(argv) {
		arg := argv[i]

		switch {
		// Flags without values
		case arg == "-h" || arg == "--help":
			p.help = true
		case arg == "-v" || arg == "--verbose":
			p.verbose = true
		case arg == "--tui":
			p.tui = true
		case arg == "-b" || arg == "--background":
			p.bg = true
		case arg == "--dashboard":
			p.dash = true

		// Flags with values
		case arg == "-p" || arg == "--max-parallel":
			if i+1 < len(argv) {
				i++
				p.parallel, _ = strconv.Atoi(argv[i])
			}
		case arg == "--poll-interval":
			if i+1 < len(argv) {
				i++
				p.poll, _ = strconv.Atoi(argv[i])
			}
		case arg == "-w" || arg == "--workdir":
			if i+1 < len(argv) {
				i++
				p.workdir = argv[i]
			}
		case arg == "--agent":
			if i+1 < len(argv) {
				i++
				p.agent = argv[i]
			}
		case arg == "-m" || arg == "--model":
			if i+1 < len(argv) {
				i++
				p.model = argv[i]
			}
		case arg == "-i" || arg == "--include":
			if i+1 < len(argv) {
				i++
				p.include = append(p.include, argv[i])
			}
		case arg == "-e" || arg == "--exclude":
			if i+1 < len(argv) {
				i++
				p.exclude = append(p.exclude, argv[i])
			}

		// Positional arguments
		default:
			if !strings.HasPrefix(arg, "-") {
				if p.command == "" {
					p.command = arg
				} else if p.project == "all" {
					p.project = arg
				}
			}
		}
		i++
	}

	// Default: no command → start with TUI
	if p.command == "" {
		p.command = "start"
		p.tui = true
	}

	return p
}

// =============================================================================
// Main
// =============================================================================

func main() {
	args := parseArgs(os.Args[1:])

	// Configure logging
	logLevel := slog.LevelInfo
	if args.verbose {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	// Help
	if args.help || args.command == "help" {
		fmt.Print(helpText)
		os.Exit(0)
	}

	// Default: start uses TUI unless user chose another mode
	if args.command == "start" && !args.bg && !args.dash {
		args.tui = true
	}

	// Route to command handler
	var exitCode int
	switch args.command {
	case "start":
		exitCode = cmdStart(args)
	case "stop":
		exitCode = cmdStop(args)
	case "status":
		exitCode = cmdStatus(args)
	case "list":
		exitCode = cmdList(args)
	case "ready":
		exitCode = cmdReady(args)
	case "config":
		exitCode = cmdConfig()
	default:
		// Bare project name → start with TUI
		// e.g. "brain-runner myproject" → start myproject --tui
		if args.command != "" && !strings.HasPrefix(args.command, "-") {
			args.project = args.command
			args.command = "start"
			args.tui = true
			exitCode = cmdStart(args)
		} else {
			fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args.command)
			fmt.Fprintln(os.Stderr, "Run 'brain-runner --help' for usage information")
			exitCode = 1
		}
	}

	os.Exit(exitCode)
}

// =============================================================================
// start command
// =============================================================================

func cmdStart(args parsedArgs) int {
	// Load config
	cfg, err := runner.LoadConfig()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return 1
	}

	// Apply CLI overrides
	if args.parallel > 0 {
		cfg.MaxParallel = args.parallel
	}
	if args.poll > 0 {
		cfg.PollInterval = args.poll
	}
	if args.workdir != "" {
		cfg.WorkDir = args.workdir
	}
	if args.agent != "" {
		cfg.Opencode.Agent = args.agent
	}
	if args.model != "" {
		cfg.Opencode.Model = args.model
	}

	// Merge CLI exclude patterns with config
	if len(args.exclude) > 0 {
		cfg.ExcludeProjects = append(cfg.ExcludeProjects, args.exclude...)
	}

	// Re-validate after overrides
	if err := runner.ValidateConfig(cfg); err != nil {
		slog.Error("invalid configuration after CLI overrides", "error", err)
		return 1
	}

	// Determine execution mode
	mode := runner.ExecutionModeBackground
	if args.tui {
		mode = runner.ExecutionModeTUI
	} else if args.dash {
		mode = runner.ExecutionModeDashboard
	}

	// Resolve projects
	projects, err := resolveProjects(cfg, args)
	if err != nil {
		slog.Error("failed to resolve projects", "error", err)
		return 1
	}
	if len(projects) == 0 {
		slog.Error("no projects found matching filters")
		return 1
	}

	// Use first project for state management (backward compat)
	primaryProject := projects[0]

	// Check if already running
	sm := runner.NewStateManager(cfg.StateDir, primaryProject)
	if sm.IsPidRunning() {
		pid := sm.LoadPid()
		slog.Error("runner already running", "project", primaryProject, "pid", pid)
		return 1
	}

	slog.Info("starting runner",
		"projects", projects,
		"mode", string(mode),
		"maxParallel", cfg.MaxParallel,
		"pollInterval", cfg.PollInterval,
	)

	// Wire up dependencies
	client := runner.NewAPIClient(cfg)
	executor := runner.NewExecutor(cfg)
	processMgr := runner.NewProcessManager(cfg)
	stateMgr := runner.NewStateManager(cfg.StateDir, primaryProject)

	// Build runner options
	opts := runner.TaskRunnerOptions{
		ProjectID:   primaryProject,
		Projects:    projects,
		Config:      cfg,
		Mode:        mode,
		StartPaused: mode == runner.ExecutionModeTUI,
		Client:      client,
		Executor:    executor,
		ProcessMgr:  processMgr,
		StateMgr:    stateMgr,
	}

	tr := runner.NewTaskRunner(opts)

	// Setup signal handler for graceful shutdown
	ctx := context.Background()
	sigHandler := runner.SetupSignalHandler(ctx, runner.SignalHandlerOptions{
		GracefulTimeout:  30 * time.Second,
		ForceKillTimeout: 5 * time.Second,
		OnShutdown: func() {
			slog.Info("shutting down runner")
			if stopErr := tr.Stop(); stopErr != nil {
				slog.Error("error during shutdown", "error", stopErr)
			}
		},
	})

	// Start the runner (blocks until context cancelled or Stop called)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Cancel context when signal handler initiates shutdown
	go func() {
		for {
			if sigHandler.IsShuttingDown() {
				cancel()
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	// TUI mode: launch Bubble Tea program
	if mode == runner.ExecutionModeTUI {
		tuiCfg := tui.Config{
			APIURL:   cfg.BrainAPIURL,
			Project:  primaryProject,
			Projects: projects,
		}
		model := tui.NewModel(tuiCfg)
		p := tea.NewProgram(model, tea.WithAltScreen())

		// Start the runner in background
		go func() {
			if startErr := tr.Start(ctx); startErr != nil {
				slog.Error("runner failed", "error", startErr)
			}
		}()

		// Run TUI (blocks until quit)
		if _, err := p.Run(); err != nil {
			slog.Error("TUI failed", "error", err)
			cancel()
			return 1
		}

		// TUI exited, stop the runner
		cancel()
		if stopErr := tr.Stop(); stopErr != nil {
			slog.Error("error stopping runner after TUI exit", "error", stopErr)
		}
		return 0
	}

	if err := tr.Start(ctx); err != nil {
		slog.Error("runner failed", "error", err)
		return 1
	}

	slog.Info("runner stopped")
	return 0
}

// resolveProjects determines which projects to monitor based on CLI args and config.
func resolveProjects(cfg runner.RunnerConfig, args parsedArgs) ([]string, error) {
	if args.project != "all" {
		return []string{args.project}, nil
	}

	// Fetch project list from API
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := runner.NewAPIClient(cfg)
	allProjects, err := client.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects from API: %w", err)
	}

	// Apply include/exclude filters
	filtered := filterProjects(allProjects, args.include, args.exclude, cfg.ExcludeProjects)
	return filtered, nil
}

// filterProjects applies include/exclude glob patterns to a project list.
func filterProjects(projects, include, exclude, configExclude []string) []string {
	// Merge CLI and config excludes
	allExclude := make([]string, 0, len(exclude)+len(configExclude))
	allExclude = append(allExclude, exclude...)
	allExclude = append(allExclude, configExclude...)

	var result []string
	for _, p := range projects {
		// If include patterns specified, project must match at least one
		if len(include) > 0 && !matchesAny(p, include) {
			continue
		}
		// Project must not match any exclude pattern
		if matchesAny(p, allExclude) {
			continue
		}
		result = append(result, p)
	}
	return result
}

// matchesAny checks if s matches any of the glob-like patterns.
func matchesAny(s string, patterns []string) bool {
	for _, pat := range patterns {
		if pat == "" {
			continue
		}
		if matchGlob(s, pat) {
			return true
		}
	}
	return false
}

// matchGlob implements simple glob matching with * wildcards.
func matchGlob(s, pattern string) bool {
	// Exact match
	if s == pattern {
		return true
	}

	// No wildcards → exact match only
	if !strings.Contains(pattern, "*") {
		return s == pattern
	}

	parts := strings.Split(pattern, "*")
	if len(parts) == 2 {
		prefix, suffix := parts[0], parts[1]
		return strings.HasPrefix(s, prefix) && strings.HasSuffix(s, suffix)
	}

	// Multi-wildcard: check parts appear in order
	pos := 0
	for i, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(s[pos:], part)
		if idx < 0 {
			return false
		}
		// First part must be prefix
		if i == 0 && idx != 0 {
			return false
		}
		pos += idx + len(part)
	}
	// Last part must reach end of string if it's not empty
	if last := parts[len(parts)-1]; last != "" {
		return strings.HasSuffix(s, last)
	}
	return true
}

// =============================================================================
// stop command
// =============================================================================

func cmdStop(args parsedArgs) int {
	cfg, err := runner.LoadConfig()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return 1
	}

	project := args.project
	if project == "all" {
		return stopAllRunners(cfg)
	}

	sm := runner.NewStateManager(cfg.StateDir, project)
	pid := sm.LoadPid()
	if pid == nil {
		slog.Warn("no runner found", "project", project)
		return 1
	}

	if !sm.IsPidRunning() {
		slog.Warn("runner not running (stale PID file)", "project", project, "pid", *pid)
		sm.ClearPid()
		return 1
	}

	slog.Info("stopping runner", "project", project, "pid", *pid)

	proc, err := os.FindProcess(*pid)
	if err != nil {
		slog.Error("failed to find process", "pid", *pid, "error", err)
		return 1
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		slog.Error("failed to send SIGTERM", "pid", *pid, "error", err)
		return 1
	}

	slog.Info("sent SIGTERM to runner", "pid", *pid)

	// Wait briefly for graceful shutdown
	time.Sleep(2 * time.Second)

	if sm.IsPidRunning() {
		slog.Warn("runner still running after SIGTERM, sending SIGKILL", "pid", *pid)
		_ = proc.Signal(syscall.SIGKILL)
	}

	sm.ClearPid()
	return 0
}

// stopAllRunners finds and stops all running runner instances.
func stopAllRunners(cfg runner.RunnerConfig) int {
	states := runner.FindAllRunnerStates(cfg.StateDir)
	if len(states) == 0 {
		slog.Info("no runners found")
		return 0
	}

	exitCode := 0
	for _, info := range states {
		sm := runner.NewStateManager(cfg.StateDir, info.ProjectID)
		pid := sm.LoadPid()
		if pid == nil || !sm.IsPidRunning() {
			continue
		}

		slog.Info("stopping runner", "project", info.ProjectID, "pid", *pid)
		proc, err := os.FindProcess(*pid)
		if err != nil {
			slog.Error("failed to find process", "project", info.ProjectID, "pid", *pid, "error", err)
			exitCode = 1
			continue
		}

		if err := proc.Signal(syscall.SIGTERM); err != nil {
			slog.Error("failed to send SIGTERM", "project", info.ProjectID, "pid", *pid, "error", err)
			exitCode = 1
		}
	}

	return exitCode
}

// =============================================================================
// status command
// =============================================================================

func cmdStatus(args parsedArgs) int {
	cfg, err := runner.LoadConfig()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return 1
	}

	project := args.project
	if project == "all" {
		return statusAllRunners(cfg)
	}

	sm := runner.NewStateManager(cfg.StateDir, project)
	state := sm.Load()
	pid := sm.LoadPid()
	isRunning := sm.IsPidRunning()

	if state == nil && pid == nil {
		fmt.Printf("Runner status: NOT STARTED (project: %s)\n", project)
		return 0
	}

	fmt.Printf("\nRunner Status: %s\n", project)
	fmt.Println(strings.Repeat("─", 40))

	statusStr := "STOPPED"
	if isRunning {
		statusStr = "RUNNING"
	}
	fmt.Printf("  Status:     %s\n", statusStr)

	if pid != nil {
		fmt.Printf("  PID:        %d\n", *pid)
	} else {
		fmt.Printf("  PID:        N/A\n")
	}

	if state != nil {
		fmt.Printf("  Started:    %s\n", state.StartedAt.Format(time.RFC3339))
		fmt.Printf("  Updated:    %s\n", state.UpdatedAt.Format(time.RFC3339))
		fmt.Printf("  Running:    %d task(s)\n", len(state.RunningTasks))
		fmt.Printf("  Completed:  %d\n", state.Stats.Completed)
		fmt.Printf("  Failed:     %d\n", state.Stats.Failed)

		if len(state.RunningTasks) > 0 {
			fmt.Println("\nRunning Tasks:")
			for _, task := range state.RunningTasks {
				fmt.Printf("  - %s (%s)\n", task.Title, task.ID)
				fmt.Printf("    Priority: %s, Started: %s\n", task.Priority, task.StartedAt.Format(time.RFC3339))
			}
		}
	}

	fmt.Println()
	return 0
}

// statusAllRunners shows status for all discovered runner instances.
func statusAllRunners(cfg runner.RunnerConfig) int {
	states := runner.FindAllRunnerStates(cfg.StateDir)
	if len(states) == 0 {
		fmt.Println("No runners found")
		return 0
	}

	fmt.Printf("\nAll Runners\n")
	fmt.Println(strings.Repeat("─", 80))

	for _, info := range states {
		sm := runner.NewStateManager(cfg.StateDir, info.ProjectID)
		state := sm.Load()
		pid := sm.LoadPid()
		isRunning := sm.IsPidRunning()

		statusStr := "STOPPED"
		if isRunning {
			statusStr = "RUNNING"
		}

		pidStr := "N/A"
		if pid != nil {
			pidStr = strconv.Itoa(*pid)
		}

		running := 0
		completed := 0
		failed := 0
		if state != nil {
			running = len(state.RunningTasks)
			completed = state.Stats.Completed
			failed = state.Stats.Failed
		}

		fmt.Printf("  %-20s  %-8s  PID=%-8s  tasks=%d  completed=%d  failed=%d\n",
			info.ProjectID, statusStr, pidStr, running, completed, failed)
	}

	fmt.Println()
	return 0
}

// =============================================================================
// list command
// =============================================================================

func cmdList(args parsedArgs) int {
	cfg, err := runner.LoadConfig()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return 1
	}

	project := args.project
	if project == "all" {
		return listProjects(cfg)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := runner.NewAPIClient(cfg)
	tasks, err := client.GetAllTasks(ctx, project)
	if err != nil {
		slog.Error("failed to list tasks", "project", project, "error", err)
		return 1
	}

	printTaskList("All Tasks", tasks)
	return 0
}

// listProjects lists all available projects.
func listProjects(cfg runner.RunnerConfig) int {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := runner.NewAPIClient(cfg)
	projects, err := client.ListProjects(ctx)
	if err != nil {
		slog.Error("failed to list projects", "error", err)
		return 1
	}

	fmt.Printf("\nProjects (%d)\n", len(projects))
	fmt.Println(strings.Repeat("─", 40))
	for _, p := range projects {
		fmt.Printf("  %s\n", p)
	}
	fmt.Println()
	return 0
}

// =============================================================================
// ready command
// =============================================================================

func cmdReady(args parsedArgs) int {
	cfg, err := runner.LoadConfig()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return 1
	}

	project := args.project
	if project == "all" {
		slog.Error("ready command requires a specific project")
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := runner.NewAPIClient(cfg)
	tasks, err := client.GetReadyTasks(ctx, project)
	if err != nil {
		slog.Error("failed to list ready tasks", "project", project, "error", err)
		return 1
	}

	printTaskList("Ready Tasks", tasks)
	return 0
}

// =============================================================================
// config command
// =============================================================================

func cmdConfig() int {
	cfg, err := runner.LoadConfig()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return 1
	}

	fmt.Printf("Config file: %s\n", runner.DefaultConfigPath())
	fmt.Println(strings.Repeat("─", 40))
	fmt.Printf("BRAIN_API_URL=%s\n", cfg.BrainAPIURL)
	fmt.Printf("Max parallel: %d\n", cfg.MaxParallel)
	fmt.Printf("Poll interval: %ds\n", cfg.PollInterval)
	if cfg.WorkDir != "" {
		fmt.Printf("Work dir: %s\n", cfg.WorkDir)
	} else {
		fmt.Println("Work dir: (default)")
	}
	fmt.Printf("State dir: %s\n", cfg.StateDir)
	fmt.Printf("Log dir: %s\n", cfg.LogDir)
	if cfg.Opencode.Agent != "" {
		fmt.Printf("Agent: %s\n", cfg.Opencode.Agent)
	}
	if cfg.Opencode.Model != "" {
		fmt.Printf("Model: %s\n", cfg.Opencode.Model)
	}

	// Full config as JSON
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		slog.Error("failed to marshal config", "error", err)
		return 1
	}
	fmt.Println()
	fmt.Println("Full config:")
	fmt.Println(string(data))
	return 0
}

// =============================================================================
// Task Printing Helpers
// =============================================================================

func printTaskList(title string, tasks []types.ResolvedTask) {
	fmt.Printf("\n%s (%d)\n", title, len(tasks))
	fmt.Println(strings.Repeat("─", 60))

	if len(tasks) == 0 {
		fmt.Println("  No tasks found")
		fmt.Println()
		return
	}

	for _, task := range tasks {
		depStr := ""
		if len(task.ResolvedDeps) > 0 {
			depStr = fmt.Sprintf(" [%d deps]", len(task.ResolvedDeps))
		}

		// Pad priority to 6 chars
		priority := task.Priority
		for len(priority) < 6 {
			priority += " "
		}

		fmt.Printf("  [%s] %s%s\n", priority, task.Title, depStr)
		fmt.Printf("           ID: %s | Status: %s\n", task.ID, task.Status)
	}

	fmt.Println()
}
