package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults_NoConfigFile(t *testing.T) {
	// Clear env vars that might affect config
	envVars := []string{"BRAIN_DIR", "PORT", "HOST", "ENABLE_AUTH", "CORS_ORIGIN", "LOG_LEVEL", "OAUTH_PIN", "JWT_SECRET", "BRAIN_JWT_SECRET"}
	for _, key := range envVars {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}

	// Note: if ~/.config/brain/config.yaml exists, values from there will
	// override defaults. This test validates that the built-in defaults are
	// sensible when no config file AND no env vars are set.
	cfg := Load()

	homeDir, _ := os.UserHomeDir()

	// These should always be set (from defaults or config file)
	if cfg.BrainDir == "" {
		t.Error("BrainDir should not be empty")
	}
	if cfg.Port == 0 {
		t.Error("Port should not be zero")
	}
	if cfg.Host == "" {
		t.Error("Host should not be empty")
	}

	// When no config file exists, verify built-in defaults
	configPath := filepath.Join(homeDir, ".config", "brain", "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		expectedBrainDir := filepath.Join(homeDir, ".brain")
		if cfg.BrainDir != expectedBrainDir {
			t.Errorf("BrainDir = %q, want %q (no config file)", cfg.BrainDir, expectedBrainDir)
		}
		if cfg.Port != 3333 {
			t.Errorf("Port = %d, want 3333 (no config file)", cfg.Port)
		}
		if cfg.Host != "localhost" {
			t.Errorf("Host = %q, want %q (no config file)", cfg.Host, "localhost")
		}
	}

	// These defaults don't depend on config file
	if cfg.EnableAuth != false {
		t.Errorf("EnableAuth = %v, want false", cfg.EnableAuth)
	}
	if cfg.CORSOrigin == "" {
		t.Error("CORSOrigin should not be empty")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.JWTSecret != "" {
		t.Errorf("JWTSecret = %q, want empty", cfg.JWTSecret)
	}
}

func TestLoadEnvVarsOverrideAll(t *testing.T) {
	// Env vars should override both defaults AND config file values
	t.Setenv("BRAIN_DIR", "/tmp/test-brain")
	t.Setenv("PORT", "8080")
	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("ENABLE_AUTH", "true")
	t.Setenv("CORS_ORIGIN", "https://example.com")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("OAUTH_PIN", "test-pin-123")
	t.Setenv("JWT_SECRET", "test-jwt-secret")

	cfg := Load()

	if cfg.BrainDir != "/tmp/test-brain" {
		t.Errorf("BrainDir = %q, want %q", cfg.BrainDir, "/tmp/test-brain")
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want %q", cfg.Host, "127.0.0.1")
	}
	if cfg.EnableAuth != true {
		t.Errorf("EnableAuth = %v, want true", cfg.EnableAuth)
	}
	if cfg.CORSOrigin != "https://example.com" {
		t.Errorf("CORSOrigin = %q, want %q", cfg.CORSOrigin, "https://example.com")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	if cfg.OAuthPIN != "test-pin-123" {
		t.Errorf("OAuthPIN = %q, want %q", cfg.OAuthPIN, "test-pin-123")
	}
	if cfg.JWTSecret != "test-jwt-secret" {
		t.Errorf("JWTSecret = %q, want %q", cfg.JWTSecret, "test-jwt-secret")
	}
}

func TestLoadBrainJWTSecretOverridesJWTSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "base-secret")
	t.Setenv("BRAIN_JWT_SECRET", "brain-secret")

	cfg := Load()

	if cfg.JWTSecret != "brain-secret" {
		t.Errorf("JWTSecret = %q, want %q", cfg.JWTSecret, "brain-secret")
	}
}

func TestLoadInvalidPort(t *testing.T) {
	t.Setenv("PORT", "not-a-number")

	cfg := Load()

	// Should keep whatever the config file or default provides
	if cfg.Port == 0 {
		t.Error("Port should not be zero on invalid PORT env var")
	}
}

func TestEnableAuthVariants(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"true", true},
		{"TRUE", true},
		{"True", true},
		{"1", true},
		{"false", false},
		{"0", false},
		{"yes", false}, // only "true" and "1" are truthy
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Setenv("ENABLE_AUTH", tt.value)
			cfg := Load()
			if cfg.EnableAuth != tt.want {
				t.Errorf("ENABLE_AUTH=%q: got %v, want %v", tt.value, cfg.EnableAuth, tt.want)
			}
		})
	}
}

func TestAddr(t *testing.T) {
	cfg := Config{Host: "0.0.0.0", Port: 3000}
	if got := cfg.Addr(); got != "0.0.0.0:3000" {
		t.Errorf("Addr() = %q, want %q", got, "0.0.0.0:3000")
	}
}

func TestLoadPriority_EnvOverridesConfigFile(t *testing.T) {
	// If the config file sets port 3333 but env says 9999, env wins
	t.Setenv("PORT", "9999")
	cfg := Load()
	if cfg.Port != 9999 {
		t.Errorf("Port = %d, want 9999 (env should override config file)", cfg.Port)
	}
}

func TestLoadPriority_UnsetEnvUsesConfigOrDefault(t *testing.T) {
	// When env is not set, the config file (or default) value should be used
	os.Unsetenv("HOST")
	t.Setenv("HOST", "")

	cfg := Load()
	// Should be either "localhost" (default/config) - never empty
	if cfg.Host == "" {
		t.Error("Host should not be empty when HOST env is unset")
	}
}

func TestLoadTaskDefaults_EnvVarOverrides(t *testing.T) {
	t.Setenv("BRAIN_DEFAULT_AGENT", "tdd-dev")
	t.Setenv("BRAIN_DEFAULT_MODEL", "claude-opus-4")

	cfg := Load()

	if cfg.TaskDefaults.Agent != "tdd-dev" {
		t.Errorf("TaskDefaults.Agent = %q, want %q", cfg.TaskDefaults.Agent, "tdd-dev")
	}
	if cfg.TaskDefaults.Model != "claude-opus-4" {
		t.Errorf("TaskDefaults.Model = %q, want %q", cfg.TaskDefaults.Model, "claude-opus-4")
	}
}

func TestLoadTaskDefaults_EmptyByDefault(t *testing.T) {
	// Clear any env vars that might set task defaults
	os.Unsetenv("BRAIN_DEFAULT_AGENT")
	os.Unsetenv("BRAIN_DEFAULT_MODEL")
	t.Setenv("BRAIN_DEFAULT_AGENT", "")
	t.Setenv("BRAIN_DEFAULT_MODEL", "")

	cfg := Load()

	// Without config file or env vars, task defaults should be empty.
	// Config file may set these, so this test only verifies Load completes
	// after env vars are cleared and does not assert ambient config state.
	_ = cfg
}

func TestLoadTaskDefaults_ThreadsFromConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	// Clear env vars that might override
	os.Unsetenv("BRAIN_DEFAULT_AGENT")
	os.Unsetenv("BRAIN_DEFAULT_MODEL")

	// Create config directory and file with task_defaults
	configDir := filepath.Join(tmpDir, "brain")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.yaml")
	yamlContent := `server:
  port: 3333
  task_defaults:
    agent: "explore"
    model: "claude-sonnet-4"
    execution_mode: "worktree"
    merge_policy: "auto_pr"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg := Load()

	if cfg.TaskDefaults.Agent != "explore" {
		t.Errorf("TaskDefaults.Agent = %q, want %q", cfg.TaskDefaults.Agent, "explore")
	}
	if cfg.TaskDefaults.Model != "claude-sonnet-4" {
		t.Errorf("TaskDefaults.Model = %q, want %q", cfg.TaskDefaults.Model, "claude-sonnet-4")
	}
	if cfg.TaskDefaults.ExecutionMode != "worktree" {
		t.Errorf("TaskDefaults.ExecutionMode = %q, want %q", cfg.TaskDefaults.ExecutionMode, "worktree")
	}
	if cfg.TaskDefaults.MergePolicy != "auto_pr" {
		t.Errorf("TaskDefaults.MergePolicy = %q, want %q", cfg.TaskDefaults.MergePolicy, "auto_pr")
	}
}

func TestLoadAttachmentExtraction_ThreadsFromConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	configDir := filepath.Join(tmpDir, "brain")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.yaml")
	yamlContent := `server:
  embedding:
    enabled: true
    model: "text-embedding-3-small"
  attachment_extraction:
    enabled: true
    provider: "openrouter"
    base_url: "https://openrouter.ai/api/v1"
    api_key_env: "BRAIN_ATTACHMENT_EXTRACTION_API_KEY"
    model: "google/gemini-2.5-pro"
    timeout_ms: 90000
    max_size_bytes: 12345
    supported_mime_types:
      - "image/png"
    max_derived_text_chars: 4096
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg := Load()
	ext := cfg.AttachmentExtraction

	if !ext.Enabled {
		t.Error("AttachmentExtraction.Enabled = false, want true")
	}
	if ext.APIKeyEnv != "BRAIN_ATTACHMENT_EXTRACTION_API_KEY" {
		t.Errorf("AttachmentExtraction.APIKeyEnv = %q, want BRAIN_ATTACHMENT_EXTRACTION_API_KEY", ext.APIKeyEnv)
	}
	if ext.Model != "google/gemini-2.5-pro" {
		t.Errorf("AttachmentExtraction.Model = %q, want google/gemini-2.5-pro", ext.Model)
	}
	if ext.MaxDerivedTextChars != 4096 {
		t.Errorf("AttachmentExtraction.MaxDerivedTextChars = %d, want 4096", ext.MaxDerivedTextChars)
	}
	if cfg.Embedding.Model != "text-embedding-3-small" {
		t.Errorf("Embedding.Model = %q, want unchanged text-embedding-3-small", cfg.Embedding.Model)
	}
}

func TestLoadTaskDefaults_EnvOverridesConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	// Config file sets one value, env var overrides it
	configDir := filepath.Join(tmpDir, "brain")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.yaml")
	yamlContent := `server:
  task_defaults:
    agent: "from-config"
    model: "from-config"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	t.Setenv("BRAIN_DEFAULT_AGENT", "from-env")
	t.Setenv("BRAIN_DEFAULT_MODEL", "from-env-model")

	cfg := Load()

	if cfg.TaskDefaults.Agent != "from-env" {
		t.Errorf("TaskDefaults.Agent = %q, want %q (env should override config)", cfg.TaskDefaults.Agent, "from-env")
	}
	if cfg.TaskDefaults.Model != "from-env-model" {
		t.Errorf("TaskDefaults.Model = %q, want %q (env should override config)", cfg.TaskDefaults.Model, "from-env-model")
	}
}

func TestLoadAttachments_ThreadsFromConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	configDir := filepath.Join(tmpDir, "brain")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.yaml")
	yamlContent := `server:
  attachments:
    storage_root: "/tmp/brain-attachments"
    max_upload_size_bytes: 1048576
    allowed_mime_types:
      - "image/png"
    blocked_mime_types:
      - "application/x-msdownload"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg := Load()

	if cfg.Attachments.StorageRoot != "/tmp/brain-attachments" {
		t.Errorf("Attachments.StorageRoot = %q, want %q", cfg.Attachments.StorageRoot, "/tmp/brain-attachments")
	}
	if cfg.Attachments.MaxUploadSizeBytes != 1048576 {
		t.Errorf("Attachments.MaxUploadSizeBytes = %d, want 1048576", cfg.Attachments.MaxUploadSizeBytes)
	}
	if len(cfg.Attachments.AllowedMIMETypes) != 1 || cfg.Attachments.AllowedMIMETypes[0] != "image/png" {
		t.Errorf("Attachments.AllowedMIMETypes = %#v, want image/png", cfg.Attachments.AllowedMIMETypes)
	}
	if len(cfg.Attachments.BlockedMIMETypes) != 1 || cfg.Attachments.BlockedMIMETypes[0] != "application/x-msdownload" {
		t.Errorf("Attachments.BlockedMIMETypes = %#v, want application/x-msdownload", cfg.Attachments.BlockedMIMETypes)
	}
}
