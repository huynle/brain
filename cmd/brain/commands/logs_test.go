package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a goroutine-safe bytes.Buffer for use with followLogs, which
// writes from a background goroutine while the test reads.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestLogsCommand_Since(t *testing.T) {
	// Create temp log file with timestamped entries
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	now := time.Now()
	entries := []struct {
		time time.Time
		msg  string
	}{
		{now.Add(-2 * time.Hour), "2 hours ago"},
		{now.Add(-1 * time.Hour), "1 hour ago"},
		{now.Add(-30 * time.Minute), "30 minutes ago"},
		{now.Add(-5 * time.Minute), "5 minutes ago"},
	}

	var logContent strings.Builder
	for _, e := range entries {
		logContent.WriteString(e.time.Format("2006-01-02T15:04:05") + " " + e.msg + "\n")
	}
	if err := os.WriteFile(logFile, []byte(logContent.String()), 0644); err != nil {
		t.Fatalf("failed to write log file: %v", err)
	}

	// Test: show logs since 1 hour
	cfg := &UnifiedConfig{}
	cfg.Server.LogFile = logFile

	var out bytes.Buffer
	cmd := &LogsCommand{
		Config: cfg,
		Flags: &LogsFlags{
			Since: "1h",
			Lines: 100,
		},
		Out: &out,
	}

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	output := out.String()
	// Should include 30m, 5m ago (not 2h or 1h ago - 1h is exactly at cutoff)
	if strings.Contains(output, "1 hour ago") {
		t.Errorf("expected '1 hour ago' to be filtered out (exactly at cutoff)")
	}
	if !strings.Contains(output, "30 minutes ago") {
		t.Errorf("expected '30 minutes ago' in output")
	}
	if !strings.Contains(output, "5 minutes ago") {
		t.Errorf("expected '5 minutes ago' in output")
	}
	if strings.Contains(output, "2 hours ago") {
		t.Errorf("expected '2 hours ago' to be filtered out")
	}
}

func TestLogsCommand_Level(t *testing.T) {
	// Create temp log file with different log levels
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	logContent := `DEBUG: debug message 1
INFO: info message 1
WARN: warning message 1
ERROR: error message 1
DEBUG: debug message 2
INFO: info message 2
`
	if err := os.WriteFile(logFile, []byte(logContent), 0644); err != nil {
		t.Fatalf("failed to write log file: %v", err)
	}

	tests := []struct {
		level    string
		expected []string
		excluded []string
	}{
		{
			level:    "error",
			expected: []string{"error message 1"},
			excluded: []string{"debug message", "info message", "warning message"},
		},
		{
			level:    "warn",
			expected: []string{"warning message 1"},
			excluded: []string{"debug message", "info message", "error message"},
		},
		{
			level:    "debug",
			expected: []string{"debug message 1", "debug message 2"},
			excluded: []string{"info message", "warning message", "error message"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			cfg := &UnifiedConfig{}
			cfg.Server.LogFile = logFile

			var out bytes.Buffer
			cmd := &LogsCommand{
				Config: cfg,
				Flags: &LogsFlags{
					Level: tt.level,
					Lines: 100,
				},
				Out: &out,
			}

			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute failed: %v", err)
			}

			output := out.String()
			for _, exp := range tt.expected {
				if !strings.Contains(output, exp) {
					t.Errorf("expected %q in output", exp)
				}
			}
			for _, exc := range tt.excluded {
				if strings.Contains(output, exc) {
					t.Errorf("expected %q to be filtered out", exc)
				}
			}
		})
	}
}

func TestLogsCommand_InvalidSince(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")
	os.WriteFile(logFile, []byte("test\n"), 0644)

	cfg := &UnifiedConfig{}
	cfg.Server.LogFile = logFile

	cmd := &LogsCommand{
		Config: cfg,
		Flags: &LogsFlags{
			Since: "invalid",
		},
	}

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid duration")
	}
	if !strings.Contains(err.Error(), "invalid duration") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestLogsCommand_FollowTailsAppends is the regression test for the bug where
// `brain api logs -f` reused a single bufio.Scanner that latched EOF and never
// surfaced lines appended after the reader caught up.
func TestLogsCommand_FollowTailsAppends(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")
	if err := os.WriteFile(logFile, []byte("existing line\n"), 0644); err != nil {
		t.Fatalf("failed to write log file: %v", err)
	}

	f, err := os.Open(logFile)
	if err != nil {
		t.Fatalf("failed to open log file: %v", err)
	}
	defer f.Close()

	out := &syncBuffer{}
	cmd := &LogsCommand{
		Config: &UnifiedConfig{},
		Flags:  &LogsFlags{Follow: true, Lines: 100},
		Out:    out,
	}

	// followLogs blocks forever; run it in a goroutine (it leaks, which is fine
	// for a unit test — it is blocked on a file read and dies with the process).
	go cmd.followLogs(f, 0)

	// Let it show existing content and reach EOF, then append after EOF — the
	// exact ordering that latched the old Scanner.
	waitForOutput(t, out, "existing line")

	af, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("failed to reopen for append: %v", err)
	}
	if _, err := af.WriteString("appended after eof\n"); err != nil {
		t.Fatalf("append failed: %v", err)
	}
	af.Close()

	waitForOutput(t, out, "appended after eof")
}

// waitForOutput polls the buffer until it contains want or the deadline passes.
func waitForOutput(t *testing.T, out *syncBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in follow output; got: %q", want, out.String())
}
