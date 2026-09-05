package runner

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// LoadConfig — defaults
// ---------------------------------------------------------------------------

func TestLoadConfig_Defaults(t *testing.T) {
	// Clear all runner env vars
	envVars := []string{
		"BRAIN_API_URL", "BRAIN_API_TOKEN",
		"RUNNER_POLL_INTERVAL", "RUNNER_TASK_POLL_INTERVAL",
		"RUNNER_MAX_PARALLEL", "RUNNER_MAX_TOTAL_PROCESSES",
		"RUNNER_MEMORY_THRESHOLD", "RUNNER_IDLE_THRESHOLD",
		"RUNNER_STATE_DIR", "RUNNER_LOG_DIR", "RUNNER_WORK_DIR",
		"RUNNER_API_TIMEOUT", "RUNNER_TASK_TIMEOUT",
		"RUNNER_REPO_CACHE_DIR", "RUNNER_GIT_TOKEN", "RUNNER_GIT_TOKEN_ENV",
		"RUNNER_REQUIRE_HTTPS", "RUNNER_ALLOW_UNAUTHENTICATED_HTTPS",
		"OPENCODE_BIN", "OPENCODE_AGENT", "OPENCODE_MODEL",
		"BRAIN_AUTO_MONITORS",
	}
	for _, key := range envVars {
		os.Unsetenv(key)
	}

	// Use empty path so file loading is skipped
	cfg, err := LoadConfigFrom("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.BrainAPIURL != "http://localhost:3333" {
		t.Errorf("BrainAPIURL = %q, want %q", cfg.BrainAPIURL, "http://localhost:3333")
	}
	if cfg.PollInterval != 30 {
		t.Errorf("PollInterval = %d, want 30", cfg.PollInterval)
	}
	if cfg.TaskPollInterval != 5 {
		t.Errorf("TaskPollInterval = %d, want 5", cfg.TaskPollInterval)
	}
	if cfg.MaxParallel != 2 {
		t.Errorf("MaxParallel = %d, want 2", cfg.MaxParallel)
	}
	if cfg.MemoryThresholdPercent != 10 {
		t.Errorf("MemoryThresholdPercent = %d, want 10", cfg.MemoryThresholdPercent)
	}
	if cfg.IdleDetectionThreshold != 60000 {
		t.Errorf("IdleDetectionThreshold = %d, want 60000", cfg.IdleDetectionThreshold)
	}
	if cfg.APITimeout != 5000 {
		t.Errorf("APITimeout = %d, want 5000", cfg.APITimeout)
	}
	if cfg.TaskTimeout != 0 {
		t.Errorf("TaskTimeout = %d, want 0", cfg.TaskTimeout)
	}
	if cfg.Opencode.Bin != "opencode" {
		t.Errorf("Opencode.Bin = %q, want %q", cfg.Opencode.Bin, "opencode")
	}
	if cfg.Opencode.Agent != "" {
		t.Errorf("Opencode.Agent = %q, want empty", cfg.Opencode.Agent)
	}
	if cfg.Opencode.Model != "" {
		t.Errorf("Opencode.Model = %q, want empty", cfg.Opencode.Model)
	}
	if cfg.AutoMonitors {
		t.Error("AutoMonitors should default to false")
	}
	homeDir, _ := os.UserHomeDir()
	if cfg.WorkDir != homeDir {
		t.Errorf("WorkDir = %q, want %q", cfg.WorkDir, homeDir)
	}
	wantRepoCacheDir := filepath.Join(homeDir, ".cache", "brain", "repos")
	if cfg.RepoCacheDir != wantRepoCacheDir {
		t.Errorf("RepoCacheDir = %q, want %q", cfg.RepoCacheDir, wantRepoCacheDir)
	}
	if cfg.GitToken != "" {
		t.Errorf("GitToken = %q, want empty", cfg.GitToken)
	}
	if cfg.GitTokenEnv != "GITHUB_TOKEN" {
		t.Errorf("GitTokenEnv = %q, want %q", cfg.GitTokenEnv, "GITHUB_TOKEN")
	}
	if !cfg.RequireHTTPS {
		t.Error("RequireHTTPS should default to true")
	}
	if cfg.AllowUnauthenticatedHTTPS {
		t.Error("AllowUnauthenticatedHTTPS should default to false")
	}
}

// ---------------------------------------------------------------------------
// LoadConfig — env var overrides
// ---------------------------------------------------------------------------

func TestLoadConfig_EnvOverrides(t *testing.T) {
	t.Setenv("BRAIN_API_URL", "http://brain.local:8080")
	t.Setenv("BRAIN_API_TOKEN", "secret-token")
	t.Setenv("RUNNER_POLL_INTERVAL", "60")
	t.Setenv("RUNNER_TASK_POLL_INTERVAL", "10")
	t.Setenv("RUNNER_MAX_PARALLEL", "5")
	t.Setenv("RUNNER_MAX_TOTAL_PROCESSES", "20")
	t.Setenv("RUNNER_MEMORY_THRESHOLD", "15")
	t.Setenv("RUNNER_IDLE_THRESHOLD", "120000")
	t.Setenv("RUNNER_API_TIMEOUT", "10000")
	t.Setenv("RUNNER_TASK_TIMEOUT", "300000")
	t.Setenv("RUNNER_REPO_CACHE_DIR", "/var/cache/brain/repos")
	t.Setenv("RUNNER_GIT_TOKEN", "env-token")
	t.Setenv("RUNNER_GIT_TOKEN_ENV", "BRAIN_GIT_TOKEN")
	t.Setenv("RUNNER_REQUIRE_HTTPS", "false")
	t.Setenv("RUNNER_ALLOW_UNAUTHENTICATED_HTTPS", "true")
	t.Setenv("OPENCODE_BIN", "/usr/local/bin/opencode")
	t.Setenv("OPENCODE_AGENT", "tdd-dev")
	t.Setenv("OPENCODE_MODEL", "anthropic/claude-sonnet-4-20250514")
	t.Setenv("BRAIN_AUTO_MONITORS", "true")

	cfg, err := LoadConfigFrom("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.BrainAPIURL != "http://brain.local:8080" {
		t.Errorf("BrainAPIURL = %q, want %q", cfg.BrainAPIURL, "http://brain.local:8080")
	}
	if cfg.APIToken != "secret-token" {
		t.Errorf("APIToken = %q, want %q", cfg.APIToken, "secret-token")
	}
	if cfg.PollInterval != 60 {
		t.Errorf("PollInterval = %d, want 60", cfg.PollInterval)
	}
	if cfg.MaxParallel != 5 {
		t.Errorf("MaxParallel = %d, want 5", cfg.MaxParallel)
	}
	if cfg.Opencode.Bin != "/usr/local/bin/opencode" {
		t.Errorf("Opencode.Bin = %q, want %q", cfg.Opencode.Bin, "/usr/local/bin/opencode")
	}
	if cfg.RepoCacheDir != "/var/cache/brain/repos" {
		t.Errorf("RepoCacheDir = %q, want %q", cfg.RepoCacheDir, "/var/cache/brain/repos")
	}
	if cfg.GitToken != "env-token" {
		t.Errorf("GitToken = %q, want env override", cfg.GitToken)
	}
	if cfg.GitTokenEnv != "BRAIN_GIT_TOKEN" {
		t.Errorf("GitTokenEnv = %q, want %q", cfg.GitTokenEnv, "BRAIN_GIT_TOKEN")
	}
	if cfg.RequireHTTPS {
		t.Error("RequireHTTPS should be false from env override")
	}
	if !cfg.AllowUnauthenticatedHTTPS {
		t.Error("AllowUnauthenticatedHTTPS should be true from env override")
	}
	if !cfg.AutoMonitors {
		t.Error("AutoMonitors should be true when BRAIN_AUTO_MONITORS=true")
	}
}

func TestLoadConfig_SchedulerMetadataEnvOverrides(t *testing.T) {
	t.Setenv("RUNNER_LABELS", "pool=fast,region=west")
	t.Setenv("RUNNER_WORKSPACE_ROOTS", "/work/a,/work/b")
	t.Setenv("RUNNER_RESOURCES", "gpu=2,ssd=true,arch=arm64")
	t.Setenv("RUNNER_CAPACITY", "memory_gb=64")
	t.Setenv("RUNNER_DRAINING", "true")

	cfg, err := LoadConfigFrom("")
	if err != nil {
		t.Fatalf("LoadConfigFrom failed: %v", err)
	}

	if cfg.Labels["pool"] != "fast" || cfg.Labels["region"] != "west" {
		t.Fatalf("Labels = %#v, want pool/region", cfg.Labels)
	}
	if !reflect.DeepEqual(cfg.WorkspaceRoots, []string{"/work/a", "/work/b"}) {
		t.Fatalf("WorkspaceRoots = %#v", cfg.WorkspaceRoots)
	}
	if cfg.Resources["gpu"] != 2 || cfg.Resources["ssd"] != true || cfg.Resources["arch"] != "arm64" {
		t.Fatalf("Resources = %#v, want parsed gpu/ssd/arch", cfg.Resources)
	}
	if cfg.Capacity["memory_gb"] != 64 {
		t.Fatalf("Capacity = %#v, want memory_gb=64", cfg.Capacity)
	}
	if !cfg.Draining {
		t.Fatal("Draining = false, want true")
	}
}

// ---------------------------------------------------------------------------
// LoadConfig — YAML file
// ---------------------------------------------------------------------------

func TestLoadConfig_YAMLFile(t *testing.T) {
	for _, key := range []string{
		"BRAIN_API_URL", "BRAIN_API_TOKEN",
		"RUNNER_POLL_INTERVAL", "RUNNER_MAX_PARALLEL",
		"OPENCODE_BIN", "OPENCODE_AGENT", "OPENCODE_MODEL",
	} {
		os.Unsetenv(key)
	}

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	yamlContent := `brain_api_url: "http://brain.local:9999"
api_token: "file-token"
poll_interval: 45
max_parallel: 4
opencode:
  bin: "/opt/opencode"
  agent: "explorer"
  model: "gpt-4"
repo_cache_dir: "/srv/brain/repos"
git_token: "file-token"
git_token_env: "CUSTOM_GIT_TOKEN"
require_https: false
allow_unauthenticated_https: true
exclude_projects:
  - "test-*"
  - "legacy-*"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfigFrom(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.BrainAPIURL != "http://brain.local:9999" {
		t.Errorf("BrainAPIURL = %q, want %q", cfg.BrainAPIURL, "http://brain.local:9999")
	}
	if cfg.PollInterval != 45 {
		t.Errorf("PollInterval = %d, want 45", cfg.PollInterval)
	}
	if cfg.MaxParallel != 4 {
		t.Errorf("MaxParallel = %d, want 4", cfg.MaxParallel)
	}
	if cfg.Opencode.Bin != "/opt/opencode" {
		t.Errorf("Opencode.Bin = %q, want %q", cfg.Opencode.Bin, "/opt/opencode")
	}
	if cfg.RepoCacheDir != "/srv/brain/repos" {
		t.Errorf("RepoCacheDir = %q, want %q", cfg.RepoCacheDir, "/srv/brain/repos")
	}
	if cfg.GitToken != "file-token" {
		t.Errorf("GitToken = %q, want %q", cfg.GitToken, "file-token")
	}
	if cfg.GitTokenEnv != "CUSTOM_GIT_TOKEN" {
		t.Errorf("GitTokenEnv = %q, want %q", cfg.GitTokenEnv, "CUSTOM_GIT_TOKEN")
	}
	if cfg.RequireHTTPS {
		t.Error("RequireHTTPS should be false from YAML")
	}
	if !cfg.AllowUnauthenticatedHTTPS {
		t.Error("AllowUnauthenticatedHTTPS should be true from YAML")
	}
	if len(cfg.ExcludeProjects) != 2 {
		t.Fatalf("ExcludeProjects len = %d, want 2", len(cfg.ExcludeProjects))
	}
}

// ---------------------------------------------------------------------------
// LoadConfig — env overrides file
// ---------------------------------------------------------------------------

func TestLoadConfig_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	yamlContent := `brain_api_url: "http://from-file:1234"
poll_interval: 45
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("BRAIN_API_URL", "http://from-env:5678")

	cfg, err := LoadConfigFrom(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.BrainAPIURL != "http://from-env:5678" {
		t.Errorf("BrainAPIURL = %q, want env override %q", cfg.BrainAPIURL, "http://from-env:5678")
	}
	if cfg.PollInterval != 45 {
		t.Errorf("PollInterval = %d, want 45 from file", cfg.PollInterval)
	}
}

func TestLoadConfig_PassiveDispatchPushEnvOverrides(t *testing.T) {
	t.Setenv("RUNNER_PASSIVE", "true")
	t.Setenv("RUNNER_DISPATCH_PUSH", "true")

	cfg, err := LoadConfigFrom("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.Passive {
		t.Fatal("Passive = false, want true from RUNNER_PASSIVE")
	}
	if !cfg.DispatchPush {
		t.Fatal("DispatchPush = false, want true from RUNNER_DISPATCH_PUSH")
	}
}

func TestLoadConfig_PassiveDispatchPushYAMLFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	yamlContent := `passive: true
dispatch_push: true
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfigFrom(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.Passive {
		t.Fatal("Passive = false, want true from YAML")
	}
	if !cfg.DispatchPush {
		t.Fatal("DispatchPush = false, want true from YAML")
	}
}

// ---------------------------------------------------------------------------
// LoadConfig — tilde expansion
// ---------------------------------------------------------------------------

func TestLoadConfig_TildeExpansion(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	yamlContent := `state_dir: "~/my-state"
log_dir: "~/my-logs"
work_dir: "~"
repo_cache_dir: "~/repo-cache"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	for _, key := range []string{"RUNNER_STATE_DIR", "RUNNER_LOG_DIR", "RUNNER_WORK_DIR", "RUNNER_REPO_CACHE_DIR"} {
		os.Unsetenv(key)
	}

	cfg, err := LoadConfigFrom(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	homeDir, _ := os.UserHomeDir()
	wantState := filepath.Join(homeDir, "my-state")
	if cfg.StateDir != wantState {
		t.Errorf("StateDir = %q, want %q", cfg.StateDir, wantState)
	}
	wantLog := filepath.Join(homeDir, "my-logs")
	if cfg.LogDir != wantLog {
		t.Errorf("LogDir = %q, want %q", cfg.LogDir, wantLog)
	}
	if cfg.WorkDir != homeDir {
		t.Errorf("WorkDir = %q, want %q", cfg.WorkDir, homeDir)
	}
	wantRepoCache := filepath.Join(homeDir, "repo-cache")
	if cfg.RepoCacheDir != wantRepoCache {
		t.Errorf("RepoCacheDir = %q, want %q", cfg.RepoCacheDir, wantRepoCache)
	}
}

func TestLoadConfig_GitTokenEnvFallback(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	yamlContent := `git_token_env: "CUSTOM_GIT_TOKEN"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("CUSTOM_GIT_TOKEN", "fallback-token")
	os.Unsetenv("RUNNER_GIT_TOKEN")
	os.Unsetenv("RUNNER_GIT_TOKEN_ENV")

	cfg, err := LoadConfigFrom(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.GitToken != "fallback-token" {
		t.Errorf("GitToken = %q, want fallback token from CUSTOM_GIT_TOKEN", cfg.GitToken)
	}
	if cfg.GitTokenEnv != "CUSTOM_GIT_TOKEN" {
		t.Errorf("GitTokenEnv = %q, want %q", cfg.GitTokenEnv, "CUSTOM_GIT_TOKEN")
	}
}

func TestLoadConfig_APITokenEnvFallback(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	yamlContent := `api_token_env: "CUSTOM_BRAIN_API_TOKEN"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("CUSTOM_BRAIN_API_TOKEN", "fallback-api-token")
	os.Unsetenv("BRAIN_API_TOKEN")

	cfg, err := LoadConfigFrom(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.APIToken != "fallback-api-token" {
		t.Errorf("APIToken = %q, want fallback token from CUSTOM_BRAIN_API_TOKEN", cfg.APIToken)
	}
	if cfg.APITokenEnv != "CUSTOM_BRAIN_API_TOKEN" {
		t.Errorf("APITokenEnv = %q, want %q", cfg.APITokenEnv, "CUSTOM_BRAIN_API_TOKEN")
	}
}

func TestLoadConfig_ExplicitAPITokenPrecedenceOverTokenEnv(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	yamlContent := `api_token: "explicit-file-token"
api_token_env: "CUSTOM_BRAIN_API_TOKEN"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("CUSTOM_BRAIN_API_TOKEN", "fallback-api-token")
	os.Unsetenv("BRAIN_API_TOKEN")

	cfg, err := LoadConfigFrom(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.APIToken != "explicit-file-token" {
		t.Errorf("APIToken = %q, want explicit file token", cfg.APIToken)
	}
}

func TestLoadConfig_EnvAPITokenPrecedenceOverTokenEnv(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	yamlContent := `api_token: "explicit-file-token"
api_token_env: "CUSTOM_BRAIN_API_TOKEN"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("BRAIN_API_TOKEN", "explicit-env-token")
	t.Setenv("CUSTOM_BRAIN_API_TOKEN", "fallback-api-token")

	cfg, err := LoadConfigFrom(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.APIToken != "explicit-env-token" {
		t.Errorf("APIToken = %q, want explicit env token", cfg.APIToken)
	}
}

func TestLoadConfig_ExplicitGitTokenPrecedenceOverTokenEnv(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	yamlContent := `git_token: "explicit-file-token"
git_token_env: "CUSTOM_GIT_TOKEN"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("CUSTOM_GIT_TOKEN", "fallback-token")
	os.Unsetenv("RUNNER_GIT_TOKEN")
	os.Unsetenv("RUNNER_GIT_TOKEN_ENV")

	cfg, err := LoadConfigFrom(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.GitToken != "explicit-file-token" {
		t.Errorf("GitToken = %q, want explicit file token", cfg.GitToken)
	}
}

func TestLoadConfig_EnvGitTokenPrecedenceOverTokenEnv(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	yamlContent := `git_token: "explicit-file-token"
git_token_env: "CUSTOM_GIT_TOKEN"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("RUNNER_GIT_TOKEN", "explicit-env-token")
	t.Setenv("CUSTOM_GIT_TOKEN", "fallback-token")
	os.Unsetenv("RUNNER_GIT_TOKEN_ENV")

	cfg, err := LoadConfigFrom(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.GitToken != "explicit-env-token" {
		t.Errorf("GitToken = %q, want explicit env token", cfg.GitToken)
	}
}

// ---------------------------------------------------------------------------
// ValidateConfig
// ---------------------------------------------------------------------------

func TestValidateConfig_Valid(t *testing.T) {
	cfg := RunnerConfig{
		PollInterval:           30,
		TaskPollInterval:       5,
		MaxParallel:            2,
		MemoryThresholdPercent: 10,
		APITimeout:             5000,
		TaskTimeout:            0,
		IdleDetectionThreshold: 60000,
		HeartbeatInterval:      30,
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("unexpected error for valid config: %v", err)
	}
}

func TestValidateConfig_InvalidMaxParallel(t *testing.T) {
	tests := []struct {
		name        string
		maxParallel int
	}{
		{"zero", 0},
		{"negative", -1},
		{"too high", 101},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := RunnerConfig{
				PollInterval:      1,
				TaskPollInterval:  1,
				MaxParallel:       tt.maxParallel,
				HeartbeatInterval: 30,
			}
			if err := ValidateConfig(cfg); err == nil {
				t.Error("expected error for invalid maxParallel")
			}
		})
	}
}

func TestValidateConfig_InvalidPollInterval(t *testing.T) {
	cfg := RunnerConfig{
		PollInterval:      0,
		TaskPollInterval:  5,
		MaxParallel:       2,
		HeartbeatInterval: 30,
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for pollInterval < 1")
	}
}

func TestValidateConfig_NegativeTimeout(t *testing.T) {
	cfg := RunnerConfig{
		PollInterval:      1,
		TaskPollInterval:  1,
		MaxParallel:       2,
		APITimeout:        -1,
		HeartbeatInterval: 30,
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for negative apiTimeout")
	}
}

// ---------------------------------------------------------------------------
// Pi config defaults
// ---------------------------------------------------------------------------

func TestLoadConfig_PiDefaults(t *testing.T) {
	// Clear Pi-related env vars
	for _, key := range []string{
		"PI_BIN", "PI_MODEL", "PI_THINKING", "PI_NO_SESSION",
		"DEFAULT_EXECUTOR",
		"BRAIN_API_URL", "BRAIN_API_TOKEN",
		"RUNNER_POLL_INTERVAL", "RUNNER_TASK_POLL_INTERVAL",
		"RUNNER_MAX_PARALLEL", "RUNNER_MAX_TOTAL_PROCESSES",
		"RUNNER_MEMORY_THRESHOLD", "RUNNER_IDLE_THRESHOLD",
		"RUNNER_STATE_DIR", "RUNNER_LOG_DIR", "RUNNER_WORK_DIR",
		"RUNNER_API_TIMEOUT", "RUNNER_TASK_TIMEOUT",
		"OPENCODE_BIN", "OPENCODE_AGENT", "OPENCODE_MODEL",
		"BRAIN_AUTO_MONITORS",
	} {
		os.Unsetenv(key)
	}

	cfg, err := LoadConfigFrom("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Pi defaults
	if cfg.Pi.Bin != "pi" {
		t.Errorf("Pi.Bin = %q, want %q", cfg.Pi.Bin, "pi")
	}
	if cfg.Pi.Model != "" {
		t.Errorf("Pi.Model = %q, want empty", cfg.Pi.Model)
	}
	if cfg.Pi.Thinking != "" {
		t.Errorf("Pi.Thinking = %q, want empty", cfg.Pi.Thinking)
	}
	homeDir, _ := os.UserHomeDir()
	wantAgentsDir := filepath.Join(homeDir, ".pi", "brain-agents")
	if cfg.Pi.AgentsDir != wantAgentsDir {
		t.Errorf("Pi.AgentsDir = %q, want %q", cfg.Pi.AgentsDir, wantAgentsDir)
	}
	wantExtDir := filepath.Join(homeDir, ".pi", "extensions")
	if cfg.Pi.ExtensionsDir != wantExtDir {
		t.Errorf("Pi.ExtensionsDir = %q, want %q", cfg.Pi.ExtensionsDir, wantExtDir)
	}
	if !cfg.Pi.NoSession {
		t.Error("Pi.NoSession should default to true")
	}
	if cfg.DefaultExecutor != "opencode" {
		t.Errorf("DefaultExecutor = %q, want %q", cfg.DefaultExecutor, "opencode")
	}
}

// ---------------------------------------------------------------------------
// Pi config from YAML
// ---------------------------------------------------------------------------

func TestLoadConfig_PiFromYAML(t *testing.T) {
	for _, key := range []string{
		"PI_BIN", "PI_MODEL", "PI_THINKING", "PI_NO_SESSION",
		"DEFAULT_EXECUTOR",
		"BRAIN_API_URL", "RUNNER_MAX_PARALLEL", "RUNNER_POLL_INTERVAL",
		"RUNNER_TASK_POLL_INTERVAL", "RUNNER_MAX_TOTAL_PROCESSES",
		"OPENCODE_BIN",
	} {
		os.Unsetenv(key)
	}

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	yamlContent := `brain_api_url: "http://localhost:3333"
max_parallel: 2
pi:
  bin: "/usr/local/bin/pi"
  model: "anthropic/claude-sonnet-4-20250514"
  thinking: "high"
  agents_dir: "~/my-agents"
  extensions_dir: "~/my-extensions"
  extensions:
    - "ext1"
    - "ext2"
  no_session: false
default_executor: "pi"
task_defaults:
  agent: "tdd-dev"
  model: "anthropic/claude-sonnet-4-20250514"
  executor: "pi"
  execution_mode: "worktree"
  merge_policy: "auto_pr"
  merge_strategy: "squash"
  merge_target_branch: "main"
  remote_branch_policy: "delete"
  target_workdir: "/tmp/work"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfigFrom(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Pi.Bin != "/usr/local/bin/pi" {
		t.Errorf("Pi.Bin = %q, want %q", cfg.Pi.Bin, "/usr/local/bin/pi")
	}
	if cfg.Pi.Model != "anthropic/claude-sonnet-4-20250514" {
		t.Errorf("Pi.Model = %q, want %q", cfg.Pi.Model, "anthropic/claude-sonnet-4-20250514")
	}
	if cfg.Pi.Thinking != "high" {
		t.Errorf("Pi.Thinking = %q, want %q", cfg.Pi.Thinking, "high")
	}
	homeDir, _ := os.UserHomeDir()
	wantAgentsDir := filepath.Join(homeDir, "my-agents")
	if cfg.Pi.AgentsDir != wantAgentsDir {
		t.Errorf("Pi.AgentsDir = %q, want %q", cfg.Pi.AgentsDir, wantAgentsDir)
	}
	wantExtDir := filepath.Join(homeDir, "my-extensions")
	if cfg.Pi.ExtensionsDir != wantExtDir {
		t.Errorf("Pi.ExtensionsDir = %q, want %q", cfg.Pi.ExtensionsDir, wantExtDir)
	}
	if len(cfg.Pi.Extensions) != 2 {
		t.Fatalf("Pi.Extensions len = %d, want 2", len(cfg.Pi.Extensions))
	}
	if cfg.Pi.Extensions[0] != "ext1" || cfg.Pi.Extensions[1] != "ext2" {
		t.Errorf("Pi.Extensions = %v, want [ext1 ext2]", cfg.Pi.Extensions)
	}
	if cfg.Pi.NoSession {
		t.Error("Pi.NoSession should be false from YAML")
	}
	if cfg.DefaultExecutor != "pi" {
		t.Errorf("DefaultExecutor = %q, want %q", cfg.DefaultExecutor, "pi")
	}

	// TaskDefaults
	if cfg.TaskDefaults.Agent != "tdd-dev" {
		t.Errorf("TaskDefaults.Agent = %q, want %q", cfg.TaskDefaults.Agent, "tdd-dev")
	}
	if cfg.TaskDefaults.Executor != "pi" {
		t.Errorf("TaskDefaults.Executor = %q, want %q", cfg.TaskDefaults.Executor, "pi")
	}
	if cfg.TaskDefaults.ExecutionMode != "worktree" {
		t.Errorf("TaskDefaults.ExecutionMode = %q, want %q", cfg.TaskDefaults.ExecutionMode, "worktree")
	}
	if cfg.TaskDefaults.MergePolicy != "auto_pr" {
		t.Errorf("TaskDefaults.MergePolicy = %q, want %q", cfg.TaskDefaults.MergePolicy, "auto_pr")
	}
	if cfg.TaskDefaults.MergeStrategy != "squash" {
		t.Errorf("TaskDefaults.MergeStrategy = %q, want %q", cfg.TaskDefaults.MergeStrategy, "squash")
	}
	if cfg.TaskDefaults.MergeTargetBranch != "main" {
		t.Errorf("TaskDefaults.MergeTargetBranch = %q, want %q", cfg.TaskDefaults.MergeTargetBranch, "main")
	}
	if cfg.TaskDefaults.RemoteBranchPolicy != "delete" {
		t.Errorf("TaskDefaults.RemoteBranchPolicy = %q, want %q", cfg.TaskDefaults.RemoteBranchPolicy, "delete")
	}
	if cfg.TaskDefaults.TargetWorkdir != "/tmp/work" {
		t.Errorf("TaskDefaults.TargetWorkdir = %q, want %q", cfg.TaskDefaults.TargetWorkdir, "/tmp/work")
	}
}

// ---------------------------------------------------------------------------
// Pi env var overrides
// ---------------------------------------------------------------------------

func TestLoadConfig_PiEnvOverrides(t *testing.T) {
	t.Setenv("PI_BIN", "/custom/pi")
	t.Setenv("PI_MODEL", "gpt-4")
	t.Setenv("PI_THINKING", "medium")
	t.Setenv("DEFAULT_EXECUTOR", "pi")

	cfg, err := LoadConfigFrom("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Pi.Bin != "/custom/pi" {
		t.Errorf("Pi.Bin = %q, want %q", cfg.Pi.Bin, "/custom/pi")
	}
	if cfg.Pi.Model != "gpt-4" {
		t.Errorf("Pi.Model = %q, want %q", cfg.Pi.Model, "gpt-4")
	}
	if cfg.Pi.Thinking != "medium" {
		t.Errorf("Pi.Thinking = %q, want %q", cfg.Pi.Thinking, "medium")
	}
	if cfg.DefaultExecutor != "pi" {
		t.Errorf("DefaultExecutor = %q, want %q", cfg.DefaultExecutor, "pi")
	}
}

// ---------------------------------------------------------------------------
// Unified config format (nested under runner: key)
// ---------------------------------------------------------------------------

func TestLoadConfig_UnifiedFormat_WithPi(t *testing.T) {
	for _, key := range []string{
		"PI_BIN", "PI_MODEL", "PI_THINKING", "DEFAULT_EXECUTOR",
		"BRAIN_API_URL", "RUNNER_MAX_PARALLEL", "RUNNER_POLL_INTERVAL",
		"RUNNER_TASK_POLL_INTERVAL", "RUNNER_MAX_TOTAL_PROCESSES",
		"OPENCODE_BIN",
	} {
		os.Unsetenv(key)
	}

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	yamlContent := `runner:
  brain_api_url: "http://unified:3333"
  max_parallel: 3
  pi:
    bin: "/opt/pi"
    model: "claude-4"
    thinking: "low"
  repo_cache_dir: "/srv/unified/repos"
  git_token_env: "UNIFIED_GIT_TOKEN"
  default_executor: "pi"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfigFrom(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.BrainAPIURL != "http://unified:3333" {
		t.Errorf("BrainAPIURL = %q, want %q", cfg.BrainAPIURL, "http://unified:3333")
	}
	if cfg.Pi.Bin != "/opt/pi" {
		t.Errorf("Pi.Bin = %q, want %q", cfg.Pi.Bin, "/opt/pi")
	}
	if cfg.Pi.Model != "claude-4" {
		t.Errorf("Pi.Model = %q, want %q", cfg.Pi.Model, "claude-4")
	}
	if cfg.Pi.Thinking != "low" {
		t.Errorf("Pi.Thinking = %q, want %q", cfg.Pi.Thinking, "low")
	}
	if cfg.DefaultExecutor != "pi" {
		t.Errorf("DefaultExecutor = %q, want %q", cfg.DefaultExecutor, "pi")
	}
	if cfg.RepoCacheDir != "/srv/unified/repos" {
		t.Errorf("RepoCacheDir = %q, want %q", cfg.RepoCacheDir, "/srv/unified/repos")
	}
	if cfg.GitTokenEnv != "UNIFIED_GIT_TOKEN" {
		t.Errorf("GitTokenEnv = %q, want %q", cfg.GitTokenEnv, "UNIFIED_GIT_TOKEN")
	}
}

// ---------------------------------------------------------------------------
// Backward compatibility: existing config without pi/task_defaults
// ---------------------------------------------------------------------------

func TestLoadConfig_BackwardCompatible_NoPiSection(t *testing.T) {
	for _, key := range []string{
		"PI_BIN", "PI_MODEL", "PI_THINKING", "DEFAULT_EXECUTOR",
		"BRAIN_API_URL", "RUNNER_MAX_PARALLEL", "RUNNER_POLL_INTERVAL",
		"RUNNER_TASK_POLL_INTERVAL", "RUNNER_MAX_TOTAL_PROCESSES",
		"OPENCODE_BIN", "OPENCODE_AGENT", "OPENCODE_MODEL",
	} {
		os.Unsetenv(key)
	}

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	yamlContent := `brain_api_url: "http://old-config:3333"
max_parallel: 2
opencode:
  bin: "opencode"
  agent: "tdd-dev"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfigFrom(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should load without error, with defaults for Pi
	if cfg.Pi.Bin != "pi" {
		t.Errorf("Pi.Bin = %q, want default %q", cfg.Pi.Bin, "pi")
	}
	if cfg.DefaultExecutor != "opencode" {
		t.Errorf("DefaultExecutor = %q, want default %q", cfg.DefaultExecutor, "opencode")
	}
	if cfg.TaskDefaults.Agent != "" {
		t.Errorf("TaskDefaults.Agent = %q, want empty", cfg.TaskDefaults.Agent)
	}
}

// ---------------------------------------------------------------------------
// Validation: executor values
// ---------------------------------------------------------------------------

func TestValidateConfig_InvalidExecutor(t *testing.T) {
	tests := []struct {
		name     string
		executor string
	}{
		{"unknown executor", "claude"},
		{"typo", "opecode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validBaseConfig()
			cfg.DefaultExecutor = tt.executor
			if err := ValidateConfig(cfg); err == nil {
				t.Errorf("expected error for invalid executor %q", tt.executor)
			}
		})
	}
}

func TestValidateConfig_ValidExecutor(t *testing.T) {
	for _, executor := range []string{"", "opencode", "pi"} {
		t.Run(executor, func(t *testing.T) {
			cfg := validBaseConfig()
			cfg.DefaultExecutor = executor
			if err := ValidateConfig(cfg); err != nil {
				t.Errorf("unexpected error for executor %q: %v", executor, err)
			}
		})
	}
}

func TestValidateConfig_InvalidThinking(t *testing.T) {
	tests := []struct {
		name     string
		thinking string
	}{
		{"invalid value", "ultra"},
		{"number", "5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validBaseConfig()
			cfg.Pi.Thinking = tt.thinking
			if err := ValidateConfig(cfg); err == nil {
				t.Errorf("expected error for invalid thinking %q", tt.thinking)
			}
		})
	}
}

func TestValidateConfig_ValidThinking(t *testing.T) {
	for _, thinking := range []string{"", "off", "minimal", "low", "medium", "high", "xhigh"} {
		t.Run(thinking, func(t *testing.T) {
			cfg := validBaseConfig()
			cfg.Pi.Thinking = thinking
			if err := ValidateConfig(cfg); err != nil {
				t.Errorf("unexpected error for thinking %q: %v", thinking, err)
			}
		})
	}
}

func TestValidateConfig_InvalidTaskDefaultsExecutor(t *testing.T) {
	cfg := validBaseConfig()
	cfg.TaskDefaults.Executor = "invalid"
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for invalid task_defaults.executor")
	}
}

// validBaseConfig returns a RunnerConfig with all required fields set to valid values.
func validBaseConfig() RunnerConfig {
	return RunnerConfig{
		PollInterval:           30,
		TaskPollInterval:       5,
		MaxParallel:            2,
		MemoryThresholdPercent: 10,
		APITimeout:             5000,
		IdleDetectionThreshold: 60000,
		HeartbeatInterval:      30,
		DefaultExecutor:        "opencode",
	}
}

// =============================================================================
// DispatchPush default behavior
//
// As of the PWA "x" / RunTaskNow change, dispatch_push defaults to true so
// freshly-configured runners are immediately eligible for push-dispatched
// tasks (the scheduler and /run path both require it). Users who want the
// old poll-only behavior can opt out via dispatch_push: false in YAML or
// RUNNER_DISPATCH_PUSH=false in env.
// =============================================================================

func TestLoadConfig_DispatchPushDefaultsTrue(t *testing.T) {
	// No env var, no file — fresh runner should advertise push capability.
	t.Setenv("RUNNER_DISPATCH_PUSH", "")
	cfg, err := LoadConfigFrom("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.DispatchPush {
		t.Fatal("DispatchPush = false, want true (default for fresh runners)")
	}
}

// =============================================================================
// LoadConfigFrom — dispatch_push: false is no longer supported.
//
// Background: push dispatch is now the only fully-functional task delivery
// path (PWA /run, scheduler, manual TUI execute all require it). Poll-only
// runners silently fail in confusing ways (e.g. /run returns "no eligible
// runner"). Reject the misconfig at load time with a pointer to the fix.
// =============================================================================

func TestLoadConfig_DispatchPushFalseInYAMLIsRejected(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("dispatch_push: false\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("RUNNER_DISPATCH_PUSH", "")

	_, err := LoadConfigFrom(configPath)
	if err == nil {
		t.Fatal("LoadConfigFrom should reject dispatch_push: false, got nil error")
	}
	if !strings.Contains(err.Error(), "dispatch_push") {
		t.Errorf("error %q should mention dispatch_push", err)
	}
}

func TestLoadConfig_DispatchPushFalseInEnvIsRejected(t *testing.T) {
	t.Setenv("RUNNER_DISPATCH_PUSH", "false")

	_, err := LoadConfigFrom("")
	if err == nil {
		t.Fatal("LoadConfigFrom should reject RUNNER_DISPATCH_PUSH=false, got nil error")
	}
	if !strings.Contains(err.Error(), "dispatch_push") {
		t.Errorf("error %q should mention dispatch_push", err)
	}
}

func TestLoadConfig_DispatchPushDefaultStillLoads(t *testing.T) {
	// No env var, no file — should succeed with DispatchPush=true.
	t.Setenv("RUNNER_DISPATCH_PUSH", "")
	cfg, err := LoadConfigFrom("")
	if err != nil {
		t.Fatalf("LoadConfigFrom default should succeed: %v", err)
	}
	if !cfg.DispatchPush {
		t.Fatal("DispatchPush = false, want true (default)")
	}
}
