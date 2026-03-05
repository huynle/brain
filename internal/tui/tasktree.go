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

// BuildTree builds a tree structure from a flat task list using DependsOn relationships.
// It detects cycles and handles diamond dependencies (each task rendered once).
// Tasks are sorted by priority (high > medium > low) then by status.
func BuildTree(tasks []types.ResolvedTask) []TreeNode {
	if len(tasks) == 0 {
		return nil
	}

	// Build lookup map
	taskMap := make(map[string]types.ResolvedTask, len(tasks))
	for _, t := range tasks {
		taskMap[t.ID] = t
	}

	// Build reverse dependency map: parent -> children
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

	// Detect cycles using DFS
	inCycle := make(map[string]bool)
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var detectCycles func(id string) bool
	detectCycles = func(id string) bool {
		visited[id] = true
		recStack[id] = true

		for _, childID := range children[id] {
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

	// Sort children by priority then status
	sortIDs := func(ids []string) {
		sort.Slice(ids, func(i, j int) bool {
			a := taskMap[ids[i]]
			b := taskMap[ids[j]]
			pa := priorityOrder[a.Priority]
			pb := priorityOrder[b.Priority]
			if pa != pb {
				return pa < pb
			}
			return statusOrder[a.Status] < statusOrder[b.Status]
		})
	}

	// Track rendered tasks for diamond dedup
	rendered := make(map[string]bool)

	// Build tree recursively
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

		childIDs := children[id]
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

	// Collect roots (tasks with no parent in the tree)
	var roots []TreeNode
	for _, t := range sorted {
		if !hasParent[t.ID] {
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

// FlattenTreeOrder flattens the tree into a list of task IDs in visual/navigation order.
// Parent appears before children, depth-first traversal.
func FlattenTreeOrder(tasks []types.ResolvedTask) []string {
	if len(tasks) == 0 {
		return nil
	}

	nodes := BuildTree(tasks)
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

	nodes []TreeNode
	order []string // flattened navigation order (task IDs)
	tasks []types.ResolvedTask
}

// NewTaskTree creates a new empty TaskTree component.
func NewTaskTree() TaskTree {
	return TaskTree{}
}

// SetTasks updates the task list, rebuilds the tree, and preserves selection if possible.
func (tt *TaskTree) SetTasks(tasks []types.ResolvedTask) {
	tt.tasks = tasks
	tt.nodes = BuildTree(tasks)
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

// MoveDown moves the cursor down one position.
func (tt *TaskTree) MoveDown() {
	if len(tt.order) == 0 {
		return
	}
	if tt.Cursor < len(tt.order)-1 {
		tt.Cursor++
		tt.SelectedID = tt.order[tt.Cursor]
	}
}

// MoveUp moves the cursor up one position.
func (tt *TaskTree) MoveUp() {
	if len(tt.order) == 0 {
		return
	}
	if tt.Cursor > 0 {
		tt.Cursor--
		tt.SelectedID = tt.order[tt.Cursor]
	}
}

// MoveToTop moves the cursor to the first task.
func (tt *TaskTree) MoveToTop() {
	if len(tt.order) == 0 {
		return
	}
	tt.Cursor = 0
	tt.SelectedID = tt.order[0]
}

// MoveToBottom moves the cursor to the last task.
func (tt *TaskTree) MoveToBottom() {
	if len(tt.order) == 0 {
		return
	}
	tt.Cursor = len(tt.order) - 1
	tt.SelectedID = tt.order[tt.Cursor]
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

// renderNodes recursively renders tree nodes into lines.
func (tt *TaskTree) renderNodes(nodes []TreeNode, prefix string, lines *[]string) {
	for i, node := range nodes {
		isLast := i == len(nodes)-1

		// Determine branch character
		branch := treeBranch
		if isLast {
			branch = treeLastBranch
		}

		// Only add prefix+branch for non-root nodes
		linePrefix := ""
		if prefix != "" || len(nodes) > 0 {
			if prefix == "" {
				// Root level: no branch prefix
				linePrefix = ""
			} else {
				linePrefix = prefix + branch
			}
		}

		// Build the line
		line := tt.renderTaskLine(node, linePrefix)
		*lines = append(*lines, line)

		// Render children with appropriate prefix
		if len(node.Children) > 0 {
			childPrefix := prefix
			if prefix != "" {
				if isLast {
					childPrefix = prefix + treeEmpty
				} else {
					childPrefix = prefix + treeVertical
				}
			} else {
				// Children of root get indentation
				childPrefix = "  "
			}
			tt.renderNodes(node.Children, childPrefix, lines)
		}
	}
}

// renderTaskLine renders a single task line with status, title, and indicators.
func (tt *TaskTree) renderTaskLine(node TreeNode, prefix string) string {
	task := node.Task
	isSelected := task.ID == tt.SelectedID

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

	return fmt.Sprintf("%s%s%s %s%s%s", selMarker, prefix, indicatorStyled, title, prioritySuffix, cycleSuffix)
}
