package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlagParsing(t *testing.T) {
	t.Run("api flags", func(t *testing.T) {
		args := []string{"--port", "3000", "--daemon", "--host", "0.0.0.0"}
		flags, err := ParseAPIFlags(args)
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

		apiFlags := &APIFlags{Port: 4000}
		ApplyFlagsToConfig(cfg, nil, apiFlags)

		assert.Equal(t, 4000, cfg.Server.Port)
	})
}

func TestGlobalFlags(t *testing.T) {
	t.Run("verbose flag", func(t *testing.T) {
		args := []string{"--verbose", "api"}
		flags, remaining := ParseGlobalFlags(args)

		assert.True(t, flags.Verbose)
		assert.Equal(t, []string{"api"}, remaining)
	})

	t.Run("verbose short flag", func(t *testing.T) {
		args := []string{"-v", "api"}
		flags, remaining := ParseGlobalFlags(args)

		assert.True(t, flags.Verbose)
		assert.Equal(t, []string{"api"}, remaining)
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

func TestAPIFlags(t *testing.T) {
	t.Run("port flag", func(t *testing.T) {
		args := []string{"--port", "8080"}
		flags, err := ParseAPIFlags(args)
		require.NoError(t, err)

		assert.Equal(t, 8080, flags.Port)
	})

	t.Run("port short flag", func(t *testing.T) {
		args := []string{"-p", "8080"}
		flags, err := ParseAPIFlags(args)
		require.NoError(t, err)

		assert.Equal(t, 8080, flags.Port)
	})

	t.Run("daemon flag", func(t *testing.T) {
		args := []string{"--daemon"}
		flags, err := ParseAPIFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.Daemon)
	})

	t.Run("daemon short flag", func(t *testing.T) {
		args := []string{"-d"}
		flags, err := ParseAPIFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.Daemon)
	})

	t.Run("TLS flags", func(t *testing.T) {
		args := []string{"--tls", "--tls-cert", "/path/to/cert", "--tls-key", "/path/to/key"}
		flags, err := ParseAPIFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.TLS)
		assert.Equal(t, "/path/to/cert", flags.TLSCert)
		assert.Equal(t, "/path/to/key", flags.TLSKey)
	})

	t.Run("combined flags", func(t *testing.T) {
		args := []string{"--port", "3000", "--daemon", "--host", "0.0.0.0", "--log-file", "/var/log/brain.log"}
		flags, err := ParseAPIFlags(args)
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
		args := []string{"--headless"}
		flags, err := ParseRunnerFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.Headless)
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
	t.Run("apply api flags to config", func(t *testing.T) {
		cfg := &UnifiedConfig{}
		cfg.Server.Port = 3000
		cfg.Server.Host = "localhost"

		apiFlags := &APIFlags{
			Port: 8080,
			Host: "0.0.0.0",
			TLS:  true,
		}

		ApplyFlagsToConfig(cfg, nil, apiFlags)

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
		assert.Equal(t, "explore", cfg.Runner.Opencode.Agent)
		assert.Equal(t, "claude-4", cfg.Runner.Opencode.Model)
		assert.Equal(t, 5, cfg.Runner.PollInterval)
		assert.Len(t, cfg.Runner.ExcludeProjects, 2)
		assert.Contains(t, cfg.Runner.ExcludeProjects, "test-*")
	})

	t.Run("flags do not override zero values", func(t *testing.T) {
		cfg := &UnifiedConfig{}
		cfg.Server.Port = 3000

		// Flags with zero values should not override config
		apiFlags := &APIFlags{
			Port: 0,  // Zero value, should not override
			Host: "", // Empty string, should not override
		}

		ApplyFlagsToConfig(cfg, nil, apiFlags)

		// Port should remain unchanged
		assert.Equal(t, 3000, cfg.Server.Port)
	})

	t.Run("TLS cert and key paths", func(t *testing.T) {
		cfg := &UnifiedConfig{}

		apiFlags := &APIFlags{
			TLS:     true,
			TLSCert: "/etc/ssl/cert.pem",
			TLSKey:  "/etc/ssl/key.pem",
		}

		ApplyFlagsToConfig(cfg, nil, apiFlags)

		assert.True(t, cfg.Server.TLS.Enabled)
		assert.Equal(t, "/etc/ssl/cert.pem", cfg.Server.TLS.CertPath)
		assert.Equal(t, "/etc/ssl/key.pem", cfg.Server.TLS.KeyPath)
	})
}

func TestParseDreamFlags(t *testing.T) {
	t.Run("default values", func(t *testing.T) {
		args := []string{}
		flags, err := ParseDreamFlags(args)
		require.NoError(t, err)

		assert.False(t, flags.Enable)
		assert.False(t, flags.Disable)
		assert.False(t, flags.Now)
		assert.Empty(t, flags.Schedule)
	})

	t.Run("now flag", func(t *testing.T) {
		args := []string{"--now"}
		flags, err := ParseDreamFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.Now)
	})

	t.Run("enable flag", func(t *testing.T) {
		args := []string{"--enable"}
		flags, err := ParseDreamFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.Enable)
	})

	t.Run("disable flag", func(t *testing.T) {
		args := []string{"--disable"}
		flags, err := ParseDreamFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.Disable)
	})

	t.Run("schedule flag", func(t *testing.T) {
		args := []string{"--schedule", "0 2 * * *"}
		flags, err := ParseDreamFlags(args)
		require.NoError(t, err)

		assert.Equal(t, "0 2 * * *", flags.Schedule)
	})

	t.Run("all flags combined", func(t *testing.T) {
		args := []string{"--enable", "--now", "--schedule", "daily"}
		flags, err := ParseDreamFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.Enable)
		assert.True(t, flags.Now)
		assert.Equal(t, "daily", flags.Schedule)
	})
}

func TestConvertToCommandsDreamFlags(t *testing.T) {
	t.Run("maps all fields correctly", func(t *testing.T) {
		flags := &DreamFlags{
			Enable:   true,
			Disable:  false,
			Now:      true,
			Schedule: "0 3 * * *",
		}

		result := convertToCommandsDreamFlags(flags)

		assert.True(t, result.Enable)
		assert.False(t, result.Disable)
		assert.True(t, result.Now)
		assert.Equal(t, "0 3 * * *", result.Schedule)
	})
}

func TestParsePluginFlags(t *testing.T) {
	t.Run("default values", func(t *testing.T) {
		args := []string{}
		flags, err := ParsePluginFlags(args)
		require.NoError(t, err)

		assert.False(t, flags.Force)
		assert.False(t, flags.DryRun)
		assert.Empty(t, flags.APIURL)
	})

	t.Run("force flag", func(t *testing.T) {
		args := []string{"--force"}
		flags, err := ParsePluginFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.Force)
	})

	t.Run("force flag short", func(t *testing.T) {
		args := []string{"-f"}
		flags, err := ParsePluginFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.Force)
	})

	t.Run("dry-run flag", func(t *testing.T) {
		args := []string{"--dry-run"}
		flags, err := ParsePluginFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.DryRun)
	})

	t.Run("api-url flag", func(t *testing.T) {
		args := []string{"--api-url", "http://localhost:4000"}
		flags, err := ParsePluginFlags(args)
		require.NoError(t, err)

		assert.Equal(t, "http://localhost:4000", flags.APIURL)
	})

	t.Run("all flags combined", func(t *testing.T) {
		args := []string{"--force", "--dry-run", "--api-url", "http://example.com:3000"}
		flags, err := ParsePluginFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.Force)
		assert.True(t, flags.DryRun)
		assert.Equal(t, "http://example.com:3000", flags.APIURL)
	})
}

func TestParseAutomationGoalFlags(t *testing.T) {
	t.Run("all string flags parsed correctly", func(t *testing.T) {
		args := []string{
			"--project", "brain-api",
			"--feature", "ga-cli",
			"--title", "My Goal",
			"--content", "Some content",
			"--trigger-source", "both",
			"--session-mode", "fresh",
			"--agent", "tdd-dev",
			"--model", "claude-3",
			"--executor", "pi",
			"--workdir", "/tmp/work",
			"--status", "active",
			"--format", "json",
		}
		flags, err := ParseAutomationGoalFlags(args)
		require.NoError(t, err)

		assert.Equal(t, "brain-api", flags.Project)
		assert.Equal(t, "ga-cli", flags.Feature)
		assert.True(t, flags.FeatureSet, "--feature must record that it was passed")
		assert.Equal(t, "My Goal", flags.Title)
		assert.Equal(t, "Some content", flags.Content)
		assert.Equal(t, "both", flags.TriggerSource)
		assert.Equal(t, "fresh", flags.SessionMode)
		assert.Equal(t, "tdd-dev", flags.Agent)
		assert.Equal(t, "claude-3", flags.Model)
		assert.Equal(t, "pi", flags.Executor)
		assert.Equal(t, "/tmp/work", flags.Workdir)
		assert.Equal(t, "active", flags.Status)
		assert.Equal(t, "json", flags.Format)
	})

	t.Run("criteria repeated twice", func(t *testing.T) {
		args := []string{"--criteria", "first", "--criteria", "second"}
		flags, err := ParseAutomationGoalFlags(args)
		require.NoError(t, err)

		assert.Len(t, flags.Criteria, 2)
		assert.Contains(t, flags.Criteria, "first")
		assert.Contains(t, flags.Criteria, "second")
	})

	t.Run("validate repeated", func(t *testing.T) {
		args := []string{"--validate", "check-a", "--validate", "check-b"}
		flags, err := ParseAutomationGoalFlags(args)
		require.NoError(t, err)

		assert.Len(t, flags.Validate, 2)
		assert.Contains(t, flags.Validate, "check-a")
		assert.Contains(t, flags.Validate, "check-b")
	})

	t.Run("limit parsed", func(t *testing.T) {
		args := []string{"--limit", "5"}
		flags, err := ParseAutomationGoalFlags(args)
		require.NoError(t, err)

		assert.Equal(t, 5, flags.Limit)
	})

	t.Run("limit defaults to 20 when absent", func(t *testing.T) {
		args := []string{"--project", "p"}
		flags, err := ParseAutomationGoalFlags(args)
		require.NoError(t, err)

		assert.Equal(t, 20, flags.Limit)
	})

	t.Run("quiet short flag", func(t *testing.T) {
		args := []string{"-q"}
		flags, err := ParseAutomationGoalFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.Quiet)
	})

	t.Run("quiet long flag", func(t *testing.T) {
		args := []string{"--quiet"}
		flags, err := ParseAutomationGoalFlags(args)
		require.NoError(t, err)

		assert.True(t, flags.Quiet)
	})

	t.Run("unknown flags silently ignored", func(t *testing.T) {
		args := []string{"--unknown", "value", "--project", "p"}
		flags, err := ParseAutomationGoalFlags(args)
		require.NoError(t, err)

		assert.Equal(t, "p", flags.Project)
	})

	t.Run("empty args returns defaults", func(t *testing.T) {
		args := []string{}
		flags, err := ParseAutomationGoalFlags(args)
		require.NoError(t, err)

		assert.Equal(t, 20, flags.Limit)
		assert.False(t, flags.Quiet)
		assert.Empty(t, flags.Project)
	})
}

func TestParseLifecycleFlagsWithEmbeddedRunner(t *testing.T) {
	args := []string{
		"--runner",
		"--runner-project", "personal-productivity",
		"--max-parallel", "4",
		"--include", "prod-*",
		"-i", "brain-*",
		"--exclude", "test-*",
		"-e", "legacy-*",
		"--executor", "pi",
	}
	flags, err := ParseLifecycleFlags(args)
	require.NoError(t, err)

	assert.True(t, flags.Runner)
	assert.Equal(t, "personal-productivity", flags.RunnerProject)
	assert.Equal(t, 4, flags.MaxParallel)
	assert.Equal(t, []string{"prod-*", "brain-*"}, flags.Include)
	assert.Equal(t, []string{"test-*", "legacy-*"}, flags.Exclude)
	assert.Equal(t, "pi", flags.Executor)
}

func TestParseAPIFlagsWithEmbeddedRunner(t *testing.T) {
	args := []string{
		"--daemon",
		"--runner",
		"--runner-project", "all",
		"--max-parallel", "6",
		"--include", "prod-*",
		"--exclude", "sandbox-*",
		"--executor", "opencode",
	}
	flags, err := ParseAPIFlags(args)
	require.NoError(t, err)

	assert.True(t, flags.Daemon)
	assert.True(t, flags.Runner)
	assert.Equal(t, "all", flags.RunnerProject)
	assert.Equal(t, 6, flags.MaxParallel)
	assert.Equal(t, []string{"prod-*"}, flags.Include)
	assert.Equal(t, []string{"sandbox-*"}, flags.Exclude)
	assert.Equal(t, "opencode", flags.Executor)
}

func TestParseAutomationGoalFlags_FeaturePresence(t *testing.T) {
	// An omitted --feature and `--feature ""` produce the same Feature value;
	// only FeatureSet separates "leave the scope alone" from "clear it".
	omitted, err := ParseAutomationGoalFlags([]string{"--title", "T"})
	require.NoError(t, err)
	assert.False(t, omitted.FeatureSet)

	cleared, err := ParseAutomationGoalFlags([]string{"--feature", ""})
	require.NoError(t, err)
	assert.True(t, cleared.FeatureSet)
	assert.Equal(t, "", cleared.Feature)
}

func TestParseRunnerFlags_NameAndAll(t *testing.T) {
	flags, err := ParseRunnerFlags([]string{"--name", "worker-a", "--max-parallel", "2"})
	require.NoError(t, err)
	assert.Equal(t, "worker-a", flags.Name)
	assert.False(t, flags.All)
	assert.Equal(t, 2, flags.MaxParallel)

	flags, err = ParseRunnerFlags([]string{"--all"})
	require.NoError(t, err)
	assert.True(t, flags.All)
	assert.Empty(t, flags.Name)

	flags, err = ParseRunnerFlags([]string{})
	require.NoError(t, err)
	assert.Empty(t, flags.Name, "an unnamed runner must stay unnamed so it keeps the default paths")
}

// The converter is the only bridge between the parsed flags and the command
// structs; a field missing here is silently dropped at runtime.
func TestConvertToCommandsRunnerFlags(t *testing.T) {
	flags := &RunnerFlags{
		Name:        "worker-a",
		All:         true,
		MaxParallel: 4,
		Executor:    "pi",
		PiBin:       "/usr/local/bin/pi",
		PiModel:     "anthropic/claude-sonnet-4",
		PiThinking:  "high",
		Include:     []string{"prod-*"},
	}
	got := convertToCommandsRunnerFlags(flags)

	assert.Equal(t, "worker-a", got.Name)
	assert.True(t, got.All)
	assert.Equal(t, 4, got.MaxParallel)
	assert.Equal(t, "pi", got.Executor)
	assert.Equal(t, "/usr/local/bin/pi", got.PiBin)
	assert.Equal(t, "anthropic/claude-sonnet-4", got.PiModel)
	assert.Equal(t, "high", got.PiThinking)
	assert.Equal(t, []string{"prod-*"}, got.Include)
}

func TestParseRunnerFlags_ShortNameAndNew(t *testing.T) {
	flags, err := ParseRunnerFlags([]string{"-n", "worker-a"})
	require.NoError(t, err)
	assert.Equal(t, "worker-a", flags.Name)
	assert.False(t, flags.New)

	flags, err = ParseRunnerFlags([]string{"--new"})
	require.NoError(t, err)
	assert.True(t, flags.New)
	assert.Empty(t, flags.Name)

	// -n is a value flag, so the project pre-scan must not mistake its value
	// for a positional.
	project, flagArgs := splitRunnerProjectArg([]string{"-n", "worker-a"})
	assert.Equal(t, "all", project)
	assert.Equal(t, []string{"-n", "worker-a"}, flagArgs)

	project, flagArgs = splitRunnerProjectArg([]string{"my-project", "-n", "worker-a"})
	assert.Equal(t, "my-project", project)
	assert.Equal(t, []string{"-n", "worker-a"}, flagArgs)
}
