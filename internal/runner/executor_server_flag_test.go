package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// v2 removed `opencode run --attach`; the runner must drive a serve instance
// with `--server <url>` and authenticate with OPENCODE_PASSWORD in the run
// process env. These tests pin both.

func TestSpawnHeadlessDirect_UsesServerFlagNotAttach(t *testing.T) {
	stateDir := t.TempDir()
	promptFile := filepath.Join(stateDir, "prompt.txt")
	if err := os.WriteFile(promptFile, []byte("do the thing"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	e := NewExecutor(cfg)

	var gotArgs []string
	var gotCmd *exec.Cmd
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		gotArgs = args
		gotCmd = exec.Command("/bin/echo", "spawned")
		return gotCmd
	}

	task := testResolvedTask("abc123")
	_, err := e.spawnHeadlessDirect(stateDir, "proj", task, promptFile, SpawnOptions{}, 4096, "ses_pinned", "servepw123")
	if err != nil {
		t.Fatalf("spawnHeadlessDirect: %v", err)
	}
	joined := strings.Join(gotArgs, " ")
	if strings.Contains(joined, "--attach") {
		t.Fatalf("v2 must not use --attach: %s", joined)
	}
	if !strings.Contains(joined, "--server http://127.0.0.1:4096") {
		t.Fatalf("run must use --server <url>, got: %s", joined)
	}
	if !strings.Contains(joined, "--session ses_pinned") {
		t.Fatalf("run args should pin the session, got: %s", joined)
	}
	// OPENCODE_PASSWORD must be exported in the run process env (set on the
	// cmd after the factory returns) so it authenticates to the serve instance.
	found := false
	for _, kv := range gotCmd.Env {
		if kv == "OPENCODE_PASSWORD=servepw123" {
			found = true
		}
	}
	if !found {
		t.Fatalf("OPENCODE_PASSWORD not exported in run env: %v", gotCmd.Env)
	}
}

func TestSpawnHeadlessDirect_NoServerFlagWithoutPort(t *testing.T) {
	stateDir := t.TempDir()
	promptFile := filepath.Join(stateDir, "prompt.txt")
	if err := os.WriteFile(promptFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	e := NewExecutor(cfg)

	var gotArgs []string
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		gotArgs = args
		return exec.Command("/bin/echo", "spawned")
	}
	task := testResolvedTask("abc123")
	if _, err := e.spawnHeadlessDirect(stateDir, "proj", task, promptFile, SpawnOptions{}, 0, "", ""); err != nil {
		t.Fatalf("spawnHeadlessDirect: %v", err)
	}
	joined := strings.Join(gotArgs, " ")
	if strings.Contains(joined, "--server") || strings.Contains(joined, "--attach") {
		t.Fatalf("non-attach run must not use --server/--attach: %s", joined)
	}
}
