package runner

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Integration Tests: Full Pi Executor Pipeline
//
// These tests verify the end-to-end flow from task creation through executor
// routing, agent bundle resolution, extension composition, spawning, and
// completion detection, exercising multiple components together.
// =============================================================================

// TestIntegration_ExecutorRouting_PiTask verifies that a task with executor:"pi"
// routes to PiExecutor, builds correct args with agent bundle + extensions,
// and spawns successfully through the entire pipeline.
func TestIntegration_ExecutorRouting_PiTask(t *testing.T) {
	// Setup: agent bundle with system prompt + extensions
	agentsDir := createAgentBundle(t, "tdd-dev", agentBundleConfig{
		SystemPromptFile: "system.md",
		Extension:        "agent-ext.ts",
		Thinking:         "high",
		Tools:            []string{"read", "write"},
	}, map[string]string{
		"system.md":    "You are a TDD developer agent.",
		"agent-ext.ts": "// agent extension",
	})

	cfg := RunnerConfig{
		BrainAPIURL:            "http://localhost:3333",
		PollInterval:           30,
		MaxParallel:            2,
		MemoryThresholdPercent: 10,
		IdleDetectionThreshold: 60000,
		APITimeout:             5000,
		StateDir:               t.TempDir(),
		WorkDir:                t.TempDir(),
		Pi: PiConfig{
			Bin:           "pi",
			Model:         "config-model",
			AgentsDir:     agentsDir,
			ExtensionsDir: "/home/user/.pi/extensions",
			Extensions:    []string{"/config/always-on.ts"},
			NoSession:     true,
		},
		DefaultExecutor: "opencode",
	}

	// Create registry
	reg := NewExecutorRegistry(cfg)

	// Verify registry has both executors
	names := reg.Names()
	if len(names) != 2 {
		t.Fatalf("expected 2 executors, got %d: %v", len(names), names)
	}

	// Create task with Pi executor
	task := &types.ResolvedTask{
		ID:         "int-test-1",
		Path:       "projects/test/task/int-test-1.md",
		Title:      "Integration Test Task",
		Priority:   "high",
		Status:     "pending",
		Executor:   "pi",
		Agent:      "tdd-dev",
		Model:      "task-model",
		Extensions: []string{"code-review"},
	}

	// Step 1: Executor resolution
	executor, name, err := reg.ResolveForTask(task)
	if err != nil {
		t.Fatalf("ResolveForTask error: %v", err)
	}
	if name != "pi" {
		t.Errorf("expected executor name 'pi', got %q", name)
	}

	// Step 2: Verify it's a PiExecutor
	piExec, ok := executor.(*PiExecutor)
	if !ok {
		t.Fatalf("expected *PiExecutor, got %T", executor)
	}

	// Step 3: Inject mock command factory and capture args
	var capturedName string
	var capturedArgs []string
	piExec.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedName = name
		capturedArgs = args
		return exec.Command("/bin/cat")
	}

	// Step 4: Build prompt
	prompt := piExec.BuildPrompt(task, false)
	if !strings.Contains(prompt, "brain-runner-queue") {
		t.Error("prompt should reference brain-runner-queue skill")
	}
	if !strings.Contains(prompt, task.Path) {
		t.Error("prompt should contain task path")
	}

	// Step 5: Spawn
	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	result, err := piExec.Spawn(ctx, task, "test-project", opts)
	if err != nil {
		t.Fatalf("Spawn error: %v", err)
	}

	// Step 6: Verify command was pi binary
	if capturedName != "pi" {
		t.Errorf("command = %q, want 'pi'", capturedName)
	}

	// Step 7: Verify --mode rpc
	if len(capturedArgs) < 2 || capturedArgs[0] != "--mode" || capturedArgs[1] != "rpc" {
		t.Errorf("expected --mode rpc first, got %v", capturedArgs[:minInt(2, len(capturedArgs))])
	}

	// Step 8: Verify agent bundle resolved — should have --system-prompt-file, -e, --thinking, --tools
	argsStr := strings.Join(capturedArgs, " ")
	for _, expected := range []string{
		"--system-prompt-file",
		"--thinking high",
		"--tools read",
		"--tools write",
	} {
		if !strings.Contains(argsStr, expected) {
			t.Errorf("args should contain %q: %s", expected, argsStr)
		}
	}

	// Step 9: Verify extension composition — 3 layers
	// Layer 1: agent-bundled (-e from agent bundle)
	// Layer 2: config always-on (-e /config/always-on.ts)
	// Layer 3: per-task (-e brain-code-review.ts)
	eCount := 0
	for _, a := range capturedArgs {
		if a == "-e" {
			eCount++
		}
	}
	if eCount != 3 {
		t.Errorf("expected 3 -e flags (agent + config + task), got %d: %v", eCount, capturedArgs)
	}

	// Step 10: Verify model — task-level model overrides config
	modelIdx := indexOf(capturedArgs, "--model")
	if modelIdx < 0 || capturedArgs[modelIdx+1] != "task-model" {
		t.Errorf("expected --model task-model, got: %v", capturedArgs)
	}

	// Step 11: Verify --no-session
	if indexOf(capturedArgs, "--no-session") < 0 {
		t.Error("expected --no-session flag")
	}

	// Step 12: Verify spawn result
	if result.PID <= 0 {
		t.Errorf("PID = %d, want > 0", result.PID)
	}
	if result.PromptFile == "" {
		t.Error("PromptFile should not be empty")
	}
}

// TestIntegration_ExecutorRouting_OpencodeTask verifies that a task with
// executor:"opencode" (or empty) routes to OpenCodeExecutor via the registry.
func TestIntegration_ExecutorRouting_OpencodeTask(t *testing.T) {
	cfg := RunnerConfig{
		BrainAPIURL:            "http://localhost:3333",
		PollInterval:           30,
		MaxParallel:            2,
		MemoryThresholdPercent: 10,
		IdleDetectionThreshold: 60000,
		APITimeout:             5000,
		StateDir:               t.TempDir(),
		WorkDir:                t.TempDir(),
		Opencode: OpencodeConfig{
			Bin: "opencode",
		},
		DefaultExecutor: "opencode",
	}

	reg := NewExecutorRegistry(cfg)

	// Task with no executor set → defaults to opencode
	task := &types.ResolvedTask{
		ID:       "oc-test-1",
		Path:     "projects/test/task/oc-test-1.md",
		Title:    "OpenCode Test Task",
		Priority: "medium",
		Status:   "pending",
	}

	executor, name, err := reg.ResolveForTask(task)
	if err != nil {
		t.Fatalf("ResolveForTask error: %v", err)
	}
	if name != "opencode" {
		t.Errorf("expected executor name 'opencode', got %q", name)
	}

	// Verify it's an OpenCodeExecutor
	_, ok := executor.(*OpenCodeExecutor)
	if !ok {
		t.Fatalf("expected *OpenCodeExecutor, got %T", executor)
	}
}

// TestIntegration_ExecutorRouting_DefaultsFallback verifies the full
// precedence chain: task > task_defaults > config > "opencode"
func TestIntegration_ExecutorRouting_DefaultsFallback(t *testing.T) {
	tests := []struct {
		name            string
		taskExecutor    string
		defaultsExec    string
		configDefault   string
		wantExecutor    string
		wantExecutorTyp string // "PiExecutor" or "OpenCodeExecutor"
	}{
		{
			name:            "task executor pi wins over defaults",
			taskExecutor:    "pi",
			defaultsExec:    "opencode",
			configDefault:   "opencode",
			wantExecutor:    "pi",
			wantExecutorTyp: "PiExecutor",
		},
		{
			name:            "task_defaults pi wins over config",
			taskExecutor:    "",
			defaultsExec:    "pi",
			configDefault:   "opencode",
			wantExecutor:    "pi",
			wantExecutorTyp: "PiExecutor",
		},
		{
			name:            "config pi wins over hardcoded",
			taskExecutor:    "",
			defaultsExec:    "",
			configDefault:   "pi",
			wantExecutor:    "pi",
			wantExecutorTyp: "PiExecutor",
		},
		{
			name:            "hardcoded opencode fallback",
			taskExecutor:    "",
			defaultsExec:    "",
			configDefault:   "",
			wantExecutor:    "opencode",
			wantExecutorTyp: "OpenCodeExecutor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := RunnerConfig{
				BrainAPIURL:            "http://localhost:3333",
				PollInterval:           30,
				MaxParallel:            2,
				MemoryThresholdPercent: 10,
				IdleDetectionThreshold: 60000,
				APITimeout:             5000,
				StateDir:               t.TempDir(),
				WorkDir:                t.TempDir(),
				DefaultExecutor:        tt.configDefault,
				TaskDefaults:           TaskDefaultsConfig{Executor: tt.defaultsExec},
				Opencode:               OpencodeConfig{Bin: "opencode"},
			}

			reg := NewExecutorRegistry(cfg)
			task := &types.ResolvedTask{
				ID:       "test-1",
				Executor: tt.taskExecutor,
			}

			executor, name, err := reg.ResolveForTask(task)
			if err != nil {
				t.Fatalf("ResolveForTask error: %v", err)
			}
			if name != tt.wantExecutor {
				t.Errorf("executor name = %q, want %q", name, tt.wantExecutor)
			}

			// Verify actual executor type
			switch tt.wantExecutorTyp {
			case "PiExecutor":
				if _, ok := executor.(*PiExecutor); !ok {
					t.Errorf("expected *PiExecutor, got %T", executor)
				}
			case "OpenCodeExecutor":
				if _, ok := executor.(*OpenCodeExecutor); !ok {
					t.Errorf("expected *OpenCodeExecutor, got %T", executor)
				}
			}
		})
	}
}

// TestIntegration_ConfigPrecedence_Model verifies that model precedence works
// end-to-end: task.Model > runtime default > config.Pi.Model
func TestIntegration_ConfigPrecedence_Model(t *testing.T) {
	stateDir := t.TempDir()

	tests := []struct {
		name                string
		configModel         string
		runtimeDefaultModel string
		taskModel           string
		wantModel           string
	}{
		{"task model wins", "config-m", "runtime-m", "task-m", "task-m"},
		{"runtime wins over config", "config-m", "runtime-m", "", "runtime-m"},
		{"config wins when others empty", "config-m", "", "", "config-m"},
		{"no model when all empty", "", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testPiConfig()
			cfg.StateDir = stateDir
			cfg.Pi.Model = tt.configModel

			var capturedArgs []string
			e := NewPiExecutor(cfg)
			e.CommandFactory = func(name string, args ...string) *exec.Cmd {
				capturedArgs = args
				return exec.Command("/bin/cat")
			}

			task := testPiResolvedTask("model-test")
			task.Model = tt.taskModel

			ctx := context.Background()
			opts := SpawnOptions{
				Mode:                ExecutionModeHeadless,
				Workdir:             t.TempDir(),
				RuntimeDefaultModel: tt.runtimeDefaultModel,
			}

			_, err := e.Spawn(ctx, task, "test", opts)
			if err != nil {
				t.Fatalf("Spawn error: %v", err)
			}

			modelIdx := indexOf(capturedArgs, "--model")
			if tt.wantModel == "" {
				if modelIdx >= 0 {
					t.Errorf("expected no --model flag, got: %v", capturedArgs)
				}
			} else {
				if modelIdx < 0 || capturedArgs[modelIdx+1] != tt.wantModel {
					t.Errorf("expected --model %s, got: %v", tt.wantModel, capturedArgs)
				}
			}
		})
	}
}

// TestIntegration_MissingPiBinary verifies graceful error when the pi binary
// is not on PATH or is an invalid path.
func TestIntegration_MissingPiBinary(t *testing.T) {
	cfg := testPiConfig()
	cfg.StateDir = t.TempDir()
	cfg.Pi.Bin = "/nonexistent/path/to/pi-binary-that-does-not-exist"

	e := NewPiExecutor(cfg)
	// Do NOT override CommandFactory — use the real exec.Command

	task := testPiResolvedTask("missing-bin")
	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	_, err := e.Spawn(ctx, task, "test-project", opts)
	if err == nil {
		t.Fatal("expected error when pi binary doesn't exist")
	}
	// The error should indicate the process failed to start
	if !strings.Contains(err.Error(), "start pi process") &&
		!strings.Contains(err.Error(), "no such file") &&
		!strings.Contains(err.Error(), "executable file not found") {
		t.Errorf("error should mention process start failure, got: %v", err)
	}
}

// TestIntegration_MixedWorkload_ExecutorTracking verifies that when tasks
// are claimed and spawned via the runner, the executor type is correctly
// tracked for both OpenCode and Pi tasks, enabling correct idle detection.
func TestIntegration_MixedWorkload_ExecutorTracking(t *testing.T) {
	client := newMockClient()
	client.claimResult = ClaimResult{Success: true}

	piExec := newMockExecutor()
	proc1 := newMockProcess(100)
	proc2 := newMockProcess(200)
	piExec.spawnResult = &SpawnResult{PID: 100, Proc: proc1, Workdir: "/test"}

	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	cfg := testRunnerConfig()
	reg := &ExecutorRegistry{
		executors: map[string]TaskExecutor{
			"opencode": piExec,
			"pi":       piExec,
		},
		config: cfg,
	}

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:         []string{"proj-a"},
		Config:           cfg,
		Mode:             ExecutionModeHeadless,
		Client:           client,
		Executor:         piExec,
		ProcessMgr:       processMgr,
		StateMgr:         stateMgr,
		ExecutorRegistry: reg,
	})

	ctx := context.Background()

	// Spawn Pi task
	piExec.spawnResult = &SpawnResult{PID: 100, Proc: proc1, Workdir: "/test"}
	piTask := testTask("pi-task-1", "proj-a")
	piTask.Executor = "pi"
	err := tr.claimAndSpawn(ctx, piTask, "proj-a")
	if err != nil {
		t.Fatalf("claimAndSpawn pi task: %v", err)
	}

	// Spawn OpenCode task
	piExec.mu.Lock()
	piExec.spawnResult = &SpawnResult{PID: 200, Proc: proc2, Workdir: "/test"}
	piExec.mu.Unlock()
	ocTask := testTask("oc-task-1", "proj-a")
	ocTask.Executor = "opencode"
	err = tr.claimAndSpawn(ctx, ocTask, "proj-a")
	if err != nil {
		t.Fatalf("claimAndSpawn oc task: %v", err)
	}

	// Verify executor types are tracked correctly
	piInfo := processMgr.Get("pi-task-1")
	if piInfo == nil {
		t.Fatal("pi task should be tracked")
	}
	if piInfo.Task.ExecutorType != "pi" {
		t.Errorf("pi task ExecutorType = %q, want 'pi'", piInfo.Task.ExecutorType)
	}

	ocInfo := processMgr.Get("oc-task-1")
	if ocInfo == nil {
		t.Fatal("oc task should be tracked")
	}
	if ocInfo.Task.ExecutorType != "opencode" {
		t.Errorf("oc task ExecutorType = %q, want 'opencode'", ocInfo.Task.ExecutorType)
	}
}

// TestIntegration_AgentBundle_GracefulFallback verifies that when a task
// specifies an agent that doesn't have a bundle directory, the system
// gracefully falls back to --append-system-prompt.
func TestIntegration_AgentBundle_GracefulFallback(t *testing.T) {
	cfg := testPiConfig()
	cfg.StateDir = t.TempDir()
	cfg.Pi.AgentsDir = t.TempDir() // empty dir - no bundles

	var capturedArgs []string
	e := NewPiExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.Command("/bin/cat")
	}

	task := testPiResolvedTask("fallback-test")
	task.Agent = "nonexistent-agent"

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	_, err := e.Spawn(ctx, task, "test", opts)
	if err != nil {
		t.Fatalf("Spawn should succeed even with missing bundle: %v", err)
	}

	// Should have --append-system-prompt fallback
	argsStr := strings.Join(capturedArgs, " ")
	if !strings.Contains(argsStr, "--append-system-prompt") {
		t.Errorf("expected --append-system-prompt fallback, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "nonexistent-agent") {
		t.Errorf("fallback prompt should mention agent name, got: %s", argsStr)
	}
}

// TestIntegration_ExtensionComposition_AllThreeLayers verifies that all three
// extension layers compose correctly when spawning through the full pipeline:
// Layer 1: Agent-bundled extension
// Layer 2: Config always-on extensions
// Layer 3: Per-task extensions
func TestIntegration_ExtensionComposition_AllThreeLayers(t *testing.T) {
	// Create agent bundle with Layer 1 extension
	agentsDir := createAgentBundle(t, "ext-agent", agentBundleConfig{
		Extension: "agent-ext.ts",
	}, map[string]string{
		"agent-ext.ts": "// layer 1 extension",
	})

	cfg := testPiConfig()
	cfg.StateDir = t.TempDir()
	cfg.Pi.AgentsDir = agentsDir
	cfg.Pi.Extensions = []string{"/config/layer2-ext.ts"} // Layer 2
	cfg.Pi.ExtensionsDir = "/ext-dir"

	var capturedArgs []string
	e := NewPiExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.Command("/bin/cat")
	}

	task := testPiResolvedTask("ext-test")
	task.Agent = "ext-agent"
	task.Extensions = []string{"code-review", "/absolute/layer3.ts"} // Layer 3

	ctx := context.Background()
	opts := SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
	}

	_, err := e.Spawn(ctx, task, "test", opts)
	if err != nil {
		t.Fatalf("Spawn error: %v", err)
	}

	// Verify extension paths
	eFlags := []string{}
	for i, a := range capturedArgs {
		if a == "-e" && i+1 < len(capturedArgs) {
			eFlags = append(eFlags, capturedArgs[i+1])
		}
	}

	if len(eFlags) != 4 {
		t.Fatalf("expected 4 extensions (1 agent + 1 config + 2 task), got %d: %v", len(eFlags), eFlags)
	}

	// Layer 1: agent-bundled (resolved relative to bundle dir)
	if !strings.HasSuffix(eFlags[0], "agent-ext.ts") {
		t.Errorf("Layer 1 extension = %q, want agent-ext.ts", eFlags[0])
	}
	// Layer 2: config always-on
	if eFlags[1] != "/config/layer2-ext.ts" {
		t.Errorf("Layer 2 extension = %q, want /config/layer2-ext.ts", eFlags[1])
	}
	// Layer 3: per-task short name → resolved to brain-code-review.ts
	if eFlags[2] != "/ext-dir/brain-code-review.ts" {
		t.Errorf("Layer 3 extension [0] = %q, want /ext-dir/brain-code-review.ts", eFlags[2])
	}
	// Layer 3: per-task absolute path → as-is
	if eFlags[3] != "/absolute/layer3.ts" {
		t.Errorf("Layer 3 extension [1] = %q, want /absolute/layer3.ts", eFlags[3])
	}
}

// TestIntegration_TaskDefaults_FallThrough verifies that empty task fields
// correctly fall through to task_defaults from config.
func TestIntegration_TaskDefaults_FallThrough(t *testing.T) {
	cfg := RunnerConfig{
		PollInterval:           30,
		MaxParallel:            2,
		MemoryThresholdPercent: 10,
		IdleDetectionThreshold: 60000,
		APITimeout:             5000,
		DefaultExecutor:        "",
		TaskDefaults: TaskDefaultsConfig{
			Executor:      "pi",
			Agent:         "tdd-dev",
			Model:         "default-model",
			ExecutionMode: "worktree",
			MergePolicy:   "auto_pr",
		},
	}

	// Empty task → should fall through to task_defaults.executor = "pi"
	task := &types.ResolvedTask{ID: "td-1"}
	got := ResolveExecutorForTask(task, cfg)
	if got != "pi" {
		t.Errorf("ResolveExecutorForTask = %q, want 'pi' from task_defaults", got)
	}

	// Task with explicit executor → should override
	task2 := &types.ResolvedTask{ID: "td-2", Executor: "opencode"}
	got2 := ResolveExecutorForTask(task2, cfg)
	if got2 != "opencode" {
		t.Errorf("ResolveExecutorForTask = %q, want 'opencode' from task override", got2)
	}
}

// TestIntegration_IdleDetection_PiVsOpencode verifies that Pi and OpenCode
// tasks use different idle detection mechanisms when running simultaneously.
func TestIntegration_IdleDetection_PiVsOpencode(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	cfg := testRunnerConfig()
	cfg.IdleDetectionThreshold = 100

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	// Pi task — still running, should NOT trigger idle
	piProc := newMockProcess(300)
	piTask := RunningTask{
		ID:             "pi-integ-1",
		Path:           "projects/proj-a/task/pi-integ-1.md",
		Title:          "Pi Integration Task",
		Priority:       "medium",
		ProjectID:      "proj-a",
		PID:            300,
		StartedAt:      time.Now(),
		ExecutorType:   "pi",
		CompleteOnIdle: true,
		OpencodePort:   0, // Pi has no port
	}
	processMgr.Add("pi-integ-1", piTask, piProc)

	// OpenCode task — no server (port 0) → should be skipped
	ocProc := newMockProcess(400)
	ocTask := RunningTask{
		ID:             "oc-integ-1",
		Path:           "projects/proj-a/task/oc-integ-1.md",
		Title:          "OpenCode Integration Task",
		Priority:       "medium",
		ProjectID:      "proj-a",
		PID:            400,
		StartedAt:      time.Now(),
		ExecutorType:   "opencode",
		CompleteOnIdle: true,
		OpencodePort:   0, // No port yet
	}
	processMgr.Add("oc-integ-1", ocTask, ocProc)

	var events []RunnerEvent
	var eventMu sync.Mutex
	tr.OnEvent(func(event RunnerEvent) {
		eventMu.Lock()
		events = append(events, event)
		eventMu.Unlock()
	})

	ctx := context.Background()
	tr.checkIdleStatus(ctx)

	// Neither task should be completed or failed
	updates := client.getUpdateStatusCalls()
	if len(updates) > 0 {
		t.Errorf("expected no status updates, got: %+v", updates)
	}

	// Pi task should still be tracked (running process = busy)
	if processMgr.Get("pi-integ-1") == nil {
		t.Error("Pi task should still be tracked")
	}
	// OpenCode task should still be tracked (no port = skipped)
	if processMgr.Get("oc-integ-1") == nil {
		t.Error("OpenCode task should still be tracked")
	}
}

// TestIntegration_RegistryNewExecutorRegistry_BothExecutorsRegistered verifies
// that NewExecutorRegistry automatically registers both opencode and pi executors.
func TestIntegration_RegistryNewExecutorRegistry_BothExecutorsRegistered(t *testing.T) {
	cfg := RunnerConfig{
		BrainAPIURL:            "http://localhost:3333",
		PollInterval:           30,
		MaxParallel:            2,
		MemoryThresholdPercent: 10,
		IdleDetectionThreshold: 60000,
		APITimeout:             5000,
		StateDir:               t.TempDir(),
		WorkDir:                t.TempDir(),
		Opencode:               OpencodeConfig{Bin: "opencode"},
		Pi:                     PiConfig{Bin: "pi", NoSession: true},
	}

	reg := NewExecutorRegistry(cfg)

	// Both should be registered
	oc, ok := reg.Get("opencode")
	if !ok || oc == nil {
		t.Error("opencode executor should be registered")
	}
	pi, ok := reg.Get("pi")
	if !ok || pi == nil {
		t.Error("pi executor should be registered")
	}

	// Unknown should not be found
	_, ok = reg.Get("unknown")
	if ok {
		t.Error("unknown executor should not be registered")
	}

	// ResolveForTask with unknown executor should error
	task := &types.ResolvedTask{Executor: "unknown"}
	_, _, err := reg.ResolveForTask(task)
	if err == nil {
		t.Error("expected error for unknown executor")
	}
}

// min returns the smaller of a or b.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
