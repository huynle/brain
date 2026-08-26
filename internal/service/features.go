package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
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

	// UnresolvedFeatureDeps lists depends_on_features entries that match no
	// known feature. Such a dep gates nothing — dropping it silently is how
	// a misspelled feature_depends_on became a no-op that also reported
	// nothing — so it is surfaced here instead.
	UnresolvedFeatureDeps []string `json:"unresolved_feature_deps"`
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

// computeTaskStats computes task statistics for a feature. Archived tasks
// are settled-and-shelved: they are excluded from Total and every bucket so
// they neither deflate progress ratios nor resurrect a finished feature.
func computeTaskStats(tasks []types.ResolvedTask) FeatureTaskStats {
	stats := FeatureTaskStats{}
	for _, task := range tasks {
		if task.Status == "archived" {
			continue
		}
		stats.Total++
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
//   - All archived -> archived
//   - All completed (ignoring archived) -> completed
//   - Any in_progress -> in_progress
//   - Any blocked (and no in_progress) -> blocked
//   - Otherwise -> pending
func ComputeFeatureStatus(tasks []types.ResolvedTask) string {
	if len(tasks) == 0 {
		return "pending"
	}

	stats := computeTaskStats(tasks)

	if stats.Total == 0 {
		// Tasks exist but all are archived: the feature is shelved, not
		// pending — "pending" would resurrect it as active work.
		return "archived"
	}
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

// collectFeatureDependencies collects all unique feature dependencies from tasks,
// in first-seen order. Order matters: these IDs flow into WaitingOnFeatures /
// BlockedByFeatures / UnresolvedFeatureDeps, which are serialized to clients —
// ranging over a map here made that output shuffle between identical requests.
func collectFeatureDependencies(tasks []types.ResolvedTask) []string {
	seen := make(map[string]bool)
	var result []string
	for _, task := range tasks {
		for _, dep := range task.FeatureDependsOn {
			if dep == "" || seen[dep] {
				continue
			}
			seen[dep] = true
			result = append(result, dep)
		}
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
			// Filled by ResolveFeatureDependencies, which is the only
			// layer that knows which deps name a real feature.
			UnresolvedFeatureDeps: []string{},
			InCycle:               false,
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

// splitFeatureDeps partitions a feature's declared dependencies into those that
// name a known feature and those that name nothing. Single source of truth for
// "what does this dep list actually resolve to" — the adjacency list and the
// classifier used to filter independently, so an unresolved dep could only ever
// be dropped, never reported.
func splitFeatureDeps(feature *ComputedFeature, maps *featureLookupMaps) (resolved, unresolved []string) {
	for _, depID := range feature.DependsOnFeatures {
		if _, ok := maps.byID[depID]; ok {
			resolved = append(resolved, depID)
		} else {
			unresolved = append(unresolved, depID)
		}
	}
	return resolved, unresolved
}

// buildFeatureAdjacencyList builds adjacency list from features.
func buildFeatureAdjacencyList(features []*ComputedFeature, maps *featureLookupMaps) map[string][]string {
	adj := make(map[string][]string, len(features))
	for _, feature := range features {
		resolvedDeps, _ := splitFeatureDeps(feature, maps)
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

	// Feature already settled (completed or archived) - no classification needed
	if feature.Status == "completed" || feature.Status == "archived" {
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
		resolvedDeps, unresolvedDeps := splitFeatureDeps(feature, maps)

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
		if unresolvedDeps != nil {
			f.UnresolvedFeatureDeps = unresolvedDeps
		} else {
			f.UnresolvedFeatureDeps = []string{}
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
// Excludes completed and archived features. Sorted by priority.
func GetReadyFeatures(features []*ComputedFeature) []*ComputedFeature {
	var ready []*ComputedFeature
	for _, f := range features {
		if f.Classification == "ready" && f.Status != "completed" && f.Status != "archived" {
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

// AssignFeatureToRunner manually assigns a project-scoped feature to a runner.
func (s *TaskServiceImpl) AssignFeatureToRunner(ctx context.Context, projectID, featureID string, req types.FeatureAssignmentRequest) (*types.FeatureAssignmentResponse, error) {
	runnerID := strings.TrimSpace(req.RunnerID)
	if runnerID == "" {
		return nil, fmt.Errorf("runner_id is required")
	}

	runner, err := s.storage.GetRunner(ctx, runnerID)
	if err != nil {
		return nil, fmt.Errorf("get runner: %w", err)
	}
	if runner == nil {
		return nil, api.ErrNotFound
	}
	if !req.Force && computeRunnerStatus(runner.LastHeartbeat) != types.RunnerStatusOnline {
		return nil, api.ErrConflict
	}

	assigned, existing, err := s.storage.AssignFeatureIfEmpty(ctx, projectID, featureID, runnerID, "manual", "active")
	if err != nil {
		return nil, fmt.Errorf("assign feature: %w", err)
	}
	if assigned {
		existing, err = s.storage.GetFeatureAssignment(ctx, projectID, featureID)
		if err != nil {
			return nil, fmt.Errorf("get feature assignment: %w", err)
		}
	}
	if existing == nil {
		return nil, api.ErrNotFound
	}
	if existing.RunnerID == runnerID {
		return featureAssignmentRowToResponse(existing, ""), nil
	}
	if strings.TrimSpace(req.Intent) != "reassign" {
		return nil, api.ErrConflict
	}

	previousRunner := existing.RunnerID
	reassigned, err := s.storage.ForceAssignFeature(ctx, projectID, featureID, runnerID, "manual", "active")
	if err != nil {
		return nil, fmt.Errorf("force assign feature: %w", err)
	}
	return featureAssignmentRowToResponse(reassigned, previousRunner), nil
}

// ClearFeatureAssignment manually clears a project-scoped feature assignment.
func (s *TaskServiceImpl) ClearFeatureAssignment(ctx context.Context, projectID, featureID string, req types.ClearFeatureAssignmentRequest) (*types.FeatureAssignmentResponse, error) {
	if strings.TrimSpace(req.Intent) != "clear" {
		return nil, api.ErrConflict
	}
	existing, err := s.storage.GetFeatureAssignment(ctx, projectID, featureID)
	if err != nil {
		return nil, fmt.Errorf("get feature assignment: %w", err)
	}
	if existing == nil {
		return nil, api.ErrNotFound
	}

	cleared, err := s.storage.ClearFeatureAssignment(ctx, projectID, featureID)
	if err != nil {
		return nil, fmt.Errorf("clear feature assignment: %w", err)
	}
	if !cleared {
		return nil, api.ErrNotFound
	}

	return &types.FeatureAssignmentResponse{
		ProjectID:      projectID,
		FeatureID:      featureID,
		PreviousRunner: existing.RunnerID,
		Source:         existing.Source,
		Status:         "cleared",
		AssignedAt:     time.UnixMilli(existing.AssignedAt).UTC().Format(time.RFC3339),
		UpdatedAt:      time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func featureAssignmentRowToResponse(assignment *storage.FeatureAssignmentRow, previousRunner string) *types.FeatureAssignmentResponse {
	return &types.FeatureAssignmentResponse{
		ProjectID:      assignment.ProjectID,
		FeatureID:      assignment.FeatureID,
		RunnerID:       assignment.RunnerID,
		PreviousRunner: previousRunner,
		Source:         assignment.Source,
		Status:         assignment.Status,
		AssignedAt:     time.UnixMilli(assignment.AssignedAt).UTC().Format(time.RFC3339),
		UpdatedAt:      time.UnixMilli(assignment.UpdatedAt).UTC().Format(time.RFC3339),
	}
}

// =============================================================================
// Dependent closure (manual "run feature + dependents")
// =============================================================================

// maxCascadeClosure bounds how many features one manual request may enrol.
//
// A chain force-dispatches past a project pause that was already on, so an
// unbounded closure on a foundational feature could run most of a project from
// one click. Truncation is reported rather than silent — see
// DependentClosure.Truncated.
const maxCascadeClosure = 32

// DependentClosure is the set of features a manual "run + dependents" request
// covers, derived fresh from the current graph.
type DependentClosure struct {
	// Members are the transitive dependents of the root, in breadth-first
	// order, so a caller dispatching in order never asks a feature to run
	// before something it depends on inside the closure.
	Members []string
	// Skipped names features that were reachable but deliberately not
	// enrolled, mapped to why. A member that can never run is worth saying
	// out loud: a chain that silently stalls is worse than one that refuses.
	Skipped map[string]string
	// External lists features OUTSIDE the closure that a member still waits
	// on. These are why an otherwise-correct chain stalls — the member's gate
	// cannot open until work nobody queued has run.
	External []string
	// Truncated is set when the closure hit maxCascadeClosure.
	Truncated bool
}

// TransitiveDependents walks the REVERSE dependency edges from root: every
// feature that declares root in feature_depends_on, then everything that
// depends on those, and so on.
//
// The root itself is never a member — it is dispatched directly by the caller.
//
// Features already settled (completed/archived) are skipped: re-enrolling them
// would keep a chain alive forever on work that is finished. Cycle members are
// skipped because applyFeatureGating blocks their tasks unconditionally, so
// enrolling one guarantees a permanent stall.
func TransitiveDependents(features []*ComputedFeature, root string) DependentClosure {
	out := DependentClosure{Skipped: map[string]string{}}
	if root == "" || len(features) == 0 {
		return out
	}

	byID := make(map[string]*ComputedFeature, len(features))
	// dependents[X] = features that declare X in their feature_depends_on.
	dependents := make(map[string][]string, len(features))
	for _, f := range features {
		byID[f.ID] = f
		for _, dep := range f.DependsOnFeatures {
			dependents[dep] = append(dependents[dep], f.ID)
		}
	}

	inClosure := map[string]bool{root: true}
	seen := map[string]bool{root: true}
	queue := []string{root}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		next := append([]string(nil), dependents[cur]...)
		sort.Strings(next) // deterministic order for equal-depth siblings

		for _, id := range next {
			if seen[id] {
				continue
			}
			seen[id] = true

			f, ok := byID[id]
			if !ok {
				continue
			}
			switch {
			case f.InCycle:
				out.Skipped[id] = "in_cycle"
				continue
			case f.Status == "completed" || f.Status == "archived":
				out.Skipped[id] = "already_settled"
				continue
			}
			if len(out.Members) >= maxCascadeClosure {
				out.Truncated = true
				continue
			}
			out.Members = append(out.Members, id)
			inClosure[id] = true
			queue = append(queue, id)
		}
	}

	// Second pass: a member may also wait on something nobody enrolled. That
	// is the difference between "queued and waiting its turn" and "queued and
	// never going to run", and only the graph can tell them apart.
	extSeen := map[string]bool{}
	for _, id := range out.Members {
		f := byID[id]
		if f == nil {
			continue
		}
		for _, dep := range append(append([]string(nil), f.WaitingOnFeatures...), f.BlockedByFeatures...) {
			if inClosure[dep] || extSeen[dep] {
				continue
			}
			// A settled dependency is not an obstacle.
			if d, ok := byID[dep]; ok && (d.Status == "completed" || d.Status == "archived") {
				continue
			}
			extSeen[dep] = true
			out.External = append(out.External, dep)
		}
	}
	sort.Strings(out.External)

	return out
}
