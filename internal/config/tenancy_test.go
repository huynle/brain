package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadTenancy(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, env, want string
		invalid               bool
	}{
		{name: "absent", want: "single"},
		{name: "unrelated config", yaml: "server:\n  port: 4444\n", want: "single"},
		{name: "yaml multi", yaml: "server:\n  tenancy:\n    mode: multi\n", want: "multi"},
		{name: "env multi", env: "multi", want: "multi"},
		{name: "env overrides", yaml: "server:\n  tenancy:\n    mode: multi\n", env: "single", want: "single"},
		{name: "env overrides invalid yaml mode", yaml: "server:\n  tenancy:\n    mode: invalid\n", env: "single", want: "single"},
		{name: "invalid env", env: "invalid", invalid: true},
		{name: "invalid yaml", yaml: "server:\n  tenancy:\n    mode: invalid\n", invalid: true},
		{name: "malformed yaml", yaml: "server: [", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("BRAIN_TENANT_MODE", tc.env)
			if tc.env == "" {
				if err := os.Unsetenv("BRAIN_TENANT_MODE"); err != nil {
					t.Fatal(err)
				}
			}
			if tc.yaml != "" {
				path := UnifiedConfigPath()
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(tc.yaml), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cfg := Load()
			if tc.invalid {
				if cfg.Err() == nil {
					t.Fatal("invalid configuration silently accepted")
				}
				if tc.name != "malformed yaml" && (!strings.Contains(cfg.Err().Error(), "single") || !strings.Contains(cfg.Err().Error(), "multi")) {
					t.Fatalf("error must name valid modes: %v", cfg.Err())
				}
				return
			}
			if cfg.Err() != nil {
				t.Fatal(cfg.Err())
			}
			if got := string(cfg.Tenancy.Mode); got != tc.want {
				t.Fatalf("mode = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTenantModeEnvironmentSingleFile(t *testing.T) {
	root := filepath.Join("..", "..")
	var matches []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", ".worktrees":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "BRAIN_TENANT_MODE") {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			matches = append(matches, filepath.ToSlash(rel))
			if strings.Count(string(data), "BRAIN_TENANT_MODE") != 1 {
				t.Errorf("%s: expected exactly one env reference", rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(matches, []string{"internal/config/config.go"}) {
		t.Fatalf("env references = %v; want only internal/config/config.go", matches)
	}
}
