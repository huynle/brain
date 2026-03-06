// Package tui implements lane-based git-graph visualization for the task tree.
//
// This module provides topological sorting and lane assignment algorithms
// to visualize task dependencies as a git-style graph with merge/fork detection.
package tui

import (
	"github.com/huynle/brain-api/internal/types"
)

// MaxLanes is the maximum number of lanes to display in the graph view.
const MaxLanes = 8

// LaneAssignment represents a task's position in the lane-based graph.
type LaneAssignment struct {
	TaskID         string // Task identifier
	Lane           int    // Column position (0 = leftmost)
	ActiveLanes    []int  // Which lanes have vertical lines
	IsMerge        bool   // Is this a merge point?
	MergeFromLanes []int  // Lanes converging at merge
}

// LanePrefixSegmentRole defines the visual role of a prefix segment.
type LanePrefixSegmentRole int

const (
	RoleVertical LanePrefixSegmentRole = iota
	RoleBranch
	RoleLastBranch
	RoleMergeStart
	RoleMergeJoin
	RoleConnector
	RoleEmpty
)

// LanePrefixSegmentKind defines the semantic relationship for coloring.
type LanePrefixSegmentKind int

const (
	KindNeutral LanePrefixSegmentKind = iota
	KindUpstream
	KindDownstream
)

// LanePrefixSegment represents a single visual element in the graph prefix.
type LanePrefixSegment struct {
	Text string                // Box-drawing characters
	Lane int                   // Lane number this segment belongs to
	Role LanePrefixSegmentRole // Visual role (for rendering logic)
	Kind LanePrefixSegmentKind // Semantic kind (for coloring)
}

// TopoSort performs topological sorting on tasks using Kahn's algorithm.
//
// Returns tasks in dependency order. Tasks in cycles are appended at the end.
// Out-of-tree dependencies (tasks not in the input set) are filtered out.
func TopoSort(tasks []types.ResolvedTask) []types.ResolvedTask {
	if len(tasks) == 0 {
		return []types.ResolvedTask{}
	}

	// Build a set of task IDs for quick lookup
	taskSet := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		taskSet[task.ID] = true
	}

	// Build adjacency list and calculate in-degrees
	// adjacency[id] = list of tasks that depend on id
	adjacency := make(map[string][]string, len(tasks))
	inDegree := make(map[string]int, len(tasks))

	// Initialize all tasks with in-degree 0
	for _, task := range tasks {
		inDegree[task.ID] = 0
	}

	// Build the graph
	for _, task := range tasks {
		for _, depID := range task.DependsOn {
			// Only count in-tree dependencies
			if !taskSet[depID] {
				continue
			}
			adjacency[depID] = append(adjacency[depID], task.ID)
			inDegree[task.ID]++
		}
	}

	// Queue all tasks with in-degree 0 (no dependencies)
	queue := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if inDegree[task.ID] == 0 {
			queue = append(queue, task.ID)
		}
	}

	// Process queue using Kahn's algorithm
	result := make([]types.ResolvedTask, 0, len(tasks))
	taskMap := make(map[string]types.ResolvedTask, len(tasks))
	for _, task := range tasks {
		taskMap[task.ID] = task
	}

	processed := make(map[string]bool, len(tasks))

	for len(queue) > 0 {
		// Dequeue
		taskID := queue[0]
		queue = queue[1:]

		// Add to result
		result = append(result, taskMap[taskID])
		processed[taskID] = true

		// Decrease in-degree of dependents
		for _, dependent := range adjacency[taskID] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	// Append any tasks that weren't processed (cycles)
	for _, task := range tasks {
		if !processed[task.ID] {
			result = append(result, task)
		}
	}

	return result
}
