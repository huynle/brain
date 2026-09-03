package commands

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/huynle/brain-api/internal/apiserver"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/lifecycle"
	"github.com/huynle/brain-api/internal/runner"
)

// UnifiedConfig represents the unified configuration structure.
// Runner uses runner.RunnerConfig directly so all fields pass through
// without lossy field-by-field copying.
type UnifiedConfig struct {
	Server struct {
		Port       int
		Host       string
		BrainDir   string
		EnableAuth bool
		LogLevel   string
		CORSOrigin string
		OAuthPIN   string
		JWTSecret  string
		TLS        struct {
			Enabled  bool
			CertPath string
			KeyPath  string
		}
		PIDFile         string
		LogFile         string
		LogMaxSize      int // MB
		LogMaxBackups   int
		TaskDefaults    config.TaskDefaultsConfig
		FeatureCheckout config.FeatureCheckoutConfig
		IndexWatch      config.IndexWatchConfig
		Embedding       config.EmbeddingConfig
		Attachments     config.AttachmentConfig

		AttachmentExtraction config.AttachmentExtractionConfig
		Assistant            config.AssistantConfig
	}
	Runner runner.RunnerConfig
	MCP    struct {
		APIURL string
	}
}

// APIFlags holds API server command flags.
type APIFlags struct {
	Port          int
	Host          string
	Daemon        bool
	LogFile       string
	TLS           bool
	TLSCert       string
	TLSKey        string
	Runner        bool
	RunnerProject string
	RunnerName    string
	MaxParallel   int
	Include       []string
	Exclude       []string
	Executor      string
}

// APICommand implements the Command interface for the api command.
type APICommand struct {
	Config *UnifiedConfig
	Flags  *APIFlags
}

// Type returns the command type identifier.
func (c *APICommand) Type() string {
	return "api"
}

// Execute starts the Brain API server.
func (c *APICommand) Execute() error {
	// Build options from config + flags
	opts := apiserver.ServerOptions{
		Port:            c.Config.Server.Port,
		Host:            c.Config.Server.Host,
		BrainDir:        c.Config.Server.BrainDir,
		EnableAuth:      c.Config.Server.EnableAuth,
		LogLevel:        c.Config.Server.LogLevel,
		CORSOrigin:      c.Config.Server.CORSOrigin,
		OAuthPIN:        c.Config.Server.OAuthPIN,
		JWTSecret:       c.Config.Server.JWTSecret,
		TaskDefaults:    c.Config.Server.TaskDefaults,
		FeatureCheckout: c.Config.Server.FeatureCheckout,
		IndexWatch:      c.Config.Server.IndexWatch,
		Embedding:       c.Config.Server.Embedding,
		Attachments:     c.Config.Server.Attachments,

		AttachmentExtraction: c.Config.Server.AttachmentExtraction,
		Assistant:            c.Config.Server.Assistant,

		TLSCert: c.Config.Server.TLS.CertPath,
		TLSKey:  c.Config.Server.TLS.KeyPath,
	}

	// Flags override config
	if c.Flags.Port != 0 {
		opts.Port = c.Flags.Port
	}
	if c.Flags.Host != "" {
		opts.Host = c.Flags.Host
	}

	// Create context with signal handling for graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Open the configured log file so slog output lands there in every mode
	// and `brain api logs -f` works. Falls back to stderr-only on failure.
	logFile := resolveLogFile(c.Config, c.Flags.LogFile)
	logWriter := openServerLogWriter(c.Config, logFile)

	// If daemon mode, handle daemonization
	if c.Flags.Daemon {
		pidFile := c.Config.Server.PIDFile
		if pidFile == "" {
			pidFile = defaultPIDFile()
		}

		// The parent already redirected this process's stdout/stderr to the
		// log file; route slog through the rotating writer only, so lines
		// are not duplicated and rotation keeps working.
		if logWriter != nil {
			opts.LogWriter = logWriter
		}
		return daemonizeServer(ctx, opts, pidFile, logWriter, c.Config, lifecycleFlagsFromAPIFlags(c.Flags))
	}

	// Otherwise run in foreground: tee slog to the terminal and the log file.
	if logWriter != nil {
		opts.LogWriter = io.MultiWriter(os.Stderr, logWriter)
	}
	fmt.Printf("Starting Brain API server on %s:%d\n", opts.Host, opts.Port)
	fmt.Printf("Logs: %s\n", logFile)
	return runServerWithOptionalRunner(ctx, c.Config, opts, lifecycleFlagsFromAPIFlags(c.Flags))
}

// resolveLogFile returns the effective server log path: flag > config > default.
func resolveLogFile(cfg *UnifiedConfig, flagLogFile string) string {
	if flagLogFile != "" {
		return flagLogFile
	}
	if cfg.Server.LogFile != "" {
		return cfg.Server.LogFile
	}
	return defaultLogFile()
}

// openServerLogWriter opens a size-rotating writer for the server log file.
// Returns nil (with a warning) if the file cannot be opened, in which case
// the server logs to stderr only.
func openServerLogWriter(cfg *UnifiedConfig, logFile string) *lifecycle.RotatingWriter {
	w, err := lifecycle.NewRotatingWriter(logFile, cfg.Server.LogMaxSize, cfg.Server.LogMaxBackups)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot open log file %s: %v (logging to stderr only)\n", logFile, err)
		return nil
	}
	return w
}

// daemonizeServer handles daemon mode for the server with SIGHUP log reopen.
// In daemon mode, we write the PID file when the server starts and setup signal handlers.
// The StartCommand is responsible for the actual fork/detach via lifecycle.Daemonize.
func daemonizeServer(ctx context.Context, opts apiserver.ServerOptions, pidFile string, logWriter *lifecycle.RotatingWriter, cfg *UnifiedConfig, flags LifecycleFlags) error {
	// Check if already running (skip if the PID file contains our own PID,
	// which happens when the parent wrote it before we started)
	if pid, err := lifecycle.ReadPID(pidFile); err == nil {
		if pid != os.Getpid() && lifecycle.IsProcessRunning(pid) {
			return fmt.Errorf("server already running (PID %d)", pid)
		}
		if pid != os.Getpid() {
			// Clean up stale PID from a dead process
			// Best-effort PID-file cleanup on a shutdown path.
			_ = lifecycle.ClearPID(pidFile)
		}
	}

	// Write PID file
	if err := lifecycle.WritePID(pidFile, os.Getpid()); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	// Setup cleanup on exit
	// Best-effort PID-file cleanup on a shutdown path.
	defer func() { _ = lifecycle.ClearPID(pidFile) }()

	// Create context for shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Setup signal handlers. Size-based rotation happens automatically in
	// the rotating writer; SIGHUP reopens the file so external rotation
	// (e.g. logrotate moving the file) doesn't strand writes on the old inode.
	_ = lifecycle.SetupSignalHandler(ctx, lifecycle.SignalHandlerOptions{
		OnShutdown: func() {
			slog.Info("received shutdown signal")
			cancel()
		},
		OnReload: func() {
			slog.Info("received SIGHUP, reopening log file")
			if logWriter == nil {
				return
			}
			if err := logWriter.Reopen(); err != nil {
				slog.Error("failed to reopen log file", "error", err)
			}
		},
	})

	logFile := "(stderr)"
	if logWriter != nil {
		logFile = logWriter.Path()
	}
	fmt.Printf("Starting Brain API server on %s:%d (PID %d)\n", opts.Host, opts.Port, os.Getpid())
	fmt.Printf("Logs: %s\n", logFile)
	fmt.Printf("PID file: %s\n", pidFile)

	// Run server
	return runServerWithOptionalRunner(ctx, cfg, opts, flags)
}
