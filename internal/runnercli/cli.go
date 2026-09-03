package runnercli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/huynle/brain-api/internal/runner"
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

// prepareRunnerConfig fills in the defaults a runner needs before it is built,
// and is the ONE place runner identity is resolved — every path that starts a
// local TaskRunner (`brain run start`, `brain runner start`, the API server's
// embedded runner) funnels through here.
//
// Identity has to land after CLI flags are merged (that is where --name arrives)
// and exactly once (ResolveRunnerIdentity is not idempotent), which is why it
// lives here rather than in LoadConfig.
func prepareRunnerConfig(cfg *runner.RunnerConfig) error {
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
	// working directory, so ResolveRunnerIdentity resolves the default explicitly
	// before appending the runner name.
	return runner.ResolveRunnerIdentity(cfg)
}

// RunTaskRunner starts the task runner in daemon mode and blocks until context is cancelled.
func RunTaskRunner(ctx context.Context, opts RunnerOptions) error {
	if len(opts.Projects) == 0 {
		return fmt.Errorf("no projects specified")
	}

	// Use the full config directly — no lossy field-by-field copying
	cfg := opts.Config

	if err := prepareRunnerConfig(&cfg); err != nil {
		return err
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
