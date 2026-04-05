package commands

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
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
		TLS        struct {
			Enabled  bool
			CertPath string
			KeyPath  string
		}
		PIDFile       string
		LogFile       string
		LogMaxSize    int // MB
		LogMaxBackups int
		LogMaxAge     int // days
		TaskDefaults  config.TaskDefaultsConfig
	}
	Runner runner.RunnerConfig
	MCP    struct {
		APIURL string
	}
	TUI struct {
		KeyBindings map[string]string
	}
}

// APIFlags holds API server command flags.
type APIFlags struct {
	Port    int
	Host    string
	Daemon  bool
	LogFile string
	TLS     bool
	TLSCert string
	TLSKey  string
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
		Port:         c.Config.Server.Port,
		Host:         c.Config.Server.Host,
		BrainDir:     c.Config.Server.BrainDir,
		EnableAuth:   c.Config.Server.EnableAuth,
		LogLevel:     c.Config.Server.LogLevel,
		CORSOrigin:   c.Config.Server.CORSOrigin,
		OAuthPIN:     c.Config.Server.OAuthPIN,
		TaskDefaults: c.Config.Server.TaskDefaults,
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

	// If daemon mode, handle daemonization
	if c.Flags.Daemon {
		pidFile := c.Config.Server.PIDFile
		if pidFile == "" {
			homeDir, _ := os.UserHomeDir()
			pidFile = filepath.Join(homeDir, ".local", "state", "brain-api", "brain-api.pid")
		}

		logFile := c.Flags.LogFile
		if logFile == "" {
			logFile = c.Config.Server.LogFile
		}
		if logFile == "" {
			homeDir, _ := os.UserHomeDir()
			logFile = filepath.Join(homeDir, ".local", "state", "brain-api", "brain-api.log")
		}

		return daemonizeServer(ctx, opts, pidFile, logFile, c.Config)
	}

	// Otherwise run in foreground
	fmt.Printf("Starting Brain API server on %s:%d\n", opts.Host, opts.Port)
	return apiserver.RunServer(ctx, opts)
}

// daemonizeServer handles daemon mode for the server with SIGHUP log rotation.
// In daemon mode, we write the PID file when the server starts and setup signal handlers.
// The StartCommand is responsible for the actual fork/detach via lifecycle.Daemonize.
func daemonizeServer(ctx context.Context, opts apiserver.ServerOptions, pidFile, logFile string, cfg *UnifiedConfig) error {
	// Check if already running (skip if the PID file contains our own PID,
	// which happens when the parent wrote it before we started)
	if pid, err := lifecycle.ReadPID(pidFile); err == nil {
		if pid != os.Getpid() && lifecycle.IsProcessRunning(pid) {
			return fmt.Errorf("server already running (PID %d)", pid)
		}
		if pid != os.Getpid() {
			// Clean up stale PID from a dead process
			lifecycle.ClearPID(pidFile)
		}
	}

	// Write PID file
	if err := lifecycle.WritePID(pidFile, os.Getpid()); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	// Setup cleanup on exit
	defer lifecycle.ClearPID(pidFile)

	// Create context for shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Get log rotation config
	maxSizeMB := cfg.Server.LogMaxSize
	if maxSizeMB == 0 {
		maxSizeMB = 100 // Default 100MB
	}
	maxBackups := cfg.Server.LogMaxBackups
	if maxBackups == 0 {
		maxBackups = 5 // Default 5 backups
	}

	// Setup signal handlers with log rotation on SIGHUP
	_ = lifecycle.SetupSignalHandler(ctx, lifecycle.SignalHandlerOptions{
		OnShutdown: func() {
			slog.Info("received shutdown signal")
			cancel()
		},
		OnReload: func() {
			slog.Info("received SIGHUP, rotating logs")
			// Rotate logs
			if err := lifecycle.RotateLogs(logFile, int64(maxSizeMB), maxBackups); err != nil {
				slog.Error("failed to rotate logs", "error", err)
			} else {
				slog.Info("log rotation complete")
			}
		},
	})

	fmt.Printf("Starting Brain API server on %s:%d (PID %d)\n", opts.Host, opts.Port, os.Getpid())
	fmt.Printf("Logs: %s\n", logFile)
	fmt.Printf("PID file: %s\n", pidFile)

	// Run server
	return apiserver.RunServer(ctx, opts)
}
