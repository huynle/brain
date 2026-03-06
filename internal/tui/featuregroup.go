package tui

import (
	"sort"

	"github.com/huynle/brain-api/internal/types"
)

// FeatureGroup represents tasks grouped by feature_id.
type FeatureGroup struct {
	ID        string               // feature_id value
	Name      string               // Display name (same as ID for now)
	Tasks     []types.ResolvedTask // Tasks in this feature
	Collapsed bool                 // Is the feature collapsed?
	Stats     FeatureStats         // Aggregated stats for the feature
	Priority  string               // Feature priority (from FeaturePriority field)
}

// FeatureStats holds statistics for a feature.
type FeatureStats struct {
	Total     int // Total tasks in feature
	Completed int // Completed tasks
	Active    int // In-progress tasks
	Ready     int // Ready tasks
	Waiting   int // Waiting tasks
	Blocked   int // Blocked tasks
}

// FeatureGroupResult holds the result of grouping tasks by feature.
type FeatureGroupResult struct {
	Features  []FeatureGroup // Features with tasks
	Ungrouped *FeatureGroup  // Tasks without feature_id (nil if none)
}

// GroupTasksByFeature groups tasks by their feature_id field.
// Tasks without a feature_id go into the Ungrouped group.
// Returns features sorted by priority, then alphabetically.
func GroupTasksByFeature(tasks []types.ResolvedTask) FeatureGroupResult {
	if len(tasks) == 0 {
		return FeatureGroupResult{}
	}

	// Build lookup map by feature_id
	featureMap := make(map[string][]types.ResolvedTask)
	var ungroupedTasks []types.ResolvedTask

	for _, task := range tasks {
		if task.FeatureID == "" {
			ungroupedTasks = append(ungroupedTasks, task)
		} else {
			featureMap[task.FeatureID] = append(featureMap[task.FeatureID], task)
		}
	}

	// Build feature groups
	var features []FeatureGroup
	for featureID, featureTasks := range featureMap {
		// Get feature priority from first task (all tasks in a feature share priority)
		featurePriority := "medium" // default
		if len(featureTasks) > 0 && featureTasks[0].FeaturePriority != "" {
			featurePriority = featureTasks[0].FeaturePriority
		}

		features = append(features, FeatureGroup{
			ID:       featureID,
			Name:     featureID,
			Tasks:    featureTasks,
			Stats:    computeFeatureStats(featureTasks),
			Priority: featurePriority,
		})
	}

	// Sort features by priority (high > medium > low), then alphabetically by ID
	sort.Slice(features, func(i, j int) bool {
		pi := priorityOrder[features[i].Priority]
		pj := priorityOrder[features[j].Priority]
		if pi != pj {
			return pi < pj
		}
		return features[i].ID < features[j].ID
	})

	// Build ungrouped group if there are ungrouped tasks
	var ungrouped *FeatureGroup
	if len(ungroupedTasks) > 0 {
		ungrouped = &FeatureGroup{
			ID:    "",
			Name:  "[Ungrouped]",
			Tasks: ungroupedTasks,
			Stats: computeFeatureStats(ungroupedTasks),
		}
	}

	return FeatureGroupResult{
		Features:  features,
		Ungrouped: ungrouped,
	}
}

// computeFeatureStats calculates aggregate statistics for a list of tasks.
func computeFeatureStats(tasks []types.ResolvedTask) FeatureStats {
	stats := FeatureStats{Total: len(tasks)}

	for _, task := range tasks {
		switch task.Status {
		case "completed", "validated":
			stats.Completed++
		case "in_progress", "active":
			stats.Active++
		}

		switch task.Classification {
		case "ready":
			stats.Ready++
		case "waiting":
			stats.Waiting++
		case "blocked":
			stats.Blocked++
		}
	}

	return stats
}
