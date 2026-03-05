package service

import (
	"sort"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// FeatureTaskStats holds per-feature task statistics.
type FeatureTaskStats struct {
	Total      int `json:"total"`
	Pending    int `json:"pending"`
	InProgress int `json:"in_progress"`
	Completed  int `json:"completed"`
	Blocked    int `json:"blocked"`
}

// ComputedFeature represents a computed feature grouping of tasks.
type ComputedFeature struct {
	ID                string               `json:"id"`
	Project           string               `json:"project"`
	Priority          string               `json:"priority"`
	DependsOnFeatures []string             `json:"depends_on_features"`
	Tasks             []types.ResolvedTask `json:"tasks"`
	Status            string               `json:"status"`
	Classification    string               `json:"classification"`
	TaskStats         FeatureTaskStats     `json:"task_stats"`
	BlockedByFeatures []string             `json:"blocked_by_features"`
	WaitingOnFeatures []string             `json:"waiting_on_features"`
	InCycle           bool                 `json:"in_cycle"`
}

// FeatureDependencyResult holds the result of feature dependency resolution.
type FeatureDependencyResult struct {
	Features []*ComputedFeature `json:"features"`
	Cycles   [][]string         `json:"cycles"`
	Stats    struct {
		Total   int `json:"total"`
		Ready   int `json:"ready"`
		Waiting int `json:"waiting"`
		Blocked int `json:"blocked"`
	} `json:"stats"`
}

// computeTaskStats computes task statistics for a feature.
func computeTaskStats(tasks []types.ResolvedTask) FeatureTaskStats {
	stats := FeatureTaskStats{Total: len(tasks)}
	for _, task := range tasks {
		switch task.Status {
		case "pending":
			stats.Pending++
		case "in_progress":
			stats.InProgress++
		case "completed", "validated":
			stats.Completed++
		case "blocked", "cancelled":
			stats.Blocked++
		}
	}
	return stats
}

// ComputeFeatureStatus computes feature status from constituent task statuses.
//
// Rules:
//   - All completed -> completed
//   - Any in_progress -> in_progress
//   - Any blocked (and no in_progress) -> blocked
//   - Otherwise -> pending
func ComputeFeatureStatus(tasks []types.ResolvedTask) string {
	if len(tasks) == 0 {
		return "pending"
	}

	stats := computeTaskStats(tasks)

	if stats.Completed == stats.Total {
		return "completed"
	}
	if stats.InProgress > 0 {
		return "in_progress"
	}
	if stats.Blocked > 0 {
		return "blocked"
	}
	return "pending"
}

// computeHighestPriority returns the highest priority from any task in the feature.
// Uses feature_priority if set, otherwise falls back to task priority.
func computeHighestPriority(tasks []types.ResolvedTask) string {
	highest := "low"
	for _, task := range tasks {
		p := task.FeaturePriority
		if p == "" {
			p = task.Priority
		}
		if getPriorityOrder(p) < getPriorityOrder(highest) {
			highest = p
		}
	}
	return highest
}

// collectFeatureDependencies collects all unique feature dependencies from tasks.
func collectFeatureDependencies(tasks []types.ResolvedTask) []string {
	deps := make(map[string]bool)
	for _, task := range tasks {
		for _, dep := range task.FeatureDependsOn {
			deps[dep] = true
		}
	}
	result := make([]string, 0, len(deps))
	for dep := range deps {
		result = append(result, dep)
	}
	return result
}

// ComputeFeatures groups tasks by feature_id and computes initial feature metadata.
// Tasks without feature_id are skipped.
func ComputeFeatures(tasks []types.ResolvedTask) []*ComputedFeature {
	// Group tasks by feature_id
	featureMap := make(map[string][]types.ResolvedTask)
	// Preserve insertion order
	var featureOrder []string

	for _, task := range tasks {
		if task.FeatureID == "" {
			continue
		}
		if _, exists := featureMap[task.FeatureID]; !exists {
			featureOrder = append(featureOrder, task.FeatureID)
		}
		featureMap[task.FeatureID] = append(featureMap[task.FeatureID], task)
	}

	features := make([]*ComputedFeature, 0, len(featureMap))
	for _, featureID := range featureOrder {
		featureTasks := featureMap[featureID]

		taskStats := computeTaskStats(featureTasks)
		status := ComputeFeatureStatus(featureTasks)
		priority := computeHighestPriority(featureTasks)
		dependsOnFeatures := collectFeatureDependencies(featureTasks)

		// Get project from first task path
		project := "default"
		if len(featureTasks) > 0 {
			parts := strings.Split(featureTasks[0].Path, "/")
			if len(parts) > 1 {
				project = parts[1]
			}
		}

		features = append(features, &ComputedFeature{
			ID:                featureID,
			Project:           project,
			Priority:          priority,
			DependsOnFeatures: dependsOnFeatures,
			Tasks:             featureTasks,
			Status:            status,
			Classification:    "waiting", // Will be resolved in ResolveFeatureDependencies
			TaskStats:         taskStats,
			BlockedByFeatures: []string{},
			WaitingOnFeatures: []string{},
			InCycle:           false,
		})
	}

	return features
}

// featureLookupMaps provides O(1) lookups for feature dependency resolution.
type featureLookupMaps struct {
	byID map[string]*ComputedFeature
}

// buildFeatureLookupMaps builds lookup maps for fast feature resolution.
func buildFeatureLookupMaps(features []*ComputedFeature) *featureLookupMaps {
	m := &featureLookupMaps{
		byID: make(map[string]*ComputedFeature, len(features)),
	}
	for _, f := range features {
		m.byID[f.ID] = f
	}
	return m
}

// buildFeatureAdjacencyList builds adjacency list from features.
func buildFeatureAdjacencyList(features []*ComputedFeature, maps *featureLookupMaps) map[string][]string {
	adj := make(map[string][]string, len(features))
	for _, feature := range features {
		var resolvedDeps []string
		for _, depID := range feature.DependsOnFeatures {
			if _, ok := maps.byID[depID]; ok {
				resolvedDeps = append(resolvedDeps, depID)
			}
		}
		adj[feature.ID] = resolvedDeps
	}
	return adj
}

// classifyFeature classifies a feature based on its dependencies.
func classifyFeature(
	feature *ComputedFeature,
	resolvedDeps []string,
	effectiveStatus map[string]string,
	inCycle map[string]bool,
) (classification string, blockedBy []string, waitingOn []string) {
	// Check if feature is in a cycle
	if inCycle[feature.ID] {
		return "blocked", nil, nil
	}

	// Feature already completed - no classification needed
	if feature.Status == "completed" {
		return "ready", nil, nil
	}

	// Check for blocked dependencies
	for _, depID := range resolvedDeps {
		status := effectiveStatus[depID]
		if status == "" {
			status = "pending"
		}
		if status == "blocked" || inCycle[depID] {
			blockedBy = append(blockedBy, depID)
		}
	}
	if len(blockedBy) > 0 {
		return "blocked", blockedBy, nil
	}

	// Check for waiting dependencies (pending or in_progress)
	for _, depID := range resolvedDeps {
		status := effectiveStatus[depID]
		if status == "" {
			status = "pending"
		}
		if status == "pending" || status == "in_progress" {
			waitingOn = append(waitingOn, depID)
		}
	}
	if len(waitingOn) > 0 {
		return "waiting", nil, waitingOn
	}

	// All dependencies satisfied
	return "ready", nil, nil
}

// ResolveFeatureDependencies resolves all feature dependencies and classifies features.
func ResolveFeatureDependencies(features []*ComputedFeature) []*ComputedFeature {
	if len(features) == 0 {
		return []*ComputedFeature{}
	}

	// Step 1: Build lookup maps
	maps := buildFeatureLookupMaps(features)

	// Step 2: Build adjacency list and detect cycles
	adjacency := buildFeatureAdjacencyList(features, maps)
	inCycle := FindCycles(adjacency)

	// Step 3: Build effective status map (with cycle override)
	effectiveStatus := make(map[string]string, len(features))
	for _, feature := range features {
		if inCycle[feature.ID] {
			effectiveStatus[feature.ID] = "blocked"
		} else {
			effectiveStatus[feature.ID] = feature.Status
		}
	}

	// Step 4: Classify each feature
	result := make([]*ComputedFeature, len(features))
	for i, feature := range features {
		// Get resolved dependencies (only those that exist)
		var resolvedDeps []string
		for _, depID := range feature.DependsOnFeatures {
			if _, ok := maps.byID[depID]; ok {
				resolvedDeps = append(resolvedDeps, depID)
			}
		}

		classification, blockedBy, waitingOn := classifyFeature(feature, resolvedDeps, effectiveStatus, inCycle)

		// Copy feature and update classification fields
		f := *feature
		f.Classification = classification
		if blockedBy != nil {
			f.BlockedByFeatures = blockedBy
		} else {
			f.BlockedByFeatures = []string{}
		}
		if waitingOn != nil {
			f.WaitingOnFeatures = waitingOn
		} else {
			f.WaitingOnFeatures = []string{}
		}
		f.InCycle = inCycle[feature.ID]

		result[i] = &f
	}

	return result
}

// SortFeaturesByPriority sorts features by priority (high first), then by completion ratio descending.
// Returns a new slice; does not mutate the original.
func SortFeaturesByPriority(features []*ComputedFeature) []*ComputedFeature {
	sorted := make([]*ComputedFeature, len(features))
	copy(sorted, features)
	sort.SliceStable(sorted, func(i, j int) bool {
		aOrder := getPriorityOrder(sorted[i].Priority)
		bOrder := getPriorityOrder(sorted[j].Priority)
		if aOrder != bOrder {
			return aOrder < bOrder
		}
		// Secondary sort: completion ratio descending
		var aRatio, bRatio float64
		if sorted[i].TaskStats.Total > 0 {
			aRatio = float64(sorted[i].TaskStats.Completed) / float64(sorted[i].TaskStats.Total)
		}
		if sorted[j].TaskStats.Total > 0 {
			bRatio = float64(sorted[j].TaskStats.Completed) / float64(sorted[j].TaskStats.Total)
		}
		return aRatio > bRatio // Descending (higher completion first)
	})
	return sorted
}

// GetReadyFeatures returns features that are ready to execute (all dependencies satisfied).
// Excludes completed features. Sorted by priority.
func GetReadyFeatures(features []*ComputedFeature) []*ComputedFeature {
	var ready []*ComputedFeature
	for _, f := range features {
		if f.Classification == "ready" && f.Status != "completed" {
			ready = append(ready, f)
		}
	}
	return SortFeaturesByPriority(ready)
}

// ComputeAndResolveFeatures is the main entry point: compute features from tasks,
// resolve dependencies, and return stats.
func ComputeAndResolveFeatures(tasks []types.ResolvedTask) *FeatureDependencyResult {
	// Compute initial features
	features := ComputeFeatures(tasks)

	// Resolve dependencies
	resolved := ResolveFeatureDependencies(features)

	// Detect cycles for reporting
	maps := buildFeatureLookupMaps(resolved)
	adjacency := buildFeatureAdjacencyList(resolved, maps)
	inCycle := FindCycles(adjacency)
	var cycles [][]string
	if len(inCycle) > 0 {
		group := make([]string, 0, len(inCycle))
		for id := range inCycle {
			group = append(group, id)
		}
		cycles = [][]string{group}
	}

	// Compute stats
	result := &FeatureDependencyResult{
		Features: resolved,
		Cycles:   cycles,
	}
	result.Stats.Total = len(resolved)
	for _, f := range resolved {
		switch f.Classification {
		case "ready":
			result.Stats.Ready++
		case "waiting":
			result.Stats.Waiting++
		case "blocked":
			result.Stats.Blocked++
		}
	}

	return result
}
