package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// LogEntry - Data Model Fields
// =============================================================================

func TestLogEntry_HasProjectIDField(t *testing.T) {
	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   "test message",
		TaskID:    "abc12def",
		ProjectID: "my-project",
	}

	if entry.ProjectID != "my-project" {
		t.Errorf("expected ProjectID 'my-project', got '%s'", entry.ProjectID)
	}
}

func TestLogEntry_HasContextField(t *testing.T) {
	ctx := map[string]interface{}{
		"component": "runner",
		"attempt":   float64(3),
	}
	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   "test message",
		Context:   ctx,
	}

	if entry.Context == nil {
		t.Fatal("expected Context to be non-nil")
	}
	if entry.Context["component"] != "runner" {
		t.Errorf("expected Context['component'] = 'runner', got '%v'", entry.Context["component"])
	}
	if entry.Context["attempt"] != float64(3) {
		t.Errorf("expected Context['attempt'] = 3, got '%v'", entry.Context["attempt"])
	}
}

func TestLogEntry_ContextCanBeNil(t *testing.T) {
	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   "test message",
	}

	if entry.Context != nil {
		t.Errorf("expected Context to be nil by default, got %v", entry.Context)
	}
}

// =============================================================================
// Config - LogDir Field
// =============================================================================

func TestConfig_HasLogDirField(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
		LogDir:  "/tmp/brain-logs",
	}

	if cfg.LogDir != "/tmp/brain-logs" {
		t.Errorf("expected LogDir '/tmp/brain-logs', got '%s'", cfg.LogDir)
	}
}

func TestConfig_LogDirDefaultsToEmpty(t *testing.T) {
	cfg := Config{
		APIURL: "http://localhost:3333",
	}

	if cfg.LogDir != "" {
		t.Errorf("expected LogDir to default to empty string, got '%s'", cfg.LogDir)
	}
}

// =============================================================================
// LogViewer - Empty State
// =============================================================================

func TestLogViewer_Empty_ShowsPlaceholder(t *testing.T) {
	lv := NewLogViewer(100)
	lv.SetSize(80, 20)

	view := lv.View()

	if !strings.Contains(view, "No logs") {
		t.Errorf("expected 'No logs' placeholder, got:\n%s", view)
	}
}

func TestLogViewer_Empty_ShowsHeader(t *testing.T) {
	lv := NewLogViewer(100)
	lv.SetSize(80, 20)

	view := lv.View()

	if !strings.Contains(view, "Logs") {
		t.Errorf("expected 'Logs' header, got:\n%s", view)
	}
}

// =============================================================================
// LogViewer - AddEntry
// =============================================================================

func TestLogViewer_AddEntry_DisplaysEntry(t *testing.T) {
	lv := NewLogViewer(100)
	lv.SetSize(80, 20)

	entry := LogEntry{
		Timestamp: time.Date(2024, 1, 15, 14, 30, 45, 0, time.UTC),
		Level:     "info",
		Message:   "Server started",
	}
	lv.AddEntry(entry)

	view := lv.View()

	if !strings.Contains(view, "14:30:45") {
		t.Errorf("expected timestamp '14:30:45' in view, got:\n%s", view)
	}
	if !strings.Contains(view, "INFO") {
		t.Errorf("expected level 'INFO' in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Server started") {
		t.Errorf("expected message 'Server started' in view, got:\n%s", view)
	}
}

func TestLogViewer_AddMultipleEntries(t *testing.T) {
	lv := NewLogViewer(100)
	lv.SetSize(80, 20)

	lv.AddEntry(LogEntry{
		Timestamp: time.Date(2024, 1, 15, 14, 30, 45, 0, time.UTC),
		Level:     "info",
		Message:   "First message",
	})
	lv.AddEntry(LogEntry{
		Timestamp: time.Date(2024, 1, 15, 14, 30, 46, 0, time.UTC),
		Level:     "warn",
		Message:   "Second message",
	})

	view := lv.View()

	if !strings.Contains(view, "First message") {
		t.Errorf("expected 'First message' in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Second message") {
		t.Errorf("expected 'Second message' in view, got:\n%s", view)
	}
}

// =============================================================================
// LogViewer - Circular Buffer
// =============================================================================

func TestLogViewer_CircularBuffer_EvictsOldEntries(t *testing.T) {
	lv := NewLogViewer(3) // max 3 entries
	lv.SetSize(80, 20)

	for i := 0; i < 5; i++ {
		lv.AddEntry(LogEntry{
			Timestamp: time.Date(2024, 1, 15, 14, 30, i, 0, time.UTC),
			Level:     "info",
			Message:   "msg" + string(rune('A'+i)),
		})
	}

	// Should only have last 3 entries
	if len(lv.entries) != 3 {
		t.Errorf("expected 3 entries after overflow, got %d", len(lv.entries))
	}

	view := lv.View()

	// First two entries should be evicted
	if strings.Contains(view, "msgA") {
		t.Errorf("expected first entry to be evicted, got:\n%s", view)
	}
	if strings.Contains(view, "msgB") {
		t.Errorf("expected second entry to be evicted, got:\n%s", view)
	}
	// Last three should remain
	if !strings.Contains(view, "msgC") {
		t.Errorf("expected 'msgC' to remain, got:\n%s", view)
	}
}

// =============================================================================
// LogViewer - Color Coding Per Level
// =============================================================================

func TestLogViewer_LevelLabels(t *testing.T) {
	lv := NewLogViewer(100)
	lv.SetSize(80, 20)

	levels := []struct {
		level    string
		expected string
	}{
		{"debug", "DEBUG"},
		{"info", "INFO"},
		{"warn", "WARN"},
		{"error", "ERROR"},
	}

	for _, tt := range levels {
		lv.AddEntry(LogEntry{
			Timestamp: time.Date(2024, 1, 15, 14, 30, 0, 0, time.UTC),
			Level:     tt.level,
			Message:   "test " + tt.level,
		})
	}

	view := lv.View()

	for _, tt := range levels {
		if !strings.Contains(view, tt.expected) {
			t.Errorf("expected level label '%s' in view, got:\n%s", tt.expected, view)
		}
	}
}

// =============================================================================
// LogViewer - Timestamp Formatting
// =============================================================================

func TestLogViewer_TimestampFormat_HHMMSS(t *testing.T) {
	lv := NewLogViewer(100)
	lv.SetSize(80, 20)

	lv.AddEntry(LogEntry{
		Timestamp: time.Date(2024, 1, 15, 9, 5, 3, 0, time.UTC),
		Level:     "info",
		Message:   "test",
	})

	view := lv.View()

	// Should be zero-padded HH:MM:SS
	if !strings.Contains(view, "09:05:03") {
		t.Errorf("expected timestamp '09:05:03' in view, got:\n%s", view)
	}
}

// =============================================================================
// LogViewer - SetSize
// =============================================================================

func TestLogViewer_SetSize_UpdatesDimensions(t *testing.T) {
	lv := NewLogViewer(100)
	lv.SetSize(120, 40)

	if lv.width != 120 {
		t.Errorf("expected width 120, got %d", lv.width)
	}
	if lv.height != 40 {
		t.Errorf("expected height 40, got %d", lv.height)
	}
}

// =============================================================================
// LogViewer - Auto-Follow
// =============================================================================

func TestLogViewer_AutoFollow_DefaultTrue(t *testing.T) {
	lv := NewLogViewer(100)

	if !lv.autoFollow {
		t.Error("expected autoFollow to be true by default")
	}
}

// =============================================================================
// LogViewer - Message Truncation
// =============================================================================

func TestLogViewer_LongMessage_Truncated(t *testing.T) {
	lv := NewLogViewer(100)
	lv.SetSize(80, 20)

	longMsg := strings.Repeat("x", 200)
	lv.AddEntry(LogEntry{
		Timestamp: time.Date(2024, 1, 15, 14, 30, 0, 0, time.UTC),
		Level:     "info",
		Message:   longMsg,
	})

	view := lv.View()

	// Should be truncated (not contain the full 200-char message)
	if strings.Contains(view, longMsg) {
		t.Errorf("expected long message to be truncated, got full message in view")
	}
	// Should contain ellipsis
	if !strings.Contains(view, "...") {
		t.Errorf("expected '...' truncation indicator in view, got:\n%s", view)
	}
}

// =============================================================================
// LogViewer - Entry Count
// =============================================================================

func TestLogViewer_EntryCount(t *testing.T) {
	lv := NewLogViewer(100)

	if lv.EntryCount() != 0 {
		t.Errorf("expected 0 entries initially, got %d", lv.EntryCount())
	}

	lv.AddEntry(LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   "test",
	})

	if lv.EntryCount() != 1 {
		t.Errorf("expected 1 entry after add, got %d", lv.EntryCount())
	}
}

// =============================================================================
// Persistence - SetLogFile
// =============================================================================

func TestSetLogFile(t *testing.T) {
	lv := NewLogViewer(100)
	lv.SetLogFile("/tmp/test.jsonl")

	if lv.logFile != "/tmp/test.jsonl" {
		t.Errorf("expected logFile '/tmp/test.jsonl', got '%s'", lv.logFile)
	}
}

// =============================================================================
// Persistence - Serialize / Deserialize
// =============================================================================

func TestSerializeEntry_RoundTrip(t *testing.T) {
	original := LogEntry{
		Timestamp: time.Date(2024, 1, 15, 14, 30, 45, 0, time.UTC),
		Level:     "info",
		Message:   "Server started on port 3333",
		TaskID:    "abc12def",
		ProjectID: "my-project",
		Context: map[string]interface{}{
			"component": "runner",
			"attempt":   float64(3),
		},
	}

	lv := NewLogViewer(100)
	serialized := lv.serializeEntry(original)

	// Verify it's valid JSON
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(serialized), &raw); err != nil {
		t.Fatalf("serializeEntry produced invalid JSON: %v", err)
	}

	// Verify expected keys exist
	for _, key := range []string{"timestamp", "level", "message", "taskId", "projectId", "context"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected key '%s' in JSON output", key)
		}
	}

	// Round-trip: deserialize back
	restored, err := lv.deserializeEntry(serialized)
	if err != nil {
		t.Fatalf("deserializeEntry failed: %v", err)
	}

	if !restored.Timestamp.Equal(original.Timestamp) {
		t.Errorf("timestamp mismatch: got %v, want %v", restored.Timestamp, original.Timestamp)
	}
	if restored.Level != original.Level {
		t.Errorf("level mismatch: got '%s', want '%s'", restored.Level, original.Level)
	}
	if restored.Message != original.Message {
		t.Errorf("message mismatch: got '%s', want '%s'", restored.Message, original.Message)
	}
	if restored.TaskID != original.TaskID {
		t.Errorf("taskID mismatch: got '%s', want '%s'", restored.TaskID, original.TaskID)
	}
	if restored.ProjectID != original.ProjectID {
		t.Errorf("projectID mismatch: got '%s', want '%s'", restored.ProjectID, original.ProjectID)
	}
	if restored.Context["component"] != "runner" {
		t.Errorf("context component mismatch: got '%v'", restored.Context["component"])
	}
}

func TestDeserializeEntry_InvalidJSON(t *testing.T) {
	lv := NewLogViewer(100)
	_, err := lv.deserializeEntry("not valid json {{{")
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestDeserializeEntry_MissingRequiredFields(t *testing.T) {
	lv := NewLogViewer(100)

	tests := []struct {
		name string
		json string
	}{
		{"missing timestamp", `{"level":"info","message":"test"}`},
		{"missing level", `{"timestamp":"2024-01-15T14:30:45Z","message":"test"}`},
		{"missing message", `{"timestamp":"2024-01-15T14:30:45Z","level":"info"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := lv.deserializeEntry(tt.json)
			if err == nil {
				t.Errorf("expected error for %s, got nil", tt.name)
			}
		})
	}
}

// =============================================================================
// Persistence - LoadFromFile
// =============================================================================

func TestLoadFromFile_NonexistentFile(t *testing.T) {
	lv := NewLogViewer(100)
	lv.SetLogFile(filepath.Join(t.TempDir(), "nonexistent.jsonl"))

	err := lv.LoadFromFile()
	if err != nil {
		t.Errorf("expected nil error for nonexistent file, got: %v", err)
	}
	if lv.EntryCount() != 0 {
		t.Errorf("expected 0 entries, got %d", lv.EntryCount())
	}
}

func TestLoadFromFile_ExistingLogs(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.jsonl")

	// Write JSONL file
	lines := []string{
		`{"timestamp":"2024-01-15T14:30:45Z","level":"info","message":"first"}`,
		`{"timestamp":"2024-01-15T14:30:46Z","level":"warn","message":"second"}`,
		`{"timestamp":"2024-01-15T14:30:47Z","level":"error","message":"third"}`,
	}
	if err := os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	lv := NewLogViewer(100)
	lv.SetLogFile(logPath)

	if err := lv.LoadFromFile(); err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if lv.EntryCount() != 3 {
		t.Errorf("expected 3 entries, got %d", lv.EntryCount())
	}

	// Verify entries loaded correctly
	if lv.entries[0].Message != "first" {
		t.Errorf("expected first entry message 'first', got '%s'", lv.entries[0].Message)
	}
	if lv.entries[2].Level != "error" {
		t.Errorf("expected third entry level 'error', got '%s'", lv.entries[2].Level)
	}
}

func TestLoadFromFile_RespectsMaxEntries(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.jsonl")

	// Write 10 lines
	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, `{"timestamp":"2024-01-15T14:30:45Z","level":"info","message":"msg`+string(rune('A'+i))+`"}`)
	}
	if err := os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	lv := NewLogViewer(3) // max 3
	lv.SetLogFile(logPath)

	if err := lv.LoadFromFile(); err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if lv.EntryCount() != 3 {
		t.Errorf("expected 3 entries (maxEntries), got %d", lv.EntryCount())
	}

	// Should keep the LAST 3 entries (H, I, J)
	if lv.entries[0].Message != "msgH" {
		t.Errorf("expected first kept entry 'msgH', got '%s'", lv.entries[0].Message)
	}
}

func TestLoadFromFile_SkipsInvalidLines(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.jsonl")

	lines := []string{
		`{"timestamp":"2024-01-15T14:30:45Z","level":"info","message":"valid1"}`,
		`not valid json`,
		`{"timestamp":"2024-01-15T14:30:46Z","level":"warn","message":"valid2"}`,
		``,
		`{"timestamp":"2024-01-15T14:30:47Z","level":"error","message":"valid3"}`,
	}
	if err := os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	lv := NewLogViewer(100)
	lv.SetLogFile(logPath)

	if err := lv.LoadFromFile(); err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if lv.EntryCount() != 3 {
		t.Errorf("expected 3 valid entries (skipping invalid), got %d", lv.EntryCount())
	}
}

// =============================================================================
// Persistence - AppendToFile
// =============================================================================

func TestAppendToFile_CreatesDirectoryAndFile(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "subdir", "nested", "test.jsonl")

	lv := NewLogViewer(100)
	lv.SetLogFile(logPath)

	entry := LogEntry{
		Timestamp: time.Date(2024, 1, 15, 14, 30, 45, 0, time.UTC),
		Level:     "info",
		Message:   "test message",
	}

	if err := lv.appendToFile(entry); err != nil {
		t.Fatalf("appendToFile failed: %v", err)
	}

	// Verify file exists and has content
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected non-empty file")
	}

	// Verify it's valid JSONL
	var raw map[string]interface{}
	if err := json.Unmarshal(data[:len(data)-1], &raw); err != nil { // -1 for trailing newline
		t.Errorf("file content is not valid JSON: %v", err)
	}
}

func TestAppendToFile_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.jsonl")

	lv := NewLogViewer(100)
	lv.SetLogFile(logPath)

	entry1 := LogEntry{
		Timestamp: time.Date(2024, 1, 15, 14, 30, 45, 0, time.UTC),
		Level:     "info",
		Message:   "first",
	}
	entry2 := LogEntry{
		Timestamp: time.Date(2024, 1, 15, 14, 30, 46, 0, time.UTC),
		Level:     "warn",
		Message:   "second",
	}

	if err := lv.appendToFile(entry1); err != nil {
		t.Fatalf("first appendToFile failed: %v", err)
	}
	if err := lv.appendToFile(entry2); err != nil {
		t.Fatalf("second appendToFile failed: %v", err)
	}

	// Read file and count lines
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}
}

func TestAppendToFile_NoOpWhenNoLogFile(t *testing.T) {
	lv := NewLogViewer(100)
	// logFile is empty string (default)

	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   "test",
	}

	err := lv.appendToFile(entry)
	if err != nil {
		t.Errorf("expected nil error when logFile is empty, got: %v", err)
	}
}

// =============================================================================
// Persistence - TruncateFile
// =============================================================================

func TestTruncateFile_OnlyAt2xThreshold(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.jsonl")

	maxEntries := 5
	lv := NewLogViewer(maxEntries)
	lv.SetLogFile(logPath)

	// Write 9 lines (less than 2x5=10) — should NOT truncate
	var lines []string
	for i := 0; i < 9; i++ {
		lines = append(lines, `{"timestamp":"2024-01-15T14:30:45Z","level":"info","message":"msg`+string(rune('A'+i))+`"}`)
	}
	if err := os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := lv.TruncateFile(); err != nil {
		t.Fatalf("TruncateFile failed: %v", err)
	}

	// File should still have 9 lines
	data, _ := os.ReadFile(logPath)
	resultLines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(resultLines) != 9 {
		t.Errorf("expected 9 lines (below threshold), got %d", len(resultLines))
	}

	// Now write 10 lines (exactly 2x5=10) — SHOULD truncate
	lines = nil
	for i := 0; i < 10; i++ {
		lines = append(lines, `{"timestamp":"2024-01-15T14:30:45Z","level":"info","message":"line`+string(rune('A'+i))+`"}`)
	}
	if err := os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := lv.TruncateFile(); err != nil {
		t.Fatalf("TruncateFile failed: %v", err)
	}

	// File should now have maxEntries (5) lines
	data, _ = os.ReadFile(logPath)
	resultLines = strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(resultLines) != 5 {
		t.Errorf("expected %d lines after truncation, got %d", maxEntries, len(resultLines))
	}
}

func TestTruncateFile_KeepsLatestEntries(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.jsonl")

	maxEntries := 3
	lv := NewLogViewer(maxEntries)
	lv.SetLogFile(logPath)

	// Write 6 lines (2x3=6, triggers truncation)
	var lines []string
	for i := 0; i < 6; i++ {
		lines = append(lines, `{"timestamp":"2024-01-15T14:30:4`+string(rune('0'+i))+`Z","level":"info","message":"entry`+string(rune('A'+i))+`"}`)
	}
	if err := os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := lv.TruncateFile(); err != nil {
		t.Fatalf("TruncateFile failed: %v", err)
	}

	// Read back and verify we kept the LAST 3 entries (D, E, F)
	data, _ := os.ReadFile(logPath)
	resultLines := strings.Split(strings.TrimSpace(string(data)), "\n")

	if len(resultLines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(resultLines))
	}

	// First kept line should be entryD
	if !strings.Contains(resultLines[0], "entryD") {
		t.Errorf("expected first kept entry to contain 'entryD', got: %s", resultLines[0])
	}
	// Last kept line should be entryF
	if !strings.Contains(resultLines[2], "entryF") {
		t.Errorf("expected last kept entry to contain 'entryF', got: %s", resultLines[2])
	}
}

// =============================================================================
// Phase 3: Integration - AddEntry Persists to File
// =============================================================================

func TestAddEntry_PersistsToFile(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test-project", "tui-logs.jsonl")

	lv := NewLogViewer(100)
	lv.SetLogFile(logPath)

	entry := LogEntry{
		Timestamp: time.Date(2024, 1, 15, 14, 30, 45, 0, time.UTC),
		Level:     "info",
		Message:   "task started",
		TaskID:    "abc12def",
		ProjectID: "test-project",
	}

	lv.AddEntry(entry)

	// Verify the entry was added to in-memory slice
	if lv.EntryCount() != 1 {
		t.Fatalf("expected 1 entry in memory, got %d", lv.EntryCount())
	}

	// Verify the entry was persisted to disk
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected log file to exist after AddEntry, got error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line in log file, got %d", len(lines))
	}

	// Verify the line is valid JSONL with correct content
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &raw); err != nil {
		t.Fatalf("log file line is not valid JSON: %v", err)
	}
	if raw["message"] != "task started" {
		t.Errorf("expected message 'task started', got '%v'", raw["message"])
	}
	if raw["taskId"] != "abc12def" {
		t.Errorf("expected taskId 'abc12def', got '%v'", raw["taskId"])
	}
}

// =============================================================================
// Phase 3: Integration - LogViewer with LogFile Loads Existing Entries
// =============================================================================

func TestNewLogViewer_WithLogFile_LoadsExisting(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "my-project", "tui-logs.jsonl")

	// Create directory and write JSONL file with existing entries
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2024-01-15T14:30:45Z","level":"info","message":"existing entry 1","projectId":"my-project"}`,
		`{"timestamp":"2024-01-15T14:30:46Z","level":"warn","message":"existing entry 2","taskId":"xyz98765"}`,
		`{"timestamp":"2024-01-15T14:30:47Z","level":"error","message":"existing entry 3"}`,
	}
	if err := os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create LogViewer, set log file, and load
	lv := NewLogViewer(100)
	lv.SetLogFile(logPath)
	if err := lv.LoadFromFile(); err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	// Verify entries were loaded
	if lv.EntryCount() != 3 {
		t.Fatalf("expected 3 entries loaded from file, got %d", lv.EntryCount())
	}

	// Verify content
	if lv.entries[0].Message != "existing entry 1" {
		t.Errorf("expected first entry message 'existing entry 1', got '%s'", lv.entries[0].Message)
	}
	if lv.entries[0].ProjectID != "my-project" {
		t.Errorf("expected first entry projectId 'my-project', got '%s'", lv.entries[0].ProjectID)
	}
	if lv.entries[1].TaskID != "xyz98765" {
		t.Errorf("expected second entry taskId 'xyz98765', got '%s'", lv.entries[1].TaskID)
	}
	if lv.entries[2].Level != "error" {
		t.Errorf("expected third entry level 'error', got '%s'", lv.entries[2].Level)
	}

	// Verify the view renders the loaded entries
	lv.SetSize(80, 20)
	view := lv.View()
	if !strings.Contains(view, "existing entry 1") {
		t.Errorf("expected view to contain 'existing entry 1', got:\n%s", view)
	}
}

// =============================================================================
// Phase 3: Integration - NewModel Wires LogFile Path
// =============================================================================

func TestNewModel_WithLogDir_SetsLogFilePath(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
		LogDir:  dir,
	}
	m := NewModel(cfg)

	expectedPath := filepath.Join(dir, "test-project", "tui-logs.jsonl")
	if m.logViewer.logFile != expectedPath {
		t.Errorf("expected logFile '%s', got '%s'", expectedPath, m.logViewer.logFile)
	}
}

func TestNewModel_WithoutLogDir_SetsDefaultLogFilePath(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
		// LogDir is empty — should use default
	}
	m := NewModel(cfg)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}

	expectedPath := filepath.Join(homeDir, ".local", "log", "brain-runner", "test-project", "tui-logs.jsonl")
	if m.logViewer.logFile != expectedPath {
		t.Errorf("expected logFile '%s', got '%s'", expectedPath, m.logViewer.logFile)
	}
}

func TestNewModel_LoadsExistingLogFile(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test-project", "tui-logs.jsonl")

	// Create directory and write existing log entries
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2024-01-15T14:30:45Z","level":"info","message":"pre-existing log"}`,
	}
	if err := os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
		LogDir:  dir,
	}
	m := NewModel(cfg)

	// Should have loaded the existing entry
	if m.logViewer.EntryCount() != 1 {
		t.Errorf("expected 1 entry loaded from existing log file, got %d", m.logViewer.EntryCount())
	}
	if m.logViewer.entries[0].Message != "pre-existing log" {
		t.Errorf("expected message 'pre-existing log', got '%s'", m.logViewer.entries[0].Message)
	}
}

// =============================================================================
// Phase 3: Integration - Truncation Counter Logic
// =============================================================================

func TestTruncateCounter_TriggersAt150(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.jsonl")

	maxEntries := 5
	lv := NewLogViewer(maxEntries)
	lv.SetLogFile(logPath)

	// Write 10 lines (2x maxEntries = triggers truncation)
	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, `{"timestamp":"2024-01-15T14:30:45Z","level":"info","message":"line`+string(rune('A'+i))+`"}`)
	}
	if err := os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Verify file has 10 lines before truncation
	data, _ := os.ReadFile(logPath)
	beforeLines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(beforeLines) != 10 {
		t.Fatalf("expected 10 lines before truncation, got %d", len(beforeLines))
	}

	// Simulate the truncation counter logic:
	// The Model should have a truncateCounter field that increments on each TickMsg.
	// When it hits 150, TruncateFile is called and counter resets.
	// We test this at the Model level by checking the field exists and the logic works.

	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.logViewer = lv

	// Verify truncateCounter starts at 0
	if m.truncateCounter != 0 {
		t.Fatalf("expected truncateCounter to start at 0, got %d", m.truncateCounter)
	}

	// Simulate 149 ticks — should NOT trigger truncation
	for i := 0; i < 149; i++ {
		updated, _ := m.Update(TickMsg{})
		m = updated.(Model)
	}

	if m.truncateCounter != 149 {
		t.Errorf("expected truncateCounter to be 149 after 149 ticks, got %d", m.truncateCounter)
	}

	// File should still have 10 lines (no truncation yet)
	data, _ = os.ReadFile(logPath)
	afterLines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(afterLines) != 10 {
		t.Errorf("expected 10 lines after 149 ticks (no truncation), got %d", len(afterLines))
	}

	// Tick 150 — should trigger truncation and reset counter
	updated, _ := m.Update(TickMsg{})
	m = updated.(Model)

	if m.truncateCounter != 0 {
		t.Errorf("expected truncateCounter to reset to 0 after 150th tick, got %d", m.truncateCounter)
	}

	// File should now have maxEntries (5) lines after truncation
	data, _ = os.ReadFile(logPath)
	afterTruncLines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(afterTruncLines) != maxEntries {
		t.Errorf("expected %d lines after truncation at tick 150, got %d", maxEntries, len(afterTruncLines))
	}
}
