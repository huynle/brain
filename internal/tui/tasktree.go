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

	// Hybrid view: Status groups for draft/completed within feature view
	draftCollapsed     bool
	completedCollapsed bool

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
		useGroupedView:     true, // Enable grouped view by default
		useFeatureView:     true, // Use feature-grouped view by default (matches origin/main)
		groupCollapsed:     settings.GroupCollapsed,
		featureCollapsed:   featureCollapsed,
		selectedStatusIdx:  0,
		selectedFeatureIdx: -2,    // -2 means "none" (on status header)
		selectedTaskIdx:    -1,    // -1 means on header
		isOnStatusHeader:   true,  // Start on status header in nested mode
		draftCollapsed:     false, // Draft section expanded by default
		completedCollapsed: true,  // Completed section collapsed by default
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

			// Preserve selection across SSE updates if the selected task still exists
			if tt.SelectedID != "" {
				if tt.restoreFeatureSelection(tt.SelectedID) {
					// Selection preserved — skip auto-select
				} else {
					// Selected task no longer exists — fall back to first active task
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
		for j, task := range feature.Tasks {
			if task.ID == previousID {
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
		for j, task := range tt.featureGroups.Ungrouped.Tasks {
			if task.ID == previousID {
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

func (tt *TaskTree) selectFirstFeatureTask() {
	// Helper to check if a task is active (not draft/completed)
	isActiveStatus := func(status string) bool {
		switch status {
		case "draft", "completed", "validated", "cancelled", "superseded", "archived":
			return false
		default:
			return true
		}
	}

	// Try features first - select first ACTIVE task
	if len(tt.featureGroups.Features) > 0 {
		for i, feature := range tt.featureGroups.Features {
			// Find first active task in this feature
			for j, task := range feature.Tasks {
				if isActiveStatus(task.Status) {
					tt.selectedFeatureIdx = i
					tt.selectedFeatureTaskIdx = j
					tt.isOnUngrouped = false
					tt.SelectedID = task.ID
					return
				}
			}
		}
	}

	// Fall back to ungrouped if no active tasks in features
	if tt.featureGroups.Ungrouped != nil && len(tt.featureGroups.Ungrouped.Tasks) > 0 {
		for j, task := range tt.featureGroups.Ungrouped.Tasks {
			if isActiveStatus(task.Status) {
				tt.selectedFeatureIdx = -1
				tt.selectedFeatureTaskIdx = j
				tt.isOnUngrouped = true
				tt.SelectedID = task.ID
				return
			}
		}
	}

	// No active tasks - fall back to first draft task
	if len(tt.featureGroups.Features) > 0 {
		for i, feature := range tt.featureGroups.Features {
			for j, task := range feature.Tasks {
				if task.Status == "draft" {
					tt.selectedFeatureIdx = i
					tt.selectedFeatureTaskIdx = j
					tt.isOnUngrouped = false
					tt.SelectedID = task.ID
					return
				}
			}
		}
	}

	// Fall back to draft in ungrouped
	if tt.featureGroups.Ungrouped != nil {
		for j, task := range tt.featureGroups.Ungrouped.Tasks {
			if task.Status == "draft" {
				tt.selectedFeatureIdx = -1
				tt.selectedFeatureTaskIdx = j
				tt.isOnUngrouped = true
				tt.SelectedID = task.ID
				return
			}
		}
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

	// Check bounds
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

	// Truncate to height
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
		lines = lines[start:end]
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
		lines = lines[start:end]
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

	// Split tasks into active, draft, and completed/validated
	// Active tasks: pending, in_progress, blocked, ready, waiting, active
	// Draft tasks: draft status
	// Completed tasks: completed, validated, cancelled, superseded, archived
	var draftTasks, completedTasks []types.ResolvedTask
	activeFeatureGroups := make([]FeatureGroup, 0)
	var activeUngrouped *FeatureGroup

	// Process features and split by status
	for _, feature := range tt.featureGroups.Features {
		var activeTasks []types.ResolvedTask
		for _, task := range feature.Tasks {
			switch task.Status {
			case "draft":
				draftTasks = append(draftTasks, task)
			case "completed", "validated", "cancelled", "superseded", "archived":
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
			case "completed", "validated", "cancelled", "superseded", "archived":
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

	// Render active features
	for fIdx, feature := range activeFeatureGroups {
		isFeatureSelected := (fIdx == tt.selectedFeatureIdx && tt.selectedFeatureTaskIdx == -1 && !tt.isOnUngrouped)

		// Collapse indicator
		collapseIndicator := "▶"
		if !feature.Collapsed {
			collapseIndicator = "▾"
		}

		// Feature header with count and stats
		featureHeader := fmt.Sprintf("%s Feature: %s [%d]", collapseIndicator, feature.Name, feature.Stats.Total)

		// Selection marker (2 spaces for alignment)
		selMarker := "  "

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
				tt.selectedFeatureIdx, // Feature-only view uses selectedFeatureIdx
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

		// Selection marker (2 spaces for alignment)
		selMarker := "  "

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
			)
		}
	}

	// Render Draft status group
	if len(draftTasks) > 0 {
		// Add blank line before Draft section
		lines = append(lines, "")

		collapseIndicator := "▶"
		if !tt.draftCollapsed {
			collapseIndicator = "▾"
		}

		draftHeader := fmt.Sprintf("%s Draft (%d)", collapseIndicator, len(draftTasks))
		draftHeader = DraftHeaderStyle.Render(draftHeader)
		draftHeader = fmt.Sprintf("  %s", draftHeader)
		lines = append(lines, draftHeader)

		if !tt.draftCollapsed {
			// Group draft tasks by feature_id
			draftByFeature := make(map[string][]types.ResolvedTask)
			for _, task := range draftTasks {
				featureID := task.FeatureID
				if featureID == "" {
					featureID = "[Ungrouped]"
				}
				draftByFeature[featureID] = append(draftByFeature[featureID], task)
			}

			// Sort feature IDs for consistent ordering
			var featureIDs []string
			for fid := range draftByFeature {
				featureIDs = append(featureIDs, fid)
			}
			sort.Strings(featureIDs)

			// Render each feature group under Draft
			for _, featureID := range featureIDs {
				featureTasks := draftByFeature[featureID]

				// Render dimmed feature header (skip for [Ungrouped])
				if featureID != "[Ungrouped]" {
					featureHeader := fmt.Sprintf("  • Feature: %s [%d]", featureID, len(featureTasks))
					lines = append(lines, DimStyle.Render(featureHeader))
				}

				// Build dependency tree for this feature's tasks
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
				)
			}
		}
	}

	// Render Completed status group
	if len(completedTasks) > 0 {
		// Add blank line before Completed section
		lines = append(lines, "")

		collapseIndicator := "▶"
		if !tt.completedCollapsed {
			collapseIndicator = "▾"
		}

		completedHeader := fmt.Sprintf("%s Completed (%d)", collapseIndicator, len(completedTasks))
		completedHeader = CompletedHeaderStyle.Render(completedHeader)
		completedHeader = fmt.Sprintf("  %s", completedHeader)
		lines = append(lines, completedHeader)

		if !tt.completedCollapsed {
			// Group completed tasks by feature_id
			completedByFeature := make(map[string][]types.ResolvedTask)
			for _, task := range completedTasks {
				featureID := task.FeatureID
				if featureID == "" {
					featureID = "[Ungrouped]"
				}
				completedByFeature[featureID] = append(completedByFeature[featureID], task)
			}

			// Sort feature IDs for consistent ordering
			var featureIDs []string
			for fid := range completedByFeature {
				featureIDs = append(featureIDs, fid)
			}
			sort.Strings(featureIDs)

			// Render each feature group under Completed
			for _, featureID := range featureIDs {
				featureTasks := completedByFeature[featureID]

				// Render dimmed feature header (skip for [Ungrouped])
				if featureID != "[Ungrouped]" {
					featureHeader := fmt.Sprintf("  • Feature: %s [%d]", featureID, len(featureTasks))
					lines = append(lines, DimStyle.Render(featureHeader))
				}

				// Build dependency tree for this feature's tasks
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
				)
			}
		}
	}

	// Handle viewport scrolling (ensure selected item is visible)
	if height > 0 && len(lines) > height {
		// Find the line index of the selected item
		selectedLineIdx := 0
		lineIdx := 0

		// Check feature groups
		for fIdx, feature := range tt.featureGroups.Features {
			// Check if feature header is selected
			if fIdx == tt.selectedFeatureIdx && tt.selectedFeatureTaskIdx == -1 && !tt.isOnUngrouped {
				selectedLineIdx = lineIdx
				break
			}
			lineIdx++ // feature header line

			// Check tasks in this feature if not collapsed
			if !feature.Collapsed {
				taskLineCount := countGroupTaskLines(BuildTree(feature.Tasks, tt.tasks))

				// If selected task is in this feature
				if fIdx == tt.selectedFeatureIdx && tt.selectedFeatureTaskIdx >= 0 && !tt.isOnUngrouped {
					selectedLineIdx = lineIdx + tt.selectedFeatureTaskIdx
					break
				}
				lineIdx += taskLineCount
			}
		}

		// Check ungrouped section
		if tt.featureGroups.Ungrouped != nil {
			ungrouped := tt.featureGroups.Ungrouped

			// Check if ungrouped header is selected
			if tt.isOnUngrouped && tt.selectedFeatureTaskIdx == -1 {
				selectedLineIdx = lineIdx
			} else if !ungrouped.Collapsed && tt.isOnUngrouped && tt.selectedFeatureTaskIdx >= 0 {
				// Selected task is in ungrouped section
				selectedLineIdx = lineIdx + 1 + tt.selectedFeatureTaskIdx
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
		lines = lines[start:end]
	}

	return strings.Join(lines, "\n")
}

// viewNestedGrouped renders tasks in 3-level nested hierarchy: Status → Feature → Tasks
// This is Phase 4 of nested grouping implementation.
func (tt *TaskTree) viewNestedGrouped(width, height int, activeProjectID string) string {
	if len(tt.statusGroups) == 0 {
		return DimStyle.Render("  No tasks")
	}

	var lines []string
	showCheckboxes := len(tt.selectedTasks) > 0

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

				// Feature header with indentation
				featureHeader := fmt.Sprintf("%s Feature: %s [%d]",
					featureCollapseIndicator, feature.Name, feature.Stats.Total)

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
		lines = lines[start:end]
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
) {

	for i, node := range nodes {
		isLast := i == len(nodes)-1

		// Check if this task is selected
		// In nested view: only mark as selected if we're actually on a task (not on a header)
		isSelected := (node.Task.ID == tt.SelectedID && tt.selectedTaskIdx >= 0)

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
func (tt *TaskTree) renderGroupedTaskLineWithTree(
	node TreeNode,
	prefix string,
	isLast bool,
	isSelected bool,
	selectedTasks map[string]bool,
	showCheckboxes bool,
	activeProjectID string,
	width int,
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

	// Not selected - apply default styling
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
		lines = lines[start:end]
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

// moveDownFeatureGrouped handles 2-level feature view navigation (Features → Tasks).
func (tt *TaskTree) moveDownFeatureGrouped() {
	// Handle ungrouped group
	if tt.isOnUngrouped {
		if tt.featureGroups.Ungrouped == nil {
			return
		}
		ungrouped := tt.featureGroups.Ungrouped

		if tt.selectedFeatureTaskIdx == -1 {
			// On ungrouped header
			if ungrouped.Collapsed {
				// Collapsed → nowhere to go (end of list)
				return
			} else {
				// Expanded → move to first task
				if len(ungrouped.Tasks) > 0 {
					treeOrder := FlattenTreeOrder(ungrouped.Tasks)
					if len(treeOrder) > 0 {
						tt.selectedFeatureTaskIdx = 0
						tt.SelectedID = treeOrder[0]
					}
				}
			}
		} else {
			// Within ungrouped tasks
			treeOrder := FlattenTreeOrder(ungrouped.Tasks)
			if tt.selectedFeatureTaskIdx < len(treeOrder)-1 {
				tt.selectedFeatureTaskIdx++
				tt.SelectedID = treeOrder[tt.selectedFeatureTaskIdx]
			}
			// At end of ungrouped → nowhere to go (end of list)
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
				// Jump to ungrouped if it exists
				if tt.featureGroups.Ungrouped != nil {
					tt.selectedFeatureIdx = -1
					tt.selectedFeatureTaskIdx = -1
					tt.isOnUngrouped = true
					tt.SelectedID = ""
				}
			}
		} else {
			// Rule 2: On FEATURE HEADER, EXPANDED → Move to first task
			if len(feature.Tasks) > 0 {
				treeOrder := FlattenTreeOrder(feature.Tasks)
				if len(treeOrder) > 0 {
					tt.selectedFeatureTaskIdx = 0
					tt.SelectedID = treeOrder[0]
				}
			}
		}
	} else {
		// Within feature tasks
		treeOrder := FlattenTreeOrder(feature.Tasks)

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
				// Jump to ungrouped if it exists
				if tt.featureGroups.Ungrouped != nil {
					tt.selectedFeatureIdx = -1
					tt.selectedFeatureTaskIdx = -1
					tt.isOnUngrouped = true
					tt.SelectedID = ""
				}
			}
		}
	}
}

// moveUpFeatureGrouped handles upward 2-level feature view navigation (Features → Tasks).
func (tt *TaskTree) moveUpFeatureGrouped() {
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
					treeOrder := FlattenTreeOrder(lastFeature.Tasks)
					tt.selectedFeatureTaskIdx = len(treeOrder) - 1
					tt.SelectedID = treeOrder[tt.selectedFeatureTaskIdx]
				} else {
					// Land on feature header
					tt.selectedFeatureTaskIdx = -1
					tt.SelectedID = ""
				}
			}
		} else {
			// Within ungrouped tasks
			if tt.selectedFeatureTaskIdx > 0 {
				treeOrder := FlattenTreeOrder(ungrouped.Tasks)
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
				treeOrder := FlattenTreeOrder(prevFeature.Tasks)
				tt.selectedFeatureTaskIdx = len(treeOrder) - 1
				tt.SelectedID = treeOrder[tt.selectedFeatureTaskIdx]
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
			treeOrder := FlattenTreeOrder(feature.Tasks)
			tt.selectedFeatureTaskIdx--
			tt.SelectedID = treeOrder[tt.selectedFeatureTaskIdx]
		} else {
			// Move to feature header
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
