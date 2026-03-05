package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	// Clear all env vars that might be set
	envVars := []string{"BRAIN_DIR", "PORT", "HOST", "ENABLE_AUTH", "API_KEY", "CORS_ORIGIN", "LOG_LEVEL"}
	for _, key := range envVars {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}

	cfg := Load()

	homeDir, _ := os.UserHomeDir()
	expectedBrainDir := filepath.Join(homeDir, ".brain")

	if cfg.BrainDir != expectedBrainDir {
		t.Errorf("BrainDir = %q, want %q", cfg.BrainDir, expectedBrainDir)
	}
	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want 3000", cfg.Port)
	}
	if cfg.Host != "0.0.0.0" {
		t.Errorf("Host = %q, want %q", cfg.Host, "0.0.0.0")
	}
	if cfg.EnableAuth != false {
		t.Errorf("EnableAuth = %v, want false", cfg.EnableAuth)
	}
	if cfg.APIKey != "" {
		t.Errorf("APIKey = %q, want empty", cfg.APIKey)
	}
	if cfg.CORSOrigin != "*" {
		t.Errorf("CORSOrigin = %q, want %q", cfg.CORSOrigin, "*")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("BRAIN_DIR", "/tmp/test-brain")
	t.Setenv("PORT", "8080")
	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("ENABLE_AUTH", "true")
	t.Setenv("API_KEY", "secret-key-123")
	t.Setenv("CORS_ORIGIN", "https://example.com")
	t.Setenv("LOG_LEVEL", "debug")

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
	if cfg.APIKey != "secret-key-123" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "secret-key-123")
	}
	if cfg.CORSOrigin != "https://example.com" {
		t.Errorf("CORSOrigin = %q, want %q", cfg.CORSOrigin, "https://example.com")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
}

func TestLoadInvalidPort(t *testing.T) {
	t.Setenv("PORT", "not-a-number")

	cfg := Load()

	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want 3000 (default on invalid input)", cfg.Port)
	}
}

func TestEnablAuthVariants(t *testing.T) {
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
		{"", false},
		{"yes", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			if tt.value == "" {
				os.Unsetenv("ENABLE_AUTH")
			} else {
				t.Setenv("ENABLE_AUTH", tt.value)
			}
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
