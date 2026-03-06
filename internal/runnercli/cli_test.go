package runnercli

import (
	"context"
	"testing"
	"time"
)

// TestRunTaskRunner_BasicStartStop tests basic runner lifecycle.
func TestRunTaskRunner_BasicStartStop(t *testing.T) {
	opts := RunnerOptions{
		Projects:    []string{"test-project"},
		Mode:        "background",
		StartPaused: false,
		Config: RunnerConfig{
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
		Mode:     "background",
		Config: RunnerConfig{
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
	opts := RunnerOptions{
		Projects:    []string{"test-project"},
		Mode:        "tui",
		StartPaused: true,
		Config: RunnerConfig{
			BrainAPIURL: "http://localhost:3333",
			MaxParallel: 1,
			WorkDir:     t.TempDir(),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// TUI should respect context cancellation
	// Note: In CI/test environments without TTY, this may fail with "could not open a new TTY"
	// which is expected behavior
	err := RunTUI(ctx, opts)
	if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
		// Allow TTY errors in non-interactive environments
		if err.Error() != "TUI failed: could not open a new TTY: open /dev/tty: device not configured" {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

// TestRunTUI_EmptyProjects tests TUI error handling.
func TestRunTUI_EmptyProjects(t *testing.T) {
	opts := RunnerOptions{
		Projects: []string{},
		Mode:     "tui",
		Config: RunnerConfig{
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
