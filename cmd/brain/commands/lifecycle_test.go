package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/huynle/brain-api/internal/lifecycle"
)

// TestStartCommand_AlreadyRunning tests that start fails gracefully when server is already running.
func TestStartCommand_AlreadyRunning(t *testing.T) {
	// Setup: Create a PID file with running process
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")
	
	// Write our own PID (we know it's running)
	if err := lifecycle.WritePID(pidFile, os.Getpid()); err != nil {
		t.Fatalf("Failed to write PID file: %v", err)
	}

	cfg := &UnifiedConfig{}
	cfg.Server.PIDFile = pidFile
	cfg.Server.Port = 3333

	cmd := &StartCommand{
		Config: cfg,
		Flags:  &LifecycleFlags{},
	}

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Expected error when server already running, got nil")
	}

	expectedMsg := "already running"
	if !contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error to contain %q, got %q", expectedMsg, err.Error())
	}
}

// TestStopCommand_NotRunning tests that stop handles gracefully when no server is running.
func TestStopCommand_NotRunning(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	cfg := &UnifiedConfig{}
	cfg.Server.PIDFile = pidFile

	cmd := &StopCommand{
		Config: cfg,
		Flags:  &LifecycleFlags{},
	}

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Expected error when no server running, got nil")
	}

	expectedMsg := "not running"
	if !contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error to contain %q, got %q", expectedMsg, err.Error())
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
