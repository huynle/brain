package runner

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// ResolveExecutorForTask Tests — Precedence: task > task_defaults > config > "opencode"
// =============================================================================

func TestResolveExecutorForTask_TaskExecutorWins(t *testing.T) {
	task := &types.ResolvedTask{Executor: "pi"}
	cfg := RunnerConfig{
		TaskDefaults:    TaskDefaultsConfig{Executor: "opencode"},
		DefaultExecutor: "opencode",
	}

	got := ResolveExecutorForTask(task, cfg)
	if got != "pi" {
		t.Errorf("ResolveExecutorForTask = %q, want %q (task.Executor wins)", got, "pi")
	}
}

func TestResolveExecutorForTask_TaskDefaultsWins(t *testing.T) {
	task := &types.ResolvedTask{} // no executor set
	cfg := RunnerConfig{
		TaskDefaults:    TaskDefaultsConfig{Executor: "pi"},
		DefaultExecutor: "opencode",
	}

	got := ResolveExecutorForTask(task, cfg)
	if got != "pi" {
		t.Errorf("ResolveExecutorForTask = %q, want %q (task_defaults.Executor wins)", got, "pi")
	}
}

func TestResolveExecutorForTask_ConfigDefaultWins(t *testing.T) {
	task := &types.ResolvedTask{} // no executor set
	cfg := RunnerConfig{
		TaskDefaults:    TaskDefaultsConfig{}, // no executor
		DefaultExecutor: "pi",
	}

	got := ResolveExecutorForTask(task, cfg)
	if got != "pi" {
		t.Errorf("ResolveExecutorForTask = %q, want %q (config.DefaultExecutor wins)", got, "pi")
	}
}

func TestResolveExecutorForTask_HardcodedDefault(t *testing.T) {
	task := &types.ResolvedTask{} // no executor
	cfg := RunnerConfig{}         // nothing set

	got := ResolveExecutorForTask(task, cfg)
	if got != "opencode" {
		t.Errorf("ResolveExecutorForTask = %q, want %q (hardcoded default)", got, "opencode")
	}
}

func TestResolveExecutorForTask_FullPrecedenceChain(t *testing.T) {
	tests := []struct {
		name         string
		taskExec     string
		defaultsExec string
		configExec   string
		want         string
	}{
		{"all set, task wins", "pi", "opencode", "opencode", "pi"},
		{"task empty, defaults wins", "", "pi", "opencode", "pi"},
		{"task+defaults empty, config wins", "", "", "pi", "pi"},
		{"all empty, hardcoded default", "", "", "", "opencode"},
		{"task set to opencode", "opencode", "pi", "pi", "opencode"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &types.ResolvedTask{Executor: tt.taskExec}
			cfg := RunnerConfig{
				TaskDefaults:    TaskDefaultsConfig{Executor: tt.defaultsExec},
				DefaultExecutor: tt.configExec,
			}

			got := ResolveExecutorForTask(task, cfg)
			if got != tt.want {
				t.Errorf("ResolveExecutorForTask = %q, want %q", got, tt.want)
			}
		})
	}
}

// =============================================================================
// ExecutorRegistry Tests
// =============================================================================

func TestNewExecutorRegistry_DefaultOpencode(t *testing.T) {
	cfg := testExecutorConfig()
	reg := NewExecutorRegistry(cfg)

	// Should have opencode registered by default
	exec, ok := reg.Get("opencode")
	if !ok {
		t.Fatal("expected opencode executor to be registered")
	}
	if exec == nil {
		t.Fatal("expected non-nil opencode executor")
	}
}

func TestNewExecutorRegistry_RegistersScriptWhenEnabled(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.Script.Enabled = true
	reg := NewExecutorRegistry(cfg)

	exec, ok := reg.Get("script")
	if !ok {
		t.Fatal("expected script executor to be registered when script execution is enabled")
	}
	if exec == nil {
		t.Fatal("expected non-nil script executor")
	}
}

func TestExecutorRegistry_RegisterAndGet(t *testing.T) {
	cfg := testExecutorConfig()
	reg := NewExecutorRegistry(cfg)

	mock := newMockExecutor()
	reg.Register("pi", mock)

	exec, ok := reg.Get("pi")
	if !ok {
		t.Fatal("expected pi executor to be registered")
	}
	if exec != mock {
		t.Error("expected to get the same mock executor back")
	}
}

func TestExecutorRegistry_GetUnknown(t *testing.T) {
	cfg := testExecutorConfig()
	reg := NewExecutorRegistry(cfg)

	_, ok := reg.Get("unknown")
	if ok {
		t.Error("expected Get to return false for unknown executor")
	}
}

func TestExecutorRegistry_ResolveForTask(t *testing.T) {
	cfg := testExecutorConfig()
	cfg.DefaultExecutor = ""
	cfg.TaskDefaults.Executor = ""
	reg := NewExecutorRegistry(cfg)

	// Register mock pi executor
	mockPi := newMockExecutor()
	reg.Register("pi", mockPi)

	// Task with executor=pi should resolve to the pi executor
	task := &types.ResolvedTask{Executor: "pi"}
	exec, name, err := reg.ResolveForTask(task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "pi" {
		t.Errorf("name = %q, want %q", name, "pi")
	}
	if exec != mockPi {
		t.Error("expected pi executor")
	}
}

func TestExecutorRegistry_ResolveForTask_DefaultsToOpencode(t *testing.T) {
	cfg := testExecutorConfig()
	reg := NewExecutorRegistry(cfg)

	task := &types.ResolvedTask{} // no executor
	exec, name, err := reg.ResolveForTask(task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "opencode" {
		t.Errorf("name = %q, want %q", name, "opencode")
	}
	if exec == nil {
		t.Fatal("expected non-nil executor")
	}
}

func TestExecutorRegistry_ResolveForTask_UnregisteredExecutor(t *testing.T) {
	cfg := testExecutorConfig()
	reg := NewExecutorRegistry(cfg)

	task := &types.ResolvedTask{Executor: "unknown-executor"} // not registered
	_, _, err := reg.ResolveForTask(task)
	if err == nil {
		t.Error("expected error for unregistered executor")
	}
}

func TestExecutorRegistry_Names(t *testing.T) {
	cfg := testExecutorConfig()
	reg := NewExecutorRegistry(cfg)

	mock := newMockExecutor()
	reg.Register("pi", mock)

	names := reg.Names()
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d: %v", len(names), names)
	}

	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["opencode"] || !found["pi"] {
		t.Errorf("expected [opencode, pi], got %v", names)
	}
}
