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

// DreamConfigMsg is sent when dream monitor configuration has been fetched from the API.
type DreamConfigMsg struct {
	Config *DreamConfigInfo
	Error  error
}

// AutomationDataMsg is sent when automation entries and scheduled task entries are fetched.
type AutomationDataMsg struct {
	Automations    []types.BrainEntry
	ScheduledTasks []types.BrainEntry
	Error          error
}

// AutomationToggleMsg is sent when a selected automation row has been toggled.
type AutomationToggleMsg struct {
	RowID string
	Error error
}

// RunnersUpdatedMsg is sent when the runner list is refreshed (via SSE or polling).
type RunnersUpdatedMsg struct {
	Runners []types.RunnerInfo
}
