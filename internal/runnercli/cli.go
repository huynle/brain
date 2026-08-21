package runnercli

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
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
	KeyBindings map[string]string // TUI keybinding overrides from config
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
	// StateDir feeds filepath.Join for state, prompt, output-log and runner-script
	// paths. Leaving it empty makes every one of those relative to the process
	// working directory, so resolve the default explicitly.
	if cfg.StateDir == "" {
		cfg.StateDir = runner.DefaultStateDir()
	}
	// Allow env var override for opencode binary
	if bin := os.Getenv("OPENCODE_BIN"); bin != "" {
		cfg.Opencode.Bin = bin
	}

	// Wire up dependencies
	client := runner.NewAPIClient(cfg)
	executorRegistry := runner.NewExecutorRegistry(cfg)
	processMgr := runner.NewProcessManager(cfg)
	stateMgr := runner.NewStateManager(cfg.StateDir, opts.Projects[0])

	// Build runner options
	runnerOpts := runner.TaskRunnerOptions{
		ProjectID:        opts.Projects[0],
		Projects:         opts.Projects,
		Config:           cfg,
		Mode:             runner.ExecutionMode(opts.Mode),
		StartPaused:      opts.StartPaused,
		Client:           client,
		Executor:         executorRegistry.MustGet("opencode"),
		ExecutorRegistry: executorRegistry,
		ProcessMgr:       processMgr,
		StateMgr:         stateMgr,
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

// RunMonitorTUI starts the TUI in monitor-only mode — no local TaskRunner.
// The TUI connects to the remote Brain API via SSE for real-time task data
// and uses HTTP API calls for pause/resume and task execution (priority bump).
// This is a pure control panel mode for observing and managing remote runners.
func RunMonitorTUI(ctx context.Context, opts RunnerOptions) error {
	if len(opts.Projects) == 0 {
		return fmt.Errorf("no projects specified")
	}

	cfg := opts.Config

	// Apply defaults for required fields
	if cfg.APITimeout == 0 {
		cfg.APITimeout = 5000
	}

	// In TUI mode, redirect logging to a file or discard so it doesn't
	// corrupt Bubbletea's alternate screen.
	if cfg.LogDir != "" {
		if err := os.MkdirAll(cfg.LogDir, 0o755); err == nil {
			logPath := filepath.Join(cfg.LogDir, "monitor.log")
			logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if err == nil {
				defer func() { _ = logFile.Close() }()
				slog.SetDefault(slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{
					Level: slog.LevelDebug,
				})))
			}
		}
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	}

	// Get BrainDir from environment or use default
	brainDir := os.Getenv("BRAIN_DIR")
	if brainDir == "" {
		homeDir, _ := os.UserHomeDir()
		brainDir = homeDir + "/.brain"
	}
	brainDir = pathutil.ExpandTilde(brainDir)

	// Create TUI model with Runner: nil (monitor-only, no embedded runner)
	tuiCfg := tui.Config{
		APIURL:      cfg.BrainAPIURL,
		APIToken:    cfg.APIToken,
		APITimeout:  cfg.APITimeout,
		Project:     opts.Projects[0],
		Projects:    opts.Projects,
		BrainDir:    brainDir,
		LogDir:      cfg.LogDir,
		Runner:      nil, // No local runner — pure monitor mode
		KeyBindings: opts.KeyBindings,
	}
	model := tui.NewModel(tuiCfg)
	p := tea.NewProgram(model, tea.WithAltScreen())

	slog.Info("starting monitor TUI", "projects", opts.Projects, "api", cfg.BrainAPIURL)

	// Run TUI (blocks until quit)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("monitor TUI failed: %w", err)
	}

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
	// StateDir feeds filepath.Join for state, prompt, output-log and runner-script
	// paths. Leaving it empty makes every one of those relative to the process
	// working directory, so resolve the default explicitly.
	if cfg.StateDir == "" {
		cfg.StateDir = runner.DefaultStateDir()
	}
	// Allow env var override for opencode binary
	if bin := os.Getenv("OPENCODE_BIN"); bin != "" {
		cfg.Opencode.Bin = bin
	}

	// Wire up dependencies
	client := runner.NewAPIClient(cfg)
	executorRegistry := runner.NewExecutorRegistry(cfg)
	processMgr := runner.NewProcessManager(cfg)
	stateMgr := runner.NewStateManager(cfg.StateDir, opts.Projects[0])

	// In TUI mode, redirect the runner's log.Logger to a file (or discard)
	// so it doesn't write to stderr and corrupt Bubbletea's alternate screen.
	var runnerLogger *log.Logger
	if cfg.LogDir != "" {
		if err := os.MkdirAll(cfg.LogDir, 0o755); err == nil {
			logPath := filepath.Join(cfg.LogDir, "runner.log")
			logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if err == nil {
				defer func() { _ = logFile.Close() }()
				runnerLogger = log.New(logFile, "", log.LstdFlags)
				// Redirect slog to the same log file so structured logging
				// (slog.Info/Warn/Debug) doesn't write to stderr and corrupt
				// Bubbletea's alternate screen.
				slog.SetDefault(slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{
					Level: slog.LevelDebug,
				})))
			}
		}
	}
	if runnerLogger == nil {
		// Fallback: discard all runner logs rather than corrupt the TUI
		runnerLogger = log.New(io.Discard, "", 0)
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	}

	// Build runner options
	runnerOpts := runner.TaskRunnerOptions{
		ProjectID:        opts.Projects[0],
		Projects:         opts.Projects,
		Config:           cfg,
		Mode:             runner.ExecutionModeTUI,
		StartPaused:      true,
		Logger:           runnerLogger,
		Client:           client,
		Executor:         executorRegistry.MustGet("opencode"),
		ExecutorRegistry: executorRegistry,
		ProcessMgr:       processMgr,
		StateMgr:         stateMgr,
	}

	tr := runner.NewTaskRunner(runnerOpts)

	// Register event logger for durable file-based logging alongside the TUI panel.
	// Both the TUI OnEvent bridge (for the log panel) and the event logger (for
	// the log file) run — they serve different purposes.
	runner.RegisterEventLogger(tr)

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
		APIURL:      cfg.BrainAPIURL,
		APIToken:    cfg.APIToken,
		APITimeout:  cfg.APITimeout,
		Project:     opts.Projects[0],
		Projects:    opts.Projects,
		BrainDir:    brainDir,
		LogDir:      cfg.LogDir,
		Runner:      tr, // Wire the embedded runner directly to the TUI
		KeyBindings: opts.KeyBindings,
	}
	model := tui.NewModel(tuiCfg)

	// Create context that cancels on signal or ctx.Done
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// tea.WithContext kills the program when runCtx is cancelled, so RunTUI
	// actually returns on context cancellation instead of blocking forever.
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(runCtx))

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
		case runner.EventRunnerStarted:
			entry.Level = "info"
			entry.Message = fmt.Sprintf("Runner %s started (%s, projects=%v)", event.RunnerID, event.Mode, event.Projects)
		case runner.EventTaskClaimed:
			entry.Level = "info"
			entry.Message = fmt.Sprintf("Task claimed: %s [%s]", event.TaskID, event.RunnerID)
			entry.TaskID = event.TaskID
			entry.ProjectID = event.ProjectID
		case runner.EventTaskClaimRejected:
			entry.Level = "warn"
			entry.Message = fmt.Sprintf("Claim rejected: %s (held by %s)", event.TaskID, event.ClaimedBy)
			entry.TaskID = event.TaskID
		case runner.EventTaskStatusChanged:
			entry.Level = "info"
			entry.Message = fmt.Sprintf("Status: %s %s → %s", event.TaskID, event.FromStatus, event.ToStatus)
			entry.TaskID = event.TaskID
			entry.ProjectID = event.ProjectID
		case runner.EventTaskReleased:
			entry.Level = "warn"
			entry.Message = fmt.Sprintf("Task released: %s", event.TaskID)
			entry.TaskID = event.TaskID
		case runner.EventTaskStarted:
			if event.Task != nil {
				entry.Level = "info"
				entry.Message = fmt.Sprintf("Task started: %s [%s] pid=%d", event.Task.Title, event.RunnerID, event.Task.PID)
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
				entry.Message = fmt.Sprintf("Task completed: %s [%s] (exit %d, %dms)", event.Result.TaskID, event.RunnerID, exitCode, event.Result.Duration)
				entry.TaskID = event.Result.TaskID
			}
		case runner.EventTaskFailed:
			if event.Result != nil {
				entry.Level = "error"
				exitCode := 0
				if event.Result.ExitCode != nil {
					exitCode = *event.Result.ExitCode
				}
				entry.Message = fmt.Sprintf("Task failed: %s [%s] (exit %d)", event.Result.TaskID, event.RunnerID, exitCode)
				entry.TaskID = event.Result.TaskID
			}
		case runner.EventTaskCancelled:
			entry.Level = "warn"
			entry.Message = fmt.Sprintf("Task cancelled: %s [%s]", event.TaskID, event.RunnerID)
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
			if runCtx.Err() != nil {
				// Killed by context cancellation or signal shutdown, not a TUI failure.
				return ctx.Err()
			}
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
