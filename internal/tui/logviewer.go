package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	ProjectID string
	RunnerID  string
	Context   map[string]interface{}
}

// LogViewer displays streaming log entries with color-coded levels.
type LogViewer struct {
	entries    []LogEntry
	maxEntries int
	autoFollow bool
	width      int
	height     int
	logFile    string

	// Filtering: when IsFiltering is true, only show entries matching FilterTaskID
	IsFiltering  bool
	FilterTaskID string

	// Multi-project mode: when true, prefix log entries with [projectName]
	isMultiProject bool
}

// NewLogViewer creates a new LogViewer with the given max entries.
func NewLogViewer(maxEntries int) LogViewer {
	return LogViewer{
		maxEntries: maxEntries,
		autoFollow: true,
	}
}

// AddEntry adds a log entry to the viewer, evicting old entries if at capacity.
// If a log file is configured, the entry is also persisted to disk.
func (lv *LogViewer) AddEntry(entry LogEntry) {
	lv.entries = append(lv.entries, entry)
	// Circular buffer: evict oldest entries when over capacity
	if len(lv.entries) > lv.maxEntries {
		lv.entries = lv.entries[len(lv.entries)-lv.maxEntries:]
	}
	// Persist to disk (ignore errors — don't fail the TUI if disk write fails)
	_ = lv.appendToFile(entry)
}

// SetSize updates the component dimensions.
func (lv *LogViewer) SetSize(width, height int) {
	lv.width = width
	lv.height = height
}

// SetMultiProject enables multi-project mode, which adds [projectName] prefix to log entries.
func (lv *LogViewer) SetMultiProject(enabled bool) {
	lv.isMultiProject = enabled
}

// EntryCount returns the number of log entries.
func (lv *LogViewer) EntryCount() int {
	return len(lv.entries)
}

// View renders the log viewer.
func (lv *LogViewer) View() string {
	// Determine which entries to show (filtered or all)
	entries := lv.visibleEntries()

	// Header changes based on filter state
	headerText := "Logs"
	if lv.IsFiltering && lv.FilterTaskID != "" {
		headerText = "Task Logs"
	}
	header := TitleStyle.Render(headerText)

	if len(entries) == 0 {
		emptyMsg := "No logs"
		if lv.IsFiltering && lv.FilterTaskID != "" {
			emptyMsg = "No logs for selected task"
		}
		return header + "\n" + DimStyle.Render(emptyMsg)
	}

	var lines []string
	lines = append(lines, header)

	for _, entry := range entries {
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

// visibleEntries returns the entries to display, filtered by task if filtering is active.
func (lv *LogViewer) visibleEntries() []LogEntry {
	if !lv.IsFiltering || lv.FilterTaskID == "" {
		return lv.entries
	}

	var filtered []LogEntry
	for _, entry := range lv.entries {
		if entry.TaskID == lv.FilterTaskID {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

// renderEntry renders a single log entry line.
func (lv *LogViewer) renderEntry(entry LogEntry) string {
	// Project prefix in multi-project mode
	var projectPrefix string
	if lv.isMultiProject && entry.ProjectID != "" {
		projectPrefix = DimStyle.Render(fmt.Sprintf("[%s] ", entry.ProjectID))
	}

	// Runner ID prefix (always shown when present to distinguish local vs remote logs)
	var runnerPrefix string
	if entry.RunnerID != "" {
		runnerPrefix = DimStyle.Render(fmt.Sprintf("[%s] ", entry.RunnerID))
	}

	// Timestamp: HH:MM:SS
	ts := formatTimestamp(entry.Timestamp)
	tsStyled := DimStyle.Render(ts)

	// Level label with color
	levelLabel := levelToLabel(entry.Level)
	levelStyled := levelStyle(entry.Level).Render(levelLabel)

	// Message with truncation
	msg := truncateMsg(entry.Message, maxMessageLength)

	// Context fields formatted as key="value" pairs
	contextStr := formatContext(entry.Context)
	if contextStr != "" {
		msg += " " + DimStyle.Render(contextStr)
	}

	return fmt.Sprintf("%s%s%s %s %s", projectPrefix, runnerPrefix, tsStyled, levelStyled, msg)
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

// formatContext formats context fields as space-separated key="value" pairs.
func formatContext(ctx map[string]interface{}) string {
	if len(ctx) == 0 {
		return ""
	}
	var parts []string
	for k, v := range ctx {
		parts = append(parts, fmt.Sprintf(`%s="%v"`, k, v))
	}
	return strings.Join(parts, " ")
}

// =============================================================================
// Persistence - JSONL File Operations
// =============================================================================

// logEntryJSON is the JSON representation of a LogEntry for JSONL serialization.
type logEntryJSON struct {
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	TaskID    string                 `json:"taskId,omitempty"`
	ProjectID string                 `json:"projectId,omitempty"`
	RunnerID  string                 `json:"runnerId,omitempty"`
	Context   map[string]interface{} `json:"context,omitempty"`
}

// SetLogFile sets the path for log file persistence.
func (lv *LogViewer) SetLogFile(path string) {
	lv.logFile = path
}

// serializeEntry converts a LogEntry to a JSON string (JSONL format).
func (lv *LogViewer) serializeEntry(entry LogEntry) string {
	j := logEntryJSON{
		Timestamp: entry.Timestamp.Format(time.RFC3339),
		Level:     entry.Level,
		Message:   entry.Message,
		TaskID:    entry.TaskID,
		ProjectID: entry.ProjectID,
		RunnerID:  entry.RunnerID,
		Context:   entry.Context,
	}
	data, err := json.Marshal(j)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// deserializeEntry parses a JSONL line back into a LogEntry.
// Returns error for invalid JSON or missing required fields (timestamp, level, message).
func (lv *LogViewer) deserializeEntry(line string) (LogEntry, error) {
	var j logEntryJSON
	if err := json.Unmarshal([]byte(line), &j); err != nil {
		return LogEntry{}, fmt.Errorf("invalid JSON: %w", err)
	}

	if j.Timestamp == "" {
		return LogEntry{}, fmt.Errorf("missing required field: timestamp")
	}
	if j.Level == "" {
		return LogEntry{}, fmt.Errorf("missing required field: level")
	}
	if j.Message == "" {
		return LogEntry{}, fmt.Errorf("missing required field: message")
	}

	ts, err := time.Parse(time.RFC3339, j.Timestamp)
	if err != nil {
		return LogEntry{}, fmt.Errorf("invalid timestamp: %w", err)
	}

	return LogEntry{
		Timestamp: ts,
		Level:     j.Level,
		Message:   j.Message,
		TaskID:    j.TaskID,
		ProjectID: j.ProjectID,
		RunnerID:  j.RunnerID,
		Context:   j.Context,
	}, nil
}

// LoadFromFile reads the logFile path, parses each line with deserializeEntry,
// and populates entries (up to maxEntries, keeping the latest).
// If the file doesn't exist, returns nil (not an error).
func (lv *LogViewer) LoadFromFile() error {
	if lv.logFile == "" {
		return nil
	}

	f, err := os.Open(lv.logFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open log file: %w", err)
	}
	defer f.Close()

	var entries []LogEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		entry, err := lv.deserializeEntry(line)
		if err != nil {
			// Skip invalid lines
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read log file: %w", err)
	}

	// Keep only the last maxEntries
	if len(entries) > lv.maxEntries {
		entries = entries[len(entries)-lv.maxEntries:]
	}

	lv.entries = entries
	return nil
}

// appendToFile appends a serialized log entry to the log file.
// Creates the directory structure if needed. If logFile is empty, returns nil (no-op).
func (lv *LogViewer) appendToFile(entry LogEntry) error {
	if lv.logFile == "" {
		return nil
	}

	dir := filepath.Dir(lv.logFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}

	f, err := os.OpenFile(lv.logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log file for append: %w", err)
	}
	defer f.Close()

	line := lv.serializeEntry(entry) + "\n"
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("write log entry: %w", err)
	}

	return nil
}

// TruncateFile truncates the log file when it has >= 2*maxEntries lines.
// Keeps the last maxEntries lines. If the file doesn't exist or logFile is empty, returns nil.
func (lv *LogViewer) TruncateFile() error {
	if lv.logFile == "" {
		return nil
	}

	f, err := os.Open(lv.logFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open log file for truncation: %w", err)
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read log file for truncation: %w", err)
	}

	// Only truncate when file has >= 2*maxEntries lines
	if len(lines) < 2*lv.maxEntries {
		return nil
	}

	// Keep the last maxEntries lines
	kept := lines[len(lines)-lv.maxEntries:]

	// Rewrite the file
	content := strings.Join(kept, "\n") + "\n"
	if err := os.WriteFile(lv.logFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("rewrite truncated log file: %w", err)
	}

	return nil
}
