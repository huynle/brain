package runnercli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/tui"
	"github.com/huynle/brain-api/pkg/pathutil"
)

// RunnerOptions holds options for running the task runner.
// Config uses runner.RunnerConfig directly so all fields pass through
// without lossy field-by-field copying.
type RunnerOptions struct {
	Projects    []string
	Mode        string
	StartPaused bool
	Config      runner.RunnerConfig
}

// RunTaskRunner starts the task runner in daemon mode and blocks until context is cancelled.
func RunTaskRunner(ctx context.Context, opts RunnerOptions) error {
	if len(opts.Projects) == 0 {
		return fmt.Errorf("no projects specified")
	}

	// Use the full config directly — no lossy field-by-field copying
	cfg := opts.Config

	// Apply defaults for required fields if not set
	if cfg.MaxParallel == 0 {
		cfg.MaxParallel = 3
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 10
	}
	if cfg.APITimeout == 0 {
		cfg.APITimeout = 5000
	}
	if cfg.Opencode.Bin == "" {
		cfg.Opencode.Bin = "opencode"
	}
	// Allow env var override for opencode binary
	if bin := os.Getenv("OPENCODE_BIN"); bin != "" {
		cfg.Opencode.Bin = bin
	}

	// Wire up dependencies
	client := runner.NewAPIClient(cfg)
	executor := runner.NewExecutor(cfg)
	processMgr := runner.NewProcessManager(cfg)
	stateMgr := runner.NewStateManager(cfg.StateDir, opts.Projects[0])

	// Build runner options
	runnerOpts := runner.TaskRunnerOptions{
		ProjectID:   opts.Projects[0],
		Projects:    opts.Projects,
		Config:      cfg,
		Mode:        runner.ExecutionMode(opts.Mode),
		StartPaused: opts.StartPaused,
		Client:      client,
		Executor:    executor,
		ProcessMgr:  processMgr,
		StateMgr:    stateMgr,
	}

	tr := runner.NewTaskRunner(runnerOpts)

	// Setup signal handler for graceful shutdown
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

	// Create a derived context that cancels when signal handler initiates shutdown
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		for {
			select {
			case <-ctx.Done():
				cancel()
				return
			default:
				if sigHandler.IsShuttingDown() {
					cancel()
					return
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()

	// Wire event logging for headless mode so task lifecycle events
	// are visible via slog instead of silently discarded.
	runner.RegisterEventLogger(tr)

	// Start the runner (blocks until context cancelled or Stop called)
	if err := tr.Start(runCtx); err != nil {
		return fmt.Errorf("runner failed: %w", err)
	}

	slog.Info("runner stopped")
	return nil
}

// RunTUI starts the task runner with interactive TUI and blocks until context is cancelled or user quits.
func RunTUI(ctx context.Context, opts RunnerOptions) error {
	if len(opts.Projects) == 0 {
		return fmt.Errorf("no projects specified")
	}

	// Use the full config directly — no lossy field-by-field copying
	cfg := opts.Config

	// Apply defaults for required fields if not set
	if cfg.MaxParallel == 0 {
		cfg.MaxParallel = 3
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 10
	}
	if cfg.APITimeout == 0 {
		cfg.APITimeout = 5000
	}
	if cfg.Opencode.Bin == "" {
		cfg.Opencode.Bin = "opencode"
	}
	// Allow env var override for opencode binary
	if bin := os.Getenv("OPENCODE_BIN"); bin != "" {
		cfg.Opencode.Bin = bin
	}

	// Wire up dependencies
	client := runner.NewAPIClient(cfg)
	executor := runner.NewExecutor(cfg)
	processMgr := runner.NewProcessManager(cfg)
	stateMgr := runner.NewStateManager(cfg.StateDir, opts.Projects[0])

	// Build runner options
	runnerOpts := runner.TaskRunnerOptions{
		ProjectID:   opts.Projects[0],
		Projects:    opts.Projects,
		Config:      cfg,
		Mode:        runner.ExecutionModeTUI,
		StartPaused: true,
		Client:      client,
		Executor:    executor,
		ProcessMgr:  processMgr,
		StateMgr:    stateMgr,
	}

	tr := runner.NewTaskRunner(runnerOpts)

	// Setup signal handler
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

	// Create TUI model
	// Get BrainDir from environment or use default
	brainDir := os.Getenv("BRAIN_DIR")
	if brainDir == "" {
		homeDir, _ := os.UserHomeDir()
		brainDir = homeDir + "/.brain"
	}
	brainDir = pathutil.ExpandTilde(brainDir)

	tuiCfg := tui.Config{
		APIURL:     cfg.BrainAPIURL,
		APIToken:   cfg.APIToken,
		APITimeout: cfg.APITimeout,
		Project:    opts.Projects[0],
		Projects:   opts.Projects,
		BrainDir:   brainDir,
		LogDir:     cfg.LogDir,
		Runner:     tr, // Wire the embedded runner directly to the TUI
	}
	model := tui.NewModel(tuiCfg)
	p := tea.NewProgram(model, tea.WithAltScreen())

	// Create context that cancels on signal or ctx.Done
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		for {
			select {
			case <-ctx.Done():
				cancel()
				return
			default:
				if sigHandler.IsShuttingDown() {
					cancel()
					return
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()

	// Wire runner events to TUI log panel + process metrics tracking
	tr.OnEvent(func(event runner.RunnerEvent) {
		var entry tui.LogEntry
		entry.Timestamp = time.Now()

		switch event.Type {
		case runner.EventTaskStarted:
			if event.Task != nil {
				entry.Level = "info"
				entry.Message = fmt.Sprintf("Task started: %s", event.Task.Title)
				entry.TaskID = event.Task.ID
				entry.ProjectID = event.Task.ProjectID
				// Track the process PID for metrics (CPU/mem/proc count)
				if event.Task.PID > 0 {
					p.Send(tui.ProcessStartedMsg{
						PID:    event.Task.PID,
						TaskID: event.Task.ID,
					})
				}
			}
		case runner.EventTaskCompleted:
			if event.Result != nil {
				entry.Level = "info"
				exitCode := 0
				if event.Result.ExitCode != nil {
					exitCode = *event.Result.ExitCode
				}
				entry.Message = fmt.Sprintf("Task completed: %s (exit %d)", event.Result.TaskID, exitCode)
				entry.TaskID = event.Result.TaskID
			}
		case runner.EventTaskFailed:
			if event.Result != nil {
				entry.Level = "error"
				exitCode := 0
				if event.Result.ExitCode != nil {
					exitCode = *event.Result.ExitCode
				}
				entry.Message = fmt.Sprintf("Task failed: %s (exit %d)", event.Result.TaskID, exitCode)
				entry.TaskID = event.Result.TaskID
			}
		case runner.EventTaskCancelled:
			entry.Level = "warn"
			entry.Message = fmt.Sprintf("Task cancelled: %s", event.TaskID)
			entry.TaskID = event.TaskID
		case runner.EventProjectPaused:
			entry.Level = "info"
			entry.Message = fmt.Sprintf("Project paused: %s", event.ProjectID)
			entry.ProjectID = event.ProjectID
		case runner.EventProjectResumed:
			entry.Level = "info"
			entry.Message = fmt.Sprintf("Project resumed: %s", event.ProjectID)
			entry.ProjectID = event.ProjectID
		case runner.EventAllPaused:
			entry.Level = "info"
			entry.Message = "All projects paused"
		case runner.EventAllResumed:
			entry.Level = "info"
			entry.Message = "All projects resumed"
		case runner.EventPollComplete:
			entry.Level = "debug"
			entry.Message = fmt.Sprintf("Poll complete: %d ready, %d running", event.ReadyCount, event.RunningCount)
		case runner.EventSessionDiscovered:
			// Send session info directly to TUI for in-memory storage
			p.Send(tui.SessionDiscoveredMsg{
				TaskPath:  event.TaskPath,
				SessionID: event.SessionID,
			})
			entry.Level = "info"
			entry.Message = fmt.Sprintf("Session discovered: %s", event.SessionID)
		case runner.EventShutdown:
			entry.Level = "info"
			entry.Message = "Runner shutting down"
			if event.Reason != "" {
				entry.Message += ": " + event.Reason
			}
		default:
			return // Don't log unknown events
		}

		if entry.Message != "" {
			p.Send(tui.LogEntryMsg{Entry: entry})
		}
	})

	// Start the runner in background
	go func() {
		if startErr := tr.Start(runCtx); startErr != nil {
			slog.Error("runner failed", "error", startErr)
		}
	}()

	// Run TUI (blocks until quit or context cancelled)
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		if _, err := p.Run(); err != nil {
			cancel()
			_ = tr.Stop()
			return fmt.Errorf("TUI failed: %w", err)
		}
	}

	// TUI exited, stop the runner
	cancel()
	if stopErr := tr.Stop(); stopErr != nil {
		slog.Error("error stopping runner after TUI exit", "error", stopErr)
	}

	return nil
}
