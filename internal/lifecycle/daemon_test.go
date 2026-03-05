package lifecycle

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// =============================================================================
// Daemonize - Integration Test
// =============================================================================

func TestDaemonize_BasicSpawn(t *testing.T) {
	// Create a test script that will run as daemon
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "test-daemon.sh")
	pidFile := filepath.Join(dir, "daemon.pid")
	logFile := filepath.Join(dir, "daemon.log")

	// Write a simple script that writes its PID and sleeps
	script := `#!/bin/bash
echo $$ > ` + pidFile + `
echo "Daemon started" >> ` + logFile + `
sleep 10
echo "Daemon finished" >> ` + logFile + `
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("Failed to write test script: %v", err)
	}

	opts := DaemonOptions{
		PIDFile: pidFile,
		LogFile: logFile,
		WorkDir: dir,
	}

	// Spawn the daemon
	pid, err := Daemonize(scriptPath, []string{}, opts)
	if err != nil {
		t.Fatalf("Daemonize failed: %v", err)
	}

	if pid <= 0 {
		t.Fatalf("Invalid PID returned: %d", pid)
	}

	// Wait for PID file to be written
	time.Sleep(100 * time.Millisecond)

	// Verify PID file was created
	writtenPID, err := ReadPID(pidFile)
	if err != nil {
		t.Fatalf("Failed to read PID file: %v", err)
	}

	if writtenPID <= 0 {
		t.Errorf("Invalid PID in file: %d", writtenPID)
	}

	// Verify process is running
	if !IsProcessRunning(writtenPID) {
		t.Error("Daemon process should be running")
	}

	// Verify log file was created and contains output
	time.Sleep(100 * time.Millisecond)
	logData, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if len(logData) == 0 {
		t.Error("Log file should contain output")
	}

	// Clean up: kill the daemon
	if proc, err := os.FindProcess(writtenPID); err == nil {
		proc.Kill()
		proc.Wait()
	}
}

func TestDaemonize_WithCommand(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "daemon.pid")
	logFile := filepath.Join(dir, "daemon.log")

	// Use 'sleep' command which is available on Unix systems
	opts := DaemonOptions{
		PIDFile: pidFile,
		LogFile: logFile,
		WorkDir: dir,
	}

	// Spawn sleep as daemon
	pid, err := Daemonize("sleep", []string{"5"}, opts)
	if err != nil {
		t.Fatalf("Daemonize failed: %v", err)
	}

	if pid <= 0 {
		t.Fatalf("Invalid PID returned: %d", pid)
	}

	// Verify process is running
	time.Sleep(100 * time.Millisecond)
	if !IsProcessRunning(pid) {
		t.Error("Daemon process should be running")
	}

	// Clean up
	if proc, err := os.FindProcess(pid); err == nil {
		proc.Kill()
		proc.Wait()
	}
}

func TestDaemonize_InvalidCommand(t *testing.T) {
	dir := t.TempDir()
	opts := DaemonOptions{
		PIDFile: filepath.Join(dir, "daemon.pid"),
		LogFile: filepath.Join(dir, "daemon.log"),
	}

	// Try to spawn a non-existent command
	_, err := Daemonize("/nonexistent/command", []string{}, opts)
	if err == nil {
		t.Fatal("Daemonize should fail for invalid command")
	}
}

// =============================================================================
// SpawnDetached - Helper Function Tests
// =============================================================================

func TestSpawnDetached_BasicSpawn(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "output.log")

	// Spawn a simple command
	cmd := exec.Command("sleep", "2")
	pid, err := SpawnDetached(cmd, logFile, logFile)
	if err != nil {
		t.Fatalf("SpawnDetached failed: %v", err)
	}

	if pid <= 0 {
		t.Fatalf("Invalid PID returned: %d", pid)
	}

	// Verify process is running
	if !IsProcessRunning(pid) {
		t.Error("Spawned process should be running")
	}

	// Clean up
	if proc, err := os.FindProcess(pid); err == nil {
		proc.Kill()
		proc.Wait()
	}
}

func TestSpawnDetached_WithOutput(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "output.log")

	// Spawn a command that produces output
	cmd := exec.Command("sh", "-c", "echo 'Hello from daemon'")
	pid, err := SpawnDetached(cmd, logFile, logFile)
	if err != nil {
		t.Fatalf("SpawnDetached failed: %v", err)
	}

	// Wait for command to complete and output to be written
	time.Sleep(200 * time.Millisecond)

	// Verify output was captured
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if len(data) == 0 {
		t.Error("Log file should contain output")
	}

	// The process may have exited by now, which is fine
	_ = pid
}

func TestSpawnDetached_SeparateErrorLog(t *testing.T) {
	dir := t.TempDir()
	stdoutLog := filepath.Join(dir, "stdout.log")
	stderrLog := filepath.Join(dir, "stderr.log")

	// Spawn a command that writes to both stdout and stderr
	cmd := exec.Command("sh", "-c", "echo 'stdout message'; echo 'stderr message' >&2")
	pid, err := SpawnDetached(cmd, stdoutLog, stderrLog)
	if err != nil {
		t.Fatalf("SpawnDetached failed: %v", err)
	}

	// Wait for command to complete
	time.Sleep(200 * time.Millisecond)

	// Verify stdout log
	stdoutData, err := os.ReadFile(stdoutLog)
	if err != nil {
		t.Fatalf("Failed to read stdout log: %v", err)
	}

	if len(stdoutData) == 0 {
		t.Error("Stdout log should contain output")
	}

	// Verify stderr log
	stderrData, err := os.ReadFile(stderrLog)
	if err != nil {
		t.Fatalf("Failed to read stderr log: %v", err)
	}

	if len(stderrData) == 0 {
		t.Error("Stderr log should contain output")
	}

	_ = pid
}

// =============================================================================
// Edge Cases
// =============================================================================

func TestDaemonize_EmptyWorkDir(t *testing.T) {
	dir := t.TempDir()
	opts := DaemonOptions{
		PIDFile: filepath.Join(dir, "daemon.pid"),
		LogFile: filepath.Join(dir, "daemon.log"),
		// WorkDir left empty - should use current directory
	}

	pid, err := Daemonize("sleep", []string{"1"}, opts)
	if err != nil {
		t.Fatalf("Daemonize should work with empty WorkDir: %v", err)
	}

	// Clean up
	if proc, err := os.FindProcess(pid); err == nil {
		proc.Kill()
		proc.Wait()
	}
}

func TestDaemonize_CreateLogDirs(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "nested", "logs")

	opts := DaemonOptions{
		PIDFile: filepath.Join(dir, "daemon.pid"),
		LogFile: filepath.Join(logDir, "daemon.log"),
		WorkDir: dir,
	}

	// Should create nested directories automatically
	pid, err := Daemonize("sleep", []string{"1"}, opts)
	if err != nil {
		t.Fatalf("Daemonize should create log directories: %v", err)
	}

	// Verify log directory was created
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		t.Error("Log directory should have been created")
	}

	// Clean up
	if proc, err := os.FindProcess(pid); err == nil {
		proc.Kill()
		proc.Wait()
	}
}
