package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/huynle/brain-api/internal/types"
)

// TreeNode represents a task in the dependency tree.
type TreeNode struct {
	Task     types.ResolvedTask
	Children []TreeNode
	InCycle  bool
}

// Priority ordering for sorting (lower = higher priority).
var priorityOrder = map[string]int{
	"high":   0,
	"medium": 1,
	"low":    2,
}

// Status ordering for sorting (lower = earlier in list).
var statusOrder = map[string]int{
	"in_progress": 0,
	"pending":     1,
	"blocked":     2,
	"cancelled":   3,
	"completed":   4,
	"draft":       5,
	"active":      6,
	"validated":   7,
	"superseded":  8,
	"archived":    9,
}

// Box drawing characters for tree rendering.
const (
	treeBranch     = "├─"
	treeLastBranch = "└─"
	treeVertical   = "│ "
	treeEmpty      = "  "
)

// BuildTree builds a tree structure from a flat task list using both parent_id
// and depends_on relationships. It detects cycles and handles diamond dependencies
// (each task rendered once). Tasks are sorted by priority (high > medium > low)
// then by status.
//
// Phase 3: Added allTasks parameter for parent_id chain walking.
// Phase 4: Merged parent_id and depends_on relationships (parent_id takes precedence).
func BuildTree(tasks []types.ResolvedTask, allTasks []types.ResolvedTask) []TreeNode {
	if len(tasks) == 0 {
		return nil
	}

	// Step 1: Create lookup maps
	taskMap := make(map[string]types.ResolvedTask, len(tasks))
	for _, t := range tasks {
		taskMap[t.ID] = t
	}

	allTaskMap := make(map[string]types.ResolvedTask)
	if len(allTasks) > 0 {
		for _, t := range allTasks {
			allTaskMap[t.ID] = t
		}
	} else {
		// If no allTasks provided, use tasks (backward compatibility)
		allTaskMap = taskMap
	}

	// Step 2a: Build parent_id relationships (Phase 3)
	parentChildren := make(map[string][]string)
	hasParentInTree := make(map[string]bool)

	for _, task := range tasks {
		if task.ParentID != "" {
			// Walk up parent chain to find nearest active ancestor
			activeAncestorID := findActiveAncestor(task.ParentID, taskMap, allTaskMap)
			if activeAncestorID != "" {
				hasParentInTree[task.ID] = true
				parentChildren[activeAncestorID] = append(
					parentChildren[activeAncestorID],
					task.ID,
				)
			}
		}
	}

	// Step 2b: Build reverse dependency map: parent -> children (existing)
	// If B depends on A, then A is parent of B
	children := make(map[string][]string)
	hasParent := make(map[string]bool)

	for _, t := range tasks {
		for _, depID := range t.DependsOn {
			if _, exists := taskMap[depID]; exists {
				children[depID] = append(children[depID], t.ID)
				hasParent[t.ID] = true
			}
		}
	}

	// Step 3: Merge children - UNION of parent_id and depends_on
	// Priority: parent_id takes precedence for placement
	mergedChildren := make(map[string][]string)

	// Start with parent_id children (these take priority)
	for parentID, childIDs := range parentChildren {
		mergedChildren[parentID] = append([]string{}, childIDs...)
	}

	// Add depends_on children, but ONLY if child doesn't have parent_id
	for depID, dependentIDs := range children {
		if _, exists := mergedChildren[depID]; !exists {
			mergedChildren[depID] = []string{}
		}
		for _, dependentID := range dependentIDs {
			// Only add if this task doesn't have active parent_id elsewhere
			if !hasParentInTree[dependentID] {
				mergedChildren[depID] = append(mergedChildren[depID], dependentID)
			}
		}
	}

	// Detect cycles using DFS (now using mergedChildren)
	inCycle := make(map[string]bool)
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var detectCycles func(id string) bool
	detectCycles = func(id string) bool {
		visited[id] = true
		recStack[id] = true

		for _, childID := range mergedChildren[id] {
			if !visited[childID] {
				if detectCycles(childID) {
					inCycle[id] = true
					return true
				}
			} else if recStack[childID] {
				inCycle[id] = true
				inCycle[childID] = true
				return true
			}
		}

		recStack[id] = false
		return false
	}

	for _, t := range tasks {
		if !visited[t.ID] {
			detectCycles(t.ID)
		}
	}

	// Sort children by dependency relationships first, then priority, then status.
	// Phase 6: Enhanced sibling sorting - siblings with inter-dependencies are
	// sorted topologically so that dependencies come before dependents.
	// Example: if sibling B depends on sibling A, A will be sorted before B.
	sortIDs := func(ids []string) {
		sort.Slice(ids, func(i, j int) bool {
			a := taskMap[ids[i]]
			b := taskMap[ids[j]]

			// Check if B depends on A (A should come first)
			for _, depID := range b.DependsOn {
				if depID == ids[i] {
					return true
				}
			}

			// Check if A depends on B (B should come first)
			for _, depID := range a.DependsOn {
				if depID == ids[j] {
					return false
				}
			}

			// Fall back to priority
			pa := priorityOrder[a.Priority]
			pb := priorityOrder[b.Priority]
			if pa != pb {
				return pa < pb
			}

			// Fall back to status
			return statusOrder[a.Status] < statusOrder[b.Status]
		})
	}

	// Track rendered tasks for diamond dedup
	rendered := make(map[string]bool)

	// Build tree recursively (using mergedChildren)
	var buildNode func(id string) *TreeNode
	buildNode = func(id string) *TreeNode {
		task, exists := taskMap[id]
		if !exists {
			return nil
		}
		if rendered[id] {
			return nil
		}
		rendered[id] = true

		childIDs := mergedChildren[id]
		sortIDs(childIDs)

		var childNodes []TreeNode
		for _, childID := range childIDs {
			// Skip cycle edges — cycle members are rendered as separate roots
			if inCycle[id] && inCycle[childID] {
				continue
			}
			node := buildNode(childID)
			if node != nil {
				childNodes = append(childNodes, *node)
			}
		}

		return &TreeNode{
			Task:     task,
			Children: childNodes,
			InCycle:  inCycle[id],
		}
	}

	// Sort tasks for root ordering
	sorted := make([]types.ResolvedTask, len(tasks))
	copy(sorted, tasks)
	sort.Slice(sorted, func(i, j int) bool {
		pi := priorityOrder[sorted[i].Priority]
		pj := priorityOrder[sorted[j].Priority]
		if pi != pj {
			return pi < pj
		}
		return statusOrder[sorted[i].Status] < statusOrder[sorted[j].Status]
	})

	// Collect roots (tasks with no parent in the tree - checks BOTH relationships)
	var roots []TreeNode
	for _, t := range sorted {
		// A task is a root if it has neither depends_on parent NOR parent_id parent
		if !hasParent[t.ID] && !hasParentInTree[t.ID] {
			node := buildNode(t.ID)
			if node != nil {
				roots = append(roots, *node)
			}
		}
	}

	// Handle orphans (tasks with unresolved deps or in cycles that weren't rendered)
	for _, t := range sorted {
		if !rendered[t.ID] {
			node := buildNode(t.ID)
			if node != nil {
				roots = append(roots, *node)
			}
		}
	}

	return roots
}

// findActiveAncestor walks up parent_id chain to find nearest active ancestor.
// Handles cases where parent_id points to completed/filtered tasks.
// Returns "" if no active ancestor found or if a cycle is detected.
// Uses visited set to prevent infinite loops from parent_id cycles.
func findActiveAncestor(
	parentID string,
	activeTaskMap map[string]types.ResolvedTask,
	allTaskMap map[string]types.ResolvedTask,
) string {
	// First pass: detect cycles by walking the chain
	visited := make(map[string]bool)
	currentID := parentID

	for currentID != "" {
		// Check if we've seen this ID before (cycle detection)
		if visited[currentID] {
			return "" // Cycle detected - entire chain is unsafe
		}
		visited[currentID] = true

		// Look up task in allTaskMap
		task, exists := allTaskMap[currentID]
		if !exists {
			break // Stop at non-existent task
		}

		// Move to parent
		currentID = task.ParentID
	}

	// Second pass: now that we know there's no cycle, find the first active ancestor
	currentID = parentID
	for currentID != "" {
		// Check if current ID is in activeTaskMap (found active ancestor)
		if _, exists := activeTaskMap[currentID]; exists {
			return currentID
		}

		// Look up task in allTaskMap
		task, exists := allTaskMap[currentID]
		if !exists {
			return "" // Task not found
		}

		// No parent? Stop here
		if task.ParentID == "" {
			return ""
		}

		// Move to parent
		currentID = task.ParentID
	}

	return ""
}

// FlattenTreeOrder flattens the tree into a list of task IDs in visual/navigation order.
// Parent appears before children, depth-first traversal.
func FlattenTreeOrder(tasks []types.ResolvedTask) []string {
	if len(tasks) == 0 {
		return nil
	}

	nodes := BuildTree(tasks, []types.ResolvedTask{})
	var result []string

	var traverse func(ns []TreeNode)
	traverse = func(ns []TreeNode) {
		for _, n := range ns {
			result = append(result, n.Task.ID)
			traverse(n.Children)
		}
	}
	traverse(nodes)

	return result
}

// TaskTree is the task tree component for the TUI left panel.
// It manages tree state, selection, and rendering.
type TaskTree struct {
	SelectedID string
	Cursor     int

	// Legacy tree mode (will be removed)
	nodes []TreeNode
	order []string // flattened navigation order (task IDs)
	tasks []types.ResolvedTask

	// New grouped mode
	groups           []TaskGroup
	groupCollapsed   map[string]bool // persistent collapsed state
	selectedGroupIdx int             // index into groups
	selectedTaskIdx  int             // index into group.Tasks, or -1 for group header
	useGroupedView   bool            // if true, use grouped view; if false, use tree view

	// Feature view mode
	useFeatureView         bool               // if true, group by feature_id instead of classification
	featureGroups          FeatureGroupResult // feature-grouped tasks
	featureCollapsed       map[string]bool    // feature ID -> collapsed state
	selectedFeatureIdx     int                // index into featureGroups.Features or -1 for ungrouped
	selectedFeatureTaskIdx int                // index into feature.Tasks, or -1 for feature header
	isOnUngrouped          bool               // true if selected feature is the ungrouped group

	// Phase 3: Nested status+feature grouping (3-level navigation)
	statusGroups      []StatusGroup // Nested status->feature groups
	selectedStatusIdx int           // Index into statusGroups
	isOnStatusHeader  bool          // true if cursor is on a status header

	// Hybrid view: Status groups for draft/cancelled/superseded/archived/completed within feature view
	draftCollapsed      bool
	cancelledCollapsed  bool
	supersededCollapsed bool
	archivedCollapsed   bool
	completedCollapsed  bool

	// Navigation state for terminal status sections in feature view
	// Section order: Draft → Cancelled → Superseded → Archived → Completed
	isOnDraftSection      bool // true if cursor is on Draft section (header or sub-feature)
	isOnCancelledSection  bool // true if cursor is on Cancelled section
	isOnSupersededSection bool // true if cursor is on Superseded section
	isOnArchivedSection   bool // true if cursor is on Archived section
	isOnCompletedSection  bool // true if cursor is on Completed section

	draftFeatureIdx      int // index into draftFeatureIDs (-1 = on section header)
	cancelledFeatureIdx  int // index into cancelledFeatureIDs (-1 = on section header)
	supersededFeatureIdx int // index into supersededFeatureIDs (-1 = on section header)
	archivedFeatureIdx   int // index into archivedFeatureIDs (-1 = on section header)
	completedFeatureIdx  int // index into completedFeatureIDs (-1 = on section header)

	// Task-level navigation within terminal sub-features
	// -1 = on sub-feature header (or section header); 0+ = index into sub-feature's flattened task list
	draftTaskIdx      int
	cancelledTaskIdx  int
	supersededTaskIdx int
	archivedTaskIdx   int
	completedTaskIdx  int

	draftFeatureIDs      []string // sorted feature IDs in draft section (cached during render)
	cancelledFeatureIDs  []string // sorted feature IDs in cancelled section (cached during render)
	supersededFeatureIDs []string // sorted feature IDs in superseded section (cached during render)
	archivedFeatureIDs   []string // sorted feature IDs in archived section (cached during render)
	completedFeatureIDs  []string // sorted feature IDs in completed section (cached during render)

	hasDraftTasks      bool // cached: whether draft tasks exist
	hasCancelledTasks  bool // cached: whether cancelled tasks exist
	hasSupersededTasks bool // cached: whether superseded tasks exist
	hasArchivedTasks   bool // cached: whether archived tasks exist
	hasCompletedTasks  bool // cached: whether completed tasks exist

	// Multi-select state (passed in during View rendering)
	selectedTasks map[string]bool

	// Lane-based rendering (Phase 4)
	useLaneView     bool
	laneAssignments []LaneAssignment
	laneTasks       []types.ResolvedTask // topo-sorted

	// Text wrap/truncation
	TextWrap bool // if true, show full titles; if false, truncate to fit width
}

// NewTaskTree creates a new empty TaskTree component.
func NewTaskTree() TaskTree {
	// Load collapsed state from settings
	settings, _ := LoadSettings()

	// Initialize featureCollapsed map if settings don't provide one
	featureCollapsed := settings.FeatureCollapsed
	if featureCollapsed == nil {
		featureCollapsed = make(map[string]bool)
	}

	return TaskTree{
		useGroupedView:      true, // Enable grouped view by default
		useFeatureView:      true, // Use feature-grouped view by default (matches origin/main)
		groupCollapsed:      settings.GroupCollapsed,
		featureCollapsed:    featureCollapsed,
		selectedStatusIdx:   0,
		selectedFeatureIdx:  -2,    // -2 means "none" (on status header)
		selectedTaskIdx:     -1,    // -1 means on header
		isOnStatusHeader:    true,  // Start on status header in nested mode
		draftCollapsed:      false, // Draft section expanded by default
		cancelledCollapsed:  true,  // Cancelled section collapsed by default
		supersededCollapsed: true,  // Superseded section collapsed by default
		archivedCollapsed:   true,  // Archived section collapsed by default
		completedCollapsed:  true,  // Completed section collapsed by default
	}
}

// SetViewMode sets the view mode (for testing purposes).
// If useGrouped is true, uses grouped view; otherwise uses legacy tree view.
func (tt *TaskTree) SetViewMode(useGrouped bool) {
	tt.useGroupedView = useGrouped
}

// SetFeatureViewMode enables or disables feature-based grouping.
// When enabled, tasks are grouped by feature_id instead of classification.
func (tt *TaskTree) SetFeatureViewMode(enabled bool) {
	tt.useFeatureView = enabled
	if enabled && tt.featureCollapsed == nil {
		tt.featureCollapsed = make(map[string]bool)
	}
}

// SetTasks updates the task list, rebuilds the tree, and preserves selection if possible.
func (tt *TaskTree) SetTasks(tasks []types.ResolvedTask) {
	tt.tasks = tasks

	if tt.useLaneView {
		// Lane-based visualization
		tt.laneTasks = TopoSort(tasks)
		tt.laneAssignments = AssignLanes(tt.laneTasks)

		// Preserve selection by task ID
		if tt.SelectedID != "" {
			found := false
			for i, task := range tt.laneTasks {
				if task.ID == tt.SelectedID {
					tt.Cursor = i
					found = true
					break
				}
			}
			if !found {
				// Selection lost, default to first task
				if len(tt.laneTasks) > 0 {
					tt.SelectedID = tt.laneTasks[0].ID
					tt.Cursor = 0
				} else {
					tt.SelectedID = ""
					tt.Cursor = 0
				}
			}
		} else {
			// Auto-select first task
			if len(tt.laneTasks) > 0 {
				tt.SelectedID = tt.laneTasks[0].ID
				tt.Cursor = 0
			} else {
				tt.SelectedID = ""
				tt.Cursor = 0
			}
		}

		return
	}

	if tt.useGroupedView {
		if tt.useFeatureView {
			// Feature-based grouping
			tt.featureGroups = GroupTasksByFeature(tasks)

			// Restore collapsed state for each feature (use in-memory state, not disk)
			for i := range tt.featureGroups.Features {
				featureID := tt.featureGroups.Features[i].ID
				if collapsed, ok := tt.featureCollapsed[featureID]; ok {
					tt.featureGroups.Features[i].Collapsed = collapsed
				}
			}
			if tt.featureGroups.Ungrouped != nil {
				if collapsed, ok := tt.featureCollapsed["[Ungrouped]"]; ok {
					tt.featureGroups.Ungrouped.Collapsed = collapsed
				}
			}

			// Preserve selection across SSE updates.
			// Save full navigation state BEFORE attempting restore, because
			// the user might be on a feature header (SelectedID == "") or on a
			// terminal section, which restoreFeatureSelection cannot handle.
			prevSelectedID := tt.SelectedID
			prevFeatureIdx := tt.selectedFeatureIdx
			prevFeatureTaskIdx := tt.selectedFeatureTaskIdx
			prevIsOnUngrouped := tt.isOnUngrouped
			prevIsOnDraftSection := tt.isOnDraftSection
			prevIsOnCancelledSection := tt.isOnCancelledSection
			prevIsOnSupersededSection := tt.isOnSupersededSection
			prevIsOnArchivedSection := tt.isOnArchivedSection
			prevIsOnCompletedSection := tt.isOnCompletedSection

			if prevSelectedID != "" {
				// If navigated into a terminal sub-feature task, check if the task
				// still exists — terminal section state is independent of featureGroups rebuild.
				isOnTerminalTask := (prevIsOnDraftSection || prevIsOnCancelledSection ||
					prevIsOnSupersededSection || prevIsOnArchivedSection || prevIsOnCompletedSection)
				if isOnTerminalTask {
					// Verify the task still exists in the full task list
					found := false
					for _, task := range tasks {
						if task.ID == prevSelectedID {
							found = true
							break
						}
					}
					if found {
						// Terminal section state + SelectedID is still valid, preserve position
					} else {
						// Task was removed — fall back
						tt.clearTerminalSectionNav()
						tt.selectFirstFeatureTask()
					}
				} else if tt.restoreFeatureSelection(prevSelectedID) {
					// Selection preserved — skip auto-select
				} else {
					// Selected task no longer exists — fall back to first active task
					tt.selectFirstFeatureTask()
				}
			} else if tt.isOnAnyTerminalSection() || prevIsOnDraftSection || prevIsOnCancelledSection ||
				prevIsOnSupersededSection || prevIsOnArchivedSection || prevIsOnCompletedSection {
				// On a terminal section header — preserve that position.
				// Terminal section state (isOnDraftSection, etc.) is independent of
				// featureGroups rebuild, so just keep the existing values.
			} else if prevFeatureTaskIdx == -1 {
				// On a feature header or ungrouped header — restore by index.
				if prevIsOnUngrouped && tt.featureGroups.Ungrouped != nil {
					// Was on ungrouped header — it still exists, keep position
					tt.selectedFeatureIdx = -1
					tt.selectedFeatureTaskIdx = -1
					tt.isOnUngrouped = true
					tt.SelectedID = ""
				} else if prevFeatureIdx >= 0 && prevFeatureIdx < len(tt.featureGroups.Features) {
					// Feature header — same index still valid
					tt.selectedFeatureIdx = prevFeatureIdx
					tt.selectedFeatureTaskIdx = -1
					tt.isOnUngrouped = false
					tt.SelectedID = ""
				} else if len(tt.featureGroups.Features) > 0 {
					// Feature was removed — clamp to last feature
					tt.selectedFeatureIdx = len(tt.featureGroups.Features) - 1
					tt.selectedFeatureTaskIdx = -1
					tt.isOnUngrouped = false
					tt.SelectedID = ""
				} else {
					tt.selectFirstFeatureTask()
				}
			} else {
				tt.selectFirstFeatureTask()
			}
		} else {
			// Nested status+feature grouping (default grouped view)
			settings, _ := LoadSettings()
			tt.statusGroups = GroupTasksByStatusAndFeature(tasks, settings.GroupVisible)

			// Also populate tt.groups for backwards compatibility with viewGrouped()
			// This ensures the old classification-only grouping still works
			tt.groups = GroupTasks(tasks, settings.GroupVisible)

			// Restore collapsed state for tt.groups (use in-memory state, not disk)
			// IMPORTANT: Do this BEFORE building visual order
			// Initialize missing groups as expanded (default state)
			for i := range tt.groups {
				groupName := tt.groups[i].Name
				if collapsed, ok := tt.groupCollapsed[groupName]; ok {
					tt.groups[i].Collapsed = collapsed
				} else {
					// Group not in map yet - default to expanded
					tt.groups[i].Collapsed = false
					tt.groupCollapsed[groupName] = false
				}
			}

			// Build visual order for grouped navigation
			// This is the order tasks appear in the TUI when rendered as a tree
			tt.order = []string{}
			for _, group := range tt.groups {
				if !group.Collapsed {
					tree := BuildTree(group.Tasks, tt.tasks)
					visualOrder := flattenTreeToVisualOrder(tree)
					tt.order = append(tt.order, visualOrder...)
				}
			}

			// Save previous selection state before rebuilding
			previousSelectedID := tt.SelectedID
			previousStatusIdx := tt.selectedStatusIdx
			previousFeatureIdx := tt.selectedFeatureIdx
			previousTaskIdx := tt.selectedTaskIdx
			previousIsOnStatusHeader := tt.isOnStatusHeader

			// Restore collapsed state for all 3 levels in statusGroups (use in-memory state, not disk):
			// 1. Status-level collapse (using in-memory groupCollapsed map)
			// 2. Feature-level collapse (using in-memory featureCollapsed map)
			// 3. Ungrouped-level collapse (using in-memory featureCollapsed map with status prefix)
			for i := range tt.statusGroups {
				statusName := tt.statusGroups[i].Name

				// Restore status-level collapsed state (from in-memory map)
				if collapsed, ok := tt.groupCollapsed[statusName]; ok {
					tt.statusGroups[i].Collapsed = collapsed
				}

				// Restore feature-level collapsed state (from in-memory map)
				for j := range tt.statusGroups[i].Features {
					featureID := tt.statusGroups[i].Features[j].ID
					if collapsed, ok := tt.featureCollapsed[featureID]; ok {
						tt.statusGroups[i].Features[j].Collapsed = collapsed
					}
				}

				// Restore ungrouped-level collapsed state (from in-memory map)
				if tt.statusGroups[i].Ungrouped != nil {
					ungroupedKey := statusName + ":[Ungrouped]"
					if collapsed, ok := tt.featureCollapsed[ungroupedKey]; ok {
						tt.statusGroups[i].Ungrouped.Collapsed = collapsed
					}
				}
			}

			// Preserve selection across SSE updates if the selected task still exists
			if previousSelectedID != "" {
				if tt.restoreNestedSelection(previousSelectedID) {
					// Selection preserved — skip auto-select
				} else if previousIsOnStatusHeader && previousStatusIdx < len(tt.statusGroups) {
					// Was on a status header and that header index still exists
					tt.selectedStatusIdx = previousStatusIdx
					tt.selectedFeatureIdx = -1
					tt.selectedTaskIdx = -1
					tt.isOnStatusHeader = true
					tt.SelectedID = ""
				} else {
					// Selected task no longer exists — fall back to first active task
					tt.selectFirstNestedTask()
				}
			} else if previousIsOnStatusHeader {
				// Was on a header with no task selected
				if previousStatusIdx < len(tt.statusGroups) {
					tt.selectedStatusIdx = previousStatusIdx
					tt.selectedFeatureIdx = previousFeatureIdx
					tt.selectedTaskIdx = previousTaskIdx
					tt.isOnStatusHeader = true
					tt.SelectedID = ""
				} else {
					tt.selectFirstNestedTask()
				}
			} else {
				// No previous selection — initialize
				tt.selectFirstNestedTask()
			}
		}
	} else {
		// Legacy tree view
		tt.nodes = BuildTree(tasks, []types.ResolvedTask{})
		tt.order = FlattenTreeOrder(tasks)

		// Preserve selection if the selected task still exists
		if tt.SelectedID != "" {
			for i, id := range tt.order {
				if id == tt.SelectedID {
					tt.Cursor = i
					return
				}
			}
		}

		// Auto-select first task if no selection or selection lost
		if len(tt.order) > 0 {
			tt.SelectedID = tt.order[0]
			tt.Cursor = 0
		} else {
			tt.SelectedID = ""
			tt.Cursor = 0
		}
	}
}

// selectFirstTask selects the first task in the grouped view.
func (tt *TaskTree) selectFirstTask() {
	if len(tt.groups) == 0 {
		tt.SelectedID = ""
		tt.selectedGroupIdx = 0
		tt.selectedTaskIdx = -1
		return
	}

	// Find first non-empty group and auto-select first task
	for i, group := range tt.groups {
		if len(group.Tasks) > 0 {
			tt.selectedGroupIdx = i
			tt.selectedTaskIdx = 0 // Auto-select first task
			tt.SelectedID = group.Tasks[0].ID
			return
		}
	}

	// No tasks available
	tt.SelectedID = ""
	tt.selectedGroupIdx = 0
	tt.selectedTaskIdx = -1
}

// selectFirstFeatureTask selects the first task in feature view mode.
// restoreFeatureSelection searches the new featureGroups for the previously selected task ID.
// If found, it restores selectedFeatureIdx, selectedFeatureTaskIdx, and isOnUngrouped.
// Returns true if the selection was restored, false if the task was not found.
func (tt *TaskTree) restoreFeatureSelection(previousID string) bool {
	// Search in feature groups
	for i, feature := range tt.featureGroups.Features {
		treeOrder := featureActiveTreeOrder(feature.Tasks)
		for j, id := range treeOrder {
			if id == previousID {
				tt.selectedFeatureIdx = i
				tt.selectedFeatureTaskIdx = j
				tt.isOnUngrouped = false
				tt.SelectedID = previousID
				return true
			}
		}
	}

	// Search in ungrouped
	if tt.featureGroups.Ungrouped != nil {
		treeOrder := featureActiveTreeOrder(tt.featureGroups.Ungrouped.Tasks)
		for j, id := range treeOrder {
			if id == previousID {
				tt.selectedFeatureIdx = -1
				tt.selectedFeatureTaskIdx = j
				tt.isOnUngrouped = true
				tt.SelectedID = previousID
				return true
			}
		}
	}

	return false
}

// restoreNestedSelection searches the new statusGroups for the previously selected task ID.
// If found, it restores selectedStatusIdx, selectedFeatureIdx, selectedTaskIdx, and related state.
// Returns true if the selection was restored, false if the task was not found.
func (tt *TaskTree) restoreNestedSelection(previousID string) bool {
	for i, statusGroup := range tt.statusGroups {
		// Search in feature groups within this status group
		for j, feature := range statusGroup.Features {
			for k, task := range feature.Tasks {
				if task.ID == previousID {
					tt.selectedStatusIdx = i
					tt.selectedFeatureIdx = j
					tt.selectedTaskIdx = k
					tt.isOnStatusHeader = false
					tt.SelectedID = previousID
					// Also restore Cursor for tt.order compatibility
					tt.restoreCursorInOrder(previousID)
					return true
				}
			}
		}

		// Search in ungrouped within this status group
		if statusGroup.Ungrouped != nil {
			for k, task := range statusGroup.Ungrouped.Tasks {
				if task.ID == previousID {
					tt.selectedStatusIdx = i
					tt.selectedFeatureIdx = -1
					tt.selectedTaskIdx = k
					tt.isOnStatusHeader = false
					tt.SelectedID = previousID
					// Also restore Cursor for tt.order compatibility
					tt.restoreCursorInOrder(previousID)
					return true
				}
			}
		}
	}

	return false
}

// restoreCursorInOrder restores the Cursor position in tt.order for a given task ID.
// This maintains compatibility with the grouped view navigation that uses Cursor + order.
func (tt *TaskTree) restoreCursorInOrder(taskID string) {
	for i, id := range tt.order {
		if id == taskID {
			tt.Cursor = i
			return
		}
	}
	// If not found in order (e.g., task is in collapsed group), set Cursor to 0
	tt.Cursor = 0
}

func isFeatureActiveStatus(status string) bool {
	switch status {
	case "draft", "completed", "validated", "cancelled", "superseded", "archived":
		return false
	default:
		return true
	}
}

func featureActiveTasks(tasks []types.ResolvedTask) []types.ResolvedTask {
	active := make([]types.ResolvedTask, 0, len(tasks))
	for _, task := range tasks {
		if isFeatureActiveStatus(task.Status) {
			active = append(active, task)
		}
	}
	return active
}

func featureActiveTreeOrder(tasks []types.ResolvedTask) []string {
	return FlattenTreeOrder(featureActiveTasks(tasks))
}

// terminalSubFeatureTreeOrder returns the flattened task IDs for a terminal
// sub-feature. Unlike featureActiveTreeOrder, it does NOT filter by active
// status because all tasks in terminal sections are draft/completed/etc.
func terminalSubFeatureTreeOrder(tasks []types.ResolvedTask) []string {
	return FlattenTreeOrder(tasks)
}

// getTerminalSubFeatureTasks retrieves the tasks for a specific sub-feature within a terminal section.
// It groups the provided tasks by feature_id (matching the rendering logic) and returns the tasks
// for the sub-feature at the given featureIdx.
func getTerminalSubFeatureTasks(allSectionTasks []types.ResolvedTask, featureIDs []string, featureIdx int, featureCollapsed map[string]bool, sectionName string) ([]types.ResolvedTask, bool) {
	if featureIdx < 0 || featureIdx >= len(featureIDs) {
		return nil, false
	}
	featureID := featureIDs[featureIdx]
	collapseKey := sectionName + ":" + featureID
	if featureCollapsed[collapseKey] {
		return nil, true // collapsed
	}

	byFeature := make(map[string][]types.ResolvedTask)
	for _, task := range allSectionTasks {
		fid := task.FeatureID
		if fid == "" {
			fid = "[Ungrouped]"
		}
		byFeature[fid] = append(byFeature[fid], task)
	}
	return byFeature[featureID], false
}

// collectTerminalSectionTasks returns all tasks belonging to a terminal section by status.
func (tt *TaskTree) collectTerminalSectionTasks(sectionName string) []types.ResolvedTask {
	var tasks []types.ResolvedTask
	statusFilter := map[string]bool{}
	switch sectionName {
	case "draft":
		statusFilter["draft"] = true
	case "cancelled":
		statusFilter["cancelled"] = true
	case "superseded":
		statusFilter["superseded"] = true
	case "archived":
		statusFilter["archived"] = true
	case "completed":
		statusFilter["completed"] = true
		statusFilter["validated"] = true
	}

	for _, feature := range tt.featureGroups.Features {
		for _, task := range feature.Tasks {
			if statusFilter[task.Status] {
				tasks = append(tasks, task)
			}
		}
	}
	if tt.featureGroups.Ungrouped != nil {
		for _, task := range tt.featureGroups.Ungrouped.Tasks {
			if statusFilter[task.Status] {
				tasks = append(tasks, task)
			}
		}
	}
	return tasks
}

func (tt *TaskTree) selectFirstFeatureTask() {
	// Try features first - select first ACTIVE task
	if len(tt.featureGroups.Features) > 0 {
		for i, feature := range tt.featureGroups.Features {
			treeOrder := featureActiveTreeOrder(feature.Tasks)
			if len(treeOrder) > 0 {
				tt.selectedFeatureIdx = i
				tt.selectedFeatureTaskIdx = 0
				tt.isOnUngrouped = false
				tt.SelectedID = treeOrder[0]
				return
			}
		}
	}

	// Fall back to ungrouped if no active tasks in features
	if tt.featureGroups.Ungrouped != nil && len(tt.featureGroups.Ungrouped.Tasks) > 0 {
		treeOrder := featureActiveTreeOrder(tt.featureGroups.Ungrouped.Tasks)
		if len(treeOrder) > 0 {
			tt.selectedFeatureIdx = -1
			tt.selectedFeatureTaskIdx = 0
			tt.isOnUngrouped = true
			tt.SelectedID = treeOrder[0]
			return
		}
	}

	// No active tasks - fall back to Draft section header.
	// Place cursor on the Draft section header so that j/k navigation
	// traverses draft sub-features via the terminal section navigation path.
	hasDraftTasks := false
	for _, feature := range tt.featureGroups.Features {
		for _, task := range feature.Tasks {
			if task.Status == "draft" {
				hasDraftTasks = true
				break
			}
		}
		if hasDraftTasks {
			break
		}
	}
	if !hasDraftTasks && tt.featureGroups.Ungrouped != nil {
		for _, task := range tt.featureGroups.Ungrouped.Tasks {
			if task.Status == "draft" {
				hasDraftTasks = true
				break
			}
		}
	}
	if hasDraftTasks {
		tt.clearTerminalSectionNav()
		tt.isOnDraftSection = true
		tt.hasDraftTasks = true
		tt.draftFeatureIdx = -1 // On the Draft section header itself
		tt.selectedFeatureIdx = -1
		tt.selectedFeatureTaskIdx = -1
		tt.isOnUngrouped = false
		tt.SelectedID = ""

		// Eagerly populate draftFeatureIDs so navigation works before first render.
		// The View function also sets this, but navigation may run before View.
		draftFeatureMap := make(map[string]bool)
		var draftFIDs []string
		for _, feature := range tt.featureGroups.Features {
			for _, task := range feature.Tasks {
				if task.Status == "draft" && !draftFeatureMap[feature.ID] {
					draftFeatureMap[feature.ID] = true
					draftFIDs = append(draftFIDs, feature.ID)
				}
			}
		}
		if tt.featureGroups.Ungrouped != nil {
			for _, task := range tt.featureGroups.Ungrouped.Tasks {
				if task.Status == "draft" && !draftFeatureMap["[Ungrouped]"] {
					draftFeatureMap["[Ungrouped]"] = true
					draftFIDs = append(draftFIDs, "[Ungrouped]")
				}
			}
		}
		tt.draftFeatureIDs = draftFIDs
		return
	}

	// Fall back to Completed section header if there are completed tasks
	hasCompletedTasks := false
	for _, feature := range tt.featureGroups.Features {
		for _, task := range feature.Tasks {
			if task.Status == "completed" || task.Status == "validated" {
				hasCompletedTasks = true
				break
			}
		}
		if hasCompletedTasks {
			break
		}
	}
	if hasCompletedTasks {
		tt.clearTerminalSectionNav()
		tt.isOnCompletedSection = true
		tt.completedFeatureIdx = -1
		tt.selectedFeatureIdx = -1
		tt.selectedFeatureTaskIdx = -1
		tt.isOnUngrouped = false
		tt.SelectedID = ""
		return
	}

	// No tasks available
	tt.SelectedID = ""
	tt.selectedFeatureIdx = -1
	tt.selectedFeatureTaskIdx = -1
	tt.isOnUngrouped = false
}

// selectFirstNestedTask selects the first task in nested status+feature view mode.
// Initializes 3-level navigation: status groups → feature groups → tasks.
func (tt *TaskTree) selectFirstNestedTask() {
	// Find first active (non-terminal) status group
	// Terminal groups: Completed, Validated, Superseded, Archived, Cancelled
	terminalStatuses := map[string]bool{
		"Completed":  true,
		"Validated":  true,
		"Superseded": true,
		"Archived":   true,
		"Cancelled":  true,
	}

	firstActiveIdx := -1
	for i, group := range tt.statusGroups {
		if !terminalStatuses[group.Name] {
			firstActiveIdx = i
			break
		}
	}

	// If we found an active group, select it; otherwise select first group (fallback)
	if firstActiveIdx >= 0 {
		tt.selectedStatusIdx = firstActiveIdx
	} else if len(tt.statusGroups) > 0 {
		tt.selectedStatusIdx = 0
	} else {
		// No tasks available
		tt.SelectedID = ""
		tt.selectedStatusIdx = 0
		tt.selectedFeatureIdx = -1
		tt.selectedTaskIdx = -1 // FIXED: use selectedTaskIdx for nested navigation
		tt.isOnStatusHeader = false
		return
	}

	// Auto-select first visible task in the selected status group
	statusGroup := tt.statusGroups[tt.selectedStatusIdx]

	// Try features first
	if len(statusGroup.Features) > 0 {
		// Find first feature with tasks
		for i, feature := range statusGroup.Features {
			if len(feature.Tasks) > 0 {
				tt.selectedFeatureIdx = i
				tt.selectedTaskIdx = 0 // FIXED: use selectedTaskIdx for nested navigation
				tt.isOnStatusHeader = false
				tt.SelectedID = feature.Tasks[0].ID
				return
			}
		}
	}

	// Fall back to ungrouped if no features with tasks
	if statusGroup.Ungrouped != nil && len(statusGroup.Ungrouped.Tasks) > 0 {
		tt.selectedFeatureIdx = -1
		tt.selectedTaskIdx = 0 // FIXED: use selectedTaskIdx for nested navigation
		tt.isOnStatusHeader = false
		tt.SelectedID = statusGroup.Ungrouped.Tasks[0].ID
		return
	}

	// No tasks in this status group - stay on status header
	tt.selectedFeatureIdx = -1
	tt.selectedTaskIdx = -1
	tt.isOnStatusHeader = true
	tt.SelectedID = ""
}

// MoveDown moves the cursor down one position.
func (tt *TaskTree) MoveDown() {
	if tt.useLaneView {
		tt.moveDownLane()
	} else if tt.useGroupedView {
		tt.moveDownGrouped()
	} else {
		tt.moveDownLegacy()
	}
}

// moveDownLegacy is the original tree-based navigation.
func (tt *TaskTree) moveDownLegacy() {
	if len(tt.order) == 0 {
		return
	}
	if tt.Cursor < len(tt.order)-1 {
		tt.Cursor++
		tt.SelectedID = tt.order[tt.Cursor]
	}
}

// moveDownGrouped navigates down in grouped view.
func (tt *TaskTree) moveDownGrouped() {
	// Auto-detect nested grouping mode and delegate
	if len(tt.statusGroups) > 0 && !tt.useFeatureView {
		tt.moveDownNestedGrouped()
		return
	}

	// Classification-only grouping or feature view (handled by legacy code below)
	if len(tt.groups) == 0 && !tt.useFeatureView {
		return
	}

	// Feature view navigation (uses featureGroups)
	if tt.useFeatureView {
		tt.moveDownFeatureGrouped()
		return
	}

	// Classification-only grouping - navigate through visual order
	if len(tt.groups) > 0 {
		// If on group header, move to first task
		if tt.selectedTaskIdx == -1 {
			group := tt.groups[tt.selectedGroupIdx]
			debugLog("[NAV_DOWN] On group header %s, collapsed=%v", group.Name, group.Collapsed)
			if group.Collapsed {
				// Group is collapsed, jump to next group header
				if tt.selectedGroupIdx < len(tt.groups)-1 {
					tt.selectedGroupIdx++
					tt.selectedTaskIdx = -1
					tt.SelectedID = ""
					debugLog("[NAV_DOWN] Skipped to next group header")
				}
			} else {
				// Group is expanded, enter first task using visual order
				tree := BuildTree(group.Tasks, tt.tasks)
				visualOrder := flattenTreeToVisualOrder(tree)
				debugLog("[NAV_DOWN] Visual order has %d tasks", len(visualOrder))
				if len(visualOrder) > 0 {
					tt.SelectedID = visualOrder[0]
					tt.selectedTaskIdx = 0
					// Update Cursor to match position in global order
					for i, id := range tt.order {
						if id == tt.SelectedID {
							tt.Cursor = i
							break
						}
					}
					debugLog("[NAV_DOWN] Moved to first task: ID=%s, Cursor=%d", tt.SelectedID, tt.Cursor)
				}
			}
		} else {
			// Within tasks - navigate through tt.order using Cursor
			debugLog("[NAV_DOWN] Within tasks: Cursor=%d, len(order)=%d, SelectedID=%s", tt.Cursor, len(tt.order), tt.SelectedID)
			if len(tt.order) > 0 && tt.Cursor < len(tt.order)-1 {
				tt.Cursor++
				tt.SelectedID = tt.order[tt.Cursor]
				tt.selectedTaskIdx++
				debugLog("[NAV_DOWN] Moved to next task: Cursor=%d, SelectedID=%s", tt.Cursor, tt.SelectedID)
			} else {
				// At end of all tasks - move to next group header if exists
				if tt.selectedGroupIdx < len(tt.groups)-1 {
					tt.selectedGroupIdx++
					tt.selectedTaskIdx = -1
					tt.SelectedID = ""
					debugLog("[NAV_DOWN] Reached end, moved to next group header")
				} else {
					debugLog("[NAV_DOWN] At end of all tasks")
				}
			}
		}
	}
}

// MoveUp moves the cursor up one position.
func (tt *TaskTree) MoveUp() {
	if tt.useLaneView {
		tt.moveUpLane()
	} else if tt.useGroupedView {
		tt.moveUpGrouped()
	} else {
		tt.moveUpLegacy()
	}
}

// moveUpLegacy is the original tree-based navigation.
func (tt *TaskTree) moveUpLegacy() {
	if len(tt.order) == 0 {
		return
	}
	if tt.Cursor > 0 {
		tt.Cursor--
		tt.SelectedID = tt.order[tt.Cursor]
	}
}

// moveUpGrouped navigates up in grouped view.
func (tt *TaskTree) moveUpGrouped() {
	// Auto-detect nested grouping mode and delegate
	if len(tt.statusGroups) > 0 && !tt.useFeatureView {
		tt.moveUpNestedGrouped()
		return
	}

	// Feature view navigation
	if tt.useFeatureView {
		tt.moveUpFeatureGrouped()
		return
	}

	// Classification-only grouping - navigate through visual order
	if len(tt.groups) == 0 {
		return
	}

	if tt.selectedTaskIdx == -1 {
		// On group header, move to previous group
		debugLog("[NAV_UP] On group header, selectedGroupIdx=%d", tt.selectedGroupIdx)
		if tt.selectedGroupIdx > 0 {
			tt.selectedGroupIdx--
			prevGroup := tt.groups[tt.selectedGroupIdx]
			// Land on last task of previous group if expanded
			if !prevGroup.Collapsed {
				tree := BuildTree(prevGroup.Tasks, tt.tasks)
				visualOrder := flattenTreeToVisualOrder(tree)
				if len(visualOrder) > 0 {
					lastTaskID := visualOrder[len(visualOrder)-1]
					tt.SelectedID = lastTaskID
					tt.selectedTaskIdx = len(visualOrder) - 1
					// Update Cursor to match position in global order
					for i, id := range tt.order {
						if id == lastTaskID {
							tt.Cursor = i
							break
						}
					}
					debugLog("[NAV_UP] Moved to last task of prev group: ID=%s, Cursor=%d", lastTaskID, tt.Cursor)
				}
			} else {
				// Stay on group header
				tt.selectedTaskIdx = -1
				tt.SelectedID = ""
				debugLog("[NAV_UP] Moved to prev group header (collapsed)")
			}
		}
	} else {
		// Within tasks - navigate through tt.order using Cursor
		debugLog("[NAV_UP] Within tasks: Cursor=%d, len(order)=%d, SelectedID=%s", tt.Cursor, len(tt.order), tt.SelectedID)
		if len(tt.order) > 0 && tt.Cursor > 0 {
			tt.Cursor--
			tt.SelectedID = tt.order[tt.Cursor]
			tt.selectedTaskIdx--
			debugLog("[NAV_UP] Moved to prev task: Cursor=%d, SelectedID=%s", tt.Cursor, tt.SelectedID)
		} else {
			// At top of tasks, move to group header
			tt.selectedTaskIdx = -1
			tt.SelectedID = ""
			debugLog("[NAV_UP] Reached top, moved to group header")
		}
	}
}

// MoveToTop moves the cursor to the first task.
func (tt *TaskTree) MoveToTop() {
	if tt.useLaneView {
		tt.moveToTopLane()
	} else if tt.useGroupedView {
		tt.moveToTopGrouped()
	} else {
		tt.moveToTopLegacy()
	}
}

// moveToTopLegacy is the original tree-based navigation.
func (tt *TaskTree) moveToTopLegacy() {
	if len(tt.order) == 0 {
		return
	}
	tt.Cursor = 0
	tt.SelectedID = tt.order[0]
}

// moveToTopGrouped moves to the first task in grouped view.
func (tt *TaskTree) moveToTopGrouped() {
	// Auto-detect nested grouping mode and delegate
	if len(tt.statusGroups) > 0 && !tt.useFeatureView {
		tt.moveToTopNestedGrouped()
		return
	}

	// Feature view navigation
	if tt.useFeatureView {
		tt.moveToTopFeatureGrouped()
		return
	}

	if len(tt.groups) == 0 {
		return
	}

	// Find first non-empty group and ALWAYS start on header
	for i, group := range tt.groups {
		if len(group.Tasks) > 0 {
			tt.selectedGroupIdx = i
			tt.selectedTaskIdx = -1 // ALWAYS start on header
			tt.SelectedID = ""
			return
		}
	}
}

// MoveToBottom moves the cursor to the last task.
func (tt *TaskTree) MoveToBottom() {
	if tt.useLaneView {
		tt.moveToBottomLane()
	} else if tt.useGroupedView {
		tt.moveToBottomGrouped()
	} else {
		tt.moveToBottomLegacy()
	}
}

// moveToBottomLegacy is the original tree-based navigation.
func (tt *TaskTree) moveToBottomLegacy() {
	if len(tt.order) == 0 {
		return
	}
	tt.Cursor = len(tt.order) - 1
	tt.SelectedID = tt.order[tt.Cursor]
}

// moveToBottomGrouped moves to the last task in grouped view.
func (tt *TaskTree) moveToBottomGrouped() {
	// Auto-detect nested grouping mode and delegate
	if len(tt.statusGroups) > 0 && !tt.useFeatureView {
		tt.moveToBottomNestedGrouped()
		return
	}

	// Feature view navigation
	if tt.useFeatureView {
		tt.moveToBottomFeatureGrouped()
		return
	}

	if len(tt.groups) == 0 {
		return
	}

	// Find last non-empty group
	for i := len(tt.groups) - 1; i >= 0; i-- {
		group := tt.groups[i]
		if len(group.Tasks) > 0 {
			tt.selectedGroupIdx = i
			if group.Collapsed {
				// Stay on group header
				tt.selectedTaskIdx = -1
				tt.SelectedID = ""
			} else {
				// Move to last task in group
				tt.selectedTaskIdx = len(group.Tasks) - 1
				tt.SelectedID = group.Tasks[tt.selectedTaskIdx].ID
			}
			return
		}
	}
}

// ToggleCollapse toggles the collapsed state of the currently selected group.
// Only works if the cursor is on a group header. Persists state to settings.
func (tt *TaskTree) ToggleCollapse() {
	if !tt.useGroupedView {
		return
	}

	// Phase 3: Nested status+feature grouping mode (only if NOT in feature view mode)
	if !tt.useFeatureView && len(tt.statusGroups) > 0 {
		// On status header - toggle status group
		if tt.isOnStatusHeader {
			tt.statusGroups[tt.selectedStatusIdx].Collapsed = !tt.statusGroups[tt.selectedStatusIdx].Collapsed
			// Use status name as collapse key
			statusName := tt.statusGroups[tt.selectedStatusIdx].Name
			tt.groupCollapsed[statusName] = tt.statusGroups[tt.selectedStatusIdx].Collapsed

			settings, _ := LoadSettings()
			settings.GroupCollapsed = tt.groupCollapsed
			settings.FeatureCollapsed = tt.featureCollapsed
			_ = SaveSettings(settings)
			return
		}

		// On feature header - toggle feature (use hierarchical keys)
		if tt.selectedTaskIdx == -1 {
			statusName := tt.getCurrentStatusName()
			featureID := tt.getCurrentFeatureID()

			if featureID == "" {
				return
			}

			// Toggle the actual feature
			if tt.selectedFeatureIdx == -1 {
				// Ungrouped
				ungrouped := tt.statusGroups[tt.selectedStatusIdx].Ungrouped
				if ungrouped != nil {
					ungrouped.Collapsed = !ungrouped.Collapsed
				}
			} else if tt.selectedFeatureIdx >= 0 && tt.selectedFeatureIdx < len(tt.statusGroups[tt.selectedStatusIdx].Features) {
				// Regular feature
				tt.statusGroups[tt.selectedStatusIdx].Features[tt.selectedFeatureIdx].Collapsed = !tt.statusGroups[tt.selectedStatusIdx].Features[tt.selectedFeatureIdx].Collapsed
			}

			// Use hierarchical collapse key
			collapseKey := makeFeatureCollapseKey(statusName, featureID)
			collapsed := isFeatureCollapsed(statusName, featureID, tt.featureCollapsed)
			tt.featureCollapsed[collapseKey] = !collapsed

			settings, _ := LoadSettings()
			settings.GroupCollapsed = tt.groupCollapsed
			settings.FeatureCollapsed = tt.featureCollapsed
			_ = SaveSettings(settings)
			return
		}

		// On task - do nothing
		return
	}

	if tt.useFeatureView {
		// Feature view mode
		if tt.selectedFeatureTaskIdx != -1 {
			return // Only toggle on headers
		}

		// Handle terminal section toggle (Draft/Cancelled/Superseded/Archived/Completed)
		if tt.isOnAnyTerminalSection() {
			// If we're on a task within a terminal sub-feature, don't toggle
			if tt.draftTaskIdx >= 0 || tt.cancelledTaskIdx >= 0 || tt.supersededTaskIdx >= 0 || tt.archivedTaskIdx >= 0 || tt.completedTaskIdx >= 0 {
				return // Only toggle on headers, not on individual tasks
			}
			// Toggle using the collapsed pointer and feature IDs from terminalSections
			type collapsibleSection struct {
				isOn       bool
				collapsed  *bool
				featureIdx int
				featureIDs []string
				name       string
			}
			secs := []collapsibleSection{
				{tt.isOnDraftSection, &tt.draftCollapsed, tt.draftFeatureIdx, tt.draftFeatureIDs, "draft"},
				{tt.isOnCancelledSection, &tt.cancelledCollapsed, tt.cancelledFeatureIdx, tt.cancelledFeatureIDs, "cancelled"},
				{tt.isOnSupersededSection, &tt.supersededCollapsed, tt.supersededFeatureIdx, tt.supersededFeatureIDs, "superseded"},
				{tt.isOnArchivedSection, &tt.archivedCollapsed, tt.archivedFeatureIdx, tt.archivedFeatureIDs, "archived"},
				{tt.isOnCompletedSection, &tt.completedCollapsed, tt.completedFeatureIdx, tt.completedFeatureIDs, "completed"},
			}
			for _, sec := range secs {
				if !sec.isOn {
					continue
				}
				if sec.featureIdx == -1 {
					// On section header → toggle entire section
					*sec.collapsed = !*sec.collapsed
				} else if sec.featureIdx >= 0 && sec.featureIdx < len(sec.featureIDs) {
					// On sub-feature header → toggle that sub-feature
					featureID := sec.featureIDs[sec.featureIdx]
					collapseKey := sec.name + ":" + featureID
					tt.featureCollapsed[collapseKey] = !tt.featureCollapsed[collapseKey]
				}
				break
			}

			settings, _ := LoadSettings()
			settings.GroupCollapsed = tt.groupCollapsed
			settings.FeatureCollapsed = tt.featureCollapsed
			_ = SaveSettings(settings)
			return
		}

		if tt.isOnUngrouped && tt.featureGroups.Ungrouped != nil {
			// Toggle ungrouped
			tt.featureGroups.Ungrouped.Collapsed = !tt.featureGroups.Ungrouped.Collapsed
			tt.featureCollapsed["[Ungrouped]"] = tt.featureGroups.Ungrouped.Collapsed
		} else if tt.selectedFeatureIdx >= 0 && tt.selectedFeatureIdx < len(tt.featureGroups.Features) {
			// Toggle feature
			featureID := tt.featureGroups.Features[tt.selectedFeatureIdx].ID
			tt.featureGroups.Features[tt.selectedFeatureIdx].Collapsed = !tt.featureGroups.Features[tt.selectedFeatureIdx].Collapsed
			tt.featureCollapsed[featureID] = tt.featureGroups.Features[tt.selectedFeatureIdx].Collapsed
		}

		// Persist feature collapsed state to settings
		settings, _ := LoadSettings()
		settings.GroupCollapsed = tt.groupCollapsed
		settings.FeatureCollapsed = tt.featureCollapsed
		_ = SaveSettings(settings) // Ignore errors (non-critical)
		return
	}

	// Classification group mode
	if len(tt.groups) == 0 {
		return
	}

	// Only toggle if on group header
	if tt.selectedTaskIdx != -1 {
		return
	}

	// Toggle collapsed state
	groupName := tt.groups[tt.selectedGroupIdx].Name
	tt.groups[tt.selectedGroupIdx].Collapsed = !tt.groups[tt.selectedGroupIdx].Collapsed
	tt.groupCollapsed[groupName] = tt.groups[tt.selectedGroupIdx].Collapsed

	// Persist to settings (both group and feature states)
	settings, _ := LoadSettings()
	settings.GroupCollapsed = tt.groupCollapsed
	settings.FeatureCollapsed = tt.featureCollapsed
	_ = SaveSettings(settings) // Ignore errors (non-critical)
}

// IsOnGroupHeader returns true if the cursor is on a group header.
// Works for both classification group view and feature view modes.
func (tt *TaskTree) IsOnGroupHeader() bool {
	if !tt.useGroupedView {
		return false
	}

	if tt.useFeatureView {
		// Feature view mode: on a header when selectedFeatureTaskIdx == -1
		// and we're either on a feature header or the ungrouped header
		if tt.selectedFeatureTaskIdx != -1 {
			return false
		}
		hasFeatures := len(tt.featureGroups.Features) > 0
		hasUngrouped := tt.featureGroups.Ungrouped != nil
		return hasFeatures || (hasUngrouped && tt.isOnUngrouped)
	}

	// Classification group mode
	if len(tt.groups) == 0 {
		return false
	}
	return tt.selectedTaskIdx == -1
}

// SelectedTask returns the currently selected task, or nil if none.
func (tt *TaskTree) SelectedTask() *types.ResolvedTask {
	if tt.SelectedID == "" || len(tt.tasks) == 0 {
		return nil
	}
	for i := range tt.tasks {
		if tt.tasks[i].ID == tt.SelectedID {
			return &tt.tasks[i]
		}
	}
	return nil
}

// GetSelectedFeatureID returns the feature ID of the currently selected feature header.
// Returns "" if not in feature view, not on a header, on ungrouped, or index out of bounds.
func (tt *TaskTree) GetSelectedFeatureID() string {
	// Only works in feature view mode
	if !tt.useFeatureView {
		return ""
	}

	// Only works when on a header (not on a task)
	if tt.selectedFeatureTaskIdx != -1 {
		return ""
	}

	// Return "" if on ungrouped
	if tt.isOnUngrouped {
		return ""
	}

	// Check terminal sections (Draft, Cancelled, Superseded, Archived, Completed)
	// Each terminal section has its own feature index and feature IDs list.
	if tt.isOnAnyTerminalSection() {
		for _, sec := range tt.terminalSections() {
			if sec.isOn() {
				idx := sec.featureIdx()
				ids := sec.featureIDs()
				// idx == -1 means on the section header itself, not a sub-feature
				if idx < 0 || idx >= len(ids) {
					return ""
				}
				return ids[idx]
			}
		}
		return ""
	}

	// Active/pending features: check bounds against featureGroups
	if tt.selectedFeatureIdx < 0 || tt.selectedFeatureIdx >= len(tt.featureGroups.Features) {
		return ""
	}

	return tt.featureGroups.Features[tt.selectedFeatureIdx].ID
}

// statusIndicator returns the status icon for a task.
// For pending tasks, classification (dependency state) takes precedence to show readiness.
// For other statuses, status (execution state) takes precedence.
func statusIndicator(status, classification string) string {
	// Priority 1: For pending tasks, check classification first to show readiness
	if status == "pending" {
		switch classification {
		case "ready":
			return IndicatorReady // ● green - ready to execute
		case "waiting":
			return IndicatorWaiting // ○ yellow - waiting on dependencies
		case "blocked":
			return IndicatorBlocked // ✗ red - blocked by deps
		default:
			// pending with unknown classification defaults to waiting
			return IndicatorWaiting // ○ yellow
		}
	}

	// Priority 2: Handle all other explicit status values
	switch status {
	case "draft":
		return IndicatorWaiting // ○ gray
	case "active":
		return IndicatorReady // ● blue
	case "in_progress":
		return IndicatorActive // ▶ cyan
	case "blocked":
		return IndicatorBlocked // ✗ red
	case "cancelled":
		return IndicatorCancelled // ⊘ magenta
	case "completed":
		return IndicatorCompleted // ✓ green dim
	case "validated":
		return IndicatorCompleted // ✓ green bright
	case "superseded":
		return IndicatorWaiting // ○ gray
	case "archived":
		return IndicatorWaiting // ○ gray
	}

	// Priority 3: Fall back to classification (dependency state) for tasks without explicit status
	switch classification {
	case "ready":
		return IndicatorReady
	case "waiting":
		return IndicatorWaiting
	case "blocked":
		return IndicatorBlocked
	default:
		return IndicatorCompleted
	}
}

// View renders the task tree as a string within the given dimensions.
// This is the legacy method without project label support. Use ViewWithProject instead.
func (tt *TaskTree) View(width, height int) string {
	return tt.ViewWithProject(width, height, "")
}

// ViewWithSelection renders the task tree with multi-select checkboxes and project labels.
// When activeProjectID == "all", shows [project-name] prefix for each task.
func (tt *TaskTree) ViewWithSelection(width, height int, selectedTasks map[string]bool, activeProjectID string) string {
	// Store selection for rendering
	tt.selectedTasks = selectedTasks
	return tt.ViewWithProject(width, height, activeProjectID)
}

// ViewWithProject renders the task tree with optional project labels.
// When activeProjectID == "all", shows [project-name] prefix for each task.
func (tt *TaskTree) ViewWithProject(width, height int, activeProjectID string) string {
	// Render "Tasks (N)" header with bold and underline
	totalCount := len(tt.tasks)
	header := lipgloss.NewStyle().
		Bold(true).
		Underline(true).
		Render(fmt.Sprintf("Tasks (%d)", totalCount))

	// Add blank line after header (marginTop={1} in TypeScript)
	content := "\n"

	// Adjust height to account for header + blank line
	contentHeight := height
	if height > 0 {
		contentHeight = height - 2 // Subtract header line + blank line
		if contentHeight < 0 {
			contentHeight = 0
		}
	}

	// Check lane view first (takes precedence over grouped view)
	if tt.useLaneView {
		content += tt.viewLaneTree(width, contentHeight)
	} else if tt.useGroupedView {
		// Phase 4: Use nested view when statusGroups is populated
		if len(tt.statusGroups) > 0 && !tt.useFeatureView {
			content += tt.viewNestedGrouped(width, contentHeight, activeProjectID)
		} else if tt.useFeatureView {
			content += tt.viewFeatureGrouped(width, contentHeight, activeProjectID)
		} else {
			content += tt.viewGrouped(width, contentHeight, activeProjectID)
		}
	} else {
		content += tt.viewLegacy(width, contentHeight)
	}

	return header + content
}

// viewLegacy is the original tree-based rendering.
func (tt *TaskTree) viewLegacy(width, height int) string {
	if len(tt.nodes) == 0 {
		return DimStyle.Render("  No tasks")
	}

	var lines []string
	showCheckboxes := len(tt.selectedTasks) > 0
	tt.renderNodes(tt.nodes, "", &lines, width, showCheckboxes)

	// Truncate to height with scroll overflow indicators
	if height > 0 && len(lines) > height {
		// Ensure selected item is visible
		start := 0
		if tt.Cursor >= height {
			start = tt.Cursor - height + 1
		}
		end := start + height
		if end > len(lines) {
			end = len(lines)
		}
		totalLines := len(lines)
		lines = applyViewportWithIndicators(lines, start, end, totalLines)
	}

	return strings.Join(lines, "\n")
}

// viewGrouped renders tasks in grouped view with collapsible headers.
func (tt *TaskTree) viewGrouped(width, height int, activeProjectID string) string {
	if len(tt.groups) == 0 {
		return DimStyle.Render("  No tasks")
	}

	var lines []string

	// Compute whether to show checkboxes (only when multi-select is active)
	showCheckboxes := len(tt.selectedTasks) > 0

	for gIdx, group := range tt.groups {
		// Render group header
		isGroupSelected := (gIdx == tt.selectedGroupIdx && tt.selectedTaskIdx == -1)

		// Collapse indicator (▶ collapsed, ▾ expanded)
		collapseIndicator := "▶"
		if !group.Collapsed {
			collapseIndicator = "▾"
		}

		groupHeader := fmt.Sprintf("%s %s (%d)", collapseIndicator, group.Name, group.Count)

		// Selection marker (distinct from collapse indicator)
		if isGroupSelected {
			groupHeader = GroupHeaderStyle.Render(groupHeader)
			groupHeader = fmt.Sprintf("→ %s", groupHeader) // Use arrow for selection
		} else {
			groupHeader = GroupHeaderStyle.Render(groupHeader)
			groupHeader = fmt.Sprintf("  %s", groupHeader) // Two spaces for alignment
		}

		lines = append(lines, groupHeader)

		// Render tasks if not collapsed
		if !group.Collapsed {
			// Build dependency tree for this group
			tree := BuildTree(group.Tasks, tt.tasks)

			// Render tree with proper indentation
			visualIndex := 0
			tt.renderGroupTaskTree(
				tree,
				"    ", // base indentation for grouped view
				&lines,
				width,
				gIdx,
				&visualIndex,
				tt.selectedTasks,
				showCheckboxes,
				activeProjectID,
				tt.selectedGroupIdx, // 2-level view uses selectedGroupIdx
				nil, nil,            // no relation highlighting in 2-level grouped view
			)
		}
	}

	// Handle viewport scrolling (ensure selected item is visible)
	if height > 0 && len(lines) > height {
		// Find the line index of the selected item
		selectedLineIdx := 0
		lineIdx := 0

		for gIdx, group := range tt.groups {
			// Check if group header is selected
			if gIdx == tt.selectedGroupIdx && tt.selectedTaskIdx == -1 {
				selectedLineIdx = lineIdx
				break
			}
			lineIdx++ // group header line

			// Check tasks in this group if not collapsed
			if !group.Collapsed {
				// Count lines for tasks in this group
				taskLineCount := countGroupTaskLines(BuildTree(group.Tasks, tt.tasks))

				// If selected task is in this group
				if gIdx == tt.selectedGroupIdx && tt.selectedTaskIdx >= 0 {
					selectedLineIdx = lineIdx + tt.selectedTaskIdx
					break
				}
				lineIdx += taskLineCount
			}
		}

		// Calculate viewport start to keep selected line visible with padding
		// Add padding to prevent selected line from being hidden in empty space at edges
		const viewportPadding = 2
		start := 0

		// If selected line is too far down, scroll down to show it with padding
		if selectedLineIdx >= start+height-viewportPadding {
			start = selectedLineIdx - height + viewportPadding + 1
			if start < 0 {
				start = 0
			}
		}

		// If selected line is too far up, scroll up to show it with padding
		if selectedLineIdx < start+viewportPadding {
			start = selectedLineIdx - viewportPadding
			if start < 0 {
				start = 0
			}
		}

		end := start + height
		if end > len(lines) {
			end = len(lines)
		}
		totalLines := len(lines)
		lines = applyViewportWithIndicators(lines, start, end, totalLines)
	}

	return strings.Join(lines, "\n")
}

// renderGroupedTaskLine renders a single task line in grouped view.
func (tt *TaskTree) renderGroupedTaskLine(task types.ResolvedTask, isSelected bool, selectedTasks map[string]bool, showCheckboxes bool) string {
	// Selection marker (always 2 spaces for alignment)
	selMarker := "  "

	// Checkbox indicator (ONLY when multi-select active)
	checkboxPart := ""
	if showCheckboxes {
		checkbox := "[ ]"
		if selectedTasks[task.ID] {
			checkbox = "[x]"
		}
		checkboxPart = checkbox + " "
	}

	// Status indicator with color
	indicator := statusIndicator(task.Status, task.Classification)

	// Title
	title := task.Title

	// Priority suffix
	prioritySuffix := ""
	if task.Priority == "high" {
		prioritySuffix = "!"
	}

	// Apply blue background to ALL parts if selected
	if isSelected {
		selMarker = SelectedRowStyle.Render(selMarker)
		checkboxPart = SelectedRowStyle.Render(checkboxPart)
		indicatorStyled := SelectedRowStyle.Render(indicator)
		title = SelectedRowStyle.Render(title)
		prioritySuffix = SelectedRowStyle.Render(prioritySuffix)
		return fmt.Sprintf("%s%s%s %s%s", selMarker, checkboxPart, indicatorStyled, title, prioritySuffix)
	}

	// Not selected - apply default styling
	indicatorStyled := StatusStyleWithState(task.Status, task.Classification).Render(indicator)

	if selectedTasks[task.ID] {
		// Apply selection style to selected tasks even when not focused
		title = SelectedTaskStyle.Render(title)
	}

	if task.Priority == "high" {
		prioritySuffix = lipgloss.NewStyle().Foreground(ColorPriorityHigh).Bold(true).Render(prioritySuffix)
	}

	return fmt.Sprintf("%s%s%s %s%s", selMarker, checkboxPart, indicatorStyled, title, prioritySuffix)
}

// viewFeatureGrouped renders tasks in feature-grouped view.
// Now includes hybrid status groups: features show only active tasks,
// with separate Draft and Completed sections after features.
func (tt *TaskTree) viewFeatureGrouped(width, height int, activeProjectID string) string {
	if len(tt.featureGroups.Features) == 0 && tt.featureGroups.Ungrouped == nil {
		return DimStyle.Render("  No tasks")
	}

	var lines []string
	showCheckboxes := len(tt.selectedTasks) > 0

	// Compute relation graph for highlighting ancestors/descendants of selected task
	var ancestors, descendants map[string]bool
	if tt.SelectedID != "" && tt.selectedFeatureTaskIdx >= 0 {
		ancestors, descendants = buildSelectedTaskRelationGraph(tt.tasks, tt.SelectedID)
	}

	// Split tasks into active, draft, cancelled, superseded, archived, and completed
	// Active tasks: pending, in_progress, blocked, ready, waiting, active
	// Draft tasks: draft status
	// Cancelled/Superseded/Archived/Completed: separate terminal status sections
	var draftTasks, cancelledTasks, supersededTasks, archivedTasks, completedTasks []types.ResolvedTask
	activeFeatureGroups := make([]FeatureGroup, 0)
	var activeUngrouped *FeatureGroup

	// Process features and split by status
	for _, feature := range tt.featureGroups.Features {
		var activeTasks []types.ResolvedTask
		for _, task := range feature.Tasks {
			switch task.Status {
			case "draft":
				draftTasks = append(draftTasks, task)
			case "cancelled":
				cancelledTasks = append(cancelledTasks, task)
			case "superseded":
				supersededTasks = append(supersededTasks, task)
			case "archived":
				archivedTasks = append(archivedTasks, task)
			case "completed", "validated":
				completedTasks = append(completedTasks, task)
			default:
				// Active statuses: pending, in_progress, blocked, active
				activeTasks = append(activeTasks, task)
			}
		}

		// Only add feature if it has active tasks
		if len(activeTasks) > 0 {
			activeFeature := feature // Copy
			activeFeature.Tasks = activeTasks
			activeFeature.Stats = computeFeatureStats(activeTasks)
			activeFeatureGroups = append(activeFeatureGroups, activeFeature)
		}
	}

	// Process ungrouped tasks
	if tt.featureGroups.Ungrouped != nil {
		var activeUngroupedTasks []types.ResolvedTask
		for _, task := range tt.featureGroups.Ungrouped.Tasks {
			switch task.Status {
			case "draft":
				draftTasks = append(draftTasks, task)
			case "cancelled":
				cancelledTasks = append(cancelledTasks, task)
			case "superseded":
				supersededTasks = append(supersededTasks, task)
			case "archived":
				archivedTasks = append(archivedTasks, task)
			case "completed", "validated":
				completedTasks = append(completedTasks, task)
			default:
				activeUngroupedTasks = append(activeUngroupedTasks, task)
			}
		}

		if len(activeUngroupedTasks) > 0 {
			activeUngrouped = &FeatureGroup{
				ID:    "",
				Name:  "[Ungrouped]",
				Tasks: activeUngroupedTasks,
				Stats: computeFeatureStats(activeUngroupedTasks),
			}
		}
	}

	// Build lookup for original (unfiltered) feature stats so we can show [completed/total complete].
	originalFeatureStats := make(map[string]FeatureStats)
	for _, f := range tt.featureGroups.Features {
		originalFeatureStats[f.ID] = f.Stats
	}

	// Resolve the selected feature ID from the unfiltered list to avoid index mismatch.
	// activeFeatureGroups is filtered (features with no active tasks removed), so its indices
	// don't align with tt.selectedFeatureIdx (which indexes into unfiltered tt.featureGroups.Features).
	selectedFeatureID := ""
	if !tt.isOnUngrouped && tt.selectedFeatureIdx >= 0 && tt.selectedFeatureIdx < len(tt.featureGroups.Features) {
		selectedFeatureID = tt.featureGroups.Features[tt.selectedFeatureIdx].ID
	}

	// Render active features
	for fIdx, feature := range activeFeatureGroups {
		isThisFeature := (feature.ID == selectedFeatureID && selectedFeatureID != "")
		isFeatureSelected := (isThisFeature && tt.selectedFeatureTaskIdx == -1 && !tt.isOnUngrouped)

		// Collapse indicator
		collapseIndicator := "▶"
		if !feature.Collapsed {
			collapseIndicator = "▾"
		}

		// Aggregated status icon from ALL tasks in the original (unfiltered) feature
		var allFeatureTasks []types.ResolvedTask
		for _, origFeature := range tt.featureGroups.Features {
			if origFeature.ID == feature.ID {
				allFeatureTasks = origFeature.Tasks
				break
			}
		}
		statusIcon, hasActiveExec := aggregateFeatureStatusIcon(allFeatureTasks)

		// Active execution indicator
		execIndicator := ""
		if hasActiveExec {
			execIndicator = " ⚡"
		}

		// Stats: [completed/total complete] using original (unfiltered) stats
		origStats := originalFeatureStats[feature.ID]
		statsStr := fmt.Sprintf("[%d/%d complete]", origStats.Completed, origStats.Total)

		// Feature header with status icon, name, execution indicator, and completion stats
		featureHeader := fmt.Sprintf("%s %s Feature: %s%s %s",
			collapseIndicator, statusIcon, feature.Name, execIndicator, statsStr)

		// Selection marker (2 spaces for alignment, → when selected)
		selMarker := "  "
		if isFeatureSelected {
			selMarker = "→ "
		}

		// Apply blue background if selected, otherwise muted blue text (no background)
		if isFeatureSelected {
			// Blue background for selected feature header
			featureHeader = SelectedRowStyle.Render(featureHeader)
			selMarker = SelectedRowStyle.Render(selMarker)
			featureHeader = fmt.Sprintf("%s%s", selMarker, featureHeader)
		} else {
			// Muted blue text for unselected feature headers (matches TypeScript)
			featureHeader = FeatureHeaderStyle.Render(featureHeader)
			featureHeader = fmt.Sprintf("%s%s", selMarker, featureHeader)
		}

		lines = append(lines, featureHeader)

		// Render tasks if not collapsed
		if !feature.Collapsed {
			// Build dependency tree for this feature
			tree := BuildTree(feature.Tasks, tt.tasks)

			// Render tree with proper indentation
			// Pass fIdx as both groupIdx and selectedGroupIdx when this is the selected feature,
			// so the internal groupIdx == selectedGroupIdx check in renderGroupTaskTree passes.
			// When it's not the selected feature, pass -1 as selectedGroupIdx to prevent matching.
			selectedGroupForRender := -1
			if isThisFeature {
				selectedGroupForRender = fIdx
			}
			visualIndex := 0
			tt.renderGroupTaskTree(
				tree,
				"    ", // base indentation for grouped view
				&lines,
				width,
				fIdx,
				&visualIndex,
				tt.selectedTasks,
				showCheckboxes,
				activeProjectID,
				selectedGroupForRender,
				ancestors,
				descendants,
			)
		}
	}

	// Render active ungrouped if present
	if activeUngrouped != nil {
		ungrouped := activeUngrouped
		isUngroupedSelected := (tt.isOnUngrouped && tt.selectedFeatureTaskIdx == -1)

		collapseIndicator := "▶"
		if !ungrouped.Collapsed {
			collapseIndicator = "▾"
		}

		ungroupedHeader := fmt.Sprintf("%s %s (%d)", collapseIndicator, ungrouped.Name, len(ungrouped.Tasks))

		// Selection marker (2 spaces for alignment, → when selected)
		selMarker := "  "
		if isUngroupedSelected {
			selMarker = "→ "
		}

		// Apply blue background if selected, otherwise dark blue background
		if isUngroupedSelected {
			ungroupedHeader = SelectedRowStyle.Render(ungroupedHeader)
			selMarker = SelectedRowStyle.Render(selMarker)
			ungroupedHeader = fmt.Sprintf("%s%s", selMarker, ungroupedHeader)
		} else {
			ungroupedHeader = GroupHeaderStyle.Render(ungroupedHeader)
			ungroupedHeader = fmt.Sprintf("%s%s", selMarker, ungroupedHeader)
		}

		lines = append(lines, ungroupedHeader)

		if !ungrouped.Collapsed {
			// Build dependency tree for ungrouped tasks
			tree := BuildTree(ungrouped.Tasks, tt.tasks)

			// Use feature count as group index for ungrouped (to calculate selection correctly)
			ungroupedIdx := len(activeFeatureGroups)
			visualIndex := 0
			tt.renderGroupTaskTree(
				tree,
				"    ", // base indentation for grouped view
				&lines,
				width,
				ungroupedIdx,
				&visualIndex,
				tt.selectedTasks,
				showCheckboxes,
				activeProjectID,
				tt.selectedFeatureIdx, // Feature view: ungrouped uses selectedFeatureIdx
				ancestors,
				descendants,
			)
		}
	}

	// Cache terminal section task existence for navigation
	tt.hasDraftTasks = len(draftTasks) > 0
	tt.hasCancelledTasks = len(cancelledTasks) > 0
	tt.hasSupersededTasks = len(supersededTasks) > 0
	tt.hasArchivedTasks = len(archivedTasks) > 0
	tt.hasCompletedTasks = len(completedTasks) > 0

	// Render terminal status sections in order: Draft → Cancelled → Superseded → Archived → Completed
	type terminalSection struct {
		label       string
		tasks       []types.ResolvedTask
		style       lipgloss.Style
		isOn        func() bool
		collapsed   *bool
		featureIdx  func() int
		taskIdx     func() int
		setFeatIDs  func([]string)
		featureIDs  func() []string
		collapseKey string
	}

	terminalSections := []terminalSection{
		{"Draft", draftTasks, DraftHeaderStyle, func() bool { return tt.isOnDraftSection }, &tt.draftCollapsed, func() int { return tt.draftFeatureIdx }, func() int { return tt.draftTaskIdx }, func(ids []string) { tt.draftFeatureIDs = ids }, func() []string { return tt.draftFeatureIDs }, "draft"},
		{"Cancelled", cancelledTasks, CancelledHeaderStyle, func() bool { return tt.isOnCancelledSection }, &tt.cancelledCollapsed, func() int { return tt.cancelledFeatureIdx }, func() int { return tt.cancelledTaskIdx }, func(ids []string) { tt.cancelledFeatureIDs = ids }, func() []string { return tt.cancelledFeatureIDs }, "cancelled"},
		{"Superseded", supersededTasks, SupersededHeaderStyle, func() bool { return tt.isOnSupersededSection }, &tt.supersededCollapsed, func() int { return tt.supersededFeatureIdx }, func() int { return tt.supersededTaskIdx }, func(ids []string) { tt.supersededFeatureIDs = ids }, func() []string { return tt.supersededFeatureIDs }, "superseded"},
		{"Archived", archivedTasks, ArchivedHeaderStyle, func() bool { return tt.isOnArchivedSection }, &tt.archivedCollapsed, func() int { return tt.archivedFeatureIdx }, func() int { return tt.archivedTaskIdx }, func(ids []string) { tt.archivedFeatureIDs = ids }, func() []string { return tt.archivedFeatureIDs }, "archived"},
		{"Completed", completedTasks, CompletedHeaderStyle, func() bool { return tt.isOnCompletedSection }, &tt.completedCollapsed, func() int { return tt.completedFeatureIdx }, func() int { return tt.completedTaskIdx }, func(ids []string) { tt.completedFeatureIDs = ids }, func() []string { return tt.completedFeatureIDs }, "completed"},
	}

	for _, sec := range terminalSections {
		if len(sec.tasks) == 0 {
			continue
		}

		// Add blank line before section
		lines = append(lines, "")

		collapseIndicator := "▶"
		if !*sec.collapsed {
			collapseIndicator = "▾"
		}

		sectionHeader := fmt.Sprintf("%s %s (%d)", collapseIndicator, sec.label, len(sec.tasks))
		if sec.isOn() && sec.featureIdx() == -1 {
			// Selected section header — show → arrow
			sectionHeader = SelectedRowStyle.Render(sectionHeader)
			sectionHeader = fmt.Sprintf("%s%s", SelectedRowStyle.Render("→ "), sectionHeader)
		} else {
			sectionHeader = sec.style.Render(sectionHeader)
			sectionHeader = fmt.Sprintf("  %s", sectionHeader)
		}
		lines = append(lines, sectionHeader)

		if !*sec.collapsed {
			// Group tasks by feature_id
			byFeature := make(map[string][]types.ResolvedTask)
			for _, task := range sec.tasks {
				featureID := task.FeatureID
				if featureID == "" {
					featureID = "[Ungrouped]"
				}
				byFeature[featureID] = append(byFeature[featureID], task)
			}

			// Sort feature IDs for consistent ordering
			var featureIDs []string
			for fid := range byFeature {
				featureIDs = append(featureIDs, fid)
			}
			sort.Strings(featureIDs)

			// Cache feature IDs for navigation
			sec.setFeatIDs(featureIDs)

			// Render each feature group
			for fIdx, featureID := range featureIDs {
				featureTasks := byFeature[featureID]
				isCollapsed := tt.featureCollapsed[sec.collapseKey+":"+featureID]

				// Render sub-feature header with collapse arrow
				collapseIcon := "▾"
				if isCollapsed {
					collapseIcon = "▶"
				}
				statusIcon, _ := aggregateFeatureStatusIcon(featureTasks)
				featureHeader := fmt.Sprintf("  %s %s Feature: %s [%d]", collapseIcon, statusIcon, featureID, len(featureTasks))

				// Highlight if this sub-feature header is selected (not when navigated into its tasks)
				if sec.isOn() && sec.featureIdx() == fIdx && sec.taskIdx() == -1 {
					featureHeader = SelectedRowStyle.Render(featureHeader)
					featureHeader = fmt.Sprintf("%s%s", SelectedRowStyle.Render("→ "), featureHeader)
				} else {
					featureHeader = DimStyle.Render(featureHeader)
				}
				lines = append(lines, featureHeader)

				// Build dependency tree for this feature's tasks (skip if collapsed)
				if !isCollapsed {
					tree := BuildTree(featureTasks, tt.tasks)
					visualIndex := 0
					tt.renderGroupTaskTree(
						tree,
						"    ",
						&lines,
						width,
						-1, // No feature index for status groups
						&visualIndex,
						tt.selectedTasks,
						showCheckboxes,
						activeProjectID,
						-1, // No feature selection for status groups
						ancestors,
						descendants,
					)
				}
			}
		}
	}

	// Handle viewport scrolling (ensure selected item is visible).
	// IMPORTANT: iterate over activeFeatureGroups (the same filtered list used for rendering)
	// and match by feature ID, not index — tt.selectedFeatureIdx indexes into the unfiltered
	// tt.featureGroups.Features, so direct index comparison against activeFeatureGroups would desync.
	if height > 0 && len(lines) > height {
		// Build terminal section descriptors for line-finding
		termSections := []terminalSectionLineInfo{
			{tasks: draftTasks, isOn: tt.isOnDraftSection, collapsed: tt.draftCollapsed, featureIdx: tt.draftFeatureIdx, taskIdx: tt.draftTaskIdx, featureIDs: tt.draftFeatureIDs, sectionName: "draft", featureCollapsed: tt.featureCollapsed},
			{tasks: cancelledTasks, isOn: tt.isOnCancelledSection, collapsed: tt.cancelledCollapsed, featureIdx: tt.cancelledFeatureIdx, taskIdx: tt.cancelledTaskIdx, featureIDs: tt.cancelledFeatureIDs, sectionName: "cancelled", featureCollapsed: tt.featureCollapsed},
			{tasks: supersededTasks, isOn: tt.isOnSupersededSection, collapsed: tt.supersededCollapsed, featureIdx: tt.supersededFeatureIdx, taskIdx: tt.supersededTaskIdx, featureIDs: tt.supersededFeatureIDs, sectionName: "superseded", featureCollapsed: tt.featureCollapsed},
			{tasks: archivedTasks, isOn: tt.isOnArchivedSection, collapsed: tt.archivedCollapsed, featureIdx: tt.archivedFeatureIdx, taskIdx: tt.archivedTaskIdx, featureIDs: tt.archivedFeatureIDs, sectionName: "archived", featureCollapsed: tt.featureCollapsed},
			{tasks: completedTasks, isOn: tt.isOnCompletedSection, collapsed: tt.completedCollapsed, featureIdx: tt.completedFeatureIdx, taskIdx: tt.completedTaskIdx, featureIDs: tt.completedFeatureIDs, sectionName: "completed", featureCollapsed: tt.featureCollapsed},
		}
		selectedLineIdx := findSelectedLineInFeatureView(
			activeFeatureGroups,
			activeUngrouped,
			tt.tasks,
			selectedFeatureID,
			tt.selectedFeatureTaskIdx,
			tt.isOnUngrouped,
			tt.isOnAnyTerminalSection(),
			termSections,
		)

		// Calculate viewport start to keep selected line visible with padding
		// Add padding to prevent selected line from being hidden in empty space at edges
		const viewportPadding = 2
		start := 0

		// If selected line is too far down, scroll down to show it with padding
		if selectedLineIdx >= start+height-viewportPadding {
			start = selectedLineIdx - height + viewportPadding + 1
			if start < 0 {
				start = 0
			}
		}

		// If selected line is too far up, scroll up to show it with padding
		if selectedLineIdx < start+viewportPadding {
			start = selectedLineIdx - viewportPadding
			if start < 0 {
				start = 0
			}
		}

		end := start + height
		if end > len(lines) {
			end = len(lines)
		}
		totalLines := len(lines)
		lines = applyViewportWithIndicators(lines, start, end, totalLines)
	}

	return strings.Join(lines, "\n")
}

// terminalSectionLineInfo holds the data needed to compute line indices for a terminal section.
type terminalSectionLineInfo struct {
	tasks            []types.ResolvedTask
	isOn             bool
	collapsed        bool
	featureIdx       int
	taskIdx          int
	featureIDs       []string
	sectionName      string
	featureCollapsed map[string]bool
}

// findSelectedLineInFeatureView computes the rendered line index of the currently
// selected item using the same filtered data that viewFeatureGrouped uses for rendering.
// This prevents desync between scroll targeting and actual rendered row order.
func findSelectedLineInFeatureView(
	activeFeatureGroups []FeatureGroup,
	activeUngrouped *FeatureGroup,
	allTasks []types.ResolvedTask,
	selectedFeatureID string,
	selectedFeatureTaskIdx int,
	isOnUngrouped bool,
	isOnAnyTerminal bool,
	termSections []terminalSectionLineInfo,
) int {
	lineIdx := 0

	// Walk active feature groups (matches rendering order)
	for _, feature := range activeFeatureGroups {
		isThisFeature := (feature.ID == selectedFeatureID && selectedFeatureID != "")

		// Check if feature header is selected
		if isThisFeature && selectedFeatureTaskIdx == -1 && !isOnUngrouped && !isOnAnyTerminal {
			return lineIdx
		}
		lineIdx++ // feature header line

		// Count tasks in this feature if not collapsed
		if !feature.Collapsed {
			taskLineCount := countGroupTaskLines(BuildTree(feature.Tasks, allTasks))

			// If selected task is in this feature
			if isThisFeature && selectedFeatureTaskIdx >= 0 && !isOnUngrouped && !isOnAnyTerminal {
				return lineIdx + selectedFeatureTaskIdx
			}
			lineIdx += taskLineCount
		}
	}

	// Walk active ungrouped section
	if activeUngrouped != nil {
		// Check if ungrouped header is selected
		if isOnUngrouped && selectedFeatureTaskIdx == -1 && !isOnAnyTerminal {
			return lineIdx
		}
		lineIdx++ // ungrouped header line

		if !activeUngrouped.Collapsed {
			taskLineCount := countGroupTaskLines(BuildTree(activeUngrouped.Tasks, allTasks))
			if isOnUngrouped && selectedFeatureTaskIdx >= 0 && !isOnAnyTerminal {
				return lineIdx + selectedFeatureTaskIdx
			}
			lineIdx += taskLineCount
		}
	}

	// Walk terminal sections (Draft → Cancelled → Superseded → Archived → Completed)
	for _, sec := range termSections {
		if len(sec.tasks) == 0 {
			continue
		}

		lineIdx++ // blank line before section
		// Section header
		if sec.isOn && sec.featureIdx == -1 {
			return lineIdx
		}
		lineIdx++ // section header line

		if !sec.collapsed {
			// Group tasks by feature (mirrors rendering)
			byFeature := make(map[string][]types.ResolvedTask)
			for _, task := range sec.tasks {
				fid := task.FeatureID
				if fid == "" {
					fid = "[Ungrouped]"
				}
				byFeature[fid] = append(byFeature[fid], task)
			}

			for fIdx, featureID := range sec.featureIDs {
				featureTasks := byFeature[featureID]
				isSubCollapsed := false
				if sec.featureCollapsed != nil {
					isSubCollapsed = sec.featureCollapsed[sec.sectionName+":"+featureID]
				}

				// Sub-feature header
				if sec.isOn && sec.featureIdx == fIdx && sec.taskIdx == -1 {
					return lineIdx
				}
				lineIdx++ // sub-feature header

				// Count task lines if sub-feature is expanded
				if !isSubCollapsed {
					taskLineCount := countGroupTaskLines(BuildTree(featureTasks, allTasks))

					// If on a task within this sub-feature
					if sec.isOn && sec.featureIdx == fIdx && sec.taskIdx >= 0 {
						return lineIdx + sec.taskIdx
					}
					lineIdx += taskLineCount
				}
			}
		}
	}

	// Fallback: if nothing matched, return 0 (top of viewport)
	return 0
}

// viewNestedGrouped renders tasks in 3-level nested hierarchy: Status → Feature → Tasks
// This is Phase 4 of nested grouping implementation.
func (tt *TaskTree) viewNestedGrouped(width, height int, activeProjectID string) string {
	if len(tt.statusGroups) == 0 {
		return DimStyle.Render("  No tasks")
	}

	var lines []string
	showCheckboxes := len(tt.selectedTasks) > 0

	// Compute relation graph for highlighting ancestors/descendants of selected task
	var ancestors, descendants map[string]bool
	if tt.SelectedID != "" && tt.selectedTaskIdx >= 0 {
		ancestors, descendants = buildSelectedTaskRelationGraph(tt.tasks, tt.SelectedID)
	}

	// Iterate through status groups (Level 1)
	for sIdx, statusGroup := range tt.statusGroups {
		// Add blank line before each status section (except the first one)
		if sIdx > 0 {
			lines = append(lines, "")
		}

		// Render status header (Level 1)
		isStatusSelected := (sIdx == tt.selectedStatusIdx && tt.isOnStatusHeader)

		// Collapse indicator for status
		collapseIndicator := "▶"
		if !statusGroup.Collapsed {
			collapseIndicator = "▾"
		}

		statusHeader := fmt.Sprintf("%s %s (%d)", collapseIndicator, statusGroup.Name, statusGroup.Count)

		// Selection marker for status header
		if isStatusSelected {
			statusHeader = GroupHeaderStyle.Render(statusHeader)
			statusHeader = fmt.Sprintf("→ %s", statusHeader)
		} else {
			statusHeader = GroupHeaderStyle.Render(statusHeader)
			statusHeader = fmt.Sprintf("  %s", statusHeader)
		}

		lines = append(lines, statusHeader)

		// Render features if status is expanded (Level 2)
		if !statusGroup.Collapsed {
			// Render features
			for fIdx, feature := range statusGroup.Features {
				isFeatureSelected := (sIdx == tt.selectedStatusIdx && fIdx == tt.selectedFeatureIdx && !tt.isOnStatusHeader && tt.selectedTaskIdx == -1)

				// Collapse indicator for feature
				featureCollapseIndicator := "▶"
				if !feature.Collapsed {
					featureCollapseIndicator = "▾"
				}

				// Aggregated status icon and execution indicator
				nestedStatusIcon, nestedHasActive := aggregateFeatureStatusIcon(feature.Tasks)
				nestedExecIndicator := ""
				if nestedHasActive {
					nestedExecIndicator = " ⚡"
				}
				nestedStatsStr := fmt.Sprintf("[%d/%d complete]", feature.Stats.Completed, feature.Stats.Total)

				// Feature header with status icon, name, execution indicator, and completion stats
				featureHeader := fmt.Sprintf("%s %s Feature: %s%s %s",
					featureCollapseIndicator, nestedStatusIcon, feature.Name, nestedExecIndicator, nestedStatsStr)

				// Selection marker for feature header (with indentation)
				if isFeatureSelected {
					featureHeader = FeatureHeaderStyle.Render(featureHeader)
					featureHeader = fmt.Sprintf("  → %s", featureHeader)
				} else {
					featureHeader = FeatureHeaderStyle.Render(featureHeader)
					featureHeader = fmt.Sprintf("    %s", featureHeader)
				}

				lines = append(lines, featureHeader)

				// Render tasks if feature is expanded (Level 3)
				if !feature.Collapsed {
					tree := BuildTree(feature.Tasks, tt.tasks)
					visualIndex := 0
					tt.renderGroupTaskTree(
						tree,
						"      ", // base indentation for nested view (3 levels deep)
						&lines,
						width,
						fIdx,
						&visualIndex,
						tt.selectedTasks,
						showCheckboxes,
						activeProjectID,
						tt.selectedFeatureIdx, // NESTED VIEW FIX: use selectedFeatureIdx not selectedGroupIdx
						ancestors,
						descendants,
					)
				}
			}

			// Render ungrouped if present
			if statusGroup.Ungrouped != nil {
				ungrouped := statusGroup.Ungrouped
				isUngroupedSelected := (sIdx == tt.selectedStatusIdx && tt.selectedFeatureIdx == -1 && !tt.isOnStatusHeader && tt.selectedTaskIdx == -1)

				// Collapse indicator for ungrouped
				ungroupedCollapseIndicator := "▶"
				if !ungrouped.Collapsed {
					ungroupedCollapseIndicator = "▾"
				}

				ungroupedHeader := fmt.Sprintf("%s %s (%d)", ungroupedCollapseIndicator, ungrouped.Name, len(ungrouped.Tasks))

				// Selection marker for ungrouped header
				if isUngroupedSelected {
					ungroupedHeader = GroupHeaderStyle.Render(ungroupedHeader)
					ungroupedHeader = fmt.Sprintf("  → %s", ungroupedHeader)
				} else {
					ungroupedHeader = GroupHeaderStyle.Render(ungroupedHeader)
					ungroupedHeader = fmt.Sprintf("    %s", ungroupedHeader)
				}

				lines = append(lines, ungroupedHeader)

				// Render ungrouped tasks if expanded
				if !ungrouped.Collapsed {
					tree := BuildTree(ungrouped.Tasks, tt.tasks)
					visualIndex := 0
					ungroupedIdx := len(statusGroup.Features) // Use feature count as group index
					tt.renderGroupTaskTree(
						tree,
						"      ",
						&lines,
						width,
						ungroupedIdx,
						&visualIndex,
						tt.selectedTasks,
						showCheckboxes,
						activeProjectID,
						tt.selectedFeatureIdx, // Nested view ungrouped: use selectedFeatureIdx
						ancestors,
						descendants,
					)
				}
			}
		}
	}

	// Handle viewport scrolling (ensure selected item is visible)
	if height > 0 && len(lines) > height {
		// Find the line index of the selected item
		selectedLineIdx := 0
		lineIdx := 0

		// Iterate through status groups to find selected line
		for sIdx, statusGroup := range tt.statusGroups {
			// Check if status header is selected
			if sIdx == tt.selectedStatusIdx && tt.isOnStatusHeader {
				selectedLineIdx = lineIdx
				break
			}
			lineIdx++ // status header line

			// Check features and tasks if status is expanded
			if !statusGroup.Collapsed {
				// Check features
				for fIdx, feature := range statusGroup.Features {
					// Check if feature header is selected
					if sIdx == tt.selectedStatusIdx && fIdx == tt.selectedFeatureIdx && !tt.isOnStatusHeader && tt.selectedTaskIdx == -1 {
						selectedLineIdx = lineIdx
						break
					}
					lineIdx++ // feature header line

					// Check tasks if feature is expanded
					if !feature.Collapsed {
						taskLineCount := countGroupTaskLines(BuildTree(feature.Tasks, tt.tasks))

						// If selected task is in this feature
						if sIdx == tt.selectedStatusIdx && fIdx == tt.selectedFeatureIdx && !tt.isOnStatusHeader && tt.selectedTaskIdx >= 0 {
							selectedLineIdx = lineIdx + tt.selectedTaskIdx
							break
						}
						lineIdx += taskLineCount
					}
				}

				// Check ungrouped if present
				if statusGroup.Ungrouped != nil {
					ungrouped := statusGroup.Ungrouped

					// Check if ungrouped header is selected
					if sIdx == tt.selectedStatusIdx && tt.selectedFeatureIdx == -1 && !tt.isOnStatusHeader && tt.selectedTaskIdx == -1 {
						selectedLineIdx = lineIdx
						break
					}
					lineIdx++ // ungrouped header line

					// Check ungrouped tasks if expanded
					if !ungrouped.Collapsed {
						taskLineCount := countGroupTaskLines(BuildTree(ungrouped.Tasks, tt.tasks))

						// If selected task is in ungrouped section
						if sIdx == tt.selectedStatusIdx && tt.selectedFeatureIdx == -1 && !tt.isOnStatusHeader && tt.selectedTaskIdx >= 0 {
							selectedLineIdx = lineIdx + tt.selectedTaskIdx
							break
						}
						lineIdx += taskLineCount
					}
				}
			}
		}

		// Calculate viewport start to keep selected line visible with padding
		// Add padding to prevent selected line from being hidden in empty space at edges
		const viewportPadding = 2
		start := 0

		// If selected line is too far down, scroll down to show it with padding
		if selectedLineIdx >= start+height-viewportPadding {
			start = selectedLineIdx - height + viewportPadding + 1
			if start < 0 {
				start = 0
			}
		}

		// If selected line is too far up, scroll up to show it with padding
		if selectedLineIdx < start+viewportPadding {
			start = selectedLineIdx - viewportPadding
			if start < 0 {
				start = 0
			}
		}

		end := start + height
		if end > len(lines) {
			end = len(lines)
		}
		totalLines := len(lines)
		lines = applyViewportWithIndicators(lines, start, end, totalLines)
	}

	return strings.Join(lines, "\n")
}

// truncateTitle truncates a plain-text title to fit within maxWidth characters,
// appending "…" if truncation occurs. Must be called BEFORE applying lipgloss styles
// to avoid cutting ANSI escape sequences.
func truncateTitle(title string, maxWidth int) string {
	if maxWidth <= 0 {
		return title
	}
	runes := []rune(title)
	if len(runes) <= maxWidth {
		return title
	}
	if maxWidth <= 3 {
		return string(runes[:maxWidth])
	}
	return string(runes[:maxWidth-1]) + "…"
}

// renderNodes recursively renders tree nodes into lines with proper box-drawing indentation.
// The prefix parameter tracks ancestor line states to render vertical continuation lines correctly.
func (tt *TaskTree) renderNodes(nodes []TreeNode, prefix string, lines *[]string, width int, showCheckboxes bool) {
	for i, node := range nodes {
		isLast := i == len(nodes)-1

		// Build the line with tree prefix
		line := tt.renderTaskLine(node, prefix, isLast, width, showCheckboxes)
		*lines = append(*lines, line)

		// Render children with updated prefix for vertical continuation
		if len(node.Children) > 0 {
			childPrefix := tt.calculateChildPrefix(prefix, isLast)
			tt.renderNodes(node.Children, childPrefix, lines, width, showCheckboxes)
		}
	}
}

// calculateChildPrefix determines the prefix to pass to child nodes.
// It maintains vertical continuation lines (│) for non-last siblings
// and empty space for last siblings (whose branch has ended).
func (tt *TaskTree) calculateChildPrefix(prefix string, isLast bool) string {
	// Add vertical line continuation or empty space
	if isLast {
		// Last sibling: branch ended, use empty space
		return prefix + treeEmpty
	}
	// Non-last sibling: continue vertical line
	return prefix + treeVertical
}

// renderTaskLine renders a single task line with status, title, indicators, and tree connectors.
// The prefix parameter contains ancestor line state, and isLast determines the branch character.
// The width parameter is used for truncation when TextWrap is false.
func (tt *TaskTree) renderTaskLine(node TreeNode, prefix string, isLast bool, width int, showCheckboxes bool) string {
	task := node.Task
	isSelected := task.ID == tt.SelectedID

	// Calculate tree connector based on depth and position
	var treeConnector string
	if prefix == "" {
		// Root level: no connector
		treeConnector = ""
	} else if isLast {
		// Last child: use └─
		treeConnector = treeLastBranch + " "
	} else {
		// Non-last child: use ├─
		treeConnector = treeBranch + " "
	}

	// Status indicator with color
	indicator := statusIndicator(task.Status, task.Classification)
	indicatorStyled := StatusStyleWithState(task.Status, task.Classification).Render(indicator)

	// Checkbox indicator (ONLY when multi-select active)
	checkboxPart := ""
	if showCheckboxes {
		checkbox := "[ ]"
		if tt.selectedTasks[task.ID] {
			checkbox = "[x]"
		}
		checkboxPart = checkbox + " "
	}

	// Title — truncate BEFORE styling to avoid cutting ANSI sequences
	title := task.Title
	if !tt.TextWrap && width > 0 {
		// Calculate overhead: selMarker(2) + prefix + treeConnector + checkbox + indicator(2) + space(1) + suffixes
		overhead := 2 + lipgloss.Width(prefix) + lipgloss.Width(treeConnector)
		if showCheckboxes {
			overhead += 4 // "[x] "
		}
		overhead += 2 + 1 // indicator + space
		if task.Priority == "high" {
			overhead++ // "!"
		}
		if node.InCycle {
			overhead += 2 // " ↺"
		}
		availableWidth := width - overhead
		title = truncateTitle(title, availableWidth)
	}

	// Priority suffix
	prioritySuffix := ""
	if task.Priority == "high" {
		prioritySuffix = "!"
	}

	// Cycle indicator
	cycleSuffix := ""
	if node.InCycle {
		cycleSuffix = " ↺"
	}

	// Selection marker (always 2 spaces for alignment)
	selMarker := "  "

	// Build the complete line first to preserve exact spacing
	if isSelected {
		// Build line without styles first
		rawLine := fmt.Sprintf("%s%s%s%s%s %s%s%s", selMarker, prefix, treeConnector, checkboxPart, indicator, title, prioritySuffix, cycleSuffix)

		// Apply blue background to the entire line at once
		return SelectedRowStyle.Render(rawLine)
	} else {
		// Apply default styling when not selected
		if task.Priority == "high" {
			prioritySuffix = lipgloss.NewStyle().Foreground(ColorPriorityHigh).Bold(true).Render(prioritySuffix)
		}
		if node.InCycle {
			cycleSuffix = lipgloss.NewStyle().Foreground(ColorMagenta).Render(cycleSuffix)
		}

		return fmt.Sprintf("%s%s%s%s%s %s%s%s", selMarker, prefix, treeConnector, checkboxPart, indicatorStyled, title, prioritySuffix, cycleSuffix)
	}
}

// renderGroupTaskTree recursively renders tree nodes for grouped view with proper indentation.
// The prefix parameter tracks ancestor line states, visualIndex tracks position for selection.
// selectedGroupIdx parameter is the group index to compare against (pass tt.selectedGroupIdx for 2-level view,
// tt.selectedFeatureIdx for nested view to fix navigation highlighting bug).
func (tt *TaskTree) renderGroupTaskTree(
	nodes []TreeNode,
	prefix string,
	lines *[]string,
	width int,
	groupIdx int,
	visualIndex *int,
	selectedTasks map[string]bool,
	showCheckboxes bool,
	activeProjectID string,
	selectedGroupIdx int,
	ancestors map[string]bool,
	descendants map[string]bool,
) {

	for i, node := range nodes {
		isLast := i == len(nodes)-1

		// Check if this task is selected
		// Works across all views (2-level, feature-only, nested) by matching on SelectedID directly.
		// SelectedID is only set to a task ID when navigated to a task (not a header/group).
		isSelected := (node.Task.ID == tt.SelectedID && tt.SelectedID != "")

		// DEBUG: Log comparison for each task		*visualIndex++

		// Build the line with tree prefix
		line := tt.renderGroupedTaskLineWithTree(
			node,
			prefix,
			isLast,
			isSelected,
			selectedTasks,
			showCheckboxes,
			activeProjectID,
			width,
			ancestors,
			descendants,
		)
		*lines = append(*lines, line)

		// Render children with updated prefix for vertical continuation
		if len(node.Children) > 0 {
			childPrefix := tt.calculateChildPrefix(prefix, isLast)
			tt.renderGroupTaskTree(
				node.Children,
				childPrefix,
				lines,
				width,
				groupIdx,
				visualIndex,
				selectedTasks,
				showCheckboxes,
				activeProjectID,
				selectedGroupIdx,
				ancestors,
				descendants,
			)
		}
	}
}

// countGroupTaskLines counts the total number of lines (including nested children) in a tree.
func countGroupTaskLines(nodes []TreeNode) int {
	count := 0
	for _, node := range nodes {
		count++ // Count this node
		if len(node.Children) > 0 {
			count += countGroupTaskLines(node.Children) // Count children recursively
		}
	}
	return count
}

// flattenTreeToVisualOrder flattens a tree into a list of task IDs in visual (depth-first) order.
// This matches the order tasks appear when rendered in the TUI.
func flattenTreeToVisualOrder(nodes []TreeNode) []string {
	var result []string
	for _, node := range nodes {
		result = append(result, node.Task.ID)
		if len(node.Children) > 0 {
			result = append(result, flattenTreeToVisualOrder(node.Children)...)
		}
	}
	return result
}

// renderGroupedTaskLineWithTree renders a single task line in grouped view with tree connectors.
// The prefix parameter contains ancestor line state, isLast determines the branch character.
// ancestors and descendants are optional maps from buildSelectedTaskRelationGraph for
// relation highlighting (cyan for ancestors, magenta for descendants).
func (tt *TaskTree) renderGroupedTaskLineWithTree(
	node TreeNode,
	prefix string,
	isLast bool,
	isSelected bool,
	selectedTasks map[string]bool,
	showCheckboxes bool,
	activeProjectID string,
	width int,
	ancestors map[string]bool,
	descendants map[string]bool,
) string {
	task := node.Task

	// Calculate tree connector
	var treeConnector string
	if prefix == "" {
		treeConnector = ""
	} else if isLast {
		treeConnector = treeLastBranch + " "
	} else {
		treeConnector = treeBranch + " "
	}

	// Determine relation highlight color for this task's tree connectors
	var relationColor lipgloss.Color
	isAncestor := ancestors[task.ID]
	isDescendant := descendants[task.ID]
	if isAncestor {
		relationColor = ColorCyan
	} else if isDescendant {
		relationColor = ColorMagenta
	}

	// Selection marker
	selMarker := "  "
	if isSelected {
		selMarker = "→ "
	}

	// Checkbox indicator (ONLY when multi-select active)
	checkboxPart := ""
	if showCheckboxes {
		checkbox := "[ ]"
		if selectedTasks[task.ID] {
			checkbox = "[x]"
		}
		checkboxPart = checkbox + " "
	}

	// Status indicator with color
	indicator := statusIndicator(task.Status, task.Classification)

	// Title — truncate BEFORE styling to avoid cutting ANSI sequences
	title := task.Title
	if !tt.TextWrap && width > 0 {
		// Calculate overhead: selMarker + prefix + treeConnector + checkbox + indicator + space + suffixes
		overhead := lipgloss.Width(selMarker) + lipgloss.Width(prefix) + lipgloss.Width(treeConnector)
		if showCheckboxes {
			overhead += 4 // "[x] "
		}
		overhead += 2 + 1 // indicator + space
		if task.Priority == "high" {
			overhead++ // "!"
		}
		if node.InCycle {
			overhead += 2 // " ↺"
		}
		availableWidth := width - overhead
		title = truncateTitle(title, availableWidth)
	}

	// Project label (only in aggregate view)
	projectLabel := ""
	if activeProjectID == "all" && task.ProjectID != "" {
		projectLabel = fmt.Sprintf("[%s] ", task.ProjectID)
	}

	// Priority suffix
	prioritySuffix := ""
	if task.Priority == "high" {
		prioritySuffix = "!"
	}

	// Cycle suffix
	cycleSuffix := ""
	if node.InCycle {
		cycleSuffix = " ↺"
	}

	// Apply blue background to ALL parts if selected
	if isSelected {
		selMarker = SelectedRowStyle.Render(selMarker)
		prefix = SelectedRowStyle.Render(prefix)
		treeConnector = SelectedRowStyle.Render(treeConnector)
		checkboxPart = SelectedRowStyle.Render(checkboxPart)
		indicatorStyled := SelectedRowStyle.Render(indicator)
		projectLabel = SelectedRowStyle.Render(projectLabel)
		title = SelectedRowStyle.Render(title)
		prioritySuffix = SelectedRowStyle.Render(prioritySuffix)
		cycleSuffix = SelectedRowStyle.Render(cycleSuffix)
		return fmt.Sprintf("%s%s%s%s%s %s%s%s%s",
			selMarker, prefix, treeConnector, checkboxPart,
			indicatorStyled, projectLabel, title, prioritySuffix, cycleSuffix)
	}

	// Not selected - apply default styling, with optional relation highlighting
	// Color tree connectors cyan (ancestor) or magenta (descendant)
	if isAncestor || isDescendant {
		relationStyle := lipgloss.NewStyle().Foreground(relationColor)
		prefix = relationStyle.Render(prefix)
		treeConnector = relationStyle.Render(treeConnector)
	}

	indicatorStyled := StatusStyleWithState(task.Status, task.Classification).Render(indicator)
	if projectLabel != "" {
		projectLabel = lipgloss.NewStyle().Foreground(ColorMagenta).Render(projectLabel)
	}
	title = DimStyle.Render(title)
	if task.Priority == "high" {
		prioritySuffix = lipgloss.NewStyle().Foreground(ColorPriorityHigh).Bold(true).Render(prioritySuffix)
	}
	if cycleSuffix != "" {
		cycleSuffix = lipgloss.NewStyle().Foreground(ColorMagenta).Render(cycleSuffix)
	}

	return fmt.Sprintf("%s%s%s%s%s %s%s%s%s",
		selMarker, prefix, treeConnector, checkboxPart,
		indicatorStyled, projectLabel, title, prioritySuffix, cycleSuffix)
}

// SetLaneViewMode enables or disables lane-based git-graph rendering.
// When enabled, tasks are displayed in a git-style graph with lanes showing dependencies.
func (tt *TaskTree) SetLaneViewMode(enabled bool) {
	tt.useLaneView = enabled
}

// viewLaneTree renders tasks in lane-based git-graph view with box-drawing connectors.
func (tt *TaskTree) viewLaneTree(width, height int) string {
	if len(tt.laneTasks) == 0 {
		return DimStyle.Render("  No tasks")
	}

	var lines []string

	for i, assignment := range tt.laneAssignments {
		task := tt.laneTasks[i]
		isSelected := task.ID == tt.SelectedID
		line := tt.renderLaneTaskLine(task, assignment, i, isSelected, width)
		lines = append(lines, line)
	}

	// Handle viewport scrolling (ensure selected task visible)
	if height > 0 && len(lines) > height {
		// Find selected task index
		selectedIdx := 0
		for i, task := range tt.laneTasks {
			if task.ID == tt.SelectedID {
				selectedIdx = i
				break
			}
		}

		// Calculate viewport start to keep selected line visible with padding
		// Add padding to prevent selected line from being hidden in empty space at edges
		const viewportPadding = 2
		start := 0

		// If selected line is too far down, scroll down to show it with padding
		if selectedIdx >= start+height-viewportPadding {
			start = selectedIdx - height + viewportPadding + 1
			if start < 0 {
				start = 0
			}
		}

		// If selected line is too far up, scroll up to show it with padding
		if selectedIdx < start+viewportPadding {
			start = selectedIdx - viewportPadding
			if start < 0 {
				start = 0
			}
		}

		end := start + height
		if end > len(lines) {
			end = len(lines)
		}
		totalLines := len(lines)
		lines = applyViewportWithIndicators(lines, start, end, totalLines)
	}

	return strings.Join(lines, "\n")
}

// renderLaneTaskLine renders a single task line with lane prefix + task info.
func (tt *TaskTree) renderLaneTaskLine(task types.ResolvedTask, assignment LaneAssignment, index int, isSelected bool, width int) string {
	// Build relation context for colored dependencies
	var context *LanePrefixSegmentContext
	if tt.SelectedID != "" {
		ancestors, descendants := buildSelectedTaskRelationGraph(tt.laneTasks, tt.SelectedID)
		ctx := buildSelectedTaskRelationLanes(tt.laneAssignments, ancestors, descendants)
		context = &ctx
	}

	// Generate lane prefix segments with coloring context
	segments := GeneratePrefixSegments(assignment, index, tt.laneAssignments, context)

	// Apply colors based on segment kind
	prefixRendered := ""
	for _, seg := range segments {
		styledText := seg.Text
		switch seg.Kind {
		case KindUpstream:
			styledText = lipgloss.NewStyle().Foreground(ColorCyan).Render(seg.Text)
		case KindDownstream:
			styledText = lipgloss.NewStyle().Foreground(ColorMagenta).Render(seg.Text)
			// KindNeutral: no color
		}
		prefixRendered += styledText
	}

	// Status indicator with color
	indicator := statusIndicator(task.Status, task.Classification)
	indicatorStyled := StatusStyleWithState(task.Status, task.Classification).Render(indicator)

	// Title — truncate BEFORE styling to avoid cutting ANSI sequences
	title := task.Title
	if !tt.TextWrap && width > 0 {
		// Overhead: selMarker(1) + prefix + space(1) + indicator(2) + space(1) + suffixes
		overhead := 1 + lipgloss.Width(prefixRendered) + 1 + 2 + 1
		if task.Priority == "high" {
			overhead++
		}
		if task.InCycle {
			overhead += 2
		}
		availableWidth := width - overhead
		title = truncateTitle(title, availableWidth)
	}

	if isSelected {
		title = lipgloss.NewStyle().Bold(true).Foreground(ColorWhite).Render(title)
	}

	// Priority suffix
	prioritySuffix := ""
	if task.Priority == "high" {
		prioritySuffix = lipgloss.NewStyle().Foreground(ColorPriorityHigh).Bold(true).Render("!")
	}

	// Cycle indicator
	cycleSuffix := ""
	if task.InCycle {
		cycleSuffix = lipgloss.NewStyle().Foreground(ColorMagenta).Render(" ↺")
	}

	// Selection marker
	selMarker := "  "
	if isSelected {
		selMarker = lipgloss.NewStyle().Foreground(ColorCyan).Render("▸ ")
	}

	return fmt.Sprintf("%s%s %s %s%s%s", selMarker, prefixRendered, indicatorStyled, title, prioritySuffix, cycleSuffix)
}

// moveDownLane navigates down in lane view.
func (tt *TaskTree) moveDownLane() {
	if len(tt.laneTasks) == 0 {
		return
	}
	if tt.Cursor < len(tt.laneTasks)-1 {
		tt.Cursor++
		tt.SelectedID = tt.laneTasks[tt.Cursor].ID
	}
}

// moveUpLane navigates up in lane view.
func (tt *TaskTree) moveUpLane() {
	if len(tt.laneTasks) == 0 {
		return
	}
	if tt.Cursor > 0 {
		tt.Cursor--
		tt.SelectedID = tt.laneTasks[tt.Cursor].ID
	}
}

// moveToTopLane moves to the first task in lane view.
func (tt *TaskTree) moveToTopLane() {
	if len(tt.laneTasks) == 0 {
		return
	}
	tt.Cursor = 0
	tt.SelectedID = tt.laneTasks[0].ID
}

// moveToBottomLane moves to the last task in lane view.
func (tt *TaskTree) moveToBottomLane() {
	if len(tt.laneTasks) == 0 {
		return
	}
	tt.Cursor = len(tt.laneTasks) - 1
	tt.SelectedID = tt.laneTasks[tt.Cursor].ID
}

// =============================================================================
// Phase 3: 3-Level Navigation Stubs
// =============================================================================

// moveDownNestedGrouped navigates down through 3-level nested status+feature hierarchy.
// Navigation rules:
// 1. On STATUS HEADER, COLLAPSED → Jump to next status header
// 2. On STATUS HEADER, EXPANDED → Move to first feature header
// 3. On FEATURE HEADER, COLLAPSED → Jump to next feature header (or next status if last)
// 4. On FEATURE HEADER, EXPANDED → Move to first task in feature
// 5. Within FEATURE, not at end → Move to next task
// 6. At END of FEATURE → Move to next feature header (or next status if last)
func (tt *TaskTree) moveDownNestedGrouped() {
	if len(tt.statusGroups) == 0 {
		return
	}

	// Defensive check: if selectedTaskIdx >= 0, we're definitely not on a status header
	// This handles cases where tests manually set indices without updating isOnStatusHeader
	if tt.selectedTaskIdx >= 0 {
		tt.isOnStatusHeader = false
	}

	// Rule 1 & 2: On STATUS HEADER
	if tt.isOnStatusHeader {
		statusGroup := tt.statusGroups[tt.selectedStatusIdx]

		if statusGroup.Collapsed {
			// Rule 1: Collapsed → jump to next status header
			if tt.selectedStatusIdx < len(tt.statusGroups)-1 {
				tt.selectedStatusIdx++
				tt.selectedFeatureIdx = -2
				tt.selectedTaskIdx = -1
				tt.isOnStatusHeader = true
				// Keep SelectedID pointing to last selected task
			}
		} else {
			// Rule 2: Expanded → move to first feature header
			if len(statusGroup.Features) > 0 {
				tt.selectedFeatureIdx = 0
				tt.selectedTaskIdx = -1
				tt.isOnStatusHeader = false
				// Keep SelectedID pointing to last selected task
			} else if statusGroup.Ungrouped != nil && len(statusGroup.Ungrouped.Tasks) > 0 {
				// No features, only ungrouped tasks - skip ungrouped header and go directly to first task
				tt.selectedFeatureIdx = -1 // ungrouped marker
				tt.isOnStatusHeader = false
				treeOrder := FlattenTreeOrder(statusGroup.Ungrouped.Tasks)
				if len(treeOrder) > 0 {
					tt.selectedTaskIdx = 0
					tt.SelectedID = treeOrder[0]
				}
			}
		}
		return
	}

	// Rule 3, 4, 5, 6: On FEATURE HEADER or within feature
	statusGroup := tt.statusGroups[tt.selectedStatusIdx]

	// Handle ungrouped group (selectedFeatureIdx == -1)
	if tt.selectedFeatureIdx == -1 {
		ungrouped := statusGroup.Ungrouped
		if ungrouped == nil {
			return
		}

		if tt.selectedTaskIdx == -1 {
			// On ungrouped header
			if ungrouped.Collapsed {
				// Jump to next status header
				if tt.selectedStatusIdx < len(tt.statusGroups)-1 {
					tt.selectedStatusIdx++
					tt.selectedFeatureIdx = -2
					tt.selectedTaskIdx = -1
					tt.isOnStatusHeader = true
					// Keep SelectedID pointing to last selected task
				}
			} else {
				// Enter ungrouped (move to first task)
				if len(ungrouped.Tasks) > 0 {
					// Use flattened tree order for navigation
					treeOrder := FlattenTreeOrder(ungrouped.Tasks)
					if len(treeOrder) > 0 {
						tt.selectedTaskIdx = 0
						tt.SelectedID = treeOrder[0]
					}
				}
			}
		} else {
			// Within ungrouped tasks - use tree order
			treeOrder := FlattenTreeOrder(ungrouped.Tasks)
			if tt.selectedTaskIdx < len(treeOrder)-1 {
				tt.selectedTaskIdx++
				tt.SelectedID = treeOrder[tt.selectedTaskIdx]
			} else {
				// At end of ungrouped → move to next status header
				if tt.selectedStatusIdx < len(tt.statusGroups)-1 {
					tt.selectedStatusIdx++
					tt.selectedFeatureIdx = -2
					tt.selectedTaskIdx = -1
					tt.isOnStatusHeader = true
					// Keep SelectedID pointing to last selected task
				}
			}
		}
		return
	}

	// Handle regular features (selectedFeatureIdx >= 0)
	if tt.selectedFeatureIdx < 0 || tt.selectedFeatureIdx >= len(statusGroup.Features) {
		return
	}

	feature := statusGroup.Features[tt.selectedFeatureIdx]

	if tt.selectedTaskIdx == -1 {
		// Rule 3 & 4: On FEATURE HEADER
		if feature.Collapsed {
			// Rule 3: Collapsed → jump to next feature header
			if tt.selectedFeatureIdx < len(statusGroup.Features)-1 {
				// Next feature in same status
				tt.selectedFeatureIdx++
				tt.selectedTaskIdx = -1
				// Keep SelectedID pointing to last selected task
			} else if statusGroup.Ungrouped != nil && len(statusGroup.Ungrouped.Tasks) > 0 {
				// Move to ungrouped header
				tt.selectedFeatureIdx = -1
				tt.selectedTaskIdx = -1
				// Keep SelectedID pointing to last selected task
			} else {
				// Move to next status header
				if tt.selectedStatusIdx < len(tt.statusGroups)-1 {
					tt.selectedStatusIdx++
					tt.selectedFeatureIdx = -2
					tt.selectedTaskIdx = -1
					tt.isOnStatusHeader = true
					// Keep SelectedID pointing to last selected task
				}
			}
		} else {
			// Rule 4: Expanded → move to first task (use tree order)
			if len(feature.Tasks) > 0 {
				treeOrder := FlattenTreeOrder(feature.Tasks)
				if len(treeOrder) > 0 {
					tt.selectedTaskIdx = 0
					tt.SelectedID = treeOrder[0]
				}
			}
		}
	} else {
		// Rule 5 & 6: Within feature - use tree order for navigation
		treeOrder := FlattenTreeOrder(feature.Tasks)
		if tt.selectedTaskIdx < len(treeOrder)-1 {
			// Rule 5: Not at end → move to next task in tree order
			tt.selectedTaskIdx++
			tt.SelectedID = treeOrder[tt.selectedTaskIdx]
		} else {
			// Rule 6: At end → move to next feature header
			if tt.selectedFeatureIdx < len(statusGroup.Features)-1 {
				// Next feature in same status
				tt.selectedFeatureIdx++
				tt.selectedTaskIdx = -1
				// Keep SelectedID pointing to last selected task
			} else if statusGroup.Ungrouped != nil && len(statusGroup.Ungrouped.Tasks) > 0 {
				// Move to ungrouped header
				tt.selectedFeatureIdx = -1
				tt.selectedTaskIdx = -1
				// Keep SelectedID pointing to last selected task
			} else {
				// Move to next status header
				if tt.selectedStatusIdx < len(tt.statusGroups)-1 {
					tt.selectedStatusIdx++
					tt.selectedFeatureIdx = -2
					tt.selectedTaskIdx = -1
					tt.isOnStatusHeader = true
					// Keep SelectedID pointing to last selected task
				}
			}
		}
	}
}

// moveUpNestedGrouped navigates up through 3-level nested status+feature hierarchy.
// Reverse of moveDownNestedGrouped logic.
func (tt *TaskTree) moveUpNestedGrouped() {
	if len(tt.statusGroups) == 0 {
		return
	}

	// Defensive check: if selectedTaskIdx >= 0, we're not on a status header
	if tt.selectedTaskIdx >= 0 {
		tt.isOnStatusHeader = false
	}

	// On status header - move to last visible item in previous status
	if tt.isOnStatusHeader {
		if tt.selectedStatusIdx > 0 {
			tt.selectedStatusIdx--
			prevStatusGroup := tt.statusGroups[tt.selectedStatusIdx]

			// If previous status group is collapsed, land on its header
			if prevStatusGroup.Collapsed {
				tt.selectedFeatureIdx = -2
				tt.selectedTaskIdx = -1
				tt.isOnStatusHeader = true
				return
			}

			// Previous status is expanded - find last visible item
			tt.isOnStatusHeader = false

			// Check ungrouped first (it comes last in rendering)
			if prevStatusGroup.Ungrouped != nil && len(prevStatusGroup.Ungrouped.Tasks) > 0 {
				tt.selectedFeatureIdx = -1
				ungrouped := prevStatusGroup.Ungrouped

				if ungrouped.Collapsed {
					// Land on ungrouped header
					tt.selectedTaskIdx = -1
				} else {
					// Land on last ungrouped task
					treeOrder := FlattenTreeOrder(ungrouped.Tasks)
					if len(treeOrder) > 0 {
						tt.selectedTaskIdx = len(treeOrder) - 1
						tt.SelectedID = treeOrder[tt.selectedTaskIdx]
					}
				}
				return
			}

			// No ungrouped - check features
			if len(prevStatusGroup.Features) > 0 {
				tt.selectedFeatureIdx = len(prevStatusGroup.Features) - 1
				lastFeature := prevStatusGroup.Features[tt.selectedFeatureIdx]

				if lastFeature.Collapsed {
					// Land on feature header
					tt.selectedTaskIdx = -1
				} else if len(lastFeature.Tasks) > 0 {
					// Land on last task in feature
					treeOrder := FlattenTreeOrder(lastFeature.Tasks)
					tt.selectedTaskIdx = len(treeOrder) - 1
					tt.SelectedID = treeOrder[tt.selectedTaskIdx]
				} else {
					// Feature is expanded but empty - land on header
					tt.selectedTaskIdx = -1
				}
				return
			}

			// No features or ungrouped - land on previous status header
			tt.selectedFeatureIdx = -2
			tt.selectedTaskIdx = -1
			tt.isOnStatusHeader = true
		}
		return
	}

	statusGroup := tt.statusGroups[tt.selectedStatusIdx]

	// Handle ungrouped group
	if tt.selectedFeatureIdx == -1 {
		ungrouped := statusGroup.Ungrouped
		if ungrouped == nil {
			return
		}

		if tt.selectedTaskIdx == -1 {
			// On ungrouped header - move to last feature (last task if expanded, or header if collapsed)
			if len(statusGroup.Features) > 0 {
				tt.selectedFeatureIdx = len(statusGroup.Features) - 1
				lastFeature := statusGroup.Features[tt.selectedFeatureIdx]

				// If last feature is expanded, land on its last task
				if !lastFeature.Collapsed && len(lastFeature.Tasks) > 0 {
					treeOrder := FlattenTreeOrder(lastFeature.Tasks)
					tt.selectedTaskIdx = len(treeOrder) - 1
					tt.SelectedID = treeOrder[tt.selectedTaskIdx]
				} else {
					// Last feature is collapsed, land on its header
					tt.selectedTaskIdx = -1
					// Keep SelectedID pointing to last selected task
				}
			} else {
				// Move to status header
				tt.selectedFeatureIdx = -2
				tt.selectedTaskIdx = -1
				tt.isOnStatusHeader = true
				// Keep SelectedID pointing to last selected task
			}
		} else {
			// Within ungrouped tasks - use tree order
			if tt.selectedTaskIdx > 0 {
				treeOrder := FlattenTreeOrder(ungrouped.Tasks)
				tt.selectedTaskIdx--
				tt.SelectedID = treeOrder[tt.selectedTaskIdx]
			} else {
				// Move to ungrouped header
				tt.selectedTaskIdx = -1
				// Keep SelectedID pointing to last selected task
			}
		}
		return
	}

	// Handle regular features
	if tt.selectedFeatureIdx < 0 || tt.selectedFeatureIdx >= len(statusGroup.Features) {
		return
	}

	feature := statusGroup.Features[tt.selectedFeatureIdx]

	if tt.selectedTaskIdx == -1 {
		// On feature header
		if tt.selectedFeatureIdx > 0 {
			// Move to previous feature
			tt.selectedFeatureIdx--
			prevFeature := statusGroup.Features[tt.selectedFeatureIdx]

			// If previous feature is expanded, land on its last task
			if !prevFeature.Collapsed && len(prevFeature.Tasks) > 0 {
				treeOrder := FlattenTreeOrder(prevFeature.Tasks)
				tt.selectedTaskIdx = len(treeOrder) - 1
				tt.SelectedID = treeOrder[tt.selectedTaskIdx]
			} else {
				// Previous feature is collapsed, land on its header
				tt.selectedTaskIdx = -1
				// Keep SelectedID pointing to last selected task
			}
		} else {
			// Move to status header
			tt.selectedFeatureIdx = -2
			tt.selectedTaskIdx = -1
			tt.isOnStatusHeader = true
			// Keep SelectedID pointing to last selected task
		}
	} else {
		// Within feature - use tree order
		if tt.selectedTaskIdx > 0 {
			treeOrder := FlattenTreeOrder(feature.Tasks)
			tt.selectedTaskIdx--
			tt.SelectedID = treeOrder[tt.selectedTaskIdx]
		} else {
			// Move to feature header
			tt.selectedTaskIdx = -1
			// Keep SelectedID pointing to last selected task
		}
	}
}

// moveToTopNestedGrouped moves to the first status header in nested grouped view.
func (tt *TaskTree) moveToTopNestedGrouped() {
	if len(tt.statusGroups) == 0 {
		return
	}

	// Move to first status header
	tt.selectedStatusIdx = 0
	tt.selectedFeatureIdx = -2
	tt.selectedTaskIdx = -1
	tt.isOnStatusHeader = true
	// Keep SelectedID pointing to last selected task
}

// moveToBottomNestedGrouped moves to the last task in the last feature of the last status.
func (tt *TaskTree) moveToBottomNestedGrouped() {
	if len(tt.statusGroups) == 0 {
		return
	}

	// Start from the last status group
	tt.selectedStatusIdx = len(tt.statusGroups) - 1
	statusGroup := tt.statusGroups[tt.selectedStatusIdx]

	// Check ungrouped first (it comes last in rendering)
	if statusGroup.Ungrouped != nil && len(statusGroup.Ungrouped.Tasks) > 0 {
		ungrouped := statusGroup.Ungrouped
		tt.selectedFeatureIdx = -1
		tt.isOnStatusHeader = false

		if ungrouped.Collapsed {
			// Stay on ungrouped header
			tt.selectedTaskIdx = -1
			// Keep SelectedID pointing to last selected task
		} else {
			// Move to last task in ungrouped (using tree order)
			treeOrder := FlattenTreeOrder(ungrouped.Tasks)
			if len(treeOrder) > 0 {
				tt.selectedTaskIdx = len(treeOrder) - 1
				tt.SelectedID = treeOrder[tt.selectedTaskIdx]
			}
		}
		return
	}

	// Check features (last feature, last task)
	if len(statusGroup.Features) > 0 {
		tt.selectedFeatureIdx = len(statusGroup.Features) - 1
		feature := statusGroup.Features[tt.selectedFeatureIdx]
		tt.isOnStatusHeader = false

		if feature.Collapsed {
			// Stay on feature header
			tt.selectedTaskIdx = -1
			// Keep SelectedID pointing to last selected task
		} else if len(feature.Tasks) > 0 {
			// Move to last task in feature (using tree order)
			treeOrder := FlattenTreeOrder(feature.Tasks)
			if len(treeOrder) > 0 {
				tt.selectedTaskIdx = len(treeOrder) - 1
				tt.SelectedID = treeOrder[tt.selectedTaskIdx]
			}
		}
		return
	}

	// No features or ungrouped, stay on status header
	tt.selectedFeatureIdx = -2
	tt.selectedTaskIdx = -1
	tt.isOnStatusHeader = true
	tt.SelectedID = ""
}

// clearTerminalSectionNav resets all terminal status section navigation state.
func (tt *TaskTree) clearTerminalSectionNav() {
	tt.isOnDraftSection = false
	tt.isOnCancelledSection = false
	tt.isOnSupersededSection = false
	tt.isOnArchivedSection = false
	tt.isOnCompletedSection = false
	tt.draftFeatureIdx = -1
	tt.cancelledFeatureIdx = -1
	tt.supersededFeatureIdx = -1
	tt.archivedFeatureIdx = -1
	tt.completedFeatureIdx = -1
	tt.draftTaskIdx = -1
	tt.cancelledTaskIdx = -1
	tt.supersededTaskIdx = -1
	tt.archivedTaskIdx = -1
	tt.completedTaskIdx = -1
}

// clearDraftCompletedNav is kept as an alias for backward compatibility.
func (tt *TaskTree) clearDraftCompletedNav() {
	tt.clearTerminalSectionNav()
}

// moveToDraftSection moves cursor to draft section header.
func (tt *TaskTree) moveToDraftSection() {
	tt.isOnUngrouped = false
	tt.selectedFeatureIdx = -1
	tt.selectedFeatureTaskIdx = -1
	tt.clearTerminalSectionNav()
	tt.isOnDraftSection = true
	tt.SelectedID = ""
}

// moveToCancelledSection moves cursor to cancelled section header.
func (tt *TaskTree) moveToCancelledSection() {
	tt.isOnUngrouped = false
	tt.selectedFeatureIdx = -1
	tt.selectedFeatureTaskIdx = -1
	tt.clearTerminalSectionNav()
	tt.isOnCancelledSection = true
	tt.SelectedID = ""
}

// moveToSupersededSection moves cursor to superseded section header.
func (tt *TaskTree) moveToSupersededSection() {
	tt.isOnUngrouped = false
	tt.selectedFeatureIdx = -1
	tt.selectedFeatureTaskIdx = -1
	tt.clearTerminalSectionNav()
	tt.isOnSupersededSection = true
	tt.SelectedID = ""
}

// moveToArchivedSection moves cursor to archived section header.
func (tt *TaskTree) moveToArchivedSection() {
	tt.isOnUngrouped = false
	tt.selectedFeatureIdx = -1
	tt.selectedFeatureTaskIdx = -1
	tt.clearTerminalSectionNav()
	tt.isOnArchivedSection = true
	tt.SelectedID = ""
}

// moveToCompletedSection moves cursor to completed section header.
func (tt *TaskTree) moveToCompletedSection() {
	tt.isOnUngrouped = false
	tt.selectedFeatureIdx = -1
	tt.selectedFeatureTaskIdx = -1
	tt.clearTerminalSectionNav()
	tt.isOnCompletedSection = true
	tt.SelectedID = ""
}

// moveToNextSectionAfterActiveFeatures moves past ungrouped → draft → cancelled → superseded → archived → completed.
func (tt *TaskTree) moveToNextSectionAfterActiveFeatures() {
	if tt.featureGroups.Ungrouped != nil {
		tt.selectedFeatureIdx = -1
		tt.selectedFeatureTaskIdx = -1
		tt.isOnUngrouped = true
		tt.clearTerminalSectionNav()
		tt.SelectedID = ""
	} else {
		tt.moveToFirstTerminalSection()
	}
}

// isOnAnyTerminalSection returns true if the cursor is on any terminal status section.
func (tt *TaskTree) isOnAnyTerminalSection() bool {
	return tt.isOnDraftSection || tt.isOnCancelledSection || tt.isOnSupersededSection || tt.isOnArchivedSection || tt.isOnCompletedSection
}

// terminalSectionOrder defines the rendering/navigation order for terminal sections.
// Each entry has: name, hasTasks func, moveToSection func, isOnSection func, collapsed func, featureIDs func, featureIdx func, setFeatureIdx func
type terminalSectionInfo struct {
	name       string
	hasTasks   func() bool
	moveTo     func()
	isOn       func() bool
	collapsed  func() bool
	featureIDs func() []string
	featureIdx func() int
	setFeatIdx func(int)
	taskIdx    func() int
	setTaskIdx func(int)
}

func (tt *TaskTree) terminalSections() []terminalSectionInfo {
	return []terminalSectionInfo{
		{
			name:       "draft",
			hasTasks:   func() bool { return tt.hasDraftTasks },
			moveTo:     tt.moveToDraftSection,
			isOn:       func() bool { return tt.isOnDraftSection },
			collapsed:  func() bool { return tt.draftCollapsed },
			featureIDs: func() []string { return tt.draftFeatureIDs },
			featureIdx: func() int { return tt.draftFeatureIdx },
			setFeatIdx: func(i int) { tt.draftFeatureIdx = i },
			taskIdx:    func() int { return tt.draftTaskIdx },
			setTaskIdx: func(i int) { tt.draftTaskIdx = i },
		},
		{
			name:       "cancelled",
			hasTasks:   func() bool { return tt.hasCancelledTasks },
			moveTo:     tt.moveToCancelledSection,
			isOn:       func() bool { return tt.isOnCancelledSection },
			collapsed:  func() bool { return tt.cancelledCollapsed },
			featureIDs: func() []string { return tt.cancelledFeatureIDs },
			featureIdx: func() int { return tt.cancelledFeatureIdx },
			setFeatIdx: func(i int) { tt.cancelledFeatureIdx = i },
			taskIdx:    func() int { return tt.cancelledTaskIdx },
			setTaskIdx: func(i int) { tt.cancelledTaskIdx = i },
		},
		{
			name:       "superseded",
			hasTasks:   func() bool { return tt.hasSupersededTasks },
			moveTo:     tt.moveToSupersededSection,
			isOn:       func() bool { return tt.isOnSupersededSection },
			collapsed:  func() bool { return tt.supersededCollapsed },
			featureIDs: func() []string { return tt.supersededFeatureIDs },
			featureIdx: func() int { return tt.supersededFeatureIdx },
			setFeatIdx: func(i int) { tt.supersededFeatureIdx = i },
			taskIdx:    func() int { return tt.supersededTaskIdx },
			setTaskIdx: func(i int) { tt.supersededTaskIdx = i },
		},
		{
			name:       "archived",
			hasTasks:   func() bool { return tt.hasArchivedTasks },
			moveTo:     tt.moveToArchivedSection,
			isOn:       func() bool { return tt.isOnArchivedSection },
			collapsed:  func() bool { return tt.archivedCollapsed },
			featureIDs: func() []string { return tt.archivedFeatureIDs },
			featureIdx: func() int { return tt.archivedFeatureIdx },
			setFeatIdx: func(i int) { tt.archivedFeatureIdx = i },
			taskIdx:    func() int { return tt.archivedTaskIdx },
			setTaskIdx: func(i int) { tt.archivedTaskIdx = i },
		},
		{
			name:       "completed",
			hasTasks:   func() bool { return tt.hasCompletedTasks },
			moveTo:     tt.moveToCompletedSection,
			isOn:       func() bool { return tt.isOnCompletedSection },
			collapsed:  func() bool { return tt.completedCollapsed },
			featureIDs: func() []string { return tt.completedFeatureIDs },
			featureIdx: func() int { return tt.completedFeatureIdx },
			setFeatIdx: func(i int) { tt.completedFeatureIdx = i },
			taskIdx:    func() int { return tt.completedTaskIdx },
			setTaskIdx: func(i int) { tt.completedTaskIdx = i },
		},
	}
}

// moveToFirstTerminalSection moves to the first terminal section that has tasks.
func (tt *TaskTree) moveToFirstTerminalSection() {
	for _, sec := range tt.terminalSections() {
		if sec.hasTasks() {
			sec.moveTo()
			return
		}
	}
}

// moveToNextTerminalSection moves to the next terminal section after the current one.
// Returns true if it moved to a next section, false if at end.
func (tt *TaskTree) moveToNextTerminalSection() bool {
	sections := tt.terminalSections()
	foundCurrent := false
	for _, sec := range sections {
		if sec.isOn() {
			foundCurrent = true
			continue
		}
		if foundCurrent && sec.hasTasks() {
			sec.moveTo()
			return true
		}
	}
	return false
}

// moveToPrevTerminalSection moves to the previous terminal section before the current one.
// Returns true if it moved, false if at beginning.
func (tt *TaskTree) moveToPrevTerminalSection() bool {
	sections := tt.terminalSections()
	currentIdx := -1
	for i, sec := range sections {
		if sec.isOn() {
			currentIdx = i
			break
		}
	}
	if currentIdx <= 0 {
		return false
	}
	// Search backward for a section with tasks
	for i := currentIdx - 1; i >= 0; i-- {
		if sections[i].hasTasks() {
			sections[i].moveTo()
			return true
		}
	}
	return false
}

// lastTerminalSectionWithTasks returns the last section in order that has tasks.
func (tt *TaskTree) lastTerminalSectionWithTasks() *terminalSectionInfo {
	sections := tt.terminalSections()
	for i := len(sections) - 1; i >= 0; i-- {
		if sections[i].hasTasks() {
			return &sections[i]
		}
	}
	return nil
}

// moveDownFeatureGrouped handles 2-level feature view navigation (Features → Tasks).
func (tt *TaskTree) moveDownFeatureGrouped() {
	// Handle terminal section navigation (Draft/Cancelled/Superseded/Archived/Completed)
	if tt.isOnAnyTerminalSection() {
		sections := tt.terminalSections()
		for _, sec := range sections {
			if !sec.isOn() {
				continue
			}
			featIdx := sec.featureIdx()
			taskIdx := sec.taskIdx()
			if featIdx == -1 {
				// On section header
				if sec.collapsed() {
					// Collapsed → jump to next section
					tt.moveToNextTerminalSection()
					return
				}
				// Expanded → move to first sub-feature
				fids := sec.featureIDs()
				if len(fids) > 0 {
					sec.setFeatIdx(0)
					sec.setTaskIdx(-1)
					tt.SelectedID = ""
				}
			} else if taskIdx == -1 {
				// On a sub-feature header
				// Check if sub-feature is expanded and has tasks
				sectionTasks := tt.collectTerminalSectionTasks(sec.name)
				subTasks, isCollapsed := getTerminalSubFeatureTasks(sectionTasks, sec.featureIDs(), featIdx, tt.featureCollapsed, sec.name)
				if !isCollapsed && len(subTasks) > 0 {
					// Expanded sub-feature with tasks → enter first task
					treeOrder := terminalSubFeatureTreeOrder(subTasks)
					if len(treeOrder) > 0 {
						sec.setTaskIdx(0)
						tt.SelectedID = treeOrder[0]
						return
					}
				}
				// Collapsed or no tasks → move to next sub-feature or next section
				fids := sec.featureIDs()
				if featIdx < len(fids)-1 {
					sec.setFeatIdx(featIdx + 1)
					sec.setTaskIdx(-1)
					tt.SelectedID = ""
				} else {
					// At end of sub-features → jump to next terminal section
					tt.moveToNextTerminalSection()
				}
			} else {
				// Within tasks of a sub-feature → advance to next task
				sectionTasks := tt.collectTerminalSectionTasks(sec.name)
				subTasks, _ := getTerminalSubFeatureTasks(sectionTasks, sec.featureIDs(), featIdx, tt.featureCollapsed, sec.name)
				treeOrder := terminalSubFeatureTreeOrder(subTasks)
				if taskIdx < len(treeOrder)-1 {
					sec.setTaskIdx(taskIdx + 1)
					tt.SelectedID = treeOrder[taskIdx+1]
				} else {
					// At last task → move to next sub-feature header or next section
					fids := sec.featureIDs()
					if featIdx < len(fids)-1 {
						sec.setFeatIdx(featIdx + 1)
						sec.setTaskIdx(-1)
						tt.SelectedID = ""
					} else {
						tt.moveToNextTerminalSection()
					}
				}
			}
			return
		}
		return
	}

	// Handle ungrouped group
	if tt.isOnUngrouped {
		if tt.featureGroups.Ungrouped == nil {
			return
		}
		ungrouped := tt.featureGroups.Ungrouped

		if tt.selectedFeatureTaskIdx == -1 {
			// On ungrouped header
			if ungrouped.Collapsed {
				// Collapsed → jump to first terminal section
				tt.moveToFirstTerminalSection()
				return
			} else {
				// Expanded → move to first task
				if len(ungrouped.Tasks) > 0 {
					treeOrder := featureActiveTreeOrder(ungrouped.Tasks)
					if len(treeOrder) > 0 {
						tt.selectedFeatureTaskIdx = 0
						tt.SelectedID = treeOrder[0]
					}
				}
			}
		} else {
			// Within ungrouped tasks
			treeOrder := featureActiveTreeOrder(ungrouped.Tasks)
			if tt.selectedFeatureTaskIdx < len(treeOrder)-1 {
				tt.selectedFeatureTaskIdx++
				tt.SelectedID = treeOrder[tt.selectedFeatureTaskIdx]
			} else {
				// At end of ungrouped → jump to first terminal section
				tt.moveToFirstTerminalSection()
			}
		}
		return
	}

	// Handle regular features
	if len(tt.featureGroups.Features) == 0 {
		return
	}

	// Bounds check
	if tt.selectedFeatureIdx < 0 || tt.selectedFeatureIdx >= len(tt.featureGroups.Features) {
		return
	}

	feature := tt.featureGroups.Features[tt.selectedFeatureIdx]

	if tt.selectedFeatureTaskIdx == -1 {
		// On feature header
		if feature.Collapsed {
			// Rule 1: On FEATURE HEADER, COLLAPSED → Jump to next feature header
			if tt.selectedFeatureIdx < len(tt.featureGroups.Features)-1 {
				tt.selectedFeatureIdx++
				tt.selectedFeatureTaskIdx = -1
				tt.SelectedID = ""
			} else {
				// Past last feature → ungrouped or draft/completed
				tt.moveToNextSectionAfterActiveFeatures()
			}
		} else {
			// Rule 2: On FEATURE HEADER, EXPANDED → Move to first task
			if len(feature.Tasks) > 0 {
				treeOrder := featureActiveTreeOrder(feature.Tasks)
				if len(treeOrder) > 0 {
					tt.selectedFeatureTaskIdx = 0
					tt.SelectedID = treeOrder[0]
				} else if tt.selectedFeatureIdx < len(tt.featureGroups.Features)-1 {
					tt.selectedFeatureIdx++
					tt.selectedFeatureTaskIdx = -1
					tt.SelectedID = ""
				} else {
					tt.moveToNextSectionAfterActiveFeatures()
				}
			}
		}
	} else {
		// Within feature tasks
		treeOrder := featureActiveTreeOrder(feature.Tasks)

		if tt.selectedFeatureTaskIdx < len(treeOrder)-1 {
			// Rule 3: Within FEATURE, not at end → Move to next task
			tt.selectedFeatureTaskIdx++
			tt.SelectedID = treeOrder[tt.selectedFeatureTaskIdx]
		} else {
			// Rule 4: At END of FEATURE → Move to next feature header
			if tt.selectedFeatureIdx < len(tt.featureGroups.Features)-1 {
				tt.selectedFeatureIdx++
				tt.selectedFeatureTaskIdx = -1
				tt.SelectedID = ""
			} else {
				// Past last feature → ungrouped or draft/completed
				tt.moveToNextSectionAfterActiveFeatures()
			}
		}
	}
}

// moveUpToEndOfActiveContent moves cursor to the last item before draft/completed sections.
func (tt *TaskTree) moveUpToEndOfActiveContent() {
	if tt.featureGroups.Ungrouped != nil {
		ungrouped := tt.featureGroups.Ungrouped
		tt.isOnUngrouped = true
		tt.selectedFeatureIdx = -1

		if !ungrouped.Collapsed && len(ungrouped.Tasks) > 0 {
			treeOrder := featureActiveTreeOrder(ungrouped.Tasks)
			if len(treeOrder) > 0 {
				tt.selectedFeatureTaskIdx = len(treeOrder) - 1
				tt.SelectedID = treeOrder[tt.selectedFeatureTaskIdx]
				return
			}
		}
		tt.selectedFeatureTaskIdx = -1
		tt.SelectedID = ""
		return
	}

	if len(tt.featureGroups.Features) > 0 {
		lastIdx := len(tt.featureGroups.Features) - 1
		lastFeature := tt.featureGroups.Features[lastIdx]
		tt.selectedFeatureIdx = lastIdx
		tt.isOnUngrouped = false

		if !lastFeature.Collapsed && len(lastFeature.Tasks) > 0 {
			treeOrder := featureActiveTreeOrder(lastFeature.Tasks)
			if len(treeOrder) > 0 {
				tt.selectedFeatureTaskIdx = len(treeOrder) - 1
				tt.SelectedID = treeOrder[tt.selectedFeatureTaskIdx]
				return
			}
		}
		tt.selectedFeatureTaskIdx = -1
		tt.SelectedID = ""
	}
}

// moveUpFeatureGrouped handles upward 2-level feature view navigation (Features → Tasks).
func (tt *TaskTree) moveUpFeatureGrouped() {
	// Handle terminal section navigation (moving up) — generic for all sections
	if tt.isOnAnyTerminalSection() {
		sections := tt.terminalSections()
		for _, sec := range sections {
			if !sec.isOn() {
				continue
			}
			featIdx := sec.featureIdx()
			taskIdx := sec.taskIdx()

			if taskIdx >= 0 {
				// Within tasks of a sub-feature
				if taskIdx > 0 {
					// Move to previous task
					sectionTasks := tt.collectTerminalSectionTasks(sec.name)
					subTasks, _ := getTerminalSubFeatureTasks(sectionTasks, sec.featureIDs(), featIdx, tt.featureCollapsed, sec.name)
					treeOrder := terminalSubFeatureTreeOrder(subTasks)
					if taskIdx < len(treeOrder) {
						sec.setTaskIdx(taskIdx - 1)
						tt.SelectedID = treeOrder[taskIdx-1]
					} else {
						// Index out of bounds — return to sub-feature header
						sec.setTaskIdx(-1)
						tt.SelectedID = ""
					}
				} else {
					// At first task → move back to sub-feature header
					sec.setTaskIdx(-1)
					tt.SelectedID = ""
				}
			} else if featIdx == -1 {
				// On section header → go up to previous section's last task/sub-feature, or active content
				if !tt.moveToPrevTerminalSection() {
					// No previous terminal section → go to end of active content
					tt.clearTerminalSectionNav()
					tt.moveUpToEndOfActiveContent()
				} else {
					// Moved to previous section header. Now position on its last sub-feature's last task if expanded.
					prevSections := tt.terminalSections()
					for _, ps := range prevSections {
						if ps.isOn() {
							if !ps.collapsed() {
								fids := ps.featureIDs()
								if len(fids) > 0 {
									lastFeatIdx := len(fids) - 1
									ps.setFeatIdx(lastFeatIdx)
									// Try to land on last task of last sub-feature
									sectionTasks := tt.collectTerminalSectionTasks(ps.name)
									subTasks, isCollapsed := getTerminalSubFeatureTasks(sectionTasks, fids, lastFeatIdx, tt.featureCollapsed, ps.name)
									if !isCollapsed && len(subTasks) > 0 {
										treeOrder := terminalSubFeatureTreeOrder(subTasks)
										if len(treeOrder) > 0 {
											ps.setTaskIdx(len(treeOrder) - 1)
											tt.SelectedID = treeOrder[len(treeOrder)-1]
										}
									} else {
										ps.setTaskIdx(-1)
										tt.SelectedID = ""
									}
								}
							}
							break
						}
					}
				}
			} else if featIdx > 0 {
				// On a sub-feature header (not the first) → move to previous sub-feature's last task
				prevFeatIdx := featIdx - 1
				sec.setFeatIdx(prevFeatIdx)
				sectionTasks := tt.collectTerminalSectionTasks(sec.name)
				subTasks, isCollapsed := getTerminalSubFeatureTasks(sectionTasks, sec.featureIDs(), prevFeatIdx, tt.featureCollapsed, sec.name)
				if !isCollapsed && len(subTasks) > 0 {
					treeOrder := terminalSubFeatureTreeOrder(subTasks)
					if len(treeOrder) > 0 {
						sec.setTaskIdx(len(treeOrder) - 1)
						tt.SelectedID = treeOrder[len(treeOrder)-1]
					} else {
						sec.setTaskIdx(-1)
						tt.SelectedID = ""
					}
				} else {
					sec.setTaskIdx(-1)
					tt.SelectedID = ""
				}
			} else {
				// On first sub-feature header → move to section header
				sec.setFeatIdx(-1)
				sec.setTaskIdx(-1)
				tt.SelectedID = ""
			}
			return
		}
		return
	}

	// Handle ungrouped group
	if tt.isOnUngrouped {
		if tt.featureGroups.Ungrouped == nil {
			return
		}
		ungrouped := tt.featureGroups.Ungrouped

		if tt.selectedFeatureTaskIdx == -1 {
			// On ungrouped header → move to last task of last feature
			if len(tt.featureGroups.Features) > 0 {
				lastFeatureIdx := len(tt.featureGroups.Features) - 1
				lastFeature := tt.featureGroups.Features[lastFeatureIdx]

				tt.selectedFeatureIdx = lastFeatureIdx
				tt.isOnUngrouped = false

				if !lastFeature.Collapsed && len(lastFeature.Tasks) > 0 {
					// Land on last task
					treeOrder := featureActiveTreeOrder(lastFeature.Tasks)
					if len(treeOrder) > 0 {
						tt.selectedFeatureTaskIdx = len(treeOrder) - 1
						tt.SelectedID = treeOrder[tt.selectedFeatureTaskIdx]
					} else {
						tt.selectedFeatureTaskIdx = -1
						tt.SelectedID = ""
					}
				} else {
					// Land on feature header
					tt.selectedFeatureTaskIdx = -1
					tt.SelectedID = ""
				}
			}
		} else {
			// Within ungrouped tasks
			if tt.selectedFeatureTaskIdx > 0 {
				treeOrder := featureActiveTreeOrder(ungrouped.Tasks)
				if len(treeOrder) == 0 {
					tt.selectedFeatureTaskIdx = -1
					tt.SelectedID = ""
					return
				}
				if tt.selectedFeatureTaskIdx >= len(treeOrder) {
					tt.selectedFeatureTaskIdx = len(treeOrder) - 1
				}
				tt.selectedFeatureTaskIdx--
				tt.SelectedID = treeOrder[tt.selectedFeatureTaskIdx]
			} else {
				// Move to ungrouped header
				tt.selectedFeatureTaskIdx = -1
				tt.SelectedID = ""
			}
		}
		return
	}

	// Handle regular features
	if len(tt.featureGroups.Features) == 0 {
		return
	}

	// Bounds check
	if tt.selectedFeatureIdx < 0 || tt.selectedFeatureIdx >= len(tt.featureGroups.Features) {
		return
	}

	feature := tt.featureGroups.Features[tt.selectedFeatureIdx]

	if tt.selectedFeatureTaskIdx == -1 {
		// On feature header → move to previous feature
		if tt.selectedFeatureIdx > 0 {
			tt.selectedFeatureIdx--
			prevFeature := tt.featureGroups.Features[tt.selectedFeatureIdx]

			if !prevFeature.Collapsed && len(prevFeature.Tasks) > 0 {
				// Land on last task of previous feature
				treeOrder := featureActiveTreeOrder(prevFeature.Tasks)
				if len(treeOrder) > 0 {
					tt.selectedFeatureTaskIdx = len(treeOrder) - 1
					tt.SelectedID = treeOrder[tt.selectedFeatureTaskIdx]
				} else {
					tt.selectedFeatureTaskIdx = -1
					tt.SelectedID = ""
				}
			} else {
				// Land on feature header
				tt.selectedFeatureTaskIdx = -1
				tt.SelectedID = ""
			}
		}
		// At first feature header → nowhere to go
	} else {
		// Within feature tasks
		if tt.selectedFeatureTaskIdx > 0 {
			treeOrder := featureActiveTreeOrder(feature.Tasks)
			if len(treeOrder) == 0 {
				tt.selectedFeatureTaskIdx = -1
				tt.SelectedID = ""
				return
			}
			if tt.selectedFeatureTaskIdx >= len(treeOrder) {
				tt.selectedFeatureTaskIdx = len(treeOrder) - 1
			}
			tt.selectedFeatureTaskIdx--
			tt.SelectedID = treeOrder[tt.selectedFeatureTaskIdx]
		} else {
			// Move to feature header
			tt.selectedFeatureTaskIdx = -1
			tt.SelectedID = ""
		}
	}
}

// moveToTopFeatureGrouped jumps to the first navigable item in feature view.
// This is the first feature header (or first task if the feature is expanded).
func (tt *TaskTree) moveToTopFeatureGrouped() {
	tt.clearDraftCompletedNav()

	if len(tt.featureGroups.Features) == 0 {
		// No features — try ungrouped
		if tt.featureGroups.Ungrouped != nil {
			tt.selectedFeatureIdx = -1
			tt.selectedFeatureTaskIdx = -1
			tt.isOnUngrouped = true
			tt.SelectedID = ""
		}
		return
	}

	// Jump to the first feature header
	tt.selectedFeatureIdx = 0
	tt.selectedFeatureTaskIdx = -1 // On header
	tt.isOnUngrouped = false
	tt.SelectedID = ""
}

// moveToBottomFeatureGrouped jumps to the last navigable item in feature view.
// Order: Active Features → Ungrouped → Draft → Cancelled → Superseded → Archived → Completed
func (tt *TaskTree) moveToBottomFeatureGrouped() {
	// Try last terminal section (rendered last)
	lastSec := tt.lastTerminalSectionWithTasks()
	if lastSec != nil {
		lastSec.moveTo()
		// Position on last sub-feature if expanded
		if !lastSec.collapsed() {
			fids := lastSec.featureIDs()
			if len(fids) > 0 {
				lastSec.setFeatIdx(len(fids) - 1)
			}
		}
		return
	}

	tt.clearTerminalSectionNav()

	// Try ungrouped section
	if tt.featureGroups.Ungrouped != nil {
		ungrouped := tt.featureGroups.Ungrouped
		tt.isOnUngrouped = true
		tt.selectedFeatureIdx = -1

		if ungrouped.Collapsed || len(ungrouped.Tasks) == 0 {
			tt.selectedFeatureTaskIdx = -1
			tt.SelectedID = ""
		} else {
			treeOrder := FlattenTreeOrder(ungrouped.Tasks)
			if len(treeOrder) > 0 {
				tt.selectedFeatureTaskIdx = len(treeOrder) - 1
				tt.SelectedID = treeOrder[tt.selectedFeatureTaskIdx]
			} else {
				tt.selectedFeatureTaskIdx = -1
				tt.SelectedID = ""
			}
		}
		return
	}

	// No ungrouped — go to last feature
	if len(tt.featureGroups.Features) == 0 {
		return
	}

	lastIdx := len(tt.featureGroups.Features) - 1
	lastFeature := tt.featureGroups.Features[lastIdx]

	tt.selectedFeatureIdx = lastIdx
	tt.isOnUngrouped = false

	if lastFeature.Collapsed || len(lastFeature.Tasks) == 0 {
		tt.selectedFeatureTaskIdx = -1
		tt.SelectedID = ""
	} else {
		treeOrder := FlattenTreeOrder(lastFeature.Tasks)
		if len(treeOrder) > 0 {
			tt.selectedFeatureTaskIdx = len(treeOrder) - 1
			tt.SelectedID = treeOrder[tt.selectedFeatureTaskIdx]
		} else {
			tt.selectedFeatureTaskIdx = -1
			tt.SelectedID = ""
		}
	}
}

// getCurrentFeatureID returns the current feature ID for hierarchical collapse keys.
// Returns empty string if not on a feature header or if on ungrouped.
func (tt *TaskTree) getCurrentFeatureID() string {
	if len(tt.statusGroups) == 0 {
		return ""
	}
	if tt.selectedStatusIdx < 0 || tt.selectedStatusIdx >= len(tt.statusGroups) {
		return ""
	}
	if tt.selectedFeatureIdx == -1 {
		return "[Ungrouped]" // Special marker for ungrouped
	}
	if tt.selectedFeatureIdx < 0 || tt.selectedFeatureIdx >= len(tt.statusGroups[tt.selectedStatusIdx].Features) {
		return ""
	}
	return tt.statusGroups[tt.selectedStatusIdx].Features[tt.selectedFeatureIdx].ID
}

// getCurrentStatusName returns the current status name for hierarchical collapse keys.
// Returns empty string if selectedStatusIdx is out of bounds.
func (tt *TaskTree) getCurrentStatusName() string {
	if len(tt.statusGroups) == 0 {
		return ""
	}
	if tt.selectedStatusIdx < 0 || tt.selectedStatusIdx >= len(tt.statusGroups) {
		return ""
	}
	return tt.statusGroups[tt.selectedStatusIdx].Name
}

// =============================================================================
// Dependency Relation Graph for Colored Lanes
// =============================================================================

// buildSelectedTaskRelationGraph builds ancestor and descendant sets for the selected task.
// Ancestors: tasks that the selected task depends on (directly or transitively).
// Descendants: tasks that depend on the selected task (directly or transitively).
func buildSelectedTaskRelationGraph(tasks []types.ResolvedTask, selectedID string) (ancestors map[string]bool, descendants map[string]bool) {
	ancestors = make(map[string]bool)
	descendants = make(map[string]bool)

	if selectedID == "" {
		return
	}

	// Build task lookup and reverse dependency index
	taskByID := make(map[string]*types.ResolvedTask)
	dependentsByTask := make(map[string][]string)
	for i := range tasks {
		task := &tasks[i]
		taskByID[task.ID] = task
		for _, depID := range task.DependsOn {
			dependentsByTask[depID] = append(dependentsByTask[depID], task.ID)
		}
	}

	// Walk up dependencies (ancestors)
	visited := make(map[string]bool)
	var walkUp func(taskID string)
	walkUp = func(taskID string) {
		if visited[taskID] {
			return
		}
		visited[taskID] = true
		task, ok := taskByID[taskID]
		if !ok {
			return
		}
		for _, depID := range task.DependsOn {
			ancestors[depID] = true
			walkUp(depID)
		}
	}
	walkUp(selectedID)

	// Walk down dependents (descendants)
	visited = make(map[string]bool)
	var walkDown func(taskID string)
	walkDown = func(taskID string) {
		if visited[taskID] {
			return
		}
		visited[taskID] = true
		for _, dependentID := range dependentsByTask[taskID] {
			descendants[dependentID] = true
			walkDown(dependentID)
		}
	}
	walkDown(selectedID)

	return
}

// buildSelectedTaskRelationLanes maps ancestors and descendants to their lane numbers.
func buildSelectedTaskRelationLanes(assignments []LaneAssignment, ancestors, descendants map[string]bool) LanePrefixSegmentContext {
	ctx := LanePrefixSegmentContext{
		UpstreamLanes:   make(map[int]bool),
		DownstreamLanes: make(map[int]bool),
	}

	// Build lane by task ID map
	laneByTaskID := make(map[string]int)
	for _, assignment := range assignments {
		laneByTaskID[assignment.TaskID] = assignment.Lane
	}

	// Map ancestors to upstream lanes
	for ancestorID := range ancestors {
		if lane, ok := laneByTaskID[ancestorID]; ok {
			ctx.UpstreamLanes[lane] = true
		}
	}

	// Map descendants to downstream lanes
	for descendantID := range descendants {
		if lane, ok := laneByTaskID[descendantID]; ok {
			ctx.DownstreamLanes[lane] = true
		}
	}

	return ctx
}

// applyViewportWithIndicators slices lines to fit within height and adds scroll
// overflow indicators (↑N more / ↓N more) when content overflows, matching the
// TS TUI behavior. It replaces the first/last visible lines with indicators.
func applyViewportWithIndicators(lines []string, start, end, totalLines int) []string {
	if start >= end || totalLines <= 0 {
		return lines
	}

	visible := lines[start:end]

	hasAbove := start > 0
	hasBelow := end < totalLines

	// Replace first/last lines with indicators when there's overflow
	if hasAbove && len(visible) > 0 {
		visible[0] = DimStyle.Render(fmt.Sprintf("  ↑%d more", start))
	}
	if hasBelow && len(visible) > 0 {
		visible[len(visible)-1] = DimStyle.Render(fmt.Sprintf("  ↓%d more", totalLines-end))
	}

	return visible
}
