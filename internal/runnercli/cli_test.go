package runnercli

import (
	"context"
	"os"
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

// TestRunTUI_BasicStartStop tests TUI mode lifecycle.
func TestRunTUI_BasicStartStop(t *testing.T) {
	// Bubbletea needs a real TTY; headless environments (CI) don't have one.
	if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err != nil {
		t.Skipf("no TTY available: %v", err)
	} else {
		_ = tty.Close()
	}

	opts := RunnerOptions{
		Projects:    []string{"test-project"},
		Mode:        "tui",
		StartPaused: true,
		Config: runner.RunnerConfig{
			BrainAPIURL: "http://localhost:3333",
			MaxParallel: 1,
			WorkDir:     t.TempDir(),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// TUI should respect context cancellation
	err := RunTUI(ctx, opts)
	if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRunTUI_EmptyProjects tests TUI error handling.
func TestRunTUI_EmptyProjects(t *testing.T) {
	opts := RunnerOptions{
		Projects: []string{},
		Mode:     "tui",
		Config: runner.RunnerConfig{
			BrainAPIURL: "http://localhost:3333",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := RunTUI(ctx, opts)
	if err == nil {
		t.Fatal("expected error for empty projects, got nil")
	}
}

// TestRunnerOptions_FullConfigPassthrough verifies that all runner.RunnerConfig fields
// are accepted by RunnerOptions without any lossy conversion layer.
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
