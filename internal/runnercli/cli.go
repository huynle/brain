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
)

// RunnerConfig holds configuration for the runner.
type RunnerConfig struct {
	BrainAPIURL  string
	MaxParallel  int
	PollInterval int
	WorkDir      string
	StateDir     string
	LogDir       string
}

// RunnerOptions holds options for running the task runner.
type RunnerOptions struct {
	Projects    []string
	Mode        string
	StartPaused bool
	Config      RunnerConfig
}

// RunTaskRunner starts the task runner in daemon mode and blocks until context is cancelled.
func RunTaskRunner(ctx context.Context, opts RunnerOptions) error {
	if len(opts.Projects) == 0 {
		return fmt.Errorf("no projects specified")
	}

	// Convert RunnerConfig to runner.RunnerConfig
	cfg := runner.RunnerConfig{
		BrainAPIURL:  opts.Config.BrainAPIURL,
		MaxParallel:  opts.Config.MaxParallel,
		PollInterval: opts.Config.PollInterval,
		WorkDir:      opts.Config.WorkDir,
		StateDir:     opts.Config.StateDir,
		LogDir:       opts.Config.LogDir,
	}

	// Set defaults if not provided
	if cfg.MaxParallel == 0 {
		cfg.MaxParallel = 3
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 10
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

	// Convert RunnerConfig to runner.RunnerConfig
	cfg := runner.RunnerConfig{
		BrainAPIURL:  opts.Config.BrainAPIURL,
		MaxParallel:  opts.Config.MaxParallel,
		PollInterval: opts.Config.PollInterval,
		WorkDir:      opts.Config.WorkDir,
		StateDir:     opts.Config.StateDir,
		LogDir:       opts.Config.LogDir,
	}

	// Set defaults
	if cfg.MaxParallel == 0 {
		cfg.MaxParallel = 3
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 10
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

	tuiCfg := tui.Config{
		APIURL:   cfg.BrainAPIURL,
		Project:  opts.Projects[0],
		Projects: opts.Projects,
		BrainDir: brainDir,
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
