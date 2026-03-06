package tui

import (
	"sort"

	"github.com/huynle/brain-api/internal/types"
)

// TaskGroup represents a collapsible group of tasks organized by classification.
type TaskGroup struct {
	Name      string               // "Ready", "Waiting", "Active", "Blocked", "Draft", "Cancelled", "Completed", "Validated", "Superseded", "Archived"
	Tasks     []types.ResolvedTask // Tasks in this group
	Collapsed bool                 // Is the group collapsed?
	Count     int                  // Total tasks in group
}

// GroupTasks organizes tasks into groups by classification with optional visibility filtering.
// Returns groups in priority order: Ready, Waiting, Active, Blocked, Draft, Cancelled, Completed, Validated, Superseded, Archived.
// If visibleGroups is nil or empty, all groups are shown. If visibleGroups[groupName] == false, that group is excluded.
func GroupTasks(tasks []types.ResolvedTask, visibleGroups map[string]bool) []TaskGroup {
	if len(tasks) == 0 {
		return nil
	}

	// Build lookup map by classification
	groups := make(map[string][]types.ResolvedTask)

	for _, task := range tasks {
		classification := normalizeClassification(task.Classification, task.Status)
		groups[classification] = append(groups[classification], task)
	}

	// Sort tasks within each group by priority and status
	for _, taskList := range groups {
		sort.Slice(taskList, func(i, j int) bool {
			pi := priorityOrder[taskList[i].Priority]
			pj := priorityOrder[taskList[j].Priority]
			if pi != pj {
				return pi < pj
			}
			return statusOrder[taskList[i].Status] < statusOrder[taskList[j].Status]
		})
	}

	// Return in display order with visibility filtering
	result := []TaskGroup{}
	for _, groupName := range []string{"Ready", "Waiting", "Active", "Blocked", "Draft", "Cancelled", "Completed", "Validated", "Superseded", "Archived"} {
		taskList, ok := groups[groupName]
		if !ok || len(taskList) == 0 {
			continue // Skip groups with no tasks
		}

		// Check visibility: if visibleGroups is nil/empty, show all groups
		// If visibleGroups exists and group is explicitly false, skip it
		if len(visibleGroups) > 0 {
			if visible, hasKey := visibleGroups[groupName]; hasKey && !visible {
				continue // Skip invisible groups
			}
		}

		result = append(result, TaskGroup{
			Name:      groupName,
			Tasks:     taskList,
			Collapsed: false, // default expanded
			Count:     len(taskList),
		})
	}

	return result
}

// normalizeClassification maps API classification and status values to display groups.
func normalizeClassification(classification, status string) string {
	// First check classification (primary indicator)
	switch classification {
	case "ready":
		return "Ready"
	case "waiting":
		return "Waiting"
	case "blocked":
		return "Blocked"
	}

	// Fall back to status for additional classification
	switch status {
	case "in_progress", "active":
		return "Active"
	case "draft":
		return "Draft"
	case "cancelled":
		return "Cancelled"
	case "completed":
		return "Completed"
	case "validated":
		return "Validated"
	case "superseded":
		return "Superseded"
	case "archived":
		return "Archived"
	case "pending":
		return "Ready"
	case "waiting":
		return "Waiting"
	case "blocked":
		return "Blocked"
	default:
		// Default: unknown statuses go to Completed
		return "Completed"
	}
}

// FlattenGroupsToIDs returns a flat list of task IDs in visual order,
// respecting collapsed state (collapsed groups' tasks are excluded).
func FlattenGroupsToIDs(groups []TaskGroup, includeCollapsed bool) []string {
	var result []string
	for _, group := range groups {
		if group.Collapsed && !includeCollapsed {
			// Skip collapsed group tasks
			continue
		}
		for _, task := range group.Tasks {
			result = append(result, task.ID)
		}
	}
	return result
}
