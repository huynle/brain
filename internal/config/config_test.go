package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults_NoConfigFile(t *testing.T) {
	// Clear env vars that might affect config
	envVars := []string{"BRAIN_DIR", "PORT", "HOST", "ENABLE_AUTH", "CORS_ORIGIN", "LOG_LEVEL", "OAUTH_PIN", "BRAIN_EMBEDDINGS_ENABLED", "BRAIN_EMBEDDINGS_PROVIDER", "BRAIN_EMBEDDINGS_MODEL", "BRAIN_EMBEDDINGS_BASE_URL", "BRAIN_EMBEDDINGS_TIMEOUT_MS", "BRAIN_EMBEDDINGS_BATCH_SIZE", "BRAIN_FILE_WATCHER_ENABLED", "BRAIN_FILE_WATCHER_DEBOUNCE_MS", "BRAIN_FILE_WATCHER_IGNORE_PATTERNS"}
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
	if cfg.Embeddings.Enabled {
		t.Error("Embeddings.Enabled should default to false")
	}
	if cfg.Embeddings.TimeoutMS != DefaultEmbeddingTimeoutMS {
		t.Errorf("Embeddings.TimeoutMS = %d, want %d", cfg.Embeddings.TimeoutMS, DefaultEmbeddingTimeoutMS)
	}
	if cfg.Embeddings.BatchSize != DefaultEmbeddingBatchSize {
		t.Errorf("Embeddings.BatchSize = %d, want %d", cfg.Embeddings.BatchSize, DefaultEmbeddingBatchSize)
	}
	if cfg.FileWatcher.Enabled {
		t.Error("FileWatcher.Enabled should default to false")
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
}

func TestLoadEmbeddingEnvVars(t *testing.T) {
	t.Setenv("BRAIN_EMBEDDINGS_ENABLED", "true")
	t.Setenv("BRAIN_EMBEDDINGS_PROVIDER", "Ollama")
	t.Setenv("BRAIN_EMBEDDINGS_MODEL", "nomic-embed-text")
	t.Setenv("BRAIN_EMBEDDINGS_BASE_URL", "http://localhost:11434/")
	t.Setenv("BRAIN_EMBEDDINGS_TIMEOUT_MS", "45000")
	t.Setenv("BRAIN_EMBEDDINGS_BATCH_SIZE", "32")

	cfg := Load()

	if !cfg.Embeddings.Enabled {
		t.Error("Embeddings.Enabled = false, want true")
	}
	if cfg.Embeddings.Provider != "ollama" {
		t.Errorf("Embeddings.Provider = %q, want %q", cfg.Embeddings.Provider, "ollama")
	}
	if cfg.Embeddings.Model != "nomic-embed-text" {
		t.Errorf("Embeddings.Model = %q, want %q", cfg.Embeddings.Model, "nomic-embed-text")
	}
	if cfg.Embeddings.BaseURL != "http://localhost:11434" {
		t.Errorf("Embeddings.BaseURL = %q, want %q", cfg.Embeddings.BaseURL, "http://localhost:11434")
	}
	if cfg.Embeddings.TimeoutMS != 45000 {
		t.Errorf("Embeddings.TimeoutMS = %d, want 45000", cfg.Embeddings.TimeoutMS)
	}
	if cfg.Embeddings.BatchSize != 32 {
		t.Errorf("Embeddings.BatchSize = %d, want 32", cfg.Embeddings.BatchSize)
	}
}

func TestLoadFileWatcherEnvVars(t *testing.T) {
	t.Setenv("BRAIN_FILE_WATCHER_ENABLED", "true")
	t.Setenv("BRAIN_FILE_WATCHER_DEBOUNCE_MS", "250")
	t.Setenv("BRAIN_FILE_WATCHER_IGNORE_PATTERNS", "drafts/, tmp/ ,")

	cfg := Load()

	if !cfg.FileWatcher.Enabled {
		t.Error("FileWatcher.Enabled = false, want true")
	}
	if cfg.FileWatcher.DebounceMS != 250 {
		t.Errorf("FileWatcher.DebounceMS = %d, want 250", cfg.FileWatcher.DebounceMS)
	}
	wantPatterns := []string{"drafts/", "tmp/"}
	if len(cfg.FileWatcher.IgnorePatterns) != len(wantPatterns) {
		t.Fatalf("FileWatcher.IgnorePatterns = %v, want %v", cfg.FileWatcher.IgnorePatterns, wantPatterns)
	}
	for i, want := range wantPatterns {
		if cfg.FileWatcher.IgnorePatterns[i] != want {
			t.Errorf("FileWatcher.IgnorePatterns[%d] = %q, want %q", i, cfg.FileWatcher.IgnorePatterns[i], want)
		}
	}
}

func TestEmbeddingConfigValidate(t *testing.T) {
	if err := (EmbeddingConfig{}).Validate(); err != nil {
		t.Fatalf("disabled embeddings should validate, got %v", err)
	}
	if err := (EmbeddingConfig{Enabled: true, Model: "m"}).Normalize().Validate(); err == nil {
		t.Fatal("enabled embeddings without provider should fail validation")
	}
	if err := (EmbeddingConfig{Enabled: true, Provider: "ollama"}).Normalize().Validate(); err == nil {
		t.Fatal("enabled embeddings without model should fail validation")
	}
	if err := (EmbeddingConfig{Enabled: true, Provider: "ollama", Model: "m"}).Normalize().Validate(); err != nil {
		t.Fatalf("valid ollama embeddings should validate, got %v", err)
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
