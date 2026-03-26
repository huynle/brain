//go:build !short
// +build !short

package commands

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/lifecycle"
)

// TestLifecycleCommands_E2E tests the full lifecycle: start -> check running -> stop.
func TestLifecycleCommands_E2E(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "brain-e2e.pid")
	logFile := filepath.Join(tmpDir, "brain-e2e.log")

	cfg := &UnifiedConfig{}
	cfg.Server.PIDFile = pidFile
	cfg.Server.LogFile = logFile
	cfg.Server.Port = 13340

	// Test 1: Start command with dry-run
	t.Run("StartDryRun", func(t *testing.T) {
		startCmd := &StartCommand{
			Config: cfg,
			Flags:  &LifecycleFlags{DryRun: true},
		}

		err := startCmd.Execute()
		if err != nil {
			t.Fatalf("Start dry-run failed: %v", err)
		}
	})

	// Test 2: Stop command when nothing running (should error)
	t.Run("StopNotRunning", func(t *testing.T) {
		stopCmd := &StopCommand{
			Config: cfg,
			Flags:  &LifecycleFlags{},
		}

		err := stopCmd.Execute()
		if err == nil {
			t.Fatal("Expected error when stopping non-running server")
		}
		if !contains(err.Error(), "not running") {
			t.Errorf("Expected 'not running' error, got: %v", err)
		}
	})

	// Test 3: Start a real process (using sleep as a stand-in)
	t.Run("StartRealProcess", func(t *testing.T) {
		// Use lifecycle.Daemonize directly since StartCommand would try to run brain binary
		pid, err := lifecycle.Daemonize("sleep", []string{"30"}, lifecycle.DaemonOptions{
			PIDFile: pidFile,
			LogFile: logFile,
		})
		if err != nil {
			t.Fatalf("Failed to start process: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		// Verify PID file exists and process is running
		readPID, err := lifecycle.ReadPID(pidFile)
		if err != nil {
			t.Fatalf("Failed to read PID file: %v", err)
		}
		if readPID != pid {
			t.Errorf("PID mismatch: expected %d, got %d", pid, readPID)
		}
		if !lifecycle.IsProcessRunning(pid) {
			t.Fatal("Process not running after start")
		}
	})

	// Test 4: Try to start again (should fail - already running)
	t.Run("StartAlreadyRunning", func(t *testing.T) {
		startCmd := &StartCommand{
			Config: cfg,
			Flags:  &LifecycleFlags{},
		}

		err := startCmd.Execute()
		if err == nil {
			t.Fatal("Expected error when starting already-running server")
		}
		if !contains(err.Error(), "already running") {
			t.Errorf("Expected 'already running' error, got: %v", err)
		}
	})

	// Test 5: Stop the running process
	t.Run("StopRunningProcess", func(t *testing.T) {
		// Read PID before stopping
		pid, err := lifecycle.ReadPID(pidFile)
		if err != nil {
			t.Fatalf("Failed to read PID: %v", err)
		}

		stopCmd := &StopCommand{
			Config: cfg,
			Flags:  &LifecycleFlags{},
		}

		err = stopCmd.Execute()
		if err != nil {
			t.Fatalf("Stop failed: %v", err)
		}

		// Verify process stopped
		time.Sleep(100 * time.Millisecond)
		if lifecycle.IsProcessRunning(pid) {
			t.Error("Process still running after stop")
		}

		// Verify PID file cleaned up
		if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
			t.Error("PID file not cleaned up")
		}
	})

	// Test 6: Restart when not running (should start)
	t.Run("RestartWhenStopped", func(t *testing.T) {
		restartCmd := &RestartCommand{
			Config: cfg,
			Flags:  &LifecycleFlags{DryRun: true},
		}

		err := restartCmd.Execute()
		if err != nil {
			t.Fatalf("Restart failed: %v", err)
		}
	})
}
