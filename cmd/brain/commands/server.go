package commands

import (
	"context"
	"fmt"

	"github.com/huynle/brain-api/internal/apiserver"
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
		return daemonizeServer(ctx, opts)
	}

	// Otherwise run in foreground
	fmt.Printf("Starting Brain API server on %s:%d\n", opts.Host, opts.Port)
	return apiserver.RunServer(ctx, opts)
}

// daemonizeServer handles daemon mode for the server.
// This will call internal/lifecycle package in Phase 3.
func daemonizeServer(ctx context.Context, opts apiserver.ServerOptions) error {
	return fmt.Errorf("daemon mode not yet implemented (Phase 3)")
}
