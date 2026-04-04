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
	DependsOn []string             // Feature IDs this feature depends on
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

		// Get feature dependencies from first task (all tasks in a feature share this value)
		var featureDeps []string
		if len(featureTasks) > 0 && len(featureTasks[0].FeatureDependsOn) > 0 {
			featureDeps = featureTasks[0].FeatureDependsOn
		}

		features = append(features, FeatureGroup{
			ID:        featureID,
			Name:      featureID,
			Tasks:     featureTasks,
			Stats:     computeFeatureStats(featureTasks),
			Priority:  featurePriority,
			DependsOn: featureDeps,
		})
	}

	// Build dependency map for topological depth computation
	depsMap := make(map[string][]string)
	for _, f := range features {
		depsMap[f.ID] = f.DependsOn
	}

	// Compute topological depth for each feature
	depths := make(map[string]int)
	for _, f := range features {
		visited := make(map[string]bool)
		d := computeTopologicalDepth(f.ID, depsMap, visited)
		if d < 0 {
			d = 0 // cycle members get depth 0
		}
		depths[f.ID] = d
	}

	// Sort features by priority (high > medium > low), then topological depth, then alphabetically by ID
	sort.Slice(features, func(i, j int) bool {
		pi := priorityOrder[features[i].Priority]
		pj := priorityOrder[features[j].Priority]
		if pi != pj {
			return pi < pj
		}
		di := depths[features[i].ID]
		dj := depths[features[j].ID]
		if di != dj {
			return di < dj
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

// aggregateFeatureStatusIcon returns an icon representing the aggregated status
// of all tasks in a feature. Priority order: in_progress > blocked > ready > waiting > completed.
// Returns (statusIcon, hasActiveExecution).
func aggregateFeatureStatusIcon(tasks []types.ResolvedTask) (string, bool) {
	if len(tasks) == 0 {
		return IndicatorReady, false
	}

	hasInProgress := false
	hasBlocked := false
	hasReady := false
	hasWaiting := false
	allCompleted := true

	for _, task := range tasks {
		switch task.Status {
		case "in_progress", "active":
			hasInProgress = true
			allCompleted = false
		case "completed", "validated":
			// completed, don't change allCompleted
		case "cancelled", "superseded", "archived":
			// terminal states, treated like completed for aggregation
		default:
			allCompleted = false
		}

		switch task.Classification {
		case "blocked":
			hasBlocked = true
		case "ready":
			hasReady = true
		case "waiting":
			hasWaiting = true
		}
	}

	// Priority: in_progress > blocked > ready > waiting > completed
	switch {
	case hasInProgress:
		return IndicatorActive, true // ▶ with ⚡
	case hasBlocked:
		return IndicatorBlocked, false // ✗
	case hasReady:
		return IndicatorReady, false // ●
	case hasWaiting:
		return IndicatorWaiting, false // ○
	case allCompleted:
		return IndicatorCompleted, false // ✓
	default:
		return IndicatorReady, false // ● default
	}
}

// computeTopologicalDepth returns the depth of a feature in the dependency graph.
// Root features (no deps) = depth 0. Others = max(depth of deps) + 1.
// Cycle members get depth 0 to avoid infinite recursion.
// visited tracks the current recursion stack to detect cycles.
func computeTopologicalDepth(featureID string, depsMap map[string][]string, visited map[string]bool) int {
	if visited[featureID] {
		return -1 // cycle detected — sentinel value
	}
	visited[featureID] = true
	defer func() { visited[featureID] = false }() // backtrack for other paths

	deps := depsMap[featureID]
	if len(deps) == 0 {
		return 0
	}
	maxDepth := 0
	cycleDetected := false
	for _, dep := range deps {
		d := computeTopologicalDepth(dep, depsMap, visited)
		if d < 0 {
			cycleDetected = true
			continue // skip cyclic deps
		}
		if d+1 > maxDepth {
			maxDepth = d + 1
		}
	}
	// If ALL deps are cyclic, propagate cycle sentinel
	if cycleDetected && maxDepth == 0 {
		return -1
	}
	return maxDepth
}

// featureDepStatusIcon returns the status icon for a dependency feature
// by examining its tasks' completion state.
// Returns: ✓ (all completed), ▶ (has in_progress/active), ✗ (has blocked),
// ○ (pending/waiting), ? (feature not found).
func featureDepStatusIcon(depFeatureID string, allFeatures []FeatureGroup) string {
	// Find the dependency feature
	var depFeature *FeatureGroup
	for i := range allFeatures {
		if allFeatures[i].ID == depFeatureID {
			depFeature = &allFeatures[i]
			break
		}
	}

	if depFeature == nil {
		return "?" // dep feature not found
	}

	tasks := depFeature.Tasks
	if len(tasks) == 0 {
		return IndicatorWaiting // ○ — no tasks yet, treat as not started
	}

	hasInProgress := false
	hasBlocked := false
	allCompleted := true

	for _, task := range tasks {
		switch task.Status {
		case "in_progress", "active":
			hasInProgress = true
			allCompleted = false
		case "completed", "validated":
			// terminal success — don't clear allCompleted
		case "cancelled", "superseded", "archived":
			// terminal states — treated like completed for aggregation
		default:
			allCompleted = false
		}

		if task.Classification == "blocked" {
			hasBlocked = true
		}
	}

	switch {
	case allCompleted:
		return IndicatorCompleted // ✓
	case hasInProgress:
		return IndicatorActive // ▶
	case hasBlocked:
		return IndicatorBlocked // ✗
	default:
		return IndicatorWaiting // ○
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
