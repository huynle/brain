package tui

import (
	"fmt"
	"os"
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
	if os.Getenv("DEBUG_TUI_GROUPING") != "" {
		fmt.Fprintf(os.Stderr, "[GroupTasks] Called with %d tasks, %d visible groups\n", len(tasks), len(visibleGroups))
	}

	if len(tasks) == 0 {
		if os.Getenv("DEBUG_TUI_GROUPING") != "" {
			fmt.Fprintf(os.Stderr, "[GroupTasks] No tasks, returning nil\n")
		}
		return nil
	}

	// Build lookup map by classification
	groups := make(map[string][]types.ResolvedTask)

	for _, task := range tasks {
		classification := normalizeClassification(task.Classification, task.Status, task.FeatureID)
		groups[classification] = append(groups[classification], task)
		if os.Getenv("DEBUG_TUI_GROUPING") != "" {
			fmt.Fprintf(os.Stderr, "[GroupTasks] Task %s -> group %s (classification=%s, status=%s, feature=%s)\n",
				task.ID, classification, task.Classification, task.Status, task.FeatureID)
		}
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
	// Order: classification-based groups first (Ready, Waiting, Blocked, Ungrouped), then status-based groups
	for _, groupName := range []string{"Ready", "Waiting", "Blocked", "Ungrouped", "Draft", "Pending", "Active", "In Progress", "Cancelled", "Completed", "Validated", "Superseded", "Archived"} {
		taskList, ok := groups[groupName]
		if !ok || len(taskList) == 0 {
			continue // Skip groups with no tasks
		}

		// Check visibility: if visibleGroups is nil/empty, show all groups
		// If visibleGroups exists and group is explicitly false, skip it
		if len(visibleGroups) > 0 {
			if visible, hasKey := visibleGroups[groupName]; hasKey && !visible {
				if os.Getenv("DEBUG_TUI_GROUPING") != "" {
					fmt.Fprintf(os.Stderr, "[GroupTasks] Skipping group %s (visibility=false, %d tasks hidden)\n", groupName, len(taskList))
				}
				continue // Skip invisible groups
			}
		}

		if os.Getenv("DEBUG_TUI_GROUPING") != "" {
			fmt.Fprintf(os.Stderr, "[GroupTasks] Adding group %s with %d tasks\n", groupName, len(taskList))
		}

		result = append(result, TaskGroup{
			Name:      groupName,
			Tasks:     taskList,
			Collapsed: false, // default expanded
			Count:     len(taskList),
		})
	}

	if os.Getenv("DEBUG_TUI_GROUPING") != "" {
		fmt.Fprintf(os.Stderr, "[GroupTasks] Returning %d groups\n", len(result))
	}

	return result
}

// normalizeClassification maps API classification and status values to display groups.
// For pending/draft/active/in_progress statuses, classification takes precedence to show readiness.
// Tasks without featureID go to "Ungrouped" regardless of status/classification.
// Terminal statuses (completed, validated, etc.) always use status groups.
func normalizeClassification(classification, status, featureID string) string {
	// Priority 1: Tasks without feature_id go to Ungrouped (except terminal states)
	if featureID == "" {
		switch status {
		case "completed", "validated", "cancelled", "superseded", "archived", "draft":
			// Terminal states and draft stay in their own groups
		default:
			// All other tasks without feature_id go to Ungrouped
			debugLog("normalizeClassification: no feature_id, status=%s -> Ungrouped", status)
			return "Ungrouped"
		}
	}

	// Priority 2: For pending/draft/active/in_progress statuses, check classification first
	if status == "pending" || status == "draft" || status == "active" || status == "in_progress" {
		switch classification {
		case "ready":
			debugLog("normalizeClassification: status=%s, classification=ready -> Ready group", status)
			return "Ready"
		case "waiting":
			debugLog("normalizeClassification: status=%s, classification=waiting -> Waiting group", status)
			return "Waiting"
		case "blocked":
			debugLog("normalizeClassification: status=%s, classification=blocked -> Blocked group", status)
			return "Blocked"
		}

		// For pending with no classification but WITH feature_id, default to Ready
		if status == "pending" && featureID != "" {
			debugLog("normalizeClassification: status=pending, no classification, has feature_id -> Ready group (default)")
			return "Ready"
		}

		// For in_progress with no classification, default to Completed
		if status == "in_progress" {
			debugLog("normalizeClassification: status=in_progress, no classification -> Completed group (default)")
			return "Completed"
		}
	}

	// Priority 3: Check status for execution states
	switch status {
	case "draft":
		debugLog("normalizeClassification: status=draft (no classification) -> Draft group")
		return "Draft"
	case "pending":
		// If we get here with pending, fallback to Waiting
		debugLog("normalizeClassification: status=pending (fallthrough) -> Waiting group")
		return "Waiting"
	case "waiting":
		debugLog("normalizeClassification: status=waiting -> Waiting group")
		return "Waiting"
	case "active":
		debugLog("normalizeClassification: status=active (no classification) -> Active group")
		return "Active"
	case "in_progress":
		// Should have been handled above, but fallback to In Progress
		debugLog("normalizeClassification: status=in_progress (fallthrough) -> In Progress group")
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

	// Priority 4: Fall back to classification if status is empty or unknown
	switch classification {
	case "ready":
		debugLog("normalizeClassification: classification=ready (no status) -> Ready group")
		return "Ready"
	case "waiting":
		debugLog("normalizeClassification: classification=waiting (no status) -> Waiting group")
		return "Waiting"
	case "blocked":
		debugLog("normalizeClassification: classification=blocked (no status) -> Blocked group")
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
