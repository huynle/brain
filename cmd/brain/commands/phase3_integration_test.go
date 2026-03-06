package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/huynle/brain-api/internal/lifecycle"
)

// TestHealthCommand_ServerNotRunning tests health command when server is not running.
func TestHealthCommand_ServerNotRunning(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	cfg := &UnifiedConfig{}
	cfg.Server.PIDFile = pidFile
	cfg.Server.Port = 3333

	var buf bytes.Buffer
	cmd := &HealthCommand{
		Config: cfg,
		Flags:  &HealthFlags{},
		Out:    &buf,
	}

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Expected error when server not running")
	}

	output := buf.String()
	if !contains(output, "not running") {
		t.Errorf("Expected output to mention server not running, got: %s", output)
	}
}

// TestLogsCommand_NoLogFile tests logs command when log file doesn't exist.
func TestLogsCommand_NoLogFile(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "nonexistent.log")

	cfg := &UnifiedConfig{}
	cfg.Server.LogFile = logFile

	var buf bytes.Buffer
	cmd := &LogsCommand{
		Config: cfg,
		Flags:  &LogsFlags{Lines: 10},
		Out:    &buf,
	}

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Expected no error when log file missing, got: %v", err)
	}

	output := buf.String()
	if !contains(output, "does not exist") {
		t.Errorf("Expected output to mention file doesn't exist, got: %s", output)
	}
}

// TestLogsCommand_ReadLines tests logs command reading from a log file.
func TestLogsCommand_ReadLines(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	// Create a log file with some content
	content := "Line 1\nLine 2\nLine 3\nLine 4\nLine 5\n"
	if err := os.WriteFile(logFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}

	cfg := &UnifiedConfig{}
	cfg.Server.LogFile = logFile

	var buf bytes.Buffer
	cmd := &LogsCommand{
		Config: cfg,
		Flags:  &LogsFlags{Lines: 3},
		Out:    &buf,
	}

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	output := buf.String()
	// Should show last 3 lines
	if !contains(output, "Line 3") || !contains(output, "Line 4") || !contains(output, "Line 5") {
		t.Errorf("Expected last 3 lines, got: %s", output)
	}
	if contains(output, "Line 1") {
		t.Errorf("Should not contain first line, got: %s", output)
	}
}

// TestStatusCommand_Integration tests full status command flow.
func TestStatusCommand_Integration(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	// Test stopped state
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
	if err == nil {
		t.Fatal("Expected error for stopped server")
	}
	if !contains(buf.String(), "stopped") {
		t.Errorf("Expected stopped status, got: %s", buf.String())
	}

	// Write PID file and test running state
	if err := lifecycle.WritePID(pidFile, os.Getpid()); err != nil {
		t.Fatalf("Failed to write PID: %v", err)
	}

	buf.Reset()
	cmd2 := &StatusCommand{
		Config: cfg,
		Flags:  &StatusFlags{},
		Out:    &buf,
	}

	err = cmd2.Execute()
	if err != nil {
		t.Fatalf("Expected no error for running server, got: %v", err)
	}
	if !contains(buf.String(), "running") {
		t.Errorf("Expected running status, got: %s", buf.String())
	}
}
