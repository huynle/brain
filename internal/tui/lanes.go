// Package tui implements lane-based git-graph visualization for the task tree.
//
// This module provides topological sorting and lane assignment algorithms
// to visualize task dependencies as a git-style graph with merge/fork detection.
package tui

import (
	"fmt"
	"sort"

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

// DetectMergePoints identifies tasks with 2+ in-tree dependencies.
//
// Only counts dependencies whose IDs exist in the input task set.
// Returns a map for quick lookup: map[taskID]bool
func DetectMergePoints(tasks []types.ResolvedTask) map[string]bool {
	if len(tasks) == 0 {
		return make(map[string]bool)
	}

	// Build a set of task IDs for quick lookup
	taskSet := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		taskSet[task.ID] = true
	}

	// Identify merge points
	merges := make(map[string]bool)
	for _, task := range tasks {
		// Count in-tree dependencies
		inTreeDepCount := 0
		for _, depID := range task.DependsOn {
			if taskSet[depID] {
				inTreeDepCount++
			}
		}

		// Mark as merge if 2+ in-tree dependencies
		if inTreeDepCount >= 2 {
			merges[task.ID] = true
		}
	}

	return merges
}

// AssignLanes assigns each topologically-sorted task a lane (column index).
//
// Algorithm:
// - Lane 0 = leftmost "main" lane
// - Fork: first child inherits parent's lane, others get new lanes
// - Merge: takes lowest lane from dependencies, frees the others
// - Lane reuse: freed lanes are recycled via a free list
//
// Requires tasks to be topologically sorted (use TopoSort first).
func AssignLanes(sortedTasks []types.ResolvedTask) []LaneAssignment {
	if len(sortedTasks) == 0 {
		return []LaneAssignment{}
	}

	// Build task set for quick lookup
	taskSet := make(map[string]bool, len(sortedTasks))
	for _, task := range sortedTasks {
		taskSet[task.ID] = true
	}

	// Detect merge points
	mergePoints := DetectMergePoints(sortedTasks)

	// Track which lane each task occupies
	taskLane := make(map[string]int, len(sortedTasks))

	// Active lanes currently in use
	activeLaneSet := make(map[int]bool)

	// Free list of available lanes (sorted ascending)
	freeLanes := []int{}
	nextNewLane := 0

	// Track remaining dependents for each task (for lane freeing)
	remainingDependents := make(map[string]int, len(sortedTasks))

	// Pre-compute in-tree dependents count for each task
	for _, task := range sortedTasks {
		count := 0
		for _, otherTask := range sortedTasks {
			for _, depID := range otherTask.DependsOn {
				if depID == task.ID {
					count++
					break
				}
			}
		}
		remainingDependents[task.ID] = count
	}

	// Track which parent lanes have been claimed by a child
	claimedLanes := make(map[string]bool)

	// Helper: allocate a lane from free list or create new
	allocateLane := func() int {
		if len(freeLanes) > 0 {
			// Sort to always pick lowest available
			sort.Ints(freeLanes)
			lane := freeLanes[0]
			freeLanes = freeLanes[1:]
			return lane
		}
		// Cap at MaxLanes - 1
		lane := nextNewLane
		nextNewLane++
		if lane >= MaxLanes {
			return MaxLanes - 1
		}
		return lane
	}

	// Helper: free a lane back to the free list
	freeLane := func(lane int) {
		delete(activeLaneSet, lane)
		freeLanes = append(freeLanes, lane)
	}

	// Helper: try to claim parent's lane (first child wins)
	tryClaimParentLane := func(parentID string) (int, bool) {
		parentLane, exists := taskLane[parentID]
		if !exists {
			return 0, false
		}
		key := fmt.Sprintf("%s:%d", parentID, parentLane)
		if claimedLanes[key] {
			return 0, false // already claimed
		}
		claimedLanes[key] = true
		return parentLane, true
	}

	results := make([]LaneAssignment, 0, len(sortedTasks))

	for _, task := range sortedTasks {
		// Filter in-tree dependencies
		inTreeDeps := []string{}
		for _, depID := range task.DependsOn {
			if taskSet[depID] {
				inTreeDeps = append(inTreeDeps, depID)
			}
		}

		isMerge := mergePoints[task.ID]
		var lane int
		mergeFromLanes := []int{}

		if len(inTreeDeps) == 0 {
			// Root task or independent: allocate new lane
			lane = allocateLane()
		} else if len(inTreeDeps) == 1 {
			// Single dependency: try to inherit its lane
			claimed, ok := tryClaimParentLane(inTreeDeps[0])
			if ok {
				lane = claimed
			} else {
				lane = allocateLane()
			}
		} else {
			// Merge: take lowest lane from dependencies
			depLanes := []int{}
			for _, depID := range inTreeDeps {
				claimed, ok := tryClaimParentLane(depID)
				if ok {
					depLanes = append(depLanes, claimed)
				} else {
					// Parent lane already claimed; use whatever lane the dep is on
					if dl, exists := taskLane[depID]; exists {
						depLanes = append(depLanes, dl)
					}
				}
			}

			if len(depLanes) == 0 {
				lane = allocateLane()
			} else {
				// Deduplicate and sort
				uniqueLanes := make(map[int]bool)
				for _, dl := range depLanes {
					uniqueLanes[dl] = true
				}
				sortedDepLanes := []int{}
				for dl := range uniqueLanes {
					sortedDepLanes = append(sortedDepLanes, dl)
				}
				sort.Ints(sortedDepLanes)

				lane = sortedDepLanes[0] // take lowest

				// Record merge-from lanes (all except the one we're taking)
				for _, dl := range sortedDepLanes {
					if dl != lane {
						mergeFromLanes = append(mergeFromLanes, dl)
					}
				}
			}
		}

		// Register this task's lane
		taskLane[task.ID] = lane
		activeLaneSet[lane] = true

		// Decrement remaining dependents for each in-tree dependency
		// Free lane if no more dependents and different from current lane
		for _, depID := range inTreeDeps {
			remaining := remainingDependents[depID] - 1
			remainingDependents[depID] = remaining
			if remaining <= 0 {
				if depLane, exists := taskLane[depID]; exists && depLane != lane {
					freeLane(depLane)
				}
			}
		}

		// Free merge-from lanes
		for _, ml := range mergeFromLanes {
			if activeLaneSet[ml] {
				freeLane(ml)
			}
		}

		// Snapshot active lanes at this row
		activeLanes := []int{}
		for l := range activeLaneSet {
			activeLanes = append(activeLanes, l)
		}
		sort.Ints(activeLanes)

		results = append(results, LaneAssignment{
			TaskID:         task.ID,
			Lane:           lane,
			ActiveLanes:    activeLanes,
			IsMerge:        isMerge,
			MergeFromLanes: mergeFromLanes,
		})

		// Free lanes for truly independent tasks (no deps AND no dependents)
		remaining := remainingDependents[task.ID]
		if remaining <= 0 && len(inTreeDeps) == 0 {
			freeLane(lane)
		}
	}

	return results
}

// Box-drawing characters for git-graph rendering
var laneChars = struct {
	Vertical    string
	Branch      string
	LastBranch  string
	MergeStart  string
	MergeJoin   string
	Empty       string
	Connector   string
}{
	Vertical:   "│ ",
	Branch:     "├─",
	LastBranch: "└─",
	MergeStart: "╰─",
	MergeJoin:  "┴─",
	Empty:      "  ",
	Connector:  "─",
}

// LanePrefixSegmentContext provides semantic coloring context for prefix segments.
type LanePrefixSegmentContext struct {
	UpstreamLanes   map[int]bool
	DownstreamLanes map[int]bool
}

// GeneratePrefix generates the box-drawing prefix string for a single row in the git-graph.
//
// Converts a LaneAssignment into the visual prefix that precedes the task
// marker (○) and task name. Pure function with no side effects.
//
// Examples:
//   Normal child:     │ ├─
//   Last child:       │ └─
//   Merge (2 lanes):  ╰─┴─
//   Merge (3 lanes):  ╰─┴─┴─
//   Active lanes:     │ │ ├─  (lane 0 + lane 1 active, branch at lane 2)
func GeneratePrefix(assignment LaneAssignment, index int, allAssignments []LaneAssignment, context *LanePrefixSegmentContext) string {
	segments := GeneratePrefixSegments(assignment, index, allAssignments, context)
	result := ""
	for _, seg := range segments {
		result += seg.Text
	}
	return result
}

// GeneratePrefixSegments generates structured segments for a prefix with role and kind tags.
//
// Returns a slice of LanePrefixSegment with visual role (for rendering) and
// semantic kind (for coloring). Supports both branch mode (normal parent-child)
// and merge mode (multiple lanes converging).
func GeneratePrefixSegments(assignment LaneAssignment, index int, allAssignments []LaneAssignment, context *LanePrefixSegmentContext) []LanePrefixSegment {
	if context == nil {
		context = &LanePrefixSegmentContext{
			UpstreamLanes:   make(map[int]bool),
			DownstreamLanes: make(map[int]bool),
		}
	}

	if assignment.IsMerge && len(assignment.MergeFromLanes) > 0 {
		return buildMergePrefixSegments(assignment, index, allAssignments, context)
	}

	return buildBranchPrefixSegments(assignment, index, allAssignments, context)
}

// laneActiveBelow checks whether a given lane continues below the current row.
// A lane "continues" if any subsequent assignment has it in activeLanes or is itself on that lane.
func laneActiveBelow(lane int, currentIndex int, allAssignments []LaneAssignment) bool {
	for i := currentIndex + 1; i < len(allAssignments); i++ {
		a := allAssignments[i]
		if a.Lane == lane {
			return true
		}
		for _, activeLane := range a.ActiveLanes {
			if activeLane == lane {
				return true
			}
		}
	}
	return false
}

// laneKind determines the semantic kind (for coloring) of a lane segment.
func laneKind(lane int, context *LanePrefixSegmentContext) LanePrefixSegmentKind {
	if context.UpstreamLanes[lane] {
		return KindUpstream
	}
	if context.DownstreamLanes[lane] {
		return KindDownstream
	}
	return KindNeutral
}

// createSegment creates a LanePrefixSegment with the given parameters.
func createSegment(lane int, role LanePrefixSegmentRole, text string, context *LanePrefixSegmentContext) LanePrefixSegment {
	return LanePrefixSegment{
		Text: text,
		Lane: lane,
		Role: role,
		Kind: laneKind(lane, context),
	}
}

// buildBranchPrefixSegments builds prefix for a non-merge row (root, single-dep, or fork child).
func buildBranchPrefixSegments(assignment LaneAssignment, index int, allAssignments []LaneAssignment, context *LanePrefixSegmentContext) []LanePrefixSegment {
	lane := assignment.Lane
	activeLanes := assignment.ActiveLanes
	maxLane := lane

	parts := []LanePrefixSegment{}

	// Render lanes before the current lane
	for l := 0; l < maxLane; l++ {
		isActive := false
		for _, active := range activeLanes {
			if active == l {
				isActive = true
				break
			}
		}

		if isActive {
			parts = append(parts, createSegment(l, RoleVertical, laneChars.Vertical, context))
		} else {
			parts = append(parts, createSegment(l, RoleEmpty, laneChars.Empty, context))
		}
	}

	// At the task's own lane, determine branch vs last-branch
	continues := laneActiveBelow(lane, index, allAssignments)
	if continues {
		parts = append(parts, createSegment(lane, RoleBranch, laneChars.Branch, context))
	} else {
		parts = append(parts, createSegment(lane, RoleLastBranch, laneChars.LastBranch, context))
	}

	return parts
}

// buildMergePrefixSegments builds prefix for a merge row (task has 2+ in-tree dependencies converging).
//
// Merge rendering strategy:
// - All lanes from the leftmost merge lane to the rightmost are joined
// - The leftmost merge source starts with ╰─
// - Each additional merge lane adds ┴─
// - Non-merge lanes between them get ──
// - The task's own lane ends the sequence
func buildMergePrefixSegments(assignment LaneAssignment, index int, allAssignments []LaneAssignment, context *LanePrefixSegmentContext) []LanePrefixSegment {
	lane := assignment.Lane
	activeLanes := assignment.ActiveLanes
	mergeFromLanes := assignment.MergeFromLanes

	// All lanes involved in the merge (the task's own lane + merge-from lanes)
	allMergeLanes := append([]int{lane}, mergeFromLanes...)
	
	// Sort allMergeLanes
	for i := 0; i < len(allMergeLanes); i++ {
		for j := i + 1; j < len(allMergeLanes); j++ {
			if allMergeLanes[i] > allMergeLanes[j] {
				allMergeLanes[i], allMergeLanes[j] = allMergeLanes[j], allMergeLanes[i]
			}
		}
	}

	minMergeLane := allMergeLanes[0]
	maxMergeLane := allMergeLanes[len(allMergeLanes)-1]

	// Create set for quick lookup
	mergeLaneSet := make(map[int]bool)
	for _, ml := range allMergeLanes {
		mergeLaneSet[ml] = true
	}

	parts := []LanePrefixSegment{}

	// Lanes before the merge region
	for l := 0; l < minMergeLane; l++ {
		isActive := false
		for _, active := range activeLanes {
			if active == l {
				isActive = true
				break
			}
		}

		if isActive {
			parts = append(parts, createSegment(l, RoleVertical, laneChars.Vertical, context))
		} else {
			parts = append(parts, createSegment(l, RoleEmpty, laneChars.Empty, context))
		}
	}

	// The merge region: from minMergeLane to maxMergeLane
	started := false
	for l := minMergeLane; l <= maxMergeLane; l++ {
		if mergeLaneSet[l] || l == lane {
			if !started {
				parts = append(parts, createSegment(l, RoleMergeStart, laneChars.MergeStart, context))
				started = true
			} else {
				parts = append(parts, createSegment(l, RoleMergeJoin, laneChars.MergeJoin, context))
			}
		} else {
			// Non-merge lane between merge lanes
			if started {
				parts = append(parts, createSegment(l, RoleConnector, laneChars.Connector+laneChars.Connector, context))
			} else {
				isActive := false
				for _, active := range activeLanes {
					if active == l {
						isActive = true
						break
					}
				}

				if isActive {
					parts = append(parts, createSegment(l, RoleVertical, laneChars.Vertical, context))
				} else {
					parts = append(parts, createSegment(l, RoleEmpty, laneChars.Empty, context))
				}
			}
		}
	}

	// If the task's lane is beyond maxMergeLane (shouldn't happen but handle defensively)
	if lane > maxMergeLane {
		for l := maxMergeLane + 1; l < lane; l++ {
			if started {
				parts = append(parts, createSegment(l, RoleConnector, laneChars.Connector+laneChars.Connector, context))
			} else {
				isActive := false
				for _, active := range activeLanes {
					if active == l {
						isActive = true
						break
					}
				}

				if isActive {
					parts = append(parts, createSegment(l, RoleVertical, laneChars.Vertical, context))
				} else {
					parts = append(parts, createSegment(l, RoleEmpty, laneChars.Empty, context))
				}
			}
		}
		parts = append(parts, createSegment(lane, RoleMergeStart, laneChars.MergeStart, context))
	}

	return parts
}
