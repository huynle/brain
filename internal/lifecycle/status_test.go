package lifecycle_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/lifecycle"
)

// TestGetServerStatus_Stopped tests status when no PID file exists.
func TestGetServerStatus_Stopped(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	// PID file doesn't exist
	state, err := lifecycle.GetServerStatus(pidFile, 3333)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if state.Status != lifecycle.ServerStatusStopped {
		t.Errorf("expected status stopped, got: %v", state.Status)
	}
	if state.PID != 0 {
		t.Errorf("expected PID 0, got: %d", state.PID)
	}
	if state.Port != 0 {
		t.Errorf("expected port 0, got: %d", state.Port)
	}
}

// TestGetServerStatus_Crashed tests status when PID file exists but process is dead.
func TestGetServerStatus_Crashed(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	// Write a PID that doesn't exist (999999)
	if err := os.WriteFile(pidFile, []byte("999999\n"), 0644); err != nil {
		t.Fatalf("failed to write PID file: %v", err)
	}

	state, err := lifecycle.GetServerStatus(pidFile, 3333)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if state.Status != lifecycle.ServerStatusCrashed {
		t.Errorf("expected status crashed, got: %v", state.Status)
	}
	if state.PID != 999999 {
		t.Errorf("expected PID 999999, got: %d", state.PID)
	}
	if state.Port != 0 {
		t.Errorf("expected port 0 for crashed process, got: %d", state.Port)
	}
}

// TestGetServerStatus_Running tests status when process is running.
func TestGetServerStatus_Running(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	// Use current process PID (we know it's running)
	currentPID := os.Getpid()
	if err := lifecycle.WritePID(pidFile, currentPID); err != nil {
		t.Fatalf("failed to write PID file: %v", err)
	}

	state, err := lifecycle.GetServerStatus(pidFile, 3333)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if state.Status != lifecycle.ServerStatusRunning {
		t.Errorf("expected status running, got: %v", state.Status)
	}
	if state.PID != currentPID {
		t.Errorf("expected PID %d, got: %d", currentPID, state.PID)
	}
	// Port should be set when running (even if we can't verify health endpoint)
	if state.Port != 3333 {
		t.Errorf("expected port 3333, got: %d", state.Port)
	}
}

// TestGetServerStatus_UptimeCalculation tests that uptime is calculated correctly.
func TestGetServerStatus_UptimeCalculation(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	// Use current process PID
	currentPID := os.Getpid()
	if err := lifecycle.WritePID(pidFile, currentPID); err != nil {
		t.Fatalf("failed to write PID file: %v", err)
	}

	state, err := lifecycle.GetServerStatus(pidFile, 3333)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Uptime should be non-zero for running process
	if state.Uptime == 0 {
		t.Error("expected non-zero uptime for running process")
	}

	// Uptime should be reasonable (less than 24 hours for this test)
	if state.Uptime > 24*time.Hour {
		t.Errorf("uptime %v seems unreasonable for test process", state.Uptime)
	}

	// StartedAt should be in the past
	if state.StartedAt.IsZero() {
		t.Error("expected non-zero StartedAt for running process")
	}
	if state.StartedAt.After(time.Now()) {
		t.Error("StartedAt should be in the past")
	}
}
