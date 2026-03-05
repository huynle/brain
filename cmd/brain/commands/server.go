package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/huynle/brain-api/internal/apiserver"
	"github.com/huynle/brain-api/internal/lifecycle"
)

// UnifiedConfig represents the unified configuration structure.
// This mirrors the type from cmd/brain/flags.go but is defined here
// to avoid circular imports with the main package.
type UnifiedConfig struct {
	Server struct {
		Port       int
		Host       string
		BrainDir   string
		EnableAuth bool
		APIKey     string
		LogLevel   string
		TLS        struct {
			Enabled  bool
			CertPath string
			KeyPath  string
		}
		PIDFile string
		LogFile string
	}
	Runner struct {
		MaxParallel     int
		PollInterval    int
		WorkDir         string
		StateDir        string
		LogDir          string
		ExcludeProjects []string
		OpenCode        struct {
			Agent string
			Model string
		}
	}
	MCP struct {
		APIURL string
	}
}

// ServerFlags holds server command flags.
type ServerFlags struct {
	Port    int
	Host    string
	Daemon  bool
	LogFile string
	TLS     bool
	TLSCert string
	TLSKey  string
}

// ServerCommand implements the Command interface for the server command.
type ServerCommand struct {
	Config *UnifiedConfig
	Flags  *ServerFlags
}

// Type returns the command type identifier.
func (c *ServerCommand) Type() string {
	return "server"
}

// Execute starts the Brain API server.
func (c *ServerCommand) Execute() error {
	// Build options from config + flags
	opts := apiserver.ServerOptions{
		Port:       c.Config.Server.Port,
		Host:       c.Config.Server.Host,
		BrainDir:   c.Config.Server.BrainDir,
		EnableAuth: c.Config.Server.EnableAuth,
		APIKey:     c.Config.Server.APIKey,
		LogLevel:   c.Config.Server.LogLevel,
	}

	// Flags override config
	if c.Flags.Port != 0 {
		opts.Port = c.Flags.Port
	}
	if c.Flags.Host != "" {
		opts.Host = c.Flags.Host
	}

	// Create context
	ctx := context.Background()

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

		return daemonizeServer(ctx, opts, pidFile, logFile)
	}

	// Otherwise run in foreground
	fmt.Printf("Starting Brain API server on %s:%d\n", opts.Host, opts.Port)
	return apiserver.RunServer(ctx, opts)
}

// daemonizeServer handles daemon mode for the server.
// In daemon mode, we write the PID file when the server starts.
// The StartCommand is responsible for the actual fork/detach via lifecycle.Daemonize.
func daemonizeServer(ctx context.Context, opts apiserver.ServerOptions, pidFile, logFile string) error {
	// Check if already running
	if pid, err := lifecycle.ReadPID(pidFile); err == nil {
		if lifecycle.IsProcessRunning(pid) {
			return fmt.Errorf("server already running (PID %d)", pid)
		}
		// Clean up stale PID
		lifecycle.ClearPID(pidFile)
	}

	// Write PID file
	if err := lifecycle.WritePID(pidFile, os.Getpid()); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	// Setup cleanup on exit
	defer lifecycle.ClearPID(pidFile)

	fmt.Printf("Starting Brain API server on %s:%d (PID %d)\n", opts.Host, opts.Port, os.Getpid())
	fmt.Printf("Logs: %s\n", logFile)
	fmt.Printf("PID file: %s\n", pidFile)

	// Run server
	return apiserver.RunServer(ctx, opts)
}
