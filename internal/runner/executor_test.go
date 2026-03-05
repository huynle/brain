package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Test Helpers
// =============================================================================

func testResolvedTask(id string) *types.ResolvedTask {
	return &types.ResolvedTask{
		ID:       id,
		Path:     "projects/test/task/" + id + ".md",
		Title:    "Test Task " + id,
		Priority: "medium",
		Status:   "pending",
	}
}

func testExecutorConfig() RunnerConfig {
	return RunnerConfig{
		BrainAPIURL: "http://localhost:3333",
		StateDir:    os.TempDir(),
		WorkDir:     "/default/workdir",
		Opencode: OpencodeConfig{
			Bin:   "opencode",
			Agent: "default-agent",
			Model: "default-model",
		},
	}
}

// =============================================================================
// BuildPrompt Tests
// =============================================================================

func TestExecutor_BuildPrompt_DirectPrompt(t *testing.T) {
	cfg := testExecutorConfig()
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.DirectPrompt = "Do this specific thing verbatim"

	prompt := e.BuildPrompt(task, false)
	if prompt != "Do this specific thing verbatim" {
		t.Errorf("BuildPrompt with direct_prompt = %q, want %q", prompt, "Do this specific thing verbatim")
	}
}

func TestExecutor_BuildPrompt_DirectPrompt_IgnoresResume(t *testing.T) {
	cfg := testExecutorConfig()
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.DirectPrompt = "Do this specific thing"

	// Even with isResume=true, direct_prompt should be used verbatim
	prompt := e.BuildPrompt(task, true)
	if prompt != "Do this specific thing" {
		t.Errorf("BuildPrompt with direct_prompt and isResume = %q, want %q", prompt, "Do this specific thing")
	}
}

func TestExecutor_BuildPrompt_NewTask(t *testing.T) {
	cfg := testExecutorConfig()
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Path = "projects/myproj/task/abc123.md"

	prompt := e.BuildPrompt(task, false)

	if !strings.Contains(prompt, "brain-runner-queue") {
		t.Error("new task prompt should reference brain-runner-queue skill")
	}
	if !strings.Contains(prompt, task.Path) {
		t.Errorf("new task prompt should contain task path %q", task.Path)
	}
	if strings.Contains(prompt, "RESUME") {
		t.Error("new task prompt should not contain RESUME")
	}
}

func TestExecutor_BuildPrompt_ResumeTask(t *testing.T) {
	cfg := testExecutorConfig()
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Path = "projects/myproj/task/abc123.md"

	prompt := e.BuildPrompt(task, true)

	if !strings.Contains(prompt, "RESUME") {
		t.Error("resume prompt should contain RESUME")
	}
	if !strings.Contains(prompt, task.Path) {
		t.Errorf("resume prompt should contain task path %q", task.Path)
	}
	if !strings.Contains(prompt, "brain-runner-queue") {
		t.Error("resume prompt should reference brain-runner-queue skill")
	}
}

// =============================================================================
// ResolveWorkdir Tests
// =============================================================================

func TestExecutor_ResolveWorkdir_TargetWorkdir(t *testing.T) {
	dir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.WorkDir = "/fallback"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.TargetWorkdir = dir // exists

	result := e.ResolveWorkdir(task)
	if result != dir {
		t.Errorf("ResolveWorkdir = %q, want %q (target_workdir)", result, dir)
	}
}

func TestExecutor_ResolveWorkdir_TargetWorkdir_NotExists(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.WorkDir = "/fallback"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.TargetWorkdir = "/nonexistent/path"

	result := e.ResolveWorkdir(task)
	if result == "/nonexistent/path" {
		t.Error("ResolveWorkdir should not return nonexistent target_workdir")
	}
}

func TestExecutor_ResolveWorkdir_ResolvedWorkdir(t *testing.T) {
	dir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.WorkDir = "/fallback"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.ResolvedWorkdir = dir

	result := e.ResolveWorkdir(task)
	if result != dir {
		t.Errorf("ResolveWorkdir = %q, want %q (resolved_workdir)", result, dir)
	}
}

func TestExecutor_ResolveWorkdir_FallbackToConfig(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.WorkDir = "/config/default"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	// No workdir fields set

	result := e.ResolveWorkdir(task)
	if result != "/config/default" {
		t.Errorf("ResolveWorkdir = %q, want %q (config default)", result, "/config/default")
	}
}

func TestExecutor_ResolveWorkdir_Priority_TargetOverResolved(t *testing.T) {
	targetDir := t.TempDir()
	resolvedDir := t.TempDir()
	cfg := testExecutorConfig()
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.TargetWorkdir = targetDir
	task.ResolvedWorkdir = resolvedDir

	result := e.ResolveWorkdir(task)
	if result != targetDir {
		t.Errorf("ResolveWorkdir = %q, want %q (target_workdir takes priority over resolved_workdir)", result, targetDir)
	}
}

// =============================================================================
// GetEffectiveAgent / GetEffectiveModel Tests
// =============================================================================

func TestExecutor_GetEffectiveAgent_TaskOverride(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.Opencode.Agent = "config-agent"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Agent = "task-agent"

	agent := e.GetEffectiveAgent(task)
	if agent != "task-agent" {
		t.Errorf("GetEffectiveAgent = %q, want %q", agent, "task-agent")
	}
}

func TestExecutor_GetEffectiveAgent_ConfigDefault(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.Opencode.Agent = "config-agent"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	// No agent override

	agent := e.GetEffectiveAgent(task)
	if agent != "config-agent" {
		t.Errorf("GetEffectiveAgent = %q, want %q", agent, "config-agent")
	}
}

func TestExecutor_GetEffectiveModel_TaskOverride(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.Opencode.Model = "config-model"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Model = "task-model"

	model := e.GetEffectiveModel(task, "")
	if model != "task-model" {
		t.Errorf("GetEffectiveModel = %q, want %q", model, "task-model")
	}
}

func TestExecutor_GetEffectiveModel_RuntimeDefault(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.Opencode.Model = "config-model"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	// No task model

	model := e.GetEffectiveModel(task, "runtime-model")
	if model != "runtime-model" {
		t.Errorf("GetEffectiveModel = %q, want %q", model, "runtime-model")
	}
}

func TestExecutor_GetEffectiveModel_ConfigDefault(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.Opencode.Model = "config-model"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")

	model := e.GetEffectiveModel(task, "")
	if model != "config-model" {
		t.Errorf("GetEffectiveModel = %q, want %q", model, "config-model")
	}
}

func TestExecutor_GetEffectiveModel_Precedence(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.Opencode.Model = "config-model"
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	task.Model = "task-model"

	// Task model takes precedence over runtime default
	model := e.GetEffectiveModel(task, "runtime-model")
	if model != "task-model" {
		t.Errorf("GetEffectiveModel = %q, want %q (task > runtime > config)", model, "task-model")
	}
}

// =============================================================================
// Background Spawn Tests (with mock command factory)
// =============================================================================

func TestExecutor_SpawnBackground_CommandArgs(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	cfg.Opencode.Bin = "/usr/local/bin/opencode"
	cfg.Opencode.Agent = "test-agent"
	cfg.Opencode.Model = "test-model"

	var capturedName string
	var capturedArgs []string

	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedName = name
		capturedArgs = args
		return exec.Command("/bin/echo", "mock")
	}

	task := testResolvedTask("abc123")
	workdir := t.TempDir()

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeBackground,
		Workdir: workdir,
	}

	result, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Verify command name
	if capturedName != "/usr/local/bin/opencode" {
		t.Errorf("command name = %q, want %q", capturedName, "/usr/local/bin/opencode")
	}

	// Verify args contain "run"
	if len(capturedArgs) == 0 || capturedArgs[0] != "run" {
		t.Errorf("first arg = %v, want 'run'", capturedArgs)
	}

	// Verify agent flag
	agentIdx := indexOf(capturedArgs, "--agent")
	if agentIdx < 0 || agentIdx+1 >= len(capturedArgs) || capturedArgs[agentIdx+1] != "test-agent" {
		t.Errorf("expected --agent test-agent in args: %v", capturedArgs)
	}

	// Verify model flag
	modelIdx := indexOf(capturedArgs, "--model")
	if modelIdx < 0 || modelIdx+1 >= len(capturedArgs) || capturedArgs[modelIdx+1] != "test-model" {
		t.Errorf("expected --model test-model in args: %v", capturedArgs)
	}

	// Verify prompt file was created
	if result.PromptFile == "" {
		t.Error("PromptFile is empty")
	}
	if _, err := os.Stat(result.PromptFile); err != nil {
		t.Errorf("prompt file does not exist: %v", err)
	}

	// Verify PID is set (from the mock process)
	if result.PID <= 0 {
		t.Errorf("PID = %d, want > 0", result.PID)
	}
}

func TestExecutor_SpawnBackground_TaskAgentOverride(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	cfg.Opencode.Agent = "config-agent"
	cfg.Opencode.Model = "config-model"

	var capturedArgs []string

	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.Command("/bin/echo", "mock")
	}

	task := testResolvedTask("abc123")
	task.Agent = "task-agent"
	task.Model = "task-model"

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeBackground,
		Workdir: t.TempDir(),
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Verify task-level agent override
	agentIdx := indexOf(capturedArgs, "--agent")
	if agentIdx < 0 || capturedArgs[agentIdx+1] != "task-agent" {
		t.Errorf("expected --agent task-agent, got args: %v", capturedArgs)
	}

	// Verify task-level model override
	modelIdx := indexOf(capturedArgs, "--model")
	if modelIdx < 0 || capturedArgs[modelIdx+1] != "task-model" {
		t.Errorf("expected --model task-model, got args: %v", capturedArgs)
	}
}

func TestExecutor_SpawnBackground_NoAgentNoModel(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	cfg.Opencode.Agent = ""
	cfg.Opencode.Model = ""

	var capturedArgs []string

	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.Command("/bin/echo", "mock")
	}

	task := testResolvedTask("abc123")

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeBackground,
		Workdir: t.TempDir(),
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Should not have --agent or --model flags
	if indexOf(capturedArgs, "--agent") >= 0 {
		t.Errorf("should not have --agent flag when agent is empty: %v", capturedArgs)
	}
	if indexOf(capturedArgs, "--model") >= 0 {
		t.Errorf("should not have --model flag when model is empty: %v", capturedArgs)
	}
}

func TestExecutor_SpawnBackground_RuntimeDefaultModel(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	cfg.Opencode.Model = "config-model"

	var capturedArgs []string

	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.Command("/bin/echo", "mock")
	}

	task := testResolvedTask("abc123")
	// No task-level model

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:                ExecutionModeBackground,
		Workdir:             t.TempDir(),
		RuntimeDefaultModel: "runtime-model",
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Runtime default should take precedence over config
	modelIdx := indexOf(capturedArgs, "--model")
	if modelIdx < 0 || capturedArgs[modelIdx+1] != "runtime-model" {
		t.Errorf("expected --model runtime-model, got args: %v", capturedArgs)
	}
}

func TestExecutor_SpawnBackground_PromptContent(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir

	var capturedArgs []string

	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.Command("/bin/echo", "mock")
	}

	task := testResolvedTask("abc123")
	task.Path = "projects/test/task/abc123.md"

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeBackground,
		Workdir: t.TempDir(),
	}

	result, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Verify prompt file content
	content, err := os.ReadFile(result.PromptFile)
	if err != nil {
		t.Fatalf("read prompt file: %v", err)
	}
	if !strings.Contains(string(content), task.Path) {
		t.Errorf("prompt file should contain task path %q, got: %s", task.Path, content)
	}

	// Last arg should be the prompt content (read from file)
	lastArg := capturedArgs[len(capturedArgs)-1]
	if !strings.Contains(lastArg, task.Path) {
		t.Errorf("last arg should contain task path, got: %q", lastArg)
	}
}

func TestExecutor_SpawnBackground_OutputLogFile(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir

	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		return exec.Command("/bin/echo", "mock")
	}

	task := testResolvedTask("abc123")

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeBackground,
		Workdir: t.TempDir(),
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Verify output log file was created
	outputFile := filepath.Join(stateDir, "output_test-project_abc123.log")
	if _, err := os.Stat(outputFile); err != nil {
		t.Errorf("output log file should exist: %v", err)
	}
}

func TestExecutor_SpawnBackground_WorkdirResolution(t *testing.T) {
	stateDir := t.TempDir()
	workdir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	cfg.WorkDir = "/fallback"

	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		return exec.Command("/bin/echo", "mock")
	}

	task := testResolvedTask("abc123")
	task.TargetWorkdir = workdir

	ctx := context.Background()
	opts := SpawnOptions{
		Mode: ExecutionModeBackground,
		// No explicit workdir — should resolve from task
	}

	result, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// The workdir should have been resolved to the task's target_workdir
	if result.Workdir != workdir {
		t.Errorf("result.Workdir = %q, want %q", result.Workdir, workdir)
	}
}

// =============================================================================
// Cleanup Tests
// =============================================================================

func TestExecutor_Cleanup(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	e := NewExecutor(cfg)

	// Create temp files that cleanup should remove
	files := []string{
		filepath.Join(stateDir, "prompt_proj_task1.txt"),
		filepath.Join(stateDir, "runner_proj_task1.sh"),
		filepath.Join(stateDir, "output_proj_task1.log"),
	}
	for _, f := range files {
		if err := os.WriteFile(f, []byte("test"), 0o644); err != nil {
			t.Fatalf("create temp file: %v", err)
		}
	}

	err := e.Cleanup("task1", "proj")
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

func TestExecutor_Cleanup_MissingFiles(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	e := NewExecutor(cfg)

	// Should not error when files don't exist
	err := e.Cleanup("nonexistent", "proj")
	if err != nil {
		t.Errorf("Cleanup returned error for missing files: %v", err)
	}
}

// =============================================================================
// ParseLsofOutput Tests
// =============================================================================

func TestParseLsofOutput_ValidOutput(t *testing.T) {
	output := `COMMAND   PID USER   FD   TYPE DEVICE SIZE/OFF NODE NAME
opencode 1234 user   12u  IPv4 0x1234 0t0  TCP *:52341 (LISTEN)
opencode 1234 user   13u  IPv4 0x5678 0t0  TCP 127.0.0.1:52341->127.0.0.1:52342 (ESTABLISHED)
`
	port, err := ParseLsofOutput(output)
	if err != nil {
		t.Fatalf("ParseLsofOutput returned error: %v", err)
	}
	if port != 52341 {
		t.Errorf("ParseLsofOutput = %d, want 52341", port)
	}
}

func TestParseLsofOutput_NoListenPort(t *testing.T) {
	output := `COMMAND   PID USER   FD   TYPE DEVICE SIZE/OFF NODE NAME
opencode 1234 user   13u  IPv4 0x5678 0t0  TCP 127.0.0.1:52341->127.0.0.1:52342 (ESTABLISHED)
`
	_, err := ParseLsofOutput(output)
	if err == nil {
		t.Error("ParseLsofOutput should return error when no LISTEN port found")
	}
}

func TestParseLsofOutput_EmptyOutput(t *testing.T) {
	_, err := ParseLsofOutput("")
	if err == nil {
		t.Error("ParseLsofOutput should return error for empty output")
	}
}

func TestParseLsofOutput_MultipleListenPorts(t *testing.T) {
	output := `COMMAND   PID USER   FD   TYPE DEVICE SIZE/OFF NODE NAME
opencode 1234 user   12u  IPv4 0x1234 0t0  TCP *:52341 (LISTEN)
opencode 1234 user   14u  IPv6 0x9abc 0t0  TCP *:52342 (LISTEN)
`
	// Should return the first LISTEN port
	port, err := ParseLsofOutput(output)
	if err != nil {
		t.Fatalf("ParseLsofOutput returned error: %v", err)
	}
	if port != 52341 {
		t.Errorf("ParseLsofOutput = %d, want 52341 (first LISTEN port)", port)
	}
}

func TestParseLsofOutput_IPv6Wildcard(t *testing.T) {
	output := `COMMAND   PID USER   FD   TYPE DEVICE SIZE/OFF NODE NAME
opencode 1234 user   12u  IPv6 0x1234 0t0  TCP [::]:8080 (LISTEN)
`
	port, err := ParseLsofOutput(output)
	if err != nil {
		t.Fatalf("ParseLsofOutput returned error: %v", err)
	}
	if port != 8080 {
		t.Errorf("ParseLsofOutput = %d, want 8080", port)
	}
}

func TestParseLsofOutput_LocalhostBind(t *testing.T) {
	output := `COMMAND   PID USER   FD   TYPE DEVICE SIZE/OFF NODE NAME
opencode 1234 user   12u  IPv4 0x1234 0t0  TCP 127.0.0.1:3000 (LISTEN)
`
	port, err := ParseLsofOutput(output)
	if err != nil {
		t.Fatalf("ParseLsofOutput returned error: %v", err)
	}
	if port != 3000 {
		t.Errorf("ParseLsofOutput = %d, want 3000", port)
	}
}

// =============================================================================
// IsPidAlive Tests
// =============================================================================

func TestIsPidAlive_CurrentProcess(t *testing.T) {
	if !IsPidAlive(os.Getpid()) {
		t.Error("IsPidAlive should return true for current process")
	}
}

func TestIsPidAlive_DeadProcess(t *testing.T) {
	if IsPidAlive(99999999) {
		t.Error("IsPidAlive should return false for nonexistent PID")
	}
}

func TestIsPidAlive_ZeroPid(t *testing.T) {
	if IsPidAlive(0) {
		t.Error("IsPidAlive should return false for PID 0")
	}
}

func TestIsPidAlive_NegativePid(t *testing.T) {
	if IsPidAlive(-1) {
		t.Error("IsPidAlive should return false for negative PID")
	}
}

// =============================================================================
// SpawnOptions / SpawnResult Types Tests
// =============================================================================

func TestSpawnOptions_Defaults(t *testing.T) {
	opts := SpawnOptions{
		Mode: ExecutionModeBackground,
	}
	if opts.IsResume {
		t.Error("IsResume should default to false")
	}
	if opts.Workdir != "" {
		t.Error("Workdir should default to empty")
	}
}

// =============================================================================
// Spawn with unknown mode
// =============================================================================

func TestExecutor_Spawn_UnknownMode(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	e := NewExecutor(cfg)

	task := testResolvedTask("abc123")
	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    "unknown_mode",
		Workdir: t.TempDir(),
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err == nil {
		t.Error("Spawn should return error for unknown mode")
	}
}

// =============================================================================
// Helpers
// =============================================================================

func indexOf(slice []string, item string) int {
	for i, s := range slice {
		if s == item {
			return i
		}
	}
	return -1
}
