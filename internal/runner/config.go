package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// =============================================================================
// Config File Locations
// =============================================================================

// DefaultConfigPath returns the primary config file path.
func DefaultConfigPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".config", "brain", "config.yaml")
}

// configFiles returns the list of config file paths to check, in priority order.
// Checks ~/.config/brain/ first (preferred), then ~/.config/brain-runner/ (legacy).
func configFiles() []string {
	homeDir, _ := os.UserHomeDir()
	primaryDir := filepath.Join(homeDir, ".config", "brain")
	legacyDir := filepath.Join(homeDir, ".config", "brain-runner")
	return []string{
		filepath.Join(primaryDir, "config.yaml"),
		filepath.Join(primaryDir, "config.yml"),
		filepath.Join(primaryDir, "config.json"),
		filepath.Join(legacyDir, "config.yaml"),
		filepath.Join(legacyDir, "config.yml"),
		filepath.Join(legacyDir, "config.json"),
	}
}

// =============================================================================
// Config Loading
// =============================================================================

// LoadConfig loads runner configuration from the default config file
// (~/.config/brain/config.yaml) with env var overrides, then validates.
// Falls back to legacy path (~/.config/brain-runner/config.yaml) if not found.
func LoadConfig() (RunnerConfig, error) {
	// Try each default config file location
	for _, path := range configFiles() {
		if _, err := os.Stat(path); err == nil {
			return LoadConfigFrom(path)
		}
	}
	// No config file found — use defaults + env vars
	return LoadConfigFrom("")
}

// LoadConfigFrom loads runner configuration from a specific file path
// with env var overrides, then validates. If path is empty, only defaults
// and env vars are used.
func LoadConfigFrom(path string) (RunnerConfig, error) {
	homeDir, _ := os.UserHomeDir()

	// Start with file config (if any)
	var fileCfg RunnerConfig
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return RunnerConfig{}, fmt.Errorf("read config file: %w", err)
		}
		// Try unified config format first (runner fields nested under "runner:" key)
		var wrapper struct {
			Runner RunnerConfig `yaml:"runner"`
		}
		if err := yaml.Unmarshal(data, &wrapper); err == nil && wrapper.Runner.BrainAPIURL != "" || wrapper.Runner.MaxParallel != 0 {
			fileCfg = wrapper.Runner
		} else {
			// Fall back to flat/legacy format (runner fields at top level)
			if err := yaml.Unmarshal(data, &fileCfg); err != nil {
				return RunnerConfig{}, fmt.Errorf("parse config file: %w", err)
			}
		}
	}

	// Expand tilde in file-sourced paths
	fileCfg.StateDir = expandTilde(fileCfg.StateDir, homeDir)
	fileCfg.LogDir = expandTilde(fileCfg.LogDir, homeDir)
	fileCfg.WorkDir = expandTilde(fileCfg.WorkDir, homeDir)

	// Build final config: file defaults → built-in defaults → env overrides
	cfg := RunnerConfig{
		BrainAPIURL:            getEnvOrDefault("BRAIN_API_URL", firstNonEmpty(fileCfg.BrainAPIURL, "http://localhost:3333")),
		APIToken:               getEnvOrDefault("BRAIN_API_TOKEN", fileCfg.APIToken),
		PollInterval:           getEnvIntOrDefault("RUNNER_POLL_INTERVAL", firstNonZero(fileCfg.PollInterval, 30)),
		TaskPollInterval:       getEnvIntOrDefault("RUNNER_TASK_POLL_INTERVAL", firstNonZero(fileCfg.TaskPollInterval, 5)),
		MaxParallel:            getEnvIntOrDefault("RUNNER_MAX_PARALLEL", firstNonZero(fileCfg.MaxParallel, 2)),
		StateDir:               getEnvOrDefault("RUNNER_STATE_DIR", firstNonEmpty(fileCfg.StateDir, filepath.Join(homeDir, ".local", "state", "brain-runner"))),
		LogDir:                 getEnvOrDefault("RUNNER_LOG_DIR", firstNonEmpty(fileCfg.LogDir, filepath.Join(homeDir, ".local", "log"))),
		WorkDir:                getEnvOrDefault("RUNNER_WORK_DIR", firstNonEmpty(fileCfg.WorkDir, homeDir)),
		APITimeout:             getEnvIntOrDefault("RUNNER_API_TIMEOUT", firstNonZero(fileCfg.APITimeout, 5000)),
		TaskTimeout:            getEnvIntOrDefault("RUNNER_TASK_TIMEOUT", fileCfg.TaskTimeout), // 0 is valid default
		IdleDetectionThreshold: getEnvIntOrDefault("RUNNER_IDLE_THRESHOLD", firstNonZero(fileCfg.IdleDetectionThreshold, 60000)),
		MaxTotalProcesses:      getEnvIntOrDefault("RUNNER_MAX_TOTAL_PROCESSES", firstNonZero(fileCfg.MaxTotalProcesses, 10)),
		MemoryThresholdPercent: getEnvIntOrDefault("RUNNER_MEMORY_THRESHOLD", firstNonZero(fileCfg.MemoryThresholdPercent, 10)),
		Opencode: OpencodeConfig{
			Bin:   getEnvOrDefault("OPENCODE_BIN", firstNonEmpty(fileCfg.Opencode.Bin, "opencode")),
			Agent: getEnvOrDefault("OPENCODE_AGENT", fileCfg.Opencode.Agent),
			Model: getEnvOrDefault("OPENCODE_MODEL", fileCfg.Opencode.Model),
		},
		Script: ScriptConfig{
			Enabled:         getEnvBoolOrDefault("RUNNER_SCRIPT_ENABLED", fileCfg.Script.Enabled),
			AllowedCommands: fileCfg.Script.AllowedCommands,
			BlockedCommands: fileCfg.Script.BlockedCommands,
			MaxTimeout:      getEnvIntOrDefault("RUNNER_SCRIPT_MAX_TIMEOUT", firstNonZero(fileCfg.Script.MaxTimeout, 300)),
			WorkdirRestrict: fileCfg.Script.WorkdirRestrict,
		},
		ExcludeProjects: fileCfg.ExcludeProjects,
		AutoMonitors:    getEnvBoolOrDefault("BRAIN_AUTO_MONITORS", fileCfg.AutoMonitors),
		EnvPassthrough:  defaultEnvPassthrough(fileCfg.EnvPassthrough),
		FeatureIDs:      getEnvCSVOrDefault("RUNNER_FEATURE_IDS", fileCfg.FeatureIDs),
		Capabilities:    getEnvCSVOrDefault("RUNNER_CAPABILITIES", fileCfg.Capabilities),
	}

	if err := ValidateConfig(cfg); err != nil {
		return RunnerConfig{}, err
	}

	return cfg, nil
}

// ValidateConfig checks that configuration values are within acceptable ranges.
func ValidateConfig(cfg RunnerConfig) error {
	var errs []string

	if cfg.MaxParallel < 1 || cfg.MaxParallel > 100 {
		errs = append(errs, fmt.Sprintf("maxParallel must be between 1 and 100, got %d", cfg.MaxParallel))
	}
	if cfg.MaxTotalProcesses < 1 || cfg.MaxTotalProcesses > 100 {
		errs = append(errs, fmt.Sprintf("maxTotalProcesses must be between 1 and 100, got %d", cfg.MaxTotalProcesses))
	}
	if cfg.MemoryThresholdPercent < 0 || cfg.MemoryThresholdPercent > 100 {
		errs = append(errs, fmt.Sprintf("memoryThresholdPercent must be between 0 and 100, got %d", cfg.MemoryThresholdPercent))
	}
	if cfg.MaxTotalProcesses < cfg.MaxParallel {
		errs = append(errs, fmt.Sprintf("maxTotalProcesses (%d) must be >= maxParallel (%d)", cfg.MaxTotalProcesses, cfg.MaxParallel))
	}
	if cfg.PollInterval < 1 {
		errs = append(errs, fmt.Sprintf("pollInterval must be >= 1, got %d", cfg.PollInterval))
	}
	if cfg.TaskPollInterval < 1 {
		errs = append(errs, fmt.Sprintf("taskPollInterval must be >= 1, got %d", cfg.TaskPollInterval))
	}
	if cfg.APITimeout < 0 {
		errs = append(errs, fmt.Sprintf("apiTimeout must be >= 0, got %d", cfg.APITimeout))
	}
	if cfg.TaskTimeout < 0 {
		errs = append(errs, fmt.Sprintf("taskTimeout must be >= 0, got %d", cfg.TaskTimeout))
	}
	if cfg.IdleDetectionThreshold < 0 {
		errs = append(errs, fmt.Sprintf("idleDetectionThreshold must be >= 0, got %d", cfg.IdleDetectionThreshold))
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid runner configuration:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// =============================================================================
// Helpers
// =============================================================================

func expandTilde(path, homeDir string) string {
	if path == "~" {
		return homeDir
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir, path[2:])
	}
	return path
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func getEnvIntOrDefault(key string, defaultValue int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultValue
	}
	return n
}

func getEnvBoolOrDefault(key string, defaultValue bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	lower := strings.ToLower(v)
	return lower == "true" || lower == "1"
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func firstNonZero(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

// getEnvCSVOrDefault reads a comma-separated env var and returns the split values.
// Falls back to defaultValue if the env var is empty.
func getEnvCSVOrDefault(key string, defaultValue []string) []string {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	var result []string
	for _, s := range strings.Split(v, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}

// defaultEnvPassthrough returns the env passthrough list, using defaults if empty.
// The defaults ensure BRAIN_API_URL and BRAIN_API_TOKEN are always forwarded.
func defaultEnvPassthrough(configured []string) []string {
	if len(configured) > 0 {
		return configured
	}
	return []string{"BRAIN_API_URL", "BRAIN_API_TOKEN"}
}
