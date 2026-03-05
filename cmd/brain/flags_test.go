package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlagParsing(t *testing.T) {
	t.Run("server flags", func(t *testing.T) {
		args := []string{"--port", "3000", "--daemon", "--host", "0.0.0.0"}
		flags, err := ParseServerFlags(args)
		require.NoError(t, err)

		assert.Equal(t, 3000, flags.Port)
		assert.Equal(t, "0.0.0.0", flags.Host)
		assert.True(t, flags.Daemon)
	})

	t.Run("runner flags", func(t *testing.T) {
		args := []string{"--max-parallel", "5", "-i", "prod-*", "-e", "test-*"}
		flags, err := ParseRunnerFlags(args)
		require.NoError(t, err)

		assert.Equal(t, 5, flags.MaxParallel)
		assert.Contains(t, flags.Include, "prod-*")
		assert.Contains(t, flags.Exclude, "test-*")
	})

	t.Run("flag precedence over config", func(t *testing.T) {
		cfg := &UnifiedConfig{}
		cfg.Server.Port = 3333

		serverFlags := &ServerFlags{Port: 4000}
		ApplyFlagsToConfig(cfg, nil, serverFlags)

		assert.Equal(t, 4000, cfg.Server.Port)
	})
}

func TestGlobalFlags(t *testing.T) {
	t.Run("verbose flag", func(t *testing.T) {
		args := []string{"--verbose", "server"}
		flags, remaining := ParseGlobalFlags(args)

		assert.True(t, flags.Verbose)
		assert.Equal(t, []string{"server"}, remaining)
	})

	t.Run("verbose short flag", func(t *testing.T) {
		args := []string{"-v", "server"}
		flags, remaining := ParseGlobalFlags(args)

		assert.True(t, flags.Verbose)
		assert.Equal(t, []string{"server"}, remaining)
	})

	t.Run("help flag", func(t *testing.T) {
		args := []string{"--help"}
		flags, _ := ParseGlobalFlags(args)

		assert.True(t, flags.Help)
	})

	t.Run("version flag", func(t *testing.T) {
		args := []string{"--version"}
		flags, _ := ParseGlobalFlags(args)

		assert.True(t, flags.Version)
	})
}

func TestServerFlags(t *testing.T) {
	t.Run("port flag", func(t *testing.T) {
		args := []string{"--port", "8080"}
		flags, err := ParseServerFlags(args)
		require.NoError(t, err)

		assert.Equal(t, 8080, flags.Port)
	})

	t.Run("port short flag", func(t *testing.T) {
		args := []string{"-p", "8080"}
		flags, err := ParseServerFlags(args)
		require.NoError(t, err)

		assert.Equal(t, 8080, flags.Port)
	})

	t.Run("daemon flag", func(t *testing.T) {
		args := []string{"--daemon"}
		flags, err := ParseServerFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.Daemon)
	})

	t.Run("daemon short flag", func(t *testing.T) {
		args := []string{"-d"}
		flags, err := ParseServerFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.Daemon)
	})

	t.Run("TLS flags", func(t *testing.T) {
		args := []string{"--tls", "--tls-cert", "/path/to/cert", "--tls-key", "/path/to/key"}
		flags, err := ParseServerFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.TLS)
		assert.Equal(t, "/path/to/cert", flags.TLSCert)
		assert.Equal(t, "/path/to/key", flags.TLSKey)
	})

	t.Run("combined flags", func(t *testing.T) {
		args := []string{"--port", "3000", "--daemon", "--host", "0.0.0.0", "--log-file", "/var/log/brain.log"}
		flags, err := ParseServerFlags(args)
		require.NoError(t, err)

		assert.Equal(t, 3000, flags.Port)
		assert.True(t, flags.Daemon)
		assert.Equal(t, "0.0.0.0", flags.Host)
		assert.Equal(t, "/var/log/brain.log", flags.LogFile)
	})
}

func TestRunnerFlags(t *testing.T) {
	t.Run("tui flag", func(t *testing.T) {
		args := []string{"--tui"}
		flags, err := ParseRunnerFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.TUI)
	})

	t.Run("foreground flag", func(t *testing.T) {
		args := []string{"--foreground"}
		flags, err := ParseRunnerFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.Foreground)
	})

	t.Run("foreground short flag", func(t *testing.T) {
		args := []string{"-f"}
		flags, err := ParseRunnerFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.Foreground)
	})

	t.Run("background flag", func(t *testing.T) {
		args := []string{"--background"}
		flags, err := ParseRunnerFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.Background)
	})

	t.Run("max-parallel flag", func(t *testing.T) {
		args := []string{"--max-parallel", "10"}
		flags, err := ParseRunnerFlags(args)
		require.NoError(t, err)

		assert.Equal(t, 10, flags.MaxParallel)
	})

	t.Run("max-parallel short flag", func(t *testing.T) {
		args := []string{"-p", "10"}
		flags, err := ParseRunnerFlags(args)
		require.NoError(t, err)

		assert.Equal(t, 10, flags.MaxParallel)
	})

	t.Run("workdir flag", func(t *testing.T) {
		args := []string{"--workdir", "/path/to/work"}
		flags, err := ParseRunnerFlags(args)
		require.NoError(t, err)

		assert.Equal(t, "/path/to/work", flags.Workdir)
	})

	t.Run("agent and model flags", func(t *testing.T) {
		args := []string{"--agent", "tdd-dev", "--model", "claude-3"}
		flags, err := ParseRunnerFlags(args)
		require.NoError(t, err)

		assert.Equal(t, "tdd-dev", flags.Agent)
		assert.Equal(t, "claude-3", flags.Model)
	})

	t.Run("model short flag", func(t *testing.T) {
		args := []string{"-m", "claude-3"}
		flags, err := ParseRunnerFlags(args)
		require.NoError(t, err)

		assert.Equal(t, "claude-3", flags.Model)
	})

	t.Run("multiple include flags", func(t *testing.T) {
		args := []string{"-i", "prod-*", "--include", "staging-*"}
		flags, err := ParseRunnerFlags(args)
		require.NoError(t, err)

		assert.Len(t, flags.Include, 2)
		assert.Contains(t, flags.Include, "prod-*")
		assert.Contains(t, flags.Include, "staging-*")
	})

	t.Run("multiple exclude flags", func(t *testing.T) {
		args := []string{"-e", "test-*", "--exclude", "dev-*"}
		flags, err := ParseRunnerFlags(args)
		require.NoError(t, err)

		assert.Len(t, flags.Exclude, 2)
		assert.Contains(t, flags.Exclude, "test-*")
		assert.Contains(t, flags.Exclude, "dev-*")
	})

	t.Run("combined runner flags", func(t *testing.T) {
		args := []string{"--max-parallel", "5", "-i", "prod-*", "-e", "test-*", "--tui", "--agent", "tdd-dev"}
		flags, err := ParseRunnerFlags(args)
		require.NoError(t, err)

		assert.Equal(t, 5, flags.MaxParallel)
		assert.Contains(t, flags.Include, "prod-*")
		assert.Contains(t, flags.Exclude, "test-*")
		assert.True(t, flags.TUI)
		assert.Equal(t, "tdd-dev", flags.Agent)
	})

	t.Run("follow flag", func(t *testing.T) {
		args := []string{"--follow"}
		flags, err := ParseRunnerFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.Follow)
	})
}

func TestApplyFlagsToConfig(t *testing.T) {
	t.Run("apply server flags to config", func(t *testing.T) {
		cfg := &UnifiedConfig{}
		cfg.Server.Port = 3000
		cfg.Server.Host = "localhost"

		serverFlags := &ServerFlags{
			Port: 8080,
			Host: "0.0.0.0",
			TLS:  true,
		}

		ApplyFlagsToConfig(cfg, nil, serverFlags)

		assert.Equal(t, 8080, cfg.Server.Port)
		assert.Equal(t, "0.0.0.0", cfg.Server.Host)
		assert.True(t, cfg.Server.TLS.Enabled)
	})

	t.Run("apply runner flags to config", func(t *testing.T) {
		cfg := &UnifiedConfig{}
		cfg.Runner.MaxParallel = 3

		runnerFlags := &RunnerFlags{
			MaxParallel:  10,
			Agent:        "explore",
			Model:        "claude-4",
			Exclude:      []string{"test-*", "dev-*"},
			PollInterval: 5,
		}

		ApplyFlagsToConfig(cfg, nil, runnerFlags)

		assert.Equal(t, 10, cfg.Runner.MaxParallel)
		assert.Equal(t, "explore", cfg.Runner.OpenCode.Agent)
		assert.Equal(t, "claude-4", cfg.Runner.OpenCode.Model)
		assert.Equal(t, 5, cfg.Runner.PollInterval)
		assert.Len(t, cfg.Runner.ExcludeProjects, 2)
		assert.Contains(t, cfg.Runner.ExcludeProjects, "test-*")
	})

	t.Run("flags do not override zero values", func(t *testing.T) {
		cfg := &UnifiedConfig{}
		cfg.Server.Port = 3000

		// Flags with zero values should not override config
		serverFlags := &ServerFlags{
			Port: 0,  // Zero value, should not override
			Host: "", // Empty string, should not override
		}

		ApplyFlagsToConfig(cfg, nil, serverFlags)

		// Port should remain unchanged
		assert.Equal(t, 3000, cfg.Server.Port)
	})

	t.Run("TLS cert and key paths", func(t *testing.T) {
		cfg := &UnifiedConfig{}

		serverFlags := &ServerFlags{
			TLS:     true,
			TLSCert: "/etc/ssl/cert.pem",
			TLSKey:  "/etc/ssl/key.pem",
		}

		ApplyFlagsToConfig(cfg, nil, serverFlags)

		assert.True(t, cfg.Server.TLS.Enabled)
		assert.Equal(t, "/etc/ssl/cert.pem", cfg.Server.TLS.CertPath)
		assert.Equal(t, "/etc/ssl/key.pem", cfg.Server.TLS.KeyPath)
	})
}
