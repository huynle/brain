package lifecycle

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// =============================================================================
// PID File Operations
// =============================================================================

func TestWritePID_Success(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "test.pid")

	err := WritePID(pidFile, 12345)
	if err != nil {
		t.Fatalf("WritePID failed: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(pidFile); os.IsNotExist(err) {
		t.Fatal("PID file was not created")
	}

	// Verify content
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("Failed to read PID file: %v", err)
	}

	if string(data) != "12345" {
		t.Errorf("PID file content = %q, want %q", string(data), "12345")
	}
}

func TestReadPID_Success(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "test.pid")

	// Write a PID file manually
	if err := os.WriteFile(pidFile, []byte("54321"), 0o644); err != nil {
		t.Fatalf("Failed to write test PID file: %v", err)
	}

	pid, err := ReadPID(pidFile)
	if err != nil {
		t.Fatalf("ReadPID failed: %v", err)
	}

	if pid != 54321 {
		t.Errorf("ReadPID = %d, want %d", pid, 54321)
	}
}

func TestReadPID_FileNotExists(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "nonexistent.pid")

	_, err := ReadPID(pidFile)
	if err == nil {
		t.Fatal("ReadPID should return error for nonexistent file")
	}

	if !os.IsNotExist(err) {
		t.Errorf("Expected IsNotExist error, got: %v", err)
	}
}

func TestReadPID_InvalidContent(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "test.pid")

	// Write invalid content
	if err := os.WriteFile(pidFile, []byte("not-a-number"), 0o644); err != nil {
		t.Fatalf("Failed to write test PID file: %v", err)
	}

	_, err := ReadPID(pidFile)
	if err == nil {
		t.Fatal("ReadPID should return error for invalid content")
	}
}

func TestClearPID_Success(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "test.pid")

	// Create a PID file
	if err := os.WriteFile(pidFile, []byte("12345"), 0o644); err != nil {
		t.Fatalf("Failed to write test PID file: %v", err)
	}

	err := ClearPID(pidFile)
	if err != nil {
		t.Fatalf("ClearPID failed: %v", err)
	}

	// Verify file was removed
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Error("PID file should have been removed")
	}
}

func TestClearPID_FileNotExists(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "nonexistent.pid")

	// Should not error when file doesn't exist
	err := ClearPID(pidFile)
	if err != nil {
		t.Errorf("ClearPID should not error for nonexistent file: %v", err)
	}
}

func TestIsProcessRunning_CurrentProcess(t *testing.T) {
	// Test with current process PID (should always be running)
	pid := os.Getpid()
	running := IsProcessRunning(pid)
	if !running {
		t.Errorf("IsProcessRunning(%d) = false, want true (current process)", pid)
	}
}

func TestIsProcessRunning_InvalidPID(t *testing.T) {
	// Test with an invalid/unlikely PID
	pid := 999999
	running := IsProcessRunning(pid)
	if running {
		t.Errorf("IsProcessRunning(%d) = true, want false (invalid PID)", pid)
	}
}

func TestIsProcessRunning_InitProcess(t *testing.T) {
	// Test with PID 1 (init/systemd on Linux, launchd on macOS)
	// On some systems we may not have permission to signal PID 1
	// This test mainly ensures the function doesn't panic
	running := IsProcessRunning(1)
	// Accept either result - main goal is no panic
	_ = running
}

// =============================================================================
// File Locking (prevents race conditions)
// =============================================================================

func TestWritePID_WithLock(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "test.pid")

	// First write should succeed
	err := WritePID(pidFile, 12345)
	if err != nil {
		t.Fatalf("First WritePID failed: %v", err)
	}

	// Verify we can read it back (lock should be released)
	pid, err := ReadPID(pidFile)
	if err != nil {
		t.Fatalf("ReadPID failed: %v", err)
	}
	if pid != 12345 {
		t.Errorf("ReadPID = %d, want 12345", pid)
	}

	// Second write should succeed (overwrite)
	err = WritePID(pidFile, 54321)
	if err != nil {
		t.Fatalf("Second WritePID failed: %v", err)
	}

	// Verify overwrite
	pid, err = ReadPID(pidFile)
	if err != nil {
		t.Fatalf("ReadPID after overwrite failed: %v", err)
	}
	if pid != 54321 {
		t.Errorf("ReadPID after overwrite = %d, want 54321", pid)
	}
}

// =============================================================================
// Helper: Check if PID is alive using signal 0
// =============================================================================

func TestCheckPIDAlive_CurrentProcess(t *testing.T) {
	pid := os.Getpid()

	proc, err := os.FindProcess(pid)
	if err != nil {
		t.Fatalf("FindProcess failed: %v", err)
	}

	// Signal 0 checks existence without killing
	err = proc.Signal(syscall.Signal(0))
	if err != nil {
		t.Errorf("Current process should be alive, got error: %v", err)
	}
}
