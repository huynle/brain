package lifecycle

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// spawnWait bounds how long tests wait for a spawned child to be scheduled
// and produce observable output. Under `go test ./...` every package binary
// runs concurrently and fork/exec of a shell can take several seconds, so
// this must be generous; polling returns as soon as the condition holds.
const spawnWait = 30 * time.Second

// waitFor polls cond every 50ms until it returns true or timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return cond()
}

// fileHasContent reports whether path exists and is non-empty.
func fileHasContent(path string) bool {
	data, err := os.ReadFile(path)
	return err == nil && len(data) > 0
}

// killProcess registers a cleanup that kills pid, so daemons are reaped even
// when an assertion fails the test early.
func killProcess(t *testing.T, pid int) {
	t.Helper()
	t.Cleanup(func() {
		if proc, err := os.FindProcess(pid); err == nil {
			proc.Kill()
			proc.Wait()
		}
	})
}

// =============================================================================
// Daemonize - Integration Test
// =============================================================================

func TestDaemonize_BasicSpawn(t *testing.T) {
	// Create a test script that will run as daemon
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "test-daemon.sh")
	pidFile := filepath.Join(dir, "daemon.pid")
	logFile := filepath.Join(dir, "daemon.log")

	// The script must NOT write the PID file: Daemonize writes it too, and
	// two truncating writers can produce a torn read (a partial PID that
	// still parses). The script only logs, then execs sleep so the kill in
	// cleanup reaps the whole daemon with no orphaned child.
	script := `#!/bin/bash
echo "Daemon started" >> ` + logFile + `
exec sleep 60
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
	killProcess(t, pid)

	if pid <= 0 {
		t.Fatalf("Invalid PID returned: %d", pid)
	}

	// Daemonize writes the PID file synchronously before returning.
	writtenPID, err := ReadPID(pidFile)
	if err != nil {
		t.Fatalf("Failed to read PID file: %v", err)
	}
	if writtenPID != pid {
		t.Errorf("PID file contains %d, want %d", writtenPID, pid)
	}

	// Verify process is running
	if !IsProcessRunning(pid) {
		t.Error("Daemon process should be running")
	}

	// Wait for the daemon's first log line.
	if !waitFor(t, spawnWait, func() bool { return fileHasContent(logFile) }) {
		t.Errorf("Log file should contain output after %v", spawnWait)
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

	// Spawn sleep as daemon; long enough that it cannot exit before the
	// running check even on a heavily loaded machine.
	pid, err := Daemonize("sleep", []string{"60"}, opts)
	if err != nil {
		t.Fatalf("Daemonize failed: %v", err)
	}
	killProcess(t, pid)

	if pid <= 0 {
		t.Fatalf("Invalid PID returned: %d", pid)
	}

	// Verify process is running
	if !IsProcessRunning(pid) {
		t.Error("Daemon process should be running")
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

	// Spawn a simple command; long enough that it cannot exit before the
	// running check even on a heavily loaded machine.
	cmd := exec.Command("sleep", "60")
	pid, err := SpawnDetached(cmd, logFile, logFile)
	if err != nil {
		t.Fatalf("SpawnDetached failed: %v", err)
	}
	killProcess(t, pid)

	if pid <= 0 {
		t.Fatalf("Invalid PID returned: %d", pid)
	}

	// Verify process is running
	if !IsProcessRunning(pid) {
		t.Error("Spawned process should be running")
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

	// Wait for the command to run and its output to be written
	if !waitFor(t, spawnWait, func() bool { return fileHasContent(logFile) }) {
		t.Errorf("Log file should contain output after %v", spawnWait)
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

	// Wait for the command to run and both streams to be written
	if !waitFor(t, spawnWait, func() bool {
		return fileHasContent(stdoutLog) && fileHasContent(stderrLog)
	}) {
		if !fileHasContent(stdoutLog) {
			t.Error("Stdout log should contain output")
		}
		if !fileHasContent(stderrLog) {
			t.Error("Stderr log should contain output")
		}
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

	pid, err := Daemonize("sleep", []string{"60"}, opts)
	if err != nil {
		t.Fatalf("Daemonize should work with empty WorkDir: %v", err)
	}
	killProcess(t, pid)
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
	pid, err := Daemonize("sleep", []string{"60"}, opts)
	if err != nil {
		t.Fatalf("Daemonize should create log directories: %v", err)
	}
	killProcess(t, pid)

	// Verify log directory was created
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		t.Error("Log directory should have been created")
	}
}
