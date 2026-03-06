package tui

import (
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Custom tea.Msg Types
// =============================================================================

// TasksUpdatedMsg is sent when the task list is refreshed (via SSE or polling).
type TasksUpdatedMsg struct {
	Tasks []types.ResolvedTask
	ProjectID string
	Stats *types.TaskStats
}

// SSEConnectedMsg is sent when the SSE connection is established.
type SSEConnectedMsg struct{}

// SSEDisconnectedMsg is sent when the SSE connection is lost.
type SSEDisconnectedMsg struct{}

// SSEErrorMsg is sent when an SSE error occurs.
type SSEErrorMsg struct {
	Err error
}

// TickMsg is sent on periodic timer ticks (for animations, status refresh).
type TickMsg struct{}
