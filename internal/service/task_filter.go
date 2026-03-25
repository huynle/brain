package service

import "github.com/huynle/brain-api/internal/types"

// filterByFeatureIDs filters tasks to only include those with matching feature IDs.
// Returns nil if tasks is nil. Returns nil if featureIDs is nil or empty.
func filterByFeatureIDs(tasks []types.ResolvedTask, featureIDs []string) []types.ResolvedTask {
	if tasks == nil {
		return nil
	}
	if len(featureIDs) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(featureIDs))
	for _, fid := range featureIDs {
		allowed[fid] = true
	}
	var filtered []types.ResolvedTask
	for _, t := range tasks {
		if allowed[t.FeatureID] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// pickHighestPriority returns the highest-priority task from a non-empty slice.
// Priority order: high > medium > low > empty.
func pickHighestPriority(tasks []types.ResolvedTask) *types.ResolvedTask {
	if len(tasks) == 0 {
		return nil
	}
	best := &tasks[0]
	for i := 1; i < len(tasks); i++ {
		if priorityRank(tasks[i].Priority) > priorityRank(best.Priority) {
			best = &tasks[i]
		}
	}
	return best
}

// priorityRank returns a numeric rank for priority comparison (higher = more important).
func priorityRank(p string) int {
	switch p {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}
