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
