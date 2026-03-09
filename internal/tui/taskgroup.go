package tui

import (
	"sort"

	"github.com/huynle/brain-api/internal/types"
)

// TaskGroup represents a collapsible group of tasks organized by classification.
type TaskGroup struct {
	Name      string               // "Draft", "Pending", "Active", "In Progress", "Blocked", "Cancelled", "Completed", "Validated", "Superseded", "Archived"
	Tasks     []types.ResolvedTask // Tasks in this group
	Collapsed bool                 // Is the group collapsed?
	Count     int                  // Total tasks in group
}

// GroupTasks organizes tasks into groups by classification with optional visibility filtering.
// Returns groups in priority order: Draft, Pending, Active, In Progress, Blocked, Cancelled, Completed, Validated, Superseded, Archived.
// If visibleGroups is nil or empty, all groups are shown. If visibleGroups[groupName] == false, that group is excluded.
func GroupTasks(tasks []types.ResolvedTask, visibleGroups map[string]bool) []TaskGroup {
	if len(tasks) == 0 {
		return nil
	}

	// Build lookup map by classification
	groups := make(map[string][]types.ResolvedTask)

	for _, task := range tasks {
		classification := normalizeClassification(task.Classification, task.Status, task.FeatureID)
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
	for _, groupName := range []string{"Draft", "Pending", "Active", "In Progress", "Blocked", "Cancelled", "Completed", "Validated", "Superseded", "Archived"} {
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
// Groups are based on status values, matching the TypeScript implementation.
func normalizeClassification(classification, status, featureID string) string {
	// Check status first (primary indicator for grouping)
	switch status {
	case "draft":
		debugLog("normalizeClassification: status=draft -> Draft group")
		return "Draft"
	case "pending":
		debugLog("normalizeClassification: status=pending -> Pending group")
		return "Pending"
	case "active":
		debugLog("normalizeClassification: status=active -> Active group")
		return "Active"
	case "in_progress":
		debugLog("normalizeClassification: status=in_progress -> In Progress group")
		return "In Progress"
	case "blocked":
		debugLog("normalizeClassification: status=blocked -> Blocked group")
		return "Blocked"
	case "cancelled":
		debugLog("normalizeClassification: status=cancelled -> Cancelled group")
		return "Cancelled"
	case "completed":
		debugLog("normalizeClassification: status=completed -> Completed group")
		return "Completed"
	case "validated":
		debugLog("normalizeClassification: status=validated -> Validated group")
		return "Validated"
	case "superseded":
		debugLog("normalizeClassification: status=superseded -> Superseded group")
		return "Superseded"
	case "archived":
		debugLog("normalizeClassification: status=archived -> Archived group")
		return "Archived"
	}

	// Fall back to classification if status doesn't match (for backward compatibility)
	// Classification "ready" maps to "Active", "waiting" maps to "Pending"
	switch classification {
	case "ready":
		debugLog("normalizeClassification: classification=ready -> Active group (fallback)")
		return "Active"
	case "waiting":
		debugLog("normalizeClassification: classification=waiting -> Pending group (fallback)")
		return "Pending"
	case "blocked":
		debugLog("normalizeClassification: classification=blocked -> Blocked group (fallback)")
		return "Blocked"
	case "not_pending":
		debugLog("normalizeClassification: classification=not_pending -> Completed group (fallback)")
		return "Completed"
	}

	// Default: unknown statuses/classifications go to Completed
	debugLog("normalizeClassification: unknown status=%s, classification=%s -> Completed (default)", status, classification)
	return "Completed"
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
