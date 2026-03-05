// Package config provides unified configuration for the Brain system.
package config

import (
	"os"
	"path/filepath"
)

// UnifiedConfig is the merged configuration for all Brain subsystems.
type UnifiedConfig struct {
	Server  ServerConfig  `yaml:"server"`
	Runner  RunnerConfig  `yaml:"runner"`
	MCP     MCPConfig     `yaml:"mcp"`
	Plugins PluginsConfig `yaml:"plugins"`
}

// ServerConfig holds API server configuration.
type ServerConfig struct {
	Port       int    `yaml:"port"`
	Host       string `yaml:"host"`
	BrainDir   string `yaml:"brain_dir"`
	EnableAuth bool   `yaml:"enable_auth"`
	APIKey     string `yaml:"api_key"`
	CORSOrigin string `yaml:"cors_origin"`
	LogLevel   string `yaml:"log_level"`
	TLSCert    string `yaml:"tls_cert"`
	TLSKey     string `yaml:"tls_key"`
}

// RunnerConfig holds task runner configuration.
type RunnerConfig struct {
	MaxParallel            int              `yaml:"max_parallel"`
	PollInterval           int              `yaml:"poll_interval"`
	TaskPollInterval       int              `yaml:"task_poll_interval"`
	WorkDir                string           `yaml:"work_dir"`
	StateDir               string           `yaml:"state_dir"`
	LogDir                 string           `yaml:"log_dir"`
	APITimeout             int              `yaml:"api_timeout"`
	TaskTimeout            int              `yaml:"task_timeout"`
	IdleDetectionThreshold int              `yaml:"idle_detection_threshold"`
	MaxTotalProcesses      int              `yaml:"max_total_processes"`
	MemoryThresholdPercent int              `yaml:"memory_threshold_percent"`
	Opencode               OpencodeSettings `yaml:"opencode"`
	ExcludeProjects        []string         `yaml:"exclude_projects"`
	AutoMonitors           bool             `yaml:"auto_monitors"`
}

// OpencodeSettings holds OpenCode executor settings.
type OpencodeSettings struct {
	Bin   string `yaml:"bin"`
	Agent string `yaml:"agent"`
	Model string `yaml:"model"`
}

// MCPConfig holds MCP integration configuration.
type MCPConfig struct {
	APIURL string `yaml:"api_url"`
}

// PluginsConfig holds plugin paths configuration.
type PluginsConfig struct {
	OpencodePath   string `yaml:"opencode_path"`
	ClaudeCodePath string `yaml:"claude_code_path"`
}

// defaultConfig returns a UnifiedConfig with sensible defaults.
// All paths use standard XDG Base Directory locations where appropriate.
func defaultConfig() UnifiedConfig {
	homeDir, _ := os.UserHomeDir()

	return UnifiedConfig{
		Server: ServerConfig{
			Port:       3333,
			Host:       "localhost",
			BrainDir:   filepath.Join(homeDir, ".brain"),
			LogLevel:   "info",
			EnableAuth: false,
			CORSOrigin: "*",
		},
		Runner: RunnerConfig{
			MaxParallel:            3, // Max concurrent tasks
			PollInterval:           5, // Seconds between task queue polls
			TaskPollInterval:       5, // Seconds between task status polls
			WorkDir:                homeDir,
			StateDir:               filepath.Join(homeDir, ".local", "state", "brain-runner"),
			LogDir:                 filepath.Join(homeDir, ".local", "log"),
			APITimeout:             5000,  // Milliseconds
			TaskTimeout:            0,     // 0 = no timeout
			IdleDetectionThreshold: 60000, // Milliseconds
			MaxTotalProcesses:      10,
			MemoryThresholdPercent: 10,
			Opencode: OpencodeSettings{
				Bin:   "opencode",
				Agent: "",
				Model: "",
			},
			ExcludeProjects: []string{},
			AutoMonitors:    true,
		},
		MCP: MCPConfig{
			APIURL: "http://localhost:3333",
		},
		Plugins: PluginsConfig{
			OpencodePath:   "opencode",
			ClaudeCodePath: "",
		},
	}
}

// getConfigHome returns the XDG config directory, with fallback to ~/.config.
func getConfigHome() string {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, ".config")
	}
	return configHome
}

// getUnifiedConfigPath returns the path to the unified config file.
// Respects XDG_CONFIG_HOME if set, otherwise uses ~/.config/brain/config.yaml.
func getUnifiedConfigPath() string {
	return filepath.Join(getConfigHome(), "brain", "config.yaml")
}

// getLegacyRunnerConfigPath returns the legacy runner config path
// at ~/.config/brain-runner/config.yaml for migration purposes.
func getLegacyRunnerConfigPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".config", "brain-runner", "config.yaml")
}

// fileExists checks if a file or directory exists at the given path.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
