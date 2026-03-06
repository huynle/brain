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

	// Multi-select state (passed in during View rendering)
	selectedTasks map[string]bool

	// Lane-based rendering (Phase 4)
	useLaneView     bool
	laneAssignments []LaneAssignment
	laneTasks       []types.ResolvedTask // topo-sorted
}

// NewTaskTree creates a new empty TaskTree component.
func NewTaskTree() TaskTree {
	// Load collapsed state from settings
	settings, _ := LoadSettings()
	return TaskTree{
		useGroupedView:   true, // Enable grouped view by default
		groupCollapsed:   settings.GroupCollapsed,
		featureCollapsed: settings.FeatureCollapsed,
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

			// Restore collapsed state for each feature
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

			// Preserve selection or auto-select first
			tt.selectFirstFeatureTask()
		} else {
			// Classification-based grouping
			// Build groups
			settings, _ := LoadSettings()
			tt.groups = GroupTasks(tasks, settings.GroupVisible)

			// Restore collapsed state for each group
			for i := range tt.groups {
				groupName := tt.groups[i].Name
				if collapsed, ok := tt.groupCollapsed[groupName]; ok {
					tt.groups[i].Collapsed = collapsed
				}
			}

			// Preserve selection if possible
			if tt.SelectedID != "" {
				found := false
				for gIdx, group := range tt.groups {
					for tIdx, task := range group.Tasks {
						if task.ID == tt.SelectedID {
							tt.selectedGroupIdx = gIdx
							tt.selectedTaskIdx = tIdx
							found = true
							break
						}
					}
					if found {
						break
					}
				}
				if !found {
					// Selection lost, default to first task
					tt.selectFirstTask()
				}
			} else {
				// Auto-select first task
				tt.selectFirstTask()
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

	// Find first non-empty group and ALWAYS start on header
	for i, group := range tt.groups {
		if len(group.Tasks) > 0 {
			tt.selectedGroupIdx = i
			tt.selectedTaskIdx = -1 // ALWAYS start on header
			tt.SelectedID = ""
			return
		}
	}

	// No tasks available
	tt.SelectedID = ""
	tt.selectedGroupIdx = 0
	tt.selectedTaskIdx = -1
}

// selectFirstFeatureTask selects the first task in feature view mode.
func (tt *TaskTree) selectFirstFeatureTask() {
	// Try features first
	if len(tt.featureGroups.Features) > 0 {
		tt.selectedFeatureIdx = 0
		tt.selectedFeatureTaskIdx = -1 // Start on header
		tt.isOnUngrouped = false
		tt.SelectedID = ""
		return
	}

	// Fall back to ungrouped if no features
	if tt.featureGroups.Ungrouped != nil && len(tt.featureGroups.Ungrouped.Tasks) > 0 {
		tt.selectedFeatureIdx = -1
		tt.selectedFeatureTaskIdx = -1 // Start on header
		tt.isOnUngrouped = true
		tt.SelectedID = ""
		return
	}

	// No tasks available
	tt.SelectedID = ""
	tt.selectedFeatureIdx = -1
	tt.selectedFeatureTaskIdx = -1
	tt.isOnUngrouped = false
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
	if len(tt.groups) == 0 {
		return
	}

	group := tt.groups[tt.selectedGroupIdx]

	if tt.selectedTaskIdx == -1 {
		// On group header
		if group.Collapsed {
			// Group is collapsed, jump to next group header
			if tt.selectedGroupIdx < len(tt.groups)-1 {
				tt.selectedGroupIdx++
				tt.selectedTaskIdx = -1
				tt.SelectedID = ""
			} else {
				// No next group - expand current group and enter it
				if len(group.Tasks) > 0 {
					tt.groups[tt.selectedGroupIdx].Collapsed = false
					tt.groupCollapsed[group.Name] = false
					tt.selectedTaskIdx = 0
					tt.SelectedID = group.Tasks[0].ID
				}
			}
		} else {
			// Group is expanded, enter group (move to first task)
			if len(group.Tasks) > 0 {
				tt.selectedTaskIdx = 0
				tt.SelectedID = group.Tasks[0].ID
			}
		}
	} else {
		// Within group
		if tt.selectedTaskIdx < len(group.Tasks)-1 {
			// Move to next task in group
			tt.selectedTaskIdx++
			tt.SelectedID = group.Tasks[tt.selectedTaskIdx].ID
		} else {
			// End of group, move to next group HEADER
			if tt.selectedGroupIdx < len(tt.groups)-1 {
				tt.selectedGroupIdx++
				tt.selectedTaskIdx = -1 // Land on header
				tt.SelectedID = ""
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
	if len(tt.groups) == 0 {
		return
	}

	if tt.selectedTaskIdx == -1 {
		// On group header, move to previous group
		if tt.selectedGroupIdx > 0 {
			tt.selectedGroupIdx--
			prevGroup := tt.groups[tt.selectedGroupIdx]
			// Land on last task of previous group if expanded
			if !prevGroup.Collapsed && len(prevGroup.Tasks) > 0 {
				tt.selectedTaskIdx = len(prevGroup.Tasks) - 1
				tt.SelectedID = prevGroup.Tasks[tt.selectedTaskIdx].ID
			} else {
				// Stay on group header
				tt.selectedTaskIdx = -1
				tt.SelectedID = ""
			}
		}
	} else {
		// Within group
		if tt.selectedTaskIdx > 0 {
			// Move to previous task
			tt.selectedTaskIdx--
			tt.SelectedID = tt.groups[tt.selectedGroupIdx].Tasks[tt.selectedTaskIdx].ID
		} else {
			// At top of group, move to group header
			tt.selectedTaskIdx = -1
			tt.SelectedID = ""
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
		settings := Settings{
			GroupCollapsed:   tt.groupCollapsed,
			FeatureCollapsed: tt.featureCollapsed,
		}
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
	settings := Settings{
		GroupCollapsed:   tt.groupCollapsed,
		FeatureCollapsed: tt.featureCollapsed,
	}
	_ = SaveSettings(settings) // Ignore errors (non-critical)
}

// IsOnGroupHeader returns true if the cursor is on a group header.
func (tt *TaskTree) IsOnGroupHeader() bool {
	if !tt.useGroupedView || len(tt.groups) == 0 {
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

// statusIndicator returns the status icon for a task classification.
func statusIndicator(classification string) string {
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
func (tt *TaskTree) View(width, height int) string {
	// Check lane view first (takes precedence over grouped view)
	if tt.useLaneView {
		return tt.viewLaneTree(width, height)
	}

	if tt.useGroupedView {
		if tt.useFeatureView {
			return tt.viewFeatureGrouped(width, height)
		}
		return tt.viewGrouped(width, height)
	}

	return tt.viewLegacy(width, height)
}

// ViewWithSelection renders the task tree with multi-select checkboxes.
func (tt *TaskTree) ViewWithSelection(width, height int, selectedTasks map[string]bool) string {
	// Store selection for rendering
	tt.selectedTasks = selectedTasks
	return tt.View(width, height)
}

// viewLegacy is the original tree-based rendering.
func (tt *TaskTree) viewLegacy(width, height int) string {
	if len(tt.nodes) == 0 {
		return DimStyle.Render("  No tasks")
	}

	var lines []string
	tt.renderNodes(tt.nodes, "", &lines)

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
func (tt *TaskTree) viewGrouped(width, height int) string {
	if len(tt.groups) == 0 {
		return DimStyle.Render("  No tasks")
	}

	var lines []string

	// Compute whether to show checkboxes (only when multi-select is active)
	showCheckboxes := len(tt.selectedTasks) > 0

	for gIdx, group := range tt.groups {
		// Render group header
		isGroupSelected := (gIdx == tt.selectedGroupIdx && tt.selectedTaskIdx == -1)

		// Collapse indicator (▸ collapsed, ▾ expanded)
		collapseIndicator := "▸"
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
			for tIdx, task := range group.Tasks {
				isTaskSelected := (gIdx == tt.selectedGroupIdx && tIdx == tt.selectedTaskIdx)
				taskLine := tt.renderGroupedTaskLine(task, isTaskSelected, tt.selectedTasks, showCheckboxes)
				lines = append(lines, taskLine)
			}
		}
	}

	// Truncate to height (simple approach - can be improved with smart scrolling)
	if height > 0 && len(lines) > height {
		// For now, just truncate from the start
		if len(lines) > height {
			lines = lines[:height]
		}
	}

	return strings.Join(lines, "\n")
}

// renderGroupedTaskLine renders a single task line in grouped view.
func (tt *TaskTree) renderGroupedTaskLine(task types.ResolvedTask, isSelected bool, selectedTasks map[string]bool, showCheckboxes bool) string {
	// Selection marker
	selMarker := "  "
	if isSelected {
		selMarker = lipgloss.NewStyle().Foreground(ColorCyan).Render("▸ ")
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
	indicator := statusIndicator(task.Classification)
	indicatorStyled := StatusStyle(task.Classification).Render(indicator)

	// Title
	title := task.Title
	if isSelected {
		title = lipgloss.NewStyle().Bold(true).Foreground(ColorWhite).Render(title)
	} else if selectedTasks[task.ID] {
		// Apply selection style to selected tasks even when not focused
		title = SelectedTaskStyle.Render(title)
	}

	// Priority suffix
	prioritySuffix := ""
	if task.Priority == "high" {
		prioritySuffix = lipgloss.NewStyle().Foreground(ColorPriorityHigh).Bold(true).Render("!")
	}

	return fmt.Sprintf("%s%s%s %s%s", selMarker, checkboxPart, indicatorStyled, title, prioritySuffix)
}

// viewFeatureGrouped renders tasks in feature-grouped view.
func (tt *TaskTree) viewFeatureGrouped(width, height int) string {
	if len(tt.featureGroups.Features) == 0 && tt.featureGroups.Ungrouped == nil {
		return DimStyle.Render("  No tasks")
	}

	var lines []string
	showCheckboxes := len(tt.selectedTasks) > 0

	// Render features
	for fIdx, feature := range tt.featureGroups.Features {
		isFeatureSelected := (fIdx == tt.selectedFeatureIdx && tt.selectedFeatureTaskIdx == -1 && !tt.isOnUngrouped)

		// Collapse indicator
		collapseIndicator := "▸"
		if !feature.Collapsed {
			collapseIndicator = "▾"
		}

		// Feature header with count and stats
		featureHeader := fmt.Sprintf("%s %s (%d) [%d/%d]", collapseIndicator, feature.Name, feature.Stats.Total, feature.Stats.Completed, feature.Stats.Total)

		// Selection marker
		if isFeatureSelected {
			featureHeader = GroupHeaderStyle.Render(featureHeader)
			featureHeader = fmt.Sprintf("→ %s", featureHeader)
		} else {
			featureHeader = GroupHeaderStyle.Render(featureHeader)
			featureHeader = fmt.Sprintf("  %s", featureHeader)
		}

		lines = append(lines, featureHeader)

		// Render tasks if not collapsed
		if !feature.Collapsed {
			for tIdx, task := range feature.Tasks {
				isTaskSelected := (fIdx == tt.selectedFeatureIdx && tIdx == tt.selectedFeatureTaskIdx && !tt.isOnUngrouped)
				taskLine := tt.renderGroupedTaskLine(task, isTaskSelected, tt.selectedTasks, showCheckboxes)
				lines = append(lines, taskLine)
			}
		}
	}

	// Render ungrouped if present
	if tt.featureGroups.Ungrouped != nil {
		ungrouped := tt.featureGroups.Ungrouped
		isUngroupedSelected := (tt.isOnUngrouped && tt.selectedFeatureTaskIdx == -1)

		collapseIndicator := "▸"
		if !ungrouped.Collapsed {
			collapseIndicator = "▾"
		}

		ungroupedHeader := fmt.Sprintf("%s %s (%d)", collapseIndicator, ungrouped.Name, len(ungrouped.Tasks))

		if isUngroupedSelected {
			ungroupedHeader = GroupHeaderStyle.Render(ungroupedHeader)
			ungroupedHeader = fmt.Sprintf("→ %s", ungroupedHeader)
		} else {
			ungroupedHeader = GroupHeaderStyle.Render(ungroupedHeader)
			ungroupedHeader = fmt.Sprintf("  %s", ungroupedHeader)
		}

		lines = append(lines, ungroupedHeader)

		if !ungrouped.Collapsed {
			for tIdx, task := range ungrouped.Tasks {
				isTaskSelected := (tt.isOnUngrouped && tIdx == tt.selectedFeatureTaskIdx)
				taskLine := tt.renderGroupedTaskLine(task, isTaskSelected, tt.selectedTasks, showCheckboxes)
				lines = append(lines, taskLine)
			}
		}
	}

	// Truncate to height
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}

	return strings.Join(lines, "\n")
}

// renderNodes recursively renders tree nodes into lines with proper box-drawing indentation.
// The prefix parameter tracks ancestor line states to render vertical continuation lines correctly.
func (tt *TaskTree) renderNodes(nodes []TreeNode, prefix string, lines *[]string) {
	for i, node := range nodes {
		isLast := i == len(nodes)-1

		// Build the line with tree prefix
		line := tt.renderTaskLine(node, prefix, isLast)
		*lines = append(*lines, line)

		// Render children with updated prefix for vertical continuation
		if len(node.Children) > 0 {
			childPrefix := tt.calculateChildPrefix(prefix, isLast)
			tt.renderNodes(node.Children, childPrefix, lines)
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
func (tt *TaskTree) renderTaskLine(node TreeNode, prefix string, isLast bool) string {
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
	indicator := statusIndicator(task.Classification)
	indicatorStyled := StatusStyle(task.Classification).Render(indicator)

	// Title
	title := task.Title
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
	if node.InCycle {
		cycleSuffix = lipgloss.NewStyle().Foreground(ColorMagenta).Render(" ↺")
	}

	// Selection marker
	selMarker := " "
	if isSelected {
		selMarker = lipgloss.NewStyle().Foreground(ColorCyan).Render("▸")
	}

	return fmt.Sprintf("%s%s%s%s %s%s%s", selMarker, prefix, treeConnector, indicatorStyled, title, prioritySuffix, cycleSuffix)
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
		line := tt.renderLaneTaskLine(task, assignment, i, isSelected)
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

		// Calculate viewport start to keep selected task visible
		start := 0
		if selectedIdx >= height {
			start = selectedIdx - height + 1
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
func (tt *TaskTree) renderLaneTaskLine(task types.ResolvedTask, assignment LaneAssignment, index int, isSelected bool) string {
	// Generate lane prefix
	prefix := GeneratePrefix(assignment, index, tt.laneAssignments, nil)

	// Status indicator with color
	indicator := statusIndicator(task.Classification)
	indicatorStyled := StatusStyle(task.Classification).Render(indicator)

	// Title with selection
	title := task.Title
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
	selMarker := " "
	if isSelected {
		selMarker = lipgloss.NewStyle().Foreground(ColorCyan).Render("▸")
	}

	return fmt.Sprintf("%s%s %s %s%s%s", selMarker, prefix, indicatorStyled, title, prioritySuffix, cycleSuffix)
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
