package tui

import (
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// FilterTasks returns tasks matching the given filter query.
// Matches against task title, ID, and tags (case-insensitive).
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
func matchesQuery(task types.ResolvedTask, query string) bool {
	// Match against title
	if strings.Contains(strings.ToLower(task.Title), query) {
		return true
	}

	// Match against ID
	if strings.Contains(strings.ToLower(task.ID), query) {
		return true
	}

	return false
}
