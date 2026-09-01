package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestExecutionMode_SpawnMode covers the fold from runner-presentation modes to
// the spawn strategy executors switch on.
func TestExecutionMode_SpawnMode(t *testing.T) {
	tests := []struct {
		name string
		mode ExecutionMode
		want ExecutionMode
	}{
		// --foreground describes the runner, not the task: it must spawn
		// exactly like headless rather than reaching executors as an
		// unrecognized mode.
		{name: "foreground folds to headless", mode: ExecutionModeForeground, want: ExecutionModeHeadless},
		{name: "empty defaults to headless", mode: "", want: ExecutionModeHeadless},
		{name: "headless is unchanged", mode: ExecutionModeHeadless, want: ExecutionModeHeadless},
		{name: "tui is unchanged", mode: ExecutionModeTUI, want: ExecutionModeTUI},
		{name: "dashboard is unchanged", mode: ExecutionModeDashboard, want: ExecutionModeDashboard},
		{name: "unknown is left alone to be rejected", mode: "nonsense", want: "nonsense"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mode.SpawnMode(); got != tt.want {
				t.Fatalf("ExecutionMode(%q).SpawnMode() = %q, want %q", tt.mode, got, tt.want)
			}
		})
	}
}

// TestOpenCodeExecutor_ForegroundSpawnsHeadless is the regression test for
// `brain run start --headless --foreground` failing every OpenCode task with
// "unknown execution mode: foreground". The script executor never reads the
// mode, which is why only OpenCode and Pi tasks broke.
func TestOpenCodeExecutor_ForegroundSpawnsHeadless(t *testing.T) {
	stateDir := t.TempDir()
	promptFile := filepath.Join(stateDir, "prompt.txt")
	if err := os.WriteFile(promptFile, []byte("do the thing"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	// Skip the serve+attach path so the test needs no OpenCode server; the
	// mode fold happens before this branch either way.
	cfg.Control.Disabled = true
	e := NewExecutor(cfg)

	var gotArgs []string
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		gotArgs = args
		return exec.Command("/bin/echo", "spawned")
	}

	task := testResolvedTask("abc123")
	res, err := e.spawnOpencode(context.Background(), task, "proj", stateDir, promptFile, SpawnOptions{
		Mode: ExecutionModeForeground,
	})
	if err != nil {
		t.Fatalf("foreground spawn should succeed, got: %v", err)
	}
	if res == nil || res.PID == 0 {
		t.Fatal("expected a spawned process")
	}
	if len(gotArgs) == 0 || gotArgs[0] != "run" {
		t.Fatalf("expected a headless `opencode run`, got args: %v", gotArgs)
	}
}

func TestOpenCodeExecutor_UnknownModeStillRejected(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.StateDir = t.TempDir()
	e := NewExecutor(cfg)

	_, err := e.spawnOpencode(context.Background(), testResolvedTask("abc123"), "proj",
		cfg.StateDir, filepath.Join(cfg.StateDir, "prompt.txt"), SpawnOptions{Mode: "nonsense"})
	if err == nil {
		t.Fatal("an unrecognized mode must still be rejected")
	}
	if !strings.Contains(err.Error(), "valid modes") {
		t.Fatalf("error should list the valid modes, got: %v", err)
	}
}
