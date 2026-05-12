package runnercli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
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
			StateDir:    t.TempDir(),
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

func TestRunTaskRunner_RemoteShutdownRunsCleanup(t *testing.T) {
	stateDir := t.TempDir()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/health":
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		case "/api/v1/events/emit":
			w.WriteHeader(http.StatusAccepted)
		case "/api/v1/runners/register":
			var req types.RegisterRunnerRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode register request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(types.RunnerInfo{RunnerID: req.RunnerID, Hostname: req.Hostname})
		case "/api/v1/runners/heartbeat":
			_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
		case "/api/v1/tasks/proj-a":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []types.ResolvedTask{}})
		case "/api/v1/tasks/proj-a/next":
			w.WriteHeader(http.StatusNotFound)
		default:
			if len(r.URL.Path) > len("/api/v1/tasks//stream") && r.URL.Path != "/api/v1/tasks/proj-a/stream" {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				if flusher, ok := w.(http.Flusher); ok {
					_, _ = w.Write([]byte("event: shutdown\ndata: {\"reason\":\"test remote shutdown\"}\n\n"))
					flusher.Flush()
				}
				<-r.Context().Done()
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			<-r.Context().Done()
		}
	}))
	defer srv.Close()

	opts := RunnerOptions{
		Projects: []string{"proj-a"},
		Mode:     "headless",
		Config: runner.RunnerConfig{
			BrainAPIURL:  srv.URL,
			MaxParallel:  1,
			PollInterval: 1,
			StateDir:     stateDir,
			WorkDir:      t.TempDir(),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := RunTaskRunner(ctx, opts); err != nil {
		t.Fatalf("RunTaskRunner returned error: %v", err)
	}

	pidFile := filepath.Join(stateDir, "runner-proj-a.pid")
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Fatalf("PID file should be cleared after remote shutdown cleanup, stat err = %v", err)
	}
}

// TestRunTUI_BasicStartStop tests TUI mode lifecycle.
func TestRunTUI_BasicStartStop(t *testing.T) {
	opts := RunnerOptions{
		Projects:    []string{"test-project"},
		Mode:        "tui",
		StartPaused: true,
		Config: runner.RunnerConfig{
			BrainAPIURL: "http://localhost:3333",
			MaxParallel: 1,
			StateDir:    t.TempDir(),
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
