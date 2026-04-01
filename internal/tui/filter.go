package tui

import (
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// FilterMode represents the 3-mode filter state machine.
type FilterMode int

const (
	// FilterOff means no filter is active and no input is being captured.
	FilterOff FilterMode = iota
	// FilterTyping means the user is actively typing a filter query.
	FilterTyping
	// FilterLocked means a filter is applied and the user can navigate normally.
	FilterLocked
)

// String returns the display name for a FilterMode.
func (m FilterMode) String() string {
	switch m {
	case FilterOff:
		return "off"
	case FilterTyping:
		return "typing"
	case FilterLocked:
		return "locked"
	default:
		return "unknown"
	}
}

// FilterTasks returns tasks matching the given filter query.
// Matches against task title, ID, feature_id, and tags (case-insensitive).
func FilterTasks(tasks []types.ResolvedTask, query string) []types.ResolvedTask {
	if query == "" {
		return tasks
	}

	query = strings.ToLower(query)
	filtered := []types.ResolvedTask{}

	for _, task := range tasks {
		if matchesQuery(task, query) {
			filtered = append(filtered, task)
		}
	}

	return filtered
}

// matchesQuery returns true if the task matches the query string.
// The query should be lowercase for best performance (FilterTasks pre-lowers it),
// but this function handles uppercase queries gracefully.
func matchesQuery(task types.ResolvedTask, query string) bool {
	q := strings.ToLower(query)

	// Match against title
	if strings.Contains(strings.ToLower(task.Title), q) {
		return true
	}

	// Match against ID
	if strings.Contains(strings.ToLower(task.ID), q) {
		return true
	}

	// Match against feature_id
	if task.FeatureID != "" && strings.Contains(strings.ToLower(task.FeatureID), q) {
		return true
	}

	// Match against schedule (typing "cron" or "schedule" finds all scheduled tasks,
	// or match partial cron expressions like "*/15")
	if task.Schedule != "" {
		if strings.Contains("cron", q) || strings.Contains("scheduled", q) ||
			strings.Contains(strings.ToLower(task.Schedule), q) {
			return true
		}
	}

	return false
}
