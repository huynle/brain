package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()

	// Server defaults
	if cfg.Server.Port != 3333 {
		t.Errorf("Server.Port = %d, want 3333", cfg.Server.Port)
	}
	if cfg.Server.Host != "localhost" {
		t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "localhost")
	}
	homeDir, _ := os.UserHomeDir()
	expectedBrainDir := filepath.Join(homeDir, ".brain")
	if cfg.Server.BrainDir != expectedBrainDir {
		t.Errorf("Server.BrainDir = %q, want %q", cfg.Server.BrainDir, expectedBrainDir)
	}
	if cfg.Server.LogLevel != "info" {
		t.Errorf("Server.LogLevel = %q, want %q", cfg.Server.LogLevel, "info")
	}
	if cfg.Server.EnableAuth != false {
		t.Errorf("Server.EnableAuth = %v, want false", cfg.Server.EnableAuth)
	}

	// Runner defaults
	if cfg.Runner.MaxParallel != 3 {
		t.Errorf("Runner.MaxParallel = %d, want 3", cfg.Runner.MaxParallel)
	}
	if cfg.Runner.PollInterval != 5 {
		t.Errorf("Runner.PollInterval = %d, want 5", cfg.Runner.PollInterval)
	}
	if cfg.Runner.AutoMonitors != true {
		t.Errorf("Runner.AutoMonitors = %v, want true", cfg.Runner.AutoMonitors)
	}

	// MCP defaults
	if cfg.MCP.APIURL != "http://localhost:3333" {
		t.Errorf("MCP.APIURL = %q, want %q", cfg.MCP.APIURL, "http://localhost:3333")
	}

	// Plugins defaults
	if cfg.Plugins.OpencodePath != "opencode" {
		t.Errorf("Plugins.OpencodePath = %q, want %q", cfg.Plugins.OpencodePath, "opencode")
	}
}

func TestGetUnifiedConfigPath(t *testing.T) {
	tests := []struct {
		name    string
		xdgHome string
		want    string
	}{
		{
			name:    "default without XDG_CONFIG_HOME",
			xdgHome: "",
			want: func() string {
				homeDir, _ := os.UserHomeDir()
				return filepath.Join(homeDir, ".config", "brain", "config.yaml")
			}(),
		},
		{
			name:    "with XDG_CONFIG_HOME set",
			xdgHome: "/tmp/custom-config",
			want:    "/tmp/custom-config/brain/config.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.xdgHome != "" {
				t.Setenv("XDG_CONFIG_HOME", tt.xdgHome)
			} else {
				os.Unsetenv("XDG_CONFIG_HOME")
			}

			got := getUnifiedConfigPath()
			if got != tt.want {
				t.Errorf("getUnifiedConfigPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetLegacyRunnerConfigPath(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	want := filepath.Join(homeDir, ".config", "brain-runner", "config.yaml")

	got := getLegacyRunnerConfigPath()
	if got != want {
		t.Errorf("getLegacyRunnerConfigPath() = %q, want %q", got, want)
	}
}

func TestFileExists(t *testing.T) {
	// Create a temporary file
	tmpDir := t.TempDir()
	existingFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(existingFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "existing file",
			path: existingFile,
			want: true,
		},
		{
			name: "non-existent file",
			path: filepath.Join(tmpDir, "nonexistent.txt"),
			want: false,
		},
		{
			name: "directory",
			path: tmpDir,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fileExists(tt.path)
			if got != tt.want {
				t.Errorf("fileExists(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestUnifiedConfigYAMLMarshaling(t *testing.T) {
	cfg := defaultConfig()

	// Marshal to YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	// Unmarshal back
	var decoded UnifiedConfig
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	// Verify key fields survived round-trip
	if decoded.Server.Port != cfg.Server.Port {
		t.Errorf("After round-trip: Server.Port = %d, want %d", decoded.Server.Port, cfg.Server.Port)
	}
	if decoded.Server.Host != cfg.Server.Host {
		t.Errorf("After round-trip: Server.Host = %q, want %q", decoded.Server.Host, cfg.Server.Host)
	}
	if decoded.Runner.MaxParallel != cfg.Runner.MaxParallel {
		t.Errorf("After round-trip: Runner.MaxParallel = %d, want %d", decoded.Runner.MaxParallel, cfg.Runner.MaxParallel)
	}
	if decoded.MCP.APIURL != cfg.MCP.APIURL {
		t.Errorf("After round-trip: MCP.APIURL = %q, want %q", decoded.MCP.APIURL, cfg.MCP.APIURL)
	}
}

// TestLoadConfigNoFile verifies that LoadConfig returns defaults when no config file exists.
func TestLoadConfigNoFile(t *testing.T) {
	// Set XDG_CONFIG_HOME to a non-existent temp directory
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() with no file should not error, got: %v", err)
	}

	// Should return default values
	if cfg.Server.Port != 3333 {
		t.Errorf("LoadConfig() Server.Port = %d, want 3333", cfg.Server.Port)
	}
	if cfg.Server.Host != "localhost" {
		t.Errorf("LoadConfig() Server.Host = %q, want %q", cfg.Server.Host, "localhost")
	}
	if cfg.Runner.MaxParallel != 3 {
		t.Errorf("LoadConfig() Runner.MaxParallel = %d, want 3", cfg.Runner.MaxParallel)
	}
}

// TestLoadConfigWithValidFile verifies that LoadConfig loads from an existing unified config.
func TestLoadConfigWithValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	// Create config directory
	configDir := filepath.Join(tmpDir, "brain")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	// Write a custom config file
	configPath := filepath.Join(configDir, "config.yaml")
	customConfig := `server:
  port: 8080
  host: "0.0.0.0"
  log_level: "debug"
runner:
  max_parallel: 5
  poll_interval: 10
mcp:
  api_url: "http://custom:9999"
`
	if err := os.WriteFile(configPath, []byte(customConfig), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() with valid file error = %v", err)
	}

	// Verify custom values were loaded
	if cfg.Server.Port != 8080 {
		t.Errorf("LoadConfig() Server.Port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("LoadConfig() Server.Host = %q, want %q", cfg.Server.Host, "0.0.0.0")
	}
	if cfg.Server.LogLevel != "debug" {
		t.Errorf("LoadConfig() Server.LogLevel = %q, want %q", cfg.Server.LogLevel, "debug")
	}
	if cfg.Runner.MaxParallel != 5 {
		t.Errorf("LoadConfig() Runner.MaxParallel = %d, want 5", cfg.Runner.MaxParallel)
	}
	if cfg.MCP.APIURL != "http://custom:9999" {
		t.Errorf("LoadConfig() MCP.APIURL = %q, want %q", cfg.MCP.APIURL, "http://custom:9999")
	}
}

// TestLoadConfigWithInvalidYAML verifies that LoadConfig returns an error for invalid YAML.
func TestLoadConfigWithInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	// Create config directory
	configDir := filepath.Join(tmpDir, "brain")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	// Write invalid YAML
	configPath := filepath.Join(configDir, "config.yaml")
	invalidYAML := `server:
  port: not_a_number
  host: [unclosed bracket
`
	if err := os.WriteFile(configPath, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("failed to write invalid config: %v", err)
	}

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("LoadConfig() with invalid YAML should return error, got nil")
	}
}

// TestLoadConfigFileValid verifies loadConfigFile parses valid YAML correctly.
func TestLoadConfigFileValid(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `server:
  port: 4444
  host: "custom-host"
runner:
  max_parallel: 7
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg := defaultConfig()
	err := loadConfigFile(configPath, &cfg)
	if err != nil {
		t.Fatalf("loadConfigFile() error = %v", err)
	}

	if cfg.Server.Port != 4444 {
		t.Errorf("loadConfigFile() Server.Port = %d, want 4444", cfg.Server.Port)
	}
	if cfg.Server.Host != "custom-host" {
		t.Errorf("loadConfigFile() Server.Host = %q, want %q", cfg.Server.Host, "custom-host")
	}
	if cfg.Runner.MaxParallel != 7 {
		t.Errorf("loadConfigFile() Runner.MaxParallel = %d, want 7", cfg.Runner.MaxParallel)
	}
}

// TestLoadConfigFileInvalid verifies loadConfigFile returns error for invalid YAML.
func TestLoadConfigFileInvalid(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	invalidYAML := `{bad yaml structure`
	if err := os.WriteFile(configPath, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("failed to write invalid config: %v", err)
	}

	cfg := defaultConfig()
	err := loadConfigFile(configPath, &cfg)
	if err == nil {
		t.Fatal("loadConfigFile() with invalid YAML should return error, got nil")
	}
}

// TestLoadConfigFileNonExistent verifies loadConfigFile returns error for missing file.
func TestLoadConfigFileNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentPath := filepath.Join(tmpDir, "does-not-exist.yaml")

	cfg := defaultConfig()
	err := loadConfigFile(nonExistentPath, &cfg)
	if err == nil {
		t.Fatal("loadConfigFile() with non-existent file should return error, got nil")
	}
}

// TestWriteConfig verifies writeConfig creates directory and writes valid YAML.
func TestWriteConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "brain", "config.yaml")

	cfg := defaultConfig()
	cfg.Server.Port = 9999
	cfg.Server.Host = "test-host"

	err := writeConfig(configPath, &cfg)
	if err != nil {
		t.Fatalf("writeConfig() error = %v", err)
	}

	// Verify directory was created
	configDir := filepath.Join(tmpDir, "brain")
	if !fileExists(configDir) {
		t.Errorf("writeConfig() did not create directory %q", configDir)
	}

	// Verify file was created
	if !fileExists(configPath) {
		t.Errorf("writeConfig() did not create file %q", configPath)
	}

	// Verify file is readable and contains valid YAML
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read written config: %v", err)
	}

	var loaded UnifiedConfig
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("written config is not valid YAML: %v", err)
	}

	if loaded.Server.Port != 9999 {
		t.Errorf("written config Server.Port = %d, want 9999", loaded.Server.Port)
	}
	if loaded.Server.Host != "test-host" {
		t.Errorf("written config Server.Host = %q, want %q", loaded.Server.Host, "test-host")
	}
}

// TestWriteConfigInvalidPath verifies writeConfig returns error for invalid paths.
func TestWriteConfigInvalidPath(t *testing.T) {
	cfg := defaultConfig()
	invalidPath := "/root/cannot-write-here/config.yaml"

	err := writeConfig(invalidPath, &cfg)
	if err == nil {
		t.Error("writeConfig() with invalid path should return error, got nil")
	}
}

// TestWriteLoadRoundTrip verifies write → load → verify equality.
func TestWriteLoadRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create a custom config
	original := defaultConfig()
	original.Server.Port = 7777
	original.Server.Host = "roundtrip-host"
	original.Server.LogLevel = "trace"
	original.Runner.MaxParallel = 9
	original.Runner.PollInterval = 15
	original.MCP.APIURL = "http://roundtrip:8888"

	// Write it
	if err := writeConfig(configPath, &original); err != nil {
		t.Fatalf("writeConfig() error = %v", err)
	}

	// Load it back
	loaded := defaultConfig()
	if err := loadConfigFile(configPath, &loaded); err != nil {
		t.Fatalf("loadConfigFile() error = %v", err)
	}

	// Verify equality of key fields
	if loaded.Server.Port != original.Server.Port {
		t.Errorf("Round-trip Server.Port = %d, want %d", loaded.Server.Port, original.Server.Port)
	}
	if loaded.Server.Host != original.Server.Host {
		t.Errorf("Round-trip Server.Host = %q, want %q", loaded.Server.Host, original.Server.Host)
	}
	if loaded.Server.LogLevel != original.Server.LogLevel {
		t.Errorf("Round-trip Server.LogLevel = %q, want %q", loaded.Server.LogLevel, original.Server.LogLevel)
	}
	if loaded.Runner.MaxParallel != original.Runner.MaxParallel {
		t.Errorf("Round-trip Runner.MaxParallel = %d, want %d", loaded.Runner.MaxParallel, original.Runner.MaxParallel)
	}
	if loaded.Runner.PollInterval != original.Runner.PollInterval {
		t.Errorf("Round-trip Runner.PollInterval = %d, want %d", loaded.Runner.PollInterval, original.Runner.PollInterval)
	}
	if loaded.MCP.APIURL != original.MCP.APIURL {
		t.Errorf("Round-trip MCP.APIURL = %q, want %q", loaded.MCP.APIURL, original.MCP.APIURL)
	}
}

// =============================================================================
// Migration Tests
// =============================================================================

// TestMigrateConfigBasicFields verifies migration of basic scalar fields from legacy config.
func TestMigrateConfigBasicFields(t *testing.T) {
	tmpDir := t.TempDir()
	legacyPath := filepath.Join(tmpDir, "legacy-config.yaml")
	unifiedPath := filepath.Join(tmpDir, "unified-config.yaml")

	// Create a legacy runner config with basic fields
	legacyYAML := `max_parallel: 5
poll_interval: 10
task_poll_interval: 3
work_dir: "/home/user/work"
state_dir: "/home/user/.state"
log_dir: "/home/user/.logs"
api_timeout: 3000
task_timeout: 120000
idle_detection_threshold: 30000
max_total_processes: 8
memory_threshold_percent: 15
auto_monitors: false
`
	if err := os.WriteFile(legacyPath, []byte(legacyYAML), 0644); err != nil {
		t.Fatalf("failed to create legacy config: %v", err)
	}

	cfg := defaultConfig()
	err := migrateConfig(legacyPath, unifiedPath, &cfg)
	if err != nil {
		t.Fatalf("migrateConfig() error = %v", err)
	}

	// Verify basic fields were migrated
	if cfg.Runner.MaxParallel != 5 {
		t.Errorf("Runner.MaxParallel = %d, want 5", cfg.Runner.MaxParallel)
	}
	if cfg.Runner.PollInterval != 10 {
		t.Errorf("Runner.PollInterval = %d, want 10", cfg.Runner.PollInterval)
	}
	if cfg.Runner.TaskPollInterval != 3 {
		t.Errorf("Runner.TaskPollInterval = %d, want 3", cfg.Runner.TaskPollInterval)
	}
	if cfg.Runner.WorkDir != "/home/user/work" {
		t.Errorf("Runner.WorkDir = %q, want %q", cfg.Runner.WorkDir, "/home/user/work")
	}
	if cfg.Runner.StateDir != "/home/user/.state" {
		t.Errorf("Runner.StateDir = %q, want %q", cfg.Runner.StateDir, "/home/user/.state")
	}
	if cfg.Runner.LogDir != "/home/user/.logs" {
		t.Errorf("Runner.LogDir = %q, want %q", cfg.Runner.LogDir, "/home/user/.logs")
	}
	if cfg.Runner.APITimeout != 3000 {
		t.Errorf("Runner.APITimeout = %d, want 3000", cfg.Runner.APITimeout)
	}
	if cfg.Runner.TaskTimeout != 120000 {
		t.Errorf("Runner.TaskTimeout = %d, want 120000", cfg.Runner.TaskTimeout)
	}
	if cfg.Runner.IdleDetectionThreshold != 30000 {
		t.Errorf("Runner.IdleDetectionThreshold = %d, want 30000", cfg.Runner.IdleDetectionThreshold)
	}
	if cfg.Runner.MaxTotalProcesses != 8 {
		t.Errorf("Runner.MaxTotalProcesses = %d, want 8", cfg.Runner.MaxTotalProcesses)
	}
	if cfg.Runner.MemoryThresholdPercent != 15 {
		t.Errorf("Runner.MemoryThresholdPercent = %d, want 15", cfg.Runner.MemoryThresholdPercent)
	}
	if cfg.Runner.AutoMonitors != false {
		t.Errorf("Runner.AutoMonitors = %v, want false", cfg.Runner.AutoMonitors)
	}

	// Verify unified config file was written
	if !fileExists(unifiedPath) {
		t.Errorf("migrateConfig() did not create unified config at %q", unifiedPath)
	}

	// Verify backup was created
	backupPath := legacyPath + ".backup"
	if !fileExists(backupPath) {
		t.Errorf("migrateConfig() did not create backup at %q", backupPath)
	}
}

// TestMigrateConfigComplexFields verifies migration of nested and array fields.
func TestMigrateConfigComplexFields(t *testing.T) {
	tmpDir := t.TempDir()
	legacyPath := filepath.Join(tmpDir, "legacy-config.yaml")
	unifiedPath := filepath.Join(tmpDir, "unified-config.yaml")

	// Create legacy config with nested opencode config and array fields
	legacyYAML := `max_parallel: 2
poll_interval: 5
opencode:
  bin: "/usr/local/bin/opencode"
  agent: "dev"
  model: "claude-sonnet-4"
exclude_projects:
  - "test-project"
  - "legacy-project"
  - "archived-*"
`
	if err := os.WriteFile(legacyPath, []byte(legacyYAML), 0644); err != nil {
		t.Fatalf("failed to create legacy config: %v", err)
	}

	cfg := defaultConfig()
	err := migrateConfig(legacyPath, unifiedPath, &cfg)
	if err != nil {
		t.Fatalf("migrateConfig() error = %v", err)
	}

	// Verify nested opencode config
	if cfg.Runner.Opencode.Bin != "/usr/local/bin/opencode" {
		t.Errorf("Runner.Opencode.Bin = %q, want %q", cfg.Runner.Opencode.Bin, "/usr/local/bin/opencode")
	}
	if cfg.Runner.Opencode.Agent != "dev" {
		t.Errorf("Runner.Opencode.Agent = %q, want %q", cfg.Runner.Opencode.Agent, "dev")
	}
	if cfg.Runner.Opencode.Model != "claude-sonnet-4" {
		t.Errorf("Runner.Opencode.Model = %q, want %q", cfg.Runner.Opencode.Model, "claude-sonnet-4")
	}

	// Verify array fields
	if len(cfg.Runner.ExcludeProjects) != 3 {
		t.Fatalf("Runner.ExcludeProjects length = %d, want 3", len(cfg.Runner.ExcludeProjects))
	}
	if cfg.Runner.ExcludeProjects[0] != "test-project" {
		t.Errorf("Runner.ExcludeProjects[0] = %q, want %q", cfg.Runner.ExcludeProjects[0], "test-project")
	}
	if cfg.Runner.ExcludeProjects[1] != "legacy-project" {
		t.Errorf("Runner.ExcludeProjects[1] = %q, want %q", cfg.Runner.ExcludeProjects[1], "legacy-project")
	}
	if cfg.Runner.ExcludeProjects[2] != "archived-*" {
		t.Errorf("Runner.ExcludeProjects[2] = %q, want %q", cfg.Runner.ExcludeProjects[2], "archived-*")
	}
}

// TestMigrateConfigInvalidYAML verifies migration handles invalid YAML gracefully.
func TestMigrateConfigInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	legacyPath := filepath.Join(tmpDir, "legacy-config.yaml")
	unifiedPath := filepath.Join(tmpDir, "unified-config.yaml")

	// Create invalid YAML
	invalidYAML := `max_parallel: not_a_number
poll_interval: [unclosed
`
	if err := os.WriteFile(legacyPath, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("failed to create invalid config: %v", err)
	}

	cfg := defaultConfig()
	err := migrateConfig(legacyPath, unifiedPath, &cfg)
	if err == nil {
		t.Fatal("migrateConfig() with invalid YAML should return error, got nil")
	}

	// Verify unified config was NOT created (migration failed)
	if fileExists(unifiedPath) {
		t.Errorf("migrateConfig() created unified config despite error")
	}

	// Verify backup was NOT created (migration failed)
	backupPath := legacyPath + ".backup"
	if fileExists(backupPath) {
		t.Errorf("migrateConfig() created backup despite error")
	}

	// Verify original file still exists
	if !fileExists(legacyPath) {
		t.Errorf("migrateConfig() removed legacy file despite error")
	}
}

// TestLoadConfigTriggersAutoMigration verifies LoadConfig auto-migrates legacy config.
func TestLoadConfigTriggersAutoMigration(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	// Create legacy config directory and file
	legacyDir := filepath.Join(tmpDir, "brain-runner")
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatalf("failed to create legacy dir: %v", err)
	}
	legacyPath := filepath.Join(legacyDir, "config.yaml")
	legacyYAML := `max_parallel: 7
poll_interval: 15
`
	if err := os.WriteFile(legacyPath, []byte(legacyYAML), 0644); err != nil {
		t.Fatalf("failed to create legacy config: %v", err)
	}

	// LoadConfig should detect and migrate
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() with legacy config error = %v", err)
	}

	// Verify migrated values
	if cfg.Runner.MaxParallel != 7 {
		t.Errorf("LoadConfig() Runner.MaxParallel = %d, want 7", cfg.Runner.MaxParallel)
	}
	if cfg.Runner.PollInterval != 15 {
		t.Errorf("LoadConfig() Runner.PollInterval = %d, want 15", cfg.Runner.PollInterval)
	}

	// Verify unified config was created
	unifiedPath := filepath.Join(tmpDir, "brain", "config.yaml")
	if !fileExists(unifiedPath) {
		t.Errorf("LoadConfig() did not create unified config at %q", unifiedPath)
	}

	// Verify backup was created
	backupPath := legacyPath + ".backup"
	if !fileExists(backupPath) {
		t.Errorf("LoadConfig() did not create backup at %q", backupPath)
	}

	// Verify original legacy config no longer exists
	if fileExists(legacyPath) {
		t.Errorf("LoadConfig() did not rename legacy config")
	}
}

// TestLoadConfigPreferUnified verifies unified config takes precedence over legacy.
func TestLoadConfigPreferUnified(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	// Create both legacy and unified configs with different values
	legacyDir := filepath.Join(tmpDir, "brain-runner")
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatalf("failed to create legacy dir: %v", err)
	}
	legacyPath := filepath.Join(legacyDir, "config.yaml")
	legacyYAML := `max_parallel: 99
poll_interval: 99
`
	if err := os.WriteFile(legacyPath, []byte(legacyYAML), 0644); err != nil {
		t.Fatalf("failed to create legacy config: %v", err)
	}

	unifiedDir := filepath.Join(tmpDir, "brain")
	if err := os.MkdirAll(unifiedDir, 0755); err != nil {
		t.Fatalf("failed to create unified dir: %v", err)
	}
	unifiedPath := filepath.Join(unifiedDir, "config.yaml")
	unifiedYAML := `server:
  port: 8080
runner:
  max_parallel: 4
  poll_interval: 8
`
	if err := os.WriteFile(unifiedPath, []byte(unifiedYAML), 0644); err != nil {
		t.Fatalf("failed to create unified config: %v", err)
	}

	// LoadConfig should use unified, not migrate legacy
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	// Verify unified values, not legacy
	if cfg.Runner.MaxParallel != 4 {
		t.Errorf("LoadConfig() Runner.MaxParallel = %d, want 4 (from unified, not 99 from legacy)", cfg.Runner.MaxParallel)
	}
	if cfg.Runner.PollInterval != 8 {
		t.Errorf("LoadConfig() Runner.PollInterval = %d, want 8 (from unified, not 99 from legacy)", cfg.Runner.PollInterval)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("LoadConfig() Server.Port = %d, want 8080", cfg.Server.Port)
	}

	// Verify legacy config still exists (no migration should occur)
	if !fileExists(legacyPath) {
		t.Errorf("LoadConfig() removed legacy config when unified exists")
	}

	// Verify no backup was created
	backupPath := legacyPath + ".backup"
	if fileExists(backupPath) {
		t.Errorf("LoadConfig() created backup when unified config exists")
	}
}

// =============================================================================
// End-to-End Integration Tests
// =============================================================================

// TestUnifiedConfigIntegration performs comprehensive end-to-end integration testing
// of the unified config system, validating the complete lifecycle from no config
// through migration to normal operation.
func TestUnifiedConfigIntegration(t *testing.T) {
	// Create isolated temp home directory for clean test environment
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	unifiedPath := filepath.Join(tmpDir, "brain", "config.yaml")
	legacyPath := filepath.Join(tmpDir, "brain-runner", "config.yaml")
	backupPath := legacyPath + ".backup"

	// =============================================================================
	// Phase 1: No config exists → LoadConfig returns defaults
	// =============================================================================
	t.Run("Phase1_NoConfig_ReturnsDefaults", func(t *testing.T) {
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig() with no files error = %v", err)
		}

		// Verify defaults for all subsystems
		if cfg.Server.Port != 3333 {
			t.Errorf("Default Server.Port = %d, want 3333", cfg.Server.Port)
		}
		if cfg.Server.Host != "localhost" {
			t.Errorf("Default Server.Host = %q, want localhost", cfg.Server.Host)
		}
		if cfg.Server.LogLevel != "info" {
			t.Errorf("Default Server.LogLevel = %q, want info", cfg.Server.LogLevel)
		}
		if cfg.Runner.MaxParallel != 3 {
			t.Errorf("Default Runner.MaxParallel = %d, want 3", cfg.Runner.MaxParallel)
		}
		if cfg.Runner.PollInterval != 5 {
			t.Errorf("Default Runner.PollInterval = %d, want 5", cfg.Runner.PollInterval)
		}
		if cfg.Runner.AutoMonitors != true {
			t.Errorf("Default Runner.AutoMonitors = %v, want true", cfg.Runner.AutoMonitors)
		}
		if cfg.MCP.APIURL != "http://localhost:3333" {
			t.Errorf("Default MCP.APIURL = %q, want http://localhost:3333", cfg.MCP.APIURL)
		}
		if cfg.Plugins.OpencodePath != "opencode" {
			t.Errorf("Default Plugins.OpencodePath = %q, want opencode", cfg.Plugins.OpencodePath)
		}

		// Verify no files created yet
		if fileExists(unifiedPath) {
			t.Errorf("Unified config should not exist yet: %q", unifiedPath)
		}
		if fileExists(legacyPath) {
			t.Errorf("Legacy config should not exist yet: %q", legacyPath)
		}
	})

	// =============================================================================
	// Phase 2: Create legacy runner config → LoadConfig auto-migrates
	// =============================================================================
	t.Run("Phase2_LegacyConfig_AutoMigrates", func(t *testing.T) {
		// Create legacy config directory
		legacyDir := filepath.Dir(legacyPath)
		if err := os.MkdirAll(legacyDir, 0755); err != nil {
			t.Fatalf("failed to create legacy dir: %v", err)
		}

		// Write comprehensive legacy config with all field types
		legacyYAML := `max_parallel: 5
poll_interval: 10
task_poll_interval: 3
work_dir: "/custom/work"
state_dir: "/custom/state"
log_dir: "/custom/logs"
api_timeout: 3000
task_timeout: 120000
idle_detection_threshold: 30000
max_total_processes: 8
memory_threshold_percent: 15
auto_monitors: false
opencode:
  bin: "/usr/local/bin/opencode"
  agent: "dev"
  model: "claude-sonnet-4"
exclude_projects:
  - "test-project"
  - "legacy-project"
  - "archived-*"
`
		if err := os.WriteFile(legacyPath, []byte(legacyYAML), 0644); err != nil {
			t.Fatalf("failed to create legacy config: %v", err)
		}

		// LoadConfig should detect and migrate
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig() with legacy config error = %v", err)
		}

		// Verify all scalar fields migrated
		if cfg.Runner.MaxParallel != 5 {
			t.Errorf("Migrated Runner.MaxParallel = %d, want 5", cfg.Runner.MaxParallel)
		}
		if cfg.Runner.PollInterval != 10 {
			t.Errorf("Migrated Runner.PollInterval = %d, want 10", cfg.Runner.PollInterval)
		}
		if cfg.Runner.TaskPollInterval != 3 {
			t.Errorf("Migrated Runner.TaskPollInterval = %d, want 3", cfg.Runner.TaskPollInterval)
		}
		if cfg.Runner.WorkDir != "/custom/work" {
			t.Errorf("Migrated Runner.WorkDir = %q, want /custom/work", cfg.Runner.WorkDir)
		}
		if cfg.Runner.StateDir != "/custom/state" {
			t.Errorf("Migrated Runner.StateDir = %q, want /custom/state", cfg.Runner.StateDir)
		}
		if cfg.Runner.LogDir != "/custom/logs" {
			t.Errorf("Migrated Runner.LogDir = %q, want /custom/logs", cfg.Runner.LogDir)
		}
		if cfg.Runner.APITimeout != 3000 {
			t.Errorf("Migrated Runner.APITimeout = %d, want 3000", cfg.Runner.APITimeout)
		}
		if cfg.Runner.TaskTimeout != 120000 {
			t.Errorf("Migrated Runner.TaskTimeout = %d, want 120000", cfg.Runner.TaskTimeout)
		}
		if cfg.Runner.IdleDetectionThreshold != 30000 {
			t.Errorf("Migrated Runner.IdleDetectionThreshold = %d, want 30000", cfg.Runner.IdleDetectionThreshold)
		}
		if cfg.Runner.MaxTotalProcesses != 8 {
			t.Errorf("Migrated Runner.MaxTotalProcesses = %d, want 8", cfg.Runner.MaxTotalProcesses)
		}
		if cfg.Runner.MemoryThresholdPercent != 15 {
			t.Errorf("Migrated Runner.MemoryThresholdPercent = %d, want 15", cfg.Runner.MemoryThresholdPercent)
		}
		if cfg.Runner.AutoMonitors != false {
			t.Errorf("Migrated Runner.AutoMonitors = %v, want false", cfg.Runner.AutoMonitors)
		}

		// Verify nested opencode config migrated
		if cfg.Runner.Opencode.Bin != "/usr/local/bin/opencode" {
			t.Errorf("Migrated Runner.Opencode.Bin = %q, want /usr/local/bin/opencode", cfg.Runner.Opencode.Bin)
		}
		if cfg.Runner.Opencode.Agent != "dev" {
			t.Errorf("Migrated Runner.Opencode.Agent = %q, want dev", cfg.Runner.Opencode.Agent)
		}
		if cfg.Runner.Opencode.Model != "claude-sonnet-4" {
			t.Errorf("Migrated Runner.Opencode.Model = %q, want claude-sonnet-4", cfg.Runner.Opencode.Model)
		}

		// Verify array fields migrated
		if len(cfg.Runner.ExcludeProjects) != 3 {
			t.Fatalf("Migrated Runner.ExcludeProjects length = %d, want 3", len(cfg.Runner.ExcludeProjects))
		}
		if cfg.Runner.ExcludeProjects[0] != "test-project" {
			t.Errorf("Migrated Runner.ExcludeProjects[0] = %q, want test-project", cfg.Runner.ExcludeProjects[0])
		}
		if cfg.Runner.ExcludeProjects[1] != "legacy-project" {
			t.Errorf("Migrated Runner.ExcludeProjects[1] = %q, want legacy-project", cfg.Runner.ExcludeProjects[1])
		}
		if cfg.Runner.ExcludeProjects[2] != "archived-*" {
			t.Errorf("Migrated Runner.ExcludeProjects[2] = %q, want archived-*", cfg.Runner.ExcludeProjects[2])
		}

		// Verify Server defaults preserved (not in legacy config)
		if cfg.Server.Port != 3333 {
			t.Errorf("Server.Port = %d, want 3333 (default preserved)", cfg.Server.Port)
		}
		if cfg.Server.Host != "localhost" {
			t.Errorf("Server.Host = %q, want localhost (default preserved)", cfg.Server.Host)
		}
	})

	// =============================================================================
	// Phase 3: Verify unified config file created
	// =============================================================================
	t.Run("Phase3_UnifiedConfigFileCreated", func(t *testing.T) {
		if !fileExists(unifiedPath) {
			t.Fatalf("Unified config file should exist after migration: %q", unifiedPath)
		}

		// Verify file is readable and valid YAML
		data, err := os.ReadFile(unifiedPath)
		if err != nil {
			t.Fatalf("failed to read unified config: %v", err)
		}

		var loaded UnifiedConfig
		if err := yaml.Unmarshal(data, &loaded); err != nil {
			t.Fatalf("unified config is not valid YAML: %v", err)
		}

		// Verify migrated values persisted to file
		if loaded.Runner.MaxParallel != 5 {
			t.Errorf("Persisted Runner.MaxParallel = %d, want 5", loaded.Runner.MaxParallel)
		}
		if loaded.Runner.Opencode.Agent != "dev" {
			t.Errorf("Persisted Runner.Opencode.Agent = %q, want dev", loaded.Runner.Opencode.Agent)
		}
		if len(loaded.Runner.ExcludeProjects) != 3 {
			t.Errorf("Persisted Runner.ExcludeProjects length = %d, want 3", len(loaded.Runner.ExcludeProjects))
		}
	})

	// =============================================================================
	// Phase 4: Verify backup file created
	// =============================================================================
	t.Run("Phase4_BackupFileCreated", func(t *testing.T) {
		if !fileExists(backupPath) {
			t.Fatalf("Backup file should exist after migration: %q", backupPath)
		}

		// Verify original legacy config no longer exists (renamed to backup)
		if fileExists(legacyPath) {
			t.Errorf("Legacy config should not exist after migration (renamed to backup): %q", legacyPath)
		}

		// Verify backup contains original legacy config data
		data, err := os.ReadFile(backupPath)
		if err != nil {
			t.Fatalf("failed to read backup file: %v", err)
		}

		var legacyData map[string]interface{}
		if err := yaml.Unmarshal(data, &legacyData); err != nil {
			t.Fatalf("backup file is not valid YAML: %v", err)
		}

		// Verify backup has legacy values
		if v, ok := legacyData["max_parallel"].(int); !ok || v != 5 {
			t.Errorf("Backup max_parallel = %v, want 5", legacyData["max_parallel"])
		}
	})

	// =============================================================================
	// Phase 5: LoadConfig again → loads from unified (no re-migration)
	// =============================================================================
	t.Run("Phase5_SecondLoad_NoReMigration", func(t *testing.T) {
		// Record current state
		unifiedStat, err := os.Stat(unifiedPath)
		if err != nil {
			t.Fatalf("failed to stat unified config: %v", err)
		}
		originalModTime := unifiedStat.ModTime()

		// Load config again
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig() second load error = %v", err)
		}

		// Verify values still match (loaded from unified)
		if cfg.Runner.MaxParallel != 5 {
			t.Errorf("Second load Runner.MaxParallel = %d, want 5", cfg.Runner.MaxParallel)
		}
		if cfg.Runner.Opencode.Agent != "dev" {
			t.Errorf("Second load Runner.Opencode.Agent = %q, want dev", cfg.Runner.Opencode.Agent)
		}

		// Verify unified config file not modified (no re-migration)
		unifiedStat2, err := os.Stat(unifiedPath)
		if err != nil {
			t.Fatalf("failed to stat unified config after second load: %v", err)
		}
		if !unifiedStat2.ModTime().Equal(originalModTime) {
			t.Errorf("Unified config was modified on second load (re-migration occurred)")
		}

		// Verify backup still exists (only one backup, no duplicates)
		if !fileExists(backupPath) {
			t.Errorf("Backup file should still exist after second load")
		}
		backupBackupPath := backupPath + ".backup"
		if fileExists(backupBackupPath) {
			t.Errorf("Duplicate backup should not exist: %q", backupBackupPath)
		}
	})

	// =============================================================================
	// Phase 6: Modify unified config → LoadConfig loads modified values
	// =============================================================================
	t.Run("Phase6_ModifyUnified_LoadsNewValues", func(t *testing.T) {
		// Modify unified config with new values
		modifiedYAML := `server:
  port: 9999
  host: "custom-host"
  log_level: "debug"
  enable_auth: true
  api_key: "secret123"
  cors_origin: "https://example.com"
  tls_cert: "/path/to/cert"
  tls_key: "/path/to/key"
runner:
  max_parallel: 10
  poll_interval: 20
  task_poll_interval: 7
  work_dir: "/modified/work"
  state_dir: "/modified/state"
  log_dir: "/modified/logs"
  api_timeout: 5000
  task_timeout: 180000
  idle_detection_threshold: 45000
  max_total_processes: 15
  memory_threshold_percent: 20
  auto_monitors: true
  opencode:
    bin: "/opt/opencode"
    agent: "tdd-dev"
    model: "claude-opus-4"
  exclude_projects:
    - "exclude-1"
    - "exclude-2"
mcp:
  api_url: "http://modified:7777"
plugins:
  opencode_path: "/custom/opencode"
  claude_code_path: "/custom/claude"
`
		if err := os.WriteFile(unifiedPath, []byte(modifiedYAML), 0644); err != nil {
			t.Fatalf("failed to modify unified config: %v", err)
		}

		// Load config again
		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig() after modification error = %v", err)
		}

		// Verify all modified Server values
		if cfg.Server.Port != 9999 {
			t.Errorf("Modified Server.Port = %d, want 9999", cfg.Server.Port)
		}
		if cfg.Server.Host != "custom-host" {
			t.Errorf("Modified Server.Host = %q, want custom-host", cfg.Server.Host)
		}
		if cfg.Server.LogLevel != "debug" {
			t.Errorf("Modified Server.LogLevel = %q, want debug", cfg.Server.LogLevel)
		}
		if cfg.Server.EnableAuth != true {
			t.Errorf("Modified Server.EnableAuth = %v, want true", cfg.Server.EnableAuth)
		}
		if cfg.Server.APIKey != "secret123" {
			t.Errorf("Modified Server.APIKey = %q, want secret123", cfg.Server.APIKey)
		}
		if cfg.Server.CORSOrigin != "https://example.com" {
			t.Errorf("Modified Server.CORSOrigin = %q, want https://example.com", cfg.Server.CORSOrigin)
		}
		if cfg.Server.TLSCert != "/path/to/cert" {
			t.Errorf("Modified Server.TLSCert = %q, want /path/to/cert", cfg.Server.TLSCert)
		}
		if cfg.Server.TLSKey != "/path/to/key" {
			t.Errorf("Modified Server.TLSKey = %q, want /path/to/key", cfg.Server.TLSKey)
		}

		// Verify all modified Runner values
		if cfg.Runner.MaxParallel != 10 {
			t.Errorf("Modified Runner.MaxParallel = %d, want 10", cfg.Runner.MaxParallel)
		}
		if cfg.Runner.PollInterval != 20 {
			t.Errorf("Modified Runner.PollInterval = %d, want 20", cfg.Runner.PollInterval)
		}
		if cfg.Runner.TaskPollInterval != 7 {
			t.Errorf("Modified Runner.TaskPollInterval = %d, want 7", cfg.Runner.TaskPollInterval)
		}
		if cfg.Runner.WorkDir != "/modified/work" {
			t.Errorf("Modified Runner.WorkDir = %q, want /modified/work", cfg.Runner.WorkDir)
		}
		if cfg.Runner.StateDir != "/modified/state" {
			t.Errorf("Modified Runner.StateDir = %q, want /modified/state", cfg.Runner.StateDir)
		}
		if cfg.Runner.LogDir != "/modified/logs" {
			t.Errorf("Modified Runner.LogDir = %q, want /modified/logs", cfg.Runner.LogDir)
		}
		if cfg.Runner.APITimeout != 5000 {
			t.Errorf("Modified Runner.APITimeout = %d, want 5000", cfg.Runner.APITimeout)
		}
		if cfg.Runner.TaskTimeout != 180000 {
			t.Errorf("Modified Runner.TaskTimeout = %d, want 180000", cfg.Runner.TaskTimeout)
		}
		if cfg.Runner.IdleDetectionThreshold != 45000 {
			t.Errorf("Modified Runner.IdleDetectionThreshold = %d, want 45000", cfg.Runner.IdleDetectionThreshold)
		}
		if cfg.Runner.MaxTotalProcesses != 15 {
			t.Errorf("Modified Runner.MaxTotalProcesses = %d, want 15", cfg.Runner.MaxTotalProcesses)
		}
		if cfg.Runner.MemoryThresholdPercent != 20 {
			t.Errorf("Modified Runner.MemoryThresholdPercent = %d, want 20", cfg.Runner.MemoryThresholdPercent)
		}
		if cfg.Runner.AutoMonitors != true {
			t.Errorf("Modified Runner.AutoMonitors = %v, want true", cfg.Runner.AutoMonitors)
		}

		// Verify modified nested opencode config
		if cfg.Runner.Opencode.Bin != "/opt/opencode" {
			t.Errorf("Modified Runner.Opencode.Bin = %q, want /opt/opencode", cfg.Runner.Opencode.Bin)
		}
		if cfg.Runner.Opencode.Agent != "tdd-dev" {
			t.Errorf("Modified Runner.Opencode.Agent = %q, want tdd-dev", cfg.Runner.Opencode.Agent)
		}
		if cfg.Runner.Opencode.Model != "claude-opus-4" {
			t.Errorf("Modified Runner.Opencode.Model = %q, want claude-opus-4", cfg.Runner.Opencode.Model)
		}

		// Verify modified array fields
		if len(cfg.Runner.ExcludeProjects) != 2 {
			t.Fatalf("Modified Runner.ExcludeProjects length = %d, want 2", len(cfg.Runner.ExcludeProjects))
		}
		if cfg.Runner.ExcludeProjects[0] != "exclude-1" {
			t.Errorf("Modified Runner.ExcludeProjects[0] = %q, want exclude-1", cfg.Runner.ExcludeProjects[0])
		}
		if cfg.Runner.ExcludeProjects[1] != "exclude-2" {
			t.Errorf("Modified Runner.ExcludeProjects[1] = %q, want exclude-2", cfg.Runner.ExcludeProjects[1])
		}

		// Verify modified MCP values
		if cfg.MCP.APIURL != "http://modified:7777" {
			t.Errorf("Modified MCP.APIURL = %q, want http://modified:7777", cfg.MCP.APIURL)
		}

		// Verify modified Plugins values
		if cfg.Plugins.OpencodePath != "/custom/opencode" {
			t.Errorf("Modified Plugins.OpencodePath = %q, want /custom/opencode", cfg.Plugins.OpencodePath)
		}
		if cfg.Plugins.ClaudeCodePath != "/custom/claude" {
			t.Errorf("Modified Plugins.ClaudeCodePath = %q, want /custom/claude", cfg.Plugins.ClaudeCodePath)
		}
	})
}
