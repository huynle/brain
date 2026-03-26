package tui

import (
	"fmt"
	"os"

	"github.com/huynle/brain-api/internal/types"
)

// debugLog writes a debug message to stderr if DEBUG_TUI_GROUPING env var is set.
// This helps diagnose why groups might not appear in the TUI.
func debugLog(format string, args ...interface{}) {
	if os.Getenv("DEBUG_TUI_GROUPING") != "" {
		fmt.Fprintf(os.Stderr, "[TUI_GROUPING] "+format+"\n", args...)
	}
}

// StatusGroup represents a collapsible group of tasks organized by status (classification),
// with nested feature grouping within each status.
type StatusGroup struct {
	Name      string         // "Draft", "Pending", "Active", "In Progress", "Blocked", "Inactive", "Ungrouped"
	Features  []FeatureGroup // Feature groups within this status
	Ungrouped *FeatureGroup  // Tasks without feature_id (nil if none)
	Collapsed bool           // Is the status group collapsed?
	Count     int            // Total tasks in this status (across all features + ungrouped)
}

// GroupTasksByStatusAndFeature groups tasks first by status (classification), then by feature_id within each status.
// Returns status groups in fixed display order: Draft, Pending, Active, In Progress, Blocked, Cancelled, Completed, Validated, Superseded, Archived.
// Within each status, features are sorted by priority (high > medium > low), then alphabetically by ID.
// Tasks without feature_id go into the Ungrouped group within their status.
// If visibleGroups is nil or empty, all groups are shown. If visibleGroups[groupName] == false, that group is excluded.
func GroupTasksByStatusAndFeature(tasks []types.ResolvedTask, visibleGroups map[string]bool) []StatusGroup {
	debugLog("GroupTasksByStatusAndFeature called: %d tasks, %d visible groups configured", len(tasks), len(visibleGroups))
	if len(tasks) == 0 {
		debugLog("No tasks to group")
		return nil
	}

	// Step 1: Group tasks by normalized status (classification)
	statusMap := make(map[string][]types.ResolvedTask)
	for _, task := range tasks {
		status := normalizeClassification(task.Classification, task.Status, task.FeatureID)
		statusMap[status] = append(statusMap[status], task)
		debugLog("Task %s: classification=%s status=%s feature_id=%s -> group=%s",
			task.ID, task.Classification, task.Status, task.FeatureID, status)
	}

	// Step 2: For each status group, create nested feature groups
	var result []StatusGroup
	statusOrder := []string{"Ungrouped", "Draft", "Ready", "Pending", "Waiting", "Active", "In Progress", "Blocked", "Inactive"}

	for _, statusName := range statusOrder {
		statusTasks, ok := statusMap[statusName]
		if !ok || len(statusTasks) == 0 {
			debugLog("Group %s: skipped (no tasks)", statusName)
			continue
		}

		// Check visibility: if visibleGroups is nil/empty, show all groups
		// If visibleGroups exists and group is explicitly false, skip it
		visible := true
		if len(visibleGroups) > 0 {
			if vis, hasKey := visibleGroups[statusName]; hasKey {
				visible = vis
			}
		}

		if !visible {
			debugLog("Group %s: skipped (visibility=false, %d tasks hidden)", statusName, len(statusTasks))
			continue
		}

		// Group tasks by feature within this status
		featureResult := GroupTasksByFeature(statusTasks)

		debugLog("Group %s: creating group (visibility=true, %d tasks, %d features)",
			statusName, len(statusTasks), len(featureResult.Features))

		result = append(result, StatusGroup{
			Name:      statusName,
			Features:  featureResult.Features,
			Ungrouped: featureResult.Ungrouped,
			Collapsed: false,
			Count:     len(statusTasks),
		})
	}

	debugLog("Final result: %d groups created", len(result))
	return result
}
