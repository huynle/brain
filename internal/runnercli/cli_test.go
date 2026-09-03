package runnercli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/runner"
)

// TestRunTaskRunner_BasicStartStop tests basic runner lifecycle.
func TestRunTaskRunner_BasicStartStop(t *testing.T) {
	opts := RunnerOptions{
		Projects:    []string{"test-project"},
		Mode:        "headless",
		StartPaused: false,
		Config: runner.RunnerConfig{
			BrainAPIURL: "http://localhost:3333",
			MaxParallel: 1,
			WorkDir:     t.TempDir(),
			// StateDir/LogDir must be temp dirs: the runner writes state, PID and
			// running-task files there, and an empty value would resolve relative
			// to the package directory and dirty the repo.
			StateDir: t.TempDir(),
			LogDir:   t.TempDir(),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Should respect context cancellation
	err := RunTaskRunner(ctx, opts)
	if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRunTaskRunner_InvalidProject tests error handling for missing project.
func TestRunTaskRunner_InvalidProject(t *testing.T) {
	opts := RunnerOptions{
		Projects: []string{}, // Empty projects should error
		Mode:     "headless",
		Config: runner.RunnerConfig{
			BrainAPIURL: "http://localhost:3333",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := RunTaskRunner(ctx, opts)
	if err == nil {
		t.Fatal("expected error for empty projects, got nil")
	}
}

func TestRunnerOptions_FullConfigPassthrough(t *testing.T) {
	// Construct a RunnerConfig with ALL fields populated
	cfg := runner.RunnerConfig{
		BrainAPIURL:            "http://localhost:9999",
		APIToken:               "test-token",
		PollInterval:           15,
		TaskPollInterval:       5,
		MaxParallel:            4,
		StateDir:               "/tmp/state",
		LogDir:                 "/tmp/logs",
		WorkDir:                "/tmp/work",
		APITimeout:             3000,
		TaskTimeout:            60000,
		IdleDetectionThreshold: 5000,
		MaxTotalProcesses:      10,
		MemoryThresholdPercent: 80,
		Opencode: runner.OpencodeConfig{
			Bin:   "/usr/local/bin/opencode",
			Agent: "test-agent",
			Model: "test-model",
		},
		ExcludeProjects: []string{"excluded-project"},
		AutoMonitors:    true,
		EnvPassthrough:  []string{"FOO", "BAR"},
		FeatureIDs:      []string{"feat-1", "feat-2"},
	}

	opts := RunnerOptions{
		Projects:    []string{"test-project"},
		Mode:        "headless",
		StartPaused: false,
		Config:      cfg,
	}

	// Verify all fields survive the passthrough (no conversion layer to lose them)
	if opts.Config.FeatureIDs[0] != "feat-1" || opts.Config.FeatureIDs[1] != "feat-2" {
		t.Errorf("FeatureIDs lost: got %v", opts.Config.FeatureIDs)
	}
	if opts.Config.Opencode.Agent != "test-agent" {
		t.Errorf("Opencode.Agent lost: got %q", opts.Config.Opencode.Agent)
	}
	if opts.Config.Opencode.Model != "test-model" {
		t.Errorf("Opencode.Model lost: got %q", opts.Config.Opencode.Model)
	}
	if opts.Config.TaskTimeout != 60000 {
		t.Errorf("TaskTimeout lost: got %d", opts.Config.TaskTimeout)
	}
	if opts.Config.IdleDetectionThreshold != 5000 {
		t.Errorf("IdleDetectionThreshold lost: got %d", opts.Config.IdleDetectionThreshold)
	}
	if opts.Config.MaxTotalProcesses != 10 {
		t.Errorf("MaxTotalProcesses lost: got %d", opts.Config.MaxTotalProcesses)
	}
	if opts.Config.MemoryThresholdPercent != 80 {
		t.Errorf("MemoryThresholdPercent lost: got %d", opts.Config.MemoryThresholdPercent)
	}
	if opts.Config.AutoMonitors != true {
		t.Errorf("AutoMonitors lost: got %v", opts.Config.AutoMonitors)
	}
	if len(opts.Config.EnvPassthrough) != 2 || opts.Config.EnvPassthrough[0] != "FOO" {
		t.Errorf("EnvPassthrough lost: got %v", opts.Config.EnvPassthrough)
	}
	if opts.Config.TaskPollInterval != 5 {
		t.Errorf("TaskPollInterval lost: got %d", opts.Config.TaskPollInterval)
	}
	if opts.Config.Opencode.Bin != "/usr/local/bin/opencode" {
		t.Errorf("Opencode.Bin lost: got %q", opts.Config.Opencode.Bin)
	}
}

// TestRunTaskRunner_EmptyStateDirDoesNotWriteToCwd is a regression guard: an
// unset StateDir used to be joined into relative paths, so the runner dropped
// runner-<project>.json / .pid into whatever directory the process ran from —
// under `go test` that is the package directory, which dirtied the repo.
func TestRunTaskRunner_EmptyStateDirDoesNotWriteToCwd(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	before := dirEntries(t, cwd)

	opts := RunnerOptions{
		Projects:    []string{"test-project"},
		Mode:        "headless",
		StartPaused: false,
		Config: runner.RunnerConfig{
			BrainAPIURL: "http://localhost:3333",
			MaxParallel: 1,
			WorkDir:     t.TempDir(),
			LogDir:      t.TempDir(),
			// StateDir deliberately left empty — it must resolve to a default.
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := RunTaskRunner(ctx, opts); err != nil &&
		err != context.DeadlineExceeded && err != context.Canceled {
		t.Fatalf("unexpected error: %v", err)
	}

	for name := range dirEntries(t, cwd) {
		if _, existed := before[name]; !existed {
			t.Errorf("runner wrote %q into the working directory; StateDir should default outside the repo", name)
		}
	}

	// And it should have landed under the resolved default instead.
	want := filepath.Join(stateHome, "brain-runner")
	if got := runner.DefaultStateDir(); got != want {
		t.Errorf("DefaultStateDir() = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(want, "runner-test-project.json")); err != nil {
		t.Errorf("expected runner state under %s: %v", want, err)
	}
}

func dirEntries(t *testing.T, dir string) map[string]struct{} {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir %s: %v", dir, err)
	}
	names := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		names[e.Name()] = struct{}{}
	}
	return names
}

// TestPrepareRunnerConfig covers the single chokepoint where a runner's name
// becomes its state dir — the thing that lets several runners share a machine.
func TestPrepareRunnerConfig(t *testing.T) {
	base := t.TempDir()

	t.Run("named runner is isolated", func(t *testing.T) {
		cfg := runner.RunnerConfig{Name: "worker-a", StateDir: base}
		if err := prepareRunnerConfig(&cfg); err != nil {
			t.Fatalf("prepareRunnerConfig: %v", err)
		}
		if want := filepath.Join(base, "worker-a"); cfg.StateDir != want {
			t.Errorf("StateDir = %q, want %q", cfg.StateDir, want)
		}
		if cfg.Labels[runner.RunnerNameLabel] != "worker-a" {
			t.Errorf("name label = %q, want worker-a", cfg.Labels[runner.RunnerNameLabel])
		}
		// Defaults still applied.
		if cfg.MaxParallel != 3 || cfg.PollInterval != 10 || cfg.APITimeout != 5000 || cfg.Opencode.Bin != "opencode" {
			t.Errorf("defaults not applied: %+v", cfg)
		}
	})

	t.Run("unnamed runner keeps its state dir", func(t *testing.T) {
		cfg := runner.RunnerConfig{StateDir: base}
		if err := prepareRunnerConfig(&cfg); err != nil {
			t.Fatalf("prepareRunnerConfig: %v", err)
		}
		if cfg.StateDir != base {
			t.Errorf("StateDir = %q, want %q", cfg.StateDir, base)
		}
	})

	t.Run("invalid name fails before anything starts", func(t *testing.T) {
		cfg := runner.RunnerConfig{Name: "bad name", StateDir: base}
		if err := prepareRunnerConfig(&cfg); err == nil {
			t.Fatal("prepareRunnerConfig accepted an invalid runner name")
		}
	})
}
