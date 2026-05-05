package runner

import (
	"fmt"
	"sort"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Executor Resolution
// =============================================================================

// ResolveExecutorForTask determines which executor should run a task.
// Precedence chain: task.Executor > task_defaults.executor > config.DefaultExecutor > "opencode"
func ResolveExecutorForTask(task *types.ResolvedTask, cfg RunnerConfig) string {
	if task.Executor != "" {
		return task.Executor
	}
	if cfg.TaskDefaults.Executor != "" {
		return cfg.TaskDefaults.Executor
	}
	if cfg.DefaultExecutor != "" {
		return cfg.DefaultExecutor
	}
	return "opencode"
}

// =============================================================================
// Executor Registry
// =============================================================================

// ExecutorRegistry holds a map of named executors and resolves per-task dispatch.
type ExecutorRegistry struct {
	executors map[string]TaskExecutor
	config    RunnerConfig
}

// NewExecutorRegistry creates a registry pre-populated with OpenCode and Pi executors.
func NewExecutorRegistry(cfg RunnerConfig) *ExecutorRegistry {
	reg := &ExecutorRegistry{
		executors: make(map[string]TaskExecutor),
		config:    cfg,
	}
	// Always register the opencode executor
	reg.executors["opencode"] = NewExecutor(cfg)
	// Always register the pi executor
	reg.executors["pi"] = NewPiExecutor(cfg)
	if cfg.Script.Enabled {
		reg.executors["script"] = NewExecutor(cfg)
	}
	return reg
}

// Register adds an executor under the given name.
func (r *ExecutorRegistry) Register(name string, executor TaskExecutor) {
	r.executors[name] = executor
}

// Get returns the executor for a given name.
func (r *ExecutorRegistry) Get(name string) (TaskExecutor, bool) {
	e, ok := r.executors[name]
	return e, ok
}

// MustGet returns the executor for a given name, panicking if not found.
// Use only during initialization when the executor is known to be registered.
func (r *ExecutorRegistry) MustGet(name string) TaskExecutor {
	e, ok := r.executors[name]
	if !ok {
		panic(fmt.Sprintf("executor %q not registered", name))
	}
	return e
}

// ResolveForTask resolves the executor for a task using the precedence chain,
// then looks it up in the registry. Returns the executor, its name, and any error.
func (r *ExecutorRegistry) ResolveForTask(task *types.ResolvedTask) (TaskExecutor, string, error) {
	name := ResolveExecutorForTask(task, r.config)
	executor, ok := r.executors[name]
	if !ok {
		return nil, name, fmt.Errorf("executor %q not registered (available: %v)", name, r.Names())
	}
	return executor, name, nil
}

// Names returns all registered executor names, sorted.
func (r *ExecutorRegistry) Names() []string {
	names := make([]string, 0, len(r.executors))
	for name := range r.executors {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
