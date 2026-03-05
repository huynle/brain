package runner

import (
	"os"
	"path/filepath"
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
	if cfg.MaxTotalProcesses != 10 {
		t.Errorf("MaxTotalProcesses = %d, want 10", cfg.MaxTotalProcesses)
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
	if !cfg.AutoMonitors {
		t.Error("AutoMonitors should be true when BRAIN_AUTO_MONITORS=true")
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

// ---------------------------------------------------------------------------
// LoadConfig — tilde expansion
// ---------------------------------------------------------------------------

func TestLoadConfig_TildeExpansion(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	yamlContent := `state_dir: "~/my-state"
log_dir: "~/my-logs"
work_dir: "~"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	for _, key := range []string{"RUNNER_STATE_DIR", "RUNNER_LOG_DIR", "RUNNER_WORK_DIR"} {
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
}

// ---------------------------------------------------------------------------
// ValidateConfig
// ---------------------------------------------------------------------------

func TestValidateConfig_Valid(t *testing.T) {
	cfg := RunnerConfig{
		PollInterval:           30,
		TaskPollInterval:       5,
		MaxParallel:            2,
		MaxTotalProcesses:      10,
		MemoryThresholdPercent: 10,
		APITimeout:             5000,
		TaskTimeout:            0,
		IdleDetectionThreshold: 60000,
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
				MaxTotalProcesses: 10,
			}
			if err := ValidateConfig(cfg); err == nil {
				t.Error("expected error for invalid maxParallel")
			}
		})
	}
}

func TestValidateConfig_MaxTotalLessThanMaxParallel(t *testing.T) {
	cfg := RunnerConfig{
		PollInterval:      1,
		TaskPollInterval:  1,
		MaxParallel:       5,
		MaxTotalProcesses: 3,
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error when maxTotalProcesses < maxParallel")
	}
}

func TestValidateConfig_InvalidPollInterval(t *testing.T) {
	cfg := RunnerConfig{
		PollInterval:      0,
		TaskPollInterval:  5,
		MaxParallel:       2,
		MaxTotalProcesses: 10,
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
		MaxTotalProcesses: 10,
		APITimeout:        -1,
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected error for negative apiTimeout")
	}
}
