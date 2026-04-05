package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// =============================================================================
// Compile-time interface conformance
// =============================================================================

var _ TaskExecutor = (*PiExecutor)(nil)

// =============================================================================
// Test Helpers
// =============================================================================

func testPiExecutorConfig() RunnerConfig {
	return RunnerConfig{
		BrainAPIURL: "http://localhost:3333",
		StateDir:    os.TempDir(),
		WorkDir:     "/default/workdir",
		Pi: PiConfig{
			Bin:   "pi",
			Model: "default-pi-model",
		},
	}
}

// =============================================================================
// BuildPrompt Tests
// =============================================================================

func TestPiExecutor_BuildPrompt_DirectPrompt(t *testing.T) {
	cfg := testPiExecutorConfig()
	pe := NewPiExecutor(cfg)

	task := testResolvedTask("abc123")
	task.DirectPrompt = "Do this specific thing verbatim"

	prompt := pe.BuildPrompt(task, false)
	if prompt != "Do this specific thing verbatim" {
		t.Errorf("BuildPrompt with direct_prompt = %q, want %q", prompt, "Do this specific thing verbatim")
	}
}

func TestPiExecutor_BuildPrompt_DirectPrompt_IgnoresResume(t *testing.T) {
	cfg := testPiExecutorConfig()
	pe := NewPiExecutor(cfg)

	task := testResolvedTask("abc123")
	task.DirectPrompt = "Do this specific thing"

	// Even with isResume=true, direct_prompt should be used verbatim
	prompt := pe.BuildPrompt(task, true)
	if prompt != "Do this specific thing" {
		t.Errorf("BuildPrompt with direct_prompt and isResume = %q, want %q", prompt, "Do this specific thing")
	}
}

func TestPiExecutor_BuildPrompt_NewTask(t *testing.T) {
	cfg := testPiExecutorConfig()
	pe := NewPiExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Path = "projects/myproj/task/abc123.md"

	prompt := pe.BuildPrompt(task, false)

	if !strings.Contains(prompt, task.Path) {
		t.Errorf("new task prompt should contain task path %q", task.Path)
	}
	if strings.Contains(prompt, "RESUME") {
		t.Error("new task prompt should not contain RESUME")
	}
	if !strings.Contains(prompt, "brain_recall") {
		t.Error("new task prompt should reference brain_recall")
	}
}

func TestPiExecutor_BuildPrompt_ResumeTask(t *testing.T) {
	cfg := testPiExecutorConfig()
	pe := NewPiExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Path = "projects/myproj/task/abc123.md"

	prompt := pe.BuildPrompt(task, true)

	if !strings.Contains(prompt, "RESUME") {
		t.Error("resume prompt should contain RESUME")
	}
	if !strings.Contains(prompt, task.Path) {
		t.Errorf("resume prompt should contain task path %q", task.Path)
	}
}

// =============================================================================
// ResolveWorkdir Tests
// =============================================================================

func TestPiExecutor_ResolveWorkdir_TargetWorkdir(t *testing.T) {
	dir := t.TempDir()
	cfg := testPiExecutorConfig()
	cfg.WorkDir = "/fallback"
	pe := NewPiExecutor(cfg)

	task := testResolvedTask("abc123")
	task.TargetWorkdir = dir // exists

	result, err := pe.ResolveWorkdir(task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != dir {
		t.Errorf("ResolveWorkdir = %q, want %q", result, dir)
	}
}

func TestPiExecutor_ResolveWorkdir_FallbackToConfig(t *testing.T) {
	cfg := testPiExecutorConfig()
	cfg.WorkDir = "/config/default"
	pe := NewPiExecutor(cfg)

	task := testResolvedTask("abc123")
	// No workdir fields set

	result, err := pe.ResolveWorkdir(task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "/config/default" {
		t.Errorf("ResolveWorkdir = %q, want %q", result, "/config/default")
	}
}

// =============================================================================
// GetEffectiveModel Tests
// =============================================================================

func TestPiExecutor_GetEffectiveModel_TaskOverride(t *testing.T) {
	cfg := testPiExecutorConfig()
	cfg.Pi.Model = "config-model"
	pe := NewPiExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Model = "task-model"

	model := pe.GetEffectiveModel(task, "")
	if model != "task-model" {
		t.Errorf("GetEffectiveModel = %q, want %q", model, "task-model")
	}
}

func TestPiExecutor_GetEffectiveModel_RuntimeDefault(t *testing.T) {
	cfg := testPiExecutorConfig()
	cfg.Pi.Model = "config-model"
	pe := NewPiExecutor(cfg)

	task := testResolvedTask("abc123")

	model := pe.GetEffectiveModel(task, "runtime-model")
	if model != "runtime-model" {
		t.Errorf("GetEffectiveModel = %q, want %q", model, "runtime-model")
	}
}

func TestPiExecutor_GetEffectiveModel_ConfigDefault(t *testing.T) {
	cfg := testPiExecutorConfig()
	cfg.Pi.Model = "config-model"
	pe := NewPiExecutor(cfg)

	task := testResolvedTask("abc123")

	model := pe.GetEffectiveModel(task, "")
	if model != "config-model" {
		t.Errorf("GetEffectiveModel = %q, want %q", model, "config-model")
	}
}

func TestPiExecutor_GetEffectiveModel_Precedence(t *testing.T) {
	cfg := testPiExecutorConfig()
	cfg.Pi.Model = "config-model"
	pe := NewPiExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Model = "task-model"

	// Task model takes precedence over runtime default
	model := pe.GetEffectiveModel(task, "runtime-model")
	if model != "task-model" {
		t.Errorf("GetEffectiveModel = %q, want %q (task > runtime > config)", model, "task-model")
	}
}

// =============================================================================
// Spawn Tests (with mock command factory using pi JSONL protocol)
// =============================================================================

func TestPiExecutor_Spawn_CreatesPromptFile(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testPiExecutorConfig()
	cfg.StateDir = stateDir
	cfg.Pi.Bin = "pi"

	pe := NewPiExecutor(cfg)

	// Use a script that emits valid JSONL and agent_end, then exits
	pe.CommandFactory = func(name string, args ...string) *exec.Cmd {
		return exec.Command("bash", "-c", `echo '{"type":"agent_end"}' && sleep 0.1`)
	}

	task := testResolvedTask("abc123")
	task.Path = "projects/test/task/abc123.md"
	workdir := t.TempDir()

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: workdir,
	}

	result, err := pe.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	defer func() {
		if result.Proc != nil {
			result.Proc.Kill(nil)
		}
	}()

	// Verify prompt file was created
	if result.PromptFile == "" {
		t.Fatal("PromptFile is empty")
	}
	content, err := os.ReadFile(result.PromptFile)
	if err != nil {
		t.Fatalf("read prompt file: %v", err)
	}
	if !strings.Contains(string(content), task.Path) {
		t.Errorf("prompt file should contain task path %q, got: %s", task.Path, content)
	}
}

func TestPiExecutor_Spawn_SetsWorkdir(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testPiExecutorConfig()
	cfg.StateDir = stateDir

	pe := NewPiExecutor(cfg)
	pe.CommandFactory = func(name string, args ...string) *exec.Cmd {
		return exec.Command("bash", "-c", `echo '{"type":"agent_end"}' && sleep 0.1`)
	}

	task := testResolvedTask("abc123")
	workdir := t.TempDir()

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: workdir,
	}

	result, err := pe.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	defer func() {
		if result.Proc != nil {
			result.Proc.Kill(nil)
		}
	}()

	if result.Workdir != workdir {
		t.Errorf("result.Workdir = %q, want %q", result.Workdir, workdir)
	}
}

func TestPiExecutor_Spawn_PIDSet(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testPiExecutorConfig()
	cfg.StateDir = stateDir

	pe := NewPiExecutor(cfg)
	pe.CommandFactory = func(name string, args ...string) *exec.Cmd {
		return exec.Command("bash", "-c", `echo '{"type":"agent_end"}' && sleep 0.1`)
	}

	task := testResolvedTask("abc123")

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	result, err := pe.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	defer func() {
		if result.Proc != nil {
			result.Proc.Kill(nil)
		}
	}()

	if result.PID <= 0 {
		t.Errorf("PID = %d, want > 0", result.PID)
	}
}

func TestPiExecutor_Spawn_CommandArgs_WithModel(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testPiExecutorConfig()
	cfg.StateDir = stateDir
	cfg.Pi.Bin = "/usr/local/bin/pi"
	cfg.Pi.Model = "test-pi-model"

	var capturedName string
	var capturedArgs []string

	pe := NewPiExecutor(cfg)
	pe.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedName = name
		capturedArgs = args
		return exec.Command("bash", "-c", `echo '{"type":"agent_end"}' && sleep 0.1`)
	}

	task := testResolvedTask("abc123")

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	result, err := pe.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	defer func() {
		if result.Proc != nil {
			result.Proc.Kill(nil)
		}
	}()

	// Verify command name
	if capturedName != "/usr/local/bin/pi" {
		t.Errorf("command name = %q, want %q", capturedName, "/usr/local/bin/pi")
	}

	// Verify model flag
	modelIdx := indexOf(capturedArgs, "--model")
	if modelIdx < 0 || modelIdx+1 >= len(capturedArgs) || capturedArgs[modelIdx+1] != "test-pi-model" {
		t.Errorf("expected --model test-pi-model in args: %v", capturedArgs)
	}
}

func TestPiExecutor_Spawn_CommandArgs_NoModel(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testPiExecutorConfig()
	cfg.StateDir = stateDir
	cfg.Pi.Bin = "pi"
	cfg.Pi.Model = ""

	var capturedArgs []string

	pe := NewPiExecutor(cfg)
	pe.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.Command("bash", "-c", `echo '{"type":"agent_end"}' && sleep 0.1`)
	}

	task := testResolvedTask("abc123")

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	result, err := pe.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	defer func() {
		if result.Proc != nil {
			result.Proc.Kill(nil)
		}
	}()

	// Should not have --model flag when model is empty
	if indexOf(capturedArgs, "--model") >= 0 {
		t.Errorf("should not have --model flag when model is empty: %v", capturedArgs)
	}
}

func TestPiExecutor_Spawn_TaskModelOverride(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testPiExecutorConfig()
	cfg.StateDir = stateDir
	cfg.Pi.Model = "config-model"

	var capturedArgs []string

	pe := NewPiExecutor(cfg)
	pe.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.Command("bash", "-c", `echo '{"type":"agent_end"}' && sleep 0.1`)
	}

	task := testResolvedTask("abc123")
	task.Model = "task-model"

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	result, err := pe.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	defer func() {
		if result.Proc != nil {
			result.Proc.Kill(nil)
		}
	}()

	// Task-level model should take precedence
	modelIdx := indexOf(capturedArgs, "--model")
	if modelIdx < 0 || capturedArgs[modelIdx+1] != "task-model" {
		t.Errorf("expected --model task-model, got args: %v", capturedArgs)
	}
}

func TestPiExecutor_Spawn_OutputLogFile(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testPiExecutorConfig()
	cfg.StateDir = stateDir

	pe := NewPiExecutor(cfg)
	pe.CommandFactory = func(name string, args ...string) *exec.Cmd {
		return exec.Command("bash", "-c", `echo '{"type":"agent_end"}' && sleep 0.1`)
	}

	task := testResolvedTask("abc123")

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	result, err := pe.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	defer func() {
		if result.Proc != nil {
			result.Proc.Kill(nil)
		}
	}()

	// Verify output log file was created
	outputFile := filepath.Join(stateDir, "output_test-project_abc123.log")
	if _, err := os.Stat(outputFile); err != nil {
		t.Errorf("output log file should exist: %v", err)
	}
}

func TestPiExecutor_Spawn_RuntimeDefaultModel(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testPiExecutorConfig()
	cfg.StateDir = stateDir
	cfg.Pi.Model = "config-model"

	var capturedArgs []string

	pe := NewPiExecutor(cfg)
	pe.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.Command("bash", "-c", `echo '{"type":"agent_end"}' && sleep 0.1`)
	}

	task := testResolvedTask("abc123")
	// No task-level model

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:                ExecutionModeHeadless,
		Workdir:             t.TempDir(),
		RuntimeDefaultModel: "runtime-model",
	}

	result, err := pe.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	defer func() {
		if result.Proc != nil {
			result.Proc.Kill(nil)
		}
	}()

	// Runtime default should take precedence over config
	modelIdx := indexOf(capturedArgs, "--model")
	if modelIdx < 0 || capturedArgs[modelIdx+1] != "runtime-model" {
		t.Errorf("expected --model runtime-model, got args: %v", capturedArgs)
	}
}

// =============================================================================
// Cleanup Tests
// =============================================================================

func TestPiExecutor_Cleanup(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testPiExecutorConfig()
	cfg.StateDir = stateDir
	pe := NewPiExecutor(cfg)

	// Create temp files that cleanup should remove
	files := []string{
		filepath.Join(stateDir, "prompt_proj_task1.txt"),
		filepath.Join(stateDir, "output_proj_task1.log"),
	}
	for _, f := range files {
		if err := os.WriteFile(f, []byte("test"), 0o644); err != nil {
			t.Fatalf("create temp file: %v", err)
		}
	}

	err := pe.Cleanup("task1", "proj")
	if err != nil {
		t.Fatalf("Cleanup returned error: %v", err)
	}

	// Verify all files are removed
	for _, f := range files {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("file %s should have been removed", f)
		}
	}
}

func TestPiExecutor_Cleanup_MissingFiles(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testPiExecutorConfig()
	cfg.StateDir = stateDir
	pe := NewPiExecutor(cfg)

	// Should not error when files don't exist
	err := pe.Cleanup("nonexistent", "proj")
	if err != nil {
		t.Errorf("Cleanup returned error for missing files: %v", err)
	}
}

// =============================================================================
// buildArgs Tests
// =============================================================================

func TestPiExecutor_buildArgs_WithModel(t *testing.T) {
	cfg := testPiExecutorConfig()
	cfg.Pi.Model = "test-model"
	pe := NewPiExecutor(cfg)

	task := testResolvedTask("abc123")
	args := pe.buildArgs(task, "")

	modelIdx := indexOf(args, "--model")
	if modelIdx < 0 || modelIdx+1 >= len(args) || args[modelIdx+1] != "test-model" {
		t.Errorf("expected --model test-model in args: %v", args)
	}
}

func TestPiExecutor_buildArgs_NoModel(t *testing.T) {
	cfg := testPiExecutorConfig()
	cfg.Pi.Model = ""
	pe := NewPiExecutor(cfg)

	task := testResolvedTask("abc123")
	args := pe.buildArgs(task, "")

	if len(args) != 0 {
		t.Errorf("expected empty args when no model set, got: %v", args)
	}
}

func TestPiExecutor_buildArgs_TaskModelOverride(t *testing.T) {
	cfg := testPiExecutorConfig()
	cfg.Pi.Model = "config-model"
	pe := NewPiExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Model = "task-model"
	args := pe.buildArgs(task, "")

	modelIdx := indexOf(args, "--model")
	if modelIdx < 0 || args[modelIdx+1] != "task-model" {
		t.Errorf("expected --model task-model, got args: %v", args)
	}
}
