package commands

import (
	"bytes"
	"encoding/json"
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

// TestStatusCommand_Stopped tests status command when server is stopped.
func TestStatusCommand_Stopped(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	cfg := &UnifiedConfig{}
	cfg.Server.PIDFile = pidFile
	cfg.Server.Port = 3333

	var buf bytes.Buffer
	cmd := &StatusCommand{
		Config: cfg,
		Flags:  &StatusFlags{},
		Out:    &buf,
	}

	err := cmd.Execute()
	// Exit code 1 means not running - command should return error
	if err == nil {
		t.Fatal("Expected error exit code when server stopped")
	}

	output := buf.String()
	if !contains(output, "stopped") {
		t.Errorf("Expected output to contain 'stopped', got: %s", output)
	}
}

// TestStatusCommand_Running tests status command when server is running.
func TestStatusCommand_Running(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	// Write current PID
	if err := lifecycle.WritePID(pidFile, os.Getpid()); err != nil {
		t.Fatalf("Failed to write PID: %v", err)
	}

	cfg := &UnifiedConfig{}
	cfg.Server.PIDFile = pidFile
	cfg.Server.Port = 3333

	var buf bytes.Buffer
	cmd := &StatusCommand{
		Config: cfg,
		Flags:  &StatusFlags{},
		Out:    &buf,
	}

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Expected no error when server running, got: %v", err)
	}

	output := buf.String()
	if !contains(output, "running") {
		t.Errorf("Expected output to contain 'running', got: %s", output)
	}
	if !contains(output, "PID") {
		t.Errorf("Expected output to contain 'PID', got: %s", output)
	}
}

// TestStatusCommand_JSON tests JSON output format.
func TestStatusCommand_JSON(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	// Write current PID
	if err := lifecycle.WritePID(pidFile, os.Getpid()); err != nil {
		t.Fatalf("Failed to write PID: %v", err)
	}

	cfg := &UnifiedConfig{}
	cfg.Server.PIDFile = pidFile
	cfg.Server.Port = 3333

	var buf bytes.Buffer
	cmd := &StatusCommand{
		Config: cfg,
		Flags:  &StatusFlags{JSON: true},
		Out:    &buf,
	}

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Parse JSON output
	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	if result["status"] != "running" {
		t.Errorf("Expected status 'running', got: %v", result["status"])
	}
}

// TestStatusCommand_Crashed tests status when PID file exists but process is dead.
func TestStatusCommand_Crashed(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	// Write a non-existent PID
	if err := os.WriteFile(pidFile, []byte("999999\n"), 0644); err != nil {
		t.Fatalf("Failed to write PID: %v", err)
	}

	cfg := &UnifiedConfig{}
	cfg.Server.PIDFile = pidFile
	cfg.Server.Port = 3333

	var buf bytes.Buffer
	cmd := &StatusCommand{
		Config: cfg,
		Flags:  &StatusFlags{},
		Out:    &buf,
	}

	err := cmd.Execute()
	// Exit code 1 for crashed state
	if err == nil {
		t.Fatal("Expected error exit code when server crashed")
	}

	output := buf.String()
	if !contains(output, "crashed") {
		t.Errorf("Expected output to contain 'crashed', got: %s", output)
	}
}
