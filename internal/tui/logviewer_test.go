package tui

import (
	"strings"
	"testing"
	"time"
)

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
