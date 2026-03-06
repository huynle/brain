package tui

import (
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
