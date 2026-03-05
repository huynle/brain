// Package config provides unified configuration management for the Brain system.
//
// # Unified Config System
//
// This package implements a unified configuration system that consolidates settings
// for all Brain subsystems (Server, Runner, MCP, Plugins) into a single config file
// at ~/.config/brain/config.yaml (respects XDG_CONFIG_HOME).
//
// # Auto-Migration
//
// The system automatically migrates legacy runner configs from the old location
// (~/.config/brain-runner/config.yaml) to the unified config on first LoadConfig()
// call. Migration behavior:
//   - Detects legacy config if unified config doesn't exist
//   - Migrates all legacy runner fields to unified config's Runner section
//   - Creates backup of legacy config at ~/.config/brain-runner/config.yaml.backup
//   - Logs migration events for observability
//   - Subsequent loads use unified config (no re-migration)
//
// # Configuration Precedence
//
// Config loading follows this precedence order:
//   1. Unified config (~/.config/brain/config.yaml) - highest priority
//   2. Legacy runner config (~/.config/brain-runner/config.yaml) - auto-migrated if found
//   3. Default values - used if no config files exist
//
// # Usage Example
//
//	cfg, err := config.LoadConfig()
//	if err != nil {
//	    log.Fatalf("Failed to load config: %v", err)
//	}
//	// Use cfg.Server, cfg.Runner, cfg.MCP, cfg.Plugins
//
// # Migration Fields Mapping
//
// Legacy runner config fields map to unified config as follows:
//   - max_parallel → Runner.MaxParallel
//   - poll_interval → Runner.PollInterval
//   - task_poll_interval → Runner.TaskPollInterval
//   - work_dir → Runner.WorkDir
//   - state_dir → Runner.StateDir
//   - log_dir → Runner.LogDir
//   - api_timeout → Runner.APITimeout
//   - task_timeout → Runner.TaskTimeout
//   - idle_detection_threshold → Runner.IdleDetectionThreshold
//   - max_total_processes → Runner.MaxTotalProcesses
//   - memory_threshold_percent → Runner.MemoryThresholdPercent
//   - auto_monitors → Runner.AutoMonitors
//   - opencode.bin → Runner.Opencode.Bin
//   - opencode.agent → Runner.Opencode.Agent
//   - opencode.model → Runner.Opencode.Model
//   - exclude_projects[] → Runner.ExcludeProjects[]
//
// All other subsystems (Server, MCP, Plugins) use default values if not present in unified config.
package config

import (
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
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
// at ~/.config/brain-runner/config.yaml (or $XDG_CONFIG_HOME/brain-runner/config.yaml).
func getLegacyRunnerConfigPath() string {
	return filepath.Join(getConfigHome(), "brain-runner", "config.yaml")
}

// fileExists checks if a file or directory exists at the given path.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// LoadConfig loads the unified configuration from the standard location.
// Returns default config if no file exists. Returns error if file exists but cannot be parsed.
// Auto-migrates legacy runner config if found.
func LoadConfig() (UnifiedConfig, error) {
	cfg := defaultConfig()

	unifiedPath := getUnifiedConfigPath()

	// If unified config exists, load it and return
	if fileExists(unifiedPath) {
		if err := loadConfigFile(unifiedPath, &cfg); err != nil {
			return UnifiedConfig{}, err
		}
		return cfg, nil
	}

	// Check for legacy runner config
	legacyPath := getLegacyRunnerConfigPath()
	if fileExists(legacyPath) {
		log.Printf("Migrating config from %s to %s", legacyPath, unifiedPath)
		// Migrate legacy config to unified format
		if err := migrateConfig(legacyPath, unifiedPath, &cfg); err != nil {
			return UnifiedConfig{}, err
		}
		log.Printf("Migration complete. Backup saved: %s.backup", legacyPath)
		// Migration successful - config already populated
		return cfg, nil
	}

	return cfg, nil
}

// loadConfigFile reads and parses a YAML config file, merging it with the provided config.
func loadConfigFile(path string, cfg *UnifiedConfig) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	return yaml.Unmarshal(data, cfg)
}

// writeConfig writes the config to a YAML file, creating parent directories if needed.
func writeConfig(path string, cfg *UnifiedConfig) error {
	// Create parent directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Marshal config to YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	// Write to file
	return os.WriteFile(path, data, 0644)
}

// migrateConfig migrates a legacy runner config to the unified config format.
// It reads the legacy config, maps fields to cfg.Runner, writes unified config, and creates backup.
func migrateConfig(legacyPath, unifiedPath string, cfg *UnifiedConfig) error {
	// Read legacy config file
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		return err
	}

	// Parse as generic map to handle legacy format
	var legacyData map[string]interface{}
	if err := yaml.Unmarshal(data, &legacyData); err != nil {
		return err
	}

	// Map legacy fields to unified config Runner section
	if v, ok := legacyData["max_parallel"].(int); ok {
		cfg.Runner.MaxParallel = v
	}
	if v, ok := legacyData["poll_interval"].(int); ok {
		cfg.Runner.PollInterval = v
	}
	if v, ok := legacyData["task_poll_interval"].(int); ok {
		cfg.Runner.TaskPollInterval = v
	}
	if v, ok := legacyData["work_dir"].(string); ok {
		cfg.Runner.WorkDir = v
	}
	if v, ok := legacyData["state_dir"].(string); ok {
		cfg.Runner.StateDir = v
	}
	if v, ok := legacyData["log_dir"].(string); ok {
		cfg.Runner.LogDir = v
	}
	if v, ok := legacyData["api_timeout"].(int); ok {
		cfg.Runner.APITimeout = v
	}
	if v, ok := legacyData["task_timeout"].(int); ok {
		cfg.Runner.TaskTimeout = v
	}
	if v, ok := legacyData["idle_detection_threshold"].(int); ok {
		cfg.Runner.IdleDetectionThreshold = v
	}
	if v, ok := legacyData["max_total_processes"].(int); ok {
		cfg.Runner.MaxTotalProcesses = v
	}
	if v, ok := legacyData["memory_threshold_percent"].(int); ok {
		cfg.Runner.MemoryThresholdPercent = v
	}
	if v, ok := legacyData["auto_monitors"].(bool); ok {
		cfg.Runner.AutoMonitors = v
	}

	// Map nested opencode config
	if opencodeData, ok := legacyData["opencode"].(map[string]interface{}); ok {
		if v, ok := opencodeData["bin"].(string); ok {
			cfg.Runner.Opencode.Bin = v
		}
		if v, ok := opencodeData["agent"].(string); ok {
			cfg.Runner.Opencode.Agent = v
		}
		if v, ok := opencodeData["model"].(string); ok {
			cfg.Runner.Opencode.Model = v
		}
	}

	// Map array fields
	if excludeData, ok := legacyData["exclude_projects"].([]interface{}); ok {
		excludeProjects := make([]string, 0, len(excludeData))
		for _, item := range excludeData {
			if str, ok := item.(string); ok {
				excludeProjects = append(excludeProjects, str)
			}
		}
		cfg.Runner.ExcludeProjects = excludeProjects
	}

	// Write unified config
	if err := writeConfig(unifiedPath, cfg); err != nil {
		return err
	}

	// Create backup of legacy config
	backupPath := legacyPath + ".backup"
	if err := os.Rename(legacyPath, backupPath); err != nil {
		return err
	}

	return nil
}
