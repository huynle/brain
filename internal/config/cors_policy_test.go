package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCORSDefaultsAndExplicitPolicy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("CORS_ORIGIN", "")
	if got := DefaultConfig().Server.CORSOrigin; got != "" {
		t.Errorf("DefaultConfig CORS = %q, want empty", got)
	}
	data, err := DefaultConfigYAML()
	if err != nil {
		t.Fatal(err)
	}
	var defaults UnifiedConfig
	if err := yaml.Unmarshal(data, &defaults); err != nil {
		t.Fatal(err)
	}
	if defaults.Server.CORSOrigin != "" {
		t.Errorf("generated YAML CORS = %q, want empty", defaults.Server.CORSOrigin)
	}
	path := UnifiedConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"", "https://trusted.example", "*"} {
		t.Run("file/"+origin, func(t *testing.T) {
			if err := os.WriteFile(path, []byte("server:\n  cors_origin: \""+origin+"\"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfig()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Server.CORSOrigin != origin {
				t.Errorf("LoadConfig = %q, want %q", cfg.Server.CORSOrigin, origin)
			}
			if got := Load().CORSOrigin; got != origin {
				t.Errorf("Load = %q, want %q", got, origin)
			}
		})
	}
	for _, origin := range []string{"https://env.example", "*"} {
		t.Run("env/"+origin, func(t *testing.T) {
			t.Setenv("CORS_ORIGIN", origin)
			if got := Load().CORSOrigin; got != origin {
				t.Errorf("Load = %q, want %q", got, origin)
			}
		})
	}
}
