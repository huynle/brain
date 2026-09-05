package runner

import (
	"os"
	"path/filepath"
	"testing"
)

func writeRunnerYAML(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfig_MemoryGuardDefaults(t *testing.T) {
	for _, k := range []string{"RUNNER_TASK_MEMORY_LIMIT_MB", "RUNNER_OPENCODE_DB_MAX_GB"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	cfg, err := LoadConfigFrom(writeRunnerYAML(t, "runner:\n  max_parallel: 3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TaskMemoryLimitMB != DefaultTaskMemoryLimitMB {
		t.Errorf("TaskMemoryLimitMB = %d, want default %d", cfg.TaskMemoryLimitMB, DefaultTaskMemoryLimitMB)
	}
	if cfg.OpencodeDBMaxGB != DefaultOpencodeDBMaxGB {
		t.Errorf("OpencodeDBMaxGB = %d, want default %d", cfg.OpencodeDBMaxGB, DefaultOpencodeDBMaxGB)
	}
}

// An explicit 0 in the file must disable the guard, not fall back to the
// default the way firstNonZero-style fields do.
func TestLoadConfig_MemoryGuardExplicitZeroDisables(t *testing.T) {
	for _, k := range []string{"RUNNER_TASK_MEMORY_LIMIT_MB", "RUNNER_OPENCODE_DB_MAX_GB"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	cfg, err := LoadConfigFrom(writeRunnerYAML(t, "runner:\n  max_parallel: 3\n  task_memory_limit_mb: 0\n  opencode_db_max_gb: 0\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TaskMemoryLimitMB != 0 || cfg.OpencodeDBMaxGB != 0 {
		t.Errorf("explicit zeros not honored: mem=%d db=%d", cfg.TaskMemoryLimitMB, cfg.OpencodeDBMaxGB)
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("0 must validate (it means disabled): %v", err)
	}
}

func TestLoadConfig_MemoryGuardEnvOverride(t *testing.T) {
	t.Setenv("RUNNER_TASK_MEMORY_LIMIT_MB", "4096")
	t.Setenv("RUNNER_OPENCODE_DB_MAX_GB", "8")
	cfg, err := LoadConfigFrom(writeRunnerYAML(t, "runner:\n  max_parallel: 3\n  task_memory_limit_mb: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TaskMemoryLimitMB != 4096 || cfg.OpencodeDBMaxGB != 8 {
		t.Errorf("env override lost: mem=%d db=%d", cfg.TaskMemoryLimitMB, cfg.OpencodeDBMaxGB)
	}
}

func TestValidateConfig_MemoryGuardNegativeRejected(t *testing.T) {
	cfg := testRunnerConfig()
	cfg.TaskMemoryLimitMB = -1
	if err := ValidateConfig(cfg); err == nil {
		t.Error("negative task_memory_limit_mb should be rejected")
	}
	cfg = testRunnerConfig()
	cfg.OpencodeDBMaxGB = -5
	if err := ValidateConfig(cfg); err == nil {
		t.Error("negative opencode_db_max_gb should be rejected")
	}
}
