package tui

import (
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Custom tea.Msg Types
// =============================================================================

// TasksUpdatedMsg is sent when the task list is refreshed (via SSE or polling).
type TasksUpdatedMsg struct {
	Tasks     []types.ResolvedTask
	ProjectID string
	Stats     *types.TaskStats
}

// SSEConnectedMsg is sent when the SSE connection is established.
type SSEConnectedMsg struct {
	ProjectID string
}

// SSEDisconnectedMsg is sent when the SSE connection is lost.
type SSEDisconnectedMsg struct {
	ProjectID string
}

// SSEErrorMsg is sent when an SSE error occurs.
type SSEErrorMsg struct {
	Err       error
	ProjectID string
}

// reconnectProjectMsg is sent internally to trigger SSE reconnection for a specific project.
type reconnectProjectMsg struct {
	ProjectID string
}

// TickMsg is sent on periodic timer ticks (for animations, status refresh).
type TickMsg struct{}

// ProcessStartedMsg is sent when a runner spawns a new child process.
// The TUI uses the PID to track resource metrics via MetricsCollector.
type ProcessStartedMsg struct {
	PID    int
	TaskID string
}

// ProcessStoppedMsg is sent when a runner child process exits.
// The TUI unregisters the PID from MetricsCollector.
type ProcessStoppedMsg struct {
	PID    int
	TaskID string
}

// LogEntryMsg is sent when a log entry should be added to the log viewer.
// This bridges runner events and TUI actions to the log panel.
type LogEntryMsg struct {
	Entry LogEntry
}

// SessionDiscoveredMsg is sent when the runner discovers a session ID
// for a running task. The TUI stores it in-memory on the task for "o"/"O" access.
type SessionDiscoveredMsg struct {
	TaskPath  string
	SessionID string
}

// DreamContentMsg is sent when dream content has been fetched from the API.
type DreamContentMsg struct {
	Content string
	Error   error
}

// RunnerListMsg is sent when the runner list has been fetched from the API.
type RunnerListMsg struct {
	Runners []types.RunnerInfo
	Err     error
}

// RunnersUpdatedMsg is sent when the SSE stream pushes an updated runner list.
type RunnersUpdatedMsg struct {
	Runners []types.RunnerInfo
}

// RunnerLogMsg is sent when a runner_log SSE event is received from a remote runner.
// This enables monitor-only mode to display logs from runners executing elsewhere.
type RunnerLogMsg struct {
	ProjectID string
	TaskID    string
	RunnerID  string
	Lines     []types.LogLine
}
