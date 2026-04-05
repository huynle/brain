package runner

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Test Helpers
// =============================================================================

// logMockClient captures PostTaskLogs calls for assertions.
type logMockClient struct {
	mockClient
	mu       sync.Mutex
	logCalls []logCall
	logErr   error
}

type logCall struct {
	ProjectID string
	TaskID    string
	RunnerID  string
	Lines     []types.LogLine
}

func (m *logMockClient) PostTaskLogs(ctx context.Context, projectID, taskID, runnerID string, lines []types.LogLine) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logCalls = append(m.logCalls, logCall{
		ProjectID: projectID,
		TaskID:    taskID,
		RunnerID:  runnerID,
		Lines:     lines,
	})
	return m.logErr
}

func (m *logMockClient) getLogCalls() []logCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]logCall, len(m.logCalls))
	copy(result, m.logCalls)
	return result
}

func (m *logMockClient) totalLinesPosted() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := 0
	for _, c := range m.logCalls {
		total += len(c.Lines)
	}
	return total
}

// =============================================================================
// detectLevel tests
// =============================================================================

func TestDetectLevel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"normal log line", "info"},
		{"ERROR: something failed", "error"},
		{"fatal error occurred", "error"},
		{"panic: runtime error", "error"},
		{"warning: deprecated function", "warn"},
		{"WARN: low disk space", "warn"},
		{"just info", "info"},
		{"", "info"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := detectLevel(tt.input)
			if got != tt.want {
				t.Errorf("detectLevel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// =============================================================================
// LogStreamer Write tests
// =============================================================================

func TestLogStreamer_WriteCompleteLines(t *testing.T) {
	client := &logMockClient{
		mockClient: *newMockClient(),
	}

	ls := NewLogStreamer(LogStreamerConfig{
		Client:        client,
		RunnerID:      "runner-1",
		ProjectID:     "proj-1",
		TaskID:        "task-1",
		BatchSize:     50,
		FlushInterval: 50 * time.Millisecond, // short interval for tests
	})

	// Write two complete lines
	n, err := ls.Write([]byte("line one\nline two\n"))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != 18 {
		t.Errorf("Write returned %d, want 18", n)
	}

	// Wait for flush
	time.Sleep(150 * time.Millisecond)

	calls := client.getLogCalls()
	if len(calls) == 0 {
		t.Fatal("expected at least one PostTaskLogs call")
	}

	// Verify total lines posted
	total := client.totalLinesPosted()
	if total != 2 {
		t.Errorf("total lines posted = %d, want 2", total)
	}

	// Verify metadata
	call := calls[0]
	if call.ProjectID != "proj-1" {
		t.Errorf("ProjectID = %q, want %q", call.ProjectID, "proj-1")
	}
	if call.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", call.TaskID, "task-1")
	}
	if call.RunnerID != "runner-1" {
		t.Errorf("RunnerID = %q, want %q", call.RunnerID, "runner-1")
	}

	ls.Stop()
}

func TestLogStreamer_PartialLines(t *testing.T) {
	client := &logMockClient{
		mockClient: *newMockClient(),
	}

	ls := NewLogStreamer(LogStreamerConfig{
		Client:        client,
		RunnerID:      "runner-1",
		ProjectID:     "proj-1",
		TaskID:        "task-1",
		BatchSize:     50,
		FlushInterval: 50 * time.Millisecond,
	})

	// Write partial line (no newline)
	ls.Write([]byte("partial"))
	time.Sleep(100 * time.Millisecond)

	// Partial should not be flushed yet
	total := client.totalLinesPosted()
	if total != 0 {
		t.Errorf("partial line was flushed prematurely, total = %d", total)
	}

	// Complete the line
	ls.Write([]byte(" complete\n"))
	time.Sleep(100 * time.Millisecond)

	total = client.totalLinesPosted()
	if total != 1 {
		t.Errorf("total lines posted = %d, want 1", total)
	}

	// Verify the line content
	calls := client.getLogCalls()
	if len(calls) > 0 && len(calls[0].Lines) > 0 {
		if calls[0].Lines[0].Content != "partial complete" {
			t.Errorf("content = %q, want %q", calls[0].Lines[0].Content, "partial complete")
		}
	}

	ls.Stop()
}

func TestLogStreamer_BatchSizeFlush(t *testing.T) {
	client := &logMockClient{
		mockClient: *newMockClient(),
	}

	ls := NewLogStreamer(LogStreamerConfig{
		Client:        client,
		RunnerID:      "runner-1",
		ProjectID:     "proj-1",
		TaskID:        "task-1",
		BatchSize:     3,                // small batch size
		FlushInterval: 10 * time.Second, // long interval — batch size should trigger flush
	})

	// Write 3 lines (hit batch size)
	ls.Write([]byte("one\ntwo\nthree\n"))

	// Give the flush goroutine time to process
	time.Sleep(100 * time.Millisecond)

	total := client.totalLinesPosted()
	if total != 3 {
		t.Errorf("total lines posted = %d, want 3 (batch flush)", total)
	}

	ls.Stop()
}

func TestLogStreamer_StopFlushesRemaining(t *testing.T) {
	client := &logMockClient{
		mockClient: *newMockClient(),
	}

	ls := NewLogStreamer(LogStreamerConfig{
		Client:        client,
		RunnerID:      "runner-1",
		ProjectID:     "proj-1",
		TaskID:        "task-1",
		BatchSize:     50,
		FlushInterval: 10 * time.Second, // long interval
	})

	// Write partial line + complete line
	ls.Write([]byte("complete line\npartial"))

	// Stop should flush everything including the partial line
	ls.Stop()

	total := client.totalLinesPosted()
	if total != 2 {
		t.Errorf("total lines posted = %d, want 2 (including partial flushed on stop)", total)
	}

	// Verify the partial line was included
	calls := client.getLogCalls()
	var foundPartial bool
	for _, call := range calls {
		for _, line := range call.Lines {
			if line.Content == "partial" {
				foundPartial = true
			}
		}
	}
	if !foundPartial {
		t.Error("expected partial line to be flushed on Stop()")
	}
}

func TestLogStreamer_LevelDetection(t *testing.T) {
	client := &logMockClient{
		mockClient: *newMockClient(),
	}

	ls := NewLogStreamer(LogStreamerConfig{
		Client:        client,
		RunnerID:      "runner-1",
		ProjectID:     "proj-1",
		TaskID:        "task-1",
		BatchSize:     50,
		FlushInterval: 50 * time.Millisecond,
	})

	ls.Write([]byte("normal info line\nERROR: something broke\nwarning: watch out\n"))
	time.Sleep(100 * time.Millisecond)
	ls.Stop()

	calls := client.getLogCalls()
	var lines []types.LogLine
	for _, c := range calls {
		lines = append(lines, c.Lines...)
	}

	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	if lines[0].Level != "info" {
		t.Errorf("lines[0].Level = %q, want %q", lines[0].Level, "info")
	}
	if lines[1].Level != "error" {
		t.Errorf("lines[1].Level = %q, want %q", lines[1].Level, "error")
	}
	if lines[2].Level != "warn" {
		t.Errorf("lines[2].Level = %q, want %q", lines[2].Level, "warn")
	}
}

func TestLogStreamer_NonBlockingOnPostFailure(t *testing.T) {
	client := &logMockClient{
		mockClient: *newMockClient(),
		logErr:     &APIError{StatusCode: 500, Body: "server error"},
	}

	ls := NewLogStreamer(LogStreamerConfig{
		Client:        client,
		RunnerID:      "runner-1",
		ProjectID:     "proj-1",
		TaskID:        "task-1",
		BatchSize:     50,
		FlushInterval: 50 * time.Millisecond,
	})

	// Write should succeed even if POST fails
	n, err := ls.Write([]byte("log line\n"))
	if err != nil {
		t.Fatalf("Write should not return error, got: %v", err)
	}
	if n != 9 {
		t.Errorf("Write returned %d, want 9", n)
	}

	// Wait for flush attempt
	time.Sleep(100 * time.Millisecond)

	// Verify the POST was attempted
	calls := client.getLogCalls()
	if len(calls) == 0 {
		t.Error("expected PostTaskLogs to be called despite error")
	}

	// Write more — should still work
	n, err = ls.Write([]byte("another line\n"))
	if err != nil {
		t.Fatalf("subsequent Write should not fail: %v", err)
	}
	if n != 13 {
		t.Errorf("Write returned %d, want 13", n)
	}

	ls.Stop()
}

func TestLogStreamer_EmptyWrite(t *testing.T) {
	client := &logMockClient{
		mockClient: *newMockClient(),
	}

	ls := NewLogStreamer(LogStreamerConfig{
		Client:        client,
		RunnerID:      "runner-1",
		ProjectID:     "proj-1",
		TaskID:        "task-1",
		FlushInterval: 50 * time.Millisecond,
	})

	n, err := ls.Write([]byte{})
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != 0 {
		t.Errorf("Write returned %d, want 0", n)
	}

	time.Sleep(100 * time.Millisecond)
	ls.Stop()

	total := client.totalLinesPosted()
	if total != 0 {
		t.Errorf("expected 0 lines posted for empty write, got %d", total)
	}
}

func TestLogStreamer_TimestampPopulated(t *testing.T) {
	client := &logMockClient{
		mockClient: *newMockClient(),
	}

	ls := NewLogStreamer(LogStreamerConfig{
		Client:        client,
		RunnerID:      "runner-1",
		ProjectID:     "proj-1",
		TaskID:        "task-1",
		FlushInterval: 50 * time.Millisecond,
	})

	ls.Write([]byte("hello\n"))
	time.Sleep(100 * time.Millisecond)
	ls.Stop()

	calls := client.getLogCalls()
	if len(calls) == 0 || len(calls[0].Lines) == 0 {
		t.Fatal("expected at least one line")
	}

	ts := calls[0].Lines[0].Timestamp
	if ts == "" {
		t.Error("expected non-empty timestamp")
	}

	// Verify it's valid RFC3339
	_, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t.Errorf("timestamp %q is not valid RFC3339: %v", ts, err)
	}
}
