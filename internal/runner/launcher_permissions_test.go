package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestRunnerScriptTightensExistingPermissions(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "provider-secret-fixture")
	for _, executor := range []string{"opencode", "pi"} {
		t.Run(executor, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "runner_p_task.sh")
			if err := os.WriteFile(path, []byte("old launcher"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0o755); err != nil {
				t.Fatal(err)
			}
			cfg := RunnerConfig{StateDir: dir}
			task := &types.ResolvedTask{ID: "task"}
			build := NewExecutor(cfg).buildRunnerScript
			if executor == "pi" {
				build = NewPiExecutor(cfg).buildRunnerScript
			}
			got, err := build(task, "p", dir, filepath.Join(dir, "prompt"), SpawnOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if got != path {
				t.Fatalf("launcher path = %q, want %q", got, path)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o700 {
				t.Errorf("launcher permissions = %04o, want 0700", info.Mode().Perm())
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(content), "provider-secret-fixture") {
				t.Error("launcher does not contain forwarded provider secret")
			}
			if strings.Contains(string(content), "old launcher") {
				t.Error("old launcher content was not replaced")
			}
		})
	}
}
