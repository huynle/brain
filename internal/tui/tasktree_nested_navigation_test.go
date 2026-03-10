package tui

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Phase 3: 3-Level Navigation Tests
// =============================================================================

// TestNestedNavigation_InitialState tests that the tree starts with correct initial state
// when using nested status+feature grouping.
func TestNestedNavigation_InitialState(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true) // grouped view

	// Set up nested status+feature groups
	tasks := []types.ResolvedTask{
		makeTaskWithStatusAndFeature("t1", "Task 1", "ready", "high", "pending", "feat-a"),
		makeTaskWithStatusAndFeature("t2", "Task 2", "ready", "medium", "pending", "feat-a"),
	}

	// Manually set statusGroups to simulate Phase 1 output
	tt.statusGroups = []StatusGroup{
		{
			Name: "Ready",
			Features: []FeatureGroup{
				{
					ID:    "feat-a",
					Name:  "Feature A",
					Tasks: tasks,
					Stats: FeatureStats{Total: 2, Completed: 0},
				},
			},
			Count: 2,
		},
	}

	// Initial state should start on first status header
	if tt.selectedStatusIdx != 0 {
		t.Errorf("Expected selectedStatusIdx=0, got %d", tt.selectedStatusIdx)
	}
	if tt.selectedFeatureIdx != -2 {
		t.Errorf("Expected selectedFeatureIdx=-2 (none, on status header), got %d", tt.selectedFeatureIdx)
	}
	if tt.selectedTaskIdx != -1 {
		t.Errorf("Expected selectedTaskIdx=-1 (on header), got %d", tt.selectedTaskIdx)
	}
	if !tt.isOnStatusHeader {
		t.Error("Expected isOnStatusHeader=true")
	}
}

// TestNestedNavigation_MoveDownFromStatusHeader tests moving down from a status header.
func TestNestedNavigation_MoveDownFromStatusHeader(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)

	tasks := []types.ResolvedTask{
		makeTaskWithStatusAndFeature("t1", "Task 1", "ready", "high", "pending", "feat-a"),
	}

	tt.statusGroups = []StatusGroup{
		{
			Name: "Ready",
			Features: []FeatureGroup{
				{ID: "feat-a", Tasks: tasks},
			},
			Collapsed: false, // expanded
		},
	}

	// Start on status header
	tt.selectedStatusIdx = 0
	tt.selectedFeatureIdx = -2
	tt.selectedTaskIdx = -1
	tt.isOnStatusHeader = true

	// Move down - should move to first feature header
	tt.moveDownNestedGrouped()

	if tt.isOnStatusHeader {
		t.Error("Expected isOnStatusHeader=false after move down")
	}
	if tt.selectedStatusIdx != 0 {
		t.Errorf("Expected selectedStatusIdx=0, got %d", tt.selectedStatusIdx)
	}
	if tt.selectedFeatureIdx != 0 {
		t.Errorf("Expected selectedFeatureIdx=0 (first feature), got %d", tt.selectedFeatureIdx)
	}
	if tt.selectedTaskIdx != -1 {
		t.Errorf("Expected selectedTaskIdx=-1 (on feature header), got %d", tt.selectedTaskIdx)
	}
}

// TestNestedNavigation_MoveDownFromStatusHeaderCollapsed tests moving down when status is collapsed.
func TestNestedNavigation_MoveDownFromStatusHeaderCollapsed(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)

	tasks1 := []types.ResolvedTask{
		makeTaskWithStatusAndFeature("t1", "Task 1", "ready", "high", "pending", "feat-a"),
	}
	tasks2 := []types.ResolvedTask{
		makeTaskWithStatusAndFeature("t2", "Task 2", "waiting", "high", "pending", "feat-b"),
	}

	tt.statusGroups = []StatusGroup{
		{Name: "Ready", Features: []FeatureGroup{{ID: "feat-a", Tasks: tasks1}}, Collapsed: true},
		{Name: "Waiting", Features: []FeatureGroup{{ID: "feat-b", Tasks: tasks2}}, Collapsed: false},
	}

	// Start on collapsed Ready status header
	tt.selectedStatusIdx = 0
	tt.selectedFeatureIdx = -2
	tt.selectedTaskIdx = -1
	tt.isOnStatusHeader = true

	// Move down - should jump to next status header
	tt.moveDownNestedGrouped()

	if !tt.isOnStatusHeader {
		t.Error("Expected isOnStatusHeader=true (should jump to next status header)")
	}
	if tt.selectedStatusIdx != 1 {
		t.Errorf("Expected selectedStatusIdx=1 (Waiting), got %d", tt.selectedStatusIdx)
	}
	if tt.selectedFeatureIdx != -2 {
		t.Errorf("Expected selectedFeatureIdx=-2 (on status header), got %d", tt.selectedFeatureIdx)
	}
}

// TestNestedNavigation_MoveDownFromFeatureHeader tests moving down from a feature header.
func TestNestedNavigation_MoveDownFromFeatureHeader(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)

	tasks := []types.ResolvedTask{
		makeTaskWithStatusAndFeature("t1", "Task 1", "ready", "high", "pending", "feat-a"),
		makeTaskWithStatusAndFeature("t2", "Task 2", "ready", "medium", "pending", "feat-a"),
	}

	tt.statusGroups = []StatusGroup{
		{
			Name: "Ready",
			Features: []FeatureGroup{
				{ID: "feat-a", Tasks: tasks, Collapsed: false},
			},
		},
	}

	// Start on feature header (expanded)
	tt.selectedStatusIdx = 0
	tt.selectedFeatureIdx = 0
	tt.selectedTaskIdx = -1
	tt.isOnStatusHeader = false

	// Move down - should enter feature (move to first task)
	tt.moveDownNestedGrouped()

	if tt.selectedStatusIdx != 0 {
		t.Errorf("Expected selectedStatusIdx=0, got %d", tt.selectedStatusIdx)
	}
	if tt.selectedFeatureIdx != 0 {
		t.Errorf("Expected selectedFeatureIdx=0, got %d", tt.selectedFeatureIdx)
	}
	if tt.selectedTaskIdx != 0 {
		t.Errorf("Expected selectedTaskIdx=0 (first task), got %d", tt.selectedTaskIdx)
	}
	if tt.SelectedID != "t1" {
		t.Errorf("Expected SelectedID='t1', got '%s'", tt.SelectedID)
	}
}

// TestNestedNavigation_MoveDownFromFeatureHeaderCollapsed tests moving down when feature is collapsed.
func TestNestedNavigation_MoveDownFromFeatureHeaderCollapsed(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)

	tasks1 := []types.ResolvedTask{
		makeTaskWithStatusAndFeature("t1", "Task 1", "ready", "high", "pending", "feat-a"),
	}
	tasks2 := []types.ResolvedTask{
		makeTaskWithStatusAndFeature("t2", "Task 2", "ready", "high", "pending", "feat-b"),
	}

	tt.statusGroups = []StatusGroup{
		{
			Name: "Ready",
			Features: []FeatureGroup{
				{ID: "feat-a", Tasks: tasks1, Collapsed: true},
				{ID: "feat-b", Tasks: tasks2, Collapsed: false},
			},
		},
	}

	// Start on collapsed feature header
	tt.selectedStatusIdx = 0
	tt.selectedFeatureIdx = 0
	tt.selectedTaskIdx = -1
	tt.isOnStatusHeader = false

	// Move down - should jump to next feature header
	tt.moveDownNestedGrouped()

	if tt.selectedStatusIdx != 0 {
		t.Errorf("Expected selectedStatusIdx=0, got %d", tt.selectedStatusIdx)
	}
	if tt.selectedFeatureIdx != 1 {
		t.Errorf("Expected selectedFeatureIdx=1 (next feature), got %d", tt.selectedFeatureIdx)
	}
	if tt.selectedTaskIdx != -1 {
		t.Errorf("Expected selectedTaskIdx=-1 (on feature header), got %d", tt.selectedTaskIdx)
	}
}

// TestNestedNavigation_MoveDownWithinFeature tests moving down between tasks within a feature.
func TestNestedNavigation_MoveDownWithinFeature(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)

	tasks := []types.ResolvedTask{
		makeTaskWithStatusAndFeature("t1", "Task 1", "ready", "high", "pending", "feat-a"),
		makeTaskWithStatusAndFeature("t2", "Task 2", "ready", "medium", "pending", "feat-a"),
		makeTaskWithStatusAndFeature("t3", "Task 3", "ready", "low", "pending", "feat-a"),
	}

	tt.statusGroups = []StatusGroup{
		{
			Name: "Ready",
			Features: []FeatureGroup{
				{ID: "feat-a", Tasks: tasks, Collapsed: false},
			},
		},
	}

	// Start on first task
	tt.selectedStatusIdx = 0
	tt.selectedFeatureIdx = 0
	tt.selectedTaskIdx = 0
	tt.SelectedID = "t1"

	// Move down - should move to second task
	tt.moveDownNestedGrouped()

	if tt.selectedTaskIdx != 1 {
		t.Errorf("Expected selectedTaskIdx=1, got %d", tt.selectedTaskIdx)
	}
	if tt.SelectedID != "t2" {
		t.Errorf("Expected SelectedID='t2', got '%s'", tt.SelectedID)
	}
}

// TestNestedNavigation_MoveDownAtEndOfFeature tests moving down from the last task in a feature.
func TestNestedNavigation_MoveDownAtEndOfFeature(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)

	tasks1 := []types.ResolvedTask{
		makeTaskWithStatusAndFeature("t1", "Task 1", "ready", "high", "pending", "feat-a"),
	}
	tasks2 := []types.ResolvedTask{
		makeTaskWithStatusAndFeature("t2", "Task 2", "ready", "high", "pending", "feat-b"),
	}

	tt.statusGroups = []StatusGroup{
		{
			Name: "Ready",
			Features: []FeatureGroup{
				{ID: "feat-a", Tasks: tasks1, Collapsed: false},
				{ID: "feat-b", Tasks: tasks2, Collapsed: false},
			},
		},
	}

	// Start on last task of first feature
	tt.selectedStatusIdx = 0
	tt.selectedFeatureIdx = 0
	tt.selectedTaskIdx = 0
	tt.SelectedID = "t1"

	// Move down - should move to next feature header
	tt.moveDownNestedGrouped()

	if tt.selectedFeatureIdx != 1 {
		t.Errorf("Expected selectedFeatureIdx=1 (next feature), got %d", tt.selectedFeatureIdx)
	}
	if tt.selectedTaskIdx != -1 {
		t.Errorf("Expected selectedTaskIdx=-1 (on feature header), got %d", tt.selectedTaskIdx)
	}
	// SelectedID should stay as 't1' (last selected task) even when on header
	if tt.SelectedID != "t1" {
		t.Errorf("Expected SelectedID='t1' (preserved from last task), got '%s'", tt.SelectedID)
	}
}

// Helper function to create task with status and feature
func makeTaskWithStatusAndFeature(id, title, classification, priority, status, featureID string) types.ResolvedTask {
	return types.ResolvedTask{
		ID:             id,
		Title:          title,
		Classification: classification,
		Priority:       priority,
		Status:         status,
		FeatureID:      featureID,
	}
}
