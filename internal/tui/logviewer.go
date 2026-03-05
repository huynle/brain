package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Maximum message length before truncation.
const maxMessageLength = 80

// LogEntry represents a single log entry.
type LogEntry struct {
	Timestamp time.Time
	Level     string
	Message   string
	TaskID    string
}

// LogViewer displays streaming log entries with color-coded levels.
type LogViewer struct {
	entries    []LogEntry
	maxEntries int
	autoFollow bool
	width      int
	height     int
}

// NewLogViewer creates a new LogViewer with the given max entries.
func NewLogViewer(maxEntries int) LogViewer {
	return LogViewer{
		maxEntries: maxEntries,
		autoFollow: true,
	}
}

// AddEntry adds a log entry to the viewer, evicting old entries if at capacity.
func (lv *LogViewer) AddEntry(entry LogEntry) {
	lv.entries = append(lv.entries, entry)
	// Circular buffer: evict oldest entries when over capacity
	if len(lv.entries) > lv.maxEntries {
		lv.entries = lv.entries[len(lv.entries)-lv.maxEntries:]
	}
}

// SetSize updates the component dimensions.
func (lv *LogViewer) SetSize(width, height int) {
	lv.width = width
	lv.height = height
}

// EntryCount returns the number of log entries.
func (lv *LogViewer) EntryCount() int {
	return len(lv.entries)
}

// View renders the log viewer.
func (lv *LogViewer) View() string {
	header := TitleStyle.Render("Logs")

	if len(lv.entries) == 0 {
		return header + "\n" + DimStyle.Render("No logs")
	}

	var lines []string
	lines = append(lines, header)

	for _, entry := range lv.entries {
		line := lv.renderEntry(entry)
		lines = append(lines, line)
	}

	// Truncate to height if needed
	if lv.height > 0 && len(lines) > lv.height {
		// Show most recent entries (auto-follow behavior)
		lines = lines[len(lines)-lv.height:]
	}

	return strings.Join(lines, "\n")
}

// renderEntry renders a single log entry line.
func (lv *LogViewer) renderEntry(entry LogEntry) string {
	// Timestamp: HH:MM:SS
	ts := formatTimestamp(entry.Timestamp)
	tsStyled := DimStyle.Render(ts)

	// Level label with color
	levelLabel := levelToLabel(entry.Level)
	levelStyled := levelStyle(entry.Level).Render(levelLabel)

	// Message with truncation
	msg := truncateMsg(entry.Message, maxMessageLength)

	return fmt.Sprintf("%s %s %s", tsStyled, levelStyled, msg)
}

// formatTimestamp formats a time as HH:MM:SS.
func formatTimestamp(t time.Time) string {
	return fmt.Sprintf("%02d:%02d:%02d", t.Hour(), t.Minute(), t.Second())
}

// levelToLabel converts a log level to its display label.
func levelToLabel(level string) string {
	switch level {
	case "debug":
		return "DEBUG"
	case "info":
		return "INFO "
	case "warn":
		return "WARN "
	case "error":
		return "ERROR"
	default:
		return strings.ToUpper(level)
	}
}

// levelStyle returns the lipgloss style for a log level.
func levelStyle(level string) lipgloss.Style {
	switch level {
	case "debug":
		return lipgloss.NewStyle().Foreground(ColorDim)
	case "info":
		return lipgloss.NewStyle().Foreground(ColorActive)
	case "warn":
		return lipgloss.NewStyle().Foreground(ColorWaiting)
	case "error":
		return lipgloss.NewStyle().Foreground(ColorBlocked).Bold(true)
	default:
		return lipgloss.NewStyle()
	}
}

// truncateMsg truncates a message with ellipsis if too long.
func truncateMsg(msg string, maxLen int) string {
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen-3] + "..."
}
