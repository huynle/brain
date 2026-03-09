package tui

import "github.com/huynle/brain-api/internal/types"

// StatusGroup represents a collapsible group of tasks organized by status (classification),
// with nested feature grouping within each status.
type StatusGroup struct {
	Name      string         // "Ready", "Waiting", "Blocked", "Draft", "Cancelled", "Completed", "Validated", "Superseded", "Archived"
	Features  []FeatureGroup // Feature groups within this status
	Ungrouped *FeatureGroup  // Tasks without feature_id (nil if none)
	Collapsed bool           // Is the status group collapsed?
	Count     int            // Total tasks in this status (across all features + ungrouped)
}

// GroupTasksByStatusAndFeature groups tasks first by status (classification), then by feature_id within each status.
// Returns status groups in fixed display order: Ready, Waiting, Blocked, Draft, Cancelled, Completed, Validated, Superseded, Archived.
// Within each status, features are sorted by priority (high > medium > low), then alphabetically by ID.
// Tasks without feature_id go into the Ungrouped group within their status.
// Note: in_progress tasks stay in their classification groups and are indicated by the blue arrow, not a separate "Active" group.
func GroupTasksByStatusAndFeature(tasks []types.ResolvedTask) []StatusGroup {
	if len(tasks) == 0 {
		return nil
	}

	// Step 1: Group tasks by normalized status (classification)
	statusMap := make(map[string][]types.ResolvedTask)
	for _, task := range tasks {
		status := normalizeClassification(task.Classification, task.Status)
		statusMap[status] = append(statusMap[status], task)
	}

	// Step 2: For each status group, create nested feature groups
	var result []StatusGroup
	statusOrder := []string{"Ready", "Waiting", "Blocked", "Draft", "Cancelled", "Completed", "Validated", "Superseded", "Archived"}

	for _, statusName := range statusOrder {
		statusTasks, ok := statusMap[statusName]
		if !ok || len(statusTasks) == 0 {
			continue
		}

		// Group tasks by feature within this status
		featureResult := GroupTasksByFeature(statusTasks)

		result = append(result, StatusGroup{
			Name:      statusName,
			Features:  featureResult.Features,
			Ungrouped: featureResult.Ungrouped,
			Collapsed: false,
			Count:     len(statusTasks),
		})
	}

	return result
}
