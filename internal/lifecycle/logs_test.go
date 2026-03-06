package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRotateLogs_SizeThreshold tests size-based log rotation
func TestRotateLogs_SizeThreshold(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	// Write a log file that exceeds the max size
	content := strings.Repeat("x", 150) // 150 bytes
	if err := os.WriteFile(logPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test log: %v", err)
	}

	// Rotate with maxSize=100 bytes
	if err := RotateLogs(logPath, 100, 5); err != nil {
		t.Fatalf("RotateLogs failed: %v", err)
	}

	// Check that backup was created
	backup1 := logPath + ".1"
	if _, err := os.Stat(backup1); os.IsNotExist(err) {
		t.Errorf("expected backup file %s to exist", backup1)
	}

	// Check that original file is now empty or small
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("failed to stat log file: %v", err)
	}
	if info.Size() > 0 {
		t.Errorf("expected log file to be empty after rotation, got size %d", info.Size())
	}
}

// TestRotateLogs_MaxBackups tests that old backups are removed
func TestRotateLogs_MaxBackups(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	// Create existing backups
	for i := 1; i <= 3; i++ {
		backup := fmt.Sprintf("%s.%d", logPath, i)
		if err := os.WriteFile(backup, []byte("old"), 0644); err != nil {
			t.Fatalf("failed to create backup %d: %v", i, err)
		}
	}

	// Write current log
	content := strings.Repeat("x", 150)
	if err := os.WriteFile(logPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test log: %v", err)
	}

	// Rotate with maxBackups=2
	if err := RotateLogs(logPath, 100, 2); err != nil {
		t.Fatalf("RotateLogs failed: %v", err)
	}

	// Check that only 2 backups exist
	for i := 1; i <= 2; i++ {
		backup := fmt.Sprintf("%s.%d", logPath, i)
		if _, err := os.Stat(backup); os.IsNotExist(err) {
			t.Errorf("expected backup %d to exist", i)
		}
	}

	// Check that backup 3 was removed
	backup3 := logPath + ".3"
	if _, err := os.Stat(backup3); !os.IsNotExist(err) {
		t.Errorf("expected backup 3 to be removed")
	}
}

// TestRotateLogs_NoRotationNeeded tests that rotation doesn't happen if file is small
func TestRotateLogs_NoRotationNeeded(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	// Write a small log file
	content := "small log"
	if err := os.WriteFile(logPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test log: %v", err)
	}

	// Rotate with maxSize=100 bytes (larger than file)
	if err := RotateLogs(logPath, 100, 5); err != nil {
		t.Fatalf("RotateLogs failed: %v", err)
	}

	// Check that no backup was created
	backup1 := logPath + ".1"
	if _, err := os.Stat(backup1); !os.IsNotExist(err) {
		t.Errorf("expected no backup to be created for small file")
	}

	// Check that original file is unchanged
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	if string(data) != content {
		t.Errorf("expected log file content to be unchanged, got %q", string(data))
	}
}

// TestCleanupOldLogs_RemovesOldFiles tests cleanup of logs older than maxAge
func TestCleanupOldLogs_RemovesOldFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create log files with different ages
	now := time.Now()
	files := []struct {
		name string
		age  time.Duration
	}{
		{"test.log.1", 1 * time.Hour},
		{"test.log.2", 25 * time.Hour},
		{"test.log.3", 48 * time.Hour},
		{"test.log.4", 72 * time.Hour},
	}

	for _, f := range files {
		path := filepath.Join(tmpDir, f.name)
		if err := os.WriteFile(path, []byte("log"), 0644); err != nil {
			t.Fatalf("failed to create file %s: %v", f.name, err)
		}
		// Set modification time
		modTime := now.Add(-f.age)
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatalf("failed to set time on %s: %v", f.name, err)
		}
	}

	// Cleanup logs older than 2 days
	if err := CleanupOldLogs(tmpDir, 2*24*time.Hour); err != nil {
		t.Fatalf("CleanupOldLogs failed: %v", err)
	}

	// Check which files remain
	for _, f := range files {
		path := filepath.Join(tmpDir, f.name)
		_, err := os.Stat(path)
		if f.age > 2*24*time.Hour {
			// Should be deleted
			if !os.IsNotExist(err) {
				t.Errorf("expected file %s to be deleted (age %v)", f.name, f.age)
			}
		} else {
			// Should still exist
			if os.IsNotExist(err) {
				t.Errorf("expected file %s to exist (age %v)", f.name, f.age)
			}
		}
	}
}

// TestCleanupOldLogs_OnlyLogsPattern tests that only log files are cleaned
func TestCleanupOldLogs_OnlyLogsPattern(t *testing.T) {
	tmpDir := t.TempDir()

	// Create various files
	now := time.Now()
	oldTime := now.Add(-72 * time.Hour)

	files := []string{
		"brain-api.log.1",
		"brain-api.log.2",
		"config.yaml",
		"data.json",
	}

	for _, name := range files {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
			t.Fatalf("failed to create file %s: %v", name, err)
		}
		if err := os.Chtimes(path, oldTime, oldTime); err != nil {
			t.Fatalf("failed to set time on %s: %v", name, err)
		}
	}

	// Cleanup logs older than 1 day
	if err := CleanupOldLogs(tmpDir, 24*time.Hour); err != nil {
		t.Fatalf("CleanupOldLogs failed: %v", err)
	}

	// Check which files remain
	for _, name := range files {
		path := filepath.Join(tmpDir, name)
		_, err := os.Stat(path)
		if strings.Contains(name, ".log.") {
			// Log files should be deleted
			if !os.IsNotExist(err) {
				t.Errorf("expected log file %s to be deleted", name)
			}
		} else {
			// Non-log files should remain
			if os.IsNotExist(err) {
				t.Errorf("expected non-log file %s to remain", name)
			}
		}
	}
}

// TestParseDuration tests parsing of human-readable durations
func TestParseDuration_Valid(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{"1h", 1 * time.Hour},
		{"30m", 30 * time.Minute},
		{"2d", 48 * time.Hour},
		{"1d", 24 * time.Hour},
		{"45s", 45 * time.Second},
		{"1h30m", 90 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := ParseDuration(tt.input)
			if err != nil {
				t.Fatalf("ParseDuration(%q) failed: %v", tt.input, err)
			}
			if result != tt.expected {
				t.Errorf("ParseDuration(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// TestParseDuration_Invalid tests invalid duration strings
func TestParseDuration_Invalid(t *testing.T) {
	tests := []string{
		"invalid",
		"1x",
		"",
		"abc",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := ParseDuration(input)
			if err == nil {
				t.Errorf("ParseDuration(%q) should fail, but succeeded", input)
			}
		})
	}
}
