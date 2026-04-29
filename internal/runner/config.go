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

// xdgConfigHome returns the XDG config directory, respecting XDG_CONFIG_HOME.
// Falls back to ~/.config if the variable is unset, matching the XDG Base Dir spec.
func xdgConfigHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".config")
}

// xdgStateHome returns the XDG state directory, respecting XDG_STATE_HOME.
// Falls back to ~/.local/state if the variable is unset.
func xdgStateHome() string {
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return v
	}
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".local", "state")
}

// DefaultConfigPath returns the primary config file path.
// Respects XDG_CONFIG_HOME: $XDG_CONFIG_HOME/brain/config.yaml
func DefaultConfigPath() string {
	return filepath.Join(xdgConfigHome(), "brain", "config.yaml")
}

// configFiles returns the list of config file paths to check, in priority order.
// Respects XDG_CONFIG_HOME for both the primary and legacy paths.
func configFiles() []string {
	configHome := xdgConfigHome()
	primaryDir := filepath.Join(configHome, "brain")
	legacyDir := filepath.Join(configHome, "brain-runner")
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
	fileHasRequireHTTPS := false
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return RunnerConfig{}, fmt.Errorf("read config file: %w", err)
		}
		fileHasRequireHTTPS = yamlKeyPresent(data, "require_https") || yamlKeyPresent(data, "runner.require_https")
		// Try unified config format first (runner fields nested under "runner:" key)
		var wrapper struct {
			Runner RunnerConfig `yaml:"runner"`
		}
		if err := yaml.Unmarshal(data, &wrapper); err == nil && yamlKeyPresent(data, "runner") {
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
	fileCfg.RepoCacheDir = expandTilde(fileCfg.RepoCacheDir, homeDir)
	fileCfg.Pi.AgentsDir = expandTilde(fileCfg.Pi.AgentsDir, homeDir)
	fileCfg.Pi.ExtensionsDir = expandTilde(fileCfg.Pi.ExtensionsDir, homeDir)

	// Build final config: file defaults → built-in defaults → env overrides
	// Resolve hooks directory: Hooks.HooksDir > legacy HooksDir > env > default
	defaultHooksDir := filepath.Join(xdgConfigHome(), "brain", "hooks")
	resolvedHooksDir := getEnvOrDefault("RUNNER_HOOKS_DIR",
		firstNonEmpty(fileCfg.Hooks.HooksDir,
			firstNonEmpty(fileCfg.HooksDir, defaultHooksDir)))
	resolvedHooksDir = expandTilde(resolvedHooksDir, homeDir)

	// Resolve hook timeout: legacy HookTimeout > env > default (30s)
	resolvedHookTimeout := getEnvIntOrDefault("RUNNER_HOOK_TIMEOUT",
		firstNonZero(fileCfg.HookTimeout, 30))
	resolvedGitTokenEnv := getEnvOrDefault("RUNNER_GIT_TOKEN_ENV", firstNonEmpty(fileCfg.GitTokenEnv, "GITHUB_TOKEN"))
	resolvedGitToken := getEnvOrDefault("RUNNER_GIT_TOKEN", fileCfg.GitToken)
	if resolvedGitToken == "" && resolvedGitTokenEnv != "" {
		resolvedGitToken = os.Getenv(resolvedGitTokenEnv)
	}
	resolvedRequireHTTPS := getEnvBoolOrDefault("RUNNER_REQUIRE_HTTPS", defaultRequireHTTPS(fileCfg.RequireHTTPS, fileHasRequireHTTPS))

	// Expand tilde in inline hook script paths.
	inlineHooks := fileCfg.Hooks.Hooks
	if inlineHooks != nil {
		for name, h := range inlineHooks {
			h.Script = expandTilde(h.Script, homeDir)
			inlineHooks[name] = h
		}
	}

	cfg := RunnerConfig{
		BrainAPIURL:               getEnvOrDefault("BRAIN_API_URL", firstNonEmpty(fileCfg.BrainAPIURL, "http://localhost:3333")),
		APIToken:                  getEnvOrDefault("BRAIN_API_TOKEN", fileCfg.APIToken),
		PollInterval:              getEnvIntOrDefault("RUNNER_POLL_INTERVAL", firstNonZero(fileCfg.PollInterval, 30)),
		TaskPollInterval:          getEnvIntOrDefault("RUNNER_TASK_POLL_INTERVAL", firstNonZero(fileCfg.TaskPollInterval, 5)),
		MaxParallel:               getEnvIntOrDefault("RUNNER_MAX_PARALLEL", firstNonZero(fileCfg.MaxParallel, 2)),
		StateDir:                  getEnvOrDefault("RUNNER_STATE_DIR", firstNonEmpty(fileCfg.StateDir, filepath.Join(xdgStateHome(), "brain-runner"))),
		LogDir:                    getEnvOrDefault("RUNNER_LOG_DIR", firstNonEmpty(fileCfg.LogDir, filepath.Join(homeDir, ".local", "log"))),
		WorkDir:                   getEnvOrDefault("RUNNER_WORK_DIR", firstNonEmpty(fileCfg.WorkDir, homeDir)),
		RepoCacheDir:              expandTilde(getEnvOrDefault("RUNNER_REPO_CACHE_DIR", firstNonEmpty(fileCfg.RepoCacheDir, filepath.Join(homeDir, ".cache", "brain", "repos"))), homeDir),
		GitToken:                  resolvedGitToken,
		GitTokenEnv:               resolvedGitTokenEnv,
		RequireHTTPS:              resolvedRequireHTTPS,
		AllowUnauthenticatedHTTPS: getEnvBoolOrDefault("RUNNER_ALLOW_UNAUTHENTICATED_HTTPS", fileCfg.AllowUnauthenticatedHTTPS),
		APITimeout:                getEnvIntOrDefault("RUNNER_API_TIMEOUT", firstNonZero(fileCfg.APITimeout, 5000)),
		TaskTimeout:               getEnvIntOrDefault("RUNNER_TASK_TIMEOUT", fileCfg.TaskTimeout), // 0 is valid default
		IdleDetectionThreshold:    getEnvIntOrDefault("RUNNER_IDLE_THRESHOLD", firstNonZero(fileCfg.IdleDetectionThreshold, 60000)),
		MaxTotalProcesses:         getEnvIntOrDefault("RUNNER_MAX_TOTAL_PROCESSES", firstNonZero(fileCfg.MaxTotalProcesses, 10)),
		MemoryThresholdPercent:    getEnvIntOrDefault("RUNNER_MEMORY_THRESHOLD", firstNonZero(fileCfg.MemoryThresholdPercent, 10)),
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
		Pi: PiConfig{
			Bin:           getEnvOrDefault("PI_BIN", firstNonEmpty(fileCfg.Pi.Bin, "pi")),
			Model:         getEnvOrDefault("PI_MODEL", fileCfg.Pi.Model),
			Thinking:      getEnvOrDefault("PI_THINKING", fileCfg.Pi.Thinking),
			AgentsDir:     firstNonEmpty(fileCfg.Pi.AgentsDir, expandTilde("~/.pi/brain-agents", homeDir)),
			ExtensionsDir: firstNonEmpty(fileCfg.Pi.ExtensionsDir, expandTilde("~/.pi/extensions", homeDir)),
			Extensions:    fileCfg.Pi.Extensions,
			NoSession:     getEnvBoolOrDefault("PI_NO_SESSION", piNoSessionDefault(fileCfg)),
		},
		Executors:       defaultExecutors(getEnvCSVOrDefault("RUNNER_EXECUTORS", fileCfg.Executors)),
		DefaultExecutor: getEnvOrDefault("DEFAULT_EXECUTOR", firstNonEmpty(fileCfg.DefaultExecutor, "opencode")),
		TaskDefaults:    fileCfg.TaskDefaults,
		ExcludeProjects: fileCfg.ExcludeProjects,
		IncludeProjects: fileCfg.IncludeProjects,
		AutoMonitors:    getEnvBoolOrDefault("BRAIN_AUTO_MONITORS", fileCfg.AutoMonitors),
		EnvPassthrough:  defaultEnvPassthrough(fileCfg.EnvPassthrough),
		FeatureIDs:      getEnvCSVOrDefault("RUNNER_FEATURE_IDS", fileCfg.FeatureIDs),
		Hooks: HooksConfig{
			HooksDir: resolvedHooksDir,
			Hooks:    inlineHooks,
		},
		// Deprecated fields kept for backward compat; values mirror Hooks.
		HooksDir:          resolvedHooksDir,
		HookTimeout:       resolvedHookTimeout,
		HeartbeatInterval: getEnvIntOrDefault("RUNNER_HEARTBEAT_INTERVAL", firstNonZero(fileCfg.HeartbeatInterval, 30)),
		LogStreaming:      getEnvBoolOrDefault("RUNNER_LOG_STREAMING", defaultLogStreaming(fileCfg.LogStreaming)),
		Capabilities:      getEnvCSVOrDefault("RUNNER_CAPABILITIES", fileCfg.Capabilities),
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
	if cfg.HeartbeatInterval < 1 {
		errs = append(errs, fmt.Sprintf("heartbeatInterval must be >= 1, got %d", cfg.HeartbeatInterval))
	}

	// Validate inline hook configs.
	for name, h := range cfg.Hooks.Hooks {
		if h.Command == "" && h.Script == "" {
			errs = append(errs, fmt.Sprintf("hook %q must define either command or script", name))
		}
		if h.Command != "" && h.Script != "" {
			errs = append(errs, fmt.Sprintf("hook %q defines both command and script; use only one", name))
		}
		if h.Timeout.Duration < 0 {
			errs = append(errs, fmt.Sprintf("hook %q timeout must be >= 0, got %v", name, h.Timeout.Duration))
		}
	}

	// Validate default_executor
	if cfg.DefaultExecutor != "" && cfg.DefaultExecutor != "opencode" && cfg.DefaultExecutor != "pi" {
		errs = append(errs, fmt.Sprintf("default_executor must be \"\", \"opencode\", or \"pi\", got %q", cfg.DefaultExecutor))
	}

	// Validate task_defaults.executor
	if cfg.TaskDefaults.Executor != "" && cfg.TaskDefaults.Executor != "opencode" && cfg.TaskDefaults.Executor != "pi" {
		errs = append(errs, fmt.Sprintf("task_defaults.executor must be \"\", \"opencode\", or \"pi\", got %q", cfg.TaskDefaults.Executor))
	}

	// Validate pi.thinking
	if cfg.Pi.Thinking != "" {
		validThinking := map[string]bool{
			"off": true, "minimal": true, "low": true,
			"medium": true, "high": true, "xhigh": true,
		}
		if !validThinking[cfg.Pi.Thinking] {
			errs = append(errs, fmt.Sprintf("pi.thinking must be one of: off, minimal, low, medium, high, xhigh; got %q", cfg.Pi.Thinking))
		}
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

func yamlKeyPresent(data []byte, dottedKey string) bool {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return false
	}
	if len(root.Content) == 0 {
		return false
	}
	node := root.Content[0]
	for _, part := range strings.Split(dottedKey, ".") {
		if node.Kind != yaml.MappingNode {
			return false
		}
		var next *yaml.Node
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == part {
				next = node.Content[i+1]
				break
			}
		}
		if next == nil {
			return false
		}
		node = next
	}
	return true
}

func defaultRequireHTTPS(configured bool, configuredInFile bool) bool {
	if configuredInFile {
		return configured
	}
	return true
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

// piNoSessionDefault returns the file value for Pi.NoSession if the Pi section
// appears to have been configured (any field set), otherwise returns true (the default).
// This allows `no_session: false` in YAML to take effect while defaulting to true
// when the Pi section is absent.
func piNoSessionDefault(fileCfg RunnerConfig) bool {
	pi := fileCfg.Pi
	if pi.Bin != "" || pi.Model != "" || pi.Thinking != "" || pi.AgentsDir != "" || pi.ExtensionsDir != "" || len(pi.Extensions) > 0 {
		// Pi section was configured in the file; use its NoSession value
		return pi.NoSession
	}
	// No Pi section in file; default to true
	return true
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

// defaultExecutors returns the executor list, defaulting to ["opencode"] if empty.
// This ensures backward compatibility: runners that don't configure executors
// still declare support for the opencode executor.
func defaultExecutors(configured []string) []string {
	if len(configured) > 0 {
		return configured
	}
	return []string{"opencode"}
}

// defaultEnvPassthrough returns the env passthrough list, using defaults if empty.
// The defaults ensure BRAIN_API_URL and BRAIN_API_TOKEN are always forwarded.
func defaultEnvPassthrough(configured []string) []string {
	if len(configured) > 0 {
		return configured
	}
	return []string{"BRAIN_API_URL", "BRAIN_API_TOKEN"}
}

// defaultLogStreaming returns the configured value, defaulting to true.
// The zero value of bool is false, so we need to detect whether the user
// explicitly set it. Since YAML unmarshal sets false for unset bools,
// we always default to true unless the env var is explicitly "false".
func defaultLogStreaming(configured bool) bool {
	// If configured is true, the file explicitly set it. If false, it
	// could be either unset or explicitly false. We default to true
	// and let the env var override.
	if configured {
		return true
	}
	return true // default to enabled
}
