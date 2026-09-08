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

// DefaultStateDir returns the default runner state directory:
// $XDG_STATE_HOME/brain-runner (falling back to ~/.local/state/brain-runner).
// Callers that build a RunnerConfig without going through LoadConfig should use
// this to fill an empty StateDir — otherwise every state, prompt, output-log and
// runner-script path becomes relative and lands in the process working directory.
func DefaultStateDir() string {
	return filepath.Join(xdgStateHome(), "brain-runner")
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
	fileHasDispatchPush := false
	fileHasTaskMemoryLimit := false
	fileHasOpencodeDBMax := false
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return RunnerConfig{}, fmt.Errorf("read config file: %w", err)
		}
		fileHasRequireHTTPS = yamlKeyPresent(data, "require_https") || yamlKeyPresent(data, "runner.require_https")
		fileHasDispatchPush = yamlKeyPresent(data, "dispatch_push") || yamlKeyPresent(data, "runner.dispatch_push")
		// Present-but-zero must mean "disabled" for the memory guards, so an
		// operator can switch one off from config.yaml; firstNonZero would
		// silently turn that 0 back into the default.
		fileHasTaskMemoryLimit = yamlKeyPresent(data, "task_memory_limit_mb") || yamlKeyPresent(data, "runner.task_memory_limit_mb")
		fileHasOpencodeDBMax = yamlKeyPresent(data, "opencode_db_max_gb") || yamlKeyPresent(data, "runner.opencode_db_max_gb")
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
	resolvedAPITokenEnv := firstNonEmpty(fileCfg.APITokenEnv, "BRAIN_API_TOKEN")
	resolvedAPIToken := getEnvOrDefault("BRAIN_API_TOKEN", fileCfg.APIToken)
	if resolvedAPIToken == "" && resolvedAPITokenEnv != "" {
		resolvedAPIToken = os.Getenv(resolvedAPITokenEnv)
	}
	resolvedGitTokenEnv := getEnvOrDefault("RUNNER_GIT_TOKEN_ENV", firstNonEmpty(fileCfg.GitTokenEnv, "GITHUB_TOKEN"))
	resolvedGitToken := getEnvOrDefault("RUNNER_GIT_TOKEN", fileCfg.GitToken)
	if resolvedGitToken == "" && resolvedGitTokenEnv != "" {
		resolvedGitToken = os.Getenv(resolvedGitTokenEnv)
	}
	resolvedRequireHTTPS := getEnvBoolOrDefault("RUNNER_REQUIRE_HTTPS", defaultRequireHTTPS(fileCfg.RequireHTTPS, fileHasRequireHTTPS))

	// Expand tilde in inline hook script paths.
	inlineHooks := fileCfg.Hooks.Hooks
	for name, h := range inlineHooks {
		h.Script = expandTilde(h.Script, homeDir)
		inlineHooks[name] = h
	}

	cfg := RunnerConfig{
		BrainAPIURL:               getEnvOrDefault("BRAIN_API_URL", firstNonEmpty(fileCfg.BrainAPIURL, "http://localhost:3333")),
		APIToken:                  resolvedAPIToken,
		APITokenEnv:               resolvedAPITokenEnv,
		PollInterval:              getEnvIntOrDefault("RUNNER_POLL_INTERVAL", firstNonZero(fileCfg.PollInterval, 30)),
		MaxParallel:               getEnvIntOrDefault("RUNNER_MAX_PARALLEL", firstNonZero(fileCfg.MaxParallel, 2)),
		Name:                      getEnvOrDefault("RUNNER_NAME", fileCfg.Name),
		StateDir:                  getEnvOrDefault("RUNNER_STATE_DIR", firstNonEmpty(fileCfg.StateDir, DefaultStateDir())),
		WorkDir:                   getEnvOrDefault("RUNNER_WORK_DIR", firstNonEmpty(fileCfg.WorkDir, homeDir)),
		RepoCacheDir:              expandTilde(getEnvOrDefault("RUNNER_REPO_CACHE_DIR", firstNonEmpty(fileCfg.RepoCacheDir, filepath.Join(homeDir, ".cache", "brain", "repos"))), homeDir),
		GitToken:                  resolvedGitToken,
		GitTokenEnv:               resolvedGitTokenEnv,
		RequireHTTPS:              resolvedRequireHTTPS,
		AllowUnauthenticatedHTTPS: getEnvBoolOrDefault("RUNNER_ALLOW_UNAUTHENTICATED_HTTPS", fileCfg.AllowUnauthenticatedHTTPS),
		APITimeout:                getEnvIntOrDefault("RUNNER_API_TIMEOUT", firstNonZero(fileCfg.APITimeout, 5000)),
		TaskTimeout:               getEnvIntOrDefault("RUNNER_TASK_TIMEOUT", fileCfg.TaskTimeout), // 0 is valid default
		IdleDetectionThreshold:    getEnvIntOrDefault("RUNNER_IDLE_THRESHOLD", firstNonZero(fileCfg.IdleDetectionThreshold, 60000)),
		MemoryThresholdPercent:    getEnvIntOrDefault("RUNNER_MEMORY_THRESHOLD", firstNonZero(fileCfg.MemoryThresholdPercent, 10)),
		TaskMemoryLimitMB:         getEnvIntOrDefault("RUNNER_TASK_MEMORY_LIMIT_MB", intOrDefault(fileCfg.TaskMemoryLimitMB, fileHasTaskMemoryLimit, DefaultTaskMemoryLimitMB)),
		OpencodeDBMaxGB:           getEnvIntOrDefault("RUNNER_OPENCODE_DB_MAX_GB", intOrDefault(fileCfg.OpencodeDBMaxGB, fileHasOpencodeDBMax, DefaultOpencodeDBMaxGB)),
		MaxTaskAttempts:           getEnvIntOrDefault("RUNNER_MAX_TASK_ATTEMPTS", firstNonZero(fileCfg.MaxTaskAttempts, DefaultMaxTaskAttempts)),
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
		ProjectRefreshInterval: getEnvIntOrDefault(
			"RUNNER_PROJECT_REFRESH_INTERVAL",
			firstNonZero(fileCfg.ProjectRefreshInterval, 60),
		),
		LogStreaming:   getEnvBoolOrDefault("RUNNER_LOG_STREAMING", defaultLogStreaming(fileCfg.LogStreaming)),
		Capabilities:   getEnvCSVOrDefault("RUNNER_CAPABILITIES", fileCfg.Capabilities),
		DispatchPush:   getEnvBoolOrDefault("RUNNER_DISPATCH_PUSH", defaultDispatchPush(fileCfg.DispatchPush, fileHasDispatchPush)),
		Labels:         getEnvStringMapOrDefault("RUNNER_LABELS", fileCfg.Labels),
		WorkspaceRoots: getEnvCSVOrDefault("RUNNER_WORKSPACE_ROOTS", fileCfg.WorkspaceRoots),
		Resources:      getEnvInterfaceMapOrDefault("RUNNER_RESOURCES", fileCfg.Resources),
		Capacity:       getEnvInterfaceMapOrDefault("RUNNER_CAPACITY", fileCfg.Capacity),
		Draining:       getEnvBoolOrDefault("RUNNER_DRAINING", fileCfg.Draining),
		Passive:        getEnvBoolOrDefault("RUNNER_PASSIVE", fileCfg.Passive),
	}

	// Push dispatch is the only fully-supported task delivery mode. The
	// poll-only path (dispatch_push: false) is deprecated: the scheduler,
	// and the /run endpoint (PWA "x" shortcut) all require runners to
	// advertise push capability. Reject the
	// misconfig here with a pointer to the fix rather than failing later
	// with confusing "no eligible runner" errors.
	if !cfg.DispatchPush {
		return RunnerConfig{}, fmt.Errorf("dispatch_push: false is no longer supported; remove the line from your config (default is true) or set RUNNER_DISPATCH_PUSH=true. See: https://opencode.ai/docs (runners section)")
	}

	if err := ValidateConfig(cfg); err != nil {
		return RunnerConfig{}, err
	}

	return cfg, nil
}

// ValidateConfig checks that configuration values are within acceptable ranges.
func ValidateConfig(cfg RunnerConfig) error {
	var errs []string

	if _, err := NormalizeRunnerName(cfg.Name); err != nil {
		errs = append(errs, err.Error())
	}
	if cfg.MaxParallel < 1 || cfg.MaxParallel > 100 {
		errs = append(errs, fmt.Sprintf("maxParallel must be between 1 and 100, got %d", cfg.MaxParallel))
	}
	if cfg.MemoryThresholdPercent < 0 || cfg.MemoryThresholdPercent > 100 {
		errs = append(errs, fmt.Sprintf("memoryThresholdPercent must be between 0 and 100, got %d", cfg.MemoryThresholdPercent))
	}
	if cfg.TaskMemoryLimitMB < 0 {
		errs = append(errs, fmt.Sprintf("taskMemoryLimitMB must be >= 0 (0 disables), got %d", cfg.TaskMemoryLimitMB))
	}
	if cfg.OpencodeDBMaxGB < 0 {
		errs = append(errs, fmt.Sprintf("opencodeDBMaxGB must be >= 0 (0 disables), got %d", cfg.OpencodeDBMaxGB))
	}
	if cfg.PollInterval < 1 {
		errs = append(errs, fmt.Sprintf("pollInterval must be >= 1, got %d", cfg.PollInterval))
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
	if cfg.ProjectRefreshInterval < 0 {
		errs = append(errs, fmt.Sprintf("projectRefreshInterval must be >= 0 (0 disables refresh), got %d", cfg.ProjectRefreshInterval))
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

// defaultDispatchPush returns the dispatch_push setting, defaulting to true
// when not explicitly configured. Push dispatch is required for the
// scheduler and the PWA "x" / RunTaskNow path; defaulting to true means
// fresh runners are immediately eligible without manual env-var nudging.
//
// Users who want poll-only behavior set dispatch_push: false explicitly in
// YAML or RUNNER_DISPATCH_PUSH=false in env.
func defaultDispatchPush(configured bool, configuredInFile bool) bool {
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

// intOrDefault returns the file value when the key was present in the file
// (so an explicit 0 survives), otherwise the default.
func intOrDefault(fileValue int, present bool, def int) int {
	if present {
		return fileValue
	}
	return def
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

func getEnvStringMapOrDefault(key string, defaultValue map[string]string) map[string]string {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	result := make(map[string]string)
	for _, part := range strings.Split(v, ",") {
		k, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || strings.TrimSpace(k) == "" {
			continue
		}
		result[strings.TrimSpace(k)] = strings.TrimSpace(val)
	}
	return result
}

func getEnvInterfaceMapOrDefault(key string, defaultValue map[string]interface{}) map[string]interface{} {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	result := make(map[string]interface{})
	for _, part := range strings.Split(v, ",") {
		k, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || strings.TrimSpace(k) == "" {
			continue
		}
		result[strings.TrimSpace(k)] = parseEnvMapValue(strings.TrimSpace(val))
	}
	return result
}

func parseEnvMapValue(v string) interface{} {
	lower := strings.ToLower(v)
	if lower == "true" {
		return true
	}
	if lower == "false" {
		return false
	}
	if i, err := strconv.Atoi(v); err == nil {
		return i
	}
	return v
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
