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
//  1. Unified config (~/.config/brain/config.yaml) - highest priority
//  2. Legacy runner config (~/.config/brain-runner/config.yaml) - auto-migrated if found
//  3. Default values - used if no config files exist
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
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// UnifiedConfig is the merged configuration for all Brain subsystems.
type UnifiedConfig struct {
	Server  ServerConfig  `yaml:"server"`
	Runner  RunnerConfig  `yaml:"runner"`
	MCP     MCPConfig     `yaml:"mcp"`
	Plugins PluginsConfig `yaml:"plugins"`
	TUI     TUIConfig     `yaml:"tui"`
}

// ServerConfig holds API server configuration.
// EmbeddingConfig holds embedding service configuration for semantic search.
type EmbeddingConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Provider  string `yaml:"provider"`
	BaseURL   string `yaml:"base_url"`
	APIKeyEnv string `yaml:"api_key_env"`
	Model     string `yaml:"model"`
	Dim       int    `yaml:"dim"`
	BatchSize int    `yaml:"batch_size"`
	TimeoutMs int    `yaml:"timeout_ms"`
}

// AssistantConfig holds server-side LLM configuration for the built-in PWA assistant.
type AssistantConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Provider  string `yaml:"provider"`
	BaseURL   string `yaml:"base_url"`
	APIKeyEnv string `yaml:"api_key_env"`
	Model     string `yaml:"model"`
	TimeoutMs int    `yaml:"timeout_ms"`
}

// AttachmentExtractionConfig holds multimodal model-role configuration for
// deriving searchable text from media attachments. Zero text limits mean unset.
type AttachmentExtractionConfig struct {
	Enabled             bool     `yaml:"enabled"`
	Provider            string   `yaml:"provider"`
	BaseURL             string   `yaml:"base_url"`
	APIKeyEnv           string   `yaml:"api_key_env"`
	Model               string   `yaml:"model"`
	TimeoutMs           int      `yaml:"timeout_ms"`
	MaxSizeBytes        int64    `yaml:"max_size_bytes"`
	SupportedMIMETypes  []string `yaml:"supported_mime_types"`
	MaxDerivedTextChars int      `yaml:"max_derived_text_chars"`
}

// AttachmentConfig holds storage and policy placeholders for first-class
// attachment support. StorageRoot is where attachment binaries will be stored;
// MaxUploadSizeBytes limits incoming uploads; MIME allow/block lists are policy
// placeholders for upload validation.
type AttachmentConfig struct {
	StorageRoot        string   `yaml:"storage_root"`
	MaxUploadSizeBytes int64    `yaml:"max_upload_size_bytes"`
	AllowedMIMETypes   []string `yaml:"allowed_mime_types"`
	BlockedMIMETypes   []string `yaml:"blocked_mime_types"`
}

type ServerConfig struct {
	Port            int                   `yaml:"port"`
	Host            string                `yaml:"host"`
	BrainDir        string                `yaml:"brain_dir"`
	EnableAuth      bool                  `yaml:"enable_auth"`
	CORSOrigin      string                `yaml:"cors_origin"`
	LogLevel        string                `yaml:"log_level"`
	OAuthPIN        string                `yaml:"oauth_pin"`
	JWTSecret       string                `yaml:"jwt_secret"`
	TLSCert         string                `yaml:"tls_cert"`
	TLSKey          string                `yaml:"tls_key"`
	PIDFile         string                `yaml:"pid_file"`
	LogFile         string                `yaml:"log_file"`
	LogMaxSizeMB    int                   `yaml:"log_max_size_mb"` // rotate log_file above this size (default 100)
	LogMaxBackups   int                   `yaml:"log_max_backups"` // rotated backups to keep (default 5)
	TaskDefaults    TaskDefaultsConfig    `yaml:"task_defaults"`
	FeatureCheckout FeatureCheckoutConfig `yaml:"feature_checkout"`
	Embedding       EmbeddingConfig       `yaml:"embedding"`
	Attachments     AttachmentConfig      `yaml:"attachments"`

	AttachmentExtraction AttachmentExtractionConfig `yaml:"attachment_extraction"`
	Assistant            AssistantConfig            `yaml:"assistant"`
}

// FeatureCheckoutConfig controls built-in feature completion checkout automation.
type FeatureCheckoutConfig struct {
	Enabled bool `yaml:"enabled"`
}

// RunnerConfig holds task runner configuration.
type RunnerConfig struct {
	BrainAPIURL            string           `yaml:"brain_api_url"`
	APIToken               string           `yaml:"api_token"`
	APITokenEnv            string           `yaml:"api_token_env"`
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
	IncludeProjects        []string         `yaml:"include_projects"`
	AutoMonitors           bool             `yaml:"auto_monitors"`
}

// OpencodeSettings holds OpenCode executor settings.
type OpencodeSettings struct {
	Bin   string `yaml:"bin"`
	Agent string `yaml:"agent"`
	Model string `yaml:"model"`
}

// TaskDefaultsConfig holds default values for task execution settings.
// These defaults are applied by the API server when resolving tasks.
// Task frontmatter values always take precedence over these defaults.
type TaskDefaultsConfig struct {
	Agent              string   `yaml:"agent"`
	Model              string   `yaml:"model"`
	Executor           string   `yaml:"executor"`
	Extensions         []string `yaml:"extensions"`
	ExecutionMode      string   `yaml:"execution_mode"`
	CompleteOnIdle     *bool    `yaml:"complete_on_idle"`
	MergePolicy        string   `yaml:"merge_policy"`
	MergeStrategy      string   `yaml:"merge_strategy"`
	MergeTargetBranch  string   `yaml:"merge_target_branch"`
	RemoteBranchPolicy string   `yaml:"remote_branch_policy"`
	OpenPRBeforeMerge  *bool    `yaml:"open_pr_before_merge"`
	TargetWorkdir      string   `yaml:"target_workdir"`
}

// TUIConfig holds TUI display and keybinding configuration.
type TUIConfig struct {
	KeyBindings map[string]string `yaml:"keybindings"`
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
// All paths respect XDG Base Directory environment variables:
//   - XDG_STATE_HOME: PID files, runner state (default ~/.local/state)
//   - BRAIN_DIR: brain data directory (default ~/.brain)
func defaultConfig() UnifiedConfig {
	homeDir, _ := os.UserHomeDir()
	stateHome := getStateHome()

	// Brain data directory: BRAIN_DIR env var > ~/.brain
	brainDir := os.Getenv("BRAIN_DIR")
	if brainDir == "" {
		brainDir = filepath.Join(homeDir, ".brain")
	}

	return UnifiedConfig{
		Server: ServerConfig{
			Port:            3333,
			Host:            "localhost",
			BrainDir:        brainDir,
			LogLevel:        "info",
			PIDFile:         filepath.Join(stateHome, "brain-api", "brain-api.pid"),
			LogFile:         filepath.Join(stateHome, "brain-api", "brain-api.log"),
			EnableAuth:      false,
			CORSOrigin:      "*",
			FeatureCheckout: FeatureCheckoutConfig{Enabled: true},
			TaskDefaults: TaskDefaultsConfig{
				ExecutionMode:      "worktree",
				MergePolicy:        "auto_merge",
				MergeStrategy:      "squash",
				MergeTargetBranch:  "main",
				RemoteBranchPolicy: "delete",
			},
			Embedding: EmbeddingConfig{
				Enabled:   false,
				Provider:  "openrouter",
				BaseURL:   "https://openrouter.ai/api/v1",
				APIKeyEnv: "OPENROUTER_API_KEY",
				Model:     "text-embedding-3-small",
				Dim:       1536,
				BatchSize: 32,
				TimeoutMs: 30000,
			},
			Attachments: AttachmentConfig{
				StorageRoot:        filepath.Join(brainDir, "attachments"),
				MaxUploadSizeBytes: 100 * 1024 * 1024,
				AllowedMIMETypes:   []string{},
				BlockedMIMETypes:   []string{},
			},
			AttachmentExtraction: AttachmentExtractionConfig{
				Enabled:             false,
				Provider:            "openrouter",
				BaseURL:             "https://openrouter.ai/api/v1",
				APIKeyEnv:           "OPENROUTER_API_KEY",
				Model:               "google/gemini-2.5-flash",
				TimeoutMs:           60000,
				MaxSizeBytes:        10 * 1024 * 1024,
				SupportedMIMETypes:  []string{"image/*", "application/pdf"},
				MaxDerivedTextChars: 0,
			},
			Assistant: AssistantConfig{
				Enabled:   false,
				Provider:  "openrouter",
				BaseURL:   "https://openrouter.ai/api/v1",
				APIKeyEnv: "OPENROUTER_API_KEY",
				Model:     "anthropic/claude-sonnet-4",
				TimeoutMs: 120000,
			},
		},
		Runner: RunnerConfig{
			BrainAPIURL:            "http://localhost:3333",
			APIToken:               "",
			APITokenEnv:            "BRAIN_API_TOKEN",
			MaxParallel:            3, // Max concurrent tasks
			PollInterval:           5, // Seconds between task queue polls
			TaskPollInterval:       5, // Seconds between task status polls
			WorkDir:                homeDir,
			StateDir:               filepath.Join(stateHome, "brain-runner"),
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
			IncludeProjects: []string{},
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

// DefaultConfig returns a fully-populated default configuration.
func DefaultConfig() UnifiedConfig {
	return defaultConfig()
}

// DefaultConfigYAML returns the default configuration as YAML.
func DefaultConfigYAML() ([]byte, error) {
	cfg := defaultConfig()
	return yaml.Marshal(&cfg)
}

// WriteDefaultConfig writes the default configuration to the unified config path.
// It refuses to overwrite an existing file unless force is true. When force
// replaces an existing file, a timestamped .bak copy is written alongside it
// first and its path is returned as backupPath ("" when nothing existed).
func WriteDefaultConfig(force bool) (path string, backupPath string, err error) {
	path = getUnifiedConfigPath()
	if fileExists(path) && !force {
		return path, "", fmt.Errorf("config file already exists: %s (use --force to overwrite; a timestamped .bak will be saved alongside it)", path)
	}
	cfg := defaultConfig()
	backupPath, err = writeConfig(path, &cfg)
	return path, backupPath, err
}

// BackupConfigFile copies an existing file to a timestamped sibling
// (<path>.bak-YYYYMMDD-HHMMSS) and returns the backup path. It returns ""
// without error when path does not exist. If the backup name is already
// taken (two backups within the same second), a numeric suffix is appended.
// The backup is written with 0600 permissions since configs may hold tokens.
func BackupConfigFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	base := fmt.Sprintf("%s.bak-%s", path, time.Now().Format("20060102-150405"))
	backupPath := base
	for i := 2; fileExists(backupPath); i++ {
		backupPath = fmt.Sprintf("%s.%d", base, i)
	}
	if err := os.WriteFile(backupPath, data, 0600); err != nil {
		return "", err
	}
	return backupPath, nil
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

// getStateHome returns the XDG state directory, with fallback to ~/.local/state.
func getStateHome() string {
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return v
	}
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".local", "state")
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

// writeConfig writes the config to a YAML file, creating parent directories if
// needed. If the destination already exists, a timestamped .bak copy is written
// first so no caller can silently clobber a live config; the backup path is
// returned ("" when the destination did not exist).
func writeConfig(path string, cfg *UnifiedConfig) (string, error) {
	// Create parent directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	// Marshal config to YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}

	backupPath, err := BackupConfigFile(path)
	if err != nil {
		return "", fmt.Errorf("backup existing config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return backupPath, err
	}
	return backupPath, nil
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
	if _, err := writeConfig(unifiedPath, cfg); err != nil {
		return err
	}

	// Create backup of legacy config
	backupPath := legacyPath + ".backup"
	if err := os.Rename(legacyPath, backupPath); err != nil {
		return err
	}

	return nil
}
