// Package tui implements the interactive terminal dashboard for the Brain task runner.
//
// It uses the Bubble Tea framework (Elm architecture) with lipgloss for styling.
// The TUI displays task status, logs, and details in a multi-panel layout.
package tui

import (
	"github.com/huynle/brain-api/internal/types"
)

// Panel identifies which panel currently has focus.
type Panel int

const (
	PanelTasks Panel = iota
	PanelDetails
	PanelLogs
)

// String returns the display name for a panel.
func (p Panel) String() string {
	switch p {
	case PanelTasks:
		return "tasks"
	case PanelDetails:
		return "details"
	case PanelLogs:
		return "logs"
	default:
		return "unknown"
	}
}

// NextPanel cycles to the next visible panel.
func NextPanel(current Panel, detailVisible, logsVisible bool) Panel {
	panels := []Panel{PanelTasks}
	if detailVisible {
		panels = append(panels, PanelDetails)
	}
	if logsVisible {
		panels = append(panels, PanelLogs)
	}

	for i, p := range panels {
		if p == current {
			return panels[(i+1)%len(panels)]
		}
	}
	return PanelTasks
}

// Config holds the configuration passed to the TUI from the runner.
type Config struct {
	APIURL   string
	Project  string
	RunnerID string
	BrainDir string
	// Projects lists all projects in multi-project mode.
	Projects []string
}

// IsMultiProject returns true if monitoring multiple projects.
func (c Config) IsMultiProject() bool {
	return len(c.Projects) > 1
}

// TaskStats mirrors types.TaskStats with an additional InProgress field
// for display purposes (active tasks currently being executed).
type TaskStats struct {
	Ready      int
	Waiting    int
	Blocked    int
	InProgress int
	Completed  int
}

// TaskStatsFromAPI converts an API TaskStats to the TUI TaskStats.
func TaskStatsFromAPI(s *types.TaskStats) TaskStats {
	if s == nil {
		return TaskStats{}
	}
	return TaskStats{
		Ready:   s.Ready,
		Waiting: s.Waiting,
		Blocked: s.Blocked,
		// NotPending includes in_progress + completed + validated + etc.
		Completed: s.NotPending,
	}
}
