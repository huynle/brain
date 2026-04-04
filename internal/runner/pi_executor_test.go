package runner

import (
	"context"
	"encoding/json"
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

func testPiConfig() RunnerConfig {
	return RunnerConfig{
		BrainAPIURL: "http://localhost:3333",
		StateDir:    os.TempDir(),
		WorkDir:     "/default/workdir",
		Pi: PiConfig{
			Bin:           "pi",
			Model:         "default-pi-model",
			Thinking:      "",
			AgentsDir:     "/tmp/pi-agents",
			ExtensionsDir: "/tmp/pi-extensions",
			Extensions:    nil,
			NoSession:     true,
		},
	}
}

func testPiResolvedTask(id string) *types.ResolvedTask {
	return &types.ResolvedTask{
		ID:       id,
		Path:     "projects/test/task/" + id + ".md",
		Title:    "Test Task " + id,
		Priority: "medium",
		Status:   "pending",
	}
}

// createAgentBundle creates a temporary agent bundle directory with config.json
// and optional files. Returns the path to the agents dir (parent of the bundle).
func createAgentBundle(t *testing.T, agentName string, cfg agentBundleConfig, files map[string]string) string {
	t.Helper()
	agentsDir := t.TempDir()
	bundleDir := filepath.Join(agentsDir, agentName)
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatalf("create bundle dir: %v", err)
	}

	// Write config.json
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "config.json"), data, 0o644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	// Write additional files
	for name, content := range files {
		filePath := filepath.Join(bundleDir, name)
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			t.Fatalf("create parent dir for %s: %v", name, err)
		}
		if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	return agentsDir
}

// =============================================================================
// Interface Compliance
// =============================================================================

func TestPiExecutor_ImplementsTaskExecutor(t *testing.T) {
	cfg := testPiConfig()
	e := NewPiExecutor(cfg)

	// Compile-time check is in pi_executor.go via var _ TaskExecutor = (*PiExecutor)(nil)
	// This test verifies runtime compliance.
	var _ TaskExecutor = e
}

// =============================================================================
// BuildPrompt Tests (delegates to common)
// =============================================================================

func TestPiExecutor_BuildPrompt_NewTask(t *testing.T) {
	cfg := testPiConfig()
	e := NewPiExecutor(cfg)

	task := testPiResolvedTask("abc123")
	task.Path = "projects/myproj/task/abc123.md"

	prompt := e.BuildPrompt(task, false)
	if !strings.Contains(prompt, "brain-runner-queue") {
		t.Error("new task prompt should reference brain-runner-queue skill")
	}
	if !strings.Contains(prompt, task.Path) {
		t.Errorf("new task prompt should contain task path %q", task.Path)
	}
}

func TestPiExecutor_BuildPrompt_DirectPrompt(t *testing.T) {
	cfg := testPiConfig()
	e := NewPiExecutor(cfg)

	task := testPiResolvedTask("abc123")
	task.DirectPrompt = "Direct instruction"

	prompt := e.BuildPrompt(task, false)
	if prompt != "Direct instruction" {
		t.Errorf("BuildPrompt = %q, want %q", prompt, "Direct instruction")
	}
}

func TestPiExecutor_BuildPrompt_Resume(t *testing.T) {
	cfg := testPiConfig()
	e := NewPiExecutor(cfg)

	task := testPiResolvedTask("abc123")
	prompt := e.BuildPrompt(task, true)
	if !strings.Contains(prompt, "RESUME") {
		t.Error("resume prompt should contain RESUME")
	}
}

// =============================================================================
// ResolveWorkdir Tests (delegates to common)
// =============================================================================

func TestPiExecutor_ResolveWorkdir_TargetWorkdir(t *testing.T) {
	dir := t.TempDir()
	cfg := testPiConfig()
	cfg.WorkDir = "/fallback"
	e := NewPiExecutor(cfg)

	task := testPiResolvedTask("abc123")
	task.TargetWorkdir = dir

	result, err := e.ResolveWorkdir(task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != dir {
		t.Errorf("ResolveWorkdir = %q, want %q", result, dir)
	}
}

func TestPiExecutor_ResolveWorkdir_FallbackToConfig(t *testing.T) {
	cfg := testPiConfig()
	cfg.WorkDir = "/config/default"
	e := NewPiExecutor(cfg)

	task := testPiResolvedTask("abc123")
	result, err := e.ResolveWorkdir(task)
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
	cfg := testPiConfig()
	cfg.Pi.Model = "config-model"
	e := NewPiExecutor(cfg)

	task := testPiResolvedTask("abc123")
	task.Model = "task-model"

	model := e.GetEffectiveModel(task, "runtime-model")
	if model != "task-model" {
		t.Errorf("GetEffectiveModel = %q, want %q (task > runtime > config)", model, "task-model")
	}
}

func TestPiExecutor_GetEffectiveModel_RuntimeDefault(t *testing.T) {
	cfg := testPiConfig()
	cfg.Pi.Model = "config-model"
	e := NewPiExecutor(cfg)

	task := testPiResolvedTask("abc123")

	model := e.GetEffectiveModel(task, "runtime-model")
	if model != "runtime-model" {
		t.Errorf("GetEffectiveModel = %q, want %q", model, "runtime-model")
	}
}

func TestPiExecutor_GetEffectiveModel_ConfigDefault(t *testing.T) {
	cfg := testPiConfig()
	cfg.Pi.Model = "config-model"
	e := NewPiExecutor(cfg)

	task := testPiResolvedTask("abc123")

	model := e.GetEffectiveModel(task, "")
	if model != "config-model" {
		t.Errorf("GetEffectiveModel = %q, want %q", model, "config-model")
	}
}

func TestPiExecutor_GetEffectiveModel_Empty(t *testing.T) {
	cfg := testPiConfig()
	cfg.Pi.Model = ""
	e := NewPiExecutor(cfg)

	task := testPiResolvedTask("abc123")

	model := e.GetEffectiveModel(task, "")
	if model != "" {
		t.Errorf("GetEffectiveModel = %q, want empty", model)
	}
}

// =============================================================================
// resolveAgentArgs Tests
// =============================================================================

func TestPiExecutor_resolveAgentArgs_EmptyAgent(t *testing.T) {
	cfg := testPiConfig()
	e := NewPiExecutor(cfg)

	args := e.resolveAgentArgs("")
	if args != nil {
		t.Errorf("resolveAgentArgs(\"\") = %v, want nil", args)
	}
}

func TestPiExecutor_resolveAgentArgs_NoBundleDir(t *testing.T) {
	cfg := testPiConfig()
	cfg.Pi.AgentsDir = t.TempDir() // empty dir
	e := NewPiExecutor(cfg)

	args := e.resolveAgentArgs("nonexistent")
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d: %v", len(args), args)
	}
	if args[0] != "--append-system-prompt" {
		t.Errorf("args[0] = %q, want --append-system-prompt", args[0])
	}
	if !strings.Contains(args[1], "nonexistent") {
		t.Errorf("args[1] = %q, should mention agent name", args[1])
	}
}

func TestPiExecutor_resolveAgentArgs_BundleWithFullConfig(t *testing.T) {
	agentsDir := createAgentBundle(t, "myagent", agentBundleConfig{
		SystemPromptFile: "system.md",
		Extension:        "extension.ts",
		Thinking:         "high",
		Tools:            []string{"read", "write"},
	}, map[string]string{
		"system.md":    "You are a helpful agent.",
		"extension.ts": "// extension code",
	})

	cfg := testPiConfig()
	cfg.Pi.AgentsDir = agentsDir
	e := NewPiExecutor(cfg)

	args := e.resolveAgentArgs("myagent")

	// Should have --system-prompt-file, -e, --thinking, --tools (x2)
	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "--system-prompt-file") {
		t.Error("expected --system-prompt-file in args")
	}
	if !strings.Contains(argsStr, "-e") {
		t.Error("expected -e in args")
	}
	if !strings.Contains(argsStr, "--thinking high") {
		t.Error("expected --thinking high in args")
	}

	// Count --tools flags
	toolsCount := 0
	for _, a := range args {
		if a == "--tools" {
			toolsCount++
		}
	}
	if toolsCount != 2 {
		t.Errorf("expected 2 --tools flags, got %d", toolsCount)
	}
}

func TestPiExecutor_resolveAgentArgs_BundleMissingFiles(t *testing.T) {
	// Config references files that don't exist in the bundle
	agentsDir := createAgentBundle(t, "sparseagent", agentBundleConfig{
		SystemPromptFile: "nonexistent.md",
		Extension:        "nonexistent.ts",
		Thinking:         "medium",
	}, map[string]string{
		// No files created — only config.json exists
	})

	cfg := testPiConfig()
	cfg.Pi.AgentsDir = agentsDir
	e := NewPiExecutor(cfg)

	args := e.resolveAgentArgs("sparseagent")

	// Should NOT have --system-prompt-file or -e (files don't exist)
	// Should have --thinking
	argsStr := strings.Join(args, " ")
	if strings.Contains(argsStr, "--system-prompt-file") {
		t.Error("should not have --system-prompt-file when file doesn't exist")
	}
	if strings.Contains(argsStr, "-e") {
		t.Error("should not have -e when extension file doesn't exist")
	}
	if !strings.Contains(argsStr, "--thinking medium") {
		t.Error("expected --thinking medium in args")
	}
}

func TestPiExecutor_resolveAgentArgs_BundleMissingConfigJSON(t *testing.T) {
	// Create bundle dir but no config.json
	agentsDir := t.TempDir()
	bundleDir := filepath.Join(agentsDir, "noconfigagent")
	os.MkdirAll(bundleDir, 0o755)

	cfg := testPiConfig()
	cfg.Pi.AgentsDir = agentsDir
	e := NewPiExecutor(cfg)

	args := e.resolveAgentArgs("noconfigagent")

	// Should fall back to --append-system-prompt
	if len(args) != 2 || args[0] != "--append-system-prompt" {
		t.Errorf("expected fallback to --append-system-prompt, got %v", args)
	}
}

func TestPiExecutor_resolveAgentArgs_AbsolutePathInConfig(t *testing.T) {
	// Test that absolute paths in config.json are used as-is
	tmpDir := t.TempDir()
	absPromptPath := filepath.Join(tmpDir, "global-system.md")
	os.WriteFile(absPromptPath, []byte("global prompt"), 0o644)

	agentsDir := createAgentBundle(t, "absagent", agentBundleConfig{
		SystemPromptFile: absPromptPath, // absolute path
	}, map[string]string{})

	cfg := testPiConfig()
	cfg.Pi.AgentsDir = agentsDir
	e := NewPiExecutor(cfg)

	args := e.resolveAgentArgs("absagent")

	// Should have --system-prompt-file with the absolute path
	idx := indexOf(args, "--system-prompt-file")
	if idx < 0 || idx+1 >= len(args) {
		t.Fatal("expected --system-prompt-file in args")
	}
	if args[idx+1] != absPromptPath {
		t.Errorf("system-prompt-file = %q, want %q", args[idx+1], absPromptPath)
	}
}

// =============================================================================
// buildExtensionArgs Tests
// =============================================================================

func TestPiExecutor_buildExtensionArgs_NoExtensions(t *testing.T) {
	cfg := testPiConfig()
	cfg.Pi.Extensions = nil
	e := NewPiExecutor(cfg)

	task := testPiResolvedTask("abc123")
	args := e.buildExtensionArgs(task)

	if len(args) != 0 {
		t.Errorf("expected 0 args, got %d: %v", len(args), args)
	}
}

func TestPiExecutor_buildExtensionArgs_ConfigExtensions(t *testing.T) {
	cfg := testPiConfig()
	cfg.Pi.Extensions = []string{"/path/to/always-on.ts", "/path/to/logging.ts"}
	e := NewPiExecutor(cfg)

	task := testPiResolvedTask("abc123")
	args := e.buildExtensionArgs(task)

	if len(args) != 4 { // 2 extensions * 2 args each (-e, path)
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}
	if args[0] != "-e" || args[1] != "/path/to/always-on.ts" {
		t.Errorf("first extension = %v, want -e /path/to/always-on.ts", args[:2])
	}
	if args[2] != "-e" || args[3] != "/path/to/logging.ts" {
		t.Errorf("second extension = %v, want -e /path/to/logging.ts", args[2:4])
	}
}

func TestPiExecutor_buildExtensionArgs_TaskExtensions_ShortName(t *testing.T) {
	cfg := testPiConfig()
	cfg.Pi.ExtensionsDir = "/home/user/.pi/extensions"
	cfg.Pi.Extensions = nil
	e := NewPiExecutor(cfg)

	task := testPiResolvedTask("abc123")
	task.Extensions = []string{"code-review", "linting"}

	args := e.buildExtensionArgs(task)

	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}
	if args[1] != "/home/user/.pi/extensions/brain-code-review.ts" {
		t.Errorf("resolved ext = %q, want brain-code-review.ts path", args[1])
	}
	if args[3] != "/home/user/.pi/extensions/brain-linting.ts" {
		t.Errorf("resolved ext = %q, want brain-linting.ts path", args[3])
	}
}

func TestPiExecutor_buildExtensionArgs_TaskExtensions_FullPath(t *testing.T) {
	cfg := testPiConfig()
	cfg.Pi.Extensions = nil
	e := NewPiExecutor(cfg)

	task := testPiResolvedTask("abc123")
	task.Extensions = []string{"/absolute/path/to/ext.ts", "relative/path/ext.ts"}

	args := e.buildExtensionArgs(task)

	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}
	// Paths containing "/" should be used as-is
	if args[1] != "/absolute/path/to/ext.ts" {
		t.Errorf("absolute ext = %q, want as-is", args[1])
	}
	if args[3] != "relative/path/ext.ts" {
		t.Errorf("relative ext = %q, want as-is", args[3])
	}
}

func TestPiExecutor_buildExtensionArgs_AllThreeLayers(t *testing.T) {
	cfg := testPiConfig()
	cfg.Pi.Extensions = []string{"/config/always-on.ts"}
	cfg.Pi.ExtensionsDir = "/home/user/.pi/extensions"
	e := NewPiExecutor(cfg)

	task := testPiResolvedTask("abc123")
	task.Extensions = []string{"code-review"}

	args := e.buildExtensionArgs(task)

	// Layer 2 (config) + Layer 3 (task) = 2 extensions
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}
	// Layer 2 first
	if args[1] != "/config/always-on.ts" {
		t.Errorf("config ext = %q, want /config/always-on.ts", args[1])
	}
	// Layer 3 second (resolved short name)
	if args[3] != "/home/user/.pi/extensions/brain-code-review.ts" {
		t.Errorf("task ext = %q, want brain-code-review.ts path", args[3])
	}
}

func TestPiExecutor_buildExtensionArgs_EmptyStringsSkipped(t *testing.T) {
	cfg := testPiConfig()
	cfg.Pi.Extensions = []string{"", "/valid.ts", ""}
	e := NewPiExecutor(cfg)

	task := testPiResolvedTask("abc123")
	task.Extensions = []string{"", "review"}

	args := e.buildExtensionArgs(task)

	// Should only have the non-empty extensions
	eCount := 0
	for _, a := range args {
		if a == "-e" {
			eCount++
		}
	}
	if eCount != 2 { // /valid.ts + brain-review.ts
		t.Errorf("expected 2 -e flags, got %d: %v", eCount, args)
	}
}

func TestPiExecutor_buildExtensionArgs_NilTask(t *testing.T) {
	cfg := testPiConfig()
	cfg.Pi.Extensions = []string{"/always.ts"}
	e := NewPiExecutor(cfg)

	args := e.buildExtensionArgs(nil)

	// Should still include config extensions
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d: %v", len(args), args)
	}
}

// =============================================================================
// resolveExtensionPath Tests
// =============================================================================

func TestPiExecutor_resolveExtensionPath_ShortName(t *testing.T) {
	cfg := testPiConfig()
	cfg.Pi.ExtensionsDir = "/home/user/.pi/extensions"
	e := NewPiExecutor(cfg)

	result := e.resolveExtensionPath("code-review")
	expected := "/home/user/.pi/extensions/brain-code-review.ts"
	if result != expected {
		t.Errorf("resolveExtensionPath = %q, want %q", result, expected)
	}
}

func TestPiExecutor_resolveExtensionPath_AbsolutePath(t *testing.T) {
	cfg := testPiConfig()
	e := NewPiExecutor(cfg)

	result := e.resolveExtensionPath("/absolute/path/ext.ts")
	if result != "/absolute/path/ext.ts" {
		t.Errorf("resolveExtensionPath = %q, want as-is", result)
	}
}

func TestPiExecutor_resolveExtensionPath_RelativePath(t *testing.T) {
	cfg := testPiConfig()
	e := NewPiExecutor(cfg)

	result := e.resolveExtensionPath("relative/path/ext.ts")
	if result != "relative/path/ext.ts" {
		t.Errorf("resolveExtensionPath = %q, want as-is for paths with /", result)
	}
}

// =============================================================================
// Headless Spawn Tests (with mock command factory)
// =============================================================================

func TestPiExecutor_SpawnHeadless_CommandArgs(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testPiConfig()
	cfg.StateDir = stateDir
	cfg.Pi.Bin = "/usr/local/bin/pi"
	cfg.Pi.Model = "test-model"
	cfg.Pi.NoSession = true
	cfg.Pi.Extensions = nil

	var capturedName string
	var capturedArgs []string

	e := NewPiExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedName = name
		capturedArgs = args
		// Return a command that reads stdin and exits
		return exec.Command("/bin/cat")
	}

	task := testPiResolvedTask("abc123")
	workdir := t.TempDir()

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: workdir,
	}

	result, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Verify command name
	if capturedName != "/usr/local/bin/pi" {
		t.Errorf("command name = %q, want %q", capturedName, "/usr/local/bin/pi")
	}

	// Verify --mode rpc is first
	if len(capturedArgs) < 2 || capturedArgs[0] != "--mode" || capturedArgs[1] != "rpc" {
		t.Errorf("expected first args to be --mode rpc, got %v", capturedArgs)
	}

	// Verify --model flag
	modelIdx := indexOf(capturedArgs, "--model")
	if modelIdx < 0 || modelIdx+1 >= len(capturedArgs) || capturedArgs[modelIdx+1] != "test-model" {
		t.Errorf("expected --model test-model in args: %v", capturedArgs)
	}

	// Verify --no-session flag
	if indexOf(capturedArgs, "--no-session") < 0 {
		t.Errorf("expected --no-session in args: %v", capturedArgs)
	}

	// Verify prompt file was created
	if result.PromptFile == "" {
		t.Error("PromptFile is empty")
	}

	// Verify PID is set
	if result.PID <= 0 {
		t.Errorf("PID = %d, want > 0", result.PID)
	}

	// Verify workdir
	if result.Workdir != workdir {
		t.Errorf("Workdir = %q, want %q", result.Workdir, workdir)
	}
}

func TestPiExecutor_SpawnHeadless_WithAgent(t *testing.T) {
	stateDir := t.TempDir()

	// Create agent bundle
	agentsDir := createAgentBundle(t, "tdd-dev", agentBundleConfig{
		SystemPromptFile: "system.md",
		Thinking:         "high",
		Tools:            []string{"read"},
	}, map[string]string{
		"system.md": "You are a TDD developer.",
	})

	cfg := testPiConfig()
	cfg.StateDir = stateDir
	cfg.Pi.Bin = "pi"
	cfg.Pi.AgentsDir = agentsDir
	cfg.Pi.NoSession = false

	var capturedArgs []string

	e := NewPiExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.Command("/bin/cat")
	}

	task := testPiResolvedTask("abc123")
	task.Agent = "tdd-dev"

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	argsStr := strings.Join(capturedArgs, " ")

	// Should have --system-prompt-file
	if !strings.Contains(argsStr, "--system-prompt-file") {
		t.Error("expected --system-prompt-file in args")
	}

	// Should have --thinking high
	if !strings.Contains(argsStr, "--thinking high") {
		t.Error("expected --thinking high in args")
	}

	// Should have --tools read
	if !strings.Contains(argsStr, "--tools read") {
		t.Error("expected --tools read in args")
	}

	// Should NOT have --no-session (NoSession is false)
	if indexOf(capturedArgs, "--no-session") >= 0 {
		t.Error("should not have --no-session when NoSession is false")
	}
}

func TestPiExecutor_SpawnHeadless_TaskModelOverride(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testPiConfig()
	cfg.StateDir = stateDir
	cfg.Pi.Model = "config-model"

	var capturedArgs []string

	e := NewPiExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.Command("/bin/cat")
	}

	task := testPiResolvedTask("abc123")
	task.Model = "task-model"

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	modelIdx := indexOf(capturedArgs, "--model")
	if modelIdx < 0 || capturedArgs[modelIdx+1] != "task-model" {
		t.Errorf("expected --model task-model, got args: %v", capturedArgs)
	}
}

func TestPiExecutor_SpawnHeadless_RuntimeDefaultModel(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testPiConfig()
	cfg.StateDir = stateDir
	cfg.Pi.Model = "config-model"

	var capturedArgs []string

	e := NewPiExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.Command("/bin/cat")
	}

	task := testPiResolvedTask("abc123")
	// No task-level model

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:                ExecutionModeHeadless,
		Workdir:             t.TempDir(),
		RuntimeDefaultModel: "runtime-model",
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	modelIdx := indexOf(capturedArgs, "--model")
	if modelIdx < 0 || capturedArgs[modelIdx+1] != "runtime-model" {
		t.Errorf("expected --model runtime-model, got args: %v", capturedArgs)
	}
}

func TestPiExecutor_SpawnHeadless_WithExtensions(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testPiConfig()
	cfg.StateDir = stateDir
	cfg.Pi.Extensions = []string{"/config/always.ts"}
	cfg.Pi.ExtensionsDir = "/home/user/.pi/extensions"

	var capturedArgs []string

	e := NewPiExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.Command("/bin/cat")
	}

	task := testPiResolvedTask("abc123")
	task.Extensions = []string{"code-review"}

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Count -e flags
	eCount := 0
	for i, a := range capturedArgs {
		if a == "-e" {
			eCount++
			// Verify the extension paths
			if i+1 < len(capturedArgs) {
				ext := capturedArgs[i+1]
				if ext != "/config/always.ts" && ext != "/home/user/.pi/extensions/brain-code-review.ts" {
					t.Errorf("unexpected extension path: %q", ext)
				}
			}
		}
	}
	if eCount != 2 {
		t.Errorf("expected 2 -e flags (config + task), got %d: %v", eCount, capturedArgs)
	}
}

func TestPiExecutor_SpawnHeadless_NoModelNoAgent(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testPiConfig()
	cfg.StateDir = stateDir
	cfg.Pi.Model = ""
	cfg.Pi.NoSession = false

	var capturedArgs []string

	e := NewPiExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.Command("/bin/cat")
	}

	task := testPiResolvedTask("abc123")

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Should not have --model flag
	if indexOf(capturedArgs, "--model") >= 0 {
		t.Errorf("should not have --model when model is empty: %v", capturedArgs)
	}
	// Should not have --no-session
	if indexOf(capturedArgs, "--no-session") >= 0 {
		t.Errorf("should not have --no-session when NoSession is false: %v", capturedArgs)
	}
	// Should still have --mode rpc
	if capturedArgs[0] != "--mode" || capturedArgs[1] != "rpc" {
		t.Errorf("expected --mode rpc: %v", capturedArgs)
	}
}

func TestPiExecutor_SpawnHeadless_OutputLogFile(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testPiConfig()
	cfg.StateDir = stateDir

	e := NewPiExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		return exec.Command("/bin/cat")
	}

	task := testPiResolvedTask("abc123")

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	outputFile := filepath.Join(stateDir, "output_test-project_abc123.log")
	if _, err := os.Stat(outputFile); err != nil {
		t.Errorf("output log file should exist: %v", err)
	}
}

// =============================================================================
// Spawn with unknown mode
// =============================================================================

func TestPiExecutor_Spawn_UnknownMode(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testPiConfig()
	cfg.StateDir = stateDir
	e := NewPiExecutor(cfg)

	task := testPiResolvedTask("abc123")
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
// Cleanup Tests
// =============================================================================

func TestPiExecutor_Cleanup(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testPiConfig()
	cfg.StateDir = stateDir
	e := NewPiExecutor(cfg)

	// Create temp files
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

	for _, f := range files {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("file %s should have been removed", f)
		}
	}
}

func TestPiExecutor_Cleanup_MissingFiles(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testPiConfig()
	cfg.StateDir = stateDir
	e := NewPiExecutor(cfg)

	err := e.Cleanup("nonexistent", "proj")
	if err != nil {
		t.Errorf("Cleanup returned error for missing files: %v", err)
	}
}

// =============================================================================
// buildHeadlessArgs Tests (comprehensive arg ordering)
// =============================================================================

func TestPiExecutor_buildHeadlessArgs_FullArgs(t *testing.T) {
	agentsDir := createAgentBundle(t, "myagent", agentBundleConfig{
		SystemPromptFile: "system.md",
		Extension:        "ext.ts",
		Thinking:         "high",
		Tools:            []string{"read"},
	}, map[string]string{
		"system.md": "prompt",
		"ext.ts":    "code",
	})

	cfg := testPiConfig()
	cfg.Pi.AgentsDir = agentsDir
	cfg.Pi.Extensions = []string{"/config/ext.ts"}
	cfg.Pi.ExtensionsDir = "/ext"
	cfg.Pi.Model = "my-model"
	cfg.Pi.NoSession = true
	e := NewPiExecutor(cfg)

	task := testPiResolvedTask("abc123")
	task.Agent = "myagent"
	task.Extensions = []string{"review"}

	args := e.buildHeadlessArgs(task, "")

	// Verify ordering: --mode rpc, agent args, extension args, --model, --no-session
	if args[0] != "--mode" || args[1] != "rpc" {
		t.Errorf("expected --mode rpc first, got %v", args[:2])
	}

	if indexOf(args, "--no-session") < 0 {
		t.Error("expected --no-session")
	}
	if indexOf(args, "--model") < 0 {
		t.Error("expected --model")
	}

	// Verify we have agent args (--system-prompt-file, -e from bundle, --thinking, --tools)
	if indexOf(args, "--system-prompt-file") < 0 {
		t.Error("expected --system-prompt-file from agent bundle")
	}
	if indexOf(args, "--thinking") < 0 {
		t.Error("expected --thinking from agent bundle")
	}

	// Count total -e flags: 1 from agent + 1 from config + 1 from task = 3
	eCount := 0
	for _, a := range args {
		if a == "-e" {
			eCount++
		}
	}
	if eCount != 3 {
		t.Errorf("expected 3 -e flags (agent + config + task), got %d: %v", eCount, args)
	}
}
