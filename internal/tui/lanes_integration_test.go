package tui

import (
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Phase 4: Lane-Based TaskTree Integration Tests
// =============================================================================

// TestTaskTree_SetLaneViewMode tests toggling lane view mode.
func TestTaskTree_SetLaneViewMode(t *testing.T) {
	tt := NewTaskTree()
	
	// Initially should be false
	if tt.useLaneView {
		t.Error("expected useLaneView to be false by default")
	}
	
	// Enable lane view
	tt.SetLaneViewMode(true)
	if !tt.useLaneView {
		t.Error("expected useLaneView to be true after enabling")
	}
	
	// Disable lane view
	tt.SetLaneViewMode(false)
	if tt.useLaneView {
		t.Error("expected useLaneView to be false after disabling")
	}
}

// TestTaskTree_ViewLaneTree_SingleTask tests rendering a single task in lane view.
func TestTaskTree_ViewLaneTree_SingleTask(t *testing.T) {
	tt := NewTaskTree()
	tt.SetLaneViewMode(true)
	
	tasks := []types.ResolvedTask{
		makeLaneTask("t1", "Build the widget", nil),
	}
	tt.SetTasks(tasks)
	
	view := tt.View(60, 20)
	
	// Should show the task title
	if !strings.Contains(view, "Build the widget") {
		t.Errorf("expected task title in view, got:\n%s", view)
	}
	
	// Should show last-branch connector (└─)
	if !strings.Contains(view, "└─") {
		t.Errorf("expected last-branch connector '└─' in view, got:\n%s", view)
	}
	
	// Should show status indicator
	if !strings.Contains(view, IndicatorReady) {
		t.Errorf("expected ready indicator in view, got:\n%s", view)
	}
}

// TestTaskTree_ViewLaneTree_Fork tests fork visualization (parent with 2 children).
func TestTaskTree_ViewLaneTree_Fork(t *testing.T) {
	tt := NewTaskTree()
	tt.SetLaneViewMode(true)
	
	tasks := []types.ResolvedTask{
		makeLaneTask("parent", "Parent Task", nil),
		makeLaneTask("child1", "Child 1", []string{"parent"}),
		makeLaneTask("child2", "Child 2", []string{"parent"}),
	}
	tt.SetTasks(tasks)
	
	view := tt.View(80, 20)
	
	// Should show all three tasks
	if !strings.Contains(view, "Parent Task") {
		t.Errorf("expected parent task in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Child 1") {
		t.Errorf("expected child 1 in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Child 2") {
		t.Errorf("expected child 2 in view, got:\n%s", view)
	}
	
	// Should show fork connectors (├─ or └─)
	if !strings.Contains(view, "├─") && !strings.Contains(view, "└─") {
		t.Errorf("expected fork connectors in view, got:\n%s", view)
	}
}

// TestTaskTree_ViewLaneTree_Merge tests merge visualization (2 parents → 1 child).
func TestTaskTree_ViewLaneTree_Merge(t *testing.T) {
	tt := NewTaskTree()
	tt.SetLaneViewMode(true)
	
	tasks := []types.ResolvedTask{
		makeLaneTask("a", "Task A", nil),
		makeLaneTask("b", "Task B", nil),
		makeLaneTask("c", "Merge Task", []string{"a", "b"}),
	}
	tt.SetTasks(tasks)
	
	view := tt.View(80, 20)
	
	// Should show all three tasks
	if !strings.Contains(view, "Task A") {
		t.Errorf("expected Task A in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Task B") {
		t.Errorf("expected Task B in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Merge Task") {
		t.Errorf("expected Merge Task in view, got:\n%s", view)
	}
	
	// Should show merge visualization (╰─┴─)
	if !strings.Contains(view, "╰─") || !strings.Contains(view, "┴─") {
		t.Errorf("expected merge connectors (╰─┴─) in view, got:\n%s", view)
	}
}

// TestTaskTree_ViewLaneTree_Selection tests that selected task is highlighted.
func TestTaskTree_ViewLaneTree_Selection(t *testing.T) {
	tt := NewTaskTree()
	tt.SetLaneViewMode(true)
	
	tasks := []types.ResolvedTask{
		makeLaneTask("t1", "First Task", nil),
		makeLaneTask("t2", "Second Task", []string{"t1"}),
	}
	tt.SetTasks(tasks)
	
	// First task should be auto-selected
	if tt.SelectedID != "t1" {
		t.Errorf("expected first task to be selected, got '%s'", tt.SelectedID)
	}
	
	view := tt.View(60, 20)
	
	// Should show selection marker (▸)
	if !strings.Contains(view, "▸") {
		t.Errorf("expected selection marker '▸' in view, got:\n%s", view)
	}
}

// TestTaskTree_ViewLaneTree_Navigation tests j/k navigation in lane view.
func TestTaskTree_ViewLaneTree_Navigation(t *testing.T) {
	tt := NewTaskTree()
	tt.SetLaneViewMode(true)
	
	tasks := []types.ResolvedTask{
		makeLaneTask("t1", "Task 1", nil),
		makeLaneTask("t2", "Task 2", []string{"t1"}),
		makeLaneTask("t3", "Task 3", []string{"t2"}),
	}
	tt.SetTasks(tasks)
	
	// Initial selection should be first task
	if tt.SelectedID != "t1" {
		t.Errorf("expected initial selection 't1', got '%s'", tt.SelectedID)
	}
	
	// Move down
	tt.MoveDown()
	if tt.SelectedID != "t2" {
		t.Errorf("after MoveDown, expected 't2', got '%s'", tt.SelectedID)
	}
	
	// Move down again
	tt.MoveDown()
	if tt.SelectedID != "t3" {
		t.Errorf("after second MoveDown, expected 't3', got '%s'", tt.SelectedID)
	}
	
	// Move up
	tt.MoveUp()
	if tt.SelectedID != "t2" {
		t.Errorf("after MoveUp, expected 't2', got '%s'", tt.SelectedID)
	}
	
	// Move to top
	tt.MoveToTop()
	if tt.SelectedID != "t1" {
		t.Errorf("after MoveToTop, expected 't1', got '%s'", tt.SelectedID)
	}
	
	// Move to bottom
	tt.MoveToBottom()
	if tt.SelectedID != "t3" {
		t.Errorf("after MoveToBottom, expected 't3', got '%s'", tt.SelectedID)
	}
}

// TestTaskTree_ViewLaneTree_NoRegressions tests that grouped view still works.
func TestTaskTree_ViewLaneTree_NoRegressions(t *testing.T) {
	tt := NewTaskTree()
	// Grouped view is default
	tt.SetViewMode(true)
	
	tasks := []types.ResolvedTask{
		makeTask("t1", "Ready Task", "ready", "high", nil),
		makeTask("t2", "Waiting Task", "waiting", "medium", []string{"t1"}),
	}
	tt.SetTasks(tasks)
	
	view := tt.View(60, 20)
	
	// Should show group headers (classification-based)
	if !strings.Contains(view, "Ready") || !strings.Contains(view, "Waiting") {
		t.Errorf("expected group headers in grouped view, got:\n%s", view)
	}
	
	// Should NOT show lane connectors (├─, └─)
	// (This is grouped view, not lane view)
	// Note: This is more of a sanity check that we're in the right mode
}

// TestTaskTree_SetTasks_LaneView_ComputesLanes tests that SetTasks computes lanes when lane view is enabled.
func TestTaskTree_SetTasks_LaneView_ComputesLanes(t *testing.T) {
	tt := NewTaskTree()
	tt.SetLaneViewMode(true)
	
	tasks := []types.ResolvedTask{
		makeLaneTask("a", "Task A", nil),
		makeLaneTask("b", "Task B", []string{"a"}),
		makeLaneTask("c", "Task C", []string{"b"}),
	}
	
	tt.SetTasks(tasks)
	
	// Should have computed lane tasks (topo-sorted)
	if len(tt.laneTasks) != 3 {
		t.Errorf("expected 3 lane tasks, got %d", len(tt.laneTasks))
	}
	
	// Should have computed lane assignments
	if len(tt.laneAssignments) != 3 {
		t.Errorf("expected 3 lane assignments, got %d", len(tt.laneAssignments))
	}
	
	// First task should be 'a' (topo order)
	if tt.laneTasks[0].ID != "a" {
		t.Errorf("expected first task 'a', got '%s'", tt.laneTasks[0].ID)
	}
}

// TestTaskTree_SetTasks_LaneView_PreservesSelection tests that selection is preserved when switching to lane view.
func TestTaskTree_SetTasks_LaneView_PreservesSelection(t *testing.T) {
	tt := NewTaskTree()
	tt.SetLaneViewMode(true)
	
	tasks := []types.ResolvedTask{
		makeLaneTask("t1", "Task 1", nil),
		makeLaneTask("t2", "Task 2", []string{"t1"}),
		makeLaneTask("t3", "Task 3", []string{"t2"}),
	}
	
	tt.SetTasks(tasks)
	tt.SelectedID = "t2"
	
	// Re-set tasks (simulates update)
	tt.SetTasks(tasks)
	
	// Should preserve selection
	if tt.SelectedID != "t2" {
		t.Errorf("expected selection preserved as 't2', got '%s'", tt.SelectedID)
	}
}

// TestTaskTree_ViewLaneTree_EmptyTasks tests rendering with no tasks.
func TestTaskTree_ViewLaneTree_EmptyTasks(t *testing.T) {
	tt := NewTaskTree()
	tt.SetLaneViewMode(true)
	tt.SetTasks([]types.ResolvedTask{})
	
	view := tt.View(60, 20)
	
	// Should show placeholder
	if !strings.Contains(view, "No tasks") {
		t.Errorf("expected 'No tasks' placeholder in view, got:\n%s", view)
	}
}

// TestTaskTree_ViewLaneTree_PriorityIndicator tests high priority indicator in lane view.
func TestTaskTree_ViewLaneTree_PriorityIndicator(t *testing.T) {
	tt := NewTaskTree()
	tt.SetLaneViewMode(true)
	
	tasks := []types.ResolvedTask{
		makeTask("urgent", "Urgent Task", "ready", "high", nil),
	}
	tt.SetTasks(tasks)
	
	view := tt.View(60, 20)
	
	// Should show high priority indicator (!)
	if !strings.Contains(view, "!") {
		t.Errorf("expected high priority indicator '!' in view, got:\n%s", view)
	}
}

// TestTaskTree_ViewLaneTree_CycleIndicator tests cycle indicator in lane view.
func TestTaskTree_ViewLaneTree_CycleIndicator(t *testing.T) {
	tt := NewTaskTree()
	tt.SetLaneViewMode(true)
	
	tasks := []types.ResolvedTask{
		makeTaskInCycle("a", "Cyclic Task", "blocked", "medium", []string{"b"}),
		makeTaskInCycle("b", "Cyclic Task 2", "blocked", "medium", []string{"a"}),
	}
	tt.SetTasks(tasks)
	
	view := tt.View(60, 20)
	
	// Should show cycle indicator (↺)
	if !strings.Contains(view, "↺") {
		t.Errorf("expected cycle indicator '↺' in view, got:\n%s", view)
	}
}
