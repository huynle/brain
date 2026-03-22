// Package tui implements the interactive terminal dashboard for the Brain task runner.
//
// It uses the Bubble Tea framework (Elm architecture) with lipgloss for styling.
// The TUI displays task status, logs, and details in a multi-panel layout.
package tui

import (
	"context"

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

// RunnerController is an interface for controlling the embedded task runner.
// If nil, the TUI falls back to HTTP API calls for pause/resume.
type RunnerController interface {
	PauseProject(projectID string)
	ResumeProject(projectID string)
	PauseAll()
	ResumeAll()
	IsPaused(projectID string) bool
	IsAllPaused() bool
	// ExecuteTask manually executes a task (TUI "x" key).
	// Performs the full claim → status update → workdir resolve → spawn pipeline.
	ExecuteTask(ctx context.Context, task *types.ResolvedTask, projectID string) error
}

// Config holds the configuration passed to the TUI from the runner.
type Config struct {
	APIURL     string
	APIToken   string
	APITimeout int // milliseconds; 0 defaults to 15000 (15s)
	Project    string
	RunnerID   string
	BrainDir   string
	LogDir     string
	// Projects lists all projects in multi-project mode.
	Projects []string
	// Runner is the embedded task runner controller (optional).
	// If set, pause/resume calls go directly to the runner instead of via HTTP.
	Runner RunnerController
}

// DefaultAPITimeout is the default HTTP client timeout for TUI API calls (15 seconds).
// This is higher than the runner default (5s) because the TUI may connect to
// remote APIs (e.g., brain.huynle.com) where network latency is higher.
const DefaultAPITimeout = 15000

// IsMultiProject returns true if monitoring multiple projects.
func (c Config) IsMultiProject() bool {
	return len(c.Projects) > 1
}

// ViewMode identifies which view mode the TUI is in.
type ViewMode int

const (
	ViewModeTasks     ViewMode = iota // Default: show task tree
	ViewModeSchedules                 // Show scheduled tasks list
)

// String returns the display name for a view mode.
func (v ViewMode) String() string {
	switch v {
	case ViewModeTasks:
		return "tasks"
	case ViewModeSchedules:
		return "schedules"
	default:
		return "unknown"
	}
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
