package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Helper: make a minimal ResolvedTask
// =============================================================================

func makeTaskTree() TaskTree {
	tt := NewTaskTree()
	// Use legacy tree view for most tests (backward compatibility)
	tt.SetViewMode(false)
	return tt
}

func makeTask(id, title, classification, priority string, dependsOn []string) types.ResolvedTask {
	return types.ResolvedTask{
		ID:             id,
		Title:          title,
		Classification: classification,
		Priority:       priority,
		Status:         "pending",
		DependsOn:      dependsOn,
	}
}

func makeTaskWithStatus(id, title, classification, priority, status string, dependsOn []string) types.ResolvedTask {
	t := makeTask(id, title, classification, priority, dependsOn)
	t.Status = status
	return t
}

func makeTaskWithFeature(id, title, classification, priority, featureID string, dependsOn []string) types.ResolvedTask {
	t := makeTask(id, title, classification, priority, dependsOn)
	t.FeatureID = featureID
	return t
}

func makeTaskInCycle(id, title, classification, priority string, dependsOn []string) types.ResolvedTask {
	t := makeTask(id, title, classification, priority, dependsOn)
	t.InCycle = true
	return t
}

func makeTaskWithParentAndDeps(id, title, classification, priority, parentID string, dependsOn []string) types.ResolvedTask {
	t := makeTask(id, title, classification, priority, dependsOn)
	t.ParentID = parentID
	return t
}

// =============================================================================
// BuildTree Tests
// =============================================================================

func TestBuildTree_EmptyTasks(t *testing.T) {
	nodes := BuildTree(nil, []types.ResolvedTask{})
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes for nil tasks, got %d", len(nodes))
	}

	nodes = BuildTree([]types.ResolvedTask{}, []types.ResolvedTask{})
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes for empty tasks, got %d", len(nodes))
	}
}

func TestBuildTree_SingleTask(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeTask("t1", "Task 1", "ready", "medium", nil),
	}
	nodes := BuildTree(tasks, []types.ResolvedTask{})

	if len(nodes) != 1 {
		t.Fatalf("expected 1 root node, got %d", len(nodes))
	}
	if nodes[0].Task.ID != "t1" {
		t.Errorf("expected root task ID 't1', got '%s'", nodes[0].Task.ID)
	}
	if len(nodes[0].Children) != 0 {
		t.Errorf("expected 0 children, got %d", len(nodes[0].Children))
	}
}

func TestBuildTree_ParentChild(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeTask("parent", "Parent Task", "ready", "high", nil),
		makeTask("child", "Child Task", "waiting", "medium", []string{"parent"}),
	}
	nodes := BuildTree(tasks, []types.ResolvedTask{})

	if len(nodes) != 1 {
		t.Fatalf("expected 1 root node, got %d", len(nodes))
	}
	if nodes[0].Task.ID != "parent" {
		t.Errorf("expected root 'parent', got '%s'", nodes[0].Task.ID)
	}
	if len(nodes[0].Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(nodes[0].Children))
	}
	if nodes[0].Children[0].Task.ID != "child" {
		t.Errorf("expected child 'child', got '%s'", nodes[0].Children[0].Task.ID)
	}
}

func TestBuildTree_MultipleRoots(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeTask("a", "Task A", "ready", "high", nil),
		makeTask("b", "Task B", "ready", "medium", nil),
		makeTask("c", "Task C", "ready", "low", nil),
	}
	nodes := BuildTree(tasks, []types.ResolvedTask{})

	if len(nodes) != 3 {
		t.Fatalf("expected 3 root nodes, got %d", len(nodes))
	}
	// Should be sorted by priority: high, medium, low
	if nodes[0].Task.ID != "a" {
		t.Errorf("expected first root 'a' (high priority), got '%s'", nodes[0].Task.ID)
	}
	if nodes[1].Task.ID != "b" {
		t.Errorf("expected second root 'b' (medium priority), got '%s'", nodes[1].Task.ID)
	}
	if nodes[2].Task.ID != "c" {
		t.Errorf("expected third root 'c' (low priority), got '%s'", nodes[2].Task.ID)
	}
}

func TestBuildTree_DeepHierarchy(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeTask("root", "Root", "ready", "high", nil),
		makeTask("mid", "Middle", "waiting", "medium", []string{"root"}),
		makeTask("leaf", "Leaf", "waiting", "low", []string{"mid"}),
	}
	nodes := BuildTree(tasks, []types.ResolvedTask{})

	if len(nodes) != 1 {
		t.Fatalf("expected 1 root, got %d", len(nodes))
	}
	if len(nodes[0].Children) != 1 {
		t.Fatalf("expected 1 child of root, got %d", len(nodes[0].Children))
	}
	mid := nodes[0].Children[0]
	if mid.Task.ID != "mid" {
		t.Errorf("expected mid 'mid', got '%s'", mid.Task.ID)
	}
	if len(mid.Children) != 1 {
		t.Fatalf("expected 1 child of mid, got %d", len(mid.Children))
	}
	if mid.Children[0].Task.ID != "leaf" {
		t.Errorf("expected leaf 'leaf', got '%s'", mid.Children[0].Task.ID)
	}
}

func TestBuildTree_CycleDetection(t *testing.T) {
	// A -> B -> A (cycle)
	tasks := []types.ResolvedTask{
		makeTask("a", "Task A", "ready", "medium", []string{"b"}),
		makeTask("b", "Task B", "ready", "medium", []string{"a"}),
	}
	nodes := BuildTree(tasks, []types.ResolvedTask{})

	// Both should be marked as in cycle
	foundA, foundB := false, false
	for _, n := range nodes {
		if n.Task.ID == "a" {
			foundA = true
			if !n.InCycle {
				t.Error("expected task 'a' to be marked InCycle")
			}
		}
		if n.Task.ID == "b" {
			foundB = true
			if !n.InCycle {
				t.Error("expected task 'b' to be marked InCycle")
			}
		}
	}
	if !foundA {
		t.Error("expected to find task 'a' in nodes")
	}
	if !foundB {
		t.Error("expected to find task 'b' in nodes")
	}
}

func TestBuildTree_DiamondDependency(t *testing.T) {
	// root -> A, root -> B, A -> leaf, B -> leaf
	// leaf should only appear once
	tasks := []types.ResolvedTask{
		makeTask("root", "Root", "ready", "high", nil),
		makeTask("a", "A", "waiting", "medium", []string{"root"}),
		makeTask("b", "B", "waiting", "medium", []string{"root"}),
		makeTask("leaf", "Leaf", "waiting", "low", []string{"a", "b"}),
	}
	nodes := BuildTree(tasks, []types.ResolvedTask{})

	// Count total nodes in tree
	count := 0
	var countNodes func(ns []TreeNode)
	countNodes = func(ns []TreeNode) {
		for _, n := range ns {
			count++
			countNodes(n.Children)
		}
	}
	countNodes(nodes)

	// leaf should appear only once (diamond handled)
	if count != 4 {
		t.Errorf("expected 4 total nodes (diamond dedup), got %d", count)
	}
}

func TestBuildTree_UnresolvedDependency(t *testing.T) {
	// Task depends on non-existent task — should be treated as root
	tasks := []types.ResolvedTask{
		makeTask("orphan", "Orphan", "ready", "medium", []string{"nonexistent"}),
	}
	nodes := BuildTree(tasks, []types.ResolvedTask{})

	if len(nodes) != 1 {
		t.Fatalf("expected 1 root (orphan with unresolved dep), got %d", len(nodes))
	}
	if nodes[0].Task.ID != "orphan" {
		t.Errorf("expected root 'orphan', got '%s'", nodes[0].Task.ID)
	}
}

func TestBuildTree_SiblingsSortedByPriority(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeTask("root", "Root", "ready", "high", nil),
		makeTask("low", "Low Child", "waiting", "low", []string{"root"}),
		makeTask("high", "High Child", "waiting", "high", []string{"root"}),
		makeTask("med", "Med Child", "waiting", "medium", []string{"root"}),
	}
	nodes := BuildTree(tasks, []types.ResolvedTask{})

	if len(nodes) != 1 {
		t.Fatalf("expected 1 root, got %d", len(nodes))
	}
	children := nodes[0].Children
	if len(children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(children))
	}
	// Should be sorted: high, medium, low
	if children[0].Task.ID != "high" {
		t.Errorf("expected first child 'high', got '%s'", children[0].Task.ID)
	}
	if children[1].Task.ID != "med" {
		t.Errorf("expected second child 'med', got '%s'", children[1].Task.ID)
	}
	if children[2].Task.ID != "low" {
		t.Errorf("expected third child 'low', got '%s'", children[2].Task.ID)
	}
}

// =============================================================================
// FlattenTreeOrder Tests
// =============================================================================

func TestFlattenTreeOrder_EmptyTasks(t *testing.T) {
	order := FlattenTreeOrder(nil)
	if len(order) != 0 {
		t.Errorf("expected 0 items, got %d", len(order))
	}
}

func TestFlattenTreeOrder_SingleTask(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeTask("t1", "Task 1", "ready", "medium", nil),
	}
	order := FlattenTreeOrder(tasks)

	if len(order) != 1 {
		t.Fatalf("expected 1 item, got %d", len(order))
	}
	if order[0] != "t1" {
		t.Errorf("expected 't1', got '%s'", order[0])
	}
}

func TestFlattenTreeOrder_ParentThenChildren(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeTask("parent", "Parent", "ready", "high", nil),
		makeTask("child1", "Child 1", "waiting", "medium", []string{"parent"}),
		makeTask("child2", "Child 2", "waiting", "low", []string{"parent"}),
	}
	order := FlattenTreeOrder(tasks)

	if len(order) != 3 {
		t.Fatalf("expected 3 items, got %d", len(order))
	}
	if order[0] != "parent" {
		t.Errorf("expected first 'parent', got '%s'", order[0])
	}
	// Children should follow parent
	if order[1] != "child1" {
		t.Errorf("expected second 'child1', got '%s'", order[1])
	}
	if order[2] != "child2" {
		t.Errorf("expected third 'child2', got '%s'", order[2])
	}
}

func TestFlattenTreeOrder_DeepHierarchy(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeTask("root", "Root", "ready", "high", nil),
		makeTask("mid", "Mid", "waiting", "medium", []string{"root"}),
		makeTask("leaf", "Leaf", "waiting", "low", []string{"mid"}),
	}
	order := FlattenTreeOrder(tasks)

	expected := []string{"root", "mid", "leaf"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d items, got %d", len(expected), len(order))
	}
	for i, id := range expected {
		if order[i] != id {
			t.Errorf("order[%d] = '%s', want '%s'", i, order[i], id)
		}
	}
}

// =============================================================================
// TaskTree Component Tests
// =============================================================================

func TestNewTaskTree_DefaultState(t *testing.T) {
	tt := makeTaskTree()

	if tt.SelectedID != "" {
		t.Errorf("expected empty SelectedID, got '%s'", tt.SelectedID)
	}
	if tt.Cursor != 0 {
		t.Errorf("expected cursor 0, got %d", tt.Cursor)
	}
	if len(tt.nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(tt.nodes))
	}
	if len(tt.order) != 0 {
		t.Errorf("expected 0 order items, got %d", len(tt.order))
	}
}

func TestTaskTree_SetTasks_BuildsTree(t *testing.T) {
	tt := makeTaskTree()
	tasks := []types.ResolvedTask{
		makeTask("t1", "Task 1", "ready", "high", nil),
		makeTask("t2", "Task 2", "waiting", "medium", []string{"t1"}),
	}

	tt.SetTasks(tasks)

	if len(tt.nodes) != 1 {
		t.Fatalf("expected 1 root node, got %d", len(tt.nodes))
	}
	if len(tt.order) != 2 {
		t.Fatalf("expected 2 items in order, got %d", len(tt.order))
	}
	// Should auto-select first task
	if tt.SelectedID != "t1" {
		t.Errorf("expected SelectedID 't1', got '%s'", tt.SelectedID)
	}
}

func TestTaskTree_SetTasks_PreservesSelection(t *testing.T) {
	tt := makeTaskTree()
	tasks := []types.ResolvedTask{
		makeTask("t1", "Task 1", "ready", "high", nil),
		makeTask("t2", "Task 2", "ready", "medium", nil),
	}

	tt.SetTasks(tasks)
	tt.SelectedID = "t2"
	tt.Cursor = 1

	// Update with same tasks — should preserve selection
	tt.SetTasks(tasks)

	if tt.SelectedID != "t2" {
		t.Errorf("expected SelectedID preserved as 't2', got '%s'", tt.SelectedID)
	}
}

func TestTaskTree_MoveDown(t *testing.T) {
	tt := makeTaskTree()
	tasks := []types.ResolvedTask{
		makeTask("t1", "Task 1", "ready", "high", nil),
		makeTask("t2", "Task 2", "ready", "medium", nil),
		makeTask("t3", "Task 3", "ready", "low", nil),
	}
	tt.SetTasks(tasks)

	tt.MoveDown()
	if tt.SelectedID != "t2" {
		t.Errorf("after MoveDown, expected 't2', got '%s'", tt.SelectedID)
	}
	if tt.Cursor != 1 {
		t.Errorf("after MoveDown, expected cursor 1, got %d", tt.Cursor)
	}

	tt.MoveDown()
	if tt.SelectedID != "t3" {
		t.Errorf("after second MoveDown, expected 't3', got '%s'", tt.SelectedID)
	}

	// At bottom — should not move further
	tt.MoveDown()
	if tt.SelectedID != "t3" {
		t.Errorf("at bottom, expected 't3', got '%s'", tt.SelectedID)
	}
}

func TestTaskTree_MoveUp(t *testing.T) {
	tt := makeTaskTree()
	tasks := []types.ResolvedTask{
		makeTask("t1", "Task 1", "ready", "high", nil),
		makeTask("t2", "Task 2", "ready", "medium", nil),
	}
	tt.SetTasks(tasks)

	// Move to second item
	tt.MoveDown()
	if tt.SelectedID != "t2" {
		t.Fatalf("expected 't2', got '%s'", tt.SelectedID)
	}

	tt.MoveUp()
	if tt.SelectedID != "t1" {
		t.Errorf("after MoveUp, expected 't1', got '%s'", tt.SelectedID)
	}

	// At top — should not move further
	tt.MoveUp()
	if tt.SelectedID != "t1" {
		t.Errorf("at top, expected 't1', got '%s'", tt.SelectedID)
	}
}

func TestTaskTree_MoveToTop(t *testing.T) {
	tt := makeTaskTree()
	tasks := []types.ResolvedTask{
		makeTask("t1", "Task 1", "ready", "high", nil),
		makeTask("t2", "Task 2", "ready", "medium", nil),
		makeTask("t3", "Task 3", "ready", "low", nil),
	}
	tt.SetTasks(tasks)
	tt.Cursor = 2
	tt.SelectedID = "t3"

	tt.MoveToTop()
	if tt.SelectedID != "t1" {
		t.Errorf("after MoveToTop, expected 't1', got '%s'", tt.SelectedID)
	}
	if tt.Cursor != 0 {
		t.Errorf("after MoveToTop, expected cursor 0, got %d", tt.Cursor)
	}
}

func TestTaskTree_MoveToBottom(t *testing.T) {
	tt := makeTaskTree()
	tasks := []types.ResolvedTask{
		makeTask("t1", "Task 1", "ready", "high", nil),
		makeTask("t2", "Task 2", "ready", "medium", nil),
		makeTask("t3", "Task 3", "ready", "low", nil),
	}
	tt.SetTasks(tasks)

	tt.MoveToBottom()
	if tt.SelectedID != "t3" {
		t.Errorf("after MoveToBottom, expected 't3', got '%s'", tt.SelectedID)
	}
	if tt.Cursor != 2 {
		t.Errorf("after MoveToBottom, expected cursor 2, got %d", tt.Cursor)
	}
}

func TestTaskTree_NavigationOnEmpty(t *testing.T) {
	tt := makeTaskTree()

	// Should not panic on empty tree
	tt.MoveDown()
	tt.MoveUp()
	tt.MoveToTop()
	tt.MoveToBottom()

	if tt.SelectedID != "" {
		t.Errorf("expected empty SelectedID on empty tree, got '%s'", tt.SelectedID)
	}
}

// =============================================================================
// View Rendering Tests
// =============================================================================

func TestTaskTree_View_ShowsTaskTitles(t *testing.T) {
	tt := makeTaskTree()
	tasks := []types.ResolvedTask{
		makeTask("t1", "Build the widget", "ready", "medium", nil),
		makeTask("t2", "Test the widget", "waiting", "medium", []string{"t1"}),
	}
	tt.SetTasks(tasks)

	view := tt.View(60, 20)

	if !strings.Contains(view, "Build the widget") {
		t.Errorf("expected view to contain 'Build the widget', got:\n%s", view)
	}
	if !strings.Contains(view, "Test the widget") {
		t.Errorf("expected view to contain 'Test the widget', got:\n%s", view)
	}
}

func TestTaskTree_View_ShowsStatusIndicators(t *testing.T) {
	tt := makeTaskTree()
	tasks := []types.ResolvedTask{
		makeTask("t1", "Ready Task", "ready", "medium", nil),
		makeTask("t2", "Waiting Task", "waiting", "medium", nil),
		makeTask("t3", "Blocked Task", "blocked", "medium", nil),
	}
	tt.SetTasks(tasks)

	view := tt.View(60, 20)

	// Should contain status indicators
	if !strings.Contains(view, IndicatorReady) {
		t.Errorf("expected ready indicator '%s' in view", IndicatorReady)
	}
	if !strings.Contains(view, IndicatorWaiting) {
		t.Errorf("expected waiting indicator '%s' in view", IndicatorWaiting)
	}
	if !strings.Contains(view, IndicatorBlocked) {
		t.Errorf("expected blocked indicator '%s' in view", IndicatorBlocked)
	}
}

func TestTaskTree_View_ShowsCycleIndicator(t *testing.T) {
	tt := makeTaskTree()
	tasks := []types.ResolvedTask{
		makeTaskInCycle("t1", "Cyclic Task", "blocked", "medium", []string{"t2"}),
		makeTaskInCycle("t2", "Cyclic Task 2", "blocked", "medium", []string{"t1"}),
	}
	tt.SetTasks(tasks)

	view := tt.View(60, 20)

	if !strings.Contains(view, "↺") {
		t.Errorf("expected cycle indicator '↺' in view, got:\n%s", view)
	}
}

func TestTaskTree_View_ShowsHighPriorityIndicator(t *testing.T) {
	tt := makeTaskTree()
	tasks := []types.ResolvedTask{
		makeTask("t1", "Urgent Task", "ready", "high", nil),
	}
	tt.SetTasks(tasks)

	view := tt.View(60, 20)

	if !strings.Contains(view, "!") {
		t.Errorf("expected high priority indicator '!' in view, got:\n%s", view)
	}
}

func TestTaskTree_View_ShowsTreeConnectors(t *testing.T) {
	tt := makeTaskTree()
	tasks := []types.ResolvedTask{
		makeTask("root", "Root", "ready", "high", nil),
		makeTask("child1", "Child 1", "waiting", "medium", []string{"root"}),
		makeTask("child2", "Child 2", "waiting", "medium", []string{"root"}),
	}
	tt.SetTasks(tasks)

	view := tt.View(60, 20)

	// Should contain tree drawing characters
	if !strings.Contains(view, "├") && !strings.Contains(view, "└") {
		t.Errorf("expected tree connectors (├ or └) in view, got:\n%s", view)
	}
}

func TestTaskTree_View_EmptyShowsPlaceholder(t *testing.T) {
	tt := makeTaskTree()
	view := tt.View(60, 20)

	if !strings.Contains(view, "No tasks") {
		t.Errorf("expected 'No tasks' placeholder in empty view, got:\n%s", view)
	}
}

func TestTaskTree_View_SelectedTaskHighlighted(t *testing.T) {
	tt := makeTaskTree()
	tasks := []types.ResolvedTask{
		makeTask("t1", "First Task", "ready", "medium", nil),
		makeTask("t2", "Second Task", "ready", "medium", nil),
	}
	tt.SetTasks(tasks)
	tt.SelectedID = "t1"

	view := tt.View(60, 20)

	// The selected task should have some visual distinction
	// We'll check that the view contains the selected task title
	if !strings.Contains(view, "First Task") {
		t.Errorf("expected selected task 'First Task' in view, got:\n%s", view)
	}
}

// =============================================================================
// Feature View Mode Tests
// =============================================================================

func TestTaskTree_FeatureView_GroupsByFeatureID(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)        // Enable grouped view
	tt.SetFeatureViewMode(true) // Enable feature grouping

	tasks := []types.ResolvedTask{
		makeTaskWithFeature("t1", "Auth Task 1", "ready", "high", "auth-system", nil),
		makeTaskWithFeature("t2", "Auth Task 2", "ready", "low", "auth-system", nil),
		makeTaskWithFeature("t3", "Dashboard Task", "ready", "medium", "dashboard", nil),
		makeTask("t4", "Ungrouped Task", "ready", "medium", nil),
	}
	tt.SetTasks(tasks)

	view := tt.View(80, 30)

	// Should show feature headers
	if !strings.Contains(view, "auth-system") {
		t.Errorf("Expected feature header 'auth-system' in view, got:\n%s", view)
	}
	if !strings.Contains(view, "dashboard") {
		t.Errorf("Expected feature header 'dashboard' in view, got:\n%s", view)
	}
	if !strings.Contains(view, "[Ungrouped]") {
		t.Errorf("Expected [Ungrouped] header in view, got:\n%s", view)
	}

	// Should show task counts
	if !strings.Contains(view, "(2)") {
		t.Errorf("Expected task count (2) for auth-system in view, got:\n%s", view)
	}
}

func TestTaskTree_FeatureView_ShowsStatsInHeader(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)
	tt.SetFeatureViewMode(true)

	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", FeatureID: "feature-a", Status: "completed", Classification: "ready", Priority: "high"},
		{ID: "t2", Title: "Task 2", FeatureID: "feature-a", Status: "pending", Classification: "ready", Priority: "high"},
		{ID: "t3", Title: "Task 3", FeatureID: "feature-a", Status: "in_progress", Classification: "ready", Priority: "high"},
	}
	tt.SetTasks(tasks)

	view := tt.View(80, 30)

	// Should show stats in feature header: [completed/total]
	if !strings.Contains(view, "feature-a") {
		t.Errorf("Expected feature header with stats in view, got:\n%s", view)
	}
	// Check for stats format
	if !strings.Contains(view, "[1/3]") {
		t.Errorf("Expected stats [1/3] (1 completed out of 3 total) in view, got:\n%s", view)
	}
}

func TestTaskTree_FeatureView_CollapsibleFeatures(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)
	tt.SetFeatureViewMode(true)

	tasks := []types.ResolvedTask{
		makeTaskWithFeature("t1", "Auth Task 1", "ready", "high", "auth-system", nil),
		makeTaskWithFeature("t2", "Auth Task 2", "ready", "low", "auth-system", nil),
	}
	tt.SetTasks(tasks)

	// Initially expanded - should show tasks
	view := tt.View(80, 30)
	if !strings.Contains(view, "Auth Task 1") {
		t.Errorf("Expected expanded feature to show tasks, got:\n%s", view)
	}
	if !strings.Contains(view, "▾") {
		t.Errorf("Expected expanded indicator ▾, got:\n%s", view)
	}

	// Toggle collapse
	tt.ToggleCollapse()

	// Now collapsed - should NOT show tasks
	view = tt.View(80, 30)
	if strings.Contains(view, "Auth Task 1") {
		t.Errorf("Expected collapsed feature to hide tasks, got:\n%s", view)
	}
	if !strings.Contains(view, "▸") {
		t.Errorf("Expected collapsed indicator ▸, got:\n%s", view)
	}
}

// =============================================================================
// Phase 3: BuildTree Signature Tests (allTasks parameter)
// =============================================================================

func TestBuildTree_WithAllTasksParameter_BackwardCompat(t *testing.T) {
	// Test that BuildTree works with new signature, empty allTasks (backward compat)
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Classification: "ready", Priority: "high", Status: "pending"},
		{ID: "t2", Title: "Task 2", Classification: "waiting", Priority: "medium", Status: "pending", DependsOn: []string{"t1"}},
	}

	// Call with empty allTasks - should behave like before
	nodes := BuildTree(tasks, []types.ResolvedTask{})

	if len(nodes) != 1 {
		t.Fatalf("expected 1 root node, got %d", len(nodes))
	}
	if nodes[0].Task.ID != "t1" {
		t.Errorf("expected root 't1', got '%s'", nodes[0].Task.ID)
	}
	if len(nodes[0].Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(nodes[0].Children))
	}
	if nodes[0].Children[0].Task.ID != "t2" {
		t.Errorf("expected child 't2', got '%s'", nodes[0].Children[0].Task.ID)
	}
}

func TestBuildTree_WithAllTasksParameter_ExplicitAllTasks(t *testing.T) {
	// Test that BuildTree works when allTasks is explicitly provided (same as tasks)
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Classification: "ready", Priority: "high", Status: "pending"},
	}

	nodes := BuildTree(tasks, tasks) // allTasks same as tasks

	if len(nodes) != 1 {
		t.Fatalf("expected 1 root node, got %d", len(nodes))
	}
	if nodes[0].Task.ID != "t1" {
		t.Errorf("expected root 't1', got '%s'", nodes[0].Task.ID)
	}
}

// =============================================================================
// Phase 6: Enhanced Sibling Sorting Tests
// =============================================================================

// Test 1: Siblings with no inter-dependencies → sorted by priority (existing behavior)
func TestBuildTree_Phase6_SiblingsNoDependencies_SortByPriority(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeTask("parent", "Parent", "ready", "high", nil),
		makeTaskWithParentAndDeps("low", "Low Child", "waiting", "low", "parent", nil),
		makeTaskWithParentAndDeps("high", "High Child", "waiting", "high", "parent", nil),
		makeTaskWithParentAndDeps("med", "Med Child", "waiting", "medium", "parent", nil),
	}
	nodes := BuildTree(tasks, tasks)

	if len(nodes) != 1 {
		t.Fatalf("expected 1 root, got %d", len(nodes))
	}
	children := nodes[0].Children
	if len(children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(children))
	}
	// Should be sorted by priority: high, medium, low
	if children[0].Task.ID != "high" {
		t.Errorf("expected first child 'high', got '%s'", children[0].Task.ID)
	}
	if children[1].Task.ID != "med" {
		t.Errorf("expected second child 'med', got '%s'", children[1].Task.ID)
	}
	if children[2].Task.ID != "low" {
		t.Errorf("expected third child 'low', got '%s'", children[2].Task.ID)
	}
}

// Test 2: Sibling A in B's depends_on → A comes first
func TestBuildTree_Phase6_SiblingAInBDependsOn(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeTask("parent", "Parent", "ready", "high", nil),
		// Both A and B are siblings under parent (via parent_id)
		// B also depends on A (sorting hint)
		makeTaskWithParentAndDeps("a", "Task A", "waiting", "medium", "parent", nil),
		makeTaskWithParentAndDeps("b", "Task B", "waiting", "medium", "parent", []string{"a"}),
	}
	nodes := BuildTree(tasks, tasks)

	if len(nodes) != 1 {
		t.Fatalf("expected 1 root, got %d", len(nodes))
	}
	children := nodes[0].Children
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}
	// A should come before B because B depends on A
	if children[0].Task.ID != "a" {
		t.Errorf("expected first child 'a', got '%s'", children[0].Task.ID)
	}
	if children[1].Task.ID != "b" {
		t.Errorf("expected second child 'b', got '%s'", children[1].Task.ID)
	}
}

// Test 3: Sibling B in A's depends_on → B comes first
func TestBuildTree_Phase6_SiblingBInADependsOn(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeTask("parent", "Parent", "ready", "high", nil),
		// Both A and B are siblings under parent (via parent_id)
		// A depends on B (sorting hint)
		makeTaskWithParentAndDeps("a", "Task A", "waiting", "medium", "parent", []string{"b"}),
		makeTaskWithParentAndDeps("b", "Task B", "waiting", "medium", "parent", nil),
	}
	nodes := BuildTree(tasks, tasks)

	if len(nodes) != 1 {
		t.Fatalf("expected 1 root, got %d", len(nodes))
	}
	children := nodes[0].Children
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}
	// B should come before A because A depends on B
	if children[0].Task.ID != "b" {
		t.Errorf("expected first child 'b', got '%s'", children[0].Task.ID)
	}
	if children[1].Task.ID != "a" {
		t.Errorf("expected second child 'a', got '%s'", children[1].Task.ID)
	}
}

// Test 4: Sibling chain: A → B → C (A in B's deps, B in C's deps) → sorted as [A, B, C]
func TestBuildTree_Phase6_SiblingChain(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeTask("parent", "Parent", "ready", "high", nil),
		// All siblings under parent (via parent_id)
		// Chain: C depends on B, B depends on A
		makeTaskWithParentAndDeps("a", "Task A", "waiting", "medium", "parent", nil),
		makeTaskWithParentAndDeps("b", "Task B", "waiting", "medium", "parent", []string{"a"}),
		makeTaskWithParentAndDeps("c", "Task C", "waiting", "medium", "parent", []string{"b"}),
	}
	nodes := BuildTree(tasks, tasks)

	if len(nodes) != 1 {
		t.Fatalf("expected 1 root, got %d", len(nodes))
	}
	children := nodes[0].Children
	if len(children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(children))
	}
	// Should be sorted as A, B, C (dependency chain order)
	if children[0].Task.ID != "a" {
		t.Errorf("expected first child 'a', got '%s'", children[0].Task.ID)
	}
	if children[1].Task.ID != "b" {
		t.Errorf("expected second child 'b', got '%s'", children[1].Task.ID)
	}
	if children[2].Task.ID != "c" {
		t.Errorf("expected third child 'c', got '%s'", children[2].Task.ID)
	}
}

// Test 5: Siblings with same priority, no deps → sorted by status
func TestBuildTree_Phase6_SiblingsSamePriorityNoDeps_SortByStatus(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeTask("parent", "Parent", "ready", "high", nil),
	}
	// Add children with different statuses using makeTaskWithParent
	completed := makeTaskWithParentAndDeps("completed", "Completed Child", "ready", "medium", "parent", nil)
	completed.Status = "completed"
	tasks = append(tasks, completed)

	inProgress := makeTaskWithParentAndDeps("in_progress", "In Progress Child", "ready", "medium", "parent", nil)
	inProgress.Status = "in_progress"
	tasks = append(tasks, inProgress)

	pending := makeTaskWithParentAndDeps("pending", "Pending Child", "ready", "medium", "parent", nil)
	pending.Status = "pending"
	tasks = append(tasks, pending)

	nodes := BuildTree(tasks, tasks)

	if len(nodes) != 1 {
		t.Fatalf("expected 1 root, got %d", len(nodes))
	}
	children := nodes[0].Children
	if len(children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(children))
	}
	// Should be sorted by status order: in_progress (0), pending (1), completed (4)
	if children[0].Task.ID != "in_progress" {
		t.Errorf("expected first child 'in_progress', got '%s'", children[0].Task.ID)
	}
	if children[1].Task.ID != "pending" {
		t.Errorf("expected second child 'pending', got '%s'", children[1].Task.ID)
	}
	if children[2].Task.ID != "completed" {
		t.Errorf("expected third child 'completed', got '%s'", children[2].Task.ID)
	}
}

// Test 6: Mix of dependencies and priorities → dependencies take precedence
func TestBuildTree_Phase6_MixDependenciesAndPriorities(t *testing.T) {
	tasks := []types.ResolvedTask{
		makeTask("parent", "Parent", "ready", "high", nil),
		// All siblings under parent (via parent_id)
		// B (low priority) depends on A (medium priority)
		// C (high priority) has no deps
		makeTaskWithParentAndDeps("a", "Task A", "waiting", "medium", "parent", nil),
		makeTaskWithParentAndDeps("b", "Task B", "waiting", "low", "parent", []string{"a"}),
		makeTaskWithParentAndDeps("c", "Task C", "waiting", "high", "parent", nil),
	}
	nodes := BuildTree(tasks, tasks)

	if len(nodes) != 1 {
		t.Fatalf("expected 1 root, got %d", len(nodes))
	}
	children := nodes[0].Children
	if len(children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(children))
	}
	// A must come before B (dependency)
	// C has high priority but no dependency relationship with A or B
	// Expected order: C (high priority first), then A, then B (A before B due to dependency)
	// Verify A comes before B
	aIndex := -1
	bIndex := -1
	for i, child := range children {
		if child.Task.ID == "a" {
			aIndex = i
		}
		if child.Task.ID == "b" {
			bIndex = i
		}
	}
	if aIndex == -1 || bIndex == -1 {
		t.Fatal("expected both A and B to be children")
	}
	if aIndex >= bIndex {
		t.Errorf("expected A (index %d) to come before B (index %d)", aIndex, bIndex)
	}
	// C should be first due to high priority (since no dependency conflict)
	if children[0].Task.ID != "c" {
		t.Errorf("expected first child 'c' (high priority), got '%s'", children[0].Task.ID)
	}
}

// =============================================================================
// Phase 4: Merge Parent and Dependency Relationships Tests
// =============================================================================

// Test 1: Task with BOTH parent_id and depends_on → parent_id wins placement
func TestBuildTree_Phase4_ParentIDTakesPrecedence(t *testing.T) {
	// Task D has both parent_id=C and depends_on=[B]
	// D should appear under C (parent_id wins), NOT under B
	tasks := []types.ResolvedTask{
		{ID: "a", Title: "Task A", Classification: "ready", Priority: "high", Status: "pending"},
		{ID: "b", Title: "Task B", Classification: "ready", Priority: "high", Status: "pending", DependsOn: []string{"a"}},
		{ID: "c", Title: "Task C", Classification: "ready", Priority: "high", Status: "pending", DependsOn: []string{"a"}},
		{ID: "d", Title: "Task D", Classification: "waiting", Priority: "medium", Status: "pending", ParentID: "c", DependsOn: []string{"b"}},
	}

	nodes := BuildTree(tasks, tasks)

	// Find node A (root)
	var nodeA *TreeNode
	for i := range nodes {
		if nodes[i].Task.ID == "a" {
			nodeA = &nodes[i]
			break
		}
	}
	if nodeA == nil {
		t.Fatal("expected to find root node A")
	}

	// A should have children B and C
	if len(nodeA.Children) != 2 {
		t.Fatalf("expected A to have 2 children (B and C), got %d", len(nodeA.Children))
	}

	// Find C among A's children
	var nodeC *TreeNode
	for i := range nodeA.Children {
		if nodeA.Children[i].Task.ID == "c" {
			nodeC = &nodeA.Children[i]
			break
		}
	}
	if nodeC == nil {
		t.Fatal("expected C to be a child of A")
	}

	// C should have D as a child (parent_id wins over depends_on)
	if len(nodeC.Children) != 1 {
		t.Fatalf("expected C to have 1 child (D), got %d", len(nodeC.Children))
	}
	if nodeC.Children[0].Task.ID != "d" {
		t.Errorf("expected D to be child of C, got '%s'", nodeC.Children[0].Task.ID)
	}

	// B should NOT have D as a child
	var nodeB *TreeNode
	for i := range nodeA.Children {
		if nodeA.Children[i].Task.ID == "b" {
			nodeB = &nodeA.Children[i]
			break
		}
	}
	if nodeB == nil {
		t.Fatal("expected B to be a child of A")
	}
	if len(nodeB.Children) != 0 {
		t.Errorf("expected B to have 0 children (D blocked by parent_id), got %d", len(nodeB.Children))
	}
}

// Test 2: Task with depends_on only → appears under dependency parent
func TestBuildTree_Phase4_DependsOnOnlyAppears(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "parent", Title: "Parent", Classification: "ready", Priority: "high", Status: "pending"},
		{ID: "child", Title: "Child", Classification: "waiting", Priority: "medium", Status: "pending", DependsOn: []string{"parent"}},
	}

	nodes := BuildTree(tasks, tasks)

	if len(nodes) != 1 {
		t.Fatalf("expected 1 root node, got %d", len(nodes))
	}
	if nodes[0].Task.ID != "parent" {
		t.Errorf("expected root 'parent', got '%s'", nodes[0].Task.ID)
	}
	if len(nodes[0].Children) != 1 {
		t.Fatalf("expected parent to have 1 child, got %d", len(nodes[0].Children))
	}
	if nodes[0].Children[0].Task.ID != "child" {
		t.Errorf("expected child 'child', got '%s'", nodes[0].Children[0].Task.ID)
	}
}

// Test 3: Task with parent_id only → appears under parent_id parent
func TestBuildTree_Phase4_ParentIDOnlyAppears(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "parent", Title: "Parent", Classification: "ready", Priority: "high", Status: "pending"},
		{ID: "child", Title: "Child", Classification: "waiting", Priority: "medium", Status: "pending", ParentID: "parent"},
	}

	nodes := BuildTree(tasks, tasks)

	if len(nodes) != 1 {
		t.Fatalf("expected 1 root node, got %d", len(nodes))
	}
	if nodes[0].Task.ID != "parent" {
		t.Errorf("expected root 'parent', got '%s'", nodes[0].Task.ID)
	}
	if len(nodes[0].Children) != 1 {
		t.Fatalf("expected parent to have 1 child, got %d", len(nodes[0].Children))
	}
	if nodes[0].Children[0].Task.ID != "child" {
		t.Errorf("expected child 'child', got '%s'", nodes[0].Children[0].Task.ID)
	}
}

// Test 4: Multiple tasks with mixed relationships
func TestBuildTree_Phase4_MixedRelationships(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "root", Title: "Root", Classification: "ready", Priority: "high", Status: "pending"},
		{ID: "dep_child", Title: "Dep Child", Classification: "waiting", Priority: "medium", Status: "pending", DependsOn: []string{"root"}},
		{ID: "parent_child", Title: "Parent Child", Classification: "waiting", Priority: "medium", Status: "pending", ParentID: "root"},
		{ID: "both_child", Title: "Both Child", Classification: "waiting", Priority: "medium", Status: "pending", ParentID: "root", DependsOn: []string{"dep_child"}},
	}

	nodes := BuildTree(tasks, tasks)

	if len(nodes) != 1 {
		t.Fatalf("expected 1 root node, got %d", len(nodes))
	}

	root := nodes[0]
	if root.Task.ID != "root" {
		t.Errorf("expected root 'root', got '%s'", root.Task.ID)
	}

	// Root should have 3 children: dep_child (from depends_on), parent_child (from parent_id), both_child (from parent_id)
	if len(root.Children) != 3 {
		t.Fatalf("expected root to have 3 children, got %d", len(root.Children))
	}

	// Verify all three children are present
	childIDs := make(map[string]bool)
	for _, child := range root.Children {
		childIDs[child.Task.ID] = true
	}

	expectedChildren := []string{"dep_child", "parent_child", "both_child"}
	for _, expectedID := range expectedChildren {
		if !childIDs[expectedID] {
			t.Errorf("expected child '%s' to be under root", expectedID)
		}
	}
}

// =============================================================================
// Phase 8: Comprehensive Integration & Edge Case Tests
// =============================================================================

// Test 1: Deep parent_id hierarchy with depends_on cross-edges
func TestBuildTree_Phase8_DeepHierarchyWithCrossEdges(t *testing.T) {
	// Root → Parent (parent_id) → Child (parent_id) → Grandchild (parent_id)
	// Plus: Child depends_on another task at same level (SiblingTask)
	// Verify: Tree structure respects parent_id, sorting respects depends_on
	tasks := []types.ResolvedTask{
		{ID: "root", Title: "Root", Classification: "ready", Priority: "high", Status: "pending"},
		{ID: "parent", Title: "Parent", Classification: "waiting", Priority: "high", Status: "pending", ParentID: "root"},
		{ID: "sibling", Title: "Sibling Task", Classification: "waiting", Priority: "medium", Status: "pending", ParentID: "root"},
		{ID: "child", Title: "Child", Classification: "waiting", Priority: "medium", Status: "pending", ParentID: "parent", DependsOn: []string{"sibling"}},
		{ID: "grandchild", Title: "Grandchild", Classification: "waiting", Priority: "low", Status: "pending", ParentID: "child"},
	}
	nodes := BuildTree(tasks, tasks)

	// Verify root structure
	if len(nodes) != 1 {
		t.Fatalf("expected 1 root, got %d", len(nodes))
	}
	if nodes[0].Task.ID != "root" {
		t.Errorf("expected root 'root', got '%s'", nodes[0].Task.ID)
	}

	// Root should have 2 children: parent and sibling
	if len(nodes[0].Children) != 2 {
		t.Fatalf("expected root to have 2 children, got %d", len(nodes[0].Children))
	}

	// Find parent and sibling
	var parentNode, siblingNode *TreeNode
	for i := range nodes[0].Children {
		if nodes[0].Children[i].Task.ID == "parent" {
			parentNode = &nodes[0].Children[i]
		}
		if nodes[0].Children[i].Task.ID == "sibling" {
			siblingNode = &nodes[0].Children[i]
		}
	}

	if parentNode == nil {
		t.Fatal("expected to find parent node")
	}
	if siblingNode == nil {
		t.Fatal("expected to find sibling node")
	}

	// Parent should have child as child
	if len(parentNode.Children) != 1 {
		t.Fatalf("expected parent to have 1 child, got %d", len(parentNode.Children))
	}
	if parentNode.Children[0].Task.ID != "child" {
		t.Errorf("expected parent's child to be 'child', got '%s'", parentNode.Children[0].Task.ID)
	}

	// Child should have grandchild
	if len(parentNode.Children[0].Children) != 1 {
		t.Fatalf("expected child to have 1 child, got %d", len(parentNode.Children[0].Children))
	}
	if parentNode.Children[0].Children[0].Task.ID != "grandchild" {
		t.Errorf("expected grandchild 'grandchild', got '%s'", parentNode.Children[0].Children[0].Task.ID)
	}
}

// Test 2: Diamond dependency with mixed relationships
func TestBuildTree_Phase8_DiamondWithMixedRelationships(t *testing.T) {
	// Root with two children via parent_id (ChildA, ChildB)
	// ChildA depends_on ChildB (cross-edge for sorting)
	// Fourth task (Leaf) has parent_id=ChildB and depends_on=[ChildA]
	// Verify: No duplication, correct placement, proper sorting
	tasks := []types.ResolvedTask{
		{ID: "root", Title: "Root", Classification: "ready", Priority: "high", Status: "pending"},
		{ID: "child_a", Title: "Child A", Classification: "waiting", Priority: "medium", Status: "pending", ParentID: "root", DependsOn: []string{"child_b"}},
		{ID: "child_b", Title: "Child B", Classification: "waiting", Priority: "medium", Status: "pending", ParentID: "root"},
		{ID: "leaf", Title: "Leaf", Classification: "waiting", Priority: "low", Status: "pending", ParentID: "child_b", DependsOn: []string{"child_a"}},
	}
	nodes := BuildTree(tasks, tasks)

	// Count all nodes in tree
	countNodes := func(ns []TreeNode) int {
		count := 0
		var traverse func([]TreeNode)
		traverse = func(nodes []TreeNode) {
			for _, n := range nodes {
				count++
				traverse(n.Children)
			}
		}
		traverse(ns)
		return count
	}

	totalNodes := countNodes(nodes)
	if totalNodes != 4 {
		t.Errorf("expected 4 total nodes (no duplication), got %d", totalNodes)
	}

	// Verify structure
	if len(nodes) != 1 {
		t.Fatalf("expected 1 root, got %d", len(nodes))
	}

	root := nodes[0]
	if len(root.Children) != 2 {
		t.Fatalf("expected root to have 2 children, got %d", len(root.Children))
	}

	// child_b should come before child_a (due to depends_on sorting)
	if root.Children[0].Task.ID != "child_b" {
		t.Errorf("expected first child to be 'child_b', got '%s'", root.Children[0].Task.ID)
	}
	if root.Children[1].Task.ID != "child_a" {
		t.Errorf("expected second child to be 'child_a', got '%s'", root.Children[1].Task.ID)
	}

	// child_b should have leaf as child
	childB := root.Children[0]
	if len(childB.Children) != 1 {
		t.Fatalf("expected child_b to have 1 child, got %d", len(childB.Children))
	}
	if childB.Children[0].Task.ID != "leaf" {
		t.Errorf("expected leaf under child_b, got '%s'", childB.Children[0].Task.ID)
	}
}

// Test 3: Parent chain through completed tasks (integration test for findActiveAncestor)
func TestBuildTree_Phase8_ParentChainThroughCompletedTasks(t *testing.T) {
	// Active root, completed parent, completed grandparent, active great-grandchild
	// Verify: Active great-grandchild appears directly under active root
	activeTasks := []types.ResolvedTask{
		{ID: "root", Title: "Root", Classification: "ready", Priority: "high", Status: "pending"},
		{ID: "great_grandchild", Title: "Great Grandchild", Classification: "waiting", Priority: "medium", Status: "pending", ParentID: "grandparent"},
	}

	allTasks := []types.ResolvedTask{
		{ID: "root", Title: "Root", Classification: "ready", Priority: "high", Status: "pending"},
		{ID: "parent", Title: "Parent", Classification: "ready", Priority: "high", Status: "completed", ParentID: "root"},
		{ID: "grandparent", Title: "Grandparent", Classification: "ready", Priority: "medium", Status: "completed", ParentID: "parent"},
		{ID: "great_grandchild", Title: "Great Grandchild", Classification: "waiting", Priority: "medium", Status: "pending", ParentID: "grandparent"},
	}

	nodes := BuildTree(activeTasks, allTasks)

	// Verify structure
	if len(nodes) != 1 {
		t.Fatalf("expected 1 root, got %d", len(nodes))
	}
	if nodes[0].Task.ID != "root" {
		t.Errorf("expected root 'root', got '%s'", nodes[0].Task.ID)
	}

	// Root should have great_grandchild directly (skipping completed ancestors)
	if len(nodes[0].Children) != 1 {
		t.Fatalf("expected root to have 1 child, got %d", len(nodes[0].Children))
	}
	if nodes[0].Children[0].Task.ID != "great_grandchild" {
		t.Errorf("expected child 'great_grandchild', got '%s'", nodes[0].Children[0].Task.ID)
	}
}

// Test 4: Multiple siblings with complex dependency chain
func TestBuildTree_Phase8_MultipleSiblingsWithDependencyChain(t *testing.T) {
	// 5 siblings under same parent
	// Dependency chain: A → B → C, D → E
	// Mixed priorities
	// Verify: Sorted [A, B, C, D, E] respecting both deps and priorities
	tasks := []types.ResolvedTask{
		{ID: "parent", Title: "Parent", Classification: "ready", Priority: "high", Status: "pending"},
		{ID: "a", Title: "Task A", Classification: "waiting", Priority: "low", Status: "pending", ParentID: "parent"},
		{ID: "b", Title: "Task B", Classification: "waiting", Priority: "low", Status: "pending", ParentID: "parent", DependsOn: []string{"a"}},
		{ID: "c", Title: "Task C", Classification: "waiting", Priority: "low", Status: "pending", ParentID: "parent", DependsOn: []string{"b"}},
		{ID: "d", Title: "Task D", Classification: "waiting", Priority: "high", Status: "pending", ParentID: "parent"},
		{ID: "e", Title: "Task E", Classification: "waiting", Priority: "medium", Status: "pending", ParentID: "parent", DependsOn: []string{"d"}},
	}
	nodes := BuildTree(tasks, tasks)

	if len(nodes) != 1 {
		t.Fatalf("expected 1 root, got %d", len(nodes))
	}

	parent := nodes[0]
	if len(parent.Children) != 5 {
		t.Fatalf("expected parent to have 5 children, got %d", len(parent.Children))
	}

	// Verify chain: A before B, B before C
	aIndex, bIndex, cIndex := -1, -1, -1
	for i, child := range parent.Children {
		switch child.Task.ID {
		case "a":
			aIndex = i
		case "b":
			bIndex = i
		case "c":
			cIndex = i
		}
	}

	if aIndex == -1 || bIndex == -1 || cIndex == -1 {
		t.Fatal("expected to find tasks A, B, and C")
	}

	if aIndex >= bIndex {
		t.Errorf("expected A (index %d) before B (index %d)", aIndex, bIndex)
	}
	if bIndex >= cIndex {
		t.Errorf("expected B (index %d) before C (index %d)", bIndex, cIndex)
	}

	// Verify chain: D before E
	dIndex, eIndex := -1, -1
	for i, child := range parent.Children {
		switch child.Task.ID {
		case "d":
			dIndex = i
		case "e":
			eIndex = i
		}
	}

	if dIndex == -1 || eIndex == -1 {
		t.Fatal("expected to find tasks D and E")
	}

	if dIndex >= eIndex {
		t.Errorf("expected D (index %d) before E (index %d)", dIndex, eIndex)
	}
}

// Test 5: Cycle detection across both relationship types
func TestBuildTree_Phase8_CycleAcrossBothRelationshipTypes(t *testing.T) {
	// Task A has parent_id → B
	// Task B has depends_on → C
	// Task C has parent_id → A
	// Verify: All marked with InCycle = true
	tasks := []types.ResolvedTask{
		{ID: "a", Title: "Task A", Classification: "blocked", Priority: "medium", Status: "pending", ParentID: "b"},
		{ID: "b", Title: "Task B", Classification: "blocked", Priority: "medium", Status: "pending", DependsOn: []string{"c"}},
		{ID: "c", Title: "Task C", Classification: "blocked", Priority: "medium", Status: "pending", ParentID: "a"},
	}
	nodes := BuildTree(tasks, tasks)

	// Check that all tasks are marked as in cycle
	foundA, foundB, foundC := false, false, false
	var checkCycle func([]TreeNode)
	checkCycle = func(ns []TreeNode) {
		for _, n := range ns {
			switch n.Task.ID {
			case "a":
				foundA = true
				if !n.InCycle {
					t.Error("expected task 'a' to be marked InCycle")
				}
			case "b":
				foundB = true
				if !n.InCycle {
					t.Error("expected task 'b' to be marked InCycle")
				}
			case "c":
				foundC = true
				if !n.InCycle {
					t.Error("expected task 'c' to be marked InCycle")
				}
			}
			checkCycle(n.Children)
		}
	}
	checkCycle(nodes)

	if !foundA || !foundB || !foundC {
		t.Error("expected to find all tasks A, B, and C in tree")
	}
}

// Test 6: Empty task list → empty tree
func TestBuildTree_Phase8_EdgeCase_EmptyTaskList(t *testing.T) {
	nodes := BuildTree([]types.ResolvedTask{}, []types.ResolvedTask{})
	if len(nodes) != 0 {
		t.Errorf("expected empty tree for empty task list, got %d nodes", len(nodes))
	}
}

// Test 7: Single task with no relationships → single root
func TestBuildTree_Phase8_EdgeCase_SingleTaskNoRelationships(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "solo", Title: "Solo Task", Classification: "ready", Priority: "medium", Status: "pending"},
	}
	nodes := BuildTree(tasks, tasks)

	if len(nodes) != 1 {
		t.Fatalf("expected 1 root node, got %d", len(nodes))
	}
	if nodes[0].Task.ID != "solo" {
		t.Errorf("expected task 'solo', got '%s'", nodes[0].Task.ID)
	}
	if len(nodes[0].Children) != 0 {
		t.Errorf("expected 0 children, got %d", len(nodes[0].Children))
	}
}

// Test 8: All tasks have cycles → all marked InCycle, still build tree structure
func TestBuildTree_Phase8_EdgeCase_AllTasksInCycle(t *testing.T) {
	// A → B → C → A (all in cycle)
	tasks := []types.ResolvedTask{
		{ID: "a", Title: "Task A", Classification: "blocked", Priority: "medium", Status: "pending", DependsOn: []string{"c"}},
		{ID: "b", Title: "Task B", Classification: "blocked", Priority: "medium", Status: "pending", DependsOn: []string{"a"}},
		{ID: "c", Title: "Task C", Classification: "blocked", Priority: "medium", Status: "pending", DependsOn: []string{"b"}},
	}
	nodes := BuildTree(tasks, tasks)

	// All tasks should be in tree
	if len(nodes) != 3 {
		t.Errorf("expected 3 root nodes (cycle members as roots), got %d", len(nodes))
	}

	// All should be marked InCycle
	for _, node := range nodes {
		if !node.InCycle {
			t.Errorf("expected task '%s' to be marked InCycle", node.Task.ID)
		}
	}
}

// Test 9: Large tree (20+ tasks) with mixed relationships → verify no performance issues
func TestBuildTree_Phase8_EdgeCase_LargeTreePerformance(t *testing.T) {
	// Create 25 tasks with mixed parent_id and depends_on relationships
	tasks := make([]types.ResolvedTask, 25)
	tasks[0] = types.ResolvedTask{
		ID:             "root",
		Title:          "Root",
		Classification: "ready",
		Priority:       "high",
		Status:         "pending",
	}

	// Create chain via parent_id (root → t1 → t2 → ... → t9)
	for i := 1; i <= 9; i++ {
		parentID := "root"
		if i > 1 {
			parentID = fmt.Sprintf("t%d", i-1)
		}
		tasks[i] = types.ResolvedTask{
			ID:             fmt.Sprintf("t%d", i),
			Title:          fmt.Sprintf("Task %d", i),
			Classification: "waiting",
			Priority:       "medium",
			Status:         "pending",
			ParentID:       parentID,
		}
	}

	// Create siblings with depends_on (t10-t19 all under root)
	for i := 10; i < 20; i++ {
		var deps []string
		if i > 10 {
			deps = []string{fmt.Sprintf("t%d", i-1)}
		}
		tasks[i] = types.ResolvedTask{
			ID:             fmt.Sprintf("t%d", i),
			Title:          fmt.Sprintf("Task %d", i),
			Classification: "waiting",
			Priority:       "low",
			Status:         "pending",
			DependsOn:      deps,
		}
	}

	// Create more independent roots (t20-t24)
	for i := 20; i < 25; i++ {
		tasks[i] = types.ResolvedTask{
			ID:             fmt.Sprintf("t%d", i),
			Title:          fmt.Sprintf("Task %d", i),
			Classification: "ready",
			Priority:       "low",
			Status:         "pending",
		}
	}

	// Measure performance
	nodes := BuildTree(tasks, tasks)

	// Verify structure was built
	if len(nodes) < 1 {
		t.Error("expected at least 1 root node")
	}

	// Count all nodes
	countNodes := func(ns []TreeNode) int {
		count := 0
		var traverse func([]TreeNode)
		traverse = func(nodes []TreeNode) {
			for _, n := range nodes {
				count++
				traverse(n.Children)
			}
		}
		traverse(ns)
		return count
	}

	totalNodes := countNodes(nodes)
	if totalNodes != 25 {
		t.Errorf("expected 25 total nodes, got %d", totalNodes)
	}
}

// Test 10: Tasks with non-existent parent_id or depends_on → handle gracefully
func TestBuildTree_Phase8_EdgeCase_NonExistentReferences(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "orphan1", Title: "Orphan 1", Classification: "ready", Priority: "medium", Status: "pending", ParentID: "nonexistent_parent"},
		{ID: "orphan2", Title: "Orphan 2", Classification: "ready", Priority: "medium", Status: "pending", DependsOn: []string{"nonexistent_dep"}},
		{ID: "valid", Title: "Valid Task", Classification: "ready", Priority: "high", Status: "pending"},
	}
	nodes := BuildTree(tasks, tasks)

	// All tasks should appear as roots (since their references don't exist)
	if len(nodes) != 3 {
		t.Errorf("expected 3 root nodes, got %d", len(nodes))
	}

	// Verify all tasks are present
	foundOrphan1, foundOrphan2, foundValid := false, false, false
	for _, node := range nodes {
		switch node.Task.ID {
		case "orphan1":
			foundOrphan1 = true
		case "orphan2":
			foundOrphan2 = true
		case "valid":
			foundValid = true
		}
	}

	if !foundOrphan1 || !foundOrphan2 || !foundValid {
		t.Error("expected all tasks to be present in tree")
	}
}

// =============================================================================
// Phase 8: Benchmark Tests
// =============================================================================

// BenchmarkBuildTree_SmallTree tests performance with 10 tasks
func BenchmarkBuildTree_SmallTree(b *testing.B) {
	tasks := make([]types.ResolvedTask, 10)
	for i := 0; i < 10; i++ {
		tasks[i] = types.ResolvedTask{
			ID:             fmt.Sprintf("t%d", i),
			Title:          fmt.Sprintf("Task %d", i),
			Classification: "ready",
			Priority:       "medium",
			Status:         "pending",
		}
		if i > 0 {
			tasks[i].DependsOn = []string{fmt.Sprintf("t%d", i-1)}
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BuildTree(tasks, tasks)
	}
}

// BenchmarkBuildTree_MediumTree tests performance with 50 tasks
func BenchmarkBuildTree_MediumTree(b *testing.B) {
	tasks := make([]types.ResolvedTask, 50)
	for i := 0; i < 50; i++ {
		tasks[i] = types.ResolvedTask{
			ID:             fmt.Sprintf("t%d", i),
			Title:          fmt.Sprintf("Task %d", i),
			Classification: "ready",
			Priority:       "medium",
			Status:         "pending",
		}
		if i > 0 && i%10 != 0 {
			tasks[i].DependsOn = []string{fmt.Sprintf("t%d", i-1)}
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BuildTree(tasks, tasks)
	}
}

// BenchmarkBuildTree_LargeTree tests performance with 100 tasks (acceptance criteria)
func BenchmarkBuildTree_LargeTree(b *testing.B) {
	tasks := make([]types.ResolvedTask, 100)
	tasks[0] = types.ResolvedTask{
		ID:             "root",
		Title:          "Root",
		Classification: "ready",
		Priority:       "high",
		Status:         "pending",
	}

	// Mix of parent_id and depends_on relationships
	for i := 1; i < 100; i++ {
		tasks[i] = types.ResolvedTask{
			ID:             fmt.Sprintf("t%d", i),
			Title:          fmt.Sprintf("Task %d", i),
			Classification: "ready",
			Priority:       "medium",
			Status:         "pending",
		}

		if i%3 == 0 {
			// Use parent_id every 3rd task
			tasks[i].ParentID = fmt.Sprintf("t%d", i/3)
		} else if i > 1 {
			// Use depends_on for others
			tasks[i].DependsOn = []string{fmt.Sprintf("t%d", i-1)}
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BuildTree(tasks, tasks)
	}
}

// =============================================================================
// truncateTitle Tests
// =============================================================================

func TestTruncateTitle_NoTruncationNeeded(t *testing.T) {
	result := truncateTitle("Hello", 20)
	if result != "Hello" {
		t.Errorf("truncateTitle('Hello', 20) = %q, want %q", result, "Hello")
	}
}

func TestTruncateTitle_ExactFit(t *testing.T) {
	result := truncateTitle("Hello", 5)
	if result != "Hello" {
		t.Errorf("truncateTitle('Hello', 5) = %q, want %q", result, "Hello")
	}
}

func TestTruncateTitle_TruncatesLongTitle(t *testing.T) {
	result := truncateTitle("Hello World", 8)
	if len(result) == 0 {
		t.Fatal("truncateTitle returned empty string")
	}
	if !strings.HasSuffix(result, "…") {
		t.Errorf("truncateTitle('Hello World', 8) = %q, should end with ellipsis", result)
	}
	if len([]rune(result)) > 8 {
		t.Errorf("truncateTitle('Hello World', 8) = %q, rune length %d exceeds maxWidth 8", result, len([]rune(result)))
	}
}

func TestTruncateTitle_ZeroWidth(t *testing.T) {
	result := truncateTitle("Hello", 0)
	if result != "Hello" {
		t.Errorf("truncateTitle('Hello', 0) = %q, want %q", result, "Hello")
	}
}

func TestTruncateTitle_NegativeWidth(t *testing.T) {
	result := truncateTitle("Hello", -5)
	if result != "Hello" {
		t.Errorf("truncateTitle('Hello', -5) = %q, want %q", result, "Hello")
	}
}

func TestTruncateTitle_VerySmallWidth(t *testing.T) {
	result := truncateTitle("Hello World", 2)
	if len([]rune(result)) > 2 {
		t.Errorf("truncateTitle('Hello World', 2) rune length %d exceeds maxWidth 2", len([]rune(result)))
	}
}

func TestTruncateTitle_EmptyString(t *testing.T) {
	result := truncateTitle("", 10)
	if result != "" {
		t.Errorf("truncateTitle('', 10) = %q, want empty string", result)
	}
}

func TestTruncateTitle_WidthOfOne(t *testing.T) {
	result := truncateTitle("Hello", 1)
	if len([]rune(result)) > 1 {
		t.Errorf("truncateTitle('Hello', 1) rune length %d exceeds maxWidth 1", len([]rune(result)))
	}
}

// =============================================================================
// TextWrap rendering tests
// =============================================================================

func TestTaskTree_TextWrap_GroupedView_Truncates(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)
	tt.TextWrap = false

	tasks := []types.ResolvedTask{
		makeTask("t1", "This is a very long task title that should be truncated when text wrap is disabled", "ready", "medium", nil),
	}
	tt.SetTasks(tasks)
	tt.SelectedID = "t1"

	output := tt.ViewWithProject(40, 20, "test-project")
	if strings.Contains(output, "This is a very long task title that should be truncated when text wrap is disabled") {
		t.Error("Expected title to be truncated in grouped view with TextWrap=false and narrow width")
	}
}

func TestTaskTree_TextWrap_GroupedView_NoTruncate(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)
	tt.TextWrap = true

	tasks := []types.ResolvedTask{
		makeTask("t1", "This is a very long task title that should NOT be truncated", "ready", "medium", nil),
	}
	tt.SetTasks(tasks)
	tt.SelectedID = "t1"

	output := tt.ViewWithProject(40, 20, "test-project")
	if !strings.Contains(output, "This is a very long task title that should NOT be truncated") {
		t.Error("Expected full title in grouped view with TextWrap=true")
	}
}

func TestTaskTree_TextWrap_LegacyView_Truncates(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(false)
	tt.TextWrap = false

	tasks := []types.ResolvedTask{
		makeTask("t1", "This is a very long task title that should be truncated in legacy view", "ready", "medium", nil),
	}
	tt.SetTasks(tasks)
	tt.SelectedID = "t1"

	output := tt.ViewWithProject(30, 20, "test-project")
	if strings.Contains(output, "This is a very long task title that should be truncated in legacy view") {
		t.Error("Expected title to be truncated in legacy view with TextWrap=false and narrow width")
	}
}

func TestTaskTree_TextWrap_LaneView_Truncates(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(false)
	tt.SetLaneViewMode(true)
	tt.TextWrap = false

	tasks := []types.ResolvedTask{
		makeTask("t1", "This is a very long task title that should be truncated in lane view", "ready", "medium", nil),
	}
	tt.SetTasks(tasks)
	tt.SelectedID = "t1"

	output := tt.ViewWithProject(30, 20, "test-project")
	if strings.Contains(output, "This is a very long task title that should be truncated in lane view") {
		t.Error("Expected title to be truncated in lane view with TextWrap=false and narrow width")
	}
}
