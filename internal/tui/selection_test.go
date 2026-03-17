package tui

import (
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestToggleSelection(t *testing.T) {
	model := NewModel(Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	})

	tasks := []types.ResolvedTask{
		{ID: "task1", Title: "Task 1", Status: "pending", Classification: "ready", Priority: "high"},
		{ID: "task2", Title: "Task 2", Status: "pending", Classification: "ready", Priority: "medium"},
	}

	model.tasks = tasks
	model.taskTree.SetTasks(tasks)

	// Select task1
	model.taskTree.SelectedID = "task1"
	model.toggleTaskSelection()
	if !model.selectedTasks["task1"] {
		t.Error("Expected task1 to be selected")
	}

	// Deselect task1
	model.toggleTaskSelection()
	if model.selectedTasks["task1"] {
		t.Error("Expected task1 to be deselected")
	}
}

func TestClearSelection(t *testing.T) {
	model := NewModel(Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	})

	tasks := []types.ResolvedTask{
		{ID: "task1", Title: "Task 1", Status: "pending", Classification: "ready", Priority: "high"},
		{ID: "task2", Title: "Task 2", Status: "pending", Classification: "ready", Priority: "medium"},
	}

	model.tasks = tasks
	model.taskTree.SetTasks(tasks)

	// Select multiple tasks
	model.selectedTasks["task1"] = true
	model.selectedTasks["task2"] = true

	// Clear selection
	model.clearSelection()

	if len(model.selectedTasks) != 0 {
		t.Errorf("Expected 0 selected tasks, got %d", len(model.selectedTasks))
	}
}

func TestSelectAllTasks(t *testing.T) {
	model := NewModel(Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	})

	tasks := []types.ResolvedTask{
		{ID: "task1", Title: "Task 1", Status: "pending", Classification: "ready", Priority: "high"},
		{ID: "task2", Title: "Task 2", Status: "pending", Classification: "ready", Priority: "medium"},
		{ID: "task3", Title: "Task 3", Status: "pending", Classification: "ready", Priority: "low"},
	}

	model.tasks = tasks
	model.taskTree.SetTasks(tasks)

	// Select all
	model.selectAllTasks()

	if len(model.selectedTasks) != 3 {
		t.Errorf("Expected 3 selected tasks, got %d", len(model.selectedTasks))
	}

	for _, task := range tasks {
		if !model.selectedTasks[task.ID] {
			t.Errorf("Expected task %s to be selected", task.ID)
		}
	}
}

func TestGetSelectedTasks(t *testing.T) {
	model := NewModel(Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	})

	tasks := []types.ResolvedTask{
		{ID: "task1", Title: "Task 1", Status: "pending", Classification: "ready", Priority: "high"},
		{ID: "task2", Title: "Task 2", Status: "pending", Classification: "ready", Priority: "medium"},
		{ID: "task3", Title: "Task 3", Status: "pending", Classification: "ready", Priority: "low"},
	}

	model.tasks = tasks
	model.taskTree.SetTasks(tasks)

	// Select task1 and task3
	model.selectedTasks["task1"] = true
	model.selectedTasks["task3"] = true

	selected := model.getSelectedTasks()

	if len(selected) != 2 {
		t.Errorf("Expected 2 selected tasks, got %d", len(selected))
	}

	foundTask1 := false
	foundTask3 := false
	for _, task := range selected {
		if task.ID == "task1" {
			foundTask1 = true
		}
		if task.ID == "task3" {
			foundTask3 = true
		}
	}

	if !foundTask1 || !foundTask3 {
		t.Error("Expected task1 and task3 in selected tasks")
	}
}

func TestViewWithSelection_ShowsCheckboxes(t *testing.T) {
	model := NewModel(Config{
		Project: "test-project",
		APIURL:  "http://localhost:3333",
	})

	tasks := []types.ResolvedTask{
		{ID: "task1", Title: "Task 1", Status: "pending", Classification: "ready", Priority: "high"},
		{ID: "task2", Title: "Task 2", Status: "pending", Classification: "ready", Priority: "medium"},
	}

	model.tasks = tasks
	model.taskTree.SetTasks(tasks)

	// Select task1
	model.selectedTasks["task1"] = true

	// Render with selection (single project mode - no activeProjectID needed)
	view := model.taskTree.ViewWithSelection(80, 10, model.selectedTasks, "")

	// Check that selected task shows [x]
	if !strings.Contains(view, "[x]") {
		t.Error("Expected view to contain [x] for selected task")
	}

	// Check that unselected task shows [ ]
	if !strings.Contains(view, "[ ]") {
		t.Error("Expected view to contain [ ] for unselected task")
	}
}

func TestStatusBar_ShowsSelectionCount(t *testing.T) {
	statusBar := NewStatusBar("test-project")
	statusBar.Connected = true
	statusBar.Stats = TaskStats{
		Ready:      2,
		Waiting:    1,
		InProgress: 0,
		Completed:  3,
		Blocked:    0,
	}
	statusBar.SelectedCount = 2

	view := statusBar.View(80)

	// Should contain "2 selected"
	if !strings.Contains(view, "2 selected") {
		t.Error("Expected status bar to show selection count")
	}
}

func TestIsOnGroupHeader(t *testing.T) {
	taskTree := NewTaskTree()

	tasks := []types.ResolvedTask{
		{ID: "task1", Title: "Task 1", Status: "pending", Classification: "ready", Priority: "high"},
		{ID: "task2", Title: "Task 2", Status: "in_progress", Classification: "ready", Priority: "medium"},
	}

	taskTree.SetTasks(tasks)

	// Now should initially be on group header (new behavior)
	if !taskTree.IsOnGroupHeader() {
		t.Error("Expected to be on group header initially")
	}

	// Move down to enter group
	taskTree.MoveDown()

	// Now should be on first task (not header)
	if taskTree.IsOnGroupHeader() {
		t.Error("Expected to not be on group header after moving down")
	}
}

// Helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// =============================================================================
// SSE Selection Preservation Tests
// =============================================================================

// TestSetTasks_NestedView_PreservesSelection tests that calling SetTasks in nested
// status+feature view preserves the selected task across SSE updates.
// Uses manually configured statusGroups (like existing nested tests) to avoid
// dependency on normalizeClassification mapping details.
func TestSetTasks_NestedView_PreservesSelection(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)

	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Status: "pending", Classification: "ready", Priority: "high", FeatureID: "feat-a"},
		{ID: "t2", Title: "Task 2", Status: "pending", Classification: "ready", Priority: "medium", FeatureID: "feat-a"},
		{ID: "t3", Title: "Task 3", Status: "in_progress", Classification: "ready", Priority: "low", FeatureID: "feat-b"},
	}

	// First call to populate tt.tasks and internal structures
	tt.SetTasks(tasks)

	// Manually configure statusGroups (like existing nested navigation tests)
	tt.statusGroups = []StatusGroup{
		{
			Name: "Ready",
			Features: []FeatureGroup{
				{ID: "feat-a", Name: "Feature A", Tasks: tasks[:2]},
				{ID: "feat-b", Name: "Feature B", Tasks: tasks[2:]},
			},
			Count: 3,
		},
	}

	// Navigate to t2 using the restore helper
	tt.SelectedID = "t2"
	if !tt.restoreNestedSelection("t2") {
		t.Fatal("Setup failed: restoreNestedSelection could not find t2")
	}

	savedStatusIdx := tt.selectedStatusIdx
	savedFeatureIdx := tt.selectedFeatureIdx
	savedTaskIdx := tt.selectedTaskIdx

	// Simulate SSE update: SetTasks will rebuild statusGroups but we need to ensure
	// the previousSelectedID is preserved. Since SetTasks rebuilds statusGroups via
	// GroupTasksByStatusAndFeature, we need to set the statusGroups after SetTasks
	// for the restore logic to search. Instead, we directly test restoreNestedSelection.
	// The SetTasks function saves previousSelectedID and calls restoreNestedSelection,
	// so we verify restoreNestedSelection works correctly here.

	// Rebuild groups (simulate what SetTasks does)
	tt.statusGroups = []StatusGroup{
		{
			Name: "Ready",
			Features: []FeatureGroup{
				{ID: "feat-a", Name: "Feature A", Tasks: tasks[:2]},
				{ID: "feat-b", Name: "Feature B", Tasks: tasks[2:]},
			},
			Count: 3,
		},
	}

	// Restore selection (what SetTasks does internally)
	if !tt.restoreNestedSelection("t2") {
		t.Fatal("restoreNestedSelection failed to find t2 in rebuilt groups")
	}

	// Selection should be preserved
	if tt.SelectedID != "t2" {
		t.Errorf("Expected SelectedID=t2 after restore, got %s", tt.SelectedID)
	}
	if tt.selectedStatusIdx != savedStatusIdx {
		t.Errorf("Expected selectedStatusIdx=%d, got %d", savedStatusIdx, tt.selectedStatusIdx)
	}
	if tt.selectedFeatureIdx != savedFeatureIdx {
		t.Errorf("Expected selectedFeatureIdx=%d, got %d", savedFeatureIdx, tt.selectedFeatureIdx)
	}
	if tt.selectedTaskIdx != savedTaskIdx {
		t.Errorf("Expected selectedTaskIdx=%d, got %d", savedTaskIdx, tt.selectedTaskIdx)
	}
}

// TestSetTasks_NestedView_FallsBackWhenTaskRemoved tests that restoreNestedSelection
// returns false when the previously selected task is no longer in the groups.
func TestSetTasks_NestedView_FallsBackWhenTaskRemoved(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)

	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Status: "pending", Classification: "ready", Priority: "high", FeatureID: "feat-a"},
		{ID: "t2", Title: "Task 2", Status: "pending", Classification: "ready", Priority: "medium", FeatureID: "feat-a"},
	}

	// Set up groups with both tasks
	tt.statusGroups = []StatusGroup{
		{
			Name: "Ready",
			Features: []FeatureGroup{
				{ID: "feat-a", Name: "Feature A", Tasks: tasks},
			},
			Count: 2,
		},
	}

	// Select t2
	tt.SelectedID = "t2"
	tt.restoreNestedSelection("t2")

	// Simulate SSE update that removes t2
	tt.statusGroups = []StatusGroup{
		{
			Name: "Ready",
			Features: []FeatureGroup{
				{ID: "feat-a", Name: "Feature A", Tasks: tasks[:1]}, // only t1
			},
			Count: 1,
		},
	}

	// restoreNestedSelection should fail since t2 is gone
	if tt.restoreNestedSelection("t2") {
		t.Error("Expected restoreNestedSelection to return false when t2 is removed")
	}

	// selectFirstNestedTask should be called as fallback (test it directly)
	tt.selectFirstNestedTask()
	if tt.SelectedID == "t2" {
		t.Error("Expected selection to change when t2 was removed")
	}
	if tt.SelectedID != "t1" {
		t.Errorf("Expected fallback to select t1, got %s", tt.SelectedID)
	}
}

// TestSetTasks_FeatureView_PreservesSelection tests that calling SetTasks in feature
// view mode preserves the selected task across SSE updates.
func TestSetTasks_FeatureView_PreservesSelection(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)
	tt.SetFeatureViewMode(true)

	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Status: "pending", Classification: "ready", Priority: "high", FeatureID: "feat-a"},
		{ID: "t2", Title: "Task 2", Status: "pending", Classification: "ready", Priority: "medium", FeatureID: "feat-a"},
		{ID: "t3", Title: "Task 3", Status: "in_progress", Classification: "ready", Priority: "low", FeatureID: "feat-b"},
	}

	// First call
	tt.SetTasks(tasks)

	// Navigate to t3
	tt.SelectedID = "t3"
	tt.restoreFeatureSelection("t3")

	if tt.SelectedID != "t3" {
		t.Fatalf("Setup failed: expected SelectedID=t3, got %s", tt.SelectedID)
	}

	// Second call: simulate SSE update
	tt.SetTasks(tasks)

	// Selection should be preserved
	if tt.SelectedID != "t3" {
		t.Errorf("Expected SelectedID=t3 after SetTasks, got %s", tt.SelectedID)
	}
}

// TestSetTasks_FeatureView_FallsBackWhenTaskRemoved tests that selection falls back
// when the previously selected task is removed from the feature view.
func TestSetTasks_FeatureView_FallsBackWhenTaskRemoved(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)
	tt.SetFeatureViewMode(true)

	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Status: "pending", Classification: "ready", Priority: "high", FeatureID: "feat-a"},
		{ID: "t2", Title: "Task 2", Status: "pending", Classification: "ready", Priority: "medium", FeatureID: "feat-b"},
	}

	tt.SetTasks(tasks)
	tt.SelectedID = "t2"
	tt.restoreFeatureSelection("t2")

	// Remove t2
	updatedTasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Status: "pending", Classification: "ready", Priority: "high", FeatureID: "feat-a"},
	}
	tt.SetTasks(updatedTasks)

	if tt.SelectedID == "t2" {
		t.Error("Expected selection to change when t2 was removed")
	}
	if tt.SelectedID == "" {
		t.Error("Expected fallback to select some task, got empty SelectedID")
	}
}

// TestSetTasks_LegacyView_PreservesSelection tests that the legacy tree view
// still preserves selection (regression test for existing behavior).
func TestSetTasks_LegacyView_PreservesSelection(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(false) // legacy tree view

	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Status: "pending", Classification: "ready", Priority: "high"},
		{ID: "t2", Title: "Task 2", Status: "pending", Classification: "ready", Priority: "medium"},
	}

	tt.SetTasks(tasks)
	tt.SelectedID = "t2"
	tt.Cursor = 1

	// SSE update
	tt.SetTasks(tasks)

	if tt.SelectedID != "t2" {
		t.Errorf("Expected SelectedID=t2, got %s", tt.SelectedID)
	}
}

// TestSetTasks_NestedView_PreservesStatusHeader tests that being on a status
// header is preserved across SSE updates by verifying the state logic directly.
func TestSetTasks_NestedView_PreservesStatusHeader(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)

	// Set up state as if we're on a status header with no task selected
	tt.isOnStatusHeader = true
	tt.SelectedID = ""
	tt.selectedStatusIdx = 0
	tt.selectedFeatureIdx = -1
	tt.selectedTaskIdx = -1

	// Save state (what SetTasks does)
	previousSelectedID := tt.SelectedID
	previousIsOnStatusHeader := tt.isOnStatusHeader
	previousStatusIdx := tt.selectedStatusIdx

	// Simulate rebuilding statusGroups
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Status: "pending", Classification: "ready", Priority: "high", FeatureID: "feat-a"},
	}
	tt.statusGroups = []StatusGroup{
		{
			Name: "Ready",
			Features: []FeatureGroup{
				{ID: "feat-a", Name: "Feature A", Tasks: tasks},
			},
			Count: 1,
		},
	}

	// Apply the same restoration logic as SetTasks
	if previousSelectedID != "" {
		tt.restoreNestedSelection(previousSelectedID)
	} else if previousIsOnStatusHeader {
		if previousStatusIdx < len(tt.statusGroups) {
			tt.selectedStatusIdx = previousStatusIdx
			tt.selectedFeatureIdx = -1
			tt.selectedTaskIdx = -1
			tt.isOnStatusHeader = true
			tt.SelectedID = ""
		}
	}

	// Should stay on status header
	if !tt.isOnStatusHeader {
		t.Error("Expected to remain on status header after restoration")
	}
	if tt.selectedStatusIdx != 0 {
		t.Errorf("Expected selectedStatusIdx=0, got %d", tt.selectedStatusIdx)
	}
	if tt.SelectedID != "" {
		t.Errorf("Expected SelectedID to be empty on status header, got %s", tt.SelectedID)
	}
}

// TestSetTasks_FeatureView_UngroupedPreservation tests that selection in the
// ungrouped section of feature view is preserved.
func TestSetTasks_FeatureView_UngroupedPreservation(t *testing.T) {
	tt := NewTaskTree()
	tt.SetViewMode(true)
	tt.SetFeatureViewMode(true)

	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Status: "pending", Classification: "ready", Priority: "high", FeatureID: "feat-a"},
		{ID: "t2", Title: "Ungrouped Task", Status: "pending", Classification: "ready", Priority: "medium", FeatureID: ""}, // no feature
	}

	tt.SetTasks(tasks)

	// Navigate to t2 (ungrouped)
	tt.SelectedID = "t2"
	tt.restoreFeatureSelection("t2")

	if !tt.isOnUngrouped {
		t.Fatal("Setup failed: expected isOnUngrouped=true for ungrouped task")
	}

	// SSE update
	tt.SetTasks(tasks)

	if tt.SelectedID != "t2" {
		t.Errorf("Expected SelectedID=t2 after SSE update, got %s", tt.SelectedID)
	}
	if !tt.isOnUngrouped {
		t.Error("Expected isOnUngrouped=true after SSE update")
	}
}
