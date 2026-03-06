// Package service implements core business logic for the Brain API.
//
// This file contains task dependency resolution algorithms:
// lookup maps, dependency resolution, cycle detection, task classification,
// priority sorting, and downstream task discovery.
package service

import (
	"regexp"
	"sort"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// pathPattern matches "projects/<project>/task/<id>.md" or "projects/<project>/task/<id>"
var pathPattern = regexp.MustCompile(`^projects/[^/]+/task/(.+?)(?:\.md)?$`)

// TaskLookupMaps provides O(1) lookups for dependency resolution.
type TaskLookupMaps struct {
	ByID      map[string]*types.BrainEntry
	TitleToID map[string]string
}

// BuildLookupMaps builds lookup maps for fast dependency resolution.
func BuildLookupMaps(tasks []types.BrainEntry) *TaskLookupMaps {
	m := &TaskLookupMaps{
		ByID:      make(map[string]*types.BrainEntry, len(tasks)),
		TitleToID: make(map[string]string, len(tasks)),
	}
	for i := range tasks {
		m.ByID[tasks[i].ID] = &tasks[i]
		m.TitleToID[tasks[i].Title] = tasks[i].ID
	}
	return m
}

// ResolveDep resolves a dependency reference to a task ID.
// Supports: bare ID ("abc12def"), project-prefixed ("project:abc12def"),
// full path ("projects/project/task/abc12def.md"), and title references.
// Returns "" if not resolved.
func ResolveDep(ref string, maps *TaskLookupMaps) string {
	// Direct ID match
	if _, ok := maps.ByID[ref]; ok {
		return ref
	}

	// Handle "project:taskId" format
	if idx := strings.Index(ref, ":"); idx != -1 {
		bareID := ref[idx+1:]
		if _, ok := maps.ByID[bareID]; ok {
			return bareID
		}
	}

	// Handle full path format
	if matches := pathPattern.FindStringSubmatch(ref); matches != nil {
		bareID := matches[1]
		if _, ok := maps.ByID[bareID]; ok {
			return bareID
		}
	}

	// Title match
	if id, ok := maps.TitleToID[ref]; ok {
		return id
	}

	return ""
}

// BuildAdjacencyList builds a map of task ID -> resolved dependency IDs.
func BuildAdjacencyList(tasks []types.BrainEntry, maps *TaskLookupMaps) map[string][]string {
	adj := make(map[string][]string, len(tasks))
	for _, task := range tasks {
		var resolved []string
		for _, ref := range task.DependsOn {
			if id := ResolveDep(ref, maps); id != "" {
				resolved = append(resolved, id)
			}
		}
		adj[task.ID] = resolved
	}
	return adj
}

// FindCycles finds all task IDs that participate in dependency cycles.
// Uses iterative BFS per node to detect if a node can reach itself.
// Safety limit: 100 iterations per node.
func FindCycles(adjacency map[string][]string) map[string]bool {
	inCycle := make(map[string]bool)

	for start, initialDeps := range adjacency {
		if len(initialDeps) == 0 {
			continue
		}

		// BFS from start's dependencies to see if we can reach start
		frontier := make([]string, len(initialDeps))
		copy(frontier, initialDeps)
		seen := make(map[string]bool)
		iterations := 0
		const maxIterations = 100

		for len(frontier) > 0 && iterations < maxIterations {
			iterations++
			current := frontier[0]
			frontier = frontier[1:]

			if current == start {
				inCycle[start] = true
				break
			}

			if seen[current] {
				continue
			}
			seen[current] = true

			if deps, ok := adjacency[current]; ok {
				frontier = append(frontier, deps...)
			}
		}
	}

	return inCycle
}

// ClassifyTask classifies a single task based on its dependencies.
// Returns classification, blockedBy IDs, and waitingOn IDs.
func ClassifyTask(
	task *types.BrainEntry,
	resolvedDeps []string,
	effectiveStatuses map[string]string,
	inCycle map[string]bool,
) (classification string, blockedBy []string, waitingOn []string) {
	// Check if task is in a cycle
	if inCycle[task.ID] {
		return "blocked", nil, nil
	}

	// Task not pending - skip classification
	if task.Status != "pending" {
		return "not_pending", nil, nil
	}

	// Check for blocked dependencies
	for _, depID := range resolvedDeps {
		status := effectiveStatuses[depID]
		if status == "" {
			status = "unknown"
		}
		if status == "blocked" || status == "cancelled" || inCycle[depID] {
			blockedBy = append(blockedBy, depID)
		}
	}
	if len(blockedBy) > 0 {
		return "blocked", blockedBy, nil
	}

	// Check for waiting dependencies (pending or in_progress)
	for _, depID := range resolvedDeps {
		status := effectiveStatuses[depID]
		if status == "" {
			status = "unknown"
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

// brainEntryToResolvedTask converts a BrainEntry to a ResolvedTask,
// copying all relevant fields.
func brainEntryToResolvedTask(task *types.BrainEntry) types.ResolvedTask {
	return types.ResolvedTask{
		ID:                 task.ID,
		Path:               task.Path,
		Title:              task.Title,
		Priority:           task.Priority,
		Status:             task.Status,
		ParentID:           task.ParentID,
		DependsOn:          task.DependsOn,
		Created:            task.Created,
		Workdir:            task.Workdir,
		GitRemote:          task.GitRemote,
		GitBranch:          task.GitBranch,
		MergeTargetBranch:  task.MergeTargetBranch,
		MergePolicy:        task.MergePolicy,
		MergeStrategy:      task.MergeStrategy,
		RemoteBranchPolicy: task.RemoteBranchPolicy,
		OpenPRBeforeMerge:  task.OpenPRBeforeMerge,
		ExecutionMode:      task.ExecutionMode,
		FeatureID:          task.FeatureID,
		FeaturePriority:    task.FeaturePriority,
		FeatureDependsOn:   task.FeatureDependsOn,
		DirectPrompt:       task.DirectPrompt,
		Agent:              task.Agent,
		Model:              task.Model,
		Generated:          task.Generated,
		GeneratedKind:      task.GeneratedKind,
		GeneratedKey:       task.GeneratedKey,
		GeneratedBy:        task.GeneratedBy,
	}
}

// ResolveDependencies resolves all dependencies and classifies all tasks.
// This is the main entry point for task dependency resolution.
func ResolveDependencies(tasks []types.BrainEntry) *types.TaskListResponse {
	if len(tasks) == 0 {
		return &types.TaskListResponse{
			Tasks:  []types.ResolvedTask{},
			Count:  0,
			Stats:  &types.TaskStats{},
			Cycles: [][]string{},
		}
	}

	// Step 1: Build lookup maps
	maps := BuildLookupMaps(tasks)

	// Step 2: Build adjacency list and detect cycles
	adjacency := BuildAdjacencyList(tasks, maps)
	inCycle := FindCycles(adjacency)

	// Step 3: Build effective status map (with cycle override)
	effectiveStatus := make(map[string]string, len(tasks))
	for _, task := range tasks {
		if inCycle[task.ID] {
			effectiveStatus[task.ID] = "circular"
		} else {
			effectiveStatus[task.ID] = task.Status
		}
	}

	// Step 4: Resolve and classify each task
	resolvedTasks := make([]types.ResolvedTask, 0, len(tasks))
	for i := range tasks {
		task := &tasks[i]

		// Resolve dependencies
		var resolvedDeps []string
		var unresolvedDeps []string
		for _, ref := range task.DependsOn {
			if id := ResolveDep(ref, maps); id != "" {
				resolvedDeps = append(resolvedDeps, id)
			} else {
				unresolvedDeps = append(unresolvedDeps, ref)
			}
		}

		// Classify
		classification, blockedBy, waitingOn := ClassifyTask(task, resolvedDeps, effectiveStatus, inCycle)

		// Determine blocked_by_reason
		var blockedByReason string
		if inCycle[task.ID] {
			blockedByReason = "circular_dependency"
		} else if len(blockedBy) > 0 {
			blockedByReason = "dependency_blocked"
		}

		rt := brainEntryToResolvedTask(task)
		rt.ResolvedDeps = resolvedDeps
		rt.UnresolvedDeps = unresolvedDeps
		rt.Classification = classification
		rt.BlockedBy = blockedBy
		rt.BlockedByReason = blockedByReason
		rt.WaitingOn = waitingOn
		rt.InCycle = inCycle[task.ID]

		// Ensure nil slices become empty slices for JSON
		if rt.ResolvedDeps == nil {
			rt.ResolvedDeps = []string{}
		}
		if rt.UnresolvedDeps == nil {
			rt.UnresolvedDeps = []string{}
		}
		if rt.BlockedBy == nil {
			rt.BlockedBy = []string{}
		}
		if rt.WaitingOn == nil {
			rt.WaitingOn = []string{}
		}
		if rt.DependsOn == nil {
			rt.DependsOn = []string{}
		}

		resolvedTasks = append(resolvedTasks, rt)
	}

	// Step 5: Compute stats
	stats := &types.TaskStats{Total: len(resolvedTasks)}
	for _, t := range resolvedTasks {
		switch t.Classification {
		case "ready":
			stats.Ready++
		case "waiting":
			stats.Waiting++
		case "blocked":
			stats.Blocked++
		case "not_pending":
			stats.NotPending++
		}
	}

	// Step 6: Extract cycle groups
	var cycles [][]string
	if len(inCycle) > 0 {
		group := make([]string, 0, len(inCycle))
		for id := range inCycle {
			group = append(group, id)
		}
		cycles = [][]string{group}
	}

	return &types.TaskListResponse{
		Tasks:  resolvedTasks,
		Count:  len(resolvedTasks),
		Stats:  stats,
		Cycles: cycles,
	}
}

// priorityOrder maps priority strings to sort order values.
var priorityOrder = map[string]int{
	"high":   0,
	"medium": 1,
	"low":    2,
}

// getPriorityOrder returns the sort order for a priority string.
// Unknown/empty priorities sort last (3).
func getPriorityOrder(p string) int {
	if order, ok := priorityOrder[p]; ok {
		return order
	}
	return 3
}

// SortByPriority sorts tasks by priority (high first), then by created date ascending.
// Returns a new slice; does not mutate the original.
func SortByPriority(tasks []types.ResolvedTask) []types.ResolvedTask {
	sorted := make([]types.ResolvedTask, len(tasks))
	copy(sorted, tasks)
	sort.SliceStable(sorted, func(i, j int) bool {
		aOrder := getPriorityOrder(sorted[i].Priority)
		bOrder := getPriorityOrder(sorted[j].Priority)
		if aOrder != bOrder {
			return aOrder < bOrder
		}
		return sorted[i].Created < sorted[j].Created
	})
	return sorted
}

// GetReadyTasks returns ready tasks sorted by priority.
func GetReadyTasks(result *types.TaskListResponse) []types.ResolvedTask {
	var ready []types.ResolvedTask
	for _, t := range result.Tasks {
		if t.Classification == "ready" {
			ready = append(ready, t)
		}
	}
	return SortByPriority(ready)
}

// GetWaitingTasks returns waiting tasks.
func GetWaitingTasks(result *types.TaskListResponse) []types.ResolvedTask {
	var waiting []types.ResolvedTask
	for _, t := range result.Tasks {
		if t.Classification == "waiting" {
			waiting = append(waiting, t)
		}
	}
	return waiting
}

// GetBlockedTasks returns blocked tasks.
func GetBlockedTasks(result *types.TaskListResponse) []types.ResolvedTask {
	var blocked []types.ResolvedTask
	for _, t := range result.Tasks {
		if t.Classification == "blocked" {
			blocked = append(blocked, t)
		}
	}
	return blocked
}

// GetNextTask returns the next task to execute with feature-based ordering.
//
// Priority order:
// 1. Tasks in "ready" features (sorted by feature priority)
// 2. Ungrouped ready tasks (no feature_id)
func GetNextTask(result *types.TaskListResponse) *types.ResolvedTask {
	allReady := GetReadyTasks(result)
	if len(allReady) == 0 {
		return nil
	}

	// Compute features from all tasks (not just ready ones)
	features := ComputeFeatures(result.Tasks)
	if len(features) == 0 {
		// No features defined, fall back to first ready task
		return &allReady[0]
	}

	// Resolve feature dependencies
	resolvedFeatures := ResolveFeatureDependencies(features)

	// Get ready features sorted by priority
	readyFeatures := GetReadyFeatures(resolvedFeatures)

	// For each ready feature, find ready tasks within it
	for _, feature := range readyFeatures {
		for i := range allReady {
			if allReady[i].FeatureID == feature.ID {
				return &allReady[i]
			}
		}
	}

	// Fall back to ungrouped ready tasks (no feature_id)
	for i := range allReady {
		if allReady[i].FeatureID == "" {
			return &allReady[i]
		}
	}

	return nil
}

// GetDownstreamTasks finds all tasks that transitively depend on a given root task.
// Returns the root task followed by dependents in topological order (Kahn's algorithm).
// Handles cycles gracefully: cycle participants are appended at the end.
func GetDownstreamTasks(rootTaskID string, allTasks []types.BrainEntry) []types.BrainEntry {
	if len(allTasks) == 0 {
		return nil
	}

	maps := BuildLookupMaps(allTasks)
	rootTask := maps.ByID[rootTaskID]
	if rootTask == nil {
		return nil
	}

	// Build reverse dependency map: taskId -> list of tasks that depend on it
	dependentsMap := make(map[string][]string)
	for _, task := range allTasks {
		for _, depRef := range task.DependsOn {
			depID := ResolveDep(depRef, maps)
			if depID == "" {
				continue
			}
			dependentsMap[depID] = append(dependentsMap[depID], task.ID)
		}
	}

	// BFS from root to collect all transitive dependents
	included := map[string]bool{rootTaskID: true}
	queue := []string{rootTaskID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, depID := range dependentsMap[current] {
			if !included[depID] {
				included[depID] = true
				queue = append(queue, depID)
			}
		}
	}

	// If only the root, return early
	if len(included) == 1 {
		return []types.BrainEntry{*rootTask}
	}

	// Topological sort (Kahn's algorithm) over the included subset
	// Compute in-degree: count of dependencies within the included set
	indegree := make(map[string]int, len(included))
	for id := range included {
		indegree[id] = 0
	}

	for id := range included {
		task := maps.ByID[id]
		if task == nil {
			continue
		}
		for _, depRef := range task.DependsOn {
			depID := ResolveDep(depRef, maps)
			if depID != "" && included[depID] {
				indegree[id]++
			}
		}
	}

	// Seed with zero-indegree nodes
	var zeroQueue []string
	for id := range included {
		if indegree[id] == 0 {
			zeroQueue = append(zeroQueue, id)
		}
	}

	var ordered []string
	for len(zeroQueue) > 0 {
		id := zeroQueue[0]
		zeroQueue = zeroQueue[1:]
		ordered = append(ordered, id)

		for _, depID := range dependentsMap[id] {
			if !included[depID] {
				continue
			}
			indegree[depID]--
			if indegree[depID] == 0 {
				zeroQueue = append(zeroQueue, depID)
			}
		}
	}

	// Cycle handling: any remaining nodes not in ordered are cycle participants
	if len(ordered) < len(included) {
		orderedSet := make(map[string]bool, len(ordered))
		for _, id := range ordered {
			orderedSet[id] = true
		}
		for id := range included {
			if !orderedSet[id] {
				ordered = append(ordered, id)
			}
		}
	}

	// Convert IDs to BrainEntries
	result := make([]types.BrainEntry, 0, len(ordered))
	for _, id := range ordered {
		if task := maps.ByID[id]; task != nil {
			result = append(result, *task)
		}
	}

	return result
}
